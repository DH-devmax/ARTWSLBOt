package control

import (
	"context"
	"slices"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/database"
	"wangshangliao/internal/security"
)

type GroupBindingService struct {
	DB      *database.DB
	Vault   *security.Vault
	Catalog []PermissionSpec
	Native  GroupBindingNativePort
	Decode  OnboardingDecode
}

type GroupBindingCreate struct {
	Platform          string `json:"platform"`
	TargetTenantID    string `json:"target_tenant_id,omitempty"`
	RestoreGroupID    string `json:"restore_group_id,omitempty"`
	BindingVersion    int64  `json:"binding_version,omitempty"`
	ControlledScopeID string `json:"controlled_scope_id,omitempty"`
	Key               string `json:"-"`
}

type GroupBindingRobot struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	NativeID     string `json:"native_id"`
	AddAccount   string `json:"add_account"`
	AddAvailable bool   `json:"add_available"`
	AddReason    string `json:"add_reason,omitempty"`
}

type GroupBindingGroup struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	NativeID string `json:"native_id"`
}

type GroupBindingChallenge struct {
	ID                  string                      `json:"id"`
	Platform            string                      `json:"platform"`
	Mode                string                      `json:"mode"`
	Customer            GroupBindingCustomer        `json:"customer"`
	Scope               GroupBindingViewScope       `json:"scope"`
	State               string                      `json:"state"`
	Version             int64                       `json:"version"`
	ExpiresAt           time.Time                   `json:"expires_at"`
	Robot               GroupBindingRobot           `json:"robot"`
	Group               *GroupBindingGroup          `json:"group,omitempty"`
	GroupChallenge      string                      `json:"group_challenge,omitempty"`
	PrivateChallenge    string                      `json:"private_challenge,omitempty"`
	FriendChallenge     string                      `json:"friend_challenge,omitempty"`
	FriendAttempt       *GroupBindingFriendAttempt  `json:"friend_attempt,omitempty"`
	FriendFlow          string                      `json:"friend_flow"`
	FriendOutbound      *GroupBindingOutbound       `json:"friend_outbound,omitempty"`
	FriendContact       *GroupBindingContact        `json:"friend_contact,omitempty"`
	CanVerifyFriend     bool                        `json:"can_verify_friend"`
	IdentityProven      bool                        `json:"identity_proven"`
	ProofRole           string                      `json:"proof_role"`
	CanConfirm          bool                        `json:"can_confirm"`
	CanCancel           bool                        `json:"can_cancel"`
	CanAcceptInvitation bool                        `json:"can_accept_invitation"`
	BlockedReason       string                      `json:"blocked_reason"`
	AcceptAttempt       *CustomerGroupAcceptAttempt `json:"accept_attempt,omitempty"`
	TransferRequired    bool                        `json:"transfer_required"`
	Transfer            *GroupBindingTransfer       `json:"transfer,omitempty"`
	ControlledScope     *ControlledOnboardingScope  `json:"controlled_scope,omitempty"`
	ProofKind           string                      `json:"proof_kind"`
}

type GroupBindingTransfer struct {
	SourceTenantID   string `json:"source_tenant_id"`
	SourceTenantName string `json:"source_tenant_name"`
	BindingVersion   int64  `json:"binding_version"`
}

type GroupBindingConfirmation struct {
	Version int64  `json:"version"`
	Key     string `json:"-"`
}

type bindingRecord struct {
	ID, Tenant, Applicant, Platform, Account, State, Group string
	Mode                                                   string
	FriendFlow                                             string
	ControlledScope                                        string
	Epoch, AccountVersion, ApplicantVersion, Version       int64
	BindingVersion                                         int64
	ProofEvent                                             *int64
	Proof, TokenHash, PrivateTokenHash                     []byte
	Identity                                               []byte
	ProofRole, ConfirmKey                                  string
	TransferFrom, TransferReviewKey, ReviewedBy            string
	ExpiresAt, ReservationUntil, CreatedAt                 time.Time
}

type bindingProof struct {
	Group, Sender, Owner, SenderRole, RobotRole string
	Event                                       int64
}

