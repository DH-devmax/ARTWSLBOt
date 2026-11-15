package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wangshangliao/internal/control"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/security"
)

type bindingOutboundNativeFixture struct {
	*bindingFriendNativeFixture
	outboundLookups, outboundSends, outboundEffects, outboundReconciles atomic.Int32
	target                                                              control.GroupBindingFriendTarget
	lookupState, outboundReconcileState                                 string
	beforeOutboundLookup                                                func(context.Context, control.GroupBindingOutboundLookup) error
	afterOutboundLookup                                                 func(context.Context, control.GroupBindingOutboundLookup) error
	beforeOutboundSend                                                  func(context.Context, control.GroupBindingOutboundRequest) error
	beforeOutboundReconcile                                             func(context.Context, control.GroupBindingOutboundRequest) error
	onOutboundEffect                                                    func(context.Context, control.GroupBindingOutboundRequest) error
	postFenceFailure                                                    error
	outboundEvidence                                                    func(*control.GroupBindingOutboundEvidence)
	outboundUnknown, outboundNoFence, outboundDoubleFence               bool
	outboundMu                                                          sync.Mutex
	lastOutbound                                                        control.GroupBindingOutboundRequest
}

var _ control.GroupBindingOutboundNativePort = (*bindingOutboundNativeFixture)(nil)

func (n *bindingOutboundNativeFixture) outgoingEvidence(ctx context.Context, scope control.GroupBindingNativeScope) (control.GroupBindingOutboundEvidence, error) {
	connection, err := n.connection(ctx, scope)
	var self string
	if err == nil {
		err = n.db.Platform.QueryRow(ctx, `SELECT native_uid FROM accounts WHERE id=$1`, scope.AccountID).Scan(&self)
	}
	return control.GroupBindingOutboundEvidence{GroupBindingConnectionEvidence: connection,
		Target: n.target, SelfID: self, State: n.lookupState}, err
}

func (n *bindingOutboundNativeFixture) LookupBindingFriend(ctx context.Context, in control.GroupBindingOutboundLookup,
	fence control.GroupBindingOutboundLookupFence) (control.GroupBindingOutboundEvidence, error) {
	var evidence control.GroupBindingOutboundEvidence
	if fence == nil {
		return evidence, errors.New("synthetic outbound lookup fence missing")
	}
	var lookupFailure error
	if n.beforeOutboundLookup != nil {
		lookupFailure = n.beforeOutboundLookup(ctx, in)
	}
	err := fence(ctx, func(work context.Context) error {
		if err := work.Err(); err != nil {
			return err
		}
		n.outboundLookups.Add(1)
		var err error
		evidence, err = n.outgoingEvidence(work, in.GroupBindingNativeScope)
		if err == nil && in.ControlledPeer != nil {
			peer := *in.ControlledPeer
			peer.Connection, err = n.connection(work, peer.GroupBindingNativeScope)
			evidence.Peer = &peer
		}
		if n.outboundEvidence != nil {
			n.outboundEvidence(&evidence)
		}
		if err != nil {
			return err
		}
		// Injected upstream failures count only after the query is authorized.
		return lookupFailure
	})
	// Post-response scope changes run after the fence releases its row locks.
	if err == nil && n.afterOutboundLookup != nil {
		err = n.afterOutboundLookup(ctx, in)
	}
	return evidence, err
}

func (n *bindingOutboundNativeFixture) SendBindingFriend(ctx context.Context, in control.GroupBindingOutboundRequest, fence control.GroupBindingOutboundFence) error {
	n.outboundSends.Add(1)
	n.outboundMu.Lock()
	n.lastOutbound = in
	n.outboundMu.Unlock()
	if n.beforeOutboundSend != nil {
		if err := n.beforeOutboundSend(ctx, in); err != nil {
			return err
		}
	}
	if n.outboundNoFence {
		return nil
	}
	evidence, err := n.outgoingEvidence(ctx, in.GroupBindingNativeScope)
	if err != nil {
		return err
	}
	if in.ControlledPeer != nil {
		peer := *in.ControlledPeer
		peer.Connection, err = n.connection(ctx, peer.GroupBindingNativeScope)
		if err != nil {
			return err
		}
		evidence.Peer = &peer
	}
	if n.outboundEvidence != nil {
		n.outboundEvidence(&evidence)
	}
	effect := func(work context.Context) error {
		if err := work.Err(); err != nil {
			return err
		}
		n.outboundEffects.Add(1)
		if n.onOutboundEffect != nil {
			if err := n.onOutboundEffect(work, in); err != nil {
				return err
			}
		}
		if n.outboundUnknown {
			return errors.New("synthetic outbound submission outcome unknown")
		}
		return nil
	}
	if err = fence(ctx, evidence, effect); err != nil {
		return err
	}
	if n.outboundDoubleFence {
		return fence(ctx, evidence, effect)
	}
	return n.postFenceFailure
}

func (n *bindingOutboundNativeFixture) ReconcileBindingOutbound(ctx context.Context, in control.GroupBindingOutboundRequest) (control.GroupBindingOutboundEvidence, error) {
	n.outboundReconciles.Add(1)
	n.outboundMu.Lock()
	original := n.lastOutbound
	n.outboundMu.Unlock()
	compared := in
	compared.ExpiresAt = original.ExpiresAt
	compared.ControlledPeer = original.ControlledPeer
	if compared != original || !reflect.DeepEqual(in.ControlledPeer, original.ControlledPeer) ||
		!in.ExpiresAt.Equal(original.ExpiresAt) {
		return control.GroupBindingOutboundEvidence{}, errors.New("reconciliation changed original outbound request")
	}
	if n.beforeOutboundReconcile != nil {
		if err := n.beforeOutboundReconcile(ctx, in); err != nil {
			return control.GroupBindingOutboundEvidence{}, err
		}
	}
	evidence, err := n.outgoingEvidence(ctx, in.GroupBindingNativeScope)
	evidence.State, evidence.ApplicationID = n.outboundReconcileState, "synthetic-outgoing-application"
	evidence.Target.AlreadyFriend = evidence.State == "friends"
	if n.outboundEvidence != nil {
		n.outboundEvidence(&evidence)
	}
	return evidence, err
}

func outboundFixture(t *testing.T, platform string) (*onboardingFixture, *bindingOutboundNativeFixture, testClient, control.SessionRef) {
	t.Helper()
	f, old, _, _ := friendFixture(t, platform)
	n := &bindingOutboundNativeFixture{bindingFriendNativeFixture: old,
		target: control.GroupBindingFriendTarget{NativeID: old.owner, AddAccount: "synthetic-customer-" + platform,
			Name: "Synthetic customer"}, lookupState: "available", outboundReconcileState: "friends"}
	f.app.GroupBinding.Native = n
	var other string
	if err := f.db.Platform.QueryRow(context.Background(), `INSERT INTO users(username,password_hash,role)
		SELECT 'synthetic-outbound-other-admin',password_hash,'superadmin' FROM users WHERE id=$1
		RETURNING id::text`, f.adminID).Scan(&other); err != nil {
		t.Fatal(err)
	}
	f.bob = f.client(t, f.admin.base, other)
	client := f.admin
	client.workspace = platform
	return f, n, client, onboardingRef(t, client, platform)
}

