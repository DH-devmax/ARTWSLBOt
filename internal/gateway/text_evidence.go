package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"wangshangliao/internal/nativewire"
)

// TextEvidence reads durable, server-received echoes only. It performs no
// native request, no retry, no raw-message delivery and no inference from absence.
// The caller must authorize the website actor and load immutable route evidence.
func (m *ReceiverManager) TextEvidence(ctx context.Context, q nativewire.TextEvidenceQuery) (nativewire.TextEvidence, error) {
	var out nativewire.TextEvidence
	if q.AccountID == "" || q.ClientID == "" || len(q.Route.Digest) != 32 ||
		q.Route.Team == "" || q.Route.Sender == "" || q.SubmittedAt.IsZero() {
		return out, ErrTextRoute
	}
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var current bool
	if err := m.DB.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM accounts
        WHERE id=$1 AND epoch=$2)`, q.AccountID, q.AccountEpoch).Scan(&current); err != nil || !current {
		return out, ErrTextOwner
	}
	kind := "group"
	if q.Route.PrivatePeer != "" {
		if !nativewire.ValidPrivateTransport(q.Route.PrivatePeer) {
			return out, ErrTextRoute
		}
		kind = "private"
	}
	rows, err := m.DB.Query(ctx, `SELECT id,event_key,payload_sealed FROM native_received_events
        WHERE account_id=$1 AND kind=$3 AND received_at>=$2 AND received_at<=clock_timestamp()
        AND received_at>clock_timestamp()-interval '7 days' ORDER BY id DESC LIMIT 201`,
		q.AccountID, q.SubmittedAt, kind)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		if count > 200 {
			out.Truncated = true
			break
		}
		var id int64
		var event string
		var sealed []byte
		if err = rows.Scan(&id, &event, &sealed); err != nil {
			return nativewire.TextEvidence{}, err
		}
		raw, err := m.Vault.Open("native-event:"+q.AccountID+":"+event, sealed)
		if err != nil {
			return nativewire.TextEvidence{}, errors.New("native_text_evidence_unavailable")
		}
		var message nativewire.IMMessage
		err = json.Unmarshal(raw, &message)
		clear(raw)
		if err != nil {
			return nativewire.TextEvidence{}, errors.New("native_text_evidence_unavailable")
		}
		receipt, matched := matchTextEcho(q, message)
		if !matched {
			continue
		}
		if out.Found && (out.Receipt.ServerID != receipt.ServerID || out.Receipt.TimeMS != receipt.TimeMS) {
			return nativewire.TextEvidence{}, errors.New("native_text_evidence_conflict")
		}
		out.Found, out.EventID, out.Receipt = true, id, receipt
	}
	// A bounded search cannot establish uniqueness if its evidence was truncated.
	if err := rows.Err(); err != nil {
		return nativewire.TextEvidence{}, err
	}
	if out.Truncated {
		out.Found, out.EventID, out.Receipt = false, 0, nativewire.SendReceipt{}
	}
	return out, nil
}

func matchTextEcho(q nativewire.TextEvidenceQuery, m nativewire.IMMessage) (nativewire.SendReceipt, bool) {
	var empty nativewire.SendReceipt
	scene, field, target := 1, "1", q.Route.Team
	if q.Route.PrivatePeer != "" {
		scene, field, target = 0, "0", q.Route.PrivatePeer
	}
	if m.Scene != scene || m.Target != target || m.Sender != q.Route.Sender ||
		m.Fields[0] != field || m.Fields[1] != target || m.Fields[2] != q.Route.Sender ||
		m.Fields[8] != "100" || m.Fields[11] != q.ClientID || m.Fields[12] != m.ServerID {
		return empty, false
	}
	hash := sha256.Sum256([]byte(m.Fields[10]))
	if !bytes.Equal(hash[:], q.Route.Digest) {
		return empty, false
	}
	body, err := nativewire.Properties([]nativewire.Property{{Key: 11, Value: []byte(q.ClientID)},
		{Key: 12, Value: []byte(m.ServerID)}, {Key: 7, Value: []byte(m.Fields[7])}})
	if err != nil {
		return empty, false
	}
	receipt, err := nativewire.ParseSendReceipt(body, q.ClientID)
	return receipt, err == nil
}
