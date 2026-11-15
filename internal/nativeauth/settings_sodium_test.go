//go:build sodium && cgo

package nativeauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"wangshangliao/internal/nativewire"
)

func TestDesktopSettingsFenceSignedSingleSubmission(t *testing.T) {
	base, err := NewClient(syntheticDeployment())
	if err != nil {
		t.Fatal(err)
	}
	client := base.(*Client)
	defer client.Close()
	client.jwt, client.fields[3] = "synthetic-settings-jwt", 7
	calls, writes := 0, 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, params := desktopRequest(t, r, client.bodyKey)
		calls++
		data := map[string]any{}
		switch r.URL.Path {
		case "/v1/settings/query-app-settings":
			if string(params["v"]) != `"0"` {
				t.Error("setting read used an incremental version without a baseline")
			}
			data = map[string]any{"uid": 7, "nickName": "synthetic", "realName": "excluded"}
		case "/v1/group/set-member-nickname":
			if string(params["groupId"]) != "123" || string(params["userId"]) != "7" || string(params["nick"]) != `"new-card"` {
				t.Error("wrong group/self nickname desktop contract")
			}
			writes++
		default:
			t.Error("unreviewed route", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer server.Close()
	client.config.Origin, client.http = server.URL, server.Client()
	view, err := client.Settings(context.Background())
	if err != nil || view.UID != "7" || view.Nickname != "synthetic" {
		t.Fatal(view, err)
	}
	change := SettingChange{Kind: "member_nickname", Group: "123", Target: "7", Text: "new-card"}
	if _, err = client.Request(context.Background(), "/v1/group/set-member-nickname", map[string]any{}); err == nil {
		t.Fatal("raw Request bypassed the final fence")
	}
	denied := errors.New("synthetic revoked")
	if err = client.ChangeSetting(context.Background(), change, func(context.Context, func(context.Context) error) error { return denied }); !errors.Is(err, denied) {
		t.Fatal("pre-submit rejection became an unknown network result", err)
	}
	if calls != 1 {
		t.Fatal("denied fence reached native HTTP")
	}
	if err = client.ChangeSetting(context.Background(), change, nativewire.SubmissionFence(
		func(ctx context.Context, submit func(context.Context) error) error { return submit(ctx) })); err != nil {
		t.Fatal("reviewed desktop nickname submission", err)
	}
	if calls != 2 || writes != 1 {
		t.Fatal("unexpected native retries", calls, writes)
	}
}
