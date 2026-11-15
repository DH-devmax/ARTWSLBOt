package nativewire

import (
	"encoding/binary"
	"slices"
	"testing"
)

func TestDesktopBroadcastNotificationIsInert(t *testing.T) {
	body, err := Properties([]Property{
		{Key: 1, Value: []byte("synthetic-broadcast")},
		{Key: 2, Value: []byte("synthetic-source")},
		{Key: 3, Value: []byte("synthetic-user")},
		{Key: 4, Value: []byte("9999999999999")},
		{Key: 5, Value: []byte(`{"url":"https://invalid.example/never-fetch","action":"never-execute"}`)},
	})
	if err != nil {
		t.Fatal(err)
	}
	duplicate, _ := Properties([]Property{{Key: 5, Value: []byte("first")}, {Key: 5, Value: []byte("second")}})
	for _, wrapper := range []byte{0, 1, 2, 10, 11} {
		wrap := func(payload []byte, code uint16) Frame {
			if wrapper == 0 {
				return Frame{Service: 7, Command: 17, Code: code, Body: payload}
			}
			packet, _ := Packet(7, 17, 1, payload)
			if code != 200 {
				packet[5] = 2
				packet = append(packet[:6], append(binary.LittleEndian.AppendUint16(nil, code), packet[6:]...)...)
			}
			return Frame{Service: 4, Command: wrapper, Code: 200,
				Body: append(binary.LittleEndian.AppendUint64(nil, ^uint64(0)), packet...)}
		}
		push, err := DecodePush(wrap(body, 200))
		if err != nil || push.Kind != "notice" || len(push.Messages) != 0 || push.Boundary != nil {
			t.Fatalf("broadcast became chat or cursor: wrapper=%d err=%v", wrapper, err)
		}
		for length := range len(body) {
			if _, err := DecodePush(wrap(body[:length], 200)); err != ErrPushBoundary {
				t.Fatalf("truncated broadcast accepted: wrapper=%d length=%d", wrapper, length)
			}
		}
		large := append([]byte{1, 5}, Varint(Limit)...)
		large = append(large, make([]byte, Limit)...)
		for _, bad := range [][]byte{append(slices.Clone(body), 0), duplicate, Varint(129), large} {
			if _, err := DecodePush(wrap(bad, 200)); err != ErrPushBoundary {
				t.Fatal("invalid broadcast boundary accepted")
			}
		}
		if _, err := DecodePush(wrap(body, 403)); err != ErrPushBoundary {
			t.Fatal("non-success broadcast accepted")
		}
		if push, err = DecodePush(wrap([]byte{0}, 200)); err != nil || push.Kind != "notice" ||
			len(push.Messages) != 0 || push.Boundary != nil {
			t.Fatal("empty broadcast Property rejected")
		}
	}
	for _, command := range []byte{16, 18} {
		if _, err := DecodePush(Frame{Service: 7, Command: command, Code: 200, Body: body}); err != ErrPushBoundary {
			t.Fatal("unreviewed adjacent broadcast command accepted")
		}
	}
}

