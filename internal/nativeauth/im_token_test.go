package nativeauth

import (
	"encoding/json"
	"testing"
)

func TestIMTokenKinds(t *testing.T) {
	for _, test := range []struct {
		kind string
		mode int
	}{
		{``, 0},
		{`"NIM_LOGIN_KIND_STATIC"`, 0},
		{`"NIM_LOGIN_KIND_DYNC"`, 1},
		{`"NIM_LOGIN_KIND_DYNAMIC"`, 1},
		{`"NIM_LOGIN_KIND_CALLBACK"`, 2},
	} {
		data := map[string]json.RawMessage{"nimToken": json.RawMessage(`"synthetic-token"`)}
		if test.kind != "" {
			data["kind"] = json.RawMessage(test.kind)
		}
		token, mode, err := decodeIMToken(data)
		if err != nil || token != "synthetic-token" || mode != test.mode {
			t.Fatalf("token kind lost: %s", test.kind)
		}
	}
	for _, kind := range []string{`null`, `2`, `""`, `"unknown"`} {
		if _, _, err := decodeIMToken(map[string]json.RawMessage{
			"nimToken": json.RawMessage(`"synthetic-token"`), "kind": json.RawMessage(kind),
		}); err == nil {
			t.Fatal("invalid kind silently became static")
		}
	}
}
