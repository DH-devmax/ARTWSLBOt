package lajiaoliao

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/openimsdk/protocol/sdkws"

	"wangshangliao/internal/nativeauth"
)

func onboardingContactFixture() (nativeauth.OnboardingContactSelector, time.Time) {
	at := time.Date(2026, 11, 8, 12, 1, 0, 123000000, time.UTC)
	return nativeauth.OnboardingContactSelector{Name: "synthetic contact", Since: at.Add(-time.Minute), Until: at.Add(time.Minute)}, at
}

func onboardingContactRow(sender, recipient, name string, created int64) map[string]any {
	row := onboardingFriendRow(sender, recipient, created, "")
	row["fromNickname"] = name
	row["reqMsg"] = map[string]any{"ignored": "private note"}
	return row
}

func TestOnboardingContactChiliProcessedAndOutsideWindowAreDiagnosticsOnly(t *testing.T) {
	selector, at := onboardingContactFixture()
	for _, state := range []int{-1, 1} {
		history := onboardingContactRow("customer", "self", selector.Name, at.Add(-24*time.Hour).UnixMilli())
		history["handleResult"] = state
		proof, err := parseOnboardingContact(onboardingFriendRaw(t, history), "self", selector, nil)
		var failure *Failure
		if !errors.As(err, &failure) || failure.Code != "lajiaoliao_onboarding_contact_processed" ||
			proof != (nativeauth.OnboardingFriendEvidence{}) {
			t.Fatal("processed history was hidden or treated as friendship/proof", state, err)
		}
		fresh := onboardingContactRow("new-customer", "self", selector.Name, at.UnixMilli())
		proof, err = parseOnboardingContact(onboardingFriendRaw(t, history, fresh), "self", selector, nil)
		if err != nil || proof.SenderID != "new-customer" || proof.State != "pending" {
			t.Fatal("history displaced a fresh pending request", state, err)
		}
		expected := proof
		for _, stamp := range []time.Time{selector.Since.Add(-time.Millisecond), selector.Until} {
			fresh["createTime"] = stamp.UnixMilli()
			proof, err = parseOnboardingContact(onboardingFriendRaw(t, history, fresh), "self", selector, nil)
			if !errors.As(err, &failure) || failure.Code != "lajiaoliao_onboarding_contact_outside_window" ||
				proof != (nativeauth.OnboardingFriendEvidence{}) {
				t.Fatal("out-of-window pending request was hidden by history or selected", state, err)
			}
		}
		_, err = parseOnboardingContact(onboardingFriendRaw(t, history), "self", selector, &expected)
		if !errors.As(err, &failure) || failure.Code != "lajiaoliao_onboarding_contact_not_found" {
			t.Fatal("reconciliation replaced the missing original request", state, err)
		}
		history["fromNickname"] = "another applicant"
		_, err = parseOnboardingContact(onboardingFriendRaw(t, history), "self", selector, nil)
		if !errors.As(err, &failure) || failure.Code != "lajiaoliao_onboarding_contact_not_found" {
			t.Fatal("another applicant's state affected the diagnosis", state, err)
		}
	}
	selector.RejectExpired = true
	stale := onboardingContactRow("customer", "self", selector.Name, selector.Since.Add(-10*time.Minute).UnixMilli())
	proof, err := parseOnboardingContact(onboardingFriendRaw(t, stale), "self", selector, nil)
	if err != nil || proof.State != "pending" || !selector.IsExpired(proof) {
		t.Fatal("new diagnostic displaced the original stale-request workflow", proof, err)
	}
}

