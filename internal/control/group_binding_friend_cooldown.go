package control

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
)

const bindingFriendSubmissionFence = 3 * time.Second

func bindingFriendWriteAvailable(ctx context.Context, q Queryer, account, operation string, reserved *time.Time) error {
	return bindingFriendWriteAvailableExcept(ctx, q, account, operation, "", reserved)
}

// retiredProof is produced only by the sealed predecessor classifier at each
// controlled outbound fence. Ordinary contact approvals always pass no ID.
func bindingFriendWriteAvailableExcept(ctx context.Context, q Queryer, account, operation, retiredProof string, reserved *time.Time) error {
	var pending, cooling bool
	err := q.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM group_binding_friend_attempts WHERE account_id=$1 AND id::text<>$2
			AND status IN ('prepared','submitting','unknown'))
		OR EXISTS(SELECT 1 FROM customer_group_accept_attempts WHERE account_id=$1
			AND status IN ('prepared','submitting','unknown'))
		OR EXISTS(SELECT 1 FROM native_setting_operations WHERE account_id=$1
			AND status IN ('preparing','submitting','unknown'))
		OR EXISTS(SELECT 1 FROM operations WHERE account_id=$1 AND status IN ('running','unknown')
			AND id::text<>$4)
		OR EXISTS(SELECT 1 FROM business_rule_jobs WHERE account_id=$1
			AND status IN ('executing','submitting','unknown')),
		EXISTS(SELECT 1 FROM business_rule_account_cooldowns WHERE account_id=$1
			AND next_write_at>clock_timestamp() AND next_write_at IS DISTINCT FROM $3::timestamptz)
		OR EXISTS(SELECT 1 FROM native_setting_operations WHERE account_id=$1
			AND updated_at>clock_timestamp()-interval '5 seconds')
		OR EXISTS(SELECT 1 FROM group_binding_friend_attempts WHERE account_id=$1 AND id::text<>$2
			AND attempts_used=1 AND updated_at>clock_timestamp()-interval '5 seconds')`,
		account, operation, reserved, retiredProof).Scan(&pending, &cooling)
	if err != nil {
		return err
	}
	if pending {
		return Denied(409, "group_binding_friend_operation_unresolved")
	}
	if cooling {
		return Denied(409, "business_account_cooling")
	}
	return nil
}

// Commit the shared write gap before any native effect. A rolled-back final
// fence or a process interruption cannot erase this account's reservation.
func (s *GroupBindingService) reserveBindingFriendWrite(ctx context.Context, r GroupBindingFriendApproval) (time.Time, error) {
	var until time.Time
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return until, err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='500ms'`); err != nil {
		return until, err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT id::text FROM accounts WHERE id=$1 AND platform=$2 AND workspace=$2
		AND purpose='management' AND epoch=$3 AND version=$4 FOR NO KEY UPDATE`,
		r.AccountID, r.Platform, r.AccountEpoch, r.AccountVersion).Scan(&id)
	if err == pgx.ErrNoRows {
		return until, Denied(409, "group_binding_friend_scope_changed")
	}
	if err != nil {
		return until, err
	}
	err = tx.QueryRow(ctx, `SELECT id::text FROM group_binding_friend_attempts WHERE id=$1
		AND account_id=$2 AND status='submitting' AND attempts_used=1
		AND expires_at>clock_timestamp()+interval '3 seconds' FOR UPDATE`, r.OperationID, r.AccountID).Scan(&id)
	if err == pgx.ErrNoRows {
		return until, Denied(409, "group_binding_friend_attempt_expired")
	}
	if err != nil {
		return until, err
	}
	if err = bindingFriendWriteAvailable(ctx, tx, r.AccountID, r.OperationID, nil); err != nil {
		return until, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO business_rule_account_cooldowns(account_id,next_write_at)
		VALUES($1,'-infinity') ON CONFLICT DO NOTHING`, r.AccountID); err != nil {
		return until, err
	}
	err = tx.QueryRow(ctx, `UPDATE business_rule_account_cooldowns
		SET next_write_at=clock_timestamp()+make_interval(secs=>$2)
		WHERE account_id=$1 AND next_write_at<=clock_timestamp() RETURNING next_write_at`,
		r.AccountID, (bindingFriendSubmissionFence + businessWriteGap).Seconds()).Scan(&until)
	if err == pgx.ErrNoRows {
		return until, Denied(409, "business_account_cooling")
	}
	if err != nil {
		return until, err
	}
	return until, tx.Commit(ctx)
}

func lockBindingFriendWrite(ctx context.Context, tx pgx.Tx, r GroupBindingFriendApproval, until time.Time) error {
	var account string
	err := tx.QueryRow(ctx, `SELECT account_id::text FROM business_rule_account_cooldowns
		WHERE account_id=$1 AND next_write_at=$2
		AND next_write_at>clock_timestamp()+interval '5 seconds' FOR UPDATE`, r.AccountID, until).Scan(&account)
	if err == pgx.ErrNoRows {
		return Denied(409, "business_account_cooling")
	}
	if err != nil {
		return err
	}
	return bindingFriendWriteAvailable(ctx, tx, r.AccountID, r.OperationID, &until)
}
