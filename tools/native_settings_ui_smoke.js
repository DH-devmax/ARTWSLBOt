// All settings and submissions are intercepted synthetic fixtures.
async (page) => {
  const base = 'http://127.0.0.1:5173'
  const id = n => `71000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const now = new Date().toISOString()
  const accounts = [1, 2].map(n => ({
    id: id(n), name: `资料账号${n}`, platform: 'wsl', workspace: 'wsl',
    auth_state: 'authenticated', native_nickname: '原生昵称', version: 1, epoch: 1,
    connection_enabled: true, runtime_state: 'connected'
  }))
  let nickname = '原生昵称', writes = 0, reads = 0, operation = null, mode = 'success'
  let hold = false, entered, release, nativeActionsEnabled = true
  let mobile = null, searchAccount = false, friend = 'FRIEND_NEED_APPROVAL', group = 'GROUP_ASK_JOIN', calls = false
  let readError = '', pendingError = false, lastChange, lastKey = '', pendingReads = 0
  let securityWrites = 0, securityLastInput, avatarReads = 0, securityStatusReads = 0
  const bitmap = async (color, text) => Buffer.from(await page.evaluate(({ color, text }) => {
    const canvas = document.createElement('canvas')
    canvas.width = canvas.height = 64
    const context = canvas.getContext('2d')
    context.fillStyle = color
    context.fillRect(0, 0, 64, 64)
    context.fillStyle = '#fff'
    context.font = 'bold 32px sans-serif'
    context.textAlign = 'center'
    context.textBaseline = 'middle'
    context.fillText(text, 32, 34)
    return canvas.toDataURL('image/png').split(',')[1]
  }, { color, text }), 'base64')
  const avatarImage = await bitmap('#1b7d72', 'A'), candidateImage = await bitmap('#4276d7', 'B')
  const settings = () => ({ uid: '7', nickname, avatar: '123.jpg', searchable_account: searchAccount,
    searchable_mobile: mobile, friend_policy: friend, group_policy: group, audio_video: calls, masked_mobile: '138****0000' })
  const dashboard = account => ({
    account, sampled_at: now, visibility: 'account',
    groups: { authorized: 1, management_enabled: 1, ai_enabled: 0, ai_pending: 0 },
    operations: { queued: 0, running: 0, unknown: 0, failed_15m: 0 },
    inbox: { pending: 0, unresolved: 0, quarantined: 0 },
    performance: { per_second: 0, inserted_60s: 0, latency_samples: 0, series: [] },
    health: { status: 'healthy', reasons: [] }, backlog: { status: 'healthy', reasons: [] },
    runtime_observed: false
  })
  const handler = async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname.replace('/api/v1', '')
    const account = accounts.find(a => path.includes(a.id)) || accounts[0]
    let data = []
    if (path === '/me') data = { user: { id: id(99), username: 'synthetic-settings', role: 'superadmin' },
      permissions: ['overview:read', 'accounts:read', 'groups:read'] }
    else if (path === '/workspaces') data = { items: [{ id: 'user', name: '用户中心' }, { id: 'wsl', name: '旺商聊机器人' }] }
    else if (path === '/accounts') data = accounts
    else if (path === '/overview') data = { accounts: 2, groups: 1, limits: { accounts: 10, groups: 300, ai_groups: 300 } }
    else if (path.endsWith('/dashboard')) data = dashboard(account)
    else if (path.includes('/native-avatar/')) {
      if (req.method() !== 'GET' || req.headers()['x-workspace'] !== 'wsl' ||
        !path.endsWith(String(accounts.indexOf(account) + 1).repeat(64)))
        throw new Error('Avatar read lost its account or workspace')
      avatarReads++
      return route.fulfill({ body: avatarImage, contentType: 'image/png', headers: { 'Cache-Control': 'no-store' } })
    }
    else if (path.endsWith('/native-settings')) {
      if (req.method() !== 'GET') throw new Error('Settings read became a write')
      reads++
      if (readError) return route.fulfill({ status: 409, json: { code: 409, msg: readError } })
      if (hold) { hold = false; entered(); await new Promise(resolve => { release = resolve }) }
      const target = url.searchParams.get('target_id')
      data = { native_ready: false,
        avatar_url: target ? '' : `/api/v1/accounts/${account.id}/native-avatar/${String(accounts.indexOf(account) + 1).repeat(64)}`,
        security: { avatar: true, password: true, phone: true }, snapshot: {
        captcha_id: 'synthetic-captcha',
        self_id: '7', account_epoch: 1, account_version: 1, deployment_version: 1,
        digest: 'a'.repeat(64), sampled_at: now, nickname_locked: false, self_role: 'admin', native_actions_enabled: nativeActionsEnabled,
        ...(target ? { member: { id: target, name: target === '7' ? '机器人自己' : '其他成员', card: '原群昵称', role: 'member', transport: 'fixture-peer' } }
          : { settings: settings() })
      } }
    } else if (path.endsWith('/native-security-status')) {
      securityStatusReads++
      throw new Error('Deferred security capability issued a status read')
    } else if (path.endsWith('/native-security-operations')) {
      const body = req.postDataJSON(), key = req.headers()['idempotency-key']
      if (req.method() !== 'POST' || !key || body.digest !== 'a'.repeat(64) ||
        body.account_epoch !== 1 || body.account_version !== 1 || 'tenant_id' in body)
        throw new Error('Security operation lost explicit scope/version')
      if (body.kind !== 'avatar') throw new Error('Retired security control issued a submission')
      securityLastInput = body.input
      if (['key','cert','url','jwt','token'].some(k => k in body.input)) throw new Error('Browser supplied trusted verification state')
      securityWrites++
      operation = { id: id(90 + securityWrites), kind: body.kind, status: 'succeeded', result_code: 'native_acknowledged' }
      data = { operation, native_ready: false }
    } else if (path.includes('/native-security-flows/')) {
      throw new Error('Retired security flow issued a read')
    } else if (path.endsWith('/native-setting-operations') && req.method() === 'POST') {
      const body = req.postDataJSON()
      if (!req.headers()['idempotency-key'] || body.digest !== 'a'.repeat(64) || body.account_epoch !== 1 ||
        body.account_version !== 1 || 'tenant_id' in body || 'role' in body || Object.keys(body.change).includes('url'))
        throw new Error('Setting submission lost its scope/version or trusted browser identity')
      if (!['nickname', 'member_nickname'].includes(body.change.kind))
        throw new Error('Retired privacy control issued a submission')
      writes++
      lastChange = body.change
      lastKey = req.headers()['idempotency-key']
      if (mode !== 'failed') {
        if (body.change.kind === 'nickname') nickname = body.change.text
        if (body.change.kind === 'friend_policy') friend = body.change.text
        if (body.change.kind === 'group_policy') group = body.change.text
        if (body.change.kind === 'search') { mobile = body.change.searchable_mobile; searchAccount = body.change.searchable_account }
        if (body.change.kind === 'audio_video') calls = body.change.enabled
      }
      operation = { id: id(50 + writes), key: lastKey, status: ['unknown', 'failed'].includes(mode) ? mode : 'succeeded',
        result_code: mode === 'unknown' ? 'native_transport_unknown' : 'native_acknowledged' }
      data = { operation, native_ready: false }
    } else if (path.includes('/native-setting-operations/') && path.endsWith('/reconcile')) {
      if (req.method() !== 'POST') throw new Error('Unexpected reconciliation method')
      operation = { ...operation, status: 'succeeded', result_code: 'desired_state_observed' }
      data = { operation, read_only: true }
    } else if (path.endsWith('/native-setting-operations')) {
      pendingReads++
      if (pendingError) return route.fulfill({ status: 503, json: { code: 503, msg: 'database_unavailable' } })
      data = url.searchParams.has('key') ? { operation } :
        { pending: operation?.status === 'unknown' ? [operation] : [] }
    }
    else if (path.endsWith('/conversations')) data = { items: [], total: 0, directory_total: 1,
      directory: { sampled_at: now, stale: false, error: '', groups: [{ id: '123', name: '设置合成群', team: '456', platform: 'wsl' }] } }
    else if (path.endsWith('/decoder-health')) data = { receiver: { observed: false, snapshot: {} }, pool: {}, reasons: {}, total: 0, pending: 0, processing: 0, decoded: 0, unsupported: 0, failed: 0 }
    else if (path.endsWith('/native-members')) data = { read_only: true, role_authorizes_actions: false, native_ready: false,
      snapshot: { group_id: '123', self_id: '7', self_role: 'admin', sampled_at: now, complete: true, mapping_complete: true, pages: 1,
        members: [{ id: '7', name: '机器人自己', card: '原群昵称', role: 'admin', transport: 'self' }, { id: '8', name: '其他成员', card: '', role: 'member', transport: 'other' }] } }
    else if (req.method() !== 'GET') throw new Error('Unexpected write: ' + path)
    await route.fulfill({ json: { code: 0, msg: 'ok', data } })
  }
  const choose = async name => {
    await page.getByRole('combobox', { name: '机器人面板', exact: true }).locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option').filter({ hasText: name }).click()
  }
  await page.route('**/api/v1/**', handler)
  await page.addInitScript(() => {
    window.initNECaptcha = (options, ready) => ready({
      verify() { options.onVerify(null, { validate: 'synthetic-manual-validation' }) },
      destroy() {}
    })
  })
  try {
    await page.setViewportSize({ width: 1440, height: 900 })
    await page.goto(base + '/#overview')
    await page.evaluate(() => sessionStorage.clear())
    await page.reload()
    await choose('资料账号1')
    await page.getByRole('button', { name: '账号资料', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '原生昵称 · 资料账号1 · 账号资料与设置', exact: true })
    await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).waitFor()
    await dialog.locator('.settings-identity-avatar img').waitFor()
    await page.waitForFunction(() => {
      const img = document.querySelector('.settings-identity-avatar img')
      return img?.complete && img.naturalWidth > 0
    })
    if (!avatarReads) throw new Error('Original avatar was replaced by a placeholder')
    if (!await dialog.getByRole('button', { name: '保存昵称', exact: true }).isDisabled())
      throw new Error('Unchanged nickname permits a write')
    // Privacy/password/phone editing was retired from this surface. Nickname and
    // avatar keep the confirmation, draft, unknown-result and account fences.
    const retiredControlsHidden = async target => {
      for (const name of ['隐私与互动', '账号安全', '头像与安全'])
        if (await target.getByRole('tab', { name, exact: true }).count())
          throw new Error('Retired settings tab remains visible: ' + name)
      for (const name of ['密码', '手机号', '二级安全码', '实名认证'])
        if (await target.getByRole('radio', { name, exact: true }).count())
          throw new Error('Retired security control remains visible: ' + name)
      if (await target.getByRole('combobox', { name: '好友申请政策', exact: true }).count())
        throw new Error('Legacy privacy grant exposed a retired editor')
    }
    await retiredControlsHidden(dialog)
    await dialog.getByRole('tab', { name: '头像', exact: true }).click()
    if (writes) throw new Error('Opening settings performed a native write')
    for (const width of [320, 390, 768, 1440]) {
      await page.setViewportSize({ width, height: 779 })
      await dialog.evaluate(async el => {
        await Promise.all(el.getAnimations({ subtree: true })
          .filter(a => a.effect?.getComputedTiming().iterations !== Infinity)
          .map(a => a.finished.catch(() => {})))
      })
      const fits = await dialog.evaluate(el => {
        const b = el.getBoundingClientRect()
        return b.x >= -1 && b.y >= -1 && b.right <= innerWidth + 1 && b.bottom <= innerHeight + 1 &&
          el.scrollWidth <= el.clientWidth + 1 &&
          Number.parseFloat(getComputedStyle(el.querySelector('.el-dialog__body')).paddingLeft) >= 16
      })
      await page.screenshot({ path: `output/playwright/native-settings-${width}.png`, fullPage: true })
      if (!fits) throw new Error('Native settings dialog overflow: ' + width + ' ' + JSON.stringify(await dialog.evaluate(el => ({
        bounds: el.getBoundingClientRect().toJSON(), scroll: el.scrollWidth, client: el.clientWidth,
        overflow: [...el.querySelectorAll('*')].filter(n => n.getBoundingClientRect().right > innerWidth).slice(0,5).map(n => n.className)
      }))))
      await dialog.getByRole('tab', { name: '账号资料', exact: true }).click()
      await dialog.evaluate(el => Promise.all(el.getAnimations({ subtree: true })
        .filter(a => a.effect?.getComputedTiming().iterations !== Infinity)
        .map(a => a.finished.catch(() => {}))))
      await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).fill('未提交资料草稿')
      await page.screenshot({ path: `output/playwright/native-settings-profile-${width}.png`, fullPage: true })
      await dialog.getByRole('tab', { name: '头像', exact: true }).click()
    }
    await dialog.getByRole('tab', { name: '账号资料', exact: true }).click()
    if (await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).inputValue() !== '未提交资料草稿')
      throw new Error('Tab switch discarded nickname draft')
    const readCount = reads
    await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).fill('😀'.repeat(11))
    if (!await dialog.getByRole('button', { name: '保存昵称', exact: true }).isDisabled())
      throw new Error('UTF-16 nickname limit not enforced')
    await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).fill(' 前后空格 ')
    if (!await dialog.getByRole('button', { name: '保存昵称', exact: true }).isDisabled())
      throw new Error('Whitespace nickname was silently trimmed')
    await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).fill('保留草稿')
    await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    await page.getByRole('button', { name: '取消', exact: true }).click()
    if (await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).inputValue() !== '保留草稿')
      throw new Error('Cancelled close discarded draft')
    await dialog.getByRole('button', { name: '重新读取', exact: true }).click()
    await page.getByRole('button', { name: '取消', exact: true }).click()
    if (reads !== readCount) throw new Error('Cancelled refresh made a native read')
    await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).fill('新原生昵称')
    await dialog.getByRole('button', { name: '保存昵称', exact: true }).click()
    await page.getByRole('dialog', { name: '确认原生修改', exact: true }).getByText('原生昵称：原生昵称 → 新原生昵称', { exact: true }).waitFor()
    if (!await dialog.getByRole('button', { name: '保存昵称', exact: true }).isDisabled())
      throw new Error('Confirmation allowed a second concurrent submission')
    await page.getByRole('button', { name: '取消', exact: true }).click()
    if (writes) throw new Error('Cancelled confirmation submitted')
    await dialog.getByRole('button', { name: '保存昵称', exact: true }).click()
    await page.getByRole('button', { name: '确认修改', exact: true }).click()
    await dialog.getByText('旺商聊已确认保存；其他客户端显示可能稍有延迟', { exact: true }).waitFor()
    if (writes !== 1) throw new Error('Nickname save count mismatch')
    await retiredControlsHidden(dialog)
    if (lastChange.kind !== 'nickname' || securityWrites || securityStatusReads)
      throw new Error('Nickname save activated an unrelated settings capability')
    mode = 'unknown'
    await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).fill('待核对昵称')
    await dialog.getByRole('button', { name: '保存昵称', exact: true }).click()
    await page.getByRole('button', { name: '确认修改', exact: true }).click()
    await dialog.getByText('请求结果未知，未自动重试。请核对当前值。', { exact: true }).waitFor()
    if (!await dialog.getByRole('button', { name: '保存昵称', exact: true }).isDisabled() || writes !== 2)
      throw new Error('Unknown submission permitted another write')
    await dialog.getByRole('button', { name: '核对当前值', exact: true }).click()
    await dialog.getByText(/已核对：当前值与期望一致/).waitFor()
    if (writes !== 2) throw new Error('Reconciliation replayed mutation')
    await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).fill('失败后保留输入')
    mode = 'failed'
    await dialog.getByRole('button', { name: '保存昵称', exact: true }).click()
    await page.getByRole('button', { name: '确认修改', exact: true }).click()
    await dialog.getByText(/^未保存：/).waitFor()
    if (await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).inputValue() !== '失败后保留输入' || writes !== 3)
      throw new Error('Failed save discarded input')
    await dialog.getByRole('button', { name: '恢复原生昵称', exact: true }).click()
    pendingError = true
    await dialog.getByRole('button', { name: '重新读取', exact: true }).click()
    await dialog.getByText('未能核对待处理请求，暂不能保存设置', { exact: true }).waitFor()
    if (!await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).isDisabled())
      throw new Error('Unavailable operation query permitted editing')
    pendingError = false
    await dialog.getByRole('button', { name: '重新读取', exact: true }).click()
    await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).waitFor()
    mode = 'unknown'
    await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).fill('跨重开未知请求')
    await dialog.getByRole('button', { name: '保存昵称', exact: true }).click()
    await page.getByRole('button', { name: '确认修改', exact: true }).click()
    await dialog.getByText('请求结果未知，未自动重试。请核对当前值。', { exact: true }).waitFor()
    await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    await page.getByRole('button', { name: '丢弃并关闭', exact: true }).click()
    readError = 'native_reauth_required'
    await page.evaluate(() => sessionStorage.clear())
    await page.getByRole('button', { name: '账号资料', exact: true }).click()
    await dialog.getByText('请求结果未知，未自动重试。请核对当前值。', { exact: true }).waitFor()
    if (!await dialog.getByRole('button', { name: '重新登录', exact: true }).isVisible() ||
      !await dialog.getByRole('button', { name: '保存昵称', exact: true }).isDisabled() || writes !== 4)
      throw new Error('Failed settings read hid pending submission or permitted save')
    await page.screenshot({ path: 'output/playwright/native-settings-pending-error.png', fullPage: true })
    readError = ''
    await dialog.getByRole('button', { name: '核对当前值', exact: true }).click()
    await dialog.getByText(/已核对：当前值与期望一致/).waitFor()
    if (writes !== 4) throw new Error('Reopening unknown operation repeated submission')
    await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    friend = 'FRIEND_ANSWER_QUESTION'
    await page.getByRole('button', { name: '账号资料', exact: true }).click()
    await dialog.getByRole('textbox', { name: '原生昵称', exact: true }).waitFor()
    await retiredControlsHidden(dialog)
    await page.setViewportSize({ width: 320, height: 480 })
    await dialog.evaluate(el => Promise.all(el.getAnimations({ subtree: true })
      .filter(a => a.effect?.getComputedTiming().iterations !== Infinity)
      .map(a => a.finished.catch(() => {}))))
    if (!await dialog.evaluate(el => {
      const b = el.getBoundingClientRect(), footer = el.querySelector('.el-dialog__footer').getBoundingClientRect()
      return b.top >= -1 && b.bottom <= innerHeight + 1 && footer.bottom <= innerHeight + 1
    })) throw new Error('Short viewport lost dialog footer')
    await page.screenshot({ path: 'output/playwright/native-settings-short.png' })
    await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    await page.setViewportSize({ width: 1440, height: 779 })
    friend = 'FRIEND_NEED_APPROVAL'
    hold = true
    const waiting = new Promise(resolve => { entered = resolve })
    await page.getByRole('button', { name: '账号资料', exact: true }).click()
    await waiting
    await dialog.getByRole('button', { name: '关闭', exact: true }).click()
    await choose('资料账号2')
    release()
    if (await dialog.isVisible()) throw new Error('Late settings response reopened old account')
    mode = 'success'
    await page.getByRole('button', { name: '消息与解码', exact: true }).click()
    const center = page.getByTestId('message-center')
    await center.waitFor({ timeout: 10000 }).catch(async error => {
      throw new Error(error.message + '\nSynthetic page: ' + await page.locator('body').innerText())
    })
    await center.getByRole('tab', { name: '原生群组 · 1', exact: true }).click({ timeout: 10000 }).catch(async error => {
      throw new Error(error.message + '\nSynthetic page: ' + await page.locator('body').innerText())
    })
    await center.getByRole('button', { name: '读取设置合成群成员', exact: true }).click()
    const members = page.getByRole('dialog', { name: '设置合成群 · 成员', exact: true })
    await members.getByRole('button', { name: '修改机器人本人群昵称', exact: true }).click()
    const memberEdit = page.getByRole('dialog', { name: '修改群内昵称', exact: true })
    await memberEdit.getByText(/机器人自己\s*（机器人本人）/).waitFor()
    await memberEdit.getByRole('textbox', { name: '新群昵称', exact: true }).fill('机器人本群昵称')
    await memberEdit.getByRole('button', { name: '保存群昵称', exact: true }).click()
    await page.getByRole('button', { name: '确认修改', exact: true }).click()
    await memberEdit.waitFor({ state: 'hidden' })
    if (writes !== 5) throw new Error('Self group nickname missing or duplicated')
    nativeActionsEnabled = false
    await members.getByRole('button', { name: '修改机器人本人群昵称', exact: true }).click()
    await memberEdit.getByText('此账号的原生动作尚未开放；群昵称修改保持关闭', { exact: true }).waitFor()
    if (!await memberEdit.getByRole('button', { name: '保存群昵称', exact: true }).isDisabled() || writes !== 5)
      throw new Error('Closed native gate permitted a group nickname submission')
    await memberEdit.getByRole('button', { name: '关闭', exact: true }).click()
    await members.getByRole('button', { name: '关闭', exact: true }).click()
    await page.goto(base + '/#overview')
    await page.reload()
    await choose('资料账号2')
    await page.getByRole('button', { name: '账号资料', exact: true }).click()
    const securityDialog = page.getByRole('dialog', { name: '原生昵称 · 资料账号2 · 账号资料与设置', exact: true })
    await securityDialog.getByRole('tab', { name: '头像', exact: true }).click()
    await securityDialog.locator('.settings-current-avatar img').waitFor()
    const originalAvatar = await securityDialog.locator('.settings-current-avatar img').getAttribute('src')
    await securityDialog.getByLabel('选择头像', { exact: true }).setInputFiles({
      name: 'avatar.png', mimeType: 'image/png',
      buffer: candidateImage
    })
    await securityDialog.getByText('待上传的新头像', { exact: true }).waitFor()
    if (await securityDialog.locator('.settings-current-avatar img').getAttribute('src') !== originalAvatar ||
        securityWrites) throw new Error('Selecting a new image changed the original avatar')
    for (const width of [320,390,768,1440]) {
      await page.setViewportSize({ width, height: 779 })
      await securityDialog.evaluate(el => Promise.all(el.getAnimations({ subtree: true }).filter(a => a.effect?.getComputedTiming().iterations !== Infinity).map(a => a.finished.catch(() => {}))))
      if (!await securityDialog.evaluate(el => el.scrollWidth <= el.clientWidth + 1 && el.getBoundingClientRect().right <= innerWidth+1))
        throw new Error('Avatar comparison overflow ' + width)
      await page.screenshot({ path: `output/playwright/native-avatar-${width}.png`, fullPage: true })
    }
    await securityDialog.getByRole('button', { name: '上传并保存头像', exact: true }).click()
    await page.getByRole('button', { name: '取消', exact: true }).click()
    if (securityWrites) throw new Error('Cancel uploaded an avatar')
    await securityDialog.getByRole('button', { name: '上传并保存头像', exact: true }).click()
    await page.getByRole('button', { name: '确认提交', exact: true }).click()
    await securityDialog.getByText(/旺商聊已确认保存/).waitFor()
    if (securityWrites !== 1 || !securityLastInput.image) throw new Error('Avatar upload missing')
    await retiredControlsHidden(securityDialog)
    if (securityStatusReads || securityWrites !== 1)
      throw new Error('Deferred security scope produced a read or write')
    const storage = await page.evaluate(() => JSON.stringify(sessionStorage))
    if (storage.includes(securityLastInput.image)) throw new Error('Avatar bytes persisted in browser storage')
    await securityDialog.getByRole('button', { name: '关闭', exact: true }).click()
    return { passed: true, viewports: 4, synthetic_submissions: writes, security_steps: securityWrites, security_status_reads: securityStatusReads,
      deferred_security_hidden: true, avatar_reads: avatarReads, original_avatar_preserved: true,
      unknown_no_retry: true, scope_fencing: true, native_gate_preserved: true, draft_preserved: true, pending_without_profile: true, native_requests: 0, reads, pendingReads }
  } finally {
    if (!page.isClosed()) await page.unroute('**/api/v1/**', handler)
  }
}
