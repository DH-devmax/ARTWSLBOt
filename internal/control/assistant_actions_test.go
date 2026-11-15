package control

import (
	"context"
	"testing"
)

func TestAssistantMessagesCannotUseManualSettingsAdmission(t *testing.T) {
	service := &NativeSettingsService{}
	for _, action := range []string{"send_text", "recall_message"} {
		req := SettingRequest{}
		req.Change.Kind = action
		if _, err := service.Execute(context.Background(), SessionRef{}, req, nil); err == nil ||
			err.Error() != "assistant_business_admission_required" {
			t.Fatal("message entered manual settings admission", action, err)
		}
	}
}

func TestAssistantActionFieldsStayBounded(t *testing.T) {
	for _, c := range []struct {
		action, text string
		minutes      int
		target, ok   bool
	}{
		{"mute_member", "", 1, true, true},
		{"mute_member", "", 1441, true, false},
		{"mute_member", "", 1, false, false},
		{"unmute_member", "", 0, true, true},
		{"unmute_member", "hidden", 0, true, false},
		{"publish_notice", "第一行\n第二行", 0, false, true},
		{"publish_notice", "\x00hidden", 0, false, false},
		{"publish_notice", string([]byte{0xff}), 0, false, false},
		{"send_text", "群消息", 0, false, true},
		{"send_text", "群消息", 0, true, false},
		{"group_info", "测试群", 0, false, true},
		{"group_info", "含 空格", 0, false, false},
		{"group_info", "测试群", 0, true, false},
		{"recall_message", "", 0, true, true},
		{"recall_message", "fake-route", 0, true, false},
		{"recall_message", "", 0, false, false},
		{"member_nickname", "群名片", 0, true, true},
		{"member_nickname", "群名片", 1, true, false},
		{"mute_group", "", 0, false, true},
		{"mute_group", "", 0, true, false},
		{"grant_admin", "", 0, true, false},
		{"group_leave", "", 0, false, false},
		{"arbitrary_http", "https://synthetic.invalid", 0, false, false},
	} {
		if err := validateAssistantAction(c.action, c.text, c.minutes, c.target); (err == nil) != c.ok {
			t.Fatalf("%s accepted=%v: %v", c.action, c.ok, err)
		}
	}
}
