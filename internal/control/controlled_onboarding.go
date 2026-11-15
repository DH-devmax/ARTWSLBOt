package control

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/security"
)

const controlledOnboardingMaxMinutes = 6 * 60

type ControlledOnboardingCreate struct {
	Platform          string `json:"platform"`
	AccountID         string `json:"account_id"`
	ClaimantAccountID string `json:"claimant_account_id"`
	GroupID           string `json:"group_id"`
	Minutes           int    `json:"minutes"`
	Key               string `json:"-"`
}

type ControlledOnboardingAccount struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	NativeID string `json:"native_id"`
	Platform string `json:"platform"`
}

type ControlledOnboardingScope struct {
	ID                        string                      `json:"id"`
	ActorID                   string                      `json:"actor_id"`
	TenantID                  string                      `json:"tenant_id"`
	Platform                  string                      `json:"platform"`
	AccountID                 string                      `json:"account_id"`
	ClaimantAccountID         string                      `json:"claimant_account_id"`
	GroupID                   string                      `json:"group_id"`
	Robot                     ControlledOnboardingAccount `json:"robot"`
	Claimant                  ControlledOnboardingAccount `json:"claimant"`
	Group                     GroupBindingGroup           `json:"group"`
	State                     string                      `json:"state"`
	Version                   int64                       `json:"version"`
	BlockedReason             string                      `json:"blocked_reason"`
	ExpiresAt                 time.Time                   `json:"expires_at"`
	NativeAttempts            int                         `json:"native_attempts_used"`
	ModelCalls                int                         `json:"model_calls_used"`
	ReservedUSDT              string                      `json:"reserved_usdt"`
	HumanVerificationRequired bool                        `json:"human_verification_required"`
}

type ControlledOnboardingCandidates struct {
	Accounts []ControlledOnboardingAccount `json:"accounts"`
	Groups   []GroupBindingGroup           `json:"groups"`
}

type ControlledOnboardingList struct {
	Items      []ControlledOnboardingScope    `json:"items"`
	Candidates ControlledOnboardingCandidates `json:"candidates"`
}

type controlledSelection struct {
	Actor                Actor
	Tenant               string
	NativeGroup          string
	Claimant             string
	ActorVersion         int64
	Epoch, Version       int64
	ClaimEpoch, ClaimVer int64
}

func (s *GroupBindingService) controlledOwner(ctx context.Context, tx pgx.Tx, ref SessionRef) (Actor, error) {
	a, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return a, err
	}
	if a.Role != "superadmin" {
		return a, Denied(403, "superadmin_required")
	}
	return a, nil
}

func (s *GroupBindingService) controlledSelection(ctx context.Context, tx pgx.Tx, ref SessionRef, in ControlledOnboardingCreate) (controlledSelection, error) {
	var out controlledSelection
	a, err := s.controlledOwner(ctx, tx, ref)
	if err != nil {
		return out, err
	}
	out.Actor = a
	space, err := OperatorWorkspace(ctx, tx, a.ID)
	if err != nil {
		return out, err
	}
	out.Tenant = space.ID
	err = tx.QueryRow(ctx, `SELECT u.access_version,a.epoch,a.version,c.epoch,c.version,c.native_uid,g.native_id
		FROM users u JOIN accounts a ON a.id=$2 JOIN accounts c ON c.id=$3
		JOIN native_groups g ON g.id=$4
		WHERE u.id=$1 AND a.platform=$5 AND c.platform=$5 AND g.platform=$5 AND g.isolated_test
		AND a.workspace=$5 AND a.purpose='management' AND a.testing_enabled AND c.testing_enabled
		AND a.auth_state='authenticated' AND c.auth_state='authenticated'
		AND a.credential_sealed IS NOT NULL AND c.credential_sealed IS NOT NULL
		AND a.native_uid IS NOT NULL AND c.native_uid IS NOT NULL AND a.native_uid<>c.native_uid
		FOR SHARE OF a,c,u,g`, a.ID, in.AccountID, in.ClaimantAccountID, in.GroupID, in.Platform).
		Scan(&out.ActorVersion, &out.Epoch, &out.Version, &out.ClaimEpoch, &out.ClaimVer, &out.Claimant, &out.NativeGroup)
	if err == pgx.ErrNoRows {
		return out, Denied(409, "controlled_onboarding_selection_unavailable")
	}
	if err != nil {
		return out, err
	}
	_, err = s.connection(ctx, tx, bindingRecord{Platform: in.Platform, Account: in.AccountID,
		Epoch: out.Epoch, AccountVersion: out.Version}, nil)
	return out, err
}

