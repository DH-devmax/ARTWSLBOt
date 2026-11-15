package gateway

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
	"wangshangliao/internal/security"
)

func TestAssistantRecallCandidateChecksRetainedScopeAndOwnsPreview(t *testing.T) {
	vault, _ := security.NewVault(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	manager := &ReceiverManager{Vault: vault}
	at := time.Now().UnixMilli()
	message := &sdkws.MsgData{ClientMsgID: "string-client", ServerMsgID: "string-server", GroupID: "string-group",
		SendID: "string-sender", RecvID: "string-group", SessionType: 3, Seq: 12, SendTime: at}
	raw, _ := proto.Marshal(message)
	projection := DecodedMessage{Platform: "lajiaoliao", NativeTarget: "string-group", NativeSender: "string-sender",
		ServerID: "string-server"}
	projection.Message.Session, projection.Message.CreatedAt, projection.Message.ClientID = 2, uint64(at), "string-client"
	projection.Message.Text = "synthetic bounded preview"
	p, _ := json.Marshal(projection)
	sealed, _ := vault.Seal(DecodedAAD("account", 1), p)
	source, _ := vault.Seal("native-event:account:key", raw)
	candidate, err := manager.readMessageCandidate("lajiaoliao", "account", "string-group", "key", 1, sealed, source)
	if err != nil || candidate.Reference.Sequence != 12 || candidate.Reference.Conversation != "sg_string-group" {
		t.Fatal(candidate, err)
	}
	clear(raw)
	clear(p)
	if candidate.Preview != "synthetic bounded preview" {
		t.Fatal("preview aliases cleared source")
	}
	if _, err = manager.readMessageCandidate("lajiaoliao", "account", "other-group", "key", 1, sealed, source); err == nil {
		t.Fatal("cross-group candidate accepted")
	}
	if _, err = manager.readMessageCandidate("wsl", "account", "123", "key", 1, sealed, source); err == nil {
		t.Fatal("cross-platform candidate accepted")
	}
	source[len(source)-1] ^= 1
	if _, err = manager.readMessageCandidate("lajiaoliao", "account", "string-group", "key", 1, sealed, source); err == nil {
		t.Fatal("tampered retained input accepted")
	}
}
