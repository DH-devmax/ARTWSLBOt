// Package assistantadapter adapts Eino to the existing restricted Runtime port.
// It has no trusted identity, database, native transport or approval capability.
package assistantadapter

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"
	"wangshangliao/internal/assistant"
)

type Runtime struct{}
type run struct {
	mu            sync.Mutex
	host          assistant.Host
	provider      assistant.Provider
	propose       bool
	models, tools int
	seen          map[string]bool
	definitions   []assistant.Tool
}

func (Runtime) Run(parent context.Context, p assistant.Provider, h assistant.Host, history []assistant.Message, propose bool) error {
	ctx, cancel := context.WithTimeout(parent, assistant.TurnTimeout)
	defer cancel()
	state := &run{host: h, provider: p, propose: propose, seen: map[string]bool{}, definitions: assistant.ToolsFor(h, propose)}
	var tools []tool.BaseTool
	returnDirectly := make(map[string]bool)
	for _, definition := range state.definitions {
		tools = append(tools, &boundTool{state: state, definition: definition})
		if definition.Function.Name == "propose_group_config_change" || definition.Function.Name == "propose_group_action" {
			returnDirectly[definition.Function.Name] = true
		}
	}
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: "management_assistant", Instruction: assistant.SystemInstruction(), Model: &boundModel{state: state},
		MaxIterations: assistant.MaxModels,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: tools, ExecuteSequentially: true},
			ReturnDirectly:  returnDirectly,
		},
	})
	if err != nil {
		return assistant.ErrInvalid
	}
	input := make([]*schema.Message, 0, len(history))
	for _, m := range history {
		input = append(input, toSchema(m))
	}
	events := adk.NewRunner(ctx, adk.RunnerConfig{Agent: agent, EnableStreaming: false}).Run(ctx, input)
	for {
		event, ok := events.Next()
		if !ok {
			break
		}
		if event.Err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return event.Err
		}
	}
	return h.Check(ctx)
}

type boundModel struct{ state *run }

func (m *boundModel) Generate(ctx context.Context, input []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	s := m.state
	s.mu.Lock()
	s.models++
	count := s.models
	s.mu.Unlock()
	if count > assistant.MaxModels {
		return nil, assistant.ErrLimit
	}
	if err := s.host.Check(ctx); err != nil {
		return nil, err
	}
	messages := make([]assistant.Message, 0, len(input))
	for _, item := range input {
		messages = append(messages, fromSchema(item))
	}
	raw, err := json.Marshal(messages)
	if err != nil || len(raw) > assistant.MaxContextBytes {
		return nil, assistant.ErrLimit
	}
	reservation, err := s.host.Reserve(ctx, int64(len(raw)+assistant.MaxOutputTokens))
	if err != nil {
		return nil, err
	}
	response, modelErr := s.provider.Generate(ctx, messages, s.definitions, func(text string) error {
		if err := s.host.Check(ctx); err != nil {
			return err
		}
		return s.host.Emit(ctx, assistant.Event{Kind: "text_delta", Data: map[string]string{"text": text}})
	})
	settle, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	err = s.host.Settle(settle, reservation, response.Usage, modelErr != nil)
	stop()
	if err != nil {
		return nil, err
	}
	if modelErr != nil {
		return nil, modelErr
	}
	raw, err = json.Marshal(response.Message)
	if err != nil || len(raw) > assistant.MaxResultBytes {
		return nil, assistant.ErrLimit
	}
	s.mu.Lock()
	proposalEnd := 0
	for i, call := range response.Message.ToolCalls {
		if call.ID == "" || len(call.ID) > 128 || s.seen[call.ID] || call.Type != "function" ||
			!assistant.ContainsTool(s.definitions, call.Function.Name) || len(call.Function.Arguments) > assistant.MaxResultBytes {
			s.mu.Unlock()
			return nil, assistant.ErrInvalid
		}
		s.seen[call.ID] = true
		if proposalEnd == 0 && (call.Function.Name == "propose_group_config_change" || call.Function.Name == "propose_group_action") {
			proposalEnd = i + 1
		}
	}
	s.mu.Unlock()
	// A proposal ends this run; later calls in the same response must wait too.
	if proposalEnd > 0 {
		response.Message.ToolCalls = response.Message.ToolCalls[:proposalEnd]
	}
	if err = s.host.Emit(ctx, assistant.Event{Kind: "usage", Data: map[string]any{"usage": response.Usage, "verified": response.Usage.Valid()}}); err != nil {
		return nil, err
	}
	return toSchema(response.Message), nil
}
func (m *boundModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	result, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{result}), nil
}

type boundTool struct {
	state      *run
	definition assistant.Tool
}

func (t *boundTool) Info(context.Context) (*schema.ToolInfo, error) {
	raw, err := json.Marshal(t.definition.Function.Parameters)
	if err != nil {
		return nil, err
	}
	var params jsonschema.Schema
	if err = json.Unmarshal(raw, &params); err != nil {
		return nil, err
	}
	return &schema.ToolInfo{Name: t.definition.Function.Name, Desc: t.definition.Function.Description,
		ParamsOneOf: schema.NewParamsOneOfByJSONSchema(&params)}, nil
}
func (t *boundTool) InvokableRun(ctx context.Context, args string, _ ...tool.Option) (string, error) {
	s := t.state
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools++
	if s.tools > assistant.MaxTools || len(args) > assistant.MaxResultBytes {
		return "", assistant.ErrLimit
	}
	ctx, stop := context.WithTimeout(ctx, 3*time.Second)
	defer stop()
	if err := s.host.Check(ctx); err != nil {
		return "", err
	}
	call := assistant.ToolCall{ID: compose.GetToolCallID(ctx), Type: "function"}
	call.Function.Name = t.definition.Function.Name
	call.Function.Arguments = args
	if err := s.host.Emit(ctx, assistant.ToolStarted(call)); err != nil {
		return "", err
	}
	result, err := s.host.Call(ctx, call)
	if err != nil {
		return "", err
	}
	raw, err := json.Marshal(result)
	if err != nil || len(raw) > assistant.MaxResultBytes {
		return "", assistant.ErrLimit
	}
	if err = s.host.Check(ctx); err != nil {
		return "", err
	}
	if err = s.host.Emit(ctx, assistant.Event{Kind: "tool_result", Data: result}); err != nil {
		return "", err
	}
	return string(raw), nil
}
func toSchema(m assistant.Message) *schema.Message {
	out := &schema.Message{Role: schema.RoleType(m.Role), Content: m.Content, ToolCallID: m.ToolCallID}
	for _, c := range m.ToolCalls {
		out.ToolCalls = append(out.ToolCalls, schema.ToolCall{ID: c.ID, Type: c.Type,
			Function: schema.FunctionCall{Name: c.Function.Name, Arguments: c.Function.Arguments}})
	}
	return out
}
func fromSchema(m *schema.Message) assistant.Message {
	out := assistant.Message{Role: string(m.Role), Content: m.Content, ToolCallID: m.ToolCallID}
	for _, c := range m.ToolCalls {
		call := assistant.ToolCall{ID: c.ID, Type: c.Type}
		call.Function.Name = c.Function.Name
		call.Function.Arguments = c.Function.Arguments
		out.ToolCalls = append(out.ToolCalls, call)
	}
	return out
}
