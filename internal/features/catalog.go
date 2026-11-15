// Package features owns the management feature catalog and its bounded settings.
// Transport and execution are deliberately outside this package.
package features

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"unicode/utf8"
)

type Field struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Type  string `json:"type"`
	Limit int    `json:"limit,omitempty"`
}

type Definition struct {
	ID                 string   `json:"id"`
	Name               string   `json:"name"`
	Category           string   `json:"category"`
	Module             string   `json:"module"`
	Status             string   `json:"status"`
	Legacy             string   `json:"-"`
	Detail             string   `json:"detail,omitempty"`
	Summary            string   `json:"summary"`
	NativeDependencies []string `json:"native_dependencies"`
	Fields             []Field  `json:"fields"`
}

type Options struct {
	Enabled         bool `json:"enabled"`
	AdminOnly       bool `json:"admin_only,omitempty"`
	CooldownSeconds int  `json:"cooldown_seconds,omitempty"`
}

type KeywordRule struct {
	Keyword  string `json:"keyword"`
	Reply    string `json:"reply"`
	Match    string `json:"match,omitempty"`
	Disabled bool   `json:"disabled,omitempty"`
}

type BlockedRule struct {
	Name     string   `json:"name"`
	Words    []string `json:"words"`
	Disabled bool     `json:"disabled,omitempty"`
}

// Existing rule data retains its original keys and omitted options retain
// their original matching and cooldown behavior.
type Config struct {
	BlockedWords      []string           `json:"blocked_words"`
	BlockedRules      []BlockedRule      `json:"blocked_rules,omitempty"`
	Blacklist         []string           `json:"blacklist"`
	Exemptions        []string           `json:"exemptions"`
	KeywordReplies    []KeywordRule      `json:"keyword_replies"`
	AICooldownSeconds int                `json:"ai_cooldown_seconds"`
	FeatureOptions    map[string]Options `json:"feature_options,omitempty"`
}

