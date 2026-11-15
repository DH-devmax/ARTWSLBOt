package lajiaoliao

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

func TestGroupNoticeProjectionBoundToOriginalGroup(t *testing.T) {
	person := map[string]any{"groupID": "group-native", "userID": "member-native", "nickname": "fixture",
		"ex": `{"secret":"synthetic-hidden"}`, "faceURL": "https://hidden.invalid/credential"}
	for _, kind := range []int32{1502, 1504, 1508, 1509, 1510, 1512, 1513, 1514, 1515, 1516, 1519, 1520, 1521, 1522} {
		detail := map[string]any{"group": map[string]any{"groupID": "group-native", "groupName": "fixture",
			"notification": "synthetic notice", "ex": "synthetic-hidden"}, "opUser": person,
			"quitUser": person, "entrantUser": person, "mutedUser": person, "changedUser": person,
			"kickedUserList": []any{person}, "invitedUserList": []any{person}, "mutedSeconds": 60}
		encoded, _ := json.Marshal(detail)
		content, _ := json.Marshal(map[string]string{"detail": string(encoded)})
		input, _ := proto.Marshal(&sdkws.MsgData{ClientMsgID: "client-native", SendID: "operator-native",
			RecvID: "group-native", GroupID: "group-native", SessionType: 3, MsgFrom: 200,
			ContentType: kind, Content: content})
		view, err := ReadMessageView(input)
		clear(input)
		if err != nil || view == nil || view.View.Kind != "notice" || view.View.Notice.Group != "group-native" {
			t.Fatal("group notice lost", kind, err)
		}
		output, _ := json.Marshal(view.View)
		if strings.Contains(string(output), "synthetic-hidden") || strings.Contains(string(output), "credential") {
			t.Fatal("opaque group metadata leaked")
		}
		if kind != 1519 && kind != 1520 && view.View.Text != "" {
			t.Fatal("full snapshot mistaken for a changed field")
		}
		detail["group"] = map[string]any{"groupID": "other-group"}
		encoded, _ = json.Marshal(detail)
		content, _ = json.Marshal(map[string]string{"detail": string(encoded)})
		if _, err := readGroupNotice(kind, content, "group-native"); err == nil {
			t.Fatal("cross-group notice accepted", kind)
		}
	}
	if _, err := readGroupNotice(1502, []byte(`{"detail":"{}","detail":"{}"}`), "group-native"); err == nil {
		t.Fatal("duplicate JSON field accepted")
	}
}
