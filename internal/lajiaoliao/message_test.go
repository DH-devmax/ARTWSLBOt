package lajiaoliao

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

func messageFixture(t *testing.T, kind int32, content string) []byte {
	t.Helper()
	raw, err := proto.Marshal(&sdkws.MsgData{ClientMsgID: "message-string", SendID: "sender-string",
		RecvID: "group-string", GroupID: "group-string", SessionType: 3, MsgFrom: 100,
		ContentType: kind, Content: []byte(content)})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestReadMessageViewIndependentStringIDsNestedAndOwnedStrings(t *testing.T) {
	content := `{"title":"group history","abstractList":["one","two"],"multiMessage":[` +
		`{"contentType":114,"senderNickname":"Alice","quoteElem":{"text":"reply","quoteMessage":` +
		`{"contentType":102,"pictureElem":{"sourcePicture":{"url":"https://private.invalid/path?token=hidden"}}}}},` +
		`{"contentType":106,"atTextElem":{"text":"hello @native-id ","atUserList":["native-id"],` +
		`"atUsersInfo":[{"atUserID":"native-id","groupNickname":"Bob"}],"isAtSelf":false}}]}`
	raw := messageFixture(t, 107, content)
	result, err := ReadMessageView(raw)
	clear(raw)
	if err != nil || result.ID != "message-string" || result.Group != "group-string" ||
		result.View.Kind != "forward" || len(result.View.Items) != 2 ||
		result.View.Items[0].Quote.Kind != "image" || result.View.Items[0].Text != "reply" ||
		result.View.Items[1].Mentions[0].ID != "native-id" || result.View.Items[1].Mentions[0].Name != "Bob" {
		t.Fatal("native string IDs, nested order or owned strings lost", result, err)
	}
	body, _ := json.Marshal(result.View)
	if strings.Contains(string(body), "private.invalid") || strings.Contains(string(body), "hidden") {
		t.Fatal("resource URL or opaque credentials leaked into display")
	}
}

func TestReadMessageViewRejectsMalformedAndRetainsUnknown(t *testing.T) {
	for _, content := range []string{
		`{"text":"a","text":"b","quoteMessage":{"contentType":101,"textElem":{"content":"quoted"}}}`,
		`{"text":"a","extra":{"secret":"opaque"},"quoteMessage":{"contentType":101,"textElem":{"content":"quoted"}}}`,
		`{"text":"a","quoteMessage":null}`,
		`{"text":"a","quoteMessage":{"contentType":101,"textElem":{"content":false}}}`,
		"{\"text\":\"\xff\",\"quoteMessage\":{}}",
	} {
		if view, err := ReadMessageView(messageFixture(t, 114, content)); err == nil || view != nil {
			t.Fatal("malformed quote downgraded to text", content, view, err)
		}
	}
	view, err := ReadMessageView(messageFixture(t, 107,
		`{"title":"unknown","abstractList":[],"multiMessage":[{"contentType":999,"content":{"unknown":true}}]}`))
	if err != nil || len(view.View.Items) != 1 || view.View.Items[0].Kind != "unsupported" ||
		view.View.Items[0].ContentType != 999 {
		t.Fatal("unknown nested item discarded", view, err)
	}
}

func TestReadMessageViewBoundsForwardAndMentions(t *testing.T) {
	rows := []map[string]any{}
	for range 40 {
		rows = append(rows, map[string]any{"contentType": 101, "textElem": map[string]any{"content": "bounded"}})
	}
	content, _ := json.Marshal(map[string]any{"title": "bounded", "abstractList": []string{}, "multiMessage": rows})
	view, err := ReadMessageView(messageFixture(t, 107, string(content)))
	if err != nil || !view.View.Truncated || len(view.View.Items) != 31 {
		t.Fatal("forward item budget escaped", view, err)
	}
	bad := []byte{0x12, 0xff}
	if _, err := ReadMessageView(bad); err == nil {
		t.Fatal("truncated protobuf accepted")
	}
}

func FuzzReadMessageView(f *testing.F) {
	for _, input := range []string{`{"content":"hello"}`, `{"text":"hi","atUserList":["string-id"],"atUsersInfo":[]}`,
		`{"text":"quote","quoteMessage":{"contentType":101,"textElem":{"content":"q"}}}`} {
		f.Add(int32(114), []byte(input))
	}
	f.Fuzz(func(t *testing.T, kind int32, content []byte) {
		raw, _ := proto.Marshal(&sdkws.MsgData{ClientMsgID: "id", SendID: "sender", RecvID: "group",
			GroupID: "group", SessionType: 3, MsgFrom: 100, ContentType: kind, Content: content})
		view, err := ReadMessageView(raw)
		clear(raw)
		if err == nil && view != nil {
			output, e := json.Marshal(view.View)
			if e != nil || len(output) > 128<<10 {
				t.Fatal("unbounded display", len(output), e)
			}
		}
	})
}
