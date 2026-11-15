package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativewire"
)

// These fixtures inherit the serial wsl_test guard. A seeded synthetic notice
// supplies routing metadata only, never identity or challenge proof.
type chiliNoticeProofFixture struct {
	*onboardingProofPG
	text           *NativeTextService
	window         OnboardingControlledProofWindow
	req            TextRequest
	recipient, key string
}

func newChiliNoticeProofFixture(t *testing.T) *chiliNoticeProofFixture {
	t.Helper()
	return newChiliNoticeProofBindingFixture(t, false)
}

func newChiliNoticeProofBindingFixture(t *testing.T, legacy bool) *chiliNoticeProofFixture {
	t.Helper()
	f := newOnboardingProofPG(t, "lajiaoliao")
	if legacy {
		f.f.exec(`DELETE FROM group_binding_ownership WHERE group_id=$1`, f.scope.GroupID)
		f.f.exec(`UPDATE group_bindings SET tenant_id=(SELECT tenant_id FROM group_bindings WHERE group_id=$2),
			primary_account_id=$3 WHERE group_id=$1`, f.scope.GroupID, f.customerScope.GroupID, f.claimant)
	}
	window, err := f.bindings.CreateControlledProofWindow(f.f.ctx, f.ref, f.challenge,
		f.input(t, "private", "synthetic-notice-private-window"))
	if err != nil {
		t.Fatal(err)
	}
	n := &chiliNoticeProofFixture{onboardingProofPG: f, window: window,
		text: &NativeTextService{DB: f.f.db, Vault: f.bindings.Vault}, key: NewID()}
	if err = f.f.db.Platform.QueryRow(f.f.ctx, `SELECT native_uid FROM accounts WHERE id=$1`,
		f.scope.AccountID).Scan(&n.recipient); err != nil {
		t.Fatal(err)
	}
	n.req = TextRequest{AccountID: f.claimant, GroupID: window.GroupID, TestWindowID: window.ID,
		AccountEpoch: window.AccountEpoch, BindingEpoch: window.BindingEpoch,
		ReceiverOwner: "synthetic-native-owner", ReceiverRevision: 1,
		IdempotencyKey: "synthetic-notice-private-send", Text: f.privateText,
		PrivateConversation: controlledProofDigest([]byte("lajiaoliao:private\x000\x00" + n.recipient))}
	raw := n.notice(t, 1201, false)
	sealed, err := f.bindings.Vault.Seal("native-event:"+f.claimant+":"+n.key, raw)
	clear(raw)
	if err != nil {
		t.Fatal(err)
	}
	if err = f.f.db.Platform.QueryRow(f.f.ctx, `INSERT INTO native_received_events(account_id,event_key,kind,received_at,payload_sealed)
		VALUES($1,$2,'private',clock_timestamp(),$3) RETURNING id`, f.claimant, n.key, sealed).
		Scan(&n.req.PrivateEventID); err != nil {
		t.Fatal(err)
	}
	f.f.exec(`INSERT INTO native_decoded_events(event_id,account_id,state,reason,scene,conversation_key,
		deployment_version,codec_version,received_at,decoded_at)
		VALUES($1,$2,'unsupported','lajiaoliao_content_type','private',$3,1,7,clock_timestamp(),clock_timestamp())`,
		n.req.PrivateEventID, f.claimant, n.req.PrivateConversation)
	return n
}

