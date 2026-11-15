# 官方客户端与开户契约核验

核验日期：2026-10-25（Asia/Shanghai）。本记录用于原生注册与登录互操作，
不表示已经注册账号、发送验证码或通过真实平台验收。

当前生产策略已改为桌面独占。E5–E18 为历史 Android 研究，移动适配器已移除；
尤其 E17 的时间型回调扩展不适用于桌面端。当前桌面依据见 E21–E25。

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

## 2026-10-26 消息认证与桌面端补充

- E17：固定 APK 的 `BaseViewModel.java` 回调认证分支构造
  `LoginInfoBuilder(nimId, nimToken, 2, t45.INSTANCE.getNowDatetime())`，
  动态分支构造类型 1 和空扩展。`t45.java::getNowDatetime` 使用设备默认时区的
  `yyyy-MM-dd HH:mm:ss`。`com/netease/nimlib/push/a.java` 分别序列化属性
  115（认证方式）、116（扩展）、1000（令牌）。Go 补齐回调扩展，
  按当前部署 Asia/Shanghai 生成；不据此声称回调服务已经接受。
- E18：业务 protobuf 的动态票据枚举是 `NIM_LOGIN_KIND_DYNC`。
  Go 保留已审阅夹具 `DYNAMIC` 拼写兼容，同时识别官方 `DYNC`，
  对未知、空或 null 类型拒绝，不静默降级静态认证。
- E19：公开 macOS x64 下载地址为
  `https://dload0828.023kjgs.com/wangliaomacpc/x64/wangshangliao_mac_2.8.3_x64.dmg`。
  长度 `135615540` 字节；SHA256
  `0d1884623895d0b593982296133cfb53ad3aef0b2865cf73d4464e46eda6889a`。
  本地 MD5 与 HTTP Content-MD5 `n6OgQ1Wquu66F5xRa3x9Lg==` 一致。
  只读挂载后 `codesign --verify --deep --strict` 成功，bundle ID
  `com.wang.wangbuinesschatpc2`、版本 2.8.3；没有独立发行证书指纹比对。
- E20：仅静态提取 `app.asar`。`dist-electron/main/index.js` 的
  `XClientServer` 从原生 `xclient/mclient` 模块调用 `xtask` 进行请求，
  `dist/assets/business-1505a688.js` 中 NIM 属性表包含 115/116。
  未运行该应用或加载原生模块。不能从存在桌面下载包直接推导完整业务协议兼容，
  更不能将 Android 会话重标为桌面会话。真实桌面登录与手机共存仍未验收。

## 2026-10-26 独立桌面配置与真实匿名请求

- E21：E19 的只读 DMG 内 `mclient` 是 ARM64 Mach-O 可执行文件，
  不是 Node 模块；静态 SHA256：
  `d1d9b4bc4802d8fe413b65aafcbb1261b6e786f92e9cd4f43c17f0fcaa183927`。
  根公钥来自文件偏移 `0x28e8c8`，验证函数 `0x100003980`
  在 `0x1000039ec` 引用它。`xclient`（x64）SHA256：
  `32a7060354b77c51d5ffe90c1599eda5fd92dbd494d45dcc9a833c0f380559ce`。
  仅复制和静态反汇编，未执行任一可执行文件、Node 模块或应用初始化。
- E22：主进程 `index.js` SHA256：
  `53e2313e46cb630924772eb796447db0474f03123d0f5c2c097d8e4a11d73e49`。
  从唯一 `xinit` 参数读取签名引导，使用 E21 桌面根验证两级证书及内容签名，
  按原始 AEAD/LZ4 解包，限定环境/线路组 `1/1`。
  业务域名只从签名字段 6 的目录取得，不使用下载页域名或 APK 配置。
  `mclient` 的 `0x100003e78..0x100003e88` 将证书 A 的
  `0x1003707d0` 公钥传给 `0x100280f14` 做 Ed25519 到 Curve25519 转换，
  结果为业务交换服务端公钥；不是把 Android 编译常量直接复用。
- E23：`mclient` 的 `0x10001f72c..0x10001f758` 固定
  `X-Device=1`、`X-Client=10`；交换版本为 `1770619133615`。
  与锁定 AstrBot 的 Windows `X-Client=11` 分开，通过显式
  `desktop_platform` 选择，未知平台拒绝。桌面配置 JS
  `config-4f66a617.js` SHA256：
  `3527939f13a6eea9d5ee7b49e37240e40414ac994f6b820c6168301b732019d9`。
