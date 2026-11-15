// SPDX-License-Identifier: AGPL-3.0-or-later
package nativewire

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/pierrec/lz4/v4"
	"google.golang.org/protobuf/encoding/protowire"
)

// DecodeError deliberately contains no payload, identity, key or upstream text.
type DecodeError struct{ Stage, Shape string }

func (e *DecodeError) Error() string { return "message_decode_" + e.Stage }

type MessageSource struct {
	ID     uint64 `json:"id"`
	Name   string `json:"name"`
	Avatar string `json:"avatar,omitempty"`
}
type MessageMention struct {
	Start uint64 `json:"start"`
	End   uint64 `json:"end"`
	UID   uint64 `json:"uid"`
	Name  string `json:"name"`
}
type MessageQuote struct {
	ClientID string `json:"client_id"`
	ServerID string `json:"server_id"`
	Text     string `json:"text"`
}
type ApplicationMessage struct {
	Sender     MessageSource      `json:"sender"`
	Target     MessageSource      `json:"target"`
	CreatedAt  uint64             `json:"created_at"`
	Session    uint64             `json:"session"`
	Format     uint64             `json:"format"`
	ClientID   string             `json:"client_id"`
	Text       string             `json:"text"`
	Mentions   []MessageMention   `json:"mentions"`
	Quote      *MessageQuote      `json:"quote,omitempty"`
	Image      *MessageImage      `json:"image,omitempty"`
	Video      *MessageVideo      `json:"video,omitempty"`
	Voice      *MessageVoice      `json:"voice,omitempty"`
	File       *MessageFile       `json:"file,omitempty"`
	Forward    *MessageForward    `json:"forward,omitempty"`
	FriendCard *MessageFriendCard `json:"friend_card,omitempty"`
	Notice     *MessageNotice     `json:"notice,omitempty"`
}

type protoValue struct {
	n uint64
	b []byte
	t protowire.Type
}

// No recursive generic protobuf traversal: nested schemas below have fixed depth.
func messageFields(data []byte) (map[protowire.Number][]protoValue, error) {
	if len(data) > Limit {
		return nil, ErrProtocol
	}
	count := 0
	for rest := data; len(rest) > 0; count++ {
		num, typ, n := protowire.ConsumeTag(rest)
		if n < 0 || num < 1 || count >= 1024 ||
			typ != protowire.VarintType && typ != protowire.Fixed64Type &&
				typ != protowire.Fixed32Type && typ != protowire.BytesType {
			return nil, ErrProtocol
		}
		size := protowire.ConsumeFieldValue(num, typ, rest[n:])
		if size < 0 {
			return nil, ErrProtocol
		}
		rest = rest[n+size:]
	}
	// Unique fields share one value allocation; repeated fields keep independent
	// slices so appending a mention cannot overwrite an adjacent schema field.
	values := make([]protoValue, count)
	out := make(map[protowire.Number][]protoValue, min(count, 32))
	for count := 0; len(data) > 0; count++ {
		if count >= 1024 {
			return nil, ErrProtocol
		}
		num, typ, n := protowire.ConsumeTag(data)
		if n < 0 || num < 1 {
			return nil, ErrProtocol
		}
		data = data[n:]
		v := protoValue{t: typ}
		switch typ {
		case protowire.VarintType:
			v.n, n = protowire.ConsumeVarint(data)
		case protowire.Fixed64Type:
			v.n, n = protowire.ConsumeFixed64(data)
		case protowire.Fixed32Type:
			var value uint32
			value, n = protowire.ConsumeFixed32(data)
			v.n = uint64(value)
		case protowire.BytesType:
			v.b, n = protowire.ConsumeBytes(data)
		default:
			return nil, ErrProtocol
		}
		if n < 0 {
			return nil, ErrProtocol
		}
		if previous := out[num]; len(previous) > 0 {
			out[num] = append(previous, v)
		} else {
			values[count] = v
			out[num] = values[count : count+1 : count+1]
		}
		data = data[n:]
	}
	return out, nil
}