func (f *chiliNoticeProofFixture) notice(t *testing.T, kind int32, reverse bool) []byte {
	t.Helper()
	m := &sdkws.MsgData{ClientMsgID: f.key, SendID: f.sender, RecvID: f.recipient,
		MsgFrom: 200, SessionType: 1, ContentType: kind, Content: []byte(`{"notice":"synthetic"}`)}
	if reverse {
		m.SendID, m.RecvID = m.RecvID, m.SendID
	}
	raw, err := proto.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func (f *chiliNoticeProofFixture) replaceNotice(t *testing.T, raw []byte) {
	t.Helper()
	sealed, err := f.bindings.Vault.Seal("native-event:"+f.claimant+":"+f.key, raw)
	if err != nil {
		t.Fatal(err)
	}
	f.f.exec(`UPDATE native_received_events SET payload_sealed=$2 WHERE id=$1`, f.req.PrivateEventID, sealed)
}

func (f *chiliNoticeProofFixture) target(t *testing.T, req TextRequest, actor string, reserved bool) (string, []byte, error) {
	t.Helper()
	tx, err := f.f.db.Platform.Begin(f.f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(f.f.ctx)
	return f.text.chiliPrivateTargetForSend(f.f.ctx, tx, req, f.sender, actor, reserved)
}

func TestPostgresChiliPrivateNoticeProofRouteDiscovery(t *testing.T) {
	f := newChiliNoticeProofFixture(t)
	for _, kind := range []int32{1201, 1204} {
		for _, reverse := range []bool{false, true} {
			raw := f.notice(t, kind, reverse)
			f.replaceNotice(t, raw)
			peer, event, err := f.text.ChiliPrivateConversation(f.f.ctx, f.ref, f.claimant, f.req.PrivateConversation)
			if err != nil || peer != f.recipient || event != f.req.PrivateEventID {
				t.Fatal("exact prepared proof could not discover the retained notice route", peer, event, err)
			}
			peer, digest, err := f.target(t, f.req, f.actor.ID, false)
			expected := chiliSourceFingerprint(raw)
			clear(raw)
			if err != nil || peer != f.recipient || !bytes.Equal(digest, expected) {
				t.Fatal("notice routing did not pin its actual source", peer, err)
			}
		}
	}
	tx, err := f.f.db.Platform.Begin(f.f.ctx)
	if err != nil {
		t.Fatal(err)
	}
	_, _, err = f.text.chiliPrivateTarget(f.f.ctx, tx, f.req, f.sender)
	tx.Rollback(f.f.ctx)
	if err == nil {
		t.Fatal("ordinary source resolver accepted a notification")
	}
	_, other := groupBusinessAccessOperator(t, f.assistantBusinessFixture, 119)
	if _, _, err = f.text.ChiliPrivateConversation(f.f.ctx, other, f.claimant, f.req.PrivateConversation); err == nil {
		t.Fatal("another actor discovered a proof-only route")
	}
	if f.f.count(`SELECT count(*) FROM operations`) != 0 ||
		f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1
			AND (identity_event_id IS NOT NULL OR identity_sealed IS NOT NULL OR proof_event_id IS NOT NULL)`, f.challenge) != 0 {
		t.Fatal("route discovery fabricated a send or onboarding proof")
	}
	assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
}

func TestPostgresChiliPrivateNoticeProofRejectsScopeAndSourceSubstitution(t *testing.T) {
	for _, name := range []string{"ordinary-window", "wrong-actor", "wrong-group", "wrong-recipient", "wrong-type",
		"wrong-source-account", "wrong-conversation", "missing-event", "expired-source", "expired-window", "closed-scope", "reused-window"} {
		t.Run(name, func(t *testing.T) {
			f := newChiliNoticeProofFixture(t)
			req, actor := f.req, f.actor.ID
			switch name {
			case "ordinary-window":
				req.TestWindowID = NewID()
				f.f.exec(`INSERT INTO test_windows(id,actor_id,group_id,account_ids,target_ids,actions,account_epochs,
					expires_at,budget,admission_mode)
					SELECT $1,actor_id,group_id,account_ids,ARRAY[$2],actions,account_epochs,expires_at,1,admission_mode
					FROM test_windows WHERE id=$3`, req.TestWindowID, f.recipient, f.window.ID)
				f.f.exec(`UPDATE test_windows SET cancelled=true WHERE id=$1`, f.window.ID)
			case "wrong-actor":
				actor = NewID()
			case "wrong-group":
				req.GroupID = NewID()
			case "wrong-recipient":
				raw, err := proto.Marshal(&sdkws.MsgData{ClientMsgID: f.key, SendID: f.sender, RecvID: "other-peer",
					MsgFrom: 200, SessionType: 1, ContentType: 1201})
				if err != nil {
					t.Fatal(err)
				}
				f.replaceNotice(t, raw)
				req.PrivateConversation = controlledProofDigest([]byte("lajiaoliao:private\x000\x00other-peer"))
				f.f.exec(`UPDATE native_decoded_events SET conversation_key=$2 WHERE event_id=$1`, req.PrivateEventID, req.PrivateConversation)
			case "wrong-type":
				f.replaceNotice(t, f.notice(t, 2200, false))
			case "wrong-source-account":
				raw := f.notice(t, 1201, false)
				sealed, err := f.bindings.Vault.Seal("native-event:"+f.scope.AccountID+":"+f.key, raw)
				clear(raw)
				if err != nil {
					t.Fatal(err)
				}
				f.f.exec(`UPDATE native_received_events SET payload_sealed=$2 WHERE id=$1`, req.PrivateEventID, sealed)
			case "wrong-conversation":
				req.PrivateConversation = controlledProofDigest([]byte("unrelated retained conversation"))
			case "missing-event":
				req.PrivateEventID++
			case "expired-source":
				f.f.exec(`UPDATE native_received_events SET received_at=clock_timestamp()-interval '8 days' WHERE id=$1`, req.PrivateEventID)
				f.f.exec(`UPDATE native_decoded_events SET received_at=clock_timestamp()-interval '8 days' WHERE event_id=$1`, req.PrivateEventID)
			case "expired-window":
				f.f.exec(`UPDATE test_windows SET created_at=clock_timestamp()-interval '10 minutes',
					expires_at=clock_timestamp()-interval '6 minutes' WHERE id=$1`, f.window.ID)
			case "closed-scope":
				f.f.exec(`UPDATE onboarding_test_scopes SET state='closed',version=version+1 WHERE id=$1`, f.scopeID)
			case "reused-window":
				f.f.exec(`UPDATE test_windows SET used=1 WHERE id=$1`, f.window.ID)
			}
			if _, _, err := f.target(t, req, actor, false); err == nil {
				t.Fatal("invalid request borrowed the notification-only route")
			}
			if f.f.count(`SELECT count(*) FROM operations`) != 0 {
				t.Fatal("route rejection created an operation")
			}
			assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
		})
	}
}

type chiliNoticeProofPort struct {
	t       *testing.T
	before  func()
	unknown bool
	noWrite error
	writes  int
	peer    string
	eventID int64
}

func (p *chiliNoticeProofPort) UploadChiliMedia(context.Context, ChiliMediaDispatch,
	func(context.Context) error) (lajiaoliao.MediaAsset, error) {
	p.t.Error("plain-text proof attempted a media upload")
	return lajiaoliao.MediaAsset{}, errors.New("synthetic proof cannot upload media")
}

func (p *chiliNoticeProofPort) SubmitChiliMedia(ctx context.Context, d ChiliMediaDispatch,
	fence nativewire.SubmissionFence) (lajiaoliao.Receipt, error) {
	p.t.Helper()
	if d.Peer != p.peer || d.Request.PrivateEventID != p.eventID || d.Message.Text == "" ||
		d.Message.Media != nil || d.Message.Quote != nil || d.Message.Mention != nil {
		return lajiaoliao.Receipt{}, errors.New("synthetic private proof dispatch escaped its exact route")
	}
	if p.before != nil {
		p.before()
	}
	err := fence(ctx, func(context.Context) error {
		if p.noWrite != nil {
			return p.noWrite
		}
		p.writes++
		return nil
	})
	if err != nil {
		return lajiaoliao.Receipt{}, err
	}
	if p.unknown {
		return lajiaoliao.Receipt{}, errors.New("synthetic receipt lost after final write")
	}
	return lajiaoliao.Receipt{ClientID: d.ClientID, ServerID: "synthetic-notice-proof-receipt", Timestamp: time.Now().UnixMilli()}, nil
}

func TestPostgresChiliPrivateProofOnlyTrustsExplicitUnsubmittedTransportEvidence(t *testing.T) {
	for _, mode := range []string{"not-submitted", "unknown", "contradictory", "untyped"} {
		t.Run(mode, func(t *testing.T) {
			f := newChiliNoticeProofFixture(t)
			op, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req)
			if err != nil {
				t.Fatal(err)
			}
			port := &chiliNoticeProofPort{t: t, peer: f.recipient, eventID: f.req.PrivateEventID}
			switch mode {
			case "not-submitted":
				port.noWrite = &lajiaoliao.Failure{Code: "lajiaoliao_not_submitted"}
			case "unknown":
				port.noWrite = &lajiaoliao.Failure{Code: "lajiaoliao_result_unknown", Submitted: true}
			case "contradictory":
				port.noWrite = &lajiaoliao.Failure{Code: "lajiaoliao_not_submitted", Submitted: true}
			case "untyped":
				port.noWrite = context.DeadlineExceeded
			}
			result, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port)
			if err != nil || port.writes != 0 {
				t.Fatal(result, port.writes, err)
			}
			status, code := "unknown", "native_text_result_unknown"
			if mode == "not-submitted" {
				status, code = "failed", "native_text_not_submitted"
			}
			if result.Status != status || result.ResultCode != code {
				t.Fatal("missing or contradictory write evidence changed submission status:", result)
			}
			again, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port)
			if err != nil || again != result || port.writes != 0 {
				t.Fatal("terminal/unknown dispatch was retried:", again, err)
			}
			if f.f.count(`SELECT count(*) FROM test_windows WHERE id=$1 AND used=1`, f.window.ID) != 1 {
				t.Fatal("failed attempt replenished its budget")
			}
		})
	}
}

func TestPostgresChiliPrivateNoticeProofStagesAndFinalSourceFence(t *testing.T) {
	for _, change := range []string{"none", "source", "scope"} {
		t.Run(change, func(t *testing.T) {
			f := newChiliNoticeProofFixture(t)
			op, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req)
			if err != nil || op.Status != "queued" {
				t.Fatal("retained friend notice could not stage exact plain-text proof", op, err)
			}
			var sealed []byte
			if err = f.f.db.Platform.QueryRow(f.f.ctx, `SELECT payload_sealed FROM operations WHERE id=$1`, op.ID).Scan(&sealed); err != nil {
				t.Fatal(err)
			}
			raw, err := f.bindings.Vault.Open("lajiaoliao-media-intent:"+op.ID, sealed)
			if err != nil {
				t.Fatal(err)
			}
			var staged TextRequest
			err = json.Unmarshal(raw, &staged)
			clear(raw)
			if err != nil || staged.PrivateEventID != f.req.PrivateEventID || len(staged.PrivateDigest) != sha256.Size ||
				staged.ProofWindowID != f.window.ID {
				t.Fatal("staging did not seal the exact proof event, digest and window", err)
			}
			port := &chiliNoticeProofPort{t: t, peer: f.recipient, eventID: f.req.PrivateEventID}
			switch change {
			case "source":
				port.before = func() { f.replaceNotice(t, f.notice(t, 1204, true)) }
			case "scope":
				port.before = func() {
					f.f.exec(`UPDATE onboarding_test_scopes SET state='closed',version=version+1 WHERE id=$1`, f.scopeID)
				}
			}
			result, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port)
			if err != nil {
				t.Fatal(result, err)
			}
			wantStatus, wantWrites := "succeeded", 1
			if change != "none" {
				wantStatus, wantWrites = "failed", 0
			}
			if result.Status != wantStatus || port.writes != wantWrites {
				t.Fatal("final proof/source fence failed", result, port.writes)
			}
			again, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port)
			if err != nil || again.ID != op.ID || again.Status != wantStatus || port.writes != wantWrites {
				t.Fatal("duplicate dispatch retried a consumed proof", again, port.writes, err)
			}
			req := f.req
			req.IdempotencyKey = "synthetic-notice-second-proof"
			if _, err = f.text.StageChiliMessage(f.f.ctx, f.ref, req); err == nil {
				t.Fatal("consumed singleton grant staged another proof")
			}
			if f.f.count(`SELECT count(*) FROM test_windows WHERE id=$1 AND used=1`, f.window.ID) != 1 ||
				f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1
					AND (identity_event_id IS NOT NULL OR identity_sealed IS NOT NULL OR proof_event_id IS NOT NULL)`, f.challenge) != 0 {
				t.Fatal("send manufactured proof or changed the singleton reservation")
			}
			assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
		})
	}
}

