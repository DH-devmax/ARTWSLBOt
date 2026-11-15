package nativeauth

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode"
	"unicode/utf8"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// OnboardingContactSelector uses a control-owned, fixed server interval
// [Since, Until). Contact selection is not a website identity proof.
type OnboardingContactSelector struct {
	Name          string
	Since, Until  time.Time
	RejectExpired bool
}

func ValidOnboardingContactSelector(s OnboardingContactSelector) bool {
	return s.Name != "" && len(s.Name) <= 200 && utf8.ValidString(s.Name) &&
		strings.TrimSpace(s.Name) == s.Name &&
		strings.IndexFunc(s.Name, func(r rune) bool {
			return unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == utf8.RuneError
		}) < 0 && !s.Since.IsZero() && !s.Until.IsZero() &&
		s.Since.Year() >= 1 && s.Until.Year() <= 9999 &&
		s.Until.After(s.Since) && s.Until.Sub(s.Since) <= 10*time.Minute
}

// Matches checks only the contact selector. Callers separately validate native
// identity, direction, state, application uniqueness and connection fences.
func (s OnboardingContactSelector) Matches(e OnboardingFriendEvidence) bool {
	return ValidOnboardingContactSelector(s) && e.Name == s.Name && !e.RequestedAt.IsZero() &&
		(!e.RequestedAt.Before(s.Since) && e.RequestedAt.Before(s.Until) || s.IsExpired(e))
}

// IsExpired uses the server-issued challenge time, leaving a ten-minute gap
// for requests that predate this challenge but may still belong to another one.
func (s OnboardingContactSelector) IsExpired(e OnboardingFriendEvidence) bool {
	return s.RejectExpired && ValidOnboardingContactSelector(s) && e.Name == s.Name &&
		!e.RequestedAt.IsZero() && e.RequestedAt.Year() >= 1 && e.RequestedAt.Year() <= 9999 &&
		!e.RequestedAt.After(s.Since.Add(-10*time.Minute))
}

func (c *Client) OnboardingContact(ctx context.Context, selector OnboardingContactSelector) (OnboardingFriendEvidence, error) {
	proof, _, err := c.onboardingContact(ctx, selector, nil)
	return proof, err
}

func (c *Client) ReconcileOnboardingContact(ctx context.Context, selector OnboardingContactSelector,
	expected OnboardingFriendEvidence) (OnboardingFriendEvidence, error) {
	proof, _, err := c.onboardingContact(ctx, selector, &expected)
	return proof, err
}

func (c *Client) onboardingContact(ctx context.Context, selector OnboardingContactSelector,
	expected *OnboardingFriendEvidence) (OnboardingFriendEvidence, *FriendApplication, error) {
	if !ValidOnboardingContactSelector(selector) {
		return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_contact_invalid")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.mobile {
		return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_contact_desktop_required")
	}
	if len(c.fields) <= 3 {
		return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_contact_invalid")
	}
	self := strconv.FormatUint(c.fields[3], 10)
	if !settingID(self) || expected != nil && (!socialLogID(expected.ApplicationID) ||
		!settingID(expected.SenderID) || expected.SenderID == self || expected.RecipientID != self ||
		expected.Name != "" && expected.Name != selector.Name ||
		selector.RejectExpired && !selector.Matches(*expected) ||
		!expected.RequestedAt.IsZero() && !selector.Matches(OnboardingFriendEvidence{
			Name: selector.Name, RequestedAt: expected.RequestedAt})) {
		return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_contact_invalid")
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
	return parseOnboardingContact(data, self, selector, expected)
}

// Fixed official api/core/friend.proto: FriendApplyItem.apply_time, field 8,
// is google.protobuf.Timestamp. Desktop 2.8.3 index-0329ac55.js reads applyTime.
// Only the official Timestamp JSON string is accepted, never guessed numeric
// units, local dates, application IDs, notes or the local sampling time.
func onboardingContactTimestamp(raw json.RawMessage) (time.Time, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, rejection("native_onboarding_contact_timestamp_unavailable")
	}
	var stamp timestamppb.Timestamp
	if len(raw) > 128 || protojson.Unmarshal(raw, &stamp) != nil || stamp.CheckValid() != nil {
		return time.Time{}, rejection("native_onboarding_contact_timestamp_invalid")
	}
	at := stamp.AsTime().UTC()
	if at.IsZero() {
		return time.Time{}, rejection("native_onboarding_contact_timestamp_invalid")
	}
	return at, nil
}

