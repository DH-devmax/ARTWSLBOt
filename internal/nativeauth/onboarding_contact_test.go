package nativeauth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/types/known/timestamppb"
)

func onboardingContactFixture() (OnboardingContactSelector, time.Time) {
	at := time.Date(2026, 11, 8, 12, 1, 0, 123456789, time.UTC)
	return OnboardingContactSelector{Name: "synthetic contact", Since: at.Add(-time.Minute), Until: at.Add(time.Minute)}, at
}

func onboardingContactRow(id, uid, name string, at time.Time) map[string]any {
	row := onboardingFriendRow(id, uid, "")
	row["user"] = map[string]any{"uid": uid, "nickName": name}
	row["applyTime"] = at.Format(time.RFC3339Nano)
	row["applyMsg"] = map[string]any{"ignored": "private note is not a selector"}
	return row
}

func TestOnboardingContactSelectorBoundsAndExactNickname(t *testing.T) {
	selector, at := onboardingContactFixture()
	for _, name := range []string{"x", "Two Names", "\u5f20\u4e09", strings.Repeat("x", 200), strings.Repeat("\u540d", 66)} {
		current := selector
		current.Name = name
		if !ValidOnboardingContactSelector(current) {
			t.Fatalf("safe nickname rejected: %q", name)
		}
	}
	for _, name := range []string{"", " name", "name ", "\t", "x\n", "x\x00", string([]byte{0xff}),
		"x\u202e", "x\ufffd", strings.Repeat("x", 201), strings.Repeat("\u540d", 67)} {
		current := selector
		current.Name = name
		if ValidOnboardingContactSelector(current) {
			t.Fatalf("unsafe nickname admitted: %q", name)
		}
	}
	for _, edit := range []func(*OnboardingContactSelector){
		func(s *OnboardingContactSelector) { s.Since = time.Time{} },
		func(s *OnboardingContactSelector) { s.Until = time.Time{} },
		func(s *OnboardingContactSelector) { s.Until = s.Since },
		func(s *OnboardingContactSelector) { s.Until = s.Since.Add(-time.Nanosecond) },
		func(s *OnboardingContactSelector) { s.Until = s.Since.Add(10*time.Minute + time.Nanosecond) },
		func(s *OnboardingContactSelector) { s.Since = time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC) },
		func(s *OnboardingContactSelector) { s.Until = time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC) },
	} {
		current := selector
		edit(&current)
		if ValidOnboardingContactSelector(current) {
			t.Fatal("unbounded or invalid server interval admitted")
		}
	}
	selector.Until = selector.Since.Add(10 * time.Minute)
	if !ValidOnboardingContactSelector(selector) {
		t.Fatal("exact ten-minute server interval rejected")
	}
	for _, tc := range []struct {
		name string
		at   time.Time
		want bool
	}{
		{selector.Name, selector.Since, true}, {selector.Name, selector.Until, false},
		{selector.Name, selector.Since.Add(-time.Nanosecond), false},
		{selector.Name, at, true}, {strings.ToUpper(selector.Name), at, false},
		{selector.Name + " ", at, false}, {selector.Name, time.Time{}, false},
	} {
		if selector.Matches(OnboardingFriendEvidence{Name: tc.name, RequestedAt: tc.at}) != tc.want {
			t.Fatal("nickname or half-open interval changed")
		}
	}
}

func TestOnboardingContactSelectorExpiredUsesChallengeCutoff(t *testing.T) {
	selector, at := onboardingContactFixture()
	cutoff := selector.Since.Add(-10 * time.Minute)
	for _, enabled := range []bool{false, true} {
		selector.RejectExpired = enabled
		for _, tc := range []struct {
			name    string
			at      time.Time
			expired bool
			current bool
		}{
			{selector.Name, cutoff.Add(-time.Nanosecond), true, false},
			{selector.Name, cutoff, true, false},
			{selector.Name, cutoff.Add(time.Nanosecond), false, false},
			{selector.Name, selector.Since.Add(-time.Nanosecond), false, false},
			{selector.Name, selector.Since, false, true},
			{selector.Name, at, false, true},
			{selector.Name, selector.Until, false, false},
			{strings.ToUpper(selector.Name), cutoff, false, false},
			{selector.Name + " ", cutoff, false, false},
			{selector.Name, time.Time{}, false, false},
			{selector.Name, time.Date(0, 1, 1, 0, 0, 0, 0, time.UTC), false, false},
			{selector.Name, time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC), false, false},
		} {
			evidence := OnboardingFriendEvidence{Name: tc.name, RequestedAt: tc.at}
			if selector.IsExpired(evidence) != (enabled && tc.expired) ||
				selector.Matches(evidence) != (tc.current || enabled && tc.expired) {
				t.Fatalf("challenge cutoff or exact name changed: enabled=%v evidence=%+v", enabled, evidence)
			}
		}
	}
	selector.Until = selector.Since
	if selector.IsExpired(OnboardingFriendEvidence{Name: selector.Name, RequestedAt: cutoff}) {
		t.Fatal("invalid challenge admitted expired evidence")
	}
}

