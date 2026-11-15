package httpapi

import (
	"testing"

	"wangshangliao/internal/control"
	"wangshangliao/internal/features"
	"wangshangliao/internal/gateway"
)

func TestBusinessReplyObservationRequiresOriginalIdentityAndText(t *testing.T) {
	in := control.BusinessAttempt{Action: features.BusinessReply, ClientID: "original", Text: "reply",
		Scope: control.BusinessScope{NativeGroup: "group", NativeSelf: "robot"}}
	echo := gateway.ChatSource{Group: "group", Sender: "robot", ClientID: "original", Text: "reply"}
	if !businessReplyMatches(in, echo) {
		t.Fatal("original echo rejected")
	}
	for _, edit := range []func(*gateway.ChatSource){
		func(e *gateway.ChatSource) { e.Group = "other" },
		func(e *gateway.ChatSource) { e.Sender = "other" },
		func(e *gateway.ChatSource) { e.ClientID = "other" },
		func(e *gateway.ChatSource) { e.Text = "other" },
	} {
		wrong := echo
		edit(&wrong)
		if businessReplyMatches(in, wrong) {
			t.Fatal("substituted echo confirmed")
		}
	}
	in.Action = features.BusinessMute
	if businessReplyMatches(in, echo) {
		t.Fatal("text echo confirmed moderation")
	}
}
