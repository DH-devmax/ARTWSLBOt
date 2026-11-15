package lajiaoliao

import (
	"context"
	"net/http"
	"testing"
)

func TestBasicProfileDoesNotFetchUnrelatedSettings(t *testing.T) {
	calls := 0
	client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
		calls++
		if r.URL.Path != "/user/get_users_info" {
			t.Fatal("basic profile fetched unrelated settings", r.URL.Path)
		}
		return map[string]any{"usersInfo": []any{map[string]any{
			"userID": "self", "nickname": "robot", "faceURL": "avatar",
			"ex": "invalid irrelevant JSON", "gender": "malformed", "phoneNumber": "private",
		}}}, nil
	})
	got, err := client.BasicProfile(context.Background(), Session{UserID: "self", IMToken: "synthetic", DeviceID: "device-fixture"})
	if err != nil || calls != 1 || got.ID != "self" || got.Name != "robot" || got.Avatar != "avatar" ||
		got.Details != nil || got.MaskedPhone != "" || got.SearchPhone != nil {
		t.Fatal("basic profile added unrelated dependencies", got, err)
	}
}

func TestBasicProfileRejectsWrongSelfAndUnverifiedResponseShape(t *testing.T) {
	for _, body := range []map[string]any{
		{"users": []any{map[string]any{"userID": "self"}}},
		{"usersInfo": []any{map[string]any{"userID": "other"}}},
		{"usersInfo": []any{map[string]any{"userID": "self"}, map[string]any{"userID": "other"}}},
	} {
		client := nativeFixture(t, func(*http.Request, map[string]any) (any, error) { return body, nil })
		if _, err := client.BasicProfile(context.Background(), Session{UserID: "self", DeviceID: "device-fixture"}); err == nil {
			t.Fatal("profile accepted an unverified response or another identity")
		}
	}
}
