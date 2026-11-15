package lajiaoliao

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

const friendNoticePair = `{"fromUserID":"sender-string","toUserID":"receiver-string"}`

func friendNoticeWrapper(t testing.TB, detail string) []byte {
	t.Helper()
	content, err := json.Marshal(map[string]string{"detail": detail})
	if err != nil {
		t.Fatal(err)
	}
	return content
}

func friendNoticeFixture(t testing.TB, kind int32, content []byte) *sdkws.MsgData {
	t.Helper()
	return &sdkws.MsgData{ClientMsgID: "synthetic-friend-notice", SendID: "sender-string",
		RecvID: "receiver-string", MsgFrom: 200, SessionType: 1, ContentType: kind, Content: content,
		SenderNickname: "synthetic-hidden-envelope", SenderFaceURL: "https://hidden.invalid/avatar"}
}

func friendNoticeWire(t testing.TB, message *sdkws.MsgData) []byte {
	t.Helper()
	raw, err := proto.Marshal(message)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func friendAddedDetail(t testing.TB, owner, user, name string) string {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"friend": map[string]any{"ownerUserID": owner, "remark": "synthetic-hidden-remark", "createTime": 1,
			"friendUser": map[string]any{"userID": user, "nickname": name, "faceURL": "https://hidden.invalid/friend",
				"ex": "synthetic-hidden-user", "createTime": 1, "appMangerLevel": 0, "globalRecvMsgOpt": 0},
			"addSource": 0, "operatorUserID": owner, "ex": "synthetic-hidden-friend", "isPinned": false},
		"operationTime": 1, "opUser": map[string]string{"userID": owner, "nickname": "synthetic-hidden-operator",
			"faceURL": "https://hidden.invalid/operator", "ex": "synthetic-hidden-operator-ex"},
		"friendVersion": 1, "friendVersionID": "synthetic-hidden-version",
	})
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func friendRequestSnapshot() *sdkws.FriendRequest {
	return &sdkws.FriendRequest{FromUserID: "sender-string", ToUserID: "receiver-string",
		FromNickname: "sender-name", ToNickname: "recipient-name",
		FromFaceURL: "https://hidden.invalid/from", ToFaceURL: "https://hidden.invalid/to",
		HandleResult: 1, ReqMsg: "synthetic-hidden-verification", CreateTime: 1,
		HandlerUserID: "receiver-string", HandleMsg: "synthetic-hidden-handling", HandleTime: 2,
		Ex: "synthetic-hidden-extension"}
}

