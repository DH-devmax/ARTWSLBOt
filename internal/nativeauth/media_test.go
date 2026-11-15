package nativeauth

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strings"
	"testing"

	"wangshangliao/internal/nativewire"
)

type mediaTransport func(*http.Request) (*http.Response, error)

func (f mediaTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestReviewedMediaSource(t *testing.T) {
	d := Deployment{Origin: "https://ifeng-api01.hfjtqy.com", ProtocolVersion: "2.8.3"}
	for _, asset := range []string{"https://127.0.0.1/a", "//example.com", "9/../../1", "01", "0", "9?x", "9#x", "9.png"} {
		if _, err := mediaURL(d, asset, false); err == nil {
			t.Fatal("arbitrary media URL or path admitted")
		}
	}
	address, err := mediaURL(d, "9007199254740993", true)
	if err != nil || address != "https://images-03003.hfjtqy.com/assets/9007199254740993.jpg" {
		t.Fatal("avatar line or string ID changed")
	}
	d.Origin = "https://arbitrary.example"
	if _, err = mediaURL(d, "9", false); err == nil {
		t.Fatal("unreviewed deployment inherited media line")
	}
}

func TestMediaDownloadLimitsAndNoCredentials(t *testing.T) {
	var data bytes.Buffer
	if err := png.Encode(&data, image.NewRGBA(image.Rect(0, 0, 2, 3))); err != nil {
		t.Fatal(err)
	}
	d := Deployment{Origin: "https://ifeng-api01.hfjtqy.com", ProtocolVersion: "2.8.3"}
	jar, _ := cookiejar.New(nil)
	request, _ := http.NewRequest("GET", "https://images-03003.hfjtqy.com", nil)
	jar.SetCookies(request.URL, []*http.Cookie{{Name: "synthetic-secret", Value: "must-not-send"}})
	count, status, body := 0, 200, data.Bytes()
	client := &http.Client{Jar: jar, Transport: mediaTransport(func(r *http.Request) (*http.Response, error) {
		count++
		if r.URL.String() != "https://images-03003.hfjtqy.com/assets/9" ||
			r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("token") != "" {
			t.Fatal("media sent credentials or changed target")
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://127.0.0.1/"}},
			Body: io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})}
	expected := &nativewire.MessageImage{Width: 2, Height: 3, Size: uint64(len(body))}
	file, err := FetchImage(context.Background(), client, d, "9", false, expected)
	if err != nil || file.MIME != "image/png" || !bytes.Equal(file.Data, body) {
		t.Fatal("valid synthetic image rejected", err)
	}
	expected.Width = 4
	if _, err = FetchImage(context.Background(), client, d, "9", false, expected); err == nil {
		t.Fatal("metadata mismatch accepted")
	}
	status = 302
	before := count
	if _, err = FetchImage(context.Background(), client, d, "9", false, nil); err == nil || count != before+1 {
		t.Fatal("redirect followed")
	}
	status, body = 200, []byte("<svg><script>untrusted</script></svg>")
	if _, err = FetchImage(context.Background(), client, d, "9", false, nil); err == nil {
		t.Fatal("active content accepted")
	}
	body = []byte(strings.Repeat("x", nativewire.MaxImageBytes+1))
	if _, err = FetchImage(context.Background(), client, d, "9", false, nil); err == nil {
		t.Fatal("oversized response accepted")
	}
}

func TestVideoDownloadBoundaries(t *testing.T) {
	d := Deployment{Origin: "https://ifeng-api01.hfjtqy.com", ProtocolVersion: "2.8.3"}
	mp4 := []byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00isommp42")
	body, status, encoding := mp4, 200, ""
	count := 0
	client := &http.Client{Transport: mediaTransport(func(r *http.Request) (*http.Response, error) {
		count++
		if r.URL.String() != "https://images-03003.hfjtqy.com/assets/9" ||
			r.Header.Get("Cookie") != "" || r.Header.Get("Authorization") != "" {
			t.Fatal("video credentials or source changed")
		}
		return &http.Response{StatusCode: status,
			Header: http.Header{"Content-Encoding": []string{encoding}, "Location": []string{"https://127.0.0.1/"}},
			Body:   io.NopCloser(bytes.NewReader(body)), Request: r}, nil
	})}
	expected := &nativewire.MessageVideo{Asset: "9", Size: uint64(len(mp4))}
	file, err := FetchVideo(context.Background(), client, d, expected)
	if err != nil || file.MIME != "video/mp4" || !bytes.Equal(file.Data, mp4) {
		t.Fatal("reviewed video container rejected", err)
	}
	for _, content := range [][]byte{[]byte("<html>active</html>"), []byte("<svg/>"), []byte("not a video")} {
		body = content
		expected.Size = 0
		if _, err = FetchVideo(context.Background(), client, d, expected); err == nil {
			t.Fatal("invalid video container accepted")
		}
	}
	body, expected.Size = mp4, 1
	if _, err = FetchVideo(context.Background(), client, d, expected); err == nil {
		t.Fatal("video size mismatch admitted")
	}
	expected.Size = nativewire.MaxVideoBytes + 1
	start := count
	if _, err = FetchVideo(context.Background(), client, d, expected); err == nil || count != start {
		t.Fatal("oversized declared video reached upstream")
	}
	expected.Size = 0
	body = make([]byte, nativewire.MaxVideoBytes+1)
	if _, err = FetchVideo(context.Background(), client, d, expected); err == nil {
		t.Fatal("oversized video response admitted")
	}
	body, encoding = mp4, "gzip"
	if _, err = FetchVideo(context.Background(), client, d, expected); err == nil {
		t.Fatal("compressed video admitted")
	}
	encoding, status = "", 302
	start = count
	if _, err = FetchVideo(context.Background(), client, d, expected); err == nil || count != start+1 {
		t.Fatal("video redirect followed")
	}
	expected.Asset = "https://127.0.0.1/a"
	start = count
	if _, err = FetchVideo(context.Background(), client, d, expected); err == nil || count != start {
		t.Fatal("external video reached upstream")
	}
}
