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
)

func proofMessagesDeleteNotice(t *testing.T, f *onboardingClaimantFixture, path string) int64 {
	t.Helper()
	key := control.NewID()
	raw, err := proto.Marshal(&sdkws.MsgData{ClientMsgID: key, SendID: f.nativeClaimant,
		RecvID: f.outbound.target.NativeID, SessionType: 1, MsgFrom: 200, ContentType: 1205,
		Content: []byte(`{"notice":"synthetic delete"}`)})
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := f.vault.Seal("native-event:"+f.claimant+":"+key, raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	var event int64
	if err = f.db.Platform.QueryRow(context.Background(), `INSERT INTO native_received_events
		(account_id,event_key,kind,received_at,payload_sealed)
		VALUES($1,$2,'private',clock_timestamp(),$3) RETURNING id`, f.claimant, key, sealed).Scan(&event); err != nil {
		t.Fatal(err)
	}
	conversation := strings.TrimPrefix(path, "/accounts/"+f.claimant+"/conversations/")
	proofMessagesExec(t, f, `INSERT INTO native_decoded_events(event_id,account_id,state,reason,scene,conversation_key,
		deployment_version,codec_version,received_at,decoded_at)
		VALUES($1,$2,'unsupported','lajiaoliao_content_type','private',$3,1,7,clock_timestamp(),clock_timestamp())`,
		event, f.claimant, conversation)
	return event
}

func TestOnboardingControlledProofPostgresMessagesPrivateAfterDeleteNotice(t *testing.T) {
	f := newOnboardingClaimantFixture(t, "lajiaoliao", "owner")
	port := &syntheticChiliMediaPort{}
	f.app.chiliMediaTestPort = port
	window := proofMessagesWindow(t, f, f.challenge, "private")
	path, retained := proofMessagesConversation(t, f, "private", f.outbound.target.NativeID)
	latest := proofMessagesDeleteNotice(t, f, path)
	view := proofMessagesRead(t, f.admin, path)
	if !view.Enabled || len(view.Windows) != 1 || view.Windows[0].ID != window.ID ||
		view.Windows[0].ProofKind != "private" || view.Windows[0].Remaining != 1 {
		t.Fatal("latest delete notice hid or broadened the exact private proof window", view)
	}
	operation := proofMessagesStage(t, f, path, f.challenge.PrivateChallenge,
		"synthetic-private-route-after-delete", window)
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
	if err != nil || request.PrivateEventID != retained || request.PrivateEventID == latest ||
		len(request.PrivateDigest) != 32 || request.ProofWindowID != window.ID {
		t.Fatal("HTTP did not seal the older valid event and exact proof fence", err)
	}
	if port.writes != 0 || f.readChallenge(t, f.challenge).IdentityProven {
		t.Fatal("discovery or staging manufactured a native effect or received proof")
	}
}

func TestOnboardingControlledProofPostgresDeleteNoticeCannotSupplyPrivateRoute(t *testing.T) {
	f := newOnboardingClaimantFixture(t, "lajiaoliao", "owner")
	port := &syntheticChiliMediaPort{}
	f.app.chiliMediaTestPort = port
	window := proofMessagesWindow(t, f, f.challenge, "private")
	path, retained := proofMessagesConversation(t, f, "private", f.outbound.target.NativeID)
	proofMessagesDeleteNotice(t, f, path)
	proofMessagesExec(t, f, `UPDATE native_received_events SET received_at=clock_timestamp()-interval '8 days'
		WHERE id=$1`, retained)
	onboardingAPI(t, f.admin, http.MethodPost, path+"/text-operations", "synthetic-delete-is-not-private-proof",
		map[string]any{"text": f.challenge.PrivateChallenge, "test_window_id": window.ID,
			"account_epoch": window.AccountEpoch, "binding_epoch": window.BindingEpoch}, http.StatusConflict)
	var count, used int
	if err := f.db.Platform.QueryRow(context.Background(), `SELECT
		(SELECT count(*) FROM operations WHERE account_id=$1),
		(SELECT used FROM test_windows WHERE id=$2)`, f.claimant, window.ID).Scan(&count, &used); err != nil {
		t.Fatal(err)
	}
	if count != 0 || used != 0 || port.writes != 0 || f.readChallenge(t, f.challenge).IdentityProven {
		t.Fatal("delete notification created an operation, spent an attempt or established proof")
	}
}