func TestDesktopProxyNotificationIsInert(t *testing.T) {
	body, err := Properties([]Property{
		{Key: 1, Value: []byte("synthetic-source")},
		{Key: 2, Value: []byte(`{"url":"https://invalid.example/never-fetch","action":"never-execute"}`)},
		{Key: 3, Value: []byte("9999999999999")},
	})
	if err != nil {
		t.Fatal(err)
	}
	duplicate, _ := Properties([]Property{{Key: 2, Value: []byte("first")}, {Key: 2, Value: []byte("second")}})
	for _, wrapper := range []byte{0, 1, 2, 10, 11} {
		wrap := func(payload []byte, code uint16) Frame {
			if wrapper == 0 {
				return Frame{Service: 22, Command: 2, Code: code, Body: payload}
			}
			packet, _ := Packet(22, 2, 1, payload)
			if code != 200 {
				packet[5] = 2
				packet = append(packet[:6], append(binary.LittleEndian.AppendUint16(nil, code), packet[6:]...)...)
			}
			return Frame{Service: 4, Command: wrapper, Code: 200,
				Body: append(binary.LittleEndian.AppendUint64(nil, ^uint64(0)), packet...)}
		}
		push, err := DecodePush(wrap(body, 200))
		if err != nil || push.Kind != "notice" || len(push.Messages) != 0 || push.Boundary != nil {
			t.Fatalf("proxy notification became chat or a recovery cursor: wrapper=%d err=%v", wrapper, err)
		}
		for length := range len(body) {
			if _, err := DecodePush(wrap(body[:length], 200)); err != ErrPushBoundary {
				t.Fatalf("truncated proxy notification accepted: wrapper=%d length=%d", wrapper, length)
			}
		}
		large := append([]byte{1, 2}, Varint(Limit)...)
		large = append(large, make([]byte, Limit)...)
		for _, bad := range [][]byte{
			append(slices.Clone(body), 0), duplicate, Varint(129), large,
		} {
			if _, err := DecodePush(wrap(bad, 200)); err != ErrPushBoundary {
				t.Fatal("invalid proxy notification boundary accepted")
			}
		}
		if _, err := DecodePush(wrap(body, 403)); err != ErrPushBoundary {
			t.Fatal("non-success proxy notification accepted")
		}
		push, err = DecodePush(wrap([]byte{0}, 200))
		if err != nil || push.Kind != "notice" || len(push.Messages) != 0 || push.Boundary != nil {
			t.Fatal("empty proxy Property rejected")
		}
	}
	for _, command := range []byte{1, 3} {
		if _, err := DecodePush(Frame{Service: 22, Command: command, Code: 200, Body: body}); err != ErrPushBoundary {
			t.Fatal("unreviewed proxy command accepted")
		}
	}
}

func TestDesktopSignalingNotificationsAreInert(t *testing.T) {
	props, err := Properties([]Property{
		{Key: 1, Value: []byte("1")}, {Key: 3, Value: []byte("synthetic-channel")},
		{Key: 4, Value: []byte("9999999999999")}, {Key: 20, Value: []byte("opaque-signaling-extension")},
	})
	if err != nil {
		t.Fatal(err)
	}
	duplicate, _ := Properties([]Property{{Key: 1, Value: []byte("a")}, {Key: 1, Value: []byte("b")}})
	for _, command := range []byte{11, 12, 13, 14} {
		body := slices.Clone(props)
		if command >= 13 {
			body = append([]byte{1}, props...)
		}
		for _, wrapper := range []byte{0, 1, 2, 10, 11} {
			wrap := func(payload []byte, code uint16) Frame {
				inner := Frame{Service: 15, Command: command, Code: code, Body: payload}
				if wrapper == 0 {
					return inner
				}
				packet, _ := Packet(15, command, 1, payload)
				if code != 200 {
					packet[5] = 2
					packet = append(packet[:6], append(binary.LittleEndian.AppendUint16(nil, code), packet[6:]...)...)
				}
				return Frame{Service: 4, Command: wrapper, Code: 200,
					Body: append(binary.LittleEndian.AppendUint64(nil, ^uint64(0)), packet...)}
			}
			push, err := DecodePush(wrap(body, 200))
			if err != nil || push.Kind != "notice" || len(push.Messages) != 0 || push.Boundary != nil {
				t.Fatalf("signaling became a message or recovery cursor: command=%d wrapper=%d err=%v", command, wrapper, err)
			}
			for length := range len(body) {
				if _, err := DecodePush(wrap(body[:length], 200)); err != ErrPushBoundary {
					t.Fatalf("truncated signaling accepted: command=%d wrapper=%d length=%d", command, wrapper, length)
				}
			}
			bad := [][]byte{append(slices.Clone(body), 0)}
			if command < 13 {
				bad = append(bad, duplicate, Varint(129))
			} else {
				bad = append(bad, append([]byte{1}, duplicate...), Varint(1025))
			}
			for _, payload := range bad {
				if _, err := DecodePush(wrap(payload, 200)); err != ErrPushBoundary {
					t.Fatal("invalid signaling boundary accepted")
				}
			}
			if _, err := DecodePush(wrap(body, 403)); err != ErrPushBoundary {
				t.Fatal("non-success signaling notification accepted")
			}
		}
		if command >= 13 {
			push, err := DecodePush(Frame{Service: 15, Command: command, Code: 200, Body: []byte{0}})
			if err != nil || push.Kind != "notice" || push.Boundary != nil || len(push.Messages) != 0 {
				t.Fatal("empty signaling synchronization array rejected")
			}
		}
	}
	for _, command := range []byte{10, 15, 16} {
		if _, err := DecodePush(Frame{Service: 15, Command: command, Code: 200, Body: props}); err != ErrPushBoundary {
			t.Fatal("unreviewed signaling command accepted")
		}
	}
}