func friendApplicationDetail(t testing.TB, kind int32, request *sdkws.FriendRequest) string {
	t.Helper()
	pair := &sdkws.FromToUserID{FromUserID: "sender-string", ToUserID: "receiver-string"}
	var detail any = &sdkws.FriendApplicationTips{FromToUserID: pair, Request: request}
	if kind == 1201 {
		pair.FromUserID, pair.ToUserID = pair.ToUserID, pair.FromUserID
		detail = &sdkws.FriendApplicationApprovedTips{FromToUserID: pair,
			HandleMsg: "synthetic-hidden-verification", FriendVersion: 1,
			FriendVersionID: "synthetic-hidden-version", Request: request}
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func friendRequestClientDetail(t testing.TB, kind int32, extensions map[string]any) string {
	t.Helper()
	var detail map[string]any
	if err := json.Unmarshal([]byte(friendApplicationDetail(t, kind, friendRequestSnapshot())), &detail); err != nil {
		t.Fatal(err)
	}
	request := detail["request"].(map[string]any)
	for key, value := range extensions {
		request[key] = value
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	return string(raw)
}

func TestFriendNoticePinnedSchemasAndSafeOwnedProjection(t *testing.T) {
	rows := []struct {
		name, detail, event, senderName, recipientName string
		kind                                           int32
	}{
		{name: "approved-minimal", kind: 1201, event: "friend_approved",
			detail: `{"fromToUserID":` + friendNoticePair + `}`},
		{name: "approved-reverse-request", kind: 1201, event: "friend_approved",
			detail: `{"fromToUserID":{"fromUserID":"receiver-string","toUserID":"sender-string"},` +
				`"handleMsg":"synthetic-hidden-verification","friendVersion":1,"friendVersionID":"synthetic-hidden-version"}`},
		{name: "incoming-application", kind: 1203, event: "friend_request",
			detail: `{"fromToUserID":` + friendNoticePair + `}`},
		{name: "approved-five-keys-null-request", kind: 1201, event: "friend_approved",
			detail: friendApplicationDetail(t, 1201, nil)},
		{name: "approved-five-keys-request", kind: 1201, event: "friend_approved",
			detail:     friendApplicationDetail(t, 1201, friendRequestSnapshot()),
			senderName: "sender-name", recipientName: "recipient-name"},
		{name: "application-two-keys-request", kind: 1203, event: "friend_request",
			detail:     friendApplicationDetail(t, 1203, friendRequestSnapshot()),
			senderName: "sender-name", recipientName: "recipient-name"},
		{name: "added-minimal", kind: 1204, event: "friend_added",
			detail: `{"friend":{"ownerUserID":"sender-string","friendUser":{"userID":"receiver-string"}},"opUser":null}`},
		{name: "added-recipient-name", kind: 1204, event: "friend_added", recipientName: "\u63a5\u6536\u65b9",
			detail: friendAddedDetail(t, "sender-string", "receiver-string", "\u63a5\u6536\u65b9")},
		{name: "added-sender-name", kind: 1204, event: "friend_added", senderName: "\u53d1\u9001\u65b9",
			detail: friendAddedDetail(t, "receiver-string", "sender-string", "\u53d1\u9001\u65b9")},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			raw := friendNoticeWire(t, friendNoticeFixture(t, row.kind, friendNoticeWrapper(t, row.detail)))
			message, err := ReadMessageView(raw)
			clear(raw)
			if err != nil || message == nil || message.ID != "synthetic-friend-notice" ||
				message.Sender != "sender-string" || message.Recipient != "receiver-string" ||
				message.Group != "" || message.SessionType != 1 {
				t.Fatal("friend notice lost its private envelope", message, err)
			}
			want := &MessageView{Kind: "friend_notice", ContentType: row.kind, FriendNotice: &FriendNotice{
				Event: row.event, Sender: MessagePerson{ID: "sender-string", Name: row.senderName},
				Recipient: MessagePerson{ID: "receiver-string", Name: row.recipientName}}}
			if !reflect.DeepEqual(message.View, want) {
				t.Fatal("friend notice projected fields beyond the bounded display", message.View)
			}
			output, err := json.Marshal(message.View)
			if err != nil || !utf8.Valid(output) || bytes.Contains(output, []byte("synthetic-hidden")) ||
				bytes.Contains(output, []byte("https://")) {
				t.Fatal("friend notice exposed a verification note, raw metadata or resource", err)
			}
		})
	}
}

func TestFriendAddedSelfSyncKeepsEnvelopeRouteAndBounds(t *testing.T) {
	for _, row := range []struct {
		name, detail, partner, nickname string
	}{
		{"pinned-five-keys", friendAddedDetail(t, "sender-string", "partner-string", "partner-name"),
			"partner-string", "partner-name"},
		{"minimal", `{"friend":{"ownerUserID":"sender-string","friendUser":{"userID":"partner-string"}}}`,
			"partner-string", ""},
		{"exact-id-name-bounds", friendAddedDetail(t, "sender-string", strings.Repeat("p", 128),
			strings.Repeat("\u754c", 333)+"a"), strings.Repeat("p", 128), strings.Repeat("\u754c", 333) + "a"},
	} {
		t.Run(row.name, func(t *testing.T) {
			m := friendNoticeFixture(t, 1204, friendNoticeWrapper(t, row.detail))
			m.RecvID = m.SendID
			raw := friendNoticeWire(t, m)
			message, err := ReadMessageView(raw)
			clear(raw)
			want := &MessageView{Kind: "friend_notice", ContentType: 1204, FriendNotice: &FriendNotice{
				Event: "friend_added", Sender: MessagePerson{ID: m.SendID}, Recipient: MessagePerson{ID: m.RecvID},
				Friend: &MessagePerson{ID: row.partner, Name: row.nickname}}}
			if err != nil || message == nil || message.ID != m.ClientMsgID ||
				message.Sender != m.SendID || message.Recipient != m.RecvID ||
				message.Group != "" || message.SessionType != 1 || !reflect.DeepEqual(message.View, want) {
				t.Fatal("self-sync changed the envelope or lost its bounded partner", message, err)
			}
			output, err := json.Marshal(message.View)
			if err != nil || !utf8.Valid(output) || len(output) > 16<<10 ||
				bytes.Contains(output, []byte("synthetic-hidden")) || bytes.Contains(output, []byte("https://")) {
				t.Fatal("self-sync exposed upstream notes or resources", err)
			}
			if _, err := ReadPrivateFriendNoticeRoute(friendNoticeWire(t, m)); err == nil {
				t.Fatal("display expanded the metadata-only friend route to self-sync")
			}
			if _, err := ReadOutboundSource(friendNoticeWire(t, m)); err == nil {
				t.Fatal("self-sync display became an ordinary outbound source")
			}
		})
	}
	for _, row := range []struct {
		name, detail string
	}{
		{"foreign-owner", friendAddedDetail(t, "foreign-owner", "partner-string", "fixture")},
		{"self-is-partner", friendAddedDetail(t, "partner-string", "sender-string", "fixture")},
		{"self-pair", friendAddedDetail(t, "sender-string", "sender-string", "fixture")},
		{"empty-partner", friendAddedDetail(t, "sender-string", "", "fixture")},
		{"long-partner", friendAddedDetail(t, "sender-string", strings.Repeat("p", 129), "fixture")},
		{"invalid-partner", friendAddedDetail(t, "sender-string", "bad\rid", "fixture")},
		{"long-name", friendAddedDetail(t, "sender-string", "partner-string", strings.Repeat("n", 1001))},
		{"control-name", friendAddedDetail(t, "sender-string", "partner-string", "name\x00")},
		{"unknown-field", `{"friend":{"ownerUserID":"sender-string","friendUser":{"userID":"partner-string","extra":true}}}`},
		{"duplicate-owner", `{"friend":{"ownerUserID":"sender-string","ownerUserID":"sender-string","friendUser":{"userID":"partner-string"}}}`},
		{"duplicate-partner", `{"friend":{"ownerUserID":"sender-string","friendUser":{"userID":"partner-string","userID":"other"}}}`},
		{"invalid-utf8", "{\"friend\":{\"ownerUserID\":\"sender-string\",\"friendUser\":{\"userID\":\"partner-string\",\"nickname\":\"\xff\"}}}"},
	} {
		t.Run(row.name, func(t *testing.T) {
			content := friendNoticeWrapper(t, row.detail)
			if row.name == "invalid-utf8" {
				content = []byte(`{"detail":"{\"friend\":{\"ownerUserID\":\"sender-string\",\"friendUser\":{\"userID\":\"partner-string\",\"nickname\":\"` + "\xff" + `\"}}}"}`)
			}
			m := friendNoticeFixture(t, 1204, content)
			m.RecvID = m.SendID
			message, err := ReadMessageView(friendNoticeWire(t, m))
			if err != nil || message == nil || message.Sender != m.SendID || message.Recipient != m.RecvID ||
				!reflect.DeepEqual(message.View, &MessageView{Kind: "unsupported", ContentType: 1204}) {
				t.Fatal("unverified self-sync shape or partner was displayed", message, err)
			}
		})
	}
}

func TestFriendNoticeUnknownSchemasKeepNativeType(t *testing.T) {
	for _, row := range []struct {
		name, detail string
		kind         int32
	}{
		{"approved-unknown-field", `{"fromToUserID":` + friendNoticePair + `,"extra":"synthetic-hidden"}`, 1201},
		{"approved-invalid-request-schema", `{"fromToUserID":` + friendNoticePair + `,"request":{}}`, 1201},
		{"request-approval-schema", `{"fromToUserID":` + friendNoticePair + `,"handleMsg":"synthetic-hidden"}`, 1203},
		{"request-added-schema", `{"friend":{"ownerUserID":"sender-string"}}`, 1203},
		{"added-application-schema", `{"fromToUserID":` + friendNoticePair + `}`, 1204},
		{"added-unknown-friend-field", `{"friend":{"ownerUserID":"sender-string","extra":true,"friendUser":{"userID":"receiver-string"}}}`, 1204},
		{"added-unknown-user-field", `{"friend":{"ownerUserID":"sender-string","friendUser":{"userID":"receiver-string","extra":true}}}`, 1204},
		{"added-unknown-operator-field", `{"friend":{"ownerUserID":"sender-string","friendUser":{"userID":"receiver-string"}},"opUser":{"extra":true}}`, 1204},
		{"added-numeric-name", `{"friend":{"ownerUserID":"sender-string","friendUser":{"userID":"receiver-string","nickname":7}}}`, 1204},
		{"approved-numeric-handle", `{"fromToUserID":` + friendNoticePair + `,"handleMsg":7}`, 1201},
		{"approved-invalid-version", `{"fromToUserID":` + friendNoticePair + `,"friendVersion":-1}`, 1201},
		{"added-invalid-time", `{"friend":{"ownerUserID":"sender-string","friendUser":{"userID":"receiver-string"}},"operationTime":[]}`, 1204},
		{"missing-pair", `{}`, 1201},
		{"null-pair", `{"fromToUserID":null}`, 1201},
		{"array-pair", `{"fromToUserID":[]}`, 1203},
		{"scalar-detail", `"synthetic-hidden"`, 1201},
		{"array-detail", `[]`, 1203},
		{"null-detail", `null`, 1204},
		{"unknown-notification", `{"content":"synthetic-hidden"}`, 1299},
		{"unadapted-rejection", `{"fromToUserID":` + friendNoticePair + `}`, 1202},
		{"system-text", `{"content":"synthetic-hidden"}`, 101},
	} {
		t.Run(row.name, func(t *testing.T) {
			raw := friendNoticeWire(t, friendNoticeFixture(t, row.kind, friendNoticeWrapper(t, row.detail)))
			message, err := ReadMessageView(raw)
			want := &MessageView{Kind: "unsupported", ContentType: row.kind}
			if err != nil || message == nil || !reflect.DeepEqual(message.View, want) {
				t.Fatal("unrecognized schema became readable or lost its native type", message, err)
			}
		})
	}
	for _, content := range [][]byte{
		[]byte(`{"fromToUserID":` + friendNoticePair + `}`),
		[]byte(`{"detail":{}}`), []byte(`{"detail":null}`), []byte(`{"detail":[]}`),
		[]byte(`{"detail":"{}","text":"synthetic-hidden"}`), []byte(`{"detail":"{}"} {}`),
	} {
		message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 1201, content)))
		if err != nil || message == nil || !reflect.DeepEqual(message.View, &MessageView{Kind: "unsupported", ContentType: 1201}) {
			t.Fatal("non-pinned detail wrapper was displayed", message, err)
		}
	}
}

