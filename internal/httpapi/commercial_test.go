package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/aigateway"
	"wangshangliao/internal/assistant"
	"wangshangliao/internal/control"
	"wangshangliao/internal/database"
	"wangshangliao/internal/security"
)

type commercialFixture struct {
	t          *testing.T
	db         *database.DB
	s          *Server
	ctx        context.Context
	admin      testClient
	adminActor control.Actor
	login      func(string) testClient
}
type commercialOwner struct {
	id, tenant, period string
	client             testClient
}

func newCommercialFixture(t *testing.T) *commercialFixture {
	t.Helper()
	f := &commercialFixture{t: t, db: integrationDB(t), ctx: context.Background()}
	hash, _ := security.Password("synthetic-commercial-password")
	var id string
	if err := f.db.Platform.QueryRow(f.ctx, `INSERT INTO users(username,password_hash,role) VALUES('commercial-admin',$1,'superadmin') RETURNING id::text`, hash).Scan(&id); err != nil {
		t.Fatal(err)
	}
	f.adminActor = control.Actor{ID: id, Role: "superadmin"}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{91}, 32)))
	f.s = newCaptchaTestServer(f.db, vault, "http://127.0.0.1:5173", false)
	server := httptest.NewServer(f.s.Handler())
	t.Cleanup(func() { server.Close(); f.s.Native.Close(); f.s.StopAssistant() })
	f.login = func(name string) testClient {
		jar, _ := cookiejar.New(nil)
		c := testClient{client: &http.Client{Jar: jar, Timeout: 5 * time.Second}, base: server.URL}
		expect(t, c, "POST", "/auth/login", map[string]string{"username": name, "password": "synthetic-commercial-password"}, 200)
		return c
	}
	f.admin = f.login("commercial-admin")
	return f
}
func (f *commercialFixture) owner(name, code string) commercialOwner {
	f.t.Helper()
	permissions := []string{}
	for _, spec := range control.BusinessPermissionCatalog() {
		permissions = append(permissions, spec.Code)
	}
	return f.ownerWithCeiling(name, code, permissions)
}
func (f *commercialFixture) ownerWithCeiling(name, code string, permissions []string) commercialOwner {
	f.t.Helper()
	row := expect(f.t, f.admin, "POST", "/users", map[string]string{"username": name, "password": "synthetic-commercial-password", "tenant_name": name}, 201)
	o := commercialOwner{id: field(f.t, row, "id"), tenant: field(f.t, row, "tenant_id")}
	var plan string
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT id::text FROM commercial_plans WHERE code=$1 AND version=1`, code).Scan(&plan); err != nil {
		f.t.Fatal(err)
	}
	price := int64(29_000_000)
	if code == "PRO" {
		price = 49_000_000
	}
	period := expect(f.t, f.admin, "POST", "/billing/confirm", control.PaymentConfirmation{TenantID: o.tenant, PlanVersionID: plan, IdempotencyKey: "start-" + name, PaymentReference: "synthetic-" + name, AmountMicroUSDT: price}, 200)
	o.period = field(f.t, period, "id")
	// Synthetic scenarios opt into their operator ceiling explicitly. A nil
	// ceiling exercises the production default: no capabilities are admitted.
	if permissions != nil {
		expect(f.t, f.admin, "PUT", "/tenants/"+o.tenant+"/capabilities",
			map[string]any{"permissions": permissions, "version": 0}, 200)
	}
	o.client = f.login(name)
	return o
}
func (f *commercialFixture) group(o commercialOwner, name string, chat bool) (string, string) {
	f.t.Helper()
	var account, group string
	if err := f.db.Platform.QueryRow(f.ctx, `INSERT INTO accounts(name) VALUES($1) RETURNING id::text`, "manager-"+name).Scan(&account); err != nil {
		f.t.Fatal(err)
	}
	if err := f.db.Platform.QueryRow(f.ctx, `INSERT INTO native_groups(native_id,name) VALUES($1,$1) RETURNING id::text`, name).Scan(&group); err != nil {
		f.t.Fatal(err)
	}
	if _, err := f.db.Platform.Exec(f.ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id) VALUES($1,$2,$3)`, group, o.tenant, account); err != nil {
		f.t.Fatal(err)
	}
	if chat {
		if err := f.db.Platform.QueryRow(f.ctx, `INSERT INTO accounts(name,platform,purpose,workspace,connection_enabled,auth_state)
			VALUES($1,'wsl','chat','maibot',true,'authenticated') RETURNING id::text`, "chat-"+name).Scan(&account); err != nil {
			f.t.Fatal(err)
		}
		if _, err := f.db.Platform.Exec(f.ctx, `INSERT INTO personas(account_id,nickname,personality) VALUES($1,'synthetic','group scoped persona');
			`, account); err != nil {
			f.t.Fatal(err)
		}
		if _, err := f.db.Platform.Exec(f.ctx, `INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
			VALUES($1,$2,'member',clock_timestamp(),'native')`, account, group); err != nil {
			f.t.Fatal(err)
		}
		if _, err := f.db.Platform.Exec(f.ctx, `UPDATE group_bindings SET ai_account_id=$2,ai_enabled=true,ai_requested=true WHERE group_id=$1`, group, account); err != nil {
			f.t.Fatal(err)
		}
	}
	return group, account
}
func TestPostgresTenantMembersScopesAndSeats(t *testing.T) {
	f := newCommercialFixture(t)
	a := f.owner("member-owner-a", "PRO")
	b := f.owner("member-owner-b", "PLUS")
	g1, _ := f.group(a, "member-g1", false)
	g2, _ := f.group(a, "member-g2", false)
	g3, _ := f.group(b, "member-g3", false)
	seedSyntheticGroupOwnership(t, f.db, f.s.Vault, g1)
	expect(t, a.client, "PUT", "/member-roles/scoped", map[string]any{"name": "Scoped", "version": 0, "permissions": []string{"overview:read", "accounts:read", "groups:read", "groups:replies", "feature:keyword_replies:rules", "operations:read", "assistant:use"}}, 200)
	member := field(t, expect(t, a.client, "POST", "/members", map[string]any{"username": "scoped-member", "password": "synthetic-commercial-password", "role": "scoped", "groups": []string{}}, 201), "id")
	m := f.login("scoped-member")
	rows := expect(t, m, "GET", "/groups", nil, 200)
	if string(rows) != "[]" {
		t.Fatal("new member inherited groups", string(rows))
	}
	expect(t, m, "GET", "/groups/"+g1+"/features", nil, 404)
	expect(t, m, "GET", "/groups/"+g3+"/features", nil, 404)
	expect(t, m, "GET", "/members", nil, 403)
	expect(t, m, "GET", "/users", nil, 403)
	expect(t, m, "GET", "/ai-gateway", nil, 403)
	change := map[string]any{"role": "scoped", "version": 1, "groups": []string{g1}, "overrides": []map[string]any{{"permission": "feature:keyword_replies:rules", "effect": "deny", "expires_at": time.Now().Add(time.Hour)}}}
	expect(t, a.client, "PUT", "/members/"+member, change, 200)
	expect(t, m, "GET", "/me", nil, 401)
	m = f.login("scoped-member")
	rows = expect(t, m, "GET", "/groups?tenant_id="+b.tenant, nil, 200)
	if !bytes.Contains(rows, []byte(g1)) || bytes.Contains(rows, []byte(g2)) || bytes.Contains(rows, []byte(g3)) {
		t.Fatal("group RLS", string(rows))
	}
	expect(t, m, "GET", "/groups/"+g1+"/features", nil, 200)
	expect(t, m, "GET", "/groups/"+g2+"/features", nil, 404)
	expect(t, m, "PUT", "/groups/"+g1+"/features", map[string]any{"version": 1, "changes": map[string]any{"keyword_replies": map[string]any{"rules": []any{}}}}, 403)
	if _, err := f.db.Platform.Exec(f.ctx, `UPDATE member_overrides SET expires_at=clock_timestamp()-interval '1 second' WHERE user_id=$1`, member); err != nil {
		t.Fatal(err)
	}
	if raw := expect(t, m, "GET", "/me", nil, 200); !bytes.Contains(raw, []byte("feature:keyword_replies:rules")) {
		t.Fatal("expired deny remained effective")
	}
	assistantRequest(t, m, "POST", "/sessions", assistant.Scope{Kind: "group", AccountID: control.NewID(), GroupID: g2}, "", 403)
	var visible int
	err := f.db.TenantTx(database.WithActor(f.ctx, member), a.tenant, func(tx pgx.Tx) error { return tx.QueryRow(f.ctx, `SELECT count(*) FROM group_bindings`).Scan(&visible) })
	if err != nil || visible != 1 {
		t.Fatal("restricted user context", visible, err)
	}
	var bill map[string]json.RawMessage
	json.Unmarshal(expect(t, m, "GET", "/billing", nil, 200), &bill)
	if string(bill["full_bill"]) != "false" || string(bill["wallet"]) != "null" {
		t.Fatal("member received tenant wallet")
	}
	expect(t, a.client, "PUT", "/members/"+a.id, map[string]any{"role": "viewer", "version": 1}, 409)
	change["groups"] = []string{g3}
	change["version"] = 2
	expect(t, a.client, "PUT", "/members/"+member, change, 404)
	change["groups"] = []string{}
	change["disabled"] = true
	expect(t, a.client, "PUT", "/members/"+member, change, 200)
	expect(t, m, "GET", "/groups", nil, 401)
	// PLUS has exactly two seats besides the owner, even with concurrent creation.
	var wg sync.WaitGroup
	statuses := make(chan int, 3)
	for _, name := range []string{"seat-one", "seat-two", "seat-three"} {
		wg.Add(1)
		go func(n string) {
			defer wg.Done()
			status, _, _ := b.client.request("POST", "/members", map[string]any{"username": n, "password": "synthetic-commercial-password", "role": "viewer", "groups": []string{}})
			statuses <- status
		}(name)
	}
	wg.Wait()
	close(statuses)
	created, denied := 0, 0
	for status := range statuses {
		if status == 201 {
			created++
		} else if status == 409 {
			denied++
		} else {
			t.Fatal("unexpected seat result", status)
		}
	}
	if created != 2 || denied != 1 {
		t.Fatal("seat race", created, denied)
	}
	var seat, otherSeat string
	f.db.Platform.QueryRow(f.ctx, `SELECT id::text FROM users WHERE tenant_id=$1 AND id<>$2 ORDER BY username LIMIT 1`, b.tenant, b.id).Scan(&seat)
	expect(t, b.client, "PUT", "/members/"+seat, map[string]any{"role": "viewer", "version": 1, "groups": []string{}, "disabled": true}, 200)
	otherSeat = field(t, expect(t, b.client, "POST", "/members", map[string]any{"username": "replacement-seat", "password": "synthetic-commercial-password", "role": "viewer"}, 201), "id")
	expect(t, b.client, "PUT", "/members/"+seat, map[string]any{"role": "viewer", "version": 2, "groups": []string{}, "disabled": false}, 409)
	expect(t, b.client, "PUT", "/members/"+otherSeat, map[string]any{"role": "viewer", "version": 1, "disabled": true}, 200)
	expect(t, b.client, "PUT", "/members/"+seat, map[string]any{"role": "viewer", "version": 2, "disabled": false}, 200)
	expect(t, b.client, "POST", "/tenants/"+b.tenant+"/owner", map[string]any{"user_id": seat, "previous_owner_id": b.id}, 403)
	expect(t, f.admin, "POST", "/tenants/"+b.tenant+"/owner", map[string]any{"user_id": seat, "previous_owner_id": ""}, 409)
	expect(t, f.admin, "POST", "/tenants/"+b.tenant+"/owner", map[string]any{"user_id": seat, "previous_owner_id": b.id}, 200)
	expect(t, b.client, "GET", "/me", nil, 401)
}
func TestPostgresCommercialPeriodsVersionsAndCorrections(t *testing.T) {
	f := newCommercialFixture(t)
	a := f.owner("billing-owner", "PRO")
	var pro, plus string
	f.db.Platform.QueryRow(f.ctx, `SELECT id::text FROM commercial_plans WHERE code='PRO'`).Scan(&pro)
	f.db.Platform.QueryRow(f.ctx, `SELECT id::text FROM commercial_plans WHERE code='PLUS'`).Scan(&plus)
	in := control.PaymentConfirmation{TenantID: a.tenant, PlanVersionID: pro, IdempotencyKey: "billing-renewal-1", PaymentReference: "synthetic-renewal", AmountMicroUSDT: 49_000_000}
	first := expect(t, f.admin, "POST", "/billing/confirm", in, 200)
	again := expect(t, f.admin, "POST", "/billing/confirm", in, 200)
	if field(t, first, "id") != field(t, again, "id") {
		t.Fatal("duplicate renewal")
	}
	in.AmountMicroUSDT = 29_000_000
	expect(t, f.admin, "POST", "/billing/confirm", in, 409)
	in.IdempotencyKey = "billing-wrong-amount"
	in.PaymentReference = "wrong-payment"
	expect(t, f.admin, "POST", "/billing/confirm", in, 400)
	var wallets int
	var duration float64
	f.db.Platform.QueryRow(f.ctx, `SELECT count(*) FROM ai_wallets WHERE tenant_id=$1`, a.tenant).Scan(&wallets)
	f.db.Platform.QueryRow(f.ctx, `SELECT extract(epoch FROM ends_at-starts_at) FROM subscription_periods WHERE id=$1`, a.period).Scan(&duration)
	if wallets != 1 || duration != 2592000 {
		t.Fatal("early issuance or wrong duration", wallets, duration)
	}
	if _, err := f.db.Platform.Exec(f.ctx, `UPDATE commercial_plans SET credits_micro=1 WHERE id=$1`, pro); err == nil {
		t.Fatal("mutable plan")
	}
	newVersion := field(t, expect(t, f.admin, "POST", "/billing/plans", map[string]any{"code": "PRO", "base_version": 1}, 201), "id")
	if newVersion == pro {
		t.Fatal("plan overwritten")
	}
	expect(t, f.admin, "POST", "/billing/plans", map[string]any{"code": "PRO", "base_version": 1}, 409)
	expect(t, a.client, "POST", "/billing/plans", map[string]any{"code": "PRO", "base_version": 2}, 403)
	// Move only synthetic periods to exercise the exact boundary without waiting 30 days.
	if _, err := f.db.Platform.Exec(f.ctx, `UPDATE subscription_periods SET starts_at=starts_at-interval '30 days 1 second',
		ends_at=ends_at-interval '30 days 1 second' WHERE tenant_id=$1`, a.tenant); err != nil {
		t.Fatal(err)
	}
	if err := (&control.BillingService{DB: f.db}).Refresh(f.ctx, a.tenant); err != nil {
		t.Fatal(err)
	}
	var grants int
	var balance int64
	f.db.Platform.QueryRow(f.ctx, `SELECT count(*) FROM credit_entries WHERE tenant_id=$1 AND kind='grant'`, a.tenant).Scan(&grants)
	f.db.Platform.QueryRow(f.ctx, `SELECT w.granted_micro-w.spent_micro-w.held_micro FROM ai_wallets w JOIN subscription_periods p ON p.id=w.period_id WHERE p.tenant_id=$1 AND p.state='active'`, a.tenant).Scan(&balance)
	if grants != 2 || balance != 10_000_000_000 {
		t.Fatal("rollover or duplicate grant", grants, balance)
	}
	f.group(a, "downgrade-1", false)
	f.group(a, "downgrade-2", false)
	in.PlanVersionID = plus
	in.AmountMicroUSDT = 29_000_000
	in.IdempotencyKey = "downgrade-order"
	in.PaymentReference = "downgrade-ref"
	expect(t, f.admin, "POST", "/billing/confirm", in, 409)
}
func TestPostgresGatewayHeatBudgetUnknownAndReconciliation(t *testing.T) {
	f := newCommercialFixture(t)
	a := f.owner("gateway-owner", "PRO")
	group, chat := f.group(a, "gateway-group", true)
	if err := f.s.Gateway.Configure(f.ctx, f.adminActor, control.GatewaySettings{Enabled: true, PlatformLimit: 100_000_000_000, TestLimit: 1_000_000_000},
		control.ModelRoute{Alias: "default", Enabled: true, Verified: true, Rates: aigateway.Rates{Input: 1_000_000, Output: 4_000_000, CacheRead: 100_000, CacheWrite: 2_000_000},
			Model: assistant.ModelConfig{BaseURL: "https://fixture.example/v1", Model: "synthetic", APIKey: "synthetic-business-only"}}); err != nil {
		t.Fatal(err)
	}
	var requests atomic.Int32
	f.s.Gateway.ProviderFactory = func(assistant.ModelConfig) (assistant.Provider, error) {
		return fixtureModel(func(ctx context.Context, _ []assistant.Message, _ []assistant.Tool, _ func(string) error) (assistant.Response, error) {
			requests.Add(1)
			if requests.Load() == 2 {
				return assistant.Response{}, context.DeadlineExceeded
			}
			u := assistant.Usage{PromptTokens: 2000, CompletionTokens: 500, TotalTokens: 2500}
			u.PromptDetails.CachedTokens = 1000
			return assistant.Response{Message: assistant.Message{Role: "assistant", Content: "synthetic text"}, Usage: u}, nil
		}), nil
	}
	run := func(token string) (assistant.Response, error) {
		return f.s.Gateway.HeatGenerate(f.ctx, token, control.HeatGatewayInput{ModelAlias: "default", ModelVersion: 1, Messages: []assistant.Message{{Role: "user", Content: "hello"}}})
	}
	mint := func() string {
		t.Helper()
		token, err := f.s.Gateway.MintHeatGrant(f.ctx, f.adminActor, group)
		if err != nil {
			t.Fatal(err)
		}
		return token
	}
	token := mint()
	out, err := run(token)
	if err != nil || out.Message.Content != "synthetic text" {
		t.Fatal("heat gateway", err)
	}
	if _, err = run(token); err == nil || requests.Load() != 1 {
		t.Fatal("single-use grant replay")
	}
	var spent, held int64
	f.db.Platform.QueryRow(f.ctx, `SELECT spent_micro,held_micro FROM ai_wallets WHERE period_id=$1`, a.period).Scan(&spent, &held)
	if spent != 3_100_000 || held != 0 {
		t.Fatal("cache counted twice / weighted charge", spent, held)
	}
	if _, err = run(mint()); err == nil {
		t.Fatal("synthetic timeout not reported")
	}
	var id string
	f.db.Platform.QueryRow(f.ctx, `SELECT id::text FROM ai_calls WHERE status='unknown'`).Scan(&id)
	f.db.Platform.QueryRow(f.ctx, `SELECT held_micro FROM ai_wallets WHERE period_id=$1`, a.period).Scan(&held)
	if id == "" || held == 0 {
		t.Fatal("unknown usage wasn't frozen")
	}
	if err = f.s.Gateway.Recover(f.ctx); err != nil {
		t.Fatal(err)
	}
	if requests.Load() != 2 {
		t.Fatal("restarted paid request")
	}
	usage := assistant.Usage{PromptTokens: 100, CompletionTokens: 10, TotalTokens: 110}
	for i := 0; i < 2; i++ {
		if err = f.s.Gateway.Reconcile(f.ctx, f.adminActor, id, usage, "fixture provider statement"); err != nil {
			t.Fatal(err)
		}
	}
	f.db.Platform.QueryRow(f.ctx, `SELECT spent_micro,held_micro FROM ai_wallets WHERE period_id=$1`, a.period).Scan(&spent, &held)
	if spent != 3_240_000 || held != 0 {
		t.Fatal("non-idempotent reconciliation", spent, held)
	}
	correction := control.CreditCorrection{Amount: 40_000, Key: "fixture-correction", Reason: "verified overcharge"}
	for i := 0; i < 2; i++ {
		if err = f.s.Gateway.CorrectCharge(f.ctx, f.adminActor, id, correction); err != nil {
			t.Fatal(err)
		}
	}
	f.db.Platform.QueryRow(f.ctx, `SELECT spent_micro FROM ai_wallets WHERE period_id=$1`, a.period).Scan(&spent)
	if spent != 3_200_000 {
		t.Fatal("double correction", spent)
	}
	correction.Key = "different-correction"
	correction.Amount = 140_000
	if err = f.s.Gateway.CorrectCharge(f.ctx, f.adminActor, id, correction); err == nil {
		t.Fatal("refund beyond charge")
	}
	old := mint()
	if _, err = f.db.Platform.Exec(f.ctx, `UPDATE personas SET version=version+1 WHERE account_id=$1`, chat); err != nil {
		t.Fatal(err)
	}
	if _, err = run(old); err == nil || requests.Load() != 2 {
		t.Fatal("stale persona admitted")
	}
	expect(t, a.client, "PUT", "/billing/limits", map[string]any{"kind": "purpose", "scope": "heatbot", "limit_micro": 0, "version": 0}, 200)
	if _, err = run(mint()); err == nil || requests.Load() != 2 {
		t.Fatal("sub-limit ignored")
	}
	raw := expect(t, f.admin, "GET", "/ai-gateway", nil, 200)
	if bytes.Contains(raw, []byte("synthetic-business-only")) {
		t.Fatal("secret returned")
	}
	expect(t, a.client, "POST", "/ai-gateway/calls/"+id+"/reconcile", map[string]any{"usage": usage, "reason": "forged"}, 403)
}

