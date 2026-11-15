package control

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/security"
)

type GroupBindingContactInput struct {
	Version  int64  `json:"version"`
	Nickname string `json:"nickname"`
	Key      string `json:"-"`
}

type GroupBindingContactApproval struct {
	Version     int64  `json:"version"`
	OperationID string `json:"operation_id"`
	Key         string `json:"-"`
}

type GroupBindingContact struct {
	GroupBindingFriendAttempt
	Name          string    `json:"name"`
	RequestedAt   time.Time `json:"requested_at"`
	Action        string    `json:"action"`
	CanApprove    bool      `json:"can_approve"`
	CanReject     bool      `json:"can_reject"`
	CanReconcile  bool      `json:"can_reconcile"`
	BlockedReason string    `json:"blocked_reason,omitempty"`
}

func contactPending(c bindingRecord, version int64) error {
	if c.FriendFlow != "incoming_contact" {
		return Denied(409, "group_binding_friend_flow_retired")
	}
	return bindingFriendPending(c, version)
}

func contactDigest(id string, in GroupBindingContactInput) []byte {
	raw, _ := json.Marshal(struct {
		ID, Name string
		Version  int64
	}{id, in.Nickname, in.Version})
	return security.Digest(string(raw))
}

func contactMatches(r GroupBindingFriendApproval, e GroupBindingFriendEvidence, state string) bool {
	return r.Contact != nil && r.ChallengeText == "" &&
		bindingFriendMatches(r, e, state) && e.Name == r.Contact.Name &&
		contactTimeMatches(r.Contact, e.RequestedAt, r.RejectExpired)
}

func contactTimeMatches(selector *GroupBindingFriendSelector, at time.Time, reject bool) bool {
	if selector == nil || at.IsZero() {
		return false
	}
	if reject {
		return selector.RejectExpired && !at.After(selector.Since.Add(-10*time.Minute))
	}
	return !at.Before(selector.Since) && at.Before(selector.Until)
}

