package httpapi

import (
	"bytes"
	"context"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func (s *Server) chiliMessageMedia(w http.ResponseWriter, r *http.Request, p Principal,
	event, epoch int64, projection []byte, decoded gateway.DecodedMessage) error {
	account, conversation := r.PathValue("id"), r.PathValue("conversation")
	var sourceKey string
	var sealed []byte
	var version int64
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT e.event_key,e.payload_sealed,a.version
		FROM native_received_events e JOIN accounts a ON a.id=e.account_id
		WHERE e.id=$1 AND e.account_id=$2 AND a.platform='lajiaoliao' AND a.epoch=$3
		AND e.received_at>clock_timestamp()-interval '7 days' AND e.payload_sealed IS NOT NULL`,
		event, account, epoch).Scan(&sourceKey, &sealed, &version)
	if err == pgx.ErrNoRows || len(sealed) > 256<<10 {
		return fail(404, "native_media_not_found")
	}
	if err != nil {
		return err
	}
	raw, err := s.Vault.Open("native-event:"+account+":"+sourceKey, sealed)
	if err != nil {
		return fail(503, "native_projection_unreadable")
	}
	message, err := lajiaoliao.ReadMessageView(raw)
	clear(raw)
	if err != nil || !chiliMessageMatches(decoded, message) {
		return fail(404, "native_media_not_found")
	}
	resource, ok := message.Resource(r.PathValue("part"))
	if !ok {
		return fail(404, "native_media_not_found")
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	file, err := lajiaoliao.FetchResource(ctx, s.chiliMediaClient, resource)
	if err != nil {
		code := "lajiaoliao_media_unavailable"
		if failure, ok := err.(*lajiaoliao.Failure); ok {
			code = failure.Code
		}
		return fail(502, code)
	}
	defer clear(file.Data)
	if err = s.accountEventAccess(r, p, "messages"); err != nil {
		return err
	}
	var currentProjection, currentSource []byte
	err = s.DB.Platform.QueryRow(ctx, `SELECT d.payload_sealed,e.payload_sealed
		FROM native_decoded_events d JOIN native_received_events e ON e.id=d.event_id AND e.account_id=d.account_id
		JOIN accounts a ON a.id=d.account_id
		WHERE d.event_id=$1 AND d.account_id=$2 AND d.conversation_key=$3 AND a.epoch=$4 AND a.version=$5
		AND a.platform='lajiaoliao' AND d.state IN('decoded','unsupported')
		AND d.received_at>clock_timestamp()-interval '7 days' AND e.received_at>clock_timestamp()-interval '7 days'`,
		event, account, conversation, epoch, version).Scan(&currentProjection, &currentSource)
	if err == pgx.ErrNoRows {
		return fail(409, "native_media_scope_changed")
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(currentProjection, projection) || !bytes.Equal(currentSource, sealed) {
		return fail(409, "native_media_scope_changed")
	}
	return writeNativeMedia(w, nativeauth.MediaFile{Data: file.Data, MIME: file.MIME})
}
