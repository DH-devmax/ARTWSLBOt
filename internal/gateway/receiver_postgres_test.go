package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jackc/pgx/v5/pgxpool"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

// Run serially after the HTTP suite has migrated wsl_test. This test never
// creates, resets or connects to the application database.
func TestPostgresNativeReceiverDurability(t *testing.T) {
	if os.Getenv("WSL_GATEWAY_INTEGRATION") != "1" {
		t.Skip("isolated receiver integration disabled")
	}
	raw, err := os.ReadFile("../../.env")
	if err != nil {
		t.Fatal("test environment unavailable")
	}
	var dsn string
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := strings.Cut(line, "=")
		if ok && key == "DATABASE_URL" {
			dsn = value
		}
	}
	u, err := url.Parse(dsn)
	if err != nil || u.Hostname() != "127.0.0.1" {
		t.Fatal("explicit localhost test configuration required")
	}
	u.Path = "/wsl_test"
	if os.Getenv("WSL_CONTAINER_TEST") == "1" {
		u.Host = "postgres:5432"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal("isolated pool unavailable")
	}
	defer db.Close()
	var name string
	if err = db.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil || name != "wsl_test" {
		t.Fatal("refusing non-test database")
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{37}, 32)))
	manager := NewReceiverManager(db, vault, nil)
	var id string
	if err = db.QueryRow(ctx, `INSERT INTO accounts(name,auth_state) VALUES('receiver-synthetic','authenticated') RETURNING id::text`).Scan(&id); err != nil {
		t.Fatal(err)
	}
	defer db.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, id)
	_, err = db.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until,cursor)
        VALUES($1,true,$2,now()+interval '1 minute',7)`, id, manager.owner)
	if err != nil {
		t.Fatal(err)
	}
	w := &receiverWorker{spec: receiverSpec{ID: id, Epoch: 1, Revision: 1}}
	observed := make(chan bool, 4)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		for {
			_, packet, err := conn.ReadMessage()
			if err != nil {
				return
			}
			f, err := nativewire.ReadPacket(packet)
			var persisted bool
			e := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM native_received_events WHERE account_id=$1 AND kind='group')`, id).Scan(&persisted)
			observed <- err == nil && e == nil && persisted && f.Service == 4 && f.Command == 5
		}
	}))
	defer server.Close()
	socket, _, err := websocket.DefaultDialer.DialContext(ctx, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &IMTransport{socket: socket, done: make(chan struct{}), pending: make(map[[3]uint16]chan nativewire.Frame)}
	defer transport.close(context.Canceled)
	body, _ := nativewire.Properties([]nativewire.Property{
		{Key: 0, Value: []byte("1")}, {Key: 1, Value: []byte("nim-team")}, {Key: 2, Value: []byte("peer")},
		{Key: 7, Value: []byte("1")}, {Key: 8, Value: []byte("100")}, {Key: 12, Value: []byte("123")},
	})
	frame := ReceivedFrame{Frame: nativewire.Frame{Service: 8, Command: 3, Code: 200, Body: body}, At: time.Now()}
	for range 2 {
		if err = manager.persistFrames(ctx, w, transport, []ReceivedFrame{frame}); err != nil {
			t.Fatal(err)
		}
		select {
		case durable := <-observed:
			if !durable {
				t.Fatal("ACK preceded durable commit")
			}
		case <-ctx.Done():
			t.Fatal("ACK missing")
		}
	}
	if w.view().Duplicates != 1 || w.view().ACKs != 2 {
		t.Fatal("duplicate accounting or ACK incorrect")
	}
	if w.view().Received60s != 2 {
		t.Fatal("receive rate lost duplicate delivery observations")
	}
	poison := ReceivedFrame{Frame: nativewire.Frame{Service: 99, Command: 99, Code: 200, Body: []byte("bad")}, At: time.Now()}
	boundary := ReceivedFrame{Frame: nativewire.Frame{Service: 5, Command: 1, Code: 200, Body: binary.LittleEndian.AppendUint64(nil, 999)}, At: time.Now()}
	if err = manager.persistFrames(ctx, w, transport, []ReceivedFrame{poison, boundary}); err != nativewire.ErrPushBoundary {
		t.Fatal("poison did not pause")
	}
	var cursor string
	if err = db.QueryRow(ctx, `SELECT cursor::text FROM account_receivers WHERE account_id=$1`, id).Scan(&cursor); err != nil || cursor != "7" {
		t.Fatal("advanced past unknown boundary")
	}
	if _, err = db.Exec(ctx, `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = manager.persistFrames(ctx, w, transport, []ReceivedFrame{frame}); err == nil {
		t.Fatal("stale account epoch accepted")
	}
	var sealed []byte
	if err = db.QueryRow(ctx, `SELECT payload_sealed FROM native_received_events WHERE account_id=$1 AND event_key='1:nim-team:123'`, id).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(sealed, []byte("nim-team")) {
		t.Fatal("plaintext stored")
	}
	if _, err = vault.Open("native-event:"+id+":wrong", sealed); err == nil {
		t.Fatal("ciphertext scope not bound")
	}
	if err = db.QueryRow(ctx, `SELECT count(*)::text FROM native_groups WHERE native_id='nim-team'`).Scan(&cursor); err != nil || cursor != "0" {
		t.Fatal("unverified IM target created business group")
	}
}
