package features

import (
	"fmt"
	"strings"
	"testing"
)

func businessRuleConfig() Config {
	return Config{
		BlockedWords:   []string{"bad", "a.b"},
		Blacklist:      []string{"blocked", "exempt"},
		Exemptions:     []string{"exempt"},
		KeywordReplies: []KeywordRule{{Keyword: "hello", Reply: "first"}, {Keyword: "hello world", Reply: "second"}},
		FeatureOptions: map[string]Options{
			"blocked_words": {Enabled: true}, "blacklist": {Enabled: true},
			"exemptions": {Enabled: true}, "keyword_replies": {Enabled: true},
		},
	}
}

func TestBusinessRulesLiteralPriorityAndRole(t *testing.T) {
	for _, tc := range []struct {
		name, sender, text, role, action, rule, reply string
		bot, challenge, suppress                      bool
	}{
		{name: "blocked_before_reply", sender: "human", text: "bad hello", role: "member", action: BusinessRecall, rule: "blocked_words", suppress: true},
		{name: "blocked_before_blacklist", sender: "blocked", text: "bad hello", role: "member", action: BusinessRecall, rule: "blocked_words", suppress: true},
		{name: "blacklist_before_reply", sender: "blocked", text: "hello", role: "member", action: BusinessMute, rule: "blacklist", suppress: true},
		{name: "exemption_skips_moderation", sender: "exempt", text: "bad hello", role: "member", action: BusinessReply, rule: "keyword_replies", reply: "first", suppress: true},
		{name: "admin_skips_moderation", sender: "blocked", text: "bad hello", role: "admin", action: BusinessReply, rule: "keyword_replies", reply: "first", suppress: true},
		{name: "owner_skips_moderation", sender: "blocked", text: "bad hello", role: "owner", action: BusinessReply, rule: "keyword_replies", reply: "first", suppress: true},
		{name: "unknown_role", sender: "blocked", text: "bad hello"},
		{name: "invalid_role", sender: "blocked", text: "bad hello", role: "moderator"},
		{name: "bot", sender: "bot", text: "bad hello", role: "member", bot: true},
		{name: "challenge", sender: "blocked", text: "bad hello", role: "member", challenge: true},
		{name: "first_keyword_only", sender: "human", text: "hello world", role: "member", action: BusinessReply, rule: "keyword_replies", reply: "first", suppress: true},
		{name: "literal_not_regex", sender: "human", text: "a.b", role: "member", action: BusinessRecall, rule: "blocked_words", suppress: true},
		{name: "no_regex", sender: "human", text: "acb", role: "member"},
		{name: "case_sensitive", sender: "human", text: "HELLO BAD", role: "member"},
		{name: "empty_sender", text: "bad", role: "member"},
		{name: "invalid_utf8", sender: "human", text: string([]byte{'b', 'a', 'd', 0xff}), role: "member"},
		{name: "oversized_source", sender: "human", text: "bad" + strings.Repeat("x", 16384), role: "member"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := EvaluateBusinessRules(businessRuleConfig(), BusinessRuleInput{Sender: tc.sender, Text: tc.text,
				Role: tc.role, PoolRobot: tc.bot, Challenge: tc.challenge})
			if got.Action != tc.action || got.Rule != tc.rule || got.Text != tc.reply || got.SuppressHeat != tc.suppress {
				t.Fatalf("unexpected decision: %+v", got)
			}
			if got.Action == BusinessMute && (got.Minutes != 10 || got.Target != tc.sender) {
				t.Fatalf("mute must be fixed to this member for ten minutes: %+v", got)
			}
			if got.Action == BusinessRecall && got.Target != tc.sender {
				t.Fatal("recall target escaped original sender")
			}
		})
	}
}

