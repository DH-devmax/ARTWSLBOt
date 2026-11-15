package httpapi

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"math"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/gateway"
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
	out.Inbox = dashboardInbox{}
	out.Operations = dashboardOperations{}
	out.Receiver = &receiverView{Enabled: true, Observed: true, Snapshot: &gateway.ReceiverSnapshot{State: "connected", HeartbeatAt: &now}}
	out.RuntimeObserved = true
	health, _ = classifyAccountDashboard(out)
	if health.Status != "unobserved" {
		t.Fatal("heartbeat without durable initial sync claimed healthy")
	}
	out.Receiver.Snapshot.SyncedAt = &now
	health, _ = classifyAccountDashboard(out)
	if health.Status != "healthy" {
		t.Fatal("fresh connected receiver was not observed")
	}
	out.Receiver.Snapshot.State = "blocked"
	out.RuntimeObserved = false
	health, _ = classifyAccountDashboard(out)
	if health.Status != "attention" {
		t.Fatal("persisted blocked state lost after owner shutdown")
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
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
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
		explicitTestCeiling(t, admin, field(t, body, "tenant_id"))
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
	if a.Receiver != nil {
		t.Fatal("tenant received shared account runtime counters")
	}
	if a.Visibility != "tenant_groups" || a.Groups.Authorized != 1 || a.Performance.Inserted != 251 ||
		a.Inbox.Pending != 1 || a.Inbox.Unresolved != 1 || a.Inbox.Quarantined != 0 ||
		a.Operations.Queued != 1 || a.Operations.Unknown != 0 || a.Health.Status != "backlogged" {
		t.Fatalf("tenant scope escaped: %+v", a)
	}
	if a.Performance.LatencySamples != 250 || a.Performance.InvalidTiming != 1 || a.Performance.P95 == nil ||
		math.Abs(*a.Performance.P95-20) > 0.01 || math.Abs(a.Performance.PerSecond-251.0/60) > 0.001 {
		t.Fatalf("wrong measured latency/rate: %+v", a.Performance)
	}
	// More than 1000 persisted observations; retained replay and a different
	// workspace must not inflate the latest account/workspace sample population.
	exec(`WITH events AS (
        INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
        SELECT $1,'performance-'||n,'group',now()-interval '1 minute','\x01'
        FROM generate_series(1,1500) n RETURNING id,event_key
    ) INSERT INTO native_decoded_events(event_id,account_id,state,deployment_version,received_at,decoded_at,decode_ms)
        SELECT id,$1,'decoded',1,now()-interval '1 minute',
            now()-((1501-split_part(event_key,'-',2)::int)*interval '1 millisecond'),
            CASE WHEN split_part(event_key,'-',2)::int>500 THEN 2 ELSE 999 END FROM events`, shared)
	exec(`WITH event AS (
        INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
        VALUES($1,'performance-chat','group',now(),'\x01') RETURNING id
    ) INSERT INTO native_decoded_events(event_id,account_id,state,deployment_version,received_at,decoded_at,decode_ms)
        SELECT id,$1,'decoded',1,now(),now(),9999 FROM event`, ai)
	accounts, sampleErr := s.sampleMonitorAccounts(ctx, db.Platform)
	if sampleErr != nil {
		t.Fatal(sampleErr)
	}
	s.monitoring.last = systemMonitor{DatabaseAvailable: true, DatabaseSampledAt: time.Now().UTC(), Accounts: accounts}
	measured := fetch(admin, shared)
	if measured.Decode == nil || measured.Decode.Samples != 1000 || measured.Decode.P95 == nil ||
		*measured.Decode.P95 != 2 || measured.NativePerformance == nil || measured.Decode.Latency.From == nil {
		t.Fatalf("latest 1000 measurements unavailable: %+v", measured.Decode)
	}
	var overview map[string]json.RawMessage
	if err := json.Unmarshal(expect(t, admin, "GET", "/overview", nil, 200), &overview); err != nil {
		t.Fatal(err)
	}
	var latency latencyDistribution
	if err := json.Unmarshal(overview["decode_latency"], &latency); err != nil ||
		latency.Samples != 1000 || latency.P95 == nil || *latency.P95 != 2 {
		t.Fatal("overview latency crossed workspace or lost sample limit", err)
	}
	tenantMetrics := fetch(alice, shared)
	if tenantMetrics.Decode != nil || tenantMetrics.NativePerformance != nil {
		t.Fatal("tenant received raw shared-account performance")
	}
	if strings.Contains(string(expect(t, alice, "GET", "/overview", nil, 200)), `"decode_latency"`) {
		t.Fatal("tenant overview leaked shared-account samples")
	}
	var plan string
	if err := db.Platform.QueryRow(ctx, `INSERT INTO plans(name,group_limit,ai_group_limit)
        VALUES('live-dashboard-plan',10,0) RETURNING id::text`).Scan(&plan); err != nil {
		t.Fatal(err)
	}
	exec(`INSERT INTO subscriptions(tenant_id,plan_id,expires_at) VALUES($1,$2,now()+interval '1 day')`, tenantA, plan)
	livePath := "/accounts/" + shared + "/events?view=dashboard"
	expect(t, alice, "GET", "/accounts/"+shared+"/events?view=messages", nil, 403)
	expect(t, bob, "GET", livePath, nil, 404)
	expect(t, alice, "GET", "/accounts/"+separate+"/events?view=dashboard", nil, 404)
	response, err := alice.client.Get(server.URL + "/api/v1" + livePath)
	if err != nil {
		t.Fatal(err)
	}
	if response.StatusCode != 200 {
		response.Body.Close()
		t.Fatal("tenant dashboard stream denied", response.StatusCode)
	}
	scanner := bufio.NewScanner(response.Body)
	var live map[string]json.RawMessage
	for scanner.Scan() {
		if strings.HasPrefix(scanner.Text(), "data: ") {
			if err := json.Unmarshal([]byte(strings.TrimPrefix(scanner.Text(), "data: ")), &live); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	response.Body.Close()
	var tenantSnapshot accountDashboard
	if err := json.Unmarshal(live["dashboard"], &tenantSnapshot); err != nil {
		t.Fatal(err)
	}
	if tenantSnapshot.Groups.Authorized != 1 || tenantSnapshot.Inbox.Quarantined != 0 ||
		tenantSnapshot.Operations.Unknown != 0 || tenantSnapshot.Receiver != nil ||
		live["receiver"] != nil || live["decoded_ids"] != nil {
		t.Fatal("tenant stream exposed shared-account activity")
	}
	accessRequest := httptest.NewRequest("GET", "/api/v1"+livePath, nil)
	accessRequest.SetPathValue("id", shared)
	base, _ := url.Parse(server.URL)
	for _, cookie := range alice.client.Jar.Cookies(base) {
		accessRequest.AddCookie(cookie)
	}
	principal := Principal{ID: aliceID, TenantID: tenantA, Role: "user"}
	if err := s.accountEventAccess(accessRequest, principal, "dashboard"); err != nil {
		t.Fatal("active tenant stream lost access", err)
	}
	exec(`UPDATE subscriptions SET expires_at=now()-interval '1 second' WHERE tenant_id=$1`, tenantA)
	if err := s.accountEventAccess(accessRequest, principal, "dashboard"); err == nil {
		t.Fatal("expired package retained stream access")
	}
	exec(`UPDATE subscriptions SET expires_at=now()+interval '1 day' WHERE tenant_id=$1`, tenantA)
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
	if err := s.accountEventAccess(accessRequest, principal, "dashboard"); err == nil {
		t.Fatal("revoked account permission retained stream access")
	}
	exec(`UPDATE user_access SET permissions=ARRAY['accounts:read'] WHERE user_id=$1`, aliceID)
	expect(t, alice, "GET", "/accounts/"+shared+"/dashboard", nil, 403)
	exec(`UPDATE user_access SET permissions=ARRAY['overview:read','accounts:read'] WHERE user_id=$1`, aliceID)
	// Losing active group authority removes ingress metrics, even if the retained
	// ownership still permits reading the robot's safe connection record.
	exec(`UPDATE group_bindings SET active=false WHERE group_id=$1`, groupA)
	if err := s.accountEventAccess(accessRequest, principal, "dashboard"); err == nil {
		t.Fatal("inactive group retained stream access")
	}
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
