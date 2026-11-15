package nativewire

import (
	"encoding/binary"
	"slices"
	"strconv"
	"testing"
)

func TestDesktopFriendSyncNotificationsAreInert(t *testing.T) {
	account := append(Varint(10), []byte("sdk-peer-1")...)
	note := []byte("synthetic friend proof")
	request := append(append(slices.Clone(account), 1), Varint(uint64(len(note)))...)
	request = append(request, note...)
	update, _ := Properties([]Property{{Key: 1, Value: []byte("sdk-peer-1")}})
	for command, body := range map[byte][]byte{101: request, 102: account, 103: update} {
		for _, wrapper := range []byte{0, 1, 2, 10, 11} {
			wrap := func(payload []byte) Frame {
				if wrapper == 0 {
					return Frame{Service: 12, Command: command, Code: 200, Body: payload}
				}
				packet, _ := Packet(12, command, 0, payload)
				return Frame{Service: 4, Command: wrapper, Code: 200,
					Body: append(binary.LittleEndian.AppendUint64(nil, ^uint64(0)), packet...)}
			}
			push, err := DecodePush(wrap(body))
			if err != nil || push.Kind != "notice" || push.Boundary != nil || len(push.Messages) != 0 {
				t.Fatalf("friend sync became message or cursor: cmd=%d wrapper=%d err=%v", command, wrapper, err)
			}
			for length := range len(body) {
				if _, err := DecodePush(wrap(body[:length])); err != ErrPushBoundary {
					t.Fatalf("truncated friend sync accepted: cmd=%d len=%d", command, length)
				}
			}
			trailing := append(slices.Clone(body), 0)
			if command == 101 {
				// One zero is a complete empty Property on a request.
				trailing = append(trailing, 0)
			}
			if _, err := DecodePush(wrap(trailing)); err != ErrPushBoundary {
				t.Fatal("friend sync trailing data accepted")
			}
			if command != 101 {
				if _, err := DecodePush(wrap(append(slices.Clone(body), update...))); err != ErrPushBoundary {
					t.Fatal("friend deletion or update accepted another Property")
				}
			}
		}
		if _, err := DecodePush(Frame{Service: 12, Command: command, Code: 403, Body: body}); err != ErrPushBoundary {
			t.Fatal("non-success friend sync accepted")
		}
	}
	for _, body := range [][]byte{
		{0}, {0x81}, {1, 0xff}, {1, '\n'}, Varint(129),
	} {
		if _, err := DecodePush(Frame{Service: 12, Command: 102, Code: 200, Body: body}); err != ErrPushBoundary {
			t.Fatal("invalid friend identity string accepted")
		}
	}
	for _, tail := range [][]byte{
		{}, {1}, {1, 0x81}, {1, 1, 0xff}, append([]byte{1}, Varint(4097)...),
	} {
		body := append(slices.Clone(account), tail...)
		if _, err := DecodePush(Frame{Service: 12, Command: 101, Code: 200, Body: body}); err != ErrPushBoundary {
			t.Fatal("invalid request note accepted")
		}
	}
	emptyNote := append(slices.Clone(account), 1, 0)
	if push, err := DecodePush(Frame{Service: 12, Command: 101, Code: 200, Body: emptyNote}); err != nil || push.Kind != "notice" {
		t.Fatal("SDK empty request note rejected", err)
	}
	duplicate, _ := Properties([]Property{{Key: 1, Value: []byte("a")}, {Key: 1, Value: []byte("b")}})
	if _, err := DecodePush(Frame{Service: 12, Command: 103, Code: 200, Body: duplicate}); err != ErrPushBoundary {
		t.Fatal("duplicate friend properties accepted")
	}
	for _, command := range []byte{100, 104} {
		if _, err := DecodePush(Frame{Service: 12, Command: command, Code: 200, Body: account}); err != ErrPushBoundary {
			t.Fatal("unreviewed adjacent friend command accepted")
		}
	}
}

