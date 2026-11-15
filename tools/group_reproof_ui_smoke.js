// Built frontend with synthetic APIs; no native message or model request.
async (page) => {
  const captcha = await process.getBuiltinModule('vm').runInThisContext(
    `(${process.getBuiltinModule('fs').readFileSync('tools/web_captcha_ui_smoke.js', 'utf8')})`,
    { filename: 'web_captcha_ui_smoke.js' }
  )(page, { fixtureOnly: true })
  const base = new URL(page.url()).origin
  const id = (n) => `d5700000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const actor = id(1), tenant = id(2), now = new Date().toISOString()
  const expires = new Date(Date.now() + 600000).toISOString()
  const grants = ['overview:read', 'groups:read', 'claims:create', 'claims:read']
  const space = () => mode === 'demo'
    ? { id: id(90), name: '我的演示工作空间', kind: 'operator_demo', owner_id: actor }
    : { id: tenant, name: '合成客户', kind: 'tenant_business', owner_id: actor }
  const plan = () => mode === 'demo'
    ? { id: 'operator_unlimited', name: '超管专属 · 不限额', system_only: true, active: true,
      permanent: true, unlimited: true, platforms: [], group_limit: null, member_limit: null, assistant_limit: null, credit_limit: null }
    : { id: id(91), name: 'PRO', active, system_only: false, group_limit: 10, member_limit: 10,
      assistant_limit: 10, credit_limit: 1000000, platforms: ['wsl', 'lajiaoliao'], modules: [], expires_at: expires }
  let mode = 'owner', active = true, selected, challenge
  const writes = [], progressReads = [], shots = []
  const pendingRequests = new Set()
  const resetURL = base + '/__group-reproof-fixture'
  const reset = route => route.fulfill({ contentType: 'text/html', body: '<!doctype html><title>Fixture reset</title>' })
  const groups = ['wsl', 'lajiaoliao'].flatMap((platform, p) =>
    ['unverified', 'paused', 'active'].map((ownership_state, n) => ({
      group_id: id(10 + p * 3 + n), name: `${platform} 测试群 ${n + 1}`, platform,
      tenant_id: tenant, customer_name: '合成客户', active: true, version: 7,
      native_id: `native-${platform}-${n}`, ownership_state,
      primary_account_id: id(30 + p), robot_nickname: `${platform} 原生机器人`
    })))
  const permissions = () => mode === 'member' ? ['overview:read', 'groups:read'] : grants
  const response = data => ({ code: 0, msg: '', data })
  const respond = async route => {
    if (await captcha.handle(route)) return
    const req = route.request(), url = new URL(req.url()), path = url.pathname.replace('/api/v1', '')
    captcha.consume(req)
    const platform = req.headers()['x-workspace'] || 'wsl'
    let data = []
    if (req.method() === 'GET') {
      if (path === '/me') data = { user: { id: actor, username: 'synthetic-customer',
        role: mode === 'demo' ? 'superadmin' : 'user',
        ...(mode === 'demo' ? {} : { tenant_id: tenant, tenant_role: mode === 'owner' ? 'owner' : 'viewer' }) },
        permissions: permissions() }
      else if (path === '/workspaces') data = { items: [
        { id: 'user', name: '用户中心' }, { id: 'wsl', name: '旺商聊机器人' },
        { id: 'lajiaoliao', name: '辣椒聊机器人' }
      ] }
      else if (path === '/user-center') data = {
        platform_admin: mode === 'demo', permissions: permissions(),
        workspace: space(), effective_plan: plan(), billing_source: mode === 'demo' ? 'platform_demo' : 'tenant_wallet',
        quotas: { groups: plan().group_limit, members: plan().member_limit, assistant: plan().assistant_limit, credits: plan().credit_limit },
        entitlement: { mode: mode === 'demo' ? 'operator_demo' : 'tenant_business',
          name: mode === 'demo' ? '演示测试套餐' : 'PRO', active, unlimited: mode === 'demo',
          permissions: permissions() },
        subscription: { name: 'PRO', active, platforms: ['wsl', 'lajiaoliao'], modules: [],
          group_limit: 10, member_limit: 10, expires_at: expires },
        groups: [], personas: [], operations: [], claims: []
      }
      else if (path === '/user-center/groups') {
        if (url.searchParams.get('platform') !== req.headers()['x-workspace'] || url.searchParams.get('page_size') !== '20')
          throw Error('Reproof group query lost platform or pagination')
        if (mode === 'demo' && !['mine', 'all'].includes(url.searchParams.get('workspace')))
          throw Error('Missing demo workspace scope')
        const items = groups.filter(g => g.platform === url.searchParams.get('platform') &&
          (mode !== 'demo' || url.searchParams.get('workspace') === 'all'))
        data = { workspace: space(), effective_plan: plan(), items, groups: items, total: items.length, limit: 20, sampled_at: now }
      } else if (path === '/user-center/usage') data = { call_count: 0, actual_micro: 0, held_micro: 0 }
      else if (path === '/group-binding/context') data = { mode: mode === 'demo' ? 'operator_demo' : 'tenant_business',
        workspace: space(), default_target_tenant_id: space().id, can_onboard_for_customer: mode === 'demo',
        customers: mode === 'demo' ? [{ tenant_id: tenant, name: '合成客户' }] : [], next_offset: -1 }
      else if (/^\/group-binding\/challenges\/[^/]+$/.test(path)) {
        progressReads.push(path)
        if (!challenge || challenge.id !== path.split('/')[3]) throw Error('Read another scope challenge')
        data = { ...challenge, group_challenge: undefined, private_challenge: undefined }
      }
    } else {
      const body = req.postDataJSON() || {}
      writes.push({ path, platform, body, key: req.headers()['idempotency-key'] })
      if (path === '/group-binding/challenges') {
        selected = groups.find(g => g.group_id === body.restore_group_id)
        if (mode !== 'owner' || !active || !selected || selected.platform !== platform ||
          body.platform !== platform || body.binding_version !== selected.version ||
          Object.keys(body).sort().join() !== 'binding_version,platform,restore_group_id' ||
          !req.headers()['idempotency-key']) throw Error('Reproof omitted server scope/version/idempotency')
        challenge = { id: id(platform === 'wsl' ? 50 : 51), platform, version: 1, state: 'waiting',
          expires_at: expires, workspace: space(), mode: 'tenant_business',
          customer: { tenant_id: tenant, name: '合成客户' },
          robot: { id: selected.primary_account_id, name: selected.robot_nickname, native_id: 'robot',
            add_account: `public-${platform}-robot`, add_available: true },
          can_verify_friend: false,
          group: { id: selected.group_id, name: selected.name, native_id: selected.native_id },
          group_challenge: '群绑定 SYNTHETIC_REPROOF', private_challenge: '群接入 SYNTHETIC_IDENTITY',
          identity_proven: false, can_confirm: false, can_cancel: true, can_accept_invitation: false,
          blocked_reason: 'group_binding_pool_identity' }
        return route.fulfill({ status: 201, json: response(challenge) })
      } else if (/\/confirm$/.test(path)) {
        if (!challenge.can_confirm || body.version !== challenge.version ||
          Object.keys(body).join() !== 'version' || !req.headers()['idempotency-key'])
          throw Error('Confirmation bypassed current proof')
        selected.ownership_state = 'active'
        challenge = { ...challenge, state: 'bound', can_confirm: false, can_cancel: false, version: challenge.version + 1 }
        data = challenge
      } else throw Error('Unexpected native or model mutation: ' + path)
    }
    await route.fulfill({ json: response(data) })
  }
  const handler = async route => {
    const pending = respond(route)
    pendingRequests.add(pending)
    try {
      await pending
    } finally {
      pendingRequests.delete(pending)
    }
  }
  const dialog = () => page.getByRole('dialog', { name: '重新核验群归属', exact: true })
  const close = async () => {
    await dialog().getByRole('button', { name: '关闭', exact: true }).click()
    await dialog().waitFor({ state: 'hidden' })
  }
  const list = () => page.locator('.uc-mobile-groups:visible, .uc-groups:visible')
  const row = group => list().locator('.uc-group-row, tr').filter({ hasText: group.name })
  const navigate = async (platform, nextMode = mode, nextActive = active) => {
    await page.goto(resetURL)
    await Promise.all([...pendingRequests])
    mode = nextMode
    active = nextActive
    await page.evaluate(({ actor, platform }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`, 'user')
      sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
    }, { actor, platform })
    await page.goto(base + '/#groups')
    await page.locator('.user-center').waitFor()
    if (mode === 'demo') {
      const mine = page.getByRole('radio', { name: '我的工作空间', exact: true })
      await mine.waitFor()
      if (!await mine.isChecked()) throw Error('Demo reproof did not default to own workspace')
      await page.locator('.uc-workspace-filter label').filter({ hasText: '全部工作空间' }).click()
      if (!await page.getByRole('radio', { name: '全部工作空间', exact: true }).isChecked())
        throw Error('Demo reproof did not explicitly select all workspaces')
    }
    await row(groups.find(g => g.platform === platform)).waitFor()
  }
  await page.route('**/api/v1/**', handler)
  await page.route(resetURL, reset)
  try {
    for (const platform of ['wsl', 'lajiaoliao']) {
      await page.setViewportSize({ width: 1440, height: 900 })
      await navigate(platform)
      const group = groups.find(g => g.platform === platform)
      const paused = groups.find(g => g.platform === platform && g.ownership_state === 'paused')
      await row(group).getByRole('button', { name: '重新核验', exact: true }).waitFor()
      await row(paused).getByRole('button', { name: '重新核验', exact: true }).waitFor()
      if (await list().getByRole('button', { name: '重新核验', exact: true }).count() !== 2)
        throw Error('Missing or excessive reproof entries: ' + platform + '/' +
          await list().getByRole('button', { name: '重新核验', exact: true }).count())
      await row(group).getByRole('button', { name: '重新核验', exact: true }).click()
      await dialog().getByText(group.name, { exact: true }).waitFor()
      if (await dialog().getByRole('radiogroup').count()) throw Error('Reproof platform can be replaced')
      await close()
      if (writes.length !== (platform === 'wsl' ? 0 : 2)) throw Error('Viewing or closing submitted')
      await row(group).getByRole('button', { name: '重新核验', exact: true }).click()
      await dialog().getByRole('button', { name: '开始核验', exact: true }).click()
      await captcha.complete()
      await dialog().getByText('请使用未接入号池的本人群主或管理员账号发送核验内容', { exact: false }).first().waitFor()
      if (await dialog().getByRole('button', { name: '确认绑定', exact: true }).count()) throw Error('Pool proof enabled confirmation')
      for (const width of [320, 390, 768, 1440]) {
        await page.setViewportSize({ width, height: 769 })
        await page.waitForTimeout(80)
        const button = await dialog().getByRole('button', { name: '刷新进度', exact: true }).boundingBox()
        if (!button || button.x < -1 || button.x + button.width > width + 1 ||
          button.y < -1 || button.y + button.height > 770) throw Error('Reproof footer hidden at ' + width)
        const file = `output/playwright/reproof-${platform}-${width}.png`
        await page.screenshot({ path: file, animations: 'disabled' }); shots.push(file)
      }
      await page.setViewportSize({ width: 1440, height: 900 })
      await close()
      await row(paused).getByRole('button', { name: '重新核验', exact: true }).click()
      await dialog().getByRole('button', { name: '开始核验', exact: true }).waitFor()
      await close()
      await row(group).getByRole('button', { name: '重新核验', exact: true }).click()
      await dialog().getByRole('button', { name: '刷新进度', exact: true }).waitFor()
      if (!progressReads.length || writes.filter(w => w.path === '/group-binding/challenges').length !==
        (platform === 'wsl' ? 1 : 2)) throw Error('Reopening duplicated or crossed a challenge')
      challenge = { ...challenge, version: 2, blocked_reason: 'group_binding_native_proof_unavailable' }
      await dialog().getByRole('button', { name: '刷新进度', exact: true }).click()
      await dialog().getByText(/重新发送.*核验|再次发送.*核验/).first().waitFor()
      challenge = { ...challenge, version: 3, state: 'proven', blocked_reason: '', proof_role: 'owner', can_confirm: true }
      await dialog().getByRole('button', { name: '刷新进度', exact: true }).click()
      await dialog().getByRole('button', { name: '确认绑定', exact: true }).click()
      const confirm = page.getByRole('dialog', { name: '确认群归属', exact: true })
      await confirm.getByText(group.name, { exact: true }).waitFor()
      await confirm.getByRole('button', { name: '确认绑定', exact: true }).click()
      await dialog().getByText('绑定成功', { exact: true }).waitFor()
      await dialog().getByRole('button', { name: '完成', exact: true }).click()
    }
    for (const variant of ['member', 'demo', 'expired']) {
      await page.setViewportSize({ width: 390, height: 820 })
      await navigate('wsl', variant === 'expired' ? 'owner' : variant, variant !== 'expired')
      if (variant === 'demo') {
        const button = list().getByRole('button', { name: '重新核验', exact: true })
        await button.waitFor()
        if (await button.count() !== 1) throw Error('Demo reproof included already verified groups')
        await button.click()
        await dialog().getByRole('combobox', { name: '群组所属客户', exact: true }).waitFor()
        if (!await dialog().getByRole('combobox', { name: '群组所属客户', exact: true }).isDisabled())
          throw Error('Demo restore can replace its resource customer')
        if (!await dialog().getByRole('radio', { name: '代客户接入', exact: true }).isChecked() ||
          !await dialog().getByRole('radio', { name: '我的工作空间', exact: true }).isDisabled())
          throw Error('Demo restore can replace its fixed target workspace')
        await close()
      } else if (await page.getByRole('button', { name: '重新核验', exact: true }).count())
        throw Error('Unauthorized reproof entry for ' + variant)
    }
    return { passed: true, synthetic_only: true, platforms: ['wsl', 'lajiaoliao'],
      viewports: [320, 390, 768, 1440], challenges: 2, confirmations: 2,
      scoped_reopen: true, fixed_group_and_version: true, restricted_roles: true,
      native_mutations: 0, model_calls: 0, screenshots: shots }
  } finally {
    await Promise.all([...pendingRequests])
    await page.unroute('**/api/v1/**', handler)
    await page.unroute(resetURL, reset)
  }
}