func Catalog() []Definition {
	toggle := Field{"enabled", "启用配置", "boolean", 0}
	return []Definition{
		{ID: "blocked_words", Name: "拦截词", Category: "群管", Module: "moderation", Status: "implemented", Legacy: "groups:moderation",
			Summary: "普通成员消息字面包含任一拦截词，或包含一条已启用组合规则的全部词后，仅撤回当前消息；运行受平台准入和本群开关控制。", NativeDependencies: []string{"receive_message", "read_roles", "recall_message"},
			Fields: []Field{toggle, {"words", "拦截词", "string_list", 200}, {"rules", "组合拦截规则", "blocked_rules", 20}}},
		{ID: "blacklist", Name: "业务黑名单", Category: "群管", Module: "moderation", Status: "implemented", Legacy: "groups:moderation",
			Summary: "名单中的普通成员发言后禁言10分钟，处置周期内去重；与原生永久禁止入群名单分开。", NativeDependencies: []string{"receive_message", "list_members", "read_roles", "mute_member"},
			Fields: []Field{toggle, {"members", "成员 UID", "string_list", 2000}}},
		{ID: "exemptions", Name: "豁免名单", Category: "群管", Module: "moderation", Status: "implemented", Legacy: "groups:moderation",
			Summary: "供群管规则跳过已核验的豁免成员，不单独执行动作。", NativeDependencies: []string{"list_members", "read_roles"},
			Fields: []Field{toggle, {"members", "成员 UID", "string_list", 2000}}},
		{ID: "keyword_replies", Name: "群内关键词回复", Category: "回复", Module: "replies", Status: "implemented", Legacy: "groups:replies",
			Summary: "按保存顺序选择首个已启用的字面包含或完全匹配规则，仅回复一条群文本；每群冷却默认30秒，可配置30至86400秒，可限定管理员触发。", NativeDependencies: []string{"receive_message", "read_roles", "send_text"},
			Fields: []Field{toggle, {"admin_only", "仅群管理员触发", "boolean", 0}, {"rules", "关键词与回复", "keyword_rules", 100}, {"cooldown_seconds", "回复冷却（秒）", "integer", 86400}}},
		{ID: "image_replies", Name: "图片回复", Category: "回复", Module: "replies", Status: "unavailable",
			Summary: "匹配条件后发送群图片。", NativeDependencies: []string{"receive_message", "send_image"}},
		{ID: "scheduled_messages", Name: "定时发布", Category: "自动化", Module: "schedules", Status: "unavailable",
			Summary: "按时段提交群文本，调度与原生发送分离。", NativeDependencies: []string{"list_groups", "read_roles", "send_text"}},
		{ID: "rankings", Name: "群消息排行", Category: "自动化", Module: "rankings", Status: "unavailable",
			Summary: "按群统计去重消息并发布排行。", NativeDependencies: []string{"receive_message", "list_members", "send_text"}},
		{ID: "cards", Name: "群名片", Category: "自动化", Module: "cards", Status: "unavailable",
			Summary: "按照名片规则处理已核验成员。", NativeDependencies: []string{"list_members", "read_roles", "set_member_card"}},
		{ID: "friend_approval", Name: "好友审批", Category: "人工操作", Module: "friends", Status: "manual_only",
			Summary: "人工逐项审批；接入向导使用独立准入，核对时可默认拒绝唯一超时旧申请并要求重申，真实效果独立验收。不是群管规则、助手或热群工具；旧ID和历史配置保留。", NativeDependencies: []string{}},
		{ID: "lottery", Name: "活动抽奖", Category: "活动", Module: "activities", Status: "web_available", Detail: "activities",
			Summary: "现有 Web 创建与抽取；群内报名和结果发布待迁移。", NativeDependencies: []string{"receive_message", "list_members", "send_text"}},
		{ID: "invitation_tracking", Name: "邀请记录", Category: "活动", Module: "activities", Status: "unavailable",
			Summary: "从原生邀请证据核对群和邀请归属。", NativeDependencies: []string{"list_members", "read_invitations"}},
		{ID: "invitation_rewards", Name: "邀请奖励", Category: "财务", Module: "finance", Status: "unavailable",
			Summary: "按核验后的邀请证据生成奖励记录；不自动转账。", NativeDependencies: []string{"list_members", "read_invitations"}},
		{ID: "cashback", Name: "返现流程", Category: "财务", Module: "finance", Status: "unavailable",
			Summary: "保留核验、台账及人工财务确认流程；不自动转账。", NativeDependencies: []string{"list_members", "read_invitations"}},
		{ID: "ledger", Name: "财务台账", Category: "财务", Module: "finance", Status: "web_available", Detail: "ledger",
			Summary: "网站内记录与确认，不依赖原生转账。", NativeDependencies: []string{}},
		{ID: "group_ai", Name: "热群机器人入群 / 启停", Category: "聊天", Module: "ai", Status: "request_only", Detail: "group-ai",
			Summary: "保存聊天服务申请；群内发言仍由控制面提交。", NativeDependencies: []string{"invite_group", "receive_message", "read_roles", "send_text"}},
		{ID: "basic_group_chat", Name: "基础群文本聊天", Category: "聊天", Module: "ai", Status: "implemented", Detail: "group-ai",
			Summary:            "双平台独立准入；原生@、回复及概率参与由持久任务和共享积分钱包控制，单条文本发送。真实聊天账号与测试群验收未完成时保持关闭。",
			NativeDependencies: []string{"receive_message", "read_roles", "read_mentions", "read_quote", "send_text"}},
	}
}

func Permission(id, field string) string { return "feature:" + id + ":" + field }

func Find(id string) (Definition, bool) {
	for _, definition := range Catalog() {
		if definition.ID == id {
			return definition, true
		}
	}
	return Definition{}, false
}

func CanEdit(grants []string, definition Definition, field string) bool {
	if slices.Contains(grants, Permission(definition.ID, field)) {
		return true
	}
	if definition.ID == "blocked_words" && field == "rules" ||
		definition.ID == "keyword_replies" && field == "cooldown_seconds" {
		return false
	}
	return definition.Legacy != "" && slices.Contains(grants, definition.Legacy)
}

func Values(config Config, id string) map[string]any {
	definition, ok := Find(id)
	if !ok || len(definition.Fields) == 0 {
		return map[string]any{}
	}
	options := config.FeatureOptions[id]
	out := map[string]any{"enabled": options.Enabled}
	switch id {
	case "blocked_words":
		out["words"] = nonnil(config.BlockedWords)
		out["rules"] = nonnil(config.BlockedRules)
	case "blacklist":
		out["members"] = nonnil(config.Blacklist)
	case "exemptions":
		out["members"] = nonnil(config.Exemptions)
	case "keyword_replies":
		out["admin_only"], out["rules"] = options.AdminOnly, nonnil(config.KeywordReplies)
		out["cooldown_seconds"] = KeywordReplyCooldown(config)
	}
	return out
}