func TestDesktopSelfProfileSyncIsInert(t *testing.T) {
	body, _ := Properties([]Property{
		{1, []byte("synthetic-subject")}, {2, []byte("fixture")},
		{10, []byte("synthetic-avatar")}, {11, []byte("synthetic-extension")}, {13, []byte("1700000000000")},
	})
	duplicate, _ := Properties([]Property{{1, []byte("first")}, {1, []byte("second")}})
	for _, wrapped := range []bool{false, true} {
		frame := Frame{Service: 3, Command: 110, Code: 200, Body: body}
		wrap := func(inner Frame) Frame {
			if !wrapped {
				return inner
			}
			packet, _ := Packet(inner.Service, inner.Command, 1, inner.Body)
			return Frame{Service: 4, Command: 1, Code: 200, Body: append(make([]byte, 8), packet...)}
		}
		push, err := DecodePush(wrap(frame))
		if err != nil || push.Kind != "notice" || push.Boundary != nil || len(push.Messages) != 0 {
			t.Fatal("profile sync became a message, identity proof or recovery watermark", err)
		}
		for length := range len(body) {
			frame.Body = body[:length]
			if _, err := DecodePush(wrap(frame)); err != ErrPushBoundary {
				t.Fatalf("truncated profile sync accepted: wrapped=%v length=%d", wrapped, length)
			}
		}
		for _, bad := range [][]byte{append(slices.Clone(body), 0), duplicate, Varint(129)} {
			frame.Body = bad
			if _, err := DecodePush(wrap(frame)); err != ErrPushBoundary {
				t.Fatal("invalid profile sync boundary accepted")
			}
		}
		original := wrap(Frame{Service: 3, Command: 110, Code: 200, Body: body})
		original.Code = 403
		if _, err := DecodePush(original); err != ErrPushBoundary {
			t.Fatal("non-success profile sync accepted")
		}
	}
	for _, command := range []byte{109, 111} {
		if _, err := DecodePush(Frame{Service: 3, Command: command, Code: 200, Body: body}); err != ErrPushBoundary {
			t.Fatal("unreviewed adjacent profile sync accepted")
		}
	}
}

