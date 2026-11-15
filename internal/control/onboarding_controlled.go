package control

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

const (
	onboardingControlledSearch = "onboarding_friend_search"
	onboardingControlledSend   = "onboarding_friend_request"
)

type OnboardingControlledWindowInput struct {
	ChallengeID     string `json:"challenge_id"`
	Version         int64  `json:"version"`
	GroupID         string `json:"group_id"`
	Account         string `json:"account"`
	TargetID        string `json:"target_id"`
	Seconds         int    `json:"seconds"`
	Budget          int    `json:"budget"`
	ConfirmIsolated bool   `json:"confirm_isolated"`
	AsClaimant      bool   `json:"as_claimant,omitempty"`
}

type OnboardingControlledWindow struct {
	ID string `json:"id"`
	TestScope
}

type OnboardingControlledTargetInput struct {
	Window  string `json:"test_window_id"`
	Version int64  `json:"version"`
	Account string `json:"account"`
	Key     string `json:"-"`
}

type OnboardingControlledConfirmation struct {
	Window  string `json:"test_window_id"`
	Version int64  `json:"version"`
	Key     string `json:"-"`
}

// Only control constructs this context, after checking a persisted human grant.
// Neither business admission nor model tools can construct or inherit it.
type onboardingControlledKey struct{}

type onboardingControlledAuthorization struct {
	Challenge, Actor, Tenant, Window, Group, NativeGroup string
	Platform, Account, AddAccount, Target                string
	Epoch, AccountVersion, Version, BindingEpoch         int64
	ExpiresAt                                            time.Time
	Claimant                                             *onboardingControlledClaimant `json:",omitempty"`
}

func onboardingControlledContext(ctx context.Context) *onboardingControlledAuthorization {
	out, _ := ctx.Value(onboardingControlledKey{}).(*onboardingControlledAuthorization)
	return out
}

func (a *onboardingControlledAuthorization) matches(c bindingRecord) bool {
	return a.matchesScope(c) &&
		(a.Claimant == nil || a.Claimant.matches(c, a.Group, a.Target))
}

func (a *onboardingControlledAuthorization) matchesRetained(c bindingRecord) bool {
	return a.matchesScope(c) &&
		(a.Claimant == nil || a.Claimant.matchesRetained(c, a.Group, a.Target))
}

func (a *onboardingControlledAuthorization) matchesScope(c bindingRecord) bool {
	return a != nil && c.FriendFlow == "incoming_contact" && c.Mode == "operator_demo" &&
		a.Challenge == c.ID && a.Actor == c.Applicant && a.Tenant == c.Tenant &&
		a.Platform == c.Platform && a.Account == c.Account && a.Epoch == c.Epoch &&
		a.AccountVersion == c.AccountVersion && ValidID(a.Window) && ValidID(a.Group) &&
		a.BindingEpoch > 0 && onboardingLabel(a.NativeGroup, 200) &&
		validBindingAddAccount(a.AddAccount) && onboardingLabel(a.Target, 200)
}

func sameOnboardingControlled(a, b *onboardingControlledAuthorization) bool {
	if a == nil || b == nil {
		return a == b
	}
	if !sameOnboardingClaimant(a.Claimant, b.Claimant) {
		return false
	}
	x, y := *a, *b
	x.Claimant, y.Claimant = nil, nil
	x.ExpiresAt, y.ExpiresAt = time.Time{}, time.Time{}
	return x == y && a.ExpiresAt.Equal(b.ExpiresAt)
}

func onboardingControlledTargets(a *onboardingControlledAuthorization) []string {
	binding := strconv.FormatInt(a.BindingEpoch, 10)
	if a.Claimant != nil {
		binding = a.Claimant.Binding.marker()
	}
	return []string{"onboarding_challenge:" + a.Challenge + ":" + strconv.FormatInt(a.Version, 10),
		"onboarding_account:" + a.AddAccount, "onboarding_target:" + a.Target,
		"onboarding_binding:" + binding}
}

