package httpapi

import (
	"encoding/json"
	"net/http"

	"wangshangliao/internal/control"
)

// centerWorkspaceFields keeps one response contract for paid and system plans.
// The system plan is a projection, not a commercial order or subscription.
func (s *Server) centerWorkspaceFields(r *http.Request, p Principal) (map[string]any, error) {
	space, err := control.ActorWorkspace(r.Context(), s.DB.Platform, principalActor(p))
	if err != nil {
		return nil, err
	}
	plan := map[string]any{
		"id": "", "name": "未开通套餐", "active": false, "system_only": false,
		"unlimited": false, "permanent": false, "platforms": []string{}, "modules": []string{},
		"group_limit": 0, "member_limit": 0, "assistant_limit": 0, "credit_limit": 0,
	}
	if space.Kind == "operator_demo" {
		entitlement, e := control.ResolveHumanEntitlement(r.Context(), s.DB.Platform, principalActor(p), tenantPermissions, "wsl", "")
		if e != nil {
			return nil, e
		}
		plan = entitlementView(entitlement)
		plan["id"], plan["name"], plan["system_only"] = "operator_unlimited", "超管专属 · 不限额", true
		plan["saleable"], plan["assignable"], plan["price"] = false, false, nil
	} else {
		var raw []byte
		err = s.DB.Platform.QueryRow(r.Context(), `SELECT coalesce((SELECT jsonb_build_object(
			'id',p.id,'name',p.name,'active',sub.enabled AND sub.expires_at>clock_timestamp(),
			'system_only',false,'unlimited',false,'permanent',false,'platforms',p.allowed_platforms,'modules',p.modules,
			'group_limit',p.group_limit,'member_limit',p.member_limit,'assistant_limit',p.monthly_assistant_tokens,
			'credit_limit',CASE WHEN EXISTS(SELECT 1 FROM commercial_plans c WHERE c.plan_id=p.id)
				THEN (SELECT cp.credits_micro FROM commercial_plans cp WHERE cp.plan_id=p.id LIMIT 1) ELSE NULL END,
			'expires_at',sub.expires_at) FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
			WHERE sub.tenant_id=$1),'null'::jsonb)`, space.ID).Scan(&raw)
		if err != nil {
			return nil, err
		}
		if string(raw) != "null" {
			if err = json.Unmarshal(raw, &plan); err != nil {
				return nil, err
			}
		}
	}
	quotas := map[string]any{"unlimited": plan["unlimited"], "groups": plan["group_limit"],
		"members": plan["member_limit"], "assistant": plan["assistant_limit"], "credits": plan["credit_limit"]}
	billing, background := "tenant_wallet", "tenant_wallet"
	if space.Kind == "operator_demo" {
		billing, background = "platform_demo", "platform_workspace"
	}
	return map[string]any{"workspace": space, "effective_plan": plan, "quotas": quotas,
		"billing_source": billing, "background_billing_source": background}, nil
}

func mergeCenterFields(out, fields map[string]any) map[string]any {
	for key, value := range fields {
		out[key] = value
	}
	return out
}
