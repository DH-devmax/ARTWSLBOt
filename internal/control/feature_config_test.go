package control

import (
	"encoding/json"
	"testing"
	"wangshangliao/internal/features"
)

func TestAssistantFieldsAndPermissionFallback(t *testing.T) {
	previous := features.Config{BlockedWords: []string{"existing"}}
	for _, path := range [][2]string{{"ai_chat", "enabled"}, {"management", "enabled"}, {"scheduled_messages", "enabled"}} {
		_, _, err := ValidateFeatures(previous, FeatureChanges{path[0]: {path[1]: json.RawMessage(`true`)}}, Actor{Role: "superadmin"}, nil, []string{"moderation", "replies", "ai"}, true)
		if err == nil {
			t.Fatalf("assistant modified toggle %v", path)
		}
	}
	for _, change := range []FeatureChanges{
		{"blocked_words": {"rules": json.RawMessage(`[{"name":"pair","words":["a","b"]}]`)}},
		{"keyword_replies": {"cooldown_seconds": json.RawMessage(`60`)}},
	} {
		_, _, err := ValidateFeatures(previous, change, Actor{Role: "superadmin"}, nil,
			[]string{"moderation", "replies"}, true)
		if err == nil || err.Error() != "assistant_field_forbidden" {
			t.Fatal("new module fields implicitly opened to assistant", err)
		}
	}
	for _, id := range []string{"blocked_words", "blacklist", "exemptions", "keyword_replies"} {
		_, _, err := ValidateFeatures(previous, FeatureChanges{id: {"enabled": json.RawMessage(`true`)}}, Actor{Role: "superadmin"}, nil, []string{"moderation", "replies"}, true)
		if err != nil {
			t.Fatalf("supported feature switch %s: %v", id, err)
		}
		_, _, err = ValidateFeatures(previous, FeatureChanges{id: {"enabled": json.RawMessage(`true`)}}, Actor{Role: "user"}, nil, []string{"moderation", "replies"}, true)
		if err == nil {
			t.Fatal("field grant required", id)
		}
	}
	config, _, err := ValidateFeatures(previous, FeatureChanges{"blocked_words": {"words": json.RawMessage(`["new"]`)}},
		Actor{Role: "superadmin"}, nil, []string{"moderation"}, true)
	if err != nil || config.BlockedWords[0] != "new" || previous.BlockedWords[0] != "existing" {
		t.Fatal("snapshot alias/validation", err)
	}
	for _, role := range []string{"manager", "operator", "viewer"} {
		grants := RolePermissions(role, []PermissionSpec{{Code: "assistant:use"}, {Code: "assistant:action_approve"}, {Code: "group_action:mute_member"}, {Code: "assistant_module:blocked_words:query"}, {Code: "groups:read"}})
		for _, grant := range grants {
			if grant != "groups:read" {
				t.Fatal("legacy inherited assistant")
			}
		}
	}
}
