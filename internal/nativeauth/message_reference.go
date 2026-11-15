package nativeauth

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"io"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"wangshangliao/internal/nativewire"
)

// MessageReference is resolved from authenticated retained input by control.
// It carries no body, credentials or model-supplied route.
type MessageReference struct {
	Platform     string                `json:"platform"`
	EventID      int64                 `json:"event_id"`
	Group        string                `json:"group"`
	Sender       string                `json:"sender"`
	SourceDigest string                `json:"source_digest"`
	CreatedAt    int64                 `json:"created_at"`
	Route        nativewire.ReplyRoute `json:"route"`
	Conversation string                `json:"conversation,omitempty"`
	Sequence     int64                 `json:"sequence,omitempty"`
}

func ReadMessageReference(text, platform, group string) (MessageReference, error) {
	var r MessageReference
	d := json.NewDecoder(strings.NewReader(text))
	d.DisallowUnknownFields()
	if len(text) > 2048 || d.Decode(&r) != nil || d.Decode(new(any)) != io.EOF {
		return r, rejection("native_message_reference_invalid")
	}
	hash, err := hex.DecodeString(r.SourceDigest)
	if err != nil || len(hash) != 32 || r.Platform != platform || r.Group != group ||
		r.EventID < 1 || r.Sender == "" || r.CreatedAt <= 0 {
		return r, rejection("native_message_reference_invalid")
	}
	if platform == "wsl" {
		if !settingID(r.Group) || !settingID(r.Sender) || r.Conversation != "" || r.Sequence != 0 {
			return r, rejection("native_message_reference_invalid")
		}
		body, err := nativewire.GroupRecallRequest(r.Route.Target, r.Route)
		clear(body)
		if err != nil {
			return r, rejection("native_message_reference_invalid")
		}
	} else if platform != "lajiaoliao" || r.Conversation != "sg_"+group || r.Sequence < 1 ||
		r.Route != (nativewire.ReplyRoute{}) {
		return r, rejection("native_message_reference_invalid")
	}
	return r, nil
}

func ValidMessageReference(text, platform, group string) bool {
	_, err := ReadMessageReference(text, platform, group)
	return err == nil
}

// The first adapter deliberately accepts recent messages only. A platform
// rejection remains a rejection, never an invitation to retry another route.
func (r MessageReference) Fresh(now time.Time) bool {
	at := time.UnixMilli(r.CreatedAt)
	return !at.After(now) && now.Sub(at) < 2*time.Minute
}

func validAssistantText(text string) bool {
	return utf8.ValidString(text) && len(text) <= 8000 && utf8.RuneCountInString(text) <= 2000 &&
		len(bytes.TrimSpace([]byte(text))) > 0 &&
		strings.IndexFunc(text, func(r rune) bool { return unicode.IsControl(r) && r != '\n' && r != '\t' }) < 0
}