- E24：桌面 `zh-cn-722bac00.js` 的 `Hb(kind, token)` 将
  `CALLBACK/THIRD` 映射为 `authType=2, token=token, loginExt=token`；
  这替代 E17 的历史 Android 时间型扩展。NIM 初始化实际调用位于
  `index-9b511940.js`，SHA256：
  `ab45bc728e778f16e12ab0623ee86e14de3e321a6b0d70e7501dcb728dcf9ed5`，
  `getInstance` 使用独立 IM AppKey。提取工具只输出到 0600 文件，不打印这些值。
- E25：桌面注册 UI 在认证前读取 `/v1/settings/get-sys-cfg`，
  参数 `{}`，检查布尔字段 `enableYidunVerify`。经超管 API 版本化导入桌面配置后，
  2026-10-26T13:29:49.622880836Z（北京时间 21:29:49）明确触发一次此匿名请求，
  当前 Go 桌面签名/加密请求取得 `code=0` 的可解析成功结果，
  `enableYidunVerify=true`。没有发起账号注册、短信、登录、换票或群操作。
  该项证明真实匿名业务请求兼容，不证明交换服务端公钥已通过实际登录挑战，
  更不证明长连接、手机共存或整体原生能力验收。

### 2026-10-26 21:42 桌面最新版复核

用户提出当前业务终端可能过旧，因此重新读取官网入口、中转配置、公开发布 API，
并额外核对两个平台的自动更新清单，未使用 2026-10-25 的缓存结论代替实时查询。

- E26：`https://www.wwtalk.site/` 当次中转配置仍指定
  `https://wldl.ynesor.com`。其公开
  `POST /api/v1/home/version-release/get-list`（`page=1,size=20`）
  返回 Windows/macOS `minVersion=maxVersion=2.8.3`；
  Windows 更新时间 `2026-10-16T16:03:41Z`，macOS
  `2026-10-16T16:03:17Z`。同一响应的 Android 为 `3.7.4`，
  不是可用于桌面端的版本号。
- E27：官方 Windows 自动更新清单
  `https://dload0828.023kjgs.com/wangliaopc/latest.yml`
  当次返回 `version: 2.8.3`，
  `releaseDate: 2026-10-15T17:48:30.665Z`，
  安装文件 `wangshangliao_win_online2.8.3.exe`，114075330 字节。
- E28：官方 macOS 自动更新清单
  `https://dload0828.023kjgs.com/wangliaomacpc/x64/latest-mac.yml`
  当次返回 `version: 2.8.3`，
  `releaseDate: 2026-10-15T17:53:24.920Z`。
  清单包含 E19 使用的 x64 DMG，135615540 字节，以及 arm64 安装包。

截至上述核对时刻，这两个官方渠道都未提供更高桌面版本。不能把 Android 的
3.7.4 标为桌面版，也不能仅修改界面版本号宣称完成协议升级。
如果另有更高版本或非公开更新通道，须取得具体官方地址/安装包后独立核验。
本次未更改认证版本白名单、现有设备身份、密钥、原生凭据或接收开关，
也没有通过登录请求探测版本差异。

## 2026-10-26 22:21 桌面消息连接与通知契约

- E29：E19 固定桌面包的 `dist/assets/business-1505a688.js`，
  SHA256 `0cd11e895d8bbed2591cb4e5c434c4ea6c3dbcbf4e1dbf388839243560212b86`。
  内置 SDK 的 packetConfig 明确列出：
  `14/2 pushEvent = Property`、`14/9 pushEvents = PropertyArray`、
  `4/25 syncSessionReliableInfo = Property context + PropertyArray datas`，
  以及 `7/116 syncMarkSessionAck = Byte scene + String to + Long timetag`。
  后者还可能封装在 `4/1` 的通知头内。它们不是聊天正文、消息 ACK 或全局恢复水位。
  Go 对这些路由校验完整结构、数组上限和尾部边界，再加密保存为 notice；
  未知路由和畸形数据仍然隔离并暂停，不能以“忽略系统包”绕过。
- E30：本次真实服务器的 `4/1 → 7/116` 已读同步在 SDK 三个字段后另有
  固定 8 字节尾部。只读解封已隔离包核对结构，没有输出正文、身份或凭据。
  Go 仅兼容基础结构或额外一个 8 字节字段，保留完整原包，不推断额外字段含义，
  不把已读时间或尾部当作本接收器的恢复水位；其他尾部长度仍拒绝。
  合成测试覆盖两种长度、嵌套通知、截断、超长和未知内部路由。
