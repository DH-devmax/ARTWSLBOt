package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/control"
	"wangshangliao/internal/database"
	"wangshangliao/internal/security"
)

type onboardingNativeFixture struct {
	db                    *database.DB
	owner, robotRole      string
	roleErr               error
	absent, unknown       bool
	roles, invites, sends atomic.Int32
	beforeSubmit          func(context.Context, control.GroupBindingAcceptRequest) error
	beforeRoles           func()
}

func (n *onboardingNativeFixture) connection(ctx context.Context, scope control.GroupBindingNativeScope) (control.GroupBindingConnectionEvidence, error) {
	var out control.GroupBindingConnectionEvidence
	out.Platform, out.AccountID, out.AccountEpoch, out.AccountVersion = scope.Platform, scope.AccountID, scope.AccountEpoch, scope.AccountVersion
	var sealed []byte
	err := n.db.Platform.QueryRow(ctx, `SELECT a.credential_sealed,r.owner,r.revision FROM accounts a
		JOIN account_receivers r ON r.account_id=a.id WHERE a.id=$1`, scope.AccountID).
		Scan(&sealed, &out.ReceiverOwner, &out.ReceiverRevision)
	if err != nil {
		return out, err
	}
	copy(out.CredentialDigest[:], security.Digest(string(sealed)))
	if scope.Platform == "wsl" {
		if err = n.db.Platform.QueryRow(ctx, `SELECT version FROM native_deployment WHERE singleton`).Scan(&out.DeploymentVersion); err != nil {
			return out, err
		}
	}
	out.ObservedAt = time.Now()
	return out, nil
}

func (n *onboardingNativeFixture) BindingRoles(ctx context.Context, scope control.GroupBindingNativeScope) (control.GroupBindingRoleEvidence, error) {
	n.roles.Add(1)
	if n.beforeRoles != nil {
		n.beforeRoles()
	}
	if n.roleErr != nil {
		return control.GroupBindingRoleEvidence{}, n.roleErr
	}
	proof, err := n.connection(ctx, scope)
	role := "member"
	if scope.NativeSenderID == n.owner {
		role = "owner"
	} else if scope.NativeSenderID == "synthetic-native-admin" {
		role = "admin"
	} else if scope.NativeSenderID == "synthetic-bot-"+scope.Platform {
		role = n.robotRole
	}
	return control.GroupBindingRoleEvidence{GroupBindingConnectionEvidence: proof, NativeGroupID: scope.NativeGroupID,
		NativeSenderID: scope.NativeSenderID, NativeOwnerID: n.owner, SenderRole: role, RobotRole: n.robotRole,
		Present: !n.absent, GroupName: "Synthetic native group"}, err
}

func (n *onboardingNativeFixture) BindingInvitation(ctx context.Context, request control.GroupBindingInvitationRequest) (control.GroupBindingInvitationEvidence, error) {
	n.invites.Add(1)
	proof, err := n.connection(ctx, request.GroupBindingNativeScope)
	var self string
	if err == nil {
		err = n.db.Platform.QueryRow(ctx, `SELECT native_uid FROM accounts WHERE id=$1`, request.AccountID).Scan(&self)
	}
	id := request.InvitationID
	if id == "" {
		id = "synthetic-pending-invitation"
	}
	return control.GroupBindingInvitationEvidence{GroupBindingConnectionEvidence: proof, InvitationID: id,
		NativeGroupID: request.NativeGroupID, InviterID: request.NativeSenderID, RecipientID: self,
		ExpiresAt: time.Now().Add(10 * time.Minute)}, err
}

func (n *onboardingNativeFixture) AcceptBindingInvitation(ctx context.Context, request control.GroupBindingAcceptRequest, fence control.GroupBindingAcceptFence) error {
	if n.beforeSubmit != nil {
		if err := n.beforeSubmit(ctx, request); err != nil {
			return err
		}
	}
	evidence, err := n.BindingInvitation(ctx, control.GroupBindingInvitationRequest{
		GroupBindingNativeScope: request.GroupBindingNativeScope, InvitationID: request.InvitationID})
	if err != nil {
		return err
	}
	evidence.ExpiresAt = request.ExpiresAt
	return fence(ctx, evidence, func(context.Context) error {
		n.sends.Add(1)
		if n.unknown {
			return errors.New("synthetic unconfirmed native result")
		}
		return nil
	})
}