func TestOnboardingContactWSLExpiredSelectionPriorityAndOriginalReconciliation(t *testing.T) {
	selector, at := onboardingContactFixture()
	selector.RejectExpired = true
	old := selector.Since.Add(-10 * time.Minute)
	oldRow := onboardingContactRow("11", "8", selector.Name, old)
	expected, _, err := parseOnboardingContact(onboardingFriendData(t, oldRow), "7", selector, nil)
	if err != nil || expected.State != "pending" || !selector.IsExpired(expected) {
		t.Fatal("unique expired pending request was not selected", expected, err)
	}
	fresh := onboardingContactRow("12", "9", selector.Name, at)
	for _, rows := range [][]map[string]any{
		{oldRow, fresh}, {fresh, oldRow},
		{oldRow, onboardingContactRow("13", "10", selector.Name, old.Add(-time.Second)), fresh},
	} {
		proof, _, err := parseOnboardingContact(onboardingFriendData(t, rows...), "7", selector, nil)
		if err != nil || proof.ApplicationID != "12" || selector.IsExpired(proof) {
			t.Fatal("expired candidates displaced the unique current pending request", proof, err)
		}
	}
	for _, rows := range [][]map[string]any{
		{oldRow, oldRow},
		{oldRow, onboardingContactRow("12", "9", selector.Name, old.Add(-time.Second))},
		{oldRow, onboardingContactRow("12", "8", selector.Name, old.Add(-time.Second))},
		{oldRow, fresh, onboardingContactRow("13", "10", selector.Name, at.Add(time.Second))},
	} {
		if _, _, err := parseOnboardingContact(onboardingFriendData(t, rows...), "7", selector, nil); err == nil {
			t.Fatal("ambiguous same-category pending requests were admitted")
		}
	}
	for wire, state := range map[string]string{
		"FRIENDLOG_STATE_PROCESSING": "pending", "FRIENDLOG_STATE_REFUSE": "rejected",
		"FRIENDLOG_STATE_EXPIRE": "expired",
	} {
		row := onboardingContactRow("11", "8", selector.Name, old)
		row["state"] = wire
		data := onboardingFriendData(t, fresh, row)
		proof, _, err := parseOnboardingContact(data, "7", selector, &expected)
		if err != nil || proof.ApplicationID != expected.ApplicationID || proof.State != state ||
			proof.Name != expected.Name || !proof.RequestedAt.Equal(old) {
			t.Fatal("expired reconciliation replaced the original request", proof, err)
		}
	}
	for _, edit := range []func(*OnboardingFriendEvidence){
		func(e *OnboardingFriendEvidence) { e.ApplicationID = "12" },
		func(e *OnboardingFriendEvidence) { e.SenderID = "9" },
		func(e *OnboardingFriendEvidence) { e.RecipientID = "9" },
		func(e *OnboardingFriendEvidence) { e.Name = "other" },
		func(e *OnboardingFriendEvidence) { e.Name = "" },
		func(e *OnboardingFriendEvidence) { e.RequestedAt = old.Add(-time.Nanosecond) },
		func(e *OnboardingFriendEvidence) { e.RequestedAt = time.Time{} },
	} {
		changed := expected
		edit(&changed)
		if _, _, err := parseOnboardingContact(onboardingFriendData(t, oldRow), "7", selector, &changed); err == nil {
			t.Fatal("expired reconciliation accepted incomplete or substituted evidence")
		}
	}
	if _, _, err := parseOnboardingContact(onboardingFriendData(t, fresh), "7", selector, &expected); err == nil {
		t.Fatal("missing original expired request was replaced by a current request")
	}
}

