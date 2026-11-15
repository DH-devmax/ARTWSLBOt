// Synthetic APIs only. Run with WSL_UI_NODE_MODULES=/absolute/web/node_modules node tools/friend_delete_ui_smoke.js --browser.
// This expression also returns the check for an external synthetic browser runner.
// prettier-ignore
(() => {
const smoke = async page => {
  page.setDefaultTimeout(10000)
  const clockStart = Date.now()
  await page.clock.install({ time: new Date(clockStart) })
  let clockOffset = 0
  const fixtureNow = () => Date.now() + clockOffset
  const fastForward = async milliseconds => {
    clockOffset += milliseconds
    await page.clock.fastForward(milliseconds)
  }
  const resetClock = async () => {
    clockOffset = 0
    await page.clock.setSystemTime(new Date())
  }
  const base = 'http://127.0.0.1:5173'
  const platforms = ['wsl', 'lajiaoliao'], viewports = [320, 390, 768, 1440]
  const id = n => `f8500000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const label = platform => platform === 'wsl' ? '旺商聊' : '辣椒聊'
  const self = platform => platform === 'wsl' ? '85001' : 'SYNTHETIC_CHILI_SELF_85001'
  const friendID = platform => platform === 'wsl' ? '85002' : 'SYNTHETIC_CHILI_FRIEND_85002'
  const friendName = platform => `${label(platform)}自有配合好友（仍在隔离群内）`
  const check = (value, message) => { if (!value) throw Error(message) }
  const clone = value => JSON.parse(JSON.stringify(value))
  const fields = (value, names) => Object.keys(value).sort().join() === names.sort().join()
  const accounts = platforms.flatMap((platform, index) => [0, 1].map(n => ({
    id: id(10 + index * 2 + n), name: `${label(platform)}合成机器人${n + 1}`,
    native_nickname: `${label(platform)}原生机器人${n + 1}`, platform, workspace: platform,
    purpose: 'management', auth_state: 'authenticated', testing_enabled: true,
    connection_enabled: true, epoch: 1, version: 1
  })))
  const groups = platforms.map((platform, index) => ({
    group_id: id(20 + index), native_id: platform === 'wsl' ? '85003' : 'SYNTHETIC_CHILI_GROUP_85003',
    name: `${label(platform)}好友删除隔离群`, platform,
    primary_account_id: accounts.find(account => account.platform === platform).id,
    management_enabled: false, ai_enabled: false
  }))
  const accountFor = platform => accounts.find(account => account.platform === platform)
  const groupFor = platform => groups.find(group => group.platform === platform)
  let actor = id(1), role = 'superadmin', activePlatform = 'wsl', serial = 100
  let variant = 'normal', outcome = 'succeeded', removed = false, resolveUnknown = false, hold
  const windows = new Map(), operations = [], calls = [], creations = [], unexpected = [], screenshots = []
  const expiryChecks = [], negativeCases = [], heldRequests = new Map()
  const bounded = async (promise, message) => {
    let timer
    try {
      return await Promise.race([promise, new Promise((_, reject) => {
        timer = setTimeout(() => reject(Error(message)), 10000)
      })])
    } finally { clearTimeout(timer) }
  }
  const holdNext = name => {
    check(!hold, 'Overlapping held synthetic requests')
    let start, finish, unblock
    hold = { name, started: new Promise(resolve => { start = resolve }),
      done: new Promise(resolve => { finish = resolve }), released: new Promise(resolve => { unblock = resolve }),
      start: () => start(), finish: () => finish(), release: () => unblock() }
    return hold
  }
  const mutations = () => calls.filter(call => call.name === 'run_social')
  const resetFixture = async (platform, value = 'normal', status = 'succeeded') => {
    activePlatform = platform; variant = value; outcome = status; removed = false; resolveUnknown = false
    operations.length = 0
    const now = fixtureNow()
    const account = accountFor(platform), group = groupFor(platform)
    const window = {
      id: id(++serial), group_id: group.group_id, account_ids: [account.id],
      target_ids: [value === 'self' ? self(platform) : value === 'not_friend' ? 'SYNTHETIC_NOT_A_FRIEND' : friendID(platform)],
      actions: ['friend_delete'], admission_mode: 'native_debug', used: 0, budget: 1,
      cancelled: value === 'cancelled', expires_at: new Date(now + (value === 'expired' ? -1000 : 300000)).toISOString()
    }
    if (value === 'mixed') window.actions.push('group_invite')
    if (value === 'package') window.admission_mode = 'package'
    if (value === 'budget') window.budget = 2
    if (value === 'multiple_targets') window.target_ids.push('SYNTHETIC_SECOND_TARGET')
    if (value === 'multiple_accounts') window.account_ids.push(accounts.find(a => a.platform === platform && a.id !== account.id).id)
    windows.set(platform, [window])
    return window
  }
  const membership = (platform, now) => {
    const account = accountFor(platform), group = groupFor(platform)
    const view = {
      platform, self_id: self(platform), self_role: 'admin', group_id: group.native_id,
      account_epoch: account.epoch, account_version: account.version,
      directory_known: true, members_known: true, friends_known: true, in_group: true,
      member_ids: [self(platform), friendID(platform)], invitations: [],
      sampled_at: new Date(now).toISOString(),
      friends: removed ? [] : [{ id: friendID(platform), name: friendName(platform) }]
    }
    if (variant === 'incomplete') view.friends_known = false
    if (variant === 'duplicate') view.friends.push(clone(view.friends[0]))
    if (variant === 'null_friends') view.friends = null
    if (variant === 'stale') view.sampled_at = new Date(now - 31000).toISOString()
    if (variant === 'wrong_epoch') view.account_epoch++
    if (variant === 'wrong_version') view.account_version++
    if (variant === 'wrong_group') view.group_id = 'SYNTHETIC_OTHER_GROUP'
    if (variant === 'wrong_platform') view.platform = platform === 'wsl' ? 'lajiaoliao' : 'wsl'
    if (variant === 'ordinary_role') view.self_role = 'member'
    if (variant === 'unknown_role') view.self_role = ''
    if (variant === 'absent') view.in_group = false
    if (variant === 'members_unknown') view.members_known = false
    if (variant === 'directory_unknown') view.directory_known = false
    if (variant === 'self_missing') view.member_ids = [friendID(platform)]
    return view
  }
  const respond = async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname.replace('/api/v1', '')
    const platform = req.headers()['x-workspace'] || activePlatform
    let data = [], status = 200
    // Routes from the old document may finish during navigation.
    const now = fixtureNow()
    if (req.method() === 'GET' && path === '/me')
      data = { user: { id: actor, username: 'synthetic-friend-delete', role,
        ...(role === 'user' ? { tenant_id: id(3), tenant_role: 'owner' } : {}) },
        permissions: role === 'superadmin' ? ['overview:read', 'accounts:read', 'groups:read', 'tests:manage'] : [] }
    else if (req.method() === 'GET' && path === '/workspaces')
      data = { items: platforms.map(id => ({ id, name: `${label(id)}机器人` })) }
    else if (req.method() === 'GET' && path === '/accounts') data = accounts.filter(account => account.platform === platform)
    else if (req.method() === 'GET' && path === '/groups') data = groups.filter(group => group.platform === platform)
    else if (req.method() === 'GET' && path === '/overview') data = { accounts: 2, groups: 1, ai_groups: 0, pending_claims: 0 }
    else if (req.method() === 'GET' && path === '/test-windows') data = windows.get(platform) || []
    else if (req.method() === 'GET' && path === '/test-tools/onboarding/scopes') data = { items: [] }
    else if (req.method() === 'POST' && path === '/test-windows') {
      const input = req.postDataJSON(), account = accountFor(platform), group = groupFor(platform)
      check(role === 'superadmin' && fields(input, ['group_id', 'account_ids', 'target_ids', 'actions',
        'admission_mode', 'confirm_isolated', 'seconds', 'budget', 'friend_application_id']) &&
        input.group_id === group.group_id && input.account_ids.length === 1 && input.account_ids[0] === account.id &&
        input.target_ids.length === 1 && input.target_ids[0] === friendID(platform) &&
        input.actions.length === 1 && input.actions[0] === 'friend_delete' && input.admission_mode === 'native_debug' &&
        input.confirm_isolated === true && input.seconds > 0 && input.seconds <= 300 && input.budget === 1 &&
        input.friend_application_id === '',
        'Delete window widened account/target/action/budget')
      creations.push({ platform, input })
      const window = { id: id(++serial), ...input, used: 0, cancelled: false,
        expires_at: new Date(now + input.seconds * 1000).toISOString() }
      windows.set(platform, [...(windows.get(platform) || []), window]); data = window
    } else if (req.method() === 'GET' && path === '/test-tools')
      data = { tools: [{ name: 'read_membership' }, { name: 'run_social' }, { name: 'inspect_operation' }, { name: 'reconcile_operation' }] }
    else if (req.method() === 'GET' && path === '/test-tools/operations')
      data = operations.filter(item => item.actor === actor && item.platform === platform &&
        item.account === url.searchParams.get('account_id')).map(item => item.operation)
    else if (req.method() === 'GET' && /^\/accounts\/[^/]+\/receiver$/.test(path))
      data = { enabled: true, revision: 1, observed: true, snapshot: { state: 'connected' } }
    else if (req.method() === 'GET' && path === '/user-center')
      data = { workspace: { id: id(3), name: '合成用户空间' },
        subscription: { name: 'PRO', active: true, platforms, modules: [], group_limit: 1, member_limit: 1 },
        entitlement: { mode: 'tenant_business', active: true, permissions: [] }, groups: [], permissions: [],
        personas: [], claims: [], operations: [], active_group_count: 0 }
    else if (req.method() === 'GET' && path === '/user-center/groups')
      data = { workspace: { id: id(3), name: '合成用户空间' }, items: [], total: 0, page_size: 20 }
    else if (req.method() === 'GET' && path === '/user-center/usage') data = { call_count: 0, actual_micro: 0, held_micro: 0 }
    else if (req.method() === 'POST' && path === '/test-tools/call') {
      const input = req.postDataJSON(), { name, arguments: args } = input
      const account = accountFor(platform), window = (windows.get(platform) || []).find(w => w.id === args.test_window_id)
      check(role === 'superadmin' && fields(input, ['name', 'arguments']) && args.account_id === account.id,
        'Tool escaped its actor/platform/account')
      calls.push({ name, args: clone(args), platform, actor, key: req.headers()['idempotency-key'] })
      if (name === 'read_membership') {
        check(window && fields(args, ['account_id', 'test_window_id']), 'Membership read lost its exact grant')
        data = { snapshot: membership(platform, now), scope: { remaining: Math.max(0, window.budget - window.used) }, read_only: true }
      } else if (name === 'run_social') {
        check(window && fields(args, ['account_id', 'test_window_id', 'action', 'target_id']) &&
          args.action === 'friend_delete' && args.target_id === friendID(platform) &&
          window.admission_mode === 'native_debug' && window.actions.join() === 'friend_delete' &&
          window.account_ids.length === 1 && window.target_ids.join() === args.target_id &&
          window.budget === 1 && window.used === 0 && !window.cancelled &&
          new Date(window.expires_at).getTime() > now + 3000 && req.headers()['idempotency-key'] &&
          variant === 'normal', 'Delete bypassed confirmation, admission or one-attempt scope')
        window.used++
        const operation = { id: id(++serial), kind: 'friend_delete',
          status: outcome === 'lost' ? 'unknown' : outcome,
          result_code: outcome === 'succeeded' ? 'native_friend_delete_acknowledged' :
            outcome === 'failed' ? (platform === 'wsl' ? 'native_business_rejected' : 'lajiaoliao_request_rejected') : 'native_setting_result_unknown',
          test_window_id: window.id, group_id: groupFor(platform).group_id, created_at: new Date(now).toISOString() }
        operations.unshift({ actor, platform, account: account.id, key: req.headers()['idempotency-key'], operation })
        if (outcome === 'succeeded') removed = true
        data = { operation }
        if (outcome === 'lost') status = 503
      } else if (name === 'inspect_operation' || name === 'reconcile_operation') {
        check(fields(args, ['account_id', ...(args.key ? ['key'] : ['operation_id'])]),
          'Query or reconciliation attempted to replay mutation arguments')
        const original = operations.find(item => item.actor === actor && item.platform === platform &&
          item.account === account.id && (args.key ? item.key === args.key : item.operation.id === args.operation_id))
        check(original, 'Original operation query lost its identity')
        if (name === 'reconcile_operation' && resolveUnknown) {
          original.operation.status = 'succeeded'; original.operation.result_code = 'desired_state_observed'; removed = true
        }
        data = { operation: clone(original.operation), read_only: true }
      } else throw Error('Unexpected native tool: ' + name)
      if (hold?.name === name) {
        const pending = hold; hold = undefined; heldRequests.set(req, pending)
        pending.start(); await pending.released
      }
    } else {
      unexpected.push({ path, method: req.method() })
      return route.fulfill({ status: 500, json: { code: 1, msg: 'unmocked_api' } })
    }
    await route.fulfill({ status, json: status === 503 ? { code: 1, msg: 'native_setting_result_unknown' } : { code: 0, msg: '', data: clone(data) } })
  }
  const handler = async route => {
    try { await respond(route) }
    finally { heldRequests.get(route.request())?.finish(); heldRequests.delete(route.request()) }
  }
  const idle = () => page.waitForFunction(() => !document.querySelector('.test-tools .is-loading'))
  const tool = () => page.locator('.test-tools')
  const run = () => tool().getByRole('button', { name: '确认验证此动作', exact: true })
  const confirmation = () => page.getByRole('dialog', { name: '确认删除好友', exact: true })
  const read = async () => {
    await tool().getByRole('button', { name: /^(读取当前好友与群状态|读取入退群状态)$/ }).click(); await idle()
  }
  const choose = async (name, option, parent = page) => {
    await parent.getByRole('combobox', { name, exact: true })
      .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option', { name: option, exact: true }).click()
    await page.getByRole('listbox').waitFor({ state: 'hidden' })
  }
  const navigate = async (platform, consoleView = 'admin', keepStorage = false) => {
    activePlatform = platform
    await page.evaluate(({ actor, platform, account, consoleView, keepStorage }) => {
      if (!keepStorage) sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`, consoleView)
      sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
      sessionStorage.setItem(`wsl:account:${actor}:${platform}`, account)
    }, { actor, platform, account: accountFor(platform).id, consoleView, keepStorage })
    await page.goto(`${base}/?friend-delete-fixture=${++serial}#${consoleView === 'admin' ? 'tests' : 'overview'}`)
    if (consoleView === 'admin' && role === 'superadmin') {
      await tool().waitFor(); await tool().locator('.tools-grant').first().waitFor(); await idle()
    } else await page.locator('.user-center').waitFor()
  }
  const selectWindow = async () => {
    await tool().locator('.tools-grant').first().click()
    await run().waitFor()
  }
  const selectFriend = async platform => {
    await choose('授权目标好友', `${friendName(platform)} · ${friendID(platform)}`, tool())
  }
  const ready = async platform => {
    await selectWindow(); await read(); await selectFriend(platform)
    await run().and(page.locator(':enabled')).waitFor()
  }
  const fit = async (platform, width, stage, modal, footerSelector = '.el-dialog__footer, .el-message-box__btns') => {
    await page.setViewportSize({ width, height: 820 })
    const subject = modal || tool(), handle = await subject.evaluateHandle(element => ({ element, previous: '', frames: 0 }))
    try {
      await page.waitForFunction(state => {
        const { element } = state
        for (let current = element; current; current = current.parentElement) {
          const style = getComputedStyle(current)
          if (!element.isConnected || style.display === 'none' || style.visibility !== 'visible' ||
            Number(style.opacity) === 0 || [...current.classList].some(name => /-(?:enter|leave)-(?:from|active|to)$/.test(name)) ||
            current.getAnimations().some(animation => animation.pending || !['finished', 'idle'].includes(animation.playState))) {
            state.frames = 0; return false
          }
        }
        const box = element.getBoundingClientRect()
        const value = JSON.stringify([box.x, box.y, box.width, box.height, element.scrollWidth, innerWidth])
        state.frames = value === state.previous ? state.frames + 1 : 0; state.previous = value
        return state.frames >= 2
      }, handle, { polling: 'raf' })
    } finally { await handle.dispose() }
    const overflow = await page.evaluate(() => ({
      width: innerWidth, scroll: document.documentElement.scrollWidth,
      elements: [...document.querySelectorAll('body *')].filter(element => {
        const box = element.getBoundingClientRect(), style = getComputedStyle(element)
        return box.width > 0 && box.right > innerWidth + 1 && style.display !== 'none'
      }).slice(0, 8).map(element => ({ tag: element.tagName, class: element.className,
        width: element.getBoundingClientRect().width, right: element.getBoundingClientRect().right }))
    }))
    check(await subject.evaluate(element => element.scrollWidth <= element.clientWidth + 1) &&
      overflow.scroll <= width + 1, `Friend delete overflow ${platform}/${stage}/${width}: ${JSON.stringify(overflow)}`)
    if (modal) {
      const box = await modal.boundingBox()
      const footer = footerSelector ? await modal.locator(footerSelector).boundingBox() : null
      check(box && box.x >= -1 && box.x + box.width <= width + 1 && box.y >= -1 && box.y + box.height <= 821 &&
        (!footerSelector || footer && footer.y >= 0 && footer.y + footer.height <= 821),
        `Friend delete dialog/actions outside viewport ${platform}/${stage}/${width}`)
    }
    const path = `output/playwright/friend-delete-${platform}-${stage}-${width}.png`
    await page.screenshot({ path, fullPage: !modal, animations: 'disabled' }); screenshots.push(path)
  }
  await page.route('**/api/v1/**', handler)
  try {
    for (const platform of platforms) {
      console.log(JSON.stringify({ diagnostic: 'friend_delete_platform', platform, phase: 'confirmation-and-budget' }))
      await resetFixture(platform); await navigate(platform)
      const viewed = calls.length, before = mutations().length
      await selectWindow()
      check(calls.length === viewed && await run().isDisabled(), 'Viewing a delete window read or mutated native state')
      await read(); await selectFriend(platform); await run().and(page.locator(':enabled')).waitFor()
      check(mutations().length === before, 'Membership read deleted a friend')
      for (const width of viewports) {
        await fit(platform, width, 'ready')
        await tool().getByRole('combobox', { name: '授权目标好友', exact: true })
          .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
        const options = page.locator('.el-popper.friend-delete-options:visible')
        await fit(platform, width, 'friends', options, '')
        check(await options.getByRole('option').evaluate(element => element.scrollWidth <= element.clientWidth + 1),
          'Friend nickname/ID overflowed its option')
        await page.keyboard.press('Escape'); await page.getByRole('listbox').waitFor({ state: 'hidden' })
      }
      await run().click()
      const content = await confirmation().innerText()
      check(content.includes(accountFor(platform).native_nickname) && content.includes(groupFor(platform).name) &&
        content.includes(friendName(platform)) && content.includes(friendID(platform)) &&
        content.includes('删除好友关系，不退群、不拉黑'), 'Delete confirmation omitted account/group/target/effect')
      for (const width of viewports) await fit(platform, width, 'confirm', confirmation())
      await confirmation().getByRole('button', { name: '取消', exact: true }).click()
      await confirmation().waitFor({ state: 'hidden' })
      check(mutations().length === before, 'Cancelling deleted a friend')
      await run().click()
      await confirmation().getByRole('button', { name: '确认提交', exact: true }).click()
      await tool().getByText('只读核对：已观察到期望状态。', { exact: true }).waitFor(); await idle()
      check(mutations().length === before + 1 && removed && await run().isDisabled(),
        'Success deleted twice or ignored one-attempt budget')
      check(membership(platform, fixtureNow()).member_ids.includes(friendID(platform)),
        'Friend deletion removed the group member')
      await navigate(platform, 'admin', true); await selectWindow()
      check(await run().isDisabled() && mutations().length === before + 1, 'Reload replayed a successful delete')

      await resetFixture(platform); await navigate(platform)
      console.log(JSON.stringify({ diagnostic: 'friend_delete_platform', platform, phase: 'window-creation' }))
      await tool().getByRole('button', { name: '创建授权窗口', exact: true }).click()
      const editor = page.getByRole('dialog', { name: '创建授权窗口', exact: true })
      await editor.locator('label.el-checkbox').filter({ hasText: '删除好友（独立授权，一次尝试）' }).click()
      check(!await editor.getByRole('checkbox', { name: '成员禁言', exact: true }).count(),
        'Delete was mixed with other actions')
      await choose('业务操作群组', groupFor(platform).name, editor)
      const creationBefore = creations.length
      await editor.getByRole('button', { name: '保存', exact: true }).click()
      await editor.getByText('删除好友需独立原生调试窗口、单个机器人、单个目标和一次预算', { exact: true }).waitFor()
      check(creations.length === creationBefore, 'Empty target created a delete grant')
      await editor.getByRole('textbox', { name: '删除目标好友 UID', exact: true }).fill(friendID(platform))
      await editor.locator('label.el-checkbox').filter({ hasText: '确认所选群为隔离测试群' }).click()
      for (const width of viewports) await fit(platform, width, 'create', editor)
      await editor.getByRole('button', { name: '保存', exact: true }).click()
      await editor.waitFor({ state: 'hidden' })
      check(creations.length === creationBefore + 1 && mutations().length === before + 1,
        'Grant creation executed a deletion or replayed creation')
      await tool().getByRole('button', { name: '创建授权窗口', exact: true }).click()
      await choose('授权用途', '生产套餐校验', editor)
      check(!await editor.getByRole('checkbox', { name: '删除好友（独立授权，一次尝试）', exact: true }).count(),
        'Package grant exposed friend deletion')
      await editor.getByRole('button', { name: '取消', exact: true }).click()

      for (const value of ['incomplete', 'duplicate', 'null_friends', 'stale', 'wrong_epoch', 'wrong_version',
        'wrong_group', 'wrong_platform', 'ordinary_role', 'unknown_role', 'absent', 'members_unknown',
        'directory_unknown', 'self_missing', 'self', 'not_friend', 'mixed', 'package', 'budget',
        'multiple_targets', 'multiple_accounts', 'expired', 'cancelled']) {
        await resetFixture(platform, value); await navigate(platform)
        const count = mutations().length
        await selectWindow()
        if (!['mixed', 'package', 'budget', 'multiple_targets', 'multiple_accounts', 'expired', 'cancelled'].includes(value)) {
          await read()
          const targetID = windows.get(platform)[0].target_ids[0]
          await tool().getByRole('combobox', { name: '授权目标好友', exact: true })
            .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
          const option = page.getByRole('option').filter({ hasText: targetID })
          const disabled = ['incomplete', 'duplicate', 'null_friends', 'self', 'not_friend'].includes(value)
          check(await option.evaluate(element => element.classList.contains('is-disabled')) === disabled,
            'Unsafe target eligibility ' + value)
          if (!disabled) await option.click()
          else await page.keyboard.press('Escape')
        }
        check(await run().isDisabled() && mutations().length === count, 'Unsafe deletion became actionable: ' + value)
        negativeCases.push({ platform, case: value, blocked: true })
      }
      console.log(JSON.stringify({ diagnostic: 'friend_delete_platform', platform, phase: 'negative-cases-complete' }))

      const expiring = await resetFixture(platform); await navigate(platform); await ready(platform)
      expiring.expires_at = new Date(fixtureNow() + 20000).toISOString()
      // Refresh the saved grant before opening confirmation; no membership timer may fetch evidence.
      await navigate(platform); await ready(platform)
      const expiryBefore = mutations().length, callsBefore = calls.length
      await run().click(); await confirmation().waitFor(); await fastForward(21000)
      await confirmation().waitFor({ state: 'hidden' })
      check(await run().isDisabled() && mutations().length === expiryBefore && calls.length === callsBefore,
        'Expiry submitted a delete or automatically refreshed native evidence')
      expiryChecks.push({ platform, writes: 0, native_reads: 0, confirmation_closed: true })

      await resetClock()
      for (const status of ['unknown', 'lost', 'failed']) {
        await resetFixture(platform, 'normal', status); await navigate(platform); await ready(platform)
        const count = mutations().length
        await run().click(); await confirmation().getByRole('button', { name: '确认提交', exact: true }).click()
        await page.waitForFunction(() => !!document.querySelector('.tools-result')); await idle()
        check(mutations().length === count + 1 && await run().isDisabled(), 'Unknown/failed submission remained replayable')
        const operation = operations[0].operation
        await tool().getByRole('button', { name: '查询原操作', exact: true }).click(); await idle()
        check(calls.at(-1).name === 'inspect_operation', 'Query called a mutation')
        await navigate(platform, 'admin', true); await selectWindow()
        check(await run().isDisabled() && mutations().length === count + 1, 'Reload retried an unresolved/failed deletion')
        await tool().getByRole('button', { name: '查询此操作', exact: true }).click(); await idle()
        if (status !== 'failed') {
          await fastForward(301000)
          await tool().getByRole('button', { name: '只读核对结果', exact: true }).click(); await idle()
          check(calls.at(-1).name === 'reconcile_operation' && calls.at(-1).args.operation_id === operation.id &&
            mutations().length === count + 1, 'Expired unknown was replayed instead of reconciling original')
          resolveUnknown = true
          await tool().getByRole('button', { name: '只读核对结果', exact: true }).click(); await idle()
          await tool().getByText('只读核对已观察到期望状态，未重复提交', { exact: true }).waitFor()
        }
        check(mutations().length === count + 1 && await run().isDisabled(), 'Reconciliation restored mutation budget')
        await resetClock()
      }

      await resetFixture(platform); await navigate(platform); await selectWindow()
      const lateRead = holdNext('read_membership')
      await tool().getByRole('button', { name: '读取当前好友与群状态', exact: true }).click()
      await bounded(lateRead.started, 'Held membership read did not start')
      const other = platform === 'wsl' ? 'lajiaoliao' : 'wsl'
      actor = id(2); await resetFixture(other); await navigate(other); await selectWindow()
      lateRead.release(); await bounded(lateRead.done, 'Held membership response did not settle')
      check(!await tool().getByText('当前角色：管理员', { exact: true }).count() &&
        !await tool().getByText(friendName(platform), { exact: true }).count() && await run().isDisabled(),
        'Late membership crossed actor/platform')
      actor = id(1); await resetFixture(platform); await navigate(platform); await ready(platform)
      const lateSubmit = holdNext('run_social'), count = mutations().length
      await run().click(); await confirmation().getByRole('button', { name: '确认提交', exact: true }).click()
      await bounded(lateSubmit.started, 'Held deletion did not start')
      actor = id(2); await resetFixture(other); await navigate(other); await selectWindow()
      lateSubmit.release(); await bounded(lateSubmit.done, 'Held deletion response did not settle')
      check(mutations().length === count + 1 && !await tool().locator('.tools-result').count() && await run().isDisabled(),
        'Late deletion crossed actor/platform or replayed itself')
      actor = id(1)
    }
    role = 'user'
    for (const platform of platforms) {
      await navigate(platform, 'user')
      check(!await tool().count() && !await page.getByRole('button', { name: '确认验证此动作', exact: true }).count() &&
        !await page.getByRole('checkbox', { name: '删除好友（独立授权，一次尝试）', exact: true }).count(),
        'Tenant page exposed friend deletion')
    }
    check(!unexpected.length, 'Unexpected API: ' + JSON.stringify(unexpected))
    check(calls.every(call => ['read_membership', 'run_social', 'inspect_operation', 'reconcile_operation'].includes(call.name)),
      'Friend deletion called unrelated tools')
    return { passed: true, synthetic_only: true, platforms, viewports,
      independent_native_debug_window: true, single_account_target_budget: true,
      explicit_confirmation: true, target_in_group_allowed: true, self_and_nonfriend_blocked: true,
      complete_current_projection_required: true, unknown_inspect_and_reconcile_only: true,
      expiry_checks: expiryChecks, late_actor_platform_guards: true, negative_cases: negativeCases,
      tenant_entry_absent: true, bounded_ui: true, synthetic_deletes: mutations().length,
      synthetic_grants: creations.length, native_calls: 0, model_calls: 0, screenshots }
  } catch (error) {
    console.error(JSON.stringify({ diagnostic: 'friend_delete_fixture_failure', unexpected, last_calls: calls.slice(-3) }))
    throw error
  } finally {
    hold?.release()
    for (const pending of heldRequests.values()) pending.release()
    await page.unroute('**/api/v1/**', handler)
  }
}

