package nativeauth

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"strconv"
	"time"

	"golang.org/x/image/draw"
	"wangshangliao/internal/nativewire"
)

type MessageUpload struct {
	Kind, Name string
	Data       []byte
	Thumbnail  []byte
}

// InspectUpload derives metadata from bounded bytes, not browser claims.
// It validates containers, not the complete audio/video codec or file safety.
func InspectUpload(in MessageUpload) (nativewire.OutboundMedia, error) {
	out := nativewire.OutboundMedia{Kind: in.Kind, Asset: "1", Size: uint64(len(in.Data))}
	if !nativewire.MediaKind(in.Kind) || len(in.Data) == 0 || len(in.Data) > nativewire.MaxUploadBytes ||
		!nativewire.ValidFilename(in.Name) || len(in.Thumbnail) > 1024*1024 {
		return out, rejection("native_upload_invalid")
	}
	switch in.Kind {
	case "image":
		cfg, format, err := image.DecodeConfig(bytes.NewReader(in.Data))
		if err != nil || (format != "jpeg" && format != "png" && format != "gif" && format != "webp") ||
			cfg.Width < 1 || cfg.Height < 1 || cfg.Width > 4096 || cfg.Height > 4096 ||
			int64(cfg.Width)*int64(cfg.Height) > 4*1024*1024 || len(in.Data) > nativewire.MaxImageBytes ||
			len(in.Thumbnail) != 0 {
			return out, rejection("native_upload_invalid")
		}
		out.Width, out.Height = uint64(cfg.Width), uint64(cfg.Height)
	case "voice":
		if len(in.Thumbnail) != 0 {
			return out, rejection("native_upload_invalid")
		}
		duration, err := waveDuration(in.Data)
		if err != nil {
			duration, err = mp3Duration(in.Data)
		}
		if err != nil {
			return out, err
		}
		out.Duration = duration
	case "video":
		duration, width, height, err := mp4Metadata(in.Data)
		if err != nil || len(in.Thumbnail) == 0 {
			return out, rejection("native_upload_invalid")
		}
		poster, err := NormalizeAvatar(in.Thumbnail)
		clear(poster)
		if err != nil {
			return out, rejection("native_upload_invalid")
		}
		out.Duration, out.Width, out.Height = duration, width, height
	case "file":
		out.Filename = in.Name
		if len(in.Thumbnail) != 0 {
			return out, rejection("native_upload_invalid")
		}
	}
	if out.Validate() != nil {
		return out, rejection("native_upload_invalid")
	}
	out.Asset = "" // A valid receipt is required before encoding a message.
	return out, nil
}

func waveDuration(data []byte) (uint64, error) {
	invalid := rejection("native_upload_audio_invalid")
	if len(data) < 44 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WAVE" ||
		uint64(binary.LittleEndian.Uint32(data[4:8]))+8 != uint64(len(data)) {
		return 0, invalid
	}
	var rate, align, count uint64
	var fmtSeen, dataSeen bool
	for offset, chunks := 12, 0; offset < len(data); chunks++ {
		if chunks > 128 || offset+8 > len(data) {
			return 0, invalid
		}
		size := uint64(binary.LittleEndian.Uint32(data[offset+4 : offset+8]))
		start, end := uint64(offset+8), uint64(offset+8)+size
		if end > uint64(len(data)) {
			return 0, invalid
		}
		switch string(data[offset : offset+4]) {
		case "fmt ":
			if fmtSeen || size < 16 {
				return 0, invalid
			}
			f := data[start:end]
			channels := uint64(binary.LittleEndian.Uint16(f[2:4]))
			sampleRate := uint64(binary.LittleEndian.Uint32(f[4:8]))
			rate = uint64(binary.LittleEndian.Uint32(f[8:12]))
			align = uint64(binary.LittleEndian.Uint16(f[12:14]))
			if binary.LittleEndian.Uint16(f[:2]) != 1 || binary.LittleEndian.Uint16(f[14:16]) != 16 ||
				channels < 1 || channels > 2 || sampleRate < 8000 || sampleRate > 48000 ||
				align != channels*2 || rate != sampleRate*align {
				return 0, invalid
			}
			fmtSeen = true
		case "data":
			if dataSeen || size == 0 {
				return 0, invalid
			}
			count, dataSeen = size, true
		}
		offset = int(end + size%2)
		if offset > len(data) {
			return 0, invalid
		}
	}
	if !fmtSeen || !dataSeen || align == 0 || count%align != 0 {
		return 0, invalid
	}
	seconds := (count + rate - 1) / rate
	if seconds == 0 || seconds > 300 {
		return 0, invalid
	}
	return seconds, nil
}

