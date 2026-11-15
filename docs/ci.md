# GitHub Actions 检查与发布

本轮只改开发检查和镜像交付。生产仍只使用现有 Compose；
CI 不登录生产、不创建原生账号、不发短信、不操作真实群、不调用付费模型。
本地检查、GitHub 实际运行、CodeBoarding 咨询和生产验收分别记录。

首次推送还同步此前未入 Git 的已部署 v134-r4 公开源码，使远端能复现当前产品。
只允许必要的 CI 阻塞修复和安全依赖更新，不携带运行凭据、现场输出或其他未发布候选。
Go 与容器构建使用 `go.mod` 对应的 1.27.4 修复版本。
Python 检查使用 3.12，与固定 CodeBoarding 0.15.2 的版本约束一致；
可选 APK 测试的 AST 指纹也绑定该解释器，不使用其他 Python 版本刷新跳过清单。
所有作业固定 Ubuntu 24.04，匹配锁定 Playwright 的系统依赖，
避免 `ubuntu-latest` 升级后在测试开始前失败。

## 必需检查

保留分支保护使用的 `foundation` 名称。它始终执行，只有以下九个作业均成功，
且当前 SHA 的必需报告及文件哈希完整时才通过。失败、取消、跳过、旧报告、
缺少覆盖率或缺少任一镜像证据均不能通过。

| 作业 | 内容 | 上限 |
| --- | --- | --- |
| quality | 仓库守卫、显式测试登记、Actionlint、Gitleaks、格式、Staticcheck、Ruff、Python 合成回归 | 30分钟 |
| unit | 先发现 Go 测试，再逐项核对 race/sodium JSON 结果及 atomic 覆盖率 | 45分钟 |
| images | 从当前 SHA 分别构建一次 API/Web，导出带来源标签的镜像归档 | 30分钟 |
| database | 串行 `wsl_test` control、httpapi、gateway；关键集成用例不能按单测策略跳过 | 90分钟 |
| frontend | 独立产品 ESLint、已登记 Node 回归、`tsconfig.wsl.json`、Vite 构建 | 30分钟 |
| browser | 复用已构建 Web 静态资源、合成浏览器用例、同一 API/Web 镜像容器 smoke | 60分钟 |
| security | 固定 govulncheck 源码扫描、CGO/sodium 实际构建的 Go 和产品 JS/TS CodeQL | 30分钟 |
| architecture | 无凭据源码事实、目录同步、事件基线差异、架构守卫和 Mermaid | 30分钟 |
| coverage | 校验并合并单测和三个数据库包的 atomic 覆盖率，报告总量及变更行基线 | 30分钟 |

PR、新主分支提交、手动及复用调用执行完整检查；普通功能分支 push 不重复执行。
新的 PR 提交取消同 PR 的旧运行，发布运行不自动取消。
默认验证 Linux 容器产品；不照搬 Python 产品的三系统矩阵。

## 测试与历史诊断

`tools/ci/registry.json` 明确登记 Python、Node、浏览器及组件测试。
发现逻辑仅检测漏登记、重复和无效分类，不按文件名直接运行未知脚本。
真实站点脚本、退役流程和兼容别名必须说明归类原因。

Go 测试及覆盖率包范围限定为 `cmd`、`internal`、`tools`，不会执行`var`中的冻结旧源码。
本地`third_party`替换依赖的实际编译文件及模块锁也纳入源码指纹，
依赖发现不执行第三方测试。标准库和版本化模块缓存不作为本仓库源码读取；
测试变体不重复登记为产品包。
本地 gopsutil 分支新增的 CPU 数值和内存水位合成边界回归由 unit 作业独立执行，
仅使用临时 proc 文件及内存结构，不读取宿主机指标；其成功收据也是必需制品。
`go-skips.json` 按 profile、完整测试名和源码指纹限制跳过；
单测中跳过数据库用例不算数据库通过。两个可选 APK 用例另由
`python-skips.json` 固定指纹；CI 不下载官方 App，也不提供真实凭据。
未登记的 skip、子进程异常、缺失/重复结果、取消和超时均使检查失败。
Go报告保留退出码、未完成测试名和父测试是否已启动；超时、panic、
runtime fatal及编译盘耗尽只输出固定分类，不保存原始测试正文。
分类仅辅助诊断，不能替代测试终态或将缺失结果判为通过。

`lint-baseline.json` 只容纳精确文件、规则、代码指纹和次数的历史诊断；
新增诊断及已修复却未清理的基线均失败。新 CI 工具、解析/编译失败、
漏洞和扫描器失败不能加入历史豁免。刷新基线是本地人工操作，CI 禁止刷新。
不设置未经测量的覆盖率百分比门槛，不声称已采集前端覆盖率。

`toolchain.json` 固定 Ruff、Staticcheck、govulncheck、CodeQL 的版本与校验依据。
Go 工具核对 module sum/GoModSum，下载的二进制/CodeQL bundle 核对 SHA-256。
所有 Actions 使用完整提交 SHA。Python CI 依赖安装两份带哈希的开发工具锁；
前端、架构和浏览器使用各自冻结锁文件，不改变生产依赖。
秘密扫描保留默认规则；生成架构清单的64位 SHA-256 与迁移清单一样使用精确路径例外，
清单内容另由架构检查校验。其他路径、凭据格式及新增诊断均不能沿用该例外。

