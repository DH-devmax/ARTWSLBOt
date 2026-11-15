package control

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

func (s *NativeTextService) privateTarget(ctx context.Context, tx pgx.Tx, req TextRequest) (nativewire.PrivateTarget, []byte, error) {
	var empty nativewire.PrivateTarget
	var raw, projection, credential []byte
	var nativeUID string
	var version int64
	err := tx.QueryRow(ctx, `SELECT e.payload_sealed,d.payload_sealed,a.credential_sealed,a.native_uid,p.version
        FROM native_decoded_events d JOIN native_received_events e ON e.id=d.event_id AND e.account_id=d.account_id
        JOIN accounts a ON a.id=d.account_id JOIN native_deployment p ON p.version=d.deployment_version
        WHERE d.event_id=$1 AND d.account_id=$2 AND d.conversation_key=$3
        AND d.state='decoded' AND d.scene='private' AND e.kind='private'
        AND a.platform='wsl' AND a.workspace='wsl' AND a.auth_state='authenticated'
        AND d.payload_sealed IS NOT NULL AND e.payload_sealed IS NOT NULL
        AND d.received_at>clock_timestamp()-interval '7 days'+interval '3 seconds'
        AND e.received_at>clock_timestamp()-interval '7 days'+interval '3 seconds'
        AND d.received_at<=clock_timestamp() AND e.received_at<=clock_timestamp()
        FOR SHARE OF d,e,a,p`, req.PrivateEventID, req.AccountID, req.PrivateConversation).
		Scan(&raw, &projection, &credential, &nativeUID, &version)
	if err != nil {
		if err == pgx.ErrNoRows {
			return empty, nil, Denied(409, "native_private_target_unavailable")
		}
		return empty, nil, err
	}
	fingerprint := (&textReference{raw: raw, projection: projection}).digest()
	if len(req.PrivateDigest) != 0 && !bytes.Equal(req.PrivateDigest, fingerprint) {
		return empty, nil, Denied(409, "native_private_target_changed")
	}
	open := func(aad string, sealed []byte, out any) error {
		plain, err := s.Vault.Open(aad, sealed)
		if err != nil {
			return err
		}
		defer clear(plain)
		return json.Unmarshal(plain, out)
	}
	var session struct {
		Version int64 `json:"deployment_version"`
		Session struct {
			UID   uint64 `json:"uid"`
			NIMID string `json:"nim_id"`
		}
	}
	var projectionRecord struct {
		Message       nativewire.ApplicationMessage `json:"message"`
		TransportPeer string                        `json:"transport_peer"`
		TransportTo   string                        `json:"transport_to"`
		NativeFormat  string                        `json:"native_format"`
	}
	if open("native-session:"+req.AccountID, credential, &session) != nil ||
		session.Version != version ||
		strconv.FormatUint(session.Session.UID, 10) != nativeUID ||
		open(security.DecodedMessageAAD(req.AccountID, req.PrivateEventID), projection, &projectionRecord) != nil ||
		projectionRecord.NativeFormat != "100" {
		return empty, nil, Denied(409, "native_private_target_unavailable")
	}
	peer, err := nativewire.PrivateTargetFor(session.Session.UID, session.Session.NIMID,
		projectionRecord.Message, projectionRecord.TransportPeer, projectionRecord.TransportTo)
	if err != nil || nativewire.PrivateConversationKey(peer) != req.PrivateConversation {
		return empty, nil, Denied(409, "native_private_target_unavailable")
	}
	return peer, fingerprint, nil
}

// PrivateConversation resolves only a retained account-scoped destination. It
// grants no send permission; Stage/Dispatch require the separate human window.
func (s *NativeTextService) PrivateConversation(ctx context.Context, ref SessionRef, account, key string) (nativewire.PrivateTarget, int64, error) {
	var peer nativewire.PrivateTarget
	tx, err := s.DB.Platform.Begin(ctx)
	if err != nil {
		return peer, 0, err
	}
	defer tx.Rollback(ctx)
	actor, err := sessionActor(ctx, tx, ref, false)
	if err != nil {
		return peer, 0, err
	}
	if actor.Role != "superadmin" {
		return peer, 0, Denied(403, "superadmin_required")
	}
	var event int64
	err = tx.QueryRow(ctx, `SELECT event_id FROM native_decoded_events WHERE account_id=$1
        AND conversation_key=$2 AND scene='private' AND state='decoded'
        ORDER BY event_id DESC LIMIT 1`, account, key).Scan(&event)
	if err != nil {
		if err == pgx.ErrNoRows {
			return peer, 0, Denied(404, "conversation_not_found")
		}
		return peer, 0, err
	}
	peer, _, err = s.privateTarget(ctx, tx, TextRequest{AccountID: account, PrivateConversation: key, PrivateEventID: event})
	if err != nil {
		return peer, 0, err
	}
	return peer, event, tx.Commit(ctx)
}

func (r TextRequest) encode(message nativewire.GroupText, peer *nativewire.PrivateTarget) ([]byte, error) {
	if peer != nil {
		return nativewire.EncodePrivateText(privateMessage(message, *peer))
	}
	return nativewire.EncodeGroupText(message)
}

func privateMessage(message nativewire.GroupText, peer nativewire.PrivateTarget) nativewire.PrivateText {
	return nativewire.PrivateText{SenderID: message.SenderID, PeerID: peer.ID, ClientID: message.ClientID,
		Text: message.Text, CreatedAt: message.CreatedAt, Media: message.Media}
}