func TestOnboardingContactWSLExpiredStillRejectsUnsafeEvidence(t *testing.T) {
	selector, _ := onboardingContactFixture()
	selector.RejectExpired = true
	old := selector.Since.Add(-10 * time.Minute)
	for _, mode := range []string{"too_recent", "at_until", "zero", "missing", "null", "invalid",
		"invalid_year", "outgoing", "self", "name_case", "refused", "expired"} {
		t.Run(mode, func(t *testing.T) {
			row := onboardingContactRow("11", "8", selector.Name, old)
			switch mode {
			case "too_recent":
				row["applyTime"] = old.Add(time.Nanosecond).Format(time.RFC3339Nano)
			case "at_until":
				row["applyTime"] = selector.Until.Format(time.RFC3339Nano)
			case "zero":
				row["applyTime"] = time.Time{}.Format(time.RFC3339Nano)
			case "missing":
				delete(row, "applyTime")
			case "null":
				row["applyTime"] = nil
			case "invalid":
				row["applyTime"] = old.UnixMilli()
			case "invalid_year":
				row["applyTime"] = "0000-01-01T00:00:00Z"
			case "outgoing":
				row["direction"] = "FROM_ME"
			case "self":
				row["user"] = map[string]any{"uid": "7", "nickName": selector.Name}
			case "name_case":
				row["user"] = map[string]any{"uid": "8", "nickName": strings.ToUpper(selector.Name)}
			case "refused":
				row["state"] = "FRIENDLOG_STATE_REFUSE"
			case "expired":
				row["state"] = "FRIENDLOG_STATE_EXPIRE"
			}
			if proof, application, err := parseOnboardingContact(onboardingFriendData(t, row), "7", selector, nil); err == nil || proof != (OnboardingFriendEvidence{}) || application != nil {
				t.Fatal("unsafe or already processed expired request was selected", proof, err)
			}
		})
	}
}

func TestOnboardingContactWSLOfficialTimestampJSONOnly(t *testing.T) {
	_, at := onboardingContactFixture()
	for _, want := range []time.Time{at, at.Truncate(time.Second), at.Truncate(time.Millisecond),
		at.Truncate(time.Microsecond)} {
		raw, err := protojson.Marshal(timestamppb.New(want))
		if err != nil {
			t.Fatal(err)
		}
		got, err := onboardingContactTimestamp(raw)
		if err != nil || !got.Equal(want) || got.Location() != time.UTC {
			t.Fatal("official Timestamp JSON lost precision", string(raw), got, err)
		}
	}
	offset := at.In(time.FixedZone("offset", 8*60*60)).Format(time.RFC3339Nano)
	raw, _ := json.Marshal(offset)
	if got, err := onboardingContactTimestamp(raw); err != nil || !got.Equal(at) {
		t.Fatal("official Timestamp JSON timezone offset rejected", err)
	}
	for _, raw := range []json.RawMessage{nil, []byte(`null`)} {
		_, err := onboardingContactTimestamp(raw)
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != "native_onboarding_contact_timestamp_unavailable" {
			t.Fatal("missing timestamp invented evidence or lost visible status", err)
		}
	}
	for _, raw := range []string{`""`, `0`, `1794139260`, `1794139260123`, `"1794139260"`,
		`{"seconds":1794139260,"nanos":123456789}`, `"2026-11-08 12:01:00"`,
		`"2026-11-08T12:01:00"`, `"2026-11-08T12:01:00.1234567891Z"`,
		`"2026-02-30T12:01:00Z"`, `"10000-01-01T00:00:00Z"`, `"0001-01-01T00:00:00Z"`, `true`, `[]`} {
		if got, err := onboardingContactTimestamp([]byte(raw)); err == nil || !got.IsZero() {
			t.Fatal("guessed or malformed timestamp became evidence", raw)
		}
	}
}

