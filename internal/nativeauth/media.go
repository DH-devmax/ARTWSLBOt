// SPDX-License-Identifier: AGPL-3.0-or-later
package nativeauth

import (
	"bytes"
	"context"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"strings"

	_ "golang.org/x/image/webp"

	"wangshangliao/internal/nativewire"
)

// The production desktop 2.8.3 signed bootstrap binds these business origins
// and this download line. A caller cannot supply a host, URL or path.
func mediaURL(d Deployment, asset string, avatar bool) (string, error) {
	if d.ProtocolVersion != "2.8.3" ||
		(d.Origin != "https://ifeng-api01.hfjtqy.com" && d.Origin != "https://ifeng-api02.hfjtqy.com") {
		return "", rejection("native_media_origin_unreviewed")
	}
	if avatar {
		asset = strings.TrimSuffix(asset, ".jpg")
	}
	if !nativewire.ValidAssetID(asset) {
		return "", rejection("native_media_reference_unsupported")
	}
	suffix := ""
	if avatar {
		suffix = ".jpg"
	}
	return "https://images-03003.hfjtqy.com/assets/" + asset + suffix, nil
}

type MediaFile struct {
	Data []byte
	MIME string
}

// Media uses a credential-free fixed-line read, separate from business HTTP
// and the receiver. It keeps no disk cache and follows no redirect.
func fetchMedia(ctx context.Context, client *http.Client, d Deployment, asset string, avatar bool, accept string, limit int64) ([]byte, error) {
	address, err := mediaURL(d, asset, avatar)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, rejection("native_media_unavailable")
	}
	req.Header.Set("Accept", accept)
	safeClient := *client
	safeClient.Jar = nil
	safeClient.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := safeClient.Do(req)
	if err != nil {
		return nil, rejection("native_media_unavailable")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, rejection("native_media_unavailable")
	}
	if response.ContentLength > limit || response.Header.Get("Content-Encoding") != "" {
		return nil, rejection("native_media_invalid")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit {
		clear(data)
		return nil, rejection("native_media_invalid")
	}
	return data, nil
}

func FetchImage(ctx context.Context, client *http.Client, d Deployment, asset string, avatar bool, expected *nativewire.MessageImage) (MediaFile, error) {
	if expected != nil && !expected.PreviewAllowed() {
		return MediaFile{}, rejection("native_image_preview_limited")
	}
	limit := int64(nativewire.MaxImageBytes)
	if avatar {
		limit = 1024 * 1024
	}
	data, err := fetchMedia(ctx, client, d, asset, avatar, "image/png, image/jpeg, image/gif, image/webp", limit)
	if err != nil {
		return MediaFile{}, err
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(data))
	mime := map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}[format]
	if err != nil || mime == "" || config.Width <= 0 || config.Height <= 0 ||
		config.Width > 16384 || config.Height > 16384 || int64(config.Width)*int64(config.Height) > nativewire.MaxImagePixels ||
		expected != nil && (expected.Size != 0 && expected.Size != uint64(len(data)) ||
			expected.Width != 0 && expected.Width != uint64(config.Width) ||
			expected.Height != 0 && expected.Height != uint64(config.Height)) {
		clear(data)
		return MediaFile{}, rejection("native_media_invalid")
	}
	return MediaFile{Data: data, MIME: mime}, nil
}

// Container sniffing is not a claim of complete video/codec validation.
// The browser reports unsupported/corrupt playback; no transcoder is started.
func FetchVideo(ctx context.Context, client *http.Client, d Deployment, expected *nativewire.MessageVideo) (MediaFile, error) {
	if expected == nil || expected.Asset == "" {
		return MediaFile{}, rejection("native_media_reference_unsupported")
	}
	if expected.Size > nativewire.MaxVideoBytes {
		return MediaFile{}, rejection("native_video_too_large")
	}
	data, err := fetchMedia(ctx, client, d, expected.Asset, false, "video/mp4, video/webm", nativewire.MaxVideoBytes)
	if err != nil {
		return MediaFile{}, err
	}
	mime := http.DetectContentType(data)
	if (mime != "video/mp4" && mime != "video/webm") ||
		expected.Size != 0 && expected.Size != uint64(len(data)) {
		clear(data)
		return MediaFile{}, rejection("native_media_invalid")
	}
	return MediaFile{Data: data, MIME: mime}, nil
}