type chiliNoticeProofEvidence struct {
	t     *testing.T
	want  lajiaoliao.MediaEvidenceQuery
	reads int
}

func (p *chiliNoticeProofEvidence) ChiliMediaEvidence(_ context.Context, q lajiaoliao.MediaEvidenceQuery) (lajiaoliao.MediaEvidence, error) {
	p.t.Helper()
	p.reads++
	if q.Account != p.want.Account || q.Sender != p.want.Sender || q.Group != p.want.Group ||
		q.Peer != p.want.Peer || q.Epoch != p.want.Epoch ||
		p.want.CurrentEpoch != 0 && q.CurrentEpoch != p.want.CurrentEpoch || q.ClientID == "" ||
		q.ContentType != 101 || len(q.Digest) != sha256.Size || q.SubmittedAt.IsZero() {
		return lajiaoliao.MediaEvidence{}, errors.New("synthetic evidence lookup changed the submitted route")
	}
	// No matching receive evidence: retain unknown rather than manufacture it.
	return lajiaoliao.MediaEvidence{}, nil
}

func TestPostgresChiliPrivateNoticeProofUnknownAfterCloseRemainsReadOnly(t *testing.T) {
	f := newChiliNoticeProofFixture(t)
	op, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req)
	if err != nil {
		t.Fatal(op, err)
	}
	port := &chiliNoticeProofPort{t: t, peer: f.recipient, eventID: f.req.PrivateEventID, unknown: true}
	result, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port)
	if err != nil || result.Status != "unknown" || port.writes != 1 {
		t.Fatal("synthetic receipt loss did not retain a single unknown send", result, port.writes, err)
	}
	f.f.exec(`UPDATE onboarding_test_scopes SET state='closed',version=version+1 WHERE id=$1`, f.scopeID)
	f.f.exec(`UPDATE test_windows SET cancelled=true,created_at=clock_timestamp()-interval '10 minutes',
		expires_at=clock_timestamp()-interval '6 minutes' WHERE id=$1`, f.window.ID)
	f.f.exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.claimant)
	evidence := &chiliNoticeProofEvidence{t: t, want: lajiaoliao.MediaEvidenceQuery{
		Account: f.claimant, Sender: f.sender, Group: f.nativeID, Peer: f.recipient,
		Epoch: f.req.AccountEpoch, CurrentEpoch: f.req.AccountEpoch + 1}}
	pending, err := f.text.PendingConversationText(f.f.ctx, f.ref, f.claimant, f.req.PrivateConversation)
	if err != nil || len(pending) != 1 || pending[0].ID != op.ID || pending[0].Status != "unknown" {
		t.Fatal("expired admission hid the original unresolved operation", pending, err)
	}
	for i := 0; i < 2; i++ {
		checked, err := f.text.ReconcileChiliMedia(f.f.ctx, f.ref, op.ID, evidence)
		if err != nil || checked.ID != op.ID || checked.Status != "unknown" || port.writes != 1 {
			t.Fatal("closed/expired proof could not read its original unknown operation", checked, port.writes, err)
		}
	}
	if evidence.reads != 2 {
		t.Fatal("unknown reconciliation never reached the read-only evidence port")
	}
	_, other := groupBusinessAccessOperator(t, f.assistantBusinessFixture, 121)
	if _, err = f.text.ReconcileChiliMedia(f.f.ctx, other, op.ID, evidence); err == nil || evidence.reads != 2 {
		t.Fatal("another actor inspected the unknown proof operation")
	}
	pending, err = f.text.PendingConversationText(f.f.ctx, other, f.claimant, f.req.PrivateConversation)
	if err != nil || len(pending) != 0 {
		t.Fatal("another actor discovered the unresolved operation", pending, err)
	}
	f.f.exec(`UPDATE accounts SET native_uid='synthetic_replaced_identity' WHERE id=$1`, f.claimant)
	if _, err = f.text.ReconcileChiliMedia(f.f.ctx, f.ref, op.ID, evidence); err == nil || evidence.reads != 2 {
		t.Fatal("replacement native identity inspected the original submission")
	}
	f.f.exec(`UPDATE accounts SET native_uid=$2 WHERE id=$1`, f.claimant, f.sender)
	// Source fingerprint changes cannot retarget even the read-only lookup.
	f.replaceNotice(t, f.notice(t, 1204, true))
	if _, err = f.text.ReconcileChiliMedia(f.f.ctx, f.ref, op.ID, evidence); err == nil || evidence.reads != 2 {
		t.Fatal("changed notice source retargeted unknown reconciliation")
	}
	if f.f.count(`SELECT count(*) FROM operations`) != 1 ||
		f.f.count(`SELECT count(*) FROM test_windows WHERE id=$1 AND used=1 AND cancelled`, f.window.ID) != 1 ||
		f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1
			AND (identity_event_id IS NOT NULL OR identity_sealed IS NOT NULL OR proof_event_id IS NOT NULL)`, f.challenge) != 0 {
		t.Fatal("unknown reconciliation created work, renewed budget or fabricated proof")
	}
	assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
}

func TestPostgresChiliProofLegacyBindingReconciliation(t *testing.T) {
	for _, found := range []bool{false, true} {
		t.Run(map[bool]string{false: "no_evidence", true: "server_evidence"}[found], func(t *testing.T) {
			f := newChiliNoticeProofBindingFixture(t, true)
			op, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req)
			if err != nil {
				t.Fatal(err)
			}
			port := &chiliNoticeProofPort{t: t, peer: f.recipient, eventID: f.req.PrivateEventID, unknown: true}
			unknown, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port)
			if err != nil || unknown.Status != "unknown" || port.writes != 1 {
				t.Fatal("legacy fixture did not retain its one unknown submission", unknown, err)
			}
			f.f.exec(`UPDATE onboarding_test_scopes SET state='closed',version=version+1 WHERE id=$1`, f.scopeID)
			immutable, receipt := f.reconciliationSnapshot(t, op.ID)
			evidence := &chiliProofHistoryEvidence{evidence: lajiaoliao.MediaEvidence{Found: found, Source: "server_history"}}
			out, err := f.text.ReconcileChiliMedia(f.f.ctx, f.ref, op.ID, evidence)
			want := unknown
			if found {
				want.Status, want.ResultCode = "succeeded", "im_server_accepted_reconciled"
			}
			if err != nil || out != want || evidence.reads != 1 || port.writes != 1 {
				t.Fatal("proof audit tenant lost the pinned resource tenant", out, err)
			}
			after, afterReceipt := f.reconciliationSnapshot(t, op.ID)
			if !bytes.Equal(after, immutable) || !found && !bytes.Equal(afterReceipt, receipt) ||
				f.f.count(`SELECT count(*) FROM group_binding_ownership WHERE group_id=$1`, f.scope.GroupID) != 0 ||
				f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1
					AND (identity_event_id IS NOT NULL OR identity_sealed IS NOT NULL OR proof_event_id IS NOT NULL)`, f.challenge) != 0 {
				t.Fatal("reconciliation moved ownership, fabricated proof or changed the sealed attempt")
			}
			assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
		})
	}
}

