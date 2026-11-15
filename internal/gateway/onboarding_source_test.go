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

func onboardingManager(t *testing.T) *ReceiverManager {
	t.Helper()
	vault, err := security.NewVault(base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{37}, 32)))
	if err != nil {
		t.Fatal(err)
	}
	return &ReceiverManager{Vault: vault}
}

func TestOnboardingSourceWSLAuthenticatedRecipient(t *testing.T) {
	m := onboardingManager(t)
	read := func(d DecodedMessage, account string, event int64) (OnboardingSource, error) {
		raw, _ := json.Marshal(d)
		sealed, err := m.Vault.Seal(DecodedAAD("account", 7), raw)
		clear(raw)
		if err != nil {
			t.Fatal(err)
		}
		return m.ReadOnboardingSource("wsl", account, "123", "key", event, sealed, nil)
	}
	d := DecodedMessage{Message: nativewire.ApplicationMessage{Session: 1, Format: 0,
		Sender: nativewire.MessageSource{ID: 456}, Target: nativewire.MessageSource{ID: 123},
		ClientID: "challenge-message", Text: "proof-synthetic"}}
	out, err := read(d, "account", 7)
	if err != nil || !out.Private || out.Sender != "456" || out.Group != "" || out.Recipient != "123" {
		t.Fatal(out, err)
	}
	for _, tc := range []struct {
		account string
		event   int64
	}{{"other-account", 7}, {"account", 8}} {
		if _, err := read(d, tc.account, tc.event); err == nil {
			t.Fatal("cross-scope ciphertext admitted")
		}
	}
	d.Message.Target.ID = 789
	if _, err := read(d, "account", 7); err == nil {
		t.Fatal("private message to another identity admitted")
	}
	d.Message.Session = 2
	out, err = read(d, "account", 7)
	if err != nil || out.Private || out.Group != "789" || out.Recipient != out.Group {
		t.Fatal(out, err)
	}
	d.Outgoing = true
	if _, err := read(d, "account", 7); err == nil {
		t.Fatal("outgoing message proved a customer")
	}
	d.Outgoing = false
	d.Message.Quote = &nativewire.MessageQuote{}
	if _, err := read(d, "account", 7); err == nil {
		t.Fatal("quoted challenge admitted")
	}
}

func TestOnboardingSourceChiliStringIdentityAndPlainText(t *testing.T) {
	m := onboardingManager(t)
	read := func(wire *sdkws.MsgData) (OnboardingSource, error) {
		raw, err := proto.Marshal(wire)
		if err != nil {
			t.Fatal(err)
		}
		sealed, err := m.Vault.Seal("native-event:account:key", raw)
		clear(raw)
		if err != nil {
			t.Fatal(err)
		}
		return m.ReadOnboardingSource("lajiaoliao", "account", "self-string", "key", 1, nil, sealed)
	}
	wire := &sdkws.MsgData{ClientMsgID: "proof-message", SendID: "customer-string", RecvID: "self-string",
		SessionType: 1, MsgFrom: 100, ContentType: 101, Content: []byte(`{"content":"proof-synthetic"}`)}
	out, err := read(wire)
	if err != nil || !out.Private || out.Sender != "customer-string" || out.Recipient != "self-string" {
		t.Fatal(out, err)
	}
	wire.RecvID = "another-string"
	if _, err := read(wire); err == nil {
		t.Fatal("wrong private recipient admitted")
	}
	wire.SessionType, wire.GroupID, wire.RecvID = 3, "group-string", "group-string"
	out, err = read(wire)
	if err != nil || out.Private || out.Group != "group-string" || out.Recipient != out.Group {
		t.Fatal(out, err)
	}
	wire.SendID = "self-string"
	if _, err := read(wire); err == nil {
		t.Fatal("self message admitted")
	}
	wire.SendID, wire.ContentType = "customer-string", 114
	wire.Content = []byte(`{"text":"proof-synthetic","quoteMessage":{}}`)
	if _, err := read(wire); err == nil {
		t.Fatal("quote admitted as a proof")
	}
}
