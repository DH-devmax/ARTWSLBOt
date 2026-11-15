package control

import (
	"bytes"
	"context"
	"encoding/json"
	"slices"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/features"
	"wangshangliao/internal/security"
)

func (scope BusinessScope) snapshot() []byte {
	raw, _ := json.Marshal(scope)
	return raw
}

func (s *BusinessRulesService) scope(ctx context.Context, q Queryer, group string) (BusinessScope, features.Config, error) {
	var scope BusinessScope
	var config features.Config
	if !ValidID(group) {
		return scope, config, Denied(404, "group_not_found")
	}
	var raw []byte
	err := q.QueryRow(ctx, `SELECT g.platform,b.tenant_id::text,t.owner_id::text,b.group_id::text,g.native_id,
		a.id::text,a.native_uid,b.version,b.epoch,a.epoch,a.version,r.version,r.enabled_at,
		u.access_version,coalesce(sub.version,0),coalesce(p.version,0),coalesce(cr.revision,0),coalesce(cr.owner,''),
		coalesce(tc.version,0),b.config,
		CASE WHEN g.platform='wsl' THEN coalesce((SELECT version FROM native_deployment WHERE singleton),0) ELSE 0 END
		FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
		JOIN accounts a ON a.id=b.primary_account_id
		JOIN tenants t ON t.id=b.tenant_id JOIN users u ON u.id=t.owner_id
		LEFT JOIN subscriptions sub ON sub.tenant_id=b.tenant_id LEFT JOIN plans p ON p.id=sub.plan_id
		JOIN group_business_runtime r ON r.group_id=b.group_id AND r.tenant_id=b.tenant_id
		LEFT JOIN tenant_capability_ceilings tc ON tc.tenant_id=b.tenant_id
		JOIN account_receivers cr ON cr.account_id=a.id
		WHERE b.group_id=$1 AND b.active AND b.management_enabled AND r.enabled AND r.enabled_at IS NOT NULL
		AND r.enabled_at<=clock_timestamp()
		AND a.purpose='management' AND a.platform=g.platform AND a.workspace=g.platform
		AND a.auth_state='authenticated' AND a.credential_sealed IS NOT NULL
		AND cr.enabled AND cr.owner<>'' AND cr.lease_until>clock_timestamp()+interval '3 seconds'
		AND cr.observed_at>clock_timestamp()-interval '15 seconds' AND cr.observed_at<=clock_timestamp()
		AND cr.snapshot->>'state'='connected'
		AND (workspace_is_demo(t.id,u.id) OR t.space_kind='tenant_business'
			AND u.role='user' AND u.tenant_id=b.tenant_id AND u.tenant_role='owner' AND NOT u.disabled
			AND sub.enabled AND sub.expires_at>clock_timestamp()+interval '3 seconds'
			AND g.platform=ANY(p.allowed_platforms) AND p.group_limit>0)`, group).Scan(
		&scope.Platform, &scope.Tenant, &scope.Actor, &scope.Group, &scope.NativeGroup,
		&scope.Account, &scope.NativeSelf, &scope.BindingVersion, &scope.BindingEpoch,
		&scope.AccountEpoch, &scope.AccountVersion, &scope.RuntimeVersion, &scope.EnabledAt,
		&scope.ActorVersion, &scope.SubscriptionVersion, &scope.PlanVersion,
		&scope.ReceiverVersion, &scope.ReceiverOwner, &scope.CapabilityVersion, &raw, &scope.DeploymentVersion)
	if err != nil {
		return scope, config, Denied(409, "business_scope_unavailable")
	}
	scope.controlledExecutionFence, err = controlledExecutionScope(ctx, q, "", scope.Platform, group)
	if err != nil {
		return scope, config, err
	}
	if len(raw) > 1<<20 || json.Unmarshal(raw, &config) != nil || features.Validate(config) != nil {
		return scope, config, Denied(409, "business_configuration_invalid")
	}
	if !features.BusinessRulesEnabled(config) {
		return scope, config, Denied(409, "business_rules_disabled")
	}
	service, err := WorkspaceServiceEntitlement(ctx, q, scope.Tenant)
	if err != nil {
		return scope, config, err
	}
	if service.OwnerID != scope.Actor || service.OwnerVersion != scope.ActorVersion {
		return scope, config, Denied(409, "business_scope_changed")
	}
	scope.Authorization, err = service.permissions(ctx, q, s.Catalog)
	if err != nil {
		return scope, config, err
	}
	slices.Sort(scope.Authorization)
	err = q.QueryRow(ctx, `SELECT coalesce(array_agg(c.action||':'||c.version::text||':'||c.state||':'||
		(EXISTS(SELECT 1 FROM business_capability_pilots bp WHERE bp.platform=c.platform
			AND bp.action=c.action AND bp.source=c.source AND bp.group_id=$2
			AND bp.gate_version=c.version))::text ORDER BY c.action),'{}')
		FROM business_capability_gates c WHERE c.platform=$1 AND c.source='automatic'
		AND c.action IN('recall_message','mute_member','send_text')`, scope.Platform, scope.Group).Scan(&scope.AdmissionVersions)
	return scope, config, err
}

