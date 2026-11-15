package control

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/features"
	"wangshangliao/internal/nativeauth"
)

type AssistantMember struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Role  string `json:"role"`
	Token string `json:"token"`
}
type AssistantMemberReader func(context.Context, string, string, string, string) ([]AssistantMember, error)
type nativeReadActorKey struct{}

func NativeReadActor(ctx context.Context) (Actor, bool) {
	a, ok := ctx.Value(nativeReadActorKey{}).(Actor)
	return a, ok && a.ID != ""
}

type AssistantMessage struct {
	Member    AssistantMember
	Reference nativeauth.MessageReference
}
type AssistantMessageReader func(context.Context, string, string, string, string) ([]AssistantMessage, error)

type assistantTarget struct {
	Actor         string
	Tenant        string
	Scope         assistant.Scope
	Member        AssistantMember
	Fingerprint   string
	ExpiresAt     time.Time
	RequireChoice bool
	Message       *nativeauth.MessageReference
}

type AssistantActionPlan struct {
	ID            string           `json:"id"`
	SessionID     string           `json:"session_id"`
	Scope         assistant.Scope  `json:"scope"`
	Action        string           `json:"action"`
	Name          string           `json:"name"`
	Target        *AssistantMember `json:"target,omitempty"`
	Text          string           `json:"text,omitempty"`
	Minutes       int              `json:"minutes,omitempty"`
	Digest        string           `json:"digest"`
	Version       int64            `json:"version"`
	BaseVersion   int64            `json:"base_version"`
	AccountEpoch  int64            `json:"-"`
	BindingEpoch  int64            `json:"-"`
	Status        string           `json:"status"`
	ResultCode    string           `json:"result_code"`
	OperationID   string           `json:"operation_id,omitempty"`
	ExpiresAt     time.Time        `json:"expires_at"`
	Production    bool             `json:"production_admitted"`
	ExecutionMode string           `json:"execution_mode"`
	CustomerName  string           `json:"customer_name"`
	GroupName     string           `json:"group_name"`
	RobotNickname string           `json:"robot_nickname"`
	Summary       string           `json:"summary"`
	PolicyVersion int64            `json:"policy_version,omitempty"`
	Module        string           `json:"module,omitempty"`
	CapabilityID  string           `json:"capability_id,omitempty"`
	EvidenceState string           `json:"evidence_state,omitempty"`
	OccurredAt    time.Time        `json:"occurred_at,omitempty"`
	message       *nativeauth.MessageReference
	controlled    controlledExecutionFence
	Observation   *AssistantObservation `json:"observation,omitempty"`
}

type assistantActionPayload struct {
	controlledExecutionFence
	Plan         AssistantActionPlan
	AccountEpoch int64
	BindingEpoch int64
	Message      *nativeauth.MessageReference
}

