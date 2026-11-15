package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/control"
	"wangshangliao/internal/gateway"
)

type proofMessagesView struct {
	Enabled bool                  `json:"enabled"`
	Scope   conversationSendScope `json:"scope"`
	Windows []struct {
		ID           string   `json:"id"`
		Remaining    int      `json:"remaining"`
		BindingEpoch int64    `json:"binding_epoch"`
		Actions      []string `json:"actions"`
		ProofKind    string   `json:"proof_kind"`
	} `json:"test_windows"`
}

func proofMessagesExec(t *testing.T, f *onboardingClaimantFixture, query string, args ...any) {
	t.Helper()
	if _, err := f.db.Platform.Exec(context.Background(), query, args...); err != nil {
		t.Fatal(err)
	}
}

func proofMessagesReady(t *testing.T, f *onboardingClaimantFixture) {
	t.Helper()
	proofMessagesExec(t, f, `UPDATE account_receivers SET observed_at=clock_timestamp(),
		snapshot=jsonb_build_object('state','connected','heartbeat_at',clock_timestamp())
		WHERE account_id IN ($1,$2)`, f.claimant, f.outbound.recipient)
}

func proofMessagesWindow(t *testing.T, f *onboardingClaimantFixture, c control.GroupBindingChallenge, kind string) control.OnboardingControlledProofWindow {
	t.Helper()
	text := f.challenge.PrivateChallenge
	if kind == "group" {
		text = f.challenge.GroupChallenge
	}
	proofMessagesReady(t, f)
	raw := onboardingAPI(t, f.admin, http.MethodPost,
		"/test-tools/onboarding-friend/challenges/"+c.ID+"/proof-windows", "synthetic-messages-proof-"+kind,
		map[string]any{"version": c.Version, "kind": kind, "text": text, "seconds": 300}, http.StatusCreated)
	var out control.OnboardingControlledProofWindow
	if err := json.Unmarshal(raw, &out); err != nil || out.AccountID != f.claimant ||
		out.ID == "" || out.Remaining != 1 {
		t.Fatal("proof preparation did not preserve the applicant", out, err)
	}
	return out
}