// Historical query/reconcile fixtures use immutable legacy scope at INSERT.
func outboundLegacyChallenge(t *testing.T, f *onboardingFixture, client testClient, platform, key string) control.GroupBindingChallenge {
	t.Helper()
	ctx := context.Background()
	ref := onboardingRef(t, client, platform)
	view, err := f.app.GroupBinding.Context(ctx, ref, platform, "", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	tenant := view.Scope.TenantID
	if view.Mode == "operator_demo" {
		tenant = f.tenantA
	}
	token, err := security.Token()
	if err != nil {
		t.Fatal(err)
	}
	private, err := security.Token()
	if err != nil {
		t.Fatal(err)
	}
	id := control.NewID()
	_, err = f.db.Platform.Exec(ctx, `INSERT INTO group_binding_challenges(id,tenant_id,applicant_id,platform,
		account_id,account_epoch,account_version,applicant_version,token_hash,private_token_hash,
		create_key,request_hash,applicant_mode,friend_flow)
		SELECT $1,$2,u.id,$3,a.id,a.epoch,a.version,u.access_version,$6,$7,$8,$9,$10,'outbound'
		FROM accounts a JOIN users u ON u.id=$4 WHERE a.id=$5`, id, tenant, platform, view.Scope.ActorID,
		f.accounts[platform], security.Digest(token), security.Digest(private), key, security.Digest(key), view.Mode)
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.app.GroupBinding.Read(ctx, ref, id)
	if err != nil {
		t.Fatal(err)
	}
	out.GroupChallenge, out.PrivateChallenge = "群绑定 "+token, "群接入 "+private
	return out
}

func (n *bindingOutboundNativeFixture) BindingRoles(ctx context.Context, in control.GroupBindingNativeScope) (control.GroupBindingRoleEvidence, error) {
	out, err := n.bindingFriendNativeFixture.BindingRoles(ctx, in)
	var self string
	if err == nil {
		err = n.db.Platform.QueryRow(ctx, `SELECT native_uid FROM accounts WHERE id=$1`, in.AccountID).Scan(&self)
	}
	if in.NativeSenderID == self {
		out.SenderRole = out.RobotRole
	}
	return out, err
}

func outboundControlledChallenge(t *testing.T, f *onboardingFixture, client testClient, platform, key string, seconds ...int) control.GroupBindingChallenge {
	t.Helper()
	ctx := context.Background()
	if _, err := f.db.Platform.Exec(ctx, `UPDATE accounts SET testing_enabled=true,connection_enabled=true
		WHERE id=$1 AND (NOT testing_enabled OR NOT connection_enabled)`, f.accounts[platform]); err != nil {
		t.Fatal(err)
	}
	client.workspace = platform
	raw := onboardingAPI(t, client, "POST", "/group-binding/challenges", key,
		map[string]any{"platform": platform, "target_tenant_id": f.tenantA}, 201)
	var c control.GroupBindingChallenge
	if err := json.Unmarshal(raw, &c); err != nil || c.FriendFlow != "incoming_contact" || c.Mode != "operator_demo" {
		t.Fatal("execution fixture must use the actual controlled incoming challenge", c, err)
	}
	var group string
	err := f.db.Platform.QueryRow(ctx, `SELECT group_id::text FROM group_bindings WHERE tenant_id=$1
		AND primary_account_id=$2 AND active LIMIT 1`, f.tenantA, c.Robot.ID).Scan(&group)
	if err != nil {
		group = control.NewID()
		if _, err = f.db.Platform.Exec(ctx, `INSERT INTO native_groups(id,platform,native_id,name)
			VALUES($1,$2,$3,'Synthetic isolated outgoing test')`, group, platform, "synthetic-controlled-"+group); err != nil {
			t.Fatal(err)
		}
		if _, err = f.db.Platform.Exec(ctx, `INSERT INTO group_bindings(group_id,tenant_id,primary_account_id)
			VALUES($1,$2,$3)`, group, f.tenantA, c.Robot.ID); err != nil {
			t.Fatal(err)
		}
	}
	n := f.app.GroupBinding.Native.(*bindingOutboundNativeFixture)
	duration := 300
	if len(seconds) != 0 {
		duration = seconds[0]
	}
	onboardingAPI(t, client, "POST", "/test-tools/onboarding-friend/windows", "",
		control.OnboardingControlledWindowInput{ChallengeID: c.ID, Version: c.Version, GroupID: group,
			Account: n.target.AddAccount, TargetID: n.target.NativeID, Seconds: duration, Budget: 1, ConfirmIsolated: true}, 201)
	return c
}

// Tests use the real persisted-window entry points, never an injected context.
type outboundControlledService struct{ *control.GroupBindingService }

func (s outboundControlledService) window(ctx context.Context, id string) (string, error) {
	var window string
	err := s.DB.Platform.QueryRow(ctx, `SELECT id::text FROM test_windows
		WHERE target_ids[1] LIKE 'onboarding_challenge:'||$1::text||':%' ORDER BY created_at DESC LIMIT 1`, id).Scan(&window)
	return window, err
}

func (s outboundControlledService) PrepareOutboundFriend(ctx context.Context, ref control.SessionRef, id string, in control.GroupBindingFriendTargetInput) (control.GroupBindingChallenge, error) {
	window, err := s.window(ctx, id)
	if err != nil {
		return control.GroupBindingChallenge{}, err
	}
	return s.PrepareControlledOutboundFriend(ctx, ref, id, control.OnboardingControlledTargetInput{
		Window: window, Version: in.Version, Account: in.Account, Key: in.Key})
}

func (s outboundControlledService) SendOutboundFriend(ctx context.Context, ref control.SessionRef, id string, in control.GroupBindingConfirmation) (control.GroupBindingChallenge, error) {
	window, err := s.window(ctx, id)
	if err != nil {
		return control.GroupBindingChallenge{}, err
	}
	return s.SendControlledOutboundFriend(ctx, ref, id, control.OnboardingControlledConfirmation{
		Window: window, Version: in.Version, Key: in.Key})
}

func outboundOpenGate(t *testing.T, f *onboardingFixture, platform string) {
	t.Helper()
	admin := f.admin
	admin.workspace = platform
	onboardingAPI(t, admin, "PUT", "/system/group-binding/outbound-friend-gates/"+f.accounts[platform], "",
		control.GroupBindingFriendGateInput{Version: 0, State: "open", Evidence: []string{bindingFriendFixtureGateEvidence}}, 200)
}

func outboundPrepare(t *testing.T, f *onboardingFixture, n *bindingOutboundNativeFixture, ref control.SessionRef, c control.GroupBindingChallenge, key string) control.GroupBindingChallenge {
	t.Helper()
	out, err := (outboundControlledService{f.app.GroupBinding}).PrepareOutboundFriend(context.Background(), ref, c.ID,
		control.GroupBindingFriendTargetInput{Version: c.Version, Account: n.target.AddAccount, Key: key})
	if err != nil || out.FriendOutbound == nil {
		t.Fatal("synthetic target preparation failed", out, err)
	}
	// Keep the create-only codes in the test's local flow state. Production
	// progress reads must not redisclose these secrets.
	out.GroupChallenge, out.PrivateChallenge = c.GroupChallenge, c.PrivateChallenge
	return out
}

func outboundSend(t *testing.T, service *control.GroupBindingService, ref control.SessionRef, c control.GroupBindingChallenge, key string) control.GroupBindingChallenge {
	t.Helper()
	out, err := (outboundControlledService{service}).SendOutboundFriend(context.Background(), ref, c.ID,
		control.GroupBindingConfirmation{Version: c.Version, Key: key})
	if err != nil || out.FriendOutbound == nil {
		t.Fatal("synthetic explicit friend submission failed", out, err)
	}
	return out
}

func outboundAPI(t *testing.T, client testClient, path, key string, body any, status int) json.RawMessage {
	t.Helper()
	headers := http.Header{"Idempotency-Key": {key}}
	got, data, message := client.requestRaw(http.MethodPost, path, body, headers)
	if got != status {
		t.Fatalf("POST %s got %d (%s), want %d", path, got, message, status)
	}
	return data
}

func outboundCalls(n *bindingOutboundNativeFixture) [11]int32 {
	return [11]int32{n.roles.Load(), n.invites.Load(), n.sends.Load(), n.lookups.Load(), n.approvals.Load(),
		n.submits.Load(), n.reconciles.Load(), n.outboundLookups.Load(), n.outboundSends.Load(),
		n.outboundEffects.Load(), n.outboundReconciles.Load()}
}

func TestGroupBindingOutboundRequestLoggingRedactsSelectors(t *testing.T) {
	in := control.GroupBindingOutboundRequest{OperationID: "private-operation",
		Target: control.GroupBindingFriendTarget{NativeID: "private-native-id", AddAccount: "private-search-account", Name: "private-name"}}
	for _, text := range []string{fmt.Sprintf("%v", in), fmt.Sprintf("%+v", in), fmt.Sprintf("%#v", in)} {
		if strings.Contains(text, in.OperationID) || strings.Contains(text, in.Target.NativeID) ||
			strings.Contains(text, in.Target.AddAccount) || strings.Contains(text, in.Target.Name) {
			t.Fatal("outgoing request logging exposed private native selectors")
		}
	}
}

func TestGroupBindingOutboundInvalidInputsAreRejectedBeforeDatabase(t *testing.T) {
	service := &control.GroupBindingService{}
	id := control.NewID()
	for _, in := range []control.GroupBindingFriendTargetInput{
		{Version: 0, Account: "valid-account", Key: "valid-prepare-key"},
		{Version: 1, Account: "valid-account", Key: "short"},
		{Version: 1, Account: "", Key: "valid-prepare-key"},
		{Version: 1, Account: " untrimmed-account", Key: "valid-prepare-key"},
		{Version: 1, Account: "account@example.test", Key: "valid-prepare-key"},
		{Version: 1, Account: strings.Repeat("a", 65), Key: "valid-prepare-key"},
	} {
		_, err := service.PrepareOutboundFriend(context.Background(), control.SessionRef{}, id, in)
		friendFailure(t, err, 400, "group_binding_outbound_invalid")
	}
	for _, in := range []control.GroupBindingConfirmation{
		{Version: 0, Key: "valid-confirm-key"},
		{Version: 1, Key: "short"},
	} {
		_, err := service.SendOutboundFriend(context.Background(), control.SessionRef{}, id, in)
		friendFailure(t, err, 400, "group_binding_outbound_invalid")
		_, err = service.ReconcileOutboundFriend(context.Background(), control.SessionRef{}, id, control.NewID(), in)
		friendFailure(t, err, 400, "group_binding_outbound_invalid")
	}
}

func TestGroupBindingOutboundFixtureReconcilePreservesOriginalRequestAcrossSerialization(t *testing.T) {
	original := control.GroupBindingOutboundRequest{
		GroupBindingNativeScope: control.GroupBindingNativeScope{Platform: "wsl", AccountID: "synthetic-account",
			AccountEpoch: 3, AccountVersion: 4},
		OperationID: "synthetic-operation", SelfID: "synthetic-robot",
		Target:    control.GroupBindingFriendTarget{NativeID: "synthetic-human", AddAccount: "synthetic-search", Name: "Synthetic target"},
		ExpiresAt: time.Date(2026, time.November, 8, 12, 0, 0, 123456000, time.FixedZone("synthetic-zone", 3600)),
	}
	raw, err := json.Marshal(original)
	if err != nil {
		t.Fatal(err)
	}
	var restored control.GroupBindingOutboundRequest
	if err = json.Unmarshal(raw, &restored); err != nil {
		t.Fatal(err)
	}
	if !restored.ExpiresAt.Equal(original.ExpiresAt) {
		t.Fatal("serialization changed the synthetic deadline")
	}
	reached := errors.New("synthetic read-only observation reached")
	for _, change := range []string{"none", "platform", "account", "epoch", "account-version", "operation",
		"target", "add-account", "name", "friendship", "self", "group", "sender", "deadline"} {
		t.Run(change, func(t *testing.T) {
			request := restored
			switch change {
			case "platform":
				request.Platform = "lajiaoliao"
			case "account":
				request.AccountID += "-other"
			case "epoch":
				request.AccountEpoch++
			case "account-version":
				request.AccountVersion++
			case "operation":
				request.OperationID += "-other"
			case "target":
				request.Target.NativeID += "-other"
			case "add-account":
				request.Target.AddAccount += "-other"
			case "name":
				request.Target.Name += " other"
			case "friendship":
				request.Target.AlreadyFriend = true
			case "self":
				request.SelfID += "-other"
			case "group":
				request.NativeGroupID = "synthetic-group"
			case "sender":
				request.NativeSenderID = "synthetic-sender"
			case "deadline":
				request.ExpiresAt = request.ExpiresAt.Add(time.Microsecond)
			}
			calls := 0
			n := &bindingOutboundNativeFixture{lastOutbound: original,
				beforeOutboundReconcile: func(context.Context, control.GroupBindingOutboundRequest) error {
					calls++
					return reached
				}}
			_, err := n.ReconcileBindingOutbound(context.Background(), request)
			if change == "none" {
				if !errors.Is(err, reached) || calls != 1 {
					t.Fatal("equivalent serialized request was rejected before observation", err, calls)
				}
			} else if err == nil || errors.Is(err, reached) || calls != 0 {
				t.Fatal("changed durable selector reached observation", change, err, calls)
			}
		})
	}
}

func TestGroupBindingOutboundPostgresPreparationConsentAndPrivateProof(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, platform)
			c := outboundControlledChallenge(t, f, client, platform, "outbound-consent-"+platform)
			if c.FriendFlow != "incoming_contact" || c.FriendChallenge != "" || c.FriendOutbound != nil || c.CanVerifyFriend {
				t.Fatal("new challenge exposed the retired friend-note path", c)
			}
			friendOpenGate(t, f, platform)
			c = outboundPrepare(t, f, n, ref, c, "outbound-target-prepare")
			if c.FriendOutbound.Status != "prepared" || c.FriendOutbound.CanSend ||
				c.FriendOutbound.SendBlockedReason != "group_binding_outbound_admission_closed" ||
				c.BlockedReason != "" || n.outboundLookups.Load() != 1 || n.outboundSends.Load() != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE id=$1 AND gate_version=0 AND attempts_used=0 AND send_key IS NULL`, c.FriendOutbound.ID) != 1 {
				t.Fatal("closed sending admission blocked read-only search or private proof", c)
			}
			_, err := (outboundControlledService{f.app.GroupBinding}).SendOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-gate-closed"})
			friendFailure(t, err, 409, "group_binding_outbound_admission_closed")
			if n.outboundSends.Load() != 0 || friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
				WHERE id=$1 AND gate_version=0 AND attempts_used=0 AND send_key IS NULL`, c.FriendOutbound.ID) != 1 {
				t.Fatal("closed outgoing gate reached native sending or consumed the attempt")
			}
			if _, err = f.db.Platform.Exec(context.Background(), `UPDATE group_binding_outbound_attempts
				SET gate_version=1 WHERE id=$1`, c.FriendOutbound.ID); err == nil {
				t.Fatal("search-only target could pin a submission admission without confirmation")
			}
			outboundOpenGate(t, f, platform)
			groupCode, privateCode := c.GroupChallenge, c.PrivateChallenge
			c, err = f.app.GroupBinding.Read(context.Background(), ref, c.ID)
			if err != nil {
				t.Fatal(err)
			}
			// Progress reads never disclose the create-only proof text.
			c.GroupChallenge, c.PrivateChallenge = groupCode, privateCode
			if c.FriendOutbound.Status != "prepared" || c.FriendOutbound.CanSend || c.IdentityProven ||
				c.FriendOutbound.SendBlockedReason != "group_binding_outbound_controlled_only" || c.CanConfirm ||
				c.Version != c.FriendOutbound.Version || n.outboundLookups.Load() != 1 || n.outboundSends.Load() != 0 {
				t.Fatal("preparing a target sent a request or established identity", c)
			}
			var sealed []byte
			if err = f.db.Platform.QueryRow(context.Background(), `SELECT payload_sealed FROM group_binding_outbound_attempts
				WHERE id=$1 AND attempts_used=0 AND send_key IS NULL`, c.FriendOutbound.ID).Scan(&sealed); err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{n.target.NativeID, n.target.AddAccount, n.target.Name, c.PrivateChallenge, c.GroupChallenge} {
				if secret != "" && bytes.Contains(sealed, []byte(secret)) {
					t.Fatal("outbound attempt persisted plaintext evidence")
				}
			}
			plain, err := f.vault.Open("group-binding-outbound:"+c.FriendOutbound.ID, sealed)
			if err != nil || !bytes.Contains(plain, []byte(n.target.NativeID)) ||
				!bytes.Contains(plain, []byte(f.adminID)) || !bytes.Contains(plain, []byte(f.tenantA)) {
				t.Fatal("sealed outgoing payload lost its pinned identity or website scope", err)
			}
			clear(plain)
			before := outboundCalls(n)
			for range 2 {
				onboardingAPI(t, client, "GET", "/group-binding/challenges/"+c.ID, "", nil, 200)
				onboardingAPI(t, client, "GET", "/group-binding/challenges/"+c.ID+"/friend-verify", "", nil, 200)
			}
			if outboundCalls(n) != before {
				t.Fatal("GET progress triggered native work")
			}
			sendPath := "/group-binding/challenges/" + c.ID + "/friend-send"
			got, _, message := client.requestRaw("POST", sendPath, map[string]any{"version": c.Version},
				http.Header{"Idempotency-Key": {"outbound-missing-captcha"}})
			if got != 410 || message != "group_binding_outbound_controlled_only" || outboundCalls(n) != before {
				t.Fatal("retired outgoing request did not stop before native work", got, message)
			}
			submitted := outboundSend(t, f.app.GroupBinding, ref, c, "outbound-human-confirm")
			if submitted.FriendOutbound == nil ||
				submitted.FriendOutbound.Status != "submitted" || submitted.IdentityProven ||
				submitted.CanConfirm || submitted.Group != nil || n.outboundEffects.Load() != 1 {
				t.Fatal("native submission was mistaken for private identity or group ownership", submitted)
			}
			if friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts a
				JOIN group_binding_outbound_gates g USING(platform,account_id)
				WHERE a.id=$1 AND a.gate_version=g.version AND a.attempts_used=1
				AND g.state='open'`, c.FriendOutbound.ID) != 1 {
				t.Fatal("explicit submission did not pin its current admission version")
			}
			before = outboundCalls(n)
			replay := outboundSend(t, f.app.GroupBinding, ref, c, "outbound-human-confirm")
			if replay.FriendOutbound.ID != submitted.FriendOutbound.ID || outboundCalls(n) != before {
				t.Fatal("idempotent confirmation repeated native work")
			}
			_, err = (outboundControlledService{f.app.GroupBinding}).SendOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-replacement-key"})
			if err == nil || n.outboundEffects.Load() != 1 {
				t.Fatal("a new key replenished the one-request budget")
			}
			observed, err := f.app.GroupBinding.ReconcileOutboundFriend(context.Background(), ref, c.ID,
				submitted.FriendOutbound.ID, control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-reconcile-friend"})
			if err != nil || observed.FriendOutbound == nil || observed.FriendOutbound.Status != "friends" ||
				observed.IdentityProven || observed.CanConfirm || n.outboundEffects.Load() != 1 {
				t.Fatal("friendship observation replaced the independent private proof", observed, err)
			}
			f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: n.target.NativeID, Text: c.PrivateChallenge}, nil)
			if err = f.app.GroupBinding.ScanOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			identity, err := f.app.GroupBinding.Read(context.Background(), ref, c.ID)
			if err != nil || !identity.IdentityProven || identity.CanConfirm || identity.Group != nil ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_challenges WHERE id=$1
					AND identity_event_id IS NOT NULL AND identity_friend_proof_id IS NULL`, c.ID) != 1 {
				t.Fatal("authenticated private proof did not establish its own source", identity, err)
			}
			if friendCount(t, f, `SELECT count(*) FROM group_binding_friend_attempts`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_friend_proofs`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_bindings WHERE management_enabled OR ai_enabled`) != 0 ||
				friendCount(t, f, `SELECT count(*) FROM audit_events WHERE detail::text LIKE '%'||$1||'%'
					OR detail::text LIKE '%'||$2||'%' OR detail::text LIKE '%'||$3||'%'`,
					n.target.NativeID, n.target.AddAccount, c.PrivateChallenge) != 0 {
				t.Fatal("outgoing onboarding activated old proof/binding records or leaked private data")
			}
			for _, statement := range []string{
				`UPDATE group_binding_outbound_attempts SET payload_sealed='changed' WHERE id=$1`,
				`UPDATE group_binding_outbound_attempts SET prepare_key='replacement-key' WHERE id=$1`,
				`UPDATE group_binding_outbound_attempts SET send_key='replacement-key' WHERE id=$1`,
				`UPDATE group_binding_outbound_attempts SET attempts_used=0,status='prepared' WHERE id=$1`,
				`DELETE FROM group_binding_outbound_attempts WHERE id=$1`,
			} {
				if _, err = f.db.Platform.Exec(context.Background(), statement, c.FriendOutbound.ID); err == nil {
					t.Fatal("outgoing scope/history or consumed budget became mutable", statement)
				}
			}
			if _, err = f.db.Platform.Exec(context.Background(), `UPDATE group_binding_challenges
				SET friend_flow='legacy' WHERE id=$1`, c.ID); err == nil {
				t.Fatal("an outbound challenge could be moved into the retired consent path")
			}
		})
	}
}

