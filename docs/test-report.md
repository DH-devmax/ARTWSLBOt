# 首批基础版本测试记录

## CI 推送与阻塞修复（2026-11-15）

用户要求将本轮工作流推送并以 GitHub Actions 实际结果验收。独立分支
`codex/ci-release-validation` 基于 `origin/main` 的
`6f96f29d256c103b8644c1c4b082f529b67b5a40` 整理，避免修改原工作区及其未发布候选。
远端缺少当前运行版的大量产品源码，因此先同步已部署 v134-r4 的公开冻结文件，
来源清单为 `4bd29ec3b349391720d47a94e39cf059bc08f9ee159d8ec94c60586b4b59e2c6`。
运行凭据、现场输出、分析缓存、临时脚本和未发布解析候选不进入提交。

已推送 PR #28，首个提交为 `8f31f62ab25b420114d9cb260e3a7c05bc7ed1a6`；
GitHub 运行 `43672037664` 检查 PR 合并版本
`52cbb021c612fee09dbbf8f6cce9ba72da295399`。首轮尚未通过：unit、architecture
成功，quality、frontend、images、security 失败，下游数据库、浏览器及覆盖率未执行。
失败不能用本地通过或上游作业成功代替。

首轮 Go race/sodium 在 Linux/amd64、Go 1.27.4 下发现并核对 1050 个父测试：
893 通过、157 个显式批准跳过，3557 个子测试，无失败或缺失。
架构源码事实、目录同步及 Mermaid 也已在该运行通过。
台账弹窗已修复加载、关闭取消及作用域隔离，定向浏览器回归通过；
缺失的开发容器文件已补齐。

后续修复将 Python 固定为受锁定 CodeBoarding 支持的 3.12，并按同一解释器
复核两个既有可选 APK 测试的 AST 指纹；不新增跳过。Node 22 的单文件 JUnit
不提供 file 属性，改由严格绑定的单文件调用补充来源，外来 file 与重复结果仍失败。
Go 与 JS/TS CodeQL 分别使用 manual 和 none 构建模式，仍要求两种完整 SARIF。
镜像归档同时核验 Docker 配置和新版 containerd/OCI 索引，保留来源、内容哈希、
平台及加载后身份校验。数据库按完整父项清单固定分批串行执行，每批仍限15分钟，
同包批次全部通过后才聚合为既有三个覆盖率制品，不跳过累计超时后的测试。

数据库定向复核确认旧 HTTP 全包900.463秒耗尽累计期限时仍在推进测试，
未复现目标事务死锁。新依赖下目标 HTTP、所有权和 control 事务定向回归通过；
gateway 140项通过、2项既有性能跳过。完整新 runner 结果仍需等待 GitHub，
定向结果不算整套数据库通过。

新增安全扫描实际检出了 Go 1.27.0 和旧依赖的可达漏洞，因此将 Go 更新为
1.27.4，并更新 pgx、jsonparser、x/crypto、x/image 及其必要依赖；不增加安全豁免。
本节只记录当前分支结果，下方较早的失败记录保留，不算本次通过证据。
后续完整 GitHub 结果以同一 PR 当前提交的 `foundation` 及各作业收据核验。
生产 Compose 未重启，readiness 未变。

## GitHub Actions 检查与镜像发布改造（2026-11-15，北京时间）

本轮范围为CI配置、检查工具、合成浏览器夹具和开发文档，不修改业务运行代码、
解析算法、原生权限、数据库迁移或生产Compose。实施说明见[CI开发与发布文档](ci.md)。
实施时HEAD为`f6692ec09b9f9ed9e9416aee21b09522c716371f`，
工作区同时含大量既有未提交候选；下列本地结果不能作为该GitHub提交的CI通过记录。

### 已实现

- 保留主分支保护要求的`foundation`名称，聚合quality、unit、database、frontend、
  images、browser、security、architecture和coverage九个作业。失败、取消、跳过、
  缺失或过期的报告均阻断；本轮只读查询确认远端主分支必需检查为`foundation`。
  仓库端已建立`release`环境，限制为`main`分支及`v*`标签；
  配置回读证据为`output/ci-local/github-settings.json`，未触发工作流或发布。
- Go测试先建立完整父项清单，再核对执行结果、子项及精确源码指纹的跳过策略；
  数据库仅允许`wsl_test`，control、httpapi和gateway串行执行。
  atomic覆盖率绑定SHA、完整源码指纹、Go版本、平台及race/sodium设置，严格校验后合并。
- 显式登记Python、Node、合成浏览器和组件用例；漏登记、重复、未知skip和缺少结果失败。
  两个需要固定官方APK的Python用例仅作有源码指纹的可选项，不自动下载或使用真实凭据。
- 加入产品ESLint、格式、Staticcheck、Ruff、govulncheck及实际CGO/sodium提取的CodeQL；
  32项历史静态诊断按文件、规则、代码指纹和次数登记，不容纳新CI工具、编译或扫描失败。
  Actions、独立浏览器依赖及检查工具锁定来源和完整性。
- 每SHA只构建一次API/Web镜像，同一归档用于数据库、浏览器、容器检查及受保护发布。
  发布核对九项收据、镜像组件、归档哈希、配置ID和源码标签，不重新构建。
  两个负向回归发现并修复了“缺少其他作业收据”和“API元数据替换为Web”的校验缺口。
- 源码架构检查无模型凭据，过期时仍保留候选差异及图；付费CodeBoarding仅保留
  默认分支的受保护手动入口。初始化仍需专项授权，缺少匹配基线不回退全量。
- 只上传安全结构化报告、覆盖率、SARIF、源码架构证据及合成截图；
  不上传环境文件、网站会话、原始进程日志、页面正文或浏览器trace。

### 本地实测

本地Python为3.13.15、Node为24.19、Go为1.27.0；GitHub工作流选用Python3.13、
Node22及`go.mod`指定的Go。不同环境的结果不混作同一次运行。

| 检查 | 结果与证据 |
| --- | --- |
| 新CI工具合成回归 | 157项PASS，含缺失/重复结果、取消、跳过指纹、覆盖率篡改、本地替换依赖变更、发布收据、组件替换及安全故障分类 |
| 显式Node回归 | 5个登记文件、21个独立用例PASS，含浏览器取消及资源清理；`output/ci-local/node-browser-cancellation-final/report.json` |
| 完整Python登记集 | 260通过、9失败、2个已批准可选APK跳过；`output/ci-local/python-diagnostics-final/report.json` |
| 产品前端 | `tsconfig.wsl.json`类型检查与Vite构建PASS；2438模块，保留既有大包提示 |
| 合成浏览器 | 48项全量原轮为45通过、3失败；注册契约及受控接入夹具修复后各自定向PASS，财务台账缺陷仍失败。未改写原轮，也未宣称48项全量重新通过 |
| 浏览器取消 | SIGINT页面、SIGTERM组件均停止后续用例，浏览器／上下文／子进程清理完成，报告按预期为失败终态；`output/ci/browser/cancel-page-final/report.json`及`cancel-child-final/report.json` |
| Actionlint／仓库守卫／架构边界守卫／功能目录同步 | 均PASS |
| Gitleaks完整已提交历史 | 扫描28个提交，PASS；初轮18处均为架构清单SHA-256误报。固定扫描器另以3个独立合成Git仓库验证：仅精确路径中的哈希免报，普通文件中的同值密钥及清单中的凭据格式均被检出；`output/ci-local/gitleaks-git-policy-final.json` |
| Lint整套检查 | 32项精确历史诊断、新增0、已修复未清基线0；Staticcheck和govulncheck受根工作区编译缺口阻断，不能判为安全扫描通过 |
| 源码架构候选与Mermaid | 候选提取PASS，9个视图渲染PASS；已提交快照检查失败，详见下方 |
| 真实Docker合成镜像 | 构建、归档、来源/配置ID校验、加载PASS；`output/ci-local/image-fixture/report.json`。仅用无业务内容的scratch夹具，不等同产品镜像smoke |
| CodeBoarding当前preflight | `compatible_baseline_missing`，0请求、0保留token；未初始化、未恢复无关基线、未调用模型 |

新Go runner另外使用已部署v134-r4冻结源码重新执行，并将结果持久保存：
`output/ci-local/frozen-r4-unit-deps-final/report.json`、`unit.cover`及`unit.cover.meta.json`。
25个包共1046项父测试全部有结果：889通过、157按显式策略跳过；
3557项子测试、0失败。此轮为Darwin/arm64、Go1.27.0、race/sodium、atomic，
源码清单含772个文件，`source_digest`为
`dc0a6c0d4c0df61acff79187e34004156d6819bc52bdb958196381fa23ce6a37`，
覆盖率文件SHA256为
`7783b8ff0122a0130fe9afe53dc294dc0bfec1767dab612daf5742fe10e0bafc`。
冻结目录继承父仓库HEAD，但其源码清单与根工作区不同，不能拿来替代根工作区或GitHub验收。
Linux目标的实际依赖发现另核对了21个`third_party/gopsutil`编译来源及模块锁；
Darwin适配不导入该Linux采集依赖，因而该平台来源清单不增加这些文件。

同一冻结Go源码另在禁用网络、只读源码、2CPU／1536MiB的Linux/arm64容器完成
race/sodium、atomic单测：1048项父测试全部有结果，891通过、157项显式跳过，
3557项子测试、0失败、0缺失。报告与覆盖率保存在
`output/ci-local/frozen-r4-linux-unit-docs-final/`。
794个编译来源包含21个本地替换依赖文件，源码指纹为
`e0484a4e64d7b5d2c3438fab8616ffb528c3c3741189d381da7e9f6fbc47cd20`，
覆盖率SHA256为
`d023affb5c9f59ff994ce49d13d2b65cad3b4efd59f352c7747dc99b7d3c8fa8`。
初轮768MiB临时编译盘耗尽、后续公开夹具缺少3份目录文档的失败均保留；
改用独立磁盘目录并补齐冻结文档后重跑，没有修改业务源码或跳过失败用例。
不同平台覆盖率没有混合，Linux结果也不替代GitHub的Linux/amd64完整CI。

新数据库runner使用相同冻结源码、独立PostgreSQL17.6、随机本地端口及新建
`wsl_test`串行验证。control为186通过、1项显式性能跳过；
HTTP为233通过、36项缺少终态，约910.459秒后退出1，gateway未启动。
598项应运行父测试中420项有结果：419通过、1跳过、178缺失，
整轮明确失败，未将缺失或未启动项计作通过。
报告为`output/ci-local/frozen-r4-database-deps-final/report.json`；
`fixture.json`确认测试容器、网络和临时凭据均已清理，没有访问生产数据库。
旧报告未保留足以确证中断原因的标记，不能断言是超时、死锁或panic。
最后可定位的未完成父项为`TestPostgresOperatorCenterPersonalClaimFeatureHTTP`；
诊断增强后另以合成回归验证固定错误分类、退出码及已启动／未启动标记，
没有改写这次失败报告或为通过而跳过HTTP用例。
将真实成功单测与该失败数据库报告交给覆盖率合并器，实测以
`test report did not pass`拒绝且未生成合并文件；
证据为`output/ci-local/frozen-r4-incomplete-coverage/validation.json`。

### 当前阻塞与未执行项

- 根工作区`internal/control/native_settings.go:491`引用尚不存在的
  `features.NativeSettingIncluded`，`native_text.go:154`引用尚不存在的
  `features.NativeProductScope`。新runner在测试发现阶段明确失败；
  未为使CI变绿补写或发布这些无关候选。
- `tools/test_compose_dev.py`需要的`deploy/web.dev.Dockerfile`及
  `tools/docker_dev.sh`缺失：类初始化失败和7个无结果用例分别计入失败。
  另1项Python失败为架构快照不新鲜。
- 架构检查列出`graph.json`、`manifest.json`、`frontend.mmd`、
  `backend-receive.mmd`、`frontend.md`、`backend-receive.md`过期。
  已生成本轮候选证据，没有将整个未发布工作区的图覆盖到已提交基线。
- 财务台账作用域弹窗打开后未请求`/ledger`、已保存记录不显示；
  浏览器回归保留失败与合成截图，没有通过改弱断言或将它移出必需清单放行。
  注册界面使用显式固定的`manual_only`契约；旧实时账号脚本退出自动执行，
  其表单、同意项、CAPTCHA迟到回调、输入清除和无自动重试覆盖已迁移到合成用例。
- 完整串行数据库HTTP包尚未通过，gateway因此未执行，数据库覆盖率不得合并。
- 当前匹配的可信CodeBoarding基线缺失；保留旧缓存，不将其他冻结版本的咨询成功追认给本轮。
- 尚无当前源码的GitHub PR运行、可复用CI成功记录或release dry-run。
  远端提交与本地大量既有未提交产品源码存在差距；没有为CI发布夹带其他候选。
  根源码编译恢复、目录与证据对齐后，仍需在同一提交实际运行完整Linux CI、
  CodeQL提取、产品容器smoke及发布dry-run。

### 交付边界

本轮未提交或推送既有未发布产品文件，未发布镜像，也未重启生产API、Web、热群或数据库。
生产仍为下节记录的v134-r4镜像及配置，readiness不变。
本节的合成检查不代表真实好友认领、群管助手业务或原生动作新增通过。

## v134规则模块补齐已交付，AstrBot功能对照与验证（2026-11-14 16:11，北京时间）

本轮对照`sh492773746/AstrBot`远端master固定提交
`5ff5113074304e6c2fe89483d3497f727620176d`，只读检查旺商聊适配器及群管插件。
完整逐项证据、本站行为差异和后续缺口见
[AstrBot旺商聊功能对照](astrbot-wsl-function-comparison.md)；
[业务模块开发文档](business-module-development.md)同步规则契约和只读试算接口。
保留原Go控制层、双平台独立适配、权限和持久执行，不引入远端Python运行时。

### 已交付范围

- 关键词回复增加字面包含／完全匹配、单条停用和顺序调整；按保存顺序取首个启用规则。
  群共享冷却可配置30至86400秒，旧缺省值仍为30秒，不采用上游逐规则零冷却语义。
- 拦截词增加最多20条命名组合，每条2至5词全部出现才命中；原单词列表仍为任一词命中。
  原群主／管理员、豁免成员、号池机器人保护及处置优先级保持。
- 增加`POST /api/v1/groups/{id}/features/preview`及作用域弹窗试算：
  已保存配置允许只读权限使用，草稿要求对应字段和租户上限；
  返回`simulation / applied=false / native_submitted=false`，不写配置、
  审计正文、任务或原生尝试，不调用模型，不将输入的模拟角色当作身份凭据。
- 新字段独立授权；旧组合为空、冷却为默认值的配置不因缺少新字段权限而停止运行。
  非默认设置重新核验字段权限和租户上限。助手不自动获得组合及冷却编辑能力。
- 草稿按操作者、平台、账号、群和版本隔离，仅恢复实际修改的字段；
  旧无基线草稿跨版本明确失效，避免覆盖他人修改。保存仍只提交当前模块，
  关闭弹窗不提交，迟到响应不覆盖新作用域或新草稿。兼容旧PATCH未携带的新字段。

### 冻结来源与部署

以当时实际运行v133 API／Web及其冻结源码为基线，候选为
`var/astrbot-rules-v134-r4-20261114/release-source`：
32项任务白名单差异、4项生成架构证据、1805项发布清单。
当前根工作区仍含未发布候选，不能据其原生目录或编译状态替代本轮冻结版本；
本轮已发布功能目录在候选的`docs/feature-catalog.md`，同步测试通过。
根工作区其他候选及其生成文件未覆盖到运行镜像。

`delivery-after.json`记录2026-11-14 16:09:30.378北京时间交付成功，
仅更新现有Compose API、Web。没有新增依赖、迁移、服务、worker或数据库连接；
schema保持`072_onboarding_outbound_diagnostic.sql`、CodecVersion保持7，
解码算法、密封格式、原生协议及连接归属保持。

| 服务 | 本轮镜像 |
| --- | --- |
| API | `sha256:2349a1db6b34233f3c3142fec5ee3b37e91fe8b81710a5076143ac6e2824db47` |
| Web | `sha256:92b45765cd507e129b649795b1ca983a05648013f575ce1d8a0818dce1e1b152` |

热群、PostgreSQL容器未重启；配置、凭据、模式及资源限制保留。
上一运行镜像分别保留为`wsl-api:rollback-before-astrbot-rules-v134-r4`、
`wsl-web:rollback-before-astrbot-rules-v134-r4`，回滚无需回退数据库结构。
源码清单SHA256为
`4bd29ec3b349391720d47a94e39cf059bc08f9ee159d8ec94c60586b4b59e2c6`，
发布清单SHA256为
`6ae063843af547f29ab9cd7e099b05d0556ce2c88c38f6fdae1a16556062d375`；
逐项文件、日志及检查provenance位于同一候选目录。
本节和项目断点为交付后的文档续记，不修改已冻结镜像来源。

### 当前候选检查

交付记录中的24项检查均PASS，源码与日志哈希匹配；Web、gateway及浏览器均为本轮新跑。
以下秒数来自检查JSON，不使用外层编排耗时。

| 检查 | 实测结果 | 秒 |
| --- | --- | --- |
| 编译／API构建 | PASS | 30.609／25.486 |
| 产品Web类型检查及构建 | PASS，`tsconfig.wsl.json`及Vite | 51.418 |
| race/sodium | 886个顶层通过、2770个子项通过；157个顶层显式跳过另列 | 77.928 |
| identity Go／UI回归 | 14个顶层通过、3项必需专项用例通过／1个MJS文件通过 | 5.842／0.100 |
| 串行隔离PostgreSQL control | 22个顶层、116个子项通过；1项性能测试跳过 | 130.041 |
| 串行隔离PostgreSQL HTTP | 1项功能字段与租户隔离集成通过 | 12.874 |
| 部署锁回归 | 2个顶层通过 | 13.447 |
| 串行隔离PostgreSQL gateway | 140个顶层、561个子项通过；2项性能测试跳过 | 26.563 |
| 架构更新／检查／架构守卫／仓库守卫 | 全部PASS | 2.879／7.074／5.839／5.802 |
| 架构工具／渲染缓存／Mermaid渲染 | 全部PASS | 21.379／5.760／18.263 |
| CodeBoarding preflight／增量 | 匹配可信基线，PASS，checkpoint已保存 | 15.452／84.088 |
| 容器smoke | PASS | 33.864 |
| 规则编辑／助手结果浏览器回归 | 两项PASS，320／390／768／1440视口 | 60.262／37.948 |
| 规则差分fuzz | 411540次合成执行，0失败 | 33.301 |
| 五组交替规则微基准 | 四类均通过，逐项见下 | 不作为端到端耗时 |

数据库测试仅串行使用隔离`wsl_test`，没有重置或压测生产`wsl`。
control跳过`TestPostgresBusinessRulesDisabledLoad`；gateway跳过
`TestCustomerClosedDecoderPerformance`及`TestPostgresProjectionCachePairedPerformance`。
离线race套件中依赖数据库、长稳和性能开关的跳过项不计通过；
包含子项时skip事件共944个，其中157个顶层。相关持久授权、版本、冷却、
未知结果不重发及字段隔离由本轮单独PostgreSQL测试覆盖，不将跳过的性能项补记为通过。

规则浏览器使用合成HTTP：42次试算、5次配置保存，真实原生请求0，
意外请求和浏览器错误均0。覆盖单模块保存、只读试算、撤权、UTF-8与数量限制、
稀疏草稿及并发版本、旧草稿迁移、切群／平台／账号／操作者和迟到结果隔离；
12张规则截图中另人工检查390宽组合编辑及1440宽关键词编辑。
助手回归同为合成：双平台14个回合、8次确认、4次取消、58次历史读取，
模型／原生／外部请求均0；验证10轮、结果去重和撤权后的历史处理。

CodeBoarding使用匹配来源与设置的`gpt-6.1-sol`可信基线，仅分析439个白名单源码文件，
没有从增量回退到全量；业务网关配置未变，没有披露运行数据或密钥。
本地检查、咨询性模型分析、Compose交付与真实原生验收分别记录，不替代GitHub CI。

### 规则性能与失败轮保留

相同Linux arm64容器、2CPU／512MiB、禁用网络，Go1.26；
基线与候选交替5组，使用相同200个拦截词／100条关键词的末项命中及未命中样本。
每次预热50ms，正式每类300ms，表内为各5次`ns/op`中位数。
完整结果见`rules-benchmark.json`；所有样本均为`0 B/op`、`0 allocs/op`。

| 纯规则场景 | v133基线ns/op | v134候选ns/op | 变化 |
| --- | --- | --- | --- |
| 拦截词命中 | 2872 | 1375 | -52.12% |
| 拦截词未命中 | 682.5 | 695.7 | +1.93% |
| 关键词命中 | 1484 | 725.3 | -51.13% |
| 关键词未命中 | 358.5 | 360.8 | +0.64% |

只优化纯匹配中的重复扫描和循环，不引入明文缓存。差分fuzz与简单参照实现核对。
这些结果不能证明解码提速、原生上游延迟改善、消息链路P95或100／300群生产容量；
未重跑30分钟长稳及此前解码整体性能矩阵，不追认其验收目标。

初始v134候选的功能目录来源错误及基准挂载夹具失败、r2的旧权限兼容问题和
关键词命中／未命中约18.2%／28.7%退化、r3未命中6.71%退化均保留原产物，未部署。
r4修正权限兼容、稀疏草稿及匹配循环后重新冻结、验证并交付，不改写失败轮结果。
架构生成改变清单前的r4微基准另外保存在`benchmark-pre-architecture`，
本节采用最终清单重新运行的结果。

### 部署后与真实验收边界

16:11:02.548北京时间只读核对四服务healthy；`/`与`/healthz`为200，
匿名`/api/v1/features/catalog`为401。有效测试范围0；
唯一旧未知操作`350974e0-eda1-415d-a0cc-2d349fb97e8c`及密封尝试哈希
`c8f018e90d62d99878ce4d0ad40f9cb3308792ce410d9efb7d5bbdfd4ce362ce`
不变，无重放。证据为`post-delivery-readonly.json`。

本轮未执行真实好友、群消息或管理动作，未启用自动业务，不新增真实通过记录；
旺商聊新好友完整认领的1001拒绝及原有未通过项仍按下节记录。
图片回复、定时广告、消息排行、自动名片、刷屏处置及群内活动／财务闭环
仍是独立缺口，不将底层发送或Web配置等同于完整业务接通。
`native_ready`、`ai_ready`仍为false。

## v133追加一次旺商聊好友申请：1001拒绝，未完成新好友认领（2026-11-14 13:37，北京时间）

所有者明确追加一次“林夕→大蛤”好友申请后，通过现有UI建立新范围与新挑战，
使用独立`native_debug`持久授权提交一次；没有复用旧申请、旧证明或耗尽预算。
实际运行仍为下节记录的v133 API／Web镜像，本轮未改代码、迁移或重新部署。

| 项目 | 真实结果 |
| --- | --- |
| 申请方向 | 旺商聊林夕`31846829` → 大蛤`23691273`，搜索账号`340738649` |
| 隔离群 | 测试群`1459282` |
| 接入范围 | `7e6d4a1f-5cc8-4872-b56e-6481c321650c`，创建后有效至19:29:20.670032，实际提前关闭 |
| 新挑战 | `73e47f64-1065-48f8-bee8-78a55789daa8`，13:30:35.518059至13:40:35.518059 |
| 独立授权 | `30684e93-5257-4e56-89df-f03674bb0668`，`native_debug`，已用1/1，13:40:12.313107到期 |
| 原操作 | `e2bc437e-2830-4558-9eb3-af7d76d76244` |
| 目标核对 | 已确认大蛤身份，未将查找成功记为好友关系证明 |
| 唯一提交 | 13:37:10.050314失败，`failed / native_business_rejected`，attempts_used=1 |
| 原提交诊断 | `provider_code=1001`、`provider_category=upstream_rejected`，发生时间13:37:10.050314 |

真实受控页面读取原记录后展示“失败阶段：好友申请”“上游返回码：1001”及采样时间。
申请方向截图为`var/onboarding-diagnostic-v133-r2-20261114/wsl-friend-1001-20261114-133710.jpg`；
关闭范围后经接收方只读原记录的完整诊断截图为同目录
`wsl-friend-1001-result-20261114.jpg`。该只读回看未产生新申请或修改原结果。
诊断链路本轮得到真实验证；数字1001本身不足以核验具体原因，不能据此认定每日限流、
参数错误或号段限制。冻结源码显示提交使用`/v1/friend/add-friend-apply`；本轮没有
获得足以确定协议修正的新证据，没有更换参数、客户端身份或再提交。

13:40:55.330882经UI关闭新范围，state=closed、version2、原生用量1、模型用量0。
林夕动作准入恢复OFF，testing原ON保留，账号v/epoch58；好友出向准入恢复原closed、
version6，原证据引用保留。独立窗口已用1/1且到期，`cancelled=false`，不记作已取消。
挑战自然过期、version2，私聊／群证明事件均NULL；没有审批、证明发送或绑定转移。
13:43:07.273只读检查四服务healthy、有效测试范围0，唯一旧未知操作及密封尝试哈希
`c8f018e90d62d99878ce4d0ad40f9cb3308792ce410d9efb7d5bbdfd4ce362ce`保持。
readiness仍false。本轮为一项明确授权的真实尝试与安全诊断验收，不新增完整认领通过记录；
未再次运行代码回归，也没有将下节原有30项检查冒充为本次重新运行。

## v133诊断修复已交付，双平台新好友完整认领仍未全部通过（2026-11-14，北京时间）

本节为v133交付时快照；最新真实尝试及清理结果见上节，下方v132及更早章节保留各时点快照。
`var/onboarding-diagnostic-v133-r2-20261114/delivery-after.json`记录
2026-11-14 05:29:35.261北京时间交付成功。以实际v132运行镜像及冻结源码为基线，
仅纳入11项产品差异和必要架构证据，发布清单1796项；未混入工作区其他候选。
仅重新交付现有Compose API、Web，并应用兼容的前向迁移
`072_onboarding_outbound_diagnostic.sql`。CodecVersion仍为7，无新增服务或依赖，
热群和PostgreSQL容器、配置、凭据及资源限制保持。05:37:47只读复核四服务healthy。

| 服务 | 当前镜像 |
| --- | --- |
| API | `sha256:fd49e85e1b4347dbbaaaceb81b579268598b5e8b92643479daa62968d09cd428` |
| Web | `sha256:68a80ddffe103a8a8b68b3435dfaa2776f71c4b9400837cb63e9103da8739e82` |

上一运行API、Web分别保留为
`wsl-api:rollback-before-onboarding-diagnostic-v133-r2`、
`wsl-web:rollback-before-onboarding-diagnostic-v133-r2`。回滚应用时保留兼容的072，
不回退已应用迁移。交付前后有效测试范围均为0；唯一既有未知操作及密封尝试哈希
不变，无重放。`native_ready`、`ai_ready`仍为false。

本次修复只补齐提交诊断：适配器保留类型化固定错误码、1至9999的上游数字码及
Submitted状态，丢弃任意上游正文；持久记录增加固定分类、数字码和原发生时间。
三字段同时为空或同时存在，仅允许已消耗一次的submitting记录进入failed／unknown时
首次保存，随后不能修改、清空或给旧失败记录补造原因。只读核对即使改变结果码，
也不覆盖原提交诊断。API增加可选`submit_diagnostic`；前端严格检查版本、平台、
阶段、枚举、数字、时间和字段集合，仅在失败／未知状态展示。未改变原生申请参数、
发送次数、授权、频控、自动重试或错误结果语义。

### 本轮冻结检查

交付记录列出30项当前检查，均PASS、冻结源码未变且具有匹配provenance。
本轮Web、浏览器和gateway均重新运行，未复用v132之前的测试来补足本轮结果。
以下耗时取各检查JSON，不使用外层编排耗时。

| 检查 | 实测结果 | 秒 |
| --- | --- | --- |
| 编译／API／产品Web类型检查与构建 | PASS | 26.641／30.833／8.850 |
| race/sodium | 866个顶层通过，157个显式跳过另列 | 70.070 |
| identity Go／UI | 14个顶层通过、3项必需诊断用例通过／2个MJS文件通过 | 6.223／0.213 |
| 串行隔离PostgreSQL control／HTTP接入专项 | 52／112个顶层通过，无失败 | 162.978／572.073 |
| 部署锁回归 | 2个顶层通过 | 12.859 |
| 串行隔离PostgreSQL gateway | 140个顶层通过 | 28.279 |
| 架构更新／检查／架构守卫／仓库守卫 | 全部PASS | 2.805／7.036／5.801／5.467 |
| 架构工具／渲染缓存／Mermaid渲染 | 全部PASS | 20.936／5.604／18.648 |
| 匹配可信基线CodeBoarding preflight／增量 | PASS，gpt-6.1-sol，checkpoint已保存 | 11.541／21.046 |
| 容器smoke | PASS | 36.770 |
| 浏览器合成回归 | 10／10，含320／390／768／1440视口及新诊断回归 | 逐项见下 |

浏览器逐项秒数：customer86.194、tools19.439、controlled entry17.662、
controlled69.848、messages18.758、dashboard11.558、confirmation47.795、
applicant118.537、outbound gate31.988、submit diagnostic20.221。
新增数据库用例验证安全诊断持久化、5类只读核对、v132 SQL兼容，以及在带有旧失败／
未知记录的原表形态上应用真实072迁移。首次候选目录
`var/onboarding-diagnostic-v133-20261114`的迁移测试因`search_path=pg_temp`
找不到函数而失败，未部署；失败产物保留。修正仅测试夹具为wsl_test内事务隔离的随机
schema后，重新建立r2冻结候选并完整验证，未追认或复用失败轮结果。
本地检查、咨询性模型分析和Compose交付不替代GitHub CI或真实原生效果验收。

### 部署后真实页面与清理复核

- 通过接收机器人大蛤的受控搜索面板，选择原隔离群并读取
  challenge`2e7e5221-5652-476d-8a1a-9dd28856c55f`，显示原操作
  `0c3941ec-f62d-407a-9346-e130757d3b9f`“本次尝试失败”及
  “此历史记录未保留具体上游返回码，无法还原；不会自动重发”。
  此为读取持久历史，未进行新搜索、申请、审批或证明发送。
- 同一操作仍为failed／native_business_rejected、attempts_used=1，
  更新时间仍为2026-11-14 03:51:41.889727北京时间；新增三项诊断均NULL。
  历史数字码无法恢复，不能认定为每日限流、参数错误或号段问题。
- 旺商聊scope`aeb297a8-a936-4c3d-818d-673a5843aeb5`已于
  2026-11-14 04:20:35.187638北京时间通过UI关闭，version2；
  林夕动作准入恢复OFF，账号v/epoch56，出向好友准入恢复closed／v4。
  原独立授权`00504f59`（ID前缀）仍已用1/1、已过期，没有追加额度或重发。
  下方v132章节“仍active”为清理前快照，不是当前状态。
- 辣椒聊challenge`6eb31e8a-414e-422e-b524-cf456a6e7092`复核仍为bound，
  私聊／群证明事件分别为370243／370322。此前已有好友、机器人已在群的受控认领
  分支通过；新待审批好友、普通客户正式认领及旺商聊新好友完整链路仍未通过。
  辣椒聊范围已关闭，后台自动业务和AI保持关闭。
- 旧未知`350974e0-eda1-415d-a0cc-2d349fb97e8c`及密封尝试保持原状。
  本轮没有新增真实原生变更，诊断交付不增加好友申请或认领通过记录。

## v132已交付，辣椒聊受控认领分支通过，旺商聊唯一申请被拒（2026-11-14，北京时间）

以下为本轮最新续记，下方部署时及早先失败快照保持原文。时间均为北京时间。
`var/onboarding-peer-v132-20261114/delivery-after.json`记录`passed=true`：
v132于2026-11-14 03:40:33.248（2026-11-13T19:40:33.248Z）
仅交付现有Compose API，镜像为
`sha256:7376ca3bed597f7380e1e79fe11af1366e43cd56d74595674e6bd33e41a1ec83`。
以实际v131为基线，产品差异仅`internal/nativeauth/onboarding_outbound.go`、
`onboarding_peer.go`、`onboarding_peer_sodium_test.go`三个文件；
兼容已密封认证账号映射的默认`accountId=0`，普通桌面搜索、UID及冲突值校验保持。
无新增迁移，schema071、CodecVersion7保持；1791项发布清单的runtime源码与冻结源码一致。
交付快照四服务healthy，配置保留，Web、热群和PostgreSQL未重新交付；
Web保持v131镜像`sha256:c5a5c649bb851a452bc3940477c7b35328c2538a0146b9a9d06faa1d78f1d47c`，
前一API保留为`wsl-api:rollback-before-onboarding-peer-v132`。
交付本身未执行真实原生验收，native_ready／ai_ready仍为false。

辣椒聊挑战`6eb31e8a-414e-422e-b524-cf456a6e7092`已完成：
私聊操作`1ec008e5-372d-40f7-a535-bf68176e2d2d`对应独立接收事件`370243`，
群证明操作`a83b8356-28ef-4907-80f2-e4aecc03a0e1`对应事件`370322`，
均取得成功回执，原生角色为owner。
正常用户中心于2026-11-14 02:40:27.799351确认后为`bound`，截图为
`var/onboarding-final-v131-20261114/chili-claim-bound-20261114.jpg`。
scope`b5c42c9f-976e-477d-b68a-5d0b0f94440e`已通过UI关闭，
群保留在本人演示空间，绑定v/epoch13，原自动业务OFF，自动群管和AI关闭。
此结果属于已有好友、机器人已在群的受控分支；独立删除及直接成为好友样本另记，
不代表待审批好友分支或普通客户生产认领通过。

旺商聊修复前03:00:56的`friend_info_account_zero`诊断及
scope`69339d2a-2509-4ef5-83d0-be148118abc5`用量0、已关闭的记录保留；
其失效挑战及查找窗口不复用。v132部署后的真实目标读取已通过，
确认已认证映射路径兼容`accountId=0`，不能将读取成功记作好友申请成功。
新scope`aeb297a8-a936-4c3d-818d-673a5843aeb5`有效期为
2026-11-14 03:43至09:43，本轮记录时仍为`active`，尚未关闭。
challenge`2e7e5221-5652-476d-8a1a-9dd28856c55f`有效期为
2026-11-14 03:48:46至03:58:46，现已过期。
唯一好友申请`0c3941ec-f62d-407a-9346-e130757d3b9f`于
2026-11-14 03:51:41.889727失败，结果为`native_business_rejected`；
grant`00504f59`（ID前缀）已用1/1，预算耗尽。
现有证据没有具体上游错误码，拒绝原因未确定，不能推测为限流或参数问题。
没有重发，也未补次数；旺商聊新好友认领仍未通过。
scope仍active与challenge已过期、grant已耗尽分别记录，不能写作范围已关闭。
旧未知`350974e0-eda1-415d-a0cc-2d349fb97e8c`及密封尝试保持不变、未重放。

交付文件`current_checks`共18项；以下为既有JSON中的确切数量及耗时，
本次仅核读，未重跑检查。18项均`passed=true`、`frozen_source_unchanged=true`、
失败数0；对应provenance均通过，结果哈希及源码标识与交付记录匹配。

| v132当前检查（JSON名） | 实测数量／结果 | 秒 |
| --- | --- | --- |
| compile | PASS | 53.294 |
| api-build | PASS | 45.763 |
| race-sodium | PASS，861个顶层通过 | 77.984 |
| identity-go | PASS，11个顶层通过，required_offline_tests_passed=true | 36.054 |
| identity-ui | PASS，2个MJS文件 | 0.307 |
| applicant-focused-control | PASS，串行隔离PostgreSQL接入专项52个顶层通过 | 229.805 |
| applicant-focused-httpapi | PASS，串行隔离PostgreSQL接入专项108个顶层通过 | 571.553 |
| deployment-lock-regression | PASS，2个顶层通过 | 13.023 |
| architecture-update | PASS | 6.096 |
| architecture-check | PASS | 7.572 |
| architecture-guard | PASS | 7.591 |
| repository-guard | PASS | 5.764 |
| architecture-unit | PASS | 23.426 |
| architecture-render-cache | PASS | 9.213 |
| architecture-render | PASS | 26.405 |
| analysis-preflight | PASS | 14.928 |
| analysis-incremental | PASS，CodeBoarding增量建议分析 | 66.602 |
| container-smoke | PASS | 37.982 |

`evidence-reuse.json`记录`passed=true`、`mode=reused_not_rerun`，
Web及gateway均`rerun=false`，完整treehash已在该记录核验；
本次另核对所列28份产物哈希，全部匹配。以下为verified reuse，未在v132新跑：

| 复用来源 | 原JSON的确切数量／耗时 |
| --- | --- |
| v131 Web构建 | PASS，66.768秒，运行Web镜像未换 |
| v131浏览器合成回归 | PASS，9／9；customer 89.191、tools 18.824、entry 18.266、controlled 69.055、messages 19.677、dashboard 12.008、confirmation 51.419、applicant 121.830、outbound gate 32.417秒 |
| v128 PostgreSQL gateway | PASS，140个顶层通过，26.942秒 |

v132另有`web-build.json`记录PASS／8.876秒，但未列入交付的18项
`current_checks`；Web构建及浏览器仍按上述verified reuse记录，不将8.876秒
写成新Web构建耗时，也不把复用结果加入v132新跑数。
本地冻结检查、复用证据、CodeBoarding建议分析、Compose交付与真实原生验收分别记录；
不替代GitHub CI或双平台完整新好友认领验收，不提高readiness。

## v131已交付，真实认领仍未通过（2026-11-14，北京时间）

`var/onboarding-final-v131-20261114/delivery-after.json`记录`passed=true`，
于2026-11-14 00:54:43.764北京时间（2026-11-13T16:54:43.764Z）
仅更新现有Compose API与Web：
API为`sha256:0915bae881d10fe8bb70581ba483290a05aeaca3fb740e63f63170301b880585`，
Web为`sha256:c5a5c649bb851a452bc3940477c7b35328c2538a0146b9a9d06faa1d78f1d47c`。
交付快照四服务healthy，热群、PostgreSQL容器、配置及资源限制未变，
无新增迁移，schema071、CodecVersion7保持，native_ready／ai_ready仍为false。
部署时有效测试范围为0；旧未知`350974e0-eda1-415d-a0cc-2d349fb97e8c`
及密封尝试保持不变、未重放。此时尚未发送真实新证明，完整认领仍未通过。

v130证据位于`var/onboarding-identity-v130-20261113`：浏览器8／9，
applicant用例发现准入错误遮住回显核对诊断，浏览器矩阵未通过。
修正期间冻结source辅助副本被改动，HTTP报告记录
`passed=false`、`frozen_source_unchanged=false`。Go日志中的control55项、
HTTP108项PASS不能计作该轮发布通过；control亦缺少最终provenance，
受影响报告与失败日志保留。辅助副本已恢复，stage verify PASS仅确认恢复后状态，
不能追认旧轮证据。

v131发布源码位于`var/onboarding-final-v131-20261114`，以v129为基线，
17项产品差异、1791项冻结清单。UI修正的MJS检查及独立applicant诊断已PASS；
这些辅助副本诊断与随后冻结源码的正式浏览器9／9结果分开记录。
v131数据库检查为本轮新跑，不复用v130受影响结果。

辣椒聊私聊路由在3秒总期限内检查最多32条持久化候选，优先有效正文；
1201／1204通知仅在精确有效证明窗口下回退，1205好友删除通知仍不能证明身份。
前端修复准入错误遮挡回显诊断的优先级问题。旺商聊仅增加固定原因白名单分类诊断，
不代表账号查找已修复或真实查找验收通过。

| v131本轮冻结检查 | 实测结果 |
| --- | --- |
| Linux race/sodium | PASS，861个顶层通过，66.991秒；156个显式跳过另列 |
| identity Go／UI | PASS，13个顶层／8.151秒；2个MJS文件／0.379秒 |
| 串行隔离PostgreSQL control／HTTP接入专项 | PASS，55／108个顶层，216.068／627.819秒，均无跳过 |
| 部署锁回归 | PASS，2个顶层，13.692秒，以最新交付JSON为准 |
| 私聊路由旧实现负对照 | PASS，1个顶层，8.624秒；非真实原生效果验收 |
| 编译／API／Web构建 | PASS，8.300／11.130／66.768秒 |
| 本轮浏览器回归 | PASS，9／9，含applicant；逐项耗时见对应JSON |
| 架构更新、检查、架构／仓库守卫、工具、渲染缓存与渲染 | 7项PASS |
| 匹配gpt-6.1-sol可信基线CodeBoarding增量 | PASS，61.079秒，checkpoint已保存 |
| 容器smoke | PASS，41.556秒 |

交付文件列出29项本轮检查；同目录JSON均记录PASS、冻结源码未变并有provenance。
部署锁回归当前为13.692秒，先前进度中的29.674秒不作为最终值。
`evidence-reuse.json`另以`reused_not_rerun`明确复用v128的gateway140项证据，
经完整treehash核验，未在v131重跑；不并入本轮新跑测试数。
本轮本地冻结验证、明确复用证据和Compose交付分别记录，不等于GitHub CI、
新的私聊／群证明或完整真实认领通过，readiness不提高。

### v131交付后真实尝试续记（2026-11-14，北京时间）

以下为辣椒聊本次真实尝试记录，与前述部署时快照分别记录。
scope`b5c42c9f-976e-477d-b68a-5d0b0f94440e`在本次记录时仍为active，
到期时间2026-11-14 06:58:02北京时间；共享native计数为0/2（已用／总额），
自动群管及AI运行均为OFF。范围尚未关闭，须由本人通过owner UI关闭，
或等待本轮6小时范围到期；不记作结束清理。

- 首个challenge`d89c…`（ID前缀）已过期，关联的两个window均used0。
- 第二个challenge为`9ff7c6c1-90a6-4f5d-bc7b-1377519e3d5c`，
  已于2026-11-14 01:22:31.443789北京时间过期。
  私聊操作`a5162b13-5d25-4222-a140-6a8815cc6242`状态为
  `succeeded`／`im_server_accepted`，创建于2026-11-14 01:18:27.366569，
  消息显示时间为同日01:18:29。
  该操作使用独立单次证明窗口（ID前缀`492d3ab6`），计数1/1，
  不消耗scope共享native预算；`reserveNative`对`ProofWindowID`显式分流。
  正常接入页面刷新后显示好友身份已核验，
  持久记录`identity_event_id=367936`，本轮私聊真实接收证明通过，
  与旧未知`350974e0-eda1-415d-a0cc-2d349fb97e8c`无关，不能混为同一操作。
- 群证明window`1661f050-618f-43b0-8063-2ccbbbd678ce`计数0/1。
  群确认时challenge已过期，前置校验返回`native_text_binding_changed`，
  未创建新operation；群证明、绑定及转移均未成功。

网站session已失效，等待本人处理登录滑块，未处理新的CAPTCHA，scope未关闭。
旺商聊尚未进行v131真实诊断。旧未知操作保持不变、未重放；
完整真实认领仍未通过。下一轮需在新的challenge内重新取得私聊及群证明，
不能跨challenge复用本轮证明；上述有效scope不代表业务已启用。

### 功能目录同步候选（未部署）

根工作区补记辣椒聊删除好友、主动申请回执及直接好友关系观察，
仅修改`internal/features/acceptance.go`、`platform.go`、
`acceptance_test.go`、`docs/native-social.md`及生成的`docs/feature-catalog.md`。
旺商聊、其他动作、群管底座计数和生产准入未变；
来向好友审批、群证明及完整认领仍未通过。

`go run -mod=readonly ./tools/feature_catalog -check`通过。
`go test -mod=readonly -race -count=1 ./internal/features ./tools/feature_catalog`
两包通过，包内报告分别为1.347秒、1.570秒，测试进程全部退出。
候选未纳入v131冻结树或运行镜像，未宣称完成其余发布检查或部署。

## v129已交付，真实认领仍未通过（2026-11-13，北京时间）

`var/onboarding-deployment-v129-20261113/delivery-after.json`确认v129于
2026-11-13 22:09:37.635（14:09:37.635Z）仅更新现有Compose API，
镜像`sha256:7e30eee46980ab97148000a8d9bb680e981310a9ff34640d0b77170746b39781`。
交付快照四服务healthy；Web、热群和PostgreSQL容器、配置及资源限制均未变，
无新增迁移，schema071、CodecVersion7保持，`native_ready`及`ai_ready`仍为false。
旧未知操作及密封尝试逐字节未变，未重放；交付时有效测试范围为0。
v129真实验收续记至23:24，双平台完整新好友认领仍未通过。以下短写编号均为ID前缀。

- 旺商聊scope`ed196…`已关闭、原生变更0；授权窗口`02b…`及挑战`960…`已取消。
  目标查询已到达上游，返回409／`native_onboarding_outbound_changed`，未提交变更。
  v130正在增加固定诊断原因白名单，以安全定位UID／account形状差异；
  仍为未部署候选，不记作修复通过。
- 辣椒聊9680209→3129316：独立删除好友
  `b37a1a76-21d9-4cf9-a5ca-eb284ab0f2ba`于22:57:27经原生回执及读回确认成功；
  新好友申请`64884446-1773-4318-83e8-b18068dce24c`约23:00收到提交回执，
  只读核对已观测到好友关系。本轮未识别到待审批申请，属于直接成为好友分支，
  不能记作来向审批通过。
- 证明阶段：挑战`b20…`于23:17过期，证明窗口`84d…`于23:14过期、用量0；
  scope`0ee…`于23:24核实已关闭。实时私聊读取／发送流程返回409／
  `native_private_message_invalid`，最新记录为1205好友删除通知，仍在排查。
  未取得新的私聊或群证明；唯一旧未知
  `350974e0-eda1-415d-a0cc-2d349fb97e8c`保持不变，未重放。

交付事实见`var/onboarding-lock-v128-20261113/delivery-after.json`及同目录检查JSON／日志。
v128于2026-11-13 21:04:02.711（13:04:02.711Z）更新现有Compose API与Web：
API为`sha256:e7ac2b7ab1eeccc071371cb353d8c7198fbd16cc81ff2923a2b2eba0ceae8af3`，
Web为`sha256:b4f4a3fdf810808219d7f3ab3b4a248638855d2f7db0da69116a30f4f7d7dc6b`。
交付快照四服务healthy，热群和PostgreSQL未重建，配置与资源限制保留；
无新增迁移，schema071、CodecVersion7及readiness保持原状态。
旧未知`350974e0-eda1-415d-a0cc-2d349fb97e8c`及密封尝试未变，未重放或改判。

| v128本地冻结验证 | 实测结果 |
| --- | --- |
| Linux race/sodium | PASS，851个顶层通过，128.648秒；151个显式跳过另列 |
| 串行隔离PostgreSQL control接入专项 | PASS，52个顶层通过、无跳过，259.122秒 |
| 串行隔离PostgreSQL HTTP接入专项 | PASS，104个顶层通过、无跳过，719.401秒 |
| 全量串行隔离PostgreSQL gateway | PASS，140个顶层通过、2个可选跳过，26.942秒 |
| 浏览器合成回归 | PASS，9／9 |
| API／Web构建及容器smoke | PASS，68.294／126.872／47.899秒 |
| 架构检查、架构／仓库守卫 | PASS，7.006／4.328／4.095秒 |
| 匹配可信基线CodeBoarding增量 | PASS，78.951秒，checkpoint已保存 |

control与HTTP为接入专项，不写成v128全量矩阵；v127全量结果独立保留于下节。
gateway跳过`TestCustomerClosedDecoderPerformance`和
`TestPostgresProjectionCachePairedPerformance`，不计为通过。
上述为本地冻结源码验证及Compose交付，不等于GitHub CI或真实原生验收。

v128从实际v127发布源码冻结四项改动：`internal/gateway/receiver.go`、
`internal/gateway/receiver_lock_postgres_test.go`、`web/src/wsl/TestTools.vue`及
`tools/onboarding_applicant_ui_smoke.js`。接收器续租、释放、claim与runtime state
写入遇行锁使用`SKIP LOCKED`；跳过不延长本地租约，MVCC复核owner、epoch、
认证及期限，真实到期取消。释放租约与断开状态更新保持原子，跳过时保留worker重试。
`postgres-gateway.log`确认该锁专项9个合成子场景通过；
`locks-negative-control.json`确认旧版超时负对照已复现。
`locks-regression-before.json/.log`是旧实现预期失败，不能算候选正向通过。

v128真实旺商聊范围`f4024794-9963-4612-9920-d914478e23c4`、挑战
`f70f1edc-9203-4c6e-b408-f6771457863c`及窗口
`640ce459-2674-46b4-a031-09f82dc3526f`的目标查询返回
`group_binding_outbound_unavailable`。原生变更及模型用量均为0；
window与challenge已取消，scope已关闭。双平台完整新好友认领仍未通过。

后续源码核对发现另一锁冲突：最终事务对`native_deployment`持有`FOR SHARE`，
同一请求的持久request_id分配通过另一事务UPDATE同一行；外层等待分配完成，
分配又等待外层释放行锁。该自等待先由代码调用链确认，随后
`var/onboarding-deployment-v129-20261113/deployment-lock-negative-control.json`
记录旧v128自等待已复现，`native_calls=0`。这是合成负对照，
不代表真实好友链路通过。
v129修复在`internal/control/group_binding_digest.go`改用
`pg_try_advisory_xact_lock_shared(798426)`与普通版本SELECT，配合
`saveNativeDeployment`已有的exclusive advisory lock保留配置互斥，
避免与request_id行更新冲突；新增
`internal/httpapi/onboarding_deployment_lock_test.go`验证该边界。
测试断言修正后，按最终冻结hash重跑的结果已落盘，当前可确认的v129本地冻结验证如下：

| v129已落盘检查 | 实测结果 |
| --- | --- |
| Linux race/sodium | PASS，851个顶层通过，60.275秒；153个显式跳过另列 |
| API构建／编译检查 | PASS，33.227／12.148秒 |
| 架构检查／架构守卫／仓库守卫 | PASS，5.745／4.462／4.157秒 |
| 架构工具／渲染缓存／渲染 | PASS，19.977／4.319／4.308秒 |
| 候选部署锁回归 | PASS，2个顶层通过，33.227秒 |
| 串行隔离PostgreSQL control接入专项 | PASS，52个顶层通过，146.575秒 |
| 串行隔离PostgreSQL HTTP接入专项 | PASS，106个顶层通过，544.701秒 |
| 容器smoke | PASS，35.130秒 |
| 匹配可信基线CodeBoarding增量 | PASS，13.831秒，0 requests，checkpoint已保存 |
| 旧v128部署行锁负对照 | PASS，自等待已复现，0次原生调用 |

证据为`var/onboarding-deployment-v129-20261113`对应JSON／日志；
上述候选检查JSON均记录`frozen_source_unchanged=true`。
同runtime源码首次CodeBoarding增量为81.896秒，已有模型调用；
当前13.831秒重跑未新增模型请求，不能把此前调用次数计入本次。
旧失败产物保留并分开解释：`deployment-lock-regression-before.json/.log`
为旧实现负对照的预期失败；首轮候选第二个测试误将错误响应的data当作msg处理，
出现`unexpected end of JSON input`，已改为断言`requestWithCaptcha`返回的code。
这是测试辅助代码的断言错误，不计作产品失败，也不隐藏首轮记录。
`evidence-reuse.json`以完整treehash确认Web及`internal/gateway`与v128一致，
并校验依赖、Web构建文件及浏览器检查脚本一致，模式为`reused_v128_not_rerun`。
据此复用v128 Web构建126.872秒、浏览器9／9及gateway140项通过
（另2个可选跳过，26.942秒）；这些检查未在v129重跑，不计入本轮重跑数量。
HTTP专项及smoke为v129本轮实测，不能与上述复用证据混计。
**v129已仅交付API；本地冻结验证、复用证据和Compose交付均不等于CI或真实认领通过。**

接收器审查仍保留三项未计作完整覆盖的测试边界：持续持锁跨真实租约后的取消及
持久化／ACK拒绝；认证撤销、凭据清空、revision变化与首次claim遇锁恢复；
释放后owner清空、租约为空及disconnected状态的联合数据库断言。

## 历史：v127交付与接收器锁故障（2026-11-13，北京时间）

交付事实以
`var/onboarding-peer-v127-20261113/delivery-after.json`及同目录检查JSON／日志为准。
v127于2026-11-13 20:07:49.372（12:07:49.372Z）仅部署现有Compose API，
镜像`sha256:69dd3cfc7bcfa36b90d14fd70d3d7f47952b77cbcddbc2c437c0a71af187fb9e`。
Web保持v119，摘要
`sha256:3819b1133e2222bca50419cef3acea02713c9207ad2042753c7ff61009469ace`；
交付快照四服务healthy，配置保留，热群和PostgreSQL未重建，无新增迁移。
schema071、CodecVersion7及readiness保持原状态。

| v127检查 | 实测结果 |
| --- | --- |
| Linux race/sodium | PASS，851个顶层通过，92.212秒；150个显式跳过另列，不充作通过 |
| 全量串行隔离PostgreSQL control | PASS，172个顶层通过、1个可选跳过，441.441秒 |
| 全量串行隔离PostgreSQL HTTP | PASS，257个顶层通过、1个可选跳过，978.086秒 |
| 全量串行隔离PostgreSQL gateway | PASS，139个顶层通过、2个可选跳过，26.887秒 |
| 浏览器合成回归 | PASS，9／9；不代表真实原生认领通过 |
| API／Web构建及容器smoke | PASS，33.403／5.923／33.309秒 |
| 架构检查、架构／仓库守卫、工具及渲染 | PASS，对应检查JSON均通过 |
| 匹配可信基线CodeBoarding增量 | PASS，预检baseline_compatible；增量143.105秒，checkpoint已保存 |

数据库可选跳过分别为control的`TestPostgresBusinessRulesDisabledLoad`、
HTTP的`TestReplayLoad`，以及gateway的`TestCustomerClosedDecoderPerformance`、
`TestPostgresProjectionCachePairedPerformance`；没有将这些性能用例计为通过。
以上为冻结候选本地检查与Compose交付结果，不是GitHub CI或生产原生验收。
旧未知私聊`350974e0-eda1-415d-a0cc-2d349fb97e8c`及其密封尝试保持不变，
没有重放、补写证明或改判成功。

v127交付后的真实旺商聊范围
`0edb329b-6370-47c4-831a-65b7b3878df2`、挑战
`61ec7b56-b779-4a4d-9574-a099cafee406`，两次只读目标核对均以
`native_read_owner_unavailable`失败，原生变更为0；scope已关闭。
未据此发送好友申请、同意申请或建立新的认领证明。

根因已确认：`controlledOutboundLookup`最终事务以`FOR SHARE`锁定
`accounts/account_receivers`，锁跨越最长3秒原生只读I/O；接收器2秒
reconcile期限内的心跳UPDATE因此阻塞超时，导致全局coordinate退出，
取消共享原生读取队列。该故障不是新的认领成功证据。

## 历史：v126最终交付及v127受控目标核对（2026-11-13，北京时间）

v126最终验证均已完成，仅更新API并前向应用071。实际运行摘要
`sha256:d8c06a2f70b775769f6371cc13c11bc239d4e3df2e02e4477b62f228c401eeb2`，
交付完成2026-11-13T11:07:43.731Z。Web、热群与PostgreSQL容器未重建，
原未知操作及密封尝试逐字节保持，readiness仍为false。

| 检查 | 实测 |
| --- | --- |
| Linux race/sodium | PASS，846个顶层，80.961秒；150个显式跳过单列 |
| 串行隔离PostgreSQL control／HTTP／gateway | PASS，52／103／17个顶层，169.813／575.270／8.131秒 |
| 受控首次认领／旧未知核对专项 | PASS，3／1个顶层，30.885／6.700秒 |
| API／Web构建 | PASS，29.007／6.073秒 |
| 浏览器／容器smoke | PASS，9／9及33.647秒 |
| 架构守卫及匹配CodeBoarding增量 | PASS；增量59.733秒，checkpoint已保存 |

首次交付未运行migrate服务，API因数据库权限检查失败，已恢复上一API。
修正后先备份wsl，在无网络、无应用进程的隔离PostgreSQL中完整恢复至wsl_test，
验证071迁移，再经现有Compose migrate应用到业务库。失败、回滚、恢复及成功
交付证据均保留于`var/onboarding-prebinding-v126-20261113`。

继续真实旺商聊新好友流程时，林夕缺少手机搜索会话，大蛤旧手机会话epoch已失效。
范围`a1e38d48-7ccf-40b5-9952-7c70f598a6da`、挑战
`2e00f5ad-e6a7-4941-8e47-251dbe90d44c`及窗口
`965f10d9-30b7-4df7-8ea9-1d307e745915`均已正常结束，原生变更为0。
没有重贴旧手机凭据、重新登录或发送申请。

v127候选只补严格受控的已认证号池目标核对：使用接收机器人已密封会话的
UID／AccountID对应关系，仍读取实时好友状态和策略，不调用公开搜索。
准备时密封两端凭据摘要、部署与连接owner／revision，最终提交复核及锁定。
普通客户和辣椒聊不使用该路径，旧密封请求不自动增加peer。
编译与新增单元回归通过；串行数据库、race、架构分析和构建验证进行中。
根工作区存在无关未验收候选，直接整体编译失败不作为此冻结候选的结果；
发布与验证均使用从实际v126源码提取的17文件候选。

双平台新好友完整认领仍未通过，不用合成验证或已有好友分支替代。

## v125已交付，双平台新好友链路继续验收（2026-11-13，北京时间）

v125于2026-11-13T08:55:12.380Z仅交付现有Compose API，
镜像`sha256:2e8f4fa832998a121e24fc3313649cb37fa3d706d860b775b865dcf6a9540194`。
冻结源码在`var/onboarding-evidence-v125-20261113/release-source`；
Web保持v119，热群、数据库、凭据、资源限制、schema070及CodecVersion7不变。
交付后四服务healthy，原未知私聊及其密封尝试逐字节不变。

| 检查 | 实测 |
| --- | --- |
| Linux race/sodium | PASS，842个顶层，52.330秒 |
| 串行隔离PostgreSQL control／HTTP／gateway | PASS，52／101／17个顶层，150.710／506.971／12.972秒 |
| 产品构建、浏览器及容器smoke | PASS；浏览器9／9，smoke32.918秒 |
| 架构及仓库守卫、工具测试、渲染 | PASS，59项架构工具测试 |
| 匹配可信基线CodeBoarding | PASS，增量分析及checkpoint保存 |

真实验收仍由项目已认证机器人执行，没有运行官方客户端或新增登录／短信：

- 辣椒聊独立删除`b1e9287a-d836-4a24-b581-e09998d7df19`成功，好友读取确认删除；
  新申请`211a83a0-7853-4a1f-b314-ac42ce1ccae2`取得原生提交回执。
  对应正常挑战`0f2a4933-a3a3-406b-805e-e57928607dde`在审批前过期。
  后续新挑战不能选择前一挑战之前的申请，未自动同意、重发或放宽时间窗口。
  范围`67b4ff8c-fd32-4a1f-aca8-6f187b66fab0`已正常关闭，用量保留1次。
- 旺商聊范围`bacedb8f-cbc6-4d1e-a797-5a05fb771fde`生效，但新窗口准备被
  既有绑定要求阻断；挑战`2275e229-54b6-4b4f-b015-77834c4c6f95`已正常取消，
  范围关闭、用量0。未发送旺商聊申请；临时动作准入及申请人好友发送准入已恢复关闭。
- 发现的循环是：隔离群仍为无归属的历史绑定，而受控申请与证明要求先具有
  同演示空间、同接收机器人的暂停受控归属。不能用SQL迁移归属代替真实证明。

v126候选仅针对该循环，增加前向071迁移，固定历史绑定租户、主账号、epoch、
version；正式客户归属及活动受控归属仍不适用。接收证明继续要求真实认证事件、
独立私聊／群挑战和正常确认／转移审核。完全无绑定群的原生发送不在本候选支持内。
首轮数据库回归发现文本／UUID参数类型冲突及接收证明误依赖申请人连接，
失败日志保留，修正后重新验证。第二轮发现旧密封字段`BindingVersion`读取兼容
遗漏及既有字段快照断言失败；已保留兼容字段，旧记录仅可读取／核对原操作，
不能获得完整绑定快照或新提交权限。双平台旧格式、越界替换和新提交拒绝回归
加入最终冻结13项改动。最新Linux race/sodium846个顶层通过，150个显式跳过
不充作通过；架构、匹配增量及容器smoke通过，串行数据库仍在进行中。
此处不将候选或过期申请记作完整认领通过。
两平台新好友到绑定全链路尚未通过，自动业务和readiness保持关闭。

## 历史：v124已交付及v125真实证据投影修复（2026-11-13，北京时间）

v124余下检查已通过：串行隔离PostgreSQL control／HTTP／gateway分别
52／101／16个顶层，174.588／549.626／6.372秒；浏览器9／9及
容器smoke34.291秒通过。2026-11-13T08:04:31Z仅交付API，
证据`var/onboarding-contact-v124-20261113/delivery-after.json`；
Web摘要不变、四服务healthy，schema070及CodecVersion7保持。

真实删除操作`352fc90d-1ee5-4fe9-a136-4354b38eab9b`于
2026-11-13T08:22:32Z创建，08:22:33Z成为
`failed/native_setting_snapshot_changed`。准备阶段已通过，但在调用原生
删除前的证据检查失败，一次额度已消耗，未退款、未重试；好友关系保持。
测试范围`92007d2c-1497-478b-b207-8d4cafcac793`随后正常关闭。

v125从实际v124冻结源码只提取三个实现文件和两个测试文件：
辣椒聊现有会话查询同时捕获接收器所有者／revision，HTTP证据投影保留该
内部字段；字段不向浏览器序列化。控制层原所有者／版本检查保持。
回归使用合成会话和隔离`wsl_test`，不调用上游或真实账号：

| 检查 | 当前实测 |
| --- | --- |
| v124证据负对照 | PASS，两个预期缺失同时复现，14.404秒 |
| v125证据回归 | PASS，2个顶层，18.495秒 |
| API／Web构建 | PASS，19.706／9.997秒 |
| 其他必需检查、部署及真实新好友链路 | 进行中，不宣称通过 |

此前旧未知私聊及密封尝试保持不变。四个机器人接收器在线不等于认领通过；
本轮没有新增登录、短信、官方客户端启动或自动业务调用。

## v123交付后恢复及v124正常向导回归（2026-11-13，北京时间）

用户专项授权后，v123 CodeBoarding新基线完成初始化和匹配增量，
分别150.003秒／1次请求／报告33157tokens，以及12.009秒／0次请求。
供应商、模型、白名单和预算未改变；旧缓存保留，新checkpoint通过完整性检查。
2026-11-13T07:10:49Z仅更新Compose API至
`sha256:ad07d4cc2935559a8afac7a6782338f42a6a43a4502fc1e9947b20884cd1d83c`，
Web仍v119，其他服务、配置、资源限制、schema070和CodecVersion7保持。
交付证据`var/onboarding-friend-v123-20261113/delivery-after.json`。

真实恢复各一次后，大蛤revision18、林夕revision33均connected，
2026-11-13T07:49:27Z新快照无错误；两账号仍使用原已认证会话。
没有运行官方客户端、新登录、注册或短信。

辣椒聊正常向导创建的新挑战在独立删除准备阶段返回`native_setting_pending`。
原因是向导尚未证明的`binding_version=0`与恢复查询强制要求当前版本不符。
没有新删除操作落库或原生提交，授权次数仍0，好友关系保持。
该挑战及窗口已过期，六小时测试范围已正常关闭。

v124冻结自实际v123发布源码，仅修改联系恢复查询与对应合成测试。
未证明的挑战可匹配版本0，但当前有效、暂停的本人受控绑定仍须锁定；
准备时密封真实绑定版本与epoch，预留和最终提交重新比较。
正常向导Create回归、非零错误版本拒绝及中途版本／epoch变化均覆盖。

| 检查 | 当前实测 |
| --- | --- |
| v123正常向导负对照 | PASS，预期的native_setting_pending稳定复现，2.532秒 |
| 修复后联系恢复数据库回归 | PASS，7个顶层，61.274秒 |
| 冻结源码Linux race/sodium | PASS，841个顶层，152.839秒；149项数据库／显式启用用例跳过，不充作通过 |
| API／Web构建 | PASS，29.683／10.946秒 |
| 架构检查／守卫／仓库守卫 | PASS，3.587／1.870／1.537秒 |
| 架构工具／渲染缓存／Mermaid | PASS，16.524／1.570／10.124秒 |
| 匹配可信基线CodeBoarding增量 | PASS，73.402秒 |
| 串行数据库矩阵、浏览器及容器smoke | 进行中，不宣称通过 |

首轮负对照因测试启动遗漏容器数据库标志而无法连接，仅发生在隔离测试环境；
失败日志保留，修正测试启动参数后才复现业务拦截。
v124当前尚未部署，真实新好友完整认领仍未通过，readiness保持false。
旧未知操作及密封尝试保持原状；没有为核对自动重发。

## 历史：v123受控好友恢复候选与发布阻塞（2026-11-13，北京时间）

本轮从v121冻结源码加入十个控制层文件和两个架构工具文件，发布投影位于
`var/onboarding-friend-v123-20261113/source`。v121仅包含v120及四项旺商聊续票
描述修复；根工作区其他解析、媒体、性能及产品范围候选未纳入。
首次v122冻结误带尚未发布的`NativeSettingIncluded`校验，导致控制包编译失败。
该失败冻结、日志及425个其他顶层通过记录保留，不算发布通过；
v123仅在独立发布投影中剔除这项无关import与校验，根工作区未回退。

控制层将旧未知私聊的只读分类器复用于两条独立受控路径：单次删除好友和新主动
申请。仅允许当前超管自己的演示空间、同平台双方机器人及同隔离群的新有效
scope／challenge，且仍要求各自授权、当前原生角色、epoch、版本、连接与频控。
删除路径把服务端解析的范围快照密封到新记录，准备、预留和最终提交均核对。
浏览器不能传入例外；普通写入、第二条未知或其他运行中操作仍阻断。
删除或申请不建立私聊证明、群证明或群归属，未知新结果不自动重试。

| 检查 | 实测 |
| --- | --- |
| 冻结源码Linux race/sodium | PASS，841个顶层，126.598秒；148项集成／显式启用项目在此无数据库运行中跳过，不充作通过 |
| 串行隔离PostgreSQL control／HTTP／gateway | PASS，51／101／16个顶层，145.346／493.114／5.868秒 |
| API／Web产品构建 | PASS，19.759／1.307秒，Web镜像与现运行版一致 |
| 容器smoke | PASS，29.445秒 |
| 架构检查／架构守卫／仓库守卫 | PASS，2.194／0.924／0.786秒 |
| 架构工具／渲染缓存／Mermaid | PASS，59项工具测试、15.483／0.813／9.088秒 |
| 浏览器合成回归 | PASS，9／9，含双平台、四视口、受控入口、挑战、确认与消息 |
| 旺商聊本地会话检查 | 两账号均deployment_matches／resume_valid、mode2、idle_blocked；native_calls0，未续票或登录 |
| CodeBoarding预检 | BLOCKED，compatible_baseline_missing；0.912秒、供应商请求0 |

单次删除成功、重复点击及重启不重复提交、未知结果不重试、挑战替换、scope关闭、
版本和连接所有者变化、最终提交前插入其他未知工作等合成回归通过；
旧未知操作及密封尝试在这些样例中逐字节保持。普通设置、好友审批及其他平台
不会继承恢复例外。以上是冻结候选本地检查，不是GitHub CI或真实原生效果验收。

CodeBoarding另修复一项真实诊断错误：此前四次增量返回的“502”包含本地预算
误判，已确认usage仍未释放过度预留。候选按核验usage结算，错误／未知usage
继续占用，分开记录本地预算拒绝与供应商错误。16次请求、300000预算、
4096输出、模型及白名单保持。runner摘要发生变化，旧可信基线不可用于新命名空间；
未复制、重标、破坏旧缓存或自动改做全量分析。新基线初始化与白名单源码披露
仍需专项授权，因此本轮没有付费模型调用，发布继续阻塞。

候选API镜像为
`sha256:ad07d4cc2935559a8afac7a6782338f42a6a43a4502fc1e9947b20884cd1d83c`，
**尚未部署**。当前API仍v120、Web仍v119，四服务healthy且容器与镜像未替换，
schema070、CodecVersion7及readiness保持。汇总为
`var/onboarding-friend-v123-20261113/verification.json`。

真实只读确认辣椒聊双方仍为好友；本轮没有新删除、申请、审批或证明发送。
辣椒聊两接收器connected，旺商聊两接收器blocked，开放受控范围0。
“加好友→私聊核验→群证明→确认绑定”的新好友完整分支及旺商聊认领仍未通过。
此前辣椒聊已有好友分支通过记录保留，不能扩大为其他分支。
旧操作`350974e0-eda1-415d-a0cc-2d349fb97e8c`仍unknown；
没有运行官方desktop应用、新登录、注册、短信、业务模型或自动任务。

## v120绑定恢复交付及辣椒聊已有好友认领（2026-11-13，北京时间）

冻结源码`var/onboarding-binding-v120-20261113/source`基于实际v119发布源码，
只接通独立新证明与最终绑定事务，补齐已有好友私聊证明重新核验及对应回归。
公共绑定入口保持严格检查，例外仅用于同操作者演示空间、同平台／双方机器人／
隔离测试群的一条已退役旧私聊unknown。新私聊、群证明、原生回执和当前角色必须
独立成立；运行中、第二条未知、旧挑战有效、身份／正文冲突等仍拒绝。
旧密封意图、操作状态及更新时间不变。无新增依赖、迁移、服务或解码算法。

| 检查 | 实测 |
| --- | --- |
| Linux race/sodium | PASS，831个顶层，60.870秒 |
| 串行隔离PostgreSQL control／HTTP／gateway | PASS，42／10／16个顶层，90.684／18.346／2.988秒 |
| 额外客户／受控／转移绑定矩阵 | PASS，control 1个顶层2.637秒、HTTP 34个顶层107.110秒 |
| API构建／Web产品类型检查及构建 | PASS，18.680／4.590秒，Web源码保持v119 |
| 容器smoke | PASS，29.554秒 |
| 架构检查／架构守卫／仓库守卫 | PASS，1.383／0.337／0.229秒 |
| 架构工具／渲染缓存／Mermaid | PASS，7.629／0.275／15.033秒 |
| 浏览器合成回归 | PASS，9／9 |
| 匹配可信基线CodeBoarding增量 | PASS，125.407秒，checkpoint已保存 |

首轮独立恢复样例和架构调用链证据失败，修正后完成以上检查；旧失败日志保留，
最终新增4个恢复用例通过，不能引用早期`recovery-control.json`为通过证据。
以上针对冻结发布源码，不声明根脏工作区或GitHub CI全部通过。
06:27:14仅重建现有Compose API；运行镜像
`sha256:4d15f9c7ffd9c8f442e0d73b82939d1ca5e5536723c0a35539be73d4f4f3654c`。
Web保持v119，热群及PostgreSQL未重建，四服务healthy。
配置、资源限制、schema070、CodecVersion7和readiness保持；
交付证据为`var/onboarding-binding-v120-20261113/delivery-after.json`。

真实验收使用正常用户中心接入向导及项目已登录机器人，未运行官方客户端：

| 阶段 | 真实结果 |
| --- | --- |
| 范围 | 本人演示空间，辣椒聊tsx9680209→我不知道啊3129316，测试群72567012；六小时范围从06:29:27生效 |
| 首轮窗口 | 挑战`8f0459f5-9a74-48fd-97d0-c63c6f77fecd`在提交前因角色证据陈旧及窗口到期被拒，未产生新发送，不延长旧窗口 |
| 新独立挑战 | `b2b88430-0a7e-45f1-bb27-7b42d1d9f967`，06:44:11创建、06:54:11到期 |
| 私聊证明 | 06:46:38，操作`6da4749a-4fe1-40d5-b692-0d397f249ce4`，`succeeded/im_server_accepted` |
| 群内证明 | 06:48:02提交、06:48:05保存，操作`db5388cd-d5eb-4cd5-a330-2022308e9c63`，`succeeded/im_server_accepted` |
| 本人确认 | 独立接收证据及当前群主核验后，正常确认绑定成功；挑战`bound/version4`，binding_version9，proof_event_id337443 |
| 页面结果 | 用户中心显示测试群、辣椒聊及我不知道啊；绑定有效，业务与热群未启用 |

证据截图为`var/onboarding-binding-v120-20261113/chili-binding-success.jpg`。
本轮通过“已有好友／机器人已在群→私聊核验→群内证明→本人确认绑定”。
这不验证新好友来向申请／同意、机器人新入群、普通客户或旺商聊完整认领。
旧操作`350974e0-eda1-415d-a0cc-2d349fb97e8c`仍unknown，
其原北京时间11月12日06:04:00更新时间及密封尝试保持；没有重发。
自动群管、热群、业务AI及readiness未开启。

## v119接入草稿切换交付（2026-11-13，北京时间）

冻结源码`var/onboarding-proof-ui-v119-20261113/source`只修复旧证明草稿锁住
新挑战输入的问题。旧请求、幂等键和窗口按挑战保留；切换要求显式读取新记录，
不续期、审批或重发。产品构建、浏览器9／9、smoke、架构及仓库守卫和匹配
可信基线CodeBoarding增量通过。05:42:42仅更新Web，API保持v118；
Web镜像`sha256:3819b1133e2222bca50419cef3acea02713c9207ad2042753c7ff61009469ace`。
这项UI交付本身不代表真实认领通过。

## v118认领恢复交付与实际阻塞（2026-11-13，北京时间）

候选`var/onboarding-recovery-v118-20261113/source`基于已交付v117。
只包含旺商聊已登录续票正文对齐、辣椒聊独立新挑战恢复及对应测试；未新增依赖、
迁移、服务、登录、短信或协议版本。官方桌面客户端未被用于执行真实操作。

本地`nativeauth-local-race-sodium`132个顶层用例通过，耗时7.105秒。
新增合成用例分别验证Windows／macOS正文精确为`nimDeviceId`、
原签名请求身份及JWT保持、IM票据更新，以及1200时单次失败且不重试。
候选控制包本地race/sodium编译通过，耗时1.460秒；该编译不算数据库测试。
首轮续票测试错误假定此路由会轮换JWT，修正测试为保留现有JWT契约后通过，
没有因此扩展真实续票的JWT处理行为。

首次隔离PostgreSQL检查在新样例中试图更改不可变挑战期限而失败。
样例已改用明确取消旧挑战，不修改期限；恢复条件相应接受过期或明确退役的挑战，
同时仍要求旧操作和旧窗口到期、旧范围关闭／到期及完全不同的新授权。
Docker曾停止，首次后续检查返回`Cannot connect to the Docker daemon`；
恢复后已继续完成以下冻结源码检查。首轮host race的两个权限样例因启动umask077失败，
改回正常启动umask022后通过，没有修改应用文件权限契约。

| 检查 | 实测 |
| --- | --- |
| Linux race/sodium | PASS，830个顶层，56.632秒 |
| 串行隔离PostgreSQL control／HTTP／gateway | PASS，41／10／16个顶层，78.470／17.761／3.290秒 |
| 独立新证明恢复数据库回归 | PASS，3个顶层，12.496秒 |
| API构建／Web产品类型检查及构建 | PASS，19.685／35.887秒 |
| 容器smoke | PASS，29.891秒 |
| 架构检查／架构守卫／仓库守卫 | PASS，1.762／0.925／0.269秒 |
| 架构工具／渲染缓存／Mermaid | PASS，8.339／0.317／13.055秒 |
| 浏览器合成回归 | PASS，9／9 |
| 匹配可信基线CodeBoarding增量 | PASS，154.880秒，checkpoint已保存 |

04:55:54仅重建现有Compose API／Web；四服务healthy，配置、资源限制及
热群／PostgreSQL保持，schema070、CodecVersion7和readiness不变。
运行镜像和回滚证据见`var/onboarding-recovery-v118-20261113/delivery-after.json`。
原未知操作`350974e0-eda1-415d-a0cc-2d349fb97e8c`和密封尝试跨部署保持一致。
旧未知卡片现在提供显式选择新独立证明的入口；旧编号仍可只读核对。

真实核对：大蛤04:58:45、林夕04:59:56分别单次恢复后仍返回
`native_session_renewal_failed`／1200，没有循环续票或重新登录。
辣椒聊两接收器连接正常；创建本人的六小时控制范围后，首个新私聊证明在提交前到期，
控制层拒绝，没有新原生发送。后续发现Web本地证明草稿使新挑战编号输入框永久锁定；
v119前端修复正在单独验证，尚未交付。两平台完整真实认领仍未通过。
没有新增好友审批、短信、注册、原生登录或业务模型调用。

## v117历史占位兼容及原私聊只读核对（2026-11-13，北京时间）

以实际运行v116及其冻结源码为基线，发布源码位于
`var/chili-evidence-v117-20261113/source`。本轮14个文件、8个Go文件，
仅包含历史RPC1005占位兼容、一次性核对诊断、产品消息界面及相关回归。
根工作区另有未验收候选；其中`native_text.go`的无关产品范围及新鲜度改动未发布。
以下检查针对范围锁定的冻结源码，不表示当前脏工作区全仓或真实认领全部通过。
没有新依赖、迁移、服务或实时接收算法变更，schema070和`CodecVersion=7`保持。

固定桌面1.0.3随包OpenIM SDK 3.8.3的
`internal/conversation_msg/message_check.go:383`明确处理空`clientMsgID`占位。
旧历史接口会拒绝含此合法占位的整批返回，本轮改为保留有合法正序号且在请求范围内的
占位，让同批真实消息继续核对。占位计入条数和字节预算，不提供路由、正文或成功证据；
零序号、错线型、非法非空ID、重复／越界序列及超大记录仍拒绝。
稀疏窗口中发现的候选仍独立严格重读同一序号，原clientID、身份、摘要、路由、
时间及服务端回执必须一致。临时正文检查后清零。

“查询回显”新增非持久化`evidence_check`，固定区分未返回会话、空历史、
未找到原消息、扫描截断及匹配。响应只包含有界数量、状态和采样时间；
错误、未知枚举及越界数量不进入前端投影。界面默认折叠诊断详情，
刷新、切会话和切账号不会自动触发上游核对，迟到响应不能污染新会话。
查询仍共用原有200项／16MiB／5秒预算及读取调度；未知操作继续阻断重发。

| 检查 | 实测 |
| --- | --- |
| 编译 | PASS，55.417秒 |
| race/sodium | PASS，828个顶层，118.229秒 |
| 串行隔离PostgreSQL control／HTTP／gateway | PASS，38／10／16个顶层，106.788／34.579／5.956秒 |
| API构建／Web产品类型检查及构建 | PASS，61.320／79.958秒 |
| 容器smoke | PASS，37.367秒 |
| 架构检查／架构守卫／仓库守卫 | PASS，2.842／0.508／0.507秒 |
| 架构工具／渲染缓存／Mermaid | PASS，10.831／0.585／0.598秒 |
| 浏览器合成回归 | PASS，9／9 |
| 最终匹配可信基线CodeBoarding增量 | PASS，84.403秒，checkpoint已保存 |

负对照使用v116旧实现加本轮占位回归，正确复现旧版拒绝合法批次；
`placeholder-negative-control.json`记录该复现通过。首轮race发现零序号占位
错误分类变化，修正后重跑完整Go矩阵。首次数据库近到期样例在并行构建期间耗尽
两秒期限，原生mock提交数为0；单项重查及随后不并行构建的串行全矩阵通过。
这些失败及修正记录保留，不用局部成功替代最终完整检查。
最终CodeBoarding为匹配的`gpt-6.1-sol`增量，requests1、reserved_tokens136243、
reported_tokens32608、usage_unknown0；没有全量初始化、基线替换或业务模型调用。
以上均为本地隔离执行，不声明GitHub CI执行成功。

北京时间2026-11-13 02:40:34（UTC `2026-11-12T18:40:34.698Z`）
仅更新现有Compose API／Web，镜像分别为
`sha256:1e28da9f14ad3010c9c0eae43a28966e5ebf2286db130aa21fb9c4b898aa31b1`、
`sha256:262f26e54cb7c3bf1b1cbb0e95129eb9ec4b2c2587435e775b782d60083f818e`。
四服务healthy；热群及PostgreSQL容器、配置、凭据、资源限制与readiness保持。
v116镜像及配置已保留，原unknown及其密封尝试在重启前后逐项相同。
维护沿用本次已授权的原记录恢复范围，没有追加发送预算或编造新的所有者回复。

正常网站消息中心对原操作`350974e0-eda1-415d-a0cc-2d349fb97e8c`
仅点击“查询回显”一次。02:41:57的实际结果为`scan_truncated`：
本地提交后候选0、服务器会话水位条目9、目标会话1、请求序号200、返回记录2。
页面明确显示达到扫描上限、原发送仍未知，发送按钮保持禁用；
没有把未找到当作未提交，也没有因通知可显示而宣称消息送达。
证据为`real-query.txt`及`final-reconcile.jpg`。

继续进行本地只读密封核对：20条保留记录均可读取，原发送正文摘要与密封意图一致，
匹配原clientID及正文的记录均为0，原提交之后新增接收事件0。
原选取来源确为1201好友通知，序号220，发送时间为北京时间11月9日07:26:21.095，
两端本地私聊游标也停在220；它不能证明11月12日06:03:57的原私聊发送。
`retained-metadata.json`只保存类型、时间、序号及一致性布尔值，不保存正文或凭据。
固定SDK的`FindMessageList`只查客户端本地库，`IsEnd/EndSeq`也不能补出缺失消息，
本轮没有借其放宽正面证据或新增未核验的远程查询接口。

两台辣椒聊接收器enabled、connected且租约有效，开放测试范围0，
群管、AI及自动业务关闭，`native_ready/ai_ready=false`。
本轮没有原生发送、好友审批、注册、短信、登录或业务模型调用。
历史占位兼容及可排查诊断已交付；原私聊仍缺可核验回显和独立接收证明，
完整真实认领仍未通过。不能清除unknown、重放旧提交或复用过期挑战补造证明。

## v116辣椒聊通知、接收同步与证据衔接（2026-11-13，北京时间）

本轮以实际v115运行镜像及对应冻结源码为基线，发布范围位于
`var/controlled-applicant-v116-20261112/source`。共36个文件、22个Go文件，
仅纳入通知投影、接收同步、后续序列证据读取、相关产品界面与回归；
根工作区的其他未验收候选未发布。没有新依赖、迁移、服务或CodecVersion变更，
`CodecVersion=7`及schema070保持。本节是冻结发布源码的验证结果，
不是当前脏工作区全仓或生产认领全部通过的声明。

修复内容：

- 兼容已核实的1201／1203／1204好友通知及2200回执通知，支持有界的客户端扩展字段。
  验证附言、处理正文和扩展正文不进入展示投影、日志或审计。
  通知展示与认领证明分离，好友通知或已读回执不能代替独立私聊／群证明。
- RPC1002保留正序列、空clientID的协议占位记录，仅在整个范围连续且已提交时推进。
  真缺口仍拒绝推进；新会话缺失游标按已有持久起点初始化，已有游标不重置。
- RPC2001同时接收普通及通知消息中的零序列推送，以有界队列持久化后唤醒解码器。
  远端关闭时先收束读取并处理已有队列；真实父请求取消立即停止。
  短暂数据库故障最多3次持久化尝试，每次重新校验提交栅栏；
  此重试只针对接收保存，不重试原生发送。
- 同一原生clientID先推送、后补拉时，保持主接收事件不可变；一致的后续正序列
  以密封补充记录保存，排除在解码、业务消费和认领证明外。
  引用、撤回候选、发送核对及助手结果读取可关联同账号、有效期内的一致补充证据，
  始终保留原主事件ID。冲突内容不能更新水位或产生第二份消息／证明。

只读核对20条真实保留样本：文本6、图片2、好友通知7、群通知2、系统通知3，
展示投影均无错误、无未支持项。这只验证这些样本的当前展示，
未重写历史解码状态或把历史未支持总数清零。

| 检查 | 实测 |
| --- | --- |
| 编译 | PASS，37.056秒 |
| race/sodium | PASS，823个顶层，78.221秒 |
| 串行隔离PostgreSQL control／HTTP／gateway | PASS，36／10／16个顶层，97.221／24.491／3.862秒 |
| API构建／Web产品类型检查及构建 | PASS，42.586／71.581秒 |
| 容器smoke | PASS，38.787秒 |
| 架构检查／架构守卫／仓库守卫 | PASS，2.251／0.644／0.444秒 |
| 架构工具／渲染缓存／Mermaid | PASS，9.517／0.375／0.443秒 |
| 浏览器合成回归 | PASS，9／9 |
| 最终匹配可信基线CodeBoarding增量 | PASS，21.729秒，checkpoint已保存 |

数据库回归包括推送先到／补拉先到、稳定去重、冲突、错账号、过期补充记录、
接收提交失败、断线、取消及引用／撤回／发送观测的原事件关联。
初轮架构提取器仍假设解码唤醒内联，初轮数据库样例的会话类型／记录选择也有误；
已修正提取器与合成样例并保留失败记录。首次Web构建发现好友事件映射的空字符串
索引类型错误，修正后重新通过产品类型检查、构建、浏览器和最终架构检查。
最终22个Go文件与已通过Go矩阵的内容逐项相同，最后改动仅为前端空值收窄。

CodeBoarding实际使用已匹配的`gpt-6.1-sol`增量命名空间。
首次本轮分析182.989秒，requests1、reserved_tokens136124、
reported_tokens34708、usage_unknown0；最终前端类型修正后再次增量，
requests0、tokens0，复用兼容分析图并保存新checkpoint。
没有全量初始化、替换模型基线或把其他版本的成功当成本轮结果。
以上检查均在本地隔离容器执行，未声明GitHub CI执行成功。

北京时间2026-11-13 01:34:39（UTC `2026-11-12T17:34:39.023Z`）
仅重建现有Compose API／Web，镜像分别为
`sha256:edc24c0276e19f87525847650dc14d8a1ad8e07a49810c7a10279573a0fb7cf8`、
`sha256:3417e7199678e98f863c43262c5d0aeecc68c9b4f17c3284867fac714c01b91e`。
四服务healthy，热群和PostgreSQL容器不变，配置、资源限制、schema及readiness保持；
v115镜像和原配置保留供回滚。旧unknown及其密封尝试重启前后逐项相同，
没有追加原生发送预算或伪造新的所有者回复。

正常网站重新加载后，同一条旧消息由“正文类型0”变为“已添加好友”，
可见好友昵称“我不知道啊”；另一私聊正确显示“好友申请已通过”和“回执通知”。
这证明通知解析和界面修复实际生效，不代表该通知建立了认领身份。
截图为`final-notification.png`、`final-reconcile.png`。
随后在原私聊点击“查询回显”一次，页面仍为“提交结果未知，请核对状态或查询回显，
勿重复发送”，发送按钮保持禁用，没有自动重发。

01:42:05的`post-deploy-state.json`确认两台辣椒聊接收器enabled、connected、
租约有效；原操作`350974e0-eda1-415d-a0cc-2d349fb97e8c`
仍unknown，result_code及updated_at未变，自原提交以来新增接收事件仍为0。
开放测试范围0，群管、AI及自动业务关闭，`native_ready/ai_ready=false`。
本轮没有新增原生发送、好友审批、注册、短信、原生登录或业务模型调用。
旧好友关系存在，但旧私聊未获得可核验的回显与接收证明；
独立私聊、群证明及完整真实认领仍未通过，不能以合成测试或通知展示替代。

## v115稀疏历史候选与独立复核（2026-11-12，北京时间）

冻结目录`var/controlled-applicant-v115-20261112/`以实际v114运行版及冻结源码为基线，
仅改动5个Go文件及生成manifest。根工作区还有未发布候选，最初一次误挂根工作区的
gateway测试因无关的`features.NativeSettingIncluded`／`NativeProductScope`缺失而编译失败；
没有修补或发布这些候选。以下发布检查全部针对范围锁定且校验未变化的冻结源码，
不是当前脏工作区全仓通过声明。

旧历史读取把“请求序列范围内每个序号均返回”作为发现候选的前提，
导致有空洞的历史整批被拒绝。本轮保留严格`History`，另增只读`HistoryWindow`用于发现候选：
返回项必须属于原会话及请求范围，序列唯一有序，数量不得超过请求量；
一旦发现窗口不完整，匹配项还须由严格接口单独重读同一序号，
再次核对原clientID、发送人、接收人、类型、摘要、时间和服务端回执。
单条缺失、变化、冲突、超时或所有者变化均不能改判成功。

请求序列数与返回条数独立计数，合并持久源扫描后各自最多200，
总16MiB、5秒；空窗口不能造成无界扫描。不修改实时RPC1002、ACK、接收水位、
发送路径、已消费预算或账号级未决提交阻断。`server_history`仅可核对原提交，
不生成接收事件或认领身份，不能替代独立私聊证明。

| 检查 | 实测 |
| --- | --- |
| 编译 | PASS，19.412秒 |
| race/sodium | PASS，798个顶层，76.673秒 |
| 串行隔离PostgreSQL control／HTTP／gateway | PASS，35／10／12个顶层，88.982／52.896／6.605秒 |
| API／Web类型检查及构建 | PASS，46.339／6.382秒，Web镜像与原版相同 |
| 容器smoke | PASS，49.702秒 |
| 架构检查／守卫／仓库守卫 | PASS，2.829／0.726／0.352秒 |
| 架构工具／渲染缓存／Mermaid | PASS，9.153／0.453／8.950秒 |
| 浏览器 | PASS，9／9 |
| CodeBoarding匹配可信基线增量 | PASS，131.980秒，checkpoint已保存 |

新增合成回归覆盖稀疏私聊候选、单条独立复核、内容／回执变化、错会话、
重复序列、空窗口、200项边界、取消及所有者变化；数据库回归核对原操作、
尝试、挑战、范围、预算、接收及证明记录，保证失败不变更授权或制造证据。
CodeBoarding实际为`gpt-6.1-sol`，requests1、reserved_tokens135454、
reported_tokens33583、usage_unknown0；没有全量初始化或改写旧费用。

北京时间22:48:21（UTC `2026-11-12T14:48:21.826Z`）仅更新现有Compose API，
镜像`sha256:c6b06eeb56d6ff1fba931f422cde1681a79395bdaed63febfc1ef47e01195178`。
Web、热群及PostgreSQL镜像、容器和配置逐项保持，四服务healthy；
资源限制、schema070、CodecVersion及readiness不变，v114镜像和配置保留供回滚。
维护记录沿用本次原操作只读恢复授权，`new_owner_reply_recorded=false`；
唯一旧unknown及密封尝试在重启前后逐项相同，没有追加预算或授权重发。

上线后在正常网站消息中心对原私聊点击“查询回显”一次，
页面返回“提交结果未知，请核对状态或查询回显，勿重复发送”，本次未显示旧批量
`lajiaoliao_evidence_history_partial_gap`错误。页面没有展示Found与Truncated的区别，
因此不能仅凭这次界面断言完整历史已恢复、消息不存在或原提交未发生。
实际截图`final-reconcile.png`保留原对话、未知状态和禁用发送按钮。

22:50:58的`post-deploy-state.json`确认原操作仍unknown、result_code及updated_at未变，
两台辣椒聊接收器enabled、connected且租约有效；自原提交以来新接收事件为0。
开放测试范围0，群管、AI及自动业务关闭，旧好友操作仍friends。
本轮没有新增原生发送、好友审批、登录或业务模型调用；原私聊证明、群证明及
完整认领仍未通过。修复只读候选发现不能恢复上游未返回的证据，
不能通过清除unknown、重用过期证明或自动重发替代真实验收。

## v114历史返回诊断与在线核对（2026-11-12，北京时间）

冻结目录`var/controlled-applicant-v114-20261112/`以实际v113为基线，
仅增加固定历史返回分类、控制层过滤回归及前端安全文案。
空历史、非完整数量、错范围、无效记录和不连续序列分别返回固定错误码；
不返回任意上游正文、消息内容、凭据或原始错误，不改变严格证据匹配条件。
`source-files.json`共9项，无新依赖、迁移、服务或实时接收路径改动。

| 检查 | 实测 |
| --- | --- |
| 编译 | PASS，24.793秒 |
| race/sodium | PASS，795个顶层，78.122秒 |
| 串行隔离PostgreSQL control／HTTP／gateway | PASS，35／10／12个顶层，89.404／23.922／3.154秒 |
| API／Web类型检查及构建 | PASS，61.620／102.828秒 |
| 容器smoke | PASS，40.782秒 |
| 架构检查／守卫／仓库守卫 | PASS，2.525／0.511／0.302秒 |
| 架构工具／渲染缓存／Mermaid | PASS，8.941／0.329／8.960秒 |
| 浏览器 | PASS，9／9，覆盖各类诊断、无自动查询重试及发送持续禁用 |
| CodeBoarding匹配可信基线增量 | PASS，134.182秒，checkpoint已保存 |

CodeBoarding实际为`gpt-6.1-sol`，requests1、reserved_tokens135473、
reported_tokens33573、usage_unknown0。合成和咨询检查不代表真实认领通过。

北京时间21:48:49（UTC `2026-11-12T13:48:49.046Z`）更新现有Compose API／Web，
镜像分别为
`sha256:cc58374f9bfff7c1e8843a81d2ead5df10fdede72e04c08717b81c8d95352bb4`、
`sha256:80dc9fe8f692d20b077d0082479369b55432ee88ba9a8d4d69759e815255e07a`。
四服务healthy，热群及PostgreSQL未重建，配置、资源限制及schema070保持；
v113镜像和配置保留，readiness不变。维护沿用本次原操作恢复授权，
明确`new_owner_reply_recorded=false`，没有追加发送、预算或新的所有者回复。
旧unknown及密封尝试重启前后逐项相同。

正常网站入口对原私聊只读核对，实际返回
`lajiaoliao_evidence_history_partial_gap`：
“平台返回的历史消息数量不足或超出请求范围。原发送结果仍待核对，未重发消息”。
这定位到目标历史批次非空、数量与请求范围不一致；尚不能确定是少于还是多于、
上游保留策略、删除、限流或其他原因，也不能推断原消息未提交、成功或送达。
刷新曾出现实时订阅不可用，点击消息中心刷新恢复原对话后再次手动核对，
仍为同一固定诊断；没有自动重查或重发。

21:56:10的`post-deploy-state.json`确认两个辣椒聊接收器connected、enabled且租约有效，
旧操作仍unknown且updated_at未变，自原提交以来新接收事件为0；
开放测试范围0，群管、AI及自动业务关闭。好友旧操作仍friends，
只能证明已建立关系，不能记为新来向待审批分支通过。
独立私聊证明、群证明和本轮完整认领仍未通过；没有新增真实原生变更、
原生登录或业务模型调用。不能清除unknown、重用过期证明或重新发送来代替核对。

## v113独立历史读取路径（2026-11-12，北京时间）

冻结目录`var/controlled-applicant-v113-20261112/`以实际v112为基线，
仅五个Go文件及生成manifest。新增只读RPC1005显式序列查询；
请求字段与固定SDK所用protocol `v0.0.72-alpha.70`以及项目现用
`v0.0.73-alpha.17`的同名消息字段一致。使用现有protowire／sdkws依赖，
未引入msg服务包或gRPC依赖。实时接收、RPC1002、ACK和水位不变。
空缺、错群、重复序列、无效消息及端到端范围变化不成为成功证据。

| 检查 | 实测 |
| --- | --- |
| 编译 | PASS，15.105秒 |
| race/sodium | PASS，795个顶层，69.798秒 |
| 串行隔离PostgreSQL control／HTTP／gateway | PASS，35／10／12个顶层，61.658／34.928／8.953秒 |
| API／Web构建 | PASS，39.242／6.839秒，Web镜像与原版相同 |
| 容器smoke | PASS，37.370秒 |
| 架构检查／守卫／仓库守卫 | PASS，2.594／0.508／0.320秒 |
| 架构工具／渲染缓存／Mermaid | PASS，8.894／0.311／8.790秒 |
| 浏览器 | PASS，9／9 |
| CodeBoarding匹配基线增量 | PASS，137.869秒，checkpoint已保存 |

分析实际记录`gpt-6.1-sol`，requests1、reserved_tokens135434、
reported_tokens33497、usage_unknown0。以上均为候选检查，不替代真实认领。

北京时间21:17:22（UTC `2026-11-12T13:17:22.215Z`）只更新Compose API，
镜像`sha256:5d710116162a453095c7d8fee05dacf6844866ee1167a9fdeb1b3390b1006325`。
Web、热群和PostgreSQL容器逐项不变，四服务healthy，配置和schema保持；
原unknown及密封尝试保持。v112镜像／配置保留，未新增迁移或提高readiness。
本次仍属于既有原操作只读恢复维护，无新所有者回复、重发或预算追加。

上线后通过正常网站入口对同一原操作只读查询一次，实际仍为
`lajiaoliao_evidence_pull_gap`。这证明改用历史接口并未恢复足以确认原发送的证据；
不能由序列缺失推断“未提交”，也不能将旧操作改判成功。
后续v114已将返回形状进一步分为固定类别，实测结果见本报告顶部；
本节保留v113当时的阻塞，不预先归因为限流或历史清理。

## v112只读核对诊断修复及真实阻塞（2026-11-12，北京时间）

所有者完成网站滑块后，正常超管会话恢复，消息中心可以找回原私聊unknown。
v111的历史读取失败原样穿透为HTTP内部错误。v112将其转换为固定阶段／原因白名单，
前端显示对应安全文案；不返回任意原始上游正文、SQL、凭据或消息内容。
没有修改原始密封意图、提交状态、已消费预算、回显匹配条件或接收水位。

冻结目录`var/controlled-applicant-v112-20261112/`的检查实测：

| 检查 | 结果 |
| --- | --- |
| 编译 | PASS，35.063秒 |
| race/sodium | PASS，792个顶层，101.277秒 |
| 串行隔离PostgreSQL control／HTTP／gateway | PASS，35／10／12个顶层，67.335／30.508／7.424秒 |
| API／Web类型检查及构建 | PASS，32.603／79.349秒 |
| 容器smoke | PASS，33.533秒 |
| 架构检查／守卫／仓库守卫 | PASS，1.754／0.362／0.269秒 |
| 架构工具／渲染缓存／Mermaid | PASS，8.549／0.289／0.351秒 |
| 浏览器 | PASS，9／9，含过期窗口恢复unknown及诊断失败不重发 |
| CodeBoarding匹配可信基线增量 | PASS，144.362秒，checkpoint已保存 |

CodeBoarding为`gpt-6.1-sol`，requests1、reserved_tokens135182、
reported_tokens34644、usage_unknown0；没有执行全量初始化或改写旧未知费用。
上述均为代码、合成数据库、浏览器及咨询分析证据，不是真实群认领通过。

北京时间20:47:27（UTC `2026-11-12T12:47:27.965Z`）完成现有Compose API／Web更新；
四服务healthy，配置、资源限制和schema070保持，热群及PostgreSQL未重建。
API镜像`sha256:8f33555255d85dca6103f4ebc32a6268467c1b70dc1f8c91f84f790aae0f3285`；
Web镜像`sha256:5cd0ba5fa6d1be39ecc51fbecd396776635f637610e97ea1756c0b42d143c54b`。
授权记录引用v111本次专项维护，明确`new_owner_reply_recorded=false`；
旧unknown及其密封尝试重启前后逐项相同，没有解除发送阻断或追加原生预算。

部署后只读查询原操作一次，产品明确显示
`lajiaoliao_evidence_pull_gap`：平台历史拉取存在缺口，原发送仍待核对，未重发。
这个真实结果将故障定位到Transport.Pull，不能当作原发送未提交或已送达。
随后对照固定桌面1.0.3随包OpenIM 3.8.3源码：
`pkg/constant/constant.go`定义历史RPC1005；
`internal/conversation_msg/message_check.go`使用GetSeqMessageReq显式序列列表，
而RPC1002属于实时同步。相应v113候选与交付结果后续独立记录。
新好友待审批、独立私聊、群证明及完整认领仍未通过；readiness不提高。

## v111 专项维护部署与在线复核（2026-11-12，北京时间）

所有者对已验证v111候选的专项维护部署明确回复“允许”。本次独立记录于
`var/controlled-applicant-v111-20261112/maintenance-authorization.json`，
不套用v110授权。正常阻断报告继续保留，后续授权预检见
`authorized-preflight.json`。

`delivery-after.json`确认北京时间19:54:47
（UTC `2026-11-12T11:54:47.692Z`）部署成功，仅更新现有Compose API和Web。
四服务healthy，配置、资源限制及schema保持，无新增迁移，readiness不变；
热群及PostgreSQL容器未变。上一版v110镜像及配置保留供回滚。
唯一旧unknown及其密封发送尝试在重启前后逐项相同，没有解除发送阻断。
必需验证使用下节同一冻结候选的通过证据，不重复收费分析或扩大真实操作。

北京时间19:58:17的`post-deploy-state.json`确认两台辣椒聊接收器已connected、
enabled且租约新鲜；开放测试范围为0，群管和AI关闭。
原私聊仍unknown，`native_text_result_unknown`及updated_at未变，
自原提交以来新接收事件为0。本次没有新的原生操作、重发或业务模型调用。

上线后浏览器重载显示网站会话失效，正常登录已进入GoCaptcha滑块。
当前等待所有者完成网站安全验证，尚未使用新入口核对真实服务器历史，
也未新增私聊／群证明。没有绕过CAPTCHA、伪造会话、自动原生登录或注册。
完整受控认领继续待验收，不因维护部署成功标记通过。

## v111 辣椒聊原私聊历史核对：候选验收（2026-11-12，北京时间）

本轮目标是继续由机器人完成真实受控认领。实际发现旧私聊unknown无法跨范围关闭后的
账号epoch核对，且发送授权过期后页面无法重新找回原操作。本轮修复只读恢复路径，
不修改原始密封意图、已消费预算、发送准入、nonce或操作结论。
冻结目录`var/controlled-applicant-v111-20261112/`以实际v110镜像对应源码为基线；
`source-files.json`列15项，未混入其他未验收候选。

新增核对只接受同一网站操作人、同一已认证原生身份及当前epoch。
服务端先查持久接收证据，再复用已有连接有限读取服务器历史，
上限为两个有固定客户端／SDK依据的会话、200条、16MiB及5秒。
只读RPC1001／1002，没有RPC1003、新连接、恢复水位更新或新接收事件。
`server_history`成功证据使用event_id=0，不能作为独立接收证明。
缺失、冲突、截断、超时或身份变化不更新原unknown，不触发重试。
页面在发送条件失败时可恢复原会话下本人的未决操作编号，不因此获得提交权。

| 最终候选检查 | 实测 |
| --- | --- |
| 编译 | PASS，31.289秒 |
| race/sodium | PASS，789个顶层，72.342秒 |
| 串行隔离PostgreSQL control | PASS，33个顶层，54.419秒 |
| 串行隔离PostgreSQL HTTP | PASS，10个顶层，30.253秒 |
| 串行隔离PostgreSQL gateway | PASS，12个顶层，12.246秒 |
| API构建 | PASS，36.910秒 |
| Web类型检查及构建 | PASS，60.083秒 |
| 容器smoke | PASS，36.488秒，独立临时wsl_test，无真实账号 |
| 架构检查／架构守卫／仓库守卫 | PASS，1.665／0.532／0.255秒 |
| 架构工具／渲染缓存／Mermaid渲染 | PASS，8.939／0.375／0.438秒 |
| CodeBoarding匹配基线增量 | PASS，224.131秒，可信checkpoint已保存 |
| 浏览器回归 | PASS，9／9，覆盖两平台原申请人发送、过期授权下找回unknown及不重发 |

浏览器逐组耗时为：customer_onboarding75.672秒、test_tools13.516秒、
controlled_onboarding_entry11.434秒、controlled_onboarding62.294秒、
messages11.184秒、account_dashboard5.806秒、test_tools_confirmation40.525秒、
onboarding_applicant80.781秒、onboarding_outbound_gate24.040秒。
以上是合成协议和本地UI回归，不是真实好友申请、私聊证明或认领通过。

Control与HTTP回归覆盖原授权关闭、epoch变化、身份被替换、读取途中epoch变化、
证据来源伪造、本人历史与另一操作人隔离、预算不增及不新增发送。
网关回归覆盖有界历史、当前连接所有者、路由、摘要、原生标识、缺口、截断及取消。
Control／HTTP的被测文件及依赖在最终冻结中保持不变；最终网关修订另完成对应集成及
全包race检查。所有报告保存冻结源码未变结果。

CodeBoarding实际记录为`gpt-6.1-sol`、requests1、reserved_tokens134857、
reported_tokens34567、usage_unknown0、checkpoint_saved=true、
baseline_restored=false。本轮不冲销此前分析超时的未知费用。
Web首轮因移动基础镜像下载及依赖网络超时失败，原`web-build.*.previous-*`保留；
随后验证Dockerfile固定使用v110同一node／nginx摘要及原锁文件，未改产品依赖，
最终构建通过。初次测试数据库未就绪的原失败记录也保留，不计作产品通过。

候选API镜像：
`sha256:1526dccbd4db3ac3504a685e6285388658493979036a15938c1bd6a4832de3f7`；
候选Web镜像：
`sha256:0ab86e548ffa170e34fd70471d001c809689e1430d0ff4ba1878954afb3cb2da`。
两镜像尚未部署。

北京时间18:56:08（UTC `2026-11-12T10:56:08.936Z`），正常发布预检被唯一旧unknown阻断。
`deployment-preflight.json`为blocked、passed=false、
reason=`unresolved_native_submission`、candidate_checks_passed=true、
deployment_started=false、runtime_unchanged=true。未套用v110专项授权。
四服务healthy，容器ID、镜像与v110冻结基线逐项相同。

北京时间18:59:23的真实只读采样确认：

- 旧私聊`350974e0-eda1-415d-a0cc-2d349fb97e8c`仍unknown，
  `native_text_result_unknown`及updated_at未变；自原提交以来两账号新接收事件为0。
- 旧好友操作仍friends，仅有直接建立好友关系的证据；新来向待审批分支仍未验收。
- 两接收器connected、enabled且租约有效。动作连接关闭、testing_enabled=true保持，
  968 epoch83、312 epoch35；没有开放测试范围。
- 群管、AI及自动业务仍关闭。现场968 outbound gate为open、version3，
  本轮未将其覆盖为v110历史关闭状态。

本轮没有真实原生变更、重发、自动登录或业务模型调用，readiness不提高。
完整认领尚未通过；需先明确授权对本候选维护部署，再核对原操作，不能用合成通过
代替真实私聊、群证明、本人确认及归属转移。

## v110 专项维护部署及测试收尾（2026-11-12，北京时间）

发布前的只读preflight确因旧unknown阻断。其后，所有者在对
“保留旧未知记录及发送阻断，维护部署已验证v110”的专项确认中回复“允许”。
本次部署脚本仅在显式维护参数下允许操作
`350974e0-eda1-415d-a0cc-2d349fb97e8c`原样保留；其他未决操作仍阻断，
没有修改产品执行准入、解除发送阻断或追溯改判该操作。
授权记录为`var/controlled-applicant-v110-20261112/maintenance-authorization.json`；
原阻断报告保留，后续专项预检记录于`authorized-preflight.json`。

`delivery-after.json`确认北京时间2026-11-12 **07:20:44**
（UTC `2026-11-11T23:20:44.656Z`）部署成功，仅更新现有Compose的API及Web。
运行镜像分别为
`sha256:b8f0b4953cb39d1450b24bfb93b2d7c396f80e867baa833ecc8f18856f7aa9a0`、
`sha256:6aa8de295a2a2d89c7c045ee9668181390df6fe6015588e3c9f3a656f62c9f10`。
四服务healthy，热群及PostgreSQL容器未变；配置、资源限制及schema070保持，
未增加迁移，readiness保持false。v109镜像及配置已留作回滚。
重启前后旧操作与密封发送尝试逐项相同；本次维护未新建原生发送或模型调用。
此前进度消息中的08:20:44为时区换算笔误，以以上07:20:44及原始UTC记录为准。

北京时间07:34:37的`live-state.json`确认：

- 已通过正常超管界面将968和312账号的临时`connection_enabled`恢复false，
  epoch分别推进至83和35；原始`testing_enabled=true`均保留。
- 968的主动好友发送准入已关闭、version2；312原有来向准入未修改。
- 受控范围保持closed、version2；群管、AI及自动业务开关均关闭。
- 两个接收器仍enabled、connected且租约新鲜；好友关系仍为friends。
- 旧私聊操作仍为unknown，result_code及updated_at未变；
  本轮没有新的接收证据，没有重发。私聊／群证明及最终认领仍未通过。

此前旧标签页确实显示登录失效；本轮新打开的管理页实际可正常鉴权，
因此已完成原先待定的开关还原，不再要求所有者为此重新登录。
候选必需检查复用下节同一冻结源码的通过证据，未重复收费分析或扩大真实测试。

## v110 receiver锁顺序及未提交分类：发布前检查（2026-11-12，北京时间）

据主代理通知，ROOT已将receiver的数据库I/O移出`m.mu`，
新增使用真实PostgreSQL行锁的合成回归；该回归不操作真实账号。
rpc1003仅在明确尚未进入`WriteMessage`时标记专用`not_submitted`，
异常保留`Unwrap`及固定`Error`码；control仅在专用`not_submitted`
且`Submitted=false`时认定failed，其他情形保留unknown，不自动重试。
Nash的新鲜role错误映射三文件候选已合入本轮候选。

冻结目录为`var/controlled-applicant-v110-20261112/`；
`source-files.json`列出9个Go文件、`web/src/wsl/api.ts`及架构graph／manifest，
共12个文件。最终候选检查全部通过，报告均记录冻结源码未变；
部署preflight另按未决提交阻断，未执行部署。

| 检查 | 已确认结果与限制 |
| --- | --- |
| 编译 | 最终`compile.json`确认25.928秒通过；早期20.253秒通过记录保留 |
| focused control | 32个顶层测试104.219秒通过 |
| focused httpapi | 8个顶层测试44.066秒通过 |
| focused gateway | 10个顶层测试23.679秒通过；含ReceiverReconcile的renew／release两例，实际等待PostgreSQL行锁，无skip |
| 旧v109 receiver负对照 | 两例稳定失败；runner确认预期回归复现，24.137秒通过，不是旧代码通过 |
| 架构检查及守卫 | update2.191秒、check2.839秒、guard0.514秒、repo0.500秒全部通过 |
| 架构工具及渲染 | unit11.056秒、rendercache0.608秒、render0.646秒全部通过 |
| CodeBoarding | 匹配可信基线的incremental263.501秒通过，可信checkpoint已保存 |
| race/sodium | 782个顶层测试174.088秒通过 |
| API构建 | 149.353秒通过 |
| Web类型检查及构建 | 295.786秒通过 |
| 浏览器回归 | 当前v110候选9／9通过，本轮重新执行，逐组测量如下 |
| 容器smoke | 139.492秒通过 |
| 交付gate | 北京时间07:00:06只读preflight为blocked、passed=false；候选检查通过，因旧unknown未开始部署 |

早期编译20.253秒通过记录保留于
`compile.*.previous-1794437077853`，不替代最终25.928秒测量。
本轮浏览器结果来自`browser-suite.json`及对应`browser-*.json`，
不沿用v105浏览器结果：

| 当前候选浏览器检查 | 结果／秒 |
| --- | --- |
| `customer_onboarding_ui_smoke.js` | PASS／119.087 |
| `test_tools_ui_smoke.js` | PASS／15.783 |
| `controlled_onboarding_entry_ui_smoke.js` | PASS／14.541 |
| `controlled_onboarding_ui_smoke.js` | PASS／68.430 |
| `messages_ui_smoke.js` | PASS／13.584 |
| `account_dashboard_ui_smoke.js` | PASS／7.570 |
| `test_tools_confirmation_ui_smoke.js` | PASS／46.455 |
| `onboarding_applicant_ui_smoke.js` | PASS／87.206 |
| `onboarding_outbound_gate_ui_smoke.js` | PASS／25.833 |

`images.json`记录已构建但未部署的候选镜像：
API为`sha256:b8f0b4953cb39d1450b24bfb93b2d7c396f80e867baa833ecc8f18856f7aa9a0`，
Web为`sha256:6aa8de295a2a2d89c7c045ee9668181390df6fe6015588e3c9f3a656f62c9f10`。

旧receiver负对照首轮因测试PostgreSQL重启后tmpfs无schema，
返回`relation "accounts" does not exist`（SQLSTATE42P01）。
原始失败保留于`receiver-regression-before-missing-schema.log`；
这是环境准备失败，不计为产品失败，也不计为产品或负对照通过。
有效负对照`receiver-regression-before.json`记录
`expected_old_revision_failure_observed=true`，renew／release两例旧代码均失败；
新代码对应两例均通过，日志无skip。以上均为`wsl_test`合成回归，不授权真实效果。

实际`analysis-incremental.log`记录模型`gpt-6.1-sol`、requests1、
reserved_tokens134761、reported_tokens33489、usage_unknown0、
checkpoint_saved=true、baseline_restored=false。
本轮usage_unknown0不冲销v108 timeout的usage_unknown1及134379未知预留，
历史实际费用仍未知。

`deployment-preflight.json`于北京时间2026-11-12 07:00:06
（UTC `2026-11-11T23:00:06.086Z`）确认status=blocked、passed=false，
reason=`unresolved_native_submission`，未决操作为
`350974e0-eda1-415d-a0cc-2d349fb97e8c`。
candidate_checks_passed=true、deployment_started=false、runtime_unchanged=true；
仅执行只读preflight，v110尚未部署，未重启API或替换Web。
四服务均healthy，容器ID及镜像与v109交付快照逐项相同。运行中的API／Web为：

| 服务 | 运行容器ID | 运行镜像 |
| --- | --- | --- |
| API | `0a5bc6bfd39072bc76aab5f2895b692e2201daeb764c396b08dce244af58cc91` | `sha256:b63809dfc65f1c63c585da9fef337f68cc2973bcdb76961b4172460e946c2dc5` |
| Web | `c12f0eb8ed4a1d0c92ebb4aff32b6e423383ae27a2ce069a285b2227f805d21a` | `sha256:18681ceb62b2a34e738d9ac8cf25c66e73cf9547c791a98f0d2b275bfcb42f51` |

本轮合成检查通过与交付阻断分别记录，不作为真实验收通过的依据；
v109已有检查及交付结果和全部历史失败保留。

以下真实状态以北京时间06:58:18采集的`live-state.json`及主代理通知为据：

- scope`1d19b3b7-a56b-4091-b367-2622315ec9c6`已通过UI关闭，
  state为closed、version2；关闭范围不解决旧操作的unknown。
- 真实私聊操作`350974e0-eda1-415d-a0cc-2d349fb97e8c`仍为unknown，
  result_code为`native_text_result_unknown`；两个账号本轮收到的新事件均为0，没有重发。
  不得用新代码追溯判定该旧操作为未提交、not_submitted或failed。
- 好友操作`e17cd944-6a9c-47e7-b421-58dbf3c3d696`属于
  `group_binding_outbound_attempts`，归属此前独立的
  scope`37bb3582-bb76-4306-bba8-72cb4082d5ee`。
  采集确认其为friends／`native_friend_relationship_observed`，
  updated_at为北京时间2026-11-12 04:09:44，不是本轮私聊操作。
  当时直接建立关系，未执行接收方好友审批；真实新好友审批分支仍未验收。
- 当前scope共享usage采集值为native_attempts0、model_calls0、reserved_micro_usdt0，
  私聊专用singleton证明窗口used1。审阅已确认：proof路径的`reserveNative`
  校验已消费的专用singleton，不写`controlled_onboarding_usage`；
  private／group按challenge+kind各自1次预算、最长300秒。
  两者为预期独立计数，均不能证明未发生原生尝试，也不能解决unknown，
  不冲销架构分析的历史未知费用。
- 网站登录已过期；cleanup目前仅成功关闭范围。
  两个账号的`testing_enabled=true`均为原始基线，必须保持true。
  待还原的仅为两个账号临时动作连接开关`connection_enabled=true`恢复为false，
  以及968的outbound gate从open、version1关闭。
  receiver的enabled状态及连接所有权保持原状，采集时两个receiver均enabled且connected；
  不记录cleanup全部完成。

候选检查已完成，旧unknown仍阻断交付，后续恢复与cleanup事实待主代理通知；
独立私聊／群证明及最终绑定尚无通过结论，不记录完整接入或认领完成。

## v109 首次私聊通知路由修复与API交付（2026-11-12，北京时间）

v109冻结差异为6个Go文件及架构生成graph／manifest，共8个文件，
准确范围见`var/controlled-applicant-v109-20261112/source-files.json`。
本版仅在有live受控私聊proofwindow时允许从1201 friend-approved／1204 friend-added
通知解析路由；普通发送不因此获准，通知也不充当独立私聊证明。
stage／final重新核对sealed事件及scope；已提交unknown的只读核对独立于新发送准入，
scope关闭或到期后仍可查，不重发。

| 检查 | 最终结果与限制 |
| --- | --- |
| race/sodium | 778个顶层测试157.990秒通过 |
| focused control | 无并行负载第二轮完整31个顶层176.536秒通过；首轮NearExpiry失败保留 |
| focused HTTP | 99个顶层测试1166.811秒一轮通过，未timeout |
| focused gateway | 26个顶层测试4.059秒通过 |
| 编译／API构建 | 第二轮编译42.626秒、API构建32.103秒通过；首轮编译失败保留 |
| 容器smoke | 33.982秒通过 |
| 架构 | update／check、架构及仓库守卫、53项工具测试与Mermaid渲染全部通过 |
| CodeBoarding | 匹配v108可信基线的增量247.751秒通过，可信检查点已保存；历史未知费用保留 |
| Web检查沿用 | 未变Web沿用v105构建及9组浏览器回归，本轮未重跑前端检查 |
| 部署／真实验收 | 2026-11-12 05:31:17仅部署API成功；新scope唯一真实私聊操作为unknown，未观测接收事件，完整流程与最终绑定尚未验收通过 |

首轮编译12.109秒失败，日志为
`internal/control/native_text.go:153:37: undefined: features.NativeProductScope`。
据主代理说明，ROOT候选混有旧未发布引用；仅将本轮一处调用改动移植至v108源码，
排除无关引用并保留ROOT候选，第二轮编译通过。首轮JSON及日志保留于
`compile.*.previous-1794430490814`。

control首轮89.120秒在`TestPostgresControlledExecutionNativeNearExpiry/wsl`
断言失败，writes0，返回`controlled_onboarding_scope_inactive`。
主代理只读定位为并行负载下拒绝保存后再次读取ActionPlan时，两秒scope已到期；
相关冻结`native_settings`／`assistant_actions`文件与v108逐字节相同，
未为此修改生产逻辑。无并行负载复跑完整31项通过，首轮失败仍保留于
`applicant-focused-control.*.previous-1794430901671`，不改写为通过。
隔离PostgreSQL仅使用`wsl_test`；据主代理通知，测试库已恢复停止。

实际`analysis-incremental.log`记录模型`gpt-6.1-sol`、requests1、
reserved_tokens134683、reported_tokens33636、usage_unknown0、
checkpoint_saved=true、baseline_restored=false。
本次usage_unknown0仅指本次分析，不冲销v108供应商timeout的usage_unknown1及
134379未知预留，前次实际费用仍未知。

`delivery-after.json`确认北京时间2026-11-12 05:31:17
（UTC `2026-11-11T21:31:17.513Z`）部署成功，API镜像摘要为
`sha256:b63809dfc65f1c63c585da9fef337f68cc2973bcdb76961b4172460e946c2dc5`，
容器ID为`0a5bc6bfd39072bc76aab5f2895b692e2201daeb764c396b08dce244af58cc91`。
仅API变更，Web、热群及PostgreSQL容器ID与镜像未变，交付时四服务healthy；
配置字节与运行配置保留，schema070、无新增迁移，
`native_ready=false`、`ai_ready=false`保持，原生连接随API重启。
据主代理通知，v108已保留供回滚。冻结、检查、增量分析及部署前后证据均在上述v109目录。
部署前“API正在构建、尚未部署”为当时快照；本次部署不代表真实证明通过。
以下真实进展以主代理本次通知为据，时间均为2026-11-12北京时间；
现有合成检查及部署结果保留，不作为本轮真实证明通过的依据。

- scope`1d19b3b7-a56b-4091-b367-2622315ec9c6`原定期限11:33，
  最新已由主代理确认通过UI关闭为closed、version2；
  挑战`be71f62e-da69-43f1-adef-7949ae13afce`。
- 本轮唯一真实私聊操作`350974e0-eda1-415d-a0cc-2d349fb97e8c`，
  方向按主代理记录为`tsx968 -> 312`；created为06:03:57.168，
  submitted为06:03:57.656，06:04:00.205进入unknown。
- 此前已只读核对一次，没有重发；最新通知确认操作仍为unknown，
  两个账号本轮收到的新事件均为0，不记为送达或独立私聊证明，
  也不能以v110候选新代码追溯判为未提交。

主代理发现疑似`ReceiverManager.m.mu`与数据库final-share锁之间的锁顺序死锁，
ROOT候选修复与新增行锁回归见上节v110，最终验证与恢复仍待通知。
v110尚未部署，v109仍在运行，旧unknown按规则阻断API重启。
cleanup目前仅完成UI关闭scope；网站session过期，
两个账号的动作连接启用临时开关及968的outbound gate临时开关尚未还原。
私聊／群证明及最终绑定尚无通过结论，不记录完整接入或认领完成。

## v108 空好友申请页兼容与API交付（2026-11-12，北京时间）

v108已在v107上完成冻结，差异严格限于7个Go文件及架构生成manifest。
本版补充明确成功的空self申请列表、incoming合法空页兼容，并增加辣椒聊
`sent_requests`阶段安全诊断；未修改Tools通用目录。
实现涉及`internal/lajiaoliao/onboarding_outbound_request.go`、
`internal/lajiaoliao/onboarding_contact.go`及`internal/control/group_binding_lookup_error.go`，
另含4个测试文件；准确八文件清单见本轮`source-files.json`。

| 检查 | 已确认结果与限制 |
| --- | --- |
| race/sodium | 774个顶层测试96.821秒通过 |
| focused HTTP | 33个顶层测试216.626秒通过 |
| focused control | 27个顶层测试41.369秒通过 |
| focused gateway | 26个顶层测试4.039秒通过 |
| 编译／API构建 | 编译29.836秒、API构建48.217秒通过 |
| 容器smoke | 39.139秒通过 |
| 架构 | 本轮更新、检查、架构及仓库守卫、53项工具测试与Mermaid渲染通过 |
| CodeBoarding | 匹配基线preflight通过；有界增量重跑109.777秒通过并保存可信检查点；前次timeout的usage_unknown1及134379未知预留独立保留 |
| Web检查沿用 | 未变Web沿用v105构建及9组浏览器回归，本轮未重跑前端检查 |
| 部署／真实验收 | 2026-11-12 04:01:35仅部署API成功；04:08:32.750一次申请获原生ACK，只读核对观察到好友关系；未执行审批，私聊证明未发送，完整好友接入与最终绑定待验收 |

隔离PostgreSQL测试仅使用`wsl_test`；据主代理通知，测试库已恢复停止。
本轮冻结清单、检查及构建证据在`var/controlled-applicant-v108-20261112/`，
其余检查沿用上次已报告的冻结结果，不重复计为新测量；最新增量分析与部署成功
分别以`analysis-incremental.json`、`analysis-incremental.log`及`delivery-after.json`为据。

分析尝试及费用保留：

- 首轮工具下载耗时后中止，464.693秒，requests0、reserved_tokens0、
  reported_tokens0、usage_unknown0；失败JSON及日志保留为
  `analysis-incremental.*.previous-1794426682616`。
- 第二轮`gpt-6.1-sol`供应商timeout，154.615秒，requests1、
  reserved_tokens134379、reported_tokens0、usage_unknown1，provider_failures.timeout1；
  失败JSON及日志保留为`analysis-incremental.*.previous-1794427094638`。
  实际用量与费用未知，reported_tokens0不能记为零费用，也不能清除该未知记录。
- 匹配基线preflight再次通过（0.709秒）。按此前授权启动的有界增量重跑
  session98940现已109.777秒通过；实际日志记录requests1、reserved_tokens134379、
  reported_tokens33531、usage_unknown0、checkpoint_saved=true、baseline_restored=false。
  `gpt-6.1-sol`模型、基线及设置未改，可信检查点已保存。本次成功运行的usage_unknown0
  仅指本次，前次timeout的usage_unknown1及134379未知预留不冲销、不记为零费用。

`delivery-after.json`确认北京时间2026-11-12 04:01:35部署成功，仅更换API，
镜像摘要`sha256:32ee6605f656666285fdbe121a2ab2db0270921e93d4ba7a1ab9505d8bcfb729`，
容器ID前缀`9dd7aab2`。Web`18681ceb62b2`、热群及PostgreSQL容器ID未变，
四服务healthy；无新增迁移，schema070、配置与`native_ready=false`、
`ai_ready=false`保持，原生接收连接随API重启。
部署前通知中“仍运行v107、无新增scope或challenge”为当时快照；
部署后主代理已开始真实新轮，以下进展以主代理本次通知为据，时间均为
2026-11-12北京时间。

- scope`37bb3582-bb76-4306-bba8-72cb4082d5ee`，原定期限10:03:45，
  现已确认closed、version2。
- 挑战`60aec537-cb36-4c92-bc24-edacf6cf253b`，期限04:15:25。
- 一次申请窗口`d01e8fcc-d661-46fa-87eb-f3bd6998121f`。
- 操作`e17cd944-6a9c-47e7-b421-58dbf3c3d696`于04:08:32.750成功，
  结果`native_friend_request_acknowledged`；随后只读reconcile得到
  `friends`／`native_friend_relationship_observed`。

接收方列表没有新待同意项，实际直接建立好友关系；未执行好友审批，不记为审批通过。
正常向导切换“已是好友”后取得fresh私聊码，创建私聊证明窗口
`7651ba89-ccc6-4f95-aa08-134eb91fbedd`成功（0／1），尚未发送。
MessageCenter发送准入返回HTTP409、`native_private_message_invalid`。
主代理真实只读保留的路由候选为event289926：`ContentType1201`、`MsgFrom200`、
`Session1`，方向按主代理记录为`sender968 -> recipient312`。
event289925为`ContentType2200`已读通知，本轮不作为fallback。
类型语义依据固定OpenIM SDK3.8.3常量：1201为friend-approved，1204为friend-added；
通知类型名不代表本轮执行过好友审批，通知本身也不作为独立私聊证明。
event289926未被v108的`ReadOutboundSource`接受，形成首次私聊证明的循环依赖。

v109候选允许在有live受控私聊proofwindow时解析1201／1204的路由，普通发送继续拒绝；
stage／final重核sealed事件及scope，已提交unknown的只读核对独立，关闭或到期后仍可查。
据主代理通知，4组新增PG回归已通过，control无并行负载复跑31个顶层176.536秒通过；
首轮NearExpiry／wsl断言失败、writes0及两秒scope到期证据保留，不改写为通过。
HTTP99个顶层1166.811秒一轮通过、未timeout，gateway26个顶层4.059秒通过。
v109已于05:31:17仅部署API，最终检查及交付详见上节；
独立私聊／群证明及最终绑定仍未验收通过。

v107真实操作`4e84e6c5-3680-40fa-8918-8a6925831b7d`的failed1保留，
一次预算已耗尽，不自动重发。据主代理最新确认，范围（ID前缀`15d21bd7`）
已closed、version2，范围计数原生使用1、模型0；该使用计数不代表好友发送成功。
范围模型0仅为该scope计数，架构分析的未知用量与费用仍按上述记录独立保留。
上述v107轮未完成好友同意或独立私聊／群证明；v108本轮未执行审批、私聊证明未发送，
完整好友接入与最终绑定仍待验收，不记录认领完成。

## v107 辣椒聊好友查找响应兼容与API交付（2026-11-12，北京时间）

v106真实查找已越过受控授权上下文检查，但在响应shape解析阶段失败；当次无原生
mutation、outbound持久记录0。该轮范围经UI关闭至version2，窗口used0／1后取消。
本版兼容`isBanned`规范表示，并仅将SDK明确成功的空好友结果识别为“尚非好友”；
封禁、非法结构及请求失败继续拒绝。辣椒聊查找诊断补充限定的`search`／`friend_info`
阶段、分类及实际客户端错误码映射，不暴露查询账号、原始响应或上游错误文本。

运行代码涉及`internal/lajiaoliao/onboarding_outbound.go`、
`internal/httpapi/group_binding_outbound_adapter.go`及
`internal/control/group_binding_lookup_error.go`；发布另含三项合成测试文件及
生成manifest，准确七文件清单记录于本轮`delivery-after.json`。

| 检查 | 最终结果与限制 |
| --- | --- |
| race/sodium | 765个顶层测试90.642秒通过 |
| focused HTTP | 33个顶层测试217.792秒通过 |
| focused control | 27个顶层测试46.852秒通过 |
| focused gateway | 26个顶层测试3.122秒通过 |
| 编译／API构建 | 编译29.237秒、API构建43.452秒通过 |
| 容器smoke | 38.001秒通过 |
| 架构 | 本轮更新、检查、架构及仓库守卫、53项工具测试与Mermaid渲染通过 |
| CodeBoarding | 匹配可信基线的增量97.845秒通过；实际`analysis-incremental.log`记录requests1、reported_tokens31936、usage_unknown0、checkpoint_saved=true，baseline_restored=false |
| Web检查沿用 | 未变Web沿用v105构建及9组浏览器回归，本轮未重跑前端检查 |

新增合成覆盖包括`isBanned`规范值与非法值、明确成功空好友结果与失败响应的区分、
辣椒聊诊断阶段及实际客户端错误码分类；合成测试不操作真实账号或群。
隔离PostgreSQL仅使用`wsl_test`；上述为最终检查结果，前轮产物保留于`.previous-*`，
不把较早版本的结果写为本轮最终测量。

`var/controlled-applicant-v107-20261112/delivery-after.json`确认于北京时间
2026-11-12 03:09:59部署成功，仅更换API，镜像摘要为
`sha256:61e33d80e17200b61aaeeefbaec3f413c743f4649c69f2210264a6b0cbe5f510`。
Web`18681ceb62b2`、热群及PostgreSQL容器ID未变，四服务healthy；无新增迁移，
schema保持070，配置保留。原生接收连接随API重启，
`native_ready=false`、`ai_ready=false`保持。

据主代理本次真实验收通知，v107已成功lookup并确认唯一目标3129316。
操作`4e84e6c5-3680-40fa-8918-8a6925831b7d`在发送预检
`self_friend_apply_list`返回shape解析失败；failed1、unknown0，一次预算已耗尽，
不自动重发。尚无原生mutation、好友同意或独立私聊／群证明，最终绑定未验收通过。
挑战（ID前缀`f278afe2`）已过期；范围（ID前缀`15d21bd7`）此前点击关闭后，
本次已由主代理确认closed、version2，范围计数原生使用1、模型0。

v108候选的空申请页兼容与`sent_requests`阶段安全诊断修复已冻结，尚未部署，
详见上节；本节不将候选工作记为已交付，不记录完整好友接入或认领完成。
实现、最终检查、增量分析及部署前后证据均位于上述v107目录。

## v106 受控好友队列授权上下文与API交付（2026-11-12，北京时间）

v105真实续接的好友目标查找被`group_binding_outbound_controlled_only`拒绝，
当次未发原生请求、窗口used0；该次受控范围随后经UI关闭至version2。
定位为实际`gateway.ReadScheduler`使用后台work context时丢失私有control context。
本版在`internal/control/group_binding_outbound.go:164`与
`internal/control/group_binding_outbound_submit.go:271`，向work显式附加已验证的
受控grant，保留work的取消和期限；原有最终授权栅栏继续执行。
发布差异仅这两个Go文件、`internal/httpapi/onboarding_controlled_queue_test.go`
及`docs/architecture/generated/manifest.json`。

| 检查 | 实际结果与限制 |
| --- | --- |
| 队列负对照 | 旧版经实际ReadScheduler复现HTTP409及`group_binding_outbound_controlled_only`；`queue-regression-before.json`保留失败，`queue-negative-control.json`确认预期失败已观察到 |
| 队列合成回归 | 2个顶层／6个子用例125.850秒通过；覆盖双平台lookup／send／重复confirm及排队期间scope close／cancel |
| 最终focused HTTP | 95个顶层测试826.518秒通过 |
| 最终focused control | 27个顶层测试45.693秒通过 |
| 最终focused gateway | 26个顶层测试2.996秒通过 |
| race/sodium | 759个顶层测试89.558秒通过 |
| 编译／API构建 | 编译96.628秒、API构建重试107.620秒通过 |
| 容器smoke | 73.733秒通过 |
| 架构 | 本轮更新、检查、架构及仓库守卫、53项工具测试与Mermaid渲染通过 |
| CodeBoarding | 匹配可信基线的增量250.723秒通过；实际`analysis-incremental.log`记录requests1、reported_tokens33113、usage_unknown0、checkpoint_saved=true，baseline_restored=false |
| Web检查沿用 | Web源码与运行容器未变，沿用v105 Web构建及9组浏览器记录，本轮未重跑前端检查 |

合成协议回归不操作真实账号或群，隔离PostgreSQL测试仅使用`wsl_test`。
首次API镜像构建因DockerHub匿名token请求EOF失败，11.042秒退出；
`api-build.json.previous-1794419647690`及同名历史日志保留，重试成功不改写首次失败。

`var/controlled-applicant-v106-20261112/delivery-after.json`确认于北京时间
2026-11-12 02:16:22部署成功，仅更换API，镜像摘要为
`sha256:a23162790bc373334298c0dc3565ffb7a96820bbed5eb0f0b9dce5cead31e69f`。
Web`18681ceb62b2`、热群与PostgreSQL容器ID未变，四服务healthy；无新增迁移，
schema保持070，配置文件字节保留，`native_ready=false`、`ai_ready=false`不变。
原生接收连接随API重启；此处记录API交付，不记为真实好友流程通过。

主代理正在进行真实完整好友接入验收。真实好友申请、独立私聊／群证明和最终绑定
尚在执行，未记为通过；本节合成回归、构建、分析与部署成功不替代该验收。
最终检查、负对照、首次构建失败及部署前后证据均位于上述v106目录。

## v105 好友受控候选展示与前端交付（2026-11-12，北京时间）

真实续接准备发现TestTools好友隔离群选项仍遍历普通`groups`，排除了仅来自受控
范围的候选。ROOT `web/src/wsl/TestTools.vue:428`改为遍历`testGroups`；
当普通`groups`为空但存在有效受控候选时，两平台均可展示该候选。
发布差异仅该Web文件、`tools/onboarding_applicant_ui_smoke.js`与
`docs/architecture/generated/manifest.json`，不包含Go或迁移改动。

| 检查 | 实际结果与限制 |
| --- | --- |
| 受控申请界面回归 | 70项合成检查通过，包含`wsl`／`lajiaoliao`仅有受控候选且普通`groups`为空的场景；原生请求0、外部请求0 |
| 浏览器 | 本轮9组全部通过，为合成界面检查，不代表真实好友／证明／绑定通过 |
| Web构建 | 117.117秒通过 |
| 容器smoke | 50.988秒通过 |
| 架构 | 本轮更新、检查、架构及仓库守卫、53项工具测试与Mermaid渲染通过 |
| Go／隔离PostgreSQL | 发布差异严格限于上述Web、合成测试与生成manifest；复用v104的race/sodium、focused control、focused HTTP及postgres-control通过记录，本轮未重跑Go／PG |
| CodeBoarding | 匹配可信基线的增量223.041秒通过；实际`analysis-incremental.log`记录requests1、reported_tokens32271、usage_unknown0、checkpoint_saved=true |

保留分析首轮失败：工具下载期间超时605.280秒，requests0、reported_tokens0、
usage_unknown0，失败JSON及日志的`.previous-1794417456676`记录保留。
重试仅复用v104工具缓存，未替换基线或state；成功增量保存可信检查点，
不把首次超时改写为通过，也未回退为全量分析。

`var/controlled-applicant-v105-20261112/delivery-after.json`确认于北京时间
2026-11-12 01:23:16交付成功，仅更换Web至`18681ceb62b2`且healthy。
API仍为`592eaba0051c`，API、热群与PostgreSQL容器均未更换且healthy；
schema保持070，无迁移或原生连接重启，配置、CodecVersion7及
`native_ready=false`、`ai_ready=false`保持。

续接沿用此前记录的6小时受控范围、两测试账号临时动作准入均true及申请人968
出向好友gate开放的准备状态。真实好友申请、独立私聊／群证明及最终绑定由主代理
测试中，本节仅记录前端交付与准备，不记为真实闭环通过。
本轮检查、分析首轮失败及成功重试、部署前后证据均位于上述v105目录。

## v104 受控申请人与独立证明交付（2026-11-12更新，北京时间）

以当时实际运行v103及冻结源码为本轮基线。v104提供受控申请入口，允许超管在有效
接入范围内使用指定的另一自有号池账号提交一次申请，接收端继续走普通来向好友向导。
父挑战的接收账号、网站身份和原生证明不因测试发送而改写。私聊和群证明各有独立的
一次窗口及精确正文约束，复用既有持久发送和最终校验；不向助手、热群或客户开放。

本版另修复最终提交期间绑定机器人、绑定版本、账号epoch／版本、范围或当前角色
变化时的拒绝；不因已准备申请而继续发送。具有好友接入尝试的挑战必须由独立私聊
接收证明建立身份，群证明及最终绑定同时复核，不能用已有群内消息跳过好友阶段。
既有“机器人已在群、未发生好友接入尝试”的独立分支保持原规则。

| 检查 | 实际结果与限制 |
| --- | --- |
| 首轮冻结候选 | 编译25.495秒、接入HTTP专项53项485.430秒通过；不是最终版本全部检查通过 |
| race/sodium | 原路径恢复后757项55.202秒通过；保留此前冻结候选757项85.597秒及绑定栅栏修复后的退出0记录 |
| 串行隔离PostgreSQL control | focused control最终27个顶层测试176.715秒通过；仅使用`wsl_test` |
| 串行隔离PostgreSQL HTTP | 初轮60个顶层测试通过后触发20分钟整轮超时，无用例断言失败；精确补跑剩余6项131.247秒通过，合计66项覆盖完整，超时失败记录保留；仅使用`wsl_test` |
| 恢复副本诊断 | 此前14组／96用例、140.491秒通过，含双平台绑定版本及主机器人变化、独立私聊和Messages证明窗口；不替代最终发布检查 |
| API／Web构建 | 最新API91.324秒、Web7.343秒通过；保留此前类型检查及构建74.019秒记录，不作为最新检查结果 |
| 浏览器 | 最新9组全部通过；此前恢复源码发送准入25.204秒、申请流程77.039秒、一次确认40.263秒及双平台四视口等诊断记录保留于`diagnostic-browsers.json` |
| 架构 | 最新架构检查、守卫、53项工具测试及Mermaid渲染通过；不等同于全部发布门槛通过 |
| CodeBoarding | 原路径恢复后，匹配可信基线的增量111.507秒通过，requests1、reported_tokens32280、unknown usage0，可信检查点已保存；首次来源核验失败另行保留 |
| 容器smoke | 59.925秒通过，冻结源码未改写 |
| 发布状态 | 2026-11-12 00:33:28部署v104，迁移070已应用；API`592eaba0051c`、Web`fb10d6e7533e` healthy，热群／PostgreSQL容器未更换且healthy，配置与readiness保持；真实新好友闭环未验收 |

保留失败：隔离库最初未启动；最终栅栏专项首先暴露绑定主机器人变化漏检，修复并
补充版本用例；合成普通窗口使用`clock_timestamp()+5分钟`，与默认事务创建时间
不一致，触发300秒约束，改为`now()+5分钟`后上述96用例通过。测试容器首次缺少
`/tmp`执行权限、iCloud挂载读取截断等基础设施失败保留；未削弱产品期限约束。
最新focused control首轮还因构建重负载导致两秒期限提前过期而失败；最终27个顶层
测试176.715秒通过，首轮失败不改写为通过。HTTP初轮整轮耗时1211.817秒，
触发20分钟测试期限；`applicant-focused-httpapi-timeout.json`保留失败结果。
`applicant-focused-httpapi-remainder.json`记录精确余下6项通过，
`applicant-focused-httpapi.json`核对列出66项与初轮60项、补跑6项的完整覆盖，
不表述为单轮无超时通过或全量HTTP测试通过。

首次部署遗漏既有Compose `migrate`服务，部署校验发现schema仍为069，自动回滚
至healthy v103。`delivery-failure.json`保留该次失败及旧镜像恢复记录，
`delivery-before-retry-1.json`确认重试前v103四服务healthy。重试先运行既有
`migrate`服务应用`070_controlled_onboarding_applicant.sql`，再部署API／Web；
`delivery-after.json`记录北京时间11月12日00:33:28成功，仅API／Web更换，
配置、readiness及上一版回滚资源保留；原生接收连接随API重启。
该部署记录的新增原生操作与模型调用均为0，不代表真实新好友流程通过。

### 文件系统事件、恢复与收尾

11月11日约22:30后冻结源及Git文件出现stat非空而读取0字节的情况；随后原路径
`/Users/xinxinzi/Documents/ChatGPT/旺商聊bot 2`和原iCloud路径消失，只能找到
`/Users/xinxinzi/iCloud云盘（归档） - 1/Documents/ChatGPT/旺商聊bot 2`的部分文件。
当时归档缺少原`.git/HEAD`、配置、分支引用及部分v103冻结源。首次CodeBoarding
增量发起1次请求、reported_tokens33534、unknown usage0，结束来源核验失败，
未保存成功可信检查点，该轮仍不判通过。未新建Git、重标分析基线、修改既定部署
目标或把镜像构建成功当作发布通过。

已在`/Users/xinxinzi/.cache/artwslbot/controlled-applicant-v104-20261111-recovery/`
保存1661文件、15141286字节的本地恢复副本及逐文件SHA-256清单；其用途明确仅为
本地恢复，不是可信分析基线。后续测试夹具修复和本节文档更新另行保存，不改写旧
清单。`diagnostic-postgres.json`及浏览器记录只作为恢复副本上的诊断证据。

原工作路径现已恢复真实Git提交`f6692ec09b9f9ed9e9416aee21b09522c716371f`
及原index，归档13处差异保留，1661文件恢复。v103冻结源缺失两项从同版
`analysis-source`补回；已有stage逐字节核验44项窄差异范围通过。
原路径与来源恢复的环境阻塞已解除；本轮未新增hash／manifest，也未重标可信基线。
最新匹配基线增量已通过并保存检查点，HTTP专项覆盖、容器smoke与v104部署现已
分别完成；真实新好友闭环仍未验收，不能把恢复或部署成功视为真实流程通过。

部署前历史收尾只读核对：当时v103 API／Web／热群／数据库healthy，schema069。旧接入范围已关闭，
有效／未决范围0，原生及好友未决操作0；两测试账号动作准入均false、接收仍connected。
测试布尔开关保持原true，来向好友准入保持既有版本9，该次收尾未另外扩展。
当时未发生新好友申请、审批、私聊或群证明，没有业务模型调用。

当前续接准备状态：已有6小时受控范围开启，两个测试账号临时动作准入均true，
申请人968的出向好友gate已打开，仅用于准备；当前无unknown原生记录。
真实UI尚未发出好友申请或执行审批，私聊／群证明与最终绑定闭环仍未验收。
真实界面另发现TestTools好友分支的`v-for`使用`groups`而非`testGroups`，
导致受控候选被排除；主代理正在以v105前端修复另行检查、发布，不计为v104真实验收。
禁止复用旧证明、申请额度或重放unknown操作。

原候选日志位于`var/controlled-applicant-v104-20261111/`；最终本地诊断、截图
`controlled-cleanup.png`及恢复说明在上述本地恢复目录，该截图仅代表部署前收尾。
v104交付以`delivery-after.json`为据，`native_ready=false`、`ai_ready=false`
及CodecVersion7不变。

## v103 独立好友调试授权与真实删除（2026-11-11，北京时间）

修复独立`native_debug`好友操作被旧认领范围关闭误阻断的问题。仅在非业务入口、
有效独立测试窗口且动作为`friend_delete`或`friend_approve`时，不再消费或要求
群认领测试范围。窗口本身的超管身份、唯一账号和目标、一次预算、绑定、接收、
当前角色、账号及绑定epoch、期限、幂等、串行和最终提交校验继续保留。
普通群管、业务、助手及package授权不获得该例外。

以冻结v102源码为基线，仅发布`native_settings.go`窄修复、专项测试和架构证据。
根工作区其他未发布功能范围检查及解析、媒体、轻量化候选未夹带。

| 检查 | 本轮实测结果 |
| --- | --- |
| race/sodium | 732项通过，87.079秒 |
| 串行隔离PostgreSQL | control130项通过，449.770秒；接触生产数据的仅为后述独立真实验收 |
| 授权专项集成 | control14项、HTTP3项通过；覆盖双平台两类好友操作、关闭范围后独立授权、提交中绑定变化、一次预算及幂等 |
| 前端与浏览器 | 产品类型检查和构建、六项浏览器回归通过；前端镜像未变化；含双平台四视口和100／300组合成候选 |
| 架构与交付检查 | 架构更新／检查、依赖及仓库守卫、53项工具测试、Mermaid渲染、容器smoke通过 |
| CodeBoarding | 匹配可信基线的增量通过；首轮242.367秒、1次请求、reported_tokens32565、unknown usage0；最终24.277秒、请求0、可信检查点保存 |

保留首轮失败：测试夹具混用事务时钟与墙钟导致期限约束失败，已改用事务时钟；
原父取消测试要求250ms内完成数据库准备并进入模型，广覆盖复跑时不稳定，
已改为验证完整父期限传递并在合成模型内主动取消，仍验证单次调用及未知费用冻结。
前端依赖和Git文件受iCloud按需下载阻塞；以相同锁文件离线复用790包、下载0，
仅终止阻塞构建元数据读取的Git子进程，未修改Git状态或产品依赖。
上述失败和最终通过证据均保留，未将失败轮次改写为通过。

19:28:29通过现有Compose仅更新API至`sha256:db6277d50d4fed08dd656972844d0b083f45172d3c3bc333f1e50326d3232ec9`。
Web仍为`b32366dea914`，热群与PostgreSQL容器未重建；配置、凭据、资源、挂载和运行模式
保持，四服务healthy。schema069、CodecVersion7及readiness=false不变。
上一v102 API镜像与回滚配置保留。部署本身新增原生操作和业务模型调用均为0。
隔离测试库已停止，数据卷保留。

### 真实单次删除与新申请准备

- 本人明确允许tsx（9680209）删除3129316现有好友关系一次。读取完整好友列表2人、
  核验当前群主身份后，经独立一次授权及确认提交，19:43:43操作
  `48062e94-2239-40b7-bc66-a3d22950cb05`成功。原生确认后完整列表1人，
  3129316不再是当前好友；页面只读核对显示期望状态。未退群、拉黑或自动重加。
- 成功窗口仅用1／1次，随后正常界面取消，9680209动作准入恢复关闭。
  之前确认期间30秒读取证据过期，窗口`7b7a4a2d-7308-4119-9806-e55afb9e61a7`
  used0／budget1，未产生原生操作；该UI自动关闭确认后等待状态未解除，
  重载后恢复。本轮未发布该前端等待状态修复，作为已发现待修项保留。
- 为接续正常新申请建立范围`1d2f0ce4-8a1f-421c-a11b-47ca5dfa9e72`，
  固定接收机器人3129316、申请人9680209、测试群72567012，
  有效至2026-11-12 01:46:36。两端测试开关开启，动作准入关闭；
  此范围原生／模型用量均0，管理及热群业务未开启。
  来向审批恢复开放至版本9，仅供正常向导中本人确认单条申请。
- 19:55:57从正常用户中心创建新挑战
  `38a2c5d4-022c-4204-96d4-bbca2c7e67b0`，waiting／version1，
  20:05:57到期。已选择“我添加机器人”并填写tsx，未在新样本到达前读取或审批；
  页面保留等待本人App发送3129316新好友申请。截图`fresh-friend-ready.png`。
- 真实新好友申请、正常向导同意、独立私聊与群证明仍须使用新的10分钟挑战继续，
  不以此次删除或此前已在群证明判为完整认领通过。原目录缺列表诊断未因本次窄修复
  自动消除；新申请后仍须核对实际返回。

冻结源码、检查、回滚及截图：
`var/contact-grant-v103-20261111/`，真实删除截图`live-delete-result.png`。

## 新好友认领：辣椒聊来向申请阶段（2026-11-11，北京时间）

本轮只认“新来向好友申请 → 向导核对并由本人确认同意 → 独立私聊证明 →
群证明 → 本人确认绑定”为目标。两自有账号分别为申请人9680209、接收机器人
3129316，唯一测试群72567012。此前这两个账号互加及直接群内证明的记录不复用。

- 06:39:11建立六小时范围`f2f0d97e-a360-4e1c-a35a-7451abcc9a4e`；
  06:59:22从正常用户中心接入向导创建独立十分钟挑战
  `b7a9bf05-d31c-4728-931c-cca9fd429f70`，选择“我添加机器人”并手动核对一次。
- v99报“好友申请目录格式暂不兼容，未接受任何申请”。该操作仅查询来向申请目录，
  失败发生在解析阶段，未到昵称／时间匹配；无法据此认定原生端没有待处理申请。
  实际上游正文未采集。挑战07:09:22到期，身份、群证明事件均为空。
- 07:20:47关闭范围；临时来向好友准入关闭至版本4，两账号测试和动作开关均恢复关闭，
  接收保持connected。未同意、拒绝、删除或发送好友申请；无私聊、群证明或重新绑定。
  持久原生操作和好友尝试数量未增加，业务模型调用未增加。
- 窄修复：辣椒聊来向接入解析接受固定SDK可表达的显式`friendRequests:null,total:0`
  为空目录，仅返回未找到，不产生证据。缺字段、未给总数、矛盾总数仍拒绝。
  原接收身份、时间窗、完整性、重复申请及提交校验不变；此改动不宣称已查明实际响应内容。
  测试页局部复用隔离群候选以准备范围和读取角色，不改变用户中心授权群的来源。

| 检查 | 结果 |
| --- | --- |
| 候选代码 | race/sodium 729项；串行隔离库接入专项26项；API构建、容器smoke、架构检查、仓库守卫、架构工具与Mermaid通过 |
| 不变的前端 | 构建和六项浏览器回归通过，Web源与本轮API候选中的对应源码逐字节一致 |
| 隔离库广覆盖 | control129、HTTP231、gateway111项；HTTP首轮总时限前通过183项，补跑47项，显式运行可选合成回放1项；回放11000条提供、持久和ACK均一致 |

保留测试基础设施的初次失败：HTTP总时限、可选回放写入只读挂载、
回放种子占满后续网关号池。补跑只使用`wsl_test`，重定向回放报告到测试专用目录，
仅清理该次合成回放数据后通过网关检查；生产库未执行修复SQL。隔离库已停止。
候选源和证据分别在`var/friend-claim-v100-20261111/`及
`var/friend-directory-v101-20261111/`。首轮真实向导截图为
`var/friend-claim-v100-20261111/chili-friend-format-error.png`。

- 07:40:15通过现有Compose更新v101 API和Web。前一镜像、配置、资源限制和
  schema069保留，四项服务healthy，`native_ready=false`、`ai_ready=false`。
  匹配的CodeBoarding增量首次冷启动超时、未发供应商请求；暖启动复跑通过，
  一次请求、可信检查点保存。未执行全量分析。
- 07:45:04从正常入口重新建立同一对账号的范围，07:46:42建立新挑战。上线后
  只读核对一次仍报格式不兼容，证明真实返回不属于已兼容的显式空页。
  取消本次挑战，07:50:37关闭范围，审批恢复关闭（版本6），两账号测试和动作
  开关恢复false。复测前后原生操作86、好友核验尝试2、外向好友尝试3、模型调用63，
  本轮未新增。第二次截图：`var/friend-directory-v101-20261111/chili-live-format-error.png`。
- v102诊断候选基于冻结v101，仅变更辣椒聊来向目录校验的错误分类、API映射和页面文案。
  区分无目录数据、缺列表、分页矛盾、接收人不符及记录格式异常；不保存上游正文，
  不将异常视为空目录或有效证明。双平台的合成受控测试已走完唯一申请、单次同意、
  独立私聊证明、独立群证明和绑定，并验证拒绝其他发送人／群及重复提交。
  本候选通过race/sodium 731项、串行隔离库接入专项27项、前端构建、六项浏览器检查、
  架构与仓库守卫、Mermaid及容器smoke。隔离库已停止，卷保留。
  CodeBoarding可信基线预检通过，首次增量于本地工具启动阶段超时，模型请求0。
  复跑前本机Git引用及分析白名单处于iCloud未下载状态；解锁后固定下载原文件，
  与已批准白名单逐字节核对一致。暖启动增量107.476秒通过，上游请求1、
  reported_tokens32626、unknown usage0，可信检查点保存；未切换模型、设置或全量分析。
  冻结源码、检查及首轮失败证据保留在`var/friend-diagnosis-v102-20261111/`。
- 13:16:46通过现有Compose发布v102，API镜像`3fa9457f0980`、
  Web镜像`b32366dea914`。只重建API和Web，热群及数据库保持；
  环境、凭据、挂载、资源限制、运行模式及schema069不变。四服务healthy，
  上一v101镜像与回滚配置保留；发布本身未新增原生操作或业务模型调用。
- 13:20:56从正常入口建立同一对账号、同一群的新六小时范围
  `d00ab807-2cf4-4ac6-9fab-c877ee96eaa6`；13:23:31建立新挑战
  `846621e6-0c4c-48a4-b36d-5ac07adb6928`，填写完整昵称tsx，只核对一次来向申请。
  线上明确返回`group_binding_friend_directory_missing`：
  “平台响应缺少好友申请列表，未接受任何申请”。该结果只证明响应中未读到约定列表，
  不证明待处理申请为空、昵称不匹配或平台拒绝加好友；没有采集上游正文。
- 随后取消挑战、关闭范围（closed／version2），来向好友准入恢复closed／version8，
  两账号测试和动作开关均false，接收仍connected。有效范围0、未决原生及好友操作均0；
  原生操作86、好友尝试2、业务模型调用63均未增加，没有同意、拒绝、删除或重发。
  真实错误截图：`var/friend-diagnosis-v102-20261111/chili-directory-missing-live.png`。
  后续静态核对固定客户端及SDK的来向目录契约，尚未以猜测格式放宽校验。

**真实新好友阶段尚未通过。** 实际同意、独立私聊、群证明与绑定必须使用同一新挑战
重新逐阶段验证。旺商聊两接收器本轮只读检查仍blocked，不算完成。

## v99 辣椒聊真实受控认领与转移验收（2026-11-11，北京时间）

本轮按本人授权，使用现有正常接入向导完成唯一辣椒聊“测试群”72567012的
群主证明、本人确认、版本化跨租户转移审核及用户中心可见性验收。
9680209为实际群主，接收机器人3129316（原生昵称“我不知道啊”）已在群内，
因此走“已在群，直接群内证明”分支，不为制造样本删除好友或重复邀请。
证明类型为controlled_test，只适用于本超管演示空间，不计作普通客户正式认领。

| 阶段 | 本轮持久证据与实际结果 |
| --- | --- |
| 六小时范围 | `761873f5-095e-4513-8f23-fdfdbb761de5`，05:10:03创建，原定11:10:03到期，双方账号及唯一群固定 |
| 独立挑战 | `a4a6f0ba-8023-41ff-859c-3d8e6614529f`，05:16:02创建，10分钟期限独立于范围；不记录挑战正文 |
| 唯一实际发送 | 05:23:39，群主通过独立native_debug单次窗口发送群证明；操作`501cca8a-6c8a-4b91-a272-8548fb301160`为succeeded／native_acknowledged |
| 真实接收及解码 | 3129316于05:23:41持久接收事件337434，同秒解码完成，state=decoded、scene=group、CodecVersion7；正常接入扫描关联该事件并核验当前owner角色 |
| 本人确认 | 05:24:28进入review，没有把发送成功当成已完成绑定 |
| 版本化转移审核 | 05:25:26通过正常超管审核，将群从“本地隔离验收”转入“admin的演示空间”；挑战bound／version4、认领approved／controlled_test |
| 归属及配置 | 转移记录`cc98f357-2170-453d-be43-751f8bba574f`保留原绑定version7／epoch7及非空密封配置归档；新绑定version8／epoch8，自动群管和AI均关闭 |
| 实际界面 | 本人用户中心显示1个有效测试群及接收机器人，向导显示“绑定成功”；没有注册客户、购买套餐或以SQL制造证明、角色和授权 |

首个动作窗口`72f8ecba-c218-45db-aa57-87a3341b4899`因角色快照在确认期间
过期未形成持久原生操作，used0／budget1，随后通过正常界面取消。
重新核对当前角色后，建立窗口`82bce211-5490-4e57-a8df-5a48bd21e418`，
只允许send_text及一次尝试，最终used1／budget1。本轮实际原生变更总计1次，
没有重发已提交或结果未知的操作。已观察到陈旧快照会在确认返回后阻止提交、
但界面未立即解释；这是后续可改进的反馈点，本轮未修改该行为。

05:28:34经正常入口提前关闭范围，state=closed／version2，不延长或复活旧挑战。
关闭后归属证明state=paused／version2，绑定仍保留active=true的持久归属，
但version／epoch提升至9／9，management_enabled、ai_enabled、ai_requested均false，
business-runtime.enabled=false。用户中心刷新后显示0个可用群，符合受控证明关闭
后隐藏的规则；不是转移丢失，也没有自动转回原租户。保存的证明、配置归档、
转移记录及原租户历史继续保留，重新使用须新范围、新证明。

05:36后只读复核：两原生窗口均cancelled，保留各自used0和used1；
9680209及3129316的testing_enabled／connection_enabled均false，
接收器仍enabled／connected且采样新鲜。有效测试范围0，未决原生操作0；
05:07起新增原生操作仅上述1条，业务AI调用0、实际及冻结费用0。
没有自动审批好友、退群、删好友、重传、重新登录或启用自动群管／热群。
API、Web、热群及PostgreSQL四服务healthy，镜像仍为已发布v99；
本轮没有源码、迁移、依赖或Compose变更，也没有重新部署。

本轮新增证据是上述真实低频验收和收尾核对；race/sodium、隔离数据库、构建、
浏览器合成、架构及CodeBoarding检查沿用下节v99发布记录，不标为本轮重跑。
绑定成功截图：
`var/controlled-scope-6h-v99-20261111/chili-claim-bound-20261111.jpg`。

**通过范围：辣椒聊已在群分支的受控群主证明、本人确认、真实转移和关闭后隔离。**
新好友申请及独立私聊分支、本轮页面／助手动作和自动业务均未重测；
普通客户正式认领、旺商聊完整认领、双平台全部功能仍不能据此判定通过。
native_ready／ai_ready保持false。此前各次过期及失败记录按原事实保留如下。

## v99 受控接入六小时与手动开关（2026-11-11，北京时间）

按本轮明确要求，将新受控接入范围默认及最长有效期由30分钟改为360分钟，
到期仍由服务端关闭。保留提前关闭，已关闭记录增加“重新开启接入范围”入口：
预填原账号与群，重新确认后创建新编号、期限、证明和预算，不复活旧记录。
原生动作窗口及尝试预算不变，接入挑战仍独立10分钟；普通客户限制未放宽。
本轮没有创建真实范围、发送证明、审批好友或再次执行原生动作。

从实际v95 API／v98 Web对应冻结源派生
`var/controlled-scope-6h-v99-20261111/source`，最终仅16个范围内差异文件。
没有混入未验收的解析、轻量化、媒体或IM换票候选，CodecVersion保持7。
新增前向迁移069，仅拓宽新范围期限约束并记录请求的规范默认时长。
001至068、依赖锁、Compose和协议／接收实现与上一运行源逐字节一致。

审查发现省略有效期的旧请求会因默认值变化产生幂等冲突，已修复：
旧记录及旧二进制写入保留默认30，新写入显式360；相同幂等键重试使用原记录
默认值。新字段不可更新，原账号、群、期限等不可变栅栏继续生效。
旧记录不会因迁移延长，旧镜像可在保留069的情况下回滚运行。

| 检查 | 本轮实测 |
| --- | --- |
| Go race／sodium | 728个顶层测试通过，72.172秒 |
| 串行隔离wsl_test | control129项／327.214秒，HTTP230项／924.294秒，gateway111项／21.577秒，全部通过 |
| 时长及兼容专项 | 默认360、1／31／360边界、361拒绝、数据库超限拒绝、旧默认幂等、重复关闭／新开、原期限不可变通过 |
| 挑战与到期 | 10分钟挑战独立于6小时范围；模拟时钟5小时有效、6小时到期立即禁用通过，并非真实6小时持续运行 |
| 产品前端 | 类型检查及构建55.615秒通过；API构建32.128秒 |
| 浏览器六套件 | 受控组件65.799秒、入口12.943秒、工具14.107秒、接入74.907秒、消息12.157秒、账号6.100秒，全部通过 |
| 响应式及规模 | 两平台320／390／768／1440及100／300候选通过，含关闭后新开、迟到响应及普通接入滑块 |
| 架构及容器 | 冻结源update/check、架构及仓库守卫、53项工具测试、渲染缓存与Mermaid通过；容器smoke37.839秒通过 |
| 只读复核 | 子代理复核旧请求兼容和不可变约束，未发现剩余具体问题；未把静态复核当成测试执行 |

匹配现有gpt-6.1-sol可信基线的增量分析217.391秒通过，检查点已保存，
上游请求1、reported_tokens33487、unknown usage0。首轮602.093秒超时发生在
本地工具初始化与语言服务准备阶段，上游请求0；缓存准备完成后重试通过。
失败证据保留，没有全量初始化、模型切换、设置变更或跨命名空间替换基线。
分析仅接收批准白名单源码，不含运行数据、凭据、挑战或消息正文。

04:53:14既有Compose仅更新API及Web，应用兼容迁移069。API镜像
`sha256:6d0a7ed67932f6a04fcc5a58c4a9e6d876ad460754abb3f710552bf90b950819`，
Web镜像
`sha256:3e65ae9a970d7106ebb4ebb7d6c9511a3ce21744d8c06136afab30dbe7f354e2`。
环境、配置、资源、挂载、网络和运行模式保持；热群及生产数据库容器未重建，
保留上一API `77fcc6343a29`、Web `5e24d82a8e4c`及配置回滚。
部署前未决提交和有效范围均0，部署后旧范围的期限、状态及版本逐项一致，
新增原生操作0、业务模型调用0，业务运行仍0，native_ready／ai_ready仍false。

04:57线上HTML、JS、CSS与运行镜像逐字节匹配，四服务健康。
真实浏览器核对旧到期记录的“创建新接入范围”会预填同一双方账号、群及360分钟，
账号测试开关关闭时确认不可用；仅查看后取消，没有创建范围或改变开关。
用户原先保存的30分钟草稿继续保留，重新从旧范围创建则明确使用360分钟。
截图为本轮目录`live-six-hour-scope.png`，只读上线结果在`live-verification.json`。
两辣椒聊接收器enabled／connected且采样新鲜，测试开关仍false；
旺商聊接收器仍有独立阻塞，本轮没有夹带认证修复或进行登录。
隔离测试数据库已停止、数据卷保留。根工作区另行更新graph／manifest行证据，
check及架构守卫通过，不把根工作区其他候选当成已部署。

真实完整认领及版本化转移仍未通过，旧取消证明不复用；
本次通过的是六小时范围、开关、隔离及兼容交付，不替代认领验收。

## v98 受控证明审核入口修复与第二轮认领（2026-11-11，北京时间）

本轮额外一条证明授权已使用，未自动追加发送或续开范围。02:30:55创建范围
`22f26db4-a8ec-4deb-9715-82cba7b88c9c`，有效至03:00:55；02:35:41创建挑战
`3e7fe11f-3d88-4ff5-94ba-0f22b00d32c4`，有效至02:45:41。
9680209于02:38:00向唯一辣椒聊测试群72567012发送一次群证明，操作
`7dcd97b4-b406-4c94-812e-6f6a3ea8145b`成功且返回native_acknowledged。
独立窗口`f87b1afe-36d7-4066-8dcf-e26be71d1173`预算1、使用1。
接收机器人3129316于02:38:01收到事件335445，CodecVersion7完成解码。
正常扫描关联该事件、核验当前群主，并于02:41:35完成本人确认进入review。
此轮已通过本人确认，不能与上一轮未及时确认的过期结果混同。

继续真实审核时发现前端只将authenticated_message送入专用审核组件，
遗漏controlled_test，误开旧人工审核弹窗。服务端返回
authenticated_review_endpoint_required，未执行转移。修复仅增加受控证明的
专用组件分支；合成回归分别覆盖两平台受控转移、普通认证审核、人工审核、
版本冲突、明确转移确认和四视口。不放宽任何服务端身份或归属检查。

从实际v97冻结源派生`var/controlled-review-v98-20261111/source`，
最终发布差异仅App.vue、对应浏览器测试和生成manifest。根工作区已有的
登录按钮文案曾被带入初稿，差异检查发现后从候选剔除，保留其原工作区改动。
最终产品类型检查及构建46.302秒、完整接入审核回归78.603秒、
容器smoke39.784秒均通过。初稿六项浏览器套件均通过；剔除文案后重新执行
完整接入审核套件，不将初稿其余五项标为最终镜像上的新执行。
架构更新、检查、守卫、仓库守卫、53项工具测试及渲染通过。
后端与v97逐字节一致，明确复用已记录的v96 race/sodium及串行wsl_test证据，
本轮未重跑后端集成。初稿匹配基线增量384.504秒、2次上游请求、
reported_tokens66851通过；最终匹配增量13.359秒、上游请求0并保存检查点，
不能称作最终Vue差异获得新一轮模型内容审查。

03:01:47现有Compose仅更新Web为`5e24d82a8e4c`，保留上一Web
`2ab91ae5a05f`及配置回滚；API仍`77fcc6343a29`，API、热群、数据库容器未变。
无后端、依赖、迁移、配置、解析算法或密封格式改动。四服务healthy，
CodecVersion7及native_ready/ai_ready=false保持。

真实转移未完成：验证和发布期间，范围于03:00:56自动变为expired/version2，
挑战变为cancelled/version4，保留confirmed_at和群证明事件；审核人及审核时间为空。
认领记录`23d985bd-ab78-474e-a46c-edffa8ac6aa0`仍pending，但专用原记录已取消，
不能继续审核、复用或宣称通过。转移记录0；原租户
`7c3bce1b-976f-4845-b267-fb9c2abafa90`、绑定version7/epoch7均未变，
管理和AI均关闭。03:07只读核验有效范围0、未决执行0，两接收器connected，
本轮业务模型调用0、费用0。架构分析请求单独计数。

刷新后网站会话失效，登录滑块交还本人；本人完成后，于03:13确认登录恢复，
通过正常管理入口取消原窗口，再关闭9680209动作及两账号测试开关。
03:20只读核验：窗口cancelled=true、used1/budget1；9680209测试false/动作false、
epoch/version60，3129316测试false/动作false、epoch/version20。
两接收器enabled=true、connected且采样新鲜；有效范围0、未决执行0，
本次收尾新增原生操作0、本轮业务模型调用0。原群租户、绑定version7/epoch7
及管理和AI关闭状态未变，四服务仍healthy，运行镜像不变。
未再次发送证明、续开范围、删除好友或退出群；已发生的好友和入群效果保留。
发送账号开关还原截图保存在
`var/controlled-review-v98-20261111/sender-switches-restored-20261111.png`。
普通客户正式认领、双平台完整认领及真实版本化转移均未通过。

## v97 受控群文本入口修复与真实群证明（2026-11-11，北京时间）

从实际v96运行版及冻结源码派生
`var/controlled-text-scope-v97-20261111/source`。受控工具错误读取角色快照的
`present`字段，实际网关契约为`in_group`，导致新鲜且有效的群主快照仍无法开启
群文本发送。已修正Vue判断并补充明确的TypeScript响应类型；不放宽服务端角色、
账号、群、期限或预算检查。发布仅含3个运行／测试文件及4个生成架构文件，
无后端、依赖、迁移、Compose、解析算法或密封格式变更。

本轮新执行：产品类型检查及Web构建37.319秒、143项接入状态回归2.425秒、
受控工具浏览器12.848秒、完整接入入口12.230秒、受控接入组件57.431秒、
消息11.243秒、账号5.919秒、容器smoke37.893秒，均通过。
新增21项群文本准入场景，覆盖有效群主／管理员／成员、离群、未知角色、错误目标、
陈旧快照、旧字段及取消／过期；受控接入覆盖双平台四视口与100／300群候选。
合成回归不调用真实原生接口。首次旧夹具缺少旺商聊角色新鲜度字段，修正夹具后通过，
保留失败记录；未以修改夹具为由放宽产品校验。

后端、依赖及Compose输入与v96逐字节一致，因此明确复用v96的726项
race/sodium／58.893秒，以及串行隔离wsl_test control129项／300.571秒、
HTTP228项／874.108秒、gateway111项／21.544秒；本轮没有重新执行这些后端测试。
架构更新／检查、架构及仓库守卫、53项工具测试、Mermaid渲染和目录同步通过。
匹配可信祖先与设置的gpt-6.1-sol真实增量433.742秒通过并保存检查点，
上游分析请求2次、reported_tokens66830、usage_unknown0。分析调用独立于业务AI，
不能把本轮业务模型调用0写成全部外部模型请求0；未执行全量初始化替代增量。

01:50:37既有Compose仅更新Web为`2ab91ae5a05f`，API仍为`77fcc6343a29`；
API、热群及生产数据库容器ID保持，原生连接没有因本轮发布重启。
上一Web `e545fac1f1fe`及配置保留回滚。02:18线上HTML、JS及CSS与运行镜像
逐字节匹配，四服务healthy。首次资源比对因Node默认输出缓冲不足中止，
增大只读命令的输出缓冲后通过，未修改运行服务。CodecVersion=7，
native_ready／ai_ready仍false；没有混入其他候选。

真实验收使用额外明确授权的唯一一条群证明：辣椒聊9680209向测试群72567012，
接收机器人3129316，本人演示空间。
01:55:40建立范围`f7e18b53-183f-41d1-aeea-762e0635bada`；
01:56:37建立10分钟挑战`74e47892-f1fe-40f8-a1de-6881824ab1b8`。
先确认新鲜群主快照后创建单次native_debug窗口
`88ce9f4b-08c9-4d3f-a8af-9bc15f807231`，仅允许该账号、该群及send_text，
budget1。第一次确认时角色快照已过期，服务端未提交且used仍0；
重新只读核验后才提交，没有重放已发生的操作。

02:05:21持久操作`8780a6f6-e221-4a4d-8e93-7e03151029a0`
为succeeded／native_acknowledged；02:05:22接收账号3129316持久收到事件334888，
02:05:23解码完成，CodecVersion7。正常接入扫描将该事件关联为群证明并核验owner，
页面进入“待本人确认”。这是实际发送、接收及群主证明通过，不仅是发送回执。
代理未在02:06:37挑战期限内完成确认弹窗，记录最终expired／version3，
confirmed_at为空。无group_claims、ownership或转移记录引用该挑战，
故完整认领及版本化转移仍未通过；不复活挑战或复用该证明。

02:08:51主动关闭本次范围，单次发送窗口取消但保留used1／budget1。
两账号测试及动作开关恢复false：9680209为epoch／version56，3129316为18，
两接收器仍connected。02:19只读复核：有效范围0、未决操作0、运行中群业务0，
本轮发布后新增原生操作恰为上述1条。测试群仍属本地隔离验收租户，
绑定version7／epoch7、管理及AI均关闭。范围自身原生计数0不代表没有真实发送：
这条发送由独立受控工具窗口记账。业务模型调用和费用0；截图保留于本轮var目录。

本次额外单条额度已消耗，不能自动补发新的挑战消息。继续确认及转移须先取得
一条新鲜证明的额外额度，再先准备角色和发送窗口，最后创建挑战、发送并及时确认。
独立MessageComposer入口此前返回internal_error，原请求核对未保存意图且未提交；
该独立入口的根因尚未解决，v97仅修复上述TestTools契约，不声称全部发送入口已修复。
旺商聊大蛤的im_token换票1200仍为独立阻塞，未发布RenewIM候选，
未进行登录、短信、旺商聊真实动作或普通客户正式认领验收。

## v96 直接群证明入口与辣椒聊恢复入群（2026-11-10至11日，北京时间）

从实际v95运行镜像和冻结源码派生
`var/onboarding-direct-group-v96-20261110/source`。发现向导把群挑战显示错误地
依赖好友证明，导致机器人已经在群内时仍必须先走私聊。现改为未完成、未终止、
未取得群证明时即可显示独立群挑战，并明确区分已在群和仍须邀请的场景。
仅显示核验内容不构成身份或归属证明；确认、邀请、原生角色及服务端最终栅栏不变。
发布差异为1个Vue、2个浏览器测试和2个生成证据文件，无后端、依赖或迁移改动。

本轮实测：143项接入状态用例、race/sodium726项／58.893秒通过；
串行隔离wsl_test control129项／300.571秒、HTTP228项／874.108秒、
gateway111项／21.544秒通过。产品类型检查及Web构建58.718秒、
容器smoke36.989秒通过。10项浏览器套件全部通过，覆盖双平台、
320／390／768／1440视口、直接群证明、100／300候选及既有消息、账号入口。
完成按钮断言原误用“关闭”，改为实际“完成”并验证不增加写入；
旧测试把挑战可见误当作已证明，改为检查持久证明缺失、确认和邀请不可用。
初始失败日志保留，没有放宽产品权限。

冻结候选架构、仓库守卫、53项架构工具测试、渲染和目录同步通过。
根工作区另行更新行证据；默认vendor检查失败，使用现有架构venv及
`GOFLAGS=-mod=mod`后update/check、两项守卫及功能目录同步测试通过，
保留其他候选和人工架构决定。匹配可信祖先及配置的gpt-6.1-sol增量
9.540秒通过，检查点保存，上游请求0；Go白名单未变化，不声称模型审查了Vue语义。
以上为本地执行及咨询分析，没有据此声称远端CI或真实认领通过。

11月10日23:53:00既有Compose仅更新Web为`e545fac1f1fe`，API保持
`77fcc6343a29`，API、热群、生产数据库容器ID及原生接收归属未变。
配置、凭据、资源限制、运行模式、数据库结构及CodecVersion=7均保留；
上一Web `1fee4df67417`和配置留存回滚。线上HTML／JS／CSS与验收镜像逐字节一致，
没有夹带此前解析、轻量化、媒体或未验证的IM换票候选，readiness仍false。
冻结源、预检、部署前后不变量和完整检查记录在本轮var目录，隔离数据库已停止、卷保留。

真实辣椒聊验收使用唯一测试群72567012。完整好友读取确认9680209与3129316
当前为好友，群主角色已核验。23:42:42仅邀请3129316一次：
`fd308f7b-5012-4f62-a5b2-d8b59711809f`为succeeded／
`native_invitation_requested`；平台仍要求接受，邀请回执未当作已经入群。
23:48:24由3129316接受原邀请一次：
`c099e1a8-ca26-41f1-873c-315ab4c21cf0`为succeeded／
`native_invitation_accept_acknowledged`。完整名单回读和23:57:11持久角色核验
均确认其已入群、为普通成员。两项使用各自单次native_debug窗口，均已取消；
没有重邀或重放，两次真实变更用完本轮两次上限。

此前接入范围`1aa116f1-2e42-42c7-8195-1a30b99fcfc6`于23:12:11主动关闭，
挑战`9541fd46-f757-41f6-a6cc-7ae1305042ee`已取消／version2，
私聊和群证明事件均为空。上述入群操作不冒充该接入范围的证明或执行占用。
11月11日00:05后两辣椒聊账号的临时受控、动作准入均恢复false，
9680209为epoch/version52，3129316为16；接收保持connected。
两群仍属原本地隔离租户、管理和AI运行均关闭；有效接入范围0、未决操作0。
本轮业务模型调用及费用0，没有新增绑定、归属转移或普通客户认领通过记录。

完整链路仍缺一条新鲜群主挑战消息、网页确认及已有绑定的版本化转移审核。
需明确补充单条证明的操作额度后，再创建新有效范围及10分钟挑战；
不能复活旧挑战、续开预算或直接写数据库证明。旺商聊大蛤仍在im_token阶段
`native_session_renewal_failed`／1200，林夕接收正常；本轮未修复该上游拒绝，
没有通过新登录、短信或猜测客户端版本规避。辣椒聊恢复入群不代替旺商聊验收，
受控号池证明也不计作普通客户正式认领通过。

## v95 接入申请诊断与未提交状态修复（2026-11-10，北京时间；已部署）

从实际运行v94镜像及冻结源码派生
`var/onboarding-contact-diagnostics-v95-20261110/source`，只调整双平台来向申请
诊断、HTTP安全错误映射及接入向导。分别说明未找到、同昵称历史已处理及时间
不在范围；历史同意不充当当前好友关系或身份/群归属证明。保留有界读取、
精确选择、一次提交、未知结果核对及现有滑块/受控豁免边界。

未提交的过期申请计划显示“尚未提交拒绝”，私聊分支明确提示须取消旧接入后
重新开始，不再展示后端无法接受的核验内容。手动取消后提供显式重新开始；
切换、刷新及草稿恢复不会提交或重建，未知操作仍不能取消/重新开始。
合成状态回归143项通过；完整race/sodium726项／71.980秒通过。
串行wsl_test control129项／258.893秒、HTTP228项／794.029秒、
gateway111项／21.170秒通过。API构建39.855秒、产品类型及Web构建51.378秒、
容器smoke40.155秒通过。受控组件59.923秒覆盖双平台四视口及100/300群；
完整App入口14.135秒、普通接入287.553秒、消息13.264秒、账号7.253秒通过。
普通接入覆盖200个截图状态，原生和模型调用均为0；取消后显式重建、未决结果禁用重建、
切换/刷新/草稿恢复不提交均已核对。收尾仅修正合成测试条件，运行源码未再变化。
架构、仓库守卫、架构工具单测、Mermaid及生成目录同步通过；
根工作区另行更新行证据并通过检查，未覆盖其他候选功能或人工架构决定。

匹配可信基线gpt-6.1-sol增量91.999秒通过、上游分析请求1次，检查点已保存；
最终候选再增量14.914秒通过、上游请求0，预检与分析代码指纹一致。
这是咨询分析，不作为原生效果或费用验收。原生/业务模型计数不包含独立的架构分析。

首轮隔离测试因只读目录缺少合成环境挂载占位而未启动，已补齐；普通接入
新增用例误等已禁用的私聊标题，取消夹具误要求幂等头，未决重建用例未适配
禁用按钮，均按实际接口及交互修正后完整通过。为保持冻结源一致，
中止当时的隔离control测试后重新完整执行，原日志保留，不计为通过。
根工作区补验证最初使用系统Python及默认vendor失败，改用现有架构venv和
`GOFLAGS=-mod=mod`后通过，没有新增依赖或修改锁定文件。

20:52:36既有Compose更新API `77fcc6343a29`、Web `1fee4df67417`；
v94 API `a9e9123e6dd6`、Web `10850911e5e5`及原配置已保留回滚。
热群和生产数据库容器ID不变，无迁移；配置、凭据、资源限制及运行模式保留。
只纳入本轮三个Go运行文件及向导改动，未夹带解析、轻量化或媒体候选；
CodecVersion=7，native_ready/ai_ready仍false。
20:53:06线上3份HTML/JS/CSS逐字节匹配，匿名范围接口401；四接收器connected，
心跳和租约新鲜。范围占用1、好友尝试2、原生操作81、业务模型63、业务运行0，
与发布前一致；有效测试范围0。当前挑战已取消/version2，私聊及群证明均无证据。
20:53浏览器刷新后接收状态已连接，旧范围仍显示过期。
隔离数据库容器已停止、数据卷保留。发布和验收证据在本轮var目录。

本轮没有真实好友变更、范围续开、业务模型调用或业务启用。此前19:30的原生只读
核对仅返回林夕已处理的来向/去向历史，同一好友列表未列出林夕；
历史同意不能认作当前好友关系，未发现本次新待处理申请。上游缺少新记录的原因
尚无证据。真实新申请同意、独立私聊/群证明和最终绑定仍未通过，不以合成回归替代。

## v94 真实认领流程续验（2026-11-10，北京时间；尚未完整通过）

18:53只读确认上一范围及挑战均已过期，无私聊或群证明、无范围执行。
经正常后台界面创建本人演示空间的大蛤／林夕／测试群限时范围
`55643760-3137-4b25-9acb-aada6f3058d2`，有效至19:26:44；
18:58:45经用户中心免滑块创建挑战`1b0bc5dc-c3b3-4de6-a199-f1d30d2aaaa6`。
没有登录原生账号、发短信、修改数据库授权或制造证明。

输入完整昵称林夕，只读取得原生时间为00:58:03的唯一旧来向申请。
先取消拒绝确认并检查私聊分支，没有发生审批；此组合发现文案错误：
尚未提交的prepared拒绝计划被提示为“结果待核对”。静态核对还确认，
选中过期申请的挑战不可直接改走私聊完成，应重新创建挑战，不能绕过。
该显示问题已记录，尚未修改或部署修复。

19:06:00明确确认后，仅拒绝上述一条旧申请。持久尝试
`eed069b7-cd52-473d-b232-f1a8c80fbb49`为succeeded／
`native_friend_expired_rejection_acknowledged`，一次预算已使用；
原挑战cancelled／version2，阻塞原因为group_binding_friend_reapply_required。
页面显示“拒绝已确认”，要求重新申请；没有同意旧申请或建立归属证明。

19:09:28通过“重新开始核验”创建新挑战
`b2b94a6d-052a-446f-91eb-0206733e1a1d`，有效至19:19:28。
范围仍为原记录，原生剩余一次、模型使用零，未续期或补预算。
下一步须在官方App由林夕向大蛤重新发一条真实好友申请；没有为制造样本
调用已退出客户流程的主动搜索／加好友，也未批量操作。
新申请同意、独立私聊与群内证明、转移审核和最终绑定仍未通过本轮真实验收。
没有启用群业务、调用模型或提高readiness；本轮无源码发布。

## v94 有界受控接入免滑块（2026-11-10，北京时间；已部署）

从实际v90 API／v93 Web及冻结源码派生
`var/controlled-onboarding-captcha-v94-20261110/source`。仅对服务端核验有效的
本人受控范围免除接入创建、单条好友同意／旧申请拒绝的滑块；登录、普通客户、
未选范围的超管接入及独立演示认领仍需验证。浏览器字段和账号布尔开关不提供豁免。
好友同意保留确认；免滑块的旧申请拒绝增加明确二次确认。失效范围禁止新提交，
相同范围的原未知操作保留只读核对，不能重放。

race/sodium724项／106.832秒通过；串行wsl_test control129项／304.096秒、
HTTP228项／868.893秒、gateway111项／21.177秒通过；专项HTTP27项另行通过。
类型／Web构建75.183秒、API构建49.742秒、容器smoke37.452秒通过。
受控组件63.713秒覆盖双平台四视口、100／300群、关闭／过期、一次处理及核对；
入口14.078秒、普通接入294.398秒、消息91.739秒、账号6.666秒通过。
架构／仓库守卫、工具单测、Mermaid和生成目录同步通过。独立静态复核无新增发现。
首轮Web有两处nullable票据类型错误、专项DB缺少只读挂载目标，均修复重跑；
一次Chrome启动180秒超时未进入页面断言，完整普通接入重跑通过，失败证据保留。

匹配可信基线gpt-6.1-sol增量88.203秒通过、实际上游请求1次；
Vue类型修正后再增量21.133秒通过、上游请求0，最终预检与代码指纹一致。
模型咨询、代码检查、本地回归与真实验收分别记录，不将咨询作为执行证明。
15:56:53既有Compose更新API `a9e9123e6dd6`、Web `10850911e5e5`；
上一API `9a7aa6bf2783`、Web `2f48c8d75a51`及配置保留回滚。
热群和生产数据库容器未变，无迁移、配置／资源／协议／CodecVersion变化，
readiness仍false，未混入未验收候选。15:57:14线上3份资源逐字节匹配，
四接收器connected且心跳、租约新鲜，匿名范围读取401。

正常UI于15:58:54创建新范围`2cfcb684-1c6c-4406-af91-12c10e777b00`：
旺商聊大蛤接收、林夕申请、测试群，16:28:54到期。15:59:41点击开始核验，
未弹滑块且创建挑战`0c87121e-ec85-46e1-9061-7c69d6f2f63f`，16:09:41独立到期。
16:02:06只读核对waiting／version1，私聊及群证明均未完成；
范围占用0、好友尝试1、原生操作81、模型调用63、后台运行0，未新增真实执行。
截图在本轮output/playwright/live-slider-free-challenge.png。
旺商聊真实免滑块创建已验证；辣椒聊为合成覆盖，好友处理、证明、两群转移及完整认领
仍待真实验收，不能将测试证明计作普通客户通过。隔离测试容器已停止，数据卷保留。

## v93 受控向导一次消费与跨平台返回（2026-11-10，北京时间；已部署）

静态复核发现v91入口请求只在子组件记忆已消费，组件卸载重挂会再次弹出向导；
v92反向交接未携带向导中另选的平台。两项均在独立v92冻结源上修复：
父级按请求token确认消费并清空；返回工具携带当前平台，切换后再次检查
网站身份、超管权限和工作区。无新增提交、范围续期或预算恢复。

冻结候选`var/controlled-onboarding-navigation-v93-20261110/source`仅改3个Vue、
2个合成浏览器工具及架构行证据。类型／Web构建38.870秒通过；完整App
双平台四视口12场景12.695秒，包括8次助手卸载重挂与双向跨平台返回，
写入0。受控组件50.892秒、容器smoke34.924秒通过；架构／仓库守卫、
工具单测和Mermaid渲染通过。独立静态复核确认两项修复，无新增发现。
首次跨平台测试误点被组件遮挡的原生radio输入而超时，改点可见label后
完整重跑通过，保留失败记录，没有放宽产品检查。

匹配可信基线增量10.620秒通过、检查点保存；Go白名单未变化，上游请求0，
Vue由构建及浏览器覆盖，不声称模型重新审查了前端。后端沿用v90全量
race／隔离数据库，普通接入沿用v91未改变部分的回归，明确区分本轮实测。
04:38:28仅更新Compose Web为`2f48c8d75a51`，API仍`9a7aa6bf2783`，
热群、数据库和API容器ID不变，保留v92 Web镜像、配置与资源供回滚。
CodecVersion=7、readiness保持false，未混入解析、轻量化或媒体候选。
真实接入仍需正常滑块及独立私聊／群证明；两测试群尚未转移或启用业务。

04:42:11线上3份HTML／JS／CSS与验收构建逐字节一致，匿名范围读取401；
四接收器connected、心跳和租约新鲜。真实界面从过期范围成功返回测试工具，
经正常确认新建范围`20b19244-ad21-455c-beca-42b173398675`：
旺商聊大蛤接收、林夕申请、测试群，04:41:49至05:11:49，active／version1。
旧范围保持expired／version2，新旧范围原生／模型占用均0；
好友尝试1、原生操作81、模型调用63、后台运行0，未因导航或创建范围新增执行。
随后进入本人演示空间的正常接入向导，准确选择新范围并打开真实滑块，
停在本人拼图步骤；尚未创建接入挑战、好友审批或证明，不判定完整认领通过。

## v92 过期范围返回测试工具（2026-11-10，北京时间；已部署）

将向导内“创建新接入范围”同样改为父界面协调导航，避免刚刷新用户中心、
尚无后台视图缓存时返回总览。范围与挑战均不续期；这个按钮只导航，不创建
新范围、不补充预算。已有普通接入、审批及执行代码不变。

从已运行v91冻结源派生`var/controlled-onboarding-reopen-v92-20261110/source`，
仅修改2个Vue组件、2个合成工具及对应源码行证据。产品类型检查／Web构建
36.552秒通过；双平台四视口及刷新后反向交接合计10场景／9.894秒，
受控组件50.490秒、容器smoke36.104秒通过；架构与仓库守卫、工具单测、
Mermaid检查通过。v90后端race／数据库、v91未改变的普通接入回归明确沿用，
没有声称重新执行。匹配基线CodeBoarding增量10.473秒通过、保存检查点；
该轮与v91前端轮均为Go分析白名单无变化、上游请求0，Vue由类型／浏览器检查覆盖。

04:18:03只更新现有Compose Web为`71b7f9e2f660`，保留v91镜像和配置。
API仍`9a7aa6bf2783`，其余容器、凭据、资源、数据库及readiness保持。
真实范围03:59:45到期后，未刷新页面即显示过期错误及禁用的开始按钮；
04:03:06服务端为expired／version2，范围使用0、后台业务开启数0。
线上3份HTML／JS／CSS与验收构建逐字节一致；匿名范围读取返回401。
好友尝试1、原生操作81、模型调用63，未因发布或到期新增。
正常滑块、独立私聊／群证明、两测试群转移及业务真实效果仍待验收，
不能将号池测试证明当作普通客户正式认领。

## v91 受控范围直接进入正常接入向导（2026-11-10，北京时间；已部署）

真实v90界面暴露入口交接缺口：子组件直接修改工作区和hash，被主界面
工作区观察器重置为总览，且没有打开用户中心向导。现改为子组件发出
显式接入事件，主界面先完成工作区切换，再携带当前身份／平台的范围选择
进入群列表；用户中心在有效权益加载后仅消费一次打开请求。
查看和关闭不创建挑战，刷新不自动重开或执行，服务端范围校验不变。

独立候选`var/controlled-onboarding-entry-v91-20261110/source`从已部署v90
冻结源码派生，仅改4个前端组件、3个合成浏览器工具及对应架构证据。
产品类型检查与Web构建36.719秒通过；完整App双平台四视口8场景7.428秒，
确认正确范围、延迟权益、代客户禁用、零写入和关闭后刷新不重开。
受控组件49.902秒、原双平台接入275.262秒回归通过；smoke32.181秒，
架构／仓库守卫、工具单测及Mermaid渲染通过。
匹配可信基线gpt-6.1-sol增量8.971秒通过，检查点保存；
后续预检仍匹配该代码；Go分析白名单未变化、上游请求0，前端由构建和
浏览器验证，不作为新一轮模型审查结论。后端继续使用已验证的v90镜像与测试结果，
未把前端检查当作新一轮后端或真实原生验收。

首次新增完整App夹具漏掉演示套餐system_only标记，按普通接入渲染，
断言失败；补齐与真实响应一致的夹具后8场景重跑通过，保留失败记录。
03:55:07现有Compose仅更新Web为`a9332c17f24f`，v90 Web回滚镜像及原配置保留。
API仍为`9a7aa6bf2783`，API、热群和PostgreSQL容器ID均未变化，
没有中断接收、增加迁移、改变配置、原生操作或模型调用。
03:58真实点击已直接打开指定范围的正常向导；旧范围仍按原03:59:45到期，
没有延长。真实挑战、好友审批、独立消息证明、转移和业务效果仍待验证，
readiness保持false。

## v90 受控接入范围与双平台测试证明（2026-11-10，北京时间；已部署）

从实际v89镜像及冻结源码派生
`var/controlled-onboarding-v90-20261110/source`。新增独立受控接入范围，
绑定超管本人、本人演示空间、平台、接收机器人、不同的号池申请账号、
隔离群、账号版本及最长30分钟期限；挑战仍为10分钟，不自动续期。
普通客户继续拒绝号池身份证明。`controlled_test`证明仅对原范围有效，
不计作普通客户认领通过。已有绑定不是创建测试范围的前提。

前向067/068分别持久化范围／证明关联和共享尝试占用；001–066未修改。
范围关闭、到期、账号／权限变化后，页面、助手、自动群管、热群及最终
原生提交均检查同一范围；已提交或未知结果保留原操作编号，只允许核对。
每范围最多2次原生变更、2次模型调用及0.02USDT，失败或未知占用不返还。
历史配置密封归档和版本化转移仍需真实群主证明，目标业务默认关闭。

| 检查 | 本轮实测 |
| --- | --- |
| race/sodium | 720项顶层通过，82.835秒 |
| 串行隔离wsl_test | control129项／297.681秒、HTTP224项／939.652秒、gateway111项／21.295秒通过 |
| API／Web构建 | 41.124／4.395秒通过；Web含产品类型检查，后一次构建命中缓存 |
| 浏览器 | 受控专项104项／28张截图／51.315秒；双平台320／390／768／1440及100／300候选 |
| 原接入／消息／工作台回归 | 297.326／11.142／5.936秒通过 |
| 容器smoke | 33.050秒通过，合成环境 |
| 架构与仓库 | update/check、两项守卫、工具单测、渲染缓存及Mermaid渲染通过 |
| CodeBoarding | 匹配可信基线gpt-6.1-sol增量79.580秒通过，保存检查点，无全量回退 |

首次HTTP全量发现普通成员群列表查询误触平台专有证明表的RLS；修正为
普通客户继续使用既有租户查询，只有超管查询受控元数据，没有扩大数据库权限。
旧浏览器夹具新增只读范围接口后补齐模拟。失败记录保留，随后全量重跑通过。

首次发布验证将原生重连正常递增的请求计数误当配置变化，自动回滚至v89；
核对加密配置、版本和修改时间未变后，改为配置逐字节检查、计数单调检查。
03:24:30重新发布通过，仅更新现有Compose API／Web，067/068已应用；
API镜像`9a7aa6bf2783`、Web镜像`5fa4d9796cdf`。热群及生产PostgreSQL
容器不变。v89镜像、受限权限配置备份和两份可读数据库备份保留供回滚。
四接收器重连正常；凭据、资源、挂载、网络和运行模式保留。
好友尝试1、原生操作81、模型调用63，与本次发布前一致。
CodecVersion=7、native_ready=false、ai_ready=false，未混入其他候选。

03:29:45正常超管界面创建旺商聊范围：大蛤接收、林夕申请、测试群，
03:59:45到期；当前原生／模型尝试及费用占用均为0。真实操作发现
“在用户中心接入”只切换工作区未打开向导，另行制作前端修复候选v91。
两测试群仍归原空间，未实际迁移、好友审批或开启业务；真实完整接入、
私聊／群证明和普通客户正式认领均不能据部署或合成结果判为通过。

## v89 双平台过期好友申请默认拒绝（2026-11-10，北京时间；已部署）

依据本人“默认拒绝，要求重新申请”的决定，从实际v88运行版及冻结源码
派生`var/expired-contact-v89-20261109/source`。正常接入向导明确核对后，
仍优先选择当前窗口内唯一有效申请；没有新申请时，仅允许选择原生时间
早于本次挑战开始至少10分钟、昵称精确一致、方向正确且仍待处理的唯一旧申请。
该申请形成密封的`reject_expired`计划，完成既有滑块后单次拒绝。
取消滑块可从“处理过期申请”恢复，刷新、GET、计时器和后台任务不触发审批。

提交前重新读取原申请并核验编号、双方身份、原生时间、账号epoch／版本、
连接及准入版本。缺少时间、列表不完整、歧义、原生已过期或已处理状态均阻断；
不扩大普通同意或客户身份证明范围。拒绝明确成功后取消旧挑战、释放预留，
返回`group_binding_friend_reapply_required`，要求重新开始并重新申请。
结果未知仍只核对原操作，不自动拒绝、同意或重发；拒绝不建立好友身份或群归属。

| 检查 | 本轮实测 |
| --- | --- |
| race/sodium | 696项顶层通过，52.072秒 |
| 串行隔离wsl_test | HTTP207项／621.709秒、control116项／200.611秒、gateway111项／20.395秒通过 |
| API／Web构建 | 26.943／40.954秒通过，Web含产品类型检查 |
| 浏览器 | 双平台320／390／768／1440接入专项269.010秒；消息页11.063秒、工作台5.309秒通过 |
| 容器smoke | 30.511秒通过，合成环境 |
| 架构与仓库 | update/check、守卫、工具单测、渲染缓存及Mermaid渲染通过 |
| CodeBoarding | 匹配可信基线gpt-6.1-sol增量127.925秒通过，检查点保存，无全量回退 |
| 独立静态复核 | 无可行动发现；不将静态复核作为真实拒绝效果证据 |

首次专项HTTP测试因测试辅助函数自动完成合成滑块、且缺少幂等键而失败；
修正测试为原始请求并提供幂等键后，全量HTTP207项及race重跑通过。
首次失败日志保留，未为通过测试放宽产品校验。隔离PostgreSQL容器已停止，
容器和卷保留；生产数据库未重建。

00:45:39仅更新现有Compose API／Web。API镜像为
`sha256:4f6cd7de91cdcd846b2214ddfc248e3a59899152646c657f5887a4adf7a74657`，
Web为`sha256:3277ef81d9a4953276c696d8216a00c7893c8ec243452d360150b666a6157ed9`。
v88镜像、配置及回滚标签保留；热群和生产PostgreSQL容器未变。发布检查确认
凭据、挂载、网络、资源、部署模式和安全配置不变，无新增依赖、迁移或服务，
CodecVersion=7及readiness不变，未混入其他解析、媒体或轻量化候选。

00:47:41四个接收器均connected，心跳及租约新鲜，epoch／版本及历史隔离数不变。
好友审批尝试0、原生资料／群操作81、业务AI调用63，与发布前一致，无新增未决提交。
00:53:47服务静态资源与验收构建逐字节一致，包含拒绝路径和“处理过期申请”入口；
当前浏览器正常加载用户中心与接入向导，打开和关闭弹窗未创建挑战或提交操作。
真实旧申请95444768尚未实际拒绝，正常好友接入及群认领不能据此判为通过。
完整结果见`var/expired-contact-v89-20261109/`；冻结候选的架构证据已更新，
根工作区并行来源的生成清单未被覆盖，根功能目录另行同步并验证。

## v88 旺商聊旧好友申请诊断（2026-11-09，北京时间；已部署）

真实只读核查：21:50:25大蛤返回林夕来向待处理申请95444768，
昵称完全匹配，原生申请时间为11月8日02:54:34；本次接入创建于
11月9日21:42:03，21:52:03到期。该编号在11月8日既有核查中已存在。
21:51:21当前GOOD好友名单不包含林夕。合计2次原生读取，好友审批、
搜索、短信、登录及其他原生变更均0；不能用旧AGREE申请代替当前好友关系。
未确认平台是否复用重复申请记录，不将推测当作上游契约。

从实际v87 API／v86 Web及var/wsl-receivers-v87-20261109/source冻结源
派生候选var/incoming-contact-v88-20261109/source。生产修改仅为旧时间申请
的安全错误分类、HTTP固定映射和用户提示；申请选择、时间窗口、权限、
独立私聊／群证明、一次确认及未知结果核对均不放宽。
错误响应不增加昵称、申请时间、原生身份、正文或附言。

| 检查 | 本轮实测 |
| --- | --- |
| race/sodium | 675项顶层通过，87.363秒 |
| 串行隔离wsl_test | control116项／257.293秒、HTTP203项／614.630秒、gateway101项／20.382秒通过 |
| API／Web构建 | 33.578／57.422秒通过，Web含产品类型检查 |
| 浏览器 | 双平台320／390／768／1440接入专项193.458秒；消息页11.542秒、工作台5.970秒通过 |
| 容器smoke | 40.129秒通过，合成环境 |
| 架构与仓库 | update/check、守卫、工具单测、渲染缓存及Mermaid渲染通过 |
| CodeBoarding | 匹配可信基线gpt-6.1-sol增量79.502秒通过，保存检查点，无全量回退 |
| 独立静态复核 | 六个变更文件无可行动发现；没有将静态复核当成真实原生效果证据 |

初次数据库容器因只读挂载缺少.env挂载点而未启动Go测试；补齐空挂载点后
重跑，保留首次日志，不修改产品配置或生产数据库。
隔离测试容器已停止，容器及数据卷保留，未重建生产数据库。

22:21:32仅重建现有Compose API／Web并通过发布不变量检查。API为
`sha256:a5cea550886cab20800d484a491213147891f03e137f3fa8ca35edf34ef83a09`，
Web为`sha256:a12cd2834876df8d8e26f2deb12f88f2355087f7df4a462d733321433367cc5c`。
上一API v87／Web v86及配置备份保留；热群和生产PostgreSQL容器、配置、
资源限制、依赖、迁移、账号epoch／版本、接收开关及准入未变，CodecVersion=7。
四个接收器均connected且心跳／租约新鲜，历史隔离数未增；
好友审批尝试0、原生资料／群操作81、业务AI调用63均与部署前一致。

22:24:41直接核对已服务静态资源包含新原因码及准确文案。浏览器加载新版并
恢复原过期记录，明确显示21:52:03到期；未创建或延长挑战、解决滑块或提交好友。
不将诊断修复、部署或合成检查记作真实好友审批／认领通过，readiness不提高。
本轮生成证据在冻结候选中更新；根工作区已有不同来源的架构清单，
未用本轮清单覆盖其并行修改。完整诊断见docs/onboarding-incoming-acceptance-20261109.md，
全部交付及测试结果保存在var/incoming-contact-v88-20261109/。

## 旺商聊来向好友准入与公开账号核对（2026-11-09，北京时间）

当前正常向导分配大蛤，公开添加账号340738649；从两个账号各自的当前密封
登录会话读取公开身份并核验UID一致，确认林夕的添加账号为148701573。
未输出凭据或个人资料，也未调用原生搜索，不能据此宣布官方App搜索通过。

大蛤准入原closed／2，林夕无记录而缺省关闭；提示发生在本地准入检查。
21:33:04通过现有超管界面，将大蛤接入专用来向审批保存为open／3，
证据引用docs/onboarding-incoming-acceptance-20261109.md。
21:35:02独立查询及审计事件一致；仅此旺商聊账号开放，其他账号／来源未改。
这是本人指示的正常接入验收准备，验收完成后恢复关闭，不是正式原生通过证据。

原挑战于21:27:47过期，未复用或延长，浏览器已返回用户中心原记录。
本轮新增好友审批、主动申请及原生变更均0；仍需新鲜本人申请、正常确认和
滑块，以及独立私聊／群内证明。配置保存不代表好友或认领链路验收通过。
本轮无生产源码、依赖、迁移或镜像变更；四服务健康，未重启。
记录与保存截图见上述文档及var/wsl-receivers-v87-20261109/。

## v87 旺商聊双接收器恢复（2026-11-09，北京时间；已部署并恢复）

本轮从实际运行v86的冻结源码派生，证据目录为
`var/wsl-receivers-v87-20261109/`。唯一生产代码改动为
`internal/nativewire/im.go`的好友申请同步属性尾段兼容；未改变应用解码算法、
CodecVersion=7、依赖、数据库迁移、号池身份或业务准入。旧解析、轻量化、
媒体候选和发布后补记的好友目录元数据未混入候选。

大蛤的接收开关原本开启，旧快照在2026-11-09 03:15:49停于im_token续期失败，
没有保留可核实的细分原因，不能将其追溯判为限流、密码错误或登录失效。
本轮只读验证密封会话的部署、设备及身份绑定，随后仅执行一次有版本栅栏的
换票并成功密封保存。20:43:49通过正常接收配置入口revision13→14恢复；
首次补收118条、其中重复6条，新鲜112条，ACK118次。20:58:11观测仍连接、
累计121条及121次ACK、新隔离0、队列0、重连0；未重新登录、发短信或发消息。

林夕的旧快照为`native_push_boundary_unknown`，`4/1/31`的31是外层正文长度。
只读打开原隔离事件286805，确认内层为`12/101/200`、17字节，旧三字段后
4字节为一个完整Property。依据既有固定Android3.7.4客户端E52，只接受旧布局
或后接一个完整有界Property；不审批好友、不赋予身份／群权限、不产生聊天ACK
或通知水位。原密封包离线回放通过，原quarantined记录保留。

合成回归覆盖旧／新布局、直接及四种已有封装、空及非空附言、逐字节截断、
重复／超量属性、大小上限、第二个Property、非成功码和其他尾部。隔离持久化
回归覆盖通知留存、无聊天投影／ACK／解码唤醒、无目录／好友／群归属副作用，
以及畸形包阻断同批后续水位。

| 检查 | 实测 |
| --- | --- |
| 全库race/sodium | 674项顶层通过，70.724秒，冻结源码未变 |
| 串行隔离wsl_test | control116项／259.163秒、HTTP203项／635.630秒、gateway101项／20.752秒均通过 |
| API／Web构建 | 30.672／3.899秒通过，Web含产品类型检查与构建 |
| 浏览器 | 现有account_dashboard与messages两套通过；工作台含320／390／647／1440视口 |
| 容器smoke | 32.802秒通过 |
| 架构／仓库守卫 | 事实更新、check、守卫、工具单测、渲染缓存及Mermaid渲染均通过 |
| CodeBoarding | 匹配可信基线gpt-6.1-sol增量47.479秒通过，持久检查点已保存；无全量回退 |

最初选用的历史`native_receiver_ui_smoke.js`不在冻结v86中，未实际运行；
ENOENT日志保留。改用冻结源中现有的工作台和消息页回归，未把缺失脚本记为通过，
未改产品代码或补入旧UI假设。真实接收恢复与合成检查分开记录。

21:06:30通过现有Compose只替换API，镜像
`sha256:8b71dd7b4673e3c4522acb91409ad9b5a68e6d77a9a1f016dd134b903a9a67e7`。
Web、热群及生产PostgreSQL的容器和镜像未变，四服务healthy。发布前未决提交为0；
资源限制、环境配置、凭据、准入、绑定及费用计数保持；未运行迁移。v86 API
`sha256:ffc7b5b396c0550042b6b2b941d46d7212357fc581bf42674ae549e188009639`
已另存回滚标签，配置备份仅存私有证据目录。

林夕21:08:25通过正常接收配置revision29→30完成重连及首次同步；
账号epoch／版本仍52。21:08:46已有808条消息／808次ACK；
21:09:19页面显示821条／821次ACK、接收正常。21:10:47持久核对如下：

| 接收器 | 本次部署后的消息／ACK | 心跳（北京时间） | 新隔离／队列／重连 |
| --- | --- | --- | --- |
| 大蛤／测试受控1 | 11／11，其中重复8条 | 21:10:46 | 0／0／0 |
| 林夕／test | 853／853，无重复 | 21:10:40 | 0／0／0 |

两账号均有新鲜独占租约，接收开关保持开启；大蛤epoch／版本17、林夕52保持。
恢复水位分别由原生完整同步边界正常前进，没有直接SQL修改。两个账号的历史隔离
各3条继续保留，原事件286805及密封内容不变。独立只读使用真实分配谓词复核，
两旺商聊管理机器人均满足连接及群名额条件；没有为此创建接入挑战或绕过套餐。
两辣椒聊接收器同时为connected、新鲜租约、队列／新隔离／重连0。

证据包括`delivery-after.json`、`live-after.json`、各次`observation-*.json`、
`daha-recovered-before-deploy.png`及`linxi-recovered-v87.png`。
大蛤从20:43恢复至最终观测约27分钟，林夕从21:08恢复至最终观测约2分钟，
属于有限时段接收恢复验收，不作全天长稳或性能优化达标结论。隔离验收数据库
已恢复为测试前停止状态，保留容器和数据卷。
部署前保留的Web订阅一度失效，整页刷新后21:13:56显示“实时订阅／接收正常”、
林夕最近15分钟919条、待解码／待操作0／0；最终截图为该刷新后的页面。
该页面状态与接收租约分开核对，没有为恢复页面再次重连或重新登录原生账号。

本次不操作真实好友、群管理、认领或模型业务；接收修复不代表这些流程已验收，
`native_ready`、`ai_ready`保持false。

## 辣椒聊单条好友申请受控验收2026-11-09

所有者手动从`我不知道啊（3129316）`向`tsx（9680209）`发送新的好友申请后，
使用本轮已部署v86的独立受控入口。北京时间07:11:15读到唯一待处理传入申请，
原生编号`1794172433693`；没有用以前已同意的记录或好友状态代替新样本。
授权固定辣椒聊唯一测试群、该机器人、该申请人及该编号，仅`friend_approve`、
`native_debug`、300秒和一次预算；群归属、管理及热群开关均保持不变。

- 第一个准备窗口`a7fdd8f2-3fa8-4dd1-aebe-6f072537894d`在确认时被30秒状态
  新鲜度校验阻断，未生成原生审批；07:22:38到期、使用量0/1，随后正常取消。
  第二个窗口`860b0a9a-080a-4b05-abd8-981529e481a9`保持相同范围与总计一次
  原生尝试，没有补充已消耗预算。
- 07:26:20持久操作`3c0e051e-ac89-426e-8dba-bc2525dfaafc`为`succeeded`，
  安全结果码`native_friend_decision_acknowledged`。界面先显示待查询，最终查询
  原记录得到平台确认；没有重发。实际窗口使用量1/1。
- 07:26:20独立只读回读精确匹配申请编号、申请人、传入方向和
  `FRIENDLOG_STATE_AGREE`，当前好友名单也包含该申请人，界面显示
  “已观察到期望状态”。未将平台回执等同对方客户端已读。
- 07:28:15前两个窗口均取消，临时受控测试与动作准入均还原关闭，接收连接保持。
  四次正常开关保存按既有控制层规则将账号epoch从44提升至48；不回写旧epoch。
  07:32:40持久核对通过：原生操作仅增加1、好友同意仅增加1，删除仍为1；
  模型调用、接入审批尝试、出站尝试、自动群管及热群任务无新增，
  有效群绑定、版本、归属及运行开关不变。
- 原始本地证据在`var/friend-approve-v86-20261109/acceptance-after.json`，
  页面结果为`approval-result.png`。没有持久化任意申请正文或凭据。

本次通过的是辣椒聊单条指定申请受控同意，以及此前删除后的单条重新添加。
客户正常接入向导、独立私聊证明、群内归属证明和最终认领没有据此通过；
旺商聊同意、人工拒绝、真实撤权竞态及未知提交恢复仍待验。
`native_ready`、`ai_ready`及生产／助手／自动业务准入不变。
工作区仅为`accept_friend`原子项登记历史证据，不把整个好友审批组或拒绝标绿；
该验收元数据在v86发布后补记，尚未发布到运行镜像。

补记后功能目录生成及专项race通过：`internal/features`1.243秒、
`tools/feature_catalog`1.288秒；增加拒绝、其他平台及整组不得继承通过记录的校验。
首次旧固定11项断言失败后按本次真实第12项同步；架构默认vendor模式缺少既有
protobuf包，改用`GOFLAGS=-mod=readonly`完成事实更新，未安装依赖或改锁文件。
架构边界、仓库守卫及选定文件diff检查通过；上述补记不称作第二次全量发布，
冻结v86的35份来源文件仍一致。

## v86 双平台指定申请受控同意（2026-11-09，北京时间；已部署）

从实际运行v85及对应冻结源码派生，限定本轮白名单文件及两份生成架构证据，
排除工作区未验收的解析、轻量化和媒体候选。候选与原始检查位于
`var/friend-approve-v86-20261109/source`及同级日志。
前向`066_controlled_friend_approval.sql`独立约束单账号、单申请人、单申请编号、
仅同意动作、`native_debug`和一次预算；旧无窗口同意入口拒绝提交。
显式审批读取才额外获取申请列表，普通群状态读取不新增该请求。
辣椒聊因原生决定接口只携带申请人，提交前另外验证其恰有一条待处理传入申请；
完整申请窗口不足或畸形时拒绝，不把截断列表当作唯一。
共享持久密封操作、账号串行、5秒频控、当前群角色、接收租约及最终栅栏不变；
普通接入证明账本独立，助手和热群不取得此能力。

| 检查 | 最终实测 |
| --- | --- |
| 全库race/sodium | 673项顶层通过，55.908秒，冻结源码未变 |
| 串行隔离wsl_test | control 116项／282.597秒；HTTP 203项／735.597秒；gateway 101项／20.422秒通过 |
| API／Web镜像 | 3.156／2.803秒通过，使用已有构建缓存；Web含产品类型检查及构建 |
| 浏览器 | 12套通过；审批专项覆盖双平台、四视口、指定申请、过期、越界和未知恢复 |
| 架构／仓库 | update/check、边界及仓库守卫、工具单元、Mermaid渲染通过 |
| CodeBoarding | 匹配GPT-6.1-sol可信基线的增量通过，10.299秒；无初始化或全量回退 |
| 容器smoke | 34.524秒通过，未操作真实原生或调用业务模型 |

首轮隔离数据库未启动、校验期间测试文件变更导致冻结不一致、浏览器点击隐藏
checkbox的失败保留，最终统一冻结并重跑通过。一次全库race中
`TestGenerateImageStructure`出现随机拼图一像素边界断言，673项最终重跑通过；
原日志保留，没有将其称为已修复，也未改动验证码算法或容差。
隔离PostgreSQL已恢复停止状态，容器和卷保留。

北京时间07:09:18现有Compose只更新API／Web，前向066已应用：
API `sha256:ffc7b5b396c0550042b6b2b941d46d7212357fc581bf42674ae549e188009639`，
Web `sha256:9c1c0d59b6a498fc306df94da079de9b6046f348c18e59c3edea4e5283cc705d`。
四服务healthy，热群与生产PostgreSQL未重建；配置、凭据、资源限制及v85回滚镜像
保留，迁移前备份已核验。部署本身没有新原生操作或模型调用，readiness不变。
发布后才执行上节独立真实审批。旺商聊既有会话续期及4/1/31阻断未在本轮修复，
不能用容器健康或辣椒聊单条通过宣称双平台原生生产验收完成。

## 辣椒聊单条删除好友受控验收2026-11-09

所有者在本轮明确确认后，通过现有v85超管受控界面执行；非自动回归、非助手或业务入口。
执行账号为自有`tsx（9680209）`，唯一目标为已认证自有`我不知道啊（3129316）`，
授权范围固定辣椒聊唯一测试群。新窗口最多300秒、仅`friend_delete`和一次预算；
此前零次过期窗口没有复用。提交前现读完整好友名单及群主角色，未直接修改数据库授权。

- 北京时间04:01:41持久原生操作`23afa112-cd97-460f-a0b7-d7a91ddc16af`明确确认。
  页面最初显示请求待查，仅查询原操作，没有重发；窗口最终使用量1/1。
- 04:01:42完整好友名单精确确认目标已消失，好友数从2变为1；执行账号仍在测试群、
  角色仍为群主。平台回执与独立状态分别核对，不推断对方客户端已经同步。
- 未提交退群、拉黑、其他好友删除或重新添加；04:03:38前临时受控测试及动作准入
  均恢复关闭，原接收连接保持在线。删除成功画面为
  `var/friend-delete-v85-20261109/delete-confirmed.jpg`。
- 本人完成接入滑块后，普通新建向导分配到另一机器人`我不知道啊（3129316）`；
  该分配按已绑定群数选择，并非删除对象`tsx（9680209）`。这条尚未提交原生操作的
  接入记录已通过正常界面取消，未更改群绑定。重新核验原群的准备也未提交。
- 04:52:53通过现有账号人工审批界面读取`tsx`申请窗口，仅返回该目标以前
  “已同意”的旧记录，没有新的待处理申请；不把旧同意当作重新添加成功。
  手机镜像此前因手机正在使用而断开，未代发新申请、重新审批或生成群归属，
  也未调用受控主动搜索制造样本。只读持久核对为删除总数1、未决删除0、
  辣椒聊有效接入挑战0；删除操作仍为`succeeded`。
  重新添加待该自有账号在官方App产生一条新申请后，继续单条人工审批及好友回读；
  此项与普通客户独立私聊／群归属核验分别记录。

本轮只确认辣椒聊单账号、单好友的一次真实删除。旺商聊真实删除仍待验；
重新添加、真实撤权竞态、断线及未知提交恢复仍未通过。`native_ready`、`ai_ready`
与生产／助手／自动业务准入保持原值。功能目录元数据已在工作区按平台更新，
与已运行v85镜像分开记录；该元数据尚未重新发布，不能把源码更新说成线上目录已刷新。

元数据更新后，`go test -race ./internal/features ./tools/feature_catalog -count=1`
通过（2.666／2.851秒），生成目录已同步；架构update/check、边界守卫、
仓库守卫及选定文件diff检查通过。首次调用系统Python缺少yaml，以及默认vendor
模式缺少既有protobuf包的失败保留；改用现有架构虚拟环境和`GOFLAGS=-mod=readonly`
完成，未安装依赖、改写模块锁定或修改发布基线。未将上述专项检查称作新候选的
全量race/sodium、PostgreSQL或发布验收；运行v85仍采用此前冻结通过的完整证据。

## v85 双平台受控删除好友（2026-11-09，北京时间；已部署，真实删除待验）

从实际v84 API／Web及冻结源码派生，候选与证据位于
`var/friend-delete-v85-20261109/`。双平台分别适配原生删除契约，
仅超管独立`native_debug`窗口可用；单账号、单好友、单动作、一次预算，
当前完整好友名单、群角色、接收租约、epoch及提交栅栏重新校验。
普通社交接口、客户、群管助手和热群均不能调用。未知只读核对，不重放、不退预算。
不修改CodecVersion=7、解码算法、依赖或服务集合。

| 检查 | 最终结果 |
| --- | --- |
| 完整race/sodium | 669项通过，92.388秒 |
| 串行隔离wsl_test | control 116项／315.859秒；httpapi 201项／1051.210秒；gateway 100项／23.215秒，均通过 |
| API／Web镜像构建 | 43.599／4.328秒，通过 |
| 浏览器 | 11套必需流程通过；删除专项覆盖双平台320／390／768／1440及越界、过期、未知恢复 |
| 架构与仓库 | 事实更新、check、守卫、单元及Mermaid渲染通过 |
| CodeBoarding | 匹配gpt-6.1-sol可信基线的incremental通过，63.863秒；非全量初始化 |
| 隔离容器smoke | 37.573秒通过；未发真实原生请求 |

首轮feature矩阵漏项及引用不存在旧浏览器脚本的失败保留，修复后统一重新冻结。
候选变化期间的旧测试结果已归档，没有计入本轮最终通过证据。
主工作区原有原生社交文档保留为历史记录，架构图从合并后的工作区重新生成。
隔离测试PostgreSQL已停止，容器及卷保留；未重置生产库。

现有Compose仅更新API／Web，前向迁移`065_controlled_friend_delete.sql`已应用。
API为`sha256:0cf7a7e911d06be0a5ce6c507cea28f789558a7aecb15f6ed20df47980ed1da6`，
Web为`sha256:4ffac85dc8bf1496903a2533847d509b0fd7264f7914735331d1d7493aaf1230`。
服务健康，配置、凭据、资源限制和上一镜像保留，热群及生产PostgreSQL未重建。
部署前后未决提交0，好友删除记录0、原生操作79、AI调用63，测试群群管／热群均关闭；
readiness仍false。静态资源与候选字节一致，部署不变量通过。

上线接收健康为partial：大蛤在部署前已`native_session_renewal_failed`，
林夕仍为`native_push_boundary_unknown`、协议包`4/1/31`；均未因本轮发布恢复，
不能据服务healthy宣称旺商聊原生接收或删除验收通过。两个辣椒聊接收器正常。

本轮真实只读核对：旺商聊大蛤与林夕互不在当前完整好友名单；不得改选未确认归属联系人。
辣椒聊`9680209`的完整好友名单包含已登录自有测试账号`3129316`（我不知道啊），
对应传入申请显示已同意，当前机器人在唯一测试群为群主。
该关系可用于单条删除准备；真实删除及重新添加尚未提交，目录保持尚无通过记录。
浏览器删除最终确认需要操作时确认，重加申请须走原用户端操作，不使用主动搜索造样本。

北京时间03:42:09通过正常超管界面建立辣椒聊单次删除窗口，固定机器人9680209、
目标3129316、唯一测试群、300秒和一次预算；03:46:31重新读取完整好友及群主角色。
未提交删除，窗口03:47:09过期，使用量0/1；03:49:10前已将临时开启的受控测试、
动作准入均恢复关闭，接收连接保持在线。未新增删除操作或重新添加申请。
目标与过期拒绝画面为`var/friend-delete-v85-20261109/delete-target-pending.jpg`。
真实删除和重新添加仍待完成，不将本次准备、过期阻断或合成回归记为真实通过。

## v84 双平台传入好友接入与受控出站隔离（2026-11-09，已部署；真实审批待验收）

从实际运行v83 API e06e4f270b00／Web 603c35ed6734及对应冻结源码派生，
候选位于var/onboarding-incoming-v84-20261108/source；目录日期为冻结开始日期。
客户向导不再查找用户或主动发好友申请。展示机器人原生昵称和对外添加账号，
由本人在App添加；按确切昵称及服务端10分钟窗口唯一匹配传入待处理申请，
确认、滑块后最多同意一次。已有好友继续独立私聊核验。

昵称、时间和好友关系仅用于选择联系人，不作为网站身份或群归属。
独立认证私聊及群内证明必须来自该联系人，绑定仍核验当前角色；
多条、缺少时间、方向错误、过期和申请变化均拒绝。选择及刷新不审批，
未知结果仅核对原记录，确认不会直接完成身份、绑定或启用业务。
申请数据密封保存，原生账号、昵称、附言、挑战文字和上游正文不进入审计。

原生搜索和主动申请保留在独立超管受控测试面板，要求持久native_debug窗口、
固定群、机器人、目标、期限和预算。旧客户创建接口鉴权后统一410，
模型、助手和业务模块不能取得该入口。排队获得读取槽后再次持锁核验窗口，
单次查找最多3秒；排队期间撤销、到期或请求取消都不会查找上游。
前向063／064扩展联系人流程及受控窗口关联，已应用迁移不改写。
依赖、Compose、CodecVersion=7、解码器及此前未验收候选均不变。

| 检查 | 本轮实测 |
| --- | --- |
| 最终race/sodium | 654项顶层通过，80.402秒 |
| 串行隔离PostgreSQL | 仅wsl_test；control116项／279.912秒、HTTP198项／838.892秒、gateway96项／20.291秒通过；为最终查找栅栏补丁前快照 |
| 补丁后查找栅栏专项 | 16项顶层／196.671秒通过；含两平台12个排队／角色读取阶段撤销、到期与请求超时分支，以及原生移动认证及持久申请相关回归 |
| API／Web | 最终API构建40.035秒；Web类型检查及构建57.823秒通过 |
| 浏览器 | 9套通过，双平台320／390／768／1440；传入专项177.873秒、受控入口12.130秒 |
| 浏览器边界 | 红色即时过期、无额外挑战读取或提交、取消及滑块迟到、切平台／范围、草稿、未知结果不重发通过 |
| 架构与仓库 | update/check、守卫、工具单测、Mermaid通过；只更新事实及来源证据 |
| 最终CodeBoarding增量 | GPT-6.1-sol匹配可信基线119.930秒通过；1请求／33971上报Token，检查点保存，无全量回退 |
| 容器smoke | 最终API与Web33.887秒通过，合成环境，无真实原生或业务模型调用 |

DeepSeek两次增量均遇到HTTP502，原日志保留；按已有备用模型授权切换
独立架构分析配置及密钥，使用已存在且匹配的GPT可信基线。首轮GPT增量
181.935秒通过；查找栅栏修改后重新完成上表最终增量。业务网关、助手模型、
钱包及其凭据没有因此改变，咨询模型用量不记作业务调用或原生验收。
期间旧移动认证夹具及匿名HTTP夹具、浏览器动画／时钟／后台轮询断言、
actor并发限额夹具的冷却遗漏均已修正，首次失败日志保留。
新增账号级和网站用户跨平台并发限额检查，防止同时预留绕过额度。

当前仍未执行真实搜索、发送、同意、登录、短信或群绑定。没有新鲜真实待处理
申请，不能判定双平台真实好友审批通过；准入保持原值，readiness保持false。
本轮证据目录：var/onboarding-incoming-v84-20261108。

首次发布未调用独立migrate服务，迁移核对发现缺少063／064后自动回滚v83，
数据库未变。失败记录及当次镜像信息保留。修正交付步骤后，暂停API写入，
保存0600权限的生产数据库custom dump及配置副本，使用pg_restore --list
核验归档，再通过现有Compose migrate服务执行前向063／064；未降级或恢复覆盖数据库。

北京时间2026-11-09 01:02:50重发成功，仅替换API、Web为
sha256:9d1900756b3047dacf6a4bbb48486e5c6cee3d542933761fbb447dcfafc30f80、
sha256:86005ad916bc365098508091f0ba0ca8b70e95b6c5a24a2f8f49f383fb7b2c30。
四服务Docker健康，热群与生产PostgreSQL容器不变；紧邻v83镜像保留回滚。
环境、资源限制、挂载、手机配置及authenticated会话、助手设置与凭据、
业务开关、群绑定及费用计数均保持，迁移列表只增加063／064。
根目录另外两份生成文档存在其他源码改动，保留并按合并后的源码重生成；
发布镜像始终来自独立候选。目录同步及根目录功能目录检查通过。

01:03:17线上资源逐字节匹配、部署不变量通过。大蛤及两个辣椒聊接收器在线；
林夕原有4/1/31 native_push_boundary_unknown阻断仍在，live-smoke整体partial，
不能写成原生接收全部通过。两测试群归属未核验、业务和AI关闭、版本／epoch
仍为旺商聊36及辣椒聊7。AI调用63、原生设置79、上传12、旧出站记录3、
新联系人尝试0均不变。大蛤旧出站准入open／版本1仅能经受控入口调用；
新传入审批准入仍closed／版本2，辣椒聊不继承开放。
真实新申请审批及完整真人认领尚未验收，native_ready／ai_ready仍false。

## v83 账号查找阶段与有界原因诊断（2026-11-08，已部署；上游拒绝根因及真实申请待验收）

本轮从实际运行的Web v82／API v81冻结源码派生。北京时间21:17:26对当前向导
目标执行一次只读核对，手机搜索返回业务码-1，未进入好友详情或申请发送。
原生读取1次、变更0次、登录0次、短信0次。旧路径没有保留原因分类，
不能反推是限流、号码不存在、会话失效或上游未提供原因。固定Android3.7.4的
请求字段与关键认证契约只读核对没有发现能确定解释本次拒绝的差异。

修复将search与friend_info分开，新增固定业务码和有界精确原因分类；-1／1001
不硬映射为频控。空原因、非空未识别、非文本、过长与网络异常分别返回安全
元数据。控制层在交付诊断及安全审计前复核当前身份、权限、版本、期限、
账号epoch及连接作用域。原始上游正文、目标账号、身份、证明及凭据不进入诊断
或审计。发送前读取失败不再被误认为好友申请已提交，真正未知提交仍只核对原操作。
前端紧凑展示阶段、业务码和安全分类，刷新进度不重新搜索，跨平台及过期响应隔离。

没有依赖、迁移、Compose、CodecVersion=7及解码器变更；大蛤open／版本1的
人工发送准入保持。真实好友申请和完整认领尚未通过，readiness保持原状态。
详细边界见docs/onboarding-search-diagnostics-20261108.md。

| 检查 | 本轮实测 |
| --- | --- |
| 专项race/sodium | 9项通过，6.194秒 |
| 全库race/sodium | 616项通过，56.537秒 |
| 串行隔离PostgreSQL | 仅wsl_test；HTTP180项／526.680秒、control114项／273.548秒、gateway91项／20.512秒通过 |
| API构建 | 32.512秒通过 |
| 产品Web | 最终类型检查及构建44.707秒通过 |
| 浏览器 | 最终6套通过；主动申请135.209秒、客户接入68.172秒、滑块44.803秒、群归属11.762秒、助手绑定12.959秒、手机认证25.391秒 |
| 前端场景 | 双平台320／390／768／1440；阶段、-1／1001不误判频控、1043人工等待、恶意元数据、无额外请求、过期／迟到隔离通过 |
| 架构与仓库 | update/check、守卫、工具单测及Mermaid通过；没有布局调整，仅候选来源证据更新 |
| 咨询分析 | 代码变更增量65.055秒通过，1请求／35150上报Token；最终文案版本匹配增量10.533秒通过，上游请求0，当前检查点保存；没有全量回退，不计入业务钱包 |
| 容器smoke | 最终API与Web 35.248秒通过；全部合成，无真实原生或业务模型调用 |

首轮新增签名夹具缺少合成身份导致失败，已修正并保留失败日志。
浏览器新增过期场景存在合成时钟失配，手机认证断言仍匹配旧提示文字；
保留原失败，修正夹具后复测，不放宽校验。一次手机UI套件名称误写，
未执行测试；保留调用日志并改用现有mobile_search_auth_ui_smoke.js。
以上都不是真实上游请求结果。

北京时间21:58:29通过现有Compose只更新API及Web，镜像分别为
sha256:e06e4f270b0069af39709c128d1f5430ebaa3acc6db57ce2a1f773a4576aabbe、
sha256:603c35ed67347dbe08d1529644bf36493c805e57b27eba293beb60a32ef49b40。
四服务Docker健康，热群及生产PostgreSQL容器不变；上一API c52d5e24e663及
Web 8c750e21c66a已保存回滚标签。运行差异仅15个相关源码／测试文件，
根工作区其他候选没有纳入镜像。所有凭据、资源限制、挂载、部署模式、
手机配置及authenticated会话、群绑定、助手设置与准入保持，没有新增迁移。

21:59:03静态资源逐字节一致，部署不变量通过。大蛤及两个辣椒聊接收器在线、
租约新鲜；林夕原有4/1/31隔离阻断仍在，因此live-smoke整体partial，不能写成
所有原生接收器通过。测试群版本／epoch仍36及7，业务和AI仍关闭，归属未核验。
AI调用仍63，主动好友申请记录仍3，准入保持大蛤open／版本1，其余不继承。
本轮部署没有增加业务模型调用、原生变更、登录或手机会话；readiness仍false。

真实浏览器已刷新并看到新版资源index-qWbJUlrh.js，回到用户中心；
没有新建挑战、解滑块、重搜、申请或绑定。旧诊断无法从旧记录补出，
下一次只能由本人按正常流程手动查找取得新分类，不能自动续期或重试。
当前-1的具体上游原因仍未确定，不能将诊断交付等同于查号或完整认领恢复。
隔离验收PostgreSQL已停止并保留容器和卷。
证据目录：var/onboarding-diagnostic-v83-20261108。

## 大蛤人工好友申请准入配置（2026-11-08，真实发送仍待验收）

本人再次报告发送未开放。只读核对实际原因为发送准入表为空；已存在一条正常
搜索固定的非好友准备记录，状态prepared、已用尝试0。北京时间20:41:51经真实
超管后台保存旺商聊大蛤为open／版本1，证据引用
docs/onboarding-outbound-acceptance-20261108.md。界面显示已保存开放，未修改
其他机器人或平台，未通过数据库直写授权。

20:42:41再次只读核对：开放记录1条，申请记录3条、已用原生尝试总和0、
未知提交0。原准备记录20:41:57到期后转失败终态，尝试仍0；没有自动补建挑战、
重复搜索或申请。需要通过正常新核验、目标确认和滑块继续真实申请。
仅变更运行准入配置与文档，没有代码、依赖、迁移或镜像变更，没有服务重启；
本轮未重跑代码套件，也不把此前合成通过记作本轮真实效果通过。
Web仍v82、API仍v81，readiness保持原状态。截图：
var/onboarding-confirm-v82-20261108/outbound-gate-open.jpg。

## v82 非好友本人确认与接入向导收口（2026-11-08，已部署Web；真实非好友发送待验收）

从实际运行v81冻结源码派生，API保持原镜像，只调整接入向导和受影响的浏览器回归。
非好友搜索只展示固定目标，点击添加后先核对确认弹窗，再进入独立滑块并沿用既有
一次持久发送。已有好友直接进入私聊核验，不发送重复申请。向导删除多余的公开
机器人加好友账号；接口字段保留兼容，未改机器人身份、搜索契约或群证明。
补查发现旧界面在关闭重开、首次签发文字已清除后仍可能发送；现在发送还要求当前
向导持有私聊和群证明文字。缺失时阻止新发送并明确提示重新开始，未把证明写入
浏览器持久草稿，也未改变已有提交的查询和核对入口。

本次真实页面显示正常精确搜索已取得目标和好友关系；数据库只读核对两条准备记录，
分别failed和friends，attempts_used均0，主动发送准入尚无开放记录。
这证明搜索及好友分支已到达，不代表真实非好友申请通过。没有为制造样本删除好友，
没有发送申请、私聊核验、邀请或绑定。完整后续链路以合成回归分别验证。

首轮滑块UI回归因仍等待已删除的公开机器人账号文字而失败；夹具已同步到新界面，
原失败日志保留。不是滑块验证或实际原生申请失败。

| 检查 | 本轮实测 |
| --- | --- |
| Go race/sodium | 611项顶层通过，51.139秒；最终Go源码与运行v81逐文件一致 |
| 串行隔离PostgreSQL | 仅wsl_test，55项接入／滑块／作用域与恢复检查通过，203.462秒；未重跑无关后端全库套件 |
| 产品Web | 类型检查及最终构建59.492秒通过 |
| 浏览器 | 5套均通过；最终主动申请专项116.872秒、客户接入67.143秒、滑块45.487秒、旧群补核验11.923秒、助手绑定12.876秒 |
| 流程与视口 | 旺商聊／辣椒聊，320／390／768／1440；连续弹窗中私聊及群证明可见可复制，邀请与绑定闭环、关闭重开防重发、缺失首次文字禁发、过期与迟到响应通过 |
| 架构与仓库 | update/check、守卫、单测及Mermaid通过；只更新候选事实图和来源证据，未覆盖根工作区其他候选 |
| 咨询分析 | 匹配可信基线增量14.149秒通过并保存检查点；Go白名单未变，上游请求0，不将其描述为模型完成UI审查 |
| 最终容器smoke | 复用运行v81 API及候选Web，37.053秒通过；合成数据，无真实原生／模型调用 |

北京时间20:21:18通过现有Compose只替换Web，镜像为
sha256:8c750e21c66a5bc52ffdf37406411edadf8667df9463aee972b1570b6a01dd3e。
API仍为v81的c52d5e24e663，API／热群／生产PostgreSQL容器ID保持，原生连接未重启。
上一Web镜像0c8833ab1402及回滚标签保留。依赖、Compose、资源、环境、挂载、手机
认证、群绑定、准入及费用计数均保持；没有新增迁移、模型调用或原生操作。

20:21:56线上资产逐字节匹配，部署不变量检查通过。大蛤和两个辣椒聊接收器在线、
租约新鲜；林夕原有4/1/31隔离阻断仍在，live-smoke总体为partial而非全部健康。
两测试群版本／epoch仍36和7、业务与AI关闭；主动申请记录仍2条、已用尝试0，
开放发送准入0；native_ready／ai_ready保持false。隔离验收PostgreSQL已停止，
保留容器及卷。

真实浏览器刷新后重开旧记录，公开机器人账号及复制按钮已消失，“已是好友”
结果和原有效期保留；19:51:15到期的记录正确显示红色过期提示，不能沿旧记录
重新申请。没有创建新挑战、代过滑块、重新搜索、发送好友申请或执行绑定。
正常搜索识别已有好友已核对；真实非好友申请／App同意以及完整真人群接入仍未验收，
发送准入保持关闭，不用合成通过替代真实通过。
证据目录：`var/onboarding-confirm-v82-20261108/`。

## v81 手机接入目标资料兼容（2026-11-08，已部署；正常搜索回归待验收）

本人完成手机认证后，正常向导精确搜索报“本人、机器人或接入范围已变化”。
只读复核发现新挑战、账号epoch／版本及手机会话均有效，没有提交好友申请。
两次有界诊断各执行精确搜索和好友资料读取，共4次原生读取、0次变更：
搜索公开账号匹配，资料UID相同，但资料accountId明确返回数字0。仅记录固定
分类、枚举及匹配布尔值，没有保存原始正文、验证文字或密封凭据。

候选从实际v80冻结源派生。仅手机路径允许好友资料明确的0／"0"默认账号，
保留精确搜索的公开账号和UID；UID必须一致，非零冲突、缺失、null、畸形值
均拒绝。桌面严格匹配不变。发送前仍重新查找、校验关系和持久最终栅栏，
默认0不会成为发送目标，也不允许跨挑战或自动发送。
目标资料冲突改为独立安全错误；好友关系不完整与真实作用域变化分别显示。

固定Android3.7.4好友资料protobuf将account_id定义为int32；本次真实返回
证明默认0兼容需求，不能据此推断是靓号、隐私开关或官方限制导致。
依赖、Compose、001至062迁移、CodecVersion=7及原解码器保持。
真实主动申请、同意、私聊证明和群绑定仍待验收；readiness不变。
证据位于var/onboarding-scope-v81-20261108。

| 检查 | 本轮实测 |
| --- | --- |
| Go race/sodium | 611项顶层通过，71.263秒；含默认账号、身份替换、拒绝冲突、桌面严格校验及固定安全错误 |
| 隔离PostgreSQL专项 | 手机版本栅栏和错误分类2项通过，15.632秒；仅wsl_test |
| 串行隔离PostgreSQL | 仅wsl_test；control114项／263.386秒、HTTP176项／502.504秒、gateway91项／20.338秒通过 |
| Web | 类型检查及构建48.017秒通过；5套浏览器通过，主动申请专项101.596秒，覆盖320／390／768／1440 |
| 架构与仓库 | update/check、守卫、单测及Mermaid通过；只更新候选manifest证据，图布局不变 |
| 咨询分析 | 匹配可信基线DeepSeek增量51.008秒通过，1请求／35956上报Token，检查点保存，无全量回退 |
| API镜像 | 构建38.226秒通过；仅本轮10个运行／测试文件差异，未混入根工作区其他候选 |
| 最终容器smoke | 31.263秒通过；合成数据库及协议，无真实原生请求 |

浏览器使用拦截后的合成接口，验证默认流程、目标不一致／关系不完整的安全提示、
刷新／重新打开无自动申请、同意不替代身份、过期及未知结果恢复；真实原生和
业务模型请求均0。静态复核未发现本轮放宽身份或提交权限。

北京时间19:35:52通过现有Compose只替换API和Web：
API为sha256:c52d5e24e6631a8092f74013eeea831793f0c5ae5653d53b5b1605dc8d4a7871，
Web为sha256:0c8833ab1402228753c3509aee0db973c27ed0e3f7303a911ff9be4e7a486e49。
四服务Docker健康，热群及生产PostgreSQL容器不变，紧邻v80镜像保留回滚。
无新迁移；资源、环境、挂载、配置、群绑定、准入和费用数量保持。
手机配置／会话版本1保留，会话authenticated，未再次登录或发短信。
主动申请准入仍关闭，真实好友尝试0，未生成归属或开启业务。

19:36:13线上静态资源逐字节一致，大蛤和两个辣椒聊接收器新鲜在线；
林夕发布前4/1/31隔离阻断未修复。live-smoke为passed=false、
deployment_invariants_passed=true、receiver_health=partial，不能称全部接收健康。
真实已登录用户中心刷新后重新打开接入弹窗，旧通用错误消失，19:18:31到期的
原记录准确显示已过期并隐藏失效核验内容；“重新开始核验”按钮可见。
没有代过滑块或创建新挑战，没有搜索重试、发送、同意、私聊证明或绑定。
下一步从正常向导重新开始、本人完成安全滑块后，逐项验收精确搜索和单次申请；
本轮真实读取定位与合成发送通过不能替代修复后正常流程的真实通过。
readiness仍false。隔离wsl_test验收容器停止并保留数据卷，生产数据库未停止。

## v80 手机短信登录响应校验修正（2026-11-08，已部署；手机认证通过，搜索待验收）

用户手动短信登录报“登录响应未通过密钥交换绑定校验”。旧原始响应未保留，
不能还原具体失败分支。核对固定Android3.7.4确认两项适配错误：手机响应被
要求包含桌面sealed-box票据并回显交换字段，且响应号码被要求完整返回。
修正后手机按自身UID、JWT、账号和安全状态契约处理；允许号码省略／脱敏，
完整号码冲突仍拒绝。请求X-Context、MAC派生和桌面票据校验保持。

官方支持HTTPS内普通JSON响应，其来源与传输完整性由TLS保障；加密响应另验
请求ID、正文摘要、设备及版本AAD，不声称普通JSON具备应用层回显证明。
新增受信TLS正例和不受信证书负例；控制保存仍核验既有UID、网站会话、账号、
配置版本和epoch。无法核验的成功响应不保存，状态unknown且不自动重试。
安全诊断版本3区分身份、账号、号码和安全状态问题；手机管理器接入已有审计，
不记录上游正文、密码、手机号、验证码、人机票据、响应票据或事务句柄。

候选从实际v79冻结源码派生，位于var/mobile-login-v80-20261108/source；
依赖、Compose、001至062迁移、CodecVersion=7和原解码实现保持。未混入根
工作区其他未验收候选，没有自动登录、发码、好友申请或业务模型调用。

| 检查 | 本轮实测 |
| --- | --- |
| Go race/sodium | 最终606项顶层通过，49.597秒；首轮605项通过后补TLS用例重跑 |
| 手机协议回归 | 三种登录方式、票据省略／空／旧值、脱敏号码、数字字符串、错误身份、安全码、请求关联及不重试通过 |
| 隔离PostgreSQL专项 | HTTP手机流程12.078秒通过；失败阶段、白名单诊断、关闭后审计、无凭据保存及无自动重试 |
| 串行隔离PostgreSQL | 仅wsl_test；control114项／241.047秒、HTTP175项／473.303秒、gateway90项／21.275秒通过 |
| Web | 类型检查及构建45.002秒通过；4套浏览器通过，手机专项26.062秒，覆盖320／390／768／1440 |
| 架构与仓库 | update/check、守卫、单测及Mermaid通过；匹配基线DeepSeek增量通过并保存检查点，最终复核9.489秒 |
| 最终容器smoke | 42.515秒通过；合成环境，无真实原生或业务模型请求 |

新增PG测试首轮把合法HTTP事务句柄也列为响应禁显项，后续又发现统计中UUID／
text参数混用及把已撤销保留行当作有效凭据；均为夹具断言问题，已修正并保留
原失败日志，没有放宽生产身份、状态、审计或重试边界。
咨询性增量首轮使用1请求、35284上报Token；补TLS回归后的最终白名单源码摘要
仍为201996f621a55905a5f230fb1c73980124a3dd50a1dbfe6ca8e5c63a19bb9ada，
匹配基线复核0请求通过并保存检查点。没有全量回退、重初始化或远端CI执行，
不把咨询分析与本地代码检查、合成回归或真实原生验收混为一项。

北京时间18:26:24通过现有Compose仅替换API和Web，未执行新迁移：
API为sha256:d02386ad74db7d97aca1d54bc266f64ef240d21476f678125c75049430aa373e，
Web为sha256:00b6dafd006a81088fed37332e408e2625cd132e62347825446a9ec94bc11a19。
四服务Docker健康，热群及PostgreSQL容器不变；上一v79镜像保留回滚。
环境、挂载、网络、资源限制、桌面配置、手机密封配置版本1、账号身份及
连接配置、助手策略、群绑定、准入和费用数量保持。正常重连可续取并重新
密封原有短时IM票据，不把这种续票声称为凭据密文字节不变。

18:27:11线上静态资源逐字节一致，大蛤及两个辣椒聊接收器新鲜在线。林夕
仍保留发布前4/1/31隔离阻断；live-smoke为passed=false、
deployment_invariants_passed=true、receiver_health=partial，不能称全接收健康。
18:28已只读刷新真实超管页面并打开手机搜索配置，显示协议3.7.4、配置1、
加密库可用、搜索会话尚未保存；未打开新登录事务、发码、提交登录或好友申请。
手机会话仍0，真实手机认证、搜索、主动申请和正常认领继续待验收，readiness不变。
隔离测试PostgreSQL已停止，保留容器及数据卷；证据位于
var/mobile-login-v80-20261108，旧失败响应不能还原，下一次真实认证由本人手动操作。

本人随后手动完成手机短信登录，持久记录认证时间为北京时间18:44:23：
手机会话authenticated、版本1、部署配置1，密封凭据存在且账号epoch匹配。
真实后台“手机搜索认证”同时显示“业务搜索会话：可用”，大蛤原接收器仍在线；
没有新建原生身份或第二个接收连接。本次确认手机认证通过，不等于搜索、主动
申请或群归属绑定通过。只读核对没有再次登录、发码或发送好友申请。
原接入挑战已过期，从用户中心重新开始后进入正常安全滑块，等待本人完成；
未绕过安全验证，搜索及单次主动申请继续待验收，readiness不变。

## v79 旺商聊手机搜索独立会话（2026-11-08，已部署；真实认证待验收）

候选以实际运行v78及冻结源码派生，仅补手机业务认证与接入搜索。Android3.7.4
使用独立配置、手动认证和密封业务会话；核对UID后附着已有管理号池身份。
不取得NIM票据、不新建接收连接、不改标桌面会话，不改辣椒聊协议。
客户端只开放必要认证、精确搜索、好友资料及接入申请路由。

062前向迁移增加手机配置和会话表；001至061逐字节保持。手机请求计数按账号、
epoch和账号／会话／配置版本持久领取；加密请求编号共用原持久高水位。
最终提交锁定并核验手机版本，旧证据不能在会话撤销、替换或换号后继续提交。
仅新增锁定的Murmur3 v1.1.0协议依赖及既有许可证，不夹带解析、轻量化、
媒体或桌面注册收口候选；CodecVersion=7保持。

| 检查 | 本轮实测 |
| --- | --- |
| Go race/sodium | 最终601项顶层通过，62.832秒 |
| 串行隔离PostgreSQL | 仅wsl_test；control114项／225.048秒、HTTP175项／608.006秒、gateway90项／19.975秒通过 |
| 手机协议 | 合成覆盖响应及身份绑定、路由白名单、无IM、未知结果、撤销和版本栅栏；24并发计数唯一且恢复后继续增长 |
| 离线配置工具 | 固定APK参与18项Python回归通过；0600新文件、不覆盖、不联网、不登录或发码 |
| Web | 类型检查及构建44.614秒通过；9套浏览器回归通过，手机专项23.544秒，覆盖320／390／768／1440 |
| 架构和仓库 | update/check、守卫、架构单测及Mermaid通过；匹配基线CodeBoarding增量45.184秒通过并保存检查点 |
| 最终容器smoke | 35.338秒通过，合成数据库及协议响应 |

首轮race发现已有IM回执与立即断线竞态：回执已入队后，等待方仍可能先选择
断线通道并丢掉明确拒绝码。修复为断线时先读取已入队的对应回执；原测试100次
复现3次失败，修复后race下1000次通过，最终完整race通过。失败日志保留，
没有修改回执含义、重试策略或暴露任意上游正文。
界面回归发现配置退出动画误关新手机登录面板，已修复。测试标签歧义、注册基线
断言和“接收独立”说明误判已纠正；无效执行器名称未运行，不计作通过。

真实手机认证、与接收共存、精确目标搜索、主动申请和正常认领尚未验收。本轮
没有真实登录、短信、好友申请或业务模型调用。主动申请准入仍关闭；
部署及合成通过不替代真实通过，readiness仍false。
证据位于var/mobile-search-v79-20261108，使用说明见native-mobile-search.md。

北京时间17:17:39通过现有Compose显式执行062，只更新API和Web：
API为sha256:6e1f47798559bffddd41fa49a110ee1633652e20621388aba4efc1a1c5288e33，
Web为sha256:e63073208ea41025b4a2642fb21a5d1546f404a32a2c00eda3ec9152a45dbeaf。
四服务Docker健康；热群和PostgreSQL容器不变，环境、挂载、网络、资源限制、
助手配置、群绑定及费用数量保持，紧邻v78镜像已留回滚。
17:21:31逐字节核对上线静态资源；大蛤及两个辣椒聊接收器在线、新鲜且租约有效。
林夕仍有发布前4/1/31隔离阻断，整体live-smoke为passed=false、
deployment_invariants_passed=true、receiver_health=partial，不能称全部接收健康。

17:21:58通过现有已登录超管后台，为大蛤导入离线APK生成的手机配置，版本1；
真实页面显示“已配置”“手机搜索配置已保存”。只是本地密封配置写入，
未点击打开手机搜索登录、未发验证码或提交密码，手机业务会话仍0。
下一步由本人在“管理后台→旺商聊→大蛤→手机搜索认证”完成同一账号正常认证，
再逐项低频验收精确搜索和单次主动申请；已有好友继续私聊证明。
17:24:58复核确认配置1条、手机会话0、真实申请0、原业务费用计数不变；
大蛤及两个辣椒聊仍在线，林夕旧阻断未变化。隔离测试PostgreSQL已停止，
容器和数据卷保留，未停止生产数据库。

## 手机好友搜索契约核对（2026-11-08，运行仍为v78）

根据用户说明核对固定Android3.7.4的好友搜索入口。手机和桌面历史客户端使用相同
搜索路由，但设备身份、客户端类型、摘要算法、交换版本及会话认证不同；
不能通过改请求头或复用桌面密封凭据宣称手机接入完成。
对自有号池账号增加一次只读搜索，业务码1001、固定分类未识别，没有取得
明确手机限制证据；未记录原始正文，未发好友申请、未登录、未发短信、未调用模型。
私有诊断分类的离线合成回归通过（0.002秒），覆盖原因分类和敏感内容不透传；
首轮因隔离测试临时目录不可执行失败，修正测试容器挂载选项后通过，生产不变。
产品源码及运行镜像不变。来源、实测和接入缺口见
[手机搜索核对](onboarding-mobile-search-investigation.md)。

## v78 搜索与好友接入准入修复（2026-11-08，已部署；真实发送待验收）

本轮修复了“搜索账号被发送准入关闭阻断”的边界：精确用户搜索和已有好友
私聊核验不再要求主动申请开关；真正发送好友申请仍由按机器人独立的准入、
滑块、版本、角色、频控和一次性持久计划共同控制。后台账号面板新增超管专用
“好友接入准入”作用域弹窗，关闭状态、证据引用、版本冲突和草稿隔离均由服务端
再次校验。未改变任何现有开放状态，未发送好友申请。

旺商聊真实只读检查使用现有自有号池账号，搜索接口返回业务码1001；私有诊断
中的官方“按账号取用户”接口返回-1，未纳入产品或发布包。没有目标资料，不能据此判断
账号不存在、号段限制或发送权限。辣椒聊真实只读检查精确匹配目标并识别双方
已是好友。两平台没有真实发送、审批、入群或绑定动作。

| 检查 | 实测 |
| --- | --- |
| Go race/sodium | 583项顶层通过，最终62.546秒 |
| 串行隔离 PostgreSQL | 仅wsl_test；HTTP 173项／932.321秒、gateway 89项／24.427秒、control 114项／593.990秒通过 |
| Native 主动申请契约 | 纳入最终583项race/sodium，覆盖单次申请、历史待审和空枚举拒绝 |
| Web | 最终类型检查与构建84.706秒通过；7套浏览器检查通过，覆盖双平台及320／390／768／1440视口；最后主动申请回归98.476秒 |
| 架构与仓库 | update/check、架构守卫、仓库守卫、架构单测及 Mermaid 渲染通过 |
| 容器 smoke | 最终镜像46.275秒通过 |
| CodeBoarding | 匹配基线增量重试通过并保存检查点；最终源码复核21.351秒通过、请求0；首轮分析失败保留日志，无全量回退 |

首轮主动申请专项中的崩溃恢复夹具仍按旧方式构造提交记录，遗漏了061要求固定的
准入版本；修正夹具以模拟真实首次提交，完整HTTP后继回归通过。没有放宽数据库
约束或实际提交校验。首轮新增浏览器检查未登记到既有执行器白名单，补齐登记后通过。

HTTP首轮测试容器在无断言失败时被系统终止，隔离测试内存从1GiB调为2GiB后完整重跑通过；
没有取得OOM事件，因此不把系统终止直接归因为已证实的内存耗尽。
该资源调整只用于隔离测试容器，未修改生产 Compose 资源。真实旺商聊搜索仍未通过，
业务码1001的原因尚未确定；应继续核对现行官方客户端契约和上游结果，不能据此
断言为供应商故障、号码不存在、号段或权限限制。`native_ready`和`ai_ready`保持false。

北京时间2026-11-08 07:54:27，现有Compose显式执行061迁移后，仅替换API和Web：

- API `sha256:2d5c14d6c4f002e53c7b22d2660c5727d281ab50d9de42c8a36e41aa17b09abb`
- Web `sha256:5bf059501905ab0b9b90623069541eb851ddaa1eb9e31e4e26e5695900eb245b`
- 四服务健康；热群及数据库容器未替换。配置、资源、凭据、助手策略及全部关闭准入
  保持，紧邻v77镜像保留为回滚标签。001至060迁移、依赖、原解析器及CodecVersion=7
  逐项核对不变，未夹带根工作区其他候选。

07:54:57发布核对确认静态资源逐字节一致、两测试群版本／epoch仍36和7，
业务及AI关闭、有效测试认领0；AI63、原生设置79、上传12、配置执行8，
自动业务／热群任务和新旧好友申请尝试均0。本轮没有新增模型调用或真实原生变更。
大蛤和两个辣椒聊接收器连接及租约新鲜；林夕仍保留发布前的4/1/31隔离阻断，
没有跳过水位或修改解析器。`live-smoke.json`整体`passed=false`、
`deployment_invariants_passed=true`、`receiver_health=partial`，不算全接收健康通过。

当前浏览器重新加载时网站会话已经过期，停留在登录页；没有绕过滑块或创建会话。
登录后的真实后台入口尚未在当前浏览器复核，7套合成界面回归不能替代这一项。
真实好友申请、App同意及完整认领仍待分别验收，主动申请准入保持关闭。
账号查找被拒绝时，页面明确显示“账号查找被拒绝，未发送好友申请”，不再混称发送失败。
测试数据库已停止，保留容器和卷；交付及私有诊断证据位于
`var/onboarding-search-v78-20261108/`。

## v77 双平台机器人主动加好友（2026-11-08，已部署；真实申请待验收）

从实际Compose API v75／Web v76及对应冻结源码派生，候选位于
`var/onboarding-outbound-v77-20261108/source`。仅纳入本轮接入改动；
依赖、Compose、原解码实现及CodecVersion=7保持，001至059迁移逐字节
不变，新增060前向迁移。根工作区未验收的解析、轻量化、媒体候选未混入。

两平台统一为：填写本人搜索账号、精确查找、核对不可变目标、本人滑块确认
机器人发送一次申请、App同意、独立私聊证明、邀请入群、群内证明和本人
确认绑定。已有好友继续独立私聊路径。新流程不依赖入站申请附言；
旧POST核验入口返回410，仅保留历史和原未知结果核对。未删除好友制造样本。

实现与正确性边界：

- 旺商聊使用固定桌面2.8.3的搜索账号、当前双向好友状态和申请契约；
  辣椒聊使用自己的精确账号查找、字符串身份和单目标好友契约。
  两平台独立核对方向、收件人及原申请，不共用数字ID、nonce或AAD。
- 一挑战固定一目标、一次持久申请；提交前重新验证网站身份、空间、
  平台、账号版本及epoch、当前连接、准入、期限和共享账号冷却。
  排队及新鲜读取后才占用变更冷却。目标选择不证明客户身份或群归属。
- 栅栏仅记录已进入提交；适配器完整核对返回后才记录成功。栅栏后解析
  失败或结果不明保留unknown；重复确认、刷新、重启及核对均不重新发送。
  残留submitting在原期限和提交栅栏期限均结束后可显式核对。
- 新用途group_binding_friend_send的一次性滑块绑定当前会话、网站用户、
  平台和挑战；旧用途票据不能代用。读取进度不会搜索、审批或发送。
- 新主动申请准入按平台及机器人独立保存，缺省关闭。旧大蛤附言准入已于
  北京时间05:18:37通过正常超管API从open／1恢复closed／2；未用SQL
  创建或修改授权，未改变好友关系、群配置或业务开关。

已取得的检查结果：

| 检查 | 实测 |
| --- | --- |
| Linux race/sodium | 581项顶层，103.240秒通过 |
| 串行隔离PostgreSQL control | 仅wsl_test，114项顶层，448.513秒通过 |
| 完整HTTP及gateway数据库回归 | 仅串行wsl_test；HTTP173项／934.839秒、gateway89项／26.692秒通过 |
| 产品类型检查及Web构建 | 114.771秒通过 |
| API镜像构建 | 36.545秒通过 |
| 浏览器 | 主动申请99.496秒、演示接入100.996秒、客户接入70.892秒、旧绑定补核验19.601秒、滑块44.197秒，5套均通过 |
| 架构与仓库 | update／check、架构与仓库守卫、工具单测、缓存及Mermaid渲染通过 |
| CodeBoarding | 匹配DeepSeek基线增量41.395秒通过；1请求、31529上报Token、未知0，检查点保存，无全量回退 |
| 容器smoke | 45.837秒通过 |

浏览器使用合成接口覆盖双平台和320／390／768／1440视口。测试验证目标
固定、本人确认、未知结果核对、旧票据拒绝、过期和作用域迟到隔离；不会
操作真实群、调用模型或绕过真人滑块。旧入站流程的两套浏览器断言已迁移，
保留首轮失败日志；一项旧滑块回归停留在已移除步骤，终止本次测试进程后
更新其正常新流程，最终通过。没有删除断言来接受错误页面。

数据库专项首轮暴露三处合成夹具问题：错误证明的有界角色读取次数、栅栏
后失败应为未知结果、时间序列化后的时区对象比较。已改为检查真实不变量：
身份／归属不变、不重复提交，以及时间点与其他固定字段一致。原失败日志
保留。完整HTTP首轮在96项顶层通过后触及测试包10分钟总时限，当时没有
单项失败；仅将完整串行测试包总时限提高至20分钟，业务期限和断言未放宽。
最终完整HTTP及gateway均通过；没有略过热群、权限或滑块的既有回归。

发布前只读快照：两测试群版本／epoch为旺商聊36、辣椒聊7；业务和AI均关闭，
归属仍unverified、有效测试认领0。累计AI63、原生设置79、上传12、配置
执行8，自动业务／热群任务及入站好友审批均0。

北京时间06:51:05，已通过现有Compose显式执行migrate，再仅更新API与Web：

- API `sha256:74e045900055bc1b71d0be24cc4a1409be61ef16480cc0f7a58c96fb18314c80`
- Web `sha256:4274b6801865bbee81f0c4ec939b0254c7a24142f5cb440bfaeafb3f1a3faf5b`
- 仅增加060_onboarding_outbound_friends.sql，旧迁移未改；四服务健康，
  热群及数据库容器ID不变。配置、凭据、资源、安全选项和挂载逐项保持，
  无未决提交。紧邻API v75／Web v76镜像保留，回滚标签为
  rollback-before-onboarding-outbound-v77-20261108。

首次发布脚本误沿用无迁移发布步骤，未在serve前执行migrate，新API健康
检查失败；已自动恢复上一版API和Web，当时060没有落库。补回现有Compose
迁移服务后才重新发布，并在启动前核验迁移清单。生产代码和镜像未因此
更改，未绕过健康检查；首次失败、回滚和重试证据分别保留。
首次失败JSON中additive_migration_retained是旧脚本常量，不能据此判断
060曾经落库，以当次实际迁移清单和重试记录为准。

07:04:31线上HTML／JS／CSS逐字节一致；两群配置、epoch、业务开关、
费用及操作计数与发布前快照一致。新主动申请尝试0、开放准入0；
旧大蛤附言准入closed／2。浏览器已加载新版，正常向导正确恢复旧已过期
挑战并隐藏失效证明，没有新建挑战、完成滑块或发送申请。隔离测试
PostgreSQL已停止，容器和卷保留。

**整体接收健康仍未通过。** 大蛤和两个辣椒聊接收器connected、观测及
租约新鲜；林夕仍因native_push_boundary_unknown阻断。其隔离事件286805
收到时间为北京时间02:53:38，早于本轮冻结和部署；外层4/1/200、31字节，
内层12/101/200、17字节。仅用已部署冻结源只读回放，当前仍拒绝；已知
String／Byte／String布局读完后余4字节，不能凭此猜测新增字段的语义。
原密封隔离记录保留，没有输出正文、跳过水位、恢复连接或发出上游请求。
live-smoke.json明确passed=false、deployment_invariants_passed=true、
receiver_health=partial；这不是部署回滚，也不记为四接收器验收通过。

真实主动申请、对方App同意、私聊和完整正常认领仍待本人真实样本，不能
把合成检查或镜像成功记为通过；新主动准入保持关闭，native_ready和
ai_ready仍false。本轮没有选取未经确认的好友目标或删除已有好友制造样本。
证据目录var/onboarding-outbound-v77-20261108/，包括delivery-after.json、
delivery-failure.json、delivery-before-retry-1.json、existing-block-replay.json、
live-smoke.json及live-onboarding-expired.png。

## 正常接入：本次好友码与双侧记录核对（2026-11-08）

本次正常挑战1b79b314-6b34-4579-afe7-012ef1333073于北京时间02:52:45
创建，分配大蛤，03:02:45到期。页面完整好友码与服务端密封挑战一致；
未将核验正文、任意上游附言、用户资料或凭据写入诊断结果。

- 02:57:35对大蛤固定好友申请接口读取一次，窗口10条、传入待处理3条，
  这3条均有附言，完整本次码匹配0。林夕的95444768仍待处理，
  95463102已经同意；两条附言均4字符且等于App默认问候。
- 03:05:07对林夕同一路由读取一次，窗口177条。两号间申请ID、方向
  和状态与收件侧相符；整个窗口的字符串字段均未包含本次20位码。
  此次没有使用最大ID猜测分页；固定桌面2.8.3仍直接使用lastId="0"。
- 03:07:50因需要排除“已经是好友”解释，对大蛤当前有效好友列表读取
  一次，3项中不含林夕。因此不能仅凭历史已同意状态认定当前仍为好友，
  也不能把未匹配附言直接归因于对方已成为好友。
- 03:11:01只检查两自有账号最近30分钟、最多40条已密封私聊投影，
  实际2条，没有本次码；未发出上游请求或输出正文。
- 当前运行版OnboardingFriend合成回归通过，0.862秒；核对官方Android
  ApplyAddFriendActivity及friend.proto，申请验证信息对应applyMsg，
  备注昵称为独立nickName字段。正确的完整附言可匹配的合成结果不能
  代替本次真实申请成功。

持久挑战已expired／版本2，好友审批尝试0；网页没有同意这条不匹配申请，
没有登录、发短信、发送消息或模型调用。此次3次上游调用均为低频只读，
未批量操作或自动重发。本次未证实用户填写内容最终提交到哪个原生字段，
不声称已定位附言未送达的具体环节，也不放宽完整码和待处理状态检查。
需要核对本人实际填写的App页面及发件账号后继续，不要求重复发申请制造样本。
产品源码、Compose镜像、群配置、业务开关与readiness保持原值。
大蛤接入好友准入仍open／1，待本轮真实验收完成后恢复关闭。

证据：var/friend-note-pending-20261108/下本次挑战的三个带时间戳result文件；
诊断仅保存固定状态、长度、匹配布尔值和必要自有账号标识。

## v76 接入核验自动过期提示（2026-11-08，已部署）

从实际Compose v75及对应冻结源码派生，仅修改CustomerOnboarding.vue。
页面使用一个按截止时间调度的定时器，到期立即显示红色圆形感叹号、
“已过期，请重新开始核验”，隐藏失效好友／私聊／群证明和旧提交入口。
切回前台或窗口获得焦点时重算时间；绑定成功、待审核状态不按旧挑战期限
改为过期。不增加轮询、后台请求、依赖、迁移、原生操作或模型调用。

“重新开始核验”保留平台和工作空间，使用新幂等键并重新走本人正常滑块，
不自动续期。原提交结果未知时禁止重新开始，保留原操作核对和不含核验码
的进度草稿，关闭后重新打开仍能核对，不重放审批或入群。

已完成检查：

| 检查 | 实测 |
| --- | --- |
| 最终产品前端类型检查及构建 | 45.992秒通过 |
| Linux race/sodium | 535项顶层，55.399秒通过；Go源码与v75逐字节一致 |
| 串行隔离PostgreSQL | 仅wsl_test，GroupBinding接口35项顶层，164.736秒通过；测试容器已停止，容器和卷保留 |
| 架构与仓库 | update／check、两项守卫、工具单测、缓存及Mermaid渲染通过；仅生成证据变化，无拓扑调整 |
| CodeBoarding | 匹配可信基线的增量12.661秒通过，模型请求0，检查点保存，无全量回退 |
| 最终容器smoke | 36.014秒通过 |
| 浏览器 | 4套通过：接入与演示110.725秒、客户接入68.446秒、旧绑定补核验11.916秒、滑块42.089秒；双平台及320／390／768／1440视口 |

首轮浏览器过期断言将工作台既有只读刷新误计为接入请求，定位日志确认
红色提示存在、旧码和提交入口均已隐藏、写请求0。已修正为允许原有固定
工作台读取，同时拒绝新增接入读取或写入，保留两次失败日志。另补齐过期
未知操作的草稿恢复；随后一轮因合成角色切换与旧页面定时刷新交错失败，
已改为卸载旧页面后切换合成身份，原日志保留。最终4套全部通过。
新增回归覆盖定时到期、前台时钟跳变、确认／滑块中途到期、新记录与幂等键、
两次关闭重开后核对原未知操作，以及绑定成功／待审核不误判。

2026-11-08 02:36:45北京时间，仅更新现有Compose Web：
`sha256:d97a7cee5bf3ca7809914ad9d2ffb21c2ce9f1f77ff88b78385203a7a0a1bee4`。
API保持v75 `7ecee01e5a1c`，API、热群和数据库容器ID不变；四服务健康，
资源、配置、凭据、安全选项、挂载和迁移清单逐项一致，无未决提交。
紧邻v75 Web保留为`wsl-web:rollback-before-onboarding-expiry-v76-20261108`。
02:37:25静态资源逐字节一致，四接收器connected且观测／租约新鲜；
两测试群版本36／7、业务和AI关闭、有效测试认领0，原生及AI计数不变。
浏览器已载入新版并验证正常打开／关闭接入向导；当前没有恢复的挑战草稿，
未为制造过期画面新建挑战或绕过滑块。过期行为由合成时钟回归与测试截图验证。

冻结候选与证据：`var/onboarding-expiry-v76-20261108/`。本轮不将界面、
合成回归或部署成功记作好友验证码匹配、审批或完整认领通过。

## 正常接入：待同意好友申请附言核对（2026-11-08，v75运行版）

本轮按本人纠正核验正常接入，不使用临时测试认领。01:12:57北京时间，
用当前v75固定申请列表读取器分别对大蛤、林夕读取一次。大蛤返回窗口8条，
林夕175条；各有2条FROM_HIM／PROCESSING待同意记录，这4条均有可读取的
applyMsg附言。当前申请码的精确匹配数0；两号之间原95456841记录仍已同意，
附言4字符，与此前或本次接入码均不匹配。未输出或保存任意附言正文、资料
或凭据；未审批、登录、发短信、发送消息或改变好友关系。

01:18:32通过当前已认证超管会话和现有正式API，仅将大蛤接入专用好友准入
由缺省关闭改为open／版本1，证据OWNER-NORMAL-FRIEND-NOTE-20261108；
未用SQL创建授权，未开启普通群管动作或readiness。旧正常挑战于01:15:12
过期，身份未证明、好友尝试0。已重新打开正常接入向导，停在本人滑块；
后续需一条本次新码对应的本人待同意申请，才可验收精确识别和单次同意，
之后继续独立群证明。不得把附言可读记为完整认领通过，也不审批其他申请。
验收完成后恢复本轮大蛤好友准入。没有源码或镜像发布。
证据：`var/friend-note-pending-20261108/result.json`。

01:22:23本人完成滑块，正常向导签发新挑战
a8e86362-40dc-4d7b-bc8e-e913c43b3264，分配大蛤，截止01:32:23。
01:25:08对该码只读核对一次：大蛤窗口8条、待同意2条且都有附言，
精确匹配仍0。好友确认步骤另要求用途为group_binding_friend的独立滑块；
未完成或绕过，已取消这次确认窗口，正常挑战保留，好友审批0。
当前等待本人向大蛤发送含本次新码的一条真实待同意申请，再继续正常确认。
证据：`var/friend-note-pending-20261108/result-a8e86362-40dc-4d7b-bc8e-e913c43b3264.json`。

01:41:17收到本人“发了”后只读核对，大蛤列表新增林夕发来的95463102，
方向FROM_HIM、状态PROCESSING，窗口9条、待同意3条。本条applyMsg存在，
4字符，但不匹配本次接入码。01:46:20针对已发现差异再做一次只读字段核对：
本条附言精确等于官方App“我是＋发送者昵称”的默认验证语；整条申请的字符串
字段均未包含本次20位验证码。固定Android客户端的ApplyAddFriendActivity、
str_wo_is_xx及friend.proto共同确认applyMsg为申请验证信息，备注昵称另属
nickName。没有输出或保存任意原始附言、用户资料或凭据，仅保存匹配布尔值、
申请状态及必要的自有账号标识。本结果证明待同意申请附言可读取，不能记为
验证码匹配或自动同意通过，也不能断言用户输入内容被何处替换。

本次挑战已经于01:32:23过期，身份和群证明均无、好友审批尝试0；未延长
期限或审批这条不匹配申请。正常页面刷新后明确显示“已过期”，已关闭旧记录、
重新打开创建接入滑块，等待本人完成后用新码替换App申请验证信息中的默认
文字；未创建新挑战、未发出新申请。好友准入仍为大蛤open／版本1，仅用于
未完成的本轮验收，完成后须恢复关闭。无产品源码、镜像、群配置或业务运行
变更，无原生审批、消息发送、登录或模型调用。

补充证据：`var/friend-note-pending-20261108/result-a8e86362-40dc-4d7b-bc8e-e913c43b3264-*.json`；
页面交接截图`var/friend-note-pending-20261108/normal-onboarding-captcha.jpg`。

## v75 临时认领清理与查询兼容（2026-11-08，已部署）

2026-11-08 00:01:43至00:11:43北京时间，在现有v74正常用户中心完成本人
临时测试认领。页面功能弹窗开启关键词回复34→35，再关闭35→36；规则始终
为空，完整配置恢复。保存后的两次后继认领保持原截止时间，没有续期。
到期前主动解除当前认领，三条记录均cancelled，助手回到“未认领·只读”；
旧回合在权限变化后不可读取。管理业务和AI运行始终关闭，原生操作、发送、
上传、自动规则和热群任务新增均0。

其中一次真实只读配置查询失败`assistant_invalid_tool`。仅解封本人该回合
的tool_started元数据，确认工具为get_group_config；历史未保存工具参数，
不能声称已经还原其具体内容或确认省略参数就是此次根因。该次模型正常结算
0.002267 USDT，由平台承担；失败后未自动重试、未生成建议或变更。
页面往返及撤权通过，不把这次助手读取记为通过，也不作为真实客户归属证明。

候选从实际运行v74及冻结源码派生，修复已确认的兼容缺口：绑定群的五个只读
工具允许省略或纯空白参数，控制层规范化为{}后继续实时鉴权；非空输入必须
是严格对象，未知字段、类型、尾随JSON和越界选择器不被忽略。新增固定参数
形态及失败分类，不记录字段名、值或任意解析器错误正文；不增加模型调用、
重试、迁移、依赖、Compose服务或解析候选。

首轮单元编译因局部变量重名失败，已修正，原失败日志保留。最终结果：

| 检查 | 实测 |
| --- | --- |
| 助手／控制单元 | 115项顶层，18.019秒通过 |
| 最终Linux race/sodium | 535项顶层，95.565秒通过 |
| 串行隔离PostgreSQL | 仅wsl_test；control114项／345.791秒，HTTP154项／468.411秒，gateway80项／19.950秒通过 |
| 绑定工具专项 | 2项顶层、双平台配置往返、空参数、未知字段及撤权；28.450秒通过 |
| 产品构建 | API49.303秒，Web83.631秒通过 |
| 浏览器 | 4套通过，覆盖双平台、320／390／768／1440、300群分页、认领撤权、Markdown／表格、折叠过程和结果卡 |
| 架构与仓库 | update／check、两项守卫、工具测试、缓存及Mermaid渲染通过 |
| CodeBoarding | 匹配DeepSeek基线增量48.969秒通过；1请求、32751上报Token、未知0，检查点保存，无全量回退 |
| 容器smoke | 48.535秒通过 |

2026-11-08 00:44:34北京时间，仅更新现有Compose API和Web；四服务健康，
热群／数据库容器ID不变，配置、凭据、资源、安全选项、助手开关及准入逐项
保持，无迁移、未决提交或发布额外调用。保留紧邻v74回滚镜像：

- API `sha256:7ecee01e5a1c85effc5c8bfcdd9c9823c1e98aa79f1baab7eee418bdc54ef451`
- Web `sha256:969f3a277dced0f3a08f437738231732019d0772a1e9b9595a6dd5e6135536ae`
- 回滚标签 `rollback-before-assistant-query-v75-20261108`

00:45:23线上HTML／JS／CSS与镜像一致，四接收器connected且观测、租约新鲜。
00:47:13至00:47:23使用相同问题完成真实只读回合，工具get_group_config，
参数形态empty_object，成功返回版本36、关键词回复关闭、规则0条。最终回答
明确“配置快照、未验证运行时”，过程默认折叠；没有建议、确认或原生执行。
仍为未认领只读，没有重新认领或再做滑块；完整配置、epoch36及业务开关保持。
该成功样本不能还原前次未保存的参数或证明其具体失败原因。

查询含一次理解／工具选择和一次结果总结，共2次模型，全部结算0.004182 USDT，
由平台承担、冻结0、没有自动重试。连同前次失败回合，本轮业务模型3次、
0.006449 USDT。AI累计63，原生设置79、上传12、配置执行8保持；
自动群管／热群任务0。两测试群分别36／7，业务和AI均关闭，readiness不提高。
隔离验收PostgreSQL已停止并保留容器和卷。

证据`var/assistant-query-v75-20261108/`；claim-cleanup.json为临时认领及完整
还原，real-readonly.json为上线后真实查询。截图
`output/playwright/assistant-v75-readonly-verified.jpg`。本轮只读记录脚本首轮
误用不存在的统计列，已改为表内created_at后重新查询；没有修改数据库。

## v74 好友接入双路径与接收恢复（2026-11-07，已部署）

从实际运行v73及对应冻结源码派生，仅同步5个运行文件及必要测试、说明：
`var/friend-onboarding-v74-20261107/source`。没有夹带根工作区未发布候选；
依赖、迁移、Compose、CodecVersion=7、业务配置及生产准入保持原状态。

- 固定桌面2.8.3 SDK核实12/101、102、103分别为好友请求、删除及更新同步。
  仅接受完整符合线型的通知；不生成聊天消息、边界或ACK，不吞相邻未知命令。
  隔离事件275270本地密封回放通过：notice、消息0、边界无，原记录逐字节保留，
  原生请求0，不写入明文。另有持久回归证明通知不推进既有恢复水位，
  畸形通知继续隔离。
- 一次好友申请读取窗口与已有解析器统一为最多500项，覆盖实测174项场景。
  仍只匹配唯一、收到的、待处理且完整验证码一致的申请；不分页批量审批。
  已知目录／状态错误转成固定安全提示，任意上游内容不返回浏览器。
- 向导明确区分“发送好友申请”和“已是好友或无需验证”。申请码20位，
  私聊证明43位，后者独立签发，不以好友已同意或本地分支选择替代身份。
  刷新、切换、迟到响应和未知提交不触发再次审批。

### 本轮检查

| 检查 | 实测结果 |
| --- | --- |
| 最终Linux race/sodium | 534项顶层／46.978秒通过 |
| 串行隔离PostgreSQL | 仅wsl_test；control 114项／240.263秒，HTTP全量153项／419.800秒，gateway 80项／20.162秒通过 |
| 最终HTTP专项 | 3项顶层／25.725秒通过，覆盖两平台普通客户两种证明、本人确认、跨客户／代确认拒绝、号池身份拒绝、错误过滤 |
| 产品构建 | 最终API 24.880秒，Web 63.113秒通过 |
| 浏览器 | 最终镜像7套通过；320／390／768／1440、100／300群、好友双路径、滑块、归属、助手隔离及未知结果恢复 |
| 架构与仓库 | update／check、守卫、8.598秒工具测试、渲染缓存和Mermaid通过 |
| CodeBoarding | 匹配可信基线的DeepSeek增量最终68.796秒通过，1请求／33703上报Token，unknown0，检查点保存；此前一次已通过记录保留，无全量回退 |
| 容器smoke | 最终31.331秒通过 |

滑块浏览器首轮因旧“好友核验成功”文案断言失败。改为独立校验“申请已接受”，
并验证读取持久进度前不得显示身份已核验，最终42.133秒通过；失败记录保留。
HTTP全量之后只补固定错误分类，最终race及HTTP专项重新通过，不把旧全量
声称为最后增量后的再次全量。全部自动回归仅合成数据，没有真实好友审批。

2026-11-07 22:53:06北京时间，只更新现有Compose API／Web：

- API：`sha256:b9b4a3dd56f73ebbe85620b509a14bb6f4a624f4ba2b6f5baa84f25a61bf8be8`
- Web：`sha256:107bdfe2d10f5728f6dc07bb4a743114482c20b33d020dd2e27e865e717a62ec`

热群及数据库容器ID不变，四服务健康。凭据、资源、安全选项、挂载、助手开关、
模块策略和准入逐项保持；无迁移或未决提交。v73 API／Web镜像保留在
`rollback-before-friend-onboarding-v74-20261107`。冻结发布源的架构证据保存在
该source目录；根工作区存在其他候选的生成证据，未将其覆盖或作为本轮证据。

### 真实操作与剩余验收

通过正常管理页面对大蛤执行一次“重新连接”，未重登或改水位。
22:56:12完成同步，界面显示接收正常，补收消息后无新增隔离包。
23:00:49线上HTML／JS／CSS与验收镜像逐字节一致；四接收器connected，
观测及租约均新鲜。两测试群绑定仍为旺商聊34／34、辣椒聊7／7，
业务／AI关闭，客户归属未证明，有效测试认领0。

正式用户中心的过期记录可关闭后重新开始。本人完成滑块后，23:02:27北京时间
成功创建本人演示空间的正常接入挑战，分配大蛤，公开添加账号340738649。
23:11:13读取持久状态为waiting，已扫描5条认证接收消息，好友身份、私聊身份、
群归属证明均未匹配，好友审批尝试0；四接收器connected且观测新鲜。
正常页面刷新与切换“已有好友或无需验证”均可用，显示独立43位私聊证明，
不把切换选项当作身份通过。23:13:58确认挑战已于23:12:27到期；不得复用。
两测试群配置、epoch及运行开关保持，好友尝试仍0，原生设置79、AI累计60、
自动群管和热群任务0，没有新增原生或模型操作。
没有创建替身客户、购买套餐、借用native_debug或测试认领；
真实申请同意、私聊身份、群证明及确认绑定仍待本人样本，不能判定全链路已通过。
大蛤是现有号池账号，不可作为普通客户身份；此前20位码挑战已过期，
历史好友同意记录也不能证明本次身份。使用未入号池的本人群主／管理员完成
界面要求即可，不需要另注册网站账号或开套餐。

发布及接收恢复没有发起业务模型调用、好友变更、原生发送或上传。
23:00:49安全计数：AI调用60、原生设置79、上传12、配置执行8、
自动群管／热群任务0；AI60是当时全库累计，不归为本轮调用。
native_ready／ai_ready不提高。证据及失败日志在
`var/friend-onboarding-v74-20261107/`，接收恢复截图为
`output/playwright/friend-v74-receiver-restored.jpg`。

## 旺商聊好友申请附言只读核查（2026-11-07）

本人使用大蛤向接入向导分配的林夕发起好友操作，提供20位Base32的群接入码。
仅核查已授权本次样本，没有同意、拒绝、删除好友、注册、短信或原生登录。
实际API仍为v73镜像`3ce8e033f096`，本次未改动运行代码或重新部署。

- 本次接入挑战绑定林夕，申请方大蛤是平台号池账号；持久状态曾记录
  `group_binding_pool_identity`，随后原10分钟挑战到期。好友核验尝试0，
  没有生成原生身份或群归属证明。
- 接收侧本次附近3条密封私聊投影均未匹配完整文字或20位码。
  使用冻结v73原生客户端及现有部署的持久请求nonce分配规则，仅实际调用
  一次固定只读`friend-apply-list(lastId=0)`，返回窗口174条。
  大蛤对应记录95456841为FROM_HIM／FRIENDLOG_STATE_AGREE，附言存在但只有
  4字符，与此次验证码不符。不能将此历史已同意记录当作新附言保留、
  本次好友核验或客户认领成功的证据。
- 当前接入核验器只接受唯一匹配的待处理申请；无待处理申请时不能据现有
  好友状态直接核验本次挑战，已是好友的接入仍需独立私聊消息证明。
- 大蛤接收器在21:45:41.478795北京时间被`4/1/25`包阻断，早于本次
  21:51:34挑战创建。隔离事件275270外层解封可读，内层为12/102、正文11字节，
  未含验证码。只读保留原隔离、水位及阻断，不据时间关联推断好友协议含义，
  不跳过包或自动恢复连接。

实际好友审批、业务变更及登录请求均0，原生设置仍79；不提高任何验收或readiness。
附言正文、原始列表、会话和密钥未输出或写入诊断报告。安全元数据与固定只读
核验器在`var/friend-note-readonly-20261107/`；本次不判定好友接入验收通过。

## v73旺商聊真实助手配置往返（2026-11-07，已通过）

本人完成旺商聊“测试群”新的安全验证，机器人林夕，测试认领原截止时间为
2026-11-07 21:16:34.970221北京时间。使用已部署v73及正常群管助手，
没有借用native_debug或受控测试授权，没有新增代码修改或重新部署。

同一会话`d87689af-58f8-4b82-a0e1-a1217d9a7714`完成两次建议及本人确认：

1. 从配置版本32读取，仅建议开启关键词回复，规则保持为空。差异核对后，
   21:09:22.766294确认保存至33，执行`384de521-c696-45f4-b82b-6af9c0a067fd`。
2. 继续要求恢复关闭，助手重新读取当前版本33，未再发生此前的
   `assistant_invalid_tool`。差异仅为开启到关闭，21:12:08.190484确认至34，
   执行`e2eb7210-b344-4e6a-8153-241cad056bbb`。
3. 完整配置与测试前逐项相同；两次保存的认领后继均保持原截止时间。
   到期前主动解除版本34的当前认领，界面显示“未认领·只读”，持久记录
   均为cancelled，有效认领0。没有自动续期或制造客户群归属。

两回合均成功，各2次模型调用，4次全部结算；费用12.125积分，
即0.012125 USDT，由platform_demo承担，冻结0。确认、读历史及解除认领
没有新增模型调用。AI累计54→58，助手配置执行6→8；原生设置79、
媒体上传12不变，本轮原生操作、文本发送尝试和上传新增均0，
自动规则和热群任务仍0。两群业务及AI始终关闭，辣椒聊配置7不变。

当前实际配置为旺商聊34／epoch34、辣椒聊7／epoch7；两平台的正常助手
配置建议、本人确认、同会话还原及解除测试认领均已有独立真实通过记录。
本轮不代表原生动作、自动业务、真实客户认领或生产长稳验收，
native_ready／ai_ready及正式准入保持原状态。

只读数据库断言及安全元数据保存于
`var/assistant-bound-tools-v73-20261107/real-acceptance-wsl-v73.json`；
结果截图`output/playwright/assistant-v73-wsl-restored.png`。
发布时的待验收记录保留在下节，已由本次真实结果更新。

## 单群工具参数与连续配置（2026-11-07，v73已部署）

以实际v72运行镜像及冻结源码为基线，候选
`var/assistant-bound-tools-v73-20261107/source`。只6个运行文件变化：
单群工具不再要求模型填写账号、群ID，查询使用空对象，配置建议只需
本次读取的`base_version`和`changes`；内置指引升至business-agent-v1.3。
控制层只在已有完整单群会话中补齐资源，仍兼容旧同范围ID调用；
显式越界ID、列表附加筛选、未知身份字段和自动确认继续拒绝。
不复用保存前的配置版本，不恢复旧正文，不放宽权限、准入或本人确认。

旺商聊失败记录见下节：已核验工具为`list_authorized_groups`，不是未知工具。
原始参数未保存，无法断言当时哪项字段非法。已复现并修复的契约缺口是：
保存后旧上下文失效，下一回合仍须模型重新寻找资源ID。
本轮统一当前群参数契约与指引，没有猜测参数后自动重试。
回归使用合成模型和wsl_test，不将合成通过当作供应商模型稳定性或真实原生效果。

| 检查 | 本轮实测 |
| --- | --- |
| 单群往返与注入专项 | 3项顶层／11.010秒通过；双平台、客户及超管演示、最新版本、同会话还原和撤权覆盖 |
| Linux race/sodium | 529项顶层／58.083秒通过 |
| 串行隔离PostgreSQL | 仅wsl_test；control 114项／236.165秒、HTTP 151项／387.372秒、gateway 79项／19.621秒通过 |
| 产品构建 | API25.856秒、Web4.244秒通过；前端镜像与v72完全相同 |
| 浏览器回归 | 最终镜像10套全部通过，包含双平台320／390／768／1440、认领、输入、富文本、历史、确认及滑块 |
| 架构与仓库 | update／check、守卫、工具测试、渲染缓存及Mermaid通过；只更新受影响证据位置 |
| CodeBoarding | 匹配可信基线增量68.034秒通过，DeepSeek 1请求／35341上报Token，检查点保存，无全量回退 |
| 容器smoke | 31.670秒通过 |
| 独立复核 | 指定差异的静态只读复核未发现新增问题；未执行数据库、原生或模型请求 |

首轮新增合成测试把bool写入RawMessage类型的测试构造，编译失败日志保留；
改为正常JSON对象后完成上述最终完整回归。无新增依赖、迁移、Compose、
解析或媒体改动，CodecVersion=7；4133个基线文件逐字节相同，截图输出按既有
Docker忽略规则单独记录。未覆盖根工作区其他未验收候选。

2026-11-07 20:44:09北京时间，仅更新现有Compose API为
`sha256:3ce8e033f0968583f0336b61dc3f90a0877c94d7b4766105cbe1532d94da2ff3`。
Web、热群及数据库容器ID和镜像保持，四服务健康；凭据、资源限制、挂载、
安全选项、助手开关和策略逐项保持，无新增迁移或未决提交。
紧邻v72镜像保留为`rollback-before-assistant-bound-tools-v73-20261107`。
20:45:09核对线上HTML／JS／CSS与验收镜像一致，四接收器connected且观测、
租约新鲜。隔离PostgreSQL已停止，容器和数据卷保留。

部署新增业务模型及原生操作0。当前旺商聊配置32、辣椒聊7，均已还原，
有效测试认领0，群业务及AI关闭；AI调用54、配置执行6、原生设置79、上传12，
自动规则及热群任务0。正常助手启用／版本17、模块及正式准入保持，
native_ready／ai_ready不提高。分析供应商调用与业务模型费用分开记录。
发布时v73真实旺商聊连续还原尚待本人安全核验；现已由上节独立补测通过。
辣椒聊v72通过记录保持，发布检查与真实验收分别记录。
交付后文档单独归档，不改写已测试冻结源码。

## v72旺商聊真实助手配置验收（2026-11-07，部分通过）

本人完成旺商聊“测试群”个人测试认领，机器人林夕，期限为
2026-11-07 20:15:56.424332北京时间。通过正常群管助手读取版本30配置，
仅建议开启关键词回复，规则保持为空；本人确认后保存至版本31，
执行编号`ededb704-9b93-4ca2-8828-e9f26eb4ce62`。认领后继保持原截止时间。

同一会话要求关闭还原时，`list_authorized_groups`调用被参数校验拒绝，
回合结果`assistant_invalid_tool`，仅1次模型调用，没有生成还原计划或保存。
工具名属于白名单；原始参数没有持久化，不能反推具体非法字段，更不能称为
未注册工具。只读诊断仅解封该本人测试回合的`tool_started`名称，不读取参数、
提示词、客户正文或凭据。

已通过普通功能弹窗关闭关键词回复，版本32／epoch32；配置完整等于测试前，
群业务及AI始终关闭。认领在解除操作前已过期，最终有效记录0，助手显示
“未认领·只读”；本次是到期，不能记作主动解除成功。

两回合共4次模型全部结算，平台演示费用0.008213 USDT，冻结0；
AI调用50→54，助手配置执行5→6。原生设置79、上传12不变，
自动规则和热群任务仍0。截图及只读核对保存于
`var/assistant-bound-tools-v73-20261107/real-acceptance-wsl-v72.json`。
旺商聊的助手还原未通过，辣椒聊下节独立通过记录继续有效。

## v72辣椒聊真实助手确认往返（2026-11-07，已通过）

本人重新完成辣椒聊“测试群”的安全验证，原生机器人为“受控测试号”，
本轮测试认领截止2026-11-07 19:58:56.574565北京时间。使用正常用户中心
群管助手，不借用受控测试授权，不开启原生动作、自动群管或热群。
实际使用已发布v72，没有修改运行代码或重新部署。

在同一会话完成两次独立建议和本人确认：

1. 读取版本5的原配置，建议开启拦截词，词表仅含“隔离配置验收1107B”。
   差异正确，确认前未保存；19:52:46.429295保存成功，配置版本和epoch为6。
   执行编号`cad73feb-24ed-4bc3-b313-9a98adabc6f7`。
2. 继续要求“将刚才的拦截词配置恢复原状”，助手重新读取当前版本，提出关闭、
   清空建议。19:56:06.559813本人确认成功，配置版本和epoch为7。
   执行编号`53665619-0e08-4211-bd8e-0472573e6d1e`。
3. 两次配置保存后的认领均沿用原截止时间，不自动续期。还原后通过当前后继
   记录解除测试认领，界面显示“已解除”；回到助手显示“未认领·只读”。
   19:59:40核对有效测试认领0，其他配置、名单、业务及AI运行开关保持原状。

配置总结分别展示实际保存内容、版本和未验证运行效果，工具过程默认折叠；
保存后没有额外模型调用。配置版本变化使旧回合正文失效，当前有权读取的
执行结果仍按既有规则展示。本人确认未再发生fresh_native_role_required，
证明本轮修复已通过辣椒聊真实配置确认；不据此宣称原生执行或自动业务通过。

两回合共6次模型调用全部settled，平台演示记账0.012662 USDT，无冻结，
不扣客户钱包。AI调用累计44→50，助手配置执行3→5；原生设置仍79、
上传仍12，自动规则和热群任务仍0。确认、刷新、解除认领和查询历史没有
追加模型调用或原生变更。四个现有服务健康，助手正常开关、准入和readiness保持。

证据截图：`output/playwright/assistant-v72-chili-enabled.png`、
`assistant-v72-chili-restored.png`及`assistant-v72-chili-readonly.png`。
只读持久核对归档在`var/assistant-config-role-v72-20261107/`；
本节和项目断点另存documentation-final，不改变已验收源码。
旺商聊本轮尚待自己的个人测试认领，不能继承上述辣椒聊结果。

## 助手确认刷新原生角色（2026-11-07，v72）

本轮先使用本人完成的辣椒聊测试认领进行真实配置验收，再依据失败修复。
测试范围仅为“测试群”、机器人“受控测试号”的拦截词配置，不发送原生消息、
不改群运行开关。原词表为空、开关关闭；通过功能弹窗启用并保存
`隔离配置验收1107`，版本3→4，随后关闭并清空恢复，版本4→5。
management_enabled和ai_enabled始终false，客户群归属仍未证明。

配置保存后的测试认领正确衔接至新版本，三个认领记录均保持原截止时间
2026-11-07 19:01:26北京时间，没有续期。助手在版本4正确读取配置并提出
“关闭、清空”的待本人确认建议；实际点击确认被fresh_native_role_required
阻断，没有产生配置执行。已通过普通功能弹窗恢复，不能将失败的助手确认
计作真实通过。该回合3次模型调用全部结算，平台演示费用0.006329 USDT，
冻结0，不扣客户钱包；确认与恢复没有追加模型调用。

根因是持久角色超过60秒后，助手配置确认只拒绝而不重新读取；其他页面读取
和配置保存没有使该角色证据重新变新。修复保留原有新鲜角色路径，仅在确认
通过本人权限、计划及版本检查后，释放事务补读一次所选群当前原生角色，
最长8秒。返回后重新完成权限、认领、策略、版本、epoch、连接和当前群主证明
检查，证据须在5秒内；较新的持久角色或所有者变化不能被覆盖。
没有自动重试、续期、额外模型调用、原生变更或合成角色写入。
已提交配置的重复确认只返回原执行，不触发原生读取。

候选从实际v71冻结源码派生，来源
`var/assistant-config-role-v72-20261107/source`，只4个运行文件变化，
另增加控制层测试和3份说明；生成架构证据仅更新受影响源码位置。
没有新增依赖、迁移、服务或Compose变更，CodecVersion=7及解析保持。
前端构建镜像与运行v71逐字节相同，未纳入根工作区其他候选。

| 检查 | 本轮实测 |
| --- | --- |
| 双平台角色刷新专项 | 2项顶层／40.589秒通过；客户及演示确认、撤权、降权、版本变化、策略关闭、取消和重复确认覆盖 |
| Linux race/sodium | 最终526项顶层／59.222秒通过 |
| 串行隔离PostgreSQL | 仅wsl_test；control 112项／253.832秒、HTTP 151项／403.427秒、gateway 79项／19.714秒通过 |
| 产品构建 | API34.136秒、Web4.350秒通过；Web复用相同镜像 |
| 最终镜像浏览器 | 10套全部通过，含双平台四种视口、认领、配置、输入、富文本、历史及滑块 |
| 架构与仓库 | update／check、守卫、工具测试、渲染缓存及Mermaid通过 |
| CodeBoarding | 匹配可信基线增量36.242秒通过，DeepSeek 1请求／32324上报Token，检查点已保存；无全量回退 |
| 容器smoke | 最终镜像33.744秒通过 |

首轮私有递归辅助函数使架构守卫无法定位Approve的直接鉴权证据，失败日志
保留。实现改为同一入口内有界循环，再全量运行race、控制层、HTTP和网关
数据库回归；没有删除或放松守卫。咨询性分析与本地检查不代替远端CI或真实
原生效果验收。隔离PostgreSQL已停止，容器和数据卷保留。

2026-11-07 19:30:03北京时间，仅更新现有Compose API为
`sha256:36f35fe1e773e65946b877f1c89ccc797e827157ef776dc6b5d966005e45cc75`。
Web、热群引擎和PostgreSQL容器ID及镜像保持；四服务健康，凭据、资源、
挂载、网络和安全选项逐项一致，无新增迁移或未决提交。紧邻v71 API以
`rollback-before-assistant-config-role-v72-20261107`保留。
19:30:53线上HTML／JS／CSS与已验收镜像一致，四接收器connected且租约、
观测新鲜。助手开关保持启用／版本17，双平台模块及正式准入保持原状态，
部署未增加模型、原生或业务任务，readiness不提高。

**配置弹窗的开启、保存、关闭还原已通过；修复后的真实助手本人确认尚待补测。**
辣椒聊测试认领已到期、有效记录0，配置版本5；旺商聊保持版本30，尚未完成
本轮独立开关往返。两群业务和AI关闭，AI调用累计44、原生设置79、上传12、
助手配置执行3、规则和热群任务0。网站登录已恢复，后续从辣椒聊新的本人测试
认领继续；不重放旧计划，不将临时演示认领当作客户归属或自动业务授权。
交付证据位于v72目录，交付后文档单独归档，不改写被测冻结源码。

## 测试认领衔接与到期同步（2026-11-07，v71）

从实际Compose v70及冻结源码派生，候选
`var/group-ownership-v71-20261107/source`。仅6个运行文件和3个测试文件变化，
1144个文件逐字节一致。没有新增依赖、迁移、服务或Compose变更，解析与
CodecVersion=7保持，不混入根工作区其他候选。

- 超管本人保存配置仍提升绑定版本和epoch，使旧计划失效；同事务取消原认领、
  创建原证据及原截止时间的后继。不会延长10分钟，不承接他人、过期或失效证明。
  创建、配置保存、解除及原生权限失效撤销共用生命周期锁。
- 旧编号已有有效后继时，解除返回状态变化；只能解除当前认领，不能用旧记录
  的取消状态冒充已撤销。其他用户和客户不能继承演示权限，客户群归属不改变。
- 助手在服务端截止前3秒恢复只读，开放中的确认入口同步关闭并保留草稿。
  到期不自动刷新，避免提前恢复只读时反复查询；当前查询发现只读及回合结束时
  同步访问状态，切平台、切群的迟到响应不能写入新视图。
- 内置指引business-agent-v1.2区分测试认领不足与模块关闭，避免建议错误地
  开放模块。没有增加模型工具或原生执行权限。

| 检查 | 当前实测 |
| --- | --- |
| 产品类型与镜像 | Web最终57.329秒、API30.214秒通过 |
| Linux race/sodium | 524项顶层测试通过，65.066秒 |
| 控制层隔离PostgreSQL | 110项／261.796秒通过；双平台开关往返、原期限及并发撤销通过 |
| HTTP隔离PostgreSQL | 首轮旧断言失败保留；专项34.645秒、最终完整151项／441.853秒通过 |
| 网关隔离PostgreSQL | 与其他数据库包串行，79项／20.786秒通过，仅wsl_test |
| 最终镜像浏览器 | 10套全部通过，双平台320／390／768／1440及到期无自动刷新通过 |
| 架构、仓库及渲染 | update／check、守卫、工具测试及Mermaid通过 |
| CodeBoarding | 匹配可信基线增量通过；首次咨询1次／37261上报Token，后续无后端变化的增量请求0；保留检查点，无全量回退 |
| 容器smoke | 最终API／Web42.536秒通过 |

最终CodeBoarding增量14.336秒通过，没有新的供应商请求。此前针对本轮后端
变化的首次咨询51.545秒、1次／37261上报Token，检查点保存。两者与真实业务
模型验收及远端CI分开，不将零请求复核说成一次新的供应商审阅。

浏览器首轮调度误写不存在的group_binding_ui_smoke.js，未启动产品用例；
已改用仓库现有assistant_binding_ui_smoke.js，最终10套通过。原错误日志保留。
HTTP旧断言要求“保存即失效”，现改为检查固定截止时间内的连续保存、其他身份
拒绝、旧编号取消冲突及当前编号解除；没有放宽权限或截止时间断言。

真实网站验收已完成辣椒聊个人测试认领，机器人为原生群主；此资格于
2026-11-07 17:47:05北京时间到期。17:47:55结束的只读模型回合正确恢复只读，
暴露了旧界面仍显示“可管理”和错误模块阻塞说明，本轮据此修复。
该回合3次模型调用全部结算，平台演示记账0.015137 USDT，无冻结及客户扣费。
临时发送动作建议已关闭，辣椒聊模块策略版本8、旺商聊版本6，各保留原13项；
原生正式准入、自动群管及热群保持关闭。

2026-11-07 18:42:46北京时间，仅部署现有Compose API／Web：
API `553e5501c87c`、Web `0ae367a36c51`。四服务健康，热群及数据库容器ID未变；
凭据、资源、挂载、安全选项、助手开关及模块策略逐项保持，无新增迁移。
未决提交0，紧邻v70镜像以`rollback-before-group-ownership-v71-20261107`
保留。18:43:28线上HTML／JS／CSS与最终镜像逐字节一致，四接收器connected、
观测及租约新鲜；隔离PG已停止，容器及卷保留。

**真实“授权→开启→核验→关闭”仍待完成。** 本轮没有发送原生消息或保存群
配置；两测试群仍无客户归属，配置版本旺商聊30、辣椒聊3，management_enabled
与ai_enabled均false。部署后网站已显示旧认领过期并打开辣椒聊新安全验证。
需本人完成滑块后立即继续配置开关往返；旺商聊仍需自己的有效认领，不重跑无关
构建而消耗10分钟窗口。不能把过期滑块或合成开关当作真实通过。
正常助手开关保持true／版本17，readiness不提高。模型调用总数41、原生设置79、
上传12、配置执行3，自动群管与热群任务均0，部署未增加模型或原生提交。

## 助手选群与群归属校验（2026-11-07，v70）

从实际Compose v69及对应冻结源码派生，来源
`var/group-ownership-v70-20261107/source`。仅12个运行文件改变，另补测试和文档；
1052个文件逐字节一致。依赖、迁移、Compose、解码及CodecVersion=7不变，
没有混入根工作区其他未验收候选。

- 普通客户助手目录只返回归属有效且本人获分配的群。授权撤销、归属暂停、
  切平台及迟到响应继续重新核验。超管可查询未认领绑定，明确显示只读。
- 配置建议、动作建议、确认、单功能保存和旧完整配置PATCH统一检查归属。
  配置提交事务锁定当前归属和绑定；不扩大租户数据库角色的私有表权限。
- 本人10分钟测试认领可以管理，但不制造客户归属；另一超管不能继承。
  到期、解除、网站撤权、账号或绑定版本变化后失效。保存配置提高绑定版本
  和epoch，不自动续期认领。原生准入、角色、预算及频控保持。

| 检查 | 本轮实测 |
| --- | --- |
| 产品类型与构建 | 最终Web 42.429秒、API 27.349秒通过，无新增依赖 |
| Linux race/sodium | 521项顶层测试通过，48.445秒 |
| 串行隔离PostgreSQL | 仅wsl_test；control 107项／208.826秒、HTTP 151项／447.033秒、gateway 79项／19.996秒通过 |
| 最终镜像浏览器 | 10套通过；双平台320／390／768／1440，认领／只读、帮助、迟到隔离、Markdown、折叠、输入、历史及滑块 |
| 配置HTTP回归 | 未认领客户GET拒绝；超管GET字段只读；PUT与旧PATCH拒绝；正式归属、个人认领、保存失效和解除后拒绝通过 |
| 架构及仓库 | update／check、两类守卫、53项工具测试、3项渲染缓存测试及Mermaid通过 |
| CodeBoarding | 匹配可信基线增量47.792秒通过，DeepSeek 1次分析请求／34147上报Token，检查点已保存；业务配置未变，无全量回退或远端CI |
| 容器smoke | 最终镜像30.849秒通过 |

首轮冻结目录的旧目录辅助函数引用和租户私有表读取失败均保留记录。
修复采用控制层有界元数据查询和显式租户／成员约束，没有通过开放数据库
权限消除失败。完整包回归最终通过；隔离PG停止，容器及数据卷保留。

2026-11-07 16:47:56北京时间，仅更新现有Compose API／Web：
API `7fd5b92b3749`，Web `8f185e8370d9`。四服务健康，热群及数据库容器ID未变，
凭据、资源、挂载、安全选项、助手开关版本17和双平台模块策略版本6保持。
无新增迁移，发布前后未决提交0；紧邻旧镜像以
`rollback-before-group-ownership-v70-20261107`保留。
16:48:39线上HTML／JS／CSS与最终镜像逐字节一致，四接收器在线且观测、租约新鲜。

已通过真实网站会话分别核对两平台测试群：均显示“未认领 · 只读”，查询可用，
建议与确认不可用；帮助内容与服务端状态一致。查看历史、帮助及切平台均未
增加业务模型、原生操作或费用。业务AI调用总数仍38，原生设置记录79、
上传12、配置执行3，自动群管及热群任务均0。

**真实“授权→开启→测试→关闭”尚未完成。** 两测试群仍未认领，
management_enabled／ai_enabled均false，版本旺商聊30、辣椒聊3。
辣椒聊测试认领已打开网站安全验证窗口，等待人工完成滑块；没有绕过验证、
直接SQL制造归属或提前开通功能。本人有效认领、开关及真实效果需后续独立记录，
不能把上述自动回归或只读上线核验计作真实功能通过。
正常助手可用开关保持启用；native_ready／ai_ready不提高。
规则与断点见[群归属说明](assistant-group-ownership-20261107.md)及
[项目检查点](project-checkpoint.md)。

## 群管助手紧凑输入、能力说明与注入回归（2026-11-07，v69）

从实际运行v68及冻结源码独立构建；本轮只发布助手前端，增加两项后端合成
测试，不改变Go运行代码、依赖及锁文件、Compose、迁移或CodecVersion=7。
605个后端／入口／部署文件与前次冻结版逐字节一致；没有混入根工作区其他候选。
来源及私有证据：`var/assistant-composer-v69-20261107/`。

- 输入框按内容从1行增高至5行，超过5行内部滚动；删除常驻权限标签，
  保留更短的用量行。移动端群名、平台和选群区压缩为90px，空输入页脚109px，
  桌面空输入页脚114.5px；保留此前Markdown、工具默认折叠及最终总结置后。
- 重新读取左侧增加圆圈感叹号，打开当前群的能力说明。只读取服务端context，
  展示可查询、需本人确认的建议、不可用项及原因，不调用模型或提交操作。
  平台、用户或群切换会取消旧读取，迟到响应不能写入新的帮助弹窗。
- 两条执行路径（核心Runner及Eino适配器）覆盖带伪系统指令的工具返回，
  验证Shell、HTTP、SQL、直接发送／保存／审批及测试授权创建均不成为可执行工具。
  双平台隔离数据库覆盖替换群／账号、伪造超管字段及native_debug、伪造确认；
  无越权计划或原生操作，文字声称已确认仍只生成待本人确认建议。
  这些是合成权限边界回归，不冒充真实供应商模型的拒绝率测试。

| 检查 | 本轮实测 |
| --- | --- |
| 产品前端 | 类型检查及最终Docker构建通过；构建54.649秒，无新增依赖 |
| Linux race/sodium | 513项顶层测试通过，51.740秒，无race报告 |
| 串行隔离PostgreSQL | 仅wsl_test；control 101项／175.006秒、HTTP 149项／483.153秒、gateway 79项／20.847秒通过 |
| 最终镜像浏览器 | 9套回归全部通过；双平台320／390／768／1440，320另测600px短屏，10张本轮截图 |
| 交互与安全 | 1／3／5行高度及超限滚动、帮助顺序、只读请求、迟到隔离、短屏关闭通过；变更及外部请求均0 |
| 架构与仓库 | update／check、架构及仓库守卫、53项工具测试、3项渲染缓存测试、Mermaid通过 |
| CodeBoarding | 匹配可信基线增量16.506秒通过并保存检查点；分析后端源无变化，供应商请求0，未执行全量或远端CI |
| 容器smoke | 最终镜像40.327秒通过 |

首轮页脚高度、短屏弹窗及快速切换问题的失败记录保留。快速切换暴露现有
Element Plus自动增高组件卸载后的nextTick访问；本轮改用局部原生textarea，
不修改依赖，宽度监听卸载时清理。浏览器等待有限入场动画后再测量及截图，
最终九套完整重跑通过，没有用放宽高度或权限断言消除失败。隔离PG恢复停止，
保留容器和数据卷。

2026-11-07 08:00:02北京时间，仅替换现有Compose Web：
`sha256:93cabdf50c83b6b5c47ad492886a36c098a42da443e84b725cfd0f26ae2e21d3`。
API仍为`acf2f97ff825`；API／热群／数据库容器ID未变，四服务健康，
配置、资源限制、挂载和安全选项保持，前次Web镜像以
`wsl-web:rollback-before-assistant-composer-v69-20261107`保留。发布前后未决提交0。
助手enabled／tenant_access／approvals_enabled保持true、版本17；双平台
模块策略仍版本6、各13项。动作准入、群运行及readiness不变。
08:01:59只读核对线上HTML、JS及CSS，与已验收镜像逐字节一致。

本轮浏览器交互使用合成API；原网站保存会话已过期，未绕过滑块或读取真实
对话正文。真实站点只核对静态资源及服务状态；新增真实模型调用、原生操作、
迁移均0，不将UI部署计为真实模型／群动作验收。根工作区其他候选的
graph／manifest保持原样，完整架构证据保存在冻结候选。交付后文档单独归档，
不修改已验收的冻结运行源码。

## 群管助手富文本、工具顺序与折叠（2026-11-07，v68）

从实际运行v67和冻结源码独立构建，只增加助手Markdown组件、工具折叠及
相关前端验证。锁定markdown-it 15.0.3及构建类型14.1.2，补齐MIT及必要间接
依赖许可；原有锁定包未升级。API、协议、CodecVersion=7、迁移和Compose不变，
没有混入根工作区未验收的其他候选。来源及私有证据：
`var/assistant-render-v68-20261107/`。

- Markdown标题、列表、强调、引用、代码和表格正常渲染；手机表格和代码块
  在自身区域横向滚动，页面无横向溢出。原始HTML转义、图片禁用、危险协议
  不生成链接，外链增加noopener/noreferrer。执行结果摘要继续按字面文本展示。
- 查询工具按服务端原顺序收纳于默认关闭的“处理过程”，支持鼠标和Enter
  展开／收起；最终回答放在工具之后，默认可见。成员／消息选择、配置及动作
  确认、失败提示、可信失败或未知结果均留在折叠区之外。刷新不提交或调用模型。
- 最终产品类型检查、Docker构建、511项Linux race/sodium通过；串行隔离
  `wsl_test`：control 100、HTTP 149、gateway 79项通过。测试库恢复停止并保留卷。
- 最终Web镜像8套浏览器回归通过，包括两平台320／390／768／1440视口的
  表格、HTML注入、默认折叠、步骤顺序、先文字后工具的SSE、选择与确认、
  失败／未知结果、历史删除和切换隔离。键盘补测曾暴露合成页首次同hash导航
  复用未认证状态的时序问题，补显式reload后5.303秒通过，失败日志保留。
- 架构与仓库守卫、53项架构测试、3项渲染缓存测试、Mermaid及容器smoke通过。
  匹配可信基线的CodeBoarding增量通过，后端分析源无变化，本轮供应商请求0，
  检查点保存；不冒充一次新的付费模型审阅，未运行全量分析或远端CI。

2026-11-07 07:11:13北京时间，仅更新现有Compose的Web：
`sha256:cf9c83fd945f5c9e1b3463d014da5fa4f0c2c4fdd4adf1686d3cdab420f07abf`。
API仍为`acf2f97ff825`；API／热群／数据库容器ID均未变，四服务健康，
配置、资源限制、挂载和安全选项逐项一致，旧Web镜像保留。发布前后未决0，
助手保持enabled／tenant_access／approvals_enabled=true、版本17，双平台
模块策略版本6、各13项，不再把正常使用开关恢复关闭。动作准入与readiness不变。
07:13:06只读核对线上HTML、JS及CSS与已验收镜像完全一致。原保存网站会话
已过期，未绕过滑块重新登录，未读取真实对话正文；本轮浏览器交互使用合成API，
真实站点仅验证静态资源与服务状态，不将其算成真实模型或原生动作验收。
模型调用、新原生操作和新增迁移均0。根工作区已有其他候选的graph／manifest
保持原样；本发布的完整架构证据保存在冻结候选，不覆盖其他人的工作。

## 可扩展群管业务Agent（2026-11-07，v67）

从实际Compose v66冻结，只纳入助手能力目录、按平台模块开放、结构化历史、
内置中文指引、可信结果和回执观测及相应UI／验证。059前向迁移已应用；
CodecVersion=7、依赖、Compose和原生解析保持，其他未验收候选未混入。

最终510项Linux race/sodium、串行wsl_test control 99／HTTP 149／gateway 79、
双平台历史批量计数专项、产品类型／镜像、8套四视口浏览器、架构／仓库守卫、
53项工具、3项缓存／Mermaid、匹配DeepSeek基线增量及容器smoke通过。
10轮重复计划仅2条密封记录读取SQL，授权查询仍实时执行；不作生产容量结论。
首轮迁移遗漏触发自动回滚，后经现有Compose migrate修复；回执边界、
夹具与计数池的失败及修正记录均保留。

02:11:12部署API／Web，02:28:43及02:35:55仅补更新API。
最终API`acf2f97ff825`、Web`6338009a2096`，四服务健康、四接收器在线；
配置、资源及回滚镜像保留，热群／数据库未重建。林夕历史发送匹配接收事件233032，
页面准确区分平台回执和接收观测，没有重发，也不宣称用户已读。

双平台在初版及v1.1指引各完成一次真实只读查询，共4回合／12次模型，
平台演示账本0.045147 USDT、冻结0，不扣客户。没有新配置计划、保存或原生尝试。
全局开关和平台开放策略恢复原值；新的两平台发送因缺少有效测试认领未执行，
不计作本轮原生通过。隔离PG停止并保留，readiness不提高。
完整实测见[业务Agent验收](assistant-agent-20261107.md)，扩展契约见
[模块开发](assistant-module-development.md)。

## 群管助手结果总结与历史衔接（2026-11-06，v66）

从实际Compose v65冻结，只修改助手结果投影、对话总结、历史上下文及对应验证。
保留Eino v0.9.22、建议直接返回、3次模型／6次工具／90秒限制；不为总结新增
模型调用，不引入依赖、服务、迁移、原生解析或其他未验收候选。
配置和动作总结来自当前重新授权的持久计划，确认、取消及核对后单次GET刷新；
旧版本正文继续隐藏，仅仍有权读取的结果可恢复。

最终504项Linux race/sodium，串行wsl_test control 95／HTTP 148／gateway 77项，
产品类型与镜像构建、最终Web镜像7套浏览器、架构／仓库守卫、53项工具、
3项渲染缓存及Mermaid、匹配DeepSeek可信基线增量与容器smoke均通过。
新双平台数据库专项14.133秒、结果浏览器28.847秒；视口320／390／768／1440。
CodeBoarding咨询1次请求、33284上报Token，持久检查点保存，无全量回退。
新浏览器首轮同hash导航等待已修正并通过；旧顶部抽屉用例因产品入口早已移除，
原样保留及记录失败，不计作通过，由当前独立页七套验收覆盖。

2026-11-06 22:32:08已仅部署API／Web：`fac70598e76e`／`9581626d7560`。
四服务健康，配置、资源、安全选项及热群／数据库容器ID保持，
紧邻镜像保留为`rollback-before-assistant-outcomes-v66-20261106`。
无迁移，发布前后未决提交0、业务状态保持。
原旺商聊发送及两平台配置记录重复回读均恢复具体总结；浏览器实际展示与接口一致。
回读前后模型21、原生设置尝试79、上传12、回合5、配置执行3均保持。
未增加真实模型、扣费或原生发送；配置历史总结不等同当前配置或生产效果。
22:35复核四接收器connected且租约／观测新鲜；隔离PG已停止并保留。
准入、全局开关及readiness保持，普通客户真实认领与套餐灰度不因此计为通过。
详细记录见[助手结果总结](assistant-outcomes-20261106.md)，私有证据在
`var/assistant-outcomes-v66-20261106/`。

## 业务验收问题修复（2026-11-06，v65）

冻结现有Compose v64，只修改会话刷新取消条件及助手建议后的回合结束逻辑。
498项Linux race/sodium、最终Web镜像7套浏览器、架构及仓库守卫、
匹配DeepSeek可信基线增量与容器smoke通过；串行wsl_test control 89／HTTP 148／
gateway 77项通过。HTTP首轮旧调用／账单断言失败已修正并完成全包重跑。
19:59:32已部署API／Web，四服务及四接收器正常，配置／资源及紧邻镜像保留。
双平台真实配置回合均成功，确认保存后还原；6次调用平台记账0.009973 USDT，无冻结。
20:33旺商聊本人完成测试认领后，正式助手提出单条文本计划、本人确认、持久发送及
大蛤独立接收均通过；仅一条发送尝试。两次调用记账0.003893 USDT，无冻结或客户扣费。
准入、认领及助手开关均还原，助手版本12；20:39复核未决提交及自动任务为0。
辣椒聊助手发送尚待独立测试认领与真实验收；客户群归属及自动业务保持未验收、关闭。
详细实测、首轮失败和真实业务结果独立记录于
[双平台业务验收修复](business-acceptance-20261106.md)，不能以配置保存代替原生发送通过。

## 网站登录与机器人授权滑块（2026-11-06，v64）

冻结当前Compose v63及`var/demo-friend-v63-20261106/source`，候选为
`var/slider-captcha-v64-20261106/source`。本轮只修改网站滑块、其保护入口、
登录品牌和页脚，以及必要的测试和文档。无数据库迁移、原生解码或Compose配置变化。

登录品牌“A哥后台 - 机器人”，删除原登录页脚；机器人接入创建、
好友核验、旧授权码申请及超管演示认领增加服务端一次性票据验证。
仍保留网站会话、CSRF、原生权限、套餐及持久执行核验，不执行真实好友或群操作。

初次依赖操作被`deps=ask`拒绝的记录保留；所有者随后明确授权`deps=allow`，
锁定`github.com/wenlng/go-captcha/v2 v2.0.5`、
`github.com/wenlng/go-captcha-assets v1.0.7`及唯一新增间接模块
`github.com/golang/freetype v0.0.0-20170609003504-e2365dfdc4a0`。
其他模块版本保持，未引入Redis或生产服务；许可证及FTL选择说明同步。

| 检查 | 本轮实测 |
| --- | --- |
| 产品前端及API | 类型检查、Vite及镜像通过；最终Web`fecc464725f3`，首次45秒、缓存复核2.668秒；API`947d16d535e8`，45.394秒 |
| Linux race/sodium | 495项顶层通过，43秒；包含完整验证码生成、状态及HTTP合成回归，无race报告 |
| 串行隔离PostgreSQL | 仅wsl_test；control 89项／121.842秒、HTTP 148项／334.210秒、gateway 77项／19.565秒通过 |
| 合成浏览器 | 最终镜像7套通过：滑块、主题、客户接入、好友向导、助手绑定、归属恢复及演示认领 |
| 视口与交互 | 320／390／768／1440，双平台，鼠标／触摸／键盘、刷新、过期、取消、用户名及平台迟到响应隔离通过 |
| 架构及仓库 | update／check、架构／仓库守卫、工具回归、渲染缓存及Mermaid通过 |
| CodeBoarding咨询 | 匹配DeepSeek可信基线增量通过，38.854秒，1次请求／33124上报Token；保存持久检查点，无全量回退 |
| 容器smoke | 最终API／Web镜像31.286秒通过；真实GoCaptcha图片、缺失／错误票据、重放拒绝、空库迁移、权限及默认关闭 |
| CLI及CI适配 | 双步人工滑块CLI包含于上述race；测试会话helper另5项Linux race通过；Python 10项合成回归通过；未运行远端CI |

检查覆盖作用域替换、一次消费、过期、并发、容量、限流、取消、
错误及panic隔离。浏览器为合成API夹具，确认无票据不提交、失败须手动重试、
网站验证不自动提交好友或邀请；真实原生及业务模型调用均为0。
首轮320像素断言遇到弹窗入场动画，修正测试为逐帧等待动画完成，
保留原有溢出断言及失败截图；最终7套全部重新通过。

首次完整HTTP数据库回归的两个Host隔离子项因测试CookieJar在连接前拒绝
手工Host而失败；改为测试传输层在Cookie策略之后覆盖Host，保留真实作用域断言，
13项专项21.353秒通过后重新执行完整三包，原失败日志保留。
未放宽生产验证或移除隔离检查。

`wslctl login-challenge`配套人工图片及坐标登录，私有状态独立、一次消费、
无自动重试。容器和CI使用严格限定`wsl_test`及固定合成身份的短期会话夹具，
不创建角色／授权，不进入生产镜像；真实`/me`、CSRF、权限和退出仍检查。
密码＋滑块成功路径由隔离HTTP测试的固定生成器覆盖，不自动求解真实验证码。
CI临时覆盖只用于隔离数据库；现有生产Compose逐字节不变。

冻结输入、最终镜像、检查及失败证据位于`var/slider-captcha-v64-20261106/`，
浏览器结果为`browser-final.json`，新接口及限制见[网站滑块验证](website-captcha.md)。
2026-11-06 **17:38:12（北京时间）**，仅更新现有Compose API／Web；
API镜像`sha256:947d16d535e8b18a593325149b5e40884e0ce7a1efc8a3fd5dae67a522eb0c61`，
Web镜像`sha256:fecc464725f37d4e42b10ca3c87bcb99ae40136110c3352dee68bc5dc6277696`。
四服务healthy；热群／数据库容器ID保持，环境、凭据、资源限制、安全配置、
挂载、端口及网络逐项保持。上一版镜像保留为
`wsl-{api,web}:rollback-before-slider-captcha-v64-20261106`。

部署后320／390／768／1440四视口实际GoCaptcha图片、品牌、页脚、
取消不登录及无横向溢出通过；实际HTTP确认缺失票据、错误坐标、重放及
未登录访问授权挑战均拒绝，响应禁止缓存。未使用真实密码或自动求解验证码。
17:42:22核对四个接收器（旺商聊2、辣椒聊2）均connected，租约及观测新鲜。
发布前后未决提交0，绑定、业务准入、运行、聊天／规则任务、好友尝试及AI调用数保持；
本轮无真实原生提交或业务模型调用，未开放好友灰度。
无数据库迁移，`CodecVersion=7`、原生／AI准入及readiness保持原状态。

部署及上线结果为`delivery-after.json`、`live-smoke.json`、`runtime-smoke.json`，
实际截图位于`output/playwright/slider-captcha-v64-live/`。
隔离PostgreSQL已停止并保留容器及数据卷。
根工作区保留其他候选与既有报告，事实图、目录同步及仓库／架构守卫独立通过；
本轮发布证据仍使用冻结候选，不将根工作区图或远端CI视为发布验收。

## 双平台好友核验、专属演示空间与统一用户中心（2026-11-06，v63）

本轮从实际Compose v62及`var/assistant-binding-v62-20261106/source`冻结，
仅纳入空间权益、好友接入、统一用户中心及其验证。保持`CodecVersion=7`，
解析、接收算法、依赖锁定、Compose及既有迁移逐字节不变；
未验收的解析、媒体及轻量化候选不随本轮发布。

- 056为真实有效超管建立唯一私有`operator_demo`空间，后续创建／启用超管
  在同一管理事务中建立。普通客户不能加入、选择或取得其他超管私有空间；
  GET不创建空间。现有客户归属与历史套餐保持。
- 用户中心默认本人空间，共用群列表、向导、配置、助手及费用页面。
  “超管专属 · 不限额”永久有效，额度为空并明确标注不限额，不可销售或分配，
  不创建订单。显式“全部授权群”仍可查看有效客户绑定。
- 添加信息分开返回内部身份和搜索账号。旺商聊读取密封会话`account_id`，
  辣椒聊读取自己的`user_id`，保留原字符串身份；创建时有界读取认证基本资料，
  补齐原生昵称。进度刷新只读本地记录，不重复读取上游。
- 好友验证码为“群接入 ”加20位随机Base32码，10分钟、一次使用。
  本人点击核验后，核对唯一传入、待处理且收件机器人正确的申请，持久同意一次；
  057独立保存好友证明及尝试，不伪造消息证据，不记录任意申请附言。
  同账号审批串行并保持至少5秒间隔；未知结果只核对，取消不删除好友。
- 好友证明不直接产生群归属。邀请接受、独立群挑战、角色核验及本人确认
  继续分开；新绑定业务关闭。好友审批具有独立平台／机器人准入，初始关闭。
- 演示空间后台任务使用实时服务上下文及`workspace_demo / platform_workspace`，
  与本人`operator_demo / platform_demo`分别统计；058扩展计费约束。
  两者计入平台AI总预算，不扣客户钱包；客户后台仍受客户权益、钱包及运行开关约束。

| 检查 | 本轮实测 |
| --- | --- |
| Linux race/sodium | 448项顶层通过，46.251秒；集成测试另行连接隔离数据库运行 |
| 串行隔离PostgreSQL | 仅wsl_test；control 89项／140.792秒、HTTP 135项／310.295秒、gateway 77项／19.777秒通过 |
| 产品类型检查及镜像 | API 22.048秒、Web类型检查及构建34.765秒通过 |
| 架构及仓库 | 候选update／check、两项守卫、53项工具回归、3项渲染缓存及Mermaid通过 |
| 容器smoke | 最终API／Web镜像通过，26.298秒；空库迁移、默认关闭及安全运行检查通过 |
| 合成浏览器 | 12套通过；320／390／768／1440、100／300群分页、本人／客户空间、双平台接入、套餐、助手及删除／迟到响应 |
| CodeBoarding咨询 | 当前源码匹配DeepSeek可信基线增量通过，54.530秒，1次请求／34433上报Token，持久检查点保存；无全量回退或远端CI |
| 部署后真实只读页面 | 两平台API合同及40组视口通过；工作台、本人空间、接入弹窗、群列表、套餐费用、助手，无HTTP写入、溢出、脚本异常或5xx |
| 真实接入准备 | 两平台各创建1条本人空间挑战并取消；添加身份刷新一致、验证码长度及关闭准入正确，无好友同意、邀请、绑定或业务模型调用 |

合成回归覆盖两平台、跨用户／空间替换、客户不能取得专属权益、100／300群分页、
租约及账号版本变化、好友请求方向／待处理状态、并发点击、一次同意、取消、
未知提交及重启恢复。另以真实数据库事务验证演示后台实际／冻结费用、
客户钱包不变、平台预算以及生成前后停用重新核验。
进度GET使用到期客户账期做副作用检查：查询不能刷新账期、发积分、
修改绑定版本或触发好友审批；其他既有账期刷新入口保持。

初轮发现并修复客户助手查询误依赖受限角色不可读的租户表、
未知好友尝试被到期任务过早释放、辣椒聊字符串添加账号刷新后消失，
以及接入进度GET继承通用账期刷新副作用。没有通过扩大数据库权限、
放宽身份或重试提交满足测试。相关初轮失败、修复和通过日志均保留。
界面夹具同步默认本人空间、费用页入口与响应完成时序，保留权限和只提交一次断言。
最终CodeBoarding增量首轮遇到上游502，保留该次未知用量与失败记录；
重新执行同一匹配基线增量后通过，没有改业务模型或恢复其他模型的基线。
旧`console_ui_smoke.js`依赖已移除的后台“业务模块”并列入口，本轮该旧专项不计通过；
不存在的`heat_chat_ui_smoke.js`未计作检查，不据此宣称热群真实验收。

本轮未自动发送好友申请、短信、注册或登录。尚无本人手动发送的有效新好友申请样本，
双平台真实好友同意、随后邀请／群证明／绑定及演示后台AI效果仍待低频独立验收；
不得删除已有好友制造样本，也不能用移动端操作或合成成功代替本站通过。
`native_ready`、`ai_ready`和未验收动作的关闭状态保持。
冻结输入、镜像、结果与失败证据位于`var/demo-friend-v63-20261106/`。

2026-11-06 **10:03:38（北京时间）**，现有Compose仅更新API／Web，
前向应用056／057／058；API镜像
`sha256:87746684e72ebdc6293ed2cdb598766c9c31099a89b4d5d19645098a93484b4d`，
Web镜像`sha256:399c644d47924e24c2d9f0da454586b83fb2b37250e60f6922b3daf9a753a180`。
四服务healthy，热群／数据库容器ID保持。环境、凭据、资源限制、安全配置、
挂载及网络逐项保持；上一版API／Web保留为
`wsl-{api,web}:rollback-before-demo-friend-v63-20261106`。
发布前未决提交0；发布前后活动绑定、正式准入、业务运行、聊天／规则任务及AI调用数相同。
独立好友准入无开放记录、好友审批尝试0，readiness不变。
最终复核四个接收器均connected且租约／观测新鲜，接入准备活动预留0；
隔离验收PostgreSQL已停止，保留容器及数据卷。

线上准备读取核对：旺商聊大蛤的添加账号为`340738649`，内部UID为`23691273`；
辣椒聊指定tsx的认证资料返回`9680209`。实际分配按现有容量规则选择机器人，
本次辣椒聊向导分配到另一已登录账号`3129316`，其原生昵称和添加信息正确，
不把tsx的添加号套到其他机器人。两条本地挑战均取消，容量预留释放，
绑定、开关、任务、好友审批及AI调用计数保持；另查tsx资料为只读操作。

主工作区既有v62报告和未发布源码差异均保留，事实图／目录重新生成并独立通过
check、架构／仓库守卫、Mermaid及目录同步测试；不把主工作区图当作冻结候选发布证据。
线上截图位于`output/playwright/demo-friend-v63-live/`，
部署、只读页面及身份准备结果分别为私有目录内
`delivery-after.json`、`live-readonly.json`、`live-identity.json`和`tsx-profile.json`。
这些记录只包含安全元数据，不保存好友验证文字、票据或模型正文。

## 助手内选群与用户中心绑定（2026-11-06，v62）

基线为实际v61-r2运行镜像及冻结源码。助手增加两平台页内切换，
按当前平台分页搜索群组；切换后保持`#assistant`，草稿按网站用户、
租户、平台、机器人和群隔离。切换及取消迟到读取不创建模型回合。
用户中心绑定入口不再跳后台，负责人走本人接入向导；
超管选择已有真实客户，以本人演示身份接入，不代登客户或创建套餐。
同一向导完成分配机器人、App邀请、认证消息核验和本人确认。
客户不能提交目标租户；管理员证明、跨租户转移继续独立审核。
免除演示的商业名额不免除全局300群、单账号30群、预留、当前角色、
epoch、版本、期限、来源准入和最终提交检查；绑定后业务关闭。

新增`GET /group-binding/context`提供有界客户选择，默认20、最多50，
分页和搜索均在服务端；打开／关闭向导不预留账号或调用原生目录。
055新增不可变`applicant_mode`，旧记录默认客户模式，旧迁移字节不变；
按实际已应用迁移核对后前向执行，没有修改或清空生产历史。
仅纳入本轮必要源码，保留工作区已有登录文案及其他未发布候选。
协议解析、密封格式、`CodecVersion=7`、Compose配置及依赖版本不变。

| 检查 | 本轮实测 |
| --- | --- |
| Linux race/sodium | 407项顶层通过，80.011秒 |
| 串行隔离PostgreSQL | 仅wsl_test；control 71项／186.362秒、HTTP 115项／295.544秒、gateway 70项／20.283秒通过 |
| 双平台接入回归 | 无套餐超管新绑定／恢复、真实资源客户、客户不可替换租户、身份与epoch变化、邀请未知结果不重放、原生管理员审核、独立转移审核及运行关闭通过 |
| 产品前端及API构建 | API 50.286秒；最终Web类型检查／构建41.857秒；依赖不变 |
| 合成浏览器 | 8套相关专项通过；320／390／768／1440，300群、客户搜索／分页、对话删除、草稿、只读开关、重新核验及迟到结果；无真实原生或模型调用 |
| 最终r2专项 | 长草稿／快速缩放及切平台、对话删除、接入向导复跑通过，分别6.706／28.149／9.217秒 |
| 架构及守卫 | 冻结候选update／check、架构／仓库守卫、53项工具测试、3项渲染缓存与Mermaid通过；根工作区事实证据另行更新 |
| CodeBoarding咨询 | 匹配DeepSeek可信基线增量通过；首轮1次请求、33745上报Token；r2最终复查新增请求0、检查点保存，无全量回退或远端CI |
| 容器smoke | 最终API／Web组合28.011秒通过 |
| 最终线上只读UI | 两平台工作台／绑定窗口／助手共26组通过，含509px助手；HTTP写入、原生变更、业务模型、脚本异常及5xx均0 |

初期合成夹具修正了隐藏radio点击、页面尚未完成加载就挂起刷新请求、
以及超管夹具遗漏接入权限的问题，保留原失败日志，权限断言没有放宽。
首轮线上检查发现Element Plus textarea的autosize延迟回调在切平台销毁后
读取空节点；两次失败记录保留。r2移除该JS测高，采用两行回退及CSS内容伸展，
最大128px、超限滚动；新增快速切换与长草稿回归后重跑通过。
未升级依赖、吞掉异常或通过等待掩盖该问题。

迁移的单独`shasum`计算被本地`hash=deny`守卫拒绝，没有改用其他方式计算；
本轮未追加055摘要锁项。发布脚本逐字节核对全部已应用旧迁移，
055原文随冻结源码保留；现有仓库守卫通过。

2026-11-06 **06:20:04（北京时间）**更新现有Compose API/Web，应用055；
发布前未决提交0，原生及业务开关、活动绑定、自动任务和AI调用数前后一致。
**06:34:54**仅更新Web修正上述输入框问题，API、热群、数据库容器不再重启。
最终API镜像`sha256:ebf913ca6187bf0c5ca0b333cd6a8dba1e64759c0c9dfd76053ebb885b3ea3ea`，
Web镜像`sha256:e6edc42228189031b68d473e2f7f83fdca06b9c98bf1267f0249bf0f6970f3dd`。
四服务healthy，环境、资源、安全配置、挂载和网络保持；
上一API/Web及r1 Web分别保留在`rollback-before-assistant-binding-v62-20261106`
和`rollback-before-assistant-binding-v62-r2-20261106`标签。
隔离测试PostgreSQL已停止，保留数据卷。

本轮没有在真实群发挑战、接受邀请或执行群管操作，接口及合成完整链路通过
不替代真实客户App邀请和证明效果。正式准入和readiness保持原结论。
私有结果、两次发布、初次失败与冻结源码位于
`var/assistant-binding-v62-20261106/`；最终真实截图位于
`output/playwright/assistant-binding-v62-live/`。

## 公共 SVG、工作台接入入口与界面精修（2026-11-06，Web v61）

按所有者本轮反馈继续使用`web-design-templates`的Lumenwave Finance，
保留指定参考项目的图形、灰白工作面、黑色主操作和少量黄绿指示。
后台、用户中心、登录、侧栏及顶部助手入口、欢迎页、回答头像和群列表助手按钮
统一复用`AdminMark.vue`的实际SVG，支持系统减少动态效果。
共享图形不改变工作区身份或服务端权限。
群选择入口和候选群列表则使用实际所属平台的旺商聊／辣椒聊原图标，
切换平台同步更新，补充所有者标注的509px视口及资源成功加载断言。

工作台首屏固定显示当前平台的接入步骤和按钮，已有群时也能继续接入。
负责人使用原有带平台向导；超管演示进入后台绑定，并按权限显示客户邀请；
成员、无套餐、缺权限和平台不包含分别展示实际原因。
套餐明细与完整费用留在“套餐与费用”，工作台保留四项权益概览。
历史操作收成最近5条，“查看全部”进入完整记录；未改变历史保留或增加请求。
1000px及以下改为两列权益和分行群操作，避免保留侧栏的平板挤压文字、
将主要按钮藏在宽表格右侧；实际截图另行核对，不能只以页面无溢出判定观感。
登录、侧栏、公共标题、筛选区、表格、行操作、输入、弹窗和助手群组／历史／输入区
统一调整层级、间距和焦点。没有新增前端依赖、远程字体或产品演示路由。

候选从实际运行v60及`var/logo-onboarding-v60-20261106/source`冻结，
只变更8份产品前端文件及本轮回归、文档、生成证据。
539份cmd／internal／deploy输入、Compose、依赖、所有迁移、消息结构与
`CodecVersion=7`逐字节保持；未使用整个脏工作区构建，旧解析、媒体和轻量化候选
没有混入。根工作区事实图另行update／check和渲染，不用它替代冻结候选证据。

| 检查 | 本轮实测 |
| --- | --- |
| Linux race/sodium | 398项顶层通过，39.732秒 |
| 串行隔离PostgreSQL | 仅wsl_test；control 66项／106.799秒、HTTP 110项／238.724秒、gateway 70项／19.362秒通过 |
| 产品类型检查及构建 | 最终Web镜像通过，32.566秒 |
| 合成浏览器 | 14套通过，覆盖主题、空群及已有群指引、双平台助手／删除、客户接入、演示／认领、套餐、补证明、账号指标、项目监控、网关、成员／费用和消息 |
| 关键交互 | 320／390／768／1440，另含509／635短屏；首屏操作、双平台带入、两列指标／分行操作、角色及套餐限制、300群分页、最近5条和完整记录、唯一空白会话、删除／取消、迟到响应及SVG精确坐标／减少动画通过 |
| 架构及仓库 | 冻结候选update／check、两项守卫、53项工具回归、3项渲染缓存及Mermaid通过；工作区事实检查和渲染独立通过 |
| CodeBoarding咨询 | 匹配DeepSeek可信基线增量通过，持久检查点保存；本轮1次新增模型请求、28923上报Token，最终源复查新增请求0；无全量回退或远端CI |
| 容器smoke | 实际API与最终Web通过，25.792秒 |
| 部署前后真实只读UI | 各94组通过：23个页面范围×4视口，双平台助手各另加509px；无横向溢出、脚本异常、5xx、HTTP写入、原生变更或业务模型调用 |

回归发现最近操作区在768px固定列过宽，改为窄屏分行后，两项受影响专项和
92组实际数据预览重跑通过。初次失败与修正日志保留。
早期对话夹具中工作区切换后的SVG断言位置也已纠正；不通过放宽权限修正测试。
平板布局改用分行群列表后，套餐和认领夹具仍按旧640px阈值查找桌面表，
改为检查实际可见列表后重跑通过，原套餐、权限和只提交一次断言全部保留。
以上为界面及既有接口回归，不新增原生效果、客户生产闭环或容量通过记录。
隔离PostgreSQL已停止，保留原数据卷。

2026-11-06 **05:03:34（北京时间）**最终仅更新现有Compose的Web：
`sha256:98320cc57cfef3accfd3d47969f367ded3c33d8acd8faf4d73ac19dcc0802d1a`。
04:39:01曾发布初版，随后按平板实拍及所有者群选择图标反馈完成本节补充；
两次均只更新Web。四服务healthy，API仍`4c2e9aac5243`、热群仍`7d72c9ac1eca`，
API／热群／数据库容器ID及启动时间均与v60冻结时一致。
环境、凭据、资源、安全配置、挂载、网络及原生连接保持。
紧邻Web保留为`wsl-web:rollback-before-workspace-polish-v61-r2-20261106`，
v60回滚镜像也保留为`wsl-web:rollback-before-workspace-polish-v61-20261106`。
正式准入、群运行、自动任务及AI业务调用计数发布前后相同；
未新增迁移，`native_ready`、`ai_ready`不变，没有真实原生提交或业务模型调用。

冻结源码、逐项检查和交付记录位于`var/workspace-polish-v61-20261106/`，
最终部署记录为`delivery-r2-after.json`、只读检查为`live-readonly.json`；
真实页面截图为`output/playwright/workspace-polish-v61/`，
部署前预览为`output/playwright/workspace-polish-v61-preview/`。
具体视觉约定见[界面约定](ui-reference-20261106.md)。

## 参考 Logo 修正与用户中心空群引导（2026-11-06，Web v60）

按所有者反馈，将后台简化三角形替换为参考项目`AdminBrandMark.vue`的实际
三段交叠SVG，保持其坐标、叠放顺序、4秒换色与浮动节奏；减少动态效果时停止。
用户中心当前平台没有授权群时，展示准备群聊、邀请机器人、核验绑定的接入指引。
负责人入口受套餐、平台及接入权限约束；未分配群的成员联系负责人，
超管演示可前往管理后台绑定群组。筛选无结果仅提供清除筛选，
加载中或读取失败不展示首次接入。向导带入当前平台，打开和关闭不提交。
创建群聊仍由用户在官方App完成，没有新增原生建群接口。

基线是实际v59及`var/lumenwave-ui-v59-20261106/source`，只修改产品中的
`AdminMark.vue`、`UserCenter.vue`，另增加合成浏览器专项及文档。
539份cmd／internal／deploy源码逐字节不变，Compose、依赖锁定、消息结构、
`CodecVersion=7`及全部迁移不变。没有以整个脏工作区构建，也未夹带旧候选。

| 检查 | 本轮实测 |
| --- | --- |
| Linux race/sodium | 398项顶层通过，32.197秒 |
| 串行隔离PostgreSQL | 仅wsl_test；control 66项／120.451秒、HTTP 110项／244.211秒、gateway 70项／19.419秒通过 |
| 产品类型检查及构建 | 最终Web镜像通过，34.315秒 |
| 相关合成浏览器 | 7套通过：空群专项、主题、客户接入、对话删除、套餐、演示及重新核验 |
| 空群与Logo专项 | 双平台×320／390／768／1440；负责人、成员、超管、套餐停用、缺权限、平台不含、清除筛选、加载／失败、向导平台和只读开关通过；SVG精确坐标、4个动画及减少动态效果通过 |
| 架构及守卫 | 候选update／check、架构／仓库守卫、53项工具回归、3项渲染缓存及Mermaid通过；工作区事实证据单独update／check通过 |
| CodeBoarding咨询 | 匹配DeepSeek可信基线增量通过，检查点已保存；新增模型请求0、token0，无远端CI或新一轮模型推理 |
| 容器smoke | 实际API与最终Web通过，24.753秒 |
| 部署后真实只读UI | 双平台后台Logo、用户群列表及筛选空结果共24组通过；无溢出、脚本异常、5xx、HTTP写入、原生变更或模型调用 |

首轮合成夹具的延迟请求、启动刷新等待及线上检查的Vue动画作用域后缀断言
已修正并重跑通过，初次失败证据保留。未将移动端展示、合成检查或页面上线
计作新的原生业务验收；既有未通过项目仍保留原结论。

2026-11-06 **03:38:19（北京时间）**，现有Compose仅更新Web：
`sha256:6444cef86861f75d2d9b144a8901669393068db33fe8ed66ed805b8497ff4dd3`。
四服务healthy；API仍`4c2e9aac5243`、热群仍`7d72c9ac1eca`，API／热群／数据库
容器ID和启动时间完全不变。环境、凭据、资源、安全配置和原生连接保持。
紧邻Web保留为`wsl-web:rollback-before-logo-onboarding-v60-20261106`。
正式准入、群运行、自动任务及AI调用计数在发布前后相同，readiness不变。
隔离PostgreSQL已停止并保留数据。私有源码和结果：
`var/logo-onboarding-v60-20261106/`；真实截图：
`output/playwright/logo-onboarding-v60/`。

## Lumenwave 整体界面适配（2026-11-06，Web v59）

本轮按所有者指定的“多租户平台接续 · 本地工作区”读取实际源码及截图，
采用该项目正在使用的 Lumenwave Finance 视觉语言。参考项目只读，没有修改。
活动主题统一为灰白画布、黑色操作／选中导航、少量黄绿指示及语义色；
保留本地字体、品牌图标、独立助手入口和原有工作区／平台分离。
详见[界面约定](ui-reference-20261106.md)及主题目录中的原说明、许可证。

- 登录、导航、顶部工具栏、指标与图表、表格、分页、表单、弹窗、消息及助手区统一适配。
  桌面侧栏224px，可折叠为68px；分组可收起，移动端保留导航抽屉。
- 修复登录输入框双层焦点、桌面提示在缩小窗口后遮挡移动按钮的问题；
  网站会话恢复后重新同步合法页面哈希，监控等页面刷新不再误回总览。
  “跳到工作区”只移动焦点，不改变路由。
- 既有作用域弹窗、冲突草稿、迟到响应隔离、助手历史删除／唯一空白会话、
  最近10轮、双平台单群范围及本人确认均保留。未增加前端依赖或深色模式。

冻结基线为实际v58镜像及`var/operator-claim-v58-20261106/release-v58/source`。
候选位于`var/lumenwave-ui-v59-20261106/source`，由冻结版单独应用本轮UI差异，
没有使用整个脏工作区构建。539份cmd／internal／deploy输入逐字节一致，
Compose、依赖锁定、Go代码及所有迁移不变；`CodecVersion=7`。
工作区原有未发布的解析、媒体、轻量化及范围候选未混入本轮。

| 检查 | 本轮实测 |
| --- | --- |
| Linux Go race/sodium | 398项顶层测试通过，33.135秒 |
| 串行隔离PostgreSQL race | control 66项／109.493秒，HTTP 110项／262.748秒，gateway 70项／20.340秒；仅wsl_test |
| 产品前端 | 冻结候选类型检查及生产Web镜像构建通过，最终29.066秒 |
| 现行合成浏览器 | 13套通过：主题、客户接入、账号指标、项目监控、网关、成员／费用、演示、认领、套餐、补证明、双平台助手及消息 |
| 视口及交互 | 320／390／768／1440，另含635／647与短视口；100／300群、分页、折叠、焦点、历史删除、确认弹窗、配置冲突及迟到响应通过 |
| 架构及仓库 | 冻结候选update／check、两项守卫、53项工具回归、3项渲染缓存与Mermaid通过；工作区事实图单独更新／检查 |
| CodeBoarding咨询 | 匹配DeepSeek可信基线的增量通过，检查点保存；本次新增模型请求0、token0，不冒充新一轮模型推理或远端CI |
| 容器smoke | 实际v58 API＋最终v59 Web通过，24.965秒；隔离空库迁移、安全运行、默认关闭及静态资源检查通过 |
| 部署后真实只读UI | 18个页面范围×4视口，共72组通过；无横向溢出、脚本异常或服务器5xx，浏览器业务提交／真实原生变更／模型调用均0 |

旧测试脚本同步到当前两个工作区、平台选择、20条分页和作用域校验后再运行，
未以放宽产品权限满足测试。首轮失败与修正日志保留。
`business_modules_ui_smoke.js`仍依赖已移除的旧并列业务入口，在原v58与候选v59
同一位置复现定位失败；该旧专项不计为通过，本轮没有修复或提高财务／抽奖业务验收。
本次界面检查不代替原生效果、客户生产闭环或容量验收。

2026-11-06 **02:58:24（北京时间）**，仅更新现有Compose的Web：
`sha256:a1689ba2692d0f9dbe66cab254a0114044d2e3a01ddcaefe530433cc5abd52f3`。
API仍`4c2e9aac5243`，热群仍`7d72c9ac1eca`，数据库保持原实例；
三者容器ID、启动时间均未改变，API／原生连接未因本次发布重启。
四服务healthy；环境、凭据、CPU／内存限制、挂载、网络与安全配置逐项保持。
上一Web保留为`wsl-web:rollback-before-lumenwave-v59-20261106`，无迁移。
部署及只读页面检查前后，正式业务准入、自动群管、热群任务与AI调用数完全一致。
`native_ready`、`ai_ready`及原有生产执行准入不变。

最终源码、日志、逐字节来源核对及交付结果保存在
`var/lumenwave-ui-v59-20261106/`；真实页面截图为
`output/playwright/lumenwave-v59/`。隔离验收PostgreSQL已停止，保留原数据卷。

## 普通客户套餐与超管限制核验（2026-11-06，运行版v58）

本轮按所有者要求，以当前v58冻结源码及实际发布Web资产测试普通客户，
验证超管分配的套餐、能力上限与成员权限是否真正由服务端执行。
运行源码776份逐字节与v58冻结输入一致；仅补测试及测试工具，没有修改生产代码、
发布新镜像或变更真实客户套餐。生产现有普通客户没有有效套餐，未重置其密码、
创建虚构生产付款或要求所有者另行注册。所有合成用户、账期、配置和预算
仅存在于串行隔离的`wsl_test`。

| 场景 | 本轮结果 |
| --- | --- |
| PLUS／PRO群数 | 双平台分别验证PLUS 1群、PRO 3群；满额后接入HTTP返回409，控制层确认为`group_quota_exceeded`，未产生机器人名额预留 |
| PLUS／PRO成员数 | 负责人计入成员总数，PLUS 3人、PRO 10人；达到上限后409 `member_limit_reached`，数据库人数未超限；既有并发抢位、停用释放与重启用检查通过 |
| 助手套餐资格 | 即使显式授予全部用户权限和租户上限，PLUS仍不能读取助手上下文或创建会话；PRO在客户访问开放时可读取本群上下文并创建空会话，不调用模型 |
| 已登录会话 | 超管通过正常HTTP更换旧制测试套餐，回复／助手权限在同一会话内立即收缩；停用后禁止配置保存，重新开通可恢复，不依赖重新登录刷新菜单 |
| 到期 | 双平台分别推进隔离订阅时间，原会话立即失去付费配置与助手权限，旧会话正文读取也403；用户中心返回inactive，历史授权群查询保留 |
| 超管能力上限 | 付费套餐和旧的模块权限不能补回被超管移除的字段；变更撤销旧网站会话，重新登录仍403；客户不能修改自己的套餐或超管上限 |
| 成员及客户隔离 | 未分配群、其他客户群、字段拒绝、代审批及伪造演示／计费身份被拒绝；负责人身份不能由旧成员管理权限或标签推断 |
| 演示权益边界 | 超管本人可在客户到期后保存配置；客户和自动任务不能继承演示权益，平台、动作及来源正式准入保持独立 |
| 积分与费用 | 并发预算预留不超额，用途限额生效；未知费用冻结、恢复不重发、对账及更正幂等；仅使用注入的合成模型，没有实际供应商调用或费用 |
| 接入与容量 | 临时接入预留、账号／平台容量、平台套餐范围、旧式配置与字段接口一致性回归通过 |

本轮Linux race/sodium与隔离PostgreSQL共**25项顶层测试、26项子测试通过，无跳过**：
既有HTTP 8项／29.845秒、控制层8项／11.046秒；
新双平台会话2项／19.466秒、新群数及成员数1项／27.485秒；
容量和配置HTTP 4项／13.024秒、积分HTTP 2项／7.541秒。
隔离数据库按顺序使用，没有并行重建。

实际v58发布资产的合成浏览器回归为**40组通过**：旺商聊／辣椒聊，
PLUS／PRO／PRO无助手权限／到期／未开通，分别覆盖320／390／768／1440视口。
校验助手按钮、接入入口、到期提示、客户不能出现超管控件及页面无横向溢出，
查看页面无HTTP写入；12份截图已留存并抽看。
浏览器使用合成API响应，服务端授权证据来自上述真实HTTP、网站会话和PostgreSQL测试，
不将界面隐藏替代接口拒绝证据。

私有源码、日志、结果为`var/tenant-limits-20261106/`；截图位于其中
`source/output/playwright/customer-package-*`。新增长期回归为
`internal/httpapi/customer_package_test.go`与`tools/customer_package_ui_smoke.js`。
本轮没有原生真实变更、真实模型调用、生产客户修改或生产服务重启；
运行镜像仍为v58，四项生产服务healthy，readiness与正式动作准入未提高。
这是当前运行版的合成套餐／授权验收，不代替真实客户接入、模型及原生业务效果验收。
没有生产源码和架构边界变更，未新增发布、CodeBoarding调用或远端CI。

## 超管测试认领直接闭环（2026-11-06，v58）

所有者明确要求超管测试不再依赖另行注册客户、登录客户负责人或开通套餐。
本轮从实际运行v57及对应冻结源码抽取23项认领、人工准入、用户中心和必要回归，
没有发布工作区未验收的解析、轻量化或媒体候选。`CodecVersion=7`不变。

**实现：** 用户中心“授权群组”增加“测试认领”。实时超管会话从有效绑定解析
平台及机器人，持久预留checking记录后，由现有认证连接读取一次真实原生角色。
机器人须为当前群主或管理员，成功形成仅限本人、单群、10分钟的独立演示证明。
不创建客户归属证明、不更换绑定、不发挑战消息，也不发放套餐。
manual／assistant仍各自检查正式准入和最终原生角色；客户及自动消费者不能使用演示证明。
查询和重开弹窗不请求原生，重复编号不重读；解除、过期、权限和作用域变化均失效。
检查中重启记为interrupted，不自动重复读取。上游错误仅保留固定安全分类。

054为唯一新增前向迁移，仅增加演示证明表、约束及平台RLS；旧迁移逐份保留原字节，
039／041继续不发布。原账号、绑定和商业数据不作转换。

| 检查 | 本轮实测 |
| --- | --- |
| 双平台专项回归 | 无套餐超管、客户拒绝、字段与平台拒绝、当前角色、取消、过期／版本／epoch、并发幂等、安全错误、恢复及最终提交栅栏通过 |
| 全量Linux race/sodium | 19个有测试包、397个顶层测试通过 |
| 串行隔离PostgreSQL race | control 132.774秒、HTTP 246.616秒、gateway 18.293秒；仅重建wsl_test |
| 产品前端 | 类型检查、独立生产镜像构建通过 |
| 合成浏览器 | 认领、客户补证明、既有演示共3套；双平台四视口、100／300群分页、关闭不提交、迟到响应及未知结果只读核对通过 |
| 架构与仓库 | 更新／检查、两项守卫、53项工具回归、3项渲染缓存及Mermaid通过 |
| CodeBoarding咨询 | 匹配DeepSeek可信基线的增量通过；1次请求、32706上报token、usage_unknown=0，可信检查点已保存 |
| 容器smoke | 最终实际API/Web镜像、空wsl_test迁移与启动、登录、隔离、安全运行、静态资产及默认关闭检查通过 |
| 部署后真实界面 | 两平台各320／390／768／1440视口共8组通过；无溢出、脚本异常，打开／关闭不提交 |

收尾另补双平台到期与重启专项，隔离PostgreSQL race为3.165秒：
到期记录不能进入人工准入，检查中断恢复为failed，原幂等键回读不再调用原生。
该补充只增加合成测试，没有再次修改生产源码或操作真实群。

首轮交付预检在任何生产变更前发现：生成目录属于冻结运行版，而工作区目录另含
未发布的范围精简候选。保留冻结版生成目录，并以候选目录同步测试验证；
其余本轮源码逐份比对工作区后才交付。没有把工作区整个目录覆盖发布。
源码事实、模型咨询、隔离执行和真实效果分别记录；本轮未运行远端CI。

2026-11-06 01:09:41（北京时间）已仅更新现有Compose API/Web：

- API `sha256:4c2e9aac5243bc2be305c743cf610e04c812509bf96a6d0cdd569ee5bc56b3c3`。
- Web `sha256:a86efbdf5ce54983c84ce490d3df492ce1c28d485f4953758b5d2c6c16b6c56e`。

四服务healthy，热群和数据库未重建。环境、凭据、资源限制、挂载、网络、
安全配置及部署模式逐项保持；上线前后未决提交均0。上一API/Web以
`rollback-before-operator-claim-v58-20261106`保留，054为可兼容的增量表。

**真实测试已完成并还原：** 01:10:49旺商聊唯一“测试群”绑定的“林夕”、
01:10:51辣椒聊唯一“测试群”绑定的“受控测试号”，均通过用户中心真实页面
明确点击核验，认证连接返回当前owner，各形成一条active证明。每平台只读取一次
原生角色；进度读取沿用原编号，没有重读。随后均通过同一弹窗解除，最终两条均cancelled。

前后逐项核对原绑定、完整配置、运行、客户归属记录、套餐及用户数量、业务准入、
助手开关和原生操作数量完全一致；绑定版本仍26／1。原生变更0、新客户0、新套餐0、
受控授权0、AI调用0；四个启用接收器均connected且观测新鲜，活动测试认领为0。
正式动作准入仍关闭，`native_ready / ai_ready`未提高。

以上是**超管测试认领**的真实验收；实际客户接入仍按本人认证消息证明归属，
不能将演示证明当作客户所有权或自动任务授权。超管不再需要客户负责人会话、
非号池申请人或商业套餐来完成本轮测试。最终私有证据、源码和还原结果位于
`var/operator-claim-v58-20261106/release-v58/`，真实页面截图位于
`output/playwright/operator-claim-v58/`。隔离验收PostgreSQL已停止并保留数据。
下文v57的前置阻塞只描述当时旧客户流程。

## 双平台认领补核验与诊断修复（2026-11-06，v57）

所有者要求自行核对“哪个号不是管理”并修复认领阻塞。本轮基于实际运行v56，
只抽取认领、用户中心入口及其回归；不发布工作区中未验收的解析、媒体或轻量化候选。
无新增迁移，`CodecVersion=7`，既有原生动作和AI准入没有提高。

**修复内容：**

- 同租户有效旧绑定缺少归属证明时，负责人可按当前绑定版本重新核验，保留原机器人。
  已暂停证明也使用该入口。创建、查看、取消挑战均不修改原绑定；真实认证消息证明及
  本人确认／管理员审核完成后，才保存证明、提升版本和epoch、保留配置并关闭运行。
  已有效核验、跨租户、版本冲突和无挑战的旧人工审核仍不能借此重复改绑。
- 用户中心群列表返回 `ownership_state`，负责人按套餐与权限看到“重新核验”。
  弹窗固定群和平台，草稿按本人、租户、平台、群及版本隔离；超管演示和普通成员
  不能通过这个入口伪装客户负责人。
- 核验进度准确区分号池身份、非群主／管理员、私聊与群内身份变化及其他固定原因。
  上游读取失败保留安全分类，不记录任意正文。单条消息失败后不因刷新重试；
  新的有效消息仍可完成原挑战。页面明确说明应重新发送有效核验内容。

**实际名单核对：** 2026-11-05 23:48（北京时间）逐平台各读取一次完整名册。
旺商聊“林夕”为群主，“大蛤”为普通成员，两者均是管理用途号池账号；
“小满”为非号池管理员。原生普通成员身份不等于非号池客户身份。
辣椒聊名册当时只有“tsx”群主，属于管理号池；另一受控账号连接正常，
但没有出现在该测试群当前完整名册中。未选择其他人的账号，也没有调整原生角色。

| 检查 | 本轮实测 |
| --- | --- |
| 重点双平台认领回归 | 旧有效绑定补证明、配置保留与运行关闭、跨租户／版本拒绝、重复确认、号池和普通成员拒绝、身份变化、安全错误及有界重试通过 |
| 全量Linux race/sodium | 19个测试包、394个顶层测试通过 |
| 串行隔离PostgreSQL race | control 98.664秒、HTTP 230.274秒、gateway 18.306秒；仅重建wsl_test |
| 产品前端 | 类型检查、最终独立镜像构建通过 |
| 合成浏览器 | 双平台及320／390／768／1440视口通过；固定群与版本、关闭不提交、分群草稿、同挑战重开和权限隐藏通过 |
| 既有演示回归 | 100／300群分页、单次操作、动态准入、未知结果不重发、迟到响应和计费单位通过 |
| 架构与仓库 | 更新／检查、边界及仓库守卫、53项工具测试、3项渲染缓存及Mermaid验证通过；仅更新两份生成证据文件 |
| CodeBoarding咨询 | 匹配DeepSeek可信基线增量通过；1次请求、33799上报token、usage_unknown=0，可信检查点保存 |
| 容器smoke | 实际API/Web镜像、空wsl_test、客户邀请、演示权益、页面计划及默认关闭状态通过 |
| 部署后真实浏览器 | 双平台各320／390／768／1440视口共8组通过；无页面溢出、脚本异常、HTTP写入或原生变更 |

重点回归、完整隔离执行、源码事实检查、模型咨询与真实认领分别记录；
本轮未运行远端CI。未把合成证据作为真人挑战证明。
前端子任务的首轮临时浏览器脚本曾定位移动端隐藏的桌面元素而超时；
保留该限制说明，最终冻结资产上的完整双平台四视口及演示回归均通过。

2026-11-06 00:20:22（北京时间）已仅更新既有Compose API/Web：

- API `sha256:af3007658bb1d0fbd0ed954ec70753981165ac2d134f4dde60ea41c8f756f83b`。
- Web `sha256:b5dcca0eb41fb8d1df95371a3e1b051971c3b48bc33ced5d2c6f8e88c62c3127`。

四服务健康，数据库和热群未重建；环境、凭据、挂载、网络、资源限制和部署模式保持。
上一API/Web镜像保留为 `rollback-before-claim-v57-20261106`。上线前后未决提交均0。
00:21:58只读核对两群均返回 `ownership_state=unverified`，绑定版本仍26／1，
四个接收器connected且租约和观测新鲜；助手三个开关仍false。
两群原绑定、运行及归属记录与认领前基线逐项相同，无新增原生变更。
本轮启用的隔离验收PostgreSQL容器已停止，保留容器和数据。

**真实认领仍未完成：** 仍需所属客户负责人网站会话，以及由实际控制的非号池
群主或管理员发送本次挑战。不能以“大蛤不是管理员”为由将号池身份当作客户；
“小满”是旺商聊名册中的角色合格候选，不能仅凭名册推断其账号归属。
辣椒聊当前名册没有非号池申请人。未重置旧客户密码、创建虚构订单、伪造证明、
更改群成员或借用受控授权；无需执行还原操作，原测试状态已保持。
真实原生业务动作及 `native_ready / ai_ready` 仍未通过本轮验收。

冻结源码及检查、镜像、只读核对记录位于
`var/claim-v57-20261105/release-v57/`；完整认领契约见
[客户接入文档](customer-onboarding.md)。

## 双平台测试群认领链路前置核验（2026-11-05）

所有者授权使用两平台唯一“测试群”验收认领并在完成后还原。
本轮已保存原绑定、业务运行和归属证明基线，逐平台各执行一次完整原生成员读取。
两群群主均属于现有号池；旺商聊另有1名非号池成员，辣椒聊没有非号池成员。
尚未确认其中有可由所有者控制的非号池群主或管理员，不能替任意成员认领。
认领扫描按既有契约排除号池机器人发出的挑战，因此不能用机器人文本发送代替客户证明。

现有网站测试客户的负责人仍有效，但无活动网站会话，未找到可复用登录凭据；
其套餐已停用且过期。使用当前超管会话分别调用两平台创建认领接口，
均准确返回 `tenant_owner_required`，没有创建挑战或伪装客户负责人。
这属于身份边界核验通过，不是认领、绑定或原生业务动作通过。

实际认领尚待：客户负责人网站登录；两平台各指定一个不在号池中的自有申请账号，
并核验其在测试群的群主或管理员角色。管理员申请须经过正常超管审核。
在这些条件满足后，再安排可恢复的测试权益、挑战及逐动作灰度；不直接写入证明，
不借用受控测试授权作为正式业务准入。现有绑定、开关、配置与基线逐项一致，
原生变更0，未重置客户密码或撤销绑定，readiness保持false。
私有结果和还原基线位于
`var/operator-v56-20261105/claim-acceptance/preflight.json`及同目录`before.json`。

## 管理后台、用户中心与超管演示权益交付（2026-11-05）

本轮已交付两个工作区、服务端演示权益、动态助手准入、独立页面动作计划、
分页群列表、本人用量及热群最终运行检查。超管默认后台，使用业务功能进入用户中心；
客户和自动任务保持客户权益。演示不限商业额度，不取消号池、并发、频控及平台成本上限。
当前契约见[工作区与演示权益](operator-demo-workspaces.md)。

冻结输入为实际运行v55及对应源码，实施前工作区另存私有快照。
验证候选为 `var/operator-v56-20261105/release-v56-r4/source`，
先抽取61个本轮文件，之后仅校正两份测试夹具并同步受影响架构证据与交付文档。
`CodecVersion=7`；解析器、领取、I/O合批、未验收媒体和轻量化候选均未混入。
仅新增053前向迁移，已应用迁移逐份保持原字节，039/041未发布。

| 检查 | 本轮实测 |
| --- | --- |
| Linux全量race/sodium | 19个有测试包、392个顶层测试通过 |
| 串行隔离PostgreSQL race | control 99.530秒；最终完整HTTP 217.732秒；gateway 18.269秒，均仅用wsl_test |
| 产品前端 | 类型检查、独立生产构建通过 |
| 合成浏览器 | 两平台、100/300群、每页20项及320/390/768/1440视口通过；准入动态变化、目标令牌、迟到响应、未知结果不重发及费用单位通过 |
| 架构事实与边界 | 候选更新/检查、架构与仓库守卫、53项工具测试、3项渲染缓存测试及Mermaid验证通过 |
| CodeBoarding咨询 | 匹配DeepSeek可信基线的增量通过；1次请求、33120上报token、usage_unknown=0、可信检查点已保存 |
| 容器smoke | 实际构建镜像、空wsl_test、053隔离、邀请激活、演示计费及页面计划检查通过 |
| 部署后真实浏览器 | 四视口通过；HTTP写入0、脚本异常0、无溢出 |

首轮失败没有删除：旧测试仍将超管视为客户套餐受限、旧历史字段白名单和目录夹具滞后；
按新契约修正断言，客户缺套餐仍拒绝。HTTP新夹具使用了不存在的owner角色码，
修正为显式角色后局部回归7.286秒、最终完整HTTP再次通过。
gateway首轮发现子用例复用被撤销会话及SQL参数类型不明，改为独立夹具和明确bigint，
最终完整隔离回归通过。生产代码冻结后未再改变。
CodeBoarding首轮在两次请求后遇上游502，62033已上报token、unknown0；
保留失败日志，第二次仍执行增量，通过后才发布，没有全量回退或替换不匹配基线。
源码事实检查、咨询分析、隔离执行和真实业务验收分开记录；本轮未运行远端CI。

2026-11-05 22:49:40（北京时间）仅重建现有Compose API/Web，均healthy：

- API `sha256:734f5560780b84784306bf9ba0915c49f167a6ab149b8e7efe4f495a79f037c6`。
- Web `sha256:e01f214c776f055aa1dc0c837d45677b4e2aab1006da7833ba3dfde92aa9ce0a`。
- 热群仍为 `sha256:7d72c9ac1eca44abacb9c267dd405a0c0e82e2fa88928df901a256c7249ae7bd`，数据库未重建。

环境、凭据、挂载、网络、端口、安全配置及部署模式逐项比较一致；
API 1核/512MiB、Web 0.25核/128MiB、热群0.5核/256MiB、数据库1核/1GiB保持。
上线前未决提交为0；原业务运行状态、正式准入、聊天/规则任务和隔离记录不变。
上一API/Web镜像以 `rollback-before-operator-v56-20261105` 标签保留。
`native_ready`、`ai_ready`均保持false。

**真实模型验收：** 旺商聊和辣椒聊唯一“测试群”各一次只读助手回合成功，
在客户无有效套餐、自动业务关闭时仍使用超管本人演示权益。
共6次模型调用全部结算，`operator_demo / platform_demo`，不关联客户账期，
实际费用8,718,000微积分，即0.008718 USDT，冻结费用0。
客户钱包和原生操作数均未变化，没有配置计划、原生计划或原生提交。
助手全局开关仅在有界验收期间开启，完成后按版本恢复原关闭设置；
未开启客户访问、审批、自动业务或热群。安全元数据见候选目录
`real-readonly-assistant.json`，不记录提示词、回答或密钥。

**原生效果仍待验收：** 两平台旧测试群绑定均没有群归属证明记录。
演示商业不限额不绕过该最终栅栏；正式页面/助手动作保持关闭。
部署后两平台各创建一个独立页面文本计划，均显示blocked，确认均返回
`business_admission_closed`；随后通过原接口取消，最终均cancelled。
原生操作、发送尝试、模型调用、准入、客户运行及后台任务均无新增，
结果见 `real-closed-gates.json`。这验证关闭准入的真实阻断，不代表原生发送成功。
须由所属客户完成既有接入挑战证明，再建立逐平台、逐动作、逐来源的测试群灰度，
才能用正常业务入口低频验证效果；受控测试授权和App演示不能替代这项业务证据。
本轮真实模型成功不作为原生动作、自动业务或全天稳定性通过。
全部检查与交付证据保存在 `var/operator-v56-20261105/release-v56-r4/`。

## 旺商聊移动端演示对齐核验（2026-11-03）

15:00–15:02新群文本、视频引用、@、图片、语音、视频均已到达双方接收器，
七条应用消息逐对认证及完整投影摘要一致；15:01:49新增群撤回通知已持久接收。
引用301只有client/server标识，没有文字，双方精确匹配同群原视频。
原页面直接绘制空文字，现改为仅从当前会话已加载、双标识精确匹配的原文
生成摘要；无跨群缓存或自动补拉。密封消息结构及CodecVersion=7不变。
初查175条私聊传输头未找到两自有账号之间的身份对。
15:43随后只读核对15:00–15:04本账号的三条私聊，定位15:00:27图片、
15:00:50带引用文本及15:03:23语音，认证与归属均有效。
不输出正文、对方身份或资源凭证，不扩展其他时间历史，不下载媒体。

新增独立移动端样本记录，九项显示“移动端样本已核验 / 本站执行待验收”，
不冒充本站持久发送或撤回动作通过。`read_quote`视频引用读取独立登记，
旺商聊27组/30独立项有指定场景记录，生产完整验收仍0，readiness不变。
范围、逐项结果与剩余缺口见[本轮记录](native-mobile-demonstrations-20261103.md)。
检查与交付完成后续记；初轮回归发现目录测试仍固定旧数量，已同步新证据数量，
不改执行门槛；初轮私有探针遇到只读挂载及旧信封格式，未产生原生网络请求。

最终全量Linux race/sodium通过；串行HTTP隔离PostgreSQL race通过
（118.040秒）。未改gateway执行与schema，其数据库回归复用v22的18.225秒结果，
未称作本轮重跑。产品类型检查及构建、引用双标识/缺省原文回归、
两平台各四视口目录回归、四视口消息与引用浏览器回归均通过，
合成浏览器真实原生请求和脚本异常均为0。
当前冻结源码架构事实检查、架构/仓库守卫、52项工具测试、
3项缓存测试及七幅Mermaid检查通过，新增引用辅助模块只更新前端视图及证据。
匹配可信基线DeepSeek增量通过，1次请求、25305 token、usage_unknown=0，
source_hash=`841f51dd9af5510b8a75ba2cfd68c4b2b26602517e409676d2497ca52518231c`；
未初始化、未披露运行数据，可信检查点保存；未运行远端CI。
最终API/Web镜像独立容器smoke通过，新建`wsl_test`，原生/AI默认准入仍关闭。
15:30:06完成API/Web交付，API镜像
`sha256:54565dd88c79558f9c91640392ab449d3b6c0af90511fb25bf99953ba3c9b34e`，
Web镜像`sha256:fb19322ad09b0875aad6394bec1e87582c61fad61f8bb5a51f9c1e43ea8e416b`。
环境、限额、挂载、网络保持一致，数据库和热群引擎未重建，无迁移，上一镜像保留。
15:40真实页面核对视频引用显示“视频”、两平台验收记录和十二项人工分离通过；
四个接收器connected且观察新鲜，浏览器异常/原生写入为零，无活动测试窗口。
初轮页面脚本未等待作用域稳定，修正空账号值并等待目标行后通过，未修改产品代码。
随后仅补两项同期私聊目录证据；补充版全量Linux race/sodium通过，
未变包复用Go缓存；目录同步、双平台四视口合成目录浏览器再次通过。
HTTP执行、数据库和Web源码未变，复用本轮118.040秒隔离HTTP race、
产品构建及消息/引用浏览器检查，不称作补充版重跑。
补充API与同一Web镜像的独立容器smoke通过，新建wsl_test、无真实原生动作。
当前架构事实/边界/仓库守卫、52项工具测试、3项渲染缓存测试及七幅图验证通过；
工具测试首轮未显式设置模块模式而命中旧vendor，补齐GOFLAGS=-mod=readonly后通过，
没有改依赖或vendor。DeepSeek匹配可信基线增量通过：
1次请求、27548 token、usage_unknown=0，可信检查点已保存，
source_hash=`a8a520efc1eb334f69d27491ea51f850d55c0d2ca7980a6f36c3c2b0a8954030`。
15:58:51补充版仅更新既有Compose API，最终镜像
`sha256:57e983aeef7456d98f9a666e598d3c6b849665196ac37de4a7bc1dcb47ac8641`，
healthy；Web仍为上述已验证镜像，数据库/热群引擎未重建。
环境、配额、挂载和网络一致，无未决提交或迁移，上一API镜像保留。
15:59真实接口与页面同时确认九项移动端样本，含独立私聊文本/图片记录；
引用读取指定场景绿色、视频摘要可见，本站发送未冒充受控执行通过。
旺商聊27组、辣椒聊4组历史受控证据及人工分离保持正确，
辣椒聊未继承本轮样本。四个接收器connected、观察新鲜，无活动测试窗口；
浏览器异常及原生写入均为零，readiness保持false。

## 原生群管底座与人工操作分离（2026-11-03）

十二项人工能力及账号设置子项移至独立人工页签，不计入群管底座分母；
后端业务依赖和助手能力目录共同拒绝人工能力。自动好友审批退出当前
业务目录，旧ID、配置和人工审批入口保留。文件发送继续不接入。
旺商聊底座35个父能力、39个独立项，26个父项/29个独立项有历史受控证据；
原27条证据均保留，其中账号资料归入人工区。辣椒聊不继承证据。

完整只读复核本群双方198条留存；60条认证应用消息中有两条@，
无引用字段或原生引用路由。未读取其他群正文、重放消息或改变解码状态。
发现双方接收器在IM续期阶段阻断，各用既有业务会话换票一次后恢复连接；
14:05:58双方同步完成、观察新鲜、队列为零。无短信、注册、新原生登录、
批量动作或自动重发。续期初始失败未复现，不宣称根因消除或全天稳定。

发送及独立受控撤回仍有真实接线/准入缺口，详见
[逐项边界及实测](native-consumer-boundaries-20261103.md)。
未把既有App样本等同本站发送通过，生产准入/readiness未提高。
本轮Linux全量race/sodium通过；串行HTTP隔离PostgreSQL race通过
（99.903秒）、gateway隔离race通过（18.225秒）；产品类型检查及构建通过。
双平台各320/390/768/1440四视口合成浏览器检查通过：十二项人工操作独立展示、
底座分母不含人工项、普通用户不出现号池人工页签、无原生写入及脚本异常。
工作区同步后的目录回归与产品类型检查也通过，但不将工作区未发布候选纳入交付。

当前冻结源码的架构事实检查、架构/仓库守卫、52项工具测试、
3项渲染缓存测试和7幅Mermaid检查通过；仅更新图证据及哈希。
DeepSeek匹配可信基线增量通过：1次请求、26314 token、usage_unknown=0，
source_hash为`643c64565646a86b3fa1b9f2e2923a63bf88fe17926646c015ca28a815bb265b`，
可信检查点已保存，无初始化、旧基线替代或运行数据披露；未运行远端CI。
最终API/Web镜像独立容器smoke通过，使用新建`wsl_test`；
额外核对双平台人工能力归属、退出自动好友审批、默认发送关闭。
未使用生产数据或发送原生动作。

14:33:24–14:33:35仅更新既有Compose的API/Web；
API镜像`sha256:26f96bd48e1ab8bd94ad6f81cf9ffc42ec8a8f1d3a6d1d05c6768e2ce7c5544d`，
Web镜像`sha256:5c71b921879debdac5eb8eb329e4d075c8e240367a4e7dce102b0d60bffd45d9`，
均healthy；环境、配额、挂载及网络逐项一致，数据库/热群引擎未重建，
上一镜像保留，无迁移；重启前未决提交为零。
14:35真实页面逐平台核对十二项人工能力已移出底座且独立显示，
旺商聊26组、辣椒聊4组受控证据显示正确；浏览器异常/原生写入均零。
四个既有认证接收器connected且观察新鲜，两群绑定有效、群管/AI关闭、
无活动测试窗口。14:36旺商聊双方再次确认接收队列和解码待办/处理中均为零。
以上没有补齐剩余发送、独立撤回或引用样本缺口，不构成完整生产验收。

## 旺商聊继续受控核验（2026-11-03）

06:23普通成员本人群名片修改、恢复两次均持久成功，逐次独立回读一致；
300秒窗口只用两次预算，随后关闭并恢复临时动作开关。无批量、自动重发、
短信、注册或新原生登录。原已认证会话在连接重建后同步及ACK正常；
双方两条新名片通知的原生标识与完整投影摘要一致，正确归群，
当前解码积压为零。以上仅短时恢复及新通知接收，不代表全天或故障矩阵通过。

真实页面验证图片1302×683及原图弹窗、三条聊天记录展开、
联系人名片和发言者头像、720×1280视频封面及12.37秒实际播放。
语音四秒投影及3312字节固定源资源可读，但产品播放未接齐，保持未通过。
本轮明确新增两项父能力历史证据，旺商聊为27项；辣椒聊仍独立四项。
所有生产准入和readiness保持原值。
旧的“无样本”等结论已明确指向
[当前逐项清单](wsl-native-acceptance-20261103.md)，避免功能表说明滞后。
本轮检查及API证据更新交付结果在下文续记。

本轮目录Linux race/sodium与同步检查通过（1.036秒），工作区目录测试也通过。
执行、数据库、依赖和前端未变，复用v19全量race、串行隔离PostgreSQL及
产品构建的通过结果，没有将复用称作重新执行。
当前候选架构事实check、架构/仓库守卫、52项工具测试及七幅Mermaid检查通过；
DeepSeek匹配可信基线增量通过，1次请求、27153 token、usage_unknown=0，
source_hash为`2fd9d25202cdc394e2162427b0cd54548bb2962a42513a7ad065853f948337f2`，
可信检查点已保存。新API镜像与未变Web的独立容器smoke通过，使用新建
`wsl_test`，未使用生产数据，无原生动作。没有运行远端CI。

06:47:19–06:47:25仅更新既有Compose的API，镜像
`sha256:b82fd5809c1be47cbfef8d9a94663d3e1b1ad02600f120ffb66536a9df8a9781`，
healthy；环境、限额、挂载和网络一致，Web/数据库/热群引擎未重建，
上一API镜像保留，无迁移。06:48部署后真实页面显示旺商聊27项、
辣椒聊4项，旺商聊撤回弹窗8条；脚本异常和原生写入均为零。
最终四个已认证接收器均connected且观测新鲜，两群绑定有效，
本轮窗口cancelled、临时动作开关关闭，native_ready仍false。

## 辣椒聊隔离测试绑定与角色验收（2026-11-03）

在已验证的v19修复部署后，超管按所有者授权为唯一测试群建立隔离绑定：
网站租户所有者明确、原生群主角色现读、同平台版本和接收器均核验；
不创建商业订阅，管理与AI模块关闭。两个接收器原本均在线，无重新登录。

05:48:15升管理员、05:48:22撤销管理员，持久回执均明确成功；
目标账号自身连接分别于05:48:16、05:48:23核验管理员和普通成员。
只操作一个自有目标、一个300秒窗口、两次预算，没有批量、重发或额外窗口。
05:48:28关闭窗口，05:48:31恢复原测试/动作开关；接收保持启用。
独立登记四项辣椒聊历史证据，不继承旺商聊或提高生产准入/readiness。
真实浏览器核验旺商聊撤回弹窗有5条通知、辣椒聊测试群可见，
无原生写入或脚本异常，媒体自动读取已阻止。
本轮检查、镜像交付与限制见[本轮记录](native-test-preparation-20261103.md)。
06:00:48完成v20证据更新的API单服务交付，健康且配置/资源限制未变化，
保留上一镜像；无数据库迁移。06:03–06:04真实页面与只读接口再次确认
旺商聊25项、辣椒聊4项历史证据，以及辣椒聊绿色管理员验收状态；
四个已认证接收器均connected，两群绑定有效、本轮窗口均关闭。
最终元数据race/同步、当前架构守卫、匹配基线增量和容器smoke通过，
执行链全量race、串行数据库和未变化前端检查复用v19通过结果。

## 旺商聊新样本与双平台测试准备（2026-11-03）

遵照所有者“禁止批量操作、先旺商聊”要求，实际仅对一个自有成员串行升权和
撤权各一次；两次持久操作明确成功，目标账号当前连接分别核验管理员和普通成员。
窗口关闭、临时动作准入恢复关闭。准备阶段连接重建曾中断一次，未提交原生变更。

双方新文本、JPEG、视频、语音元数据、名片、三条合并记录均成功解码到测试群；
新撤回通知各一条，按群过滤且匹配各自原消息。图片、封面、MP4、名片头像和
发言者头像各一次有界读取均HTTP200/no-store，无媒体落盘。语音播放仍未接齐；
本轮新窗口未检出引用/@投影，不把这些或本站发送误记为通过。
功能表新增五项限定受控读取证据，更新图片及双端角色证据；生产准入不变。

辣椒聊两个接收器本轮均connected，群主角色已现读。测试/动作开关关闭且没有
测试群绑定；已修复动作准入和认领码的WSL硬编码。新增独立超管隔离测试绑定：
明确租户、已确认网站所有者、同平台账号、版本、原生角色、接收新鲜度和容量
全部核验，管理/AI保持关闭，不创建商业订阅或动作窗口。
新双平台受控准入/绑定PostgreSQL race回归通过，6.838秒。
后续完整检查与交付结果在[本轮记录](native-test-preparation-20261103.md)补记。
本轮不带入解码、轻量化、媒体候选，不加迁移、不提高readiness。

## 项目自身监控改造（2026-11-03）

gopsutil/v4 v4.26.9替换Simon；API与热群引擎只采自身进程/cgroup，
Web仅静态服务可达性，PostgreSQL仅当前业务库统计，ECharts按服务展示。
最终适配版本已通过全量race/sodium、串行HTTP（148.534秒）和gateway（18.719秒）隔离集成、
产品构建、四视口浏览器回归、架构与仓库守卫、工具与Mermaid检查、
匹配可信基线的DeepSeek增量分析。依赖深查后已限制gopsutil仅自身RSS，
并固定源码移除其整机CPU导入采样；最终源码的新一轮分析通过，
最终镜像1800秒隔离观测通过：361份样本、两Go服务各360次采集、
数据库统计31份；两个SSE客户端各892帧、17次正常重连。
API/热群采集耗时P95为0.787/0.483ms，监控连接上限1，无OOM，
重复读取没有放大采集频率。
03:25:44–03:26:07 Asia/Shanghai已同步既有Compose，
API/Web/热群引擎healthy；Simon容器及专用网络已移除。
PostgreSQL未重建，仅应用045迁移；配置、限额及上一镜像均保留，
readiness仍为false。
部署后另完成300秒只读观测与四视口真实页面检查，
四项服务全程可用、SSE重连正常、浏览器异常和原生写入均为0；
60次API采样、6份本库统计，监控连接上限1。
基线/候选的资源原始范围和未受控负载限制已记录，不宣称性能提升比例。
完整范围、基准、失败记录和发布状态见[本轮记录](project-monitor-20261103.md)。

## 系统监控修复（2026-11-02 开始）

从当前运行的 v16 冻结源码构建 v17，仅修改监控接口、分位数聚合与监控前端。
未混入工作区未发布的解析、轻量化或媒体候选；无数据库迁移，
CodecVersion=7、native_ready=false、ai_ready=false 保持原状态。
详见[监控口径与交互](system-monitor-repair-20261102.md)。

完成内容：按平台合并最近1000条、最长24小时原始观测，展示总解码P95、
延迟直方图和账号消息速率；数据库时间控制采样可用性；
当前链路异常与近7天失败记录、累计隔离数分别显示。
API与Simon环境内存使用独立刻度，CPU说明实际配额占用；
监控标签关闭更新淡入，缺失与过期值不补零。
HTTP/SSE共用范围检查，旧快照不可覆盖新快照；
趋势请求取消旧范围，错误按面板保留，刷新同时更新当前视图，
故障事件去重且最多加载1000条。

已执行检查：

- 全量 Linux `go test -race -tags sodium ./...` 通过。
- 串行隔离 `wsl_test`：完整 HTTP race 101.544秒通过；
  gateway race 18.212秒通过，测试未使用生产数据或发送原生动作。
- 最终Web镜像产品 `vue-tsc -p tsconfig.wsl.json` 与 Vite构建通过。
  既有主包超过2MB的构建提示仍存在，本轮未调整全站打包。
- 从最终Web镜像提取产物，320/390/647/1440四种视口、
  四个监控页签浏览器回归通过；截图及上下两幅曲线像素检查通过，
  页面无横向溢出。验证总P95采用服务端聚合、旧SSE不覆盖、
  历史失败不计入当前链路故障、数据库不可用不显示零、
  历史积压陈旧标记、趋势和事件失败后刷新恢复、标签无淡入闪烁。
  保留群分配未知结果复用同一幂等键的既有回归；原生动作数为0。
- 最终API/Web镜像独立容器smoke通过，包含监控SSE代理、
  历史和事件接口；数据库为新建`wsl_test`，readiness保持false。
- 发布源码架构事实update/check、架构守卫、仓库守卫通过；
  52项架构工具测试、3项渲染缓存测试与7幅Mermaid校验通过。
  本次只更新受影响的源码证据与哈希，图布局未变化。
- DeepSeek兼容可信基线增量通过。首轮1次模型请求、上报25260 token；
  标签过渡修正后再次增量通过，复用可验证结果，新增模型请求0。
  最终source_hash为
  `0016127ed79c4f1f137c0be272bca4d9f2e7936cf9995fc73cc1665a6145e4b5`，
  usage_unknown=0，可信检查点已保存。以上是本地检查和咨询分析，
  没有声称远端CI执行或生产群管验收。

发布镜像：API
`sha256:259861d64e10f98db1b0e8ae55666677819b62daab0d7daccdb99ecbbc431aa9`；
Web
`sha256:de3b718474a4433d640e7b11351f6684823577939dd1616492baec5655d34942`。
2026-11-03 00:22:49–00:23:11 Asia/Shanghai完成现有Compose的API/Web切换，
两服务healthy；环境配置、凭据指纹、CPU/内存/交换限制、挂载和网络均一致。
PostgreSQL/heatbot/Simon未重建，上一API/Web镜像以
`rollback-before-monitor-repair-20261102`保留。
切换前通用操作、设置、媒体上传及助手回合未完成/未知总数为0。

00:24的生产只读复核：旺商聊当前平台聚合1000条，
P95=1.09655ms、P50=0.421ms、P99=3.80152ms、解码积压0；
辣椒聊无有效解码样本，P95=null。近7天3条旺商聊失败记录保留，
辣椒聊原有一个账号观测过期未伪装为在线。
随后直接访问已部署网页，1440/390视口通过，SSE连接正常，
运行总览显示在线2/3、积压0、P95 1.1ms、当前链路异常0；
资源页加载正常、无页面溢出、浏览器运行错误0、原生写入0。
这些瞬时观测不构成容量或长稳结论。

## 消息解码兼容修复与部署（2026-11-02）

本轮按所有者“修复部署”要求，以运行v15冻结源码构建独立v16。
修复四字段aad=0群系统通知的独立只读投影、SDK群通知占位、
已认证提示/公告/红包/游戏类型展示及大图片元数据误拒绝。
通知不进入业务执行或权限判断，缺失目录不猜测会话；
缺失目标ID、空正文继续拒绝。诊断明确7天窗口，并增加按原始接收时间统计的近1小时异常。
详见[解码诊断续记](decoder-diagnosis-20261102.md)与固定客户端证据E46/E47。

部署候选API：
`sha256:b05cc7d431722f3671b49aa9574bfd298c2e68ff6c51d031779960653e5746a5`；
Web：
`sha256:afd41a15c32b87db499947f0433eae6e2515c67076746362de77c6418d340aa7`。
CodecVersion=7只纠正版本6失败/未支持项，保留正常投影；无新增迁移，
不夹带未达标的解析/轻量化/媒体候选。

已实测通过：全量Linux race/sodium；最终gateway隔离PostgreSQL 18.212秒；
产品类型检查与构建；新镜像独立wsl_test容器smoke；
架构事实check/守卫、仓库守卫、52项架构工具测试、3项渲染缓存测试及7图校验。
最终完整HTTP隔离回归95.483秒通过，先前版本同套回归109.546秒通过。
DeepSeek匹配可信基线增量通过，source_hash
76627309b3c780c28a9e01453e7ff9fea62027cf4cdd13d727a17e36973205e7，
1请求，上报26646 token，usage_unknown=0、供应商失败0，已保存可信检查点。
这是咨询性分析与本地检查，不声称远端CI或生产能力验收。

新镜像提取的实际前端产物通过消息、资源、视频与实时增量四套浏览器回归；
320/390/647/1440视口通过，包含通知文字转义、只读标记、大图占位、
切账号迟到响应、撤权及有界历史。初轮浏览器运行器读取了旧web/dist，
新增断言如实失败；补齐产物路径参数后使用候选镜像资源通过，
没有将旧产物结果当成新修复证据。

工作区同步采用运行基线三方合并，并单独适配其尚未发布的固定槽解析候选；
工作区相关四包race通过，不把该候选作为本次运行版。
22:52:37–22:52:49 Asia/Shanghai完成现有Compose的API/Web切换，
两服务healthy；配置、凭据哈希、CPU/内存/交换限制、挂载和网络保持一致。
PostgreSQL/heatbot/Simon容器未重建，上一API/Web镜像均已独立标记保留。
切换前通用操作、设置、媒体上传及助手回合的未完成/未知数均为0。
以固定事件上限核对版本6成功投影的event_id、codec及decoded_at摘要一致，
确认未重算正常历史。没有执行短信、注册、原生登录、消息发送或群管动作。

22:56:50的生产只读复核：

| 账号 | 已恢复旧异常投影 | 未支持（7天） | 失败（7天） | 未完成 |
| --- | ---: | ---: | ---: | ---: |
| test | 3571 | 40 | 3 | 0 |
| 测试受控1 | 424 | 0 | 0 | 0 |

恢复数按部署前已接收、版本7已成功且尝试数大于1统计，共3995条。
保留的40项均为无法唯一匹配同账号目录的通知；3项为缺失目标ID两条、
空正文一条，不填造身份或清零掩盖。
部署后至此次复核分别收到58/17条新消息，均decoded，新增失败/未支持为0。
两台旺商聊接收器connected、错误为空、队列0。
另一辣椒聊旧踢下线状态未恢复或改写；本次没有新登录授权。
API单次采样75.68 MiB/512 MiB，不作为容量或全天稳定性验收。
`native_ready`、`ai_ready`及生产动作准入均不变。

## 消息中心解码计数只读诊断（2026-11-02）

运行API镜像610ae0ca2c4efb2ebc9ce2b5dea4f42411cbd2c85407a9248ebf95b70dffe6f5，
核对其精确冻结源码，未混入解析/轻量化/媒体候选。
13:47 UTC时test最近7天未支持2301、失败1404；接收器在线、连接队列0，
13:45 UTC待处理/处理中均0，但近1小时仍有新增兼容失败。
两账号1535条packet_signature均为fixed64字段1/2/3及bytes字段4的未兼容封装，
实际在签名AEAD调用前因MessagePacket类型不符返回，不能称密码学签名失败。
两账号native_format2383条均为SDK notification(5)；
application_format182条为群提示/公告、自动回复/通用提示、游戏及红包提示。
11条图片已通过正文认证，2条触及5 MiB大小限制、9条触及1600万像素限制；
另2条图片/视频缺失目标业务ID，1条正文为空。
诊断仅输出固定原因、枚举、结构及计数；生产事务只读，无短信/原生动作、
原包持久明文、清零、重投影或部署。未声称兼容问题修复，
未用本轮替代合成回归及发布检查，readiness不变。
完整口径、限制及修复顺序见
[解码诊断](decoder-diagnosis-20261102.md)。

## 旺商聊完整能力受控核验（2026-11-02）

本轮所有者要求核验旺商聊全部纳入产品范围的原生能力，仍使用唯一“测试群”、
自有群主和自有普通成员。未申请短信、注册或原生登录；排除项不重新纳入。
20:18–20:19 Asia/Shanghai，单个300秒native_debug窗口预算6/6：
成员禁言一分钟、解禁、全群禁言、解除、设置管理员和撤销管理员均有持久成功回执，
每次独立回读确认目标名单/群策略/普通成员→管理员→普通成员变化。
窗口已取消，账号动作开关恢复。角色、完整名单、好友关系与唯一目录映射已现读核验。

两账号测试群留存消息分别58和30条，均decoded、ID唯一且目标群一致；
有文本、图片与@样本；图片、群头像和本人头像通过有界资源接口读取，
响应no-store。未以其他群的语音/视频/转发样本冒充本群验收。
双方七天通知已按原API分页：群主30459条、普通成员1649条，无测试群匹配撤回；
其中非撤回/不可识别通知计数分别17、68，未删除或改写任何消息。
双方没有互相的留存私聊会话；群主没有待处理好友申请，普通成员有一项非自有目标申请，
没有操作该第三方申请。以上缺样本项均未判通过。
群主群发送状态明确native_text_acceptance_pending，媒体入口关闭；
普通成员native_text_group_unbound。nonce历史、生产发送和助手执行门槛未绕过。

昵称修改和恢复两次均明确成功、独立回读相符；随后接收器隔离`4/1/138`。
此标识第三段是正文长度，不是命令号。只读解封确认为内层`3/110/200`、
124字节完整Property，对应固定桌面syncUpdateMyInfo；见E45。
候选仅增加精确路由的完整边界验证及惰性通知保留，不更新身份、权限、聊天ACK或水位；
保留原隔离记录、CodecVersion=6、应用解码算法及其他未达标候选。
修复通过必要检查后仅重建API，镜像e4d1afdea03f9f52de02225c4c53b74cd054bfe0817d993778d6af8e8c15a6ac。
通过原版本化配置将接收器revision25→26正常恢复；账号在线、队列0，原隔离记录保留。
未使用SQL改游标、跳过恢复水位或重放未知提交。

精确运行基线候选v13全量Linux race/sodium及目录同步通过；
串行wsl_test完整HTTP100.610秒、gateway18.426秒通过。
架构事实/守卫、52项工具和3项渲染缓存测试、7张图验证通过；
匹配DeepSeek增量1请求，上报23799 Token、usage_unknown=0、供应商失败0，
持久检查点已保存；source_hash为
`380b6092514a11d8fc20d859251f7cceaf605d46b5541f02bec2e5a3ba089925`。
前端输入未改变，类型检查及构建缓存命中同一镜像；
隔离真实原包经候选读取器验证recognized=true。
此处模型结果仅为咨询性分析，不是CI或原生验收。

20:40公告一次发布成功，独立回读11→12条且正文唯一匹配；该测试公告保留。
20:42大蛤依次完成移出、邀请并接受、本人退群、再次邀请并接受，六次持久成功，
每阶段双方目录/完整名单核对；最终恢复普通成员，所有窗口取消、动作开关恢复。
21:05新增受控编辑入口后，群名称修改/恢复、本人群昵称修改/恢复、
他人群昵称修改/恢复六次均成功，每次独立回读一致。
头像沿用当前本人原图，仅规范化重传：上传、检查、设置及新资源读取成功，
新JPEG36572字节、no-store，昵称未变；不是恢复原资源编号验收。
首次头像请求因测试脚本幂等键不是UUID被本站拒绝，未建立操作、未调用上游；
修正为前端同样的UUID格式后仅实际上传一次。没有自动重传或重发未知操作。
本轮合计22次原生变更明确成功；没有使用第三方成员或好友申请作为目标。

受控编辑候选v14精确冻结自已部署v13，只增加群名称及WSL名片的受控适配，
不向助手/生产开放调试权限。初次构建发现引用未部署的方法，已改用运行版
既有只读Settings路径；修正后全量Linux race/sodium通过，
串行wsl_test完整HTTP129.538秒、gateway18.127秒通过。
新增合成覆盖昵称锁、本人编辑、幂等、未知结果、关闭窗口后只读核对、
取消后拒绝、无商业套餐的调试和生产不激活。
架构事实/守卫、52项工具、3项渲染缓存及7张图验证通过；
匹配DeepSeek增量1请求、26693 Token、未知usage/供应商失败均0、检查点保存，
source_hash为`1a91b5372d7c309a6f0fd2ccf728919d1d93d847de4ed6ffd5d35d3d969935a0`。
Web输入未变，产品类型/构建全缓存命中同一镜像；
容器smoke独立wsl_test通过，未使用生产数据，042–044兼容、无新迁移。
仅API交付镜像17827cd51782893266bca6397548fdd3ca9f7c7b45a3fed4bdd7e183e7078e34，
其余容器、配置、资源限制不变，前一镜像保留回滚。
smoke的product_scope=false、retired_new_submissions_closed=false、chili_media_migration=false
为本轮冻结版预期：未夹带尚未验收的轻量化及041媒体候选，不表示这些候选已交付。

功能目录记录20个父能力的限定范围历史证据，昵称、头像、本人和他人名片分别记录；
已排除通话设置的旧历史不转移为其他字段的证据。
全部40个纳入父项及原子项的逐项结论见
[旺商聊原生能力全量核验矩阵](wsl-native-acceptance-20261102.md)。
发送、撤回、缺样本、故障恢复及生产准入未通过项保持明确阻塞；
不以22次成功声明全部原生能力或生产验收完成。两项readiness保持false。

最终证据候选v15保持v14原生行为，只更新目录及字段/目标独立证据。
Linux全量race/sodium通过，目录生成/同步与全量核验矩阵覆盖测试通过。
首次HTTP集成因仍写死12条旧记录失败，已改为与当前定义数量核对；
重跑完整串行wsl_test HTTP98.241秒通过，gateway18.392秒通过。
架构事实/守卫、仓库守卫、52项工具、3项渲染缓存及7图验证通过。
匹配DeepSeek增量1请求、26021 Token、未知usage/供应商失败均0，
source_hash为`3e3947bee1cf41d67c9e37b247bdec1a252775d645e5257f5cf7e95ea80792ab`，
私有可信检查点保存。产品Web输入仍相同，类型/构建缓存命中原镜像；
最终容器smoke在独立wsl_test通过，预期的未纳入候选标志与v14相同。
21:28:55现有Compose仅API更新为
`sha256:610ae0ca2c4efb2ebc9ce2b5dea4f42411cbd2c85407a9248ebf95b70dffe6f5`，
healthy，配置/资源/其余容器不变，无迁移，上一镜像保留。
21:29最终管理API返回20条限定受控父项证据；原生生产accepted仍0。
双方完整三人名单一致，群主及普通成员身份恢复，两接收器connected/queue0，
临时动作开关false；活动测试窗口、未决操作/设置/上传/模型回合均0。
健康接口native_ready=false、ai_ready=false。浏览器控制不可用，
没有将管理API和原生效果核验描述为新增实页截图或CI执行。

## 助手真实模型与原生受控复验（2026-11-02）

所有者授权在“测试群”进行真实验收。18:19–18:28 Asia/Shanghai，
现有DeepSeek商业路由真实回合成功：能力查询、密封全群禁言建议和最终答复，
共3次模型调用、6462 Token，3笔usage均settled，按既有费率计6.462积分，
没有修改费率或使用架构分析凭据。建议production_admitted=false、
blocked/production_acceptance_required；直接审批因审批开关关闭被拒绝，
随后取消，无原生执行。

旺商聊原生现读确认唯一测试群的群主与自有普通成员后，
建立唯一native_debug窗口：300秒、目标限定、动作限定、预算2。
禁言1分钟和手动解禁均取得持久原生确认；即时禁言名单独立回读
未禁言→已禁言→未禁言，mutes_known始终true。
预算2/2、窗口取消，助手及动作开关恢复原值。
错平台、错绑定账号、未知群和伪造平台正文分别按404/403拒绝。
最终原生未决、活动模型回合、未知费用及活动窗口均0；两台旺商聊接收器在线、队列0。

前置恢复保留了blocked隔离记录及恢复水位；连接换代时的旧角色证明被拒绝，
该次无授权窗口和原生提交，等待新连接后继续，没有重发或扩充预算。
辣椒聊当前没有有效授权群绑定，未进行真实验收，也未推断租户归属。
群改名/文本/撤回、真实租户及助手本人确认执行仍未过；
本轮Tools原生复验不等于助手生产全链路。生产准入与两项readiness均false。
浏览器控制不可用，本轮是管理API及原生效果验收，没有新增实页截图证据。
详细边界与私有安全元数据位置见[本轮真实复验](assistant-acceptance-20261102.md)。

两项功能表证据更新后，精确冻结源码Linux全量race/sodium及目录同步通过；
串行wsl_test完整HTTP 97.425秒、gateway 18.226秒通过。容器smoke、
架构事实/守卫、52项工具及7图验证通过；匹配DeepSeek增量2请求、
上报53724 Token、未知usage与失败均0，检查点保存。
前端相同输入构建全缓存命中并得到同一Web镜像，没有新增实页截图验收。
18:41现有Compose仅API同步验收元数据并healthy，无迁移，原配置/限制保留，
API镜像d097cc89a5c53c18ee4cecbed122962bf0cbc5c9a7fd558890ce6d31234d9211；
Web、数据库、heatbot、Simon未重建，前一API镜像可回滚。
856份cmd/internal/web文件仅验收元数据一份变化；未夹带解析、轻量化或媒体候选。
发布后目录返回本次证据、两台旺商聊在线队列0，未决及活动窗口仍0。

## 双平台助手持久适配与部署续作（2026-11-02）

本轮补齐群名称、群文本和撤回的双平台助手控制适配；所有原生生产准入保持关闭，
模型不能执行或审批。用户已专项授权可信基线初始化和白名单源码披露；
DeepSeek初始化及最终v10增量均通过，持久检查点已保存。
最终冻结候选Linux全量race+sodium、产品类型检查及构建通过；
串行wsl_test完整HTTP 105.487秒、gateway 18.205秒通过，
九组浏览器及前端刷新修复后的三组回归、架构事实/守卫和最终容器smoke通过。
实页发现并修复同身份每15秒刷新误清理群范围的问题；最终Web源码的
DeepSeek增量通过，253文件、9组件、持久检查点已保存。
已通过现有Compose应用042–044并更新API/Web，后续修复仅重建Web。
两个服务healthy，线上资产与已测版本逐字节一致，原配置及资源限制保留，
上一镜像可回滚；两台connected、两台blocked与发布前相同。
本轮真实业务模型、原生写入均0，`native_ready`、`ai_ready`仍false。
实际镜像、检查结果及发布记录见
[本轮续作验收](assistant-acceptance-20261102.md)。
本段及该文最新章节为当前状态，下方候选结论保留为历史。

## 双平台群管助手隔离与Fairweather候选（2026-11-02）

本轮已实现双平台会话/历史/配置隔离、显式动作权限、本人确认密封计划及
七类已有设置/群管动作的共享控制适配，并统一登录与产品工作台主题。
保留另一对话的助手入口；每群没有常驻Agent，模型没有执行或受控测试授权。
完整记录、平台能力差异和镜像见
[本轮助手验收](assistant-acceptance-20261102.md)。

| 检查 | 本轮实际结果 |
| --- | --- |
| 冻结候选 | 基于运行版白名单整理；解析/接收代码不变，旧轻量化和媒体候选未混入，只新增042/043 |
| Linux | cmd/internal/目录生成器全部race+sodium通过；串行wsl_test完整HTTP 96.833秒、gateway 18.130秒通过 |
| 作用域 | 网站本人/租户/平台/账号/群隔离、歧义成员本人选择、撤权与epoch、重复确认/取消、重启未知结果、原操作核对、审计过滤通过；300群分页通过 |
| 前端和浏览器 | 冻结依赖类型检查/构建通过；九组全合成UI回归通过，登录和助手含320/390/768/1440，短视口600及图表像素检查通过，真实模型/原生动作0 |
| 容器 | 修复冻结公开文件0600后，非root只读、静态资源、042/043强制隔离、默认关闭及代理smoke通过；仅一次性wsl_test空库 |
| 架构 | 工作区事实检查/守卫、52项工具、3项缓存、7图验证通过；实际冻结另提取168节点/382边，全部有源码证据 |
| 咨询分析 | 16:13:06 Asia/Shanghai新预检仍compatible_baseline_missing，模型请求/Token均0；没有初始化、披露源码或沿用旧豁免 |
| 未完成 | 群资料变更、群文本和撤回尚无助手持久业务适配；两平台生产准入全部关闭；真实模型/租户灰度/原生验收未执行 |
| 交付 | 候选镜像已构建和smoke，Compose尚未同步；运行镜像、资源和readiness不变，等待匹配可信模型基线 |

## 双平台原生轻量化及群媒体候选（2026-11-02）

本轮按已确认的两阶段范围实施。第一阶段API窄冻结为
`20261102-native-lightweight-v10`，第二阶段为`20261102-native-media-v6`；
前端收尾分别冻结为`20261102-native-lightweight-v10-ui4`及
`20261102-native-media-v6-ui4`，仅修正目录统计、登录文案和格式。
375/383份API源码、测试及迁移文件和模块清单逐字节与已验证候选一致，
复用对应API镜像与后端结果。
两者均从实际运行版完整冻结源码按白名单组装，未以整个脏工作区构建交付。
运行基线API为`663434e0a6b4caa260fb3936fcdf0925ff29e61e500d5b3ca6df8b212b1e90eb`，
Web为`329dcaf0f3a225abe6e921f99026578318a3770bf32d1cf4d9ae9f0935841575`。
本节候选尚未同步Compose，不能据此认为当前页面或原生连接已更新。

| 范围 | 已实现及已核对边界 |
| --- | --- |
| 产品范围 | 功能目录增加独立`product_scope`和排除原因；保留旧ID、授权和历史证据。默认目录及验收分母只计纳入项，超管只读查看排除项 |
| 后端及资料 | 注册、改密、换绑等排除项关闭HTTP、新任务及最终提交入口；历史查询和未知核对保留。必要登录认证不整体关闭。基础资料只读取身份、昵称、头像 |
| 解码领取 | 最多20账号边界及版本、每账号8项、公平顺序、5秒恢复和首次空结果补查；错误清边界、版本绕过、晚提交及过期租约回归通过。最终版本显式使用`e.id`范围条件 |
| 辣椒补拉 | 每连接缓存已提交会话游标、新会话按需读取，头部最多4批或1秒；50条/100ms及提示、取消、断线、缺口、倒退检查保留 |
| 接收器和监控 | 每秒授权协调、15秒租约每5秒续期、5秒快照及状态变化写入、每秒最多一次分位数；历史积压60秒独立采样，失败保留旧值、120秒失效，总P95合并原始样本 |
| 群媒体 | 仅纳入语音、视频；Chili独立103/104、字符串身份、固定上传契约及041尝试记录，不套用WSL nonce/AAD。上传和发送分别计预算，未知结果不自动重传或重发 |

第一阶段保持部署版协议解析算法、`CodecVersion=6`、消息及密封格式，
无新增生产迁移、服务、worker或连接；取源、写回合批继续关闭。
第二阶段只增加前向041，不包含工作区中未交付的039。
文件发送及个人扩展设置不属于本次产品范围；收到这些类型仍按原解析器处理。
辣椒头像上传仍未实现，没有可提交控件；旺商聊头像沿用已有上传边界。

| 检查 | 最终候选本轮实测 |
| --- | --- |
| 第一阶段Go | Linux全`./...` race/sodium通过；完整gateway PostgreSQL 23.948秒、完整HTTP PostgreSQL 87.647秒通过 |
| 第二阶段Go | Linux全`./...` race/sodium通过；完整gateway PostgreSQL 23.923秒、完整HTTP PostgreSQL 91.977秒通过 |
| 媒体持久控制 | 22种合成场景覆盖语音/视频、HTTP入口、重复、重启、RLS、取消、撤权、角色、epoch、绑定、连接所有者、窗口、预算、上传及发送未知结果 |
| 产品前端 | 收尾两份前端分别清理自动导入声明，类型检查、全产品Prettier和2329模块构建通过，分别10.90秒、10.88秒；主JS仍约1.997MB |
| 浏览器 | 收尾第二阶段11组、第一阶段6组通过，320/390/647/1440。排除子项的旧验收不再计入纳入项，目录只读、分母不变、旧设置隐藏、未知不重发、刷新恢复、陈旧积压不显示为0、图表非空通过，真实原生请求0 |
| 容器smoke | UI4两阶段分别再次串行使用一次性`wsl_test`空库通过；非root/只读、静态资源、迁移、排除项拒绝和默认关闭通过；041仅在第二阶段出现，租户不可直接读取尝试记录 |
| 架构事实 | 工作区update/check、所有权/仓库守卫、51项架构工具及3项渲染缓存测试、7图验证通过；两份UI4实际冻结重新提取160节点/368边、167节点/375边，边均有源码证据，图布局未变 |
| 模型咨询 | 2026-11-02 11:27:27 Asia/Shanghai新预检仍为`blocked / compatible_baseline_missing`，DeepSeek命名空间不变，模型请求、预留及上报Token均0，未初始化或发送源码 |
| 外部CI及真实原生 | 未运行远端CI；未发短信、注册、原生登录、真实媒体上传、发送或群管理修改；`native_ready`和`ai_ready`仍false |

正式性能使用私有`native-lightweight-v10-controlled-performance-20261102`
中的固定二进制：
基线`3e38c5654f573feca8fa8958da4b60e0119fc26c795e1e70c91cbfa009cd8e71`，
候选`e664ac34e9a8cbf1a5a4c00582a98aae5c64dcf22dc1a7082193ac03b36106ef`。
计时边界、资源、预热、四worker/16队列/四连接一致，解析器不变。
22项矩阵已全部完成五组对照并通过，共240万条正式计时样本。
四项格式补充对照和30分钟固定到达率测试也已完成并通过，
合成性能及本地检查通过，发布与真实媒体验收门槛仍未完成。
私有`matrix-assessment.json`保留全部场景判定，
没有剔除单轮波动或改用候选自对照。

下表为五组各自中位数，吞吐单位条/秒，P95单位ms；
延迟列来自对应基线容量70%的同速率输入，不能和饱和吞吐混为一次测量。
每行吞吐、持久化前及含写回P95分别达到20%/15%/15%的改善门槛，
各消息类别服务及到达排队P95未出现超过5%的稳定退化。
这是相同资源下的合成解码链路结论，不代表实际群业务容量或纯协议解析加速。

| 平台 / 场景 | 饱和吞吐：基线→候选 | 持久化前P95：基线→候选 | 含写回服务P95：基线→候选 |
| --- | --- | --- | --- |
| 旺商聊 / 单账号 | 2174.42→4202.17 | 0.282→0.206 | 0.9200→0.6526 |
| 旺商聊 / 100群 | 1152.95→3698.43 | 0.300→0.142 | 1.0065→0.5721 |
| 旺商聊 / 300群 | 633.20→3604.81 | 0.310→0.154 | 0.9819→0.5818 |
| 旺商聊 / 300群、50%热点 | 512.22→3588.30 | 0.3111→0.165 | 0.9719→0.5587 |
| 旺商聊 / 10万条已完成历史 | 192.45→3963.57 | 0.384→0.2821 | 1.1502→0.9087 |
| 辣椒聊 / 单账号 | 2277.95→4232.76 | 0.260→0.187 | 0.7975→0.6060 |
| 辣椒聊 / 100群 | 1185.21→3837.07 | 0.2691→0.131 | 0.8273→0.5032 |
| 辣椒聊 / 300群 | 627.41→3564.29 | 0.326→0.146 | 1.0729→0.5463 |
| 辣椒聊 / 300群、50%热点 | 518.48→3756.59 | 0.288→0.154 | 0.8706→0.5365 |
| 辣椒聊 / 10万条已完成历史 | 234.28→4097.56 | 0.3291→0.246 | 0.9812→0.8016 |
| 双平台 / 共300群、50%热点 | 307.43→3588.03 | 0.329→0.205 | 0.9391→0.7060 |

首轮计时发现测试表重建后沿用前场景统计；补`ANALYZE`后又确认空投影表
无法建立相同的有效分布。两份未完成矩阵主动停止并保留，未计入验收。
最终夹具在预热前及计时前以测试库所有者更新统计，保留两版本相同的1000条
已完成预热历史，且排除在正式样本外；另加大历史的事件键与预热区间不重叠。
这次只改变两版本共有的测试夹具，候选生产Go摘要仍为
`e45f6cd6b5605b8b8921ecfee2f6119c2a7275560b02b077a30f1626d1097f38`。

格式补充每平台每轮1万条、交替五组，另计40万正式样本。
旺商聊吞吐改善83.14%，持久化前/含写回P95改善26.81%/24.53%；
辣椒聊分别为91.83%、25.42%/20.46%，各类别服务及排队无超过5%的退化。
旺商聊每轮8750 decoded、1250 unsupported；辣椒聊2500 decoded、
7500 unsupported，两版一致且failed为0。辣椒仍只保留原解析器支持范围，
不能据此声称引用、媒体等投影已完整支持。

最终带SQL跟踪的诊断使用相同新统计口径，与正式无跟踪计时分开。
每万条两版均有1250次领取、10000次取源和10000次写回，另有一次结果核对，
共21251次池获取；本次没有通过合批减少请求数量。
旺商聊基线/候选累计池等待1586.260/2293.491ms，
辣椒聊1555.084/2135.428ms；这些是饱和诊断中的累计值，
不替代正式固定输入率下的服务P95。

| 10万条已完成历史、无积压 | 三次空领取执行耗时ms | 共享块命中 |
| --- | --- | --- |
| 旺商聊基线 | 41.687 / 40.292 / 40.816 | 每次7806 |
| 旺商聊候选 | 71.583 / 70.707 / 0.086 | 305866 / 305866 / 7 |
| 辣椒聊基线 | 34.423 / 34.570 / 34.177 | 每次4469 |
| 辣椒聊候选 | 64.072 / 63.156 / 0.114 | 302810 / 302810 / 7 |

全量及首次空结果恢复扫描仍存在，且这次单次成本高于基线。
运行器完全空闲时每五秒触发恢复，不能把连续诊断第三次的边界快查询
描述成真实空闲P95或所有领取的耗时。旧统计口径的首次诊断保留在私有目录，
不再作为最终性能改善证据。四份最终诊断的十二次EXPLAIN均未出现JIT。

持续负载阶段测试库由该负载独占；期间完成主机侧前端构建、
合成浏览器和源码静态检查，没有并发数据库集成或容器构建。
产品后端、数据库容器和资源限制保持不变。普通移动视口截图确认
固定导航在顶部，原全页长截图取景单独保留，没有据此修改产品布局。

最终持续负载使用10账号、共300群及50%热点，1800秒、215条/秒，
基于混合场景基线容量70%向下取整。387000条输入全部decoded，
停止输入后0.395892秒排空，生产者最大调度延迟22.408ms。
Go队列最大12/16，堆采样最大3808864字节（约3.63MiB），
RSS最大29597696字节（约28.23MiB），协程最多13。
共60个资源快照，30秒采样中的未完成差值最大151，后续追平，
最终无processing/unsupported/failed遗留；此差值不是全时刻积压上限。
资源守卫无中止/OOM，VM可用内存最低5711816KiB。
这些结果只证明本资源和合成固定输入下的有界性，不替代全天生产长稳。
完整日志、`soak-summary.json`及逐账号阶段聚合保留在私有性能目录。

之前V3矩阵保留为失败证据：22场景中21项通过，混合300群50%热点中
旺商聊长文本类别的服务P95中位数退化9.00%，超过5%门槛；
总体吞吐或总体P95改善不能抵消此项。V8约6分钟负载因领取代码再次修改
而主动停止，结果保留，不计30分钟稳定性通过。最终V10已重新完成上文的
完整对照及30分钟固定到达率负载，旧轮次不计入其通过结果。

首轮HTTP媒体夹具漏填部署/codec版本、容器检查误将辣椒签名子项当作
旺商聊必有项，以及媒体浏览器夹具启动作用域错误，均保留失败记录并修正后
通过。资料及媒体UI回归未放宽授权，容器检查仍覆盖两平台共同排除项和
辣椒独有排除项。一次gateway首轮因前一合成负载留下测试账号触发容量上限，
仅清理`wsl_test`后重新全套通过，未修改生产库或容量限制。

候选API镜像分别为`d720c3be13016b3fd3f66ee27088564e093c3e4233d039ae2ab058f360a2ed04`、
`2752701a357ffda1bc0b8425515f2ecdd24a6985cb2baa226af9a49384989d61`；
收尾Web分别为`5e4d227432f9d921ddea6c541376ed913eaa7ff8430ff7a8571f24bc591c192f`、
`4e938a3dbfe56d01ec481a4ab498a5187593048cb6b4c5ee7a030d56659d9234`。
最终只读核对运行API/Web仍为本节开头的基线摘要，三服务healthy；
API 1CPU/512MiB、Web 0.25CPU/128MiB、PostgreSQL 1CPU/1GiB保持。
生产仍为39项迁移，039及041均未应用；上一运行版回滚镜像仍存在，
未执行Compose同步或重启。性能核心门槛已达，但CodeBoarding可信基线缺失仍阻断发布，
按`AGENTS.md:146`，新基线初始化及白名单源码披露另需所有者专项授权。
群媒体真实验收与WSL历史nonce安全核验尚未完成，生产发送端口继续关闭。

## CodeBoarding恢复DeepSeek配置（2026-11-02）

按所有者“上游正常后改回”的要求，恢复架构专用`deepseek-v4.1-flash`模型和
此前私有备份中的原架构密钥；供应商地址不变。密钥通过既有配置入口写入仓库外
0600文件，GPT模型配置及密钥另有私有回滚备份。业务AI网关、助手、热群机器人、
Compose配置和运行服务未修改，没有原生操作或业务模型调用。

- 原架构密钥可读取模型目录，目标模型存在；未复用业务密钥。
- 三次有界合成生成检查均使用原架构密钥，不包含源码或运行数据。
  首次JSON样本不符合预期对象，不能记为通过；随后固定函数调用及原工具同参数
  JSON预检分别通过，返回同名模型。三次供应商上报总Token分别19、324、19，
  合计362；短请求通过不代表上游持续稳定或正式架构分析通过。
- 50项架构工具测试、3项渲染缓存测试、7图渲染、架构边界及仓库守卫通过。
  工具链变化导致清单过期，已重新生成`manifest.json`，图布局保持不变。
- 当前DeepSeek设置命名空间`aadc91a1297891f92ee25fa8f5d466fc997f961ea012106f02fe9429b9d8fea1`
  没有匹配活动基线或可信检查点；正式预检为
  `blocked / compatible_baseline_missing`，模型请求及预留Token均0。
  旧GPT及其他命名空间保留，不复制或重标其分析产物。
- 按`AGENTS.md`的所有者批准要求，尚未执行全量初始化或发送源码。
  本轮完成本地模型/密钥恢复，正式增量分析仍需先建立独立兼容基线；
  没有远端CI执行、提交、推送或产品发布。

## 辣椒聊独立原生接入（2026-11-02）

按所有者要求优先补辣椒聊原生功能，使用固定桌面1.0.3资源核对本平台契约，
没有把旺商聊枚举、数字ID、角色或“本人接受邀请”语义直接复制到辣椒聊。
已接通持久控制、最终栅栏和界面的范围见`docs/lajiaoliao-native.md`：
成员/全群禁言及解除、踢人、公告、授予/撤销管理员、群内昵称、邀请/退出、
好友审批，以及本人昵称、搜索、好友、群邀请、通话策略；入群申请只读。
引用114、提及106和合并记录107增加消息页按需有界读取，不修改历史投影状态、
恢复水位或CodecVersion。媒体当前仅识别类型，未据此声称资源预览可用。

| 检查 | 本轮实测 |
| --- | --- |
| 实际冻结Go | `97ade83ba3c8292c4a942c053253a1a81695b69125e278f10d28eff810b3bf28`；Linux全产品`cmd/... internal/...` race/sodium、vet通过 |
| 隔离PostgreSQL | 全HTTP race/sodium 92.055秒、全gateway 18.162秒通过，串行使用且只重建`wsl_test` |
| 原生契约 | 独立路由/载荷、字符串ID、角色20/60/100、Chat/IM票据分离、proto3省略零值、资料ex保留、取消/最终栅栏及未知不重发通过 |
| 持久权限 | native_debug在商业模块关闭时仅执行显式动作；账号/群/平台/epoch、取消、预算、原操作恢复、跨平台拒绝及历史隔离通过 |
| 消息读取 | 严格JSON、UTF-8、引用/提及/嵌套顺序、深度3/总32项、源缓冲清零后内容完整、资源地址不外泄、精确会话匹配及旧状态保持通过 |
| 功能表 | 平台独立定义、子项及生成目录同步检查通过；支持/接入/受控验收/生产验收分别显示，不继承其他平台结果 |
| 前端 | 产品类型检查与2329模块构建通过，12.51秒；JS `index-CVN392KJ.js`、CSS `index-Bpjk6FfY.css`，保留构建体积告警 |
| 浏览器 | 辣椒原生、Tools、角色、设置、社交、目录、消息七组通过，320/390/647/1440；辣椒独立策略、15字昵称、一次提交、未知恢复、平台切换隔离及HTML转义通过，真实原生请求0 |
| 架构事实 | update/check、所有权/Docker及仓库/迁移守卫通过；50项Python工具、3项渲染缓存回归和七图验证通过，仅更新受影响视图 |
| 模型咨询 | 兼容可信增量通过，247份白名单源码/9组件，1请求、92557预留Token、24716上报Token、usage_unknown0；持久检查点已保存，无全量回退 |
| 候选镜像smoke | 最终API `d86e1af0dc7c…`、Web `66105df0e49e…`在一次性空库通过；38迁移、非root/只读、静态资源、双平台Tools、租户隔离及默认关闭门槛通过 |

咨询源码摘要`b899bdbdc0928c908e92f7e68ea5bc26d3d7997300b51007edaa4d83e5c23bd0`，
配置摘要`d1c0fd3df8fc2a7b2ce5f2a9d6e46fdb6db1157f0200b02a01613e1109f73e9a`。
咨询针对当前工作区白名单源码；实际交付另从正在运行的上一版完整冻结源码
制作窄补丁，并逐项核对。未达标解码优化、039及暂缓安全实现未加入此镜像。
本轮不把当前工作区整张图描述成实际较窄镜像的完整源码证明。

首轮HTTP回归发现操作历史漏验空账号而返回500，已补入口校验；旧测试仍把
辣椒Tools目录一律判403，已更新为新入口可用、跨平台账号仍拒绝。最终全套
数据库回归重新通过。浏览器首轮夹具缺用户名/速率字段，随后漏切隐私页签、
点击了Element Plus只读输入而非选择器外层，修正夹具后七组通过；未放宽产品
权限。第一次Mermaid启动超时，空闲后重跑通过。系统Python缺yaml的守卫调用
改用既有架构venv后通过。失败记录不计为通过。

只读核对11条既有密封源记录，群/私聊引用、群提及及两项合并记录可由新读取器
解析，错误为零；不是本站发送、撤回或真实群管理动作验收。没有触发短信、
注册、原生登录、禁言、踢人、邀请或其他真实修改。发送/撤回持久链路、媒体
资源、头像/改密/换绑及未核实的平台邀请语义仍待接通，不能称“全部功能完成”。
实名认证及二级安全码继续暂缓，`native_ready`/`ai_ready`保持false，远端CI未运行。
本轮交付记录位于仓库外`~/.cache/artwslbot/deployments/20261102-lajiaoliao-native/`；
以下补记实际Compose交付与目录收尾结果。

01:23 Asia/Shanghai原Compose仅重建API/Web，原资源限制、环境摘要、凭据、
网络和挂载保持，PostgreSQL/heatbot/Simon未重建。38项迁移未变，未决提交0，
10条隔离编号及密文摘要保留，公开JS/CSS与冻结构建逐项匹配，上一版镜像
保留为`rollback-20261102-pre-lajiaoliao-native`。自动浏览器连接报告
`browser.user is unavailable`，不能完成部署后真实页面复核；没有据此声称
线上页面或真实群动作通过，合成浏览器和镜像smoke结果仍独立有效。

收尾发现quote_reply/mention_member旧说明与新增独立读取项冲突，修正为
“读取已接入，发送待接入”，同步生成目录；对应两项发送的pending状态未改。
第二份窄冻结仅变更`internal/features/platform.go`与生成目录，Go摘要
`87850de02f5dc1dc6c566da222f4ea0425bfa8c7632d61480f868f4b6af3ac9d`；
全产品race/sodium、vet、HTTP PostgreSQL 127.423秒和gateway 18.244秒重新通过。
前端输入和构建资源逐项相同，沿用同一七组合成回归结果；未额外改前端。
架构check/守卫、仓库/迁移守卫、差异格式和七图校验通过，图布局未变。
最终兼容增量通过，source
`1f21986cc641030a31a87fc3864a275546ac086a45ccee91fe59ab3ddfd4a3eb`，
1请求、92947预留Token、23235上报Token、usage_unknown0，检查点已保存；
与上面的首轮咨询分别计费，没有把任一预检算作正式分析。
新API与相同Web再次通过一次性空库容器smoke。

01:36:26仅替换API，最终API镜像
`7a58708cdac344e6e0aebf5a8b98adab560f3b7450b3b38a3d2830d7ce072645`，
Web仍`66105df0e49edc60cc892c013fdb9e76019e5175a92a7324c65ab5cb6222c9ef`。
Web/PostgreSQL/heatbot/Simon容器ID与配置未变，原API 1CPU/512MiB、
Web 0.25CPU/128MiB、非root/只读均保持；服务正常，未决0、迁移38、隔离10，
静态资源摘要一致。紧邻API回滚镜像保留于
`rollback-20261102-pre-lajiaoliao-catalog`，首轮交付及更早回滚记录未删除。
最终私有证据位于`~/.cache/artwslbot/deployments/20261102-lajiaoliao-catalog-final/`。
两个readiness仍false，无真实原生修改、历史重投影或额外协议激活。

## 架构分析临时模型切换（2026-11-01）

按所有者本次指示，将架构咨询模型临时改为`gpt-6.1-sol`，替换仓库外
0600架构专用凭据；原凭据另存于私有交付目录供恢复。业务AI网关、群管助手、
热群机器人及Compose凭据未变。模型列表与JSON连通预检通过，返回同名模型，
供应商上报4231输入、12输出、4243总Token；预检不代替正式分析。

切换后的首个新命名空间在语言工具准备阶段停止，模型请求0；保留现场。
第二次初始化在模型请求前遭本地403，模型请求及未知用量均0。
根因为客户端库在GPT-6工具调用时自动选择Responses，而受限relay只允许
Chat Completions；不能归因为供应商拒绝新密钥。
适配器已显式设置`use_responses_api=False`，保持单一路由、原鉴权、SDK零重试
及原预算。增加真实SDK带工具调用穿过本地relay的合成回归，以及Responses
被拒绝且不触达供应商的反向回归；47项架构工具测试通过。

第一份接口修订使用独立命名空间`d36d23e70826…`；旧分析缓存与可信检查点保留。
仅复用同一私有安装的语言工具二进制，1150项逐项摘要及权限比对一致，
未复制或重标分析结果、pickle、状态或基线。架构事实check、所有权/Docker、
仓库守卫、3项渲染缓存测试、七图校验通过。系统Python缺yaml的首轮守卫调用
失败，改用既有锁定venv后通过，未更改依赖。

该轮静态分析完成，但正式语义分析失败，实际3请求、246159预留Token、
reported0、usage_unknown3。供应商未返回可核对用量，记录保留。
额外三个短合成兼容请求分别验证双长度参数、工具调用、严格JSON均可用，
总Token分别为4247、4302、4270；排除“两个长度参数必然导致拒绝”的猜测。
私有日志显示前三次失败各约45秒，另两次被本地预算拒绝；无法由原日志
区分供应商HTTP失败与本地超时，不将relay502直接归因于供应商。

进一步核对发现上游scope/planner中间件另有五次尝试，SDK零重试不能关闭它。
适配器已将两处尝试次数固定为1；供应商单请求时限由45秒调到120秒，
整轮600秒、16请求、300000预留Token上限不变。新增固定超时/传输/HTTP状态
分类计数，不保存原响应正文；失败已提交请求仍保留未知用量。
新增回归后共50项工具测试通过，架构事实/所有权/仓库/渲染检查再次通过。
最终设置使用独立命名空间`d1c0fd3df8fc…`，仅复用17407项校验一致的完整
语言工具安装，不复制分析状态或pickle。

23:06:15 Asia/Shanghai，最终初始化通过：237份白名单源码，
source`5d4235bf533fac559bfd48bb3e2a4806ecca6fedb2ff348221640a17f7bdd378`，
1请求、82053预留Token、24803供应商上报Token、usage_unknown0、
provider_failures为空，9个组件；可信基线及持久检查点已保存。
兼容预检通过后，23:07:52同源增量通过，0新增请求/Token，9组件，
检查点保存通过。此增量是初始化后的同源复用验证，不称为新的付费语义分析。
没有全量回退、豁免、重标或迁移旧基线；历史3次未知用量并未因成功而清除。

23:10按现有Compose仅更新API/Web，使用下节已验证的独立撤回读取候选，
API`37903387fe49124f41a8f44b5cda5df8eee43efb5d62f578b259d121b235a7e0`、
Web`8353a26682be0b348339af8b483c657b0f1de9ece7c0d6da39b5eafd7b81a27f`。
交付前再次比对实际冻结Go/前端摘要、原配置、镜像及未决记录；五服务健康，
38项迁移未变、未决0、10条隔离编号及密文摘要保留，公开静态资源逐项匹配。
postgres/heatbot/simon未重建，API/Web资源、环境、挂载、网络及运行方式不变。
保留`wsl-{api,web}:rollback-20261101-pre-recall-readonly`上一版镜像。
未混入未达标解码优化、039或暂缓安全实现。

真实浏览器刷新后，旺商聊“撤回通知读取”已显示
“已核对支持 / 部分接入 / 尚无通过记录”，详情弹窗可查看，截图私有保存。
这是实际目录展示验证，不等于测试群撤回通知弹窗或完整生产验收。
没有执行真实发送、撤回、短信、注册、原生登录、安全码设置或认证；
native_ready/ai_ready仍false。远端CI未执行，源码事实、咨询分析、
独立候选回归、Compose交付与真实原生验收分开记录。

## 撤回读取独立候选与旧页面核查（2026-11-01）

22:12 Asia/Shanghai复核：真实浏览器功能表中旺商聊“撤回通知读取”仍为
“待核验 / 待接入 / 尚无通过记录”。Compose实际仍运行API5bc70429e4d4…、
Web37cac7060727…，与19:13交付一致；不是本轮新目录已上线后又回退。
正确候选状态为“已核对支持 / 部分接入 / 尚无通过记录”，不把留存通知
解析证据升级为测试群页面或完整生产验收。

为避免夹带已暂缓功能，从这份运行版的完整冻结源码单独制作
`20261101-recall-readonly`候选：包含两平台纯撤回读取器、账号/会话只读HTTP、
消息弹窗、独立平台功能表及暂缓标记；不包含二级安全码实现、安全状态接口、
039迁移、未达标解码候选或其他助手样式改动。现有38项迁移原样保留。
原工作区候选与暂缓纯协议代码未删除，不能把本次较窄镜像等同于整个工作区。

| 检查 | 本次实测 |
| --- | --- |
| 实际冻结Go输入 | `93616c9855d531f82c56fbd94b392c97c1846e21781efbbc83fb5ac652245053`；Linux全`./...` race/sodium与vet通过 |
| 隔离PostgreSQL | 全HTTP race/sodium 94.448秒通过，只重建`wsl_test`；双平台撤回会话隔离、保留原消息通过 |
| 功能表 | 工作区与实际冻结目录同步检查通过；新增页面回归核对两平台支持、接入、无通过记录三列，不继承验收 |
| 前端 | 461份独立输入，类型检查及2326模块构建通过；私有副本按上一版哈希排除无关助手样式，未改原文件 |
| 浏览器 | 设置、消息、目录三组合成回归通过，320/390/647/1440；两次显式撤回读取、暂缓控制隐藏、跨账号/平台隔离，无真实原生请求 |
| 架构与仓库 | 工作区事实check、所有权/Docker及仓库守卫通过；45项工具、3项缓存、七图校验通过；没有改变图布局 |
| 实际候选镜像 | API `37903387fe49124f41a8f44b5cda5df8eee43efb5d62f578b259d121b235a7e0`；Web `8353a26682be0b348339af8b483c657b0f1de9ece7c0d6da39b5eafd7b81a27f` |
| 容器smoke | 独立空库通过；实际API返回两平台撤回`confirmed/partial`、两个安全子项`deferred`；确认039不存在，非root/只读、资源及隔离门槛通过 |
| 交付预检 | 五服务健康、38迁移、未决提交0；10条隔离编号及密文摘要保持，原配置、限制、挂载、网络及镜像不变 |

首轮测试命令重复传入Go入口、系统Python缺yaml，分别更正为显式Go入口和既有
架构venv后完成。smoke私有工具最初引用缺失的冻结init脚本，修正为现有受审
init脚本；新增暂缓断言起初误查父目录，修正为独立子项目录后通过。
这些失败未被计作通过；未因其更改应用、迁移或权限。

CodeBoarding仅重新进行零调用兼容预检：source仍为
`5d4235bf533fac559bfd48bb3e2a4806ecca6fedb2ff348221640a17f7bdd378`，
baseline_compatible。对应变更此前的attempt-y0elhc9q实际增量失败，保留日志中
本地relay502及ScopeSemanticsError；未将预检算作咨询成功，未重新付费调用或
进行全量回退。本份独立候选没有成功的对应咨询分析，也没有获得本次豁免。
按AGENTS失败检查阻断交付，候选未部署，实际页面仍旧状态；远程CI未运行。
没有真实发送、撤回、注册短信、登录、认证或安全码设置；readiness仍为false。
私有检查、来源清单和交付预检均在仓库外`20261101-recall-readonly`目录。

## 暂缓安全扩展与辣椒聊独立目录（2026-11-01）

所有者决定暂不接入二级安全码设置和实名认证；辣椒聊底座按本平台证据独立核实。
两个子项保留原ID并改为`deferred`，不计入当前独立验收项，关闭前端选择、
安全状态读取和安全码设置HTTP入口。纯协议/持久候选代码及迁移记录保留，
没有执行真实设置或认证。历史候选记录不覆盖本节范围决定。

辣椒聊能力组、子项改为本平台显式登记，移除遍历旺商聊目录自动补齐的逻辑；
保留原ID供核实，不继承权限或验收。补充本平台字符串ID、roleLevel20/60/100、
独立禁言/解禁路由、purpose=3及login_or_signup字段依据；删除辣椒聊注册子项中
套用旺商聊业务1001的说明。管理员修改契约仍待核实，不凭读取角色推断写权限。
详见`platform-native-scope.md`。

生成目录及Web增加已确认/待核实能力组数量：辣椒聊17组已核对支持、
23组待本平台核实，40个登记ID保留；两平台当前各54个独立验收项、
2个暂缓项，完整验收均0。旺商聊历史受控证据仍12组/14个指定可用子项，
辣椒聊没有继承其通过记录。运行`go run ./tools/feature_catalog -write`并通过同步测试。

### 本次检查

- 受影响features/httpapi/control/lajiaoliao和目录工具race/sodium通过，
  vet通过；产品`cmd/internal/tools`范围完整race/sodium通过。
- HTTP暂缓回归通过：nil数据库/接收器条件下状态读取与安全码提交返回409，
  未触达原生或存储且释放并发槽。底层保留代码回归不代表产品入口开放。
- 隔离`wsl_test`目标回归7.190秒通过，涵盖原设置/安全流、保留的安全码持久代码、
  双平台撤回读取及原文保留；串行执行，无生产库重建。
- 产品前端类型及Vite构建2327模块通过；主JS2000.18kB触发既有单包大小阈值警告。
  三组全拦截浏览器回归通过：320/390/647/1440，8个原有合成安全步骤，
  暂缓安全状态读取0，目录暂缓项排除、跨平台证据隔离及两次显式撤回读取通过。
  未发起真实短信、注册、原生登录、发送、撤回或群修改。
- 架构update/check、所有权/Docker及仓库守卫、45项工具测试、
  3项渲染缓存测试和七图验证通过。没有改变架构所有权或图布局。

首轮浏览器运行缺少默认playwright模块，改用本机已有模块后完成，未改依赖。
直接`go test -race -tags sodium ./...`因忽略目录`var`中的既有临时诊断文件
混用main/nativeauth包而失败；这些文件未修改或删除。产品包范围回归独立通过，
不将其描述为该全目录命令通过。

本轮source`5d4235bf533fac559bfd48bb3e2a4806ecca6fedb2ff348221640a17f7bdd378`
的CodeBoarding预检baseline_compatible；21:46:16 Asia/Shanghai实际增量仍失败，
reason=analysis_failed，3请求、reported0、usage_unknown3，私有attempt-y0elhc9q
保留。无全量回退、重标或删除基线，未将预检当分析成功。
前轮候选源码/镜像未更新为本节修订，不作为
当前源码的容器或交付证据。尚未重建发布候选、执行本修订容器smoke、
同步Compose或运行远程CI，两个readiness不变。

## 撤回通知读取状态复核（2026-11-01）

用户确认两个原生App撤回均有群内通知，质疑功能表“待核验”。
代码中的两平台`read_recalls`已为`support=confirmed / integration=partial`；
当前Compose仍为前一轮API5bc70429e4d4…/Web37cac7060727…，
未包含该目录及弹窗候选，不能把旧页面文案解释为原生不支持。

2026-11-01 20:57:16 Asia/Shanghai运行手动只读探针，
对既有密封记录使用本轮新读取器；只读事务、最近七天、
每平台最多1000条/4MiB，不新增消息、撤回、认证、原生连接或数据库写入。

| 平台 | 实际扫描 | 实际读取结果 | 真实验收边界 |
| --- | --- | --- | --- |
| 旺商聊 | 1000个notice源包，253033密封字节 | 85个已知封装内群撤回通知包解析成功，解析错误0；两个账号目录含测试群映射，但本页没有匹配该群的通知 | 已验证留存群通知解析；不能称测试群匹配或已部署弹窗通过，也不能据本页断言测试群从未撤回 |
| 辣椒聊 | 11个群/私聊源包，17686密封字节 | 一条2101/200群撤回通知解析成功，并匹配测试群、一个账号，解析错误0 | 已验证真实测试群输入读取兼容；尚未验证部署后HTTP及弹窗展示 |

探针只输出计数、窗口和已知路由标签，不输出身份、原消息标识、正文或密钥；
留存包数量不等于独立撤回动作数，不用其他群包代替测试群验收。
功能目录说明补充上述边界；没有新增完整验收、生产自动撤回或readiness。
CodeBoarding本轮增量失败仍阻止候选交付，用户本次询问不视为豁免授权。
既有候选镜像未纳入本节文案更新，不能使用旧清单宣称当前源码已交付。
本次相关features/nativewire/lajiaoliao的race/sodium回归通过，手动探针vet通过；
架构update仅更新manifest、check及所有权/Docker/仓库守卫通过，
45项架构工具测试、3项渲染缓存测试和七图验证通过。未改前端、数据库或协议读取
运行逻辑，本次未重复前轮前端构建、隔离数据库及容器smoke，未执行远程CI、
新付费模型分析或部署；不将前轮检查宣称为当前修订的完整发布验收。

## 实名状态、二级安全码与撤回通知候选（2026-11-01）

本轮要求补接实名认证、二级安全码和撤回通知读取，并核对CodeBoarding故障归属。
此节是候选实现/合成检查记录，尚未部署或通过真实账号验收。

### 接入范围

- 旺商聊二级安全码：固定`get_safe_code`/`set_safe_code`路由；仅首次设置，
  六位输入及二次确认，提交前重新核对尚未设置，复用持久意图、当前网站会话、
  原生账号/epoch/版本/部署和最终授权栅栏。原始安全码不进入记录、审计或浏览器
  存储；输入仅留在请求内存，存储的是带密钥指纹，完成流清空密文。未知不重试。
  已设置的修改/找回仍由本人在官方App处理，不伪造重置入口。
- 旺商聊实名认证：固定finance真实认证服务只读审核状态；不使用仅保存资料的
  `settings/legal`冒充认证。只输出审核枚举及认证/过期标记，不返回姓名、
  证件号、认证时间或票据；状态读取按选项独立调度。证件上传、申请及原生人脸
  SDK尚未接入，页面明确由本人在官方App完成，不宣称完整实名认证已完成。
- 双平台撤回通知：独立显式只读弹窗。旺商聊核对7/14及7/15布局，辣椒聊读取
  2101/MsgFrom200和严格detail JSON；按当前账号、群目标或私聊双方过滤。
  每次最多1000原始记录、4MiB扫描预算，最多128条一包且不拆包丢通知；最近七天
  和手动事件ID分页。保留原消息、旧unsupported状态、源包、ACK和恢复水位。
  不接入撤回动作或自动业务。详见`docs/native-recalls.md`。
- 新增兼容迁移039，仅扩充安全操作种类白名单；已应用迁移不改、运行库未迁移。
  功能目录同步为partial，未增加任何新真实通过项，readiness维持false。

### 已完成检查

| 范围 | 实际结果 |
| --- | --- |
| 工作树Go | 受影响nativeauth/nativewire/lajiaoliao/control/gateway/httpapi race/sodium通过；目录新状态对应回归通过 |
| 冻结Linux Go | 排除未达标解码候选后的全包`test -race -tags sodium ./...`、独立vet通过；测试镜像不含.env |
| 隔离PostgreSQL | 工作树HTTP97.765秒、随后gateway24.725秒；冻结Linux完整HTTP88.951秒、随后gateway18.180秒均通过，仅wsl_test、串行重建 |
| 协议与隐私 | 空/未知/非法认证状态拒绝，身份字段过滤；安全码字段白名单、现有码拒绝、权限/epoch失效、同键不重复且换码拒绝；读取失败无自动请求重试 |
| 撤回隔离 | 两平台合成解析、畸形与重复JSON、源缓冲区清零后字符串有效；HTTP账号/群过滤、跨平台404和原消息保留通过 |
| 页面 | 工作树产品类型/Vite2327模块、独立冻结产品类型/Vite2326模块通过；冻结与工作树三组浏览器回归320/390/647/1440均通过，9个合成安全步骤、4次安全状态读取、2次显式撤回读取；无真实原生请求 |
| 架构 | update/check、所有权/Docker及仓库守卫、45项工具测试、3项渲染缓存、七图验证通过；仅后端/前端依赖视图及源码证据更新 |
| 空库容器 | 候选API4639dcb9f156…及最终独立Web1b4768902bf8…smoke通过，新039白名单/隔离、非root只读运行和静态资源可读通过；未使用生产数据 |

首轮目录测试沿用“撤回读取pending”的旧断言而失败，已按新独立读取范围修正，
仍要求partial且无生产通过；首轮浏览器在取消后未重新输入安全码而超时，
新增取消清空断言并通过。独立vet发现新夹具跨包未具名字段，改为具名字段后通过。
一次架构check与前端文案编辑交错产生manifest过期，update后复核。
独立冻结前端首个Web44c0df3704f3…镜像的公开图片因私有构建输入0600/
root归属而403；保留失败，COPY指定nginx归属后重新构建并完成整套smoke。
最终安全码成功提示不再误导为必须重新登录，改密/换绑提示保留。
这些失败没有被解释为真实账号或原生服务故障。

### 来源、交付与未完成项

私有候选目录`~/.cache/artwslbot/deployments/20261101-security-recalls/`保存
2549项冻结源码摘要、461项前端摘要、检查结果和候选镜像。Go输入摘要
`e6435bdfc4f3e5b4d20a62d44df58d07bc50a430a97dc9aa2d5704202c9a2bcf`。
基于紧邻运行5bc70429e4d4…的源码，只纳入本轮文件和已真实核验的管理员目录
元数据；未纳入性能未达标解码器、公共消息结构/密封格式及Codec版本变化。
工作树另有助手入口样式改动；仅在私有前端副本中恢复与运行清单完全匹配的
旧文件摘要，未改动工作树，不随本轮交付。独立冻结前端最终构建/浏览器及
容器smoke均已通过，最终工作树分析源摘要为97ac78980b3b…。

CodeBoarding预检为baseline_compatible；本轮实际增量在source
`5c280d52c5c206eb384da7f8a91781176829cd7c8387b1748823b819155019e9`
失败，3次请求、reported0、usage_unknown3；本地转发层报告502，后续为
ScopeSemanticsError。随后独立供应商预检能列出模型，最小生成请求仍传输失败，
证据指向模型服务链路，不能据此定位供应商内部原因。旧可信检查点保留，
没有全量回退、改供应商配置、重标基线或业务AI请求。之后仅修正安全码成功
提示和合成夹具；最终源码仍没有成功的本轮咨询分析，不拿预检代替分析通过。

按AGENTS“failed checks block release”门槛，本轮尚未同步Compose，原运行
API5bc70429e4d4…/Web37cac7060727…保留，未执行039或重启原生连接。
最终只读核对五服务运行（API/Web/PG/heatbot健康）、38迁移、未决0、
十条历史隔离仍保留，`/healthz`正常且readiness为false。
没有自动设置真实安全码、提交实名资料、发送短信、注册、原生登录、消息发送
或撤回。真实认证状态、安全码首次设置、双平台通知展示仍须单独验收；
实名认证完整申请/核身明确未完成，两个readiness不提升。远程CI未执行。

## 管理员授撤与独立原生调试验收（2026-11-01）

时间口径Asia/Shanghai。所有者要求接入设置/撤销管理员，并将完整原生调试与
生产业务使用范围分开。旺商聊固定桌面2.8.3路由、参数和UI调用已逐项核对，
不以Android定义或辣椒聊同名功能推断契约。新增两个固定单目标控制动作，
共用密封持久意图、幂等键、预算、当前账号/版本/绑定、原生完整名单和最终栅栏。
只有当前原生群主可授撤；授予只对member、撤销只对admin；本人/群主/
未知或不在群目标、额外字段和批量目标均拒绝。未知仅核对原操作，不重放。

### 测试与生产分离

前向兼容迁移038新增`test_windows.admission_mode`，旧窗口及省略该字段的
旧客户端保持`package`门槛。超管新建`native_debug`窗口可测试已实现的
持久群管/入退群Tools，无需开通生产订阅或启用群管/AI；active租户群绑定、
authenticated/testing/action开关、原生角色、账号epoch、明确动作/目标、
最多300秒和10次预算仍校验，不提供任意路由或助手执行权限。
可登记动作种类上限32不改变次数预算。未接通发送、撤回等不能借此获准，
本人接受邀请仍需独立窗口。此次未修改已应用迁移或公共消息/密封/Codec版本。

### 真实R3

- 当前唯一旺商聊测试群1459282；执行号林夕owner、自有大蛤member，双方
  完整名单和身份映射一致。未操作第三位成员、未发短信/注册/原生登录或消息。
- 临时打开执行号动作准入后epoch从20到21；立即读取曾返回
  `native_text_connection_changed`，未发起群修改。新代际连接就绪后重新只读核验，
  没有绕过epoch或重放写入。正式窗口
  `68b54bc3-765c-4a11-ba29-e1bbba644a7f`，native_debug、300秒、预算2。
- 19:11:58持久授予`d6af1d24-8904-43fc-95cd-1744e193bd47`，
  succeeded/native_moderation_acknowledged；19:11:59双侧完整名单均admin。
- 19:12:27持久撤销`1c512a0b-aeeb-47f8-bd4d-1c3d872494e7`，
  succeeded/native_moderation_acknowledged；双侧完整名单均member，执行号仍owner。
  从首次提交至最终回读30.486秒。两次写入均有独立读回，不以回执代替生效。
- 19:12:59窗口已取消、used2/budget2；执行号动作准入恢复false、testing维持原true，
  epoch/version正常递进到22，另一号仍9。生产群管/AI均false、群配置及version26
  原值不变，没有临时充值、延长套餐或启用商业模块。

### 检查与交付

| 检查 | 实际结果 |
| --- | --- |
| Go | 工作树受影响产品、冻结全包及Linux冻结全包race/sodium通过；工作树/冻结vet通过 |
| 隔离PostgreSQL | 仅wsl_test；新增调试角色用例2.146秒、工作树完整HTTP122.393秒；冻结Linux HTTP86.995秒和随后gateway18.188秒通过 |
| 合成边界 | 无订阅/生产开关关闭仍可显式调试；非群主、目标角色不符、授权取消/过期、绑定撤销、账号epoch变化、退出测试模式拒绝；幂等不重复、未知只读恢复、历史和租户隔离通过 |
| 前端 | 产品类型、Vite2322模块与Docker锁定依赖构建通过；授撤/Tools/入退群/目录四组浏览器回归，320/390/647/1440视口通过，零真实原生请求 |
| 浏览器修复 | 初始模块路径/隐藏input断言/异步读取等待不合适，修正测试；另发现pending目录有受控入口，补confirmed及接入状态判断后重建并通过，失败记录保留 |
| 架构 | update/check/所有权Docker守卫/仓库及目录同步通过；45项Python、3项缓存、七图验证通过；只更新graph/manifest源码证据 |
| 可信增量 | 首轮source3568006cdbe0…通过，3请求reported24482/usage_unknown2；最终UI门槛sourceb7188c9e91e5…增量校验通过、0新增请求、持久检查点保存；没有全量回退 |
| 容器 | API5bc70429e4d4…/Web37cac7060727…在空库smoke通过；新迁移、认证/资源/隔离/非root只读和默认关闭门槛正常；Web沿用原运行底座，测试镜像无.env |

已按原release Compose先执行兼容迁移038，再仅重建API/Web，五服务健康。
38迁移、未决0、十条历史隔离密文摘要不变；资源、环境、挂载、网络均保持，
PostgreSQL/heatbot/Simon未重建。紧邻0819c107c175…/20c39d84065f…镜像保留为
`wsl-{api,web}:rollback-20261101-pre-native-roles`。
本轮受限冻结以紧邻已部署源码为基线，仅加入本次文件，不混入未达标解码候选，
不改变运行依赖。真实及交付证据保存在
`~/.cache/artwslbot/deployments/20261101-native-roles/`。

真实通过后目录增加grant_admin/revoke_admin两个绿色指定场景；WSL受控12组、
绿色14独立项，完整验收仍0。辣椒聊仍无对应控制契约/平台化受控入口，本轮不
自动接管其App连接或制造群绑定；其授撤条目维持pending，未伪称双平台通过。
R4撤权后旧证明真实拒绝、故障/未知/重启及生产业务未完成，两个readiness维持false。
目录元数据同步候选API`b55d83ef357e…`的Linux全包race/sodium/vet、
仅wsl_test完整HTTP91.426秒和随后gateway18.203秒、目录测试与空库smoke通过；
前端460项摘要与已交付Web完全相同，无需重建Web。
本次候选架构事实/守卫及七图验证通过，但source
`52c9b6f83089abb9a08317c6bdb4830c76397e35102cdf5c62fb677437aa9de7`
的咨询增量失败：3请求、reported0/usage_unknown3，stderr证实上游HTTP502，
其后ScopeSemanticsError。兼容可信基线保留，没有全量回退或改名掩盖。
此时结果元数据候选未交付，运行功能镜像仍为已验收5bc70429e4d4…；
不能将工作树12/14计数当作运行接口已更新。其后一次有界供应商预检可列出
模型，但生成JSON契约仍因传输失败退出；未继续付费重试，usage未确认。
19:31最终核对五服务健康、38迁移、未决0、十条隔离摘要保持，两个WSL接收器
connected/队列0；窗口已取消、预算2/2，群version26及生产开关保持。
当前目录两项授撤均confirmed/partial，运行受控仍10组/绿色12项，
候选12组/14项未发布。专用网站测试会话已退出，不影响所有者浏览器会话。
当前咨询检查失败仍阻止二次交付，需同源增量恢复通过或所有者本轮明确豁免；
此前另一修复的豁免未沿用。上述失败与已上线授撤功能及真实R3结果分别记录。

## 双平台样本核对与旺商聊设置验收（2026-11-01）

时间口径为Asia/Shanghai。所有者要求补充辣椒聊App已使用的引用、@、撤回和
聊天记录，并继续WSL受控测试、将可用场景标绿。本站使用网站超管会话；
没有新原生登录、设备接管、发短信、注册、群消息发送或业务模型调用。

### 本轮真实证据

- 17:28–17:29辣椒聊测试群收到114引用一条、106结构化@两条、
  2101撤回通知一条、107合并聊天记录一条；仅内存解封并输出固定键/类型/数量。
  全部五条旧投影为unsupported，不能宣称已经展示或本站执行通过。
  合并记录内有两条，依次引用/@，引用指向图片；详见
  [样本结构与边界](lajiaoliao-message-evidence.md)。
- 旺商聊17:42:19/20双账号当前完整名单均三位成员、mapping_complete=true，
  两侧成员/角色集合完全一致；自有执行号林夕owner、另一号大蛤member。
  本次读取不操作第三位成员，不据原生名单推定租户授权。
- R2通话开关已先公告300秒/共两次尝试，原值false。持久操作
  `a1baf2e1-3819-41d4-8332-da03241e4cd4`改为true并独立回读，
  `c4bc2925-b24b-431d-86e3-75e2e994a8e9`恢复false并再次回读；
  均succeeded/native_acknowledged，两次预算用尽，18.827秒内完成。
  仅使用既有单项设置控制API/当前摘要、epoch、版本和最终栅栏，
  不创建群管测试窗口或借用群操作权限；不证明真实呼叫或其他字段通过。
- 原API仍98bdf818运行版，接收和原生发送内核未修改。新增结果只涉及本人通话
  设置、成员读取和角色读取；没有重测历史公告/退群来刷新日期。

### 目录与展示

辣椒聊四项App能力改为已核对原生支持，本站接入仍pending。新增两平台
read_quote/read_mentions/read_recalls，将读取、发送及撤回控制分开。
两平台现各40能力组、56独立验收项；WSL历史受控10组、指定场景可用12项，
辣椒聊受控0、两平台完整验收0。通话结果只附privacy_calls子项，
不外溢至昵称、头像、安全或其他隐私项。

图表和行标记绿色“指定场景可用”，完整验收单列；同一独立项只计一次，
未知元数据不增计，不继承父组/另一平台通过，不放宽NativeBlockers或readiness。
本轮验证：

| 检查 | 当前结果 |
| --- | --- |
| Go | 工作树产品范围、冻结完整源码race/sodium与vet通过；Linux冻结源码全包race/sodium/count=1通过 |
| 隔离PostgreSQL | 仅wsl_test；Linux HTTP87.372秒通过，随后gateway18.140秒通过；两套串行，race/sodium |
| 前端 | 产品类型检查/Vite、标准Dockerfile锁定依赖构建通过；当前2322模块 |
| 浏览器 | 首次合成预期把10项错写为11，断言失败；修正预期后19独立项/9受控组/10绿色场景通过，零真实写入，320/390/647/1440无越界；这不是实际目录12项的计数 |
| 架构事实 | update仅graph/manifest；check、所有权/Docker及仓库守卫、45项Python/3项渲染缓存测试及七图摘要验证通过 |
| 架构增量 | 兼容可信基线，231源文件、9组件、source29776a24bfb12efd57889d1f36a18cdbb1c9f701a18daf5160fd221b0f59adea；3请求、reported21915、usage_unknown2，最终校验通过并保存检查点；保守reserved277107不是实付账单 |
| 容器 | 实际候选API/Web空库smoke通过，非root只读、认证/SSE/资源、平台隔离与默认关闭门槛正常；测试镜像无.env检查通过，集成环境仅运行时只读挂载 |
| 恢复检查 | 其他账号字段与原快照一致，两个WSL接收器connected/队列0，当前设置未决0 |

新候选继续从98bdf818运行源码及上一轮目录候选制作受限冻结补丁，
核对460项前端源码，未修改解析/接收代码、迁移或运行依赖。
本轮咨询模型成功不追溯改写上一轮失败；未知usage仍单列，不宣称无费用。
最终已按原Compose交付API `0819c107c175…`、Web `20c39d84065f…`。
首次交付复核发现标准Web Dockerfile的浮动基础镜像改变了Nginx/NJS内置版本
环境摘要；服务健康但不满足“保留原运行环境”，没有忽略差异。随后以原661a5d2e
运行镜像为底座仅复制已构建前端资源，最终Web重新smoke通过后仅重建Web。
API未再次重启，最终资源、环境摘要、只读/用户/挂载/网络与交付前一致，
PostgreSQL/heatbot/Simon容器不变。首次复核失败和修正均保留在私有证据。

最终五服务健康、37迁移、未决0、十条历史隔离密文摘要保持；
API现读WSL40组/56项、受控10组/绿色12项，辣椒聊40组/56项、受控及绿色0。
两个WSL接收器已connected/队列0，通话设置仍恢复原false。
`native_ready / ai_ready`仍false；两平台完整验收仍0，辣椒聊五条复杂格式
仍待解析接入，本次没有重投影历史或宣称格式已经展示。
紧邻旧API/Web保留`wsl-api:rollback-20261101-pre-native-evidence`、
`wsl-web:rollback-20261101-pre-native-evidence`。私有检查/来源/交付/设置证据：
`~/.cache/artwslbot/deployments/20261101-native-evidence/`及上轮目录证据中的
`controlled-settings.json`。本地CI规则通过，未运行远端CI、提交或推送。

## 原生与业务目录补齐、独立验收与防漂移（2026-11-01 17:20，Asia/Shanghai）

本轮修复功能表的漏项、合并过粗和描述滞后，不执行原生测试或新增管理员功能。
保留旧能力组ID/业务依赖/授权与控制语义，新增可选只读`native_details`。
补列语音/文件接收、私信图片、设置/撤销管理员及辣椒聊注册；辣椒聊对应未适配能力
显式标记review/pending，不继承旺商聊支持或受控结果。设置、三类媒体发送、
邀请/接受、本人/他人名片、全群禁言/解除、好友同意/拒绝及注册短信/提交独立列项。
引用与@说明更新至持久意图/最终栅栏事实，生产入口仍关闭。

当前两平台各37个能力组、53个独立验收项（子项替换组，不重复计数）；
WSL历史受控组仍7、辣椒聊0、两平台完整验收通过均0。
本人名片结果只附本人子项。页面标明原生底座/业务模块，并可从业务依赖查看组与
独立子项；无配置字段的业务模块有只读详情，无假开关/保存。
`tools/feature_catalog`从服务端同源定义生成`docs/feature-catalog.md`，
包含全部目录/子项/业务/验收元数据摘要；Go测试与CI的`-check`拒绝过期快照。
同步维护规则已写入AGENTS。当前是已登记产品范围，不能宣称官方原生功能全集。

| 检查 | 本轮真实结果 |
| --- | --- |
| 工作树Go | 产品cmd/internal/目录工具race/sodium及vet通过；首轮`./...`误纳忽略目录var的异构临时包而setup失败，未删除研究文件；限定产品范围后通过 |
| 冻结Go | 从运行98bdf818源码及2500文件摘要制作受限补丁，完整`./...` race/sodium/count=1和vet通过；无var，未混入未达标解码器 |
| Linux Go | 独立测试镜像`79508c5787b5…`，完整`./...` race/sodium/count=1通过；测试镜像构建排除.env，无网络调用 |
| 中间构建清理 | 首次中间测试镜像COPY误含冻结目录的私有.env；已删除该未引用镜像13bb1b26…及本轮对应COPY/源上下文缓存，最终镜像无.env的只读检查通过；未向仓库/远端发布，未改业务配置 |
| 隔离PostgreSQL | 冻结源码、GOMAXPROCS=4、race/sodium/count=1；HTTP106.679秒，随后gateway21.067秒通过，串行且仅wsl_test |
| 产品前端 | vue-tsc/Vite及三文件Prettier通过；标准Dockerfile干净依赖/产品构建通过，2322模块 |
| 浏览器 | 扩展native_acceptance_ui_smoke，全API拦截，只读；17项合成独立列表无重复，子项结果/平台隔离、管理员待接入无执行、业务无假保存、父子依赖导航通过；320/390/647/1440无越界，真实写入/原生动作0 |
| 目录同步 | 工作树与冻结容器`go run ./tools/feature_catalog -check`通过，父子/平台/依赖/无身份数据测试通过 |
| 架构事实 | update仅graph/manifest，check、所有权/Docker守卫、仓库/迁移守卫通过；45项Python、3项渲染缓存、七图缓存/摘要验证通过 |
| 守卫环境 | 系统python缺yaml的首轮守卫未执行，使用已配置架构venv后通过；未更改工具门槛 |
| 咨询性模型 | 可信基线兼容，231源文件，source73b608c325882d4637a7c5d6a62fa685e75902ff152b8d3285457558801e528b；本轮incremental失败，分析端502/ScopeSemanticsError，3请求、reported0/unknown3，reserved277110为保守额度非账单；旧基线保留，未全量回退或初始化 |
| 供应商独立预检 | 同一固定模型仍可列出，但生成未满足JSON契约，预检失败；未修改分析配置或重跑付费增量掩盖失败 |
| 容器smoke | 最终API f83df3b1…与Web a6efe072…，空库角色/迁移/bootstrap、非root只读、产品资源、网站认证、SSE及默认关闭门槛通过；临时容器/网络清理 |
| 交付预检 | 原五服务健康，37迁移，未决原生/设置/上传/注册提交0，隔离记录10；.env/资源/release模式不变；因本轮模型咨询失败，未部署、未重启原服务 |

候选镜像保留`wsl-api:native-catalog-20261101` /
`sha256:f83df3b1fbb4f6cb126bd505829df191d0371bace5bc964fddf84eee18f355ef`、
`wsl-web:native-catalog-20261101` /
`sha256:a6efe0728b466ecde4dee7d23092678ca7878089b8a09ace56b05da104af91fe`。
前端460项源码摘要相对已交付661a5d2e基线仅FeatureCenter、NativeCapabilities、
featureCatalog三文件变化；后端运行基线的无关文件逐项不变，不新增迁移/依赖。
私有完整来源、候选、浏览器、预检证据：
`~/.cache/artwslbot/deployments/20261101-native-catalog/`。

原API仍98bdf818、Web仍661a5d2e且healthy。本轮没有短信、注册、原生登录、
真实群动作、业务模型调用、readiness提升、远端CI运行、提交或推送；
架构模型咨询请求单独记账，未称为业务生成验收。需要供应商恢复并通过同源增量，
或所有者明确豁免本轮咨询失败后，才可按现有Compose交付已验证候选。

## 首轮受控验收与临时套餐精确恢复（2026-11-01 06:36，Asia/Shanghai）

所有者明确要求开始受控验收。先公告唯一测试群、执行者林夕、目标大蛤，
一分钟禁言后解禁、单窗最长300秒、总共两次尝试。当前完整原生成员现读确认
双方分别owner/member，既有租户群绑定与执行账号匹配；没有用SQL制造权限。
只读预检发现原订阅disabled且已过期，旧接口不能还原该过去到期值，
因此先修复恢复机制，通过全部必需检查并交付，再创建实际窗口。

### 独立补丁、回归与交付

`internal/httpapi/control.go`只改变订阅时间校验：停用的非商业套餐允许非零过去值；
启用套餐仍要求未来时间。商业计划或已有商业账期仍409、租户仍403，
版本冲突、配额、绑定epoch及安全审计保持。新增合成回归验证
过去值创建/启用临时资格/精确恢复、零值与启用过期拒绝、陈旧版本拒绝、
商业计划及已有付费账期拒绝，审计三次且不创建商业周期。

| 检查 | 本轮实际结果 |
| --- | --- |
| 工作树Go | 产品cmd/internal全范围race/sodium及vet通过；未变包可用缓存，不把默认跳过的PG测试计入 |
| 冻结补丁Go | 同范围race/sodium及vet通过；独立冻结源码只包含本轮两文件差异 |
| 首轮PG | HTTP380.778秒失败：TestPostgresAssistantIsolationBudgetAndApproval等待turn超时；gateway23.191秒通过；失败保留 |
| 最终PG | 构建任务结束后独立串行重跑、GOMAXPROCS=4，race/sodium/count=1；HTTP176.953秒、gateway21.394秒全部通过，含新增恢复回归，仅重建wsl_test |
| 首轮超时限制 | 重跑未复现；未证明具体环境根因，没有扩大测试期限或修改助手代码来掩盖失败 |
| 产品构建 | vue-tsc及Vite通过，2322模块、36.39秒；前端未变，JS仍index-Dfj6goHN.js/CSS index-d5qnK-Pi.css |
| 架构事实 | update仅manifest、check、所有权/Docker及仓库/迁移守卫通过，45项Python/3项缓存测试及七图验证通过 |
| 模型咨询 | 可信基线增量通过，230文件/9组件，1请求、24153reported tokens、未知0；source811880252afc6bf47a5ba55f03b8fc286ef8c9d73e9425d4839c9097da4eb912，持久检查点保存，无全量回退 |
| 容器smoke | 最终98bdf818候选API与现有661a5d2e Web，空库角色/迁移/bootstrap/非root只读/认证/产品资源/SSE/默认关闭门槛通过；无生产数据 |
| 交付与复查 | 原五服务健康，原Compose仅更新API，37迁移、未决0、十条历史隔离密文摘要保持，资源/环境/挂载/网络/release模式不变 |

实际补丁自b5b705d04391…运行源码冻结，仅control.go和subscription_test.go差异，
2500项文件摘要核对；没有加入未达标解码优化、其他候选或新迁移。
当前API镜像wsl-api:controlled-restoration-20261101 /
sha256:98bdf818af643a1dfe5b06c6d0ac364be3dbed3dbc1bd098442d8ca4bfccd049；
紧邻b5b705d0保留wsl-api:rollback-20261101-pre-controlled-restoration。
Web/PostgreSQL/heatbot/Simon未重启。本轮咨询分析针对工作树候选，
交付的受限文件集另有摘要证明，不把工作树图当作所有候选均已部署的证据。

### 实际原生结果

06:34:07正式API建立唯一窗口8b6a49e9-d6b4-4d35-b313-7eb3caabeca8，
仅account林夕、target大蛤、mute_member/unmute_member，300秒、budget2。
临时启用既有零AI额度非商业测试套餐、林夕动作开关及该绑定管理服务；
完整配置未改，AI未启用，原生动作之前现读确认大蛤未被禁言且禁言名单已知。

| 时间 | 动作与独立读回 |
| --- | --- |
| 06:34:32 | 唯一一次一分钟禁言；eff48076-40ec-4afe-92b7-556dad76a2e3 succeeded/native_moderation_acknowledged |
| 06:34:56 | read_group现读完整禁言名单，mutes_known=true、大蛤在名单中，非单凭回执判断成功 |
| 06:34:58 | 唯一一次解禁；e20666c6-0ab8-421d-8303-fe50b7af80e4 succeeded/native_moderation_acknowledged |
| 06:35:18 | 现读mutes_known=true、大蛤不在禁言名单中；群政策和owner/member角色未变；used2/budget2 |
| 06:35:18 | 取消窗口；恢复群管理/AI/完整配置、林夕原动作和测试开关、原套餐/disabled/原过去到期值 |
| 06:35:59 | 再读确认精确恢复、无冲突；两个WSL接收器connected、队列0、当前连接新增隔离0、revision24/9；大蛤仍member；网站会话随后退出 |

本轮恰好两次原生修改，没有失败重发、补窗或追加次数。
原到期值2026-10-31T18:25:05.805Z精确恢复；配置摘要一致，
版本/epoch按正常栅栏递增，没有强行回退版本。
只证明R1基础动作和状态读回；未发送消息，未验禁言时发言拒绝、解禁后收发、
精确自动到期或断线/未知恢复，不把N21/N22整体升为生产通过。

辣椒聊在线账号的完整原生成员现读确认测试群72567012内，
受控测试号为owner、受控测试号2为member。群主号仍blocked/lajiaoliao_kicked，
另一号connected；产品绑定0，WSL-only受控控制门槛仍未补齐。
没有重登录/设备接管、空开测试开关或绕过持久控制直接调用协议原语。
该平台群修改仍待前置；发送nonce、持久撤回、授撤管理员及其他计划用例未验收。

native_ready/ai_ready保持false，production accepted保持0。没有真实SMS、注册、
原生业务登录、消息发送或付费业务模型调用；咨询分析用量与业务调用分开记录。
未运行远端CI、提交或推送。私有最小元数据、检查及交付证据在
~/.cache/artwslbot/deployments/20261101-controlled-restoration/；
未将原始上游正文、名单、密码、手机号、验证码、票据或凭据写入本节或测试证据。

## 双平台完整验收计划及只读范围预检（2026-11-01 05:49，Asia/Shanghai）

按所有者要求制定`docs/acceptance-plan.md`，覆盖旺商聊/辣椒聊各自测试群与双账号，
管理员授予/撤销、消息撤回、32/12项原生能力、15/6项业务目录及AI/商用/性能/恢复。
每项列前置、用例、证据、次数上限和收尾；当前授撤管理控制入口、辣椒聊持久
控制与群授权等列为先修项，不用目录/开关改值伪造通过。

本轮仅通过已认证网站接口读取账号、接收器、缓存目录、群绑定、能力和测试窗口，
网站会话登录后退出；没有真实原生动作、窗口创建、发码、注册、原生登录或模型调用。
05:49:22采样WSL林夕/大蛤connected、队列0，目录对应唯一测试群1459282、
owner/member；该群活跃绑定仍在，群管理/AI和账号原生动作开关关闭。
辣椒聊已存在两个认证号，一个blocked/lajiaoliao_kicked、一个connected；
缓存测试群72567012角色unknown，随后`GET /groups`再核对绑定总数0。
未自动重登录或设备接管，没有将缓存目录当成实时角色或SaaS授权。
当前两readiness=false，production accepted两平台均0，WSL历史受控7项保留。

只修改计划/检查点/本记录文档，不执行部署、迁移、重启或新一轮产品测试；
本节不是本计划的真实验收完成报告。文档空白检查通过；源码目录逐项核对
WSL32项、辣椒聊12项均映射到32行原生矩阵，15项WSL业务及其中6项辣椒聊业务
均有规划；10项相对文档链接存在，检查点和本报告均已链接计划。

## 转交截图与桌面动态提示复核（2026-11-01，Asia/Shanghai）

先读取转交的忽略目录核查记录`registration-hint-mapping-20261101.md`并核对截图，
再核对当前运行构建与代码。截图是未登录“测试受控1”的注册短信失败，
不是同名已在线账号“大蛤”的接收故障；阶段为注册短信，业务码1001，
未展示diagnostic或diagnostic_version，不能据截图证明响应字段缺失。按转交记录，订单323在04:16:04过期，
不作为有效测试号码。本轮没有查询接码平台、续订或发起新的短信/注册/原生登录。

重新验证官方Register、共享handleReq及proto三个SHA与核查记录一致。
重新扫描固定桌面127文件/12790126字节并解码Unicode转义；没有号段禁止注册固定
文案，唯一“不支持注册”来自音频插件。handleReq将业务拒绝e.msg直接显示给用户，
与本地传输t.code=1001“解析错误”分属不同层；动态号段提示可能存在，
但不能由本次通用业务1001推断。静态证据详见official-client-research.md E44/E45。

当前API b5b705d0、Web 661a5d2e均healthy，诊断支持版本2，安全审计没有这次历史
失败。只读浏览器确认未登录面板及加载index-Dfj6goHN.js；线上JS的SHA与已验证
交付资产一致，含新提示/诊断版本/未识别类别，不含截图中的旧短信拒绝文案。
不能由此倒推截图当时资源版本、原始msg或本次diagnostic类别。
旧事务已过期，历史原因无法补录；此次发码/收码/注册均未验收成功，根因仍未确定。

本轮只新增测试`diagnostic_chain_sodium_test.go`，不改生产码、请求参数、短语词表、
业务权限、迁移或Compose。六组HTTP200业务1001合成响应经真实Client、Manager及
安全审计回调贯通，验证空/缺失、未识别文本、明确号段、明确号码、非文本分支，
并验证signup_sms/1001/诊断版本2、原文与敏感字段过滤及轮询/关闭不重发。

| 检查 | 本轮实际结果 |
| --- | --- |
| 认证回归 | nativeauth全包race/sodium、count=1通过4.298秒，含六组新增贯通案例；该包vet通过 |
| 隔离PG | 既有失败审计关闭后留存/账号隔离/过滤/单次请求回归count=1、race/sodium通过4.705秒，仅wsl_test |
| 合成浏览器 | 运行静态资源上的12类短信失败/18次模拟提交通过，包含明确号段与号码限制及缺分类旧结果；真实原生请求0 |
| 构建一致性 | 当前23项相关生产源码SHA与运行版冻结清单一致；当前线上JS SHA与已测Web交付资产一致 |
| 架构事实 | update仅更新manifest，check及所有权/Docker守卫通过，不改变视图布局 |
| 咨询分析 | 本轮只做兼容基线预检，ready；生产分析源230文件/hash d92a0ed66513bbf64504cc38c64a34127eca18cca695ed3db2b58e3ee8117e43未变，没有新模型分析调用，不冒充本轮增量成功 |

此次测试和文档不产生新运行镜像，未重启任何生产服务，未将未达标解码器候选上线。
不重复执行上一轮的发布构建/smoke，也不将上一轮结果列为本轮新执行。
04:45:40只读回查两个已有接收器connected/队列0/新增隔离0，readiness仍false。
没有对过期订单重发或补造历史失败审计，未远端CI/提交/推送。

## 手动短信再次1001与诊断留存补齐（2026-11-01，Asia/Shanghai）

所有者报告再次手动申请注册短信仍返回1001。只读访问日志定位04:07:26的单次认证
动作HTTP202及04:07:27轮询HTTP200，前有04:05:21注册事务创建。202/200不等于
上游短信成功；失败业务码来自所有者报告。运行API确认仍为da4e6b66诊断版2，
不是旧18db镜像。读取原事务返回404；其十分钟期限已到，未保存原始msg或分类，
本次无法事后确认是空原因、未识别文本还是其他类别。没有读取或保存原始正文。

静态复核固定桌面Register-03d04bd2.js：注册短信仅ty/phone/validateStr，
phone三个字符串字段及掩码与当前实现一致；官方前端号码正则允许所报格式。
这不代表服务端准入通过，也不构成170号段受限证据。此次未更改发码参数或猜测根因。
开发代理本轮没有真实短信、注册或原生业务登录请求；网站鉴权用于只读核查。
**真实注册短信仍失败、根因未确定、验收未通过。**

补齐已证实的排查缺口：Go认证完成后通过控制层保存固定`native_auth_rejected`审计，
只含用途、阶段、白名单错误/数字码、原因分类和诊断版本，绑定本站账号/发起人；
不保存手机号、OTP、密码、人机票据、事务句柄、原生身份或任意上游原文。
已有审计权限保持，管理器锁外四秒限时写入，不阻塞轮询/取消，不自动重试，
账号删除后迟到记录不重建历史。保存失败为固定告警；进程退出等仍可能缺失，
不能伪称已恢复这次原始原因。无新迁移、前端或原生协议行为变更。

| 检查 | 本轮实际结果 |
| --- | --- |
| 工作树Go | cmd/internal全范围race/sodium、vet通过；原生认证单独count=1通过 |
| 新增隔离PG | 失败审计在关闭事务后仍可查询、账号隔离/敏感过滤/单次请求/未提交注册通过，5.477秒 |
| 工作树全量隔离PG | race/sodium串行，仅wsl_test；HTTP227.718秒、gateway26.484秒通过 |
| 冻结补丁Go | cmd/internal全范围race/sodium及vet通过 |
| 冻结全量隔离PG | race/sodium串行，仅wsl_test；HTTP138.308秒、gateway20.809秒通过 |
| 产品构建 | vue-tsc/Vite通过，2322模块、19.39秒；前端未改不交付新Web |
| 合成浏览器 | 12种短信诊断、18次模拟提交通过；重复回调/关闭/轮询无自动重试，真实原生请求0 |
| 架构事实 | update/check、所有权/Docker及仓库守卫通过，45工具测试/3渲染缓存测试/七视图渲染通过，仅更新证据及清单 |
| 咨询分析 | 可信兼容基线增量通过，230文件/9组件，source d92a0ed66513bbf64504cc38c64a34127eca18cca695ed3db2b58e3ee8117e43，3请求/67727 reported/未知0，持久检查点保存，无全量回退 |
| 容器smoke | b5b705d0候选API/现有661a5d2e Web空库验证通过，非root/只读/迁移/受限角色/静态资源/SSE及默认关闭门槛；无生产数据 |
| 交付预检 | 现有五服务健康，37迁移，原生/设置/上传/注册未决均0，十条历史隔离密文摘要保留 |

实际补丁冻结自正在运行da4e6b66的2495项源码，仅六项相关Go/测试文件差异，
合计2499项；没有混入未达标解码器。冻结脚本初次误排除vendor中的.env.test.dist，
完整性检查阻断，修正为仅排除真实.env后再次核对通过；基线源码本身没有漂移。
首次两条go test误用-p1/-p2被参数解析阻断，改为-p 1/-p 2后真实执行通过，
不把失败命令记为测试成功。

04:37既有Compose仅重建API，镜像
`wsl-api:auth-diagnostic-audit-20261101` /
`b5b705d0439157134be86a4c51b98b7257dac7fe4566c439403854c4c335d073`；
Web/PostgreSQL/heatbot/Simon未重启。紧邻da4e6b66保留为
`wsl-api:rollback-20261101-pre-auth-diagnostic-audit`，五服务健康，
API资源/配置/环境/网络/挂载及release模式不变，37迁移及十条隔离密文摘要保持，
未决原生/设置/上传/注册均0。04:37:17网站鉴权只读回查两个接收器revision24/9
均connected、队列0、新增隔离0，两readiness仍false；新审计中没有补造历史失败。
没有重发这次短信或提交注册/业务登录，没有混入解码候选。私有冻结与交付证据在
~/.cache/artwslbot/deployments/20261101-auth-diagnostic-audit/。
此次用户已手动请求不计作开发代理发码，也不将1001标记为成功；根因仍需此次
页面保留的安全分类等新证据，不能从日志或手机号前缀推断。未执行远端CI、提交或推送。

## 受控结果展示与注册短信诊断第二轮（2026-11-01，Asia/Shanghai）

本轮所有者仅授权诊断修复及既有Compose交付，不授权发真实短信、注册或原生登录。
侧边核查引用的18db1cfa为旧运行版；交付前实际API是0c25e843，已包含01:02交付的
第一轮原因分类。历史1001未保存原始msg，无法事后恢复或断言号段限制。

此次确实补上两个缺口：能力页把production integration accepted当成唯一验收计数，
隐藏了已审阅的七项历史受控结果；认证拒绝的msg或data不是预期类型时，
共用解析器会先丢掉有效业务码。新结果保留有效认证业务码，区分空/缺失/null、
非空未识别、非文本、超长原因，只输出固定安全枚举及诊断版本2。
512字节文本分类及最多3074字节JSON转义预算明确受限；原始正文、手机号、密码、
验证码和票据不返回、不审计、不持久化。成功响应和非认证路由保留原有严格校验；
不会把畸形群管回包判成已知拒绝而开放重试。

路由/短信用途限定映射、明确短语白名单及本地频控沿用第一轮已核验底座，
不把通用业务1001套成桌面传输解析码，也不据此推断号段。旧结果缺分类时明确提示
无法判断原因是否为空；新失败显示诊断版本，部署只读接口显示服务支持的版本。
没有为采集原因自动发码，**真实注册短信验收仍未完成**。

能力页新增独立的“受控动作已核验7”与“生产业务已验收0”，逐项只读展示范围、
日期、结果、剩余项和报告依据；支持筛选受控结果。七项仅覆盖历史报告中明确的
成员禁言、成员解禁、全群禁言/解除、公告、本人群昵称、邀请接受入群、本人退群；
不覆盖其他成员名片、踢人、自动规则、故障恢复或当前选中账号健康。
元数据无原生身份或操作票据，辣椒聊不继承，不能改变integration、
NativeBlockers、控制权限或两项readiness。

| 检查 | 本轮实际结果 |
| --- | --- |
| 工作树Go | 最终cmd/internal全范围race/sodium及vet通过，部分缓存；新增认证、目录及HTTP测试另count=1通过 |
| 工作树隔离PostgreSQL | HTTP148.194秒、gateway22.942秒，race/sodium、串行且仅wsl_test |
| 实际冻结补丁 | 全范围race/sodium及vet通过；最终隔离HTTP178.826秒、gateway21.126秒，串行且仅wsl_test |
| 早次冻结检查 | 非认证严格校验保护补入前HTTP160.001秒/gateway21.522秒通过；最终版本重新验证，不以早次结果替代 |
| 前端构建 | 最终vue-tsc/Vite通过，2322模块、14.50秒 |
| 合成浏览器 | 最终两组通过：12种短信失败、18次模拟提交；轮询/重复回调不自动重试，真实原生请求0；受控计数7/生产0、平台隔离、只读和320/390/647/1440布局通过，手机截图人工检查 |
| 架构事实 | update/check、所有权/Docker及仓库守卫、45项工具测试、3项渲染缓存测试、七视图渲染通过；仅相关前端依赖证据更新 |
| 咨询分析 | 可信兼容基线增量最终通过，228文件/9组件，source 4cca3f7b58102ca81c27e41b229afac4ab3a053722971f194c4f891eb0acfa0d，1请求/23068 reported/未知0，持久检查点保存，无全量回退 |
| 分析早次及失败 | 独立provider JSON预检曾失败，未记成功；正式增量先对早次源码通过1请求/25009 reported，产品补充严格保护后对最终源码重跑通过 |
| 容器smoke | 最终da4e6b66 API / 661a5d2e Web独立空库验证通过：迁移/bootstrap、受限角色、非root只读、静态资源、SSE/代理及默认关闭门槛；无生产数据 |
| 交付前 | 现有五服务健康，37迁移，原生/设置/上传未决及submitted注册均0，十条历史隔离密文摘要预存 |

首次新增浏览器脚本点击Element Plus输入框被占位文本拦截，修正为点击其实际选择器
容器后通过，没有修改产品权限或放宽断言。首次冻结检测到未纳入清单的web/.env
配置文件，按源清单同规则排除，未将其内容或摘要纳入前端交付清单；随后核对仅四项前端源码差异。
最终API只在0c25运行版冻结源码上加入十二项相关Go/测试文件差异，
2495文件核对，未混入未达标解码器或其他后端候选。无新增迁移或生产数据删除。
03:09（Asia/Shanghai）原Compose仅重建API/Web，镜像分别为
`wsl-api:auth-diagnostics-v2-20261101` /
`da4e6b66572ee50b5fb92142219e309fc1ca0b4a25f71d20d502eccea79f9e15`及
`wsl-web:auth-diagnostics-v2-20261101` /
`661a5d2eb6f126b01969a2d51eaa938fcf029df78b96fe17fa356e5ac0fc588d`。
十一项线上静态资源SHA与已测构建一致；环境、挂载、资源、网络和release模式保持，
PostgreSQL/heatbot/Simon未重启。紧邻上一版0c25/4fa0分别保存在
`rollback-20261101-pre-auth-diagnostics-v2` API/Web标签，不涉及数据恢复或新迁移。

03:09:47网站鉴权只读回查确认32项目录、七项受控动作、生产accepted=0，
当前服务auth_diagnostic_version=2；两个WSL接收器revision24/9均connected，
队列0、新增隔离0，未修改接收开关。十条历史隔离编号/密文摘要及37迁移保持。
03:10刷新所有者原有features页面，实际显示7/0；没有打开认证弹窗或提交验证。
实时截图`output/playwright/native-acceptance-live-20261101.png`，
短信诊断截图为合成数据，不作为真实发码证据。
`native_ready=false`、`ai_ready=false`，真实短信、注册和原生业务登录均0。
私有证据目录
~/.cache/artwslbot/deployments/20261101-auth-diagnostics-v2/。
未运行远端CI、提交或推送；合成回归、咨询分析和真实原生验收分别记录。

## 最新入群结果（2026-11-01，Asia/Shanghai）

02:05 大蛤接受林夕现有邀请，单次提交成功，双方完整成员名单均确认已在
测试群1459282、角色普通成员。02:10收尾复核仍在群，授权已取消，临时启停恢复；
完整实现、测试及数据恢复限制见本文末“本人接受入群邀请补齐”。
这只完成本项验收，不替代其余原生功能和长稳验收，两 readiness 仍 false。

## 受控验收收尾、注册诊断与接收恢复（2026-11-01，Asia/Shanghai）

本节续接下节32项完整盘点，不重复已取消窗口或已提交的群动作。**整体原生验收
仍未通过**；群禁言/解除、公告、本人的名片修改及恢复只保留下节明确的实测结论。
01:18再次通过正式原生只读接口确认“大蛤”身份一致、完整目录已知但不在测试群
1459282，不能对其他成员补做禁言、踢人或名片测试。没有当前自有待处理好友申请，
不操作第三方。发送仍受nonce历史验收限制，撤回缺持久控制，媒体和辣椒聊动作
仍待验收；没有绕过发布门槛。历史邀请尚未完成入群回读，需所有者在大蛤端接受。

### 注册短信拒绝的安全诊断修复

另按所有者委托修复注册短信1001的诊断：固定官方共享API定义1001为通用码，
并无“1001等于号段禁止”的定义。对认证路由及短信用途限定已核对业务码；
注册已注册、短信频繁、验证拒绝、版本/IP拒绝分别映射固定枚举。只有明确匹配的
号码/号段注册限制文案才显示相应限制。最多检查512字节，原文不返回、不审计、
不持久化；空文本与非空但未识别分别显示，不能由未知文本推断号段原因。
静态出处及桌面适用边界见official-client-research.md的E42/E43。

前端保留失败阶段和业务码，说明原因依据及不会自动重发。原生上游历史msg已隐藏，
不能事后恢复其内容来证明旧1001的原因。本轮未发真实短信、注册、原生业务登录，
也未探测手机号；实际发码仍由所有者手动操作，合成回归不算真实短信验收。

| 检查 | 本轮真实结果 |
| --- | --- |
| 候选Go | 生产cmd/internal全范围race/sodium及vet通过，部分缓存；新nativeauth测试另以count=1通过 |
| 候选隔离PostgreSQL | HTTP race/sodium173.453秒、gateway30.836秒，串行且仅wsl_test |
| 实际热修复源码 | 冻结源码./...全范围race/sodium及vet通过；隔离HTTP155.087秒、gateway21.491秒，串行通过 |
| 产品前端 | vue-tsc/Vite最终通过，2322模块、19.62秒 |
| 合成UI | 九种短信失败、15次模拟提交；重复回调/轮询不重试，原生动作0；320/390/647/1440布局检查通过，320截图已人工查看 |
| 架构事实 | update/check、所有权/Docker及仓库守卫、45项工具测试、3项渲染缓存测试通过；七图无布局变更 |
| 咨询性分析 | 可信预检兼容，incremental通过；最终227文件/9组件，源码b3c1cfcbdf29cf26bca6e7aed472e4be29c993584d6f069968766efc156937b6，模型请求0，持久检查点保存；此前本轮一次分析请求1/上报21521/未知0，无全量回退 |
| 容器smoke | 最终API/Web组合独立空库迁移、bootstrap、受限角色、非root只读、登录退出、SSE、代理边界和关闭门槛通过；不使用生产数据 |
| 交付后复核 | 五服务运行状态正常，线上11项资源与已测构建SHA一致；36迁移、未决原生/设置/上传及submitted注册0；10条历史隔离编号与密文摘要保持 |

UI首轮因相同1001提示先匹配上一条轮询结果而失败，改为等待本用例诊断文字后
最终全组通过；没有通过放宽产品校验消除失败。截图
output/playwright/native-signup-sms-diagnostic-320.png和1440.png均为合成数据。
未运行远端CI、提交或推送；本地回归、咨询分析和真实原生验收分别记录。

01:02原Compose仅替换API/Web，当前镜像分别为
`wsl-api:registration-diagnostics-20261101` /
`4c3d7884973279f93fe6fdffd2201b33af821e93ac391849b82d5aaa6ca0493b`及
`wsl-web:registration-diagnostics-20261101` /
`24ec812863431109db24098bf17c7e27e20ea3a1f95f4194f97a3facee805740`。
容器c70e491bcf5c…/a79a231a344f…均healthy、RestartCount0，
保留release模式、资源、环境、网络和挂载；PostgreSQL/heatbot/Simon未重启。
JS为index-Jze7Zjpu.js，CSS为index-ifghHpND.css。
回滚标签wsl-api:rollback-20261101-pre-registration-diagnostics和
wsl-web:rollback-20261101-pre-registration-diagnostics固定紧邻上一版18db1cfa…/f484282f…。

API从上一运行版本冻结源码开始，2492文件核对仅nativeauth/client.go、
manager.go、rejection.go及对应三个测试文件六项差异；没有部署未通过整体性能的
解码器候选。咨询模型分析描述当前候选，实际交付另有冻结源码摘要，不混为一谈。

### 交付后接收异常与一次恢复

01:03读到两个旺商聊接收器在im_token阶段blocked，错误均为
native_session_renewal_failed，未保存非零upstream_code。现有接收观测将多种
nativeauth错误收敛为此码，缺少当时的具体原因，**不能声称已经定位或根治此次
续票故障**，更不能把它等同于先前HTTP502或注册短信1001。

大蛤01:03:31由本轮脚本之外的操作停用revision8、启用revision9，01:03:51同步；
审计与时间均核实，不将这次外部恢复记成本轮操作。林夕离线密封会话检查通过，
部署版本2、设备/身份一致，未发起网络换票；随后正式原生本人资料读取成功。
这些证明当前会话可用于该只读请求，不能补足先前续票的响应证据。

按所有者既有接收恢复授权，林夕只成功提交一次带revision23栅栏的接收配置恢复，
revision24；未重复业务登录、短信、群动作或未知原生请求。首个管理请求误用PATCH，
接口不支持，审计确认revision仍23、无接收变更；核实PUT契约后才纠正提交，失败记录
保留。现有接收所有者随后按既有机制自动续票并连接，不能称本轮完全没有原生网络活动。
01:19:04首次同步，01:19:07消息/ACK181/181；01:20:20增至207/207，
心跳新鲜、队列0、新增隔离0。同期大蛤24/24、revision9、connected。
本轮不把计数因重建连接改变解释为丢失，也不将短时恢复当作全天稳定性。

两账号原测试开关保持，connection_enabled仍false，测试群管理/AI均false，
无活动窗口、无未决设置操作；没有重开临时业务套餐。下节订阅到期元数据未精确
恢复的限制仍存在，订阅保持disabled。native_ready/ai_ready均false，
能力目录没有升级accepted。

私有证据在~/.cache/artwslbot/deployments/20261101-registration-diagnostics/，
含provenance、delivery-preflight/delivery、acceptance-readback、两次管理请求记录
及receiver-resume-put-result。原账号/群内容不发送给架构模型。后续成员测试先等待
自有账号实际入群；发送、撤回、真实媒体及解析失败仍需独立补齐和验收。

<a id="完整受控原生验收与观测热修复2026-10-31至2026-11-01asiashanghai"></a>

## 完整受控原生验收与观测热修复（2026-10-31至2026-11-01，Asia/Shanghai）

所有者已开启受控测试并明确要求完整验收。本轮逐项核对32项旺商聊原生能力，
在唯一“测试群”1459282内执行有界实测，未把目录中的implemented/partial改成accepted。
**整体原生验收仍未通过**：发送/撤回的发布前置尚未补齐；成员动作缺少当前在群的
自有普通成员；接收投影仍有签名失败及未支持格式；辣椒聊没有本轮业务消息样本。
下表的“通过”仅指本轮列明的子场景，不是该能力全部格式、权限或故障场景均通过。
没有发起原生注册、短信、密码登录、改密、换绑、真实资金或AI业务调用。

### 真实范围与逐项结果

群绑定仍指向原隔离租户，网站owner由既有明确owner记录核对，未从原生成员推断。
执行者为已认证的“林夕”，原生群主；另一自有账号“大蛤”当前不在该群。
完整成员快照包含林夕和一位其他成员，后者不属于已证明自有目标，未操作。
大蛤既有邀请仍为待接受状态，不能把“邀请请求成功”当作已入群。

| 原生能力（覆盖目录全部32项） | 本轮实际结果及未验收范围 |
| --- | --- |
| account_signup / account_login | 仅确认现有两账号authenticated和本地身份映射；未重复注册/短信/业务登录，不新增该项验收结论 |
| session_restore | API替换后既有接收器恢复并同步；未故障注入换票、认证拒绝或重放未知请求，不证明历史502已根治 |
| account_settings | 两账号本人UID、昵称、搜索开关、好友/群邀请策略、通话开关读取成功；修改隐私、上传头像、密码/换绑等未测 |
| receive_message | 测试群留存58条投影全部decoded：文本44、图片14，含1条@；不是本轮主动发送58条。账号级仍有失败/未支持投影，完整格式和长稳未通过 |
| read_images | 测试群一项图片代理返回200/JPEG/1006字节；表情、其他格式及失效资源尚未完成实样覆盖 |
| read_forward | 本群无新代表样本；离线最近1000条探针有3条转发，但未验收跨条目UI与媒体完整性 |
| read_avatars | 两账号头像均200/JPEG/251318字节、同一摘要；测试群头像200/PNG/26399字节。只证明代理可读，不证明两账号使用不同原生头像 |
| read_contact_cards / read_video | 测试群缺少代表样本，本轮未完成真实展示/播放验收 |
| send_text / send_private_text | 群发送状态enabled=false、native_text_acceptance_pending；Tools报告nonce_history_acceptance_pending。未绕过端口注册和nonce历史门槛 |
| quote_reply / mention_member | 接收有1条@，发送引用/@及可信来源到持久意图的完整链路未接通验收，不降级文本 |
| recall_message | durable_control_pending；未提交撤回 |
| send_image / send_media | 上传/发送底座有合成覆盖，但媒体真实验收与发布端口未就绪，未上传或发送真实媒体 |
| list_groups / list_members / read_roles | 唯一测试群目录、完整成员及当前执行者owner证明成功；大蛤不在群。辣椒聊测试群72567012完整成员读取为2人；不等于所有群分页验收 |
| mute_member / unmute_member / kick_member | 本轮未执行，缺少已证明自有且当前在群的普通目标；既往禁言结果只作历史，不代替本轮 |
| mute_group | 23:28:13原生成功并回读muted=true，23:28:24解除成功并回读false；原未禁言状态已恢复 |
| publish_notice | 23:28:28成功发布一条明确测试公告；热修复后00:16:45读回唯一匹配正文，原生公告ID1400362，未重复发布 |
| set_member_card | 本人群名片“林夕”→“林夕·受控验收”→“林夕”，两次持久操作及独立回读一致；00:16:45再次核对原值；其他成员名片未测 |
| read_mutes | 新鲜角色证明包含当前连接的完整单独禁言映射核对；没有本轮新禁言目标来验收持续时长/目标变化 |
| read_invitations | 正式Tools接口成功，返回本群2条可见记录、cursor1793459416、complete=false；不宣称完整历史/奖励归属 |
| list_friends | 执行者153条好友投影可读，包含自有目标；不能据好友关系推断对方在群 |
| approve_friend | 两账号申请窗口可读，但无另一个自有账号发来的待处理申请，未审批第三方 |
| invite_group / leave_group | 当前大蛤已离群且有待接受旧邀请；未重复邀请或对不在群账号发退出。既往成功操作仅作历史，完整入群回读闭环仍缺 |

23:28群管窗口`55eb0f09-eafb-4c06-9918-31ac4de03fa1`为300秒、3次修改预算，
只提交全群禁言、解除、公告，used=3/3后取消；没有补充该轮修改额度。
对应操作为`e6b5d9a3-e615-4c21-8061-4d370e47894f`、
`d92d0056-e09f-415b-8a6b-b0e74684e57e`、
`98f85c9c-524a-4025-8ede-71fa68b1ff64`，均succeeded。
本人名片为独立设置事务
`5260ec3e-bad1-4f1c-8256-dcb9d6eeb796`和
`b97c61f0-1218-4aa9-b9aa-0c303ef0ac39`，各提交一次，均成功并回读。
00:16新建的回读窗口`0f81e023-861d-40fb-9ee6-6a00291216ce`仅调用read_group，
used=0，读后取消；未向原窗口续期、补次数或重放任何原生修改。
测试公告保留为验收痕迹，未调用删除。

### 接收与解码限制

23:35账号级解码存量分别为35229/2176条，成功34019/2060、失败356/27、
unsupported854/89，pending/processing均0；这不是本轮测试消息数。
执行者失败含签名348、schema_image7、encoding1，unsupported包含
application_format132、native_format722。只读近30分钟还发现10条签名失败、
1条application_format、14条native_format，不能全部归为旧故障。
本地只读探针对最近1000条保留原包做离线解析：文本797、图片108、语音27、
视频27、格式7一条、转发3条，仍有签名失败15及原生格式5的22条。
探针使用工作区候选解析代码，不能冒充当前冻结运行版的性能/兼容结论。
未跳过签名、改密钥、清理原包或调整恢复水位。

辣椒聊先进行32秒观测，后在热修复上做40秒、5次观测：
00:15:13.503建立连接，connected_at在五次采样中完全不变，
heartbeat_at/synced_at正常推进到00:15:43；全部connected、重连0、
队列0、新增隔离0，但messages/frames/ACK均0。只能确认连接及时间修复，
不能确认应用消息格式、持续流量或全天稳定性。原disabled配置每轮均CAS恢复，
revision2→3→4、修复后4→5→6；未开启辣椒聊群操作或发送。

00:18收尾两个旺商聊接收器仍enabled、revision23/7不变、connected，
心跳新鲜，当前进程/连接观测消息与ACK分别12/12和38/38，队列0、新增隔离0。
计数会随重建连接更新，不与发布前计数直接相减解释丢失。
未决原生/设置/上传均0，36迁移不变，10条历史隔离编号与密文摘要全部保留。
账户级解码失败与接收协议隔离是不同统计，不能用“隔离0”声称全部解码成功。

### 修复、回归与实际交付

本轮修复两项：read_group在原有网站权限、套餐、窗口及epoch复核后单独返回
公告列表，其他gateway快照仍不序列化公告；辣椒聊连接、心跳和同步时间改为独立
值，防止循环复用同一个time变量使历史快照随新心跳变化。新增合成传输测试覆盖
连接时间固定、后续同步/心跳推进、旧快照不被修改、取消退出并关闭连接。
测试依赖接口只复用现有连接所有者，不增加socket、运行服务或协议支持。

| 检查 | 本轮真实结果 |
| --- | --- |
| 当前候选Go | 生产cmd/internal全范围race/sodium与vet通过，部分命中缓存；裸./...首次受ignored var诊断目录多个main影响失败，未删除用户文件掩盖 |
| 当前候选隔离PostgreSQL | 串行HTTP race/sodium171.380秒、gateway28.835秒通过，含新时间/取消测试；仅wsl_test |
| 实际热修复源码 | 冻结源码./...全范围race/sodium及vet通过；隔离HTTP173.201秒、gateway20.853秒，串行执行通过 |
| 前端 | 当前产品vue-tsc/Vite通过，2322模块、27.18秒；没有前端代码变更，不重建或重启Web |
| 架构事实 | update/check、所有权及Docker守卫、45项工具测试、3项渲染缓存测试通过；七图布局不变，仅连接包装调用链证据更新 |
| 咨询性模型分析 | 本次可信预检兼容，incremental通过；227文件，源码f078ff7a0fdd40d2da3269d0d9168ad8666f6f3fa3f0e58dc9d8537964446caf，9组件，1请求/预留91461/上报22115/未知0，持久检查点保存；未全量回退或使用旧豁免 |
| 容器smoke | 新API配现有Web，独立空库迁移、bootstrap、受限角色、非root只读、登录退出、SSE、代理及关闭门槛全部通过；无生产数据 |
| 交付前 | 五服务健康，36迁移、原生/设置/上传未决0；10条历史隔离摘要预存 |
| 交付 | 2026-11-01 00:07左右，仅现有Compose API替换，当前容器5c9dc1a8148f…、healthy/RestartCount0，1CPU/512MiB、非root只读；环境、挂载、网络及限额相同 |
| 交付后 | 00:18只读验证通过；Web/PostgreSQL/heatbot/Simon身份、镜像、配置及重启次数未变；无生产迁移或数据删除，两readiness仍false |

运行镜像为`wsl-api:acceptance-observations-20261031` /
`18db1cfa893eb5c7605911fbb0046de81d26826d8fafe8d18bcced2ad7ac0a12`，
local标签相同。回滚`wsl-api:rollback-20261031-pre-acceptance-observations`
固定紧邻上一运行版`027704483c8a92bf850e29de377a20483dc870d74a3c2ed5533fb2c173754ba2`。
构建从该运行版的冻结源码开始，2492文件摘要对比仅
gateway/lajiaoliao.go、新增其时间测试、httpapi/test_tools.go三项不同；
未夹带尚未通过整体性能的解码候选。咨询分析描述当前候选源码，
不把候选图等同于这份最小交付源码。

首轮准备时账号connection PATCH已提交成功，随后的角色读取因连接版本切换
拒绝；保留失败，不将成功PATCH误记成失败或重放。公告补读的第一次准备在20秒
等待内未获得新鲜角色，未创建窗口或启用套餐，已恢复临时开关；再次只读准备
保留旧失败，等到新连接及角色就绪才建立回读窗口。原生修改始终只有3+2次。
现场UI曾显示“测试受控1未登录”，网络请求ID证明选中了另一个同名未认证槽位；
切换已认证“大蛤·测试受控1”后显示接收正常、真实P95及样本，未伪造登录状态，
也未删除那个未认证槽位。

临时非商业零积分套餐和审计保留；群管理/AI、订阅enabled、原账号连接开关
均恢复原关闭状态，原测试开关保持开启，两旺商聊接收开关保持原开启状态，
所有本轮窗口已取消。订阅plan已恢复，但原到期时间是过去时间，现有API即使
disabled也拒绝过去日期，因此到期元数据暂保留临时未来值，**未精确恢复旧到期时间**；
订阅仍disabled，无积分发放或商业扣款，原值在私有证据保留，不用SQL绕过API。
这项收尾限制不应表述为“全部配置逐字段恢复”。

私有证据位于`~/.cache/artwslbot/deployments/20261031-complete-native-acceptance/`，
包含state.json原始前后快照/每次提交和失败、冻结源码/provenance、镜像上下文、
发布预检及delivery.json。原生内容仅保留在本地私有证据，未提供给架构模型。
本轮未执行远端CI、提交或推送；本地回归、咨询分析、实际交付与原生验收分开记录。
下一轮先由所有者用“大蛤”接受现有测试群邀请，再以新鲜完整成员证明规划成员
禁言/解禁、他人名片和移出；发送、撤回及解码失败需实现补齐后独立验收。

## 原生换票502追查与本地运行时恢复（2026-10-31，Asia/Shanghai）

历史记录时间为03:09:51，`im_token / native_unknown_result / 502`。
代码核实固定换票接口`/v1/user/get-nim-token`的HTTP状态不低于500时直接保存
`response.StatusCode`，随后被接收器映射为未知结果并阻断自动恢复。
这是原生出站HTTP返回的502，不是业务JSON状态码，也不是网站Nginx转发返回值。
原生HTTP客户端禁用环境代理、保留TLS验证；本次核对该路径源码与已部署冻结源码
一致。旧API容器和当时响应头/请求关联号未保留，不能进一步声称已证明提供方
内部哪台代理或换票后端出错；当前成功也不能补足该历史证据。

追查中另发现22:05及22:12的本地事故，必须与凌晨故障分开：
PostgreSQL日志记录进程SIGKILL及恢复，cgroup的`oom_kill`计数22→23，
VM一度仅约252MiB可用内存、1GiB交换空间几乎耗尽。其后Docker持续报告
API任务仍running、无法删除及restart manager状态冲突；
API最终积累97条空闲数据库连接，报`SQLSTATE 53300`连接槽耗尽。
这同时解释当时网站访问失败、`receiver_owner_unavailable`与接收观测不再更新。
未取得对应内核调用栈，不将单个退出137都认定为独立的API内存超额。

22:25前只读确认36迁移、未决原生/设置/上传均0，保存服务镜像/资源/
环境摘要。仅通过原Compose停止、移除异常API；旧地址已无网络端点，
97条连接均为已移除API地址、早于停止时间、idle/ClientRead且无事务，
留存逐项清单后终止97/97。随后按原镜像重建API，未重启数据库或其他产品服务，
未直接修改接收器owner/租约、账号凭据、权限、接收开关或水位。

| 当前核验 | 实际结果 |
| --- | --- |
| API恢复 | 22:27:39新容器`1fb638b239b8…`启动；镜像仍`027704483c8a…`，healthy、RestartCount0，1CPU/512MiB、非root只读 |
| 协调所有者 | 原97条孤立连接释放，新协调连接22:27:40建立；22:29读取同地址仅11条连接，无旧所有者 |
| API实际上游读取 | 22:28:13现有管理API匿名加密配置检查成功，`verified=true`、部署版本2；没有发起诊断性原生登录或换票 |
| 容器网络元数据 | 22:37:48一次性诊断容器共享API网络；三次HEAD为404且TLS均有效；匿名加密配置读取`verified=true`，374.915ms，连接`111.29.46.72:443` |
| 大蛤接收 | revision7未改，22:27:49连接并同步；22:28及22:33均connected，后者消息9/ACK9、新增隔离0 |
| 另一启用账号 | revision23未改；22:33消息238/ACK238、新增隔离0，connected且心跳存在 |
| 恢复后最终观测 | 22:41两账号消息/ACK增长到439/439及18/18，均connected；心跳22:41:03/22:41:04，队列0、新增隔离0、重连0；API已运行约13分钟，healthy/RestartCount0 |
| 持久状态 | 22:34只读核实36迁移、未决原生/设置/上传0、历史隔离总数10；未迁移或重置库 |
| 本地诊断程序 | 新增显式诊断工具和三项合成测试，默认无网络；race/sodium通过1.324秒，vet通过；不属于运行镜像 |
| 架构事实 | 既有架构venv运行check通过、七视图无变化；所有权/Docker守卫通过，没有新增生产入口或结构依赖 |

宿主网络诊断在22:12记录目标`ifeng-api01.hfjtqy.com`、实际连接
`120.221.107.138:443`、有效TLS链和证书摘要；三次HEAD均404，
匿名加密配置读取成功。根路径404不代表业务接口失败；该组只称宿主网络证据，
不冒充容器结果。第一次程序未从复用TLS连接补取状态，后续已修正并用合成测试
验证复用连接也保留证书校验信息，原文件保持不改。

容器诊断前置失败分别为缺少缓存模块、只读嵌套挂载、512MiB限额下编译器
被杀及临时目录禁止执行，均在工具执行网络请求之前；保留失败，不算上游故障。
随后维持512MiB限制，串行编译并限制Go编译器内存，临时编译目录允许执行，
同一诊断代码成功运行。缓存和源码只挂入临时诊断容器，未改生产运行时或放宽
生产容器权限。工具最终一次网络运行共三次HEAD、一次匿名读取，没有自动重试。
容器与宿主解析到不同公网地址，但证书摘要一致；不能据此把历史502归因到某个节点。
本轮临时容器并未增加生产资源限制或停止其他项目容器，运行结束自动移除。
先前90容器停止清单与当前新建验收容器无重合，未复用旧授权操作新容器；
观测到其他项目容器数量下降后VM可用内存回升，不归功于本轮操作。

原已启用接收器在API恢复后按既有机制自动连接/续票，不能把本轮描述成完全
没有原生网络活动；诊断工具不打开账号会话、不直接续票，不进行登录、短信、
发消息或群管动作。原镜像、回滚标签、配置及readiness保持；
未把工作区未达性能目标的解码候选部署。新增诊断不改变未知结果禁止自动重放的规则。
本轮没有新源码发布，没有重跑完整产品构建/隔离PG/镜像smoke或咨询模型分析；
上表是诊断工具检查及同一运行镜像的恢复核验，不援引旧结果冒充新一轮发布验收。

私有证据位于`~/.cache/artwslbot/feature-dialog-receiver-20261031/`：
`upstream-host-probe.json`、`upstream-local-db-incident.json`、
`upstream-recovery-before.json`、`upstream-orphan-connections.json`、
`upstream-recovery-after.json`、`upstream-api-anonymous-check.json`及
`upstream-container-probe.json`。后者按成功程序输出保存并注明来源，
不包含任何用户令牌或响应正文；`upstream-receivers-2233.json`和
`upstream-receivers-final.json`保留前后观测。诊断边界和后续操作见
`native-upstream-diagnostics.md`。

## 大蛤测试受控1接收恢复复核（2026-10-31，Asia/Shanghai）

21:42只读确认该账号revision5阻断原因是`im_token / native_unknown_result / HTTP502`，
不是协议隔离；本地保存会话、设备和部署版本校验通过，不等于上游会话有效。
所有者随后询问修复方式，本轮准备先经既有只读目录刷新核验会话，再限定一次
接收CAS恢复。执行前错误原因栅栏发现状态已经变化，因此在任何目录刷新、
接收写入或换票前退出；没有重复原未知请求或发起新业务登录。

21:52重新读取已是revision7/connected。只读审计确认21:47:46.468停用到revision6、
21:47:46.901启用到revision7，21:47:50.253连接、21:47:50.581首次同步完成。
该启停发生在本轮恢复脚本之前，不能归功于本轮脚本。
21:52:56、21:53:12、21:53:28三次观测心跳分别为21:52:50、21:53:05、
21:53:20；均消息100/ACK100、队列0、新增隔离0、重连次数0。
该窗口未新增消息，不能据此证明持续流量吞吐或全天稳定性。

API/Web仍为上一节已交付镜像，容器身份未变、healthy/RestartCount0，未再次部署。
只读数据库复核36迁移、原生/设置/上传未决0、历史隔离总数10。
本次仅网站管理登录、数据库/API读取及私有观测记录；没有目录刷新、接收CAS、
原生登录、短信、主动消息、群操作或模型调用；readiness保持false。
人工重新连接是一次新的接收恢复，不代表HTTP502的上游根因已消除。
私有证据在`~/.cache/artwslbot/feature-dialog-receiver-20261031/`的
`blocked-reconnected-audit.json`及三份`blocked-reconnected-observation-*.json`。

## 功能弹窗基准与 4/31 通知交付（2026-10-31，Asia/Shanghai）

**已部署 API/Web，并恢复受 `4/31/144` 影响账号的接收。** 上一轮因咨询性
CodeBoarding 增量分析连续两次失败暂缓发布；2026-10-31 所有者对“本次豁免
这项咨询性分析，部署已验证镜像并恢复受影响账号的接收”明确回复“允许”。
本轮只依据这次授权豁免该项，分析仍记失败；没有全量回退、增加预算、
改写可信基线或放宽后续发布检查。下表其他检查沿用同一已验收镜像的实测。

功能目录改为只读已保存状态，移除列表开关与全局保存；模块弹窗内编辑、保存、
放弃当前模块修改，保存不夹带其他模块草稿。财务、抽奖记录管理也改为弹窗，
创建与开奖沿用已有表单/确认、权限、作用域与服务端契约，旧链接打开同一弹窗。
基准记入 AGENTS.md 与 feature-center.md，不开放未迁移模块或真实原生执行。

`4/31/144` 已只读确认是完整单个 Property，与官方固定 web-ng 10.11.5 的
`v2FriendOperationListOnline` 契约一致，证据见 E41。新增 `31:"P"` 通知结构，
完整消费及计数/大小/重复检查继续由既有读取器处理，只密封留存，不审批好友、
生成聊天 ACK、删除历史或按通知时间推进水位。原隔离记录完全保留。

| 检查 | 当前实测 |
| --- | --- |
| 产品构建 | vue-tsc/Vite通过，2322模块，14.57秒；CSS `index-B13RAIAW.css`，JS `index-DUfdAlnT.js`；三个产品Vue文件Prettier通过 |
| 功能编辑UI | features合成回归通过：版本冲突、当前模块独立保存/其他草稿保留、账号草稿、迟到读取/保存、只读角色、原生依赖；三次模拟保存，无真实原生调用；320/647/1440编辑器与底部保存边界通过，320截图人工复核 |
| 其他合成UI | business_modules、console、native_social、native_receiver、multi_platform、theme、assistant、platform_assistant、go_console通过；业务弹窗320/390/647/1440及390/1440截图人工复核，无真实财务/开奖/群操作 |
| 产品Go | 当前候选及实际热修复源码分别完成全范围race/sodium与vet；当前候选部分使用缓存，热修复源码重新运行 |
| 实际热修复PostgreSQL | 串行HTTP140.292秒、gateway21.768秒，通过；仅重建wsl_test。覆盖好友通知先于心跳、持久通知不发聊天ACK/不唤醒解码器、畸形通知后不能越过水位、既有ACK落盘/epoch栅栏 |
| 架构事实 | update/check、所有权守卫、45项Python检查、3项渲染缓存测试通过；新增业务弹窗组件只更新frontend视图布局，其余视图布局不变；本次交付后使用现有架构venv再次运行check/守卫通过 |
| 咨询性模型分析 | 可信基线预检兼容；同一227文件源码`ed204cf3612081639043b702eb24bc1a943e03d84a0d0fcee7c0a758089b1574`两次incremental失败。首轮出站3次/保守预留286906/上报45232/未知1；次轮2次/195745/45443/未知0；scope root返回502，后续请求未新增出站，当前安全日志不细分上游与预算拒绝。旧可信基线/检查点保持完整 |
| 容器smoke | 新API配新Web通过：空库迁移/bootstrap、受限角色/RLS、非root只读、会话登录退出、SSE、上传代理边界、关闭门槛；未使用生产数据 |
| 发布前只读状态 | 21:12重新核验五服务运行且配置有healthcheck者健康；36迁移；running/unknown原生操作0，preparing/submitting/unknown设置0、媒体上传0；10条既有隔离记录保存编号/密文摘要 |
| 实际交付 | 只通过现有compose.yaml+compose.local.yaml更新API/Web，其他三服务身份/镜像/资源/环境/挂载/重启次数保持；线上HTML/JS/CSS/两平台图片五项与已验收dist逐字节一致 |
| 接收恢复 | 21:16既有receiver PUT按revision CAS将4/31账号22→23；21:16:20连接，21:16:21首次同步；21:16:44、21:17:48、21:18:04三次采样均connected，新鲜心跳递增，消息/ACK为368/368、373/373、375/375，队列0、新增隔离0 |
| 交付后只读复核 | 21:19确认36迁移、未决原生/设置/上传0、原10条隔离密文摘要全部一致；恢复后新增持久事件为群360、私聊22、通知34、水位1，未新增隔离；两readiness仍false |

为避免夹带未达性能目标的候选，热修复从工作台运行镜像 `c013d10ab9e43…`
的冻结源码构建。该快照与解码优化前基线的326个Go源码/依赖文件逐项摘要一致；
只增加 im.go 的通知声明及两个测试文件，未纳入优化后的解析、合批、队列或诊断代码。
实际运行二进制为
`wsl-api:friend-notice-20261031` / `027704483c8a92bf850e29de377a20483dc870d74a3c2ed5533fb2c173754ba2`；
Web为
`wsl-web:feature-dialogs-20261031` / `f484282f83a2d27b47446175b9d29f55e98588bf105d5d95ae4db450b6daa92b`。
回滚镜像已固定为 `wsl-api:rollback-20261031-pre-friend-notice`（c013）、
`wsl-web:rollback-20261031-pre-feature-dialogs`（9b6927），local标签已指向本轮镜像。
新API容器`a49f11ce6577…`、Web`48ef67a9b381…`均healthy/RestartCount0；
保留release、API 1CPU/512MiB、Web 0.25CPU/128MiB、非root只读、原环境与挂载。
PostgreSQL、heatbot、Simon未重启，生产库未迁移或删除数据。

恢复只提交一次既有接收配置CAS，没有原生账号密码登录、短信、主动发消息、
审批好友、群操作或业务模型调用。另一个`native_unknown_result`账号的
revision5及完整接收视图与发布前一致，未执行恢复或未知提交重放。
消息/ACK计数属于新revision的连接观测，不与旧revision790/790相减计算丢失。
本次真实证据为限定时段接收恢复，不代表全天稳定性、原生整体或AI验收通过；
native_ready/ai_ready继续false。网站管理登录、接收配置CAS和Compose更新
为实际操作；未执行远端CI、提交或推送。

首轮功能UI脚本在关闭动画中强制点击账号控件，改为等待弹窗隐藏；
只读角色默认进入用户中心，脚本显式选择WSL工作台后通过，不放宽产品权限。
新增编辑器边界检查首轮早于入场动画结束，等待动画250ms后全部通过；
截图确认320宽弹窗正文滚动、底部操作可见，没有改松产品布局或边界判定。
发布前只读脚本曾因多行JSON和json/jsonb类型匹配错误退出，修正为一致jsonb后
成功完成预检；两次失败均在任何标签切换/Compose操作前，无运行影响。
交付脚本在完成替换后首次因API环境变量数组顺序变化误报摘要不一致。
对当前10项完整值求排列摘要，第1549861次匹配发布前原摘要，证明值未变；
原预检保持不改，仅在只读复核接受该明确证明。首次复核的证明比较误传数组
导致类型错误，改为序列化后复核通过；这些失败均未触发第二次重启或配置写入。
凭据未输出；`environment-proof.json`只保存摘要、变量名顺序和匹配次数。
私有证据、镜像构建上下文、发布前备份、交付/恢复及三次观测脚本输出在
`~/.cache/artwslbot/feature-dialog-receiver-20261031/`。

## 财务活动归入业务模块（2026-10-31，Asia/Shanghai）

两项定义复用现有功能目录，不重复注册或修改后端。前端保留业务模块和原生功能表
两个标签，默认进入业务模块（已选原生视图的账号保留偏好）；财务台账、活动抽奖
从目录进入，子页提供返回入口。当前群传到列表筛选与创建表单，返回恢复目录选择，
账号/平台/用户/租户范围独立；群列保留归属，缺少群名时显示既有记录的群编号。
筛选仍是已有最近最多200条可见投影，不称为全量历史或财务总额。
只有对应业务读取权限的成员使用精简目录，不请求受限的原生/群配置目录。
旧深链接继续受权限和平台限制，未添加业务启用开关、自动转账、报名或通知。

另将模型配置单一来源、重复能力占位、指标去重、聊天默认值继承列为后续候选；
操作/安全/助手审计及群台账/套餐积分/上游预算应保留各自职责。
清单与代码依据见
`architecture/decisions/2026-10-31-business-module-consolidation.md`；
清单中的后续建议本批未实施，不把文档设计当成运行能力。

| 检查 | 本批结果 |
| --- | --- |
| 产品构建 | vue-tsc/Vite通过，2319模块、13.41秒；CSS `index-DHUozCVg.css`，JS `index-ChE5gRY2.js`；产品文件Prettier通过 |
| 合成UI | 九组通过：business_modules、console、multi_platform、commercial、native_social、theme、assistant、platform_assistant、go_console |
| 新增业务回归 | 父级标签和返回、群筛选/创建预填/返回恢复、切换账号隔离、业务只读角色、无群读取时不请求群配置、旧链接/无权限拒绝、WSL业务不出现在辣椒聊通过；业务脚本写入0 |
| 视觉与兼容 | 业务页320/390/647/1440截图，390/1440人工复核；既有主题、助手唯一空会话/首问标题/十轮窗口、原生昵称、配置CAS、可选扩展关闭及多平台隔离回归通过 |
| 产品Go | `go test -race -tags sodium ./cmd/... ./internal/...`和同范围vet通过；本批未改Go，使用测试缓存；数据库另列 |
| 隔离PostgreSQL | 串行 `WSL_INTEGRATION=1 WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium -p 1 -count=1 ./internal/httpapi ./internal/gateway`通过；HTTP134.186秒、gateway26.769秒；只重建`wsl_test` |
| 架构事实 | update/check、所有权/Docker守卫、45项架构Python与3项渲染缓存检查通过；图布局无变化，仅源码证据/清单更新，七图渲染缓存完整 |
| 咨询性分析 | 可信基线兼容，incremental通过，226文件/9组件；源码`785d7711036b1cf6d632d00c89b444d80a8edd1111702934d4c04292afba65c7`，请求/Token/未知0，持久检查点保存，无全量回退 |
| 容器smoke | 新Web配原运行API，在一次性空库完成迁移/bootstrap、受限角色/RLS、非root只读、登录退出、SSE、上传边界、关闭门槛检查；未使用生产数据 |
| 线上复核 | HTML/JS/CSS/两平台图片五项与已验收dist逐字节一致，nginx配置通过；仅Web容器更换，其他四服务身份、镜像、资源、环境/挂载及重启次数保持 |

首轮业务UI脚本使用宽泛`.el-select`定位，新建台账的群与类型两个控件触发严格模式；
改为按“业务操作群组”定位。次轮切换模拟角色前的hash导航仍经过上一身份的内存状态，
出现旧权限目录读取；改为跨文档加载后验证新身份，最终九组通过。未放宽产品权限。

18:11–18:14通过现有`compose.yaml + compose.local.yaml`仅更新Web：
镜像`9b6927c5d125f44fc5ec5ce4abb4efc3072edf4243f603b33e85aefb9b32ced2`，
容器`6ed24448a253…`，healthy/RestartCount0，release、0.25CPU/128MiB、非root只读保持。
API仍`c013d10ab9e43…` / 容器`95ff555cddfb…`，历史RestartCount5；
原生接收及其余服务未重启，36迁移不变。
上一版`1029707d2ee6…`保留为`wsl-web:rollback-20261031-prebusiness-modules`。

交付脚本更新Web后首次误报PostgreSQL挂载变化：原脚本直接对Docker返回数组求摘要，
重复inspect确认同一挂载清单存在两种顺序，两种摘要均已复现；容器启动时间仍为
10月30日01:18、RestartCount0，未新增重启。修正为按目标路径排序，复核旧预检摘要
确实匹配当前完整清单的排列后再规范化，没有忽略挂载内容差异。
只读`--verify`完成全服务与线上五项资源检查，未再次重启。原预检未覆盖，
`delivery.json`标记此次顺序规范化及只读复核。
私有证据`~/.cache/artwslbot/business-modules-20261031/`保存预检、交付、已测dist及镜像构建；
截图在`output/playwright/business-modules-*.png`。

本批没有真实账目写入、开奖、原生动作、短信、原生账号登录或业务模型调用；
原生/AI readiness仍false。未交付性能仍未过的解码候选，未执行远端CI、提交或推送。

## 自营服务分层、助手审计与扩展目录（2026-10-31，Asia/Shanghai）

按所有者确认，平台提供完整机器人业务，客户负责选择服务、授权群和配置，
不要求客户自行编写业务。Web群管助手保留为对话配置入口；超管独立AI对话
退出产品导航，原深链接在WSL超管视图转向助手审计，其他视图回工作台。
审计复用最近200条可见事件中的四类助手元事件，白名单展示操作人、编号、
版本/结束状态和时间，不显示完整detail、提示词、会话正文或密钥。
它不是完整历史查询、逐工具追踪或跨成员审批队列。

学习、长期记忆、人物印象、媒体记忆和工具归入“可选扩展 → 扩展中心”。
目录使用真实能力清单；当前运行链路未接入，五项运行开关均禁用。
学习/记忆详情仍保存账号默认意向，明确区分默认配置与群级运行状态；
详情不再占据主导航，既有深链接仍受热群超管权限限制。
目录读取不提前读取每个模块配置，不创建任务、会话或模型调用。
窄屏使用无框列表，完整显示状态、作用域、关闭的运行开关和设置入口。

四层边界、目标图、关闭/取消/恢复、保留和费用契约记录于
`architecture/decisions/2026-10-31-managed-service-and-extensions.md`。
持久事件、群级扩展注册/启停与独立调度仍是待实施设计，
没有增加解码器钩子或声称插件运行闭环、零总延迟、生产容量通过。
必要的协议认证、严格格式和UTF-8检查不受扩展开关影响。

| 检查 | 本批实际结果 |
| --- | --- |
| 产品构建 | vue-tsc/Vite通过，2319模块、14.10秒；CSS `index-DS9zxc3a.css`，JS `index-DZlAN56R.js` |
| 最终合成UI | 七组通过：go_console、theme、multi_platform、messages、platform_assistant退场、console、assistant；320/390/647/1440，登录额外768 |
| 关键回归 | 旧链接不请求旧平台助手API、租户不能访问运营审计、五项未接入开关禁用、默认配置按需读取、审计正文哨兵不显示；群按钮、原生昵称、唯一空会话、首问标题、十轮窗口、人工确认、迟到结果隔离及未知不重放通过 |
| 配置与窄屏 | 账号草稿隔离、密钥不存浏览器、CAS冲突保留输入、移动导航收起后再显式展开、末项可达；扩展列表390截图复核开关对齐，无页面横向溢出 |
| Go | `go test -race -tags sodium ./cmd/... ./internal/...`及同范围vet通过，未改Go包使用缓存；数据库另列 |
| PostgreSQL | `WSL_INTEGRATION=1 WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium -p 1 -count=1 ./internal/httpapi ./internal/gateway`通过，HTTP151.267秒、gateway26.788秒；串行且只重建`wsl_test` |
| 架构事实 | update/check、所有权/Docker守卫通过；仅frontend依赖布局变化，其他六图不重画，最终只刷新manifest；45项Python及3项渲染缓存测试通过，七个生成图和新增目标图渲染通过 |
| 可信增量 | 兼容预检及incremental通过，226文件/9组件，源码`8b4f23073590bfed92354869b9f7f8201c8ef1b0ea080ddb6bf6b20abfd91cc7`；请求/Token/未知均0，可信持久检查点保存，无全量回退 |
| 容器smoke | 最终Web配现有运行API，在一次性空库完成角色、迁移/bootstrap、强制RLS与租户拒绝、非root只读、登录退出、SSE/上传边界及关闭门槛；未使用生产数据 |
| 线上校验 | 新HTML/JS/CSS及两平台图片五项与已测dist逐字节一致；Web healthy、RestartCount0，其他四个运行服务身份、镜像、环境/挂载摘要、资源和重启计数与交付前一致 |

首次旧链接夹具在旧身份页面内改变模拟身份再进行hash导航，导致旧链接提前按原身份
解析，改为独立文档导航后通过；不以此放宽产品角色检查。
菜单与快捷按钮同名后，助手脚本明确定位顶栏快捷入口。
旧配置脚本仍假定移动导航切换后可见，改为核对视图并显式展开后继续，
保留导航关闭时退出焦点顺序的产品行为。上述失败输出/截图与后续通过日志分别保留。
架构守卫首次误用缺PyYAML的系统Python，改用项目既有架构venv后通过；
没有安装新产品依赖或放宽守卫。

17:24本地交付仅执行现有 `compose.yaml + compose.local.yaml`
的 `up -d --no-deps --no-build web`：

- Web镜像 `1029707d2ee69b87535192a345d8aef003edc48b57f641725ceffc73499d870d`，
  容器 `b0294e4e0a91…`；release模式、0.25CPU/128MiB、非root只读保持。
- API仍为 `c013d10ab9e43…` / 容器 `95ff555cddfb…`，1CPU/512MiB，
  历史RestartCount5未变；未重启原生接收，数据库/heatbot/Simon容器未变。
- 通过既有网站超管身份及版本化API关闭独立平台AI新调用，设置版本1→2；
  原月预算保持，历史及未知用量预留未删除/释放。保留旧后端与存储，
  不把菜单收起描述为已删除后端端点。
- 紧邻上一版Web `ec7e0ca57efb…` 保留为
  `wsl-web:rollback-20261031-preservice-boundaries`；未删除其他镜像/数据卷。
  页面回滚不会自动重新开放超管AI。

私有证据：`~/.cache/artwslbot/service-boundaries-20261031/`，
包括`ui-final-pass.log`、`ui-config-final.log`、`container-smoke-final.log`、
`preflight.json`、`admission.json`和`delivery.json`。
没有新增迁移，没有真实原生动作、短信、原生账号登录或业务模型调用；
网站管理登录与关闭设置为本批实际操作。既有原生接收可继续正常联网。
解码器候选整体性能未过，未随本次Web发布；native/AI readiness仍false。
本地检查、咨询性增量分析和本地交付分别记录，未执行远端CI、提交或推送。

## 界面精修、资源释放与完整对照验收（2026-10-31，Asia/Shanghai）

**本批只交付前端；解码器候选整体性能仍未通过，API未更新。**
继续使用所有者指定 `web-design-templates` 的 Voltline Analytics 本地资源，
重新调整产品布局，而非仅替换主题色。登录使用独立单列表单及两平台标识；
侧栏改为浅色分组导航，工作区统一白色底面、细分隔线、紧凑标题和墨色主操作。
指标使用分栏带及语义图标，图表保持多种状态颜色；未测容量目标默认折叠。
重复的全局验收提示移入顶栏弹层，仍明确显示原生与AI未验收。
助手保留群按钮、原生昵称及独立空白会话，调整欢迎状态、输入区和历史外观；
网关按运行预算、模型连接、计价核验分组，钱包状态独立展示。
移动端群服务四个标签和操作图例完整显示，无需翻页查看图例。
未增加依赖、迁移或业务权限；模板许可、原设计文档及本机字体继续保留。

| 检查 | 本批实测 |
| --- | --- |
| 产品构建 | vue-tsc / Vite通过，2325模块、最终14.88秒；CSS `index-c_QWASey.css`、JS `index-Bl3dfk7J.js` |
| 合成UI | 23组全部通过：theme、console、account_dashboard、gateway_inspection、platform_assistant、multi_platform、commercial、assistant、messages、runtime_monitor、test_tools、native_settings、native_social、native_members、group_membership、media_sending、message_media、message_sending、native_receiver、lajiaoliao_auth、native_auth_error、message_video、live_updates |
| 最终局部复查 | 最后仅修改WorkspaceInsights的移动标签和图例后，console、multi_platform、account_dashboard三组再次通过；320/390/647/1440，登录额外768、接收页额外654，图表及视频像素验证通过 |
| 隔离与业务状态 | 合成验证租户/超管、平台/用途/群范围、空白会话复用、草稿、未知提交不重放、SSE撤权及迟到响应；所有真实原生动作、登录、短信及业务模型调用新增0 |
| 架构事实 | update/check、所有权守卫、45项架构Python、23项Compose/性能工具及3项渲染缓存检查通过；七个Mermaid视图布局不变，最终仅刷新源码行证据 |
| 可信增量 | 最终226文件/9组件，源码 `047dd6ec789587a6705e9f76a10dc9305049fc35c0ee026aa78753707b11f9c7`；preflight兼容、incremental通过，0请求/0token/未知0，持久检查点保存，无全量回退 |
| 容器smoke | 新Web配当前运行API，在独立空库完成受限角色/迁移/bootstrap、非root只读、登录退出、SSE、上传拒绝及关闭门槛；非生产数据 |
| 线上核对 | HTML、CSS、JS和三项平台图片/图标逐字节及摘要一致；Web healthy、RestartCount0；API/PG/heatbot容器ID未变，API仍为历史RestartCount5 |

合成工具修正也单独保留：新欢迎状态及折叠目标替换旧断言；屏宽切换后等待两帧，
让ResizeObserver/ECharts完成尺寸同步，仍执行真实边界及像素断言。
原多组浏览器复用同页的initScript污染后续夹具，改为每组独立浏览器上下文；
辣椒授权夹具只在同源顶层初始化sessionStorage。首次失败日志/截图保留，
未削弱产品授权、验证码隔离或未知提交门槛。
交付预检误将无healthcheck的Simon判失败，Shell未及时终止仍执行已授权Web更新；
随后按实际健康检查配置重新核对运行状态、容器身份、环境匹配和全部线上资源，
均通过。该预检错误不是产品验收成功记录。

最终Web镜像
`ec7e0ca57efb53a214aa1cccbec9984e55208832f46914e99d8841551807f578`，
容器 `0e513cd36386…`，通过现有 `compose.yaml + compose.local.yaml`
执行 `up -d --no-deps --no-build web`，保留release模式及0.25CPU/128MiB限制。
API仍为 `c013d10ab9e43…` / 容器 `95ff555cddfb…`，
1CPU/512MiB未变，未重启原生连接；Compose配置/凭据/绑定及36迁移保持。
紧邻上一版 `94ef924f66ab…` 保留 `wsl-web:rollback-20261031-prerefine`。
私有证据 `~/.cache/artwslbot/console-refinement-20261031/`，
包含 `ui-final.log`、`ui-insights-final.log`、`container-smoke-final.log`、`delivery.json`。
未操作已有浏览器标签页中的未提交草稿；刷新可加载新前端。
原生与AI readiness继续false；没有执行远端CI、提交或推送。

### 已授权的测试资源释放

所有者本批明确批准停止已列出的90个验收PostgreSQL容器。
停止前逐项核对 `resource-review.json` 的确切ID、名称和镜像，
只停止清单中 `tenant-acceptance-pg-*`，90个均已确认退出；
没有删除容器或数据卷，没有prune，也没有停止平台服务。
授权和实际停止结果保存在当前解码器私有目录 `authorized-stop.json`。
后续矩阵每轮仍使用1536MiB启动/1024MiB中断阈值及PG OOM变化保护，
没有提高本地资源限制。

### 当前解码器完整矩阵

相同冻结基线、产品Go `cae989ca8ef8…`、测试二进制 `0a3529afb932…`，
合批模式0，label `released-memory`。两平台各四场景、五组交替、
每版本每轮10000条，共800000条测量消息；计数/状态检查均通过，
资源保护未中断，原早期OOM和不完整矩阵仍保留。
吞吐门槛使用饱和负载，P95门槛使用各自基线容量70%的相同输入速率；
下表为五组中位数，不挑选最快单轮。

| 平台/场景 | 饱和吞吐（条/秒）或同速率输入 | decode P95 ms，基线→候选 | 含写回P95 ms，基线→候选 | 排队P95 ms，基线→候选 |
| --- | --- | --- | --- | --- |
| 旺商聊混合饱和 | 303.10→765.83，+152.67% | 2.10920→1.77755 | 39.89505→22.94918 | 14.21885→68.38519 |
| 旺商聊混合同速率 | 212.16884条/秒 | 2.54380→1.31671，-48.24% | 12.92234→4.79464，-62.90% | 180.40454→14.23529 |
| 旺商聊单账号饱和 | 488.09→911.20，+86.68% | 2.08015→1.67330 | 53.67403→9.78213 | 60.28266→56.62366 |
| 旺商聊单账号同速率 | 341.66607条/秒 | 1.70600→1.34013，-21.45% | 9.87324→5.49580，-44.34% | 101.80753→30.72322 |
| 辣椒聊混合饱和 | 409.16→731.85，+78.87% | 1.66330→1.41231 | 42.04689→28.42517 | 16.03373→71.21539 |
| 辣椒聊混合同速率 | 286.40877条/秒 | 1.97035→0.93924，-52.33% | 21.08629→4.66092，-77.90% | 2664.70440→13.68172 |
| 辣椒聊单账号饱和 | 536.12→938.53，+75.06% | 1.88615→1.38433 | 52.41162→7.78517 | 64.64488→64.89198 |
| 辣椒聊单账号同速率 | 375.28560条/秒 | 1.40805→1.24342，**-11.69%，未达15%** | 5.80666→5.19707，**-10.50%，未达15%** | 60.95448→26.92455 |

两平台解析集合耗时/分配字节均达到20%改善、分配次数下降；
旺商聊当前矩阵通过，辣椒聊单账号两项P95仍失败，**整体不得发布**。
同速率排队没有超过5%的稳定退化；混合饱和的排队确实变长，
原值完整保留，不能宣称所有排队指标改善或把饱和吞吐当作单条P95。
当前30分钟同源码持续负载通过：1800.509秒、347200条，两平台八账号，
每批持久计数/状态一致；最终active/queue_depth/queue_bytes均0，worker正常退出。
29次分钟采样协程9–10、队列均0，采样最大GC后堆2366504字节、
RSS26501120字节；这些是分钟观测，不能描述为瞬时峰值。
VM可用内存最低3079128KiB，PG OOM kill保持11、无新增，资源保护未中断。
该负载逐批排空、有背压，仍不是固定到达率或全天生产长稳。
汇总 `summary-0-released-memory.json` 确认持续负载与八个数据库场景产品源码及
合批模式一致，soak complete=true，但整体performance_accepted=false；
辣椒单账号两项P95失败不能由持续负载通过替代。
旧378400条持续负载属于另一源码摘要，原记录保留，未复用。
最近1000条阶段诊断显示辣椒聊单账号主要耗时仍在取源和写回，
协议解析阶段P95约0.024–0.033ms；诊断窗口不同于每轮万条总体，
不能将各阶段P95相加或替代总体门槛。
本批未更改产品Go、4 worker/16队列/4连接、CodecVersion=6、权限或两个关闭的合批常量。
后续继续定位数据库I/O和调度，不通过放宽目标、改计时边界或扩资源取得准入。

## 解码器领取优化与扩容检查（前一轮记录，2026-10-31，Asia/Shanghai）

以下为资源释放前的阶段记录，当前状态以本文件最上方批次为准。

**当前候选未部署，整体性能未验收通过。** 本轮继续使用10月30日冻结工作区，
不以缺少未提交改动的 HEAD 为基线；原冻结摘要
`a91c894b53391fb97b07cf93906e7b75d8a1597753e50e8d13bb98d541a835bf`。
当前产品 Go 摘要
`cae989ca8ef8c789f58e15d63e3961a02e80758f026f52bc736d7134c8441665`，
测试二进制 `0a3529afb932465e20d412f0767b7a20b32110b6aca7992c5d90045b8e01dded`；
原基线二进制始终为 `25771e24a256…`，历史结果未覆盖或改判。

定位到领取 SQL 对完成历史的反复扫描，以及部署单例的错误行数估算触发 JIT。
新增每账号 ID/版本游标、同快照全局源上界、明确单例查询；
每五秒和窗口内第一次空结果重新核对，避免迟提交和过期租约漏领。
小批请求使用 `LEAST(8, requested)`，减少固定到达率下不必要的候选读取；
独立完整排序参照核对1/2/3/5/7/8/16项前缀。公平顺序、4 worker、
16队列、4连接池和提交栅栏不变。取源/写回数组合批仍为独立试验，
**两个默认常量继续 false**；CodecVersion=6、36迁移及密封格式不变。
具体查询、缓存恢复、计时与扩容条件见 `docs/decoder-optimization.md`。

| 检查 | 当前证据及限制 |
| --- | --- |
| 产品 Go | 当前产品 race/sodium、vet通过；未增加依赖 |
| 当前解析微基准 | Linux arm64、1CPU/512MiB、GOMAXPROCS=2，五组交替全部完成；有效集合串行耗时WSL下降26.85%、辣椒下降60.90%，分配字节下降39.77%/74.68%、次数下降48.59%/47.86%，无单类稳定超过5%退化 |
| 隔离数据库 | 当前完整gateway race24.133秒通过；并发owner不重复领取、迟提交、取消、部署变化、租约、资格矩阵、冷热账号公平前缀及重复写回顺序通过；只重建wsl_test |
| HTTP数据库race | 当前完整host race158.441秒通过，只重建wsl_test；前一步产品曾91.426秒通过。此前Linux完整race内存失败保留，不以host成功改写 |
| 当前差分fuzz | host sodium五项各请求30秒，WSL认证/schema521633次、编码/签名1368652次、压缩边界1244934次，辣椒JSON29022次、原始wire22951次，共3187192次，无差异；这是正确性复查，不是Linux性能测量 |
| 架构事实 | update/check、所有权守卫、45项架构Python、23项Compose/性能工具测试、3项渲染缓存通过，七图布局未变 |
| 可信增量 | 当前226文件/9组件通过，源码`8bb2729c17a0aa2a6ef78468984b454ad9bbc2582359c19584da320b32e06fa6`；1请求、22669报告token、未知0，检查点保存；后续preflight兼容，无全量回退 |
| 前端 | 本轮vue-tsc/Vite通过，2325模块、12.51秒；界面在该检查后未改，CSS/JS仍为Voltline已部署版本 |
| 当前镜像smoke | API `7f03e1c05493a5b25db72252a73eb35e3b40eaea113a2afdbb66535a8b48bfbc` 配当前Web，在独立空库完成角色/迁移/bootstrap、非root只读、登录退出、SSE、上传拒绝与关闭门槛；未部署 |

### 性能阶段与失败保留

中间单例候选 `6ff9558e…` 的600000条、两平台三场景五组对照完整执行，
混合饱和中位吞吐WSL734.13→3137.42条/秒、辣椒763.08→3156.02条/秒；
该版本缺少单账号同速率延迟，不能作为当前最终验收。
下一版 `a924209f…` 补齐单账号同速率后，WSL含写回P95
2.00563→2.10533ms、排队106.755→584.15ms，失败已保留，
由此定位小领取仍读取每账号八项及重复恢复扫描的问题。

当前小领取候选的WSL单账号同速率诊断只执行两组，输入728.6909条/秒：
中位decode P95 0.533→0.188947ms，含写回P95 2.215768→0.848073ms，
排队P95 265.644329→3.115015ms。该诊断有改善，但不是五组两平台验收，
不能挑选这一结果宣布完成。

当前完整矩阵在基线第2次运行遇到`unexpected EOF`，168.11秒失败。
2026-10-31 08:13:10数据库checkpointer被SIGKILL，内部恢复后
08:13:20重新接受连接。共享Docker虚拟机约7.65GiB，swap接近满；
数据库cgroup的`max=0/oom=0`、`oom_kill=11`及日志与全局内存压力一致，
不能把该中断当成候选算法性能结果。数据库恢复期间API自动重启计数从0增至5，
日志包含恢复模式下连接失败；API原容器/镜像保留，当前API和数据库均健康，
未人工重启服务。该影响不能记成“运行完全未中断”。
随后只读检查没有queued/running/unknown原生操作或preparing/submitting/unknown
设置/媒体提交。没有更改配额、停止其他项目或重放未知操作。

测试驱动新增VM资源保护：启动前至少1536MiB可用，每两秒监测，
低于1024MiB、监测失败或PG OOM计数变化时仅停止本次唯一标记容器，
退出后复查；基线与候选规则相同。受保护重试仅完成一条10000样本基线，
下一次启动被内存准入拦截，矩阵不完整，不改判通过。
四项资源保护用例和七项性能汇总用例通过；原失败和`.guard.json`保留。
当前源码尚未完成新的30分钟持续负载；旧378400条通过属于另一产品摘要，
不能移用为当前候选准入证据。

主要私有证据：
`~/.cache/artwslbot/decoder-optimization-20261031-small-claims/`；
中间及失败目录`…-claim/`、`…-account-cursors/`、`…-singleton/`、`…-final/`
保留。当前微基准原始五轮与汇总`current-micro-summary.json`保留；
集合串行ns/op总和WSL164340→120217、辣椒60840→23785.9；
并行集合耗时分别下降21.84%/57.39%，不作为单条P95。
每类ns/op、B/op、allocs/op见私有原始日志，旧百分比不冒充当前完整链路结果。
最终汇总器对不完整五组证据明确拒绝，未生成可准入的完整性能报告；
当前源码没有新的完整30分钟持续负载，整体保持未通过。
共享VM内另有90个`tenant-acceptance-pg-*`容器，运行约28–31小时；
已保存只含ID/名称/创建时间/镜像/挂载元数据的`resource-review.json`供确认。
未确认其业务使用状态，不能仅凭年龄或CPU空闲判断可停，本轮未停止或删除这些容器。

### 上百群扩容结论

现实现的receiver全局advisory lock为798428，API内同时启动decoder；
ChangeBus及诊断环在进程内。因此增加API副本不会自动分摊原生账号连接，
还会增加数据库解码并发和连接数；跨实例实时通知尚未实现。
群数不能直接换算消息容量，300群授权上限也不是实测容量。
数据库瓶颈先优化查询/I/O；解析CPU饱和且数据库有余量时再拆独立解码角色；
连接处理瓶颈才按账号做唯一租约分片。同一平台原生身份始终一个连接所有者，
不能按群重复连接账号。100/300群分布、热点、通知、交接和固定速率长稳
仍需专门合成验收，真实群测试另按授权执行。

运行API仍 `c013d10ab9e43…` / Web `94ef924f66ab…`，
本地release、资源、凭据和迁移保留；本轮真实原生动作/登录/短信及业务模型调用0，
native_ready/ai_ready仍false。未执行远端CI、提交或推送。

## 平台登录与整体主题（前一轮记录，2026-10-31，Asia/Shanghai）

采用所有者指定 `web-design-templates` 技能中的 Voltline Analytics。
产品新增独立模板令牌/适配样式，移除框架演示全局 SCSS 和初始暗色偏好加载；
Vue、Element Plus、Element Plus X 及既有独立图表保留，没有新增依赖或迁移。
模板许可/原始设计文档保存在 `web/src/wsl/theme/voltline/`，
本机字体、既有平台图片与图标离线使用，没有远程字体请求。

平台登录为单列实际账号表单，标签与输入关联、浏览器自动填充用途保留，
空输入禁用、进行中重复提交拦截、错误重试和退出后密码清空已验证。
主导航/二级分类、顶栏、表单、表格、指标、弹窗、助手统一为浅色运营界面，
墨黑侧栏和青柠色操作；小圆角、零字距、表格信息密度保留。
移动导航隐藏时不参与焦点顺序，支持展开状态、关闭按钮与 Esc 返回入口。
侧栏同步状态改取实际刷新/错误/更新时间，不再固定宣称已连接。
业务权限、账号用途/平台、群范围、十轮会话和模型/原生门槛不变。

| 检查 | 本轮实测 |
| --- | --- |
| 产品构建 | vue-tsc / Vite 通过，2325 模块、最终 12.58 秒；CSS `index-DZErOzC9.css`、JS `index-D9npkcDp.js` |
| 浏览器 | 最终同进程十组全部通过：theme、console、assistant、account_dashboard、messages、multi_platform、gateway_inspection、platform_assistant、lajiaoliao_auth、native_settings |
| 屏宽与交互 | 320/390/647/1440，登录额外768；图表像素/边界、紧凑消息页、移动菜单、平台选择、登录错误/键盘/重复提交、弹窗与权限隔离通过 |
| Go race/sodium、vet | 当前产品 `./cmd/... ./internal/...` 通过；Go 源码未在本批修改，非数据库套件不代替数据库测试 |
| 串行隔离 PostgreSQL | HTTP 完整非race31.709秒、gateway完整race18.117秒通过；仅重建 `wsl_test`，保留上轮完整HTTP race未通过的历史结论 |
| 架构事实 | update/check、所有权/Compose、45个架构Python测试、3个渲染缓存测试、12个Compose测试、仓库/迁移守卫及格式检查通过；仅前端依赖视图随入口变化重绘 |
| 可信增量 | 225文件/9组件通过，当前源码 `20193c3fdcbc9d45425a5a0c083f5448b289c667db9ec29e414bf1bddcd066cd`，模型请求0、可信检查点已保存，无全量回退 |
| 容器 smoke | 新Web与当前已部署API配对，在独立空库通过角色/迁移/bootstrap、非root只读、登录退出、静态资源、SSE、上传拒绝与关闭门槛；未使用生产数据 |

修复过程保留：320px登录网格初次溢出、输入双焦点、移动消息顶栏高度已修正；
菜单关闭后退出可访问性树，因此更新工作台夹具先展开再定位。
辣椒登录夹具增加真实重载，避免沿用上一组页面的已认证主体。
新登录夹具的表达式格式错误已修正；Compose测试初次误用无yaml的系统Python，
改用已有锁定venv通过。这些中间失败未描述为产品验收通过。

Web `94ef924f66ab0c7672d18b900c898599a6193f965cd33c0a1899241eff328374`
已离线构建、通过 smoke，并用现有 `compose.yaml + compose.local.yaml`
执行 `up -d --no-deps --no-build web` 交付，保持release模式。
线上HTML/CSS/JS与已验收dist逐字节/摘要一致，Web/API均healthy、RestartCount0。
API容器仍为 `95ff555cddfb…`、镜像 `c013d10ab9e43…`，未重启；
API 1CPU/512MiB、Web 0.25CPU/128MiB保持，凭据/数据库/绑定配置不变。
上一版Web `8e11922f5cae…` 保留 `wsl-web:rollback-20261031-prevoltline`，
前端源码另有私有快照。当前解码器API候选仍未发布，原完整链路未达标结论不变。
私有证据 `~/.cache/artwslbot/voltline-ui-20261031/`，
最终十组日志 `ui-final.log`，原失败日志独立保留。
浏览器连接器无法绑定现有用户标签页，因此未重载该标签页或操作其草稿；
实际线上资产/health通过HTTP读取核对，用户刷新即可加载新主题。
真实原生动作/登录/短信、业务模型调用新增0；native_ready/ai_ready继续false。
未执行远端CI、提交或推送。

## 双平台解码器优化候选（2026-10-31，Asia/Shanghai）

当前为未部署候选。解析微基准达标，完整数据库五轮对比未达到整链路门槛；
持续负载首轮失败，夹具诊断修订后30分钟复查通过；整体性能准入仍失败，
不能称为整体优化完成。
原运行 API `c013d10ab9e43…` / Web `8e11922f5cae…` 保留；
CodecVersion=6、36 项迁移、原生及 AI readiness 均未改变。
基线冻结的是本轮开始时的工作区源码及运行镜像，而非缺少未提交改动的 HEAD。
私有证据目录 `~/.cache/artwslbot/decoder-optimization-20261030/`，
原冻结源码摘要 `a91c894b53391fb97b07cf93906e7b75d8a1597753e50e8d13bb98d541a835bf`。

旺商聊改为单遍固定槽及独立重复集合；同一 libsodium 八字节 nonce AEAD
改用分离标签原位解封，省去拼接与额外正文缓冲区，退出和失败均清零。
辣椒聊新增只复制投影字符串的单遍路径，复杂/未知/未覆盖线型从原输入回退
原 SDK，严格 JSON、UTF-8、覆盖语义及原有格式边界不变。
两平台旧实现仅作为测试差分参照保留。
完整设计与复现命令见 [解码器候选验收](decoder-optimization.md)。

四 worker、16 队列及独立四连接池不变，公平领取 SQL 未改。
取源和写回的机会式合批分别实现并测试，均最多四项、无定时凑批等待；
保留逐项账号/平台/epoch/版本/owner/租约/保留期栅栏，确认事务结束才通知。
早期数组 SQL 及后续驱动批处理的写回延迟/吞吐门槛未通过，
**两个产品合批常量均为 false**；试验开关只在测试驱动中存在。

`decode_ms` 仍含取源至投影密封、排除最终 UPDATE。
另有含写回完整服务、领取及排队观测，未搬动边界。
decoder-health 统一到工作台的最近 1000 条/最长 24 小时有效成功总体。
超管原折叠诊断新增分阶段 P95、计数、窗口及采样时间；
环最多 20 账号 × 1000 条，本进程重启清空，普通租户不返回该字段。

| 检查 | 本轮当前证据 |
| --- | --- |
| 产品 Go race/sodium | 最终 `GOMAXPROCS=2 go test -race -tags sodium ./cmd/... ./internal/...`通过；gateway 3.438 秒、httpapi 1.531 秒、nativewire 2.218 秒，未变包可命中缓存 |
| vet / 依赖 | 同产品范围 vet、`go mod verify`通过；未新增依赖或迁移 |
| 串行 PostgreSQL | 当前HTTP普通完整33.019秒通过；HTTP完整race两次失败，详见下文，席位/工作台专项race19.962秒通过。当前gateway完整race21.322秒通过，只重建`wsl_test`；JSON末次优化前HTTP曾90.133秒通过，不替代当前race失败 |
| 合批正确性 | 最终专项 2.294 秒通过；混批错误账号/平台/版本不借用成功结果，七项栅栏独立；后续语句约束失败整批回滚、超时不确认、到期租约由新 manager 恢复 |
| 差分 fuzz | WSL 认证/schema 576360 次、编码/签名 1580438 次、压缩边界 717918 次；辣椒聊原始 wire 657482 次；最终严格 JSON 快速拒绝另有118362次正文差分，五项均30秒并通过，总3650560次，无差异 |
| 前端构建 | 产品 vue-tsc / Vite 通过，2323 模块、12.03 秒；CSS `index-D2MJn53O.css` / JS `index-DHWuHX5a.js` |
| 合成浏览器 | account_dashboard、native_receiver、runtime_monitor、live_updates、console、multi_platform 六组通过；320/390/647/1440，接收另含654；图表像素/不溢出、阶段诊断、迟到隔离、租户隐藏通过，真实原生/模型调用0 |
| 架构事实 | update/check、所有权/Docker、仓库/不可变迁移守卫通过；45 项架构 Python、3 项渲染缓存、七图缓存摘要校验通过；只刷新证据/manifest，布局不变 |
| 可信模型增量 | 最终 225 文件/9 组件通过，1 请求、91356 保守预留/22751 报告 token、未知0；源码 `002fc1480280f129f3f4f0e933e095d08f5b776df519c360cf18f8da3259ff31`，检查点已保存，无全量回退 |
| 候选镜像 smoke | 独立空库首跑通过：迁移/bootstrap/RLS、非root只读、静态资源、登录退出、SSE、上传拒绝及默认关闭；未使用生产数据 |
| 最终数据库性能 | 两平台各完成混合饱和、固定输入速率、单账号饱和各五组交替基线/候选，每轮10000条；共600000条测量消息，逐轮计数/状态通过，整链路性能门槛失败，详见下表 |
| 持续负载 | 首轮280.25秒失败；诊断修订后短版300.63秒通过，完整用例1800.72秒通过，实际负载1800.6597秒、378400条、两平台8账号；分钟采样堆/RSS/协程有界，结束无积压，详见下文 |

候选 API `9c06c9ca3fc63972eb71913ad7185affd1a751324ba050c2a7299096d72a18a0` /
Web `492ebc624bf2c427757e731a487941eb928cc47c1b41ab8fc492a2f47d7146f5`
已构建并 smoke，未替换运行标签或重启本地服务。
构建使用锁定 vendor、既有构建/运行基础镜像及 `--network none`，
不声称完成上游基础镜像的联网干净重建。未执行远端 CI、提交或推送。

### 试验边界与失败保留

最终 HTTP 完整 race 重跑耗时445.698秒且并发席位用例收到transport_error/
status0；该用例的客户端固定超时未调整。独立席位8.08秒及账号工作台9.99秒
复现通过；安静条件下整套再次327.159秒并在同一用例失败，完整race子项仍未通过。
不能因专项通过就把当前完整HTTP race套件写为通过，也没有修改无关权限实现或放宽超时。
同期gateway完整21.322秒通过。架构守卫初次误用系统Python缺yaml，
改用仓库既有锁定venv通过，未修改依赖或跳过检查。
30分钟持续负载结束后串行补跑完整HTTP普通数据库测试，
`go test -p 1 -tags sodium ./internal/httpapi -count=1 -timeout=15m`通过33.019秒，
含离线编译的容器墙钟57.675秒；1 CPU / 1 GiB、GOMAXPROCS=2、
GOMEMLIMIT=512MiB，明确启用`WSL_INTEGRATION=1`且只重建`wsl_test`。
该普通完整结果与前述完整race失败分别记录，不修改产品权限或客户端超时。

短微基准在 Docker 1 CPU / 512 MiB / GOMAXPROCS=2 下出现后段数倍波动。
只读检查发现该 VM 总内存约 7.65 GiB、可用约 622 MiB，1 GiB swap 几乎用满；
这是并存压力观测，不证明波动原因。没有停止其他项目容器或提高现有资源限制。
这些原始日志保留为 `*-noisy-short-micro.log`，不挑选其中最快一轮。
最终微基准采用每类500ms、五轮交替基线/候选先后顺序。

早期热点分配遗漏一个账号的问题已修复，最终四账号流量为热点50%及其他三账号
各约1/6；增加相同1000条预热，预热数据不进入测量。更换候选代码时旧轮次改名为
`prebuffer` / `prepipeline` 保留，不与最终指纹混算。性能驱动记录每次二进制摘要，
汇总拒绝混版本、缺轮及少于万条的数据；后续补充持续负载失败留存、
短版/失败结果拒绝及源码关联检查，目前9项工具回归通过。
并行 ns/op 只解释为并行吞吐，不解释为单条 P95。千条观测或30分钟合成负载
都不能代替全天原生稳定性验收。

### 最终解析微基准

Linux/arm64、同一 `wsl-test:local`、1 CPU / 512 MiB、GOMAXPROCS=2，
每类500ms、五轮交替先后顺序。集合按每种有效类各运行一次后相加，
旺商聊20类（十种有效正文×压缩/不压缩），辣椒聊5类；
未知/未支持/畸形类单列回归检查，不混入有效集合提速。
五轮中位数形成的集合结果如下，不能解释为单条P95：

| 平台 | 集合耗时基线→候选 ns | 分配字节基线→候选 | 分配次数基线→候选 |
| --- | --- | --- | --- |
| 旺商聊 | 312530→230872，下降26.13% | 201952→121630，下降39.77% | 852→438，下降48.59% |
| 辣椒聊 | 135689→51438，下降62.09% | 71670→18147，下降74.68% | 117→61，下降47.86% |

| 串行单类中位数 | 基线 ns/op / B/op / allocs/op | 候选 ns/op / B/op / allocs/op |
| --- | --- | --- |
| 旺商聊短文本，压缩 | 6306 / 2984 / 28 | 3929 / 1520 / 14 |
| 旺商聊长文本，压缩 | 26033 / 36080 / 28 | 26050 / 34296 / 14 |
| 辣椒聊短文本 | 3297 / 1152 / 20 | 1461 / 192 / 10 |
| 辣椒聊复杂嵌套回退 | 3987 / 1584 / 27 | 3513 / 968 / 19 |

有效集合的两项20%门槛与分配次数门槛通过，最终串行及并行完整矩阵未检出
“至少4/5轮且中位数均超过5%”的稳定单类退化。较早版本的畸形JSON退化
通过只拒绝确定未知键的快速失败修正；重复、转义及大小写content仍按旧规则处理。
每项完整原始值在 `nativewire-micro.log` / `gateway-micro.log`，工具摘要在
`summary.json`。这组主机有明显并存压力，绝对耗时不可推广为其他部署容量。
微基准之后只在测试驱动增加领取输出，产品源码未变；数据库和持续负载另记
各自可执行文件及产品源码摘要，不把测试驱动元数据变化冒充产品变更。

### 最终数据库五轮

每平台每场景五组交替基线/候选，均为四worker、16项队列、四连接池，
1 CPU / 512 MiB测试容器和既有1 CPU / 1 GiB PostgreSQL。
每次相同1000条预热不进入测量，热点占50%，其余三个账号均有流量。
取源/写回合批均关闭，公平领取SQL和原计时边界未变。
数据库矩阵用共同驱动调用`claim`/`process`，不包含原生网络、接收落盘及ACK。
驱动的排队观测来自外部计划/入队时刻；手动worker不设置`queuedAt`，
所以此矩阵阶段诊断中的queue=0不能解释为没有排队，排队以独立观测为准。
新增阶段打点开销计入候选，没有从服务计时中扣除；
外部驱动、预热和资源配置在基线/候选间一致。
下表为五轮结果的中位数；不能从中推断原生生产容量或全天稳定性。

| 平台/场景 | 吞吐 基线→候选 条/秒 | decode P95 基线→候选 ms | 含写回服务 P95 基线→候选 ms |
| --- | --- | --- | --- |
| 旺商聊/混合饱和 | 405.44→422.99，+4.33% | 1.06005→1.55648 | 45.42615→8.41334 |
| 旺商聊/固定283.8069条/秒 | 185.31→172.08 | 4.89545→6.05843 | 50.23791→33.43404 |
| 旺商聊/单账号饱和 | 311.53→346.80，+11.32% | 5.00860→4.83876 | 58.16341→39.12400 |
| 辣椒聊/混合饱和 | 217.27→213.25，-1.85% | 5.87045→6.06171 | 58.48801→57.76368 |
| 辣椒聊/固定152.0904条/秒 | 149.64→151.94 | 5.00415→5.59044 | 32.11538→36.07850 |
| 辣椒聊/单账号饱和 | 562.05→469.36，-16.49% | 1.96900→2.54921 | 43.85676→55.95918 |

两个固定输入速率分别按本平台前述基线饱和容量中位数的70%设定；
旺商聊后续多数轮次已不能跟上这个名义速率，不能把该场景称为稳态低负载。
其排队P95中位数16666.56→19022.58ms，增加14.14%；
辣椒聊714.32→573.23ms。该机器并存压力明显、轮间波动很大，
这些证据无法证明回归控制达标，也不能将波动全部归因于解析代码。
没有删除失败轮次、下调目标、改变资源限制或挑选较快子集。

跨五轮四账号的阶段P95再取中位数，仅用于定位：
旺商聊协议约0.039755ms、取源1.187080ms、写回23.614051ms；
辣椒聊协议约0.016923ms、取源5.264795ms、写回54.733936ms。
这不是合并总体P95，阶段分位数不能相加。完整总体仍以原始观测计算。
数据库往返/领取等待远大于解析阶段，现有证据不支持仅凭微基准提速就发布。

机会式合批各自的万条单组试跑亦保留：旺商聊取源吞吐312.75→266.11，
写回740.52→652.76条/秒且服务P95增加；
辣椒聊取源592.67→773.10、写回613.12→877.18条/秒，
但写回服务P95仅3.43460→3.21741ms，未达到15%下降。
单组不是五组验收，辣椒聊取源一次改善不能据此启用。
两个产品常量继续关闭，整体性能结果为`performance_accepted=false`。

最终数据库基线二进制`25771e24a256…`，候选`58cc4b17dacd…`；
候选产品源码摘要`2f3e4ff3cb80c52af536324d0d65cf26668e214c4df174bd7924ac755843de2d`。
各场景完整记录、环境前后压力及阶段聚合均留在原私有目录。

### 持续负载首轮与复查

首轮请求1800秒，280.25秒时报告`60488/62000`未完成而失败。
前一采样242.38秒完成55600条，队列0、GC后堆2312856字节、
RSS15286272字节、10个协程；这些只说明当时采样，不能排除随后异常。
原夹具在每平台1000条的周期突发后固定15秒排空，
失败时未记录持久状态分布，因此既不能宣布丢失，也不能断言只是数据库慢。
首轮`soak.log`和`soak-outcome.json`完整保留，不替换为后续通过。

仅修订测试夹具：排空上限60秒，超时时同时输出decoded/processing/failed计数、
队列及manager快照；数据计数、状态一致、资源边界和1800秒验收下限不变。
持续负载运行实际`DecoderManager.Run`，每周期注入两平台各100条、
周期性各1000条突发，等待该批排空才进入下周期，具有背压；
它不是固定到达率或饱和持续负载，瞬时资源峰值也不能由分钟采样代替。
后续短版运行300.63秒、实际负载300.47秒，65000条均逐批核对成功，
结束无积压、8个合成账号覆盖两平台，采样最大GC后堆2353296字节、
RSS22441984字节，未超过资源门槛；原生动作0。
短版不替代完整验收。
复查二进制`49338640eefd…`与数据库候选具有相同产品源码摘要，
差异仅在测试夹具，未修改产品逻辑或重建/重启运行服务。
复查证据位于`~/.cache/artwslbot/decoder-optimization-20261031-soakfix/`。

后续完整复查`soak-full.log`/`soak-full-outcome.json`通过，用例1800.72秒，
实际负载1800.659675612秒，共378400条（每平台189200条）逐批核对成功。
30次分钟采样的最大GC后堆2381592字节、RSS23486464字节，协程9–10，
各次排空后采样queue=0。结束时active=0、queue=0、queue_bytes=0，
四worker及16容量不变，停止检查通过；八账号各保留1000条阶段观测，
无新原生动作、登录、短信或模型调用。
这些最大值是采样最大值，不是整个过程的瞬时峰值，也不是固定到达率长稳。
汇总显式选择该复查目录及`full`标签，产品源码摘要与数据库所有场景一致；
`soak.complete=true`、`source_matches_database=true`，
但`performance_accepted=false`，整链路未达项不会被持续负载通过抵消。
末次只读核对：API/Web仍为本节开头原运行镜像，资源限制保持，
均healthy、RestartCount0；健康接口仍返回两readiness=false。
末次架构免费preflight仍匹配`002fc1480280…`、225文件，0模型请求，
没有把预检当成新的模型分析，也没有覆盖此前通过的可信增量检查点。

## 账号工作台图表、千条采样及总览 P95（2026-10-30 22:30，Asia/Shanghai）

账号主视图改为授权群、60秒吞吐、解码P95、待解码/操作四项指标，
并列15分钟吞吐柱图与解码直方图，显示P50/P95/P99、实际样本数和覆盖时段。
低频接收/HTTP/业务诊断折叠，阻断时自动展开。超管吞吐改用真实去重原生入库；
租户仍只看其授权群业务入库，不暴露共享账号的原生消息或解码观测。
复用现有Art Design Pro柱图及Element Plus，未增加前端依赖。

解码统计改为最近1000条有效成功观测、最长24小时，独立于原接收提交/ACK的
60秒8192样本窗口及全局worker窗口。数据来自既有15秒共享监测，
不在消息提交/ACK链路增加查询。总览从当前工作台各账号原始耗时中选择最新1000条
计算插值P95，绝不平均账号P95；不补造样本，非有限/负数/未来/过期观测拒绝。
直方图与分位数共用相同总体，原始事件ID和耗时点不进入API/分钟历史。
数据库失败或超过45秒的观测标记不可用，保留原采样时间。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go race/sodium | 最终`GOMAXPROCS=2 go test -race -tags sodium ./cmd/... ./internal/...`通过；httpapi 2.537秒、nativewire 2.988秒，未改包可用缓存 |
| Go vet / 格式 | 相同产品范围vet通过；本轮Go文件gofmt检查无输出 |
| HTTP PostgreSQL | 完整非race套件29.916秒通过；最终采样元数据修订后账号/运行专项race7.158秒通过。1500条持久合成观测只取最新1000，工作台/账号/租户隔离通过 |
| gateway PostgreSQL | 完整race17.893秒通过；租约恢复、公平调度及取消回归保留。数据库套件串行且只重建`wsl_test` |
| 分位数 | 单元覆盖1000上限、24小时开边界、未来/负数/NaN/Infinity、空值、直方图总体一致；990条1ms加10条100ms的总P95仍为1ms，证明没有平均账号P95 |
| 协议回归 | 加密/压缩/媒体/路由/重复单字段拒绝、重复集合不覆盖邻字段、1024字段上限及严格Base64URL位/换行/填充边界通过 |
| 产品构建 | vue-tsc / Vite通过；2323模块、12.05秒，CSS `index-CAfIfCv6.css` / JS `index-DkoSDgac.js` |
| 合成浏览器 | account_dashboard、native_receiver、runtime_monitor、live_updates、console、multi_platform六组通过；320/390/647/1440宽度，接收测试另含654；双图像素非空、无溢出，旧账号迟到响应丢弃 |
| 恢复与权限 | 默认折叠、阻断自动展开、版本化停接收后重新登录、停用失败不打开登录、同原键未知核对、撤权取消订阅均通过；所有原生/分配/登录写入为拦截的合成数据 |
| 架构 | update/check、所有权/Docker与仓库/迁移守卫通过；45项Python、3项渲染缓存、12项Compose测试通过；只更新frontend布局，7张Mermaid验证通过 |
| 容器验收 | 最终API/Web使用独立全新临时数据库一次通过：迁移/bootstrap/RLS、只读非root、登录退出、SSE、静态资源、上传拒绝边界、默认关闭及两readiness false |

浏览器首跑发现旧脚本等待已更名的全局标题，并把“延迟 · 样本数”整段误写成
单独数值的exact断言；修复清理阶段等待及完整值断言后，六组完整重跑通过，
未放宽数值、权限或业务约束。本轮容器smoke首跑即通过；历史上传代理偶发502
仍是历史未根治项，不因这次通过宣称修复。

### 解析效率实测边界

本机Apple M4 / darwin arm64 / GOMAXPROCS=2，现有
`BenchmarkOpenMessage`的合成加密压缩消息、五次运行：
原基线1094–1161 ns/op，中位1122；最终安静复测1086–1181 ns/op，
中位1112，耗时基本持平。分配由51次/4640字节降为36次/4096字节，
分别减少29.4%和11.7%。优化为有界字段值连续分配、复用已解析信封、
去除严格Base64验证后的重复编码；重复字段仍保留独立容量边界。
构建/容器等并发负载时另次测得1120–2408 ns/op，不用于宣称提速。
这不是整体worker吞吐或8核目标机长稳验收。

只读EXPLAIN ANALYZE验证当前5账号的千条观测聚合使用既有
`native_monitor_decode_window`索引、每账号LIMIT1000，执行7.107ms；
这是该查询一次观测，不是整个监控轮次或原生处理吞吐。

### 模型与本地交付

CodeBoarding两次默认增量均通过并保存221文件/9组件可信检查点：
22:09:15为1请求/90102保守预留/22590报告token，
22:21:24为1请求/90117保守预留/23666报告token，均未知0。
最终源码指纹`2466186ace110a508e10d6cbef43cb731bd5ee618cc8dbdded62fdc3e6084375`。
沿用兼容基线及独立架构凭据，未全量初始化、抬高预算或外发运行数据。
未执行远端CI、提交或推送。

本轮已同步原release Compose的API/Web；36项迁移完全不变，无迁移、清库或新服务。
账号身份/epoch/版本、绑定、密封凭据、网关/助手配置、环境及CPU/内存/挂载
核验保持；PostgreSQL/heatbot/Simon容器未重建，两新容器healthy且RestartCount0。

- API：`c013d10ab9e43f6dfaadc3fde8b810bfbd246a51d1785ecadfeb46b8bd1953df`。
- Web：`8e11922f5cae22d7bfa0a357de44424eee649bfad6cdf431701ed7e784a9dec6`。
- 紧邻前版API `69b31138ec91…` / Web `8c5efb017976…`保留为
  `rollback-20261030-preworkbenchperformance`；资源仍为API 1CPU/512MiB、
  Web 0.25CPU/128MiB。
- 22:24只读实测：WSL总P95为1.74725ms/1000条，覆盖21:30:00–22:23:50，
  两账号分别各1000条；API值与相同截止时点的PostgreSQL `percentile_cont`相符。
  辣椒聊两账号、热群一账号为0有效样本/P95 null，未借用其他工作台数据。
- 两个已启用WSL接收均connected/observed。实际浏览器总览P95及账号双图、
  千条覆盖均可见，诊断默认折叠；网页会话操作仅查询和网站登录/退出。
  本轮真实原生动作、原生登录/短信和业务模型调用新增均0。
- 两readiness仍false。千条覆盖约54分钟，只能说明这段观测；不等于日常全天稳定性，
  未执行24小时持续验收，也不将跨版本历史耗时归因于本轮解析器优化。

私有冻结源码、镜像/交付/只读比对及实际截图：
`~/.cache/artwslbot/workbench-performance-20261030/`；
`account-live-full.png`、`overview-live.png`。合成四宽度截图位于
`output/playwright/account-dashboard-*.png`，不包含真实消息正文。

## 会话新建、首问标题及机器人边界修正（2026-10-30 21:38，Asia/Shanghai）

### 本轮行为

- 仅助手会话页移除重复的page-heading及全局验收条，保留其他页面状态。
- “新对话”立即建立并选中空白会话；相同网站用户与固定范围由事务锁复用
  唯一空白会话，并与第一回合写入串行。重复点击当前空白会话保留未发送草稿。
  历史版本的重复空白只展示最新一条，按原到期时间保留，不破坏性清库。
- 历史标题改为首次提问的前32个Unicode字符，折叠空白，较长追加省略号。
  前向迁移036仅加可空密封标题及授权指纹；标题不受十轮正文清理影响，
  但权限变化后不可披露，随会话七天到期。001–035哈希与既有记录保留。
- 名称明确为Web群管助手、超管只读助手、群内热群机器人。群内热群开关、
  冷却和聊天人设不控制Web助手。机器人面板/群绑定/人设编辑优先原生昵称，
  不同网站备注并列；人设编辑显示平台及原生ID。未修改原生昵称或增加模型工具。

### 实测结果

| 检查 | 当前候选实际结果 |
| --- | --- |
| Go race/sodium及vet | 产品`./cmd/... ./internal/...`通过；control 2.069秒、httpapi 1.772秒，未变包可命中缓存；vet通过 |
| 助手 PostgreSQL race | 9.558秒通过；8个并发新建同一空白ID，首问中文/表情标题密封，11轮后仍为首问标题；权限变化隐藏标题，跨成员/租户与已有审批/预算/十轮保留场景通过 |
| 完整HTTP PostgreSQL | 非race `-p 1 -tags sodium ./internal/httpapi -count=1` 31.522秒通过；本轮未将完整数据库套件描述为race通过 |
| gateway PostgreSQL race | 串行18.001秒通过；数据库回归只重建`wsl_test` |
| 前端最终构建 | vue-tsc / Vite通过，2316模块、12.31秒，CSS `index-CIEAEwv1.css` / JS `index-CCeKBOW2.js` |
| 浏览器 | assistant、native_settings、multi_platform、console、gateway_inspection、platform_assistant、commercial七组通过，320/390/647/1440；最后草稿小修后assistant再次通过 |
| 会话UI专项 | 新建立即展示、重复新建只有一条空白、草稿保留、首问标题与历史恢复、连续11轮只留10轮、不重放模型/审批、切群迟到隔离、原生昵称、人设原生ID、群内/Web边界与无重复页头全部通过 |
| 其他UI | 账号设置10次合成提交/8步安全流程、跨平台与账号目的池、租户/超管隔离、网关未知调用不重放及商业页面回归通过；真实原生请求和真实业务模型调用均为0 |
| 架构事实与工具 | update/check、架构/仓库/迁移守卫、git diff空白检查通过；45项架构Python测试、3项渲染缓存、12项Compose测试通过；只更新frontend依赖布局，其余6视图保持，7张Mermaid最终均验证通过 |
| 容器smoke | 最终API/Web镜像使用全新临时数据库完成迁移、bootstrap、RLS、非root/只读、静态资源、SSE、上传边界及默认关闭检查；不使用业务数据 |

初次浏览器回归有三处测试断言需适配：首问标题不能被当成已经清理的正文、
工作台按钮实际名称、原生昵称加入两个资料对话框标题。修正后重跑通过，
未修改权限或放宽业务断言。容器smoke首跑媒体上传拒绝边界出现502，
API仍运行、非OOM、代理记录Connection reset by peer；同一最终镜像完整复跑
返回预期409并全部通过。该早拒绝/上传连接重置在历史批次也有记录，
本轮未复现其原因、未改代理、未加自动重试、未将其描述为已根治。

### 建议性模型分析

本轮CodeBoarding默认增量已恢复通过：21:24:48分析219文件/9组件，
1次上游请求、89774保守预留、25205已报告token、未知0，保存兼容可信检查点。
最后空白会话草稿修正后，21:30:44再次完成增量验证与检查点更新，
本次无需新上游请求，最终源码指纹
`b08abec8a6e398a259caed09569a054736544d6db701f39e80afd34fcb865146`。
21:34:34免费preflight为baseline_compatible。未全量初始化、提高预算或
复用业务密钥；前一批失败记录仍保留。代码事实检查、模型建议和上线验收分别记账。
未运行远端CI、提交或推送。

### 本地交付与实际页面

仅同步现有release Compose中的API/Web，前向迁移036，前35条迁移记录完整保留。
账号身份/epoch/版本、绑定、凭据、助手/网关设置、环境摘要及本地资源限制核对一致，
PostgreSQL、heatbot、Simon容器未重建；native_ready及ai_ready继续false。

- API：`69b31138ec91a603955ee0764196f482eb6b51663adbcf7cb98290c653e05ead`。
- Web：`8c5efb017976504801b5e2fe1634343e08162e60ec83c7d437e8a46fb24a4b2b`。
- 紧邻上一版API `7b910297e139…`、Web `e28631b0b4a9…`保留为
  `rollback-20261030-preassistantconversations`，兼容前向表，可按镜像回滚。
- 对唯一“测试群”实际连发4次空白会话POST，返回同一ID，历史只展示1条空白；
  两个账号均有采集原生昵称。可见回合0，业务模型调用0，真实原生动作0。
- 实际浏览器选择测试群、填入本轮临时草稿、再次点击新对话，草稿保留；
  随后清除本轮草稿。页面无重复标题，历史弹层只显示一条“新对话”。
  两个已开启WSL接收均connected；网关关闭/未核价/预算0和超管预览配置保持。
  首问标题及真实生成在生产数据上未试发，标题行为由合成浏览器与隔离库验证。

私有镜像/运行/验收记录和实际截图位于
`~/.cache/artwslbot/assistant-conversation-fixes-20261030/`；截图
`assistant-live.png`。本轮没有原生登录、短信、群发、真实模型回合或替用户审批。

## 群管助手一键选群与最近十轮会话（2026-10-30，Asia/Shanghai）

按用户要求，将群管助手改为已授权群直接选择、连续问答及有界保留。
本轮“10条”按每个对话最近10轮问答实现，每轮包含输入、回复及固定工具卡片。
不会自动启用模型、扩展助手工具、发起原生群动作或替用户审批配置。

### 实现与验证

- 使用固定版本 `vue-element-plus-x@2.0.3` 的 Bubble / Conversations，保留
  Art Design Pro / Element Plus 布局及原 Cookie/CSRF/Origin/SSE 请求边界。
  模型正文纯文本显示，不引入富文本发送器、附件或上游请求 hooks。
- 顶部跨已授权 WSL 账号显示群按钮；输入包含完整群名时候选排前。
  工具群列表中的按钮必须与当前授权列表的群ID、绑定账号同时匹配；
  模型伪造群无按钮。选择后恢复本人该固定群范围最近会话，不依赖全局账号下拉框。
- 新对话先清空本地，首次发送才创建会话。已完成历史直接读取快照，
  执行中从持久事件游标续订；切群取消读取并拒绝迟到结果，草稿按范围隔离。
- 前向迁移035新增可见历史标记/索引。新回合与分钟保留任务清除溢出终态回合
  的密封输入和事件，运行回合保留；回合编号、输入哈希、幂等键和终态仍在，
  不重置每日额度。费用台账、独立计划和审批执行凭证不随滚动正文删除。
  001–034迁移未改，035哈希已进入迁移锁。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go race/sodium | 产品 `./cmd/... ./internal/...` 通过；最后后端小修后control/httpapi再跑通过，HTTP 1.904秒，未变包可使用缓存 |
| Go vet | 产品范围通过；最后control/httpapi复核通过 |
| 助手专项 PostgreSQL race | 7.270秒通过，内含滚动保留子用例0.86秒：13回合仅10轮可见、旧正文/事件删除、旧键不重放、改正文旧键409、旧正文410、60个增量完整恢复、跨租户/同租户不同成员拒绝、权限指纹失效、运行回合保留、财务/审批证据不变 |
| 完整 HTTP PostgreSQL | `test -p 1 -tags sodium ./internal/httpapi -count=1` 26.494秒通过；此项最终未启用race，race完整运行的限制另列 |
| 串行 gateway PostgreSQL race | 17.908秒通过；所有数据库回归仅重建 `wsl_test`，未重置业务库 |
| 产品构建 | 最终vue-tsc/Vite通过，2315模块、11.29秒；CSS `index-BKUcVEvn.css`，JS `index-BKWR8_mq.js` |
| Chromium合成回归 | assistant、platform_assistant、gateway_inspection三组均通过，320/390/647/1440；助手覆盖12个合成POST、连续11轮保留10轮、一次合成审批、跨账号按钮、历史不重放、迟到历史不串群、伪造群拒绝与纯文本XSS |
| 实际布局 | 核对手机工作台和桌面抽屉截图；修复隐藏抽屉自动高度0及缩屏宽度过渡溢出；单个群按钮不再铺满宽屏整行 |
| 架构事实/工具 | update/check、所有权/仓库/迁移守卫与空白检查通过；45项架构工具、3项渲染缓存、12项Compose测试通过；更新frontend依赖图及源码证据，7张Mermaid验证通过 |
| 最终镜像smoke | 最终API/Web组合在全新隔离数据库完成35迁移、bootstrap、非root/只读、静态资源、登录退出、代理及默认关闭检查；未使用业务数据 |

完整HTTP数据库race两次运行在资源限制下被 `signal: killed` 终止；
第一次在被终止前还暴露了新增测试对最新回合保留的错误预期，已修正。
最终完整数据库套件以非race通过，助手专项与gateway数据库以race通过；
不得将本轮完整HTTP PostgreSQL race描述为通过。

**建议性模型分析未完成：**本轮默认CodeBoarding增量运行失败，首次上游502
后未知用量保守占用，后续请求触及本轮300,000保守预留上限而被relay拒绝。
3个实际上游请求、274603保守预留、44093已报告token、未知1；
`attempt-yacc9qpq`及旧可信基线保留，未提高预算、初始化或回退全量。
该失败发生在中间源码 `425bc0610771…`，不能当作最终修订分析通过。
最终20:15:41免费preflight为 `baseline_compatible`，217文件，
源码指纹 `2259231a868d206d0bfd13c10b3932349ec57ea51b15da3dde748140170ab6b0`；
它只证明可继续增量，不代表分析完成。业务AI调用与此独立计数。
未执行远端CI、提交或推送。

### 本地交付

现有 `wsl-platform` / `compose.yaml + compose.local.yaml` 保持release模式。
先同步API/Web并前向迁移035，原34迁移记录完整保留，账号身份、epoch、
绑定、凭据、助手/网关设置及本地资源限制核对一致；PostgreSQL/heatbot/Simon
均未重建。最后单个群按钮布局微调只更换Web，API容器保持
`7ef2e6313167…`，没有再次中断原生连接。

- 最终API：`7b910297e1399343c2d23b52b55224e2df6637584b96b98818bff9e6ade17998`。
- 最终Web：`e28631b0b4a915ff7965b1b7b1c3397884219a2d54939b1900631dbeefa67b17`。
- 本批前一运行API/Web `f9c10e3f7397…` / `5bfd780c2626…`保留
  `rollback-20261030-preassistantgroups`；Web紧邻微调前 `565e1a67df81…`
  另保留 `rollback-20261030-preassistantbuttonsize`。

Web微调的交付辅助校验先后误判Compose自动替换标签和挂载列表顺序，
均自动只回退Web。修正为忽略非配置的替换记录、按挂载目标稳定排序后最终交付
通过；不是业务配置变化，未停用或放宽产品权限/提交校验。

交付后只读API核对当前WSL视图2账号、1授权群、3个既有群会话均为空正文，
新快照路由成功返回；两WSL接收均connected。实机点击“测试群”选中并恢复历史。
没有新会话/回合POST、真实群动作、业务登录/短信或业务付费模型调用。
共享网关仍关闭、未核价、平台/测试预算0；USD口径与独立超管5积分月预算保持。
`native_ready`、`ai_ready`继续false，真实生成验收不因UI交付提高。
私有交付记录和实机截图在 `~/.cache/artwslbot/assistant-group-chat-20261030/`。

## USD 记账口径确认与网关回归（2026-10-30，真实采样18:50:05，Asia/Shanghai）

用户确认账面 USD 按 1:1 USDT 记账。本批已实现并保存当前业务路由的
显式口径，未把该确认当作缺失报价数字、缓存价格、市场汇率或新增月预算。

### 实现与检查

- `usd_usdt_parity`随业务来源/模型/密钥密封保存在不可变费率快照，
  保存产生新rate_id并记录超管审计；旧快照默认未确认，原运输模型字段兼容。
  查询不能临时注入此口径，普通租户不能修改，旧rate_id查询被拒绝。
- 明确USDT、或已确认口径的USD钱包可向下计算参考积分上限；只有完整、
  唯一且单位明确的报价才向上生成费率草稿。缺缓存价仍阻塞，CNY仍不换算。
  页面显示原币余额、参考上限、当前口径及独立确认框；不会自动核价或增预算。

| 检查 | 本批实际结果 |
| --- | --- |
| Go race/sodium及vet | `./cmd/... ./internal/...`通过；修正新增测试SQL后HTTP纯测试再跑2.004秒通过 |
| Docker/Linux HTTP PostgreSQL完整回归 | 最终串行95.658秒通过；只重建`wsl_test`，容器1GiB、`GOMEMLIMIT=512MiB` |
| 串行gateway PostgreSQL回归 | 18.040秒通过；仍仅`wsl_test` |
| 产品类型检查/构建 | 2278模块、12.59秒通过；CSS `index-I0Jv50DK.css` / JS `index-DtJh9mC7.js` |
| 三组Chromium合成回归 | gateway_inspection、platform_assistant、assistant均通过；320/390/647/1440，外部请求阻断，核对手机截图 |
| 架构与仓库 | update/check、边界/仓库/迁移锁/空白检查，45工具、3渲染缓存、12Compose测试通过；只更新图证据/manifest，7视图布局与渲染缓存不变 |
| 最终候选镜像smoke | 新临时数据库34迁移、受限非root/只读、静态资源、网站登录/登出及默认关闭检查通过；未使用生产数据 |

初次数据库回归发现新增测试断言的UUID/text参数类型不一致，已改显式转换；
一次与镜像构建重叠的重跑被资源限制终止，未作为通过证据。最终停止并行构建后
串行重跑完整套件通过。浏览器首次直接操作Element Plus隐藏checkbox超时，
改为点击可见标签后回归通过，未用强制点击隐藏问题。

**建议性模型分析：**当前可信命名空间默认增量通过，215源码文件/9组件；
1请求、89572保守预留、23480已报告token、未知0，18:41:24持久检查点保存。
源码指纹`2617d9dd85de4d076088107ecaa4fd61208dc9bf966e1d34830e23ab7421b08b`；
18:51:31再做preflight仍兼容，未全量初始化或替换基线。分析使用独立架构凭据，
与下述业务调用分开计数。未执行远端CI、提交或推送。

### 本地交付与真实核对

原Compose release已更新API `f9c10e3f7397…` / Web `5bfd780c2626…`，
仅重建API/Web。34迁移记录完全不变；凭据、模式、资源、账号身份与开关保持。
紧邻上一版API `f6877a949278…` / Web `43f45c4de9c6…`保留
`rollback-20261030-preusdparity`标签。PostgreSQL/heatbot/Simon未重建。

通过现有管理API保留密封业务密钥并保存`usd_usdt_parity=true`；只读核对
`ai_gateway_configured`和`ai_upstream_inspected`审计均记录此口径。
18:50:05固定查询返回钱包`53.22706947 USD`，参考上限`53,227.06947`积分；
这是采样值，不是本地预算。目标模型已列出，价格接口仍为CNY/百万token且
无目标模型报价，`mapping_status=model_price_missing`，没有费率草稿。

本地平台/超管测试月预算均0，网关与路由关闭，四项费率仍未核验；
单次测试提交被409拒绝，生产业务`ai_calls`仍0条。独立超管只读预览及其
5积分月预算保持；两WSL接收读回均connected。原生动作/业务登录/短信0，
`native_ready`/`ai_ready`仍false。**真实生成仍未验收通过**，剩余资料是
该模型完整价格的列标题、计价单位及四类token价格，不再要求重复确认USD口径。
私有交付/API证据及页面截图位于`~/.cache/artwslbot/gateway-currency-20261030/`。

## 超管只读助手启用与隔离验收（2026-10-30，页面采样18:20，Asia/Shanghai）

按用户“启用测试”交付独立超管只读入口；接收补充的供应商充值记账口径
“CNY 1:1 USD”和模型统计行。该行缺少列标题/计价单位及缓存价格，
不能认定 `$0.019/$0.313` 是每百万token费率；未据此核验费率或放开付费生成。

### 实现与隔离

- 新增034前向迁移：独立平台设置、会话和回合表，强制RLS，租户无表权限；
  `platform_assistant`固定用途，无租户/群参数，不使用群管会话或其工具。
  正文以actor/session/turn密封，24小时有效；每人20会话、每会话20问。
- 只向模型提供固定`platform_health`的计数及开关；无账号姓名、租户正文、
  附件、凭据、配置体或可调用工具。每问独立快照，不拼接之前回合。
  一次模型请求、512输出token、45秒、单次预留最多5积分，无自动重试。
- 独立自然月预算仍受共享平台总预算限制；不扣租户钱包或超管测试预算。
  预留及提交再次检查当前网站超管、有效会话、消费者设置版本及取消状态。
  运行中/未知不能换键重发；取消/撤权/过期中断网络等待，未知用量继续冻结。
- 浏览器发现并修复预算框初始`aria-disabled`残留和页面底部无关记录表。
  传输失败保留待核对编号，刷新仅查询原记录，不自动重放。

### 本批实际结果

| 检查 | 结果 |
| --- | --- |
| Go race/sodium、相同产品目录vet | 通过；部分缓存命中 |
| Docker/Linux HTTP PostgreSQL完整回归 | 82.284秒通过；包含普通用户/其他超管拒绝、群管/平台ID隔离、独立预算、幂等、并发、取消冻结、过期、撤销登录、设置变化和恢复 |
| 串行gateway PostgreSQL回归 | 17.912秒通过；两组只重建`wsl_test` |
| 产品类型检查/构建 | 通过；2278模块，12.78秒；CSS `index-KmoVMXLM.css` / JS `index-C-oRF1Cb.js` |
| 三组Chromium回归 | platform_assistant、gateway_inspection、assistant通过；320/390/647/1440，全部合成API，外部请求阻断；核对手机截图 |
| 架构工具/边界/仓库/迁移锁 | 45工具、3渲染缓存、12Compose测试、update/check及边界/仓库/空白检查通过；仅frontend和agents-approval布局改变，7视图渲染成功 |
| 最终镜像容器smoke | 新临时数据库34迁移、受限非root/只读、静态资源、登录/登出、平台助手默认关闭及RLS检查通过，两readiness保持false |

**建议性模型分析单独记录：**同一可信命名空间默认增量通过；首次2请求、
193944保守预留、49573已报告token，无未知用量。最终UI修复后增量0请求，
215源码文件、9组件，指纹
`36c85a2f989529ab5e62eb0c58cf8b0406eefe9025ca2c72a10df6884bd7f91e`；
18:17:14持久检查点保存。未全量初始化、切换基线或复用业务凭据。
未执行远端CI、提交或推送。

**本地交付与真实API检查：**API `f6877a949278…`、Web `43f45c4de9c6…`
已交付原Compose release，仅重建API/Web；前33个迁移记录完整保留。
紧邻上一版API `d82902b7034f…`、Web `132bab1a8b86…`保留
`rollback-20261030-preplatformassistant`标签。PostgreSQL/heatbot/Simon未重建；
凭据、模式、资源、账号身份和开关保持，两只WSL接收读回均connected。

通过管理API显式开启只读预览，独立月预算5积分。创建一个本人平台会话，
固定问题提交在共享网关准入处终止，记录`failed`、无call_id、无生成文本；
同键再次查询返回同一记录。生产`ai_calls`仍0条，租户不能读平台回合表，
该表强制RLS。业务付费生成0、原生动作/业务登录/短信0；
`native_ready`/`ai_ready`仍false。**真实模型对话尚未验收通过**，仍需确认
报价单位、缓存价及内部记账币种政策。完整方案见
[助手隔离方案](assistant-isolation-and-gateway.md)，私有交付证据在
`~/.cache/artwslbot/platform-assistant-20261030/`。

## AI 网关实查、测试底座与助手权限模板（2026-10-30，最终上游实查16:35，Asia/Shanghai）

按用户提供的独立业务密钥、`https://www.sevnx.lol` 和
`deepseek-v4.1-flash` 进行有界验证，并落地固定上游查询、单次模型测试底座、
三种权限模板草稿及详细隔离规划。密钥经管理 API 密封保存，源码、审计及
查询投影不含密钥；没有从架构分析配置复制凭据。

### 本批实现

- 来源根地址规范为 `/v1`，拒绝任意探测路径；仅访问同源 `/v1/usage`、
  `/v1/models` 和无密钥的 `/api/provider/pricing`。复用公开地址校验、
  禁重定向/代理的 HTTP 客户端；每请求4秒、256 KiB、共享有界调度。
  查询前后复核当前网站超管及 rate_id；新增迁移033只存安全元数据快照。
- 精确模型、唯一完整报价、明确 USDT/百万token 时自动换算费率草稿；
  未核定币种/缓存价/多组报价不映射。钱包余额不是月预算，不自动扩额/
  发积分/改商业售价。来源变更且未提供新业务密钥时拒绝沿用旧密钥。
- 单次测试固定提示、无工具、最多32输出token、45秒，入口及写入超时55秒；
  使用独立幂等键、单次预留上限及现有平台/测试月预算、版本费率和账本。
  未知只查询与对账；同键不重发，有未决测试时换新键也不能再次提交。
- 租户角色编辑新增“助手只读 / 回复规则建议 / 回复规则建议与本人确认”
  三种v1模板。选择仅修改新角色草稿，人工保存和成员/群分配沿用原事务；
  不含平台、成员授权、原生、财务或受控测试权限，不扩张已有角色。
  独立平台 AI 助手、跨成员审核仍为后续方案，未把管理页面当作新AI代理。
  设计及分阶段验收见 [隔离方案](assistant-isolation-and-gateway.md)。

### 实测与交付

**真实上游只读验收：**16:35:51通过已部署应用的 `/ai-gateway/inspect`
返回有效钱包 `57.21489314 USD`、目标模型已列出、价格接口为 CNY，
`model_price_missing`。这是采样余额，不是月额度或对账结果，也不据此
推断余额变化由本项目引起。没有该模型完整报价和到USDT的换算依据，
因此保持费率未核验、网关/路由关闭和平台/测试预算0。
真实 `/ai-gateway/test` 请求在提交前返回409 `ai_gateway_disabled`，
生产 `ai_calls` 仍0条。本批业务付费生成0；没有声称真实生成或AI完整验收。

检查结果：

- Go `test -race -tags sodium ./cmd/... ./internal/...` 和相同范围vet通过。
  宿主 `./...` 会包含既有忽略的var研究文件，初次因此setup失败，
  已改用明确产品目录验证；未删除或改写这些文件。
- Docker/Linux HTTP PostgreSQL完整回归77.434秒通过；随后串行gateway
  PostgreSQL回归18.272秒通过，仅重建`wsl_test`。
  初次直接复用旧test镜像缺少当前模块；挂载本批匹配的go.mod/sum/vendor后通过，
  没有添加或升级依赖。
- 产品TypeScript检查及前端构建通过；上游/测试模板、商业、助手三组真实
  Chromium浏览器合成回归覆盖320/390/647/1440，无真实模型或原生动作。
  已核对手机截图、越权深链接、模板不自动提交及未知调用保留同一幂等键。
- 45架构工具、3渲染缓存、12Compose测试通过；update/check、边界/仓库/
  迁移锁、空白检查通过；7个Mermaid视图布局不变，证据和清单更新。
  系统Python无PyYAML的初次Compose测试未运行，改用既有锁定工具环境后通过。
- 最终API/Web镜像的临时独立数据库容器smoke通过：33迁移、受限非root/
  只读运行、静态资源、登录/登出、助手默认关闭和两readiness为false。

**建议性模型分析单独记录：**当前可信命名空间默认增量两次通过，没有全量
初始化、替换基线或业务配置复用。第一次2次请求、23526已报告token、
1次未知用量；最终UI错误文案后增量1次请求、25136已报告token、无未知。
未知估算继续保留。最终212文件、9组件、源码指纹
`3e858db8a8e51dea4ea312e97ae1cb4b573ef81bdb8c4c4a1df0ffc6075a65de`，
16:31:38持久检查点保存成功；未执行远端CI、提交或推送。

本地release已同步：API `d82902b7034f…`，Web `132bab1a8b86…`，
只重建这两个服务。新增033兼容性元数据迁移，保留原32个迁移；
原运行API `47c8c829aa21…` / Web `2d9457fac588…` 分别保留
`rollback-20261030-pregateway`标签，旧API可忽略新增表。
PostgreSQL、heatbot、Simon未重建，账号身份/开关、凭据、资源限额及
Compose release模式保持；两只WSL管理账号接收读回均connected。
生产只读核对新增表已强制RLS，租户数据库角色无读取权限，快照1条。
原生动作、注册、登录及短信为0；native_ready和ai_ready仍false。
私有交付、真实安全投影和分析报告位于
`~/.cache/artwslbot/business-gateway-20261030/`，不作为源码或公开附件。

## CodeBoarding 基线修复与真实分析（2026-10-30 15:19–15:31，Asia/Shanghai）

按用户“修复缺少兼容可信基线、完成分析、后续流程畅通”的要求，
修复模型指纹误包含普通图提取器的问题，并在当前分支明确初始化新基线。
源码边界、模型runner、依赖锁、忽略策略、仓库/分支/祖先校验仍生效；
旧全局缓存与旧命名空间保留，没有改标签或绕过预检。

- 共享源码枚举独立到common.py；普通业务变更进入增量，普通架构图维护
  不再反复创建新模型命名空间。真实模型runner或来源策略变化仍需兼容基线。
- 成功基线逐产物记录SHA-256，要求分析、fingerprint和静态序列化/标签完整；
  上游读pickle前核对所有权、权限、身份、祖先和散列。
- 新增同一私有仓库/分支/设置的持久检查点，放在非临时缓存目录。
  活动baseline/state同时缺失时先验证并恢复，损坏或不匹配不自动覆盖。
  旧检查点保留，增量入口禁止上游静默全量回退。
- 45项工具回归、3项渲染缓存测试、架构update/check、边界/仓库/
  迁移基线及diff空白检查通过。7视图布局保持不变，仅更新源码清单。

**真实模型与恢复结果：**209个白名单文件、1,736,074字节，
源码指纹`f4b28d6719d38e9bf07995dd84148bbe06cbe3c658697e89cc54781811c47daa`。
15:24:17首次初始化通过，9个组件且持久检查点成功；
2次请求、157662保守预留、22697已报告Token、1次用量未知。
未知部分保留估算，未宣称零消耗或核清账单。
15:27:45预检ready，15:28:08默认增量通过，模型请求0。
随后实际将活动基线/状态移入备份，预检自动恢复并逐产物核对一致，
报告baseline_restored=true、模型请求0；原基线保留。
15:30:44恢复后再次真实增量通过，9组件与源码指纹不变，模型请求0。
不再沿用此前compatible_baseline_missing作为当前状态。

完整根因、操作和证据位置见
[本批验证记录](architecture/validation-2026-10-30-baseline.md)。
模型结论仍是建议性分析，不替代确定性代码事实。没有执行远端CI、提交推送、
修改工作流或重建产品镜像；本批未运行Go业务/数据库/前端/容器全套发布检查，
因为没有产品或部署变更。Web/API、账号/连接、32迁移及业务模型设置保持运行；
原生群动作、登录和短信为0，native_ready与ai_ready继续false。

## 控制台分类、指标与助手分权 UI（2026-10-30，Asia/Shanghai）

按用户要求完成侧栏二级分类、指标及功能进度展示、Art Design Pro 图表复用，
并详细规划和实施群管助手用户端/超管端界面。保留此前未提交代码，
本批未修改 Go 业务、原生动作、数据库模式或迁移。

### 实现与发现的问题

- 侧栏按工作空间、群管与服务、学习与内容、记录与分析、组织与订阅、
  平台运维及个人服务组织，依据当前工作台与权限隐藏空分类。
  修复原导航 `admin` 元数据未统一过滤，以及 hashchange 未同步页面的问题；
  非超管即使持有无效管理权限元数据或输入管理深链接，也不会加载管理页面。
- 复用现有 ArtBarChart / ArtRingChart 及 Element Plus 统计、进度、描述等组件。
  两图表显式导入 Vue、按需注册 ECharts、独立清理/缩放，不引入演示路由、
  框架设置仓库、生成声明或新依赖。新增当前授权群服务柱图、
  当前可见操作分布、准入额度及原生接入进度；账号15分钟图保持原统计口径。
- 无 HTTP 观察显示未采集，而非空闲或零延迟；无额度不编造分母，
  移除旧100群展示；辣椒聊操作图接现有返回列表，热群无来源时显示未提供，
  不用固定空数组冒充采集结果。目标容量与实测仍分开。
- 功能概况区分可见定义、套餐包含、已保存启用、实际链路启用。
  修复目录/保存固定 `wsl` 的跨平台错误，平台纳入请求与草稿作用域，
  响应平台不匹配拒绝。浏览器目录行断言另发现首次已有群数据时没有触发读取；
  已调整群选择监听注册顺序，首次进入立即加载，不依赖后续刷新。
- 用户端新增独立“助手会话”页，快捷抽屉复用同一组件；管理员设置从会话移出，
  独立“助手管理”提供运行采样、三项准入开关、模型边界和权限表。
  管理设置保留版本 CAS、现有路由/计费字段，不提交 API key。
  指标读取失败清空旧样本，未知用量继续显示待核对。

分权与前端流程完整方案见
[`console-information-architecture.md`](console-information-architecture.md)：
网站角色、租户权限、原生角色分别核对；显式 use/propose/approve、
套餐/群/账号范围与控制事务继续取交集。当前仅本人建议/本人确认，
超管治理不获得他人会话或任意原生执行权。跨成员审批、授权向导、
租户健康聚合和真实模型灰度明确列为尚未实现。

### 本批实际检查

| 检查 | 当前结果 |
| --- | --- |
| 产品类型检查及最终 Vite 构建 | 通过，2271模块，11.29秒；最终 CSS `index-Ds8OpIsJ.css` / JS `index-CAi9f9Wb.js` |
| 六组浏览器回归 | console、assistant、account_dashboard、multi_platform、native_social、group_membership 全部通过；API 合成响应，外部请求阻断 |
| 布局与图表 | 320/390/647/1440屏宽、柱/环图像素、账号图像素、导航分类、管理深链接隔离、实际目录行、跨平台目录、保存/生效统计、未知指标、设置 CAS、无自动调用均通过；检查实际截图 |
| 既有助手回归 | 作用域草稿、流式结果、人工确认、迟到结果及安全输出通过；合成回合1次、合成审批1次 |
| 合成写入 | 管理设置1次；跨平台登录/账号9次、好友1次、入退群3次均只写夹具；真实原生动作、业务登录、短信及模型调用为0 |
| 完整 Go race/sodium | `./cmd/... ./internal/...` 通过，命中缓存；本批后端源码未变化 |
| 串行隔离 PostgreSQL | HTTP 69.615秒、gateway 17.916秒通过，仅重建 wsl_test，未并行重建 |
| 架构代码事实与工具 | update/check、边界守卫、36项工具测试、3项渲染缓存测试及7视图渲染通过；新增共享图表源码抽取测试，不暴露演示模块 |
| Compose 静态检查 | 12项通过；开发镜像及只读挂载仅增加两图表、生命周期及类型文件；本批未执行开发镜像 HMR 运行检查 |
| 最终候选容器 smoke | 通过，临时新库32迁移、bootstrap、非root/只读、静态产物、租户/代理边界及默认关闭发送/AI；未使用生产数据 |
| 仓库与候选源 | 仓库检查、迁移基线、diff空白检查通过；候选源清单随最终文档更新 |

早期小屏测试在图表 ResizeObserver 的下一帧布局前测量，得到瞬时旧宽度；
回归改为等待两帧后检查实际溢出，四屏宽通过。目录首次为空则为实际代码问题，
已修复并新增数据行断言，未仅以页面标题或等待时间判定成功。

CodeBoarding 最新预检于 `2026-10-29T23:05:55Z` 返回
`compatible_baseline_missing`，本次分析阻塞，模型请求0。
保留旧缓存，未初始化、全量回退或外发源码；未执行远端 CI。
上述代码事实、合成回归、本地部署与真实业务验收分别记录。

### 本地交付

07:13前后按已有授权只重建现有 Compose 的 Web 服务，保持 release 模式：
API 仍为 `47c8c829aa21…`，Web 更新为 `2d9457fac588…`。
旧 Web `b0dd4a2ff4ac…` 保留为 `wsl-web:rollback-20261030-preconsole-ui`。
API、PostgreSQL、heatbot、Simon 容器身份/运行配置/重启计数保持不变；
Web 健康且 RestartCount=0。资源限额、只读挂载、凭据散列、32迁移和原部署文件不变，
交付前各类未决提交均为0，服务返回首页与最终构建逐字节一致。
私有交付证据保存在
`/Users/xinxinzi/.cache/artwslbot/deployments/20261030-console-ui/`。

本批不开放助手服务、租户灰度或模型调用；未创建真实测试窗口或执行群动作。
`native_ready=false`、`ai_ready=false`，本次 UI 交付不提升原生/AI整体验收。

## 入退群受控底座（2026-10-30，Asia/Shanghai）

本轮按用户“补退出群组和拉入群组原生功能底座”实施，保留此前未提交代码。
原生契约仍为固定桌面 2.8.3 的单人 add-group-member 与本人 leave-group，
没有解散群、自动加好友、转移所有权或替换连接。

- 功能目录拆分“拉入群组”和“退出群组”，两者进入受控 Tools；
  新增固定 read_membership / run_social，目录由 5 项变为 7 项，
  没有向助手或 heatbot 注册执行/授权工具。
- 群邀请与退群的所有写入（含旧 social API）要求持久窗口及 friends 套餐。
  退群授权目标必须包含账号本人 UID，普通成员只能取得仅退群窗口，群主拒绝；
  当前角色、完整成员、好友、账号/绑定版本和最终提交栅栏继续生效。
- 入群回执只标记邀请获确认；是否实际加入另读完整名单。退群仅从已知完整群目录
  判定离群，离群后不依赖好友读取；缺失/部分状态不当作已离群。
  持久历史扩展至两项动作，未知结果或已关闭窗口只能查询/核对原操作。
- 邀请记录请求修正为固定 lastId="0"，并接受合法零游标；签名合成 HTTP 验证
  参数、空窗口及无额外群参数。初次真实读取证明仅修正参数仍未成功；
  随后独立只读核查确认响应超出解压额度，修复及实测结果见下文。

首批代码检查：完整 Go race/sodium 通过；HTTP 隔离 PostgreSQL
82.864 秒通过，gateway 隔离 PostgreSQL 17.983 秒通过，严格串行重建 wsl_test；
Go vet、宿主 go mod verify、产品类型检查与 Vite 构建通过。
初轮完整回归发现旧夹具空 UID 导致读取 500，修复为兼容读取后重跑通过，
退群目标校验未放宽。离线容器 mod verify 缺模块缓存未成功，后由宿主完成。

浏览器原群管回归、好友审批/目录回归及新增入退群回归均通过：
新流程覆盖 320/390/647/1440 四种屏宽、确认取消、已在群好友不可选、
回执不等于入群、一次预算立即封口、未知结果刷新/关闭窗口后只读恢复、
群主不可退群与普通成员自行退群。新流程 3 次为合成提交，真实请求为 0；
检查实际手机和桌面截图，无遮挡/溢出。测试等待 CSS 过渡完成后再截图。

架构代码事实 update/check、边界守卫、35 项工具测试、3 项渲染缓存测试及
7 视图渲染通过；仅 controlled-tools 关系图增加带源码证据的入退群分支，
没有改变其余视图布局。12 项 Compose 测试通过（使用已具 PyYAML 的环境）。
CodeBoarding 本批预检仍 compatible_baseline_missing，保留旧缓存、模型请求为 0；
未执行远端 CI、全量分析或源码披露。

候选 API 9cc4b00640df… / Web b0dd4a2ff4ac… 的独立容器 smoke 通过：
临时新库、32 迁移、bootstrap、非 root/只读、资源/代理/租户边界、
7 项 Tools 目录、静态产物、默认关闭发送与 AI 均正常，未使用生产数据。
首批候选本地同步成功，仅重建 API/Web，保持 release、32 迁移、凭据、
账号身份/开关、资源限额、挂载与其他服务不变；旧运行镜像保留为
rollback-20261030-premembership，健康与 RestartCount=0 检查通过。
`native_ready=false`、`ai_ready=false`，未提升目录 accepted 状态。

### 真实隔离验收及修复

本次使用唯一“测试群”、已有审核绑定、群主和另一已认证自有普通成员。
预检确认双方当前在群、群主与目标为好友、群服务关闭、未决为零。
声明本轮仅本人退出一次、群主邀请拉回一次，使用两个分别限动作/账号/本人目标、
最长 300 秒、各预算 1 的窗口，总预算 2。没有沿用旧轮窗口或补充预算。
准备阶段第一次因账号开关变更后连接 epoch 尚未刷新而停止，
当时窗口和原生提交均为 0，临时配置已恢复。等待已有接收所有者就绪后，
继续同一范围准备，没有重发任何原生动作。

| 时间（Asia/Shanghai） | 实测结果 |
| --- | --- |
| 05:44:27 | 原生群主/普通成员、好友关系和审核归属核验通过；临时启用非商业 friends-only 套餐，无付费积分 |
| 05:44:29 | 新版本 read_invitations 仍为 409/native_response_invalid；未转为空记录 |
| 05:44:31 | 普通成员本人退群 succeeded/native_leave_acknowledged |
| 05:44:32 | 本人完整群目录已无该群，群主完整名单已无目标；两侧只读核对通过 |
| 05:44:35 | 群主发起一次邀请，succeeded/native_invitation_requested |
| 截至 05:44:47 | 四次有界只读核对未观察到重新入群；仅判定邀请获确认，不能判定拉回完成，没有重发或执行第三个动作 |
| 05:44:47 | 两窗口均 used=1/budget=1 且 cancelled；未决为 0，临时管理、套餐及动作准入均收尾，账号原开关恢复，原套餐及本次未来到期字段恢复，无数据删除 |

目标账号截至该采样已离群，不能把它描述为已恢复原群成员状态。
后续限定账号的原生只读核查返回该群 2 条记录，目标最新记录为
INVITATION / MEMBER_STATE_INVITED，仍待接受邀请；没有自动同意或追加邀请。
该核查使用本地固定路由的有界读取，不是重新授权或关闭窗口后的 Tools 读取。

随后以限定自有账号、固定只读路由的本地 Go 诊断核查该读取失败，只记录结构、
长度与计数，不记录联系人、记录正文或密钥。认证成功的压缩帧声明
解压后 1,418,150 字节，超过通用 1,048,576 字节额度；这解释了本轮实际拒绝，
并非邀请记录为空。对照使用有界 3 MiB 解压成功，2798 条安全投影解析通过。
诊断副本未安装进服务，也未创建 IM 连接、原生登录或群动作。

产品修复只给 get-apply-logs 路由 3 MiB 响应额度，仍限制 4096 条并逐项校验；
其他路由仍 1 MiB。新合成覆盖大窗口、普通路由上限不变、超限/过多记录拒绝、
无自动重试；加密回归覆盖 1.5 MiB、3 MiB、3 MiB+1、篡改及认证解压额度。
修复后完整 Go race/sodium、HTTP 隔离 PostgreSQL 71.300 秒及 gateway 隔离
PostgreSQL 18.146 秒通过；Go vet、宿主模块校验、架构检查、35 项工具测试、
3 项渲染缓存测试及 7 视图渲染均通过。最终候选独立容器 smoke 通过，
32 迁移、7 项 Tools、默认关闭发送与 AI、非 root/只读及租户边界正常。

后续仅重建 API，最终本地 release 为 API 47c8c829aa21… /
Web b0dd4a2ff4ac…。保留前一 API 9cc4b00640df… 为
rollback-20261030-premembership-reads；Web、其他服务、资源限额、挂载、
凭据、账号身份与配置保持不变，健康检查通过，未增加迁移。

06:03:17 最终只读复核通过：群主仍在群，目标仍不在双方完整名单中；
两个原操作仍可查询、两窗口均 cancelled / used=1 / budget=1、未决均为 0。
账号原开关及原套餐恢复，临时订阅关闭，群管理和 AI 关闭；
两个 WSL 接收连接均 enabled / connected。该复核没有新授权或原生写入。
实际退群通过、邀请请求通过，实际重新入群仍待目标在官方客户端接受邀请后核验。
能力目录共 32 项、accepted=0，native_ready=false、ai_ready=false；
CodeBoarding 预检仍阻塞且模型请求为 0，未运行远端 CI。

私有窗口、操作键、原始采样和清理证据分别位于
`/Users/xinxinzi/.cache/artwslbot/deployments/20261030-group-membership/`、
`/Users/xinxinzi/.cache/artwslbot/deployments/20261030-group-membership-reads/`；
不包含网站密码或业务密钥。真实验收、合成回归、代码事实与未运行的远端 CI
分别记录，不将这些局部结果提升为整体上线。

## 新一轮真实定时禁言及跨平台读取（2026-10-30 04:49–04:57）

用户本次再次要求执行受控测试。本轮沿用“测试群”和已核验的自有账号，
单独建立一个 300 秒、预算 2 次、仅允许对该普通成员禁言/解禁的审计窗口。
上一轮已关闭窗口和原预算均未复用。事先声明禁言 1 分钟并等待自动解除，
第二次仅预留必要时解禁收尾；实际只提交一次禁言，没有手动解禁或重放。
以下均为真实本地 release 验证，时区 Asia/Shanghai；并非合成夹具。

### 实际结果

| 项目 | 实测结果与限制 |
| --- | --- |
| 身份与授权准备 | 群主执行账号和另一自有普通成员仍已认证，原生成员完整名单 3 人，目标初始未禁言、全群未禁言；沿用已人工核验归属的绑定，通过现有审计 API 临时启用非商业 moderation 套餐、管理和测试动作准入，没有直接写库或新增付费额度 |
| 一分钟禁言 | 04:49:53 上游确认 succeeded / native_moderation_acknowledged；随后完整禁言名单回读目标存在 |
| 自动解除 | 确认成功后第 56.680 秒仍被禁言，第 67.746 秒首次观察到已解除，第二次新鲜读取仍未禁言；期间手动解禁为 0。该采样区间支持自动到期观察，不证明精确到期时刻或 exactly-once |
| 窗口与持久记录 | 实际 used=1 / budget=2；04:51:04 取消窗口后 read_group 返回 409 / native_test_window_invalid，原操作仍可按原 key 查询，账号历史包含本轮操作，未决操作为 0 |
| 账号及成员资料 | 本人账号资料和指定群内自有目标的资料读取成功；身份与普通成员角色相符，无资料修改 |
| 邀请记录 | 真实 read_invitations 失败，HTTP 409 / native_response_invalid。没有把失败作为空记录、完整历史、奖励依据或验收成功；没有扩大预算重复动作 |
| 两个 WSL 接收账号 | 本轮只读检查时均 enabled / connected，目录均新鲜，“测试群”各匹配 1 个；解码 pending=0、processing=0。目标账号的接收开启状态与较早记录不同，保留当前状态，未为恢复旧记录而关闭它 |
| 辣椒聊成员读取 | 已登录管理账号的“测试群”唯一匹配，真实名单 2 人读取成功；返回 read_only=true、role_authorizes_actions=false，没有据此获得群管执行权 |
| 辣椒聊短时接收 | 原开关关闭，通过现有 receiver API 启用已有会话，5.016、10.025、15.033、20.039 秒四次均 observed / connected，queue_depth=0、quarantined=0；未发起原生登录。没有应用消息样本，解码仍 idle / total=0，因此不证明收消息、ACK 互操作或长稳；完成后恢复原关闭开关 |

WSL 七日留存投影的只读检查仍为 degraded：群主账号 total=16584、
decoded=15926、unsupported=487、failed=171；目标账号 total=210、
decoded=162、unsupported=38、failed=10。失败原因包含 packet_signature、
schema_image，未支持包含 native_format、application_format。
这些是七日累计记录，不能断言全部发生于本轮；待处理为零同样不能证明格式完整。

邀请读取的只读本地契约核查发现一处差异：固定桌面 2.8.3
`desktop283/dist/assets/zh-cn-722bac00.js` 的 `getApplyLogs` 显式传入
`{lastId:"0"}`；本地同批 `api/core/group.proto` 的
`GetGroupApplyLogsRequest` 也有 `last_id` 字段，而当前
`internal/nativeauth/invitations.go` 发送 `{}`。
该差异是下一修复候选，尚无对照请求或响应结构证据证明它就是本次错误根因。
本轮没有放宽响应校验、猜测成功数据或外发本地客户端源码。

### 收尾及剩余验收

新窗口已取消，目标已自动解禁，临时管理、非商业套餐及执行账号动作准入均关闭，
原 testing_enabled=true 保留，执行账号接收仍 connected。
临时套餐仅保留本轮新增的十分钟到期值并已停用：原到期时间已经过去，
现有管理 API 不接受过去的 expires_at，故未声称完整还原到期字段。
没有删除绑定、审计、租户或其他数据。原生密码登录、注册、短信、
聊天/公告发送、踢人、财务和模型调用均为 0。

| 范围 | 仍需完成 |
| --- | --- |
| WSL 管理原语 | 全群禁言/解除、公告、踢人、名片和资料写入、好友审批、邀请/退群的独立范围验收；本轮单成员结果不覆盖这些动作 |
| 读取与接收 | 修复并复测邀请读取；核对留存格式失败与未支持样本；两平台真实消息、重连和长稳，辣椒聊尚无本轮应用消息样本 |
| 文本及媒体发送 | nonce 历史安全、文本/@/引用/私信及图片/语音/视频/文件实际互操作；生产发送口仍未注册，撤回持久控制尚未接通 |
| 自动业务 | 拦截词/黑名单/豁免/关键词回复的事件到控制执行闭环；图片回复、定时、排行、名片规则、好友/邀请自动化仍不能靠保存开关启用 |
| 热群与上线 | 真实模型费率/usage、原生事件到 heatbot 再到最终控制、MaiBot 记忆/学习等迁移、两租户与容量长稳；CodeBoarding 兼容可信基线仍为独立工程待办 |

实时目录核对：WSL 有 31 项原生能力，integration=accepted 仍为 0；
15 项业务目录分别为 native_pending=4、unavailable=8、web_available=2、
request_only=1。辣椒聊 12 项原生能力 accepted=0，
6 项业务目录为 native_pending=4、unavailable=1、request_only=1。
本轮只通过上述明确样例，没有提升目录级、业务级或整体验收状态，
`native_ready=false`、`ai_ready=false`。

私有范围、原始操作 key、时间采样及清理证据位于
`/Users/xinxinzi/.cache/artwslbot/deployments/20261030-timed-mute-acceptance/`，
不含网站密码、业务密钥或消息正文。此次不修改产品代码、迁移或运行镜像，
不重新部署，也不把上一批代码测试或远端 CI 视为本批新执行。
本批实际执行架构事实 check（7 视图，无变化）、架构/Docker 边界、
仓库/迁移基线、私有测试脚本语法与 diff 检查，均通过；
架构守卫使用现有含 PyYAML 的专用环境，宿主裸 Python 的缺模块不作为成功。
额外只读复查再次确认新窗口 cancelled、used=1/2、临时配置停用、
原操作可查询及未决为 0。本批没有执行模型分析、远端 CI 或完整代码回归。

## 受控测试流程完善与本地交付（2026-10-30 04:33）

本轮按“继续完善受控测试，优化其不足”实施页面与只读历史接口改进。
本节的原生验证均为合成夹具；没有续开上一轮已用尽的真实窗口、补充预算、
执行真实群动作、原生登录、注册或短信。下节 03:38 的两次真实动作仍为独立验收。

### 改进范围

- 消息接收与动作准入分开显示；提供已有会话接收启用/恢复及当前群角色核验入口，
  均需明确点击，打开页面只读取本地状态与记录。创建窗口预选当前账号和群，
  默认 300 秒/2 次，仍需人工确认范围与提交。
- 动作前要求 30 秒内群状态，排除本人、群主及越级目标；确认期间锁定账号、
  窗口、动作、目标、时长和公告正文，提交使用已确认的参数副本。
- 成功后自动执行一次只读效果核对。读回失败或不一致不会重发动作；
  成员禁言读回只证明布尔状态，不证明自动到期时长。
- 增加超管本人、当前 WSL management 账号的持久操作记录接口，
  最多 50 条，未决优先，支持窗口关闭或浏览器临时存储丢失后的原 ID 查询。
  会话、账号、actor/window/group 范围均受校验；不返回请求正文、密封内容、
  目标或幂等键，响应 no-store。工具目录仍为原有 5 项，未向助手/heatbot 加权。
- 未知结果及记录检查失败阻止新提交；切换账号丢弃旧响应，切换窗口清理旧效果。
  持久操作返回即本地计次，并单调合并服务端预算，避免父列表稍晚刷新时短暂开放按钮。

### 实际验证

| 检查 | 本轮实际结果 |
| --- | --- |
| Go | `go test -race -tags sodium ./cmd/... ./internal/...`、vet、go mod verify 通过；使用当前 go.mod/go.sum 与匹配 vendor |
| PostgreSQL | 完整串行 HTTP 回归 74.618 秒、gateway 18.355 秒通过；只重建 wsl_test，无生产库重建或写入授权夹具 |
| 历史接口 | 超过 50 条时仍优先显示未决操作；隔离其他 actor/账号/平台、不匹配窗口、普通用户、已撤销会话及私有字段 |
| 前端 | 最终 vue-tsc/Vite 通过，2249 模块，63 秒；JS `index-CUrZ06Of.js`、CSS `index-BM4pjgTr.css` |
| 浏览器 | 最终受控 Tools、原生设置、好友邀请、多工作台 4 组回归通过；320/390/647/1440 像素无溢出，已人工查看截图 |
| Tools 浏览器边界 | 3 次合成提交；确认取消为 0 次提交；刷新及丢失临时存储后恢复未知原操作、取消后只读核对、成功/失败读回不重发、旧账号迟到响应丢弃、记录不可用阻断、旧父列表下预算用尽立即禁用均通过；真实原生请求 0 |
| 架构与工程 | update/check、架构/Docker 守卫、仓库/迁移基线和 diff 检查通过；35 项架构工具、12 项 Compose、3 项渲染缓存测试通过；仅受控图新增历史读取边，其余布局不变，最终 7 图渲染缓存核验通过 |
| CodeBoarding | 04:16:29 预检 compatible_baseline_missing，模型请求/Token 均 0；保留旧缓存，无全量回退或源码外发 |
| 容器 | 最终 API/Web 候选镜像空库 smoke 全通过；非 root/只读、认证、迁移、上传边界、SSE、生产发送/模型默认关闭及两个 readiness=false |
| 交付后 HTTP | 使用网站超管会话只读检查 2 个 WSL 管理账号的历史响应、50 条上限、no-store、字段白名单及辣椒聊/MaiBot 403；目录仍 5 项，真实动作/原生登录/新窗口均为 0 |

初跑用例发现真实的最后一次动作与父列表刷新之间的计次时序问题，已修复；
最终用例刻意保留旧次数，验证按钮持续禁用。另修正不可变平台账号夹具和
等待读取完成的浏览器用例；最初容器测试未挂载当前 go.mod 导致 vendor 不匹配、
宿主 Python 缺 PyYAML，均改用当前文件和现有锁定工具环境后完成检查，
没有改变依赖、账号不可变约束或降低断言。

### 本地交付

04:33 前按既有授权同步 `wsl-platform` / `compose.yaml + compose.local.yaml`
release，只重建 API、Web。停止前及停止后未决资料操作、原生操作、上传、
辣椒聊认证和注册提交均为 0；无新增迁移，仍为 32。

| 服务 | 当前镜像与运行情况 |
| --- | --- |
| API | `sha256:3c6483956889d7499db7a9ee06adb3f1163b908e9ff93069197b0111bab3b29a`；容器 `3048f44e7728`，healthy / RestartCount 0，wsl 非 root/只读，1 CPU / 512 MiB |
| Web | `sha256:70498de8c65c8960c4ff02c01672ecfd4f61f360d6c4609617e4980d8f073c18`；容器 `66c295b3cc79`，healthy / RestartCount 0，nginx 非 root/只读，0.25 CPU / 128 MiB |

账号身份、epoch/version、测试/连接开关、环境配置、挂载及资源限额核对一致，
PostgreSQL、heatbot、Simon 容器未变。页面返回的 index 与最终 dist 一致。
交付后再次只读检查接收：原已启用的 WSL 账号 observed / connected，
另一账号保留原接收未启用状态；未为验证此页面自动开启它。
立即前一运行 API `3e9943b9558f…` 与 Web `39ced7948c2d…` 分别保留为
`wsl-api:rollback-20261030-pretools-ux`、`wsl-web:rollback-20261030-pretools-ux`。
私有交付与 HTTP 检查证据保存在
`/Users/xinxinzi/.cache/artwslbot/deployments/20261030-controlled-tools-ux/`。

本轮没有运行远端 CI、提交或推送。当前真实浏览器连接工具不可用，未声称刷新了
用户已打开的标签；隔离浏览器验证针对最终 dist，运行页面可刷新加载。
`native_ready=false`、`ai_ready=false`，完整业务闭环和其他平台原生群管仍待验收。

<a id="真实隔离群禁言与解禁验收2026-10-30"></a>

## 真实隔离群禁言与解禁验收（2026-10-30）

本节是下方 02:45 只读准备记录的后续实测，时区均为 Asia/Shanghai。
用户指定“测试群”，并授权使用已登录自有账号测试；按 `AGENTS.md` 中
本次明确委托，先声明群主执行账号、另一自有普通成员目标、
1 分钟禁言后解禁、唯一 300 秒窗口和总共 2 次尝试。
网站超管会话通过已审计管理入口完成准备，没有直接写库制造角色或授权。
真实原生密码登录、注册、短信、发消息及其他群动作均为 0。

### 实测结果

03:37 完成当前原生连接的群主角色证明，显式创建独立本地验收租户、
非商业零 AI 额度的仅 moderation 套餐，再经一次性认领码、
租户申请和明确身份核验审核建立群授权。原生群主身份没有自动转成租户所有权。
测试窗口只含一个执行账号、一个目标及 mute_member / unmute_member。

| 时间 | 实际结果 |
| --- | --- |
| 03:38:51 | 完整群名单 3 人；执行者 owner，目标 member，个人禁言列表已知，目标初始未禁言 |
| 03:38:53 | `mute_member` 携带 `minutes=1`，持久操作为 succeeded / native_moderation_acknowledged |
| 03:38:54 | 新鲜读回确认目标已在禁言列表 |
| 03:38:55 | `unmute_member` 为 succeeded / native_moderation_acknowledged |
| 03:38:56 | 新鲜读回确认目标不在禁言列表；回到初始未禁言状态 |
| 03:39:44 | 按原 key 查询两个操作仍成功；唯一窗口 budget=2、used=2；没有重放、补充预算或续开窗口 |
| 03:40:15 | 取消窗口、关闭临时管理开关及套餐，恢复执行账号原 connection_enabled=false；保留用户原 testing_enabled=true |

禁言后即解禁，因此证明的是请求确认及两次布尔状态读回，未测满 1 分钟自动到期，
也不证明平台严格 exactly-once。没有产生 unknown；本轮不需要只读对账恢复。
最终只读库核对为窗口 1、已用 2、未决原生操作和资料操作 0、启用套餐 0。
保留验收租户、非商业套餐、群绑定与操作审计，没有删除业务数据；
绑定管理关闭且套餐停用，不能继续执行。私有验收状态文件为 0600，真实身份、
随机网站密码及认领码不写入本报告。

### 本轮修复与实际检查

修复三个准入断点：账号连接开关只接纳已经启用测试、已登录且接收租约/
同步/心跳新鲜的 WSL management 账号，退出测试清除连接开关；
为已有认领码控制事务补上超管管理接口，重新核验网站会话及角色，审计不存码；
Tools 结果交付使用经验证的显式账号，而非其路由中不存在的 `{id}`。
生产操作入队、发送端口、助手和 heatbot 权限保持关闭。

准备期间接收再次被两个通知阻断：`4/1 → 7/12` 回执、
`4/1 → 7/124` 跨端删除标记数组。只读解封核对形状并复核固定桌面 SDK 哈希，
按精确结构补齐 notice 校验，不删除本地历史、生成 ACK、确认操作或推进游标。
旧隔离证据未删，未知及畸形边界仍停流。证据见
`official-client-research.md` E40。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go | 最终 `go test -race -tags sodium ./cmd/... ./internal/...`、vet 通过；go mod verify 通过 |
| PostgreSQL | 准入/Tools 最终完整串行回归 HTTP 241.140 秒、gateway 19.873 秒通过；通知修复后相关 HTTP 16.368 秒、gateway 1.694 秒再次通过，仅重建 wsl_test |
| 新增覆盖 | 未登录、陈旧租约/心跳、旧版本、测试退出撤权、伪造/过期角色、工作台隔离、租户越权、码哈希及无泄漏审计、撤销会话、Tools 显式账号/旧 epoch、通知所有截断前缀/尾部/数组上限/未知相邻路由 |
| 产品构建 | vue-tsc / Vite 通过，2249 模块，58.56 秒；前端产物未变，本轮不重启 Web |
| 架构与工程 | update/check、架构/Docker 守卫、仓库/迁移基线及 diff 检查通过，35 项工具与 3 项渲染缓存测试通过；受控测试图补充超管准备与显式账号交付校验，其余 6 张布局不变，渲染检查通过 |
| CodeBoarding | 03:14:25 预检 compatible_baseline_missing，模型请求与 Token 均 0，未全量回退、复制基线或外发源码 |
| 容器 | 最终通知修复镜像以锁定 vendor、既有构建/运行层及 network none 构建；空库 smoke 全部通过，生产发送口、模型与 readiness 默认关闭 |
| 失败记录 | 首次 smoke 上传边界出现 502，同镜像复跑通过但原因未复现；补充仅布尔诊断，未降低断言。首次两次 API 交付因环境/挂载列表顺序比较误报而自动回滚，排序规范化后通过 |
| 真实接收 | 最终版本显式 resume 后消息/ACK 各 118、隔离 0；测试收尾及 epoch 恢复后新进程窗口消息/ACK 各 40、隔离 0、队列 0、connected，有同步和心跳。最近样本落盘 P95 12.151ms、ACK 写出 13.307ms，仅为当前自然流量观测 |

03:25:46 首先交付准入修复 API，03:35:27 交付包含通知修复的最终 API。
仍为既有 `wsl-platform` / `compose.yaml + compose.local.yaml` release，
仅 API 重建；无新迁移，迁移总数仍为 32。
最终容器 `0051d1eb72f3`，镜像
`sha256:3e9943b9558f55b53a2527c0c78b8478f38aef1f34fbed0cda13df91e5812638`，
healthy、RestartCount 0、wsl 非 root/只读、1 CPU/512 MiB。
Web、PostgreSQL、heatbot、Simon 容器及配额未变；配置与账号身份核对一致。
立即前一运行 API `46a88e0baea8…` 保留为 `wsl-api:rollback-20261030-precontrolled`，
更早 `35ea1ca2583d…` 亦保留原镜像；未回滚数据库或覆盖运行数据。
交付与有界实测证据在仓库外 0700 目录
`/Users/xinxinzi/.cache/artwslbot/deployments/20261030-controlled-admission/` 和
`/Users/xinxinzi/.cache/artwslbot/deployments/20261030-controlled-native/`。

本轮是本机 WSL 一群一目标的真实群管原语验收，辣椒聊群管未开放，
消息发送、规则闭环、AI 与长期稳定性不在通过范围。
`native_ready=false`、`ai_ready=false`，未运行远端 CI、提交或推送。

## 受控测试准备与真实只读核对（2026-10-30）

用户指定现有隔离候选群后，于 02:45:18（Asia/Shanghai）通过现有网站超管
成员读取入口完成一次真实只读核对。目录与完整名单均能读取，当前账号为群主，
群内 3 人，成员传输身份完整；另一已登录旺商聊账号对应其中一名普通成员。
具体身份只保留在当前网站和受保护业务数据中，不写入本报告。

两个旺商聊账号和一个辣椒聊账号均为 authenticated；两个旺商聊账号的
testing_enabled 已由用户开启，connection_enabled 仍为 false。
运行库没有群授权绑定，也没有测试窗口。已有接收器仍为 blocked /
native_push_boundary_unknown；目录及名单读取成功不代表接收连接或角色证明通过。

代码事实核对发现前置条件冲突：createTestWindow 要求已授权群、原生新鲜角色、
testing_enabled 和 connection_enabled；但 updateAccount 对任何
connection_enabled=true 都返回 native_compatibility_not_verified。
现有群认领批准也依赖该连接字段。仅打开受控开关不能完成当前准入流程。
此项为源码检查发现，没有提交开启连接的请求、绕过检查、直接写库制造授权，
也没有把目录中的群主投影写成可信角色证明。

本轮尚未取得完整目标及动作授权窗口，真实禁言、解禁、发送、注册、短信和登录
提交均为 0；没有重启服务、清除 blocked、推进游标或提高 readiness。
辣椒聊受控执行仍未开放。仅记录本地准备及只读观察结果，不计为原生动作验收。

## 辣椒聊短信注册与两平台轻量验证续接（2026-10-30）

从“评估旺商聊插件与接入器重构方案”的最近断点续接，保留全部已有业务改动。
辣椒聊短信注册、共享认证客户端与验证码生命周期实现此前已经写入，
本轮完成复核、修正、实际回归和本地交付。架构与下一断点汇总于
[项目检查点](project-checkpoint.md)，认证契约与固定客户端证据见
[原生认证](native-auth.md)。

辣椒聊采用官方桌面 `usedFor=3` 短信与 `login_or_signup`，未注册手机号可能开户，
已注册手机号可能登录；发短信与确认提交分开，必须人工同意协议与完成验证。
手机号短期密封保存并绑定网站用户、会话、账号与设备；验证码与滑块结果不落库。
未知提交保持屏障，不自动重发；保存会话不自动启动接收。

旺商聊登录与安全面板共用按需 SDK 加载器，辣椒聊同次打开的面板复用不透明
sandbox iframe 与 SDK，每次验证单独编号并销毁实例。认证 HTTP 客户端只共享
有界连接池，账号令牌与设备仍属请求局部。未增加依赖、生产服务或原生连接所有者。
本轮另修复两项生命周期边界：换空面板时清零旧短信倒计时；已下载脚本未提供
SDK 时清除失败加载状态，允许用户重新发起。浏览器夹具改为等待 iframe 导航，
避免 DOM 已挂载但帧尚处于 about:blank 时误报缺少 sandbox。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go | `go test -race -tags sodium ./cmd/... ./internal/...`、对应 vet 和 `go mod verify` 通过；未变包使用缓存，不把默认跳过的数据库测试计入此项 |
| 串行 PostgreSQL | `WSL_INTEGRATION=1 WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/httpapi ./internal/gateway` 通过；HTTP 103.307 秒，gateway 19.030 秒，仅重建 `wsl_test` |
| 注册控制与协议 | 固定路由/设备与字段、人工同意、短信不触发开户、网站会话绑定、跨面板同手机号冷却、伪造手机号拒绝、密封会话保存、重复提交及未知结果屏障通过，全部原生客户端为合成夹具 |
| 验证生命周期 | 3 项 Node 测试通过，覆盖共享加载、独立取消、迟到回调、加载失败/缺失 SDK 后重试、滑块单次失败保留实例及完成销毁 |
| 产品构建 | 产品 vue-tsc / Vite 和修改的 Vue/TS 文件 Prettier 检查通过；最终 2249 模块、53.84 秒，JS `index-WqvbiPO2.js`、CSS `index-_RdSFSnb.css`；构建耗时不是验证码延迟测量 |
| 合成浏览器 | 辣椒注册、旺商聊认证错误、账号设置、四工作台共 4 组通过，320/390/647/1440 宽度；查看 320 宽度注册截图，无横向溢出 |
| 注册界面状态 | SDK 加载 1 次，滑块失败后原框重试，取消后新实例复用 SDK；双成功回调仅 1 次合成短信，明确确认仅 1 次合成注册提交。换账号不继承冷却、重开原面板恢复安全短信状态且不保留手机号输入，未知结果禁止切换流程和重发 |
| 既有界面回归 | 旺商聊 6 次合成认证提交；设置保留 10 次合成提交、8 步安全流程和 7 次头像读取；四工作台权限/草稿/套餐回归通过，真实原生测试请求均 0 |
| 架构与工程 | update/check、所有权/Docker 守卫、仓库/迁移哈希和差异格式检查通过；35 项架构工具、3 项渲染缓存及 12 项 Compose 测试通过。本轮仅刷新 manifest 证据，7 张 Mermaid 不重画，渲染缓存校验通过 |
| CodeBoarding | 当前分支预检于 `2026-10-29T17:59:16Z`（上海 10 月 30 日 01:59:16）以 `compatible_baseline_missing` 阻断，请求及预留/上报/未知 Token 均 0；未读取分析密钥、外发源码、全量回退或替换旧缓存 |
| 容器 | 锁定既有 vendor、构建层和运行层，以 `--network none` 构建候选；未宣称标准 Dockerfile 联网干净重建。最终镜像与扩展后的空库 smoke 通过 032 字段/强制 RLS/租户拒绝、两用途空面板短信默认状态、非 root 只读、登录退出、静态资源与关闭门槛；临时容器/网络均清理 |

首轮浏览器检查因上述 iframe 导航竞态失败；修正并补充账号冷却隔离断言后，
最终 4 组检查全部通过，没有把失败运行记为通过。
032 为追加短信流程字段的兼容前向迁移，SHA-256 为
`4cab9fdc08f2d4ae72c55b24738bdec0eac58248c26e067f4dfe5b16d9b96814`，
001～031 未改。本轮未运行远端 CI、提交或推送。

### 实际本地交付

2026-10-30 02:10:02（Asia/Shanghai）完成既有 `wsl-platform` 的
`compose.yaml + compose.local.yaml` release 同步，未切换 HMR。
API 容器 `cb92a868b0ca`，镜像
`sha256:35ea1ca2583d5df4b5a96dbf643cb6b0c9fcd59a4f5bc896f0e438ea873d350e`；
Web 容器 `16f32ae17173`，镜像
`sha256:39ced7948c2deff1a8270822b02d228880284ecf32d1cd18f36dc0094e847ca5`，
均 healthy、RestartCount 0、原非 root/只读身份。

冻结唯一 API 写入者后保存一致数据库备份与独立配置副本，在内部临时 PostgreSQL
恢复并演练 031→032、重复运行 migrator。所有旧表原字段指纹一致，新增短信字段
保持空值/零默认，强制 RLS 与租户拒绝有效，没有启动副本 API 或原生运行时。
正式迁移仍保持旧字段指纹一致，线上现有 32 项迁移，未重置 `wsl` 或恢复旧数据覆盖新写入。

仅重建 API/Web；PostgreSQL、heatbot、Simon 的容器、镜像、挂载与配额未变。
API 仍为 1 CPU/512 MiB，Web 为 0.25 CPU/128 MiB。6 个现有账号的身份、
版本/epoch、启停及授权、套餐、台账、AI 配置和 `.env` 保持；原接收器开关未改。
实际服务的 HTML/JS/CSS/验证码页面与候选逐字节一致，Nginx 配置检查通过。
未决原生操作、资料操作、媒体上传、辣椒聊认证和原生开户提交均为 0。

前一运行 API `42a7d74a87e9…`、Web `ad67017ca46a…` 保留为
`rollback-20261030-preauth`。运行中的 heatbot 镜像 `d02ec7c7c9f4…` 与开发镜像保留。
备份、恢复、镜像及核对证据位于仓库外受保护目录
`/Users/xinxinzi/.cache/artwslbot/deployments/20261030-auth-lightweight/`，
目录 0700、敏感文件 0600；备份记录 24 小时删除期限和恢复前保留策略过滤要求，
临时恢复凭据已删除。

接收器仍为 blocked / `native_push_boundary_unknown`，消息/ACK 各 654、
队列 0，提交/ACK 延迟样本均 0、P95 为 null；不能证明接收性能无回归或问题已修复。
`native_ready=false`、`ai_ready=false`。本轮没有真实注册、短信、登录、原生发言、
群动作或付费模型调用。真实 SDK/网络耗时、隔离账号实测及生产吞吐/长稳均未验收，
不能把减少重复加载或健康检查成功当作这些指标达标。

## WSL私信文本与图片持久发送（2026-10-29）

补齐WSL私信专属目标、独立测试授权、session 1 / 7/1路由、图片凭证作用域及
精确回显对账，复用既有持久发送器。新增031前向迁移，001～030哈希未变。
本批没有注册生产发送口、测试真实账号或提高readiness；辣椒聊发送未因此接通。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go | 最终产品包`go test -race -tags sodium ./cmd/... ./internal/...`、对应vet及mod verify通过；没有新增运行依赖或服务 |
| PostgreSQL | 完整HTTP集成race/sodium 152.201秒通过；随后串行Gateway集成19.713秒通过。追加私信RLS测试后私信控制集2.130秒通过，最终含启动/定时恢复隐私检查的完整NativeTextFoundation 44.515秒通过；仅重建wsl_test |
| 控制与隔离 | 私信文本/图片、旧群动作授权拒绝、目标撤销、取消、会话/绑定变化、原文过期/变更、幂等、图片凭证不能用于群聊、未知结果不重发通过。租户已获群授权仍查不到该群关联的私信操作；群管助手查询另排除私信 |
| 协议与连接 | 合成入站/出站身份、session 1图片、7/1唯一连接发送、当前成员及好友核验通过；错误NIM映射/非好友/源变化均阻断。群回显和其他私信对象回显不能核销未知结果，正确私信文本/图片回显可以；未把上游接受当作送达 |
| 前端 | 产品vue-tsc/Vite及修改文件Prettier通过；JS`index-DbSrQsVx.js`、CSS`index-CrFCpAd7.css`。修复把完整服务端scope展开进请求而夹带group_id/scene的问题，仅提交两个epoch字段 |
| 浏览器 | 文本、四类群媒体、私信文本/图片、未知/丢失响应恢复、取消及草稿作用域、Tools四屏宽回归通过；私信只提供图片附件，320/390/647/1440截图目检且无横向溢出。全部API拦截为合成响应，真实原生请求0 |
| 容器 | 锁定vendor、复用既有基础镜像，以`--network none`构建候选；一次性空库smoke通过031、限制性私信RLS、原有媒体/安全上传代理、非root只读、SSE及默认关闭门槛；临时容器/网络已清理 |
| 架构 | update/check、依赖守卫、仓库及迁移哈希检查通过；35项工具、3项渲染缓存、12项Compose缩容/HMR测试通过。前端及启动恢复委托control所影响的后端Mermaid拓扑变化，其余五图未重画，证据按实际代码刷新 |
| CodeBoarding | 最终2026-10-29T15:30:12Z当前分支新预检为`compatible_baseline_missing`，请求/Token均0；未全量回退、重标旧缓存、外发源码或执行远端CI |
| 未作验收 | nonce历史安全、真实隔离私信及群媒体互通、辣椒聊持久发送、生产性能/长稳仍未完成；没有真实登录、短信、资源上传、原生发言、群动作或付费模型调用 |

### 本地部署

- 2026-10-29T15:17:02Z（Asia/Shanghai 23:17:02）完成Compose release同步。
  API`e8f5d0a3b774` / 镜像`sha256:c7994a5ef119167163a1179a254739417094681b9a8c73157d9b4fdd68884576`；
  Web`04e9cfdfea64` / 镜像`sha256:ad67017ca46ab262042c1e5e60e363627839be5c1fef86668cb07356df989662`，
  均healthy、restart0。只重启API和Web；heatbot仍用原运行镜像`d02ec7c7c9f4…`。
- 在线快照在内部隔离网络演练030→031，再冻结API建立一致快照与配置副本后正式迁移。
  所有旧表原字段指纹一致，新增会话/私信路由列保持空默认；私信RLS为restrictive。
  未重置wsl、修改凭据/群绑定/台账、推定租户主账号或更改AI设置。
- `.env`哈希、release模式及API1CPU/512MiB、Web0.25CPU/128MiB不变；
  PostgreSQL、Simon、heatbot容器和运行身份未变。5173的HTML/JS/CSS逐字节匹配构建，
  公网`/internal/`仍404，两readiness均false。未决设置、原生提交和媒体上传均0。
- 当前API/Web及立即上一版本`rollback-20261029-preprivate`保留；
  上一API为`d02ec7c7c9f4…`、Web为`2ba5def71b4e…`。已清理更旧premedia两镜像和候选别名，
  开发、测试及其他项目镜像/卷未动；heatbot继续引用的镜像就是保留的上一API。
- 接收器仍blocked，原因`native_push_boundary_unknown`；消息/ACK均654、队列0，
  提交/ACK延迟样本0、P95为null。这是既有未解决故障，不能据此证明性能无回归，
  也不能把已部署的私信代码称为真实收发已验收。
- 受保护备份与核对目录：
  `/Users/xinxinzi/.cache/artwslbot/deployments/20261029-private-send.lkJNjN/`，
  目录0700、敏感文件0600；演练快照、临时网络/容器及临时数据库凭据已清理。
  一致快照记录24小时删除期限；恢复必须过滤过期原文，不直接回灌覆盖新增数据。

### 启动恢复审计补修

最终复核发现启动时旧SQL会把私信操作的安全恢复元数据写进测试群所属租户的审计，
虽不含正文，仍违反私信隔离。现委托control的`RecoverInterrupted`共用作用域处理：
群操作保留租户审计，私信审计的tenant为空；状态与审计仍同一SQL原子提交。
新增合成测试同时验证启动恢复和定时恢复为unknown、私信审计仅平台可见，绝不重发。

- 最终Go race/sodium、vet、完整NativeTextFoundation及候选空库容器smoke通过。
  Web未变，无新增迁移；2026-10-29T15:31:24Z（Asia/Shanghai 23:31:24）
  仅替换API为`0c2ab949c20d`，镜像
  `sha256:42a7d74a87e96720e341bf52f24b0935cbb98523a6df4a5b8fa3ac27d22a9f07`。
  健康、restart0，原业务表指纹、`.env`、资源和release模式不变。
- API上一运行版本为`rollback-20261029-prerecovery`（`c7994a5ef119…`），
  Web上一版本仍为`rollback-20261029-preprivate`（`2ba5def71b4e…`）。
  清理过时API回滚标签及候选别名。heatbot未重启，仍在用的`d02ec7c7c9f4…`
  明确标记为`wsl-heatbot:running`，不是额外保留的闲置回滚版本。
- 补修未改变接收现状：blocked / `native_push_boundary_unknown`、654消息/ACK、
  队列0、延迟样本0；两readiness仍false，无真实原生操作或付费模型调用。
  受保护核对记录位于
  `/Users/xinxinzi/.cache/artwslbot/deployments/20261029-private-recovery.PlnRlw/`。

## 图片、语音、视频与文件发送底座（2026-10-29）

基于当前产品工作树新增029/030前向迁移，未修改001～028。
029增加强制平台RLS的媒体上传回执，扩展既有操作类型；030允许保存未核价且关闭的
模型草稿，未核验费率仍不能调用。实际实现边界详见`native-media.md`。

| 检查 | 本轮实际结果 |
| --- | --- |
| 协议/上传 | sodium下图片1/101、语音2/102、视频3/103、文件6/104合成向量往返通过，仍用原nonce/压缩/认证封包。固定上传域、multipart字段、提交前栅栏、429、断线、跳转拒绝、无重试及尺寸/时长/容器边界通过；语音是模式兼容实现，未借上游测试宣称验收 |
| 控制/数据库 | 全量HTTP API race/sodium、串行wsl_test 74.867秒通过；后续新增multipart和有界恢复检查后，完整NativeTextFoundation 11.240秒通过。覆盖重复字段、伪造可信字段、额外字段、超大文件、幂等、未知不重传、撤权、跨群/过期回执拒绝及一回执一操作。上传/发送共消耗两次测试动作，原文件不进数据库 |
| Go回归 | 最终`go test -race -tags sodium ./cmd/... ./internal/...`、vet及mod verify通过；Gateway单独以`WSL_GATEWAY_INTEGRATION=1`串行重建wsl_test，18.203秒通过。使用产品包入口，未将var诊断文件冲突记成通过 |
| 前端 | vue-tsc、Vite和全src/wsl Prettier通过；JS`index-D0X7LEJ2.js`、CSS`index-BQ0WaE4D.css`。新增媒体工具栏、目标确认、上传/发送独立状态恢复；320/390/647/1440宽度通过并目检320截图，无横向溢出 |
| 浏览器 | 四种媒体流程、上传响应丢失后查原key而不重传、发送未知不重发及默认关闭通过。既有文本、图片/视频只读、多工作台、Tools、助手、资料设置、好友操作、商业界面回归通过。视频封面使用浏览器生成的WebM夹具模拟前端操作；Go另行拒绝WebM冒充MP4，未将该夹具当作原生MP4验收 |
| 容器 | 首次候选发现Nginx正则`{64}`未加引号，Web启动失败，未部署；修正后Nginx `-t`及最终空库smoke通过。检查029/030、强制RLS、媒体专属大请求路由返回关闭门槛、普通接口仍413、非root只读、SSE及双readiness关闭。所有临时容器/网络清除 |
| 架构/工程 | 35项架构工具、3项渲染缓存、12项Compose缩容/HMR边界测试通过，架构update/check/guard及仓库迁移哈希检查通过。仅前端Mermaid拓扑改变；后端/系统/Agent等六张图不重画，仅刷新实际证据 |
| CodeBoarding | 2026-10-29T14:02:44Z当前分支新预检为`compatible_baseline_missing`，请求/Token均0；未恢复无关基线、全量回退、外发源码或执行远端CI |
| 未作验收 | 未登录真实原生账号、发短信、上传原生资源、发送消息或执行群动作；生产文本和媒体发送器仍未注册。nonce历史安全、真实隔离群/媒体格式、私聊/辣椒聊持久发送及接收性能仍待验收，readiness均false |

### 本地部署与业务模型配置

- 2026-10-29T14:01:17Z（Asia/Shanghai 22:01:17）完成当前Compose release部署。
  API/heatbot镜像`sha256:d02ec7c7c9f456a39a32abb6b333a688200148a4d87c4a6cea38c60677151d47`；
  Web镜像`sha256:2ba5def71b4e797a0363fbe4a55aa3613c1d0d36d7af1c6171719773ec19f53e`。
  API`428491d07f70`、Web`b30e581b25fb`、heatbot`ec3cbfa7bb55`均healthy、restart0。
- 先在线快照并在隔离PostgreSQL演练028→030，再停API/heatbot建立一致备份与配置副本；
  迁移前后全部旧表行指纹一致，未猜测主账号、变更凭据、重置数据库或修改旧台账。
  实际库现有30项迁移。5173的HTML/JS/CSS与构建文件逐字节一致，公网`/internal/`仍404。
- API1CPU/512MiB、Web0.25CPU/128MiB、heatbot0.5CPU/256MiB及release模式不变，
  PostgreSQL、Simon完整容器身份及`.env`哈希不变。单次空闲观察为93.87/8.926/2.984MiB，
  不代表媒体满载或生产容量。已保留部署前API`7c95b666e9ba…`、Web`93c512367b60…`
  为唯一上一发布版本；清理更旧两个发布镜像、失败Web候选及候选别名，开发/测试和其他项目不动。
- 通过正常网站认证、CSRF和超管配置接口，将用户明确提供的业务密钥密封存入数据库，
  地址`https://www.sevnx.lol/v1`、模型`deepseek-v4.1-flash`。源码候选及`.env`精确秘密扫描
  未命中，GET只返回`has_key`。费率未核验，平台/测试预算0、网关及模型路由关闭；
  调用记录0，没有付费推理。供应商匿名models接口要求key，带key只读models探测返回403，
  无法据此证明模型/密钥无效，也没有据此伪造可用或填写估算费率。
- 接收前后仍blocked，消息/ACK均654，队列0、提交/ACK延迟样本均0、P95均null。
  初次部署脚本误取不存在的`reason`字段得到null，未作原因证据；
  之后按实际`error`字段复核仍为`native_push_boundary_unknown`。
  该既有故障未在本批修复，不能据此证明接收/ACK无回归。
- 受保护的备份、演练及核对记录位于仓库外缓存，含原文的恢复需按保留政策过滤，
  不得直接恢复旧快照覆盖新写入。此次配置、迁移与容器健康不提高native/AI就绪状态。

## 租户分权、双档套餐与统一积分网关（2026-10-29）

基于当前产品工作树实施，不覆盖旧账号、凭据、群绑定或旧Token历史。
新增025～028前向迁移：显式成员角色/群范围与actor RLS、不可变商业版本与
720小时账期、共享积分钱包/预留/流水/费率及单次热群作用域凭证。
028补齐短期热群凭证到期清理及账号删除级联，避免凭证永久阻断机器人移除；
积分流水及调用历史不随凭证清理删除。
PLUS为29 USDT、1群3人；PRO为49 USDT、3群10人、10,000共享AI积分。
本轮未确认真实收款，未给既有租户自动换套餐、推定主账号或发积分。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go | `go test -race -tags sodium ./cmd/... ./internal/...`及对应vet通过；发布源码全部覆盖。根目录`./...`仍受既有忽略目录var的诊断文件包名冲突影响，未删除无关文件、未记成通过 |
| PostgreSQL | 最终含028的`WSL_INTEGRATION=1 go test -race -tags sodium ./internal/httpapi -count=1`67.335秒；随后串行Gateway race/sodium 18.154秒，均仅重建wsl_test |
| 权限 | 两租户及同租户跨群、无默认群授权、个人拒绝/过期、受限actor RLS、会话撤销、并发PLUS席位、恢复名额检查及主账号转移CAS/最后主账号保护通过 |
| 套餐/计费 | 精确720小时、收款幂等及重复引用、提前续期不发分、下一期单次激活、不结转、降档超额阻断、不可变版本/原流水、缓存不重复计费、并发余额、子上限、取消、未知冻结、重启不重发及追加纠错通过 |
| 网关/助手 | 合成供应商及Eino适配测试通过：3模型/6顺序工具/90秒、禁止apply、取消、独立回合状态；单次热群凭证重放/人设变更/撤权拒绝，供应商构建期间撤权不发起付费调用。新增回归确认只清理过期凭证，实际删除机器人接口成功后凭证清除、积分钱包保留 |
| 前端 | 产品vue-tsc/Vite及全量src/wsl Prettier通过；最终JS`index-B2ZafSkI.js`、CSS`index-B004qYBT.css`。修复会话恢复重置目标路由、格式化导致内联多语句事件无法编译的问题；旧受控测试页仅整理格式 |
| 浏览器 | 成员/账单/网关320、390、647、1440宽度通过并截图目检；保存冲突保留输入、新成员默认只读且无群、普通成员不见租户账单控件。助手审批/刷新/注入文本、现有四面板及受控测试未知恢复回归通过；全部API为合成夹具 |
| 镜像/空库 | 锁定vendor、复用已核对Go1.27/libsodium及Alpine/Nginx基础，以`--network none`打包候选；未宣称完成从上游干净拉取基础镜像的构建。最终API镜像与当前Web的独立空库容器smoke通过025～028、强制RLS、凭证账号级联、零预算/无业务密钥、非root只读、SSE与关闭门槛，临时容器/网络已清理 |
| 资源/架构 | 12项Compose缩容/HMR边界测试、35项架构工具和3项渲染缓存测试通过；仅后端、前端、Agent及审批业务视图变化，其余三图保持。update/check、依赖守卫、仓库/迁移哈希检查通过 |
| CodeBoarding | 最终2026-10-29T12:39:24Z当前分支预检仍为compatible_baseline_missing，请求/Token均0；未重标旧缓存、全量回退、外发源码或运行远端CI |
| 测试环境限制 | 宿主Python缺PyYAML；改用现有隔离架构venv完成Compose测试，未安装全局依赖。Go1.27的Sonic兼容警告与JSON回退已记录，不据此宣称性能 |
| 未作验收 | 真实模型流式/usage与供应商核价、两租户真实灰度、满载AI与接收/ACK对照压测、目标机长稳仍未执行。没有付费模型调用、真实注册/登录/短信或群动作；native_ready及ai_ready仍false |

最终API及heatbot为
`sha256:7c95b666e9ba8f8504f6098f02c31f0ba13bfff7e06256d660d2057a0996a4bb`，
Web为`sha256:93c512367b60c023b1806797ed4d81c131bc40c9b0c97d48bcd61b363241cf60`。
首轮Go镜像`5430c99c2738…`现保留为上一版本。
上述镜像已按下述记录部署；测试通过不代表已完成真实服务验收。
Eino与网关实现边界见`commercial-billing.md`、`maibot-migration.md`。
旧镜像缺少新用户作用域支持，回退需先审查并关闭租户入口，不能恢复旧数据库覆盖新流水。

### 实际本地部署

- 2026-10-29T12:19:21Z（Asia/Shanghai 20:19:21）完成。沿用
  `compose.yaml + compose.local.yaml`的release模式，未切换到HMR。
  API`10440d798272`、Web`542aab49cb4f`、heatbot`87f48069e35b`
  均healthy、restart0；API与heatbot使用上述同一新Go镜像。
- 先在线快照并在隔离PostgreSQL演练024→027，再短暂冻结API/heatbot，
  建立一致快照与配置副本，才执行运行库前向迁移。旧表及新增列之外的原字段
  指纹一致：凭据、群绑定、台账、旧订阅与Token数据均保留。
  新增两商业版本及显式旧权限，不自动推定旧租户owner。
- API1CPU/512MiB、Web0.25CPU/128MiB、heatbot0.5CPU/256MiB不变；
  PostgreSQL与Simon完整容器/镜像/重启数不变，`.env`哈希不变。
  12:21Z单次空闲观察API79.39MiB、Web8.941MiB、heatbot4.996MiB，
  不是满载或性能容量验收。
- 5173的HTML/JS/CSS与构建文件逐字节一致，`/internal/`公网代理404，
  `/healthz`正常且两readiness仍false。网关无业务密钥、预算零、默认关闭；
  没有付费调用、自动登录、短信、原生发送或群动作。
- 接收连接原先即blocked，部署前后消息/ACK均654、队列0。12:21Z正确口径
  的`durable_p95_ms`、`ack_p95_ms`均null，样本数均0；原因仍为
  `native_push_boundary_unknown`。本轮不修复该原生问题，也不能据此证明
  接收/ACK延迟无回归。部署脚本的`commit_*`旧字段名未命中，不作测量使用。
- 首轮保留当时上一API/Web发布及heatbot自己的上一活动运行镜像。
  删除两旧pretools镜像、一过期Web候选和当前候选别名；未清理开发、
  测试、其他项目镜像或卷，未全局prune。最终保留状态见下述补修。

受保护记录：
`/Users/xinxinzi/.cache/artwslbot/deployments/20261029-commercial.oTG99L/`。
在线演练临时库/网络及多余演练快照已移除；最终一致快照记录24小时删除期限，
恢复必须执行原文保留期过滤。未运行破坏性down migration或旧快照回灌。
当前是默认关闭的本地交付，供应商核价/usage与隔离租户验收后才可开放PRO AI；
不能把套餐开通或网关健康当作热群原生发言已验收。

### 凭证生命周期补修及最终部署

- 最终复核发现027的短期凭证账号外键会阻断后续机器人删除。新增028，
  不改已应用迁移；增加每批最多512条的到期清理，复用现有维护循环。
  独立数据库测试覆盖过期/未过期凭证及账号删除，钱包与账本保留。
- 2026-10-29T12:36:33Z（Asia/Shanghai 20:36:33）完成补修部署。
  先以在线快照在隔离库演练027→028并核对业务指纹，再冻结API/heatbot，
  建立最终一致快照并迁移运行库。只替换API为`7c523b10af30`、
  heatbot为`478d779393d0`；Web仍为`542aab49cb4f`。
  三者healthy、restart0；PostgreSQL及Simon容器、镜像和重启数不变。
- 运行库已应用001～028；商业版本2个，收款订单、AI调用及模型费率均0，
  网关关闭且没有业务密钥。原有3个账号与接收开关保留；接收仍被既有
  `native_push_boundary_unknown`阻断，可靠落盘及ACK分位数无样本。
  没有真实登录、短信、发言、群管或付费模型调用，两readiness仍false。
- `.env`、Compose release模式、所有资源上限及构建页面哈希不变。
  API与heatbot共用当前`7c95b666e9ba…`，共同上一版本为
  `wsl-api:rollback-20261029-pregrantcleanup`（`5430c99c2738…`）；
  Web上一版本为`wsl-web:rollback-20261029-precommercial`
  （`12ce27b24b68…`）。最终仅当前/上一两组共4个发布标签。
  删除无容器引用的更早API回滚镜像和旧heatbot运行镜像；
  未清理无关项目、开发/测试镜像或卷，未全局prune。

最终受保护部署记录：
`/Users/xinxinzi/.cache/artwslbot/deployments/20261029-commercial-final.jHzHtF/`。
包含一致快照、配置副本、迁移演练、部署及核对清单；目录0700、文件0600。
临时恢复容器/网络及临时凭据文件已清理；快照记录24小时删除期限，
恢复须执行原文保留期过滤。回退仅回退镜像并重新核验租户访问边界，
不执行down migration或用旧快照覆盖新账本。

## 账号资料设置窗口优化上线（2026-10-29）

本轮用户要求完善“账号资料与设置”及窗口。前端分为账号资料、隐私与互动，
保留原有单项原生设置接口。资料、搜索、好友验证、群邀请、通话分别保存，
确认框显示修改前后值；未变化不提交，昵称按20个UTF-16单位验证，不隐式trim。
未采集字段及未适配政策保持禁用，头像上传和账号安全修改仍明确未接入。

草稿切页保留，关闭/重新读取需确认丢弃；单项成功不丢失其他字段草稿，
失败保留输入。资料与未决请求独立读取，资料读取失败也能恢复未知记录；
未决查询失败不放行保存，查询、重开、核对均不重发。过期会话有手动重新登录入口，
不会自动发起原生登录。作用域切换清除旧草稿并取消读取及确认框。

| 检查 | 本轮实际结果 |
| --- | --- |
| 产品前端 | 产品vue-tsc、Vite及改动文件Prettier通过；JS`index-Cy-mVpsc.js`、CSS`index-D2-m2k_m.css` |
| 设置浏览器 | 320/390/647/1440宽度及320×480短屏通过，截图目检；10次合成提交覆盖五类个人设置、本人群昵称、false值、取消、旧/新值、UTF-16/空格、其他字段草稿、未知不重发、清空sessionStorage后恢复及未决查询失败。真实原生请求0 |
| 界面回归 | 原生成员、四面板及消息中心三组现有浏览器回归通过，接口全为合成拦截，未操作真实账号 |
| Go | `go test -race -tags sodium ./cmd/... ./internal/...`及对应vet通过，未改后端包使用缓存。直接`./...`因既有忽略目录var内诊断文件包名冲突失败；未删除用户文件，未把该命令写成通过 |
| PostgreSQL | 显式`WSL_INTEGRATION=1 WSL_GATEWAY_INTEGRATION=1`、`-count=1 -p 1`串行通过：HTTP41.690秒、Gateway18.064秒；仅重建wsl_test |
| 容器 | 新Web与当前API在独立空库smoke通过：021、受限角色/RLS、非root只读、资源、SSE、四面板与关闭门槛。临时容器/网络清理，无生产数据 |
| 构建限制 | 标准Dockerfile构建两次均因Docker Hub获取Node/Nginx元数据超时失败。最终使用当前已核对Web镜像`44b54461bcc3…`作运行基础，仅替换经产品构建的dist，保留原Nginx配置及nginx用户；记录为本地应急打包，不声称完成干净标准镜像构建 |
| 架构事实 | update/check、架构及仓库守卫、34项提取工具及3项渲染缓存测试通过。新增nativeSettings字段/草稿模块，只更新前端图及证据；其余5图不重画 |
| CodeBoarding | 2026-10-28T16:37:35Z增量预检以compatible_baseline_missing阻断，模型请求/Token均0；未全量回退、初始化、源码外发或远端CI |
| 实际部署 | 2026-10-29 01:03（Asia/Shanghai），仅Web更新为`aa6f9dcd27d1`、镜像`sha256:d81712ddbf3d684b21c225fd3c0398e995d514f29f2df55cf9d2a13ce1275109`；healthy、restart0、0.25CPU/128MiB不变 |
| 实际资源/边界 | 5173实际HTML/JS/CSS与dist及运行镜像哈希一致；API`61f516b6c416`、heatbot、PostgreSQL、Simon完整容器/镜像/启动时间/重启数不变；.env不变，无运行库迁移或写入、无原生登录/短信/修改。native_ready及ai_ready仍false |
| 镜像保留 | Web当前local与上一运行版`rollback-20261029-presettingsui`；已删除冗余候选及更早Web回滚标签。API当前/上一运行版、heatbot活动依赖、Web dev及测试镜像未动，无全局prune或卷清理 |

部署记录在
`/Users/xinxinzi/.cache/artwslbot/deployments/20261029-settings-ui.acToVf/`，
文件0600、目录0700。本次只更新Web，无需中断原生所有者，
未切换前端HMR、未改生产Dockerfile或新增部署服务。实际原生资料修改仍需人工验收；
本轮合成保存不代表上游成功或原生就绪。

## 密码登录`-1`诊断上线（2026-10-28）

本轮用户报告密码登录持续返回“旺商聊未接受此操作／上游返回码-1”。
已只读确认页面错误，未读取密码，未提交真实登录、短信或验证码。
现有失败没有保留上游原始原因，也没有失败阶段，无法据此追认真实根因。
固定桌面表单中的`account/passwd/validateStr/type`与当前请求一致；
没有以猜测修改密码、切换移动协议或将`-1`统一解释为密码错误。

- 修正未识别的业务拒绝按登录、注册、短信操作分类；补上桌面源码中两种
  “账号或密码错误，请重新输入”标点写法的精确映射，附带其他文字仍不匹配。
  原始上游文案、密码、手机、验证码及票据不返回、持久化或记录日志。
- 新增固定`failure_stage`：区分密码、换设备、短信、安全码、IM凭据和本地保存；
  新提交清除旧阶段。页面同时显示阶段、安全网络诊断及原返回码。
  未知结果继续禁止重试，未增加自动登录或短信降级。
- 本次是诊断缺陷修正，不是上游密码登录兼容验收。新诊断上线后仍需用户
  手动提交登录，依据实际失败阶段继续定位；不能从合成样本宣称已经打通。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go | 全包race/sodium及vet通过；最后新增短信/安全码阶段用例后nativeauth race为1.746秒，定向vet通过。覆盖负返回码、未知文案不外泄、不自动重试、四阶段区分、手机号密码字段及事务所属用户 |
| PostgreSQL | 显式`WSL_INTEGRATION=1 WSL_GATEWAY_INTEGRATION=1`、`-p 1`串行通过：HTTP40.855秒、Gateway18.556秒；仅重建wsl_test |
| 前端/浏览器 | 产品vue-tsc与Vite通过；JS`index-NOP2358h.js`、CSS`index-Dh8Hid9G.css`。六次合成提交覆盖泛化-1、明确密码错误、1001注册分类、验证码重复/关闭回调及未知不重试；320/390/647/1440宽度检查和320截图目检通过，真实原生操作0 |
| 容器 | API/Web候选构建及独立空库smoke通过，非root只读、libsodium、四面板、RLS、SSE及关闭的AI/原生门槛通过；临时网络、容器自动清理。Web首次使用了错误的根构建上下文，已按Compose的web上下文重建成功，未改Dockerfile |
| 架构 | update/check、架构与仓库守卫通过；34项提取工具测试、3项渲染缓存测试通过。仅manifest源指纹变化，全部六张图未变化，复用已校验SVG |
| CodeBoarding | 2026-10-28T14:29:55Z增量预检因compatible_baseline_missing阻断，请求/Token为0；没有全量回退、初始化、源码外发或远端CI |
| 运行只读检查 | running/unknown业务操作0，preparing/submitting/unknown资料操作0；三个账号中一个authenticated，原生动作开关均关闭。本次不涉及迁移、会话替换或readiness变更 |
| 一致快照及恢复 | 用户确认重启窗口后，15:37:00Z冻结API，生成40,662,890字节数据库快照及独立受保护配置副本；隔离恢复后所有业务表指纹一致，候选migrator无新迁移，强制RLS及受限角色通过；临时数据库与网络已移除 |
| 实际部署 | 15:37:36Z前完成（Asia/Shanghai 23:37:36）。API换为`61f516b6c416`，Web换为`7267d7275c96`，均健康、重启计数0；仅替换API/Web，仍使用原Compose及本地缩容配置 |
| 状态保持 | 运行库仍为021；账号身份、密封凭据、群绑定、财务、nonce与发送证据核对一致；.env及资源上限未改。heatbot、PostgreSQL、Simon容器、镜像及重启计数未变 |
| 实际页面/API | 5173返回的HTML、JS及CSS与新镜像逐字节一致，浏览器刷新后加载`index-NOP2358h.js`；网站认证及只读查询通过，健康接口200，两个readiness仍为false |
| 原生边界 | 原生登录、短信、资料变更、发送及群管均0次；接收开关保持关闭，已有快照为blocked/im_kicked，消息/ACK均729、队列0；authenticated不代表连接恢复 |
| 镜像保留 | 当前`:local`及唯一上一运行版`rollback-20261028-preauthdiagnostics`；成功验证后删除冗余诊断候选标签和更早`rollback-20261028-presettings`。活动heatbot依赖、Web dev、测试镜像及无关项目未清理 |

当前已部署镜像：
API `wsl-api:local`／
`sha256:017d4c7720bf1e0671beb63eaffce6319766c651101ca09ff410daa933144f6b`；
Web `wsl-web:local`／
`sha256:44b54461bcc36b89b296599c52b6480503a0cd4bd17eba14bea8a69272d8f5d0`。
上一运行版分别为API`9eec2dca8899…`与Web`7c5cb7b4c8ed…`，
使用`rollback-20261028-preauthdiagnostics`保留；未执行回滚。
受保护快照、恢复核对及部署清单位于
`/Users/xinxinzi/.cache/artwslbot/deployments/20261028-auth-diagnostics.jme9g3wx/`，
目录0700、文件0600，记录七天保留期。维护窗口包含恢复验证，
不是测得的API不可用时长。用户本轮“允许”授权此窗口，已完成，
未复用此前窗口或切换前端HMR。

**诊断版已上线，真实密码登录仍待用户手动复测。** 未提交新的真实登录请求，
不能把部署健康或合成样本视为密码登录成功，也未提高原生或AI验收状态。

## 账号资料与群昵称上线（2026-10-28）

用户本轮明确要求补齐群内昵称（含机器人本人）、账号设置并部署重启。
本节记录实际上线结果，替代下面类型13/群头像及类型3候选当时的
“尚未部署”状态；历史测试结果保持原样。本次同一API/Web发布包含这些媒体候选，
没有启动生产消息发送器、调整登录配置或提高readiness。

- 桌面2.8.3设置与群昵称表单提供固定路径及字段证据。新增原生昵称、
  搜索方式、好友验证、群邀请及语音视频偏好的单项确认操作；
  网站机器人名称与原生昵称、群内昵称分开。头像仅已有资源映射，
  上传修改、密码、手机号及安全字段未接入。
- 群昵称须重新读取成员及群限制，检查绑定、WSL/cards套餐、账号原生动作开关、
  管理开关、双方角色与epoch。自己的群昵称使用同一路径；原生动作未开放时
  明确阻断，未借本次资料功能开放发送、禁言或其他群管。
- 控制层保存密封请求及幂等键，提交前重新检查网站会话、身份、版本、
  最新原生证据和期限。未知结果不自动重试；仅可只读核对当前值。
  未决设置阻止其他资料修改、账号删除、关联群迁移及文本执行。
  细节和原生跨客户端CAS限制见[账号资料契约](native-settings.md)。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go | 全包race/sodium、vet、go mod verify通过；未变包使用缓存。签名请求、内部提交栅栏、字段白名单、UTF-16长度、空值与false区分、角色层级及未知结果分类通过 |
| PostgreSQL | 最终HTTP集成45.859秒；随后显式`WSL_GATEWAY_INTEGRATION=1`的Gateway集成17.975秒通过。串行使用wsl_test，未重置运行wsl；覆盖并发重复提交、处理中撤权、丢响应、未知阻断、只读对账、本人群昵称、撤销绑定和租户角色隔离 |
| 关闭门槛回归 | 发布后仅补测试并复核：HTTP定向集成1.392秒通过，新增未决记录恢复及原生动作关闭拒绝；浏览器确认群昵称按钮在关闭状态禁用，仍为3次合成提交/0次真实操作。未改变已部署业务二进制或再次重启 |
| 前端 | 产品vue-tsc、Vite、相关文件Prettier及diff空白检查通过；JS `index-DWKrAhTF.js`、CSS `index-CGblMdzJ.css` |
| 浏览器 | 资料及成员弹窗320/390/647/1440宽度、确认取消、3次合成提交、未知不重试、只读核对及切换账号迟到响应通过；所有API拦截，真实原生请求0。成员、视频、名片/图片/群头像、消息中心和实时更新回归通过 |
| 容器 | API/Web构建及隔离容器smoke通过；空数据库021迁移、强制RLS与租户拒绝、非root只读、四面板、SSE及关闭的生成/发送门槛通过，未使用真实账号 |
| 架构事实 | update/check、架构/Docker与仓库/迁移守卫通过；34项工具测试及3项渲染缓存测试通过。仅后端、前端结构图变化，其余四图复用；随后仅证据变更，无图重画 |
| CodeBoarding | 最终2026-10-28T13:56:24Z增量预检以compatible_baseline_missing阻断，请求/Token为0；未初始化、全量回退、源码外发或启动远端CI |
| 升级前状态 | 运行库020；running/unknown原生操作0。已有接收为blocked/im_kicked，消息/ACK均729、队列0，目录为native_reauth_required；不是本次重启导致的新状态 |
| 一致快照 | 13:46:52Z冻结API，数据库快照40,682,068字节，配置另存受保护目录；所有原有业务表恢复指纹一致，恢复副本先验证020→021不改变既有业务行、受限角色及新增设置表RLS，再迁移运行库 |
| 实际部署 | 13:47:38Z验证完成（Asia/Shanghai 21:47:38）。API镜像`sha256:9eec2dca88993084c13113b17f3ece8f78283a230d439318106cf1ec583b5a8a`，Web镜像`sha256:7c5cb7b4c8ed65b4d6e459721da0230aa00dcbc9af6c4bd38e6150e32c40b2a8` |
| 容器及配置 | API换为`edde6a023ef4`，Web换为`2abe3678d518`，均健康。heatbot`d33522a1d35f`、PostgreSQL`79489f5a7f89`、Simon`b0b6ac39dd6a`及其镜像/重启计数未变；全部本地资源上限、.env、账号身份、密封会话、群绑定、财务和原生nonce/发送证据核对一致 |
| 页面及API | 5173的HTML/JS/CSS与运行镜像逐字节一致；设置状态查询可用、未决记录0。仅一次真实资料只读请求，返回409/native_reauth_required；未重新登录、发消息、改资料或执行群管 |
| 接收及投影 | 仍为原有im_kicked；Codec 5共11637条，decoded11042、unsupported411、failed184，pending/processing为0。失败含packet_signature135、schema_video44、schema_forward4、schema_image1；未绕过校验，也未将合成测试当作真实媒体兼容验收 |
| 镜像保留 | 当前`:local`及上一运行版`rollback-20261028-presettings`；删除旧video候选、冗余settings候选及更早回滚标签。仍被heatbot使用的旧API镜像单独以`wsl-api:heatbot-runtime`保留；Web dev、测试镜像及无关项目未动，无全局prune或卷清理 |

受保护快照、完整镜像/资源清单、恢复指纹及HTTP检查位于
`/Users/xinxinzi/.cache/artwslbot/deployments/20261028-native-settings.hb4rqmlc/`，
目录0700、文件0600，清单记录最迟七天清理期限；临时恢复数据库及网络已移除。
首轮预检因Docker无IPAM网络的空配置停止，尚未冻结API；修正维护脚本后才执行上述窗口，
未修改Compose网络或清理其他项目。

**部署完成，真实资料与群昵称变更尚未验收。** 当前保存的认证状态仍为authenticated，
但原生资料读取要求重新验证，消息连接为im_kicked；本轮没有伪造可用状态、
自动登录或提高`native_ready`/`ai_ready`。账号原生动作门槛与群授权仍控制群昵称；
未开放的账号不能通过这个新入口绕开。头像上传及账号安全字段保持未接入。
旧API不认识021设置操作，回退前必须冻结并检查新增未决设置及架构兼容性，
不得直接恢复升级前快照覆盖新业务写入。

## 类型3视频消息候选（2026-10-28）

在上一名片/群头像候选上继续实现类型3视频，仍未部署新API/Web。
固定桌面2.8.3的格式枚举、MessageVideo及VideoMsg提供字段和资源路径证据，
未通过猜测把类型3当成图片或文本。Codec 5保留原认证、可靠落盘与ACK链路，
部署后重投影七天内留存输入；没有新迁移、生产发送器或readiness变更。

- 解析field103视频资源、截图引用、秒时长、宽高及字节数；合并记录中的
  视频复用相同schema。重复字段、负数/溢出、非法资源和已知正文冲突拒绝。
  外链只投影为标志，不暴露地址或发起读取。
- 封面按桌面VideoMsg使用同一视频asset的`.jpg`路径，懒加载；
  点击播放读取至多16MiB的MP4/WebM。超过上限仍显示消息信息与封面。
  文件读取复用固定无凭据HTTPS源、两个全局槽、十秒期限及下载后作用域复核；
  内存有界、不写磁盘、不增加转码器或服务，不把容器嗅探当作完整解码验证。
  媒体响应发送`X-Accel-Buffering: no`，阻止Nginx为慢客户端缓冲至临时文件。
- 浏览器复用两读/32排队限制，每个标签页最多保留一个已加载/正在读取的视频；
  换视频、换账号和卸载会取消、停止播放并释放Blob。损坏或不兼容编码明确报错。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go | 全包race/sodium、vet及模块校验通过，未变包使用缓存；视频大小、错密钥、非法/重复字段、外链、转发结构、容器头与真实大小不符、超限响应、压缩、跳转、凭据和固定源检查通过 |
| PostgreSQL | 最终HTTP集成43.004秒、Gateway集成17.924秒通过；均串行重建wsl_test，未修改运行wsl |
| API资源 | 合成MP4请求、封面路径、密封投影生成链接、下载中epoch变化拒绝交付及关闭代理缓冲响应头通过；既有跨账号/会话/工作台、禁用用户及过期测试继续通过 |
| 前端 | 产品vue-tsc、Vite及全产品Prettier通过；JS`index-EPbTM-Yv.js`、CSS`index-DUwa2Qq-.css` |
| 浏览器播放 | Chrome用canvas/MediaRecorder生成本地合成WebM，点击后videoWidth160、readyState至少2且抽样RGBA为[32,105,67,255]，确认真实非空播放画面；6次模拟视频读取，写请求0 |
| 浏览器边界 | 未点击无视频读取、失败重试、超过16MiB与外链不读、最多一段视频、损坏文件提示、转发视频及账号切换迟到取消通过；320/390/647/1440宽度与手机截图通过 |
| UI回归 | 上批名片/群头像、图片/表情/转发、消息中心及实时增量四组检查通过，接口均拦截为合成数据，未读取真实视频 |
| 镜像/容器 | API/Web候选构建与隔离容器smoke通过，临时空数据库、020迁移、受限角色、非root只读运行和关闭的发送/生成门槛通过；没有使用生产数据或原生账号 |
| 架构事实 | update/check、架构/Docker、仓库/迁移守卫通过；34项工具测试及3项渲染缓存测试通过，只更新frontend视图，其余5图复用缓存 |
| CodeBoarding | 最终2026-10-28T12:22:35Z增量预检以compatible_baseline_missing阻断；请求/Token均0，没有全量回退、初始化或源码外发，未提交、推送或启动远端CI |
| 在线状态 | API`d37913b6323e`、Web`7e59e6edd037`、heatbot`d33522a1d35f`、PostgreSQL`79489f5a7f89`及Simon`b0b6ac39dd6a`保持原容器；没有重启连接、切换HMR、登录或群操作 |

合并后的待部署镜像：
API `wsl-api:video-candidate` /
`sha256:530f54af2b1130a46b4b290ae58dee2867cc0fe98b79c8781bd0191692ddbaf7`；
Web `wsl-web:video-candidate` /
`sha256:aa0f7922832491fa76f3401890f1d02aa6761033669ad5c3174643f52e84755d`。
其中已包含上一批个人名片和群头像，不需要分两次API重启。
已删除被替代且无容器引用的`contact-candidate`镜像/标签；当前`:local`、
上一回滚镜像、工作用dev镜像及无关项目保持，没有全局prune或卷清理。

**候选已测试，尚未部署；真实旺商聊视频/封面及不同编码仍待验收。**
当前AGENTS.md要求“explicitly confirmed API restart window”，用户本轮要求
补功能并未确认新的API重启窗口。现有Codec 3不会因网页刷新变为Codec 5。
两个readiness及生产发送继续关闭；合成播放和本地检查不代表原生整体或容量验收。

## 类型13个人名片与群头像映射候选（2026-10-28）

本轮按用户要求补齐收到的`msgFormat=13`个人名片及群头像映射。
主业务工作树保留原有未提交改动，未修改历史迁移、登录、ACK、游标、
发送器或readiness。具体字段、离线桌面证据与资源限制见`docs/native-media.md`。

- Codec 4在原签名、AEAD及路由校验之后解析field108，显示昵称、账号及
  可用头像。名片主体与消息发送者分离，不能据此获得联系人读取或操作权限。
  类型冲突、字段损坏和过大输入拒绝解析；不把名片强转为普通文本。
- 群目录保留已有`groupAvatar`资源ID，群目录、会话列表及会话标题显示
  受作用域保护的头像。装饰会话须同时匹配业务群ID与transport team；
  无聊天记录的群也可使用自己的目录头像，不按群ID猜资源ID或URL。
- 新头像路由只从账号的密封目录选择资源，复用两个全局下载槽和固定
  HTTPS下载源；下载前后复核会话、账号epoch、部署版本与目录。
  图片不落盘；目录复用现有后台/手动同步，不增加页面驱动的原生轮询。
- 升级后由原版本化claim重投影七天内保留输入；新增测试确认不能抢占
  旧版本仍有效的执行租约。签名失败继续保留失败，不绕过认证。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go | 全包race/sodium、vet及模块校验通过；未变包使用缓存 |
| 协议 | 合成AEAD名片、独立主体、账号/VIP/布尔/Timestamp、重复/缺失字段、错误密钥、无效UTF-8、过大字段及多种正文冲突通过；没有复制原生密钥或真实载荷进测试 |
| PostgreSQL | 最终HTTP集成41.020秒、Gateway集成18.014秒通过；串行重建wsl_test，未重置wsl |
| 资源隔离 | 账号/工作台/群/引用替换、普通用户、任意URL参数、过期目录、满下载槽、下载中目录或epoch变化及用户禁用均阻断交付；群路由不匹配不使用目录头像 |
| 前端 | 产品vue-tsc、Vite及全产品Prettier通过；JS为`index-BFRJV0yi.js`，CSS为`index-45Y4spdM.css` |
| 浏览器 | 名片及目录头像合成检查通过，写请求0；320/390/647/1440宽度、恶意昵称纯文本、长昵称换行、无历史群头像、图片失败重试及账号切换迟到结果通过 |
| 界面回归 | 消息中心及实时增量检查通过，包含手机导航、紧凑监控、权限撤销、去重、有界历史及返回最新；查看了手机合成截图 |
| 容器 | 新API/Web镜像构建及隔离容器smoke通过；临时空数据库、受限角色、020迁移、只读非root、四面板及关闭的生成/发送门槛通过，未使用生产数据或原生账号 |
| 架构事实 | update/check、架构/Docker及仓库/迁移守卫通过；34项工具测试、3项渲染缓存测试通过；只更新frontend视图，其余5图复用缓存，没有改人工所有权决策 |
| CodeBoarding | 2026-10-28T11:48:28Z增量预检仍以compatible_baseline_missing阻断；模型请求/Token均0，没有全量回退、初始化、源码外发、提交、推送或远端CI |
| 运行边界 | API`d37913b6323e`、Web`7e59e6edd037`、heatbot`d33522a1d35f`、PostgreSQL`79489f5a7f89`、Simon`b0b6ac39dd6a`均未更换；没有切换HMR、运行库迁移、原生登录或群操作 |

待部署候选镜像：
API `wsl-api:contact-candidate` /
`sha256:be3415f1f705fd17fcf45ed7aeb3a3ca10fdd0568d6af25430aed121d41bd6da`；
Web `wsl-web:contact-candidate` /
`sha256:cccb4b58792e20be5ab22c2dddc41a5ac54e99321c204fcf5f314716768bbe34`。
未改`:local`及上一回滚标签，未删除运行镜像、卷或无关项目。

**尚未部署本批候选API/Web，也未验收真实名片与群头像下载。**
既有目录没有头像字段时，须等原有目录同步取得新字段，不能伪造映射。
现有Codec 3部署不会因前端刷新自动获得Codec 4。本批API重启需要遵守
当前AGENTS.md的“explicitly confirmed API restart window”；先前媒体上线窗口
已完成，不能直接复用。`native_ready=false`、`ai_ready=false`及生产发送保持关闭。

## 媒体候选 API/Web 上线（2026-10-28）

用户明确确认本轮短暂API重启与Web同步更新窗口；本节记录实际部署，
替代下节候选实现当时的“尚未部署”状态，不回写之前的验收结果。
使用原Compose及`compose.local.yaml`，没有切换HMR或重建业务代码。
`2026-10-28T10:46:19Z`冻结API写入，`10:47:32Z`前新API/Web均健康
（Asia/Shanghai为18:47:32）；这段维护窗口包含快照恢复验证，不是测得的API不可用时长。

| 检查 | 本轮实际结果 |
| --- | --- |
| 升级前 | 迁移仍为020；running/unknown原生操作0；接收connected、消息/ACK均832、队列0 |
| 一致快照 | API停止后生成38,191,604字节自定义格式数据库快照；配置及MASTER_KEY另存受保护目录，未变更；文件0600、目录0700，记录最迟七天清理期限 |
| 隔离恢复 | 临时私有Compose仅启动PostgreSQL及候选migrator，无端口、原生所有者或API；恢复后账号/接收配置/绑定/财务/nonce/发送证据/事件/迁移八表指纹一致，24张RLS表、24条策略及受限角色属性通过 |
| 迁移 | 候选migrator在恢复副本与运行库均成功且无新迁移；副本执行前后指纹相同；未重置wsl、改历史迁移或恢复旧快照覆盖运行库 |
| 实际镜像 | API为`sha256:b4dc02dc6907e1885e97c1b3c4180958b17b2fffefcb2de7d29cf921528e3d62`，Web为`sha256:9e531eb01185bd84b8ab9996f82feb2ec9e3373f496d5ca5a5def46b425ec502` |
| 容器 | 仅API换为`d37913b6323e`、Web换为`7e59e6edd037`，健康且重启计数0；heatbot/PostgreSQL/Simon的容器、镜像、重启计数和所有本地资源上限未变 |
| 页面 | 5173返回HTML及`index-ByNSmfCi.js`、`index-DUBwYviX.css`与运行镜像逐字节一致；HTTP健康检查200 |
| 状态保持 | 群绑定、财务台账、消息nonce、原生发送证据及迁移指纹不变；账号3个、认证1个，已有密封会话沿用；未修改.env、接收启停或readiness |
| 自动恢复 | 已有账号自动恢复connected、同步及心跳；没有原生注册、短信、重新登录、发送或群管操作 |
| 重投影 | 10:52:31Z所有现存解码记录均为Codec 3，total11256、decoded10580、unsupported542、failed134，pending/processing均0；升级前为Codec 2，unsupported1858 |
| 接收观察 | 10:52:31Z消息/ACK均348、队列0、协议隔离包0；60秒落盘P95为7.786ms、ACK写出P95为8.487ms；为本地自然负载快照，不是容量验收 |
| 媒体投影 | 44个对话首屏合计590条中，图片74条（其中表情标志11条）、发送者头像链接345条、目标头像链接166条；未采到合并记录样本，未输出姓名/正文/资源ID |
| 剩余解码错误 | application_format254、native_format288、packet_signature133及schema_image1；保持明确失败/未支持，不绕过签名或图片结构校验 |
| 镜像保留 | 仅保留每个应用当前`:local`及上一运行版`rollback-20261028-premedia`；删除已退出成功的旧migrator容器、更早回滚镜像及候选别名；保留工作用Web dev镜像和无关项目，无全局prune或卷清理 |
| 架构事实 | 使用既有独立工具环境重新执行architecture.py check及架构/Docker守卫通过；六份视图changed均为空，没有重画图；部署文档diff空白检查通过 |
| 模型边界 | 本轮没有结构代码变更或模型请求；先前CodeBoarding兼容基线阻断不因上线变为通过，没有全量回退或远端CI执行 |

上一运行API为`sha256:561cc1a79d4e82ec610b9d33eec1fa9124538aadcdef92521946623b1b0bd692`，
Web为`sha256:5da55df83848c4608f3487610a7dad66bdb6a21788bf96a7aaf66a6d9ac264bd`，
分别由同名`rollback-20261028-premedia`标签保留。仍运行的heatbot继续使用该上一API镜像；
本次未将候选内的热群代码部署给heatbot。API回退仍需另行确认窗口和业务状态，
不能自动恢复数据库或把组件回滚标签当作完整业务兼容证明。

受保护快照、逐组件镜像及资源清单、恢复指纹、HTTP及接收观察位于
`/Users/xinxinzi/.cache/artwslbot/deployments/20261028-media-release.v6t4nroa/`。
临时恢复容器及网络验证后已移除，未持久保存第二份数据库。
真实媒体下载/预览、合并记录及动画兼容仍待限定范围验收；本轮原生媒体下载0，
原生发送/群管0。`native_ready=false`、`ai_ready=false`及生产发送门槛保持关闭。

## 旺商聊图片、表情、合并记录与头像（2026-10-28）

在主业务工作树实现候选代码，保留既有未提交改动。补充参考固定
`sh492773746/AstrBot@5ff5113074304e6c2fe89483d3497f727620176d`，
不替换原登录/加密基线。固定桌面包的签名启动配置离线核验了业务域及
图片下载线路；未运行原生客户端、登录、短信、发送或群管测试。
协议字段、资源限制与尚未验收范围见`docs/native-media.md`。

- Codec 3在原签名/AEAD/路由核验后解析图片与表情标志、合并记录及头像。
  资源ID保持字符串；转发最多100条，未支持条目、嵌套引用与截断明确提示。
  升级后由既有版本化claim重投影七天内密封输入，不修改ACK或恢复游标。
- 文件读取独立于接收与解码热路径；固定HTTPS下载源，无凭据、任意URL、
  重定向或磁盘缓存。最多2个并发、5MiB图片/1MiB头像，并校验图像头、
  尺寸与可用的原消息元数据。下载前后复核会话、账号、保留期限和epoch。
- 消息中心按需显示图片/表情、原图预览、聊天记录弹窗和懒加载头像；
  切换作用域取消请求并释放对象URL。原生能力表列出对应只读能力，
  保持partial，不将解析/配置保存当成发送或完整原生验收。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go | 最终全包race/sodium及vet通过；未变包命中缓存，模块校验通过 |
| 协议/下载 | 合成AEAD媒体、超JS精度ID、表情标志、转发上限、未知条目、错误资源ID、重复尺寸、错误密钥；PNG下载、元数据不符、超限、活动内容、跳转与Cookie隔离测试通过 |
| PostgreSQL | 最终HTTP集成40.518秒通过，Gateway集成17.958秒通过；串行使用wsl_test，没有重置wsl |
| 资源隔离 | 跨账号/会话/工作台、普通用户、任意URL参数、过期消息、满下载槽、下载中epoch变化及禁用网站用户均阻断交付 |
| 前端 | vue-tsc、Vite和产品目录Prettier通过；最终JS`index-ByNSmfCi.js`、CSS`index-DUBwYviX.css` |
| 浏览器 | 媒体合成检查14次模拟下载、写请求0；失败重试、图片/表情、转发纯文本、长昵称、嵌套占位及账号切换迟到结果通过；320/390/647/1440宽度通过 |
| 界面回归 | 原消息中心与实时增量检查通过：移动端导航/布局、权限撤销、去重及有界历史保持；查看了390宽度截图，截图仅含合成素材 |
| Docker | API/Web最终候选镜像干净构建通过；配套隔离容器smoke通过，临时数据库/合成网站用户，生产数据和原生账号使用0 |
| 架构事实 | update/check、架构/Docker及仓库/迁移守卫通过；34项工具测试、3项渲染缓存测试通过；仅frontend视图结构改变，其余5图不重画，最终渲染复用缓存 |
| CodeBoarding | 最终增量预检在2026-10-28T10:19:23Z以compatible_baseline_missing阻断；请求/Token均0，没有全量回退、初始化或源码外发；未提交、推送或运行远端CI |
| 运行边界 | 生产仍为020迁移，本次没有新迁移；只读核对running/unknown原生操作合计0；未重启任何现有项目容器或切换HMR |

最终候选镜像：
API `wsl-api:media-candidate` /
`sha256:b4dc02dc6907e1885e97c1b3c4180958b17b2fffefcb2de7d29cf921528e3d62`；
Web `wsl-web:media-candidate` /
`sha256:9e531eb01185bd84b8ab9996f82feb2ec9e3373f496d5ca5a5def46b425ec502`。
已删除本轮被取代且无标签/无容器引用的中间镜像`cf4ce74b9789`、
`612312d5ba1b`；保留运行版、已有上一回滚版与待确认的最终候选，
没有全局prune、删除卷或清理其他项目。

当前运行API`c88b2084c20f`、Web`284c9ce880eb`、heatbot`d33522a1d35f`、
PostgreSQL`79489f5a7f89`、Simon`b0b6ac39dd6a`未更换，重启计数均0。
只读接收观察仍connected、已同步且有心跳，消息/ACK均662、队列0；
此为旧部署自然负载观察，不是新媒体代码上线证据。
旧投影仍含unsupported及packet_signature失败，未绕过签名或宣称清零。

**尚未部署新API/Web，也未进行真实媒体下载/收发验收。**
需要按当前AGENTS的“explicitly confirmed API restart window”完成API窗口确认，
才能切换候选镜像并触发Codec 3留存重投影。图片/表情真实样本、原生下载
可用性、缺失资源、动画效果及所有格式兼容仍需限定范围验收；
合成PNG界面通过不等于每种媒体真实可用。辣椒聊媒体、嵌套记录补拉、
媒体上传/发送不在本批实现内。两个readiness及原生发送继续关闭。

## 已有会话接收恢复与群管图标（2026-10-28）

用户明确要求修复已有WSL账号`native_session_renewal_failed`暂停。
旧快照为`2026-10-28T07:24:04.178438842Z`、阶段`im_token`，
没有底层错误或安全上游数字码，因此本轮不推断原始失败原因，
也不把它当成JWT失效的证据。现存密封桌面会话通过部署、身份和设备绑定核验；
一次人工换票成功并按epoch/revision/部署版本密封保存。随后通过控制面的
版本化接收开关恢复该已有账号，revision由9升至10，没有改账号epoch。
没有重新登录原生账号、发短信、发送消息、拉群或执行群管动作。

| 检查 | 本轮实际结果 |
| --- | --- |
| 真实接收 | `2026-10-28T09:06:54.629171836Z`建立连接；09:16心跳仍正常，首次同步已可靠提交；连续约9分钟重连数0 |
| 接收证据 | 首轮403条落盘/ACK均403；后续为432/432，接收队列0、协议隔离包0；ACK表示socket写出，不表示服务器业务处理完成 |
| 目录与对话 | 群目录同步成功、6群；44个已接收对话中群聊6、私聊38；未读取或打印对话正文 |
| 延迟快照 | 后续60秒窗口落盘P95为9.603ms、ACK写出P95为10.192ms；初始补收403条时为87.214/90.018ms。这些是本地自然负载快照，不是容量或延迟目标验收 |
| 解码缺口 | 采样累计10712条、decoded8804、unsupported1780、failed128，待领/处理中均0；失败仍含packet_signature，未支持含application_format/native_format，未绕过校验或将这些消息报为成功 |
| 图标 | 群管用途改为离线Lucide机器人图标；平台选择及各工作台继续使用各自Logo；ISC来源与完整许可证已保留 |
| 前端 | 产品vue-tsc、Vite、Prettier通过；JS`index-Qn7UdDhV.js`，CSS`index-Z6lym3qc.css` |
| 合成浏览器 | 添加流程的四种用途/平台、真实弹窗动画、320/390/647/1440宽度、机器人图标加载通过；消息布局、实时订阅和未知发送结果回归通过；所有浏览器写请求为模拟接口 |
| Go | `go test -race -tags sodium ./cmd/... ./internal/...`及vet通过，未变业务包命中缓存；本轮没有修改生产Go业务代码 |
| PostgreSQL | HTTP race/sodium集成41.336秒通过，随后串行Gateway集成17.979秒通过；测试只重建wsl_test |
| 容器 | 新Web干净构建通过；配当前API的隔离容器smoke通过，使用临时空数据库、合成网站账号和020迁移，没有使用真实原生账号 |
| 架构 | 事实update只更新manifest、6份Mermaid不重画；check、所有权/Docker和仓库/迁移守卫通过；34项Python与3项渲染缓存测试通过；渲染器复用6份未变SVG |
| CodeBoarding | 当前增量仍以compatible_baseline_missing阻断，请求/Token均0；未初始化、全量回退或外发源码。本轮没有提交、推送或启动远端CI |

人工诊断工具`tools/native_session_probe.go`默认不联网，仅显式
`--renew-token`才可针对已暂停、无活动租约、目录空闲且无冷却的账号换票一次。
持有生命周期/账号/目录行锁，使用数据库持久化请求序号，回写保留密封格式；
不会创建原生登录、IM连接或改变接收开关。它不进入自动化测试或生产镜像。
这次成功恢复不能追溯证明旧失败原因，也没有更改未知提交、令牌异常、
毒包边界的自动重试/阻断策略。

本轮只重建并部署Web，容器`284c9ce880eb`健康，镜像
`sha256:5da55df83848c4608f3487610a7dad66bdb6a21788bf96a7aaf66a6d9ac264bd`。
5173返回的JS/CSS/robot.svg与构建逐字节一致。
API`c88b2084c20f`、heatbot`d33522a1d35f`、PostgreSQL`79489f5a7f89`、
Simon`b0b6ac39dd6a`的容器ID、镜像、重启数和资源限制均未变化；
没有切换HMR、运行库迁移、修改配置密钥或为前端重启原生连接。

Web保留当前`:local`与上一运行版`rollback-20261028-receiver`
（`sha256:d593d1ee3729a80cf8892b984a75ef9edbd2c3992faf80659de12563b631dcba`）。
已显式删除更早Web回滚和候选别名；API当前/上一版及可抛弃dev镜像保持原状，
没有全局prune或删除数据卷。受保护核对清单位于
`/Users/xinxinzi/.cache/artwslbot/deployments/20261028-receiver-recovery.84dpo_ut/`。
两个readiness、原生发送及热群生成继续关闭；真实接收恢复不是整体原生、
完整解码、手机共存、生产容量或长期稳定性验收通过。

## 添加机器人修复与 Go 文本引擎迁移（2026-10-28）

本轮在当前主业务工作树继续实现，保留此前未提交产品改动。
添加入口先选择群管／热群用途，再选择旺商聊／辣椒聊平台；选择框的
迟到关闭事件不再关闭新登录表单，两个登录组件保持挂载并分别校验事件来源。
跨池创建使用选定工作台的请求作用域，当前视图不跳转；账号选择记在对应面板。
辣椒聊使用用户提供的PNG，热群使用固定MaiBot版本自带的麦麦图标；
来源与许可证记录于`THIRD_PARTY_NOTICES.md`。

Go文本引擎新增不可变配置／上下文快照、受限历史组装、输出Token上限、
温度配置、排队取消、上下文过期和用量回传。提案携带配置／上下文版本及
实际期限，取消后迟到的回复不可交付。拒绝工具／函数调用、拒答、截断、
异常角色、无效UTF-8和超大文本；缺失费用数据标为未知，失败不自动重试。
这只消费已授权的上下文片段，不是人物／学习／记忆持久化已经迁移完成。

| 检查 | 本轮实际结果 |
| --- | --- |
| 前端 | 产品`vue-tsc`、Vite和全量产品目录Prettier通过；JS`index-Ci762ohI.js`、CSS`index-Z6lym3qc.css` |
| 添加流程 | Playwright使用真实弹窗动画；320／390／647／1440宽度，四种用途／平台组合、表单不闪退、输入保持、返回取消及Logo加载通过 |
| 跨池与权限 | 模拟管理／聊天池独立登录、跨池创建不跳视图、账号草稿恢复、普通用户面板、过期／撤销群配置阻断通过；9次写请求均为模拟接口 |
| 消息回归 | 消息中心布局、发送确认／未知结果不重试、账号实时更新、成员列表及桌面注册1001错误展示等6组合成浏览器场景通过；真实原生动作0 |
| Go | 最终`go test -race -tags sodium ./cmd/... ./internal/...`和`go vet -tags sodium ./cmd/... ./internal/...`通过；未变包命中测试缓存 |
| 引擎专项 | 新增作用域、并发配置变更、排队／运行取消、期限、未知用量、供应商文本协议与私有HTTP门槛测试通过；额外8轮race重复检查通过，随后补充调用方期限测试并通过最终全包检查 |
| HTTP集成 | `WSL_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/httpapi`通过，41.356秒；只重建`wsl_test` |
| Gateway集成 | HTTP完成后串行运行`WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/gateway`通过，17.986秒 |
| Docker构建 | API／Go引擎和Web干净产品构建通过；首次API构建遇上游Registry TLS超时，重试通过，没有修改镜像来源 |
| 容器问题与修复 | 首次smoke发现附件PNG权限600导致非root Nginx返回403；仅将公开Logo文件改为644，重建并重新验收通过 |
| 最终容器smoke | 隔离临时数据库、020迁移、受限角色、四面板、平台／用途、合成网站登录、SSE、三个Logo及非root只读运行通过；Go引擎健康仍为`parity_verified=false`、`native_sender=false` |
| 架构 | 事实update／check、所有权与Docker守卫、仓库／迁移守卫通过；34项Python工具测试、3项渲染缓存测试通过；只更新前端Mermaid与SVG，其余5视图不重画 |
| CodeBoarding | 当前增量以`compatible_baseline_missing`阻断，请求和Token均0；没有全量回退、初始化基线或外发源码 |
| 远端CI | 本轮未提交、推送或调度远端；上述为当前工作树本地结果 |

本轮**只部署Web**，通过原Compose与本地缩容覆盖配置执行
`up -d --no-deps --no-build --wait web`。新Web容器`0236c4c528f8`健康，
镜像`sha256:d593d1ee3729a80cf8892b984a75ef9edbd2c3992faf80659de12563b631dcba`。
5173返回的JS／CSS及三个PNG与验证构建逐字节一致。
API`c88b2084c20f`、heatbot`d33522a1d35f`、PostgreSQL`79489f5a7f89`、
Simon`b0b6ac39dd6a`的ID、镜像、重启次数及资源限制均未变化。
未切换HMR、迁移运行库、修改配置密钥或重启原生连接。

Go引擎改动完成候选镜像验证，**没有替换正在运行的后端或热群容器**。
已验证候选镜像为
`sha256:b330dfda1ab14dac2616458c29f127b2d109636a486bc3db26bb3e1ac92b6847`，
验证后按镜像保留要求清理；后续后端发布须从当时源码重新构建与验收。
完整迁移仍缺人物／记忆／学习的群级持久化、媒体／视觉／向量执行、
热群原子Token台账、原生输入路由及最终发送闭环；详见
`docs/maibot-migration.md`。本轮测试不是完整MaiBot行为对照或真实群验收。

Web仅保留当前`:local`和上一运行版
`rollback-20261028-robot-add`（`sha256:2dd1f872f13e8dae14e72d20783aee98386af8bf467be3ebdd40bb2e5992b762`）；
API仍保留当前`:local`与`rollback-20261028-predeploy`。清除旧Web回滚、
本轮候选别名及明确属于本轮的未运行中间镜像；没有全局prune或删除真实数据卷。
保留的组件版本不冒充已验证配套回滚版本，回退说明已更新。
受保护核对清单位于
`/Users/xinxinzi/.cache/artwslbot/deployments/20261028-robot-add.oWmSsK/`。
`ENABLE_NATIVE=false`、热群生成关闭以及两个readiness保持不变，
没有自动开户、短信、真实账号登录、原生收发或群管测试。

## 四面板与桌面注册配套部署（2026-10-28）

用户明确要求部署后，从当前主业务工作树构建并更新本地5173的API、
Go热群服务及Web。继续使用`compose.yaml`和`compose.local.yaml`，
没有切换Docker开发/HMR模式，没有提交、推送或运行远端CI。
Docker Desktop起初未运行，启动后恢复既有容器；下面的切换另行停止
web/api/heatbot并完成备份、演练及前后端配套更新。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go / race / sodium | `go test -race -tags sodium ./cmd/... ./internal/...`通过，命中当前源码的测试缓存；`go vet -tags sodium ./cmd/... ./internal/...`通过 |
| HTTP / PostgreSQL | `WSL_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/httpapi`通过，41.033秒；只重建`wsl_test` |
| Gateway / PostgreSQL | HTTP完成后串行运行`WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/gateway`通过，17.972秒 |
| 干净镜像构建 | 两个Dockerfile构建通过；产品`vue-tsc`和Vite通过，JS`index-D8fl1RBQ.js`、CSS`index-DND3-JIk.css` |
| 候选容器smoke | 新建隔离数据库、迁移和合成网站登录通过；四面板、平台/用途、受限角色、凭据隔离、账号/监控SSE、助手默认关闭、非root只读及资产检查通过；不使用真实账号或业务数据 |
| 架构与工具 | 事实check、所有权/Docker、仓库/迁移守卫、34项Python架构测试、3项Node渲染缓存测试通过；六份Mermaid/SVG均未发生结构变化 |
| CodeBoarding | 增量预检仍以`compatible_baseline_missing`阻断；请求、预留及上报Token均0，未初始化基线、调用模型或全量回退 |
| 切换前保护 | 停止应用写入，确认业务操作及未决开户数为0；保存35,061,819字节自定义格式数据库备份、独立配置副本、源码指纹和镜像清单；目录0700、保护文件0600，均在Git外 |
| 实际备份恢复 | 私有隔离Compose只启动PostgreSQL和一次性迁移器；恢复前后证据一致，019→020迁移通过。未启动API、原生连接或模型 |
| 保留期检查 | 恢复副本的原始接收、解码、inbox正文及助手事件没有超过7天的数据；不代表未来恢复自动过滤或长期备份保留已经实现 |
| 运行库迁移 | 运行库前向增加`020_multi_platform.sql`，共20项；与演练一致，未重置`wsl`，未执行bootstrap或旧版迁移器 |
| 运行更新 | API`c88b2084c20f`、heatbot`d33522a1d35f`、Web`f7ecfd7b3c29`均healthy、重启计数0；原PostgreSQL及Simon容器没有重建 |
| 页面资产 | 5173首页、JS、CSS、Logo和healthz成功；运行Web实际返回本次构建资产，`native_ready=false`、`ai_ready=false` |
| 业务核对 | 3个账号、1份账号凭据、账号epoch、绑定、接收revision/游标、15762条原始接收及10280条解码记录保持一致；财务和业务操作均0；配置及MASTER_KEY未改 |
| 本地预算 | API1CPU/512MiB，热群0.5CPU/256MiB，Web0.25CPU/128MiB，PostgreSQL1CPU/1GiB，Simon0.1CPU/64MiB；逐容器核验，未扩大配额，不代表测得生产吞吐 |
| 镜像保留 | 当前`:local`与`rollback-20261028-predeploy`各一版；旧20261027回滚镜像及本轮候选别名已显式无force移除。原有可抛弃`:dev`工作镜像未启用，不作为发布回滚版 |
| 临时资源 | 仅清除本轮容器smoke及隔离恢复项目的容器、网络和恢复卷；保留真实数据卷和其他项目，没有全局prune |

当前API/heatbot镜像：
`sha256:561cc1a79d4e82ec610b9d33eec1fa9124538aadcdef92521946623b1b0bd692`；
当前Web：
`sha256:2dd1f872f13e8dae14e72d20783aee98386af8bf467be3ebdd40bb2e5992b762`。
上一运行版API为`sha256:dd318008abd8a9adba2d3a372477671ff33b49999bc5353c82fdbb291b431b27`，
Web为`sha256:b619c4320d4ca607154f382f7e75999e6d80e4d0a6154e5682a7002afe9935b9`。

受保护备份与核对清单：
`/Users/xinxinzi/.cache/artwslbot/deployments/20261028-console.66x3a2a0/`。
本轮已配套部署桌面注册参数/1001范围修正、紧凑面板切换、辣椒聊、
热群及用户中心；本轮未重复此前五组合成浏览器检查，不把先前记录当作
新部署后的真实账号验收。实际开户仍需用户手动验证，不自动注册、发短信、
登录、发送消息或执行群管动作。

既有账号接收快照在停止前和启动后均为
`blocked / native_session_renewal_failed`，没有清除阻断、变更epoch、
推进游标或开启发送。部署成功不等于该账号已经恢复接收。
`ENABLE_NATIVE=false`、热群测试生成关闭及两个就绪门槛保持不变。

020新增平台唯一索引及作用域约束。保留旧镜像不等于旧二进制支持全部新增状态；
回滚前必须冻结写入、核对新业务及旧SQL兼容性，不运行旧迁移器或恢复旧库
覆盖切换后的写入。本轮未实际回滚。CodeBoarding基线门槛仍未通过，
本次是已验证的本地功能部署，不声明完整生产容量、原生或AI验收通过。

## 本地部署与单版本回滚保留（2026-10-27）

经用户明确要求部署后，使用主业务工作树及
`compose.yaml` + `compose.local.yaml`，更新了本地5173服务。
这是本地缩容功能部署，不是生产容量或原生链路验收。
以下结果在候选构建及开发验证完成后实际执行；此前章节的
“尚未部署、运行库015”等表述保留为当时的历史记录。

| 项目 | 实际结果 |
| --- | --- |
| 升级前保护 | 按运行容器的真实镜像ID保存上一版；停止web/api/heatbot后，生成8.8 MiB自定义格式数据库备份及独立配置副本，目录0700、文件0600，保存在Git外 |
| 恢复演练 | 新建私有Compose项目，仅启动无外网网络的PostgreSQL与一次性迁移器；恢复实际备份，015→019通过，不启动API、解码器、原生连接或模型 |
| 恢复核对 | 账号/凭据存在性、epoch/登录与连接开关、接收revision/游标/阻断状态、群绑定版本指纹、消息计数及财务总额一致；租户不能读取native_text_routes |
| 运行库升级 | 新镜像迁移器正常退出0；最大迁移为019_native_text_routes.sql，共19项。未重置wsl、未运行bootstrap或down migration |
| 服务更新 | api/heatbot/web按本地覆盖配置重建并通过健康检查，重启计数均0；原PostgreSQL与Simon容器没有重建 |
| 页面与资产 | 5173返回新版JS index-BtDlRhnG.js、CSS index-C8ihx_yB.css及Logo，均HTTP成功；现有浏览器刷新后网站会话仍有效，工作台、消息中心及系统监控正常加载 |
| 监控 | 页面显示实时连接，API/PostgreSQL/Simon可观测；未采集的热群/Web资源仍显示未采集，不冒充项目用量 |
| 启动后核对 | 与备份相比，账号及receiver指纹、3692条原生接收事件、2857条已解码记录一致；群绑定/业务操作/财务流水均0，不存在未知操作 |
| 本地预算 | PostgreSQL 1 CPU/1 GiB，API 1 CPU/512 MiB，heatbot 0.5 CPU/256 MiB，Web 0.25 CPU/128 MiB，Simon 0.1 CPU/64 MiB；实际容器配置核对一致，不代表测得吞吐 |
| 镜像清理 | 删除3个旧API候选和2个旧Web候选的镜像ID，移除当前候选重复别名；两个仓库均只剩当前:local与一个上一运行版本rollback-* |
| 临时资源 | 仅删除本次恢复演练私有项目的容器/卷；保留wsl数据卷、基础镜像及其他项目，没有执行全局prune |

当前API/heatbot镜像：
`sha256:dd318008abd8a9adba2d3a372477671ff33b49999bc5353c82fdbb291b431b27`；
当前Web：
`sha256:b619c4320d4ca607154f382f7e75999e6d80e4d0a6154e5682a7002afe9935b9`。
上一版分别为`wsl-api:rollback-20261027-predeploy`
（`sha256:47a1d97552cd96cedf1aae89a468794accdd7b3453b9d8ad20ed9779a2bd6ab5`）
和`wsl-web:rollback-20261027-predeploy`
（`sha256:aed958f55cc984e26a00f1bf8d77c5d49051ddcfc07ed31042b83650a8b1d5d1`）。

保护备份及核对清单位于
`/Users/xinxinzi/.cache/artwslbot/deployments/20261027-native-owner.r5KQu9/`；
不是可公开上传的测试产物，包含受保护的配置和加密业务数据。
恢复/保留仍须执行七天原文保留政策，不能把备份当作永久原文副本。
未执行真实回滚；回滚前仍须复核新业务状态的旧二进制兼容性，不覆盖升级后的数据库。

边界不变：账号保持authenticated，receiver仍为blocked /
native_push_boundary_unknown / 4/1/184，revision及游标未改；
没有自动重登、恢复接收、发送或群管动作。助手总开关、租户开放和审批均关闭，
助手月额度0；native_ready=false、ai_ready=false。
CodeBoarding本地增量基线不兼容仍未解决，未改用全量或发送源码给模型；
本次未声明完整生产发布门槛全部通过。镜像保留操作步骤见docs/docker-operations.md。

## 连接所有者适配与接收阻断修复（2026-10-27）

范围：当前本地候选工作树，不是5173运行版本或远端CI发布。只读诊断运行状态及
本地已隔离协议包，输出形状/计数，不输出消息正文、账号身份或凭据。没有真实原生
登录、发送、群管动作；没有修改`wsl`、清除blocked记录或推进恢复水位。

实际原因：运行账号auth_state为authenticated，account_receivers.enabled为true，
但snapshot.state为blocked，error为native_push_boundary_unknown，形状4/1/184；
account.connection_enabled另为false。隔离包内层8/119/200为桌面SDK单Property
成员同步通知，本批严格兼容后，同一隔离包只读回放recognized_by_current_codec=true。
旧隔离证据保留；无法核验的应用签名及未知格式仍保留原有失败/不支持状态，不绕过认证。

| 验证 | 实际结果 |
| --- | --- |
| Go race/sodium全包 | `go test -race -tags sodium ./cmd/... ./internal/...`通过 |
| Go vet | 同范围及sodium标签通过 |
| HTTP PostgreSQL | `WSL_INTEGRATION=1 ... ./internal/httpapi -count=1`最终全包通过，62.323s；仅重建wsl_test |
| gateway PostgreSQL | 最终HTTP完成后串行运行，race/sodium通过，18.364s；仅wsl_test |
| 新协议单测 | 8/119直包/通知封装、截断、尾部及相邻未知路由；不产生消息/ACK/水位 |
| 连接适配 | 合成HTTP目录/名单与本机假WebSocket；复用一个连接，身份/角色/owner失效及取消不写出，发送后原心跳可继续 |
| 未知对账 | 合成可靠回显：账号/群/发送者/客户端ID/精确密文匹配；无证据、冲突、201条截断、越权和epoch变化不改写unknown；重复对账不重发 |
| 迁移隔离 | 019为前向新增；001–018哈希不变；wsl_tenant不能读路由证据 |
| 产品前端 | vue-tsc及pnpm build通过；JS index-BtDlRhnG.js，CSS index-C8ihx_yB.css |
| 浏览器 | 合成API及已构建资产，全部网络由测试路由拦截，不访问真实账号；320/390/647/1440宽度通过，无横向溢出，账号迟到响应隔离、协议阻断说明及租户隐藏通过 |
| Docker候选 | API/Web构建及临时独立数据库smoke通过；019迁移与角色隔离、非root只读、网站登录退出、账户/SSE/监控正常；无原生配置、native_ready=false、ai_ready=false |
| 架构 | update/check、所有权/仓库/迁移守卫、17项工具单测通过；关系未变，不重写四张Mermaid图，只更新事实来源及指纹 |
| CodeBoarding | 已执行增量入口，在模型调用前拒绝不兼容本地基线：Baseline toolchain differs；未重建基线、未改用全量、未调用付费模型。该门槛未通过，不冒用此前远端架构CI结果 |

候选API为`wsl-api:native-owner-candidate-20261027`，
镜像ID `sha256:dd318008abd8a9adba2d3a372477671ff33b49999bc5353c82fdbb291b431b27`；
候选Web同后缀，镜像ID
`sha256:b619c4320d4ca607154f382f7e75999e6d80e4d0a6154e5682a7002afe9935b9`。
5173仍使用原wsl-api:local / wsl-web:local，未替换、重启或部署本批候选镜像。
最终只读复核运行库最大迁移仍为015_native_conversations.sql，原接收blocked状态不变；
候选019已在wsl_test及全新smoke库验证，不在运行wsl库执行。

内部连接适配和对账已经具备合成验收，不等于生产发送已注册或真实权限已建立。
可信角色采集授权闭环、禁言/可发言证据、nonce跨旧系统/备份范围及指定隔离群实测
仍未完成；没有回显或证据不完整时保持unknown。引用/@/群管和原生/AI验收门槛仍关闭。

## 远端架构 CI 启用（2026-10-27）

最终状态：PR #21、#22 均已合并；当前 `main` 为
`6f96f29d256c103b8644c1c4b082f529b67b5a40`。
完整平台运行 `40400542847`、兼容基线运行 `40400624878`、
冷启动增量运行 `40401293005` 均通过。最后一次增量 0 模型调用、0 Token；
通过后才恢复 `ARCHITECTURE_MODEL_ENABLED=true`。
两次显式远端基线共 2 次模型调用，供应商报告合计 22,902 Token，不推算货币账单。
本地 90 个业务源码文件指纹与启动前一致，未上传或部署其未提交改动。

后续恢复验证 `40398489211` 暴露上游冷启动 stdout 安装进度与 JSON 结果混排问题，
未产生模型调用或 Token 预留。自动模型开关立即关闭；PR #22 修复严格结果帧解析，
增加模式、路径、冲突及截断回归，共 17 项单元测试通过，未重画无变化 Mermaid。
新工具指纹不冒用旧缓存，待显式基线与恢复验收后再重新开启。

PR #21 仅发布架构工具、工作流和文档，未上传当前本地未提交业务功能。
合并提交为 `601e70a65b8ddc7c836b3b587264c5c54765b035`。
PR 架构运行 `40397037808` 和完整平台运行 `40397037938` 均通过；
后者实测 4 分 32 秒，覆盖 Go race/sodium、vet、独立 PostgreSQL、
产品构建、容器 smoke 和浏览器测试。没有削弱原 `foundation` 合并门槛。
密钥只配置到限制 `main` 的 GitHub 环境 Secret，真实密钥和完整历史扫描均未检出泄露。
本地工具测试增至 14 项并通过，增量复核为 0 次模型请求、0 个报告 Token。
远端模型初始化与后续验证结果见 `docs/architecture/ci-activation.md`。
这些结果不代表当前本地业务版本已上线或原生/AI 容量验收通过。

日期：2026-10-25，时区 Asia/Shanghai。
这是本地开发与基础组件的验证记录，不是全计划验收、生产上线或目标容量通过报告。
未提供或使用真实旺商聊账号、生产台账、外部模型和原生群。
各增量章节按实施顺序记录；早期的「配置为空」等描述保留为当时证据，
当前本机配置状态以「3.7.4 编码与配置增量验证」为准；
2026-10-26 登录错误与网络回退变更见「原生登录网络错误增量验证」。

## 已执行

| 验证 | 结果与范围 |
| --- | --- |
| `go test -race ./...` | 通过。常规测试不会自动打开 PostgreSQL 集成测试或 CGO/libsodium 标签测试 |
| `go vet ./...` | 通过 |
| `WSL_INTEGRATION=1 go test -race -count=1 ./internal/httpapi` | 通过；只重建受保护的本机 `wsl_test` 数据库 |
| `PYTHONPATH=ai python3 -m unittest discover -s ai/tests -v` | 8 项通过；只验证新桥接基础，不验证未改造的 MaiBot 核心 |
| `pnpm --dir web build` | TypeScript 与 Vite 产品构建通过 |
| libsodium Docker 测试 | 通过；独立业务加密向量、原始 8 字节 nonce、metadata/sealed-box 固定样本、签名、SipHash、认证与解压边界 |
| 上游 Python 编码器交叉核对 | 固定 AstrBot 提交的合成请求向量与 Go 编码器一致；不等于登录/消息全链路兼容 |
| API/Web 镜像构建 | 通过；Web 构建使用 Node 22 |
| `python3 tools/container_smoke.py` | 通过；临时全新 PostgreSQL、受限角色、迁移、首次超管创建、非 root/只读 API 与 Nginx、静态资源、实际登录/退出。临时网络/容器及密钥文件在结束时清理 |
| Playwright 本地 WebUI | 实际登录、两个无原生凭据的临时账号、独立筛选与测试状态、独立人设草稿、并发保存冲突、失败不丢输入、人工核对版本恢复均通过 |
| 桌面及移动视口 | 1440×900 与 390×844；抽屉导航及页面无横向溢出检查通过；截图另做人工视觉检查 |

PostgreSQL 集成覆盖会话和 CSRF、超管/用户、跨租户与伪造资源 ID、RLS 及连接池作用域、
共享账号凭据列保护、一次性授权申请及人工批准、并发配额、版本冲突、
测试时间/次数边界、精确分金额/不可变台账/重复流水、跨租户活动外键与开奖时点、
加密 inbox/去重/整批回滚/未知群、旧角色快照与撤权、撤销后不自动转移归属。
这些用例不代表尚未实现的附件、导出、实时订阅或原生执行器已覆盖。

UI 临时账号使用明确的 `ui-smoke-*-20261025` 名称，无原生 UID、连接或群授权。
测试后由带保护条件的 `tools/cleanup-ui-fixtures.sql` 清理，只移除本次人工创建的界面测试数据。
浏览器登录密码和会话文件不纳入测试报告。截图保存在忽略提交的 `output/playwright/`。

## 原生认证增量验证

本轮新增的是认证代码和本地验证，不是真实平台注册/登录验收。未导入真实部署配置，
未发短信、未操作真实原生账号，也没有向旺商聊服务器发测试请求。

| 验证 | 结果与范围 |
| --- | --- |
| `go test -race -tags sodium ./...` | 通过；包含原生认证与原协议加密分支 |
| `go vet -tags sodium ./...` | 通过 |
| `WSL_INTEGRATION=1 go test -race -tags sodium -count=1 ./internal/httpapi` | 通过；只重建 `wsl_test`，不修改开发账号数据 |
| 三协议 HTTP 回放 | 3.7.3、3.7.1、2.8.3；独立验签、请求解密、sealed 交换、旧 token 关联校验、安全码及 JWT 轮换、IM 凭据 |
| 限流及状态机 | 429 不重试、未知/5xx/坏响应不算成功、短信重发冷却不挡验证码验证、换设备挑战与原始 Key、取消、过期、登录频率限制 |
| 账号与凭据 | 事务发起人/账号隔离、重复 UID 拒绝、AES-GCM 账号 AAD、网站/部署/账号版本最终复核；租户角色无法读取协议和凭据列 |
| 请求 ID | 并发同时间戳不碰撞、不回绕；数据库高水位跨分配器保留，时钟回退不重用，旧部署不能分配 |
| 构建 | 本机 TypeScript/Vite、Go/libsodium 和 Linux API/Web 镜像通过 |
| 容器 | 非 root/只读运行、全新数据库 005/006 迁移、网站认证、libsodium 动态库可用、无预置原生部署密钥 |

WebUI 增量检查只读取现有开发账号，未新增、修改或删除账号。验证配置状态、空文件不能保存、
缺协议配置时不显示凭据表单、开户按钮禁用、两个账号弹窗查看范围，以及 1440×960、390×844 布局。
截图时禁用过渡动画并检查稳定边界，避免把弹窗入场动画中间帧误判成溢出。
截图为 `native-accounts-desktop.png`、`native-deployment-desktop.png`、
`native-login-desktop.png`、`native-login-mobile.png`，均位于 `output/playwright/`。
人机验证 SDK 和真实账号尚未实测，不能用这些 UI 检查声称已验证官方开户或真实登录成功。

## 官网开户增量验证

同日根据用户提供的 `www.wwtalk.site` 核验公开下载链及版本列表，静态分析 Android 3.7.4。
APK 自身签名校验通过，文件哈希及接口证据见 `docs/official-client-research.md`。
版本列表是公开只读请求；没有向原生短信、注册或登录接口发真实测试请求。
完整 Jadx 分析有部分失败，未将它当作全协议验证通过。

| 验证 | 范围 |
| --- | --- |
| Go/libsodium 竞态测试 | 独立匿名开户路由、请求验签/解密、专用短信用途、大小写字段、手机号绑定、输入和协议确认、不自动登录 |
| PostgreSQL 集成测试 | `007_native_signup.sql`；发送前持久记录、成功账号与 UID 分离、后续登录身份匹配、明确拒绝后修正、未知结果跨 Manager 重启拦截 |
| 撤销与版本 | 账号版本变化阻止发送；提交后取消仍保存已确认成功结果，取消不能擦除持久屏障 |
| 权限 | 超管/CSRF 边界沿用认证 API；租户数据库角色不能读取开户记录，普通账号列表不查询平台专属开户表 |
| 原生部署缺失的真实 UI | 本地真实 API 返回配置缺失；开户弹窗没有手机号或密码输入，不发送开户 |
| 合成 UI | 1440×960、390×844；人工验证 SDK 和开户 API 均在本地拦截模拟，确认勾选、专用注册短信、成功账号显示、不自动登录、跨账号清空输入及无页面溢出通过 |
| Linux/offline 容器 | 当前源代码的 libsodium 协议/认证测试通过；更新 API/Web 镜像后，临时全新库迁移、租户开户表权限、非 root/只读运行、网站登录退出与静态资源通过 |

UI 复现脚本为 `tools/native_signup_ui_smoke.js`，通过 Playwright CLI `run-code` 执行。
它只允许本地 `127.0.0.1:5173`，使用现有「小满」「2」空面板读取数据，不新增账号或修改业务库。
截图：`native-signup-missing-config-desktop.png`、`native-signup-form-mobile.png`、
`native-signup-success-desktop.png`。后两张展示合成回包，不是已在真实平台完成开户的凭证。
本轮开发库仅增加迁移表；账号仍为两个，原生凭据、部署配置与开户记录均为空。

最终执行 `go test -race ./...`、`go test -race -tags sodium ./...`、
`go vet -tags sodium ./...`、`WSL_INTEGRATION=1 go test -race -tags sodium -count=1 ./internal/httpapi`、
`pnpm --dir web build`、合成 UI 脚本和 `python3 tools/container_smoke.py` 均通过。
最终合成移动弹窗边界为 x=16、宽 358（视口宽 390），并人工查看桌面、手机截图。

Docker Hub 元数据请求连续 EOF，最后一次标准 Dockerfile 重建未完成。
本次使用已存在的 Go 1.27.0/libsodium 构建镜像和 Alpine/Nginx 运行镜像离线生成当前 API/Web：
API 完整重编译；Web 使用刚通过的本机 Vite 产品产物。没有修改生产 Dockerfile 或降低版本，
但不能把这个 fallback 写成「从远端全新拉取并重建成功」。
协议测试以只读挂载当前 `internal/` 的方式在已有构建镜像中执行，禁止网络。
临时构建说明位于忽略提交的 `var/research/official-client-20261025/`；
容器 smoke 的临时网络、数据库与密钥文件已自动清理。

尚未通过真实手机号、官方人机验证联网、当前原生部署参数和 3.7.4 线协议验收；
首期也没有将未知开户直接解锁的自助接口。不能用静态字段核验替代线上兼容验证。

## 3.7.4 编码与配置增量验证

同日继续核对固定 APK 的 SDK、ARM64 交换编码、上下文序列化和生产引导链，
新增独立 3.7.4 分支，不通过替换旧版本字符串冒充兼容。
证据 E10–E16 及离线边界见 `docs/official-client-research.md`。

| 验证 | 结果与范围 |
| --- | --- |
| 官方 ARM64 编码例程 | 有界 Unicorn 执行 `0x2c3a8` 与 `0x2dae4`；合成密钥、统一合法 Zstd raw frame，得到独立交换/AAD/nonce/上下文固定样本，不启动客户端或发网络请求 |
| Go 3.7.4 分支 | Murmur3 请求/设备摘要、交换版本 `1703240058617`、APK 身份摘要、注册 `X-Context`；注册不建立用户会话；旧协议样本不变 |
| `go test -race ./...` | 通过 |
| `go test -race -tags sodium ./...` | 通过；包含官方原生固定样本与四协议合成 HTTP 回放 |
| `go vet -tags sodium ./...` | 通过 |
| `WSL_INTEGRATION=1 go test -race -tags sodium -count=1 ./internal/httpapi` | 通过，8.205 秒；只重建 `wsl_test` |
| Python 配置工具 | 指定 `WSL_CLIENT_APK` 后 4 项全部通过，无跳过；签名引导与篡改拒绝、32/64 位边界解析、受限输出/不覆盖/不泄密、固定真实 APK 身份 |
| TypeScript/Vite 产品构建 | `pnpm --dir web build` 通过 |
| 本机运行服务 | 重新构建 Go/libsodium 二进制并重启本项目 API/Vite；`healthz` 正常，原生连接与 AI 均未开放 |
| 实际 WebUI 配置导入 | 原先版本 0、无配置；通过文件上传和 CSRF 接口加密导入，重新读取为版本 1、协议 3.7.4、`crypto_ready=true`，查询不返回密钥 |
| 配置工具出站边界 | 生成器零网络请求；另外只对签名业务 origin 做一次根路径 HEAD，TLS 成功、HTTP/2 404，不能据此判定业务路由已验收 |
| 实际注册 UI | 使用真实本地 API 创建空事务；桌面/390×844 表单、未确认协议时短信按钮禁用，手机号与昵称保持空白；未点击发送或注册 |
| 实际登录 UI | 同一面板创建独立登录事务；密码/短信模式切换及空输入显示正常，未提交登录或短信，检查后取消事务 |
| 开发数据核对 | 两个原有账号面板保持版本 1、接入和测试开关关闭；原生 UID、加密会话与开户提交均为 0，部署配置为 1 条加密记录 |

实际截图 `native-deployment-374-ready.png`、`native-signup-configured-desktop.png`、
`native-signup-configured-mobile.png` 保存在 `output/playwright/`；
手机截图已人工查看，没有字段或按钮横向溢出。本轮截图不模拟注册成功，
也不包含手机号、密码、验证码或部署密钥。空验证事务在检查后取消。

本轮没有再次重建 API/Web 容器镜像；此前镜像通过记录不能替代本次源代码的容器验收。
目前仅本机 Go 服务运行新 3.7.4 代码。完整依赖/发行审查仍未完成，
新增 Murmur3 BSD-3-Clause notice 已保留。

**仍需人工完成真实手机号、人机验证和受控登录/注册回包核对。**
没有发真实短信、注册或登录原生账号，也没有取得 IM 凭据或打开长连接。
官方编码离线一致、签名引导正确和配置已导入，不等于线上风控/认证已经通过。

## 验证码弹层视口修复

根据用户的 647×779 视口复现真实易盾组件：旧弹层挂在登录表单内部，
验证码框 y=589.85、高 297，底部约 886.85，超出视口并被原登录框裁切。
SDK v2 将 `element` 用作弹层挂载容器，而不是只用作触发点。

现在使用组件独占、Teleport 到 body 的全屏容器，按打开时的视口计算验证内容宽度；
只为该容器内的 SDK 弹层设置最大尺寸。原生登录/注册窗口限制为视口高度减 32px，
正文可滚动，标题和底部按钮不随正文滚动，按钮可换行。没有修改验证算法或放宽验证要求。

`tools/native_captcha_layout_smoke.js` 通过 Playwright CLI 打开真实 SDK，
使用合成表单输入，拦截全部本地原生认证写入；不解验证码、不提交认证或发短信。
断言验证框、登录框和底部按钮均在视口内，没有页面或验证框横向溢出。

| 视口 | 验证码框 x / y / 宽 / 高 | 正文滚动 |
| --- | --- | --- |
| 647×779，原反馈尺寸及错误提示 | 147.5 / 241 / 352 / 297 | 不需要 |
| 390×844 | 19 / 273.5 / 352 / 297 | 不需要 |
| 320×568，错误提示 | 16 / 151.5 / 288 / 265 | 可滚动 |
| 844×390，横屏及错误提示 | 246 / 46.5 / 352 / 297 | 可滚动 |
| 647×480，短窗口及错误提示 | 147.5 / 91.5 / 352 / 297 | 可滚动 |
| 1440×960 | 544 / 331.5 / 352 / 297 | 不需要 |

六种尺寸全部通过；验证框不在 `.el-dialog` 内，属于 `.native-captcha-layer`。
逐项检查关闭验证保留表单输入、关闭面板销毁 SDK 弹层，无残留遮罩；
原生认证提交计数为 0。截图 `native-captcha-{reported,mobile,narrow,landscape,short,desktop}.png`
位于 `output/playwright/`，已人工查看原反馈尺寸、窄屏和横屏。
`pnpm --dir web build` 与相关文件的 Prettier 检查通过。
既有 `tools/native_signup_ui_smoke.js` 也回归通过：仍仅使用合成 SDK/API，
覆盖注册短信、协议确认、注册成功后显式登录和切换账号清空输入；
390×844 注册表单无横向溢出。不能将该合成流程与真实验证码组件布局测试混称为真实开户。
此记录只验收弹层显示与生命周期，不代表真实验证码、注册或登录通过。

## 短时接收回放

实际执行：

```sh
WSL_INTEGRATION=1 WSL_REPLAY=1 WSL_REPLAY_SECONDS=60 WSL_BURST_SECONDS=10 \
  go test -run '^TestReplayLoad$' -count=1 -timeout 5m -v ./internal/httpapi
```

本机 PostgreSQL、本地合成负载、10 个账号队列、100 个原生群标识，单条载荷 517 字节。
ACK 延迟包含本地排队、小批次等待、加密及数据库提交，未包含原生网络 ACK 发送。

| 指标 | 实测 |
| --- | --- |
| 稳态 | 100 条/秒，60.000929 秒 |
| 突发 | 500 条/秒，10.001960 秒 |
| 提交 / 可靠持久化 / ACK 回调 | 11,000 / 11,000 / 11,000 |
| P50 | 4.250 ms |
| P95 | 6.977 ms |
| P99 | 8.562 ms |
| 最大值 | 278.982 ms |

原始报告 `var/replay-report.json` 的 `capacity_accepted`、`native_connected`、`ai_connected` 均为 `false`。
报告生成时间 `2026-10-25T00:37:17.986723Z`。后续显式回放会覆盖该本地报告。
这只是 **60 秒** 稳态测试，不是计划的 **60 分钟**，也不是 24 小时稳定性测试。
没有在目标 8 核/32GB Linux 上测试，没有原生网络、规则处置、模型或全量业务负载；
不能据此承诺生产容量、端到端零丢失、动作恰好一次或纯本地规则 P95。

## 注册与登录统一 TAB

2026-10-25 修复同一超管的注册/登录事务互相占用，却被误报为“另一位超管登录”的问题。
账号行只保留「注册 / 登录」入口，在同一弹窗内切换「原生登录」「新账号注册」。
同一超管的空闲事务原子替换，旧 ID 失效；保留尝试次数、冷却时间及原有效期。
真正不同超管的互斥、执行中禁止切换、未知开户的持久化防重屏障均保留。

| 验证 | 结果与边界 |
| --- | --- |
| Go 单元测试 | 带/不带 sodium 的 `go test -race ./...` 均通过；覆盖 12 次模式替换、旧 ID 作废、空闲客户端清理、另一超管拒绝、执行中/未知状态拒绝、初始化失败保持原事务、冷却及尝试预算不重置 |
| PostgreSQL 集成 | `WSL_INTEGRATION=1 go test -race -tags sodium -count=1 ./internal/httpapi` 通过，8.470 秒；仅重建 `wsl_test`，验证同一超管切换和不同超管不可接管，开户持久化防重复、结果未知及取消后证据测试不变 |
| 前端构建与静态检查 | TypeScript/Vite 产品构建、Prettier、`go vet -tags sodium ./...` 通过 |
| TAB UI 回归 | `tools/native_auth_tabs_ui_smoke.js` 全部原生写入被拦截；6 次来回切换、切换失败保留旧 TAB、执行中/未知时禁止切换、关窗重开恢复事务及 TAB、清理敏感输入、迟到响应不覆盖另一账号 |
| 注册流程回归 | `tools/native_signup_ui_smoke.js` 使用合成短信及注册结果；协议同意门槛、390×844 表单不溢出、注册后显式切登录并预填账号、已注册面板禁止再注册、第二账号不串昵称 |

## 2026-10-25 双工作台与用户权限

本轮交付的是控制面、认证入口及权限隔离，不是原生长连接或 MaiBot 引擎上线。
固定参考 `agent-desk` 的 SHA 已记录在 `upstream.lock.json`，只参考其权限目录、角色模板和用户分配方式，未复制源码。

| 检查 | 结果与边界 |
| --- | --- |
| 数据库迁移 008 | 本地开发库已应用，原有账号默认归旺商聊，不复制、登录或断开原生账号；无开发数据清空 |
| Go 单元与静态检查 | 带/不带 sodium 的 race 测试、`go vet -tags sodium ./...` 通过 |
| PostgreSQL 集成 | `WSL_INTEGRATION=1 go test -race -tags sodium -count=1 ./internal/httpapi` 通过；仅重建 `wsl_test` |
| 工作台边界 | 旺商聊/MaiBot 独立建号和列表；跨池账号认证、开户、清除会话、人设请求被拒绝；普通用户不能进入 MaiBot，聊天工作台不能调用群管、财务、用户、套餐或测试接口 |
| 用户与套餐 | 角色模板/自定义权限、拒绝平台权限授予、版本冲突、套餐模块字段检查、过期后保留历史读取、取消受影响用户的排队任务而不影响其他操作者 |
| AI 申请 | 两群争抢单个 AI 名额仅一项成功；请求保存后 `ai_enabled=false`，没有产生原生操作；数据库拒绝群管/聊天账号错池绑定；撤销群授权同步取消待处理入群申请 |
| 数据库纵深隔离 | 租户角色不能枚举共享 MaiBot 账号或读取平台人设；既有跨租户 RLS 测试不变 |
| 前端构建 | TypeScript/Vite 产品构建与相关文件 Prettier 检查通过 |
| 双工作台合成 UI | `tools/workspaces_ui_smoke.js` 通过；切换账号/工作台均为 0 次写入，各自在统一表单创建一个合成机器人并登录，核验请求账号及工作台一致 |
| 面板与延迟响应 | A/B 草稿独立，跨工作台保留草稿，延迟/取消的旧工作台响应不覆盖新面板；只读用户看不到原生凭据和管理按钮 |
| 布局 | 320、390、647、1440 宽度下顶部控件、统一认证表单无横向溢出；桌面及手机截图人工复核 |
| 既有认证回归 | TAB 往返、执行中/未知拒绝切换、注册后显式登录全部通过；真实验证码 SDK 在 647×779、390×844、320×568、844×390、647×480、1440×960 六种视口无越界，0 次认证提交 |

合成 UI 的所有 API 写入均被拦截，新增机器人仅在脚本内存中存在，不留到开发库。
验证码布局只打开并关闭官方界面，不识别、求解或提交验证。没有向真实手机号发短信、开户、登录、拉群或发言。

截图位于本机 `output/playwright/workspaces-*.png`、`maibot-workspace-desktop.png` 和 `native-captcha-*.png`。
`tools/ui_smoke.js` 已改为真实页面的只读导航检查，不再依赖删除的账号接入菜单或创建开发测试账号。
`native_ready=false`、`ai_ready=false` 仍为硬门槛，以上测试不能解释为“所有账号已在线”或“AI 已开通”。

最新后端重载后的真实页面只读检查通过，切换工作台产生 0 次写入。
开发库最终只读核验：旺商聊保留 2 个原面板，MaiBot 0 个面板；两池已认证与在线数均为 0，没有合成测试面板落库。
| 真实验证 UI 布局 | 新 TAB 下重新执行 `tools/native_captcha_layout_smoke.js`；647×779、390×844、320×568、844×390、647×480、1440×960 六种视口均通过，弹层与底栏不越界，认证提交为 0 |

本轮原生登录、开户与短信请求均未向旺商聊实际提交；模拟注册成功不是线上开户验收。
未重建或重新验收 Docker 镜像。开发 API/Vite 重启加载新代码，持久化账号、会话及协议配置不清除。

## 2026-10-26 Go 引擎、Docker 与工程门槛

用户已确认聊天引擎也改写为 Go；此前保留 Python 生产引擎的方案作废。
`ai/` 仅作为历史契约测试参照，不进入生产镜像。当前 Go 引擎基础不等于完整 MaiBot 移植；
逐项交付边界见 `docs/maibot-migration.md`。

两个工作台分别限制 10 个账号面板、100 个授权群，替代此前共享 10 账号 / 30 AI 群的目标。
原生身份仍全局唯一。上述限制是准入上限，尚未通过目标机容量验收。

| 检查 | 实际结果与边界 |
| --- | --- |
| Go 引擎 | 独立进程、不可变请求快照、4 个全局 worker、每群 1 个执行 / 1 个待处理候选；候选替换、取消、过期、作用域与配置快照测试通过；只返回文本提案，不持有原生发送能力 |
| 外部模型契约 | 隔离 TLS 假服务验证请求、固定来源白名单、禁止跳转、响应大小及错误脱敏；没有调用真实供应商 |
| Go 全量测试 | Docker 中 `go test -race -tags sodium ./...`、`go vet -tags sodium ./...` 均通过 |
| PostgreSQL 集成 | Docker 中隔离重建 `wsl_test`，完整 httpapi race 集成通过，17.979 秒；新增配置加密 / 密钥脱敏 / CAS / 租户角色拒绝 / 跨工作台拒绝 / 两池独立 10 账号并发准入测试 |
| 迁移 009 | 本地开发库已应用；新增密封配置记录及套餐 100 AI 群约束；保留原有数据；迁移文件及哈希固定，后续只能追加迁移 |
| 配置控制台 | 聊天、模型、表达、记忆保存真实版本化配置；返回 `applied=false`；密钥不进入草稿存储；冲突保留输入并支持显式重读 |
| UI 布局 | 采用用户 PNG，侧边栏小按钮切换「旺商聊机器人 / 热群机器人」，顶部独立账号面板；320 / 390 / 647 / 1440 像素无横向溢出，长侧栏可滚动到末项；截图人工复核 |
| Docker 浏览器回归 | `wsl-browser-test:local` 内四组检查全部通过：真实页面只读、双工作台合成、真实 Go 控制台 / Simon、配置草稿与版本冲突；实际业务写入 0 |
| Simon | 固定 0.3.7 镜像摘要；仅私网 `/ws/g` 指标投影；无宿主目录、PID、Docker socket 或公开端口；真实采样通过，界面标记为容器可见系统；并发合并、限长及缓存测试通过 |
| 容器交付 | API / web / Go heatbot / PostgreSQL / Simon 已在本地 Compose 运行，健康检查通过；关闭原宿主 API / Vite；临时容器验收通过，PNG 静态资源可读 |
| 架构与源码 | 依赖边界、Compose 权限、不可变迁移、上游固定版本、源码候选清单、Gitleaks 暂存扫描及 Actionlint 均通过 |
| 前端与历史参照 | TypeScript / Vite、相关 UI Prettier、8 个历史 Python 契约测试通过；Python 仅在测试镜像内使用 |

Docker 切换前已备份本地开发库：
`var/backups/pre-docker-go-console-20261025.dump`，权限 0600，
SHA-256 `5a0fcd1694ef25ccc1641a71267ffc9535d048df5404a02c5063d184249010cc`。
备份、`.env`、原生客户端资料、运行产物不进入 Git。
切换后保留旺商聊 2 个原有面板、热群 0 个面板；已认证 / 在线均为 0。

上述真实页面检查产生 0 次配置 / 原生 / 模型 / 群管写入；
合成配置检查的 2 次保存全部由内存 API 拦截，不修改开发库。
`native_ready=false`、`ai_ready=false`，生产配置禁止测试生成。
100 群完整 AI 能力、原生长连接、端到端权限复核及持续负载均未验收。

## 远端 CI 状态

专属仓库 `DH-devmax/ARTWSLBOt` 已初始化并推送。
GitHub Actions `platform-checks` 运行 `40213188096` 对提交
`72e51908064bc4a1585fcfb21acb086d1fb1c837` 的完整验收结论为 **success**。
覆盖源码 / 迁移 / 工作流检查、Gitleaks、产品类型检查及构建、Docker Go race / vet、
架构边界、历史契约测试、PostgreSQL 集成、独立容器验收、完整 Compose
启动及四组容器内浏览器检查。镜像发布工作流尚未触发，没有发布到生产服务器。

首次运行 `40212823709` 暴露了干净检出缺失框架示例自动生成类型的问题。
修复为 `tsconfig.wsl.json` 显式检查实际产品入口，并从 Docker 上下文排除
自动生成的声明文件；复验从干净检出通过，没有跳过实际产品类型检查。
后续提交的状态以其对应 Actions 运行结果为准。

协议兼容向量仍是本地隔离验证；真实页面检查为只读，
写入场景使用合成 API，临时容器及远端 CI 只使用其独立数据库。
不应在生产库执行 UI 测试或清理脚本。

## 2026-10-26 机器人删除与关联清理

新增超管删除预览和按名称确认入口，覆盖两个工作台。迁移 010 允许保留停用的租户群归属，
不授予删除财务记录的权限。具体清理、保留及离线备份边界见 `docs/account-removal.md`。

| 检查 | 实际结果与边界 |
| --- | --- |
| Go / PostgreSQL | sodium race、vet、完整隔离 httpapi 集成通过；仅重建 `wsl_test`，首次完整集成耗时 17.102 秒 |
| 清理与保留 | 测试验证账号、密封配置、人设、收件箱、群关系、授权码、申请、注册及操作记录物理删除；群归属、共享目录、其他账号消息、财务金额及活动保留 |
| 一致性 | 执行中/未知结果及提交中注册阻止删除；意外外键依赖整笔回滚；并发删除仅一项成功；已删除群归属不能继续保存配置 |
| 认证与权限 | 进行中的合成登录不能重建已删面板；管理器事务撤销；租户、错工作台、缺 CSRF、错误名称、旧版本被拒绝 |
| 浏览器 | 新增删除回归并纳入 Docker CI；取消无写入、错误名称禁用、版本冲突保留输入、独立工作台清理、草稿不串账号、未知结果阻断、租户无入口；320 / 390 / 647 / 1440 视口通过 |
| 既有回归 | 真实页面只读、双工作台切换、Go 配置控制台和 Simon 检查通过，临时容器初始化及运行权限检查通过 |
| 本地数据 | 应用迁移 010 前建立 0600 备份，更新 Docker API/web；保留原有 2 个旺商聊面板、0 个热群面板，真实删除及原生写入均为 0 |

备份 `var/backups/pre-account-removal-20261026.dump`，SHA-256
`cb0463757da2472333c21ab1e8253e6a79f4bce18d02728f4bdd805bbd69f2e1`。
截图位于 `output/playwright/account-removal-*.png`，macOS 浏览器人工复核中文内容及底部按钮。
容器浏览器补充中文字体，避免字体缺失使截图无法审核。

未执行真实账号删除、官方注销、短信、拉群或模型调用。删除清理的是当前在线库及当前浏览器，
不宣称擦除 WAL、离线备份、其他浏览器或供应商数据。大量历史消息的清理时延尚未压测；
事务超时或依赖异常不能回报成功。原生连接及 AI readiness 门槛继续关闭。

## 2026-10-26 单账号工作台与消息指标

账号选择后展示独立工作台；平台汇总、全账号列表和容量目标仅保留在“工作台总览”。
指标定义、权限边界及未接入的实时监测见 `docs/account-dashboard.md`。

| 检查 | 实际结果与边界 |
| --- | --- |
| Go / 数据库 | sodium race、vet、完整隔离 httpapi 集成通过，完整集成耗时 15.826 秒；只重建 `wsl_test` |
| 账号与租户范围 | 两租户共享机器人、独立机器人、独立热群账号、错工作台、伪造租户参数、权限缺失及撤销授权均通过；RLS 业务聚合不暴露共享原文 |
| 消息统计 | 250 条以上样本聚合不受页面分页影响；60 秒速率、非负时间差分位数、异常时钟样本排除、15 分钟补零桶、积压与未知结果、空样本 null 均通过 |
| 浏览器 | 六组 Docker 浏览器检查通过；320 / 390 / 647 / 1440 视口、图表像素与自适应尺寸、迟到响应隔离、同账号失败保留采样、切换失败不沿用旧账号数据、用户无原生控制入口 |
| 回归 | 既有注册登录表单、双工作台草稿、删除预览与版本冲突、Go 配置控制台、Simon 和架构检查通过；临时全新库容器 smoke 通过 |
| 真实只读 | 现有两个旺商聊面板逐一查询独立指标通过；热群 0 面板，已认证 / 在线均为 0；未提交原生、模型、群管、配置或删除请求 |
| 本地部署 | 备份开发库后应用仅新增索引的迁移 011/012，更新 Compose API/web；服务健康，readiness 仍为 false |

API Dockerfile 的 Go 构建阶段在容器中完成当前源码编译，本机 TypeScript/Vite 构建通过。
Docker Hub 对 Alpine/Nginx 元数据请求连续 EOF，完整标准运行镜像的本地重建未成功；
本地使用已有 API/Web 运行镜像缓存装入新 Go 二进制和前端产物后通过容器验证。
未修改正式 Dockerfile、版本、部署方式或 CI 检查；远端 CI 仍执行完整标准镜像构建。
浏览器运行器使用既有容器镜像并只读挂载当前 CI 脚本。

开发库备份 `var/backups/pre-account-dashboard-20261026.dump`，权限 0600，SHA-256
`0f355e351730170ce018cff921794ff9e811c081333aa80846f11a5f6c521075`。
备份及临时构建目录不纳入 Git；原有账号、会话和业务记录保留。
截图 `output/playwright/account-dashboard-{320,390,647,1440}.png` 为合成指标，
已人工查看桌面与手机图；不代表真实平台已产生消息或通过性能容量验收。

入库延迟仅到数据库 INSERT，不能当作可靠提交或 ACK 时延。实时心跳、内存队列、
原生读取限流及 ACK 尚未采集，界面明确为“暂无监测”，不生成健康百分比或虚假零时延。
这次没有执行目标机压力、24 小时稳定性或真实账号测试。

## 原生登录网络错误增量验证

日期：2026-10-26，时区 Asia/Shanghai。问题与 Docker 设置见
`docs/native-auth-network.md`。本轮读取了用户已提交事务的安全状态字段，
未读取/重放凭据、发送短信、自动登录或自动注册。

| 检查 | 实际结果与边界 |
| --- | --- |
| 旧事务 | `unknown / native_transport_unknown`，无上游 HTTP 返回码；旧版没有保留底层错误，不能据此判定密码或验证码被拒绝 |
| 网络复现 | 容器中相同 Go 客户端 HEAD 官方域名首页；回退关闭为 `native_dns_fake_ip`，开启后取得 HTTPS 404。只证明公网连接和 TLS 可用 |
| Go 单元与竞态 | sodium race、vet 通过；覆盖 fake-IP、混合私网、公网、无效 DNS 回答、固定回退失败、未发送与未知结果分类、有限诊断传递和不自动重试 |
| PostgreSQL | 隔离 httpapi 集成通过；原有未知开户跨重启拦截继续有效；未发送的 DNS 拦截不应写为已提交结果未知 |
| WebUI | 七组 Docker 浏览器检查通过；新增合成 SDK 先失败后通过、真实错误替换、重复回调只提交一次、关闭后的迟到回调不提交、未知不重试 |
| 布局 | 320 / 390 / 647 / 1440 登录错误弹窗边界通过；已人工查看手机截图 |
| 构建与部署 | 标准 API/Web Dockerfile 构建通过；本机 Compose API/Web 更新且健康；本机启用加密 DNS 回退，默认部署仍关闭 |
| 基础回归 | 架构检查、产品 TypeScript/Vite、Prettier、8 项历史 AI 契约、临时全新库/受限角色/非 root/只读容器 smoke 均通过 |

错误回包只附带限定的网络诊断类别及既有上游返回码，不暴露底层 URL 或敏感内容。
旧验证码和登录事务不会自动重放。真实开户、登录、消息接收和容量验收仍未通过，
`native_ready` / `ai_ready` 均保持 false。

截图 `output/playwright/native-auth-errors-{320,390,647,1440}.png`
均为合成回包，不是实际旺商聊登录结果。

## 2026-10-26 消息接收器与实时观测

时区 Asia/Shanghai。迁移、协议证据、恢复边界见 `docs/native-receiver.md`。

| 检查 | 实际结果与边界 |
| --- | --- |
| 协议与竞态 | sodium race、vet 通过；覆盖群/私聊路由、完整登录回包、uint64 ACK、同步边界 FIFO、心跳关联、踢线终止、未知边界阻断和保存会话绑定 |
| 数据库 | 完整隔离 httpapi 集成 18.698 秒通过；接收器专用隔离测试通过：跨连接核对提交后 ACK、去重、旧 epoch 拒绝、毒包后不推进水位、原文加密绑定、IM 群 ID 不写入业务群 |
| 权限与启停 | 接收开关携带 revision，普通租户禁止启停及访问共享遥测；接收启停不删除凭据、不打开原生动作；删除机器人级联清理接收事件、租约与水位 |
| UI | 八组 Docker 浏览器检查通过；新增接收开关两次合成写入、320/647/1440 布局；已有 320/390/647/1440 账号/租户/工作台隔离及图表像素测试继续通过 |
| 构建与部署 | 标准 API/Web Dockerfile 构建和全新数据库/受限角色/非 root/只读容器 smoke 通过；迁移 013 已应用，本机 Compose 服务健康 |
| 真实账号 | 用户此前手动登录的加密会话可读，离线设备/协议恢复绑定通过；缓存 IM 令牌被拒绝后的换票返回需要重新验证。没有观察到真实消息连接或心跳，未自动重新登录 |
| 门槛 | 原生动作及 AI readiness 仍为 false；本轮不启用群管、排行、财务、AI，不宣称容量或端到端验收通过 |

迁移前已一致备份数据库，文件权限 0600，SHA256：
`ec2943601ada95492f13a8b1a72bf70d484907970160e8d3082a3012e7b06b1d`。
原始消息事件保留 7 天；尚未映射或解密的业务数据保持暂停，不回放旧管理命令。
真实联调下一步需要用户先停用接收，在 WebUI 重新原生登录，再启用接收。
截图 `output/playwright/native-receiver-{320,647,1440}.png` 为合成数据，不能作为真实在线证据。

## 2026-10-26 功能管理与设备身份

本轮整理现有配置与入口，没有开放原生规则执行。字段清单见
`docs/feature-center.md`，终端证据与未确认原因见 `docs/native-device-sessions.md`。

| 检查 | 实际结果与边界 |
| --- | --- |
| 设备身份 | 离线验证业务/IM 指纹稳定且互相独立；API 仅返回摘要，未返回原始设备 ID 或密钥；登录表单标明 Android 兼容接入及共存未验收 |
| 功能目录 | 15 类功能统一入口；4 类可保存开关与既有规则；不适用字段显示“无”，未迁移能力没有可点击假开关；保存 `applied=false` |
| 字段与套餐 | 两租户、共享账号、单字段授权、越权字段、套餐缺模块/过期、旧版本、停用操作者最终复核、热群工作台拒绝及审计不含回复正文均通过 |
| 旧配置与清理 | 沿用群配置 JSON，无新增迁移；旧群接口往返保留新开关，并拒绝绕过独立字段接口修改；现有机器人删除路径清空整个群配置 |
| Go / 数据库 | Docker sodium race、vet 通过；完整隔离 httpapi 集成 22.766 秒，随后 gateway 集成 1.129 秒通过；只重建 `wsl_test` |
| 浏览器 | 最终九组 Docker 回归通过；功能页版本冲突保留输入、人工核对后采用新版本、账号草稿隔离、迟到回包与只读禁改通过；真实原生写入 0 |
| 视口 | 功能页 320/647/1440，新增设备提示的登录错误弹窗 320/390/647/1440 均通过；桌面表格、手机横向滚动区域及窄屏登录截图已人工查看 |
| 工程检查 | 产品 TypeScript/Vite、Prettier、仓库检查、8 项历史契约及架构检查通过；新增 features 依赖边界禁止数据库、HTTP、原生及其他非标准库依赖 |
| 容器 | 标准 API/Web Dockerfile 最终构建并更新本机 Compose，健康检查通过；全新数据库、受限角色、非 root/只读容器 smoke 通过 |

Docker Hub 元数据请求首次断线，随后重试标准构建成功，没有使用替代运行镜像。
本机默认 Docker 网络池不足，临时 smoke 显式使用已核对空闲的私有 /28 网络；
只清理该测试自己的容器和网络，未改生产网络、未清理其他项目。
最初两处回归夹具因旧字段定位和缺 JSON 请求头失败，修正后完整重跑通过，
没有跳过权限或界面断言。截图保存在忽略提交的 `output/playwright/features-*.png`
和 `native-auth-errors-*.png`，数据与原生写入均为合成。

固定官方 APK 显示 IM 区分 PC/Web 客户端；当前 Go 的业务登录仍是 Android 兼容协议，
IM 则为 Web 类型。手机退出可能涉及业务会话替换，但没有取得本次退出事件或完整上游规则，
不能宣称已定位到具体互踢层或已修复手机/机器人共存。本轮没有修改设备 ID、
协议标志、凭据、接收开关，也未重试原生登录、注册、换票或发送短信。
真实只读检查为旺商聊 1 个面板、已保存认证 1 个、在线 0 个；认证标记不等于当前 IM 在线。
`native_ready=false`、`ai_ready=false`，不把已保存开关当作功能已运行。

### 2026-10-26 功能目录与模块依赖补充

用户要求区分旺商聊原生能力与基于原生能力工作的业务模块。新增只读原生表及
模块依赖关系，不声称仅有目录或配置即完成原生执行。

| 检查 | 实际结果 |
| --- | --- |
| 目录 | 24 项原生能力；22 项有旧接入器/官方客户端证据，好友审批和邀请入群提交契约待核验 |
| 模块映射 | 业务模块明确列出原生依赖；原生支持、项目接入、当前账号状态分别呈现 |
| Go / 数据库 | Docker 全量 sodium race、vet、架构检查通过；完整隔离 httpapi 集成 22.303 秒，随后 gateway 集成 1.122 秒通过，仅重建 `wsl_test` |
| 前端构建 | `deploy/web.Dockerfile` 构建及 `vue-tsc` / Vite 生产构建通过；Compose Web 健康 |
| 容器 | 最终 API/Web 镜像在临时空数据库完成 smoke：受限角色、迁移、非 root/只读运行、静态资源和网站认证通过；未使用生产数据 |
| 浏览器 | 完整九组 Docker 浏览器回归通过；功能详情、版本冲突、账号草稿/迟到响应隔离、只读用户和 320/647/1440 视口通过 |
| 安全边界 | 合成 UI 配置写入 3 次；真实原生请求/写入 0；`native_ready=false`、`ai_ready=false` |
| 源码 | `python3 tools/check_repository.py`、`git diff --check`、产品 Prettier 检查通过 |

原生目录状态是源码证据和项目实现进度，不是旺商聊当前服务端能力保证；
最终发送与群管动作仍需单独实现、角色/租户/套餐/期限复核和隔离原生验收。
新增 UI 回归曾因 Element Plus 隐藏输入框和弹窗遮罩/动画定位失败；
改为点击可见控件、等待实际弹窗内容并关闭截图动画后，完整九组重跑通过。
人工查看 `output/playwright/native-catalog-{320,1440}.png` 和
`native-detail-320.png`，未发现文本重叠或弹窗溢出。截图使用合成群数据。

## 2026-10-26 接收启动与恢复稳定性

本轮优先处理接收链路，不新增群目录映射、原生发言、群管或 AI 执行。
实现与恢复边界见 `docs/native-receiver.md`。没有修改迁移、设备身份或原生凭据。

| 检查 | 实际结果与边界 |
| --- | --- |
| 启动顺序 | 持久化消费者先于首个心跳及同步启动；合成登录后立即下发 80 条消息，全部落盘并 ACK 后才完成同步 |
| 同步证据 | 数据库行锁延迟提交时保持同步中；收到心跳和同步回包也不能提前显示健康；提交及前序 ACK 完成后才记录 `synced_at` |
| 故障分类 | 同步断线不再误报未知边界；非法边界和心跳前毒包仍阻断且不推进水位；LBS/WSS 429 遵守 Retry-After；未知或无效换票回包阻断自动恢复 |
| Go | Docker 全量 sodium race、vet 通过；新增重试分类和限流向量；架构检查、仓库检查及 `git diff --check` 通过 |
| PostgreSQL | 完整隔离 httpapi 集成 22.463 秒通过，随后 gateway 集成 1.177 秒通过；包含提交后 ACK、延迟同步、80 条突发、断线与毒包测试；仅重建 `wsl_test` |
| UI | 完整九组 Docker 浏览器回归通过；恢复入口停收失败保留原状态，成功后打开同账号表单，切换账号后忽略迟到响应；5 次合成接收开关请求、1 次本地合成验证事务、真实原生请求 0 |
| 视口 | 接收面板 320/647/1440；已人工查看 320 窄屏及恢复表单截图，未发现重叠或弹窗溢出；既有账号、工作台和权限回归继续通过 |
| 构建 | 标准 API/Web/Test Dockerfile 构建通过；产品 TypeScript/Vite、Prettier 通过；Docker Hub 首次元数据读取断线后正常重试成功 |
| 容器 | 最终镜像在临时全新数据库完成迁移、受限角色、非 root/只读运行、静态资源与网站认证 smoke；未使用生产数据 |
| 部署 | 本机 Compose 已更新，API/Web/heatbot/PostgreSQL 健康，Simon 运行；没有自动提交原生登录、注册、短信或群动作 |
| 真实只读复核 | 更新前后均为 `enabled=true / blocked / native_reauth_required`；心跳、收包及 ACK 均未观测到，队列为 0，延迟无样本 |

当前真实账号仍需人工重新验证。工作台新增“重新验证账号”，按版本停收成功后
打开原生登录表单；不会自动登录或重新开启接收。登录完成并由用户重新启用接收后，
才能继续真实心跳、消息、断线恢复和 ACK 验收。此前手动登录成功不等于该链路通过。
`native_ready=false`、`ai_ready=false`；本轮没有执行生产容量或 24 小时稳定性测试。
截图 `output/playwright/native-receiver-recovery.png` 与
`native-receiver-{320,647,1440}.png` 使用合成数据，不作为真实在线证据。

## 2026-10-26 回调认证修正与登录入口

本轮针对用户已重新登录但消息协议仍被拒绝的问题。对照锁定 AstrBot 与
固定官方 APK，不再把 IM 拒绝一概解释为密码登录过期。证据与桌面边界见
`docs/official-client-research.md`、`docs/native-device-sessions.md`。

| 检查 | 实际结果与边界 |
| --- | --- |
| 会话只读诊断 | 保存会话离线绑定通过，使用 3.7.4 / 回调认证类型 2；未输出 JWT、令牌、原生 UID、设备原值或密码 |
| 协议修正 | 动态/回调先换票、加密保存再连接；回调握手补属性 116 时间扩展；显式识别官方 `DYNC` 枚举；没有改密码学构件、设备 ID 或 nonce 格式 |
| 合成向量 | 静态/动态/回调、Android/PC 分支、独立令牌与时间扩展、每次连接刷新时间、非法认证方式拒绝、合成 302 诊断不泄漏上游正文均通过 |
| Go | 最终标准 Test Dockerfile 构建；全量 `go test -race -tags sodium -count=1 ./...`、`go vet -tags sodium ./...` 通过；8 项历史 AI 契约通过，不部署 Python 运行时 |
| PostgreSQL | 完整隔离 httpapi 集成 21.993 秒通过，随后 gateway 集成 1.220 秒通过；仅重建 `wsl_test`，新增同为 enabled 时人工重连清阻断、旧 revision 拒绝、凭据及原生动作状态不变断言 |
| 浏览器 | 最终九组 Docker 回归通过；重新登录统一先停收，停收冲突保留原状态，迟到响应不串账号；直接重新连接不创建密码验证事务；安全数字码及失败阶段可见 |
| 浏览器写入 | 接收回归 7 次合成开关写入、2 次网站本地合成验证事务、真实原生请求 0；未自动发短信、开户、登录或群动作 |
| 视口 | 320 / 647 / 654 / 1440 接收及总览入口、320 / 390 / 647 / 1440 登录错误弹窗回归通过；人工查看窄屏总览按钮与登录表单截图，无重叠或越界 |
| 容器与工程 | 标准 API/Web 镜像构建、产品 TypeScript/Vite、Prettier、架构/仓库检查、`git diff --check` 通过；临时全新库、受限角色、非 root/只读容器 smoke 通过 |
| 本机部署 | Compose API/Web/heatbot/PostgreSQL 健康，Simon 运行；没有新增迁移、修改设备身份或原生凭据；整体 `native_ready=false` / `ai_ready=false` |
| 真实只读复核 | 修正前后均为保留快照 `enabled=true / observed=false / blocked / im_auth_rejected`，心跳、收包及 ACK 未观测；不是修正版已经再次连接失败的证据 |
| 桌面研究 | 官方 macOS x64 2.8.3 公开包只读解包，自身代码签名验证通过；未运行客户端或原生 SDK。旧 PC 分支已标明桌面终端，但本版本完整兼容与手机共存仍未验收 |

本机新 API 镜像为
`sha256:3e10629f0040f7e115643288ec6e156719fc80f18ad02ed2e1dce26d7cf4458f`，
Web 镜像为
`sha256:ad46a4012d08e7a3e271b39a7dfef54d7fb4446fee6f7ca68e2ab0175846131c`。
完整浏览器回归首次因旧夹具仍按链接名称定位失败，修正为新按钮及接收状态后重跑通过，
未跳过断言。临时容器 smoke 使用已核对空闲的私有 /28 网络，没有清理其他项目网络。

截图 `output/playwright/native-overview-login-654.png`、
`native-receiver-recovery.png` 及 `native-receiver-{320,647,1440}.png`
均为合成数据。真实账号仍需用户显式点击账号面板“重新连接”，
才会沿用保存会话执行修正后的握手。除非新结果明确要求原生人工验证，
不能再以无数字码的旧 IM 拒绝快照要求重复密码登录。
本轮没有真实长连接、桌面登录、多端共存、100 条/秒或 24 小时验收结果。

## 2026-10-26 移除移动登录接入器

按用户要求只保留桌面 2.8.3 业务认证，消息接入仍使用独立 Web IM；
不把修改终端标志或离线合成通过当作真实桌面兼容。详见 `docs/native-auth.md`。

| 检查 | 实际结果与边界 |
| --- | --- |
| 认证入口 | 删除移动协议分支、APK appHash、移动请求 ID 通道及默认版本回退；空版本、3.7.1/3.7.3/3.7.4 和其他版本均拒绝；导入头不能覆盖桌面终端标志 |
| 会话及连接 | Manager、Client、会话恢复、接收加载和消息拨号均拦截移动配置/旧会话；静态 IM 认证不能绕过；没有自动换设备、重登或发送原生请求 |
| 合成协议 | 桌面签名、XXH3、metadata、sealed exchange、响应关联、安全码、JWT 轮换、IM 换票及 429/未知结果不重试通过；非 sodium 构建的目标包测试通过 |
| PostgreSQL | 修正两处旧夹具后，完整隔离 httpapi sodium race 集成 30.309 秒通过，随后 gateway 集成 1.241 秒通过；只重建 `wsl_test`；覆盖旧配置不可用、桌面显式导入和迁移保留数据/提升栅栏 |
| Go / 工程 | 全量 sodium race、vet、架构检查、产品 TypeScript/Vite、Prettier、仓库检查及 diff 空白检查通过；8 项历史 AI 契约通过，不部署 Python 运行时 |
| 离线工具 | 历史 APK 检查改为只读；移动配置输出在读文件前拒绝；3 项合成工具测试通过，1 项可选真实 APK 检查未运行；没有运行官方客户端 |
| 浏览器 | 九组 Docker 浏览器回归通过；桌面提示、旧配置无可提交表单、无新验证事务、账号/工作台隔离和既有权限检查通过；真实原生请求 0 |
| 布局 | 登录错误弹窗 320/390/647/1440，接收面板 320/647/654/1440 回归通过；人工查看 `desktop-only-migration-647.png`，未发现溢出 |
| 容器 | 标准 API/Web/Test 镜像构建通过；全新临时库、受限角色、非 root/只读容器 smoke 通过；本机 Compose API/Web/heatbot/PostgreSQL 健康，Simon 运行 |
| 迁移 | API 停写后完成 0600 一致数据库备份并检查归档目录；014 已应用及锁定哈希；1 个账号与 1 份加密凭据保留，账号需重新认证，启用接收由 1 变 0，原生动作保持关闭 |
| 数据核对 | 迁移前后账号数均为 1；群授权、财务行数/金额和操作数均为 0，没有删除或新建业务数据 |

备份为忽略目录中的
`var/backups/pre-desktop-only-20261026-201836.dump`，SHA256：
`2357987dcfa3890e2559a4fd82bd3a7f214f52811a1c05be7d3f91bf48349562`。
归档目录可读不等于完成生产恢复演练；主密钥仍由原受保护环境配置持有。
本机 API 镜像为
`sha256:575824f15cc355e2cf73033461446a615531000e4c3ac8b2c41f4a111744d35f`，
Web 为
`sha256:41eb0ea970f9f4bbe3ec04f3aa7b09fdd9ef7c3ad9f759bcbb62eda6214248f3`。

实际部署只读接口返回 `nativeAdapter=desktop`、`nativeConfigured=false`、
`desktopMigrationRequired=true`；已保存认证和在线账号计数均为 0。
当前保存的旧移动部署被标记为待迁移，不能用于桌面认证。尚未取得独立核验的
桌面密钥/引导配置，未自动沿用 Android 密钥或改写旧票据。必须先导入桌面配置，
再由用户人工登录并显式启用接收。真实桌面开户、登录、长连接、手机共存和容量
尚未验收，`native_ready=false` / `ai_ready=false`。

## 2026-10-26 独立桌面配置打通

用户已删除原机器人面板。本轮不恢复旧面板或旧移动会话，目标是提供真实可核验的
桌面配置与人工认证入口。固定官方桌面客户端仅读取、复制和静态分析，未运行程序。
E21–E25 的来源、哈希和证明边界见 `docs/official-client-research.md`。

| 检查 | 实际结果与边界 |
| --- | --- |
| 桌面来源 | 固定 macOS 2.8.3 `mclient`、主进程及配置 JS 完成两级证书/内容签名校验；根和交换公钥独立来自桌面材料，未从 Android 配置改版本 |
| 终端身份 | macOS 固定 `X-Client=10`，Windows 分支固定 11，均为 `X-Device=1`；未知平台拒绝；保存会话的平台不符不能恢复 |
| IM 修正 | 桌面回调 `loginExt=token`，属性 116/1000 使用本次相同令牌；每次换票同时更新，删除桌面连接中的 Android 日期扩展 |
| 私有导入 | 通过超管网站会话及 CSRF 调用既有版本化 API，桌面部署版本变为 2，独立桌面 IM 配置已加密保存；密钥、密码、JWT、IM 令牌未打印或提交 |
| 真实匿名请求 | 2026-10-26 21:29:49（Asia/Shanghai），一次 `/v1/settings/get-sys-cfg` 加密请求返回成功，验证码开启；没有账号注册、短信、原生登录、换票或群操作 |
| 新入口 | 超管手动“验证配置”；固定匿名路由、持久化 nonce、单进程串行/30 秒冷却、20 秒期限、配置/会话复核，无自动重试、无任意路由代理 |
| Go | 标准 Docker API/Test 构建、全量 sodium race、vet、架构检查通过；覆盖 macOS/Windows 头、严格布尔响应、耐久请求编号与平台恢复栅栏 |
| PostgreSQL | 完整隔离 httpapi sodium race 集成 21.467 秒通过，随后 gateway 集成 1.243 秒通过；只重建 `wsl_test`；新增租户拒绝、版本冲突、冷却和迟到配置结果拒绝 |
| 离线工具 | 9 项检查中 8 通过、1 项历史 APK 可选检查跳过；本轮可选固定桌面材料检查实际通过；覆盖哈希、符号链接、独立设备身份、私有文件不覆盖、IM 来源唯一性 |
| 前端 | 产品 TypeScript / Vite、Prettier 通过；十组 Docker 浏览器回归全通过，新增配置验证仅使用合成响应，真实原生请求 0 |
| 布局 | 配置弹窗 320/647/1440 回归通过；人工查看 `desktop-config-320.png` 与 `desktop-config-1440.png`，正文可滚动、底部按钮换行，未发现重叠或越界；截图为合成数据 |
| 工程 | 仓库检查、`git diff --check`、GitHub 工作流 actionlint 通过；CI 新增合成离线工具测试；8 项历史 AI 契约通过，不部署 Python 运行时 |
| 容器 | 最终 API/Web 镜像完成全新临时库、迁移、受限角色、非 root/只读运行与网站认证 smoke，通过；未使用生产数据 |
| 本机复核 | Compose 已更新；真实只读接口返回桌面 configured=true / migration_required=false，两工作台面板均为 0、认证 0、在线 0，Simon 可用；readiness 均为 false |

导入前创建 0600 数据库备份
`var/backups/pre-desktop-profile-20261026-212908.dump`，91940 字节，
SHA256 `c4408ec28c344ba179de028559bf066a28f03878fdadcf2a18d185a396848c5e`。
仅更新受控协议配置，没有新迁移、重置开发库、创建机器人面板或写入群业务数据。
公开 DMG 的只读研究挂载已卸载。

本机 API 镜像
`sha256:4e5735e35c9264bbc40d72ab9f4e827a23e8efb87e972c13403d3b68f78dbe90`，
Web 镜像
`sha256:7489f41436aa922bf958540e1a0170932ae5256b3811c86eb958fdf9833f73a3`。
最终容器 smoke 使用已核对的私有 /28 网络，没有清理其他项目网络。

匿名配置接口通过不代表真实账号已经登录；服务端交换公钥尚需实际人工登录挑战验证，
IM 回调修正仍需取得真实桌面票据后联调。用户在顶部填写机器人名称，使用已有旺商聊
账号选择“原生登录”并人工完成人机验证；本地面板删除不代表要重新开户。
原生桌面登录、心跳、消息落盘/ACK、断线恢复、手机共存、容量及稳定性尚未验收，
`native_ready=false`、`ai_ready=false`，不能因本项成功而自动开启动作或消息接收。

## 2026-10-26 桌面消息连接真实联调

用户已手动完成桌面登录并明确要求连接完善。本轮只启用既有账号的消息接收，
不自动重做原生登录、不发短信、不注册、不发送群消息，也不执行群管或 AI。

| 检查 | 实际结果与边界 |
| --- | --- |
| 首次拨号 | 保存会话成功换 IM 票、握手及心跳，随后在 `14/9` 事件通知触发严格边界保护；不是密码登录失败 |
| 协议修复 | 对照固定桌面 SDK 补齐 `14/2`/`14/9`、`4/25`、嵌套 `7/116` 三类非聊天通知，验证完整字段与边界；只保留原包，不执行动作或推进独立水位 |
| 兼容扩展 | 真实 `7/116` 比 SDK 基础结构多 8 字节；只接受该固定扩展或基础长度，不解释扩展含义，截断、额外字节与未知内部路由仍拒绝 |
| 真实收包 | 修正前已持久化并写出 913 次聊天 ACK，随后遇已读通知暂停；本轮最终连接 22:21:01 建立，22:21:02 首次同步完成，22:22:40 为 connected |
| 最终连接样本 | 上述时点 187 条接收、187 次 ACK 写出、1 次重复、本轮重连 0、隔离 0、积压 0，心跳持续更新；ACK 仅证明 socket 写出成功 |
| 实际延迟 | 当前最近 187 样本落盘 P95 18.491ms、ACK 写出 P95 20.544ms；此前 913 条集中补收为 265.206ms / 325.804ms，不是 100 条/秒压测 |
| 加密证据 | 旧 3 个隔离事件未删除或伪装为处理成功；修正后只读解封验证三种通知均可解析，诊断未打印正文、原生身份或凭据；临时诊断源码已移除 |
| Go | 最终标准 API/Test 镜像构建、全量 sodium race、vet 及架构检查通过；单包非 sodium 测试通过，未知路由仍暂停 |
| PostgreSQL | 最终完整 httpapi sodium race 集成 21.502 秒通过，随后 gateway 1.178 秒通过；串行只重建 `wsl_test`，覆盖三类通知先于心跳、持久化、同步完成且不误发聊天 ACK |
| 前端 | 本轮没有 UI 源码变更；标准 Web Docker 构建通过（产品构建层命中已验证缓存），实际网站状态继续由原接收观测接口提供 |
| 浏览器 | 十组 Docker 浏览器回归全部通过；真实只读检查确认已登录 1、已连接 1、desktop configured=true；切换账号不写入，其他认证/开关/删除/配置写请求全部拦截为合成响应，真实原生请求 0 |
| 容器 | 最终 API/Web 镜像通过全新临时数据库、迁移、受限角色、非 root/只读容器及网站登录退出 smoke，未用生产数据；早次 smoke 曾遇临时端口映射检查失败，原样复跑及最终镜像检查均通过 |
| 部署 | Compose API/Web/heatbot/PostgreSQL 健康；API 镜像 `sha256:6995dfe2e030cc370e50545101eec7eaa2877739cce62687553e3529b06ed17e`；未更换凭据或新建迁移，原生业务动作开关仍关闭 |

这次取得真实单账号的消息连接、首次同步、收包、去重、落盘和 ACK 证据，
不再要求用户为了旧阻断快照反复密码登录。原始消息尚未解密并关联业务群，
不恢复旧管理指令，不启用规则、排行、财务或 AI；长期稳定性、恢复与容量仍需验收。
整体 `native_ready=false`、`ai_ready=false`。

22:25:24 再次只读复核：该连接仍为 connected，有效租约及新鲜心跳，
累计 236 条消息、236 次 ACK 写出、重复 1、本轮重连 0、隔离 0、队列深度 0。
落盘 P95 18.491ms、ACK 写出 P95 20.515ms。连续观测约四分钟，
并非 24 小时稳定性或容量验收，未改变 readiness。

## 2026-10-26 只读群组、私聊与中心化解码

本轮依据用户要求添加原生群目录、已接收私聊/群聊的应用消息解码及健康面板。
只读取已登录桌面账号并显式重启接收；没有密码登录、开户、短信、消息发送、
群管、原生探测上传或 AI 操作。实现边界见 `docs/native-conversations.md`，
固定协议证据见官方研究 E32–E36。

| 检查 | 实际结果与边界 |
| --- | --- |
| 原生只读目录 | 固定 `get-group-list` 路由取得 6 群；账号级合并刷新、十分钟快照、旧数据采样时间/错误可见；没有成员全量扫描或创建 SaaS 授权 |
| 会话路由 | 修正本人发出消息的私聊同步方向；重新投影后列出 42 会话：6 群、36 私聊。没有把原生格式 5 的通知猜成对话；不代表服务器全部历史会话 |
| 应用协议 | 保留原 libsodium AEAD，补齐有界 Base64URL/protobuf/LZ4、签名 envelope、桌面 Timestamp、文本、引用和 @ 元数据；未知签名仍失败，不返回猜测正文 |
| 真实连接 | 最终只读观测为 connected，heartbeat 存在；本次连接 100 消息 / 100 次 ACK 写出，隔离 0、接收队列 0；ACK 写出不是服务端已处理证明 |
| 真实投影 | 该次观测保留 1761 条输入：文本解码 1265、未支持 441、失败 55，pending/processing 均 0；未支持为应用格式 322、原生格式 119；55 项为签名结构失败 |
| 实际延迟 | 同次观测解码 P95 2.868ms（最近 1024 样本，含读取/解封/解码、不含投影提交）；接收到落盘 P95 33.606ms、ACK 写出 P95 37.259ms。不是目标 Linux 机器、100/s 或端到端容量验收 |
| 并发边界 | 4 个 Go worker、16 个内存排队槽、独立最多 4 连接的解码 DB pool；按账号交错取任务，数据库持久化 owner/租约，重启恢复、升级重投影不抢活跃租约 |
| Go | 最终源码全量 `-race -tags sodium -count=1 ./...`、vet 通过；覆盖密钥/认证头错误、尺寸边界、签名/LZ4、Timestamp、私聊双向路由、通知惰性解析、取消与作用域 |
| PostgreSQL | 最终 httpapi sodium race 隔离集成 40.715 秒通过，随后 gateway 2.181 秒通过。串行只重建 `wsl_test`；覆盖公平领取、过期租约恢复、投影去重、受限角色、工作台隔离及清理级联 |
| 前端 | 最终 Docker 产品 TypeScript/Vite 构建、Prettier 通过；11 组浏览器回归全通过，新消息中心检查账号迟到响应、租户菜单拒绝、纯文本/XSS 与 320/647/1440 视口；真实原生写入 0 |
| 视觉检查 | 人工查看 `messages-thread-647.png`、`messages-health-320.png` 合成截图；未发现正文/指标重叠或横向越界。历史消息加载后不会被五秒轮询清空 |
| 工程 | 架构检查、迁移哈希及 `git diff --check` 通过；离线桌面工具 4 项通过、1 项可选原始材料测试跳过。新增 lz4 v4.1.22 锁定依赖及完整 BSD notice，整体发布许可证审计仍未完成 |
| 容器 | 最终镜像完成新临时库、迁移、受限角色、非 root/只读容器、资产与网站认证 smoke，通过，未使用生产数据。早次检查遇 Docker 端口发布未就绪；仅对空映射做有界等待，仍拒绝非本机或多映射 |
| 数据迁移 | 添加 015，未修改既有迁移、重置 `wsl`、删除旧隔离包或重写财务。新表只供平台角色，原始输入到期及账号删除级联清理解码投影；接口/worker 均执行原始七天期限 |

接收联调新增严格通知结构：`2/7` 多端在线、固定 `4/*` 离线/已读/目录通知、
`6/31`/`6/37` 连接检测通知。它们只保存为 notice；不跟随 URL、不执行指令、
不上传主机数据、不产生聊天 ACK 或独立推进恢复水位。未知传输边界仍阻断；
已解码的历史消息不能把阻断连接标为健康。

最终 API 镜像
`sha256:47a1d97552cd96cedf1aae89a468794accdd7b3453b9d8ad20ed9779a2bd6ab5`，
Web 镜像
`sha256:aed958f55cc984e26a00f1bf8d77c5d49051ddcfc07ed31042b83650a8b1d5d1`。
Compose API/Web/heatbot/PostgreSQL 健康，Simon 可用；
`native_ready=false`、`ai_ready=false`，原生业务动作仍关闭。

迁移前建立 0600 一致数据库备份
`var/backups/pre-conversations-20261026.dump`，SHA256
`9c8ddc7b200844961118f57a81dcf2a1c99cc44939eafa92e551a36fce5e4d4b`。
该备份含保密短期数据，不进入 Git/长期归档，必须在原始保留期限前清理；
系统已有新增写入，不得直接恢复此旧快照。完整备份到期自动化仍属发布门槛。

图片/语音/视频/文件/表情内容、未识别签名、全量私聊会话同步、
目标机吞吐与长稳/故障压测仍未验收，不能将上述局部观测称为原生整体打通。

再次只读复核连接仍为 connected：本连接 162 消息 / 162 次 ACK 写出、
隔离 0、接收队列 0；共 1823 输入，其中文本 1322、未支持 445、签名失败 56，
待解码及处理中均 0。最近解码 P95 2.178ms；目录仍为 6 群，42 会话
（6 群/36 私聊）。样本随实时收包增长，不能把历史失败隐藏为健康。

## 2026-10-27 旺商聊群管助手候选实现

本轮基于整合后的当前本地工作树实施，不回退桌面登录、功能中心、消息目录或
集中解码器。Git 基础提交为 `f6692ec09b9f9ed9e9416aee21b09522c716371f`，
候选源码逐文件哈希见 `docs/assistant-source-manifest.json`；这不是已提交的发布标签。
实现边界、接口和恢复步骤见 `docs/management-assistant.md`。

| 检查 | 实际结果与边界 |
| --- | --- |
| 分层与默认值 | 新增纯 Go 有界 Runtime、受信任 control 服务和 Element Plus 助手抽屉；未安装 Eino、增加微服务或改动 heatbot。总开关、租户开放、审批均默认关闭，独立月额默认0 |
| 权限与隔离 | 显式 `assistant:use/propose/approve` 不进入 manager 回退；隔离库验证跨租户、同租户不同用户、共享账号、伪造身份参数、WSL 工作台限制及 RLS；历史卡片也受撤销与授权指纹约束 |
| 费用与取消 | 合成模型验证预算原子预留、并发争额、零额度不调用、实际用量结算、取消、缺失 usage 保留预留及重启未知状态；未调用真实付费供应商 |
| 建议与审批 | 生成建议不改变配置或版本；摘要篡改、过期、字段撤权、原生角色失效被拒绝；重复审批只生成一次保存和执行记录。共用功能中心验证、CAS、epoch、队列失效和事务审计，返回 `applied=false` |
| 保留 | 七天正文/事件/计划密封保存；隔离库验证过期会话清理不级联删除90天最小用量元数据。供应商、长期备份及恢复清理政策仍须单独验收 |
| Go | 最终 `go test -race -tags sodium ./cmd/... ./internal/...`、`go vet -tags sodium ./cmd/... ./internal/...` 通过；覆盖实际生产包，部分未改包命中测试缓存 |
| PostgreSQL | 最终 `WSL_INTEGRATION=1 go test -race -tags sodium ./internal/httpapi -count=1` 44.667秒通过，结束后 `WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium ./internal/gateway -count=1` 1.774秒通过；仅串行重建 `wsl_test` |
| 模型流 | 本地合成 HTTP/TLS 夹具验证分块 UTF-8、SSE 结束标记缺失、超大帧、429不重试、取消及非法/重复工具；来源检查拒绝内网、保留地址和 URL 凭据，不发送思维链或上游错误正文 |
| 前端 | 最终 Web Docker 构建中的产品 `vue-tsc --noEmit -p tsconfig.wsl.json && vite build` 通过；助手、App 与 API 文件 Prettier 通过，不依赖框架演示生成的声明 |
| 浏览器 | 最终候选 Web 的 `assistant_ui_smoke.js` 通过：320/390/647/1440宽度、账号草稿隔离、纯文本 XSS、一次回合/一次审批及无权限入口隐藏；全部 API 被合成响应拦截，真实模型/原生写入均为0 |
| 视觉复核 | 查看 `assistant-320.png` 与 `assistant-1440.png`，正文、卡片和输入区无重叠、发送按钮在视口内。首轮布局断言落在入场动画中间态，等待动画稳定后复测通过；未将该中间态误报为布局修复 |
| 容器 | 最终 API/Web 候选镜像通过新临时数据库、完整迁移、受限角色、非 root/只读运行、资产及网站认证 smoke；助手初始关闭、无预置模型密钥、`native_ready=false` / `ai_ready=false` |
| 工程 | 架构边界、迁移锁定哈希、仓库检查及 `git diff --check` 通过；助手浏览器检查加入现有 CI 清单，本轮没有将“加入CI”冒称为完整12组浏览器回归已执行 |

新增016为前向迁移，SHA256
`63fca52d416436c455fb2e0e07c01524e2100e3965337833f40c38b05de93990`。
未修改014/015或其他既有迁移，未重置、迁移当前运行的 `wsl`。
测试容器使用新建且销毁的临时数据库，与既有数据及原生连接无关。

最终候选 API 镜像为
`sha256:194ef88e0578d94c3ca86ddc2d382c2cec9d2e58e11a2311f1b32f19e3690cca`，
Web 镜像为
`sha256:f9d4853930d40e97e17b0b50fec9c21c90d7b6d15975f391019b026493fe1f94`。
容器 smoke 指定候选标签，使用已核对无冲突的 `10.240.64.0/24` 临时网络；
默认 Docker 地址池不足的早次失败未通过清理其他项目网络来绕过。
早次 `go test ... ./...` 被既有忽略目录 `var` 内诊断源码的混合 package 阻断，
未删除这些非本轮文件；最终检查明确覆盖 `cmd/...` 与 `internal/...` 的生产代码。

当前5173服务未替换，既有 API/Web 仍分别为
`sha256:47a1d97552cd96cedf1aae89a468794accdd7b3453b9d8ad20ed9779a2bd6ab5`、
`sha256:aed958f55cc984e26a00f1bf8d77c5d49051ddcfc07ed31042b83650a8b1d5d1`。
未新建或重登原生账号、发送短信、执行群操作、配置真实模型、开放租户或自动提交推送。
5174临时合成界面检查容器不作为第二种生产部署方式，检查结束即清理。

Phase 0–3 的主要实现链路已有上述回归证据，不代表全部生产门槛完成：
目标 Linux 8核32GB机器上的接收/ACK对照、助手满载与慢消费者、数据库/磁盘/网络
故障注入、COMMIT邻近断连的真实故障演练、24小时长稳、供应商留存政策、监控告警联调、
两个隔离租户的灰度以及镜像回退演练仍未验收。SSE恢复与未知提交的实现/合成测试
不能代替这些实测。保持助手关闭，原生与AI readiness均不提升。

## 2026-10-27 轻量流式接收与界面更新候选实现

本轮只实施提交后解码唤醒、账号作用域增量事件及分层健康推送。
保留既有 Go 桌面协议契约，不引入完整旺商聊桌面程序、Electron、
Python 运行时、Eino、新服务或新迁移。消息连接持续接收，统计仍按窗口聚合；
不能把减少轮询称为已经测得吞吐或延迟提升。

| 检查 | 实际结果与边界 |
| --- | --- |
| 接收与解码 | 原始事件提交后非阻塞唤醒有界工作池；启动/5秒补查恢复漏通知及租约。合成隔离库验证通知发生在提交后、重复/回滚不唤醒，空闲后新输入在2秒测试期限内被处理；ACK仍先可靠提交 |
| 事件资源边界 | 每用户最多2条、全局16条、每订阅64项通知；通知溢出要求重取快照，不丢弃数据库消息。单元测试覆盖账号过滤、满队列、并发订阅/取消和通知合并 |
| 权限 | HTTP隔离库验证消息流仅超管、跨账号/工作台拒绝、撤销网站会话后无资源结果、连接名额释放；租户流复用RLS快照，只含自己的群指标，拒绝无订阅、过期套餐、账号读取权限撤销及停用群授权 |
| 增量与恢复 | 消息按最多64个精确ID补取，含较小ID的迟到完成，仍受会话/账号/七天范围约束；非法ID和混用分页参数被拒绝。初次、溢出及30秒对账恢复快照，事件序号仅属单连接，不宣称持久事件重放 |
| Go | 最终 `go test -race -tags sodium ./cmd/... ./internal/...` 与 `go vet -tags sodium ./cmd/... ./internal/...` 通过；未改包部分使用测试缓存 |
| PostgreSQL | 最终HTTP完整隔离套件42.632秒通过；结束后串行gateway完整隔离套件1.907秒通过，仅使用 `wsl_test`，未重置或迁移当前应用库 |
| 前端 | 最终候选Web的产品 `vue-tsc --noEmit -p tsconfig.wsl.json && vite build` 通过；改动Vue/TS文件Prettier通过。原5秒消息中心轮询及选中账号的整页15秒刷新移除 |
| 浏览器 | 最终Web镜像的合成 `live_updates_ui_smoke.js` 通过：重复/较小ID、6.2秒内无周期健康GET、切账号清空、撤权清空、500条DOM上限、历史翻页及返回最新、320/390/647/1440宽度；真实原生/模型写入0 |
| 视觉 | 已查看320/1440截图，正文按纯文本渲染，消息、状态与按钮无重叠或横向溢出；390/647通过自动溢出检查。有限SSE合成响应会立即重连，首轮等待常驻“实时订阅”的断言因此超时，改为验证数据和实时/重连状态后复测通过；不是产品连接修复 |
| 容器 | 新临时数据库完整迁移、受限角色、非root/只读API与Web、资产、认证、助手默认关闭及Nginx后账号SSE首帧全部通过；不包含真实原生账号或付费模型 |
| 回归范围 | 新浏览器检查加入现有13项CI清单；本轮实际执行新流式检查，不冒称13项浏览器检查全部重跑。Go完整生产包及完整HTTP/gateway隔离套件已重跑 |

消息通知按500ms合并；接收观察沿用1秒节奏，解码聚合在有变化时至多5秒一次，
账号业务聚合15秒一次，对话列表最多每3秒刷新一次。可见正文走授权增量查询，
不是每条消息重新读取整页。隐藏页暂停订阅；断线有界退避。
页面每个线程最多500条DOM，历史浏览不被实时插入打断。
事件数据不含正文/凭据，权限与数据库访问失败时拒绝继续交付。

本轮未修改既有迁移或在 `wsl` 执行016；助手功能仍关闭。
最终候选API为
`sha256:afdebc1a5e7af3b6d10437c553d3969b55f71b6a050b28bcd41cfbacdc6da425`，
Web为
`sha256:51d4253449eb18cd472d2e4637b1d52f808bc5188cdee5fb7841501a8b1921a2`。
API候选标签为 `wsl-live-api:test`，Web候选标签为 `wsl-live-web:test`；
基线及逐文件源码哈希继续使用 `docs/assistant-source-manifest.json`。
临时容器 smoke 使用 `10.240.64.0/24`，检查结束清理本轮容器/网络；
5174合成页面检查容器及浏览器也已关闭，不新增生产部署路径。

当前5173的API/Web只读核对仍为healthy，镜像分别是
`sha256:47a1d97552cd96cedf1aae89a468794accdd7b3453b9d8ad20ed9779a2bd6ab5`、
`sha256:aed958f55cc984e26a00f1bf8d77c5d49051ddcfc07ed31042b83650a8b1d5d1`。
本轮没有重启/重登或切换当前原生连接，没有发送短信、群消息、群管动作、
提交或推送Git。`native_ready=false`、`ai_ready=false`。

剩余门槛：目标Linux机器上的接收/ACK对照压测、多个真实浏览器慢消费者、
数据库/网络故障、长稳和受控Compose发布后的只读联调均尚未验收。
进程内通知和合成界面测试不构成端到端零丢失、100条/秒容量或延迟目标的证明。

## 架构文档工具链增量验证（2026-10-27）

本轮只增加架构文档、独立开发分析工具与 GitHub Actions，未修改现有业务链路。
完整范围与限制见 [`architecture/validation-2026-10-27.md`](architecture/validation-2026-10-27.md)。

- 四类代码证据视图生成并渲染：系统 7 节点/9 关系，后端 15/23，
  产品前端 19/40，Agent 7/5。每条关系有文件、行号和符号；外部依赖另存。
- 12 项工具测试、原架构守卫、仓库制品/迁移守卫、Actionlint、产品前端构建、
  Go 产品范围 race/sodium 测试和 vet 通过。Go 部分使用已有测试缓存。
- 指定供应商/模型的 JSON 预检及 CodeBoarding 首次分析成功，供应商分别报告
  18 和 14,887 Token；真实无变化增量成功，模型请求数及报告用量均为 0。
  这些数字不是独立账单，未验证模型的所有协议能力。
- 全目录 `go test ... ./...` 被已有忽略目录 `var/` 中混合包名阻断；
  未动这些诊断文件，改以明确产品源码范围重跑，不记为全目录通过。
- 密钥仓库外 `0600` 保存，Git 候选扫描未发现该密钥；未配置业务模型、
  修改 readiness、访问数据库、重启服务或操作原生账号。
- 未提交/推送或启用远端 CI Secret；未重建业务镜像，也未重跑数据库、
  容器 smoke、原生验收或容量测试，不能替代发布完整门槛。

## 2026-10-27 本地资源缩容与定向镜像清理

本轮应用户要求处理本地资源，不修改生产 `compose.yaml`、业务代码、
数据库结构、账号登录或群操作；不提交或推送。主机为24 GiB/10核，
Docker Engine可见约7.65 GiB/10核，不能照搬8核32GB生产预算。

| 检查 | 实际结果与边界 |
| --- | --- |
| 本地覆盖 | 新增显式 `compose.local.yaml`，仅覆盖七个既有服务的CPU、内存和swap；不更换镜像、网络、数据卷、凭据、权限或验收开关。五个常驻服务合计上限1984 MiB/2.85 CPU，不是预留内存或生产容量 |
| 在线应用 | 通过 `docker update` 调整现有五个常驻容器；调整前后ID、启动时间、重启次数、挂载及账号/群绑定计数一致，未重建容器或重新登录。API 1 CPU/512 MiB，PostgreSQL 1 CPU/1 GiB，heatbot 0.5 CPU/256 MiB，Web 0.25 CPU/128 MiB，Simon 0.1 CPU/64 MiB |
| 当前健康 | API、PostgreSQL、heatbot和Web维持healthy；Simon没有Docker健康检查，确认仍running，不伪称healthy。`/healthz`正常，`native_ready=false`、`ai_ready=false`；单次空闲采样合计约250 MiB，不代表压力测试结果 |
| 历史状态 | PostgreSQL在本轮调整前已有 `OOMKilled=true` 历史标记；调整后未变化，启动时间和重启次数均未变化。未将该历史标记描述为本轮新增OOM或已经修复其根因 |
| 镜像清理 | 删除5个未引用的本项目临时标签镜像及74个明确属于WSL的无标签旧镜像，共79个；删除前核对所有容器的镜像引用，不使用强制删除，保留当前API/Web、测试工具镜像及其他项目镜像 |
| 构建缓存 | 按 `description~="cmd/wsl"` 且 `until=12h` 定向清理旧Go构建缓存，`docker buildx prune` 报告回收4.545 GB。Docker镜像与构建缓存可能共享层，不把各统计项简单相加，也不声称macOS稀疏磁盘文件已立即缩小同等大小 |
| 保留 | 容器总数176、运行数86、数据卷总数104均未因清理变化；未执行system/volume/container prune、停止其他项目或删除数据库卷。其他项目仍有81个运行中容器，其中78个为 `tenant-acceptance-pg-*`，需要归属项目另行确认清理 |
| 配置回归 | 本地5项资源契约测试、生产及本地Compose语法/合并校验、原架构守卫、四视图架构check、仓库/迁移守卫和 `git diff --check` 通过；5项测试也在现有测试镜像中以断网、只读、0.5 CPU/128 MiB执行通过。四视图无变化，不重画架构图 |

新增资源契约测试纳入现有foundation流程；测试Dockerfile同步纳入覆盖文件。
本轮未重建业务或测试镜像、未执行完整Go/前端/数据库/容器发布套件，
未触发远端CI或付费CodeBoarding分析；以上检查不是生产发布验收。
本轮未变更Go/Vue依赖、服务拓扑或所有权边界。

此前报告中 `wsl-live-*`、`wsl-assistant-*` 临时候选镜像已按本轮清理，
历史源码/镜像哈希记录保留，但不能再视为本机可直接发布的镜像。
需要发布时必须按对应审阅源码重新构建和验收。当前5173仍使用原先运行镜像。
后续本地 `up` 使用 `-f compose.yaml -f compose.local.yaml`；
生产及目标机压测只使用基础 `compose.yaml`，在维护窗口恢复正式资源限制。
在线 `docker update` 不更新Compose配置哈希；在同一已调整栈恢复时，
普通 `up` 不保证覆盖手动限额，需维护窗口显式重建服务并复核，
具体保留数据卷的命令见 `docs/docker-operations.md`，本轮未执行恢复重建。

## 多账号调度与运行监控候选实现（2026-10-27）

本轮在当前业务工作树增量实现，保留此前未提交改动；没有回退到旧 CI
工作树，没有提交或推送 Git，也没有部署到当前 5173。沿用单活动协调者、
既有 Compose 服务及本地缩容上限，没有新增常驻服务或复制每账号数据库。

| 检查 | 实际结果与边界 |
| --- | --- |
| Go 产品范围 | `go test -race -tags sodium ./cmd/... ./internal/...`、同范围 `go vet -tags sodium` 通过；包含调度并发/账号串行/16等待任务、换票优先、刷新合并、取消、公平性、窗口过期与计数器重置 |
| HTTP 隔离集成 | 最终完整 `WSL_INTEGRATION=1 go test -race -tags sodium ./internal/httpapi -count=1` 通过（56.715秒）；仅重建 `wsl_test`，没有重置 `wsl` |
| Gateway 隔离集成 | 在 HTTP 套件之后串行运行 `WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium ./internal/gateway -count=1` 通过（2.582秒）；覆盖原收包/提交后 ACK、解码与持久冷却换调度器恢复 |
| 容量与迁移 | 第30/31群并发准入、功能关闭仍占名额、撤销释放、低绑定数推荐、未知操作阻断、旧预览冲突、同键同结果/换载荷拒绝、迁移取消旧排队、同群一主一shadow验证通过；均为合成角色/租约，不连接原生 |
| 监控与隔离 | 单条 Simon 连接连续收帧、读快照不等待网络、取消/压缩界限、缺失 cgroup/计数重置、独立故障原因、分钟清理/事件去重恢复、租户数据库拒绝及跨工作台账号历史拒绝通过 |
| 账号指标 | 详情复用共享解码采样，补持久冷却、30群容量及业务 HTTP；提交与 ACK 各自报告样本数、60秒窗口、8192上限。原 INSERT 延迟保留并明确早于提交，不混作端到端耗时 |
| 产品构建 | 最终宿主及标准 Docker Web 构建通过，使用 `web/tsconfig.wsl.json`；没有引入生产 Python、监控服务或新的产品依赖 |
| 合成浏览器 | `runtime_monitor_ui_smoke.js` 与 `account_dashboard_ui_smoke.js` 通过320/390/647/1440宽度；监控四视图、实际趋势颜色像素、无样本不补零、同键未知结果核对、迟到响应隔离、失败旧数据提示、租户不显示共享遥测、切面板零写入均通过 |
| 视觉复核 | 查看390资源趋势与1440总览截图；表格局部滚动，页面无横向溢出。历史造出缺失分钟，不连线补零；单点保留可见标记。截图使用合成名称及指标，不是在线账号证据 |
| 候选容器 | API/Web标准镜像及最终独立冒烟通过：新库完整迁移、受限角色、非root/只读、产品资产、网站认证、助手默认关闭、Nginx账号与监控SSE首帧、历史与事件接口；已有运行容器、数据卷、账号会话及资源限额未切换 |
| 架构 | 原所有权/Docker守卫、仓库制品与迁移守卫、四视图检查、17项工具测试及四图渲染通过；结构关系变化只更新受影响前端图，后续样式/行号变化只更新证据或清单 |

首次完整 HTTP 回归有两处旧测试夹具失败：未知绑定原期望进入业务待解析，
以及缺少原生角色证据时原期望自动分配。现明确验证前者进入 `shadow`、
后者先补合成可信角色/接收证据再分配；没有恢复无证据放行。
浏览器早期失败来自夹具权限、页面重载与 Element Plus 隐藏单选框的点击方式，
已修正测试后复跑；没有绕过业务授权或去掉溢出/像素断言。
一次 Web 构建使用了不存在的 Dockerfile 路径，纠正为现有
`deploy/web.Dockerfile` 后通过，没有新建第二套部署文件。

迁移新增017，001–016未修改；017只应用到临时/测试数据库，
未在业务库执行。017最终 SHA256：
`3fd0d983bc6799c966263c29ea0fc6efb4204b018644733eb68d5aefd191022f`。
新增平台专用表启用强制 RLS，监控分钟24小时、事件7天、分配回执7天。
历史只存数值及安全状态，不保存聊天正文、凭据、请求载荷或机器人名称；
账号删除时监控账号行通过FK级联或既有删除事务清理。

最终候选镜像（不是当前5173部署）：
- `wsl-api:runtime-candidate-20261027`：
  `sha256:b3645f3818b9737a9755d11b737eeb952cf680ffcac62e7f09e8dbf2d5928670`
- `wsl-web:runtime-candidate-20261027`：
  `sha256:008ab06c71bd35b1925b8ed3448fcbe85dafd66da69cbb36c65575b0e4844b7e`

### 本地短回放，不构成容量验收

两组在同一最终候选源码、相同 API 初始化下串行执行，每组10账号、
100个合成群标识、100条/秒20秒后500条/秒5秒，共4500条。
数据库为 `wsl_test`，只回放持久化收件箱，不启动原生、Simon、目录 HTTP、
解码 worker 或业务动作。监控开启组运行共享资源/数据库采集和分钟持久化。
本轮重建镜像与浏览器检查结束后才执行该组对照，但没有隔离其他项目负载。

| 观测 | 监控关闭 | 监控开启 |
| --- | --- | --- |
| 提供 / 可靠入库 / 本地 ACK 回调 | 4500 / 4500 / 4500 | 4500 / 4500 / 4500 |
| 收件→持久化→本地 ACK 回调 P95 / P99 | 15.232 / 46.520 ms | 14.581 / 27.800 ms |
| 平台池空池获取次数增量 | 9 | 9 |
| 平台池获取总耗时增量 | 204.667 ms | 106.658 ms |
| Go测试进程堆起点 → 终点 | 64.64 → 82.35 MiB | 64.64 → 82.70 MiB |

时间起点是合成事件的 `ReceivedAt`，终点是持久化后的本地 ACK 回调，
不含真实 socket ACK 写出或远端确认。平台池总耗时含正常获取，不等于纯等待；
没有拿它代替专用监控池或整个数据库的资源消耗。Go堆包含网站API初始化和
测试代码，不是容器RSS，也不是项目总内存；宿主测试没有 cgroup，
CPU/容器内存返回 null，没有补造测量值。
原始数值在忽略目录 `var/replay-report-monitor-off.json`、
`var/replay-report-monitor-on.json`，均标记 `capacity_accepted:false`。

早期与构建并行的回放不作对照；另发现仅监控开启组创建API导致混入密码哈希
初始化，已改为两组共同初始化后重测，旧结果不用于性能结论。
最终单次短样本有顺序、缓存和宿主噪声影响，不据此宣称监控提升延迟、
没有任何资源影响、调度改造性能已验收或已经达到生产持续吞吐。

CodeBoarding 增量命令已尝试，但在调用模型之前拒绝：
`Baseline toolchain differs; review and archive it before explicit initialization`。
本轮未静默回退全量、修改缓存指纹或覆盖可信基线；**增量分析门槛未通过**。
需先人工核对工具链来源并批准兼容基线处理，再重新执行增量检查。
确定性代码证据及 Mermaid 检查通过不能代替这一结果。

仍未完成：目标 Linux 的同基线旧/新调度对照、100条/秒60分钟、
500条/秒10秒及24小时长稳；慢模型/慢 SSE/数据库与磁盘故障下的满载影响；
明确授权隔离真实账号的多账号原生联调。合成隔离测试不表示这些门槛通过。
全部 readiness 继续保持 false，助手及原生动作未因本轮实现而开放。
使用说明及测量口径见 [`account-runtime-monitoring.md`](account-runtime-monitoring.md)。

## 原生成员底座与缺口核对（2026-10-27）

沿当前未提交业务工作树继续实现，只读成员原语不等于原生写操作已打通。
没有修改旧迁移、业务库、真实账号、登录/接收开关或原生 readiness。
功能与逐项缺口见 [`native-foundation.md`](native-foundation.md)。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go / sodium / race | `go test -race -tags sodium ./cmd/... ./internal/...` 最终通过；同范围 `go vet -tags sodium` 通过 |
| 原生合成契约 | 固定桌面签名及加密请求、分页游标、跨页去重、ID/角色冲突、缺失传输身份、分页/投影上限、取消、429和网络错误不重试通过；未支持的原生写路由继续拒绝 |
| 合并结果 | 两个等待同一资源的请求取得同一不可变结果，不再依赖第一个调用者闭包中的局部结果；成员服务分别复制输出切片 |
| HTTP / PostgreSQL | 完整 `WSL_INTEGRATION=1 go test -race -tags sodium ./internal/httpapi -count=1` 通过，45.447秒；仅重建 `wsl_test`。新增成员路由超管/工作台/输入校验，以及交付前账号 epoch、用户禁用和网站会话撤销拒绝 |
| Gateway / PostgreSQL | 随后串行运行完整 `WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium ./internal/gateway -count=1` 最终通过，17.477秒；合成目录限制、当前账号传输身份、自身角色、429持久冷却/换调度器恢复和迟到 epoch 栅栏通过，确认只读查询没有写入 `account_groups` |
| 产品前端 | 宿主及标准 Docker 构建通过，产品 tsconfig；JS `index-BpTZZW7J.js`，CSS `index-DOoS3jQR.css`，没有新增运行依赖 |
| 浏览器 | `native_members_ui_smoke.js` 在320/390/647/1440宽度通过；50人分页、搜索、文本转义、失败清空、关闭/账号切换迟到响应隔离、零原生动作；已加入既有 CI 浏览器列表，本轮未重跑全部15个浏览器脚本 |
| 视觉复核 | 查看390宽度成员弹窗截图，页脚可见、表格局部滚动、无页面横向溢出。截图仅使用合成群名、昵称及名单 |
| 候选容器 | 最终 API/Web 镜像的独立 `tools/container_smoke.py` 通过，新建临时数据库/角色、迁移、非root只读、认证、产品资源、助手默认关闭及原有账号/监控 SSE 代理通过；临时容器自动清理，没有切换当前服务 |
| 架构 | 代码证据 update/check、所有权/Docker守卫、仓库/迁移守卫及17项工具测试通过；仅前端依赖图有结构变化，其他视图未重写，四图 SVG 渲染通过 |

浏览器首次断言采到了 Element Plus 开场动画的缩放中间态，等待有限动画结束后
保留同样的弹窗边界断言通过；账号切换用例另外修正为先进入新账号的群目录标签。
没有移除移动端尺寸断言。最初离线换票夹具误把安全码确认路由的 JWT 轮换套在普通
成员读取上；修正夹具验证普通读取不会因非契约响应头改变 JWT，没有放宽认证协议。
Mermaid 首次 Chrome/CDP 启动超时，独立重试四图通过；Web镜像首次被 Docker Hub
匿名令牌网络连接重置阻断，沿原 Dockerfile 重试成功，没有换用其他部署路径。

最终候选镜像：
- `wsl-api:native-foundation-candidate-20261027`：
  `sha256:39b93768918b6cd36b40b844d1ec280e0ccbb7c5d195a74e001321022fed2d61`
- `wsl-web:native-foundation-candidate-20261027`：
  `sha256:7790944d779069072ac1f7685c3a1cea18fd16dfa015b5e29d31bad69f5e5686`

本轮静态前端预览已关闭，自己产生的无标签中间 API 镜像已删除；没有清理无关镜像、
容器或数据卷。没有提交/推送，也没有将候选镜像部署至5173。

CodeBoarding 增量再次在模型调用前因
`Baseline toolchain differs; review and archive it before explicit initialization`
拒绝；没有静默全量分析或覆盖可信缓存，仍是未满足的发布门槛。
真实成员分页完整性、角色枚举及服务端限制尚未联调，完整写操作、业务消费与生产容量
均未因此通过；`native_ready=false`、`ai_ready=false`。

## 文本出站与提交栅栏底座（2026-10-27）

本批只在当前业务工作树补内部原语和合成测试，没有切换5173服务、操作真实账号、
修改既有迁移或重建 `wsl`。018仅应用于 `wsl_test` 及容器smoke的临时数据库。
真实文本发送尚未打通，详细边界见 [`native-foundation.md`](native-foundation.md)。

| 检查 | 实际结果 |
| --- | --- |
| Go / sodium / race | 最终 `go test -race -tags sodium ./cmd/... ./internal/...` 通过；同范围 `go vet -tags sodium` 通过 |
| 出站合成契约 | 桌面Timestamp/protobuf固定向量、LZ4/原始nonce AEAD解密往返、正文/总包/身份限制、规范整数回执、序号与客户端ID不匹配拒绝通过；没有真实上游发送 |
| 传输与收包隔离 | 本机假WebSocket验证服务器接受、明确拒绝、断线、取消、残缺回执、错误序号及不重发；控制检查等待期间心跳仍完成；合法晚到8/2回执是notice，不产生聊天ACK或推进水位 |
| HTTP / PostgreSQL | 最终完整 `WSL_INTEGRATION=1 go test -race -tags sodium ./internal/httpapi -count=1` 通过，53.435秒，仅重建 `wsl_test` |
| 控制与持久证据 | 合成服务验证25个控制子用例及生产入口关闭回归：幂等、密封正文、预算、最终epoch/授权/管理/测试/角色/套餐/会话/租约撤销，临近到期余量、并发一次提交、未知不重发、未知不能换key绕过、过期/中断不重排、清理后nonce继续递增 |
| Gateway / PostgreSQL | HTTP之后串行完整 `WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium ./internal/gateway -count=1` 通过，18.176秒；包含原可靠落盘/ACK、读取/冷却及成员只读回归 |
| 产品前端 | `pnpm run build` 通过，JS/CSS仍为 `index-BpTZZW7J.js` / `index-DOoS3jQR.css`；未改前端源码、未新建按钮，本批未重复浏览器视口验收 |
| 候选容器 | 最终API镜像配合既有同资源哈希Web镜像，独立 `tools/container_smoke.py` 通过：新建数据库/角色/018迁移/bootstrap、非root只读、网站登录退出、资源、助手关闭、原账号/监控SSE及历史接口；没有生产数据或原生部署凭据 |
| 架构 | 证据update/check、所有权/Docker守卫、仓库/迁移守卫、17项架构工具测试及四图SVG渲染通过；只有后端依赖图结构改变，其他Mermaid未重画 |

新增 `internal/control/native_text.go` 没有注册到HTTP或后台worker；
`internal/gateway/send_text.go` 没有真实账号调用者。控制测试的native角色和租约
均来自隔离数据库合成夹具，不是原生身份或可发言权限的真实验收。
生产 `POST /api/v1/operations` 仍返回 `native_compatibility_not_verified`，
功能目录的发文本只是“部分接入”，所有业务动作验收数仍为零。
`succeeded/im_server_accepted` 在本批只表示合成服务端接受，不表示成员送达。

018 `native_message_nonce` 的高水位不随账号/意图清理重置。其迁移哈希为
`e72201ea2ff0bfd63faca82add3c73999379408ff1b9c5dd08d21477e56eea93`。
跨旧系统及备份恢复的nonce历史仍未核验，不声称已解决跨系统密钥/nonce复用。
未知结果仍需后续可信查询/原生证据对账，本批没有自行升级为成功的接口或任务。

测试夹具修正记录：首次尾部迁移查询误用了受限平台角色读取schema_migrations，
移除该多余查询，没有放宽数据库权限；生产入口关闭单测最初漏填Content-Type，
补齐JSON请求头后沿原拒绝条件通过，没有放宽API。

最终候选：
- API `wsl-api:native-text-candidate-20261027`：
  `sha256:533c2b9cff96701bb0ffe9f465dab15327812df7fd0b422b1cc98886eede38c1`
- Web复用 `wsl-web:native-foundation-candidate-20261027`：
  `sha256:7790944d779069072ac1f7685c3a1cea18fd16dfa015b5e29d31bad69f5e5686`

CodeBoarding增量已再次尝试，在调用模型前因
`Baseline toolchain differs; review and archive it before explicit initialization`
拒绝；未全量回退、修改基线或发送密钥/运行数据。此发布门槛仍未通过。
没有新增生产依赖/部署服务、扩大资源上限、提交或推送；未部署当前候选。
`native_ready=false`、`ai_ready=false`保持不变；本地测试不代表生产吞吐或长稳通过。

## 尚未验收

2026-10-26 21:42（Asia/Shanghai）应用户“业务终端过旧”反馈，另行实时核对官网
发布接口和 Windows/macOS 自动更新清单，三者仍为桌面 `2.8.3`；
不是把 Android `3.7.4` 用作桌面新版。来源与具体时间见官方研究 E26–E28。
没有核实到更高桌面版本，故未伪造升级、改变协议版本或重复原生登录。

- 用户已手动完成真实桌面登录，单账号 IM 心跳、同步、落盘/ACK 和部分应用消息 protobuf/LZ4 已有上述证据；过期续期、完整验证挑战、全部消息格式、断线恢复及受控换线仍未完整验收。
- 原生唯一连接所有者、同群多账号唯一采集者、公平业务消费、毒包/缺口与重放边界。
- 最终动作提交前的完整权限栅栏、未知结果复核和真实多账号测试。
- 旧规则、排行、邀请奖励、返现状态机及历史数据迁移。
- MaiBot 全部身份/人物/学习/图谱/向量/媒体/插件/缓存隔离、真实模型层并发和全部管理页。
- 目标机 60 分钟/24 小时负载、100 AI 群压力、完整故障注入和资源预算验证。
- 附件、日志、备份、供应商及所有派生数据的完整保留/删除政策。
- 完整依赖许可证/漏洞审查、TLS 生产部署、监控告警和生产恢复演练。

以上未通过前原生业务动作与群 AI 保持不可启用；消息接收联调由独立开关控制，
不代表原生整体 readiness 通过。

## 2026-10-27 架构增量维护加固

范围仅为架构工具、文档、AGENTS 和架构 CI；详见
[`架构增量验证`](architecture/validation-2026-10-27-incremental.md)。
33 项 Python 工具测试、3 项 Node 缓存测试、架构/仓库守卫、
Actionlint、实际 SVG 渲染与局部重绘检查通过。Go 提取器的 race 编译与 vet 通过；
它没有 Go 单元测试，AST 行为由 Python 合成夹具覆盖。

工具同步到业务工作树前后，602 个业务/部署及人工契约文件哈希一致。
两工作树各自提取证据，不复制旧业务图或覆盖人工作出的设计决策。
当前 CodeBoarding 增量在预检以 `compatible_baseline_missing` 阻断，
模型请求和 Token 均为 0；旧缓存保留，没有全量回退或伪称增量成功。

本轮未提交、推送、触发远端 CI、重建产品镜像或操作数据库/真实账号。
完整产品 Go/前端、隔离数据库及容器发布检查未重跑；
本轮架构工具测试不替代正式发布验收，也不提升 readiness。

## 用户中心、辣椒聊底座与跨平台账号池（2026-10-27）

实施对象是当前产品工作树，保留此前未提交的旺商聊、功能中心、助手和监控实现。
本批不是五阶段计划的全部交付；能力及剩余项见
[`multi-platform.md`](multi-platform.md)。没有提交、推送或触发远端 CI，
没有对生产 `wsl` 执行迁移、重置、原生登录或群操作，也没有把候选镜像切换到5173。

| 检查 | 本批实际结果 |
| --- | --- |
| Go / sodium / race | 最终 `go test -race -tags sodium ./cmd/... ./internal/...` 通过；同范围 `go vet -tags sodium` 通过。使用项目命令和内部包范围，不扫描用户忽略目录中的临时 Go 文件 |
| HTTP / PostgreSQL | 最终完整 `WSL_INTEGRATION=1 go test -race -tags sodium ./internal/httpapi -count=1` 通过，62.569秒，仅重建 `wsl_test` |
| Gateway / PostgreSQL | HTTP之后串行完整 `WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium ./internal/gateway -count=1` 通过，19.160秒；包含已有旺商聊接收、目录、读取调度及冷却回归 |
| 平台、账号池与容量 | 隔离数据库验证两平台共享每用途10账号，第11账号拒绝；同平台身份不能跨池重复，跨平台同名ID分开；第30/31群及300/301群、关闭仍占位、撤销释放和跨平台成员关系拒绝通过。它们不是生产容量测量 |
| 用户与套餐 | 辣椒聊单平台套餐、默认用户中心、受限平台入口、租户无原生认证权限、发布人设仅返回名称、配置CAS及保存不启用引擎通过 |
| 辣椒聊手动认证 | 固定桌面HTTP合成夹具验证验证挑战、设备不接管、密封会话、稳定设备、并发重复登录拒绝、epoch/版本变化拒绝最终保存；原生登录后换票失败一律保留未知状态，未自动重试。人工复核重置保留设备及恢复游标，进行中的新尝试不可清除 |
| 辣椒聊合成传输 | 20条本机假WebSocket独立连接、响应/推送分流、关闭一条不影响其他连接、序号缺口/身份错误停止、固定文本回执字段、发送栅栏及未知不重复通过；未连接真实上游 |
| 可靠恢复与解码 | 游标与密封原始事件同事务提交、缺口和旧epoch拒绝、集中解码平台作用域通过；300会话轮转测试通过。初次接入跳过旧消息，恢复不能跳水位。LJL显示 `sequence_commit_only`，没有声称写出网络ACK |
| 热群基础契约 | 平台/租户/群队列键、配置快照、300/301状态、全局4并发、每群串行、候选替换及取消通过；没有将原生消息接入热群运行时，也没有运行真实模型 |
| 产品前端 | 主机构建及Docker内 `pnpm build` 通过，资源为 `index-DQApZObo.css` / `index-DZtcQ-4r.js`；产品类型检查未依赖框架Demo生成文件 |
| 浏览器 | 新的 `tools/run_multi_platform_ui_smoke.mjs` 在320、390、647、1440宽度通过：四面板图标名称弹窗、普通用户默认面板及套餐隔离、热群新账号平台选择、模拟辣椒聊手动登录、配置弹窗页脚可见；真实原生动作数为0，所有非夹具网络被阻断 |
| CI浏览器夹具 | 原有面板切换/工作台API夹具已适配，新用例已加入现有CI运行器；17份浏览器脚本语法解析通过，但未在本轮逐一执行全部CI浏览器用例 |
| Docker候选 | API与Web最终串行构建；临时数据库容器smoke通过020迁移/bootstrap、受限角色、非root只读、网站认证、资源、四面板、两平台/用途账号创建、辣椒聊凭据及游标租户隔离、原SSE代理及默认关闭门槛。没有载入生产数据或执行原生登录 |
| 架构 | 代码证据update/check、所有权/Docker守卫、仓库/迁移守卫、34项Python工具测试、3项Node渲染缓存测试通过。实际更新后端依赖、接收路径和前端图，未变化的系统及Agent图未重画；最终渲染缓存逐图核对通过 |
| CodeBoarding | 已执行增量入口，在预检以 `compatible_baseline_missing` 阻断；模型请求、预留及上报Token均为0。旧基线保留，没有全量回退、读取模型密钥或将阻断冒充分析成功 |

### 环境故障及重试记录

首次尾部HTTP数据库复测失败时，本地PostgreSQL日志记录
2026-10-27 15:42:16 UTC 的checkpointer被signal 9终止，15:42:21 UTC完成自动恢复；
Docker检查同时显示该容器曾被OOM终止。事件发生在并行候选构建期间，本批没有
单独证明主机级完整因果链。API随数据库故障发生自动恢复，不是本批手工发布或重启。
此后不再并行构建，不扩大资源配额；构建结束后串行完整复测HTTP及Gateway，
结果如表。没有清理其他项目的容器、网络或数据卷。

容器smoke首次在启动容器前因Docker默认地址池耗尽失败。只读检查169个网络，
确认 `10.249.27.0/28` 无重叠后，通过已有 `WSL_SMOKE_SUBNET` 参数重试通过。
该临时网络和容器由smoke清理，没有更改全局Docker地址池或删除其他项目网络。

### 迁移与镜像记录

020是前向迁移，未修改既有迁移；校验值为
`3eac780dcd10b7521545f76b8118a8a0106b293dd8b7e03c6b72e755e354177b`。
它只在 `wsl_test` 及smoke临时数据库执行，生产数据库未由本批升级。
旧套餐保持原额度且默认只允许WSL；旧账号密封数据和身份不会因新增面板自动重登。

本批最终经过smoke的镜像：
- API：`sha256:6bae7e2c54e34622e922319b7fb6fa8a84167b194538e679a12fcf6c0efde649`
- Web：`sha256:99dc46f9d3e293460446aa734e1edec113fc2bceddbcc106fcc752f678d15f20`

上述候选及8个明确属于本批、无容器引用的中间镜像已删除，避免额外保留版本；
没有全局prune。项目现用 `local` 与 `rollback-20261027-predeploy` 镜像保持原样：
- API现用 `dd318008abd8`，前版 `47a1d97552cd`
- Web现用 `b619c4320d4c`，前版 `aed958f55cc9`

### 尚未交付与发布阻断

辣椒聊文本发送、回执及禁言/解禁只有协议原语和合成夹具，尚未注册为生产控制动作；
统一持久提交、最新可发言证据、未知结果对账、原生事件到规则执行、定时发布和
角色核验后的授权码发放/群认领闭环仍需补齐。不能因目录可见便批准群归属。
引用、@、撤回及媒体仍按能力目录显示未接入/待验收，不丢弃组件后返回成功。

跨平台热群已具备作用域及公平队列基座，但原生输入路由、调用前原子Token预留、
调用后结算、最终control发送、记忆/学习隔离和MaiBot行为对比仍未完成。
用户中心Token用量显示“未采集”，不是零；热群启停申请和配置保存不代表启用。
辣椒聊完整真实挑战、设备共存、分页/角色枚举及重放窗口都未获本批原生验收。
20条合成连接及300个队列状态测试不等于目标机吞吐、资源或长稳通过。

发布仍缺兼容可信CodeBoarding基线、全部CI浏览器回归、完整依赖许可证/漏洞审查、
生产迁移演练、明确授权的隔离群实测及目标机20连接/300群资源验收。
本批仅为新增OpenIM协议依赖保留固定版本及Apache-2.0声明，不宣称完成全部许可证审查。
`native_ready=false`、`ai_ready=false`保持不变。

## 消息中心原生式布局与紧凑解码监控（2026-10-28）

本批仅调整消息中心及其页面容器、浏览器夹具，不改变原生协议、授权、账号池、
数据库迁移、消息保留时间或接收/ACK链路。没有发布镜像或切换5173服务。

- 桌面采用左侧搜索/筛选/会话列表、右侧消息气泡与独立滚动；消息页移除重复的大标题
  和网站页脚，聊天区域使用剩余视口高度。群聊、私聊、目录及成员入口保留。
- 320/390/647宽度使用列表与会话单页切换，提供返回按钮，不再把聊天记录堆在列表下方。
- 解码健康页签和常驻指标条移除，改为右上角状态按钮及按需弹层。异常状态不会伪装健康；
  阻断原因、采样时间、积压、工作池和诊断计数仍可查看。辣椒聊仅提交序号的状态不展示成网络ACK。
- 消息仍以纯文本显示，引用、@和截断提示保留；只读页脚不伪装可发送的输入框。
  返回最新、历史窗口及账户作用域沿用原链路，新增本地滚动跟随不增加HTTP轮询。

| 验证 | 实际结果 |
| --- | --- |
| 产品类型检查和构建 | `npm run build` 通过；CSS `index-DYxGVLoX.css`，JS `index-OSIJd0c3.js` |
| 消息中心合成浏览器检查 | 320、390、647、1440宽度通过；无页面横/纵向溢出，桌面双栏、移动列表/会话返回、解码弹层尺寸、群目录刷新、私聊切换及协议阻断解释通过 |
| 安全和账户隔离 | 消息中的HTML保持文本，不产生图片节点；迟到的A账号响应不能覆盖B；租户菜单不暴露共享私聊 |
| 实时订阅回归 | 增量去重、迟到的较小消息ID、无固定健康HTTP轮询、账号切换、权限撤销、500条DOM上限、历史返回最新通过；该检查写请求为0 |
| 多面板回归 | 四面板、用户套餐隔离、聊天账号平台选择及模拟辣椒聊认证通过；没有真实原生动作 |
| 架构维护 | update/check、所有权/Docker守卫、仓库/迁移守卫、34项Python工具测试及3项Node渲染缓存测试通过；刷新代码证据，六份Mermaid拓扑未变，渲染器确认原SVG仍匹配，没有重画 |
| CodeBoarding | 增量入口预检返回 `compatible_baseline_missing`；请求/Token为0，没有读取模型密钥、全量回退或修改旧缓存 |

截图均为合成数据，位于忽略目录 `output/playwright/messages-{thread,list,health}-*.png`，
未读取或拍摄真实群聊。测试运行器仍默认执行原多面板检查，仅增加白名单参数用于
消息中心和实时订阅回归，没有新增项目依赖、常驻服务或宿主机部署路径。

截图复核发现刷新图标的组件悬浮提示会残留；增加提示关闭断言后曾超时。
改用按钮原生 `title` 保留说明、避免额外悬浮层后重新构建和复测。
截图等待目录入队提示真正出现并关闭，且关闭过渡动画，不把半透明过渡帧当作最终界面。

本批没有后端改动，未重新执行Go/数据库/容器发布全套检查，浏览器检查也不等于
原生发送或生产验收。没有新增迁移、扩大资源、自动登录、发送、禁言、提交或推送。
两个readiness门槛保持关闭；本批源码及构建结果不能描述为5173运行镜像已更新。

## 手机端单行顶部栏与微信式对话（2026-10-28）

本批继续调整产品前端及合成浏览器检查，不修改后端、原生能力、权限口径或数据库。
手机端采用52px单行顶部栏：保留导航、无边框账号选择和有权限的助手入口；
注册/登录、机器人连接状态、刷新及网站退出收进“更多操作”。普通用户菜单不包含
原生登录或账号连接入口。账号连接详情复用同一抽屉，最大宽度不超过视口，
打开登录或删除面板时关闭连接抽屉，避免叠加多个操作面板。

消息页在760px及以下移除外围边框、圆角和16px留白，铺满剩余屏幕。会话内不再
重复显示页签栏，48px标题行保留返回、名称及右上角解码/刷新；返回列表后仍能进入
原生群目录。群ID和映射说明保留在标题可访问描述中，桌面继续显示完整元数据。
列表采用白底、头像和细分隔线，去掉每行类型/条数的重复标签；聊天采用浅灰背景、
无边框白色气泡、方形头像和紧凑只读说明。引用、@、未知组件、原文转义和七天保留
不变，没有伪造自身发送方向或加入未接入的发送框。

| 验证 | 本批实际结果 |
| --- | --- |
| 产品类型检查与构建 | `npm run build` 通过；CSS `index-C2k8yL-9.css`，JS `index-CFKZsCS7.js` |
| 手机端消息与全局顶部栏 | 320、390、647宽度通过；有助手按钮时仍为单行，账号选择可用，聊天根节点无边框/圆角/外侧留白，无整页横纵溢出，标题行不超过52px |
| 菜单和连接详情 | 三种手机宽度均验证登录、连接状态、刷新、退出入口及抽屉边界；320宽度实际触发合成页面刷新；普通用户菜单不出现原生控制项 |
| 消息中心与桌面回归 | 320、390、647、1440宽度通过列表/会话返回、原生群目录、私聊、健康详情及阻断原因显示；1440保留双栏和原桌面登录入口，移动“更多”不显示 |
| 作用域与实时订阅 | HTML仅作文本；迟到账号响应、增量去重、较小ID迟到、权限撤销、500条历史边界和返回最新通过；实时检查写请求0 |
| 多面板回归 | 四面板、用户套餐作用域、聊天账号平台选择和合成辣椒聊登录通过；真实原生动作0 |
| 架构事实及工具 | update/check、所有权/Docker守卫、仓库/迁移守卫、34项Python测试、3项Node渲染缓存测试通过；只更新证据/指纹，六份Mermaid及对应SVG均未重画 |
| CodeBoarding | 增量入口在预检以 `compatible_baseline_missing` 阻断，请求/预留/上报Token均为0；未读取密钥、恢复不可信缓存或全量回退 |

新增检查首次在抽屉入场动画开始时读取矩形，得到屏幕外的过渡位置而失败；
截图确认最终抽屉正常。检查改为等待抽屉实际进入视口后再测量，完整三组浏览器
回归重新通过，没有放宽溢出断言。截图使用合成消息，已复核390手机会话、列表
及1440桌面；产物位于忽略目录 `output/playwright/messages-*.png`。

本批没有构建/替换Docker镜像或重启5173服务，也没有提交、推送或运行远端CI。
没有重新运行后端Go、数据库和容器发布测试；本批通过的是前端检查，不是完整发布
或原生/容量验收。`native_ready=false`、`ai_ready=false`不变。

## 消息中心受控文本命令与底座缺口（2026-10-28）

本批修改当前产品工作树，不回退既有桌面登录、会话、解码或手机布局。
新增消息输入与独立受控命令接口，复用原有control发送意图、单次提交及回显对账。
**生产发送器未注册，真实发送仍关闭**；不能把页面或合成发送成功描述为已打通真实群聊。
接口范围及未交付项见 [`native-conversations.md`](native-conversations.md#manual-text-commands)
与 [`native-foundation.md`](native-foundation.md)。

| 验证 | 本批实际结果 |
| --- | --- |
| Go / race / sodium | 最终 `go test -race -tags sodium ./cmd/... ./internal/...` 通过；同范围 `go vet -tags sodium` 通过 |
| HTTP / PostgreSQL | 最终完整 `WSL_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/httpapi` 通过，66.241秒；只重建 `wsl_test`，未修改生产 `wsl` |
| 发送命令合成控制 | Stage保存密封意图，按原键Inspect不发送，Dispatch接受/未知状态真实返回，重复Dispatch不再次调用传输；校验超长UTF-8、伪造tenant/group、旧账号/绑定epoch、他人状态查询、租户拒绝、未绑定原生群及跨账号目标 |
| 产品前端 | 最终 `npm run build` 通过；CSS `index-CB6HtdLD.css`，JS `index-BgPhm3V_.js`；产品类型检查使用 `tsconfig.wsl.json` |
| 发送界面合成浏览器 | 320、390、647、1440宽度无整页/发送区溢出；确认取消、输入法、3000字节上限、权限撤销保留草稿、未知不重发、Stage响应丢失/503按原键核对、明确续交、账号切走再切回、组件卸载及群/私聊切换通过 |
| 真实状态与作用域 | 合成服务端接受不伪造送达/已读或本地消息气泡；按用户/租户/工作台/账号/会话隔离32条内存草稿，不持久存储消息；合成用例共7次Stage、4次Dispatch、1次Reconcile，真实原生动作0 |
| 既有前端回归 | 消息布局、手机单行顶部栏、目录、解码详情、私聊只读、安全转义、SSE去重/迟到ID/权限撤销/500条上限以及四面板套餐隔离通过；原实时用例写请求0 |
| 架构维护 | update/check、所有权/Docker守卫、仓库/迁移守卫、34项Python工具测试及3项Node渲染缓存测试通过。仅前端Mermaid依赖拓扑变化并重渲染；系统、后端及Agent视图保持原图 |
| CodeBoarding | 重新执行当前分支预检及增量入口，均在预检以 `compatible_baseline_missing` 阻断；请求、预留及上报Token均为0，未读取密钥。旧无作用域schema-1缓存完整保留，未重标、复制为schema-2或全量回退 |

新增发送检查已纳入现有Docker CI浏览器清单；本轮只在本地合成浏览器执行上述四组，
没有执行远端CI或声称全部CI用例已通过。截图为合成消息，位于忽略目录
`output/playwright/message-sending-{320,390,647,1440}.png`，已复核390手机界面。

首次 `go test -race -tags sodium ./...` 因既有忽略目录 `var` 内两种Go包混用而失败；
没有删除用户诊断文件。按产品命令及内部包范围重跑通过。初次新增夹具因空白页访问
sessionStorage、缺失JSON请求头而失败，修正后完整复测通过。扩充用例又确认503不能
当作确定未保存，及切走再切回/卸载必须使用视图版本而非仅比较账号字符串来阻断续交。

没有增加迁移、服务或依赖，没有提高资源配额；没有构建/替换镜像、重启5173、
触发原生短信/登录/发送/群管、提交或推送。当前Docker运行结果不是本批候选源码。
本轮未重跑容器smoke或Gateway隔离数据库整套测试，不作为发布全套验收。
真实文本发送还需nonce历史、可信角色/可发言证据与明确授权的隔离群验收；
原有两个readiness门槛保持关闭。

CodeBoarding首次全量初始化涉及白名单源码外发及受限费用，已向所有者请求明确授权；
未得到确认前不创建替代基线，不能把本轮阻断写成已修复或模型分析通过。

## 恢复、可信群身份与出站原语（2026-10-28）

本批继续修改当前产品工作树，未回退已审阅改动。补齐API进程内的数据库恢复、
明确确认的当前群身份采集、账号级最小状态恢复，以及私聊/@/引用/群撤回的部分协议底座。
**不是P0–P2整体交付完成，不是原生动作已打通或当前5173镜像已更新。**
本批范围与未交付项见[`native-foundation.md`](native-foundation.md#恢复可信角色与消息原语补齐2026-10-28)。

| 验证 | 当前候选源码的实际结果 |
| --- | --- |
| Go / race / sodium | 最终`go test -race -tags sodium ./cmd/... ./internal/...`通过；同范围`go vet -tags sodium`通过 |
| HTTP / PostgreSQL | 最终`WSL_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/httpapi`通过，63.760秒 |
| Gateway / PostgreSQL | 最终`WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/gateway`通过，18.791秒；与HTTP数据库测试串行，仅重建`wsl_test` |
| 数据库恢复与生命周期 | 单批100条、105条收敛、跳过被锁定操作、重复执行无重复审计、queued→expired和running→unknown通过；循环在数据库错误后继续、取消后退出，不调用任何原生端 |
| 可信角色采集与落库 | 当前socket的完整目录/成员/8/27证据、角色丢失、本人禁言、外来禁言身份、错误/截断证据、owner/revision/epoch/部署变化、陈旧/未来/较旧样本、会话撤销通过；分页开始时间不能被结束时间替换成新鲜样本 |
| 原生角色失效 | member观察使原管理绑定停用、epoch提升并取消旧queued；相同admin观察不重新启用或重置绑定；伪造角色和owner请求参数拒绝，普通用户与跨工作台请求拒绝 |
| 解绑与保留后的状态恢复 | 解绑并使投影超过七天后，仍可按原键或操作ID取得本人最小状态；其他超管/账号不能读取；无正文返回、无额外传输调用，恢复路由没有stage/dispatch |
| 协议原语 | 既有纯文本黄金向量不变；UTF-16表情/中文@、精确范围和当前名片、引用应用字段及29–38元数据、跨群引用拒绝、群撤回7/13字段、独立私聊peer/session/7/1回执通过；仍未注册这些动作的生产执行器 |
| 取消与迟到回复 | 取消8/27读取后迟到完整回复仅成为notice；接收连接和心跳可继续，不生成消息、ACK或恢复水位。群/私聊晚回执同样不直接推断成功；未知邻近路由与非法边界仍拒绝 |
| 产品前端 | `npm run build`通过，产品类型检查使用`tsconfig.wsl.json`；CSS `index-gTIlDakl.css`，JS `index-DhuKYRZk.js` |
| 合成浏览器 | 五组检查通过：多面板、成员核验、受控发送、消息布局、实时订阅；320/390/647/1440宽度通过。核验取消不请求，确认仅提交group_id；结果显示单独禁言但不宣称完整发言许可；迟到账号响应不覆盖当前界面 |
| 发送回归 | 默认真实发送关闭、UTF-8上限、目标确认、原键恢复、未知不重发、账号/群草稿隔离及卸载栅栏通过；状态与回显查询使用账号级恢复路径，Stage/Dispatch仍使用原控制路径 |
| 架构与治理 | update/check、所有权/Docker守卫、仓库/迁移守卫、34项Python工具测试及3项Node渲染缓存测试通过；更新事实/指纹，六份Mermaid拓扑及匹配SVG均未重画 |
| CodeBoarding | 最终增量入口在预检以`compatible_baseline_missing`阻断；请求/预留/上报Token均为0，未读取密钥、全量回退或冒用旧缓存 |

早期回归中，旧目录测试只允许send_text为partial，新增加的私聊/引用/@/撤回原语导致
断言失败；改为明确列出这5项partial，同时继续强制每项动作受验收门槛阻断，其余未实现
动作保持pending。最终重新执行Go测试通过，没有放宽任何动作到accepted。
本地浏览器运行器原不含成员检查白名单，且成员检查未处理初始空白页的存储访问；补白名单、
固定合成来源及空白页容错，并将清理存储移到首次导航之后；成员与发送检查从空白页独立重跑通过。
截图曾包含确认弹窗退出的过渡帧，改为等待其实际隐藏后复核320截图，
未放宽视口或权限断言。

浏览器全部使用合成API和构建资产，阻止其他来源访问，真实原生动作数为0。
截图位于忽略目录`output/playwright/native-members-*.png`及既有消息截图目录。
没有重建`wsl`，没有增加迁移、依赖或Compose服务，也未扩大资源配额；
未构建/替换镜像、重启5173、运行容器smoke、提交/推送或触发远端CI。
本批不是完整发布、接收吞吐/长稳或真实原生验收。

仍需旧系统/备份nonce台账与共用密钥历史核验，以及明确限定账号、隔离群、目标、
允许动作、次数和期限的实测授权。私聊/辣椒聊持久发送、引用/撤回控制闭环、
禁言/解禁/踢人持久动作、媒体、好友/邀请、原生头像昵称和规则/任务真实执行仍未完成。
生产发送器保持未注册，`native_ready=false`、`ai_ready=false`，没有借目录更新或
合成测试宣称这些能力已开放。

## 可选 Docker 开发覆盖与热更新验证（2026-10-28）

本批只增加本地开发配置、操作脚本、合成测试及文档，不修改业务代码。
`compose.dev.yaml` 显式叠加在现有基础与缩容配置上，只替换前端启动方式；
后端仍是原有唯一连接所有者，没有增加第二套API、数据库或原生接收器。
**现有5173服务尚未切换到开发模式。**

- 前端使用非root Docker Vite，产品源码只读挂载，独立依赖缓存；不挂载宿主
  node_modules、环境文件、凭据或框架Demo。代理固定到现有API并保留浏览器同源头。
- 后端没有自动监听重启。`api-reload` 必须提供准确的确认参数，且检查Compose
  所属目录；确认后才构建独立`:dev`标签并仅重启API。不运行迁移或bootstrap。
- 开发前端上限为0.75CPU/512MiB，Node old-space上限384MiB，esbuild采用
  128MiB Go软限制/两线程，关闭额外Sass工作线程。镜像构建时预热依赖缓存。
  这些不是预留资源，也不是生产容量或所有未来修改均可在该上限运行的承诺。

| 检查 | 本批实际结果 |
| --- | --- |
| 配置与脚本 | 三份Compose合并校验、Shell语法、Node脚本语法通过；不带确认的后端重载以退出码2拒绝，未调用Docker重启 |
| 静态保护 | 6项开发配置测试及5项本地资源测试通过，共11项；已接入现有CI测试镜像，工作流actionlint通过 |
| 开发镜像 | `wsl-web:dev`构建成功，最终镜像ID `sha256:d923edf442fb56283d97f229c28dc1a823fb75e3d8b04f85c74ec79a9fc20575`；未移动当前或回滚发布标签 |
| CSS与Vue热更新 | 独立Docker前端与合成API验证通过；临时CSS及Vue模板更新均无需整页刷新，未提交的用户名输入与页面标记保留 |
| 代理与流 | 合成POST核对Host、Origin、Cookie和CSRF头保留；SSE首个事件及时到达，不使用真实业务或原生接口 |
| 文件边界 | `.env`、`.env.local`、`.git/config`、密钥扩展路径、越界`/@fs`及框架Demo入口均403/404；没有真实敏感文件作为夹具 |
| 资源与视口 | 320、390、647、1440截图完成，无浏览器脚本异常；最终短测前端cgroup峰值228691968字节，约218.1MiB，未OOM，容器上限512MiB。只覆盖该合成登录页和热更新过程，不是整套服务压测 |
| 产品构建 | 本批产品类型检查/生产构建通过；开发Vite配置不替换生产配置，未将开发服务器部署为生产入口 |
| 架构 | update/check、所有权/Docker守卫、仓库/迁移守卫、34项Python工具测试及3项渲染缓存测试通过；六个生成视图拓扑不变，渲染确认原SVG仍匹配。另补基于实际开发配置的可选系统视图 |
| CodeBoarding | 当前预检仍以`compatible_baseline_missing`阻断，请求/预留/上报Token均0；未读取模型密钥、初始化基线或调用付费分析 |
| 清理与运行隔离 | 合成测试容器、匿名缓存、网络及宿主临时文件已删除；只删除本轮生成的两份旧无标签开发镜像，没有全局prune或清理他人资源 |

初次运行发现未预热的依赖优化在512MiB限制下OOM；随后将依赖预打包移入镜像构建，
并限制运行期额外并行，重新完整测试通过，没有直接扩大内存配额。另一轮检查发现
不存在的敏感路径会进入SPA首页回退；改为明确403后重测通过，不把返回首页等同于
真实密钥泄露，也不放宽文件边界断言。早期隔离`internal`网络在本机Docker Desktop
未发布宿主端口，最终采用独立桥接网络并阻止浏览器访问其他来源；从未加入生产网络。
一次并行工具检查期间架构check报告manifest陈旧；复查当前输入指纹无差异，待其他
检查结束后串行重跑通过，没有为消除该提示修改业务代码或重画拓扑。

运行中的API仍为容器`2171ffab70f0`、镜像`dd318008abd8`，启动时间
`2026-10-27T15:42:22.905944586Z`；Web仍为`43fdfc803f11`、镜像
`b619c4320d4c`，启动时间`2026-10-27T11:55:07.670137094Z`。
与本批开始前一致，没有切换前端、重启账号连接、读写生产数据库、触发真实登录/
消息/群管、提交、推送或启动远端CI。原生与AI就绪门槛不变。
本批没有重跑Go/race/sodium、数据库与全套发布容器验收，不能据此宣称生产发布通过。

## Docker 开发模式复核与登录后界面（2026-10-28）

本轮核对当前产品工作树与实际运行容器，没有切换现有前端、重启API或执行真实原生动作。
当前5173仍使用`wsl-web:local`发布镜像，不含源码挂载；配置存在不等于已启用HMR。
现有`wsl-web:dev`镜像为`d923edf442fb`，本轮未重新构建或移动发布/回滚标签。

| 检查 | 本轮实际结果 |
| --- | --- |
| Compose与资源保护 | 三份Compose合并校验、11项开发/缩容配置测试通过 |
| Docker HMR | CSS与Vue模板热更新通过；页面标记及未提交输入保留，不依赖整页刷新 |
| 代理与来源边界 | 同源Host、Origin、Cookie、CSRF与SSE通过；敏感路径及框架入口保持403/404 |
| 登录后产品界面 | 隔离Vite前端新增复用成员核验、受控发送、消息布局三组合成检查，全部通过；覆盖320/390/647/1440宽度、身份确认、草稿与迟到响应隔离、未知结果不重发及租户入口限制 |
| 开发前端资源 | 最终短测峰值226119680字节，约215.6MiB，容器上限512MiB、0.75CPU，未OOM；不代表所有未来模块或生产负载容量 |
| 现有项目采样 | 发布模式五个常驻容器合计约245MiB；Docker引擎约7.65GiB内存、10CPU。采样包含隔离测试容器时共88个容器、容器用量约2672MiB，不含完整宿主/虚拟机开销，不能作为长期余量承诺 |
| 数据库状态 | 当前running/healthy；Docker仍有`OOMKilled=true`标记、RestartCount=0，未据此推断本轮发生OOM、具体时间或原因，也未重启数据库 |
| 架构与仓库 | 架构事实check、所有权/Docker守卫、仓库/迁移守卫通过；本轮仅测试与文档变化，无生产结构变化，不重画Mermaid、不调用CodeBoarding模型 |

第一次扩展检查的三组界面流程已完成，但汇总器把只读布局检查的返回值也当作
`native_actions`字段，造成断言失败；按各检查已有返回契约修正后完整重跑通过，
没有放宽业务权限或未知结果断言。所有新增接口响应均为合成数据，真实原生动作数为0。

这套预算足够作为当前前端开发起点，不代表Go后端热替换。依赖/锁文件变化需重建前端
镜像；新增API及原生执行链路需另行审核、构建并明确确认重启API，可能中断账号连接。
不应在缩容机器上并行执行镜像构建与数据库集成测试。测试结束只清理本轮隔离资源，
不清理其他项目容器、生产卷或已有镜像。本轮未重跑Go或数据库套件，不是原生发布验收。

## HMR边界固化与引用/@持久接线（2026-10-28）

本轮在当前产品工作树继续实现，不覆盖旧改动，不部署候选源码。
`AGENTS.md`新增HMR/后端重启/迁移/协议启用/原生验收的分界，并由开发配置测试
检查约束仍在；没有为刷新前端而重启API、账号连接或切换发布/开发镜像。

群引用及@复用原有Stage/Dispatch、nonce账本、测试窗口、当前socket、最终控制事务及
未知结果处理。引用来自保留期内本群已认证原消息，不允许浏览器提供正文或NIM路由；
意图保存事件ID和原始/投影密文摘要，提交前重查保留期、版本与原证据。
当前连接再认证原始封包及新鲜成员映射。@需匹配UTF-16正文范围、当前成员名片和
测试明确目标。详见[`native-foundation.md`](native-foundation.md)的“引用与 @ 的持久提交接线”。
消息中心尚无引用选择/成员选择交互，不能把本批API底座当作完整UI或真实发送开放。

| 验证 | 当前候选源码实际结果 |
| --- | --- |
| Go / race / sodium | `go test -race -tags sodium ./cmd/... ./internal/...`通过；最终复查命中测试缓存，不含默认跳过的数据库测试 |
| Go vet | `go vet -tags sodium ./cmd/... ./internal/...`通过 |
| HTTP / PostgreSQL | 最终完整`WSL_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/httpapi`通过，67.520秒 |
| Gateway / PostgreSQL | HTTP完成后串行运行`WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/gateway`通过，18.593秒；仅重建`wsl_test`，未重建或迁移现有`wsl` |
| 引用与@控制事务 | 有效组件、同键恢复、跨账号/群、私聊、未来/过期、错误部署/密封、超长引用、非法UTF-16范围、未授权目标通过；Stage后证据到期/变化及最终栅栏目标撤销均阻断，拒绝的Stage不占测试次数或创建意图 |
| 当前所有者与协议 | 合成加密原消息、业务/NIM群、发送者唯一映射、客户端/服务端ID、精确正文、时间与密文校验通过；数据库原消息过期或跨群无写出，合法引用与@在单次8/2写出中保留，无纯文本降级 |
| 历史兼容 | 共享投影AAD辅助函数保持既有命名空间；纯文本请求省略新增组件字段，既有编码与发送回归通过，无密文迁移 |
| 产品前端 | `npm run build`通过，使用`tsconfig.wsl.json`；CSS`index-gTIlDakl.css`，JS`index-DRPma_Ia.js`；补引用证据/测试目标错误提示，未新增发送启用入口 |
| 合成浏览器 | 成员核验、受控发送、消息布局三组通过，覆盖320/390/647/1440宽度；确认取消、未知不重发、原键核对、草稿/迟到响应/卸载隔离和默认关闭通过；合成7次Stage、4次Dispatch、1次Reconcile，真实原生动作0 |
| HMR与资源保护 | 12项Docker开发/缩容配置测试通过，包括新增项目约束检查；没有重新执行HMR资源压测，不引用旧峰值作为本批测量 |
| 架构与仓库 | update/check、所有权/Docker、仓库/迁移守卫、34项Python架构工具测试、3项Node渲染缓存测试通过；本批仅更新`graph.json`及`manifest.json`证据，六份Mermaid及SVG均未重画 |
| CodeBoarding | 当前增量入口预检以`compatible_baseline_missing`阻断；请求、预留及上报Token均0。未读取密钥、外发源码、复制旧缓存或全量回退 |

首次HTTP完整回归在既有授权码用例的十分钟有效期断言失败（业务返回err为nil）；
该用例单独复跑4.930秒通过，随后完整套件67.520秒通过。此次未定位瞬时时间边界原因，
没有修改授权码逻辑或放宽断言，不把首次失败隐藏为从未发生。

收尾只读确认API仍为`2171ffab70f0`/`wsl-api:local`、Web仍为
`43fdfc803f11`/`wsl-web:local`，两者healthy；当前5173没有自动切到开发HMR。
没有构建/替换镜像、重启API、执行容器smoke、提交/推送或触发远端CI。
本批不是完整发布验收或生产性能测量。

仍未具备nonce旧系统/备份与共用密钥历史、完整原生发言策略及明确隔离账号/群的实测
授权。私聊/辣椒聊持久发送、撤回、禁言/解禁/踢人执行、媒体、好友/邀请、
原生资料和规则/任务真实执行仍未交付。生产发送器未注册，
`native_ready=false`、`ai_ready=false`保持不变。

## 桌面原生注册参数与1001错误范围修正（2026-10-28）

本轮针对“注册账号却返回登录被拒绝、上游1001”排查和修正候选源码，不自动注册、
发送短信、登录或重试。对现有`wsl`仅执行一次最小只读状态查询，不读取手机号、
密码、令牌或验证载荷；最近注册尝试确为`rejected`，旧结果码却是
`native_login_rejected_unspecified`。历史记录没有保留上游原始错误正文，
因此不能仅凭1001还原本次具体拒绝原因，也没有改写既有记录。

固定官方桌面2.8.3资源的注册表单使用字符串国家码及手机号、掩码号码，
短信参数不带旧`Key`，注册参数不带`nickName`。本轮按该源码修正请求；
电话号码仍从当前验证会话绑定，后续请求不能换号。网站机器人名称仍是本地备注，
不冒充原生昵称。旧昵称输入仅兼容解码，不提交给桌面注册接口。
证据、资源哈希及桌面客户端传输码与业务响应码的区别见
[`official-client-research.md`](official-client-research.md)。

1001按登录、注册、短信路由分别分类；仅对白名单中的完整安全错误短语生成内部码，
不返回、记录或持久化任意上游正文。没有把客户端自身的1001“解析错误”推定为
本次HTTP业务1001的原因。已提交结果未知、验证码状态和人工重试边界保持不变。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go / race / sodium | `go test -race -tags sodium ./cmd/... ./internal/...`通过；包含路由分类、单次提交、隐私过滤及桌面注册参数回归，不包含默认跳过的数据库测试 |
| Go vet | `go vet -tags sodium ./cmd/... ./internal/...`通过 |
| HTTP / PostgreSQL | `WSL_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/httpapi`完整通过，81.618秒；仅重建`wsl_test`，未迁移或重建`wsl` |
| 注册状态恢复 | 合成1001保存为注册拒绝，轮询返回同一结果且不再次调用注册；之后只允许既有显式人工提交路径，不自动开户或登录 |
| 产品前端 | `npm run build`通过，使用`tsconfig.wsl.json`；CSS`index-v_0LvzlR.css`、JS`index-DIdziQ8p.js` |
| 合成浏览器 | `tools/run_multi_platform_ui_smoke.mjs native_auth_error_ui_smoke.js`通过；320/390/647/1440宽度，检查注册/登录错误区分、无原生昵称字段、1001显示、同意协议、验证码回调及未知结果不重试；合成POST共4次，真实原生动作0 |
| 布局复核 | 已查看390宽度注册拒绝截图，错误文字换行、表单与底部按钮位于弹窗内；其他宽度由浏览器边界断言检查 |
| 架构与仓库 | update/check、所有权/Docker及仓库/迁移守卫、34项Python工具测试、3项渲染缓存测试通过；仅更新图证据与manifest，六份Mermaid拓扑与SVG不变 |
| CodeBoarding | 当前增量预检仍以`compatible_baseline_missing`阻断，请求、预留及上报Token均0；未读取密钥、外发源码、复制基线或自动全量分析 |

本轮没有重跑旧的`native_signup_ui_smoke.js`及`native_auth_tabs_ui_smoke.js`，
只修正其与已移除昵称输入不一致的选择器；不把脚本更新称为执行通过。
没有重跑gateway数据库套件、构建发布镜像、容器smoke或远端CI，不能称为完整发布验收。

收尾只读确认API仍为`2171ffab70f0`/`wsl-api:local`，Web仍为
`43fdfc803f11`/`wsl-web:local`，两者healthy，5173尚未使用本轮候选源码。
没有重启API或现有账号连接，没有提交或推送。实际注册成功与否需在明确部署窗口
更新后由用户手动验证；参数与错误提示修正不等同于上游真实开户验收通过，
`native_ready=false`、`ai_ready=false`保持不变。

## 紧凑面板切换与三类工作台补全（2026-10-28）

本轮仅修改候选前端、合成界面测试和说明文档，沿用既有后端查询及权限，
不新增服务、API、迁移或原生动作。切换器保留服务器返回的图标/名称网格，
限定为两列、最大344px宽；覆盖框架针对手机表单弹窗的强制宽度和正文留白，
不影响其他登录/配置弹窗。手机选择后关闭侧栏，选择本身不提交业务请求。

新增展示组件`RobotWorkbench.vue`复用现有账号、群、总览及人设投影：
辣椒聊展示接入/授权/功能入口，热群展示两个平台的独立聊天账号、已绑定群、
人设与配置入口。连接记录不冒充实时健康，模型遥测未采集不显示虚假数字，
原生/AI验收门槛不变。账号详情纠正辣椒聊平台名称，工作台保存各自的页面同步时间。
热群总览关联群数可含待分配申请，已绑定群列表不冒充所有申请或原生成员目录。

用户中心增加套餐状态、操作权限、原用户/租户作用域筛选、手机群列表及
超管管理入口；撤销授权、套餐过期或权限不足时禁用热群编辑，群功能入口继续受
已授权平台和原有字段权限约束。用户中心不再显示无关账号范围或其他面板同步时间。
客户端进入热群控制台额外检查超管角色，后端仍是最终授权来源。

| 检查 | 本轮实际结果 |
| --- | --- |
| 产品类型/构建 | `npm run build`通过，使用`tsconfig.wsl.json`；最终CSS`index-DND3-JIk.css`、JS`index-D8fl1RBQ.js`，Vite构建14.85秒 |
| 面板与用户中心 | 扩展`multi_platform_ui_smoke.js`通过，覆盖320/390/647/1440宽度；切换器实际内容尺寸、两平台热群账号、账号选择保留、聊天草稿恢复、用户筛选、超管入口及租户入口隔离通过 |
| 禁用与副作用 | 已撤销群和过期套餐不能打开热群配置；视图切换不保存配置、不启用接收；辣椒聊登录仅以3次明确合成POST验证前端路由，全部被拦截，真实原生动作0 |
| 既有页面回归 | 同一最终构建的注册/登录错误、消息布局、受控发送、成员核验四组合成检查全部通过；发送夹具7次Stage、4次Dispatch、1次Reconcile，均非原生请求 |
| 视觉复核 | 查看390宽度切换器、辣椒聊、热群及用户中心截图；切换器紧凑，手机群列表没有横向表格挤压，四种宽度由浏览器断言检查 |
| 架构证据 | update/check、所有权/Docker及仓库/迁移守卫通过；新增组件仅影响frontend Mermaid及证据，其他五份图不重画；frontend SVG重新渲染，其他SVG复用已核验缓存 |
| 工具测试 | 34项Python架构测试、3项Node渲染缓存测试通过；两份修改的浏览器脚本Node语法检查及`git diff --check`通过 |
| CodeBoarding | 当前增量入口仍以`compatible_baseline_missing`阻断，请求、预留和上报Token均0；没有初始化基线、调用模型或外发源码 |

初次构建发现格式化后的旧多语句弹窗关闭表达式不能被Vue编译，提取为具名函数后
重新完整构建通过。界面测试首先误测了全屏遮罩而非弹窗内容，修正测量目标；
随后真实发现框架手机宽度覆盖，限定到切换器修正后通过，没有放宽344px断言。
扩展测试还修正了带连接状态的下拉项名称，以及原本会保留账号选择的工作台返回预期；
补入有效的合成账号指标响应，继续验证账号保留而非将选择清空来满足测试。

`workspaces_ui_smoke.js`仅更新热群账号入口选择器并执行语法检查，没有运行这份需要
网站会话的完整旧脚本；不把选择器更新当作执行通过。没有重跑Go/race/sodium、
数据库或容器发布smoke，也没有在缩容机器上声称验证生产吞吐。

收尾只读确认API`2171ffab70f0`/`wsl-api:local`、Web`43fdfc803f11`/
`wsl-web:local`仍healthy，运行容器未变化，5173没有自动启用HMR或载入候选构建。
未构建镜像、重启API/账号连接、读写业务数据库、提交、推送或启动远端CI。
工作树还包含上一轮注册前后端修正，部署时需配套审核并确认API重启窗口，
不能仅替换新注册表单而保留不兼容的旧API。当前仍不是原生功能或生产发布验收。

## 原生头像、改密与换绑手机号候选接入（2026-10-29）

本轮在当前产品工作树增加账号安全分步接口和`NativeSecurity.vue`，继续使用桌面
会话、账号串行业务队列、两请求全局上限、最终控制栅栏及持久操作记录。
没有增加连接所有者、生产服务、移动登录接入器或任意上传代理。
固定桌面资源、官方API定义及哈希见`official-client-research.md`；
手机号换绑只有官方API契约依据，桌面2.8.3没有相应页面，仍待真实账号验收。
具体请求、数据保留和结果边界见`native-settings.md`。

头像限定1MiB JPG/PNG，解码限制4096边长/4MP，在内存裁剪为256×256 JPEG并去除元数据，
固定上传目的地，随后图片检查、设置头像；每步重新检查提交权限，无磁盘图片缓存。
密码必须先手动完成原手机短信验证，再提交旧/新密码；换绑必须完成原手机证明及新手机验证码。
短信需要用户确认和手动人机验证，浏览器不能提供服务端密钥、证明或上游地址。
密码、验证码、验证答案及原图不写入持久操作；幂等校验使用密钥化、账号作用域指纹，
验证流程仅短期加密保存必要凭据和新号码。成功改密/换绑在同一结果事务中标记重新认证、
提升epoch/version、停用接入与接收；不会自动登录或重新启用接收。
未知安全结果只恢复原请求查询，不通过再次登录试探密码、不自动补发或重复修改。

| 检查 | 本轮候选实际结果 |
| --- | --- |
| Go / race / sodium | `go test -race -tags sodium ./cmd/... ./internal/...`通过，部分未变包使用缓存；数据库测试另列 |
| Go vet | `go vet -tags sodium ./cmd/... ./internal/...`通过 |
| 串行数据库回归 | `WSL_INTEGRATION=1 WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/httpapi ./internal/gateway`通过，HTTP43.329秒、gateway18.043秒；只重建`wsl_test` |
| 合成协议 | 密码三步、手机四步、头像三次提交共10次最终栅栏；校验加密请求参数、上传字段、固定来源、越权阻断、输入白名单、图片解码/元数据限制通过，无真实原生请求 |
| 控制与机密 | 阶段跳过、替换流程、其他超管复用、跨工作台、过期、同键恢复、改变幂等输入、未知结果阻断、结果落库不含密码/验证码、终态清理证明及停用旧接收通过 |
| 产品构建 | `npm run build`通过，使用`tsconfig.wsl.json`；CSS`index-BAOppaSO.css`、JS`index-xl8MPabm.js`，Vite11.23秒 |
| 合成界面 | 最终构建的账号设置、成员核验、受控发送、四工作台四组检查通过；320/390/647/1440宽度；设置10次合成提交及8步安全操作，取消不提交、未知不重试、原键恢复、作用域/草稿隔离、手机号掩码、验证恢复和存储无机密通过 |
| 视觉复核 | 查看320和390宽度账号安全截图，表单滚动、固定底部和密码字段未横向溢出；其他宽度由浏览器边界断言验证 |
| 架构与仓库 | update/check、所有权/Docker守卫、仓库/迁移守卫及`git diff --check`通过；仅新增安全组件影响frontend图，其他视图拓扑未改，SVG渲染检查通过 |
| 工具测试 | 34项Python架构测试、3项Node渲染缓存测试通过；Python首轮本地供应商夹具遇到环境代理HTTP502，清除该命令代理变量后完整34项重跑通过 |
| CodeBoarding | 本轮增量预检仍以`compatible_baseline_missing`阻断，fresh report时间`2026-10-28T18:46:27Z`（上海10月29日02:46）；模型请求和Token预留/上报均0，未读取密钥、外发源码、复制旧基线或自动全量回退 |
| 候选容器 | 临时独立数据库、角色、迁移/bootstrap、非root/只读API/Web、静态资源、四面板、SSE及默认关闭通过；新增确认022强制RLS且租户无权限，超过64KiB的非法合成上传仅新接口到达API并被拒绝，其他接口仍413 |

前向迁移`022_native_security.sql`哈希
`8d6790acf1115063e7ed36b87accfa499dfe948db5837c6c531d1702450df7f7`，
旧迁移未修改。只读在线检查：3个账号、0个未决设置操作、0个执行中/未知原生操作、
0个已启用接收，022尚未应用。没有读取账号凭据或向在线数据库写入。

Docker Hub拉取基础镜像超时；本轮未声称标准Dockerfile从干净基础镜像构建成功。
候选使用现存`wsl-test:local`构建层和当前API/Web运行层，并在仓库外临时目录
按现有锁文件生成vendor，离线编译同一候选Go源码、复制已验证前端和对应代理配置。
首次离线编译发现旧构建层缺少依赖，补充仓库外vendor后重建通过；
首次Web临时构建被项目`.dockerignore`排除dist，改用仓库外专用上下文后通过。
没有更改生产Dockerfile、依赖锁文件或引入第二条生产部署方式；
镜像依然只能经原Compose部署，标准联网可重现构建尚受基础镜像网络阻断。

候选镜像：

- `wsl-api:settings-candidate-20261029`：
  `sha256:6a796f6980f0d8c6bf1c9d89430c95dac3dbf5f70074edf719e8441f96838456`。
- `wsl-web:settings-candidate-20261029`：
  `sha256:8c969831568828a9ba70fac2fff03954952af441c80c9004b44d30b1cb2098fe`。

在线仍为API`61f516b6c416`/`017d4c7720bf`和Web`aa6f9dcd27d1`/`d81712ddbf3d`，
均healthy，未移动local或rollback标签。未应用022、重启API/Web、登录、发送短信、
上传真实头像、改密、换绑、执行群操作、提交、推送或触发远端CI。
临时smoke资源已按本轮标签清理；当前候选需新的明确API重启窗口后才能部署。
所有原生/AI全局就绪门槛保持false；上述结果不代表真实账号、生产吞吐或长稳验收。

## 头像与账号安全部署完成（2026-10-29）

用户明确回复“允许”，批准本次API重启、Web配套更新及022迁移。
本节记录实际部署，补充上节候选状态，不把合成测试升级为真实原生验收。
使用原`wsl-platform` Compose及本地缩容覆盖，未切换HMR、增加服务或扩大配额。

| 检查 | 本轮实际结果 |
| --- | --- |
| 冻结与备份 | 停止唯一API写入者后，复查未决设置/原生操作均0、已启用接收0；生成一致数据库dump及单独0600配置副本，受保护目录0700，记录SHA256、最迟一天清理及恢复保留策略 |
| 恢复演练 | 仅在无发布端口的内部网络启动临时PostgreSQL，恢复全库后逐表行数/内容指纹一致；候选migrator只增加022及空验证流程表，原业务表指纹均不变；未启动副本API、原生接收或热群引擎 |
| 在线迁移 | 原Compose的候选migrator应用022；原业务表指纹与冻结点一致，租户无验证流程表权限，强制RLS有效，没有重建或重置`wsl` |
| 配套更新 | 仅重建API/Web；API容器`e3489919dc20`，镜像`6a796f6980f0`；Web容器`98048265bbbe`，镜像`8c9698315688`，均healthy、非root、只读、RestartCount0 |
| 缩容资源 | API仍1CPU/512MiB，Web仍0.25CPU/128MiB；PostgreSQL/heatbot/Simon的容器ID、镜像、挂载及资源限额不变 |
| 数据与会话 | 3个账号保留；账号及密封会话、绑定、原生群、台账、套餐、订阅、人设、部署/IM配置及接收记录指纹一致；`.env`及MASTER_KEY保持原值，当前接收仍关闭 |
| HTTP与Web | 5173健康接口返回ok及两个readiness=false；新JS`index-xl8MPabm.js`、CSS`index-BAOppaSO.css`已服务，JS含新安全接口；新GET/POST接口匿名请求均401，Nginx配置检查通过 |
| 运行边界 | 未决设置/原生操作仍0，验证流程0；没有原生登录、短信、头像上传、密码/手机号变更、群操作或模型调用 |
| 镜像保留 | `wsl-api:local`及`wsl-web:local`为新版本；两仓库各仅保留上一运行版`rollback-20261029-presecurity`；已显式无force删除更早回滚镜像及冗余候选别名；heatbot仍使用的`heatbot-runtime`、前端dev、测试及其他项目镜像未动 |

第一次收尾断言因为Docker返回挂载数组顺序变化而停止，API/Web此时已经healthy。
复核发现PostgreSQL容器、卷、路径、镜像与限额完全一致，只是数组顺序不同。
随后按挂载目标排序重新比较，并补完全部业务/HTTP/权限/机密保持验证通过；
没有重跑部署、重启数据库或为满足断言改动挂载。

受保护备份和核验记录：
`/Users/xinxinzi/.cache/artwslbot/deployments/20261029-native-security.uemHn3/`。
本轮内部恢复数据库及网络已经删除，没有清理业务卷或其他项目资源。
备份属于短期维护材料，恢复时仍须执行七天原文保留策略，不能当作永久聊天档案。

当前和回滚的完整镜像：

- API当前`sha256:6a796f6980f0d8c6bf1c9d89430c95dac3dbf5f70074edf719e8441f96838456`；
  上一运行版`sha256:017d4c7720bf1e0671beb63eaffce6319766c651101ca09ff410daa933144f6b`。
- Web当前`sha256:8c969831568828a9ba70fac2fff03954952af441c80c9004b44d30b1cb2098fe`；
  上一运行版`sha256:d81712ddbf3d684b21c225fd3c0398e995d514f29f2df55cf9d2a13ce1275109`。

022之后的代码回退必须审查新的安全操作、验证流程和会话变化，不能直接运行旧
migrator、降级表结构或恢复旧快照覆盖新写入。真实头像、改密和换绑仍由用户手动验收；
没有自动操作真实账号，也没有提高`native_ready`或`ai_ready`。
本轮是已批准候选的部署和运行核对，没有重新声称执行Go全量测试、远端CI、
CodeBoarding模型分析或生产吞吐/长稳验收；候选测试结果见上一节。

## 原头像显示修复候选（2026-10-29）

账号设置窗口此前使用默认人形图标，头像字段只显示“已设置”，没有读取图片预览。
本轮补上资料头部、头像表单的当前原生头像，以及独立的待上传图片预览。
选择文件不覆盖当前头像，也不触发上传；只有显式确认原有保存流程才会修改头像。
未查询、上传、清空或更改真实账号头像。

复用已有受限图片下载器；设置GET从当前快照生成十分钟加密引用，不增加一次原生
设置查询、不新增迁移或持久图片缓存。新资源GET绑定超管、账号、工作台及会话，
下载前后检查账号身份、epoch/version、凭据指纹和部署版本；固定CDN、
1MiB头像上限、两下载槽、十秒期限和no-store保持有效。详情见`native-settings.md`。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go / race / sodium / vet | 命令及内部包范围通过，未变包使用缓存；数据库测试另列 |
| PostgreSQL | HTTP完整串行重跑通过，46.918秒；gateway串行通过18.033秒，仅重建`wsl_test`。新增覆盖换账号/工作台/用户、过期/伪造引用、任意URL、下载槽满、凭据变化及下载中epoch/version、用户或部署变化 |
| 测试修正 | 首轮HTTP在最后删除用例遇到version_conflict：新增头像用例故意提升账号版本，旧删除夹具仍传1。删除预期改为2后完整重跑通过，未放宽业务CAS |
| 产品构建 | 产品vue-tsc及Vite通过，2228模块，10.52秒；CSS`index-p1F3iIL1.css`、JS`index-CYa3c6f-.js` |
| 合成浏览器 | 账号设置、消息媒体、成员、四工作台检查通过；320/390/647/1440宽度。设置检查保留10次合成配置提交和8步安全操作，新增7次头像读取与原图/新图分离断言，无真实原生请求 |
| 视觉检查 | 查看390宽度截图；使用彩色A/B合成头像，当前头像、替换预览及固定底部均可见，无横向溢出 |
| 架构 | 更新代码事实、检查与所有权/仓库守卫通过；只更新frontend拓扑，其余五图不重画。34项Python测试、3项渲染缓存测试、Mermaid渲染通过 |
| CodeBoarding | 本轮预检`2026-10-28T19:45:36Z`仍因`compatible_baseline_missing`阻断；请求/Token均0，没有外发源码、复制旧基线或全量回退 |
| 容器 | 候选API/Web使用既有构建/运行层与锁定vendor离线构建；独立临时数据库、迁移、强制RLS、非root只读、静态资源及关闭门槛smoke通过。首次宿主代理使localhost返回502，清除该检查的代理后完整重跑通过 |

候选镜像为`wsl-api:avatar-preview-candidate` /
`sha256:3394d64edeffae6725663ce09e5a749ab49c89cfd677acdcc065ecde0322778c`，
以及`wsl-web:avatar-preview-candidate` /
`sha256:3667cf751878bb6dc48cfb5a88cf7c863c5e73a7a1e00228e1b7c3f1c8d37085`。
沿用上一轮仓库外离线构建方法，未将其描述为标准Dockerfile联网干净重建。

尚未部署。线上API`e3489919dc20`、Web`98048265bbbe`及local/rollback标签未替换；
健康检查正常、两个readiness仍false。只读计数为3账号、22迁移、0未决设置、
0执行中/未知原生操作、0启用接收。没有修改`wsl`、重启连接、提交、推送或远端CI。
旧API/Web重启窗口已完成；本候选的API资源接口与Web需配套部署，待新的明确窗口。

## 默认本地同步授权与头像预览部署（2026-10-29）

用户要求后续修改默认重启同步。本轮将该长期本地交付授权写入`AGENTS.md`、
`docker-operations.md`和`docker-development.md`：每批完成且验证通过后，
默认向现有本地Compose同步并只重启必要服务，不再重复索要普通API窗口。
保留当前release/development模式、资源限额和前一个运行版本；失败检查、
未决提交仍阻断，破坏性删除、不兼容迁移和新部署目标另行确认，真实原生测试
不因此获准。它不是每次文件保存就自动重启，也没有新增监视器、服务或调度任务。
开发助手保留显式重启flag，提示与现有规则测试同步调整；12项Compose测试、
架构所有权/仓库守卫、Compose配置和差异格式检查通过。

采用上一节已通过测试、源码匹配的头像候选，本轮未把旧测试说成重新执行。
沿用现有`wsl-platform`、`compose.yaml + compose.local.yaml`，没有切换HMR。

| 检查 | 本轮部署实际结果 |
| --- | --- |
| 冻结与备份 | 核验0未决设置、0执行中/未知原生操作、0启用接收；停止唯一API写入者后做一致dump及独立配置备份，目录0700、文件0600，记录哈希、最迟一天清理和恢复保留政策 |
| 首次恢复演练 | 临时恢复库在启动阶段关闭连接，部署在移动local标签前退出，原API恢复健康；失败诊断和备份保留，没有触碰线上数据库或替换镜像 |
| 再次恢复演练 | pg_isready改查127.0.0.1 TCP后重做；仅内部网络的临时PostgreSQL恢复全库指纹一致，候选migrator无待执行迁移、仍22项，业务指纹不变，强制RLS及租户拒绝有效；未启动副本API或原生连接 |
| 运行升级 | API`81e1f026b4fa` / `3394d64edeff`；Web`f46dbfb31085` / `3667cf751878`，均healthy、非root、只读、RestartCount0 |
| 范围与数据 | 仅API/Web重建；PostgreSQL/heatbot/Simon的容器、镜像、挂载和资源限额不变。账号、原会话及接收配置、群绑定、套餐、台账和配置指纹一致，.env/MASTER_KEY未改；仍3账号，未执行线上迁移 |
| Web与路由 | 5173首页引用`index-CYa3c6f-.js`和`index-p1F3iIL1.css`，对应脚本含新头像及安全接口；新头像GET未经网站认证返回401 |
| 门槛 | healthz正常，native_ready/ai_ready保持false；未自动登录、读真实头像、上传、发短信、改密、换绑或执行群操作 |
| 回滚保留 | 当前API/Web为local，前一个实际运行版本为rollback-20261029-preavatar。核实无容器引用后显式无force删除更旧presecurity镜像，移除冗余候选别名；保留正在使用的heatbot-runtime及开发/测试工具镜像 |

成功部署清单位于受保护的仓库外
`~/.cache/artwslbot/deployments/20261029-avatar-preview.jaqkHG/`；
首次未提升镜像的诊断在`20261029-avatar-preview.Ew4Maf/`。
临时恢复数据库和网络已清理。没有恢复旧业务快照覆盖新写入，也没有提交、
推送、启动远端CI或调用CodeBoarding模型。本轮是本地同步规则与已验证候选部署，
真实头像显示和生产吞吐/长稳仍不作为自动验收完成。

## 好友审批与邀请、退群接入及部署（2026-10-29）

本轮在当前产品工作树继续实现用户指定的两项原生能力，保留已有未提交改动，
不覆盖原接入、资料、安全设置及读取调度。协议来源为固定官方桌面2.8.3离线副本；
具体路由、证据哈希、状态和限制见`native-social.md`。
没有增加服务、依赖、自动审批任务、私聊管理指令或第二个原生连接所有者。

好友审批支持当前返回窗口内的单项同意／拒绝，严格校验收件方向、待处理状态、
申请ID和用户身份。邀请一次一位当前好友，并复核完整群成员、自身身份及管理员角色；
退群要求已有授权和套餐、人工停用群管与热群服务，群主拒绝直接退出。
全部复用账号串行读取、最终控制栅栏、密封操作台账、幂等和未知结果恢复；
不重发未知动作，邀请回执不冒充实际入群。退群确认后标记离群角色并使旧任务失效，
保留原授权、绑定和历史，名额不会自动释放。

| 检查 | 本轮实际结果 |
| --- | --- |
| Go / race / sodium | `go test -race -tags sodium ./cmd/... ./internal/...`通过；未变包可使用缓存，数据库测试另列 |
| Go vet | `go vet -tags sodium ./cmd/... ./internal/...`通过 |
| 串行隔离数据库 | `WSL_INTEGRATION=1 WSL_GATEWAY_INTEGRATION=1 go test -race -tags sodium -count=1 -p 1 ./internal/httpapi ./internal/gateway`通过，HTTP48.740秒、gateway18.004秒；仅重建`wsl_test` |
| 合成协议 | 固定路由、签名请求、int64申请ID字符串、隐私位保持、输入及名单上限、最终栅栏、回包关联、错误回包进入未知、不自动重试通过；未请求真实平台 |
| 权限与恢复 | 跨工作台／租户拒绝、会话与凭据／epoch变化、套餐／模块／群绑定、当前角色与目标、重复提交、未知退群及只读对账、旧任务失效、保留历史、审计脱敏通过 |
| 产品构建 | `npm --prefix web run build`通过产品vue-tsc和Vite，11.35秒；JS`index-D1ovO3SG.js`，CSS`index-CO4eELkZ.css` |
| 合成界面 | 好友／群操作与原账号设置两组浏览器检查通过，320/390/647/1440宽度；社交2次合成提交及1次只读核对，取消不提交、长ID精度保持、未知恢复不重发；设置保留10次合成提交、8步安全流程及7次头像读取，真实原生请求均0 |
| 视觉检查 | 初次窄屏表格截断后，移动端改为紧凑申请列表；重新构建、四宽度检查及390截图复核通过，桌面仍为表格 |
| 架构与仓库 | update/check、所有权/Docker守卫、仓库/迁移守卫及差异格式检查通过；新增社交组件仅改变frontend拓扑，其他五图不重画，证据随源码更新 |
| 工具测试 | 34项Python架构测试、3项Mermaid渲染缓存测试、12项Compose覆盖测试通过；本地夹具检查清除环境代理，未调整产品代理策略 |
| CodeBoarding | 本轮预检时间`2026-10-28T20:52:41.461473Z`，因`compatible_baseline_missing`阻断；请求和Token预留／上报／未知均0，未读取模型密钥、外发源码、复制旧基线或全量回退 |
| 容器smoke | 候选API/Web在独立临时库完成角色、迁移、bootstrap、强制RLS及租户拒绝、非root／只读、登录退出与静态资源检查；关闭门槛有效，未使用生产数据 |

首轮能力目录测试仍断言好友列表为待接入，按本批真实实现改为部分接入后，
完整范围检查通过；未把合成检查或网页入口算作真实能力已验收。
前向迁移`023_native_social.sql`仅扩展原生设置操作类型约束，SHA-256为
`0b520e124ec7fea559743a2041434014533e39b7544e4c6de06b590e59928322`，
001–022内容不变，没有新增业务数据表或放宽RLS。

候选使用既有`wsl-test:local`构建层和当前API/Web运行层，在仓库外专用上下文
按既有锁文件vendor离线编译同一Go源码，复制已验证dist和原代理配置。
没有把它描述为标准Dockerfile从干净网络环境构建成功；Docker Hub基础镜像拉取
仍受前轮网络限制。正式交付仍仅使用原有Compose，没有增加部署入口。

沿用用户已确认的默认本地同步授权，本轮实际更新原`wsl-platform`的
`compose.yaml + compose.local.yaml`，保留release模式及本地缩容限额：

| 部署检查 | 本轮实际结果 |
| --- | --- |
| 预检 | 部署开始时已存在1个启用的接收器；未决设置操作及执行中／未知原生操作均0，没有修改其开关 |
| 冻结备份 | 停止唯一API写入者后保存一致dump和独立配置副本，目录0700／文件0600，记录哈希、最迟一天清理及恢复保留策略 |
| 恢复演练 | 内部网络临时PostgreSQL恢复全库指纹一致；候选migrator仅增加023，所有原业务指纹一致，强制RLS及租户拒绝有效；未启动副本API、接收或热群运行时 |
| 在线迁移 | 原Compose的migrate应用023，22项变23项；API恢复前全部业务指纹仍与冻结点一致，未重置`wsl`或覆盖新增数据 |
| 运行镜像 | API容器`63315524846c`，镜像`16b327d064b7`；Web容器`bfdfacf7c1f5`，镜像`8376890ac107`，均healthy、非root、只读、RestartCount0 |
| 范围和资源 | 只重建API/Web，仍为1CPU/512MiB及0.25CPU/128MiB；PostgreSQL、heatbot、Simon的容器、镜像、挂载及配额均不变 |
| 数据和会话 | 3个账号、原身份、版本、开关、授权绑定、群目录、套餐、订阅、人设、台账及IM配置保持；`.env`及MASTER_KEY未改。既有接收恢复引起续票密文、请求序号、游标、租约及接收快照正常变化 |
| HTTP | healthz正常且native_ready／ai_ready均false；新JS/CSS已服务，三条新社交路由匿名请求均401，Nginx配置检查通过 |
| 接收观察 | 原有1个已启用接收器恢复connected；本次采样75帧、302条消息、302次ACK写出，未决操作仍0。仅为现有接收恢复观察，不是社交动作或生产容量验收 |
| 镜像保留 | 当前API/Web为local，各保留紧邻上一运行版`rollback-20261029-presocial`；核实无容器引用后无force删除更旧preavatar镜像及冗余候选别名，保留运行中的heatbot-runtime和开发／测试、其他项目镜像 |

首次部署收尾对整个账号／接收表作严格指纹比较，因原有接收器自动续票和恢复运行
而停止，API/Web此时已healthy。保留该失败报告，随后在内存比较备份和当前字段：
账号仅`credential_sealed`变化，部署仅`request_id_ns`变化，接收记录仅
`owner/lease_until/observed_at/snapshot/cursor`变化；原生身份、epoch/version、
配置、启停与其余业务指纹一致。补完HTTP、路由、配置、资源及门槛核验后通过，
没有为满足断言暂停接收、改写会话、恢复旧快照或重跑部署。

本轮私有部署、备份及恢复清单位于
`~/.cache/artwslbot/deployments/20261029-native-social.gAeSrh/`；
首次严格断言失败与后续逐字段核验分别保留。临时恢复库与网络已清理。
当前完整镜像为API
`sha256:16b327d064b77f4fc0543975b07dcfa05e883db6dd46fec22effba4dc00dfbac`
和Web`sha256:8376890ac107bc4cc376b76461c5e0e15423848dfafe7550203740e89842d953`；
上一运行版为API
`sha256:3394d64edeffae6725663ce09e5a749ab49c89cfd677acdcc065ecde0322778c`
和Web`sha256:3667cf751878bb6dc48cfb5a88cf7c863c5e73a7a1e00228e1b7c3f1c8d37085`。

没有主动登录、发短信、审批真实好友、邀请、退群、模型调用、提交、推送或远端CI。
原有接收按既有启用配置恢复连接和续票，不把它描述为完全没有原生网络活动。
自动好友审批模块保持关闭；两项能力目录为部分接入、真实验收待完成。
CodeBoarding预检阻断与代码事实检查通过分别记录，尚无本轮远端CI或目标机吞吐／长稳结果。

## 原生群管底座与受控 Tools 实现、部署（2026-10-29）

本批基于当前产品工作树补齐旺商聊个人禁言／解禁、全群禁言／解禁、
单成员移除和发布公告，另接账号级共享的邀请记录读取。固定桌面2.8.3协议、
读取证据、控制栅栏及限制见`controlled-tools.md`。沿用现有接收连接所有者、
账号串行业务读取和全局两个在途上限，没有新增原生连接或常驻服务。
群资料兼容已核实的嵌套`groupInfo`及原扁平响应；混合／冲突群ID拒绝。
已删除公告不参与期望状态匹配。邀请记录每账号最多缓存4096条、30秒、最多20账号，
按已核验群映射过滤；不冒充完整历史、角色证明或财务奖励证据。

受控测试页升级为授权窗口及固定JSON Schema Tools面板，新增
`GET /api/v1/test-tools`、`POST /api/v1/test-tools/call`，提供`read_group`、
`read_invitations`、`run_moderation`、`inspect_operation`和
`reconcile_operation`；CLI增加`tools`和`tool-call`。这是网站会话认证的
JSON Tools，并未实现MCP传输协议或增加MCP服务。没有将原生执行工具注册给
租户群管助手或heatbot，也没有让模型创建／扩大授权、运行Shell/SQL或任意HTTP。

授权必须由超管明确选择隔离授权群、账号、目标、动作、时限和次数；
默认上限300秒、10次提交尝试，个人禁言最长5分钟。账号需启用受控测试与接收，
具备新鲜管理员／群主证据。勾选确认隔离群只标记所选群并同事务审计。
每次读结果及最终提交重新校验会话、套餐、群绑定、epoch和当前原生角色。
准备时原子扣除次数并保存密封意图，同幂等键恢复原操作，不重复扣次；
取消使未提交动作失效，已经提交的未知结果只能查询或只读核对。
个人禁言名单的布尔状态不能证明时长，不能据此把未知限时禁言改为成功。

| 检查 | 本批实际结果 |
| --- | --- |
| Go / race / sodium | `go test -race -tags sodium ./cmd/... ./internal/...`最终完整产品范围通过；未变包可使用缓存，数据库测试另列 |
| Go vet | `go vet -tags sodium ./cmd/... ./internal/...`通过 |
| 串行HTTP数据库 | `WSL_INTEGRATION=1 go test -race -p 1 -tags sodium ./internal/httpapi -count=1`通过，46.860秒；授权、并发预算、撤销、重复键、未知核对及原功能回归 |
| 串行gateway数据库 | `WSL_GATEWAY_INTEGRATION=1 go test -race -p 1 -tags sodium ./internal/gateway -count=1`通过，17.980秒；邀请同账号采集合并与过滤、最终栅栏及原链路回归；仅重建`wsl_test` |
| 合成协议 | 六条固定动作路由与载荷、发送前栅栏、长ID精度、角色层级、回包关联、嵌套群资料、已删除公告、邀请枚举／游标、名单未知与限时禁言不误核对通过；未操作真实群 |
| 权限与持久化 | Tools超管／工作台限制、有效隔离授权、目标白名单、预算占用、相同键不重发、账号／绑定epoch变化、取消及到期、套餐拒绝和审计脱敏通过；租户助手及heatbot工具集不扩权 |
| 产品构建 | 产品vue-tsc及Vite通过，2236模块，11.030秒；最终JS`index-6dM6Qyi5.js`，CSS`index-1TtWUU73.css` |
| 合成浏览器 | Tools及社交两组检查通过，320/390/647/1440宽度。Tools一项合成提交、取消不提交、未知后禁重发、刷新恢复原键、取消授权后只读核对；最终提示修正后重跑Tools四宽度通过。真实原生动作0 |
| 界面状态 | 只读核对显示“已观察到期望状态，未重复提交”；上游确认与实际生效区分。临时保存仅原操作键／账号／授权／时间，不存公告正文、成员名单或凭据 |
| 架构事实 | update/check、所有权／Docker守卫、仓库／迁移守卫和差异格式检查通过；新增受控Tools业务视图，更新实际受影响的frontend关系，其他图未重画，最终微调仅manifest变化 |
| 工具测试 | 35项Python架构测试、3项渲染缓存测试、12项Compose测试通过；七张Mermaid视图渲染验证通过，未变化图命中缓存 |
| CodeBoarding | 最新预检`2026-10-29T09:21:23.106168Z`因`compatible_baseline_missing`阻断；请求、Token及未知用量均0，没有读取模型密钥、外发源码、复用不兼容基线或全量回退 |
| 容器smoke | 最终API及最终Web组合在一次性数据库完成迁移、bootstrap、强制RLS、Tools目录、只读非root、登录退出、代理及静态资源检查；未使用生产数据，两项readiness均false |

初次完整测试发现无效邀请游标被归一化为空串；已改为严格拒绝并补回归。
顶层`go test ./...`／race因本机忽略目录`var/`内研究副本的混合Go包失败，
不能称该命令通过；实际产品入口／内部包范围如上，CI构建上下文不含该研究目录。
一次gateway运行误用HTTP测试开关而跳过数据库测试，随后以正确
`WSL_GATEWAY_INTEGRATION=1`明确重跑完整gateway，表中为后者。
架构测试及一次容器检查受到宿主localhost代理影响，分别以本地NO_PROXY配置重跑
通过，没有更改产品网络策略或放宽检查。

初次候选提升早于完整race结果确认，是本轮部署顺序错误。发现失败后已恢复
上一版兼容API/Web，未回退数据库；024保留，失败报告标记为被替代，不作为最终
验收。修复后等待全部产品范围race、串行数据库、构建及最终镜像smoke通过，
再部署最终API。真实群动作一直未执行，没有把失败候选描述为通过。

前向迁移`024_native_test_tools.sql`扩展已有密封操作类型及test-window引用，
SHA-256为`bb4bac226ed897284260ba13b462d7a16624aa0e707ddd62d65b6b7e63b11551`。
001–023未改、RLS及租户权限保持。线上现为24项迁移；未重置`wsl`，
未恢复旧业务快照。冻结唯一API写入者后的一致dump及配置副本存于仓库外0700目录、
0600文件，记录哈希、一天到期及恢复前执行保留策略；内部临时恢复库指纹、幂等
迁移演练及强制RLS通过，没有启动第二个原生运行时。

| 最终本地交付 | 实际结果 |
| --- | --- |
| Compose | 原`wsl-platform`、`compose.yaml + compose.local.yaml`，保留release模式及原缩容限制，没有新增部署入口 |
| API | 容器`6216a4e4b23a`，镜像`sha256:8d3ec5595944dc41322d0b6583ef71e6fbbc9370133e4e2bc829da8e6698f33f` |
| Web | 最终容器`d95134e0b058`，镜像`sha256:12ce27b24b685a3003bdb15c3b01be74a7d32f9b64b47b54ee48a51e1861866d`，新JS/CSS已服务，Nginx配置检查通过 |
| 最终Web微调 | 独立重建并smoke后仅重建Web；API容器ID、PostgreSQL、heatbot及Simon完全未变，没有再次中断原生连接 |
| 运行限制 | API 1CPU/512MiB，Web 0.25CPU/128MiB，均healthy、非root、只读、RestartCount0；其他服务容器／挂载／配额不变 |
| 数据与配置 | 3个原账号、原身份／epoch／版本／启停、1个启用接收器及授权绑定、套餐、台账、人设和IM配置保持；`.env`及MASTER_KEY未改，既有接收的续票／租约／游标可正常变化 |
| 回滚镜像 | 保留上一版API`16b327d064b7`和Web`8376890ac107`，标记`rollback-20261029-pretools`；核实无容器引用后无force清理6个更旧／中间项目镜像及2个候选别名 |
| 镜像边界 | API/Web只保留当前与紧邻前一批稳定版；heatbot-runtime、开发／测试及其他项目镜像未动，没有全局prune |
| 门槛 | healthz正常，`native_ready`、`ai_ready`仍false；未自动登录、发短信、真实群禁言／踢人／公告／邀请操作，没有模型调用、提交、推送或远端CI |

候选仍使用既有锁定vendor、构建层和运行层在仓库外离线构建，并对照产品源码；
没有把该结果描述为标准Dockerfile联网干净重建通过。最终私有部署及恢复记录在
`~/.cache/artwslbot/deployments/20261029-native-tools-final.n6a4hb/`，
初次被替代部署及回退证据在`20261029-native-tools.jQca3z/`。临时库和网络已清理。

本批仅把上述原语和受控验证链路接入，能力目录保持部分接入／待真实验收。
原规则自动处置、定时与排行到执行器的闭环尚未因此开放；媒体发送、
持久撤回、私聊／辣椒聊发送、文本nonce历史安全核验和真实隔离群验收仍未完成。
没有真实账号动作、生产容量或长稳结果，不把已保存配置／本地健康检查算作原生验收。
<a id="本人接受入群邀请补齐"></a>

## 2026-11-01 本人接受入群邀请补齐

本轮目标是补齐邀请接收方的原生确认，并按所有者指令让自有账号“大蛤”加入唯一
“测试群”。以下自动检查均使用合成服务或隔离数据库，不是原生入群证据；
实际提交、双侧核验与恢复结果另记。

| 检查 | 当前实测 |
| --- | --- |
| 候选 Go | 全产品 `cmd/... internal/...` race/sodium、vet 通过 |
| 候选隔离 PostgreSQL | 串行 HTTP 186.568 秒、gateway 29.504 秒通过；只重建 `wsl_test` |
| 实际冻结补丁 Go | 全 `./...` race/sodium、vet 通过 |
| 实际冻结补丁 PostgreSQL | 串行 HTTP 223.469 秒、gateway 22.026 秒通过 |
| 前端 | 产品类型检查及构建通过，23.86 秒；入退群/受控工具/好友三组浏览器回归通过，每组四种视口，无原生请求 |
| 新能力 | 超过 JS 精确范围的申请 ID、固定路由与最终栅栏、本人/群/待处理记录隔离、会话变更、撤权、一次预算、未知不重放、关闭窗口后只读核对均覆盖 |
| 架构 | 提取/check、守卫、45 项 Python、3 项渲染缓存测试及七图渲染检查通过；系统 Python 缺 yaml 的首轮守卫未运行，改用既有架构 venv 后通过 |
| 模型咨询 | 可信增量通过，227 文件/9 组件，1 次请求，23,164 reported tokens，持久检查点已保存；源码 bb6fb4d743fe…，无全量回退 |
| 最终镜像 | API 0c25e843cf76…、Web 4fa0f908948b…；空库容器 smoke 通过，非 root/只读、前端资源、迁移及隔离门槛通过 |

API 从当前运行 4c3d78849732… 的完整冻结源码制作补丁，仅 13 项 Go/测试/037
差异，2493 文件摘要已核对；未加入未达标的解码候选。既有迁移未修改，037
只给密封操作台账的 kind 约束增加 `group_accept`，旧代码允许的值全部保留。
前端未变更依赖。交付预检五服务健康、36 迁移，未决原生/设置/上传/注册提交为零；
10 条隔离记录及密文摘要保留。真实执行不能凭镜像健康或邀请回执判定已入群。

### 本地交付与真实受控结果

02:01原Compose仅替换API/Web，保留原资源限制、环境摘要、网络和挂载，其他
PostgreSQL/heatbot/Simon容器未变。首次交付脚本漏显式迁移，检查发现仍36项后
停止验收；未提交原生动作。保存0600私有自定义格式数据库备份
SHA-256 c1e4b9ff234078596c41e13a672d7bed58e900340cdc3f85ce26710820c46e19，
通过既有Compose migrate应用037后，复核37迁移、未决0、10条隔离编号及密文摘要
保持、11项前端资源与构建摘要一致。脚本补上显式迁移。紧邻回滚标签
wsl-api:rollback-20261101-pre-self-invitation /
wsl-web:rollback-20261101-pre-self-invitation保留4c3d…/24ec…。
备份146,166,754字节，权限0600，全档案通过pg_restore解压至/dev/null校验，
未执行备份SQL。最终API容器a7a91dda24c1…、Web容器daea651325ae…，
均healthy/RestartCount0。

02:04正式原生只读接口确认群名唯一、林夕仍群主、大蛤目录完整且不在群，
唯一一条本人待处理邀请来自林夕。按所有者明确请求，正式API建立仅本人接受的
300秒、预算1窗口4933eca8-852d-4348-ab6c-6cd77a6b9520，
启用既有非商业零AI额度测试套餐及大蛤动作开关，不开放群管理/AI。
02:05:48唯一一次group_accept提交，密封操作
0d931ed5-734b-4e35-a865-0158c915f8a0为succeeded /
native_invitation_accept_acknowledged，无自动重试或重复发送邀请。

02:05:49大蛤和林夕分别现读完整群目录及完整成员列表，均包含两自有UID：
大蛤23691273为member，林夕31846829为owner。02:07既有连接所有者角色观测
再次核实并记录member，不创建租户所有权或借助SQL制造权限。
02:07:54取消窗口，used1/budget1；大蛤connection_enabled恢复false，原testing
开关true保持，订阅恢复原plan/disabled，群管理/AI均false。
旧过期到期字段因现有订阅接口仅允许未来时间而**未精确恢复**，保留本轮20分钟
期限作为disabled元数据；没有商业订阅、AI额度、财务充值或付费模型调用。

02:10收尾读回仍为双方确认在群；林夕接收revision24、360消息/360ACK，大蛤
revision9、3消息/3ACK，两者connected、observed、队列0、当前连接新增隔离0、
未决设置0，心跳新鲜。native_ready/ai_ready保持false。此短时结果不证明全天稳定，
未补做禁言、踢人、好友审批或其他群操作。证据在本机私有
~/.cache/artwslbot/deployments/20261101-self-invitation/；未运行远端CI、提交或推送。

## 2026-11-02 辣椒聊账号资料与设置补充

本轮按固定桌面1.0.3独立契约补充本人性别、生日、80字符签名、地区名称只读、
四个消息通知开关及脱敏手机。改密使用purpose=2短信及验证码/新密码MD5提交；
换绑使用purpose=6旧号验证及场景7新号发码，不套用旺商聊旧密码/证书步骤。
敏感输入仅在单次请求中使用，流程只密封手机/区号，终态清空；
未知结果不重发，成功改密/换绑停用本站旧会话，要求手动重新登录。

- 候选基于当前运行的`20261102-lajiaoliao-catalog-final`冻结源码，仅选本轮文件。
  排除未验收解码候选、039及其二级安全码/实名认证实现，保留原前端无关改动。
  新增前向040仅扩充四类设置操作，既有约束、数据和授权保持兼容。
- Go race/sodium（cmd/internal）与Go vet通过；功能表生成同步测试通过。
- 独立PostgreSQL HTTP全套通过（首轮90.748秒，增加完整换绑回归后89.933秒）；
  网关全套18.194秒通过。
  新增合成测试覆盖本平台改密、换绑状态、单项资料/通知、幂等、防跳步、
  密封目标和敏感内容过滤；测试数据库仅`wsl_test`。
- 产品类型检查和Vite构建通过；七组浏览器合成回归通过，覆盖320/390/647/1440宽度，
  原生昵称/资料单项提交、人机场景隔离、取消发码零提交、密码表单差异、
  结果未知及迟到响应隔离。截图位于私有候选`ui/output/playwright/chili-settings-*.png`。
- 架构事实更新/校验、边界检查、50项工具测试、3项渲染缓存测试、7幅Mermaid通过。
  兼容可信基线的CodeBoarding增量分析通过，独立于生产验收；未使用全量初始化。
- API/Web候选容器smoke通过，临时数据库、非root只读运行、资源及租户隔离通过；
  `native_ready/ai_ready`保持false。未执行真实短信、注册、登录、改密或换绑。
- 头像上传和地区目录编辑尚未接入，实名认证及二级安全码仍暂缓；
  当前不能宣称辣椒聊全部资料功能或真实账号安全修改已经验收。

2026-11-02 02:55（Asia/Shanghai）已同步现有本地Compose，只更新API/Web并应用040。
API镜像`663434e0a6b4…`，Web`329dcaf0f3a2…`；39项迁移（038后直接040），039未部署。
五个服务运行正常，API/Web健康，两项公开页面资源与冻结构建逐字节一致；
未决操作0、原有10条隔离记录的密封摘要保留，资源限制、环境及凭据配置不变。
两个旺商聊与一个辣椒聊接收器心跳新鲜且connected；另一个辣椒聊账号保持
更新前已有的connection_enabled=false和无有效租约，不自动恢复。
回滚镜像保留为`wsl-api:rollback-20261102-pre-chili-settings`及对应Web标签，
040仅放宽设置类型，旧镜像仍兼容。交付证据位于本机私有
`~/.cache/artwslbot/deployments/20261102-chili-settings/`。
本轮为本地检查与交付，未运行远端CI、未提交或推送。

## 2026-11-03 本站原生上传受控验收

从实际运行v24冻结源码单独补入WSL仅上传授权和现有上传适配器，
未混入工作区轻量化、解码或辣椒聊媒体候选。仅API更新为`29c84d47bb2e…`；
Web/数据库/热群引擎未重启，无迁移，配置和资源限制保持，上一API镜像留作回滚。

Go race/sodium、串行隔离`wsl_test` HTTP race（109.066秒）、产品类型检查及构建、
架构/仓库守卫、52项架构工具测试、3项渲染缓存及7幅Mermaid、容器smoke通过。
匹配可信基线的DeepSeek增量分析通过（1请求、27601报告token、未知用量0）。
首轮过期样本、旧smoke预期及临时库断连失败记录保留，不计为通过。

2026-11-03 09:13–09:16 UTC，“测试群”林夕账号依次提交PNG、1秒PCM WAV、
1秒带封面MP4各一次：图片和视频上传成功、持久回执检查一致；语音被上游明确
拒绝，安全码为`native_upload_rejected`，没有重传。首窗口used2取消后，
仅将原预算剩余1次用于视频，总消耗3次；两个窗口均取消，账号动作开关恢复false。
未启用订阅、群管/AI业务模块，也未产生群内发送或撤回。

上传授权不能暂存或发送消息；这不构成发送/撤回通过，功能表和readiness不提高。
Nonce旧发送器/备份恢复历史及独立受控撤回入口仍阻塞发送链路验收。
详见[本站上传验收记录](native-outbound-acceptance-20261103.md)。未运行远端CI、提交或推送。

<a id="旺商聊本站发送与撤回验收2026-11-03"></a>

## 2026-11-03 旺商聊本站发送与撤回验收

独立候选从实际运行v25/source冻结，仅加入本站发送、受控撤回、
随机起点事务nonce、MP3/MIME及安全上传诊断修复；未混入未通过的解析、
轻量化或辣椒聊媒体候选。无迁移，CodecVersion=7不变。
旧发送器使用已确认，数据库恢复未确认；不宣称完整旧nonce历史已核清。

行为修复v27完成全产品race/sodium、串行隔离wsl_test HTTP race
（104.530秒）、功能目录同步、架构及仓库守卫、容器smoke；
该次gateway环境开关未设置、数据库用例被跳过，完整gateway集成在最终目录候选
明确启用独立开关后串行重跑，不将先前包通过误报为数据库通过。
不变前端复用已通过产品类型检查/构建的v26镜像。
可信DeepSeek增量通过，源码9f9ae23b79f6…，1请求、26036报告token、
未知用量0。架构工具52项、渲染缓存3项及七图检查在本轮v26通过，
后续仅证据行号变化，不重画未变化布局。首轮分析超时及临时库挂载失败
单列保留，均不冒充通过。

10:19–10:32 UTC 在唯一隔离“测试群”实际执行：
普通成员群文本和@、群主引用回复、图片、短MP3语音、带封面短MP4，
共五条消息；三次上传；群主对同轮成员文本撤回一次。
全部最终持久成功，另一账号逐项解码且发送者和群归属一致。
引用client标识、提及成员和UTF-16位置独立核对；双方撤回通知最终可读。
PNG原图、MP4及JPEG封面再次受限读取均200/no-store。

首轮普通成员发送在原生写入前被旧网关角色校验拒绝，消耗一次预算，
修复并补回归后交付v27。另一次上传准备因连接核验中断而未进入提交栅栏，
持久used=0、没有上游上传；保留失败后只用原剩余额度。
撤回回执先于通知，脚本立即查询未命中后停止；后续仅只读核对原通知，
没有重复撤回。累计持久预算10次，所有窗口取消、动作开关恢复false，
两个接收器仍connected、队列0，业务模块及AI均未启用。

网站真实执行证据独立加入功能表六个父项和语音/视频两个原子项；
历史受控父项由28增至34，其中原生底座33项、账号人工操作1项。
辣椒聊保持自己的4项证据，不继承本轮结果。生产集成状态仍partial，
native_ready/ai_ready=false；PCM WAV、私信、移动端播放、异常恢复、
旧nonce完整历史/备份恢复及生产准入未因此通过。
详见[本站群发送与撤回记录](native-controlled-sends-20261103.md)。
最终目录候选v28全产品race/sodium及目录同步通过；
隔离HTTP race 104.285秒、gateway race 20.065秒通过，
均仅使用隔离wsl_test并按序执行；gateway独立复跑前由单个HTTP用例初始化迁移。
旧普通成员拒绝断言已更新为允许当前普通成员、拒绝未知角色，
控制层生产及套餐拒绝测试保持。第一次独立gateway缺少预初始化schema、
第一次工具测试缺少本地锁定依赖的失败记录保留，修复测试环境后重跑通过。
最终52项架构工具、3项渲染缓存、7图、守卫和容器smoke通过；
匹配可信基线的增量分析通过，源码9de1e5f2b35f…、1请求、
27426报告token、未知用量0。源码运行逻辑与已验收v27相同，
v28仅更新功能目录、证据和测试。
最终现有Compose仅更新API为
`sha256:53e4189219c8564f6da9aa67496eb52bd48bd8ef2f8824672c11b05ef28417f8`，
Web仍为`4f8e8ae16034…`，数据库/热群引擎/Web容器未替换；
环境摘要、挂载、网络及资源限制保持，紧邻v27镜像保留供回滚，无迁移。
1440与390宽度真实页面回归通过：本轮六个父项与语音/视频子项均绿色，
移动端样本及账号人工入口仍分开，私信没有继承通过；页面无横向溢出、
脚本错误0、原生写入0。首次脚本相对定位错误导致超时，修正定位后重跑通过。
线上API目录底座受控33/35，独立指定场景37/39，完整验收仍0；
这些统计不改变原生/AI readiness和生产准入。
四个双平台接收器均connected且观测新鲜，动作准入false，活动窗口0。
私有交付/截图证据位于`~/.cache/artwslbot/assistant/20261103-controlled-send-v28/`。
本轮未发短信、新登录、启用商业业务、执行辣椒聊动作或运行远端CI。

## 旺商聊私聊准入与故障回归（2026-11-03）

以实际运行v28 API `53e4189219c8…` 对应冻结源码为基线，单独补齐
私聊 `native_debug` 创建及执行白名单。只纳入两个动作、独立上传约束、
对应回归和目录说明；未引入未达标的解析、轻量化或辣椒聊媒体候选。
消息密封、CodecVersion=7、生产准入和数据库迁移保持不变。

| 本轮实际检查 | 结果 |
| --- | --- |
| 全产品Go race/sodium | 33.835秒，516个测试通过，62个按其环境条件跳过 |
| 隔离PostgreSQL HTTP race/sodium | 108.088秒，241个测试通过；负载专用TestReplayLoad未运行 |
| 隔离PostgreSQL gateway race/sodium | 在HTTP之后串行执行，19.113秒，126个测试通过 |
| 私聊权限/恢复 | 套餐及native_debug分别覆盖文本、图片、目标/会话撤销、绑定变化、取消、错误回显、重启与未知结果不重发 |
| 最终提交栅栏 | 账号/绑定epoch、撤权、角色、租约、会话及窗口边界合成回归通过 |
| 产品前端 | 对冻结前端重新执行包含vue-tsc的Docker构建，2.651秒通过；不替换Web |
| 架构与仓库 | 当前工作区及冻结候选检查通过；52项工具测试、3项渲染缓存检查、7图均通过 |
| CodeBoarding | 匹配可信基线的增量通过；源码6dfa0ca15ace…，1请求、26826报告token、未知用量0；未完整初始化 |
| API镜像与容器smoke | 新API构建16.666秒；隔离容器smoke23.859秒通过 |

合成回归没有连接真实原生上游或操作真实账号；测试数据库仅为一次性
网络内的wsl_test，HTTP与网关按序执行，结束后移除本轮临时容器和网络。
首次测试容器缺少只读挂载目标，尚未启动Go测试即失败，补齐合成环境挂载
占位后重跑通过。架构命令首次遗漏既有-mod=readonly设置而失败；
另一次候选位于忽略目录被拒绝，改用既有匹配的可信分析工作区后通过，
未绕过源码候选守卫或重建不匹配基线。

本轮真实私聊首次准备检查：两个自有账号接收在线、队列为零，未决操作
为零；但没有林夕与大蛤之间的留存私聊。再次预检查仍为
`owned_private_conversation_missing`，原生变更0、测试窗口0，
没有向其他联系人发送或伪造会话。等待手机端这两个账号之间的一条
私聊样本，之后最多三次变更：文本、图片上传、图片发送。
目前不增加私聊通过记录。

撤权、断线及未知结果恢复本轮已完成代码核对和合成回归；尚未实施真实
上游故障注入。普通业务提交仍明确返回`native_compatibility_not_verified`，
自动关键词回复/拦截等事件消费到原生执行闭环不能由受控群发送代替。
WAV、好友审批、语音播放入口和文件下载按所有者要求不再补测，
保留原状态，不作为本轮阻塞。native_ready/ai_ready及业务准入均不提高。
详情见[native-private-acceptance-20261103.md](native-private-acceptance-20261103.md)。

验证通过后，现有Compose仅更新API为v29：
`sha256:514975042b3397749767802dfb785aeb35f02bd30d1d5d9df0ac21a513bbb230`。
紧邻v28镜像保留为`wsl-api:rollback-before-private-v29-20261103`。
Web保持`4f8e8ae16034…`，Web、PostgreSQL及热群引擎容器未替换；
Compose文件摘要、环境摘要、资源限制、挂载及网络保持，无数据库迁移。
部署前各持久操作及助手队列未决数为零。部署后API健康，
2026-11-03 11:47 UTC线上核对两个旺商聊接收器均connected、队列0，
动作准入false、活动窗口0；目录底座受控33/35保持，私聊两项仍待真实验收。

## 旺商聊本站私聊发送验收（2026-11-03）

所有者确认已从手机端发送双账号私聊样本后，重新只读核对自有账号身份、
接收在线、唯一测试群绑定和留存私聊路由。本站由林夕向大蛤执行一次
私聊文本、一次380字节PNG上传、一次私聊图片发送；没有向第三方发送。
执行前经当前连接核验群主、目标普通成员及最新好友关系，使用单个
持久native_debug窗口、300秒、三次总预算，群管业务与AI未启用。

2026-11-03 12:02:05 UTC本轮完成：文本和图片持久回执均为
`im_server_accepted`，上传为`native_upload_received`。大蛤端分别
实际接收并解码为私聊文本、私聊图片；发送者、收件人、session及client
标识精确匹配。三次预算已用完，失败0、自动重发0；窗口取消且临时
动作开关恢复原值。私有操作与投影核对记录保存在
`var/private-acceptance-20261103/native-results.json`，不包含原生凭据。

两项本站执行证据独立录入旺商聊功能表；不继承辣椒聊或群图片结果，
不宣称真实故障、其他格式、旧nonce完整历史、生产业务闭环通过。
WAV、好友审批、语音播放入口及文件下载按所有者要求不再补测。

以运行v29的冻结源码单独形成目录候选v30，不修改发送运行逻辑。
全产品race/sodium 29.582秒、516项通过、62项按条件跳过；
隔离wsl_test的HTTP race 104.119秒、241项通过、负载专用项跳过，
随后串行gateway race 18.861秒、126项通过。首次目录回归因旧测试仍
要求私聊无记录而失败，改为检查私聊独立证据且不能继承移动端/群聊
后重跑通过；初次失败日志保留，未重复任何真实发送。

产品前端类型检查及构建3.074秒通过，API构建16.643秒、
容器smoke 24.601秒通过；52项架构工具测试、3项渲染缓存测试、
7图及架构/仓库守卫通过。匹配可信基线的CodeBoarding增量通过，
源码55c2750a1c65…、1请求、25074报告token、未知用量0；
可信检查点已保存，没有完整初始化或业务AI配置变化。

v30已同步现有Compose，仅替换API为
`sha256:83ff7a278c00aa5d480edb6db5d3e2c7162698e512f78d55662d74eeaf940f59`，
健康检查通过。Web仍为`4f8e8ae16034…`，其他服务容器未替换，配置、
环境摘要、挂载、网络和资源限制核对一致，无迁移；紧邻v29保留为
`wsl-api:rollback-before-private-v30-20261103`。部署前未决提交为零。

2026-11-03 20:14北京时间线上只读核对：两个旺商聊接收器connected、
队列0，动作开关false、活动窗口0，底座受控35/35。
随后真实页面刷新核对两项私聊为绿色“指定场景可用/已核验”，独立场景
39/39、完整验收0；没有由目录更新提高native_ready/ai_ready。
页面截图保存在`var/private-acceptance-v30-20261103/private-features.jpg`。
本轮未进行其他真实操作或执行远端CI。

## 辣椒聊解包补齐与真实发送撤回验收（2026-11-03）

固定 Android APK 1.7.3/versionCode20 与桌面1.0.3做本地解包、静态核对；
安装包摘要及消息字段证据见 `lajiaoliao-client-completion.md`。
没有运行新客户端、注册、发短信、原生登录或向架构模型传递安装包及真实消息。
基线为当前v30镜像及对应完整冻结源码，排除未验收的解码、轻量化、
辣椒聊上传发送候选。运行CodecVersion7、数据库迁移及四worker不变。

本轮补图片、视频、联系人名片、头像和有界嵌套媒体的按需读取；资源
绑定账号/会话/原始事件，读取后复核权限及版本；复用两个媒体槽，
不进入接收、提交、ACK，不后台下载或转码。群文本与撤回复用密封
native_setting_operations及现有连接，client标识使用持久操作编号。
受控面板增加群文本、近期撤回候选和群名编辑，助手生产准入不变。

v31全量race/sodium 33.176秒、519项通过、64项按环境跳过；
串行隔离wsl_test HTTP race 110.639秒、243项通过、1项负载测试跳过，
随后gateway race 19.056秒、126项通过。新增资源作用域/过期/迟到epoch、
未知结果冻结、重复提交不重发、撤权/租约版本最终栅栏通过。
首轮HTTP旧测试仍把Chili文本视为不支持，期望400；现在正确拒绝
借用WSL群授权并返回409。修正契约断言后重跑，首轮日志保留。

前端含vue-tsc构建32.316秒，API构建21.110秒；
容器smoke25.978秒通过。架构/仓库守卫、52项工具测试、3项渲染缓存、
7图通过；匹配可信基线CodeBoarding增量69.821秒通过，源码摘要
cd2cb21feb70…，可信检查点保存，没有完整初始化。
v31仅更新现有Compose的API/Web；API为
`sha256:d8821d810232bee951bf8537deba97bb18a8f86d45b99f359296ae95ed5790f8`，
Web为`sha256:f0afd64514c94eafcd2ce7feeae8274513a5d6c34b646cc58d9091da5d6dfe6a`。
配置、资源限制、挂载、网络一致，PostgreSQL和热群引擎未重建，零迁移，
紧邻v30镜像及Web均保留。发布前持久未决操作为零。

2026-11-03 14:30 UTC线上只读核对自有账号群/私聊共11条旧演示：
7条有结构化视图，嵌套遍历含图片5、引用3、合并记录1、提及3；
API读到既有群撤回1条，解析错误0。实际JPEG357409字节、
发送者头像PNG4045字节均200/no-store；页面图片解码、
群引用、提及和两条合并记录展开可用。既有引用里的64×56旧图片另来自
官方im-api域名，补固定图片来源后单独核对，不扩大视频来源。

2026-11-03 14:31:22–14:31:44 UTC，由已认证群主在唯一测试群创建
一个native_debug窗口，300秒、总预算2，目标仅本人。
本站群文本持久成功，另一个自有普通成员实际接收解码；
发送者、群及client标识精确匹配。随后仅撤回该条本人文本一次，
持久成功且对端收到2101通知，原消息标识及操作者精确匹配。
预算2/2、失败0、自动重发0；窗口取消，testing_enabled及
connection_enabled恢复false，已有接收恢复connected。
私有日志在`var/chili-completion-20261103/release/live-text-recall.json`。

真实页面撤回弹窗显示既有及本轮两条通知；原消息历史未删除。
本轮实际通过证据独立录入辣椒聊目录，不借用旺商聊结果。
群名编辑只补接线，尚未做真实改名。视频及联系人名片暂无留存样本，
只完成固定包与合成回归，不能记为真实通过；引用/@发送、媒体上传发送、
私聊发送等仍保留各自未接入或未验收状态。native_ready/ai_ready保持false，
未执行远端CI或提升生产业务准入。

v32目录与资源修正回归：race/sodium 38.404秒、520项通过、64项按条件跳过；
串行隔离wsl_test HTTP race 120.669秒、243项通过、1项负载专用项跳过，
gateway race 19.539秒、126项通过。前端含vue-tsc构建33.186秒，
API构建16.060秒、容器smoke24.476秒通过。架构/仓库守卫、52项工具测试、
3项渲染缓存及7图通过。匹配可信基线的CodeBoarding增量58.597秒通过，
源码0838b6847920…，1请求、报告32484 token、未知用量0，可信检查点保存；
没有完整初始化、替换模型基线或修改业务AI配置。

v32首次部署核对将PostgreSQL挂载返回顺序变化误判为配置变化并自动恢复v31；
原PostgreSQL容器ID未变，两个挂载反序后的摘要与发布前完全一致。
保留失败记录，将挂载按目标路径排序后重新核对。14:59:47 UTC交付成功：
API为`sha256:9a788c19a9bd6318960c4e9c7b10edc9223928d80115dd4cde07a0ab265de4ea`，
Web为`sha256:8acb75615b307e6107b498a68fe8c815aca4d64c0f71a9f1ca5d22c49ab441ac`；
环境摘要、资源、挂载与网络一致，零迁移，上一版镜像保留。

v33只修正作用域弹窗的多行勾选项和窄屏溢出，并采用工作区已有的容器
端口就绪读取方式。Go、产品TS/Vue与v32逐文件一致，复用其后端测试和
匹配源码的模型增量证据；不把另一候选或旧模型成功记录用于本轮。
前端重建37.807秒、容器smoke25.238秒及架构/仓库守卫、52项工具测试、
3项渲染缓存和7图通过。
前三次smoke失败均保留：隔离目录复制没有保留文件权限，候选Nginx配置
成为0600，非root服务无法读取。已修正复制方式并恢复基线0644后重建；
这些失败镜像没有部署，端口等待修正没有跳过健康及业务断言。
15:16:14 UTC仅更新Web为
`sha256:514cfde535c661097f00138072063ec1c0072a4839c72c69bcd839666488a842`；
API、数据库和热群引擎容器未重建，配置与资源一致，紧邻Web保留供回滚。

线上只读核对：辣椒聊受控记录由4项增至14项，页面指定场景可用14、
完整验收0；旺商聊35项底座通过记录保持不变。四个自有接收器connected、
队列0，双平台活动窗口0、动作准入false；辣椒聊临时测试开关恢复false，
旺商聊原测试开关保留原值。没有额外原生提交或自动重发。
320/390/768/1440实际CSS视口中弹窗正文无横向溢出、保存/取消可见；
未提交的弹窗已取消，浏览器尺寸恢复。页面核对新增发送、撤回和读取
状态均为绿色“指定场景可用/已核验”。截图及数值位于
`var/chili-dialog-v33-20261103`，交付及检查位于各自release目录。

旧引用中的64×56图片从官方固定im-api域读取仍返回media_unavailable，
没有证据判断为过期，也未计入资源通过。下一轮真实读取只缺当前样本中的
短视频及本人联系人名片；引用、提及和合并记录无需重复演示。
群名变更、本站引用/@与媒体及私聊发送仍分别保留未接入/未验收状态，
本轮不将移动端演示替代本站执行，不改变生产业务准入和readiness。

## 辣椒聊基础资料与新增媒体验收（2026-11-04）

本轮从运行API v32、Web v33冻结源码开始，仅补基础资料弹窗、头像链路及
新样本读取证据；不纳入未通过的解析、接收轻量化和群媒体发送候选。
基本资料改用单个本人身份查询，取消完整资料、隐私与通知读取的前置依赖。
昵称沿用原单项持久保存；头像增加固定OpenIM单分片上传及独立最终设置栅栏，
沿用账号版本、身份、幂等及未知结果核对，不新增迁移或后台任务。
界面只提供昵称与头像，不重新加入生日、性别、签名、通知等个人设置。

真实只读验收于2026-11-03 16:02:58 UTC完成：所有者新发的视频生成本站
作用域资源，MP4 2642719字节、封面JPEG 44038字节；浏览器readyState=4、
720×1280、10.054195秒并完整播放结束，无媒体错误。本人名片按字符串身份
及昵称展示，头像PNG 4045字节、402×402实际解码成功。辣椒聊新增
read_video/read_contact_cards各自的指定场景证据，不等同于本站发送通过。
私有证据位于`var/chili-profile-v34-20261103/release/live-read.json`、
`media-browser.json`及`new-media.png`。

真实昵称/头像修改尚未计为通过；生产准入与readiness保持原状态。

v34的526项race/sodium、隔离HTTP244项及gateway126项、前端构建、
52项架构单测、仓库守卫、Mermaid、匹配基线增量分析与容器smoke均通过。
但部署后本人资料仍返回profile_shape，线上验收失败记录保留，未计为通过。
定位固定SDK `internal/user/server_api.go`及`GetDesignateUsersResp`：
`/user/get_users_info`字段是usersInfo；原解析和合成样本误写users。
v35修正基础与历史资料读取字段，新增错误字段、错误身份、多条资料拒绝用例；
不修改`/user/find/full`的另一份users契约。v35重新完成发布检查与真实回读，
结果如下。

v35冻结发布源码的race/sodium 34.869秒、527项通过、64项条件跳过；
串行隔离wsl_test HTTP race 125.578秒、244项通过、1项负载专用项跳过，
gateway race 20.218秒、126项通过。产品前端类型检查/构建通过，Web与v34
逐字节一致，构建缓存验证3.633秒；API构建24.857秒、容器smoke25.219秒。
架构与仓库守卫通过，52项架构单测12.645秒、3项渲染缓存检查及7图通过。
匹配可信基线的CodeBoarding增量32.375秒通过，源码
`6bb609a411e093e9e2b1e034feab8c0de77d31b6075a7a1641093168340214bd`，
1请求、报告26132 token、未知用量0、可信检查点已保存。
此为咨询分析结果，没有远端CI执行或生产准入结论。

2026-11-04 00:27:25 Asia/Shanghai（2026-11-03 16:27:25 UTC）仅更新API为
`sha256:38edf5a8c8e3d85488a657d5bac2bbea9221ab1a6a73c96303e0c3a27a0f4f60`；
Web保持本轮v34的
`sha256:2491e861acd48458894dff4494888c25341cde6e74c13578d8140271d34bca20`。
Web、PostgreSQL及热群引擎容器未再次重建。环境摘要、资源限制、挂载、
网络和既有凭据保持一致，零迁移；紧邻API/Web镜像保留供回滚。
无未决提交后才重启，未混入解析、轻量化或群媒体发送候选。

部署后两个辣椒聊账号本人资料回读通过，身份与已认证账号精确匹配；
返回昵称、受控头像引用及头像编辑能力，不返回原生资源URL、隐私或
通知设置。页面读取当前昵称，402×402头像实际解码；昵称编辑后保存按钮
启用，恢复草稿后禁用，全程没有保存昵称、上传头像或原生登录。
320/390/768/1440实际CSS视口下，昵称和头像页均无横向溢出，当前头像
加载完成，选择文件、保存、刷新及关闭操作可见；视口已恢复。
刷新后实时订阅及接收正常。线上四个自有接收器connected、队列0，
活动受控窗口0；辣椒聊测试开关false，动作准入均false。
辣椒聊指定场景记录16项；旺商聊原35项底座通过记录未变。

私有检查与部署证据位于`var/chili-profile-v35-20261104/release`：
`live-check.json`、`ui-check.json`、`profile-ready.png`及独立检查日志。
v34线上回读失败截图继续保留，不以合成通过覆盖真实失败。
头像新图上传/设置、昵称真实改动及异常恢复尚未执行，不能标为真实通过；
本轮只完成相应实现、合成回归和实际只读/界面验收。

## 辣椒聊头像与群管实操验收（2026-11-04）

所有者明确要求上传头像，并继续核对本站发送及群管能力。头像先用
受控测试号当前PNG同图上传一次，没有另选他人图片、改昵称或发起登录。
原操作持久记录为failed、lajiaoliao_avatar_origin_unverified，未记为通过，
也没有自动重试。原实现仅接纳静态资源域；补充已在固定IM协议及资源读取
路径中的im-api来源，仍限定两个HTTPS固定域、无重定向及凭据外送。
后续实操须另记结果，不能用合成测试覆盖本次失败。

01:06:10至01:06:17 Asia/Shanghai，群主在唯一辣椒聊测试群对另一个
已认证自有普通成员执行一分钟禁言，再手动解禁。300秒native_debug
窗口总预算2，两个持久操作均succeeded；每次独立回读mutes_known=true，
目标先在muted_ids中、再从名单移出。本人及目标角色均未改变。
两次预算用尽后取消窗口，testing_enabled与connection_enabled恢复false。
没有批量操作、自动重发或原生登录。名单读取、禁言和解禁各自记录平台
专属指定场景证据，不等同生产准入、自动到期或故障恢复验收。

私有原始检查与失败操作号位于
`var/chili-outbound-v36-20261104/release/avatar-result.json`和
`moderation-result.json`；本轮后续实现及发布检查结果待补录。

v36上述群管实现完成528项race/sodium、隔离HTTP244项、gateway126项、
前端构建、52项架构单测及守卫、Mermaid、匹配基线增量分析和容器smoke；
本轮真实禁言及解禁结果才计为指定场景通过，未提高生产准入。

## 辣椒聊本站头像与八种发送验收（2026-11-04）

实施范围以紧邻运行版本冻结源码为父版本，仅纳入辣椒聊发送、头像、
相关读取修复及验收目录。没有打包工作区中的旧解码算法、轻量化或041
媒体候选。保持CodecVersion=6、现有连接归属、资源限制及生产业务准入。
只使用所有者自有双账号和唯一隔离测试群；没有批量原生操作、自动重发、
注册短信或原生登录。以下时间均为Asia/Shanghai。

| 单项 | 实际结果 | 核验范围 |
| --- | --- | --- |
| 头像 | 03:16:04上传及最终设置成功 | 本人当前图片同图规范化重传；新JPEG 3919字节，浏览器256×256，昵称不变 |
| 群文本 | 03:17:54服务器接受 | 对端101、持久client标识、发送者及本群精确匹配 |
| 引用回复 | 03:18:25服务器接受 | 对端114及结构化引用与本群选中原文一致，发送消息client标识匹配 |
| @成员 | 03:18:53服务器接受 | 对端106包含另一个自有账号的字符串身份 |
| 群图片 | 03:26:42上传与发送分别成功 | 对端102及本站作用域图片资源均核验 |
| 群语音 | 03:27:07上传与发送分别成功 | MP3，对端103、client标识及本群核验；未宣称播放或时长展示通过 |
| 群视频 | 03:27:36上传与发送分别成功 | MP4及JPEG封面，对端104、两份资源均读取成功 |
| 私聊文本 | 03:51:26服务器接受 | 自有收件账号101、client标识、发送者及私聊归属匹配 |
| 私聊图片 | 03:52:15上传与发送分别成功 | 自有收件账号102、client标识及本站图片资源读取匹配 |

群文本轮窗口预算4，实际发送3次，未使用的私聊预算没有补发；
群媒体轮预算8，实际上传/发送共6次；独立私聊轮300秒预算3，文本一次、
图片上传及发送各一次。每轮均独立持久授权、逐项有界执行，随后取消窗口，
恢复testing_enabled/connection_enabled=false。原生群主及普通成员身份
没有改变。对端核对使用原操作的clientMsgID，不凭正文相似或移动端演示
判定本站成功。私有证据：
`var/chili-avatar-v39-20261104/release/send-*-result.json`、
`var/chili-resources-v40-20261104/release/send-peer-verification.json`及
`avatar-readback.json`、`avatar-ready.png`。均不含密钥、票据或完整原生资源URL。

本轮发现并修复的真实缺口：

- 固定对象上传线路与旧静态资源域不同，补入已核验的精确存储域；
  签名PUT不携带IM票据，不接受任意地址。
- 头像原两秒提交时限过短。现仅头像每阶段八秒；完成上传前不调用账号更新。
  v38旧未知操作经原actor、账号、密封请求及原上传UUID核对后安全结案为失败，
  没有原生重放；最终更新未知及进程中断不适用这项结案。
- 新资源的IM对象地址返回302到固定CDN。仅允许这一个已核验跳转，不转发
  Cookie、token或Authorization；第二跳、非对象视频地址等合成用例均拒绝。
- 图片私聊已有留存，但旧发送入口仅选decoded投影，导致找不到会话。
  现从明确的content_type占位及原始保留消息重新验证支持格式和对端；
  未知类型、错误隔离原因、跨账号及过期消息仍拒绝。
- 收件人原被误纳入执行账号选择，受非执行者动作开关影响。执行者集合只含
  本次群主；另一已认证自有目标单独记录epoch，不授予执行权。

失败及交付记录继续保留：早期头像来源拒绝、v38上传阶段超时未记为通过。
v37首次交付遗漏046迁移先回滚，随后通过现有Compose migrate执行前向046；
未修改或应用工作区041。v38冻结时目录权限受umask影响，非root Web验收失败，
修复为保持父版本文件及目录权限后才交付。两次CodeBoarding上游502未伪报成功，
其后匹配可信基线的增量运行通过。v40首次架构update缺少mod=readonly，
仅修正调用环境后通过，没有重写vendor或替换分析基线。

| 冻结版本 | race/sodium通过/跳过 | 隔离HTTP通过/跳过 | 隔离gateway通过 | 增量分析秒 | smoke秒 |
| --- | --- | --- | --- | --- | --- |
| v36 | 528 / 64 | 244 / 1 | 126 | 64.753 | 24.534 |
| v37 | 569 / 66 | 280 / 1 | 151 | 75.788 | 23.944 |
| v38 | 570 / 66 | 282 / 1 | 151 | 87.081 | 25.361 |
| v39 | 571 / 68 | 292 / 1 | 151 | 36.363 | 24.811 |
| v40 | 578 / 68 | 295 / 1 | 151 | 52.890 | 24.720 |

各轮均完成产品类型检查和构建、架构/仓库守卫、52项架构单测及Mermaid渲染；
HTTP和gateway在独立wsl_test中串行执行。跳过项为显式集成/负载专用条件，
不计为本轮通过。CodeBoarding为咨询性分析，本轮未执行远端CI。
v40仅更新API至
`sha256:476d6a7aba8749a8a2e9a7e046d71ce680d9e0ceef51db6c54d3f791487b717a`，
Web字节未变，无新迁移，部署配置摘要与资源限制未变，紧邻镜像保留。
运行前后服务健康、待处理未知提交为零才重启；原生连接按现有协调器恢复。

功能目录增加本平台引用、提及、群图片、群媒体、私聊文本和私聊图片的独立
指定场景证据；头像、语音和视频原子项分别记录，不继承其他子项或平台。
昵称未实际修改，不因头像成功获得通过记录。WAV、其他编码、移动端播放、
生产准入、撤权/断线/未知结果真实恢复和全天长稳不在本轮通过范围。
`native_ready`、`ai_ready`及普通业务/助手生产动作保持原状态。

最终v41目录交付再次完成race/sodium：578项通过、68项条件跳过，30.542秒；
串行隔离HTTP 295项通过、1项负载专用跳过，153.849秒，gateway 151项
通过，19.424秒。首次目录回归因旧断言“辣椒聊没有任何原子项通过记录”
失败；改为核对本平台独立证据、不继承WSL后重跑通过，首轮日志保留在
`before-independent-evidence-race-sodium.log`。产品类型检查及前端构建
30.688秒、API构建25.530秒、52项架构单测14.504秒，架构与仓库守卫、
3项渲染缓存测试和Mermaid均通过。匹配可信基线增量38.472秒通过，
容器smoke 25.308秒。工作区另运行目录同步测试及代码事实架构检查通过；
咨询性增量针对冻结发布源码，不拿工作区其他候选充当运行版证据。

2026-11-04 04:14:39 Asia/Shanghai更新现有Compose的API和Web：

- API：`sha256:8b3ac8d2d7a06662641529581b634a4fca070c0cd644f0ea5d42b04d2b2bdd66`
- Web：`sha256:6acf7a7d7fa4fc08837dc41d127c8f47a05113b685f5968b34cc76a11c4edba5`

零新增迁移，PostgreSQL和热群引擎没有重建；配置、资源、凭据及挂载摘要
保持一致，v40镜像保留供回滚。v41只补实际目录证据及账号工具栏换行，
未重新执行任何发送。界面原生表所列禁言、解禁、引用、@、群/私聊文本、
群/私聊图片及群语音/视频均显示绿色指定场景状态。原生底座当前26个独立
验收项有指定场景记录，25个受控能力组含原子项替换口径；完整验收仍为0。
头像位于账号与人工操作，不加入群管助手工具。

最终浏览器回归：320/390/768/1440头像弹窗均无页面横向溢出，当前图片均
完成256×256解码，保存和关闭控件可见。768下原工具栏溢出52像素已修复；
图片标签使用作用域请求后的blob，不把原生资源URL直接交给浏览器。
新发送的一秒MP4在网页完成160×96、readyState=4播放并结束，无媒体错误，
证据`var/chili-resources-v40-20261104/release/sent-video-browser.json`。
这不代表语音播放或移动端编码验收。功能表在390/768/1440无页面横向溢出，
320下长表页面仍有全局min-width与滚动条造成的7像素溢出；已如实留在
`features-ui-check.json`，未宣称所有页面的窄屏布局全部通过。
最终截图及只读结果位于`var/chili-acceptance-v41-20261104/release`。

发布后辣椒聊两接收器connected、队列0，测试及动作开关false，双平台活动
受控窗口0、未决提交0。旺商聊test接收器connected；测试受控1保留的
`native_push_boundary_unknown`发生于01:42:38，早于最终部署，协议包
`4/1/318`。该阻断独立记录，未跳过原始隔离数据或恢复水位，不能写成
“四个接收器均在线”。本轮辣椒聊验收通过不代表该旺商聊阻断已经修复。
readiness仍为false。临时网站会话缓存未进入冻结源码、镜像或分析白名单，
核验结束后清除。

## 辣椒聊群通知与原生禁入接入（2026-11-04）

本轮由实际运行 v41 冻结源码派生 v42，排除工作区未验收的解码、轻量化、
账号范围和其他媒体候选。固定桌面 1.0.3 及随包 WASM/SDK 核对
`permanentBan`、原生禁入名单和解除路由；详见
`lajiaoliao-membership-20261104.md`。

05:08 Asia/Shanghai 只读核查双账号各 19 份留存群通知，共 38 份，含此前
受控样本；不是 38 次新操作。资料、移出、加入、成员禁言/解禁、全群禁言/
解除、成员资料、公告和取消置顶均按原群投影成功，无读取错误。
固定原生禁入名单接口返回 total=0，空列表完整；未发出原生变更。
普通移出、永久禁入和解除分别校验动作授权；移出通知不含永久标记。

新增前向迁移047仅扩展持久操作类型约束。功能目录增加原生群通知读取、
禁入名单及解除，并拆分普通移出/永久禁入子项；保留旧ID和原授权。
新增两项只读实测证据，不把App演示记为本站写入通过。
真实永久禁入、解除和非空名单尚未验收，readiness与生产准入不变。

冻结候选完成 race/sodium：590 项通过、68 项条件跳过，28.345 秒；
串行隔离 HTTP 296 项通过、1 项负载专用跳过，120.507 秒，gateway
152 项通过，19.030 秒。新增回归覆盖普通移出授权不能用于永久禁入、
名单不完整拒绝、账号版本变化、取消及未知结果仅核对；HTTP 群通知投影
覆盖会话隔离。首轮因目录测试仍断言移动端证据为 0 而失败，修正为本轮
4 项独立演示记录后完整重跑通过；首轮失败日志保留。

产品类型检查与前端构建 32.318 秒、API 构建 16.296 秒、架构单测
8.759 秒、容器 smoke 24.671 秒通过；架构事实检查、架构及仓库守卫、
渲染缓存测试和 Mermaid 渲染通过。自动测试只使用合成输入和串行隔离
wsl_test，不自动登录、发短信、发消息或操作群。
匹配可信基线的 CodeBoarding 增量分析 40.055 秒通过，使用现有 DeepSeek
设置，1 次模型请求，费用用量已返回，私有持久检查点保存成功；没有全量
分析回退。该结果针对冻结 v42 白名单源码，工作区未验收候选不纳入发布证据。

2026-11-04 05:30:08 Asia/Shanghai 更新现有 Compose 的 API 和 Web，
前向迁移047已记录：

- API：`sha256:2542fff450225c490674051e99400f17b7aeaec7cc0b1cc6392121c57a5f6797`
- Web：`sha256:dda16bc4c996e52534261e32033a7eac13e79dabfcc019ee10c89602dd9a864b`

API/Web 健康，PostgreSQL 和热群引擎未重建；部署前后资源、配置、凭据、
挂载及网络摘要一致，v41 镜像保留供回滚。部署前未决提交为0。
冻结源码、镜像、检查输出及交付摘要位于
`var/chili-membership-v42-20261104/release`；发布后的实测记录另存该目录，
没有反向改写已分析、构建的冻结来源。

05:36:49 的最终只读 HTTP 核查通过：群主读取完整空禁入名单；
普通成员返回 `lajiaoliao_native_role_required`，跨平台请求拒绝。
双账号消息页各返回38条记录，其中各20份群通知按原群投影，投影错误0；
这是发布后消息页查询范围，包含历史样本，不是40次新群操作。
两个辣椒聊接收器均为connected，测试/动作开关false，活动受控窗口0，
未决提交0，`native_ready`、`ai_ready` 仍为false。
本轮不宣称已修复独立存在的旺商聊 `4/1/318` 阻断。

浏览器已核对新目录条目及真实通知：全群禁言/解除、公告、成员资料、
移出和重新加入均有可读展示；普通移出与永久禁止申请进群分为独立子项，
禁入名单显示指定场景已核验，解除及两种移出保持本站写入待验收。
截图 `features-deployed.png`、`group-notifications-deployed.png` 与安全聚合
`live-verification.json` 保留在上述目录。本轮没有创建新的原生授权，
没有踢人、禁入、解除、邀请或其他原生变更。

## 辣椒聊剩余原生能力受控验收（2026-11-04）

按所有者要求，对唯一测试群和既有两个自有账号开展分项验收，不批量调用。
第一轮窗口300秒、预算10，实际先提交7次：群名修改/恢复、本人群昵称
修改/恢复、另一自有普通成员群昵称修改/恢复及开启全群禁言。前6次均有
持久成功回执及逐项原生回读；第7次禁言回执成功，随后读取失败
`lajiaoliao_group_shape`，窗口已取消，没有重发禁言或续预算。

固定桌面1.0.3随包SDK的状态枚举在偏移31931附近声明
Normal=0、Banned=1、Dismissed=2、Muted=3。运行版错误接受0/2并将2映射
为禁言，实测禁言后的3被拒绝。候选修复为只接受0/3、按3映射禁言，
封禁、解散和未知状态保持拒绝；保留缺省正常状态的proto3语义。
没有变更协议路由、授权栅栏、解码器、消息结构或生产准入。

状态修复候选从v42冻结源码派生，仅同步两个运行源码文件及其回归测试。
race/sodium 599项通过、68项条件跳过，53.759秒；前端构建61.585秒。
后续检查、恢复及逐项验收结果在本节追加，以上不代表整个验收已通过。

v43状态修复已完成全部检查并部署：隔离PostgreSQL HTTP测试296通过、
1跳过，200.357秒；gateway152通过，20.191秒。架构检查、仓库守卫、
52项架构工具测试、Mermaid渲染和匹配DeepSeek基线的增量分析通过，
增量34.401秒并保存可信检查点。首轮容器smoke因冻结目录未保留文件权限
导致Nginx启动失败，未部署；修正打包权限后重新构建，smoke24.873秒通过。
API健康、仅API重启，Web及其他服务未重启，配置和前镜像保留。

原7次窗口已取消，恢复窗口仅使用原计划剩余3次预算：解除全群禁言、
临时公告及恢复原公告，逐项持久成功且独立回读一致，随后取消窗口。
群名、两类群昵称、公告及未禁言状态已恢复；未重做成功的动作。
重启后群主从既有密封凭据恢复连接，双账号接收快照持续刷新；
没有申请短信或进行原生登录。

随后单独授权成员轮，300秒预算6次，实际只使用2次：普通移出自有普通
成员成功，完整名单确认离群，原生禁入名单为空；邀请请求明确被接受，
但目标方目录仍不在群。固定桌面邀请入口显示待确认，受邀者本人接受在
本站尚未接入，不能将邀请回执当入群通过。该窗口已取消，没有重邀、
永久移出、解除禁入或退群。已提示所有者在原生App接受这份原邀请，
后续必须先现读确认恢复为普通成员。

申请列表兼容诊断另发现`total=0, groupRequests=null`被旧代码误判为未知
格式。修复仅接受有明确零总数依据的空集合，缺少集合且缺少零总数仍报错。
11组空值、错误类型及总数边界合成用例通过；修复代码在本群管理侧只读
返回完整0项（2026-11-04 06:41:20，Asia/Shanghai）。普通成员不能借此
管理侧接口读取申请。非空申请及受邀者本人接受仍未验收。

功能证据新增8个能力组及5个独立原子项，通过场景与剩余缺口分别登记，
生产准入和`native_ready`、`ai_ready`不变。v44仅承接上述空列表修复和
验收证据，未混入未通过的解码、轻量化或其他功能候选；最终检查和交付
结果另行追加。详细记录见`docs/lajiaoliao-partial-acceptance-20261104.md`。

v44最终检查：全量Linux race/sodium611通过、68条件跳过，44.208秒；
串行隔离PostgreSQL HTTP296通过、1条件跳过，137.861秒，gateway152通过、
19.155秒。目录生成及同步测试通过；产品前端构建2.889秒，API构建23.086秒，
独立容器smoke25.060秒。架构事实、边界/仓库守卫、52项工具测试、渲染缓存
及七幅Mermaid检查通过。本轮没有执行远端CI。

DeepSeek增量首次61.247秒因上游HTTP502失败，usage未知1次，预留109829
保持登记；未回退全量或覆盖失败记录。只读模型目录仍可达，原可信基线
完整，在总预留不超过300000的限额内再执行一轮相同源码增量，29.347秒
通过并保存可信检查点。两轮合计2请求、预留219658、已报告26906 token、
未知用量仍为1；业务模型配置未改动。来源摘要
`5ba99641efdc08f0a2d80ceddd355b9a125c70c99167a087945a94d6f35322f7`。

2026-11-04 06:55:40完成v44本地Compose交付，仅API更新为
`sha256:eaca15e9a3d95952900805e69c90cfe684b7b4e2e144379c5270e14074f21f2e`，
healthy；Web仍为`sha256:dda16bc4c996e52534261e32033a7eac13e79dabfcc019ee10c89602dd9a864b`。
无新增迁移，环境、资源限制、挂载、网络一致，数据库及热群引擎未重建，
上一版v43镜像保留回滚。临时动作开关均恢复，三个窗口已取消，未决提交为0。

06:56:55真实接口确认新增10个可见独立项有通过记录，剩余部分接入4项：
解除原生群禁入、退出群组、永久移出、发送入群邀请；本人接受邀请另为待接入。
两个辣椒聊接收器均connected且观察新鲜。07:00真实浏览器确认38/43指定场景
可用、4部分接入、1待接入；禁言和解除各自绿色，完整验收仍0，生产准入未变。
网页会话到期后仅恢复本站登录，没有操作原生登录。07:02:07目标方原生目录
仍明确不在群，故未继续移出、禁入或退群；等待所有者在App接受原邀请一次。
私有部署、运行核对与截图见`var/chili-partial-results-v44-20261104/release/`。

## 辣椒聊邀请恢复与永久禁入受控验收（2026-11-04）

所有者确认App接受此前邀请后，16:52及16:54 Asia/Shanghai从双方现读
完整成员名单，目标账号自身目录确认在群且为普通成员，执行方仍为群主。
此前本站邀请请求和App接受后的入群完成核验；本站“本人接受邀请”
接口仍未接入，不能以App行为替代其通过记录。

仅对该自有普通成员实施原计划剩余项目：300秒窗口、预算3。16:55:59
永久移出持久成功，16:56:24独立现读目标离群、完整禁入名单精确含目标；
16:56:55解除禁入持久成功，16:57:29完整名单恢复为空且目标仍在群外；
16:57:51单次恢复邀请持久成功，16:58:18仍待App接受。16:58:46已取消
窗口并恢复原测试/动作开关。三个动作各一次，无批量、重试或补充预算。
准备脚本误取被省略的空名单字段导致只读断言异常，修正脚本后只补做
空名单读取，没有新建窗口或重复提交。操作及回读详见
`docs/lajiaoliao-membership-acceptance-20261104.md`。

17:01:59目标自身目录仍确认不在群。本人退群及后续恢复邀请未执行。
本轮更新邀请、永久禁入、解除和非空名单的独立证据，仍不启用生产动作。
发布候选只从当前运行v44冻结源码派生，不夹带解码、轻量化或媒体候选；
后续必需检查、匹配CodeBoarding增量及Compose部署结果另行追加。

v45冻结候选最终检查：Linux race/sodium611通过、68条件跳过，39.595秒；
串行隔离wsl_test的HTTP296通过、1条件跳过，152.909秒，gateway152通过，
19.515秒。根工作区及冻结候选目录分别生成并完成同步测试。产品前端构建
2.643秒，API构建29.057秒，容器smoke25.433秒通过。架构事实检查、
边界守卫、仓库守卫、52项工具测试（13.462秒）、渲染缓存及七幅Mermaid
检查通过；只更新源文件证据摘要，图布局不变。本轮未执行远端CI。

匹配可信基线的DeepSeek CodeBoarding增量分析55.370秒通过，单次模型请求，
预留110079、已报告29118 token，本轮usage_unknown=0，持久检查点保存成功。
源码摘要`071eaa01585f4e99268c23469849d17b20262d662c251a844b00da738c38740c`。
这属于本轮冻结源码的咨询性分析，独立于本地测试和真实原生验收；没有
全量回退、业务模型配置变更或将运行证据及凭据发送给分析供应商。此前
其他轮次登记的未知用量不因本次成功而清除。

2026-11-04 17:11:45 Asia/Shanghai完成现有Compose交付，仅API更新为
`sha256:871c2678bcaec682d426ad8d30ec52995111f08a8b5397d1a8ebc9bc9695f905`，
healthy。Web仍为
`sha256:dda16bc4c996e52534261e32033a7eac13e79dabfcc019ee10c89602dd9a864b`，
数据库和热群引擎未重建，无新增迁移；资源、环境、挂载和网络摘要一致，
v44镜像保留回滚。重启前未决提交为0。相对v44只更新3个功能定义/证据
源码、1个对应回归测试文件、生成目录、报告及架构源证据摘要；没有改变
控制、协议、媒体或解码运行实现。

17:12:26真实HTTP确认41/43个独立项指定场景可用，两个辣椒聊接收器均
connected且观测新鲜，临时测试/动作开关均false，活动测试窗口0；17:13
真实浏览器显示永久移出、解除禁入及发送邀请均为绿色“指定场景可用”。
受控能力组已核验37，完整生产验收仍为0，`native_ready`、`ai_ready`
仍false。剩余本人退群部分接入、本站本人接受邀请待接入，各自独立显示。
目标自身目录最后回读仍未入群，恢复邀请不重发；继续退群验收需要所有者
在App接受该邀请一次。冻结来源、检查、分析及交付结果位于
`var/chili-membership-followup-v45-20261104/release/`，发布后证据没有反向
改写已分析或构建的冻结来源。

## 辣椒聊本站本人接受邀请（2026-11-04）

所有者要求补齐本站本人接受入口，不再重复要求App演示。核对固定桌面通知页、
SDK包装层及WASM描述符，确认本人邀请和管理者审批共用接受方法，
分别依靠申请者本人、来源2及非本人邀请人识别。此前仅凭方法名把接受解释为
管理员审批不准确；实现与固定哈希见`docs/lajiaoliao-self-invitation-20261104.md`。

本轮只读核查：自有目标仍在群外，申请侧接口返回唯一测试群的一条完整待处理
邀请，来源2、申请者本人、另一自有账号为邀请人及结果0全部匹配。
该邀请沿用上一轮已成功发出的恢复邀请，没有重新邀请或批量原生操作。

已补协议白名单、按群本人读取、不可变邀请指纹、独立单项授权及持久确认，
并复用现有受控工具和前端邀请选择。平台、账号epoch/version、凭据、原生连接
和网站权限继续在最终提交核验；不伪造群成员角色，不向助手开放测试授权。
群/本人无法区分多条待处理邀请时拒绝提交，已处理或换时间的记录不可重用。
未知结果只核对原操作，不自动重新接受。无需新增迁移或服务。

本机合成单元测试覆盖本平台列表结构、错误身份、普通入群申请、失效/重复记录、
确认指纹、跨平台隔离、取消及超时只发一次。正式冻结候选将从运行v45派生，
只包含本轮实现、回归及文档；完整发布检查、真实单次接受和双方名单结果待追加，
在真实验收之前不增加通过证据。`native_ready/ai_ready`保持false。

v46冻结候选Linux race/sodium641通过、70条件跳过，42.104秒；
串行隔离wsl_test HTTP299通过、1条件跳过，139.152秒，gateway153通过、
19.465秒。产品前端最终构建27.512秒，API缓存构建2.346秒通过。
两项浏览器合成回归11.895秒通过，覆盖320/390/768/1440、本人邀请选择、
取消不提交、确认单次提交、预算耗尽及未知结果不重发；真实原生请求0。
首轮浏览器脚本因JSON属性顺序比较误判，已改为逐字段核对并保留失败记录，
没有变更产品逻辑以迁就测试。最终架构增量、容器smoke及真实接受另行记录。

v46最终架构事实、边界及仓库守卫、52项工具测试8.281秒、Mermaid检查通过，
容器smoke24.630秒通过。匹配可信基线CodeBoarding增量31.892秒通过，
本轮2次模型请求、已报告58952 token、未知用量0，持久检查点已保存。
源码摘要`636f282c3390f28024418c303348ebafb906fa941a75a73594fda59f7bd8d22b`。
18:05后现有Compose API/Web已更新且healthy，配置及资源摘要一致，
无新增迁移，v45镜像保留。API为
`sha256:e5cc687ed0f317649a2380bd5678e6f720759589b71c10f535a79f8df1c32a7f`。

部署后真实只读GET本人邀请返回HTTP200空正文；尚未创建窗口或提交接受。
只读协议核查确认当前reqTime为毫秒，原实现按秒生成time.Time，
序列化超出支持年份导致响应为空。此前合成秒级数据未覆盖此问题。
修复毫秒转换、保留原始指纹时间并增加可序列化范围检查及完整投影JSON回归；
后续v47仅从已部署v46派生该修复，不修改已发布冻结来源或隐去失败记录。

v47修复候选Linux race/sodium643通过、70条件跳过，36.397秒；
串行隔离wsl_test HTTP299通过、1条件跳过，127.441秒，gateway154通过、
19.048秒。前端构建3.085秒，API构建21.342秒，容器smoke24.880秒通过。
浏览器两项合成回归16.270秒通过，无原生请求；架构事实、边界与仓库守卫、
52项工具测试8.080秒、渲染缓存与Mermaid检查通过。匹配可信基线的
CodeBoarding增量27.975秒通过，一次模型请求、已报告27319 token、
未知用量0并保存检查点，源码摘要
`83bf1d964c8f4c9725424a9f5edd8c374986692a289eecd6b7e1ad756ad35836`。
未执行远端CI，无全量分析回退或业务模型配置变更。

18:19:11 Asia/Shanghai完成v47现有Compose交付，仅API重建，健康镜像
`sha256:db2a6846b0cb87c89b1b6c4e9bc0cb8322134ac8b8b749fde949ad8b6f0d92dd`；
Web保持`sha256:53268a5a08840f6dceaad49330510679abaa4092425d7b750990d2c394ba009e`。
其他服务未重建，无新增迁移，配置及资源摘要一致，v46镜像保留供回滚。
18:19:45本站读到唯一待处理邀请。首次准备脚本同时开启测试及动作开关被
现有前置状态校验拒绝，未创建窗口；改为按现有界面顺序逐项启用后通过，
没有更改产品门禁或使用SQL造证据。

真实受控验收：受控测试号2、唯一测试群、300秒、预算1，
只接受上一轮已发出的恢复邀请。18:21:45本站持久操作
`1bf4c147-34f5-4569-8790-f380b30a558c`明确成功；
18:22:02从双方分别读取完整成员名单和群目录，群主仍为群主，
目标已入群且为普通成员。没有重新邀请、批量原生操作、自动重发或增加预算。
18:22:28取消窗口，使用次数1，临时测试及动作开关恢复原值。
本站本人接受与此前App行为分别留证，功能原子项增加独立绿色通过记录，
不继承发送邀请或另一平台结果；本人退群未在本轮执行，生产准入保持关闭。
冻结来源及发布检查分别位于v46/v47 release目录，真实单步证据位于
`var/chili-self-invitation-v46-20261104/evidence/`，不会发送给架构模型。

v48只更新本轮验收定义、说明和生成目录；协议与控制实现沿用已验证v47。
Linux race/sodium643通过、70条件跳过，34.000秒；串行隔离wsl_test
HTTP299通过、1条件跳过，126.492秒，gateway154通过、19.356秒。
前端构建3.236秒、API构建23.013秒、容器smoke23.791秒，
两项浏览器合成回归17.715秒全部通过。架构事实、边界及仓库守卫、
52项工具测试8.039秒、渲染缓存与Mermaid检查通过。CodeBoarding匹配
可信基线增量36.565秒通过，1次请求、已报告29251 token、未知用量0，
检查点保存；源码摘要
`af9c6494b464f04a6580697067cc256982e68e6df24a66928a880f74d4910eeb`。
这些检查独立于真实原生验收，本轮没有执行远端CI。

18:35:32现有Compose仅更新API至
`sha256:466f8d81e3189dfaf655e31acf9c1c1b8473754dd70b8ce30a770a0684dd49a6`，
healthy；Web、数据库、热群引擎保持原容器，无迁移，环境、资源、挂载及网络
摘要一致，保留v47镜像回滚。18:37:27真实HTTP确认本站接受的独立证据已生效，
两个接收器均connected且观测新鲜，临时开关false、活动授权0。
18:37:51真实浏览器确认“本人接受入群邀请”两列绿色显示，
43个独立验收项中42项指定场景可用、完整生产验收0；剩余本人退群未在本轮执行。
`native_ready/ai_ready`仍false。发布后验证脚本曾误把healthz包装层当正文，
修正读取data后通过，没有修改健康接口或触发原生动作。当前测试群已恢复双账号在群。
发布与浏览器证据位于`var/chili-self-acceptance-v48-20261104/release/`，
真实页面截图为`output/playwright/chili-self-acceptance-live.png`及详情截图。

## 辣椒聊本站本人退群验收（2026-11-04）

所有者报告已在App退出。19:27:48 Asia/Shanghai只读核对群主完整成员名单
及目标本人群目录，均确认目标已离群、无待处理邀请。为补本站真实执行，
公告同一自有双账号、唯一隔离测试群的邀请恢复、本站接受及本站退出三个步骤；
三个单次持久授权共用不超过300秒的期限，没有批量、自动重发或增加预算。

19:32:27邀请、19:32:54本人接受分别明确成功；完整名单确认目标恢复普通成员。
19:33:37本站本人退出的持久操作
`dd69ef16-14bd-4604-9d0b-f36024df1d6d`明确返回
`native_leave_acknowledged`。19:34:16再次独立读取：群主完整名单已无目标，
目标本人目录明确离群，群主身份不变，待处理邀请0。
19:35:22三个窗口全部关闭，实际尝试总数3，双账号临时测试及动作开关
恢复原值；最终目标保持离群。

群主连接分别收到19:26 App退出及19:33本站退出的1504通知，
现有按群读取接口均输出`member_left`，目标和群精确匹配。
这两条记录的历史持久解码状态仍为`unsupported`，按需结构化读取负责展示；
本轮没有修改解码状态、水位或解析算法，不以通知充当角色或授权证据。
详见`docs/lajiaoliao-self-leave-20261104.md`。

新增`leave_group`本平台独立验收，更新群事件读取证据；
38个纳入原生能力组、43个独立验收项均有指定场景记录，完整生产验收仍0。
`native_ready/ai_ready`保持false。v49仅从运行v48冻结来源派生本轮元数据、
测试及文档，强制发布检查和部署生效结果待追加；真实单步证据位于
`var/chili-leave-v49-20261104/evidence/`，不向架构模型披露。

v49冻结候选Linux race/sodium643通过、70条件跳过，36.251秒；
串行隔离wsl_test HTTP299通过、1条件跳过，143.179秒，
gateway154通过、19.378秒。产品前端构建2.862秒、API构建29.173秒、
容器smoke25.636秒通过；两项四视口浏览器合成回归12.532秒通过，
覆盖本站接受和普通成员本人退出、取消不提交及未知结果不重发，无真实原生请求。
架构事实、边界与仓库守卫、52项工具测试、渲染缓存及Mermaid检查通过。
分析工作区首次更新未指定只读模块模式，遇到旧vendor不一致而停止；
显式使用既定`GOFLAGS=-mod=readonly`后更新和全部检查通过，没有改动依赖。

CodeBoarding匹配可信基线的DeepSeek增量66.276秒通过，
1次请求、已报告28487 token、未知用量0，持久检查点已保存；
源码摘要`fc3c6ac3d10330f1f9862bfea7e5bce3f0c3decc0888f172afd7a2fd3c7c48af`。
没有全量分析回退或业务模型配置变更，未执行远端CI。
发布前核对未解决提交为0；现有Compose只更新API，Web、数据库和热群引擎
保持原容器，环境、资源、挂载和网络摘要一致，无新增迁移，v48镜像保留。
API镜像为`sha256:b378a1ae364986bc70fc5507be05980b8573250204af3609057be1dbf7cfc536`，
健康检查通过。部署后真实HTTP和页面结果另行追加。

19:45:33完成现有Compose交付。19:46:03真实HTTP确认本站退群独立证据已生效，
双账号接收器connected且采样新鲜、临时开关均false、活动授权0，
`native_ready/ai_ready`仍false。19:46:54真实浏览器确认“退出群组”两列绿色，
38个能力组、43/43独立项指定场景可用、完整生产验收0，无额外原生提交。
页面截图为`output/playwright/chili-self-leave-live.png`及详情截图；
发布和只读核验记录位于`var/chili-leave-v49-20261104/release/`。
测试号2最终保持离群，下一次真实群动作须先核对当前在群状态。

## 工作区清理、文档整理与原生性能复核（2026-11-04）

本轮没有产品算法、依赖、迁移、真实原生操作或生产部署。开始和结束均核对现有 v49：
API `b378a1ae364986bc70fc5507be05980b8573250204af3609057be1dbf7cfc536`、
Web `53268a5a08840f6dceaad49330510679abaa4092425d7b750990d2c394ba009e` 不变，
四服务健康。当前/上一版 v49/v48 冻结源码、配置及镜像保留，readiness 不变。

### 清理与文档

19 份旧发布重复源码/资产和一次性脚本，加上散落 var 的临时诊断脚本、退役 Simon
研究材料及可重建产物，共 64713 文件、941267004 字节。逐文件 SHA-256，
压缩后解压核对、再检查原文件未变化后移出工作区。仓库外私有归档共 20 组，
压缩包 223458229 字节、校验清单 14183359 字节，合计 237641588 字节；
按逻辑大小净减少约 671.03 MiB。没有删除失败报告、验收 JSON、生产数据、凭据、
nonce 历史、镜像或数据卷。当前仍依赖的 v42 `live-api.mjs` 另行保留。

清单及执行记录：`var/workspace-maintenance-20261104/cleanup-result.json`。
归档位置：`~/.local/share/artwslbot/workspace-archives/20261104/`。
恢复方式见 `docs/workspace-maintenance.md`。
清理后 `go list ./...` 只发现 22 个正式工程/工具包，不再将 var 临时 Go 脚本作为包扫描。

重整 README、实施状态与文档导航，增加 `docs/business-module-development.md`；
对应现有目录、配置事务、权限、平台原语与持久执行约定。明确通用自动业务消费器尚未闭环，
受控授权不能复用于业务执行。更新检查点与历史候选提示，修正当前 Compose 4 服务/
1920 MiB、本地开发 2304 MiB 等旧文案。历史失败和日期专题保留。

### 当前源码合成微基准

当前工作区分别编译 nativewire/gateway sodium 测试二进制，
使用 `wsl-test:assistant-final-20261102`，1 CPU/512 MiB/GOMAXPROCS=1，
无网络、只读容器，串行 200ms 子项，交替 reference/candidate 及平台顺序运行 5 轮，
共 330 组读数。每轮差分测试通过，源清零后投影完整；结束后源及二进制摘要仍匹配。

有效场景每子项中位数等权相加：旺商聊耗时下降 23.06%、分配字节下降 37.87%；
辣椒聊耗时下降 50.33%、分配字节下降 74.67%，有效子项分配次数均未增加。
所有测量子项中位数未见耗时退化，但没有据此判断统计稳定性。
旺商聊压缩长文本字节仅改善 4.41%，不是每个子项都达到 20%。
这是工作区候选与测试专用冻结参照的局部比较，不是 v49 两服务对照，也不是 P95 或整链路通过。

运行版代码复核仍发现历史领取扫描、辣椒聊重复游标读取和每秒重复状态写入；
旧增量领取候选的首次/恢复全扫退化仍需处理，未直接启用。
完整方法、各类 ns/op/B/op/allocs/op 及后续验证顺序见
`docs/native-performance-review-20261104.md`；
原始日志、源/镜像/二进制摘要、330 个读数和汇总位于
`var/native-performance-review-20261104/`。

### 检查与未执行项

当前工作区 nativewire、gateway、features、control、assistant 的
`-race -tags sodium -count=1` 非数据库集成测试全部通过，网络隔离；
五包耗时分别 1.222/2.283/1.025/1.009/1.074 秒。
仓库产物/固定上游/不可变迁移守卫、架构事实与所有权守卫通过。
最初使用系统 Python 因缺 PyYAML 未运行检查；改用既有架构工具虚拟环境后通过，
没有安装新依赖或修改架构图。新增 6 份入口/指南的 56 个本地 Markdown 链接存在。

本轮没有发布，因此没有执行发布专用前端构建、串行隔离 PostgreSQL 性能矩阵、
容器 smoke 或付费 CodeBoarding；没有调用业务模型、短信、登录、上传或发送。
整链路 P95、饱和吞吐、100/300 群及 30 分钟持续负载没有本轮新验收结论。

收尾检查：20 个归档摘要复核、v49/v48 来源存在、功能目录同步、56 个本地文档链接、
新增入口/指南和工具的 Prettier、Node 语法及本轮文件 `git diff --check` 均通过。

## 双平台四分区缓存与基础群文本聊天（2026-11-04）

### 范围与冻结

以复核的 v49 Compose 及对应冻结来源为基线，只叠加本轮白名单。
第一阶段 `var/cache-v50-20261104-r1/` 已于 22:38:51 部署，仅更新 API；
API 为 `sha256:cdcb9474629302ba85647a15d6e96b8b724ebc039675b8327b74cd9959ddf297`。
Web、热群引擎和 PostgreSQL 容器保持不变，没有迁移。
环境、资源、挂载和网络摘要一致，v49 镜像保留。

第二阶段冻结来源为 `var/basic-chat-v51-20261104-r8/source`。
70 个原生协议及解码相关文件与 v49 摘要相同，`CodecVersion=7`。
领取算法、取源/写回合批和旧解析候选未纳入。
仅辣椒聊连接内游标/水位复用作为本计划明确要求的接收改动纳入，
确认提交后更新、提示/重连/不明提交刷新由隔离数据库回归覆盖。

四分区 TTL/LRU、容量、按账号限额、只读请求合并和独立取消已实现。
成员展示投影清除执行角色，入口/交付仍核验权限和实时版本。
连接所有者也参与缓存键；新增连接交接和迟到返回拒绝回归。
配置缓存仅存密封字节。现有持久群目录快照继续提供群资料；
没有缓存授权、密钥、nonce、消息正文或模型回答。

基础聊天实现位于 control：双平台原生文本/@/引用、持久唯一决策、
有界候选、群设置覆盖、北京时间时段、冷却、共享网关、单次费用和独立发送尝试。
热群引擎仅生成文本，不接入管理工具、长期记忆、印象、学习或插件。
新增迁移 048，平台准入默认关闭。生成/发送不明不重放；
费用不明冻结并禁止发送，连接/部署版本变化阻断最终写入。

### 缓存合成性能

缓存核心每平台五组交替对照，每场景每侧 1000 次读取，
一毫秒合成读取端口；100 群、300 群全部未命中、300 群半数热点，
以及 80% TTL 内重复读取。没有连接真实原生接口。

| 平台 | 重复读取请求减少 | 全未命中100群P95中位变化 | 全未命中300群P95中位变化 | 半数热点请求减少 |
| --- | --- | --- | --- | --- |
| 旺商聊 | 80.00% | +2.34% | +3.62% | 49.90% |
| 辣椒聊 | 80.00% | +1.89% | +2.03% | 49.90% |

旺商聊 300 群有一轮高于 5%，五轮中位数低于门槛，原始轮次保留。
重复读取的 P95 没有显著下降：20% 未命中仍进入 P95；
不能用请求数下降替代延迟改善。

另在 `wsl_test` 运行四分区各五组、80 对/160000 次读取，
每次保留两条真实版本查询、使用相同的一毫秒合成原生端口。
独立冷/热缓存逐请求交替，避免整段先后运行的时间漂移。
四分区重复读取均减少 80%，全未命中 P95 中位变化如下：

| 平台/用途 | 100群 | 300群 |
| --- | --- | --- |
| 旺商聊/管理 | +0.87% | +0.63% |
| 旺商聊/聊天 | +2.73% | +2.71% |
| 辣椒聊/管理 | -0.24% | +0.48% |
| 辣椒聊/聊天 | +0.80% | +0.31% |

每侧每千次读取均为 2000 次连接池获取，没有查询放大；
并未减少这些实时数据库查询。
记录包含每轮 P95、连接池等待、Go 堆、协程及原生端口调用数。
所有每账号群分布不超过 30 个绑定。

30 分钟缓存核心持续观测采用 1 CPU/512MiB/GOMAXPROCS=1 和 race：
总计 719248 次读取、403607 次合成端口读取；
三阶段分别 239776/239892/239580 次，覆盖 100/300/300 个不同群。
采样最大 Go 堆 3839120 字节、最大 2 协程，调度最晚 83.95ms；
四分区各自不超过 256 项，结束无执行中读取。
被测 cache.go 和 soak_test.go 的摘要与最终候选相同，原日志来自 r4，
最终候选记录了这一来源和逐文件摘要，未继承其他代码的旧检查。
这不是全 API/数据库/聊天链路持续负载、真实百群容量或全天稳定性证明。

### 正确性、界面与发布检查

最终 Go race/sodium：653 通过、90 条件跳过。
串行隔离 PostgreSQL：HTTP 315 通过、1 跳过；gateway 158 通过、1 跳过。
双平台聊天覆盖确认发送、未知发送、未知 usage、撤权、连接交接、
零概率、号池机器人互触发及旺商聊部署版本变化；同任务最多一次模型和有效写入，
重复栅栏拒绝，恢复不重放。现有共享网关、引擎与权限回归同时通过。
新原生读取器夹具核验真实结构的 @/引用、私聊与媒体排除和解封清零后的结果所有权。

产品类型检查与构建、API 构建、容器 smoke 通过。
四视口 320/390/768/1440 的浏览器合成回归通过：
双平台作用域、原生昵称、有效 0% 概率、时段/冷却/模型、
查看关闭零提交、迟到响应隔离、一次版本化保存和折叠缓存监控。
浏览器所有 API 均为合成夹具，原生动作和业务模型调用为 0。
功能目录已重新生成；“已实现”状态补中文文案，不自动显示为生产可用。

冻结来源的架构事实、边界/仓库守卫、工具单元测试、渲染缓存和 Mermaid 通过。
匹配 DeepSeek 可信基线的增量分析通过，保留持久检查点；
最后模型源码摘要 `084f361c69601367d74312884c475517b34c83e6b83c7a2e23119fce5e633757`。
同一模型输入的最终增量检查没有新增模型请求；此前当前实现增量
1 请求、已报告 30270 token、未知用量 0。没有全量分析回退或业务密钥变更，
未执行远端 CI。前端状态文案修正后另做构建、浏览器及增量检查。

### 失败记录与真实验收前置

保留数据库性能第一次未迁移测试库而失败的 r5 日志；
r7 两次整段先后对照因分区间 P95 漂移未过 5% 门槛，报告与日志均保留。
改为逐请求交替后 r8 完整五轮通过，没有放宽门槛或删除失败。
旧浏览器 320 视口检查曾在弹窗进入动画中测量；等待实际动画完成后四视口通过。

只读账号清单：旺商聊管理认证 2、聊天未认证 1；
辣椒聊管理认证 2、没有聊天用途账号。真实聊天定向回复和随机参与未验收，
没有复制管理身份、自动登录、短信、付费业务生成或真实原生发送。
真实验收须补齐两个平台独立且已登录的聊天用途账号、对应测试群成员身份和有效申请。
原管理用途测试群的旧通过记录不替代本次聊天验收。
`native_ready` / `ai_ready` 保持 false。

### 第二阶段部署及只读核验

2026-11-04 23:55:44（北京时间）完成现有 Compose 交付，仅更新 API、Web、
热群引擎；PostgreSQL 容器未重建。兼容迁移 048 成功，旧迁移未改动。
API/热群镜像为
`sha256:7d72c9ac1eca44abacb9c267dd405a0c0e82e2fa88928df901a256c7249ae7bd`，
Web 为 `sha256:f6239052f07dcefcddf9ac775b6300477f28cdadd6a03d8fb0765c78ee3b2cc4`。
资源、挂载、网络和其他环境摘要一致；API 仅增加与既有热群服务相同的内部桥接令牌，
没有新增供应商凭据。三个组件各自实际上一运行镜像已保留回滚标签。

部署前未决提交为 0。部署后四服务 healthy，四个接收器保持 enabled，
三个持有当前连接租约，与部署前一致；未因本次发布提高 readiness。
两平台 `heat_chat_admission.enabled=false`，聊天任务和消费者均为 0。
23:56 真实 HTTP 核对四个缓存分区、新增独立业务目录及零候选/调用/发送；
两个“测试群”的运行状态均 blocked，原因是聊天作用域前置条件不完整，
没有错误显示为已运行。
23:59 真实浏览器在 320/390/768/1440 视口确认折叠诊断含四行缓存分区、
无整页横向溢出、无浏览器运行错误；仅 GET 请求，写入/模型/原生动作均为 0。

发布及只读结果位于 `var/basic-chat-v51-20261104-r8/` 的
`delivery-before.json`、`delivery-after.json`、`live-readonly.json`、
`live-browser.json`。失败性能记录保留在 r5/r7；当前代码、矩阵与完整负载日志
分别指向各自被测冻结输入，未混用其他工作树的分析图或检查。
本轮临时验收数据库容器和网络均已清理，没有删除生产数据或旧版本镜像。

## 大蛤接收阻断修复与恢复（2026-11-05）

本轮针对旺商聊管理账号“大蛤／测试受控1”，不扩大原生动作或聊天准入。
阻断快照从 `2026-11-03T17:42:38Z`（北京时间11月4日01:42:38）留存，
错误 `native_push_boundary_unknown`，协议包 `4/1/318`。
账号仍为 authenticated；这不是新版本部署后发生的登录失败。

只读解封该条密封隔离事件，确认外层正文318字节、内层 `15/11/200`、
正文304字节，完整 Property。`318` 是字节长度，不是上游业务错误码。
固定桌面 SDK 的四类 avSignal 通知契约见 `official-client-research.md` E48。
补丁只登记 `15/11–12` Property、`15/13–14` PropertyArray，完整验证后
密封保留为通知；不接听或发起通话，不生成聊天 ACK，不以通知时间推进恢复水位。
畸形与未审阅命令仍暂停。原包在补丁下为 notice、消息0、无 Boundary，
原隔离事件未删除、改写或直接用 SQL 转换状态。

从实际 v51 冻结来源派生 v52，变更仅 `internal/nativewire/im.go`、
`internal/nativewire/im_test.go` 与 `internal/gateway/receiver_postgres_test.go`。
应用解码、密封格式、`CodecVersion=7`、领取算法、worker及合批未改；
没有纳入工作区其他未发布候选，也没有新增迁移。

| 检查 | 本轮实测 |
| --- | --- |
| Go race/sodium | 654通过、90条件跳过，41.365秒 |
| 串行隔离 wsl_test HTTP | 315通过、1条件跳过，192.583秒 |
| 串行隔离 wsl_test gateway | 158通过、1条件跳过，19.336秒 |
| 产品类型检查及前端构建 | 3.861秒，命中未改动前端构建缓存，镜像字节与v51相同 |
| API构建／容器smoke | 32.523秒／24.699秒，通过 |
| 合成浏览器 | 接收器与消息页面，15.489秒；版本化重连、停收冲突与迟到响应隔离通过，真实原生请求0 |
| 架构事实与守卫 | 冻结来源 update/check、边界/仓库守卫、工具单测、缓存及Mermaid渲染通过；根工作区另做update/check通过 |
| 咨询性分析 | 匹配DeepSeek可信基线的增量44.734秒通过，1请求、28022上报Token、unknown0、持久检查点成功；未执行远端CI |

模型分析只使用冻结白名单源码，未发送运行数据、隔离正文或凭据。
本轮模型源码摘要为
`4b74eb481a42cd08aed4c0dad61fc2a72ece90ab8838a2837b3b3265bafbca9d`，
没有全量回退或用旧成功代替本轮检查。

2026-11-05 00:20:37（北京时间）仅更新现有Compose的API至
`sha256:3c3b5006b49b8469dd5e0dd15c7e05e1eb2152b57669a31bdcb30ea7e95a875a`。
发布前未决提交0；Web、热群引擎及PostgreSQL容器未重建，环境、挂载、网络、
资源限制摘要一致，紧邻v51镜像保留回滚。四服务健康，无新增迁移。

00:21:55通过既有接收API执行一次带revision的恢复，10→11；
没有直接SQL修改开关、水位、授权或原生角色，没有发消息、群管动作、
SMS或业务登录。00:22:00完成首次同步，落盘111条、写出111次ACK。
40秒八次采样均 observed/connected，心跳从00:22:00递增至00:22:30，
队列0、重连0、新增隔离0；原隔离事件仍存在。
恢复水位由既有正常同步流程从1793726315842推进至1793809321260，
不是手动跳过未知包。此处ACK仅表示socket写出，不代表上游处理回执。

00:23:37真实只读浏览器在320/390/768/1440视口均显示“大蛤／测试受控1”
接收正常，无“接收器已阻断”提示、横向溢出、浏览器错误或写请求。
独立只读查询确认四个启用接收器均connected且持有当前租约。
00:26:45再次只读核对大蛤已接收120条、ACK120次，心跳新鲜，
队列0、新增隔离0、重连0，错误为空。
两平台聊天准入仍false，`native_ready`、`ai_ready`未提高；
本轮短时恢复不作为全天稳定性或其他格式/业务验收。
冻结来源、检查、部署、恢复采样与浏览器结果位于
`var/wsl-receiver-v52-20261105/release/`，截图位于
`output/playwright/wsl-receiver-recovered-{320,390,768,1440}.png`。

## 2026-11-05 助手对话与侧栏候选

已完成顶部独立助手入口、TanStack AI Vue 布局参考、历史搜索／逐条删除、
复制回答及移动端铺满工作区。超管用户中心改“后台管理”并加入用户指定的三色
动画标识，侧栏作用域标签和等待同步页脚移除。

从运行 v52 冻结源码单独派生，新增049会话删除前向迁移，未带入其他候选。
race/sodium 654项、串行隔离 HTTP 316项／gateway 158项通过；
类型检查、产品镜像构建、三套助手浏览器回归、架构守卫、52项架构工具测试、
Mermaid渲染及最终镜像容器 smoke 通过。浏览器覆盖两平台、300群分页、
320／390／635／768／1440及短屏；删除回归验证正文清除、未确认建议取消，
费用／幂等／已提交操作不丢失。没有真实模型或原生群操作。

CodeBoarding 匹配可信基线预检通过，前两次增量分析先遭上游502，再超时；
失败及未知 usage 均保留。用户要求重试，2026-11-05 02:09:24（北京时间）第三次
DeepSeek 增量42.286秒通过：2请求、65391上报Token、未知usage0，持久检查点成功。
继续使用 `deepseek-v4.1-flash`，未触发备用GPT、重初始化或复用旧成功；
本轮白名单源码摘要为
`b48840fd2bd5866cf12f406ebaf700d56b671e7db767b356333623afd255c39f`。

02:10:27完成现有 Compose 发布，发布前未决提交0；生产已应用049，仅更新 API／Web。
API镜像为
`sha256:592430b4b886a6e04f6a5790fbc44907e158843d36136ee9a35a4d5d2d46ff8b`，
Web镜像为
`sha256:c2698fdfc8266b7bfb2e929349b6a19eb92ac00cb838ae168f07846052554164`。
四服务健康，热群引擎及PostgreSQL容器未重建，环境、资源、挂载和网络摘要一致，
紧邻旧镜像已保留。业务网关、原生和AI准入不变，未混入其他未发布候选。

上线后真实只读浏览器验证两平台及320／390／635／768／1440视口：
独立助手入口、后台管理三色标识、侧栏清理和输入区均生效，无横向溢出。
页面写请求0、浏览器运行错误0，未删除真实对话、调用模型或执行原生动作；
“大蛤／测试受控1”接收器observed、connected。真实删除效果以先前隔离库
及合成交互验收为证，不将本次只读检查描述为真实业务验收。
交付证据位于 `var/assistant-ui-v53-20261105/release-v53/`，
最终文档单独归档，保留构建时冻结源码不变；没有执行远端CI。
完整范围、实测和截图见 [助手UI本轮记录](assistant-ui-20261105.md)。

## 林夕接收阻断修复与恢复（2026-11-05）

本轮针对旺商聊管理账号“林夕／test”。登录状态为authenticated，
接收器保留北京时间01:07:22的blocked快照，错误 `native_push_boundary_unknown`，
协议包 `22/2/932`，revision27。当时队列0；不是本次重新认证的需求。

固定桌面2.8.3 SDK将该路由定义为 `proxyService.onProxyMsg`，响应为单个完整
`proxyMsgTag` Property。只读解封隔离事件181878，确认返回码200、正文932字节，
属性1／2／3长度11／900／13，与SDK结构一致；没有输出字段值、正文或凭据。
932为长度，不是业务错误码。源码证据及哈希见 `official-client-research.md` E49。

从实际运行v53冻结源码派生v54，仅改 `internal/nativewire/im.go` 及两份回归测试。
新增精确22/2路由的有界Property和完整消费校验，消息只密封保留为notice；
不执行代理正文，不进行HTTP、模型或原生调用，不产生聊天投影、ACK或恢复水位。
未知／畸形包仍隔离暂停。`CodecVersion=7`、解码算法、队列、并发、授权及发送链路不变，
未夹带工作区其他候选，无新增迁移。

| 检查 | 本轮实测 |
| --- | --- |
| Go race/sodium | 655通过、90条件跳过，44.480秒 |
| 串行隔离wsl_test HTTP | 316通过、1条件跳过，192.683秒 |
| 串行隔离wsl_test gateway | 158通过、1条件跳过，20.010秒 |
| 通知回归 | 直接及四种封装、截断、重复字段、尾部、大小上限、空Property、非成功码和相邻未审阅命令通过 |
| 持久接收回归 | 正常通知密封保留，不增加ACK／聊天／解码唤醒／水位；畸形通知隔离并阻止后续水位通过 |
| 产品前端 | 类型检查和构建通过，2.919秒，未改动前端且镜像字节与运行v53相同 |
| API构建／容器smoke | 26.569秒／25.410秒，通过 |
| 合成浏览器 | 接收器与消息页面通过，真实原生请求0 |
| 架构事实及守卫 | 冻结来源update/check、架构及仓库守卫、52项工具测试、缓存与Mermaid渲染通过；根工作区另做事实检查通过 |
| 咨询性分析 | 匹配DeepSeek可信基线的增量78.875秒通过，2请求、60037上报Token、unknown0，持久检查点成功 |

分析只接收冻结白名单源码，未发送运行数据、隔离内容或凭据；
源码摘要 `d6aff334361080e999a4abf67e864069cac4e2183946e2bd591327ae153f7495`。
未全量回退、切换模型或使用旧成功作为本轮证据，未执行远端CI。

2026-11-05 03:23:36（北京时间）仅重建现有Compose的API，镜像
`sha256:28aed76ad885d85f4e7bd06392d64f2c497d4e87e25d4805f7936b6db1c7ea0f`。
发布前未决提交0，数据库迁移集合与运行版一致；Web、热群引擎及PostgreSQL
容器未重建，环境、挂载、网络、资源限制和只读设置摘要一致，紧邻v53 API镜像保留。

03:24:29通过既有接收配置API进行一次带revision的恢复，27→28；
没有SQL修改开关、角色、授权或水位，没有发消息、群管动作、SMS或业务登录。
03:24:35完成首次同步，约42秒八次观测均observed/connected、新增隔离0、
队列0、重连0，消息和ACK写出从348增至358，心跳持续更新。
ACK只表示既有socket写出，不代表上游业务回执。原隔离事件181878仍为
quarantined，密封内容校验值与恢复前相同；水位从1793810686075由正常同步推进至
1793820275634，未手动跳过未知包。

03:25:48真实只读浏览器在320／390／635／768／1440均显示“林夕／test”
接收正常，无阻断提示、横向溢出、浏览器错误或写请求；此时消息365、ACK365。
只读数据库核对旺商聊2个、辣椒聊2个已启用接收器均connected且租约／观测新鲜。
四服务健康，`native_ready`、`ai_ready`仍false。短时观察不代表全天稳定性验收。

冻结输入、检查、分析、交付、恢复及浏览器证据位于
`var/wsl-proxy-notice-v54-20261105/release-v54/`，交付后文档独立归档，
不改写被测冻结源码。截图位于 `output/playwright/wsl-linxi-recovered-v54-*.png`。

## 客户接入、权限分控与自动业务（2026-11-05，已部署）

本轮基线为运行中的v54 API、v53 Web和v51热群引擎。冻结工作区断点位于
`~/.cache/artwslbot/assistant/2026-11-04T21-24-17-093Z/`。运行API仍为
`sha256:28aed76ad885d85f4e7bd06392d64f2c497d4e87e25d4805f7936b6db1c7ea0f`。
此镜像为实施基线；本轮已于17:22:49（北京时间）发布v55。
API限制1核／512MiB、Web限制0.25核／128MiB保持不变。
交付候选从运行版源码叠加本轮差异，不包含未发布解析、媒体或轻量化候选。
`CodecVersion=7`，不调整解析计时边界或密封格式。

已实现网站一次性邀请、双平台认证接收挑战、独立邀请接受尝试、群主本人确认、
管理员审核及独立跨租户转移审核。绑定恢复重新证明并提升版本；新绑定、
恢复和转移均关闭运行。迁移050至052已前向应用生产，旧迁移保持原样。
具体流程、接口及边界见 `docs/customer-onboarding.md`。

权限上限、字段和动作来自统一目录；负责人及超管之外的成员不能管理成员。
助手、自动规则、接入和热群采用独立正式准入；原生受控测试授权不参与这些
正式提交。新上限、准入和业务运行默认关闭，不迁移为自动开放。
首批规则实现拦截词撤回、业务黑名单禁言10分钟、豁免和关键词单条文本回复。
公开页面、助手及审计均不返回密封凭据、提示词或客户对话正文。

已修复预检暴露的SQL参数类型及合成名册问题、助手夹具UUID冲突、全局轮次
漏掉部分群晚提交回扫，以及热群过早抑制的问题。晚提交回扫改为每群持久进度；
热群按已核验的规则判断。关闭业务且没有接入挑战时，两个消费者仅按原1秒周期
各执行一次只读发现查询，不开启空事务或读取消息；新挑战、新启用群在下次周期发现。

最终发布源码冻结在 `release-v55-r5/source`。r4相对r3只修改两个压测夹具，
r5相对r4仅增加广播通知兼容和两份对应测试，详见下节。客户业务、解码器、
两个消费者和前端与r4字节相同；r5重新构建API并全量复跑发布检查。

| 检查 | 结果与范围 |
| --- | --- |
| r5 Go race／sodium | 19包、379个顶层测试通过，90条件跳过；51.133秒 |
| r5串行隔离PostgreSQL | HTTP 196.114秒、control 85.734秒，151个顶层测试通过、2跳过；gateway 18.254秒，70通过、2跳过 |
| 空闲恢复专项 | 两平台关闭态发现、新启用及接入挑战测试通过；没有真实原生调用 |
| 产品前端 | 产品类型检查及构建通过，r1实际构建22.49秒；r1至r5前端源码字节一致，复用同一已测Web镜像 |
| 合成浏览器 | 320／390／768／1440、100／300群通过；证明与转移审核、版本冲突、运行开关、逐动作授权及迟到响应通过 |
| 助手回归 | 双平台10轮历史、唯一空白会话、删除及取消通过；模型和原生调用均0 |
| 架构事实 | r5冻结来源update/check、架构与仓库守卫、53项工具测试、3项渲染缓存测试及9个Mermaid视图通过；根工作区另做事实检查通过 |
| 咨询性分析 | r5匹配DeepSeek可信基线的增量通过，1请求、29745上报Token、unknown 0，持久检查点成功 |
| 最终镜像容器smoke | 23.989秒通过；新网站邀请激活及重放、登录、默认关闭、迁移和既有能力检查通过，真实模型及原生调用0 |

模型只接收冻结白名单源码，未发送运行数据、正文或凭据；r5源码摘要
`43b4ccb3d5c9df9d54d6cf3c5f32db43e5e770345b0d69c9137db005df4d1fe8`。
没有全量回退、重初始化、切换模型或执行远端CI。r3／r4检查也保留，
不以其成功替代已变化源码的本轮检查。

关闭态性能使用与v54相同的领取、取源、解析、密封和写回代码，保持4个worker、
16项队列及4连接解码池。压测进程2核／1GiB，测试数据库1核／1GiB；每个平台、
100／300群各5组交替运行，每个变体预热1000条、测量10000条，热点占50%，
每账号最多30群。对比新增消费者不运行和运行但所有群业务关闭的增量成本，
不作为旧解析优化目标或生产容量验收。

r2首轮旺商聊100群未通过：配对中位数解析P95增加5.06%、排队P95增加6.73%，
吞吐比0.763。修复空闲8次／秒查询后，r3旺商聊100群通过；300群测量发现原有
领取计划受PostgreSQL JIT和后台统计变更干扰，r3主动中止并保留记录。只读计划
诊断总278毫秒，其中JIT约239毫秒、49个函数；完整领取计划为58个JIT函数。
这些是测试库诊断，不是新代码的容量结论。

r4使用确定性账号／群ID、重置合成序列，计时前显式VACUUM／ANALYZE，测量期间
只对 `wsl_test` 的两个消息表暂停后台自动维护，结束恢复原选项；JIT保持on，
生产数据库未修改。矩阵2059.83秒完成：40个变体、400000条测量、40000条预热，
五组配对全部通过本轮关闭态回归门槛。测量路径与r5字节相同，广播分类另做
完整持久回归；未对新通知冒充旧压测结果。

| 平台／群数 | 解码P95变化 | 含写回服务P95变化 | 排队P95变化 |
| --- | --- | --- | --- |
| 旺商聊／100 | -8.18% | -8.72% | -2.87% |
| 旺商聊／300 | -6.15% | +0.24% | -4.16% |
| 辣椒聊／100 | +0.34% | -1.55% | -0.73% |
| 辣椒聊／300 | -1.67% | -2.95% | -2.45% |

以上为配对中位数；关闭态消费者正文读取、原生读取、任务均0。测量进程堆峰值
6764296字节、协程峰值20、队列峰值12。旺商聊300群吞吐比1.5665仍有原有JIT
波动，不能解释为容量提升56%；其他吞吐比约1.000至1.010。本次只验收新增
消费者的关闭态增量成本，不是此前解码优化强目标、真实300群或全天稳定性结论。
首轮失败和中止日志保留，不删除或覆盖；测试表自动维护选项已恢复。

17:22:49仅更新现有Compose的API／Web，发布前未决提交0。最终API镜像：
`sha256:aa3ad71ee82e457a037f064fbde4f7cdbd66e8ce002ff845df914d19b5650c1f`；
Web镜像：
`sha256:de64d02c38f2bbc45855e2299040e911df43efd0a64a35e79d171170c5949fe3`。
环境、挂载、网络、资源限制、只读设置与部署模式一致；热群引擎及PostgreSQL
容器未重建，上一API／Web镜像已保留。050至052没有自动开放准入、启用群业务
或创建接入接受尝试及规则任务。

17:25上线只读API核对两平台各11个群管动作目录，成员管理均限定负责人，
助手／自动规则／接入／热群的抽查准入均关闭；两平台各两个已启用接收器均
observed、connected。真实客户邀请激活、客户本人挑战、正式准入后的助手与
自动规则灰度仍未完成。号池机器人不能代替客户证明，不能用SQL制造该证据。
下一步按 `customer-onboarding.md` 的开通顺序，由真实客户完成证明后，再逐平台、
动作、来源开放指定测试群灰度。`native_ready`、`ai_ready`仍false。

工作区预检日志位于 `var/customer-v55-20261105/precheck/`，冻结候选及各轮证据位于
`var/customer-v55-20261105/release-v55-r1` 至 `release-v55-r5`。交付后文档独立归档，
不修改已测构建源码。真实浏览器验证邀请弹窗320／390／768／1440可用，关闭弹窗
未创建邀请，写请求和运行错误均0。

## 旺商聊双账号广播通知阻断修复（2026-11-05）

发布前发现林夕／test与大蛤／测试受控1均在08:05:20 UTC暂停，错误
`native_push_boundary_unknown`，协议包 `7/17/1299`。1299是正文长度，返回码200，
不是登录失败。固定桌面2.8.3 SDK将其定义为 `msg.broadcastMsg`，完整单Property，
来源及只读形状核对见官方研究E50。

补丁只增加精确7/17路由和完整边界校验，密封保留notice；不解释广播内容、
发起HTTP或模型、增加聊天消息或ACK，不采用其时间戳更新水位。未知及畸形包
仍隔离暂停。直接及四种封装、逐字节截断、重复、尾部、上限、空Property、
非成功码和相邻命令回归通过；隔离PG验证无聊天唤醒或水位变化，畸形包阻止
后续水位。应用消息算法、`CodecVersion=7`及发送授权均不变。

17:23:27起通过既有接收配置API分别进行一次带revision恢复：
林夕28→29，大蛤11→12；没有SQL修改开关、授权、角色或水位，没有发送消息、
群管动作、SMS或新业务登录。17:23:31／17:23:36完成首次同步，约60秒八次采样
均observed、connected，心跳持续更新，队列、新隔离和重连均0。
林夕408条消息／408次ACK写出，大蛤100条／100次ACK写出；ACK只表示既有socket
写出，不是上游业务回执。

原隔离事件192903、192904继续为quarantined，部署及恢复前后在内存中直接比较
密封字节完全一致，没有删除原证据或手动跳水位。17:25真实只读浏览器确认两个
账号工作台在320／390／635／768／1440均显示接收正常，无阻断提示、横向溢出、
写请求或运行错误。截图为 `output/playwright/customer-v55-receiver-*.png`，
恢复与最终观测在r5交付目录。此短时恢复不代替全天稳定性、其他格式或业务验收，
历史失败统计不会因恢复连接而清零。