func parseOnboardingContact(data map[string]json.RawMessage, self string, selector OnboardingContactSelector,
	expected *OnboardingFriendEvidence) (OnboardingFriendEvidence, *FriendApplication, error) {
	var times []struct {
		At json.RawMessage `json:"applyTime"`
	}
	if !settingID(self) || !ValidOnboardingContactSelector(selector) || !utf8.Valid(data["list"]) ||
		json.Unmarshal(data["list"], &times) != nil || times == nil || len(times) > 500 ||
		expected != nil && selector.RejectExpired && !selector.Matches(*expected) {
		return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_contact_shape")
	}
	applications, err := parseFriendApplications(data)
	if err != nil {
		return OnboardingFriendEvidence{}, nil, err
	}
	var proofs [2]OnboardingFriendEvidence
	var selected [2]*FriendApplication
	var matches, pending [2]int
	outsideWindow, processed := false, false
	for i, application := range applications {
		if strings.ContainsRune(application.User.Name, utf8.RuneError) {
			return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_contact_shape")
		}
		if application.Direction != "FROM_HIM" || application.User.ID == self || application.User.Name != selector.Name {
			continue
		}
		state := map[string]string{
			"FRIENDLOG_STATE_UNKNOWN": "unknown", "FRIENDLOG_STATE_AGREE": "approved",
			"FRIENDLOG_STATE_REFUSE": "rejected", "FRIENDLOG_STATE_PROCESSING": "pending",
			"FRIENDLOG_STATE_DELETE": "deleted", "FRIENDLOG_STATE_EXPIRE": "expired",
		}[application.State]
		same := expected != nil && application.ID == expected.ApplicationID &&
			application.User.ID == expected.SenderID && self == expected.RecipientID
		if state != "pending" && !same {
			processed = processed || state == "approved" || state == "rejected" ||
				state == "deleted" || state == "expired"
			continue
		}
		at, err := onboardingContactTimestamp(times[i].At)
		if err != nil {
			return OnboardingFriendEvidence{}, nil, err
		}
		current := OnboardingFriendEvidence{ApplicationID: application.ID, SenderID: application.User.ID,
			RecipientID: self, State: state, Name: application.User.Name, RequestedAt: at}
		if !selector.Matches(current) {
			outsideWindow = outsideWindow || state == "pending"
			continue
		}
		bucket := 0
		if selector.IsExpired(current) {
			bucket = 1
		}
		if state == "pending" {
			pending[bucket]++
		}
		if expected != nil && (!same || expected.Name != "" && expected.Name != current.Name ||
			!expected.RequestedAt.IsZero() && !expected.RequestedAt.Equal(at)) {
			continue
		}
		matches[bucket]++
		proofs[bucket] = current
		copy := application
		selected[bucket] = &copy
	}
	// Discovery prefers current requests; reconciliation keeps the original
	// request's category even if a new current request has since arrived.
	bucket := 0
	if expected == nil && pending[0] == 0 || expected != nil && selector.IsExpired(*expected) {
		bucket = 1
	}
	if matches[bucket] == 0 {
		if expected == nil && outsideWindow {
			return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_contact_outside_window")
		}
		if expected == nil && processed {
			return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_contact_processed")
		}
		return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_contact_not_found")
	}
	proof := proofs[bucket]
	if matches[bucket] != 1 || proof.State == "pending" && pending[bucket] != 1 {
		return OnboardingFriendEvidence{}, nil, rejection("native_onboarding_contact_ambiguous")
	}
	return proof, selected[bucket], nil
}

func (c *Client) ApproveOnboardingContact(ctx context.Context, selector OnboardingContactSelector,
	expected OnboardingFriendEvidence, deadline time.Time, fence OnboardingFriendFence) error {
	return c.changeOnboardingContact(ctx, selector, expected, deadline, fence, false)
}

func (c *Client) RejectOnboardingContact(ctx context.Context, selector OnboardingContactSelector,
	expected OnboardingFriendEvidence, deadline time.Time, fence OnboardingFriendFence) error {
	return c.changeOnboardingContact(ctx, selector, expected, deadline, fence, true)
}

func (c *Client) changeOnboardingContact(ctx context.Context, selector OnboardingContactSelector,
	expected OnboardingFriendEvidence, deadline time.Time, fence OnboardingFriendFence, rejectExpired bool) error {
	if fence == nil || !OnboardingFriendDeadline(deadline, time.Now()) {
		return rejection("native_onboarding_contact_expired")
	}
	work, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	proof, application, err := c.onboardingContact(work, selector, &expected)
	if err != nil {
		return err
	}
	if proof.State != "pending" || selector.IsExpired(proof) != rejectExpired {
		return rejection("native_onboarding_contact_changed")
	}
	kind := "friend_approve"
	if rejectExpired {
		kind = "friend_reject"
	}
	var fenced, submitted atomic.Bool
	err = c.ChangeSocial(work, SettingChange{Kind: kind, Target: proof.SenderID, Text: proof.ApplicationID},
		application, func(current context.Context, submit func(context.Context) error) error {
			if !fenced.CompareAndSwap(false, true) || !OnboardingFriendDeadline(deadline, time.Now()) {
				return rejection("native_onboarding_contact_expired")
			}
			err := fence(current, proof, func(final context.Context) error {
				if !submitted.CompareAndSwap(false, true) {
					return rejection("native_onboarding_contact_already_submitted")
				}
				if err := work.Err(); err != nil {
					return err
				}
				if err := final.Err(); err != nil {
					return err
				}
				if !OnboardingFriendDeadline(deadline, time.Now()) {
					return rejection("native_onboarding_contact_expired")
				}
				bounded, stop := context.WithDeadline(final, deadline)
				stopParent := context.AfterFunc(work, stop)
				defer stopParent()
				defer stop()
				return submit(bounded)
			})
			if err == nil && !submitted.Load() {
				return rejection("native_onboarding_contact_fence_required")
			}
			return err
		})
	if err == nil && !submitted.Load() {
		return rejection("native_onboarding_contact_fence_required")
	}
	return err
}