func TestOnboardingContactChiliExpiredPriorityAndExactReconciliation(t *testing.T) {
	selector, at := onboardingContactFixture()
	selector.RejectExpired = true
	old := selector.Since.Add(-10 * time.Minute)
	oldRow := onboardingContactRow("customer", "self", selector.Name, old.UnixMilli())
	expected, err := parseOnboardingContact(onboardingFriendRaw(t, oldRow), "self", selector, nil)
	if err != nil || expected.State != "pending" || !selector.IsExpired(expected) {
		t.Fatal("unique expired pending request was not selected", expected, err)
	}
	fresh := onboardingContactRow("new-customer", "self", selector.Name, at.UnixMilli())
	for _, rows := range [][]map[string]any{
		{oldRow, fresh}, {fresh, oldRow},
		{oldRow, onboardingContactRow("another", "self", selector.Name, old.Add(-time.Second).UnixMilli()), fresh},
	} {
		proof, err := parseOnboardingContact(onboardingFriendRaw(t, rows...), "self", selector, nil)
		if err != nil || proof.SenderID != "new-customer" || selector.IsExpired(proof) {
			t.Fatal("expired candidates displaced the unique current pending request", proof, err)
		}
	}
	for _, rows := range [][]map[string]any{
		{oldRow, oldRow},
		{oldRow, onboardingContactRow("another", "self", selector.Name, old.UnixMilli()-1)},
		{oldRow, onboardingContactRow("customer", "self", selector.Name, old.UnixMilli()-1)},
		{oldRow, onboardingContactRow("customer", "self", "another name", at.UnixMilli())},
		{oldRow, onboardingContactRow("customer", "self", selector.Name, at.UnixMilli())},
		{oldRow, fresh, onboardingContactRow("another", "self", selector.Name, at.UnixMilli()+1)},
	} {
		if _, err := parseOnboardingContact(onboardingFriendRaw(t, rows...), "self", selector, nil); err == nil {
			t.Fatal("ambiguous category or sender-only mutation target was admitted")
		}
	}
	for result, state := range map[int]string{-1: "rejected", 0: "pending", 1: "approved"} {
		row := onboardingContactRow("customer", "self", selector.Name, old.UnixMilli())
		row["handleResult"] = result
		proof, err := parseOnboardingContact(onboardingFriendRaw(t, fresh, row), "self", selector, &expected)
		if err != nil || proof.ApplicationID != expected.ApplicationID || proof.State != state ||
			proof.Name != expected.Name || !proof.RequestedAt.Equal(old) {
			t.Fatal("expired reconciliation replaced the original request", proof, err)
		}
	}
	for _, edit := range []func(*nativeauth.OnboardingFriendEvidence){
		func(e *nativeauth.OnboardingFriendEvidence) { e.ApplicationID = "1" },
		func(e *nativeauth.OnboardingFriendEvidence) { e.SenderID = "another" },
		func(e *nativeauth.OnboardingFriendEvidence) { e.RecipientID = "another" },
		func(e *nativeauth.OnboardingFriendEvidence) { e.Name = "another name" },
		func(e *nativeauth.OnboardingFriendEvidence) { e.Name = "" },
		func(e *nativeauth.OnboardingFriendEvidence) { e.RequestedAt = old.Add(-time.Millisecond) },
		func(e *nativeauth.OnboardingFriendEvidence) { e.RequestedAt = time.Time{} },
	} {
		changed := expected
		edit(&changed)
		if _, err := parseOnboardingContact(onboardingFriendRaw(t, oldRow), "self", selector, &changed); err == nil {
			t.Fatal("expired reconciliation accepted incomplete or substituted evidence")
		}
	}
	if _, err := parseOnboardingContact(onboardingFriendRaw(t, fresh), "self", selector, &expected); err == nil {
		t.Fatal("missing original expired request was replaced by a current request")
	}
	selector.RejectExpired = false
	if _, err := parseOnboardingContact(onboardingFriendRaw(t, oldRow), "self", selector, nil); err == nil {
		t.Fatal("default selector admitted a historical pending request")
	}
}

