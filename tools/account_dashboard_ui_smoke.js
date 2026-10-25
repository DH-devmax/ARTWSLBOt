// All account metrics are synthetic. Switching panels must send no writes.
// prettier-ignore
async (page) => {
  const base = 'http://127.0.0.1:5173'
  if (new URL(page.url()).origin !== base) throw new Error('Local console only')
  const realMe = (await (await page.request.get(base + '/api/v1/me')).json()).data
  const original = await page.evaluate(() => Object.fromEntries(Object.entries(sessionStorage).filter(([key]) => key.startsWith('wsl:'))))
  const id = (n) => `50000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const accounts = [
    { id: id(1), name: '群管甲', workspace: 'wsl' },
    { id: id(2), name: '群管乙', workspace: 'wsl' },
    { id: id(3), name: '聊天甲', workspace: 'maibot' }
  ].map((a) => ({ ...a, auth_state: 'authenticated', connection_enabled: true, runtime_state: 'connected', version: 1 }))
  const now = new Date(), writes = []
  let fail = false, delay = false, viewer = false, release, entered
  const delayed = new Promise((resolve) => { release = resolve })
  const seen = new Promise((resolve) => { entered = resolve })
  const snapshot = (account) => {
    const count = account.id === id(1) ? 7 : account.id === id(2) ? 19 : 3
    return {
      account, sampled_at: now.toISOString(), visibility: viewer ? 'tenant_groups' : 'account',
      groups: { authorized: count, management_enabled: count - 1, ai_enabled: count - 2, ai_pending: 1 },
      operations: { queued: count, running: 0, unknown: 1, failed_15m: 2, oldest_queued_at: new Date(now - 40000).toISOString() },
      inbox: { pending: count, unresolved: 2, quarantined: 1, oldest_pending_at: new Date(now - 125000).toISOString() },
      performance: {
        window_seconds: 60, inserted_60s: count * 60, per_second: count, latency_samples: count * 60,
        invalid_timing_samples: 0, p50_ms: 8, p95_ms: 19.5, p99_ms: 40,
        last_inserted_at: now.toISOString(),
        series: Array.from({ length: 15 }, (_, i) => ({ at: new Date(now - (14 - i) * 60000).toISOString(), count: count * (i + 1) }))
      },
      health: { status: 'attention', reasons: ['unknown_operations', 'runtime_unobserved'] },
      backlog: { status: 'attention', reasons: ['unknown_operations'] },
      backlog_threshold_seconds: 30, runtime_observed: false
    }
  }
  const pattern = '**/api/v1/**'
  const handler = async (route) => {
    const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', '')
    const workspace = req.headers()['x-workspace'] || 'wsl'
    if (req.method() !== 'GET') {
      writes.push(path)
      await route.abort('blockedbyclient')
      return
    }
    let data = []
    if (path === '/me') data = viewer
      ? { user: { ...realMe.user, role: 'user', tenant_id: id(9) }, permissions: ['overview:read', 'accounts:read'] }
      : realMe
    else if (path === '/accounts') data = accounts.filter((a) => a.workspace === workspace)
    else if (path === '/overview') data = { accounts: 2, groups: 26, limits: { accounts: 10, groups: 100, ai_groups: 100 } }
    else if (path.endsWith('/dashboard')) {
      const account = accounts.find((a) => path === `/accounts/${a.id}/dashboard` && a.workspace === workspace)
      if (!account) throw new Error('Account request scope mismatch')
      data = snapshot(account)
      if (fail) {
        await route.fulfill({ status: 503, json: { code: 1, msg: 'database_unavailable' } })
        return
      }
      if (delay && account.id === id(1)) {
        delay = false
        entered()
        await delayed
      }
    }
    try { await route.fulfill({ json: { code: 0, data, msg: '' } }) }
    catch (e) { if (!String(e).includes('Route is already handled')) throw e }
  }
  const choose = async (name) => {
    await page.getByRole('combobox', { name: '机器人面板', exact: true })
      .locator('xpath=ancestor::div[contains(@class, "el-select__wrapper")]').click()
    await page.getByRole('option').filter({ hasText: name }).click()
  }
  const dashboard = page.getByTestId('account-dashboard')
  const loaded = async (name, groups) => {
    await dashboard.locator('.account-dashboard-heading h2').filter({ hasText: name }).waitFor()
    await dashboard.locator('.account-kpis > div').first().getByText(String(groups), { exact: true }).waitFor()
  }
  await page.route(pattern, handler)
  try {
    await page.evaluate(() => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
    })
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.goto(base + '/#overview')
    await page.reload()
    await page.getByRole('heading', { name: '群管机器人状态', exact: true }).waitFor()
    await choose('群管甲')
    await loaded('群管甲', 7)
    if (await page.getByRole('heading', { name: '验收目标', exact: true }).count() || await page.locator('.overview-grid').count()) throw new Error('Account panel includes platform overview')
    await dashboard.getByText('19.5 ms', { exact: true }).waitFor()
    await dashboard.getByText('实时连接、心跳与 ACK 尚无监测数据', { exact: true }).waitFor()
    for (const viewport of [{ width: 320, height: 740 }, { width: 390, height: 844 }, { width: 647, height: 779 }, { width: 1440, height: 960 }]) {
      await page.setViewportSize(viewport)
      await dashboard.locator('canvas').waitFor()
      await page.waitForFunction(() => {
        const chart = document.querySelector('.account-message-chart'), canvas = chart?.querySelector('canvas')
        return canvas && Math.abs(canvas.clientWidth - chart.clientWidth) < 2
      })
      const fits = await dashboard.evaluate((el) => {
        const rect = el.getBoundingClientRect()
        const chart = el.querySelector('canvas'), pixels = chart.getContext('2d').getImageData(0, 0, chart.width, chart.height).data
        let colored = 0
        for (let i = 0; i < pixels.length; i += 4) if (pixels[i + 3] && pixels[i + 2] > pixels[i] + 30) colored++
        const labelsFit = [...el.querySelectorAll('h2,h3,dt,dd,strong')].every((node) => {
          const b = node.getBoundingClientRect()
          return b.left >= -1 && b.right <= innerWidth + 1 && node.scrollWidth <= node.clientWidth + 1
        })
        return rect.left >= 0 && rect.right <= innerWidth + 1 && document.documentElement.scrollWidth <= innerWidth && colored > 100 && labelsFit
      })
      if (!fits) throw new Error(`Dashboard overflow or blank chart at ${viewport.width}`)
      await page.screenshot({ path: `output/playwright/account-dashboard-${viewport.width}.png`, fullPage: true, animations: 'disabled' })
    }
    fail = true
    await dashboard.getByRole('button', { name: '刷新账号指标', exact: true }).click()
    await dashboard.getByText('采样失败，当前显示上一次数据', { exact: true }).waitFor()
    await loaded('群管甲', 7)
    await choose('群管乙')
    await dashboard.getByText('暂无账号指标', { exact: true }).waitFor()
    if (await dashboard.locator('.account-kpis').count() || await dashboard.getByText('采样失败，当前显示上一次数据', { exact: true }).count()) throw new Error('Failed B fetch reused A sample')
    fail = false
    await choose('群管甲')
    await loaded('群管甲', 7)
    delay = true
    await dashboard.getByRole('button', { name: '刷新账号指标', exact: true }).click()
    await seen
    await choose('群管乙')
    await loaded('群管乙', 19)
    release()
    await page.getByRole('button', { name: '刷新', exact: true }).click()
    await loaded('群管乙', 19)
    await choose('工作台总览')
    await page.getByRole('heading', { name: '群管机器人状态', exact: true }).waitFor()
    await page.getByRole('heading', { name: '验收目标', exact: true }).waitFor()
    if (await dashboard.count()) throw new Error('Overview still shows account panel')
    await page.getByRole('button', { name: '切换工作台', exact: true }).click()
    await page.locator('.brand-name').filter({ hasText: '热群机器人' }).waitFor()
    await choose('聊天甲')
    await loaded('聊天甲', 3)
    viewer = true
    await page.evaluate((owner) => {
      sessionStorage.setItem(`wsl:workspace:${owner}`, 'wsl')
      sessionStorage.setItem(`wsl:account:${owner}:wsl`, '')
    }, realMe.user.id)
    await page.reload()
    await choose('群管甲')
    await loaded('群管甲', 7)
    await dashboard.getByText('仅本租户授权群', { exact: true }).waitFor()
    if (await dashboard.getByRole('button', { name: /^删除机器人/ }).count() ||
      await dashboard.getByRole('button', { name: '注册 / 登录', exact: true }).count()) throw new Error('Tenant sees native account controls')
    if (writes.length) throw new Error('Panel views mutated a connection')
    return { passed: true, scope: ['account', 'workspace', 'tenant'], lateResponseIgnored: true, staleScopeSafe: true, chartPixelCheck: true, viewports: [320, 390, 647, 1440], writes: 0 }
  } finally {
    release()
    await page.unroute(pattern, handler)
    await page.evaluate((saved) => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
      for (const [key, value] of Object.entries(saved)) sessionStorage.setItem(key, value)
    }, original)
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.goto(base + '/#overview')
    await page.reload()
  }
}