func TestFriendNoticeRequestSchemaRejectsAmbiguityIdentityUTF8AndBounds(t *testing.T) {
	for _, kind := range []int32{1201, 1203} {
		for _, row := range []struct {
			name   string
			change func(*sdkws.FriendRequest)
		}{
			{"foreign-sender", func(r *sdkws.FriendRequest) { r.FromUserID = "foreign" }},
			{"foreign-recipient", func(r *sdkws.FriendRequest) { r.ToUserID = "foreign" }},
			{"self-request", func(r *sdkws.FriendRequest) { r.ToUserID = r.FromUserID }},
			{"missing-sender", func(r *sdkws.FriendRequest) { r.FromUserID = "" }},
			{"long-sender", func(r *sdkws.FriendRequest) { r.FromUserID = strings.Repeat("s", 129) }},
			{"long-sender-name", func(r *sdkws.FriendRequest) { r.FromNickname = strings.Repeat("s", 1001) }},
			{"long-recipient-name", func(r *sdkws.FriendRequest) { r.ToNickname = strings.Repeat("r", 1001) }},
			{"control-name", func(r *sdkws.FriendRequest) { r.FromNickname = "name\x00" }},
			{"long-request-note", func(r *sdkws.FriendRequest) { r.ReqMsg = strings.Repeat("n", 8193) }},
			{"long-handle-note", func(r *sdkws.FriendRequest) { r.HandleMsg = strings.Repeat("n", 8193) }},
			{"long-extension", func(r *sdkws.FriendRequest) { r.Ex = strings.Repeat("e", 8193) }},
			{"long-face-url", func(r *sdkws.FriendRequest) { r.ToFaceURL = strings.Repeat("u", 8193) }},
			{"negative-created", func(r *sdkws.FriendRequest) { r.CreateTime = -1 }},
			{"negative-handled", func(r *sdkws.FriendRequest) { r.HandleTime = -1 }},
			{"unknown-result", func(r *sdkws.FriendRequest) { r.HandleResult = 2 }},
			{"invalid-handler", func(r *sdkws.FriendRequest) { r.HandlerUserID = "bad\rid" }},
		} {
			t.Run(row.name, func(t *testing.T) {
				request := friendRequestSnapshot()
				row.change(request)
				message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, kind,
					friendNoticeWrapper(t, friendApplicationDetail(t, kind, request)))))
				if err != nil || message == nil ||
					!reflect.DeepEqual(message.View, &MessageView{Kind: "unsupported", ContentType: kind}) {
					t.Fatal("unbounded or mismatched request was displayed", kind, message, err)
				}
			})
		}
	}
	for _, snapshot := range []string{
		`{"fromUserID":"sender-string","toUserID":"receiver-string","fromUserID":"sender-string"}`,
		`{"fromUserID":"sender-string","toUserID":"receiver-string","reqMsg":"one","reqMsg":"two"}`,
		`{"fromUserID":"sender-string","toUserID":"receiver-string","extra":true}`,
		`{"fromUserID":"sender-string","toUserID":"receiver-string","fromNickname":7}`,
		`{"fromUserID":"sender-string","toUserID":"receiver-string","reqMsg":{}}`,
		`{"fromUserID":"sender-string","toUserID":"receiver-string","createTime":1.5}`,
		`[]`, `"synthetic-hidden"`,
	} {
		detail := `{"fromToUserID":` + friendNoticePair + `,"request":` + snapshot + `}`
		message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 1201, friendNoticeWrapper(t, detail))))
		if err != nil || message == nil || message.View.Kind != "unsupported" {
			t.Fatal("unknown, duplicate or mistyped request snapshot accepted", message, err)
		}
	}
	content := friendNoticeWrapper(t, friendApplicationDetail(t, 1201, friendRequestSnapshot()))
	content = bytes.Replace(content, []byte("sender-name"), []byte{'n', 0xff}, 1)
	message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 1201, content)))
	if err != nil || message == nil || message.View.Kind != "unsupported" {
		t.Fatal("invalid UTF-8 request name accepted", message, err)
	}
	request := friendRequestSnapshot()
	request.FromNickname, request.ToNickname = strings.Repeat("s", 1000), strings.Repeat("r", 1000)
	request.ReqMsg, request.HandleMsg = strings.Repeat("n", 8192), strings.Repeat("h", 8192)
	message, err = ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 1201,
		friendNoticeWrapper(t, friendApplicationDetail(t, 1201, request)))))
	if err != nil || message == nil || message.View.Kind != "friend_notice" ||
		len(message.View.FriendNotice.Sender.Name) != 1000 || message.View.Text != "" {
		t.Fatal("exact request bounds were not retained privately", message, err)
	}
	request.FromNickname, request.ToNickname = strings.Repeat("<", 1000), strings.Repeat(">", 1000)
	message, err = ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 1201,
		friendNoticeWrapper(t, friendApplicationDetail(t, 1201, request)))))
	if err != nil || message == nil || message.View.Kind != "friend_notice" {
		t.Fatal("bounded names requiring JSON escaping were rejected", message, err)
	}
	output, err := json.Marshal(message.View)
	if err != nil || len(output) > 16<<10 {
		t.Fatal("escaped display names exceeded their serialized bound", len(output), err)
	}
}

