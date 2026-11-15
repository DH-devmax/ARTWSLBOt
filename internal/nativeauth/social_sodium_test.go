//go:build sodium && cgo

package nativeauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"wangshangliao/internal/nativewire"
)

func TestDesktopSocialFixedContractsAndNoRetry(t *testing.T) {
	base, err := NewClient(syntheticDeployment())
	if err != nil {
		t.Fatal(err)
	}
	c := base.(*Client)
	defer c.Close()
	c.jwt, c.fields[3] = "synthetic-social-jwt", 7
	writes, reads := 0, 0
	badResponse := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, p := desktopRequest(t, r, c.bodyKey)
		data := map[string]any{}
		switch r.URL.Path {
		case "/v1/friend/friend-apply-list":
			reads++
			if string(p["lastId"]) != `"0"` {
				t.Fatal("invented request pagination")
			}
			data["list"] = []any{map[string]any{"flId": "9007199254740993", "direction": "FROM_HIM", "state": "FRIENDLOG_STATE_PROCESSING", "user": map[string]any{"uid": 8, "nickName": "peer"}}}
		case "/v1/friend/get-friend-list":
			reads++
			if string(p["v"]) != `"0"` {
				t.Fatal("used incremental version without a baseline")
			}
			data["friendList"] = []any{map[string]any{"uid": 8, "nickName": "peer", "state": "FRIEND_STATE_GOOD"}}
		case "/v1/group/get-apply-logs":
			reads++
			if string(p["lastId"]) != `"0"` || len(p) != 1 {
				t.Fatal("invitation log request departed from fixed desktop lastId contract")
			}
			data["lastId"], data["list"] = "0", []any{}
		case "/v1/friend/friend-apply-handler":
			writes++
			if string(p["flId"]) != `"9007199254740993"` || string(p["state"]) != `"FRIENDLOG_STATE_AGREE"` ||
				string(p["classId"]) != "0" || len(p) != 5 {
				t.Fatal("wrong friend approval payload")
			}
			data["flId"], data["state"] = "9007199254740993", "FRIENDLOG_STATE_AGREE"
			if badResponse {
				data["flId"] = "1"
			}
		case "/v1/group/add-group-member":
			writes++
			if string(p["groupId"]) != "123" || string(p["groupMemberIds"]) != "[8]" || len(p) != 2 {
				t.Fatal("invitation was not a single reviewed member")
			}
		case "/v1/group/leave-group":
			writes++
			if string(p["groupId"]) != "123" || len(p) != 1 {
				t.Fatal("leave was not distinct from disband/kick")
			}
		default:
			t.Error("unreviewed native social route", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": data})
	}))
	defer server.Close()
	c.config.Origin, c.http = server.URL, server.Client()
	apps, err := c.FriendApplications(context.Background())
	if err != nil || len(apps) != 1 {
		t.Fatal(apps, err)
	}
	if _, err = c.Friends(context.Background()); err != nil {
		t.Fatal(err)
	}
	if records, cursor, err := c.InvitationRecords(context.Background()); err != nil || len(records) != 0 || cursor != "0" {
		t.Fatal(records, cursor, err)
	}
	for _, route := range []string{"/v1/friend/friend-apply-handler", "/v1/group/add-group-member", "/v1/group/leave-group"} {
		if _, err := c.Request(context.Background(), route, map[string]any{}); err == nil {
			t.Fatal("raw request bypassed final fence", route)
		}
	}
	approve := SettingChange{Kind: "friend_approve", Target: "8", Text: apps[0].ID}
	denied := errors.New("synthetic-revoked")
	if err := c.ChangeSocial(context.Background(), approve, &apps[0],
		func(context.Context, func(context.Context) error) error { return denied }); !errors.Is(err, denied) || writes != 0 {
		t.Fatal("revoked final fence reached native HTTP", err)
	}
	fence := func(ctx context.Context, submit func(context.Context) error) error { return submit(ctx) }
	for _, change := range []SettingChange{approve, {Kind: "group_invite", Group: "123", Target: "8"}, {Kind: "group_leave", Group: "123"}} {
		var app *FriendApplication
		if change.Kind == "friend_approve" {
			app = &apps[0]
		}
		if err := c.ChangeSocial(context.Background(), change, app, fence); err != nil {
			t.Fatal(change, err)
		}
	}
	badResponse = true
	var native *Error
	if err = c.ChangeSocial(context.Background(), approve, &apps[0], fence); !errors.As(err, &native) || native.Code != "native_response_invalid" || !native.Submitted {
		t.Fatal("ambiguous friend response did not remain unknown", err)
	}
	if writes != 4 || reads != 3 {
		t.Fatal("unexpected native retries", writes, reads)
	}
}

