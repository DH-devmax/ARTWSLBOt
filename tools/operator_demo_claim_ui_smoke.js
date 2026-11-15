// Synthetic APIs only; no native action, model request, login or subscription mutation.
// prettier-ignore
async (page) => {
  const captcha = await process.getBuiltinModule('vm').runInThisContext(
    `(${process.getBuiltinModule('fs').readFileSync('tools/web_captcha_ui_smoke.js', 'utf8')})`,
    { filename: 'web_captcha_ui_smoke.js' }
  )(page, { fixtureOnly: true })
  const base = new URL(page.url()).origin
  const id = n => `d5800000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const actor = id(1), tenant = id(2), now = new Date().toISOString()
  const expires = new Date(Date.now() + 600000).toISOString()
  const grants = ['overview:read', 'groups:read']
  const records = new Map(), writes = [], reads = [], screenshots = []
  let mode = 'demo', nextError = '', holdNextRead = false, releaseRead, delayedRead = false, readStarted
  let mismatchedRead = false, loseNextResponse = false, nextState = ''
  const groups = ['wsl', 'lajiaoliao'].flatMap((platform, p) =>
    ['测试群', '另一个测试群', '已撤销测试群'].map((name, n) => ({
      group_id: id(10 + p * 3 + n), name: `${platform} ${name}`, platform,
      tenant_id: tenant, customer_name: '未开套餐客户', active: n !== 2, version: 7,
      primary_account_id: id(30 + p), robot_nickname: `${platform} 原生机器人`,
      ownership_state: 'unverified', business_enabled: false, ai_enabled: false
    })))
  const isDemo = () => mode === 'demo' || mode === 'customer-with-demo'
  const isAdmin = () => mode === 'demo' || mode === 'admin-without-demo'
  const space = () => ({ id: isAdmin() ? id(90) : tenant,
    name: isAdmin() ? '我的演示工作空间' : '未开套餐客户',
    kind: isAdmin() ? 'operator_demo' : 'tenant_business', owner_id: actor })
  const plan = () => isDemo()
    ? { id: 'operator_unlimited', name: '超管专属 · 不限额', system_only: true, active: true,
      permanent: true, unlimited: true, platforms: [], group_limit: null, member_limit: null, assistant_limit: null, credit_limit: null }
    : { id: '', name: '未开通套餐', active: false, system_only: false, platforms: [], modules: [],
      group_limit: 0, member_limit: 0, assistant_limit: 0, credit_limit: 0 }
  const emptyRecord = group => ({
    id: '', group_id: group.group_id, platform: group.platform, state: 'unverified', version: 0
  })
  const record = group => records.get(group.group_id) || emptyRecord(group)
  const envelope = data => ({ code: 0, msg: '', data })
  const handler = async route => {
    if (await captcha.handle(route)) return
    const req = route.request(), url = new URL(req.url()), path = url.pathname.replace('/api/v1', '')
    captcha.consume(req)
    const platform = req.headers()['x-workspace'] || 'wsl'
    let data = []
    const match = /^\/user-center\/groups\/([^/]+)\/demo-claim(?:\/([^/]+)\/cancel)?$/.exec(path)
    if (match) {
      const group = groups.find(g => g.group_id === match[1])
      if (!group || group.platform !== platform || !group.active || !isAdmin() || !isDemo())
        throw Error('Demo claim crossed platform, binding or role scope')
      if (req.method() === 'GET') {
        if (match[2]) throw Error('Unexpected cancellation read')
        reads.push({ group: group.group_id, platform })
        data = { ...record(group) }
        if (mismatchedRead) { mismatchedRead = false; data.platform = 'lajiaoliao' }
        if (holdNextRead) {
          holdNextRead = false; delayedRead = true
          readStarted()
          await new Promise(resolve => { releaseRead = resolve })
        }
      } else if (req.method() === 'POST') {
        const body = req.postDataJSON() || {}, key = req.headers()['idempotency-key']
        writes.push({ path, platform, body, key })
        if (!key || writes.filter(w => w.key === key).length !== 1)
          throw Error('Mutation omitted or reused its explicit click idempotency key')
        if (nextError) {
          const code = nextError; nextError = ''
          return route.fulfill({ status: 409, json: { code: 1, msg: code } })
        }
        const prior = record(group)
        if (match[2]) {
          if (match[2] !== prior.id || prior.state !== 'active' ||
            Object.keys(body).join() !== 'version' || body.version !== prior.version)
            throw Error('Cancellation lost original record and version')
          data = { ...prior, state: 'cancelled', version: prior.version + 1 }
        } else {
          if (Object.keys(body).join() !== 'binding_version' || body.binding_version !== group.version)
            throw Error('Verification supplied client identity or lost binding version')
          data = { id: id(50 + writes.length), group_id: group.group_id, platform,
            state: 'active', version: 2, expires_at: expires, robot_role: 'owner' }
        }
        if (nextState) {
          data = { ...data, state: nextState, version: nextState === 'checking' ? 1 : 2,
            ...(nextState === 'failed' ? { blocked_reason: 'operator_demo_claim_role_required' } : {}) }
          nextState = ''
        }
        records.set(group.group_id, data)
        if (loseNextResponse) { loseNextResponse = false; return route.abort('failed') }
      } else throw Error('Unexpected demo claim method')
    } else if (req.method() !== 'GET') throw Error('Unexpected native or customer mutation: ' + path)
    else if (path === '/me') data = { user: { id: actor, username: 'synthetic-demo',
      role: isAdmin() ? 'superadmin' : 'user', ...(!isAdmin() ? { tenant_id: tenant, tenant_role: 'owner' } : {}) },
      permissions: grants }
    else if (path === '/workspaces') data = { items: [
      { id: 'user', name: '用户中心' }, { id: 'wsl', name: '旺商聊机器人' },
      { id: 'lajiaoliao', name: '辣椒聊机器人' }
    ] }
    else if (path === '/user-center') data = {
      platform_admin: isAdmin(), permissions: grants, subscription: null,
      workspace: space(), effective_plan: plan(), billing_source: isDemo() ? 'platform_demo' : 'tenant_wallet',
      quotas: { groups: plan().group_limit, members: plan().member_limit, assistant: plan().assistant_limit, credits: plan().credit_limit },
      entitlement: { mode: isDemo() ? 'operator_demo' : 'tenant_business',
        name: isDemo() ? '演示测试套餐' : '未开通套餐', unlimited: isDemo(),
        active: isDemo(), permissions: grants },
      groups: [], personas: [], operations: [], claims: []
    }
    else if (path === '/user-center/groups') {
      const items = groups.filter(g => g.platform === url.searchParams.get('platform') &&
        (!isAdmin() || url.searchParams.get('workspace') === 'all')).map(g => ({
        ...g, demo_claim: isAdmin() && isDemo() ? record(g) : null
      }))
      data = { workspace: space(), effective_plan: plan(), items, total: items.length, limit: 20, sampled_at: now }
    } else if (path === '/user-center/usage') data = { call_count: 0, actual_micro: 0, held_micro: 0 }
    try { await route.fulfill({ json: envelope(data) }) }
    catch (e) {
      if (!/closed|disposed|cancel|intercept|target/i.test(String(e))) throw e
    }
  }
  const dialog = () => page.getByRole('dialog', { name: '测试认领', exact: true })
  const list = () => page.locator('.uc-mobile-groups:visible, .uc-groups:visible')
  const row = group => list().locator('.uc-group-row, tr').filter({ hasText: group.name })
  const open = async group => {
    await row(group).getByRole('button', { name: '测试认领', exact: true }).click()
    await dialog().getByRole('button', { name: '刷新进度', exact: true }).waitFor()
  }
  const close = async () => {
    await dialog().getByRole('button', { name: '关闭', exact: true }).click()
    await dialog().waitFor({ state: 'hidden' })
  }
  const navigate = async platform => {
    await page.evaluate(({ actor, platform }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`, 'user')
      sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
    }, { actor, platform })
    await page.goto(base + '/#groups')
    await page.reload()
    if (isAdmin())
      await page.locator('.uc-workspace-filter label').filter({ hasText: '全部工作空间' }).click()
    await row(groups.find(g => g.platform === platform)).waitFor()
  }
  const preserveClaimOnRefresh = async (automatic = false) => {
    const element = await dialog().elementHandle(), before = writes.length, beforeReads = reads.length
    try {
      await captcha.refreshSession(automatic)
      if (!await dialog().isVisible()) throw Error('Same-identity /me refresh closed the test-claim dialog')
      if (!await dialog().evaluate((current, previous) => current === previous, element))
        throw Error('Same-identity /me refresh remounted the test-claim dialog')
      if (writes.length !== before || reads.length !== beforeReads)
        throw Error('Same-identity /me refresh restarted test-claim work')
    } finally { await element.dispose() }
  }
  await page.route('**/api/v1/**', handler)
  try {
    for (const platform of ['wsl', 'lajiaoliao']) {
      await page.setViewportSize({ width: 1440, height: 900 })
      await navigate(platform)
      const group = groups.find(g => g.platform === platform)
      const activeGroups = groups.filter(g => g.platform === platform && g.active)
      for (const activeGroup of activeGroups)
        await row(activeGroup).getByRole('button', { name: '测试认领', exact: true }).waitFor()
      if (await row(groups.find(g => g.platform === platform && !g.active))
        .getByRole('button', { name: '测试认领', exact: true }).count())
        throw Error('Revoked binding exposed test claim')
      if (await list().getByRole('button', { name: '测试认领', exact: true }).count() !== 2)
        throw Error('Missing entry for valid binding or exposed entry for revoked binding')
      const before = writes.length
      await open(group)
      await dialog().getByText('待核验', { exact: true }).waitFor()
      await dialog().getByText('未开套餐客户', { exact: true }).waitFor()
      await dialog().getByText(group.robot_nickname, { exact: true }).waitFor()
      await dialog().getByText('仅用于本人 10 分钟测试，不开通套餐，不改动客户群绑定或业务运行状态。', { exact: true }).waitFor()
      await preserveClaimOnRefresh()
      await close()
      if (writes.length !== before) throw Error('Opening or closing created a record')
      for (const width of [320, 390, 768, 1440]) {
        await page.setViewportSize({ width, height: 769 })
        await open(group)
        await dialog().getByText('待核验', { exact: true }).waitFor()
        const footer = dialog().getByRole('button', { name: '核验测试群', exact: true })
        const box = await footer.boundingBox(), bounds = await dialog().boundingBox()
        if (!bounds || bounds.x < -1 || bounds.x + bounds.width > width + 1 ||
          !box || box.x < -1 || box.x + box.width > width + 1 || box.y < 0 || box.y + box.height > 770)
          throw Error('Dialog or footer overflow at ' + width)
        const screenshot = `output/playwright/operator-demo-claim-${platform}-${width}.png`
        await page.screenshot({ path: screenshot, animations: 'disabled' }); screenshots.push(screenshot)
        await close()
        if (writes.length !== before) throw Error('Reopening renewed verification')
      }
      await page.setViewportSize({ width: 390, height: 769 })
      await open(group)
      await dialog().getByText('待核验', { exact: true }).waitFor()
      await dialog().getByRole('button', { name: '核验测试群', exact: true }).click()
      await captcha.preserveOnRefresh(() => preserveClaimOnRefresh(true))
      grants.reverse()
      await captcha.preserveOnRefresh(preserveClaimOnRefresh)
      if (writes.length !== before) throw Error('Session refresh submitted test-claim verification')
      await captcha.complete()
      await dialog().getByText('已核验', { exact: true }).waitFor()
      await dialog().getByText('到期时间', { exact: true }).waitFor()
      if (writes.length !== before + 1) throw Error('Explicit verification did not submit exactly once')
      await close()
      await row(group).getByText('已核验', { exact: true }).waitFor()
      await open(group)
      await dialog().getByText('已核验', { exact: true }).waitFor()
      if (writes.length !== before + 1) throw Error('Reading an active claim renewed it')
      await dialog().getByRole('button', { name: '解除测试认领', exact: true }).click()
      await dialog().getByText('已解除', { exact: true }).waitFor()
      if (writes.length !== before + 2) throw Error('Cancellation did not use one explicit submission')
      await close()
      await row(group).getByText('已核验', { exact: true }).waitFor({ state: 'hidden' })
      if (groups.some(g => g.business_enabled || g.ownership_state !== 'unverified'))
        throw Error('Demo claim changed customer business or binding')
    }
    await page.setViewportSize({ width: 1440, height: 900 })
    await navigate('wsl')
    const first = groups.find(g => g.platform === 'wsl'), other = groups.find(g => g.platform === 'lajiaoliao')
    for (const [code, text] of [
      ['operator_demo_claim_required', '请先核验本群的测试认领'],
      ['operator_demo_claim_expired', '测试认领已到期，请重新核验'],
      ['operator_demo_claim_role_required', '机器人需要是本群群主或管理员，才可测试认领'],
      ['operator_demo_claim_scope_changed', '群授权或机器人状态已变化，请关闭后重新核验']
    ]) {
      await open(first)
      await dialog().getByText('已解除', { exact: true }).waitFor()
      nextError = code
      await dialog().getByRole('button', { name: '核验测试群', exact: true }).click()
      await captcha.complete()
      await dialog().getByText(text, { exact: true }).waitFor()
      await close()
    }
    mismatchedRead = true
    await open(first)
    await dialog().getByText('群授权或机器人状态已变化，请关闭后重新核验', { exact: true }).waitFor()
    if (!await dialog().getByRole('button', { name: '核验测试群', exact: true }).isDisabled())
      throw Error('Mismatched scope became actionable')
    await close()
    nextState = 'checking'
    await open(first)
    await dialog().getByText('已解除', { exact: true }).waitFor()
    await dialog().getByRole('button', { name: '核验测试群', exact: true }).click()
    await captcha.complete()
    await dialog().getByText('核验中', { exact: true }).waitFor()
    if (!await dialog().getByRole('button', { name: '核验测试群', exact: true }).isDisabled())
      throw Error('In-flight checking state allowed another submission')
    const checkingWrites = writes.length
    await dialog().getByRole('button', { name: '刷新进度', exact: true }).click()
    await dialog().getByText('核验中', { exact: true }).waitFor()
    if (writes.length !== checkingWrites) throw Error('Checking progress triggered POST')
    records.set(first.group_id, { ...record(first), state: 'failed',
      blocked_reason: 'operator_demo_claim_role_required', version: 2 })
    await dialog().getByRole('button', { name: '刷新进度', exact: true }).click()
    await dialog().getByText('核验失败', { exact: true }).waitFor()
    await dialog().getByText('机器人需要是本群群主或管理员，才可测试认领', { exact: true }).waitFor()
    nextError = 'operator_demo_claim_commit_unknown'
    await dialog().getByRole('button', { name: '核验测试群', exact: true }).click()
    await captcha.complete()
    await dialog().getByText('核验提交结果待确认，请刷新进度', { exact: true }).waitFor()
    if (!await dialog().getByRole('button', { name: '核验测试群', exact: true }).isDisabled())
      throw Error('Unknown commit result allowed another submission')
    await dialog().getByRole('button', { name: '刷新进度', exact: true }).click()
    await dialog().getByText('核验失败', { exact: true }).waitFor()
    await close()
    loseNextResponse = true
    await open(first)
    await dialog().getByText('核验失败', { exact: true }).waitFor()
    await dialog().getByRole('button', { name: '核验测试群', exact: true }).click()
    await captcha.complete()
    await dialog().getByText('结果待核对，请刷新进度。', { exact: true }).waitFor()
    const unknownWrites = writes.length
    if (!await dialog().getByRole('button', { name: '核验测试群', exact: true }).isDisabled())
      throw Error('Unknown result allowed automatic or repeated submission')
    await dialog().getByRole('button', { name: '刷新进度', exact: true }).click()
    await dialog().getByText('已核验', { exact: true }).waitFor()
    if (writes.length !== unknownWrites) throw Error('Reconciling unknown result submitted')
    await close()
    holdNextRead = true
    const readObserved = new Promise(resolve => { readStarted = resolve })
    await open(first)
    await readObserved
    if (!delayedRead) throw Error('Delayed read fixture was not exercised')
    await navigate('lajiaoliao')
    await open(other)
    await dialog().getByText('已解除', { exact: true }).waitFor()
    releaseRead()
    await page.waitForTimeout(120)
    await dialog().getByText(other.robot_nickname, { exact: true }).waitFor()
    await dialog().getByText('已解除', { exact: true }).waitFor()
    if (await dialog().getByText('已核验', { exact: true }).count())
      throw Error('Late cross-platform response replaced selected claim')
    await close()
    const boundaries = [
      ['actor', { user: { id: id(100) } }],
      ['tenant', { user: { tenant_id: id(101) } }],
      ['role', { user: { role: 'user' } }],
      ['tenant-role', { user: { tenant_role: 'viewer' } }],
      ['permissions', { permissions: [...grants, 'claims:create'] }],
      ['console', { console: 'admin' }],
      ['platform', { workspace: 'lajiaoliao' }],
      ['account', { selectedAccount: id(102) }]
    ]
    for (const [boundary, patch] of boundaries) {
      await page.setViewportSize({ width: 1440, height: 900 })
      records.delete(first.group_id)
      await navigate('wsl'); await open(first)
      await dialog().getByText('待核验', { exact: true }).waitFor()
      await dialog().getByRole('button', { name: '核验测试群', exact: true }).click()
      const before = writes.length, hold = captcha.holdNext('verify')
      await captcha.position(); await captcha.slider().press('Enter'); await hold.started
      await captcha.patchSession(patch)
      await dialog().waitFor({ state: 'hidden' }); await captcha.dialog().waitFor({ state: 'hidden' })
      hold.release(); await hold.done; await captcha.settled()
      if (writes.length !== before || await dialog().isVisible() || await captcha.dialog().isVisible())
        throw Error(`Late test-claim proof survived ${boundary} change`)
    }
    for (const variant of ['customer', 'customer-with-demo', 'admin-without-demo']) {
      mode = variant
      for (const width of [390, 1440]) {
        await page.setViewportSize({ width, height: 769 })
        await navigate('wsl')
        if (await page.getByRole('button', { name: '测试认领', exact: true }).count())
          throw Error('Demo entry exposed without both server demo mode and superadmin role')
      }
    }
    return { passed: true, synthetic_only: true, platforms: ['wsl', 'lajiaoliao'],
      viewports: [320, 390, 768, 1440], explicit_posts: writes.length, read_only_queries: reads.length,
      scope_and_version: true, cancellation: true, no_writes_on_open_close: true,
      same_identity_refresh_preserves_dialog: true, scheduled_refresh_preserves_slider: true,
      equivalent_permissions_preserved: true, scope_cancellation: boundaries.map(([boundary]) => boundary),
      late_platform_isolation: true, unknown_result_reconciled: true, restricted_roles: true,
      native_mutations: 0, model_calls: 0, screenshots }
  } finally {
    if (releaseRead) releaseRead()
    await page.unroute('**/api/v1/**', handler)
  }
}
