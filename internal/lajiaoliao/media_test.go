package lajiaoliao

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func syntheticMedia(kind string) MediaAsset {
	out := MediaAsset{ID: "synthetic-upload", Kind: kind, URL: "https://lj-im-chat-01.lajiaoliao.com/media",
		Size: 1024, Duration: 1}
	if kind == "video" {
		out.SnapshotURL, out.SnapshotSize, out.Width, out.Height = out.URL+"-cover", 128, 64, 32
	}
	return out
}

func TestMediaReceiptPlatformAndOrigin(t *testing.T) {
	for _, kind := range []string{"voice", "video"} {
		asset := syntheticMedia(kind)
		raw, contentType, err := asset.content()
		if err != nil || contentType != map[string]int32{"voice": 103, "video": 104}[kind] {
			t.Fatal("native content type changed", kind, err)
		}
		var body map[string]any
		if json.Unmarshal(raw, &body) != nil || bytes.Contains(raw, []byte("nonce")) || bytes.Contains(raw, []byte("aad")) {
			t.Fatal("cross-platform payload", string(raw))
		}
		for _, address := range []string{"https://example.com/media", "https://lj-im-chat-01.lajiaoliao.com.evil/media",
			"https://lj-im-chat-01.lajiaoliao.com:443/media", "http://lj-im-chat-01.lajiaoliao.com/media",
			"https://secret@lj-im-chat-01.lajiaoliao.com/media", "https://127.0.0.1/media"} {
			asset.URL = address
			if asset.Validate() == nil {
				t.Fatal("unverified resource accepted", address)
			}
		}
	}
	if syntheticMedia("file").Validate() == nil || syntheticMedia("image").Validate() == nil {
		t.Fatal("unimplemented media admitted")
	}
}

func TestMultipartOneAttemptAndTokenIsolation(t *testing.T) {
	for _, unknown := range []bool{false, true} {
		t.Run(map[bool]string{false: "complete", true: "unknown"}[unknown], func(t *testing.T) {
			calls, puts, guards := 0, 0, 0
			c := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				calls++
				if r.Method == http.MethodPut {
					puts++
					if r.URL.Host != "lj-im-chat-01.lajiaoliao.com" || r.Header.Get("token") != "" ||
						r.Header.Get("deviceID") != "" || r.GetBody != nil {
						t.Fatal("business identity or retry body reached signed resource")
					}
					data, _ := io.ReadAll(r.Body)
					if len(data) != 4 {
						t.Fatal("unexpected part", len(data))
					}
					if unknown {
						return nil, errors.New("synthetic lost response")
					}
					return response(""), nil
				}
				if r.URL.Host != "im-api.lajiaoliao.com" || r.Header.Get("token") != "synthetic-im" {
					t.Fatal("wrong multipart API")
				}
				switch r.URL.Path {
				case "/object/part_limit":
					return response(`{"errCode":0,"data":{"minPartSize":4,"maxPartSize":4,"maxNumSize":4}}`), nil
				case "/object/initiate_multipart_upload":
					if guards != 1 {
						t.Fatal("missing durable admission")
					}
					var in map[string]any
					json.NewDecoder(r.Body).Decode(&in)
					if in["cause"] != "msg-voice" || in["size"] != float64(8) || in["partSize"] != float64(4) ||
						in["name"] != "self/msg_voice_synthetic-upload.mp3" || in["contentType"] != "audio/mpeg" {
						t.Fatal("multipart contract", in)
					}
					return response(`{"errCode":0,"data":{"upload":{"uploadID":"upload","partSize":4,"sign":{
						"url":"https://lj-im-chat-01.lajiaoliao.com/part","query":[{"key":"signature","values":["synthetic"]}],
						"parts":[{"partNumber":1},{"partNumber":2}]}}}}`), nil
				case "/object/complete_multipart_upload":
					if puts != 2 {
						t.Fatal("completed before parts")
					}
					return response(`{"errCode":0,"data":{"url":"https://lj-im-chat-01.lajiaoliao.com/result"}}`), nil
				default:
					t.Fatal("unexpected route", r.URL.Path)
					return nil, nil
				}
			})}}
			out, err := c.UploadMedia(context.Background(), Session{UserID: "self", IMToken: "synthetic-im"},
				"synthetic-upload", MediaUpload{Kind: "voice", Data: []byte("12345678"), Duration: 1},
				func(context.Context) error { guards++; return nil })
			if guards != 1 {
				t.Fatal("repeated admission")
			}
			if unknown {
				var failure *Failure
				if !errors.As(err, &failure) || !failure.Submitted || failure.Code != "lajiaoliao_result_unknown" ||
					puts != 1 || calls != 3 {
					t.Fatal("unknown multipart retried", err, calls, puts)
				}
			} else if err != nil || out.Validate() != nil || calls != 5 {
				t.Fatal("upload", out, err, calls)
			}
		})
	}
}

