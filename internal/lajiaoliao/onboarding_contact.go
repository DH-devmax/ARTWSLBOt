package lajiaoliao

import (
	"bytes"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"wangshangliao/internal/nativeauth"
)

func (c *Client) OnboardingContact(ctx context.Context, s Session,
	selector nativeauth.OnboardingContactSelector) (nativeauth.OnboardingFriendEvidence, error) {
	return c.onboardingContact(ctx, s, selector, nil)
}

func (c *Client) ReconcileOnboardingContact(ctx context.Context, s Session,
	selector nativeauth.OnboardingContactSelector, expected nativeauth.OnboardingFriendEvidence) (nativeauth.OnboardingFriendEvidence, error) {
	return c.onboardingContact(ctx, s, selector, &expected)
}

func (c *Client) onboardingContact(ctx context.Context, s Session, selector nativeauth.OnboardingContactSelector,
	expected *nativeauth.OnboardingFriendEvidence) (nativeauth.OnboardingFriendEvidence, error) {
	if !ValidID(s.UserID) || !nativeauth.ValidOnboardingContactSelector(selector) ||
		expected != nil && (!ValidID(expected.SenderID) || expected.SenderID == s.UserID ||
			expected.ApplicationID == "" || expected.RecipientID != s.UserID ||
			expected.Name != "" && expected.Name != selector.Name ||
			selector.RejectExpired && !selector.Matches(*expected) ||
			!expected.RequestedAt.IsZero() && !selector.Matches(nativeauth.OnboardingFriendEvidence{
				Name: selector.Name, RequestedAt: expected.RequestedAt})) {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_invalid"}
	}
	raw, err := c.request(ctx, "/friend/get_friend_apply_list", map[string]any{"userID": s.UserID,
		"pagination": map[string]int{"pageNumber": 1, "showNumber": 100}}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return nativeauth.OnboardingFriendEvidence{}, err
	}
	defer clear(raw)
	return parseOnboardingContact(raw, s.UserID, selector, expected)
}

func onboardingContactTimestamp(raw json.RawMessage) (time.Time, string, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return time.Time{}, "", &Failure{Code: "lajiaoliao_onboarding_contact_timestamp_unavailable"}
	}
	var created int64
	if len(raw) > 32 || json.Unmarshal(raw, &created) != nil || created <= 0 || created > 253402300799999 {
		return time.Time{}, "", &Failure{Code: "lajiaoliao_onboarding_contact_timestamp_invalid"}
	}
	// Fixed desktop 1.0.3 App-24552bef.js, XF, normalizes
	// createTime below 1e11 as seconds and otherwise as milliseconds.
	at := time.UnixMilli(created).UTC()
	if created < 100000000000 {
		at = time.Unix(created, 0).UTC()
	}
	return at, strconv.FormatInt(created, 10), nil
}

func parseOnboardingContact(raw json.RawMessage, self string, selector nativeauth.OnboardingContactSelector,
	expected *nativeauth.OnboardingFriendEvidence) (nativeauth.OnboardingFriendEvidence, error) {
	if !ValidID(self) || !nativeauth.ValidOnboardingContactSelector(selector) ||
		expected != nil && selector.RejectExpired && !selector.Matches(*expected) {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_shape"}
	}
	var result struct {
		Total    *int            `json:"total"`
		Requests json.RawMessage `json:"friendRequests"`
	}
	var rows []struct {
		From    string          `json:"fromUserID"`
		To      string          `json:"toUserID"`
		Name    string          `json:"fromNickname"`
		Created json.RawMessage `json:"createTime"`
		Result  json.RawMessage `json:"handleResult"`
	}
	page := bytes.TrimSpace(raw)
	if len(page) == 0 || page[0] != '{' || !utf8.Valid(raw) {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_directory_invalid"}
	}
	pageErr := json.Unmarshal(raw, &result)
	defer clear(result.Requests)
	if pageErr != nil {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_directory_invalid"}
	}
	if onboardingOutboundEmptyApplications(raw) {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_not_found"}
	}
	if len(result.Requests) == 0 {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_directory_missing"}
	}
	if json.Unmarshal(result.Requests, &rows) != nil {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_directory_rows"}
	}
	// Preserve the legacy SDK's capitalized empty list with an explicit zero
	// total; omitted proto3 defaults were handled by the shared helper above.
	if len(rows) > 100 ||
		len(rows) == 0 && (result.Total == nil || *result.Total != 0) ||
		result.Total != nil && *result.Total < len(rows) {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_directory_count"}
	}
	seen := map[string]bool{}
	pendingBySender := map[string]int{}
	var proofs [2]nativeauth.OnboardingFriendEvidence
	var matches, pending [2]int
	outsideWindow, processed := false, false
	for _, row := range rows {
		state := 0 // The pinned SDK omits the proto3 pending zero value.
		if len(row.Result) > 0 && (string(row.Result) == "null" || json.Unmarshal(row.Result, &state) != nil) {
			return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_directory_state"}
		}
		if state != -1 && state != 0 && state != 1 {
			return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_directory_state"}
		}
		if row.To != self {
			return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_directory_recipient"}
		}
		if !ValidID(row.From) || row.From == self || !validText(row.Name, 1000, true) ||
			strings.ContainsRune(row.Name, utf8.RuneError) || strings.ContainsRune(row.From, utf8.RuneError) {
			return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_directory_identity"}
		}
		at, id, err := onboardingContactTimestamp(row.Created)
		if err != nil {
			return nativeauth.OnboardingFriendEvidence{}, err
		}
		key := row.From + ":" + id
		if seen[key] {
			return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_ambiguous"}
		}
		seen[key] = true
		if state == 0 {
			pendingBySender[row.From]++
		}
		current := nativeauth.OnboardingFriendEvidence{ApplicationID: id, SenderID: row.From, RecipientID: self,
			State: map[int]string{-1: "rejected", 0: "pending", 1: "approved"}[state],
			Name:  row.Name, RequestedAt: at}
		if row.Name != selector.Name {
			continue
		}
		if expected == nil && state != 0 {
			processed = true
			continue
		}
		if !selector.Matches(current) {
			outsideWindow = outsideWindow || state == 0
			continue
		}
		bucket := 0
		if selector.IsExpired(current) {
			bucket = 1
		}
		if state == 0 {
			pending[bucket]++
		}
		if expected != nil && (id != expected.ApplicationID || row.From != expected.SenderID || self != expected.RecipientID ||
			expected.Name != "" && expected.Name != current.Name ||
			!expected.RequestedAt.IsZero() && !expected.RequestedAt.Equal(at)) {
			continue
		}
		matches[bucket]++
		proofs[bucket] = current
	}
	// Discovery prefers current requests; reconciliation never replaces the
	// original request with one from the other time category.
	bucket := 0
	if expected == nil && pending[0] == 0 || expected != nil && selector.IsExpired(*expected) {
		bucket = 1
	}
	if matches[bucket] == 0 {
		if expected == nil && outsideWindow {
			return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_outside_window"}
		}
		if expected == nil && processed {
			return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_processed"}
		}
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_not_found"}
	}
	proof := proofs[bucket]
	// The mutation identifies only sender/recipient. Pending requests outside
	// this nickname/time selector still compete on that same wire target.
	if matches[bucket] != 1 || proof.State == "pending" && (pending[bucket] != 1 || pendingBySender[proof.SenderID] != 1) {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_ambiguous"}
	}
	if proof.State == "pending" && (result.Total != nil && *result.Total > len(rows) ||
		result.Total == nil && len(rows) == 100) {
		return nativeauth.OnboardingFriendEvidence{}, &Failure{Code: "lajiaoliao_onboarding_contact_incomplete"}
	}
	return proof, nil
}