func nonnil[T any](values []T) []T {
	if values == nil {
		return []T{}
	}
	return values
}

var ErrSettings = errors.New("invalid_feature_settings")

func strict(raw []byte, target any) error {
	if !utf8.Valid(raw) {
		return ErrSettings
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ErrSettings
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return ErrSettings
	}
	return nil
}

func Apply(config *Config, id, field string, raw json.RawMessage) error {
	definition, ok := Find(id)
	if config == nil || !ok || !slices.ContainsFunc(definition.Fields, func(spec Field) bool { return spec.Key == field }) {
		return ErrSettings
	}
	next := *config
	switch field {
	case "enabled", "admin_only", "cooldown_seconds":
		options := config.FeatureOptions[id]
		if field == "cooldown_seconds" {
			if strict(raw, &options.CooldownSeconds) != nil {
				return ErrSettings
			}
		} else {
			var value bool
			if strict(raw, &value) != nil {
				return ErrSettings
			}
			if field == "enabled" {
				options.Enabled = value
			} else {
				options.AdminOnly = value
			}
		}
		next.FeatureOptions = make(map[string]Options, len(config.FeatureOptions)+1)
		for key, value := range config.FeatureOptions {
			next.FeatureOptions[key] = value
		}
		next.FeatureOptions[id] = options
	case "rules":
		switch id {
		case "keyword_replies":
			var rules []KeywordRule
			if strict(raw, &rules) != nil {
				return ErrSettings
			}
			next.KeywordReplies = rules
		case "blocked_words":
			var rules []BlockedRule
			if strict(raw, &rules) != nil {
				return ErrSettings
			}
			next.BlockedRules = rules
		default:
			return ErrSettings
		}
	default:
		var values []string
		if strict(raw, &values) != nil {
			return ErrSettings
		}
		switch id {
		case "blocked_words":
			next.BlockedWords = values
		case "blacklist":
			next.Blacklist = values
		case "exemptions":
			next.Exemptions = values
		default:
			return ErrSettings
		}
	}
	if err := Validate(next); err != nil {
		return err
	}
	*config = next
	return nil
}

func Validate(config Config) error {
	for _, list := range []struct {
		values []string
		limit  int
	}{{config.BlockedWords, 200}, {config.Blacklist, 2000}, {config.Exemptions, 2000}} {
		if len(list.values) > list.limit {
			return ErrSettings
		}
		for _, value := range list.values {
			if !validRuleString(value, 200) {
				return ErrSettings
			}
		}
	}
	if len(config.BlockedRules) > 20 || len(config.KeywordReplies) > 100 ||
		config.AICooldownSeconds < 0 || config.AICooldownSeconds > 86400 {
		return ErrSettings
	}
	for _, rule := range config.BlockedRules {
		if !validBlockedRule(rule) {
			return ErrSettings
		}
	}
	for _, rule := range config.KeywordReplies {
		if !validRuleString(rule.Keyword, 200) || len(rule.Reply) > 4000 ||
			!utf8.ValidString(rule.Reply) || rule.Match != "" && rule.Match != "contains" && rule.Match != "exact" {
			return ErrSettings
		}
	}
	for id, options := range config.FeatureOptions {
		definition, ok := Find(id)
		if !ok || len(definition.Fields) == 0 || options.AdminOnly && id != "keyword_replies" ||
			options.CooldownSeconds != 0 && (id != "keyword_replies" ||
				options.CooldownSeconds < 30 || options.CooldownSeconds > 86400) {
			return ErrSettings
		}
	}
	return nil
}

func validRuleString(value string, limit int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= limit && utf8.ValidString(value)
}

func validBlockedRule(rule BlockedRule) bool {
	if !validRuleString(rule.Name, 120) || len(rule.Words) < 2 || len(rule.Words) > 5 {
		return false
	}
	for i, word := range rule.Words {
		if !validRuleString(word, 200) || slices.Contains(rule.Words[:i], word) {
			return false
		}
	}
	return true
}