type chiliProofHistoryEvidence struct {
	before   func()
	evidence lajiaoliao.MediaEvidence
	reads    int
}

type chiliProofFailureEvidence struct{ err error }

func (p chiliProofFailureEvidence) ChiliMediaEvidence(context.Context, lajiaoliao.MediaEvidenceQuery) (lajiaoliao.MediaEvidence, error) {
	return lajiaoliao.MediaEvidence{}, p.err
}

func (f *chiliNoticeProofFixture) reconciliationSnapshot(t *testing.T, id string) (immutable, receipt []byte) {
	t.Helper()
	// Only the original operation's status, result and receipt may change.
	err := f.f.db.Platform.QueryRow(f.f.ctx, `SELECT jsonb_build_object(
		'operation',to_jsonb(o)-ARRAY['status','result_code','updated_at'],
		'attempt',to_jsonb(a)-'receipt_sealed','window',to_jsonb(w),
		'challenge',(SELECT to_jsonb(c) FROM group_binding_challenges c WHERE c.id=$2),
		'ownership',(SELECT to_jsonb(b) FROM group_binding_ownership b WHERE b.group_id=o.group_id),
		'scope',(SELECT to_jsonb(s) FROM onboarding_test_scopes s WHERE s.id=$3),
		'operations',(SELECT count(*) FROM operations),
		'attempts',(SELECT count(*) FROM lajiaoliao_media_attempts),
		'windows',(SELECT count(*) FROM test_windows),
		'received',(SELECT count(*) FROM native_received_events),
		'decoded',(SELECT count(*) FROM native_decoded_events))::text,a.receipt_sealed
		FROM operations o JOIN lajiaoliao_media_attempts a ON a.operation_id=o.id
		JOIN test_windows w ON w.id=o.test_window_id WHERE o.id=$1`,
		id, f.challenge, f.scopeID).Scan(&immutable, &receipt)
	if err != nil {
		t.Fatal(err)
	}
	return immutable, receipt
}

