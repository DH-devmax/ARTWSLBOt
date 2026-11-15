# 辣椒聊本站接受本人邀请

## 固定契约与缺口修复

2026-11-04，Asia/Shanghai。固定桌面1.0.3：
`App-24552bef.js` SHA256
`8b4f4aa542461c7878513b40dadab0896ab9dceadba12d910e61ea6207b32676`，
随包 `@openim-f80223cd.js` SHA256
`ea45ea3a94e10e196a4313a17d701a706ed129401464987de6b7d8cf12135f15`，
`openIM.wasm` SHA256
`94d7ef0f64b1a42564f2d29452fa982b0abe098618b4e08ecfad78ee1f695640`。
这些本地固定资料不包含本项目账号凭据或运行消息。

桌面通知页将 `joinSource=2`、存在非本人 `inviterUserID`、
`userID` 为当前账号的记录显示为本人收到的邀请。其确认调用
`acceptGroupApplication({groupID,fromUserID:record.userID,handleMsg})`，
与管理者审批共用 SDK 方法，并非仅供管理者使用。
此前文档把共用方法一概解释为管理者审批，造成本站本人接受入口缺失，现纠正。

随包 WASM 的固定路由和 protobuf 描述符分别为：

- `/group/get_user_req_group_applicationList`：
  `GetUserReqApplicationListReq` 的 `userID/groupIDs/pagination/handleResults`。
  本站仅读取当前账号、选定群，每页100项，最多100页。
- `/group/group_application_response`：
  `GroupApplicationResponseReq` 的 `groupID/fromUserID/handledMsg/handleResult`。
  本人确认使用当前账号、空说明和 `handleResult=1`。
  注意 HTTP 字段为 `handledMsg`，不能直接复制 SDK 的 `handleMsg` 字段名。
- `sdkws.GroupRequest` 独立包含 `joinSource/inviterUserID/reqTime/handleResult`。
  读取时只保留匹配群和本人、来源为邀请的安全字段。

2026-11-04本轮只读接口核查得到一条完整待处理邀请：
本人、选定测试群、来源2、另一自有账号作为邀请人、结果0均匹配。
只保存结构、数量和身份匹配布尔值，不公开账号ID、昵称正文或票据。
该读取不是本站接受成功的证据。

首次本站只读验证发现当前`reqTime`为毫秒，按秒构造会产生超出JSON时间范围的值。
已改为`time.UnixMilli`，指纹仍保留原始整数；超出可序列化时间范围的记录拒绝。
补充毫秒精度、越界及完整投影JSON回归。本次发现发生在提交前，没有发出接受请求。

## 控制与界面

复用既有 `group_accept` 持久类型，无迁移。邀请选择使用群、本人、邀请人、
原生申请时间构成的SHA256指纹，不套用旺商聊数字记录ID。
上游写入仅按群和用户定位，因此同群本人存在多条待处理邀请时拒绝提交。
已处理、换群、换人、重新发出的邀请、未知或不完整列表均不能沿用旧确认。

超管授权只允许单独的 `group_accept`，目标为受邀账号本人，最多300秒及显式次数预算。
创建窗口时重新检查活跃租户群绑定、已认证账号、连接、租约、
账号epoch/version及凭据摘要。不伪造尚未入群账号的角色，
辣椒聊不读取旺商聊部署版本。提交前再次读取本人邀请并通过既有最终栅栏。
账号换票、撤权、到期、取消或预算耗尽均拒绝新提交。

前端复用现有受控窗口弹窗、本人邀请选择和确认操作；关闭弹窗不提交。
本站确认只表示服务端确认接受，随后以本人群目录和双方完整成员名单确认入群。
超时或结果未知只查询、核对原操作，不重发。工具不加入群管助手或生产业务准入。

## 交付与验收

候选从运行v45冻结源码派生，只加入本轮邀请读取、持久控制、现有UI入口、
合成回归及相关文档；不夹带未验收的解析、轻量化或媒体候选。
真实接受仅限已有自有账号和唯一测试群，在完成发布检查后独立进行。
实现、合成验证、本站真实执行、目录证据及发布结果见 `docs/test-report.md`
“辣椒聊本站本人接受邀请”条目。`native_ready/ai_ready` 保持false。

2026-11-04 18:21:45 Asia/Shanghai，受邀本人通过本站`run_social/group_accept`
持久操作`1bf4c147-34f5-4569-8790-f380b30a558c`单次接受成功；
18:22:02分别读取双方完整成员名单及本人群目录，均确认已入群、目标为普通成员。
沿用此前恢复邀请，没有重新邀请。300秒窗口预算1已使用1次，18:22:28取消授权，
临时测试及动作开关恢复原值。真实接受通过，生产准入与其他恢复场景不随之开启。
