package features

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func TestSettingsAndUnsupportedFields(t *testing.T) {
	var config Config
	for _, change := range []struct{ id, field, value string }{
		{"keyword_replies", "enabled", "true"},
		{"keyword_replies", "admin_only", "true"},
		{"keyword_replies", "rules", `[{"keyword":"hello","reply":"world"}]`},
		{"blocked_words", "words", `["blocked"]`},
	} {
		if err := Apply(&config, change.id, change.field, json.RawMessage(change.value)); err != nil {
			t.Fatal(err)
		}
	}
	if !config.FeatureOptions["keyword_replies"].Enabled || !config.FeatureOptions["keyword_replies"].AdminOnly ||
		config.KeywordReplies[0].Reply != "world" || config.BlockedWords[0] != "blocked" {
		t.Fatal("settings did not retain original rule storage and independent switches")
	}
	for _, change := range []struct{ id, field, value string }{
		{"keyword_replies", "timed_publish", "true"},
		{"scheduled_messages", "enabled", "true"},
		{"keyword_replies", "enabled", `"true"`},
		{"keyword_replies", "enabled", "null"},
		{"keyword_replies", "enabled", "true false"},
		{"keyword_replies", "rules", `[{"keyword":"hello","reply":"world","command":"exec"}]`},
		{"blacklist", "members", `[""]`},
		{"blocked_words", "words", `["` + strings.Repeat("x", 201) + `"]`},
	} {
		var other Config
		if Apply(&other, change.id, change.field, json.RawMessage(change.value)) == nil {
			t.Fatalf("accepted unsupported settings: %s/%s", change.id, change.field)
		}
	}
}

func TestFieldGrantsAreIndependent(t *testing.T) {
	definition, _ := Find("keyword_replies")
	grants := []string{Permission(definition.ID, "enabled")}
	if !CanEdit(grants, definition, "enabled") || CanEdit(grants, definition, "admin_only") ||
		CanEdit(grants, definition, "rules") {
		t.Fatal("field permission expanded into a different field")
	}
	if !CanEdit([]string{"groups:replies"}, definition, "rules") {
		t.Fatal("legacy broad permission lost compatibility")
	}
	if CanEdit([]string{"groups:replies"}, definition, "cooldown_seconds") {
		t.Fatal("legacy permission granted the new cooldown field")
	}
	blocked, _ := Find("blocked_words")
	if CanEdit([]string{"groups:moderation"}, blocked, "rules") ||
		!CanEdit([]string{"groups:moderation"}, blocked, "words") {
		t.Fatal("new combination field changed legacy permission scope")
	}
	seen := map[string]bool{}
	for _, definition := range Catalog() {
		if seen[definition.ID] {
			t.Fatal("duplicate feature ID")
		}
		seen[definition.ID] = true
		if definition.Status == "unavailable" && len(definition.Fields) != 0 {
			t.Fatal("unavailable feature exposed a fictitious setting")
		}
	}
}

func TestNativeCatalogAndModuleDependencies(t *testing.T) {
	seen := map[string]bool{}
	for _, capability := range NativeCatalog() {
		if capability.ID == "" || seen[capability.ID] || capability.Evidence == "" || capability.Role == "" {
			t.Fatalf("invalid native contract: %s", capability.ID)
		}
		seen[capability.ID] = true
		if capability.Integration == "accepted" {
			t.Fatalf("native acceptance has not passed: %s", capability.ID)
		}
	}
	for _, definition := range Catalog() {
		dependencies := map[string]bool{}
		for _, id := range definition.NativeDependencies {
			if !seen[id] || dependencies[id] {
				t.Fatalf("invalid native dependency: %s -> %s", definition.ID, id)
			}
			dependencies[id] = true
		}
		if len(definition.NativeDependencies) != len(NativeBlockers(definition)) {
			t.Fatalf("unaccepted dependency bypassed the catalog gate: %s", definition.ID)
		}
	}
	if len(NativeBlockers(Definition{NativeDependencies: []string{"unknown_contract"}})) != 1 {
		t.Fatal("unknown native capability did not fail closed")
	}
	replies, _ := Find("keyword_replies")
	for _, required := range []string{"receive_message", "read_roles", "send_text"} {
		found := false
		for _, id := range replies.NativeDependencies {
			found = found || id == required
		}
		if !found {
			t.Fatalf("reply module lost a required native contract: %s", required)
		}
	}
}

