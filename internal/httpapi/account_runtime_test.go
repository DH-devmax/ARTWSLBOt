package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/security"
)

func TestMonitorReasonsAreIndependent(t *testing.T) {
	now := time.Now()
	old := now.Add(-31 * time.Second)
	cool := now.Add(time.Minute)
	account := accountMonitor{Receiver: receiverView{Enabled: true, Observed: true, Snapshot: &gateway.ReceiverSnapshot{State: "connected", SyncedAt: &now, HeartbeatAt: &now}},
		HTTP: gateway.ReadSnapshot{OldestWaiting: &old, CooldownUntil: &cool}, OperationsUnknown: 1}
	reasons := monitorReasons(account, now)
	for _, reason := range []string{"backlog_oldest", "http_rate_limited", "operation_unknown"} {
		if !slices.Contains(reasons, reason) {
			t.Fatal("missing independent reason", reason)
		}
	}
	if slices.Contains(reasons, "receiver_disconnected") || slices.Contains(reasons, "heartbeat_stale") {
		t.Fatal("quiet account treated as offline")
	}
	account.HTTP = gateway.ReadSnapshot{}
	account.OperationsUnknown = 0
	if len(monitorReasons(account, now)) != 0 {
		t.Fatal("healthy quiet connection invented a fault")
	}
}

func assignmentRequest(t *testing.T, c testClient, path, key string, body any) (int, json.RawMessage, string) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", c.base+"/api/v1"+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "http://127.0.0.1:5173")
	req.Header.Set("Idempotency-Key", key)
	if c.workspace != "" {
		req.Header.Set("X-Workspace", c.workspace)
	}
	u, _ := url.Parse(c.base)
	for _, cookie := range c.client.Jar.Cookies(u) {
		if cookie.Name == "wsl_csrf" {
			req.Header.Set("X-CSRF-Token", cookie.Value)
		}
	}
	resp, err := c.client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out struct {
		Data json.RawMessage
		Msg  string
	}
	if err = json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return resp.StatusCode, out.Data, out.Msg
}

