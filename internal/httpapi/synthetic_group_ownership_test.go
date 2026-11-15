package httpapi

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/control"
	"wangshangliao/internal/database"
	"wangshangliao/internal/security"
)

func syntheticGroupOwnershipTx(t *testing.T, db *database.DB) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := db.Platform.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var name string
	if err = tx.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil || name != "wsl_test" {
		_ = tx.Rollback(ctx)
		t.Fatalf("synthetic ownership requires wsl_test, got %q: %v", name, err)
	}
	return tx
}

func syntheticGroupOwnershipBytes() []byte {
	return []byte(strings.ReplaceAll(control.NewID(), "-", ""))
}

// These are isolated admission fixtures, never native or production evidence.
// Each token has its own 32-byte UUID marker; no SQL hashing privileges are used.
func seedSyntheticGroupOwnership(t *testing.T, db *database.DB, vault *security.Vault, group string) {
	t.Helper()
	ctx := context.Background()
	tx := syntheticGroupOwnershipTx(t, db)
	defer tx.Rollback(ctx)
	var tenant, platform, account, applicant string
	var accountEpoch, accountVersion, applicantVersion, bindingVersion, event int64
	err := tx.QueryRow(ctx, `SELECT b.tenant_id::text,g.platform,a.id::text,u.id::text,
		a.epoch,a.version,u.access_version,b.version,
		coalesce((SELECT max(c.proof_event_id) FROM group_binding_challenges c WHERE c.account_id=a.id),0)+1
		FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
		JOIN accounts a ON a.id=b.primary_account_id JOIN tenants t ON t.id=b.tenant_id
		JOIN users u ON u.id=t.owner_id
		WHERE b.group_id=$1 AND b.active AND a.platform=g.platform
		AND a.workspace=g.platform AND a.purpose='management' AND t.space_kind='tenant_business'
		AND u.tenant_id=t.id AND u.tenant_role='owner' AND u.role='user' AND NOT u.disabled
		FOR SHARE OF b,a,u`, group).
		Scan(&tenant, &platform, &account, &applicant, &accountEpoch, &accountVersion, &applicantVersion, &bindingVersion, &event)
	if err != nil {
		t.Fatal("synthetic ownership needs a bound group and confirmed tenant owner", err)
	}
	identity := []byte("synthetic-http-owner-" + control.NewID())
	owner, err := vault.Seal("group-binding-owner:"+platform+":"+group, identity)
	if err != nil {
		t.Fatal(err)
	}
	applicantIdentity, err := vault.Seal("group-binding-applicant:"+platform+":"+group, identity)
	if err != nil {
		t.Fatal(err)
	}
	challenge := control.NewID()
	if _, err = tx.Exec(ctx, `INSERT INTO group_binding_challenges(id,tenant_id,applicant_id,platform,
		account_id,account_epoch,account_version,applicant_version,token_hash,private_token_hash,
		create_key,request_hash,state,group_id,binding_version,proof_event_id,proof_sealed,proof_role)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$1::uuid::text,$11,'bound',$12,$13,$14,$15,'owner')`,
		challenge, tenant, applicant, platform, account, accountEpoch, accountVersion, applicantVersion,
		syntheticGroupOwnershipBytes(), syntheticGroupOwnershipBytes(), syntheticGroupOwnershipBytes(),
		group, bindingVersion, event, []byte("synthetic-http-proof")); err != nil {
		t.Fatal(err)
	}
	if _, err = tx.Exec(ctx, `INSERT INTO group_binding_ownership(group_id,tenant_id,platform,owner_identity_hash,
		owner_identity_sealed,applicant_identity_sealed,applicant_role,robot_role,proof_challenge_id,proof_event_id)
		VALUES($1,$2,$3,$4,$5,$6,'owner','admin',$7,$8)`,
		group, tenant, platform, syntheticGroupOwnershipBytes(), owner, applicantIdentity, challenge, event); err != nil {
		t.Fatal(err)
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
}

func seedSyntheticPersonalGroupClaim(t *testing.T, db *database.DB, actor, group string) string {
	t.Helper()
	ctx := context.Background()
	tx := syntheticGroupOwnershipTx(t, db)
	defer tx.Rollback(ctx)
	id := control.NewID()
	result, err := tx.Exec(ctx, `INSERT INTO operator_demo_group_claims(id,actor_id,actor_version,tenant_id,
		group_id,platform,account_id,account_epoch,account_version,binding_epoch,binding_version,
		create_key,state,robot_role,owner_identity_hash)
		SELECT $1,u.id,u.access_version,b.tenant_id,b.group_id,g.platform,a.id,a.epoch,a.version,b.epoch,b.version,
		$1::uuid::text,'active','admin',$4 FROM group_bindings b JOIN native_groups g ON g.id=b.group_id
		JOIN accounts a ON a.id=b.primary_account_id JOIN users u ON u.id=$2 WHERE b.group_id=$3`,
		id, actor, group, syntheticGroupOwnershipBytes())
	if err != nil {
		t.Fatal(err)
	}
	if result.RowsAffected() != 1 {
		t.Fatal("synthetic personal claim did not capture one current binding")
	}
	if err = tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return id
}
