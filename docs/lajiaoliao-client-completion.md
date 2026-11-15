# 辣椒聊客户端核对与消息补齐

## 固定资料

2026-11-03，本地只读解包，不运行客户端或执行注册、短信、登录。

| 资料 | 版本 / SHA256 |
| --- | --- |
| Android `com.lajiao.chat` | 1.7.3 / versionCode 20 |
| 原 APK | `7a635f491932f8b5abd70305f8365cf838ee0e9fcafa5bfb863c22bbe2aee289` |
| Flutter `libapp.so` | `95a3cfaac78baf603f4857f5a68b31716f031c3e709d349e1ecda9f97fc60ebc` |
| 随包 `libgojni.so` | `865ee4a0e9bab62f918e154ea00748d59e49822912e2ad4323c879b23672fcad` |
| 桌面 App | 1.0.3，随包 OpenIM 3.8.3 包装层 |
| `App-24552bef.js` | `8b4f4aa542461c7878513b40dadab0896ab9dceadba12d910e61ea6207b32676` |
| `@openim-f80223cd.js` | `ea45ea3a94e10e196a4313a17d701a706ed129401464987de6b7d8cf12135f15` |

APK 位于本地私有研究缓存，解包输出在 `var/chili-completion-20261103/apk`。
不把安装包、反编译全文或真实消息发送给架构分析模型。

## 本轮契约与实现

| 能力 | 客户端依据 | 本站接线 |
| --- | --- | --- |
| 图片 | 类型102；`sourcePicture`、`bigPicture`、`snapshotPicture` 的 url/size/width/height；桌面 `bW` | 七天内原始密封事件按需读取，图片不进入接收或 ACK 路径 |
| 视频 | 类型104；`videoElem.videoUrl`、`snapshotUrl`、大小、时长；桌面 `yW` | 封面独立读取，16MiB内点击播放，复用既有视频组件 |
| 消息头像 | `senderFaceUrl` | 只返回本站账号/会话/事件绑定地址 |
| 联系人名片 | 类型108；`createCardMessage` 的 userID/nickname/faceURL/ex | 字符串ID、昵称、头像展示；不透明ex不返回，不添加好友 |
| 引用、合并记录媒体 | 114 `quoteMessage`、107 `multiMessage` 的对应元素 | 深度3、总32项，按同一个先序编号生成及解析资源引用 |
| 群文本 | `createTextMessage` / `sendMessage`、原WebSocket1003及回执 | 显式超管测试授权；原连接发送，持久操作ID作为clientMsgID，不增加服务或worker |
| 撤回 | 桌面 `revokeMessage`；Android Go SDK `/msg/revoke_msg` | 独立受控入口接到已有持久链路，使用conversationID/seq与两分钟内可信消息 |
| 群名 | `set_group_info_ex` 的 `groupName` | 补齐原生调试授权及受控面板；保留现有控制校验和单群状态回读 |

资源仅允许已在测试群实际观测到的固定HTTPS来源
`lj-im-chat-01.lajiaoliao.com`，嵌套旧图片另允许官方 `im-api.lajiaoliao.com`，
后者不用于视频。不接受浏览器URL、Cookie或原生凭据，
不跟随重定向，不缓存明文、不自动下载、不新增自动重试。
读取后重新校验网站权限、账号epoch/version、原始事件与投影、七天保留期。
两平台共用两个有界媒体槽；超额立即拒绝。

群文本及撤回复用 `native_setting_operations`，不新增迁移。测试入口不向助手
提供授权，也不开放生产业务准入。超时、断线和未知结果保留原操作，禁止重发；
只有确切的留存回显或通知才能作为实际效果证据。
监控采样、解码领取、解析算法及运行 `CodecVersion=7` 均不因本轮改动而改变。

## 不能混淆的原生语义

`acceptGroupApplication(groupID, fromUserID, handleMsg)` 对应
`/group/group_application_response`，由管理者审批和受邀本人接受共用。
2026-11-04继续核对固定桌面通知页，已确认本人邀请使用来源2、非本人邀请人、
当前账号作为申请者；此前把该方法仅解释为管理者审批不准确。
本站本人接受的独立实现和验收见 `lajiaoliao-self-invitation-20261104.md`，
不能仅凭方法名或App操作认定本站通过。
群主、管理员、普通成员仍分别按辣椒聊的100/60/20核验，
不继承旺商聊ID、nonce、权限或通过记录。

引用/@发送、群及私聊图片上传发送、群语音/视频发送、私聊文本仍需要
各自的持久执行适配或真实验收；移动端演示只能补格式及实际行为证据。
已存在的未验收媒体发送候选不随本轮只读资源及文本/撤回补齐一起发布。

## 验收记录

本轮实现、自动回归、真实读取/发送及部署结果分别登记在 `docs/test-report.md`。
功能表支持、实现、受控验收与生产准入分开；合成夹具不产生真实通过记录，
`native_ready`、`ai_ready`保持false。

