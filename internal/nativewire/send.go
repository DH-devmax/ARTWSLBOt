// SPDX-License-Identifier: AGPL-3.0-or-later
package nativewire

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/pierrec/lz4/v4"
	"google.golang.org/protobuf/encoding/protowire"
)

// GroupText is an application payload, not an authorization or transport route.
// Callers must resolve mentions and quotes from current, scoped server evidence.
type GroupText struct {
	SenderID, GroupID uint64
	ClientID          string
	Text              string
	CreatedAt         time.Time
	Mentions          []MessageMention
	Quote             *MessageQuote
	Media             *OutboundMedia
}

// PrivateText has an explicit business peer, not a native group or NIM route.
// Group-only mentions and quotes are deliberately absent from this contract.
type PrivateText struct {
	SenderID, PeerID uint64
	ClientID, Text   string
	CreatedAt        time.Time
	Media            *OutboundMedia
}

func (m PrivateText) payload() GroupText {
	return GroupText{SenderID: m.SenderID, GroupID: m.PeerID, ClientID: m.ClientID, Text: m.Text, CreatedAt: m.CreatedAt, Media: m.Media}
}

// ReplyRoute is authenticated transport metadata for the exact quoted message.
// It is never accepted from a browser or a model.
type ReplyRoute struct {
	Sender, Target, TimeMS, ServerID, ClientID string
}

func messageNumber(n protowire.Number, v uint64) []byte {
	return protowire.AppendVarint(protowire.AppendTag(nil, n, protowire.VarintType), v)
}

func messageBytes(n protowire.Number, v []byte) []byte {
	return protowire.AppendBytes(protowire.AppendTag(nil, n, protowire.BytesType), v)
}

func clientMessageID(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i, b := range []byte(id) {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if b != '-' {
				return false
			}
		} else if !strings.ContainsRune("0123456789abcdef", rune(b)) {
			return false
		}
	}
	return true
}

// EncodeGroupText uses desktop 2.8.3 Timestamp (not the historical int64
// timestamp), and the reviewed adapter's session=2/device=1/version=2 contract.
func EncodeGroupText(m GroupText) ([]byte, error) {
	return encodeText(m, 2)
}

// EncodePrivateText uses a distinct typed peer and private application session.
func EncodePrivateText(m PrivateText) ([]byte, error) {
	return encodeText(m.payload(), 1)
}

func encodeText(m GroupText, session uint64) ([]byte, error) {
	if m.SenderID == 0 || m.SenderID >= 1<<30 || m.GroupID == 0 || m.GroupID >= 1<<30 ||
		!clientMessageID(m.ClientID) || !utf8.ValidString(m.Text) || m.Media == nil && strings.TrimSpace(m.Text) == "" ||
		len(m.Text) > 4096 || m.CreatedAt.Unix() <= 0 || m.CreatedAt.Unix() > 253402300799 {
		return nil, ErrProtocol
	}
	plain := messageBytes(1, messageNumber(1, m.SenderID))
	plain = append(plain, messageBytes(2, messageNumber(1, m.GroupID))...)
	stamp := messageNumber(1, uint64(m.CreatedAt.Unix()))
	if ns := m.CreatedAt.Nanosecond(); ns != 0 {
		stamp = append(stamp, messageNumber(2, uint64(ns))...)
	}
	plain = append(plain, messageBytes(3, stamp)...)
	plain = append(plain, messageNumber(4, 1)...)
	plain = append(plain, messageNumber(5, session)...)
	plain = append(plain, messageNumber(6, 2)...)
	plain = append(plain, messageBytes(50, []byte(m.ClientID))...)
	if m.Media != nil {
		if session != 2 && (session != 1 || m.Media.Kind != "image") ||
			m.Text != "" || len(m.Mentions) != 0 || m.Quote != nil {
			return nil, ErrProtocol
		}
		payload, format, field, err := m.Media.encode()
		if err != nil {
			return nil, err
		}
		plain = append(plain, messageNumber(11, format)...)
		plain = append(plain, messageBytes(field, payload)...)
	} else if len(m.Mentions) == 0 {
		plain = append(plain, messageBytes(100, messageBytes(1, []byte(m.Text)))...)
	} else {
		if session != 2 || len(m.Mentions) > 32 {
			return nil, ErrProtocol
		}
		units := utf16.Encode([]rune(m.Text))
		var end uint64
		mentions := messageBytes(1, messageBytes(1, []byte(m.Text)))
		for _, person := range m.Mentions {
			if person.UID == 0 || person.UID >= 1<<30 || person.Start < end ||
				person.Start >= person.End || person.End > uint64(len(units)) ||
				person.Name == "" || len(person.Name) > 256 || !utf8.ValidString(person.Name) {
				return nil, ErrProtocol
			}
			expected := utf16.Encode([]rune("@" + person.Name + " "))
			actual := units[person.Start:person.End]
			if len(expected) != len(actual) {
				return nil, ErrProtocol
			}
			for i := range actual {
				if actual[i] != expected[i] {
					return nil, ErrProtocol
				}
			}
			end = person.End
			item := messageNumber(1, person.Start)
			item = append(item, messageNumber(2, person.End)...)
			item = append(item, messageNumber(3, person.UID)...)
			item = append(item, messageBytes(4, []byte(person.Name))...)
			mentions = append(mentions, messageBytes(2, item)...)
		}
		plain = append(plain, messageBytes(105, mentions)...)
	}
	if q := m.Quote; q != nil {
		if session != 2 || len(q.ClientID) == 0 || len(q.ClientID) > 128 || !utf8.ValidString(q.ClientID) ||
			!decimal(q.ServerID, true) || len(q.Text) > 1024 || !utf8.ValidString(q.Text) || strings.TrimSpace(q.Text) == "" {
			return nil, ErrProtocol
		}
		if _, err := strconv.ParseUint(q.ServerID, 10, 64); err != nil {
			return nil, ErrProtocol
		}
		plain = append(plain, messageNumber(14, 1)...)
		reply := messageBytes(1, []byte(q.ClientID))
		reply = append(reply, messageBytes(2, []byte(q.ServerID))...)
		reply = append(reply, messageBytes(100, messageBytes(1, []byte(q.Text)))...)
		plain = append(plain, messageBytes(301, reply)...)
	}
	if len(plain) > 4096 {
		clear(plain)
		return nil, ErrProtocol
	}
	return plain, nil
}