## 数据、浏览器与制品边界

数据库作业先用本次验证的 API `init` 生成私有合成配置，再显式改到新建的
`wsl_test`。拒绝生产库、非本地 URL、重复配置及连接覆盖变量。
三个数据库包在同一作业串行执行；浏览器的 Compose 栈位于另一台隔离 runner。
清理只针对该作业新建的栈，不用于本地生产目录。
生成隔离 overlay 与启动 Compose 分属相邻步骤，确保 `GITHUB_ENV` 已生效；
容器 smoke 显式预拉取 PostgreSQL，并按当前已应用媒体迁移检查隔离约束。
每个数据库包将排序后的完整父测试清单按最多32项分批，使用精确锚定的名称选择器，
保持 `-p=1`、`-parallel=1` 和每批15分钟上限。批次不能跳过或重复；
任一批次失败即阻断后续包。仅在同包全部通过后合并 atomic 覆盖率，
仍交付 control、httpapi、gateway 各一份来源一致的制品。

浏览器用例只使用合成 HTTP；默认拒绝未模拟 API。直连的
`APIRequestContext` 也隔离，测试中的路由不能回落到现有站点。
真实认证 smoke 只使用新建 `wsl_test` 的合成网站会话，
不绕过产品 CAPTCHA、不调用原生或模型。

报告保留14天，镜像归档保留1天。只上传报告、覆盖率、SARIF、架构证据及合成截图；
不上传 `.env`、网站会话、原始日志、完整页面或 trace。Lint 仅保留结构化诊断。
每个作业生成文件哈希收据；`foundation` 校验九份收据及 GitHub 作业结果。
CodeQL 使用已校验哈希的固定 CLI bundle，为 Go 的实际 CGO/sodium 手动构建
和无需构建的 JS/TS 分别提取数据库；两种 SARIF 均须有匹配语言类别及完成证据。
原始 SARIF 保留全部结果。仅两处辣椒聊协议强制的 `passwordMD5/passwordMd5`
操作按已核对的协议契约单独列入审计结果：绑定完整函数哈希、文件、规则、
精确位置和一次出现次数，并关联合成协议测试。它们不用于本站密码存储或核验；
源码变化、其他 MD5、重复或新告警仍阻断，不关闭整条安全规则。
Docker 归档兼容配置 ID 和 containerd 的 OCI 索引 ID，同时验证索引、配置、
层内容、来源标签及加载后身份，不把索引 ID 误当配置内容哈希。

## 发布

`release.yml` 的 tag/手动运行先复用完整 CI。发布作业只下载该运行的镜像和
`foundation` 证明，核对来源 SHA、归档哈希、镜像配置 ID 和构建收据，
不会重新构建 API/Web。实际推送使用受保护的 `release` environment。
手动 `dry_run=true` 只核验和加载，不登录 registry、不推送。
推送后的 registry digest 与源码、配置 ID、归档哈希一并记录。
这里的“发布”是镜像发布，不是生产 Compose 部署。
仓库端`release`环境限制为`main`分支及`v*`标签，与工作流中的完整检查共同生效。

## 付费架构分析

PR 的源码架构检查属于 `platform-checks / architecture`。
`Trusted architecture advisory / model` 仅可从 canonical 仓库默认分支手动触发，
使用受保护的 `architecture-model` environment。
不再另跑一份 PR/普通分支架构工作流。

默认先无凭据执行兼容基线 preflight，再在单独步骤运行增量分析；
缺少匹配基线就阻断，不回退全量。`initialize=true` 只用于另经所有者授权的初始化
及白名单源码披露；本轮 CI 改造不构成该授权。PR 缓存及模型正文不能恢复到可信基线，
摘要还须匹配当前 SHA、run ID 和 attempt。咨询失败不能被旧成功覆盖。

## 本地验证

以下仅运行合成工具检查，不初始化本地数据库或重启服务：

```sh
python3 tools/ci/registry.py
python3 tools/ci/run.py --profile python --output output/ci-local/python
python3 tools/ci/run.py --profile node --output output/ci-local/node
python3 tools/ci/lint.py --check all --output output/ci-local/lint
python3 tools/ci/go_tests.py --profile unit --output output/ci-local/unit
pnpm --dir web install --frozen-lockfile --ignore-scripts
pnpm --dir web run build
```

先在独立 Python 环境安装
`tools/ci/requirements.lock` 和 `tools/architecture/requirements.lock`，
按 `install.py` 安装固定扫描器，并 `npm ci --prefix tools/architecture --ignore-scripts`。
Go/架构命令采用 `GOFLAGS=-mod=readonly`，防止工作区不完整 vendor 影响结果。
同一 Go/覆盖率输出目录不能复用，须为新运行选择新目录。
源码未提交时的本地通过不是对应 GitHub 提交的通过；当前实测与阻塞见
[测试报告](test-report.md)。
