package control

import (
	"testing"

	"wangshangliao/internal/security"
)

func TestGroupBindingPostgresTransferOwnerAnchor(t *testing.T) {
	f := newBusinessFixture(t)
	account := f.account("lajiaoliao", 1)
	source := f.group("lajiaoliao", account, f.tenant, 1, true)
	f.ownership(source)
	target, applicant := f.customer()
	admin, challenge := NewID(), NewID()
	f.exec(`INSERT INTO users(id,username,password_hash,role)
		VALUES($1::uuid,$1::uuid::text,'synthetic-unused-password','superadmin')`, admin)
	f.exec(`INSERT INTO group_binding_challenges(id,tenant_id,applicant_id,platform,account_id,
		account_epoch,account_version,applicant_version,token_hash,private_token_hash,create_key,request_hash,
		state,group_id,binding_version,transfer_from_tenant_id,proof_event_id,proof_sealed,proof_role)
		VALUES($1::uuid,$2,$3,$4,$5,$6,$7,1,$8,$9,$1::uuid::text,$8,'review',$10,$11,$12,2,$13,'owner')`,
		challenge, target, applicant, source.Platform, account, source.AccountEpoch, source.AccountVersion,
		security.Digest(challenge), security.Digest("private:"+challenge), source.Group,
		source.BindingVersion, source.Tenant, []byte("synthetic-authenticated-owner-proof"))
	service := GroupBindingService{DB: f.db, Vault: f.service.Vault}
	tx, err := f.db.Platform.Begin(f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.ctx)
	if _, err = tx.Exec(f.ctx, `SELECT pg_advisory_xact_lock(798423)`); err != nil {
		t.Fatal(err)
	}
	c, err := loadBindingRecord(f.ctx, tx, challenge, true)
	if err != nil {
		t.Fatal(err)
	}
	if err = service.transferBinding(f.ctx, tx, Actor{ID: admin, Role: "superadmin"}, c, "explicit-anchor-transfer"); err != nil {
		t.Fatal(err)
	}
	if err = service.saveOwnership(f.ctx, tx, c, bindingProof{Event: 2, Owner: "target-native-owner",
		Sender: "target-native-owner", SenderRole: "owner", RobotRole: "admin"}); err != nil {
		t.Fatal("controlled replacement of the existing source anchor failed", err)
	}
	if _, err = tx.Exec(f.ctx, `UPDATE group_binding_challenges SET state='bound' WHERE id=$1`, challenge); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(f.ctx); err != nil {
		t.Fatal(err)
	}
	current := source
	current.Tenant, current.Actor = target, applicant
	if err = requireBusinessOwnerAnchor(f.ctx, f.db.Platform, current, "target-native-owner"); err != nil {
		t.Fatal("business fence rejected the transferred active owner anchor", err)
	}
	if err = requireBusinessOwnerAnchor(f.ctx, f.db.Platform, source, "native-owner"); err == nil {
		t.Fatal("the source tenant retained business ownership after transfer")
	}
	if err = requireBusinessOwnerAnchor(f.ctx, f.db.Platform, current, "native-owner"); err == nil {
		t.Fatal("the new tenant accepted the previous native owner identity")
	}
	scope, err := service.OwnershipScope(f.ctx, source.Platform, source.Group)
	if err != nil || scope.NativeSenderID != "target-native-owner" || scope.AccountID != account {
		t.Fatal("OwnershipScope returned the previous tenant's anchor", err)
	}
	if f.count(`SELECT count(*) FROM group_binding_ownership WHERE group_id=$1 AND tenant_id=$2
		AND state='active' AND proof_challenge_id=$3`, source.Group, target, challenge) != 1 {
		t.Fatal("ownership upsert succeeded without installing the current active anchor")
	}
}
