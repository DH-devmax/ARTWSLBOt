// Package nativewire ports bounded codecs from the pinned AstrBot adapter.
// SPDX-License-Identifier: AGPL-3.0-or-later
package nativewire

import (
	"encoding/base64"
	"encoding/binary"
	"errors"
)

const Limit = 1024 * 1024

var ErrProtocol = errors.New("invalid_native_frame")

func Varint(value uint64) []byte {
	out := make([]byte, 0, 10)
	for value >= 128 {
		out = append(out, byte(value&127)|128)
		value >>= 7
	}
	return append(out, byte(value))
}

func ReadVarint(data []byte, offset, bits int) (uint64, int, error) {
	if offset < 0 || offset > len(data) || (bits != 32 && bits != 64) {
		return 0, offset, ErrProtocol
	}
	var value uint64
	for shift := 0; shift < bits; shift += 7 {
		if offset >= len(data) {
			return 0, offset, ErrProtocol
		}
		b := data[offset]
		offset++
		if (bits == 32 && shift == 28 && b&127 > 15) || (bits == 64 && shift == 63 && b&127 > 1) {
			return 0, offset, ErrProtocol
		}
		value |= uint64(b&127) << shift
		if b < 128 {
			return value, offset, nil
		}
	}
	return 0, offset, ErrProtocol
}

func Metadata(fields []uint64) ([]byte, error) {
	if len(fields) != 14 {
		return nil, ErrProtocol
	}
	out := []byte{}
	for i, value := range fields {
		n := i + 1
		bounded := n == 2 || n == 3 || n == 4 || n == 5 || n == 7 || n == 12 || n == 13
		if bounded && value >= 1<<32 {
			return nil, ErrProtocol
		}
		if (n == 2 || n == 3 || n == 4 || n == 5 || n == 7 || n == 12) && value >= 1<<31 {
			value = uint64(int64(int32(value)))
		}
		if value != 0 {
			out = append(out, Varint(uint64(n<<3))...)
			out = append(out, Varint(value)...)
		}
	}
	return out, nil
}

type Property struct {
	Key   uint32
	Value []byte
}

func Properties(properties []Property) ([]byte, error) {
	if len(properties) > 128 {
		return nil, ErrProtocol
	}
	out := Varint(uint64(len(properties)))
	for _, p := range properties {
		if len(p.Value) > Limit {
			return nil, ErrProtocol
		}
		out = append(out, Varint(uint64(p.Key))...)
		out = append(out, Varint(uint64(len(p.Value)))...)
		out = append(out, p.Value...)
		if len(out) > Limit {
			return nil, ErrProtocol
		}
	}
	return out, nil
}

func ReadProperties(data []byte, offset int) (map[uint32][]byte, int, error) {
	if len(data) > Limit {
		return nil, offset, ErrProtocol
	}
	count, next, err := ReadVarint(data, offset, 32)
	if err != nil || count > 128 {
		return nil, offset, ErrProtocol
	}
	offset = next
	fields := make(map[uint32][]byte)
	for range count {
		key, next, err := ReadVarint(data, offset, 32)
		if err != nil {
			return nil, offset, err
		}
		offset = next
		size, next, err := ReadVarint(data, offset, 32)
		if err != nil {
			return nil, offset, err
		}
		offset = next
		if _, exists := fields[uint32(key)]; exists || size > uint64(len(data)-offset) {
			return nil, offset, ErrProtocol
		}
		fields[uint32(key)] = append([]byte(nil), data[offset:offset+int(size)]...)
		offset += int(size)
	}
	return fields, offset, nil
}

type Frame struct {
	Service, Command byte
	Serial           uint16
	Code             uint16
	Body             []byte
}

func Packet(service, command byte, serial uint16, body []byte) ([]byte, error) {
	if len(body) > Limit-6 {
		return nil, ErrProtocol
	}
	out := []byte{0, service, command, byte(serial), byte(serial >> 8), 0}
	return append(out, body...), nil
}

func ReadPacket(data []byte) (Frame, error) {
	if len(data) > Limit {
		return Frame{}, ErrProtocol
	}
	_, offset, err := ReadVarint(data, 0, 32)
	if err != nil || len(data)-offset < 5 {
		return Frame{}, ErrProtocol
	}
	frame := Frame{Service: data[offset], Command: data[offset+1], Serial: binary.LittleEndian.Uint16(data[offset+2:]), Code: 200}
	tag := data[offset+4]
	offset += 5
	if tag&2 != 0 {
		if len(data)-offset < 2 {
			return Frame{}, ErrProtocol
		}
		frame.Code = binary.LittleEndian.Uint16(data[offset:])
		offset += 2
	}
	frame.Body = append([]byte(nil), data[offset:]...)
	return frame, nil
}

func Base64URL(value string) ([]byte, error) {
	if len(value) > (Limit+2)/3*4 {
		return nil, ErrProtocol
	}
	data, err := base64.RawURLEncoding.Strict().DecodeString(value)
	if err != nil || len(data) > Limit || base64.RawURLEncoding.EncodeToString(data) != value {
		return nil, ErrProtocol
	}
	return data, nil
}

// SerialAllocator rotates before exhaustion; it never wraps into an in-flight ID.
type SerialAllocator struct{ next uint16 }

func (s *SerialAllocator) Next() (uint16, error) {
	if s.next >= 60000 {
		return 0, errors.New("native_connection_rotation_required")
	}
	s.next++
	return s.next, nil
}