func TestReadFoundationStatusDoesNotClaimActionAcceptance(t *testing.T) {
	for _, id := range []string{"receive_message", "list_groups", "list_members", "read_roles", "read_mutes", "read_images", "read_forward", "read_avatars", "list_friends", "read_invitations"} {
		value, ok := FindNative(id)
		if !ok || value.Integration != "partial" {
			t.Fatalf("implemented read projection missing from catalog: %s", id)
		}
	}
	partial := map[string]bool{"send_text": true, "send_private_text": true, "quote_reply": true,
		"mention_member": true, "recall_message": true, "account_settings": true, "set_member_card": true,
		"approve_friend": true, "invite_group": true, "leave_group": true, "mute_member": true, "unmute_member": true,
		"mute_group": true, "kick_member": true, "publish_notice": true, "send_image": true, "send_media": true,
		"send_private_image": true, "grant_admin": true, "revoke_admin": true, "edit_group_info": true,
		"onboarding_friend_request": true, "friend_delete": true}
	for _, value := range NativeCatalog() {
		if value.Kind != "action" {
			continue
		}
		expected := "pending"
		if partial[value.ID] {
			expected = "partial"
		}
		if value.Integration != expected {
			t.Fatalf("unimplemented action status changed: %s", value.ID)
		}
		if len(NativeBlockers(Definition{NativeDependencies: []string{value.ID}})) != 1 {
			t.Fatalf("protocol primitives bypassed acceptance: %s", value.ID)
		}
	}
	send, _ := FindNative("send_text")
	if send.Integration != "partial" || len(NativeBlockers(Definition{NativeDependencies: []string{"send_text"}})) != 1 {
		t.Fatal("synthetic text foundation raised native action readiness")
	}
}

func TestRuleSettingsLegacyJSONAndDefaults(t *testing.T) {
	raw := []byte(`{"blocked_words":["bad","bad"],"blacklist":["blocked"],"exemptions":["exempt"],
		"keyword_replies":[{"keyword":"hello","reply":"first"},{"keyword":"hello","reply":"second"}],
		"ai_cooldown_seconds":0,"feature_options":{"keyword_replies":{"enabled":true}}}`)
	var config Config
	if err := strict(raw, &config); err != nil {
		t.Fatal(err)
	}
	if err := Validate(config); err != nil {
		t.Fatal("legacy literal lists or duplicate ordered replies lost compatibility", err)
	}
	if got := EvaluateBusinessRules(config, BusinessRuleInput{Sender: "human", Text: "hello world", Role: "member"}); got.Action != BusinessReply || got.Text != "first" {
		t.Fatalf("legacy matching changed: %+v", got)
	}
	before, _ := json.Marshal(config)
	values := Values(config, "keyword_replies")
	if values["cooldown_seconds"] != 30 || KeywordReplyCooldown(config) != 30 {
		t.Fatal("omitted cooldown did not project the legacy thirty-second default")
	}
	if rules, ok := Values(config, "blocked_words")["rules"].([]BlockedRule); !ok || rules == nil || len(rules) != 0 {
		t.Fatal("missing combination rules must project an empty list")
	}
	after, _ := json.Marshal(config)
	if string(before) != string(after) {
		t.Fatal("reading settings materialized defaults in stored configuration")
	}
	for _, added := range []string{`"match"`, `"disabled"`, `"cooldown_seconds"`, `"blocked_rules"`} {
		if strings.Contains(string(after), added) {
			t.Fatalf("legacy JSON acquired an omitted field: %s", added)
		}
	}
}

