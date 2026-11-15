package features

// ControlledAcceptance records a reviewed historical test scope, never current
// account health or production admission. No native identities or payloads belong here.
type ControlledAcceptance struct {
	VerifiedAt string `json:"verified_at"`
	Scope      string `json:"scope"`
	Result     string `json:"result"`
	Remaining  string `json:"remaining"`
	Evidence   string `json:"evidence"`
}

// PendingControlledAcceptance is a test requirement, never a historical result.
// It deliberately has no verification time or successful execution evidence.
type PendingControlledAcceptance struct {
	Status    string `json:"status"`
	Scope     string `json:"scope"`
	Remaining string `json:"remaining"`
	Evidence  string `json:"evidence"`
}

func NativePendingAcceptanceForPlatform(platform string) map[string]PendingControlledAcceptance {
	out := map[string]PendingControlledAcceptance{}
	switch platform {
	case "wsl":
		out["friend_delete"] = PendingControlledAcceptance{
			Status:    "not_run",
			Scope:     "旺商聊固定桌面2.8.3；仅超管独立native_debug窗口，所选隔离群当前群主或管理员账号、一个非本人好友、一次预算。",
			Remaining: "本站真实删除、完整好友名单精确回读、撤权、期限及未知结果恢复均未验收；不继承好友读取、审批、退群或辣椒聊结果，生产与业务准入保持关闭。",
			Evidence:  "docs/native-social.md#删除好友",
		}
	case "lajiaoliao":
		out["friend_delete"] = PendingControlledAcceptance{
			Status:    "not_run",
			Scope:     "辣椒聊固定桌面1.0.3；仅超管独立native_debug窗口，所选隔离群当前群主或管理员账号、一个非本人字符串好友身份、一次预算。",
			Remaining: "本站真实删除、字符串身份及完整好友名单精确回读、撤权、期限及未知结果恢复均未验收；不继承好友读取、审批、退群或旺商聊结果，生产与业务准入保持关闭。",
			Evidence:  "docs/native-social.md#删除好友",
		}
	}
	return out
}

