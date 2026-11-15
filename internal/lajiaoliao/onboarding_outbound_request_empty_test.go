package lajiaoliao

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

type outboundEmptyApplicationsCase struct {
	name     string
	data     any
	envelope string
}

func outboundEmptyApplicationsCases(t *testing.T) []outboundEmptyApplicationsCase {
	t.Helper()
	// Pinned GetPaginationFriendsApplyFromResp permits omitted proto3 defaults.
	return []outboundEmptyApplicationsCase{
		{name: "sdk_proto3_zero", data: json.RawMessage("{}")},
		{name: "nil_data"},
		{name: "raw_null", data: json.RawMessage("null")},
		{name: "empty_object", data: map[string]any{}},
		{name: "omitted_list_zero_total", data: map[string]any{"total": 0}},
		{name: "null_list_omitted_total", data: map[string]any{"friendRequests": nil}},
		{name: "empty_list_omitted_total", data: map[string]any{"friendRequests": []any{}}},
		{name: "null_list_zero_total", data: map[string]any{"friendRequests": nil, "total": 0}},
		{name: "empty_list_zero_total", data: map[string]any{"friendRequests": []any{}, "total": 0}},
		{name: "success_envelope_missing_data", envelope: `{"errCode":0}`},
		{name: "success_envelope_null_data", envelope: `{"errCode":0,"errMsg":"","errDlt":"","data":null}`},
	}
}

type outboundEmptyApplicationsFixture struct {
	applications any
	friends      any
	envelope     string
	status       int
	readErr      error
	writeErr     error
	routes       []string
	writes       int
	fences       int
}

func (f *outboundEmptyApplicationsFixture) client(t *testing.T) *Client {
	t.Helper()
	note, err := onboardingOutboundNote(outboundOperation)
	if err != nil {
		t.Fatal(err)
	}
	client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
		f.routes = append(f.routes, r.URL.Path)
		switch r.URL.Path {
		case "/user/search/full":
			return outboundSearchResponse(outboundSearchUser()), nil
		case "/friend/get_designated_friends":
			if !reflect.DeepEqual(body, map[string]any{"ownerUserID": outboundSession.UserID,
				"friendUserIDs": []any{"target-string"}}) {
				t.Fatal("relationship read escaped the selected account")
			}
			return f.friends, nil
		case "/friend/get_self_friend_apply_list":
			if r.Header.Get("token") != outboundSession.IMToken || r.Header.Get("platform") != "5" ||
				!reflect.DeepEqual(body, map[string]any{"userID": outboundSession.UserID,
					"pagination": map[string]any{"pageNumber": float64(1), "showNumber": float64(100)}}) {
				t.Fatal("outgoing application read escaped its bounded authenticated scope")
			}
			return f.applications, f.readErr
		case "/friend/add_friend":
			if f.fences != 1 || r.Header.Get("token") != outboundSession.IMToken ||
				r.Header.Get("platform") != "5" || !reflect.DeepEqual(body, map[string]any{
				"fromUserID": outboundSession.UserID, "toUserID": "target-string", "reqMsg": note, "ex": ""}) {
				t.Fatal("empty list bypassed the selected target or durable final fence")
			}
			f.writes++
			return map[string]any{}, f.writeErr
		default:
			t.Fatal("unexpected outbound route", r.URL.Path)
			return nil, nil
		}
	})
	if f.envelope != "" {
		transport := client.http.Transport
		client.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
			resp, err := transport.RoundTrip(r)
			if err != nil || r.URL.Path != "/friend/get_self_friend_apply_list" {
				return resp, err
			}
			resp.Body.Close()
			resp = response(f.envelope)
			if f.status != 0 {
				resp.StatusCode = f.status
			}
			return resp, nil
		})
	}
	return client
}

func (f *outboundEmptyApplicationsFixture) confirm(t *testing.T) OnboardingOutboundFence {
	t.Helper()
	return func(ctx context.Context, target OnboardingOutboundTarget, submit func(context.Context) error) error {
		f.fences++
		if target.ID != "target-string" || target.Account != "native-account" ||
			target.IsFriend || !target.CanRequest {
			t.Fatal("final fence did not receive the freshly checked target", target)
		}
		return submit(ctx)
	}
}

func outboundEmptyApplicationsRequest() OnboardingOutboundRequest {
	return OnboardingOutboundRequest{OperationID: outboundOperation, SelfID: outboundSession.UserID,
		ExpiresAt: time.Now().Add(time.Minute), Target: OnboardingOutboundTarget{
			ID: "target-string", Account: "native-account", Name: "old nickname"}}
}

