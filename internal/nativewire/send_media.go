package nativewire

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protowire"
)

const MaxUploadBytes = 16 * 1024 * 1024

// OutboundMedia contains only a server-held upload receipt and checked metadata.
// A browser asset ID or remote URL is never an admissible upload receipt.
type OutboundMedia struct {
	Kind     string `json:"kind"`
	Asset    string `json:"asset"`
	Filename string `json:"filename,omitempty"`
	Size     uint64 `json:"size"`
	Width    uint64 `json:"width,omitempty"`
	Height   uint64 `json:"height,omitempty"`
	Duration uint64 `json:"duration,omitempty"`
}

func MediaKind(kind string) bool {
	return kind == "image" || kind == "voice" || kind == "video" || kind == "file"
}

func ValidFilename(name string) bool {
	return utf8.ValidString(name) && len(name) > 0 && len(name) <= 240 &&
		strings.TrimSpace(name) == name && name != "." && name != ".." &&
		!strings.ContainsAny(name, `/\`) &&
		strings.IndexFunc(name, unicode.IsControl) < 0
}

func (m OutboundMedia) Validate() error {
	if !MediaKind(m.Kind) || !ValidAssetID(m.Asset) || m.Size == 0 || m.Size > MaxUploadBytes {
		return ErrProtocol
	}
	switch m.Kind {
	case "image", "video":
		if m.Width == 0 || m.Height == 0 || m.Width > 16384 || m.Height > 16384 ||
			m.Width*m.Height > MaxImagePixels || m.Filename != "" {
			return ErrProtocol
		}
		if m.Kind == "image" && (m.Size > MaxImageBytes || m.Duration != 0) ||
			m.Kind == "video" && (m.Duration == 0 || m.Duration > 3600) {
			return ErrProtocol
		}
	case "voice":
		if m.Duration == 0 || m.Duration > 300 || m.Width != 0 || m.Height != 0 || m.Filename != "" {
			return ErrProtocol
		}
	case "file":
		if !ValidFilename(m.Filename) || m.Duration != 0 || m.Width != 0 || m.Height != 0 {
			return ErrProtocol
		}
	}
	return nil
}

func (m OutboundMedia) encode() ([]byte, uint64, protowire.Number, error) {
	if err := m.Validate(); err != nil {
		return nil, 0, 0, err
	}
	out := messageBytes(1, messageBytes(1, []byte(m.Asset)))
	switch m.Kind {
	case "image":
		out = append(out, messageNumber(2, m.Width)...)
		out = append(out, messageNumber(3, m.Height)...)
		out = append(out, messageNumber(4, m.Size)...)
		return out, 1, 101, nil
	case "voice":
		out = append(out, messageNumber(2, m.Duration)...)
		return out, 2, 102, nil
	case "video":
		// Desktop uploads use the same asset's .jpg cover, not a guessed ID.
		out = append(out, messageBytes(2, nil)...)
		out = append(out, messageNumber(4, m.Duration)...)
		out = append(out, messageNumber(5, m.Width)...)
		out = append(out, messageNumber(6, m.Height)...)
		out = append(out, messageNumber(7, m.Size)...)
		return out, 3, 103, nil
	default:
		out = append(out, messageBytes(2, []byte(m.Filename))...)
		out = append(out, messageNumber(3, m.Size)...)
		// File format is 6; 4 is geography, despite upload asset_type=4.
		return out, 6, 104, nil
	}
}