func TestLegacyEmptyReplySettingsRemainEditable(t *testing.T) {
	for _, rules := range []string{
		`[{"keyword":"hello","reply":""},{"keyword":"hello","reply":"later"}]`,
		`[{"keyword":"hello"},{"keyword":"hello","reply":"later"}]`,
	} {
		t.Run(rules, func(t *testing.T) {
			var config Config
			raw := []byte(`{"keyword_replies":` + rules + `,"feature_options":{"keyword_replies":{"enabled":true}}}`)
			if err := strict(raw, &config); err != nil {
				t.Fatal(err)
			}
			if err := Validate(config); err != nil {
				t.Fatal("legacy empty reply invalidated saved configuration", err)
			}
			wantRules := []KeywordRule{{Keyword: "hello", Reply: ""}, {Keyword: "hello", Reply: "later"}}
			for _, change := range []struct{ id, field, raw string }{
				{"keyword_replies", "admin_only", `true`},
				{"keyword_replies", "cooldown_seconds", `60`},
				{"blocked_words", "words", `["blocked"]`},
				{"blocked_words", "rules", `[{"name":"pair","words":["one","two"]}]`},
				{"exemptions", "enabled", `true`},
				{"keyword_replies", "enabled", `false`},
				{"keyword_replies", "rules", rules},
			} {
				if err := Apply(&config, change.id, change.field, json.RawMessage(change.raw)); err != nil {
					t.Fatalf("legacy empty reply blocked saving %s/%s: %v", change.id, change.field, err)
				}
				if err := Validate(config); err != nil || !reflect.DeepEqual(config.KeywordReplies, wantRules) {
					t.Fatalf("saving another field changed legacy rules or validation: %+v, %v", config.KeywordReplies, err)
				}
			}
			projected := Values(config, "keyword_replies")["rules"]
			if !reflect.DeepEqual(projected, wantRules) {
				t.Fatal("reading settings hid or changed a legacy empty reply")
			}
			encoded, err := json.Marshal(config)
			if err != nil {
				t.Fatal(err)
			}
			var restored Config
			if err := strict(encoded, &restored); err != nil || Validate(restored) != nil ||
				!reflect.DeepEqual(restored.KeywordReplies, wantRules) {
				t.Fatalf("saved legacy empty reply did not round-trip: %v", err)
			}
		})
	}
}

func TestRuleSettingsFieldsAndIndependentApplication(t *testing.T) {
	for _, tc := range []struct {
		id, key, kind string
		limit         int
	}{
		{"blocked_words", "rules", "blocked_rules", 20},
		{"keyword_replies", "cooldown_seconds", "integer", 86400},
	} {
		definition, _ := Find(tc.id)
		found := false
		for _, field := range definition.Fields {
			if field.Key == tc.key {
				found = field.Type == tc.kind && field.Limit == tc.limit
			}
		}
		if !found || CanEdit([]string{Permission(tc.id, "enabled")}, definition, tc.key) ||
			!CanEdit([]string{Permission(tc.id, tc.key)}, definition, tc.key) {
			t.Fatalf("new field lost its contract or independent permission: %s/%s", tc.id, tc.key)
		}
	}
	config := businessRuleConfig()
	originalOptions := config.FeatureOptions
	for _, change := range []struct{ id, field, raw string }{
		{"blocked_words", "rules", `[{"name":"pair","words":["red","blue"],"disabled":true}]`},
		{"keyword_replies", "rules", `[{"keyword":"hello","reply":"new","match":"exact","disabled":true}]`},
		{"keyword_replies", "cooldown_seconds", `60`},
	} {
		if err := Apply(&config, change.id, change.field, json.RawMessage(change.raw)); err != nil {
			t.Fatalf("%s/%s: %v", change.id, change.field, err)
		}
	}
	if !reflect.DeepEqual(config.BlockedWords, []string{"bad", "a.b"}) || len(config.BlockedRules) != 1 ||
		!config.BlockedRules[0].Disabled || len(config.KeywordReplies) != 1 || config.KeywordReplies[0].Match != "exact" ||
		!config.KeywordReplies[0].Disabled || !config.FeatureOptions["keyword_replies"].Enabled ||
		KeywordReplyCooldown(config) != 60 || Values(config, "keyword_replies")["cooldown_seconds"] != 60 {
		t.Fatalf("applying one field replaced another field: %+v", config)
	}
	if originalOptions["keyword_replies"].CooldownSeconds != 0 {
		t.Fatal("applying settings mutated a prior snapshot's options")
	}
	if !reflect.DeepEqual(Values(config, "blocked_words")["rules"], config.BlockedRules) {
		t.Fatal("combination settings did not round-trip through Values")
	}
	for _, id := range []string{"blocked_words", "keyword_replies"} {
		if err := Apply(&config, id, "rules", json.RawMessage(`[]`)); err != nil {
			t.Fatal(err)
		}
	}
	if len(config.BlockedRules) != 0 || len(config.KeywordReplies) != 0 || len(config.BlockedWords) != 2 {
		t.Fatal("clearing a rule list cleared another list")
	}
}

