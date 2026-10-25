// Run through playwright-cli run-code in the existing local console session.
// prettier-ignore
async (page) => {
  if (new URL(page.url()).origin !== 'http://127.0.0.1:5173') {
    throw new Error('This mock-only smoke must run against the local development UI')
  }
  const sent = []
  const flows = new Map()
  const accounts = (await (await page.request.get(new URL('/api/v1/accounts', page.url()).href)).json()).data.map((row) => ({
    ...row, native_uid: null, connection_enabled: false, auth_state: 'not_authenticated',
    signup_state: '', registered_account: ''
  }))
  let sequence = 0
  const end = new Date(Date.now() + 600000).toISOString()
  const envelope = (data) => ({ code: 0, msg: '', data })
  const pattern = '**/api/v1/**'
  const closePanel = () => page.getByRole('button', { name: '关闭面板', exact: true })
  const handler = async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    if (path === '/api/v1/accounts' && method === 'GET') {
      await route.fulfill({ json: envelope(accounts) })
      return
    }
    if (path.endsWith('/native-deployment')) {
      await route.fulfill({
        json: envelope({ configured: true, crypto_ready: true, version: 1, protocol_version: '3.7.3' })
      })
      return
    }
    if (path.endsWith('/native-signup') || /\/accounts\/[^/]+\/native-auth$/.test(path)) {
      const body = request.postDataJSON()
      const signup = path.endsWith('/native-signup')
      const accountID = signup ? body.account_id : path.split('/')[4]
      for (const [id, view] of flows) {
        if (view.account_id === accountID) flows.delete(id)
      }
      const view = {
        transaction_id: `ui-synthetic-${++sequence}`,
        account_id: accountID,
        purpose: signup ? 'signup' : 'login',
        status: 'pending',
        captcha_id: 'synthetic-local-only',
        expires_at: end
      }
      flows.set(view.transaction_id, view)
      await route.fulfill({ status: 201, json: envelope(view) })
      return
    }
    if (path.includes('/native-auth/')) {
      const id = path.split('/').at(-1)
      const view = flows.get(id)
      if (!view) {
        await route.fulfill({
          status: 404,
          json: { code: 404, msg: 'native_auth_not_found', data: null }
        })
        return
      }
      if (method === 'GET') {
        await route.fulfill({ json: envelope(view) })
        return
      }
      if (method === 'DELETE') {
        flows.delete(id)
        await route.fulfill({ json: envelope({ ...view, status: 'cancelled' }) })
        return
      }
      const body = request.postDataJSON()
      sent.push({
        action: body.action,
        account: view.account_id,
        consent: body.agreed,
        hasPassword: !!body.password
      })
      view.status = body.action === 'signup_submit' ? 'registered' : 'signup_sms_required'
      if (view.status === 'registered') {
        view.registered_account = '456'
        Object.assign(accounts.find((row) => row.id === view.account_id), {
          signup_state: 'registered', registered_account: '456'
        })
      }
      await route.fulfill({
        status: 202,
        json: envelope({ ...view, status: 'working' })
      })
      return
    }
    if (method === 'GET') {
      await route.continue()
      return
    }
    await route.abort('blockedbyclient')
    throw new Error(`Unmocked write blocked: ${path}`)
  }
  await page.route(pattern, handler)
  try {
    await page.reload()
    // This stub is used only with intercepted local API requests, never upstream.
    await page.evaluate(() => {
      window.initNECaptcha = (options, resolve) =>
        resolve({
          verify: () => options.onVerify(null, { validate: 'synthetic-ui-only' }),
          destroy: () => {}
        })
    })
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addStyleTag({
      content: '*,*::before,*::after{transition:none!important;animation:none!important}'
    })
    const open = async (name) => {
      await page.getByRole('row').filter({ hasText: name })
        .getByRole('button', { name: '注册 / 登录', exact: true }).click()
      await page.getByLabel('账号 / 手机号', { exact: true }).waitFor()
      await page.getByRole('tab', { name: '新账号注册', exact: true }).click()
      await page.getByLabel('注册手机号', { exact: true }).waitFor()
    }
    await open('小满')
    await page.getByLabel('注册手机号', { exact: true }).fill('13800000000')
    await page.getByLabel('昵称', { exact: true }).fill('离线注册测试')
    const send = page.getByRole('button', { name: '验证并发送注册短信', exact: true })
    if (await send.isEnabled()) throw new Error('Consent gate failed')
    await page.getByText('我已阅读并同意旺商聊用户协议和隐私政策，并确认有权使用此手机号。', { exact: true }).click()
    await send.click()
    await page.getByLabel('注册短信验证码', { exact: true }).waitFor()
    await page.setViewportSize({ width: 390, height: 844 })
    await page.screenshot({ path: 'output/playwright/native-signup-form-mobile.png' })
    const mobile = await page.locator('.el-dialog:visible').boundingBox()
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)
    if (overflow || mobile.x < 0 || mobile.x + mobile.width > 391) {
      throw new Error('Mobile dialog overflow')
    }
    await page.getByLabel('注册短信验证码', { exact: true }).fill('123456')
    await page.getByLabel('设置原生账号密码', { exact: true }).fill('synthetic1')
    await page.getByRole('button', { name: '验证并注册新账号', exact: true }).click()
    await page.getByText('注册成功，尚未登录', { exact: true }).waitFor()
    if (await page.getByRole('dialog').getByText('456', { exact: true }).count() !== 1) {
      throw new Error('Native account missing')
    }
    if (!await page.getByRole('button', { name: '登录新账号', exact: true }).isVisible()) {
      throw new Error('Login transition missing')
    }
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.screenshot({ path: 'output/playwright/native-signup-success-desktop.png' })
    await page.getByRole('button', { name: '登录新账号', exact: true }).click()
    await page.getByLabel('账号 / 手机号', { exact: true }).waitFor()
    if (await page.getByLabel('账号 / 手机号', { exact: true }).inputValue() !== '456') {
      throw new Error('Explicit login did not prefill the registered account')
    }
    if (!await page.getByRole('tab', { name: '新账号注册', exact: true })
      .evaluate((element) => element.classList.contains('is-disabled'))) {
      throw new Error('Registered panel can register again')
    }
    if (sent.some((step) => step.action === 'password' || step.action === 'sms_request')) {
      throw new Error('Registration silently submitted native login')
    }
    await closePanel().click()
    await open('2')
    if (await page.getByLabel('昵称', { exact: true }).inputValue() !== '') {
      throw new Error('Nickname leaked between accounts')
    }
    return {
      mockOnly: true,
      sent,
      mobile,
      overflow,
      secondDialog: await page.getByRole('dialog').getAttribute('aria-label')
    }
  } finally {
    await page.unroute(pattern, handler)
    await page.evaluate(() => { delete window.initNECaptcha })
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.reload()
  }
}
