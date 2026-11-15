package control

import (
	"context"
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
)

func (s *GroupBindingService) outboundWriteRate(ctx context.Context, tx pgx.Tx, record bindingOutboundRecord, target string) error {
	var actorUsed, accountUsed int
	var pending bool
	err := tx.QueryRow(ctx, `SELECT
		(SELECT count(*) FROM group_binding_outbound_attempts WHERE actor_id=$1 AND attempts_used=1
			AND id<>$3 AND updated_at>clock_timestamp()-interval '1 hour'),
		(SELECT count(*) FROM group_binding_outbound_attempts WHERE account_id=$2 AND attempts_used=1
			AND id<>$3 AND updated_at>clock_timestamp()-interval '1 hour'),
		EXISTS(SELECT 1 FROM group_binding_outbound_attempts WHERE account_id=$2 AND id<>$3
			AND status IN ('submitting','unknown'))`, record.Actor, record.Account, record.ID).
		Scan(&actorUsed, &accountUsed, &pending)
	if err != nil {
		return err
	}
	if pending {
		return Denied(409, "group_binding_outbound_operation_unresolved")
	}
	if actorUsed >= 3 || accountUsed >= 12 {
		return Denied(429, "group_binding_outbound_rate_limited")
	}
	// Account-wide hourly limits bound this privacy-preserving comparison to
	// twelve sealed records; searchable native identities are not indexed.
	rows, err := tx.Query(ctx, `SELECT id::text,payload_sealed FROM group_binding_outbound_attempts
		WHERE account_id=$1 AND id<>$2 AND attempts_used=1
		AND updated_at>clock_timestamp()-interval '10 minutes' ORDER BY updated_at DESC LIMIT 12`,
		record.Account, record.ID)
	if err != nil {
		return err
	}
	type prior struct {
		id     string
		sealed []byte
	}
	list := []prior{}
	for rows.Next() {
		var p prior
		if err = rows.Scan(&p.id, &p.sealed); err != nil {
			rows.Close()
			return err
		}
		list = append(list, p)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	for _, old := range list {
		raw, err := s.Vault.Open("group-binding-outbound:"+old.id, old.sealed)
		if err != nil {
			return err
		}
		var p bindingOutboundPayload
		err = json.Unmarshal(raw, &p)
		clear(raw)
		if err != nil {
			return Denied(409, "group_binding_outbound_scope_changed")
		}
		if p.Request.Target.NativeID == target {
			return Denied(429, "group_binding_outbound_rate_limited")
		}
	}
	return nil
}

func (s *GroupBindingService) prepareOutboundSubmission(ctx context.Context, ref SessionRef, id string, in GroupBindingConfirmation) (bindingOutboundRecord, bindingOutboundPayload, bool, error) {
	var record bindingOutboundRecord
	var payload bindingOutboundPayload
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return record, payload, false, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='500ms'`); err != nil {
		return record, payload, false, err
	}
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		return record, payload, false, err
	}
	c, err := loadBindingRecord(ctx, tx, id, true)
	if err != nil {
		return record, payload, false, err
	}
	if _, err = s.checkRecord(ctx, tx, ref, c, true); err != nil {
		return record, payload, false, err
	}
	record, err = readBindingOutbound(ctx, tx, c.ID)
	if err == pgx.ErrNoRows {
		err = Denied(404, "group_binding_outbound_attempt_not_found")
	}
	if err != nil {
		return record, payload, false, err
	}
	payload, err = s.openBindingOutbound(record, c)
	if err != nil {
		return record, payload, false, err
	}
	if !sameOnboardingControlled(payload.Controlled, onboardingControlledContext(ctx)) || payload.Controlled == nil {
		return record, payload, false, Denied(409, "group_binding_outbound_controlled_only")
	}
	if _, err = s.verifyOnboardingControlled(ctx, tx, c, payload.Controlled, true, record.Used == 1); err != nil {
		return record, payload, false, err
	}
	if record.SendKey != "" {
		if record.SendKey != in.Key || record.Version != in.Version {
			return record, payload, false, Denied(409, "idempotency_conflict")
		}
		// Return the original operation. Even a submitting/unknown record cannot
		// dispatch a second native attempt after a repeated confirmation.
		return record, payload, false, nil
	}
	if err = outboundPendingContext(ctx, c, in.Version); err != nil {
		return record, payload, false, err
	}
	if err = requireIncomingContactResolved(ctx, tx, id); err != nil {
		return record, payload, false, err
	}
	if record.Status != "prepared" || record.Used != 0 || record.Version != in.Version {
		return record, payload, false, Denied(409, "group_binding_outbound_not_sendable")
	}
	source := onboardingOutboundSource(c, payload.Controlled)
	gate, err := requireOutboundFriendGate(ctx, tx, c.Platform, source.Account, record.Gate)
	if err != nil {
		return record, payload, false, err
	}
	if err = bindingOutboundTarget(ctx, tx, c.Platform, payload.Request.Target.NativeID, payload.Controlled); err != nil {
		return record, payload, false, err
	}
	var account string
	if err = tx.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, source.Account).Scan(&account); err != nil {
		return record, payload, false, err
	}
	if err = s.outboundFriendWriteAvailable(ctx, tx, c, record, payload, nil); err != nil {
		return record, payload, false, err
	}
	if err = s.outboundWriteRate(ctx, tx, record, payload.Request.Target.NativeID); err != nil {
		return record, payload, false, err
	}
	if payload.Controlled != nil {
		if payload.Controlled.Claimant != nil {
			if err = ReserveControlledScopeNativeTx(ctx, tx, payload.Controlled.Claimant.Scope,
				"onboarding_applicant_friend", record.ID); err != nil {
				return record, payload, false, err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE test_windows SET used=used+1
			WHERE id=$1 AND used<budget AND NOT cancelled AND expires_at>clock_timestamp()+interval '4 seconds'`,
			record.Window)
		if err != nil {
			return record, payload, false, err
		}
		if tag.RowsAffected() != 1 {
			return record, payload, false, Denied(409, "native_test_window_invalid")
		}
	}
	tag, err := tx.Exec(ctx, `UPDATE group_binding_outbound_attempts SET status='submitting',attempts_used=1,
		send_key=$2,gate_version=$3,updated_at=clock_timestamp() WHERE id=$1 AND status='prepared' AND attempts_used=0
		AND expires_at>clock_timestamp()+interval '4 seconds'`, record.ID, in.Key, gate)
	if err != nil {
		return record, payload, false, err
	}
	if tag.RowsAffected() != 1 {
		return record, payload, false, Denied(409, "group_binding_outbound_not_sendable")
	}
	// A lost preparation acknowledgement leaves a durable submitting marker;
	// only explicit read-only reconciliation may resolve it.
	if err = tx.Commit(ctx); err != nil {
		return record, payload, false, err
	}
	record.Gate = gate
	record.Used = 1
	return record, payload, true, nil
}