func TestPostgresGatewayAtomicBudgetAndSubmissionCancellation(t *testing.T) {
	f := newCommercialFixture(t)
	a := f.owner("atomic-owner", "PRO")
	group, _ := f.group(a, "atomic-group", true)
	second, _ := f.group(a, "atomic-group-2", true)
	if err := f.s.Gateway.Configure(f.ctx, f.adminActor, control.GatewaySettings{Enabled: true, PlatformLimit: 100_000_000_000, TestLimit: 1_000_000_000},
		control.ModelRoute{Alias: "default", Enabled: true, Verified: true, Rates: aigateway.Rates{Input: 1_000_000, Output: 4_000_000},
			Model: assistant.ModelConfig{BaseURL: "https://fixture.example/v1", Model: "synthetic", APIKey: "atomic-fixture-key"}}); err != nil {
		t.Fatal(err)
	}
	// Exactly one conservative reservation fits; calls share the same period wallet.
	if _, err := f.db.Platform.Exec(f.ctx, `UPDATE ai_wallets SET spent_micro=granted_micro-9000000 WHERE period_id=$1`, a.period); err != nil {
		t.Fatal(err)
	}
	started, complete := make(chan struct{}, 2), make(chan struct{})
	var count atomic.Int32
	f.s.Gateway.ProviderFactory = func(assistant.ModelConfig) (assistant.Provider, error) {
		return fixtureModel(func(ctx context.Context, _ []assistant.Message, _ []assistant.Tool, _ func(string) error) (assistant.Response, error) {
			count.Add(1)
			started <- struct{}{}
			select {
			case <-complete:
			case <-ctx.Done():
				return assistant.Response{}, ctx.Err()
			}
			return assistant.Response{Message: assistant.Message{Role: "assistant", Content: "ok"}, Usage: assistant.Usage{PromptTokens: 10, CompletionTokens: 1, TotalTokens: 11}}, nil
		}), nil
	}
	tokens := make([]string, 2)
	for i, target := range []string{group, second} {
		var err error
		tokens[i], err = f.s.Gateway.MintHeatGrant(f.ctx, f.adminActor, target)
		if err != nil {
			t.Fatal(err)
		}
	}
	results := make(chan error, 2)
	for _, token := range tokens {
		go func() {
			_, err := f.s.Gateway.HeatGenerate(f.ctx, token, control.HeatGatewayInput{ModelAlias: "default", ModelVersion: 1, Messages: []assistant.Message{{Role: "user", Content: "fixture"}}})
			results <- err
		}()
	}
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("model not started")
	}
	select {
	case err := <-results:
		if err == nil {
			t.Fatal("concurrent budget overflow")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("budget contention blocked")
	}
	close(complete)
	if err := <-results; err != nil {
		t.Fatal(err)
	}
	if count.Load() != 1 {
		t.Fatal("double reservation", count.Load())
	}
	// A cancelled request cannot reserve or submit a paid request.
	token, err := f.s.Gateway.MintHeatGrant(f.ctx, f.adminActor, group)
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(f.ctx)
	cancel()
	if _, err = f.s.Gateway.HeatGenerate(cancelled, token, control.HeatGatewayInput{ModelAlias: "default", ModelVersion: 1, Messages: []assistant.Message{{Role: "user", Content: "cancel"}}}); err == nil {
		t.Fatal("cancel ignored")
	}
	if count.Load() != 1 {
		t.Fatal("cancelled paid call")
	}
	// Revocation during provider construction must release, never submit.
	f.s.Gateway.ProviderFactory = func(assistant.ModelConfig) (assistant.Provider, error) {
		_, e := f.db.Platform.Exec(f.ctx, `UPDATE users SET disabled=true WHERE id=$1`, a.id)
		if e != nil {
			return nil, e
		}
		return fixtureModel(func(context.Context, []assistant.Message, []assistant.Tool, func(string) error) (assistant.Response, error) {
			count.Add(1)
			return assistant.Response{}, nil
		}), nil
	}
	if _, err = f.s.Gateway.HeatGenerate(f.ctx, token, control.HeatGatewayInput{ModelAlias: "default", ModelVersion: 1,
		Messages: []assistant.Message{{Role: "user", Content: "revoke-before-submit"}}}); err == nil {
		t.Fatal("revoked submission accepted")
	}
	if count.Load() != 1 {
		t.Fatal("revoked paid call")
	}
}