func TestFriendNoticeClientRequestExtensionsStayBoundedAndPrivate(t *testing.T) {
	for _, kind := range []int32{1201, 1203} {
		for _, expired := range []bool{false, true} {
			extensions := map[string]any{"sourceType": strings.Repeat("s", 128), "expired": expired,
				"requestVersion": strings.Repeat("v", 128), "personalState": nil}
			m := friendNoticeFixture(t, kind, friendNoticeWrapper(t, friendRequestClientDetail(t, kind, extensions)))
			message, err := ReadMessageView(friendNoticeWire(t, m))
			event := "friend_approved"
			if kind == 1203 {
				event = "friend_request"
			}
			want := &MessageView{Kind: "friend_notice", ContentType: kind, FriendNotice: &FriendNotice{
				Event: event, Sender: MessagePerson{ID: m.SendID, Name: "sender-name"},
				Recipient: MessagePerson{ID: m.RecvID, Name: "recipient-name"}}}
			if err != nil || message == nil || message.Sender != m.SendID || message.Recipient != m.RecvID ||
				!reflect.DeepEqual(message.View, want) {
				t.Fatal("client extension changed the event or envelope display", kind, message, err)
			}
			output, err := json.Marshal(message.View)
			if err != nil || bytes.Contains(output, []byte("synthetic-hidden")) ||
				bytes.Contains(output, []byte("sourceType")) || bytes.Contains(output, []byte("expired")) ||
				bytes.Contains(output, []byte("requestVersion")) || bytes.Contains(output, []byte("personalState")) {
				t.Fatal("request extension or hidden notes leaked into display", kind, err)
			}
			if _, err := ReadOutboundSource(friendNoticeWire(t, m)); err == nil {
				t.Fatal("extended request was admitted as a human source", kind)
			}
		}
		message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, kind,
			friendNoticeWrapper(t, friendRequestClientDetail(t, kind,
				map[string]any{"sourceType": "", "requestVersion": "", "expired": false, "personalState": nil})))))
		if err != nil || message == nil || message.View.Kind != "friend_notice" {
			t.Fatal("empty scalar defaults were rejected", kind, message, err)
		}
		for _, row := range []struct {
			name, key string
			value     any
		}{
			{"source-null", "sourceType", nil},
			{"source-number", "sourceType", 1},
			{"source-object", "sourceType", map[string]any{}},
			{"source-long", "sourceType", strings.Repeat("s", 129)},
			{"source-control", "sourceType", "source\n"},
			{"source-tab", "sourceType", "source\t"},
			{"version-null", "requestVersion", nil},
			{"version-number", "requestVersion", 1},
			{"version-array", "requestVersion", []any{}},
			{"version-long", "requestVersion", strings.Repeat("v", 129)},
			{"version-control", "requestVersion", "version\x00"},
			{"version-newline", "requestVersion", "version\n"},
			{"expired-null", "expired", nil},
			{"expired-number", "expired", 0},
			{"expired-string", "expired", "false"},
			{"state-object", "personalState", map[string]any{}},
			{"state-array", "personalState", []any{}},
			{"state-string", "personalState", "synthetic-hidden-state"},
			{"state-bool", "personalState", false},
			{"unknown-extension", "unknownExtension", nil},
			{"foreign-request", "fromUserID", "foreign"},
		} {
			t.Run(row.name, func(t *testing.T) {
				extensions := map[string]any{"sourceType": "synthetic-hidden-source", "expired": false,
					"requestVersion": "synthetic-hidden-version", "personalState": nil}
				extensions[row.key] = row.value
				message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, kind,
					friendNoticeWrapper(t, friendRequestClientDetail(t, kind, extensions)))))
				if err != nil || message == nil ||
					!reflect.DeepEqual(message.View, &MessageView{Kind: "unsupported", ContentType: kind}) {
					t.Fatal("unverified request extension shape was displayed", kind, message, err)
				}
			})
		}
	}
	for _, field := range []string{"sourceType", "requestVersion", "expired", "personalState"} {
		request := `{"fromUserID":"sender-string","toUserID":"receiver-string","` + field + `":null,"` + field + `":null}`
		detail := `{"fromToUserID":` + friendNoticePair + `,"request":` + request + `}`
		message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 1201, friendNoticeWrapper(t, detail))))
		if err != nil || message == nil || message.View.Kind != "unsupported" {
			t.Fatal("duplicate request extension accepted", field, message, err)
		}
	}
	content := friendNoticeWrapper(t, friendRequestClientDetail(t, 1201,
		map[string]any{"requestVersion": "synthetic-version"}))
	content = bytes.Replace(content, []byte("synthetic-version"), []byte{'v', 0xff}, 1)
	message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 1201, content)))
	if err != nil || message == nil || message.View.Kind != "unsupported" {
		t.Fatal("invalid UTF-8 extension accepted", message, err)
	}
}

