package nativeauth

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestMessageReferencePlatformScopeAndExpiry(t *testing.T) {
	now := time.Now()
	ref := MessageReference{Platform: "lajiaoliao", EventID: 1, Group: "string-group", Sender: "string-sender",
		SourceDigest: strings.Repeat("a", 64), CreatedAt: now.UnixMilli(), Conversation: "sg_string-group", Sequence: 71}
	raw, _ := json.Marshal(ref)
	if !ValidMessageReference(string(raw), "lajiaoliao", ref.Group) || !ref.Fresh(now) {
		t.Fatal("valid scoped reference lost")
	}
	for _, bad := range []string{
		strings.Replace(string(raw), "sg_string-group", "sg_other-group", 1),
		strings.Replace(string(raw), `"sequence":71`, `"sequence":0`, 1),
		strings.TrimSuffix(string(raw), "}") + `,"url":"https://invalid.test"}`,
		string(raw) + `{}`,
	} {
		if ValidMessageReference(bad, "lajiaoliao", ref.Group) {
			t.Fatal("forged reference accepted")
		}
	}
	if ValidMessageReference(string(raw), "wsl", ref.Group) || ValidMessageReference(string(raw), "lajiaoliao", "other") ||
		ref.Fresh(now.Add(2*time.Minute)) || ref.Fresh(now.Add(-time.Second)) {
		t.Fatal("platform, group or time fence widened")
	}
}
