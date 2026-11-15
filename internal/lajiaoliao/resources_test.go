package lajiaoliao

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net/http"
	"strings"
	"testing"
)

type resourceTransport func(*http.Request) (*http.Response, error)

func (f resourceTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestChiliResourceContractNestedAndCredentialFreeProjection(t *testing.T) {
	raw := messageFixture(t, 107, `{"title":"history","abstractList":[],"multiMessage":[`+
		`{"contentType":114,"quoteElem":{"text":"reply","quoteMessage":{"contentType":102,"pictureElem":`+
		`{"sourcePicture":{"url":"https://lj-im-chat-01.lajiaoliao.com/private?signature=hidden","size":80,"width":2,"height":3}}}}},`+
		`{"contentType":108,"cardElem":{"userID":"string-person","nickname":"<b>name</b>","faceURL":`+
		`"https://lj-im-chat-01.lajiaoliao.com/avatar","ex":"opaque"}}]}`)
	message, err := ReadMessageView(raw)
	clear(raw)
	if err != nil || message.View.Items[1].Contact.ID != "string-person" {
		t.Fatal("pinned card contract rejected", err)
	}
	resource, ok := message.Resource("node-2-image")
	if !ok || resource.Size != 80 || !strings.Contains(resource.URL, "hidden") {
		t.Fatal("nested resource not bound to preorder", resource, ok)
	}
	if resource, ok = message.Resource("node-3-avatar"); !ok || resource.Kind != "avatar" {
		t.Fatal("card avatar absent", resource, ok)
	}
	for _, part := range []string{"node-02-image", "node-3-image", "node-32-image", "node--1-image", "https://example.com"} {
		if _, ok := message.Resource(part); ok {
			t.Fatal("unbound selector accepted", part)
		}
	}
	encoded, _ := json.Marshal(message.View)
	for _, private := range []string{"https://", "signature", "hidden", "opaque", "Attachments"} {
		if bytes.Contains(encoded, []byte(private)) {
			t.Fatal("upstream resource leaked", string(encoded))
		}
	}
	if message.View.Items[1].Contact.Name != "<b>name</b>" {
		t.Fatal("display text changed")
	}
}

func TestChiliMediaParsingDoesNotFetchOrChangeUnsupportedPolicy(t *testing.T) {
	for _, test := range []struct {
		kind              int32
		content, selector string
	}{
		{102, `{"sourcePicture":{"url":"https://lj-im-chat-01.lajiaoliao.com/a","url":"https://evil.invalid/a"}}`, "node-0-image"},
		{102, `{"sourcePicture":{"url":"file:///local"}}`, "node-0-image"},
		{102, `{"sourcePicture":{"url":"https://lj-im-chat-01.lajiaoliao.com/a","size":5242881}}`, "node-0-image"},
		{104, `{"videoUrl":"https://lj-im-chat-01.lajiaoliao.com/a","videoSize":16777217,"duration":2}`, "node-0-video"},
	} {
		message, err := ReadMessageView(messageFixture(t, test.kind, test.content))
		if err != nil || message == nil || message.View.Kind == "unsupported" {
			t.Fatal("unavailable media blocked type display", err)
		}
		if _, ok := message.Resource(test.selector); ok {
			t.Fatal("unreviewed media enabled")
		}
	}
	message, err := ReadMessageView(messageFixture(t, 104, `{"videoUrl":"https://lj-im-chat-01.lajiaoliao.com/video",`+
		`"videoSize":24,"duration":2,"snapshotUrl":"https://lj-im-chat-01.lajiaoliao.com/cover","snapshotSize":80,"snapshotWidth":2,"snapshotHeight":3}`))
	if err != nil || message.View.Duration != 2 || message.View.Size != 24 {
		t.Fatal("video metadata lost", err)
	}
	for _, part := range []string{"node-0-video", "node-0-poster"} {
		if _, ok := message.Resource(part); !ok {
			t.Fatal("reviewed resource rejected", part)
		}
	}
}

func TestChiliNestedOfficialImageOriginDoesNotAdmitVideoOrOtherHosts(t *testing.T) {
	message, err := ReadMessageView(messageFixture(t, 114,
		`{"text":"reply","quoteMessage":{"contentType":102,"pictureElem":`+
			`{"sourcePicture":{"url":"https://im-api.lajiaoliao.com/synthetic.png","size":745,"width":64,"height":56}}}}`))
	if err != nil {
		t.Fatal(err)
	}
	resource, ok := message.Resource("node-1-image")
	if !ok || resource.Width != 64 || resource.Height != 56 {
		t.Fatal("observed official nested image rejected")
	}
	resource.Kind = "video"
	if resource.Valid() {
		t.Fatal("image origin admitted video")
	}
	resource.Kind, resource.URL = "image", "https://im-api.lajiaoliao.com.evil/synthetic.png"
	if resource.Valid() {
		t.Fatal("suffix host admitted")
	}
}

func TestChiliFetchResourceBoundsAndNoRedirect(t *testing.T) {
	var buffer bytes.Buffer
	png.Encode(&buffer, image.NewRGBA(image.Rect(0, 0, 2, 3)))
	resource := Resource{URL: "https://lj-im-chat-01.lajiaoliao.com/synthetic", Kind: "image",
		Width: 2, Height: 3, Size: int64(buffer.Len())}
	payload, status, calls := buffer.Bytes(), 200, 0
	client := &http.Client{Transport: resourceTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Header.Get("Cookie") != "" || r.Header.Get("token") != "" ||
			r.Header.Get("Authorization") != "" || r.Method != http.MethodGet {
			t.Error("credentials or mutation reached the resource host")
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"https://api.lajiaoliao.com/private"}},
			Body: io.NopCloser(bytes.NewReader(payload)), Request: r}, nil
	})}
	file, err := FetchResource(context.Background(), client, resource)
	if err != nil || file.MIME != "image/png" || !bytes.Equal(file.Data, payload) {
		t.Fatal("pinned image rejected", err)
	}
	status = 302
	if _, err = FetchResource(context.Background(), client, resource); err == nil || calls != 2 {
		t.Fatal("redirect followed", calls, err)
	}
	status = 200
	for _, address := range []string{"https://127.0.0.1/a", "file:///a", "http://lj-im-chat-01.lajiaoliao.com/a",
		"https://lj-im-chat-01.lajiaoliao.com:443/a", "https://secret@lj-im-chat-01.lajiaoliao.com/a",
		"https://lj-im-chat-01.lajiaoliao.com.evil/a", "https://lj-im-chat-01.lajiaoliao.com/a#fragment"} {
		copy := resource
		copy.URL = address
		if _, err = FetchResource(context.Background(), client, copy); err == nil || calls != 2 {
			t.Fatal("invalid reference reached transport", address)
		}
	}
	status = 404
	if _, err = FetchResource(context.Background(), client, resource); code(err) != "lajiaoliao_media_expired" {
		t.Fatal("missing upstream resource misclassified", err)
	}
	status = 403
	if _, err = FetchResource(context.Background(), client, resource); code(err) != "lajiaoliao_media_access_denied" {
		t.Fatal("denied upstream resource misclassified", err)
	}
	status = 200
	copy := resource
	copy.Width = 3
	if _, err = FetchResource(context.Background(), client, copy); err == nil {
		t.Fatal("image dimensions not checked")
	}
	payload = []byte("<html>not media</html>")
	if _, err = FetchResource(context.Background(), client, resource); err == nil {
		t.Fatal("non-media accepted")
	}
	payload = []byte("\x00\x00\x00\x18ftypisom\x00\x00\x00\x00isommp42")
	resource.Kind, resource.Size = "video", int64(len(payload))
	if file, err = FetchResource(context.Background(), client, resource); err != nil || file.MIME != "video/mp4" {
		t.Fatal("video container rejected", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err = FetchResource(ctx, ResourceHTTP(), resource); err == nil {
		t.Fatal("cancelled request submitted")
	}
}

func TestChiliOfficialObjectRedirectIsOneBoundedCredentialFreeHop(t *testing.T) {
	var imageBytes bytes.Buffer
	png.Encode(&imageBytes, image.NewRGBA(image.Rect(0, 0, 2, 3)))
	for _, target := range []string{
		"https://lj-chat-im-01.lajiaoliao.com/synthetic",
		"https://lj-chat-im-01.lajiaoliao.com.evil/synthetic",
		"https://lj-chat-im-01.lajiaoliao.com:443/synthetic",
		"https://secret@lj-chat-im-01.lajiaoliao.com/synthetic",
		"http://lj-chat-im-01.lajiaoliao.com/synthetic",
		"https://api.lajiaoliao.com/private",
	} {
		t.Run(target, func(t *testing.T) {
			calls := 0
			client := &http.Client{Transport: resourceTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method != "GET" || r.Header.Get("token") != "" || r.Header.Get("Authorization") != "" ||
					r.Header.Get("Cookie") != "" {
					t.Fatal("redirect carried native credentials or mutation")
				}
				if calls == 1 {
					return &http.Response{StatusCode: 302, Header: http.Header{"Location": []string{target}},
						Body: io.NopCloser(strings.NewReader("")), Request: r}, nil
				}
				if calls > 2 || r.URL.Host != objectReadHost {
					t.Fatal("unbounded or unverified redirect")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{},
					Body: io.NopCloser(bytes.NewReader(imageBytes.Bytes())), Request: r}, nil
			})}
			file, err := FetchResource(context.Background(), client,
				Resource{URL: "https://im-api.lajiaoliao.com/object/synthetic", Kind: "avatar"})
			if target == "https://lj-chat-im-01.lajiaoliao.com/synthetic" {
				if err != nil || calls != 2 || file.MIME != "image/png" {
					t.Fatal("reviewed redirect failed", calls, err)
				}
			} else if err == nil || calls != 1 {
				t.Fatal("unreviewed redirect followed", calls, err)
			}
		})
	}
	calls := 0
	loop := &http.Client{Transport: resourceTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		return &http.Response{StatusCode: 302,
			Header: http.Header{"Location": []string{"https://lj-chat-im-01.lajiaoliao.com/again"}},
			Body:   io.NopCloser(strings.NewReader("")), Request: r}, nil
	})}
	if _, err := FetchResource(context.Background(), loop,
		Resource{URL: "https://im-api.lajiaoliao.com/object/synthetic", Kind: "image"}); err == nil || calls != 2 {
		t.Fatal("second redirect followed", calls, err)
	}
	if !(Resource{URL: "https://im-api.lajiaoliao.com/object/synthetic", Kind: "video"}).Valid() ||
		(Resource{URL: "https://im-api.lajiaoliao.com/private", Kind: "video"}).Valid() {
		t.Fatal("video object route widened to arbitrary IM routes")
	}
}