// SealGroupText ports wire.seal_message, retaining original libsodium AEAD,
// eight-byte nonce and LZ4 block framing. The caller must reserve a unique nonce
// durably for this key before calling; this function never invents/retries it.
func SealGroupText(key []byte, m GroupText, nonce, aad uint64) (string, error) {
	return sealText(key, m, nonce, aad, 2)
}

func SealPrivateText(key []byte, m PrivateText, nonce, aad uint64) (string, error) {
	return sealText(key, m.payload(), nonce, aad, 1)
}

func sealText(key []byte, m GroupText, nonce, aad, session uint64) (string, error) {
	if len(key) != 32 || nonce == 0 || aad == 0 {
		return "", ErrProtocol
	}
	plain, err := encodeText(m, session)
	if err != nil {
		return "", err
	}
	defer clear(plain)
	block := make([]byte, lz4.CompressBlockBound(len(plain)))
	defer clear(block)
	n, err := lz4.CompressBlock(plain, block, nil)
	if err != nil {
		return "", ErrProtocol
	}
	if n == 0 {
		// LZ4 permits an all-literal final sequence for incompressible input.
		literal := []byte{byte(min(len(plain), 15) << 4)}
		if len(plain) >= 15 {
			left := len(plain) - 15
			for left >= 255 {
				literal = append(literal, 255)
				left -= 255
			}
			literal = append(literal, byte(left))
		}
		literal = append(literal, plain...)
		copy(block, literal)
		n = len(literal)
		clear(literal)
	}
	compressed := append(binary.LittleEndian.AppendUint32(nil, uint32(len(plain))), block[:n]...)
	defer clear(compressed)
	sealed, err := originalSeal(key, binary.LittleEndian.AppendUint64(nil, nonce),
		compressed, binary.LittleEndian.AppendUint64(nil, aad))
	if err != nil {
		return "", err
	}
	defer clear(sealed)
	header := m.SenderID<<34 | m.GroupID<<4 | session<<2 | 1
	envelope := protowire.AppendFixed64(protowire.AppendTag(nil, 1, protowire.Fixed64Type), header)
	envelope = protowire.AppendFixed64(protowire.AppendTag(envelope, 2, protowire.Fixed64Type), uint64(m.CreatedAt.Unix()))
	envelope = protowire.AppendFixed64(protowire.AppendTag(envelope, 3, protowire.Fixed64Type), nonce)
	envelope = append(envelope, messageBytes(4, sealed[:len(sealed)-16])...)
	envelope = append(envelope, messageNumber(6, aad)...)
	envelope = append(envelope, messageBytes(7, sealed[len(sealed)-16:])...)
	wrapped, _ := json.Marshal(struct {
		Body string `json:"b"`
	}{base64.RawURLEncoding.EncodeToString(envelope)})
	return string(wrapped), nil
}

// GroupTextRequest builds only the reviewed NIM group custom-message request.
func GroupTextRequest(team, clientID, attachment string) ([]byte, error) {
	return GroupTextReplyRequest(team, clientID, attachment, nil)
}

