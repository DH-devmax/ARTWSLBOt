package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/database"
	"wangshangliao/internal/features"
	"wangshangliao/internal/security"
)

// ManualActionService owns human page intents. No assistant session, model call
// or controlled-test grant participates in this path.
type ManualActionService struct {
	DB             *database.DB
	Vault          *security.Vault
	Catalog        []PermissionSpec
	NativeMembers  AssistantMemberReader
	NativeMessages AssistantMessageReader
	Executor       AssistantActionExecutor
}

func (s *ManualActionService) Recover(ctx context.Context) error {
	// A restarted process may only reconcile the original durable operation.
	_, err := s.DB.Platform.Exec(ctx, `UPDATE manual_action_plans
		SET status='unknown',result_code='operation_result_unknown',version=version+1
		WHERE status IN('approved','submitting')`)
	return err
}

func actionScopeLabels(ctx context.Context, q Queryer, p *AssistantActionPlan) error {
	return q.QueryRow(ctx, `SELECT t.name,g.name,coalesce(nullif(a.native_nickname,''),a.name)
		FROM group_bindings b JOIN tenants t ON t.id=b.tenant_id JOIN native_groups g ON g.id=b.group_id
		JOIN accounts a ON a.id=b.primary_account_id WHERE b.group_id=$1 AND a.id=$2 AND g.platform=$3`,
		p.Scope.GroupID, p.Scope.AccountID, p.Scope.Platform).Scan(&p.CustomerName, &p.GroupName, &p.RobotNickname)
}

func (s *ManualActionService) scope(ctx context.Context, q Queryer, ref SessionRef, group, action string) (Actor, assistant.Scope, BusinessEntitlement, error) {
	a, err := sessionActor(ctx, q, ref, true)
	scope := assistant.Scope{Platform: ref.assistantPlatform(), Kind: "group", GroupID: group}
	var e BusinessEntitlement
	if err != nil {
		return a, scope, e, err
	}
	e, err = ResolveHumanEntitlement(ctx, q, a, s.Catalog, scope.Platform, group)
	if err != nil {
		return a, scope, e, err
	}
	if !slices.Contains(e.Permissions, "groups:read") || action != "" && !slices.Contains(e.Permissions, "group_action:"+action) {
		return a, scope, e, Denied(403, "permission_denied")
	}
	err = q.QueryRow(ctx, `SELECT a.id::text FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id
		WHERE b.group_id=$1 AND b.active AND a.platform=$2 AND a.workspace=$2 AND a.purpose='management' FOR SHARE OF b,a`,
		group, scope.Platform).Scan(&scope.AccountID)
	if err == pgx.ErrNoRows {
		err = Denied(404, "group_not_found")
	}
	return a, scope, e, err
}

func humanActionHash(ctx context.Context, q Queryer, a Actor, scope assistant.Scope, e BusinessEntitlement) (string, error) {
	var resource []byte
	err := q.QueryRow(ctx, `SELECT jsonb_build_object('tenant',b.tenant_id,'version',b.version,'epoch',b.epoch,
		'account',a.id,'account_epoch',a.epoch,'access_version',u.access_version)
		FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id JOIN users u ON u.id=$3
		WHERE b.group_id=$1 AND a.id=$2 AND b.active FOR SHARE OF b,a,u`,
		scope.GroupID, scope.AccountID, a.ID).Scan(&resource)
	if err != nil {
		return "", err
	}
	fence, err := controlledExecutionScope(ctx, q, a.ID, scope.Platform, scope.GroupID)
	if err != nil {
		return "", err
	}
	parts := []any{a, scope, e, json.RawMessage(resource)}
	if fence.ControlledScopeID != "" {
		parts = append(parts, fence)
	}
	return digest(parts), nil
}

