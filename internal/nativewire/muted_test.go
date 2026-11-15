package nativewire

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func syntheticMuteList(t *testing.T, team uint64, peers ...string) []byte {
	t.Helper()
	body := binary.LittleEndian.AppendUint64(nil, team)
	body = append(body, Varint(uint64(len(peers)))...)
	for _, peer := range peers {
		row, err := Properties([]Property{
			{Key: 3, Value: []byte(peer)}, {Key: 9, Value: []byte("1")}, {Key: 13, Value: []byte("1")},
		})
		if err != nil {
			t.Fatal(err)
		}
		body = append(body, row...)
	}
	return body
}

func TestMutedMembersStrictFraming(t *testing.T) {
	request, err := MutedMembersRequest("789")
	if err != nil || !bytes.Equal(request, []byte{21, 3, 0, 0, 0, 0, 0, 0}) {
		t.Fatal("SDK mute query changed")
	}
	for _, peers := range [][]string{nil, {"7", "8"}} {
		got, err := ParseMutedMembers(syntheticMuteList(t, 789, peers...), "789")
		if err != nil || len(got) != len(peers) {
			t.Fatal("complete mute list rejected", err)
		}
	}
	valid := syntheticMuteList(t, 789, "7")
	if _, err := ParseMutedMembers(valid, "788"); err == nil {
		t.Fatal("query accepted another team's mute list")
	}
	notice, err := DecodePush(Frame{Service: 8, Command: 27, Code: 200, Body: valid})
	if err != nil || notice.Kind != "notice" || notice.Boundary != nil || len(notice.Messages) != 0 {
		t.Fatal("late mute read poisoned stream or advanced cursor", err)
	}
	if _, err := DecodePush(Frame{Service: 8, Command: 28, Code: 200, Body: valid}); err == nil {
		t.Fatal("adjacent unknown SDK route silently ignored")
	}
	badFlag := append([]byte(nil), valid...)
	badFlag[len(badFlag)-1] = '0'
	for _, body := range [][]byte{
		valid[:8], valid[:len(valid)-1], append(append([]byte(nil), valid...), 0),
		syntheticMuteList(t, 789, "7", "7"),
		syntheticMuteList(t, 789, "0"), syntheticMuteList(t, 789, "07"),
		syntheticMuteList(t, 789, "4294967296"), syntheticMuteList(t, 789, "someone"),
		badFlag, bytes.Repeat([]byte{0}, Limit+1),
	} {
		if _, err := ParseMutedMembers(body, "789"); err == nil {
			t.Fatal("partial or ambiguous mute evidence accepted")
		}
		if _, err := DecodePush(Frame{Service: 8, Command: 27, Code: 200, Body: body}); err == nil {
			t.Fatal("malformed late mute reply bypassed stream boundary validation")
		}
	}
	for _, team := range []string{"0", "01", "-1", "18446744073709551616", "business/7"} {
		if _, err := MutedMembersRequest(team); err == nil {
			t.Fatal("invalid team accepted")
		}
	}
}
