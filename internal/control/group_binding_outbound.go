package control

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

type bindingOutboundPayload struct {
	Challenge, Actor, Tenant  string
	Version, ApplicantVersion int64
	Request                   GroupBindingOutboundRequest
	Connection                GroupBindingConnectionEvidence
	Controlled                *onboardingControlledAuthorization `json:",omitempty"`
}

type bindingOutboundRecord struct {
	ID, Challenge, Actor, Tenant, Platform, Account, Status, Code, PrepareKey, SendKey string
	Epoch, AccountVersion, ApplicantVersion, Version, Gate                             int64
	Used                                                                               int
	Payload                                                                            []byte
	ExpiresAt                                                                          time.Time
	Window                                                                             string
	ProviderCode                                                                       *int
	ProviderCategory                                                                   string
	ProviderObservedAt                                                                 *time.Time
}

func validBindingAddAccount(value string) bool {
	if len(value) < 3 || len(value) > 64 || strings.TrimSpace(value) != value {
		return false
	}
	for _, ch := range value {
		if !(ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '-') {
			return false
		}
	}
	return true
}

func readBindingOutbound(ctx context.Context, q Queryer, challenge string) (bindingOutboundRecord, error) {
	var out bindingOutboundRecord
	err := q.QueryRow(ctx, `SELECT id::text,challenge_id::text,actor_id::text,tenant_id::text,
		platform,account_id::text,status,result_code,prepare_key,coalesce(send_key,''),
		account_epoch,account_version,applicant_version,challenge_version,gate_version,attempts_used,
		payload_sealed,expires_at,coalesce(test_window_id::text,''),provider_code,coalesce(provider_category,''),provider_observed_at
		FROM group_binding_outbound_attempts WHERE challenge_id=$1`, challenge).
		Scan(&out.ID, &out.Challenge, &out.Actor, &out.Tenant, &out.Platform, &out.Account, &out.Status, &out.Code,
			&out.PrepareKey, &out.SendKey, &out.Epoch, &out.AccountVersion, &out.ApplicantVersion, &out.Version,
			&out.Gate, &out.Used, &out.Payload, &out.ExpiresAt, &out.Window,
			&out.ProviderCode, &out.ProviderCategory, &out.ProviderObservedAt)
	return out, err
}

func (s *GroupBindingService) openBindingOutbound(record bindingOutboundRecord, c bindingRecord) (bindingOutboundPayload, error) {
	var p bindingOutboundPayload
	raw, err := s.Vault.Open("group-binding-outbound:"+record.ID, record.Payload)
	if err != nil {
		return p, err
	}
	defer clear(raw)
	if json.Unmarshal(raw, &p) != nil {
		return p, Denied(409, "group_binding_outbound_scope_changed")
	}
	source := onboardingOutboundSource(c, p.Controlled)
	if p.Challenge != c.ID || p.Actor != c.Applicant || p.Tenant != c.Tenant ||
		p.Version != record.Version || p.ApplicantVersion != c.ApplicantVersion ||
		record.Challenge != c.ID || record.Actor != c.Applicant || record.Tenant != c.Tenant ||
		record.Platform != c.Platform || record.Account != source.Account || record.Epoch != source.Epoch ||
		record.AccountVersion != source.AccountVersion || record.ApplicantVersion != c.ApplicantVersion ||
		p.Request.OperationID != record.ID || p.Request.Platform != c.Platform || p.Request.AccountID != source.Account ||
		p.Request.AccountEpoch != source.Epoch || p.Request.AccountVersion != source.AccountVersion ||
		p.Request.NativeGroupID != "" || p.Request.NativeSenderID != "" || !p.Request.ExpiresAt.Equal(record.ExpiresAt) {
		return bindingOutboundPayload{}, Denied(409, "group_binding_outbound_scope_changed")
	}
	if p.Controlled == nil && (record.Window != "" || c.FriendFlow != "outbound") ||
		p.Controlled != nil && (record.Window != p.Controlled.Window || !p.Controlled.matchesRetained(c) ||
			p.Controlled.Version != record.Version ||
			p.Controlled.AddAccount != p.Request.Target.AddAccount || p.Controlled.Target != p.Request.Target.NativeID ||
			!p.Controlled.ExpiresAt.Equal(record.ExpiresAt)) {
		return bindingOutboundPayload{}, Denied(409, "group_binding_outbound_scope_changed")
	}
	if !validOnboardingOutboundPeer(c, p.Controlled, p.Request.ControlledPeer) {
		return bindingOutboundPayload{}, Denied(409, "group_binding_outbound_scope_changed")
	}
	return p, nil
}

