package lajiaoliao

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// The fixed desktop app appends this source marker to account-search requests.
// The whole note is 30 characters; it identifies the durable operation, not a
// website identity proof. It is not returned in the evidence projection.
func onboardingOutboundNote(operationID string) (string, error) {
	if len(operationID) != 36 {
		return "", &Failure{Code: "lajiaoliao_onboarding_outbound_invalid"}
	}
	for i, r := range operationID {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			if r != '-' {
				return "", &Failure{Code: "lajiaoliao_onboarding_outbound_invalid"}
			}
		} else if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return "", &Failure{Code: "lajiaoliao_onboarding_outbound_invalid"}
		}
	}
	if strings.ReplaceAll(operationID, "-", "") == "00000000000000000000000000000000" {
		return "", &Failure{Code: "lajiaoliao_onboarding_outbound_invalid"}
	}
	return "\u63a5\u5165" + strings.ReplaceAll(operationID, "-", "")[:20] + "\n[src:1]", nil
}

func validOnboardingOutboundDeadline(deadline time.Time) bool {
	now := time.Now()
	return deadline.After(now) && !deadline.After(now.Add(10*time.Minute))
}

type onboardingOutboundApplication struct {
	From    string `json:"fromUserID"`
	To      string `json:"toUserID"`
	Created int64  `json:"createTime"`
	Result  *int   `json:"handleResult"`
	Note    string `json:"reqMsg"`
}

func (c *Client) onboardingOutboundApplications(ctx context.Context, s Session) ([]onboardingOutboundApplication, bool, error) {
	raw, err := c.request(ctx, "/friend/get_self_friend_apply_list", map[string]any{
		"userID": s.UserID, "pagination": map[string]int{"pageNumber": 1, "showNumber": 100},
	}, s.IMToken, "5", s.DeviceID)
	if err != nil {
		return nil, false, err
	}
	defer clear(raw)
	var result struct {
		Requests json.RawMessage `json:"friendRequests"`
		Total    *int            `json:"total"`
	}
	var rows []onboardingOutboundApplication
	if json.Unmarshal(raw, &result) != nil {
		return nil, false, &Failure{Code: "lajiaoliao_onboarding_outbound_shape"}
	}
	if len(result.Requests) == 0 || strings.TrimSpace(string(result.Requests)) == "null" {
		if onboardingOutboundEmptyApplications(raw) {
			return nil, true, nil
		}
		return nil, false, &Failure{Code: "lajiaoliao_onboarding_outbound_shape"}
	}
	if json.Unmarshal(result.Requests, &rows) != nil || len(rows) > 100 ||
		result.Total != nil && (*result.Total < 0 || len(rows) > *result.Total) {
		return nil, false, &Failure{Code: "lajiaoliao_onboarding_outbound_shape"}
	}
	seen := make(map[string]bool, len(rows))
	for i := range rows {
		row := &rows[i]
		key := row.To + ":" + strconv.FormatInt(row.Created, 10)
		if row.From != s.UserID || !ValidID(row.To) || row.To == s.UserID || row.Created <= 0 ||
			seen[key] || !validText(row.Note, 4096, true) ||
			row.Result != nil && *row.Result != -1 && *row.Result != 0 && *row.Result != 1 {
			return nil, false, &Failure{Code: "lajiaoliao_onboarding_outbound_shape"}
		}
		seen[key] = true
	}
	complete := len(rows) < 100
	if result.Total != nil {
		complete = *result.Total == len(rows)
	}
	return rows, complete, nil
}