func (s *BusinessRulesService) authorizeAction(ctx context.Context, q Queryer, scope BusinessScope,
	config features.Config, rule, action string) error {
	if err := RequireGroupRuntime(ctx, q, scope.Group); err != nil {
		return err
	}
	if err := RequireBusinessAdmission(ctx, q, scope.Platform, scope.Group, action, "automatic"); err != nil {
		return err
	}
	if !slices.Contains(scope.Authorization, "group_action:"+action) {
		return Denied(403, "permission_denied")
	}
	service, err := WorkspaceServiceEntitlement(ctx, q, scope.Tenant)
	if err != nil {
		return err
	}
	if service.OwnerID != scope.Actor || service.OwnerVersion != scope.ActorVersion {
		return Denied(409, "business_scope_changed")
	}
	definition, ok := features.Find(rule)
	if !ok || !slices.Contains([]string{"blocked_words", "blacklist", "keyword_replies"}, rule) {
		return Denied(409, "business_rule_invalid")
	}
	for _, field := range definition.Fields {
		// Default values retain the pre-extension runtime contract. Editing the
		// new fields still requires their independent permissions.
		if rule == "blocked_words" && field.Key == "rules" && len(config.BlockedRules) == 0 ||
			rule == "keyword_replies" && field.Key == "cooldown_seconds" && features.KeywordReplyCooldown(config) == 30 {
			continue
		}
		if !service.Demo {
			if err := RequireTenantCapability(ctx, q, scope.Tenant, features.Permission(rule, field.Key)); err != nil {
				return err
			}
		}
		if !features.CanEdit(scope.Authorization, definition, field.Key) {
			return Denied(403, "permission_denied")
		}
	}
	return service.requireGroup(ctx, q, scope.Group)
}

