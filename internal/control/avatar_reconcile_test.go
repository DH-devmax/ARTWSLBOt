package control

import (
	"testing"
	"time"

	"wangshangliao/internal/nativeauth"
)

func TestAvatarRecoveryRequiresFinishedUploadPhaseEvidence(t *testing.T) {
	key, account := NewID(), NewID()
	req := SettingRequest{Account: account, Workspace: "lajiaoliao", Key: key,
		InputDigest: "synthetic-fingerprint", Change: nativeauth.SettingChange{Kind: "avatar", Text: key}}
	if !avatarUploadUnsubmitted(req, account, key, "lajiaoliao_result_unknown") {
		t.Fatal("known pre-update upload outcome cannot be resolved")
	}
	for _, change := range []func(*SettingRequest){
		func(r *SettingRequest) { r.Change.Text = "https://lj-im-chat-01.lajiaoliao.com/synthetic.jpg" },
		func(r *SettingRequest) { r.Workspace = "wsl" },
		func(r *SettingRequest) { r.Account = NewID() },
		func(r *SettingRequest) { r.InputDigest = "" },
		func(r *SettingRequest) { r.Change.Kind = "nickname" },
	} {
		altered := req
		change(&altered)
		if avatarUploadUnsubmitted(altered, account, key, "lajiaoliao_result_unknown") {
			t.Fatal("missing or final-update evidence accepted")
		}
	}
	if avatarUploadUnsubmitted(req, account, key, "process_restarted") {
		t.Fatal("interrupted process cannot prove final update was not called")
	}
	if settingFenceDuration(req) != 8*time.Second ||
		settingFenceDuration(SettingRequest{Workspace: "wsl", Change: req.Change}) != 2*time.Second ||
		settingFenceDuration(SettingRequest{Workspace: "lajiaoliao", Change: nativeauth.SettingChange{Kind: "nickname"}}) != 2*time.Second {
		t.Fatal("avatar upload timeout changed unrelated native operations")
	}
}
