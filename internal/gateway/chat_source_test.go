package gateway

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/nativewire"
	"wangshangliao/internal/security"
)

func TestChatSourceWSLNativeTargetsAndOwnedProjection(t *testing.T) {
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{31}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	manager := &ReceiverManager{Vault: vault}
	message := nativewire.ApplicationMessage{Session: 2, Format: 0, ClientID: "native-client",
		Sender: nativewire.MessageSource{ID: 123}, Target: nativewire.MessageSource{ID: 456},
		Text: "hello @self", Mentions: []nativewire.MessageMention{{UID: 789}},
		Quote: &nativewire.MessageQuote{ClientID: "prior-native-client"}}
	read := func(message nativewire.ApplicationMessage) (ChatSource, error) {
		raw, _ := json.Marshal(DecodedMessage{Message: message})
		sealed, e := vault.Seal(DecodedAAD("synthetic", 1), raw)
		clear(raw)
		if e != nil {
			t.Fatal(e)
		}
		out, e := manager.ReadChatSource("wsl", "synthetic", "key", 1, sealed, nil)
		clear(sealed)
		return out, e
	}
	out, err := read(message)
	if err != nil || out.Group != "456" || out.Sender != "123" || out.Text != message.Text ||
		out.ReplyClientID != "prior-native-client" || len(out.Mentions) != 1 || out.Mentions[0] != "789" {
		t.Fatal(out, err)
	}
	message.Mentions, message.Quote = nil, nil
	out, err = read(message)
	if err != nil || len(out.Mentions) != 0 || out.ReplyClientID != "" {
		t.Fatal("plain @ text manufactured native targeting", out, err)
	}
	message.Session = 1
	if _, err = read(message); err == nil {
		t.Fatal("private message entered group chat")
	}
	message.Session, message.Image = 2, &nativewire.MessageImage{}
	if _, err = read(message); err == nil {
		t.Fatal("media entered basic chat")
	}
}

func TestChatSourceChiliNativeStringMentionAndQuote(t *testing.T) {
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{32}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	manager := &ReceiverManager{Vault: vault}
	read := func(kind, session int32, content string) (ChatSource, error) {
		raw, e := proto.Marshal(&sdkws.MsgData{ClientMsgID: "source-string", SendID: "sender-string",
			GroupID: "group-string", RecvID: "group-string", SessionType: session,
			MsgFrom: 100, ContentType: kind, Content: []byte(content)})
		if e != nil {
			t.Fatal(e)
		}
		sealed, e := vault.Seal("native-event:synthetic:key", raw)
		clear(raw)
		if e != nil {
			t.Fatal(e)
		}
		out, e := manager.ReadChatSource("lajiaoliao", "synthetic", "key", 1, nil, sealed)
		clear(sealed)
		return out, e
	}
	out, err := read(106, 3, `{"text":"hi @self-string","atUserList":["self-string"],"atUsersInfo":[],"isAtSelf":true}`)
	if err != nil || out.Group != "group-string" || out.Sender != "sender-string" ||
		len(out.Mentions) != 1 || out.Mentions[0] != "self-string" {
		t.Fatal(out, err)
	}
	out, err = read(114, 3, `{"text":"reply","quoteMessage":{"clientMsgID":"prior-string","contentType":101,"textElem":{"content":"quoted"}}}`)
	if err != nil || out.ReplyClientID != "prior-string" || out.Text != "reply" {
		t.Fatal(out, err)
	}
	out, err = read(101, 3, `{"content":"plain @self-string"}`)
	if err != nil || len(out.Mentions) != 0 || out.ReplyClientID != "" {
		t.Fatal("plain @ text manufactured native targeting", out, err)
	}
	if _, err = read(101, 1, `{"content":"private"}`); err == nil {
		t.Fatal("private message entered group chat")
	}
	if _, err = read(999, 3, `{"content":"unknown"}`); err == nil {
		t.Fatal("unknown format entered basic chat")
	}
}
