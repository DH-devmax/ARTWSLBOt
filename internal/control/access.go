package control

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/features"
	"wangshangliao/internal/security"
)

type Actor struct {
	ID, TenantID, Role string
}

type PermissionSpec struct {
	Code   string `json:"code"`
	Label  string `json:"label"`
	Module string `json:"module,omitempty"`
}

type Queryer interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type SessionRef struct {
	hash     []byte
	platform string
}

// InPlatform selects a view, never an identity or authorization.
func (r SessionRef) InPlatform(platform string) SessionRef {
	r.platform = platform
	return r
}

func (r SessionRef) assistantPlatform() string {
	if r.platform == "" {
		return "wsl"
	}
	return r.platform
}

func LiveActorPermissions(ctx context.Context, tx pgx.Tx, actor Actor, catalog []PermissionSpec) ([]string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT id::text FROM users WHERE id=$1 AND NOT disabled AND role=$2
        AND coalesce(tenant_id::text,'')=$3 FOR SHARE`, actor.ID, actor.Role, actor.TenantID).Scan(&id)
	if err == pgx.ErrNoRows {
		return nil, Denied(403, "permission_denied")
	}
	if err != nil {
		return nil, err
	}
	return EffectivePermissions(ctx, tx, actor, catalog)
}

func WebsiteSession(cookie string) (SessionRef, error) {
	if len(cookie) != 43 {
		return SessionRef{}, errors.New("authentication_required")
	}
	return SessionRef{hash: security.Digest(cookie)}, nil
}

func RolePermissions(role string, catalog []PermissionSpec) []string {
	out := []string{}
	for _, spec := range catalog {
		// New assistant permissions never inherit the legacy manager fallback.
		if strings.HasPrefix(spec.Code, "assistant:") || strings.HasPrefix(spec.Code, "assistant_module:") || strings.HasPrefix(spec.Code, "group_action:") {
			continue
		}
		if role == "manager" || role == "operator" && spec.Code != "groups:revoke" && spec.Module != "finance" ||
			role == "viewer" && strings.HasSuffix(spec.Code, ":read") {
			out = append(out, spec.Code)
		}
	}
	return out
}

func EffectivePermissions(ctx context.Context, q Queryer, actor Actor, catalog []PermissionSpec) ([]string, error) {
	if actor.Role == "superadmin" {
		out := []string{}
		for _, spec := range catalog {
			out = append(out, spec.Code)
		}
		return out, nil
	}
	var allowed []string
	err := q.QueryRow(ctx, `SELECT permissions FROM user_access WHERE user_id=$1`, actor.ID).Scan(&allowed)
	hasIndividual := err == nil
	if err == pgx.ErrNoRows {
		allowed = []string{}
	} else if err != nil {
		return nil, err
	}
	var role string
	var owner bool
	if err = q.QueryRow(ctx, `SELECT u.tenant_role,u.tenant_role='owner' AND t.owner_id=u.id
		FROM users u JOIN tenants t ON t.id=u.tenant_id WHERE u.id=$1 AND u.tenant_id=$2 AND NOT u.disabled AND u.role='user'`,
		actor.ID, actor.TenantID).Scan(&role, &owner); err != nil {
		if err == pgx.ErrNoRows {
			return nil, Denied(403, "permission_denied")
		}
		return nil, err
	}
	if role != "legacy" && !hasIndividual {
		allowed = []string{}
		err = q.QueryRow(ctx, `SELECT permissions FROM tenant_roles WHERE tenant_id=$1 AND code=$2`,
			actor.TenantID, role).Scan(&allowed)
		if err != nil && err != pgx.ErrNoRows {
			return nil, err
		}
	}
	var overrides []string
	if err = q.QueryRow(ctx, `SELECT coalesce(array_agg(permission||':'||effect),'{}') FROM member_overrides
        WHERE user_id=$1 AND (expires_at IS NULL OR expires_at>clock_timestamp())`, actor.ID).Scan(&overrides); err != nil {
		return nil, err
	}
	denied := map[string]bool{}
	for _, item := range overrides {
		code, effect, _ := strings.Cut(item, ":allow")
		if effect == "" && strings.HasSuffix(item, ":deny") {
			denied[strings.TrimSuffix(item, ":deny")] = true
		} else {
			allowed = append(allowed, code)
		}
	}
	// A field-specific deny must also close its legacy module-wide editing alias.
	for code := range denied {
		parts := strings.Split(code, ":")
		if len(parts) == 3 && parts[0] == "feature" {
			definition, ok := features.Find(parts[1])
			if ok && definition.Legacy != "" &&
				features.CanEdit([]string{definition.Legacy}, definition, parts[2]) {
				denied[definition.Legacy] = true
			}
		}
	}
	modules := []string{}
	var aiLimit int
	err = q.QueryRow(ctx, `SELECT p.modules,p.ai_group_limit FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
        WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>clock_timestamp()`,
		actor.TenantID).Scan(&modules, &aiLimit)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}
	var ceiling []string
	err = q.QueryRow(ctx, `SELECT permissions FROM tenant_capability_ceilings WHERE tenant_id=$1`, actor.TenantID).Scan(&ceiling)
	if err != nil && err != pgx.ErrNoRows {
		return nil, err
	}
	out := []string{}
	for _, spec := range catalog {
		entitled := moduleEntitled(modules, spec.Module)
		if spec.Module == "ai" {
			entitled = entitled && aiLimit > 0
		}
		if spec.Code == "members:manage" && !owner {
			continue
		}
		if entitled && ceilingAllows(ceiling, spec.Code) && slices.Contains(allowed, spec.Code) && !denied[spec.Code] {
			out = append(out, spec.Code)
		}
	}
	return out, nil
}

func RequireGroup(ctx context.Context, q Queryer, actor Actor, group string) error {
	if !ValidID(group) || !ValidID(actor.ID) || actor.Role != "superadmin" && !ValidID(actor.TenantID) {
		return Denied(404, "group_not_found")
	}
	var ok bool
	spaceJoin, spaceScope := "", "TRUE"
	if actor.Role == "superadmin" {
		spaceJoin = `JOIN tenants t ON t.id=b.tenant_id`
		spaceScope = `(t.space_kind='tenant_business' OR workspace_is_demo(t.id,$3::uuid))`
	}
	if err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_bindings b
		JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
		`+spaceJoin+`
		WHERE b.group_id=$2 AND b.active AND a.purpose='management' AND a.platform=g.platform
		AND `+spaceScope+`
		AND a.workspace=g.platform AND ($4 OR b.tenant_id=nullif($1,'')::uuid
		AND member_group_visible(b.tenant_id,b.group_id,$3::uuid)))`,
		actor.TenantID, group, actor.ID, actor.Role == "superadmin").Scan(&ok); err != nil {
		return err
	}
	if !ok {
		return Denied(404, "group_not_found")
	}
	var platform string
	if err := q.QueryRow(ctx, `SELECT platform FROM native_groups WHERE id=$1`, group).Scan(&platform); err != nil {
		return err
	}
	return RequireControlledBinding(ctx, q, actor.ID, platform, group)
}

func NewID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("random source unavailable")
	}
	b[6], b[8] = (b[6]&15)|64, (b[8]&63)|128
	s := hex.EncodeToString(b)
	return s[:8] + "-" + s[8:12] + "-" + s[12:16] + "-" + s[16:20] + "-" + s[20:]
}

func ValidID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, r := range s {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}