func TestGroupBindingOutboundPostgresAlreadyFriendsStillRequiresPrivateProof(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, platform)
			n.target.AlreadyFriend, n.lookupState = true, "friends"
			c := outboundControlledChallenge(t, f, client, platform, "outbound-existing-friend")
			c = outboundPrepare(t, f, n, ref, c, "outbound-existing-prepare")
			if c.FriendOutbound.Status != "friends" || c.FriendOutbound.CanSend || c.IdentityProven ||
				n.outboundEffects.Load() != 0 || friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE id=$1 AND attempts_used=0 AND send_key IS NULL`, c.FriendOutbound.ID) != 1 {
				t.Fatal("existing friendship sent another request or bypassed private proof")
			}
			f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: "synthetic-wrong-human", Text: c.PrivateChallenge}, nil)
			if err := f.app.GroupBinding.ScanOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			current, err := f.app.GroupBinding.Read(context.Background(), ref, c.ID)
			if err != nil || !current.IdentityProven {
				t.Fatal("controlled contact selection constrained independent private identity", current, err)
			}
			f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: n.target.NativeID, Text: c.PrivateChallenge}, nil)
			if err = f.app.GroupBinding.ScanOnce(context.Background()); err != nil {
				t.Fatal(err)
			}
			current, err = f.app.GroupBinding.Read(context.Background(), ref, c.ID)
			if err != nil || !current.IdentityProven || n.outboundSends.Load() != 0 || n.outboundEffects.Load() != 0 {
				t.Fatal("existing friend could not use authenticated private proof without a new request", current, err)
			}
		})
	}
}

func TestGroupBindingOutboundPostgresActorPlatformAndTargetIsolation(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, platform)
			outboundOpenGate(t, f, platform)
			c := outboundControlledChallenge(t, f, client, platform, "outbound-scope-create")
			for _, other := range []testClient{f.bob, f.alice, f.member} {
				other.workspace = platform
				_, err := (outboundControlledService{f.app.GroupBinding}).PrepareOutboundFriend(context.Background(), onboardingRef(t, other, platform), c.ID,
					control.GroupBindingFriendTargetInput{Version: c.Version, Account: n.target.AddAccount, Key: "outbound-proxy-prepare"})
				if err == nil {
					t.Fatal("another website actor borrowed target preparation")
				}
			}
			otherPlatform := "wsl"
			if platform == "wsl" {
				otherPlatform = "lajiaoliao"
			}
			_, err := (outboundControlledService{f.app.GroupBinding}).PrepareOutboundFriend(context.Background(), onboardingRef(t, client, otherPlatform), c.ID,
				control.GroupBindingFriendTargetInput{Version: c.Version, Account: n.target.AddAccount, Key: "outbound-cross-platform"})
			if err == nil || n.outboundLookups.Load() != 0 {
				t.Fatal("cross-actor/platform scope reached native lookup")
			}
			prepared := outboundPrepare(t, f, n, ref, c, "outbound-owner-prepare")
			before := outboundCalls(n)
			for _, other := range []testClient{f.bob, f.alice, f.member} {
				other.workspace = platform
				otherRef := onboardingRef(t, other, platform)
				if _, err = (outboundControlledService{f.app.GroupBinding}).SendOutboundFriend(context.Background(), otherRef, c.ID,
					control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-proxy-send"}); err == nil {
					t.Fatal("another actor confirmed an outgoing request")
				}
				if _, err = f.app.GroupBinding.ReconcileOutboundFriend(context.Background(), otherRef, c.ID,
					prepared.FriendOutbound.ID, control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-proxy-reconcile"}); err == nil {
					t.Fatal("another actor reconciled an outgoing request")
				}
			}
			for _, in := range []control.GroupBindingFriendTargetInput{
				{Version: c.Version, Account: n.target.AddAccount + "-other", Key: "outbound-owner-prepare"},
				{Version: c.Version + 1, Account: n.target.AddAccount, Key: "outbound-owner-prepare"},
				{Version: c.Version, Account: n.target.AddAccount, Key: "outbound-different-prepare"},
			} {
				if _, err = (outboundControlledService{f.app.GroupBinding}).PrepareOutboundFriend(context.Background(), ref, c.ID, in); err == nil {
					t.Fatal("prepared immutable target accepted changed identity/version/key")
				}
			}
			if outboundCalls(n) != before || n.outboundEffects.Load() != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts WHERE challenge_id=$1`, c.ID) != 1 {
				t.Fatal("scope rejection changed the pinned target or performed native work")
			}
		})
	}
}

