package nativeauth

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"wangshangliao/internal/nativewire"
)

const (
	onboardingOutboundSearchRoute = "/v1/friend/search-conversation"
	onboardingOutboundInfoRoute   = "/v1/friend/get-friend-info"
	onboardingOutboundApplyRoute  = "/v1/friend/add-friend-apply"
)

// OnboardingOutboundTarget is a bounded projection, not identity or group proof.
type OnboardingOutboundTarget struct {
	ID            string `json:"id"`
	Account       string `json:"account"`
	Name          string `json:"name"`
	State         string `json:"state"`
	FriendPolicy  string `json:"friend_policy"`
	IsFriend      bool   `json:"is_friend"`
	CanRequest    bool   `json:"can_request"`
	BlockedReason string `json:"blocked_reason,omitempty"`
}

// IsFriend requires a current mutual relationship. An approved historical
// application alone does not set it or prove the website applicant's identity.
type OnboardingOutboundEvidence struct {
	OperationID   string `json:"operation_id"`
	ApplicationID string `json:"application_id,omitempty"`
	SenderID      string `json:"sender_id"`
	RecipientID   string `json:"recipient_id"`
	State         string `json:"state"`
	IsFriend      bool   `json:"is_friend"`
}

type OnboardingOutboundRequest struct {
	OperationID string
	SelfID      string
	Target      OnboardingOutboundTarget
	ExpiresAt   time.Time
}

type OnboardingOutboundFence func(context.Context, OnboardingOutboundTarget, func(context.Context) error) error

func onboardingOutboundRoute(route string) bool {
	return route == onboardingOutboundSearchRoute || route == onboardingOutboundInfoRoute ||
		route == onboardingOutboundApplyRoute
}

// The account is ultimately encoded as desktop Identity.id (int32). Do not
// silently strip text, reinterpret it as a mobile number, or round a JSON ID.
func ValidOnboardingOutboundAccount(account string) bool {
	n, err := strconv.ParseUint(account, 10, 31)
	return err == nil && n > 0 && strconv.FormatUint(n, 10) == account
}

func onboardingOutboundNote(operation string) (string, error) {
	if len(operation) != 36 || operation[8] != '-' || operation[13] != '-' || operation[18] != '-' || operation[23] != '-' {
		return "", rejection("native_onboarding_outbound_operation_invalid")
	}
	compact := operation[:8] + operation[9:13] + operation[14:18] + operation[19:23] + operation[24:]
	id, err := hex.DecodeString(compact)
	if err != nil || len(id) != 16 || hex.EncodeToString(id) != compact || compact == "00000000000000000000000000000000" {
		return "", rejection("native_onboarding_outbound_operation_invalid")
	}
	// The pinned desktop limits applyMsg to 20 UTF-16 code units. The marker
	// identifies an operation within its exact sender/recipient scope, not a user.
	return "群接入 " + compact[:8] + compact[24:], nil
}

