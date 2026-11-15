package httpapi

import (
	"encoding/json"
	"net/http"
	"slices"
	"sort"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/control"
	"wangshangliao/internal/security"
)

func (s *Server) userCenter(w http.ResponseWriter, r *http.Request, p Principal) error {
	if p.Role == "superadmin" {
		return s.operatorUserCenter(w, r, p)
	}
	var subscription []byte
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT coalesce((SELECT jsonb_build_object(
        'name',p.name,'expires_at',s.expires_at,'enabled',s.enabled,'active',s.enabled AND s.expires_at>now(),
        'group_limit',p.group_limit,'ai_group_limit',p.ai_group_limit,'monthly_ai_tokens',p.monthly_ai_tokens,
        'platforms',p.allowed_platforms,'modules',p.modules,'member_limit',p.member_limit,
        'billing_mode',CASE WHEN EXISTS(SELECT 1 FROM commercial_plans cp WHERE cp.plan_id=p.id) THEN 'credits' ELSE 'legacy' END)
        FROM subscriptions s JOIN plans p ON p.id=s.plan_id WHERE s.tenant_id=$1),'null'::jsonb)`,
		p.TenantID).Scan(&subscription)
	if err != nil {
		return err
	}
	grants, err := s.permissions(r.Context(), p)
	if err != nil {
		return err
	}
	entitlement, err := control.ResolveHumanEntitlement(r.Context(), s.DB.Platform, principalActor(p), tenantPermissions, requestWorkspace(r), "")
	if err != nil {
		return err
	}
	groupData, err := s.userCenterGroupData(r, p)
	if err != nil {
		return err
	}
	groups, err := json.Marshal(groupData["items"])
	if err != nil {
		return err
	}
	personas := []byte("[]")
	claims, operations := []byte("[]"), []byte("[]")
	if slices.Contains(grants, "claims:read") || slices.Contains(grants, "operations:read") {
		err = s.DB.TenantTx(r.Context(), p.TenantID, func(tx pgx.Tx) error {
			if slices.Contains(grants, "claims:read") {
				if e := tx.QueryRow(r.Context(), `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]')FROM(
                    SELECT c.id,c.state,c.created_at,g.name,g.platform FROM group_claims c
                    JOIN native_groups g ON g.id=c.group_id ORDER BY c.created_at DESC LIMIT 100)x`).Scan(&claims); e != nil {
					return e
				}
			}
			return nil
		})
		if err != nil {
			return err
		}
	}
	if slices.Contains(grants, "operations:read") {
		combined := []map[string]any{}
		for _, platform := range []string{"wsl", "lajiaoliao"} {
			raw, e := control.SafeBusinessOperations(r.Context(), s.DB.Platform, principalActor(p), platform, "", 100)
			if e != nil {
				return e
			}
			var records []map[string]any
			if e = json.Unmarshal(raw, &records); e != nil {
				return e
			}
			for _, record := range records {
				record["platform"] = platform
				combined = append(combined, record)
			}
		}
		sort.SliceStable(combined, func(i, j int) bool {
			a, _ := combined[i]["created_at"].(string)
			b, _ := combined[j]["created_at"].(string)
			return a > b
		})
		if len(combined) > 100 {
			combined = combined[:100]
		}
		operations, err = json.Marshal(combined)
		if err != nil {
			return err
		}
	}
	// Only deliberately published names are disclosed, never prompt bodies or native identity.
	if slices.Contains(grants, "groups:ai") {
		err = s.DB.Platform.QueryRow(r.Context(), `SELECT coalesce(jsonb_agg(to_jsonb(x)),'[]') FROM (
        SELECT p.account_id id,p.nickname name,a.platform,p.version FROM personas p JOIN accounts a ON a.id=p.account_id
        WHERE p.published AND a.purpose='chat' AND EXISTS(SELECT 1 FROM subscriptions sub JOIN plans plan ON plan.id=sub.plan_id
            WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>now()
            AND a.platform=ANY(plan.allowed_platforms) AND 'ai'=ANY(plan.modules))
        ORDER BY p.account_id LIMIT 10) x`, p.TenantID).Scan(&personas)
		if err != nil {
			return err
		}
	}
	var wallet []byte
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT coalesce((SELECT jsonb_build_object('granted_micro',w.granted_micro,
        'spent_micro',w.spent_micro,'held_micro',w.held_micro,'available_micro',w.granted_micro-w.spent_micro-w.held_micro)
        FROM ai_wallets w JOIN subscription_periods sp ON sp.id=w.period_id JOIN tenants t ON t.id=w.tenant_id
        WHERE w.tenant_id=$1 AND t.owner_id=$2 AND $3 AND sp.state='active' AND sp.ends_at>clock_timestamp()),'null')`,
		p.TenantID, p.ID, slices.Contains(grants, "billing:read")).Scan(&wallet)
	if err != nil {
		return err
	}
	cookie, err := r.Cookie("wsl_session")
	if err != nil {
		return fail(401, "authentication_required")
	}
	var current bool
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM sessions se JOIN users u ON u.id=se.user_id
		WHERE se.token_hash=$1 AND se.expires_at>clock_timestamp() AND u.id=$2 AND u.tenant_id=$3
		AND NOT u.disabled AND u.role='user')`, security.Digest(cookie.Value), p.ID, p.TenantID).Scan(&current)
	if err != nil {
		return err
	}
	if !current {
		return fail(401, "authentication_required")
	}
	grants, err = s.permissions(r.Context(), p)
	if err != nil {
		return err
	}
	if !slices.Contains(grants, "groups:read") {
		groups = []byte("[]")
	} else {
		// Recheck visibility after gathering the multi-source response.
		err = s.DB.Platform.QueryRow(r.Context(), `SELECT coalesce(jsonb_agg(x.row),'[]')
			FROM jsonb_array_elements($1::jsonb) x(row)
			JOIN group_bindings b ON b.group_id=(x.row->>'group_id')::uuid
				AND b.primary_account_id=(x.row->>'primary_account_id')::uuid
			WHERE b.active AND b.tenant_id=$2 AND b.version=(x.row->>'version')::bigint
			AND member_group_visible(b.tenant_id,b.group_id,$3::uuid)`, groups, p.TenantID, p.ID).Scan(&groups)
		if err != nil {
			return err
		}
	}
	if !slices.Contains(grants, "operations:read") {
		operations = []byte("[]")
	}
	if !slices.Contains(grants, "claims:read") {
		claims = []byte("[]")
	}
	if !slices.Contains(grants, "groups:ai") {
		personas = []byte("[]")
	}
	if !slices.Contains(grants, "billing:read") {
		wallet = []byte("null")
	}
	fields, err := s.centerWorkspaceFields(r, p)
	if err != nil {
		return err
	}
	send(w, 200, mergeCenterFields(map[string]any{"subscription": json.RawMessage(subscription), "groups": json.RawMessage(groups),
		"entitlement": entitlementView(entitlement), "group_count": groupData["total"], "active_group_count": groupData["total"],
		"personas": json.RawMessage(personas), "permissions": grants, "native_ready": false, "ai_ready": false,
		"claims": json.RawMessage(claims), "operations": json.RawMessage(operations),
		"token_usage": nil, "token_usage_state": "legacy_not_collected", "credit_wallet": json.RawMessage(wallet)}, fields))
	return nil
}

func (s *Server) saveChatPreferences(w http.ResponseWriter, r *http.Request, p Principal) error {
	var in struct {
		Version    int64  `json:"version"`
		Activity   int    `json:"activity"`
		Start      int    `json:"start_minute"`
		End        int    `json:"end_minute"`
		Monthly    int64  `json:"monthly_token_limit"`
		Persona    string `json:"persona_id"`
		Preference int64  `json:"preference_version"`
	}
	if err := decode(w, r, &in); err != nil {
		return err
	}
	if in.Version < 1 || in.Preference < 0 || in.Activity < 0 || in.Activity > 100 || in.Start < 0 ||
		in.Start > 1439 || in.End < 0 || in.End > 1439 || in.Monthly < 0 || in.Monthly > 1e12 ||
		(in.Persona != "" && !validUUID(in.Persona)) {
		return fail(400, "invalid_chat_preferences")
	}
	tx, err := s.DB.Platform.Begin(r.Context())
	if err != nil {
		return err
	}
	defer tx.Rollback(r.Context())
	entitlement, err := control.ResolveHumanEntitlement(r.Context(), tx, principalActor(p), tenantPermissions, requestWorkspace(r), r.PathValue("id"))
	if err != nil {
		return err
	}
	if err = entitlement.RequireModule("ai"); err != nil {
		return err
	}
	tenant := entitlement.TenantID
	grants, err := control.LiveActorPermissions(r.Context(), tx, control.Actor{ID: p.ID, TenantID: p.TenantID, Role: p.Role}, tenantPermissions)
	if err != nil {
		return err
	}
	if p.Role != "superadmin" && !slices.Contains(grants, "groups:ai") {
		return fail(403, "permission_denied")
	}
	var version int64
	var nativePlatform string
	if err = tx.QueryRow(r.Context(), `SELECT b.version,g.platform FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
        WHERE b.group_id=$1 AND b.active FOR UPDATE OF b`, r.PathValue("id")).Scan(&version, &nativePlatform); err != nil {
		return err
	}
	if version != in.Version {
		return fail(409, "version_conflict")
	}
	var limit int64
	if !entitlement.OperatorDemo {
		if err = tx.QueryRow(r.Context(), `SELECT p.monthly_ai_tokens FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
        WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>now() AND $2=ANY(p.allowed_platforms)`,
			tenant, nativePlatform).Scan(&limit); err == pgx.ErrNoRows {
			return fail(403, "platform_not_in_plan")
		} else if err != nil {
			return err
		}
		if in.Monthly > limit {
			return fail(400, "chat_budget_exceeds_plan")
		}
	}
	if in.Persona != "" {
		var available bool
		if err = tx.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM personas p JOIN accounts a ON a.id=p.account_id
            WHERE p.account_id=$1 AND p.published AND a.purpose='chat' AND a.platform=$2)`,
			in.Persona, nativePlatform).Scan(&available); err != nil {
			return err
		}
		if !available {
			return fail(409, "chat_persona_unavailable")
		}
	}
	err = tx.QueryRow(r.Context(), `INSERT INTO group_chat_preferences(group_id,tenant_id,persona_id,activity,start_minute,end_minute,monthly_token_limit)
        SELECT $1,$2,nullif($3,'')::uuid,$4,$5,$6,$7 WHERE $8::bigint=0
        ON CONFLICT(group_id) DO NOTHING RETURNING version`,
		r.PathValue("id"), tenant, in.Persona, in.Activity, in.Start, in.End, in.Monthly, in.Preference).Scan(&version)
	if err == pgx.ErrNoRows && in.Preference > 0 {
		err = tx.QueryRow(r.Context(), `UPDATE group_chat_preferences SET persona_id=nullif($3,'')::uuid,
            activity=$4,start_minute=$5,end_minute=$6,monthly_token_limit=$7,version=version+1,updated_at=now()
            WHERE group_id=$1 AND tenant_id=$2 AND version=$8 RETURNING version`,
			r.PathValue("id"), tenant, in.Persona, in.Activity, in.Start, in.End, in.Monthly, in.Preference).Scan(&version)
	}
	if err == pgx.ErrNoRows {
		return fail(409, "version_conflict")
	} else if err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE group_bindings SET version=version+1,epoch=epoch+1 WHERE group_id=$1`,
		r.PathValue("id")); err != nil {
		return err
	}
	if _, err = tx.Exec(r.Context(), `UPDATE operations SET status='cancelled',result_code='chat_preferences_changed',updated_at=now()
        WHERE group_id=$1 AND status='queued'`, r.PathValue("id")); err != nil {
		return err
	}
	if err = auditTx(r.Context(), tx, p, tenant, "group_chat_preferences_saved", r.PathValue("id"),
		map[string]any{"preference_version": version, "applied": false}); err != nil {
		return err
	}
	if err = tx.Commit(r.Context()); err != nil {
		return err
	}
	send(w, 200, map[string]any{"preference_version": version, "applied": false})
	return nil
}