func (s *ManualActionService) Targets(ctx context.Context, ref SessionRef, group, action, search string) ([]AssistantMember, error) {
	a, scope, e, err := s.scope(ctx, s.DB.Platform, ref, group, action)
	if err != nil {
		return nil, err
	}
	if _, ok := features.FindBusinessAction(scope.Platform, action, "manual"); !ok ||
		!slices.Contains([]string{"mute_member", "unmute_member", "kick_member", "member_nickname", "recall_message"}, action) {
		return nil, Denied(400, "assistant_action_invalid")
	}
	search = strings.TrimSpace(search)
	if search == "" || len(search) > 240 {
		return nil, Denied(400, "assistant_invalid_input")
	}
	fingerprint, err := humanActionHash(ctx, s.DB.Platform, a, scope, e)
	if err != nil {
		return nil, err
	}
	var native string
	if err = s.DB.Platform.QueryRow(ctx, `SELECT native_id FROM native_groups WHERE id=$1 AND platform=$2`, group, scope.Platform).Scan(&native); err != nil {
		return nil, err
	}
	items := []AssistantMember{}
	messages := []AssistantMessage{}
	if action == "recall_message" {
		if s.NativeMessages == nil {
			return nil, Denied(503, "assistant_member_lookup_unavailable")
		}
		messages, err = s.NativeMessages(ctx, scope.Platform, scope.AccountID, native, search)
		for _, message := range messages {
			items = append(items, message.Member)
		}
	} else {
		if s.NativeMembers == nil {
			return nil, Denied(503, "assistant_member_lookup_unavailable")
		}
		items, err = s.NativeMembers(context.WithValue(ctx, nativeReadActorKey{}, a), scope.Platform, scope.AccountID, native, search)
	}
	if err != nil {
		return nil, err
	}
	items = items[:min(len(items), 20)]
	for i := range items {
		items[i].Token = ""
		target := assistantTarget{Actor: a.ID, Tenant: a.TenantID, Scope: scope, Member: items[i],
			Fingerprint: fingerprint, ExpiresAt: time.Now().Add(10 * time.Minute)}
		if action == "recall_message" {
			target.Message = &messages[i].Reference
			target.ExpiresAt = time.UnixMilli(target.Message.CreatedAt).Add(2 * time.Minute)
		}
		raw, _ := json.Marshal(target)
		sealed, sealErr := s.Vault.Seal("manual:target:"+action, raw)
		clear(raw)
		if sealErr != nil {
			return nil, sealErr
		}
		items[i].Token = base64.RawURLEncoding.EncodeToString(sealed)
	}
	current, err := s.currentHash(ctx, ref, scope, action)
	if err != nil || current != fingerprint {
		if err == nil {
			err = Denied(403, "assistant_history_scope_changed")
		}
		return nil, err
	}
	return items, nil
}

func (s *ManualActionService) currentHash(ctx context.Context, ref SessionRef, scope assistant.Scope, action string) (string, error) {
	a, current, e, err := s.scope(ctx, s.DB.Platform, ref, scope.GroupID, action)
	if err != nil || current != scope {
		if err == nil {
			err = Denied(409, "assistant_scope_changed")
		}
		return "", err
	}
	return humanActionHash(ctx, s.DB.Platform, a, scope, e)
}