// Only bounded ISO-BMFF metadata boxes are traversed. Fragmented or ambiguous
// metadata fails closed; this does not validate codec samples or encryption.
func mp4Metadata(data []byte) (duration, width, height uint64, err error) {
	invalid := rejection("native_upload_video_invalid")
	if http.DetectContentType(data) != "video/mp4" {
		return 0, 0, 0, invalid
	}
	var moov, mdat bool
	err = mp4Boxes(data, func(kind string, box []byte) error {
		switch kind {
		case "mdat":
			mdat = len(box) > 0
		case "moof":
			return invalid
		case "moov":
			if moov {
				return invalid
			}
			moov = true
			return mp4Boxes(box, func(kind string, box []byte) error {
				switch kind {
				case "mvhd":
					if duration != 0 || len(box) < 20 || box[0] != 0 {
						return invalid
					}
					scale, ticks := uint64(binary.BigEndian.Uint32(box[12:16])), uint64(binary.BigEndian.Uint32(box[16:20]))
					if scale == 0 || ticks == 0 {
						return invalid
					}
					duration = (ticks + scale - 1) / scale
				case "trak":
					var w, h uint64
					video := false
					if e := mp4Boxes(box, func(kind string, box []byte) error {
						if kind == "tkhd" {
							if len(box) < 84 || box[0] != 0 || w != 0 {
								return invalid
							}
							w, h = uint64(binary.BigEndian.Uint32(box[76:80])>>16), uint64(binary.BigEndian.Uint32(box[80:84])>>16)
						}
						if kind == "mdia" {
							return mp4Boxes(box, func(kind string, box []byte) error {
								if kind == "hdlr" {
									if len(box) < 12 {
										return invalid
									}
									video = string(box[8:12]) == "vide"
								}
								return nil
							})
						}
						return nil
					}); e != nil {
						return e
					}
					if video {
						if width != 0 || w == 0 || h == 0 {
							return invalid
						}
						width, height = w, h
					}
				}
				return nil
			})
		}
		return nil
	})
	if err != nil || !moov || !mdat || duration == 0 || duration > 3600 ||
		width == 0 || height == 0 || width > 16384 || height > 16384 || width*height > nativewire.MaxImagePixels {
		return 0, 0, 0, invalid
	}
	return duration, width, height, nil
}

func mp4Boxes(data []byte, visit func(string, []byte) error) error {
	for count := 0; len(data) > 0; count++ {
		if count > 1024 || len(data) < 8 {
			return rejection("native_upload_video_invalid")
		}
		size := uint64(binary.BigEndian.Uint32(data[:4]))
		header := uint64(8)
		if size == 1 && len(data) >= 16 {
			size, header = binary.BigEndian.Uint64(data[8:16]), 16
		}
		if size < header || size > uint64(len(data)) {
			return rejection("native_upload_video_invalid")
		}
		if err := visit(string(data[4:8]), data[header:size]); err != nil {
			return err
		}
		data = data[size:]
	}
	return nil
}

func uploadThumbnail(in MessageUpload) ([]byte, error) {
	if in.Kind == "video" {
		in.Data = in.Thumbnail
	}
	src, _, err := image.Decode(bytes.NewReader(in.Data))
	if err != nil {
		return nil, rejection("native_upload_invalid")
	}
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	if w >= h {
		h, w = max(1, h*256/w), 256
	} else {
		w, h = max(1, w*256/h), 256
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	var out bytes.Buffer
	if jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85}) != nil {
		return nil, rejection("native_upload_invalid")
	}
	return out.Bytes(), nil
}

