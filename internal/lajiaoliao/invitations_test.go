package lajiaoliao

import (
	"context"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

func invitationFixture() map[string]any {
	return map[string]any{
		"userInfo":   map[string]any{"userID": "self-string", "nickname": "synthetic"},
		"groupInfo":  map[string]any{"groupID": "group-string"},
		"joinSource": 2, "inviterUserID": "owner-string", "reqTime": int64(1700000000123),
	}
}

func TestChiliSelfInvitationListAndShape(t *testing.T) {
	session := Session{UserID: "self-string", IMToken: "im-fixture", DeviceID: "device-fixture"}
	for _, mode := range []string{"pending", "accepted", "ordinary_application", "known_empty",
		"missing_total", "missing_list", "wrong_group", "wrong_user", "self_inviter", "no_inviter",
		"bad_result", "bad_time", "overflow_time", "duplicate", "truncated_page", "negative_total", "over_limit"} {
		t.Run(mode, func(t *testing.T) {
			row := invitationFixture()
			value := map[string]any{"total": 1, "groupRequests": []any{row}}
			switch mode {
			case "accepted":
				row["handleResult"] = 1
			case "ordinary_application":
				row["joinSource"], row["inviterUserID"] = 1, ""
			case "known_empty":
				value["total"], value["groupRequests"] = 0, nil
			case "missing_total":
				delete(value, "total")
			case "missing_list":
				delete(value, "groupRequests")
			case "wrong_group":
				row["groupInfo"] = map[string]any{"groupID": "other-group"}
			case "wrong_user":
				row["userInfo"] = map[string]any{"userID": "other-user"}
			case "self_inviter":
				row["inviterUserID"] = session.UserID
			case "no_inviter":
				delete(row, "inviterUserID")
			case "bad_result":
				row["handleResult"] = 99
			case "bad_time":
				row["reqTime"] = 0
			case "overflow_time":
				row["reqTime"] = int64(253402300800000)
			case "duplicate":
				value["total"], value["groupRequests"] = 2, []any{row, row}
			case "truncated_page":
				value["total"] = 2
			case "negative_total":
				value["total"] = -1
			case "over_limit":
				value["total"] = 10001
			}
			reads := 0
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				reads++
				if r.URL.Path != "/group/get_user_req_group_applicationList" || r.Header.Get("token") != session.IMToken ||
					!reflect.DeepEqual(body, map[string]any{"userID": session.UserID, "groupIDs": []any{"group-string"},
						"pagination": map[string]any{"pageNumber": float64(1), "showNumber": float64(100)}}) {
					t.Fatal("self list borrowed administrator route or foreign identity")
				}
				return value, nil
			})
			rows, more, err := client.SelfInvitations(context.Background(), session, "group-string", 1)
			valid := mode == "pending" || mode == "accepted" || mode == "ordinary_application" || mode == "known_empty"
			if reads != 1 || more || valid != (err == nil) {
				t.Fatal("unbounded, unknown or malformed invitation accepted", reads, more, err)
			}
			if valid && (mode == "pending" || mode == "accepted") {
				if len(rows) != 1 || !validInvitationID(rows[0].ID()) || rows[0].User != session.UserID {
					t.Fatal("lost immutable self invitation")
				}
				if rows[0].Time().Format(time.RFC3339Nano) != "2023-11-14T22:13:20.123Z" {
					t.Fatal("invitation timestamp is not milliseconds")
				}
				prior := rows[0].ID()
				rows[0].Result++
				if rows[0].ID() != prior {
					t.Fatal("result changed invitation identity")
				}
				rows[0].Created++
				if rows[0].ID() == prior {
					t.Fatal("reissued invitation inherited old identity")
				}
			}
			if valid && (mode == "ordinary_application" || mode == "known_empty") && (rows == nil || len(rows) != 0) {
				t.Fatal("ordinary application became an invitation")
			}
		})
	}
}

func TestChiliSelfInvitationSingleFencedSubmission(t *testing.T) {
	s := Session{UserID: "self-string", IMToken: "im-fixture", DeviceID: "device-fixture"}
	id := (SelfInvitation{Group: "group-string", User: s.UserID, Inviter: "owner-string", Created: 1700000000123}).ID()
	for _, mode := range []string{"success", "unknown", "fence_denied", "cancelled", "other_target",
		"changed_record", "accepted", "ordinary_application", "ambiguous", "extra_field"} {
		t.Run(mode, func(t *testing.T) {
			writes, fences := 0, 0
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				if r.URL.Path == "/group/get_user_req_group_applicationList" {
					row := invitationFixture()
					total, items := 1, []any{row}
					if mode == "accepted" {
						row["handleResult"] = 1
					}
					if mode == "ordinary_application" {
						row["joinSource"] = 1
					}
					if mode == "ambiguous" {
						other := invitationFixture()
						other["reqTime"] = int64(1700000000124)
						total, items = 2, []any{row, other}
					}
					return map[string]any{"total": total, "groupRequests": items}, nil
				}
				if r.URL.Path != "/group/group_application_response" || fences != 1 ||
					!reflect.DeepEqual(body, map[string]any{"groupID": "group-string", "fromUserID": s.UserID,
						"handledMsg": "", "handleResult": float64(1)}) {
					t.Fatal("unfenced or incorrect self-accept contract", r.URL.Path, body)
				}
				writes++
				if mode == "unknown" {
					return nil, io.ErrUnexpectedEOF
				}
				return map[string]any{}, nil
			})
			change := Change{Kind: "group_accept", Group: "group-string", Target: s.UserID, Text: id}
			switch mode {
			case "other_target":
				change.Target = "other-user"
			case "changed_record":
				change.Text = strings.Repeat("0", 64)
			case "extra_field":
				change.Enabled = true
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			err := client.Execute(ctx, s, change, func(work context.Context, submit func(context.Context) error) error {
				fences++
				if mode == "fence_denied" {
					return errors.New("synthetic denied")
				}
				return submit(work)
			})
			if mode == "success" {
				if err != nil || writes != 1 || fences != 1 {
					t.Fatal("single acceptance failed", err, writes, fences)
				}
			} else if mode == "unknown" {
				var failure *Failure
				if !errors.As(err, &failure) || failure.Code != "lajiaoliao_result_unknown" ||
					!failure.Submitted || writes != 1 || fences != 1 {
					t.Fatal("unknown acceptance was retried", err, writes)
				}
			} else if err == nil || writes != 0 {
				t.Fatal("changed or unauthorized invitation submitted", err, writes)
			}
		})
	}
}
