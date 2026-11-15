package lajiaoliao

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"testing"

	"github.com/openimsdk/protocol/sdkws"
)

func nativeFixture(t *testing.T, visit func(*http.Request, map[string]any) (any, error)) *Client {
	t.Helper()
	return &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		if r.URL.Host != "im-api.lajiaoliao.com" && r.URL.Host != "api.lajiaoliao.com" {
			t.Fatal("unfixed host")
		}
		if r.Header.Get("operationID") == "" || r.Header.Get("deviceID") != "device-fixture" {
			t.Fatal("missing request identity")
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Fatal("invalid JSON")
		}
		value, err := visit(r, body)
		if err != nil {
			return nil, err
		}
		raw, err := json.Marshal(map[string]any{"errCode": 0, "data": value})
		if err != nil {
			t.Fatal(err)
		}
		return response(string(raw)), nil
	})}}
}

func TestNativeModerationIndependentContracts(t *testing.T) {
	session := Session{UserID: "self-native", IMToken: "im-fixture", ChatToken: "chat-fixture", DeviceID: "device-fixture"}
	cases := []struct {
		change  Change
		route   string
		payload map[string]any
	}{
		{Change{Kind: "grant_admin", Group: "group-native", Target: "target-native"}, "/group/set_group_member_info",
			map[string]any{"members": []any{map[string]any{"groupID": "group-native", "userID": "target-native", "roleLevel": float64(60)}}}},
		{Change{Kind: "revoke_admin", Group: "group-native", Target: "target-native"}, "/group/set_group_member_info",
			map[string]any{"members": []any{map[string]any{"groupID": "group-native", "userID": "target-native", "roleLevel": float64(20)}}}},
		{Change{Kind: "mute_member", Group: "group-native", Target: "target-native", Minutes: 1}, "/group/mute_group_member",
			map[string]any{"groupID": "group-native", "userID": "target-native", "mutedSeconds": float64(60)}},
		{Change{Kind: "unmute_member", Group: "group-native", Target: "target-native"}, "/group/cancel_mute_group_member",
			map[string]any{"groupID": "group-native", "userID": "target-native"}},
		{Change{Kind: "mute_group", Group: "group-native"}, "/group/mute_group", map[string]any{"groupID": "group-native"}},
		{Change{Kind: "unmute_group", Group: "group-native"}, "/group/cancel_mute_group", map[string]any{"groupID": "group-native"}},
		{Change{Kind: "kick_member", Group: "group-native", Target: "target-native"}, "/group/kick_group",
			map[string]any{"groupID": "group-native", "kickedUserIDs": []any{"target-native"}, "reason": "", "permanentBan": false}},
		{Change{Kind: "kick_member_ban", Group: "group-native", Target: "target-native"}, "/group/kick_group",
			map[string]any{"groupID": "group-native", "kickedUserIDs": []any{"target-native"}, "reason": "", "permanentBan": true}},
		{Change{Kind: "remove_group_ban", Group: "group-native", Target: "target-native"}, "/group/remove_group_permanent_ban",
			map[string]any{"groupID": "group-native", "userID": "target-native"}},
		{Change{Kind: "publish_notice", Group: "group-native", Text: "synthetic notice"}, "/group/set_group_info_ex",
			map[string]any{"groupID": "group-native", "notification": "synthetic notice"}},
		{Change{Kind: "group_info", Group: "group-native", Text: "synthetic"}, "/group/set_group_info_ex",
			map[string]any{"groupID": "group-native", "groupName": "synthetic"}},
		{Change{Kind: "member_nickname", Group: "group-native", Target: "target-native", Text: "synthetic"}, "/group/set_group_member_info",
			map[string]any{"members": []any{map[string]any{"groupID": "group-native", "userID": "target-native", "nickname": "synthetic"}}}},
		{Change{Kind: "group_invite", Group: "group-native", Target: "target-native"}, "/group/invite_user_to_group",
			map[string]any{"groupID": "group-native", "invitedUserIDs": []any{"target-native"}, "reason": ""}},
		{Change{Kind: "group_leave", Group: "group-native"}, "/group/quit_group", map[string]any{"groupID": "group-native", "userID": "self-native"}},
		{Change{Kind: "friend_approve", Target: "target-native", Text: "1700000000"}, "/friend/add_friend_response",
			map[string]any{"fromUserID": "target-native", "toUserID": "self-native", "handleResult": float64(1), "handleMsg": ""}},
		{Change{Kind: "friend_reject", Target: "target-native", Text: "1700000000"}, "/friend/add_friend_response",
			map[string]any{"fromUserID": "target-native", "toUserID": "self-native", "handleResult": float64(-1), "handleMsg": ""}},
	}
	for _, test := range cases {
		t.Run(test.change.Kind, func(t *testing.T) {
			writes, fences := 0, 0
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				if r.Header.Get("token") != session.IMToken || r.Header.Get("platform") != "5" {
					t.Fatal("wrong platform or ticket")
				}
				if r.URL.Path == "/group/get_group_members_info" {
					id := body["userIDs"].([]any)[0].(string)
					role := 20
					if id == session.UserID {
						role = 100
						if test.change.Kind == "group_leave" {
							role = 60
						}
					}
					if id == test.change.Target && test.change.Kind == "revoke_admin" {
						role = 60
					}
					return map[string]any{"members": []any{map[string]any{"groupID": "group-native", "userID": id, "roleLevel": role}}}, nil
				}
				if r.URL.Path == "/group/get_group_permanent_ban_list" {
					return map[string]any{"total": 1, "bans": []any{map[string]any{
						"groupID": "group-native", "userID": "target-native", "nickname": "fixture",
						"blockedBy": session.UserID, "blockedAt": 1700000000}}}, nil
				}
				if fences != 1 || r.URL.Path != test.route || !reflect.DeepEqual(body, test.payload) {
					t.Fatalf("wrong fenced contract: %s", r.URL.Path)
				}
				writes++
				return map[string]any{}, nil
			})
			err := client.Execute(context.Background(), session, test.change, func(ctx context.Context, send func(context.Context) error) error { fences++; return send(ctx) })
			if err != nil || writes != 1 || fences != 1 {
				t.Fatal("single mutation failed", err, writes, fences)
			}
		})
	}
}