func (f *outboundEmptyApplicationsFixture) assertReads(t *testing.T, sent bool) {
	t.Helper()
	want := []string{"/user/search/full", "/friend/get_designated_friends", "/friend/get_self_friend_apply_list"}
	if sent {
		want = append(want, "/friend/add_friend")
	}
	if !reflect.DeepEqual(f.routes, want) {
		t.Fatal("outbound preflight retried, skipped a read, or dispatched an extra mutation", f.routes)
	}
}

func assertOutboundEmptyApplicationsProjection(t *testing.T, out OnboardingOutboundEvidence) {
	t.Helper()
	raw, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	note, _ := onboardingOutboundNote(outboundOperation)
	for _, private := range []string{note, `"reqMsg"`, `"friendRequests"`, `"phoneNumber"`, "not-returned",
		outboundSession.IMToken, outboundSession.ChatToken} {
		if strings.Contains(string(raw), private) {
			t.Fatal("outgoing application source data escaped the evidence projection")
		}
	}
}

func TestOnboardingOutboundApplicationsProto3EmptySendContract(t *testing.T) {
	for _, tc := range outboundEmptyApplicationsCases(t) {
		for _, friend := range []struct {
			name string
			data any
		}{
			{"nil_relationship", nil},
			{"empty_relationship_object", map[string]any{}},
			{"null_relationship", map[string]any{"friendsInfo": nil}},
			{"empty_relationship", outboundFriendResponse(false)},
		} {
			t.Run(tc.name+"/"+friend.name, func(t *testing.T) {
				f := &outboundEmptyApplicationsFixture{applications: tc.data, friends: friend.data, envelope: tc.envelope}
				out, err := f.client(t).SendOnboardingOutboundFriend(context.Background(), outboundSession,
					outboundEmptyApplicationsRequest(), f.confirm(t))
				want := OnboardingOutboundEvidence{OperationID: outboundOperation, SenderID: outboundSession.UserID,
					RecipientID: "target-string", State: "pending"}
				if err != nil || out != want || f.writes != 1 || f.fences != 1 {
					t.Fatal("SDK empty application response bypassed or blocked a single fenced submission", out, err, f.writes, f.fences)
				}
				f.assertReads(t, true)
				assertOutboundEmptyApplicationsProjection(t, out)
			})
		}
	}
}

func TestOnboardingOutboundApplicationsRejectMalformedBeforeSend(t *testing.T) {
	const shape = "lajiaoliao_onboarding_outbound_shape"
	for _, tc := range []struct {
		name     string
		data     any
		envelope string
		status   int
		readErr  error
		code     string
	}{
		{name: "unknown_field", data: map[string]any{"other": true}, code: shape},
		{name: "missing_list_nonzero_total", data: map[string]any{"total": 1}, code: shape},
		{name: "missing_list_negative_total", data: map[string]any{"total": -1}, code: shape},
		{name: "missing_list_null_total", data: map[string]any{"total": nil}, code: shape},
		{name: "missing_list_string_total", data: map[string]any{"total": "0"}, code: shape},
		{name: "null_list_nonzero_total", data: map[string]any{"friendRequests": nil, "total": 1}, code: shape},
		{name: "null_list_unknown_field", data: map[string]any{"friendRequests": nil, "other": true}, code: shape},
		{name: "numeric_list", data: map[string]any{"friendRequests": 0}, code: shape},
		{name: "string_list", data: map[string]any{"friendRequests": "[]"}, code: shape},
		{name: "object_list", data: map[string]any{"friendRequests": map[string]any{}}, code: shape},
		{name: "boolean_list", data: map[string]any{"friendRequests": false}, code: shape},
		{name: "null_row", data: map[string]any{"friendRequests": []any{nil}}, code: shape},
		{name: "array_data", data: []any{}, code: shape},
		{name: "string_data", data: "{}", code: shape},
		{name: "boolean_data", data: false, code: shape},
		{name: "empty_list_nonzero_total", data: map[string]any{"friendRequests": []any{}, "total": 1},
			code: "lajiaoliao_onboarding_outbound_incomplete"},
		{name: "empty_list_negative_total", data: map[string]any{"friendRequests": []any{}, "total": -1}, code: shape},
		{name: "envelope_unknown_field", envelope: `{"errCode":0,"data":null,"other":true}`, code: shape},
		{name: "nonzero_business_code", envelope: `{"errCode":1001,"data":null}`, code: "lajiaoliao_request_rejected"},
		{name: "missing_business_code", envelope: `{"data":null}`, code: "lajiaoliao_result_unknown"},
		{name: "null_business_code", envelope: `{"errCode":null,"data":null}`, code: "lajiaoliao_result_unknown"},
		{name: "http_failure", envelope: `{"errCode":0,"data":null}`, status: http.StatusBadGateway, code: "lajiaoliao_result_unknown"},
		{name: "read_failure", readErr: io.ErrUnexpectedEOF, code: "lajiaoliao_result_unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &outboundEmptyApplicationsFixture{applications: tc.data, envelope: tc.envelope,
				status: tc.status, readErr: tc.readErr}
			out, err := f.client(t).SendOnboardingOutboundFriend(context.Background(), outboundSession,
				outboundEmptyApplicationsRequest(), f.confirm(t))
			if err == nil || code(err) != tc.code || out != (OnboardingOutboundEvidence{}) || f.writes != 0 || f.fences != 0 {
				t.Fatal("failed or malformed preflight became a mutation or successful receipt", out, err, f.writes, f.fences)
			}
			if tc.code != "lajiaoliao_onboarding_outbound_incomplete" {
				var lookup *OnboardingLookupError
				if !errors.As(err, &lookup) || lookup.Stage != "sent_requests" || lookup.Cause == nil {
					t.Fatal("preflight read failure lost its safe sent-requests stage", err)
				}
			}
			f.assertReads(t, false)
		})
	}
}