const browser = async () => {
  const fs = process.getBuiltinModule('fs/promises'), path = process.getBuiltinModule('path')
  const { createRequire } = process.getBuiltinModule('module'), { pathToFileURL } = process.getBuiltinModule('url')
  const root = path.resolve(path.dirname(process.argv[1]), '..'), web = path.join(root, 'web')
  const deps = process.env.WSL_UI_NODE_MODULES || path.join(web, 'node_modules')
  if (!path.isAbsolute(deps)) throw Error('WSL_UI_NODE_MODULES must be absolute')
  const require = createRequire(path.join(deps, 'package.json'))
  const { chromium } = await import(process.env.WSL_PLAYWRIGHT_MODULE || pathToFileURL(require.resolve('playwright')).href)
  const files = new Map()
  const assets = process.env.WSL_UI_ASSET_ROOT
  if (!assets) {
    const { build } = await import(pathToFileURL(require.resolve('vite')).href)
    const { default: vue } = await import(pathToFileURL(require.resolve('@vitejs/plugin-vue')).href)
    const manifest = JSON.parse(await fs.readFile(path.join(web, 'package.json'), 'utf8'))
    const aliases = Object.keys(manifest.dependencies).map(name => ({
      find: new RegExp('^' + name.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + '(?=/|$)'),
      replacement: path.join(deps, name)
    }))
    const built = await build({ configFile: false, root: web, publicDir: path.join(web, 'public'),
      plugins: [vue()], resolve: { alias: [{ find: '@', replacement: path.join(web, 'src') }, ...aliases] },
      define: { __APP_VERSION__: JSON.stringify('synthetic-friend-delete') },
      build: { write: false, copyPublicDir: false, emptyOutDir: false, minify: true }, logLevel: 'warn' })
    for (const result of Array.isArray(built) ? built : [built])
      for (const item of result.output) files.set('/' + item.fileName, item.type === 'asset' ? item.source : item.code)
    console.log(JSON.stringify({ diagnostic: 'friend_delete_memory_build', passed: true, files: files.size }))
  }
  process.chdir(root)
  await fs.mkdir(path.join(root, 'output/playwright'), { recursive: true })
  const browser = await chromium.launch({ headless: true, channel: process.env.WSL_BROWSER_CHANNEL || 'chrome',
    ...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH } : {}) })
  const page = await browser.newPage(), errors = [], external = []
  page.on('pageerror', error => errors.push(error.stack || error.message))
  await page.route('**/*', async route => {
    const url = new URL(route.request().url())
    if (url.origin !== 'http://127.0.0.1:5173') {
      external.push(url.origin); return route.abort('blockedbyclient')
    }
    if (url.pathname.startsWith('/api/'))
      return route.fulfill({ status: 500, json: { code: 1, msg: 'unmocked_api' } })
    const file = url.pathname === '/' ? '/index.html' : url.pathname
    let body = files.get(file)
    if (body === undefined) {
      const directory = path.resolve(assets || path.join(web, 'public')), target = path.resolve(directory, '.' + file)
      if (!target.startsWith(directory + path.sep)) return route.abort('blockedbyclient')
      try { body = await fs.readFile(target) } catch { return route.fulfill({ status: 404, body: 'missing_asset' }) }
    }
    await route.fulfill({ body, contentType: { '.html': 'text/html', '.js': 'text/javascript', '.css': 'text/css',
      '.png': 'image/png', '.svg': 'image/svg+xml', '.woff2': 'font/woff2' }[path.extname(file)] || 'application/octet-stream' })
  })
  const started = Date.now()
  try {
    await page.goto('http://127.0.0.1:5173')
    const result = await smoke(page)
    if (errors.length || external.length) throw Error('Browser errors/external requests: ' + JSON.stringify({ errors, external }))
    const evidence = { check: 'friend_delete_ui_smoke.js', seconds: (Date.now() - started) / 1000, ...result }
    await fs.writeFile('output/playwright/friend-delete-results.json', JSON.stringify(evidence, null, 2))
    console.log(JSON.stringify(evidence))
  } catch (error) {
    await page.screenshot({ path: 'output/playwright/friend_delete_ui_smoke.js-failure.png', fullPage: true })
    await fs.writeFile('output/playwright/friend_delete_ui_smoke.js-failure.txt', await page.locator('body').innerText())
    if (errors.length) console.error(JSON.stringify({ browser_errors: errors }))
    throw error
  } finally { await browser.close() }
}
if (process.argv[1]?.endsWith('/friend_delete_ui_smoke.js') && process.argv.includes('--browser'))
  browser().catch(error => { console.error(error.stack || error); process.exitCode = 1 })