func TestBusinessRulesEnabledAndAdminOnly(t *testing.T) {
	c := businessRuleConfig()
	c.FeatureOptions = map[string]Options{}
	if BusinessRulesEnabled(c) || BusinessRuleCandidate(c, BusinessRuleInput{Sender: "blocked", Text: "bad hello"}) {
		t.Fatal("saved data activated a disabled feature")
	}
	c.FeatureOptions["keyword_replies"] = Options{Enabled: true, AdminOnly: true}
	for _, role := range []string{"", "member", "admin", "owner"} {
		in := BusinessRuleInput{Sender: "human", Text: "hello", Role: role}
		got := EvaluateBusinessRules(c, in)
		want := role == "owner" || role == "admin"
		if (got.Action == BusinessReply) != want || got.SuppressHeat != want {
			t.Fatalf("%s: %+v", role, got)
		}
	}
	c.FeatureOptions["exemptions"] = Options{Enabled: true}
	c.FeatureOptions["keyword_replies"] = Options{}
	if BusinessRulesEnabled(c) {
		t.Fatal("exemptions alone created an execution consumer")
	}
}

func TestBusinessReplyByteLimit(t *testing.T) {
	for _, reply := range []string{"", strings.Repeat("x", 4000), strings.Repeat("x", 4001),
		strings.Repeat("\xe4\xb8\xad", 1333), strings.Repeat("\xe4\xb8\xad", 1334), string([]byte{0xff})} {
		c := businessRuleConfig()
		c.KeywordReplies = []KeywordRule{{Keyword: "hello", Reply: reply}}
		got := EvaluateBusinessRules(c, BusinessRuleInput{Sender: "human", Text: "hello", Role: "member"})
		valid := len(reply) == 4000 || len(reply) == 3999
		if (got.Action == BusinessReply) != valid || !got.SuppressHeat {
			t.Fatalf("reply length %d: %+v", len(reply), got)
		}
	}
}

func TestKeywordRuleMatchModesAndDisabledOrder(t *testing.T) {
	for _, tc := range []struct {
		name, text, reply string
		rules             []KeywordRule
	}{
		{"legacy_contains", "well hello world", "legacy", []KeywordRule{{Keyword: "hello", Reply: "legacy"}}},
		{"explicit_contains", "well hello world", "contains", []KeywordRule{{Keyword: "hello", Reply: "contains", Match: "contains"}}},
		{"exact", "hello", "exact", []KeywordRule{{Keyword: "hello", Reply: "exact", Match: "exact"}}},
		{"exact_does_not_contain", "hello world", "", []KeywordRule{{Keyword: "hello", Reply: "exact", Match: "exact"}}},
		{"exact_does_not_trim", " hello ", "", []KeywordRule{{Keyword: "hello", Reply: "exact", Match: "exact"}}},
		{"exact_preserves_spaces", " hello ", "spaces", []KeywordRule{{Keyword: " hello ", Reply: "spaces", Match: "exact"}}},
		{"case_sensitive", "HELLO", "", []KeywordRule{{Keyword: "hello", Reply: "contains", Match: "contains"}}},
		{"literal_punctuation", "a.b[0]", "literal", []KeywordRule{{Keyword: "a.b[0]", Reply: "literal"}}},
		{"no_regex", "axb0", "", []KeywordRule{{Keyword: "a.b[0]", Reply: "literal"}}},
		{"no_unicode_normalization", "e\u0301", "", []KeywordRule{{Keyword: "\u00e9", Reply: "literal"}}},
		{"disabled", "hello", "", []KeywordRule{{Keyword: "hello", Reply: "disabled", Disabled: true}}},
		{"disabled_first_is_skipped", "hello", "second", []KeywordRule{
			{Keyword: "hello", Reply: "disabled", Disabled: true}, {Keyword: "hello", Reply: "second", Match: "exact"}}},
		{"saved_order_precedes_specificity", "hello", "first", []KeywordRule{
			{Keyword: "hell", Reply: "first"}, {Keyword: "hello", Reply: "second", Match: "exact"}}},
		{"exact_miss_continues_in_saved_order", "hello world", "second", []KeywordRule{
			{Keyword: "hello", Reply: "first", Match: "exact"}, {Keyword: "hello", Reply: "second"}}},
		{"unknown_match_fails_closed", "hello", "", []KeywordRule{{Keyword: "hello", Reply: "unknown", Match: "regex"}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := Config{KeywordReplies: tc.rules, FeatureOptions: map[string]Options{"keyword_replies": {Enabled: true}}}
			in := BusinessRuleInput{Sender: "human", Text: tc.text, Role: "member"}
			got := EvaluateBusinessRules(config, in)
			if BusinessRuleCandidate(config, in) != (tc.reply != "") || got.Text != tc.reply ||
				(got.Action == BusinessReply) != (tc.reply != "") || got.SuppressHeat != (tc.reply != "") {
				t.Fatalf("candidate and decision disagreed with literal rule matching: %+v", got)
			}
		})
	}
}