func TestInvitationWindowResponseBoundDoesNotWidenOtherRoutes(t *testing.T) {
	base, err := NewClient(syntheticDeployment())
	if err != nil {
		t.Fatal(err)
	}
	c := base.(*Client)
	defer c.Close()
	c.jwt, c.fields[3] = "synthetic-history-jwt", 7
	rows := make([]map[string]any, 2798)
	for i := range rows {
		rows[i] = map[string]any{"applyId": strconv.Itoa(i + 1), "groupId": 123, "groupApplyType": "INVITATION",
			"state": "MEMBER_STATE_INVITED", "applicant": map[string]any{"userId": 8, "nick": "synthetic"},
			"remark": string(bytes.Repeat([]byte("x"), 400))}
	}
	body, _ := json.Marshal(map[string]any{"code": 0, "data": map[string]any{"lastId": "2798", "list": rows}})
	if len(body) <= nativewire.Limit || len(body) >= 3*nativewire.Limit {
		t.Fatal("fixture does not exercise the large bounded server window")
	}
	requests := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _, params := desktopRequest(t, r, c.bodyKey)
		if r.URL.Path == "/v1/group/get-apply-logs" && (len(params) != 1 || string(params["lastId"]) != `"0"`) {
			t.Error("history request lost fixed desktop scope")
		}
		requests++
		w.Write(body)
	}))
	defer server.Close()
	c.config.Origin, c.http = server.URL, server.Client()
	if got, _, err := c.InvitationRecords(context.Background()); err != nil || len(got) != 2798 {
		t.Fatal("valid bounded window rejected", len(got), err)
	}
	if _, err := c.Request(context.Background(), "/v1/friend/get-friend-list", map[string]any{"v": "0"}); err == nil {
		t.Fatal("history exception widened an unrelated route")
	}
	body = bytes.Repeat([]byte("x"), 3*nativewire.Limit+4097)
	if _, _, err := c.InvitationRecords(context.Background()); err == nil {
		t.Fatal("unbounded history response accepted")
	}
	rows = append(rows, make([]map[string]any, 4097-len(rows))...)
	body, _ = json.Marshal(map[string]any{"code": 0, "data": map[string]any{"lastId": "2798", "list": rows}})
	if _, _, err := c.InvitationRecords(context.Background()); err == nil {
		t.Fatal("history record count limit removed")
	}
	if requests != 4 {
		t.Fatal("oversize responses were automatically retried", requests)
	}
}

func TestDesktopSelfInvitationAcceptanceIsFencedAndNeverRetried(t *testing.T) {
	base, err := NewClient(syntheticDeployment())
	if err != nil {
		t.Fatal(err)
	}
	c := base.(*Client)
	defer c.Close()
	c.jwt, c.fields[3] = "synthetic-self-invitation", 7
	requests := 0
	response := map[string]any{"newAdd": true, "state": "MEMBER_STATE_GOOD", "groupCloudId": "9007199254740995"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		_, _, params := desktopRequest(t, r, c.bodyKey)
		if r.URL.Path != "/v1/group/group-member-invite" || len(params) != 2 ||
			string(params["applyId"]) != `"9007199254740993"` || string(params["agree"]) != "true" {
			t.Error("self acceptance departed from the pinned desktop contract", r.URL.Path, params)
		}
		json.NewEncoder(w).Encode(map[string]any{"code": 0, "data": response})
	}))
	defer server.Close()
	c.config.Origin, c.http = server.URL, server.Client()
	change := SettingChange{Kind: "group_accept", Group: "123", Target: "7", Text: "9007199254740993"}
	fence := func(ctx context.Context, submit func(context.Context) error) error { return submit(ctx) }
	if _, err := c.Request(context.Background(), "/v1/group/group-member-invite", map[string]any{}); err == nil {
		t.Fatal("raw acceptance bypassed final fence")
	}
	bad := change
	bad.Target = "8"
	if err := c.ChangeSocial(context.Background(), bad, nil, fence); err == nil || requests != 0 {
		t.Fatal("accepted another person's invitation", err)
	}
	for _, id := range []string{"", "01", "-1", "9223372036854775808"} {
		bad = change
		bad.Text = id
		if bad.Validate() == nil {
			t.Fatal("invalid application identifier", id)
		}
	}
	revoked := errors.New("synthetic-revoked-invitation")
	if err := c.ChangeSocial(context.Background(), change, nil,
		func(context.Context, func(context.Context) error) error { return revoked }); !errors.Is(err, revoked) || requests != 0 {
		t.Fatal("revoked invitation reached native HTTP", err)
	}
	if err := c.ChangeSocial(context.Background(), change, nil, fence); err != nil || requests != 1 {
		t.Fatal("owned invitation was not submitted exactly once", err, requests)
	}
	response = map[string]any{"newAdd": false, "state": "MEMBER_STATE_APPLIED"}
	if err := c.ChangeSocial(context.Background(), change, nil, fence); err != nil || requests != 2 {
		t.Fatal("pending administrator approval was not acknowledged", err)
	}
	for _, invalid := range []map[string]any{
		{"newAdd": true, "state": "MEMBER_STATE_GOOD"},
		{"newAdd": true, "state": "MEMBER_STATE_APPLIED", "groupCloudId": "1"},
		{"newAdd": false, "state": "MEMBER_STATE_INVITED"},
		{"state": "MEMBER_STATE_GOOD"},
	} {
		response = invalid
		before := requests
		var native *Error
		err := c.ChangeSocial(context.Background(), change, nil, fence)
		if !errors.As(err, &native) || !native.Submitted || native.Code != "native_response_invalid" || requests != before+1 {
			t.Fatal("ambiguous response was retried or considered safe to replay", err, requests)
		}
	}
}