func (f *onboardingFixture) decode(platform, account, self, key string, event int64, projection, source []byte) (control.OnboardingMessage, error) {
	var out control.OnboardingMessage
	plain, err := f.vault.Open("onboarding-fixture-projection", projection)
	if err != nil {
		return out, err
	}
	defer clear(plain)
	original, err := f.vault.Open("onboarding-fixture-source", source)
	if err != nil {
		return out, err
	}
	defer clear(original)
	var route struct{ Account, Self, Key string }
	if json.Unmarshal(original, &route) != nil || route.Account != account || route.Self != self || route.Key != key {
		return out, errors.New("synthetic authenticated route changed")
	}
	err = json.Unmarshal(plain, &out)
	return out, err
}

func (f *onboardingFixture) ingress(t *testing.T, platform string, message control.OnboardingMessage, tx pgx.Tx) {
	t.Helper()
	account, key := f.accounts[platform], control.NewID()
	message.ClientID = key
	self := "synthetic-bot-" + platform
	message.Recipient = self
	if !message.Private {
		message.Recipient = message.Group
	}
	raw, _ := json.Marshal(message)
	projection, err := f.vault.Seal("onboarding-fixture-projection", raw)
	if err != nil {
		t.Fatal(err)
	}
	route, _ := json.Marshal(struct{ Account, Self, Key string }{account, self, key})
	source, err := f.vault.Seal("onboarding-fixture-source", route)
	if err != nil {
		t.Fatal(err)
	}
	scene := "group"
	if message.Private {
		scene = "private"
	}
	ctx := context.Background()
	owned := tx == nil
	if owned {
		tx, err = f.db.Platform.Begin(ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback(ctx)
	}
	var event int64
	err = tx.QueryRow(ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
		VALUES($1,$2,$3,clock_timestamp(),$4) RETURNING id`, account, key, scene, source).Scan(&event)
	if err == nil {
		_, err = tx.Exec(ctx, `INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
			deployment_version,codec_version,payload_sealed,received_at,decoded_at)
			SELECT id,account_id,'decoded',$2,$3,1,7,$4,received_at,clock_timestamp()
			FROM native_received_events WHERE id=$1`, event, scene, message.Group, projection)
	}
	if err == nil && owned {
		err = tx.Commit(ctx)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func (f *onboardingFixture) createChallenge(t *testing.T, client testClient, platform, key string) control.GroupBindingChallenge {
	t.Helper()
	client.workspace = platform
	raw := onboardingAPI(t, client, "POST", "/group-binding/challenges", key, map[string]any{"platform": platform}, 201)
	var c control.GroupBindingChallenge
	if err := json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func onboardingRef(t *testing.T, c testClient, platform string) control.SessionRef {
	t.Helper()
	u, _ := url.Parse(c.base)
	for _, cookie := range c.client.Jar.Cookies(u) {
		if cookie.Name == "wsl_session" {
			ref, err := control.WebsiteSession(cookie.Value)
			if err != nil {
				t.Fatal(err)
			}
			return ref.InPlatform(platform)
		}
	}
	t.Fatal("synthetic website session missing")
	return control.SessionRef{}
}

func (f *onboardingFixture) prove(t *testing.T, client testClient, platform string, c control.GroupBindingChallenge, sender, group string) control.GroupBindingChallenge {
	t.Helper()
	f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: sender, Text: c.PrivateChallenge}, nil)
	if err := f.app.GroupBinding.ScanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	f.ingress(t, platform, control.OnboardingMessage{Group: group, Sender: sender, Text: c.GroupChallenge}, nil)
	if err := f.app.GroupBinding.ScanOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	result, err := f.app.GroupBinding.Read(context.Background(), onboardingRef(t, client, platform), c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if result.State != "proven" {
		t.Fatalf("expected proven, got %s (%s)", result.State, result.BlockedReason)
	}
	return result
}

func TestGroupBindingPostgresIdentityOwnerAdminAndRestore(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	onboardingAPI(t, f.member, "POST", "/group-binding/challenges", "member-cannot-create",
		map[string]string{"platform": "wsl"}, 403)
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			client := f.alice
			client.workspace = platform
			c := f.createChallenge(t, client, platform, "owner-initial-"+platform)
			beforeProofCalls := f.native.roles.Load()
			if c.GroupChallenge == "" || c.PrivateChallenge == "" ||
				strings.TrimPrefix(c.GroupChallenge, "群绑定 ") == strings.TrimPrefix(c.PrivateChallenge, "群接入 ") {
				t.Fatal("independent one-time challenge secrets missing")
			}
			f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: f.native.owner,
				Text: "群接入 " + strings.TrimPrefix(c.GroupChallenge, "群绑定 ")}, nil)
			f.ingress(t, platform, control.OnboardingMessage{Group: "native-group-" + platform, Sender: f.native.owner,
				Text: "群绑定 " + strings.TrimPrefix(c.PrivateChallenge, "群接入 ")}, nil)
			if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
				t.Fatal(err)
			}
			current, err := f.app.GroupBinding.Read(ctx, onboardingRef(t, client, platform), c.ID)
			if err != nil || current.IdentityProven || current.State != "waiting" || f.native.roles.Load() != beforeProofCalls {
				t.Fatal("cross-purpose secret or unproven identity accepted", err)
			}
			proven := f.prove(t, client, platform, c, f.native.owner, "native-group-"+platform)
			bob := f.bob
			bob.workspace = platform
			onboardingAPI(t, bob, "GET", "/group-binding/challenges/"+c.ID, "", nil, 404)
			calls := f.native.roles.Load()
			read := onboardingAPI(t, client, "GET", "/group-binding/challenges/"+c.ID, "", nil, 200)
			if bytesContainsSecrets(read, c.GroupChallenge, c.PrivateChallenge) || f.native.roles.Load() != calls {
				t.Fatal("progress leaked secret or called native API")
			}
			result := onboardingAPI(t, client, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "owner-confirm-"+platform,
				map[string]any{"version": proven.Version}, 200)
			if field(t, result, "state") != "bound" {
				t.Fatal("native group owner did not self-bind")
			}
			group := proven.Group.ID
			var management, ai, runtime bool
			var epoch, version int64
			err = f.db.Platform.QueryRow(ctx, `SELECT b.management_enabled,b.ai_enabled,r.enabled,b.epoch,b.version
				FROM group_bindings b JOIN group_business_runtime r ON r.group_id=b.group_id WHERE b.group_id=$1`, group).
				Scan(&management, &ai, &runtime, &epoch, &version)
			if err != nil || management || ai || runtime {
				t.Fatal("new binding activated a runtime", err)
			}
			scope, err := f.app.GroupBinding.OwnershipScope(ctx, platform, group)
			if err != nil {
				t.Fatal(err)
			}
			roles, _ := f.native.BindingRoles(ctx, scope)
			roles.NativeOwnerID = "new-native-owner"
			if err = f.app.GroupBinding.ObserveGroupOwnership(ctx, scope, roles); err == nil {
				t.Fatal("owner transfer did not stop admission")
			}
			var paused bool
			var after int64
			if err = f.db.Platform.QueryRow(ctx, `SELECT o.state='paused',b.epoch FROM group_binding_ownership o
				JOIN group_bindings b ON b.group_id=o.group_id WHERE o.group_id=$1`, group).Scan(&paused, &after); err != nil || !paused || after <= epoch {
				t.Fatal("ownership anchor did not pause and advance epoch", err)
			}
			// Repeated observation cannot repeatedly bump the binding epoch.
			_ = f.app.GroupBinding.ObserveGroupOwnership(ctx, scope, roles)
			var repeated int64
			if err = f.db.Platform.QueryRow(ctx, `SELECT epoch,version FROM group_bindings WHERE group_id=$1`, group).Scan(&repeated, &version); err != nil || repeated != after {
				t.Fatal("paused ownership repeatedly advanced epoch", err)
			}
			if _, err = f.db.Platform.Exec(ctx, `UPDATE group_bindings SET active=false,management_enabled=false,ai_enabled=false WHERE group_id=$1`, group); err != nil {
				t.Fatal(err)
			}
			raw := onboardingAPI(t, client, "POST", "/group-binding/challenges", "restore-challenge-"+platform,
				map[string]any{"platform": platform, "restore_group_id": group, "binding_version": version}, 201)
			var restore control.GroupBindingChallenge
			_ = json.Unmarshal(raw, &restore)
			restore = f.prove(t, client, platform, restore, f.native.owner, "native-group-"+platform)
			onboardingAPI(t, client, "POST", "/group-binding/challenges/"+restore.ID+"/confirm", "restore-confirm-"+platform,
				map[string]any{"version": restore.Version}, 200)
			adminProof := f.createChallenge(t, client, platform, "native-admin-challenge-"+platform)
			adminProof = f.prove(t, client, platform, adminProof, "synthetic-native-admin", "admin-group-"+platform)
			review := onboardingAPI(t, client, "POST", "/group-binding/challenges/"+adminProof.ID+"/confirm", "native-admin-confirm-"+platform,
				map[string]any{"version": adminProof.Version}, 200)
			if field(t, review, "state") != "review" {
				t.Fatal("native administrator bypassed superadmin review")
			}
			var pending control.GroupBindingChallenge
			_ = json.Unmarshal(review, &pending)
			admin := f.admin
			admin.workspace = platform
			onboardingAPI(t, admin, "POST", "/group-binding/challenges/"+pending.ID+"/review", "",
				map[string]any{"version": pending.Version, "approve": true}, 200)
		})
	}
}

func bytesContainsSecrets(raw []byte, secrets ...string) bool {
	for _, secret := range secrets {
		if secret != "" && strings.Contains(string(raw), secret) {
			return true
		}
	}
	return false
}

func TestGroupBindingPostgresLateCommitAndUnknownAccept(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	c := f.createChallenge(t, f.alice, "wsl", "late-event-challenge")
	late, err := f.db.Platform.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer late.Rollback(ctx)
	f.ingress(t, "wsl", control.OnboardingMessage{Private: true, Sender: f.native.owner, Text: c.PrivateChallenge}, late)
	f.ingress(t, "wsl", control.OnboardingMessage{Private: true, Sender: f.native.owner, Text: "ordinary-text"}, nil)
	if err = f.app.GroupBinding.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	if err = late.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err = f.app.GroupBinding.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	ref := onboardingRef(t, f.alice, "wsl")
	current, err := f.app.GroupBinding.Read(ctx, ref, c.ID)
	if err != nil || !current.IdentityProven {
		t.Fatal("late lower-ID identity proof lost", err)
	}
	// Closed production admission must not borrow a test window.
	_, err = f.app.GroupBinding.ConfirmAccept(ctx, ref, c.ID, control.GroupBindingAcceptInput{
		NativeGroupID: "pending-native-group", Version: current.Version, Key: "closed-accept-attempt"})
	if err == nil || f.native.sends.Load() != 0 {
		t.Fatal("closed production admission submitted an invitation")
	}
	if _, err = f.db.Platform.Exec(ctx, `INSERT INTO business_capability_gates(platform,action,source,state,evidence_refs)
		VALUES('wsl','group_accept','onboarding','open',ARRAY['synthetic-onboarding-regression']) ON CONFLICT(platform,action,source)
		DO UPDATE SET state='open',version=business_capability_gates.version+1`); err != nil {
		t.Fatal(err)
	}
	f.native.unknown = true
	input := control.GroupBindingAcceptInput{NativeGroupID: "pending-native-group", Version: current.Version, Key: "one-unknown-native-accept"}
	out, err := f.app.GroupBinding.ConfirmAccept(ctx, ref, c.ID, input)
	if err != nil || out.Status != "unknown" || f.native.sends.Load() != 1 {
		t.Fatalf("unknown submission not retained: %s %v", out.Status, err)
	}
	replay, err := f.app.GroupBinding.ConfirmAccept(ctx, ref, c.ID, input)
	if err != nil || replay.ID != out.ID || f.native.sends.Load() != 1 {
		t.Fatal("unknown accept was replayed", err)
	}
	if err = f.app.GroupBinding.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	resolved, err := f.app.GroupBinding.ReconcileAccept(ctx, ref, c.ID, out.ID)
	if err != nil || resolved.Status != "succeeded" || resolved.Code != "membership_observed" || f.native.sends.Load() != 1 {
		t.Fatal("read-only recovery did not resolve original attempt", err)
	}
	var anchors int
	if err = f.db.Platform.QueryRow(ctx, `SELECT count(*) FROM group_binding_ownership`).Scan(&anchors); err != nil || anchors != 0 {
		t.Fatal("acceptance fabricated native group ownership", err)
	}
}

func TestGroupBindingPostgresCapacityReservations(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	if _, err := f.db.Platform.Exec(ctx, `UPDATE plans SET group_limit=300 WHERE id=$1`, f.plan); err != nil {
		t.Fatal(err)
	}
	f.createChallenge(t, f.alice, "wsl", "capacity-reserved-challenge")
	accounts := []string{f.accounts["wsl"], f.accounts["lajiaoliao"]}
	for len(accounts) < 10 {
		var id string
		if err := f.db.Platform.QueryRow(ctx, `INSERT INTO accounts(name,platform,workspace,purpose)
			VALUES($1,'wsl','wsl','management') RETURNING id::text`, fmt.Sprintf("capacity-%d", len(accounts))).Scan(&id); err != nil {
			t.Fatal(err)
		}
		accounts = append(accounts, id)
	}
	bind := func(account, platform, label string) error {
		tx, err := f.db.Platform.Begin(ctx)
		if err != nil {
			return err
		}
		defer tx.Rollback(ctx)
		var group string
		err = tx.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name) VALUES($1,$2,$2) RETURNING id::text`, platform, label).Scan(&group)
		if err == nil {
			_, err = tx.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id) VALUES($1,$2,$3)`, group, f.tenantB, account)
		}
		if err != nil {
			return err
		}
		return tx.Commit(ctx)
	}
	for index, account := range accounts {
		platform := "wsl"
		if account == f.accounts["lajiaoliao"] {
			platform = "lajiaoliao"
		}
		limit := 30
		if index == 0 {
			limit = 29
		}
		for group := 0; group < limit; group++ {
			if err := bind(account, platform, fmt.Sprintf("capacity-group-%d-%d", index, group)); err != nil {
				t.Fatal(err)
			}
		}
		if index == 0 && bind(account, platform, "cannot-steal-account-reservation") == nil {
			t.Fatal("reserved thirtieth account slot consumed by another binding")
		}
	}
	if err := bind(accounts[1], "lajiaoliao", "cannot-steal-global-reservation"); err == nil {
		t.Fatal("reserved three-hundredth global slot consumed")
	}
}

func TestGroupBindingPostgresAbsenceRequiresAuthenticatedDirectory(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	c := f.createChallenge(t, f.alice, "wsl", "absence-proof-challenge")
	c = f.prove(t, f.alice, "wsl", c, f.native.owner, "absence-native-group")
	onboardingAPI(t, f.alice, "POST", "/group-binding/challenges/"+c.ID+"/confirm", "absence-bind-confirm",
		map[string]any{"version": c.Version}, 200)
	scope, err := f.app.GroupBinding.OwnershipScope(ctx, "wsl", c.Group.ID)
	if err != nil {
		t.Fatal(err)
	}
	roles, err := f.native.BindingRoles(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	stale := roles
	stale.ObservedAt = time.Now().Add(-time.Minute)
	if err = f.app.GroupBinding.ObserveGroupOwnership(ctx, scope, stale); err == nil {
		t.Fatal("expired native role evidence was accepted")
	}
	roles.Present, roles.NativeOwnerID = false, ""
	if err = f.app.GroupBinding.ObserveGroupOwnership(ctx, scope, roles); err == nil {
		t.Fatal("an unsuccessful roster read fabricated departure evidence")
	}
	var state string
	if err = f.db.Platform.QueryRow(ctx, `SELECT state FROM group_binding_ownership WHERE group_id=$1`, c.Group.ID).Scan(&state); err != nil || state != "active" {
		t.Fatal("unproven departure paused the binding", err)
	}
	roles.AbsenceVerified = true
	wrong := roles
	wrong.ReceiverOwner = "wrong-connection-owner"
	if err = f.app.GroupBinding.ObserveGroupOwnership(ctx, scope, wrong); err == nil {
		t.Fatal("departure accepted from an unauthenticated receiver")
	}
	if err = f.app.GroupBinding.ObserveGroupOwnership(ctx, scope, roles); err == nil {
		t.Fatal("verified departure did not stop admission")
	}
	if err = f.db.Platform.QueryRow(ctx, `SELECT state FROM group_binding_ownership WHERE group_id=$1`, c.Group.ID).Scan(&state); err != nil || state != "paused" {
		t.Fatal("verified departure with an empty owner did not pause", err)
	}
	roles.Present, roles.NativeOwnerID = true, ""
	if err = f.app.GroupBinding.ObserveGroupOwnership(ctx, scope, roles); err == nil {
		t.Fatal("present roster accepted an empty owner")
	}
}

func TestGroupBindingPostgresAcceptFinalFenceRevocation(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	c := f.createChallenge(t, f.alice, "lajiaoliao", "accept-revocation-challenge")
	f.ingress(t, "lajiaoliao", control.OnboardingMessage{Private: true, Sender: f.native.owner, Text: c.PrivateChallenge}, nil)
	if err := f.app.GroupBinding.ScanOnce(ctx); err != nil {
		t.Fatal(err)
	}
	ref := onboardingRef(t, f.alice, "lajiaoliao")
	current, err := f.app.GroupBinding.Read(ctx, ref, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Platform.Exec(ctx, `INSERT INTO business_capability_gates(platform,action,source,state,evidence_refs)
		VALUES('lajiaoliao','group_accept','onboarding','open',ARRAY['synthetic-revocation'])`); err != nil {
		t.Fatal(err)
	}
	f.native.beforeSubmit = func(ctx context.Context, _ control.GroupBindingAcceptRequest) error {
		_, err := f.db.Platform.Exec(ctx, `UPDATE users SET access_version=access_version+1 WHERE id=$1`, f.aliceID)
		return err
	}
	out, err := f.app.GroupBinding.ConfirmAccept(ctx, ref, c.ID, control.GroupBindingAcceptInput{
		NativeGroupID: "revoked-native-group", Version: current.Version, Key: "revoked-native-accept"})
	if err != nil || out.Status != "failed" || f.native.sends.Load() != 0 {
		t.Fatal("final website-session revocation did not block native acceptance", out, err)
	}
	var attempts int
	if err = f.db.Platform.QueryRow(ctx, `SELECT count(*) FROM customer_group_accept_attempts WHERE challenge_id=$1`, c.ID).Scan(&attempts); err != nil || attempts != 1 {
		t.Fatal("the blocked original attempt was not retained", err)
	}
}

func TestGroupBindingPostgresTransferNeedsExplicitReview(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	first := f.createChallenge(t, f.alice, "wsl", "transfer-source-challenge")
	first = f.prove(t, f.alice, "wsl", first, f.native.owner, "transfer-native-group")
	onboardingAPI(t, f.alice, "POST", "/group-binding/challenges/"+first.ID+"/confirm", "transfer-source-confirm",
		map[string]any{"version": first.Version}, 200)
	group := first.Group.ID
	if _, err := f.db.Platform.Exec(ctx, `UPDATE group_bindings SET config='{"blocked_words":["old-tenant-only"]}'
		WHERE group_id=$1`, group); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Platform.Exec(ctx, `INSERT INTO ledger_entries(tenant_id,group_id,member_id,kind,
		amount_cents,external_key,evidence,actor_id) VALUES($1,$2,'synthetic-member','adjustment',100,'historical-entry','{}',$3)`,
		f.tenantA, group, f.aliceID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Platform.Exec(ctx, `INSERT INTO activities(tenant_id,group_id,name,closes_at,winners)
		VALUES($1,$2,'Historical activity',now()+interval '1 day',1)`, f.tenantA, group); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Platform.Exec(ctx, `INSERT INTO member_groups(user_id,tenant_id,group_id) VALUES($1,$2,$3)`,
		f.aliceID, f.tenantA, group); err != nil {
		t.Fatal(err)
	}
	next := f.createChallenge(t, f.bob, "wsl", "transfer-target-challenge")
	next = f.prove(t, f.bob, "wsl", next, f.native.owner, "transfer-native-group")
	if !next.TransferRequired || next.Transfer != nil {
		t.Fatal("customer must see review requirement without source tenant disclosure")
	}
	result := onboardingAPI(t, f.bob, "POST", "/group-binding/challenges/"+next.ID+"/confirm", "transfer-target-confirm",
		map[string]any{"version": next.Version}, 200)
	var review control.GroupBindingChallenge
	if err := json.Unmarshal(result, &review); err != nil || review.State != "review" {
		t.Fatal("an owner in another website tenant bypassed explicit review", err)
	}
	adminRead := onboardingAPI(t, f.admin, "GET", "/group-binding/challenges/"+next.ID, "", nil, 200)
	if err := json.Unmarshal(adminRead, &review); err != nil || review.Transfer == nil ||
		review.Transfer.SourceTenantID != f.tenantA {
		t.Fatal("superadmin transfer source/version missing", err)
	}
	body := map[string]any{"version": review.Version, "approve": true, "source_tenant_id": f.tenantA,
		"binding_version": review.Transfer.BindingVersion}
	onboardingAPI(t, f.bob, "POST", "/group-binding/challenges/"+next.ID+"/transfer-review", "not-admin-transfer", body, 403)
	onboardingAPI(t, f.admin, "POST", "/group-binding/challenges/"+next.ID+"/review", "",
		map[string]any{"version": review.Version, "approve": true}, 409)
	body["source_tenant_id"] = f.tenantB
	onboardingAPI(t, f.admin, "POST", "/group-binding/challenges/"+next.ID+"/transfer-review", "wrong-source-transfer", body, 409)
	body["source_tenant_id"] = f.tenantA
	body["version"] = review.Version + 1
	onboardingAPI(t, f.admin, "POST", "/group-binding/challenges/"+next.ID+"/transfer-review", "wrong-review-version", body, 409)
	body["version"] = review.Version
	body["binding_version"] = review.Transfer.BindingVersion + 1
	onboardingAPI(t, f.admin, "POST", "/group-binding/challenges/"+next.ID+"/transfer-review", "wrong-binding-version", body, 409)
	body["binding_version"] = review.Transfer.BindingVersion
	if _, err := f.db.Platform.Exec(ctx, `UPDATE subscriptions SET expires_at=now()-interval '1 second' WHERE tenant_id=$1`, f.tenantB); err != nil {
		t.Fatal(err)
	}
	onboardingAPI(t, f.admin, "POST", "/group-binding/challenges/"+next.ID+"/transfer-review", "expired-plan-transfer", body, 409)
	var stillSource string
	if err := f.db.Platform.QueryRow(ctx, `SELECT tenant_id::text FROM group_bindings WHERE group_id=$1`, group).Scan(&stillSource); err != nil || stillSource != f.tenantA {
		t.Fatal("failed transfer changed the original tenant", err)
	}
	if _, err := f.db.Platform.Exec(ctx, `UPDATE subscriptions SET expires_at=now()+interval '1 day' WHERE tenant_id=$1`, f.tenantB); err != nil {
		t.Fatal(err)
	}
	var oldEpoch int64
	if err := f.db.Platform.QueryRow(ctx, `SELECT epoch FROM group_bindings WHERE group_id=$1`, group).Scan(&oldEpoch); err != nil {
		t.Fatal(err)
	}
	unknown := control.NewID()
	if _, err := f.db.Platform.Exec(ctx, `INSERT INTO operations(id,tenant_id,actor_id,group_id,account_id,
		idempotency_key,request_hash,kind,account_epoch,binding_epoch,status,expires_at)
		SELECT $1,$2,$3,$4,$5,'transfer-unknown-operation',$6,'send_text',a.epoch,$7,'unknown',
			clock_timestamp()+interval '1 minute' FROM accounts a WHERE a.id=$5`,
		unknown, f.tenantA, f.aliceID, group, first.Robot.ID, security.Digest("synthetic-unknown-operation"), oldEpoch); err != nil {
		t.Fatal(err)
	}
	onboardingAPI(t, f.admin, "POST", "/group-binding/challenges/"+next.ID+"/transfer-review", "unknown-blocks-transfer", body, 409)
	var notTransferred bool
	if err := f.db.Platform.QueryRow(ctx, `SELECT b.tenant_id=$2 AND b.epoch=$3 AND o.tenant_id=$2
		AND NOT EXISTS(SELECT 1 FROM customer_group_transfers tr WHERE tr.group_id=b.group_id)
		FROM group_bindings b JOIN group_binding_ownership o ON o.group_id=b.group_id WHERE b.group_id=$1`,
		group, f.tenantA, oldEpoch).Scan(&notTransferred); err != nil || !notTransferred {
		t.Fatal("unknown operation was bypassed or partially transferred", err)
	}
	// The synthetic original operation has now been reconciled. Its source
	// tenant and historical row must survive the subsequent transfer.
	if _, err := f.db.Platform.Exec(ctx, `UPDATE operations SET status='succeeded',result_code='synthetic_reconciled'
		WHERE id=$1 AND status='unknown'`, unknown); err != nil {
		t.Fatal(err)
	}
	approved := onboardingAPI(t, f.admin, "POST", "/group-binding/challenges/"+next.ID+"/transfer-review", "explicit-owner-transfer", body, 200)
	if field(t, approved, "state") != "bound" {
		t.Fatal("explicit transfer did not bind the new tenant")
	}
	var firstEpoch, firstAnchorVersion int64
	if err := f.db.Platform.QueryRow(ctx, `SELECT b.epoch,o.version FROM group_bindings b
		JOIN group_binding_ownership o ON o.group_id=b.group_id WHERE b.group_id=$1`, group).
		Scan(&firstEpoch, &firstAnchorVersion); err != nil {
		t.Fatal(err)
	}
	calls := f.native.roles.Load()
	onboardingAPI(t, f.admin, "POST", "/group-binding/challenges/"+next.ID+"/transfer-review", "explicit-owner-transfer", body, 200)
	if f.native.roles.Load() != calls {
		t.Fatal("idempotent transfer approval repeated a native role read")
	}
	var tenant, config string
	var disabled, anchored, boundary bool
	var epoch, anchorVersion int64
	err := f.db.Platform.QueryRow(ctx, `SELECT b.tenant_id::text,b.config::text,
		NOT b.management_enabled AND NOT b.ai_enabled AND NOT b.ai_requested AND NOT r.enabled,
		b.epoch,o.tenant_id=b.tenant_id AND o.state='active',b.data_since>now()-interval '1 minute',o.version
		FROM group_bindings b JOIN group_business_runtime r ON r.group_id=b.group_id
		JOIN group_binding_ownership o ON o.group_id=b.group_id WHERE b.group_id=$1`, group).
		Scan(&tenant, &config, &disabled, &epoch, &anchored, &boundary, &anchorVersion)
	if err != nil || tenant != f.tenantB || config != "{}" || !disabled || epoch <= oldEpoch || epoch != firstEpoch ||
		anchorVersion != firstAnchorVersion || !anchored || !boundary {
		t.Fatal("transfer did not reset scope/runtime/ownership", err)
	}
	scope, err := f.app.GroupBinding.OwnershipScope(ctx, "wsl", group)
	if err != nil || scope.NativeSenderID != f.native.owner || scope.AccountID != first.Robot.ID {
		t.Fatal("transferred ownership no longer resolves the current proved applicant", err)
	}
	proof, err := f.native.BindingRoles(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.app.GroupBinding.ObserveGroupOwnership(ctx, scope, proof); err != nil {
		t.Fatal("the transferred current-tenant anchor rejected unchanged native ownership", err)
	}
	var transfers, sourceLedger, sourceActivities, sourceOperations, sessions, assignments int
	err = f.db.Platform.QueryRow(ctx, `SELECT (SELECT count(*) FROM customer_group_transfers WHERE group_id=$1),
		(SELECT count(*) FROM ledger_entries WHERE group_id=$1 AND tenant_id=$2),
		(SELECT count(*) FROM activities WHERE group_id=$1 AND tenant_id=$2),
		(SELECT count(*) FROM operations WHERE group_id=$1 AND tenant_id=$2 AND status='succeeded'),
		(SELECT count(*) FROM sessions WHERE user_id IN ($3::uuid,$4::uuid)),
		(SELECT count(*) FROM member_groups WHERE group_id=$1)`, group, f.tenantA, f.aliceID, f.bobID).
		Scan(&transfers, &sourceLedger, &sourceActivities, &sourceOperations, &sessions, &assignments)
	if err != nil || transfers != 1 || sourceLedger != 1 || sourceActivities != 1 || sourceOperations != 1 || sessions != 0 || assignments != 0 {
		t.Fatal("transfer replay or cross-tenant history/session leak", err)
	}
	var visible, activities, operations int
	err = f.db.ActorTenantTx(ctx, f.tenantB, f.bobID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT (SELECT count(*) FROM ledger_entries WHERE group_id=$1),
			(SELECT count(*) FROM activities WHERE group_id=$1),(SELECT count(*) FROM operations WHERE group_id=$1)`,
			group).Scan(&visible, &activities, &operations)
	})
	if err != nil || visible != 0 || activities != 0 || operations != 0 {
		t.Fatal("new tenant gained the previous tenant's financial/activity/operation history", err)
	}
	err = f.db.ActorTenantTx(ctx, f.tenantA, f.aliceID, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, `SELECT count(*) FROM group_bindings WHERE group_id=$1`, group).Scan(&visible)
	})
	if err != nil || visible != 0 {
		t.Fatal("former tenant retained current group access", err)
	}
}