func TestNativeRoleChangeFenceCancelAndUnknownDoNotReplay(t *testing.T) {
	s := Session{UserID: "self-native", IMToken: "im-fixture", DeviceID: "device-fixture"}
	for _, mode := range []string{"admin_cannot_promote", "protected_owner", "cancelled", "fence_denied", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			writes := 0
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				if r.URL.Path == "/group/get_group_members_info" {
					id := body["userIDs"].([]any)[0].(string)
					role := 20
					if id == s.UserID {
						role = 100
						if mode == "admin_cannot_promote" {
							role = 60
						}
					}
					if mode == "protected_owner" && id != s.UserID {
						role = 100
					}
					return map[string]any{"members": []any{map[string]any{"groupID": "group-native", "userID": id, "roleLevel": role}}}, nil
				}
				writes++
				return nil, io.ErrUnexpectedEOF
			})
			change := Change{Kind: "grant_admin", Group: "group-native", Target: "target-native"}
			ctx, cancel := context.WithCancel(context.Background())
			if mode == "cancelled" {
				cancel()
			}
			defer cancel()
			err := client.Execute(ctx, s, change, func(work context.Context, send func(context.Context) error) error {
				if mode == "fence_denied" {
					return errors.New("fixture denied")
				}
				return send(work)
			})
			if err == nil {
				t.Fatal("missing failure")
			}
			if mode == "unknown" {
				if writes != 1 || code(err) != "lajiaoliao_result_unknown" {
					t.Fatal("unknown retried", err, writes)
				}
			} else if writes != 0 {
				t.Fatal("unauthorized mutation", writes)
			}
		})
	}
}

func TestNativeDirectoryUsesNestedSDKContractsAndProtoDefaults(t *testing.T) {
	s := Session{UserID: "self-native", IMToken: "im-fixture", DeviceID: "device-fixture"}
	client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
		switch r.URL.Path {
		case "/friend/get_friend_list":
			return map[string]any{"friendsInfo": []*sdkws.FriendInfo{{OwnerUserID: s.UserID, FriendUser: &sdkws.UserInfo{UserID: "friend-native", Nickname: "synthetic"}}}}, nil
		case "/friend/get_friend_apply_list":
			return map[string]any{"friendRequests": []*sdkws.FriendRequest{{FromUserID: "friend-native", ToUserID: s.UserID, CreateTime: 1700000000}}}, nil
		case "/group/get_recv_group_applicationList":
			return map[string]any{"groupRequests": []*sdkws.GroupRequest{{UserInfo: &sdkws.PublicUserInfo{UserID: "friend-native"}, GroupInfo: &sdkws.GroupInfo{GroupID: "group-native"}, ReqTime: 1700000000}}}, nil
		case "/group/get_groups_info":
			return map[string]any{"groupInfos": []*sdkws.GroupInfo{{GroupID: "group-native"}}}, nil
		default:
			t.Fatal("unreviewed route")
			return nil, nil
		}
	})
	friends, more, err := client.Friends(context.Background(), s, 1)
	if err != nil || more || len(friends) != 1 || friends[0].ID != "friend-native" {
		t.Fatal("friend nesting lost", err)
	}
	requests, more, err := client.FriendRequests(context.Background(), s, 1)
	if err != nil || more || len(requests) != 1 || requests[0].Result == nil || *requests[0].Result != 0 {
		t.Fatal("pending default lost", err)
	}
	groups, more, err := client.GroupRequests(context.Background(), s, "group-native", 1)
	if err != nil || more || len(groups) != 1 || groups[0].User != "friend-native" || *groups[0].Result != 0 {
		t.Fatal("group request nesting lost", err)
	}
	group, err := client.GroupInfo(context.Background(), s, "group-native")
	if err != nil || group.Status == nil || *group.Status != 0 {
		t.Fatal("normal default lost", err)
	}
}

