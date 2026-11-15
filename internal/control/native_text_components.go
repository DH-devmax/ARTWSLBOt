package control

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"slices"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

type textReference struct {
	raw, projection []byte
}

func (r *textReference) equal(other *textReference) bool {
	return other != nil && bytes.Equal(r.raw, other.raw) && bytes.Equal(r.projection, other.projection)
}

func (r *textReference) digest() []byte {
	raw, projection := sha256.Sum256(r.raw), sha256.Sum256(r.projection)
	return append(append(make([]byte, 0, 64), raw[:]...), projection[:]...)
}

// A reference is a retained source, never caller-supplied quote text or a route.
// Three seconds of retention slack cover the final bounded socket write.
func (s *NativeTextService) textReference(ctx context.Context, tx pgx.Tx, req TextRequest) (*textReference, error) {
	out := &textReference{}
	err := tx.QueryRow(ctx, `SELECT e.payload_sealed,d.payload_sealed FROM native_decoded_events d
        JOIN native_received_events e ON e.id=d.event_id AND e.account_id=d.account_id
        JOIN native_deployment p ON p.version=d.deployment_version
        WHERE d.event_id=$1 AND d.account_id=$2 AND d.state='decoded' AND d.scene='group' AND e.kind='group'
        AND d.payload_sealed IS NOT NULL AND e.payload_sealed IS NOT NULL
        AND d.received_at>clock_timestamp()-interval '7 days'+interval '3 seconds'
        AND e.received_at>clock_timestamp()-interval '7 days'+interval '3 seconds'
        AND d.received_at<=clock_timestamp() AND e.received_at<=clock_timestamp()
        FOR SHARE OF d,e,p`, req.QuoteEventID, req.AccountID).Scan(&out.raw, &out.projection)
	if err == pgx.ErrNoRows {
		return nil, Denied(409, "native_text_reference_unavailable")
	}
	return out, err
}

func (s *NativeTextService) textComponents(ctx context.Context, tx pgx.Tx, req TextRequest,
	scope textScope, message *nativewire.GroupText) (*textReference, error) {
	message.Mentions = slices.Clone(req.Mentions)
	if req.QuoteEventID == 0 {
		return nil, nil
	}
	reference, err := s.textReference(ctx, tx, req)
	if err != nil {
		return nil, err
	}
	if len(req.ReferenceDigest) != 0 && !bytes.Equal(reference.digest(), req.ReferenceDigest) {
		return nil, Denied(409, "native_text_reference_changed")
	}
	plain, err := s.Vault.Open(security.DecodedMessageAAD(req.AccountID, req.QuoteEventID), reference.projection)
	if err != nil {
		return nil, Denied(409, "native_text_reference_unavailable")
	}
	// Read only the authenticated message projection needed for the quote.
	// Gateway independently opens its raw source and verifies the current route.
	var projection struct {
		Message      nativewire.ApplicationMessage `json:"message"`
		ServerID     string                        `json:"server_id"`
		NativeFormat string                        `json:"native_format"`
	}
	err = json.Unmarshal(plain, &projection)
	clear(plain)
	if err != nil || projection.NativeFormat != "100" || projection.Message.Session != 2 ||
		projection.Message.Format != 0 || projection.Message.Target.ID != scope.group ||
		projection.Message.Sender.ID == 0 || projection.Message.Sender.ID >= 1<<30 {
		return nil, Denied(409, "native_text_reference_unavailable")
	}
	message.Quote = &nativewire.MessageQuote{ClientID: projection.Message.ClientID,
		ServerID: projection.ServerID, Text: projection.Message.Text}
	return reference, nil
}
