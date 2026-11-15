package gateway

import (
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/lajiaoliao"
)

// OnboardingSource is consumed by control's one-time challenge verifier. It is
// never a customer-facing private-message history.
type OnboardingSource struct {
	Group, Sender, Recipient, Text, ClientID string
	Private                                 bool
}

func (m *ReceiverManager) ReadOnboardingSource(platform, account, self, key string, event int64,
	projection, source []byte) (OnboardingSource, error) {
	out := OnboardingSource{}
	if account == "" || self == "" || event < 1 || len(projection) > 256<<10 || len(source) > 256<<10 {
		return out, ErrMemberScope
	}
	if platform == "wsl" {
		raw, err := m.Vault.Open(DecodedAAD(account, event), projection)
		if err != nil {
			return out, ErrMemberScope
		}
		defer clear(raw)
		var decoded DecodedMessage
		if json.Unmarshal(raw, &decoded) != nil {
			return out, ErrMemberScope
		}
		msg := decoded.Message
		if decoded.Outgoing || msg.Format != 0 || msg.Session != 1 && msg.Session != 2 ||
			msg.Notice != nil || msg.Image != nil || msg.Voice != nil || msg.Video != nil ||
			msg.File != nil || msg.Forward != nil || msg.FriendCard != nil || msg.Quote != nil ||
			len(msg.Mentions) != 0 || msg.Sender.ID == 0 || msg.Target.ID == 0 {
			return out, ErrMemberScope
		}
		out = OnboardingSource{Sender: strconv.FormatUint(msg.Sender.ID, 10),
			Recipient: strconv.FormatUint(msg.Target.ID, 10), Text: msg.Text,
			ClientID: msg.ClientID, Private: msg.Session == 1}
		if out.Private {
			if out.Recipient != self {
				return OnboardingSource{}, ErrMemberScope
			}
		} else {
			out.Group = out.Recipient
		}
	} else if platform == "lajiaoliao" {
		raw, err := m.Vault.Open("native-event:"+account+":"+key, source)
		if err != nil {
			return out, ErrMemberScope
		}
		defer clear(raw)
		view, err := lajiaoliao.ReadMessageView(raw)
		if err != nil || view == nil || view.View == nil || view.View.Kind != "text" ||
			view.View.Truncated || view.SessionType != 1 && view.SessionType != 3 {
			return out, ErrMemberScope
		}
		var wire sdkws.MsgData
		if proto.Unmarshal(raw, &wire) != nil || wire.ContentType != 101 || wire.MsgFrom != 100 {
			return out, ErrMemberScope
		}
		defer clear(wire.Content)
		out = OnboardingSource{Group: view.Group, Sender: view.Sender, Recipient: wire.RecvID,
			Text: view.View.Text, ClientID: view.ID, Private: view.SessionType == 1}
		if out.Private && (out.Recipient != self || out.Group != "") ||
			!out.Private && (out.Group == "" || out.Recipient != out.Group) {
			return OnboardingSource{}, ErrMemberScope
		}
	} else {
		return out, ErrMemberScope
	}
	if out.Sender == "" || out.Sender == self || out.ClientID == "" || len(out.Text) > 160 ||
		strings.TrimSpace(out.Text) == "" || !utf8.ValidString(out.Text) {
		return OnboardingSource{}, ErrMemberScope
	}
	return out, nil
}