// Read is database-only. Page refresh, polling and reconnect cannot read native
// directories, accept invitations, consume a proof or create a binding.
func (s *GroupBindingService) Read(ctx context.Context, ref SessionRef, id string) (GroupBindingChallenge, error) {
	var out GroupBindingChallenge
	if !ValidID(id) {
		return out, Denied(404, "group_binding_challenge_not_found")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, err := sessionActor(ctx, tx, ref, false)
	if err != nil {
		return out, err
	}
	if a.Role != "superadmin" {
		a, err = onboardingOwner(ctx, tx, ref, false)
		if err != nil {
			return out, err
		}
	}
	return s.readTx(ctx, tx, ref, a, id)
}

func (s *GroupBindingService) readTx(ctx context.Context, tx pgx.Tx, ref SessionRef, a Actor, id string) (GroupBindingChallenge, error) {
	var out GroupBindingChallenge
	var group GroupBindingGroup
	var applicant string
	var expired, changed, pending bool
	err := tx.QueryRow(ctx, `SELECT c.id::text,c.platform,c.state,c.version,c.expires_at,
		a.id::text,coalesce(nullif(a.native_nickname,''),a.name),coalesce(a.native_uid,''),
		coalesce(g.id::text,''),coalesce(g.native_id,''),coalesce(g.name,''),c.applicant_id::text,c.blocked_reason,
		c.expires_at<=clock_timestamp(),a.epoch<>c.account_epoch OR a.version<>c.account_version OR u.access_version<>c.applicant_version
			OR u.disabled OR NOT (c.applicant_mode='operator_demo' AND u.role='superadmin'
				OR c.applicant_mode='tenant_business' AND u.role='user' AND u.tenant_id=c.tenant_id
					AND u.tenant_role='owner' AND t.owner_id=u.id)
			OR NOT (workspace_customer_owner(t.id) OR workspace_is_demo(t.id,u.id)),
		EXISTS(SELECT 1 FROM customer_group_accept_attempts ca WHERE ca.challenge_id=c.id AND ca.status IN ('prepared','submitting','unknown')),
		c.identity_sealed IS NOT NULL,coalesce(c.proof_role,''),c.transfer_from_tenant_id IS NOT NULL,
		c.applicant_mode,t.id::text,t.name,c.friend_flow
		FROM group_binding_challenges c JOIN accounts a ON a.id=c.account_id JOIN users u ON u.id=c.applicant_id
		JOIN tenants t ON t.id=c.tenant_id
		LEFT JOIN native_groups g ON g.id=c.group_id WHERE c.id=$1 AND c.platform=$2
		AND (t.space_kind<>'operator_demo' OR t.operator_owner_id=$4)
		AND ($3 OR c.applicant_id=$4 AND c.tenant_id=nullif($5,'')::uuid)`,
		id, ref.assistantPlatform(), a.Role == "superadmin", a.ID, a.TenantID).Scan(&out.ID, &out.Platform, &out.State,
		&out.Version, &out.ExpiresAt, &out.Robot.ID, &out.Robot.Name, &out.Robot.NativeID,
		&group.ID, &group.NativeID, &group.Name, &applicant, &out.BlockedReason, &expired, &changed, &pending,
		&out.IdentityProven, &out.ProofRole, &out.TransferRequired, &out.Mode, &out.Customer.TenantID, &out.Customer.Name,
		&out.FriendFlow)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "group_binding_challenge_not_found")
	}
	if err != nil {
		return out, err
	}
	out.Scope = GroupBindingViewScope{Platform: out.Platform, ActorID: applicant, TenantID: out.Customer.TenantID}
	var scopeID string
	if err = tx.QueryRow(ctx, `SELECT coalesce(controlled_scope_id::text,'') FROM group_binding_challenges WHERE id=$1`, id).Scan(&scopeID); err != nil {
		return out, err
	}
	out.ProofKind = "authenticated_message"
	if scopeID != "" {
		scope, scopeErr := s.readControlledScope(ctx, tx, applicant, out.Platform, scopeID)
		if scopeErr != nil {
			return out, scopeErr
		}
		out.ControlledScope, out.ProofKind = &scope, "controlled_test"
		if scope.State != "active" {
			changed = true
			out.BlockedReason = scope.BlockedReason
		}
	}
	if err = s.robotAddingProjection(ctx, tx, out.Platform, &out.Robot); err != nil {
		return out, err
	}
	if group.ID != "" {
		out.Group = &group
	}
	if out.TransferRequired && a.Role == "superadmin" {
		out.Transfer = &GroupBindingTransfer{}
		if err = tx.QueryRow(ctx, `SELECT t.id::text,t.name,c.binding_version
			FROM group_binding_challenges c JOIN tenants t ON t.id=c.transfer_from_tenant_id
			WHERE c.id=$1`, id).Scan(&out.Transfer.SourceTenantID, &out.Transfer.SourceTenantName, &out.Transfer.BindingVersion); err != nil {
			return out, err
		}
	}
	if expired && (out.State == "waiting" || out.State == "proven") && !pending {
		out.State = "expired"
	}
	if changed && out.State != "bound" {
		if out.BlockedReason == "" {
			out.BlockedReason = "group_binding_scope_changed"
		}
	}
	own := applicant == a.ID && !changed
	out.CanConfirm = own && !expired && out.State == "proven" && !pending
	out.CanCancel = own && (out.State == "waiting" || out.State == "proven" || out.State == "review")
	out.CanAcceptInvitation = own && !expired && out.State == "waiting" && out.IdentityProven && !pending && s.Native != nil
	attempt, _, err := readCustomerAccept(ctx, tx, id, "")
	if err == nil {
		out.AcceptAttempt = &attempt
		out.CanAcceptInvitation = false
	} else if err != pgx.ErrNoRows {
		return out, err
	}
	if err = s.readFriendChallenge(ctx, tx, id, a.ID, &out); err != nil {
		return out, err
	}
	if err = s.readOutboundChallenge(ctx, tx, a.ID, &out); err != nil {
		return out, err
	}
	if err = s.readContactChallenge(ctx, tx, a.ID, &out); err != nil {
		return out, err
	}
	if changed || expired {
		out.CanVerifyFriend, out.CanConfirm, out.CanAcceptInvitation = false, false, false
	}
	return out, nil
}