func TestOnboardingOutboundApplicationsProto3PendingAndHistory(t *testing.T) {
	note, _ := onboardingOutboundNote(outboundOperation)
	for _, mode := range []string{"pending_omitted_result", "pending_zero", "rejected_history", "accepted_history",
		"same_operation_history", "wrong_sender", "self_recipient", "missing_time", "invalid_result",
		"duplicate_rows", "contradicting_total", "full_page_without_total", "overfull_page"} {
		t.Run(mode, func(t *testing.T) {
			row := outboundApplication("unrelated old request", 1700000000)
			wantCode := ""
			switch mode {
			case "pending_omitted_result":
				wantCode = "lajiaoliao_onboarding_outbound_pending"
			case "pending_zero":
				row["handleResult"] = 0
				wantCode = "lajiaoliao_onboarding_outbound_pending"
			case "rejected_history":
				row["handleResult"] = -1
			case "accepted_history":
				row["handleResult"] = 1
			case "same_operation_history":
				row["handleResult"], row["reqMsg"] = -1, note
				wantCode = "lajiaoliao_onboarding_outbound_pending"
			case "wrong_sender":
				row["fromUserID"] = "other-account"
				wantCode = "lajiaoliao_onboarding_outbound_shape"
			case "self_recipient":
				row["toUserID"] = outboundSession.UserID
				wantCode = "lajiaoliao_onboarding_outbound_shape"
			case "missing_time":
				delete(row, "createTime")
				wantCode = "lajiaoliao_onboarding_outbound_shape"
			case "invalid_result":
				row["handleResult"] = 2
				wantCode = "lajiaoliao_onboarding_outbound_shape"
			case "duplicate_rows", "contradicting_total", "overfull_page":
				wantCode = "lajiaoliao_onboarding_outbound_shape"
			case "full_page_without_total":
				wantCode = "lajiaoliao_onboarding_outbound_incomplete"
			}
			rows := []any{row}
			if mode == "duplicate_rows" {
				rows = append(rows, row)
			}
			if mode == "full_page_without_total" || mode == "overfull_page" {
				count := 100
				if mode == "overfull_page" {
					count++
				}
				rows = nil
				for i := range count {
					item := outboundApplication("unrelated history", 1700000000+int64(i))
					item["toUserID"], item["handleResult"] = "other-target", -1
					rows = append(rows, item)
				}
			}
			data := map[string]any{"friendRequests": rows}
			if mode == "contradicting_total" {
				data["total"] = 0
			}
			f := &outboundEmptyApplicationsFixture{applications: data}
			out, err := f.client(t).SendOnboardingOutboundFriend(context.Background(), outboundSession,
				outboundEmptyApplicationsRequest(), f.confirm(t))
			if wantCode == "" {
				if err != nil || out.State != "pending" || f.writes != 1 || f.fences != 1 {
					t.Fatal("complete unrelated processed history blocked one new request", out, err)
				}
			} else if err == nil || code(err) != wantCode || out != (OnboardingOutboundEvidence{}) ||
				f.writes != 0 || f.fences != 0 {
				t.Fatal("pending, malformed, or incomplete history allowed a new request", out, err)
			}
			f.assertReads(t, wantCode == "")
		})
	}
}