func (s *GroupBindingService) CreateControlledScope(ctx context.Context, ref SessionRef, in ControlledOnboardingCreate) (ControlledOnboardingScope, error) {
	var out ControlledOnboardingScope
	defaultDuration := in.Minutes == 0
	if defaultDuration {
		in.Minutes = controlledOnboardingMaxMinutes
	}
	if !bindingPlatform(in.Platform) || ref.assistantPlatform() != in.Platform || !ValidID(in.AccountID) ||
		!ValidID(in.ClaimantAccountID) || !ValidID(in.GroupID) || in.AccountID == in.ClaimantAccountID ||
		in.Minutes < 1 || in.Minutes > controlledOnboardingMaxMinutes || !onboardingKey(in.Key) {
		return out, Denied(400, "controlled_onboarding_scope_invalid")
	}
	if s.Native == nil || s.Vault == nil {
		return out, Denied(503, "group_binding_native_unavailable")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, err := s.controlledOwner(ctx, tx, ref)
	if err != nil {
		return out, err
	}
	var old string
	var oldHash []byte
	var oldDefault int
	err = tx.QueryRow(ctx, `SELECT id::text,request_hash,request_default_minutes FROM onboarding_test_scopes
		WHERE actor_id=$1 AND create_key=$2`, a.ID, in.Key).Scan(&old, &oldHash, &oldDefault)
	if err == nil && defaultDuration {
		in.Minutes = oldDefault
	}
	raw, _ := json.Marshal(in)
	digest := security.Digest(string(raw))
	if err == nil {
		if !bytes.Equal(digest, oldHash) {
			return out, Denied(409, "idempotency_conflict")
		}
		_ = tx.Rollback(ctx)
		return s.ReadControlledScope(ctx, ref, old)
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	initial, err := s.controlledSelection(ctx, tx, ref, in)
	if err != nil {
		return out, err
	}
	_ = tx.Rollback(ctx)
	scope := GroupBindingNativeScope{Platform: in.Platform, AccountID: in.AccountID,
		AccountEpoch: initial.Epoch, AccountVersion: initial.Version,
		NativeGroupID: initial.NativeGroup, NativeSenderID: initial.Claimant}
	roles, err := s.Native.BindingRoles(ctx, scope)
	if err != nil {
		return out, err
	}
	if roles.Present && !bindingRolesMatch(bindingProof{Group: initial.NativeGroup, Sender: initial.Claimant,
		Owner: roles.NativeOwnerID, SenderRole: roles.SenderRole, RobotRole: roles.RobotRole}, roles) ||
		!roles.Present && (!roles.AbsenceVerified || roles.NativeGroupID != initial.NativeGroup || roles.NativeSenderID != initial.Claimant) {
		return out, Denied(409, "group_binding_native_role_required")
	}
	var ownerHash []byte
	if roles.Present {
		ownerHash = bindingIdentityDigest(in.Platform, in.GroupID, roles.NativeOwnerID)
	} else {
		roles.SenderRole, roles.RobotRole = "", ""
	}
	tx, err = s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return out, err
	}
	if err = s.expireControlledScopesTx(ctx, tx); err != nil {
		return out, err
	}
	current, err := s.controlledSelection(ctx, tx, ref, in)
	if err != nil {
		return out, err
	}
	if initial.Tenant != current.Tenant || initial.ActorVersion != current.ActorVersion ||
		initial.Epoch != current.Epoch || initial.Version != current.Version ||
		initial.ClaimEpoch != current.ClaimEpoch || initial.ClaimVer != current.ClaimVer ||
		initial.Claimant != current.Claimant || initial.NativeGroup != current.NativeGroup {
		return out, Denied(409, "controlled_onboarding_scope_changed")
	}
	if _, err = s.connection(ctx, tx, bindingRecord{Platform: in.Platform, Account: in.AccountID,
		Epoch: current.Epoch, AccountVersion: current.Version}, &roles.GroupBindingConnectionEvidence); err != nil {
		return out, err
	}
	var pending bool
	err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM onboarding_test_scopes
		WHERE state IN ('preparing','active') AND (group_id=$1 OR actor_id=$2 AND platform=$3))`,
		in.GroupID, a.ID, in.Platform).Scan(&pending)
	if err != nil {
		return out, err
	}
	if pending {
		return out, Denied(409, "controlled_onboarding_scope_pending")
	}
	id := NewID()
	_, err = tx.Exec(ctx, `INSERT INTO onboarding_test_scopes(id,actor_id,tenant_id,platform,account_id,
		claimant_account_id,group_id,actor_version,account_epoch,account_version,claimant_epoch,claimant_version,
		claimant_native_id,claimant_role,robot_role,owner_identity_hash,create_key,request_hash,expires_at,request_default_minutes)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,
			clock_timestamp()+$19*interval '1 minute',$20)`,
		id, a.ID, current.Tenant, in.Platform, in.AccountID, in.ClaimantAccountID, in.GroupID, current.ActorVersion,
		current.Epoch, current.Version, current.ClaimEpoch, current.ClaimVer, current.Claimant,
		roles.SenderRole, roles.RobotRole, ownerHash,
		in.Key, digest, in.Minutes, controlledOnboardingMaxMinutes)
	if err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, a, current.Tenant, "controlled_onboarding_scope_created", id,
		map[string]any{"platform": in.Platform, "group_id": in.GroupID, "minutes": in.Minutes}); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return s.ReadControlledScope(ctx, ref, id)
}

// All scope progress reads are local. They never approve a friend or refresh a
// roster and expired rows cannot appear active while cleanup is still pending.
func (s *GroupBindingService) readControlledScope(ctx context.Context, q Queryer, actor, platform, id string) (ControlledOnboardingScope, error) {
	var out ControlledOnboardingScope
	var live bool
	err := q.QueryRow(ctx, `SELECT s.id::text,s.actor_id::text,s.tenant_id::text,s.platform,s.account_id::text,
		s.claimant_account_id::text,s.group_id::text,s.state,s.version,s.blocked_reason,s.expires_at,
		a.id::text,coalesce(nullif(a.native_nickname,''),a.name),coalesce(a.native_uid,''),
		c.id::text,coalesce(nullif(c.native_nickname,''),c.name),coalesce(c.native_uid,''),
		g.id::text,g.name,g.native_id,onboarding_test_scope_active(s.id)
		FROM onboarding_test_scopes s JOIN accounts a ON a.id=s.account_id
		JOIN accounts c ON c.id=s.claimant_account_id JOIN native_groups g ON g.id=s.group_id
		WHERE s.id=$1 AND s.actor_id=$2 AND s.platform=$3`,
		id, actor, platform).Scan(&out.ID, &out.ActorID, &out.TenantID, &out.Platform, &out.AccountID,
		&out.ClaimantAccountID, &out.GroupID, &out.State, &out.Version, &out.BlockedReason, &out.ExpiresAt,
		&out.Robot.ID, &out.Robot.Name, &out.Robot.NativeID, &out.Claimant.ID, &out.Claimant.Name, &out.Claimant.NativeID,
		&out.Group.ID, &out.Group.Name, &out.Group.NativeID, &live)
	if err == pgx.ErrNoRows {
		return out, Denied(404, "controlled_onboarding_scope_not_found")
	}
	if err != nil {
		return out, err
	}
	out.Robot.Platform, out.Claimant.Platform = platform, platform
	if out.State == "active" && !live {
		out.State, out.BlockedReason = "closed", "controlled_onboarding_scope_changed"
		if !out.ExpiresAt.After(time.Now()) {
			out.State, out.BlockedReason = "expired", "controlled_onboarding_scope_expired"
		}
	}
	var reserved int64
	out.NativeAttempts, out.ModelCalls, reserved, err = ReadControlledUsage(ctx, q, id)
	if err != nil {
		return out, err
	}
	out.ReservedUSDT = fmt.Sprintf("%d.%06d", reserved/1_000_000, reserved%1_000_000)
	var pending bool
	if err = q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_binding_friend_attempts a
		JOIN group_binding_challenges c ON c.id=a.challenge_id WHERE c.controlled_scope_id=$1
		AND a.status IN ('submitting','unknown')) OR EXISTS(SELECT 1 FROM customer_group_accept_attempts a
		JOIN group_binding_challenges c ON c.id=a.challenge_id WHERE c.controlled_scope_id=$1
		AND a.status IN ('submitting','unknown')) OR EXISTS(SELECT 1 FROM group_binding_outbound_attempts a
		JOIN group_binding_challenges c ON c.id=a.challenge_id WHERE c.controlled_scope_id=$1
		AND a.status IN ('submitting','unknown'))`, id).Scan(&pending); err != nil {
		return out, err
	}
	if pending {
		out.State, out.BlockedReason = "unknown", "group_binding_operation_unresolved"
	}
	out.HumanVerificationRequired = !live || out.State != "active" || out.BlockedReason != ""
	return out, nil
}

