package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"slices"
	"testing"
	"time"

	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

func TestPostgresReceiverFriendSyncPreservesWatermark(t *testing.T) {
	db := receiverTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{38}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	manager := NewReceiverManager(db, vault, nil)
	var account string
	if err = db.QueryRow(ctx, `INSERT INTO accounts(name,auth_state) VALUES('friend-sync-synthetic','authenticated')
		RETURNING id::text`).Scan(&account); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, account)
	if _, err = db.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until,cursor)
		VALUES($1,true,$2,now()+interval '1 minute',7)`, account, manager.owner); err != nil {
		t.Fatal(err)
	}
	worker := &receiverWorker{spec: receiverSpec{ID: account, Epoch: 1, Revision: 1}}
	wakes := 0
	manager.WakeDecoder = func() { wakes++ }
	peer := append(nativewire.Varint(10), []byte("sdk-peer-1")...)
	request := append(slices.Clone(peer), 1, 0)
	update, _ := nativewire.Properties([]nativewire.Property{{Key: 1, Value: []byte("sdk-peer-1")}})
	props, _ := nativewire.Properties([]nativewire.Property{{Key: 1, Value: []byte("x")}})
	extended := append(slices.Clone(request), props...)
	message, _ := nativewire.Properties([]nativewire.Property{
		{Key: 0, Value: []byte("1")}, {Key: 1, Value: []byte("sdk-peer-1")},
		{Key: 2, Value: []byte("owner")}, {Key: 7, Value: []byte("1")},
		{Key: 8, Value: []byte("9999999999999")}, {Key: 12, Value: []byte("123")},
	})
	wrap := func(command, wrapper byte, body []byte) ReceivedFrame {
		t.Helper()
		frame := nativewire.Frame{Service: 12, Command: command, Code: 200, Body: body}
		if wrapper != 0 {
			packet, err := nativewire.Packet(12, command, 0, body)
			if err != nil {
				t.Fatal(err)
			}
			frame = nativewire.Frame{Service: 4, Command: wrapper, Code: 200,
				Body: append(binary.LittleEndian.AppendUint64(nil, ^uint64(0)), packet...)}
		}
		return ReceivedFrame{Frame: frame, At: time.Now()}
	}
	if len(extended) != 17 || len(wrap(101, 1, extended).Frame.Body) != 31 {
		t.Fatal("synthetic request must have 17-byte inner and 31-byte outer bodies")
	}
	valid := []struct {
		command byte
		body    []byte
	}{
		{101, request},
		{101, append(slices.Clone(request), 0)},
		{101, extended},
		{101, append(slices.Clone(request), message...)},
		{102, peer},
		{103, update},
	}
	expectedNotices := 0
	for _, tc := range valid {
		for _, wrapper := range []byte{0, 1, 2, 10, 11} {
			if err = manager.persistFrames(ctx, worker, nil, []ReceivedFrame{wrap(tc.command, wrapper, tc.body)}); err != nil {
				t.Fatalf("verified friend notification blocked receive: cmd=%d wrapper=%d err=%v", tc.command, wrapper, err)
			}
			expectedNotices++
		}
	}
	var notices int
	var cursor string
	if err = db.QueryRow(ctx, `SELECT count(*) FROM native_received_events WHERE account_id=$1 AND kind='notice'`,
		account).Scan(&notices); err != nil || notices != expectedNotices {
		t.Fatal("friend metadata not durably retained", err)
	}
	if err = db.QueryRow(ctx, `SELECT cursor::text FROM account_receivers WHERE account_id=$1`, account).
		Scan(&cursor); err != nil || cursor != "7" {
		t.Fatal("friend metadata changed recovery boundary", err)
	}
	view := worker.view()
	if view.ACKs != 0 || view.Messages != 0 || view.Quarantined != 0 || wakes != 0 {
		t.Fatal("friend metadata entered chat processing")
	}
	duplicate, _ := nativewire.Properties([]nativewire.Property{
		{Key: 1, Value: []byte("a")}, {Key: 1, Value: []byte("b")},
	})
	invalid := []struct {
		command byte
		body    []byte
	}{
		{101, extended[:len(extended)-1]},
		{101, append(slices.Clone(request), 0x81)},
		{101, append(slices.Clone(request), duplicate...)},
		{101, append(slices.Clone(request), nativewire.Varint(129)...)},
		{101, append(slices.Clone(extended), 0)},
		{101, append(slices.Clone(request), 0, 0)},
		{102, peer[:len(peer)-1]},
		{102, append(slices.Clone(peer), 0)},
		{103, append(slices.Clone(update), 0)},
	}
	boundary := ReceivedFrame{Frame: nativewire.Frame{Service: 5, Command: 1, Code: 200,
		Body: binary.LittleEndian.AppendUint64(nil, 999)}, At: time.Now()}
	expectedQuarantined := 0
	for _, tc := range invalid {
		for _, wrapper := range []byte{0, 1, 2, 10, 11} {
			bad := wrap(tc.command, wrapper, tc.body)
			if err = manager.persistFrames(ctx, worker, nil, []ReceivedFrame{bad, boundary}); err != nativewire.ErrPushBoundary {
				t.Fatalf("malformed friend notification did not block later boundary: cmd=%d wrapper=%d err=%v",
					tc.command, wrapper, err)
			}
			expectedQuarantined++
		}
	}
	if err = db.QueryRow(ctx, `SELECT cursor::text FROM account_receivers WHERE account_id=$1`, account).
		Scan(&cursor); err != nil || cursor != "7" {
		t.Fatal("malformed friend notification skipped recovery boundary", err)
	}
	var quarantined int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM native_received_events WHERE account_id=$1 AND kind='quarantined'`,
		account).Scan(&quarantined); err != nil || quarantined != expectedQuarantined {
		t.Fatal("malformed notification not preserved without side effects", err)
	}
	view = worker.view()
	if view.ACKs != 0 || view.Messages != 0 || view.Quarantined != int64(expectedQuarantined) || wakes != 0 {
		t.Fatal("friend notification or quarantine entered chat processing")
	}
	var effects bool
	if err = db.QueryRow(ctx, `SELECT
		EXISTS(SELECT 1 FROM native_received_events WHERE account_id=$1 AND kind NOT IN ('notice','quarantined'))
		OR EXISTS(SELECT 1 FROM native_decoded_events WHERE account_id=$1)
		OR EXISTS(SELECT 1 FROM native_directory_snapshots WHERE account_id=$1)
		OR EXISTS(SELECT 1 FROM account_groups WHERE account_id=$1)
		OR EXISTS(SELECT 1 FROM native_setting_operations WHERE account_id=$1)
		OR EXISTS(SELECT 1 FROM group_binding_friend_gates WHERE account_id=$1)
		OR EXISTS(SELECT 1 FROM group_binding_friend_attempts WHERE account_id=$1)
		OR EXISTS(SELECT 1 FROM group_binding_friend_proofs WHERE account_id=$1)`, account).
		Scan(&effects); err != nil || effects {
		t.Fatal("friend notice changed messages, directory, friendship admission or group authority", err)
	}
}