func NativeAcceptanceForPlatform(platform string) map[string]ControlledAcceptance {
	out := map[string]ControlledAcceptance{}
	if platform == "lajiaoliao" {
		const scope = "所有者自有双账号、辣椒聊唯一隔离测试群；历史结果，不代表当前所选账号"
		const evidence = "docs/test-report.md#辣椒聊隔离测试绑定与角色验收2026-11-03"
		add := func(id, at, result, remaining string) {
			out[id] = ControlledAcceptance{at, scope, result, remaining, evidence}
		}
		add("list_members", "2026-11-03T05:46:42+08:00",
			"群主通过当前连接读取完整成员名单，确认另一个已认证自有测试账号是普通成员。",
			"仅当前隔离小群；大群分页、并发变更和异常恢复未真实验收。")
		add("read_roles", "2026-11-03T05:48:23+08:00",
			"群主与普通成员初始身份现读一致；升权、撤权后从目标账号自身连接分别核验管理员和普通成员。",
			"不继承旺商聊结果；其他角色组合、旧证明竞态和故障恢复未验收。")
		add("grant_admin", "2026-11-03T05:48:16+08:00",
			"群主对单个自有普通成员设置管理员一次；持久原生确认成功，目标账号自身现读为管理员。",
			"单窗口预算两次包含随后撤销；其他目标、批量、并发和未知结果恢复未验收。")
		add("revoke_admin", "2026-11-03T05:48:23+08:00",
			"同窗口撤销管理员一次，持久确认成功且目标账号自身现读恢复普通成员；窗口关闭，临时动作开关恢复。",
			"没有批量或自动重发；故障恢复和生产业务准入仍未验收。")
		const completionEvidence = "docs/test-report.md#辣椒聊解包补齐与真实发送撤回验收2026-11-03"
		completed := func(id, at, result, remaining string) {
			out[id] = ControlledAcceptance{at, scope, result, remaining, completionEvidence}
		}
		completed("send_text", "2026-11-03T14:31:28Z",
			"本站群主通过持久受控入口发送单条文本成功；另一自有账号实际收到，发送者、群、client标识与持久操作精确匹配。",
			"仅单群一次；无自动重发，生产准入、其他角色、断线及未知结果恢复未真实验收。")
		completed("recall_message", "2026-11-03T14:31:32Z",
			"同一窗口撤回上述本人文本一次，持久确认成功；另一自有账号收到2101通知，原消息标识和操作者精确匹配。",
			"两次总预算已用完，窗口及临时动作开关已关闭；撤回他人、时限边界和真实故障未验收。")
		completed("receive_message", "2026-11-03T14:31:44Z",
			"双账号当前连接收到并持久解码新群文本；发送回显与对端归群一致，撤回通知持久可读，操作后双方接收在线。",
			"仅隔离小群及既有样本；不代表全部格式、多群容量、生产业务或全天长稳通过。")
		completed("list_groups", "2026-11-03T14:20:56Z",
			"两个已认证自有账号分别读到唯一测试群，原生群标识与本站绑定一致；没有据此生成新租户授权。",
			"仅当前单群目录；大列表分页、入退群变化及故障恢复未验收。")
		completed("read_quote", "2026-11-03T14:30:14Z",
			"既有App演示的114引用及合并记录内引用均能结构化读取，真实页面显示引用正文及图片占位，账号和会话归属核对一致。",
			"仅既有样本读取，旧引用图片资源单独核验；不代表本站引用发送通过。")
		completed("read_mentions", "2026-11-03T14:30:14Z",
			"既有106演示按字符串原生ID和群昵称显示两个明确成员，真实页面提及列表可读；未套用旺商聊数字身份。",
			"仅既有样本；改名、同名和其他成员组合未验收，不代表本站@发送通过。")
		completed("read_forward", "2026-11-03T14:30:14Z",
			"既有107合并记录保持两条原顺序，真实页面展开后分别显示引用与提及，未自动补拉其他群。",
			"深层嵌套、截断和旧条目资源另行验收；不代表本站转发发送通过。")
		completed("read_images", "2026-11-03T14:30:14Z",
			"群及私聊102演示生成本站作用域地址；真实JPEG读取357409字节并禁止缓存，页面加载及解码成功。",
			"仅本JPEG样本；其他表情、超限、原生上传及本站发送未真实验收。")
		completed("read_avatars", "2026-11-03T14:30:14Z",
			"消息发送者头像通过本站作用域地址读取4045字节PNG，禁止缓存且页面显示成功。",
			"名片头像、所有缺省图、过期资源和目录头像未全部验收；未修改账号资料。")
		completed("read_recalls", "2026-11-03T14:31:32Z",
			"既有演示及本站本轮2101通知均按本群读取；页面弹窗展示原消息标识，另一账号匹配本轮撤回，原消息记录保留。",
			"其他角色、私聊及过期消息未验收，不改恢复游标或原始历史。")
		out["read_video"] = ControlledAcceptance{"2026-11-03T16:02:58Z", scope,
			"所有者App新发104短视频通过本站授权地址读取2642719字节MP4及44038字节JPEG封面；浏览器完成720×1280解码播放，未出现媒体错误。",
			"仅当前单个短视频；超限、其他编码及本站视频发送未验收。",
			"docs/test-report.md#辣椒聊基础资料与新增媒体验收2026-11-04"}
		out["read_contact_cards"] = ControlledAcceptance{"2026-11-03T16:02:58Z", scope,
			"所有者App新发108本人名片按字符串身份和昵称展示；名片头像通过本站授权地址读取4045字节PNG并在页面显示。",
			"仅本人名片读取，不代表好友操作、其他名片类型或本站发送通过。",
			"docs/test-report.md#辣椒聊基础资料与新增媒体验收2026-11-04"}
		const moderationEvidence = "docs/test-report.md#辣椒聊头像与群管实操验收2026-11-04"
		out["mute_member"] = ControlledAcceptance{"2026-11-04T01:06:10+08:00", scope,
			"本站群主对另一个已认证自有普通成员禁言一分钟，持久回执成功，独立回读明确禁言名单包含该目标。",
			"单群单目标一次，随后手动解禁；精确自动到期、角色变化及故障恢复未验收。", moderationEvidence}
		out["unmute_member"] = ControlledAcceptance{"2026-11-04T01:06:17+08:00", scope,
			"同一窗口手动解禁成功，独立回读明确禁言名单已无该目标；两次预算用尽，窗口关闭，动作开关恢复。",
			"无批量、自动重发或生产业务激活；未知结果和重启恢复未验收。", moderationEvidence}
		out["read_mutes"] = ControlledAcceptance{"2026-11-04T01:06:17+08:00", scope,
			"禁言与解禁后名单均明确可读，mutes_known为true，按目标精确核对状态变化；未把未知结果当空名单。",
			"仅当前两人测试群；大群、到期边界、并发变更和故障恢复未验收。", moderationEvidence}
		const sendsEvidence = "docs/test-report.md#辣椒聊本站头像与八种发送验收2026-11-04"
		sent := func(id, at, result, remaining string) {
			out[id] = ControlledAcceptance{at, scope, result, remaining, sendsEvidence}
		}
		sent("send_text", "2026-11-03T19:17:54Z",
			"本站群主持久发送一条101群文本，服务器接受；另一自有账号实际接收，原生client标识、发送者和本群精确匹配。",
			"保留此前单群结果；生产业务、断线、撤权及未知提交恢复未真实验收，无自动重发。")
		sent("quote_reply", "2026-11-03T19:18:25Z",
			"本站发送原生114文本引用成功；对端结构化引用正文与本群选中原文一致，发送消息的持久client标识和本群精确匹配。",
			"仅本群近期文本引用；媒体引用、原文过期及故障恢复未验收，生产入口关闭。")
		sent("mention_member", "2026-11-03T19:18:53Z",
			"本站发送原生106并明确提及另一自有普通成员；对端结构化提及包含该字符串身份，client标识和本群一致。",
			"仅一个明确授权成员；多人、改名竞态及生产业务未验收，不以普通@文字替代。")
		sent("send_image", "2026-11-03T19:26:42Z",
			"本站PNG上传和持久102群图片发送分别成功；对端实际接收，图片经本站作用域地址读取成功，client标识和群归属一致。",
			"仅PNG单样本；其他编码、超限、故障恢复及生产业务未验收。")
		sent("send_media", "2026-11-03T19:27:36Z",
			"本站MP3语音与带JPEG封面MP4分别上传并持久发送；对端分别收到103/104，视频与封面资源可读，语音、视频原子项独立留证。",
			"仅所列样本；WAV、其他编码、移动端实际播放和故障恢复未验收，生产入口关闭。")
		const privateScope = "所有者自有双账号、辣椒聊隔离测试群授权下的一对一私聊；仅单个自有收件人"
		out["send_private_text"] = ControlledAcceptance{"2026-11-03T19:51:26Z", privateScope,
			"本站向明确自有收件人持久发送一条101私聊文本；另一账号实际收到，原生client标识、发送者及私聊归属精确匹配。",
			"单条、无自动重发；不同好友策略、撤权、断线和未知提交恢复未验收，生产入口关闭。", sendsEvidence}
		out["send_private_image"] = ControlledAcceptance{"2026-11-03T19:52:15Z", privateScope,
			"本站PNG上传及102私聊发送分别成功；另一自有账号收到精确client标识的私聊图片，本站作用域资源可读。全轮三次预算用完，窗口关闭且动作开关恢复。",
			"仅PNG单样本；其他格式、过期资源及故障恢复未验收，不继承群图片或另一平台结果。", sendsEvidence}
		const leaveEvidence = "docs/test-report.md#辣椒聊本站本人退群验收2026-11-04"
		out["read_group_events"] = ControlledAcceptance{"2026-11-04T11:34:16Z", scope,
			"此前38份留存群通知逐项按原群读取；新增App退出及本站本人退出分别产生1504，按原群结构化显示成员离开且目标精确匹配，未修改历史解码状态。",
			"通知不作为当前原生角色或永久禁入证据；邀请及置顶等未观察类型仍只经过合成契约验证。", leaveEvidence}
		out["leave_group"] = ControlledAcceptance{"2026-11-04T11:34:16Z", scope,
			"本站普通成员本人单次持久退群明确成功；群主完整名单已无该目标，本人群目录也确认离群，并收到目标与本群精确匹配的1504通知。",
			"同轮邀请、本站接受和退群共三次且无重发；授权关闭，最终保持离群。其他角色、并发、未知结果恢复及生产业务未真实验收。", leaveEvidence}
		const partialEvidence = "docs/test-report.md#辣椒聊剩余原生能力受控验收2026-11-04"
		partial := func(id, at, result, remaining string) {
			out[id] = ControlledAcceptance{at, scope, result, remaining, partialEvidence}
		}
		partial("session_restore", "2026-11-03T22:40:50Z",
			"本轮API重启后群主使用既有密封会话重新连接，接收快照持续更新；未申请短信或重新进行原生登录。",
			"仅服务重启恢复；过期换票、跨部署交接和未知提交恢复未真实验收。")
		partial("edit_group_info", "2026-11-03T22:08:20Z",
			"本站群主单项修改群名称并恢复，两个持久操作明确成功，每次完整群资料回读均与预期一致。",
			"仅群名；群头像、其他角色和故障恢复未验收，生产准入保持关闭。")
		partial("set_member_card", "2026-11-03T22:10:01Z",
			"本站修改本人和另一自有普通成员群昵称并分别恢复，四个持久操作及逐项独立回读均一致。",
			"两个原子项独立留证；其他层级、同名竞态及故障恢复未验收。")
		partial("mute_group", "2026-11-03T22:27:01Z",
			"开启全群禁言后原生状态为3；修复状态枚举读取后，以剩余授权解除，现读恢复未禁言，没有重复开启。",
			"首次状态读取失败保留；其他角色、自动到期和未知结果恢复未验收。")
		partial("publish_notice", "2026-11-03T22:27:58Z",
			"本站设置临时公告并恢复原公告，两次持久回执成功，每次独立回读正文完全一致。",
			"仅单群文本公告；其他内容、并发变更及故障恢复未验收。")
		partial("list_friends", "2026-11-03T22:15:50Z",
			"本站只读取得当前群主两项好友记录，完整性明确，另一自有测试账号的好友关系已精确核对。",
			"仅当前小列表；大列表分页、好友审批及好友策略写入不包含在本次结果中。")
		partial("kick_member", "2026-11-03T22:30:18Z",
			"本站普通移出另一自有普通成员一次，持久回执成功，完整成员名单已无目标，原生禁入名单仍为空。",
			"此记录仅普通移出；永久禁入、解除及邀请恢复另有独立证据，其他角色和故障恢复未验收。")
		const followupEvidence = "docs/test-report.md#辣椒聊邀请恢复与永久禁入受控验收2026-11-04"
		const selfEvidence = "docs/test-report.md#辣椒聊本站本人接受邀请2026-11-04"
		out["read_invitations"] = ControlledAcceptance{"2026-11-04T10:21:32Z", scope,
			"本站读取受邀本人申请侧完整列表，唯一待处理记录精确匹配本群、本人及另一自有邀请人；与已核验的群主管理侧空列表分开核对。",
			"仅当前单群可见申请窗口；大列表、完整邀请历史及邀请归属计费未验收。", selfEvidence}
		out["invite_group"] = ControlledAcceptance{"2026-11-04T10:22:02Z", scope,
			"本站此前单次恢复邀请持久成功；本轮受邀本人从本站单次接受成功，双方完整名单和自身目录均确认入群为普通成员。",
			"发送与接受分别使用独立授权和预算；其他好友策略、并发及未知提交恢复未真实验收，生产准入未开启。", selfEvidence}
		out["read_group_bans"] = ControlledAcceptance{"2026-11-04T08:57:29Z", scope,
			"本站永久移出自有普通成员后，完整原生禁入名单精确包含该目标；单次解除后独立回读恢复完整空名单，成员仍在群外。",
			"仅单个自有目标的非空及空名单；大列表分页、并发变化和故障恢复未真实验收。", followupEvidence}
		out["remove_group_ban"] = ControlledAcceptance{"2026-11-04T08:57:29Z", scope,
			"本站对明确在原生禁入名单中的自有目标解除一次，持久回执成功；完整名单确认该目标已移除，未自动入群，随后另行授权邀请。",
			"与永久移出、邀请分别消耗预算；单群单目标，其他角色、并发及未知结果恢复未真实验收。", followupEvidence}
		return out
	}
	if platform != "wsl" {
		return out
	}
	const scope = "所有者自有账号、唯一隔离测试群；历史结果，不代表当前所选账号"
	const evidence = "docs/test-report.md#旺商聊完整能力受控核验2026-11-02"
	add := func(id, at, result, remaining string) {
		out[id] = ControlledAcceptance{at, scope, result, remaining, evidence}
	}
	add("mute_member", "2026-11-02T20:19:00+08:00",
		"群主对自有普通成员禁言一分钟成功，持久回执和即时禁言名单独立回读确认。",
		"本轮随后手动解禁；其他角色组合、精确到期时刻和故障恢复未验收。")
	add("unmute_member", "2026-11-02T20:19:00+08:00",
		"同一受控窗口内手动解禁成功，独立回读确认恢复未禁言。",
		"其他角色组合、未知结果和崩溃恢复未验收。")
	add("mute_group", "2026-11-02T20:19:00+08:00",
		"全群禁言及解除均有持久成功回执；每次回读群策略并恢复原状态。",
		"自动业务触发、权限变化和故障恢复未验收。")
	add("publish_notice", "2026-11-02T20:40:00+08:00",
		"单次发布测试公告成功；独立回读总数增加一条且正文唯一匹配，公告保留为测试记录。",
		"其他格式、自动业务触发和故障恢复未验收。")
	add("kick_member", "2026-11-02T20:42:00+08:00",
		"单次移出自有普通成员成功；执行方名单和目标方目录均确认离群，随后邀请并接受恢复普通成员。",
		"其他角色策略、真实未知结果与故障恢复未验收。")
	add("invite_group", "2026-11-02T20:42:00+08:00",
		"移出及本人退群后分别邀请自有好友；两次均由受邀者在独立窗口接受，双方名单确认入群。",
		"不自动接受；其他邀请策略、非好友和异常恢复未验收。")
	add("leave_group", "2026-11-02T20:42:00+08:00",
		"普通成员本人退出成功；双方核验离群，随后邀请并接受恢复为普通成员。",
		"群主退出不开放；其他策略、角色变化和未知结果恢复未验收。")
	add("edit_group_info", "2026-11-02T21:05:00+08:00",
		"群名称单项修改和恢复各一次，持久回执成功，独立群资料回读均精确一致。",
		"只验收群主单群改名；其他角色和故障恢复未验收。")
	add("set_member_card", "2026-11-02T21:05:00+08:00",
		"本人群昵称及自有普通成员群昵称分别修改、恢复，四次持久操作成功，每次独立回读一致。",
		"昵称锁、角色变化、自动名片及故障恢复未进行真实验收。")
	add("account_settings", "2026-11-02T21:08:00+08:00",
		"本人原生昵称修改并恢复成功；当前本人头像原图重新上传、检查、设置及新资源读取成功。",
		"头像仅同图规范化重传，不代表任意新图及原资源编号恢复；其他设置已退出产品范围。")
	add("list_members", "2026-11-02T21:05:00+08:00",
		"双方读取完整三人名单及身份映射；入退群回读与最终普通成员状态一致。",
		"大群缺页、并发变更和故障恢复未进行真实验收。")
	add("read_mutes", "2026-11-02T20:19:00+08:00",
		"完整禁言名单随成员禁言、解禁精确变化，群禁言策略也独立回读，未知列表未当作空列表。",
		"大群分页、到期边界和故障恢复未验收。")
	add("list_groups", "2026-11-02T20:42:00+08:00",
		"两账号唯一测试群目录与业务群/传输群映射一致；目标离群、重入均由双方目录核验。",
		"仅隔离群范围；多群规模、分页异常和恢复未真实验收。")
	add("list_friends", "2026-11-02T20:42:00+08:00",
		"两账号有界好友目录读取成功，自有互相好友关系明确，实际两次邀请可用。",
		"未增删好友；大列表分页、黑名单及异常恢复未真实验收。")
	add("read_invitations", "2026-11-02T20:42:00+08:00",
		"可见记录按授权测试群过滤；目标的两次唯一待处理邀请均能读取并分别接受。",
		"上游窗口不完整；不作为完整历史、邀请奖励或邀请归属计费依据。")
	add("read_mentions", "2026-11-02T20:22:00+08:00",
		"双方测试群留存消息各有一条成员提及投影，群归属和成员字段可读；没有执行发送。",
		"新鲜原生样本、完整UTF-16定位显示及本站发送另行验收。")
	const sampleEvidence = "docs/test-report.md#旺商聊新样本与双平台测试准备2026-11-03"
	sample := func(id, at, result, remaining string) {
		out[id] = ControlledAcceptance{at, scope, result, remaining, sampleEvidence}
	}
	sample("grant_admin", "2026-11-03T04:59:25+08:00",
		"单群、单个自有成员升权成功；持久原生确认与目标账号自身角色现读均为管理员。",
		"两次预算包含随后撤销；其他目标、并发撤权、未知结果与故障恢复未验收。")
	sample("revoke_admin", "2026-11-03T04:59:31+08:00",
		"同一窗口撤销管理员成功，目标账号自身角色现读恢复普通成员；窗口关闭，临时动作准入恢复关闭。",
		"无批量操作或重发；旧证明竞态和故障恢复未验收。")
	sample("read_roles", "2026-11-03T04:59:31+08:00",
		"群主完整名单确认初始普通成员；升权与撤权后均从目标账号当前连接现读管理员、普通成员身份。",
		"未覆盖其他角色组合、连接故障及旧证明竞态。")
	sample("read_images", "2026-11-03T04:45:50+08:00",
		"双方收到本轮新图片并解码到对应测试群；JPEG资源有界读取成功，响应禁止缓存。",
		"表情、其他格式及过期资源未在本轮真实验收；不代表本站发送通过。")
	sample("read_forward", "2026-11-03T04:45:50+08:00",
		"双方收到同一份新合并记录，均解码到测试群并投影三条记录，未截断。",
		"深层嵌套、混合资源、截断和页面展开仍分别验收。")
	sample("read_contact_cards", "2026-11-03T04:45:50+08:00",
		"双方收到新联系人名片，名片字段已投影且群归属一致；受限名片头像JPEG读取成功。",
		"页面交互、缺失头像及过期资源未在本轮独立验收；不执行加好友。")
	sample("read_avatars", "2026-11-03T04:45:50+08:00",
		"新样本的发送者与名片头像引用存在，两个作用域资源均读取成功且禁止缓存。",
		"目录头像、嵌套条目头像、过期资源和所有缺省显示未完全覆盖。")
	sample("read_video", "2026-11-03T04:45:50+08:00",
		"双方新视频投影一致；JPEG封面及约3.14MiB的MP4均通过16MiB有界资源接口读取。",
		"浏览器播放、其他编码、超限及过期资源仍待验收；不代表本站发送通过。")
	sample("read_recalls", "2026-11-03T04:45:50+08:00",
		"双方均收到新撤回通知，只读接口按测试群过滤，均匹配各自留存的原消息标识。",
		"页面展示、其他撤回角色及旧消息资源失效未完全验收；不改原消息和恢复水位。")
	const continuedEvidence = "docs/test-report.md#旺商聊继续受控核验2026-11-03"
	out["read_quote"] = ControlledAcceptance{
		"2026-11-03T15:01:05+08:00", scope,
		"两个接收器认证同一条视频引用回复；301原消息client/server标识精确匹配各自本群留存视频，双方回复投影摘要一致。页面按当前会话已加载原文显示视频引用，不自动跨会话补拉。",
		"仅视频引用读取；文本、其他媒体、原文过期及引用发送仍分别验收。",
		"docs/test-report.md#旺商聊移动端演示对齐核验2026-11-03"}
	continued := func(id, at, result, remaining string) {
		out[id] = ControlledAcceptance{at, scope, result, remaining, continuedEvidence}
	}
	continued("session_restore", "2026-11-03T14:05:58+08:00",
		"已有连接重建、同步和ACK证据；新增双账号在IM续期失败阻断后，各人工换票一次并密封保存，再恢复现有接收连接，双方均完成同步且队列为零，未新登录。",
		"原续期失败的上游根因未复现；失效业务会话、上游502、换设备、崩溃和全天长稳未验收。")
	continued("receive_message", "2026-11-03T06:28:01+08:00",
		"两次本人名片变更均产生新群通知；双方原生消息标识与完整投影摘要一致，正确归群、落盘、解码及ACK，当前解码积压为零。",
		"仅隔离群新通知及已有文本/媒体样本；不代表业务消费、全部格式、故障矩阵或多群容量通过。")
	continued("set_member_card", "2026-11-03T06:23:07+08:00",
		"此前群主本人及目标名片修改/恢复通过；新增普通成员本人修改与恢复，两次持久成功且独立回读精确一致，角色始终为普通成员。",
		"单窗口两次预算已关闭、临时动作准入恢复；昵称锁、其他层级、自动名片和故障恢复未验收。")
	continued("read_images", "2026-11-03T06:26:43+08:00",
		"双方新图片归群一致；真实页面解码1302×683图片，原图弹窗打开及关闭通过，有界JPEG资源读取成功。",
		"表情、其他格式及过期资源未真实验收；不代表本站发送通过。")
	continued("read_forward", "2026-11-03T06:26:43+08:00",
		"双方新合并记录均投影三条且未截断；真实页面展开聊天记录弹窗显示三条，关闭不产生提交。",
		"深层嵌套、混合资源和截断分别验收，不自动补拉其他会话。")
	continued("read_contact_cards", "2026-11-03T06:30:52+08:00",
		"真实页面显示新联系人名片的昵称、账号及有效头像；主体仅只读展示，没有加好友或其他动作按钮。",
		"缺失头像、过期资源及其他名片格式未真实验收；不执行加好友。")
	continued("read_avatars", "2026-11-03T06:30:52+08:00",
		"新样本的发言者与名片头像均在真实页面懒加载并解码成功，使用各自消息作用域资源。",
		"目录头像、嵌套条目头像、过期资源和所有缺省显示未完全覆盖。")
	continued("read_video", "2026-11-03T06:30:52+08:00",
		"真实页面显示720×1280封面；同一视频有界读取约3.14MiB，播放尺寸720×1280、时长12.37秒且播放时间实际推进。",
		"仅本样本浏览器编码；其他编码、超限及过期资源分别验收，不代表本站发送通过。")
	continued("read_recalls", "2026-11-03T06:03:49+08:00",
		"双方新撤回均匹配本群各自原消息；部署后真实页面撤回弹窗显示六条通知，无原生写入或脚本异常。",
		"其他撤回角色及旧消息资源失效未完全验收；不改原消息和恢复水位。")
	const sendsEvidence = "docs/test-report.md#旺商聊本站发送与撤回验收2026-11-03"
	sent := func(id, result, remaining string) {
		out[id] = ControlledAcceptance{"2026-11-03T10:32:27Z", scope, result, remaining, sendsEvidence}
	}
	sent("send_text",
		"本站自有普通成员在隔离群发送文本成功；持久回执为服务器接受，另一账号实际接收、解码且发送者和群归属一致。",
		"首轮网关角色冲突已修复并保留失败；仅单群受控发送，生产准入、备份恢复及旧nonce完整历史核对未通过。")
	sent("mention_member",
		"同轮文本明确提及自有群主；接收端成员标识和UTF-16位置与提交内容一致，不靠移动端演示替代本站执行。",
		"仅单个明确成员；多人、同名、改名竞态及生产业务未真实验收。")
	sent("quote_reply",
		"群主通过本站引用同轮成员文本发送成功；另一账号接收、解码，引用client标识精确匹配本群原消息。",
		"仅近期文本引用；媒体引用发送、原消息过期及异常恢复未验收，生产入口关闭。")
	sent("recall_message",
		"群主通过独立受控入口撤回同轮自有成员文本一次；持久原生确认成功，两账号最终均收到匹配原消息的撤回通知。",
		"通知晚于提交回执，首次立即查询未命中后仅只读核对，无重复撤回；其他角色、过期消息及生产准入未验收。")
	sent("send_image",
		"本站PNG上传与发送分别成功并消耗独立预算；另一账号实际解码为图片，原生消息标识、发送者和测试群归属一致。",
		"仅群图片单样本；私信、其他格式、未知结果和生产业务另行验收。")
	sent("send_media",
		"本站群MP3语音与带封面MP4各完成一次上传、持久发送及另一账号解码，分别为原生类型2和3；原子项独立保留证据。",
		"语音仅MP3样本，PCM WAV兼容性仍未通过；其他编码、超限、私信、故障恢复和生产准入未验收。")
	const privateScope = "所有者自有双账号、隔离测试群授权下的一对一私聊；历史结果，不代表当前所选账号"
	const privateEvidence = "docs/test-report.md#旺商聊本站私聊发送验收2026-11-03"
	out["send_private_text"] = ControlledAcceptance{
		"2026-11-03T12:02:05Z", privateScope,
		"本站向明确自有收件人发送一条私聊文本，持久回执确认服务器接受；收件账号实际解码为私聊文本，发送者、收件人及client标识精确匹配。",
		"仅单个自有好友；原生故障、不同好友策略、旧nonce完整历史及生产准入未验收。没有自动重发。",
		privateEvidence}
	out["send_private_image"] = ControlledAcceptance{
		"2026-11-03T12:02:05Z", privateScope,
		"本站PNG上传与私聊发送分别成功、独立计数；自有收件账号实际解码为私聊图片，发送者、收件人和client标识精确匹配。全轮三次预算用完，窗口关闭且临时动作准入恢复。",
		"仅PNG单样本；其他格式、过期资源、真实未知结果恢复及生产准入未验收，不继承群图片或另一平台结果。",
		privateEvidence}
	return out
}