func TestOnboardingContactWSLNameTimeDirectionAndNoNoteMatching(t *testing.T) {
	selector, at := onboardingContactFixture()
	for _, note := range []any{nil, "", selector.Name, map[string]any{"arbitrary": true}, []any{1, 2}} {
		row := onboardingContactRow("9007199254740993", "8", selector.Name, at)
		row["applyMsg"] = note
		proof, application, err := parseOnboardingContact(onboardingFriendData(t, row), "7", selector, nil)
		if err != nil || proof.ApplicationID != "9007199254740993" || proof.SenderID != "8" ||
			proof.RecipientID != "7" || proof.State != "pending" || proof.Name != selector.Name ||
			!proof.RequestedAt.Equal(at) || application == nil || !application.NotSeeMe {
			t.Fatal("incoming contact evidence was not exact", proof, err)
		}
		raw, _ := json.Marshal(proof)
		if bytes.Contains(raw, []byte("applyMsg")) || bytes.Contains(raw, []byte("private note")) {
			t.Fatal("application note escaped evidence")
		}
	}
	for _, mode := range []string{"wrong_name", "name_case", "note_only", "outgoing", "self",
		"approved", "rejected", "before_since", "at_until", "missing_timestamp", "null_timestamp", "wrong_timestamp"} {
		t.Run(mode, func(t *testing.T) {
			row := onboardingContactRow("11", "8", selector.Name, at)
			switch mode {
			case "wrong_name", "note_only":
				row["user"] = map[string]any{"uid": "8", "nickName": "another contact"}
				row["applyMsg"] = selector.Name
			case "name_case":
				row["user"] = map[string]any{"uid": "8", "nickName": strings.ToUpper(selector.Name)}
			case "outgoing":
				row["direction"] = "FROM_ME"
			case "self":
				row["user"] = map[string]any{"uid": "7", "nickName": selector.Name}
			case "approved":
				row["state"] = "FRIENDLOG_STATE_AGREE"
			case "rejected":
				row["state"] = "FRIENDLOG_STATE_REFUSE"
			case "before_since":
				row["applyTime"] = selector.Since.Add(-time.Nanosecond).Format(time.RFC3339Nano)
			case "at_until":
				row["applyTime"] = selector.Until.Format(time.RFC3339Nano)
			case "missing_timestamp":
				delete(row, "applyTime")
				row["createTime"] = at.Format(time.RFC3339Nano)
			case "null_timestamp":
				row["applyTime"] = nil
			case "wrong_timestamp":
				row["applyTime"] = map[string]any{"seconds": at.Unix()}
			}
			proof, app, err := parseOnboardingContact(onboardingFriendData(t, row), "7", selector, nil)
			if err == nil || proof != (OnboardingFriendEvidence{}) || app != nil {
				t.Fatal("unverified contact admitted", proof, err)
			}
		})
	}
}

func TestOnboardingContactWSLRejectsDuplicateMalformedAndInvalidUTF8(t *testing.T) {
	selector, at := onboardingContactFixture()
	row := onboardingContactRow("11", "8", selector.Name, at)
	for _, rows := range [][]map[string]any{
		{row, row},
		{row, onboardingContactRow("12", "9", selector.Name, at)},
		{row, onboardingContactRow("12", "8", selector.Name, at.Add(time.Second))},
		make([]map[string]any, 501),
	} {
		data := onboardingFriendData(t, rows...)
		for _, expected := range []*OnboardingFriendEvidence{nil, {
			ApplicationID: "11", SenderID: "8", RecipientID: "7", Name: selector.Name, RequestedAt: at,
		}} {
			if proof, _, err := parseOnboardingContact(data, "7", selector, expected); err == nil || proof != (OnboardingFriendEvidence{}) {
				t.Fatal("duplicate or oversized window admitted", err)
			}
		}
	}
	valid := onboardingFriendData(t, row)["list"]
	badUTF8 := bytes.Replace(valid, []byte(selector.Name), []byte{0xff}, 1)
	badSurrogate := bytes.Replace(valid, []byte(selector.Name), []byte(`\ud800`), 1)
	for _, raw := range [][]byte{nil, []byte(`null`), []byte(`{}`), []byte(`[null]`),
		[]byte(`[`), badUTF8, badSurrogate} {
		if _, _, err := parseOnboardingContact(map[string]json.RawMessage{"list": raw}, "7", selector, nil); err == nil {
			t.Fatal("malformed or invalid UTF-8 list admitted", string(raw))
		}
	}
}