- E31：用户本轮人工完成桌面登录后，明确要求连接完善，随后通过既有超管
  CSRF/revision 接收开关启用连接；未提交原生密码登录、短信、注册或群动作。
  首次真实握手和心跳成功后依次暴露 E29/E30 的三类未适配通知。
  补齐前已完成 913 条真实消息落盘与 ACK 写出；这些失败样本保留为旧隔离证据，
  没有删除或改写为成功。最终版本于北京时间 22:21:01 建立连接，
  22:21:02 完成首次同步；22:22:40 观测为 connected，累计 187 条收包、
  187 次 ACK 写出、1 次重复投递、0 次重连、0 个本轮隔离包、队列深度 0。
  该时点最近样本落盘 P95 18.491ms，ACK 写出 P95 20.544ms；
  属于本机单账号实际负载观测，不是 100 条/秒压测或服务端 ACK 处理证明。
  群映射、应用消息解密、业务执行、发送及长期稳定性仍未完成验收，
  `native_ready=false`、`ai_ready=false`，原生业务动作开关未开启。

## 2026-10-26 应用消息与只读目录

- E32：固定 AstrBot `a672ef9` 的 `wire.py` 提供应用层 envelope、原始
  8 字节 nonce ChaCha20-Poly1305、LZ4 长度前缀、签名包和 protobuf
  结构。Go 使用相同算法，通过受控 libsodium 绑定调用，不替换为 IETF nonce。
  合成向量覆盖压缩、签名、错误密钥、认证头不匹配、尺寸边界和畸形输入。
- E33：固定桌面 `zh-cn-722bac00.js` 的 `api.common.Message.createdAt`
  为 `google.protobuf.Timestamp`（字段 3、wire type 2），不能全部按旧版
  int64 解释。现保留两种明确类型，验证 seconds/nanos 后转换为毫秒。
  固定桌面项目根能认证本次真实应用消息；已有 2.8.3 配置缺少独立
  `message_key` 时按该版本契约使用 project root，新离线配置显式写出此字段，
  不通过循环尝试密钥接受失败包。真实诊断仅输出分类型计数，不输出正文或密钥。
- E34：E29 的固定 SDK 同时定义 `2/7 multiPortLogin` 为
  `Byte + PropertyArray`，`4/6 syncOfflineSysMsgs` 为 `PropertyArray`。
  同一固定表中的目录、已读及回执通知按各自的数组、映射和标量结构完整校验，
  限制数量并拒绝截断、非法布尔值及多余尾部。真实隔离包经只读解封验证两项结构
  可解析，旧证据不删除；通知既不产生猜测的会话，也不推进行为/恢复水位。
- E35：固定 `/v1/group/get-group-list`、参数 `{"v":"0"}` 返回账号群目录。
  2026-10-26 本机已取得 6 群快照。群业务 ID 和传输 team ID 同时相符才关联
  显示名称，目录角色不等于实时群管授权。私聊展示仅覆盖已接收消息中的会话，
  包含本人发出的同步消息，不宣称已获取服务器全部联系人或历史私聊。
  只读解码进展与最终样本见 `docs/test-report.md`，原生/AI readiness 仍为 false。
- E36：E29 固定 SDK 的 `6/31 link.notifyDetect` 及
  `6/37 link.binarySocketDetect` 都只有一个 Property 响应。
  本次真实 `6/31` 隔离样本为 34 字节，按该结构可完整解析；
  Go 只保留通知，绝不跟随其中的 URL、执行探测命令或上传主机信息。
  完整/截断/多余尾部的合成边界测试均覆盖这两个明确路由。

## 2026-10-27 成员同步通知阻断

- E37：只读解封本地隔离包 `4/1/184`，内层 `8/119/200`，
  正文170字节；仅记录路由、长度和字段长度，不输出资料、正文、账号身份或凭据。
  已审阅桌面 `business-1505a688.js` 定义 `8_119` 为
  `team.syncUpdateTeamMember`，响应是单个 `Property teamMember`；
  文件SHA256为 `0cd11e895d8bbed2591cb4e5c434c4ea6c3dbcbf4e1dbf388839243560212b86`。
  Go新增严格边界校验后可识别同一隔离包，并将其保留为notice，不生成消息、
  角色授权、ACK或恢复水位。截断、额外尾部及未知相邻路由仍拒绝。
  候选修复不自动清除原blocked记录，也不代表运行镜像已更新或连接已恢复。

