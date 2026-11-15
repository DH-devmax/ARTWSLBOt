package gateway

import (
	"fmt"
	"testing"

	"github.com/openimsdk/protocol/sdkws"
)

func TestChiliRecoveryRotatesThreeHundredConversations(t *testing.T) {
	heads := &sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{}}
	cursor := map[string]int64{}
	for i := range 300 {
		heads.MaxSeqs[fmt.Sprintf("sg_%03d", i)] = 10000
	}
	seen, after := map[string]bool{}, ""
	for range 300 {
		key, previous, end, err := nextChiliRange(heads, cursor, after)
		if err != nil || seen[key] || previous != 0 || end != 50 {
			t.Fatalf("unfair or unbounded range: %q %d %d %v", key, previous, end, err)
		}
		seen[key], cursor[key], after = true, end, key
	}
	key, previous, end, err := nextChiliRange(heads, cursor, after)
	if err != nil || key != "sg_000" || previous != 50 || end != 100 || len(seen) != 300 {
		t.Fatal("recovery did not wrap fairly", key, previous, end, err)
	}
}

func TestChiliRecoveryValidatesUnselectedGapAndIdle(t *testing.T) {
	heads := &sdkws.GetMaxSeqResp{MaxSeqs: map[string]int64{"sg_a": 1, "sg_z": 9}, MinSeqs: map[string]int64{"sg_z": 8}}
	if _, _, _, err := nextChiliRange(heads, nil, ""); err == nil {
		t.Fatal("a gap in another conversation was hidden by the selected range")
	}
	heads.MinSeqs = nil
	if _, _, _, err := nextChiliRange(heads, map[string]int64{"sg_z": 10}, ""); err == nil {
		t.Fatal("sequence regression accepted")
	}
	key, _, _, err := nextChiliRange(heads, map[string]int64{"sg_a": 1, "sg_z": 9}, "")
	if err != nil || key != "" {
		t.Fatal("idle conversation caused a pull", key, err)
	}
}
