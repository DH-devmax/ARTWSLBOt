package httpapi

import (
	"context"
	"strconv"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/lajiaoliao"
)

// Explicit reads enrich only the bounded page already authorized for this
// conversation. They never rewrite historical decoder state or recovery cursors.
func (s *Server) enrichChiliMessages(ctx context.Context, account, conversation string, items []map[string]any) error {
	selected := map[int64]map[string]any{}
	ids := []int64{}
	for _, item := range items {
		message := item["content"].(gateway.DecodedMessage)
		if message.Platform != "lajiaoliao" {
			continue
		}
		id, _ := strconv.ParseInt(item["id"].(string), 10, 64)
		selected[id], ids = item, append(ids, id)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := s.DB.Platform.Query(ctx, `SELECT e.id,e.event_key,e.payload_sealed
		FROM native_received_events e JOIN accounts a ON a.id=e.account_id
		WHERE e.account_id=$1 AND a.platform='lajiaoliao' AND e.id=ANY($2)
		AND e.received_at>now()-interval '7 days' AND e.payload_sealed IS NOT NULL
		ORDER BY e.id DESC LIMIT 64`, account, ids)
	if err != nil {
		return err
	}
	defer rows.Close()
	bytesRead := 0
	for rows.Next() {
		var id int64
		var sourceKey string
		var sealed []byte
		if err = rows.Scan(&id, &sourceKey, &sealed); err != nil {
			return err
		}
		bytesRead += len(sealed)
		if len(sealed) > 256<<10 || bytesRead > 4<<20 {
			continue
		}
		raw, err := s.Vault.Open("native-event:"+account+":"+sourceKey, sealed)
		if err != nil {
			continue
		}
		view, err := lajiaoliao.ReadMessageView(raw)
		clear(raw)
		item := selected[id]
		message := item["content"].(gateway.DecodedMessage)
		if err != nil {
			item["native_view_error"] = "lajiaoliao_message_content_shape"
		} else if chiliMessageMatches(message, view) && view.View.Kind != "unsupported" {
			item["media"] = chiliMediaLinks(account, conversation, id, view)
			item["native_view"] = view.View
		}
	}
	return rows.Err()
}

func chiliMediaLinks(account, conversation string, event int64, message *lajiaoliao.ReadMessage) mediaLinks {
	base := "/api/v1/accounts/" + account + "/conversations/" + conversation +
		"/messages/" + strconv.FormatInt(event, 10) + "/media/"
	links := mediaLinks{}
	if message.Avatar.Valid() {
		links.Sender = base + "sender"
	}
	message.Visit(func(index int, view *lajiaoliao.MessageView) {
		for kind, resource := range view.Attachments {
			if resource.Valid() {
				if view.Media == nil {
					view.Media = map[string]string{}
				}
				view.Media[kind] = base + "node-" + strconv.Itoa(index) + "-" + kind
			}
		}
	})
	return links
}

func chiliMessageMatches(selected gateway.DecodedMessage, source *lajiaoliao.ReadMessage) bool {
	if source == nil || selected.Platform != "lajiaoliao" || selected.Message.ClientID != source.ID ||
		selected.NativeSender != source.Sender || selected.TransportPeer != source.Sender ||
		selected.TransportTo != source.Recipient {
		return false
	}
	if selected.Message.Session == 2 {
		return (source.SessionType == 2 || source.SessionType == 3) && selected.NativeTarget != "" &&
			selected.NativeTarget == source.Group
	}
	if selected.Message.Session != 1 || source.SessionType != 1 {
		return false
	}
	if selected.Outgoing {
		return selected.NativeTarget == source.Recipient
	}
	return selected.NativeTarget == source.Sender
}