func TestGroupBindingOutboundPostgresRejectsUnverifiedTargetEvidence(t *testing.T) {
	for _, change := range []string{"account", "native-id", "name", "self", "self-target", "pending", "friends-without-proof"} {
		t.Run(change, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, "wsl")
			outboundOpenGate(t, f, "wsl")
			c := outboundControlledChallenge(t, f, client, "wsl", "outbound-bad-target-"+change)
			n.outboundEvidence = func(e *control.GroupBindingOutboundEvidence) {
				switch change {
				case "account":
					e.Target.AddAccount += "-other"
				case "native-id":
					e.Target.NativeID = ""
				case "name":
					e.Target.Name = ""
				case "self":
					e.SelfID = "synthetic-other-robot"
				case "self-target":
					e.Target.NativeID = e.SelfID
				case "pending":
					e.State = "pending"
				case "friends-without-proof":
					e.State, e.Target.AlreadyFriend = "friends", false
				}
			}
			_, err := (outboundControlledService{f.app.GroupBinding}).PrepareOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingFriendTargetInput{Version: c.Version, Account: n.target.AddAccount, Key: "outbound-bad-evidence"})
			if err == nil || n.outboundEffects.Load() != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts WHERE challenge_id=$1`, c.ID) != 0 {
				t.Fatal("unverified native target was pinned", err)
			}
		})
	}
}

func TestGroupBindingOutboundPostgresPreparationRejectsLateScopeChange(t *testing.T) {
	for _, change := range []string{"cancel", "version", "epoch", "account-version", "actor", "receiver-owner", "receiver-version", "credential"} {
		t.Run(change, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, "wsl")
			outboundOpenGate(t, f, "wsl")
			c := outboundControlledChallenge(t, f, client, "wsl", "outbound-late-"+change)
			n.beforeOutboundLookup = func(ctx context.Context, _ control.GroupBindingOutboundLookup) error {
				if change == "cancel" {
					_, err := f.app.GroupBinding.Cancel(ctx, ref, c.ID, c.Version)
					return err
				}
				statement, id := "", c.ID
				switch change {
				case "version":
					statement = `UPDATE group_binding_challenges SET version=version+1 WHERE id=$1`
				case "epoch":
					statement, id = `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.accounts["wsl"]
				case "account-version":
					statement, id = `UPDATE accounts SET version=version+1 WHERE id=$1`, f.accounts["wsl"]
				case "actor":
					statement, id = `UPDATE users SET access_version=access_version+1 WHERE id=$1`, f.adminID
				case "receiver-owner":
					statement, id = `UPDATE account_receivers SET owner='replacement-receiver' WHERE account_id=$1`, f.accounts["wsl"]
				case "receiver-version":
					statement, id = `UPDATE account_receivers SET revision=revision+1 WHERE account_id=$1`, f.accounts["wsl"]
				case "credential":
					statement, id = `UPDATE accounts SET credential_sealed='replacement-credential' WHERE id=$1`, f.accounts["wsl"]
				}
				_, err := f.db.Platform.Exec(ctx, statement, id)
				return err
			}
			_, err := (outboundControlledService{f.app.GroupBinding}).PrepareOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingFriendTargetInput{Version: c.Version, Account: n.target.AddAccount, Key: "outbound-late-prepare"})
			if err == nil || n.outboundEffects.Load() != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts WHERE challenge_id=$1`, c.ID) != 0 {
				t.Fatal("late lookup persisted a changed website/native scope", err)
			}
		})
	}
}

func TestGroupBindingOutboundPostgresFinalSubmissionFences(t *testing.T) {
	for _, change := range []string{"cancel", "version", "session", "session-deadline", "actor", "epoch", "account-version",
		"receiver", "receiver-owner", "receiver-version", "credential", "deployment", "gate", "gate-version",
		"target", "self", "stale", "cancel-context", "no-fence", "double-fence"} {
		t.Run(change, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, "wsl")
			outboundOpenGate(t, f, "wsl")
			c := outboundControlledChallenge(t, f, client, "wsl", "outbound-fence-"+change)
			c = outboundPrepare(t, f, n, ref, c, "outbound-fence-prepare")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			n.beforeOutboundSend = func(work context.Context, _ control.GroupBindingOutboundRequest) error {
				statement, id := "", f.accounts["wsl"]
				switch change {
				case "cancel":
					_, err := f.app.GroupBinding.Cancel(work, ref, c.ID, c.Version)
					return err
				case "version":
					statement, id = `UPDATE group_binding_challenges SET version=version+1 WHERE id=$1`, c.ID
				case "session":
					statement, id = `DELETE FROM sessions WHERE user_id=$1`, f.adminID
				case "session-deadline":
					statement, id = `UPDATE sessions SET expires_at=clock_timestamp()+interval '1 second' WHERE user_id=$1`, f.adminID
				case "actor":
					statement, id = `UPDATE users SET access_version=access_version+1 WHERE id=$1`, f.adminID
				case "epoch":
					statement = `UPDATE accounts SET epoch=epoch+1 WHERE id=$1`
				case "account-version":
					statement = `UPDATE accounts SET version=version+1 WHERE id=$1`
				case "receiver":
					statement = `UPDATE account_receivers SET enabled=false WHERE account_id=$1`
				case "receiver-owner":
					statement = `UPDATE account_receivers SET owner='replacement-owner' WHERE account_id=$1`
				case "receiver-version":
					statement = `UPDATE account_receivers SET revision=revision+1 WHERE account_id=$1`
				case "credential":
					statement = `UPDATE accounts SET credential_sealed='replacement-credential' WHERE id=$1`
				case "deployment":
					_, err := f.db.Platform.Exec(work, `UPDATE native_deployment SET version=version+1 WHERE singleton`)
					return err
				case "gate":
					statement = `UPDATE group_binding_outbound_gates SET state='closed',version=version+1 WHERE account_id=$1`
				case "gate-version":
					statement = `UPDATE group_binding_outbound_gates SET version=version+1 WHERE account_id=$1`
				case "cancel-context":
					cancel()
				case "no-fence":
					n.outboundNoFence = true
				case "double-fence":
					n.outboundDoubleFence = true
				}
				if statement != "" {
					_, err := f.db.Platform.Exec(work, statement, id)
					return err
				}
				return nil
			}
			n.outboundEvidence = func(e *control.GroupBindingOutboundEvidence) {
				switch change {
				case "target":
					e.Target.NativeID = "replacement-human"
				case "self":
					e.SelfID = "replacement-bot"
				case "stale":
					e.ObservedAt = time.Now().Add(-time.Minute)
				}
			}
			out, err := (outboundControlledService{f.app.GroupBinding}).SendOutboundFriend(ctx, ref, c.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-final-consent"})
			var state string
			if readErr := f.db.Platform.QueryRow(context.Background(), `SELECT status FROM group_binding_outbound_attempts
				WHERE challenge_id=$1`, c.ID).Scan(&state); readErr != nil {
				t.Fatal(readErr)
			}
			if friendCount(t, f, `SELECT count(*) FROM group_binding_challenges WHERE id=$1
				AND identity_sealed IS NOT NULL`, c.ID) != 0 {
				t.Fatal("invalid final submission established a native identity")
			}
			if change == "double-fence" {
				if err != nil || n.outboundEffects.Load() != 1 || state != "unknown" ||
					out.FriendOutbound == nil || out.FriendOutbound.ID != c.FriendOutbound.ID ||
					out.FriendOutbound.Status != "unknown" || out.FriendOutbound.CanSend ||
					out.IdentityProven || out.CanCancel ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
						WHERE challenge_id=$1 AND status='unknown' AND attempts_used=1
						AND send_key='outbound-final-consent'`, c.ID) != 1 {
					t.Fatal("second fence did not freeze the single dispatched request", state, err, n.outboundEffects.Load())
				}
				before := outboundCalls(n)
				replay := outboundSend(t, f.app.GroupBinding, ref, c, "outbound-final-consent")
				onboardingAPI(t, client, "GET", "/group-binding/challenges/"+c.ID, "", nil, 200)
				if replay.FriendOutbound.Status != "unknown" || outboundCalls(n) != before {
					t.Fatal("confirmation replay after a duplicate fence repeated native work", replay)
				}
			} else if n.outboundEffects.Load() != 0 || err == nil && state == "submitted" {
				t.Fatal("final scope/fence change admitted an invalid or repeated native request", state, err, n.outboundEffects.Load())
			}
		})
	}
}