func TestFriendApprovalApplicationPaginationCompleteness(t *testing.T) {
	s := Session{UserID: "self-native", IMToken: "im-fixture", DeviceID: "device-fixture"}
	for _, test := range []struct {
		name               string
		page, count, total int
		ok, more           bool
	}{
		{"truncated_final", 1, 1, 2, false, false},
		{"reported_too_small", 1, 1, 0, false, false},
		{"empty", 1, 0, 0, true, false},
		{"exact", 1, 1, 1, true, false},
		{"full_final", 1, 100, 100, true, false},
		{"full_more", 1, 100, 101, true, true},
		{"second_final", 2, 1, 101, true, false},
		{"second_truncated", 2, 1, 102, false, false},
		{"negative", 1, 0, -1, false, false},
		{"oversized", 1, 100, 10001, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			c := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				if r.URL.Path != "/friend/get_friend_apply_list" ||
					body["pagination"].(map[string]any)["pageNumber"] != float64(test.page) {
					t.Fatal("unexpected application page")
				}
				rows := make([]FriendRequest, test.count)
				for i := range rows {
					rows[i] = FriendRequest{From: "peer-native", To: s.UserID, Created: int64(1700000000 + i)}
				}
				return map[string]any{"friendRequests": rows, "total": test.total}, nil
			})
			rows, more, err := c.FriendRequests(context.Background(), s, test.page)
			if (err == nil) != test.ok || err == nil && (len(rows) != test.count || more != test.more) {
				t.Fatal("incomplete applications were presented as complete", rows, more, err)
			}
		})
	}
}

func TestGroupRequestsExplicitEmptyAndUnknownRemainDistinct(t *testing.T) {
	s := Session{UserID: "self-native", IMToken: "im-fixture", DeviceID: "device-fixture"}
	for _, test := range []struct {
		name  string
		value any
		valid bool
	}{
		{"null_with_zero_total", map[string]any{"total": 0, "groupRequests": nil}, true},
		{"omitted_with_zero_total", map[string]any{"total": 0}, true},
		{"empty_list", map[string]any{"groupRequests": []any{}}, true},
		{"missing_collection", map[string]any{}, false},
		{"null_without_total", map[string]any{"groupRequests": nil}, false},
		{"nonzero_without_collection", map[string]any{"total": 1, "groupRequests": nil}, false},
		{"negative_total", map[string]any{"total": -1, "groupRequests": []any{}}, false},
		{"oversized_total", map[string]any{"total": 10001}, false},
		{"wrong_total_type", map[string]any{"total": "0"}, false},
		{"wrong_collection_type", map[string]any{"total": 0, "groupRequests": map[string]any{}}, false},
		{"more_rows_than_total", map[string]any{"total": 0, "groupRequests": []any{map[string]any{}}}, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			calls := 0
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				calls++
				if r.URL.Path != "/group/get_recv_group_applicationList" ||
					body["fromUserID"] != s.UserID ||
					!reflect.DeepEqual(body["groupIDs"], []any{"group-native"}) {
					t.Fatal("request escaped its group or read route")
				}
				return test.value, nil
			})
			rows, more, err := client.GroupRequests(context.Background(), s, "group-native", 1)
			if calls != 1 || test.valid && (err != nil || rows == nil || len(rows) != 0 || more) {
				t.Fatal("explicit empty list lost or request repeated", err, calls)
			}
			if !test.valid {
				var failure *Failure
				if !errors.As(err, &failure) || failure.Code != "lajiaoliao_group_request_shape" {
					t.Fatal("unknown response became an empty list", err)
				}
			}
		})
	}
}

