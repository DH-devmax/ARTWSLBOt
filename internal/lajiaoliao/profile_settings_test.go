package lajiaoliao

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"testing"
)

func TestSelfBusinessProfileProjectionAndSingleFieldWrites(t *testing.T) {
	s := Session{UserID: "self-native", IMToken: "im-fixture", ChatToken: "chat-fixture", DeviceID: "device-fixture"}
	writes := 0
	client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
		switch r.URL.Path {
		case "/user/get_users_info":
			return map[string]any{"usersInfo": []any{map[string]any{"userID": s.UserID, "nickname": "synthetic"}}}, nil
		case "/friend/privacy/get":
			return map[string]any{}, nil
		case "/user/find/full":
			if r.Header.Get("token") != s.ChatToken {
				t.Fatal("mixed self ticket")
			}
			return map[string]any{"users": []any{map[string]any{"userID": s.UserID, "phoneNumber": "13800000000",
				"areaCode": "+86", "gender": 0, "birth": 946656000000, "signature": "synthetic", "countryName": "CN",
				"password": "must-not-project", "email": "must-not-project"}}}, nil
		case "/push_config/get":
			return map[string]any{"showMessageDetail": false, "receiveNewMessage": true,
				"groupMessageNotification": false, "notificationSound": "normal", "vibration": true}, nil
		case "/user/update":
			if len(body) != 2 || body["userID"] != s.UserID || body["signature"] != "" {
				t.Fatal("single field write changed unrelated profile fields")
			}
			writes++
			return map[string]any{}, nil
		case "/push_config/set":
			if len(body) != 5 || body["notificationSound"] != "normal" || body["receiveNewMessage"] != false {
				t.Fatal("notification wire shape")
			}
			writes++
			return map[string]any{}, nil
		default:
			t.Fatal("unexpected self route", r.URL.Path)
			return nil, nil
		}
	})
	profile, err := client.FullProfile(context.Background(), s)
	if err != nil || profile.MaskedPhone != "+86 138****0000" || profile.Details.Gender == nil ||
		*profile.Details.Gender != 0 || profile.Details.Region != "CN" || !profile.Details.Push.Complete() {
		t.Fatal("self projection", err)
	}
	raw, _ := json.Marshal(profile.Details)
	if strings.Contains(string(raw), "13800000000") || strings.Contains(string(raw), "must-not-project") {
		t.Fatal("private business data projected")
	}
	fence := func(ctx context.Context, send func(context.Context) error) error { return send(ctx) }
	if err = client.ChangeProfile(context.Background(), s, Change{Kind: "profile_signature"}, fence); err != nil {
		t.Fatal(err)
	}
	push := `{"showMessageDetail":false,"receiveNewMessage":false,"groupMessageNotification":false,"notificationSound":"normal","vibration":true}`
	if err = client.ChangeProfile(context.Background(), s, Change{Kind: "push_notifications", Text: push}, fence); err != nil || writes != 2 {
		t.Fatal(err, writes)
	}
	if err = client.ChangeProfile(context.Background(), s, Change{Kind: "push_notifications", Text: strings.Replace(push, "normal", "unverified", 1)}, fence); err == nil || writes != 2 {
		t.Fatal("unverified sound submitted")
	}
	if err = client.ChangeProfile(context.Background(), s, Change{Kind: "profile_gender", Text: "1"}, func(context.Context, func(context.Context) error) error { return context.Canceled }); !errors.Is(err, context.Canceled) || writes != 2 {
		t.Fatal("fence ignored")
	}
	for _, change := range []Change{
		{Kind: "profile_gender", Text: "3"}, {Kind: "profile_birth", Text: "0"}, {Kind: "profile_signature", Text: strings.Repeat("x", 81)},
		{Kind: "push_notifications", Text: `{"receiveNewMessage":true}`},
		{Kind: "push_notifications", Text: strings.TrimSuffix(push, "}") + `,"route":"/arbitrary"}`},
		{Kind: "profile_signature", Text: "safe", Target: "other-native"},
	} {
		if change.Validate() == nil {
			t.Fatal("invalid profile admitted", change.Kind)
		}
	}
}