func (s *GroupBindingService) onboardingControlledActor(ctx context.Context, tx pgx.Tx, ref SessionRef, c bindingRecord) (Actor, error) {
	actor, err := s.checkRecord(ctx, tx, ref, c, true)
	if err != nil {
		return actor, err
	}
	if actor.Role != "superadmin" {
		return actor, Denied(403, "superadmin_required")
	}
	if c.Mode != "operator_demo" || c.FriendFlow != "incoming_contact" {
		return actor, Denied(409, "onboarding_controlled_challenge_required")
	}
	return actor, nil
}

func (s *GroupBindingService) onboardingControlledGroupFor(ctx context.Context, tx pgx.Tx, c bindingRecord,
	group string, allowMark bool, auth *onboardingControlledAuthorization) (string, int64, string, error) {
	source := onboardingOutboundSource(c, auth)
	claimant := auth != nil && auth.Claimant != nil
	if claimant {
		if err := s.requireOnboardingClaimant(ctx, tx, c, auth); err != nil {
			return "", 0, "", err
		}
	}
	var native, self string
	var epoch int64
	err := tx.QueryRow(ctx, `SELECT g.native_id,b.epoch,a.native_uid FROM native_groups g
		JOIN group_bindings b ON b.group_id=g.id JOIN accounts a ON a.id=$2
		WHERE g.id=$1 AND g.platform=$3 AND b.active
		AND (NOT $8 AND b.tenant_id=$4 AND b.primary_account_id=a.id OR $8) AND (g.isolated_test OR $5)
		AND a.platform=$3 AND a.workspace=$3 AND a.purpose='management'
		AND a.epoch=$6 AND a.version=$7 AND a.auth_state='authenticated'
		AND a.credential_sealed IS NOT NULL AND a.testing_enabled AND a.connection_enabled
		FOR SHARE OF g,b,a`, group, source.Account, c.Platform, c.Tenant, allowMark, source.Epoch, source.AccountVersion, claimant).
		Scan(&native, &epoch, &self)
	if err == pgx.ErrNoRows {
		return "", 0, "", Denied(409, "native_test_window_invalid")
	}
	if err != nil {
		return "", 0, "", err
	}
	if _, err = s.connection(ctx, tx, source, nil); err != nil {
		return "", 0, "", err
	}
	return native, epoch, self, nil
}

type onboardingControlledRoles struct {
	GroupBindingRoleEvidence
	Recipient *GroupBindingRoleEvidence
}

func (s *GroupBindingService) onboardingControlledRole(ctx context.Context, c bindingRecord,
	a *onboardingControlledAuthorization, self string) (onboardingControlledRoles, error) {
	if s.Native == nil {
		return onboardingControlledRoles{}, Denied(503, "group_binding_outbound_unavailable")
	}
	e, err := s.Native.BindingRoles(ctx, onboardingOutboundSource(c, a).scope(a.NativeGroup, self))
	out := onboardingControlledRoles{GroupBindingRoleEvidence: e}
	if err != nil {
		return out, err
	}
	if !e.Present || e.NativeGroupID != a.NativeGroup || e.NativeSenderID != self ||
		!onboardingLabel(e.NativeOwnerID, 200) || e.SenderRole != e.RobotRole ||
		e.RobotRole != "owner" && e.RobotRole != "admin" {
		return out, Denied(409, "fresh_test_account_role_required")
	}
	if a.Claimant != nil {
		recipient, err := s.Native.BindingRoles(ctx, c.scope(a.NativeGroup, self))
		if err != nil {
			return out, err
		}
		if !recipient.Present || recipient.NativeGroupID != a.NativeGroup ||
			recipient.NativeSenderID != self || recipient.NativeOwnerID != e.NativeOwnerID ||
			recipient.SenderRole != e.SenderRole ||
			recipient.RobotRole != "owner" && recipient.RobotRole != "admin" && recipient.RobotRole != "member" {
			return out, Denied(409, "fresh_test_account_role_required")
		}
		out.Recipient = &recipient
	}
	return out, nil
}