func TestGroupBindingOutboundPostgresConcurrentConfirmationSubmitsOnce(t *testing.T) {
	f, n, client, ref := outboundFixture(t, "wsl")
	outboundOpenGate(t, f, "wsl")
	c := outboundControlledChallenge(t, f, client, "wsl", "outbound-concurrent")
	c = outboundPrepare(t, f, n, ref, c, "outbound-concurrent-prepare")
	type result struct {
		out control.GroupBindingChallenge
		err error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	for range 2 {
		go func() {
			<-start
			out, err := (outboundControlledService{f.app.GroupBinding}).SendOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-concurrent-confirm"})
			results <- result{out, err}
		}()
	}
	close(start)
	var successes int
	for range 2 {
		select {
		case got := <-results:
			if got.err == nil && got.out.FriendOutbound != nil && got.out.FriendOutbound.ID == c.FriendOutbound.ID {
				successes++
			}
		case <-time.After(10 * time.Second):
			t.Fatal("concurrent outgoing confirmation did not complete")
		}
	}
	if successes == 0 || n.outboundEffects.Load() != 1 ||
		friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
			WHERE challenge_id=$1 AND status='submitted' AND attempts_used=1`, c.ID) != 1 {
		t.Fatal("concurrent confirms created more than one effective request")
	}
}

func TestGroupBindingOutboundPostgresConcurrentPreparationPinsOneTarget(t *testing.T) {
	f, n, client, ref := outboundFixture(t, "wsl")
	outboundOpenGate(t, f, "wsl")
	c := outboundControlledChallenge(t, f, client, "wsl", "outbound-concurrent-target")
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	n.beforeOutboundLookup = func(ctx context.Context, _ control.GroupBindingOutboundLookup) error {
		entered <- struct{}{}
		select {
		case <-release:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	type result struct {
		out control.GroupBindingChallenge
		err error
	}
	results := make(chan result, 2)
	for range 2 {
		go func() {
			out, err := (outboundControlledService{f.app.GroupBinding}).PrepareOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingFriendTargetInput{Version: c.Version, Account: n.target.AddAccount, Key: "outbound-concurrent-target-prepare"})
			results <- result{out, err}
		}()
	}
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("concurrent target lookup did not start")
		}
	}
	close(release)
	a, b := <-results, <-results
	if a.err != nil || b.err != nil || a.out.FriendOutbound == nil || b.out.FriendOutbound == nil ||
		a.out.FriendOutbound.ID != b.out.FriendOutbound.ID || n.outboundEffects.Load() != 0 ||
		friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts WHERE challenge_id=$1`, c.ID) != 1 {
		t.Fatal("concurrent lookup created multiple pinned targets or sent an application", a.err, b.err)
	}
}

