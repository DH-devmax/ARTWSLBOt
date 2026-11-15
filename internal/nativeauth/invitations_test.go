package nativeauth

import (
	"encoding/json"
	"testing"
)

func TestInvitationProjectionPreservesIdentityAndUnknownStates(t *testing.T) {
	parse := func(raw string) ([]InvitationRecord, string, error) {
		var data map[string]json.RawMessage
		if err := json.Unmarshal([]byte(raw), &data); err != nil {
			t.Fatal(err)
		}
		return parseInvitationRecords(data)
	}
	rows, cursor, err := parse(`{"lastId":"9007199254740993","list":[{"applyId":"9007199254740993","groupId":123,
        "groupApplyType":"INVITATION","state":"MEMBER_STATE_MANAGER_REJECT",
        "applicant":{"userId":8,"nick":"same-name"},"inviter":{"userId":9,"nick":"same-name"}}]}`)
	if err != nil || cursor != "9007199254740993" || len(rows) != 1 || rows[0].Applicant.ID == rows[0].Inviter.ID ||
		rows[0].State != "MEMBER_STATE_MANAGER_REJECT" {
		t.Fatal(rows, cursor, err)
	}
	for _, raw := range []string{
		`{"list":null}`, `{"lastId":"not-a-cursor","list":[]}`,
		`{"lastId":null,"list":[]}`, `{"lastId":"01","list":[]}`, `{"lastId":-1,"list":[]}`,
		`{"lastId":1.5,"list":[]}`, `{"lastId":"9223372036854775808","list":[]}`,
		`{"list":[{"applyId":"11","groupId":123,"groupApplyType":"INVITATION","state":"FUTURE_STATE","applicant":{"userId":8}}]}`,
	} {
		if _, _, err := parse(raw); err == nil {
			t.Fatal("unknown/incomplete invitation response accepted", raw)
		}
	}
	for _, raw := range []string{`{"lastId":"0","list":[]}`, `{"lastId":0,"list":[]}`} {
		rows, cursor, err := parse(raw)
		if err != nil || rows == nil || len(rows) != 0 || cursor != "0" {
			t.Fatal("valid empty server window rejected", rows, cursor, err)
		}
	}
}

func TestGroupInfoRejectsConflictingOrUnscopedIdentities(t *testing.T) {
	for _, raw := range []string{
		`{"groupInfo":{"groupId":999}}`, `{"groupInfo":null}`,
		`{"groupId":123,"groupInfo":{"groupId":123}}`, `{"groupMuteMode":"MUTE_NO"}`,
	} {
		var data map[string]json.RawMessage
		json.Unmarshal([]byte(raw), &data)
		if _, err := groupInfoFields(data, "123"); err == nil {
			t.Fatal("ambiguous GroupInfo accepted", raw)
		}
	}
}
