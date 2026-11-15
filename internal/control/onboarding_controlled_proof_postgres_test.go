package control

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

// All identities, routes, observations and native ports below are synthetic.
// The inherited fixture resets only wsl_test and must run serially with main.
type onboardingProofPG struct {
	*controlledExecutionFixture
	bindings                    *GroupBindingService
	challenge, claimant, sender string
	privateText, groupText      string
	reads                       atomic.Int32
}

type onboardingProofNative struct{ f *onboardingProofPG }

func (n onboardingProofNative) BindingRoles(ctx context.Context, scope GroupBindingNativeScope) (GroupBindingRoleEvidence, error) {
	f := n.f
	f.reads.Add(1)
	if scope.Platform != f.scope.Platform || scope.NativeGroupID != f.nativeID ||
		scope.NativeSenderID != f.sender || scope.AccountID != f.claimant && scope.AccountID != f.scope.AccountID {
		return GroupBindingRoleEvidence{}, errors.New("synthetic proof scope escaped")
	}
	deadline, ok := ctx.Deadline()
	if scope.AccountID == f.claimant && (!ok || time.Until(deadline) > 8*time.Second) {
		return GroupBindingRoleEvidence{}, errors.New("synthetic proof read is unbounded")
	}
	out := GroupBindingRoleEvidence{GroupBindingConnectionEvidence: GroupBindingConnectionEvidence{
		Platform: scope.Platform, AccountID: scope.AccountID, AccountEpoch: scope.AccountEpoch,
		AccountVersion: scope.AccountVersion, ObservedAt: time.Now()},
		NativeGroupID: scope.NativeGroupID, NativeSenderID: scope.NativeSenderID,
		NativeOwnerID: "native-owner", SenderRole: "admin", RobotRole: "admin", Present: true,
		GroupName: "Synthetic controlled proof group"}
	var credential []byte
	err := f.f.db.Platform.QueryRow(ctx, `SELECT a.credential_sealed,r.owner,r.revision
		FROM accounts a JOIN account_receivers r ON r.account_id=a.id WHERE a.id=$1`,
		scope.AccountID).Scan(&credential, &out.ReceiverOwner, &out.ReceiverRevision)
	copy(out.CredentialDigest[:], security.Digest(string(credential)))
	if err == nil && scope.Platform == "wsl" {
		err = f.f.db.Platform.QueryRow(ctx, `SELECT version FROM native_deployment WHERE singleton`).Scan(&out.DeploymentVersion)
	}
	return out, err
}

func (onboardingProofNative) BindingInvitation(context.Context, GroupBindingInvitationRequest) (GroupBindingInvitationEvidence, error) {
	return GroupBindingInvitationEvidence{}, errors.New("unexpected synthetic invitation read")
}

func (onboardingProofNative) AcceptBindingInvitation(context.Context, GroupBindingAcceptRequest, GroupBindingAcceptFence) error {
	return errors.New("unexpected synthetic invitation mutation")
}