## 2026-10-28 桌面注册参数复核

- E38：复查已固定的桌面2.8.3 `Register-03d04bd2.js`，SHA256
  `d918ea07a42d2a3d9d9c8dee1fe2ac2b7c0ce54ec4a765ad23e8bfa76a45ca17`。
  `Ce`的短信参数为`ty=VERIFY_FOR_REGISTER`、`phone`及`validateStr`，没有大写`Key`。
  `phone`中`countryCode="86"`、原手机号和前三/后四掩码均为字符串。
  `ze`将同一个phone对象连同`key`、`verificationCode`、`password`、
  `validateStr`提交注册，没有`nickName`。这替代E6/E7历史Android界面对当前
  桌面注册参数的误用；没有改动登录、加密、nonce、签名或交换算法。
- E39：同一固定版本`zh-cn-722bac00.js`，SHA256
  `53c3d0aab8c98009ac62beb0576c88361c774a77162989c767f4c4c17880e38c`。
  `TO.handleReq`先区分原生客户端结果`t.code`和HTTP200响应内的业务`e.code`；
  客户端层1001的“解析错误”不能直接套用到本项目业务JSON中的1001。
  现有最小开户记录只证明注册被明确拒绝，不含原始上游msg，不能逆推出手机号、
  密码、短信或人机验证的具体失败原因。只按调用步骤及精确安全词表分类。

本次仅静态复核已有公开客户端副本，并只读核对开户状态/安全原因码；没有执行官方
客户端、开户、短信、登录或修改既有开户记录，也未声称本次注册参数已通过真实平台验收。

### 2026-11-01 注册四输入项与协议确认复核

所有者反馈所见注册页需要昵称、手机号、验证码、密码及用户服务协议确认，
尚未提供该界面的客户端平台和版本，不能直接认定为当前桌面契约变化。
本轮静态读取E9对应`activity_register.xml`，确认Android3.7.4存在
`et_nickname/et_mobile/et_code/et_password`及`crb_agreement`；
E7的`UserRegisterRequestBean`也确有`nickName`。

重新计算E38的桌面注册脚本SHA256，与上述固定摘要一致。其`Ce`申请短信，
`ze`提交注册；注册参数仍为`key/verificationCode/password/validateStr/phone`，
脚本没有`nickName`或`nickname`，表单是手机号、验证码和密码。
协议状态`_e`默认false，由复选框更新，注册按钮及`Ve`入口检查该状态；
协议确认是客户端检查，没有另加到这两个原生请求的参数中。
这仅证明固定桌面2.8.3的代码，不能代替其他版本的实际请求证据。

当前`NativeLogin.vue`已有未预选的协议确认，`Manager.Action`对三个注册步骤
均检查`agreed=true`。短信请求不读取注册昵称或密码，因此已知注册短信阶段1001
不能仅因完整注册表单包含昵称而定因为漏传昵称；真实拒绝原因仍未确定。
本轮只读核对本地客户端资源和源码，没有运行客户端、执行网络注册/发码/登录、
修改产品请求、补造历史原因或重新部署。后续若所见界面来自不同桌面版本，
需先取得对应版本的注册契约再决定适配，不把移动字段直接并入现有桌面请求。

## 2026-11-01 注册短信拒绝诊断静态复核

- E42：固定Android安装包内`api/common/c_errorcode.proto`的SHA-256为
  `28fb47b92e0d69fdd2d1efc53c776ce398bfcf15c27fd2c3dcd725fbb512fd22`。
  这是共享服务API定义的离线证据，不是现行桌面服务的一次真实发码结果。
  其中1001为DEFAULT_CODE；1000/1002为参数错误/缺失，1036为验证码无效，
  1037为手机号已注册，1038为短信频控，1039为手机号未注册，1040为行为安全
  验证失败，1043为请求频繁，1044为验证码失效，1200为版本低，1201为IP受限。
  没有“1001等于号段限制”的定义。