func (s *GroupBindingService) PrepareContact(ctx context.Context, ref SessionRef, id string, in GroupBindingContactInput) (GroupBindingChallenge, error) {
	var out GroupBindingChallenge
	if !ValidID(id) || in.Version < 1 || !onboardingKey(in.Key) ||
		!onboardingLabel(in.Nickname, 200) || strings.TrimSpace(in.Nickname) != in.Nickname {
		return out, Denied(400, "group_binding_contact_invalid")
	}
	c, _, err := s.confirmationRecord(ctx, ref, id, false)
	if err != nil {
		return out, err
	}
	if err = contactPending(c, in.Version); err != nil {
		return out, err
	}
	if err = requireControlledChallenge(ctx, s.DB.Platform, c); err != nil {
		return out, err
	}
	old, err := readBindingFriend(ctx, s.DB.Platform, id, c.Applicant, in.Key)
	if err == nil {
		if old.Challenge != id || old.Actor != c.Applicant || old.Key != in.Key ||
			!bytes.Equal(old.RequestHash, contactDigest(id, in)) {
			return out, Denied(409, "idempotency_conflict")
		}
		return s.Read(ctx, ref, id)
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	if err = requirePendingOutboundResolved(ctx, s.DB.Platform, id); err != nil {
		return out, err
	}
	port, ok := s.Native.(GroupBindingFriendNativePort)
	if !ok || s.Vault == nil {
		return out, Denied(503, "group_binding_friend_native_unavailable")
	}
	if _, err = requireBindingFriendGate(ctx, s.DB.Platform, c.Platform, c.Account, 0); err != nil {
		return out, err
	}
	// The browser supplies a nickname only. Time bounds and the receiving
	// account come from the website actor's persisted ten-minute challenge.
	selector := &GroupBindingFriendSelector{Name: in.Nickname,
		Since: c.CreatedAt.UTC().Truncate(time.Second), Until: time.Now().UTC().Truncate(time.Microsecond),
		RejectExpired: true}
	if end := selector.Since.Add(10 * time.Minute); selector.Until.After(end) {
		selector.Until = end
	}
	request := GroupBindingFriendRequest{GroupBindingNativeScope: c.scope("", ""), Contact: selector}
	e, err := port.BindingFriend(ctx, request)
	if err != nil {
		return out, err
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	c, err = loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return out, err
	}
	a, err := s.checkRecord(ctx, tx, ref, c, true)
	if err != nil {
		return out, err
	}
	if err = s.applicantPermission(ctx, tx, c); err != nil {
		return out, err
	}
	if err = contactPending(c, in.Version); err != nil {
		return out, err
	}
	if err = requireControlledChallenge(ctx, tx, c); err != nil {
		return out, err
	}
	old, err = readBindingFriend(ctx, tx, id, "", "")
	if err == nil {
		if old.Key != in.Key || !bytes.Equal(old.RequestHash, contactDigest(id, in)) {
			return out, Denied(409, "idempotency_conflict")
		}
		return s.readTx(ctx, tx, ref, a, id)
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	if err = requirePendingOutboundResolved(ctx, tx, id); err != nil {
		return out, err
	}
	gate, err := requireBindingFriendGate(ctx, tx, c.Platform, c.Account, 0)
	if err != nil {
		return out, err
	}
	self, err := s.connection(ctx, tx, c, &e.GroupBindingConnectionEvidence)
	if err != nil {
		return out, err
	}
	reject := contactTimeMatches(selector, e.RequestedAt, true)
	if !bindingFriendEvidence(request, e, self) || e.Name != in.Nickname ||
		!contactTimeMatches(selector, e.RequestedAt, reject) {
		return out, Denied(409, "group_binding_friend_application_changed")
	}
	if reject {
		if _, ok := s.Native.(GroupBindingExpiredFriendNativePort); !ok {
			return out, Denied(503, "group_binding_friend_native_unavailable")
		}
	}
	if !reject || c.ControlledScope != "" {
		if err = bindingChallengeSender(ctx, tx, c, e.SenderID); err != nil {
			return out, err
		}
	}
	if err = bindingFriendWriteAvailable(ctx, tx, c.Account, "", nil); err != nil {
		return out, err
	}
	operation := NewID()
	approval := GroupBindingFriendApproval{GroupBindingFriendRequest: request, OperationID: operation,
		ApplicationID: e.ApplicationID, SenderID: e.SenderID, RecipientID: e.RecipientID,
		RequestedAt: e.RequestedAt, ExpiresAt: c.ExpiresAt.UTC().Truncate(time.Microsecond), RejectExpired: reject}
	payload := bindingFriendPayload{Challenge: c.ID, Actor: c.Applicant, Tenant: c.Tenant,
		ChallengeVersion: c.Version, ApplicantVersion: c.ApplicantVersion, Approval: approval,
		Contact: selector, Connection: e.GroupBindingConnectionEvidence, RequestedAt: e.RequestedAt}
	raw, err := json.Marshal(payload)
	if err != nil {
		return out, err
	}
	defer clear(raw)
	sealed, err := s.Vault.Seal("group-binding-friend-attempt:"+operation, raw)
	if err != nil {
		return out, err
	}
	_, err = tx.Exec(ctx, `INSERT INTO group_binding_friend_attempts(id,challenge_id,actor_id,tenant_id,platform,
		account_id,account_epoch,account_version,applicant_version,challenge_version,gate_version,
		request_hash,payload_sealed,idempotency_key,expires_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)`,
		operation, c.ID, c.Applicant, c.Tenant, c.Platform, c.Account, c.Epoch, c.AccountVersion, c.ApplicantVersion,
		c.Version, gate, contactDigest(c.ID, in), sealed, in.Key, approval.ExpiresAt)
	if err != nil {
		return out, err
	}
	if err = onboardingAudit(ctx, tx, a, c.Tenant, "group_binding_contact_selected", operation,
		map[string]any{"challenge_id": c.ID, "platform": c.Platform, "version": c.Version}); err != nil {
		return out, err
	}
	if err = tx.Commit(ctx); err != nil {
		return out, err
	}
	return s.Read(ctx, ref, id)
}

func (s *GroupBindingService) readContactChallenge(ctx context.Context, tx pgx.Tx, actor string, out *GroupBindingChallenge) error {
	if out.FriendFlow != "incoming_contact" || actor != out.Scope.ActorID ||
		out.BlockedReason == "group_binding_scope_changed" {
		return nil
	}
	c, err := loadBindingRecord(ctx, tx, out.ID, false)
	if err != nil {
		return err
	}
	record, err := readBindingFriend(ctx, tx, c.ID, "", "")
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	p, err := s.openContact(record, c)
	if err != nil {
		return err
	}
	out.FriendContact = &GroupBindingContact{GroupBindingFriendAttempt: record.GroupBindingFriendAttempt,
		Name: p.Contact.Name, RequestedAt: p.RequestedAt, Action: "approve"}
	f := out.FriendContact
	if p.Approval.RejectExpired {
		f.Action = "reject_expired"
	}
	f.CanReconcile = record.Status == "unknown" || record.Status == "submitting" &&
		!time.Now().Before(record.ExpiresAt.Add(bindingFriendSubmissionFence))
	if record.Status == "submitting" || record.Status == "unknown" {
		out.CanCancel, out.CanConfirm, out.CanAcceptInvitation = false, false, false
		out.BlockedReason = "group_binding_friend_operation_unresolved"
		out.State = c.State
	}
	if record.Status == "prepared" && out.State == "waiting" && !out.IdentityProven &&
		c.Version == record.Version && c.ExpiresAt.After(time.Now().Add(4*time.Second)) && out.BlockedReason == "" {
		_, gateErr := requireBindingFriendGate(ctx, tx, c.Platform, c.Account, record.GateVersion)
		if gateErr == nil {
			f.CanApprove, f.CanReject = !p.Approval.RejectExpired, p.Approval.RejectExpired
		} else {
			var failure *Failure
			if !errors.As(gateErr, &failure) {
				return gateErr
			}
			f.BlockedReason = failure.Code
		}
	}
	return nil
}

func (s *GroupBindingService) openContact(record bindingFriendRecord, c bindingRecord) (bindingFriendPayload, error) {
	p, err := s.openBindingFriendPayload(record, c)
	if err != nil {
		return p, err
	}
	if c.FriendFlow != "incoming_contact" || p.Contact == nil || p.Approval.ChallengeText != "" ||
		p.Contact.Name == "" || !p.Contact.Since.Equal(c.CreatedAt.UTC().Truncate(time.Second)) ||
		p.Contact.Until.After(c.ExpiresAt) || p.Contact.Until.Before(p.Contact.Since) ||
		p.Contact.Until.Sub(p.Contact.Since) > 10*time.Minute ||
		!contactTimeMatches(p.Contact, p.RequestedAt, p.Approval.RejectExpired) ||
		!p.Approval.RequestedAt.IsZero() && !p.Approval.RequestedAt.Equal(p.RequestedAt) {
		return bindingFriendPayload{}, Denied(409, "group_binding_friend_proof_changed")
	}
	// Earlier sealed selections kept this timestamp beside the approval.
	p.Approval.RequestedAt = p.RequestedAt
	return p, nil
}

func (s *GroupBindingService) checkContactSender(ctx context.Context, q Queryer, c bindingRecord, sender string) error {
	if c.FriendFlow != "incoming_contact" {
		return nil
	}
	r, err := readBindingFriend(ctx, q, c.ID, "", "")
	if err == pgx.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}
	p, err := s.openContact(r, c)
	if err != nil {
		return err
	}
	if p.Approval.RejectExpired {
		return Denied(409, "group_binding_friend_reapply_required")
	}
	if p.Approval.SenderID != sender {
		return Denied(409, "group_binding_identity_changed")
	}
	return nil
}

func completeUnsubmittedContact(ctx context.Context, tx pgx.Tx, c bindingRecord) error {
	if c.FriendFlow != "incoming_contact" {
		return nil
	}
	// Authenticated private/group proof can arrive after an out-of-band
	// friendship. Release an unused approval, never resolve submitted work.
	_, err := tx.Exec(ctx, `UPDATE group_binding_friend_attempts SET status='failed',attempts_used=1,
		result_code='group_binding_contact_approval_not_required',updated_at=clock_timestamp()
		WHERE challenge_id=$1 AND status='prepared' AND attempts_used=0`, c.ID)
	return err
}
