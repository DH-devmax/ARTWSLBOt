# 旺商聊 · 辣椒聊管理平台

专属仓库：`https://github.com/DH-devmax/ARTWSLBOt.git`。
面向群主的双平台群管服务：平台维护机器人号池，用户在授权群内使用本项目提供的业务模块和 Web 群管助手，
无需自行开发机器人。原生底座、人工账号操作、业务模块和 AI 建议保持独立边界。

Go 控制面、Go 热群引擎、PostgreSQL 租户隔离、Vue / Element Plus 产品控制台。
产品使用 Fairweather 浅色主题，复用现有 Element Plus X 与 ECharts；
不开放框架演示路由、不嵌入上游 UI。固定依赖来源见 `upstream.lock.json`。
生产仅使用 Docker Compose；Python 只用于开发工具或历史契约测试，不部署生产聊天运行时。

## 当前状态

- 管理和聊天各一个用途池，每池最多 10 个账号，跨旺商聊/辣椒聊共享配额；
  每账号最多 30 个群绑定，管理最多 300 群，聊天仅覆盖同一批授权群。这是准入限制，不是容量验收结论。
- 原生连接、持久收发和多项群管动作已有平台独立的受控记录；完整生产异常恢复与业务闭环仍需单独验收。
- `native_ready` / `ai_ready` 保持 false。保存配置、受控通过、界面更新和服务健康均不能代替生产准入。
- 当前工作区含未发布候选，不能整仓直接重建当作交付。运行来源、镜像和回滚断点见
  [项目检查点](docs/project-checkpoint.md)。
- 四个常驻服务：API、Web、PostgreSQL、热群引擎。Simon 已移除；监控仅覆盖本项目。

## 文档

| 入口                                                                          | 内容                                            |
| ----------------------------------------------------------------------------- | ----------------------------------------------- |
| [文档导航](docs/README.md)                                                    | 按开发、接入、运维和验收组织的完整入口          |
| [架构契约](docs/architecture.md)                                              | 所有权和依赖边界，修改模块前必读                |
| [当前实施](docs/implementation.md)                                            | 已实现、候选和未闭环能力                        |
| [业务模块开发](docs/business-module-development.md)                           | 基于现有原生能力的配置、执行、隔离与测试        |
| [功能目录](docs/feature-catalog.md)                                           | 从 `internal/features` 生成，平台和子项独立统计 |
| [受控 Tools](docs/controlled-tools.md)                                        | 超管隔离测试、期限、预算及未知结果核对          |
| [Docker 运维](docs/docker-operations.md) / [开发](docs/docker-development.md) | Compose、资源、迁移、HMR 与回滚                 |
| [工作区维护](docs/workspace-maintenance.md)                                   | 临时文件、可校验归档和恢复                      |
| [测试报告](docs/test-report.md)                                               | 实测、失败、未达项及实际部署证据                |

## 权限默认值

网站会话确定操作者与租户，服务端从有效群绑定确定机器人。
普通成员仅访问明确分配的群，角色模板不自动授予群范围。
原生最终提交重新核验授权、套餐、绑定、版本、epoch、当前角色、目标及期限。
凭据保持密封；不向浏览器、审计或架构模型输出密钥、密码和票据。

群管助手只查询和提出建议，由本人确认后走控制层；不开放 Shell、Coding、
任意 HTTP、SQL、MCP、插件执行或测试授权。热群引擎只建议文本，不直接发送。
关闭的可选扩展不应加入消息解析热路径。详见 [业务开发](docs/business-module-development.md)。

## Docker 部署

宿主只要求 Docker Engine 和 Compose。编译 Go、前端与 libsodium 在镜像中完成。
新环境先按 [运维手册](docs/docker-operations.md) 准备配置与数据库角色：

```sh
docker build -f deploy/api.Dockerfile -t wsl-api:local .
docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/workspace" \
  -w /workspace wsl-api:local init
docker compose up -d --build --wait
docker compose --profile ops run --rm bootstrap
```

`init` 只生成不存在的 `0600` `.env`，初始网站用户名为 `admin`，
随机密码在本机 `BOOTSTRAP_PASSWORD`。不要覆盖已有 `.env` 或重复初始化已运行环境。
不预置演示租户、原生凭据或模拟财务。正式对外须配置 HTTPS、`PUBLIC_ORIGIN` 和 `COOKIE_SECURE=true`。

已有本地部署保留其覆盖文件和模式：

```sh
docker compose -f compose.yaml -f compose.local.yaml ps
docker compose -f compose.yaml -f compose.local.yaml up -d --no-build --wait
```

本地覆盖的四个常驻服务内存上限合计 1920 MiB；它不是新部署路径，也不是生产容量证明。
控制台为 `http://127.0.0.1:5173`，`/healthz` 转发到 API。
Web 自身可达性使用静态 `HEAD /`，不能用 API 健康替代。
API 和热群引擎不发布宿主端口；PostgreSQL 仅绑定本机端口。
`docker compose stop` 保留数据，禁止为修复测试而删除生产卷或重建 `wsl`。

## 开发与验证

产品源码为 `web/src/wsl`，类型检查使用 `web/tsconfig.wsl.json`；
不得依赖运行演示路由生成的忽略文件。开发模式见 `docs/docker-development.md`：
前端挂载源码 HMR 不会部署后端、执行迁移或激活原生能力；依赖变化须重建开发 Web 镜像。

```sh
docker build -f deploy/test.Dockerfile -t wsl-test:local .
docker run --rm wsl-test:local
docker run --rm wsl-test:local vet -tags sodium ./...
python3 tools/architecture/architecture.py check
python3 tools/check_architecture.py
```

发布前还须通过 race/sodium、串行隔离 PostgreSQL、产品前端构建、架构/仓库守卫、
匹配可信基线的 CodeBoarding 和容器 smoke；测试命令及结果以实际候选报告为准。
集成测试只使用 `wsl_test`，不要并行重建同一测试库。合成回归不登录、不注册、不发短信、
不操作真实群。真实受控验收另用已有自有账号、指定隔离群和持久授权。

CLI 运行在同一镜像和权限模型中：

```sh
docker compose exec api wslctl --help
```

只同步已验证白名单改动，保留资源限制、凭据和上一运行版。
未决提交先核对，未知结果不得自动重发；HMR 或容器健康不提高 readiness。
CI/发布流程不自动登录生产主机，也不代替真实原生验收。
