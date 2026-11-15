// Synthetic APIs only. Every request is intercepted; no model or native service is contacted.
// CLI: WSL_UI_ASSET_ROOT=/path/to/assets WSL_PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node tools/assistant_ownership_ui_smoke.js
// prettier-ignore
(() => {
const smoke = async (page) => {
  const origin = new URL(page.url()).origin
  const id = n => `d7000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const check = (condition, message) => { if (!condition) throw Error(message) }
  const now = new Date().toISOString(), expires = new Date(Date.now() + 600000).toISOString()
  const permissions = ['overview:read', 'accounts:read', 'groups:read', 'assistant:use',
    'assistant:propose', 'assistant:approve', 'assistant:action_propose', 'assistant:action_approve', 'group_action:send_text']
  const kinds = ['unverified', 'active', 'paused', 'legacy']
  const names = { unverified: '合成待认领群', active: '合成已认领群', paused: '合成暂停群', legacy: '合成旧响应群' }
  const accounts = ['wsl', 'lajiaoliao'].map((platform, i) => ({
    id: id(10 + i), platform, workspace: platform, purpose: 'management',
    name: '合成机器人', native_nickname: '合成机器人'
  }))
  const mutations = [], unexpected = [], foreign = [], screenshots = [], measurements = [], claimReads = []
  const selectedKinds = new Map()
  let claimed = false, claimRevoked = false, claimExpires = expires, expireOnQuery = false, readonlyToolResults = 0, contextReads = 0,
    role = 'superadmin', live, serial = 900, delayed
  const held = new Set()
  const access = kind => kind === 'legacy' ? undefined : {
    ownership_state: kind,
    access_mode: kind === 'active' || (kind === 'unverified' && claimed) ? 'manage' : 'read_only',
    blocked_reason: kind === 'unverified' && !claimed ? (claimRevoked ? 'operator_demo_claim_required' : 'group_ownership_proof_required')
      : kind === 'paused' ? 'group_ownership_reproof_required' : ''
  }
  const group = (platform, kind) => ({
    group_id: id((platform === 'wsl' ? 20 : 30) + kinds.indexOf(kind)), name: names[kind],
    platform, active: true, primary_account_id: accounts.find(a => a.platform === platform).id,
    native_id: `synthetic-${platform}-${kind}`, robot_nickname: '合成机器人', ...access(kind)
  })
  const kindOf = (platform, groupID) => kinds.find(kind => group(platform, kind).group_id === groupID) || 'unverified'
  const scope = (platform, kind) => ({ platform, kind: 'group', group_id: group(platform, kind).group_id,
    account_id: group(platform, kind).primary_account_id })
  const sessionFor = (platform, kind) => ({
    id: id((platform === 'wsl' ? 100 : 110) + kinds.indexOf(kind)), scope: scope(platform, kind),
    title: '认领与查询合成回归', is_empty: false, created_at: now
  })
  const sessionByID = sessionID => ['wsl', 'lajiaoliao'].flatMap(platform =>
    kinds.map(kind => ({ platform, kind, session: sessionFor(platform, kind) }))).find(row => row.session.id === sessionID)
  const card = (platform, kind, type, data) => ({
    type, scope: scope(platform, kind), data, sampled_at: now, verification: 'query_snapshot', truncated: false
  })
  const transcript = (platform, kind) => [{
    id: id((platform === 'wsl' ? 200 : 210) + kinds.indexOf(kind)), session_id: sessionFor(platform, kind).id,
    input: '查看配置与历史建议', status: 'succeeded', result_code: '', cursor: 5,
    answer: '## 查询总结\n\n**合成查询完成**，仅当前群快照。\n\n- 配置已读取\n- 等待本人核对',
    cards: [
      card(platform, kind, 'get_group_config', { version: 7, values: { blocked_words: { enabled: false, words: ['合成词'] } } }),
      card(platform, kind, 'get_group_capabilities', { actions: [{ id: 'send_text', name: '发送群文本', production_admitted: true }] }),
      card(platform, kind, 'propose_group_config_change', { id: id(300), summary: '配置尚未保存' }),
      card(platform, kind, 'propose_group_action', { id: id(301), summary: '消息尚未发送' }),
      card(platform, kind, 'find_group_members', [{ id: 'synthetic-member', name: '合成成员', role: 'member', token: 'synthetic-token' }])
    ],
    outcomes: [{ plan_id: id(302), kind: 'action', status: 'unknown', evidence_state: 'unknown', summary: '历史结果待核对，不会自动重发。' }]
  }, ...(live?.session_id === sessionFor(platform, kind).id ? [live] : [])]
  const context = (platform, url) => {
    const kind = kindOf(platform, url.searchParams.get('group_id'))
    const list = (role === 'superadmin' ? kinds : ['active']).map(key => group(platform, key))
      .filter(item => !url.searchParams.get('search') || item.name.includes(url.searchParams.get('search')))
    return { platform, scope: scope(platform, kind), groups: list, group_access: access(kind),
      permissions, total: list.length, next_offset: -1, sampled_at: now,
      capabilities: [
        { id: 'config.query', name: '拦截词', tool: 'get_group_config', category: 'query', business_permission: 'groups:read', enabled: true, implemented: true },
        { id: 'config.propose', name: '拦截词建议', tool: 'propose_group_config_change', category: 'config_propose', business_permission: 'assistant:propose', enabled: true, implemented: true },
        { id: 'action.propose', name: '发送群文本', tool: 'propose_group_action', action: 'send_text', category: 'action_propose', business_permission: 'group_action:send_text', enabled: true, implemented: true }
      ], actions: [{ id: 'send_text', production_admitted: true }] }
  }
  const handler = async route => {
    const request = route.request(), url = new URL(request.url())
    if (url.origin !== origin) { foreign.push(url.href); return route.abort('blockedbyclient') }
    if (!url.pathname.startsWith('/api/')) return route.fallback()
    const path = url.pathname.replace('/api/v1', ''), platform = request.headers()['x-workspace'] || 'wsl'
    let data = []
    if (request.method() !== 'GET') {
      mutations.push({ path, platform })
      if (request.method() !== 'POST' || !/^\/assistant\/sessions\/[^/]+\/turns$/.test(path)) {
        unexpected.push(path)
        return route.fulfill({ status: 500, json: { code: 1, msg: 'unexpected_mutation' } })
      }
      live = { id: id(serial++), session_id: path.split('/')[3], status: 'queued', result_code: '',
        input: request.postDataJSON().input, answer: '', cards: [], cursor: 0 }
      data = live
    } else if (path === '/me') data = { user: { id: id(1), tenant_id: id(2), username: 'synthetic-ownership', role }, permissions }
    else if (path === '/workspaces') data = { items: [{ id: 'wsl', name: '旺商聊' }, { id: 'lajiaoliao', name: '辣椒聊' }] }
    else if (path === '/accounts') data = accounts.filter(account => account.platform === platform)
    else if (path === '/groups') data = (role === 'superadmin' ? kinds : ['active']).map(kind => group(platform, kind))
    else if (/^\/user-center\/groups\/[^/]+\/demo-claim$/.test(path)) {
      const groupID = path.split('/')[3]
      claimReads.push({ platform, group: groupID })
      data = { id: id(600), group_id: groupID, platform, state: claimed ? 'active' : 'expired',
        expires_at: claimExpires, blocked_reason: claimed ? '' : 'operator_demo_claim_expired' }
      if (delayed?.platform === platform && delayed.group === groupID && delayed.help === 'claim') {
        const pending = delayed; delayed = undefined
        await new Promise(resolve => { pending.release = resolve; held.add(pending); pending.started() })
        held.delete(pending)
      }
    }
    else if (path === '/assistant/context') {
      contextReads++
      data = context(platform, url)
      if (delayed && delayed.platform === platform && delayed.group === url.searchParams.get('group_id') &&
        delayed.help === (url.searchParams.get('limit') === '1')) {
        const pending = delayed; delayed = undefined
        await new Promise(resolve => { pending.release = resolve; held.add(pending); pending.started() })
        held.delete(pending)
      }
    } else if (path === '/assistant/usage') data = { unit: 'micro_credit', used_or_reserved: 1000000 }
    else if (path === '/assistant/sessions') {
      const kind = kindOf(platform, url.searchParams.get('group_id'))
      selectedKinds.set(platform, kind)
      data = [sessionFor(platform, kind)]
    }
    else if (/^\/assistant\/sessions\/[^/]+\/transcript$/.test(path)) {
      const selected = sessionByID(path.split('/')[3])
      check(selected?.platform === platform, 'Transcript crossed platforms')
      data = transcript(selected.platform, selected.kind)
    } else if (/^\/assistant\/turns\/[^/]+\/events$/.test(path)) {
      check(live && live.id === path.split('/')[3], 'Unknown synthetic turn')
      if (expireOnQuery) { claimed = false; claimRevoked = true; expireOnQuery = false; readonlyToolResults++ }
      const selected = sessionByID(live.session_id)
      check(selected?.platform === platform, 'Query result crossed platforms')
      live = { ...live, status: 'succeeded', answer: '## 授权查询完成\n\n只读群仍可查询。', cursor: 3 }
      const events = [{ kind: 'tool_result', data: card(platform, selected.kind, 'get_group_config', {
        group_id: scope(platform, selected.kind).group_id, account_id: scope(platform, selected.kind).account_id,
        version: 7, values: { blocked_words: { words: ['合成词'] } }, group_access: access(selected.kind)
      }) }, { kind: 'text_delta', data: { text: live.answer } },
        { kind: 'turn_finished', data: { status: 'succeeded', code: '' } }]
        .map((event, i) => ({ ...event, turn_id: live.id, seq: i + 1 }))
      return route.fulfill({ contentType: 'text/event-stream; charset=utf-8',
        body: events.map(event => `data: ${JSON.stringify(event)}\n\n`).join('') })
    } else if (/^\/assistant\/turns\/[^/]+$/.test(path)) data = live
    else if (/^\/assistant\/plans\/[^/]+$/.test(path)) {
      const key = selectedKinds.get(platform)
      data = { id: id(300), session_id: sessionFor(platform, key).id, platform, account_id: scope(platform, key).account_id,
        group_id: scope(platform, key).group_id, status: 'pending_approval', base_version: 7, version: 1, digest: 'synthetic',
        expires_at: expires, before: { blocked_words: { enabled: false } }, changes: { blocked_words: { enabled: true } },
        message: '合成配置建议', summary: '尚未保存', applied: false }
    } else if (/^\/assistant\/action-plans\/[^/]+$/.test(path)) {
      const kind = selectedKinds.get(platform)
      data = { id: id(301), session_id: sessionFor(platform, kind).id, scope: scope(platform, kind), action: 'send_text',
        name: '发送群文本', text: '合成文本', status: 'pending_approval', result_code: '', version: 1, digest: 'synthetic',
        expires_at: expires, production_admitted: true, summary: '尚未发送' }
    }
    try { await route.fulfill({ json: { code: 0, msg: 'ok', data } }) }
    catch (error) { if (!/closed|intercept|invalid|handled/i.test(String(error))) throw error }
  }
  const region = () => page.getByRole('region', { name: '助手会话工作台' })
  const choose = async (platform, kind) => {
    await region().getByRole('radiogroup', { name: '助手平台', exact: true }).locator('label')
      .filter({ hasText: platform === 'wsl' ? '旺商聊' : '辣椒聊' }).click()
    const mobile = region().locator('.assistant-mobile-groups')
    if (await mobile.isVisible() && await mobile.getAttribute('aria-expanded') !== 'true') await mobile.click()
    await region().getByRole('button', { name: new RegExp('选择群组 ' + names[kind]) }).click()
    await region().locator('.assistant-heading strong').filter({ hasText: names[kind] }).waitFor()
    await region().locator('.assistant-model-answer h2').first().waitFor()
  }
  const openHelp = async () => {
    await region().getByRole('button', { name: '助手能力说明', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '助手能力说明', exact: true })
    await dialog.locator('.assistant-help-access').waitFor()
    return dialog
  }
  const closeHelp = async dialog => {
    await dialog.getByRole('button', { name: '知道了', exact: true }).click()
    await dialog.waitFor({ state: 'hidden' })
  }
  const section = (dialog, title) => dialog.locator('.assistant-help-section').filter({
    has: page.getByRole('heading', { name: title, exact: true })
  })
  const delayNext = (platform, kind, help) => {
    let started
    const ready = new Promise(resolve => { started = resolve })
    const pending = { platform, group: group(platform, kind).group_id, help, started }
    delayed = pending
    return { ready, pending }
  }
  await page.route('**/*', handler)
  try {
    await page.evaluate(actor => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`, 'user')
    }, id(1))
    for (const platform of ['wsl', 'lajiaoliao']) for (const width of [320, 390, 768, 1440]) {
      claimed = false; live = undefined
      await page.setViewportSize({ width, height: width === 320 ? 600 : 779 })
      await page.goto(origin + '/#assistant'); await page.reload()
      await choose(platform, 'unverified')
      await region().locator('.assistant-ownership-status').getByText('未认领 · 只读', { exact: true }).waitFor()
      const mobile = region().locator('.assistant-mobile-groups')
      if (await mobile.isVisible()) await mobile.click()
      const picker = region().getByRole('region', { name: '授权群组选择' })
      check((await picker.getByRole('button', { name: /选择群组 合成待认领群/ }).innerText()).includes('未认领 · 只读'), 'Unclaimed picker label missing')
      check((await picker.getByRole('button', { name: /选择群组 合成暂停群/ }).innerText()).includes('需重新核验 · 只读'), 'Paused picker label missing')
      if (await mobile.isVisible()) await mobile.click()
      check((await region().locator('.assistant-ownership-status').getAttribute('aria-label')).includes('尚未认领'), 'Selected group block reason missing')
      check(await region().getByRole('button', { name: '查看配置差异', exact: true }).count() === 0, 'Read-only config appears executable')
      check(await region().getByRole('button', { name: '查看动作与影响', exact: true }).count() === 0, 'Read-only action appears executable')
      check(await region().getByRole('button', { name: '选择成员', exact: true }).isDisabled(), 'Read-only target remains executable')
      const process = region().locator('.assistant-tool-process').first()
      check(await process.getAttribute('open') === null, 'Tool process expanded itself')
      await process.locator('summary').click()
      check(await process.getByText('可本人确认', { exact: true }).count() === 0, 'Read-only capability card still promises execution')
      await process.locator('summary').click()
      let dialog = await openHelp()
      check((await dialog.locator('.assistant-help-access').innerText()).includes('未认领 · 只读'), 'Help ownership state missing')
      check((await dialog.locator('.assistant-help-access').innerText()).includes('尚未认领'), 'Help blocked reason missing')
      check(await section(dialog, '可以查询').getByText('拦截词配置', { exact: true }).isVisible(), 'Read-only authorized query hidden')
      check(await section(dialog, '当前不可用').getByText('拦截词建议', { exact: true }).isVisible(), 'Read-only config not listed as unavailable')
      check(await section(dialog, '当前不可用').getByText('发送群文本', { exact: true }).isVisible(), 'Read-only action not listed as unavailable')
      const close = await dialog.getByRole('button', { name: '知道了', exact: true }).boundingBox()
      check(close && close.y + close.height <= page.viewportSize().height + 1, 'Help footer outside viewport')
      const helpShot = `output/playwright/assistant-ownership-help-${platform}-${width}.png`
      await page.screenshot({ path: helpShot, animations: 'disabled' }); screenshots.push(helpShot)
      await closeHelp(dialog)
      const input = region().getByRole('textbox', { name: '助手问题', exact: true }), heights = []
      for (const lines of [1, 3, 5, 8]) {
        await input.fill(Array.from({ length: lines }, () => '合成查询').join('\n'))
        await page.waitForTimeout(40)
        heights.push(await input.evaluate(el => ({ height: el.clientHeight, scroll: el.scrollHeight })))
      }
      check(heights[0].height < heights[1].height && heights[1].height < heights[2].height, 'Composer lost line growth')
      check(Math.abs(heights[2].height - heights[3].height) <= 2 && heights[3].scroll > heights[3].height, 'Composer no longer caps at five lines')
      await input.fill('查看群配置')
      await region().getByRole('button', { name: '发送', exact: true }).click()
      await region().getByRole('heading', { name: '授权查询完成', exact: true }).waitFor()
      await region().locator('.assistant-turn').last().getByText('本轮结束', { exact: true }).waitFor()
      await input.fill('')
      const dimensions = await region().evaluate(el => ({
        header: el.querySelector('header').getBoundingClientRect().height, sidebar: el.querySelector('aside').getBoundingClientRect().height,
        footer: el.querySelector('footer').getBoundingClientRect().height, page: document.documentElement.scrollWidth, width: innerWidth
      }))
      check(dimensions.page <= width + 1 && dimensions.footer <= 120, 'Ownership UI overflow or tall footer')
      check(dimensions.header <= 80, 'Ownership status enlarged the compact heading')
      if (width < 768) check(dimensions.header + dimensions.sidebar <= 100, 'Ownership UI enlarged mobile heading')
      measurements.push({ platform, width, dimensions, heights })
      const shot = `output/playwright/assistant-ownership-${platform}-${width}.png`
      await page.screenshot({ path: shot, animations: 'disabled' }); screenshots.push(shot)
      // A personal demo claim permits management without changing customer ownership.
      claimed = true
      await choose(platform, 'unverified')
      await region().locator('.assistant-ownership-status').getByText('测试认领 · 可管理', { exact: true }).waitFor()
      check(access('unverified').ownership_state === 'unverified' && access('unverified').access_mode === 'manage', 'Demo claim changed customer ownership')
      check(await region().getByRole('button', { name: '选择成员', exact: true }).isEnabled(), 'Demo claim did not enable target selection')
      await region().getByRole('button', { name: '查看配置差异', exact: true }).click()
      const config = page.getByRole('dialog', { name: '配置建议审批', exact: true })
      await config.getByRole('button', { name: '确认保存配置', exact: true }).waitFor()
      check(await config.getByRole('button', { name: '确认保存配置', exact: true }).isEnabled(), 'Demo claim did not enable config confirmation')
      await page.keyboard.press('Escape'); await config.waitFor({ state: 'hidden' })
      await region().getByRole('button', { name: '查看动作与影响', exact: true }).click()
      const action = page.getByRole('dialog', { name: '确认群管动作', exact: true })
      await action.getByRole('button', { name: '确认发送', exact: true }).waitFor()
      check(await action.getByRole('button', { name: '确认发送', exact: true }).isEnabled(), 'Demo claim did not enable action confirmation')
      await page.keyboard.press('Escape'); await action.waitFor({ state: 'hidden' })
      dialog = await openHelp()
      check((await dialog.locator('.assistant-help-access').innerText()).trim() === '测试认领 · 可管理', 'Help mislabels personal demo claim as customer ownership')
      check(await section(dialog, '可以查询').getByText('拦截词配置', { exact: true }).isVisible(), 'Demo claim hid authorized query')
      check(await section(dialog, '可以提出建议 · 需要本人确认').getByText('拦截词建议', { exact: true }).isVisible(), 'Demo claim cannot propose config')
      check(await section(dialog, '可以提出建议 · 需要本人确认').getByText('发送群文本', { exact: true }).isVisible(), 'Demo claim cannot propose action')
      await closeHelp(dialog)
      // Synthetic clearclaim: a fresh help snapshot revokes personal demo management.
      claimed = false
      dialog = await openHelp()
      await region().locator('.assistant-ownership-status').getByText('未认领 · 只读', { exact: true }).waitFor()
      check(access('unverified').ownership_state === 'unverified' && access('unverified').access_mode === 'read_only', 'Clearclaim changed customer ownership or retained management')
      check((await dialog.locator('.assistant-help-access').innerText()).includes('未认领 · 只读'), 'Clearclaim did not restore read-only help')
      check((await dialog.locator('.assistant-help-access').innerText()).includes('尚未认领'), 'Clearclaim help lost block reason')
      check(await section(dialog, '可以查询').getByText('拦截词配置', { exact: true }).isVisible(), 'Clearclaim hid authorized query')
      check(await section(dialog, '当前不可用').getByText('拦截词建议', { exact: true }).isVisible(), 'Clearclaim left config available in help')
      check(await section(dialog, '当前不可用').getByText('发送群文本', { exact: true }).isVisible(), 'Clearclaim left action available in help')
      await closeHelp(dialog)
      check(await region().getByRole('button', { name: '查看配置差异', exact: true }).count() === 0, 'Clearclaim did not fence config controls')
      check(await region().getByRole('button', { name: '查看动作与影响', exact: true }).count() === 0, 'Clearclaim did not fence action controls')
      check(await region().getByRole('button', { name: '选择成员', exact: true }).isDisabled(), 'Clearclaim did not fence target selection')
      await choose(platform, 'paused')
      dialog = await openHelp()
      check((await dialog.locator('.assistant-help-access').innerText()).includes('重新核验'), 'Paused help reason missing')
      await closeHelp(dialog)
      await choose(platform, 'legacy')
      dialog = await openHelp()
      check((await dialog.locator('.assistant-help-access').innerText()).trim() === '认领状态待核对', 'Missing legacy fields implied ownership')
      check(await section(dialog, '可以提出建议 · 需要本人确认').getByText('拦截词建议', { exact: true }).isVisible(), 'Legacy response compatibility lost')
      await closeHelp(dialog)
    }
    // A new query can discover revocation without reopening help or selecting a group.
    await page.setViewportSize({ width: 390, height: 779 })
    for (const platform of ['wsl', 'lajiaoliao']) {
      claimed = true; live = undefined
      await choose(platform, 'unverified')
      await region().locator('.assistant-ownership-status').getByText('测试认领 · 可管理', { exact: true }).waitFor()
      expireOnQuery = true
      await region().getByRole('textbox', { name: '助手问题', exact: true }).fill('核对当前群配置')
      await region().getByRole('button', { name: '发送', exact: true }).click()
      await region().getByRole('heading', { name: '授权查询完成', exact: true }).waitFor()
      await region().locator('.assistant-ownership-status').getByText('未认领 · 只读', { exact: true }).waitFor()
      check((await region().locator('.assistant-ownership-status').getAttribute('aria-label')).includes('测试认领'), 'Fresh context lost the operator block reason')
      check(await region().getByRole('button', { name: '查看配置差异', exact: true }).count() === 0, 'Fresh query retained configuration controls')
      check(await region().getByRole('button', { name: '查看动作与影响', exact: true }).count() === 0, 'Fresh query retained action controls')
      check(await region().getByRole('button', { name: '选择成员', exact: true }).isDisabled(), 'Fresh query retained target selection')
      await region().locator('.assistant-turn').last().getByText('本轮结束', { exact: true }).waitFor()
    }
    // Delayed context and help snapshots must not cross selected groups or platforms.
    await choose('wsl', 'active')
    let hold = delayNext('wsl', 'unverified', false)
    await choose('wsl', 'unverified'); await hold.ready
    await choose('wsl', 'active')
    hold.pending.release(); await page.waitForTimeout(80)
    check((await region().locator('.assistant-ownership-status').innerText()) === '已认领', 'Late group context replaced selected access')
    await choose('wsl', 'unverified')
    for (const nextPlatform of ['wsl', 'lajiaoliao']) {
      hold = delayNext('wsl', 'unverified', true)
      await region().getByRole('button', { name: '助手能力说明', exact: true }).click()
      await hold.ready
      await page.keyboard.press('Escape')
      await page.getByRole('dialog', { name: '助手能力说明', exact: true }).waitFor({ state: 'hidden' })
      await choose(nextPlatform, 'active')
      const dialog = await openHelp()
      hold.pending.release(); await page.waitForTimeout(80)
      check((await dialog.locator('.assistant-help-access').innerText()).trim() === '已认领', 'Late help response replaced current ownership')
      check((await dialog.locator('.assistant-help-scope').innerText()).includes(nextPlatform === 'wsl' ? '旺商聊' : '辣椒聊'), 'Help crossed platforms')
      await closeHelp(dialog)
      await choose('wsl', 'unverified')
    }
    role = 'custom'
    for (const platform of ['wsl', 'lajiaoliao']) {
      await page.reload()
      await choose(platform, 'active')
      const mobile = region().locator('.assistant-mobile-groups')
      if (await mobile.isVisible()) await mobile.click()
      const picker = region().getByRole('region', { name: '授权群组选择' })
      check(await picker.getByRole('button', { name: /选择群组/ }).count() === 1, 'Ordinary user list differs from server-assigned active groups')
      check(!(await picker.innerText()).includes(names.unverified), 'Ordinary list exposed unclaimed group')
    }
    // Keep the server snapshot manageable during the early expiry fence. A local
    // downgrade must close approval controls without creating a refresh loop.
    role = 'superadmin'
    await page.clock.install({ time: new Date() })
    await page.reload()
    for (const platform of ['wsl', 'lajiaoliao']) {
      claimed = true; live = undefined
      claimExpires = await page.evaluate(() => new Date(Date.now() + 23000).toISOString())
      const deadlineRead = page.waitForResponse(response => response.url().endsWith(`/groups/${group(platform, 'unverified').group_id}/demo-claim`))
      await choose(platform, 'unverified')
      await deadlineRead
      await region().locator('.assistant-ownership-status').getByText('测试认领 · 可管理', { exact: true }).waitFor()
      const configPlan = platform === 'wsl'
      await region().getByRole('button', { name: configPlan ? '查看配置差异' : '查看动作与影响', exact: true }).click()
      const approval = page.getByRole('dialog', { name: configPlan ? '配置建议审批' : '确认群管动作', exact: true })
      const confirmation = approval.getByRole('button', { name: configPlan ? '确认保存配置' : '确认发送', exact: true })
      await confirmation.waitFor()
      await page.waitForLoadState('networkidle')
      await region().getByRole('textbox', { name: '助手问题', exact: true }).fill('到期后保留的合成草稿', { force: true })
      const beforeExpiryReads = [contextReads, claimReads.length]
      await page.clock.fastForward(21000)
      await region().locator('.assistant-ownership-status').getByText('未认领 · 只读', { exact: true }).waitFor()
      check(await confirmation.count() === 0, 'Expired personal claim kept an open confirmation executable')
      check(await approval.getByText('请先在用户中心为本群完成测试认领', { exact: true }).isVisible(), 'Expired approval lost its block reason')
      check(await region().getByRole('textbox', { name: '助手问题', exact: true }).inputValue() === '到期后保留的合成草稿', 'Expiry erased the current draft')
      const expiryShot = `output/playwright/assistant-ownership-expired-${platform}.png`
      await page.screenshot({ path: expiryShot, animations: 'disabled' }); screenshots.push(expiryShot)
      await page.clock.fastForward(4000)
      check(contextReads === beforeExpiryReads[0] && claimReads.length === beforeExpiryReads[1],
        'Early expiry fence started automatic context or claim refreshes')
      claimed = false
      await page.keyboard.press('Escape'); await approval.waitFor({ state: 'hidden' })
      check(await region().getByRole('button', { name: '查看配置差异', exact: true }).count() === 0, 'Expiry kept configuration proposals visible')
      check(await region().getByRole('button', { name: '选择成员', exact: true }).isDisabled(), 'Expiry kept target selection enabled')
      check(await region().getByRole('button', { name: '发送', exact: true }).isEnabled(), 'Expiry disabled an authorized query')
    }
    // Missing deadlines use a bounded local fence; no timer exceeds ten minutes.
    for (const lifetime of ['invalid', 86400000]) {
      claimed = true
      claimExpires = lifetime === 'invalid' ? 'invalid' :
        await page.evaluate(ms => new Date(Date.now() + ms).toISOString(), lifetime)
      const deadlineRead = page.waitForResponse(response => response.url().endsWith(`/groups/${group('wsl', 'unverified').group_id}/demo-claim`))
      await choose('wsl', 'unverified')
      await deadlineRead
      await region().locator('.assistant-ownership-status').getByText('测试认领 · 可管理', { exact: true }).waitFor()
      claimed = false
      await page.clock.fastForward(lifetime === 'invalid' ? 61000 : 600001)
      await region().locator('.assistant-ownership-status').getByText('未认领 · 只读', { exact: true }).waitFor()
    }
    await page.waitForLoadState('networkidle')
    const readonlyReads = [contextReads, claimReads.length]
    await page.clock.fastForward(600001)
    check(contextReads === readonlyReads[0] && claimReads.length === readonlyReads[1], 'Expired read-only access kept polling context or personal claims')
    claimed = true
    claimExpires = await page.evaluate(() => new Date(Date.now() + 23000).toISOString())
    const oldClaim = delayNext('wsl', 'unverified', 'claim')
    await choose('wsl', 'unverified'); await oldClaim.ready
    await choose('lajiaoliao', 'active')
    oldClaim.pending.release()
    await page.clock.fastForward(610000)
    check((await region().locator('.assistant-ownership-status').innerText()) === '已认领', 'Late claim response or old timer downgraded another platform')
    check(readonlyToolResults === 2 && claimReads.length > 0, 'Fresh access or claim-deadline regression did not execute')
    check(unexpected.length === 0 && foreign.length === 0, `Unexpected requests: ${JSON.stringify({ unexpected, foreign })}`)
    check(mutations.length === 10, `Expected only ten synthetic queries, got ${JSON.stringify(mutations)}`)
    return { passed: true, synthetic_only: true, platforms: ['wsl', 'lajiaoliao'], viewports: [320, 390, 768, 1440],
      unclaimed_read_only: true, paused_read_only: true, authorized_query_allowed: true, claimed_manage: true,
      demo_claim_unverified_manage: true, clearclaim_read_only: true,
      fresh_query_access_synchronized: true, personal_claim_expiry_read_only: true,
      bounded_deadline_fallback: true, open_approval_fenced_on_expiry: true, late_claim_response_isolated: true,
      expired_read_only_does_not_poll: true, early_expiry_does_not_refresh: true,
      help_downgrade: true, legacy_compatible_without_claim_inference: true, late_group_and_platform_isolated: true,
      ordinary_assigned_groups_only: true, native_calls: 0, model_calls: 0, mutations, measurements, screenshots }
  } finally {
    for (const pending of held) pending.release?.()
    await page.unroute('**/*', handler)
  }
}
if (process.argv[1]?.endsWith('/assistant_ownership_ui_smoke.js')) {
  void (async () => {
    const fs = process.getBuiltinModule('fs/promises'), path = process.getBuiltinModule('path')
    const assets = process.env.WSL_UI_ASSET_ROOT
    if (!assets || !path.isAbsolute(assets)) throw Error('WSL_UI_ASSET_ROOT must name the absolute built-asset directory')
    const { chromium } = await import(process.env.WSL_PLAYWRIGHT_MODULE || 'playwright')
    const output = await fs.mkdtemp(path.join(process.getBuiltinModule('os').tmpdir(), 'assistant-ownership-ui-'))
    await fs.mkdir(path.join(output, 'output/playwright'), { recursive: true })
    process.chdir(output)
    const browser = await chromium.launch({ headless: true, channel: process.env.WSL_BROWSER_CHANNEL || 'chrome' })
    try {
      const page = await browser.newPage(), errors = []
      page.on('pageerror', error => errors.push(error.message))
      await page.route('**/*', async route => {
        const url = new URL(route.request().url())
        if (url.origin !== 'http://127.0.0.1:5173') return route.abort('blockedbyclient')
        if (url.pathname.startsWith('/api/'))
          return route.fulfill({ status: 500, json: { code: 1, msg: 'unmocked_api' } })
        const file = path.resolve(assets, '.' + (url.pathname === '/' ? '/index.html' : url.pathname))
        if (!file.startsWith(path.resolve(assets) + path.sep)) return route.abort('blockedbyclient')
        try {
          await route.fulfill({ body: await fs.readFile(file), contentType: {
            '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css', '.png': 'image/png', '.svg': 'image/svg+xml'
          }[path.extname(file)] || 'application/octet-stream' })
        } catch {
          await route.fulfill({ status: 404, body: 'missing_asset' })
        }
      })
      await page.goto('http://127.0.0.1:5173')
      try {
        const result = await smoke(page)
        if (errors.length) throw Error(errors.join('; '))
        await fs.writeFile(path.join(output, 'result.json'), JSON.stringify(result, null, 2))
        console.log(JSON.stringify({ check: 'assistant_ownership_ui_smoke.js', ...result, artifacts: output }))
      } catch (error) {
        await page.screenshot({ path: path.join(output, 'failure.png'), fullPage: true })
        console.error('Browser artifacts: ' + output)
        throw error
      }
    } finally { await browser.close() }
  })().catch(error => { console.error(error); process.exitCode = 1 })
}
return smoke
})()
