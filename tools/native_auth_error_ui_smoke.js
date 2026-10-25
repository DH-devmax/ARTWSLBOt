// Synthetic CAPTCHA and API only. No native login/SMS request leaves the browser.
// prettier-ignore
async (page) => {
  const base = 'http://127.0.0.1:5173'
  if (new URL(page.url()).origin !== base) throw new Error('Local console only')
  const realMe = (await (await page.request.get(base + '/api/v1/me')).json()).data
  const saved = await page.evaluate(() => Object.fromEntries(Object.entries(sessionStorage).filter(([key]) => key.startsWith('wsl:'))))
  const account = { id: '60000000-0000-4000-8000-000000000001', name: '验证回归', version: 1, workspace: 'wsl', auth_state: 'not_authenticated', runtime_state: 'disconnected' }
  let flow, sequence = 0, posts = 0, result = 'unknown'
  const pattern = '**/api/v1/**'
  const handler = async (route) => {
    const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', '')
    let data = []
    if (req.method() === 'GET') {
      if (path === '/me') data = realMe
      else if (path === '/accounts') data = [account]
      else if (path === '/native-deployment') data = { configured: true, crypto_ready: true }
      else if (path === '/overview') data = { accounts: 1, groups: 0 }
      else if (path.includes('/native-auth/')) data = flow
    } else if (req.method() === 'POST' && path === `/accounts/${account.id}/native-auth`) {
      flow = { transaction_id: `synthetic-error-${++sequence}`, account_id: account.id, purpose: 'login', status: 'pending', captcha_id: 'synthetic-only', expires_at: new Date(Date.now() + 600000).toISOString() }
      data = flow
    } else if (req.method() === 'POST' && path.endsWith(`/native-auth/${flow?.transaction_id}`)) {
      posts++
      const body = req.postDataJSON()
      if (body.captcha !== 'synthetic-human' || body.account !== 'synthetic-login') throw new Error('Unexpected synthetic auth input')
      data = { ...flow, status: 'working' }
      flow = { ...flow, status: result === 'unknown' ? 'unknown' : 'failed',
        error: result === 'unknown' ? 'native_transport_unknown' : 'native_dns_fake_ip',
        diagnostic: result === 'unknown' ? 'connection_closed' : '' }
    } else if (req.method() === 'DELETE' && path.includes('/native-auth/')) {
      flow = { ...flow, status: 'cancelled' }
      data = flow
    } else {
      await route.abort('blockedbyclient')
      throw new Error('Unexpected write blocked')
    }
    await route.fulfill({ json: { code: 0, data, msg: '' } })
  }
  await page.addInitScript(() => {
    window.__syntheticCaptcha = []
    window.initNECaptcha = (options, ready) => {
      window.__syntheticCaptcha.push(options)
      ready({ verify() {}, destroy() {} })
    }
  })
  await page.route(pattern, handler)
  // No SDK/verification traffic is permitted even if the stub is accidentally bypassed.
  const external = '**/*'
  const externalHandler = (route) => new URL(route.request().url()).origin === base ? route.fallback() : route.abort('blockedbyclient')
  await page.route(external, externalHandler)
  const dialog = page.locator('.native-auth-dialog')
  const verify = async (index, pass) => page.evaluate(({ index, pass }) => {
    window.__syntheticCaptcha[index].onVerify(pass ? null : new Error('synthetic failed challenge'), { validate: pass ? 'synthetic-human' : '' })
  }, { index, pass })
  const fill = async () => {
    await dialog.getByRole('textbox', { name: '账号 / 手机号', exact: true }).fill('synthetic-login')
    await dialog.getByRole('textbox', { name: '原生账号密码', exact: true }).fill('synthetic-not-real')
    await dialog.getByRole('button', { name: '验证并登录', exact: true }).click()
  }
  try {
    await page.evaluate(() => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
    })
    await page.goto(base + '/#overview')
    await page.reload()
    await page.getByRole('heading', { name: '群管机器人状态', exact: true }).waitFor()
    await page.getByRole('row').filter({ hasText: account.name }).getByRole('button', { name: '注册 / 登录', exact: true }).click()
    await dialog.getByText('等待登录', { exact: true }).waitFor()
    await fill()
    await verify(0, false)
    await dialog.getByText('人机验证未通过', { exact: true }).waitFor()
    if (posts) throw new Error('Failed CAPTCHA submitted a request')
    await verify(0, true)
    await verify(0, true)
    await dialog.getByText('请求结果未知', { exact: true }).waitFor()
    await dialog.getByText('上游连接提前关闭，未取得可核验的结果。', { exact: true }).waitFor()
    if (await dialog.getByText('人机验证未通过', { exact: true }).count()) throw new Error('Stale CAPTCHA error masked the real result')
    await page.waitForTimeout(1200)
    if (posts !== 1) throw new Error('Unknown request or CAPTCHA callback retried')
    for (const width of [320, 390, 647, 1440]) {
      await page.setViewportSize({ width, height: width === 390 ? 844 : 779 })
      await page.screenshot({ path: `output/playwright/native-auth-errors-${width}.png`, fullPage: false, animations: 'disabled' })
      if (!await dialog.evaluate((el) => {
        const rect = el.getBoundingClientRect()
        return rect.left >= 0 && rect.right <= innerWidth && rect.top >= 0 && rect.bottom <= innerHeight &&
          document.documentElement.scrollWidth <= innerWidth
      })) throw new Error('Native auth error dialog overflow')
    }
    result = 'dns'
    await dialog.getByRole('button', { name: '重新开始', exact: true }).click()
    await dialog.getByText('等待登录', { exact: true }).waitFor()
    await fill()
    await verify(1, true)
    await dialog.getByText('登录未完成', { exact: true }).waitFor()
    await dialog.getByText('服务端 DNS 返回代理映射地址，未发送请求，请配置真实 DNS 或启用加密 DNS 回退', { exact: true }).waitFor()
    if (await dialog.getByText('请求结果未知', { exact: true }).count() || posts !== 2) throw new Error('Pre-connect rejection presented as unknown')
    await fill()
    await page.evaluate(() => window.__syntheticCaptcha[2].onClose())
    await verify(2, true)
    if (posts !== 2) throw new Error('Closed CAPTCHA submitted a late callback')
    return { passed: true, staleCaptchaCleared: true, noAutomaticRetry: true, preconnectSeparated: true, duplicateAndClosedCallbacksIgnored: true, syntheticPosts: posts }
  } finally {
    await page.unroute(external, externalHandler)
    await page.unroute(pattern, handler)
    await page.evaluate((original) => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
      for (const [key, value] of Object.entries(original)) sessionStorage.setItem(key, value)
    }, saved)
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.goto(base + '/#overview')
    await page.reload()
  }
}
