package lajiaoliao

import (
	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

// PrivateFriendNoticeRoute contains routing metadata only. A friend notice is
// neither a readable chat message nor evidence of an onboarding challenge.
type PrivateFriendNoticeRoute struct {
	ClientID, Sender, Recipient string
}

// ReadPrivateFriendNoticeRoute accepts only the pinned SDK's friend-approved
// (1201) and friend-added (1204) private notifications. Control must independently
// authorize the exact onboarding proof window before using the route.
func ReadPrivateFriendNoticeRoute(raw []byte) (PrivateFriendNoticeRoute, error) {
	var m sdkws.MsgData
	if len(raw) > 128<<10 || proto.Unmarshal(raw, &m) != nil {
		return PrivateFriendNoticeRoute{}, &Failure{Code: "lajiaoliao_message_shape"}
	}
	defer clear(m.Content)
	if m.MsgFrom != 200 || m.SessionType != 1 ||
		m.ContentType != 1201 && m.ContentType != 1204 ||
		!ValidID(m.ClientMsgID) || !ValidID(m.SendID) || !ValidID(m.RecvID) ||
		m.SendID == m.RecvID || m.GroupID != "" || len(m.Content) > 65536 {
		return PrivateFriendNoticeRoute{}, &Failure{Code: "lajiaoliao_message_shape"}
	}
	return PrivateFriendNoticeRoute{ClientID: m.ClientMsgID, Sender: m.SendID, Recipient: m.RecvID}, nil
}