func TestPostgresChiliProofEvidenceFailureRetainsUnknown(t *testing.T) {
	f := newChiliNoticeProofFixture(t)
	op, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req)
	if err != nil {
		t.Fatal(err)
	}
	port := &chiliNoticeProofPort{t: t, peer: f.recipient, eventID: f.req.PrivateEventID, unknown: true}
	result, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port)
	if err != nil || result.Status != "unknown" || result.ResultCode != "native_text_result_unknown" || port.writes != 1 {
		t.Fatal("synthetic lost receipt did not retain a single attempt", result, err)
	}
	immutable, receipt := f.reconciliationSnapshot(t, op.ID)
	if len(receipt) != 0 {
		t.Fatal("unknown submission already has a receipt")
	}
	for _, tc := range []struct {
		name, code string
		status     int
		failure    error
	}{
		{"sequence_gap", "native_text_evidence_gap", 409, &lajiaoliao.Failure{Code: "lajiaoliao_sequence_gap"}},
		{"history_partial_gap", "lajiaoliao_evidence_history_partial_gap", 409,
			&lajiaoliao.Failure{Code: "lajiaoliao_evidence_history_partial_gap"}},
		{"timeout", "native_text_evidence_timeout", 503, context.DeadlineExceeded},
		{"private_error", "native_text_evidence_unavailable", 503,
			errors.New("password=synthetic-secret payload=synthetic-private")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checked, err := f.text.ReconcileChiliMedia(f.f.ctx, f.ref, op.ID, chiliProofFailureEvidence{tc.failure})
			var safe *Failure
			if !errors.As(err, &safe) || safe.Code != tc.code || safe.Status != tc.status ||
				err.Error() != tc.code || errors.Unwrap(err) != nil || checked != result || port.writes != 1 {
				t.Fatal("failed evidence read changed or replayed the original submission", checked, err)
			}
			after, afterReceipt := f.reconciliationSnapshot(t, op.ID)
			if !bytes.Equal(after, immutable) || !bytes.Equal(afterReceipt, receipt) ||
				f.f.count(`SELECT count(*) FROM operations WHERE id=$1 AND status='unknown'
					AND result_code='native_text_result_unknown'`, op.ID) != 1 ||
				f.f.count(`SELECT count(*) FROM audit_events WHERE action='native_media_reconciled' AND resource_id=$1`, op.ID) != 0 {
				t.Fatal("failed evidence read changed sealed intent, attempt, budget, ingress or proof")
			}
			assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
		})
	}
}

