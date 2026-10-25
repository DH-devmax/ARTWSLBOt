# 旺商聊管理平台

专属仓库：`https://github.com/DH-devmax/ARTWSLBOt.git`。
Go 控制面、Go 热群聊天引擎、PostgreSQL 租户隔离、Art Design Pro 控制台和私有 Simon 监控。
源码基线固定在 `upstream.lock.json`，不跟随上游 HEAD 自动升级。
生产仅使用 Docker Compose；2026-10-25 起 MaiBot 引擎也迁移为 Go，不部署 Python。
旺商聊机器人、热群机器人各支持最多 10 个账号面板和 100 个授权群；这是准入上限，容量待验收。

**当前交付是可运行的首批基础版本，不是六阶段计划已全部完成。**
原生开户、密码/短信登录及安全码校验已接入代码，并通过离线协议和数据库测试；
Android 3.7.4 已完成官方原生例程的合成向量对照，尚未使用真实账号验收。
长连接、消息全链路和 Go 引擎的完整 MaiBot 行为迁移尚未接通。
真实账号、自动管理动作和 AI 发言均默认关闭，不能绕过验收开关。

## 安全默认值

- 不启用原生账号连接，不自动登录或发送群消息。
- 不创建演示租户、模拟财务记录或预置生产密码。
- 原生操作和 AI 返回都必须经过服务器权限、版本及有效期校验。
- 未完成兼容和群级记忆隔离验收的接入能力保持关闭，不能据此宣称生产可用。

## Docker 部署

宿主只要求 Docker Engine 和 Docker Compose。编译 Go、前端与 libsodium 均在镜像内完成。
`init` 生成权限为 `0600` 的 `.env`，已有文件不会覆盖。
初始网站用户名为 `admin`，随机密码保存在本机 `.env` 的 `BOOTSTRAP_PASSWORD`。
不会预置演示群、模拟台账或原生账号凭据。不要将 `.env` 提交到 Git。

```sh
docker build -f deploy/api.Dockerfile -t wsl-api:local .
docker run --rm --user "$(id -u):$(id -g)" -v "$PWD:/workspace" -w /workspace wsl-api:local init
docker compose up -d --build --wait
docker compose --profile ops run --rm bootstrap
```

控制台与健康检查共用 `http://127.0.0.1:5173` 和 `/healthz`；API、引擎和 Simon 不发布宿主端口。
首次启动自动执行版本化迁移，已有超管时不要重复 bootstrap。
`docker compose ps` 查看独立服务状态，`docker compose stop` 保留数据。
停止旧本机 API/Vite 后再占用 5173；不删除既有 PostgreSQL 卷。
PostgreSQL 仅绑定 `127.0.0.1:55439`。正式对外需配置 HTTPS、`PUBLIC_ORIGIN` 与
`COOKIE_SECURE=true`，详细发布与恢复步骤见 `docs/docker-operations.md`。

## 验证

```sh
docker build -f deploy/test.Dockerfile -t wsl-test:local .
docker run --rm wsl-test:local
docker run --rm wsl-test:local vet -tags sodium ./...
docker run --rm --entrypoint python3 wsl-test:local tools/check_architecture.py
docker run --rm --network wsl-platform_default -v "$PWD/.env:/src/.env:ro" \
  -e WSL_INTEGRATION=1 -e WSL_CONTAINER_TEST=1 \
  wsl-test:local test -race -tags sodium -count=1 ./internal/httpapi
```

PostgreSQL 集成测试只允许重建本机 `wsl_test` 库，不清空开发库 `wsl`。
测试镜像含 libsodium 并运行标签测试；普通无标签构建仍明确拒绝原生登录。
测试通过不等于真实平台协议、目标机器容量或 MaiBot Go 迁移已验收。
`ai/` Python 文件只保留历史契约测试用途，不进入生产镜像。

原生账号入口统一在顶部「注册 / 登录」，仅超管可用；在同一弹窗填写机器人名称并切换登录/注册 TAB，不必先建面板。
已有机器人可从顶部连接状态或工作台状态表重新登录。账号选择仅切换配置面板，不启停连接。
超管通过侧栏 Logo 旁的小按钮切换「旺商聊机器人 / 热群机器人」；
顶部只选当前账号面板，两套账号独立登录。群 AI 申请由旺商聊群管理入口按权限和套餐受理。
热群聊天/模型/表达学习/长期记忆配置可版本化保存；未完成运行能力明确标为待迁移，
完整清单见 `docs/maibot-migration.md`。Simon 指标只经超管 API 投影，不提供其文件管理或原页面。
先通过「协议配置」导入已有部署的受控协议配置，
不需要把密钥或账号密码发到聊天中。机器人面板只是本地管理资源，不是旺商聊开户；原生开户由「新账号注册」TAB 单独确认提交。
空面板可切换到「新账号注册」，使用独立注册短信和人工验证；注册成功后仍需显式登录。
已从用户提供官网核实开户契约，提交前有持久化防重复屏障。3.7.4 使用独立哈希/版本分支，
离线生成器校验固定 APK 内的签名引导数据，再生成仅保存在本机的受限配置。
本机开发库已通过 WebUI 加密导入该配置，不在项目或新部署中预置密钥；
尚未发送短信、注册或登录真实账号，离线对照不代表平台已经接受请求。
详见 `docs/native-auth.md`、`docs/official-client-research.md`。

短时数据库接收回放（同样只使用 `wsl_test`，不得与其他数据库集成测试同时运行）：

```sh
docker run --rm --network wsl-platform_default -v "$PWD/.env:/src/.env:ro" \
  -e WSL_INTEGRATION=1 -e WSL_CONTAINER_TEST=1 -e WSL_REPLAY=1 \
  -e WSL_REPLAY_SECONDS=60 -e WSL_BURST_SECONDS=10 \
  wsl-test:local test -run '^TestReplayLoad$' -count=1 -timeout 5m -v ./internal/httpapi
```

默认只验证 60 秒稳态和 10 秒突发，不等于 60 分钟或 24 小时生产验收。
全新临时数据库与非 root/只读容器检查：

```sh
docker build -f deploy/api.Dockerfile -t wsl-api:local .
docker build -f deploy/web.Dockerfile -t wsl-web:local web
python3 tools/container_smoke.py
```

## CLI

```sh
docker compose exec api wslctl --help
```

CLI 为镜像内的同权限工具；会话路径需指向单独挂载的 `0600` 运维目录，
使用同一套 WebUI 后端权限、CSRF、测试范围及兼容门槛。
原生发送未验收时 `operation-create` 明确失败，不伪造成功。

迁移、生产切换与真实平台测试需要另行配置原生账号和明确的隔离测试范围。
实现与验收进度见 `docs/implementation.md`，真实测试范围与数值见 `docs/test-report.md`。
工程边界见 `AGENTS.md`、`docs/architecture.md`。CI 在 PR/推送运行；
标签发布或手动运行 `docker-release` 会先通过同一套检查，再发布不可变 SHA 镜像到 GHCR。
工作流不会自动登录生产主机或执行原生账号操作。
