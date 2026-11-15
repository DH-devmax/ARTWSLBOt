package httpapi

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
)

type directoryView struct {
	Groups    []directoryGroupView `json:"groups"`
	SampledAt *time.Time           `json:"sampled_at"`
	RetryAt   *time.Time           `json:"retry_at"`
	Error     string               `json:"error"`
	Stale     bool                 `json:"stale"`
}

type directoryGroupView struct {
	nativeauth.DirectoryGroup
	AvatarURL string `json:"avatar_url,omitempty"`
}

func (s *Server) readDirectory(r *http.Request) (directoryView, error) {
	out := directoryView{Groups: []directoryGroupView{}, Stale: true}
	var sealed []byte
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT payload_sealed,sampled_at,retry_at,error
        FROM native_directory_snapshots WHERE account_id=$1`, r.PathValue("id")).
		Scan(&sealed, &out.SampledAt, &out.RetryAt, &out.Error)
	if err == pgx.ErrNoRows {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	if len(sealed) > 0 {
		plain, e := s.Vault.Open("native-directory:"+r.PathValue("id"), sealed)
		if e != nil {
			return out, fail(503, "native_directory_unreadable")
		}
		e = json.Unmarshal(plain, &out.Groups)
		clear(plain)
		if e != nil {
			return out, fail(503, "native_directory_unreadable")
		}
		for index := range out.Groups {
			group := &out.Groups[index]
			group.AvatarURL = nativeGroupAvatarURL(r.PathValue("id"), group.DirectoryGroup)
		}
	}
	out.Stale = out.SampledAt == nil || time.Since(*out.SampledAt) > 10*time.Minute || out.Error != ""
	return out, nil
}

type nativeConversation struct {
	Key        string                 `json:"key"`
	Scene      string                 `json:"scene"`
	Title      string                 `json:"title"`
	BusinessID string                 `json:"business_id"`
	Transport  string                 `json:"transport"`
	Mapping    string                 `json:"mapping"`
	LastAt     time.Time              `json:"last_at"`
	Count      int64                  `json:"count"`
	Last       gateway.DecodedMessage `json:"last"`
	AvatarURL  string                 `json:"avatar_url,omitempty"`
}

func pageNumber(r *http.Request, key string, fallback, maxValue int) (int, error) {
	value := r.URL.Query().Get(key)
	if value == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(value)
	if err != nil || n < 0 || n > maxValue {
		return 0, fail(400, "invalid_pagination")
	}
	return n, nil
}

func (s *Server) nativeConversations(w http.ResponseWriter, r *http.Request, p Principal) error {
	scene := r.URL.Query().Get("scene")
	if scene != "" && scene != "group" && scene != "private" {
		return fail(400, "invalid_scene")
	}
	limit, err := pageNumber(r, "limit", 50, 100)
	if err != nil || limit == 0 {
		return fail(400, "invalid_pagination")
	}
	offset, err := pageNumber(r, "offset", 0, 100000)
	if err != nil {
		return err
	}
	directory, err := s.readDirectory(r)
	if err != nil {
		return err
	}
	rows, err := s.DB.Platform.Query(r.Context(), `WITH latest AS (
        SELECT scene,conversation_key,max(event_id) AS event_id,count(*) AS n
        FROM native_decoded_events WHERE account_id=$1 AND conversation_key<>''
            AND state IN ('decoded','unsupported') AND received_at>now()-interval '7 days'
            AND ($2='' OR scene=$2) GROUP BY scene,conversation_key
    ) SELECT x.event_id,x.scene,x.conversation_key,x.received_at,l.n,x.payload_sealed,count(*) OVER()
        FROM latest l JOIN native_decoded_events x ON x.event_id=l.event_id
        ORDER BY x.received_at DESC,x.conversation_key LIMIT $3 OFFSET $4`,
		r.PathValue("id"), scene, limit, offset)
	if err != nil {
		return err
	}
	defer rows.Close()
	items, total := []nativeConversation{}, int64(0)
	for rows.Next() {
		var item nativeConversation
		var event int64
		var sealed []byte
		if err = rows.Scan(&event, &item.Scene, &item.Key, &item.LastAt, &item.Count, &sealed, &total); err != nil {
			return err
		}
		plain, e := s.Vault.Open(gateway.DecodedAAD(r.PathValue("id"), event), sealed)
		if e != nil {
			return fail(503, "native_projection_unreadable")
		}
		e = json.Unmarshal(plain, &item.Last)
		clear(plain)
		if e != nil {
			return fail(503, "native_projection_unreadable")
		}
		// Conversation previews never return an entire large message/quote.
		item.Last.Message.Text, _ = boundedText(item.Last.Message.Text, 200)
		item.Last.Message.Quote, item.Last.Message.Mentions = nil, nil
		item.Last.Message.Forward = previewForward(item.Last.Message.Forward)
		media := nativeMediaLinks(r.PathValue("id"), item.Key, event, item.Last.Message)
		item.AvatarURL = media.Sender
		if item.Scene == "group" || item.Last.Outgoing {
			item.AvatarURL = media.Target
		}
		item.Mapping = item.Last.Mapping
		if item.Last.Platform == "lajiaoliao" {
			item.BusinessID, item.Transport = item.Last.NativeTarget, item.Last.NativeTarget
			item.Title = item.Last.NativeName
			if item.Scene == "group" {
				item.Title = item.BusinessID
				for _, group := range directory.Groups {
					if group.ID == item.BusinessID {
						item.Title = group.Name
						break
					}
				}
			}
		} else if item.Scene == "private" {
			item.BusinessID = strconv.FormatUint(item.Last.Message.Sender.ID, 10)
			item.Transport = item.Last.TransportPeer
			item.Title = item.Last.Message.Sender.Name
			if item.Last.Outgoing {
				item.BusinessID = strconv.FormatUint(item.Last.Message.Target.ID, 10)
				item.Transport, item.Title = item.Last.TransportTo, item.Last.Message.Target.Name
			}
		} else {
			item.BusinessID = strconv.FormatUint(item.Last.Message.Target.ID, 10)
			item.Transport = item.Last.TransportTo
			item.Title = item.Last.Message.Target.Name
			for _, group := range directory.Groups {
				if group.ID == item.BusinessID && group.Team == item.Transport {
					item.Title, item.Mapping = group.Name, "directory_matched"
					if group.AvatarURL != "" {
						item.AvatarURL = group.AvatarURL
					}
					break
				}
			}
		}
		if item.Title == "" {
			item.Title = item.BusinessID
			if item.Title == "0" {
				item.Title = "未支持的原生会话"
			}
		}
		items = append(items, item)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	groupOffset, err := pageNumber(r, "group_offset", 0, 100000)
	if err != nil {
		return err
	}
	groupTotal := len(directory.Groups)
	start := min(groupOffset, groupTotal)
	directory.Groups = directory.Groups[start:min(start+100, groupTotal)]
	send(w, 200, map[string]any{"items": items, "total": total, "limit": limit, "offset": offset,
		"directory": directory, "directory_total": groupTotal, "group_offset": groupOffset,
		"retention_days": 7, "read_only": true})
	return nil
}

var conversationPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)

func (s *Server) nativeMessages(w http.ResponseWriter, r *http.Request, p Principal) error {
	key := r.PathValue("conversation")
	if !conversationPattern.MatchString(key) {
		return fail(400, "invalid_conversation")
	}
	before := int64(9223372036854775807)
	var ids []int64
	if value := r.URL.Query().Get("ids"); value != "" {
		parts := strings.Split(value, ",")
		if len(parts) > 64 || r.URL.Query().Get("before") != "" {
			return fail(400, "invalid_pagination")
		}
		for _, part := range parts {
			id, err := strconv.ParseInt(part, 10, 64)
			if err != nil || id <= 0 {
				return fail(400, "invalid_pagination")
			}
			ids = append(ids, id)
		}
	}
	if value := r.URL.Query().Get("before"); value != "" {
		var err error
		before, err = strconv.ParseInt(value, 10, 64)
		if err != nil || before <= 0 {
			return fail(400, "invalid_pagination")
		}
	}
	rows, err := s.DB.Platform.Query(r.Context(), `SELECT event_id,received_at,state,reason,payload_sealed
        FROM native_decoded_events WHERE account_id=$1 AND conversation_key=$2
            AND received_at>now()-interval '7 days' AND state IN ('decoded','unsupported') AND event_id<$3
            AND ($4::bigint[] IS NULL OR event_id=ANY($4))
        ORDER BY event_id DESC LIMIT 65`, r.PathValue("id"), key, before, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	items := []map[string]any{}
	more := false
	for rows.Next() {
		if len(items) == 50 && ids == nil {
			more = true
			break
		}
		var id int64
		var at time.Time
		var state, reason string
		var sealed []byte
		if err = rows.Scan(&id, &at, &state, &reason, &sealed); err != nil {
			return err
		}
		plain, e := s.Vault.Open(gateway.DecodedAAD(r.PathValue("id"), id), sealed)
		if e != nil {
			return fail(503, "native_projection_unreadable")
		}
		var message gateway.DecodedMessage
		e = json.Unmarshal(plain, &message)
		clear(plain)
		if e != nil {
			return fail(503, "native_projection_unreadable")
		}
		text, truncated := boundedText(message.Message.Text, 8192)
		message.Message.Text = text
		if quote := message.Message.Quote; quote != nil {
			var clipped bool
			quote.Text, clipped = boundedText(quote.Text, 1024)
			truncated = truncated || clipped
		}
		if len(message.Message.Mentions) > 32 {
			message.Message.Mentions = message.Message.Mentions[:32]
			truncated = true
		}
		truncated = clipForward(message.Message.Forward) || truncated
		items = append(items, map[string]any{"id": strconv.FormatInt(id, 10), "received_at": at,
			"state": state, "reason": reason, "content": message, "display_truncated": truncated,
			"media": nativeMediaLinks(r.PathValue("id"), key, id, message.Message)})
	}
	if err = rows.Err(); err != nil {
		return err
	}
	rows.Close()
	if err = s.enrichChiliMessages(r.Context(), r.PathValue("id"), key, items); err != nil {
		return err
	}
	next := ""
	if len(items) > 0 && more {
		next = items[len(items)-1]["id"].(string)
	}
	send(w, 200, map[string]any{"items": items, "next_before": next, "read_only": true})
	return nil
}

func boundedText(text string, limit int) (string, bool) {
	count := 0
	for index := range text {
		if count == limit {
			return text[:index], true
		}
		count++
	}
	return text, false
}

func (s *Server) refreshNativeDirectory(w http.ResponseWriter, r *http.Request, p Principal) error {
	if s.Receiver == nil {
		return fail(503, "receiver_unavailable")
	}
	tag, err := s.DB.Platform.Exec(r.Context(), `INSERT INTO native_directory_snapshots(account_id)
        SELECT id FROM accounts WHERE id=$1 AND auth_state='authenticated'
        ON CONFLICT(account_id) DO UPDATE SET requested_at=now()
        WHERE native_directory_snapshots.attempted_at IS NULL
            OR native_directory_snapshots.attempted_at<now()-interval '30 seconds'`, r.PathValue("id"))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fail(429, "native_directory_cooldown")
	}
	send(w, 202, map[string]any{"state": "queued", "read_only": true})
	return nil
}

func (s *Server) decoderHealth(w http.ResponseWriter, r *http.Request, p Principal) error {
	out, err := s.readDecoderHealth(r)
	if err != nil {
		return err
	}
	send(w, 200, out)
	return nil
}

func (s *Server) readDecoderHealth(r *http.Request) (map[string]any, error) {
	var total, pending, processing, decoded, unsupported, failed int64
	var oldest, last *time.Time
	var p95 *float64
	var unsupportedHour, failedHour, notices int64
	err := s.DB.Platform.QueryRow(r.Context(), `SELECT count(*),
        count(*) FILTER(WHERE x.event_id IS NULL),
        count(*) FILTER(WHERE x.state='processing'),
        count(*) FILTER(WHERE x.state='decoded'),
        count(*) FILTER(WHERE x.state='unsupported'),
        count(*) FILTER(WHERE x.state='failed'),
        count(*) FILTER(WHERE x.state='unsupported' AND e.received_at>now()-interval '1 hour'),
        count(*) FILTER(WHERE x.state='failed' AND e.received_at>now()-interval '1 hour'),
        count(*) FILTER(WHERE x.state='decoded' AND x.reason='transport_notice'),
        min(e.received_at) FILTER(WHERE x.event_id IS NULL OR x.state='processing'),
        max(x.decoded_at)
        FROM native_received_events e LEFT JOIN native_decoded_events x ON x.event_id=e.id
        WHERE e.account_id=$1 AND e.kind IN ('group','private') AND e.received_at>now()-interval '7 days'`,
		r.PathValue("id")).Scan(&total, &pending, &processing, &decoded, &unsupported, &failed,
		&unsupportedHour, &failedHour, &notices, &oldest, &last)
	if err != nil {
		return nil, err
	}
	var processed60 int64
	if err = s.DB.Platform.QueryRow(r.Context(), `SELECT percentile_cont(0.95) WITHIN GROUP(ORDER BY decode_ms),
        (SELECT count(*) FROM native_decoded_events WHERE account_id=$1
            AND decoded_at>now()-interval '60 seconds' AND received_at>now()-interval '7 days')
        FROM (SELECT decode_ms,decoded_at FROM native_decoded_events WHERE account_id=$1
            AND decoded_at IS NOT NULL AND received_at>now()-interval '7 days'
            ORDER BY decoded_at DESC LIMIT 1024) x`, r.PathValue("id")).Scan(&p95, &processed60); err != nil {
		return nil, err
	}
	receiver, err := s.readReceiver(r, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	directory, err := s.readDirectory(r)
	if err != nil {
		return nil, err
	}
	directory.Groups = nil
	pool := gateway.DecoderSnapshot{Workers: gateway.DecoderWorkers, QueueCapacity: gateway.DecoderQueueCapacity}
	if s.Decoder != nil {
		pool = s.Decoder.Snapshot()
	}
	status := "healthy"
	if !pool.Observed {
		status = "unavailable"
	} else if pool.Error != "" || oldest != nil && time.Since(*oldest) > 30*time.Second {
		status = "blocked"
	} else if failed > 0 || unsupported > 0 {
		status = "degraded"
	} else if total == 0 {
		status = "idle"
	}
	reasons := map[string]int64{}
	rows, err := s.DB.Platform.Query(r.Context(), `SELECT reason,count(*) FROM native_decoded_events
        WHERE account_id=$1 AND state IN ('failed','unsupported') AND received_at>now()-interval '7 days'
        GROUP BY reason ORDER BY count(*) DESC LIMIT 16`, r.PathValue("id"))
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var reason string
		var n int64
		if err = rows.Scan(&reason, &n); err != nil {
			rows.Close()
			return nil, err
		}
		reasons[reason] = n
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	return map[string]any{"status": status, "sampled_at": time.Now().UTC(), "total": total,
		"pending": pending, "processing": processing, "decoded": decoded, "unsupported": unsupported,
		"failed": failed, "oldest_pending_at": oldest, "last_decoded_at": last, "decode_p95_ms": p95,
		"recent_hour":       map[string]int64{"unsupported": unsupportedHour, "failed": failedHour},
		"transport_notices": notices,
		"processed_60s":     processed60, "per_second": float64(processed60) / 60, "reasons": reasons,
		"pool": pool, "receiver": receiver, "directory": directory, "retention_days": 7,
		"native_ready": false, "ai_ready": false}, nil
}