func (s *AssistantService) actionTool(ctx context.Context, ref SessionRef, session AssistantSession, call assistant.ToolCall) (assistant.ToolResult, error) {
	out := assistant.ToolResult{Type: call.Function.Name, Scope: session.Scope, SampledAt: time.Now().UTC(), Verification: "suggestion_only"}
	if session.Scope.Kind != "group" {
		return out, Denied(403, "assistant_group_scope_required")
	}
	if _, err := s.authorize(ctx, s.DB.Platform, ref, session.Scope, "assistant:use", true, false); err != nil {
		return out, err
	}
	switch call.Function.Name {
	case "get_group_capabilities":
		out.Verification = "capability_snapshot"
		var args struct{}
		if err := strictJSON(call.Function.Arguments, &args); err != nil {
			return out, err
		}
		view, err := s.Context(ctx, ref, session.Scope.GroupID, "", 0, 1)
		out.Data = map[string]any{"actions": view.Actions, "capabilities": view.Capabilities, "policy_version": view.PolicyVersion}
		return out, err
	case "find_group_members", "find_group_messages":
		var args struct {
			Search string `json:"search"`
		}
		if err := strictJSON(call.Function.Arguments, &args); err != nil {
			return out, err
		}
		args.Search = strings.TrimSpace(args.Search)
		if args.Search == "" || len(args.Search) > 240 ||
			call.Function.Name == "find_group_members" && s.NativeMembers == nil ||
			call.Function.Name == "find_group_messages" && s.NativeMessages == nil {
			return out, Denied(409, "assistant_member_lookup_unavailable")
		}
		a, err := s.authorize(ctx, s.DB.Platform, ref, session.Scope, "assistant:action_propose", true, false)
		if err != nil {
			return out, err
		}
		tools, _, e := s.effectiveAssistantTools(ctx, a, session.Scope)
		if e != nil {
			return out, e
		}
		if !assistant.ContainsTool(tools, call.Function.Name) {
			return out, Denied(409, "assistant_module_closed")
		}
		if call.Function.Name == "find_group_messages" {
			grants, e := EffectivePermissions(ctx, s.DB.Platform, a, s.Catalog)
			if e != nil {
				return out, e
			}
			if !slices.Contains(grants, "group_action:recall_message") {
				return out, Denied(403, "permission_denied")
			}
		}
		var nativeGroup string
		err = s.DB.Platform.QueryRow(ctx, `SELECT native_id FROM native_groups WHERE id=$1 AND platform=$2`, session.Scope.GroupID, session.Scope.Platform).Scan(&nativeGroup)
		if err != nil {
			return out, err
		}
		fingerprint, err := s.authorizationHash(ctx, ref, session.Scope)
		if err != nil {
			return out, err
		}
		var members []AssistantMember
		var messages []AssistantMessage
		if call.Function.Name == "find_group_messages" {
			messages, err = s.NativeMessages(ctx, session.Scope.Platform, session.Scope.AccountID, nativeGroup, args.Search)
			for _, message := range messages {
				members = append(members, message.Member)
			}
		} else {
			members, err = s.NativeMembers(context.WithValue(ctx, nativeReadActorKey{}, a), session.Scope.Platform, session.Scope.AccountID, nativeGroup, args.Search)
		}
		if err != nil {
			return out, err
		}
		if len(members) > 20 {
			members = members[:20]
			out.Truncated = true
		}
		for i := range members {
			members[i].Token = ""
			target := assistantTarget{Actor: a.ID, Tenant: a.TenantID, Scope: session.Scope, Member: members[i],
				Fingerprint: fingerprint, ExpiresAt: time.Now().Add(10 * time.Minute), RequireChoice: len(members) > 1}
			if call.Function.Name == "find_group_messages" {
				target.Message = &messages[i].Reference
				target.RequireChoice = true
				target.ExpiresAt = time.UnixMilli(target.Message.CreatedAt).Add(2 * time.Minute)
			}
			raw, _ := json.Marshal(target)
			sealed, e := s.Vault.Seal("assistant:target", raw)
			clear(raw)
			if e != nil {
				return out, e
			}
			members[i].Token = base64.RawURLEncoding.EncodeToString(sealed)
		}
		current, err := s.authorizationHash(ctx, ref, session.Scope)
		if err != nil {
			return out, err
		}
		if current != fingerprint {
			return out, Denied(403, "assistant_history_scope_changed")
		}
		out.Data = members
		return out, nil
	case "propose_group_action":
		var args struct {
			Action      string `json:"action"`
			TargetToken string `json:"target_token"`
			Text        string `json:"text"`
			Minutes     int    `json:"minutes"`
		}
		if err := strictJSON(call.Function.Arguments, &args); err != nil {
			return out, err
		}
		plan, err := s.proposeAction(ctx, ref, session, args.Action, args.TargetToken, args.Text, args.Minutes)
		out.Data = plan
		return out, err
	}
	return out, Denied(400, "assistant_invalid_tool")
}