func (s *BusinessRulesService) lockScope(ctx context.Context, tx pgx.Tx, expected BusinessScope,
	payload businessPayload, proof BusinessEvidence) error {
	if _, err := tx.Exec(ctx, `SET LOCAL lock_timeout='500ms'`); err != nil {
		return err
	}
	var id string
	err := tx.QueryRow(ctx, `SELECT a.id::text FROM accounts a
		JOIN group_bindings b ON b.primary_account_id=a.id
		JOIN native_groups g ON g.id=b.group_id JOIN tenants t ON t.id=b.tenant_id
		JOIN users u ON u.id=t.owner_id
		JOIN group_business_runtime r ON r.group_id=b.group_id
		WHERE b.group_id=$1 AND a.id=$2
		FOR SHARE OF a,b,g,t,u,r`, expected.Group, expected.Account).Scan(&id)
	if err != nil {
		return Denied(409, "business_scope_changed")
	}
	service, err := WorkspaceServiceEntitlement(ctx, tx, expected.Tenant)
	if err != nil {
		return err
	}
	if !service.Demo {
		err = tx.QueryRow(ctx, `SELECT sub.tenant_id::text FROM subscriptions sub
			JOIN plans p ON p.id=sub.plan_id JOIN tenant_capability_ceilings tc ON tc.tenant_id=sub.tenant_id
			WHERE sub.tenant_id=$1 AND sub.expires_at>clock_timestamp()+make_interval(secs=>$2)
			FOR SHARE OF sub,p,tc`, expected.Tenant, businessWriteFence.Seconds()+1).Scan(&id)
		if err != nil {
			return Denied(409, "business_scope_changed")
		}
	}
	if expected.Platform == "wsl" {
		var version int64
		if err = tx.QueryRow(ctx, `SELECT version FROM native_deployment WHERE singleton FOR SHARE`).Scan(&version); err != nil {
			return Denied(409, "business_deployment_unavailable")
		}
	}
	current, config, err := s.scope(ctx, tx, expected.Group)
	if err != nil || !bytes.Equal(current.snapshot(), expected.snapshot()) {
		return Denied(409, "business_scope_changed")
	}
	if err = s.authorizeAction(ctx, tx, current, config, payload.Decision.Rule, payload.Decision.Action); err != nil {
		return err
	}
	var credential []byte
	if err = tx.QueryRow(ctx, `SELECT credential_sealed FROM accounts WHERE id=$1`, current.Account).Scan(&credential); err != nil {
		return err
	}
	if !bytes.Equal(security.Digest(string(credential)), proof.CredentialDigest[:]) {
		return Denied(409, "business_native_evidence_changed")
	}
	if err = validateBusinessEvidence(current, payload, config, proof, time.Now()); err != nil {
		return err
	}
	if err = requireBusinessOwnerAnchor(ctx, tx, current, proof.OwnerID); err != nil {
		return err
	}
	var robot bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts WHERE platform=$1 AND native_uid=$2)`,
		current.Platform, payload.Sender).Scan(&robot); err != nil {
		return err
	}
	if robot {
		return Denied(409, "business_pool_robot_ignored")
	}
	return nil
}

func requireBusinessOwnerAnchor(ctx context.Context, q Queryer, scope BusinessScope, owner string) error {
	if err := scope.controlledExecutionFence.require(ctx, q, "", scope.Platform, scope.Group); err != nil {
		return err
	}
	var tenant, platform, state string
	var digest []byte
	err := q.QueryRow(ctx, `SELECT tenant_id::text,platform,state,owner_identity_hash
		FROM group_binding_ownership WHERE group_id=$1 FOR SHARE`, scope.Group).
		Scan(&tenant, &platform, &state, &digest)
	if err == pgx.ErrNoRows {
		// A legacy manual binding has no new proof. Losing the anchor of an
		// authenticated onboarding binding cannot turn it into a legacy one.
		var proven bool
		err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_binding_challenges
			WHERE group_id=$1 AND tenant_id=$2 AND platform=$3 AND state='bound')`,
			scope.Group, scope.Tenant, scope.Platform).Scan(&proven)
		if err == nil && proven {
			return Denied(409, "group_ownership_proof_required")
		}
		return err
	}
	if err != nil {
		return err
	}
	if tenant != scope.Tenant || platform != scope.Platform || state != "active" {
		return Denied(409, "group_ownership_reproof_required")
	}
	if owner == "" || !bytes.Equal(digest, bindingIdentityDigest(scope.Platform, scope.Group, owner)) {
		return Denied(409, "group_ownership_changed")
	}
	return nil
}