func TestOnboardingContactWSLOutsideWindowDoesNotSelectOrReplaceEvidence(t *testing.T) {
	selector, at := onboardingContactFixture()
	for _, stamp := range []time.Time{selector.Since.Add(-time.Nanosecond), selector.Until,
		selector.Since.Add(-48 * time.Hour)} {
		row := onboardingContactRow("11", "8", selector.Name, stamp)
		proof, app, err := parseOnboardingContact(onboardingFriendData(t, row), "7", selector, nil)
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != "native_onboarding_contact_outside_window" ||
			proof != (OnboardingFriendEvidence{}) || app != nil {
			t.Fatal("out-of-window pending request was hidden or selected", err)
		}
		fresh := onboardingContactRow("12", "9", selector.Name, at)
		proof, _, err = parseOnboardingContact(onboardingFriendData(t, row, fresh), "7", selector, nil)
		if err != nil || proof.ApplicationID != "12" || !proof.RequestedAt.Equal(at) {
			t.Fatal("old pending request displaced a valid current request", proof, err)
		}
		expected := OnboardingFriendEvidence{ApplicationID: "12", SenderID: "9", RecipientID: "7",
			Name: selector.Name, RequestedAt: at}
		_, _, err = parseOnboardingContact(onboardingFriendData(t, row), "7", selector, &expected)
		if !errors.As(err, &failure) || failure.Code != "native_onboarding_contact_not_found" {
			t.Fatal("reconciliation suggested replacing the original request", err)
		}
		for _, mode := range []string{"outgoing", "self", "wrong_name", "approved"} {
			other := onboardingContactRow("11", "8", selector.Name, stamp)
			switch mode {
			case "outgoing":
				other["direction"] = "FROM_ME"
			case "self":
				other["user"] = map[string]any{"uid": "7", "nickName": selector.Name}
			case "wrong_name":
				other["user"] = map[string]any{"uid": "8", "nickName": "unrelated"}
			case "approved":
				other["state"] = "FRIENDLOG_STATE_AGREE"
			}
			_, _, err = parseOnboardingContact(onboardingFriendData(t, other), "7", selector, nil)
			want := "native_onboarding_contact_not_found"
			if mode == "approved" {
				want = "native_onboarding_contact_processed"
			}
			if !errors.As(err, &failure) || failure.Code != want {
				t.Fatal("unrelated record disclosed a matching pending application", mode, err)
			}
		}
	}
}

func TestOnboardingContactWSLProcessedHistoryIsNotCurrentFriendOrProof(t *testing.T) {
	selector, at := onboardingContactFixture()
	for _, state := range []any{"FRIENDLOG_STATE_AGREE", "FRIENDLOG_STATE_REFUSE",
		"FRIENDLOG_STATE_DELETE", "FRIENDLOG_STATE_EXPIRE", 1, 2, 4, 5} {
		history := onboardingContactRow("11", "8", selector.Name, at.Add(-24*time.Hour))
		history["state"] = state
		proof, application, err := parseOnboardingContact(onboardingFriendData(t, history), "7", selector, nil)
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != "native_onboarding_contact_processed" ||
			proof != (OnboardingFriendEvidence{}) || application != nil {
			t.Fatal("processed history was hidden or admitted as evidence", state, err)
		}
		current := onboardingContactRow("12", "9", selector.Name, at)
		proof, _, err = parseOnboardingContact(onboardingFriendData(t, history, current), "7", selector, nil)
		if err != nil || proof.ApplicationID != "12" || proof.State != "pending" {
			t.Fatal("processed history displaced a current request", state, err)
		}
		current["applyTime"] = selector.Since.Add(-time.Second).Format(time.RFC3339Nano)
		_, _, err = parseOnboardingContact(onboardingFriendData(t, history, current), "7", selector, nil)
		if !errors.As(err, &failure) || failure.Code != "native_onboarding_contact_outside_window" {
			t.Fatal("processed history hid an out-of-window pending request", state, err)
		}
		expected := OnboardingFriendEvidence{ApplicationID: "12", SenderID: "9", RecipientID: "7",
			Name: selector.Name, RequestedAt: at}
		_, _, err = parseOnboardingContact(onboardingFriendData(t, history), "7", selector, &expected)
		if !errors.As(err, &failure) || failure.Code != "native_onboarding_contact_not_found" {
			t.Fatal("reconciliation changed its original-request diagnosis", state, err)
		}
	}
	for _, mode := range []string{"outgoing", "self", "different_name", "unknown"} {
		row := onboardingContactRow("11", "8", selector.Name, at)
		row["state"] = "FRIENDLOG_STATE_AGREE"
		switch mode {
		case "outgoing":
			row["direction"] = "FROM_ME"
		case "self":
			row["user"] = map[string]any{"uid": "7", "nickName": selector.Name}
		case "different_name":
			row["user"] = map[string]any{"uid": "8", "nickName": "another applicant"}
		case "unknown":
			row["state"] = "FRIENDLOG_STATE_UNKNOWN"
		}
		_, _, err := parseOnboardingContact(onboardingFriendData(t, row), "7", selector, nil)
		var failure *Error
		if !errors.As(err, &failure) || failure.Code != "native_onboarding_contact_not_found" {
			t.Fatal("unrelated or unknown history was classified as processed", mode, err)
		}
	}
}

