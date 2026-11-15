package heatbot

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

func TestUsageNeverTreatsMissingOrMalformedAsZero(t *testing.T) {
	for _, raw := range []string{"", "null", "{}", `{"prompt_tokens":10,"completion_tokens":3}`,
		`{"prompt_tokens":-1,"completion_tokens":4,"total_tokens":3}`,
		`{"prompt_tokens":10,"completion_tokens":3,"total_tokens":12}`,
		`{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}`,
		`{"prompt_tokens":1.5,"completion_tokens":3,"total_tokens":4.5}`,
		`{"prompt_tokens":9223372036854775807,"completion_tokens":1,"total_tokens":1}`} {
		got := reportedUsage(json.RawMessage(raw))
		if got.Status != "unknown" || got.InputTokens != nil || got.OutputTokens != nil || got.TotalTokens != nil {
			t.Fatalf("invalid usage treated as known: %s: %#v", raw, got)
		}
	}
	got := reportedUsage(json.RawMessage(`{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}`))
	if got.Status != "reported" || *got.InputTokens != 10 || *got.OutputTokens != 3 || *got.TotalTokens != 13 {
		t.Fatalf("%#v", got)
	}
}

type detailedFixture struct{ err error }

func (p detailedFixture) Generate(context.Context, Request) (string, error) {
	panic("detailed provider bypassed")
}
func (p detailedFixture) GenerateDetailed(context.Context, Request) (Generation, error) {
	return Generation{Text: "reply", Usage: reportedUsage(json.RawMessage(`{"prompt_tokens":10,"completion_tokens":3,"total_tokens":13}`))}, p.err
}

func TestEnginePreservesUsageOnSuccessAndFailure(t *testing.T) {
	for _, failure := range []error{nil, errors.New("provider_response_incomplete")} {
		engine := New(detailedFixture{failure})
		ch, err := engine.Submit(context.Background(), fixture("group"))
		if err != nil {
			t.Fatal(err)
		}
		got := result(t, ch)
		engine.Close()
		if got.Usage.Status != "reported" || *got.Usage.TotalTokens != 13 || !errors.Is(got.Err, failure) {
			t.Fatalf("%#v", got)
		}
		if failure == nil && got.Proposal.Usage.Status != "reported" {
			t.Fatal("proposal omitted usage")
		}
		if failure != nil && got.Proposal.Text != "" {
			t.Fatal("failed generation exposed text")
		}
	}
}