func (s *ManualActionService) Create(ctx context.Context, ref SessionRef, group, action, token, text string, minutes int) (AssistantActionPlan, error) {
	p := AssistantActionPlan{ID: NewID(), Action: action, Text: text, Minutes: minutes, Version: 1, Status: "pending_approval"}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx)
	a, scope, e, err := s.scope(ctx, tx, ref, group, action)
	if err != nil {
		return p, err
	}
	p.Scope, p.ExecutionMode = scope, e.Mode
	d, err := ScopedHumanAction(ctx, tx, a, s.Catalog, scope.Platform, group, action, "manual")
	if err != nil {
		return p, err
	}
	p.Name, p.Production = d.Name, d.Production
	if !d.Production {
		p.Status, p.ResultCode = "blocked", d.BlockReason
	}
	fingerprint, err := humanActionHash(ctx, tx, a, scope, e)
	if err != nil {
		return p, err
	}
	if token != "" {
		if len(token) > 4096 {
			return p, Denied(400, "assistant_target_invalid")
		}
		sealed, decodeErr := base64.RawURLEncoding.DecodeString(token)
		if decodeErr != nil {
			return p, Denied(400, "assistant_target_invalid")
		}
		raw, openErr := s.Vault.Open("manual:target:"+action, sealed)
		if openErr != nil {
			return p, Denied(400, "assistant_target_invalid")
		}
		var target assistantTarget
		err = json.Unmarshal(raw, &target)
		clear(raw)
		if err != nil || target.Actor != a.ID || target.Tenant != a.TenantID || target.Scope != scope ||
			target.Fingerprint != fingerprint || !time.Now().Before(target.ExpiresAt) {
			return p, Denied(409, "assistant_target_expired")
		}
		p.Target, p.message = &target.Member, target.Message
		if (action == "recall_message") != (p.message != nil) {
			return p, Denied(400, "assistant_target_invalid")
		}
	}
	if err = validateAssistantAction(action, text, minutes, p.Target != nil); err != nil {
		return p, err
	}
	var native, tenant string
	err = tx.QueryRow(ctx, `SELECT g.native_id,b.tenant_id::text,b.version,b.epoch,a.epoch FROM group_bindings b
		JOIN native_groups g ON g.id=b.group_id JOIN accounts a ON a.id=b.primary_account_id WHERE b.group_id=$1
		AND b.active AND a.id=$2 FOR SHARE OF b,a`, group, scope.AccountID).
		Scan(&native, &tenant, &p.BaseVersion, &p.BindingEpoch, &p.AccountEpoch)
	if err != nil {
		return p, err
	}
	if err = ValidateNativeChange(scope.Platform, p.settingChange(native)); err != nil {
		return p, Denied(400, "assistant_action_invalid")
	}
	if err = actionScopeLabels(ctx, tx, &p); err != nil {
		return p, err
	}
	p.ExpiresAt = time.Now().UTC().Add(10 * time.Minute)
	p.controlled, err = controlledExecutionScope(ctx, tx, a.ID, scope.Platform, group)
	if err != nil {
		return p, err
	}
	payload := assistantActionPayload{controlledExecutionFence: p.controlled, Plan: p, AccountEpoch: p.AccountEpoch, BindingEpoch: p.BindingEpoch, Message: p.message}
	p.Digest = digest([]any{a.ID, tenant, "manual", payload})
	payload.Plan = p
	raw, _ := json.Marshal(payload)
	sealed, err := s.Vault.Seal("manual:action:"+p.ID, raw)
	clear(raw)
	if err != nil {
		return p, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO manual_action_plans(id,actor_id,tenant_id,domain,account_id,group_id,action,
		execution_mode,digest,authorization_hash,payload_sealed,account_epoch,binding_epoch,base_version,status,result_code,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)`, p.ID, a.ID, tenant, scope.Platform,
		scope.AccountID, group, action, e.Mode, p.Digest, fingerprint, sealed, p.AccountEpoch, p.BindingEpoch,
		p.BaseVersion, p.Status, p.ResultCode, p.ExpiresAt)
	if err != nil {
		return p, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		VALUES($1,$2,'manual_action_proposed',$3,jsonb_build_object('platform',$4::text,'action',$5::text,'execution_mode',$6::text))`,
		tenant, a.ID, p.ID, scope.Platform, action, e.Mode)
	if err != nil {
		return p, err
	}
	return p, tx.Commit(ctx)
}