func (s *GroupBindingService) reserveOutboundWrite(ctx context.Context, ref SessionRef, record bindingOutboundRecord) (time.Time, error) {
	var until time.Time
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return until, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='500ms'`); err != nil {
		return until, err
	}
	c, err := loadBindingRecord(ctx, tx, record.Challenge, true)
	if err != nil {
		return until, err
	}
	if _, err = s.checkRecord(ctx, tx, ref, c, true); err != nil {
		return until, err
	}
	payload, err := s.openBindingOutbound(record, c)
	if err != nil {
		return until, err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1 AND platform=$2
		AND purpose='management' AND epoch=$3 AND version=$4 FOR NO KEY UPDATE`,
		record.Account, record.Platform, record.Epoch, record.AccountVersion).Scan(&id)
	if err != nil {
		return until, err
	}
	if err = tx.QueryRow(ctx, `SELECT id::text FROM group_binding_outbound_attempts WHERE id=$1
		AND status='submitting' AND attempts_used=1 AND expires_at>clock_timestamp()+interval '3 seconds'
		FOR UPDATE`, record.ID).Scan(&id); err != nil {
		return until, err
	}
	if err = s.outboundFriendWriteAvailable(ctx, tx, c, record, payload, nil); err != nil {
		return until, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO business_rule_account_cooldowns(account_id,next_write_at)
		VALUES($1,'-infinity') ON CONFLICT DO NOTHING`, record.Account); err != nil {
		return until, err
	}
	err = tx.QueryRow(ctx, `UPDATE business_rule_account_cooldowns
		SET next_write_at=clock_timestamp()+make_interval(secs=>$2)
		WHERE account_id=$1 AND next_write_at<=clock_timestamp() RETURNING next_write_at`,
		record.Account, (bindingFriendSubmissionFence + businessWriteGap).Seconds()).Scan(&until)
	if err == pgx.ErrNoRows {
		return until, Denied(409, "business_account_cooling")
	}
	if err != nil {
		return until, err
	}
	return until, tx.Commit(ctx)
}

func outboundRequestMatches(r GroupBindingOutboundRequest, e GroupBindingOutboundEvidence) bool {
	return e.SelfID == r.SelfID && e.Target.NativeID == r.Target.NativeID &&
		e.Target.AddAccount == r.Target.AddAccount && e.Target.NativeID != r.SelfID &&
		e.Platform == r.Platform && e.AccountID == r.AccountID &&
		e.AccountEpoch == r.AccountEpoch && e.AccountVersion == r.AccountVersion
}

func (s *GroupBindingService) SendOutboundFriend(ctx context.Context, ref SessionRef, id string, in GroupBindingConfirmation) (GroupBindingChallenge, error) {
	var out GroupBindingChallenge
	if !ValidID(id) || in.Version < 1 || !onboardingKey(in.Key) {
		return out, Denied(400, "group_binding_outbound_invalid")
	}
	if onboardingControlledContext(ctx) == nil {
		return out, Denied(409, "group_binding_outbound_controlled_only")
	}
	port, ok := s.Native.(GroupBindingOutboundNativePort)
	if !ok || s.Vault == nil {
		return out, Denied(503, "group_binding_outbound_unavailable")
	}
	record, payload, prepared, err := s.prepareOutboundSubmission(ctx, ref, id, in)
	if err != nil {
		return out, err
	}
	if !prepared {
		return s.Read(ctx, ref, id)
	}
	r := payload.Request
	var fences atomic.Int32
	var attempted atomic.Bool
	var controlledRole *onboardingControlledRoles
	if payload.Controlled != nil {
		var c bindingRecord
		c, _, err = s.confirmationRecord(ctx, ref, id, false)
		if err == nil {
			var proof onboardingControlledRoles
			proof, err = s.onboardingControlledRole(ctx, c, payload.Controlled, r.SelfID)
			controlledRole = &proof
		}
	}
	// The native port holds the account's serial reader during its fence.
	// Role reads must finish before entering it; the final transaction checks
	// their original connection and five-second evidence deadline.
	if err == nil {
		err = port.SendBindingFriend(ctx, r, func(work context.Context, e GroupBindingOutboundEvidence, submit func(context.Context) error) error {
			work = context.WithValue(work, onboardingControlledKey{}, onboardingControlledContext(ctx))
			if fences.Add(1) != 1 || submit == nil || !outboundRequestMatches(r, e) || e.State != "available" ||
				e.ReceiverOwner != payload.Connection.ReceiverOwner || e.ReceiverRevision != payload.Connection.ReceiverRevision ||
				e.MobileDeploymentVersion != payload.Connection.MobileDeploymentVersion || e.MobileSessionVersion != payload.Connection.MobileSessionVersion ||
				e.DeploymentVersion != payload.Connection.DeploymentVersion || e.CredentialDigest != payload.Connection.CredentialDigest ||
				!sameOnboardingPeerConnection(r.ControlledPeer, e.Peer) {
				return Denied(409, "group_binding_outbound_target_changed")
			}
			// Queueing and fresh native reads precede the durable write reservation.
			until, err := s.reserveOutboundWrite(work, ref, record)
			if err != nil {
				return err
			}
			final, cancel := context.WithTimeout(work, bindingFriendSubmissionFence)
			defer cancel()
			tx, err := s.DB.Platform.Begin(final)
			if err != nil {
				return err
			}
			defer tx.Rollback(context.WithoutCancel(final))
			if _, err = tx.Exec(final, `SET LOCAL lock_timeout='500ms'`); err != nil {
				return err
			}
			c, err := loadBindingRecord(final, tx, id, true)
			if err != nil {
				return err
			}
			actor, err := s.checkRecord(final, tx, ref, c, true)
			if err != nil {
				return err
			}
			if err = outboundPendingContext(final, c, record.Version); err != nil {
				return err
			}
			if err = requireIncomingContactResolved(final, tx, id); err != nil {
				return err
			}
			if payload.Controlled != nil {
				if !sameOnboardingControlled(payload.Controlled, onboardingControlledContext(final)) {
					return Denied(409, "group_binding_outbound_controlled_only")
				}
				if _, err = s.verifyOnboardingControlled(final, tx, c, payload.Controlled, true, true); err != nil {
					return err
				}
				if err = s.verifyOnboardingControlledRoles(final, tx, c, payload.Controlled, *controlledRole); err != nil {
					return err
				}
			}
			source := onboardingOutboundSource(c, payload.Controlled)
			if _, err = requireOutboundFriendGate(final, tx, c.Platform, source.Account, record.Gate); err != nil {
				return err
			}
			if _, err = s.connection(final, tx, source, &e.GroupBindingConnectionEvidence); err != nil {
				return err
			}
			if e.Peer != nil {
				if _, err = s.connection(final, tx, c, &e.Peer.Connection); err != nil {
					return err
				}
			}
			if err = bindingOutboundTarget(final, tx, c.Platform, r.Target.NativeID, payload.Controlled); err != nil {
				return err
			}
			var operation string
			err = tx.QueryRow(final, `SELECT id::text FROM group_binding_outbound_attempts WHERE id=$1
			AND status='submitting' AND attempts_used=1 AND send_key=$2
			AND expires_at>clock_timestamp()+interval '3 seconds' FOR UPDATE`, record.ID, in.Key).Scan(&operation)
			if err == pgx.ErrNoRows {
				err = Denied(409, "group_binding_outbound_not_sendable")
			}
			if err != nil {
				return err
			}
			var actorID string
			err = tx.QueryRow(final, `SELECT user_id::text FROM sessions WHERE token_hash=$1 AND user_id=$2
			AND expires_at>clock_timestamp()+interval '3 seconds' FOR SHARE`, ref.hash, c.Applicant).Scan(&actorID)
			if err == pgx.ErrNoRows {
				err = Denied(401, "authentication_required")
			}
			if err != nil {
				return err
			}
			if err = tx.QueryRow(final, `SELECT account_id::text FROM business_rule_account_cooldowns
			WHERE account_id=$1 AND next_write_at=$2 AND next_write_at>clock_timestamp()+interval '5 seconds'
			FOR UPDATE`, source.Account, until).Scan(&operation); err != nil {
				if err == pgx.ErrNoRows {
					return Denied(409, "business_account_cooling")
				}
				return err
			}
			if err = s.outboundFriendWriteAvailable(final, tx, c, record, payload, &until); err != nil {
				return err
			}
			if err = final.Err(); err != nil {
				return err
			}
			attempted.Store(true)
			if err = submit(final); err != nil {
				return err
			}
			if _, err = tx.Exec(final, `UPDATE business_rule_account_cooldowns
			SET next_write_at=greatest(next_write_at,clock_timestamp()+interval '5 seconds') WHERE account_id=$1`, source.Account); err != nil {
				return err
			}
			if err = onboardingAudit(final, tx, actor, c.Tenant, "group_binding_outbound_dispatched", record.ID,
				map[string]any{"challenge_id": c.ID, "platform": c.Platform, "version": record.Version}); err != nil {
				return err
			}
			return tx.Commit(final)
		})
	}
	if err == nil && (fences.Load() != 1 || !attempted.Load()) {
		err = Denied(409, "group_binding_outbound_submission_missing")
	}
	status, code := settingOutcome(err, attempted.Load())
	if status == "succeeded" {
		status, code = "submitted", "native_friend_request_acknowledged"
	}
	// The adapter validates the complete native response after the submission
	// fence returns. A later parse/scope failure must remain an unknown result.
	save, cancel := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer cancel()
	_, saveErr := s.DB.Platform.Exec(save, `UPDATE group_binding_outbound_attempts SET status=$2,result_code=$3,
		provider_code=nullif($4::integer,0),
		provider_category=CASE WHEN $4::integer>0 THEN $5::text END,
		provider_observed_at=CASE WHEN $4::integer>0 THEN clock_timestamp() END,
		updated_at=clock_timestamp() WHERE id=$1 AND status='submitting'`,
		record.ID, status, code, outboundSubmitProviderCode(err, attempted.Load()), outboundSubmitCategory(code))
	if saveErr != nil {
		return out, saveErr
	}
	return s.Read(save, ref, id)
}
