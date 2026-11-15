# 双平台原生范围

所有者2026-11-01调整：设置二级安全码、实名认证暂缓接入。保留
`secondary_security_code`、`legal_identity`及历史代码，目录状态为`deferred`，
从本轮独立验收统计中排除；关闭设置、安全状态读取和前端操作入口。
不修改已应用迁移、已有授权或原生账号资料。

## 辣椒聊独立核实

`internal/features/platform.go`和`native_details.go`独立登记辣椒聊能力及子项，
不再遍历旺商聊目录自动补齐。已有ID作为兼容条目保留；尚无本平台证据的
条目为`support=review / integration=pending`，不预设功能存在或角色权限。
新的旺商聊能力不会自动进入辣椒聊目录。总数由生成目录统计。

| 范围 | 当前辣椒聊依据 | 与旺商聊区分 |
| --- | --- | --- |
| 注册 | `internal/lajiaoliao/signup.go`：purpose=3短信、`login_or_signup`，手机号、验证码和设备信息 | 不套用旺商聊昵称、密码字段或业务1001诊断 |
| 接收与恢复 | `internal/lajiaoliao/transport.go`、`internal/gateway/lajiaoliao.go` | OpenIM增量序列和持久游标，不显示为旺商聊ACK或恢复水位 |
| 文本发送 | `internal/lajiaoliao/transport.go SendText`：101、MsgFrom100、SessionType3 | 有协议原语，统一持久控制和真实发送待验收 |
| 成员与角色 | `internal/lajiaoliao/directory.go`：原生字符串ID，roleLevel20/60/100 | 不将字符串ID转换为旺商聊数字ID；成员快照不授予SaaS群权限 |
| 成员禁言与解禁 | `internal/lajiaoliao/native.go`：管理员操作普通成员，群主可操作管理员；独立mute/cancel路由 | 持久控制及受控Tools已接入；真实执行独立验收 |
| 引用、@、合并记录 | 所有者App产生114/106/107真实测试群样本，见`lajiaoliao-message-evidence.md` | 平台支持已确认，结构化读取及发送分别适配 |
| 撤回通知 | 2101/MsgFrom200及detail字符串，`internal/lajiaoliao/recall.go` | 测试群留存输入解析和群匹配通过；弹窗展示及撤回提交分别验收 |
| 管理员授撤、全群禁言、公告、入退群、好友、昵称和隐私 | 固定桌面1.0.3及随包SDK，见`lajiaoliao-native.md` | 已接独立契约和持久控制；不继承旺商聊验收 |
| 引用、@、合并记录读取 | `internal/lajiaoliao/message.go`及消息页有界读取 | 逐账号、会话与原投影匹配；不改解码状态或恢复水位 |
| 发送、撤回提交、媒体、头像、改密及换绑 | 尚缺完整持久执行或资源契约 | 保持待接入；原生类型识别不算资源预览或动作通过 |

以上“依据”是当前源码和已留存样本检查。适配器实现、真实输入读取、受控
动作生效及生产业务完整验收分别记录在`test-report.md`。

## 接下来如何核实

先核对辣椒聊固定客户端/SDK的格式、路由、字段和角色规则，再接入独立
适配器；自动回归使用合成样本。已有自有测试群中的留存包可只读核实，
真实发送、撤回及管理修改另按明确范围和持久授权执行。
功能表、子项和业务依赖更新后运行`go run ./tools/feature_catalog -write`，
通过同步测试；每个平台记录自己的证据与验收。
