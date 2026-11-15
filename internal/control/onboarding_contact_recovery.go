package control

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
)

// A snapshot is resolved once from server-owned records, then compared in both
// later authorization transactions. It is also sealed with the new operation.
type onboardingContactSnapshot struct {
	Scope, Challenge, Window, RetiredOperation string
	Tenant, Account, Receiver, Group           string
	NativeSender, NativeGroup, Recipient       string
	ScopeVersion, ChallengeVersion             int64
	BindingVersion, BindingEpoch               int64
	ReceiverEpoch, ReceiverVersion             int64
	SourceOwner, ReceiverOwner                 string
	SourceRevision, ReceiverRevision           int64
}

type onboardingContactRecovery struct {
	actor    string
	snapshot *onboardingContactSnapshot
}

func (r *onboardingContactRecovery) excludedOperation() string {
	if r == nil || r.snapshot == nil {
		return ""
	}
	return r.snapshot.RetiredOperation
}

// The normal contact policy still checks the live receiver first. Only its
// pending-work rejection may be narrowed, after the existing grant, binding and
// role checks; cooling and every other durable writer retain their fences.
func (s *NativeSettingsService) onboardingContactWriteAvailable(ctx context.Context, tx pgx.Tx,
	req SettingRequest, proof *SettingEvidence, group, tenant string, binding int64, debug bool) (string, error) {
	err := friendSocialWriteAvailable(ctx, tx, req, "native_"+req.Change.Kind+"_receiver_required")
	var denied *Failure
	if err != nil && (!errors.As(err, &denied) || denied.Code != "native_setting_pending") {
		return "", err
	}
	recovery := req.contactRecovery
	if recovery == nil || err == nil && recovery.snapshot == nil {
		return "", err
	}
	if req.business != nil || req.Workspace != "lajiaoliao" || req.Change.Kind != "friend_delete" ||
		!debug || !ValidID(req.TestWindowID) || !ValidID(recovery.actor) {
		return "", Denied(409, "native_setting_pending")
	}
	current, resolveErr := s.resolveOnboardingContactRecovery(ctx, tx, req, proof, group, tenant, binding)
	if resolveErr != nil {
		return "", resolveErr
	}
	retired, resolveErr := retiredChiliPrivateProof(ctx, tx, s.Vault, retiredChiliProofContext{
		Actor: recovery.actor, Tenant: current.Tenant, Account: current.Account, Group: current.Group,
		NativeSender: current.NativeSender, NativeGroup: current.NativeGroup, Recipient: current.Recipient,
		Challenge: current.Challenge, Scope: current.Scope, Window: current.Window,
	}, "")
	if resolveErr != nil {
		return "", resolveErr
	}
	if !ValidID(retired) {
		return "", Denied(409, "native_setting_pending")
	}
	current.RetiredOperation = retired
	if recovery.snapshot != nil && *recovery.snapshot != current {
		return "", Denied(409, "controlled_onboarding_scope_changed")
	}
	if recovery.snapshot == nil {
		recovery.snapshot = &current
	}
	if err = friendSocialWriteAvailableWithContactRecovery(ctx, tx, req,
		"native_friend_delete_receiver_required", recovery); err != nil {
		return "", err
	}
	return retired, nil
}