func validateAssistantAction(action, text string, minutes int, target bool) error {
	if !utf8.ValidString(text) || strings.IndexFunc(text, func(r rune) bool {
		return unicode.IsControl(r) && r != '\n' && r != '\t'
	}) >= 0 {
		return Denied(400, "assistant_action_invalid")
	}
	switch action {
	case "mute_member":
		if !target || text != "" || minutes < 1 || minutes > 1440 {
			return Denied(400, "assistant_action_invalid")
		}
	case "unmute_member", "kick_member":
		if !target || text != "" || minutes != 0 {
			return Denied(400, "assistant_action_invalid")
		}
	case "member_nickname":
		if !target || len([]rune(text)) > 40 || minutes != 0 {
			return Denied(400, "assistant_action_invalid")
		}
	case "recall_message":
		if !target || text != "" || minutes != 0 {
			return Denied(400, "assistant_action_invalid")
		}
	case "group_info":
		if target || !nativeauth.ValidNativeName(text, false) || strings.IndexFunc(text, unicode.IsSpace) >= 0 || minutes != 0 {
			return Denied(400, "assistant_action_invalid")
		}
	case "publish_notice", "send_text":
		limit := 600
		if action == "send_text" {
			limit = 2000
		}
		if target || strings.TrimSpace(text) == "" || len([]rune(text)) > limit || minutes != 0 {
			return Denied(400, "assistant_action_invalid")
		}
	case "mute_group", "unmute_group":
		if target || text != "" || minutes != 0 {
			return Denied(400, "assistant_action_invalid")
		}
	default:
		return Denied(409, "assistant_action_unavailable")
	}
	return nil
}

