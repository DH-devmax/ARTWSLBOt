package control

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestFeaturePreviewBoundedSingleFeatureInput(t *testing.T) {
	base := FeaturePreviewInput{Version: 1, Feature: "keyword_replies", Text: "hello", Role: "member"}
	for _, feature := range []string{"blocked_words", "blacklist", "exemptions", "keyword_replies"} {
		in := base
		in.Feature = feature
		if err := ValidateFeaturePreview(in); err != nil {
			t.Fatal("saved configuration preview rejected", feature, err)
		}
		in.Changes = FeatureChanges{feature: {"enabled": json.RawMessage("false")}}
		if err := ValidateFeaturePreview(in); err != nil {
			t.Fatal("single feature draft rejected", feature, err)
		}
	}
	for _, mutate := range []func(*FeaturePreviewInput){
		func(in *FeaturePreviewInput) { in.Version = 0 },
		func(in *FeaturePreviewInput) { in.Feature = "scheduled_messages" },
		func(in *FeaturePreviewInput) { in.Text = "" },
		func(in *FeaturePreviewInput) { in.Text = strings.Repeat("a", 16385) },
		func(in *FeaturePreviewInput) { in.Text = string([]byte{0xff}) },
		func(in *FeaturePreviewInput) { in.Sender = strings.Repeat("a", 201) },
		func(in *FeaturePreviewInput) { in.Sender = string([]byte{0xff}) },
		func(in *FeaturePreviewInput) { in.Role = "superadmin" },
		func(in *FeaturePreviewInput) { in.Role = "" },
		func(in *FeaturePreviewInput) {
			in.Changes = FeatureChanges{"blocked_words": {"enabled": json.RawMessage("true")}}
		},
		func(in *FeaturePreviewInput) {
			in.Changes = FeatureChanges{"keyword_replies": {"enabled": json.RawMessage("true")},
				"blacklist": {"enabled": json.RawMessage("true")}}
		},
	} {
		in := base
		mutate(&in)
		if err := ValidateFeaturePreview(in); err == nil {
			t.Fatal("unbounded or cross-feature preview was accepted")
		}
	}
}
