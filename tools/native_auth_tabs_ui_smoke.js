// All authentication writes are intercepted; no native login, signup or SMS is sent.
// prettier-ignore
async (page) => {
  if (new URL(page.url()).origin !== 'http://127.0.0.1:5173') {
    throw new Error('Tab smoke only supports the local development UI')
  }
  const accounts = (await (await page.request.get(new URL('/api/v1/accounts', page.url()).href)).json()).data.map((row) => ({
    ...row, native_uid: null, connection_enabled: false, auth_state: 'not_authenticated',
    signup_state: '', registered_account: ''
  }))
  if (accounts.length < 2) throw new Error('Two local account panels are required')
  const flows = new Map()
  const current = new Map()
  let sequence = 0
  let rejectSwitch = false
  let starts = 0
  let submissions = 0
  let cancellations = 0
  let delayedStart
  let releaseStart
  const end = new Date(Date.now() + 600000).toISOString()
  const envelope = (data) => ({ code: 0, data, msg: '' })
  const pattern = '**/api/v1/**'
  const handler = async (route) => {
    const request = route.request()
    const path = new URL(request.url()).pathname
    const method = request.method()
    if (path === '/api/v1/accounts' && method === 'GET') {
      await route.fulfill({ json: envelope(accounts) })
      return
    }
    if (path.endsWith('/native-deployment')) {
      await route.fulfill({ json: envelope({ configured: true, crypto_ready: true }) })
      return
    }
    if (path.endsWith('/native-signup') || /\/accounts\/[^/]+\/native-auth$/.test(path)) {
      starts++
      if (rejectSwitch) {
        await route.fulfill({ status: 409, json: { code: 409, msg: 'native_auth_account_busy' } })
        return
      }
      const signup = path.endsWith('/native-signup')
      const accountID = signup ? request.postDataJSON().account_id : path.split('/')[4]
      const old = current.get(accountID)
      if (old) flows.delete(old)
      const view = {
        transaction_id: `tabs-synthetic-${++sequence}`, account_id: accountID,
        purpose: signup ? 'signup' : 'login', status: 'pending',
        captcha_id: 'synthetic-local-only', expires_at: end
      }
      current.set(accountID, view.transaction_id)
      flows.set(view.transaction_id, view)
      if (delayedStart) {
        const delay = delayedStart
        delayedStart = undefined
        await delay
      }
      await route.fulfill({ status: 201, json: envelope(view) })
      return
    }
    if (path.includes('/native-auth/')) {
      const id = path.split('/').at(-1)
      const view = flows.get(id)
      if (!view) {
        await route.fulfill({ status: 404, json: { code: 404, msg: 'native_auth_not_found' } })
      } else if (method === 'GET') {
        await route.fulfill({ json: envelope(view) })
      } else if (method === 'DELETE') {
        cancellations++
        flows.delete(id)
        current.delete(view.account_id)
        await route.fulfill({ json: envelope({ ...view, status: 'cancelled' }) })
      } else {
        submissions++
        await route.fulfill({ status: 202, json: envelope({ ...view, status: 'working' }) })
      }
      return
    }
    if (method === 'GET') {
      await route.continue()
      return
    }
    await route.abort('blockedbyclient')
    throw new Error(`Unexpected write blocked: ${path}`)
  }
  const loginTab = () => page.getByRole('tab', { name: '原生登录', exact: true })
  const signupTab = () => page.getByRole('tab', { name: '新账号注册', exact: true })
  const close = () => page.getByRole('button', { name: '关闭面板', exact: true })
  const open = async (account) => {
    await page.getByRole('row').filter({ hasText: account.name })
      .getByRole('button', { name: '注册 / 登录', exact: true }).click()
  }
  const active = (account) => flows.get(current.get(account.id))
  const select = async (tab, field) => {
    await tab.click()
    await field.waitFor()
    if (await tab.getAttribute('aria-selected') !== 'true') throw new Error('Wrong active tab')
  }
  await page.route(pattern, handler)
  try {
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.reload()
    await page.addStyleTag({
      content: '*,*::before,*::after{transition:none!important;animation:none!important}'
    })
    await page.getByRole('button', { name: '工作台', exact: true }).click()
    await page.getByRole('heading', { name: '工作台', exact: true }).waitFor()
    await open(accounts[0])
    const loginInput = page.getByLabel('账号 / 手机号', { exact: true })
    const signupInput = page.getByLabel('注册手机号', { exact: true })
    await loginInput.waitFor()
    for (let i = 0; i < 6; i++) {
      await loginInput.fill('13800000000')
      await page.getByLabel('原生账号密码', { exact: true }).fill('synthetic-tabs-only1')
      await select(signupTab(), signupInput)
      if (await signupInput.inputValue() !== '') throw new Error('Login input leaked into signup')
      await signupInput.fill('13900000000')
      await page.getByLabel('昵称', { exact: true }).fill('仅本地测试')
      await select(loginTab(), loginInput)
      if (await loginInput.inputValue() !== '' ||
          await page.getByLabel('原生账号密码', { exact: true }).inputValue() !== '') {
        throw new Error('Switch restored sensitive or wrong-purpose inputs')
      }
    }
    if (flows.size !== 1 || submissions || cancellations) {
      throw new Error('Tabs duplicated, submitted or explicitly cancelled a transaction')
    }
    active(accounts[0]).status = 'working'
    await close().click()
    await open(accounts[0])
    await page.getByText('正在验证', { exact: true }).waitFor()
    const beforeWorking = starts
    await signupTab().click({ force: true })
    if (starts !== beforeWorking || await loginTab().getAttribute('aria-selected') !== 'true') {
      throw new Error('A working native request allows tab switching')
    }
    active(accounts[0]).status = 'unknown'
    await page.getByText('请求结果未知', { exact: true }).waitFor()
    const beforeUnknown = starts
    await signupTab().click()
    await page.getByText('上次请求结果未知，请先核对官方客户端状态，不能直接切换注册与登录', { exact: true }).waitFor()
    if (starts !== beforeUnknown || await loginTab().getAttribute('aria-selected') !== 'true') {
      throw new Error('Unknown outcome was replaced or submitted')
    }
    active(accounts[0]).status = 'pending'
    await close().click()
    await open(accounts[0])
    await loginInput.waitFor()
    rejectSwitch = true
    await signupTab().click()
    await page.getByText('此账号的注册或登录正在由另一位超管操作', { exact: true }).waitFor()
    if (await loginTab().getAttribute('aria-selected') !== 'true' ||
        active(accounts[0]).purpose !== 'login') {
      throw new Error('Failed switch replaced the previous tab or transaction')
    }
    rejectSwitch = false
    await select(signupTab(), signupInput)
    await signupInput.fill('13800000000')
    const resumedID = active(accounts[0]).transaction_id
    const beforeReopen = starts
    await close().click()
    await open(accounts[0])
    await signupInput.waitFor()
    if (starts !== beforeReopen || active(accounts[0]).transaction_id !== resumedID ||
        await signupInput.inputValue() !== '') {
      throw new Error('Closing/reopening lost flow purpose or retained sensitive input')
    }
    await page.setViewportSize({ width: 390, height: 844 })
    await page.screenshot({ path: 'output/playwright/native-auth-tabs-mobile.png' })
    const bounds = await page.locator('.native-auth-dialog:visible').boundingBox()
    if (!bounds || bounds.x < 0 || bounds.y < 0 || bounds.x + bounds.width > 391 ||
        bounds.y + bounds.height > 845) throw new Error('Unified auth dialog overflows mobile')
    await page.setViewportSize({ width: 1440, height: 960 })
    await select(loginTab(), loginInput)
    await page.screenshot({ path: 'output/playwright/native-auth-tabs-desktop.png' })
    // Hold a switching response while moving to a different account panel.
    delayedStart = new Promise((resolve) => { releaseStart = resolve })
    const switching = page.waitForRequest((request) =>
      new URL(request.url()).pathname.endsWith('/native-signup') && request.method() === 'POST')
    await signupTab().click()
    await switching
    await close().click()
    await open(accounts[1])
    await loginInput.waitFor()
    const lateResponse = page.waitForResponse((response) =>
      new URL(response.url()).pathname.endsWith('/native-signup'))
    releaseStart()
    await lateResponse
    if (await loginTab().getAttribute('aria-selected') !== 'true' ||
        await loginInput.inputValue() !== '' ||
        !(await page.getByRole('dialog', { name: `${accounts[1].name} · 注册 / 登录`, exact: true }).isVisible())) {
      throw new Error('A late response overwrote another account panel')
    }
    await close().click()
    await open(accounts[0])
    await loginInput.waitFor()
    if (active(accounts[0]).purpose !== 'login') {
      throw new Error('Returning to an account did not recover the server flow')
    }
    if (submissions) throw new Error('Tab test unexpectedly submitted authentication')
    return { mockOnly: true, roundTrips: 6, starts, submissions, cancellations,
      failedSwitchKeptLogin: true, workingBlocked: true, unknownBlocked: true,
      restoredPurpose: true, lateResponseIsolated: true,
      mobile: bounds }
  } finally {
    releaseStart?.()
    await page.unroute(pattern, handler)
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.reload()
  }
}