func (p *chiliProofHistoryEvidence) ChiliMediaEvidence(_ context.Context, q lajiaoliao.MediaEvidenceQuery) (lajiaoliao.MediaEvidence, error) {
	p.reads++
	if p.before != nil {
		p.before()
	}
	p.evidence.Receipt = lajiaoliao.Receipt{ClientID: q.ClientID, ServerID: "synthetic-server-proof",
		Timestamp: q.SubmittedAt.UnixMilli()}
	return p.evidence, nil
}

func TestPostgresChiliProofHistoricalObservationFencesCurrentIdentity(t *testing.T) {
	for _, tc := range []struct {
		name, code string
		evidence   lajiaoliao.MediaEvidence
	}{
		{"observed", "", lajiaoliao.MediaEvidence{Found: true, Source: "server_history"}},
		{"epoch_changed", "native_text_account_changed", lajiaoliao.MediaEvidence{Found: true, Source: "server_history"}},
		{"not_found", "", lajiaoliao.MediaEvidence{Source: "server_history"}},
		{"truncated", "", lajiaoliao.MediaEvidence{Found: true, Truncated: true, Source: "server_history"}},
		{"not_found_truncated_invalid_source", "", lajiaoliao.MediaEvidence{Truncated: true, Source: "untrusted_model", EventID: 1}},
		{"invalid_source", "native_text_evidence_conflict", lajiaoliao.MediaEvidence{Found: true, Source: "untrusted_model"}},
		{"server_history_with_event", "native_text_evidence_conflict", lajiaoliao.MediaEvidence{Found: true, Source: "server_history", EventID: 1}},
		{"server_history_negative_event", "native_text_evidence_conflict", lajiaoliao.MediaEvidence{Found: true, Source: "server_history", EventID: -1}},
		{"durable_echo_without_event", "native_text_evidence_conflict", lajiaoliao.MediaEvidence{Found: true, Source: "durable_echo"}},
		{"missing_source_without_event", "native_text_evidence_conflict", lajiaoliao.MediaEvidence{Found: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newChiliNoticeProofFixture(t)
			op, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req)
			if err != nil {
				t.Fatal(err)
			}
			port := &chiliNoticeProofPort{t: t, peer: f.recipient, eventID: f.req.PrivateEventID, unknown: true}
			unknown, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port)
			if err != nil || unknown.Status != "unknown" || unknown.ResultCode != "native_text_result_unknown" || port.writes != 1 {
				t.Fatal(unknown, err)
			}
			f.f.exec(`UPDATE onboarding_test_scopes SET state='closed',version=version+1 WHERE id=$1`, f.scopeID)
			f.f.exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.claimant)
			immutable, receipt := f.reconciliationSnapshot(t, op.ID)
			if len(receipt) != 0 {
				t.Fatal("unknown submission already has a receipt")
			}
			evidence := &chiliProofHistoryEvidence{evidence: tc.evidence}
			if tc.name == "epoch_changed" {
				evidence.before = func() { f.f.exec(`UPDATE accounts SET epoch=epoch+1 WHERE id=$1`, f.claimant) }
			}
			out, err := f.text.ReconcileChiliMedia(f.f.ctx, f.ref, op.ID, evidence)
			want := unknown
			if tc.name == "observed" {
				want.Status, want.ResultCode = "succeeded", "im_server_accepted_reconciled"
				if err != nil || out != want {
					t.Fatal("server evidence could not resolve the immutable original", out, err)
				}
				if again, err := f.text.ReconcileChiliMedia(f.f.ctx, f.ref, op.ID, evidence); err != nil || again != out || evidence.reads != 1 {
					t.Fatal("terminal observation performed another native read", err, evidence.reads)
				}
				if f.f.count(`SELECT count(*) FROM audit_events WHERE action='native_media_reconciled'
					AND resource_id=$1 AND detail->>'evidence_source'='server_history'
					AND detail->>'event_id'='0'`, op.ID) != 1 {
					t.Fatal("server history lost its distinct evidence source")
				}
			} else {
				if tc.code == "" {
					if err != nil {
						t.Fatal("incomplete history observation should retain unknown", err)
					}
				} else {
					capabilityError(t, err, tc.code)
				}
				if out != unknown ||
					f.f.count(`SELECT count(*) FROM audit_events WHERE action='native_media_reconciled' AND resource_id=$1`, op.ID) != 0 {
					t.Fatal("incomplete, changed identity or untrusted evidence resolved an unknown", out, err)
				}
			}
			after, afterReceipt := f.reconciliationSnapshot(t, op.ID)
			if !bytes.Equal(after, immutable) || evidence.reads != 1 ||
				f.f.count(`SELECT count(*) FROM operations WHERE id=$1 AND status=$2 AND result_code=$3`,
					op.ID, want.Status, want.ResultCode) != 1 {
				t.Fatal("observation changed sealed intent, attempt, budget, ingress or proof")
			}
			if tc.name == "observed" {
				raw, err := f.bindings.Vault.Open("lajiaoliao-media-receipt:"+op.ID, afterReceipt)
				if err != nil {
					t.Fatal("reconciled receipt was not sealed for the original operation", err)
				}
				var got lajiaoliao.Receipt
				err = json.Unmarshal(raw, &got)
				clear(raw)
				if err != nil || got != evidence.evidence.Receipt {
					t.Fatal("reconciled receipt changed the original submission", err)
				}
			} else if !bytes.Equal(afterReceipt, receipt) {
				t.Fatal("rejected or incomplete evidence replaced the sealed receipt")
			}
			if port.writes != 1 || f.f.count(`SELECT count(*) FROM operations`) != 1 ||
				f.f.count(`SELECT count(*) FROM native_received_events`) != 1 ||
				f.f.count(`SELECT count(*) FROM native_decoded_events`) != 1 ||
				f.f.count(`SELECT count(*) FROM test_windows WHERE id=$1 AND used=1`, f.window.ID) != 1 ||
				f.f.count(`SELECT count(*) FROM group_binding_challenges WHERE id=$1 AND state='waiting'
					AND identity_event_id IS NULL AND identity_sealed IS NULL
					AND proof_event_id IS NULL AND proof_sealed IS NULL`, f.challenge) != 1 {
				t.Fatal("historical observation replayed, manufactured proof or changed the attempt budget")
			}
			assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
		})
	}
}
