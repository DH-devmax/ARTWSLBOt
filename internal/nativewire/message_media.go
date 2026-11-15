// SPDX-License-Identifier: AGPL-3.0-or-later
package nativewire

import (
	"strconv"

	"google.golang.org/protobuf/encoding/protowire"
)

const MaxImageBytes = 5 * 1024 * 1024
const MaxImagePixels = 16_000_000
const MaxForwardEntries = 100
const MaxVideoBytes = 16 * 1024 * 1024

type MessageImage struct {
	Asset          string `json:"asset"`
	Width          uint64 `json:"width"`
	Height         uint64 `json:"height"`
	Size           uint64 `json:"size"`
	Sticker        bool   `json:"sticker"`
	PreviewLimited bool   `json:"preview_limited,omitempty"`
}

type MessageForward struct {
	Title     string                `json:"title"`
	Entries   []MessageForwardEntry `json:"entries"`
	Total     int                   `json:"total"`
	Truncated bool                  `json:"truncated"`
}

type MessageForwardEntry struct {
	Sender    MessageSource `json:"sender"`
	CreatedAt uint64        `json:"created_at"`
	Format    uint64        `json:"format"`
	Text      string        `json:"text"`
	Image     *MessageImage `json:"image,omitempty"`
	Video     *MessageVideo `json:"video,omitempty"`
	Reason    string        `json:"reason,omitempty"`
}

type MessageVideo struct {
	Asset      string `json:"asset,omitempty"`
	Screenshot string `json:"screenshot,omitempty"`
	External   bool   `json:"external"`
	Duration   uint64 `json:"duration"`
	Width      uint64 `json:"width"`
	Height     uint64 `json:"height"`
	Size       uint64 `json:"size"`
}

type MessageVoice struct {
	Asset    string `json:"asset"`
	Duration uint64 `json:"duration"`
}

type MessageFile struct {
	Asset    string `json:"asset"`
	Filename string `json:"filename"`
	Size     uint64 `json:"size"`
}

type MessageFriendCard struct {
	UserID    uint64 `json:"user_id"`
	Avatar    string `json:"avatar,omitempty"`
	Nickname  string `json:"nickname"`
	AccountID uint64 `json:"account_id"`
	VIPLevel  uint64 `json:"vip_level"`
	IsPretty  bool   `json:"is_pretty"`
	SharedAt  uint64 `json:"shared_at"`
}

func ValidAssetID(value string) bool {
	n, err := strconv.ParseUint(value, 10, 64)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == value
}

// SupportedContent never equates authenticated but unknown formats with text.
func (m ApplicationMessage) SupportedContent() bool {
	return m.Format == 0 || m.Format == 1 && m.Image != nil || m.Format == 9 && m.Forward != nil ||
		m.Format == 13 && m.FriendCard != nil || m.Format == 3 && m.Video != nil ||
		m.Format == 2 && m.Voice != nil || m.Format == 6 && m.File != nil ||
		noticeFormat(m.Format) && m.Notice != nil
}

// Desktop Message.video (103) and forward-entry video (103) share this schema.
// External URLs are represented only by a flag; they never become fetch targets.
func parseMessageVideo(f map[protowire.Number][]protoValue) (*MessageVideo, error) {
	if len(f[103]) != 1 {
		return nil, ErrProtocol
	}
	video, err := nested(f, 103)
	if err != nil {
		return nil, err
	}
	out := &MessageVideo{}
	for _, item := range []struct {
		field protowire.Number
		dst   *string
	}{{1, &out.Asset}, {2, &out.Screenshot}} {
		if len(video[item.field]) == 0 {
			continue
		}
		if item.field == 2 && len(video[2]) == 1 && video[2][0].t == protowire.BytesType && len(video[2][0].b) == 0 {
			continue // Desktop emits an empty screenshot asset for uploaded video.
		}
		asset, err := nested(video, item.field)
		if err != nil {
			return nil, err
		}
		*item.dst, err = fieldText(asset, 1, 20)
		if err != nil || !ValidAssetID(*item.dst) {
			return nil, ErrProtocol
		}
	}
	external, err := fieldText(video, 3, 2048)
	if err != nil {
		return nil, err
	}
	out.External = external != ""
	if out.Asset == "" && !out.External {
		return nil, ErrProtocol
	}
	for _, item := range []struct {
		field protowire.Number
		dst   *uint64
	}{{4, &out.Duration}, {5, &out.Width}, {6, &out.Height}, {7, &out.Size}} {
		*item.dst, err = number(video, item.field)
		if err != nil || *item.dst >= 1<<31 {
			return nil, ErrProtocol
		}
	}
	if out.Width > 16384 || out.Height > 16384 {
		return nil, ErrProtocol
	}
	return out, nil
}