func TestBlockedCombinationLiteralAND(t *testing.T) {
	for _, tc := range []struct {
		name, text string
		rule       BlockedRule
		match      bool
	}{
		{"both_words", "prefix a.b and 猫 suffix", BlockedRule{Name: "pair", Words: []string{"a.b", "猫"}}, true},
		{"any_word_order", "猫 then a.b", BlockedRule{Name: "pair", Words: []string{"a.b", "猫"}}, true},
		{"only_first", "a.b", BlockedRule{Name: "pair", Words: []string{"a.b", "猫"}}, false},
		{"only_second", "猫", BlockedRule{Name: "pair", Words: []string{"a.b", "猫"}}, false},
		{"no_regex", "axb and 猫", BlockedRule{Name: "pair", Words: []string{"a.b", "猫"}}, false},
		{"disabled", "a.b 猫", BlockedRule{Name: "pair", Words: []string{"a.b", "猫"}, Disabled: true}, false},
		{"overlapping_literals", "alphabet", BlockedRule{Name: "pair", Words: []string{"alpha", "alphabet"}}, true},
		{"case_sensitive", "a B", BlockedRule{Name: "pair", Words: []string{"a", "b"}}, false},
		{"spaces_are_literal", "a b", BlockedRule{Name: "pair", Words: []string{" a ", "b"}}, false},
		{"distinct_unicode_forms", "\u00e9 only", BlockedRule{Name: "pair", Words: []string{"\u00e9", "e\u0301"}}, false},
		{"empty_words_do_not_match_everything", "text", BlockedRule{Name: "pair"}, false},
		{"one_word_fails_closed", "first", BlockedRule{Name: "pair", Words: []string{"first"}}, false},
		{"duplicates_fail_closed", "first", BlockedRule{Name: "pair", Words: []string{"first", "first"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := Config{BlockedRules: []BlockedRule{tc.rule}, FeatureOptions: map[string]Options{"blocked_words": {Enabled: true}}}
			in := BusinessRuleInput{Sender: "human", Text: tc.text, Role: "member"}
			got := EvaluateBusinessRules(config, in)
			if BusinessRuleCandidate(config, in) != tc.match || (got.Action == BusinessRecall) != tc.match ||
				got.SuppressHeat != tc.match || tc.match && (got.Rule != "blocked_words" || got.Target != in.Sender) {
				t.Fatalf("combination did not require all literal words: %+v", got)
			}
		})
	}
	config := Config{
		BlockedWords: []string{"legacy"},
		BlockedRules: []BlockedRule{
			{Name: "disabled", Words: []string{"red", "blue"}, Disabled: true},
			{Name: "enabled", Words: []string{"green", "gold"}},
		},
		FeatureOptions: map[string]Options{"blocked_words": {Enabled: true}},
	}
	for _, text := range []string{"legacy", "green and gold"} {
		if got := EvaluateBusinessRules(config, BusinessRuleInput{Sender: "human", Text: text, Role: "member"}); got.Action != BusinessRecall {
			t.Fatalf("legacy words and enabled combination rules must be OR alternatives: %q %+v", text, got)
		}
	}
}

func TestCombinedRulesPreserveProtectionAndPriority(t *testing.T) {
	for _, tc := range []struct {
		name, sender, role, action, rule string
		poolRobot, challenge             bool
	}{
		{name: "combination_before_blacklist_and_reply", sender: "blocked", role: "member", action: BusinessRecall, rule: "blocked_words"},
		{name: "combination_before_reply", sender: "human", role: "member", action: BusinessRecall, rule: "blocked_words"},
		{name: "exempt_member", sender: "exempt", role: "member", action: BusinessReply, rule: "keyword_replies"},
		{name: "admin", sender: "blocked", role: "admin", action: BusinessReply, rule: "keyword_replies"},
		{name: "owner", sender: "blocked", role: "owner", action: BusinessReply, rule: "keyword_replies"},
		{name: "unknown_role", sender: "blocked"},
		{name: "pool_robot", sender: "blocked", role: "member", poolRobot: true},
		{name: "challenge", sender: "blocked", role: "member", challenge: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := businessRuleConfig()
			config.BlockedWords = nil
			config.BlockedRules = []BlockedRule{{Name: "pair", Words: []string{"bad", "hello"}}}
			in := BusinessRuleInput{Sender: tc.sender, Text: "bad hello", Role: tc.role, PoolRobot: tc.poolRobot, Challenge: tc.challenge}
			got := EvaluateBusinessRules(config, in)
			if got.Action != tc.action || got.Rule != tc.rule || got.SuppressHeat != (tc.action != "") {
				t.Fatalf("new rules bypassed protection or changed priority: %+v", got)
			}
		})
	}
	config := businessRuleConfig()
	config.BlockedWords = nil
	config.BlockedRules = []BlockedRule{{Name: "pair", Words: []string{"bad", "hello"}}}
	config.FeatureOptions["blocked_words"] = Options{}
	if got := EvaluateBusinessRules(config, BusinessRuleInput{Sender: "blocked", Text: "bad hello", Role: "member"}); got.Action != BusinessMute {
		t.Fatalf("disabled combination module replaced blacklist priority: %+v", got)
	}
	config.FeatureOptions["keyword_replies"] = Options{Enabled: true, AdminOnly: true}
	for _, role := range []string{"member", "admin", "owner"} {
		got := EvaluateBusinessRules(config, BusinessRuleInput{Sender: "human", Text: "hello", Role: role})
		if (got.Action == BusinessReply) != (role == "admin" || role == "owner") {
			t.Fatalf("new keyword rules bypassed administrator-only setting: %s %+v", role, got)
		}
	}
}

