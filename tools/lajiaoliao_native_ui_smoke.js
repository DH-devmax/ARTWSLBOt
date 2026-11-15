// Synthetic platform-specific settings, grants and message display.
async (page) => {
  const base = 'http://127.0.0.1:5173'
  const id = n => `73000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const actor = id(99), now = new Date().toISOString(), key = 'c'.repeat(64)
  const account = { id: id(1), name: '辣椒受控账号', native_nickname: '原生辣椒',
    workspace: 'lajiaoliao', platform: 'lajiaoliao', purpose: 'management',
    auth_state: 'authenticated', testing_enabled: true, connection_enabled: true, epoch: 1, version: 1 }
  const group = { group_id: id(2), native_id: 'group-string', name: '辣椒隔离群', platform: 'lajiaoliao',
    primary_account_id: account.id, management_enabled: false, ai_enabled: false }
  const grant = { id: id(3), group_id: group.group_id, account_ids: [account.id], target_ids: ['self-string', 'target-string'],
    actions: ['grant_admin', 'revoke_admin', 'member_nickname'], admission_mode: 'native_debug',
    expires_at: new Date(Date.now() + 300000).toISOString(), used: 0, budget: 3 }
  let writes = 0, card = '原群昵称', operation = null, profileWrites = 0, securityWrites = 0
  let nickname = '原生辣椒'
  const content = { platform: 'lajiaoliao', native_sender: 'target-string', native_target: 'group-string',
    native_name: '成员', native_format: '107', message: { sender: { name: '成员' }, target: {}, session: 2 } }
  const snapshot = () => ({ platform: 'lajiaoliao', self_id: 'self-string', self_role: 'owner',
    group_id: 'group-string', account_epoch: 1, account_version: 1, deployment_version: 0,
    digest: 'a'.repeat(64), sampled_at: new Date().toISOString(), in_group: true,
    policy: { muted: false }, mutes_known: true, muted_ids: [],
    members: [{ id: 'self-string', name: '原生辣椒', card, role: 'owner' },
      { id: 'target-string', name: '配合账号', card: '配合账号', role: 'member' }] })
  const handler = async route => {
    const request = route.request(), url = new URL(request.url()), path = url.pathname.replace('/api/v1', '')
    let data = []
    if (path === '/me') data = { user: { id: actor, username: 'chili-owner', role: 'superadmin' },
      permissions: ['overview:read', 'accounts:read', 'groups:read', 'tests:manage'] }
    else if (path === '/workspaces') data = { items: [{ id: 'lajiaoliao', name: '辣椒聊机器人' }] }
    else if (path === '/accounts') data = [account]
    else if (path === '/groups') data = [group]
    else if (path === '/test-windows') data = [grant]
    else if (path === '/test-tools') data = { tools: [{ name: 'run_moderation' }] }
    else if (path === '/test-tools/operations') data = operation ? [operation] : []
    else if (path === '/test-tools/onboarding/scopes') data = { items: [] }
    else if (path.endsWith('/receiver')) data = { enabled: true, observed: true, snapshot: { state: 'connected' } }
    else if (path.endsWith('/dashboard')) data = { account, sampled_at: now, groups: { authorized: 1 },
      operations: { queued: 0, running: 0, unknown: 0 }, inbox: { pending: 0, unresolved: 0, quarantined: 0 },
      performance: { per_second: 0, inserted_60s: 0, series: [] },
      health: { status: 'healthy', reasons: [] }, backlog: { status: 'healthy', reasons: [] } }
    else if (path.endsWith('/native-settings')) data = { snapshot: { ...snapshot(), native_actions_enabled: true,
      nickname_locked: false, settings: { uid: 'self-string', nickname, avatar: '',
        searchable_account: true, searchable_mobile: false, friend_policy: 'FRIEND_REJECT_ALL',
        group_policy: 'GROUP_APPROVAL_JOIN', audio_video: false, masked_mobile: '+86 138****0000',
        profile_details: { gender: 0, birth: 946656000000, signature: '合成签名', region: '合成地区',
          notifications: { showMessageDetail: true, receiveNewMessage: true, groupMessageNotification: true, notificationSound: 'normal', vibration: true } } } },
      security: { avatar: false, password: true, phone: true }, native_ready: false }
    else if (path.endsWith('/native-setting-operations') && request.method() === 'POST') {
      const input = request.postDataJSON()
      if (request.headers()['x-workspace'] !== 'lajiaoliao' || !request.headers()['idempotency-key'] ||
        Object.keys(input.change).sort().join(',') !== 'kind,text')
        throw Error('Profile lost single-field scope')
      if (input.change.kind === 'nickname') nickname = input.change.text
      else throw Error('Unexpected profile write')
      profileWrites++
      data = { operation: {id:id(61),kind:input.change.kind,status:'succeeded',result_code:'native_acknowledged'} }
    }
    else if (path.endsWith('/native-setting-operations')) data = { pending: [] }
    else if (path.includes('/native-security-')) {
      securityWrites++
      throw Error('Retired Chili security controls issued a request')
    }
    else if (path.endsWith('/native-role')) data = { observation: { role: 'owner', observed_at: now } }
    else if (path === '/test-tools/call') {
      if (request.headers()['x-workspace'] !== 'lajiaoliao') throw Error('Chili tool lost platform')
      const { name, arguments: args } = request.postDataJSON()
      if (args.account_id !== account.id) throw Error('Chili tool lost account')
      if (name === 'read_group') data = { snapshot: snapshot(), scope: { remaining: 3 } }
      else if (name === 'run_moderation') {
        if (args.test_window_id !== grant.id || args.action !== 'member_nickname' ||
          args.target_id !== 'self-string' || args.text !== '新群昵称' || !request.headers()['idempotency-key'])
          throw Error('Chili card changed immutable scope')
        writes++; grant.used++; card = args.text
        operation = { id: id(5), kind: 'member_nickname', status: 'succeeded',
          result_code: 'native_acknowledged', test_window_id: grant.id, group_id: group.group_id }
        data = { operation }
      } else throw Error('Unexpected Chili tool')
    } else if (path.endsWith('/conversations')) data = { items: [{ key, scene: 'group', title: '辣椒隔离群',
      business_id: 'group-string', count: 1, last_at: now, last: content }], total: 1,
      directory_total: 1, directory: { sampled_at: now, groups: [{ id: 'group-string', name: '辣椒隔离群' }] } }
    else if (path.endsWith('/messages')) data = { items: [{ id: '1', received_at: now, state: 'unsupported',
      content, native_view: { kind: 'forward', content_type: 107, title: '辣椒聊天记录', items: [
        { kind: 'quote', content_type: 114, text: '<img src=x onerror=alert(1)>',
          quote: { kind: 'image', content_type: 102 } },
        { kind: 'mention', content_type: 106, text: '原生提及', mentions: [{ id: 'target-string', name: '配合账号' }] }
      ] } }], next_before: '' }
    else if (path.endsWith('/sending')) data = { enabled: false, reason: 'native_text_acceptance_pending', recent: [], test_windows: [] }
    else if (path.endsWith('/decoder-health')) data = { status: 'degraded', total: 1, unsupported: 1, reasons: {},
      pool: { workers: 4, queue_capacity: 16 }, receiver: { snapshot: { state: 'connected' } } }
    else if (path.endsWith('/events')) return route.fulfill({ contentType: 'text/event-stream', body: '' })
    else if (request.method() !== 'GET') throw Error('Unexpected mutation: ' + path)
    await route.fulfill({ json: { code: 0, msg: '', data } })
  }
  await page.route('**/api/v1/**', handler)
  try {
    await page.evaluate(({ actor, account }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:workspace:${actor}`, 'lajiaoliao')
      sessionStorage.setItem(`wsl:account:${actor}:lajiaoliao`, account)
    }, { actor, account: account.id })
    await page.goto(base + '/#overview')
    await page.reload()
    await page.getByRole('button', { name: '账号资料', exact: true }).click()
    const settings = page.locator('.native-settings-dialog')
    if (profileWrites || securityWrites) throw Error('Opening settings submitted a mutation')
    // The current product keeps nickname only on Chili. Retired signature,
    // privacy and SMS paths are replaced by absence/no-request assertions;
    // nickname CAS, explicit confirmation, string-ID tools and XSS stay covered.
    for (const name of ['隐私与互动', '账号安全', '头像', '头像与安全'])
      if (await settings.getByRole('tab', { name, exact: true }).count())
        throw Error('Retired Chili settings tab exposed: ' + name)
    if (await settings.getByRole('textbox', { name: '个性签名', exact: true }).count())
      throw Error('Retired profile editor remains visible')
    await settings.getByRole('textbox', {name:'原生昵称',exact:true}).fill('新辣椒昵称')
    await settings.getByRole('button',{name:'保存昵称',exact:true}).click()
    await page.getByRole('button',{name:'取消',exact:true}).click()
    if (profileWrites || securityWrites) throw Error('Cancelled nickname confirmation submitted')
    await settings.getByRole('button',{name:'保存昵称',exact:true}).click()
    await page.getByRole('button',{name:'确认修改',exact:true}).click()
    await page.getByText('辣椒聊已确认保存；其他客户端显示可能稍有延迟',{exact:true}).waitFor()
    if (profileWrites !== 1 || securityWrites) throw Error('Profile write replayed or triggered SMS')
    if (await settings.getByText('旧密码',{exact:true}).count() || await settings.getByText('上传并保存头像',{exact:true}).count())
      throw Error('WSL security form leaked into Chili')
    if (securityWrites || await settings.getByRole('button',{name:'验证并发送原手机验证码',exact:true}).count())
      throw Error('Legacy security flags exposed SMS')
    for (const width of [320,390,768,1440]) {
      await page.setViewportSize({width,height:900})
      await page.screenshot({path:`output/playwright/chili-settings-${width}.png`,fullPage:true})
      if (!await settings.evaluate(el => el.scrollWidth <= el.clientWidth+1)) throw Error('Settings overflow '+width)
    }
    await page.setViewportSize({width:1280,height:900})
    await settings.getByRole('button', { name: '关闭', exact: true }).click()
    await page.goto(base + '/#tests')
    await page.locator('.tools-grant').click()
    await page.getByRole('button', { name: '读取当前群状态', exact: true }).click()
    const choose = async (name, option) => {
      await page.getByRole('combobox', { name, exact: true })
        .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
      await page.getByRole('option', { name: option, exact: true }).click()
    }
    await choose('验证动作', '修改群内昵称')
    await choose('授权目标成员', '原生辣椒 · self-string')
    if (await page.getByRole('textbox', { name: '群内昵称', exact: true }).getAttribute('maxlength') !== '15')
      throw Error('Chili card lost its native length limit')
    await page.getByRole('textbox', { name: '群内昵称', exact: true }).fill('新群昵称')
    await page.getByRole('button', { name: '确认验证此动作', exact: true }).click()
    await page.getByRole('button', { name: '确认提交', exact: true }).click()
    await page.getByText('上游已确认操作', { exact: true }).waitFor()
    if (writes !== 1) throw Error('Card action replayed')
    await page.goto(base + '/#messages')
    await page.getByRole('button', { name: /辣椒隔离群/ }).first().click()
    await page.locator('.chili-message > details > summary').click()
    await page.getByText('原生提及', { exact: true }).waitFor()
    if (await page.locator('.chili-message img').count()) throw Error('Native text became HTML')
    for (const width of [320, 390, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await page.screenshot({ path: `output/playwright/chili-native-${width}.png`, fullPage: true })
      if (!await page.locator('.message-center').evaluate(el => el.scrollWidth <= el.clientWidth + 1))
        throw Error('Chili message layout overflow: ' + width)
    }
    return { passed: true, synthetic_submissions: writes, profile_submissions:profileWrites, security_submissions:securityWrites, viewports: 4,
      retired_settings_hidden: true, string_id_card: true, native_requests: 0 }
  } finally { await page.unroute('**/api/v1/**', handler) }
}
