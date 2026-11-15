// Synthetic APIs only. No native login, model request or group action leaves
// this test. The browser runner must also block non-local upstream requests.
async (page) => {
  const base = 'http://127.0.0.1:5173'
  const original = await page.evaluate(() => Object.fromEntries(Object.entries(sessionStorage))).catch(() => ({}))
  const id = n => `ac000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  let tenant = false
  const writes = []
  const deployments = []
  const account = { id: id(2), name: '辣椒聊测试号', platform: 'lajiaoliao', purpose: 'management',
    workspace: 'lajiaoliao', version: 1, auth_state: 'not_authenticated', runtime_state: 'disconnected' }
  const chatAccounts = [
    { ...account, id: id(5), name: '旺商聊聊天号', platform: 'wsl', purpose: 'chat', workspace: 'maibot' },
    { ...account, id: id(6), name: '辣椒聊聊天号', purpose: 'chat', workspace: 'maibot' }
  ]
  const group = { group_id: id(10), name: '我的辣椒群', platform: 'lajiaoliao', version: 1,
    preference_version: 0, active: true, ai_enabled: false, ai_requested: false,
    primary_account_id: account.id, account_name: account.name,
    start_minute: 0, end_minute: 1439, activity: 20, monthly_token_limit: 0 }
  const revokedGroup = { ...group, group_id: id(11), name: '已撤销辣椒群', active: false }
  const chatGroups = [
    { ...group, ai_account_id: id(6), ai_requested: true, account_name: chatAccounts[1].name },
    { ...group, group_id: id(12), name: '旺商聊聊天群', platform: 'wsl', ai_account_id: id(5), account_name: chatAccounts[0].name }
  ]
  const allPanels = [
    { id: 'user', name: '用户中心' }, { id: 'wsl', name: '旺商聊机器人' },
    { id: 'lajiaoliao', name: '辣椒聊机器人' }, { id: 'maibot', name: '热群机器人' }
  ]
  const adminGrants = ['overview:read', 'accounts:read', 'groups:read', 'claims:read',
    'operations:read', 'personas:read', 'workspaces:maibot', 'users:manage', 'plans:manage']
  const tenantGrants = ['overview:read', 'accounts:read', 'groups:read', 'groups:ai', 'claims:read', 'claims:create', 'operations:read']
  let expired = false
  const handler = async route => {
    const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', '')
    const workspace = req.headers()['x-workspace'] || 'wsl'
    let data = []
    if (req.method() !== 'GET') {
      writes.push({ path, method: req.method(), body: req.postDataJSON(), workspace })
      if (path === '/accounts') data = { id: workspace === 'maibot' ? id(6) : account.id }
      else if (path.endsWith('/lajiaoliao-challenge')) data = { challenge: { required: false } }
      else if (path.endsWith('/lajiaoliao-login')) data = { version: 2, auth_state: 'authenticated', native_ready: false }
      else {
        await route.abort('blockedbyclient')
        throw new Error(`Unexpected synthetic write: ${path}`)
      }
    } else if (path === '/me') data = {
      user: { id: id(tenant ? 99 : 1), username: 'synthetic', role: tenant ? 'user' : 'superadmin', tenant_id: tenant ? id(98) : '', ...(tenant ? { tenant_role: 'owner' } : {}) },
      permissions: tenant ? tenantGrants : adminGrants
    }
    else if (path === '/workspaces') data = { items: tenant ? allPanels.filter(p => ['user', 'lajiaoliao'].includes(p.id)) : allPanels }
    else if (path === '/accounts') data = workspace === 'lajiaoliao' ? [account] : workspace === 'maibot' ? chatAccounts : []
    else if (path === '/groups') data = workspace === 'lajiaoliao' ? [group] : workspace === 'maibot' ? chatGroups : []
    else if (path === '/operations') data = workspace === 'lajiaoliao'
      ? [{ id: id(80), account_id: account.id, status: 'succeeded' }] : []
    else if (path === '/personas') data = [{ account_id: id(5), nickname: '已发布聊天人设', published: true, platform: 'wsl' }]
    else if (path.endsWith('/maibot-settings/chat')) data = { version: 1, config: { enabled: false, reply_probability: 0.2, cooldown_seconds: 30, start_hour: 0, end_hour: 24 } }
    else if (path.endsWith('/dashboard')) {
      const current = [account, ...chatAccounts].find(a => path === `/accounts/${a.id}/dashboard` && a.workspace === workspace)
      if (!current) throw new Error('Dashboard escaped the selected account scope')
      data = { account: current, sampled_at: new Date().toISOString(), visibility: 'account',
        groups: { authorized: 1, management_enabled: 0, ai_enabled: 0, ai_pending: 0 },
        operations: { queued: 0, running: 0, unknown: 0, failed_15m: 0, oldest_queued_at: null },
        inbox: { pending: 0, unresolved: 0, quarantined: 0, oldest_pending_at: null },
        performance: { window_seconds: 60, inserted_60s: 0, per_second: 0, latency_samples: 0,
          invalid_timing_samples: 0, p50_ms: null, p95_ms: null, p99_ms: null, last_inserted_at: null, series: [] },
        health: { status: 'attention', reasons: ['runtime_unobserved'] }, backlog: { status: 'clear', reasons: [] },
        backlog_threshold_seconds: 30, runtime_observed: false, capacity: { bound: 1, limit: 30, over_capacity: false } }
    }
    else if (path === '/overview') data = { accounts: workspace === 'lajiaoliao' ? 1 : workspace === 'maibot' ? 2 : 0,
      groups: workspace === 'lajiaoliao' ? 1 : workspace === 'maibot' ? 2 : 0, ai_requests: workspace === 'maibot' ? 1 : 0,
      ai_groups: 0, pending_claims: 0, limits: { accounts: 10, groups: 300, ai_groups: 300 }, native_ready: false, ai_ready: false }
    else if (path === '/native-deployment') {
      deployments.push(workspace)
      data = { configured: true, crypto_ready: true, login_adapter: 'desktop', supported_protocol: '2.8.3' }
    }
    else if (path === '/user-center') data = !tenant ? { platform_admin: true } : { subscription: {
      name: '辣椒套餐', expires_at: '2027-01-01T00:00:00Z', active: !expired, group_limit: 30,
      ai_group_limit: 5, monthly_ai_tokens: 10000, platforms: ['lajiaoliao'], modules: ['ai', 'moderation']
    }, groups: [group, revokedGroup], personas: [{ id: id(3), name: '已发布人设', platform: 'lajiaoliao' }],
      permissions: tenantGrants, claims: [], operations: [], token_usage: null }
    else if (path === '/user-center/groups') {
      const query = new URL(req.url()).searchParams
      const rows = tenant ? [group, revokedGroup].filter(row =>
        row.platform === query.get('platform') && row.name.includes(query.get('search') || '')) : []
      data = { items: rows, total: rows.length, page: 1, page_size: 20 }
    }
    else if (path === '/user-center/usage') data = { call_count: 0, actual_micro: 0, held_micro: 0 }
    else if (path.endsWith('/chat-runtime')) data = { runtime_state: 'blocked',
      blocked_reason: 'heat_chat_platform_not_admitted', runtime_ready: false }
    else if (path.endsWith('/features') || path === '/features/catalog') data = { group_id: group.group_id,
      version: 1, active: true, items: [], native_capabilities: [], native_ready: false }
    else if (path.endsWith('/receiver')) data = { enabled: false, revision: 1 }
    await route.fulfill({ status: req.method() === 'POST' && path === '/accounts' ? 201 : 200,
      json: { code: 0, data, msg: '' } })
  }
  const picker = () => page.getByRole('dialog', { name: '选择平台', exact: true })
  const openPicker = async () => {
    const toggle=page.getByRole('button',{name:'切换工作台',exact:true})
    // Closed mobile navigation is hidden from the accessibility tree.
    if (!await toggle.isVisible()) await page.getByRole('button',{name:'展开导航',exact:true}).click()
    await toggle.click()
  }
  const switchTo = async name => {
    if (name === '用户中心') {
      await page.getByRole('button', { name: '切换到用户中心', exact: true }).click()
      return
    }
    await openPicker()
    await picker().getByRole('button', { name, exact: true }).click()
  }
  // ResizeObserver schedules chart sizing in the next animation frame.
  const settleCharts = () => page.evaluate(() => new Promise(resolve =>
    requestAnimationFrame(() => requestAnimationFrame(resolve))))
  const addPicker = () => page.getByRole('dialog', { name: '添加机器人', exact: true })
  const openAdd = async () => {
    if (await page.locator('.native-login-entry').isVisible()) await page.locator('.native-login-entry').click()
    else {
      await page.getByRole('button', { name: '更多操作', exact: true }).click()
      await page.getByRole('menuitem', { name: /^(账号登录|注册 \/ 登录)$/ }).click()
    }
    await addPicker().getByRole('button', { name: '群管机器人', exact: true }).waitFor()
    await page.waitForTimeout(350)
    if (!await addPicker().locator('img').evaluateAll(images => images.every(img => img.complete && img.naturalWidth > 0)))
      throw new Error('Robot purpose logos did not load')
  }
  const chooseAdd = async (purpose, platform) => {
    await addPicker().getByRole('button', { name: purpose === 'chat' ? '热群机器人' : '群管机器人', exact: true }).click()
    await addPicker().getByRole('button', { name: platform === 'wsl' ? '旺商聊' : '辣椒聊', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: platform === 'wsl'
      ? purpose === 'chat' ? '添加热群机器人' : '添加旺商聊机器人'
      : `${purpose === 'chat' ? '添加热群机器人' : '添加群管机器人'} · 辣椒聊登录`, exact: true })
    await dialog.getByLabel('机器人名称', { exact: true }).waitFor()
    return dialog
  }
  await page.route('**/api/v1/**', handler)
  try {
    await page.goto(base)
    await page.evaluate(() => sessionStorage.clear())
    await page.reload()
    await page.emulateMedia({reducedMotion:'reduce'})
    const noMotion = await page.addStyleTag({content:'*,*::before,*::after{transition:none!important;animation:none!important}'})
    await page.getByRole('heading', { name: '群管机器人状态', exact: true }).waitFor()
    for (const width of [1440, 647, 390, 320]) {
      await page.setViewportSize({ width, height: 900 })
      await openPicker()
      for (const p of allPanels.filter(panel => panel.id !== 'user'))
        await picker().getByRole('button', { name: p.name, exact: true }).waitFor()
      if (await picker().getByRole('button', { name: '用户中心', exact: true }).count())
        throw Error('Console selection leaked into platform selection')
      const bounds = await picker().evaluate(el => {
        const r = (el.querySelector('.workspace-picker') || el).getBoundingClientRect()
        return { width: r.width, height: r.height, left: r.left, right: r.right, bottom: r.bottom,
          viewport: innerWidth, viewportHeight: innerHeight, pageWidth: document.documentElement.scrollWidth }
      })
      await page.screenshot({ path: `output/playwright/platform-picker-${width}.png` })
      if (bounds.width > 344 || bounds.height > 300 || bounds.left < 0 || bounds.right > width ||
        bounds.bottom > bounds.viewportHeight || bounds.pageWidth > width)
        throw new Error(`Panel picker overflow: ${width}: ${JSON.stringify(bounds)}`)
      await picker().getByRole('button', { name: '旺商聊机器人', exact: true }).click()
    }
    await page.setViewportSize({ width: 1440, height: 960 })
    await switchTo('辣椒聊机器人')
    const chiliWorkbench = page.locator('[data-workbench="lajiaoliao"]')
    await chiliWorkbench.getByRole('button', { name: `查看账号 ${account.name}`, exact: true }).waitFor()
    await chiliWorkbench.getByText('1 条记录 · 非历史累计或全平台成功率', { exact: true }).waitFor()
    await chiliWorkbench.getByRole('button', { name: '原生能力与模块', exact: true }).waitFor()
    for (const width of [320, 390, 647, 1440]) {
      await page.setViewportSize({ width, height: 779 })
      await chiliWorkbench.getByText('序号持久化，无网络 ACK', { exact: true }).waitFor()
      await settleCharts()
      if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) {
        const overflow = await page.evaluate(() => [...document.querySelectorAll('main *')].map(el => {
          const r = el.getBoundingClientRect()
          return { tag: el.tagName, class: el.className, left: r.left, right: r.right }
        }).filter(r => r.right > innerWidth && r.left >= 0).slice(0, 12))
        await page.screenshot({ path: `output/playwright/chili-overflow-${width}.png`, fullPage: true })
        throw new Error(`Chili workbench overflow: ${width}: ${JSON.stringify(overflow)}`)
      }
      await page.screenshot({ path: `output/playwright/chili-workbench-${width}.png`, fullPage: true })
    }
    await page.setViewportSize({ width: 1440, height: 960 })
    if (writes.length) throw new Error('Workbench views triggered native or business writes')
    // Keep real leave animations here: the regression was a delayed close event.
    await noMotion.evaluate(el => el.remove())
    await page.emulateMedia({reducedMotion:'no-preference'})
    for (const width of [320, 390, 647, 1440]) {
      await page.setViewportSize({ width, height: 779 })
      for (const purpose of ['management', 'chat']) for (const platform of ['wsl', 'lajiaoliao']) {
        await openAdd()
        const managementIcon = addPicker().getByRole('button', { name: '群管机器人', exact: true }).locator('img')
        if (await managementIcon.getAttribute('src') !== '/robot.svg' ||
          !await managementIcon.evaluate(el => el.complete && el.naturalWidth > 0))
          throw new Error('Management purpose did not show the offline robot icon')
        const pickerBounds = await addPicker().evaluate(el => {
          const r = (el.querySelector('.robot-add-picker') || el).getBoundingClientRect()
          return { width: r.width, height: r.height, left: r.left, right: r.right }
        })
        if (pickerBounds.width > 344 || pickerBounds.height > 300 || pickerBounds.left < 0 || pickerBounds.right > width)
          throw new Error(`Add picker overflow: ${JSON.stringify(pickerBounds)}`)
        if (purpose === 'chat' && platform === 'lajiaoliao')
          await page.screenshot({ path: `output/playwright/robot-add-picker-${width}.png` })
        const before = deployments.length
        const dialog = await chooseAdd(purpose, platform)
        await dialog.getByLabel('机器人名称', { exact: true }).fill(`保留草稿-${purpose}-${platform}`)
        await page.waitForTimeout(650)
        if (!await dialog.isVisible() || await dialog.getByLabel('机器人名称', { exact: true }).inputValue() !== `保留草稿-${purpose}-${platform}`)
          throw new Error('Picker leave animation closed or reset the login form')
        if (platform === 'wsl' && !deployments.slice(before).includes(purpose === 'chat' ? 'maibot' : 'wsl'))
          throw new Error('Desktop login configuration used the view workspace instead of the selected purpose')
        if (!await dialog.evaluate(el => {
          const r = (el.querySelector('.el-dialog') || el).getBoundingClientRect()
          return r.left >= 0 && r.right <= innerWidth && r.top >= 0 && r.bottom <= innerHeight
        })) throw new Error(`Login dialog overflow: ${width}/${purpose}/${platform}`)
        if (width === 390 && purpose === 'chat')
          await page.screenshot({ path: `output/playwright/robot-add-chat-${platform}-390.png` })
        await dialog.getByRole('button', { name: platform === 'wsl' ? '关闭面板' : '关闭', exact: true }).click()
        await dialog.waitFor({state:'hidden'})
      }
    }
    if (writes.length) throw new Error('Selecting purpose/platform unexpectedly created or logged in an account')
    await page.setViewportSize({ width: 1440, height: 960 })
    await openAdd()
    await addPicker().getByRole('button', { name: '热群机器人', exact: true }).click()
    await addPicker().getByRole('button', { name: '返回选择用途', exact: true }).click()
    await addPicker().getByRole('button', { name: '群管机器人', exact: true }).waitFor()
    await addPicker().locator('.el-dialog__headerbtn').click()
    if (writes.length) throw new Error('Backing out of robot selection wrote an account')
    await openAdd()
    await chooseAdd('management', 'lajiaoliao')
    await page.getByLabel('机器人名称', { exact: true }).fill('新建辣椒号')
    await page.getByLabel('辣椒聊手机号', { exact: true }).fill('13800000000')
    await page.getByLabel('原生账号密码', { exact: true }).fill('synthetic-password-only')
    await page.getByRole('button', { name: '验证并登录', exact: true }).click()
    await page.getByText('会话已加密保存；消息接收尚未开启', { exact: true }).waitFor()
    if (writes.filter(w => w.path.endsWith('/lajiaoliao-login')).length !== 1 ||
      writes.some(w => w.path.includes('native-auth') || w.path.includes('receiver'))) throw new Error('Wrong adapter or implicit reception')
    await page.getByRole('dialog', { name: /辣椒聊登录/ }).getByRole('button', { name: '关闭', exact: true }).click()
    await switchTo('热群机器人')
    const chatWorkbench = page.locator('[data-workbench="maibot"]')
    await chatWorkbench.getByText('未提供操作记录', { exact: true }).waitFor()
    await chatWorkbench.getByRole('button', { name: '查看账号 旺商聊聊天号', exact: true }).waitFor()
    await chatWorkbench.getByRole('button', { name: '查看账号 辣椒聊聊天号', exact: true }).waitFor()
    await chatWorkbench.getByText('未开放', { exact: true }).waitFor()
    for (const width of [320, 390, 647, 1440]) {
      await page.setViewportSize({ width, height: 779 })
      await settleCharts()
      if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error(`Chat workbench overflow: ${width}`)
      await page.screenshot({ path: `output/playwright/chat-workbench-${width}.png`, fullPage: true })
    }
    await page.setViewportSize({ width: 1440, height: 960 })
    // Create in the management pool while viewing chat; the view must stay put.
    await openAdd()
    await chooseAdd('management', 'lajiaoliao')
    await page.getByLabel('机器人名称', { exact: true }).fill('从热群面板添加群管')
    await page.getByLabel('辣椒聊手机号', { exact: true }).fill('13800000000')
    await page.getByLabel('原生账号密码', { exact: true }).fill('synthetic-password-only')
    await page.getByRole('button', { name: '验证并登录', exact: true }).click()
    await page.getByText('会话已加密保存；消息接收尚未开启', { exact: true }).waitFor()
    if (!await chatWorkbench.isVisible() || writes.slice(-3).some(w => w.workspace !== 'lajiaoliao'))
      throw new Error('Cross-pool creation switched the view or used the wrong account pool')
    await page.getByRole('dialog', { name: /辣椒聊登录/ }).getByRole('button', { name: '关闭', exact: true }).click()
    await openAdd()
    await chooseAdd('chat', 'lajiaoliao')
    await page.getByLabel('机器人名称', { exact: true }).fill('独立辣椒聊天号')
    await page.getByLabel('辣椒聊手机号', { exact: true }).fill('13900000000')
    await page.getByLabel('原生账号密码', { exact: true }).fill('synthetic-password-only')
    await page.getByRole('button', { name: '验证并登录', exact: true }).click()
    await page.getByText('会话已加密保存；消息接收尚未开启', { exact: true }).waitFor()
    if (writes.slice(-3).some(w => w.workspace !== 'maibot')) throw new Error('Chat login escaped its independent pool')
    await page.getByRole('dialog', { name: /辣椒聊登录/ }).getByRole('button', { name: '关闭', exact: true }).click()
    await page.getByRole('combobox', { name: '机器人面板', exact: true }).locator('xpath=ancestor::div[contains(@class, "el-select__wrapper")]').click()
    await page.getByRole('option', { name: '工作台总览', exact: true }).click()
    await chatWorkbench.getByRole('button', { name: '聊天配置', exact: true }).click()
    await page.getByRole('combobox', { name: '机器人面板', exact: true }).locator('xpath=ancestor::div[contains(@class, "el-select__wrapper")]').click()
    await page.getByRole('option').filter({ hasText: '旺商聊聊天号' }).click()
    await page.getByRole('spinbutton', { name: '回复概率', exact: true }).fill('0.65')
    await switchTo('辣椒聊机器人')
    await page.getByTestId('account-dashboard').getByRole('heading', { name: account.name, exact: true }).waitFor()
    await page.getByRole('combobox', { name: '机器人面板', exact: true }).locator('xpath=ancestor::div[contains(@class, "el-select__wrapper")]').click()
    await page.getByRole('option', { name: '工作台总览', exact: true }).click()
    await chiliWorkbench.getByRole('heading', { name: '群管机器人状态', exact: true }).waitFor()
    await switchTo('热群机器人')
    await page.getByRole('spinbutton', { name: '回复概率', exact: true }).waitFor()
    if (await page.getByRole('spinbutton', { name: '回复概率', exact: true }).inputValue() !== '0.65') throw new Error('Chat draft lost after switching workbench')
    if (writes.some(w => w.path.includes('/maibot-settings') || w.path.includes('/receiver'))) throw new Error('View switch activated native reception or saved chat config')
    await switchTo('用户中心')
    await page.locator('.user-center').waitFor()
    if (await page.locator('.native-login-entry').count())
      throw Error('User console exposed native credential entry')
    await page.getByRole('button', { name: '切换到管理后台', exact: true }).click()
    await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '用户管理', exact: true }).click()
    await page.getByRole('heading', { name: '用户管理', exact: true }).waitFor()
    tenant = true
    await page.evaluate(() => sessionStorage.clear())
    await page.reload()
    await page.getByRole('heading', { name: '用户中心', exact: true }).waitFor()
    await page.addStyleTag({content:'*,*::before,*::after{transition:none!important;animation:none!important}'})
    await page.getByText('辣椒套餐', { exact: true }).waitFor()
    await page.getByRole('textbox', { name: '搜索我的群组', exact: true }).fill('我的')
    await page.getByText('1 个群组', { exact: true }).waitFor()
    for (const width of [320, 390, 647, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error(`User center overflow: ${width}`)
      await page.screenshot({ path: `output/playwright/user-center-${width}.png`, fullPage: true })
    }
    await page.getByRole('button', { name: '热群配置', exact: true }).click()
    await page.getByRole('dialog', { name: '本群热群配置', exact: true }).waitFor()
    await page.getByText('本平台基础聊天尚未开放', { exact: true }).waitFor()
    if (writes.some(w => w.path.includes('/chat-runtime') || w.path.includes('/preferences')))
      throw Error('Viewing blocked runtime submitted preferences or activation')
    for (const width of [320, 390, 647, 1440]) {
      await page.setViewportSize({ width, height: 779 })
      const bounds = await page.getByRole('dialog', { name: '本群热群配置', exact: true }).evaluate(el => {
        const dialog = el.getBoundingClientRect(), footer = el.querySelector('.el-dialog__footer').getBoundingClientRect()
        return { fits: dialog.left >= 0 && dialog.right <= innerWidth && dialog.top >= 0 &&
          dialog.bottom <= innerHeight && footer.bottom <= innerHeight,
          left: dialog.left, right: dialog.right, top: dialog.top, bottom: dialog.bottom, footer: footer.bottom }
      })
      await page.screenshot({ path: `output/playwright/user-preferences-${width}.png` })
      if (!bounds.fits) throw new Error(`Group preferences dialog overflow: ${width}: ${JSON.stringify(bounds)}`)
    }
    await page.getByRole('dialog', { name: '本群热群配置', exact: true }).locator('.el-dialog__headerbtn').click()
    await page.getByRole('textbox', { name: '搜索我的群组', exact: true }).clear()
    // Authorization status is now displayed on each server-filtered group row.
    // Keep the revoked/expired denial checks on those rows.
    const revokedButton = page.locator('.uc-groups').getByRole('row').filter({ hasText: revokedGroup.name })
      .getByRole('button', { name: '热群配置', exact: true })
    await revokedButton.waitFor()
    if (!(await revokedButton.isDisabled())) throw new Error('Revoked group allowed chat configuration')
    expired = true
    await page.getByRole('button', { name: '刷新用户中心', exact: true }).click()
    await page.getByText('套餐未生效或已到期，暂不可申请授权或修改热群配置', { exact: true }).waitFor()
    if (!(await page.locator('.uc-groups').getByRole('row').filter({ hasText: group.name })
      .getByRole('button', { name: '热群配置', exact: true }).isDisabled())) throw new Error('Expired package allowed chat configuration')
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.getByRole('button', { name: '切换工作台', exact: true }).click()
    if (await picker().getByRole('button', { name: '热群机器人', exact: true }).count() ||
      await picker().getByRole('button', { name: '旺商聊机器人', exact: true }).count()) throw new Error('Tenant entered ungranted workspace')
    await picker().getByRole('button', { name: '辣椒聊机器人', exact: true }).click()
    return { passed: true, fourPanels: true, compactPicker: true, scopedWorkbenches: true,
      chatDraftPreserved: true, userGroupFilters: true, revokedAndExpiredBlocked: true,
      tenantPackageScope: true, chatPlatformChoice: true, manualChiliLoginSynthetic: true,
      animatedAddHandoff: true, explicitPurposeAndPlatform: true, crossPoolCreation: true,
      syntheticWrites: writes.length, nativeActions: 0, widths: [320, 390, 647, 1440] }
  } finally {
    await page.unroute('**/api/v1/**', handler)
    await page.evaluate(saved => {
      sessionStorage.clear()
      for (const [key, value] of Object.entries(saved)) sessionStorage.setItem(key, value)
    }, original)
  }
}
