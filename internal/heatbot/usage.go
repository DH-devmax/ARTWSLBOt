package heatbot

import "encoding/json"

// Unknown usage is not zero usage. Control must retain its budget reservation
// until reconciliation; this computation-only package cannot settle a ledger.
type Usage struct {
	Status       string `json:"status"`
	InputTokens  *int64 `json:"input_tokens,omitempty"`
	OutputTokens *int64 `json:"output_tokens,omitempty"`
	TotalTokens  *int64 `json:"total_tokens,omitempty"`
}

type Generation struct {
	Text  string
	Usage Usage
}

func unknownUsage() Usage { return Usage{Status: "unknown"} }

func reportedUsage(raw json.RawMessage) Usage {
	var value struct {
		Input  *int64 `json:"prompt_tokens"`
		Output *int64 `json:"completion_tokens"`
		Total  *int64 `json:"total_tokens"`
	}
	if json.Unmarshal(raw, &value) != nil || value.Input == nil || value.Output == nil || value.Total == nil ||
		*value.Input < 0 || *value.Output < 0 || *value.Total <= 0 || *value.Total > 1_000_000_000 ||
		*value.Input > *value.Total || *value.Output > *value.Total || *value.Input+*value.Output != *value.Total {
		return unknownUsage()
	}
	return Usage{Status: "reported", InputTokens: value.Input, OutputTokens: value.Output, TotalTokens: value.Total}
}

func normalizeUsage(u Usage) Usage {
	if u.Status != "reported" || u.InputTokens == nil || u.OutputTokens == nil || u.TotalTokens == nil {
		return unknownUsage()
	}
	raw, _ := json.Marshal(map[string]*int64{"prompt_tokens": u.InputTokens,
		"completion_tokens": u.OutputTokens, "total_tokens": u.TotalTokens})
	return reportedUsage(raw)
}
