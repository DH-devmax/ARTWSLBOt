// All native endpoints are synthetic; no browser request reaches a real group.
async (page) => {
  const base = 'http://127.0.0.1:5173'
  const id = n => `76000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const actor = id(99)
  const account = { id: id(1), name: '群主机器人', workspace: 'wsl', platform: 'wsl',
    purpose: 'management', testing_enabled: true, connection_enabled: true, version: 1, epoch: 1 }
  const group = { group_id: id(2), name: '隔离角色验证群', native_id: '123', platform: 'wsl',
    primary_account_id: account.id, management_enabled: false, ai_enabled: false }
  const grant = { id: id(3), group_id: group.group_id, account_ids: [account.id], target_ids: ['7', '8', '9'],
    actions: ['grant_admin', 'revoke_admin'], admission_mode: 'native_debug', used: 0, budget: 2,
    cancelled: false, expires_at: new Date(Date.now() + 300000).toISOString() }
  let selfRole = 'admin', targetRole = 'member', writes = 0
  const history = []
  const handler = async route => {
    const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', '')
    let data = []
    if (path === '/me') data = { user: { id: actor, username: 'synthetic-owner', role: 'superadmin' },
      permissions: ['overview:read', 'accounts:read', 'groups:read', 'tests:manage'] }
    else if (path === '/workspaces') data = { items: [{ id: 'wsl', name: '旺商聊机器人' }] }
    else if (path === '/accounts') data = [account]
    else if (path === '/groups') data = [group]
    else if (path === '/test-windows') {
      if (req.method() !== 'GET') throw new Error('Opening the editor created a grant')
      data = [grant]
    }
    else if (path === '/test-tools') data = { tools: [{ name: 'run_moderation' }] }
    else if (path === '/test-tools/operations') data = history
    else if (path === `/accounts/${account.id}/receiver`)
      data = { enabled: true, revision: 1, observed: true, snapshot: { state: 'connected' } }
    else if (path === '/test-tools/call') {
      const { name, arguments: args } = req.postDataJSON()
      if (args.account_id !== account.id || args.test_window_id !== grant.id)
        throw new Error('Role action lost its grant or account')
      if (name === 'read_group') data = { scope: { remaining: 2 - writes },
        snapshot: { self_id: '7', self_role: selfRole, group_id: '123', policy: { muted: false },
          mutes_known: true, muted_ids: [], sampled_at: new Date().toISOString(),
          members: [{ id: '7', name: '本人', role: selfRole }, { id: '8', name: '配合成员', role: targetRole },
            { id: '9', name: '受保护群主', role: 'owner' }] } }
      else if (name === 'run_moderation') {
        if (selfRole !== 'owner' || args.target_id !== '8' || !req.headers()['idempotency-key'] ||
          args.action !== (writes === 0 ? 'grant_admin' : 'revoke_admin') ||
          Object.keys(args).sort().join(',') !== 'account_id,action,target_id,test_window_id')
          throw new Error('Role action widened scope or bypassed ownership')
        writes++; grant.used = writes
        targetRole = args.action === 'grant_admin' ? 'admin' : 'member'
        const operation = { id: id(10 + writes), kind: args.action, status: 'succeeded',
          result_code: 'native_moderation_acknowledged', test_window_id: grant.id,
          created_at: new Date().toISOString() }
        history.unshift(operation)
        data = { operation }
      } else throw new Error('Unexpected role tool: ' + name)
    }
    await route.fulfill({ json: { code: 0, data, msg: '' } })
  }
  await page.route('**/api/v1/**', handler)
  try {
    await page.goto(base)
    await page.evaluate(({ actor, account }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:account:${actor}:wsl`, account)
    }, { actor, account: account.id })
    await page.goto(base + '/#tests')
    await page.reload()
    await page.locator('.tools-grant').click()
    await page.getByText(/原生调试授权/).waitFor()
    await page.getByRole('button', { name: '创建授权窗口', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '创建授权窗口', exact: true })
    await dialog.getByText('原生调试（独立于生产业务开关）', { exact: true }).waitFor()
    if (await dialog.getByRole('checkbox', { name: '设置群管理员（群主）', exact: true }).count() !== 1 ||
      await dialog.getByRole('checkbox', { name: '撤销群管理员（群主）', exact: true }).count() !== 1 ||
      await dialog.getByRole('checkbox', { name: '文本（待验收）', exact: true }).count())
      throw new Error('Debug editor omitted roles or exposed unavailable actions')
    await dialog.getByRole('button', { name: '取消', exact: true }).click()
    await page.getByRole('button', { name: '读取当前群状态', exact: true }).click()
    await page.getByText('机器人角色：管理员', { exact: true }).waitFor()
    await page.waitForFunction(() => !document.querySelector('.test-tools .is-loading'))
    if (!await page.getByRole('button', { name: '确认验证此动作', exact: true }).isDisabled())
      throw new Error('Native admin could change administrators')
    selfRole = 'owner'
    await page.getByRole('button', { name: '读取当前群状态', exact: true }).click()
    await page.getByText('机器人角色：群主', { exact: true }).waitFor()
    await page.waitForFunction(() => !document.querySelector('.test-tools .is-loading'))
    await page.getByRole('combobox', { name: '授权目标成员' }).press('ArrowDown')
    for (const name of ['本人 · 7', '受保护群主 · 9'])
      if (!await page.getByRole('option', { name, exact: true }).evaluate(el => el.classList.contains('is-disabled')))
        throw new Error('Protected role target selectable')
    await page.getByRole('option', { name: '配合成员 · 8', exact: true }).click()
    await page.getByRole('button', { name: '确认验证此动作', exact: true }).click()
    await page.getByRole('button', { name: '取消', exact: true }).click()
    if (writes) throw new Error('Cancelling confirmation submitted a role mutation')
    for (const action of ['设置群管理员', '撤销群管理员']) {
      if (writes) {
        await page.getByRole('combobox', { name: '验证动作' }).press('ArrowDown')
        await page.getByRole('option', { name: action, exact: true }).click()
      }
      await page.getByRole('button', { name: '确认验证此动作', exact: true }).click()
      await page.getByRole('button', { name: '确认提交', exact: true }).click()
      await page.getByText('只读核对：已观察到期望状态。', { exact: true }).waitFor()
      await page.waitForFunction(() => !document.querySelector('.test-tools .is-loading'))
    }
    if (writes !== 2 || targetRole !== 'member' ||
      !await page.getByRole('button', { name: '确认验证此动作', exact: true }).isDisabled())
      throw new Error('Role restoration/budget failed')
    for (const width of [320, 390, 647, 1440]) {
      await page.setViewportSize({ width, height: 779 })
      await page.evaluate(() => Promise.all(document.getAnimations()
        .filter(a => a.effect?.getComputedTiming().iterations !== Infinity).map(a => a.finished.catch(() => {}))))
      if (!await page.locator('.test-tools').evaluate(el => el.scrollWidth <= el.clientWidth + 1))
        throw new Error('Role tools overflow: ' + width)
      await page.screenshot({ path: `output/playwright/native-roles-${width}.png`, fullPage: true })
    }
    return { checks: 12, synthetic_writes: writes, native_actions: 0, production_switches: false,
      restored_role: targetRole, viewports: [320, 390, 647, 1440] }
  } finally {
    await page.unroute('**/api/v1/**', handler)
  }
}