func TestEnhancedRulesBoundedContext(t *testing.T) {
	config := Config{
		BlockedRules:   []BlockedRule{{Name: "pair", Words: []string{"left", "right"}}},
		KeywordReplies: []KeywordRule{{Keyword: "left", Reply: "reply"}},
		FeatureOptions: map[string]Options{"blocked_words": {Enabled: true}, "keyword_replies": {Enabled: true}},
	}
	for _, tc := range []struct {
		name, text string
		match      bool
	}{
		{"empty", "", false},
		{"at_byte_limit", "left" + strings.Repeat("x", 16384-len("leftright")) + "right", true},
		{"past_byte_limit", "left" + strings.Repeat("x", 16385-len("leftright")) + "right", false},
		{"invalid_utf8", "left right" + string([]byte{0xff}), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := BusinessRuleInput{Sender: "human", Text: tc.text, Role: "member"}
			got := EvaluateBusinessRules(config, in)
			if BusinessRuleCandidate(config, in) != tc.match || (got.Action == BusinessRecall) != tc.match || got.SuppressHeat != tc.match {
				t.Fatalf("bounded context changed: %+v", got)
			}
		})
	}
}

func TestLegacyEmptyReplySuppressesHeatWithoutAction(t *testing.T) {
	for _, mode := range []string{"", "contains", "exact"} {
		t.Run("match="+mode, func(t *testing.T) {
			config := Config{
				KeywordReplies: []KeywordRule{
					{Keyword: "hello", Reply: "", Match: mode},
					{Keyword: "hello", Reply: "must not fall through"},
				},
				FeatureOptions: map[string]Options{"keyword_replies": {Enabled: true}},
			}
			if err := Validate(config); err != nil {
				t.Fatal("legacy empty reply became invalid", err)
			}
			for _, role := range []string{"member", "admin", "owner"} {
				in := BusinessRuleInput{Sender: "human", Text: "hello", Role: role}
				want := BusinessDecision{Rule: "keyword_replies", SuppressHeat: true}
				if !BusinessRuleCandidate(config, in) || EvaluateBusinessRules(config, in) != want {
					t.Fatalf("legacy empty reply sent, fell through or stopped suppressing heat for %s: %+v",
						role, EvaluateBusinessRules(config, in))
				}
			}
		})
	}
}

