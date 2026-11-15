package control

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/database"
	"wangshangliao/internal/security"
)

type AssistantService struct {
	DB             *database.DB
	Vault          *security.Vault
	Catalog        []PermissionSpec
	Gateway        *GatewayService
	NativeMembers  AssistantMemberReader
	NativeMessages AssistantMessageReader
	ConfigRoles    func(context.Context, GroupBindingNativeScope) (GroupBindingRoleEvidence, error)
	ActionExecutor AssistantActionExecutor
	ReceiptReader  AssistantReceiptReader
}

type AssistantSettings struct {
	Enabled          bool   `json:"enabled"`
	TenantAccess     bool   `json:"tenant_access"`
	ApprovalsEnabled bool   `json:"approvals_enabled"`
	MonthlyTokens    int64  `json:"monthly_tokens"`
	Version          int64  `json:"version"`
	BaseURL          string `json:"base_url"`
	Model            string `json:"model"`
	APIKey           string `json:"api_key,omitempty"`
	Configured       bool   `json:"configured"`
}

func (s *AssistantService) settings(ctx context.Context, q Queryer) (AssistantSettings, assistant.ModelConfig, error) {
	var settings AssistantSettings
	var sealed []byte
	err := q.QueryRow(ctx, `SELECT enabled,tenant_access,approvals_enabled,monthly_tokens,version,sealed
        FROM assistant_settings WHERE singleton`).Scan(&settings.Enabled, &settings.TenantAccess,
		&settings.ApprovalsEnabled, &settings.MonthlyTokens, &settings.Version, &sealed)
	var model assistant.ModelConfig
	if err != nil {
		return settings, model, err
	}
	if len(sealed) > 0 && s.Gateway == nil {
		raw, e := s.Vault.Open("assistant:model", sealed)
		if e != nil {
			return settings, model, e
		}
		if e = json.Unmarshal(raw, &model); e != nil {
			return settings, model, e
		}
		settings.BaseURL, settings.Model, settings.Configured = model.BaseURL, model.Model, true
	}
	if s.Gateway != nil {
		settings.BaseURL = ""
		settings.Model = "default"
		if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM ai_gateway_settings s JOIN ai_model_routes r ON r.alias='default'
            JOIN ai_model_rates m ON m.id=r.rate_id WHERE s.enabled AND r.enabled AND m.verified)`).Scan(&settings.Configured); err != nil {
			return settings, model, err
		}
		model = assistant.ModelConfig{}
	}
	return settings, model, nil
}

func (s *AssistantService) Settings(ctx context.Context, ref SessionRef) (AssistantSettings, error) {
	a, err := s.actor(ctx, s.DB.Platform, ref, false)
	if err != nil {
		return AssistantSettings{}, err
	}
	if a.Role != "superadmin" {
		return AssistantSettings{}, Denied(403, "superadmin_required")
	}
	out, _, err := s.settings(ctx, s.DB.Platform)
	return out, err
}

func (s *AssistantService) SaveSettings(ctx context.Context, ref SessionRef, in AssistantSettings) (AssistantSettings, error) {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return in, err
	}
	defer tx.Rollback(ctx)
	a, err := s.actor(ctx, tx, ref, true)
	if err != nil {
		return in, err
	}
	if a.Role != "superadmin" {
		return in, Denied(403, "superadmin_required")
	}
	if _, err = tx.Exec(ctx, `SELECT singleton FROM assistant_settings FOR UPDATE`); err != nil {
		return in, err
	}
	old, model, err := s.settings(ctx, tx)
	if err != nil {
		return in, err
	}
	if old.Version != in.Version {
		return in, Denied(409, "version_conflict")
	}
	if in.MonthlyTokens < 0 || in.MonthlyTokens > 1e12 {
		return in, Denied(400, "invalid_assistant_settings")
	}
	model.BaseURL, model.Model = strings.TrimSpace(in.BaseURL), strings.TrimSpace(in.Model)
	if in.APIKey != "" {
		model.APIKey = in.APIKey
	}
	var sealed []byte
	if s.Gateway != nil {
		if in.APIKey != "" || in.BaseURL != "" || in.Model != "" && in.Model != "default" {
			return in, Denied(409, "use_ai_gateway_configuration")
		}
	} else if model.BaseURL != "" || model.Model != "" || model.APIKey != "" {
		if model.Validate() != nil {
			return in, Denied(400, "invalid_assistant_model")
		}
		raw, _ := json.Marshal(model)
		sealed, err = s.Vault.Seal("assistant:model", raw)
		if err != nil {
			return in, err
		}
	} else if in.Enabled && s.Gateway == nil {
		return in, Denied(409, "assistant_not_configured")
	}
	if in.Enabled && in.MonthlyTokens == 0 && s.Gateway == nil {
		return in, Denied(409, "assistant_budget_exhausted")
	}
	err = tx.QueryRow(ctx, `UPDATE assistant_settings SET enabled=$1,tenant_access=$2,approvals_enabled=$3,
        monthly_tokens=$4,sealed=$5,version=version+1,updated_at=now() WHERE singleton RETURNING version`,
		in.Enabled, in.TenantAccess, in.ApprovalsEnabled, in.MonthlyTokens, sealed).Scan(&in.Version)
	if err != nil {
		return in, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,resource_id,detail)
        VALUES($1,'assistant_settings_updated','assistant',jsonb_build_object('version',$2::bigint))`, a.ID, in.Version)
	if err != nil {
		return in, err
	}
	if err = tx.Commit(ctx); err != nil {
		return in, err
	}
	in.APIKey = ""
	in.Configured = len(sealed) > 0
	return in, nil
}

