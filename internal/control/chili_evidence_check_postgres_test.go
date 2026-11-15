package control

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"wangshangliao/internal/lajiaoliao"
)

func TestPostgresChiliEvidenceCheckIsReadOnlyAndBounded(t *testing.T) {
	f := newChiliNoticeProofFixture(t)
	op, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req)
	if err != nil {
		t.Fatal(err)
	}
	port := &chiliNoticeProofPort{t: t, peer: f.recipient, eventID: f.req.PrivateEventID, unknown: true}
	unknown, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port)
	if err != nil || unknown.Status != "unknown" || port.writes != 1 {
		t.Fatal("synthetic unknown submission not established", err)
	}
	immutable, receipt := f.reconciliationSnapshot(t, op.ID)
	for _, state := range []string{"conversation_not_returned", "history_empty", "message_not_found", "scan_truncated"} {
		t.Run(state, func(t *testing.T) {
			check := &lajiaoliao.EvidenceCheck{State: state, DurableRecords: 1,
				SequenceHeads: 4, Conversations: 1, Requested: 3, Returned: 3, SampledAt: time.Now().UTC()}
			evidence := &chiliProofHistoryEvidence{evidence: lajiaoliao.MediaEvidence{
				Truncated: state == "scan_truncated", Check: check,
			}}
			out, err := f.text.ReconcileChiliMedia(f.f.ctx, f.ref, op.ID, evidence)
			if err != nil || out.ID != unknown.ID || out.Status != unknown.Status ||
				out.ResultCode != unknown.ResultCode || out.EvidenceCheck == nil ||
				*out.EvidenceCheck != *check || out.EvidenceCheck == check || evidence.reads != 1 {
				t.Fatal("bounded observation did not preserve the unknown operation", out, err)
			}
			raw, err := json.Marshal(out.EvidenceCheck)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if json.Unmarshal(raw, &fields) != nil || len(fields) != 7 {
				t.Fatal("observation response contract changed")
			}
			for _, key := range []string{"state", "durable_records", "sequence_heads", "conversations", "requested", "returned", "sampled_at"} {
				if _, ok := fields[key]; !ok {
					t.Fatal("observation leaked or omitted a field", key)
				}
			}
			for _, private := range []string{f.sender, f.recipient, f.req.Text, f.req.PrivateConversation, op.ID} {
				if private != "" && strings.Contains(string(raw), private) {
					t.Fatal("bounded diagnostic included private content or identity")
				}
			}
			after, afterReceipt := f.reconciliationSnapshot(t, op.ID)
			if !bytes.Equal(after, immutable) || !bytes.Equal(afterReceipt, receipt) ||
				port.writes != 1 ||
				f.f.count(`SELECT count(*) FROM operations WHERE id=$1 AND status='unknown'
					AND result_code='native_text_result_unknown'`, op.ID) != 1 ||
				f.f.count(`SELECT count(*) FROM audit_events WHERE action='native_media_reconciled' AND resource_id=$1`, op.ID) != 0 {
				t.Fatal("read-only diagnostic changed the submission, proof or budget")
			}
			assertControlledUsage(t, f.f, f.scopeID, 2, 0, 0)
		})
	}
}

func TestPostgresChiliEvidenceCheckRejectsInvalidDiagnostics(t *testing.T) {
	f := newChiliNoticeProofFixture(t)
	op, err := f.text.StageChiliMessage(f.f.ctx, f.ref, f.req)
	if err != nil {
		t.Fatal(err)
	}
	port := &chiliNoticeProofPort{t: t, peer: f.recipient, eventID: f.req.PrivateEventID, unknown: true}
	unknown, err := f.text.DispatchChiliMedia(f.f.ctx, f.ref, op.ID, port)
	if err != nil || unknown.Status != "unknown" {
		t.Fatal(err)
	}
	for _, mode := range []string{"unknown_state", "matched", "negative", "too_many", "too_many_heads",
		"too_many_conversations", "too_many_requested", "too_many_returned", "missing_time",
		"truncated_without_flag", "flag_without_truncated"} {
		t.Run(mode, func(t *testing.T) {
			check := &lajiaoliao.EvidenceCheck{State: "message_not_found", SampledAt: time.Now().UTC()}
			evidence := &chiliProofHistoryEvidence{evidence: lajiaoliao.MediaEvidence{Check: check}}
			switch mode {
			case "unknown_state":
				check.State = "password=synthetic-private"
			case "matched":
				check.State = "matched"
			case "negative":
				check.DurableRecords = -1
			case "too_many":
				check.DurableRecords = 201
			case "too_many_heads":
				check.SequenceHeads = 10001
			case "too_many_conversations":
				check.Conversations = 3
			case "too_many_requested":
				check.Requested = 201
			case "too_many_returned":
				check.Returned = 201
			case "missing_time":
				check.SampledAt = time.Time{}
			case "truncated_without_flag":
				check.State = "scan_truncated"
			case "flag_without_truncated":
				evidence.evidence.Truncated = true
			}
			out, err := f.text.ReconcileChiliMedia(f.f.ctx, f.ref, op.ID, evidence)
			if err != nil || out != unknown || evidence.reads != 1 || port.writes != 1 {
				t.Fatal("invalid or contradictory observation escaped the port", out, err)
			}
		})
	}
	_, other := groupBusinessAccessOperator(t, f.assistantBusinessFixture, 121)
	evidence := &chiliProofHistoryEvidence{evidence: lajiaoliao.MediaEvidence{
		Check: &lajiaoliao.EvidenceCheck{State: "message_not_found", SampledAt: time.Now().UTC()},
	}}
	if out, err := f.text.ReconcileChiliMedia(f.f.ctx, other, op.ID, evidence); err == nil ||
		out.EvidenceCheck != nil || evidence.reads != 0 {
		t.Fatal("another actor received the diagnostic or triggered an upstream read")
	}
}
