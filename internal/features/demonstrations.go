package features

// NativeDemonstrationsForPlatform records inspected official-client samples.
// It is separate from controlled website execution and cannot admit an action.
func NativeDemonstrationsForPlatform(platform string) map[string]ControlledAcceptance {
	out := map[string]ControlledAcceptance{}
	if platform == "lajiaoliao" {
		const scope = "所有者移动端操作、辣椒聊唯一隔离测试群、两个自有账号接收；不是本站发出"
		const evidence = "docs/lajiaoliao-membership-20261104.md"
		add := func(id, result string) {
			out[id] = ControlledAcceptance{"2026-11-04T05:08:27+08:00", scope, result,
				"仅App演示接收及有界展示；本站持久写入、恢复和生产准入分别验收。", evidence}
		}
		add("read_group_events", "双账号留存群资料、移出、加入、成员禁言及解禁、全群禁言及解除、成员资料、公告和取消置顶通知，逐条按原群核对。")
		add("kick_member", "双账号收到1508成员被移出通知，目标与当前测试群一致；通知没有permanentBan字段，不能判定永久禁入。")
		add("mute_group", "双账号分别收到1514开启及1515解除全群禁言通知，不继承成员禁言或其他平台执行结果。")
		add("publish_notice", "双账号收到1519群公告更新通知，当前群及操作者结构有效；1502完整快照不用于推断具体改动。")
		return out
	}
	if platform != "wsl" {
		return out
	}
	const scope = "所有者移动端操作、旺商聊唯一测试群、两个既有认证账号接收；不是本站发出"
	const evidence = "docs/native-mobile-demonstrations-20261103.md"
	add := func(id, at, result, remaining string) {
		out[id] = ControlledAcceptance{at, scope, result, remaining, evidence}
	}
	add("send_text", "2026-11-03T15:02:15+08:00",
		"两条新群文本均由两个接收器认证解码；消息身份、测试群归属及双方投影摘要一致。",
		"本站持久发送、nonce历史准入和对端回显仍须独立验收。")
	add("quote_reply", "2026-11-03T15:01:05+08:00",
		"真实回复携带301引用及原消息client/server标识；双方精确匹配本群留存视频，回复投影一致。",
		"本站引用发送及其持久授权未验收；不将移动端动作计入本站执行通过。")
	add("mention_member", "2026-11-03T15:02:03+08:00",
		"新消息携带105提及结构；双方UID及UTF-16区间有效，完整投影摘要一致。",
		"未操作被提及成员；本站生成提及、发送及回显仍需独立验收。")
	add("send_image", "2026-11-03T15:00:58+08:00",
		"新图片双方归群、摘要一致；资源标识有效，尺寸1302×683，大小298238字节。",
		"本站上传与发送分别授权及其真实回显未验收。")
	add("send_voice", "2026-11-03T15:01:30+08:00",
		"新语音双方认证解码及摘要一致；102结构、有效资源标识及3秒时长可读。",
		"此证据不包含语音播放或本站上传、发送；不从其他媒体继承通过。")
	add("send_video", "2026-11-03T15:01:20+08:00",
		"新视频双方认证解码及摘要一致；103结构、720×1280、12秒、3289201字节及资源标识有效。",
		"本站上传与发送、超限和未知结果恢复仍需独立验收。")
	add("recall_message", "2026-11-03T15:01:49+08:00",
		"新撤回群通知已持久接收并由测试群只读接口返回，未重放原消息。",
		"只证明移动端撤回通知读取；本站受控撤回持久执行适配及真实动作仍未验收。")
	out["send_private_text"] = ControlledAcceptance{
		"2026-11-03T15:00:50+08:00", "所有者移动端演示、既有认证账号的同期私聊；仅本端接收，不是本站发出",
		"同期私聊带引用文本认证解码成功，12字节正文、301引用标识有效，归属当前账号私聊；未输出正文或对方身份。",
		"本站持久私聊发送、nonce历史准入及对端回显仍须独立验收；此证据不代表引用原文已匹配。",
		evidence,
	}
	out["send_private_image"] = ControlledAcceptance{
		"2026-11-03T15:00:27+08:00", "所有者移动端演示、既有认证账号的同期私聊；仅本端接收，不是本站发出",
		"同期私聊图片认证解码成功，尺寸1302×683、298238字节及有效资源标识；未读取对方其他私聊或下载资源。",
		"本站私聊上传、持久发送及对端回显仍须独立验收，不从群图片继承执行结果。",
		evidence,
	}
	return out
}
