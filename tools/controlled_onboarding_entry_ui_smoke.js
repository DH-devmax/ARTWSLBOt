// Complete built-app navigation with synthetic APIs; never contacts native services.
// prettier-ignore
(() => async page => {
  const check = (value, message) => { if (!value) throw Error(message) }
  const id = n => `f9100000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const actor = id(1), tenant = id(2), platforms = ['wsl', 'lajiaoliao']
  const widths = [320, 390, 768, 1440], reads = [], writes = [], unexpected = [], screenshots = []
  const permissions = ['overview:read', 'accounts:read', 'groups:read', 'claims:create', 'tests:manage',
    'assistant:use']
  const space = { id: tenant, name: '合成演示空间', kind: 'operator_demo' }
  const accounts = platforms.flatMap((platform, index) => [
    { id: id(10 + index * 2), platform, purpose: 'management', name: `${platform}接收机器人`,
      native_id: `robot-${platform}`, auth_state: 'authenticated', testing_enabled: true, version: 1 },
    { id: id(11 + index * 2), platform, purpose: 'management', name: `${platform}申请账号`,
      native_id: `claimant-${platform}`, auth_state: 'authenticated', testing_enabled: true, version: 1 }
  ])
  const scopes = platforms.map((platform, index) => ({
    id: id(30 + index), actor_id: actor, tenant_id: tenant, platform,
    account_id: accounts[index * 2].id, claimant_account_id: accounts[index * 2 + 1].id,
    group_id: id(40 + index), version: 1, state: 'active', blocked_reason: '',
    expires_at: new Date(Date.now() + 21600000).toISOString(),
    native_attempts_used: 0, model_calls_used: 0, reserved_usdt: '0.000000',
    robot: { id: accounts[index * 2].id, name: accounts[index * 2].name, native_id: `robot-${platform}` },
    claimant: { id: accounts[index * 2 + 1].id, name: accounts[index * 2 + 1].name, native_id: `claimant-${platform}` },
    group: { id: id(40 + index), name: `${platform}隔离测试群`, native_id: `test-${platform}` }
  }))
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), url = new URL(request.url())
    const path = url.pathname.replace('/api/v1', ''), platform = request.headers()['x-workspace'] || 'wsl'
    let data
    if (request.method() !== 'GET') {
      writes.push({ path, platform })
      return route.fulfill({ status: 409, json: { code: 1, msg: 'synthetic_mutation_forbidden' } })
    }
    reads.push({ path, platform })
    if (path === '/me') data = { user: { id: actor, username: 'entry-fixture', role: 'superadmin' }, permissions }
    else if (path === '/workspaces') data = { items: platforms.map(id => ({ id, name: id })) }
    else if (path === '/accounts') data = accounts.filter(item => item.platform === platform)
    else if (['/groups', '/test-windows', '/test-tools/operations'].includes(path)) data = []
    else if (path === '/overview') data = { accounts: 2, groups: 0 }
    else if (path === '/assistant/context') data = { groups: [], next_offset: -1 }
    else if (/^\/accounts\/[^/]+\/receiver$/.test(path))
      data = { enabled: true, revision: 1, snapshot: { state: 'running' } }
    else if (path === '/test-tools/onboarding/scopes')
      data = { items: scopes.filter(item => item.platform === platform), candidates: {
        accounts: accounts.filter(item => item.platform === platform),
        groups: scopes.filter(item => item.platform === platform).map(item => item.group) } }
    else if (path === '/user-center') {
      // Let the real UserCenter mount before its entitlement becomes available.
      await new Promise(resolve => setTimeout(resolve, 100))
      data = { workspace: space, effective_plan: { id: 'operator_unlimited', system_only: true, unlimited: true,
        permanent: true, active: true, name: '超管专属 · 不限额', platforms },
      entitlement: { mode: 'operator_demo', active: true, unlimited: true, permissions },
      permissions, groups: [], personas: [], claims: [], operations: [] }
    } else if (path === '/user-center/groups')
      data = { workspace: space, items: [], total: 0, page_size: 20, customers: [] }
    else if (path === '/user-center/usage') data = { call_count: 0, actual_micro: 0, held_micro: 0 }
    else if (path === '/group-binding/context')
      data = { mode: 'operator_demo', workspace: space, default_target_tenant_id: tenant,
        can_onboard_for_customer: true, customers: [], next_offset: -1 }
    else if (path.endsWith('/events'))
      return route.fulfill({ contentType: 'text/event-stream', body: '' })
    else {
      unexpected.push({ path, platform })
      return route.fulfill({ status: 500, json: { code: 1, msg: 'unmocked_api' } })
    }
    await route.fulfill({ json: { code: 0, data } })
  })
  const modal = () => page.getByRole('dialog', { name: '接入群组', exact: true })
  const enter = async (platform, width) => {
    await page.setViewportSize({ width, height: 820 })
    await page.evaluate(({ actor, platform, account }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`, 'admin')
      sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
      sessionStorage.setItem(`wsl:account:${actor}:${platform}`, account)
    }, { actor, platform, account: accounts.find(item => item.platform === platform).id })
    await page.goto('http://127.0.0.1:5173/#tests')
    await page.reload()
    const card = page.locator('.controlled-item').filter({ hasText: `${platform}隔离测试群` })
    await card.getByRole('button', { name: '在用户中心接入', exact: true }).click()
    await modal().waitFor()
    await modal().getByRole('button', { name: '开始核验', exact: true })
      .and(page.locator(':enabled')).waitFor()
    check(new URL(page.url()).hash === '#groups', 'Workspace watcher overwrote onboarding navigation')
    const notice = modal().locator('.onboarding-controlled-notice')
    await notice.getByText(`${platform}申请账号 · claimant-${platform}`, { exact: true }).waitFor()
    await notice.getByText(`${platform}隔离测试群 · test-${platform}`, { exact: true }).waitFor()
    check(await modal().getByRole('radio', { name: '代客户接入', exact: true }).isDisabled(),
      'Handoff allowed changing the target tenant')
    check(writes.length === 0, 'Opening the normal wizard created a challenge or native operation')
    const file = `output/playwright/controlled-onboarding-entry-${platform}-${width}.png`
    await page.screenshot({ path: file, animations: 'disabled' }); screenshots.push(file)
    check(!await modal().evaluate(element => element.scrollWidth > element.clientWidth + 1),
      `Wizard overflow at ${platform}/${width}`)
    await modal().getByRole('button', { name: '关闭', exact: true }).click()
    await modal().waitFor({ state: 'hidden' })
    await page.getByRole('button', { name: '刷新用户中心', exact: true }).click()
    await page.getByRole('button', { name: '刷新用户中心', exact: true })
      .and(page.locator(':enabled')).waitFor()
    check(!await modal().isVisible(), 'Consumed onboarding handoff reopened on refresh')
    await page.evaluate(() => { location.hash = '#assistant' })
    await page.locator('.assistant-workspace').waitFor()
    await page.evaluate(() => { location.hash = '#groups' })
    await page.getByRole('button', { name: '接入群组', exact: true }).waitFor()
    await page.getByRole('button', { name: '刷新用户中心', exact: true })
      .and(page.locator(':enabled')).waitFor()
    check(!await modal().isVisible(), 'Consumed onboarding handoff reopened after component remount')
  }
  for (const platform of platforms) for (const width of widths) await enter(platform, width)
  for (const scope of scopes) { scope.state = 'expired'; scope.version++ }
  for (const platform of platforms) for (const selected of platforms) {
    const expired = scopes.find(item => item.platform === selected)
    await page.evaluate(({ actor, platform, selected, scope }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`, 'user')
      sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
      sessionStorage.setItem(`wsl:controlled-onboarding:${actor}:operator:${selected}`, scope)
    }, { actor, platform, selected, scope: expired.id })
    // A fresh user-center load has no remembered admin page to rescue a bad hash handoff.
    await page.goto('http://127.0.0.1:5173/#groups'); await page.reload()
    await page.getByRole('button', { name: '接入群组', exact: true }).click()
    if (selected !== platform)
      await modal().locator('label.el-radio-button')
        .filter({ hasText: selected === 'wsl' ? '旺商聊' : '辣椒聊' }).click()
    await modal().getByRole('button', { name: '创建新接入范围', exact: true }).last().click()
    await page.locator('.controlled-item').filter({ hasText: `${selected}隔离测试群` }).waitFor()
    check(new URL(page.url()).hash === '#tests' && !await modal().isVisible(),
      'Expired scope handoff failed without a remembered admin view')
    check(await page.evaluate(actor => sessionStorage.getItem(`wsl:workspace:${actor}`), actor) === selected,
      'Returning to controlled tools lost the wizard platform')
  }
  check(!unexpected.length, 'Unmocked APIs: ' + JSON.stringify(unexpected))
  check(!writes.length, 'Navigation performed a mutation')
  return { passed: true, scenarios: widths.length * platforms.length + platforms.length ** 2,
    remount_cases: widths.length * platforms.length, cross_platform_return_cases: platforms.length,
    platforms, viewports: widths, writes: writes.length, read_count: reads.length, screenshots }
})()