func (s *GroupBindingService) ReadControlledScope(ctx context.Context, ref SessionRef, id string) (ControlledOnboardingScope, error) {
	var out ControlledOnboardingScope
	if !ValidID(id) {
		return out, Denied(404, "controlled_onboarding_scope_not_found")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, err := s.controlledOwner(ctx, tx, ref)
	if err != nil {
		return out, err
	}
	return s.readControlledScope(ctx, tx, a.ID, ref.assistantPlatform(), id)
}

func (s *GroupBindingService) ControlledScopes(ctx context.Context, ref SessionRef, platform string) (ControlledOnboardingList, error) {
	out := ControlledOnboardingList{Items: []ControlledOnboardingScope{}, Candidates: ControlledOnboardingCandidates{
		Accounts: []ControlledOnboardingAccount{}, Groups: []GroupBindingGroup{}}}
	if !bindingPlatform(platform) || ref.assistantPlatform() != platform {
		return out, Denied(400, "invalid_platform")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	a, err := s.controlledOwner(ctx, tx, ref)
	if err != nil {
		return out, err
	}
	rows, err := tx.Query(ctx, `SELECT id::text FROM onboarding_test_scopes WHERE actor_id=$1 AND platform=$2
		ORDER BY created_at DESC,id LIMIT 20`, a.ID, platform)
	if err != nil {
		return out, err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			rows.Close()
			return out, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	for _, id := range ids {
		item, err := s.readControlledScope(ctx, tx, a.ID, platform, id)
		if err != nil {
			return out, err
		}
		out.Items = append(out.Items, item)
	}
	rows, err = tx.Query(ctx, `SELECT id::text,coalesce(nullif(native_nickname,''),name),native_uid,platform
		FROM accounts WHERE platform=$1 AND auth_state='authenticated' AND credential_sealed IS NOT NULL
		AND native_uid IS NOT NULL ORDER BY purpose,id LIMIT 20`, platform)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item ControlledOnboardingAccount
		if err = rows.Scan(&item.ID, &item.Name, &item.NativeID, &item.Platform); err != nil {
			rows.Close()
			return out, err
		}
		out.Candidates.Accounts = append(out.Candidates.Accounts, item)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return out, err
	}
	rows, err = tx.Query(ctx, `SELECT g.id::text,g.name,g.native_id FROM native_groups g
		WHERE g.platform=$1 AND g.isolated_test AND EXISTS(SELECT 1 FROM account_groups ag JOIN accounts a ON a.id=ag.account_id
			WHERE ag.group_id=g.id AND a.platform=$1 AND a.auth_state='authenticated')
		ORDER BY g.name,g.id LIMIT 300`, platform)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var item GroupBindingGroup
		if err = rows.Scan(&item.ID, &item.Name, &item.NativeID); err != nil {
			return out, err
		}
		out.Candidates.Groups = append(out.Candidates.Groups, item)
	}
	return out, rows.Err()
}
