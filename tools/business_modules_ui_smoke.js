// Synthetic navigation and business projections only; all writes are rejected.
async (page) => {
  const base = 'http://127.0.0.1:5173'
  const id = n => `be000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const accounts = [1, 2].map(n => ({ id: id(n), name: `业务机器人${n}`, workspace: 'wsl' }))
  const groups = [1, 2].map(n => ({ group_id: id(n + 10), primary_account_id: id(n), name: `业务群${n}`, platform: 'wsl', active: true, version: 1 }))
  const definitions = [
    { id: 'lottery', name: '活动抽奖', category: '活动', detail: 'activities', status: 'web_available', fields: [], native_dependencies: [] },
    { id: 'ledger', name: '财务台账', category: '财务', detail: 'ledger', status: 'web_available', fields: [], native_dependencies: [] }
  ]
  const original = await page.evaluate(() => Object.fromEntries(Object.entries(sessionStorage).filter(([key]) => key.startsWith('wsl:'))))
  let mode = 'full', actor = id(99), reads = [], writes = 0, delayedLedger
  const pending = new Set()
  const delayNextLedger = () => {
    let entered, release, finished
    const started = new Promise(resolve => { entered = resolve })
    const gate = new Promise(resolve => { release = resolve })
    const completed = new Promise(resolve => { finished = resolve })
    const delayed = { entered, release, finished, gate, started, completed }
    delayedLedger = delayed
    pending.add(delayed)
    return delayed
  }
  const permissions = () => mode === 'full'
    ? ['overview:read', 'groups:read', 'accounts:read', 'ledger:read', 'ledger:create', 'activities:read', 'activities:create']
    : mode === 'ledger' ? ['overview:read', 'ledger:read']
      : mode === 'activities' ? ['overview:read', 'activities:read'] : ['overview:read']
  const handler = async route => {
    const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', '')
    if (req.method() !== 'GET') {
      writes++
      return route.fulfill({ status: 403, json: { code: 1, msg: 'synthetic_writes_forbidden' } })
    }
    reads.push(path)
    let data = []
    if (path === '/me') data = { user: { id: actor, username: 'synthetic-business', role: 'user', tenant_id: id(98) }, permissions: permissions() }
    else if (path === '/workspaces') data = { items: [{ id: 'wsl', name: '旺商聊机器人' }, { id: 'lajiaoliao', name: '辣椒聊机器人' }] }
    else if (path === '/accounts') data = accounts
    else if (path === '/groups') data = groups
    else if (path === '/user-center') data = { groups, permissions: permissions(), subscription: {
      active: true, platforms: ['wsl'], modules: ['finance', 'activities'], group_limit: 2
    }, claims: [], operations: [] }
    else if (path === '/user-center/groups') data = { items: mode === 'full' ? groups : [], total: mode === 'full' ? groups.length : 0 }
    else if (path === '/user-center/usage') data = { call_count: 0 }
    else if (path === '/features/catalog') data = { items: definitions, native_capabilities: [], platform: 'wsl' }
    else if (/^\/groups\/[^/]+\/features$/.test(path)) {
      const group = groups.find(group => group.group_id === path.split('/')[2])
      data = { group_id: group.group_id, account_id: group.primary_account_id, version: 1, platform: 'wsl',
        native_capabilities: [], items: definitions.map(definition => ({ definition, values: {}, editable_fields: [], entitled: true, effective_enabled: false })) }
    } else if (path === '/ledger') data = groups.map((group, index) => ({ id: id(20 + index), group_id: group.group_id, member_id: `成员${index + 1}`, kind: 'reward', amount_cents: 100, external_key: `fixture-${index}`, created_at: '2026-10-31T01:00:00Z' }))
    else if (path === '/activities') data = groups.map((group, index) => ({ id: id(30 + index), group_id: group.group_id, name: `合成活动${index + 1}`, participant_count: 0, winners: 1, state: 'open', closes_at: '2026-11-30T01:00:00Z' }))
    else if (path === '/overview') data = { accounts: 2, groups: 2, limits: { accounts: 10, groups: 300 } }
    if (path === '/ledger' && delayedLedger) {
      const delayed = delayedLedger
      delayedLedger = undefined
      delayed.entered()
      await delayed.gate
      data = data.map(row => ({ ...row, member_id: '迟到台账' }))
      await route.fulfill({ json: { code: 0, msg: '', data } })
      pending.delete(delayed)
      delayed.finished()
      return
    }
    await route.fulfill({ json: { code: 0, msg: '', data } })
  }
  await page.addInitScript(() => {
    const originalFetch = window.fetch.bind(window)
    window.fetch = (url, init) => originalFetch(url,
      new URL(url, location.href).pathname === '/api/v1/ledger' && init?.signal
        ? { ...init, signal: undefined } : init)
  })
  await page.route('**/api/v1/**', handler)
  try {
  const boot = async (hash = 'features', workspace = 'wsl', account = '') => {
    await page.evaluate(({ user, workspace, account }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:workspace:${user}`, workspace)
      sessionStorage.setItem(`wsl:account:${user}:${workspace}`, account)
    }, { user: actor, workspace, account })
    await page.goto('about:blank')
    await page.goto(base + '/#' + hash)
    await page.waitForFunction(() => !document.querySelector('.boot-state'))
  }
  const moduleRow = name => page.getByTestId('feature-center').getByRole('row').filter({ hasText: name })
  const chooseGroup = async (label, name) => {
    await page.getByRole('combobox', { name: label, exact: true }).locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option', { name, exact: true }).click()
  }
  await page.setViewportSize({ width: 1440, height: 960 })
  await boot()
  await page.locator('.uc-groups').getByRole('row').filter({ hasText: '业务群1' })
    .getByRole('button', { name: '群功能', exact: true }).click()
  await moduleRow('财务台账').waitFor()
  const nav = page.getByRole('navigation', { name: '主导航' })
  for (const name of ['活动抽奖', '财务台账']) {
    if (await nav.getByRole('button', { name, exact: true }).count()) throw new Error('Duplicate sidebar entry')
    if (await page.getByRole('tab', { name, exact: true }).count()) throw new Error('Business detail is still a parallel tab')
    if (await moduleRow(name).getByRole('checkbox').count()) throw new Error('Manual business acquired an activation switch')
  }
  await moduleRow('财务台账').getByRole('button', { name: '进入管理', exact: true }).click()
  await page.getByRole('dialog', { name: '财务台账', exact: true }).waitFor()
  await page.getByRole('cell', { name: '成员1', exact: true }).waitFor({ timeout: 3000 }).catch(async () => {
    await page.screenshot({ path: 'output/playwright/business-modules-ledger-empty.png', fullPage: true })
    throw Error(reads.includes('/ledger')
      ? 'Scoped ledger fetched records but did not display the selected group'
      : 'Scoped ledger opened without fetching /ledger; saved records are absent')
  })
  if (await page.getByRole('cell', { name: '成员2', exact: true }).count()) throw new Error('Selected group was lost')
  if (!page.url().endsWith('#ledger')) throw new Error('Business dialog lost its selected route')
  await page.getByRole('button', { name: '新增记录', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '新增记录', exact: true })
  await dialog.getByRole('combobox', { name: '业务操作群组', exact: true }).waitFor()
  if (!await dialog.getByRole('combobox', { name: '业务操作群组', exact: true }).locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').innerText().then(value => value.includes('业务群1'))) throw new Error('New ledger lost selected group')
  await dialog.getByRole('button', { name: '取消', exact: true }).click()
  await chooseGroup('业务记录群组', '业务群2')
  await page.getByRole('cell', { name: '成员2', exact: true }).waitFor()
  if (await page.getByRole('cell', { name: '成员1', exact: true }).count()) throw new Error('Group filter did not update')
  const lateGroup = delayNextLedger()
  await chooseGroup('业务记录群组', '业务群1')
  await lateGroup.started
  await chooseGroup('业务记录群组', '业务群2')
  await page.getByRole('cell', { name: '成员2', exact: true }).waitFor()
  lateGroup.release()
  await lateGroup.completed
  await page.waitForTimeout(100)
  if (await page.getByRole('cell', { name: '迟到台账', exact: true }).count()) throw new Error('Late ledger crossed group scope')
  await page.getByRole('cell', { name: '成员2', exact: true }).waitFor()
  const lateModule = delayNextLedger()
  await chooseGroup('业务记录群组', '业务群1')
  await lateModule.started
  await page.getByRole('button', { name: '返回业务模块', exact: true }).click()
  await moduleRow('活动抽奖').waitFor()
  await chooseGroup('功能配置群组', '业务群2')
  await moduleRow('活动抽奖').getByRole('button', { name: '进入管理', exact: true }).click()
  await page.getByRole('dialog', { name: '活动抽奖', exact: true }).waitFor()
  await page.getByRole('cell', { name: '合成活动2', exact: true }).waitFor()
  lateModule.release()
  await lateModule.completed
  await page.waitForTimeout(100)
  if (await page.getByRole('dialog', { name: '财务台账', exact: true }).count()) throw new Error('Late ledger reopened a closed dialog')
  await page.getByRole('cell', { name: '合成活动2', exact: true }).waitFor()
  await page.getByRole('button', { name: '创建活动', exact: true }).click()
  const activity = page.getByRole('dialog', { name: '创建活动', exact: true })
  if (!await activity.locator('.el-select').innerText().then(value => value.includes('业务群2'))) throw new Error('Activity lost selected group')
  await activity.getByRole('button', { name: '取消', exact: true }).click()
  for (const width of [320, 390, 647, 1440]) {
    await page.setViewportSize({ width, height: 900 })
    await page.waitForTimeout(100)
    const bounds = await page.getByRole('dialog', { name: '活动抽奖', exact: true }).boundingBox()
    if (!bounds || bounds.x < 0 || bounds.x + bounds.width > width + 1 || bounds.y < 0 || bounds.y + bounds.height > 901) throw new Error('Business dialog overflow ' + width)
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)) throw new Error('Business detail overflow ' + width)
    await page.screenshot({ path: `output/playwright/business-modules-${width}.png`, fullPage: true, animations: 'disabled' })
  }
  await page.setViewportSize({ width: 1440, height: 960 })
  await chooseGroup('业务记录群组', '业务群1')
  await page.getByRole('button', { name: '返回业务模块', exact: true }).click()
  await boot('features', 'wsl', id(2))
  await page.locator('.uc-groups').getByRole('row').filter({ hasText: '业务群2' })
    .getByRole('button', { name: '群功能', exact: true }).click()
  await moduleRow('活动抽奖').getByRole('button', { name: '进入管理', exact: true }).click()
  const accountGroup = page.getByRole('combobox', { name: '业务记录群组', exact: true })
    .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]')
  await accountGroup.hover()
  await accountGroup.locator('.el-select__clear').click()
  await page.getByRole('cell', { name: '合成活动2', exact: true }).waitFor()
  if (await page.getByRole('cell', { name: '合成活动1', exact: true }).count()) throw new Error('Business scope crossed account')
  await page.getByRole('button', { name: '返回业务模块', exact: true }).click()
  const lateActor = delayNextLedger()
  await moduleRow('财务台账').getByRole('button', { name: '进入管理', exact: true }).click()
  await lateActor.started
  await page.getByRole('button', { name: '返回业务模块', exact: true }).click()
  actor = id(97)
  await page.getByRole('button', { name: '刷新', exact: true }).click()
  await page.getByRole('heading', { name: '用户中心', exact: true }).waitFor()
  await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '群功能配置', exact: true }).click()
  await page.locator('.uc-groups').getByRole('row').filter({ hasText: '业务群2' })
    .getByRole('button', { name: '群功能', exact: true }).click()
  await moduleRow('财务台账').getByRole('button', { name: '进入管理', exact: true }).click()
  await page.getByRole('cell', { name: '成员2', exact: true }).waitFor()
  lateActor.release()
  await lateActor.completed
  await page.waitForTimeout(100)
  if (await page.getByRole('cell', { name: '迟到台账', exact: true }).count()) throw new Error('Late ledger crossed actor scope')
  await page.getByRole('cell', { name: '成员2', exact: true }).waitFor()
  for (const entry of [{ mode: 'ledger', label: '财务台账', page: 'ledger', absent: '活动抽奖' }, { mode: 'activities', label: '活动抽奖', page: 'activities', absent: '财务台账' }]) {
    mode = entry.mode
    reads = []
    await boot()
    const directory = page.getByRole('region', { name: '业务模块', exact: true })
    await directory.getByRole('button', { name: entry.label, exact: true }).click()
    await page.getByRole('button', { name: '返回业务模块', exact: true }).waitFor()
    await page.getByRole('cell', { name: entry.page === 'ledger' ? '成员1' : '合成活动1', exact: true }).waitFor()
    if (reads.some(path => path === '/groups' || path.includes('/features'))) throw new Error('Business-only role requested protected group catalog')
    await page.getByRole('button', { name: '返回业务模块', exact: true }).click()
    if (await directory.getByRole('button', { name: entry.absent, exact: true }).count()) throw new Error('Business permission widened')
    await page.goto(base + '/#' + entry.page)
    await page.reload()
    await page.getByRole('button', { name: '返回业务模块', exact: true }).waitFor()
    await page.getByRole('cell', { name: entry.page === 'ledger' ? '成员1' : '合成活动1', exact: true }).waitFor()
    if (await page.getByRole('button', { name: '新增记录', exact: true }).count() || await page.getByRole('button', { name: '创建活动', exact: true }).count()) throw new Error('Reader acquired a create action')
  }
  mode = 'none'
  await boot('ledger')
  if (await page.getByRole('button', { name: '返回业务模块', exact: true }).count()) throw new Error('Unauthorized legacy route exposed ledger')
  mode = 'ledger'
  reads = []
  await boot('ledger', 'lajiaoliao')
  await page.getByRole('heading', { name: '用户中心', exact: true }).waitFor()
  await page.getByRole('button', { name: '切换工作台', exact: true }).click()
  await page.getByRole('button', { name: '辣椒聊机器人', exact: true, pressed: true }).waitFor()
  await page.getByRole('button', { name: '辣椒聊机器人', exact: true, pressed: true }).click()
  if (reads.some(path => path === '/ledger' || path === '/activities' || path.includes('/features'))) throw new Error('WSL business leaked into Chili')
  if (writes) throw new Error('Navigation caused business effects')
  return { passed: true, nestedModules: true, selectedGroupAndAccount: true, lateResponseIsolation: true, businessOnlyRoles: true, legacyRoutes: true, platformIsolation: true, viewports: [320, 390, 647, 1440], writes: 0 }
  } finally {
    for (const delayed of pending) delayed.release()
    await page.unroute('**/api/v1/**', handler)
    await page.evaluate(items => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
      for (const [key, value] of Object.entries(items)) sessionStorage.setItem(key, value)
    }, original)
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.goto(base + '/#overview')
    await page.reload()
  }
}
