package nativewire

import (
	"encoding/binary"
	"testing"
)

func recallFixture(t *testing.T, kind string) []byte {
	t.Helper()
	body, err := Properties([]Property{
		{0, []byte("1700000000000")}, {1, []byte(kind)}, {2, []byte("team")},
		{3, []byte("sender")}, {6, []byte("100")}, {10, []byte("client")},
		{11, []byte("99")}, {16, []byte("actor")},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestRecallNotificationLayoutsStayReadOnly(t *testing.T) {
	frame := Frame{Service: 7, Command: 14, Code: 200, Body: recallFixture(t, "8")}
	got, err := ReadRecallNotices(frame)
	if err != nil || len(got) != 1 || got[0].Scene != 1 || got[0].Target != "team" ||
		got[0].ClientID != "client" || got[0].ServerID != "99" || got[0].Actor != "actor" {
		t.Fatal("recall layout mismatch", err)
	}
	for size := 0; size < len(frame.Body); size++ {
		broken := frame
		broken.Body = frame.Body[:size]
		if _, err := ReadRecallNotices(broken); err == nil {
			t.Fatal("truncated property accepted", size)
		}
	}
	batch := append([]byte{1}, frame.Body...)
	batch = binary.LittleEndian.AppendUint64(batch, 1700000000000)
	batch = append(batch, 1)
	frame.Command, frame.Body = 15, batch
	got, err = ReadRecallNotices(frame)
	if err != nil || len(got) != 1 {
		t.Fatal("offline recall layout mismatch", err)
	}
	frame.Body = append(frame.Body, 0)
	if _, err := ReadRecallNotices(frame); err == nil {
		t.Fatal("trailing bytes accepted")
	}
	frame.Command, frame.Body = 3, nil
	if got, err := ReadRecallNotices(frame); err != nil || len(got) != 0 {
		t.Fatal("delete response fabricated recall")
	}
	frame.Command, frame.Body = 14, recallFixture(t, "100")
	if got, err := ReadRecallNotices(frame); err != nil || len(got) != 0 {
		t.Fatal("custom notice fabricated recall")
	}
}
