package nativeauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func memberData(t *testing.T, raw string) map[string]json.RawMessage {
	t.Helper()
	var data map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		t.Fatal(err)
	}
	return data
}

func TestMembersSerialCompleteProjection(t *testing.T) {
	pages := []string{
		`{"groupId":"123","groupMemberInfo":[{"userId":"7","nimId":"me","groupRole":"GROUP_ROLE_ADMIN","nickName":"Synthetic","password":"never project"},{"userId":8,"nimId":88,"groupRole":"GROUP_ROLE_MEMBER","groupMemberNick":"Card"}],"hasMore":true,"nextCursor":"page-2"}`,
		`{"groupMemberInfo":[{"userId":"8","nimId":"88","groupRole":"GROUP_ROLE_MEMBER","groupMemberNick":"Card"},{"userId":"9","groupRole":"FUTURE_ROLE"}],"pageInfo":{"hasMore":false}}`,
	}
	calls := 0
	value, err := readMembers(context.Background(), "123", func(_ context.Context, route string, params map[string]any) (map[string]json.RawMessage, error) {
		if route != "/v1/group/get-group-members" || params["groupId"] != uint64(123) || params["v"] != "0" ||
			(calls == 0 && params["cursor"] != nil) || (calls == 1 && params["cursor"] != "page-2") {
			t.Fatal("unexpected fixed read contract", route, params)
		}
		data := memberData(t, pages[calls])
		calls++
		return data, nil
	})
	if err != nil || calls != 2 || !value.Complete || value.MappingComplete || len(value.Members) != 3 ||
		value.Members[0].Role != "admin" || value.Members[1].Transport != "88" || value.Members[2].Role != "unknown" {
		t.Fatal(value, err)
	}
	raw, _ := json.Marshal(value)
	if strings.Contains(string(raw), "password") || strings.Contains(string(raw), "never project") {
		t.Fatal("raw upstream record leaked")
	}
}

func TestMembersRejectPartialAndAmbiguousPages(t *testing.T) {
	for _, raw := range []string{
		`{}`, `{"groupMemberInfo":null}`, `{"groupMemberInfo":{}}`,
		`{"groupMemberInfo":[{"userId":"01"}]}`,
		`{"groupMemberInfo":[{"userId":"0"}]}`,
		`{"groupMemberInfo":[{"userId":"1","nimId":true}]}`,
		`{"groupMemberInfo":[{"userId":"1","nimId":" bad "}]}`,
		`{"groupMemberInfo":[{"userId":"1","groupRole":2}]}`,
		`{"groupMemberInfo":[{"userId":"1","groupRole":"FUTURE_ROLE","role":"GROUP_ROLE_ADMIN"}]}`,
		`{"groupMemberInfo":[{"userId":"1","groupRole":"GROUP_ROLE_ADMIN","role":"GROUP_ROLE_MEMBER"}]}`,
		`{"groupMemberInfo":[],"hasMore":true}`,
		`{"groupMemberInfo":[],"hasMore":"false"}`,
		`{"groupMemberInfo":[],"nextCursor":42}`,
		`{"groupMemberInfo":[],"nextCursor":"one","pageInfo":{"nextCursor":"two"}}`,
		`{"groupMemberInfo":[],"pageInfo":[]}`,
		`{"groupId":"124","groupMemberInfo":[]}`,
		`{"groupMemberInfo":[{"userId":"1","nimId":"same"},{"userId":"2","nimId":"same"}]}`,
		`{"groupMemberInfo":[{"userId":"1","nimId":"one"},{"userId":"1","nimId":"two"}]}`,
	} {
		value, err := readMembers(context.Background(), "123", func(context.Context, string, map[string]any) (map[string]json.RawMessage, error) {
			return memberData(t, raw), nil
		})
		if err == nil || value.Complete || value.Members != nil {
			t.Fatalf("published incomplete/ambiguous roster for %s", raw)
		}
	}
	for _, group := range []string{"", "01", "-1", "18446744073709551616", "1/../2"} {
		_, err := readMembers(context.Background(), group, func(context.Context, string, map[string]any) (map[string]json.RawMessage, error) {
			t.Fatal("invalid identity made a request")
			return nil, nil
		})
		if err == nil {
			t.Fatal("invalid group accepted")
		}
	}
}

func TestMembersCursorCancellationLimitsAndNoRetry(t *testing.T) {
	for _, kind := range []string{"cycle", "limit", "cancel", "429", "transport", "role_changed", "body_limit"} {
		t.Run(kind, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			calls := 0
			sentinel := errors.New("synthetic read unavailable")
			value, err := readMembers(ctx, "123", func(context.Context, string, map[string]any) (map[string]json.RawMessage, error) {
				calls++
				cursor := fmt.Sprint(calls)
				role := "GROUP_ROLE_MEMBER"
				if kind == "cycle" {
					cursor = "same"
				}
				if kind == "cancel" {
					cancel()
				}
				if kind == "429" {
					return nil, &Error{Code: "native_rate_limited"}
				}
				if kind == "transport" {
					return nil, sentinel
				}
				if kind == "role_changed" && calls == 2 {
					role = "GROUP_ROLE_ADMIN"
				}
				if kind == "body_limit" {
					rows := make([]map[string]any, 4000)
					for i := range rows {
						rows[i] = map[string]any{"userId": i + 1, "nimId": fmt.Sprintf("%04d%s", i, strings.Repeat("x", 1000))}
					}
					raw, _ := json.Marshal(map[string]any{"groupMemberInfo": rows})
					return memberData(t, string(raw)), nil
				}
				return memberData(t, fmt.Sprintf(`{"groupMemberInfo":[{"userId":"1","groupRole":%q}],"nextCursor":%q}`, role, cursor)), nil
			})
			if err == nil || value.Complete || value.Members != nil {
				t.Fatal("partial roster returned", err)
			}
			if calls > MemberPageLimit || kind == "cycle" && calls != 2 ||
				kind == "429" && calls != 1 || kind == "transport" && calls != 1 ||
				kind == "cancel" && calls != 1 || kind == "role_changed" && calls != 2 {
				t.Fatal("unbounded pagination or automatic retry", calls)
			}
			if kind == "transport" && !errors.Is(err, sentinel) {
				t.Fatal("read error replaced")
			}
		})
	}
}