- E43：同包`api/core/verify.proto`的SHA-256为
  `f0034bf1b9c59cbbaa11c3587775ee1bc7fc96047f85c910496655ce323828e4`，
  `SMSAnonRequest`明确携带用途ty、phone与validateStr。E38固定桌面注册也使用
  `/v1/verify/sms-anon`和VERIFY_FOR_REGISTER，E39桌面handleReq继续区分客户端
  传输状态与HTTP200内部业务code。桌面资源未找到独立业务码字典或号段准入表，
  因此诊断只对固定认证路由/短信用途应用共享码定义，并标明诊断来自业务码
  或安全词表；不会将客户端1001“解析错误”移植成业务1001的原因。

本次未请求真实短信、注册、登录或探测手机号。对于历史1001未识别msg，
旧版本已丢弃原文，无法回溯断定是号段、频控或安全验证。新诊断仅输出固定
分类，区分空原因与非空未识别原因，不存储原始msg、号码或验证码。
专用码映射经过合成回归，实际服务语义及短信发送仍需用户手动验收。

### 桌面动态注册限制提示复核（2026-11-01）

- E44：读取转交的本地`registration-hint-mapping-20261101.md`后，重新核对E38
  `Register-03d04bd2.js`、E39`zh-cn-722bac00.js`和E42共享proto，
  SHA256分别仍为`d918ea07a42d2a3d9d9c8dee1fe2ac2b7c0ce54ec4a765ad23e8bfa76a45ca17`、
  `53c3d0aab8c98009ac62beb0576c88361c774a77162989c767f4c4c17880e38c`及
  `28fb47b92e0d69fdd2d1efc53c776ce398bfcf15c27fd2c3dcd725fbb512fd22`。
  桌面业务失败直接通过`Nl.warning(e.msg)`显示服务器文案；因此能出现动态号段
  提示，不代表本地维护“1001=号段限制”的固定表。
- E45：重新扫描固定桌面dist和dist-electron中的127个JS/JSON/HTML文件，
  共12790126字节，并解码`\uXXXX`后检索。未命中“号段”或“不允许注册”；
  “不支持注册”仅命中音频SDK的“当前流已经在混音中，不支持注册插件”。
  此结论只适用于该固定包，不覆盖后续版本或任意服务器动态msg。

转交截图对应未登录的测试面板，显示注册短信阶段及1001，但没有原因分类或
诊断版本，不能据截图证明响应字段缺失；截图的旧短信文案不在当前运行的`index-Dfj6goHN.js`中。当前浏览器加载
该包，线上内容SHA与已验证交付资产一致；这只能核实当前页面，无法反推截图时
加载的包或原始上游文案。历史原因已丢弃，不能补录或断言为空/号段受限。
新增纯合成贯通回归验证HTTP200业务1001从真实客户端解析、事务到安全诊断记录：
空、缺失、未识别文本、明确号段、明确号码、非文本六分支准确分开，轮询和关闭不重发。
本轮未发送真实短信、查询接码凭据或调用原生注册/登录。

## 账号头像、改密与换绑静态契约（2026-10-29）

复用固定桌面2.8.3离线副本，没有执行客户端或调用真实账号。
`index-9b511940.js`的`ModifyPwd`先发送`VERIFY_FOR_RESET_PASSWORD`短信，
调用`verify/verify`验证，再以`verify.pwd.passwd`与`newPassword`提交。
`CardDialog`的`setAvatar`传`{id}`；`uploadFile`要求`file/thumbnail`、
`asset_type=1/use_for=3/width/height`，JWT只在上传`token`头，完成后还调用
`user/image-check`。签名bootstrap已离线验签，字段8首条上传线路是
`authupapi01.hfjtqy.com/v1/assets/userupload`；Go仅使用该固定线路，不复制
客户端的线路竞速、任意URL或失败重试。

| 离线证据 | SHA-256 |
| --- | --- |
| 桌面`index-9b511940.js` | `ab45bc728e778f16e12ab0623ee86e14de3e321a6b0d70e7501dcb728dcf9ed5` |
| 桌面`uploadFile-4c116263.js` | `d189367d29dd36ca9f5883ae533b7f9a87f7ed705f2780cae0f751ef2e59c0bf` |
| 桌面`CardDialog.vue_vue_type_style_index_0_lang-bdb36666.js` | `a98ffe71d0cbefc62c9a242be64da19cf5c2e3d6c7c5f78f837c7bf87b0b3e56` |
| 官方`api/core/user_settings.proto` | `415e995ee3c337c1c377ed450ef650dea349cfe52af762df447fc2fb3cb3b0d6` |
| 官方`api/common/c_verify.proto` | `76009a51d8de707869a7bdc9e8fa314c83c8fcf0bd4b1301eec9b1d4afadf3a5` |

