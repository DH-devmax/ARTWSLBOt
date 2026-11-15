package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestFriendApprovalRequiresItsControlledEndpoint(t *testing.T) {
	r := httptest.NewRequest("POST", "/api/v1/accounts/unused/native-social-operations",
		bytes.NewBufferString(`{"change":{"kind":"friend_approve","target_id":"8","text":"11"}}`))
	r.Header.Set("Content-Type", "application/json")
	err := (&Server{}).changeNativeSocial(httptest.NewRecorder(), r, Principal{Role: "superadmin"})
	if err == nil || err.Error() != "native_friend_approve_controlled_only" {
		t.Fatal("approval bypassed the controlled scope", err)
	}
}

func TestFriendApprovalGrantRejectsUnboundedScopeBeforeReads(t *testing.T) {
	for _, mutate := range []func(map[string]any){
		func(in map[string]any) { in["admission_mode"] = "package" },
		func(in map[string]any) { in["actions"] = []string{"friend_approve", "friend_delete"} },
		func(in map[string]any) {
			in["account_ids"] = []string{"00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003"}
		},
		func(in map[string]any) { in["target_ids"] = []string{"8", "9"} },
		func(in map[string]any) { in["budget"] = 2 },
		func(in map[string]any) { delete(in, "friend_application_id") },
	} {
		in := map[string]any{"group_id": "00000000-0000-4000-8000-000000000001",
			"account_ids": []string{"00000000-0000-4000-8000-000000000002"},
			"target_ids":  []string{"8"}, "actions": []string{"friend_approve"},
			"friend_application_id": "11", "seconds": 300, "budget": 1, "admission_mode": "native_debug"}
		mutate(in)
		body, _ := json.Marshal(in)
		r := httptest.NewRequest("POST", "/api/v1/test-windows", bytes.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("X-Workspace", "wsl")
		err := (&Server{}).createTestWindow(httptest.NewRecorder(), r, Principal{Role: "superadmin"})
		if err == nil || err.Error() != "native_friend_approve_single_scope_required" && err.Error() != "native_friend_delete_single_scope_required" {
			t.Fatal("unbounded approval scope reached native reads", in, err)
		}
	}
}
