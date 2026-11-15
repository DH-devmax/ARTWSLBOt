package gateway

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
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
func receiverTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	db, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatal("isolated pool unavailable")
	}
	t.Cleanup(db.Close)
	var name string
	if err = db.QueryRow(ctx, `SELECT current_database()`).Scan(&name); err != nil || name != "wsl_test" {
		t.Fatal("refusing non-test database")
	}
	return db
}

func TestPostgresNativeReceiverDurability(t *testing.T) {
	db := receiverTestDB(t)
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{37}, 32)))
	if err != nil {
		t.Fatal(err)
	}
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
	wakeCalls := 0
	manager.WakeDecoder = func() {
		wakeCalls++
		var committed bool
		if err := db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM native_received_events
            WHERE account_id=$1 AND kind='group')`, id).Scan(&committed); err != nil || !committed {
			t.Fatal("decoder was notified before durable ingress committed")
		}
	}
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
	if wakeCalls != 1 {
		t.Fatal("duplicate delivery needlessly woke decoder")
	}
	friendBody, _ := nativewire.Properties([]nativewire.Property{
		{Key: 1, Value: []byte("synthetic-applicant")}, {Key: 2, Value: []byte("synthetic-recipient")},
		{Key: 7, Value: []byte("9999999999999")},
	})
	friend := ReceivedFrame{Frame: nativewire.Frame{Service: 4, Command: 31, Code: 200, Body: friendBody}, At: time.Now()}
	if err = manager.persistFrames(ctx, w, transport, []ReceivedFrame{friend}); err != nil {
		t.Fatal("valid friend notification paused receiver", err)
	}
	if w.view().ACKs != 2 || w.view().Messages != 2 || wakeCalls != 1 {
		t.Fatal("friend notice entered the chat pipeline")
	}
	var beforeCursor string
	if err = db.QueryRow(ctx, `SELECT cursor::text FROM account_receivers WHERE account_id=$1`, id).Scan(&beforeCursor); err != nil || beforeCursor != "7" {
		t.Fatal("friend timestamp advanced global recovery cursor")
	}
	signalBody, _ := nativewire.Properties([]nativewire.Property{
		{Key: 1, Value: []byte("1")}, {Key: 3, Value: []byte("synthetic-channel")},
		{Key: 4, Value: []byte("9999999999999")}, {Key: 20, Value: []byte("synthetic-call-extension")},
	})
	for _, command := range []byte{11, 12, 13, 14} {
		body := append([]byte(nil), signalBody...)
		if command >= 13 {
			body = append([]byte{1}, body...)
		}
		packet, _ := nativewire.Packet(15, command, 1, body)
		signaling := ReceivedFrame{Frame: nativewire.Frame{Service: 4, Command: 1, Code: 200,
			Body: append(binary.LittleEndian.AppendUint64(nil, ^uint64(0)), packet...)}, At: time.Now()}
		if err = manager.persistFrames(ctx, w, transport, []ReceivedFrame{signaling}); err != nil {
			t.Fatal("valid signaling notification paused receiver", err)
		}
	}
	var notices int
	if err = db.QueryRow(ctx, `SELECT count(*) FROM native_received_events
		WHERE account_id=$1 AND kind='notice'`, id).Scan(&notices); err != nil || notices != 5 {
		t.Fatal("signaling notices were not independently persisted")
	}
	if err = db.QueryRow(ctx, `SELECT cursor::text FROM account_receivers WHERE account_id=$1`, id).Scan(&beforeCursor); err != nil || beforeCursor != "7" {
		t.Fatal("signaling metadata advanced global recovery cursor")
	}
	if w.view().ACKs != 2 || w.view().Messages != 2 || w.view().Quarantined != 0 || wakeCalls != 1 {
		t.Fatal("signaling notices entered the chat pipeline")
	}
	proxyBody, _ := nativewire.Properties([]nativewire.Property{
		{Key: 1, Value: []byte("synthetic-source")}, {Key: 2, Value: []byte("opaque-proxy-body")},
		{Key: 3, Value: []byte("9999999999999")},
	})
	proxy := ReceivedFrame{Frame: nativewire.Frame{Service: 22, Command: 2, Code: 200, Body: proxyBody}, At: time.Now()}
	if err = manager.persistFrames(ctx, w, transport, []ReceivedFrame{proxy}); err != nil {
		t.Fatal("valid proxy notice paused receiver", err)
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM native_received_events
		WHERE account_id=$1 AND kind='notice'`, id).Scan(&notices); err != nil || notices != 6 {
		t.Fatal("proxy notice was not independently persisted")
	}
	if err = db.QueryRow(ctx, `SELECT cursor::text FROM account_receivers WHERE account_id=$1`, id).Scan(&beforeCursor); err != nil || beforeCursor != "7" {
		t.Fatal("proxy timestamp advanced global recovery cursor")
	}
	if w.view().ACKs != 2 || w.view().Messages != 2 || w.view().Quarantined != 0 || wakeCalls != 1 {
		t.Fatal("proxy notice entered the chat pipeline")
	}
	broadcastBody, _ := nativewire.Properties([]nativewire.Property{
		{Key: 1, Value: []byte("synthetic-broadcast")}, {Key: 2, Value: []byte("synthetic-source")},
		{Key: 3, Value: []byte("synthetic-user")}, {Key: 4, Value: []byte("9999999999999")},
		{Key: 5, Value: []byte("opaque-global-broadcast")},
	})
	broadcast := ReceivedFrame{Frame: nativewire.Frame{Service: 7, Command: 17, Code: 200, Body: broadcastBody}, At: time.Now()}
	if err = manager.persistFrames(ctx, w, transport, []ReceivedFrame{broadcast}); err != nil {
		t.Fatal("valid broadcast notice paused receiver", err)
	}
	packet, _ := nativewire.Packet(7, 17, 1, broadcastBody)
	wrappedBroadcast := ReceivedFrame{Frame: nativewire.Frame{Service: 4, Command: 1, Code: 200,
		Body: append(binary.LittleEndian.AppendUint64(nil, ^uint64(0)), packet...)}, At: time.Now()}
	if err = manager.persistFrames(ctx, w, transport, []ReceivedFrame{wrappedBroadcast}); err != nil {
		t.Fatal("valid wrapped broadcast notice paused receiver", err)
	}
	if err = db.QueryRow(ctx, `SELECT count(*) FROM native_received_events
		WHERE account_id=$1 AND kind='notice'`, id).Scan(&notices); err != nil || notices != 8 {
		t.Fatal("broadcast notices were not independently persisted")
	}
	if err = db.QueryRow(ctx, `SELECT cursor::text FROM account_receivers WHERE account_id=$1`, id).Scan(&beforeCursor); err != nil || beforeCursor != "7" {
		t.Fatal("broadcast timestamp advanced global recovery cursor")
	}
	if w.view().ACKs != 2 || w.view().Messages != 2 || w.view().Quarantined != 0 || wakeCalls != 1 {
		t.Fatal("broadcast notice entered the chat pipeline")
	}
	invalidBroadcast := broadcast
	invalidBroadcast.Frame.Body = broadcastBody[:len(broadcastBody)-1]
	broadcastBoundary := ReceivedFrame{Frame: nativewire.Frame{Service: 5, Command: 1, Code: 200,
		Body: binary.LittleEndian.AppendUint64(nil, 999)}, At: time.Now()}
	if err = manager.persistFrames(ctx, w, transport, []ReceivedFrame{invalidBroadcast, broadcastBoundary}); err != nativewire.ErrPushBoundary {
		t.Fatal("malformed broadcast did not quarantine before later watermark")
	}
	invalidProxy := proxy
	invalidProxy.Frame.Body = proxyBody[:len(proxyBody)-1]
	proxyBoundary := ReceivedFrame{Frame: nativewire.Frame{Service: 5, Command: 1, Code: 200,
		Body: binary.LittleEndian.AppendUint64(nil, 999)}, At: time.Now()}
	if err = manager.persistFrames(ctx, w, transport, []ReceivedFrame{invalidProxy, proxyBoundary}); err != nativewire.ErrPushBoundary {
		t.Fatal("malformed proxy notice did not quarantine before later watermark")
	}
	invalidSignal, _ := nativewire.Packet(15, 11, 1, signalBody[:len(signalBody)-1])
	invalidNotice := ReceivedFrame{Frame: nativewire.Frame{Service: 4, Command: 1, Code: 200,
		Body: append(make([]byte, 8), invalidSignal...)}, At: time.Now()}
	laterBoundary := ReceivedFrame{Frame: nativewire.Frame{Service: 5, Command: 1, Code: 200,
		Body: binary.LittleEndian.AppendUint64(nil, 999)}, At: time.Now()}
	if err = manager.persistFrames(ctx, w, transport, []ReceivedFrame{invalidNotice, laterBoundary}); err != nativewire.ErrPushBoundary {
		t.Fatal("malformed signaling did not quarantine before the later watermark")
	}
	friend.Frame.Body = friendBody[:len(friendBody)-1]
	blockedBoundary := ReceivedFrame{Frame: nativewire.Frame{Service: 5, Command: 1, Code: 200, Body: binary.LittleEndian.AppendUint64(nil, 999)}, At: time.Now()}
	if err = manager.persistFrames(ctx, w, transport, []ReceivedFrame{friend, blockedBoundary}); err != nativewire.ErrPushBoundary {
		t.Fatal("malformed friend notification did not quarantine and pause")
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
	if wakeCalls != 1 {
		t.Fatal("rolled-back ingress notified decoder")
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

func TestPostgresReceiverInitialSync(t *testing.T) {
	db := receiverTestDB(t)
	for _, mode := range []string{
		"drop_sync", "durable_sync", "login_burst", "presence_before_heartbeat",
		"session_metadata_before_heartbeat", "session_read_before_heartbeat",
		"friend_operation_before_heartbeat",
		"invalid_boundary", "poison_before_heartbeat",
	} {
		t.Run(mode, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{38}, 32)))
			manager := NewReceiverManager(db, vault, nil)
			var id string
			if err := db.QueryRow(ctx, `INSERT INTO accounts(name,auth_state)
				VALUES('receiver-bootstrap-synthetic','authenticated') RETURNING id::text`).Scan(&id); err != nil {
				t.Fatal(err)
			}
			defer db.Exec(context.Background(), `DELETE FROM accounts WHERE id=$1`, id)
			if _, err := db.Exec(ctx, `INSERT INTO account_receivers(account_id,enabled,owner,lease_until)
				VALUES($1,true,$2,now()+interval '1 minute')`, id, manager.owner); err != nil {
				t.Fatal(err)
			}
			// A delayed commit must leave health at "synchronizing", even when
			// the sync reply and initial heartbeat have already arrived.
			lock, err := db.Begin(ctx)
			if err != nil {
				t.Fatal(err)
			}
			defer lock.Rollback(context.Background())
			if mode == "durable_sync" {
				if _, err = lock.Exec(ctx, `SELECT account_id FROM account_receivers WHERE account_id=$1 FOR UPDATE`, id); err != nil {
					t.Fatal(err)
				}
			}
			syncSent := make(chan struct{})
			upgrader := websocket.Upgrader{}
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				conn, err := upgrader.Upgrade(w, r, nil)
				if err != nil {
					return
				}
				defer conn.Close()
				for {
					_, raw, err := conn.ReadMessage()
					if err != nil {
						return
					}
					f, err := nativewire.ReadPacket(raw)
					if err != nil {
						return
					}
					switch f.Service {
					case 2:
						body, _ := nativewire.Properties([]nativewire.Property{{Key: 102, Value: []byte("synthetic-connection")}})
						if conn.WriteMessage(websocket.BinaryMessage, replyPacket(f, body)) != nil {
							return
						}
						if mode == "presence_before_heartbeat" {
							event, _ := nativewire.Properties([]nativewire.Property{
								{Key: 1, Value: []byte("1")}, {Key: 103, Value: []byte("synthetic-peer")},
							})
							packet, _ := nativewire.Packet(14, 9, 41, append(nativewire.Varint(1), event...))
							if conn.WriteMessage(websocket.BinaryMessage, packet) != nil {
								return
							}
						}
						if mode == "session_metadata_before_heartbeat" {
							context, _ := nativewire.Properties([]nativewire.Property{
								{Key: 1, Value: []byte("0")}, {Key: 2, Value: []byte("1")},
							})
							packet, _ := nativewire.Packet(4, 25, 41, append(context, nativewire.Varint(0)...))
							if conn.WriteMessage(websocket.BinaryMessage, packet) != nil {
								return
							}
						}
						if mode == "friend_operation_before_heartbeat" {
							body, _ := nativewire.Properties([]nativewire.Property{
								{Key: 1, Value: []byte("synthetic-applicant")},
								{Key: 2, Value: []byte("synthetic-recipient")},
								{Key: 7, Value: []byte("9999999999999")},
							})
							packet, _ := nativewire.Packet(4, 31, 41, body)
							if conn.WriteMessage(websocket.BinaryMessage, packet) != nil {
								return
							}
						}
						if mode == "session_read_before_heartbeat" {
							target := "synthetic-peer"
							body := append([]byte{0}, nativewire.Varint(uint64(len(target)))...)
							body = append(body, target...)
							body = binary.LittleEndian.AppendUint64(body, 7)
							body = binary.LittleEndian.AppendUint64(body, 11)
							inner, _ := nativewire.Packet(7, 116, 41, body)
							packet, _ := nativewire.Packet(4, 1, 41, append(make([]byte, 8), inner...))
							if conn.WriteMessage(websocket.BinaryMessage, packet) != nil {
								return
							}
						}
						if mode == "poison_before_heartbeat" {
							packet, _ := nativewire.Packet(99, 99, 42, []byte("synthetic-poison"))
							if conn.WriteMessage(websocket.BinaryMessage, packet) != nil {
								return
							}
						}
						if mode == "login_burst" {
							for i := range 80 {
								body, _ := nativewire.Properties([]nativewire.Property{
									{Key: 0, Value: []byte("1")}, {Key: 1, Value: []byte("synthetic-team")},
									{Key: 2, Value: []byte("synthetic-peer")}, {Key: 7, Value: []byte("1")},
									{Key: 8, Value: []byte("100")}, {Key: 12, Value: []byte(strconv.Itoa(i + 1))},
								})
								packet, _ := nativewire.Packet(8, 3, uint16(i+1), body)
								if conn.WriteMessage(websocket.BinaryMessage, packet) != nil {
									return
								}
							}
						}
					case 1:
						_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(f, nil))
					case 5:
						if mode == "drop_sync" {
							return
						}
						body := binary.LittleEndian.AppendUint64(nil, 88)
						if mode == "invalid_boundary" {
							body = []byte{1, 2, 3}
						}
						_ = conn.WriteMessage(websocket.BinaryMessage, replyPacket(f, body))
						close(syncSent)
					}
				}
			}))
			defer server.Close()
			transport, err := openIM(ctx, websocket.DefaultDialer, "ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer transport.close(context.Canceled)
			previous := time.Now().Add(-time.Hour)
			w := &receiverWorker{spec: receiverSpec{ID: id, Epoch: 1, Revision: 1},
				snapshot: ReceiverSnapshot{SyncedAt: &previous, HeartbeatAt: &previous}}
			done := make(chan error, 1)
			go func() { done <- manager.receiveConnection(ctx, w, transport, "0") }()
			if mode == "drop_sync" || mode == "invalid_boundary" || mode == "poison_before_heartbeat" {
				select {
				case err := <-done:
					if mode == "drop_sync" && receiverBlocked(receiverError(err)) {
						t.Fatal("sync disconnect was confused with poisoned message boundary")
					}
					if mode != "drop_sync" && !errors.Is(err, nativewire.ErrPushBoundary) {
						t.Fatal("unverified sync boundary accepted")
					}
				case <-ctx.Done():
					t.Fatal("initial sync failure did not terminate")
				}
				if w.view().SyncedAt != nil {
					t.Fatal("previous connection's sync evidence survived a new connection")
				}
			} else {
				select {
				case <-syncSent:
				case <-ctx.Done():
					t.Fatal("sync request stalled behind bootstrap pushes")
				}
				if mode == "durable_sync" && (w.view().State != "synchronizing" || w.view().SyncedAt != nil) {
					t.Fatal("socket reply claimed healthy before durable commit")
				}
				if err = lock.Commit(ctx); err != nil {
					t.Fatal(err)
				}
				tick := time.NewTicker(time.Millisecond)
				defer tick.Stop()
				for w.view().SyncedAt == nil {
					select {
					case err := <-done:
						t.Fatalf("receiver exited before sync commit: %v", err)
					case <-ctx.Done():
						t.Fatal("sync commit not observed")
					case <-tick.C:
					}
				}
				view := w.view()
				if view.State != "connected" || view.HeartbeatAt == nil ||
					mode == "login_burst" && (view.Messages != 80 || view.ACKs != 80) {
					t.Fatal("initial sync did not retain bootstrap messages and heartbeat evidence")
				}
				if mode == "presence_before_heartbeat" || mode == "session_metadata_before_heartbeat" ||
					mode == "session_read_before_heartbeat" || mode == "friend_operation_before_heartbeat" {
					var notices int
					if err = db.QueryRow(ctx, `SELECT count(*) FROM native_received_events
						WHERE account_id=$1 AND kind='notice'`, id).Scan(&notices); err != nil {
						t.Fatal(err)
					}
					if notices != 1 || view.Messages != 0 || view.ACKs != 0 || view.Quarantined != 0 {
						t.Fatal("desktop notices must be durable without chat ACKs or quarantine")
					}
				}
				cancel()
				<-done
			}
			var cursor string
			if err = db.QueryRow(context.Background(), `SELECT cursor::text FROM account_receivers WHERE account_id=$1`, id).Scan(&cursor); err != nil {
				t.Fatal(err)
			}
			want := "88"
			if mode == "drop_sync" || mode == "invalid_boundary" || mode == "poison_before_heartbeat" {
				want = "0"
			}
			if cursor != want {
				t.Fatal("sync cursor did not match verified durable boundary")
			}
		})
	}
}