func TestOnboardingContactWSLServerWindowAndReconcileOriginal(t *testing.T) {
	selector, at := onboardingContactFixture()
	row := onboardingContactRow("9007199254740993", "8", selector.Name, at)
	for _, count := range []int{101, 174, 500} {
		rows := make([]map[string]any, count)
		for i := range rows {
			rows[i] = onboardingFriendRow(strconv.Itoa(i+1), "9", "ignored")
			rows[i]["state"] = "FRIENDLOG_STATE_AGREE"
		}
		rows[count-1] = row
		if _, _, err := parseOnboardingContact(onboardingFriendData(t, rows...), "7", selector, nil); err != nil {
			t.Fatal("bounded first window was truncated", count, err)
		}
	}
	expected, _, err := parseOnboardingContact(onboardingFriendData(t, row), "7", selector, nil)
	if err != nil {
		t.Fatal(err)
	}
	for wire, state := range map[string]string{
		"FRIENDLOG_STATE_PROCESSING": "pending", "FRIENDLOG_STATE_AGREE": "approved",
		"FRIENDLOG_STATE_REFUSE": "rejected", "FRIENDLOG_STATE_EXPIRE": "expired",
		"FRIENDLOG_STATE_DELETE": "deleted", "FRIENDLOG_STATE_UNKNOWN": "unknown",
	} {
		row["state"] = wire
		proof, _, err := parseOnboardingContact(onboardingFriendData(t, row), "7", selector, &expected)
		if err != nil || proof.State != state || !proof.RequestedAt.Equal(expected.RequestedAt) {
			t.Fatal("read-only reconciliation lost original record", err)
		}
	}
	for _, edit := range []func(*OnboardingFriendEvidence){
		func(e *OnboardingFriendEvidence) { e.ApplicationID = "12" },
		func(e *OnboardingFriendEvidence) { e.SenderID = "9" },
		func(e *OnboardingFriendEvidence) { e.RecipientID = "9" },
		func(e *OnboardingFriendEvidence) { e.Name = "another contact" },
		func(e *OnboardingFriendEvidence) { e.RequestedAt = e.RequestedAt.Add(time.Nanosecond) },
	} {
		changed := expected
		edit(&changed)
		if _, _, err := parseOnboardingContact(onboardingFriendData(t, row), "7", selector, &changed); err == nil {
			t.Fatal("reconciliation substituted a different contact")
		}
	}
}

func TestOnboardingContactWSLDesktopOnlyAndOptionalHistoricalEvidence(t *testing.T) {
	selector, _ := onboardingContactFixture()
	client := &Client{mobile: true}
	if _, err := client.OnboardingContact(context.Background(), selector); err == nil {
		t.Fatal("mobile client admitted to desktop incoming contract")
	}
	client = &Client{}
	if _, err := client.OnboardingContact(context.Background(), OnboardingContactSelector{}); err == nil {
		t.Fatal("invalid selector reached uninitialized transport")
	}
	if _, err := client.ReconcileOnboardingContact(context.Background(), selector, OnboardingFriendEvidence{}); err == nil {
		t.Fatal("missing authenticated self reached transport")
	}
	raw, err := json.Marshal(OnboardingFriendEvidence{ApplicationID: "11", SenderID: "8", RecipientID: "7", State: "pending"})
	if err != nil || bytes.Contains(raw, []byte("name")) || bytes.Contains(raw, []byte("requested_at")) {
		t.Fatal("new optional fields changed historical evidence", string(raw), err)
	}
}
