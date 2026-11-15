package nativeauth

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"
)

// OnboardingFriendEvidence contains no application note or profile. The WSL
// recipient is the authenticated self, proven by FROM_HIM in its own list.
type OnboardingFriendEvidence struct {
	ApplicationID string    `json:"application_id"`
	SenderID      string    `json:"sender_id"`
	RecipientID   string    `json:"recipient_id"`
	State         string    `json:"state"`
	Name          string    `json:"name,omitempty"`
	RequestedAt   time.Time `json:"requested_at,omitzero"`
}

type OnboardingFriendFence func(context.Context, OnboardingFriendEvidence, func(context.Context) error) error

func ValidOnboardingFriendChallenge(text string) bool {
	return text != "" && len(text) <= 160 && utf8.ValidString(text) &&
		strings.TrimSpace(text) == text && !stringsContainControl(text)
}

func OnboardingFriendDeadline(deadline, now time.Time) bool {
	return deadline.After(now) && !deadline.After(now.Add(10*time.Minute))
}

// OnboardingFriend reads exactly the desktop's first server window. The
// ordinary friend application projection deliberately continues to omit notes.
func (c *Client) OnboardingFriend(ctx context.Context, challenge string) (OnboardingFriendEvidence, error) {
	proof, _, err := c.onboardingFriend(ctx, challenge, nil)
	return proof, err
}

func (c *Client) ReconcileOnboardingFriend(ctx context.Context, challenge string,
	expected OnboardingFriendEvidence) (OnboardingFriendEvidence, error) {
	proof, _, err := c.onboardingFriend(ctx, challenge, &expected)
	return proof, err
}

func (c *Client) onboardingFriend(ctx context.Context, challenge string,
	expected *OnboardingFriendEvidence) (OnboardingFriendEvidence, *FriendApplication, error) {
	if !ValidOnboardingFriendChallenge(challenge) {
		return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_friend_invalid")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	self := strconv.FormatUint(c.fields[3], 10)
	if !settingID(self) {
		return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_friend_invalid")
	}
	data, err := c.request(ctx, "/v1/friend/friend-apply-list", map[string]any{"lastId": "0"}, nil)
	if err != nil {
		return OnboardingFriendEvidence{}, nil, err
	}
	defer func() {
		for _, raw := range data {
			clear(raw)
		}
	}()
	return parseOnboardingFriend(data, self, challenge, expected)
}

func parseOnboardingFriend(data map[string]json.RawMessage, self, challenge string,
	expected *OnboardingFriendEvidence) (OnboardingFriendEvidence, *FriendApplication, error) {
	var notes []struct {
		Note json.RawMessage `json:"applyMsg"`
	}
	if !settingID(self) || !ValidOnboardingFriendChallenge(challenge) ||
		json.Unmarshal(data["list"], &notes) != nil || notes == nil || len(notes) > 500 {
		return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_friend_shape")
	}
	applications, err := parseFriendApplications(data)
	if err != nil {
		return OnboardingFriendEvidence{}, nil, err
	}
	var proof OnboardingFriendEvidence
	var selected *FriendApplication
	matches, pending := 0, 0
	for i, application := range applications {
		var note string
		if len(notes[i].Note) > 0 && json.Unmarshal(notes[i].Note, &note) != nil ||
			!utf8.ValidString(note) || len(note) > 4096 {
			return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_friend_shape")
		}
		state := map[string]string{
			"FRIENDLOG_STATE_UNKNOWN": "unknown", "FRIENDLOG_STATE_AGREE": "approved",
			"FRIENDLOG_STATE_REFUSE": "rejected", "FRIENDLOG_STATE_PROCESSING": "pending",
			"FRIENDLOG_STATE_DELETE": "deleted", "FRIENDLOG_STATE_EXPIRE": "expired",
		}[application.State]
		if application.Direction != "FROM_HIM" || application.User.ID == self || note != challenge ||
			expected == nil && state != "pending" {
			continue
		}
		if state == "pending" {
			pending++
		}
		if expected != nil && (application.ID != expected.ApplicationID || application.User.ID != expected.SenderID ||
			self != expected.RecipientID) {
			continue
		}
		matches++
		proof = OnboardingFriendEvidence{ApplicationID: application.ID, SenderID: application.User.ID,
			RecipientID: self, State: state}
		copy := application
		selected = &copy
	}
	if matches == 0 {
		return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_friend_not_found")
	}
	if matches != 1 || proof.State == "pending" && pending != 1 {
		return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_friend_ambiguous")
	}
	return proof, selected, nil
}

// ApproveOnboardingFriend rereads the exact pending application, then uses the
// existing social submission fence. It never creates a new friend application.
func (c *Client) ApproveOnboardingFriend(ctx context.Context, challenge string, expected OnboardingFriendEvidence,
	deadline time.Time, fence OnboardingFriendFence) error {
	if fence == nil || !OnboardingFriendDeadline(deadline, time.Now()) {
		return rejection("native_onboarding_friend_expired")
	}
	proof, application, err := c.onboardingFriend(ctx, challenge, &expected)
	if err != nil {
		return err
	}
	if proof.State != "pending" {
		return rejection("native_onboarding_friend_changed")
	}
	change := SettingChange{Kind: "friend_approve", Target: proof.SenderID, Text: proof.ApplicationID}
	var submitted atomic.Bool
	return c.ChangeSocial(ctx, change, application, func(work context.Context, submit func(context.Context) error) error {
		if !OnboardingFriendDeadline(deadline, time.Now()) {
			return rejection("native_onboarding_friend_expired")
		}
		return fence(work, proof, func(final context.Context) error {
			if !submitted.CompareAndSwap(false, true) {
				return rejection("native_onboarding_friend_already_submitted")
			}
			if err := final.Err(); err != nil {
				return err
			}
			if !OnboardingFriendDeadline(deadline, time.Now()) {
				return rejection("native_onboarding_friend_expired")
			}
			return submit(final)
		})
	})
}