func outboundShortChallenge(t *testing.T, f *onboardingFixture, client testClient, platform string, remaining time.Duration) control.GroupBindingChallenge {
	t.Helper()
	id := control.NewID()
	_, err := f.db.Platform.Exec(context.Background(), `INSERT INTO group_binding_challenges(id,tenant_id,applicant_id,
		platform,account_id,account_epoch,account_version,applicant_version,token_hash,private_token_hash,
		create_key,request_hash,created_at,expires_at,reservation_until,friend_flow,applicant_mode)
		SELECT $1,$2,u.id,$3,a.id,a.epoch,a.version,u.access_version,$6,$7,$8,$6,
		clock_timestamp()-interval '1 minute',clock_timestamp()+make_interval(secs=>$9),
		clock_timestamp()+make_interval(secs=>$9),'outbound','operator_demo'
		FROM accounts a JOIN users u ON u.id=$4 WHERE a.id=$5`, id, f.tenantA, platform, f.adminID,
		f.accounts[platform], bytes.Repeat([]byte{21}, 32), bytes.Repeat([]byte{22}, 32),
		"outbound-short-"+id, remaining.Seconds())
	if err != nil {
		t.Fatal(err)
	}
	out, err := f.app.GroupBinding.Read(context.Background(), onboardingRef(t, client, platform), id)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGroupBindingOutboundPostgresExpirationStopsLookupAndSubmission(t *testing.T) {
	for _, phase := range []string{"lookup", "send"} {
		t.Run(phase, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, "wsl")
			outboundOpenGate(t, f, "wsl")
			remaining := -time.Second
			if phase == "send" {
				remaining = 6 * time.Second
			}
			var c control.GroupBindingChallenge
			if phase == "lookup" {
				c = outboundShortChallenge(t, f, client, "wsl", remaining)
			} else {
				c = outboundControlledChallenge(t, f, client, "wsl", "outbound-expiring-create", 6)
			}
			if phase == "lookup" {
				_, err := f.app.GroupBinding.PrepareOutboundFriend(context.Background(), ref, c.ID,
					control.GroupBindingFriendTargetInput{Version: c.Version, Account: n.target.AddAccount, Key: "outbound-expired-prepare"})
				if err == nil || c.State != "expired" || n.outboundLookups.Load() != 0 {
					t.Fatal("expired challenge consulted a native account", c, err)
				}
				return
			}
			c = outboundPrepare(t, f, n, ref, c, "outbound-expiring-prepare")
			var expires time.Time
			if err := f.db.Platform.QueryRow(context.Background(), `SELECT expires_at FROM group_binding_outbound_attempts
				WHERE challenge_id=$1`, c.ID).Scan(&expires); err != nil {
				t.Fatal(err)
			}
			if delay := time.Until(expires.Add(-3900 * time.Millisecond)); delay > 0 {
				timer := time.NewTimer(delay)
				defer timer.Stop()
				<-timer.C
			}
			out, err := (outboundControlledService{f.app.GroupBinding}).SendOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-expiring-confirm"})
			if err == nil || n.outboundSends.Load() != 0 || n.outboundEffects.Load() != 0 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE challenge_id=$1 AND status='prepared' AND attempts_used=0 AND send_key IS NULL`, c.ID) != 1 {
				t.Fatal("near-expired confirmation started or consumed a native request", out, err)
			}
		})
	}
}

func TestGroupBindingOutboundPostgresInterruptedPreparationAndSubmissionStayBounded(t *testing.T) {
	for _, status := range []string{"prepared", "submitting"} {
		t.Run(status, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, "wsl")
			outboundOpenGate(t, f, "wsl")
			c := outboundControlledChallenge(t, f, client, "wsl", "outbound-recovery-"+status)
			c = outboundPrepare(t, f, n, ref, c, "outbound-recovery-prepare")
			if status == "submitting" {
				if _, err := f.db.Platform.Exec(context.Background(), `UPDATE test_windows SET used=1
					WHERE target_ids[1]='onboarding_challenge:'||$1::text||':'||$2::bigint::text`, c.ID, c.Version); err != nil {
					t.Fatal(err)
				}
				if _, err := f.db.Platform.Exec(context.Background(), `UPDATE group_binding_outbound_attempts
					SET status='submitting',attempts_used=1,send_key='synthetic-interrupted-confirm',
						gate_version=(SELECT version FROM group_binding_outbound_gates
							WHERE platform='wsl' AND account_id=$2 AND state='open')
					WHERE id=$1`, c.FriendOutbound.ID, f.accounts["wsl"]); err != nil {
					t.Fatal(err)
				}
			}
			before := outboundCalls(n)
			restarted := &control.GroupBindingService{DB: f.db, Vault: f.vault, Catalog: f.app.GroupBinding.Catalog,
				Native: n, Decode: f.decode}
			for range 2 {
				if err := restarted.RecoverOutboundFriends(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			current, err := restarted.Read(context.Background(), ref, c.ID)
			want := "failed"
			if status == "submitting" {
				want = "unknown"
			}
			if err != nil || current.FriendOutbound == nil || current.FriendOutbound.ID != c.FriendOutbound.ID ||
				current.FriendOutbound.Status != want || current.FriendOutbound.CanSend ||
				outboundCalls(n) != before || current.IdentityProven {
				t.Fatal("restart replayed pending work or lost its immutable operation", current, err)
			}
		})
	}
}

func TestGroupBindingOutboundPostgresUnknownIsReconciledWithoutResend(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, platform)
			outboundOpenGate(t, f, platform)
			c := outboundControlledChallenge(t, f, client, platform, "outbound-unknown")
			c = outboundPrepare(t, f, n, ref, c, "outbound-unknown-prepare")
			n.outboundUnknown = true
			out := outboundSend(t, f.app.GroupBinding, ref, c, "outbound-unknown-confirm")
			if out.FriendOutbound.Status != "unknown" || out.CanCancel || out.IdentityProven ||
				n.outboundEffects.Load() != 1 {
				t.Fatal("unknown request was not frozen", out)
			}
			if _, err := f.app.GroupBinding.Cancel(context.Background(), ref, c.ID, c.Version); err == nil {
				t.Fatal("cancellation discarded an unknown outgoing operation")
			}
			before := outboundCalls(n)
			outboundSend(t, f.app.GroupBinding, ref, c, "outbound-unknown-confirm")
			onboardingAPI(t, client, "GET", "/group-binding/challenges/"+c.ID, "", nil, 200)
			if outboundCalls(n) != before {
				t.Fatal("unknown replay or GET issued another request")
			}
			restarted := &control.GroupBindingService{DB: f.db, Vault: f.vault, Catalog: f.app.GroupBinding.Catalog,
				Native: n, Decode: f.decode}
			for range 2 {
				if err := restarted.Recover(context.Background()); err != nil {
					t.Fatal(err)
				}
				if err := restarted.RecoverOutboundFriends(context.Background()); err != nil {
					t.Fatal(err)
				}
			}
			if outboundCalls(n) != before {
				t.Fatal("startup recovery replayed an outgoing native operation")
			}
			n.outboundReconcileState = "pending"
			reconciled, err := restarted.ReconcileOutboundFriend(context.Background(), ref, c.ID, c.FriendOutbound.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-unknown-read"})
			if err != nil || reconciled.FriendOutbound == nil || reconciled.FriendOutbound.Status == "prepared" ||
				reconciled.IdentityProven || n.outboundEffects.Load() != 1 {
				t.Fatal("read-only pending reconciliation replenished a request", reconciled, err)
			}
			n.outboundReconcileState = "friends"
			reconciled, err = restarted.ReconcileOutboundFriend(context.Background(), ref, c.ID, c.FriendOutbound.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-friends-read"})
			if err != nil || reconciled.FriendOutbound == nil || reconciled.FriendOutbound.Status != "friends" ||
				reconciled.IdentityProven || n.outboundEffects.Load() != 1 || n.outboundSends.Load() != 1 {
				t.Fatal("original outgoing request was resent or mistaken for identity", reconciled, err)
			}
		})
	}
}

func TestGroupBindingOutboundPostgresPostFenceFailureRequiresExplicitReconciliation(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		for _, failure := range []struct {
			name, code string
			err        error
		}{
			{"invalid_response", "native_response_invalid", &nativeauth.Error{Code: "native_response_invalid", Submitted: true}},
			{"ordinary_error", "native_setting_result_unknown", errors.New("synthetic post-fence response validation failure")},
		} {
			t.Run(platform+"/"+failure.name, func(t *testing.T) {
				f, n, client, ref := outboundFixture(t, platform)
				outboundOpenGate(t, f, platform)
				c := outboundControlledChallenge(t, f, client, platform, "outbound-post-fence-create")
				c = outboundPrepare(t, f, n, ref, c, "outbound-post-fence-prepare")
				n.postFenceFailure = failure.err
				const sendKey = "outbound-post-fence-confirm"
				out := outboundSend(t, f.app.GroupBinding, ref, c, sendKey)
				operation := c.FriendOutbound.ID
				if out.FriendOutbound.ID != operation || out.FriendOutbound.Status != "unknown" ||
					out.FriendOutbound.CanSend || out.CanCancel || out.CanConfirm || out.IdentityProven || out.Group != nil ||
					n.outboundSends.Load() != 1 || n.outboundEffects.Load() != 1 || n.outboundReconciles.Load() != 0 {
					t.Fatal("post-fence failure was acknowledged or retried instead of frozen", out)
				}
				if friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE id=$1 AND challenge_id=$2 AND status='unknown' AND attempts_used=1
					AND send_key=$3 AND result_code=$4`, operation, c.ID, sendKey, failure.code) != 1 {
					t.Fatal("successful fence concealed a later adapter failure in durable state")
				}
				if friendCount(t, f, `SELECT count(*) FROM audit_events
					WHERE resource_id=$1 AND action='group_binding_outbound_dispatched'`, operation) != 1 ||
					friendCount(t, f, `SELECT count(*) FROM audit_events
						WHERE resource_id=$1 AND action='group_binding_outbound_submitted'`, operation) != 0 {
					t.Fatal("dispatch audit claimed a fully validated submission before the adapter returned")
				}

				before := outboundCalls(n)
				for range 2 {
					replay := outboundSend(t, f.app.GroupBinding, ref, c, sendKey)
					if replay.FriendOutbound.ID != operation || replay.FriendOutbound.Status != "unknown" {
						t.Fatal("same-key confirmation did not retain the unknown original operation", replay)
					}
				}
				onboardingAPI(t, client, "GET", "/group-binding/challenges/"+c.ID, "", nil, 200)
				if outboundCalls(n) != before {
					t.Fatal("unknown confirmation replay or progress read issued native work")
				}

				for _, observed := range []struct{ native, status, code string }{
					{"pending", "submitted", "native_friend_request_observed"},
					{"friends", "friends", "native_friend_relationship_observed"},
				} {
					n.outboundReconcileState = observed.native
					reconcileCalls := n.outboundReconciles.Load()
					reconciled, err := f.app.GroupBinding.ReconcileOutboundFriend(context.Background(), ref, c.ID, operation,
						control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-post-fence-" + observed.native})
					if err != nil || reconciled.FriendOutbound == nil || reconciled.FriendOutbound.ID != operation ||
						reconciled.FriendOutbound.Status != observed.status || reconciled.FriendOutbound.CanSend ||
						reconciled.IdentityProven || reconciled.CanConfirm || reconciled.Group != nil ||
						n.outboundReconciles.Load() != reconcileCalls+1 ||
						n.outboundSends.Load() != 1 || n.outboundEffects.Load() != 1 {
						t.Fatal("explicit reconciliation did not advance the original request without resending", reconciled, err)
					}
					if friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
						WHERE id=$1 AND status=$2 AND result_code=$3 AND attempts_used=1 AND send_key=$4`,
						operation, observed.status, observed.code, sendKey) != 1 {
						t.Fatal("reconciliation lost the original attempt or replenished its budget")
					}
					before = outboundCalls(n)
					replay := outboundSend(t, f.app.GroupBinding, ref, c, sendKey)
					if replay.FriendOutbound.ID != operation || replay.FriendOutbound.Status != observed.status ||
						outboundCalls(n) != before {
						t.Fatal("confirmation replay after reconciliation resubmitted the original request", replay)
					}
				}

				f.ingress(t, platform, control.OnboardingMessage{Private: true, Sender: n.target.NativeID, Text: c.PrivateChallenge}, nil)
				if err := f.app.GroupBinding.ScanOnce(context.Background()); err != nil {
					t.Fatal(err)
				}
				identity, err := f.app.GroupBinding.Read(context.Background(), ref, c.ID)
				if err != nil || !identity.IdentityProven || identity.CanConfirm || identity.Group != nil ||
					identity.FriendOutbound == nil || identity.FriendOutbound.Status != "friends" ||
					n.outboundSends.Load() != 1 || n.outboundEffects.Load() != 1 || n.outboundReconciles.Load() != 2 ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts WHERE challenge_id=$1`, c.ID) != 1 ||
					friendCount(t, f, `SELECT count(*) FROM group_bindings WHERE management_enabled OR ai_enabled`) != 0 {
					t.Fatal("independent private proof could not continue safely after post-fence recovery", identity, err)
				}
			})
		}
	}
}

