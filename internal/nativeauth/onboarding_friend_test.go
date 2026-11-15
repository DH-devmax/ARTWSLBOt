package nativeauth

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func onboardingFriendData(t *testing.T, rows ...map[string]any) map[string]json.RawMessage {
	t.Helper()
	return socialData(t, func() string {
		raw, err := json.Marshal(map[string]any{"list": rows})
		if err != nil {
			t.Fatal(err)
		}
		return string(raw)
	}())
}

func onboardingFriendRow(id, uid, text string) map[string]any {
	return map[string]any{"flId": id, "user": map[string]any{"uid": uid, "nickName": "synthetic"},
		"direction": "FROM_HIM", "state": "FRIENDLOG_STATE_PROCESSING", "applyMsg": text,
		"friendCircle": map[string]bool{"notSeeMe": true}}
}

func TestOnboardingFriendWSLExactPrivateNoteAndPublicProjection(t *testing.T) {
	const code = "proof-synthetic-exact"
	data := onboardingFriendData(t, onboardingFriendRow("9007199254740993", "8", code))
	proof, application, err := parseOnboardingFriend(data, "7", code, nil)
	if err != nil || proof.ApplicationID != "9007199254740993" || proof.SenderID != "8" ||
		proof.RecipientID != "7" || proof.State != "pending" || application == nil || !application.NotSeeMe {
		t.Fatal("exact authenticated application not resolved", err)
	}
	ordinary, err := parseFriendApplications(data)
	if err != nil || len(ordinary) != 1 {
		t.Fatal("ordinary projection changed", err)
	}
	for _, value := range []any{proof, ordinary} {
		raw, _ := json.Marshal(value)
		if strings.Contains(string(raw), code) || strings.Contains(string(raw), "applyMsg") ||
			strings.Contains(string(raw), "notSeeMe") {
			t.Fatal("private note or privacy bits escaped safe evidence")
		}
	}
	for _, mismatch := range []string{strings.ToUpper(code), code + " ", " " + code, code + "\n", "proof-synthetic"} {
		if _, _, err := parseOnboardingFriend(data, "7", mismatch, nil); err == nil {
			t.Fatal("inexact proof accepted")
		}
	}
}

func TestOnboardingFriendWSLRejectsAmbiguityScopeAndUnboundedWindow(t *testing.T) {
	const code = "proof-synthetic"
	tests := []struct {
		name string
		edit func(map[string]any) []map[string]any
	}{
		{"outgoing", func(row map[string]any) []map[string]any {
			row["direction"] = "FROM_ME"
			return []map[string]any{row}
		}},
		{"self", func(row map[string]any) []map[string]any {
			row["user"] = map[string]any{"uid": "7"}
			return []map[string]any{row}
		}},
		{"already_approved", func(row map[string]any) []map[string]any {
			row["state"] = "FRIENDLOG_STATE_AGREE"
			return []map[string]any{row}
		}},
		{"missing_note", func(row map[string]any) []map[string]any {
			delete(row, "applyMsg")
			return []map[string]any{row}
		}},
		{"wrong_note_shape", func(row map[string]any) []map[string]any {
			row["applyMsg"] = map[string]any{"content": code}
			return []map[string]any{row}
		}},
		{"two_senders", func(row map[string]any) []map[string]any {
			return []map[string]any{row, onboardingFriendRow("12", "9", code)}
		}},
		{"duplicate_id", func(row map[string]any) []map[string]any { return []map[string]any{row, row} }},
		{"over_limit", func(row map[string]any) []map[string]any {
			return make([]map[string]any, 501)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := parseOnboardingFriend(onboardingFriendData(t, tc.edit(onboardingFriendRow("11", "8", code))...),
				"7", code, nil); err == nil {
				t.Fatal("unverified friend proof admitted")
			}
		})
	}
}

func TestOnboardingFriendWSLUsesBoundedServerWindowBeyond100(t *testing.T) {
	const code = "proof-synthetic-newest"
	for _, count := range []int{101, 174, 500} {
		rows := make([]map[string]any, count)
		for i := range rows {
			rows[i] = onboardingFriendRow(strconv.Itoa(i+1), "9", "unrelated-old-note")
			rows[i]["state"] = "FRIENDLOG_STATE_AGREE"
		}
		rows[count-1] = onboardingFriendRow("9007199254740993", "8", code)
		proof, _, err := parseOnboardingFriend(onboardingFriendData(t, rows...), "7", code, nil)
		if err != nil || proof.ApplicationID != "9007199254740993" || proof.SenderID != "8" {
			t.Fatalf("valid proof in %d-row server window rejected: %v", count, err)
		}
	}
}

func TestOnboardingFriendWSLReconciliationRequiresOriginalIdentityAndNote(t *testing.T) {
	const code = "proof-synthetic"
	expected := OnboardingFriendEvidence{ApplicationID: "11", SenderID: "8", RecipientID: "7", State: "pending"}
	for wire, state := range map[string]string{
		"FRIENDLOG_STATE_PROCESSING": "pending", "FRIENDLOG_STATE_AGREE": "approved",
		"FRIENDLOG_STATE_REFUSE": "rejected", "FRIENDLOG_STATE_EXPIRE": "expired",
		"FRIENDLOG_STATE_DELETE": "deleted", "FRIENDLOG_STATE_UNKNOWN": "unknown",
	} {
		row := onboardingFriendRow("11", "8", code)
		row["state"] = wire
		proof, _, err := parseOnboardingFriend(onboardingFriendData(t, row), "7", code, &expected)
		if err != nil || proof.State != state {
			t.Fatal("original result not observed", err)
		}
	}
	for _, row := range []map[string]any{
		onboardingFriendRow("12", "8", code), onboardingFriendRow("11", "9", code),
		onboardingFriendRow("11", "8", code+" changed"),
	} {
		if _, _, err := parseOnboardingFriend(onboardingFriendData(t, row), "7", code, &expected); err == nil {
			t.Fatal("another request reconciled the original")
		}
	}
	if _, _, err := parseOnboardingFriend(onboardingFriendData(t, onboardingFriendRow("11", "8", code),
		onboardingFriendRow("12", "9", code)), "7", code, &expected); err == nil {
		t.Fatal("fresh competing proof ignored by exact-ID reread")
	}
}

func TestOnboardingFriendDeadlineAndChallengeBound(t *testing.T) {
	now := time.Now()
	if !OnboardingFriendDeadline(now.Add(time.Minute), now) {
		t.Fatal("valid local deadline rejected")
	}
	for _, deadline := range []time.Time{{}, now, now.Add(-time.Second), now.Add(10*time.Minute + time.Nanosecond)} {
		if OnboardingFriendDeadline(deadline, now) {
			t.Fatal("unbounded or expired proof admitted")
		}
	}
	for _, text := range []string{"", " proof", "proof ", "proof\n", string([]byte{0xff}), strings.Repeat("x", 161)} {
		if ValidOnboardingFriendChallenge(text) {
			t.Fatal("malformed challenge admitted")
		}
	}
}