func (s *AssistantService) proposeAction(ctx context.Context, ref SessionRef, session AssistantSession, action, token, text string, minutes int) (AssistantActionPlan, error) {
	plan := AssistantActionPlan{ID: NewID(), SessionID: session.ID, Scope: session.Scope, Action: action, Text: text, Minutes: minutes, Version: 1, Status: "pending_approval"}
	d, ok := features.FindAssistantAction(session.Scope.Platform, action)
	if !ok || !d.Implemented {
		return plan, Denied(409, "assistant_action_unavailable")
	}
	a, err := s.authorize(ctx, s.DB.Platform, ref, session.Scope, "assistant:action_propose", true, false)
	if err != nil {
		return plan, err
	}
	entitlement, err := ResolveHumanEntitlement(ctx, s.DB.Platform, a, s.Catalog, session.Scope.Platform, session.Scope.GroupID)
	if err != nil {
		return plan, err
	}
	plan.ExecutionMode = entitlement.Mode
	grants, err := EffectivePermissions(ctx, s.DB.Platform, a, s.Catalog)
	if err != nil {
		return plan, err
	}
	if !slices.Contains(grants, d.Permission) {
		return plan, Denied(403, "permission_denied")
	}
	fingerprint, err := s.authorizationHash(ctx, ref, session.Scope)
	if err != nil {
		return plan, err
	}
	if token != "" {
		if len(token) > 4096 {
			return plan, Denied(400, "assistant_target_invalid")
		}
		sealed, e := base64.RawURLEncoding.DecodeString(token)
		if e != nil {
			return plan, Denied(400, "assistant_target_invalid")
		}
		raw, e := s.Vault.Open("assistant:target", sealed)
		if e != nil {
			return plan, Denied(400, "assistant_target_invalid")
		}
		defer clear(raw)
		var target assistantTarget
		if json.Unmarshal(raw, &target) != nil || target.Actor != a.ID || target.Tenant != a.TenantID || target.Scope != session.Scope ||
			target.Fingerprint != fingerprint || !time.Now().Before(target.ExpiresAt) {
			return plan, Denied(409, "assistant_target_expired")
		}
		if target.RequireChoice {
			var chosen bool
			err = s.DB.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM assistant_target_choices
				WHERE session_id=$1 AND actor_id=$2 AND token_digest=$3 AND expires_at>clock_timestamp())`,
				session.ID, a.ID, digest(token)).Scan(&chosen)
			if err != nil {
				return plan, err
			}
			if !chosen {
				return plan, Denied(409, "assistant_target_choice_required")
			}
		}
		plan.Target = &target.Member
		plan.Target.Token = ""
		if action == "recall_message" {
			if target.Message == nil || !target.Message.Fresh(time.Now()) {
				return plan, Denied(409, "assistant_target_expired")
			}
			plan.message = target.Message
		} else if target.Message != nil {
			return plan, Denied(400, "assistant_target_invalid")
		}
	}
	if err = validateAssistantAction(action, text, minutes, plan.Target != nil); err != nil {
		return plan, err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return plan, err
	}
	defer tx.Rollback(ctx)
	if _, err = s.authorize(ctx, tx, ref, session.Scope, "assistant:action_propose", true, true); err != nil {
		return plan, err
	}
	plan.PolicyVersion, err = s.assistantPolicyFor(ctx, tx, a, session.Scope, "action_propose", "", action, 0)
	if err != nil {
		return plan, err
	}
	plan.OccurredAt = time.Now().UTC()
	for _, capability := range features.AssistantCapabilities(session.Scope.Platform) {
		if capability.Action == action && capability.Category == "action_propose" {
			plan.Module, plan.CapabilityID = capability.Module, capability.ID
		}
	}
	var tenant, nativeGroup string
	err = tx.QueryRow(ctx, `SELECT b.tenant_id::text,b.version,b.epoch,a.epoch,g.native_id
		FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id JOIN native_groups g ON g.id=b.group_id
		WHERE b.group_id=$1 AND b.primary_account_id=$2 AND b.active AND a.platform=$3 FOR SHARE OF b,a`,
		session.Scope.GroupID, session.Scope.AccountID, session.Scope.Platform).Scan(&tenant, &plan.BaseVersion, &plan.BindingEpoch, &plan.AccountEpoch, &nativeGroup)
	if err != nil {
		return plan, Denied(409, "assistant_scope_changed")
	}
	change := plan.settingChange(nativeGroup)
	if err = ValidateNativeChange(session.Scope.Platform, change); err != nil {
		return plan, Denied(400, "assistant_action_invalid")
	}
	d, err = ScopedHumanAction(ctx, tx, a, s.Catalog, session.Scope.Platform, session.Scope.GroupID, action, "assistant")
	if err != nil {
		return plan, err
	}
	if err = actionScopeLabels(ctx, tx, &plan); err != nil {
		return plan, err
	}
	plan.controlled, err = controlledExecutionScope(ctx, tx, a.ID, session.Scope.Platform, session.Scope.GroupID)
	if err != nil {
		return plan, err
	}
	plan.Name, plan.Production = d.Name, d.Production
	if !d.Production {
		plan.Status = "blocked"
		plan.ResultCode = d.BlockReason
	}
	plan.Summary = assistantActionSummary(plan)
	plan.ExpiresAt = time.Now().UTC().Add(10 * time.Minute)
	plan.Digest = digest(struct {
		Actor   string
		Tenant  string
		Payload assistantActionPayload
	}{a.ID, tenant, assistantActionPayload{controlledExecutionFence: plan.controlled, Plan: plan, AccountEpoch: plan.AccountEpoch, BindingEpoch: plan.BindingEpoch, Message: plan.message}})
	raw, _ := json.Marshal(assistantActionPayload{controlledExecutionFence: plan.controlled, Plan: plan, AccountEpoch: plan.AccountEpoch, BindingEpoch: plan.BindingEpoch, Message: plan.message})
	sealed, err := s.Vault.Seal("assistant:action:"+plan.ID, raw)
	clear(raw)
	if err != nil {
		return plan, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO assistant_action_plans(id,session_id,actor_id,tenant_id,domain,account_id,group_id,action,
		digest,authorization_hash,payload_sealed,account_epoch,binding_epoch,base_version,status,result_code,expires_at,execution_mode)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18)`, plan.ID, session.ID, a.ID, tenant,
		session.Scope.Platform, session.Scope.AccountID, session.Scope.GroupID, action, plan.Digest, fingerprint, sealed,
		plan.AccountEpoch, plan.BindingEpoch, plan.BaseVersion, plan.Status, plan.ResultCode, plan.ExpiresAt, plan.ExecutionMode)
	if err != nil {
		return plan, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		VALUES($1,$2,'assistant_action_proposed',$3,jsonb_build_object('action',$4::text,'status',$5::text,'platform',$6::text))`,
		tenant, a.ID, plan.ID, action, plan.Status, plan.Scope.Platform)
	if err != nil {
		return plan, err
	}
	return plan, tx.Commit(ctx)
}

func (s *AssistantService) ChooseTarget(ctx context.Context, ref SessionRef, sessionID, token string) (AssistantMember, error) {
	var out AssistantMember
	if len(token) > 4096 {
		return out, Denied(400, "assistant_target_invalid")
	}
	session, err := s.Session(ctx, ref, sessionID, true)
	if err != nil {
		return out, err
	}
	a, err := s.authorize(ctx, s.DB.Platform, ref, session.Scope, "assistant:action_propose", true, false)
	if err != nil {
		return out, err
	}
	sealed, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		return out, Denied(400, "assistant_target_invalid")
	}
	raw, err := s.Vault.Open("assistant:target", sealed)
	if err != nil {
		return out, Denied(400, "assistant_target_invalid")
	}
	defer clear(raw)
	var target assistantTarget
	fingerprint, err := s.authorizationHash(ctx, ref, session.Scope)
	if err != nil {
		return out, err
	}
	if json.Unmarshal(raw, &target) != nil || target.Actor != a.ID || target.Scope != session.Scope || target.Tenant != a.TenantID ||
		target.Fingerprint != fingerprint || !time.Now().Before(target.ExpiresAt) {
		return out, Denied(409, "assistant_target_expired")
	}
	_, err = s.DB.Platform.Exec(ctx, `INSERT INTO assistant_target_choices(session_id,actor_id,tenant_id,domain,token_digest,expires_at)
		VALUES($1,$2,nullif($3,'')::uuid,$4,$5,$6) ON CONFLICT(session_id) DO UPDATE SET token_digest=excluded.token_digest,expires_at=excluded.expires_at`,
		session.ID, a.ID, a.TenantID, session.Scope.Platform, digest(token), target.ExpiresAt)
	if err != nil {
		return out, err
	}
	out = target.Member
	out.Token = token
	return out, nil
}

func (s *AssistantService) ActionPlan(ctx context.Context, ref SessionRef, id string) (AssistantActionPlan, error) {
	var out AssistantActionPlan
	if !ValidID(id) {
		return out, Denied(404, "assistant_plan_not_found")
	}
	a, err := s.actor(ctx, s.DB.Platform, ref, false)
	if err != nil {
		return out, err
	}
	var sealed []byte
	var fingerprint, status, code, operation, mode string
	var version int64
	var accountEpoch, bindingEpoch int64
	if batch := cachedAssistantPlans(ctx, a.ID, ref.assistantPlatform()); batch != nil {
		record, ok := batch.actions[id]
		if !ok {
			err = pgx.ErrNoRows
		} else {
			sealed, fingerprint, status, code, version, operation, accountEpoch, bindingEpoch, mode = record.Sealed, record.Fingerprint, record.Status, record.Code, record.Version, record.Operation, record.AccountEpoch, record.BindingEpoch, record.Mode
		}
	} else {
		err = s.read(ctx, a, ref.assistantPlatform(), func(q Queryer) error {
			return q.QueryRow(ctx, `SELECT payload_sealed,authorization_hash,status,result_code,version,coalesce(operation_id::text,''),account_epoch,binding_epoch,execution_mode
			FROM assistant_action_plans WHERE id=$1 AND actor_id=$2 AND domain=$3`, id, a.ID, ref.assistantPlatform()).
				Scan(&sealed, &fingerprint, &status, &code, &version, &operation, &accountEpoch, &bindingEpoch, &mode)
		})
	}
	if err == pgx.ErrNoRows {
		return out, Denied(404, "assistant_plan_not_found")
	}
	if err != nil {
		return out, err
	}
	raw, err := s.Vault.Open("assistant:action:"+id, sealed)
	if err != nil {
		return out, err
	}
	defer clear(raw)
	var payload assistantActionPayload
	if err = json.Unmarshal(raw, &payload); err != nil {
		return out, err
	}
	out = payload.Plan
	out.controlled = payload.controlledExecutionFence
	if err = out.controlled.require(ctx, s.DB.Platform, a.ID, out.Scope.Platform, out.Scope.GroupID); err != nil {
		return AssistantActionPlan{}, err
	}
	if _, err = s.Session(ctx, ref, out.SessionID, false); err != nil {
		return AssistantActionPlan{}, err
	}
	out.message = payload.Message
	if out.ID != id || payload.AccountEpoch != accountEpoch || payload.BindingEpoch != bindingEpoch ||
		mode != "legacy" && out.ExecutionMode != mode {
		return AssistantActionPlan{}, Denied(409, "assistant_plan_mismatch")
	}
	out.ExecutionMode = mode
	entitlement, err := ResolveHumanEntitlement(ctx, s.DB.Platform, a, s.Catalog, out.Scope.Platform, out.Scope.GroupID)
	if err != nil {
		return AssistantActionPlan{}, err
	}
	if mode != "legacy" && mode != entitlement.Mode {
		return AssistantActionPlan{}, Denied(409, "assistant_scope_changed")
	}
	current, err := s.authorizationHash(ctx, ref, out.Scope)
	if err != nil {
		return AssistantActionPlan{}, err
	}
	if current != fingerprint {
		// Only submitted pre-policy records can use the old fingerprint format.
		// This never admits an old pending plan or ignores changed scope/rights.
		if out.PolicyVersion != 0 || !slices.Contains([]string{"approved", "submitting", "unknown", "succeeded", "failed"}, status) {
			return AssistantActionPlan{}, Denied(403, "assistant_history_scope_changed")
		}
		prior, e := s.assistantAuthorizationHash(ctx, ref, out.Scope, true)
		if e != nil {
			return AssistantActionPlan{}, e
		}
		if prior != fingerprint {
			return AssistantActionPlan{}, Denied(403, "assistant_history_scope_changed")
		}
	}
	grants, err := EffectivePermissions(ctx, s.DB.Platform, a, s.Catalog)
	if err != nil {
		return AssistantActionPlan{}, err
	}
	if !slices.Contains(grants, "assistant:action_propose") || !slices.Contains(grants, "group_action:"+out.Action) {
		return AssistantActionPlan{}, Denied(403, "permission_denied")
	}
	out.Status, out.ResultCode, out.Version, out.OperationID = status, code, version, operation
	out.AccountEpoch, out.BindingEpoch = accountEpoch, bindingEpoch
	if status == "unknown" || status == "submitting" || status == "approved" {
		// Only reconcile the original durable record. A read never dispatches it.
		var recordedID, recordedStatus, recordedCode string
		e := s.DB.Platform.QueryRow(ctx, `SELECT id::text,status,result_code FROM native_setting_operations
			WHERE actor_id=$1 AND account_id=$2 AND group_id=$3 AND idempotency_key=$4 AND kind=$5
			AND ($6='' OR id::text=$6)`, a.ID, out.Scope.AccountID, out.Scope.GroupID, out.ID, out.Action, operation).
			Scan(&recordedID, &recordedStatus, &recordedCode)
		if e != nil && e != pgx.ErrNoRows {
			return AssistantActionPlan{}, e
		}
		if e == nil {
			out.OperationID = recordedID
			if recordedStatus == "succeeded" || recordedStatus == "failed" {
				out.Status, out.ResultCode = recordedStatus, recordedCode
			} else {
				out.Status = "unknown"
			}
		}
	}
	if (status == "pending_approval" || status == "blocked") && !time.Now().Before(out.ExpiresAt) {
		out.Status = "expired"
	}
	d, err := ScopedHumanAction(ctx, s.DB.Platform, a, s.Catalog, out.Scope.Platform, out.Scope.GroupID, out.Action, "assistant")
	if err != nil {
		return AssistantActionPlan{}, err
	}
	out.Production = d.Production
	if out.Status == "pending_approval" || out.Status == "blocked" {
		_, e := s.assistantPolicyFor(ctx, s.DB.Platform, a, out.Scope, "action_propose", "", out.Action, out.PolicyVersion)
		if out.PolicyVersion == 0 || e != nil {
			if e != nil {
				if _, ok := e.(*Failure); !ok {
					return AssistantActionPlan{}, e
				}
			}
			out.Status, out.Production, out.ResultCode = "invalidated", false, "assistant_module_policy_changed"
		}
	}
	if _, err = s.Session(ctx, ref, out.SessionID, false); err != nil {
		return AssistantActionPlan{}, err
	}
	out.Summary = assistantActionSummary(out)
	s.observeAssistantAction(ctx, &out)
	if _, err = s.Session(ctx, ref, out.SessionID, false); err != nil {
		return AssistantActionPlan{}, err
	}
	currentFingerprint, e := s.authorizationHash(ctx, ref, out.Scope)
	if e != nil {
		return AssistantActionPlan{}, e
	}
	if currentFingerprint != current {
		return AssistantActionPlan{}, Denied(403, "assistant_scope_changed")
	}
	out.Summary = assistantActionSummary(out)
	return out, nil
}

func (s *AssistantService) CancelAction(ctx context.Context, ref SessionRef, id string) (AssistantActionPlan, error) {
	out, err := s.ActionPlan(ctx, ref, id)
	if err != nil {
		return out, err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, err := s.authorize(ctx, tx, ref, out.Scope, "assistant:action_propose", false, true)
	if err != nil {
		return out, err
	}
	tag, err := tx.Exec(ctx, `UPDATE assistant_action_plans SET status='cancelled',version=version+1
		WHERE id=$1 AND status IN('pending_approval','blocked')`, id)
	if err != nil {
		return out, err
	}
	if tag.RowsAffected() == 1 {
		_, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
			SELECT tenant_id,$2,'assistant_action_cancelled',id::text,
			jsonb_build_object('action',action,'platform',domain,'status','cancelled')
			FROM assistant_action_plans WHERE id=$1`, id, a.ID)
		if err != nil {
			return out, err
		}
	}
	if err = tx.Commit(ctx); err != nil {
		return out, Denied(503, "assistant_commit_unknown")
	}
	return s.ActionPlan(ctx, ref, id)
}

