package control

import (
	"bytes"
	"strconv"
	"testing"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

func (f *chiliNoticeProofFixture) retainedRoute(t *testing.T, account, conversation string, kind int32) int64 {
	t.Helper()
	key := NewID()
	message := &sdkws.MsgData{ClientMsgID: key, SendID: f.sender, RecvID: f.recipient,
		MsgFrom: 200, SessionType: 1, ContentType: kind, Content: []byte(`{"notice":"synthetic"}`)}
	state, reason := "unsupported", "lajiaoliao_content_type"
	if kind == 101 {
		message.MsgFrom, message.Content = 100, []byte(`{"content":"synthetic retained route"}`)
		state, reason = "decoded", ""
	} else if kind == 102 {
		message.MsgFrom, message.Content = 100,
			[]byte(`{"sourcePicture":{"url":"https://lj-im-chat-01.lajiaoliao.com/synthetic.png"}}`)
	}
	raw, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := f.bindings.Vault.Seal("native-event:"+account+":"+key, raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	var event int64
	if err = f.f.db.Platform.QueryRow(f.f.ctx, `INSERT INTO native_received_events
		(account_id,event_key,kind,received_at,payload_sealed)
		VALUES($1,$2,'private',clock_timestamp(),$3) RETURNING id`, account, key, sealed).Scan(&event); err != nil {
		t.Fatal(err)
	}
	f.f.exec(`INSERT INTO native_decoded_events(event_id,account_id,state,reason,scene,conversation_key,
		deployment_version,codec_version,received_at,decoded_at)
		VALUES($1,$2,$3,$4,'private',$5,1,7,clock_timestamp(),clock_timestamp())`,
		event, account, state, reason, conversation)
	return event
}

// Run this unchanged with the v129 chili_message.go overlay for the behavioral
// red control: LIMIT 1 returns native_private_message_invalid for each case.
func TestPostgresChiliPrivateRouteRecoveryAfterDelete(t *testing.T) {
	for _, kind := range []int32{101, 102, 1201, 1204} {
		t.Run(strconv.Itoa(int(kind)), func(t *testing.T) {
			f := newChiliNoticeProofFixture(t)
			old, _, _ := f.restartChallenge(t)
			immutable := f.originalAttempt(t, old)
			want := f.retainedRoute(t, f.claimant, f.req.PrivateConversation, kind)
			latest := f.retainedRoute(t, f.claimant, f.req.PrivateConversation, 1205)
			peer, event, err := f.text.ChiliPrivateConversation(f.f.ctx, f.ref, f.claimant, f.req.PrivateConversation)
			if err != nil || peer != f.recipient || event != want || event == latest {
				t.Fatal("a later delete notice masked the retained route", kind, event, err)
			}
			if !bytes.Equal(immutable, f.originalAttempt(t, old)) ||
				f.f.count(`SELECT count(*) FROM operations`) != 1 ||
				f.f.count(`SELECT used FROM test_windows WHERE id=$1`, f.window.ID) != 0 ||
				f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1
					AND (identity_event_id IS NOT NULL OR identity_sealed IS NOT NULL OR proof_event_id IS NOT NULL)`,
					f.challenge) != 0 {
				t.Fatal("route discovery changed the old unknown, consumed authorization or manufactured proof")
			}
		})
	}
}

func TestPostgresChiliPrivateRouteRecoveryBoundaries(t *testing.T) {
	for _, mode := range []string{"only-delete", "other-account", "other-conversation", "expired-source",
		"bad-seal", "candidate-limit", "cancelled-proof"} {
		t.Run(mode, func(t *testing.T) {
			f := newChiliNoticeProofFixture(t)
			switch mode {
			case "other-account":
				f.retainedRoute(t, f.scope.AccountID, f.req.PrivateConversation, 101)
			case "other-conversation":
				f.retainedRoute(t, f.claimant, controlledProofDigest([]byte("unrelated conversation")), 101)
			case "expired-source":
				f.f.exec(`UPDATE native_received_events SET received_at=clock_timestamp()-interval '8 days' WHERE id=$1`,
					f.req.PrivateEventID)
			case "bad-seal":
				event := f.retainedRoute(t, f.claimant, f.req.PrivateConversation, 1205)
				f.f.exec(`UPDATE native_received_events SET payload_sealed='synthetic-invalid-seal'::bytea WHERE id=$1`, event)
			case "candidate-limit":
				for range 32 {
					f.retainedRoute(t, f.claimant, f.req.PrivateConversation, 1205)
				}
			case "cancelled-proof":
				f.f.exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, f.window.ID)
				f.retainedRoute(t, f.claimant, f.req.PrivateConversation, 1205)
			}
			if mode == "only-delete" || mode == "other-account" || mode == "other-conversation" {
				f.replaceNotice(t, f.notice(t, 1205, false))
			}
			peer, event, err := f.text.ChiliPrivateConversation(f.f.ctx, f.ref, f.claimant, f.req.PrivateConversation)
			if err == nil || peer != "" || event != 0 {
				t.Fatal("invalid or unrelated candidates supplied a route", peer, event)
			}
			if f.f.count(`SELECT count(*) FROM operations`) != 0 ||
				f.f.count(`SELECT used FROM test_windows WHERE id=$1`, f.window.ID) != 0 ||
				f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1
					AND (identity_event_id IS NOT NULL OR identity_sealed IS NOT NULL OR proof_event_id IS NOT NULL)`,
					f.challenge) != 0 {
				t.Fatal("rejected discovery submitted work or manufactured proof")
			}
		})
	}
}

func TestPostgresChiliRecoveredRouteKeepsFinalDigestFence(t *testing.T) {
	for _, changed := range []bool{false, true} {
		f := newChiliNoticeProofFixture(t)
		f.retainedRoute(t, f.claimant, f.req.PrivateConversation, 1205)
		_, event, err := f.text.ChiliPrivateConversation(f.f.ctx, f.ref, f.claimant, f.req.PrivateConversation)
		if err != nil || event != f.req.PrivateEventID {
			t.Fatal("fixture did not recover the original notice route", event, err)
		}
		op, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req)
		if err != nil {
			t.Fatal(err)
		}
		port := &chiliNoticeProofPort{t: t, peer: f.recipient, eventID: event}
		port.before = func() {
			f.retainedRoute(t, f.claimant, f.req.PrivateConversation, 1205)
			if changed {
				f.replaceNotice(t, f.notice(t, 1204, true))
			}
		}
		result, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port)
		wantStatus, wantWrites := "succeeded", 1
		if changed {
			wantStatus, wantWrites = "failed", 0
		}
		if err != nil || result.Status != wantStatus || port.writes != wantWrites {
			t.Fatal("recovered route escaped the exact final source fence", result, port.writes, err)
		}
		if f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1
			AND (identity_event_id IS NOT NULL OR identity_sealed IS NOT NULL OR proof_event_id IS NOT NULL)`, f.challenge) != 0 {
			t.Fatal("synthetic dispatch manufactured receive proof")
		}
	}
}