func TestDesktopFriendRequestPropertyTailBoundaries(t *testing.T) {
	account := append(Varint(10), []byte("sdk-peer-1")...)
	legacy := append(slices.Clone(account), 1, 0)
	props, _ := Properties([]Property{{Key: 1, Value: []byte("x")}})
	extended := append(slices.Clone(legacy), props...)
	if len(extended) != 17 {
		t.Fatal("synthetic inner body must be 17 bytes")
	}
	opaque, _ := Properties([]Property{{Key: ^uint32(0), Value: []byte{0xff, 0, 0x81}}})
	message, _ := Properties([]Property{
		{Key: 0, Value: []byte("1")}, {Key: 1, Value: []byte("sdk-peer-1")},
		{Key: 2, Value: []byte("owner")}, {Key: 7, Value: []byte("1")},
		{Key: 8, Value: []byte("9999999999999")}, {Key: 12, Value: []byte("123")},
	})
	fields := make([]Property, 128)
	for i := range fields {
		fields[i] = Property{Key: uint32(i)}
	}
	maxFields, _ := Properties(fields)
	tooMany := Varint(129)
	for i := range 129 {
		tooMany = append(tooMany, Varint(uint64(i))...)
		tooMany = append(tooMany, 0)
	}
	duplicate, _ := Properties([]Property{{Key: 1, Value: []byte("a")}, {Key: 1, Value: []byte("b")}})
	note := []byte("synthetic friend proof")
	noted := append(slices.Clone(account), 1)
	noted = append(noted, Varint(uint64(len(note)))...)
	noted = append(noted, note...)
	valid := []struct {
		name   string
		body   []byte
		prefix int
	}{
		{"legacy", legacy, len(legacy)},
		{"empty_property", append(slices.Clone(legacy), 0), len(legacy)},
		{"17_byte_extension", extended, len(legacy)},
		{"opaque_property", append(slices.Clone(legacy), opaque...), len(legacy)},
		{"message_shaped_property", append(slices.Clone(legacy), message...), len(legacy)},
		{"128_fields", append(slices.Clone(legacy), maxFields...), len(legacy)},
		{"nonempty_note", append(slices.Clone(noted), props...), len(noted)},
	}
	invalid := []struct {
		name string
		tail []byte
	}{
		{"truncated_count", []byte{0x81}},
		{"overflow_count", []byte{0xff, 0xff, 0xff, 0xff, 0x10}},
		{"truncated_key", []byte{1, 0x81}},
		{"overflow_key", []byte{1, 0xff, 0xff, 0xff, 0xff, 0x10}},
		{"truncated_size", []byte{1, 1, 0x81}},
		{"overflow_size", []byte{1, 1, 0xff, 0xff, 0xff, 0xff, 0x10}},
		{"truncated_value", []byte{1, 1, 2, 'x'}},
		{"oversized_value", append([]byte{1, 1}, Varint(Limit+1)...)},
		{"duplicate_key", duplicate},
		{"129_fields", tooMany},
		{"two_empty_properties", []byte{0, 0}},
		{"extra_byte", append(slices.Clone(props), 0x81)},
		{"second_empty_property", append(slices.Clone(props), 0)},
		{"second_property", append(slices.Clone(props), props...)},
	}
	for _, wrapper := range []byte{0, 1, 2, 10, 11} {
		wrap := func(payload []byte) Frame {
			if wrapper == 0 {
				return Frame{Service: 12, Command: 101, Code: 200, Body: payload}
			}
			packet, err := Packet(12, 101, 7, payload)
			if err != nil {
				t.Fatal(err)
			}
			return Frame{Service: 4, Command: wrapper, Code: 200,
				Body: append(binary.LittleEndian.AppendUint64(nil, ^uint64(0)), packet...)}
		}
		for _, tc := range valid {
			t.Run(tc.name+"/wrapper_"+strconv.Itoa(int(wrapper)), func(t *testing.T) {
				frame := wrap(tc.body)
				if tc.name == "17_byte_extension" && wrapper != 0 && len(frame.Body) != 31 {
					t.Fatal("synthetic outer body must be 31 bytes")
				}
				push, err := DecodePush(frame)
				if err != nil || push.Kind != "notice" || push.Boundary != nil || len(push.Messages) != 0 {
					t.Fatal("friend request Property became a message or cursor", err)
				}
				for length := range len(tc.body) {
					if length == tc.prefix {
						continue // The complete legacy prefix remains valid.
					}
					if _, err := DecodePush(wrap(tc.body[:length])); err != ErrPushBoundary {
						t.Fatalf("truncated request accepted at %d: %v", length, err)
					}
				}
				if wrapper != 0 && tc.name == "17_byte_extension" {
					for length := range len(frame.Body) {
						if length == 14+len(legacy) {
							continue
						}
						short := frame
						short.Body = frame.Body[:length]
						if _, err := DecodePush(short); err != ErrPushBoundary {
							t.Fatalf("truncated wrapper accepted at %d: %v", length, err)
						}
					}
				}
				frame.Code = 403
				if _, err := DecodePush(frame); err != ErrPushBoundary {
					t.Fatal("non-success friend request accepted")
				}
			})
		}
		for _, tc := range invalid {
			t.Run(tc.name+"/wrapper_"+strconv.Itoa(int(wrapper)), func(t *testing.T) {
				body := append(slices.Clone(legacy), tc.tail...)
				if _, err := DecodePush(wrap(body)); err != ErrPushBoundary {
					t.Fatal("invalid friend request Property tail accepted", err)
				}
			})
		}
	}
	// A complete field still cannot bypass ReadProperties' total body limit.
	oversized := append(slices.Clone(legacy), 1, 1)
	oversized = append(oversized, Varint(Limit)...)
	oversized = append(oversized, make([]byte, Limit)...)
	if _, err := DecodePush(Frame{Service: 12, Command: 101, Code: 200, Body: oversized}); err != ErrPushBoundary {
		t.Fatal("oversized complete Property accepted", err)
	}
}