func TestGroupBindingPostgresHistoryCleanupPreservesFinance(t *testing.T) {
	f := newOnboardingFixture(t)
	ctx := context.Background()
	create := func(label string) string {
		t.Helper()
		var group string
		if err := f.db.Platform.QueryRow(ctx, `INSERT INTO native_groups(platform,native_id,name)
			VALUES('wsl',$1,$1) RETURNING id::text`, label).Scan(&group); err != nil {
			t.Fatal(err)
		}
		if _, err := f.db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id)
			VALUES($1,$2,$3)`, group, f.tenantA, f.accounts["wsl"]); err != nil {
			t.Fatal(err)
		}
		return group
	}
	empty := create("empty-fixture-history")
	if _, err := f.db.Platform.Exec(ctx, `DELETE FROM group_bindings WHERE group_id=$1`, empty); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Platform.Exec(ctx, `DELETE FROM native_groups WHERE id=$1`, empty); err != nil {
		t.Fatal("empty fixture group could not clean up its derived history/runtime", err)
	}
	var derived int
	if err := f.db.Platform.QueryRow(ctx, `SELECT (SELECT count(*) FROM group_binding_tenant_history WHERE group_id=$1)
		+(SELECT count(*) FROM group_business_runtime WHERE group_id=$1)`, empty).Scan(&derived); err != nil || derived != 0 {
		t.Fatal("empty fixture left derived group rows", err)
	}
	protected := create("protected-financial-history")
	if _, err := f.db.Platform.Exec(ctx, `INSERT INTO ledger_entries(tenant_id,group_id,member_id,kind,amount_cents,
		external_key,evidence,actor_id) VALUES($1,$2,'synthetic-member','adjustment',100,'cleanup-ledger','{}',$3)`,
		f.tenantA, protected, f.aliceID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Platform.Exec(ctx, `DELETE FROM group_bindings WHERE group_id=$1`, protected); err != nil {
		t.Fatal(err)
	}
	if _, err := f.db.Platform.Exec(ctx, `DELETE FROM native_groups WHERE id=$1`, protected); err == nil {
		t.Fatal("native group deletion cascaded through financial history")
	}
	var retained int
	var deletePermission bool
	if err := f.db.Platform.QueryRow(ctx, `SELECT (SELECT count(*) FROM ledger_entries WHERE group_id=$1),
		has_table_privilege(current_user,'ledger_entries','DELETE')`, protected).Scan(&retained, &deletePermission); err != nil || retained != 1 || deletePermission {
		t.Fatal("financial history or platform delete privileges changed", err)
	}
}
