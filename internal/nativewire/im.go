// SPDX-License-Identifier: AGPL-3.0-or-later
package nativewire

import (
	"encoding/binary"
	"errors"
	"strconv"
	"unicode/utf8"
)

var ErrPushBoundary = errors.New("native_push_boundary_unknown")

type IMMessage struct {
	Scene      int               `json:"scene"`
	Target     string            `json:"target"`
	Sender     string            `json:"sender"`
	ServerID   string            `json:"server_id"`
	Fields     map[uint32]string `json:"fields"`
	Properties map[uint32][]byte `json:"properties"`
}

type Push struct {
	Messages []IMMessage
	Boundary *uint64
	Kind     string
}

// DecodePush verifies the complete frame before returning any ACK candidates.
// Application ciphertext remains opaque until native/business group mapping.
func DecodePush(f Frame) (Push, error) {
	if f.Service == 8 && f.Command == 2 || f.Service == 7 && f.Command == 1 {
		// A timed-out waiter may have gone away before this send response
		// arrives. Preserve the bounded opaque frame as evidence, not a chat
		// message/ACK/watermark, and do not poison an otherwise healthy stream.
		// This does not resolve an unknown operation or claim delivery.
		if f.Code != 200 && f.Code >= 100 {
			return Push{Kind: "notice"}, nil
		}
		if f.Code != 200 {
			return Push{}, ErrPushBoundary
		}
		if _, err := parseUnmatchedSendReceipt(f.Body); err != nil {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	}
	if f.Service == 8 && f.Command == 27 {
		// A cancelled read may still receive its SDK mute-list reply. Validate
		// framing before retaining it as a notice; it is not a role grant,
		// permission cache, message, ACK candidate or recovery watermark.
		if f.Code != 200 && f.Code >= 100 {
			return Push{Kind: "notice"}, nil
		}
		if f.Code != 200 || len(f.Body) < 9 {
			return Push{}, ErrPushBoundary
		}
		team := strconv.FormatUint(binary.LittleEndian.Uint64(f.Body[:8]), 10)
		if _, err := ParseMutedMembers(f.Body, team); err != nil {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	}
	if f.Code != 200 {
		return Push{}, ErrPushBoundary
	}
	if f.Service == 4 && (f.Command == 1 || f.Command == 2 || f.Command == 10 || f.Command == 11) {
		if len(f.Body) < 8 {
			return Push{}, ErrPushBoundary
		}
		inner, err := ReadPacket(f.Body[8:])
		if err != nil || inner.Service == 4 {
			return Push{}, ErrPushBoundary
		}
		return DecodePush(inner)
	}
	if f.Service == 5 && f.Command == 1 {
		if len(f.Body) != 8 {
			return Push{}, ErrPushBoundary
		}
		boundary := binary.LittleEndian.Uint64(f.Body)
		return Push{Boundary: &boundary, Kind: "boundary"}, nil
	}
	if f.Service == 4 {
		if layout, known := desktopNoticeLayouts[f.Command]; known {
			if !validNoticeLayout(f.Body, layout) {
				return Push{}, ErrPushBoundary
			}
			return Push{Kind: "notice"}, nil
		}
	}
	count, offset := uint64(1), 0
	switch {
	case f.Service == 12 && (f.Command == 101 || f.Command == 102 || f.Command == 103):
		// Friend sync: legacy requests are String/Byte/String; Android 3.7.4
		// appends one Property. Deletion is String, update is Property.
		// Keep these inert: neither friendship nor a note proves group ownership.
		if f.Command == 103 {
			if !validNoticeLayout(f.Body, "P") {
				return Push{}, ErrPushBoundary
			}
		} else {
			size, next, err := ReadVarint(f.Body, 0, 32)
			if err != nil || size == 0 || size > 128 || size > uint64(len(f.Body)-next) ||
				!identifier(string(f.Body[next:next+int(size)])) {
				return Push{}, ErrPushBoundary
			}
			offset = next + int(size)
			if f.Command == 101 {
				if offset >= len(f.Body) {
					return Push{}, ErrPushBoundary
				}
				size, next, err = ReadVarint(f.Body, offset+1, 32)
				if err != nil || size > 4096 || size > uint64(len(f.Body)-next) ||
					!utf8.Valid(f.Body[next:next+int(size)]) {
					return Push{}, ErrPushBoundary
				}
				offset = next + int(size)
				if offset < len(f.Body) {
					_, offset, err = ReadProperties(f.Body, offset)
					if err != nil {
						return Push{}, ErrPushBoundary
					}
				}
			}
			if offset != len(f.Body) {
				return Push{}, ErrPushBoundary
			}
		}
		return Push{Kind: "notice"}, nil
	case f.Service == 3 && f.Command == 110:
		// Pinned desktop syncUpdateMyInfo carries one complete user Property.
		// Retain it without changing identity, permissions or recovery cursors.
		if !validNoticeLayout(f.Body, "P") {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	case f.Service == 7 && (f.Command == 12 || f.Command == 124):
		// Pinned SDK msgReceipt / onDeleteMsgSelfBatch. Retain the complete
		// notice without changing local history, operation status or cursors.
		layout := "P"
		if f.Command == 124 {
			layout = "A"
		}
		if !validNoticeLayout(f.Body, layout) {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	case f.Service == 7 && f.Command == 17:
		// Desktop broadcastMsg is one Property. Retain global broadcast
		// metadata only; never turn its body/timestamp into chat or a cursor.
		if !validNoticeLayout(f.Body, "P") {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	case f.Service == 8 && f.Command == 119:
		// Desktop SDK syncUpdateTeamMember: one complete teamMember Property.
		// This is directory metadata, not a message, role grant or watermark.
		_, next, err := ReadProperties(f.Body, 0)
		if err != nil || next != len(f.Body) {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	case f.Service == 7 && (f.Command == 2 || f.Command == 101),
		f.Service == 8 && (f.Command == 3 || f.Command == 102):
	case f.Service == 4 && (f.Command == 4 || f.Command == 9), f.Service == 8 && f.Command == 4:
		var err error
		count, offset, err = ReadVarint(f.Body, 0, 32)
		if err != nil {
			return Push{}, ErrPushBoundary
		}
	case f.Service == 7 && (f.Command == 3 || f.Command == 14 || f.Command == 15):
		// Recall notifications are retained as opaque notices, never as actions.
		return Push{Kind: "notice"}, nil
	case f.Service == 14 && (f.Command == 2 || f.Command == 9):
		// Desktop SDK eventService pushes are presence/custom events, not chat
		// messages. Validate their complete property boundary before retaining.
		var err error
		if f.Command == 9 {
			offset, err = readPropertyArray(f.Body, 0)
		} else {
			_, offset, err = ReadProperties(f.Body, 0)
		}
		if err != nil || offset != len(f.Body) {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	case f.Service == 15 && f.Command >= 11 && f.Command <= 14:
		// Pinned desktop avSignal notifications contain one Property (11/12)
		// or a PropertyArray (13/14). Retain complete signaling metadata only;
		// never join/answer a call, emit chat ACKs or infer a recovery cursor.
		layout := "P"
		if f.Command >= 13 {
			layout = "A"
		}
		if !validNoticeLayout(f.Body, layout) {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	case f.Service == 22 && f.Command == 2:
		// Desktop proxyService.onProxyMsg is one complete Property. Retain it
		// opaquely; its body is not a request, chat message or recovery cursor.
		if !validNoticeLayout(f.Body, "P") {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	case f.Service == 6 && (f.Command == 31 || f.Command == 37):
		// Desktop link.notifyDetect/binarySocketDetect carries one Property.
		// Retain only; do not follow upstream URLs or run detection commands.
		_, next, err := ReadProperties(f.Body, 0)
		if err != nil || next != len(f.Body) {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	case f.Service == 2 && f.Command == 7:
		// Pinned desktop SDK auth.multiPortLogin: Byte state followed by
		// PropertyArray loginPorts. A device-presence notice, not a kick.
		if len(f.Body) < 2 {
			return Push{}, ErrPushBoundary
		}
		next, err := readPropertyArray(f.Body, 1)
		if err != nil || next != len(f.Body) {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	case f.Service == 4 && f.Command == 25:
		// SDK syncSessionReliableInfo: context Property + datas PropertyArray.
		// This session metadata is not a message or the global sync watermark.
		_, next, err := ReadProperties(f.Body, 0)
		if err != nil {
			return Push{}, ErrPushBoundary
		}
		offset, err = readPropertyArray(f.Body, next)
		if err != nil || offset != len(f.Body) {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	case f.Service == 7 && f.Command == 116:
		// Other clients' session-read synchronization: Byte scene, String to,
		// Long timetag. The observed desktop server can append one opaque Long;
		// retain it, but never interpret either field as our recovery cursor.
		if len(f.Body) < 1 || f.Body[0] > 1 {
			return Push{}, ErrPushBoundary
		}
		size, next, err := ReadVarint(f.Body, 1, 32)
		if err != nil || size == 0 || size > 128 {
			return Push{}, ErrPushBoundary
		}
		remaining := uint64(len(f.Body) - next)
		if remaining != size+8 && remaining != size+16 ||
			!identifier(string(f.Body[next:next+int(size)])) {
			return Push{}, ErrPushBoundary
		}
		return Push{Kind: "notice"}, nil
	case f.Service == 4 && f.Command == 5 && len(f.Body) == 0:
		return Push{Kind: "notice"}, nil // optional ACK receipt
	default:
		return Push{}, ErrPushBoundary
	}
	if count > 1024 {
		return Push{}, ErrPushBoundary
	}
	out := Push{Kind: "messages"}
	for range count {
		fields, next, err := ReadProperties(f.Body, offset)
		if err != nil {
			return Push{}, ErrPushBoundary
		}
		offset = next
		m := IMMessage{Fields: make(map[uint32]string, len(fields)), Properties: fields}
		for key, value := range fields {
			if !utf8.Valid(value) {
				// Binary extensions remain in the sealed original properties.
				continue
			}
			m.Fields[key] = string(value)
		}
		scene := m.Fields[0]
		m.Target, m.Sender, m.ServerID = m.Fields[1], m.Fields[2], m.Fields[12]
		if scene != "0" && scene != "1" || !identifier(m.Target) || !identifier(m.Sender) ||
			!decimal(m.ServerID, true) || m.Fields[7] == "" || m.Fields[8] == "" {
			return Push{}, ErrPushBoundary
		}
		if scene == "1" {
			m.Scene = 1
		}
		out.Messages = append(out.Messages, m)
	}
	if offset != len(f.Body) {
		return Push{}, ErrPushBoundary
	}
	return out, nil
}

// Fixed schemas from the pinned NIM SDKs (desktop 2.8.3 and web-ng 10.11.5).
// These are read/receipt/
// directory/system notifications, never business actions or global cursors.
var desktopNoticeLayouts = map[byte]string{
	6: "A", 8: "A", 12: "AL", 13: "PL", 14: "STL", 15: "A", 16: "A",
	18: "A", 19: "ALB", 20: "TL", 21: "A", 23: "LZA", 24: "A", 101: "A",
	// v2FriendOperationListOnline: one complete Property, retained only.
	// Its timestamps belong to friend notifications, never our sync cursor.
	31: "P",
}

func validNoticeLayout(body []byte, layout string) bool {
	offset := 0
	for _, kind := range layout {
		var err error
		switch kind {
		case 'A':
			offset, err = readPropertyArray(body, offset)
		case 'P':
			_, offset, err = ReadProperties(body, offset)
		case 'L':
			offset += 8
		case 'B', 'Z':
			if offset >= len(body) || kind == 'Z' && body[offset] > 1 {
				return false
			}
			offset++
		case 'S', 'T':
			var count uint64
			count, offset, err = ReadVarint(body, offset, 32)
			if err != nil || count > 1024 {
				return false
			}
			for range count {
				if kind == 'S' {
					var size uint64
					size, offset, err = ReadVarint(body, offset, 32)
					if err != nil || size > 128 {
						return false
					}
					offset += int(size)
				} else {
					offset += 8
				}
				offset += 8
				if offset > len(body) {
					return false
				}
			}
		default:
			return false
		}
		if err != nil || offset > len(body) {
			return false
		}
	}
	return offset == len(body)
}

func readPropertyArray(body []byte, offset int) (int, error) {
	count, offset, err := ReadVarint(body, offset, 32)
	if err != nil || count > 1024 {
		return offset, ErrPushBoundary
	}
	for range count {
		_, offset, err = ReadProperties(body, offset)
		if err != nil {
			return offset, ErrPushBoundary
		}
	}
	return offset, nil
}

func identifier(value string) bool {
	if len(value) == 0 || len(value) > 128 {
		return false
	}
	for _, b := range []byte(value) {
		if b < 33 || b > 126 {
			return false
		}
	}
	return true
}

func decimal(value string, positive bool) bool {
	n, err := strconv.ParseUint(value, 10, 64)
	return err == nil && strconv.FormatUint(n, 10) == value && (!positive || n > 0)
}

func ACKBody(scene int, serverID string) ([]byte, error) {
	if scene != 0 && scene != 1 || !decimal(serverID, true) {
		return nil, ErrProtocol
	}
	body := []byte{7, 2, 1}
	if scene == 1 {
		body[0], body[1] = 8, 3
	}
	return binary.LittleEndian.AppendUint64(body, mustUint(serverID)), nil
}

func mustUint(value string) uint64 {
	n, _ := strconv.ParseUint(value, 10, 64)
	return n
}

func ValidateIMLogin(body []byte) error {
	fields, offset, err := ReadProperties(body, 0)
	if err != nil || len(fields[102]) == 0 {
		return ErrProtocol
	}
	if offset < len(body) {
		var count uint64
		count, offset, err = ReadVarint(body, offset, 32)
		if err != nil || count > 128 {
			return ErrProtocol
		}
		for range count {
			_, offset, err = ReadProperties(body, offset)
			if err != nil {
				return ErrProtocol
			}
		}
		if offset < len(body) {
			_, offset, err = ReadProperties(body, offset)
		}
	}
	if err != nil || offset != len(body) {
		return ErrProtocol
	}
	return nil
}
