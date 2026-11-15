package lajiaoliao

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

func TestChiliRecallStrictEnvelopeAndOwnedStrings(t *testing.T) {
	detail := `{"revokerUserID":"actor","clientMsgID":"client","revokeTime":1700000000,
		"sesstionType":3,"seq":7,"conversationID":"sg_group","isAdminRevoke":true}`
	makeRaw := func(value string, session int32) []byte {
		t.Helper()
		wrapper, _ := json.Marshal(map[string]string{"detail": value})
		raw, err := proto.Marshal(&sdkws.MsgData{SendID: "sender", RecvID: "self", GroupID: "group",
			SessionType: session, ContentType: 2101, MsgFrom: 200, Content: wrapper})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	raw := makeRaw(detail, 3)
	got, err := ReadRecallNotice(raw)
	clear(raw)
	if err != nil || got == nil || got.Actor != "actor" || got.ClientID != "client" ||
		got.Target != "group" || got.Scene != 1 {
		t.Fatal("valid recall lost", err)
	}
	for _, bad := range []string{
		strings.Replace(detail, `"seq":7`, `"seq":0`, 1),
		strings.Replace(detail, `"seq":7`, `"seq":7,"seq":8`, 1),
		strings.Replace(detail, `"sesstionType":3`, `"sesstionType":1`, 1),
		strings.Replace(detail, `"actor"`, `"bad\nactor"`, 1),
		strings.Replace(detail, `"isAdminRevoke":true`, `"isAdminRevoke":true,"unknown":"raw"`, 1),
		detail + `{}`, detail[:len(detail)-2],
	} {
		if _, err := ReadRecallNotice(makeRaw(bad, 3)); err == nil {
			t.Fatal("ambiguous recall accepted")
		}
	}
	if _, err := ReadRecallNotice(makeRaw(detail, 1)); err == nil {
		t.Fatal("cross-session detail accepted")
	}
}
