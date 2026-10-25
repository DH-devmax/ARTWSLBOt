# 官方客户端与开户契约核验

核验日期：2026-10-25（Asia/Shanghai）。本记录用于原生注册与登录互操作，
不表示已经注册账号、发送验证码或通过真实平台验收。

## 来源证据

- E1：用户提供 `https://www.wwtalk.site/`。入口返回 Gateway，通过中转页配置
  `accessDomain=www.wwtalk.site`、`domains=["https://wldl.ynesor.com"]` 跳转至下载页。
  中转域名为动态选择，不应将其硬编码为业务 API。
- E2：下载页标题为「旺商聊，更有趣的交友」。页面自身调用
  `POST /api/v1/home/version-release/get-list`，请求 `{"page":1,"size":20}`。
  本次只读取公开版本列表，不调用任何开户、短信或登录接口。
- E3：2026-10-25 公开版本列表返回 Android `maxVersion=minVersion=3.7.4`，
  包名 `com.android.wangwang`，更新时间 `2026-10-21T16:01:09Z`，
  下载地址 `https://dload0828.023kjgs.com/wangwangOnline374.apk`。
  Windows/macOS 返回 2.8.3，不能据此假定 Android 3.7.4 与旧接入器协议完全相同。
- E4：从上述 HTTPS 下载地址取得 APK，长度 `73335376` 字节。
  本地 MD5 `bc269530ead59d9f41cef696e05b70bc` 与服务器 `Content-MD5`
  `vCaVMOrVnZ9BzvaW4FtwvA==` 一致。MD5 仅作传输一致性核对。
  本地 SHA-256：
  `8f9a1e4483b0dd210b1c2c6e9549776a103af777b08bc04a06252e2fbd91f96d`。
  公开版本列表没有独立发布 SHA-256 或签名证书指纹，不能将本地哈希称为独立的发行方签名认证。

## 分析边界与状态

安装包与分析产物只保存在被 Git 忽略的
`var/research/official-client-20261025/`，不将完整客户端、反编译源码或密钥提交进仓库。
使用静态只读分析，并在有界模拟器中执行选定的纯编码例程；不安装、启动或初始化 APK，
不绕过人机验证，不发送短信或创建真实账号。

## 客户端身份与契约证据

- E5：Apktool 3.0.2 解出的 Manifest 包名为 `com.wangwang.imchatcontact`，
  版本 `3.7.4`、versionCode `337`；入口 Application 为
  `com.android.wangwang.MainApplication`。因此版本列表的 `packageName`
  不能作为实际安装包 applicationId 使用。Android Build Tools 36.0.0 的
  `apksigner verify --print-certs` 成功；签名证书 SHA-256 为
  `3cf2352f6db285679825b12f065efc5c7aac0f6d7467da4ef9500558c442f196`。
  这验证了该 APK 自身签名，不构成与另一可信发布渠道的独立指纹比对。
- E6：`RegisterActivity.onClick` 在发送注册短信和最终开户前分别调用官方人机验证。
  `tp` 对应注册确认分支使用 `VerifyFor.VERIFY_FOR_REGISTER`，而非
  `VERIFY_FOR_LOGIN`；`BaseViewModel.getSmsAnon` 与 `SMSAnonRequestBean`
  确认请求 `phone`、`ty`、`validateStr`、大写 `Key`，注册默认 `Key=""`。
- E7：`b96.USER_REGISTER` 明确为 `/v1/user/register`。
  `RegisterViewModel.A`、`RegisterViewModel$register$1` 和
  `UserRegisterRequestBean` 确认开户字段为 `key`、`verificationCode`、
  `password`、`validateStr`、`phone`、`nickName`，密码字段不是登录使用的 `passwd`。
  手机结构为 `countryCode=86`、数字型 `nationalNumber` 和空 `maskedNationalNumber`。
- E8：`e54` 的注册成功回调读取 `LoginResponseBean.accountId`，显示跳转登录提示；
  `pn3` 对应回调打开登录页。不能把 `accountId` 当作原生 UID，也不能因为响应 Bean
  复用了 LoginResponse 命名而直接宣称已登录。
- E9：解码的 `res/layout/activity_register.xml` 指定昵称最长 20、手机号 11、
  短信验证码 6、密码 16；密码字符集来自 `res/values/strings.xml` 的 `str_pwd_limit`，
  混合规则来自 `vw0.REGEX_PASSWORD`。实现按这些输入边界校验，不自行放宽为 64 字昵称。

