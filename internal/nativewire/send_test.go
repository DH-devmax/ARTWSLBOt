package nativewire

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

const syntheticClientID = "e391ad75-cbbc-4ce1-a333-b4b38be7e415"

func syntheticText() GroupText {
	return GroupText{SenderID: 123, GroupID: 456, ClientID: syntheticClientID,
		Text: "synthetic group text", CreatedAt: time.Unix(1700000000, 123000000).UTC()}
}

func TestEncodeGroupTextBounds(t *testing.T) {
	m := syntheticText()
	plain, err := EncodeGroupText(m)
	if err != nil || len(plain) > 4096 {
		t.Fatal("valid synthetic text failed", err)
	}
	for _, mutate := range []func(*GroupText){
		func(m *GroupText) { m.SenderID = 0 },
		func(m *GroupText) { m.GroupID = 1 << 30 },
		func(m *GroupText) { m.Text = " \n" },
		func(m *GroupText) { m.Text = string([]byte{0xff}) },
		func(m *GroupText) { m.Text = strings.Repeat("x", 4096) },
		func(m *GroupText) { m.ClientID = "not-a-client-id" },
		func(m *GroupText) { m.CreatedAt = time.Time{} },
	} {
		bad := m
		mutate(&bad)
		if _, err := EncodeGroupText(bad); err == nil {
			t.Fatal("invalid outbound payload accepted")
		}
	}
}

func TestGroupTextRequestAndReceipts(t *testing.T) {
	body, err := GroupTextRequest("789", syntheticClientID, `{"b":"synthetic"}`)
	fields, end, shape := ReadProperties(body, 0)
	if err != nil || shape != nil || end != len(body) || len(fields) != 5 ||
		string(fields[0]) != "1" || string(fields[1]) != "789" || string(fields[8]) != "100" {
		t.Fatal("unexpected outbound property schema")
	}
	receipt := func(extra ...Property) []byte {
		body, _ := Properties(append([]Property{
			{Key: 12, Value: []byte("18446744073709551615")},
			{Key: 7, Value: []byte("1700000000123")},
		}, extra...))
		return body
	}
	for _, body := range [][]byte{receipt(), receipt(Property{Key: 11, Value: []byte(syntheticClientID)})} {
		out, err := ParseSendReceipt(body, syntheticClientID)
		if err != nil || out.ServerID != "18446744073709551615" || out.ClientID != syntheticClientID {
			t.Fatal("receipt mapping failed", err)
		}
		for _, route := range [][2]byte{{8, 2}, {7, 1}} {
			push, err := DecodePush(Frame{Service: route[0], Command: route[1], Code: 200, Body: body})
			if err != nil || push.Kind != "notice" || push.Boundary != nil || len(push.Messages) != 0 {
				t.Fatal("late send receipt poisoned receive or advanced watermark", err)
			}
		}
	}
	for _, body := range [][]byte{
		receipt(Property{Key: 11, Value: []byte("d391ad75-cbbc-4ce1-a333-b4b38be7e415")}),
		append(receipt(), 0), receipt()[:3], []byte{0},
		receipt(Property{Key: 12, Value: []byte("01")}),
	} {
		if _, err := ParseSendReceipt(body, syntheticClientID); err == nil {
			t.Fatal("ambiguous receipt accepted")
		}
	}
	for _, team := range []string{"", "0", "01", "private/123", "-1", "18446744073709551616"} {
		if _, err := GroupTextRequest(team, syntheticClientID, "synthetic"); err == nil {
			t.Fatal("unverified route accepted")
		}
	}
	if _, err := ParseSendReceipt(bytes.Repeat([]byte{1}, Limit+1), syntheticClientID); err == nil {
		t.Fatal("oversize receipt accepted")
	}
}

func TestRichTextAndPrivateRequestBounds(t *testing.T) {
	valid := syntheticText()
	valid.Text = "\U0001f642 @阿明 hello"
	valid.Mentions = []MessageMention{{UID: 7, Name: "阿明", Start: 3, End: 7}}
	valid.Quote = &MessageQuote{ClientID: syntheticClientID, ServerID: "9001", Text: "quoted"}
	if _, err := EncodeGroupText(valid); err != nil {
		t.Fatal("reviewed mention/quote rejected", err)
	}
	for _, mutate := range []func(*GroupText){
		func(m *GroupText) { m.Mentions[0].Start = 1 },
		func(m *GroupText) { m.Mentions[0].End = 99 },
		func(m *GroupText) { m.Mentions[0].UID = 0 },
		func(m *GroupText) { m.Mentions[0].Name = "another" },
		func(m *GroupText) { m.Mentions = append(m.Mentions, m.Mentions[0]) },
		func(m *GroupText) { m.Quote.ServerID = "18446744073709551616" },
		func(m *GroupText) { m.Quote.Text = " " },
	} {
		bad := valid
		bad.Mentions = append([]MessageMention(nil), valid.Mentions...)
		q := *valid.Quote
		bad.Quote = &q
		mutate(&bad)
		if _, err := EncodeGroupText(bad); err == nil {
			t.Fatal("unsafe rich message accepted")
		}
	}
	if _, err := encodeText(valid, 1); err == nil {
		t.Fatal("private send silently discarded group components")
	}
	if _, err := EncodePrivateText(PrivateText{SenderID: valid.SenderID, PeerID: valid.GroupID, ClientID: valid.ClientID,
		Text: valid.Text, CreatedAt: valid.CreatedAt}); err != nil {
		t.Fatal("typed private target rejected", err)
	}
	body, err := PrivateTextRequest("synthetic-peer", syntheticClientID, "synthetic")
	fields, end, parse := ReadProperties(body, 0)
	if err != nil || parse != nil || end != len(body) || string(fields[0]) != "0" ||
		string(fields[1]) != "synthetic-peer" {
		t.Fatal("private transport route incorrect")
	}
	for _, peer := range []string{"", "someone else", "someone\n", strings.Repeat("x", 129)} {
		if _, err := PrivateTextRequest(peer, syntheticClientID, "synthetic"); err == nil {
			t.Fatal("invalid private peer accepted")
		}
	}
	reply := &ReplyRoute{Sender: "synthetic-peer", Target: "789", TimeMS: "1700000000123",
		ServerID: "9001", ClientID: syntheticClientID}
	body, err = GroupTextReplyRequest("789", syntheticClientID, "synthetic", reply)
	fields, end, parse = ReadProperties(body, 0)
	if err != nil || parse != nil || end != len(body) || len(fields) != 15 ||
		string(fields[29]) != reply.Sender || string(fields[34]) != reply.Sender ||
		string(fields[30]) != "789" || string(fields[38]) != syntheticClientID {
		t.Fatal("reply/root transport metadata not preserved")
	}
	reply.Target = "790"
	if _, err := GroupTextReplyRequest("789", syntheticClientID, "synthetic", reply); err == nil {
		t.Fatal("cross-group quote accepted")
	}
	if _, err := GroupRecallRequest("789", *reply); err == nil {
		t.Fatal("cross-group recall accepted")
	}
	reply.Target = "789"
	body, err = GroupRecallRequest("789", *reply)
	fields, end, parse = ReadProperties(body, 0)
	if err != nil || parse != nil || end != len(body) || len(fields) != 7 || string(fields[1]) != "8" ||
		string(fields[2]) != "789" || string(fields[3]) != reply.Sender || string(fields[16]) != reply.Sender ||
		string(fields[10]) != syntheticClientID || string(fields[11]) != "9001" {
		t.Fatal("group recall contract changed")
	}
}
