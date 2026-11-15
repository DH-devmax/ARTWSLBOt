package lajiaoliao

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestGroupBanBoundsIdentityAndNoMutationOnUnknown(t *testing.T) {
	s := Session{UserID: "self-native", IMToken: "im-fixture", DeviceID: "device-fixture"}
	for _, mode := range []string{"empty", "cross_group", "duplicate", "short_page", "denied", "unknown"} {
		t.Run(mode, func(t *testing.T) {
			writes := 0
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				switch r.URL.Path {
				case "/group/get_group_members_info":
					return map[string]any{"members": []any{map[string]any{
						"groupID": "group-native", "userID": s.UserID, "roleLevel": 100}}}, nil
				case "/group/get_group_permanent_ban_list":
					ban := map[string]any{"groupID": "group-native", "userID": "target-native",
						"nickname": "fixture", "blockedBy": s.UserID, "blockedAt": 1700000000}
					if mode == "empty" {
						return map[string]any{}, nil
					}
					if mode == "cross_group" {
						ban["groupID"] = "other-group"
					}
					bans, total := []any{ban}, 1
					if mode == "duplicate" {
						bans, total = append(bans, ban), 2
					} else if mode == "short_page" {
						total = 2
					}
					return map[string]any{"bans": bans, "total": total}, nil
				default:
					writes++
					return nil, errors.New("synthetic response loss")
				}
			})
			err := client.Execute(context.Background(), s, Change{Kind: "remove_group_ban",
				Group: "group-native", Target: "target-native"}, func(ctx context.Context, send func(context.Context) error) error {
				if mode == "denied" {
					return errors.New("synthetic revoked")
				}
				return send(ctx)
			})
			if err == nil || (mode == "unknown" && (writes != 1 || code(err) != "lajiaoliao_result_unknown")) ||
				mode != "unknown" && writes != 0 {
				t.Fatal("ban evidence or single-attempt boundary failed", mode, writes, err)
			}
		})
	}
}