func TestFriendNoticeClientFriendGroupsOnlyAcceptsVerifiedEmptyShape(t *testing.T) {
	for _, self := range []bool{false, true} {
		for _, row := range []struct {
			name   string
			groups any
			valid  bool
		}{
			{"null", nil, true},
			{"empty", []any{}, true},
			{"unverified-group", []any{map[string]any{"groupID": "synthetic-hidden-group"}}, false},
			{"null-entry", []any{nil}, false},
			{"string", "synthetic-hidden-group", false},
			{"object", map[string]any{}, false},
			{"boolean", false, false},
			{"number", 0, false},
		} {
			t.Run(row.name, func(t *testing.T) {
				var detail map[string]any
				if err := json.Unmarshal([]byte(friendAddedDetail(t, "sender-string", "receiver-string", "partner-name")), &detail); err != nil {
					t.Fatal(err)
				}
				detail["friend"].(map[string]any)["friendGroups"] = row.groups
				body, err := json.Marshal(detail)
				if err != nil {
					t.Fatal(err)
				}
				m := friendNoticeFixture(t, 1204, friendNoticeWrapper(t, string(body)))
				if self {
					m.RecvID = m.SendID
				}
				message, err := ReadMessageView(friendNoticeWire(t, m))
				if err != nil || message == nil || message.Sender != m.SendID || message.Recipient != m.RecvID {
					t.Fatal("client friend groups changed the envelope", message, err)
				}
				want := &MessageView{Kind: "unsupported", ContentType: 1204}
				if row.valid {
					want.Kind = "friend_notice"
					want.FriendNotice = &FriendNotice{Event: "friend_added",
						Sender: MessagePerson{ID: m.SendID}, Recipient: MessagePerson{ID: m.RecvID, Name: "partner-name"}}
					if self {
						want.FriendNotice.Recipient.Name = ""
						want.FriendNotice.Friend = &MessagePerson{ID: "receiver-string", Name: "partner-name"}
					}
				}
				if !reflect.DeepEqual(message.View, want) {
					t.Fatal("unverified group shape or extra display data accepted", message.View)
				}
			})
		}
	}
	detail := `{"friend":{"ownerUserID":"sender-string","friendUser":{"userID":"receiver-string"},"friendGroups":null,"friendGroups":[]}}`
	message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 1204, friendNoticeWrapper(t, detail))))
	if err != nil || message == nil || message.View.Kind != "unsupported" {
		t.Fatal("duplicate friend group field accepted", message, err)
	}
}

func TestFriendNoticeRejectsDuplicateKeysAndInvalidUTF8(t *testing.T) {
	for _, row := range []struct {
		name, detail string
		kind         int32
	}{
		{"detail-pair", `{"fromToUserID":` + friendNoticePair + `,"fromToUserID":` + friendNoticePair + `}`, 1201},
		{"escaped-pair-key", `{"fromToUserID":` + friendNoticePair + `,"fromToUser\u0049D":` + friendNoticePair + `}`, 1201},
		{"sender", `{"fromToUserID":{"fromUserID":"sender-string","fromUserID":"sender-string","toUserID":"receiver-string"}}`, 1201},
		{"recipient", `{"fromToUserID":{"fromUserID":"sender-string","toUserID":"receiver-string","toUserID":"receiver-string"}}`, 1203},
		{"friend", `{"friend":{"ownerUserID":"sender-string","ownerUserID":"sender-string","friendUser":{"userID":"receiver-string"}}}`, 1204},
		{"user", `{"friend":{"ownerUserID":"sender-string","friendUser":{"userID":"receiver-string","userID":"receiver-string"}}}`, 1204},
		{"nickname", `{"friend":{"ownerUserID":"sender-string","friendUser":{"userID":"receiver-string","nickname":"one","nickname":"two"}}}`, 1204},
		{"operator", `{"friend":{"ownerUserID":"sender-string","friendUser":{"userID":"receiver-string"}},"opUser":{"userID":"sender-string","userID":"sender-string"}}`, 1204},
		{"escaped-invalid-id", `{"fromToUserID":{"fromUserID":"\ud800","toUserID":"receiver-string"}}`, 1203},
	} {
		t.Run(row.name, func(t *testing.T) {
			view := readFriendNotice(row.kind, friendNoticeWrapper(t, row.detail), "sender-string", "receiver-string")
			if !reflect.DeepEqual(view, &MessageView{Kind: "unsupported", ContentType: row.kind}) {
				t.Fatal("ambiguous or invalid identity/schema was displayed", view)
			}
		})
	}
	valid := friendNoticeWrapper(t, `{"fromToUserID":`+friendNoticePair+`}`)
	for _, content := range [][]byte{
		[]byte(`{"detail":"{}","detail":"{}"}`),
		bytes.Replace(valid, []byte("sender-string"), []byte{'s', 0xff}, 1),
		append(append([]byte{}, valid...), 0xff),
		[]byte("{\"detail\":\"\xff\"}"),
	} {
		view := readFriendNotice(1201, content, "sender-string", "receiver-string")
		if !reflect.DeepEqual(view, &MessageView{Kind: "unsupported", ContentType: 1201}) {
			t.Fatal("duplicate wrapper or invalid UTF-8 was displayed", view)
		}
	}
}

