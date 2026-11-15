package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"

	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/security"
)

func TestPostgresChiliSettingEvidenceCapturesReceiverGeneration(t *testing.T) {
	db := receiverTestDB(t)
	ctx := context.Background()
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{31}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	var account string
	if err = db.QueryRow(ctx, `INSERT INTO accounts(name,platform,native_uid,auth_state)
		VALUES('synthetic-chili-evidence','lajiaoliao','self-string','authenticated')
		RETURNING id::text`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(ctx, `DELETE FROM accounts WHERE id=$1`, account)
	plain, err := json.Marshal(lajiaoliao.Session{UserID: "self-string", IMToken: "synthetic-im"})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := vault.Seal("lajiaoliao-session:"+account, plain)
	clear(plain)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = db.Exec(ctx, `UPDATE accounts SET credential_sealed=$2 WHERE id=$1`, account, sealed); err != nil {
		t.Fatal(err)
	}
	m := NewReceiverManager(db, vault, nil)
	_, withoutReceiver, err := m.openChiliNative(ctx, account)
	if err != nil || withoutReceiver.ReceiverOwner != "" || withoutReceiver.ReceiverRevision != 0 {
		t.Fatal("account-only reads changed", err)
	}
	if _, err = db.Exec(ctx, `INSERT INTO account_receivers(account_id,owner,revision)
		VALUES($1,'synthetic-first-owner',7)`, account); err != nil {
		t.Fatal(err)
	}
	_, before, err := m.openChiliNative(ctx, account)
	if err != nil || before.ReceiverOwner != "synthetic-first-owner" || before.ReceiverRevision != 7 {
		t.Fatal("native snapshot omitted the receiver generation", err)
	}
	if _, err = db.Exec(ctx, `UPDATE account_receivers SET owner='synthetic-next-owner',revision=8
		WHERE account_id=$1`, account); err != nil {
		t.Fatal(err)
	}
	_, after, err := m.openChiliNative(ctx, account)
	if err != nil || after.ReceiverOwner != "synthetic-next-owner" || after.ReceiverRevision != 8 ||
		before.ReceiverOwner != "synthetic-first-owner" || before.ReceiverRevision != 7 {
		t.Fatal("receiver replacement relabeled an earlier snapshot", err)
	}
	encoded, err := json.Marshal(after)
	if err != nil || strings.Contains(string(encoded), "synthetic-next-owner") ||
		strings.Contains(string(encoded), "receiver_owner") || strings.Contains(string(encoded), "ReceiverRevision") {
		t.Fatal("private receiver evidence leaked into the public snapshot", err)
	}
}
