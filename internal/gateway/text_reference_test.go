package gateway

import (
	"bytes"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func TestTextReplyRequiresAuthenticatedSameGroupReference(t *testing.T) {
	if !nativewire.Available() {
		t.Skip("synthetic references require sodium")
	}
	key := bytes.Repeat([]byte{17}, 32)
	for _, mode := range []string{"valid", "group", "team", "sender", "member_mapping", "missing_member",
		"partial_roster", "client", "server", "text", "timestamp", "ciphertext", "private", "missing_quote"} {
		t.Run(mode, func(t *testing.T) {
			original := nativewire.GroupText{SenderID: 8, GroupID: 123, ClientID: outboundClientID,
				Text: "authenticated quoted text", CreatedAt: time.Now().UTC()}
			attachment, err := nativewire.SealGroupText(key, original, 100, 200)
			if err != nil {
				t.Fatal(err)
			}
			raw := nativewire.IMMessage{Scene: 1, Target: "789", Sender: "8", ServerID: "9001",
				Fields: map[uint32]string{0: "1", 1: "789", 2: "8", 7: "1700000000123", 8: "100",
					10: attachment, 11: outboundClientID, 12: "9001"}}
			message := nativewire.GroupText{SenderID: 7, GroupID: 123,
				ClientID: "a391ad75-cbbc-4ce1-a333-b4b38be7e415", Text: "reply", CreatedAt: time.Now().UTC(),
				Quote: &nativewire.MessageQuote{ClientID: outboundClientID, ServerID: "9001", Text: original.Text}}
			roster := nativeauth.MemberDirectory{Group: "123", Complete: true,
				Members: []nativeauth.GroupMember{{ID: "8", Transport: "8", Role: "member"}}}
			switch mode {
			case "group":
				message.GroupID++
			case "team":
				raw.Target = "790"
			case "sender":
				raw.Sender = "another"
			case "member_mapping":
				roster.Members[0].Transport = "another"
			case "missing_member":
				roster.Members = nil
			case "partial_roster":
				roster.Complete = false
			case "client":
				raw.Fields[11] = "another"
			case "server":
				message.Quote.ServerID = "9002"
			case "text":
				message.Quote.Text = "not the original"
			case "timestamp":
				raw.Fields[7] = "01"
			case "ciphertext":
				raw.Fields[10] = "unauthenticated"
			case "private":
				raw.Scene = 0
			case "missing_quote":
				message.Quote = nil
			}
			reply, err := verifyTextReply(message, key, "789", roster, raw)
			if mode == "valid" {
				if err != nil || reply == nil || reply.Sender != "8" || reply.Target != "789" ||
					reply.ClientID != outboundClientID || reply.ServerID != "9001" {
					t.Fatal("trusted reference rejected", reply, err)
				}
			} else if err == nil || reply != nil {
				t.Fatal("untrusted quote admitted", mode, reply)
			}
		})
	}
}