桌面设置页未提供手机号换绑入口。换绑依据官方包内API定义，使用
`VERIFY_FOR_CHANGE_PHONE`核验旧手机、`VERIFY_FOR_RESET_PHONE`给新手机发码，
向`settings/changePhone`传`phone/newDeviceKey/newDeviceCode/cert`。
旧schema曾注释“暂时不用”验证旧手机，但Go仍要求完成验证，不据此省略。
这不是恢复移动登录适配器；它沿用当前桌面会话、签名与nonce分配。
尚无真实桌面会话的换绑验收结果，不能从字段匹配推断实际服务接受。
所有新能力维持候选/待验收，详细流程见`native-settings.md`。

## 好友审批、邀请与退群静态契约（2026-10-29）

固定桌面`zh-cn-722bac00.js`路由表与`index-0329ac55.js`好友页确认：
列表请求`lastId="0"`，处理请求携带`flId/state/nickName/classId/friendCircle`；
`flId`是int64申请记录，不能用申请人UID替代。申请方向与状态对应官方
`c_friend.proto`，仅接收方向的待处理申请可执行同意／拒绝。

`groupSetting-87c11629.js`及官方`group.proto`确认邀请为
`add-group-member(groupId,groupMemberIds)`，平台自行判断直接加入或发送邀请；
退群为`leave-group(groupId)`，没有复制解散群或踢人逻辑。
本项目采用单目标邀请、最新完整成员与好友核验及独立人工确认。
证据哈希、作用域、只读对账和真实验收缺口集中记录在`native-social.md`。
本次没有执行桌面客户端或向任何真实账号提交好友／群操作。

## 2026-10-30 回执与跨端删除标记通知

- E40：复核 E29 的固定 `business-1505a688.js`，SHA-256 仍为
  `0cd11e895d8bbed2591cb4e5c434c4ea6c3dbcbf4e1dbf388839243560212b86`。
  `packetConfig["7_12"]` 为 `msgReceipt / Property msgReceipt`；
  `packetConfig["7_124"]` 为 `onDeleteMsgSelfBatch / PropertyArray deleteMsgSelfTags`。
  本地只读诊断观察到封装于 `4/1` 的两类隔离包，内层正文分别为 88、121 字节。
  复现入口为 `go run -tags sodium tools/native_payload_probe.go`，
  只输出形状、长度与识别结果，不输出正文或身份。
  Go 按这两个固定结构验证完整边界后仅密封保留通知，不删除本地历史、
  更新操作结果、生成聊天 ACK 或推进恢复水位。合成测试覆盖直包、封装、
  所有截断前缀、多余尾部、数组上限和相邻未知路由。

## 2026-10-31 好友操作在线通知

- E41：官方 npm 包 `nim-web-sdk-ng@10.11.5`，归档 shasum
  `617fe6345ade88195a02808e8743219b3cedbd24`；`dist/esm/nim.js` SHA-256
  `5fa9ed737c42b38ee934dc3a236a036ceff239d871cbbc289c8dde7a7834717b`。
  固定桌面 SDK 没有 `4_31`，因此另核对这个精确版本的公开 SDK，不修改桌面登录版本。
  路由表 `4_31` 映射 `v2FriendOperationListOnline`，声明
  `sid:4,cid:31,response:[Property data]`（字节偏移 694558）。
  其映射包含申请人/接收人/操作人、状态、时间、附言历史和服务端编号。
  本地只读隔离包为 144 字节，一个完整 Property，字段编号 1–9，长度依次为
  10/10/10/1/1/13/13/54/13；不记录身份、附言或原始正文。
  这证明通知的外层边界，不代表好友审批业务已验收。

Go 对 `4/31` 使用有界 Property 读取和完整消费检查，重复字段、截断、超限及尾部
仍拒绝。仅密封保留通知，不应用好友状态或调用好友接口，不把字段时间当成
`5/1` 全局恢复水位，也不生成聊天 ACK。未知相邻命令继续隔离暂停。
运行原始密封证据与水位不删除、不重写；恢复由已有版本化接收配置完成。

## 2026-11-01 本人处理入群邀请