func TestOnboardingContactChiliExpiredStillRejectsUnsafeOrIncompleteEvidence(t *testing.T) {
	selector, _ := onboardingContactFixture()
	selector.RejectExpired = true
	old := selector.Since.Add(-10 * time.Minute)
	for _, mode := range []string{"too_recent", "at_until", "zero", "negative", "missing", "null", "invalid",
		"outgoing", "other_recipient", "self", "name_case", "refused", "incomplete", "full_page"} {
		t.Run(mode, func(t *testing.T) {
			row := onboardingContactRow("customer", "self", selector.Name, old.UnixMilli())
			rows := []map[string]any{row}
			switch mode {
			case "too_recent":
				row["createTime"] = old.UnixMilli() + 1
			case "at_until":
				row["createTime"] = selector.Until.UnixMilli()
			case "zero":
				row["createTime"] = 0
			case "negative":
				row["createTime"] = -1
			case "missing":
				delete(row, "createTime")
			case "null":
				row["createTime"] = nil
			case "invalid":
				row["createTime"] = strconv.FormatInt(old.UnixMilli(), 10)
			case "outgoing":
				row["fromUserID"], row["toUserID"] = "self", "customer"
			case "other_recipient":
				row["toUserID"] = "another"
			case "self":
				row["fromUserID"] = "self"
			case "name_case":
				row["fromNickname"] = strings.ToUpper(selector.Name)
			case "refused":
				row["handleResult"] = -1
			case "full_page":
				for i := 1; i < 100; i++ {
					other := onboardingContactRow("other-"+strconv.Itoa(i), "self", "another", old.UnixMilli())
					other["handleResult"] = 1
					rows = append(rows, other)
				}
			}
			raw := onboardingFriendRaw(t, rows...)
			if mode == "incomplete" {
				raw, _ = json.Marshal(map[string]any{"friendRequests": rows, "total": 2})
			}
			if proof, err := parseOnboardingContact(raw, "self", selector, nil); err == nil || proof != (nativeauth.OnboardingFriendEvidence{}) {
				t.Fatal("unsafe or incomplete expired request was selected", proof, err)
			}
		})
	}
}

func TestOnboardingContactChiliExactIncomingNameTimeAndSDKZeroState(t *testing.T) {
	selector, at := onboardingContactFixture()
	session := Session{UserID: "self-string", IMToken: "synthetic", DeviceID: "device-fixture"}
	reads := 0
	client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
		reads++
		if r.URL.Path != "/friend/get_friend_apply_list" || len(body) != 2 || body["userID"] != session.UserID ||
			!reflect.DeepEqual(body["pagination"], map[string]any{"pageNumber": float64(1), "showNumber": float64(100)}) {
			t.Fatal("incoming read escaped its fixed first page")
		}
		return map[string]any{"friendRequests": []*sdkws.FriendRequest{{
			FromUserID: "customer-string", ToUserID: session.UserID, FromNickname: selector.Name,
			CreateTime: at.UnixMilli(), ReqMsg: "unrelated note",
		}}}, nil
	})
	proof, err := client.OnboardingContact(context.Background(), session, selector)
	if err != nil || !selector.Matches(proof) || proof.ApplicationID != strconv.FormatInt(at.UnixMilli(), 10) ||
		proof.SenderID != "customer-string" || proof.RecipientID != session.UserID || proof.State != "pending" || reads != 1 {
		t.Fatal("native SDK incoming contact was not selected exactly", proof, err)
	}
	raw, _ := json.Marshal(proof)
	if bytes.Contains(raw, []byte("reqMsg")) || bytes.Contains(raw, []byte("unrelated note")) {
		t.Fatal("application note escaped contact evidence")
	}
	for _, note := range []any{nil, "", selector.Name, true, map[string]any{"ignored": 1}} {
		row := onboardingContactRow("customer-string", session.UserID, selector.Name, at.UnixMilli())
		row["reqMsg"] = note
		if _, err := parseOnboardingContact(onboardingFriendRaw(t, row), session.UserID, selector, nil); err != nil {
			t.Fatal("note was parsed as contact evidence", err)
		}
	}
	for _, created := range []int64{at.Unix(), at.UnixMilli()} {
		proof, err := parseOnboardingContact(onboardingFriendRaw(t,
			onboardingContactRow("customer", "self", selector.Name, created)), "self", selector, nil)
		want := at
		if created == at.Unix() {
			want = at.Truncate(time.Second)
		}
		if err != nil || !proof.RequestedAt.Equal(want) || proof.ApplicationID != strconv.FormatInt(created, 10) {
			t.Fatal("fixed desktop timestamp units or original application ID changed", proof, err)
		}
	}
}

