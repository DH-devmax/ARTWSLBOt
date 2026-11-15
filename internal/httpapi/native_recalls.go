package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativewire"
)

// Native recall reads are bounded, explicit queries over already sealed input.
// They do not change projections, source events, message bodies or ACK/cursors.
func (s *Server) nativeRecalls(w http.ResponseWriter, r *http.Request, p Principal) error {
	key, account := r.PathValue("conversation"), r.PathValue("id")
	if !conversationPattern.MatchString(key) {
		return fail(400, "invalid_conversation")
	}
	before := int64(0)
	if value := r.URL.Query().Get("before"); value != "" {
		var err error
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before < 1 {
			return fail(400, "invalid_pagination")
		}
	}
	var sealed []byte
	var event int64
	var scene, platform string
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT d.event_id,d.scene,d.payload_sealed,a.platform
		FROM native_decoded_events d JOIN accounts a ON a.id=d.account_id
		WHERE d.account_id=$1 AND d.conversation_key=$2 AND d.state IN ('decoded','unsupported')
		AND d.received_at>now()-interval '7 days' ORDER BY d.event_id DESC LIMIT 1`, account, key).
		Scan(&event, &scene, &sealed, &platform)
	if err == pgx.ErrNoRows {
		return fail(404, "native_conversation_not_found")
	}
	if err != nil {
		return err
	}
	plain, err := s.Vault.Open(gateway.DecodedAAD(account, event), sealed)
	if err != nil {
		return fail(503, "native_projection_unreadable")
	}
	var selected gateway.DecodedMessage
	err = json.Unmarshal(plain, &selected)
	clear(plain)
	if err != nil {
		return fail(503, "native_projection_unreadable")
	}
	kind := "notice"
	if platform == "lajiaoliao" {
		kind = scene
	}
	rows, err := s.DB.Platform.Query(r.Context(), `SELECT id,event_key,payload_sealed,received_at
		FROM native_received_events WHERE account_id=$1 AND kind=$2 AND payload_sealed IS NOT NULL
		AND received_at>now()-interval '7 days' AND ($3::bigint=0 OR id<$3)
		ORDER BY id DESC LIMIT 1001`, account, kind, before)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	scanned, invalid, bytesRead, last := 0, 0, 0, int64(0)
	more := false
	for rows.Next() {
		if scanned >= 1000 || bytesRead >= 4<<20 || len(items) >= 100 {
			more = true
			break
		}
		var id int64
		var sourceKey string
		var raw []byte
		var at time.Time
		if err = rows.Scan(&id, &sourceKey, &raw, &at); err != nil {
			return err
		}
		last, scanned, bytesRead = id, scanned+1, bytesRead+len(raw)
		if len(raw) > 256<<10 {
			invalid++
			continue
		}
		body, err := s.Vault.Open("native-event:"+account+":"+sourceKey, raw)
		if err != nil {
			invalid++
			continue
		}
		notices := []nativewire.RecallNotice{}
		if platform == "lajiaoliao" {
			notice, e := lajiaoliao.ReadRecallNotice(body)
			err = e
			if notice != nil {
				notices = append(notices, *notice)
			}
		} else {
			var frame nativewire.Frame
			err = json.Unmarshal(body, &frame)
			if err == nil {
				notices, err = nativewire.ReadRecallNotices(frame)
			}
		}
		clear(body)
		if err != nil {
			invalid++
			continue
		}
		for index, notice := range notices {
			if recallMatches(selected, scene, platform, notice) {
				items = append(items, map[string]any{"id": strconv.FormatInt(id, 10) + ":" + strconv.Itoa(index),
					"received_at": at, "notice": notice})
			}
		}
	}
	if err = rows.Err(); err != nil {
		return err
	}
	next := ""
	if more {
		next = strconv.FormatInt(last, 10)
	}
	send(w, 200, map[string]any{"items": items, "next_before": next, "read_only": true,
		"scanned": scanned, "invalid": invalid, "bounded_scan": true, "sampled_at": time.Now().UTC(),
		"window_seconds": 7 * 86400, "scan_limit": 1000})
	return nil
}

func recallMatches(selected gateway.DecodedMessage, scene, platform string, notice nativewire.RecallNotice) bool {
	if scene == "group" {
		target := selected.TransportTo
		if platform == "lajiaoliao" {
			target = selected.NativeTarget
		}
		return notice.Scene == 1 && target != "" && notice.Target == target
	}
	if notice.Scene != 0 {
		return false
	}
	return notice.Sender == selected.TransportPeer && notice.Target == selected.TransportTo ||
		notice.Sender == selected.TransportTo && notice.Target == selected.TransportPeer
}