func onboardingOutboundEmptyApplications(raw json.RawMessage) bool {
	// request verified this route's successful envelope. The pinned SDK's
	// GetPaginationFriendsApplyFromResp permits omitted repeated/zero fields.
	if onboardingOutboundEmptyFriends(raw) {
		return true
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil || fields == nil {
		return false
	}
	for name := range fields {
		if name != "friendRequests" && name != "total" {
			return false
		}
	}
	if total, present := fields["total"]; present {
		var value *int
		if json.Unmarshal(total, &value) != nil || value == nil || *value != 0 {
			return false
		}
	}
	if requests, present := fields["friendRequests"]; present {
		var rows []json.RawMessage
		if json.Unmarshal(requests, &rows) != nil || len(rows) != 0 {
			return false
		}
	}
	return true
}

// SendOnboardingOutboundFriend re-resolves the confirmed search account and
// checks existing requests before invoking the caller's durable final fence.
// The account's serial write lane and cooldown remain the caller's ownership.
func (c *Client) SendOnboardingOutboundFriend(ctx context.Context, s Session, req OnboardingOutboundRequest,
	fence OnboardingOutboundFence) (OnboardingOutboundEvidence, error) {
	note, err := onboardingOutboundNote(req.OperationID)
	if err != nil || !ValidID(s.UserID) || req.SelfID != s.UserID ||
		!ValidID(req.Target.ID) || !ValidOnboardingOutboundAccount(req.Target.Account) {
		return OnboardingOutboundEvidence{}, &Failure{Code: "lajiaoliao_onboarding_outbound_invalid"}
	}
	if fence == nil {
		return OnboardingOutboundEvidence{}, &Failure{Code: "lajiaoliao_fence_required"}
	}
	if !validOnboardingOutboundDeadline(req.ExpiresAt) {
		return OnboardingOutboundEvidence{}, &Failure{Code: "lajiaoliao_onboarding_outbound_expired"}
	}
	work, cancel := context.WithDeadline(ctx, req.ExpiresAt)
	defer cancel()
	current, err := c.LookupOnboardingOutboundFriend(work, s, req.Target.Account)
	if err != nil {
		return OnboardingOutboundEvidence{}, err
	}
	if current.ID != req.Target.ID {
		return OnboardingOutboundEvidence{}, &Failure{Code: "lajiaoliao_onboarding_outbound_changed"}
	}
	evidence := OnboardingOutboundEvidence{OperationID: req.OperationID, SenderID: s.UserID, RecipientID: current.ID,
		State: "available", IsFriend: current.IsFriend}
	if current.IsFriend {
		evidence.State = "friends"
		return evidence, nil
	}
	rows, complete, err := c.onboardingOutboundApplications(work, s)
	if err != nil {
		return OnboardingOutboundEvidence{}, &OnboardingLookupError{Stage: "sent_requests", Cause: err}
	}
	for _, row := range rows {
		if row.To == current.ID && (row.Result == nil || *row.Result == 0 || row.Note == note) {
			return OnboardingOutboundEvidence{}, &Failure{Code: "lajiaoliao_onboarding_outbound_pending"}
		}
	}
	if !complete {
		return OnboardingOutboundEvidence{}, &Failure{Code: "lajiaoliao_onboarding_outbound_incomplete"}
	}
	var submitted atomic.Bool
	err = fence(work, current, func(final context.Context) error {
		if err := final.Err(); err != nil {
			return err
		}
		if err := work.Err(); err != nil {
			return err
		}
		if !validOnboardingOutboundDeadline(req.ExpiresAt) {
			return &Failure{Code: "lajiaoliao_onboarding_outbound_expired"}
		}
		if !submitted.CompareAndSwap(false, true) {
			return &Failure{Code: "lajiaoliao_onboarding_outbound_already_submitted", Submitted: true}
		}
		submit, stop := context.WithDeadline(final, req.ExpiresAt)
		stopParent := context.AfterFunc(work, stop)
		defer stopParent()
		defer stop()
		raw, err := c.request(submit, "/friend/add_friend", map[string]any{
			"fromUserID": s.UserID, "toUserID": current.ID, "reqMsg": note, "ex": "",
		}, s.IMToken, "5", s.DeviceID)
		clear(raw)
		return err
	})
	if err != nil {
		return OnboardingOutboundEvidence{}, err
	}
	if !submitted.Load() {
		return OnboardingOutboundEvidence{}, &Failure{Code: "lajiaoliao_fence_required"}
	}
	// This is the platform's successful submission receipt, not confirmation of
	// acceptance, private-message delivery or the website applicant's identity.
	evidence.State = "pending"
	return evidence, nil
}

// ReconcileOnboardingOutboundFriend is read-only. Only the stable operation
// note matches this request; an unrelated historical pending request cannot.
// A current friend relationship is separate contact evidence, never identity.
func (c *Client) ReconcileOnboardingOutboundFriend(ctx context.Context, s Session,
	req OnboardingOutboundRequest) (OnboardingOutboundEvidence, error) {
	note, err := onboardingOutboundNote(req.OperationID)
	if err != nil || !ValidID(s.UserID) || req.SelfID != s.UserID || !ValidID(req.Target.ID) ||
		req.Target.ID == s.UserID || !ValidOnboardingOutboundAccount(req.Target.Account) {
		return OnboardingOutboundEvidence{}, &Failure{Code: "lajiaoliao_onboarding_outbound_invalid"}
	}
	friend, err := c.onboardingOutboundIsFriend(ctx, s, req.Target.ID)
	if err != nil {
		return OnboardingOutboundEvidence{}, err
	}
	evidence := OnboardingOutboundEvidence{OperationID: req.OperationID, SenderID: s.UserID,
		RecipientID: req.Target.ID, State: "missing", IsFriend: friend}
	if friend {
		evidence.State = "friends"
		return evidence, nil
	}
	rows, complete, err := c.onboardingOutboundApplications(ctx, s)
	if err != nil {
		return OnboardingOutboundEvidence{}, &OnboardingLookupError{Stage: "sent_requests", Cause: err}
	}
	matches := 0
	for _, row := range rows {
		if row.To != req.Target.ID || row.Note != note {
			continue
		}
		matches++
		evidence.ApplicationID = strconv.FormatInt(row.Created, 10)
		state := 0 // The pinned SDK omits pending's proto3 zero.
		if row.Result != nil {
			state = *row.Result
		}
		evidence.State = map[int]string{-1: "rejected", 0: "pending", 1: "unknown"}[state]
	}
	if matches > 1 {
		return OnboardingOutboundEvidence{}, &Failure{Code: "lajiaoliao_onboarding_outbound_ambiguous"}
	}
	if matches == 0 && !complete {
		return OnboardingOutboundEvidence{}, &Failure{Code: "lajiaoliao_onboarding_outbound_incomplete"}
	}
	return evidence, nil
}
