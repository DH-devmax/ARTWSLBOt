package control

import (
	"context"

	"github.com/jackc/pgx/v5"
)

// The account lock held by authorize serializes this check with preparation.
// The durable operation then excludes other writes until its result is known.
func friendSocialWriteAvailable(ctx context.Context, tx pgx.Tx, req SettingRequest, receiverCode string) error {
	return friendSocialWriteAvailableWithContactRecovery(ctx, tx, req, receiverCode, nil)
}

// Only the synchronous controlled-delete caller can carry this private,
// server-classified snapshot. The ordinary wrapper never excludes any work.
func friendSocialWriteAvailableWithContactRecovery(ctx context.Context, tx pgx.Tx, req SettingRequest,
	receiverCode string, recovery *onboardingContactRecovery) error {
	retired := ""
	if recovery != nil {
		snapshot := recovery.snapshot
		if recovery != req.contactRecovery || req.business != nil || req.Workspace != "lajiaoliao" ||
			req.Change.Kind != "friend_delete" || snapshot == nil || !ValidID(recovery.actor) ||
			!ValidID(snapshot.RetiredOperation) || snapshot.Account != req.Account ||
			snapshot.Window != req.TestWindowID || snapshot.NativeGroup != req.Change.Group ||
			snapshot.Recipient != req.Change.Target {
			return Denied(409, "native_setting_pending")
		}
		retired = snapshot.RetiredOperation
	}
	var connected bool
	err := tx.QueryRow(ctx, `SELECT enabled AND owner<>'' AND lease_until>clock_timestamp()+interval '3 seconds'
		AND observed_at>clock_timestamp()-interval '15 seconds' AND snapshot->>'state'='connected'
		FROM account_receivers WHERE account_id=$1 FOR SHARE`, req.Account).Scan(&connected)
	if err == pgx.ErrNoRows || err == nil && !connected {
		return Denied(409, receiverCode)
	}
	if err != nil {
		return err
	}
	var pending, cooling bool
	err = tx.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM native_setting_operations WHERE account_id=$1 AND idempotency_key<>$2
			AND status IN ('preparing','submitting','unknown'))
		OR EXISTS(SELECT 1 FROM operations WHERE account_id=$1 AND status IN ('running','unknown') AND id::text<>$3)
		OR EXISTS(SELECT 1 FROM group_binding_friend_attempts WHERE account_id=$1 AND status IN ('prepared','submitting','unknown'))
		OR EXISTS(SELECT 1 FROM group_binding_outbound_attempts WHERE account_id=$1 AND status IN ('prepared','submitting','unknown'))
		OR EXISTS(SELECT 1 FROM customer_group_accept_attempts WHERE account_id=$1 AND status IN ('prepared','submitting','unknown'))
		OR EXISTS(SELECT 1 FROM business_rule_jobs WHERE account_id=$1 AND status IN ('executing','submitting','unknown')),
		EXISTS(SELECT 1 FROM business_rule_account_cooldowns WHERE account_id=$1 AND next_write_at>clock_timestamp())
		OR EXISTS(SELECT 1 FROM native_setting_operations WHERE account_id=$1 AND idempotency_key<>$2
			AND updated_at>clock_timestamp()-interval '5 seconds')
		OR EXISTS(SELECT 1 FROM group_binding_friend_attempts WHERE account_id=$1
			AND attempts_used=1 AND updated_at>clock_timestamp()-interval '5 seconds')
		OR EXISTS(SELECT 1 FROM group_binding_outbound_attempts WHERE account_id=$1
			AND attempts_used=1 AND updated_at>clock_timestamp()-interval '5 seconds')`,
		req.Account, req.Key, retired).Scan(&pending, &cooling)
	if err != nil {
		return err
	}
	if pending {
		return Denied(409, "native_setting_pending")
	}
	if cooling {
		return Denied(409, "business_account_cooling")
	}
	return nil
}
