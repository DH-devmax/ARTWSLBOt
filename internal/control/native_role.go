package control

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"wangshangliao/internal/nativewire"
)

type RoleObservation struct {
	AccountID       string    `json:"account_id"`
	NativeGroup     string    `json:"native_group_id"`
	GroupID         string    `json:"group_id"`
	Role            string    `json:"role"`
	ObservedAt      time.Time `json:"observed_at"`
	Changed         bool      `json:"changed"`
	IndividualMuted bool      `json:"individual_muted"`
}

// RecordNativeRole accepts evidence only from the internal connection owner.
// The HTTP boundary must never decode RoleEvidence from request JSON. Recording
// it neither claims a group nor enables actions or declares native acceptance.
func RecordNativeRole(ctx context.Context, db *pgxpool.Pool, ref SessionRef, proof nativewire.RoleEvidence) (RoleObservation, error) {
	out := RoleObservation{}
	nativeID, err := strconv.ParseUint(proof.NativeGroup, 10, 64)
	if !ValidID(proof.AccountID) || err != nil || nativeID == 0 ||
		strconv.FormatUint(nativeID, 10) != proof.NativeGroup || proof.UID == 0 ||
		proof.ReceiverOwner == "" || proof.Sender == "" || proof.Team == "" ||
		(proof.Role != "member" && proof.Role != "admin" && proof.Role != "owner") ||
		proof.ObservedAt.IsZero() || time.Since(proof.ObservedAt) > 5*time.Second || proof.ObservedAt.After(time.Now()) {
		return out, Denied(409, "native_role_evidence_required")
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	tx, err := db.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `SET LOCAL lock_timeout='750ms'`); err != nil {
		return out, err
	}
	actor, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return out, err
	}
	if actor.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	var uid string
	err = tx.QueryRow(ctx, `SELECT native_uid FROM accounts WHERE id=$1 AND epoch=$2
        AND platform='wsl' AND purpose='management' AND workspace='wsl'
        AND auth_state='authenticated' AND credential_sealed IS NOT NULL FOR SHARE`,
		proof.AccountID, proof.AccountEpoch).Scan(&uid)
	if err != nil || uid != strconv.FormatUint(proof.UID, 10) {
		return out, Denied(409, "native_role_account_changed")
	}
	var version int64
	err = tx.QueryRow(ctx, `SELECT version FROM native_deployment WHERE singleton FOR SHARE`).Scan(&version)
	if err != nil || version != proof.DeploymentVersion {
		return out, Denied(409, "native_role_deployment_changed")
	}
	var snapshot []byte
	err = tx.QueryRow(ctx, `SELECT snapshot FROM account_receivers WHERE account_id=$1 AND enabled
        AND owner=$2 AND revision=$3 AND lease_until>clock_timestamp()+interval '3 seconds'
        AND observed_at>clock_timestamp()-interval '5 seconds' AND observed_at<=clock_timestamp() FOR SHARE`,
		proof.AccountID, proof.ReceiverOwner, proof.ReceiverRevision).Scan(&snapshot)
	if err != nil {
		return out, Denied(409, "native_role_connection_changed")
	}
	var live struct {
		State     string     `json:"state"`
		Heartbeat *time.Time `json:"heartbeat_at"`
	}
	if json.Unmarshal(snapshot, &live) != nil || live.State != "connected" || live.Heartbeat == nil ||
		live.Heartbeat.After(time.Now()) || time.Since(*live.Heartbeat) > 37*time.Second {
		return out, Denied(409, "native_role_connection_changed")
	}
	name := []rune(proof.Name)
	if len(name) == 0 {
		name = []rune(proof.NativeGroup)
	}
	// A verified unclaimed native group is an observation, not a tenant binding.
	if _, err = tx.Exec(ctx, `INSERT INTO native_groups(platform,native_id,name)
        VALUES('wsl',$1,$2) ON CONFLICT(platform,native_id) DO NOTHING`,
		proof.NativeGroup, string(name[:min(len(name), 200)])); err != nil {
		return out, err
	}
	err = tx.QueryRow(ctx, `SELECT id::text FROM native_groups WHERE platform='wsl' AND native_id=$1 FOR UPDATE`,
		proof.NativeGroup).Scan(&out.GroupID)
	if err != nil {
		return out, err
	}
	var latest time.Time
	err = tx.QueryRow(ctx, `SELECT observed_at FROM account_groups WHERE account_id=$1 AND group_id=$2 FOR UPDATE`,
		proof.AccountID, out.GroupID).Scan(&latest)
	if err == nil && !proof.ObservedAt.After(latest) {
		return out, Denied(409, "native_role_evidence_superseded")
	}
	if err != nil && err != pgx.ErrNoRows {
		return out, err
	}
	out.Changed, err = applyNativeRoleTx(ctx, tx, proof.AccountID, out.GroupID, proof.Role, proof.ObservedAt)
	if err != nil {
		return out, err
	}
	// The proof may have aged while waiting for row locks. It cannot refresh
	// authorization using its completion time or an old receiver observation.
	if time.Since(proof.ObservedAt) > 5*time.Second {
		return RoleObservation{}, Denied(409, "native_role_evidence_expired")
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,resource_id,detail)
        VALUES($1,'native_role_verified',$2,jsonb_build_object('account_id',$3::text,'role',$4::text,
        'individual_muted',$5::boolean))`, actor.ID, out.GroupID, proof.AccountID, proof.Role, proof.IndividualMuted)
	if err != nil {
		return RoleObservation{}, err
	}
	if err = tx.Commit(ctx); err != nil {
		return RoleObservation{}, err
	}
	out.Role, out.ObservedAt, out.IndividualMuted = proof.Role, proof.ObservedAt, proof.IndividualMuted
	out.AccountID, out.NativeGroup = proof.AccountID, proof.NativeGroup
	return out, nil
}