func (s *NativeSettingsService) resolveOnboardingContactRecovery(ctx context.Context, tx pgx.Tx,
	req SettingRequest, proof *SettingEvidence, group, tenant string, binding int64) (onboardingContactSnapshot, error) {
	var out onboardingContactSnapshot
	used := 0
	if proof != nil {
		used = 1
	}
	// The live indexes permit one scope and one waiting challenge per actor and
	// platform. A new wizard challenge has no proven binding version yet; the
	// current paused binding is locked here and pinned by the recovery snapshot.
	// The singleton delete window is still independently required.
	err := tx.QueryRow(ctx, `SELECT s.id::text,s.version,c.id::text,c.version,w.id::text,
		s.tenant_id::text,claimant.id::text,receiver.id::text,g.id::text,
		claimant.native_uid,g.native_id,receiver.native_uid,b.version,b.epoch,
		receiver.epoch,receiver.version,cr.owner,cr.revision,rr.owner,rr.revision
		FROM onboarding_test_scopes s JOIN group_binding_challenges c ON c.controlled_scope_id=s.id
		JOIN accounts claimant ON claimant.id=s.claimant_account_id
		JOIN accounts receiver ON receiver.id=s.account_id
		JOIN account_receivers cr ON cr.account_id=claimant.id
		JOIN account_receivers rr ON rr.account_id=receiver.id
		JOIN native_groups g ON g.id=s.group_id
		JOIN group_bindings b ON b.group_id=g.id
		JOIN group_binding_ownership o ON o.group_id=g.id
		JOIN test_windows w ON w.id=$2
		JOIN users u ON u.id=s.actor_id JOIN tenants t ON t.id=s.tenant_id
		WHERE s.actor_id=$1 AND s.platform='lajiaoliao' AND s.claimant_account_id=$3
		AND s.tenant_id=$4 AND s.group_id=$5 AND onboarding_test_scope_active(s.id)
		AND u.role='superadmin' AND NOT u.disabled AND u.access_version=s.actor_version
		AND t.space_kind='operator_demo' AND t.operator_owner_id=u.id
		AND claimant.platform=s.platform AND claimant.workspace=s.platform AND claimant.purpose='management'
		AND claimant.connection_enabled AND claimant.native_uid=s.claimant_native_id
		AND receiver.platform=s.platform AND receiver.workspace=s.platform AND receiver.purpose='management'
		AND receiver.connection_enabled AND receiver.native_uid=$6 AND receiver.id<>claimant.id
		AND c.applicant_id=s.actor_id AND c.applicant_mode='operator_demo' AND c.tenant_id=s.tenant_id
		AND c.platform=s.platform AND c.account_id=receiver.id AND c.group_id=g.id
		AND c.account_epoch=receiver.epoch AND c.account_version=receiver.version
		AND c.applicant_version=u.access_version AND c.friend_flow='incoming_contact' AND c.state='waiting'
		AND c.created_at>=s.created_at AND (c.binding_version=0 OR c.binding_version=b.version)
		AND g.platform=s.platform AND g.isolated_test AND g.native_id=$7
		AND b.active AND b.tenant_id=s.tenant_id AND b.primary_account_id=receiver.id AND b.epoch=$8
		AND o.tenant_id=s.tenant_id AND o.platform=s.platform
		AND o.proof_kind='controlled_test' AND o.state='paused'
		AND w.actor_id=s.actor_id AND w.group_id=g.id AND w.admission_mode='native_debug'
		AND w.actions=ARRAY['friend_delete']::text[] AND w.account_ids=ARRAY[claimant.id]
		AND w.target_ids=ARRAY[receiver.native_uid]::text[] AND w.budget=1
		AND w.used=$9 AND NOT w.cancelled AND w.account_epochs->>claimant.id::text=claimant.epoch::text
		AND w.created_at>=s.created_at AND w.expires_at<=w.created_at+interval '300 seconds'
		AND least(w.expires_at,c.expires_at,s.expires_at)>clock_timestamp()+make_interval(secs=>$10)
		AND cr.enabled AND cr.owner<>'' AND cr.lease_until>clock_timestamp()+interval '3 seconds'
		AND cr.observed_at>clock_timestamp()-interval '15 seconds' AND cr.snapshot->>'state'='connected'
		AND rr.enabled AND rr.owner<>'' AND rr.lease_until>clock_timestamp()+interval '3 seconds'
		AND rr.observed_at>clock_timestamp()-interval '15 seconds' AND rr.snapshot->>'state'='connected'
		FOR SHARE OF s,c,w,claimant,receiver,cr,rr,g,b,o,u,t`,
		req.contactRecovery.actor, req.TestWindowID, req.Account, tenant, group, req.Change.Target,
		req.Change.Group, binding, used, settingFenceDuration(req).Seconds()+1).
		Scan(&out.Scope, &out.ScopeVersion, &out.Challenge, &out.ChallengeVersion, &out.Window,
			&out.Tenant, &out.Account, &out.Receiver, &out.Group,
			&out.NativeSender, &out.NativeGroup, &out.Recipient, &out.BindingVersion, &out.BindingEpoch,
			&out.ReceiverEpoch, &out.ReceiverVersion, &out.SourceOwner, &out.SourceRevision,
			&out.ReceiverOwner, &out.ReceiverRevision)
	if err == pgx.ErrNoRows {
		return out, Denied(409, "native_setting_pending")
	}
	if err != nil {
		return out, err
	}
	if proof != nil && (proof.ReceiverOwner != out.SourceOwner || proof.ReceiverRevision != out.SourceRevision) {
		return out, Denied(409, "native_setting_snapshot_changed")
	}
	return out, nil
}