func TestMediaWriteFenceAndUnknownReceipt(t *testing.T) {
	for _, kind := range []string{"voice", "video"} {
		t.Run(kind, func(t *testing.T) {
			var sends atomic.Int64
			client := mockTransport(t, func(in wireRequest) *wireResponse {
				if in.ReqIdentifier == 1001 {
					raw, _ := proto.Marshal(&sdkws.GetMaxSeqResp{})
					return &wireResponse{Data: raw}
				}
				sends.Add(1)
				var m sdkws.MsgData
				if proto.Unmarshal(in.Data, &m) != nil || m.SendID != "synthetic" || m.GroupID != "string-group" ||
					m.RecvID != m.GroupID || m.ClientMsgID != "string-message" || m.ContentType != map[string]int32{"voice": 103, "video": 104}[kind] ||
					m.MsgFrom != 100 || m.SessionType != 3 || strings.Contains(string(m.Content), "synthetic-im") {
					t.Error("native routing or media changed")
				}
				return nil
			})
			ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
			defer cancel()
			asset := syntheticMedia(kind)
			write := func(ctx context.Context, send func(context.Context) error) error { return send(ctx) }
			if _, err := client.SendMedia(ctx, "string-group", "string-message", asset, write); code(err) != "lajiaoliao_connection_unverified" {
				t.Fatal("unverified send", err)
			}
			if _, err := client.Sequences(ctx); err != nil {
				t.Fatal(err)
			}
			_, err := client.SendMedia(ctx, "string-group", "string-message", asset, func(context.Context, func(context.Context) error) error { return context.Canceled })
			if !errors.Is(err, context.Canceled) || sends.Load() != 0 {
				t.Fatal("failed fence wrote", err)
			}
			_, err = client.SendMedia(ctx, "string-group", "string-message", asset, write)
			if code(err) != "lajiaoliao_result_unknown" || sends.Load() != 1 {
				t.Fatal("unknown send repeated", err, sends.Load())
			}
		})
	}
}

func TestMediaReceiptStringIDs(t *testing.T) {
	raw := protowire.AppendString(protowire.AppendTag(nil, 1, protowire.BytesType), "server-not-a-number")
	raw = protowire.AppendString(protowire.AppendTag(raw, 2, protowire.BytesType), "client-not-a-number")
	raw = protowire.AppendVarint(protowire.AppendTag(raw, 3, protowire.VarintType), 1700000000000)
	out, err := decodeReceipt(raw)
	if err != nil || out.ServerID != "server-not-a-number" {
		t.Fatal("string receipt lost", out, err)
	}
}

func TestVideoCoverFormatAndUploadCancellation(t *testing.T) {
	var cover, other bytes.Buffer
	frame := image.NewRGBA(image.Rect(0, 0, 64, 32))
	jpeg.Encode(&cover, frame, nil)
	png.Encode(&other, frame)
	if w, h, err := SnapshotDimensions(cover.Bytes()); err != nil || w != 64 || h != 32 {
		t.Fatal("JPEG cover metadata", w, h, err)
	}
	for _, invalid := range [][]byte{other.Bytes(), cover.Bytes()[:16], make([]byte, (1<<20)+1)} {
		if _, _, err := SnapshotDimensions(invalid); err == nil {
			t.Fatal("unsupported cover accepted")
		}
	}
	for _, cancelled := range []bool{false, true} {
		t.Run(map[bool]string{false: "video", true: "cancel_after_cover"}[cancelled], func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			guards, inits := 0, 0
			c := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path == "/object/part_limit" {
					return response(`{"errCode":0,"data":{"minPartSize":1048576,"maxPartSize":16777216,"maxNumSize":32}}`), nil
				}
				if r.URL.Path != "/object/initiate_multipart_upload" {
					t.Fatal("unexpected media route", r.URL.Path)
				}
				inits++
				var body struct{ Cause, ContentType string }
				if json.NewDecoder(r.Body).Decode(&body) != nil || guards != 1 {
					t.Fatal("missing admission")
				}
				if inits == 1 {
					if body.Cause != "msg-video-snapshot" || body.ContentType != "image/jpeg" {
						t.Fatal("cover contract changed")
					}
					if cancelled {
						cancel()
					}
				} else if inits != 2 || body.Cause != "msg-video" || body.ContentType != "video/mp4" {
					t.Fatal("video contract changed")
				}
				return response(`{"errCode":0,"data":{"url":"https://lj-im-chat-01.lajiaoliao.com/deduplicated"}}`), nil
			})}}
			out, err := c.UploadMedia(ctx, Session{UserID: "self", IMToken: "synthetic"}, "upload",
				MediaUpload{Kind: "video", Data: []byte("synthetic"), Thumbnail: cover.Bytes(), Width: 64, Height: 32, Duration: 2},
				func(context.Context) error { guards++; return nil })
			if cancelled {
				if err == nil || inits != 1 || guards != 1 {
					t.Fatal("cancelled video proceeded or retried", err, inits, guards)
				}
			} else if err != nil || out.Validate() != nil || inits != 2 || guards != 1 {
				t.Fatal("video upload", out, err, inits, guards)
			}
		})
	}
}