// Child evidence describes the tested field rather than a different field's
// success from an aggregate parent.
func nativeDetailAcceptanceForPlatform(platform string) map[string]ControlledAcceptance {
	out := map[string]ControlledAcceptance{}
	if platform == "lajiaoliao" {
		const scope = "所有者自有辣椒聊账号与唯一隔离测试群；仅所列单项历史结果"
		const evidence = "docs/test-report.md#辣椒聊本站头像与八种发送验收2026-11-04"
		out["account_avatar"] = ControlledAcceptance{"2026-11-03T19:16:04Z", scope,
			"本人当前头像规范化重传，上传及最终设置明确成功；新JPEG经本站作用域读取3919字节，浏览器实际解码256×256，昵称未变。",
			"仅同图规范化重传；任意新图、原资源编号恢复和最终提交未知的真实恢复未验收，不代表昵称修改通过。", evidence}
		out["send_voice"] = ControlledAcceptance{"2026-11-03T19:27:07Z", scope,
			"本站MP3上传和持久发送分别成功；另一自有账号收到103语音，client标识、发送者和群精确核对。",
			"仅MP3单样本；WAV、移动端实际播放、其他编码及故障恢复未验收，生产入口关闭。", evidence}
		out["send_video"] = ControlledAcceptance{"2026-11-03T19:27:36Z", scope,
			"本站非分片MP4及JPEG封面上传、持久104发送分别成功；对端client标识和群一致，视频及封面经本站作用域资源读取成功。",
			"仅一秒短视频；其他编码、超限、移动端播放和故障恢复未验收，不继承语音结果。", evidence}
		const partialEvidence = "docs/test-report.md#辣椒聊剩余原生能力受控验收2026-11-04"
		out["set_self_card"] = ControlledAcceptance{"2026-11-03T22:09:12Z", scope,
			"本站修改本人群昵称并恢复，两个持久操作成功，各自独立成员回读一致。",
			"仅群主本人；其他角色、昵称限制和故障恢复未验收。", partialEvidence}
		out["set_other_card"] = ControlledAcceptance{"2026-11-03T22:10:01Z", scope,
			"本站群主修改另一自有普通成员群昵称并恢复，两个持久操作及逐项回读一致。",
			"仅该自有普通成员；其他层级和并发变更未验收。", partialEvidence}
		out["enable_group_mute"] = ControlledAcceptance{"2026-11-03T22:26:38Z", scope,
			"本站开启全群禁言一次，回执成功；修复读取后独立现读状态3并确认已禁言。",
			"首次读取错误保留，没有重复开启；发言被拒绝及其他角色未额外尝试。", partialEvidence}
		out["disable_group_mute"] = ControlledAcceptance{"2026-11-03T22:27:01Z", scope,
			"使用剩余授权解除全群禁言一次，持久回执和独立回读一致，最终群恢复未禁言。",
			"未续预算重复动作；故障恢复和生产业务未验收。", partialEvidence}
		out["kick_member_once"] = ControlledAcceptance{"2026-11-03T22:30:18Z", scope,
			"显式普通移出自有成员一次，完整名单确认离群，独立原生禁入名单确认未增加该目标。",
			"后续App接受与双方恢复入群另有独立证据；不继承永久移出的结果，其他角色和故障恢复未验收。", partialEvidence}
		const followupEvidence = "docs/test-report.md#辣椒聊邀请恢复与永久禁入受控验收2026-11-04"
		out["kick_member_permanent"] = ControlledAcceptance{"2026-11-04T08:56:24Z", scope,
			"本站群主显式永久移出另一自有普通成员一次，持久回执成功；完整成员名单无目标，完整原生禁入名单精确包含该目标。随后另一次授权解除已独立核验。",
			"仅单群单目标，未批量或重发；申请受拒、其他角色、并发变化及故障恢复未真实验收。", followupEvidence}
		out["invite_group_request"] = ControlledAcceptance{"2026-11-04T08:54:12Z", scope,
			"本站此前邀请自有好友的持久回执成功；所有者在App接受后，双方完整名单和目标自身目录确认普通成员。解除禁入后的恢复邀请也单次成功。",
			"邀请回执不等于已入群；本站本人接受独立留证，其他好友策略及未知提交恢复未真实验收。", followupEvidence}
		out["accept_group_invitation"] = ControlledAcceptance{"2026-11-04T10:22:02Z", scope,
			"本站受邀本人以独立300秒、预算1的持久授权接受现有唯一邀请一次；原生确认成功，双方完整名单和本人群目录均确认恢复普通成员。",
			"窗口已取消且临时测试和动作开关恢复；没有重新邀请、批量或自动重发。其他邀请策略、并发及未知结果恢复未真实验收，生产入口关闭。",
			"docs/test-report.md#辣椒聊本站本人接受邀请2026-11-04"}
		return out
	}
	if platform != "wsl" {
		return out
	}
	const evidence = "docs/test-report.md#旺商聊完整能力受控核验2026-11-02"
	const scope = "所有者自有账号与隔离测试群；仅所列单项历史结果"
	out["account_nickname"] = ControlledAcceptance{
		"2026-11-02T20:24:00+08:00", scope, "本人原生昵称修改、恢复均明确成功，独立回读一致。",
		"不代表头像或其他设置；其他角色与未知结果恢复未验收。", evidence}
	out["account_avatar"] = ControlledAcceptance{
		"2026-11-02T21:08:00+08:00", scope, "当前本人头像原图规范化重传，上传、检查、设置成功，新资源可读取且昵称未变。",
		"仅同图重传；未验证任意新图及原资源编号恢复，异常恢复未真实验收。", evidence}
	out["set_self_card"] = ControlledAcceptance{
		"2026-11-03T06:23:07+08:00", scope, "群主本人及普通成员本人分别修改、恢复群昵称，持久回执成功且独立回读一致。",
		"昵称锁、角色变化和故障恢复未真实验收。",
		"docs/test-report.md#旺商聊继续受控核验2026-11-03"}
	out["set_other_card"] = ControlledAcceptance{
		"2026-11-02T21:05:00+08:00", scope, "群主对自有普通成员群昵称单项修改、恢复各一次，独立回读一致。",
		"其他层级、昵称锁及故障恢复未真实验收。", evidence}
	out["privacy_calls"] = ControlledAcceptance{
		"2026-11-01T17:43:02+08:00", "所有者自有账号；排除项的历史证据，不重新纳入产品",
		"本人通话开关修改和恢复曾独立回读通过。", "真实呼叫未验收；本轮未操作该已排除设置。",
		"docs/test-report.md#双平台样本核对与旺商聊设置验收2026-11-01"}
	const sends = "docs/test-report.md#旺商聊本站发送与撤回验收2026-11-03"
	out["send_voice"] = ControlledAcceptance{
		"2026-11-03T10:32:27Z", scope,
		"本站MP3短语音上传、持久发送及另一账号解码成功，类型2和语音时长存在，上传回执独立检查一致。",
		"不继承视频结果；PCM WAV上游兼容性、移动端实际播放及其他编码未验收，生产入口关闭。", sends}
	out["send_video"] = ControlledAcceptance{
		"2026-11-03T10:32:27Z", scope,
		"本站一秒非分片MP4及封面上传、持久发送和另一账号解码成功，类型3及视频元数据一致。",
		"不继承语音结果；其他编码、超限、移动端播放及故障恢复未验收，生产入口关闭。", sends}
	return out
}
