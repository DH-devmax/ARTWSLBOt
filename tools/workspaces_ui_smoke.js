// Every API request is synthetic in this smoke; no native or business action is sent.
// prettier-ignore
async (page) => {
  if (new URL(page.url()).origin !== 'http://127.0.0.1:5173') throw new Error('Local UI only')
  const original = await page.evaluate(() => Object.fromEntries(Object.entries(sessionStorage).filter(([key]) => key.startsWith('wsl:'))))
  const realMe = (await (await page.request.get('http://127.0.0.1:5173/api/v1/me')).json()).data
  if (realMe.user.role !== 'superadmin') throw new Error('Website superadmin session required')
  const id = (n) => `10000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const accounts = {
    wsl: [
      { id: id(1), name: '群管甲', version: 1, workspace: 'wsl', runtime_state: 'connected', auth_state: 'authenticated' },
      { id: id(2), name: '群管乙', version: 1, workspace: 'wsl', runtime_state: 'reconnecting', auth_state: 'authenticated' }
    ],
    maibot: [{ id: id(3), name: '聊天甲', version: 1, workspace: 'maibot', runtime_state: 'disconnected', auth_state: 'not_authenticated' }]
  }
  const groups = [
    { group_id: id(11), name: '隔离群甲', primary_account_id: id(1), ai_account_id: id(3), account_name: '群管甲', version: 1, active: true, management_enabled: false, ai_enabled: false, config: {} },
    { group_id: id(12), name: '隔离群乙', primary_account_id: id(2), account_name: '群管乙', version: 1, active: true, management_enabled: false, ai_enabled: false, config: {} }
  ]
  const writes = []
  const flows = new Map()
  const end = new Date(Date.now() + 600000).toISOString()
  let viewer = false
  let sequence = 40
  let delayAccounts = false
  let releaseAccounts
  let delayedSeen
  let delayedResponse = Promise.resolve()
  const pattern = '**/api/v1/**'
  const envelope = (data) => ({ code: 0, data, msg: '' })
  const handler = async (route) => {
    const req = route.request()
    const path = new URL(req.url()).pathname.replace('/api/v1', '')
    const workspace = req.headers()['x-workspace'] || 'wsl'
    const method = req.method()
    if (workspace !== 'wsl' && workspace !== 'maibot') throw new Error('Bad workspace header')
    if (method === 'GET') {
      let result = []
      let finishDelayed
      if (path === '/me') result = viewer ? {
        user: { id: id(99), username: '只读测试用户', role: 'user', tenant_id: id(98) },
        permissions: ['overview:read', 'accounts:read', 'groups:read', 'operations:read']
      } : realMe
      else if (path === '/accounts') {
        result = [...accounts[workspace]]
        if (workspace === 'wsl' && delayAccounts) {
          delayAccounts = false
          delayedResponse = new Promise((resolve) => { finishDelayed = resolve })
          delayedSeen()
          await new Promise((resolve) => { releaseAccounts = resolve })
        }
      } else if (path === '/groups') result = workspace === 'wsl' ? groups : [
        { group_id: id(11), name: '隔离群甲', ai_account_id: id(3), account_name: '聊天甲', ai_request_state: 'disabled', version: 1 }
      ]
      else if (path === '/personas') result = [{
        account_id: id(3), account_name: '聊天甲', nickname: '聊天甲', personality: '独立聊天人设', reply_style: '简短', version: 1
      }]
      else if (path === '/overview') result = {
        accounts: accounts[workspace].length, groups: workspace === 'wsl' ? 2 : 1,
        ai_groups: 0, ai_requests: 0, pending_claims: 0, native_ready: false, ai_ready: false
      }
      else if (path === '/native-deployment') result = { configured: true, crypto_ready: true }
      else if (path.includes('/native-auth/')) result = flows.get(path.split('/').at(-1))
      try {
        await route.fulfill({ json: envelope(result) })
      } catch (error) {
        // AbortController may have already closed the deliberately delayed read.
        if (!finishDelayed || !String(error).includes('Route is already handled')) throw error
      } finally {
        finishDelayed?.()
      }
      return
    }
    const body = req.postDataJSON()
    writes.push({ path, method, workspace, body })
    if (path === '/accounts' && method === 'POST') {
      const row = {
        id: id(++sequence), name: body.name, workspace, version: 1,
        runtime_state: 'disconnected', auth_state: 'not_authenticated'
      }
      accounts[workspace].push(row)
      await route.fulfill({ status: 201, json: envelope({ id: row.id }) })
    } else if (/\/accounts\/[^/]+\/native-auth$/.test(path) || path === '/native-signup') {
      const view = {
        transaction_id: `workspace-synthetic-${++sequence}`,
        account_id: body.account_id || path.split('/')[2],
        purpose: path === '/native-signup' ? 'signup' : 'login',
        status: 'pending', captcha_id: 'synthetic-local-only', expires_at: end
      }
      flows.set(view.transaction_id, view)
      await route.fulfill({ status: 201, json: envelope(view) })
    } else if (path.includes('/native-auth/')) {
      const view = flows.get(path.split('/').at(-1))
      await route.fulfill({ json: envelope({
        ...view, status: method === 'DELETE' ? 'cancelled' : 'pending',
        error: method === 'DELETE' ? '' : 'native_invalid_credentials'
      }) })
    } else {
      await route.abort('blockedbyclient')
      throw new Error(`Unexpected write blocked: ${method} ${path}`)
    }
  }
  const choose = async (name, option) => {
    if (name === '切换工作台') {
      await page.getByRole('button', { name, exact: true }).click()
      return
    }
    await page.getByRole('combobox', { name, exact: true })
      .locator('xpath=ancestor::div[contains(@class, "el-select__wrapper")]').click()
    await page.getByRole('option').filter({ hasText: option }).click()
  }
  const navigate = async (name) => page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name, exact: true }).click()
  const edit = async (name) => page.getByRole('row').filter({ hasText: name }).getByRole('button', { name: '群配置', exact: true }).click()
  const configDialog = () => page.locator('.platform-config-dialog')
  await page.route(pattern, handler)
  try {
    await page.evaluate(() => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
    })
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('http://127.0.0.1:5173/#overview')
    await page.reload()
    await page.addStyleTag({ content: '*,*::before,*::after{transition:none!important;animation:none!important}' })
    await page.getByRole('row').filter({ hasText: '群管甲' }).waitFor()
    const nav = page.getByRole('navigation', { name: '主导航' })
    if (await nav.getByRole('button', { name: '账号接入' }).count()) throw new Error('Old account menu still exists')
    if (await nav.getByRole('button', { name: 'AI 人设' }).count()) throw new Error('WSL includes AI engine settings')
    await choose('机器人面板', '群管甲')
    await navigate('群组管理')
    await edit('隔离群甲')
    await page.getByLabel('拦截词（一行一个）', { exact: true }).fill('只属于群管甲的草稿')
    await configDialog().getByRole('button', { name: '取消', exact: true }).click()
    await choose('机器人面板', '群管乙')
    await edit('隔离群乙')
    if (await page.getByLabel('拦截词（一行一个）', { exact: true }).inputValue() !== '') throw new Error('Account draft leaked')
    await configDialog().getByRole('button', { name: '取消', exact: true }).click()
    await choose('机器人面板', '群管甲')
    await edit('隔离群甲')
    if (await page.getByLabel('拦截词（一行一个）', { exact: true }).inputValue() !== '只属于群管甲的草稿') throw new Error('Draft was lost on account switching')
    await configDialog().getByRole('button', { name: '取消', exact: true }).click()
    if (writes.length) throw new Error('Panel switching mutated a connection or configuration')
    // A slow response from WSL must not populate the MaiBot workspace.
    const seen = new Promise((resolve) => { delayedSeen = resolve })
    delayAccounts = true
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await seen
    await choose('切换工作台', 'MaiBot 工作台')
    await page.getByRole('heading', { name: '聊天机器人状态', exact: true }).waitFor()
    releaseAccounts()
    await page.getByRole('row').filter({ hasText: '聊天甲' }).waitFor()
    for (const name of ['群组管理', '用户管理', 'SaaS 套餐', '财务台账', '操作记录', '受控测试']) {
      if (await nav.getByRole('button', { name, exact: true }).count()) throw new Error(`MaiBot includes management: ${name}`)
    }
    if (await page.getByRole('row').filter({ hasText: '群管甲' }).count()) throw new Error('Late account response crossed workspaces')
    await page.screenshot({ path: 'output/playwright/maibot-workspace-desktop.png', fullPage: true, animations: 'disabled' })
    await navigate('AI 人设')
    await page.getByText('独立聊天人设', { exact: true }).waitFor()
    await choose('切换工作台', '旺商聊工作台')
    await page.getByRole('heading', { name: '群组管理', exact: true }).waitFor()
    await edit('隔离群甲')
    if (await page.getByLabel('拦截词（一行一个）', { exact: true }).inputValue() !== '只属于群管甲的草稿') throw new Error('Workspace draft was lost')
    await configDialog().getByRole('button', { name: '取消', exact: true }).click()
    if (writes.length) throw new Error('Workspace switching dispatched a native action')
    // Creation happens inside the auth form, with the selected workspace captured.
    await page.locator('.native-login-entry').click()
    await page.getByLabel('机器人名称', { exact: true }).waitFor()
    if (writes.length) throw new Error('Opening the auth form created an empty account')
    await page.getByRole('tab', { name: '新账号注册', exact: true }).click()
    await page.getByLabel('注册手机号', { exact: true }).waitFor()
    await page.getByRole('tab', { name: '原生登录', exact: true }).click()
    await page.getByLabel('机器人名称', { exact: true }).fill('表单新建群管')
    await page.getByLabel('账号 / 手机号', { exact: true }).fill('13800000000')
    await page.getByLabel('原生账号密码', { exact: true }).fill('synthetic-ui-only1')
    await page.evaluate(() => {
      window.initNECaptcha = (options, resolve) => resolve({
        verify: () => options.onVerify(null, { validate: 'synthetic-workspace-only' }),
        destroy: () => {}
      })
    })
    await page.getByRole('button', { name: '验证并登录', exact: true }).click()
    await page.getByText('旺商聊账号或密码不正确', { exact: true }).waitFor()
    const created = writes.filter((entry) => entry.path === '/accounts')
    if (created.length !== 1 || created[0].workspace !== 'wsl' || created[0].body.name !== '表单新建群管') throw new Error('Auth form created the wrong panel')
    const native = writes.find((entry) => entry.path.endsWith('/native-auth'))
    if (!native || native.workspace !== 'wsl' || !native.path.includes(accounts.wsl.at(-1).id)) throw new Error('Native login used the wrong account scope')
    await page.getByRole('button', { name: '关闭面板', exact: true }).click()
    await choose('切换工作台', 'MaiBot 工作台')
    await page.getByRole('heading', { name: 'AI 人设', exact: true }).waitFor()
    await page.locator('.native-login-entry').click()
    await page.getByLabel('机器人名称', { exact: true }).fill('表单新建聊天')
    await page.getByLabel('账号 / 手机号', { exact: true }).fill('13900000000')
    await page.getByLabel('原生账号密码', { exact: true }).fill('synthetic-chat-only1')
    await page.getByRole('button', { name: '验证并登录', exact: true }).click()
    await page.getByText('旺商聊账号或密码不正确', { exact: true }).waitFor()
    const chatCreate = writes.filter((entry) => entry.path === '/accounts' && entry.workspace === 'maibot')
    const chatLogin = writes.find((entry) => entry.workspace === 'maibot' && entry.path.endsWith('/native-auth'))
    if (chatCreate.length !== 1 || chatCreate[0].body.name !== '表单新建聊天' ||
        !chatLogin || !chatLogin.path.includes(accounts.maibot.at(-1).id)) throw new Error('MaiBot login crossed the management account pool')
    await page.getByRole('button', { name: '关闭面板', exact: true }).click()
    await choose('切换工作台', '旺商聊工作台')
    await navigate('工作台')
    await choose('机器人面板', '工作台总览')
    await page.locator('.overview-grid').getByText('在线', { exact: true }).waitFor()
    await page.screenshot({ path: 'output/playwright/workspaces-desktop.png', fullPage: true, animations: 'disabled' })
    for (const viewport of [{ width: 320, height: 740 }, { width: 390, height: 844 }, { width: 647, height: 779 }]) {
      await page.setViewportSize(viewport)
      await page.evaluate(() => window.scrollTo(0, 0))
      const overflow = await page.evaluate(() => {
        const bar = document.querySelector('.topbar')
        const rect = bar.getBoundingClientRect()
        return {
          page: document.documentElement.scrollWidth > innerWidth,
          controls: [...bar.querySelectorAll('button,.el-select')].filter((el) => el.getClientRects().length).some((el) => {
            const b = el.getBoundingClientRect()
            return b.left < -1 || b.right > innerWidth + 1 || b.bottom > rect.bottom + 1
          })
        }
      })
      if (overflow.page || overflow.controls) throw new Error(`Topbar overflow ${JSON.stringify({ viewport, overflow })}`)
      await page.locator('.native-login-entry').click()
      await page.getByLabel('机器人名称', { exact: true }).waitFor()
      const bounds = await page.locator('.native-auth-dialog').boundingBox()
      if (!bounds || bounds.x < 0 || bounds.y < 0 || bounds.x + bounds.width > viewport.width + 1 || bounds.y + bounds.height > viewport.height + 1) throw new Error('Auth dialog exceeds viewport')
      await page.screenshot({ path: `output/playwright/workspaces-${viewport.width}.png`, fullPage: true, animations: 'disabled' })
      await page.getByRole('button', { name: '关闭面板', exact: true }).click()
    }
    viewer = true
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.reload()
    await page.getByRole('button', { name: '只读测试用户' }).waitFor()
    if (await page.getByRole('button', { name: '切换工作台' }).count() ||
        await page.locator('.native-login-entry').count()) throw new Error('Tenant sees platform or native credentials controls')
    await navigate('群组管理')
    if (await page.getByRole('button', { name: '群配置', exact: true }).count() ||
        await page.getByRole('button', { name: 'AI 入群 / 启停', exact: true }).count()) throw new Error('Read-only user sees management controls')
    return {
      result: 'passed', accountSwitchWrites: 0, workspaceSwitchWrites: 0,
      createdPanels: { wsl: created.length, maibot: chatCreate.length }, nativeRequests: 'all intercepted',
      checked: ['separate account pools', 'draft preservation', 'late response isolation', 'topbar auth', 'viewer permissions', '320/390/647/1440 layouts']
    }
  } finally {
    if (releaseAccounts) releaseAccounts()
    await delayedResponse
    await page.unroute(pattern, handler)
    await page.evaluate((saved) => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
      for (const [key, value] of Object.entries(saved)) sessionStorage.setItem(key, value)
    }, original)
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.goto('http://127.0.0.1:5173/#overview')
    await page.reload()
  }
}
