package nativeauth

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/image/draw"
	"wangshangliao/internal/nativewire"
)

// NormalizeAvatar bounds decode work and removes EXIF/ancillary image data.
func NormalizeAvatar(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > 1024*1024 {
		return nil, rejection("native_avatar_invalid")
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || format != "jpeg" && format != "png" || cfg.Width < 1 || cfg.Height < 1 ||
		cfg.Width > 4096 || cfg.Height > 4096 || cfg.Width*cfg.Height > 4*1024*1024 {
		return nil, rejection("native_avatar_invalid")
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, rejection("native_avatar_invalid")
	}
	bounds := src.Bounds()
	side := min(bounds.Dx(), bounds.Dy())
	crop := image.Rect(bounds.Min.X+(bounds.Dx()-side)/2, bounds.Min.Y+(bounds.Dy()-side)/2,
		bounds.Min.X+(bounds.Dx()+side)/2, bounds.Min.Y+(bounds.Dy()+side)/2)
	dst := image.NewRGBA(image.Rect(0, 0, 256, 256))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, crop, draw.Src, nil)
	var out bytes.Buffer
	if jpeg.Encode(&out, dst, &jpeg.Options{Quality: 90}) != nil {
		return nil, rejection("native_avatar_invalid")
	}
	return out.Bytes(), nil
}

func avatarUploadURL(d Deployment) (string, error) {
	if _, err := mediaURL(d, "1", false); err != nil {
		return "", err
	}
	// Production desktop signed bootstrap field 8, first upload line.
	return "https://authupapi01.hfjtqy.com/v1/assets/userupload", nil
}

func (c *Client) changeAvatar(ctx context.Context, imageBytes []byte, state *SecurityState, fence nativewire.SubmissionFence) error {
	address, err := avatarUploadURL(c.config)
	if err != nil {
		return err
	}
	data, err := NormalizeAvatar(imageBytes)
	if err != nil {
		return err
	}
	defer clear(data)
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for _, name := range []string{"file", "thumbnail"} {
		part, err := writer.CreateFormFile(name, "avatar.jpg")
		if err != nil {
			return rejection("native_avatar_invalid")
		}
		if _, err = part.Write(data); err != nil {
			return err
		}
	}
	for k, v := range map[string]string{"asset_type": "1", "use_for": "3", "height": "256", "width": "256"} {
		if err = writer.WriteField(k, v); err != nil {
			return err
		}
	}
	if err = writer.Close(); err != nil {
		return err
	}
	defer clear(body.Bytes())
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body.Bytes()))
	if err != nil {
		return rejection("native_avatar_invalid")
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("token", c.jwt)
	var encoded []byte
	attempted := false
	err = fence(ctx, func(work context.Context) error {
		attempted = true
		if observe, ok := ctx.Value(requestObserverKey{}).(func()); ok {
			observe()
		}
		client := *c.http
		client.Jar = nil
		client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
		resp, err := client.Do(req.WithContext(work))
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode == 429 {
			seconds, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
			delay := time.Duration(max(1, min(seconds, 86400))) * time.Second
			c.cooldown = time.Now().Add(delay)
			return &Error{Code: "native_rate_limited", RetryAfter: delay, Submitted: true}
		}
		if resp.StatusCode >= 500 {
			return &Error{Code: "native_transport_unknown", Submitted: true}
		}
		if resp.StatusCode != 200 {
			return &Error{Code: "native_http_rejected", Submitted: true}
		}
		encoded, err = io.ReadAll(io.LimitReader(resp.Body, 4097))
		return err
	})
	if err != nil {
		if !attempted {
			return err
		}
		if _, ok := err.(*Error); ok {
			return err
		}
		return &Error{Code: "native_transport_unknown", Submitted: true}
	}
	var response struct {
		Code *int   `json:"code"`
		ID   string `json:"id"`
	}
	if len(encoded) > 4096 || json.Unmarshal(encoded, &response) != nil || response.Code == nil {
		return &Error{Code: "native_response_invalid", Submitted: true}
	}
	if *response.Code != 0 {
		return &Error{Code: "native_business_rejected", Number: *response.Code, Submitted: true}
	}
	if !nativewire.ValidAssetID(response.ID) {
		return &Error{Code: "native_response_invalid", Submitted: true}
	}
	state.Asset = response.ID
	if _, err = c.request(ctx, "/v1/user/image-check", map[string]any{"assetId": state.Asset}, nil); err != nil {
		return err
	}
	_, err = c.request(ctx, "/v1/settings/avatar", map[string]any{"id": state.Asset}, nil)
	return err
}
