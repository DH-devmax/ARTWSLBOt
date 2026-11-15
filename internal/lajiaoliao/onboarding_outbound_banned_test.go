package lajiaoliao

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

func checkOnboardingOutboundBanned(t *testing.T, banned, code string, friend bool) {
	t.Helper()
	user := outboundSearchUser()
	if banned != "" {
		// Preserve numeric spellings so 1.0 cannot collapse to an integer fixture.
		user["isBanned"] = json.RawMessage(banned)
	}
	user["phone"] = "synthetic-private-phone"
	user["info"] = map[string]any{"detail": "synthetic-private-info"}
	var routes []string
	client := nativeFixture(t, func(r *http.Request, _ map[string]any) (any, error) {
		routes = append(routes, r.URL.Path)
		switch r.URL.Path {
		case "/user/search/full":
			return outboundSearchResponse(user), nil
		case "/friend/get_designated_friends":
			if code != "" {
				t.Fatal("rejected banned value reached the relationship read")
			}
			return outboundFriendResponse(friend), nil
		default:
			t.Fatalf("banned lookup reached an unexpected route %s", r.URL.Path)
			return nil, nil
		}
	})
	target, err := client.LookupOnboardingOutboundFriend(context.Background(), outboundSession, "native-account")
	wantRoutes := []string{"/user/search/full"}
	if code == "" {
		wantRoutes = append(wantRoutes, "/friend/get_designated_friends")
		want := OnboardingOutboundTarget{ID: "target-string", Account: "native-account",
			Name: "synthetic", IsFriend: friend, CanRequest: !friend}
		if err != nil || target != want {
			t.Fatal("canonical false banned value changed the selected projection", target, err)
		}
	} else {
		var failure *Failure
		if !errors.As(err, &failure) || failure.Code != code || failure.Submitted ||
			target != (OnboardingOutboundTarget{}) {
			t.Fatal("banned lookup did not return an empty projection and safe failure", target, err)
		}
	}
	if !reflect.DeepEqual(routes, wantRoutes) {
		t.Fatal("banned lookup continued, retried, or skipped a required read", routes)
	}
	raw, marshalErr := json.Marshal(target)
	if marshalErr != nil {
		t.Fatal(marshalErr)
	}
	for _, private := range []string{`"phone"`, `"phoneNumber"`, `"info"`,
		"not-returned", "synthetic-private-phone", "synthetic-private-info"} {
		if strings.Contains(string(raw), private) || err != nil && strings.Contains(err.Error(), private) {
			t.Fatal("private search data escaped the target or failure projection")
		}
	}
}

func TestOnboardingOutboundBannedCanonicalFalseReadsRelationship(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"missing", ""},
		{"null", "null"},
		{"boolean_false", "false"},
		{"numeric_zero", "0"},
		{"string_zero", `"0"`},
		{"string_false", `"false"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, friend := range []bool{false, true} {
				name := "not_friend"
				if friend {
					name = "existing_friend"
				}
				t.Run(name, func(t *testing.T) {
					checkOnboardingOutboundBanned(t, tc.raw, "", friend)
				})
			}
		})
	}
}

func TestOnboardingOutboundBannedCanonicalTrueStopsAfterSearch(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"boolean_true", "true"},
		{"numeric_one", "1"},
		{"string_one", `"1"`},
		{"string_true", `"true"`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkOnboardingOutboundBanned(t, tc.raw, "lajiaoliao_onboarding_outbound_unavailable", false)
		})
	}
}

func TestOnboardingOutboundBannedInvalidShapeStopsAfterSearch(t *testing.T) {
	for _, tc := range []struct {
		name string
		raw  string
	}{
		{"numeric_two", "2"},
		{"negative_one", "-1"},
		{"decimal_one", "1.0"},
		{"decimal_zero", "0.0"},
		{"exponent_one", "1e0"},
		{"negative_zero", "-0"},
		{"uppercase_false", `"False"`},
		{"uppercase_true", `"True"`},
		{"padded_zero", `" 0"`},
		{"trailing_zero", `"0 "`},
		{"padded_false", `" false"`},
		{"empty_string", `""`},
		{"string_two", `"2"`},
		{"string_decimal_one", `"1.0"`},
		{"empty_array", "[]"},
		{"boolean_array", "[false]"},
		{"empty_object", "{}"},
		{"object_value", `{"value":false}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkOnboardingOutboundBanned(t, tc.raw, "lajiaoliao_onboarding_outbound_shape", false)
		})
	}
}

