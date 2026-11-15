// Synthetic APIs and CAPTCHA only. Run through run_multi_platform_ui_smoke.mjs.
// No native login, SMS, CAPTCHA provider or friend request may leave this runner.
// prettier-ignore
async (page) => {
  const base = new URL(page.url()).origin
  if (base !== 'http://127.0.0.1:5173') throw Error('Use the intercepted synthetic UI runner')
  page.setDefaultTimeout(10000)
  const check = (value, message) => { if (!value) throw Error(message) }
  const copy = value => JSON.parse(JSON.stringify(value))
  const id = n => `f7900000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const actor = id(1), tenant = id(2), now = new Date().toISOString()
  const accounts = [
    { id: id(10), platform: 'wsl', workspace: 'wsl', name: '手机搜索甲', native_nickname: '原生甲' },
    { id: id(11), platform: 'wsl', workspace: 'wsl', name: '手机搜索乙', native_nickname: '原生乙' },
    { id: id(12), platform: 'lajiaoliao', workspace: 'lajiaoliao', name: '辣椒聊甲', native_nickname: '辣椒原生甲' }
  ].map(account => ({ ...account, purpose: 'management', auth_state: 'authenticated', version: 4,
    connection_enabled: false, runtime_state: 'disconnected' }))
  const deployment = { configured: false, version: 0, origin: 'https://synthetic.example.invalid',
    protocol_version: 'synthetic-mobile', crypto_ready: true }
  const statuses = new Map(accounts.map(account => [account.id, {
    available: false, configured: false, version: 0,
    blocked_reason: 'native_mobile_configuration_missing'
  }]))
  const flows = new Map(), active = new Map(), reads = [], starts = [], submissions = [],
    imports = [], unexpected = [], screenshots = [], holds = new Set()
  let serial = 0, role = 'superadmin', importError = '', authResult = '', held
  const envelope = data => ({ code: 0, data, msg: '' })
  const failure = (route, msg) => route.fulfill({ status: 409, json: { code: 1, msg } })
  const delayNext = (path, method) => {
    let started, release, finished
    const pending = { path, method, ready: new Promise(resolve => { started = resolve }),
      done: new Promise(resolve => { finished = resolve }),
      wait: new Promise(resolve => { release = resolve }),
      started: () => started(), finish: () => finished(), release: () => release() }
    held = pending; holds.add(pending)
    return pending
  }
  const snapshot = account => ({
    account, sampled_at: now, visibility: 'account',
    groups: { authorized: 0, management_enabled: 0, ai_enabled: 0, ai_pending: 0 },
    operations: { queued: 0, running: 0, unknown: 0, failed_15m: 0 },
    inbox: { pending: 0, unresolved: 0, quarantined: 0 },
    performance: { per_second: 0, inserted_60s: 0, series: [] },
    health: { status: 'unobserved', reasons: [] }, backlog: { status: 'empty', reasons: [] },
    runtime_observed: false, receiver: { enabled: false, revision: 1 }
  })
  const handler = async route => {
    const request = route.request(), path = new URL(request.url()).pathname.replace('/api/v1', '')
    const method = request.method(), workspace = request.headers()['x-workspace']
    const mobile = path.includes('native-mobile-'), auth = /^\/accounts\/([^/]+)\/(native-mobile-auth|native-auth)(?:\/([^/]+))?$/.exec(path)
    let data = [], pending
    if (mobile) check(role === 'superadmin' && workspace === 'wsl', 'Mobile setup escaped its admin/WSL scope')
    if (path === '/native-mobile-deployment') {
      if (method === 'GET') {
        reads.push({ path, workspace })
        data = copy(deployment)
      } else {
        const body = request.postDataJSON()
        check(method === 'PUT' && Object.keys(body).sort().join() === 'configuration,version' &&
          typeof body.configuration === 'string' && Buffer.byteLength(body.configuration) <= 60000,
          'Unbounded configuration or unexpected fields reached import')
        imports.push({ version: body.version, bytes: Buffer.byteLength(body.configuration) })
        if (importError) return failure(route, importError)
        check(body.version === deployment.version, 'Configuration import lost its version fence')
        JSON.parse(body.configuration)
        deployment.configured = true; deployment.version++
        for (const status of statuses.values()) {
          status.available = false; status.blocked_reason = 'native_mobile_reauth_required'
        }
        data = copy(deployment)
      }
    } else if (auth) {
      const account = accounts.find(account => account.id === auth[1]), adapter = auth[2], transaction = auth[3]
      check(account && account.workspace === workspace &&
        (adapter !== 'native-mobile-auth' || account.platform === 'wsl'), 'Auth escaped its account/platform')
      if (!transaction && method === 'GET') {
        check(adapter === 'native-mobile-auth', 'Desktop flow unexpectedly read the mobile status contract')
        reads.push({ path, workspace }); data = copy(statuses.get(account.id))
      } else if (!transaction && method === 'POST') {
        const body = request.postDataJSON()
        check(Object.keys(body).join() === 'version' && body.version === account.version,
          'Start included credentials, changed account version or requested signup')
        data = { transaction_id: `synthetic-auth-${++serial}`, account_id: account.id, purpose: 'login',
          status: 'pending', captcha_id: 'synthetic-mobile-only',
          expires_at: new Date(Date.now() + 600000).toISOString() }
        active.set(`${account.id}:${adapter}`, data.transaction_id)
        flows.set(data.transaction_id, { ...data, adapter })
        starts.push({ path, account: account.id, adapter })
      } else {
        const flow = flows.get(transaction)
        if (!flow) return route.fulfill({ status: 404, json: { code: 1, msg: 'native_auth_not_found' } })
        check(flow.account_id === account.id && flow.adapter === adapter, 'Transaction crossed adapters/accounts')
        if (method === 'GET') {
          reads.push({ path, workspace }); data = copy(flow)
        } else if (method === 'DELETE') {
          flow.status = 'cancelled'; data = copy(flow)
        } else {
          const body = request.postDataJSON()
          check(method === 'POST' && body.captcha === 'SYNTHETIC_HUMAN_ONLY' &&
            ['password', 'sms_request', 'sms_resend', 'sms_verify'].includes(body.action),
            'Submission lost its synthetic CAPTCHA or login-only action')
          if (body.action === 'password') check(body.account === 'synthetic-self' &&
            body.password === 'SYNTHETIC_PASSWORD_ONLY', 'Wrong synthetic password input')
          if (body.action === 'sms_verify') check(body.code === '123456', 'Wrong synthetic SMS input')
          submissions.push({ path, account: account.id, adapter, action: body.action })
          data = { ...copy(flow), status: 'working' }
          if (authResult) {
            flow.status = authResult === 'native_mobile_identity_mismatch' ? 'connection_pending'
              : authResult === 'native_mobile_login_response_invalid' ? 'unknown' : 'failed'
            flow.error = authResult
            if (authResult === 'native_mobile_login_response_invalid') {
              flow.failure_stage = body.action === 'sms_verify' ? 'sms_login' : 'password_login'
              flow.diagnostic = 'login_security_flags_invalid'; flow.diagnostic_version = 3
            }
          } else if (body.action === 'sms_request' || body.action === 'sms_resend') {
            flow.status = 'sms_required'
            flow.resend_at = new Date(Date.now() + 60000).toISOString()
          } else {
            flow.status = 'authenticated'; flow.error = ''
            flow.native_uid = `synthetic-uid-${account.id}`; flow.nickname = account.native_nickname
            if (adapter === 'native-mobile-auth') statuses.set(account.id, {
              available: true, configured: true, version: deployment.version, authenticated_at: now
            })
          }
        }
      }
    } else if (method !== 'GET') {
      unexpected.push({ method, path })
      return route.fulfill({ status: 400, json: { code: 1, msg: 'unexpected_mutation' } })
    } else if (path === '/me') data = {
      user: { id: actor, username: 'synthetic-mobile-auth', role,
        ...(role === 'user' ? { tenant_id: tenant, tenant_role: 'owner' } : {}) },
      permissions: ['overview:read', 'accounts:read', 'groups:read', 'claims:create']
    }
    else if (path === '/workspaces') data = { items: ['wsl', 'lajiaoliao'].map(platform => ({
      id: platform, name: platform === 'wsl' ? '旺商聊机器人' : '辣椒聊机器人'
    })) }
    else if (path === '/accounts') data = accounts.filter(account => account.workspace === workspace)
    else if (path === '/native-deployment') data = {
      configured: true, version: 7, crypto_ready: true, login_adapter: 'desktop', supported_protocol: '2.8.3'
    }
    else if (/^\/accounts\/[^/]+\/dashboard$/.test(path)) {
      const account = accounts.find(account => account.id === path.split('/')[2] && account.workspace === workspace)
      check(account, 'Wrong dashboard scope'); data = snapshot(account)
    }
    else if (path.endsWith('/receiver')) data = { enabled: false, revision: 1 }
    else if (path.endsWith('/events'))
      return route.fulfill({ contentType: 'text/event-stream', body: '' })
    else if (path === '/overview') data = { accounts: 2, groups: 0 }
    else if (path === '/user-center') data = {
      subscription: { name: 'PRO', active: true, platforms: ['wsl', 'lajiaoliao'], modules: [], group_limit: 30 },
      groups: [], permissions: ['overview:read', 'groups:read', 'claims:create'], personas: [], claims: [], operations: []
    }
    else if (path === '/user-center/groups') data = { items: [], total: 0, page: 1, page_size: 20 }
    else if (path === '/user-center/usage') data = { call_count: 0, actual_micro: 0, held_micro: 0 }
    else if (path === '/group-binding/context')
      return failure(route, 'native_mobile_search_required')
    else if (path !== '/groups') unexpected.push({ method, path })
    if (held?.path === path && held.method === method) {
      pending = held; held = undefined; pending.started()
      await pending.wait
    }
    try { await route.fulfill({ json: envelope(data) }) }
    finally { pending?.finish() }
  }
  await page.addInitScript(() => {
    window.__mobileSyntheticCaptcha = []
    window.initNECaptcha = (options, ready) => {
      window.__mobileSyntheticCaptcha.push(options)
      ready({ verify() {}, destroy() {} })
    }
    // Exercise generation fences even if a transport ignores cancellation.
    const fetch = window.fetch
    window.fetch = (input, options) => fetch(input,
      String(input).includes('native-mobile-') ? { ...options, signal: undefined } : options)
  })
  const dashboard = () => page.getByTestId('account-dashboard')
  const setup = () => page.getByRole('dialog', { name: '手机搜索认证', exact: true })
  const login = account => page.getByRole('dialog', { name: `${account.name} · 手机搜索认证`, exact: true })
  const open = async account => {
    await dashboard().getByRole('button', { name: '手机搜索认证', exact: true }).click()
    await setup().getByText(account.native_nickname + ' · ' + account.name, { exact: true }).waitFor()
    await setup().getByText('业务搜索会话', { exact: true }).waitFor()
  }
  const ready = async account => {
    await dashboard().getByRole('heading', { name: `${account.native_nickname} · ${account.name}`, exact: true }).waitFor()
    await dashboard().locator('.account-kpis').waitFor()
  }
  const navigate = async (account, console = 'admin') => {
    await page.evaluate(({ actor, account, console }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`, console)
      sessionStorage.setItem(`wsl:workspace:${actor}`, account.workspace)
      sessionStorage.setItem(`wsl:account:${actor}:${account.workspace}`, account.id)
    }, { actor, account, console })
    await page.goto(base + '/#overview'); await page.reload()
    if (console === 'admin') await ready(account)
    else await page.locator('.user-center').waitFor()
  }
  const choose = async account => {
    await page.getByRole('combobox', { name: '机器人面板', exact: true })
      .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option').filter({ hasText: account.name }).click()
    await ready(account)
  }
  const closeSetup = async () => {
    const writes = imports.length + starts.length + submissions.length
    await setup().getByRole('button', { name: '关闭', exact: true }).click()
    await setup().waitFor({ state: 'hidden' })
    check(imports.length + starts.length + submissions.length === writes, 'Closing setup submitted work')
  }
  const closeLogin = async account => {
    const before = submissions.length
    await login(account).getByRole('button', { name: '关闭面板', exact: true }).click()
    await login(account).waitFor({ state: 'hidden' })
    check(submissions.length === before, 'Closing login submitted credentials')
  }
  const authenticate = async account => {
    await setup().getByRole('button', { name: '打开手机搜索登录', exact: true }).click()
    await login(account).getByText('等待登录', { exact: true }).waitFor()
    await login(account).getByRole('radio', { name: '密码登录', exact: true })
      .locator('xpath=ancestor::label').click()
  }
  const fill = async account => {
    await login(account).getByRole('textbox', { name: '账号 / 手机号', exact: true }).fill('synthetic-self')
    await login(account).getByRole('textbox', { name: '原生账号密码', exact: true }).fill('SYNTHETIC_PASSWORD_ONLY')
  }
  const captchaCount = () => page.evaluate(() => window.__mobileSyntheticCaptcha.length)
  const verify = async (index, success = true) => {
    await page.waitForFunction(index => window.__mobileSyntheticCaptcha.length > index, index)
    await page.evaluate(({ index, success }) => window.__mobileSyntheticCaptcha[index].onVerify(
      success ? null : new Error('synthetic-only'), { validate: success ? 'SYNTHETIC_HUMAN_ONLY' : '' }
    ), { index, success })
  }
  const fit = async (dialog, stage) => {
    for (const width of [320, 390, 768, 1440]) {
      await page.setViewportSize({ width, height: 820 })
      await dialog.evaluate(async element => {
        await Promise.all(element.getAnimations({ subtree: true })
          .filter(animation => animation.effect?.getComputedTiming().iterations !== Infinity)
          .map(animation => animation.finished.catch(() => {})))
      })
      check(await dialog.evaluate(element => {
        const rect = element.getBoundingClientRect(), footer = element.querySelector('.el-dialog__footer')
        return rect.left >= 0 && rect.right <= innerWidth + 1 && rect.top >= 0 &&
          rect.bottom <= innerHeight + 1 && element.scrollWidth <= element.clientWidth + 1 &&
          footer.getBoundingClientRect().bottom <= innerHeight + 1 &&
          [...footer.querySelectorAll('button')].every(button => {
            const box = button.getBoundingClientRect()
            return box.left >= rect.left && box.right <= rect.right + 1 && box.height > 0
          })
      }), `Mobile search ${stage} overflow at ${width}`)
      const path = `output/playwright/mobile-search-${stage}-${width}.png`
      await page.screenshot({ path, animations: 'disabled' }); screenshots.push(path)
    }
    await page.setViewportSize({ width: 1440, height: 900 })
  }
  const configFile = () => setup().locator('input[type="file"][aria-label="手机搜索协议配置 JSON"]')
  const selectFile = text => configFile()
    .setInputFiles({ name: 'synthetic-mobile.json', mimeType: 'application/json', buffer: Buffer.from(text) })
  const save = () => setup().getByRole('button', { name: '导入并保存', exact: true })
  const normalMessage = '此机器人的手机搜索尚未配置或认证，请联系平台管理员；已有好友可继续私聊核验'
  await page.route('**/api/v1/**', handler)
  try {
    await page.setViewportSize({ width: 1440, height: 900 })
    const [account, other, chili] = accounts
    await navigate(account)
    check(reads.length === 0, 'Account dashboard eagerly fetched mobile configuration')
    await open(account)
    await setup().getByText('尚未导入手机搜索协议配置，请先由超管完成配置', { exact: true }).waitFor()
    check(await setup().getByRole('button', { name: '打开手机搜索登录', exact: true }).isDisabled() &&
      starts.length === 0 && submissions.length === 0, 'Viewing missing configuration started authentication')
    await fit(setup(), 'setup')
    await selectFile('x'.repeat(60001))
    await setup().getByText('配置文件不能超过 60KB', { exact: true }).waitFor()
    check(await save().isDisabled() && imports.length === 0, 'Oversized file was submitted')
    await selectFile('{"invalid"')
    await setup().getByText('手机搜索协议配置格式、密钥或加密存储无效，请联系平台管理员核对', { exact: true }).waitFor()
    check(await save().isDisabled() && imports.length === 0, 'Invalid JSON was submitted')
    const configuration = '{"origin":"https://synthetic.example.invalid","key":"SYNTHETIC_CONFIGURATION_ONLY"}'
    await selectFile(configuration)
    await save().and(page.locator(':enabled')).waitFor()
    await closeSetup(); await open(account)
    check(await save().isDisabled() &&
      await configFile().inputValue() === '',
      'Closed configuration draft was retained')
    for (const code of ['native_mobile_configuration_invalid', 'native_mobile_configuration_changed']) {
      importError = code; await selectFile(configuration); await save().click()
      await setup().getByText(code.endsWith('changed')
        ? '手机搜索协议配置已变化，请重新读取配置并重新认证'
        : '手机搜索协议配置格式、密钥或加密存储无效，请联系平台管理员核对', { exact: true }).waitFor()
      check(await save().isDisabled(), 'Rejected import remained actionable without a fresh read')
      await setup().getByRole('button', { name: '重新读取手机搜索状态', exact: true }).click()
      await setup().getByRole('button', { name: '重新读取手机搜索状态', exact: true })
        .and(page.locator(':not(.is-loading)')).waitFor()
    }
    importError = ''
    const boundary = JSON.stringify({ padding: 'x'.repeat(60000 - '{"padding":""}'.length) })
    await selectFile(boundary); await save().click()
    await setup().getByRole('status').getByText('手机搜索配置已保存，请单独打开登录完成认证', { exact: true }).waitFor()
    check(imports.at(-1).version === 0 && imports.at(-1).bytes === 60000 &&
      starts.length === 0 && submissions.length === 0 && await captchaCount() === 0,
      'Import auto-started authentication/CAPTCHA or lost its bound')
    await setup().getByText('手机搜索会话已失效，请联系超管为此机器人手动重新认证；不会自动登录', { exact: true }).waitFor()
    await authenticate(account)
    check(starts.length === 1 && starts[0].adapter === 'native-mobile-auth' &&
      submissions.length === 0 && await captchaCount() === 0, 'Opening mobile login submitted native work')
    check(!await login(account).getByRole('textbox', { name: '机器人名称', exact: true }).count() &&
      !await login(account).getByRole('tab').count() &&
      !await login(account).getByText('消息接收', { exact: true }).count() &&
      !/IM 凭据|尚未连接|桌面端接入/.test(await login(account).innerText()),
      'Mobile auth exposed account creation, signup or IM progress')
    await fit(login(account), 'login')
    await fill(account); await closeLogin(account)
    await open(account); await authenticate(account)
    check(await login(account).getByRole('textbox', { name: '账号 / 手机号', exact: true }).inputValue() === '' &&
      await login(account).getByRole('textbox', { name: '原生账号密码', exact: true }).inputValue() === '' &&
      starts.length === 1, 'Reopening retained credentials or replaced a resumable mobile transaction')
    await fill(account)
    let captcha = await captchaCount()
    await login(account).getByRole('button', { name: '验证并登录', exact: true }).click()
    await page.waitForFunction(index => window.__mobileSyntheticCaptcha.length > index, captcha)
    await closeLogin(account); await verify(captcha)
    check(submissions.length === 0, 'Late CAPTCHA callback submitted after closing')
    await open(account); await authenticate(account); await fill(account)
    captcha = await captchaCount(); authResult = 'native_mobile_identity_mismatch'
    await login(account).getByRole('button', { name: '验证并登录', exact: true }).click()
    await verify(captcha, false)
    await login(account).getByText('人机验证未通过', { exact: true }).waitFor()
    check(submissions.length === 0, 'Failed CAPTCHA submitted mobile login')
    await verify(captcha); await verify(captcha)
    await login(account).getByText('手机登录身份与此机器人不一致，业务搜索会话未保存，请使用同一原生账号', { exact: true }).waitFor()
    check(submissions.length === 1 && submissions[0].adapter === 'native-mobile-auth',
      'Duplicate CAPTCHA submitted twice or used desktop authentication')
    authResult = ''
    await login(account).getByRole('button', { name: '重新开始', exact: true }).click()
    await login(account).getByText('等待登录', { exact: true }).waitFor()
    await login(account).getByRole('radio', { name: '短信登录', exact: true })
      .locator('xpath=ancestor::label').click()
    await login(account).getByRole('textbox', { name: '手机号', exact: true }).fill('synthetic-self')
    captcha = await captchaCount()
    await login(account).getByRole('button', { name: '验证并发送短信', exact: true }).click()
    check(submissions.length === 1, 'SMS was sent before human verification')
    await verify(captcha)
    await login(account).getByText('等待短信验证码', { exact: true }).waitFor()
    await login(account).getByRole('textbox', { name: '短信验证码', exact: true }).fill('123456')
    captcha = await captchaCount()
    await login(account).getByRole('button', { name: '验证短信并登录', exact: true }).click()
    await verify(captcha)
    await login(account).locator('.auth-status').getByText('业务搜索会话已保存', { exact: true }).waitFor()
    check(submissions.length === 3 && submissions.every(value => value.adapter === 'native-mobile-auth') &&
      !await login(account).getByText('消息接收', { exact: true }).count() &&
      !/IM 凭据|尚未连接|已登录，会话/.test(await login(account).innerText()),
      'Mobile success claims an IM connection, retried or used desktop routes')
    await closeLogin(account); await open(account)
    await setup().getByText('可用', { exact: true }).last().waitFor()
    const startsBeforeConfigChange = starts.length, callsBeforeConfigChange = submissions.length
    await selectFile(configuration); await save().click()
    await setup().getByRole('status').getByText('手机搜索配置已保存，请单独打开登录完成认证', { exact: true }).waitFor()
    check(starts.length === startsBeforeConfigChange && submissions.length === callsBeforeConfigChange,
      'Replacing configuration automatically authenticated the account')
    await authenticate(account)
    check(starts.length === startsBeforeConfigChange + 1 &&
      !await login(account).getByText('业务搜索会话已保存', { exact: true }).count(),
      'Replaced configuration restored an old authenticated transaction')
    await closeLogin(account); await open(account)
    await closeSetup()
    // The existing default login still uses the desktop deployment/transaction routes.
    await dashboard().getByRole('button', { name: '重新登录', exact: true }).click()
    const desktop = page.getByRole('dialog').filter({
      has: page.getByText(new RegExp(`^${account.name} · (注册 / )?登录$`))
    })
    await desktop.getByText('桌面端接入', { exact: true }).waitFor()
    await desktop.getByText('等待登录', { exact: true }).waitFor()
    check(starts.at(-1).adapter === 'native-auth', 'Default desktop login used the companion adapter')
    await desktop.getByRole('button', { name: '关闭面板', exact: true }).click()
    // A delayed status response must not populate a different account's setup.
    const delayedStatus = delayNext(`/accounts/${account.id}/native-mobile-auth`, 'GET')
    await dashboard().getByRole('button', { name: '手机搜索认证', exact: true }).click()
    await delayedStatus.ready; await closeSetup(); await choose(other); await open(other)
    await setup().getByText('尚未保存', { exact: true }).waitFor()
    delayedStatus.release(); await delayedStatus.done
    await setup().getByText(`${other.native_nickname} · ${other.name}`, { exact: true }).waitFor()
    check(await setup().getByText('暂无记录', { exact: true }).isVisible(), 'Late status crossed accounts')
    await closeSetup()
    // A delayed transaction start cannot restore a closed account's login form.
    const delayedStart = delayNext(`/accounts/${other.id}/native-mobile-auth`, 'POST')
    await open(other)
    await setup().getByRole('button', { name: '打开手机搜索登录', exact: true }).click()
    await delayedStart.ready; await closeLogin(other); await choose(account)
    flows.delete(active.get(`${account.id}:native-mobile-auth`))
    await open(account); await authenticate(account)
    delayedStart.release(); await delayedStart.done
    check(await login(account).getByRole('textbox', { name: '账号 / 手机号', exact: true }).inputValue() === '',
      'Late transaction response changed another account login')
    await login(account).getByRole('radio', { name: '短信登录', exact: true })
      .locator('xpath=ancestor::label').click()
    await login(account).getByRole('textbox', { name: '手机号', exact: true }).fill('synthetic-self')
    captcha = await captchaCount()
    await login(account).getByRole('button', { name: '验证并发送短信', exact: true }).click()
    await verify(captcha)
    await login(account).getByText('等待短信验证码', { exact: true }).waitFor()
    await login(account).getByRole('textbox', { name: '短信验证码', exact: true }).fill('123456')
    authResult = 'native_mobile_login_response_invalid'
    captcha = await captchaCount()
    const beforeInvalid = submissions.length
    await login(account).getByRole('button', { name: '验证短信并登录', exact: true }).click()
    await verify(captcha)
    await login(account).getByText('手机登录响应的身份或安全字段无法核验，搜索会话未保存；不会自动重试',
      { exact: true }).waitFor()
    await login(account).getByText(
      '失败阶段：短信登录；手机响应的安全验证状态格式无效，未保存会话。；诊断版本：3', { exact: true }).waitFor()
    await login(account).getByText('上游请求结果未知，未自动重试。请先核对官方客户端状态。',
      { exact: true }).waitFor()
    await fit(login(account), 'response-failure')
    check(submissions.length === beforeInvalid + 1 &&
      !await login(account).getByRole('button', { name: '验证短信并登录', exact: true }).count(),
      'Response failure silently retried authentication or retained a submit button')
    authResult = ''
    await closeLogin(account)
    statuses.get(account.id).blocked_reason = 'native_mobile_account_unavailable'
    await open(account)
    await setup().getByText('此旺商聊管理机器人尚不可用，请先完成桌面登录或重新选择账号', { exact: true }).waitFor()
    check(await setup().getByRole('button', { name: '打开手机搜索登录', exact: true }).isDisabled(),
      'Unavailable WSL account could start companion auth')
    await closeSetup(); await navigate(chili)
    check(!await dashboard().getByRole('button', { name: '手机搜索认证', exact: true }).count(),
      'Chili dashboard exposed WSL companion credentials')
    const mobileReads = reads.length, mobileWrites = starts.length + submissions.length + imports.length
    role = 'user'; await navigate(account, 'user')
    check(!await page.getByRole('button', { name: '手机搜索认证', exact: true }).count(),
      'Ordinary customer exposed mobile setup')
    await page.getByRole('button', { name: '接入新群组', exact: true }).click()
    await page.getByText(normalMessage, { exact: true }).waitFor()
    check(reads.length === mobileReads &&
      starts.length + submissions.length + imports.length === mobileWrites &&
      !await page.getByRole('textbox', { name: '原生账号密码', exact: true }).count(),
      'Ordinary user read configuration or submitted authentication')
    check(!await page.evaluate(() => [...Object.values(sessionStorage), ...Object.values(localStorage)]
      .some(value => /SYNTHETIC_PASSWORD_ONLY|SYNTHETIC_CONFIGURATION_ONLY/.test(value))),
      'Configuration/credentials leaked into persistent browser storage')
    check(!unexpected.length, 'Unexpected API routes: ' + JSON.stringify(unexpected))
    return { passed: true, mock_only: true, native_requests: 0, imports: imports.length,
      synthetic_submissions: submissions.length, viewports: [320, 390, 768, 1440],
      read_only_setup: true, bounded_import: true, desktop_mobile_separated: true,
      login_only: true, close_clears_sensitive_input: true, late_results_isolated: true,
      configuration_version_fenced: true, captcha_required_once: true, mobile_errors_visible: true,
      response_diagnostic_visible: true, invalid_response_not_retried: true,
      customer_credentials_hidden: true, screenshots }
  } finally {
    for (const hold of holds) hold.release()
    await page.unroute('**/api/v1/**', handler)
  }
}