func (s *GroupBindingService) owner(ctx context.Context, tx pgx.Tx, ref SessionRef, lock bool) (Actor, error) {
	a, err := sessionActor(ctx, tx, ref, lock)
	if err != nil {
		return a, err
	}
	if !bindingPlatform(ref.assistantPlatform()) {
		return a, Denied(400, "invalid_platform")
	}
	if a.Role == "superadmin" {
		return a, nil
	}
	a, err = onboardingOwner(ctx, tx, ref, lock)
	if err != nil {
		return a, err
	}
	permissions, err := EffectivePermissions(ctx, tx, a, s.Catalog)
	if err != nil {
		return a, err
	}
	if !slices.Contains(permissions, "claims:create") {
		return a, Denied(403, "permission_denied")
	}
	return a, nil
}

func loadBindingRecord(ctx context.Context, tx pgx.Tx, id string, lock bool) (bindingRecord, error) {
	var c bindingRecord
	sql := `SELECT id::text,tenant_id::text,applicant_id::text,platform,account_id::text,state,
		coalesce(group_id::text,''),account_epoch,account_version,applicant_version,version,binding_version,
		proof_event_id,proof_sealed,token_hash,private_token_hash,coalesce(proof_role,''),coalesce(confirm_key,''),
		expires_at,reservation_until,created_at,identity_sealed,coalesce(transfer_from_tenant_id::text,''),
		coalesce(transfer_review_key,''),coalesce(reviewed_by::text,''),applicant_mode,friend_flow,
		coalesce(controlled_scope_id::text,'') FROM group_binding_challenges WHERE id=$1`
	if lock {
		sql += ` FOR UPDATE`
	}
	err := tx.QueryRow(ctx, sql, id).Scan(&c.ID, &c.Tenant, &c.Applicant, &c.Platform, &c.Account, &c.State,
		&c.Group, &c.Epoch, &c.AccountVersion, &c.ApplicantVersion, &c.Version, &c.BindingVersion,
		&c.ProofEvent, &c.Proof, &c.TokenHash, &c.PrivateTokenHash, &c.ProofRole, &c.ConfirmKey, &c.ExpiresAt, &c.ReservationUntil, &c.CreatedAt, &c.Identity,
		&c.TransferFrom, &c.TransferReviewKey, &c.ReviewedBy, &c.Mode, &c.FriendFlow, &c.ControlledScope)
	if err == pgx.ErrNoRows {
		return c, Denied(404, "group_binding_challenge_not_found")
	}
	return c, err
}

func (s *GroupBindingService) checkRecord(ctx context.Context, tx pgx.Tx, ref SessionRef, c bindingRecord, lock bool) (Actor, error) {
	a, err := s.owner(ctx, tx, ref, lock)
	if err != nil {
		return a, err
	}
	if !bindingRecordMatches(c, a, ref.assistantPlatform()) {
		return a, Denied(404, "group_binding_challenge_not_found")
	}
	return a, bindingApplicant(ctx, tx, c, lock)
}

func bindingApplicant(ctx context.Context, tx pgx.Tx, c bindingRecord, lock bool) error {
	sql := `SELECT u.id::text FROM users u JOIN tenants t ON t.id=$2 JOIN users owner ON owner.id=t.owner_id
		WHERE u.id=$1 AND NOT u.disabled AND u.access_version=$3
		AND ($4='operator_demo' AND u.role='superadmin'
				AND (workspace_customer_owner(t.id) OR workspace_is_demo(t.id,u.id))
			OR $4='tenant_business' AND u.role='user' AND u.tenant_id=t.id
				AND u.tenant_role='owner' AND t.owner_id=u.id AND workspace_customer_owner(t.id))`
	if lock {
		sql += ` FOR SHARE OF u,t,owner`
	}
	var id string
	err := tx.QueryRow(ctx, sql, c.Applicant, c.Tenant, c.ApplicantVersion, c.Mode).Scan(&id)
	if err == pgx.ErrNoRows {
		return Denied(409, "group_binding_scope_changed")
	}
	return err
}

