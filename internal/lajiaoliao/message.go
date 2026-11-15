package lajiaoliao

import (
	"bytes"
	"encoding/json"
	"io"
	"unicode/utf8"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

// MessageView is a read-only display projection. Native IDs remain strings;
// resource URLs, local file paths and opaque SDK extensions never leave it.
type MessageView struct {
	Kind         string              `json:"kind"`
	ContentType  int32               `json:"content_type"`
	Text         string              `json:"text,omitempty"`
	Title        string              `json:"title,omitempty"`
	Summaries    []string            `json:"summaries,omitempty"`
	Mentions     []MessagePerson     `json:"mentions,omitempty"`
	Quote        *MessageView        `json:"quote,omitempty"`
	Items        []*MessageView      `json:"items,omitempty"`
	Contact      *MessagePerson      `json:"contact,omitempty"`
	Media        map[string]string   `json:"media,omitempty"`
	Duration     int64               `json:"duration,omitempty"`
	Size         int64               `json:"size,omitempty"`
	Truncated    bool                `json:"truncated,omitempty"`
	Attachments  map[string]Resource `json:"-"`
	Notice       *GroupNotice        `json:"notice,omitempty"`
	FriendNotice *FriendNotice       `json:"friend_notice,omitempty"`
	SystemNotice *SystemNotice       `json:"system_notice,omitempty"`
}

type MessagePerson struct {
	ID   string `json:"id"`
	Name string `json:"name,omitempty"`
}

type ReadMessage struct {
	ID, Sender, Recipient, Group string
	SessionType                  int32
	View                         *MessageView
	Avatar                       Resource `json:"-"`
}

func ReadMessageView(raw []byte) (*ReadMessage, error) {
	var message sdkws.MsgData
	if len(raw) > 128<<10 || proto.Unmarshal(raw, &message) != nil {
		return nil, &Failure{Code: "lajiaoliao_message_shape"}
	}
	defer clear(message.Content)
	if message.MsgFrom != 100 && message.MsgFrom != 200 {
		return nil, nil
	}
	if !ValidID(message.ClientMsgID) || !ValidID(message.SendID) ||
		message.SessionType != 1 && message.SessionType != 2 && message.SessionType != 3 ||
		message.SessionType == 1 && !ValidID(message.RecvID) ||
		message.SessionType != 1 && !ValidID(message.GroupID) {
		return nil, &Failure{Code: "lajiaoliao_message_shape"}
	}
	remaining := 32
	var view *MessageView
	var err error
	if message.MsgFrom == 200 {
		if message.SessionType == 1 {
			if message.GroupID != "" || len(message.Content) > 65536 ||
				message.SendID == message.RecvID && message.ContentType != 1204 && message.ContentType != 2200 {
				return nil, &Failure{Code: "lajiaoliao_message_shape"}
			}
			if message.ContentType == 2200 {
				view = readSystemReceiptNotice(message.Content, message.SendID, message.RecvID)
			} else {
				view = readFriendNotice(message.ContentType, message.Content, message.SendID, message.RecvID)
			}
		} else {
			view, err = readGroupNotice(message.ContentType, message.Content, message.GroupID)
		}
	} else {
		view, err = readMessageContent(message.ContentType, message.Content, 0, &remaining)
	}
	if err != nil {
		return nil, err
	}
	return &ReadMessage{ID: message.ClientMsgID, Sender: message.SendID, Recipient: message.RecvID,
		Group: message.GroupID, SessionType: message.SessionType, View: view,
		Avatar: Resource{URL: message.SenderFaceURL, Kind: "avatar"}}, nil
}

func messageObject(raw []byte) (map[string]json.RawMessage, bool) {
	if len(raw) == 0 || len(raw) > 65536 || !utf8.Valid(raw) {
		return nil, false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	if start, err := d.Token(); err != nil || start != json.Delim('{') {
		return nil, false
	}
	out := map[string]json.RawMessage{}
	for d.More() {
		key, err := d.Token()
		name, ok := key.(string)
		if err != nil || !ok || out[name] != nil || len(out) >= 64 {
			return nil, false
		}
		var value json.RawMessage
		if d.Decode(&value) != nil {
			return nil, false
		}
		out[name] = value
	}
	if end, err := d.Token(); err != nil || end != json.Delim('}') || d.Decode(new(any)) != io.EOF {
		return nil, false
	}
	return out, true
}

func messageFields(object map[string]json.RawMessage, names ...string) bool {
	for key := range object {
		found := false
		for _, name := range names {
			found = found || key == name
		}
		if !found {
			return false
		}
	}
	return true
}

func messageText(raw json.RawMessage, limit int) (string, bool) {
	var text string
	if json.Unmarshal(raw, &text) != nil || !validText(text, 65536, true) {
		return "", false
	}
	runes := []rune(text)
	if len(runes) > limit {
		return string(runes[:limit]), true
	}
	return text, false
}

func readMessageContent(kind int32, raw []byte, depth int, remaining *int) (*MessageView, error) {
	out := &MessageView{Kind: "unsupported", ContentType: kind}
	if depth > 3 || *remaining <= 0 {
		out.Truncated = true
		return out, nil
	}
	*remaining = *remaining - 1
	object, ok := messageObject(raw)
	if !ok {
		return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
	}
	text := func(key string) bool {
		if value := object[key]; value != nil {
			var decoded string
			if json.Unmarshal(value, &decoded) != nil || !validText(decoded, 65536, true) {
				return false
			}
			out.Text, out.Truncated = messageText(value, 8192)
			return true
		}
		return false
	}
	switch kind {
	case 101:
		if !messageFields(object, "content") || !text("content") {
			return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
		}
		out.Kind = "text"
	case 106:
		if !messageFields(object, "text", "atUserList", "atUsersInfo", "isAtSelf") || !text("text") {
			return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
		}
		var ids []string
		var people []struct {
			ID   string `json:"atUserID"`
			Name string `json:"groupNickname"`
		}
		if json.Unmarshal(object["atUserList"], &ids) != nil || ids == nil || len(ids) > 1024 ||
			object["atUsersInfo"] != nil && json.Unmarshal(object["atUsersInfo"], &people) != nil || len(people) > 1024 {
			return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
		}
		if value := object["isAtSelf"]; value != nil {
			var self bool
			if json.Unmarshal(value, &self) != nil {
				return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
			}
		}
		names := map[string]string{}
		for _, person := range people {
			if !ValidID(person.ID) || !validText(person.Name, 1000, true) {
				return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
			}
			names[person.ID] = person.Name
		}
		for _, id := range ids {
			if !ValidID(id) {
				return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
			}
			if len(out.Mentions) < 32 {
				out.Mentions = append(out.Mentions, MessagePerson{ID: id, Name: names[id]})
			} else {
				out.Truncated = true
			}
		}
		out.Kind = "mention"
	case 114:
		if !messageFields(object, "text", "quoteMessage") || !text("text") || object["quoteMessage"] == nil {
			return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
		}
		var err error
		out.Quote, err = readNestedMessage(object["quoteMessage"], depth+1, remaining)
		if err != nil {
			return nil, err
		}
		out.Kind = "quote"
	case 107:
		if !messageFields(object, "title", "abstractList", "multiMessage") {
			return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
		}
		var title string
		var summaries []string
		var messages []json.RawMessage
		if json.Unmarshal(object["title"], &title) != nil || !validText(title, 1000, true) ||
			json.Unmarshal(object["abstractList"], &summaries) != nil || len(summaries) > 100 ||
			json.Unmarshal(object["multiMessage"], &messages) != nil || messages == nil || len(messages) > 100 {
			return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
		}
		out.Title = title
		for _, summary := range summaries {
			if !validText(summary, 8192, true) {
				return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
			}
			if len(out.Summaries) < 8 {
				out.Summaries = append(out.Summaries, summary)
			} else {
				out.Truncated = true
			}
		}
		for _, message := range messages {
			if *remaining <= 0 {
				out.Truncated = true
				break
			}
			view, err := readNestedMessage(message, depth+1, remaining)
			if err != nil {
				return nil, err
			}
			out.Items = append(out.Items, view)
		}
		out.Kind = "forward"
	case 108:
		var person struct {
			ID     string `json:"userID"`
			Name   string `json:"nickname"`
			Avatar string `json:"faceURL"`
		}
		if !messageFields(object, "userID", "nickname", "faceURL", "ex") ||
			json.Unmarshal(raw, &person) != nil || !ValidID(person.ID) || !validText(person.Name, 1000, true) {
			return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
		}
		out.Kind, out.Contact = "contact", &MessagePerson{ID: person.ID, Name: person.Name}
		out.Attachments = map[string]Resource{"avatar": {URL: person.Avatar, Kind: "avatar"}}
	case 102, 103, 104, 105:
		out.Kind = map[int32]string{102: "image", 103: "voice", 104: "video", 105: "file"}[kind]
		readResources(out, object, raw)
	default:
		return out, nil
	}
	return out, nil
}

func readNestedMessage(raw []byte, depth int, remaining *int) (*MessageView, error) {
	object, ok := messageObject(raw)
	var kind int32
	if !ok || json.Unmarshal(object["contentType"], &kind) != nil || kind <= 0 {
		return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
	}
	content := object["content"]
	if len(content) > 0 && content[0] == '"' {
		var encoded string
		if json.Unmarshal(content, &encoded) != nil {
			return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
		}
		content = []byte(encoded)
	}
	if len(content) == 0 || string(content) == `""` || string(content) == "null" {
		element := map[int32]string{101: "textElem", 106: "atTextElem", 114: "quoteElem", 107: "mergeElem",
			102: "pictureElem", 103: "soundElem", 104: "videoElem", 105: "fileElem", 108: "cardElem"}[kind]
		content = object[element]
	}
	if len(content) == 0 {
		out := &MessageView{Kind: "unsupported", ContentType: kind}
		if *remaining > 0 && depth <= 3 {
			*remaining = *remaining - 1
		} else {
			out.Truncated = true
		}
		return out, nil
	}
	view, err := readMessageContent(kind, content, depth, remaining)
	if err != nil {
		return nil, err
	}
	if sender := object["senderNickname"]; sender != nil {
		var name string
		if json.Unmarshal(sender, &name) != nil || !validText(name, 1000, true) {
			return nil, &Failure{Code: "lajiaoliao_message_content_shape"}
		}
		if view.Title == "" {
			view.Title = name
		}
	}
	return view, nil
}
