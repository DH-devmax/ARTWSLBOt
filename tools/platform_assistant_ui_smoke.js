// The retired platform-chat URL must never create sessions or model calls.
async (page) => {
  const base = 'http://127.0.0.1:5173'
  const id = n => `be000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  let admin = true, settingsReads = 0, extensionSettingsReads = 0
  const writes = [], forbidden = [], screenshots = []
  const handler = async route => {
    const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', '')
    const workspace = req.headers()['x-workspace'] || 'wsl'
    if (path.startsWith('/platform-assistant')) {
      forbidden.push(path)
      return route.abort('blockedbyclient')
    }
    if (req.method() !== 'GET') {
      writes.push(path)
      return route.abort('blockedbyclient')
    }
    let data = []
    if (path === '/me') data = {
      user: { id: id(2), role: admin ? 'superadmin' : 'user', tenant_id: admin ? undefined : id(3) },
      permissions: ['overview:read', 'accounts:read', 'groups:read', 'plans:manage', 'audit:read', 'assistant:use', 'workspaces:maibot']
    }
    else if (path === '/workspaces') data = { items: [
      { id: 'wsl', name: '旺商聊机器人' },
      ...(admin ? [{ id: 'maibot', name: '热群机器人' }] : [])
    ] }
    else if (path === '/accounts') data = workspace === 'maibot' ? [
      { id: id(1), name: '合成聊天账号', platform: 'wsl', workspace: 'maibot' }
    ] : []
    else if (path === '/assistant/settings') {
      settingsReads++
      data = { enabled: false, tenant_access: false, approvals_enabled: false, version: 7 }
    }
    else if (path === '/assistant/health') data = { sampled_at: new Date().toISOString() }
    else if (path === '/maibot/capabilities') data = { engine_ready: false, items: [
      { id: 'chat', name: '聊天配置', status: 'configuration_only', scope: 'account' },
      ...['learning', 'memory', 'people', 'emoji', 'plugins'].map((key, index) => ({
        id: key, name: ['表达与黑话学习', '长期记忆与向量检索', '人物与印象', '表情与媒体记忆', '插件与工具'][index],
        status: index < 2 ? 'configuration_only' : index === 4 ? 'isolation_blocked' : 'engine_pending',
        scope: index === 4 ? 'superadmin' : 'group_required'
      }))
    ] }
    else if (path === `/accounts/${id(1)}/maibot-settings/memory`) {
      extensionSettingsReads++
      data = { version: 1, config: { enabled: false, top_k: 5, derived_retention_days: 30 }, applied: false, runtime_ready: false }
    }
    else if (path === '/overview') data = { accounts: 0, groups: 0, limits: { accounts: 10, groups: 300 } }
    else if (path === '/user-center') data = { groups: [], permissions: ['groups:read'], claims: [], operations: [] }
    else if (path === '/user-center/groups') data = { items: [], total: 0 }
    else if (path === '/user-center/usage') data = { call_count: 0 }
    await route.fulfill({ json: { code: 0, msg: '', data } })
  }
  await page.route('**/api/v1/**', handler)
  const remember = workspace => page.evaluate(({ user, account, workspace }) => {
    sessionStorage.clear()
    sessionStorage.setItem(`wsl:workspace:${user}`, workspace)
    if (workspace === 'maibot') sessionStorage.setItem(`wsl:account:${user}:maibot`, account)
  }, { user: id(2), account: id(1), workspace })
  const screenshot = async (name, width) => {
    await page.setViewportSize({ width, height: 960 })
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1))
      throw new Error(`${name} overflow at ${width}`)
    const path = `output/playwright/${name}-${width}.png`
    await page.screenshot({ path, fullPage: true, animations: 'disabled' })
    screenshots.push(path)
  }
  try {
    await page.goto(base)
    await remember('wsl')
    await page.goto(base + '/?fixture=operator#platform-assistant')
    await page.getByTestId('assistant-governance').waitFor()
    await page.waitForFunction(() => location.hash === '#assistant-admin')
    const nav = page.getByRole('navigation', { name: '主导航' })
    // The group assistant entry moved to the user console. The admin console
    // retains audit only, with no retired platform-chat launcher.
    if (await page.locator('#primary-sidebar > .sidebar-assistant').count())
      throw new Error('Admin console exposed a platform-chat launcher')
    if (await nav.getByRole('button', { name: '超管助手', exact: true }).count())
      throw new Error('Retired platform chat still visible')
    for (const width of [320, 390, 647, 1440]) await screenshot('assistant-audit', width)
    const reads = settingsReads
    admin = false
    await remember('wsl')
    await page.goto(base + '/?fixture=tenant#platform-assistant')
    await page.waitForFunction(() => location.hash === '#overview')
    await page.getByRole('heading', { name: '用户中心', exact: true }).waitFor()
    if (settingsReads !== reads || await page.getByTestId('assistant-governance').count())
      throw new Error('Tenant accessed operator audit')

    admin = true
    await remember('maibot')
    await page.goto(base + '/?fixture=chat#platform-assistant')
    await page.waitForFunction(() => location.hash === '#overview')
    await page.getByRole('heading', { name: '聊天机器人状态', exact: true }).waitFor()
    if (settingsReads !== reads) throw new Error('Legacy URL fetched WSL audit in chat scope')
    await page.goto(base + '/#ai-plugins')
    await page.getByRole('listitem').filter({ hasText: '长期记忆与向量检索' }).waitFor()
    await nav.getByRole('heading', { name: '可选扩展', exact: true }).waitFor({ state: 'attached' })
    if (await nav.getByRole('button', { name: '长期记忆', exact: true }).count())
      throw new Error('Extension still appears as a core service')
    const switches = page.locator('.ai-console').getByRole('switch')
    if (await switches.count() !== 5) throw new Error('Extension inventory mismatch')
    for (const toggle of await switches.all())
      if (await toggle.isEnabled() || await toggle.isChecked())
        throw new Error('Unimplemented extension can be activated')
    if (extensionSettingsReads) throw new Error('Extension catalog loaded per-extension settings eagerly')
    for (const width of [320, 390, 647, 1440]) await screenshot('extensions', width)
    await page.getByRole('listitem').filter({ hasText: '长期记忆与向量检索' })
      .getByRole('button', { name: '账号默认设置', exact: true }).click()
    await page.getByText('仅保存账号默认值；扩展尚未接入，当前不运行学习或检索。', { exact: true }).waitFor()
    if (extensionSettingsReads !== 1 || writes.length || forbidden.length)
      throw new Error('Catalog navigation caused side effects or legacy model requests')
    return {
      passed: true, synthetic_only: true, retired_deep_link: true, tenant_denied: true,
      disabled_extensions: 5, lazy_configuration: true, model_calls: 0, native_actions: 0,
      writes: 0, viewports: [320, 390, 647, 1440], screenshots
    }
  } finally { await page.unroute('**/api/v1/**', handler) }
}
