package nativewire

import "google.golang.org/protobuf/encoding/protowire"

// Notice content is display-only. Verification distinguishes application AEAD
// from an event delivered by the account's native transport.
type MessageNotice struct {
	Kind         string `json:"kind"`
	Text         string `json:"text,omitempty"`
	Verification string `json:"verification"`
}

func noticeFormat(format uint64) bool {
	switch format {
	case 7, 8, 10, 12, 14, 28, 31, 39:
		return true
	}
	return false
}

func parseMessageNotice(f map[protowire.Number][]protoValue, format uint64) (*MessageNotice, error) {
	out := &MessageNotice{}
	field := protowire.Number(100)
	textField := protowire.Number(1)
	switch format {
	case 7:
		out.Kind, field, textField = "group_notification", 106, 5
	case 8:
		out.Kind, field, textField = "group_announcement", 107, 2
	case 14:
		// Display the event class, never financial fields or executable links.
		out.Kind, field, textField = "red_envelope_tip", 201, 0
	case 10:
		out.Kind, field, textField = "red_envelope", 201, 0
	case 12:
		out.Kind = "extension_notification"
	case 28:
		out.Kind = "auto_reply_tip"
	case 31:
		out.Kind = "tip"
	case 39:
		out.Kind, field, textField = "game", 112, 0
	default:
		return nil, ErrProtocol
	}
	if len(f[field]) != 1 {
		return nil, ErrProtocol
	}
	for n, values := range f {
		if len(values) > 0 && n != field && (n >= 100 && n <= 112 ||
			n >= 200 && n <= 299 || n >= 314 && n <= 317 || n >= 2140 && n <= 2147) {
			return nil, ErrProtocol
		}
	}
	fields, err := nested(f, field)
	if err != nil {
		return nil, err
	}
	if textField != 0 {
		out.Text, err = fieldText(fields, textField, 4096)
	}
	return out, err
}

// ReadUnencryptedGroupNotice implements only the fixed four-field, aad=0
// notification form. It deliberately cannot return an ApplicationMessage.
// OpenMessage still rejects it, so quotes, moderation and sends cannot treat
// this legacy display record as authenticated application identity or text.
func ReadUnencryptedGroupNotice(text string) (uint64, *MessageNotice, error) {
	encoded, err := messageEncoded(text)
	if err != nil {
		return 0, nil, err
	}
	envelope, err := messageFields(encoded)
	if err != nil || len(envelope) != 4 {
		return 0, nil, ErrProtocol
	}
	for _, n := range []protowire.Number{1, 2, 3} {
		if len(envelope[n]) != 1 || envelope[n][0].t != protowire.Fixed64Type {
			return 0, nil, ErrProtocol
		}
	}
	body, err := fieldBytes(envelope, 4)
	if err != nil || len(envelope[4]) != 1 {
		return 0, nil, ErrProtocol
	}
	fields, err := messageFields(body)
	if err != nil {
		return 0, nil, err
	}
	sender, e1 := source(fields, 1)
	target, e2 := source(fields, 2)
	session, e3 := number(fields, 5)
	device, e4 := number(fields, 4)
	format, e5 := number(fields, 11)
	if e1 != nil || e2 != nil || e3 != nil || e4 != nil || e5 != nil ||
		(session != 2 && session != 3) || device > 3 || format != 7 && format != 10 && format != 12 ||
		envelope[1][0].n != sender.ID<<34|target.ID<<4|session<<2|device {
		return 0, nil, ErrProtocol
	}
	notice, err := parseMessageNotice(fields, format)
	if err != nil {
		return 0, nil, err
	}
	notice.Verification = "transport"
	return target.ID, notice, nil
}