func (s *ManualActionService) Plan(ctx context.Context, ref SessionRef, group, id string) (AssistantActionPlan, error) {
	var p AssistantActionPlan
	if !ValidID(id) {
		return p, Denied(404, "assistant_plan_not_found")
	}
	a, _, _, err := s.scope(ctx, s.DB.Platform, ref, group, "")
	if err != nil {
		return p, err
	}
	var sealed []byte
	var fingerprint, mode, status, code, operation string
	var version int64
	err = s.DB.Platform.QueryRow(ctx, `SELECT payload_sealed,authorization_hash,execution_mode,status,result_code,
		version,coalesce(operation_id::text,'') FROM manual_action_plans
		WHERE id=$1 AND actor_id=$2 AND group_id=$3 AND domain=$4`, id, a.ID, group, ref.assistantPlatform()).
		Scan(&sealed, &fingerprint, &mode, &status, &code, &version, &operation)
	if err == pgx.ErrNoRows {
		return p, Denied(404, "assistant_plan_not_found")
	}
	if err != nil {
		return p, err
	}
	raw, err := s.Vault.Open("manual:action:"+id, sealed)
	if err != nil {
		return p, err
	}
	var payload assistantActionPayload
	err = json.Unmarshal(raw, &payload)
	clear(raw)
	if err != nil {
		return p, err
	}
	p = payload.Plan
	p.controlled = payload.controlledExecutionFence
	if err = p.controlled.require(ctx, s.DB.Platform, a.ID, p.Scope.Platform, group); err != nil {
		return AssistantActionPlan{}, err
	}
	p.message, p.AccountEpoch, p.BindingEpoch = payload.Message, payload.AccountEpoch, payload.BindingEpoch
	if p.ID != id || p.Scope.GroupID != group || p.Scope.Platform != ref.assistantPlatform() || p.ExecutionMode != mode {
		return AssistantActionPlan{}, Denied(409, "assistant_plan_mismatch")
	}
	current, err := s.currentHash(ctx, ref, p.Scope, p.Action)
	if err != nil || current != fingerprint {
		if err == nil {
			err = Denied(403, "assistant_history_scope_changed")
		}
		return AssistantActionPlan{}, err
	}
	p.Status, p.ResultCode, p.Version, p.OperationID = status, code, version, operation
	if status == "approved" || status == "submitting" || status == "unknown" {
		var recorded SettingOperation
		err = s.DB.Platform.QueryRow(ctx, `SELECT id::text,status,result_code FROM native_setting_operations
			WHERE actor_id=$1 AND account_id=$2 AND group_id=$3 AND idempotency_key=$4 AND kind=$5
			AND ($6='' OR id::text=$6)`, a.ID, p.Scope.AccountID, group, id, p.Action, operation).
			Scan(&recorded.ID, &recorded.Status, &recorded.Code)
		if err != nil && err != pgx.ErrNoRows {
			return AssistantActionPlan{}, err
		}
		if err == nil {
			p.OperationID, p.Status, p.ResultCode = recorded.ID, recorded.Status, recorded.Code
			if p.Status != "failed" && p.Status != "succeeded" {
				p.Status = "unknown"
			}
		}
	}
	if (status == "pending_approval" || status == "blocked") && !time.Now().Before(p.ExpiresAt) {
		p.Status = "expired"
	}
	d, err := ScopedHumanAction(ctx, s.DB.Platform, a, s.Catalog, p.Scope.Platform, group, p.Action, "manual")
	p.Production = d.Production
	if err != nil {
		return AssistantActionPlan{}, err
	}
	current, err = s.currentHash(ctx, ref, p.Scope, p.Action)
	if err != nil || current != fingerprint {
		if err == nil {
			err = Denied(403, "assistant_history_scope_changed")
		}
		return AssistantActionPlan{}, err
	}
	return p, nil
}

