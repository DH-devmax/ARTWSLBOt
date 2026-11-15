# 文档导航

更新：2026-11-06。先看当前约定，再看专题实现和对应日期的验收。历史报告不是当前运行状态。

## 开发入口

| 文档                                                 | 用途                                                  |
| ---------------------------------------------------- | ----------------------------------------------------- |
| [项目说明](../README.md)                             | 产品定位、部署入口及安全默认值                        |
| [架构契约](architecture.md)                          | 所有权、依赖方向、连接与租户隔离                      |
| [实施状态](implementation.md)                        | 已有实现、未发布候选与未闭环能力                      |
| [业务模块开发](business-module-development.md)       | 如何复用原生底座实现业务，配置、执行、测试和交付要求  |
| [功能目录](feature-catalog.md)                       | 从 `internal/features` 生成的平台独立能力、依赖及验收 |
| [功能中心](feature-center.md)                        | 作用域弹窗、独立草稿、保存与实际启用的区别            |
| [权限](permissions.md)                               | 网站用户、租户、套餐与群授权的交集                    |
| [工作区与演示权益](operator-demo-workspaces.md)       | 管理后台、用户中心、演示计费与页面单次操作            |
| [网站滑块验证](website-captcha.md)                   | 登录、机器人接入、一次性验证票据、CLI与发布状态       |
| [工作台界面](ui-reference-20261106.md)                | Lumenwave参考、样式入口、导航及界面交付边界            |
| [多平台](multi-platform.md)                          | 平台、账号用途与群绑定边界                            |
| [群管助手](management-assistant.md)                  | 对话查询、建议、本人确认与隔离                        |
| [助手与网关隔离](assistant-isolation-and-gateway.md) | AI 工具、预算、费用及控制层边界                       |
| [热群迁移](maibot-migration.md)                      | Go 引擎及可选扩展的真实迁移状态                       |
| [缓存与基础聊天](cache-and-basic-chat.md)            | 四分区缓存、聊天任务与发送栅栏、实际运行状态和启用条件 |

## 原生接入

| 文档                                                                                             | 用途                                         |
| ------------------------------------------------------------------------------------------------ | -------------------------------------------- |
| [原生底座](native-foundation.md)                                                                 | 协议接入和持久动作的基本约定                 |
| [消费者边界](native-consumer-boundaries-20261103.md)                                             | 群管底座、人工账号操作及不接入项             |
| [受控 Tools](controlled-tools.md)                                                                | 超管调试授权、期限、预算与未知结果           |
| [旺商聊认证](native-auth.md)                                                                     | 登录、会话及诊断，不能用普通回归触发真实认证 |
| [原生接收](native-receiver.md)                                                                   | 落盘、ACK、水位、隔离和恢复                  |
| [会话与消息](native-conversations.md)                                                            | 有界读取与消息投影                           |
| [资料设置](native-settings.md) / [社交](native-social.md)                                        | 平台契约与操作边界                           |
| [媒体](native-media.md) / [撤回](native-recalls.md)                                              | 持久上传、提交、回显与撤回通知               |
| [辣椒聊](lajiaoliao-native.md)                                                                   | 独立平台身份、SDK 与协议约定                 |
| [辣椒聊入群](lajiaoliao-self-invitation-20261104.md) / [退群](lajiaoliao-self-leave-20261104.md) | 对应场景的实现和真实受控记录                 |

## 运维与质量

| 文档                                                                    | 用途                                        |
| ----------------------------------------------------------------------- | ------------------------------------------- |
| [Docker 运维](docker-operations.md) / [开发模式](docker-development.md) | 唯一 Compose 路径、资源、迁移与回滚         |
| [工作区维护](workspace-maintenance.md)                                  | 临时产物清理、可校验归档、保留与恢复        |
| [项目监控](project-monitor-20261103.md)                                 | API、热群引擎、本库、Web 的有限监控范围     |
| [架构工具](architecture/README.md)                                      | 代码证据、守卫及匹配可信基线的 CodeBoarding |
| [测试报告](test-report.md)                                              | 实测、失败、未达项与部署证据，按日期追溯    |
| [续接检查点](project-checkpoint.md)                                     | 最新断点在前，旧断点仅供追溯                |
| [本轮性能复核](native-performance-review-20261104.md)                   | 当前工作区的合成解析复测及运行版瓶颈判断    |
| [解码候选](decoder-optimization.md)                                     | 早期候选算法与计时边界，不是当前部署清单    |
| [轻量化历史验收](native-lightweight-acceptance-20261102.md)             | 旧冻结版本的矩阵与限制，不继承到新版本      |

## 维护原则

- 功能支持、代码接入、指定场景通过、生产业务准入分别维护；不能用一项代替另一项。
- 能力或验收变化先改 `internal/features`，再运行 `go run ./tools/feature_catalog -write`；不要手填能力总数。
- 新结果追加到测试报告，保留失败记录；日期专题文档不得悄悄改成新版本的验收结论。
- 修改模块边界先更新架构证据。咨询分析、代码事实检查、CI、合成性能与真实原生验收分开报告。
- 不把密钥、原始票据、手机号、用户正文或分析私有缓存写进文档；历史归档不作为发布输入自动恢复。
