package control

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5"
)

func (s *GroupBindingService) bindingFriendProof(ctx context.Context, tx pgx.Tx, c bindingRecord, request GroupBindingFriendApproval, version int64, connection *GroupBindingConnectionEvidence) (string, error) {
	var id string
	var sender, sealed []byte
	err := tx.QueryRow(ctx, `SELECT id::text,native_sender_sealed,evidence_sealed FROM group_binding_friend_proofs
		WHERE attempt_id=$1 AND challenge_id=$2 AND actor_id=$3 AND tenant_id=$4 AND platform=$5 AND account_id=$6
		AND account_epoch=$7 AND account_version=$8 AND applicant_version=$9 AND challenge_version=$10
		AND source='friend_application' FOR SHARE`, request.OperationID, c.ID, c.Applicant, c.Tenant,
		c.Platform, c.Account, c.Epoch, c.AccountVersion, c.ApplicantVersion, version).Scan(&id, &sender, &sealed)
	if err == pgx.ErrNoRows {
		return "", Denied(409, "group_binding_friend_proof_changed")
	}
	if err != nil {
		return "", err
	}
	plain, err := s.Vault.Open("group-binding-friend-sender:"+id, sender)
	if err != nil {
		return "", err
	}
	same := string(plain) == request.SenderID
	clear(plain)
	raw, err := s.Vault.Open("group-binding-friend-evidence:"+id, sealed)
	if err != nil {
		return "", err
	}
	defer clear(raw)
	var original GroupBindingFriendEvidence
	if !same || json.Unmarshal(raw, &original) != nil || original.State != "pending" ||
		original.Platform != c.Platform || original.AccountID != c.Account || original.AccountEpoch != c.Epoch ||
		original.AccountVersion != c.AccountVersion || original.ApplicationID != request.ApplicationID ||
		original.SenderID != request.SenderID || original.RecipientID != request.RecipientID ||
		connection != nil && (original.ReceiverOwner != connection.ReceiverOwner ||
			original.ReceiverRevision != connection.ReceiverRevision || original.DeploymentVersion != connection.DeploymentVersion ||
			original.CredentialDigest != connection.CredentialDigest) {
		return "", Denied(409, "group_binding_friend_proof_changed")
	}
	return id, nil
}

func bindingFriendPayloadMatches(payload bindingFriendPayload, attempt bindingFriendRecord, c bindingRecord) bool {
	r := payload.Approval
	return payload.Challenge == c.ID && payload.Actor == c.Applicant && payload.Tenant == c.Tenant &&
		payload.ChallengeVersion == attempt.Version && payload.ApplicantVersion == c.ApplicantVersion &&
		r.OperationID == attempt.ID && r.Platform == c.Platform &&
		r.AccountID == c.Account && r.AccountEpoch == c.Epoch && r.AccountVersion == c.AccountVersion &&
		r.NativeGroupID == "" && r.NativeSenderID == "" && attempt.Challenge == c.ID &&
		attempt.Actor == c.Applicant && attempt.Tenant == c.Tenant && attempt.Platform == c.Platform &&
		attempt.Account == c.Account && attempt.Epoch == c.Epoch && attempt.AccountVersion == c.AccountVersion &&
		attempt.ApplicantVersion == c.ApplicantVersion && r.ExpiresAt.Equal(attempt.ExpiresAt)
}

func (s *GroupBindingService) openBindingFriendPayload(attempt bindingFriendRecord, c bindingRecord) (bindingFriendPayload, error) {
	var out bindingFriendPayload
	raw, err := s.Vault.Open("group-binding-friend-attempt:"+attempt.ID, attempt.Payload)
	if err != nil {
		return out, err
	}
	defer clear(raw)
	if json.Unmarshal(raw, &out) != nil || !bindingFriendPayloadMatches(out, attempt, c) {
		return bindingFriendPayload{}, Denied(409, "group_binding_friend_proof_changed")
	}
	out.Approval.ChallengeText = out.ChallengeText
	out.Approval.Contact = out.Contact
	return out, nil
}

func (s *GroupBindingService) finishBindingFriend(ctx context.Context, tx pgx.Tx, c bindingRecord, r GroupBindingFriendApproval, proof, code string, identity bool) error {
	if identity {
		sealed, err := s.Vault.Seal("group-binding-identity:"+c.ID, []byte(r.SenderID))
		if err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `UPDATE group_binding_challenges SET identity_friend_proof_id=$2,identity_sealed=$3,
			version=version+1,blocked_reason='' WHERE id=$1 AND version=$4 AND state='waiting' AND identity_sealed IS NULL`,
			c.ID, proof, sealed, c.Version)
		if err != nil {
			return err
		}
		if tag.RowsAffected() != 1 {
			return Denied(409, "group_binding_friend_scope_changed")
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE group_binding_friend_attempts SET status='succeeded',result_code=$2,
		updated_at=clock_timestamp() WHERE id=$1 AND status IN ('submitting','unknown')`, r.OperationID, code)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return Denied(409, "group_binding_friend_attempt_changed")
	}
	return onboardingAudit(ctx, tx, Actor{ID: c.Applicant}, c.Tenant, "group_binding_friend_verified", r.OperationID,
		map[string]any{"challenge_id": c.ID, "platform": c.Platform, "identity_recorded": identity})
}
