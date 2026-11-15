//go:build sodium && cgo

package nativeauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMembersSignedDesktopRequestAndJWTContinuity(t *testing.T) {
	base, err := NewClient(syntheticDeployment())
	if err != nil {
		t.Fatal(err)
	}
	client := base.(*Client)
	defer client.Close()
	client.jwt, client.fields[3] = "synthetic-original-jwt", 7
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, params := desktopRequest(t, r, client.bodyKey)
		if r.URL.Path != "/v1/group/get-group-members" || string(params["groupId"]) != "123" || string(params["v"]) != `"0"` {
			t.Error("member read did not use the fixed desktop contract")
		}
		calls++
		entries := []map[string]any{{"userId": "7", "nimId": "me", "groupRole": "GROUP_ROLE_ADMIN"}}
		data := map[string]any{"groupMemberInfo": entries}
		if calls == 1 {
			if r.Header.Get("x-jwt") != "synthetic-original-jwt" {
				t.Error("session missing")
			}
			w.Header().Set("x-set-jwt", "synthetic-rotated-jwt")
			data["nextCursor"] = "second-page"
		} else {
			// Only the reviewed security-verification route rotates JWT.
			if r.Header.Get("x-jwt") != "synthetic-original-jwt" || string(params["cursor"]) != `"second-page"` {
				t.Error("read changed session identity or lost cursor")
			}
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer server.Close()
	client.config.Origin, client.http = server.URL, server.Client()
	roster, err := client.Members(context.Background(), "123")
	if err != nil || roster.Pages != 2 || !roster.Complete || len(roster.Members) != 1 || calls != 2 {
		t.Fatal("signed member read", err)
	}
	if client.jwt != "synthetic-original-jwt" {
		t.Fatal("unreviewed read rotated the session")
	}
	for _, route := range []string{"/v1/group/set-member-mute", "/v1/group/remove-group-member", "/v1/group/get-group-members/../set-group-mute"} {
		if _, err := client.Request(context.Background(), route, map[string]any{}); err == nil {
			t.Fatal("unreviewed native write admitted")
		}
	}
	if calls != 2 {
		t.Fatal("route guard made a native mutation request")
	}
}
