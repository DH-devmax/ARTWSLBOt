package control

import (
	"context"
	"encoding/json"
	"slices"
	"strings"

	"wangshangliao/internal/features"
)

// SafeBusinessOperations runs on the platform pool with explicit live website
// scope. Native tables remain inaccessible to the tenant database role.
func SafeBusinessOperations(ctx context.Context, q Queryer, actor Actor, platform, group string, limit int) (json.RawMessage, error) {
	if platform != "wsl" && platform != "lajiaoliao" || limit < 1 || limit > 200 || group != "" && !ValidID(group) ||
		!ValidID(actor.ID) || actor.Role != "superadmin" && (actor.Role != "user" || !ValidID(actor.TenantID)) {
		return nil, Denied(400, "invalid_operation_scope")
	}
	grants, err := EffectivePermissions(ctx, q, actor, BusinessPermissionCatalog())
	if err != nil {
		return nil, err
	}
	if !slices.Contains(grants, "operations:read") {
		return nil, Denied(403, "permission_denied")
	}
	if group != "" {
		if err = RequireGroup(ctx, q, actor, group); err != nil {
			return nil, err
		}
	}
	kinds := []string{"recall", "mute", "unmute"}
	for _, d := range features.AssistantActions(platform) {
		kinds = append(kinds, d.ID)
	}
	var out json.RawMessage
	visibleControlled := strings.ReplaceAll(controlledBindingVisibleSQL, "$4", "$3")
	err = q.QueryRow(ctx, `WITH visible AS (
		SELECT b.group_id,b.tenant_id,b.primary_account_id,b.ai_account_id,t.space_kind FROM group_bindings b
		JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
		JOIN tenants t ON t.id=b.tenant_id
		LEFT JOIN group_binding_ownership o ON o.group_id=b.group_id AND o.tenant_id=b.tenant_id AND o.platform=g.platform
		WHERE b.active AND g.platform=$1 AND a.platform=g.platform AND a.workspace=g.platform AND a.purpose='management'
		AND `+visibleControlled+`
		AND (t.space_kind='tenant_business' OR workspace_is_demo(t.id,$3::uuid))
		AND ($5 OR b.tenant_id=nullif($2,'')::uuid AND member_group_visible(b.tenant_id,b.group_id,$3::uuid))
		AND ($4='' OR b.group_id=nullif($4,'')::uuid)
		AND EXISTS(SELECT 1 FROM users u WHERE u.id=$3 AND NOT u.disabled AND
			($5 AND u.role='superadmin' OR NOT $5 AND u.role='user' AND u.tenant_id=nullif($2,'')::uuid))
	), legacy AS (
		SELECT o.id,'business'::text source,'tenant_business'::text execution_mode,o.kind,o.status,o.result_code,o.group_id,o.account_id,o.created_at,o.updated_at
		FROM operations o JOIN visible v ON v.group_id=o.group_id AND v.tenant_id=o.tenant_id AND v.primary_account_id=o.account_id
		WHERE o.test_window_id IS NULL AND o.conversation_key='' AND o.kind=ANY($7)
		ORDER BY o.created_at DESC,o.id DESC LIMIT $6
	), native AS (
		SELECT o.id,'assistant'::text source,ap.execution_mode,o.kind,o.status,o.result_code,o.group_id,o.account_id,o.created_at,o.updated_at
		FROM native_setting_operations o JOIN assistant_action_plans ap
			ON ap.id::text=o.idempotency_key AND (ap.operation_id IS NULL OR ap.operation_id=o.id) AND ap.actor_id=o.actor_id
		JOIN visible v ON v.group_id=o.group_id AND v.tenant_id=ap.tenant_id AND v.primary_account_id=o.account_id
		WHERE o.actor_id=$3 AND o.test_window_id IS NULL AND ap.domain=$1
			AND ap.group_id=o.group_id AND ap.account_id=o.account_id AND ap.action=o.kind AND o.kind=ANY($7)
		ORDER BY o.created_at DESC,o.id DESC LIMIT $6
	), configurations AS (
		SELECT e.id,'assistant_configuration'::text source,p.execution_mode,'configuration'::text kind,'committed'::text status,
			'configuration_saved_not_verified'::text result_code,e.group_id,p.account_id,e.created_at,e.created_at updated_at
		FROM assistant_plan_executions e JOIN assistant_plans p ON p.id=e.plan_id AND p.actor_id=e.actor_id
		JOIN visible v ON v.group_id=e.group_id AND v.tenant_id=e.tenant_id AND v.primary_account_id=p.account_id
		WHERE e.actor_id=$3 AND e.domain=$1 AND p.domain=e.domain AND p.tenant_id=e.tenant_id AND p.group_id=e.group_id
		ORDER BY e.created_at DESC,e.id DESC LIMIT $6
	), automatic AS (
		SELECT j.id,'automatic'::text source,
			CASE WHEN v.space_kind='operator_demo' THEN 'workspace_demo' ELSE 'tenant_business' END execution_mode,
			j.action kind,j.status,j.result_code,j.group_id,j.account_id,j.created_at,j.updated_at
		FROM business_rule_jobs j JOIN visible v
			ON v.group_id=j.group_id AND v.tenant_id=j.tenant_id AND v.primary_account_id=j.account_id
		WHERE j.platform=$1 ORDER BY j.created_at DESC,j.id DESC LIMIT $6
	), heat AS (
		SELECT j.id,'heatbot'::text source,
			CASE WHEN v.space_kind='operator_demo' THEN 'workspace_demo' ELSE 'tenant_business' END execution_mode,'send_text'::text kind,
			CASE WHEN j.status='sent' THEN 'succeeded' ELSE j.status END status,
			j.result_code,j.group_id,j.account_id,j.created_at,j.updated_at
		FROM heat_chat_jobs j JOIN visible v
			ON v.group_id=j.group_id AND v.tenant_id=j.tenant_id AND v.ai_account_id=j.account_id
		JOIN accounts a ON a.id=j.account_id AND a.platform=$1 AND a.purpose='chat'
		WHERE j.platform=$1 ORDER BY j.created_at DESC,j.id DESC LIMIT $6
	), onboarding AS (
		SELECT ca.id,'onboarding'::text source,
			CASE WHEN v.space_kind='operator_demo' THEN 'workspace_demo' ELSE 'tenant_business' END execution_mode,'group_accept'::text kind,ca.status,ca.result_code,
			ca.group_id,ca.account_id,ca.created_at,ca.updated_at
		FROM customer_group_accept_attempts ca JOIN visible v
			ON v.group_id=ca.group_id AND v.tenant_id=ca.tenant_id AND v.primary_account_id=ca.account_id
		WHERE ca.platform=$1 AND ($5 OR EXISTS(SELECT 1 FROM tenants t JOIN users u ON u.id=t.owner_id
			WHERE t.id=v.tenant_id AND u.id=$3 AND u.tenant_role='owner' AND NOT u.disabled))
		ORDER BY ca.created_at DESC,ca.id DESC LIMIT $6
	), manual AS (
		SELECT o.id,'manual'::text source,ap.execution_mode,o.kind,o.status,o.result_code,o.group_id,o.account_id,o.created_at,o.updated_at
		FROM native_setting_operations o JOIN manual_action_plans ap
			ON ap.id::text=o.idempotency_key AND (ap.operation_id IS NULL OR ap.operation_id=o.id) AND ap.actor_id=o.actor_id
		JOIN visible v ON v.group_id=o.group_id AND v.tenant_id=ap.tenant_id AND v.primary_account_id=o.account_id
		WHERE o.actor_id=$3 AND o.test_window_id IS NULL AND ap.domain=$1
			AND ap.group_id=o.group_id AND ap.account_id=o.account_id AND ap.action=o.kind AND o.kind=ANY($7)
		ORDER BY o.created_at DESC,o.id DESC LIMIT $6
	), controlled AS (
		SELECT o.id,'controlled_test'::text source,'legacy'::text execution_mode,o.kind,o.status,o.result_code,
			o.group_id,o.account_id,o.created_at,o.updated_at FROM native_setting_operations o
		JOIN visible v ON v.group_id=o.group_id AND v.primary_account_id=o.account_id
		WHERE $5 AND o.actor_id=$3 AND o.test_window_id IS NOT NULL AND o.kind=ANY($7)
		ORDER BY o.created_at DESC,o.id DESC LIMIT $6
	), combined AS (SELECT * FROM legacy UNION ALL SELECT * FROM native UNION ALL SELECT * FROM configurations
		UNION ALL SELECT * FROM automatic UNION ALL SELECT * FROM heat UNION ALL SELECT * FROM onboarding
		UNION ALL SELECT * FROM manual UNION ALL SELECT * FROM controlled)
	SELECT coalesce(jsonb_agg(to_jsonb(x) ORDER BY created_at DESC,id DESC),'[]') FROM (
		SELECT id,source,execution_mode,execution_mode='operator_demo' operator_demo,kind,status,CASE WHEN result_code=ANY($8) THEN result_code
			WHEN status='unknown' THEN 'operation_result_unknown'
			WHEN status='succeeded' THEN 'operation_succeeded'
			WHEN status='failed' THEN 'operation_failed' ELSE 'operation_state_recorded' END result_code,
			group_id,account_id,created_at,updated_at
		FROM combined ORDER BY created_at DESC,id DESC LIMIT $6
	) x`, platform, actor.TenantID, actor.ID, group, actor.Role == "superadmin", limit, kinds,
		[]string{"configuration_saved_not_verified", "desired_state_observed", "permission_denied",
			"operation_result_unknown", "native_setting_result_unknown", "native_transport_unknown",
			"native_moderation_target_protected", "native_moderation_role_required", "native_text_connection_changed",
			"member_access_changed", "group_features_changed", "user_cancelled", "subscription_period_changed",
			"business_admission_closed", "tenant_capability_denied", "group_runtime_disabled"}).Scan(&out)
	if err != nil {
		return nil, err
	}
	// A concurrent revocation cannot turn an already started query into delivery.
	current, err := EffectivePermissions(ctx, q, actor, BusinessPermissionCatalog())
	if err != nil {
		return nil, err
	}
	if !slices.Contains(current, "operations:read") {
		return nil, Denied(403, "permission_denied")
	}
	if group != "" {
		if err = RequireGroup(ctx, q, actor, group); err != nil {
			return nil, err
		}
	}
	// Recheck each projected scope after collecting mixed-group results. Account
	// replacement and member revocation must also close in-flight list responses.
	var currentOut json.RawMessage
	err = q.QueryRow(ctx, `SELECT coalesce(jsonb_agg(x.row ORDER BY x.row->>'created_at' DESC,x.row->>'id' DESC),'[]')
		FROM jsonb_array_elements($1::jsonb) x(row)
		JOIN group_bindings b ON b.group_id=(x.row->>'group_id')::uuid
			AND CASE WHEN x.row->>'source'='heatbot' THEN b.ai_account_id ELSE b.primary_account_id END=(x.row->>'account_id')::uuid
		JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id
		JOIN tenants t ON t.id=b.tenant_id
		LEFT JOIN group_binding_ownership o ON o.group_id=b.group_id AND o.tenant_id=b.tenant_id AND o.platform=g.platform
		WHERE b.active AND g.platform=$2 AND a.platform=g.platform AND a.workspace=g.platform AND a.purpose='management'
		AND `+controlledBindingVisibleSQL+`
		AND (t.space_kind='tenant_business' OR workspace_is_demo(t.id,$4::uuid))
		AND ($5 OR b.tenant_id=nullif($3,'')::uuid AND member_group_visible(b.tenant_id,b.group_id,$4::uuid))
		AND (x.row->>'source'<>'onboarding' OR $5 OR EXISTS(SELECT 1 FROM tenants t JOIN users u ON u.id=t.owner_id
			WHERE t.id=b.tenant_id AND u.id=$4 AND u.tenant_role='owner' AND NOT u.disabled))
		AND EXISTS(SELECT 1 FROM users u WHERE u.id=$4 AND NOT u.disabled AND
			($5 AND u.role='superadmin' OR NOT $5 AND u.role='user' AND u.tenant_id=nullif($3,'')::uuid))`,
		out, platform, actor.TenantID, actor.ID, actor.Role == "superadmin").Scan(&currentOut)
	return currentOut, err
}