func outboundPending(c bindingRecord, version int64) error {
	if c.FriendFlow != "outbound" {
		return Denied(409, "group_binding_friend_flow_retired")
	}
	return bindingFriendPending(c, version)
}

func outboundPendingContext(ctx context.Context, c bindingRecord, version int64) error {
	if !onboardingControlledContext(ctx).matches(c) {
		return Denied(409, "group_binding_outbound_controlled_only")
	}
	return bindingFriendPending(c, version)
}

func validOutboundEvidence(e GroupBindingOutboundEvidence, account, self string) bool {
	return validBindingAddAccount(account) && e.Target.AddAccount == account &&
		onboardingLabel(e.Target.NativeID, 200) && onboardingLabel(e.Target.Name, 200) &&
		e.SelfID == self && e.Target.NativeID != self &&
		(e.State == "available" && !e.Target.AlreadyFriend || e.State == "friends" && e.Target.AlreadyFriend)
}

func (s *GroupBindingService) outboundPrepareReplay(ctx context.Context, old bindingOutboundRecord, c bindingRecord, in GroupBindingFriendTargetInput) error {
	p, err := s.openBindingOutbound(old, c)
	if err != nil {
		return err
	}
	if old.PrepareKey != in.Key || old.Version != in.Version || p.Request.Target.AddAccount != in.Account ||
		!sameOnboardingControlled(p.Controlled, onboardingControlledContext(ctx)) {
		return Denied(409, "idempotency_conflict")
	}
	return nil
}

