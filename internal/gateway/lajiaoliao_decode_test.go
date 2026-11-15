package gateway

import (
	"testing"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/proto"
)

func TestChiliDecodeScopedTextAndComponents(t *testing.T) {
	base := &sdkws.MsgData{SendID: "native-user", RecvID: "self", GroupID: "same-id", ClientMsgID: "message",
		SessionType: 3, MsgFrom: 100, ContentType: 101, Content: []byte(`{"content":"hello"}`)}
	for _, test := range []struct {
		name          string
		edit          func(*sdkws.MsgData)
		state, target string
		outgoing      bool
	}{
		{"group", func(*sdkws.MsgData) {}, "decoded", "same-id", false},
		{"private", func(m *sdkws.MsgData) { m.SessionType = 1 }, "decoded", "native-user", false},
		{"outgoing-private", func(m *sdkws.MsgData) { m.SessionType = 1; m.SendID = "self"; m.RecvID = "peer" }, "decoded", "peer", true},
		{"foreign-private", func(m *sdkws.MsgData) { m.SessionType = 1; m.RecvID = "foreign" }, "failed", "", false},
		{"missing-group", func(m *sdkws.MsgData) { m.GroupID = "" }, "failed", "", false},
		{"quote", func(m *sdkws.MsgData) { m.ContentType = 114 }, "unsupported", "same-id", false},
		{"system-notification", func(m *sdkws.MsgData) { m.MsgFrom = 200 }, "unsupported", "same-id", false},
		{"text-with-unhandled-component", func(m *sdkws.MsgData) { m.Content = []byte(`{"content":"hello","quote":{"id":"secret"}}`) }, "failed", "same-id", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			m := proto.Clone(base).(*sdkws.MsgData)
			test.edit(m)
			raw, _ := proto.Marshal(m)
			out, scene, key, state, _ := decodeChiliMessage(raw, "self")
			if state != test.state || out.NativeTarget != test.target || out.Outgoing != test.outgoing {
				t.Fatalf("projection %+v %s", out, state)
			}
			if state == "decoded" {
				if out.Platform != "lajiaoliao" || out.NativeSender != m.SendID || out.Message.Text != "hello" {
					t.Fatal("identity/text lost")
				}
				if key == conversationKey(scene, 0, out.NativeTarget) {
					t.Fatal("cross-platform conversation collision")
				}
			}
			if state != "decoded" && out.Message.Text != "" {
				t.Fatal("unsupported component silently downgraded")
			}
		})
	}
}

func TestChiliSequencePlaceholderCannotBecomeIdentityEvidence(t *testing.T) {
	for _, seq := range []int64{0, 42} {
		raw, _ := proto.Marshal(&sdkws.MsgData{Seq: seq, SendID: "peer", RecvID: "self",
			SessionType: 1, MsgFrom: 100, ContentType: 101, Content: []byte(`{"content":"synthetic proof"}`)})
		out, scene, key, state, reason := decodeChiliMessage(raw, "self")
		want := "failed"
		if seq > 0 {
			want = "unsupported"
			if reason != "lajiaoliao_sequence_placeholder" {
				t.Fatal("placeholder diagnostic lost", reason)
			}
		}
		if state != want || scene != "" || key != "" || out.Message.Text != "" || out.NativeSender != "" {
			t.Fatal("placeholder acquired a chat route or identity", state, out)
		}
	}
	raw := []byte{0x80}
	if _, _, _, state, _ := decodeChiliMessage(raw, "self"); state != "failed" {
		t.Fatal("truncated protobuf became a placeholder")
	}
}