func (s *GroupBindingService) verifyOnboardingControlledRoles(ctx context.Context, tx pgx.Tx, c bindingRecord,
	a *onboardingControlledAuthorization, proof onboardingControlledRoles) error {
	if _, err := s.connection(ctx, tx, onboardingOutboundSource(c, a), &proof.GroupBindingConnectionEvidence); err != nil {
		return err
	}
	if a.Claimant != nil {
		if proof.Recipient == nil {
			return Denied(409, "fresh_test_account_role_required")
		}
		_, err := s.connection(ctx, tx, c, &proof.Recipient.GroupBindingConnectionEvidence)
		return err
	}
	return nil
}

func (s *GroupBindingService) CreateOnboardingControlledWindow(ctx context.Context, ref SessionRef,
	in OnboardingControlledWindowInput) (OnboardingControlledWindow, error) {
	var out OnboardingControlledWindow
	if !ValidID(in.ChallengeID) || !ValidID(in.GroupID) || in.Version < 1 ||
		!validBindingAddAccount(in.Account) || !onboardingLabel(in.TargetID, 200) ||
		in.Seconds < 5 || in.Seconds > 300 || in.Budget < 1 || in.Budget > 10 {
		return out, Denied(400, "invalid_test_scope")
	}
	c, actor, err := s.confirmationRecord(ctx, ref, in.ChallengeID, false)
	if err != nil {
		return out, err
	}
	if actor.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	if c.Mode != "operator_demo" || c.FriendFlow != "incoming_contact" {
		return out, Denied(409, "onboarding_controlled_challenge_required")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	auth := &onboardingControlledAuthorization{Challenge: c.ID, Actor: actor.ID, Tenant: c.Tenant, Window: NewID(),
		Group: in.GroupID, Platform: c.Platform, Account: c.Account, AddAccount: in.Account,
		Target: in.TargetID, Epoch: c.Epoch, AccountVersion: c.AccountVersion, Version: in.Version}
	if in.AsClaimant {
		auth.Claimant, err = s.onboardingClaimant(ctx, tx, c, in.GroupID)
		if err != nil {
			return out, err
		}
		if in.Budget != 1 || in.TargetID != auth.Claimant.Recipient {
			return out, Denied(409, "onboarding_controlled_target_changed")
		}
	}
	native, epoch, self, err := s.onboardingControlledGroupFor(ctx, tx, c, in.GroupID, in.ConfirmIsolated, auth)
	if err != nil {
		return out, err
	}
	if err = tx.Rollback(ctx); err != nil {
		return out, err
	}
	auth.NativeGroup, auth.BindingEpoch = native, epoch
	proof, err := s.onboardingControlledRole(ctx, c, auth, self)
	if err != nil {
		return out, err
	}
	tx, err = s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	c, err = loadBindingRecord(ctx, tx, c.ID, true)
	if err != nil {
		return out, err
	}
	if actor, err = s.onboardingControlledActor(ctx, tx, ref, c); err != nil {
		return out, err
	}
	if err = bindingFriendPending(c, in.Version); err != nil {
		return out, err
	}
	native, epoch, _, err = s.onboardingControlledGroupFor(ctx, tx, c, in.GroupID, in.ConfirmIsolated, auth)
	if err != nil {
		return out, err
	}
	if native != auth.NativeGroup || epoch != auth.BindingEpoch || !auth.matches(c) {
		return out, Denied(409, "native_test_window_invalid")
	}
	source := onboardingOutboundSource(c, auth)
	if err = s.verifyOnboardingControlledRoles(ctx, tx, c, auth, proof); err != nil {
		return out, err
	}
	if in.TargetID == self {
		return out, Denied(409, "group_binding_outbound_self")
	}
	if in.ConfirmIsolated {
		if _, err = tx.Exec(ctx, `UPDATE native_groups SET isolated_test=true WHERE id=$1`, in.GroupID); err != nil {
			return out, err
		}
	}
	epochs, _ := json.Marshal(map[string]int64{source.Account: source.Epoch})
	expiry := c.ExpiresAt
	if auth.Claimant != nil && auth.Claimant.ExpiresAt.Before(expiry) {
		expiry = auth.Claimant.ExpiresAt
	}
	err = tx.QueryRow(ctx, `INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,
		account_epochs,expires_at,budget,admission_mode)
		VALUES($1,$2,$3,ARRAY[$4::uuid],$5,$6,$7,least($8,now()+make_interval(secs=>$9)),$10,'native_debug')
		RETURNING id::text,expires_at`, auth.Window, actor.ID, in.GroupID, source.Account, onboardingControlledTargets(auth),
		[]string{onboardingControlledSearch, onboardingControlledSend}, epochs, expiry, in.Seconds, in.Budget).
		Scan(&out.ID, &out.ExpiresAt)
	if err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, actor, c.Tenant, "onboarding_controlled_window_created", out.ID,
		map[string]any{"challenge_id": c.ID, "group_id": in.GroupID, "platform": c.Platform,
			"admission_mode": "native_debug", "budget": in.Budget, "seconds": in.Seconds,
			"as_claimant": in.AsClaimant}); err != nil {
		return out, err
	}
	out.TestScope = TestScope{GroupID: in.GroupID, NativeGroup: native, AccountEpoch: source.Epoch,
		BindingEpoch: epoch, ExpiresAt: out.ExpiresAt, Remaining: in.Budget, AdmissionMode: "native_debug"}
	return out, tx.Commit(ctx)
}