func TestOnboardingOutboundEmptyRelationshipSuccessContract(t *testing.T) {
	const shape = "lajiaoliao_onboarding_outbound_shape"
	for _, tc := range []struct {
		name      string
		data      any
		envelope  string
		status    int
		code      string
		submitted bool
	}{
		{name: "nil_data", data: nil},
		{name: "empty_object", data: map[string]any{}},
		{name: "raw_null", data: json.RawMessage("null")},
		{name: "friends_null", data: map[string]any{"friendsInfo": nil}},
		{name: "friends_empty", data: map[string]any{"friendsInfo": []any{}}},
		{name: "envelope_missing_data", envelope: `{"errCode":0}`},
		{name: "envelope_null_with_metadata", envelope: `{"errCode":0,"errMsg":"","errDlt":"","data":null}`},
		{name: "nonempty_missing_field", data: map[string]any{"other": true}, code: shape},
		{name: "friends_numeric", data: map[string]any{"friendsInfo": 0}, code: shape},
		{name: "friends_string", data: map[string]any{"friendsInfo": "[]"}, code: shape},
		{name: "friends_object", data: map[string]any{"friendsInfo": map[string]any{}}, code: shape},
		{name: "wrong_owner", data: json.RawMessage(`{"friendsInfo":[{"ownerUserID":"other-owner","friendUser":{"userID":"target-string","nickname":"synthetic"}}]}`), code: shape},
		{name: "wrong_target", data: json.RawMessage(`{"friendsInfo":[{"ownerUserID":"robot-string","friendUser":{"userID":"other-target","nickname":"synthetic"}}]}`), code: shape},
		{name: "null_friend_row", data: map[string]any{"friendsInfo": []any{nil}}, code: shape},
		{name: "array_data", data: []any{}, code: shape},
		{name: "string_data", data: "{}", code: shape},
		{name: "numeric_data", data: 0, code: shape},
		{name: "boolean_data", data: false, code: shape},
		{name: "envelope_unknown_field", envelope: `{"errCode":0,"data":null,"other":true}`, code: shape},
		{name: "rejected_null_data", envelope: `{"errCode":1001,"data":null}`, code: "lajiaoliao_request_rejected", submitted: true},
		{name: "rejected_empty_data", envelope: `{"errCode":1001,"data":{}}`, code: "lajiaoliao_request_rejected", submitted: true},
		{name: "http_failure", envelope: `{"errCode":0,"data":null}`, status: http.StatusBadGateway,
			code: "lajiaoliao_result_unknown", submitted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var routes []string
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				routes = append(routes, r.URL.Path)
				switch r.URL.Path {
				case "/user/search/full":
					user := outboundSearchUser()
					user["phone"] = "synthetic-private-phone"
					user["info"] = "synthetic-private-info"
					return outboundSearchResponse(user), nil
				case "/friend/get_designated_friends":
					if r.Header.Get("token") != outboundSession.IMToken || r.Header.Get("platform") != "5" ||
						!reflect.DeepEqual(body, map[string]any{"ownerUserID": outboundSession.UserID,
							"friendUserIDs": []any{"target-string"}}) {
						t.Fatal("empty relationship read escaped its authenticated target")
					}
					return tc.data, nil
				default:
					t.Fatalf("empty relationship lookup reached an unexpected route %s", r.URL.Path)
					return nil, nil
				}
			})
			if tc.envelope != "" {
				transport := client.http.Transport
				client.http.Transport = roundTrip(func(r *http.Request) (*http.Response, error) {
					resp, err := transport.RoundTrip(r)
					if err != nil || r.URL.Path != "/friend/get_designated_friends" {
						return resp, err
					}
					resp.Body.Close()
					// Only these cases replace the full wire envelope; nil data
					// above keeps nativeFixture's ordinary successful envelope.
					resp = response(tc.envelope)
					if tc.status != 0 {
						resp.StatusCode = tc.status
					}
					return resp, nil
				})
			}
			target, err := client.LookupOnboardingOutboundFriend(context.Background(), outboundSession, "native-account")
			if !reflect.DeepEqual(routes, []string{"/user/search/full", "/friend/get_designated_friends"}) {
				t.Fatal("empty relationship lookup retried or skipped a required read", routes)
			}
			if tc.code == "" {
				want := OnboardingOutboundTarget{ID: "target-string", Account: "native-account",
					Name: "synthetic", CanRequest: true}
				if err != nil || target != want {
					t.Fatal("verified empty relationship did not permit one friend request", target, err)
				}
			} else {
				var failure *Failure
				if !errors.As(err, &failure) || failure.Code != tc.code || failure.Submitted != tc.submitted ||
					target != (OnboardingOutboundTarget{}) {
					t.Fatal("invalid relationship or failed request became an empty success", target, err)
				}
			}
			raw, marshalErr := json.Marshal(target)
			if marshalErr != nil {
				t.Fatal(marshalErr)
			}
			for _, private := range []string{`"phone"`, `"phoneNumber"`, `"info"`,
				"not-returned", "synthetic-private-phone", "synthetic-private-info"} {
				if strings.Contains(string(raw), private) || err != nil && strings.Contains(err.Error(), private) {
					t.Fatal("private search data escaped the empty relationship projection")
				}
			}
		})
	}
}
