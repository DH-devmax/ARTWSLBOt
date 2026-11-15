package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"wangshangliao/internal/gateway"
	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

type mediaLinks struct {
	Sender  string              `json:"sender,omitempty"`
	Target  string              `json:"target,omitempty"`
	Image   string              `json:"image,omitempty"`
	Video   string              `json:"video,omitempty"`
	Poster  string              `json:"poster,omitempty"`
	Contact string              `json:"contact,omitempty"`
	Entries []map[string]string `json:"entries,omitempty"`
}

func nativeMediaLinks(account, conversation string, event int64, message nativewire.ApplicationMessage) mediaLinks {
	base := "/api/v1/accounts/" + account + "/conversations/" + conversation +
		"/messages/" + strconv.FormatInt(event, 10) + "/media/"
	out := mediaLinks{}
	if nativewire.ValidAssetID(strings.TrimSuffix(message.Sender.Avatar, ".jpg")) {
		out.Sender = base + "sender"
	}
	if nativewire.ValidAssetID(strings.TrimSuffix(message.Target.Avatar, ".jpg")) {
		out.Target = base + "target"
	}
	if message.Image != nil && message.Image.PreviewAllowed() {
		out.Image = base + "image"
	}
	if message.Video != nil && nativewire.ValidAssetID(message.Video.Asset) {
		out.Poster = base + "video-poster"
		if message.Video.Size <= nativewire.MaxVideoBytes {
			out.Video = base + "video"
		}
	}
	if message.FriendCard != nil && nativewire.ValidAssetID(strings.TrimSuffix(message.FriendCard.Avatar, ".jpg")) {
		out.Contact = base + "contact-avatar"
	}
	if message.Forward != nil {
		for index, entry := range message.Forward.Entries {
			link := map[string]string{}
			prefix := base + "entry-" + strconv.Itoa(index)
			if nativewire.ValidAssetID(strings.TrimSuffix(entry.Sender.Avatar, ".jpg")) {
				link["avatar"] = prefix + "-avatar"
			}
			if entry.Image != nil && entry.Image.PreviewAllowed() {
				link["image"] = prefix + "-image"
			}
			if entry.Video != nil && nativewire.ValidAssetID(entry.Video.Asset) {
				link["poster"] = prefix + "-poster"
				if entry.Video.Size <= nativewire.MaxVideoBytes {
					link["video"] = prefix + "-video"
				}
			}
			out.Entries = append(out.Entries, link)
		}
	}
	return out
}

func previewForward(forward *nativewire.MessageForward) *nativewire.MessageForward {
	if forward == nil {
		return nil
	}
	title, _ := boundedText(forward.Title, 100)
	return &nativewire.MessageForward{Title: title, Total: forward.Total, Truncated: forward.Truncated}
}

func clipForward(forward *nativewire.MessageForward) bool {
	if forward == nil {
		return false
	}
	clipped := false
	forward.Title, clipped = boundedText(forward.Title, 256)
	if len(forward.Entries) > nativewire.MaxForwardEntries {
		forward.Entries = forward.Entries[:nativewire.MaxForwardEntries]
		clipped = true
	}
	remaining := 32768
	for index := range forward.Entries {
		entry := &forward.Entries[index]
		text, cut := boundedText(entry.Text, min(2048, remaining))
		entry.Text = text
		remaining -= len([]rune(text))
		clipped = clipped || cut
	}
	forward.Truncated = forward.Truncated || clipped
	return forward.Truncated
}

func messageMedia(message nativewire.ApplicationMessage, part string) (string, bool, *nativewire.MessageImage, error) {
	switch part {
	case "sender":
		return message.Sender.Avatar, true, nil, nil
	case "target":
		return message.Target.Avatar, true, nil, nil
	case "image":
		if message.Image != nil {
			return message.Image.Asset, false, message.Image, nil
		}
	case "contact-avatar":
		if message.FriendCard != nil {
			return message.FriendCard.Avatar, true, nil, nil
		}
	case "video-poster":
		if message.Video != nil {
			return message.Video.Asset, true, nil, nil
		}
	default:
		pieces := strings.Split(part, "-")
		if len(pieces) == 3 && pieces[0] == "entry" && message.Forward != nil {
			index, err := strconv.Atoi(pieces[1])
			if err == nil && index >= 0 && strconv.Itoa(index) == pieces[1] && index < len(message.Forward.Entries) {
				item := message.Forward.Entries[index]
				if pieces[2] == "avatar" {
					return item.Sender.Avatar, true, nil, nil
				}
				if pieces[2] == "image" && item.Image != nil {
					return item.Image.Asset, false, item.Image, nil
				}
				if pieces[2] == "poster" && item.Video != nil {
					return item.Video.Asset, true, nil, nil
				}
			}
		}
	}
	return "", false, nil, fail(404, "native_media_not_found")
}

