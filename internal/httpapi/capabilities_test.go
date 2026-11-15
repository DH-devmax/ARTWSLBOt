package httpapi

import (
	"bytes"
	"encoding/json"
	"testing"

	"wangshangliao/internal/assistant"
	"wangshangliao/internal/control"
)

func explicitTestCeiling(t *testing.T, admin testClient, tenant string) {
	t.Helper()
	permissions := []string{}
	for _, spec := range control.BusinessPermissionCatalog() {
		permissions = append(permissions, spec.Code)
	}
	expect(t, admin, "PUT", "/tenants/"+tenant+"/capabilities",
		map[string]any{"permissions": permissions, "version": 0}, 200)
}

func TestPostgresCapabilityCeilingDefaultsClosedAndOwnerIsExplicit(t *testing.T) {
	f := newCommercialFixture(t)
	owner := f.ownerWithCeiling("no-ceiling-owner", "PRO", nil)
	path := "/tenants/" + owner.tenant + "/capabilities"
	before := expect(t, f.admin, "GET", path, nil, 200)
	if !bytes.Contains(before, []byte(`"version":0`)) || !bytes.Contains(before, []byte(`"ceiling":[]`)) {
		t.Fatal("missing ceiling was inferred from owner/package", string(before))
	}
	expect(t, owner.client, "GET", "/members", nil, 403)
	expect(t, owner.client, "PUT", "/member-roles/scoped", map[string]any{
		"name": "Scoped", "permissions": []string{"groups:read"}, "version": 0,
	}, 403)
	explicitTestCeiling(t, f.admin, owner.tenant)
	expect(t, owner.client, "GET", "/me", nil, 401)
	owner.client = f.login("no-ceiling-owner")
	expect(t, owner.client, "GET", "/members", nil, 200)
	expect(t, owner.client, "PUT", path, map[string]any{"permissions": []string{}, "version": 1}, 403)
	expect(t, f.admin, "GET", "/members", nil, 400)
	expect(t, f.admin, "GET", "/members?tenant_id=invalid", nil, 400)
	rows := expect(t, f.admin, "GET", "/members?tenant_id="+owner.tenant, nil, 200)
	if !bytes.Contains(rows, []byte(owner.id)) {
		t.Fatal("explicit admin tenant scope was not applied", string(rows))
	}
	roles := expect(t, f.admin, "GET", "/member-roles?tenant_id="+owner.tenant, nil, 200)
	if field(t, roles, "tenant_id") != owner.tenant {
		t.Fatal("role projection did not bind tenant")
	}
	// Old members:manage grants and even a forged role label are not ownership.
	fake := control.NewID()
	_, err := f.db.Platform.Exec(f.ctx, `INSERT INTO users(id,tenant_id,username,password_hash,role,tenant_role)
		SELECT $1,$2,'fake-owner',password_hash,'user','owner' FROM users WHERE id=$3`,
		fake, owner.tenant, owner.id)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.db.Platform.Exec(f.ctx, `INSERT INTO user_access(user_id,role_code,permissions)
		VALUES($1,'manager',ARRAY['members:manage','groups:read'])`, fake)
	if err != nil {
		t.Fatal(err)
	}
	fakeClient := f.login("fake-owner")
	expect(t, fakeClient, "GET", "/members", nil, 403)
	expect(t, fakeClient, "GET", "/member-roles", nil, 403)
	expect(t, owner.client, "GET", "/members?tenant_id="+control.NewID(), nil, 403)
}