func TestPostgresAccountRuntimeCapacityAssignmentAndMonitor(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	hash, _ := security.Password("runtime-isolated-password")
	_, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role) VALUES('runtime-admin',$1,'superadmin')`, hash)
	if err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{28}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	t.Cleanup(s.Native.Close)
	server := httptest.NewServer(s.Handler())
	t.Cleanup(server.Close)
	jar, _ := cookiejar.New(nil)
	admin := testClient{client: &http.Client{Jar: jar, Timeout: 10 * time.Second}, base: server.URL}
	expect(t, admin, "POST", "/auth/login", map[string]string{"username": "runtime-admin", "password": "runtime-isolated-password"}, 200)
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	a := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "runtime-a"}, 201), "id")
	b := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "runtime-b"}, 201), "id")
	user := expect(t, admin, "POST", "/users", map[string]string{"username": "runtime-user", "password": "runtime-isolated-password", "tenant_name": "runtime-tenant"}, 201)
	tenant := field(t, user, "tenant_id")
	plan := field(t, expect(t, admin, "POST", "/plans", map[string]any{"name": "runtime-plan", "group_limit": 100, "ai_group_limit": 100, "modules": []string{"moderation", "ai"}}, 201), "id")
	expect(t, admin, "PUT", "/subscriptions/"+tenant, map[string]any{"plan_id": plan, "enabled": true, "version": 0, "expires_at": time.Now().Add(time.Hour)}, 200)
	newGroup := func(n int) string {
		t.Helper()
		var group string
		if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(native_id,name) VALUES($1,$1) RETURNING id::text`, fmt.Sprintf("runtime-%d", n)).Scan(&group); err != nil {
			t.Fatal(err)
		}
		return group
	}
	groups := []string{}
	for i := 0; i < 29; i++ {
		g := newGroup(i)
		groups = append(groups, g)
		exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,management_enabled) VALUES($1,$2,$3,false)`, g, tenant, a)
	}
	// Two concurrent platform writers cannot consume the 30th slot twice.
	extra := []string{newGroup(29), newGroup(30)}
	var wg sync.WaitGroup
	errs := make(chan error, 2)
	for _, g := range extra {
		wg.Add(1)
		go func(g string) {
			defer wg.Done()
			_, e := db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id) VALUES($1,$2,$3)`, g, tenant, a)
			errs <- e
		}(g)
	}
	wg.Wait()
	close(errs)
	passed := 0
	for e := range errs {
		if e == nil {
			passed++
		}
	}
	if passed != 1 {
		t.Fatal("concurrent capacity admission", passed)
	}
	capacity, err := control.ReadAccountCapacity(ctx, db.Platform, a)
	if err != nil || capacity.Bound != 30 {
		t.Fatal(capacity, err)
	}
	exec(`UPDATE group_bindings SET active=false WHERE group_id=$1`, groups[0])
	if capacity, err = control.ReadAccountCapacity(ctx, db.Platform, a); err != nil || capacity.Bound != 29 {
		t.Fatal("revocation did not release capacity", capacity, err)
	}
	// Synthetic trusted role and receiver fixtures only; no native transport starts.
	g := groups[1]
	exec(`UPDATE accounts SET auth_state='authenticated',credential_sealed='\x0102' WHERE id=ANY($1::text[]::uuid[])`, []string{a, b})
	for _, id := range []string{a, b} {
		exec(`INSERT INTO account_groups(account_id,group_id,role,observation_source,observed_at) VALUES($1,$2,'admin','native',now())`, id, g)
		exec(`INSERT INTO account_receivers(account_id,enabled,lease_until,observed_at,snapshot)
            VALUES($1,true,now()+interval '1 minute',now(),jsonb_build_object('state','connected','synced_at',now(),'heartbeat_at',now()))`, id)
	}
	path := "/groups/" + g + "/assignment"
	var preview control.AssignmentPreview
	if err = json.Unmarshal(expect(t, admin, "GET", path, nil, 200), &preview); err != nil {
		t.Fatal(err)
	}
	if preview.Recommended != b {
		t.Fatal("recommendation did not prefer low binding count", preview)
	}
	var epoch int64
	for _, c := range preview.Candidates {
		if c.ID == b {
			epoch = c.Epoch
		}
	}
	input := map[string]any{"target_account_id": b, "source_account_id": preview.Source, "source_epoch": preview.SourceEpoch,
		"target_epoch": epoch, "version": preview.Version, "binding_epoch": preview.Epoch}
	key := "runtime-assignment-1"
	exec(`INSERT INTO operations(group_id,account_id,idempotency_key,request_hash,kind,account_epoch,status,expires_at)
        VALUES($1,$2,'runtime-unknown','\x01','send_text',1,'unknown',now()+interval '1 minute')`, g, a)
	if code, _, msg := assignmentRequest(t, admin, path, key, input); code != 409 || msg != "assignment_operations_pending" {
		t.Fatal("unknown result did not fence migration", code, msg)
	}
	exec(`UPDATE operations SET status='failed' WHERE group_id=$1`, g)
	exec(`INSERT INTO operations(group_id,account_id,idempotency_key,request_hash,kind,account_epoch,status,expires_at)
        VALUES($1,$2,'runtime-queued','\x01','send_text',1,'queued',now()+interval '1 minute')`, g, a)
	if code, _, msg := assignmentRequest(t, admin, path, key, input); code != 200 {
		t.Fatal("assignment", code, msg)
	}
	if code, _, msg := assignmentRequest(t, admin, path, key, input); code != 200 {
		t.Fatal("idempotent replay", code, msg)
	}
	if code, _, msg := assignmentRequest(t, admin, path, key+"new", input); code != 409 || msg != "version_conflict" {
		t.Fatal("stale preview", code, msg)
	}
	input["target_account_id"] = a
	if code, _, msg := assignmentRequest(t, admin, path, key, input); code != 409 || msg != "idempotency_key_conflict" {
		t.Fatal("idempotency substitution", code, msg)
	}
	var version int
	if err = db.Platform.QueryRow(ctx, `SELECT version FROM group_bindings WHERE group_id=$1`, g).Scan(&version); err != nil || version != 2 {
		t.Fatal("duplicate assignment wrote twice", version, err)
	}
	var queued int
	if err = db.Platform.QueryRow(ctx, `SELECT count(*) FROM operations WHERE group_id=$1 AND status='queued'`, g).Scan(&queued); err != nil || queued != 0 {
		t.Fatal("old queue survived migration", queued, err)
	}
	ledger := gateway.PostgresLedger{DB: db.Platform, Vault: vault}
	for _, id := range []string{a, b} {
		if err = ledger.Commit(ctx, []gateway.Message{{AccountID: id, GroupID: g, NativeID: "shared-business-message", SenderID: "synthetic-member", ReceivedAt: time.Now(), Resolved: true}}); err != nil {
			t.Fatal(err)
		}
	}
	var active, shadow int
	if err = db.Platform.QueryRow(ctx, `SELECT count(*) FILTER(WHERE state='pending'),count(*) FILTER(WHERE state='shadow')
        FROM inbox WHERE group_id=$1`, g).Scan(&active, &shadow); err != nil || active != 1 || shadow != 1 {
		t.Fatal("multiple collectors admitted business work", active, shadow, err)
	}
	exec(`INSERT INTO account_read_state(account_id,account_epoch,cooldown_until)
        SELECT id,epoch,now()+interval '2 minutes' FROM accounts WHERE id=$1`, a)
	var dashboard accountDashboard
	if err = json.Unmarshal(expect(t, admin, "GET", "/accounts/"+a+"/dashboard", nil, 200), &dashboard); err != nil ||
		dashboard.HTTP == nil || dashboard.HTTP.CooldownUntil == nil || !dashboard.HTTP.CooldownUntil.After(time.Now()) {
		t.Fatal("durable cooldown disappeared without a scheduler", dashboard.HTTP, err)
	}
	// Safe source-specific projections and bounded retention.
	fresh, err := s.sampleMonitorAccounts(ctx, db.Platform)
	if err != nil || len(fresh) != 2 {
		t.Fatal("monitor account query", err)
	}
	for _, account := range fresh {
		if account.ID == a && account.HTTP.CooldownUntil == nil {
			t.Fatal("monitor lost durable cooldown")
		}
	}
	now := time.Now().UTC()
	current := systemMonitor{Accounts: fresh, DatabaseAvailable: true, UpdatedAt: now}
	if err = persistMonitorMinute(ctx, db.Platform, current, now.Truncate(time.Minute)); err != nil {
		t.Fatal("persist minute", err)
	}
	changes := monitorChanges(map[string]string{}, map[string]string{"|database": "database_unavailable"}, now.Add(-time.Minute))
	for range 2 {
		if err = flushMonitorTransitions(ctx, db.Platform, changes, fresh); err != nil {
			t.Fatal(err)
		}
	}
	stored, err := loadMonitorStates(ctx, db.Platform)
	if err != nil || stored["|database"] != "database_unavailable" {
		t.Fatal("open incident was not restored", stored, err)
	}
	if err = flushMonitorTransitions(ctx, db.Platform, monitorChanges(stored, map[string]string{}, now), fresh); err != nil {
		t.Fatal(err)
	}
	var transitions int
	if err = db.Platform.QueryRow(ctx, `SELECT count(*) FROM monitor_incidents`).Scan(&transitions); err != nil || transitions != 2 {
		t.Fatal("transition retry duplicated events", transitions, err)
	}
	exec(`INSERT INTO monitor_minutes(scope,scope_id,minute,workspace,sample) VALUES('system','platform',now()-interval '25 hours','platform','{}')`)
	exec(`INSERT INTO monitor_incidents(workspace,component,reason,state,created_at) VALUES('platform','database','database_unavailable','opened',now()-interval '8 days')`)
	if err = persistMonitorMinute(ctx, db.Platform, current, now.Truncate(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var expired int
	if err = db.Platform.QueryRow(ctx, `SELECT (SELECT count(*) FROM monitor_minutes WHERE minute<now()-interval '24 hours')+(SELECT count(*) FROM monitor_incidents WHERE created_at<now()-interval '7 days')`).Scan(&expired); err != nil || expired != 0 {
		t.Fatal("retention failed", expired, err)
	}
	s.monitoring.mu.Lock()
	s.monitoring.last = current
	s.monitoring.mu.Unlock()
	if err = json.Unmarshal(expect(t, admin, "GET", "/accounts/"+a+"/dashboard", nil, 200), &dashboard); err != nil ||
		dashboard.Decode == nil || dashboard.Decode.SampledAt.IsZero() {
		t.Fatal("account dashboard did not reuse shared decoder sampling", dashboard.Decode, err)
	}
	expect(t, admin, "GET", "/system/monitor", nil, 200)
	expect(t, admin, "GET", "/system/monitor/history", nil, 200)
	expect(t, admin, "GET", "/system/monitor/history?start=2026-01-01T00:00:00Z&end=2026-01-03T00:00:00Z", nil, 400)
	expect(t, admin, "GET", "/system/monitor/incidents", nil, 200)
	chat := admin
	chat.workspace = "maibot"
	body := expect(t, chat, "GET", "/system/monitor", nil, 200)
	if bytes.Contains(body, []byte(a)) || bytes.Contains(body, []byte(b)) {
		t.Fatal("monitor workspace leak")
	}
	expect(t, chat, "GET", "/system/monitor/history?account_id="+a, nil, 404)
	expect(t, chat, "GET", path, nil, 200) // May preview this authorized group's independent AI binding only.
	tenantJar, _ := cookiejar.New(nil)
	userClient := testClient{client: &http.Client{Jar: tenantJar}, base: server.URL}
	expect(t, userClient, "POST", "/auth/login", map[string]string{"username": "runtime-user", "password": "runtime-isolated-password"}, 200)
	for _, endpoint := range []string{"/system/monitor", "/system/monitor/history", "/system/monitor/incidents", "/system/monitor/events", path} {
		expect(t, userClient, "GET", endpoint, nil, 403)
	}
	if _, err = db.Tenant.Exec(ctx, `SELECT * FROM monitor_minutes`); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Fatal("restricted tenant DB role could read platform monitoring", err)
	}
	monitorCtx, stopMonitoring := context.WithCancel(ctx)
	done := s.RunMonitoring(monitorCtx)
	until := time.Now().Add(3 * time.Second)
	for s.monitorSnapshot("wsl").UpdatedAt.Equal(now) && time.Now().Before(until) {
		time.Sleep(time.Millisecond)
	}
	stopMonitoring()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("monitor collector did not cancel")
	}
}
