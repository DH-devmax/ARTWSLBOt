package control

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/features"
)

// resolveScope never derives tenant ownership from native membership.
func (s *AssistantService) resolveScope(ctx context.Context, ref SessionRef, scope assistant.Scope) (assistant.Scope, error) {
	if scope.Platform == "" {
		scope.Platform = ref.assistantPlatform()
	}
	if scope.Platform != ref.assistantPlatform() {
		return scope, Denied(403, "assistant_scope_mismatch")
	}
	if scope.Kind != "group" {
		return scope, nil
	}
	if !ValidID(scope.GroupID) {
		return scope, Denied(400, "assistant_invalid_scope")
	}
	a, err := s.authorize(ctx, s.DB.Platform, ref, assistant.Scope{Kind: "overview", Platform: scope.Platform}, "assistant:use", false, false)
	if err != nil {
		return scope, err
	}
	err = RequireGroup(ctx, s.DB.Platform, a, scope.GroupID)
	if err != nil {
		return scope, err
	}
	var account string
	err = s.DB.Platform.QueryRow(ctx, `SELECT a.id::text FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id
		WHERE b.group_id=$1 AND b.active AND a.workspace=$2 AND a.platform=$2 AND a.purpose='management'
		AND ($3 OR b.tenant_id=nullif($4,'')::uuid)`, scope.GroupID, scope.Platform, a.Role == "superadmin", a.TenantID).Scan(&account)
	if err == pgx.ErrNoRows {
		return scope, Denied(404, "assistant_scope_not_found")
	}
	if err != nil {
		return scope, err
	}
	if scope.AccountID != "" && scope.AccountID != account {
		return scope, Denied(403, "assistant_scope_mismatch")
	}
	scope.AccountID = account
	return scope, nil
}

type AssistantContext struct {
	Platform       string                         `json:"platform"`
	Groups         json.RawMessage                `json:"groups"`
	Total          int                            `json:"total"`
	NextOffset     int                            `json:"next_offset"`
	Scope          *assistant.Scope               `json:"scope,omitempty"`
	Permissions    []string                       `json:"permissions"`
	Actions        []features.AssistantAction     `json:"actions"`
	SampledAt      time.Time                      `json:"sampled_at"`
	RuntimeEnabled bool                           `json:"runtime_enabled"`
	BlockedReason  string                         `json:"blocked_reason,omitempty"`
	ExecutionMode  string                         `json:"execution_mode"`
	CatalogVersion string                         `json:"catalog_version"`
	PolicyVersion  int64                          `json:"policy_version"`
	GuideVersion   string                         `json:"guide_version"`
	Capabilities   []AssistantEffectiveCapability `json:"capabilities"`
	GroupAccess    *HumanGroupAccess              `json:"group_access,omitempty"`
}
type assistantContextGroup struct {
	GroupID          string `json:"group_id"`
	Name             string `json:"name"`
	PrimaryAccountID string `json:"primary_account_id"`
	RobotNickname    string `json:"robot_nickname"`
	NativeID         string `json:"native_id"`
	Platform         string `json:"platform"`
	HumanGroupAccess
}
type AssistantEffectiveCapability struct {
	features.AssistantCapability
	Enabled       bool   `json:"enabled"`
	BlockedReason string `json:"blocked_reason,omitempty"`
}