// UploadMessage uses one fixed reviewed origin, one request and no retry.
// The guard commits authorization before network I/O; no transaction waits for
// an upload response. The later message submission has its own final fence.
func (c *Client) UploadMessage(ctx context.Context, in MessageUpload, guard func(context.Context) error) (nativewire.OutboundMedia, error) {
	meta, err := InspectUpload(in)
	if err != nil || guard == nil {
		return meta, rejection("native_upload_invalid")
	}
	address, err := avatarUploadURL(c.config)
	if err != nil {
		return meta, err
	}
	var body bytes.Buffer
	defer func() { clear(body.Bytes()) }()
	writer := multipart.NewWriter(&body)
	contentType := http.DetectContentType(in.Data)
	if in.Kind == "voice" {
		contentType = "audio/wav"
		if !bytes.HasPrefix(in.Data, []byte("RIFF")) {
			contentType = "audio/mpeg"
		}
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": in.Name}))
	header.Set("Content-Type", contentType)
	part, err := writer.CreatePart(header)
	if err != nil {
		return meta, err
	}
	if _, err = part.Write(in.Data); err != nil {
		return meta, err
	}
	if in.Kind == "image" || in.Kind == "video" {
		thumbnail, err := uploadThumbnail(in)
		if err != nil {
			return meta, err
		}
		part, err = writer.CreateFormFile("thumbnail", "thumbnail.jpg")
		if err == nil {
			_, err = part.Write(thumbnail)
		}
		clear(thumbnail)
		if err != nil {
			return meta, err
		}
	}
	assetType := map[string]string{"image": "1", "voice": "2", "video": "3", "file": "4"}[in.Kind]
	fields := map[string]string{"asset_type": assetType, "use_for": "1"}
	if meta.Width != 0 {
		fields["width"], fields["height"] = strconv.FormatUint(meta.Width, 10), strconv.FormatUint(meta.Height, 10)
	}
	for key, value := range fields {
		if err = writer.WriteField(key, value); err != nil {
			return meta, err
		}
	}
	if err = writer.Close(); err != nil {
		return meta, err
	}
	ctx, stop := context.WithTimeout(ctx, 10*time.Second)
	defer stop()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body.Bytes()))
	if err != nil {
		return meta, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("token", c.jwt)
	if err = guard(ctx); err != nil {
		return meta, err
	}
	if observe, ok := ctx.Value(requestObserverKey{}).(func()); ok {
		observe()
	}
	client := *c.http
	client.Jar = nil
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	resp, err := client.Do(req)
	if err != nil {
		return meta, &Error{Code: "native_transport_unknown", Submitted: true}
	}
	defer resp.Body.Close()
	if resp.StatusCode == 429 {
		delay := time.Duration(min(86400, max(1, retryAfter(resp.Header.Get("Retry-After"))))) * time.Second
		c.cooldown = time.Now().Add(delay)
		return meta, &Error{Code: "native_rate_limited", RetryAfter: delay, Submitted: true}
	}
	if resp.StatusCode >= 500 || resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return meta, &Error{Code: "native_transport_unknown", Submitted: true}
	}
	if resp.StatusCode != 200 {
		return meta, &Error{Code: "native_http_rejected", Submitted: true}
	}
	encoded, err := io.ReadAll(io.LimitReader(resp.Body, 4097))
	defer clear(encoded)
	var result struct {
		Code *int   `json:"code"`
		ID   string `json:"id"`
	}
	if err != nil || len(encoded) > 4096 || json.Unmarshal(encoded, &result) != nil || result.Code == nil {
		return meta, &Error{Code: "native_response_invalid", Submitted: true}
	}
	if *result.Code != 0 {
		return meta, &Error{Code: "native_business_rejected", Number: *result.Code, Submitted: true}
	}
	meta.Asset = result.ID
	if meta.Validate() != nil {
		return nativewire.OutboundMedia{}, &Error{Code: "native_response_invalid", Submitted: true}
	}
	return meta, nil
}

func retryAfter(value string) int {
	if n, err := strconv.Atoi(value); err == nil {
		return n
	}
	if at, err := http.ParseTime(value); err == nil {
		return int(time.Until(at).Seconds()) + 1
	}
	return 1
}
