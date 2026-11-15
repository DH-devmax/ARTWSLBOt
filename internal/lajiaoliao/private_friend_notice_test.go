package lajiaoliao

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

func TestPrivateFriendNoticeRoutePinnedMetadataOnly(t *testing.T) {
	for _, kind := range []int32{1201, 1204} {
		for _, reverse := range []bool{false, true} {
			m := &sdkws.MsgData{ClientMsgID: "synthetic-notice", SendID: "sender-string", RecvID: "receiver-string",
				SessionType: 1, MsgFrom: 200, ContentType: kind,
				Content: []byte(`{"private":"synthetic-notice-body"}`), SenderNickname: "synthetic-name"}
			if reverse {
				m.SendID, m.RecvID = m.RecvID, m.SendID
			}
			raw, err := proto.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			route, err := ReadPrivateFriendNoticeRoute(raw)
			if err != nil || route.ClientID != m.ClientMsgID || route.Sender != m.SendID || route.Recipient != m.RecvID {
				t.Fatal("pinned friend notice lost its route", route, err)
			}
			clear(raw)
			if route.ClientID != m.ClientMsgID || route.Sender != m.SendID || route.Recipient != m.RecvID {
				t.Fatal("route retained caller-owned source bytes")
			}
			projection, err := json.Marshal(route)
			if err != nil || strings.Contains(string(projection), "synthetic-notice-body") ||
				strings.Contains(string(projection), "synthetic-name") {
				t.Fatal("route exposed notice content or profile fields")
			}
			raw, err = proto.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ReadOutboundSource(raw); err == nil {
				t.Fatal("ordinary outbound source accepted a friend notice")
			}
		}
	}
}

func TestPrivateFriendNoticeRouteRejectsOtherShapes(t *testing.T) {
	cases := []struct {
		name   string
		change func(*sdkws.MsgData)
	}{
		{"ordinary-message", func(m *sdkws.MsgData) { m.MsgFrom = 100 }},
		{"read-receipt", func(m *sdkws.MsgData) { m.ContentType = 2200 }},
		{"friend-application", func(m *sdkws.MsgData) { m.ContentType = 1202 }},
		{"unknown-type", func(m *sdkws.MsgData) { m.ContentType = 0 }},
		{"group-session", func(m *sdkws.MsgData) { m.SessionType = 3 }},
		{"group-routing", func(m *sdkws.MsgData) { m.GroupID = "other-group" }},
		{"missing-client", func(m *sdkws.MsgData) { m.ClientMsgID = "" }},
		{"missing-sender", func(m *sdkws.MsgData) { m.SendID = "" }},
		{"missing-recipient", func(m *sdkws.MsgData) { m.RecvID = "" }},
		{"same-party", func(m *sdkws.MsgData) { m.RecvID = m.SendID }},
		{"invalid-client", func(m *sdkws.MsgData) { m.ClientMsgID = strings.Repeat("a", 201) }},
		{"large-content", func(m *sdkws.MsgData) { m.Content = make([]byte, 65537) }},
	}
	for _, row := range cases {
		t.Run(row.name, func(t *testing.T) {
			m := &sdkws.MsgData{ClientMsgID: "synthetic-notice", SendID: "sender-string", RecvID: "receiver-string",
				SessionType: 1, MsgFrom: 200, ContentType: 1201}
			row.change(m)
			raw, err := proto.Marshal(m)
			if err != nil {
				t.Fatal(err)
			}
			if _, err = ReadPrivateFriendNoticeRoute(raw); err == nil {
				t.Fatal("unsupported notice supplied a private route")
			}
		})
	}
	for _, raw := range [][]byte{{0xff}, make([]byte, 128<<10+1)} {
		if _, err := ReadPrivateFriendNoticeRoute(raw); err == nil {
			t.Fatal("malformed or oversized notice supplied a route")
		}
	}
}