func one(f map[protowire.Number][]protoValue, n protowire.Number, types ...protowire.Type) (protoValue, error) {
	v := f[n]
	if len(v) == 0 {
		return protoValue{}, nil
	}
	if len(v) != 1 {
		return protoValue{}, ErrProtocol
	}
	for _, t := range types {
		if v[0].t == t {
			return v[0], nil
		}
	}
	return protoValue{}, ErrProtocol
}

func number(f map[protowire.Number][]protoValue, n protowire.Number) (uint64, error) {
	v, err := one(f, n, protowire.VarintType)
	return v.n, err
}
func fieldBytes(f map[protowire.Number][]protoValue, n protowire.Number) ([]byte, error) {
	v, err := one(f, n, protowire.BytesType)
	return v.b, err
}
func fieldText(f map[protowire.Number][]protoValue, n protowire.Number, limit int) (string, error) {
	b, err := fieldBytes(f, n)
	if err != nil || len(b) > limit || !utf8.Valid(b) {
		return "", ErrProtocol
	}
	return string(b), nil
}
func nested(f map[protowire.Number][]protoValue, n protowire.Number) (map[protowire.Number][]protoValue, error) {
	b, err := fieldBytes(f, n)
	if err != nil {
		return nil, err
	}
	return messageFields(b)
}

func messageEncoded(text string) ([]byte, error) {
	if len(text) == 0 || len(text) > Limit*2 {
		return nil, ErrProtocol
	}
	if strings.HasPrefix(text, "{") {
		var wrapper map[string]json.RawMessage
		if json.Unmarshal([]byte(text), &wrapper) != nil || len(wrapper) != 1 ||
			json.Unmarshal(wrapper["b"], &text) != nil {
			return nil, ErrProtocol
		}
	}
	// Match the original adapter's canonical unpadded Base64URL.
	b, err := base64.RawURLEncoding.Strict().DecodeString(text)
	if err != nil || len(b) > Limit+1024 || strings.ContainsAny(text, "\r\n") {
		return nil, ErrProtocol
	}
	return b, nil
}

func source(f map[protowire.Number][]protoValue, n protowire.Number) (MessageSource, error) {
	s, err := nested(f, n)
	if err != nil {
		return MessageSource{}, err
	}
	id, e1 := number(s, 1)
	name, e2 := fieldText(s, 2, 1024)
	avatar, e3 := fieldText(s, 3, 1024)
	if e1 != nil || e2 != nil || e3 != nil || id == 0 || id >= 1<<30 {
		return MessageSource{}, ErrProtocol
	}
	return MessageSource{ID: id, Name: name, Avatar: avatar}, nil
}
func content(f map[protowire.Number][]protoValue, n protowire.Number) (string, error) {
	c, err := nested(f, n)
	if err != nil {
		return "", err
	}
	return fieldText(c, 1, Limit)
}

