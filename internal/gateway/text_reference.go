package gateway

import (
	"context"
	"encoding/json"
	"strconv"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func (m *ReceiverManager) resolveTextReply(ctx context.Context, intent TextIntent, key []byte, team string,
	roster nativeauth.MemberDirectory, version int64) (*nativewire.ReplyRoute, error) {
	var sealed []byte
	var eventKey string
	err := m.DB.QueryRow(ctx, `SELECT e.payload_sealed,e.event_key FROM native_received_events e
        JOIN native_decoded_events d ON d.event_id=e.id AND d.account_id=e.account_id
        WHERE e.id=$1 AND e.account_id=$2 AND e.kind='group' AND d.scene='group' AND d.state='decoded'
        AND e.payload_sealed IS NOT NULL AND d.payload_sealed IS NOT NULL AND d.deployment_version=$3
        AND e.received_at>clock_timestamp()-interval '7 days'+interval '3 seconds'
        AND d.received_at>clock_timestamp()-interval '7 days'+interval '3 seconds'
        AND e.received_at<=clock_timestamp() AND d.received_at<=clock_timestamp()`,
		intent.QuoteEventID, intent.AccountID, version).Scan(&sealed, &eventKey)
	if err != nil {
		return nil, ErrTextRoute
	}
	plain, err := m.Vault.Open("native-event:"+intent.AccountID+":"+eventKey, sealed)
	if err != nil {
		return nil, ErrTextRoute
	}
	var raw nativewire.IMMessage
	err = json.Unmarshal(plain, &raw)
	clear(plain)
	if err != nil {
		return nil, ErrTextRoute
	}
	return verifyTextReply(intent.Message, key, team, roster, raw)
}

func verifyTextReply(message nativewire.GroupText, key []byte, team string, roster nativeauth.MemberDirectory,
	raw nativewire.IMMessage) (*nativewire.ReplyRoute, error) {
	if message.Quote == nil || !roster.Complete || roster.Group != strconv.FormatUint(message.GroupID, 10) ||
		raw.Scene != 1 || raw.Target != team || raw.Fields[0] != "1" || raw.Fields[1] != team ||
		raw.Fields[2] != raw.Sender || raw.Fields[8] != "100" || raw.Fields[12] != raw.ServerID {
		return nil, ErrTextRoute
	}
	original, err := nativewire.OpenMessage(key, raw.Fields[10])
	if err != nil || original.Session != 2 || original.Format != 0 || original.Target.ID != message.GroupID ||
		original.ClientID != raw.Fields[11] || original.ClientID != message.Quote.ClientID ||
		raw.ServerID != message.Quote.ServerID || original.Text != message.Quote.Text {
		return nil, ErrTextRoute
	}
	matched := false
	for _, member := range roster.Members {
		if member.ID != strconv.FormatUint(original.Sender.ID, 10) {
			continue
		}
		if matched || member.Transport != raw.Sender {
			return nil, ErrTextRoute
		}
		matched = true
	}
	if !matched {
		return nil, ErrTextRoute
	}
	route := &nativewire.ReplyRoute{Sender: raw.Sender, Target: team, TimeMS: raw.Fields[7],
		ServerID: raw.ServerID, ClientID: original.ClientID}
	check, err := nativewire.GroupTextReplyRequest(team, message.ClientID, "reference-validation", route)
	clear(check)
	if err != nil {
		return nil, ErrTextRoute
	}
	return route, nil
}