func TestFriendOperationNotificationIsInert(t *testing.T) {
	body, _ := Properties([]Property{
		{1, []byte("synthetic-applicant")}, {2, []byte("synthetic-recipient")},
		{3, []byte("synthetic-operator")}, {4, []byte("2")}, {5, []byte("0")},
		{6, []byte("1700000000000")}, {7, []byte("1700000000001")},
		{8, []byte(`[{"accid":"synthetic-applicant","msg":"fixture","time":1}]`)},
		{9, []byte("1700000000002")},
	})
	frame := Frame{Service: 4, Command: 31, Code: 200, Body: body}
	push, err := DecodePush(frame)
	if err != nil || push.Kind != "notice" || len(push.Messages) != 0 || push.Boundary != nil {
		t.Fatal("friend notification became an action, chat message or recovery cursor", err)
	}
	for length := range len(body) {
		frame.Body = body[:length]
		if _, err := DecodePush(frame); err != ErrPushBoundary {
			t.Fatalf("truncated friend notification accepted at %d", length)
		}
	}
	duplicate, _ := Properties([]Property{{1, []byte("a")}, {1, []byte("b")}})
	for _, bad := range [][]byte{append(slices.Clone(body), 0), duplicate, Varint(129)} {
		frame.Body = bad
		if _, err := DecodePush(frame); err != ErrPushBoundary {
			t.Fatal("invalid friend notification framing accepted")
		}
	}
	for _, command := range []byte{30, 32} {
		if _, err := DecodePush(Frame{Service: 4, Command: command, Code: 200, Body: body}); err != ErrPushBoundary {
			t.Fatal("unreviewed neighboring notification accepted")
		}
	}
	frame.Body, frame.Code = body, 403
	if _, err := DecodePush(frame); err != ErrPushBoundary {
		t.Fatal("friend notification error code accepted")
	}
}

func TestDesktopReceiptAndDeleteTagsAreInert(t *testing.T) {
	props, _ := Properties([]Property{{Key: 2, Value: []byte("synthetic-peer")},
		{Key: 7, Value: []byte("123456789")}, {Key: 12, Value: []byte("1")}})
	for _, command := range []byte{12, 124} {
		body := slices.Clone(props)
		if command == 124 {
			body = append([]byte{1}, props...)
		}
		for _, wrapped := range []bool{false, true} {
			frame := Frame{Service: 7, Command: command, Code: 200, Body: body}
			if wrapped {
				inner, _ := Packet(7, command, 9, body)
				frame = Frame{Service: 4, Command: 1, Code: 200, Body: append(make([]byte, 8), inner...)}
			}
			push, err := DecodePush(frame)
			if err != nil || push.Kind != "notice" || push.Boundary != nil || len(push.Messages) != 0 {
				t.Fatalf("notification became a message, ACK candidate or cursor: %v", err)
			}
			original := slices.Clone(frame.Body)
			for length := range len(original) {
				frame.Body = original[:length]
				if _, err := DecodePush(frame); err != ErrPushBoundary {
					t.Fatalf("truncated notification accepted: command %d length %d", command, length)
				}
			}
			frame.Body = append(original, 0)
			if _, err := DecodePush(frame); err != ErrPushBoundary {
				t.Fatal("notification with trailing bytes accepted")
			}
		}
	}
	for _, command := range []byte{11, 123, 125} {
		if _, err := DecodePush(Frame{Service: 7, Command: command, Code: 200, Body: props}); err != ErrPushBoundary {
			t.Fatal("unreviewed neighboring notification accepted")
		}
	}
	if _, err := DecodePush(Frame{Service: 7, Command: 124, Code: 200, Body: []byte{0x81, 0x08}}); err != ErrPushBoundary {
		t.Fatal("oversized notification array accepted")
	}
}

