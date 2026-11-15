package gateway

import (
	"testing"

	"wangshangliao/internal/nativeauth"
	"wangshangliao/internal/nativewire"
)

func TestTransportNoticeUsesExactDirectoryScopeOnly(t *testing.T) {
	outer := nativewire.IMMessage{Scene: 1, Sender: "native-peer", Target: "native-team", Fields: map[uint32]string{8: "5"}}
	notice := &nativewire.MessageNotice{Kind: "native_group_notification", Verification: "transport"}
	groups := []nativeauth.DirectoryGroup{{ID: "456", Team: "native-team", Name: "synthetic group"}}
	out, scene, key, ok := bindTransportNotice(outer, 0, notice, groups)
	if !ok || scene != "group" || key != conversationKey("group", 456, "native-team") ||
		out.Mapping != "transport_notice" || out.Message.Sender.ID != 0 || out.Message.Text != "" || out.Message.Format != 7 {
		t.Fatal("notice invented application identity or text")
	}
	for _, target := range []uint64{456, 789} {
		_, _, _, ok := bindTransportNotice(outer, target, notice, groups)
		if ok != (target == 456) {
			t.Fatal("unverified header selected another group")
		}
	}
	if _, _, _, ok := bindTransportNotice(outer, 0, notice, nil); ok {
		t.Fatal("empty directory fallback")
	}
	if _, _, _, ok := bindTransportNotice(outer, 0, notice, append(groups, groups...)); ok {
		t.Fatal("ambiguous mapping")
	}
	other := outer
	other.Target = "another-team"
	if _, _, _, ok := bindTransportNotice(other, 0, notice, groups); ok {
		t.Fatal("cross-group notice")
	}
}