func (s *AssistantService) Context(ctx context.Context, ref SessionRef, group, search string, offset, limit int) (AssistantContext, error) {
	out := AssistantContext{Platform: ref.assistantPlatform(), Groups: json.RawMessage("[]"), Actions: []features.AssistantAction{}, NextOffset: -1, SampledAt: time.Now().UTC()}
	if offset < 0 || offset > 300 || limit < 1 || limit > 50 || len(search) > 120 {
		return out, Denied(400, "assistant_invalid_cursor")
	}
	scope := assistant.Scope{Platform: out.Platform, Kind: "overview"}
	a, err := s.authorize(ctx, s.DB.Platform, ref, scope, "assistant:use", false, false)
	if err != nil {
		return out, err
	}
	entitlement, err := ResolveHumanEntitlement(ctx, s.DB.Platform, a, s.Catalog, out.Platform, group)
	if err != nil {
		return out, err
	}
	out.ExecutionMode = entitlement.Mode
	policy, err := assistantExposure(ctx, s.DB.Platform, out.Platform)
	if err != nil {
		return out, err
	}
	out.CatalogVersion, out.PolicyVersion, out.GuideVersion = features.AssistantCatalogVersion, policy.Version, assistant.GuideVersion
	out.Capabilities = []AssistantEffectiveCapability{}
	access, err := s.assistantModuleAccess(ctx, s.DB.Platform, a, scope)
	if err != nil {
		return out, err
	}
	for _, d := range policy.Catalog {
		c := AssistantEffectiveCapability{AssistantCapability: d, Enabled: true}
		if e := access.check(out.Platform, policy, d); e != nil {
			if f, ok := e.(*Failure); ok {
				c.Enabled, c.BlockedReason = false, f.Code
			} else {
				return out, e
			}
		}
		out.Capabilities = append(out.Capabilities, c)
	}
	out.Permissions, err = EffectivePermissions(ctx, s.DB.Platform, a, s.Catalog)
	if err != nil {
		return out, err
	}
	if group != "" {
		scope, err = s.resolveScope(ctx, ref, assistant.Scope{Platform: out.Platform, Kind: "group", GroupID: group})
		if err != nil {
			return out, err
		}
		out.Scope = &scope
		groupAccess, e := ResolveHumanGroupAccess(ctx, s.DB.Platform, a, out.Platform, group)
		if e != nil {
			return out, e
		}
		out.GroupAccess = &groupAccess
		for i := range out.Capabilities {
			c := &out.Capabilities[i]
			if c.Category != "query" && groupAccess.AccessMode == "read_only" {
				c.Enabled, c.BlockedReason = false, groupAccess.BlockedReason
			}
		}
		if runtimeErr := RequireGroupRuntime(ctx, s.DB.Platform, group); runtimeErr != nil {
			if failure, ok := runtimeErr.(*Failure); ok {
				out.BlockedReason = failure.Code
			} else {
				return out, runtimeErr
			}
		} else {
			out.RuntimeEnabled = true
		}
		out.Actions = features.AssistantActions(out.Platform)
		for i := range out.Actions {
			d := &out.Actions[i]
			resolved, e := ScopedHumanAction(ctx, s.DB.Platform, a, s.Catalog, out.Platform, group, d.ID, "assistant")
			if e != nil {
				return out, e
			}
			*d = resolved
			for _, c := range out.Capabilities {
				if c.Action == d.ID && !c.Enabled {
					d.BlockReason, d.Production = c.BlockedReason, false
				}
			}
			if !slices.Contains(out.Permissions, "assistant:action_propose") || !slices.Contains(out.Permissions, d.Permission) {
				d.BlockReason = "permission_denied"
				d.Production = false
			}
			if groupAccess.AccessMode == "read_only" {
				d.BlockReason, d.Production = groupAccess.BlockedReason, false
			}
		}
	}
	// Collect the visible page after capability reads, so an assignment revoked
	// while those reads ran cannot survive in the returned group list.
	search = strings.TrimSpace(search)
	spaceJoin, spaceScope := "", "TRUE"
	claimSQL := "false"
	if a.Role == "superadmin" {
		spaceJoin = `JOIN tenants t ON t.id=b.tenant_id`
		spaceScope = `(t.space_kind='tenant_business' OR workspace_is_demo(t.id,$4::uuid))`
		claimSQL = operatorDemoClaimVisibleSQL
	}
	// Ownership evidence is control-private. Project only directory metadata here,
	// with the same explicit tenant/member scope used by RequireGroup.
	err = s.DB.Platform.QueryRow(ctx, `WITH visible AS (
			SELECT b.group_id,g.name,a.id AS primary_account_id,
			coalesce(nullif(a.native_nickname,''),a.name) AS robot_nickname,g.native_id,a.platform,
			coalesce(o.state,'unverified') AS ownership_state,
			`+claimSQL+` AS demo_claim_active
			FROM group_bindings b JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
			LEFT JOIN group_binding_ownership o ON o.group_id=b.group_id AND o.tenant_id=b.tenant_id AND o.platform=g.platform
			`+spaceJoin+`
			WHERE b.active AND a.platform=$1 AND a.workspace=$1 AND a.purpose='management'
			AND `+spaceScope+`
			AND `+controlledBindingVisibleSQL+`
			AND ($2 OR b.tenant_id=nullif($3,'')::uuid AND member_group_visible(b.tenant_id,b.group_id,$4::uuid))
			AND ($2 OR o.state='active')
			AND ($5='' OR strpos(lower(g.name),lower($5))>0)
		), page AS (SELECT * FROM visible ORDER BY group_id LIMIT $6 OFFSET $7)
		SELECT (SELECT count(*)::int FROM visible),coalesce((SELECT jsonb_agg(to_jsonb(page)) FROM page),'[]')`,
		out.Platform, a.Role == "superadmin", a.TenantID, a.ID, search, limit, offset).Scan(&out.Total, &out.Groups)
	if err != nil {
		return out, err
	}
	var records []struct {
		assistantContextGroup
		DemoClaimActive bool `json:"demo_claim_active"`
	}
	if err = json.Unmarshal(out.Groups, &records); err != nil {
		return out, err
	}
	groups := make([]assistantContextGroup, 0, len(records))
	for _, record := range records {
		if err = RequireControlledBinding(ctx, s.DB.Platform, a.ID, out.Platform, record.GroupID); err != nil {
			return out, err
		}
		record.HumanGroupAccess = humanGroupAccess(record.OwnershipState, a.Role == "superadmin", record.DemoClaimActive)
		groups = append(groups, record.assistantContextGroup)
	}
	out.Groups, err = json.Marshal(groups)
	if err != nil {
		return out, err
	}
	if offset+limit < out.Total {
		out.NextOffset = offset + limit
	}
	a, err = s.authorize(ctx, s.DB.Platform, ref, scope, "assistant:use", false, false)
	if err != nil {
		return out, err
	}
	out.Permissions, err = EffectivePermissions(ctx, s.DB.Platform, a, s.Catalog)
	if err != nil {
		return out, err
	}
	if group != "" {
		currentAccess, e := ResolveHumanGroupAccess(ctx, s.DB.Platform, a, out.Platform, group)
		if e != nil {
			return out, e
		}
		out.GroupAccess = &currentAccess
		if currentAccess.AccessMode == "read_only" {
			for i := range out.Capabilities {
				if out.Capabilities[i].Category != "query" {
					out.Capabilities[i].Enabled, out.Capabilities[i].BlockedReason = false, currentAccess.BlockedReason
				}
			}
			for i := range out.Actions {
				out.Actions[i].Production, out.Actions[i].BlockReason = false, currentAccess.BlockedReason
			}
		}
	}
	for i := range out.Actions {
		if !slices.Contains(out.Permissions, "assistant:action_propose") || !slices.Contains(out.Permissions, out.Actions[i].Permission) {
			out.Actions[i].Production, out.Actions[i].BlockReason = false, "permission_denied"
		}
	}
	current, err := assistantExposure(ctx, s.DB.Platform, out.Platform)
	if err != nil {
		return out, err
	}
	if current.Version != out.PolicyVersion {
		return out, Denied(409, "assistant_module_policy_changed")
	}
	return out, err
}
