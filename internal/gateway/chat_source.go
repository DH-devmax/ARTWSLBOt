package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

type ChatSource struct {
	Group, Sender, Text, ClientID, ReplyClientID string
	Mentions                                     []string
}

// ChatConversation resolves an existing sealed directory projection. It performs
// no upstream request and supplies routing only, never group authorization.
func (m *ReceiverManager) ChatConversation(ctx context.Context, platform, account, group string) (string, error) {
	if platform == "lajiaoliao" {
		return conversationKey("lajiaoliao:group", 0, group), nil
	}
	if platform != "wsl" {
		return "", ErrMemberScope
	}
	var sealed []byte
	if err := m.DB.QueryRow(ctx, `SELECT payload_sealed FROM native_directory_snapshots
		WHERE account_id=$1 AND sampled_at>clock_timestamp()-interval '10 minutes' AND error=''`, account).Scan(&sealed); err != nil {
		return "", err
	}
	raw, err := m.Vault.Open("native-directory:"+account, sealed)
	if err != nil {
		return "", err
	}
	defer clear(raw)
	var rows []nativeauth.DirectoryGroup
	if json.Unmarshal(raw, &rows) != nil {
		return "", ErrMemberScope
	}
	id, err := strconv.ParseUint(group, 10, 64)
	if err != nil {
		return "", err
	}
	for _, row := range rows {
		if row.ID == group && row.Team != "" {
			return conversationKey("group", id, row.Team), nil
		}
	}
	return "", ErrMemberDirectory
}

// ReadChatSource reuses authenticated projections and the existing Chili view
// parser. It does not change decoder states, format support or receive cursors.
func (m *ReceiverManager) ReadChatSource(platform, account, key string, event int64, projection, source []byte) (ChatSource, error) {
	var out ChatSource
	if len(source) > 256<<10 || len(projection) > 256<<10 {
		return out, ErrMemberScope
	}
	if platform == "wsl" {
		raw, err := m.Vault.Open(DecodedAAD(account, event), projection)
		if err != nil {
			return out, err
		}
		defer clear(raw)
		var decoded DecodedMessage
		if json.Unmarshal(raw, &decoded) != nil || decoded.Message.Session != 2 || decoded.Message.Format != 0 ||
			decoded.Message.Notice != nil || decoded.Message.Image != nil || decoded.Message.Voice != nil ||
			decoded.Message.Video != nil || decoded.Message.File != nil || decoded.Message.Forward != nil ||
			decoded.Message.FriendCard != nil {
			return out, ErrMemberScope
		}
		msg := decoded.Message
		out = ChatSource{Group: strconv.FormatUint(msg.Target.ID, 10), Sender: strconv.FormatUint(msg.Sender.ID, 10), Text: msg.Text, ClientID: msg.ClientID}
		for _, mention := range msg.Mentions {
			out.Mentions = append(out.Mentions, strconv.FormatUint(mention.UID, 10))
		}
		if msg.Quote != nil {
			out.ReplyClientID = msg.Quote.ClientID
		}
	} else if platform == "lajiaoliao" {
		raw, err := m.Vault.Open("native-event:"+account+":"+key, source)
		if err != nil {
			return out, err
		}
		defer clear(raw)
		message, err := lajiaoliao.ReadMessageView(raw)
		if err != nil || message == nil || message.View == nil || message.SessionType == 1 {
			return out, ErrMemberScope
		}
		view := message.View
		if view.Truncated || view.Kind != "text" && view.Kind != "mention" && view.Kind != "quote" {
			return out, ErrMemberScope
		}
		out = ChatSource{Group: message.Group, Sender: message.Sender, Text: view.Text, ClientID: message.ID}
		for _, mention := range view.Mentions {
			out.Mentions = append(out.Mentions, mention.ID)
		}
		if view.Kind == "quote" {
			var msg sdkws.MsgData
			if proto.Unmarshal(raw, &msg) != nil {
				return out, ErrMemberScope
			}
			defer clear(msg.Content)
			var content struct {
				Quote struct {
					ID string `json:"clientMsgID"`
				} `json:"quoteMessage"`
			}
			if json.Unmarshal(msg.Content, &content) != nil {
				return out, ErrMemberScope
			}
			out.ReplyClientID = content.Quote.ID
		}
	} else {
		return out, ErrMemberScope
	}
	if out.Group == "" || out.Sender == "" || out.ClientID == "" || strings.TrimSpace(out.Text) == "" ||
		len(out.Text) > 16384 || !utf8.ValidString(out.Text) {
		return ChatSource{}, errors.New("heat_chat_source_unsupported")
	}
	return out, nil
}