func (s *AssistantService) actor(ctx context.Context, q Queryer, ref SessionRef, lock bool) (Actor, error) {
	return sessionActor(ctx, q, ref, lock)
}

func sessionActor(ctx context.Context, q Queryer, ref SessionRef, lock bool) (Actor, error) {
	var a Actor
	if len(ref.hash) != 32 {
		return a, Denied(401, "authentication_required")
	}
	sql := `SELECT u.id::text,coalesce(u.tenant_id::text,''),u.role FROM sessions se JOIN users u ON u.id=se.user_id
        WHERE se.token_hash=$1 AND se.expires_at>clock_timestamp() AND NOT u.disabled`
	if lock {
		sql += ` FOR SHARE OF se,u`
	}
	err := q.QueryRow(ctx, sql, ref.hash).Scan(&a.ID, &a.TenantID, &a.Role)
	if err == pgx.ErrNoRows {
		return a, Denied(401, "authentication_required")
	}
	return a, err
}

func validScope(scope assistant.Scope) bool {
	if scope.Platform != "" && scope.Platform != "wsl" && scope.Platform != "lajiaoliao" {
		return false
	}
	switch scope.Kind {
	case "overview":
		return scope.AccountID == "" && scope.GroupID == ""
	case "account":
		return ValidID(scope.AccountID) && scope.GroupID == ""
	case "group":
		return ValidID(scope.AccountID) && ValidID(scope.GroupID)
	}
	return false
}

