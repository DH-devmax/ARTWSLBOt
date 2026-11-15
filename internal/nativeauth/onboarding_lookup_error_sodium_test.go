//go:build sodium && cgo

package nativeauth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
)

func TestOnboardingMobileLookupRetainsFailingReadAndNeverRetries(t *testing.T) {
	for _, stage := range []string{"search", "friend_info"} {
		for _, number := range []int{-1, 1001, 1043, 1053, 1000, 1201} {
			t.Run(stage+"/"+strconv.Itoa(number), func(t *testing.T) {
				c, _ := newSyntheticMobileClient(t)
				c.jwt, c.fields[3], c.mac = "synthetic-mobile-jwt", 7, append([]byte(nil), c.public[16:]...)
				c.identity = map[string]json.RawMessage{"accountId": json.RawMessage("340738650")}
				var reads atomic.Int32
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					mobileRequest(t, r, c.bodyKey)
					reads.Add(1)
					if r.URL.Path == onboardingOutboundApplyRoute {
						t.Error("lookup submitted a friend request")
					}
					if stage == "friend_info" && r.URL.Path == onboardingOutboundSearchRoute {
						json.NewEncoder(w).Encode(map[string]any{"code": 0,
							"data": map[string]any{"items": []any{onboardingOutboundSearchRow()}}})
						return
					}
					json.NewEncoder(w).Encode(map[string]any{"code": number, "msg": "synthetic-secret phone=13800000000", "data": map[string]any{}})
				}))
				defer server.Close()
				c.config.Origin, c.http = server.URL, server.Client()
				target, err := c.LookupOnboardingOutboundFriend(context.Background(), "340738649")
				var lookup *OnboardingLookupError
				var native *Error
				if target != (OnboardingOutboundTarget{}) || !errors.As(err, &lookup) ||
					!errors.As(err, &native) || lookup.Stage != stage || native.Number != number ||
					native.Diagnostic == "" || strings.Contains(err.Error(), "synthetic-secret") {
					t.Fatal("lookup lost its safe stage/code or leaked provider content", err)
				}
				expected := int32(1)
				if stage == "friend_info" {
					expected = 2
				}
				if reads.Load() != expected {
					t.Fatal("lookup retried or continued beyond a rejected read")
				}
				if number == 1043 {
					c.LookupOnboardingOutboundFriend(context.Background(), "340738649")
					if reads.Load() != expected {
						t.Fatal("known rate limit bypassed the existing client cooldown")
					}
				}
				if number == 1053 && (c.jwt != "" || len(c.mac) != 0) {
					t.Fatal("expired lookup session remained usable in the client")
				}
			})
		}
	}
}
