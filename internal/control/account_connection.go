package control

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5"
)

// RequireTestConnectionTx admits only the controlled test path. Submission still
// needs its own grant, role proof, package and current account epoch.
func RequireTestConnectionTx(ctx context.Context, tx pgx.Tx, ref SessionRef, account string, version int64) error {
	actor, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return err
	}
	if actor.Role != "superadmin" {
		return Denied(403, "superadmin_required")
	}
	var eligible bool
	var current int64
	err = tx.QueryRow(ctx, `SELECT platform IN('wsl','lajiaoliao') AND workspace=platform AND purpose='management'
        AND testing_enabled AND auth_state='authenticated' AND credential_sealed IS NOT NULL,version
        FROM accounts WHERE id=$1 FOR UPDATE`, account).Scan(&eligible, &current)
	if err != nil {
		return err
	}
	if current != version {
		return Denied(409, "version_conflict")
	}
	if !eligible {
		return Denied(409, "native_compatibility_not_verified")
	}
	var raw []byte
	err = tx.QueryRow(ctx, `SELECT snapshot FROM account_receivers WHERE account_id=$1 AND enabled
        AND owner<>'' AND lease_until>clock_timestamp()+interval '3 seconds'
        AND observed_at>clock_timestamp()-interval '15 seconds' FOR SHARE`, account).Scan(&raw)
	if err == pgx.ErrNoRows {
		return Denied(409, "native_test_receiver_required")
	}
	if err != nil {
		return err
	}
	var snapshot struct {
		State       string     `json:"state"`
		SyncedAt    *time.Time `json:"synced_at"`
		HeartbeatAt *time.Time `json:"heartbeat_at"`
	}
	if json.Unmarshal(raw, &snapshot) != nil || snapshot.State != "connected" || snapshot.SyncedAt == nil ||
		snapshot.HeartbeatAt == nil || snapshot.HeartbeatAt.After(time.Now().Add(time.Second)) ||
		time.Since(*snapshot.HeartbeatAt) > 37*time.Second {
		return Denied(409, "native_test_receiver_required")
	}
	return nil
}
