// Synthetic fixtures only. Register with run_multi_platform_ui_smoke.mjs.
// API interception is a test guardrail; persisted authorization is tested in Go.
// prettier-ignore
async (page, me) => {
  const base = new URL(page.url()).origin
  if (base !== 'http://127.0.0.1:5173' || me?.user?.role !== 'superadmin')
    throw Error('Use the intercepted synthetic UI runner and its superadmin fixture')
  page.setDefaultTimeout(10000)
  const check = (value, message) => { if (!value) throw Error(message) }
  const id = n => `e8400000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const actor = me.user.id, platforms = ['wsl', 'lajiaoliao']
  const widths = [320, 390, 768, 1440], screenshots = [], unexpected = []
  const accounts = [], groups = [], windows = [], challenges = new Map()
  const calls = new Map(platforms.map(platform => [platform, {
    windows: 0, searches: 0, sends: 0, reads: 0, cancels: 0, reconciles: 0
  }]))
  for (const [index, platform] of platforms.entries()) {
    const account = {
      id: id(10 + index), platform, workspace: platform, purpose: 'management',
      name: `${platform} synthetic robot`, native_nickname: '合成测试账号',
      epoch: 1, version: 1, testing_enabled: true, connection_enabled: true,
      auth_state: 'authenticated'
    }
    const group = {
      group_id: id(20 + index), platform, native_id: `synthetic-group-${platform}`,
      name: `${platform} 合成隔离群`, primary_account_id: account.id, tenant_id: id(2),
      active: true, isolated_test: true, management_enabled: false, ai_enabled: false
    }
    accounts.push(account); groups.push(group)
    challenges.set(id(30 + index), {
      id: id(30 + index), platform, mode: 'operator_demo', friend_flow: 'incoming_contact',
      scope: { actor_id: actor, platform, tenant_id: id(2) },
      customer: { name: '合成测试租户' }, robot: { id: account.id, name: account.name },
      state: 'waiting', version: 1, identity_proven: false,
      expires_at: new Date(Date.now() + 900000).toISOString()
    })
  }
  const fields = (body, expected) =>
    Object.keys(body).sort().join() === [...expected].sort().join()
  const handler = async route => {
    const request = route.request(), method = request.method()
    const path = new URL(request.url()).pathname.replace('/api/v1', '')
    const platform = request.headers()['x-workspace'] || 'wsl'
    const account = accounts.find(value => value.platform === platform)
    const group = groups.find(value => value.platform === platform)
    const count = calls.get(platform)
    let data = []
    try {
      check(account && group && count, 'Unknown synthetic platform')
      if (method === 'GET') {
        if (path === '/me') data = {
          ...me, user: { ...me.user, username: 'synthetic-controlled-admin' },
          permissions: [...new Set([...me.permissions, 'tests:manage', 'operations:read'])]
        }
        else if (path === '/workspaces')
          data = { items: platforms.map(id => ({ id, name: id })) }
        else if (path === '/accounts') data = [account]
        else if (path === '/groups') data = [group]
        else if (path === '/test-windows')
          data = windows.filter(window => window.account_ids.includes(account.id))
        else if (path === '/overview') data = { accounts: 1, groups: 1 }
        else if (/^\/accounts\/[^/]+\/receiver$/.test(path)) {
          check(path.split('/')[2] === account.id, 'Receiver escaped its account')
          data = { enabled: true, observed: true, revision: 1, snapshot: { state: 'connected' } }
        } else if (/^\/group-binding\/challenges\/[^/]+$/.test(path)) {
          data = challenges.get(path.split('/')[3])
          check(data?.platform === platform && data.scope.actor_id === actor,
            'Read escaped its challenge, actor or platform')
          count.reads++
        } else if (path.endsWith('/events'))
          return route.fulfill({ contentType: 'text/event-stream', body: '' })
      } else {
        check(method === 'POST', `Unexpected mutation method: ${method}`)
        const body = request.postDataJSON(), key = request.headers()['idempotency-key']
        if (path === '/test-tools/onboarding-friend/windows') {
          const challenge = challenges.get(body.challenge_id)
          check(fields(body, ['challenge_id', 'version', 'group_id', 'account', 'target_id',
            'seconds', 'budget', 'confirm_isolated']), 'Unexpected controlled grant fields')
          check(challenge?.platform === platform && challenge.scope.actor_id === actor &&
            challenge.robot.id === account.id && body.version === challenge.version &&
            body.group_id === group.group_id && body.account === `synthetic-friend-${platform}` &&
            body.target_id === `synthetic-target-${platform}` && body.seconds === 300 &&
            body.budget === 1 && body.confirm_isolated === true && count.windows === 0,
            'Grant lost its explicit scope, deadline, budget or isolated confirmation')
          const window = {
            id: id(100 + platforms.indexOf(platform)), actor_id: actor, group_id: group.group_id,
            account_ids: [account.id], account_epochs: { [account.id]: 1 },
            admission_mode: 'native_debug', cancelled: false, used: 0, budget: 1,
            actions: ['onboarding_friend_search', 'onboarding_friend_request'],
            target_ids: [`onboarding_challenge:${challenge.id}:${challenge.version}`,
              `onboarding_account:${body.account}`, `onboarding_target:${body.target_id}`,
              'onboarding_binding:1'],
            expires_at: new Date(Date.now() + 300000).toISOString()
          }
          windows.push(window); count.windows++
          data = { id: window.id, group_id: window.group_id, admission_mode: window.admission_mode,
            expires_at: window.expires_at, remaining: 1, account_epoch: 1, binding_epoch: 1 }
        } else if (/^\/test-tools\/onboarding-friend\/challenges\/[^/]+\/friend-(target|send)$/.test(path)) {
          const challenge = challenges.get(path.split('/')[4])
          const window = windows.find(value => value.id === body.test_window_id)
          const search = path.endsWith('/friend-target')
          check(fields(body, ['test_window_id', 'version', ...(search ? ['account'] : [])]) &&
            key && challenge?.platform === platform && challenge.scope.actor_id === actor &&
            window?.actor_id === actor && window.account_ids[0] === account.id &&
            window.group_id === group.group_id && !window.cancelled && window.used < window.budget &&
            Date.parse(window.expires_at) > Date.now() && body.version === challenge.version,
            'Controlled action lost its persisted scope, version, deadline, budget or key')
          if (search) {
            check(count.searches === 0 && !challenge.friend_outbound &&
              window.target_ids[1] === `onboarding_account:${body.account}`, 'Search changed or replayed its target')
            count.searches++
            challenge.friend_outbound = {
              id: id(200 + platforms.indexOf(platform)), status: 'prepared', version: challenge.version,
              can_send: true, can_reconcile: false,
              target: { add_account: body.account, native_id: `synthetic-target-${platform}`, name: '合成目标' }
            }
          } else {
            check(count.searches === 1 && count.sends === 0 &&
              challenge.friend_outbound.status === 'prepared', 'Repeated or unprepared mutation')
            count.sends++; window.used++; window.sendKey = key
            Object.assign(challenge.friend_outbound, { status: 'unknown', can_send: false, can_reconcile: true })
            // The mock accepted the attempt before losing the response.
            return route.abort('failed')
          }
          data = challenge
        } else if (/^\/test-windows\/[^/]+\/cancel$/.test(path)) {
          const window = windows.find(value => value.id === path.split('/')[2])
          check(window?.actor_id === actor && window.account_ids[0] === account.id &&
            !window.cancelled, 'Cancellation escaped its window')
          window.cancelled = true; count.cancels++
          data = { cancelled: true }
        } else if (/^\/group-binding\/challenges\/[^/]+\/outbound-attempts\/[^/]+\/reconcile$/.test(path)) {
          const challenge = challenges.get(path.split('/')[3])
          check(fields(body, ['version']) && key && challenge?.platform === platform &&
            challenge.friend_outbound?.id === path.split('/')[5] &&
            challenge.friend_outbound.status === 'unknown' && body.version === challenge.version,
            'Reconciliation changed the original operation')
          count.reconciles++
          Object.assign(challenge.friend_outbound, { status: 'friends', can_reconcile: false })
          data = challenge
        } else throw Error(`Unexpected mutation: ${path}`)
      }
      return route.fulfill({ json: { code: 0, msg: '', data } })
    } catch (error) {
      unexpected.push(String(error.message || error))
      return route.fulfill({ status: 500, json: { code: 1, msg: 'synthetic_contract_failed' } })
    }
  }
  const modal = () => page.getByRole('dialog', { name: '受控搜索加好友', exact: true })
  const send = () => modal().getByRole('button', { name: '确认发送好友申请', exact: true })
  const confirmation = () => page.getByRole('dialog', { name: '确认受控好友申请', exact: true })
  const original = () => modal().locator('[aria-label="受控好友原操作"]')
  const enabled = button => button.and(page.locator(':enabled')).waitFor()
  const ready = () => page.getByRole('button', { name: '受控搜索加好友', exact: true }).waitFor()
  const layout = async (platform, state, width) => {
    await page.setViewportSize({ width, height: 844 })
    await modal().evaluate(async element => {
      await Promise.all(element.getAnimations({ subtree: true })
        .filter(animation => animation.effect?.getComputedTiming().iterations !== Infinity)
        .map(animation => animation.finished.catch(() => {})))
    })
    check(await modal().evaluate(element => {
      const box = element.getBoundingClientRect()
      const footer = element.querySelector('.el-dialog__footer').getBoundingClientRect()
      return box.left >= 0 && box.right <= innerWidth + 1 && box.top >= 0 &&
        box.bottom <= innerHeight + 1 && footer.bottom <= innerHeight + 1 &&
        element.scrollWidth <= element.clientWidth + 1 &&
        [...element.querySelectorAll('.el-dialog__footer button')].every(button => {
          const rect = button.getBoundingClientRect()
          return rect.left >= box.left && rect.right <= box.right &&
            rect.top >= box.top && rect.bottom <= innerHeight + 1 && rect.height > 0
        })
    }), `${platform}/${state}/${width}: modal or footer overflowed`)
    const path = `output/playwright/onboarding-controlled-${platform}-${state}-${width}.png`
    await page.screenshot({ path, fullPage: true }); screenshots.push(path)
  }
  await page.route('**/api/**', handler)
  try {
    await page.evaluate(() => sessionStorage.clear())
    for (const [index, platform] of platforms.entries()) {
      const account = accounts[index], group = groups[index], challenge = challenges.get(id(30 + index))
      const count = calls.get(platform)
      await page.setViewportSize({ width: 1440, height: 844 })
      await page.evaluate(({ actor, platform, account }) => {
        sessionStorage.setItem(`wsl:console:${actor}`, 'admin')
        sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
        sessionStorage.setItem(`wsl:account:${actor}:${platform}`, account)
      }, { actor, platform, account: account.id })
      await page.goto(base + '/#tests'); await page.reload(); await ready()
      await page.getByRole('button', { name: '受控搜索加好友', exact: true }).click()
      await modal().waitFor()
      check(Object.values(count).every(value => value === 0), 'Opening performed a controlled request')
      check(await send().isDisabled(), 'Opening enabled a mutation')
      await modal().getByRole('button', { name: '关闭', exact: true }).click()
      await modal().waitFor({ state: 'hidden' })
      check(Object.values(count).every(value => value === 0), 'Closing performed a controlled request')
      await page.getByRole('button', { name: '受控搜索加好友', exact: true }).click()
      await modal().getByRole('combobox', { name: '受控好友隔离群组', exact: true })
        .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
      await page.getByRole('option', { name: group.name, exact: true }).click()
      await modal().getByRole('textbox', { name: '受控好友接入记录', exact: true }).fill(challenge.id)
      await modal().getByRole('button', { name: '读取原记录', exact: true }).click()
      const create = modal().getByRole('button', { name: '创建本次授权', exact: true })
      await create.waitFor()
      check(await create.isDisabled(), 'Empty target or isolation confirmation enabled a grant')
      await modal().getByRole('textbox', { name: '受控好友搜索账号', exact: true })
        .fill(`synthetic-friend-${platform}`)
      await modal().getByRole('textbox', { name: '受控好友目标原生ID', exact: true })
        .fill(`synthetic-target-${platform}`)
      check(await create.isDisabled(), 'Grant omitted explicit isolated confirmation')
      await modal().getByText('确认此群为自有隔离测试群', { exact: true }).click()
      await enabled(create); await create.click()
      const search = modal().getByRole('button', { name: '搜索授权目标', exact: true })
      await enabled(search)
      check(count.windows === 1 && count.searches === 0 && count.sends === 0,
        'Creating a grant executed an outgoing action')
      check(await send().isDisabled(), 'Unsearched grant enabled send')
      await search.click(); await original().getByText(/目标已确认/).waitFor(); await enabled(send())
      check(count.searches === 1 && count.sends === 0 && windows[index].used === 0,
        'Search sent a request or consumed mutation budget')
      check(await search.isDisabled(), 'Prepared target enabled a second search')
      for (const width of widths) await layout(platform, 'prepared', width)
      await send().click(); await confirmation().waitFor()
      await confirmation().getByText(new RegExp(`synthetic-friend-${platform}`)).waitFor()
      check(count.sends === 0, 'Opening confirmation sent the request')
      await confirmation().getByRole('button', { name: '取消', exact: true }).click()
      await confirmation().waitFor({ state: 'hidden' }); await enabled(send())
      check(count.sends === 0, 'Cancelling confirmation sent the request')
      await page.setViewportSize({ width: 390, height: 844 })
      await send().click(); await confirmation().waitFor()
      const confirmPath = `output/playwright/onboarding-controlled-${platform}-confirmation-390.png`
      await page.screenshot({ path: confirmPath, fullPage: true }); screenshots.push(confirmPath)
      await confirmation().getByRole('button', { name: '确认发送', exact: true }).click()
      await modal().getByRole('alert').filter({ hasText: '原请求尚待核对' }).waitFor()
      check(count.sends === 1 && await send().isDisabled(), 'Ambiguous response allowed a repeat send')
      const pending = await page.evaluate(() => Object.entries(sessionStorage)
        .filter(([key]) => key.startsWith('wsl:controlled-friend:'))
        .map(([, value]) => JSON.parse(value)))
      const saved = pending.find(value => value.challengeID === challenge.id)
      // The authorization snapshot now survives reloads. It contains only the
      // original scope and idempotency keys, never native contact bodies.
      check(saved?.windowID === windows[index].id && saved.sendKey === windows[index].sendKey &&
        fields(saved, ['challengeID', 'windowID', 'window', 'prepareKey', 'sendKey']) &&
        fields(saved.window, ['id', 'actor_id', 'group_id', 'account_ids', 'account_epochs',
          'admission_mode', 'cancelled', 'used', 'budget', 'actions', 'target_ids', 'expires_at']) &&
        saved.window.id === windows[index].id && saved.window.actor_id === actor &&
        saved.window.group_id === group.group_id &&
        saved.window.account_ids.join() === account.id &&
        saved.window.target_ids.join() === windows[index].target_ids.join(),
        'Ambiguous send lost its original key or stored private target payloads')
      await layout(platform, 'pending', 390)
      await page.reload(); await ready()
      await page.getByRole('button').filter({ hasText: group.name })
        .filter({ hasText: '受控搜索加好友' }).click()
      await modal().waitFor()
      check(await send().isDisabled() && count.sends === 1, 'Reload reopened an ambiguous mutation')
      await modal().getByRole('button', { name: '读取原记录', exact: true }).click()
      await original().getByText(/结果未知/).waitFor()
      check(await send().isDisabled(), 'Reading unknown history reopened send')
      await modal().getByRole('button', { name: '取消本次授权', exact: true }).click()
      await modal().locator('[aria-label="受控好友授权"]').getByText(/已取消/).waitFor()
      await original().getByRole('button', { name: '只读核对原结果', exact: true }).click()
      await original().getByText(/已观察到好友关系/).waitFor()
      check(count.windows === 1 && count.searches === 1 && count.sends === 1 &&
        count.cancels === 1 && count.reconciles === 1 && !challenge.identity_proven &&
        await send().isDisabled(), 'Recovery replayed a mutation or promoted contact to identity')
      await layout(platform, 'reconciled', 390)
      await modal().getByRole('button', { name: '关闭', exact: true }).click()
      await modal().waitFor({ state: 'hidden' })
    }
    check(unexpected.length === 0, unexpected.join('; '))
    return { platforms, widths, calls: Object.fromEntries(calls), screenshots,
      synthetic_only: true, unknown_send_replayed: false }
  } finally {
    await page.unroute('**/api/**', handler)
  }
}
