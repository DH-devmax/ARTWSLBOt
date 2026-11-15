package lajiaoliao

import (
	"context"
	"encoding/json"
	"strconv"
	"sync/atomic"
	"time"

	"wangshangliao/internal/nativeauth"
)

// OnboardingFriend uses only page one. reqMsg is compared inside this adapter
// and never added to FriendRequest or any customer-facing social projection.
func (c *Client) OnboardingFriend(ctx context.Context, s Session, challenge string) (nativeauth.OnboardingFriendEvidence, error) {
	return c.onboardingFriend(ctx, s, challenge, nil)
}

func (c *Client) ReconcileOnboardingFriend(ctx context.Context, s Session, challenge string,
	expected nativeauth.OnboardingFriendEvidence) (nativeauth.OnboardingFriendEvidence, error) {
	return c.onboardingFriend(ctx, s, challenge, &expected)
}

func (c *Client) onboardingFriend(ctx context.Context, s Session, challenge string,
	expected *nativeauth.OnboardingFriendEvidence) (nativeauth.OnboardingFriendEvidence, error) {
	if !ValidID(s.UserID) || !nativeauth.ValidOnboardingFriendChallenge(challenge) {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_friend_invalid"}
	}
	raw, err := c.request(ctx, "/friend/get_friend_apply_list", map[string]any{"userID": s.UserID,
		"pagination": map[string]int{"pageNumber": 1, "showNumber": 100}}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return nativeauth.OnboardingFriendEvidence{}, err
	}
	defer clear(raw)
	return parseOnboardingFriend(raw, s.UserID, challenge, expected)
}

func parseOnboardingFriend(raw json.RawMessage, self, challenge string,
	expected *nativeauth.OnboardingFriendEvidence) (nativeauth.OnboardingFriendEvidence, error) {
	var result struct {
		Requests *[]struct {
			FriendRequest
			Note string `json:"reqMsg"`
		} `json:"friendRequests"`
	}
	if !ValidID(self) || !nativeauth.ValidOnboardingFriendChallenge(challenge) ||
		json.Unmarshal(raw, &result) != nil || result.Requests == nil || len(*result.Requests) > 100 {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_friend_shape"}
	}
	seen := map[string]bool{}
	pendingBySender := map[string]int{}
	var proof nativeauth.OnboardingFriendEvidence
	matches, pending := 0, 0
	for _, row := range *result.Requests {
		id := strconv.FormatInt(row.Created, 10)
		key := row.From + ":" + id
		state := 0 // Pinned SDK proto3 omits the pending zero value.
		if row.Result != nil {
			state = *row.Result
		}
		if row.To != self || !ValidID(row.From) || row.From == self || row.Created <= 0 || seen[key] ||
			state != -1 && state != 0 && state != 1 || !validText(row.Name, 1000, true) ||
			!validText(row.Note, 4096, true) {
			return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_friend_shape"}
		}
		seen[key] = true
		if state == 0 {
			pendingBySender[row.From]++
		}
		if row.Note != challenge || expected == nil && state != 0 {
			continue
		}
		if state == 0 {
			pending++
		}
		if expected != nil && (id != expected.ApplicationID || row.From != expected.SenderID || self != expected.RecipientID) {
			continue
		}
		matches++
		proof = nativeauth.OnboardingFriendEvidence{ApplicationID: id, SenderID: row.From, RecipientID: self,
			State: map[int]string{-1: "rejected", 0: "pending", 1: "approved"}[state]}
	}
	if matches == 0 {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_friend_not_found"}
	}
	// The response route selects sender/recipient only, not createTime. A second
	// pending application from that sender cannot be disambiguated on the wire.
	if matches != 1 || proof.State == "pending" && (pending != 1 || pendingBySender[proof.SenderID] != 1) {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_friend_ambiguous"}
	}
	return proof, nil
}

func (c *Client) ApproveOnboardingFriend(ctx context.Context, s Session, challenge string,
	expected nativeauth.OnboardingFriendEvidence, deadline time.Time, fence nativeauth.OnboardingFriendFence) error {
	if fence == nil || !nativeauth.OnboardingFriendDeadline(deadline, time.Now()) {
		return &Failure{Code: "lajiaoliao_onboarding_friend_expired"}
	}
	proof, err := c.onboardingFriend(ctx, s, challenge, &expected)
	if err != nil {
		return err
	}
	if proof.State != "pending" {
		return &Failure{Code: "lajiaoliao_onboarding_friend_changed"}
	}
	var submitted atomic.Bool
	return c.Execute(ctx, s, Change{Kind: "friend_approve", Target: proof.SenderID, Text: proof.ApplicationID},
		func(work context.Context, submit func(context.Context) error) error {
			if !nativeauth.OnboardingFriendDeadline(deadline, time.Now()) {
				return &Failure{Code: "lajiaoliao_onboarding_friend_expired"}
			}
			return fence(work, proof, func(final context.Context) error {
				if !submitted.CompareAndSwap(false, true) {
					return &Failure{Code: "lajiaoliao_onboarding_friend_already_submitted"}
				}
				if err := final.Err(); err != nil {
					return err
				}
				if !nativeauth.OnboardingFriendDeadline(deadline, time.Now()) {
					return &Failure{Code: "lajiaoliao_onboarding_friend_expired"}
				}
				return submit(final)
			})
		})
}
