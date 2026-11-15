package platform

import "testing"

func TestAccountScopes(t *testing.T) {
	for _, p := range []ID{WSL, LajiaoLiao} {
		for _, purpose := range []Purpose{Management, Chat} {
			workspace, err := Workspace(p, purpose)
			if err != nil || workspace == "" {
				t.Fatalf("%s/%s: %v", p, purpose, err)
			}
			if purpose == Chat && workspace != "maibot" {
				t.Fatal("chat accounts must share one pool")
			}
		}
	}
	if _, err := Workspace("", Management); err == nil {
		t.Fatal("empty platform accepted")
	}
	a, b := Identity{WSL, "42"}, Identity{LajiaoLiao, "42"}
	if !a.Valid() || !b.Valid() || a == b {
		t.Fatal("native IDs must be qualified by platform")
	}
	if GroupsPerPool != 300 || AccountsPerPool != 10 || GroupsPerAccount != 30 {
		t.Fatal("unexpected admission policy")
	}
}
