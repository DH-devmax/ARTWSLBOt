// All API responses are synthetic. This test never reaches a native platform.
async (page) => {
  const captcha = await process.getBuiltinModule('vm').runInThisContext(
    `(${process.getBuiltinModule('fs').readFileSync('tools/web_captcha_ui_smoke.js', 'utf8')})`,
    { filename: 'web_captcha_ui_smoke.js' }
  )(page, { fixtureOnly: true })
  const base = new URL(page.url()).origin
  const id = n => `ca620000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const now = new Date().toISOString(), expires = new Date(Date.now() + 600000).toISOString()
  const customers = Array.from({ length: 62 }, (_, n) => ({ tenant_id: id(100 + n), name: `合成客户 ${n + 1}` }))
  const owner = { id: id(2), tenant_id: customers[0].tenant_id, tenant_role: 'owner', role: 'user', username: '合成负责人' }
  const admin = { id: id(1), role: 'superadmin', username: '合成超管' }
  const ownSpace = { id: id(90), name: '我的演示工作空间', kind: 'operator_demo', owner_id: admin.id }
  const space = () => mode === 'operator_demo' ? ownSpace :
    { id: owner.tenant_id, name: '合成客户 1', kind: 'tenant_business', owner_id: owner.id }
  const plan = () => mode === 'operator_demo'
    ? { id: 'operator_unlimited', name: '超管专属 · 不限额', system_only: true,
      permanent: true, unlimited: true, active: true, platforms: [], group_limit: null, member_limit: null, assistant_limit: null, credit_limit: null }
    : { id: id(91), name: 'PRO', active: true, system_only: false, platforms: ['wsl', 'lajiaoliao'],
      modules: ['management_assistant'], group_limit: 30, member_limit: 10, assistant_limit: 100, credit_limit: 1000000 }
  const grants = ['overview:read', 'groups:read', 'claims:create', 'operations:read', 'assistant:use']
  let mode = 'operator_demo', challenge, delayedContext, releaseContext, contextReady
  const writes = [], reads = [], screenshots = []
  const handler = async route => {
    if (await captcha.handle(route)) return
    const req = route.request(), url = new URL(req.url())
    captcha.consume(req)
    const path = url.pathname.replace('/api/v1', ''), platform = req.headers()['x-workspace'] || 'wsl'
    let data = []
    if (req.method() === 'GET') {
      reads.push({ path, platform })
      if (path === '/me') data = { user: mode === 'operator_demo' ? admin : owner, permissions: grants }
      else if (path === '/workspaces') data = { items: [{ id: 'user', name: '用户中心' }, { id: 'wsl', name: '旺商聊机器人' }, { id: 'lajiaoliao', name: '辣椒聊机器人' }] }
      else if (path === '/user-center') data = {
        workspace: space(), effective_plan: plan(),
        billing_source: mode === 'operator_demo' ? 'platform_demo' : 'tenant_wallet',
        quotas: { groups: plan().group_limit, members: plan().member_limit, assistant: plan().assistant_limit, credits: plan().credit_limit },
        entitlement: mode === 'operator_demo' ? { mode, name: '演示测试套餐', unlimited: true, active: true, permissions: grants } : { mode, permissions: grants },
        ...(mode !== 'operator_demo' ? { subscription: { active: true, platforms: ['wsl', 'lajiaoliao'], name: 'PRO', modules: ['management_assistant'], group_limit: 30 } } : {}),
        groups: [], operations: [], permissions: grants
      }
      else if (path === '/user-center/groups') data = { workspace: space(), effective_plan: plan(),
        items: [], total: 0, page: 1, page_size: 20, customers: [], sampled_at: now }
      else if (path === '/user-center/usage') data = { workspace: space(), effective_plan: plan(),
        billing_source: mode === 'operator_demo' ? 'platform_demo' : 'tenant_wallet', call_count: 0, sampled_at: now }
      else if (path === '/group-binding/context') {
        if (url.searchParams.get('platform') !== platform) throw Error('Customer context scope mismatch')
        const offset = Number(url.searchParams.get('offset')), limit = Number(url.searchParams.get('limit'))
        if (limit > 50 || offset < 0) throw Error('Unbounded customer selection')
        const list = customers.filter(item => !url.searchParams.get('search') || item.name.includes(url.searchParams.get('search')))
        data = {
          mode, workspace: space(), default_target_tenant_id: space().id,
          can_onboard_for_customer: mode === 'operator_demo',
          customers: mode === 'operator_demo'
            ? list.slice(offset, offset + limit)
            : [{ tenant_id: owner.tenant_id, name: '合成客户 1' }],
          next_offset: mode === 'operator_demo' && offset + limit < list.length ? offset + limit : -1
        }
        if (delayedContext) {
          delayedContext = false
          contextReady?.()
          await new Promise(resolve => { releaseContext = resolve })
        }
      }
      else if (/^\/group-binding\/challenges\/[^/]+$/.test(path)) data = { ...challenge, friend_challenge: undefined, group_challenge: undefined, private_challenge: undefined }
    } else {
      const body = req.postDataJSON()
      writes.push({ path, platform, body, key: req.headers()['idempotency-key'] })
      if (path === '/group-binding/challenges') {
        const fields = Object.keys(body).sort().join()
        if (fields !== (mode === 'operator_demo' ? 'platform,target_tenant_id' : 'platform') ||
          body.platform !== platform || !req.headers()['idempotency-key']) throw Error('Untrusted binding request')
        const customer = mode === 'operator_demo'
          ? body.target_tenant_id === ownSpace.id ? { tenant_id: ownSpace.id, name: ownSpace.name } :
            customers.find(item => item.tenant_id === body.target_tenant_id)
          : customers[0]
        if (!customer) throw Error('Missing resource customer')
        challenge = { id: id(platform === 'wsl' ? 3 : 4), platform, mode, customer, state: 'waiting', version: 1, expires_at: expires,
          workspace: { ...space(), id: customer.tenant_id, name: customer.name },
          robot: { id: id(5), name: platform === 'wsl' ? '旺商测试机器人' : '辣椒测试机器人', native_id: 'synthetic-robot',
            add_account: `public-${platform}-robot`, add_available: true },
          friend_challenge: `群接入 ${'A'.repeat(20)}`, can_verify_friend: true,
          group_challenge: '群绑定 SYNTHETIC_PROOF', private_challenge: '群接入 SYNTHETIC_PRIVATE',
          can_confirm: false, can_cancel: true, can_accept_invitation: false, identity_proven: false }
        data = challenge
      } else if (/\/confirm$/.test(path)) {
        if (Object.keys(body).join() !== 'version' || body.version !== challenge.version || !challenge.can_confirm ||
          !req.headers()['idempotency-key']) throw Error('Confirmation omitted proof/version')
        challenge = { ...challenge, version: 3, state: 'bound', can_confirm: false, can_cancel: false }
        data = challenge
      } else if (/\/cancel$/.test(path)) data = { ...challenge, state: 'cancelled', can_cancel: false }
      else throw Error('Unexpected effect ' + path)
    }
    await route.fulfill({ json: { code: 0, msg: '', data } })
  }
  const modal = () => page.getByRole('dialog', { name: mode === 'operator_demo' ? '接入群组' : '接入我的群组', exact: true })
  const open = async () => {
    const button = page.getByRole('region', { name: '工作台接入指引', exact: true })
      .getByRole('button', { name: mode === 'operator_demo' ? '绑定群组' : '接入新群组', exact: true })
    await button.click()
    await modal().waitFor()
    if (await page.evaluate(() => location.hash !== '#overview') ||
      await page.evaluate(actor => sessionStorage.getItem(`wsl:console:${actor}`) !== 'user', mode === 'operator_demo' ? admin.id : owner.id))
      throw Error('Binding left the user center')
  }
  const chooseCustomer = async () => {
    await modal().locator('label.el-radio').filter({ hasText: '代客户接入' }).click()
    if (!await modal().getByRole('button', { name: '开始核验', exact: true }).isDisabled())
      throw Error('Customer target was implicit')
    const select = modal().getByRole('combobox', { name: '群组所属客户', exact: true })
    await select.click()
    await page.getByRole('button', { name: '加载更多客户', exact: true }).click()
    await page.getByRole('option', { name: '合成客户 21', exact: true }).waitFor()
    await select.fill('合成客户 42')
    await page.getByRole('option', { name: '合成客户 42', exact: true }).click()
  }
  const reset = async (platform, nextMode) => {
    mode = nextMode
    challenge = undefined
    await page.goto(base)
    await page.evaluate(({ actor, platform }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`, 'user')
      sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
    }, { actor: mode === 'operator_demo' ? admin.id : owner.id, platform })
    await page.goto(base + '/#overview')
    await page.reload()
    await page.locator('.uc-start').waitFor()
  }
  await page.route('**/api/v1/**', handler)
  try {
    for (const platform of ['wsl', 'lajiaoliao']) {
      await reset(platform, 'operator_demo')
      let count = writes.length
      await open()
      await modal().getByRole('radio', { name: '我的工作空间', exact: true }).waitFor()
      if (writes.length !== count || !await modal().getByRole('radio', { name: '我的工作空间', exact: true }).isChecked() ||
        await modal().getByRole('combobox', { name: '群组所属客户', exact: true }).count())
        throw Error('Opening wizard reserved a robot or changed own workspace default')
      if (await modal().locator('.onboarding-steps li').count() !== 5) throw Error('Missing five-step binding flow')
      await modal().getByRole('button', { name: '关闭', exact: true }).click()
      await modal().waitFor({ state: 'hidden' })
      if (writes.length !== count) throw Error('Closing wizard submitted')
      await open()
      await chooseCustomer()
      for (const width of [320, 390, 768, 1440]) {
        await page.setViewportSize({ width, height: 779 })
        await page.waitForTimeout(60)
        const box = await modal().getByRole('button', { name: '开始核验', exact: true }).boundingBox()
        if (!box || box.x < 0 || box.x + box.width > width + 1 || box.y + box.height > 780) throw Error('Wizard footer overflow ' + width)
        const file = `output/playwright/binding-entry-${platform}-${width}.png`
        await page.screenshot({ path: file, animations: 'disabled' })
        screenshots.push(file)
      }
      await modal().getByRole('button', { name: '开始核验', exact: true }).click()
      await captcha.complete()
      await modal().getByText(platform === 'wsl' ? '旺商测试机器人' : '辣椒测试机器人', { exact: true }).waitFor()
      if (writes.length !== count + 1 || writes.at(-1).body.target_tenant_id !== customers[41].tenant_id)
        throw Error('Robot allocation repeated or selected customer was lost')
      challenge = { ...challenge, state: 'proven', version: 2, proof_role: 'owner', can_confirm: true,
        group: { id: id(6), name: '同名测试群', native_id: 'synthetic-group' } }
      await modal().getByRole('button', { name: '刷新进度', exact: true }).click()
      await modal().getByRole('button', { name: '确认绑定', exact: true }).click()
      const confirmation = page.getByRole('dialog', { name: '确认群归属', exact: true })
      await confirmation.getByText('合成客户 42', { exact: true }).waitFor()
      if (writes.length !== count + 1) throw Error('Opening confirmation submitted')
      await confirmation.getByRole('button', { name: '确认绑定', exact: true }).click()
      await modal().getByText('绑定成功', { exact: true }).waitFor()
      if (writes.length !== count + 2) throw Error('Confirmation repeated')
    }
    await reset('wsl', 'tenant_business')
    await open()
    if (await modal().getByRole('combobox', { name: '群组所属客户', exact: true }).count())
      throw Error('Customer was allowed to choose another tenant')
    const contextReads = reads.filter(row => row.path === '/group-binding/context').length
    await modal().getByRole('button', { name: '开始核验', exact: true }).click()
    await captcha.complete()
    await modal().getByText('旺商测试机器人', { exact: true }).waitFor()
    if ('target_tenant_id' in writes.at(-1).body || reads.filter(row => row.path === '/group-binding/context').length !== contextReads)
      throw Error('Customer requested operator scope')
    await modal().getByRole('button', { name: '关闭', exact: true }).click()
    await reset('wsl', 'operator_demo')
    delayedContext = true
    const started = new Promise(resolve => { contextReady = resolve })
    await open()
    await started
    await modal().getByRole('button', { name: '关闭', exact: true }).click()
    releaseContext?.()
    await modal().waitFor({ state: 'hidden' })
    return { passed: true, synthetic_only: true, platforms: ['wsl', 'lajiaoliao'], in_user_center: true,
      opening_and_closing_do_not_submit: true, resource_customer_confirmed: true, customer_cannot_choose_tenant: true,
      customer_search_and_pagination: true,
      native_calls: 0, model_calls: 0, writes: writes.length, screenshots }
  } finally {
    releaseContext?.()
    await page.unroute('**/api/v1/**', handler)
  }
}