func TestFriendNoticeEnvelopeAndPayloadRouting(t *testing.T) {
	base := friendNoticeFixture(t, 1201, friendNoticeWrapper(t, `{"fromToUserID":`+friendNoticePair+`}`))
	for _, row := range []struct {
		name   string
		change func(*sdkws.MsgData)
	}{
		{"missing-client", func(m *sdkws.MsgData) { m.ClientMsgID = "" }},
		{"missing-sender", func(m *sdkws.MsgData) { m.SendID = "" }},
		{"missing-recipient", func(m *sdkws.MsgData) { m.RecvID = "" }},
		{"self-route", func(m *sdkws.MsgData) { m.RecvID = m.SendID }},
		{"group-on-private", func(m *sdkws.MsgData) { m.GroupID = "other-group" }},
		{"invalid-session", func(m *sdkws.MsgData) { m.SessionType = 4 }},
		{"long-sender", func(m *sdkws.MsgData) { m.SendID = strings.Repeat("s", 129) }},
		{"long-recipient", func(m *sdkws.MsgData) { m.RecvID = strings.Repeat("r", 129) }},
	} {
		t.Run(row.name, func(t *testing.T) {
			m := proto.Clone(base).(*sdkws.MsgData)
			row.change(m)
			if view, err := ReadMessageView(friendNoticeWire(t, m)); err == nil || view != nil {
				t.Fatal("invalid private envelope accepted", view, err)
			}
		})
	}
	for _, row := range []struct {
		name, detail string
		kind         int32
	}{
		{"foreign-sender", `{"fromToUserID":{"fromUserID":"foreign","toUserID":"receiver-string"}}`, 1201},
		{"foreign-recipient", `{"fromToUserID":{"fromUserID":"sender-string","toUserID":"foreign"}}`, 1203},
		{"self-pair", `{"fromToUserID":{"fromUserID":"sender-string","toUserID":"sender-string"}}`, 1201},
		{"reversed-application", `{"fromToUserID":{"fromUserID":"receiver-string","toUserID":"sender-string"}}`, 1203},
		{"foreign-friend-owner", friendAddedDetail(t, "foreign", "receiver-string", "fixture"), 1204},
		{"foreign-friend-user", friendAddedDetail(t, "sender-string", "foreign", "fixture"), 1204},
		{"self-friend", friendAddedDetail(t, "sender-string", "sender-string", "fixture"), 1204},
	} {
		t.Run(row.name, func(t *testing.T) {
			m := friendNoticeFixture(t, row.kind, friendNoticeWrapper(t, row.detail))
			view, err := ReadMessageView(friendNoticeWire(t, m))
			if err != nil || view == nil || view.View.Kind != "unsupported" || view.View.FriendNotice != nil {
				t.Fatal("payload identity escaped its private envelope", view, err)
			}
		})
	}
}

func TestFriendNoticeDisplayNeverChangesProofOrHumanRouting(t *testing.T) {
	for _, kind := range []int32{1201, 1203, 1204} {
		detail := `{"fromToUserID":` + friendNoticePair + `}`
		if kind == 1204 {
			detail = friendAddedDetail(t, "sender-string", "receiver-string", "fixture")
		}
		m := friendNoticeFixture(t, kind, friendNoticeWrapper(t, detail))
		raw := friendNoticeWire(t, m)
		if _, err := ReadOutboundSource(raw); err == nil {
			t.Fatal("friend display enabled an ordinary outbound source", kind)
		}
		route, err := ReadPrivateFriendNoticeRoute(raw)
		if kind == 1203 {
			if err == nil {
				t.Fatal("display expanded the route-only parser to friend applications")
			}
		} else if err != nil || route.Sender != m.SendID || route.Recipient != m.RecvID {
			t.Fatal("display changed the pinned metadata-only route", kind, route, err)
		}
		m.Content = []byte(`{"synthetic-hidden":"unrecognized-content"}`)
		if kind != 1203 {
			if _, err := ReadPrivateFriendNoticeRoute(friendNoticeWire(t, m)); err != nil {
				t.Fatal("display schema checks leaked into metadata-only routing", err)
			}
		}
		m.Content = friendNoticeWrapper(t, detail)
		m.MsgFrom = 100
		human, err := ReadMessageView(friendNoticeWire(t, m))
		if err != nil || human == nil || human.View.Kind != "unsupported" || human.View.FriendNotice != nil {
			t.Fatal("human message was elevated to a system notice", human, err)
		}
		m.MsgFrom = 200
		for _, session := range []int32{2, 3} {
			m.SessionType, m.GroupID = session, "group-string"
			group, err := ReadMessageView(friendNoticeWire(t, m))
			if err != nil || group == nil || group.View.Kind != "unsupported" || group.View.FriendNotice != nil {
				t.Fatal("private friend projection crossed into a group", group, err)
			}
		}
	}
	nested := `{"text":"reply","quoteMessage":{"contentType":1201,"content":` +
		string(friendNoticeWrapper(t, `{"fromToUserID":`+friendNoticePair+`}`)) + `}}`
	quote, err := ReadMessageView(messageFixture(t, 114, nested))
	if err != nil || quote == nil || quote.View.Quote.Kind != "unsupported" || quote.View.Quote.FriendNotice != nil {
		t.Fatal("quoted payload manufactured a friend notice", quote, err)
	}
}