func TestOnboardingContactChiliRejectsDirectionNameTimeAndSenderAmbiguity(t *testing.T) {
	selector, at := onboardingContactFixture()
	cases := []struct {
		name string
		edit func(map[string]any) []map[string]any
	}{
		{"outgoing", func(row map[string]any) []map[string]any {
			row["fromUserID"], row["toUserID"] = "self", "customer"
			return []map[string]any{row}
		}},
		{"other_recipient", func(row map[string]any) []map[string]any {
			row["toUserID"] = "other"
			return []map[string]any{row}
		}},
		{"self", func(row map[string]any) []map[string]any {
			row["fromUserID"] = "self"
			return []map[string]any{row}
		}},
		{"name_case", func(row map[string]any) []map[string]any {
			row["fromNickname"] = strings.ToUpper(selector.Name)
			return []map[string]any{row}
		}},
		{"name_suffix", func(row map[string]any) []map[string]any {
			row["fromNickname"] = selector.Name + " "
			return []map[string]any{row}
		}},
		{"note_only", func(row map[string]any) []map[string]any {
			row["fromNickname"], row["reqMsg"] = "other", selector.Name
			return []map[string]any{row}
		}},
		{"approved", func(row map[string]any) []map[string]any {
			row["handleResult"] = 1
			return []map[string]any{row}
		}},
		{"rejected", func(row map[string]any) []map[string]any {
			row["handleResult"] = -1
			return []map[string]any{row}
		}},
		{"unknown_state", func(row map[string]any) []map[string]any {
			row["handleResult"] = 2
			return []map[string]any{row}
		}},
		{"null_state", func(row map[string]any) []map[string]any {
			row["handleResult"] = nil
			return []map[string]any{row}
		}},
		{"wrong_state", func(row map[string]any) []map[string]any {
			row["handleResult"] = "pending"
			return []map[string]any{row}
		}},
		{"before_since", func(row map[string]any) []map[string]any {
			row["createTime"] = selector.Since.UnixMilli() - 1
			return []map[string]any{row}
		}},
		{"at_until", func(row map[string]any) []map[string]any {
			row["createTime"] = selector.Until.UnixMilli()
			return []map[string]any{row}
		}},
		{"missing_timestamp", func(row map[string]any) []map[string]any {
			delete(row, "createTime")
			row["reqTime"] = at.UnixMilli()
			return []map[string]any{row}
		}},
		{"null_timestamp", func(row map[string]any) []map[string]any {
			row["createTime"] = nil
			return []map[string]any{row}
		}},
		{"two_senders", func(row map[string]any) []map[string]any {
			return []map[string]any{row, onboardingContactRow("another", "self", selector.Name, at.UnixMilli()+1)}
		}},
		{"same_sender_two_pending", func(row map[string]any) []map[string]any {
			return []map[string]any{row, onboardingContactRow("customer", "self", selector.Name, at.UnixMilli()+1)}
		}},
		{"same_sender_outside_selector", func(row map[string]any) []map[string]any {
			return []map[string]any{row, onboardingContactRow("customer", "self", "other", at.Add(-time.Hour).UnixMilli())}
		}},
		{"duplicate", func(row map[string]any) []map[string]any { return []map[string]any{row, row} }},
		{"over_limit", func(row map[string]any) []map[string]any { return make([]map[string]any, 101) }},
	}
	expected := nativeauth.OnboardingFriendEvidence{ApplicationID: strconv.FormatInt(at.UnixMilli(), 10),
		SenderID: "customer", RecipientID: "self", State: "pending", Name: selector.Name, RequestedAt: at}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := onboardingFriendRaw(t, tc.edit(onboardingContactRow("customer", "self", selector.Name, at.UnixMilli()))...)
			proof, err := parseOnboardingContact(raw, "self", selector, nil)
			if err == nil || proof != (nativeauth.OnboardingFriendEvidence{}) {
				t.Fatal("unverified contact or ambiguous sender admitted", proof, err)
			}
			if tc.name != "approved" && tc.name != "rejected" {
				if _, err := parseOnboardingContact(raw, "self", selector, &expected); err == nil {
					t.Fatal("original ID bypassed ambiguity or changed selection")
				}
			}
			if tc.name == "missing_timestamp" || tc.name == "null_timestamp" {
				var failure *Failure
				if !errors.As(err, &failure) || failure.Code != "lajiaoliao_onboarding_contact_timestamp_unavailable" {
					t.Fatal("missing timestamp lost its visible status", err)
				}
			}
		})
	}
}

