// Package assistant owns bounded management conversation orchestration, not transport or storage.
package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	MaxModels       = 3
	MaxTools        = 6
	MaxInputBytes   = 8000
	MaxContextBytes = 48000
	MaxOutputTokens = 2048
	MaxResultBytes  = 24000
	MaxEvents       = 512
	TurnTimeout     = 90 * time.Second
)

type Error string

func (e Error) Error() string { return string(e) }

var (
	ErrBusy    = Error("assistant_busy")
	ErrInvalid = Error("assistant_invalid_request")
	ErrLimit   = Error("assistant_limit")
	ErrModel   = Error("assistant_model_failed")
)

type Scope struct {
	Platform  string `json:"platform"`
	Kind      string `json:"kind"`
	AccountID string `json:"account_id,omitempty"`
	GroupID   string `json:"group_id,omitempty"`
}

type ToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
}

type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
	PromptDetails    struct {
		CachedTokens     int64 `json:"cached_tokens"`
		CacheWriteTokens int64 `json:"cache_write_tokens"`
	} `json:"prompt_tokens_details,omitempty"`
}

func (u Usage) Valid() bool {
	return u.PromptTokens >= 0 && u.CompletionTokens >= 0 && u.TotalTokens > 0 &&
		u.TotalTokens == u.PromptTokens+u.CompletionTokens && u.TotalTokens < 10000000 &&
		u.PromptDetails.CachedTokens >= 0 && u.PromptDetails.CacheWriteTokens >= 0 &&
		u.PromptDetails.CachedTokens+u.PromptDetails.CacheWriteTokens <= u.PromptTokens
}

type Response struct {
	Message Message
	Usage   Usage
}

type Tool struct {
	Type     string       `json:"type"`
	Function ToolFunction `json:"function"`
}

type ToolFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Parameters  map[string]any `json:"parameters"`
}

type Event struct {
	Kind string `json:"kind"`
	Data any    `json:"data"`
}

type ToolResult struct {
	Type         string    `json:"type"`
	Scope        Scope     `json:"scope"`
	SampledAt    time.Time `json:"sampled_at"`
	Verification string    `json:"verification"`
	Truncated    bool      `json:"truncated"`
	Data         any       `json:"data"`
}

type Provider interface {
	Generate(context.Context, []Message, []Tool, func(string) error) (Response, error)
}

// Host is bound by trusted control to one current website session and resource scope.
type Host interface {
	Check(context.Context) error
	Emit(context.Context, Event) error
	Call(context.Context, ToolCall) (ToolResult, error)
	Reserve(context.Context, int64) (string, error)
	Settle(context.Context, string, Usage, bool) error
}

type Runtime interface {
	Run(context.Context, Provider, Host, []Message, bool) error
}

type Runner struct{}

func safeCode(err error) string {
	var code Error
	if errors.As(err, &code) {
		return string(code)
	}
	return string(ErrModel)
}

func payloadSize(v any) int {
	b, err := json.Marshal(v)
	if err != nil {
		return MaxContextBytes + 1
	}
	return len(b)
}
