package assistantadapter

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"

	"wangshangliao/internal/assistant"
)

type provider func(context.Context, []assistant.Message, []assistant.Tool, func(string) error) (assistant.Response, error)

func (p provider) Generate(c context.Context, m []assistant.Message, t []assistant.Tool, e func(string) error) (assistant.Response, error) {
	return p(c, m, t, e)
}
func TestEinoSequentialToolBoundAndConcurrentScopes(t *testing.T) {
	var active, maximum atomic.Int32
	var wg sync.WaitGroup
	for _, scope := range []string{"tenant-a/group-a", "tenant-b/group-b"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			h := &host{scope: scope}
			p := provider(func(ctx context.Context, m []assistant.Message, _ []assistant.Tool, _ func(string) error) (assistant.Response, error) {
				if m[len(m)-1].Role == "tool" {
					t.Error("seven-tool proposal should hit the tool limit before another model call")
				}
				out := assistant.Response{Message: assistant.Message{Role: "assistant"}}
				for i := 0; i < 7; i++ {
					c := assistant.ToolCall{ID: string(rune('a' + i)), Type: "function"}
					c.Function.Name = "list_authorized_groups"
					c.Function.Arguments = `{}`
					out.Message.ToolCalls = append(out.Message.ToolCalls, c)
				}
				return out, nil
			})
			wrapped := &scopedHost{host: h, active: &active, maximum: &maximum}
			if err := (Runtime{}).Run(context.Background(), p, wrapped, []assistant.Message{{Role: "user", Content: scope}}, false); err == nil || h.calls != 6 {
				t.Error("tool limit", scope, h.calls, err)
			}
		}()
	}
	wg.Wait()
	if maximum.Load() > 2 {
		t.Fatal("concurrent tools in a single run", maximum.Load())
	}
}

type scopedHost struct {
	*host
	active, maximum *atomic.Int32
}

func (h *scopedHost) Call(ctx context.Context, c assistant.ToolCall) (assistant.ToolResult, error) {
	active := h.active.Add(1)
	defer h.active.Add(-1)
	for prior := h.maximum.Load(); active > prior && !h.maximum.CompareAndSwap(prior, active); prior = h.maximum.Load() {
	}
	return h.host.Call(ctx, c)
}

type host struct {
	mu             sync.Mutex
	calls, settles int
	scope          string
}