func TestDesktopTeamMemberSyncIsInert(t *testing.T) {
	body, _ := Properties([]Property{{1, []byte("123")}, {3, []byte("456")},
		{5, []byte("synthetic-client")}, {8, []byte("1")}, {12, []byte("synthetic-profile")}})
	for _, wrapped := range []bool{false, true} {
		frame := Frame{Service: 8, Command: 119, Code: 200, Body: body}
		if wrapped {
			inner, _ := Packet(8, 119, 7, body)
			frame = Frame{Service: 4, Command: 1, Code: 200, Body: append(make([]byte, 8), inner...)}
		}
		push, err := DecodePush(frame)
		if err != nil || push.Kind != "notice" || push.Boundary != nil || len(push.Messages) != 0 {
			t.Fatal("member sync became a message/role grant/watermark", err)
		}
		for _, bad := range [][]byte{frame.Body[:len(frame.Body)-1], append(slices.Clone(frame.Body), 0)} {
			frame.Body = bad
			if _, err = DecodePush(frame); err != ErrPushBoundary {
				t.Fatal("malformed member sync boundary accepted")
			}
		}
	}
	for _, command := range []byte{118, 120} {
		if _, err := DecodePush(Frame{Service: 8, Command: command, Code: 200, Body: body}); err != ErrPushBoundary {
			t.Fatal("unreviewed adjacent route accepted")
		}
	}
}

func TestDesktopLinkDetectionIsInertAndBounded(t *testing.T) {
	body, err := Properties([]Property{{Key: 1, Value: []byte("synthetic")}})
	if err != nil {
		t.Fatal(err)
	}
	for _, command := range []byte{31, 37} {
		frame := Frame{Service: 6, Command: command, Code: 200, Body: body}
		push, err := DecodePush(frame)
		if err != nil || push.Kind != "notice" || len(push.Messages) != 0 || push.Boundary != nil {
			t.Fatal("link notification was not an inert notice")
		}
		for _, bad := range [][]byte{body[:len(body)-1], append(append([]byte{}, body...), 0)} {
			frame.Body = bad
			if _, err := DecodePush(frame); err != ErrPushBoundary {
				t.Fatal("link notification accepted an invalid boundary")
			}
		}
	}
}

// A multi-port login notice is not an auth rejection or a recovery watermark.
func TestMultiPortLoginNotice(t *testing.T) {
	props, _ := Properties([]Property{{Key: 1, Value: []byte("synthetic-device")}})
	body := append([]byte{1, 1}, props...)
	frame := Frame{Service: 2, Command: 7, Code: 200, Body: body}
	push, err := DecodePush(frame)
	if err != nil || push.Kind != "notice" || push.Boundary != nil || len(push.Messages) != 0 {
		t.Fatal("multi-port notice not isolated", err)
	}
	for _, malformed := range [][]byte{{}, {1}, body[:len(body)-1], append(body, 1)} {
		frame.Body = malformed
		if _, err := DecodePush(frame); err == nil {
			t.Fatal("partial multi-port boundary accepted")
		}
	}
}

func TestDesktopOfflineNoticeLayouts(t *testing.T) {
	for command, layout := range desktopNoticeLayouts {
		body := []byte{}
		for _, kind := range layout {
			switch kind {
			case 'A', 'S', 'T', 'B', 'Z', 'P':
				body = append(body, 0)
			case 'L':
				body = append(body, make([]byte, 8)...)
			}
		}
		push, err := DecodePush(Frame{Service: 4, Command: command, Code: 200, Body: body})
		if err != nil || push.Kind != "notice" || push.Boundary != nil {
			t.Fatalf("desktop notice %d: %v", command, err)
		}
		for _, bad := range [][]byte{body[:len(body)-1], append(append([]byte{}, body...), 1)} {
			if _, err := DecodePush(Frame{Service: 4, Command: command, Code: 200, Body: bad}); err == nil {
				t.Fatalf("partial desktop notice %d accepted", command)
			}
		}
	}
}

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