func (c *Client) ApproveOnboardingContact(ctx context.Context, s Session, selector nativeauth.OnboardingContactSelector,
	expected nativeauth.OnboardingFriendEvidence, deadline time.Time, fence nativeauth.OnboardingFriendFence) error {
	return c.changeOnboardingContact(ctx, s, selector, expected, deadline, fence, false)
}

func (c *Client) RejectOnboardingContact(ctx context.Context, s Session, selector nativeauth.OnboardingContactSelector,
	expected nativeauth.OnboardingFriendEvidence, deadline time.Time, fence nativeauth.OnboardingFriendFence) error {
	return c.changeOnboardingContact(ctx, s, selector, expected, deadline, fence, true)
}

func (c *Client) changeOnboardingContact(ctx context.Context, s Session, selector nativeauth.OnboardingContactSelector,
	expected nativeauth.OnboardingFriendEvidence, deadline time.Time, fence nativeauth.OnboardingFriendFence,
	rejectExpired bool) error {
	if fence == nil || !nativeauth.OnboardingFriendDeadline(deadline, time.Now()) {
		return &Failure{Code: "lajiaoliao_onboarding_contact_expired"}
	}
	work, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	proof, err := c.onboardingContact(work, s, selector, &expected)
	if err != nil {
		return err
	}
	if proof.State != "pending" || selector.IsExpired(proof) != rejectExpired {
		return &Failure{Code: "lajiaoliao_onboarding_contact_changed"}
	}
	kind := "friend_approve"
	if rejectExpired {
		kind = "friend_reject"
	}
	var fenced, submitted atomic.Bool
	err = c.Execute(work, s, Change{Kind: kind, Target: proof.SenderID, Text: proof.ApplicationID},
		func(current context.Context, submit func(context.Context) error) error {
			if !fenced.CompareAndSwap(false, true) || !nativeauth.OnboardingFriendDeadline(deadline, time.Now()) {
				return &Failure{Code: "lajiaoliao_onboarding_contact_expired"}
			}
			err := fence(current, proof, func(final context.Context) error {
				if !submitted.CompareAndSwap(false, true) {
					return &Failure{Code: "lajiaoliao_onboarding_contact_already_submitted"}
				}
				if err := work.Err(); err != nil {
					return err
				}
				if err := final.Err(); err != nil {
					return err
				}
				if !nativeauth.OnboardingFriendDeadline(deadline, time.Now()) {
					return &Failure{Code: "lajiaoliao_onboarding_contact_expired"}
				}
				bounded, stop := context.WithDeadline(final, deadline)
				stopParent := context.AfterFunc(work, stop)
				defer stopParent()
				defer stop()
				return submit(bounded)
			})
			if err == nil && !submitted.Load() {
				return &Failure{Code: "lajiaoliao_onboarding_contact_fence_required"}
			}
			return err
		})
	if err == nil && !submitted.Load() {
		return &Failure{Code: "lajiaoliao_onboarding_contact_fence_required"}
	}
	return err
}
