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
	count, offset := uint64(1), 0
	switch {
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
