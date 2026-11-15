package control

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base32"
	"encoding/json"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/security"
)

type GroupBindingFriendInput struct {
	Version int64  `json:"version"`
	Key     string `json:"-"`
}

type GroupBindingFriendAttempt struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Code    string `json:"result_code"`
	Version int64  `json:"version"`
}

type GroupBindingFriendState struct {
	ChallengeID    string                     `json:"challenge_id"`
	Version        int64                      `json:"version"`
	IdentityProven bool                       `json:"identity_proven"`
	CanVerify      bool                       `json:"can_verify"`
	Attempt        *GroupBindingFriendAttempt `json:"attempt,omitempty"`
}

type bindingFriendRecord struct {
	GroupBindingFriendAttempt
	Challenge, Actor, Tenant, Platform, Account, Key string
	Epoch, AccountVersion, ApplicantVersion          int64
	GateVersion                                      int64
	RequestHash, Payload                             []byte
	ExpiresAt                                        time.Time
}

type bindingFriendPayload struct {
	Challenge, Actor, Tenant string
	ChallengeVersion         int64
	ApplicantVersion         int64
	ChallengeText            string
	Approval                 GroupBindingFriendApproval
	Contact                  *GroupBindingFriendSelector
	Connection               GroupBindingConnectionEvidence
	RequestedAt              time.Time
}

// The full text is sealed by Create with group-binding-friend-text:<id>.
// private_token_hash remains independent for pre-existing private-message proofs.
func newFriendChallenge() (text string, hash []byte, err error) {
	var random [13]byte
	if _, err = rand.Read(random[:]); err != nil {
		return "", nil, err
	}
	code := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random[:])[:20]
	text = "群接入 " + code
	return text, security.Digest(text), nil
}

func validFriendChallenge(text string, hash []byte) bool {
	code, ok := strings.CutPrefix(text, "群接入 ")
	if !ok || len(code) != 20 || len(hash) != 32 ||
		strings.IndexFunc(code, func(r rune) bool { return !(r >= 'A' && r <= 'Z' || r >= '2' && r <= '7') }) >= 0 {
		return false
	}
	return subtle.ConstantTimeCompare(hash, security.Digest(text)) == 1
}

// Only the original website actor may recover the sealed text. Other operator
// views may inspect ordinary challenge progress but cannot borrow its consent.
func (s *GroupBindingService) readFriendChallenge(ctx context.Context, tx pgx.Tx, id, actor string, out *GroupBindingChallenge) error {
	if out.FriendFlow == "outbound" || out.FriendFlow == "incoming_contact" {
		return nil
	}
	c, err := loadBindingRecord(ctx, tx, id, false)
	if err != nil {
		return err
	}
	if actor != c.Applicant {
		return nil
	}
	attempt, err := readBindingFriend(ctx, tx, id, "", "")
	if err == nil {
		out.FriendAttempt = &attempt.GroupBindingFriendAttempt
		if attempt.Status == "prepared" || attempt.Status == "submitting" || attempt.Status == "unknown" {
			out.State = c.State
			out.CanConfirm, out.CanAcceptInvitation = false, false
			if attempt.Status != "prepared" {
				out.CanCancel = false
			}
		}
	} else if err != pgx.ErrNoRows {
		return err
	}
	out.CanVerifyFriend = false
	return nil
}

func bindingFriendDigest(challenge string, in GroupBindingFriendInput) []byte {
	raw, _ := json.Marshal(struct {
		Challenge string
		Version   int64
	}{challenge, in.Version})
	return security.Digest(string(raw))
}

func (s *GroupBindingService) friendText(ctx context.Context, q Queryer, c bindingRecord) (string, error) {
	var hash, sealed []byte
	if err := q.QueryRow(ctx, `SELECT friend_token_hash,friend_text_sealed FROM group_binding_challenges WHERE id=$1`, c.ID).
		Scan(&hash, &sealed); err != nil {
		return "", err
	}
	if len(hash) == 0 || len(sealed) == 0 || s.Vault == nil {
		return "", Denied(409, "group_binding_friend_challenge_unavailable")
	}
	raw, err := s.Vault.Open("group-binding-friend-text:"+c.ID, sealed)
	if err != nil {
		return "", err
	}
	defer clear(raw)
	text := string(raw)
	if !validFriendChallenge(text, hash) {
		return "", Denied(409, "group_binding_friend_challenge_unavailable")
	}
	return text, nil
}