func (s *GroupBindingService) verifyOnboardingControlled(ctx context.Context, tx pgx.Tx, c bindingRecord,
	a *onboardingControlledAuthorization, send, reserved bool) (TestScope, error) {
	var out TestScope
	if !a.matches(c) || a.Version != c.Version {
		return out, Denied(409, "native_test_window_invalid")
	}
	action := onboardingControlledSearch
	source := onboardingOutboundSource(c, a)
	if send {
		action = onboardingControlledSend
	}
	var allowed bool
	err := tx.QueryRow(ctx, `SELECT w.expires_at,greatest(0,w.budget-w.used),
		w.actor_id=$2 AND w.group_id=$3 AND w.account_ids=ARRAY[$4::uuid]
		AND w.account_epochs->>$4::uuid::text=$5::bigint::text AND w.admission_mode='native_debug'
		AND w.actions=ARRAY['onboarding_friend_search','onboarding_friend_request']::text[]
		AND $6=ANY(w.actions) AND w.target_ids=$7 AND NOT w.cancelled
		AND w.expires_at>clock_timestamp()+interval '4 seconds'
		AND ($8 OR w.used<w.budget) AND (NOT $8 OR w.used>0)
		AND u.role='superadmin' AND NOT u.disabled
		FROM test_windows w JOIN users u ON u.id=w.actor_id WHERE w.id=$1 FOR UPDATE OF w FOR SHARE OF u`,
		a.Window, a.Actor, a.Group, source.Account, source.Epoch, action, onboardingControlledTargets(a), reserved).
		Scan(&out.ExpiresAt, &out.Remaining, &allowed)
	if err == pgx.ErrNoRows || err == nil && (!allowed || !out.ExpiresAt.Equal(a.ExpiresAt)) {
		return out, Denied(409, "native_test_window_invalid")
	}
	if err != nil {
		return out, err
	}
	native, epoch, _, err := s.onboardingControlledGroupFor(ctx, tx, c, a.Group, false, a)
	if err != nil {
		return out, err
	}
	if native != a.NativeGroup || epoch != a.BindingEpoch {
		return out, Denied(409, "native_test_window_invalid")
	}
	out.GroupID, out.NativeGroup, out.BindingEpoch, out.AccountEpoch, out.AdmissionMode =
		a.Group, native, epoch, source.Epoch, "native_debug"
	return out, nil
}