func newOnboardingProofPG(t *testing.T, platform string) *onboardingProofPG {
	t.Helper()
	base := newControlledExecutionFixture(t, platform, true, 10*time.Minute)
	f := &onboardingProofPG{controlledExecutionFixture: base, challenge: NewID(),
		privateText: "群接入 " + strings.Repeat("B", 43), groupText: "群绑定 " + strings.Repeat("A", 43)}
	err := f.f.db.Platform.QueryRow(f.f.ctx, `SELECT claimant_account_id::text,claimant_native_id
		FROM onboarding_test_scopes WHERE id=$1`, f.scopeID).Scan(&f.claimant, &f.sender)
	if err != nil {
		t.Fatal(err)
	}
	f.f.exec(`UPDATE accounts SET connection_enabled=true WHERE id IN($1,$2)`, f.claimant, f.scope.AccountID)
	f.f.exec(`UPDATE account_receivers SET observed_at=clock_timestamp(),
		snapshot=jsonb_build_object('state','connected','heartbeat_at',clock_timestamp())
		WHERE account_id IN($1,$2)`, f.claimant, f.scope.AccountID)
	f.f.exec(`INSERT INTO account_groups(account_id,group_id,role,observed_at,observation_source)
		VALUES($1,$2,'admin',clock_timestamp(),'native')`, f.claimant, f.scope.GroupID)
	f.f.exec(`UPDATE group_binding_ownership SET state='paused',version=version+1 WHERE group_id=$1`, f.scope.GroupID)
	f.f.exec(`INSERT INTO group_binding_challenges(id,tenant_id,applicant_id,applicant_mode,platform,account_id,
		account_epoch,account_version,applicant_version,token_hash,private_token_hash,create_key,request_hash,state,
		group_id,binding_version,controlled_scope_id)
		SELECT $1,s.tenant_id,s.actor_id,'operator_demo',s.platform,s.account_id,s.account_epoch,s.account_version,
		s.actor_version,$3,$4,$1::uuid::text,$3,'waiting',s.group_id,b.version,s.id
		FROM onboarding_test_scopes s JOIN group_bindings b ON b.group_id=s.group_id WHERE s.id=$2`,
		f.challenge, f.scopeID, bindingTokenDigest(strings.Repeat("A", 43)), bindingTokenDigest(strings.Repeat("B", 43)))
	f.bindings = &GroupBindingService{DB: f.f.db, Vault: f.f.service.Vault, Native: onboardingProofNative{f}}
	f.bindings.Decode = func(platform, account, self, key string, _ int64, projection, source []byte) (OnboardingMessage, error) {
		var out OnboardingMessage
		plain, err := f.bindings.Vault.Open("proof-fixture-projection", projection)
		if err != nil {
			return out, err
		}
		defer clear(plain)
		original, err := f.bindings.Vault.Open("proof-fixture-source", source)
		if err != nil {
			return out, err
		}
		defer clear(original)
		var route struct{ Platform, Account, Self, Key string }
		if json.Unmarshal(original, &route) != nil || route.Platform != platform ||
			route.Account != account || route.Self != self || route.Key != key {
			return out, errors.New("synthetic authenticated proof route changed")
		}
		return out, json.Unmarshal(plain, &out)
	}
	for i := 0; i < 2; i++ {
		if err = controlledUsageTx(t, f.f, func(tx pgx.Tx) error {
			return ReserveControlledScopeNativeTx(f.f.ctx, tx, f.scopeID, "synthetic_onboarding_contact", NewID())
		}); err != nil {
			t.Fatal(err)
		}
	}
	assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
	return f
}

func (f *onboardingProofPG) input(t *testing.T, kind, key string) OnboardingControlledProofInput {
	t.Helper()
	var version int64
	if err := f.f.db.Platform.QueryRow(f.f.ctx, `SELECT version FROM group_binding_challenges WHERE id=$1`,
		f.challenge).Scan(&version); err != nil {
		t.Fatal(err)
	}
	text := f.groupText
	if kind == "private" {
		text = f.privateText
	}
	return OnboardingControlledProofInput{Version: version, Kind: kind, Text: text, Seconds: 300, Key: key}
}

