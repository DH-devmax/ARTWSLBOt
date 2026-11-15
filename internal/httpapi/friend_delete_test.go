package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

func TestFriendDeleteCannotUseOrdinarySocialEndpoint(t *testing.T) {
	request := httptest.NewRequest("POST", "/api/v1/accounts/unused/native-social-operations",
		bytes.NewBufferString(`{"change":{"kind":"friend_delete","group_id":"123","target_id":"8"}}`))
	request.Header.Set("Content-Type", "application/json")
	err := (&Server{}).changeNativeSocial(httptest.NewRecorder(), request, Principal{Role: "superadmin"})
	if err == nil || err.Error() != "native_friend_delete_controlled_only" {
		t.Fatal("ordinary social route reached native transport", err)
	}
}

func TestFriendDeleteGrantRejectsMixedOrMultipleScopeBeforeNativeReads(t *testing.T) {
	for _, mutate := range []func(map[string]any){
		func(in map[string]any) { in["admission_mode"] = "package" },
		func(in map[string]any) { in["actions"] = []string{"friend_delete", "group_invite"} },
		func(in map[string]any) {
			in["account_ids"] = []string{"00000000-0000-4000-8000-000000000002", "00000000-0000-4000-8000-000000000003"}
		},
		func(in map[string]any) { in["target_ids"] = []string{"8", "9"} },
		func(in map[string]any) { in["budget"] = 2 },
	} {
		in := map[string]any{"group_id": "00000000-0000-4000-8000-000000000001",
			"account_ids": []string{"00000000-0000-4000-8000-000000000002"},
			"target_ids":  []string{"8"}, "actions": []string{"friend_delete"},
			"seconds": 300, "budget": 1, "admission_mode": "native_debug"}
		mutate(in)
		raw, _ := json.Marshal(in)
		request := httptest.NewRequest("POST", "/api/v1/test-windows", bytes.NewReader(raw))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Workspace", "wsl")
		err := (&Server{}).createTestWindow(httptest.NewRecorder(), request, Principal{Role: "superadmin"})
		if err == nil || err.Error() != "native_friend_delete_single_scope_required" {
			t.Fatal("malformed deletion grant reached upstream or database", in, err)
		}
	}
}