func (s *AssistantService) ApproveAction(ctx context.Context, ref SessionRef, id, summary string, version int64, key string) (AssistantActionPlan, error) {
	out, err := s.ActionPlan(ctx, ref, id)
	if err != nil {
		return out, err
	}
	if len(key) < 8 || len(key) > 100 {
		return out, Denied(400, "idempotency_key_required")
	}
	if summary != out.Digest {
		return out, Denied(409, "assistant_plan_mismatch")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return out, err
	}
	a, err := s.authorize(ctx, tx, ref, out.Scope, "assistant:action_approve", false, true)
	if err != nil {
		return out, err
	}
	if err = out.controlled.require(ctx, tx, a.ID, out.Scope.Platform, out.Scope.GroupID); err != nil {
		return out, err
	}
	if err = liveAssistantSession(ctx, tx, out.SessionID, a.ID, out.Scope.Platform); err != nil {
		return out, err
	}
	grants, err := EffectivePermissions(ctx, tx, a, s.Catalog)
	if err != nil {
		return out, err
	}
	if !slices.Contains(grants, "assistant:action_propose") || !slices.Contains(grants, "group_action:"+out.Action) {
		return out, Denied(403, "permission_denied")
	}
	var status, oldKey string
	var currentVersion int64
	var expires time.Time
	err = tx.QueryRow(ctx, `SELECT status,coalesce(idempotency_key,''),version,expires_at FROM assistant_action_plans
		WHERE id=$1 AND actor_id=$2 FOR UPDATE`, id, a.ID).Scan(&status, &oldKey, &currentVersion, &expires)
	if err != nil {
		return out, err
	}
	if oldKey != "" {
		if oldKey != key {
			return out, Denied(409, "idempotency_conflict")
		}
		if err = tx.Commit(ctx); err != nil {
			return out, err
		}
		return s.ActionPlan(ctx, ref, id) // Never replay an approved, submitting or unknown operation.
	}
	if currentVersion != version {
		return out, Denied(409, "version_conflict")
	}
	if !time.Now().Before(expires) {
		return out, Denied(409, "assistant_plan_not_pending")
	}
	if out.PolicyVersion == 0 {
		return out, Denied(409, "assistant_module_policy_changed")
	}
	if _, err = s.assistantPolicyFor(ctx, tx, a, out.Scope, "action_propose", "", out.Action, out.PolicyVersion); err != nil {
		return out, err
	}
	if _, err = tx.Exec(ctx, `SELECT singleton FROM assistant_settings FOR SHARE`); err != nil {
		return out, err
	}
	settings, _, err := s.settings(ctx, tx)
	if err != nil {
		return out, err
	}
	if !settings.Enabled || !settings.ApprovalsEnabled {
		return out, Denied(409, "assistant_approvals_disabled")
	}
	if err = RequireHumanBusinessAdmission(ctx, tx, a, s.Catalog, out.Scope.Platform, out.Scope.GroupID, out.Action, "assistant"); err != nil {
		return out, err
	}
	if status != "pending_approval" && status != "blocked" {
		return out, Denied(409, "assistant_plan_not_pending")
	}
	if s.ActionExecutor == nil {
		return out, Denied(409, "assistant_action_executor_unavailable")
	}
	var valid bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_bindings b JOIN accounts a ON a.id=b.primary_account_id
		WHERE b.group_id=$1 AND b.primary_account_id=$2 AND b.active
		AND b.version=$3 AND b.epoch=$4 AND a.epoch=$5 AND a.auth_state='authenticated'
		AND a.platform=$6 AND a.workspace=$6)`,
		out.Scope.GroupID, out.Scope.AccountID, out.BaseVersion, out.BindingEpoch, out.AccountEpoch, out.Scope.Platform).Scan(&valid)
	if err != nil {
		return out, err
	}
	if !valid {
		return out, Denied(409, "assistant_scope_changed")
	}
	_, err = tx.Exec(ctx, `UPDATE assistant_action_plans SET status='approved',version=version+1,idempotency_key=$2 WHERE id=$1`, id, key)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		SELECT tenant_id,$2,'assistant_action_approved',id::text,
		jsonb_build_object('action',action,'platform',domain,'status','approved')
		FROM assistant_action_plans WHERE id=$1`, id, a.ID)
	if err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, Denied(503, "assistant_commit_unknown")
	}
	operation, executeErr := s.ActionExecutor(ctx, ref, out)
	state, code := operation.Status, operation.Code
	if operation.ID == "" {
		state = "failed"
		code = assistantCode(executeErr)
	}
	if state != "succeeded" && state != "failed" {
		state = "unknown"
	}
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, err = s.DB.Platform.Exec(save, `WITH finished AS (
		UPDATE assistant_action_plans SET status=$2,result_code=$3,operation_id=nullif($4,'')::uuid,version=version+1
		WHERE id=$1 AND status IN('approved','submitting') RETURNING *)
		INSERT INTO audit_events(tenant_id,actor_id,action,resource_id,detail)
		SELECT tenant_id,actor_id,'assistant_action_finished',id::text,
		jsonb_build_object('action',action,'platform',domain,'status',status) FROM finished`, id, state, code, operation.ID)
	if err != nil {
		return out, Denied(503, "assistant_commit_unknown")
	}
	return s.ActionPlan(ctx, ref, id)
}
