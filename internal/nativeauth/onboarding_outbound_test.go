package nativeauth

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

const onboardingOutboundTestOperation = "12345678-1234-4234-8234-123456789abc"

func onboardingOutboundData(t *testing.T, value any) map[string]json.RawMessage {
	t.Helper()
	body, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	var data map[string]json.RawMessage
	if err := json.Unmarshal(body, &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func onboardingOutboundSearchRow() map[string]any {
	return map[string]any{
		"id": 8, "originId": 8, "account": "340738649", "name": "synthetic-peer",
		"relation": "CONVERSATION_RELATION_STRANGER_USER", "friendSearchType": "FRIEND_SRC_MEMBER",
	}
}

func onboardingOutboundInfoRow() map[string]any {
	return map[string]any{
		"uid": 8, "accountId": 340738649, "nickName": "synthetic-peer",
		"state": "FRIEND_STATE_NONE", "state1": "FRIEND_STATE_NONE", "friendPolicy": "FRIEND_NEED_APPROVAL",
	}
}

func TestOnboardingOutboundWSLExactAccountAndProjection(t *testing.T) {
	row := onboardingOutboundSearchRow()
	group := onboardingOutboundSearchRow()
	group["relation"], group["id"] = "CONVERSATION_RELATION_GROUP", 99
	data := onboardingOutboundData(t, map[string]any{"items": []any{group, row}})
	target, relation, err := parseOnboardingOutboundSearch(data, "7", "340738649")
	if err != nil || target.ID != "8" || target.Account != "340738649" || target.Name != "synthetic-peer" {
		t.Fatal("exact account did not select a single user", target, err)
	}
	info := onboardingOutboundInfoRow()
	info["phone"], info["jwtToken"], info["description"] = "synthetic-private", "synthetic-secret", "unrelated"
	target, err = parseOnboardingOutboundInfo(onboardingOutboundData(t, info), target, relation, false)
	if err != nil || !target.CanRequest || target.IsFriend || target.State != "stranger" {
		t.Fatal("fresh stranger projection was wrong", target, err)
	}
	raw, _ := json.Marshal(target)
	for _, forbidden := range []string{"phone", "jwtToken", "description", "synthetic-private", "synthetic-secret"} {
		if strings.Contains(string(raw), forbidden) {
			t.Fatal("unrelated profile escaped projection")
		}
	}
	for _, account := range []string{"", " 340738649", "340738649 ", "+340738649", "0340738649",
		"1.2", "1e3", "17017872008", "2147483648", "account340738649", "0"} {
		if ValidOnboardingOutboundAccount(account) {
			t.Fatal("noncanonical or non-int32 account admitted", account)
		}
	}
	for _, account := range []string{"1", "340738649", "2147483647"} {
		if !ValidOnboardingOutboundAccount(account) {
			t.Fatal("valid int32 account rejected", account)
		}
	}
}

func TestOnboardingOutboundWSLRejectsWrongOrAmbiguousSearchTarget(t *testing.T) {
	for _, mode := range []string{"different_account", "group_only", "two_users", "self", "mobile_source",
		"wrong_origin", "bad_uid", "bad_name", "wrong_relation", "over_limit", "missing_items"} {
		t.Run(mode, func(t *testing.T) {
			row := onboardingOutboundSearchRow()
			rows := []any{row}
			switch mode {
			case "different_account":
				row["account"] = "340738650"
			case "group_only":
				row["relation"] = "CONVERSATION_RELATION_STRANGER_GROUP"
			case "two_users":
				rows = append(rows, onboardingOutboundSearchRow())
			case "self":
				row["id"], row["originId"] = 7, 7
			case "mobile_source":
				row["friendSearchType"] = "FRIEND_SRC_MOBIL"
			case "wrong_origin":
				row["originId"] = 9
			case "bad_uid":
				row["id"] = "08"
			case "bad_name":
				row["name"] = "unsafe\nname"
			case "wrong_relation":
				row["relation"] = "FUTURE_USER_TYPE"
			case "over_limit":
				rows = make([]any, 101)
			case "missing_items":
				rows = nil
			}
			if _, _, err := parseOnboardingOutboundSearch(
				onboardingOutboundData(t, map[string]any{"items": rows}), "7", "340738649"); err == nil {
				t.Fatal("unverified search target accepted")
			}
		})
	}
	invalid := map[string]json.RawMessage{"items": []byte("[\"\xff\"]")}
	if _, _, err := parseOnboardingOutboundSearch(invalid, "7", "340738649"); err == nil {
		t.Fatal("invalid UTF-8 search result admitted")
	}
}

func TestOnboardingOutboundWSLCurrentRelationshipAndPolicy(t *testing.T) {
	target := OnboardingOutboundTarget{ID: "8", Account: "340738649"}
	for _, tc := range []struct {
		own, peer, policy, want string
		friend, send            bool
	}{
		{"FRIEND_STATE_NONE", "FRIEND_STATE_NONE", "FRIEND_NEED_APPROVAL", "stranger", false, true},
		{"FRIEND_STATE_NONE", "FRIEND_STATE_NONE", "FRIEND_ANY_ONE", "stranger", false, true},
		{"FRIEND_STATE_GOOD", "FRIEND_STATE_GOOD", "FRIEND_NEED_APPROVAL", "friends", true, false},
		{"FRIEND_STATE_GOOD", "FRIEND_STATE_DELETED", "FRIEND_NEED_APPROVAL", "unavailable", false, false},
		{"FRIEND_STATE_APPLIED", "FRIEND_STATE_PENDING", "FRIEND_NEED_APPROVAL", "outgoing_pending", false, false},
		{"FRIEND_STATE_NONE", "FRIEND_STATE_PENDING", "FRIEND_NEED_APPROVAL", "outgoing_pending", false, false},
		{"FRIEND_STATE_PENDING", "FRIEND_STATE_APPLIED", "FRIEND_NEED_APPROVAL", "incoming_pending", false, false},
		{"FRIEND_STATE_BLACKLIST", "FRIEND_STATE_NONE", "FRIEND_NEED_APPROVAL", "blocked", false, false},
		{"FRIEND_STATE_NONE", "FRIEND_STATE_BLACKLIST", "FRIEND_NEED_APPROVAL", "blocked", false, false},
		{"FRIEND_STATE_NONE", "FRIEND_STATE_NONE", "FRIEND_ANSWER_CHECK", "stranger", false, false},
		{"FRIEND_STATE_NONE", "FRIEND_STATE_NONE", "FRIEND_ASK_ONLY", "stranger", false, false},
		{"FRIEND_STATE_NONE", "FRIEND_STATE_NONE", "FRIEND_ANY_REFUSE", "stranger", false, false},
	} {
		t.Run(tc.own+"/"+tc.peer+"/"+tc.policy, func(t *testing.T) {
			info := onboardingOutboundInfoRow()
			info["state"], info["state1"], info["friendPolicy"] = tc.own, tc.peer, tc.policy
			got, err := parseOnboardingOutboundInfo(onboardingOutboundData(t, info), target, "CONVERSATION_RELATION_STRANGER_USER", false)
			if err != nil || got.State != tc.want || got.IsFriend != tc.friend || got.CanRequest != tc.send {
				t.Fatal("relationship or policy confused", got, err)
			}
		})
	}
	for _, field := range []string{"uid", "accountId", "state", "state1", "friendPolicy", "forbidAddFriend"} {
		info := onboardingOutboundInfoRow()
		info[field] = "malformed"
		if _, err := parseOnboardingOutboundInfo(onboardingOutboundData(t, info), target, "CONVERSATION_RELATION_STRANGER_USER", false); err == nil {
			t.Fatal("unverified current profile accepted", field)
		}
	}
	info := onboardingOutboundInfoRow()
	info["forbidAddFriend"] = true
	if got, err := parseOnboardingOutboundInfo(onboardingOutboundData(t, info), target, "CONVERSATION_RELATION_STRANGER_USER", false); err != nil || got.CanRequest {
		t.Fatal("ignored explicit friend rejection policy", got, err)
	}
}

func TestOnboardingOutboundWSLNullDoesNotBecomeRelationshipOrDirectionZero(t *testing.T) {
	target := OnboardingOutboundTarget{ID: "8", Account: "340738649"}
	base := OnboardingOutboundEvidence{
		OperationID: onboardingOutboundTestOperation, SenderID: "7", RecipientID: "8", State: "unknown",
	}
	for _, field := range []string{"state", "state1", "friendPolicy", "forbidAddFriend"} {
		t.Run("info/"+field, func(t *testing.T) {
			info := onboardingOutboundInfoRow()
			info[field] = nil
			if _, err := parseOnboardingOutboundInfo(onboardingOutboundData(t, info), target,
				"CONVERSATION_RELATION_STRANGER_USER", false); err == nil {
				t.Fatal("null profile evidence became permission to request")
			}
		})
	}
	for _, field := range []string{"state", "state1"} {
		t.Run("apply/"+field, func(t *testing.T) {
			response := map[string]any{"flId": "11", "state": "FRIEND_STATE_APPLIED", "state1": "FRIEND_STATE_PENDING"}
			response[field] = nil
			if _, err := parseOnboardingOutboundApply(onboardingOutboundData(t, response), base); err == nil {
				t.Fatal("null response state became a pending submission receipt")
			}
		})
	}
	note, _ := onboardingOutboundNote(onboardingOutboundTestOperation)
	for _, field := range []string{"direction", "state"} {
		t.Run("application/"+field, func(t *testing.T) {
			row := onboardingFriendRow("11", "8", note)
			row["direction"], row[field] = "FROM_ME", nil
			if _, err := parseOnboardingOutboundApplication(onboardingFriendData(t, row), base, note); err == nil {
				t.Fatal("null application evidence resolved an outbound operation")
			}
		})
	}
	info := onboardingOutboundInfoRow()
	info["state"], info["state1"], info["friendPolicy"], info["forbidAddFriend"] = 0, 0, 0, false
	if got, err := parseOnboardingOutboundInfo(onboardingOutboundData(t, info), target,
		"CONVERSATION_RELATION_STRANGER_USER", false); err != nil || !got.CanRequest {
		t.Fatal("explicit zero enums lost their pinned meaning", got, err)
	}
	row := onboardingFriendRow("11", "8", note)
	row["direction"], row["state"] = 0, 3
	if got, err := parseOnboardingOutboundApplication(onboardingFriendData(t, row), base, note); err != nil ||
		got.State != "pending" || got.ApplicationID != "11" {
		t.Fatal("explicit outbound zero direction was rejected", got, err)
	}
}

func TestOnboardingOutboundWSLOperationMarkerAndResponseDirection(t *testing.T) {
	note, err := onboardingOutboundNote(onboardingOutboundTestOperation)
	if err != nil || !ValidNativeName(note, false) || len(utf16.Encode([]rune(note))) != 20 {
		t.Fatal("operation marker violates pinned desktop input bound", note, err)
	}
	same, _ := onboardingOutboundNote(onboardingOutboundTestOperation)
	different, _ := onboardingOutboundNote("12345678-1234-4234-8234-123456789abd")
	if note != same || note == different {
		t.Fatal("operation marker is not stable or does not bind the operation")
	}
	for _, operation := range []string{"", "not-a-uuid", strings.Repeat("a", 36),
		"00000000-0000-0000-0000-000000000000", strings.ToUpper(onboardingOutboundTestOperation)} {
		if _, err := onboardingOutboundNote(operation); err == nil {
			t.Fatal("invalid stable operation admitted", operation)
		}
	}
	base := OnboardingOutboundEvidence{OperationID: onboardingOutboundTestOperation, SenderID: "7", RecipientID: "8"}
	for _, tc := range []struct {
		own, peer, id string
		valid, friend bool
	}{
		{"FRIEND_STATE_APPLIED", "FRIEND_STATE_PENDING", "9007199254740993", true, false},
		{"FRIEND_STATE_NONE", "FRIEND_STATE_PENDING", "11", true, false},
		{"FRIEND_STATE_PENDING", "FRIEND_STATE_APPLIED", "11", false, false},
		{"FRIEND_STATE_APPLIED", "FRIEND_STATE_NONE", "11", false, false},
		{"FRIEND_STATE_APPLIED", "FRIEND_STATE_PENDING", "0", false, false},
		{"FRIEND_STATE_APPLIED", "FRIEND_STATE_PENDING", "9223372036854775808", false, false},
		{"FRIEND_STATE_GOOD", "FRIEND_STATE_GOOD", "0", true, true},
		{"FRIEND_STATE_GOOD", "FRIEND_STATE_DELETED", "11", false, false},
	} {
		got, err := parseOnboardingOutboundApply(onboardingOutboundData(t, map[string]any{
			"flId": tc.id, "state": tc.own, "state1": tc.peer,
		}), base)
		if (err == nil) != tc.valid || tc.valid && (got.OperationID != base.OperationID ||
			got.SenderID != "7" || got.RecipientID != "8" || got.IsFriend != tc.friend) {
			t.Fatal("inbound or invalid response was accepted as outbound", got, err)
		}
	}
}

func TestOnboardingOutboundWSLReconcileMatchesOriginalOperationOnly(t *testing.T) {
	note, _ := onboardingOutboundNote(onboardingOutboundTestOperation)
	base := OnboardingOutboundEvidence{
		OperationID: onboardingOutboundTestOperation, SenderID: "7", RecipientID: "8", State: "unknown",
	}
	for _, mode := range []string{"exact", "old_note", "wrong_sender", "wrong_direction", "same_note_other_target",
		"two_matching", "approved_not_friend", "missing_note", "empty", "over_limit"} {
		t.Run(mode, func(t *testing.T) {
			row := onboardingFriendRow("9007199254740993", "8", note)
			row["direction"] = "FROM_ME"
			rows := []map[string]any{row}
			switch mode {
			case "old_note":
				row["applyMsg"] = "unrelated old request"
			case "wrong_sender":
				row["user"] = map[string]any{"uid": "7"}
			case "wrong_direction":
				row["direction"] = "FROM_HIM"
			case "same_note_other_target":
				row["user"] = map[string]any{"uid": "9"}
			case "two_matching":
				copy := onboardingFriendRow("12", "8", note)
				copy["direction"] = "FROM_ME"
				rows = append(rows, copy)
			case "approved_not_friend":
				row["state"] = "FRIENDLOG_STATE_AGREE"
			case "missing_note":
				delete(row, "applyMsg")
			case "empty":
				rows = []map[string]any{}
			case "over_limit":
				rows = make([]map[string]any, 501)
			}
			got, err := parseOnboardingOutboundApplication(onboardingFriendData(t, rows...), base, note)
			switch mode {
			case "exact", "approved_not_friend":
				want := "pending"
				if mode == "approved_not_friend" {
					want = "approved"
				}
				if err != nil || got.State != want || got.ApplicationID != "9007199254740993" || got.IsFriend {
					t.Fatal("exact operation reconciliation failed or forged friendship", got, err)
				}
			case "two_matching", "over_limit":
				if err == nil {
					t.Fatal("ambiguous or unbounded operation accepted")
				}
			default:
				if err != nil || got.State != "unknown" || got.ApplicationID != "" || got.IsFriend {
					t.Fatal("unrelated pending request resolved this operation", got, err)
				}
			}
		})
	}
	rows := make([]map[string]any, 500)
	for i := range rows {
		rows[i] = onboardingFriendRow(strconv.Itoa(i+1), "9", "old")
	}
	rows[499] = onboardingFriendRow("9007199254740993", "8", note)
	rows[499]["direction"] = "FROM_ME"
	if got, err := parseOnboardingOutboundApplication(onboardingFriendData(t, rows...), base, note); err != nil || got.State != "pending" {
		t.Fatal("bounded server window was arbitrarily truncated", got, err)
	}
}
