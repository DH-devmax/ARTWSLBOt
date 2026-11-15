package nativewire

import (
	"testing"
	"time"
)

func TestPrivateTargetAndMedia(t *testing.T) {
	valid := ApplicationMessage{Session: 1, Sender: MessageSource{ID: 7}, Target: MessageSource{ID: 8}}
	for _, mode := range []string{"outgoing", "incoming", "group", "self", "wrong_self", "forged_transport", "empty_peer"} {
		t.Run(mode, func(t *testing.T) {
			message, from, to := valid, "self", "peer"
			switch mode {
			case "incoming":
				message.Sender.ID, message.Target.ID, from, to = 8, 7, "peer", "self"
			case "group":
				message.Session = 2
			case "self":
				message.Target.ID = 7
			case "wrong_self":
				message.Sender.ID = 9
			case "forged_transport":
				from = "not-self"
			case "empty_peer":
				to = ""
			}
			peer, err := PrivateTargetFor(7, "self", message, from, to)
			if mode == "outgoing" || mode == "incoming" {
				if err != nil || peer.ID != 8 || peer.Transport != "peer" || len(PrivateConversationKey(peer)) != 64 {
					t.Fatal(peer, err)
				}
			} else if err == nil {
				t.Fatal("unverified private destination", mode)
			}
		})
	}
	message := PrivateText{SenderID: 7, PeerID: 8, ClientID: "a391ad75-cbbc-4ce1-a333-b4b38be7e415", CreatedAt: time.Now().UTC(),
		Media: &OutboundMedia{Kind: "image", Asset: "9007199254740993", Size: 100, Width: 8, Height: 8}}
	if _, err := EncodePrivateText(message); err != nil {
		t.Fatal("private image rejected", err)
	}
	for _, kind := range []string{"voice", "video", "file"} {
		message.Media.Kind = kind
		if _, err := EncodePrivateText(message); err == nil {
			t.Fatal("unsupported private media silently encoded", kind)
		}
	}
}