func (f *onboardingProofPG) ingress(t *testing.T, kind, recipient, text string) {
	t.Helper()
	key := NewID()
	message := OnboardingMessage{Sender: f.sender, Recipient: recipient, Text: text, ClientID: key, Private: kind == "private"}
	if !message.Private {
		message.Group = f.nativeID
	}
	raw, _ := json.Marshal(message)
	projection, err := f.bindings.Vault.Seal("proof-fixture-projection", raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	var self string
	if err = f.f.db.Platform.QueryRow(f.f.ctx, `SELECT native_uid FROM accounts WHERE id=$1`,
		f.scope.AccountID).Scan(&self); err != nil {
		t.Fatal(err)
	}
	raw, _ = json.Marshal(struct{ Platform, Account, Self, Key string }{f.scope.Platform, f.scope.AccountID, self, key})
	source, err := f.bindings.Vault.Seal("proof-fixture-source", raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	var event int64
	err = f.f.db.Platform.QueryRow(f.f.ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
		VALUES($1,$2,$3,clock_timestamp(),$4) RETURNING id`, f.scope.AccountID, key, kind, source).Scan(&event)
	if err != nil {
		t.Fatal(err)
	}
	f.f.exec(`INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
		deployment_version,codec_version,payload_sealed,received_at,decoded_at)
		SELECT id,account_id,'decoded',$2,$3,1,7,$4,received_at,clock_timestamp()
		FROM native_received_events WHERE id=$1`, event, kind, message.Group, projection)
	if err = f.bindings.scanChallenge(f.f.ctx, f.challenge); err != nil {
		t.Fatal(err)
	}
}

func (f *onboardingProofPG) provePrivate(t *testing.T) {
	t.Helper()
	var receiver string
	if err := f.f.db.Platform.QueryRow(f.f.ctx, `SELECT native_uid FROM accounts WHERE id=$1`,
		f.scope.AccountID).Scan(&receiver); err != nil {
		t.Fatal(err)
	}
	f.ingress(t, "private", "another-recipient", f.privateText)
	if n := f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1 AND identity_sealed IS NOT NULL`,
		f.challenge); n != 0 {
		t.Fatal("wrong private recipient established identity")
	}
	f.ingress(t, "private", receiver, f.privateText)
	if n := f.f.count(`SELECT count(*) FROM group_binding_challenges
		WHERE id=$1 AND identity_event_id IS NOT NULL AND identity_sealed IS NOT NULL AND state='waiting'`,
		f.challenge); n != 1 {
		t.Fatal("authenticated synthetic private ingress did not establish identity")
	}
}

func TestPostgresOnboardingControlledProofPreparationAndReceive(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingProofPG(t, platform)
			in := f.input(t, "private", "synthetic-private-window")
			window, err := f.bindings.CreateControlledProofWindow(f.f.ctx, f.ref, f.challenge, in)
			if err != nil || window.AccountID != f.claimant || window.Remaining != 1 || window.ProofKind != "private" ||
				window.GroupID != f.scope.GroupID || !window.ExpiresAt.After(time.Now()) {
				t.Fatal("new claimant proof depended on paused old ownership", window, err)
			}
			if f.reads.Load() != 1 || f.f.count(`SELECT count(*) FROM native_setting_operations`) != 0 ||
				f.f.count(`SELECT count(*) FROM operations`) != 0 ||
				f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1
					AND identity_sealed IS NULL AND identity_event_id IS NULL AND proof_event_id IS NULL`, f.challenge) != 1 {
				t.Fatal("preparing a proof grant submitted work or manufactured proof")
			}
			replay, err := f.bindings.CreateControlledProofWindow(f.f.ctx, f.ref, f.challenge, in)
			if err != nil || replay.ID != window.ID || f.reads.Load() != 1 {
				t.Fatal("idempotent preparation repeated native reads or created another window", err)
			}
			in.Key = "different-private-key"
			_, err = f.bindings.CreateControlledProofWindow(f.f.ctx, f.ref, f.challenge, in)
			capabilityError(t, err, "onboarding_controlled_proof_window_exists")
			_, err = f.bindings.CreateControlledProofWindow(f.f.ctx, f.ref, f.challenge, f.input(t, "group", "premature-group-window"))
			capabilityError(t, err, "controlled_onboarding_scope_inactive")
			assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
			f.provePrivate(t)
			_, _, _, err = controlledProofExecution(f.f.ctx, f.f.db.Platform, window.ID, false)
			capabilityError(t, err, "controlled_onboarding_scope_inactive")
			group, err := f.bindings.CreateControlledProofWindow(f.f.ctx, f.ref, f.challenge, f.input(t, "group", "synthetic-group-window"))
			if err != nil || group.ID == window.ID || group.Remaining != 1 || group.AccountID != f.claimant {
				t.Fatal("verified private ingress did not unlock one separate group proof window", group, err)
			}
			if f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1 AND proof_event_id IS NULL`, f.challenge) != 1 {
				t.Fatal("creating a group proof window manufactured group proof")
			}
			f.ingress(t, "group", f.nativeID, f.groupText)
			if f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1 AND proof_event_id IS NOT NULL
				AND state='proven'`, f.challenge) != 1 {
				t.Fatal("authenticated synthetic group ingress did not establish group proof")
			}
			assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
			for _, secret := range []string{f.privateText, f.groupText, strings.Repeat("A", 43), strings.Repeat("B", 43)} {
				if f.f.count(`SELECT count(*) FROM audit_events WHERE detail::text LIKE '%'||$1||'%'`, secret) != 0 {
					t.Fatal("proof preparation or receive exposed challenge text in audit")
				}
			}
		})
	}
}

func TestPostgresOnboardingControlledProofSingletonConcurrent(t *testing.T) {
	f := newOnboardingProofPG(t, "lajiaoliao")
	in := f.input(t, "private", "synthetic-concurrent-proof")
	var wg sync.WaitGroup
	results, failures := make(chan OnboardingControlledProofWindow, 2), make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := f.bindings.CreateControlledProofWindow(f.f.ctx, f.ref, f.challenge, in)
			results <- result
			failures <- err
		}()
	}
	wg.Wait()
	close(results)
	close(failures)
	for err := range failures {
		if err != nil {
			t.Fatal(err)
		}
	}
	id := ""
	for window := range results {
		if id != "" && window.ID != id {
			t.Fatal("concurrent creation issued multiple singleton grants")
		}
		id = window.ID
	}
	if f.f.count(`SELECT count(*) FROM test_windows WHERE $1=ANY(target_ids)`,
		controlledProofMarker(f.challenge, "private")) != 1 {
		t.Fatal("concurrent creation increased proof budget")
	}
	assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
}

func TestPostgresOnboardingControlledProofChiliSettingsSharedBudgetAndOrdinaryWindow(t *testing.T) {
	f := newOnboardingProofPG(t, "lajiaoliao")
	f.provePrivate(t)
	window, err := f.bindings.CreateControlledProofWindow(f.f.ctx, f.ref, f.challenge, f.input(t, "group", "synthetic-settings-proof"))
	if err != nil {
		t.Fatal(err)
	}
	req := SettingRequest{Account: f.claimant, Workspace: "lajiaoliao", Key: "synthetic-proof-send",
		Epoch: window.AccountEpoch, Version: 1, TestWindowID: window.ID, Digest: strings.Repeat("b", 64),
		ReceiverOwner: "synthetic-native-owner", ReceiverRevision: 1,
		Change: nativeauth.SettingChange{Kind: "send_text", Group: f.nativeID, Text: f.groupText}}
	writes := 0
	port := func(ctx context.Context, fence SettingFence) error {
		var credential []byte
		if err := f.f.db.Platform.QueryRow(ctx, `SELECT credential_sealed FROM accounts WHERE id=$1`, req.Account).
			Scan(&credential); err != nil {
			return err
		}
		proof := SettingEvidence{UID: f.sender, Role: "admin", Digest: req.Digest, Epoch: req.Epoch,
			Version: req.Version, ObservedAt: time.Now(), ModerationAllowed: true}
		copy(proof.CredentialDigest[:], security.Digest(string(credential)))
		return fence(ctx, proof, func(context.Context) error { writes++; return nil })
	}
	op, err := f.native.ExecuteTestChiliText(f.f.ctx, f.ref, req, port)
	if err != nil || op.Status != "succeeded" || writes != 1 {
		t.Fatal("exact applicant proof could not use its independent one-attempt window", op, err)
	}
	assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
	again, err := f.native.ExecuteTestChiliText(f.f.ctx, f.ref, req, port)
	if err != nil || again.ID != op.ID || writes != 1 {
		t.Fatal("duplicate confirmation replayed a proof send", again, err)
	}
	if f.f.count(`SELECT count(*) FROM test_windows WHERE id=$1 AND used=1 AND budget=1`, window.ID) != 1 ||
		f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1 AND proof_event_id IS NULL`, f.challenge) != 1 {
		t.Fatal("platform success created receive evidence or changed the singleton limit")
	}
	req.Key = "synthetic-proof-second-send"
	if _, err = f.native.ExecuteTestChiliText(f.f.ctx, f.ref, req, port); err == nil || writes != 1 {
		t.Fatal("consumed proof window permitted a new send")
	}
	ordinary := NewID()
	epochs, _ := json.Marshal(map[string]int64{f.claimant: req.Epoch})
	f.f.exec(`INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,account_epochs,
		expires_at,budget,admission_mode) VALUES($1,$2,$3,ARRAY[$4::uuid],ARRAY[$5],ARRAY['send_text'],$6,
		now()+interval '300 seconds',1,'native_debug')`,
		ordinary, f.actor.ID, f.scope.GroupID, f.claimant, f.sender, epochs)
	req.Key, req.TestWindowID = "synthetic-ordinary-cannot-bypass", ordinary
	if _, err = f.native.ExecuteTestChiliText(f.f.ctx, f.ref, req, port); err == nil || writes != 1 {
		t.Fatal("ordinary test window borrowed proof's source/ownership exception")
	}
	assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
}