if (process.argv[1]?.endsWith('/friend_delete_ui_smoke.js') && process.argv.includes('--typecheck')) {
  const fs = process.getBuiltinModule('fs'), path = process.getBuiltinModule('path')
  const { createRequire } = process.getBuiltinModule('module')
  const web = path.resolve(path.dirname(process.argv[1]), '../web')
  const deps = process.env.WSL_UI_NODE_MODULES || path.join(web, 'node_modules')
  const require = createRequire(path.join(deps, 'package.json')), read = fs.readFileSync
  const configPath = path.join(web, 'tsconfig.json')
  const config = JSON.parse(read(configPath, 'utf8'))
  config.compilerOptions.paths = Object.fromEntries(Object.entries(config.compilerOptions.paths)
    .map(([name, values]) => [name, values.map(value => path.resolve(web, value))]))
  config.compilerOptions.paths['*'] = [path.join(deps, '*')]
  config.compilerOptions.typeRoots = [path.join(deps, '@types'), deps]
  // Resolve installed dependencies without adding node_modules or generated declarations to the candidate.
  fs.readFileSync = function (file, ...options) {
    if (String(file) === configPath) return JSON.stringify(config)
    return read.call(this, file, ...options)
  }
  process.argv = [process.argv[0], require.resolve('vue-tsc/bin/vue-tsc.js'), '--noEmit', '-p', path.join(web, 'tsconfig.wsl.json')]
  process.once('exit', code => {
    console.log(JSON.stringify({ check: 'friend_delete_ui_smoke.js --typecheck', passed: code === 0 }))
  })
  try {
    require('vue-tsc').run()
  } finally { fs.readFileSync = read }
}
return smoke
})()
