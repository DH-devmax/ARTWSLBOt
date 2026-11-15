package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

func TestPostgresNativeSocialAuthorizationDurabilityAndLeave(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{52}, 32)))
	service := &control.NativeSettingsService{DB: db, Vault: vault}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	account, actor, tenant, plan, group := control.NewID(), control.NewID(), control.NewID(), control.NewID(), control.NewID()
	token, _ := security.Token()
	ref, _ := control.WebsiteSession(token)
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'social-owner','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`,
		security.Digest(token), security.Digest("csrf"), actor)
	exec(`INSERT INTO accounts(id,name,native_uid,auth_state,credential_sealed,connection_enabled,testing_enabled)
        VALUES($1,'social-account','7','authenticated',$2,true,true)`, account, []byte("synthetic-social-session"))
	exec(`INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,77)`, []byte("synthetic-deployment"))
	exec(`INSERT INTO tenants(id,name) VALUES($1,'social-tenant')`, tenant)
	exec(`INSERT INTO plans(id,name,group_limit,ai_group_limit,modules) VALUES($1,'social-plan',10,0,ARRAY['friends'])`, plan)
	exec(`INSERT INTO subscriptions(tenant_id,plan_id,expires_at) VALUES($1,$2,now()+interval '1 day')`, tenant, plan)
	exec(`INSERT INTO native_groups(id,native_id,name,isolated_test) VALUES($1,'123','social-group',true)`, group)
	exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,active,management_enabled)
        VALUES($1,$2,$3,true,true)`, group, tenant, account)
	exec(`INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
        VALUES($1,$2,'admin',now()-interval '1 minute','native')`, account, group)
	req := control.SettingRequest{Account: account, Workspace: "wsl", Epoch: 1, Version: 1,
		Key: control.NewID(), Digest: strings.Repeat("a", 64),
		Change: nativeauth.SettingChange{Kind: "friend_reject", Target: "8", Text: "9007199254740993"}}
	proof := func() control.SettingEvidence {
		p := control.SettingEvidence{UID: "7", Epoch: 1, Version: 1, DeploymentVersion: 77,
			Role: "admin", Target: req.Change.Target, Digest: req.Digest, SocialAllowed: true,
			CredentialDigest: sha256.Sum256([]byte("synthetic-social-session")), ObservedAt: time.Now()}
		if req.Change.Kind == "group_leave" {
			p.Role = "member"
		}
		return p
	}
	submissions := 0
	port := func(work context.Context, fence control.SettingFence) error {
		return fence(work, proof(), func(context.Context) error { submissions++; return nil })
	}
	if _, err := service.Execute(ctx, ref, req, port); err == nil {
		t.Fatal("profile endpoint admitted social action")
	}
	out, err := service.ExecuteSocial(ctx, ref, req, port)
	if err != nil || out.Status != "succeeded" || out.Code != "native_friend_decision_acknowledged" {
		t.Fatal(out, err)
	}
	again, err := service.ExecuteSocial(ctx, ref, req, port)
	if err != nil || again.ID != out.ID || submissions != 1 {
		t.Fatal("same-key approval duplicated native submission", again, err)
	}
	req.Key, req.Change = control.NewID(), nativeauth.SettingChange{Kind: "group_invite", Group: "123", Target: "8"}
	if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil {
		t.Fatal("membership mutation bypassed persisted human grant")
	}
	t.Run("ordinary member can grant only self departure", func(t *testing.T) {
		s := newCaptchaTestServer(db, vault, "http://127.0.0.1:5173", false)
		defer s.Native.Close()
		create := func(actions, targets []string) error {
			raw, _ := json.Marshal(map[string]any{"group_id": group, "account_ids": []string{account},
				"target_ids": targets, "actions": actions, "seconds": 300, "budget": 1})
			r := httptest.NewRequest("POST", "/api/v1/test-windows", bytes.NewReader(raw))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("X-Workspace", "wsl")
			return s.createTestWindow(httptest.NewRecorder(), r, Principal{ID: actor, Role: "superadmin"})
		}
		exec(`UPDATE account_groups SET role='member',observed_at=now()`)
		if err := create([]string{"group_leave"}, []string{"7"}); err != nil {
			t.Fatal("ordinary owned member could not receive a self-departure grant", err)
		}
		for _, scope := range []struct{ actions, targets []string }{
			{[]string{"group_invite"}, []string{"8"}},
			{[]string{"group_leave", "group_invite"}, []string{"7", "8"}},
			{[]string{"group_leave"}, []string{"8"}},
		} {
			if err := create(scope.actions, scope.targets); err == nil {
				t.Fatal("ordinary member grant expanded", scope)
			}
		}
		exec(`UPDATE account_groups SET role='owner'`)
		if err := create([]string{"group_leave"}, []string{"7"}); err == nil {
			t.Fatal("group owner received a departure grant")
		}
		exec(`UPDATE account_groups SET role='admin',observed_at=now()`)
	})
	window := control.NewID()
	epochs, _ := json.Marshal(map[string]int{account: 1})
	exec(`INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,account_epochs,expires_at,budget)
        VALUES($1,$2,$3,ARRAY[$4::uuid],ARRAY['7','8'],ARRAY['group_invite','group_leave'],$5,now()+interval '300 seconds',10)`,
		window, actor, group, account, epochs)
	req.TestWindowID = window
	if _, err := service.TestScope(ctx, ref, "wsl", window, account, "read_membership", ""); err != nil {
		t.Fatal("friends-only plan cannot read membership", err)
	}
	if _, err := service.TestScope(ctx, ref, "wsl", window, account, "mute_member", "8"); err == nil {
		t.Fatal("friends grant inherited moderation")
	}
	for _, pair := range [][2]string{
		{`UPDATE test_windows SET target_ids=ARRAY['7']`, `UPDATE test_windows SET target_ids=ARRAY['7','8']`},
		{`UPDATE test_windows SET cancelled=true`, `UPDATE test_windows SET cancelled=false`},
		{`UPDATE accounts SET testing_enabled=false`, `UPDATE accounts SET testing_enabled=true`},
		{`UPDATE test_windows SET actions=ARRAY['group_leave']`, `UPDATE test_windows SET actions=ARRAY['group_invite','group_leave']`},
	} {
		exec(pair[0])
		if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil || submissions != 1 {
			t.Fatal("membership escaped grant", pair[0], err)
		}
		exec(pair[1])
	}
	tests := []struct{ before, after string }{
		{`UPDATE users SET disabled=true`, `UPDATE users SET disabled=false`},
		{`UPDATE accounts SET connection_enabled=false`, `UPDATE accounts SET connection_enabled=true`},
		{`UPDATE group_bindings SET active=false`, `UPDATE group_bindings SET active=true`},
		{`UPDATE plans SET modules=ARRAY['cards'] WHERE id NOT IN (SELECT plan_id FROM commercial_plans)`, `UPDATE plans SET modules=ARRAY['friends'] WHERE id NOT IN (SELECT plan_id FROM commercial_plans)`},
		{`UPDATE plans SET allowed_platforms=ARRAY['lajiaoliao'] WHERE id NOT IN (SELECT plan_id FROM commercial_plans)`, `UPDATE plans SET allowed_platforms=ARRAY['wsl'] WHERE id NOT IN (SELECT plan_id FROM commercial_plans)`},
		{`UPDATE subscriptions SET expires_at=now()-interval '1 day'`, `UPDATE subscriptions SET expires_at=now()+interval '1 day'`},
	}
	for _, test := range tests {
		req.Key = control.NewID()
		exec(test.before)
		if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil || submissions != 1 {
			t.Fatal("missing current authorization admitted action", test.before, err)
		}
		exec(test.after)
		exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour') ON CONFLICT DO NOTHING`,
			security.Digest(token), security.Digest("synthetic-csrf"), actor)
	}
	req.Key = control.NewID()
	revoked, err := service.ExecuteSocial(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		exec(`UPDATE group_bindings SET epoch=epoch+1`)
		return port(work, fence)
	})
	if err != nil || revoked.Status != "failed" || submissions != 1 {
		t.Fatal("binding epoch changed after preparation escaped final fence", revoked, err)
	}
	req.Key = control.NewID()
	roleBlocked, err := service.ExecuteSocial(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		p := proof()
		p.Role = "member"
		return fence(work, p, func(context.Context) error { submissions++; return nil })
	})
	if err != nil || roleBlocked.Status != "failed" || submissions != 1 {
		t.Fatal("ordinary member could invite without role policy", roleBlocked, err)
	}
	req.Key = control.NewID()
	out, err = service.ExecuteSocial(ctx, ref, req, port)
	if err != nil || out.Code != "native_invitation_requested" || submissions != 2 {
		t.Fatal("invitation falsely reported membership", out, err)
	}
	req.Key, req.Change = control.NewID(), nativeauth.SettingChange{Kind: "group_leave", Group: "123"}
	if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil {
		t.Fatal("left with group service enabled")
	}
	exec(`UPDATE group_bindings SET management_enabled=false`)
	exec(`UPDATE test_windows SET target_ids=ARRAY['8']`)
	if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil {
		t.Fatal("leave did not require the account's own UID in the grant")
	}
	exec(`UPDATE test_windows SET target_ids=ARRAY['7','8']`)
	exec(`UPDATE account_groups SET role='member'`)
	req.Key = control.NewID()
	out, err = service.ExecuteSocial(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		if err := port(work, fence); err != nil {
			return err
		}
		return &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
	})
	if err != nil || out.Status != "unknown" || submissions != 3 {
		t.Fatal("ambiguous leave was retried or lost", out, err)
	}
	again, err = service.ExecuteSocial(ctx, ref, req, port)
	if err != nil || again.ID != out.ID || submissions != 3 {
		t.Fatal("unknown operation resubmitted", err)
	}
	exec(`UPDATE test_windows SET cancelled=true`)
	if rows, err := service.TestOperations(ctx, ref, account); err != nil || len(rows) != 4 || rows[0].ID != out.ID {
		t.Fatal("membership history lost unknown operation after cancellation", rows, err)
	}
	req.Key = control.NewID()
	if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil {
		t.Fatal("new operation bypassed unknown leave")
	}
	observed, err := service.Reconcile(ctx, ref, account, out.ID, func(context.Context, control.SettingRequest) (control.SettingEvidence, error) {
		p := proof()
		p.Matches = true
		return p, nil
	})
	if err != nil || observed.Code != "desired_state_observed" || submissions != 3 {
		t.Fatal("read-only reconciliation resubmitted leave", observed, err)
	}
	var role string
	var active bool
	var epoch int64
	if err := db.Platform.QueryRow(ctx, `SELECT ag.role,b.active,b.epoch FROM account_groups ag
        JOIN group_bindings b USING(group_id) WHERE ag.account_id=$1`, account).Scan(&role, &active, &epoch); err != nil ||
		role != "left" || !active || epoch < 3 {
		t.Fatal("leave removed ownership/history or retained stale role", role, active, epoch, err)
	}
	var plain bool
	if err := db.Platform.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_events WHERE detail::text LIKE '%9007199254740993%')`).Scan(&plain); err != nil || plain {
		t.Fatal("friend application payload leaked into audit", err)
	}
}

func TestSelfInvitationGrantRejectsForeignAndProcessedRecords(t *testing.T) {
	record := nativeauth.InvitationRecord{ID: "11", Group: "123", Type: "INVITATION",
		State: "MEMBER_STATE_INVITED", Applicant: nativeauth.Friend{ID: "7"}}
	view := gateway.SocialSnapshot{SettingSnapshot: gateway.SettingSnapshot{SelfID: "7"},
		Group: "123", DirectoryKnown: true, InvitesKnown: true, Invitations: []nativeauth.InvitationRecord{record}}
	if !selfInvitationGrantAllowed(view) {
		t.Fatal("fresh owned invitation could not grant self acceptance")
	}
	for _, mutate := range []func(*gateway.SocialSnapshot){
		func(s *gateway.SocialSnapshot) { s.Present = true },
		func(s *gateway.SocialSnapshot) { s.SelfID = "8" },
		func(s *gateway.SocialSnapshot) { s.Group = "999" },
		func(s *gateway.SocialSnapshot) { s.InvitesKnown = false },
		func(s *gateway.SocialSnapshot) { s.DirectoryKnown = false },
		func(s *gateway.SocialSnapshot) { s.Invitations = nil },
	} {
		bad := view
		mutate(&bad)
		if selfInvitationGrantAllowed(bad) {
			t.Fatal("unverified invitation authorized self acceptance", bad)
		}
	}
	view.Invitations[0].State = "MEMBER_STATE_GOOD"
	if selfInvitationGrantAllowed(view) {
		t.Fatal("processed invitation received a new grant")
	}
}

func TestPostgresSelfInvitationDurabilityRevocationAndNoRoleFabrication(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) { testSelfInvitationDurability(t, platform) })
	}
}

func testSelfInvitationDurability(t *testing.T, platform string) {
	db := integrationDB(t)
	ctx := context.Background()
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{53}, 32)))
	service := &control.NativeSettingsService{DB: db, Vault: vault}
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	account, actor, tenant, plan, group := control.NewID(), control.NewID(), control.NewID(), control.NewID(), control.NewID()
	self, nativeGroup, invitation, deployment, admission := "7", "123", "9007199254740993", int64(77), "package"
	if platform == "lajiaoliao" {
		self, nativeGroup, invitation, deployment, admission = "self-string", "group-string", strings.Repeat("b", 64), 0, "native_debug"
	}
	token, _ := security.Token()
	ref, _ := control.WebsiteSession(token)
	exec(`INSERT INTO users(id,username,password_hash,role) VALUES($1,'self-invite-owner','synthetic','superadmin')`, actor)
	exec(`INSERT INTO sessions(token_hash,csrf_hash,user_id,expires_at) VALUES($1,$2,$3,now()+interval '1 hour')`,
		security.Digest(token), security.Digest("csrf"), actor)
	exec(`INSERT INTO accounts(id,name,workspace,platform,native_uid,auth_state,credential_sealed,connection_enabled,testing_enabled)
        VALUES($1,'self-invite-account',$3,$3,$4,'authenticated',$2,true,true)`, account, []byte("synthetic-self-invitation"), platform, self)
	if platform == "wsl" {
		exec(`INSERT INTO native_deployment(singleton,sealed,version) VALUES(true,$1,77)`, []byte("synthetic-deployment"))
	}
	exec(`INSERT INTO tenants(id,name) VALUES($1,'self-invite-tenant')`, tenant)
	exec(`INSERT INTO plans(id,name,group_limit,ai_group_limit,modules,allowed_platforms)
        VALUES($1,'self-invite-plan',10,0,ARRAY['friends'],ARRAY[$2])`, plan, platform)
	exec(`INSERT INTO subscriptions(tenant_id,plan_id,expires_at) VALUES($1,$2,now()+interval '1 day')`, tenant, plan)
	exec(`INSERT INTO native_groups(id,platform,native_id,name,isolated_test)
        VALUES($1,$2,$3,'self-invite-group',true)`, group, platform, nativeGroup)
	exec(`INSERT INTO group_bindings(group_id,tenant_id,primary_account_id,active,management_enabled,ai_enabled)
        VALUES($1,$2,$3,true,false,false)`, group, tenant, account)
	epochs, _ := json.Marshal(map[string]int{account: 1})
	newWindow := func() string {
		id := control.NewID()
		exec(`INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,account_epochs,expires_at,budget,admission_mode)
            VALUES($1,$2,$3,ARRAY[$4::uuid],ARRAY[$6],ARRAY['group_accept'],$5,now()+interval '300 seconds',1,$7)`,
			id, actor, group, account, epochs, self, admission)
		return id
	}
	req := control.SettingRequest{Account: account, Workspace: platform, Epoch: 1, Version: 1,
		Key: control.NewID(), Digest: strings.Repeat("b", 64), TestWindowID: newWindow(),
		Change: nativeauth.SettingChange{Kind: "group_accept", Group: nativeGroup, Target: self, Text: invitation}}
	proof := func() control.SettingEvidence {
		return control.SettingEvidence{UID: self, Epoch: 1, Version: 1, DeploymentVersion: deployment,
			Target: req.Change.Target, Digest: req.Digest, SocialAllowed: true,
			CredentialDigest: sha256.Sum256([]byte("synthetic-self-invitation")), ObservedAt: time.Now()}
	}
	submissions := 0
	port := func(work context.Context, fence control.SettingFence) error {
		return fence(work, proof(), func(context.Context) error { submissions++; return nil })
	}
	if _, err := service.TestScope(ctx, ref, platform, req.TestWindowID, account, "read_membership", ""); err != nil {
		t.Fatal("absent account could not read its invitation scope", err)
	}
	otherPlatform := "wsl"
	if platform == otherPlatform {
		otherPlatform = "lajiaoliao"
	}
	if _, err := service.TestScope(ctx, ref, otherPlatform, req.TestWindowID, account, "read_membership", ""); err == nil {
		t.Fatal("self invitation inherited the other platform's grant")
	}
	req.Change.Target = "8"
	if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil || submissions != 0 {
		t.Fatal("grant accepted another account's invitation", err)
	}
	req.Change.Target = self
	revoked, err := service.ExecuteSocial(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, req.TestWindowID)
		return port(work, fence)
	})
	if err != nil || revoked.Status != "failed" || submissions != 0 {
		t.Fatal("grant cancellation during preparation reached native transport", revoked, err)
	}
	req.Key, req.TestWindowID = control.NewID(), newWindow()
	out, err := service.ExecuteSocial(ctx, ref, req, func(work context.Context, fence control.SettingFence) error {
		if err := port(work, fence); err != nil {
			return err
		}
		return &nativeauth.Error{Code: "native_transport_unknown", Submitted: true}
	})
	if err != nil || out.Status != "unknown" || submissions != 1 {
		t.Fatal("ambiguous acceptance did not remain unknown", out, err)
	}
	again, err := service.ExecuteSocial(ctx, ref, req, port)
	if err != nil || again.ID != out.ID || submissions != 1 {
		t.Fatal("unknown self acceptance was replayed", again, err)
	}
	exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, req.TestWindowID)
	observed, err := service.Reconcile(ctx, ref, account, out.ID,
		func(context.Context, control.SettingRequest) (control.SettingEvidence, error) {
			p := proof()
			p.Matches = true
			return p, nil
		})
	if err != nil || observed.Code != "desired_state_observed" || submissions != 1 {
		t.Fatal("closed grant could not reconcile without replay", observed, err)
	}
	var roles int
	if err := db.Platform.QueryRow(ctx, `SELECT count(*) FROM account_groups WHERE account_id=$1`, account).Scan(&roles); err != nil || roles != 0 {
		t.Fatal("acceptance manufactured a persisted group role", roles, err)
	}
	req.Key, req.TestWindowID = control.NewID(), newWindow()
	out, err = service.ExecuteSocial(ctx, ref, req, port)
	if err != nil || out.Status != "succeeded" || out.Code != "native_invitation_accept_acknowledged" || submissions != 2 {
		t.Fatal("acceptance acknowledgement falsely implied membership", out, err)
	}
	req.Key = control.NewID()
	if _, err := service.ExecuteSocial(ctx, ref, req, port); err == nil || submissions != 2 {
		t.Fatal("exhausted self acceptance budget allowed another submission", err)
	}
	t.Run("controlled friend deletion", func(t *testing.T) {
		testFriendDeleteDurability(t, service, ref, req, proof)
	})
	t.Run("controlled friend approval", func(t *testing.T) {
		testFriendApprovalDurability(t, service, ref, req, proof)
	})
}

func TestPostgresChiliSelfInvitationGrantAccountProof(t *testing.T) {
	db := integrationDB(t)
	ctx := context.Background()
	account := control.NewID()
	credential := []byte("synthetic-chili-invitation")
	exec := func(sql string, args ...any) {
		t.Helper()
		if _, err := db.Platform.Exec(ctx, sql, args...); err != nil {
			t.Fatal(err)
		}
	}
	exec(`INSERT INTO accounts(id,name,workspace,platform,native_uid,auth_state,credential_sealed,testing_enabled,connection_enabled)
        VALUES($1,'invited','lajiaoliao','lajiaoliao','self-string','authenticated',$2,true,true)`, account, credential)
	exec(`INSERT INTO account_receivers(account_id,enabled,lease_until,snapshot,observed_at)
        VALUES($1,true,now()+interval '15 seconds','{"state":"connected"}',now())`, account)
	proof := gateway.SocialSnapshot{SettingSnapshot: gateway.SettingSnapshot{
		Platform: "lajiaoliao", SelfID: "self-string", AccountEpoch: 1, AccountVersion: 1,
		CredentialDigest: sha256.Sum256(credential), SampledAt: time.Now()}}
	check := func(p gateway.SocialSnapshot, platform string, targets map[string]bool) bool {
		t.Helper()
		tx, err := db.Platform.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
		epoch, err := selfInvitationAccountEpoch(ctx, tx, account, platform, p, targets)
		return err == nil && epoch == 1
	}
	targets := map[string]bool{"self-string": true}
	if !check(proof, "lajiaoliao", targets) {
		t.Fatal("fresh invited Chili account required a WSL deployment or invented member role")
	}
	if check(proof, "wsl", targets) || check(proof, "lajiaoliao", map[string]bool{"other-user": true}) {
		t.Fatal("foreign platform or target passed the invitation account fence")
	}
	for _, mutate := range []func(*gateway.SocialSnapshot){
		func(p *gateway.SocialSnapshot) { p.SelfID = "other-user" },
		func(p *gateway.SocialSnapshot) { p.AccountEpoch++ },
		func(p *gateway.SocialSnapshot) { p.AccountVersion++ },
		func(p *gateway.SocialSnapshot) { p.DeploymentVersion = 77 },
		func(p *gateway.SocialSnapshot) { p.CredentialDigest = [32]byte{} },
		func(p *gateway.SocialSnapshot) { p.SampledAt = time.Now().Add(-6 * time.Second) },
		func(p *gateway.SocialSnapshot) { p.SampledAt = time.Now().Add(time.Minute) },
	} {
		bad := proof
		mutate(&bad)
		if check(bad, "lajiaoliao", targets) {
			t.Fatal("changed invitation account proof admitted")
		}
	}
	for _, pair := range [][2]string{
		{`UPDATE accounts SET testing_enabled=false`, `UPDATE accounts SET testing_enabled=true`},
		{`UPDATE accounts SET connection_enabled=false`, `UPDATE accounts SET connection_enabled=true`},
		{`UPDATE account_receivers SET enabled=false`, `UPDATE account_receivers SET enabled=true`},
		{`UPDATE account_receivers SET lease_until=now()-interval '1 second'`, `UPDATE account_receivers SET lease_until=now()+interval '15 seconds'`},
		{`UPDATE account_receivers SET observed_at=now()-interval '16 seconds'`, `UPDATE account_receivers SET observed_at=now()`},
		{`UPDATE account_receivers SET snapshot='{"state":"blocked"}'`, `UPDATE account_receivers SET snapshot='{"state":"connected"}'`},
	} {
		exec(pair[0])
		if check(proof, "lajiaoliao", targets) {
			t.Fatal("disabled or stale receiver passed the invitation grant fence")
		}
		exec(pair[1])
	}
}