func (s *ManualActionService) Cancel(ctx context.Context, ref SessionRef, group, id string) (AssistantActionPlan, error) {
	p, err := s.Plan(ctx, ref, group, id)
	if err != nil {
		return p, err
	}
	_, err = s.DB.Platform.Exec(ctx, `WITH cancelled AS (
		UPDATE manual_action_plans SET status='cancelled',version=version+1
		WHERE id=$1 AND status IN('pending_approval','blocked') RETURNING *)
		INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		SELECT tenant_id,actor_id,'manual_action_cancelled',id::text,
		jsonb_build_object('platform',domain,'action',action,'execution_mode',execution_mode) FROM cancelled`, id)
	if err != nil {
		return p, err
	}
	return s.Plan(ctx, ref, group, id)
}

func (s *ManualActionService) Approve(ctx context.Context, ref SessionRef, group, id, summary string, version int64, key string) (AssistantActionPlan, error) {
	p, err := s.Plan(ctx, ref, group, id)
	if err != nil {
		return p, err
	}
	if summary != p.Digest || len(key) < 8 || len(key) > 100 {
		return p, Denied(409, "assistant_plan_mismatch")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return p, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return p, err
	}
	a, _, e, err := s.scope(ctx, tx, ref, group, p.Action)
	if err != nil {
		return p, err
	}
	if err = p.controlled.require(ctx, tx, a.ID, p.Scope.Platform, group); err != nil {
		return p, err
	}
	var oldKey, status string
	var current int64
	var expires time.Time
	err = tx.QueryRow(ctx, `SELECT coalesce(idempotency_key,''),status,version,expires_at FROM manual_action_plans
		WHERE id=$1 AND actor_id=$2 AND execution_mode=$3 FOR UPDATE`, id, a.ID, e.Mode).
		Scan(&oldKey, &status, &current, &expires)
	if err != nil {
		return p, err
	}
	if oldKey != "" {
		if oldKey != key {
			return p, Denied(409, "idempotency_conflict")
		}
		if err = tx.Commit(ctx); err != nil {
			return p, err
		}
		return s.Plan(ctx, ref, group, id)
	}
	if current != version {
		return p, Denied(409, "version_conflict")
	}
	if !slices.Contains([]string{"pending_approval", "blocked"}, status) || !time.Now().Before(expires) {
		return p, Denied(409, "assistant_plan_not_pending")
	}
	if err = RequireHumanBusinessAdmission(ctx, tx, a, s.Catalog, p.Scope.Platform, group, p.Action, "manual"); err != nil {
		return p, err
	}
	if s.Executor == nil {
		return p, Denied(503, "assistant_action_executor_unavailable")
	}
	fingerprint, err := humanActionHash(ctx, tx, a, p.Scope, e)
	if err != nil {
		return p, err
	}
	tag, err := tx.Exec(ctx, `UPDATE manual_action_plans SET status='approved',version=version+1,idempotency_key=$2
		WHERE id=$1 AND authorization_hash=$3`, id, key, fingerprint)
	if err != nil {
		return p, err
	}
	if tag.RowsAffected() != 1 {
		return p, Denied(409, "assistant_scope_changed")
	}
	if err = tx.Commit(ctx); err != nil {
		return p, Denied(503, "assistant_commit_unknown")
	}
	op, executeErr := s.Executor(ctx, ref, p)
	state, code := op.Status, op.Code
	if op.ID == "" {
		state, code = "failed", assistantCode(executeErr)
	}
	if state != "succeeded" && state != "failed" {
		state = "unknown"
	}
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, err = s.DB.Platform.Exec(save, `WITH finished AS (
		UPDATE manual_action_plans SET status=$2,result_code=$3,operation_id=nullif($4,'')::uuid,version=version+1
		WHERE id=$1 AND status IN('approved','submitting') RETURNING *)
		INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		SELECT tenant_id,actor_id,'manual_action_finished',id::text,jsonb_build_object(
			'action',action,'platform',domain,'status',status,'execution_mode',execution_mode) FROM finished`, id, state, code, op.ID)
	if err != nil {
		return p, Denied(503, "assistant_commit_unknown")
	}
	return s.Plan(ctx, ref, group, id)
}