// These isolated protocol fixtures model retained messages only. Window
// discovery cannot synthesize a private target or establish an identity proof.
func proofMessagesConversation(t *testing.T, f *onboardingClaimantFixture, scene, peer string) (string, int64) {
	t.Helper()
	key := control.NewID()
	conversation := strings.ReplaceAll(control.NewID()+control.NewID(), "-", "")
	message := &sdkws.MsgData{ClientMsgID: key, ServerMsgID: control.NewID(),
		SendID: peer, RecvID: f.nativeClaimant, SessionType: 1, MsgFrom: 100,
		ContentType: 102, Content: []byte(`{"sourcePicture":{"url":"https://lj-im-chat-01.lajiaoliao.com/synthetic.png"}}`)}
	state, reason := "unsupported", "lajiaoliao_content_type"
	if scene == "group" {
		message.RecvID, message.GroupID, message.SessionType = f.nativeGroup, f.nativeGroup, 3
		message.ContentType, message.Content = 101, []byte(`{"content":"synthetic retained group"}`)
		state, reason = "decoded", ""
	}
	raw, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	source, err := f.vault.Seal("native-event:"+f.claimant+":"+key, raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	var event int64
	if err = f.db.Platform.QueryRow(context.Background(), `INSERT INTO native_received_events
		(account_id,event_key,kind,received_at,payload_sealed)
		VALUES($1,$2,$3,clock_timestamp()-interval '2 days',$4) RETURNING id`,
		f.claimant, key, scene, source).Scan(&event); err != nil {
		t.Fatal(err)
	}
	raw, err = json.Marshal(gateway.DecodedMessage{Platform: "lajiaoliao", NativeTarget: f.nativeGroup})
	if err != nil {
		t.Fatal(err)
	}
	projection, err := f.vault.Seal(gateway.DecodedAAD(f.claimant, event), raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	proofMessagesExec(t, f, `INSERT INTO native_decoded_events(event_id,account_id,state,reason,scene,conversation_key,
		payload_sealed,deployment_version,codec_version,received_at,decoded_at)
		VALUES($1,$2,$3,$4,$5,$6,$7,1,7,clock_timestamp()-interval '2 days',clock_timestamp())`,
		event, f.claimant, state, reason, scene, conversation, projection)
	return "/accounts/" + f.claimant + "/conversations/" + conversation, event
}

func proofMessagesRead(t *testing.T, client testClient, path string) proofMessagesView {
	t.Helper()
	raw := onboardingAPI(t, client, http.MethodGet, path+"/sending", "", nil, http.StatusOK)
	var out proofMessagesView
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func proofMessagesOrdinaryWindow(t *testing.T, f *onboardingClaimantFixture, forged bool) {
	t.Helper()
	targets := []string{f.nativeGroup}
	if forged {
		targets = append(targets, "onboarding_proof:"+control.NewID()+":group")
	}
	proofMessagesExec(t, f, `INSERT INTO test_windows(actor_id,group_id,account_ids,target_ids,actions,
		account_epochs,expires_at,budget,admission_mode)
		SELECT $1,$2,ARRAY[a.id],$3,ARRAY['send_text'],
		jsonb_build_object(a.id::text,a.epoch,r.id::text,r.epoch),
		now()+interval '5 minutes',1,'native_debug'
		FROM accounts a JOIN accounts r ON r.id=$5 WHERE a.id=$4`,
		f.adminID, f.group, targets, f.claimant, f.outbound.recipient)
}

func proofMessagesStage(t *testing.T, f *onboardingClaimantFixture, path, text, key string,
	window control.OnboardingControlledProofWindow) control.TextOperation {
	t.Helper()
	raw := onboardingAPI(t, f.admin, http.MethodPost, path+"/text-operations", key,
		map[string]any{"text": text, "test_window_id": window.ID, "account_epoch": window.AccountEpoch,
			"binding_epoch": window.BindingEpoch}, http.StatusCreated)
	var out control.TextOperation
	if err := json.Unmarshal(raw, &out); err != nil || out.ID == "" || out.Status != "queued" {
		t.Fatal("Messages did not stage its existing durable text operation", out, err)
	}
	return out
}

func TestOnboardingControlledProofPostgresMessagesPrivate(t *testing.T) {
	f := newOnboardingClaimantFixture(t, "lajiaoliao", "owner")
	port := &syntheticChiliMediaPort{}
	f.app.chiliMediaTestPort = port
	window := proofMessagesWindow(t, f, f.challenge, "private")
	path, event := proofMessagesConversation(t, f, "private", f.outbound.target.NativeID)
	view := proofMessagesRead(t, f.admin, path)
	if !view.Enabled || view.Scope.Scene != "private" || len(view.Windows) != 1 ||
		view.Windows[0].ID != window.ID || view.Windows[0].Remaining != 1 ||
		view.Windows[0].ProofKind != "private" ||
		strings.Join(view.Windows[0].Actions, ",") != "send_private_text" {
		t.Fatal("exact private proof window was hidden or broadened", view)
	}
	otherPath, _ := proofMessagesConversation(t, f, "private", "synthetic-unselected-peer")
	if other := proofMessagesRead(t, f.admin, otherPath); other.Enabled || len(other.Windows) != 0 {
		t.Fatal("private proof window crossed its retained peer", other)
	}
	input := map[string]any{"text": "arbitrary text", "test_window_id": window.ID,
		"account_epoch": window.AccountEpoch, "binding_epoch": window.BindingEpoch}
	onboardingAPI(t, f.admin, http.MethodPost, path+"/text-operations", "synthetic-proof-wrong-text", input, http.StatusConflict)
	operation := proofMessagesStage(t, f, path, f.challenge.PrivateChallenge, "synthetic-messages-private", window)
	var sealed []byte
	if err := f.db.Platform.QueryRow(context.Background(), `SELECT payload_sealed FROM operations WHERE id=$1`,
		operation.ID).Scan(&sealed); err != nil {
		t.Fatal(err)
	}
	raw, err := f.vault.Open("lajiaoliao-media-intent:"+operation.ID, sealed)
	if err != nil {
		t.Fatal(err)
	}
	var request control.TextRequest
	err = json.Unmarshal(raw, &request)
	clear(raw)
	if err != nil || request.PrivateEventID != event ||
		request.PrivateConversation != strings.TrimPrefix(path, "/accounts/"+f.claimant+"/conversations/") {
		t.Fatal("HTTP fabricated or dropped its retained private event", err)
	}
	onboardingAPI(t, f.admin, http.MethodPost, path+"/text-operations/"+operation.ID+"/dispatch",
		"synthetic-messages-private-dispatch", map[string]any{}, http.StatusOK)
	if port.writes != 1 || port.dispatch.Peer != f.outbound.target.NativeID ||
		port.dispatch.Message.Text != f.challenge.PrivateChallenge {
		t.Fatal("private proof did not use the real retained receiver", port.writes, port.dispatch.Peer)
	}
	if c := f.readChallenge(t, f.challenge); c.IdentityProven {
		t.Fatal("preparing or submitting applicant proof manufactured its receiving event")
	}
	view = proofMessagesRead(t, f.admin, path)
	if view.Enabled || len(view.Windows) != 0 {
		t.Fatal("consumed singleton private proof remained selectable", view)
	}
}

func TestOnboardingControlledProofPostgresMessagesGroup(t *testing.T) {
	f := newOnboardingClaimantFixture(t, "lajiaoliao", "owner")
	port := &syntheticChiliMediaPort{}
	f.app.chiliMediaTestPort = port
	f.scan(t, control.OnboardingMessage{Sender: f.nativeClaimant, Private: true, Text: f.challenge.PrivateChallenge})
	c := f.readChallenge(t, f.challenge)
	if !c.IdentityProven {
		t.Fatal("ordinary private receive consumer did not establish the selected identity")
	}
	path, _ := proofMessagesConversation(t, f, "group", f.outbound.target.NativeID)
	proofMessagesOrdinaryWindow(t, f, false)
	proofMessagesOrdinaryWindow(t, f, true)
	if view := proofMessagesRead(t, f.admin, path); view.Enabled {
		t.Fatal("an ordinary or forged window admitted a non-primary Chili sender")
	}
	window := proofMessagesWindow(t, f, c, "group")
	view := proofMessagesRead(t, f.admin, path)
	if !view.Enabled || view.Scope.GroupID != f.group || len(view.Windows) != 1 ||
		view.Windows[0].ProofKind != "group" ||
		view.Windows[0].ID != window.ID || strings.Join(view.Windows[0].Actions, ",") != "send_text" {
		t.Fatal("exact non-primary Chili group proof was not selectable", view)
	}
	choices := onboardingAPI(t, f.admin, http.MethodGet, path+"/sending-targets?window="+window.ID,
		"", nil, http.StatusOK)
	if string(choices) != `{"members":[],"quotes":[]}` {
		t.Fatal("pure proof exposed quote or mention candidates", string(choices))
	}
	otherAdmin := controlledOnboardingOtherAdmin(t, f.controlledOnboardingFixture)
	if other := proofMessagesRead(t, otherAdmin, path); other.Enabled {
		t.Fatal("another operator borrowed the applicant proof window")
	}
	operation := proofMessagesStage(t, f, path, f.challenge.GroupChallenge, "synthetic-messages-group", window)
	onboardingAPI(t, f.admin, http.MethodPost, path+"/text-operations/"+operation.ID+"/dispatch",
		"synthetic-messages-group-dispatch", map[string]any{}, http.StatusOK)
	if port.writes != 1 || port.dispatch.Group != f.nativeGroup || port.dispatch.Peer != "" ||
		port.dispatch.Message.Text != f.challenge.GroupChallenge {
		t.Fatal("group proof changed its fixed destination", port.writes, port.dispatch.Group)
	}
	if got := f.readChallenge(t, c); got.CanConfirm {
		t.Fatal("outgoing send was confused with the independently received group proof")
	}
	if view := proofMessagesRead(t, f.admin, path); view.Enabled || len(view.Windows) != 0 {
		t.Fatal("consumed singleton group proof remained selectable", view)
	}
	proofMessagesExec(t, f, `UPDATE test_windows SET cancelled=true WHERE id=$1`, window.ID)
	if view := proofMessagesRead(t, f.admin, path); view.Enabled {
		t.Fatal("closed group proof still resolved a non-primary Chili sender")
	}
}