func TestOnboardingOutboundApplicationsEmptyReconcileRemainsReadOnly(t *testing.T) {
	for _, tc := range outboundEmptyApplicationsCases(t) {
		t.Run(tc.name, func(t *testing.T) {
			f := &outboundEmptyApplicationsFixture{applications: tc.data, envelope: tc.envelope}
			out, err := f.client(t).ReconcileOnboardingOutboundFriend(context.Background(), outboundSession,
				outboundEmptyApplicationsRequest())
			want := OnboardingOutboundEvidence{OperationID: outboundOperation, SenderID: outboundSession.UserID,
				RecipientID: "target-string", State: "missing"}
			if err != nil || out != want || f.writes != 0 || f.fences != 0 ||
				!reflect.DeepEqual(f.routes, []string{"/friend/get_designated_friends", "/friend/get_self_friend_apply_list"}) {
				t.Fatal("empty reconciliation invented a request, friend, or mutation", out, err, f.routes)
			}
			assertOutboundEmptyApplicationsProjection(t, out)
		})
	}
}

func TestOnboardingOutboundApplicationsReadFailureRetainsReadOnlyStage(t *testing.T) {
	for _, mode := range []string{"send", "reconcile"} {
		t.Run(mode, func(t *testing.T) {
			f := &outboundEmptyApplicationsFixture{readErr: io.ErrUnexpectedEOF}
			client := f.client(t)
			var out OnboardingOutboundEvidence
			var err error
			if mode == "send" {
				out, err = client.SendOnboardingOutboundFriend(context.Background(), outboundSession,
					outboundEmptyApplicationsRequest(), f.confirm(t))
			} else {
				out, err = client.ReconcileOnboardingOutboundFriend(context.Background(), outboundSession,
					outboundEmptyApplicationsRequest())
			}
			var lookup *OnboardingLookupError
			var cause *Failure
			if !errors.As(err, &lookup) || lookup.Stage != "sent_requests" || lookup.Cause == nil ||
				!errors.As(lookup.Cause, &cause) || cause.Code != "lajiaoliao_result_unknown" || !cause.Submitted ||
				out != (OnboardingOutboundEvidence{}) || f.writes != 0 || f.fences != 0 {
				t.Fatal("read-only transport uncertainty lost its stage or became a friend mutation", out, err, f.writes, f.fences)
			}
			if mode == "send" {
				f.assertReads(t, false)
			} else if !reflect.DeepEqual(f.routes, []string{"/friend/get_designated_friends", "/friend/get_self_friend_apply_list"}) {
				t.Fatal("read-only reconciliation retried or changed its target", f.routes)
			}
		})
	}
}

func TestOnboardingOutboundApplicationsEmptyStillRequiresFinalFence(t *testing.T) {
	for _, mode := range []string{"revoked", "cancelled", "no_submit", "duplicate_submit", "unknown_write"} {
		t.Run(mode, func(t *testing.T) {
			f := &outboundEmptyApplicationsFixture{}
			if mode == "unknown_write" {
				f.writeErr = io.ErrUnexpectedEOF
			}
			client := f.client(t)
			revoked := errors.New("synthetic final admission revoked")
			out, err := client.SendOnboardingOutboundFriend(context.Background(), outboundSession,
				outboundEmptyApplicationsRequest(), func(ctx context.Context, _ OnboardingOutboundTarget, submit func(context.Context) error) error {
					f.fences++
					switch mode {
					case "revoked":
						return revoked
					case "cancelled":
						cancelled, cancel := context.WithCancel(ctx)
						cancel()
						return submit(cancelled)
					case "no_submit":
						return nil
					case "duplicate_submit":
						if err := submit(ctx); err != nil {
							t.Fatal("first synthetic submission failed", err)
						}
					}
					return submit(ctx)
				})
			if err == nil || out != (OnboardingOutboundEvidence{}) || f.fences != 1 {
				t.Fatal("empty preflight fabricated a completed submission", out, err)
			}
			if mode == "duplicate_submit" || mode == "unknown_write" {
				var failure *Failure
				if !errors.As(err, &failure) || !failure.Submitted || f.writes != 1 {
					t.Fatal("unknown or repeated write lost its single-attempt boundary", err, f.writes)
				}
			} else if f.writes != 0 || mode == "revoked" && !errors.Is(err, revoked) ||
				mode == "cancelled" && !errors.Is(err, context.Canceled) ||
				mode == "no_submit" && code(err) != "lajiaoliao_fence_required" {
				t.Fatal("failed final fence dispatched a mutation", err, f.writes)
			}
			f.assertReads(t, f.writes == 1)
		})
	}
}