func (c *Client) LookupOnboardingOutboundFriend(ctx context.Context, account string) (OnboardingOutboundTarget, error) {
	if !ValidOnboardingOutboundAccount(account) {
		return OnboardingOutboundTarget{}, rejection("native_onboarding_outbound_account_invalid")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lookupOnboardingOutboundFriend(ctx, account)
}

func clearOnboardingOutboundData(data map[string]json.RawMessage) {
	for _, raw := range data {
		clear(raw)
	}
}

func (c *Client) lookupOnboardingOutboundFriend(ctx context.Context, account string) (OnboardingOutboundTarget, error) {
	self := strconv.FormatUint(c.fields[3], 10)
	if !settingID(self) {
		return OnboardingOutboundTarget{}, rejection("native_onboarding_outbound_identity")
	}
	if c.onboardingPeer != nil {
		return c.lookupAuthenticatedOnboardingPeer(ctx, account)
	}
	var qrcodeID any = "0"
	if c.mobile {
		qrcodeID = 0
	}
	data, err := c.request(ctx, onboardingOutboundSearchRoute, map[string]any{
		"key": account, "qrcodeId": qrcodeID, "type": "CONVERSATION_TYPE_SEARCH",
	}, nil)
	if err != nil {
		return OnboardingOutboundTarget{}, &OnboardingLookupError{Stage: "search", Cause: err}
	}
	target, relation, err := parseOnboardingOutboundSearch(data, self, account)
	clearOnboardingOutboundData(data)
	if err != nil {
		return OnboardingOutboundTarget{}, &OnboardingLookupError{Stage: "search", Cause: err}
	}
	uid, _ := strconv.ParseUint(target.ID, 10, 30)
	info, err := c.request(ctx, onboardingOutboundInfoRoute, map[string]any{
		"uid": uid, "showTempChat": false,
	}, nil)
	if err != nil {
		return OnboardingOutboundTarget{}, &OnboardingLookupError{Stage: "friend_info", Cause: err}
	}
	defer clearOnboardingOutboundData(info)
	target, err = parseOnboardingOutboundInfo(info, target, relation, c.mobile)
	if err != nil {
		return OnboardingOutboundTarget{}, onboardingLookupError("friend_info", err)
	}
	return target, nil
}

func parseOnboardingOutboundSearch(data map[string]json.RawMessage, self, account string) (OnboardingOutboundTarget, string, error) {
	var rows []struct {
		ID       json.RawMessage `json:"id"`
		Origin   json.RawMessage `json:"originId"`
		Account  json.RawMessage `json:"account"`
		Name     json.RawMessage `json:"name"`
		Relation json.RawMessage `json:"relation"`
		Source   json.RawMessage `json:"friendSearchType"`
	}
	if !settingID(self) || !ValidOnboardingOutboundAccount(account) || !utf8.Valid(data["items"]) ||
		json.Unmarshal(data["items"], &rows) != nil || rows == nil || len(rows) > 100 {
		return OnboardingOutboundTarget{}, "", rejection("native_onboarding_outbound_shape")
	}
	var selected OnboardingOutboundTarget
	var selectedRelation string
	matches := 0
	for _, row := range rows {
		relation := onboardingOutboundEnum(row.Relation, []string{
			"CONVERSATION_RELATION_UNKNOWN", "CONVERSATION_RELATION_FRIEND",
			"CONVERSATION_RELATION_STRANGER_USER", "CONVERSATION_RELATION_BLACK",
			"CONVERSATION_RELATION_GROUP", "CONVERSATION_RELATION_STRANGER_GROUP",
		})
		if relation == "" {
			return OnboardingOutboundTarget{}, "", rejection("native_onboarding_outbound_shape")
		}
		if relation != "CONVERSATION_RELATION_FRIEND" && relation != "CONVERSATION_RELATION_STRANGER_USER" &&
			relation != "CONVERSATION_RELATION_BLACK" || directoryID(row.Account) != account {
			continue
		}
		id := directoryID(row.ID)
		source := onboardingOutboundEnum(row.Source, []string{
			"FRIEND_SRC_UNKNOWN", "FRIEND_SRC_QR", "FRIEND_SRC_MEMBER", "FRIEND_SRC_MOBIL",
			"FRIEND_SRC_CARD", "FRIEND_SRC_APPLY", "FRIEND_SRC_GROUP", "FRIEND_SRC_FRIEND_VALID",
		})
		name, err := socialName(row.Name)
		if err != nil || !settingID(id) || len(row.Origin) > 0 && directoryID(row.Origin) != id ||
			source != "FRIEND_SRC_MEMBER" {
			return OnboardingOutboundTarget{}, "", rejection("native_onboarding_outbound_shape")
		}
		matches++
		selected, selectedRelation = OnboardingOutboundTarget{ID: id, Account: account, Name: name}, relation
	}
	if matches == 0 {
		return OnboardingOutboundTarget{}, "", rejection("native_onboarding_outbound_not_found")
	}
	if matches != 1 {
		return OnboardingOutboundTarget{}, "", rejection("native_onboarding_outbound_ambiguous")
	}
	if selected.ID == self {
		return OnboardingOutboundTarget{}, "", rejection("native_onboarding_outbound_self")
	}
	return selected, selectedRelation, nil
}

func onboardingOutboundEnum(raw json.RawMessage, names []string) string {
	// encoding/json accepts null into an int as zero, which is a valid
	// relationship, policy or direction. Require an explicit enum value.
	if bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	return socialEnum(raw, names)
}

func onboardingOutboundState(raw json.RawMessage) string {
	return onboardingOutboundEnum(raw, []string{
		"FRIEND_STATE_NONE", "FRIEND_STATE_APPLIED", "FRIEND_STATE_PENDING", "FRIEND_STATE_FAILED",
		"FRIEND_STATE_REJECTED", "FRIEND_STATE_GOOD", "FRIEND_STATE_DELETED", "FRIEND_STATE_BLACKLIST",
	})
}

func onboardingOutboundIdentityReason(raw json.RawMessage, expected string) string {
	raw = bytes.TrimSpace(raw)
	switch {
	case len(raw) == 0:
		return "missing"
	case bytes.Equal(raw, []byte("null")):
		return "null"
	case bytes.Equal(raw, []byte("0")) || bytes.Equal(raw, []byte(`"0"`)):
		return "zero"
	}
	actual := directoryID(raw)
	if actual == "" {
		return "invalid"
	}
	if actual != expected {
		return "mismatch"
	}
	return ""
}

func onboardingOutboundIdentityError(reason string) error {
	return &OnboardingLookupError{Stage: "friend_info", Reason: SafeOnboardingLookupReason(reason),
		Cause: rejection("native_onboarding_outbound_changed")}
}

func parseOnboardingOutboundInfo(data map[string]json.RawMessage, target OnboardingOutboundTarget, relation string, allowDefaultAccount bool) (OnboardingOutboundTarget, error) {
	if !settingID(target.ID) || !ValidOnboardingOutboundAccount(target.Account) {
		return OnboardingOutboundTarget{}, rejection("native_onboarding_outbound_changed")
	}
	if reason := onboardingOutboundIdentityReason(data["uid"], target.ID); reason != "" {
		return OnboardingOutboundTarget{}, onboardingOutboundIdentityError("friend_info_uid_" + reason)
	}
	// Mobile search or a sealed authenticated-peer mapping can independently
	// bind the public account to the UID when details return accountId=0.
	// The UID must still match; a conflicting nonzero account is never accepted.
	if reason := onboardingOutboundIdentityReason(data["accountId"], target.Account); reason != "" &&
		!(allowDefaultAccount && reason == "zero") {
		return OnboardingOutboundTarget{}, onboardingOutboundIdentityError("friend_info_account_" + reason)
	}
	name, err := socialName(data["nickName"])
	own, peer := onboardingOutboundState(data["state"]), onboardingOutboundState(data["state1"])
	policy := onboardingOutboundEnum(data["friendPolicy"], []string{
		"FRIEND_NEED_APPROVAL", "FRIEND_ANY_ONE", "FRIEND_ANSWER_CHECK", "FRIEND_ASK_ONLY",
	})
	if string(data["friendPolicy"]) == `"FRIEND_ANY_REFUSE"` {
		policy = "FRIEND_ANY_REFUSE"
	}
	var forbidden bool
	if err != nil || own == "" || peer == "" || policy == "" ||
		len(data["forbidAddFriend"]) > 0 && (bytes.Equal(bytes.TrimSpace(data["forbidAddFriend"]), []byte("null")) ||
			json.Unmarshal(data["forbidAddFriend"], &forbidden) != nil) {
		return OnboardingOutboundTarget{}, rejection("native_onboarding_outbound_shape")
	}
	target.Name, target.FriendPolicy = name, policy
	target.State = "stranger"
	switch {
	case relation == "CONVERSATION_RELATION_BLACK" || own == "FRIEND_STATE_BLACKLIST" || peer == "FRIEND_STATE_BLACKLIST":
		target.State, target.BlockedReason = "blocked", "native_onboarding_outbound_blocked"
	case own == "FRIEND_STATE_GOOD" && peer == "FRIEND_STATE_GOOD":
		target.State, target.IsFriend = "friends", true
	case own == "FRIEND_STATE_APPLIED" || own == "FRIEND_STATE_NONE" && peer == "FRIEND_STATE_PENDING":
		target.State, target.BlockedReason = "outgoing_pending", "native_onboarding_outbound_pending"
	case own == "FRIEND_STATE_PENDING" || peer == "FRIEND_STATE_APPLIED":
		target.State, target.BlockedReason = "incoming_pending", "native_onboarding_outbound_incoming"
	case own == "FRIEND_STATE_GOOD" || peer == "FRIEND_STATE_GOOD" || peer == "FRIEND_STATE_PENDING":
		target.State, target.BlockedReason = "unavailable", "native_onboarding_outbound_read_incomplete"
	case forbidden || policy != "FRIEND_NEED_APPROVAL" && policy != "FRIEND_ANY_ONE":
		target.BlockedReason = "native_onboarding_outbound_policy"
	default:
		target.CanRequest = true
	}
	return target, nil
}

// SendOnboardingOutboundFriend performs fresh exact-account and policy reads,
// then exposes one encoded HTTP submission to the caller's durable final fence.
// It neither sends a greeting chat message nor retries an uncertain application.
func (c *Client) SendOnboardingOutboundFriend(ctx context.Context, request OnboardingOutboundRequest,
	fence OnboardingOutboundFence) (OnboardingOutboundEvidence, error) {
	expected, deadline := request.Target, request.ExpiresAt
	text, err := onboardingOutboundNote(request.OperationID)
	if err != nil {
		return OnboardingOutboundEvidence{}, err
	}
	if !settingID(expected.ID) || !ValidOnboardingOutboundAccount(expected.Account) || !settingID(request.SelfID) {
		return OnboardingOutboundEvidence{}, rejection("native_onboarding_outbound_invalid")
	}
	if fence == nil {
		return OnboardingOutboundEvidence{}, rejection("native_setting_fence_required")
	}
	if !OnboardingFriendDeadline(deadline, time.Now()) {
		return OnboardingOutboundEvidence{}, rejection("native_onboarding_outbound_expired")
	}
	work, cancel := context.WithDeadline(ctx, deadline)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if request.SelfID != strconv.FormatUint(c.fields[3], 10) {
		return OnboardingOutboundEvidence{}, rejection("native_onboarding_outbound_identity")
	}
	target, err := c.lookupOnboardingOutboundFriend(work, expected.Account)
	if err != nil {
		return OnboardingOutboundEvidence{}, err
	}
	if target.ID != expected.ID || target.Account != expected.Account {
		return OnboardingOutboundEvidence{}, rejection("native_onboarding_outbound_changed")
	}
	evidence := OnboardingOutboundEvidence{
		OperationID: request.OperationID, SenderID: request.SelfID, RecipientID: target.ID,
	}
	if target.IsFriend {
		evidence.State, evidence.IsFriend = "friends", true
		return evidence, nil
	}
	if !target.CanRequest {
		return OnboardingOutboundEvidence{}, rejection(target.BlockedReason)
	}
	account, _ := strconv.ParseUint(target.Account, 10, 31)
	params := map[string]any{
		"friendId": map[string]any{"id": account, "idType": "ID_TP_ACCOUNT"},
		"addType":  "FRIEND_SRC_MEMBER", "applyMsg": text, "nickName": "", "classId": 0,
		"friendCircle": map[string]bool{"notSeeMe": true, "notSeeHim": true},
		"friendPolicy": target.FriendPolicy,
	}
	if c.mobile {
		// Android ApplyAddFriendViewModel passes these Gson defaults explicitly.
		params["uid"], params["questions"], params["fromGroupId"] = 0, []any{}, 0
	}
	var submitted atomic.Bool
	finalFence := func(check context.Context, submit func(context.Context) error) error {
		if !OnboardingFriendDeadline(deadline, time.Now()) {
			return rejection("native_onboarding_outbound_expired")
		}
		err := fence(check, target, func(final context.Context) error {
			if !submitted.CompareAndSwap(false, true) {
				return rejection("native_onboarding_outbound_already_submitted")
			}
			if err := final.Err(); err != nil {
				return err
			}
			if !OnboardingFriendDeadline(deadline, time.Now()) {
				return rejection("native_onboarding_outbound_expired")
			}
			return submit(final)
		})
		if err == nil && !submitted.Load() {
			return rejection("native_onboarding_outbound_not_submitted")
		}
		return err
	}
	data, err := c.request(context.WithValue(work, settingFenceKey{}, nativewire.SubmissionFence(finalFence)),
		onboardingOutboundApplyRoute, params, nil)
	if err != nil {
		return OnboardingOutboundEvidence{}, err
	}
	defer clearOnboardingOutboundData(data)
	result, err := parseOnboardingOutboundApply(data, evidence)
	if err != nil {
		return OnboardingOutboundEvidence{}, &Error{Code: "native_response_invalid", Submitted: true}
	}
	return result, nil
}

func parseOnboardingOutboundApply(data map[string]json.RawMessage, evidence OnboardingOutboundEvidence) (OnboardingOutboundEvidence, error) {
	own, peer := onboardingOutboundState(data["state"]), onboardingOutboundState(data["state1"])
	id := directoryID(data["flId"])
	if own == "FRIEND_STATE_GOOD" && peer == "FRIEND_STATE_GOOD" &&
		(len(data["flId"]) == 0 || string(data["flId"]) == `"0"` || string(data["flId"]) == "0" || socialLogID(id)) {
		evidence.ApplicationID, evidence.State, evidence.IsFriend = id, "friends", true
		return evidence, nil
	}
	if !socialLogID(id) || peer != "FRIEND_STATE_PENDING" ||
		own != "FRIEND_STATE_APPLIED" && own != "FRIEND_STATE_NONE" {
		return OnboardingOutboundEvidence{}, rejection("native_onboarding_outbound_shape")
	}
	evidence.ApplicationID, evidence.State = id, "pending"
	return evidence, nil
}

// ReconcileOnboardingOutboundFriend only reads. A pending relationship cannot
// identify an operation without its exact marker in the outbound server window.
func (c *Client) ReconcileOnboardingOutboundFriend(ctx context.Context, request OnboardingOutboundRequest) (OnboardingOutboundEvidence, error) {
	expected := request.Target
	note, err := onboardingOutboundNote(request.OperationID)
	if err != nil {
		return OnboardingOutboundEvidence{}, err
	}
	if !settingID(expected.ID) || !ValidOnboardingOutboundAccount(expected.Account) ||
		!settingID(request.SelfID) {
		return OnboardingOutboundEvidence{}, rejection("native_onboarding_outbound_invalid")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if request.SelfID != strconv.FormatUint(c.fields[3], 10) {
		return OnboardingOutboundEvidence{}, rejection("native_onboarding_outbound_identity")
	}
	target, err := c.lookupOnboardingOutboundFriend(ctx, expected.Account)
	if err != nil {
		return OnboardingOutboundEvidence{}, err
	}
	if target.ID != expected.ID || target.Account != expected.Account {
		return OnboardingOutboundEvidence{}, rejection("native_onboarding_outbound_changed")
	}
	result := OnboardingOutboundEvidence{
		OperationID: request.OperationID, SenderID: request.SelfID, RecipientID: expected.ID, State: "unknown",
	}
	if target.IsFriend {
		result.State, result.IsFriend = "friends", true
		return result, nil
	}
	data, err := c.request(ctx, "/v1/friend/friend-apply-list", map[string]any{"lastId": "0"}, nil)
	if err != nil {
		return OnboardingOutboundEvidence{}, err
	}
	defer clearOnboardingOutboundData(data)
	return parseOnboardingOutboundApplication(data, result, note)
}

func parseOnboardingOutboundApplication(data map[string]json.RawMessage, result OnboardingOutboundEvidence,
	note string) (OnboardingOutboundEvidence, error) {
	var notes []struct {
		Note      json.RawMessage `json:"applyMsg"`
		Direction json.RawMessage `json:"direction"`
		State     json.RawMessage `json:"state"`
	}
	if !utf8.Valid(data["list"]) || json.Unmarshal(data["list"], &notes) != nil || notes == nil || len(notes) > 500 {
		return OnboardingOutboundEvidence{}, rejection("native_onboarding_outbound_shape")
	}
	rows, err := parseFriendApplications(data)
	if err != nil {
		return OnboardingOutboundEvidence{}, err
	}
	matches := 0
	for i, row := range rows {
		var text string
		if bytes.Equal(bytes.TrimSpace(notes[i].Direction), []byte("null")) ||
			bytes.Equal(bytes.TrimSpace(notes[i].State), []byte("null")) ||
			len(notes[i].Note) > 0 && json.Unmarshal(notes[i].Note, &text) != nil ||
			!utf8.ValidString(text) || len(text) > 4096 {
			return OnboardingOutboundEvidence{}, rejection("native_onboarding_outbound_shape")
		}
		if text != note || row.Direction != "FROM_ME" || row.User.ID != result.RecipientID ||
			row.User.ID == result.SenderID {
			continue
		}
		matches++
		if matches > 1 {
			return OnboardingOutboundEvidence{}, rejection("native_onboarding_outbound_ambiguous")
		}
		result.ApplicationID = row.ID
		result.State = map[string]string{
			"FRIENDLOG_STATE_UNKNOWN": "unknown", "FRIENDLOG_STATE_PROCESSING": "pending",
			"FRIENDLOG_STATE_AGREE": "approved", "FRIENDLOG_STATE_REFUSE": "rejected",
			"FRIENDLOG_STATE_DELETE": "deleted", "FRIENDLOG_STATE_EXPIRE": "expired",
		}[row.State]
	}
	return result, nil
}
