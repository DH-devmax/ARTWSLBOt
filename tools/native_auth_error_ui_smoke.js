// Synthetic CAPTCHA and API only. No native login/SMS request leaves the browser.
// prettier-ignore
async (page, fixtureMe, contracts) => {
  const base = 'http://127.0.0.1:5173'
  const registration = contracts?.native_auth_registration
  if (!['manual_only', 'retired'].includes(registration)) throw new Error('Explicit registration contract required')
  const retiredSignup = registration === 'retired'
  if (page.url() !== 'about:blank' && new URL(page.url()).origin !== base) throw new Error('Local console only')
  const realMe = { user: { id: '60000000-0000-4000-8000-000000000002', role: 'superadmin', username: 'auth-errors-fixture' },
    permissions: ['overview:read', 'accounts:read', 'groups:read'] }
  const saved = await page.evaluate(() => Object.fromEntries(Object.entries(sessionStorage).filter(([key]) => key.startsWith('wsl:')))).catch(() => ({}))
  const account = { id: '60000000-0000-4000-8000-000000000001', name: '验证回归', version: 1, workspace: 'wsl', auth_state: 'not_authenticated', runtime_state: 'disconnected' }
  let flow, sequence = 0, posts = 0, signupStarts = 0, signupSubmissions = 0, result = 'unknown', migration = false
  const pattern = '**/api/v1/**'
  const handler = async (route) => {
    const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', '')
    let data = []
    if (req.method() === 'GET') {
      if (path === '/me') data = realMe
      else if (path === '/workspaces') data = { items: [{ id: 'user', name: '用户中心' }, { id: 'wsl', name: '旺商聊机器人' }] }
      else if (path === '/accounts') data = [account]
      else if (path === '/native-deployment') data = migration
        ? {configured:false,crypto_ready:true,version:7,migration_required:true,login_adapter:'desktop',supported_protocol:'2.8.3'}
        : { configured: true, crypto_ready: true,
        login_adapter:'desktop', supported_protocol:'2.8.3',
        device: { business_client: 'desktop', business_fingerprint: 'synthetic-device', im_client: 'web', im_fingerprint: 'synthetic-im', coexistence: 'unverified' } }
      else if (path === '/overview') data = { accounts: 1, groups: 0 }
      else if (path === `/accounts/${account.id}/receiver`) data = { enabled: false, revision: 0 }
      else if (path.includes('/native-auth/')) data = flow
    } else if (req.method() === 'POST' && (path === `/accounts/${account.id}/native-auth` || path === '/native-signup')) {
      const signup = path === '/native-signup'
      if (signup) {
        const body = req.postDataJSON()
        if (retiredSignup || body.account_id !== account.id ||
            Object.keys(body).sort().join() !== 'account_id,version' || body.version !== account.version)
          throw new Error('Signup preparation crossed its explicit contract or submitted input')
        signupStarts++
      }
      flow = { transaction_id: `synthetic-error-${++sequence}`, account_id: account.id,
        purpose: signup ? 'signup' : 'login', status: 'pending', captcha_id: 'synthetic-only', expires_at: new Date(Date.now() + 600000).toISOString() }
      data = flow
    } else if (req.method() === 'POST' && path.endsWith(`/native-auth/${flow?.transaction_id}`)) {
      const body = req.postDataJSON()
      if (flow.purpose === 'signup' || body.action?.startsWith('signup')) {
        signupSubmissions++
        throw new Error('Signup or registration SMS content must never be submitted by this check')
      }
      posts++
      if (body.captcha !== 'synthetic-human' || body.account !== 'synthetic-login') throw new Error('Unexpected synthetic auth input')
      data = { ...flow, status: 'working' }
      flow = { ...flow, status: result === 'unknown' ? 'unknown' : 'failed',
        error: { unknown: 'native_transport_unknown', dns: 'native_dns_fake_ip',
          generic: 'native_login_rejected_unspecified', credentials: 'native_invalid_credentials' }[result],
        diagnostic: result === 'unknown' ? 'connection_closed' : '',
        provider_code: ['generic', 'credentials'].includes(result) ? -1 : undefined,
        failure_stage: ['generic', 'credentials'].includes(result) ? 'password_login' : undefined }
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
    await page.goto(base + '/#overview')
    await page.evaluate(() => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
    })
    await page.goto(base + '/#overview')
    await page.reload()
    await page.getByRole('heading', { name: '群管机器人状态', exact: true }).waitFor()
    await page.getByRole('row').filter({ hasText: account.name }).getByRole('button', { name: `登录 ${account.name}`, exact: true }).click()
    await dialog.getByText('等待登录', { exact: true }).waitFor()
    await dialog.getByText('桌面端接入', { exact: true }).waitFor()
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
    if (!retiredSignup) {
      const before = sequence
      await dialog.getByRole('tab', { name: '新账号注册', exact: true }).click()
      if (sequence !== before || signupStarts ||
          await dialog.getByRole('tab', { name: '原生登录', exact: true }).getAttribute('aria-selected') !== 'true' ||
          await dialog.getByRole('textbox', { name: '注册手机号', exact: true }).count())
        throw new Error('Unknown login result allowed a registration switch')
    }
    for (const viewport of [{ width: 320, height: 568 }, { width: 390, height: 844 },
      { width: 647, height: 779 }, { width: 768, height: 900 }, { width: 1440, height: 960 },
      { width: 844, height: 390 }, { width: 647, height: 480 }]) {
      await page.setViewportSize(viewport)
      await page.screenshot({ path: `output/playwright/native-auth-errors-${viewport.width}-${viewport.height}.png`, fullPage: false, animations: 'disabled' })
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
    result = 'generic'
    await fill()
    await verify(await page.evaluate(() => window.__syntheticCaptcha.length - 1), true)
    await dialog.getByText('旺商聊拒绝了登录请求，尚无法确认具体原因', { exact: true }).waitFor()
    await dialog.getByText('失败阶段：密码登录；上游返回码：-1', { exact: true }).waitFor()
    await page.waitForTimeout(1200)
    if (posts !== 3) throw new Error('Generic -1 rejection retried')
    for (const width of [320, 390, 647, 768, 1440]) {
      await page.setViewportSize({ width, height: 779 })
      await page.screenshot({ path: `output/playwright/native-password-minus-one-${width}.png`, animations: 'disabled' })
      if (!await dialog.evaluate(el => {
        const rect = el.getBoundingClientRect()
        return rect.left >= 0 && rect.right <= innerWidth && rect.top >= 0 && rect.bottom <= innerHeight &&
          el.scrollWidth <= el.clientWidth && document.documentElement.scrollWidth <= innerWidth
      })) throw new Error('Password failure stage dialog overflow')
    }
    result = 'credentials'
    await fill()
    await verify(await page.evaluate(() => window.__syntheticCaptcha.length - 1), true)
    await dialog.getByText('旺商聊账号或密码不正确', { exact: true }).waitFor()
    await dialog.getByText('失败阶段：密码登录；上游返回码：-1', { exact: true }).waitFor()
    if (posts !== 4) throw new Error('Known credential rejection retried')
    const registrationVisible = await dialog.getByRole('tab', { name: '新账号注册', exact: true }).count() ||
      await dialog.getByRole('textbox', { name: '注册手机号', exact: true }).count() ||
      await dialog.getByRole('button', { name: /注册短信|注册新账号/ }).count()
    if (Boolean(registrationVisible) === retiredSignup)
      throw new Error('Registration controls changed from the selected product baseline')
    if (!retiredSignup) {
      const before = posts
      await fill()
      const staleLogin = await page.evaluate(() => window.__syntheticCaptcha.length - 1)
      await page.evaluate(index => window.__syntheticCaptcha[index].onClose(), staleLogin)
      await dialog.getByRole('button', { name: '验证并登录', exact: true }).and(page.locator(':enabled')).waitFor()
      await dialog.getByRole('tab', { name: '新账号注册', exact: true }).click()
      const phone = dialog.getByRole('textbox', { name: '注册手机号', exact: true })
      await phone.waitFor()
      if (await phone.inputValue() !== '' || signupStarts !== 1 || posts !== before)
        throw new Error('Manual registration inherited login input or submitted content')
      await phone.fill('13800000000')
      const register = dialog.getByRole('button', { name: '验证并发送注册短信', exact: true })
      if (!await register.isDisabled()) throw new Error('Registration consent was not required')
      await verify(staleLogin, true)
      if (posts !== before || signupSubmissions) throw new Error('Late login CAPTCHA submitted into registration')
      await dialog.locator('.signup-consent').click()
      if (!await dialog.getByRole('checkbox').isChecked()) throw new Error('Manual registration consent did not update')
      await register.and(page.locator(':enabled')).waitFor()
      for (const width of [320, 390, 768, 1440]) {
        await page.setViewportSize({ width, height: 900 })
        await page.screenshot({ path: `output/playwright/native-manual-registration-${width}.png`, animations: 'disabled' })
        if (!await dialog.evaluate(el => {
          const rect = el.getBoundingClientRect()
          return rect.left >= 0 && rect.right <= innerWidth && rect.top >= 0 && rect.bottom <= innerHeight &&
            el.scrollWidth <= el.clientWidth && document.documentElement.scrollWidth <= innerWidth
        })) throw new Error('Manual registration dialog overflow')
      }
      await register.click()
      const staleSignup = await page.evaluate(() => window.__syntheticCaptcha.length - 1)
      if (posts !== before || signupSubmissions) throw new Error('Registration submitted before CAPTCHA')
      await page.evaluate(index => window.__syntheticCaptcha[index].onClose(), staleSignup)
      await register.and(page.locator(':enabled')).waitFor()
      await dialog.getByRole('tab', { name: '原生登录', exact: true }).click()
      await dialog.getByRole('textbox', { name: '账号 / 手机号', exact: true }).waitFor()
      await verify(staleSignup, true)
      await verify(staleSignup, true)
      if (posts !== before || signupSubmissions ||
          await dialog.getByRole('textbox', { name: '账号 / 手机号', exact: true }).inputValue() !== '' ||
          await dialog.getByRole('textbox', { name: '原生账号密码', exact: true }).inputValue() !== '' ||
          await page.evaluate(() => [...Object.values(sessionStorage), ...Object.values(localStorage)]
            .some(value => /13800000000|synthetic-not-real|synthetic-login/.test(value))))
        throw new Error('Cancelled registration leaked input, persisted content or replayed a CAPTCHA')
    }
    migration=true
    const before=sequence
    await page.reload()
    await page.getByRole('button',{name:`登录 ${account.name}`,exact:true}).click()
    await dialog.getByText('移动端接入已移除，请先导入桌面端协议配置',{exact:true}).waitFor()
    if(sequence!==before || await dialog.getByRole('textbox',{name:'原生账号密码',exact:true}).count() ||
      await dialog.getByRole('button',{name:'验证并登录',exact:true}).count())throw new Error('Legacy mobile profile still exposed a usable login flow')
    await page.setViewportSize({width:647,height:779})
    await page.screenshot({path:'output/playwright/desktop-only-migration-647.png',animations:'disabled'})
    return { passed: true, desktopOnly:true, legacyProfileBlocked:true, staleCaptchaCleared: true, noAutomaticRetry: true, preconnectSeparated: true, duplicateAndClosedCallbacksIgnored: true, registrationContract:registration, registrationHidden:retiredSignup, signupPreparation:signupStarts, signupSubmissions, genericMinusOneScoped:true, failureStageVisible:true, syntheticPosts: posts, native_actions:0 }
  } catch (error) {
    await page.screenshot({ path: 'output/playwright/native-auth-errors-current-failure.png', animations: 'disabled' })
    throw error
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
