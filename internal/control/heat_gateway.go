package control

import (
	"bytes"
	"context"
	"encoding/hex"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/security"
)

func (s *GatewayService) PruneGrants(ctx context.Context) error {
	_, err := s.DB.Platform.Exec(ctx, `DELETE FROM heat_gateway_grants WHERE token_hash IN (
        SELECT token_hash FROM heat_gateway_grants WHERE expires_at<=clock_timestamp()
        AND coalesce(generation_deadline,expires_at)<=clock_timestamp()
        ORDER BY expires_at LIMIT 512)`)
	return err
}

// MintHeatGrant is a control-plane primitive, never a model tool or public API.
// Production generation and native sending retain their independent gates.
func (s *GatewayService) MintHeatGrant(ctx context.Context, a Actor, group string) (string, error) {
	if a.Role != "superadmin" || !ValidID(group) {
		return "", Denied(403, "superadmin_required")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	var tenant, owner, account string
	var binding, epoch, accountEpoch, persona, route int64
	err = tx.QueryRow(ctx, `SELECT b.tenant_id::text,t.owner_id::text,a.id::text,b.version,b.epoch,a.epoch,pr.version,r.version
        FROM group_bindings b JOIN tenants t ON t.id=b.tenant_id JOIN accounts a ON a.id=b.ai_account_id
        JOIN personas pr ON pr.account_id=a.id
        JOIN native_groups g ON g.id=b.group_id JOIN subscriptions sub ON sub.tenant_id=b.tenant_id
        JOIN plans p ON p.id=sub.plan_id JOIN ai_model_routes r ON r.alias='default'
        WHERE b.group_id=$1 AND b.active AND b.ai_enabled AND b.ai_requested
        AND a.purpose='chat' AND a.platform=g.platform AND a.connection_enabled AND a.auth_state='authenticated'
        AND sub.enabled AND sub.expires_at>clock_timestamp() AND 'ai'=ANY(p.modules)
        AND g.platform=ANY(p.allowed_platforms) AND r.enabled
        AND EXISTS(SELECT 1 FROM account_groups ag WHERE ag.group_id=b.group_id AND ag.account_id=a.id
            AND ag.role IN ('member','admin','owner') AND ag.observation_source='native'
            AND ag.observed_at>clock_timestamp()-interval '60 seconds') FOR SHARE OF b,a,sub,r`, group).
		Scan(&tenant, &owner, &account, &binding, &epoch, &accountEpoch, &persona, &route)
	if err == pgx.ErrNoRows {
		return "", Denied(409, "heat_gateway_scope_unverified")
	}
	if err != nil {
		return "", err
	}
	service, err := WorkspaceServiceEntitlement(ctx, tx, tenant)
	if err != nil {
		return "", err
	}
	if service.Demo || service.OwnerID != owner {
		return "", Denied(409, "heat_gateway_durable_task_required")
	}
	token, err := security.Token()
	if err != nil {
		return "", err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO heat_gateway_grants(token_hash,tenant_id,actor_id,group_id,account_id,
        binding_version,binding_epoch,account_epoch,persona_version,model_alias,route_version,expires_at)
        VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,'default',$10,clock_timestamp()+interval '30 seconds')`,
		security.Digest(token), tenant, owner, group, account, binding, epoch, accountEpoch, persona, route); err != nil {
		return "", err
	}
	if err = tx.Commit(ctx); err != nil {
		return "", err
	}
	return token, nil
}

type HeatGatewayInput struct {
	ModelAlias   string              `json:"model_alias"`
	ModelVersion int64               `json:"model_version"`
	Messages     []assistant.Message `json:"messages"`
}

func (s *GatewayService) HeatGenerate(ctx context.Context, token string, input HeatGatewayInput) (assistant.Response, error) {
	var out assistant.Response
	if len(token) != 43 || input.ModelAlias != "default" || len(input.Messages) < 1 || len(input.Messages) > 100 {
		return out, Denied(400, "invalid_heat_gateway_request")
	}
	for _, m := range input.Messages {
		if len(m.ToolCalls) > 0 || m.ToolCallID != "" || m.Role != "system" && m.Role != "assistant" && m.Role != "user" {
			return out, Denied(400, "heat_gateway_text_only")
		}
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	var tenant, owner, group string
	var route int64
	var deadline time.Time
	var job string
	err = tx.QueryRow(ctx, `UPDATE heat_gateway_grants SET used_at=clock_timestamp()
        WHERE token_hash=$1 AND used_at IS NULL AND expires_at>clock_timestamp()
        RETURNING tenant_id::text,actor_id::text,group_id::text,route_version,
        coalesce(generation_deadline,expires_at),coalesce(chat_job_id::text,'')`,
		security.Digest(token)).Scan(&tenant, &owner, &group, &route, &deadline, &job)
	if err == pgx.ErrNoRows {
		return out, Denied(403, "heat_gateway_grant_invalid")
	}
	if err != nil {
		return out, err
	}
	if route != input.ModelVersion {
		return out, Denied(409, "model_route_changed")
	}
	service, err := WorkspaceServiceEntitlement(ctx, tx, tenant)
	if err != nil {
		return out, err
	}
	if service.OwnerID != owner || service.Demo && !ValidID(job) {
		return out, Denied(403, "heat_gateway_scope_changed")
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	ctx, stop := context.WithDeadline(ctx, deadline)
	defer stop()
	key := "heat:" + hex.EncodeToString(security.Digest(token))
	if job != "" {
		key = "heat-job:" + job
	}
	request := gatewayRequest{Service: &service, ServiceTask: job, HeatGrant: security.Digest(token),
		Group: group, Purpose: "heatbot", Key: key, ExpectedRoute: route}
	check := func(work context.Context) error { return s.heatServiceAdmission(work, s.DB.Platform, request) }
	request.Check = check
	out, err = s.generate(ctx, request, input.Messages, nil, func(string) error { return check(ctx) })
	if err == nil && len(out.Message.ToolCalls) > 0 {
		return assistant.Response{}, Denied(502, "heat_gateway_text_only")
	}
	return out, err
}

func (s *GatewayService) heatServiceAdmission(ctx context.Context, q Queryer, request gatewayRequest) error {
	if _, _, err := gatewayAccounting(request); err != nil {
		return err
	}
	service := request.Service
	if service == nil {
		return Denied(403, "heat_gateway_scope_changed")
	}
	if err := service.requireGroup(ctx, q, request.Group); err != nil {
		return err
	}
	if err := service.requirePermission(ctx, q, s.Catalog, "groups:ai"); err != nil {
		return err
	}
	var account, group string
	if request.ServiceTask == "" {
		// Preserve existing customer-only standalone grants and their commercial
		// fences. A demo workspace always needs a durable control task.
		err := q.QueryRow(ctx, `SELECT h.account_id::text,h.group_id::text
			FROM heat_gateway_grants h JOIN group_bindings b ON b.group_id=h.group_id
			JOIN accounts a ON a.id=h.account_id JOIN users u ON u.id=h.actor_id
			JOIN tenants t ON t.id=h.tenant_id AND t.owner_id=u.id JOIN personas pr ON pr.account_id=a.id
			JOIN native_groups g ON g.id=h.group_id JOIN ai_model_routes r ON r.alias=h.model_alias
			JOIN subscriptions sub ON sub.tenant_id=h.tenant_id JOIN plans p ON p.id=sub.plan_id
			WHERE h.token_hash=$1 AND h.used_at IS NOT NULL AND h.expires_at>clock_timestamp()
			AND h.chat_job_id IS NULL AND h.tenant_id=$2 AND h.actor_id=$3 AND h.group_id=$4
			AND b.tenant_id=h.tenant_id AND b.active AND b.ai_enabled AND b.ai_requested AND b.ai_account_id=h.account_id
			AND b.version=h.binding_version AND b.epoch=h.binding_epoch AND a.epoch=h.account_epoch
			AND pr.version=h.persona_version AND r.version=h.route_version AND r.version=$5 AND r.enabled
			AND g.platform=a.platform AND a.purpose='chat' AND g.platform=ANY(p.allowed_platforms)
			AND t.space_kind='tenant_business' AND u.role='user' AND u.tenant_id=h.tenant_id AND u.tenant_role='owner'
			AND a.auth_state='authenticated' AND a.connection_enabled AND NOT u.disabled
			AND sub.enabled AND sub.expires_at>clock_timestamp() AND 'ai'=ANY(p.modules)
			FOR SHARE OF h,b,a,u,t,pr,r,sub,p`,
			request.HeatGrant, service.TenantID, service.OwnerID, request.Group, request.ExpectedRoute).Scan(&account, &group)
		if err == pgx.ErrNoRows {
			return Denied(403, "heat_gateway_scope_changed")
		}
		if err != nil {
			return err
		}
	} else {
		scope, err := (&HeatChatService{DB: s.DB, Vault: s.Vault}).heatScope(ctx, q, request.Group, true)
		if err != nil {
			return err
		}
		if scope.Service != *service || scope.RouteVersion != request.ExpectedRoute || !scope.Effective.allows(time.Now()) {
			return Denied(409, "heat_chat_scope_changed")
		}
		var digest []byte
		err = q.QueryRow(ctx, `SELECT j.scope_digest,h.account_id::text,h.group_id::text
			FROM heat_chat_jobs j JOIN heat_gateway_grants h ON h.chat_job_id=j.id
			JOIN group_bindings b ON b.group_id=j.group_id AND b.tenant_id=j.tenant_id
			JOIN accounts a ON a.id=j.account_id
			JOIN group_business_runtime br ON br.group_id=b.group_id AND br.tenant_id=b.tenant_id
			JOIN heat_chat_admission ha ON ha.platform=j.platform
			JOIN ai_model_routes r ON r.alias=h.model_alias
			WHERE j.id=$1 AND j.tenant_id=$2 AND j.actor_id=$3 AND j.group_id=$4
			AND j.status='generating' AND j.expires_at>clock_timestamp()
			AND h.token_hash=$5 AND h.used_at IS NOT NULL AND h.generation_deadline>clock_timestamp()
			AND h.tenant_id=j.tenant_id AND h.actor_id=j.actor_id AND h.group_id=j.group_id AND h.account_id=j.account_id
			AND b.active AND b.ai_requested AND b.ai_enabled AND b.ai_account_id=a.id
			AND b.version=h.binding_version AND b.epoch=h.binding_epoch AND a.epoch=h.account_epoch
			AND b.version=$6 AND b.epoch=$7 AND a.epoch=$8 AND a.version=$9 AND a.id=$10
			AND br.enabled AND br.version=$11 AND ha.enabled AND ha.version=$12
			AND h.persona_version=$13 AND r.enabled AND r.version=h.route_version AND r.version=$14
			FOR SHARE OF j,h,b,a,br,ha,r`,
			request.ServiceTask, service.TenantID, service.OwnerID, request.Group, request.HeatGrant,
			scope.BindingVersion, scope.BindingEpoch, scope.AccountEpoch, scope.AccountVersion, scope.Account,
			scope.RuntimeVersion, scope.AdmissionVersion, scope.PersonaVersion, scope.RouteVersion).Scan(&digest, &account, &group)
		if err == pgx.ErrNoRows {
			return Denied(409, "heat_chat_job_changed")
		}
		if err != nil {
			return err
		}
		if !bytes.Equal(digest, scope.digest()) {
			return Denied(409, "heat_chat_scope_changed")
		}
	}
	var role string
	err := q.QueryRow(ctx, `SELECT role FROM account_groups WHERE account_id=$1 AND group_id=$2
		AND role IN('member','admin','owner') AND observation_source='native'
		AND observed_at<=clock_timestamp() AND observed_at>clock_timestamp()-interval '60 seconds'
		FOR SHARE`, account, group).Scan(&role)
	if err == pgx.ErrNoRows {
		return Denied(403, "heat_gateway_scope_changed")
	}
	return err
}
