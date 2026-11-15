package lajiaoliao

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	_ "golang.org/x/image/webp"
)

const (
	MaxPreviewImage = 5 << 20
	MaxPreviewVideo = 16 << 20
)

// A Resource is read only from retained native input. It is never serialized
// into the browser projection; the HTTP layer substitutes a scoped local URL.
type Resource struct {
	URL                 string
	Kind                string
	Size, Width, Height int64
}

type ResourceFile struct {
	Data []byte
	MIME string
}

func (r Resource) Valid() bool {
	u, err := url.Parse(r.URL)
	if err != nil || len(r.URL) > 8192 || u.Scheme != "https" || u.User != nil ||
		(u.Host != "lj-im-chat-01.lajiaoliao.com" && u.Host != objectStorageHost && u.Host != objectReadHost &&
			(u.Host != "im-api.lajiaoliao.com" || r.Kind == "video" && !strings.HasPrefix(u.Path, "/object/"))) ||
		u.Fragment != "" || u.Path == "" ||
		r.Size < 0 || r.Width < 0 || r.Height < 0 {
		return false
	}
	switch r.Kind {
	case "video":
		return r.Size <= MaxPreviewVideo
	case "image", "avatar":
		return r.Size <= MaxPreviewImage && r.Width <= 16384 && r.Height <= 16384 &&
			(r.Width == 0 || r.Height == 0 || r.Width*r.Height <= 40_000_000)
	}
	return false
}

func readResources(out *MessageView, object map[string]json.RawMessage, raw []byte) {
	out.Attachments = map[string]Resource{}
	if out.Kind == "image" {
		for _, field := range []string{"sourcePicture", "bigPicture", "snapshotPicture"} {
			picture, ok := messageObject(object[field])
			var resource struct {
				URL    string `json:"url"`
				Size   int64  `json:"size"`
				Width  int64  `json:"width"`
				Height int64  `json:"height"`
			}
			if !ok || json.Unmarshal(object[field], &resource) != nil || picture["url"] == nil {
				continue
			}
			r := Resource{URL: resource.URL, Kind: "image", Size: resource.Size, Width: resource.Width, Height: resource.Height}
			if r.Valid() {
				out.Attachments["image"] = r
				break
			}
		}
	}
	if out.Kind == "video" {
		var video struct {
			URL        string `json:"videoUrl"`
			Poster     string `json:"snapshotUrl"`
			Size       int64  `json:"videoSize"`
			PosterSize int64  `json:"snapshotSize"`
			Width      int64  `json:"snapshotWidth"`
			Height     int64  `json:"snapshotHeight"`
			Duration   int64  `json:"duration"`
		}
		if json.Unmarshal(raw, &video) != nil || video.Duration < 0 || video.Duration > 86400 ||
			video.Size < 0 {
			return
		}
		out.Duration, out.Size = video.Duration, video.Size
		out.Attachments["video"] = Resource{URL: video.URL, Kind: "video", Size: video.Size}
		out.Attachments["poster"] = Resource{URL: video.Poster, Kind: "image", Size: video.PosterSize,
			Width: video.Width, Height: video.Height}
	}
}

// Visits use the same bounded preorder for link creation and later resolution.
func (m *ReadMessage) Visit(visit func(int, *MessageView)) {
	index := 0
	var walk func(*MessageView)
	walk = func(view *MessageView) {
		if view == nil || index >= 32 {
			return
		}
		current := index
		index++
		visit(current, view)
		walk(view.Quote)
		for _, item := range view.Items {
			walk(item)
		}
	}
	walk(m.View)
}

func (m *ReadMessage) Resource(part string) (Resource, bool) {
	if part == "sender" {
		return m.Avatar, m.Avatar.Valid()
	}
	var out Resource
	m.Visit(func(index int, view *MessageView) {
		for name, resource := range view.Attachments {
			if part == "node-"+strconv.Itoa(index)+"-"+name && resource.Valid() {
				out = resource
			}
		}
	})
	return out, out.Valid()
}

// ResourceHTTP uses the platform's fixed public DNS dialer without cookies,
// native tokens, automatic redirects, or an environment proxy.
func ResourceHTTP() *http.Client { return NewClient().http }

func FetchResource(ctx context.Context, client *http.Client, resource Resource) (ResourceFile, error) {
	if !resource.Valid() || client == nil {
		return ResourceFile{}, &Failure{Code: "lajiaoliao_media_reference_unsupported"}
	}
	limit := int64(MaxPreviewImage)
	if resource.Kind == "avatar" {
		limit = 1 << 20
	}
	if resource.Kind == "video" {
		limit = MaxPreviewVideo
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, resource.URL, nil)
	if err != nil {
		return ResourceFile{}, &Failure{Code: "lajiaoliao_media_unavailable"}
	}
	req.Header.Set("Accept-Encoding", "identity")
	safe := *client
	safe.Jar = nil
	safe.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := safe.Do(req)
	if err != nil {
		return ResourceFile{}, &Failure{Code: "lajiaoliao_media_unavailable"}
	}
	if response.StatusCode == http.StatusFound && req.URL.Host == "im-api.lajiaoliao.com" &&
		strings.HasPrefix(req.URL.Path, "/object/") {
		target, locationErr := response.Location()
		if locationErr == nil && target.Host == objectReadHost &&
			(Resource{URL: target.String(), Kind: resource.Kind}).Valid() {
			response.Body.Close()
			next, e := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
			if e != nil {
				return ResourceFile{}, &Failure{Code: "lajiaoliao_media_unavailable"}
			}
			next.Header.Set("Accept-Encoding", "identity")
			response, err = safe.Do(next)
			if err != nil {
				return ResourceFile{}, &Failure{Code: "lajiaoliao_media_unavailable"}
			}
		}
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound || response.StatusCode == http.StatusGone {
		return ResourceFile{}, &Failure{Code: "lajiaoliao_media_expired"}
	}
	if response.StatusCode == http.StatusForbidden {
		return ResourceFile{}, &Failure{Code: "lajiaoliao_media_access_denied"}
	}
	if response.StatusCode != http.StatusOK || response.ContentLength > limit ||
		response.Header.Get("Content-Encoding") != "" {
		return ResourceFile{}, &Failure{Code: "lajiaoliao_media_unavailable"}
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || int64(len(data)) > limit || resource.Size != 0 && resource.Size != int64(len(data)) {
		clear(data)
		return ResourceFile{}, &Failure{Code: "lajiaoliao_media_invalid"}
	}
	mime := http.DetectContentType(data)
	if resource.Kind == "video" {
		if mime == "video/mp4" || mime == "video/webm" {
			return ResourceFile{Data: data, MIME: mime}, nil
		}
	} else {
		config, format, decodeErr := image.DecodeConfig(bytes.NewReader(data))
		mime = map[string]string{"png": "image/png", "jpeg": "image/jpeg", "gif": "image/gif", "webp": "image/webp"}[format]
		if decodeErr == nil && mime != "" && config.Width > 0 && config.Height > 0 &&
			config.Width <= 16384 && config.Height <= 16384 && int64(config.Width)*int64(config.Height) <= 40_000_000 &&
			(resource.Width == 0 || resource.Width == int64(config.Width)) &&
			(resource.Height == 0 || resource.Height == int64(config.Height)) {
			return ResourceFile{Data: data, MIME: mime}, nil
		}
	}
	clear(data)
	return ResourceFile{}, &Failure{Code: "lajiaoliao_media_invalid"}
}
