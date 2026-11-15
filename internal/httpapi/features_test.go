package httpapi

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"testing"
	"time"

	"wangshangliao/internal/features"
	"wangshangliao/internal/security"
)

func TestPostgresFeatureFieldsAndTenantIsolation(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	password := "synthetic-feature-password"
	hash, _ := security.Password(password)
	if _, err := db.Platform.Exec(ctx, `INSERT INTO users(username,password_hash,role)
        VALUES('feature-admin',$1,'superadmin')`, hash); err != nil {
		t.Fatal(err)
	}
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{43}, 32)))
	s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
	defer s.Native.Close()
	server := httptest.NewServer(s.Handler())
	defer server.Close()
	login := func(name string) testClient {
		jar, _ := cookiejar.New(nil)
		client := testClient{client: &http.Client{Jar: jar}, base: server.URL}
		expect(t, client, "POST", "/auth/login", map[string]string{"username": name, "password": password}, 200)
		return client
	}
	admin := login("feature-admin")
	account := field(t, expect(t, admin, "POST", "/accounts", map[string]string{"name": "feature-robot"}, 201), "id")
	type fixture struct{ user, tenant, group string }
	create := func(name string, modules []string) fixture {
		result := expect(t, admin, "POST", "/users", map[string]string{"username": name, "password": password, "tenant_name": name}, 201)
		f := fixture{user: field(t, result, "id")}
		if err := db.Platform.QueryRow(ctx, `SELECT tenant_id::text FROM users WHERE id=$1`, f.user).Scan(&f.tenant); err != nil {
			t.Fatal(err)
		}
		plan := field(t, expect(t, admin, "POST", "/plans", map[string]any{"name": name, "group_limit": 2, "ai_group_limit": 0, "modules": modules}, 201), "id")
		expect(t, admin, "PUT", "/subscriptions/"+f.tenant, map[string]any{"plan_id": plan,
			"expires_at": time.Now().Add(time.Hour), "enabled": true, "version": 0}, 200)
		explicitTestCeiling(t, admin, f.tenant)
		if err := db.Platform.QueryRow(ctx, `INSERT INTO native_groups(native_id,name) VALUES($1,$1) RETURNING id::text`, name).Scan(&f.group); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id) VALUES($1,$2,$3)`,
			f.group, f.tenant, account); err != nil {
			t.Fatal(err)
		}
		seedSyntheticGroupOwnership(t, db, vault, f.group)
		return f
	}
	a := create("feature-tenant-a", []string{"moderation", "replies"})
	b := create("feature-tenant-b", []string{"moderation"})
	user := login("feature-tenant-a")
	var catalog struct {
		Items        []features.Definition                    `json:"items"`
		Capabilities []features.NativeCapability              `json:"native_capabilities"`
		Details      []features.NativeDetail                  `json:"native_details"`
		Acceptance   map[string]features.ControlledAcceptance `json:"native_acceptance"`
		NativeReady  bool                                     `json:"native_ready"`
	}
	if err := json.Unmarshal(expect(t, user, "GET", "/features/catalog", nil, 200), &catalog); err != nil {
		t.Fatal(err)
	}
	if len(catalog.Capabilities) == 0 || len(catalog.Items) == 0 || catalog.NativeReady {
		t.Fatal("native capability catalog missing or activated")
	}
	if len(catalog.Acceptance) != len(features.NativeAcceptanceForPlatform("wsl")) {
		t.Fatal("scoped acceptance metadata missing from catalog")
	}
	if len(catalog.Details) != len(features.NativeDetailsForPlatform("wsl")) {
		t.Fatal("native checklist missing from catalog")
	}
	path := "/groups/" + a.group + "/features"
	var scoped struct {
		Acceptance map[string]features.ControlledAcceptance `json:"native_acceptance"`
		Details    []features.NativeDetail                  `json:"native_details"`
		Items      []struct {
			Effective  bool                `json:"effective_enabled"`
			Blockers   []string            `json:"native_blockers"`
			Definition features.Definition `json:"definition"`
		} `json:"items"`
	}
	if err := json.Unmarshal(expect(t, user, "GET", path, nil, 200), &scoped); err != nil ||
		len(scoped.Acceptance) != len(features.NativeAcceptanceForPlatform("wsl")) || len(scoped.Details) != len(catalog.Details) {
		t.Fatal("group catalog lost acceptance metadata", err)
	}
	for _, item := range scoped.Items {
		if item.Effective || len(item.Definition.NativeDependencies) != len(item.Blockers) {
			t.Fatal("historical controlled result activated a business module")
		}
	}
	expect(t, user, "GET", "/groups/"+b.group+"/features", nil, 404)
	expect(t, user, "PUT", "/groups/"+b.group+"/features", map[string]any{"version": 1,
		"changes": map[string]any{"keyword_replies": map[string]any{"enabled": true}}}, 404)
	write := func(client testClient, version int, changes any, status int) json.RawMessage {
		return expect(t, client, "PUT", path, map[string]any{"version": version, "changes": changes}, status)
	}
	initial := write(admin, 1, map[string]any{
		"keyword_replies": map[string]any{
			"enabled": true, "admin_only": true, "rules": []features.KeywordRule{{Keyword: "hello", Reply: "secret-reply"}}},
		"blocked_words": map[string]any{"rules": []features.BlockedRule{{Name: "pair", Words: []string{"one", "two"}}}}}, 200)
	if bytes.Contains(initial, []byte(`"applied":true`)) {
		t.Fatal("configuration activated native execution")
	}
	preview := path + "/preview"
	previewInput := func(version int, changes any) map[string]any {
		return map[string]any{"version": version, "feature": "keyword_replies", "changes": changes,
			"text": "hello", "sender": "synthetic-member", "role": "member"}
	}
	var beforePreview string
	snapshot := `SELECT json_build_array(b.config,b.version,b.epoch,
		(SELECT count(*) FROM audit_events),(SELECT count(*) FROM operations),
		(SELECT count(*) FROM business_rule_jobs),(SELECT count(*) FROM business_rule_attempts))::text
		FROM group_bindings b WHERE b.group_id=$1`
	if err := db.Platform.QueryRow(ctx, snapshot, a.group).Scan(&beforePreview); err != nil {
		t.Fatal(err)
	}
	// Unsaved changes share the field validators, but can only simulate a rule.
	out := expect(t, admin, "POST", preview, previewInput(2, map[string]any{"keyword_replies": map[string]any{
		"admin_only": false, "cooldown_seconds": 90,
		"rules": []features.KeywordRule{{Keyword: "hello", Reply: "preview-only", Match: "exact"}}}}), 200)
	var simulated struct {
		Applied         bool                      `json:"applied"`
		NativeSubmitted bool                      `json:"native_submitted"`
		ValidationLevel string                    `json:"validation_level"`
		CooldownSeconds int                       `json:"cooldown_seconds"`
		Decision        features.BusinessDecision `json:"decision"`
	}
	if err := json.Unmarshal(out, &simulated); err != nil || simulated.Applied || simulated.NativeSubmitted ||
		simulated.ValidationLevel != "simulation" || simulated.CooldownSeconds != 90 ||
		simulated.Decision.Text != "preview-only" || simulated.Decision.Action != features.BusinessReply {
		t.Fatal("preview did not return an explicit simulation", string(out), err)
	}
	expect(t, admin, "POST", preview, previewInput(1, nil), 409)
	expect(t, user, "POST", "/groups/"+b.group+"/features/preview", previewInput(1, nil), 404)
	expect(t, admin, "POST", preview, previewInput(2, map[string]any{
		"blocked_words": map[string]any{"words": []string{"hello"}}}), 400)
	badPreview := previewInput(2, nil)
	badPreview["role"] = "superadmin"
	expect(t, admin, "POST", preview, badPreview, 400)
	badPreview = previewInput(2, nil)
	badPreview["native_debug"] = true
	expect(t, admin, "POST", preview, badPreview, 400)
	otherPlatform := admin
	otherPlatform.workspace = "lajiaoliao"
	expect(t, otherPlatform, "POST", preview, previewInput(2, nil), 404)
	var afterPreview string
	if err := db.Platform.QueryRow(ctx, snapshot, a.group).Scan(&afterPreview); err != nil || afterPreview != beforePreview {
		t.Fatal("preview saved data, audited content, or queued native work", err)
	}
	expect(t, admin, "PUT", "/users/"+a.user+"/access", map[string]any{"role_code": "custom", "version": 0,
		"permissions": []string{"groups:read", "feature:keyword_replies:enabled"}}, 200)
	expect(t, user, "GET", "/me", nil, 401)
	user = login("feature-tenant-a")
	// Read access permits checking saved rules, never previewing unauthorized edits.
	expect(t, user, "POST", preview, previewInput(2, nil), 200)
	if _, err := db.Platform.Exec(ctx, `UPDATE plans SET modules=ARRAY['moderation'],version=version+1
		WHERE id=(SELECT plan_id FROM subscriptions WHERE tenant_id=$1)`, a.tenant); err != nil {
		t.Fatal(err)
	}
	expect(t, user, "POST", preview, previewInput(2, nil), 200)
	expect(t, user, "POST", preview, previewInput(2, map[string]any{
		"keyword_replies": map[string]any{"enabled": false}}), 403)
	if _, err := db.Platform.Exec(ctx, `UPDATE plans SET modules=ARRAY['moderation','replies'],version=version+1
		WHERE id=(SELECT plan_id FROM subscriptions WHERE tenant_id=$1)`, a.tenant); err != nil {
		t.Fatal(err)
	}
	expect(t, user, "POST", preview, previewInput(2, map[string]any{
		"keyword_replies": map[string]any{"cooldown_seconds": 60}}), 403)
	expect(t, user, "POST", preview, previewInput(2, map[string]any{
		"keyword_replies": map[string]any{"rules": []features.KeywordRule{}}}), 403)
	expect(t, user, "PUT", path, map[string]any{"version": 2, "changes": map[string]any{
		"blocked_words": map[string]any{"rules": []features.BlockedRule{{Name: "pair", Words: []string{"a", "b"}}}}}}, 403)
	write(user, 2, map[string]any{"keyword_replies": map[string]any{"admin_only": false}}, 403)
	write(user, 2, map[string]any{"keyword_replies": map[string]any{"rules": []features.KeywordRule{}}}, 403)
	write(user, 2, map[string]any{"keyword_replies": map[string]any{"enabled": false}}, 200)
	write(user, 2, map[string]any{"keyword_replies": map[string]any{"enabled": true}}, 409)
	write(admin, 3, map[string]any{"keyword_replies": map[string]any{"enabled": "true"}}, 400)
	// Demo bypasses commercial ceilings, not field schemas or implementation.
	for _, row := range []struct {
		changes any
		status  int
		code    string
	}{
		{map[string]any{"keyword_replies": map[string]any{"timed_publish": true}}, 400, "invalid_feature_settings"},
		{map[string]any{"scheduled_messages": map[string]any{"enabled": true}}, 400, "invalid_feature_settings"},
	} {
		status, _, code := admin.request("PUT", path, map[string]any{"version": 3, "changes": row.changes})
		if status != row.status || code != row.code {
			t.Fatal("unknown or unimplemented field was admitted", status, code)
		}
	}
	var raw []byte
	if err := db.Platform.QueryRow(ctx, `SELECT config FROM group_bindings WHERE group_id=$1`, a.group).Scan(&raw); err != nil {
		t.Fatal(err)
	}
	var config features.Config
	json.Unmarshal(raw, &config)
	if config.FeatureOptions["keyword_replies"].Enabled || !config.FeatureOptions["keyword_replies"].AdminOnly {
		t.Fatal("field denial modified another setting")
	}
	// Legacy round trips preserve switches; they cannot bypass field permissions.
	legacy := config
	legacy.FeatureOptions = nil
	legacy.BlockedRules = nil
	expect(t, admin, "PATCH", "/groups/"+a.group, map[string]any{"version": 3, "management_enabled": false, "ai_enabled": false, "config": legacy}, 200)
	var retainedRules int
	if err := db.Platform.QueryRow(ctx, `SELECT jsonb_array_length(config->'blocked_rules') FROM group_bindings WHERE group_id=$1`,
		a.group).Scan(&retainedRules); err != nil || retainedRules != 1 {
		t.Fatal("legacy caller erased separately scoped combination rules", err)
	}
	legacyBlob, _ := json.Marshal(legacy)
	var legacyFields map[string]any
	if err := json.Unmarshal(legacyBlob, &legacyFields); err != nil {
		t.Fatal(err)
	}
	legacyFields["blocked_rules"] = []any{}
	expect(t, admin, "PATCH", "/groups/"+a.group, map[string]any{"version": 4,
		"management_enabled": false, "ai_enabled": false, "config": legacyFields}, 400)
	config.FeatureOptions["keyword_replies"] = features.Options{Enabled: true}
	expect(t, admin, "PATCH", "/groups/"+a.group, map[string]any{"version": 4, "management_enabled": false, "ai_enabled": false, "config": config}, 400)
	var unchanged bool
	db.Platform.QueryRow(ctx, `SELECT (config->'feature_options'->'keyword_replies'->>'admin_only')::boolean FROM group_bindings WHERE group_id=$1`, a.group).Scan(&unchanged)
	if !unchanged {
		t.Fatal("legacy caller erased feature switches")
	}
	expect(t, login("feature-tenant-b"), "PUT", "/groups/"+b.group+"/features", map[string]any{"version": 1,
		"changes": map[string]any{"keyword_replies": map[string]any{"enabled": true}}}, 403)
	expect(t, admin, "PUT", "/groups/"+b.group+"/features", map[string]any{"version": 1,
		"changes": map[string]any{"keyword_replies": map[string]any{"enabled": true}}}, 200)
	if _, err := db.Platform.Exec(ctx, `INSERT INTO member_groups(user_id,tenant_id,group_id) VALUES($1,$2,$3)`, a.user, a.tenant, a.group); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE tenants SET owner_id=NULL WHERE id=$1`, a.tenant); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE users SET tenant_role='legacy',disabled=true WHERE id=$1`, a.user); err != nil {
		t.Fatal(err)
	}
	// Simulate a request authenticated just before its actor was disabled.
	payload, _ := json.Marshal(map[string]any{"version": 4,
		"changes": map[string]any{"keyword_replies": map[string]any{"enabled": true}}})
	request := httptest.NewRequest("PUT", path, bytes.NewReader(payload))
	request.Header.Set("Content-Type", "application/json")
	request.SetPathValue("id", a.group)
	err := s.saveGroupFeatures(httptest.NewRecorder(), request, Principal{ID: a.user, TenantID: a.tenant, Role: "user"})
	if err == nil {
		t.Fatalf("disabled actor passed final permission check: %v", err)
	}
	if _, err := db.Platform.Exec(ctx, `UPDATE users SET disabled=false WHERE id=$1`, a.user); err != nil {
		t.Fatal(err)
	}
	expect(t, admin, "PUT", "/users/"+a.user+"/access", map[string]any{"role_code": "custom", "version": 1,
		"permissions": []string{"groups:read"}}, 200)
	user = login("feature-tenant-a")
	write(user, 4, map[string]any{"keyword_replies": map[string]any{"enabled": true}}, 403)
	if _, err := db.Platform.Exec(ctx, `UPDATE subscriptions SET expires_at=now()-interval '1 second' WHERE tenant_id=$1`, a.tenant); err != nil {
		t.Fatal(err)
	}
	write(admin, 4, map[string]any{"keyword_replies": map[string]any{"enabled": true}}, 200)
	var leaked bool
	if err := db.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_events WHERE action='group_features_updated'
        AND detail::text LIKE '%secret-reply%')`).Scan(&leaked); err != nil || leaked {
		t.Fatal("feature audit retained reply content")
	}
	maibot := admin
	maibot.workspace = "maibot"
	expect(t, maibot, "GET", path, nil, 403)
	expect(t, maibot, "GET", "/features/catalog", nil, 403)
}
