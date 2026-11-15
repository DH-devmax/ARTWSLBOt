package lajiaoliao

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/jpeg"
	"io"
	"net/http"
	"testing"
)

func TestAvatarUploadDedupAndFinalFence(t *testing.T) {
	var data bytes.Buffer
	if err := jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 256, 256)), nil); err != nil {
		t.Fatal(err)
	}
	for _, denyFinal := range []bool{false, true} {
		t.Run(map[bool]string{false: "success", true: "revoked"}[denyFinal], func(t *testing.T) {
			calls, fences, updated := 0, 0, false
			target := "https://lj-im-chat-01.lajiaoliao.com/avatar.jpg"
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				calls++
				switch r.URL.Path {
				case "/object/part_limit":
					return map[string]any{"minPartSize": 1 << 20, "maxPartSize": 5 << 20, "maxNumSize": 100}, nil
				case "/object/initiate_multipart_upload":
					if r.Header.Get("token") != "im-fixture" || body["contentType"] != "image/jpeg" ||
						body["name"] != "self/avatar_operation.jpg" || body["maxParts"] != float64(1) {
						t.Fatal("wrong upload contract")
					}
					return map[string]any{"url": target}, nil
				case "/user/update":
					updated = true
					if r.Header.Get("token") != "chat-fixture" || r.URL.Host != "api.lajiaoliao.com" ||
						body["userID"] != "self" || body["faceURL"] != target || len(body) != 2 {
						t.Fatal("avatar update changed unrelated settings")
					}
					return struct{}{}, nil
				}
				t.Fatal("unexpected route")
				return nil, nil
			})
			var receipt string
			err := client.ChangeAvatar(context.Background(), Session{UserID: "self", DeviceID: "device-fixture",
				IMToken: "im-fixture", ChatToken: "chat-fixture"}, "operation", data.Bytes(),
				func(ctx context.Context, send func(context.Context) error) error {
					fences++
					if denyFinal && fences == 2 {
						return errors.New("revoked")
					}
					return send(ctx)
				}, func(url string) { receipt = url })
			if receipt != target || fences != 2 || updated == denyFinal || (err != nil) != denyFinal || calls != 3-boolInt(denyFinal) {
				t.Fatal("upload/final fence was not one shot", calls, fences, updated, err)
			}
		})
	}
}

func boolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func TestAvatarSignedUploadHasNoCredentialsAndUnknownDoesNotRetry(t *testing.T) {
	var data bytes.Buffer
	jpeg.Encode(&data, image.NewRGBA(image.Rect(0, 0, 256, 256)), nil)
	for _, uncertain := range []bool{false, true} {
		step := 0
		client := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
			step++
			switch step {
			case 1:
				return response(`{"errCode":0,"data":{"minPartSize":1048576,"maxPartSize":5242880,"maxNumSize":100}}`), nil
			case 2:
				return response(`{"errCode":0,"data":{"upload":{"uploadID":"upload","partSize":1048576,"sign":{"url":"https://lj-chat-im-01-hz-acc.lajiaoliao.com/part","parts":[{"partNumber":1}]}}}}`), nil
			case 3:
				body, _ := io.ReadAll(r.Body)
				if r.Method != "PUT" || r.URL.Host != objectStorageHost || !bytes.Equal(body, data.Bytes()) || r.GetBody != nil ||
					r.Header.Get("token") != "" || r.Header.Get("Authorization") != "" || r.Header.Get("Cookie") != "" {
					t.Fatal("signed upload leaked credentials or became replayable")
				}
				if uncertain {
					return nil, errors.New("connection lost")
				}
				return response(""), nil
			case 4:
				return response(`{"errCode":0,"data":{"url":"https://lj-im-chat-01.lajiaoliao.com/avatar.jpg"}}`), nil
			case 5:
				return response(`{"errCode":0,"data":{}}`), nil
			}
			t.Fatal("upload automatically retried")
			return nil, nil
		})}}
		fences := 0
		err := client.ChangeAvatar(context.Background(), Session{UserID: "self", IMToken: "im", ChatToken: "chat"},
			"operation", data.Bytes(), func(ctx context.Context, send func(context.Context) error) error {
				fences++
				return send(ctx)
			}, func(string) {})
		if uncertain && (step != 3 || fences != 2 || err == nil) ||
			!uncertain && (step != 5 || fences != 3 || err != nil) {
			t.Fatal("multipart outcome changed", step, fences, err)
		}
	}
	for _, sign := range []avatarSign{
		{URL: "https://127.0.0.1/part", Parts: []avatarPart{{Number: 1}}},
		{URL: "https://lj-im-chat-01.lajiaoliao.com/part", Parts: []avatarPart{{Number: 1}},
			Header: []avatarValues{{Key: "Authorization", Values: []string{"secret"}}}},
		{URL: "https://lj-im-chat-01.lajiaoliao.com/part", Parts: []avatarPart{{Number: 2}}},
	} {
		if _, _, err := sign.request(); err == nil {
			t.Fatal("unverified signed upload accepted")
		}
	}
}

func TestFixedObjectStorageOriginIsNarrow(t *testing.T) {
	valid := "https://" + objectStorageHost + "/synthetic"
	if !avatarURL(valid) || !mediaURL(valid) {
		t.Fatal("verified multipart storage origin rejected")
	}
	for _, kind := range []string{"avatar", "image", "video"} {
		if !(Resource{URL: valid, Kind: kind}).Valid() {
			t.Fatal("retained resource at verified storage origin rejected", kind)
		}
	}
	for _, address := range []string{"http://" + objectStorageHost + "/x",
		"https://" + objectStorageHost + ":443/x", "https://" + objectStorageHost + ".evil/x",
		"https://secret@" + objectStorageHost + "/x", "https://" + objectStorageHost + "/x#fragment"} {
		if avatarURL(address) || mediaURL(address) || (Resource{URL: address, Kind: "image"}).Valid() {
			t.Fatal("unverified storage address accepted")
		}
	}
}

func TestAvatarFixedIMOriginAndSignedIdentityIsolation(t *testing.T) {
	target, headers, err := (avatarSign{
		URL:   "https://im-api.lajiaoliao.com/object/part_upload",
		Query: []avatarValues{{Key: "signature", Values: []string{"synthetic"}}},
		Parts: []avatarPart{{Number: 1}},
	}).request()
	if err != nil || target != "https://im-api.lajiaoliao.com/object/part_upload?signature=synthetic" ||
		len(headers) != 0 || !avatarURL("https://im-api.lajiaoliao.com/object/avatar.jpg") {
		t.Fatal("fixed IM upload contract rejected", err)
	}
	for _, address := range []string{"https://im-api.lajiaoliao.com.evil/object",
		"https://im-api.lajiaoliao.com:443/object", "http://im-api.lajiaoliao.com/object",
		"https://secret@im-api.lajiaoliao.com/object", "https://127.0.0.1/object",
		"https://im-api.lajiaoliao.com/object#fragment"} {
		if avatarURL(address) {
			t.Fatal("unverified upload origin accepted")
		}
	}
}