func TestPostgresBusinessGatesEvidenceAndRuntimeAreScoped(t *testing.T) {
	f := newCommercialFixture(t)
	a := f.owner("capability-owner", "PRO")
	g, account := f.group(a, "scoped-capability-group", false)
	path := "/capabilities/gates/mute_member/assistant"
	initial := expect(t, f.admin, "GET", path, nil, 200)
	if !bytes.Contains(initial, []byte(`"state":"closed"`)) || !bytes.Contains(initial, []byte(`"version":0`)) {
		t.Fatal("production gate default is not closed", string(initial))
	}
	expect(t, a.client, "PUT", path, map[string]any{"state": "open", "version": 0}, 403)
	expect(t, f.admin, "PUT", path, map[string]any{"state": "open", "version": 0}, 400)
	expect(t, f.admin, "PUT", path, map[string]any{"state": "open", "version": 0,
		"evidence_refs": []string{"https://synthetic.invalid/report?secret=blocked"}}, 400)
	body := map[string]any{"state": "pilot", "version": 0, "pilot_group_ids": []string{g},
		"evidence_refs": []string{"acceptance:SYNTHETIC-WSL-001", "docs/test-report.md#合成回归"}}
	saved := expect(t, f.admin, "PUT", path, body, 200)
	for _, token := range []string{`"state":"pilot"`, `"version":1`, "acceptance:SYNTHETIC-WSL-001", g} {
		if !bytes.Contains(saved, []byte(token)) {
			t.Fatal("saved gate lost state/version/evidence", token, string(saved))
		}
	}
	loaded := expect(t, f.admin, "GET", path, nil, 200)
	var old, current map[string]json.RawMessage
	json.Unmarshal(saved, &old)
	json.Unmarshal(loaded, &current)
	if !bytes.Equal(old["evidence_refs"], current["evidence_refs"]) || !bytes.Equal(old["version"], current["version"]) {
		t.Fatal("gate evidence did not read back")
	}
	expect(t, f.admin, "PUT", path, body, 409)
	expect(t, f.admin, "GET", "/capabilities/gates/publish_notice/automatic", nil, 404)
	expect(t, f.admin, "GET", "/capabilities/gates/mute_member/heatbot", nil, 404)
	chili := f.admin
	chili.workspace = "lajiaoliao"
	other := expect(t, chili, "GET", path, nil, 200)
	if !bytes.Contains(other, []byte(`"state":"closed"`)) {
		t.Fatal("one platform inherited another platform's pilot")
	}
	expect(t, chili, "PUT", path, body, 400)
	runtime := "/groups/" + g + "/business-runtime"
	view := expect(t, a.client, "GET", runtime, nil, 200)
	if !bytes.Contains(view, []byte(`"enabled":false`)) || !bytes.Contains(view, []byte(`"can_manage":true`)) {
		t.Fatal("runtime default/read-only projection", string(view))
	}
	var runtimeVersion int64
	if err := f.db.Platform.QueryRow(f.ctx, `SELECT version FROM group_business_runtime WHERE group_id=$1`, g).Scan(&runtimeVersion); err != nil {
		t.Fatal(err)
	}
	status, _, code := a.client.request("PUT", runtime, map[string]any{"enabled": true, "version": runtimeVersion})
	if status != 409 || code != "group_ownership_proof_required" {
		t.Fatal("legacy binding was treated as authenticated ownership", status, code)
	}
	expect(t, a.client, "PUT", runtime, map[string]any{"enabled": false, "version": runtimeVersion}, 200)
	expect(t, a.client, "PUT", runtime, map[string]any{"enabled": false, "version": runtimeVersion}, 409)
	var ready, aiReady bool
	raw := expect(t, f.admin, "GET", "/features/catalog", nil, 200)
	var readiness struct {
		Native bool `json:"native_ready"`
		AI     bool `json:"ai_ready"`
	}
	json.Unmarshal(raw, &readiness)
	ready, aiReady = readiness.Native, readiness.AI
	if ready || aiReady {
		t.Fatal("scoped admission changed global readiness")
	}
	// Superadmin queries can use all real bindings with their own session.
	ref := onboardingRef(t, f.admin, "wsl")
	sess, err := f.s.Assistant.CreateSession(f.ctx, ref, assistant.Scope{Platform: "wsl", Kind: "group", GroupID: g})
	if err != nil || sess.Scope.AccountID != account || sess.Actor.ID != f.adminActor.ID {
		t.Fatal("admin same-assistant scope did not resolve its own session", sess, err)
	}
	_, err = f.s.Assistant.CreateSession(f.ctx, ref, assistant.Scope{Platform: "wsl", Kind: "group", GroupID: control.NewID()})
	if err == nil {
		t.Fatal("superadmin invented an unbound group")
	}
	// An explicitly bound account change stops runtime rather than carrying it.
	_, err = f.db.Platform.Exec(f.ctx, `UPDATE group_bindings SET active=false WHERE group_id=$1`, g)
	if err != nil {
		t.Fatal(err)
	}
	var enabled bool
	if err = f.db.Platform.QueryRow(f.ctx, `SELECT enabled FROM group_business_runtime WHERE group_id=$1`, g).Scan(&enabled); err != nil || enabled {
		t.Fatal("binding revocation retained runtime", err)
	}
	expect(t, f.admin, "GET", runtime, nil, 404)
}