// OpenMessage ports AstrBot a672ef9 wire.open_message, including the original
// eight-byte-nonce AEAD and LZ4 block length prefix. It never trusts MessageSign
// metadata as a sender or a permission source.
func OpenMessage(key []byte, text string) (out ApplicationMessage, err error) {
	stage := "encoding"
	shape := ""
	defer func() {
		if err != nil {
			out, err = ApplicationMessage{}, &DecodeError{Stage: stage, Shape: shape}
		}
	}()
	if len(key) != 32 {
		return out, ErrProtocol
	}
	encoded, err := messageEncoded(text)
	if err != nil {
		return out, err
	}
	stage = "envelope"
	f, err := messageFields(encoded)
	if err != nil {
		return out, err
	}
	tag, _ := fieldBytes(f, 7)
	if len(tag) != 16 {
		if header, e := one(f, 1, protowire.VarintType, protowire.Fixed64Type); e == nil && header.t != protowire.BytesType && len(f[1]) == 1 {
			stage = "envelope_authentication_missing"
			return out, ErrProtocol
		}
		stage = "packet_signature"
		packet := f
		msg, e1 := fieldText(packet, 1, Limit*2)
		sig, e2 := fieldText(packet, 2, 100000)
		version, e3 := number(packet, 3)
		if e1 != nil || e2 != nil || e3 != nil || version != 1 {
			return out, ErrProtocol
		}
		signature, e := messageEncoded(sig)
		if e != nil || len(signature) <= 24 || len(signature) > 65536 {
			return out, ErrProtocol
		}
		aad := binary.LittleEndian.AppendUint64(nil, version)
		sealed := append(append([]byte{}, signature[24:]...), signature[8:24]...)
		meta, e := originalOpen(key, signature[:8], sealed, aad)
		if e != nil {
			return out, e
		}
		_, e = messageFields(meta)
		clear(meta)
		if e != nil {
			return out, e
		}
		stage = "packet_message"
		encoded, err = messageEncoded(msg)
		if err != nil {
			return out, err
		}
		f, err = messageFields(encoded)
		if err != nil {
			return out, err
		}
	}
	stage = "authentication"
	header, e1 := one(f, 1, protowire.VarintType, protowire.Fixed64Type)
	nonce, e2 := one(f, 3, protowire.VarintType, protowire.Fixed64Type)
	aad, e3 := one(f, 6, protowire.VarintType, protowire.Fixed64Type)
	body, e4 := fieldBytes(f, 4)
	tag, e5 := fieldBytes(f, 7)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil || len(tag) != 16 {
		return out, ErrProtocol
	}
	compressed, err := originalOpen(key, binary.LittleEndian.AppendUint64(nil, nonce.n),
		append(append([]byte{}, body...), tag...), binary.LittleEndian.AppendUint64(nil, aad.n))
	if err != nil {
		return out, err
	}
	defer clear(compressed)
	stage = "compression"
	plain := compressed
	if aad.n != 0 {
		if len(compressed) < 4 {
			return out, ErrProtocol
		}
		size := binary.LittleEndian.Uint32(compressed)
		if size == 0 || size > Limit {
			return out, ErrProtocol
		}
		plain = make([]byte, int(size))
		defer clear(plain)
		n, e := lz4.UncompressBlock(compressed[4:], plain)
		if e != nil || n != len(plain) {
			return out, ErrProtocol
		}
	} else if len(plain) > Limit {
		return out, ErrProtocol
	}
	stage = "schema"
	f, err = messageFields(plain)
	if err != nil {
		return out, err
	}
	stage = "schema_sender"
	out.Sender, err = source(f, 1)
	if err != nil {
		return out, err
	}
	stage = "schema_target"
	out.Target, err = source(f, 2)
	if err != nil {
		return out, err
	}
	stage = "schema_fields"
	var device uint64
	values := []struct {
		n   protowire.Number
		dst *uint64
	}{
		{4, &device}, {5, &out.Session}, {11, &out.Format},
	}
	// Official desktop 2.8.3 uses google.protobuf.Timestamp; historical
	// adapter messages used an int64. Both are authenticated before parsing.
	if stamp, e := one(f, 3, protowire.VarintType, protowire.BytesType); e != nil {
		return out, e
	} else if stamp.t == protowire.BytesType {
		ts, e := messageFields(stamp.b)
		if e != nil {
			return out, e
		}
		seconds, e1 := number(ts, 1)
		nanos, e2 := number(ts, 2)
		if e1 != nil || e2 != nil || seconds > 253402300799 || nanos >= 1000000000 {
			return out, ErrProtocol
		}
		out.CreatedAt = seconds*1000 + nanos/1000000
	} else {
		out.CreatedAt = stamp.n
	}
	for _, v := range values {
		*v.dst, err = number(f, v.n)
		if err != nil {
			for _, field := range f[v.n] {
				shape += fmt.Sprintf("f%dw%d", v.n, field.t)
			}
			return out, err
		}
	}
	stage = "schema_client"
	out.ClientID, err = fieldText(f, 50, 256)
	if err != nil {
		return out, err
	}
	// Non-text formats keep authenticated identity and format, not guessed text.
	if out.Format == 0 {
		stage = "schema_content"
		out.Text, err = content(f, 100)
		if err != nil {
			return out, err
		}
		if len(f[105]) > 0 {
			stage = "schema_mentions"
			m, e := nested(f, 105)
			if e != nil || len(m[2]) > 256 {
				return out, ErrProtocol
			}
			out.Text, err = content(m, 1)
			if err != nil {
				return out, err
			}
			for _, v := range m[2] {
				if v.t != protowire.BytesType {
					return out, ErrProtocol
				}
				p, e := messageFields(v.b)
				if e != nil {
					return out, e
				}
				var mention MessageMention
				mention.Start, e1 = number(p, 1)
				mention.End, e2 = number(p, 2)
				mention.UID, e3 = number(p, 3)
				mention.Name, e4 = fieldText(p, 4, 1024)
				if e1 != nil || e2 != nil || e3 != nil || e4 != nil || mention.Start > mention.End ||
					mention.End > uint64(len(out.Text)*2) {
					return out, ErrProtocol
				}
				out.Mentions = append(out.Mentions, mention)
			}
		}
		if len(f[301]) > 0 {
			stage = "schema_quote"
			q, e := nested(f, 301)
			if e != nil {
				return out, e
			}
			quote := &MessageQuote{}
			quote.ClientID, e1 = fieldText(q, 1, 256)
			quote.ServerID, e2 = fieldText(q, 2, 128)
			quote.Text, e = content(q, 100)
			if e1 != nil || e2 != nil || e != nil {
				return out, ErrProtocol
			}
			out.Quote = quote
		}
	}
	if out.Format == 1 {
		stage = "schema_image"
		out.Image, err = parseMessageImage(f, 101)
		if err != nil || len(f[100])+len(f[105]) > 0 {
			return out, ErrProtocol
		}
	}
	if out.Format == 9 {
		stage = "schema_forward"
		out.Forward, err = parseMessageForward(f)
		if err != nil || len(f[100])+len(f[101])+len(f[105]) > 0 {
			return out, ErrProtocol
		}
	}
	if out.Format == 3 {
		stage = "schema_video"
		out.Video, err = parseMessageVideo(f)
		if err != nil {
			return out, ErrProtocol
		}
		for field, values := range f {
			if field != 103 && (field >= 100 && field <= 112 || field == 317) && len(values) > 0 {
				return out, ErrProtocol
			}
		}
	}
	if out.Format == 13 {
		stage = "schema_friend_card"
		out.FriendCard, err = parseMessageFriendCard(f)
		if err != nil {
			return out, ErrProtocol
		}
		for field, values := range f {
			if field != 108 && (field >= 100 && field <= 112 || field == 317) && len(values) > 0 {
				return out, ErrProtocol
			}
		}
	}
	if out.Format == 2 || out.Format == 6 {
		field := protowire.Number(102)
		stage = "schema_voice"
		if out.Format == 6 {
			field, stage = 104, "schema_file"
		}
		for n, values := range f {
			if n != field && (n >= 100 && n <= 112 || n == 317) && len(values) > 0 {
				return out, ErrProtocol
			}
		}
		fields, e := nested(f, field)
		asset, ae := nested(fields, 1)
		id, ie := fieldText(asset, 1, 20)
		if e != nil || ae != nil || ie != nil || len(f[field]) != 1 || !ValidAssetID(id) {
			return out, ErrProtocol
		}
		if out.Format == 2 {
			duration, e := number(fields, 2)
			if e != nil || duration >= 1<<31 {
				return out, ErrProtocol
			}
			out.Voice = &MessageVoice{Asset: id, Duration: duration}
		} else {
			filename, e := fieldText(fields, 2, 240)
			size, se := number(fields, 3)
			if e != nil || se != nil || !ValidFilename(filename) || size >= 1<<32 {
				return out, ErrProtocol
			}
			out.File = &MessageFile{Asset: id, Filename: filename, Size: size}
		}
	}
	if noticeFormat(out.Format) {
		stage = "schema_notice"
		out.Notice, err = parseMessageNotice(f, out.Format)
		if err != nil {
			return out, err
		}
		out.Notice.Verification = "authenticated"
	}
	stage = "routing"
	expected := out.Sender.ID<<34 | out.Target.ID<<4 | min(out.Session, 3)<<2 | device
	if device > 3 || out.Session > 1<<31-1 || header.n != expected {
		return out, fmt.Errorf("%w", ErrProtocol)
	}
	return out, nil
}
