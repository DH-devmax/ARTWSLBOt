package control

import "testing"

func TestAssignmentSortIsStableAndPlacesUnknownLatencyLast(t *testing.T) {
	fast, slow := 1.0, 9.0
	items := []AssignmentCandidate{
		{ID: "z", Eligible: true, Bound: 2, DurableP95: nil},
		{ID: "b", Eligible: true, Bound: 2, DurableP95: &slow},
		{ID: "a", Eligible: true, Bound: 2, DurableP95: &fast},
		{ID: "low", Eligible: true, Bound: 1},
		{ID: "blocked", Eligible: false, Bound: 0},
	}
	SortAssignmentCandidates(items)
	for i, want := range []string{"low", "a", "b", "z", "blocked"} {
		if items[i].ID != want {
			t.Fatal("unstable recommendation", items)
		}
	}
}