func (s *AssistantService) authorize(ctx context.Context, q Queryer, ref SessionRef, scope assistant.Scope,
	permission string, active, lock bool) (Actor, error) {
	a, err := s.actor(ctx, q, ref, lock)
	if err != nil {
		return a, err
	}
	if !validScope(scope) {
		return a, Denied(400, "assistant_invalid_scope")
	}
	platform := ref.assistantPlatform()
	if platform != "wsl" && platform != "lajiaoliao" || scope.Platform != "" && scope.Platform != platform {
		return a, Denied(403, "assistant_scope_mismatch")
	}
	grants, err := EffectivePermissions(ctx, q, a, s.Catalog)
	if err != nil {
		return a, err
	}
	if !slices.Contains(grants, permission) || !slices.Contains(grants, "assistant:use") {
		return a, Denied(403, "permission_denied")
	}
	settings, _, err := s.settings(ctx, q)
	if err != nil {
		return a, err
	}
	if a.Role != "superadmin" && !settings.TenantAccess {
		return a, Denied(403, "assistant_admin_preview")
	}
	entitlement, err := ResolveHumanEntitlement(ctx, q, a, s.Catalog, platform, scope.GroupID)
	if err != nil {
		return a, err
	}
	if err = entitlement.RequireModule("management_assistant"); err != nil {
		return a, err
	}
	if active && (!settings.Enabled || !settings.Configured) {
		return a, Denied(409, "assistant_disabled")
	}
	if scope.Kind != "overview" {
		if scope.Kind == "group" {
			if err = RequireGroup(ctx, q, a, scope.GroupID); err != nil {
				return a, err
			}
			if a.Role != "superadmin" || permission != "assistant:use" {
				if err = RequireHumanGroupOwnership(ctx, q, a, platform, scope.GroupID); err != nil {
					return a, err
				}
			}
		}
		var ok bool
		err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts a WHERE a.id=$1 AND a.workspace=$6 AND a.platform=$6
            AND ($2::boolean OR EXISTS(SELECT 1 FROM group_bindings b WHERE b.primary_account_id=a.id
                AND b.tenant_id=nullif($3,'')::uuid AND b.active
                AND member_group_visible(b.tenant_id,b.group_id,nullif($5,'')::uuid)))
            AND ($4='' OR EXISTS(SELECT 1 FROM group_bindings b WHERE b.group_id=nullif($4,'')::uuid
                AND b.primary_account_id=a.id AND b.active AND ($2::boolean OR b.tenant_id=nullif($3,'')::uuid))))`,
			scope.AccountID, a.Role == "superadmin", a.TenantID, scope.GroupID, a.ID, platform).Scan(&ok)
		if err != nil {
			return a, err
		}
		if !ok {
			return a, Denied(404, "assistant_scope_not_found")
		}
	}
	return a, nil
}

func (s *AssistantService) read(ctx context.Context, a Actor, platform string, fn func(Queryer) error) error {
	if a.Role == "superadmin" {
		return fn(s.DB.Platform)
	}
	return s.DB.ActorTenantDomainTx(ctx, a.TenantID, a.ID, platform, func(tx pgx.Tx) error { return fn(tx) })
}

type AssistantSession struct {
	ID        string          `json:"id"`
	Actor     Actor           `json:"-"`
	Scope     assistant.Scope `json:"scope"`
	CreatedAt time.Time       `json:"created_at"`
	ExpiresAt time.Time       `json:"expires_at"`
	IsEmpty   bool            `json:"is_empty"`
}

func (s *AssistantService) CreateSession(ctx context.Context, ref SessionRef, scope assistant.Scope) (AssistantSession, error) {
	if scope.Platform == "" {
		scope.Platform = ref.assistantPlatform()
	}
	var resolveErr error
	scope, resolveErr = s.resolveScope(ctx, ref, scope)
	if resolveErr != nil {
		return AssistantSession{}, resolveErr
	}
	a, err := s.authorize(ctx, s.DB.Platform, ref, scope, "assistant:use", false, false)
	out := AssistantSession{ID: NewID(), Actor: a, Scope: scope, IsEmpty: true}
	if err != nil {
		return out, err
	}
	// Body lifetime is bounded by this session, including turns created near expiry.
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,798441))`, a.ID); err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT id::text,created_at,expires_at FROM assistant_sessions s
		WHERE actor_id=$1 AND domain=$5 AND scope_kind=$2
		AND coalesce(account_id::text,'')=$3 AND coalesce(group_id::text,'')=$4
		AND deleted_at IS NULL AND expires_at>clock_timestamp() AND NOT EXISTS(SELECT 1 FROM assistant_turns WHERE session_id=s.id)
		ORDER BY created_at DESC,id DESC LIMIT 1`,
		a.ID, scope.Kind, scope.AccountID, scope.GroupID, scope.Platform).Scan(&out.ID, &out.CreatedAt, &out.ExpiresAt)
	if err == nil {
		if _, err = s.authorize(ctx, tx, ref, scope, "assistant:use", false, true); err != nil {
			return out, err
		}
		return out, tx.Commit(ctx)
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	var count int
	if err = tx.QueryRow(ctx, `SELECT count(*) FROM assistant_sessions WHERE actor_id=$1 AND deleted_at IS NULL AND expires_at>clock_timestamp()`, a.ID).Scan(&count); err != nil {
		return out, err
	}
	if count >= 100 {
		return out, Denied(429, "assistant_session_limit")
	}
	err = tx.QueryRow(ctx, `INSERT INTO assistant_sessions(id,actor_id,tenant_id,scope_kind,account_id,group_id,domain)
        VALUES($1,$2,nullif($3,'')::uuid,$4,nullif($5,'')::uuid,nullif($6,'')::uuid,$7)
        RETURNING created_at,expires_at`, out.ID, a.ID, a.TenantID, scope.Kind, scope.AccountID, scope.GroupID, scope.Platform).Scan(&out.CreatedAt, &out.ExpiresAt)
	if err != nil {
		return out, err
	}
	_, err = s.authorize(ctx, tx, ref, scope, "assistant:use", false, true)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}

func (s *AssistantService) Session(ctx context.Context, ref SessionRef, id string, active bool) (AssistantSession, error) {
	var out AssistantSession
	if !ValidID(id) {
		return out, Denied(404, "assistant_session_not_found")
	}
	a, err := s.actor(ctx, s.DB.Platform, ref, false)
	if err != nil {
		return out, err
	}
	err = s.read(ctx, a, ref.assistantPlatform(), func(q Queryer) error {
		return q.QueryRow(ctx, `SELECT id::text,actor_id::text,coalesce(tenant_id::text,''),scope_kind,
            coalesce(account_id::text,''),coalesce(group_id::text,''),created_at,expires_at,
			NOT EXISTS(SELECT 1 FROM assistant_turns WHERE session_id=assistant_sessions.id) FROM assistant_sessions
            WHERE id=$1 AND actor_id=$2 AND domain=$3 AND deleted_at IS NULL AND expires_at>clock_timestamp()`, id, a.ID, ref.assistantPlatform()).
			Scan(&out.ID, &out.Actor.ID, &out.Actor.TenantID, &out.Scope.Kind, &out.Scope.AccountID, &out.Scope.GroupID, &out.CreatedAt, &out.ExpiresAt, &out.IsEmpty)
	})
	out.Scope.Platform = ref.assistantPlatform()
	if err == pgx.ErrNoRows {
		return out, Denied(404, "assistant_session_not_found")
	}
	if err != nil {
		return out, err
	}
	out.Actor, err = s.authorize(ctx, s.DB.Platform, ref, out.Scope, "assistant:use", active, false)
	return out, err
}

func strictJSON(raw string, out any) error {
	d := json.NewDecoder(strings.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil || d.Decode(new(any)) != io.EOF {
		return Denied(400, "assistant_invalid_tool")
	}
	return nil
}

func digest(data any) string {
	raw, _ := json.Marshal(data)
	return hex.EncodeToString(security.Digest(string(raw)))
}

func assistantCode(err error) string {
	if errors.Is(err, context.Canceled) {
		return "assistant_cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "assistant_expired"
	}
	var failure *Failure
	if errors.As(err, &failure) {
		return failure.Code
	}
	var failureCode assistant.Error
	if errors.As(err, &failureCode) {
		return string(failureCode)
	}
	return "assistant_unavailable"
}

func equalDigest(a, b []byte) bool { return bytes.Equal(a, b) }