func messageVideo(message nativewire.ApplicationMessage, part string) *nativewire.MessageVideo {
	if part == "video" {
		return message.Video
	}
	pieces := strings.Split(part, "-")
	if len(pieces) == 3 && pieces[0] == "entry" && pieces[2] == "video" && message.Forward != nil {
		index, err := strconv.Atoi(pieces[1])
		if err == nil && index >= 0 && strconv.Itoa(index) == pieces[1] && index < len(message.Forward.Entries) {
			return message.Forward.Entries[index].Video
		}
	}
	return nil
}

func (s *Server) nativeMessageMedia(w http.ResponseWriter, r *http.Request, p Principal) error {
	event, err := strconv.ParseInt(r.PathValue("event"), 10, 64)
	if err != nil || event <= 0 || !conversationPattern.MatchString(r.PathValue("conversation")) || r.URL.RawQuery != "" {
		return fail(400, "invalid_media_reference")
	}
	// Reject rather than build a download backlog in the control API.
	select {
	case s.mediaSlots <- struct{}{}:
		defer func() { <-s.mediaSlots }()
	default:
		w.Header().Set("Retry-After", "2")
		return fail(429, "native_media_busy")
	}
	var sealed []byte
	var epoch int64
	var platform string
	err = s.DB.Platform.QueryRow(r.Context(), `SELECT e.payload_sealed,a.epoch,a.platform
        FROM native_decoded_events e JOIN accounts a ON a.id=e.account_id
        WHERE e.event_id=$1 AND e.account_id=$2 AND e.conversation_key=$3
        AND e.received_at>clock_timestamp()-interval '7 days'
        AND e.state IN ('decoded','unsupported') AND a.platform IN('wsl','lajiaoliao')`,
		event, r.PathValue("id"), r.PathValue("conversation")).Scan(&sealed, &epoch, &platform)
	if err == pgx.ErrNoRows {
		return fail(404, "native_media_not_found")
	}
	if err != nil {
		return err
	}
	plain, err := s.Vault.Open(gateway.DecodedAAD(r.PathValue("id"), event), sealed)
	if err != nil {
		return fail(503, "native_projection_unreadable")
	}
	var decoded gateway.DecodedMessage
	err = json.Unmarshal(plain, &decoded)
	clear(plain)
	if err != nil {
		return fail(503, "native_projection_unreadable")
	}
	if platform == "lajiaoliao" {
		return s.chiliMessageMedia(w, r, p, event, epoch, sealed, decoded)
	}
	video := messageVideo(decoded.Message, r.PathValue("part"))
	asset, avatar := "", false
	var expected *nativewire.MessageImage
	if video == nil {
		asset, avatar, expected, err = messageMedia(decoded.Message, r.PathValue("part"))
		if err != nil {
			return err
		}
		if asset == "" {
			return fail(404, "native_media_not_found")
		}
	}
	d, version, err := s.loadNativeDeployment(r.Context())
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	var file nativeauth.MediaFile
	if video != nil {
		file, err = nativeauth.FetchVideo(ctx, s.mediaClient, d, video)
	} else {
		file, err = nativeauth.FetchImage(ctx, s.mediaClient, d, asset, avatar, expected)
	}
	if err != nil {
		var code = "native_media_unavailable"
		if e, ok := err.(*nativeauth.Error); ok {
			code = e.Code
		}
		return fail(502, code)
	}
	defer clear(file.Data)
	if err = s.accountEventAccess(r, p, "messages"); err != nil {
		return err
	}
	// Revalidate retained evidence, epoch and deployment after the network wait.
	var current []byte
	err = s.DB.Platform.QueryRow(ctx, `SELECT e.payload_sealed FROM native_decoded_events e
        JOIN accounts a ON a.id=e.account_id CROSS JOIN native_deployment d
        WHERE e.event_id=$1 AND e.account_id=$2 AND e.conversation_key=$3
        AND e.received_at>clock_timestamp()-interval '7 days'
        AND a.epoch=$4 AND d.version=$5`, event, r.PathValue("id"), r.PathValue("conversation"), epoch, version).Scan(&current)
	if err == pgx.ErrNoRows {
		return fail(409, "native_media_scope_changed")
	}
	if err != nil {
		return err
	}
	if !bytes.Equal(current, sealed) {
		return fail(409, "native_media_scope_changed")
	}
	return writeNativeMedia(w, file)
}

func writeNativeMedia(w http.ResponseWriter, file nativeauth.MediaFile) error {
	w.Header().Set("X-Accel-Buffering", "no")
	w.Header().Set("Content-Type", file.MIME)
	w.Header().Set("Content-Disposition", `inline; filename="native-media"`)
	w.Header().Set("Content-Security-Policy", "default-src 'none'; sandbox")
	w.Header().Set("Cross-Origin-Resource-Policy", "same-origin")
	w.Header().Set("Content-Length", strconv.Itoa(len(file.Data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(file.Data)
	return nil
}