func BenchmarkBusinessRulesLegacyNoCombinations(b *testing.B) {
	blocked := Config{
		BlockedWords:   make([]string, 200),
		FeatureOptions: map[string]Options{"blocked_words": {Enabled: true}},
	}
	for i := range blocked.BlockedWords {
		blocked.BlockedWords[i] = fmt.Sprintf("blocked-%03d", i)
	}
	replies := Config{
		KeywordReplies: make([]KeywordRule, 100),
		FeatureOptions: map[string]Options{"keyword_replies": {Enabled: true}},
	}
	for i := range replies.KeywordReplies {
		replies.KeywordReplies[i] = KeywordRule{Keyword: fmt.Sprintf("keyword-%03d", i), Reply: "legacy reply"}
	}
	for _, combinations := range []string{"nil", "empty"} {
		b.Run(combinations, func(b *testing.B) {
			for _, tc := range []struct {
				name, text string
				config     Config
				want       BusinessDecision
			}{
				{"blocked_hit", "prefix blocked-199 suffix", blocked,
					BusinessDecision{Rule: "blocked_words", Action: BusinessRecall, Target: "human", SuppressHeat: true}},
				{"blocked_miss", "ordinary group text", blocked, BusinessDecision{}},
				{"keyword_hit", "prefix keyword-099 suffix", replies,
					BusinessDecision{Rule: "keyword_replies", Action: BusinessReply, Text: "legacy reply", SuppressHeat: true}},
				{"keyword_miss", "ordinary group text", replies, BusinessDecision{}},
			} {
				b.Run(tc.name, func(b *testing.B) {
					config := tc.config
					if combinations == "empty" {
						config.BlockedRules = []BlockedRule{}
					}
					if err := Validate(config); err != nil {
						b.Fatal(err)
					}
					in := BusinessRuleInput{Sender: "human", Text: tc.text, Role: "member"}
					b.Run("candidate", func(b *testing.B) {
						want := tc.want.Rule != ""
						if BusinessRuleCandidate(config, in) != want {
							b.Fatal("invalid candidate benchmark fixture")
						}
						b.ReportAllocs()
						for b.Loop() {
							BusinessRuleCandidate(config, in)
						}
					})
					b.Run("evaluate", func(b *testing.B) {
						if EvaluateBusinessRules(config, in) != tc.want {
							b.Fatal("invalid decision benchmark fixture")
						}
						b.ReportAllocs()
						for b.Loop() {
							EvaluateBusinessRules(config, in)
						}
					})
				})
			}
		})
	}
}

func FuzzBusinessRulesNoRoleNoAction(f *testing.F) {
	f.Add("blocked", "bad hello")
	f.Add("human", "hello")
	f.Fuzz(func(t *testing.T, sender, text string) {
		config := businessRuleConfig()
		for _, role := range []string{"", "unknown"} {
			if got := EvaluateBusinessRules(config, BusinessRuleInput{Sender: sender, Text: text, Role: role}); got.Action != "" {
				t.Fatalf("unverified role executed: %+v", got)
			}
		}
		for _, in := range []BusinessRuleInput{
			{Sender: sender, Text: text, Role: "member", PoolRobot: true},
			{Sender: sender, Text: text, Role: "member", Challenge: true},
		} {
			if got := EvaluateBusinessRules(config, in); got.Action != "" || got.SuppressHeat {
				t.Fatalf("excluded source executed: %+v", got)
			}
		}
	})
}