func readBindingFriend(ctx context.Context, q Queryer, challenge, actor, key string) (bindingFriendRecord, error) {
	var out bindingFriendRecord
	err := q.QueryRow(ctx, `SELECT id::text,status,result_code,challenge_version,challenge_id::text,actor_id::text,
		tenant_id::text,platform,account_id::text,idempotency_key,account_epoch,account_version,applicant_version,
		gate_version,request_hash,payload_sealed,expires_at FROM group_binding_friend_attempts
		WHERE challenge_id=$1 OR actor_id=nullif($2,'')::uuid AND idempotency_key=$3`, challenge, actor, key).
		Scan(&out.ID, &out.Status, &out.Code, &out.Version, &out.Challenge, &out.Actor, &out.Tenant,
			&out.Platform, &out.Account, &out.Key, &out.Epoch, &out.AccountVersion, &out.ApplicantVersion,
			&out.GateVersion, &out.RequestHash, &out.Payload, &out.ExpiresAt)
	return out, err
}

func bindingFriendReplay(attempt bindingFriendRecord, c bindingRecord, in GroupBindingFriendInput) error {
	if attempt.Challenge != c.ID || attempt.Actor != c.Applicant || attempt.Key != in.Key ||
		!bytes.Equal(attempt.RequestHash, bindingFriendDigest(c.ID, in)) {
		return Denied(409, "idempotency_conflict")
	}
	return nil
}

// FriendState reads only persisted state. Viewing or refreshing never performs
// a native lookup, approves an application, or reconciles an unknown result.
func (s *GroupBindingService) FriendState(ctx context.Context, ref SessionRef, id string) (GroupBindingFriendState, error) {
	var out GroupBindingFriendState
	if !ValidID(id) {
		return out, Denied(404, "group_binding_challenge_not_found")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	c, err := loadBindingRecord(ctx, tx, id, false)
	if err != nil {
		return out, err
	}
	if _, err = s.checkRecord(ctx, tx, ref, c, false); err != nil {
		return out, err
	}
	out.ChallengeID, out.Version, out.IdentityProven = c.ID, c.Version, len(c.Identity) != 0
	old, err := readBindingFriend(ctx, tx, c.ID, "", "")
	if err == nil {
		out.Attempt = &old.GroupBindingFriendAttempt
		return out, nil
	}
	if err != pgx.ErrNoRows {
		return out, err
	}
	return out, nil
}

func bindingFriendPending(c bindingRecord, version int64) error {
	if c.Version != version || c.State != "waiting" || len(c.Identity) != 0 ||
		!c.ExpiresAt.After(time.Now().Add(4*time.Second)) {
		return Denied(409, "group_binding_friend_scope_changed")
	}
	return nil
}

func bindingFriendEvidence(request GroupBindingFriendRequest, evidence GroupBindingFriendEvidence, self string) bool {
	return request.NativeGroupID == "" && request.NativeSenderID == "" &&
		onboardingLabel(evidence.ApplicationID, 300) && onboardingLabel(evidence.SenderID, 200) &&
		evidence.RecipientID == self && evidence.SenderID != self && evidence.State == "pending" &&
		evidence.ExpiresAt.After(time.Now().Add(4*time.Second))
}

func bindingFriendMatches(request GroupBindingFriendApproval, evidence GroupBindingFriendEvidence, state string) bool {
	return request.NativeGroupID == "" && request.NativeSenderID == "" &&
		evidence.ApplicationID == request.ApplicationID && evidence.SenderID == request.SenderID &&
		evidence.RecipientID == request.RecipientID && evidence.State == state &&
		onboardingLabel(request.ApplicationID, 300) && onboardingLabel(request.SenderID, 200) &&
		onboardingLabel(request.RecipientID, 200) && request.SenderID != request.RecipientID &&
		(state != "pending" || request.ExpiresAt.After(time.Now().Add(3*time.Second)) &&
			evidence.ExpiresAt.After(time.Now().Add(3*time.Second)))
}

// VerifyFriend is the human consent action. The native port must resolve one
// exact incoming pending application by ChallengeText, never by browser identity.
func (s *GroupBindingService) VerifyFriend(ctx context.Context, ref SessionRef, id string, in GroupBindingFriendInput) (GroupBindingFriendAttempt, error) {
	if !ValidID(id) || in.Version < 1 || !onboardingKey(in.Key) {
		return GroupBindingFriendAttempt{}, Denied(400, "group_binding_friend_invalid")
	}
	if _, _, err := s.confirmationRecord(ctx, ref, id, false); err != nil {
		return GroupBindingFriendAttempt{}, err
	}
	return GroupBindingFriendAttempt{}, Denied(410, "group_binding_friend_flow_retired")
}