func PrivateTextRequest(peer, clientID, attachment string) ([]byte, error) {
	if peer == "" || len(peer) > 128 || !utf8.ValidString(peer) || strings.IndexFunc(peer, func(r rune) bool {
		return unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 || !clientMessageID(clientID) || len(attachment) == 0 || len(attachment) > 16384 || !utf8.ValidString(attachment) {
		return nil, ErrProtocol
	}
	return Properties([]Property{
		{Key: 0, Value: []byte("0")}, {Key: 1, Value: []byte(peer)},
		{Key: 8, Value: []byte("100")}, {Key: 10, Value: []byte(attachment)},
		{Key: 11, Value: []byte(clientID)},
	})
}

func GroupTextReplyRequest(team, clientID, attachment string, reply *ReplyRoute) ([]byte, error) {
	if !decimal(team, true) || !clientMessageID(clientID) || len(attachment) == 0 || len(attachment) > 16384 ||
		!utf8.ValidString(attachment) {
		return nil, ErrProtocol
	}
	fields := []Property{
		{Key: 0, Value: []byte("1")}, {Key: 1, Value: []byte(team)},
		{Key: 8, Value: []byte("100")}, {Key: 10, Value: []byte(attachment)},
		{Key: 11, Value: []byte(clientID)},
	}
	if reply != nil {
		if !validReplyRoute(team, *reply) {
			return nil, ErrProtocol
		}
		values := []string{reply.Sender, team, reply.TimeMS, reply.ServerID, reply.ClientID}
		for _, base := range []uint32{29, 34} {
			for index, value := range values {
				fields = append(fields, Property{Key: base + uint32(index), Value: []byte(value)})
			}
		}
	}
	return Properties(fields)
}

func validReplyRoute(team string, route ReplyRoute) bool {
	if !decimal(team, true) || route.Sender == "" || len(route.Sender) > 128 || !utf8.ValidString(route.Sender) ||
		strings.IndexFunc(route.Sender, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) }) >= 0 ||
		route.Target != team || !decimal(route.TimeMS, true) || !decimal(route.ServerID, true) ||
		route.ClientID == "" || len(route.ClientID) > 128 || !utf8.ValidString(route.ClientID) {
		return false
	}
	for _, n := range []string{route.TimeMS, route.ServerID} {
		if _, err := strconv.ParseUint(n, 10, 64); err != nil {
			return false
		}
	}
	return true
}

// GroupRecallRequest follows the reviewed (7,13) group-recall contract. The
// control layer must resolve this reference from retained authenticated input,
// recheck the recall window/roles, and persist intent before using it.
func GroupRecallRequest(team string, message ReplyRoute) ([]byte, error) {
	if !validReplyRoute(team, message) {
		return nil, ErrProtocol
	}
	return Properties([]Property{
		{Key: 0, Value: []byte(message.TimeMS)}, {Key: 1, Value: []byte("8")},
		{Key: 2, Value: []byte(team)}, {Key: 3, Value: []byte(message.Sender)},
		{Key: 10, Value: []byte(message.ClientID)}, {Key: 11, Value: []byte(message.ServerID)},
		{Key: 16, Value: []byte(message.Sender)},
	})
}

type SendReceipt struct {
	ClientID string `json:"client_id"`
	ServerID string `json:"server_id"`
	TimeMS   string `json:"time_ms"`
}

// SubmissionFence must wrap exactly one socket write. Control owns the fence;
// transport waits for the server reply only after it returns.
type SubmissionFence func(context.Context, func(context.Context) error) error

type TextSendResult struct {
	Attempted bool
	Code      uint16
	Receipt   SendReceipt
}

// ParseSendReceipt establishes server acceptance, never recipient delivery.
// Missing echo is permitted by the pinned adapter; a present echo must match.
func ParseSendReceipt(body []byte, clientID string) (SendReceipt, error) {
	if !clientMessageID(clientID) {
		return SendReceipt{}, ErrProtocol
	}
	receipt, err := parseUnmatchedSendReceipt(body)
	if err != nil || receipt.ClientID != "" && receipt.ClientID != clientID {
		return SendReceipt{}, ErrProtocol
	}
	receipt.ClientID = clientID
	return receipt, nil
}

func parseUnmatchedSendReceipt(body []byte) (SendReceipt, error) {
	fields, end, err := ReadProperties(body, 0)
	if err != nil || end != len(body) ||
		!decimal(string(fields[12]), true) || !decimal(string(fields[7]), true) {
		return SendReceipt{}, ErrProtocol
	}
	if echo, exists := fields[11]; exists && !clientMessageID(string(echo)) {
		return SendReceipt{}, ErrProtocol
	}
	return SendReceipt{ClientID: string(fields[11]), ServerID: string(fields[12]), TimeMS: string(fields[7])}, nil
}