func bindingPlatform(platform string) bool { return platform == "wsl" || platform == "lajiaoliao" }

// Called with advisory lock 798423. Unknown submissions retain their slot after
// their challenge expires. A retained active binding does not consume two slots.
func bindingCapacity(ctx context.Context, tx pgx.Tx, tenant, platform, account, group, challenge string) error {
	mode := "tenant_business"
	if challenge != "" {
		c, err := loadBindingRecord(ctx, tx, challenge, false)
		if err != nil {
			return err
		}
		if c.Tenant != tenant || c.Platform != platform || c.Account != account || c.Group != "" && c.Group != group {
			return Denied(409, "group_binding_scope_changed")
		}
		if c.Mode == "operator_demo" {
			if err = bindingApplicant(ctx, tx, c, true); err != nil {
				return err
			}
			mode = c.Mode
		}
	}
	return bindingCapacityMode(ctx, tx, tenant, platform, account, group, challenge, mode)
}

// The mode comes from the live website actor at creation, or an immutable
// challenge at subsequent fences. It is never a request-selected exemption.
func bindingCapacityMode(ctx context.Context, tx pgx.Tx, tenant, platform, account, group, challenge, mode string) error {
	if mode != "tenant_business" && mode != "operator_demo" {
		return Denied(409, "group_binding_scope_changed")
	}
	var limit, used, reserved, total, globalReserved, accountUsed, accountReserved int
	if mode == "tenant_business" {
		err := tx.QueryRow(ctx, `SELECT p.group_limit FROM subscriptions sub JOIN plans p ON p.id=sub.plan_id
			WHERE sub.tenant_id=$1 AND sub.enabled AND sub.expires_at>clock_timestamp()+interval '3 seconds'
			AND $2=ANY(p.allowed_platforms) FOR SHARE OF sub,p`, tenant, platform).Scan(&limit)
		if err == pgx.ErrNoRows {
			return Denied(409, "active_subscription_required")
		}
		if err != nil {
			return err
		}
	}
	err := tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE tenant_id=$1),count(*),
		count(*) FILTER(WHERE primary_account_id::text=$3 OR ai_account_id::text=$3)
		FROM group_bindings WHERE active AND group_id::text<>$2`, tenant, group, account).
		Scan(&used, &total, &accountUsed)
	if err != nil {
		return err
	}
	err = tx.QueryRow(ctx, `SELECT count(*) FILTER(WHERE c.tenant_id=$1 AND NOT EXISTS(
			SELECT 1 FROM group_bindings b WHERE b.active AND b.group_id=c.group_id AND b.tenant_id=c.tenant_id)),
		count(*) FILTER(WHERE NOT EXISTS(SELECT 1 FROM group_bindings b WHERE b.active AND b.group_id=c.group_id)),
		count(*) FILTER(WHERE c.account_id::text=$3 AND NOT EXISTS(SELECT 1 FROM group_bindings b
			WHERE b.active AND b.group_id=c.group_id AND (b.primary_account_id=c.account_id OR b.ai_account_id=c.account_id)))
		FROM group_binding_challenges c
		WHERE c.id::text<>$4 AND ($2='' OR c.group_id IS DISTINCT FROM nullif($2,'')::uuid)
		AND ((c.state IN ('waiting','proven','review') AND c.reservation_until>clock_timestamp())
			OR EXISTS(SELECT 1 FROM customer_group_accept_attempts a WHERE a.challenge_id=c.id AND a.status IN ('prepared','submitting','unknown')))`,
		tenant, group, account, challenge).Scan(&reserved, &globalReserved, &accountReserved)
	if err != nil {
		return err
	}
	if mode == "tenant_business" && used+reserved >= limit || total+globalReserved >= MaxAuthorizedGroups {
		return Denied(409, "group_quota_exceeded")
	}
	if account != "" && accountUsed+accountReserved >= MaxGroupsPerAccount {
		return Denied(409, "account_group_limit_reached")
	}
	return nil
}

func expireBindingChallenges(ctx context.Context, tx pgx.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE group_binding_challenges c SET state='expired',version=version+1
		WHERE state IN ('waiting','proven','review') AND reservation_until<=clock_timestamp()
		AND NOT EXISTS(SELECT 1 FROM customer_group_accept_attempts a WHERE a.challenge_id=c.id
			AND a.status IN ('prepared','submitting','unknown'))
		AND NOT group_binding_friend_pending(c.id)`)
	return err
}

func (c bindingRecord) scope(group, sender string) GroupBindingNativeScope {
	return GroupBindingNativeScope{Platform: c.Platform, AccountID: c.Account, AccountEpoch: c.Epoch,
		AccountVersion: c.AccountVersion, NativeGroupID: group, NativeSenderID: sender}
}