func (h *host) Check(ctx context.Context) error                             { return ctx.Err() }
func (h *host) Emit(ctx context.Context, e assistant.Event) error           { return ctx.Err() }
func (h *host) Reserve(context.Context, int64) (string, error)              { return "synthetic", nil }
func (h *host) Settle(context.Context, string, assistant.Usage, bool) error { h.settles++; return nil }
func (h *host) Call(ctx context.Context, c assistant.ToolCall) (assistant.ToolResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.calls++
	return assistant.ToolResult{Data: h.scope}, ctx.Err()
}
func TestEinoLimitsWhitelistAndScope(t *testing.T) {
	for _, name := range []string{"get_group_config", "shell"} {
		h := &host{scope: "group-a"}
		count := 0
		p := provider(func(ctx context.Context, m []assistant.Message, _ []assistant.Tool, emit func(string) error) (assistant.Response, error) {
			count++
			out := assistant.Response{Message: assistant.Message{Role: "assistant"}}
			if count == 1 {
				c := assistant.ToolCall{ID: "one", Type: "function"}
				c.Function.Name = name
				c.Function.Arguments = `{}`
				out.Message.ToolCalls = []assistant.ToolCall{c}
			} else {
				out.Message.Content = "done"
			}
			return out, nil
		})
		err := (Runtime{}).Run(context.Background(), p, h, []assistant.Message{{Role: "user", Content: "test"}}, false)
		if name == "shell" {
			if err == nil || h.calls != 0 {
				t.Fatal("tool escaped whitelist", err, h.calls)
			}
		} else if err != nil || h.calls != 1 || count != 2 {
			t.Fatal(err, h.calls, count)
		}
	}
	h := &host{}
	count := 0
	p := provider(func(context.Context, []assistant.Message, []assistant.Tool, func(string) error) (assistant.Response, error) {
		count++
		c := assistant.ToolCall{ID: string(rune('a' + count)), Type: "function"}
		c.Function.Name = "list_authorized_bots"
		c.Function.Arguments = `{}`
		return assistant.Response{Message: assistant.Message{Role: "assistant", ToolCalls: []assistant.ToolCall{c}}}, nil
	})
	if err := (Runtime{}).Run(context.Background(), p, h, []assistant.Message{{Role: "user", Content: "test"}}, false); err == nil || count > 3 {
		t.Fatal("model bound", count, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := (Runtime{}).Run(ctx, p, h, nil, false); err == nil {
		t.Fatal("cancel ignored")
	}
}

type proposalHost struct {
	calls, reserves int
	settlements     []assistant.Usage
	results         []string
	revoked         bool
	onProposal      func()
}

func (*proposalHost) AssistantTools() []assistant.Tool {
	return append(assistant.Tools(true), assistant.ActionTools([]string{"send_text"}, false)...)
}
func (h *proposalHost) Check(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if h.revoked {
		return errors.New("revoked")
	}
	return nil
}
func (h *proposalHost) Emit(ctx context.Context, e assistant.Event) error {
	if err := h.Check(ctx); err != nil {
		return err
	}
	if e.Kind == "tool_result" {
		result := e.Data.(assistant.ToolResult)
		h.results = append(h.results, result.Type)
		if result.Verification == "suggestion_only" && h.onProposal != nil {
			h.onProposal()
		}
	}
	return nil
}
func (h *proposalHost) Reserve(context.Context, int64) (string, error) {
	h.reserves++
	return fmt.Sprintf("reservation-%d", h.reserves), nil
}
func (h *proposalHost) Settle(ctx context.Context, id string, usage assistant.Usage, failed bool) error {
	if ctx.Err() != nil || failed || id != fmt.Sprintf("reservation-%d", len(h.settlements)+1) {
		return errors.New("unexpected settlement")
	}
	h.settlements = append(h.settlements, usage)
	return nil
}
func (h *proposalHost) Call(ctx context.Context, call assistant.ToolCall) (assistant.ToolResult, error) {
	h.calls++
	verification := "verified"
	if call.Function.Name == "propose_group_config_change" || call.Function.Name == "propose_group_action" {
		verification = "suggestion_only"
	}
	return assistant.ToolResult{Type: call.Function.Name, Verification: verification,
		Data: map[string]string{"id": "synthetic-plan", "status": "pending_approval"}}, ctx.Err()
}

func TestEinoWaitsForHumanAfterProposal(t *testing.T) {
	usage := assistant.Usage{PromptTokens: 12, CompletionTokens: 3, TotalTokens: 15}
	for _, action := range []string{"propose_group_config_change", "propose_group_action"} {
		for _, proposalRound := range []int{1, assistant.MaxModels} {
			t.Run(fmt.Sprintf("%s/round_%d", action, proposalRound), func(t *testing.T) {
				h := &proposalHost{}
				rounds := 0
				p := provider(func(context.Context, []assistant.Message, []assistant.Tool, func(string) error) (assistant.Response, error) {
					rounds++
					if rounds > proposalRound {
						t.Error("model called while waiting for human approval")
						return assistant.Response{}, assistant.ErrLimit
					}
					name := "get_group_config"
					if rounds == proposalRound {
						name = action
					}
					call := assistant.ToolCall{ID: fmt.Sprintf("synthetic-%d", rounds), Type: "function"}
					call.Function.Name, call.Function.Arguments = name, "{}"
					calls := []assistant.ToolCall{call}
					if name == action {
						extra := call
						extra.ID, extra.Function.Name = "must-not-run-after-proposal", action
						calls = append(calls, extra)
					}
					return assistant.Response{Message: assistant.Message{Role: "assistant", ToolCalls: calls}, Usage: usage}, nil
				})
				err := (Runtime{}).Run(context.Background(), p, h, []assistant.Message{{Role: "user", Content: "synthetic"}}, true)
				if err != nil {
					t.Fatal(err)
				}
				if rounds != proposalRound || h.calls != proposalRound || h.reserves != proposalRound ||
					len(h.settlements) != proposalRound || len(h.results) != proposalRound || h.results[len(h.results)-1] != action {
					t.Fatalf("unexpected work after proposal: rounds=%d host=%+v", rounds, h)
				}
				for _, settled := range h.settlements {
					if settled != usage {
						t.Fatalf("usage changed: %+v", settled)
					}
				}
			})
		}
	}
}

func TestEinoProposalStillChecksCancellationAndRevocation(t *testing.T) {
	for _, reason := range []string{"cancel", "revoke"} {
		t.Run(reason, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			h := &proposalHost{}
			h.onProposal = func() {
				if reason == "cancel" {
					cancel()
				} else {
					h.revoked = true
				}
			}
			p := provider(func(context.Context, []assistant.Message, []assistant.Tool, func(string) error) (assistant.Response, error) {
				call := assistant.ToolCall{ID: "proposal", Type: "function"}
				call.Function.Name, call.Function.Arguments = "propose_group_config_change", "{}"
				return assistant.Response{Message: assistant.Message{Role: "assistant", ToolCalls: []assistant.ToolCall{call}},
					Usage: assistant.Usage{PromptTokens: 2, CompletionTokens: 1, TotalTokens: 3}}, nil
			})
			if err := (Runtime{}).Run(ctx, p, h, nil, true); err == nil {
				t.Fatal("proposal bypassed final authorization check")
			}
			if h.calls != 1 || h.reserves != 1 || len(h.settlements) != 1 {
				t.Fatalf("unexpected replay or unsettled usage: %+v", h)
			}
		})
	}
}