- E44：固定桌面 `index-254d77a7.js` SHA-256
  `ece64278fcb3f6fa1019b31793b31ca7d826f32457fd76cef5bc0259cb75a38a`。
  邀请处理页向固定 `groupMemberInvite` 包装器传 `applyId/agree`，并用回包 `state`
  更新该记录；路由表与 `groupSetting-87c11629.js` 对应
  `/v1/group/group-member-invite`。既有 `api/core/group.proto` 定义
  `GroupInviteMemberHandleRequest/Response`，响应包括 `new_add/state/group_cloud_id`。
  它与管理员审核申请、邀请好友、修改全局邀请隐私分别处理。
  Go 仅接入同意本人待处理邀请；静态契约不能证明实际入群，需独立成员目录核验。

## 2026-11-02 本人资料更新同步通知

- E45：固定桌面 `business-1505a688.js` SHA-256 仍为
  `0cd11e895d8bbed2591cb4e5c434c4ea6c3dbcbf4e1dbf388839243560212b86`。
  字节偏移 650614 的 `packetConfig["3_110"]` 声明
  `user / syncUpdateMyInfo / response:[Property user]`。
  本次昵称修改恢复均收到持久成功回执，随后接收器隔离了 138 字节的 `4/1` 封装；
  本地只读解封确认内层 `3/110/200`、正文 124 字节，单个完整 Property。
  字段 1、2、10、11、13 的长度分别为 10、8、70、12、13；未导出字段正文。

Go 仅验证该精确路由的有界 Property 和完整消费后密封保留通知，不应用原生身份、
昵称或头像，不产生权限证明、聊天 ACK 或全局恢复水位。截断、重复字段、
多余尾部、超过字段上限、非成功码和相邻未知路由继续隔离。`CodecVersion=6`
及应用消息解析算法不变；本地隔离原包和原有恢复水位保持原样。

## 2026-11-02 旧群通知与消息展示契约

- E46：本地固定客户端资料`core-protocol.c`中`FUN_0012b6bc`
  （1335–1575行）与`FUN_0012a280`（199行附近）分别表明aad=0的读取与编码
  不执行正文AEAD；随包Java `com/xclient/sdk/crypto/a.java`的
  `parseAndDecrypt`与其一致。四字段fixed64 1/2/3、bytes 4的旧通知
  不能当成字符串字段的`MessagePacket`，也不能伪称已经认证。
  `resources/unknown/common/c_message.proto`的`MSG_KIND_GROUP_SYSTEM=3`，
  与真实旧包结构复核一致；没有复制真实正文或身份到测试夹具。
- E47：同一proto的Message format及oneof字段定义支持群通知106、
  群公告107、游戏112、红包201及提示文本100。桌面2.8.3 SDK将外层5
  定义为notification，100为custom。仅增加只读标签或有界文字展示，
  没有接入红包、游戏业务或从通知推导当前权限。

真实资料只在本机只读事务中解封，输出固定结构和聚合数；
合成夹具独立构造。图片预览策略与消息元数据读取分开，不扩大下载或像素限额。

## 2026-11-05 通话信令通知与接收阻断

- E48：桌面 2.8.3 固定 `business-1505a688.js` 的 SHA-256 为
  `0cd11e895d8bbed2591cb4e5c434c4ea6c3dbcbf4e1dbf388839243560212b86`。
  首组 `packetConfig` 的字节偏移 165233、165332、165446、165567 分别定义
  `15_11 signalingNotify`、`15_12 signalingMutilClientSyncNotify`、
  `15_13 signalingUnreadMessageSyncNotify`、`15_14 signalingChannelsSyncNotify`；
  前两项完整读取一个 Property，后两项完整读取一个 PropertyArray。
  所属服务为 `avSignal`，不是普通聊天或全局同步水位。
- 本地只读核对大蛤／测试受控1 在 `2026-11-03T17:42:38Z` 的一条密封
  隔离事件：外层 `4/1` 的正文长 318 字节，内层 `15/11/200` 的正文长
  304 字节，为完整 Property。旧版拒绝该未登记路由；补丁将原包识别为
  `notice`，消息数为0，未返回 Boundary。仅输出路由、长度与结构判定，
  未导出通话参数、正文、身份或凭据。

补丁只密封保留这四类通知，不接听、加入或发起通话，不发聊天 ACK，不将
封装或属性时间当作恢复水位。合成回归覆盖原生与四种已有封装、截断、重复
属性、尾部、上限、空同步数组、非成功码及相邻未审阅命令。真实恢复继续走
既有带 revision 的接收配置入口，保留原隔离证据，不修改应用消息 CodecVersion。