func TestFriendNoticeBounds(t *testing.T) {
	for _, length := range []int{1000, 1001} {
		name := strings.Repeat("\u754c", 333) + strings.Repeat("a", length-999)
		content := friendNoticeWrapper(t, friendAddedDetail(t, "sender-string", "receiver-string", name))
		message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 1204, content)))
		if err != nil || message == nil || (message.View.Kind == "friend_notice") != (length == 1000) {
			t.Fatal("friend nickname byte bound changed", length, message, err)
		}
	}
	for _, name := range []string{"control\rname", "control\x00name"} {
		message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 1204,
			friendNoticeWrapper(t, friendAddedDetail(t, "sender-string", "receiver-string", name)))))
		if err != nil || message == nil || message.View.Kind != "unsupported" {
			t.Fatal("unsafe display name accepted", message, err)
		}
	}
	detail := `{"fromToUserID":` + friendNoticePair + `,"handleMsg":""}`
	empty := friendNoticeWrapper(t, detail)
	content := friendNoticeWrapper(t, strings.Replace(detail, `"handleMsg":""`,
		`"handleMsg":"`+strings.Repeat("a", 65536-len(empty))+`"`, 1))
	if len(content) != 65536 {
		t.Fatal("fixture must exercise the exact content bound", len(content))
	}
	message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 1201, content)))
	if err != nil || message == nil || message.View.Kind != "friend_notice" || message.View.Text != "" {
		t.Fatal("bounded notice metadata leaked or was not accepted", message, err)
	}
	for _, kind := range []int32{1201, 1299} {
		large := friendNoticeFixture(t, kind, append(content, ' '))
		if message, err := ReadMessageView(friendNoticeWire(t, large)); err == nil || message != nil {
			t.Fatal("private notification exceeded its content bound", kind)
		}
	}
	if message, err := ReadMessageView(make([]byte, 128<<10+1)); err == nil || message != nil {
		t.Fatal("oversized protobuf accepted")
	}
}

func TestSystemReceiptNoticePinnedSchemaIsInformationalOnly(t *testing.T) {
	for _, row := range []struct {
		name, detail string
		self         bool
	}{
		{"peer-receipt", `{"markAsReadUserID":"sender-string","conversationID":"si_receiver-string_sender-string","seqs":[1,2],"hasReadSeq":2}`, false},
		{"recipient-receipt", `{"markAsReadUserID":"receiver-string","conversationID":"si_receiver-string_sender-string","seqs":null,"hasReadSeq":0}`, false},
		{"self-sync", `{"markAsReadUserID":"sender-string","conversationID":"si_other_sender-string","seqs":[],"hasReadSeq":2}`, true},
		{"minimal", `{"markAsReadUserID":"sender-string","conversationID":"si_receiver-string_sender-string"}`, false},
	} {
		t.Run(row.name, func(t *testing.T) {
			m := friendNoticeFixture(t, 2200, friendNoticeWrapper(t, row.detail))
			if row.self {
				m.RecvID = m.SendID
			}
			raw := friendNoticeWire(t, m)
			message, err := ReadMessageView(raw)
			if err != nil || message == nil || message.Sender != m.SendID || message.Recipient != m.RecvID ||
				!reflect.DeepEqual(message.View, &MessageView{Kind: "system_notice", ContentType: 2200,
					SystemNotice: &SystemNotice{Event: "read_receipt"}}) {
				t.Fatal("pinned receipt lost informational display", message, err)
			}
			clear(raw)
			body, err := json.Marshal(message.View)
			if err != nil || bytes.Contains(body, []byte("conversationID")) || bytes.Contains(body, []byte("si_")) ||
				bytes.Contains(body, []byte("seq")) || bytes.Contains(body, []byte("synthetic-hidden")) {
				t.Fatal("receipt exposed upstream scope, sequence or status", string(body), err)
			}
			if _, err := ReadPrivateFriendNoticeRoute(friendNoticeWire(t, m)); err == nil {
				t.Fatal("receipt supplied a friend proof route")
			}
			if _, err := ReadOutboundSource(friendNoticeWire(t, m)); err == nil {
				t.Fatal("receipt supplied an ordinary outbound source")
			}
			m.MsgFrom = 100
			human, err := ReadMessageView(friendNoticeWire(t, m))
			if err != nil || human == nil || human.View.Kind != "unsupported" || human.View.SystemNotice != nil {
				t.Fatal("human content was promoted to a receipt", human, err)
			}
			m.MsgFrom, m.SessionType, m.GroupID = 200, 3, "group-string"
			group, err := ReadMessageView(friendNoticeWire(t, m))
			if err != nil || group == nil || group.View.Kind != "unsupported" || group.View.SystemNotice != nil {
				t.Fatal("receipt display crossed into group routing", group, err)
			}
		})
	}
}

