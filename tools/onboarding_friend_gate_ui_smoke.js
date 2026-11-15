// Synthetic fixtures only; all requests are intercepted, including static assets.
// CLI: WSL_UI_ASSET_ROOT=/absolute/assets WSL_PLAYWRIGHT_MODULE=/path/to/playwright/index.mjs node tools/onboarding_friend_gate_ui_smoke.js
// prettier-ignore
(() => {
const smoke = async page => {
  const base = new URL(page.url()).origin
  if (base !== 'http://127.0.0.1:5173') throw Error('Use the intercepted synthetic UI runner')
  page.setDefaultTimeout(10000)
  const id = n => `f7400000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const check = (condition, message) => { if (!condition) throw Error(message) }
  const copy = value => JSON.parse(JSON.stringify(value))
  const platforms = ['wsl', 'lajiaoliao'], now = new Date().toISOString()
  const platformName = platform => platform === 'wsl' ? '旺商聊' : '辣椒聊'
  const accounts = platforms.flatMap((platform, index) => [0, 1].map(offset => ({
    id: id(10 + index * 2 + offset), platform, workspace: platform, purpose: 'management',
    name: `${platform}-面板${offset + 1}`, native_nickname: `${platformName(platform)}原生昵称${offset + 1}`,
    auth_state: 'authenticated', version: 1
  })))
  accounts.push({ id: id(20), platform: 'wsl', workspace: 'wsl', purpose: 'chat',
    name: '合成聊天用途', native_nickname: '聊天用途昵称', version: 1 })
  accounts.push({ id: id(21), platform: 'wsl', workspace: 'maibot', purpose: 'chat',
    name: '合成热群机器人', version: 1 })
  const key = account => `${account.platform}:${account.id}`
  const gates = new Map(accounts.map(account => [key(account), {
    platform: account.platform, account_id: account.id, state: 'closed', version: 0, evidence_refs: []
  }]))
  let actor = id(1), role = 'superadmin', held
  const reads = [], writes = [], unexpected = [], holds = new Set(), screenshots = []
  const delayNext = (account, method, response) => {
    let started, release, finished
    const pending = { key: key(account), method, response,
      ready: new Promise(resolve => { started = resolve }),
      done: new Promise(resolve => { finished = resolve }),
      wait: new Promise(resolve => { release = resolve }),
      started: () => started(), finish: () => finished(), release: () => release() }
    held = pending; holds.add(pending)
    return pending
  }
  const snapshot = account => ({
    account, sampled_at: now, visibility: 'account',
    groups: { authorized: 0, management_enabled: 0, ai_enabled: 0, ai_pending: 0 },
    operations: { queued: 0, running: 0, unknown: 0, failed_15m: 0 },
    inbox: { pending: 0, unresolved: 0, quarantined: 0 },
    performance: { per_second: 0, inserted_60s: 0, series: [] },
    health: { status: 'unobserved', reasons: [] }, backlog: { status: 'empty', reasons: [] },
    runtime_observed: false
  })
  const handler = async route => {
    const request = route.request(), url = new URL(request.url())
    const path = url.pathname.replace('/api/v1', ''), workspace = request.headers()['x-workspace']
    let data = [], status = 200, pending
    const gateMatch = /^\/system\/group-binding\/friend-gates\/([^/]+)$/.exec(path)
    if (gateMatch) {
      const account = accounts.find(item => item.id === gateMatch[1])
      check(role === 'superadmin' && account?.purpose === 'management' &&
        account.platform === workspace && account.workspace === workspace, 'Gate escaped admin/account/platform scope')
      const value = gates.get(key(account))
      if (request.method() === 'GET') reads.push({ account: account.id, platform: workspace, actor })
      else {
        const body = request.postDataJSON()
        check(request.method() === 'PUT' &&
          Object.keys(body).sort().join() === 'evidence_refs,state,version', 'Unexpected gate mutation fields')
        check(['closed', 'open'].includes(body.state) && Array.isArray(body.evidence_refs) &&
          body.evidence_refs.length <= 16 && (body.state !== 'open' || body.evidence_refs.length),
          'Invalid or unbounded evidence reached the API')
        writes.push({ account: account.id, platform: workspace, actor, ...copy(body) })
        if (body.version !== value.version)
          return route.fulfill({ status: 409, json: { code: 1, msg: 'version_conflict' } })
        gates.set(key(account), { ...value, ...body, version: value.version + 1 })
      }
      data = copy(gates.get(key(account)))
      if (held?.key === key(account) && held.method === request.method()) {
        pending = held; held = undefined; pending.started()
        await pending.wait
        if (pending.response) data = pending.response
      }
    } else if (request.method() !== 'GET') {
      unexpected.push({ method: request.method(), path })
      return route.fulfill({ status: 400, json: { code: 1, msg: 'unexpected_mutation' } })
    } else if (path === '/me') data = {
      user: { id: actor, username: 'synthetic-friend-gate', role,
        ...(role === 'user' ? { tenant_id: id(90), tenant_role: 'owner' } : {}) },
      permissions: ['overview:read', 'accounts:read', 'groups:read',
        ...(role === 'superadmin' ? ['workspaces:maibot'] : [])]
    }
    else if (path === '/workspaces') data = { items: [
      ...platforms.map(platform => ({ id: platform, name: `${platformName(platform)}机器人` })),
      ...(role === 'superadmin' ? [{ id: 'maibot', name: '热群机器人' }] : [])
    ] }
    else if (path === '/accounts') data = accounts.filter(account => account.workspace === workspace)
    else if (path === '/overview') data = { accounts: 2, groups: 0 }
    else if (/^\/accounts\/[^/]+\/dashboard$/.test(path)) {
      const account = accounts.find(item => item.id === path.split('/')[2] && item.workspace === workspace)
      check(account, 'Dashboard escaped its workspace')
      data = snapshot(account)
    } else if (path.endsWith('/events'))
      return route.fulfill({ contentType: 'text/event-stream', body: '' })
    else if (path === '/user-center') data = {
      subscription: { name: 'PRO', active: true, platforms, modules: [], group_limit: 30 },
      groups: [], permissions: ['overview:read', 'groups:read'], personas: [], claims: [], operations: []
    }
    else if (path === '/user-center/groups') data = { items: [], total: 0, page: 1, page_size: 20 }
    else if (path === '/user-center/usage') data = { call_count: 0, actual_micro: 0, held_micro: 0 }
    else if (path !== '/groups') unexpected.push({ method: request.method(), path })
    try { await route.fulfill({ status, json: { code: 0, msg: '', data } }) }
    finally { pending?.finish() }
  }
  const dashboard = () => page.getByTestId('account-dashboard')
  const modal = () => page.getByRole('dialog', { name: '来向好友审批准入', exact: true })
  const saved = () => modal().getByRole('region', { name: '已保存准入', exact: true })
  const evidence = () => modal().getByRole('textbox', { name: '来向好友审批证据引用', exact: true })
  const saveButton = () => modal().getByRole('button', { name: '保存准入', exact: true })
  const ready = async account => {
    await dashboard().getByRole('heading', { name: `${account.native_nickname ? account.native_nickname + ' · ' : ''}${account.name}`, exact: true }).waitFor()
    await dashboard().locator('.account-kpis').waitFor()
  }
  const navigate = async account => {
    await page.evaluate(({ actor, account }) => {
      sessionStorage.setItem(`wsl:console:${actor}`, 'admin')
      sessionStorage.setItem(`wsl:workspace:${actor}`, account.workspace)
      sessionStorage.setItem(`wsl:account:${actor}:${account.workspace}`, account.id)
    }, { actor, account })
    await page.goto(base + '/#overview'); await page.reload()
    await ready(account)
  }
  const choose = async account => {
    await page.getByRole('combobox', { name: '机器人面板', exact: true })
      .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option').filter({ hasText: account?.name || '工作台总览' }).click()
    if (account) await ready(account)
    else await dashboard().waitFor({ state: 'hidden' })
  }
  const open = async account => {
    await dashboard().getByRole('button', { name: '好友接入准入', exact: true }).click()
    await saved().getByText(`版本 ${gates.get(key(account)).version}`, { exact: true }).waitFor()
    check(await modal().getByText(account.native_nickname + ' · ' + account.name, { exact: true }).isVisible(),
      'Native nickname/account label missing')
    await modal().getByRole('img', { name: platformName(account.platform), exact: true }).waitFor()
  }
  const edit = async () => {
    await modal().getByRole('button', { name: '编辑准入', exact: true }).click()
    await evidence().waitFor()
  }
  const selectState = async state => {
    await modal().getByRole('combobox', { name: '来向好友审批准入状态', exact: true })
      .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option', { name: state === 'open' ? '开放' : '关闭', exact: true }).click()
  }
  const close = async () => {
    const before = writes.length, readCount = reads.length
    await modal().getByRole('button', { name: '关闭', exact: true }).click()
    await modal().waitFor({ state: 'hidden' })
    check(writes.length === before && reads.length === readCount, 'Closing issued a gate request')
  }
  const save = async () => {
    await saveButton().click()
    await modal().getByRole('status').getByText('来向好友审批准入已保存', { exact: true }).waitFor()
  }
  const switchActor = async value => {
    actor = value
    await navigate(accounts[0])
  }
  // Let old fixture responses resolve even after the UI aborts them, exercising
  // generation and unmount checks independently of fetch cancellation.
  await page.addInitScript(() => {
    const originalFetch = window.fetch
    window.fetch = (input, options) => originalFetch(input,
      String(input).includes('/system/group-binding/friend-gates/')
        ? { ...options, signal: undefined } : options)
  })
  await page.route('**/api/v1/**', handler)
  try {
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.evaluate(() => sessionStorage.clear())
    for (const platform of platforms) {
      const [account, other] = accounts.filter(item => item.platform === platform && item.purpose === 'management')
      const readCount = reads.length, writeCount = writes.length
      await navigate(account)
      check(reads.length === readCount, 'Selected account eagerly fetched its gate')
      await open(account)
      check(await evidence().count() === 0 && writes.length === writeCount, 'Viewing edited or wrote the gate')
      await saved().getByText('关闭', { exact: true }).waitFor()
      await close()
      await open(account); await edit(); await selectState('open')
      check(await saveButton().isDisabled(), 'Open gate accepted empty evidence')
      await evidence().fill(Array.from({ length: 17 }, (_, n) => `acceptance:TEST_${n}`).join('\n'))
      check(await saveButton().isDisabled(), 'Evidence count was unbounded')
      await evidence().fill('docs/' + 'a'.repeat(260) + '.md')
      check(await saveButton().isDisabled(), 'Evidence length was unbounded')
      await evidence().fill('acceptance:DUPLICATE\nacceptance:DUPLICATE')
      check(await saveButton().isDisabled(), 'Duplicate evidence accepted')
      await evidence().fill('https://invalid.example/evidence')
      check(await saveButton().isDisabled(), 'Arbitrary evidence URL accepted')
      await evidence().fill(`docs/customer-onboarding.md#好友接入\nacceptance:SYNTH_${platform}`)
      await saved().getByText('关闭', { exact: true }).waitFor()
      await save()
      check(writes.at(-1).version === 0 && writes.at(-1).state === 'open' &&
        writes.at(-1).platform === platform && writes.at(-1).account === account.id,
        'Save lost initial version or explicit platform/account')
      await close(); await open(account)
      await saved().getByText('开放', { exact: true }).waitFor()
      await edit(); await selectState('closed'); await evidence().fill(''); await save()
      check(writes.at(-1).version === 1 && writes.at(-1).state === 'closed' &&
        writes.at(-1).evidence_refs.length === 0, 'Closing admission lost its version or required evidence')
      await close(); await open(account); await edit(); await selectState('open')
      const draft = `acceptance:DRAFT_${platform}`
      await evidence().fill(draft); await close()
      await choose(other); await open(other); await edit()
      check(await evidence().inputValue() === '', 'Draft crossed an account')
      await close(); await choose(account); await open(account); await edit()
      check(await evidence().inputValue() === draft, 'Scoped draft was lost on close/account switch')
      gates.get(key(account)).version++
      await saveButton().click()
      await modal().getByText('准入记录已变化，草稿已保留。请重新读取并核对当前版本。', { exact: true }).waitFor()
      check(await saveButton().isDisabled() &&
        await modal().getByRole('button', { name: '使用当前版本保留草稿', exact: true }).isDisabled(),
        'Conflict could be overwritten without a fresh read')
      await close(); await open(account)
      await modal().getByText('准入记录已变化，草稿已保留。请重新读取并核对当前版本。', { exact: true }).waitFor()
      await edit()
      check(await evidence().inputValue() === draft, 'Conflict draft was lost on reopening')
      await modal().getByRole('button', { name: '重新读取来向好友审批准入', exact: true }).click()
      await modal().getByRole('button', { name: '使用当前版本保留草稿', exact: true }).and(page.locator(':enabled')).waitFor()
      await modal().getByRole('button', { name: '使用当前版本保留草稿', exact: true }).click()
      await save()
      check(writes.at(-1).version === 3, 'Conflict save did not use the refreshed version')
      await edit()
      for (const width of [320, 390, 768, 1440]) {
        await page.setViewportSize({ width, height: 780 })
        await modal().evaluate(async element => {
          await Promise.all(element.getAnimations({ subtree: true })
            .filter(animation => animation.effect?.getComputedTiming().iterations !== Infinity)
            .map(animation => animation.finished.catch(() => {})))
        })
        const fits = await modal().evaluate(element => {
          const box = element.getBoundingClientRect()
          const footer = element.querySelector('.el-dialog__footer').getBoundingClientRect()
          return box.left >= 0 && box.right <= innerWidth + 1 && box.top >= 0 &&
            box.bottom <= innerHeight + 1 && footer.bottom <= innerHeight + 1 &&
            element.scrollWidth <= element.clientWidth + 1 &&
            [...element.querySelectorAll('.el-dialog__footer button')].every(button => {
              const rect = button.getBoundingClientRect()
              return rect.left >= box.left && rect.right <= box.right && rect.height > 0
            })
        })
        check(fits, `Dialog/footer overflow at ${platform}/${width}`)
        const path = `output/playwright/onboarding-friend-gate-${platform}-${width}.png`
        await page.screenshot({ path, fullPage: true, animations: 'disabled' }); screenshots.push(path)
      }
      await close(); await open(account)
      check(!await modal().getByText('有未保存草稿', { exact: true }).count(),
        'Unchanged editor was retained as a dirty draft')
      await edit()
      const unchanged = await evidence().inputValue()
      await modal().getByRole('button', { name: '重新读取来向好友审批准入', exact: true }).click()
      await modal().getByRole('button', { name: '重新读取来向好友审批准入', exact: true }).and(page.locator(':not(.is-loading)')).waitFor()
      check(await evidence().inputValue() === unchanged, 'Refreshing an unchanged editor hid the form')
      await close(); await page.setViewportSize({ width: 1440, height: 900 })
    }
    const [account, other] = accounts
    await navigate(account)
    const stale = { ...gates.get(key(account)), version: 999, evidence_refs: ['acceptance:STALE_RESPONSE'] }
    const lateRead = delayNext(account, 'GET', stale)
    await dashboard().getByRole('button', { name: '好友接入准入', exact: true }).click()
    await lateRead.ready; await close(); await choose(other); await open(other); await edit()
    await evidence().fill('acceptance:OTHER_ACCOUNT')
    lateRead.release(); await lateRead.done
    await page.evaluate(() => new Promise(requestAnimationFrame))
    check(await evidence().inputValue() === 'acceptance:OTHER_ACCOUNT' &&
      !await modal().getByText('acceptance:STALE_RESPONSE', { exact: true }).count(),
      'Late GET overwrote another account')
    await close(); await choose(account); await open(account); await edit()
    await evidence().fill('acceptance:HELD_SAVE')
    const lateSave = delayNext(account, 'PUT')
    await saveButton().click(); await lateSave.ready; await close()
    await choose(other); await open(other); await edit()
    lateSave.release(); await lateSave.done
    await page.evaluate(() => new Promise(requestAnimationFrame))
    check(await evidence().inputValue() === 'acceptance:OTHER_ACCOUNT' &&
      !await modal().getByText('来向好友审批准入已保存', { exact: true }).count(),
      'Late PUT cleared another account draft or showed its success')
    await close(); await choose(account)
    const unmounted = delayNext(account, 'GET', stale)
    await dashboard().getByRole('button', { name: '好友接入准入', exact: true }).click()
    await unmounted.ready; await close(); await choose(null)
    unmounted.release(); await unmounted.done
    await choose(account); await open(account); await edit()
    if (await modal().getByRole('button', { name: '使用当前版本保留草稿', exact: true }).count())
      await modal().getByRole('button', { name: '使用当前版本保留草稿', exact: true }).click()
    await evidence().fill('acceptance:ACTOR_ONE'); await close()
    await switchActor(id(2)); await open(account); await edit()
    check(await evidence().inputValue() !== 'acceptance:ACTOR_ONE', 'Draft crossed a website actor')
    await evidence().fill('acceptance:ACTOR_TWO'); await close()
    await switchActor(id(1)); await open(account); await edit()
    check(await evidence().inputValue() === 'acceptance:ACTOR_ONE', 'Original actor draft was lost')
    await close()
    const chiliAccount = accounts.find(item => item.platform === 'lajiaoliao')
    await navigate(chiliAccount); await open(chiliAccount); await edit()
    check(await evidence().inputValue() !== 'acceptance:ACTOR_ONE', 'Draft crossed platforms')
    await evidence().fill('acceptance:CHILI_ONLY'); await close()
    await navigate(account); await open(account); await edit()
    check(await evidence().inputValue() === 'acceptance:ACTOR_ONE', 'Platform round trip lost the draft')
    await close()
    const hiddenReads = reads.length
    for (const hidden of accounts.filter(item => item.purpose === 'chat')) {
      await navigate(hidden)
      check(await dashboard().getByRole('button', { name: '好友接入准入', exact: true }).count() === 0,
        'Chat/maibot purpose exposed admission')
    }
    await navigate(account)
    await page.getByRole('button', { name: '切换到用户中心', exact: true }).click()
    await page.getByRole('heading', { name: '我的群组', exact: true }).waitFor()
    check(await page.getByRole('button', { name: '好友接入准入', exact: true }).count() === 0,
      'User center exposed admission')
    role = 'user'
    await page.reload()
    await page.getByRole('heading', { name: '我的群组', exact: true }).waitFor()
    check(await page.getByRole('button', { name: '好友接入准入', exact: true }).count() === 0 &&
      reads.length === hiddenReads, 'Customer/chat views read or exposed admission')
    check(unexpected.length === 0, 'Unexpected APIs: ' + JSON.stringify(unexpected))
    return { passed: true, synthetic_only: true, platforms, viewports: [320, 390, 768, 1440],
      explicit_workspace: true, view_close_writes: 0, scoped_drafts: true, conflicts_retained: true,
      late_get_isolated: true, late_put_isolated: true, unmount_isolated: true,
      customer_chat_hidden: true, native_calls: 0, writes, screenshots }
  } finally {
    for (const pending of holds) pending.release()
    await page.unroute('**/api/v1/**', handler)
  }
}
if (process.argv[1]?.endsWith('/onboarding_friend_gate_ui_smoke.js')) {
  void (async () => {
    const fs = process.getBuiltinModule('fs/promises'), path = process.getBuiltinModule('path')
    const assets = process.env.WSL_UI_ASSET_ROOT
    if (!assets || !path.isAbsolute(assets)) throw Error('WSL_UI_ASSET_ROOT must be an absolute built-asset directory')
    const { chromium } = await import(process.env.WSL_PLAYWRIGHT_MODULE || 'playwright')
    const output = await fs.mkdtemp(path.join(process.getBuiltinModule('os').tmpdir(), 'onboarding-friend-gate-ui-'))
    await fs.mkdir(path.join(output, 'output/playwright'), { recursive: true })
    process.chdir(output)
    const browser = await chromium.launch({ headless: true, channel: process.env.WSL_BROWSER_CHANNEL || 'chrome' })
    try {
      const page = await browser.newPage({ serviceWorkers: 'block' }), errors = []
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
        } catch { await route.fulfill({ status: 404, body: 'missing_asset' }) }
      })
      await page.goto('http://127.0.0.1:5173')
      try {
        const result = await smoke(page)
        if (errors.length) throw Error(errors.join('; '))
        await fs.writeFile(path.join(output, 'result.json'), JSON.stringify(result, null, 2))
        console.log(JSON.stringify({ check: 'onboarding_friend_gate_ui_smoke.js', ...result, artifacts: output }))
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
