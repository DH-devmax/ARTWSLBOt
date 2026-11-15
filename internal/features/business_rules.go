package features

import (
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	BusinessRecall = "recall_message"
	BusinessMute   = "mute_member"
	BusinessReply  = "send_text"
)

// BusinessRuleInput is a single authenticated group text. A role must be
// supplied from a fresh native observation; an unknown role never authorizes
// moderation or an administrator-only reply.
type BusinessRuleInput struct {
	Sender, Text, Role string
	PoolRobot          bool
	Challenge          bool
}

type BusinessDecision struct {
	Rule         string `json:"rule,omitempty"`
	Action       string `json:"action,omitempty"`
	Target       string `json:"target,omitempty"`
	Text         string `json:"text,omitempty"`
	Minutes      int    `json:"minutes,omitempty"`
	SuppressHeat bool   `json:"suppress_heat"`
}

// KeywordReplyCooldown returns seconds from a validated configuration. Zero
// retains the cooldown used before the setting was exposed.
func KeywordReplyCooldown(config Config) int {
	if seconds := config.FeatureOptions["keyword_replies"].CooldownSeconds; seconds != 0 {
		return seconds
	}
	return 30
}

func containedKeyword(rules []KeywordRule, text string) int {
	for i, rule := range rules {
		if rule.Keyword != "" && strings.Contains(text, rule.Keyword) {
			return i
		}
	}
	return -1
}

func matchingKeywordRule(rules []KeywordRule, text string) *KeywordRule {
	for len(rules) > 0 {
		i := containedKeyword(rules, text)
		if i < 0 {
			return nil
		}
		rule := &rules[i]
		if !rule.Disabled {
			switch rule.Match {
			case "", "contains":
				return rule
			case "exact":
				if text == rule.Keyword {
					return rule
				}
			}
		}
		rules = rules[i+1:]
	}
	return nil
}

func blockedWordsMatch(config Config, text string) bool {
	for _, word := range config.BlockedWords {
		if word != "" && strings.Contains(text, word) {
			return true
		}
	}
	for _, rule := range config.BlockedRules {
		if rule.Disabled || !validBlockedRule(rule) {
			continue
		}
		if slices.ContainsFunc(rule.Words, func(word string) bool { return !strings.Contains(text, word) }) {
			continue
		}
		return true
	}
	return false
}

func BusinessRulesEnabled(config Config) bool {
	for _, id := range []string{"blocked_words", "blacklist", "keyword_replies"} {
		if config.FeatureOptions[id].Enabled {
			return true
		}
	}
	return false
}

// BusinessRuleCandidate avoids a native role read for texts which cannot match
// any enabled rule. It is a filter, never an authorization decision.
func BusinessRuleCandidate(config Config, in BusinessRuleInput) bool {
	if in.Challenge || in.PoolRobot || in.Sender == "" || in.Text == "" ||
		len(in.Text) > 16384 || !utf8.ValidString(in.Text) {
		return false
	}
	if config.FeatureOptions["blocked_words"].Enabled && blockedWordsMatch(config, in.Text) {
		return true
	}
	if config.FeatureOptions["blacklist"].Enabled && slices.Contains(config.Blacklist, in.Sender) {
		return true
	}
	if config.FeatureOptions["keyword_replies"].Enabled {
		return matchingKeywordRule(config.KeywordReplies, in.Text) != nil
	}
	return false
}

// EvaluateBusinessRules chooses at most one operation. Native administrators
// and exemptions skip moderation but may trigger an enabled keyword reply.
func EvaluateBusinessRules(config Config, in BusinessRuleInput) BusinessDecision {
	if in.Challenge || in.PoolRobot || in.Sender == "" || in.Text == "" ||
		len(in.Text) > 16384 || !utf8.ValidString(in.Text) ||
		in.Role != "member" && in.Role != "admin" && in.Role != "owner" {
		return BusinessDecision{}
	}
	blocked, blacklist := config.FeatureOptions["blocked_words"].Enabled, config.FeatureOptions["blacklist"].Enabled
	if in.Role == "member" && (blocked || blacklist) {
		exempt := config.FeatureOptions["exemptions"].Enabled && slices.Contains(config.Exemptions, in.Sender)
		if !exempt {
			if blocked && blockedWordsMatch(config, in.Text) {
				return BusinessDecision{Rule: "blocked_words", Action: BusinessRecall,
					Target: in.Sender, SuppressHeat: true}
			}
			if blacklist && slices.Contains(config.Blacklist, in.Sender) {
				return BusinessDecision{Rule: "blacklist", Action: BusinessMute,
					Target: in.Sender, Minutes: 10, SuppressHeat: true}
			}
		}
	}
	options := config.FeatureOptions["keyword_replies"]
	if options.Enabled && (!options.AdminOnly || in.Role == "admin" || in.Role == "owner") {
		if rule := matchingKeywordRule(config.KeywordReplies, in.Text); rule != nil {
			if len(rule.Reply) == 0 || len(rule.Reply) > 4000 || !utf8.ValidString(rule.Reply) {
				return BusinessDecision{Rule: "keyword_replies", SuppressHeat: true}
			}
			return BusinessDecision{Rule: "keyword_replies", Action: BusinessReply,
				Text: rule.Reply, SuppressHeat: true}
		}
	}
	return BusinessDecision{}
}