func (s *GroupBindingService) onboardingControlledContextFor(ctx context.Context, ref SessionRef,
	id, window string, version int64, send bool) (context.Context, error) {
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return ctx, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	c, err := loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return ctx, err
	}
	if _, err = s.onboardingControlledActor(ctx, tx, ref, c); err != nil {
		return ctx, err
	}
	var a onboardingControlledAuthorization
	var targets, accounts []string
	err = tx.QueryRow(ctx, `SELECT group_id::text,target_ids,expires_at,account_ids::text[] FROM test_windows
		WHERE id=$1 AND actor_id=$2`, window, c.Applicant).Scan(&a.Group, &targets, &a.ExpiresAt, &accounts)
	if err == pgx.ErrNoRows {
		return ctx, Denied(409, "native_test_window_invalid")
	}
	if err != nil {
		return ctx, err
	}
	if len(targets) != 4 || len(accounts) != 1 || !strings.HasPrefix(targets[1], "onboarding_account:") ||
		!strings.HasPrefix(targets[2], "onboarding_target:") || c.Version != version {
		return ctx, Denied(409, "native_test_window_invalid")
	}
	a.Challenge, a.Actor, a.Tenant, a.Window = c.ID, c.Applicant, c.Tenant, window
	a.Platform, a.Account, a.Epoch, a.AccountVersion, a.Version = c.Platform, c.Account, c.Epoch, c.AccountVersion, version
	a.AddAccount, a.Target = strings.TrimPrefix(targets[1], "onboarding_account:"), strings.TrimPrefix(targets[2], "onboarding_target:")
	if accounts[0] != c.Account {
		a.Claimant, err = s.onboardingClaimant(ctx, tx, c, a.Group)
		if err != nil || a.Claimant.Account != accounts[0] {
			return ctx, Denied(409, "native_test_window_invalid")
		}
	}
	a.NativeGroup, a.BindingEpoch, _, err = s.onboardingControlledGroupFor(ctx, tx, c, a.Group, false, &a)
	if err != nil {
		return ctx, err
	}
	// A replay recovers its durable record even after budget exhaustion; final
	// submission still verifies the original reservation and current grant.
	reserved := false
	if old, readErr := readBindingOutbound(ctx, tx, c.ID); readErr == nil {
		reserved = send && old.Window == window && old.Used == 1
	} else if readErr != pgx.ErrNoRows {
		return ctx, readErr
	}
	if _, err = s.verifyOnboardingControlled(ctx, tx, c, &a, send, reserved); err != nil {
		return ctx, err
	}
	if err = tx.Commit(ctx); err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, onboardingControlledKey{}, &a), nil
}

func (s *GroupBindingService) authorizeOutboundLookup(ctx context.Context, ref SessionRef, c bindingRecord) (*onboardingControlledRoles, error) {
	a := onboardingControlledContext(ctx)
	if !a.matches(c) {
		return nil, Denied(409, "group_binding_outbound_controlled_only")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = s.onboardingControlledActor(ctx, tx, ref, c); err != nil {
		return nil, err
	}
	if _, err = s.verifyOnboardingControlled(ctx, tx, c, a, false, false); err != nil {
		return nil, err
	}
	_, _, self, err := s.onboardingControlledGroupFor(ctx, tx, c, a.Group, false, a)
	if err != nil {
		return nil, err
	}
	if err = tx.Commit(ctx); err != nil {
		return nil, err
	}
	proof, err := s.onboardingControlledRole(ctx, c, a, self)
	if err != nil {
		return nil, err
	}
	tx, err = s.DB.Platform.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	err = s.verifyOnboardingControlledRoles(ctx, tx, c, a, proof)
	return &proof, err
}

func (s *GroupBindingService) controlledOutboundLookup(ctx context.Context, ref SessionRef, original bindingRecord,
	proof onboardingControlledRoles, lookup func(context.Context) error) error {
	if lookup == nil {
		return Denied(409, "native_test_window_invalid")
	}
	// The read queue invokes this fence after waiting. Hold the grant and scope
	// locks for one bounded lookup so revocation cannot race its dispatch.
	work, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := s.DB.Platform.Begin(work)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(work))
	c, err := loadBindingRecord(work, tx, original.ID, true)
	if err != nil {
		return err
	}
	if _, err = s.onboardingControlledActor(work, tx, ref, c); err != nil {
		return err
	}
	if err = outboundPendingContext(work, c, original.Version); err != nil {
		return err
	}
	if err = requireIncomingContactResolved(work, tx, c.ID); err != nil {
		return err
	}
	if _, err = s.verifyOnboardingControlled(work, tx, c, onboardingControlledContext(work), false, false); err != nil {
		return err
	}
	if err = s.verifyOnboardingControlledRoles(work, tx, c, onboardingControlledContext(work), proof); err != nil {
		return err
	}
	if err = work.Err(); err != nil {
		return err
	}
	return lookup(work)
}

