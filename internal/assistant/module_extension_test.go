package assistant

import (
	"context"
	"strings"
	"testing"
	"time"
)

type syntheticModuleHost struct {
	testHost
	exposed bool
}

func (h *syntheticModuleHost) AssistantTools() []Tool {
	if !h.exposed {
		return nil
	}
	return []Tool{{Type: "function", Function: ToolFunction{
		Name: "synthetic_business_query", Description: "Read one authorized synthetic module snapshot.",
		Parameters: map[string]any{"type": "object", "additionalProperties": false},
	}}}
}

func TestRunnerAcceptsExplicitModuleWithoutCoreChanges(t *testing.T) {
	for _, exposed := range []bool{false, true} {
		h := &syntheticModuleHost{exposed: exposed}
		h.callResult = func(call ToolCall) ToolResult {
			return ToolResult{Type: call.Function.Name, Scope: Scope{Platform: "wsl", Kind: "group", GroupID: "synthetic"},
				SampledAt: time.Now(), Verification: "snapshot", Data: map[string]any{"enabled": false}}
		}
		rounds := 0
		p := testProvider(func(_ context.Context, messages []Message, tools []Tool, emit func(string) error) (Response, error) {
			rounds++
			if len(messages) == 0 || messages[0].Role != "system" ||
				messages[0].Content != SystemInstruction() ||
				!strings.Contains(messages[0].Content, GuideVersion) {
				t.Fatal("versioned product guide was omitted")
			}
			if rounds == 1 {
				if ContainsTool(tools, "synthetic_business_query") != exposed {
					t.Fatal("module exposure did not control the whitelist")
				}
				return Response{Message: Message{Role: "assistant", ToolCalls: []ToolCall{tool("synthetic_business_query")}}}, nil
			}
			if len(messages) == 0 || messages[len(messages)-1].Role != "tool" {
				t.Fatal("module result did not reach the normal conversation")
			}
			return Response{Message: Message{Role: "assistant", Content: "The authorized module is currently disabled."}},
				emit("The authorized module is currently disabled.")
		})
		err := (Runner{}).Run(context.Background(), p, h, nil, false)
		if !exposed {
			if err != ErrInvalid || h.calls != 0 || rounds != 1 {
				t.Fatal("default-closed module was called", err, h.calls, rounds)
			}
		} else if err != nil || h.calls != 1 || rounds != 2 {
			t.Fatal("new query required a core special case", err, h.calls, rounds)
		}
	}
}