func TestOnboardingContactChiliExplicitEmptyDirectoryCannotProduceEvidence(t *testing.T) {
	selector, at := onboardingContactFixture()
	expected := nativeauth.OnboardingFriendEvidence{
		ApplicationID: strconv.FormatInt(at.UnixMilli(), 10), SenderID: "customer",
		RecipientID: "self", State: "pending", Name: selector.Name, RequestedAt: at,
	}
	for _, tc := range []struct {
		name, raw, code string
	}{
		{"sdk_zero", `{"FriendRequests":null,"total":0}`, "not_found"},
		{"array_zero", `{"friendRequests":[],"total":0}`, "not_found"},
		{"missing_list", `{"total":0}`, "not_found"},
		{"missing_total", `{"friendRequests":null}`, "not_found"},
		{"success_without_data", `{"errCode":0}`, "not_found"},
		{"success_with_null_data", `{"errCode":0,"data":null}`, "not_found"},
		{"null_total", `{"friendRequests":null,"total":null}`, "directory_count"},
		{"nonzero_total", `{"friendRequests":null,"total":1}`, "directory_count"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, original := range []*nativeauth.OnboardingFriendEvidence{nil, &expected} {
				proof, err := parseOnboardingContact(json.RawMessage(tc.raw), "self", selector, original)
				var failure *Failure
				if proof != (nativeauth.OnboardingFriendEvidence{}) || !errors.As(err, &failure) ||
					failure.Code != "lajiaoliao_onboarding_contact_"+tc.code {
					t.Fatalf("empty or unverifiable directory returned %v, %v", proof, err)
				}
			}
		})
	}
}

func TestOnboardingContactChiliDirectoryFailureDoesNotAuthorizeAnyRow(t *testing.T) {
	selector, at := onboardingContactFixture()
	valid := onboardingContactRow("customer", "self", selector.Name, at.UnixMilli())
	for _, tc := range []struct {
		name, raw, code string
	}{
		{"non_object", `null`, "invalid"},
		{"bad_list", `{"friendRequests":{"records":[]}}`, "rows"},
		{"bad_row_shape", `{"friendRequests":[1]}`, "rows"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			checkInvalidChiliDirectory(t, json.RawMessage(tc.raw), selector, tc.code)
		})
	}
	for _, tc := range []struct {
		field string
		value any
		code  string
	}{
		{"toUserID", "another-receiver", "recipient"},
		{"fromUserID", "self", "identity"},
		{"handleResult", nil, "state"},
		{"handleResult", 2, "state"},
	} {
		t.Run(tc.code+"/"+tc.field, func(t *testing.T) {
			unrelated := onboardingContactRow("other", "self", "other name", at.UnixMilli())
			unrelated[tc.field] = tc.value
			checkInvalidChiliDirectory(t, onboardingFriendRaw(t, valid, unrelated), selector, tc.code)
		})
	}
}

func checkInvalidChiliDirectory(t *testing.T, raw json.RawMessage, selector nativeauth.OnboardingContactSelector, code string) {
	t.Helper()
	proof, err := parseOnboardingContact(raw, "self", selector, nil)
	var failure *Failure
	if proof != (nativeauth.OnboardingFriendEvidence{}) || !errors.As(err, &failure) ||
		failure.Code != "lajiaoliao_onboarding_contact_directory_"+code {
		t.Fatalf("invalid directory produced evidence or lost its category: %v, %v", proof, err)
	}
}