func (s *GroupBindingService) PrepareOutboundFriend(ctx context.Context, ref SessionRef, id string, in GroupBindingFriendTargetInput) (GroupBindingChallenge, error) {
	var out GroupBindingChallenge
	if !ValidID(id) || in.Version < 1 || !onboardingKey(in.Key) || !validBindingAddAccount(in.Account) {
		return out, Denied(400, "group_binding_outbound_invalid")
	}
	if onboardingControlledContext(ctx) == nil {
		return out, Denied(409, "group_binding_outbound_controlled_only")
	}
	c, _, err := s.confirmationRecord(ctx, ref, id, false)
	if err != nil {
		return out, err
	}
	if trusted := onboardingControlledContext(ctx); trusted != nil && trusted.AddAccount != in.Account {
		return out, Denied(409, "onboarding_controlled_target_changed")
	}
	old, err := readBindingOutbound(ctx, s.DB.Platform, id)
	if err == nil {
		if err = s.outboundPrepareReplay(ctx, old, c, in); err != nil {
			return out, err
		}
		return s.Read(ctx, ref, id)
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	if err = outboundPendingContext(ctx, c, in.Version); err != nil {
		return out, err
	}
	if err = requireIncomingContactResolved(ctx, s.DB.Platform, c.ID); err != nil {
		return out, err
	}
	lookupCtx, cancelLookup := context.WithDeadline(ctx, onboardingControlledContext(ctx).ExpiresAt)
	defer cancelLookup()
	controlledRole, err := s.authorizeOutboundLookup(lookupCtx, ref, c)
	if err != nil {
		return out, err
	}
	port, ok := s.Native.(GroupBindingOutboundNativePort)
	if !ok || s.Vault == nil {
		return out, Denied(503, "group_binding_outbound_unavailable")
	}
	var fenced atomic.Bool
	e, err := port.LookupBindingFriend(lookupCtx,
		GroupBindingOutboundLookup{GroupBindingNativeScope: onboardingOutboundSource(c, onboardingControlledContext(ctx)).scope("", ""),
			AddAccount: in.Account, ControlledPeer: onboardingOutboundPeer(c, onboardingControlledContext(ctx))},
		func(work context.Context, lookup func(context.Context) error) error {
			work = context.WithValue(work, onboardingControlledKey{}, onboardingControlledContext(ctx))
			if !fenced.CompareAndSwap(false, true) {
				return Denied(409, "native_test_window_invalid")
			}
			return s.controlledOutboundLookup(work, ref, c, *controlledRole, lookup)
		})
	if err == nil && !fenced.Load() {
		err = Denied(409, "native_test_window_invalid")
	}
	if err != nil {
		if onboardingControlledContext(ctx) != nil {
			return out, s.recordControlledLookupFailure(ctx, ref, id, in.Version, err)
		}
		return out, s.recordLookupFailure(ctx, ref, id, in.Version, err)
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	c, err = loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return out, err
	}
	actor, err := s.checkRecord(ctx, tx, ref, c, true)
	if err != nil {
		return out, err
	}
	// Concurrent lookups may finish, but only one immutable target can be pinned.
	old, err = readBindingOutbound(ctx, tx, id)
	if err == nil {
		if err = s.outboundPrepareReplay(ctx, old, c, in); err != nil {
			return out, err
		}
		return s.readTx(ctx, tx, ref, actor, id)
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	if err = outboundPendingContext(ctx, c, in.Version); err != nil {
		return out, err
	}
	if err = requireIncomingContactResolved(ctx, tx, c.ID); err != nil {
		return out, err
	}
	controlled := onboardingControlledContext(ctx)
	source := onboardingOutboundSource(c, controlled)
	if controlled != nil {
		if _, err = s.verifyOnboardingControlled(ctx, tx, c, controlled, false, false); err != nil {
			return out, err
		}
		if controlled.AddAccount != in.Account || controlled.Target != e.Target.NativeID {
			return out, Denied(409, "onboarding_controlled_target_changed")
		}
		if err = s.verifyOnboardingControlledRoles(ctx, tx, c, controlled, *controlledRole); err != nil {
			return out, err
		}
	}
	self, err := s.connection(ctx, tx, source, &e.GroupBindingConnectionEvidence)
	if err != nil {
		return out, err
	}
	if !validOutboundEvidence(e, in.Account, self) {
		return out, Denied(409, "group_binding_outbound_target_changed")
	}
	if peer := onboardingOutboundPeer(c, controlled); peer != nil {
		if e.Peer == nil || !validOnboardingOutboundPeer(c, controlled, e.Peer) {
			return out, Denied(409, "group_binding_outbound_target_changed")
		}
		if _, err = s.connection(ctx, tx, c, &e.Peer.Connection); err != nil {
			return out, err
		}
	} else if e.Peer != nil {
		return out, Denied(409, "group_binding_outbound_target_changed")
	}
	if err = bindingOutboundTarget(ctx, tx, c.Platform, e.Target.NativeID, controlled); err != nil {
		return out, err
	}
	attempt := NewID()
	p := bindingOutboundPayload{Challenge: c.ID, Actor: c.Applicant, Tenant: c.Tenant, Version: c.Version,
		ApplicantVersion: c.ApplicantVersion, Connection: e.GroupBindingConnectionEvidence,
		Controlled: controlled,
		Request: GroupBindingOutboundRequest{GroupBindingNativeScope: source.scope("", ""), OperationID: attempt,
			Target: e.Target, SelfID: self, ExpiresAt: c.ExpiresAt.UTC().Truncate(time.Microsecond),
			ControlledPeer: e.Peer}}
	window := ""
	if controlled != nil {
		window, p.Request.ExpiresAt = controlled.Window, controlled.ExpiresAt
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return out, err
	}
	defer clear(raw)
	sealed, err := s.Vault.Seal("group-binding-outbound:"+attempt, raw)
	if err != nil {
		return out, err
	}
	state, code := "prepared", ""
	if e.Target.AlreadyFriend {
		state, code = "friends", "native_friend_relationship_observed"
	}
	_, err = tx.Exec(ctx, `INSERT INTO group_binding_outbound_attempts(id,challenge_id,actor_id,tenant_id,platform,account_id,
		account_epoch,account_version,applicant_version,challenge_version,gate_version,
		payload_sealed,prepare_key,status,result_code,expires_at,test_window_id)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,nullif($17,'')::uuid)`,
		attempt, c.ID, c.Applicant, c.Tenant, c.Platform, source.Account, source.Epoch, source.AccountVersion, c.ApplicantVersion, c.Version,
		0, sealed, in.Key, state, code, p.Request.ExpiresAt, window)
	if err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, actor, c.Tenant, "group_binding_outbound_target_prepared", attempt,
		map[string]any{"challenge_id": c.ID, "platform": c.Platform, "version": c.Version, "already_friend": e.Target.AlreadyFriend}); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return s.Read(ctx, ref, id)
}

func (s *GroupBindingService) readOutboundChallenge(ctx context.Context, tx pgx.Tx, actor string, out *GroupBindingChallenge) error {
	if actor != out.Scope.ActorID || out.BlockedReason == "group_binding_scope_changed" {
		return nil
	}
	record, err := readBindingOutbound(ctx, tx, out.ID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	c, err := loadBindingRecord(ctx, tx, out.ID, false)
	if err != nil {
		return err
	}
	p, err := s.openBindingOutbound(record, c)
	if err != nil {
		return err
	}
	out.FriendOutbound = &GroupBindingOutbound{ID: record.ID, Status: record.Status, Code: record.Code,
		Version: record.Version, Target: p.Request.Target, SubmitDiagnostic: outboundSubmitDiagnostic(record)}
	f := out.FriendOutbound
	f.CanReconcile = (record.Status == "unknown" || record.Status == "submitted" ||
		record.Status == "submitting" && !time.Now().Before(record.ExpiresAt.Add(bindingFriendSubmissionFence))) &&
		out.BlockedReason != "group_binding_scope_changed"
	if record.Status == "submitting" || record.Status == "unknown" {
		out.State = c.State
		out.CanConfirm, out.CanAcceptInvitation, out.CanCancel = false, false, false
		out.BlockedReason = "group_binding_outbound_operation_unresolved"
	}
	if record.Status == "prepared" && record.Used == 0 && out.State == "waiting" && !out.IdentityProven &&
		c.Version == record.Version && c.ExpiresAt.After(time.Now().Add(4*time.Second)) && out.BlockedReason == "" {
		if p.Controlled == nil {
			f.SendBlockedReason = "group_binding_outbound_controlled_only"
			return nil
		}
		if p.Controlled != nil {
			if !sameOnboardingControlled(p.Controlled, onboardingControlledContext(ctx)) {
				f.SendBlockedReason = "group_binding_outbound_controlled_only"
				return nil
			}
			if _, err = s.verifyOnboardingControlled(ctx, tx, c, p.Controlled, true, false); err != nil {
				var failure *Failure
				if !errors.As(err, &failure) {
					return err
				}
				f.SendBlockedReason = failure.Code
				return nil
			}
		}
		_, gateErr := requireOutboundFriendGate(ctx, tx, c.Platform, record.Account, record.Gate)
		if gateErr == nil {
			f.CanSend = true
		} else {
			var failure *Failure
			if !errors.As(gateErr, &failure) {
				return gateErr
			}
			f.SendBlockedReason = failure.Code
		}
	}
	return nil
}

// Legacy rows keep their historical proof constraint. Controlled contact
// selection does not constrain the customer's independent message identity.
func (s *GroupBindingService) checkOutboundSender(ctx context.Context, q Queryer, c bindingRecord, sender string) error {
	record, err := readBindingOutbound(ctx, q, c.ID)
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	p, err := s.openBindingOutbound(record, c)
	if err != nil {
		return err
	}
	if p.Controlled != nil {
		return nil
	}
	if p.Request.Target.NativeID != sender {
		return Denied(409, "group_binding_identity_changed")
	}
	return nil
}

func requireOutboundResolved(ctx context.Context, q Queryer, challenge string) error {
	var pending bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_binding_outbound_attempts
		WHERE challenge_id=$1 AND status IN ('submitting','unknown')) OR
		EXISTS(SELECT 1 FROM group_binding_friend_attempts
			WHERE challenge_id=$1 AND status IN ('submitting','unknown'))`, challenge).Scan(&pending)
	if err != nil {
		return err
	}
	if pending {
		return Denied(409, "group_binding_outbound_operation_unresolved")
	}
	return nil
}

func requirePendingOutboundResolved(ctx context.Context, q Queryer, challenge string) error {
	var pending bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_binding_outbound_attempts
		WHERE challenge_id=$1 AND status IN ('submitting','unknown'))`, challenge).Scan(&pending)
	if err != nil {
		return err
	}
	if pending {
		return Denied(409, "group_binding_outbound_operation_unresolved")
	}
	return nil
}

func requireIncomingContactResolved(ctx context.Context, q Queryer, challenge string) error {
	var pending bool
	err := q.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM group_binding_friend_attempts
		WHERE challenge_id=$1 AND status IN ('prepared','submitting','unknown'))`, challenge).Scan(&pending)
	if err != nil {
		return err
	}
	if pending {
		return Denied(409, "group_binding_friend_operation_unresolved")
	}
	return nil
}

func bindingOutboundTarget(ctx context.Context, q Queryer, platform, target string, controlled *onboardingControlledAuthorization) error {
	if controlled != nil {
		if controlled.Platform != platform || controlled.Target != target {
			return Denied(409, "onboarding_controlled_target_changed")
		}
		return nil
	}
	return bindingHumanSender(ctx, q, platform, target)
}