func TestSystemReceiptNoticeRejectsUnknownDuplicateAndUnboundedTips(t *testing.T) {
	for _, row := range []struct {
		name, detail string
	}{
		{"missing-user", `{"conversationID":"si_sender-string_receiver-string"}`},
		{"missing-conversation", `{"markAsReadUserID":"sender-string"}`},
		{"foreign-user", `{"markAsReadUserID":"foreign","conversationID":"si_sender-string_receiver-string"}`},
		{"numeric-user", `{"markAsReadUserID":7,"conversationID":"si_sender-string_receiver-string"}`},
		{"null-conversation", `{"markAsReadUserID":"sender-string","conversationID":null}`},
		{"unknown-field", `{"markAsReadUserID":"sender-string","conversationID":"si_sender-string_receiver-string","read":true}`},
		{"duplicate-user", `{"markAsReadUserID":"sender-string","markAsReadUserID":"sender-string","conversationID":"si_sender-string_receiver-string"}`},
		{"duplicate-conversation", `{"markAsReadUserID":"sender-string","conversationID":"si_sender-string_receiver-string","conversationID":"si_other"}`},
		{"duplicate-seqs", `{"markAsReadUserID":"sender-string","conversationID":"si_sender-string_receiver-string","seqs":[1],"seqs":[2]}`},
		{"negative-cursor", `{"markAsReadUserID":"sender-string","conversationID":"si_sender-string_receiver-string","hasReadSeq":-1}`},
		{"cursor-overflow", `{"markAsReadUserID":"sender-string","conversationID":"si_sender-string_receiver-string","hasReadSeq":9223372036854775808}`},
		{"string-cursor", `{"markAsReadUserID":"sender-string","conversationID":"si_sender-string_receiver-string","hasReadSeq":"1"}`},
		{"negative-seq", `{"markAsReadUserID":"sender-string","conversationID":"si_sender-string_receiver-string","seqs":[-1]}`},
		{"zero-seq", `{"markAsReadUserID":"sender-string","conversationID":"si_sender-string_receiver-string","seqs":[0]}`},
		{"fraction-seq", `{"markAsReadUserID":"sender-string","conversationID":"si_sender-string_receiver-string","seqs":[1.5]}`},
		{"duplicate-seq", `{"markAsReadUserID":"sender-string","conversationID":"si_sender-string_receiver-string","seqs":[1,1]}`},
		{"object-seqs", `{"markAsReadUserID":"sender-string","conversationID":"si_sender-string_receiver-string","seqs":{}}`},
		{"long-conversation", `{"markAsReadUserID":"sender-string","conversationID":"` + strings.Repeat("c", 129) + `"}`},
		{"invalid-detail-utf8", "{\"markAsReadUserID\":\"sender-string\",\"conversationID\":\"\xff\"}"},
	} {
		t.Run(row.name, func(t *testing.T) {
			content := friendNoticeWrapper(t, row.detail)
			if row.name == "invalid-detail-utf8" {
				// Preserve invalid inner bytes instead of json.Marshal's UTF-8 repair.
				content = []byte(`{"detail":"{\"markAsReadUserID\":\"sender-string\",\"conversationID\":\"` + "\xff" + `\"}"}`)
			}
			message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 2200, content)))
			if err != nil || message == nil ||
				!reflect.DeepEqual(message.View, &MessageView{Kind: "unsupported", ContentType: 2200}) {
				t.Fatal("unrecognized receipt became a supported notice", message, err)
			}
		})
	}
	for _, content := range [][]byte{[]byte(`{"detail":"{}","detail":"{}"}`), []byte(`{"detail":{}}`),
		[]byte(`{"markAsReadUserID":"sender-string","conversationID":"si_receiver-string_sender-string"}`)} {
		if view := readSystemReceiptNotice(content, "sender-string", "receiver-string"); view.Kind != "unsupported" {
			t.Fatal("receipt accepted an unpinned wrapper", view)
		}
	}
	for _, count := range []int{1024, 1025} {
		seqs := make([]int64, count)
		for i := range seqs {
			seqs[i] = int64(i + 1)
		}
		detail, err := json.Marshal(map[string]any{"markAsReadUserID": "sender-string",
			"conversationID": "si_receiver-string_sender-string", "seqs": seqs, "hasReadSeq": count})
		if err != nil {
			t.Fatal(err)
		}
		message, err := ReadMessageView(friendNoticeWire(t, friendNoticeFixture(t, 2200, friendNoticeWrapper(t, string(detail)))))
		if err != nil || message == nil || (message.View.Kind == "system_notice") != (count == 1024) {
			t.Fatal("receipt sequence budget changed", count, message, err)
		}
	}
}

func FuzzReadFriendNotice(f *testing.F) {
	for _, kind := range []int32{1201, 1203, 1204, 1299, 2200} {
		f.Add(kind, friendNoticeWrapper(f, `{"fromToUserID":`+friendNoticePair+`}`))
	}
	f.Add(int32(1204), friendNoticeWrapper(f, friendAddedDetail(f, "sender-string", "receiver-string", "fixture")))
	f.Add(int32(1201), friendNoticeWrapper(f, friendApplicationDetail(f, 1201, friendRequestSnapshot())))
	f.Add(int32(1203), friendNoticeWrapper(f, friendApplicationDetail(f, 1203, friendRequestSnapshot())))
	f.Add(int32(1201), friendNoticeWrapper(f, friendRequestClientDetail(f, 1201,
		map[string]any{"sourceType": "synthetic-source", "expired": false, "requestVersion": "synthetic-version", "personalState": nil})))
	f.Add(int32(1201), []byte(`{"detail":"{}","detail":"{}"}`))
	f.Add(int32(1203), []byte{0xff})
	f.Add(int32(2200), friendNoticeWrapper(f,
		`{"markAsReadUserID":"sender-string","conversationID":"si_receiver-string_sender-string","seqs":[1],"hasReadSeq":1}`))
	f.Fuzz(func(t *testing.T, kind int32, content []byte) {
		raw := friendNoticeWire(t, friendNoticeFixture(t, kind, content))
		message, err := ReadMessageView(raw)
		clear(raw)
		if err != nil {
			return
		}
		if message == nil || message.View == nil || message.View.ContentType != kind ||
			(message.View.Kind != "friend_notice" && message.View.Kind != "system_notice" && message.View.Kind != "unsupported") ||
			message.View.Text != "" {
			t.Fatal("system notice lost its type or became human text", message)
		}
		output, err := json.Marshal(message.View)
		if err != nil || !utf8.Valid(output) || len(output) > 16<<10 || len(message.View.Attachments) != 0 {
			t.Fatal("unbounded or resource-bearing friend display", len(output), err)
		}
	})
}