func TestOnboardingContactChiliMalformedUTF8TimestampAndIncompletePage(t *testing.T) {
	selector, at := onboardingContactFixture()
	for _, stamp := range []any{int64(0), int64(-1), int64(253402300800000), "1794139260123",
		map[string]any{"seconds": at.Unix()}, 1.5, true} {
		row := onboardingContactRow("customer", "self", selector.Name, at.UnixMilli())
		row["createTime"] = stamp
		if _, err := parseOnboardingContact(onboardingFriendRaw(t, row), "self", selector, nil); err == nil {
			t.Fatal("invalid timestamp became evidence", stamp)
		}
	}
	valid := onboardingFriendRaw(t, onboardingContactRow("customer", "self", selector.Name, at.UnixMilli()))
	for _, raw := range [][]byte{nil, []byte(`{}`), []byte(`{"friendRequests":null}`),
		[]byte(`{"friendRequests":[null]}`), []byte(`{"friendRequests":[`),
		bytes.Replace(valid, []byte(selector.Name), []byte{0xff}, 1),
		bytes.Replace(valid, []byte(selector.Name), []byte(`\ud800`), 1)} {
		if _, err := parseOnboardingContact(raw, "self", selector, nil); err == nil {
			t.Fatal("malformed or invalid UTF-8 page became evidence")
		}
	}
	for _, count := range []int{1, 100} {
		rows := make([]map[string]any, count)
		for i := range rows {
			rows[i] = onboardingContactRow("old-"+strconv.Itoa(i), "self", "other", at.UnixMilli()+int64(i))
			rows[i]["handleResult"] = 1
		}
		rows[count-1] = onboardingContactRow("customer", "self", selector.Name, at.UnixMilli())
		for _, total := range []int{count - 1, count, count + 1} {
			raw, _ := json.Marshal(map[string]any{"friendRequests": rows, "total": total})
			_, err := parseOnboardingContact(raw, "self", selector, nil)
			if (err == nil) != (total == count) {
				t.Fatal("incomplete sender-only mutation target was admitted", count, total, err)
			}
		}
		if count == 100 {
			if _, err := parseOnboardingContact(onboardingFriendRaw(t, rows...), "self", selector, nil); err == nil {
				t.Fatal("full page without completeness evidence was admitted")
			}
		}
	}
}

func TestOnboardingContactChiliReconcileExactOriginalNameTimeAndIdentity(t *testing.T) {
	selector, at := onboardingContactFixture()
	row := onboardingContactRow("customer", "self", selector.Name, at.UnixMilli())
	expected, err := parseOnboardingContact(onboardingFriendRaw(t, row), "self", selector, nil)
	if err != nil {
		t.Fatal(err)
	}
	for result, state := range map[int]string{-1: "rejected", 0: "pending", 1: "approved"} {
		row["handleResult"] = result
		proof, err := parseOnboardingContact(onboardingFriendRaw(t, row), "self", selector, &expected)
		if err != nil || proof.State != state || proof.Name != expected.Name || !proof.RequestedAt.Equal(at) {
			t.Fatal("original contact state was not reconciled", proof, err)
		}
	}
	for _, edit := range []func(*nativeauth.OnboardingFriendEvidence){
		func(e *nativeauth.OnboardingFriendEvidence) { e.ApplicationID = "1" },
		func(e *nativeauth.OnboardingFriendEvidence) { e.SenderID = "other" },
		func(e *nativeauth.OnboardingFriendEvidence) { e.RecipientID = "other" },
		func(e *nativeauth.OnboardingFriendEvidence) { e.Name = "other" },
		func(e *nativeauth.OnboardingFriendEvidence) { e.RequestedAt = e.RequestedAt.Add(time.Nanosecond) },
	} {
		changed := expected
		edit(&changed)
		if _, err := parseOnboardingContact(onboardingFriendRaw(t, row), "self", selector, &changed); err == nil {
			t.Fatal("reconciliation substituted another contact")
		}
	}
}