func TestRuleSettingsStrictAndAtomic(t *testing.T) {
	for _, change := range []struct{ id, field, raw string }{
		{"keyword_replies", "rules", `[{"keyword":"hello","reply":"world","match":"regex"}]`},
		{"keyword_replies", "rules", `[{"keyword":"hello","reply":"world","disabled":"false"}]`},
		{"keyword_replies", "rules", `[{"keyword":"hello","reply":"world","extra":true}]`},
		{"keyword_replies", "rules", `[{"keyword":"hello","reply":"world"},false]`},
		{"blocked_words", "rules", `[{"name":"pair","words":["one","two"],"command":"exec"}]`},
		{"blocked_words", "rules", `[{"name":"pair","words":["one","one"]}]`},
		{"blocked_words", "rules", `[{"name":"pair","words":["one"]}]`},
		{"blocked_words", "rules", `[{"keyword":"one","reply":"two"}]`},
		{"keyword_replies", "rules", `[{"name":"pair","words":["one","two"]}]`},
		{"blocked_words", "rules", `null`},
		{"keyword_replies", "rules", `[] []`},
		{"blocked_words", "cooldown_seconds", `30`},
		{"keyword_replies", "cooldown_seconds", `29`},
		{"keyword_replies", "cooldown_seconds", `-1`},
		{"keyword_replies", "cooldown_seconds", `86401`},
		{"keyword_replies", "cooldown_seconds", `"30"`},
		{"keyword_replies", "cooldown_seconds", `30.5`},
		{"keyword_replies", "cooldown_seconds", `null`},
		{"keyword_replies", "cooldown_seconds", `30 60`},
		{"blocked_words", "words", `["` + string([]byte{0xff}) + `"]`},
	} {
		t.Run(change.id+"/"+change.field+"/"+change.raw, func(t *testing.T) {
			config := businessRuleConfig()
			before, _ := json.Marshal(config)
			if err := Apply(&config, change.id, change.field, json.RawMessage(change.raw)); err != ErrSettings {
				t.Fatalf("invalid settings accepted: %v", err)
			}
			after, _ := json.Marshal(config)
			if string(after) != string(before) {
				t.Fatal("a rejected field change partially mutated stored settings")
			}
		})
	}
	if Apply(nil, "keyword_replies", "cooldown_seconds", json.RawMessage(`30`)) != ErrSettings {
		t.Fatal("nil settings target accepted")
	}
}