## 2026-11-05 代理服务通知与接收阻断

- E49：同一固定桌面2.8.3 `business-1505a688.js`，SHA-256与E48一致。
  字节偏移672799、1730778、3729032的 `22_2` 均定义为
  `proxyService.onProxyMsg`，响应完整读取一个 `proxyMsgTag` Property。
  偏移332540的属性表定义 `from=1, body=2, time=3`；其
  `processProxyService` 分支只向已注册回调交付 `proxyMsg`。
- 只读核对林夕／test在 `2026-11-04T17:07:22Z` 的密封隔离事件181878：
  `22/2/200`，正文932字节，完整Property的字段1／2／3长度分别11／900／13。
  没有导出字段值、正文或凭据。`932`是正文长度，不是上游业务错误码。

Go只登记 `22/2`，完成有界Property、判重及整包消费校验后密封保留为通知，
不调用代理接口、不解释或执行body、不触发模型、聊天投影、ACK或恢复水位。
`22/1`及其他未审阅命令仍拒绝。合成回归覆盖直接及四种既有封装、截断、
重复字段、尾部、大小上限、空Property及非成功码；隔离数据库验证持久保留、
不唤醒聊天解码、不推进水位，畸形通知仍隔离并阻止后续水位提交。
运行交付和恢复结果以 `test-report.md` 本轮记录为准。

## 2026-11-05 广播通知与双账号接收阻断

- E50：同一固定桌面2.8.3 `business-1505a688.js`，来源与E48一致。
  SDK的服务表定义 `msg.id=7`、`broadcastMsg=17`，`7_17`响应完整读取
  单个 `broadcastMsg` Property。字段表为 `broadcastId=1, fromAccid=2,
  fromUid=3, timestamp=4, body=5`；分发进入 `onBroadcastMsg`。
  广播正文不是普通聊天消息，也不是同步水位。
- 只读核对两条2026-11-05 08:05:20 UTC的密封隔离事件192903、192904：
  路由均为 `7/17/200`，正文1299字节，完整Property的字段1至5长度分别
  14／5／12／13／1243。旧版拒绝该路由；补丁回放均为notice、聊天消息0、
  Boundary为空。只输出路由、长度和判定，没有导出字段值或凭据。

Go仅增加精确 `7/17` 的完整有界Property校验与密封通知保留；不解释正文，
不请求其中地址、不执行广播动作、不额外发送广播已读或聊天ACK，不采用
其时间戳更新水位。合成回归覆盖直接及四种已有封装、逐字节截断、重复字段、
尾部、大小上限、空Property、非成功码及相邻未审阅命令；持久回归核对通知
不唤醒聊天解码，畸形广播隔离并阻断后续水位。原隔离证据保留，应用消息
算法与 `CodecVersion=7` 不变。发布与真实接收恢复单独记入测试报告。

## 2026-11-07 好友同步通知与核验窗口

- E51：同一固定桌面2.8.3 `business-1505a688.js`，来源与E48一致。
  `friend` 服务编号12，`syncFriendRequest=101`、`syncDeleteFriend=102`、
  `syncUpdateFriend=103`。`packetConfig["12_101"]`依次读取String账号、
  Byte类型、String附言；`12_102`只读取String账号；`12_103`只读取
  一个friend Property。`processFriend`将三者交给各自好友同步回调，
  不是聊天正文或全局同步水位。
- 本地只读核对大蛤在2026-11-07 13:45:41 UTC的密封隔离事件275270：
  外层`4/1/200`正文25字节，内层`12/102/200`正文11字节。
  原始隔离事件继续保留；通知不触发审批、身份或群归属授予。
- 同日林夕的固定好友申请目录一次返回174项，原核验附言读取限100项，
  与普通申请目录既有500项上限不一致。核验改为同样最多500项，
  仍只读一次服务器窗口，按完整本次附言及唯一传入待处理申请匹配。
  历史已同意申请或旧附言不能变成新挑战证据。

协议回归使用合成数据覆盖直接及四种已有封装、截断、尾部、非法UTF-8、
长度上限、重复Property、错误状态和相邻未审阅命令；持久回归检查通知
不唤醒聊天解码、不产生ACK、不推进水位，畸形通知仍阻止后续水位。
普通客户链路与已有好友私聊分支另作合成及真实验收，不由协议通知推导通过。
