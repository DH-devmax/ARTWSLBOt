package gateway

import (
	"context"
	"encoding/json"
	"strconv"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
)

type AssistantReceipt struct {
	EventID    int64
	NativeID   string
	ObservedAt time.Time
}

// A receipt is a matching durable native identifier, never a text search or an
// upstream request. Control must authorize the group before invoking this port.
func (m *ReceiverManager) AssistantReceipt(ctx context.Context, platform, account, group, client, sender string, at time.Time) (AssistantReceipt, error) {
	work, cancel := context.WithTimeout(ctx, time.Second)
	defer cancel()
	rows, err := m.DB.Query(work, `SELECT d.event_id,d.account_id::text,d.payload_sealed,d.received_at,
  coalesce(sx.event_key,''),sx.payload_sealed FROM native_decoded_events d
  JOIN native_received_events e ON e.id=d.event_id AND e.account_id=d.account_id
  JOIN accounts a ON a.id=d.account_id JOIN account_groups ag ON ag.account_id=a.id
  JOIN native_groups g ON g.id=ag.group_id AND g.platform=a.platform
  LEFT JOIN native_received_events sx ON a.platform='lajiaoliao' AND e.event_key LIKE 'lajiao:message:%'
  AND sx.account_id=e.account_id AND sx.kind='notice'
  AND sx.event_key=replace(e.event_key,'lajiao:message:','lajiao:sequence:')
  AND sx.received_at>clock_timestamp()-interval '7 days' AND sx.payload_sealed IS NOT NULL
  WHERE a.platform=$1 AND g.native_id=$2 AND d.state='decoded' AND d.scene='group'
  AND d.received_at BETWEEN $3::timestamptz-interval '1 minute' AND $3::timestamptz+interval '5 minutes'
  AND d.payload_sealed IS NOT NULL
  ORDER BY (d.received_at<$3::timestamptz),d.received_at,d.event_id LIMIT 100`, platform, group, at)
	if err != nil {
		return AssistantReceipt{}, err
	}
	defer rows.Close()
	total := 0
	for rows.Next() {
		var r AssistantReceipt
		var recipient, sequenceKey string
		var sealed, sequenceSource []byte
		if err = rows.Scan(&r.EventID, &recipient, &sealed, &r.ObservedAt, &sequenceKey, &sequenceSource); err != nil {
			return AssistantReceipt{}, err
		}
		total += len(sealed) + len(sequenceSource)
		if total > 2<<20 {
			break
		}
		if len(sealed) > 256<<10 || len(sequenceSource) > 128<<10+64 {
			continue
		}
		raw, e := m.Vault.Open(DecodedAAD(recipient, r.EventID), sealed)
		if e != nil {
			continue
		}
		var d DecodedMessage
		e = json.Unmarshal(raw, &d)
		clear(raw)
		if e != nil {
			continue
		}
		if platform == "lajiaoliao" && len(sequenceSource) > 0 {
			raw, e = m.Vault.Open("native-event:"+recipient+":"+sequenceKey, sequenceSource)
			if e != nil {
				continue
			}
			var message sdkws.MsgData
			e = proto.Unmarshal(raw, &message)
			clear(raw)
			clear(message.Content)
			if e != nil || message.Seq < 1 || message.SessionType != 3 ||
				message.ClientMsgID != d.Message.ClientID || message.SendID != d.NativeSender ||
				message.GroupID != d.NativeTarget || message.SendTime < 1 ||
				uint64(message.SendTime) != d.Message.CreatedAt || !lajiaoliao.ValidID(message.ServerMsgID) {
				continue
			}
			d.ServerID = message.ServerMsgID
		}
		if !matchingAssistantReceipt(d, platform, group, client, sender) {
			continue
		}
		r.NativeID = d.ServerID
		return r, nil
	}
	return AssistantReceipt{}, rows.Err()
}

func matchingAssistantReceipt(d DecodedMessage, platform, group, client, sender string) bool {
	if client == "" || sender == "" || d.Message.ClientID != client || d.Message.Session != 2 {
		return false
	}
	if platform == "wsl" {
		return (d.Platform == "" || d.Platform == platform) && strconv.FormatUint(d.Message.Target.ID, 10) == group && strconv.FormatUint(d.Message.Sender.ID, 10) == sender && d.ServerID != ""
	}
	return platform == "lajiaoliao" && d.Platform == platform && d.NativeTarget == group && d.NativeSender == sender && d.ServerID != ""
}
