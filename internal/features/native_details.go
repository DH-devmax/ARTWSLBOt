package features

// NativeDetail refines a compatibility group without changing its ID, grants or
// admission gate. Historical group evidence is never inherited by a child.
type NativeDetail struct {
	NativeCapability
	ParentID   string                `json:"parent_id"`
	Acceptance *ControlledAcceptance `json:"acceptance,omitempty"`
}

func NativeDetailsForPlatform(platform string) []NativeDetail {
	out := []NativeDetail{}
	verifiedDetails := nativeDetailAcceptanceForPlatform(platform)
	add := func(parent, id, name, support, integration, role, summary, evidence, verified string) {
		detail := NativeDetail{
			NativeCapability: NativeCapability{ID: id, Name: name, Kind: "action", Support: support,
				Integration: integration, Role: role, Summary: summary, Evidence: evidence},
			ParentID: parent,
		}
		for _, group := range NativeCatalogForPlatform(platform) {
			if group.ID == parent {
				detail.Category = group.Category
				detail.Kind = group.Kind
				break
			}
		}
		if check, ok := verifiedDetails[id]; ok {
			detail.Acceptance = &check
		} else if verified != "" {
			check := NativeAcceptanceForPlatform(platform)[verified]
			detail.Acceptance = &check
		}
		out = append(out, detail)
	}
	switch platform {
	case "wsl":
		const settings = "internal/control/native_settings.go；internal/control/native_security.go；docs/native-settings.md"
		for _, item := range []struct{ id, name, summary string }{
			{"account_nickname", "修改原生昵称", "本人昵称单项修改、恢复及独立回读已受控核验；异常恢复仍待验收。"},
			{"account_avatar", "修改原生头像", "当前本人头像同图规范化重传、检查、设置及新资源读取已受控核验；任意新图及恢复原资源编号未验收。"},
			{"privacy_search", "账号 / 手机号搜索设置", "已接入明确的搜索开关；缺失值不猜测，真实修改未验收。"},
			{"privacy_friend", "好友申请隐私策略", "已接入需要审批 / 允许任何人；问题策略只读，真实修改未验收。"},
			{"privacy_invitation", "群邀请隐私策略", "已接入拒绝 / 允许 / 询问；真实修改未验收。"},
			{"privacy_calls", "语音 / 视频通话设置", "本人通话开关修改和恢复均独立回读通过；不代表真实呼叫或媒体收发通过。"},
			{"change_password", "修改账号密码", "已接入短信验证及旧密码校验；真实改密、失效与重新认证未验收。"},
			{"change_phone", "换绑手机号", "已接入旧手机验证及新手机短信；真实换绑及恢复未验收。"},
		} {
			add("account_settings", item.id, item.name, "confirmed", "partial", "超管本人显式确认", item.summary, settings, "")
		}
		add("account_settings", "secondary_security_code", "设置二级安全码", "confirmed", "deferred", "本轮不开放",
			"所有者决定暂缓接入；关闭设置与状态读取入口，保留原ID和候选代码，不计入当前验收。",
			"docs/platform-native-scope.md；2026-11-01所有者范围调整", "")
		add("account_settings", "legal_identity", "实名认证", "confirmed", "deferred", "本轮不开放",
			"所有者决定暂缓接入；关闭审核状态与认证入口，不计入当前验收。",
			"docs/platform-native-scope.md；2026-11-01所有者范围调整", "")
		for _, item := range []struct{ id, name, summary string }{
			{"send_voice", "发送群语音", "已接入有界MP3及16位PCM WAV检查；MP3上传、本站发送及另一账号解码已核验，WAV上游兼容性未通过。"},
			{"send_video", "发送群视频", "有封面非分片MP4的上传、本站持久发送及另一账号实际解码已独立核验。"},
			{"send_file", "发送群文件", "已接入原生类型6及有界上传、持久发送；nonce历史与真实收发未验收。"},
		} {
			add("send_media", item.id, item.name, "confirmed", "partial", "超管受控窗口；在群且可发言",
				item.summary+"生产入口关闭，私信不在此范围。", "internal/control/native_media.go；internal/nativewire/send_media.go；docs/native-media.md", "")
		}
		add("invite_group", "invite_group_request", "发送入群邀请", "confirmed", "partial", "群主 / 管理员及受控窗口",
			"持久邀请已接入；请求确认不等于已入群，须由受邀者接受并核对完整名单。", "internal/control/native_social.go；docs/native-social.md", "invite_group")
		add("invite_group", "accept_group_invitation", "本人接受入群邀请", "confirmed", "partial", "受邀账号本人及独立group_accept窗口",
			"持久接受、本人待处理邀请及双侧成员核对已接入；不自动接受，不沿用邀请发送权限。", "internal/control/native_social.go；docs/native-social.md", "invite_group")
		add("set_member_card", "set_self_card", "修改本人群昵称", "confirmed", "partial", "本人；群昵称规则允许",
			"本人修改和恢复已受控核验；其他角色和故障恢复仍待验收。", settings, "")
		add("set_member_card", "set_other_card", "修改他人群昵称", "confirmed", "partial", "有目标层级权限的群主 / 管理员",
			"群主对自有普通成员名片修改及恢复已独立回读核验；其他层级、昵称锁与故障恢复未验收。", settings, "")
		for _, item := range []struct{ id, name string }{
			{"enable_group_mute", "开启全群禁言"}, {"disable_group_mute", "解除全群禁言"},
		} {
			add("mute_group", item.id, item.name, "confirmed", "partial", "群主 / 管理员及受控窗口",
				"已接入持久操作及独立回读；角色变化和故障恢复未完整验收。", "internal/control/test_tools.go；docs/controlled-tools.md", "mute_group")
		}
		for _, item := range []struct{ id, name string }{
			{"accept_friend", "人工同意好友申请"}, {"reject_friend", "人工拒绝好友申请"},
		} {
			add("approve_friend", item.id, item.name, "confirmed", "partial", "超管本人显式审批",
				"受控同意固定申请编号、申请人、账号、隔离群和一次预算；接入向导另按服务端时限默认拒绝唯一旧申请，要求重新申请。真实效果独立验收，不供助手或自动业务调用。", "internal/control/native_social.go；internal/control/group_binding_contact_submit.go；docs/native-social.md", "")
		}
	case "lajiaoliao":
		add("kick_member", "kick_member_once", "普通移出成员", "confirmed", "partial",
			"当前原生群主 / 管理员；明确成员及kick_member授权",
			"显式permanentBan=false；本站单次移出、完整成员名单及空禁入名单已独立核验，不借普通移出授权执行永久禁入。",
			"internal/lajiaoliao/native.go；docs/lajiaoliao-membership-20261104.md", "")
		add("kick_member", "kick_member_permanent", "移出并永久禁止申请进群", "confirmed", "partial",
			"当前原生群主 / 管理员；明确成员及独立kick_member_ban授权",
			"显式permanentBan=true；本站对单个自有普通成员写入成功，独立回读同时确认离群及精确禁入，随后另行解除。生产与故障恢复未验收。",
			"固定桌面1.0.3随包SDK；internal/lajiaoliao/group_bans.go；docs/lajiaoliao-membership-20261104.md", "")
		for _, item := range []struct{ parent, id, name string }{
			{"account_settings", "account_nickname", "修改原生昵称"},
			{"account_settings", "account_avatar", "修改原生头像"},
			{"account_settings", "profile_gender", "设置本人性别"},
			{"account_settings", "profile_birth", "设置本人生日"},
			{"account_settings", "profile_signature", "设置本人签名"},
			{"account_settings", "push_notifications", "原生消息通知设置"},
			{"account_settings", "privacy_search", "账号 / 手机号搜索设置"},
			{"account_settings", "privacy_friend", "好友申请隐私策略"},
			{"account_settings", "privacy_invitation", "群邀请隐私策略"},
			{"account_settings", "privacy_calls", "语音 / 视频通话设置"},
			{"account_settings", "change_password", "修改账号密码"},
			{"account_settings", "change_phone", "换绑手机号"},
			{"send_media", "send_voice", "发送群语音"},
			{"send_media", "send_video", "发送群视频"},
			{"send_media", "send_file", "发送群文件"},
			{"invite_group", "invite_group_request", "发送入群邀请"},
			{"invite_group", "accept_group_invitation", "本人接受入群邀请"},
			{"set_member_card", "set_self_card", "修改本人群昵称"},
			{"set_member_card", "set_other_card", "修改他人群昵称"},
			{"mute_group", "enable_group_mute", "开启全群禁言"},
			{"mute_group", "disable_group_mute", "解除全群禁言"},
			{"approve_friend", "accept_friend", "人工同意好友申请"},
			{"approve_friend", "reject_friend", "人工拒绝好友申请"},
		} {
			support, integration, role := "review", "pending", "本平台契约及角色限制待核验"
			summary, evidence := "保留原子项ID供独立核实；需取得辣椒聊格式、接口和权限证据后适配。",
				"docs/platform-native-scope.md；本平台契约尚未核验"
			switch item.id {
			case "account_avatar":
				support, integration, role = "confirmed", "partial", "超管本人显式确认"
				summary = "固定桌面头像上传和/user/update faceURL已接通；本人头像同图规范化重传及最终设置成功，新256×256资源实际回显，昵称未变。"
				evidence = "internal/lajiaoliao/avatar.go；internal/gateway/settings.go；internal/httpapi/account_avatar.go；docs/native-settings.md"
			case "send_voice":
				support, integration, role = "confirmed", "partial", "超管受控窗口；当前群主 / 管理员且可发言"
				summary = "本站MP3上传、持久103发送及另一自有账号接收已独立核对；WAV、其他编码和移动端实际播放未验收，生产入口关闭。"
				evidence = "internal/lajiaoliao/media.go；internal/control/lajiaoliao_media.go；docs/lajiaoliao-outbound-20261104.md"
			case "send_video":
				support, integration, role = "confirmed", "partial", "超管受控窗口；当前群主 / 管理员且可发言"
				summary = "本站非分片MP4与JPEG封面上传、持久104发送及对端接收独立通过，视频和封面经本站作用域读取；其他编码和生产准入未验收。"
				evidence = "internal/lajiaoliao/media.go；internal/control/lajiaoliao_media.go；docs/lajiaoliao-outbound-20261104.md"
			case "account_nickname", "privacy_search", "privacy_friend", "privacy_invitation", "privacy_calls",
				"profile_gender", "profile_birth", "profile_signature", "push_notifications":
				support, integration, role = "confirmed", "partial", "超管本人显式确认"
				summary = "已接本平台固定接口、独立取值和单项持久控制；真实修改及恢复未验收。"
				evidence = "internal/lajiaoliao/profile.go；docs/lajiaoliao-native.md"
			case "change_password", "change_phone":
				support, integration, role = "confirmed", "partial", "超管本人逐步显式确认"
				summary = "按桌面1.0.3独立短信purpose及路由接入；当前绑定手机由服务端读取，结果未知不重发，真实改密/换绑未验收。"
				evidence = "internal/lajiaoliao/security.go；internal/control/native_security.go；docs/lajiaoliao-native.md"
			case "invite_group_request":
				support, integration, role = "confirmed", "partial", "原生群主 / 管理员、当前好友及受控窗口"
				summary = "本站单次邀请持久成功，所有者在App接受后双方完整名单确认入群；本人接受邀请独立接入和验收，不继承邀请结果。"
				evidence = "internal/lajiaoliao/native.go；docs/lajiaoliao-membership-acceptance-20261104.md"
			case "accept_group_invitation":
				support, integration, role = "confirmed", "partial", "受邀账号本人及独立group_accept窗口"
				summary = "本站受邀本人读取唯一邀请并单次持久确认，双方完整名单和本人目录均核验恢复普通成员；独立预算已耗尽且关闭，其他策略及生产准入未验收。"
				evidence = "internal/lajiaoliao/invitations.go；internal/gateway/lajiaoliao_native.go；docs/lajiaoliao-self-invitation-20261104.md"
			case "accept_friend", "reject_friend":
				support, integration, role = "confirmed", "partial", "当前申请、独立准入及控制栅栏"
				summary = "原生审批已接入持久操作；接入向导核对时可默认拒绝唯一超时旧申请，要求重申。真实效果独立验收，不继承旺商聊结论。"
				evidence = "internal/lajiaoliao/onboarding_contact.go；internal/control/group_binding_contact_submit.go；docs/native-social.md"
			case "set_self_card", "set_other_card", "enable_group_mute", "disable_group_mute":
				support, integration, role = "confirmed", "partial", "原生角色、当前目标及控制栅栏"
				summary = "已按辣椒聊接口接入持久操作；原子项真实验收独立记录，不继承旺商聊结论。"
				evidence = "internal/lajiaoliao/native.go；docs/lajiaoliao-native.md"
			}
			add(item.parent, item.id, item.name, support, integration, role, summary, evidence, "")
		}
		for _, item := range []struct{ id, name string }{
			{"secondary_security_code", "设置二级安全码"}, {"legal_identity", "实名认证"},
		} {
			add("account_settings", item.id, item.name, "review", "deferred", "本轮不开放",
				"所有者决定暂缓接入；本平台支持尚未独立核实，不计入当前验收。",
				"docs/platform-native-scope.md；2026-11-01所有者范围调整", "")
		}
	default:
		return out
	}
	integration, evidence := "implemented", "internal/nativeauth/client.go；internal/httpapi/native_auth.go；docs/native-auth.md"
	if platform == "lajiaoliao" {
		integration, evidence = "partial", "internal/lajiaoliao/signup.go；internal/httpapi/lajiaoliao_signup.go"
		add("account_signup", "signup_sms", "手动申请注册短信", "confirmed", integration, "超管本人手动发起",
			"使用辣椒聊桌面purpose=3短信及人工验证；真实收码未验收，失败不自动重发。", evidence, "")
		add("account_signup", "signup_submit", "提交注册资料", "confirmed", integration, "超管本人手动提交",
			"使用辣椒聊login_or_signup及手机号、验证码、设备信息；不套用旺商聊昵称和密码字段，真实开户未验收。", evidence, "")
		return out
	}
	add("account_signup", "signup_sms", "手动申请注册短信", "confirmed", integration, "超管本人手动发起",
		"已有有界诊断及人工短信流程；真实收码未验收，不自动重试。业务1001不能据此判定号段受限。", evidence, "")
	add("account_signup", "signup_submit", "提交注册资料", "confirmed", integration, "超管本人手动提交",
		"已有持久未知结果屏障；真实注册未验收。各平台按已核对客户端契约，不混用Android和桌面字段。", evidence, "")
	return out
}
