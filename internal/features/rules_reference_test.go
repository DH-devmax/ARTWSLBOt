package features

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// This oracle uses only the contract and standard string operations, not the
// production candidate or matching helpers.
func referenceBusinessDecision(config Config, in BusinessRuleInput) BusinessDecision {
	if in.Challenge || in.PoolRobot || in.Sender == "" || in.Text == "" ||
		len(in.Text) > 16384 || !utf8.ValidString(in.Text) ||
		in.Role != "member" && in.Role != "admin" && in.Role != "owner" {
		return BusinessDecision{}
	}
	exempt := false
	if config.FeatureOptions["exemptions"].Enabled {
		for _, sender := range config.Exemptions {
			exempt = exempt || sender == in.Sender
		}
	}
	if in.Role == "member" && !exempt {
		blocked := false
		if config.FeatureOptions["blocked_words"].Enabled {
			for _, word := range config.BlockedWords {
				blocked = blocked || strings.Contains(in.Text, word)
			}
			for _, rule := range config.BlockedRules {
				all := !rule.Disabled
				for _, word := range rule.Words {
					all = all && strings.Contains(in.Text, word)
				}
				blocked = blocked || all
			}
		}
		if blocked {
			return BusinessDecision{Rule: "blocked_words", Action: BusinessRecall, Target: in.Sender, SuppressHeat: true}
		}
		if config.FeatureOptions["blacklist"].Enabled {
			for _, sender := range config.Blacklist {
				if sender == in.Sender {
					return BusinessDecision{Rule: "blacklist", Action: BusinessMute, Target: in.Sender, Minutes: 10, SuppressHeat: true}
				}
			}
		}
	}
	options := config.FeatureOptions["keyword_replies"]
	if options.Enabled && (!options.AdminOnly || in.Role != "member") {
		for _, rule := range config.KeywordReplies {
			match := in.Text == rule.Keyword
			if rule.Match != "exact" {
				match = strings.Contains(in.Text, rule.Keyword)
			}
			if !rule.Disabled && match {
				if rule.Reply == "" {
					return BusinessDecision{Rule: "keyword_replies", SuppressHeat: true}
				}
				return BusinessDecision{Rule: "keyword_replies", Action: BusinessReply, Text: rule.Reply, SuppressHeat: true}
			}
		}
	}
	return BusinessDecision{}
}

func FuzzBusinessRulesReference(f *testing.F) {
	f.Add("hello world red blue", "red", "blue", "hello", uint16(0))
	f.Add("hello", "red", "blue", "hello", uint16(7))
	f.Add("legacy!", "a.b", "猫", "reply", uint16(31))
	f.Add("a.b 猫", "a.b", "猫", "猫", uint16(513))
	f.Add("e\u0301", "\u00e9", "e\u0301", "\u00e9", uint16(1023))
	f.Add("hello", "red", "blue", "hello", uint16(32768))
	f.Fuzz(func(t *testing.T, text, first, second, keyword string, flags uint16) {
		mode := []string{"", "contains", "exact"}[int(flags)%3]
		config := Config{
			BlockedWords: []string{"legacy!"},
			BlockedRules: []BlockedRule{{Name: "synthetic", Words: []string{first, second}, Disabled: flags&4 != 0}},
			Blacklist:    []string{"blocked"}, Exemptions: []string{"exempt"},
			KeywordReplies: []KeywordRule{
				{Keyword: keyword, Reply: "first reply", Match: mode, Disabled: flags&8 != 0},
				{Keyword: keyword, Reply: "fallback reply"},
			},
			FeatureOptions: map[string]Options{
				"blocked_words":   {Enabled: flags&16 == 0},
				"blacklist":       {Enabled: flags&32 == 0},
				"exemptions":      {Enabled: flags&64 == 0},
				"keyword_replies": {Enabled: flags&128 == 0, AdminOnly: flags&256 != 0},
			},
		}
		if flags&32768 != 0 {
			config.KeywordReplies[0].Reply = ""
		}
		if Validate(config) != nil {
			return
		}
		in := BusinessRuleInput{
			Sender: []string{"human", "blocked", "exempt", ""}[int(flags>>10)%4],
			Text:   text, Role: []string{"member", "admin", "owner", "", "unknown"}[int(flags>>3)%5],
			PoolRobot: flags&512 != 0, Challenge: flags&16384 != 0,
		}
		got, want := EvaluateBusinessRules(config, in), referenceBusinessDecision(config, in)
		if got != want {
			t.Fatalf("decision differs from independent contract: got %+v want %+v", got, want)
		}
		if got.Action != "" && !BusinessRuleCandidate(config, in) {
			t.Fatal("candidate filter discarded an executable rule")
		}
	})
}
