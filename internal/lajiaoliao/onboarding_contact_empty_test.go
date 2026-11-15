package lajiaoliao

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"reflect"
	"strconv"
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
)

func TestOnboardingContactEmptyResponsesNeverAuthorizeChange(t *testing.T) {
	for _, tc := range []struct {
		name, envelope string
	}{
		{"omitted_data", `{"errCode":0}`},
		{"null_data", `{"errCode":0,"errMsg":"","errDlt":"","data":null}`},
		{"empty_data", `{"errCode":0,"data":{}}`},
		{"omitted_list", `{"errCode":0,"data":{"total":0}}`},
		{"null_list", `{"errCode":0,"data":{"friendRequests":null}}`},
		{"null_list_zero_total", `{"errCode":0,"data":{"friendRequests":null,"total":0}}`},
		{"sdk_null_list", `{"errCode":0,"data":{"FriendRequests":null,"total":0}}`},
		{"empty_list", `{"errCode":0,"data":{"friendRequests":[]}}`},
		{"empty_list_zero_total", `{"errCode":0,"data":{"friendRequests":[],"total":0}}`},
	} {
		for _, action := range []string{"discover", "reconcile", "approve", "reject"} {
			t.Run(tc.name+"/"+action, func(t *testing.T) {
				selector, at := onboardingContactFixture()
				if action == "reject" {
					selector.RejectExpired = true
					at = selector.Since.Add(-10 * time.Minute)
				}
				expected := nativeauth.OnboardingFriendEvidence{
					ApplicationID: strconv.FormatInt(at.UnixMilli(), 10), SenderID: "customer",
					RecipientID: "self", State: "pending", Name: selector.Name, RequestedAt: at,
				}
				client, reads := onboardingContactEmptyClient(t, tc.envelope)
				session := Session{UserID: "self", IMToken: "im-fixture", DeviceID: "device-fixture"}
				fences := 0
				fence := func(context.Context, nativeauth.OnboardingFriendEvidence, func(context.Context) error) error {
					fences++
					t.Fatal("empty incoming page reached the mutation fence")
					return nil
				}
				var proof nativeauth.OnboardingFriendEvidence
				var err error
				switch action {
				case "discover":
					proof, err = client.OnboardingContact(context.Background(), session, selector)
				case "reconcile":
					proof, err = client.ReconcileOnboardingContact(context.Background(), session, selector, expected)
				case "approve":
					err = client.ApproveOnboardingContact(context.Background(), session, selector, expected,
						time.Now().Add(time.Minute), fence)
				case "reject":
					err = client.RejectOnboardingContact(context.Background(), session, selector, expected,
						time.Now().Add(time.Minute), fence)
				}
				var failure *Failure
				if !errors.As(err, &failure) || failure.Code != "lajiaoliao_onboarding_contact_not_found" ||
					failure.Submitted || proof != (nativeauth.OnboardingFriendEvidence{}) ||
					*reads != 1 || fences != 0 {
					t.Fatalf("empty page produced proof, submission or the wrong status: proof=%+v reads=%d fences=%d err=%v",
						proof, *reads, fences, err)
				}
			})
		}
	}
}

func TestOnboardingContactEmptyCompatibilityRejectsUnverifiedContainers(t *testing.T) {
	selector, _ := onboardingContactFixture()
	for _, tc := range []struct {
		name, data string
	}{
		{"missing_list_nonzero_total", `{"total":1}`},
		{"null_list_nonzero_total", `{"friendRequests":null,"total":1}`},
		{"empty_list_nonzero_total", `{"friendRequests":[],"total":1}`},
		{"null_total", `{"friendRequests":null,"total":null}`},
		{"empty_list_null_total", `{"friendRequests":[],"total":null}`},
		{"negative_total", `{"friendRequests":null,"total":-1}`},
		{"string_total", `{"friendRequests":null,"total":"0"}`},
		{"unknown_container", `{"records":[]}`},
		{"unknown_field", `{"friendRequests":null,"records":[]}`},
		{"nested_list", `{"friendRequests":{"records":[]}}`},
		{"string_list", `{"friendRequests":""}`},
		{"null_row", `{"friendRequests":[null]}`},
		{"array_data", `[]`},
		{"string_data", `""`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client, reads := onboardingContactEmptyClient(t, `{"errCode":0,"data":`+tc.data+`}`)
			proof, err := client.OnboardingContact(context.Background(),
				Session{UserID: "self", IMToken: "im-fixture", DeviceID: "device-fixture"}, selector)
			var failure *Failure
			if !errors.As(err, &failure) || failure.Code == "lajiaoliao_onboarding_contact_not_found" ||
				failure.Submitted || proof != (nativeauth.OnboardingFriendEvidence{}) || *reads != 1 {
				t.Fatalf("unverified container became an empty page or proof: proof=%+v reads=%d err=%v", proof, *reads, err)
			}
		})
	}
}

func TestOnboardingContactEmptyCompatibilityPreservesEnvelopeFailure(t *testing.T) {
	selector, _ := onboardingContactFixture()
	for _, envelope := range []string{
		`{"errCode":1001,"data":null}`,
		`{"errCode":1001,"data":{}}`,
		`{"data":{}}`,
		`{"errCode":0,"data":null,"records":[1]}`,
	} {
		t.Run(envelope, func(t *testing.T) {
			client, reads := onboardingContactEmptyClient(t, envelope)
			proof, err := client.OnboardingContact(context.Background(),
				Session{UserID: "self", IMToken: "im-fixture", DeviceID: "device-fixture"}, selector)
			var failure *Failure
			if !errors.As(err, &failure) || failure.Code == "lajiaoliao_onboarding_contact_not_found" ||
				proof != (nativeauth.OnboardingFriendEvidence{}) || *reads != 1 {
				t.Fatalf("unverified envelope became an empty page or proof: proof=%+v reads=%d err=%v", proof, *reads, err)
			}
		})
	}
}

func onboardingContactEmptyClient(t *testing.T, envelope string) (*Client, *int) {
	t.Helper()
	reads := new(int)
	client := &Client{http: &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		*reads++
		if r.Method != http.MethodPost || r.URL.Host != "im-api.lajiaoliao.com" ||
			r.URL.Path != "/friend/get_friend_apply_list" || r.Header.Get("token") != "im-fixture" ||
			r.Header.Get("platform") != "5" || r.Header.Get("deviceID") != "device-fixture" ||
			r.Header.Get("operationID") == "" {
			t.Fatal("empty incoming page escaped the authenticated read route", r.URL.Path)
		}
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil || !reflect.DeepEqual(body, map[string]any{
			"userID": "self", "pagination": map[string]any{"pageNumber": float64(1), "showNumber": float64(100)},
		}) {
			t.Fatal("empty incoming read escaped its bounded first page")
		}
		return response(envelope), nil
	})}}
	return client, reads
}