// Only this isolated fixture seeds protocol records. The production window
// path cannot create a conversation or turn a contact identifier into an event.
func (f *onboardingProofPG) privateConversation(t *testing.T, peer string) (string, int64) {
	t.Helper()
	key := NewID()
	seal := func(aad string, value any) []byte {
		t.Helper()
		plain, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		defer clear(plain)
		sealed, err := f.bindings.Vault.Seal(aad, plain)
		if err != nil {
			t.Fatal(err)
		}
		return sealed
	}
	var source, projection []byte
	var conversation string
	if f.scope.Platform == "wsl" {
		selfID, err := strconv.ParseUint(f.sender, 10, 30)
		if err != nil {
			t.Fatal(err)
		}
		peerID, err := strconv.ParseUint(peer, 10, 30)
		if err != nil {
			t.Fatal(err)
		}
		var deployment int64
		if err = f.f.db.Platform.QueryRow(f.f.ctx, `SELECT version FROM native_deployment WHERE singleton`).
			Scan(&deployment); err != nil {
			t.Fatal(err)
		}
		credential := seal("native-session:"+f.claimant, map[string]any{
			"deployment_version": deployment,
			"session":            map[string]any{"uid": selfID, "nim_id": "synthetic-proof-self"},
		})
		f.f.exec(`UPDATE accounts SET credential_sealed=$2 WHERE id=$1`, f.claimant, credential)
		message := nativewire.ApplicationMessage{Sender: nativewire.MessageSource{ID: peerID},
			Target: nativewire.MessageSource{ID: selfID}, Session: 1, Format: 1,
			ClientID: key, Text: "synthetic retained conversation"}
		peerTransport := "synthetic-proof-peer-" + peer
		source = seal("native-event:"+f.claimant+":"+key,
			nativewire.IMMessage{Scene: 0, Sender: peerTransport, Target: "synthetic-proof-self"})
		plain, _ := json.Marshal(map[string]any{"message": message, "transport_peer": peerTransport,
			"transport_to": "synthetic-proof-self", "native_format": "100"})
		projection = plain
		conversation = nativewire.PrivateConversationKey(nativewire.PrivateTarget{ID: peerID, Transport: peerTransport})
	} else {
		plain, err := proto.Marshal(&sdkws.MsgData{ClientMsgID: key, ServerMsgID: NewID(),
			SendID: peer, RecvID: f.sender, SessionType: 1, MsgFrom: 100, ContentType: 101,
			Content: []byte(`{"content":"synthetic retained conversation"}`)})
		if err != nil {
			t.Fatal(err)
		}
		source, err = f.bindings.Vault.Seal("native-event:"+f.claimant+":"+key, plain)
		clear(plain)
		if err != nil {
			t.Fatal(err)
		}
		conversation = controlledProofDigest([]byte("synthetic-private:" + f.sender + ":" + peer))
		projection = []byte(`{"text":"synthetic retained conversation"}`)
	}
	defer clear(projection)
	var event int64
	if err := f.f.db.Platform.QueryRow(f.f.ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
		VALUES($1,$2,'private',clock_timestamp(),$3) RETURNING id`, f.claimant, key, source).Scan(&event); err != nil {
		t.Fatal(err)
	}
	sealed, err := f.bindings.Vault.Seal(security.DecodedMessageAAD(f.claimant, event), projection)
	if err != nil {
		t.Fatal(err)
	}
	f.f.exec(`INSERT INTO native_decoded_events(event_id,account_id,state,scene,conversation_key,
		deployment_version,codec_version,payload_sealed,received_at,decoded_at)
		VALUES($1,$2,'decoded','private',$3,1,7,$4,clock_timestamp(),clock_timestamp())`,
		event, f.claimant, conversation, sealed)
	return conversation, event
}

func TestPostgresOnboardingControlledProofPrivateRetainedTarget(t *testing.T) {
	for _, platform := range []string{"wsl", "lajiaoliao"} {
		t.Run(platform, func(t *testing.T) {
			f := newOnboardingProofPG(t, platform)
			window, err := f.bindings.CreateControlledProofWindow(f.f.ctx, f.ref, f.challenge,
				f.input(t, "private", "synthetic-retained-private-proof"))
			if err != nil {
				t.Fatal(err)
			}
			text := &NativeTextService{DB: f.f.db, Vault: f.bindings.Vault}
			resolve := func(conversation string) (string, int64, error) {
				if platform == "lajiaoliao" {
					return text.ChiliPrivateConversation(f.f.ctx, f.ref, f.claimant, conversation)
				}
				peer, event, err := text.PrivateConversation(f.f.ctx, f.ref, f.claimant, conversation)
				return strconv.FormatUint(peer.ID, 10), event, err
			}
			if _, _, err = resolve(strings.Repeat("f", 64)); err == nil {
				t.Fatal("missing conversation manufactured a private event")
			}
			var receiver string
			if err = f.f.db.Platform.QueryRow(f.f.ctx, `SELECT native_uid FROM accounts WHERE id=$1`,
				f.scope.AccountID).Scan(&receiver); err != nil {
				t.Fatal(err)
			}
			conversation, event := f.privateConversation(t, receiver)
			peer, actual, err := resolve(conversation)
			if err != nil || peer != receiver || actual != event {
				t.Fatal("retained private source did not resolve the real receiver and event", peer, actual, err)
			}
			req := TextRequest{AccountID: f.claimant, GroupID: window.GroupID, TestWindowID: window.ID,
				AccountEpoch: window.AccountEpoch, BindingEpoch: window.BindingEpoch,
				ReceiverOwner: "synthetic-native-owner", ReceiverRevision: 1,
				IdempotencyKey: "synthetic-private-evidence", Text: f.privateText,
				PrivateConversation: conversation, PrivateEventID: actual}
			authorize := func(req TextRequest) (textScope, error) {
				tx, err := f.f.db.Platform.Begin(f.f.ctx)
				if err != nil {
					return textScope{}, err
				}
				defer tx.Rollback(f.f.ctx)
				return text.authorizeSendPlatform(f.f.ctx, tx, f.ref, req, false, platform)
			}
			scope, err := authorize(req)
			if err != nil || scope.ProofTarget != receiver || len(scope.privateDigest) == 0 {
				t.Fatal("proof did not preserve the retained private target fence", err)
			}
			wrong := req
			wrong.PrivateEventID++
			if _, err = authorize(wrong); err == nil {
				t.Fatal("nonexistent private event authorized proof")
			}
			wrong.PrivateConversation, wrong.PrivateEventID = f.privateConversation(t, "900000")
			if _, err = authorize(wrong); err == nil {
				t.Fatal("another retained private contact borrowed the exact receiver proof grant")
			}
			f.f.exec(`UPDATE native_received_events SET received_at=clock_timestamp()-interval '8 days' WHERE id=$1`, event)
			f.f.exec(`UPDATE native_decoded_events SET received_at=clock_timestamp()-interval '8 days' WHERE event_id=$1`, event)
			if _, err = authorize(req); err == nil {
				t.Fatal("expired retained private event authorized proof")
			}
			if f.f.count(`SELECT count(*) FROM operations`) != 0 ||
				f.f.count(`SELECT count(*) FROM native_setting_operations`) != 0 ||
				f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1 AND identity_sealed IS NOT NULL`,
					f.challenge) != 0 {
				t.Fatal("private evidence resolution submitted or established identity")
			}
			assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
		})
	}
}