func TestGroupBindingOutboundPostgresQueuedSendUsesFreshCooldownReservation(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, platform)
			outboundOpenGate(t, f, platform)
			c := outboundControlledChallenge(t, f, client, platform, "outbound-queued-create")
			c = outboundPrepare(t, f, n, ref, c, "outbound-queued-prepare")
			n.beforeOutboundSend = func(ctx context.Context, _ control.GroupBindingOutboundRequest) error {
				timer := time.NewTimer(3200 * time.Millisecond)
				defer timer.Stop()
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-timer.C:
					return nil
				}
			}
			const sendKey = "outbound-queued-confirm"
			out := outboundSend(t, f.app.GroupBinding, ref, c, sendKey)
			if out.FriendOutbound.ID != c.FriendOutbound.ID || out.FriendOutbound.Status != "submitted" ||
				out.FriendOutbound.Code != "native_friend_request_acknowledged" || out.FriendOutbound.CanSend ||
				out.IdentityProven || n.outboundSends.Load() != 1 || n.outboundEffects.Load() != 1 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE id=$1 AND status='submitted' AND attempts_used=1 AND send_key=$2`,
					c.FriendOutbound.ID, sendKey) != 1 ||
				friendCount(t, f, `SELECT count(*) FROM business_rule_account_cooldowns
					WHERE account_id=$1 AND next_write_at>clock_timestamp()`, f.accounts[platform]) != 1 {
				t.Fatal("queueing consumed the final cooldown reservation before native dispatch", out)
			}
			before := outboundCalls(n)
			replay := outboundSend(t, f.app.GroupBinding, ref, c, sendKey)
			if replay.FriendOutbound.Status != "submitted" || outboundCalls(n) != before {
				t.Fatal("queued successful request was resent on confirmation replay", replay)
			}
		})
	}
}

func TestGroupBindingOutboundPostgresRejectedOriginalRequestRetainsBudgetAndAllowsCancel(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, platform)
			outboundOpenGate(t, f, platform)
			c := outboundControlledChallenge(t, f, client, platform, "outbound-rejected-create")
			c = outboundPrepare(t, f, n, ref, c, "outbound-rejected-prepare")
			n.postFenceFailure = &nativeauth.Error{Code: "native_response_invalid", Submitted: true}
			const sendKey = "outbound-rejected-confirm"
			out := outboundSend(t, f.app.GroupBinding, ref, c, sendKey)
			operation := c.FriendOutbound.ID
			if out.FriendOutbound.Status != "unknown" || out.CanCancel || n.outboundEffects.Load() != 1 {
				t.Fatal("synthetic rejected request did not begin with an unresolved single attempt", out)
			}
			n.outboundReconcileState = "rejected"
			n.outboundEvidence = func(e *control.GroupBindingOutboundEvidence) { e.ApplicationID = "" }
			out, err := f.app.GroupBinding.ReconcileOutboundFriend(context.Background(), ref, c.ID, operation,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-rejected-no-application"})
			if err != nil || out.FriendOutbound == nil || out.FriendOutbound.Status != "unknown" || out.CanCancel {
				t.Fatal("rejection without the original native application evidence released unknown work", out, err)
			}
			n.outboundEvidence = nil
			out, err = f.app.GroupBinding.ReconcileOutboundFriend(context.Background(), ref, c.ID, operation,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-rejected-original"})
			if err != nil || out.FriendOutbound == nil || out.FriendOutbound.ID != operation ||
				out.FriendOutbound.Status != "failed" || out.FriendOutbound.Code != "native_friend_request_rejected" ||
				out.FriendOutbound.CanSend || out.FriendOutbound.CanReconcile || !out.CanCancel ||
				out.IdentityProven || out.CanConfirm || n.outboundSends.Load() != 1 ||
				n.outboundEffects.Load() != 1 || n.outboundReconciles.Load() != 2 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE id=$1 AND status='failed' AND result_code='native_friend_request_rejected'
					AND attempts_used=1 AND send_key=$2`, operation, sendKey) != 1 {
				t.Fatal("verified original rejection did not settle with a consumed budget", out, err)
			}
			before := outboundCalls(n)
			replay := outboundSend(t, f.app.GroupBinding, ref, c, sendKey)
			if replay.FriendOutbound.Status != "failed" || outboundCalls(n) != before {
				t.Fatal("confirmation replay resent a rejected original request", replay)
			}
			_, err = (outboundControlledService{f.app.GroupBinding}).SendOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-rejected-replacement"})
			friendFailure(t, err, 409, "idempotency_conflict")
			cancelled, err := f.app.GroupBinding.Cancel(context.Background(), ref, c.ID, c.Version)
			if err != nil || cancelled.State != "cancelled" || outboundCalls(n) != before ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE id=$1 AND status='failed' AND result_code='native_friend_request_rejected'
					AND attempts_used=1 AND send_key=$2`, operation, sendKey) != 1 {
				t.Fatal("cancelling a settled rejection retried it or erased its attempt history", cancelled, err)
			}
		})
	}
}

// Seed an interrupted operation with its original expired deadline. Immutable
// payloads and timestamps are fixed at insertion, never changed to age a test.
func outboundExpiredSubmitting(t *testing.T, f *onboardingFixture, n *bindingOutboundNativeFixture,
	ref control.SessionRef, c control.GroupBindingChallenge, sendKey string) control.GroupBindingChallenge {
	t.Helper()
	ctx := context.Background()
	var epoch, accountVersion, applicantVersion, gate int64
	var created, expires time.Time
	if err := f.db.Platform.QueryRow(ctx, `SELECT c.account_epoch,c.account_version,c.applicant_version,g.version,
		c.created_at+interval '5 seconds',c.created_at+interval '10 seconds'
		FROM group_binding_challenges c JOIN group_binding_outbound_gates g
		ON g.account_id=c.account_id AND g.platform=c.platform WHERE c.id=$1`, c.ID).
		Scan(&epoch, &accountVersion, &applicantVersion, &gate, &created, &expires); err != nil {
		t.Fatal(err)
	}
	scope := control.GroupBindingNativeScope{Platform: c.Platform, AccountID: c.Robot.ID,
		AccountEpoch: epoch, AccountVersion: accountVersion}
	evidence, err := n.outgoingEvidence(ctx, scope)
	if err != nil {
		t.Fatal(err)
	}
	request := control.GroupBindingOutboundRequest{GroupBindingNativeScope: scope, OperationID: control.NewID(),
		Target: n.target, SelfID: evidence.SelfID, ExpiresAt: expires}
	payload := struct {
		Challenge, Actor, Tenant  string
		Version, ApplicantVersion int64
		Request                   control.GroupBindingOutboundRequest
		Connection                control.GroupBindingConnectionEvidence
	}{c.ID, c.Scope.ActorID, c.Scope.TenantID, c.Version, applicantVersion, request, evidence.GroupBindingConnectionEvidence}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	defer clear(raw)
	sealed, err := f.vault.Seal("group-binding-outbound:"+request.OperationID, raw)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = f.db.Platform.Exec(ctx, `INSERT INTO group_binding_outbound_attempts(id,challenge_id,actor_id,tenant_id,
		platform,account_id,account_epoch,account_version,applicant_version,challenge_version,gate_version,
		payload_sealed,prepare_key,send_key,status,attempts_used,created_at,expires_at,updated_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,'submitting',1,$15,$16,$15)`,
		request.OperationID, c.ID, c.Scope.ActorID, c.Scope.TenantID, c.Platform, c.Robot.ID,
		epoch, accountVersion, applicantVersion, c.Version, gate, sealed, "expired-prepare-"+request.OperationID,
		sendKey, created, expires); err != nil {
		t.Fatal(err)
	}
	n.outboundMu.Lock()
	n.lastOutbound = request
	n.outboundMu.Unlock()
	out, err := f.app.GroupBinding.Read(ctx, ref, c.ID)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestGroupBindingOutboundPostgresExpiredSubmittingReconcilesWithoutResend(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f, n, client, ref := outboundFixture(t, platform)
			outboundOpenGate(t, f, platform)
			c := outboundShortChallenge(t, f, client, platform, time.Minute)
			const sendKey = "outbound-expired-submit-confirm"
			c = outboundExpiredSubmitting(t, f, n, ref, c, sendKey)
			if c.FriendOutbound == nil || c.FriendOutbound.Status != "submitting" ||
				!c.FriendOutbound.CanReconcile || c.FriendOutbound.CanSend || c.CanCancel ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE challenge_id=$1 AND expires_at+interval '3 seconds'<clock_timestamp()
					AND status='submitting' AND attempts_used=1`, c.ID) != 1 {
				t.Fatal("expired interrupted operation was not available for explicit recovery", c)
			}
			operation := c.FriendOutbound.ID
			before := outboundCalls(n)
			_, err := f.app.GroupBinding.SendOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: sendKey})
			friendFailure(t, err, 409, "group_binding_outbound_controlled_only")
			replay, err := f.app.GroupBinding.Read(context.Background(), ref, c.ID)
			onboardingAPI(t, client, "GET", "/group-binding/challenges/"+c.ID, "", nil, 200)
			if err != nil || replay.FriendOutbound.Status != "submitting" || outboundCalls(n) != before {
				t.Fatal("expired send replay or progress read dispatched or reconciled native work", replay)
			}
			_, err = f.app.GroupBinding.SendOutboundFriend(context.Background(), ref, c.ID,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-expired-submit-replacement"})
			friendFailure(t, err, 409, "group_binding_outbound_controlled_only")
			readFailure := errors.New("synthetic read-only outbound observation failed")
			n.beforeOutboundReconcile = func(ctx context.Context, request control.GroupBindingOutboundRequest) error {
				if friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE id=$1 AND status='unknown' AND result_code='submission_deadline_elapsed'
					AND attempts_used=1 AND send_key=$2`, request.OperationID, sendKey) != 1 {
					t.Fatal("deadline recovery did not persist unknown before native observation")
				}
				return readFailure
			}
			_, err = f.app.GroupBinding.ReconcileOutboundFriend(context.Background(), ref, c.ID, operation,
				control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-expired-submit-observe"})
			if !errors.Is(err, readFailure) || n.outboundReconciles.Load() != 1 ||
				friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
					WHERE id=$1 AND status='unknown' AND result_code='submission_deadline_elapsed'
					AND attempts_used=1`, operation) != 1 {
				t.Fatal("failed observation left the elapsed operation submitting or lost its original error", err)
			}
			before = outboundCalls(n)
			replay, err = f.app.GroupBinding.Read(context.Background(), ref, c.ID)
			if err != nil || replay.FriendOutbound.Status != "unknown" || replay.CanCancel || outboundCalls(n) != before {
				t.Fatal("elapsed unknown request could be resent or discarded", replay)
			}
			n.beforeOutboundReconcile = nil
			for _, observed := range []struct{ native, status string }{{"pending", "submitted"}, {"friends", "friends"}} {
				n.outboundReconcileState = observed.native
				reconciled, err := f.app.GroupBinding.ReconcileOutboundFriend(context.Background(), ref, c.ID, operation,
					control.GroupBindingConfirmation{Version: c.Version, Key: "outbound-expired-submit-" + observed.native})
				if err != nil || reconciled.FriendOutbound == nil || reconciled.FriendOutbound.ID != operation ||
					reconciled.FriendOutbound.Status != observed.status || reconciled.FriendOutbound.CanSend ||
					reconciled.IdentityProven || n.outboundSends.Load() != 0 || n.outboundEffects.Load() != 0 ||
					friendCount(t, f, `SELECT count(*) FROM group_binding_outbound_attempts
						WHERE id=$1 AND status=$2 AND attempts_used=1 AND send_key=$3`,
						operation, observed.status, sendKey) != 1 {
					t.Fatal("expired operation did not reconcile its original request without dispatch", reconciled, err)
				}
			}
			if n.outboundReconciles.Load() != 3 {
				t.Fatal("deadline recovery performed an implicit extra native reconciliation")
			}
		})
	}
}