func TestPostgresHeatGrantCleanupAndAccountRemoval(t *testing.T) {
	f := newCommercialFixture(t)
	a := f.owner("cleanup-owner", "PRO")
	group, account := f.group(a, "cleanup-group", true)
	if err := f.s.Gateway.Configure(f.ctx, f.adminActor, control.GatewaySettings{Enabled: true,
		PlatformLimit: 100_000_000_000, TestLimit: 1_000_000_000}, control.ModelRoute{Alias: "default",
		Enabled: true, Verified: true, Rates: aigateway.Rates{Input: 1_000_000, Output: 4_000_000},
		Model: assistant.ModelConfig{BaseURL: "https://fixture.example/v1", Model: "synthetic", APIKey: "cleanup-fixture"}}); err != nil {
		t.Fatal(err)
	}
	expired, err := f.s.Gateway.MintHeatGrant(f.ctx, f.adminActor, group)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Platform.Exec(f.ctx, `UPDATE heat_gateway_grants SET expires_at=clock_timestamp()-interval '1 second'
		WHERE token_hash=$1`, security.Digest(expired)); err != nil {
		t.Fatal(err)
	}
	if _, err = f.s.Gateway.MintHeatGrant(f.ctx, f.adminActor, group); err != nil {
		t.Fatal(err)
	}
	if err = f.s.Gateway.PruneGrants(f.ctx); err != nil {
		t.Fatal(err)
	}
	var count int
	if err = f.db.Platform.QueryRow(f.ctx, `SELECT count(*) FROM heat_gateway_grants`).Scan(&count); err != nil || count != 1 {
		t.Fatal("expired grants not pruned / live grant lost", count, err)
	}
	admin := f.admin
	admin.workspace = "maibot"
	expect(t, admin, "DELETE", "/accounts/"+account, map[string]any{"version": 1, "confirm_name": "chat-cleanup-group"}, 200)
	if err = f.db.Platform.QueryRow(f.ctx, `SELECT count(*) FROM heat_gateway_grants`).Scan(&count); err != nil || count != 0 {
		t.Fatal("removed account retained credential", count, err)
	}
	if err = f.db.Platform.QueryRow(f.ctx, `SELECT count(*) FROM ai_wallets WHERE tenant_id=$1`, a.tenant).Scan(&count); err != nil || count != 1 {
		t.Fatal("account cleanup removed billing history", count, err)
	}
}