// Desktop 2.8.3 Message.friendCard is field 108. A card identifies its subject,
// not the authenticated sender and never a grant to read that user's profile.
func parseMessageFriendCard(f map[protowire.Number][]protoValue) (*MessageFriendCard, error) {
	if len(f[108]) != 1 {
		return nil, ErrProtocol
	}
	fields, err := nested(f, 108)
	if err != nil {
		return nil, err
	}
	card := &MessageFriendCard{}
	card.UserID, err = number(fields, 1)
	if err != nil || card.UserID == 0 || card.UserID >= 1<<30 {
		return nil, ErrProtocol
	}
	card.Avatar, err = fieldText(fields, 2, 1024)
	if err != nil {
		return nil, err
	}
	card.Nickname, err = fieldText(fields, 3, 1024)
	if err != nil {
		return nil, err
	}
	card.AccountID, err = number(fields, 4)
	if err != nil || card.AccountID >= 1<<31 {
		return nil, ErrProtocol
	}
	card.VIPLevel, err = number(fields, 5)
	if err != nil || card.VIPLevel >= 1<<31 {
		return nil, ErrProtocol
	}
	pretty, err := number(fields, 6)
	if err != nil || pretty > 1 {
		return nil, ErrProtocol
	}
	card.IsPretty = pretty == 1
	if len(fields[7]) > 0 {
		stamp, err := nested(fields, 7)
		if err != nil {
			return nil, err
		}
		seconds, e1 := number(stamp, 1)
		nanos, e2 := number(stamp, 2)
		if e1 != nil || e2 != nil || seconds > 253402300799 || nanos >= 1000000000 {
			return nil, ErrProtocol
		}
		card.SharedAt = seconds*1000 + nanos/1000000
	}
	return card, nil
}

func parseMessageImage(f map[protowire.Number][]protoValue, field protowire.Number) (*MessageImage, error) {
	if len(f[field]) != 1 {
		return nil, ErrProtocol
	}
	image, err := nested(f, field)
	if err != nil {
		return nil, err
	}
	asset, err := nested(image, 1)
	if err != nil {
		return nil, err
	}
	out := &MessageImage{}
	out.Asset, err = fieldText(asset, 1, 20)
	if err != nil || !ValidAssetID(out.Asset) {
		return nil, ErrProtocol
	}
	for _, value := range []struct {
		field protowire.Number
		dst   *uint64
	}{{2, &out.Width}, {3, &out.Height}, {4, &out.Size}} {
		*value.dst, err = number(image, value.field)
		if err != nil {
			return nil, err
		}
	}
	sticker, err := number(image, 5)
	// Projection reads metadata only. Resource acquisition keeps its own byte,
	// dimension and pixel limits; a large image must not hide the whole message.
	if err != nil || sticker > 1 || out.Width >= 1<<31 || out.Height >= 1<<31 || out.Size >= 1<<31 {
		return nil, ErrProtocol
	}
	out.Sticker = sticker == 1
	out.PreviewLimited = !out.PreviewAllowed()
	return out, nil
}

func (m MessageImage) PreviewAllowed() bool {
	return m.Size <= MaxImageBytes && m.Width <= 16384 && m.Height <= 16384 &&
		m.Width*m.Height <= MaxImagePixels
}

// Desktop 2.8.3 forward field 317 contains flat entries, not recursive messages.
// Nested forward references remain explicit; they never trigger history reads.
func parseMessageForward(f map[protowire.Number][]protoValue) (*MessageForward, error) {
	if len(f[317]) != 1 {
		return nil, ErrProtocol
	}
	forward, err := nested(f, 317)
	if err != nil {
		return nil, err
	}
	title, err := fieldText(forward, 1, 1024)
	if err != nil {
		return nil, err
	}
	out := &MessageForward{Title: title, Entries: []MessageForwardEntry{}, Total: len(forward[3])}
	for index, raw := range forward[3] {
		if raw.t != protowire.BytesType {
			return nil, ErrProtocol
		}
		if index >= MaxForwardEntries {
			out.Truncated = true
			continue
		}
		entry, err := messageFields(raw.b)
		if err != nil {
			return nil, err
		}
		item := MessageForwardEntry{}
		item.Sender.ID, err = number(entry, 1)
		if err != nil || item.Sender.ID >= 1<<30 {
			return nil, ErrProtocol
		}
		item.Sender.Name, err = fieldText(entry, 2, 1024)
		if err != nil {
			return nil, err
		}
		item.Sender.Avatar, err = fieldText(entry, 8, 1024)
		if err != nil {
			return nil, err
		}
		item.Format, err = number(entry, 23)
		if err != nil {
			return nil, err
		}
		item.CreatedAt, err = number(entry, 25)
		if err != nil || item.CreatedAt > 253402300799999 {
			return nil, ErrProtocol
		}
		payloads := len(entry[100]) + len(entry[101]) + len(entry[102]) + len(entry[103]) + len(entry[104])
		if payloads > 1 {
			return nil, ErrProtocol
		}
		switch item.Format {
		case 0:
			if len(entry[100]) != 1 {
				item.Reason = "forward_content_missing"
			} else if item.Text, err = content(entry, 100); err != nil {
				return nil, err
			}
		case 1:
			if item.Image, err = parseMessageImage(entry, 101); err != nil {
				return nil, err
			}
		case 3:
			if item.Video, err = parseMessageVideo(entry); err != nil {
				return nil, err
			}
		case 9:
			item.Reason = "nested_forward_unresolved"
		default:
			item.Reason = "forward_format_unsupported"
		}
		out.Entries = append(out.Entries, item)
	}
	return out, nil
}