Jadx 1.5.6 对完整包报告 188 项反编译错误；没有将「有输出」写成「所有方法反编译成功」。
上述契约来自能正常读取的界面、字段 Bean 和对应请求/成功回调。
后续按下列证据独立核对 SDK/原生编码，不能只替换版本字符串、versionCode 或 appHash。

## 3.7.4 线格式与初始化证据

- E10：`com/xclient/sdk/crypto/d.java` 的交换版本为 `1703240058617`；
  `XClient$initSDK$1.java` 以 `Math.abs(LongHashFunction.murmur_3().hashBytes(publicKey))`
  计算设备身份，`wx3.java` 以同算法计算请求摘要。不能继续套用 3.7.3 的
  `1703240058615` 和 XXH3 请求摘要。Go 使用 Murmur3 x64_128 低 64 位并保留 Java 溢出语义。
- E11：`XClient$executeCall$2.java` 固定 Android `X-Device=3`、`X-Client=2`、
  `X-Version=1.1703240058617.0`，14 字段 metadata 没有更换结构；
  登录和注册均携带 `X-Context`，注册短信不携带。注册上下文证明设备而非用户，
  不代表已经取得 UID、JWT 或 IM 凭据。
- E12：对固定 ARM64 `libcore.so` 的静态分析定位交换编码 `0x2c3a8`、
  解码 `0x2ccdc` 和上下文序列化 `0x2dae4`。业务体仍是原始 8 字节 nonce 的
  ChaCha20-Poly1305，结构为 `MAC(16) || nonce material(16) || ciphertext`。
  AAD 仍由 XXH3_128 处理请求 ID、调用方提供的请求摘要、设备和交换版本；
  只改请求摘要算法，不能把所有 XXH3 一并替换。上下文仍是六字段 protobuf 加 sealed box。
- E13：`libsecurity.so` 的 `security_yntb`（`0x65058`）对完整 `sourceDir`
  （及存在时的 splits）执行 HMAC-SHA256，不是普通 APK SHA-256。
  `bytesToHex`（`0x63870`）确认大写和证书冒号格式；
  本固定 APK 的身份摘要为
  `5D499D6530B3C4DC2787F668CF85F7A676CB45B85E71F3FD8105006D1B732A45`，
  与证书指纹一起组成 3.7.4 `appHash`。工具从固定 APK 读取材料，不提交原生私有常量。
- E14：安装包 DEX 中的引导数据经过两级 Ed25519 证书和内容签名校验，
  再按原始 libsodium KDF、8 字节 nonce AEAD、LZ4 和 protobuf 解包。
  仅选择生产环境/线路组 `1/1`，业务 origin 来自其签名内容；
  生产 server Ed25519 公钥按 SDK 语义转换为 Curve25519。
  `tools/native_client_config.py` 校验 APK 长度和 E4 SHA-256，来源不符直接拒绝，
  输出独立设备身份和 `0600` 私有配置，不打印密钥或发起网络请求。
- E15：`tools/android374_oracle.py` 用 Unicorn 在指令/时间/内存上限内执行 E12 两个编码例程。
  没有运行 `JNI_OnLoad`、网络或客户端初始化；仅由宿主提供内存操作及原始 libsodium 调用。
  合成密钥和固定输入生成 `android374_exchange.json`，Go 逐字节核对交换帧、AAD、
  nonce 及上下文序列化。压缩输入统一为 Go 的合法 Zstd raw frame；
  官方默认 Zstd level 3/checksum 压缩结果不要求逐字节相同。它证明选定格式的离线互操作，
  不证明平台风控或认证路由接受本实现。
- E16：2026-10-25 对所选签名业务 origin 只执行一次根路径 HEAD；
  TLS 校验成功，响应 HTTP/2 404。该只读探测只证明连接/证书可用，没有调用登录、
  注册或短信路由，404 不是业务兼容成功证据。

## 实施结果与剩余边界

已依照 E6–E8 接入 Go 开户状态机和 WebUI：人工验证、注册短信、设置昵称及密码、
提交开户、显示新账号，再显式原生登录。开户提交前落盘，结果未知或记录失败会保留
持久屏障，重启、取消、切换账号不解除它；明确的上游拒绝才允许修正后再提交。
注册过程中不调用登录、不生成 IM 凭据、不发送群消息。

官网和安装包已取得，缺失项不再是下载来源，`native_signup_contract_missing`
已由实际状态机替换。3.7.4 已以独立分支实现并通过上述离线对照，
原三种协议继续通过固定回归测试；本机开发库已通过 WebUI 加密导入生成的配置。
仍需受控手机号及人工验证码完成真实验收。未发送短信、开户或真实登录，
离线格式一致和根路径可达均不能证明当前平台接受认证请求。