func TestRuleSettingsValidationBoundsAndUTF8(t *testing.T) {
	valid := func() Config {
		return Config{
			BlockedWords:   []string{"literal", "literal"},
			BlockedRules:   []BlockedRule{{Name: "pair", Words: []string{"first", "second"}}},
			KeywordReplies: []KeywordRule{{Keyword: "hello", Reply: "world"}},
		}
	}
	badUTF8 := string([]byte{0xff})
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{"too_many_combinations", func(c *Config) { c.BlockedRules = make([]BlockedRule, 21) }},
		{"empty_name", func(c *Config) { c.BlockedRules[0].Name = "" }},
		{"blank_name", func(c *Config) { c.BlockedRules[0].Name = " \n" }},
		{"name_byte_limit", func(c *Config) { c.BlockedRules[0].Name = strings.Repeat("中", 41) }},
		{"name_utf8", func(c *Config) { c.BlockedRules[0].Name = badUTF8 }},
		{"missing_words", func(c *Config) { c.BlockedRules[0].Words = nil }},
		{"one_word", func(c *Config) { c.BlockedRules[0].Words = []string{"first"} }},
		{"six_words", func(c *Config) { c.BlockedRules[0].Words = []string{"a", "b", "c", "d", "e", "f"} }},
		{"duplicate_words", func(c *Config) { c.BlockedRules[0].Words = []string{"first", "first"} }},
		{"blank_word", func(c *Config) { c.BlockedRules[0].Words[0] = "\t" }},
		{"word_byte_limit", func(c *Config) { c.BlockedRules[0].Words[0] = strings.Repeat("中", 67) }},
		{"word_utf8", func(c *Config) { c.BlockedRules[0].Words[0] = badUTF8 }},
		{"disabled_still_validated", func(c *Config) { c.BlockedRules[0].Disabled = true; c.BlockedRules[0].Name = "" }},
		{"legacy_word_utf8", func(c *Config) { c.BlockedWords[0] = badUTF8 }},
		{"blacklist_utf8", func(c *Config) { c.Blacklist = []string{badUTF8} }},
		{"exemptions_utf8", func(c *Config) { c.Exemptions = []string{badUTF8} }},
		{"keyword_utf8", func(c *Config) { c.KeywordReplies[0].Keyword = badUTF8 }},
		{"keyword_byte_limit", func(c *Config) { c.KeywordReplies[0].Keyword = strings.Repeat("中", 67) }},
		{"reply_utf8", func(c *Config) { c.KeywordReplies[0].Reply = badUTF8 }},
		{"reply_byte_limit", func(c *Config) { c.KeywordReplies[0].Reply = strings.Repeat("中", 1334) }},
		{"unknown_match", func(c *Config) { c.KeywordReplies[0].Match = "CONTAINS" }},
		{"too_many_keywords", func(c *Config) { c.KeywordReplies = make([]KeywordRule, 101) }},
		{"other_feature_cooldown", func(c *Config) { c.FeatureOptions = map[string]Options{"blacklist": {CooldownSeconds: 30}} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := valid()
			tc.change(&config)
			if Validate(config) != ErrSettings {
				t.Fatal("invalid rule settings accepted")
			}
		})
	}
	config := valid()
	config.BlockedRules = make([]BlockedRule, 20)
	for i := range config.BlockedRules {
		config.BlockedRules[i] = BlockedRule{Name: strings.Repeat("中", 40),
			Words: []string{strings.Repeat("x", 200), "a", "A", " a", "a "}}
	}
	config.KeywordReplies = make([]KeywordRule, 100)
	for i := range config.KeywordReplies {
		config.KeywordReplies[i] = KeywordRule{Keyword: strings.Repeat("x", 200), Reply: strings.Repeat("x", 4000)}
	}
	if err := Validate(config); err != nil {
		t.Fatal("valid maximum byte/count bounds or literal distinct words rejected", err)
	}
	config.KeywordReplies[0] = KeywordRule{Keyword: " hello ", Reply: " \n "}
	if err := Validate(config); err != nil {
		t.Fatal("existing literal whitespace settings were normalized or newly rejected", err)
	}
}

func TestKeywordReplyCooldownSettings(t *testing.T) {
	for _, seconds := range []int{0, 30, 31, 86400} {
		var config Config
		raw, _ := json.Marshal(seconds)
		if err := Apply(&config, "keyword_replies", "cooldown_seconds", raw); err != nil {
			t.Fatalf("valid cooldown %d: %v", seconds, err)
		}
		want := seconds
		if want == 0 {
			want = 30
		}
		if KeywordReplyCooldown(config) != want || Values(config, "keyword_replies")["cooldown_seconds"] != want ||
			BusinessRulesEnabled(config) {
			t.Fatalf("cooldown %d activated a feature or projected the wrong default", seconds)
		}
	}
}
