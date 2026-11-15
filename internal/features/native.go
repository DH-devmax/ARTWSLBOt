package features

// NativeCapability describes a reviewed contract, never an executable endpoint.
// Integration is code coverage, not current account health or native acceptance.
type NativeCapability struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Category    string `json:"category"`
	Kind        string `json:"kind"`
	Support     string `json:"support"`
	Integration string `json:"integration"`
	Role        string `json:"role"`
	Summary     string `json:"summary"`
	Evidence    string `json:"evidence"`
}

func NativeCatalog() []NativeCapability {
	const legacy = "已核对旧接入器 a672ef9；当前平台兼容性待验收"
	const client = "已核对固定官方客户端开户契约；当前仅桌面 2.8.3 接入，完整链路待验收"
	return []NativeCapability{
		{"account_signup", "新账号注册", "账号", "session", "confirmed", "implemented", "超管操作",
			"独立开户流程，成功后仍需显式登录。", client},
		{"account_login", "密码 / 短信登录", "账号", "session", "confirmed", "implemented", "超管操作",
			"包含人工人机验证、换设备验证和安全码；登录不等于消息连接在线。", client},
		{"session_restore", "会话恢复与换票", "账号", "session", "confirmed", "partial", "系统内部",
			"密封会话的短时连接重建、同步及ACK已有隔离证据；失效换票、上游故障和全天长稳另行验收。", legacy},
		{"account_settings", "账号资料与隐私设置", "账号", "action", "confirmed", "partial", "超管显式确认",
			"安全读取本人资料，单项修改昵称及隐私；头像、改密和换绑分别验收。二级安全码设置与实名认证按所有者决定暂缓，入口关闭，不计入本轮验收。", "固定桌面2.8.3设置及安全路由；docs/native-settings.md；docs/platform-native-scope.md"},
		{"receive_message", "群消息接收", "消息", "read", "confirmed", "partial", "在群且可接收",
			"可靠落盘后ACK、认证文本/@/引用及媒体投影；原生群通知单独只读展示，不作为权限或业务执行依据。完整业务消费仍待验收。", "internal/nativewire/message_notice.go；internal/gateway/decoder_notices.go；docs/decoder-diagnosis-20261102.md"},
		{"read_images", "图片与表情包解析", "消息", "read", "confirmed", "partial", "超管读取当前账号留存消息",
			"图片/表情元数据与有界受控下载；超出预览大小/像素限制仍保留消息占位，下载限制不放宽。真实资源可用性分别验收，不含发送。", "固定桌面2.8.3字段；docs/native-media.md；docs/decoder-diagnosis-20261102.md"},
		{"read_forward", "合并聊天记录解析", "消息", "read", "confirmed", "partial", "超管读取当前账号留存消息",
			"显示消息附带的文本/图片记录；嵌套引用、未支持条目和截断明确提示，不自动补拉其他会话历史。", "固定桌面 2.8.3 forward/entry 字段；见 docs/native-media.md"},
		{"read_avatars", "消息头像解析", "消息", "read", "confirmed", "partial", "超管读取当前账号留存消息",
			"解析消息、转发条目、联系人名片及当前账号群目录的头像引用，按作用域懒加载；目录头像与业务群/传输群对应，缺失回退占位，不修改原生资料。", "固定桌面 2.8.3 MessageSource、friendCard 与 groupAvatar；见 docs/native-media.md"},
		{"read_contact_cards", "联系人名片解析", "消息", "read", "confirmed", "partial", "超管读取当前账号留存消息",
			"只读显示类型13的联系人昵称、账号及受限头像；名片主体不替代消息发送者或操作授权，不调用资料查询或加好友。", "固定桌面 2.8.3 Message.friendCard 字段108；见 docs/native-media.md"},
		{"read_video", "视频消息解析与预览", "消息", "read", "confirmed", "partial", "超管读取当前账号留存消息",
			"解析类型3及合并记录内的视频信息，懒加载封面、点击读取不超过16MiB的MP4/WebM；外链不读取，不上传、转码或发送。", "固定桌面 2.8.3 Message.video 字段103、VideoMsg；见 docs/native-media.md"},
		{"send_text", "发送群文本", "消息", "action", "confirmed", "partial", "在群且可发言；独立超管受控授权",
			"自有普通成员的本站群文本发送和另一账号实际解码已核验；持久意图、最终栅栏及未知结果核对保持。生产准入及旧nonce/恢复历史核对单列。", legacy},
		{"send_private_text", "发送私聊文本", "消息", "action", "confirmed", "partial", "仅超管独立私信目标授权；生产准入另验",
			"本站自有双账号私聊文本发送、持久回执和收件端实际解码已核验；7/1当前连接、留存私聊和最新成员/好友核验保持，支持独立native_debug授权。真实故障、nonce历史和生产准入另验。", "internal/control/native_private.go；internal/control/test_tools.go；docs/native-private-acceptance-20261103.md"},
		{"quote_reply", "引用回复", "消息", "action", "confirmed", "partial", "在群且可发言",
			"本站本群近期文本引用已持久发送并由另一账号解码，原消息标识一致；媒体引用发送及生产业务另验，生产入口关闭。", legacy},
		{"mention_member", "@成员", "消息", "action", "confirmed", "partial", "在群且可发言",
			"本站对单个自有成员的提及发送、接收成员标识和UTF-16位置已核验；多人、改名竞态及生产业务另验，生产入口关闭。", legacy},
		{"recall_message", "撤回消息", "消息", "action", "confirmed", "partial", "本人消息或原生管理权限",
			"独立受控入口已持久撤回本群两分钟内自有成员文本，双方通知匹配；助手保持原生产准入和本人确认，未知结果只读核对。", "internal/control/native_recall.go；internal/gateway/assistant_messages.go；docs/native-controlled-sends-20261103.md"},
		{"send_image", "发送图片", "消息", "action", "confirmed", "partial", "超管隔离测试；在群且可发言",
			"本站PNG上传、持久群发送和另一账号实际解码已核验，上传与发送独立授权和计数；私信另列，生产入口关闭。", "固定桌面2.8.3 uploadFile及Message.image字段101；见 docs/native-media.md"},
		{"send_media", "语音 / 视频发送", "消息", "action", "confirmed", "partial", "超管隔离测试；在群且可发言",
			"16MiB有界上传和持久群发送；MP3语音、有封面非分片MP4分别通过本站发送及另一账号解码。PCM WAV兼容性未通过；文件不接入，生产入口关闭。", "固定桌面2.8.3 AssetType、Message.voice/video；见 docs/native-media.md"},
		{"list_groups", "群列表与群资料", "群组", "read", "confirmed", "partial", "账号可见群组",
			"已接入只读目录、业务群/消息群映射和带采样时间的加密快照；目录不能代替成员权限核验。", legacy},
		{"edit_group_info", "修改群名称", "群组", "action", "confirmed", "partial", "当前群主 / 管理员；单群本人确认",
			"固定set-group-name、单项名称校验、持久操作及最终角色栅栏；群主修改和恢复已独立回读核验，生产准入未验收。", "固定官方group.proto SetGroupNameRequest；internal/nativeauth/moderation.go；internal/control/test_tools.go"},
		{"list_members", "成员列表与身份映射", "群组", "read", "confirmed", "partial", "超管按需读取原生可见群",
			"已接入有界分页、身份去重、冲突拒绝、读取合并及只读成员面板；真实完整性和业务复用待验收。", legacy},
		{"read_roles", "群主 / 管理员身份核验", "群组", "read", "confirmed", "partial", "原生可读范围",
			"显式核验从当前连接取得完整成员、自身身份及角色，落库前复核网站会话、epoch、部署和租约；不会自动认领或启用群组。", legacy},
		{"mute_member", "成员禁言", "群管", "action", "confirmed", "partial", "超管隔离测试；群主 / 管理员，受目标角色限制",
			"已接入固定桌面接口、授权窗口、持久操作及最终角色栅栏；测试时长最多5分钟。自有成员一分钟禁言及到期观察已核验；其他场景及生产业务仍待验收。", "桌面2.8.3 set-member-mute；见 docs/controlled-tools.md"},
		{"unmute_member", "成员解禁", "群管", "action", "confirmed", "partial", "超管隔离测试；群主 / 管理员，受目标角色限制",
			"已接入原生解禁、持久操作及完整禁言名单只读核对；自有成员手动解禁已核验，名单未知不按空列表处理；其他场景及生产业务仍待验收。", "桌面2.8.3 member-mute-cancel；见 docs/controlled-tools.md"},
		{"mute_group", "全群禁言 / 解除", "群管", "action", "confirmed", "partial", "超管隔离测试；群主 / 管理员",
			"原生全群禁言及解除已在隔离群提交、回读并恢复原状态；自动业务触发及异常恢复仍待验收。", "桌面2.8.3 set-group-mute；见 docs/controlled-tools.md"},
		{"kick_member", "移出群成员", "群管", "action", "confirmed", "partial", "超管隔离测试；群主 / 管理员，受目标角色限制",
			"群主单次移出自有普通成员及双方离群回读已受控核验，随后恢复入群；保护本人、群主与角色层级，未知结果只读核对，生产准入未验收。", "桌面2.8.3 remove-group-member；见 docs/controlled-tools.md"},
		{"publish_notice", "发布群公告", "群管", "action", "confirmed", "partial", "超管隔离测试；群主 / 管理员",
			"普通公告在隔离群单次发布及独立回读已核验，不降级为聊天文本；最多600字，自动业务触发及其他场景仍待验收。", "桌面2.8.3 add-notice；见 docs/controlled-tools.md"},
		{"set_member_card", "修改群内昵称", "群管", "action", "confirmed", "partial", "本人或有目标层级权限的群主 / 管理员",
			"群主本人、群主修改自有成员及普通成员本人编辑均已修改/恢复并回读核验；自动名片和故障恢复未验收。提交仍核对绑定、准入、角色、旧值和版本。", "固定桌面2.8.3 set-member-nickname；见 docs/native-settings.md"},
		{"read_mutes", "禁言名单查询", "群管", "read", "confirmed", "partial", "原生可读范围",
			"已补8/27完整单独禁言列表及成员映射核对，用于发送前检查和账号身份核验；不等于全群发言策略已验收。", legacy},
		{"read_invitations", "邀请记录与邀请归属", "好友与邀请", "read", "confirmed", "partial", "超管隔离测试；仅账号可见窗口",
			"Tools按账号统一采集并共享30秒窗口，再按已核对群映射过滤；保留申请人和邀请人独立ID及状态，不宣称完整历史，不直接用于奖励记账。", "固定桌面2.8.3 get-apply-logs；见 docs/controlled-tools.md"},
		{"list_friends", "好友列表与资料", "好友与邀请", "read", "confirmed", "partial", "超管读取当前账号",
			"邀请面板读取有界好友安全投影；黑名单不作邀请目标，不恢复私聊管理指令。", "固定桌面2.8.3 get-friend-list；见 docs/native-social.md"},
		{"approve_friend", "好友申请审批", "好友与邀请", "action", "confirmed", "partial", "超管逐项审批或接入专用准入及本人单次确认",
			"受控同意使用独立原生动作授权；接入向导按服务器10分钟窗口及完整昵称选择唯一待处理申请，密封后本人单次确认。普通接入要求滑块；超管本人演示空间的有效6小时接入测试范围免滑块，仅允许所选自有号池身份，真实私聊及群证明分别核验；测试证明不算客户认领通过。关闭或到期后阻断新提交，未知仅核对原记录，不向助手或热群开放好友审批。", "固定桌面2.8.3 friend-apply-list/handler及applyTime；internal/nativeauth/onboarding_contact.go；internal/control/group_binding_contact_submit.go；internal/control/controlled_onboarding_verification.go；internal/database/migrations/066_controlled_friend_approval.sql；docs/controlled-onboarding.md"},
		{"onboarding_friend_request", "主动搜索加好友（仅超管受控测试）", "好友与邀请", "action", "confirmed", "partial", "仅超管独立native_debug窗口；精确账号、目标、期限与次数",
			"客户接入不再提供主动搜索或加好友，旧HTTP入口统一410。仅超管隔离测试复用持久授权窗口与单次出站意图；有效接入测试范围可指定申请账号向接收机器人发送一次申请，父挑战和身份、群证明保持独立。搜索和发送最终重验双方账号、角色、连接、epoch、期限和预算；未知结果只核对原记录。联系人不证明网站身份，不供助手、热群或业务模块调用。WSL搜索仍须独立Android3.7.4认证；真实出站验收未完成。", "固定Android3.7.4搜索及好友申请契约；internal/nativeauth/onboarding_outbound.go；internal/control/onboarding_controlled.go；internal/control/onboarding_controlled_claimant.go；internal/control/group_binding_outbound_submit.go；internal/httpapi/onboarding_controlled.go；internal/database/migrations/064_onboarding_controlled_outbound.sql；internal/database/migrations/070_controlled_onboarding_applicant.sql"},
		{"friend_delete", "删除好友（仅超管受控测试）", "好友与邀请", "action", "confirmed", "partial", "仅超管独立native_debug窗口；一账号、一目标、一次预算",
			"人工账号好友操作，仅从当前完整好友名单选择非本人目标，所选隔离群的当前群主或管理员账号显式确认。复用密封持久操作及最终授权栅栏，删除好友不退群、不拉黑；未知结果只核对原操作。客户接入取消或到期不自动删除；不供助手、热群或业务模块调用，不进入群管底座。本站真实删除及故障恢复待独立验收。", "固定桌面2.8.3 /v1/friend/del-friend uid及空响应；internal/nativeauth/social.go；internal/nativeauth/friend_delete_sodium_test.go；internal/control/friend_delete.go；internal/httpapi/test_tools.go；internal/database/migrations/065_controlled_friend_delete.sql；docs/native-social.md"},
		{"invite_group", "拉入群组", "好友与邀请", "action", "confirmed", "partial", "群主 / 管理员、好友套餐与受控授权窗口",
			"一次邀请一位授权且未在群的好友；另有受邀账号本人接受入口。自有账号邀请、接受及双方名单确认已核验；不会自动接受邀请，其他场景仍待验收。", "固定桌面2.8.3 add-group-member/group-member-invite；见 docs/native-social.md"},
		{"leave_group", "退出群组", "好友与邀请", "action", "confirmed", "partial", "本人非群主、停止群服务与受控授权窗口",
			"仅退出账号自己所在的授权隔离群；须先停用群服务，保留授权和历史。自有普通成员退群及双方目录确认已核验，其他场景仍待验收。", "固定桌面2.8.3 leave-group；见 docs/native-social.md"},
		{"read_voice", "语音消息解析与播放", "消息", "read", "confirmed", "partial", "超管读取当前账号留存消息",
			"四秒真实语音元数据及固定源资源已只读核对；产品页面仍仅展示时长，受控资源播放入口未接齐，不能判定播放或发送通过。", "internal/nativewire/message_media.go；docs/native-media.md；docs/wsl-native-acceptance-20261103.md"},
		{"read_file", "文件消息解析与下载", "消息", "read", "confirmed", "partial", "超管读取当前账号留存消息",
			"已投影文件元数据；完整下载、资源可用性和真实样本验收未完成。", "internal/nativewire/message_media.go；docs/native-media.md"},
		{"send_private_image", "发送私聊图片", "消息", "action", "confirmed", "partial", "仅超管独立私信目标授权",
			"本站PNG上传、私聊持久发送及自有收件账号实际解码已核验；5MiB有界上传与发送独立native_debug授权计数，群发送权限不能代用。其他格式、nonce历史和生产准入另验，生产入口关闭。", "internal/control/native_private.go；internal/control/test_tools.go；docs/native-private-acceptance-20261103.md"},
		{"grant_admin", "设置群管理员", "群管", "action", "confirmed", "partial", "当前原生群主；受控窗口内单个普通成员",
			"已接入固定桌面接口、密封持久意图、群主与目标保护、最终授权栅栏和只读角色核对；真实受控验收单独记录，生产业务未启用。", "固定桌面2.8.3 add-group-manage/groupManagerIds；docs/controlled-tools.md"},
		{"revoke_admin", "撤销群管理员", "群管", "action", "confirmed", "partial", "当前原生群主；受控窗口内单个管理员",
			"已接入固定桌面接口、持久提交和未知结果只读核对；禁止撤销本人或群主、禁止管理员代替群主操作，生产业务未启用。", "固定桌面2.8.3 del-group-manage/userId；docs/controlled-tools.md"},
		{"read_quote", "引用消息读取", "消息", "read", "confirmed", "partial", "超管读取当前账号留存消息",
			"已投影留存消息的引用ID及摘要；读取和发送分别验收，完整格式及会话边界真实覆盖待完成。", "internal/nativewire/message.go；internal/httpapi/conversations.go"},
		{"read_mentions", "@成员消息读取", "消息", "read", "confirmed", "partial", "超管读取当前账号留存消息",
			"已投影成员ID、名称及UTF-16位置；读取能力不等于本站已验收@发送。", "internal/nativewire/message.go；internal/httpapi/conversations.go"},
		{"read_recalls", "撤回通知读取", "消息", "read", "confirmed", "partial", "超管显式读取当前账号与会话",
			"7/14及7/15留存通知只读入口已部署；新测试群撤回在双方均匹配原消息，真实页面六条通知可见；其他角色另行验收。不改原消息、恢复水位或ACK。", "internal/nativewire/recall.go；internal/httpapi/native_recalls.go；docs/native-recalls.md；docs/test-report.md#旺商聊继续受控核验2026-11-03"},
	}
}

func FindNative(id string) (NativeCapability, bool) {
	for _, capability := range NativeCatalog() {
		if capability.ID == id {
			return capability, true
		}
	}
	return NativeCapability{}, false
}

// NativeBlockers fails closed for unknown or unaccepted contracts. This catalog
// gate cannot replace the control layer's submission-time authorization.
func NativeBlockers(definition Definition) []string {
	blocked := []string{}
	for _, id := range definition.NativeDependencies {
		capability, ok := FindNative(id)
		if !ok || !NativeBusinessAllowed("wsl", id) || capability.Support != "confirmed" || capability.Integration != "accepted" {
			blocked = append(blocked, id)
		}
	}
	return blocked
}
