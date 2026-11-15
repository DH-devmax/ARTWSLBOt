package control

import (
	"context"
	"crypto/sha256"
	"time"

	"wangshangliao/internal/lajiaoliao"
)

// proof is constructed by the internal native read port, never decoded from
// HTTP or model output. A roster is role evidence, not tenant ownership.
func (s *NativeSettingsService) RecordChiliRole(ctx context.Context, ref SessionRef, account, group, name string, proof SettingEvidence) (RoleObservation, error) {
	out := RoleObservation{AccountID: account, NativeGroup: group, Role: proof.Role, ObservedAt: proof.ObservedAt}
	if !ValidID(account) || !lajiaoliao.ValidID(group) || proof.UID == "" ||
		(proof.Role != "owner" && proof.Role != "admin" && proof.Role != "member") ||
		proof.ObservedAt.After(time.Now()) || time.Since(proof.ObservedAt) > 5*time.Second {
		return out, Denied(409, "native_role_evidence_required")
	}
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return out, err
	}
	defer tx.Rollback(ctx)
	actor, err := sessionActor(ctx, tx, ref, true)
	if err != nil {
		return out, err
	}
	if actor.Role != "superadmin" {
		return out, Denied(403, "superadmin_required")
	}
	var sealed []byte
	var uid string
	err = tx.QueryRow(ctx, `SELECT a.native_uid,a.credential_sealed FROM accounts a
		WHERE a.id=$1 AND a.platform='lajiaoliao' AND a.workspace='lajiaoliao' AND a.purpose='management'
		AND a.auth_state='authenticated' AND a.epoch=$2 AND a.version=$3
		AND EXISTS(SELECT 1 FROM account_receivers ar WHERE ar.account_id=a.id AND ar.enabled
			AND ar.lease_until>clock_timestamp() AND ar.observed_at>now()-interval '20 seconds'
			AND ar.snapshot->>'state'='connected') FOR SHARE OF a`,
		account, proof.Epoch, proof.Version).Scan(&uid, &sealed)
	if err != nil || uid != proof.UID || sha256.Sum256(sealed) != proof.CredentialDigest {
		return out, Denied(409, "native_role_account_changed")
	}
	label := []rune(name)
	if len(label) == 0 {
		label = []rune(group)
	}
	err = tx.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name)VALUES('lajiaoliao',$1,$2)
		ON CONFLICT(platform,native_id)DO UPDATE SET name=excluded.name RETURNING id::text`,
		group, string(label[:min(len(label), 200)])).Scan(&out.GroupID)
	if err != nil {
		return out, err
	}
	out.Changed, err = applyNativeRoleTx(ctx, tx, account, out.GroupID, proof.Role, proof.ObservedAt)
	if err != nil {
		return out, err
	}
	if time.Since(proof.ObservedAt) > 5*time.Second {
		return out, Denied(409, "native_role_evidence_expired")
	}
	_, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_id,action,resource_id,detail)
		VALUES($1,'native_role_verified',$2,jsonb_build_object('account_id',$3::text,'role',$4::text,'platform','lajiaoliao'))`,
		actor.ID, out.GroupID, account, proof.Role)
	if err != nil {
		return out, err
	}
	return out, tx.Commit(ctx)
}