func (s *GroupBindingService) PrepareControlledOutboundFriend(ctx context.Context, ref SessionRef, id string,
	in OnboardingControlledTargetInput) (GroupBindingChallenge, error) {
	if !ValidID(id) || !ValidID(in.Window) || in.Version < 1 || !onboardingKey(in.Key) || !validBindingAddAccount(in.Account) {
		return GroupBindingChallenge{}, Denied(400, "group_binding_outbound_invalid")
	}
	work, err := s.onboardingControlledContextFor(ctx, ref, id, in.Window, in.Version, false)
	if err != nil {
		return GroupBindingChallenge{}, err
	}
	return s.PrepareOutboundFriend(work, ref, id, GroupBindingFriendTargetInput{Version: in.Version, Account: in.Account, Key: in.Key})
}

func (s *GroupBindingService) SendControlledOutboundFriend(ctx context.Context, ref SessionRef, id string,
	in OnboardingControlledConfirmation) (GroupBindingChallenge, error) {
	if !ValidID(id) || !ValidID(in.Window) || in.Version < 1 || !onboardingKey(in.Key) {
		return GroupBindingChallenge{}, Denied(400, "group_binding_outbound_invalid")
	}
	work, err := s.onboardingControlledContextFor(ctx, ref, id, in.Window, in.Version, true)
	if err != nil {
		return GroupBindingChallenge{}, err
	}
	return s.SendOutboundFriend(work, ref, id, GroupBindingConfirmation{Version: in.Version, Key: in.Key})
}

func (s *GroupBindingService) recordControlledLookupFailure(ctx context.Context, ref SessionRef,
	id string, version int64, cause error) error {
	var failure *GroupBindingLookupFailure
	if !errors.As(cause, &failure) {
		return cause
	}
	d, ok := SafeGroupBindingLookupDiagnostic(failure.Diagnostic)
	if !ok {
		return failure.Failure
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	c, err := loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return err
	}
	actor, err := s.onboardingControlledActor(ctx, tx, ref, c)
	if err != nil {
		return err
	}
	if err = outboundPendingContext(ctx, c, version); err != nil {
		return err
	}
	if _, err = s.verifyOnboardingControlled(ctx, tx, c, onboardingControlledContext(ctx), false, false); err != nil {
		return err
	}
	if d.Platform != c.Platform {
		return Denied(409, "group_binding_outbound_scope_changed")
	}
	if err = onboardingAudit(ctx, tx, actor, c.Tenant, "group_binding_outbound_lookup_failed", id, map[string]any{
		"version": d.Version, "platform": d.Platform, "stage": d.Stage, "category": d.Category,
		"provider_code": d.ProviderCode, "reason": d.Reason, "observed_at": d.ObservedAt,
		"retry_after_seconds": d.RetryAfterSeconds,
	}); err != nil {
		return err
	}
	if err = tx.Commit(ctx); err != nil {
		return err
	}
	return &GroupBindingLookupFailure{Failure: failure.Failure, Diagnostic: d}
}