// Pause through onboarding's independent transaction; rejecting the native
// submission must not roll this pause back. A stable or manual binding is
// read-only here and never acquires an invented ownership proof.
func (s *BusinessRulesService) observeOwner(ctx context.Context, scope BusinessScope, proof BusinessEvidence) error {
	err := requireBusinessOwnerAnchor(ctx, s.DB.Platform, scope, proof.OwnerID)
	if businessErrorCode(err) != "group_ownership_changed" {
		return err
	}
	bindings := GroupBindingService{DB: s.DB, Vault: s.Vault}
	return bindings.ObserveGroupOwnership(ctx, GroupBindingNativeScope{
		Platform: scope.Platform, AccountID: scope.Account, AccountEpoch: scope.AccountEpoch,
		AccountVersion: scope.AccountVersion, NativeGroupID: scope.NativeGroup,
		NativeSenderID: proof.Target,
	}, GroupBindingRoleEvidence{
		GroupBindingConnectionEvidence: GroupBindingConnectionEvidence{
			Platform: proof.Platform, AccountID: proof.Account, AccountEpoch: proof.AccountEpoch,
			AccountVersion: proof.AccountVersion, DeploymentVersion: proof.DeploymentVersion,
			ReceiverOwner: scope.ReceiverOwner, ReceiverRevision: scope.ReceiverVersion,
			CredentialDigest: proof.CredentialDigest, ObservedAt: proof.ObservedAt,
		},
		NativeGroupID: proof.Group, NativeSenderID: proof.Target, NativeOwnerID: proof.OwnerID,
		SenderRole: proof.TargetRole, RobotRole: proof.SelfRole, Present: true,
	})
}

func validateBusinessEvidence(scope BusinessScope, payload businessPayload, config features.Config,
	proof BusinessEvidence, now time.Time) error {
	if err := validateBusinessRole(scope, payload.Sender, proof, now); err != nil {
		return err
	}
	d := payload.Decision
	if !config.FeatureOptions[d.Rule].Enabled {
		return Denied(409, "business_rule_disabled")
	}
	switch d.Action {
	case features.BusinessRecall, features.BusinessMute:
		if proof.SelfRole != "admin" && proof.SelfRole != "owner" || proof.TargetRole != "member" ||
			d.Target != payload.Sender || payload.Sender == scope.NativeSelf ||
			config.FeatureOptions["exemptions"].Enabled && slices.Contains(config.Exemptions, payload.Sender) {
			return Denied(403, "business_native_role_required")
		}
		if d.Action == features.BusinessMute && (d.Rule != "blacklist" || d.Minutes != 10 ||
			!slices.Contains(config.Blacklist, payload.Sender)) {
			return Denied(409, "business_action_invalid")
		}
		if d.Action == features.BusinessRecall && (d.Rule != "blocked_words" || payload.Reference == nil ||
			payload.Reference.EventID < 1 || payload.Reference.Platform != scope.Platform ||
			payload.Reference.Group != scope.NativeGroup || payload.Reference.Sender != payload.Sender ||
			!payload.Reference.Fresh(now)) {
			return Denied(409, "business_message_reference_expired")
		}
	case features.BusinessReply:
		if d.Rule != "keyword_replies" || len(d.Text) == 0 || len(d.Text) > 4000 || !utf8.ValidString(d.Text) ||
			config.FeatureOptions["keyword_replies"].AdminOnly && proof.TargetRole != "admin" && proof.TargetRole != "owner" {
			return Denied(403, "business_reply_not_allowed")
		}
	default:
		return Denied(409, "business_action_invalid")
	}
	return nil
}

func validateBusinessRole(scope BusinessScope, sender string, proof BusinessEvidence, now time.Time) error {
	if proof.Platform != scope.Platform || proof.Account != scope.Account || proof.Group != scope.NativeGroup ||
		proof.SelfID != scope.NativeSelf || proof.Target != sender ||
		proof.AccountEpoch != scope.AccountEpoch || proof.AccountVersion != scope.AccountVersion ||
		proof.DeploymentVersion != scope.DeploymentVersion ||
		proof.ObservedAt.IsZero() || proof.ObservedAt.After(now) || now.Sub(proof.ObservedAt) > 5*time.Second {
		return Denied(409, "business_native_evidence_changed")
	}
	if proof.SelfRole != "member" && proof.SelfRole != "admin" && proof.SelfRole != "owner" ||
		proof.TargetRole != "member" && proof.TargetRole != "admin" && proof.TargetRole != "owner" ||
		proof.OwnerID == "" || (proof.SelfRole == "owner") != (proof.OwnerID == proof.SelfID) ||
		(proof.TargetRole == "owner") != (proof.OwnerID == proof.Target) {
		return Denied(409, "business_native_role_unknown")
	}
	return nil
}
