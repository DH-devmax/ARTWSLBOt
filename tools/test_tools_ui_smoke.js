// Synthetic tool endpoints only. The shared runner blocks external requests.
async (page) => {
  const base = 'http://127.0.0.1:5173'
  const id = n => `72000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const owner = id(99)
  const account = { id: id(1), name: '验证账号甲', workspace: 'wsl', platform: 'wsl', purpose: 'management',
    testing_enabled: true, connection_enabled: true, version: 1, epoch: 1 }
  const second = { ...account, id: id(2), name: '验证账号乙' }
  const group = { group_id: id(3), name: '隔离验证群', native_id: '123', platform: 'wsl', primary_account_id: account.id }
  const grant = { id: id(4), group_id: group.group_id, account_ids: [account.id], target_ids: ['8'],
    actions: ['mute_member', 'unmute_member', 'mute_group', 'unmute_group', 'kick_member', 'publish_notice'],
    used: 0, budget: 3, cancelled: false, expires_at: new Date(Date.now() + 300000).toISOString() }
  let writes = 0, reads = 0, reconcile = 0, key = '', op = null, mode = 'unknown',
    holdReceiver = false, releaseReceiver, receiverArrived, effectFailure = false, historyFailure = false
  const history = []
  const handler = async route => {
    const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', '')
    let data = []
    if (path === '/me') data = { user: { id: owner, username: 'tool-owner', role: 'superadmin' },
      permissions: ['overview:read', 'accounts:read', 'groups:read', 'tests:manage'] }
    else if (path === '/workspaces') data = { items: [{ id: 'wsl', name: '旺商聊机器人' }] }
    else if (path === '/accounts') data = [account, second]
    // A paused controlled proof is absent from the customer/business directory.
    else if (path === '/groups') data = []
    else if (path === '/test-tools/onboarding/scopes') data = {
      items: [],
      candidates: { accounts: [], groups: [
        { id: group.group_id, name: group.name, native_id: group.native_id },
        { id: id(33), name: '其他平台隔离群', native_id: 'other-platform', platform: 'lajiaoliao' }
      ] }
    }
    // Keep the final parent-list count stale to exercise immediate local fencing.
    else if (path === '/test-windows') data = [{ ...grant, used: Math.min(grant.used, 2) }]
    else if (path === '/test-tools') data = { tools: [{ name: 'run_moderation', description: '仅限授权窗口' }] }
    else if (path === `/accounts/${account.id}/receiver` || path === `/accounts/${second.id}/receiver`) {
      if (holdReceiver && path.includes(account.id)) {
        holdReceiver = false
        receiverArrived?.()
        await new Promise(resolve => { releaseReceiver = resolve })
      }
      data = { enabled: true, revision: 1, observed: true, snapshot: { state: 'connected' } }
    }
    else if (path === '/test-tools/operations') {
      if (historyFailure) return route.fulfill({ status: 503, json: { code: 1, msg: 'database_unavailable' } })
      data = new URL(req.url()).searchParams.get('account_id') === account.id ? history : []
    }
    else if (path === `/accounts/${account.id}/native-role`) {
      if (req.postDataJSON().group_id !== group.native_id) throw new Error('Role check changed group')
      data = { observation: { role: 'admin', observed_at: new Date().toISOString() } }
    }
    else if (path === '/test-tools/call') {
      const body = req.postDataJSON(), args = body.arguments
      if (req.method() !== 'POST' || args.account_id !== account.id || req.headers()['x-workspace'] !== 'wsl' ||
        ['tenant_id', 'role', 'user_id'].some(k => k in args)) throw new Error('Tool scope was not fixed')
      switch (body.name) {
        case 'read_group':
          reads++
          if (args.test_window_id !== grant.id) throw new Error('Read grant missing')
          if (effectFailure) return route.fulfill({ status: 409, json: { code: 1, msg: 'native_test_window_invalid' } })
          data = { snapshot: { self_id: '7', self_role: 'admin', in_group: true, policy: { muted: false }, mutes_known: mode !== 'unknown',
            muted_ids: writes > 1 ? ['8'] : [], members: [{ id: '8', name: '配合成员', role: 'member' }],
            sampled_at: new Date().toISOString() } }
          break
        case 'run_moderation':
          if (args.test_window_id !== grant.id || args.target_id !== '8' || args.action !== 'mute_member' ||
            args.minutes !== 1 || !req.headers()['idempotency-key']) throw new Error('Native action expanded its grant')
          key = req.headers()['idempotency-key']; writes++; grant.used++
          op = { id: id(79 + writes), kind: 'mute_member',
            status: mode === 'unknown' ? 'unknown' : 'succeeded',
            result_code: mode === 'unknown' ? 'native_transport_unknown' : 'native_moderation_acknowledged',
            group_id: group.group_id, test_window_id: grant.id, created_at: new Date().toISOString() }
          history.unshift(op)
          data = { operation: op }
          break
        case 'inspect_operation':
          if (args.key !== key && args.operation_id !== op?.id) throw new Error('Original key lost')
          data = { operation: op }
          break
        case 'reconcile_operation':
          if (args.operation_id !== op?.id) throw new Error('Reconciliation changed operation')
          reconcile++
          op = { ...op, status: 'succeeded', result_code: 'desired_state_observed' }
          history[0] = op
          data = { operation: op, read_only: true }
          break
        default: throw new Error('Unexpected tool ' + body.name)
      }
    } else if (path === `/test-windows/${grant.id}/cancel`) { grant.cancelled = true; data = { cancelled: true } }
    await route.fulfill({ json: { code: 0, data, msg: '' } })
  }
  await page.route('**/api/v1/**', handler)
  try {
    await page.goto(base)
    await page.evaluate(({ owner, account }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:account:${owner}:wsl`, account)
    }, { owner, account: account.id })
    await page.goto(base + '/#tests')
    await page.reload()
    await page.locator('.tools-grant').click()
    await page.getByText('消息接收：已连接', { exact: true }).waitFor()
    if (!await page.getByText('动作准入', { exact: true }).isVisible() ||
      await page.getByText('接收已启用', { exact: true }).count()) throw new Error('Control admission confused with receive state')
    await page.getByRole('button', { name: '核验当前角色', exact: true }).click()
    await page.getByText(/当前角色：管理员/).waitFor()
    await page.getByRole('combobox', { name: '角色核验群组', exact: true }).press('ArrowDown')
    if (await page.getByRole('option', { name: '其他平台隔离群', exact: true }).count())
      throw new Error('Test group preparation crossed platforms')
    await page.getByRole('option', { name: '隔离验证群', exact: true }).click()
    await page.getByRole('button', { name: '创建授权窗口', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '创建授权窗口', exact: true })
    await dialog.getByText('验证账号甲', { exact: true }).waitFor()
    await dialog.getByText('隔离验证群', { exact: true }).waitFor()
    if (await dialog.getByRole('spinbutton').first().inputValue() !== '300')
      throw new Error('Grant form did not retain the selected preparation scope')
    await dialog.getByRole('button', { name: '取消', exact: true }).click()
    for (const width of [320, 390, 647, 1440]) {
      await page.setViewportSize({ width, height: 779 })
      await page.evaluate(() => Promise.all(document.getAnimations().filter(a => a.effect?.getComputedTiming().iterations !== Infinity).map(a => a.finished.catch(() => {}))))
      if (!await page.locator('.test-tools').evaluate(el => el.scrollWidth <= el.clientWidth + 1))
        throw new Error('Tools page overflow ' + width)
      await page.screenshot({ path: `output/playwright/test-tools-${width}.png`, fullPage: true })
    }
    await page.getByRole('button', { name: '读取当前群状态', exact: true }).click()
    await page.getByText('个人禁言名单：未取得可靠状态', { exact: true }).waitFor()
    await page.getByRole('combobox', { name: '授权目标成员' }).press('ArrowDown')
    await page.getByRole('option', { name: '配合成员 · 8', exact: true }).click()
    await page.getByRole('button', { name: '确认验证此动作', exact: true }).click()
    if (await page.getByRole('combobox', { name: '验证动作' }).isEnabled() ||
      await page.getByRole('combobox', { name: '测试账号' }).isEnabled() ||
      await page.locator('.tools-grant').isEnabled()) throw new Error('Confirmation did not freeze scope and parameters')
    await page.getByRole('button', { name: '取消', exact: true }).click()
    if (writes !== 0) throw new Error('Cancelled native action submitted')
    await page.getByRole('button', { name: '确认验证此动作', exact: true }).click()
    await page.getByRole('button', { name: '确认提交', exact: true }).click()
    await page.getByText('结果未知，禁止重复提交', { exact: true }).waitFor()
    if (!await page.getByRole('button', { name: '确认验证此动作', exact: true }).isDisabled()) throw new Error('Unknown result could be resubmitted')
    await page.reload()
    await page.getByRole('button', { name: '查询此操作', exact: true }).waitFor()
    await page.getByRole('button', { name: '查询原操作', exact: true }).click()
    await page.getByText('结果未知，禁止重复提交', { exact: true }).waitFor()
    await page.evaluate(() => Object.keys(sessionStorage).filter(k => k.startsWith('wsl:test-tool:')).forEach(k => sessionStorage.removeItem(k)))
    await page.reload()
    await page.getByRole('button', { name: '查询此操作', exact: true }).click()
    await page.getByText('结果未知，禁止重复提交', { exact: true }).waitFor()
    await page.locator('.tools-grant').click()
    await page.getByRole('button', { name: '取消授权', exact: true }).click()
    await page.getByRole('button', { name: '只读核对结果', exact: true }).click()
    await page.waitForFunction(() => !document.querySelector('.test-tools .is-loading'))
    await page.getByText('只读核对已观察到期望状态，未重复提交', { exact: true }).waitFor()
    if (writes !== 1 || reads !== 1 || reconcile !== 1) throw new Error('Refresh/cancel/reconcile repeated mutation')
    // Server history recovers even after local browser storage is lost.
    await page.evaluate(() => Object.keys(sessionStorage).filter(k => k.startsWith('wsl:test-tool:')).forEach(k => sessionStorage.removeItem(k)))
    await page.reload()
    await page.getByRole('button', { name: '查询此操作', exact: true }).click()
    await page.getByText('只读核对已观察到期望状态，未重复提交', { exact: true }).waitFor()
    // A fresh synthetic window exercises automatic effect reads, then a failed
    // readback. Neither path may submit a second mutation.
    grant.cancelled = false
    mode = 'succeeded'
    await page.reload()
    await page.locator('.tools-grant').click()
    await page.getByRole('button', { name: '读取当前群状态', exact: true }).click()
    await page.locator('.tools-state').waitFor()
    await page.getByRole('combobox', { name: '授权目标成员' }).press('ArrowDown')
    await page.getByRole('option', { name: '配合成员 · 8', exact: true }).click()
    historyFailure = true
    await page.getByRole('button', { name: '刷新检查', exact: true }).click()
    await page.locator('.tools-preparation .el-alert').waitFor()
    if (!await page.getByRole('button', { name: '确认验证此动作', exact: true }).isDisabled())
      throw new Error('History check failure admitted a new submission')
    historyFailure = false
    await page.getByRole('button', { name: '刷新检查', exact: true }).click()
    await page.getByRole('button', { name: '确认验证此动作', exact: true }).click()
    await page.getByRole('button', { name: '确认提交', exact: true }).click()
    await page.getByText('只读核对：已观察到期望状态；未验证禁言自动到期时长。', { exact: true }).waitFor()
    if (writes !== 2) throw new Error('Effect readback repeated a mutation')
    await page.getByRole('button', { name: '确认验证此动作', exact: true }).click()
    effectFailure = true
    await page.getByRole('button', { name: '确认提交', exact: true }).click()
    await page.getByText(/操作已确认；效果读取未完成/).waitFor()
    if (writes !== 3) throw new Error('Failed effect readback repeated a mutation')
    if (!await page.getByRole('button', { name: '确认验证此动作', exact: true }).isDisabled()) throw new Error('Exhausted window accepted an action')
    await page.getByText('3 / 3 次 · 次数已用完', { exact: true }).waitFor()
    // Account switch while the old read is outstanding must discard its result.
    holdReceiver = true
    const arrived = new Promise(resolve => { receiverArrived = resolve })
    await page.getByRole('button', { name: '刷新检查', exact: true }).click()
    await arrived
    await page.getByRole('combobox', { name: '机器人面板', exact: true }).press('ArrowDown')
    await page.getByRole('option', { name: /验证账号乙/ }).click()
    await page.getByText('暂无操作记录', { exact: true }).waitFor()
    releaseReceiver()
    await page.waitForFunction(() => !document.querySelector('.test-tools .is-loading'))
    if (await page.locator('.tools-history-row').count() || await page.locator('.tools-result').count())
      throw new Error('Old account leaked results into new account')
    historyFailure = true
    await page.getByRole('button', { name: '刷新检查', exact: true }).click()
    await page.locator('.tools-preparation .el-alert').waitFor()
    const textAdmission = []
    let clockOffset = 0
    const fixtureNow = () => Date.now() + clockOffset
    await page.clock.install({ time: new Date(fixtureNow()) })
    for (const platform of ['lajiaoliao', 'wsl']) {
      const sender = { ...account, workspace: platform, platform }
      const self = platform === 'wsl' ? '7' : 'sender-string'
      const nativeGroup = platform === 'wsl' ? '123' : 'group-string'
      const textGroup = { ...group, platform, native_id: nativeGroup }
      const textGrant = { ...grant, admission_mode: 'native_debug', actions: ['send_text'],
        target_ids: [self], used: 0, budget: 1, cancelled: false,
        expires_at: new Date(fixtureNow() + 60000).toISOString() }
      const conversation = 'd'.repeat(64)
      let inGroup = true, role = 'admin', stale = false, legacyPresent = false, textWrites = 0, dispatches = 0
      const admitted = () => inGroup && ['member', 'admin', 'owner'].includes(role) &&
        textGrant.target_ids.includes(self) && !textGrant.cancelled &&
        new Date(textGrant.expires_at).getTime() > fixtureNow() + 3000
      const textHandler = async route => {
        const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', '')
        const fulfill = data => route.fulfill({ json: { code: 0, data, msg: '' } })
        const messagePath = `/accounts/${sender.id}/conversations/${conversation}`
        if (path === '/me') return fulfill({ user: { id: owner, username: 'text-owner', role: 'superadmin' },
          permissions: ['overview:read', 'accounts:read', 'groups:read', 'tests:manage'] })
        if (path === '/workspaces') return fulfill({ items: [{ id: platform, name: '合成文本验证' }] })
        if (path === '/accounts') return fulfill([sender])
        if (path === '/groups') return fulfill([textGroup])
        if (path === '/test-windows') return fulfill([textGrant])
        if (path === '/test-tools') return fulfill({ tools: [] })
        if (path === `/accounts/${sender.id}/receiver`)
          return fulfill({ enabled: true, revision: 1, observed: true, snapshot: { state: 'connected' } })
        if (path === '/test-tools/operations') return fulfill([])
        if (path === '/test-tools/call') {
          const body = req.postDataJSON()
          if (body.name !== 'read_group' || body.arguments.account_id !== sender.id ||
            body.arguments.test_window_id !== textGrant.id || req.headers()['x-workspace'] !== platform)
            throw new Error('Text snapshot escaped its platform/account/window')
          return fulfill({ snapshot: {
            platform, self_id: self, self_role: role, in_group: legacyPresent ? undefined : inGroup,
            ...(legacyPresent ? { present: true } : {}),
            group_id: nativeGroup, group_name: textGroup.name, members: [], policy: { group_id: nativeGroup, muted: false },
            muted_ids: [], mutes_known: true, nickname_locked: false, native_actions_enabled: true,
            account_epoch: 1, account_version: 1, deployment_version: 1, digest: 'synthetic',
            sampled_at: new Date(fixtureNow() - (stale ? 31000 : 1000)).toISOString()
          } })
        }
        if (path === '/test-tools/text' || path === `${messagePath}/text-operations`) {
          const body = req.postDataJSON()
          if (!admitted() || !req.headers()['idempotency-key'] || req.headers()['x-workspace'] !== platform ||
            body.test_window_id !== textGrant.id || body.text !== '合成受控文本' ||
            (platform === 'lajiaoliao' ? path !== '/test-tools/text' || body.account_id !== sender.id :
              path !== `${messagePath}/text-operations` || body.account_epoch !== 1 || body.binding_epoch !== 1) ||
            ['tenant_id', 'role', 'user_id'].some(field => field in body))
            throw new Error('Text submission bypassed its existing endpoint or scope')
          textWrites++
          const operation = { id: id(88), kind: 'send_text',
            status: platform === 'wsl' ? 'queued' : 'succeeded',
            result_code: platform === 'wsl' ? '' : 'native_acknowledged',
            group_id: textGroup.group_id, test_window_id: textGrant.id }
          return fulfill(operation)
        }
        if (path === `${messagePath}/text-operations/${id(88)}/dispatch`) {
          if (platform !== 'wsl' || !admitted() || textWrites !== 1 || req.method() !== 'POST')
            throw new Error('Unexpected text dispatch')
          dispatches++
          return fulfill({ id: id(88), kind: 'send_text', status: 'succeeded', result_code: 'im_server_accepted' })
        }
        if (path === `${messagePath}/sending`) return fulfill({
          enabled: admitted(), reason: admitted() ? '' : 'native_test_window_invalid', max_text_bytes: 3000,
          scope: { scene: 'group', group_id: textGroup.group_id, account_epoch: 1, binding_epoch: 1 },
          test_windows: admitted() ? [{ ...textGrant, remaining: 1, binding_epoch: 1 }] : [], recent: []
        })
        if (path === `/accounts/${sender.id}/conversations`) return fulfill({ items: [{
          key: conversation, scene: 'group', title: textGroup.name, business_id: nativeGroup,
          count: 1, last_at: new Date(fixtureNow()).toISOString(),
          last: { message: { format: 0, text: '合成历史文本', sender: { id: self }, target: {} } }
        }], total: 1, directory_total: 0, directory: { groups: [], stale: false } })
        if (path === `${messagePath}/messages`) return fulfill({ items: [], next_before: '' })
        if (path.endsWith('/decoder-health')) return fulfill({ status: 'healthy', total: 0, reasons: {},
          pool: { workers: 4, queue_capacity: 16 }, receiver: { snapshot: { state: 'connected' } } })
        if (path.endsWith('/events'))
          return route.fulfill({ contentType: 'text/event-stream', body: '' })
        if (req.method() !== 'GET') throw new Error('Unexpected text fixture mutation: ' + path)
        return fulfill([])
      }
      await page.route('**/api/v1/**', textHandler)
      try {
        await page.evaluate(({ owner, platform, account }) => {
          sessionStorage.clear()
          sessionStorage.setItem(`wsl:workspace:${owner}`, platform)
          sessionStorage.setItem(`wsl:account:${owner}:${platform}`, account)
        }, { owner, platform, account: sender.id })
        await page.goto(base + '/#tests')
        await page.reload()
        await page.locator('.tools-grant').click()
        let send, input
        if (platform === 'lajiaoliao') {
          send = page.getByRole('button', { name: '确认验证此动作', exact: true })
          input = page.getByRole('textbox', { name: '群文本', exact: true })
        } else {
          // WSL uses its durable conversation composer, never the Chili-only Tools route.
          if (await page.getByRole('textbox', { name: '群文本', exact: true }).count() ||
            !await page.getByRole('button', { name: '确认验证此动作', exact: true }).isDisabled())
            throw new Error('WSL text was routed to the Chili-only Tools endpoint')
          await page.goto(base + '/#messages')
          await page.getByTestId('message-center').getByRole('button').filter({ hasText: textGroup.name }).click()
          const composer = page.getByRole('region', { name: '消息发送', exact: true })
          send = composer.getByRole('button', { name: '发送消息', exact: true })
          input = composer.getByRole('textbox', { name: '消息内容', exact: true })
        }
        await input.fill('合成受控文本')
        const refresh = async () => {
          const button = platform === 'lajiaoliao'
            ? page.getByRole('button', { name: '读取当前群状态', exact: true })
            : page.getByRole('button', { name: '刷新发送条件', exact: true })
          await button.click()
          await page.waitForFunction(() => !document.querySelector('.test-tools .is-loading, .message-composer .is-loading'))
        }
        const verify = async (name, enabled) => {
          await refresh()
          if (await send.isEnabled() !== enabled || textWrites || dispatches)
            throw new Error(`${platform} text admission failed: ${name}`)
          textAdmission.push(`${platform}:${name}`)
        }
        for (const validRole of ['member', 'admin', 'owner']) {
          role = validRole
          await verify('in_group_' + role, true)
        }
        inGroup = false
        await verify('left_group', false)
        inGroup = true; role = ''
        await verify('unverified_role', false)
        role = 'admin'; textGrant.target_ids = ['other-member']
        if (platform === 'lajiaoliao') {
          await page.reload()
          await page.locator('.tools-grant').click()
          await input.fill('合成受控文本')
        }
        await verify('wrong_authorized_target', false)
        textGrant.target_ids = [self]
        if (platform === 'lajiaoliao') {
          await page.reload()
          await page.locator('.tools-grant').click()
          await input.fill('合成受控文本')
          stale = true
          await verify('stale_snapshot', false)
          stale = false; legacyPresent = true
          await verify('legacy_present_is_not_in_group', false)
          legacyPresent = false
        }
        await refresh()
        await send.click()
        await page.getByRole('button', { name: '取消', exact: true }).click()
        if (textWrites || dispatches) throw new Error('Text confirmation cancellation submitted')
        textAdmission.push(`${platform}:cancel_without_submit`)
        if (platform === 'lajiaoliao') {
          await send.click()
          clockOffset += 61000
          await page.clock.fastForward(61000)
          await page.getByRole('dialog', { name: '确认原生验证', exact: true }).waitFor({ state: 'hidden' })
          await page.getByRole('button', { name: '刷新检查', exact: true }).and(page.locator(':enabled')).waitFor()
          await page.getByText('窗口已关闭或即将到期；原操作仍可在下方查询。', { exact: true }).waitFor()
          if (textWrites || !await send.isDisabled()) throw new Error('Expired confirmation submitted or retained executable evidence')
          textGrant.expires_at = new Date(fixtureNow() + 60000).toISOString()
          await page.reload()
          await page.locator('.tools-grant').click()
          await input.fill('合成受控文本')
          await refresh()
        } else {
          textGrant.expires_at = new Date(fixtureNow() - 1000).toISOString()
          await verify('expired_window', false)
          textGrant.expires_at = new Date(fixtureNow() + 60000).toISOString()
          await refresh()
        }
        textAdmission.push(`${platform}:expired_without_submit`)
        await send.click()
        await page.getByRole('button', {
          name: platform === 'lajiaoliao' ? '确认提交' : '确认发送', exact: true
        }).last().click()
        await page.getByText(platform === 'lajiaoliao' ? '上游已确认操作' :
          '原生服务端已接受，不代表成员已送达或已读', { exact: true }).waitFor()
        if (textWrites !== 1 || dispatches !== (platform === 'wsl' ? 1 : 0))
          throw new Error('Controlled text did not submit exactly once through its platform endpoint')
        textAdmission.push(`${platform}:confirmed_once`)
      } finally { await page.unroute('**/api/v1/**', textHandler) }
    }
    return { passed: true, viewports: 4, synthetic_submissions: writes, confirmation_frozen: true,
      server_history_recovered: true, stale_account_read_discarded: true, effect_read_failure_no_replay: true,
      unknown_recovered_after_refresh: true, text_admission: textAdmission, native_requests: 0 }
  } finally { await page.unroute('**/api/v1/**', handler) }
}
