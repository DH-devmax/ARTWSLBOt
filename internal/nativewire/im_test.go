package nativewire

import (
	"encoding/binary"
	"slices"
	"testing"
)

func imFixture(t *testing.T) []byte {
	t.Helper()
	body, err := Properties([]Property{
		{0, []byte("1")}, {1, []byte("team-1")}, {2, []byte("sender-1")},
		{7, []byte("1700000000000")}, {8, []byte("100")}, {10, []byte("synthetic-ciphertext")},
		{12, []byte("18446744073709551615")}, {99, []byte{255, 254}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return body
}

func TestIMMessageBoundaryVectors(t *testing.T) {
	body := imFixture(t)
	for _, route := range [][2]byte{{7, 2}, {7, 101}, {8, 3}, {8, 102}, {4, 4}, {4, 9}, {8, 4}} {
		payload := body
		if route[0] == 4 || route[1] == 4 {
			payload = append(Varint(1), body...)
		}
		out, err := DecodePush(Frame{Service: route[0], Command: route[1], Code: 200, Body: payload})
		if err != nil || len(out.Messages) != 1 || out.Messages[0].Scene != 1 {
			t.Fatalf("pinned route rejected %v: %v", route, err)
		}
		if len(out.Messages[0].Properties[99]) != 2 {
			t.Fatal("binary extension was lost before persistence")
		}
		ack, err := ACKBody(1, out.Messages[0].ServerID)
		if err != nil || len(ack) != 11 || binary.LittleEndian.Uint64(ack[3:]) != ^uint64(0) {
			t.Fatal("ACK must preserve uint64 server ID")
		}
	}
	inner, _ := Packet(8, 3, 7, body)
	wrapped := append(make([]byte, 8), inner...)
	out, err := DecodePush(Frame{Service: 4, Command: 1, Code: 200, Body: wrapped})
	if err != nil || len(out.Messages) != 1 {
		t.Fatal("notification wrapper rejected")
	}
	for _, malformed := range []Frame{
		{Service: 8, Command: 3, Code: 200, Body: append(body, 0)},
		{Service: 8, Command: 4, Code: 200, Body: Varint(1025)},
		{Service: 5, Command: 1, Code: 200, Body: make([]byte, 9)},
		{Service: 255, Command: 255, Code: 200},
		{Service: 8, Command: 3, Code: 403, Body: body},
	} {
		if _, err := DecodePush(malformed); err != ErrPushBoundary {
			t.Fatal("unknown boundary accepted")
		}
	}
	if _, err = ACKBody(0, "01"); err == nil {
		t.Fatal("noncanonical server ID accepted")
	}
	n := uint64(998)
	out, err = DecodePush(Frame{Service: 5, Command: 1, Code: 200, Body: binary.LittleEndian.AppendUint64(nil, n)})
	if err != nil || out.Boundary == nil || *out.Boundary != n {
		t.Fatal("sync boundary lost")
	}
}

func TestIMLoginShape(t *testing.T) {
	valid, _ := Properties([]Property{{102, []byte("connection-1")}})
	if ValidateIMLogin(valid) != nil {
		t.Fatal("valid login rejected")
	}
	for _, body := range [][]byte{nil, {0}, append(slices.Clone(valid), 129, 1), append(slices.Clone(valid), 0, 0, 1)} {
		if ValidateIMLogin(body) == nil {
			t.Fatal("malformed login accepted")
		}
	}
}
