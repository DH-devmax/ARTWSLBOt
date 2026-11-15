package features

// Platform catalogs distinguish reviewed contracts from explicitly unreviewed
// product gaps. Neither partial nor pending satisfies NativeBlockers.
func NativeCatalogForPlatform(platform string) []NativeCapability {
	if platform == "wsl" {
		return NativeCatalog()
	}
	if platform != "lajiaoliao" {
		return []NativeCapability{}
	}
	const evidence = "AstrBot e62e2ce12e7d8591afe995c56478205224294afd；本项目真实原生验收未完成"
	out := []NativeCapability{
		{"account_signup", "短信注册 / 登录", "账号", "session", "confirmed", "partial", "超管手动操作",
			"已接入官方桌面purpose=3短信和login_or_signup、账号隔离及未知结果屏障；真实收码与开户未验收，不自动发码或登录。", "internal/lajiaoliao/signup.go；internal/httpapi/lajiaoliao_signup.go；docs/multi-platform.md"},
		{"account_login", "桌面密码登录与人工验证", "账号", "session", "confirmed", "partial", "超管操作", "固定来源、稳定设备标识、加密会话；设备占用不自动接管。", evidence},
		{"session_restore", "会话恢复与换票", "账号", "session", "confirmed", "partial", "系统内部", "复用账号读取调度器；只有明确过期码触发换票。", evidence},
		{"receive_message", "持续接收与文本解码", "消息", "read", "confirmed", "partial", "在群且可接收",
			"连续序号及占位原包密封提交；零序号推送独立去重且不推进恢复水位。好友与已读通知仅作只读展示，不建立认领证明或送达结论；真实认领及全天稳定性仍待验收。",
			"固定OpenIM SDK3.8.3；internal/lajiaoliao/transport.go；internal/lajiaoliao/push.go；internal/lajiaoliao/friend_notice.go；internal/gateway/lajiaoliao_push.go；docs/native-receiver.md"},
		{"send_text", "发送群文本", "消息", "action", "confirmed", "partial", "显式原生调试授权；在群且可发言",
			"受控入口复用既有持久操作、原连接及回执；消息ID绑定持久操作，最终重验授权、角色、租约和epoch；未知结果冻结，不自动重发，真实发送独立验收。",
			"internal/httpapi/lajiaoliao_test_text.go；internal/control/native_recall.go；internal/gateway/assistant_messages.go；docs/lajiaoliao-client-completion.md"},
		{"list_groups", "群列表", "群组", "read", "confirmed", "partial", "账号可见群", "有界分页与加密快照；目录不代表 SaaS 授权。", evidence},
		{"edit_group_info", "修改群名称", "群组", "action", "confirmed", "partial", "当前群主 / 管理员；单群本人确认",
			"独立set_group_info_ex groupName、持久操作及最终角色栅栏；真实原生和生产准入未验收。", "固定OpenIM SetGroupInfoExReq；internal/lajiaoliao/native.go；internal/control/assistant_execution.go"},
		{"list_members", "群成员目录", "群组", "read", "confirmed", "partial", "账号原生可读范围", "按需合并刷新；不完整列表不用于动作授权。", evidence},
		{"read_roles", "群主与管理员角色", "群组", "read", "confirmed", "partial", "账号原生可读范围", "明确角色映射；动作必须再次读取当前身份。", evidence},
		{"mute_member", "成员禁言", "群管", "action", "confirmed", "partial", "管理员操作普通成员；群主可操作普通成员或管理员，不能操作本人或群主",
			"独立角色复核、mutedSeconds及持久控制已接通；本群对自有普通成员禁言一分钟成功，明确名单独立回读一致。", "internal/lajiaoliao/native.go；internal/control/test_tools.go；docs/lajiaoliao-native.md"},
		{"unmute_member", "成员解禁", "群管", "action", "confirmed", "partial", "管理员操作普通成员；群主可操作普通成员或管理员，不能操作本人或群主",
			"使用cancel_mute_group_member、持久提交和只读对账；同窗口手动解禁成功，明确名单已无目标。", "internal/lajiaoliao/native.go；internal/control/test_tools.go；docs/lajiaoliao-native.md"},
		{"quote_reply", "引用回复", "消息", "action", "confirmed", "partial", "超管受控窗口；本群近期文本引用",
			"已接原生114及密封持久意图；本站发送与对端结构化引用、原消息标识独立核对通过。", "internal/lajiaoliao/outbound.go；internal/control/chili_message.go；docs/lajiaoliao-outbound-20261104.md"},
		{"mention_member", "@成员", "消息", "action", "confirmed", "partial", "超管受控窗口；明确授权成员",
			"已接原生106及字符串成员列表；本站发送与对端实际提及核对通过，不以普通@文字认定。", "internal/lajiaoliao/outbound.go；internal/control/chili_message.go；docs/lajiaoliao-outbound-20261104.md"},
		{"recall_message", "撤回消息", "消息", "action", "confirmed", "partial", "当前群主 / 管理员；原生时限仍待真实核验",
			"已接独立受控入口及助手生产隔离：从本群两分钟内留存消息确定conversationID/seq，经持久授权和原连接最终栅栏撤回；未知结果不重发，真实原生及生产准入分别验收。",
			"固定桌面1.0.3及Android1.7.3；internal/httpapi/test_recall.go；internal/control/native_recall.go；docs/lajiaoliao-client-completion.md"},
		{"read_quote", "引用消息读取", "消息", "read", "confirmed", "partial", "超管读取当前账号留存消息",
			"114正文及有界quoteMessage已接入消息页，嵌套图片、视频、名片按当前账号及原会话生成按需预览；不修改历史解码状态。",
			"internal/lajiaoliao/message.go；internal/httpapi/lajiaoliao_resources.go；docs/lajiaoliao-client-completion.md"},
		{"read_mentions", "@成员消息读取", "消息", "read", "confirmed", "partial", "超管读取当前账号留存消息",
			"106正文及字符串成员ID、群昵称已接入有界展示；按账号和原会话核对，不套用旺商聊数字ID。", "internal/lajiaoliao/message.go；internal/httpapi/lajiaoliao_messages.go；docs/lajiaoliao-message-evidence.md"},
		{"read_recalls", "撤回通知读取", "消息", "read", "confirmed", "partial", "超管显式读取当前账号与会话",
			"原生群内通知及测试群留存2101解析匹配已核对；只读弹窗已部署，真实页面展示独立验收，不改历史消息或恢复水位。", "internal/lajiaoliao/recall.go；docs/native-recalls.md；docs/lajiaoliao-message-evidence.md"},
		{"read_forward", "合并聊天记录解析", "消息", "read", "confirmed", "partial", "超管读取当前账号留存消息",
			"107按顺序展示有界嵌套引用及@；深度、条数和正文均有限制，未知项保留占位，不自动补拉历史。", "internal/lajiaoliao/message.go；internal/httpapi/lajiaoliao_messages.go；docs/lajiaoliao-message-evidence.md"},
		{"grant_admin", "设置群管理员", "群管", "action", "confirmed", "partial", "仅原生群主；目标当前为普通成员",
			"独立roleLevel=60持久Tools已在单个自有成员上受控成功，目标自身角色现读为管理员；生产业务和异常恢复未验收。", "internal/lajiaoliao/native.go；docs/native-test-preparation-20261103.md"},
		{"revoke_admin", "撤销群管理员", "群管", "action", "confirmed", "partial", "仅原生群主；目标当前为管理员",
			"独立roleLevel=20持久提交受控成功，目标自身角色现读恢复普通成员，窗口已关闭；生产业务和异常恢复未验收。", "internal/lajiaoliao/native.go；docs/native-test-preparation-20261103.md"},
		{"read_group_events", "群管理通知读取", "群管", "read", "confirmed", "partial", "超管显式读取当前账号与原会话",
			"群资料、成员入退、移出、禁言、公告及置顶变化按类型投影；新移动端通知已逐条核对，不把完整群快照当成字段变更，也不作为角色授权证据。",
			"internal/lajiaoliao/group_notice.go；docs/lajiaoliao-membership-20261104.md"},
		{"read_group_bans", "原生群禁入名单", "群管", "read", "confirmed", "partial", "当前原生群主 / 管理员",
			"独立分页读取group permanent bans；本站永久移出及解除后，完整名单精确经历单目标到空列表。与好友黑名单、业务黑名单独立。",
			"internal/lajiaoliao/group_bans.go；internal/httpapi/lajiaoliao_group_bans.go；docs/lajiaoliao-membership-20261104.md"},
		{"remove_group_ban", "解除原生群禁入", "群管", "action", "confirmed", "partial", "当前原生群主 / 管理员；明确禁入目标及独立动作授权",
			"固定remove_group_permanent_ban、完整名单核对及密封持久操作已接通；本站单目标解除与独立回读通过，不自动邀请或入群，生产和故障恢复未验收。",
			"internal/lajiaoliao/group_bans.go；internal/control/native_social.go；docs/lajiaoliao-membership-20261104.md"},
		{"friend_delete", "删除好友（仅超管受控测试）", "好友与邀请", "action", "confirmed", "partial", "仅超管独立native_debug窗口；一账号、一目标、一次预算",
			"人工账号好友操作，本平台独立使用字符串身份和完整好友列表核对。仅所选隔离群的当前群主或管理员账号、明确非本人好友及单次持久意图可提交；删除不退群、不拉黑，未知只核对原操作。客户接入取消或到期不自动删除；不供助手、热群或业务模块调用，不进入群管底座。本平台真实删除及故障恢复尚未验收。",
			"固定桌面1.0.3/OpenIM /friend/delete_friend ownerUserID及friendUserID；internal/lajiaoliao/friend_delete.go；internal/lajiaoliao/friend_delete_test.go；internal/control/friend_delete.go；internal/httpapi/test_tools.go；internal/database/migrations/065_controlled_friend_delete.sql；docs/native-social.md"},
	}
	// Preserve existing IDs as explicit investigation entries. New WSL entries
	// must never silently enlarge the Lajiaoliao capability or permission set.
	for _, item := range []struct{ id, name, category, kind string }{
		{"account_settings", "基础账号资料", "账号", "action"},
		{"read_images", "图片与表情包解析", "消息", "read"},
		{"read_avatars", "消息头像解析", "消息", "read"},
		{"read_contact_cards", "联系人名片解析", "消息", "read"},
		{"read_video", "视频消息解析与预览", "消息", "read"},
		{"send_private_text", "发送私聊文本", "消息", "action"},
		{"send_image", "发送图片", "消息", "action"},
		{"send_media", "群语音 / 视频", "消息", "action"},
		{"mute_group", "全群禁言 / 解除", "群管", "action"},
		{"kick_member", "移出群成员", "群管", "action"},
		{"publish_notice", "发布群公告", "群管", "action"},
		{"set_member_card", "修改群内昵称", "群管", "action"},
		{"read_mutes", "禁言名单查询", "群管", "read"},
		{"read_invitations", "邀请记录与邀请归属", "好友与邀请", "read"},
		{"list_friends", "好友列表与资料", "好友与邀请", "read"},
		{"approve_friend", "好友申请审批", "好友与邀请", "action"},
		{"invite_group", "拉入群组", "好友与邀请", "action"},
		{"leave_group", "退出群组", "好友与邀请", "action"},
		{"onboarding_friend_request", "主动搜索加好友（仅超管受控测试）", "好友与邀请", "action"},
		{"read_voice", "语音消息解析与播放", "消息", "read"},
		{"read_file", "文件消息解析与下载", "消息", "read"},
		{"send_private_image", "发送私聊图片", "消息", "action"},
	} {
		out = append(out, NativeCapability{ID: item.id, Name: item.name, Category: item.category, Kind: item.kind,
			Support: "review", Integration: "pending", Role: "本平台契约及角色限制待核验",
			Summary:  "保留原目录ID供本平台独立核实；格式、接口、权限和本站适配均待取得辣椒聊证据。",
			Evidence: "docs/platform-native-scope.md；本平台契约尚未核验"})
	}
	reviewed := map[string]struct{ role, summary string }{
		"quote_reply":               {"超管受控窗口；仅当前群留存文本引用", "原生114、原消息ID/seq、密封意图和回执已接通；本站发送与另一自有账号的结构化引用精确匹配，生产准入独立关闭。"},
		"mention_member":            {"超管受控窗口；单个明确授权成员", "原生106及字符串atUserList已接通；本站发送后对端明确匹配自有成员，不以普通@文字代替，生产准入独立关闭。"},
		"send_private_text":         {"超管独立私聊目标授权", "原生sessionType=1和字符串对端已接通；单个自有收件人实际收到本站文本，client标识和私聊归属一致，未知结果不重发。"},
		"send_private_image":        {"超管独立私聊图片上传和发送授权", "102、单独上传预算及私聊目标栅栏已接通；PNG实际发送及对端资源读取通过，群授权不替代私聊授权。"},
		"send_image":                {"超管受控窗口；在群且可发送", "JPEG/PNG按需上传、原生102及密封资源回执已接通；PNG上传、发送、对端接收及资源读取独立通过。"},
		"read_images":               {"超管读取当前账号与会话的留存消息", "已核对102的sourcePicture/bigPicture/snapshotPicture；群及私聊图片按需读取，嵌套引用和合并记录复用有界预览；真实资源验收独立记录。"},
		"read_avatars":              {"超管读取当前账号与会话的留存消息", "已接消息senderFaceURL及联系人faceURL；仅本站授权地址、固定资源域名、按需加载，不输出上游URL或票据。"},
		"read_contact_cards":        {"超管读取当前账号与会话的留存消息", "已核对108的userID/nickname/faceURL；原生字符串身份及名片头像独立展示，忽略不透明ex，不增加好友动作。"},
		"read_video":                {"超管读取当前账号与会话的留存消息", "已接104的videoUrl、snapshotUrl及大小/时长；16MiB内按需播放，封面独立读取，不下载后台媒体或转码。"},
		"send_media":                {"超管受控窗口；当前群主 / 管理员且可发言", "本平台MP3与带JPEG封面的MP4分别完成上传、持久发送和对端103/104核对；视频及封面可读，无后台转码，其他编码和生产准入未验收。"},
		"account_settings":          {"超管本人显式确认", "基础昵称、当前头像预览及头像上传保存已接入；按单项持久控制和账号版本核对，真实修改独立验收。其他个人设置使用官方 App，历史状态保留只读核对。"},
		"mute_group":                {"原生群主 / 管理员及受控窗口", "mute_group / cancel_mute_group已接入持久Tools及独立状态读取；真实执行未验收。"},
		"kick_member":               {"管理员操作普通成员；群主可操作管理员，禁止本人及群主", "本站普通移出和永久禁入分别通过单个自有成员验收，完整名单及禁入状态独立回读；两种动作授权分开，生产及故障恢复未验收。"},
		"publish_notice":            {"原生群主 / 管理员及受控窗口", "set_group_info_ex notification已接入持久Tools；读取当前公告，不声称有完整公告历史。"},
		"set_member_card":           {"本人或有目标层级权限的群主 / 管理员", "set_group_member_info nickname已接入单项编辑及控制栅栏；最多15字符，真实修改未验收。"},
		"read_mutes":                {"本平台可读群成员范围", "通过完整有界成员列表的muteEndTime读取当前禁言；分页不完整不用于授权。"},
		"read_invitations":          {"原生群主 / 管理员或受邀本人及受控窗口", "管理侧申请和受邀本人单群邀请分别读取；本人唯一待处理邀请已核对，不作返现、奖励或完整历史依据。"},
		"list_friends":              {"超管本人原生好友范围", "按friendsInfo.friendUser读取字符串ID及昵称，有界分页完整性校验已接入。"},
		"approve_friend":            {"超管逐项审批或接入专用准入及本人单次确认", "受控同意固定申请createTime编号、申请人和一次预算；接入向导按完整昵称、服务器10分钟窗口选择申请，密封后本人单次确认。普通接入要求滑块；超管本人演示空间的有效6小时接入测试范围免滑块，仅允许指定自有号池身份，私聊及群证明独立核验。测试证明不作为客户认领通过，关闭或到期后阻断新提交；旧附言核验关闭，不向助手或热群开放。"},
		"onboarding_friend_request": {"仅超管独立native_debug窗口；精确账号、目标、期限与次数", "客户接入的旧主动搜索、添加入口统一410。仅隔离受控测试允许精确查找与单次持久申请；有效接入范围可指定申请账号向接收机器人发送一次申请，父挑战和独立私聊、群证明不改写。最终提交复核双方账号、授权、角色、连接、epoch、期限和预算；已有好友不重加，未知只核对原记录。联系人不作为网站身份证据，不供助手、热群或业务模块调用；本平台真实出站验收尚未完成。"},
		"invite_group":              {"原生群主 / 管理员、当前好友及受控窗口；接受须独立本人授权", "本站单次邀请及受邀本人独立接受均持久成功，双方完整名单和本人目录确认恢复普通成员；其他策略、生产及故障恢复未验收。"},
		"leave_group":               {"非群主本人及受控窗口；该群业务均停止", "本站普通成员单次持久退群通过，完整群主名单、本人目录及1504通知独立核对；不自动退出或重发，其他角色、生产及故障恢复未验收。"},
	}
	for i := range out {
		if item, ok := reviewed[out[i].ID]; ok {
			out[i].Support, out[i].Integration = "confirmed", "partial"
			out[i].Role, out[i].Summary = item.role, item.summary
			out[i].Evidence = "internal/lajiaoliao/native.go；internal/lajiaoliao/profile.go；internal/gateway/lajiaoliao_native.go；docs/lajiaoliao-native.md"
			if out[i].ID == "read_invitations" {
				out[i].Name = "入群申请读取"
			}
			if out[i].ID == "send_media" {
				out[i].Evidence = "internal/lajiaoliao/media.go；internal/control/lajiaoliao_media.go；docs/lajiaoliao-outbound-20261104.md"
			}
			switch out[i].ID {
			case "onboarding_friend_request":
				out[i].Evidence = "固定桌面1.0.3及OpenIM好友契约；internal/lajiaoliao/onboarding_outbound.go；internal/control/onboarding_controlled.go；internal/control/onboarding_controlled_claimant.go；internal/control/group_binding_outbound_submit.go；internal/httpapi/onboarding_controlled.go；internal/database/migrations/064_onboarding_controlled_outbound.sql；internal/database/migrations/070_controlled_onboarding_applicant.sql"
			case "approve_friend":
				out[i].Evidence = "固定桌面1.0.3 get_friend_apply_list/respond_friend_apply及createTime；internal/lajiaoliao/onboarding_contact.go；internal/control/group_binding_contact_submit.go；docs/customer-onboarding.md"
			case "quote_reply", "mention_member", "send_private_text", "send_private_image", "send_image":
				out[i].Evidence = "固定OpenIM v3.8.3；internal/lajiaoliao/outbound.go；internal/control/chili_message.go；internal/control/lajiaoliao_media.go；docs/lajiaoliao-outbound-20261104.md"
			case "read_images", "read_avatars", "read_contact_cards", "read_video":
				out[i].Evidence = "固定桌面1.0.3及Android1.7.3；internal/lajiaoliao/resources.go；internal/httpapi/lajiaoliao_resources.go；docs/lajiaoliao-client-completion.md"
			}
		}
	}
	return out
}

func CatalogForPlatform(platform string) []Definition {
	if platform == "wsl" {
		out := []Definition{}
		for _, item := range Catalog() {
			if item.Status != "manual_only" {
				out = append(out, item)
			}
		}
		return out
	}
	out := []Definition{}
	if platform != "lajiaoliao" {
		return out
	}
	for _, d := range Catalog() {
		switch d.ID {
		case "blocked_words", "blacklist", "exemptions", "keyword_replies", "scheduled_messages", "group_ai", "basic_group_chat":
			out = append(out, d)
		}
	}
	return out
}

func NativeBlockersForPlatform(platform string, definition Definition) []string {
	blocked := []string{}
	for _, id := range definition.NativeDependencies {
		accepted := false
		for _, c := range NativeCatalogForPlatform(platform) {
			if c.ID == id && NativeBusinessAllowed(platform, id) && c.Support == "confirmed" && c.Integration == "accepted" {
				accepted = true
				break
			}
		}
		if !accepted {
			blocked = append(blocked, id)
		}
	}
	return blocked
}