func TestOnboardingContactChiliNativeApprovalRereadFenceUnknownAndNoReplay(t *testing.T) {
	testOnboardingContactChiliNativeChange(t, false)
}

func TestOnboardingContactChiliNativeRejectExpiredRereadFenceUnknownAndNoReplay(t *testing.T) {
	testOnboardingContactChiliNativeChange(t, true)
}

func testOnboardingContactChiliNativeChange(t *testing.T, rejectExpired bool) {
	t.Helper()
	modes := []string{"approved", "name_changed", "timestamp_changed", "missing_timestamp",
		"sender_changed", "recipient_changed", "outgoing", "self", "processed", "refused",
		"ambiguous", "same_sender_outside_selector", "revoked", "expired", "wrong_action",
		"nil_fence", "no_submit", "expired_in_fence", "cancelled_final", "cancelled_parent",
		"cancelled_before", "incomplete", "unknown", "duplicate_submit", "concurrent_submit"}
	if rejectExpired {
		modes = append(modes, "flag_disabled", "expected_name_missing", "expected_time_missing", "new_current")
	} else {
		modes = append(modes, "current_with_expired")
	}
	for _, mode := range modes {
		t.Run(mode, func(t *testing.T) {
			session := Session{UserID: "self-string", IMToken: "synthetic", DeviceID: "device-fixture"}
			selector, at := onboardingContactFixture()
			selector.RejectExpired = rejectExpired || mode == "wrong_action" || mode == "current_with_expired"
			if rejectExpired && mode != "wrong_action" || !rejectExpired && mode == "wrong_action" {
				at = selector.Since.Add(-10 * time.Minute)
			}
			handleResult := 1
			if rejectExpired {
				handleResult = -1
			}
			var reads, writes, fences atomic.Int32
			client := nativeFixture(t, func(r *http.Request, body map[string]any) (any, error) {
				switch r.URL.Path {
				case "/friend/get_friend_apply_list":
					n := reads.Add(1)
					if len(body) != 2 || body["userID"] != session.UserID ||
						!reflect.DeepEqual(body["pagination"], map[string]any{"pageNumber": float64(1), "showNumber": float64(100)}) {
						t.Fatal("approval reread escaped first incoming page")
					}
					row := onboardingContactRow("customer", session.UserID, selector.Name, at.UnixMilli())
					rows := []map[string]any{row}
					if n > 1 {
						switch mode {
						case "name_changed":
							row["fromNickname"] = "changed"
						case "timestamp_changed":
							row["createTime"] = at.UnixMilli() + 1
						case "missing_timestamp":
							delete(row, "createTime")
						case "sender_changed":
							row["fromUserID"] = "changed"
						case "recipient_changed":
							row["toUserID"] = "changed"
						case "outgoing":
							row["fromUserID"], row["toUserID"] = session.UserID, "customer"
						case "self":
							row["fromUserID"] = session.UserID
						case "processed":
							row["handleResult"] = 1
						case "refused":
							row["handleResult"] = -1
						case "ambiguous":
							rows = append(rows, onboardingContactRow("other", session.UserID, selector.Name, at.UnixMilli()-1))
						case "same_sender_outside_selector":
							rows = append(rows, onboardingContactRow("customer", session.UserID, "other", at.Add(-time.Hour).UnixMilli()))
						case "new_current":
							rows = append(rows, onboardingContactRow("other", session.UserID, selector.Name, selector.Since.UnixMilli()))
						}
					}
					if mode == "current_with_expired" {
						rows = append(rows, onboardingContactRow("other", session.UserID, selector.Name,
							selector.Since.Add(-10*time.Minute).UnixMilli()))
					}
					if n == 3 {
						row["handleResult"] = handleResult
					}
					result := map[string]any{"friendRequests": rows}
					if n > 1 && mode == "incomplete" {
						result["total"] = len(rows) + 1
					}
					return result, nil
				case "/friend/add_friend_response":
					writes.Add(1)
					if fences.Load() != 1 || !reflect.DeepEqual(body, map[string]any{"fromUserID": "customer",
						"toUserID": session.UserID, "handleResult": float64(handleResult), "handleMsg": ""}) {
						t.Fatal("approval escaped exact sender/recipient or final fence")
					}
					if mode == "unknown" {
						return nil, io.ErrUnexpectedEOF
					}
					return map[string]any{}, nil
				default:
					t.Fatal("unreviewed contact route", r.URL.Path)
					return nil, nil
				}
			})
			proof, err := client.OnboardingContact(context.Background(), session, selector)
			if err != nil || reads.Load() != 1 || writes.Load() != 0 || !selector.Matches(proof) {
				t.Fatal("incoming preparation lost evidence or mutated", err)
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled_before" {
				cancel()
			}
			deadline := time.Now().Add(time.Minute)
			if mode == "expired" {
				deadline = time.Now().Add(-time.Second)
			}
			if mode == "expired_in_fence" {
				deadline = time.Now().Add(30 * time.Millisecond)
			}
			var fence nativeauth.OnboardingFriendFence = func(work context.Context, current nativeauth.OnboardingFriendEvidence,
				send func(context.Context) error) error {
				fences.Add(1)
				if current != proof || reads.Load() != 2 {
					t.Fatal("changed or stale contact reached final fence")
				}
				switch mode {
				case "revoked":
					return errors.New("synthetic-revoked")
				case "no_submit":
					return nil
				case "expired_in_fence":
					<-work.Done()
				case "cancelled_final":
					stopped, stop := context.WithCancel(work)
					stop()
					return send(stopped)
				case "cancelled_parent":
					cancel()
					return send(context.Background())
				case "concurrent_submit":
					var wg sync.WaitGroup
					var success atomic.Int32
					for range 8 {
						wg.Go(func() {
							if send(work) == nil {
								success.Add(1)
							}
						})
					}
					wg.Wait()
					if success.Load() != 1 {
						t.Fatal("concurrent contact approval was not single use")
					}
					return nil
				}
				err := send(work)
				if mode == "duplicate_submit" {
					return send(work)
				}
				return err
			}
			if mode == "nil_fence" {
				fence = nil
			}
			expected := proof
			switch mode {
			case "flag_disabled":
				selector.RejectExpired = false
			case "expected_name_missing":
				expected.Name = ""
			case "expected_time_missing":
				expected.RequestedAt = time.Time{}
			}
			if rejectExpired {
				err = client.RejectOnboardingContact(ctx, session, selector, expected, deadline, fence)
			} else {
				err = client.ApproveOnboardingContact(ctx, session, selector, expected, deadline, fence)
			}
			success := mode == "approved" || mode == "concurrent_submit" ||
				mode == "current_with_expired" || mode == "new_current"
			wantWrites := int32(0)
			if success || mode == "unknown" || mode == "duplicate_submit" {
				wantWrites = 1
			}
			if (err == nil) != success || writes.Load() != wantWrites {
				t.Fatal("contact approval bypass, false success or automatic replay", writes.Load(), err)
			}
			if writes.Load() > 0 && (fences.Load() != 1 || reads.Load() != 2) {
				t.Fatal("contact change did not reread once and fence once")
			}
			if mode == "expired" || mode == "nil_fence" {
				if reads.Load() != 1 || fences.Load() != 0 {
					t.Fatal("expired or unfenced approval read upstream")
				}
			}
			if mode == "unknown" {
				var failure *Failure
				if !errors.As(err, &failure) || !failure.Submitted {
					t.Fatal("unknown result lost submission status", err)
				}
			}
			if success || mode == "unknown" {
				result, readErr := client.ReconcileOnboardingContact(context.Background(), session, selector, proof)
				wantState := "approved"
				if rejectExpired {
					wantState = "rejected"
				}
				if readErr != nil || result.State != wantState || !selector.Matches(result) ||
					reads.Load() != 3 || writes.Load() != 1 {
					t.Fatal("read-only reconciliation replayed or changed contact evidence", readErr)
				}
			}
		})
	}
}
