package control

import (
	"wangshangliao/internal/lajiaoliao"
	"wangshangliao/internal/nativeauth"
)

func ValidateNativeChange(platform string, change nativeauth.SettingChange) error {
	if platform != "lajiaoliao" {
		return change.Validate()
	}
	if nativeauth.SecurityKind(change.Kind) {
		if change.Kind == "secondary_security_code" || change.Kind == "password_verify" {
			return Denied(409, "lajiaoliao_action_unavailable")
		}
		return change.Validate()
	}
	return (lajiaoliao.Change{Kind: change.Kind, Group: change.Group, Target: change.Target,
		Text: change.Text, Minutes: change.Minutes, Enabled: change.Enabled,
		SearchAccount: change.SearchAccount, SearchMobile: change.SearchMobile}).Validate()
}

func ValidateSecurityInput(platform, kind string, in nativeauth.SecurityInput) error {
	if platform != "lajiaoliao" {
		return in.Validate(kind)
	}
	if kind == "avatar" {
		return in.Validate(kind)
	}
	if len(in.Image) != 0 || in.OldPassword != "" {
		return Denied(400, "lajiaoliao_invalid_input")
	}
	return (lajiaoliao.SecurityChange{Flow: in.Flow, Captcha: in.Captcha, Code: in.Code,
		Phone: in.Phone, Password: in.NewPassword}).Validate(kind)
}

func NativeDebugActionForPlatform(platform, action string) bool {
	if platform == "wsl" {
		return NativeDebugAction(action)
	}
	if platform == "lajiaoliao" {
		return NativeModerationActionForPlatform(platform, action) || action == "group_invite" || action == "group_leave" || action == "group_accept" || action == "friend_approve" || action == "friend_delete" ||
			action == "recall_message" || action == "send_text" || NativeUploadAction(action) ||
			action == "send_image" || action == "send_voice" || action == "send_video" ||
			action == "send_private_text" || action == "send_private_image"
	}
	return false
}

func NativeModerationActionForPlatform(platform, action string) bool {
	return nativeauth.ModerationKind(action) || platform == "lajiaoliao" && (action == "member_nickname" || lajiaoliao.BanAction(action))
}

func nativePlatform(workspace string) string {
	if workspace == "lajiaoliao" {
		return workspace
	}
	return "wsl"
}