func TestGroupInfoUsesPinnedChiliStatusEnums(t *testing.T) {
	session := Session{UserID: "self-native", IMToken: "im-fixture", DeviceID: "device-fixture"}
	// Desktop 1.0.3: normal=0, banned=1, dismissed=2, muted=3.
	for _, test := range []struct {
		name    string
		status  any
		want    int
		allowed bool
	}{
		{"normal-omitted", nil, GroupStatusNormal, true},
		{"normal", 0, GroupStatusNormal, true},
		{"muted", 3, GroupStatusMuted, true},
		{"banned", 1, 0, false},
		{"dismissed", 2, 0, false},
		{"unknown", 4, 0, false},
		{"negative", -1, 0, false},
		{"wrong-type", "3", 0, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				if r.URL.Path != "/group/get_groups_info" || !reflect.DeepEqual(body["groupIDs"], []any{"group-native"}) {
					t.Fatal("unexpected group query")
				}
				info := map[string]any{"groupID": "group-native"}
				if test.status != nil {
					info["status"] = test.status
				}
				return map[string]any{"groupInfos": []any{info}}, nil
			})
			info, err := client.GroupInfo(context.Background(), session, "group-native")
			if !test.allowed {
				var failure *Failure
				if !errors.As(err, &failure) || failure.Code != "lajiaoliao_group_shape" {
					t.Fatalf("unavailable group accepted: %#v %v", info, err)
				}
				return
			}
			if err != nil || info.Status == nil || *info.Status != test.want {
				t.Fatalf("status lost: %#v %v", info, err)
			}
		})
	}
}

func TestNativeProfilePreservesExAndSeparatesTickets(t *testing.T) {
	s := Session{UserID: "self-native", IMToken: "im-fixture", ChatToken: "chat-fixture", DeviceID: "device-fixture"}
	writes := 0
	client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
		switch r.URL.Path {
		case "/user/get_users_info":
			if r.Header.Get("token") != s.IMToken {
				t.Fatal("wrong IM ticket")
			}
			return map[string]any{"usersInfo": []any{map[string]any{"userID": s.UserID, "nickname": "synthetic", "ex": `{"allowGroupInvite":1,"disableAudioVideoCalls":false,"keep":{"version":2}}`}}}, nil
		case "/friend/privacy/get":
			if r.Header.Get("token") != s.ChatToken || r.URL.Host != "api.lajiaoliao.com" {
				t.Fatal("privacy host or ticket mixed")
			}
			return map[string]any{"privacy": map[string]any{"findByLajiaoID": false, "findByPhone": true, "addFriendPolicy": 3}}, nil
		case "/user/update_user_info_ex":
			var ex map[string]any
			info := body["userInfo"].(map[string]any)
			if info["userID"] != s.UserID || json.Unmarshal([]byte(info["ex"].(string)), &ex) != nil ||
				ex["allowGroupInvite"] != float64(1) || ex["disableAudioVideoCalls"] != true || ex["keep"].(map[string]any)["version"] != float64(2) {
				t.Fatal("unrelated profile fields lost")
			}
			writes++
			return map[string]any{}, nil
		default:
			t.Fatal("wrong route")
			return nil, nil
		}
	})
	view, err := client.Profile(context.Background(), s)
	if err != nil || view.FriendPolicy != "FRIEND_REJECT_ALL" || view.SearchAccount == nil || *view.SearchAccount || view.AudioVideo == nil || !*view.AudioVideo {
		t.Fatal("profile mapping", err)
	}
	err = client.ChangeProfile(context.Background(), s, Change{Kind: "audio_video", Enabled: false}, func(ctx context.Context, send func(context.Context) error) error { return send(ctx) })
	if err != nil || writes != 1 {
		t.Fatal("profile mutation", err, writes)
	}
}

func TestNativeUnsupportedAndExtraFieldsNeverSubmit(t *testing.T) {
	for _, change := range []Change{
		{Kind: "group_accept", Group: "group-native", Target: "self-native", Text: "1"},
		{Kind: "grant_admin", Group: "group-native", Target: "target-native", Text: "extra"},
		{Kind: "mute_member", Group: "group-native", Target: "target-native", Minutes: 0},
		{Kind: "member_nickname", Group: "group-native", Target: "target-native", Text: "1234567890123456"},
		{Kind: "audio_video", Enabled: true, Target: "other-native"},
		{Kind: "secondary_security_code", Text: "fixture"},
	} {
		if change.Validate() == nil {
			t.Fatal("unsupported or extra fields accepted", change.Kind)
		}
	}
}