func TestDesktopEventPushBoundaries(t *testing.T) {
	event, err := Properties([]Property{
		{1, []byte("1")}, {2, []byte("online")}, {103, []byte("synthetic-account")},
	})
	if err != nil {
		t.Fatal(err)
	}
	array := append(Varint(2), event...)
	array = append(array, event...)
	for _, vector := range []struct {
		command byte
		body    []byte
	}{{2, event}, {9, array}, {9, Varint(0)}} {
		frame := Frame{Service: 14, Command: vector.command, Code: 200, Body: vector.body}
		out, err := DecodePush(frame)
		if err != nil || out.Kind != "notice" || len(out.Messages) != 0 || out.Boundary != nil {
			t.Fatal("event push must not become a chat message, ACK or cursor")
		}
		for _, malformed := range [][]byte{
			append(slices.Clone(vector.body), 0),
			vector.body[:len(vector.body)-1],
		} {
			frame.Body = malformed
			if _, err := DecodePush(frame); err != ErrPushBoundary {
				t.Fatal("malformed event boundary accepted")
			}
		}
	}
	for _, frame := range []Frame{
		{Service: 14, Command: 9, Code: 200, Body: Varint(1025)},
		{Service: 14, Command: 9, Code: 403, Body: array},
		{Service: 14, Command: 8, Code: 200, Body: array},
	} {
		if _, err := DecodePush(frame); err != ErrPushBoundary {
			t.Fatal("unverified event route accepted")
		}
	}
}

func TestDesktopReliableSessionNotice(t *testing.T) {
	context, _ := Properties([]Property{{1, []byte("0")}, {2, []byte("1")}})
	session, _ := Properties([]Property{{1, []byte("synthetic-session")}, {2, []byte("7")}})
	for _, body := range [][]byte{
		append(slices.Clone(context), Varint(0)...),
		append(append(slices.Clone(context), Varint(1)...), session...),
	} {
		frame := Frame{Service: 4, Command: 25, Code: 200, Body: body}
		out, err := DecodePush(frame)
		if err != nil || out.Kind != "notice" || len(out.Messages) != 0 || out.Boundary != nil {
			t.Fatal("session metadata must not become messages or a global watermark")
		}
		for _, malformed := range [][]byte{
			body[:len(body)-1], append(slices.Clone(body), 0),
			append(slices.Clone(context), Varint(1025)...),
		} {
			frame.Body = malformed
			if _, err := DecodePush(frame); err != ErrPushBoundary {
				t.Fatal("unverified session metadata accepted")
			}
		}
	}
}

func TestDesktopSessionReadNoticeAndWrapper(t *testing.T) {
	target := "synthetic-session"
	body := append([]byte{1}, Varint(uint64(len(target)))...)
	body = append(body, target...)
	body = binary.LittleEndian.AppendUint64(body, 456)
	inner := Frame{Service: 7, Command: 116, Code: 200, Body: body}
	packet, _ := Packet(7, 116, 42, body)
	wrapper := Frame{Service: 4, Command: 1, Code: 200, Body: append(make([]byte, 8), packet...)}
	extended := binary.LittleEndian.AppendUint64(slices.Clone(body), 987)
	extendedPacket, _ := Packet(7, 116, 42, extended)
	extendedWrapper := Frame{Service: 4, Command: 1, Code: 200, Body: append(make([]byte, 8), extendedPacket...)}
	for _, frame := range []Frame{inner, wrapper, extendedWrapper} {
		out, err := DecodePush(frame)
		if err != nil || out.Kind != "notice" || out.Boundary != nil || len(out.Messages) != 0 {
			t.Fatal("session-read synchronization was not a bounded notice")
		}
	}
	for _, malformed := range [][]byte{
		nil, {0}, {2, 0}, {1, 255, 255, 255, 255, 127},
		body[:len(body)-1], append(slices.Clone(body), 0),
		extended[:len(extended)-1], append(slices.Clone(extended), 0),
		append([]byte{1, 0}, make([]byte, 8)...),
	} {
		inner.Body = malformed
		if _, err := DecodePush(inner); err != ErrPushBoundary {
			t.Fatal("malformed session-read notice accepted")
		}
	}
	unknown, _ := Packet(99, 99, 42, body)
	wrapper.Body = append(make([]byte, 8), unknown...)
	if _, err := DecodePush(wrapper); err != ErrPushBoundary {
		t.Fatal("known wrapper hid an unknown inner route")
	}
}
