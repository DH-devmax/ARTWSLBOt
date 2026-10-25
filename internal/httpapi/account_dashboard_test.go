package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/security"
)

func TestDashboardHealth(t *testing.T) {
	now := time.Now()
	old := now.Add(-31 * time.Second)
	out := accountDashboard{SampledAt: now, BacklogThreshold: 30}
	out.Account.AuthState = "authenticated"
	out.Account.ConnectionEnabled = true
	out.Account.RuntimeState = "connected"
	health, backlog := classifyAccountDashboard(out)
	if health.Status != "unobserved" || backlog.Status != "empty" {
		t.Fatal("stored connection state and empty inbox must not claim live health")
	}
	out.Inbox.Pending = 1
	out.Inbox.OldestPending = &old
	health, backlog = classifyAccountDashboard(out)
	if health.Status != "backlogged" || backlog.Status != "backlogged" {
		t.Fatal("old pending work must flag backlog")
	}
	out.Operations.Unknown = 1
	health, backlog = classifyAccountDashboard(out)
	if health.Status != "attention" || backlog.Status != "attention" {
		t.Fatal("unknown results require review even when also backlogged")
	}
}

func TestPostgresAccountDashboard(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	password := "dashboard-isolated-password-123"
	hash, _ := security.Password(password)
	if _, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role)
        VALUES('dashboard-admin',$1,'superadmin')`, hash); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{23}, 32)))
	s := New(db, vault, "http://127.0.0.1:5173", false)
	t.Cleanup(s.Native.Close)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	login := func(name string) testClient {
		jar, _ := cookiejar.New(nil)
		c := testClient{client: &http.Client{Jar: jar, Timeout: 10 * time.Second}, base: server.URL}
		expect(t, c, "POST", "/auth/login", map[string]string{"username": name, "password": password}, 200)
		return c
	}
	admin := login("dashboard-admin")
	chat := admin
	chat.workspace = "maibot"
	tenant := func(name string) (testClient, string, string) {
		body := expect(t, admin, "POST", "/users", map[string]string{
			"username": name, "password": password, "tenant_name": name,
		}, 201)
		return login(name), field(t, body, "tenant_id"), field(t, body, "id")
	}
	alice, tenantA, aliceID := tenant("dashboard-alice")
	bob, tenantB, _ := tenant("dashboard-bob")
	account := func(c testClient, name string) string {
		return field(t, expect(t, c, "POST", "/accounts", map[string]string{"name": name}, 201), "id")
	}
	shared, separate, ai := account(admin, "dashboard-shared"), account(admin, "dashboard-separate"), account(chat, "dashboard-chat")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	group := func(name, tenant, primary, chat string) string {
		t.Helper()
		var id string
		if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(native_id,name) VALUES($1,$1) RETURNING id::text`, name).Scan(&id); err != nil {
			t.Fatal(err)
		}
		exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,ai_account_id,management_enabled,ai_enabled)
            VALUES($1,$2,$3,nullif($4,'')::uuid,true,$4<>'')`, id, tenant, primary, chat)
		return id
	}
	groupA := group("dashboard-group-a", tenantA, shared, ai)
	groupB := group("dashboard-group-b", tenantB, shared, "")
	groupC := group("dashboard-group-c", tenantB, separate, "")
	// Large fixtures prove account counts are SQL aggregates, not the UI's
	// paginated lists. No native receiver or sender is invoked.
	now := time.Now().UTC().Add(-2 * time.Second)
	exec(`INSERT INTO inbox(account_id,group_id,native_message_id,sender_id,received_at,persisted_at,state)
        SELECT $1,$2,'a-'||n,'private-sender-a',$3::timestamptz-interval '20 milliseconds',$3,'processed'
        FROM generate_series(1,250) n`, shared, groupA, now)
	exec(`INSERT INTO inbox(account_id,group_id,native_message_id,sender_id,received_at,persisted_at,state)
        VALUES($1,$2,'clock','private-sender-a',$3::timestamptz+interval '1 second',$3,'unresolved'),
            ($1,$2,'old','private-sender-a',$3::timestamptz-interval '2 minutes',$3::timestamptz-interval '2 minutes','pending')`,
		shared, groupA, now)
	exec(`INSERT INTO inbox(account_id,group_id,native_message_id,sender_id,received_at,persisted_at,state)
        SELECT $1,$2,'b-'||n,'private-sender-b',$3::timestamptz-interval '900 milliseconds',$3,'quarantined'
        FROM generate_series(1,3) n`, shared, groupB, now)
	exec(`INSERT INTO inbox(account_id,group_id,native_message_id,sender_id,received_at,persisted_at,state)
        VALUES($1,$2,'other','private-sender-c',$3,$3,'pending')`, separate, groupC, now)
	exec(`INSERT INTO operations(tenant_id,group_id,account_id,idempotency_key,request_hash,kind,account_epoch,
        expires_at,status,created_at) VALUES
        ($1,$2,$3,'dashboard-a','\x01','send_text',1,now()+interval '1 hour','queued',now()-interval '40 seconds'),
        ($4,$5,$3,'dashboard-b','\x01','send_text',1,now()+interval '1 hour','unknown',now())`,
		tenantA, groupA, shared, tenantB, groupB)
	fetch := func(c testClient, account string) accountDashboard {
		t.Helper()
		raw := expect(t, c, "GET", "/accounts/"+account+"/dashboard?tenant_id="+tenantB, nil, 200)
		for _, forbidden := range []string{"private-sender", "credential", "payload_sealed", tenantB, groupB} {
			if strings.Contains(string(raw), forbidden) {
				t.Fatalf("dashboard leaked sensitive field: %s", forbidden)
			}
		}
		var out accountDashboard
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	all := fetch(admin, shared)
	if all.Groups.Authorized != 2 || all.Performance.Inserted != 254 || all.Inbox.Quarantined != 3 ||
		all.Operations.Unknown != 1 || all.Health.Status != "attention" || all.RuntimeObserved {
		t.Fatalf("wrong account snapshot: %+v", all)
	}
	a := fetch(alice, shared)
	if a.Visibility != "tenant_groups" || a.Groups.Authorized != 1 || a.Performance.Inserted != 251 ||
		a.Inbox.Pending != 1 || a.Inbox.Unresolved != 1 || a.Inbox.Quarantined != 0 ||
		a.Operations.Queued != 1 || a.Operations.Unknown != 0 || a.Health.Status != "backlogged" {
		t.Fatalf("tenant scope escaped: %+v", a)
	}
	if a.Performance.LatencySamples != 250 || a.Performance.InvalidTiming != 1 || a.Performance.P95 == nil ||
		math.Abs(*a.Performance.P95-20) > 0.01 || math.Abs(a.Performance.PerSecond-251.0/60) > 0.001 {
		t.Fatalf("wrong measured latency/rate: %+v", a.Performance)
	}
	total := 0
	for i, bucket := range a.Performance.Series {
		total += bucket.Count
		if i > 0 && bucket.At.Sub(a.Performance.Series[i-1].At) != time.Minute {
			t.Fatal("chart has unordered or missing minutes")
		}
	}
	if len(a.Performance.Series) != 15 || total != 252 {
		t.Fatalf("wrong chart window: %+v", a.Performance.Series)
	}
	b := fetch(bob, shared)
	if b.Performance.Inserted != 3 || b.Inbox.Quarantined != 3 || b.Operations.Queued != 0 || b.Operations.Unknown != 1 {
		t.Fatal("shared robot leaked Alice metrics to Bob")
	}
	expect(t, alice, "GET", "/accounts/"+separate+"/dashboard", nil, 404)
	expect(t, admin, "GET", "/accounts/"+ai+"/dashboard", nil, 404)
	expect(t, chat, "GET", "/accounts/"+shared+"/dashboard", nil, 404)
	empty := fetch(chat, ai)
	if empty.Groups.Authorized != 1 || empty.Performance.Inserted != 0 || empty.Performance.P95 != nil ||
		empty.Performance.LastInserted != nil || empty.Health.Status != "not_authenticated" {
		t.Fatalf("empty chat account claimed performance: %+v", empty)
	}
	tenantChat := alice
	tenantChat.workspace = "maibot"
	expect(t, tenantChat, "GET", "/accounts/"+ai+"/dashboard", nil, 403)
	exec(`INSERT INTO user_access(user_id,role_code,permissions) VALUES($1,'custom',ARRAY['overview:read'])`, aliceID)
	expect(t, alice, "GET", "/accounts/"+shared+"/dashboard", nil, 403)
	exec(`UPDATE user_access SET permissions=ARRAY['accounts:read'] WHERE user_id=$1`, aliceID)
	expect(t, alice, "GET", "/accounts/"+shared+"/dashboard", nil, 403)
	exec(`UPDATE user_access SET permissions=ARRAY['overview:read','accounts:read'] WHERE user_id=$1`, aliceID)
	// Losing active group authority removes ingress metrics, even if the retained
	// ownership still permits reading the robot's safe connection record.
	exec(`UPDATE group_bindings SET active=false WHERE group_id=$1`, groupA)
	revoked := fetch(alice, shared)
	if revoked.Performance.Inserted != 0 || revoked.Inbox.Pending != 0 || revoked.Groups.Authorized != 0 {
		t.Fatal("inactive binding still granted shared ingress metrics")
	}
	// No read above changes connection flags, epochs or readiness gates.
	var epoch int
	if err := db.Platform.QueryRow(ctx, `SELECT epoch FROM accounts WHERE id=$1`, shared).Scan(&epoch); err != nil || epoch != 1 {
		t.Fatal("dashboard changed account lifecycle")
	}
}
