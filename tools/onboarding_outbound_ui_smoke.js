// Synthetic contract fixtures only. Run with the intercepted UI runner, never a live API.
// prettier-ignore
async (page) => {
  page.setDefaultTimeout(10000)
  await page.clock.install()
  const base = new URL(page.url()).origin
  if (base !== 'http://127.0.0.1:5173') throw Error('Use the intercepted synthetic UI runner')
  const id = n => `f6300000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const actor = id(1), ownTenant = id(2), customerTenant = id(3)
  const platforms = ['wsl', 'lajiaoliao'], viewports = [320, 390, 768, 1440]
  const name = value => value === 'wsl' ? '旺商聊' : '辣椒聊'
  const privateProof = value => `群接入 ${(value === 'wsl' ? 'p' : 'q').repeat(43)}`
  const ownSpace = { id: ownTenant, name: '我的演示工作空间', kind: 'operator_demo', owner_id: actor }
  const customerSpace = { id: customerTenant, name: '合成客户', kind: 'tenant_business', owner_id: id(4) }
  const plan = { id: 'operator_unlimited', code: 'SUPERADMIN', name: '超管专属 · 不限额',
    system_only: true, saleable: false, assignable: false, unlimited: true, permanent: true,
    group_limit: null, member_limit: null, assistant_limit: null, credit_limit: null,
    price_micro_usdt: null, credits_micro: null, platforms: [] }
  let serial = 100, latest, role = 'operator', variant = 'normal', expiryDeadline, expiryState = 'waiting'
  let holdPrepare, releasePrepare, startedPrepare, backgroundActual = 25000000
  let lookupFailure, sendFailure
  const records = new Map(), reads = [], writes = [], screenshots = [], captchaRequests = [], consumed = []
  const captchaChallenges = new Map(), captchaTickets = new Map()
  const operator = () => role === 'operator'
  const grants = () => ['overview:read', 'groups:read', 'operations:read', 'assistant:use',
    ...(role === 'member' ? [] : ['claims:create']),
    ...(operator() ? ['users:manage', 'plans:manage'] : [])]
  const check = (value, message) => { if (!value) throw Error(message) }
  const fields = (body, names) => Object.keys(body).sort().join() === [...names].sort().join()
  const copy = value => JSON.parse(JSON.stringify(value))
  const lookupSecret = 'SYNTHETIC_LOOKUP_PRIVATE_METADATA'
  const lookupAttack = `<img src="data:invalid" onerror="document.documentElement.dataset.lookupLeak=1">${lookupSecret}`
  const lookupData = overrides => ({ lookup_diagnostic: {
    version: 1, platform: 'wsl', stage: 'search', category: 'upstream_rejected',
    provider_code: -1, reason: 'provider_reason_empty', observed_at: new Date().toISOString(),
    ...overrides
  } })
  const waitPrepare = async () => {
    if (!holdPrepare) return
    holdPrepare = false; startedPrepare()
    await new Promise(resolve => { releasePrepare = resolve })
  }
  const progress = (record, firstIssued = false) => {
    const { privateSecret, ...value } = record
    return { ...copy(value), ...(firstIssued ? { private_challenge: privateSecret,
      group_challenge: '群绑定 SYNTHETIC_GROUP_PROOF' } : {}),
      // Deliberately present on legacy responses: the UI must never render it.
      friend_challenge: '群接入 AAAAAAAAAAAAAAAAAAAA' }
  }
  const createOutbound = (record, status = 'prepared') => ({
    id: id(++serial), version: 1, status,
    code: status === 'friends' ? 'native_friend_relationship_observed' : '', can_send: status === 'prepared',
    can_reconcile: ['submitted', 'submitting', 'unknown'].includes(status),
    target: { native_id: `INTERNAL_TARGET_${record.platform}`, add_account: `self-${record.platform}`,
      name: `${name(record.platform)}本人`, already_friend: status === 'friends' }
  })
  const png = (width, height) => {
    const crc = bytes => {
      let value = 0xffffffff
      for (const byte of bytes) {
        value ^= byte
        for (let n = 0; n < 8; n++) value = (value >>> 1) ^ ((value & 1) ? 0xedb88320 : 0)
      }
      return (value ^ 0xffffffff) >>> 0
    }
    const chunk = (type, data) => {
      const tag = Buffer.from(type), length = Buffer.alloc(4), checksum = Buffer.alloc(4)
      length.writeUInt32BE(data.length); checksum.writeUInt32BE(crc(Buffer.concat([tag, data])))
      return Buffer.concat([length, tag, data, checksum])
    }
    const header = Buffer.alloc(13), pixels = Buffer.alloc((width * 4 + 1) * height)
    header.writeUInt32BE(width); header.writeUInt32BE(height, 4); header[8] = 8; header[9] = 6
    for (let y = 0; y < height; y++) for (let x = 0; x < width; x++)
      pixels.set([64 + x % 100, 100 + y % 100, 160, 255], y * (width * 4 + 1) + 1 + x * 4)
    return 'data:image/png;base64,' + Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
      chunk('IHDR', header), chunk('IDAT', process.getBuiltinModule('zlib').deflateSync(pixels)),
      chunk('IEND', Buffer.alloc(0))]).toString('base64')
  }
  const background = png(300, 220), tile = png(44, 44)
  const captchaScope = request => {
    const body = request.postDataJSON(), platform = request.headers()['x-workspace']
    check(fields(body, ['purpose', 'platform', ...(body.target ? ['target'] : []),
      ...(body.previous_id ? ['previous_id'] : []),
      ...(body.challenge_id ? ['challenge_id', 'x', 'y'] : [])]), 'Unexpected captcha fields')
    check(['group_binding_create', 'group_binding_friend_send'].includes(body.purpose) &&
      platforms.includes(platform) && body.platform === platform, 'Captcha lost its new purpose or platform')
    check(body.purpose === 'group_binding_friend_send' ? records.has(body.target) : !body.target,
      'Captcha lost its exact challenge target')
    return { purpose: body.purpose, platform, ...(body.target ? { target: body.target } : {}) }
  }
  const consume = (request, expected) => {
    const token = request.headers()['x-human-verification'], ticket = captchaTickets.get(token)
    check(ticket && !ticket.used && JSON.stringify(ticket.scope) === JSON.stringify(expected),
      'Submission omitted, reused or crossed its captcha ticket')
    ticket.used = true; consumed.push({ ...expected, token })
  }
  const handler = async route => {
    const req = route.request(), url = new URL(req.url()), path = url.pathname.replace('/api/v1', '')
    const platform = req.headers()['x-workspace'] || 'wsl'
    // Fixture deadlines share the browser clock advanced by page.clock.
    const now = await page.evaluate(() => Date.now())
    let data = [], status = 200
    if (/^\/human-verification\/(?:challenge|verify)$/.test(path)) {
      const body = req.postDataJSON(), scope = captchaScope(req)
      captchaRequests.push({ path, scope })
      if (path.endsWith('/challenge')) {
        const value = { challenge_id: id(++serial), image: { background, tile, width: 300, height: 220,
          tile_width: 44, tile_height: 44, initial_x: 128, initial_y: 80 },
          expires_at: new Date(now + 120000).toISOString() }
        captchaChallenges.set(value.challenge_id, scope); data = value
      } else {
        check(JSON.stringify(captchaChallenges.get(body.challenge_id)) === JSON.stringify(scope) &&
          body.x === 148 && body.y === 80, 'Captcha verify crossed scope or fixture position')
        captchaChallenges.delete(body.challenge_id)
        const token = `SYNTHETIC_${String(++serial).padStart(33, '0')}`
        captchaTickets.set(token, { scope, used: false })
        data = { ticket: token, expires_at: new Date(now + 60000).toISOString() }
      }
    } else if (req.method() === 'GET') {
      reads.push({ path, platform })
      if (path === '/me') data = { user: { id: actor, username: 'synthetic-friend',
        role: operator() ? 'superadmin' : 'user',
        ...(!operator() ? { tenant_id: customerTenant, tenant_role: role === 'member' ? 'viewer' : 'owner' } : {}) },
        permissions: grants() }
      else if (path === '/workspaces') data = { items: platforms.map(id => ({ id, name: `${name(id)}机器人` })) }
      else if (path === '/user-center') data = {
        workspace: operator() ? ownSpace : customerSpace, effective_plan: operator() ? plan :
          { id: 'pro', name: 'PRO', active: role !== 'no_plan', platforms, modules: ['management_assistant'] },
        entitlement: { mode: operator() ? 'operator_demo' : 'tenant_business', name: operator() ? plan.name : 'PRO',
          unlimited: operator(), active: operator() || role !== 'no_plan', permissions: grants() },
        subscription: operator() || role === 'no_plan' ? null : { name: 'PRO', active: true, platforms,
          group_limit: 8, member_limit: 4, modules: ['management_assistant'],
          expires_at: new Date(now + 600000).toISOString() },
        quotas: { groups: operator() ? null : 8, members: operator() ? null : 4, assistant: null, credits: null },
        active_group_count: 0, billing_source: operator() ? 'platform_demo' : 'tenant_wallet',
        permissions: grants(), groups: [], claims: [], operations: [], personas: []
      }
      else if (path === '/user-center/groups') {
        check(url.searchParams.get('page_size') === '20' && url.searchParams.get('platform') === platform,
          'Group read lost bounded pagination or platform')
        data = { workspace: operator() ? ownSpace : customerSpace, items: [], total: 0, page_size: 20,
          customers: [{ tenant_id: customerTenant, name: customerSpace.name }] }
      }
      else if (path === '/user-center/usage') data = {
        workspace: operator() ? ownSpace : customerSpace, effective_plan: operator() ? plan : null,
        billing_source: operator() ? 'platform_demo' : 'tenant_wallet', actual_micro: 12500, held_micro: 1000,
        call_count: 3, settled_call_count: 2, pending_call_count: 1, currency: 'USDT', currency_scale: 1000000,
        sampled_at: new Date().toISOString(),
        background_usage: operator() ? { tenant_id: ownTenant, execution_mode: 'workspace_demo',
          billing_source: 'platform_workspace', call_count: 11, settled_call_count: 8, pending_call_count: 3,
          actual_micro: backgroundActual, held_micro: 7500000, currency: 'USDT', currency_scale: 1000000000,
          unlimited: true, limit: null } : null
      }
      else if (path === '/group-binding/context') data = {
        mode: operator() ? 'operator_demo' : 'tenant_business', workspace: operator() ? ownSpace : customerSpace,
        default_target_tenant_id: operator() ? ownTenant : customerTenant, can_onboard_for_customer: operator(),
        customers: operator() ? [{ tenant_id: customerTenant, name: customerSpace.name }] : [], next_offset: -1
      }
      else if (/^\/group-binding\/challenges\/[^/]+$/.test(path)) {
        const record = records.get(path.split('/')[3])
        check(record && record.platform === platform, 'Progress crossed a challenge or platform')
        data = progress(record)
      } else if (/^\/groups\/[^/]+\/(?:business-runtime|capabilities)$/.test(path))
        data = { group_id: path.split('/')[2], version: 1, enabled: false, management_enabled: false,
          can_manage: true, permissions: [], actions: [], runtime_state: 'disabled', sampled_at: new Date().toISOString() }
      else if (path === '/users') data = [{ id: id(4), tenant_id: customerTenant,
        tenant_name: customerSpace.name, username: 'customer-owner', tenant_role: 'owner' }]
      else if (path === '/overview') data = { accounts: 0, groups: 0 }
      else if (path === '/billing' || path === '/billing/plans')
        throw Error('User fees escaped the existing user-center API contract')
    } else {
      check(req.method() === 'POST', 'Unexpected mutation method')
      const body = req.postDataJSON() || {}, key = req.headers()['idempotency-key']
      writes.push({ path, platform, body, key })
      if (path === '/group-binding/challenges') {
        consume(req, { purpose: 'group_binding_create', platform })
        check(key && body.platform === platform &&
          fields(body, operator() ? ['platform', 'target_tenant_id'] : ['platform']), 'Creation lost trusted scope/key')
        const target = operator() ? body.target_tenant_id : customerTenant
        check([ownTenant, customerTenant].includes(target), 'Creation lost explicit resource ownership')
        latest = { id: id(++serial), platform, mode: operator() ? 'operator_demo' : 'tenant_business',
          workspace: target === ownTenant ? ownSpace : customerSpace,
          customer: { tenant_id: target, name: target === ownTenant ? ownSpace.name : customerSpace.name },
          friend_flow: variant.startsWith('legacy') ? undefined : 'outbound',
          state: expiryState, version: 1, expires_at: expiryDeadline || new Date(now + 600000).toISOString(),
          robot: { id: id(30 + platforms.indexOf(platform)), name: `${name(platform)}接入机器人`,
            native_id: `INTERNAL_NOT_SEARCHABLE_${platform}`, add_account: `public-${platform}-robot`,
            add_available: true },
          privateSecret: variant === 'private_missing' ? '' : privateProof(platform),
          identity_proven: false, can_confirm: expiryState === 'proven',
          can_cancel: ['waiting', 'proven'].includes(expiryState),
          can_accept_invitation: false }
        if (['proven', 'bound', 'review'].includes(expiryState)) {
          latest.proof_role = 'owner'; latest.group = { id: id(50), name: '合成接入群', native_id: 'group-public-id' }
        }
        if (variant.startsWith('legacy')) latest.friend_attempt = { id: id(++serial),
          status: variant === 'legacy_unknown' ? 'unknown' : 'succeeded', result_code: '' }
        if (variant === 'expiry_unknown') latest.friend_outbound = createOutbound(latest, 'unknown')
        records.set(latest.id, latest); data = progress(latest, true); status = 201
      } else {
        const record = records.get(path.split('/')[3])
        check(record && record.platform === platform, 'Mutation crossed a challenge or platform')
        if (path.endsWith('/friend-target')) {
          check(key && fields(body, ['version', 'account']) && body.version === record.version &&
            body.account === `self-${platform}` && !record.friend_outbound, 'Lookup supplied trusted identity or replayed')
          if (lookupFailure) {
            const failure = copy(lookupFailure)
            await waitPrepare()
            return route.fulfill({ status: 409, json: {
              code: 1, msg: 'group_binding_outbound_rejected', ...failure
            } })
          }
          if (variant === 'not_found')
            return route.fulfill({ status: 404, json: { code: 1, msg: 'group_binding_outbound_target_not_found' } })
          if (variant === 'lookup_rejected')
            return route.fulfill({ status: 409, json: { code: 1, msg: 'group_binding_outbound_rejected' } })
          if (['target_inconsistent', 'read_incomplete'].includes(variant))
            return route.fulfill({ status: 409, json: { code: 1, msg: `group_binding_outbound_${variant}` } })
          record.friend_outbound = createOutbound(record, variant === 'already_friend' ? 'friends' : 'prepared')
          if (variant === 'closed') {
            record.friend_outbound.can_send = false
            record.friend_outbound.send_blocked_reason = 'group_binding_outbound_admission_closed'
          }
          data = progress(record)
          await waitPrepare()
        } else if (path.endsWith('/friend-send')) {
          consume(req, { purpose: 'group_binding_friend_send', platform, target: record.id })
          check(key && fields(body, ['version']) && body.version === record.version &&
            record.friend_outbound?.status === 'prepared' && record.friend_outbound.can_send,
            'Send changed target, lost version/key or replayed')
          if (sendFailure)
            return route.fulfill({ status: 409, json: {
              code: 1, msg: 'group_binding_outbound_rejected', data: copy(sendFailure)
            } })
          record.friend_outbound = { ...record.friend_outbound, status:
            ['unknown', 'lost'].includes(variant) ? 'unknown' : variant === 'submitting' ? 'submitting' :
              variant === 'failed' ? 'failed' :
              variant === 'lost_prepared' ? 'prepared' : 'submitted',
            can_send: variant === 'lost_prepared', can_reconcile: variant !== 'failed',
            code: variant === 'normal' ? 'native_friend_request_acknowledged' : '' }
          data = progress(record)
          if (['lost', 'lost_prepared'].includes(variant)) return route.abort('failed')
        } else if (/\/outbound-attempts\/[^/]+\/reconcile$/.test(path)) {
          check(key && fields(body, ['version']) && body.version === record.version &&
            record.friend_outbound?.id === path.split('/')[5] && record.friend_outbound.can_reconcile,
            'Reconciliation changed the original operation/version/key')
          record.friend_outbound = { ...record.friend_outbound, status: 'friends', can_send: false,
            can_reconcile: false, code: 'native_friend_relationship_observed',
            target: { ...record.friend_outbound.target, already_friend: true } }
          data = progress(record)
        } else if (/\/friend-attempts\/[^/]+\/reconcile$/.test(path)) {
          check(record.friend_attempt?.id === path.split('/')[5] && fields(body, []),
            'Legacy reconciliation changed its original operation')
          record.friend_attempt = { ...record.friend_attempt, status: 'succeeded' }; data = progress(record)
        } else if (path.endsWith('/invitation-prepare')) {
          check(record.identity_proven && record.can_accept_invitation &&
            fields(body, ['version', 'native_group_id']) && body.version === record.version &&
            body.native_group_id === 'group-public-id', 'Invitation bypassed private identity proof or scope')
          data = { version: record.version, native_group_id: body.native_group_id,
            invitation_id: 'synthetic-invitation', group_name: '合成接入群', expires_at: record.expires_at }
        } else if (path.endsWith('/invitation-accept')) {
          check(key && record.identity_proven && record.can_accept_invitation &&
            fields(body, ['version', 'native_group_id', 'invitation_id']) &&
            body.version === record.version && body.native_group_id === 'group-public-id' &&
            body.invitation_id === 'synthetic-invitation', 'Invitation changed the confirmed scope')
          record.can_accept_invitation = false
          record.accept_attempt = { id: id(++serial), status: 'succeeded', result_code: '' }
          data = copy(record.accept_attempt)
        } else if (path.endsWith('/confirm')) {
          check(key && fields(body, ['version']) && body.version === record.version && record.can_confirm,
            'Confirmation lost proof/version/key')
          record.state = 'bound'; record.can_confirm = record.can_cancel = false; record.version++
          data = progress(record)
        } else if (path.endsWith('/cancel')) {
          record.state = 'cancelled'; record.can_cancel = false; record.version++; data = progress(record)
        } else throw Error('Unexpected native/model/legacy-approval operation: ' + path)
      }
    }
    try { await route.fulfill({ status, json: { code: 0, msg: '', data } }) }
    catch (error) { if (!/closed|disposed|cancel|intercept|target/i.test(String(error))) throw error }
  }
  const modal = () => page.getByRole('dialog', { name: operator() ? '接入群组' : '接入我的群组', exact: true })
  const navigate = async (platform = 'wsl', view = 'groups') => {
    await page.evaluate(({ actor, platform }) => {
      sessionStorage.clear(); sessionStorage.setItem(`wsl:console:${actor}`, 'user')
      sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
    }, { actor, platform })
    await page.goto(`${base}/#${view}`); await page.reload()
    await page.getByRole('button', { name: view === 'billing' ? '刷新实际用量' : '刷新用户中心', exact: true }).waitFor()
  }
  const close = async () => {
    await modal().getByRole('button', { name: /^(关闭|完成)$/ }).click()
    await modal().waitFor({ state: 'hidden' })
  }
  const open = async () => {
    await page.getByRole('button', { name: '接入群组', exact: true }).click()
    await modal().getByRole('button', { name: '开始核验', exact: true }).waitFor()
  }
  const captchaDialog = () => page.getByRole('dialog', { name: '安全验证', exact: true })
  const friendConfirmation = () => page.getByRole('dialog', { name: '确认发送好友申请', exact: true })
  const confirmFriend = async () => {
    await modal().getByRole('button', { name: '让机器人添加我', exact: true }).click()
    await friendConfirmation().getByRole('button', { name: '确认发送申请', exact: true }).click()
  }
  const completeCaptcha = async () => {
    const verify = captchaDialog().getByRole('button', { name: '验证', exact: true })
    await verify.and(page.locator(':enabled')).waitFor()
    const slider = captchaDialog().getByRole('slider', { name: '拼图位置', exact: true })
    await slider.focus()
    for (let step = 128; step < 148; step++) await slider.press('ArrowRight')
    await slider.press('Enter'); await captchaDialog().waitFor({ state: 'hidden' })
  }
  const begin = async () => {
    await open(); await modal().getByRole('button', { name: '开始核验', exact: true }).click()
    await completeCaptcha(); await modal().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
  }
  const refresh = async () => {
    await modal().getByRole('button', { name: '刷新进度', exact: true }).click()
    await page.waitForFunction(() => !document.querySelector('.onboarding-dialog .el-dialog__footer .is-loading'))
  }
  const selectPrivate = async () => {
    await modal().locator('.onboarding-friend-paths label').filter({ hasText: '已是好友' }).click()
    check(await modal().getByRole('radio', { name: '已是好友', exact: true }).isChecked(),
      'Private proof branch was not selected')
    await modal().getByRole('heading', { name: '私聊核验本人身份', exact: true }).waitFor()
  }
  const prepare = async platform => {
    await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).fill(`self-${platform}`)
    await modal().getByRole('button', { name: '查找我的账号', exact: true }).click()
    await modal().getByText('账号已核对，尚未发送', { exact: true }).waitFor()
  }
  const assertNoOldFlow = async () => {
    check(!await modal().getByRole('button', { name: /已发送，开始核验|复制好友核验内容/ }).count() &&
      !await modal().getByText('群接入 AAAAAAAAAAAAAAAAAAAA', { exact: true }).count() &&
      !await modal().getByRole('radio', { name: '发送好友申请', exact: true }).count(),
      'Old incoming application flow is still actionable')
    check(!await modal().getByText('公开加好友账号', { exact: true }).count() &&
      !await modal().getByRole('button', { name: '复制公开加好友账号', exact: true }).count() &&
      !await modal().getByText(`public-${latest.platform}-robot`, { exact: true }).count(),
      'Unified outbound onboarding still exposes the unnecessary robot public account')
  }
  const assertIdentityPending = async () => {
    check(!await modal().getByText('好友身份已核验，可邀请机器人入群', { exact: true }).count() &&
      !await modal().getByRole('button', { name: /^(确认绑定|提交审核|核对群邀请)$/ }).count() &&
      await modal().locator('.onboarding-steps .is-current strong').innerText() === '好友核验',
      'Friend request or relationship became website identity/group proof')
  }
  const checkStorage = async () => {
    check(!await page.evaluate(() => [...Object.values(sessionStorage), ...Object.values(localStorage)].some(value =>
      /群接入 |SYNTHETIC_GROUP_PROOF|INTERNAL_TARGET_|self-wsl|self-lajiaoliao|SYNTHETIC_LOOKUP_PRIVATE_METADATA|lookup_diagnostic|provider_reason_/.test(value))),
      'Proof text or native identity escaped into persisted drafts')
  }
  const fit = async (platform, width, branch) => {
    await page.setViewportSize({ width, height: 820 })
    const content = branch.startsWith('lookup-') ? modal().locator('.onboarding-error') :
      modal().getByRole('region', { name: '好友接入核验', exact: true })
    await content.evaluate(element => element.scrollIntoView({ block: 'start' }))
    const box = await modal().boundingBox()
    const footer = await modal().getByRole('button', { name: '刷新进度', exact: true }).boundingBox()
    check(box && box.x >= -1 && box.x + box.width <= width + 1 &&
      footer && footer.y >= 0 && footer.y + footer.height <= 821 &&
      !await modal().evaluate(el => el.scrollWidth > el.clientWidth + 1) &&
      !await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1),
      'Onboarding overflow at ' + width)
    if (branch.startsWith('lookup-'))
      check(await content.evaluate(element => {
        const rect = element.getBoundingClientRect()
        return rect.left >= 0 && rect.right <= innerWidth &&
          element.scrollWidth <= element.clientWidth + 1 &&
          [...element.querySelectorAll('.el-alert__title, .el-alert__description')].every(item =>
            item.scrollWidth <= item.clientWidth + 1)
      }), 'Lookup diagnosis overflow at ' + width)
    const file = `output/playwright/onboarding-outbound-${platform}-${branch}-${width}.png`
    await page.screenshot({ path: file, animations: 'disabled' }); screenshots.push(file)
  }
  await page.route('**/api/v1/**', handler)
  try {
    await page.setViewportSize({ width: 1440, height: 960 })
    for (const platform of platforms) {
      variant = 'normal'; await navigate(platform)
      const beforeOpen = writes.length
      await open(); await close()
      check(writes.length === beforeOpen, 'Opening/closing submitted onboarding work')
      await begin(); await assertNoOldFlow()
      check(writes.at(-1).body.target_tenant_id === ownTenant, 'Own onboarding targeted a customer')
      await modal().getByText(`${name(platform)}接入机器人`, { exact: true }).waitFor()
      check(!await modal().getByText(`INTERNAL_NOT_SEARCHABLE_${platform}`, { exact: true }).count(),
        'Internal robot identity became its searchable account')
      const beforeLookup = writes.length, beforeCaptcha = captchaRequests.length
      await prepare(platform)
      check(writes.length === beforeLookup + 1 && writes.at(-1).path.endsWith('/friend-target') &&
        captchaRequests.length === beforeCaptcha &&
        !writes.slice(beforeLookup).some(write => write.path.endsWith('/friend-send')), 'Lookup sent an application or opened captcha')
      await modal().getByText(`self-${platform}`, { exact: true }).waitFor()
      await modal().getByText(`${name(platform)}本人`, { exact: true }).waitFor()
      await assertIdentityPending()
      for (const width of viewports) await fit(platform, width, 'target')
      const send = () => modal().getByRole('button', { name: '让机器人添加我', exact: true })
      const beforeConfirmationCaptcha = captchaRequests.length
      await send().click()
      await friendConfirmation().getByText(`self-${platform}`, { exact: true }).waitFor()
      await friendConfirmation().getByText(`${name(platform)}本人`, { exact: true }).waitFor()
      check(writes.length === beforeLookup + 1 && captchaRequests.length === beforeConfirmationCaptcha,
        'Opening human confirmation sent an application or started captcha')
      for (const width of viewports) {
        await page.setViewportSize({ width, height: 820 })
        const box = await friendConfirmation().boundingBox()
        const button = await friendConfirmation().getByRole('button', { name: '确认发送申请', exact: true }).boundingBox()
        check(box && box.x >= -1 && box.x + box.width <= width + 1 &&
          button && button.y >= 0 && button.y + button.height <= 821 &&
          !await friendConfirmation().evaluate(el => el.scrollWidth > el.clientWidth + 1),
          'Friend confirmation overflow at ' + width)
        const file = `output/playwright/onboarding-outbound-${platform}-confirm-${width}.png`
        await page.screenshot({ path: file, animations: 'disabled' }); screenshots.push(file)
      }
      await friendConfirmation().getByRole('button', { name: '取消', exact: true }).click()
      await friendConfirmation().waitFor({ state: 'hidden' })
      check(writes.length === beforeLookup + 1 && captchaRequests.length === beforeConfirmationCaptcha,
        'Cancelling human confirmation sent an application or started captcha')
      await confirmFriend(); await captchaDialog().waitFor()
      check(writes.length === beforeLookup + 1, 'Application submitted before human verification')
      await captchaDialog().getByRole('button', { name: '取消', exact: true }).click()
      await captchaDialog().waitFor({ state: 'hidden' })
      check(writes.length === beforeLookup + 1, 'Cancelled captcha submitted an application')
      const beforeDoubleClick = captchaRequests.length
      await send().click()
      const confirmSend = friendConfirmation().getByRole('button', { name: '确认发送申请', exact: true })
      await confirmSend.evaluate(button => { button.click(); button.click() })
      await completeCaptcha()
      check(captchaRequests.length === beforeDoubleClick + 2, 'Double click started more than one captcha')
      await modal().getByText('好友申请已提交', { exact: true }).waitFor()
      check(writes.length === beforeLookup + 2 && writes.at(-1).path.endsWith('/friend-send') &&
        consumed.at(-1).purpose === 'group_binding_friend_send' &&
        consumed.at(-1).target === latest.id && !await send().count(), 'Send was not exactly once and target-bound')
      await modal().getByText(privateProof(platform), { exact: true }).waitFor()
      await assertIdentityPending()
      const beforeRefresh = writes.length
      await refresh()
      await modal().getByText('好友申请已提交', { exact: true }).waitFor()
      check(writes.length === beforeRefresh && !await send().count(), 'Refresh sent another application')
      await modal().getByRole('button', { name: '核对原好友申请', exact: true }).click()
      await modal().getByRole('heading', { name: '私聊核验本人身份', exact: true }).waitFor()
      await modal().getByText(privateProof(platform), { exact: true }).waitFor()
      check(await modal().getByRole('button', { name: '复制身份核验内容', exact: true }).count() === 1,
        'Continuous onboarding lost its first-issued private proof')
      await assertIdentityPending()
      latest.identity_proven = true; latest.can_accept_invitation = true; await refresh()
      await modal().getByText('好友身份已核验，可邀请机器人入群', { exact: true }).waitFor()
      await modal().getByRole('textbox', { name: '目标群原生标识', exact: true }).fill('group-public-id')
      await modal().getByRole('button', { name: '核对群邀请', exact: true }).click()
      await modal().getByRole('button', { name: '接受此群邀请', exact: true }).click()
      const invitation = page.getByRole('dialog', { name: '确认接受群邀请', exact: true })
      await invitation.getByRole('button', { name: '确认接受邀请', exact: true }).click()
      await modal().getByText('已接受', { exact: true }).waitFor()
      await modal().getByText('群绑定 SYNTHETIC_GROUP_PROOF', { exact: true }).waitFor()
      check(await modal().getByRole('button', { name: '复制群归属核验内容', exact: true }).count() === 1,
        'Continuous onboarding lost its first-issued group proof')
      check(latest.state === 'waiting' && !latest.can_confirm,
        'Accepting an invitation bypassed independent group proof')
      latest.state = 'proven'; latest.can_confirm = true; latest.proof_role = 'owner'; latest.version++
      latest.group = { id: id(50), name: '合成接入群', native_id: 'group-public-id' }; await refresh()
      await modal().getByRole('button', { name: '确认绑定', exact: true }).click()
      const confirmation = page.getByRole('dialog', { name: '确认群归属', exact: true })
      await confirmation.getByText(/业务运行保持关闭/).waitFor()
      await confirmation.getByRole('button', { name: '确认绑定', exact: true }).click()
      await modal().getByText('绑定成功', { exact: true }).waitFor()
      await modal().getByText(/群组已绑定，业务运行保持关闭/).waitFor()
      await close(); await checkStorage()
    }
    for (const platform of platforms) {
      variant = 'already_friend'; await navigate(platform); await begin()
      const before = writes.length
      await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).fill(`self-${platform}`)
      await modal().getByRole('button', { name: '查找我的账号', exact: true }).click()
      await modal().getByRole('heading', { name: '私聊核验本人身份', exact: true }).waitFor()
      await modal().getByText(privateProof(platform), { exact: true }).waitFor()
      check(writes.length === before + 1 && !await modal().getByRole('button', { name: '让机器人添加我', exact: true }).count(),
        'Already-friend lookup offered or sent a duplicate application')
      await assertIdentityPending(); await close()
      variant = 'normal'; await navigate(platform); await begin()
      const beforePrivate = writes.length; await selectPrivate(); await refresh()
      await modal().getByText(privateProof(platform), { exact: true }).waitFor()
      check(writes.length === beforePrivate, 'Existing-friend path submitted a request')
      for (const width of viewports) await fit(platform, width, 'private')
      await close()
    }
    for (const platform of platforms) {
      variant = 'normal'; await navigate(platform); await begin(); await prepare(platform)
      await close()
      const before = writes.length, beforeCaptcha = captchaRequests.length
      await page.getByRole('button', { name: '接入群组', exact: true }).click()
      await modal().getByText('账号已核对，尚未发送', { exact: true }).waitFor()
      await modal().getByText(
        '本次身份或群归属核验内容未保留。请取消本次接入，重新开始核验后再申请好友。',
        { exact: true }).waitFor()
      check(await modal().getByRole('button', { name: '让机器人添加我', exact: true }).isDisabled() &&
        !await modal().getByRole('button', { name: /复制身份核验内容|复制群归属核验内容/ }).count() &&
        writes.length === before && captchaRequests.length === beforeCaptcha,
        'Reopening a prepared request lost proof text but still allowed sending')
      await close()
    }
    await page.setViewportSize({ width: 1440, height: 960 })
    for (const value of ['submitted', 'submitting', 'unknown', 'lost', 'lost_prepared', 'failed']) {
      variant = value; await navigate(); await begin(); await prepare('wsl')
      await confirmFriend()
      await completeCaptcha()
      await modal().getByText(value.startsWith('lost') ?
        '好友申请提交结果待确认，请刷新原记录核对，不要重新提交。' :
        value === 'failed' ? '好友申请未完成' : value === 'submitted' ? '好友申请已提交' :
          value === 'submitting' ? '好友申请提交中' :
          '好友申请结果待核对', { exact: true }).waitFor()
      const before = writes.length
      await refresh(); await close(); await page.reload()
      await page.getByRole('button', { name: '接入群组', exact: true }).click()
      await modal().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
      check(!await modal().getByRole('button', { name: '让机器人添加我', exact: true }).count() &&
        writes.length === before, 'Unknown or failed send became replayable after reload')
      await assertIdentityPending()
      if (value !== 'failed') {
        await modal().getByRole('button', { name: '核对原好友申请', exact: true }).click()
        await modal().getByRole('heading', { name: '私聊核验本人身份', exact: true }).waitFor()
        check(writes.length === before + 1 && writes.at(-1).path.endsWith('/reconcile'),
          'Reconciliation sent a replacement application')
        await assertIdentityPending()
      }
      await close()
    }
    for (const value of ['legacy', 'legacy_unknown', 'private_missing', 'closed']) {
      variant = value; await navigate(); await begin(); await assertNoOldFlow()
      const before = writes.length
      if (value.startsWith('legacy')) {
        await modal().getByRole('heading', { name: '旧版好友操作记录', exact: true }).waitFor()
        check(!await modal().getByRole('button', { name: /查找我的账号|让机器人添加我/ }).count(),
          'Legacy record exposed a new mutation')
        if (value === 'legacy_unknown') {
          await modal().getByRole('button', { name: '核对原好友操作', exact: true }).click()
          await modal().getByText('好友申请已接受', { exact: true }).waitFor()
        }
        await assertIdentityPending()
      } else if (value === 'private_missing') {
        await selectPrivate()
        await modal().getByText(/身份核验内容仅在首次签发时显示/).waitFor()
        check(!await modal().getByRole('button', { name: '复制身份核验内容', exact: true }).count(),
          'Missing private proof fell back to the old friend note')
      } else {
        await prepare('wsl')
        check(await modal().getByRole('button', { name: '让机器人添加我', exact: true }).isDisabled(),
          'Closed admission enabled sending')
        await modal().getByText(
          '此机器人的好友申请发送尚未开放，账号查找与已有好友的私聊核验仍可使用',
          { exact: true }).waitFor()
        const afterSearch = writes.length, afterSearchCaptcha = captchaRequests.length
        await selectPrivate()
        await modal().getByText(privateProof('wsl'), { exact: true }).waitFor()
        await refresh()
        check(writes.length === afterSearch && captchaRequests.length === afterSearchCaptcha,
          'Closed send admission blocked private proof or started another operation')
      }
      check(writes.length === before + (value === 'closed' || value === 'legacy_unknown' ? 1 : 0),
        'History or private proof unexpectedly mutated onboarding')
      await close()
    }
    for (const [value, message] of Object.entries({
      not_found: '未找到此账号，请在官方 App 核对本人可搜索账号',
      lookup_rejected: '平台拒绝了本次账号查找，未发送好友申请。请核对本人可搜索账号；已有好友可继续私聊核验',
      target_inconsistent: '平台返回的账号资料与精确查找结果不一致，未发送好友申请；请核对原记录或联系管理员',
      read_incomplete: '尚未取得完整好友状态，未发送申请，请稍后手动核对'
    })) {
      variant = value; await navigate(); await begin()
      const beforeLookup = writes.length, beforeLookupCaptcha = captchaRequests.length
      await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).fill('self-wsl')
      await modal().getByRole('button', { name: '查找我的账号', exact: true }).click()
      await modal().getByText(message, { exact: true }).waitFor()
      if (['target_inconsistent', 'read_incomplete'].includes(value)) {
        await page.clock.fastForward(5000)
        await refresh(); await close()
        await page.getByRole('button', { name: '接入群组', exact: true }).click()
        await modal().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
        check(!latest.friend_outbound, 'Inconsistent or incomplete lookup pinned a target')
      }
      check(writes.length === beforeLookup + 1 && captchaRequests.length === beforeLookupCaptcha &&
        !await modal().getByRole('button', { name: '让机器人添加我', exact: true }).count(),
        'Rejected lookup or missing exact target started captcha or sending')
      await assertIdentityPending(); await close()
    }
    variant = 'normal'; await navigate(); await begin()
    const lookupError = () => modal().locator('.onboarding-error')
    const lookupDetails = () => lookupError().locator('.el-alert__description')
    const lookupTitle = () => lookupError().locator('.el-alert__title')
    const genericLookup = '平台拒绝了本次账号查找，未发送好友申请。请核对本人可搜索账号；已有好友可继续私聊核验'
    const submitLookupFailure = async failure => {
      lookupFailure = failure
      const before = { writes: writes.length, reads: reads.length, captcha: captchaRequests.length }
      await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).fill('self-wsl')
      await modal().getByRole('button', { name: '查找我的账号', exact: true }).click()
      await lookupError().waitFor()
      await page.waitForFunction(() => !document.querySelector('.onboarding-friend-target .is-loading'))
      await page.clock.fastForward(1000)
      check(writes.length === before.writes + 1 && writes.at(-1).path.endsWith('/friend-target') &&
        captchaRequests.length === before.captcha &&
        reads.slice(before.reads).every(read => !read.path.startsWith('/group-binding/')) &&
        !latest.friend_outbound &&
        !await modal().getByRole('button', { name: '让机器人添加我', exact: true }).count(),
        'Failed lookup retried, sent, refreshed or opened captcha')
      check(!await modal().evaluate((element, secret) => element.textContent.includes(secret), lookupSecret) &&
        !await page.evaluate(() => document.documentElement.dataset.lookupLeak) &&
        !await lookupError().locator('img, script, iframe, a').count(),
        'Untrusted lookup metadata escaped into the UI')
      await assertIdentityPending(); await assertNoOldFlow(); await checkStorage()
    }
    for (const [code, message] of Object.entries({
      group_binding_outbound_parameters_rejected: '平台未接受账号查找参数，请核对本人可搜索账号后手动操作',
      group_binding_outbound_security_required: '账号查找需要平台安全验证，请联系超管在官方 App 手动核对',
      group_binding_outbound_client_version: '平台不接受当前查找客户端版本，请联系超管核对',
      group_binding_outbound_ip_restricted: '平台限制了账号查找的网络来源，请联系超管核对',
      group_binding_outbound_lookup_forbidden: '机器人当前无权查找账号，请联系超管核对权限',
      group_binding_outbound_account_unavailable: '查找使用的机器人账号暂不可用，请联系超管核对账号状态',
      native_mobile_reauth_required: '手机搜索会话已失效，请联系超管为此机器人手动重新认证；不会自动登录',
      group_binding_outbound_rate_limited: '平台限制了本次账号查找频率，未发送好友申请。请稍后由本人手动操作，不会自动重试。'
    })) {
      await submitLookupFailure({ msg: code })
      await lookupError().getByText(message, { exact: true }).waitFor()
      check(!await lookupDetails().count(), 'Legacy lookup code fabricated diagnostic evidence')
    }
    for (const code of [-1, 1001]) {
      await submitLookupFailure({ data: lookupData({ provider_code: code }) })
      await lookupError().getByText(
        `失败阶段：账号搜索；上游返回码：${code}；原因分类：上游原因文本为空`, { exact: true }).waitFor()
      check(!/频率|频繁|限流/.test(await lookupError().innerText()),
        'Generic provider code was inferred to mean rate limiting')
    }
    for (const [reason, text] of Object.entries({
      provider_code_classified: '已按业务码分类',
      provider_reason_classified: '已按上游原因分类',
      provider_reason_empty: '上游原因文本为空',
      provider_reason_unrecognized: '上游原因非空但未识别，原文已过滤',
      provider_reason_oversized: '上游原因过长，原文已过滤',
      provider_reason_nontext: '上游原因为非文本，原文已过滤',
      http_status: '已按 HTTP 状态分类',
      network_timeout: '网络等待超时',
      network_unavailable: '网络不可用',
      connection_closed: '连接提前关闭'
    })) {
      await submitLookupFailure({ data: lookupData({ stage: 'friend_info', reason,
        provider_reason: lookupAttack, token: lookupSecret, password: lookupSecret,
        account: lookupSecret, url: lookupAttack, metadata: { text: lookupAttack } }) })
      await lookupError().getByText(
        `失败阶段：账号详情读取；上游返回码：-1；原因分类：${text}`, { exact: true }).waitFor()
      check((await lookupTitle().innerText()).startsWith('账号详情读取未完成，未发送好友申请。'),
        'Friend-info failure was misreported as search or send')
    }
    for (const [category, message] of Object.entries({
      rate_limited: '平台限制了本次查找频率',
      session_expired: '请联系超管手动重新认证；不会自动登录',
      parameters_rejected: '平台未接受查找参数',
      security_verification: '平台要求安全验证',
      client_version: '平台不接受当前查找客户端版本',
      ip_restricted: '平台限制了当前网络来源',
      permission_denied: '机器人当前无权执行此读取',
      account_unavailable: '机器人账号暂不可用',
      upstream_rejected: '尚无法确认具体原因',
      transport_unavailable: '暂未取得平台响应',
      response_invalid: '平台响应暂无法核验'
    })) {
      await submitLookupFailure({ data: lookupData({ category,
        provider_code: category === 'rate_limited' ? 1043 : undefined, reason: undefined,
        retry_after_seconds: 60 }) })
      check((await lookupTitle().innerText()).includes(message) &&
        (await lookupDetails().innerText()).includes('原因分类：尚无可用分类'),
        'Lookup category or omitted optional evidence was misreported')
      check((await lookupDetails().innerText()).includes('建议至少等待 60 秒') === (category === 'rate_limited'),
        'Retry guidance escaped the rate-limited category')
      if (category === 'rate_limited') {
        const before = { writes: writes.length, reads: reads.length, captcha: captchaRequests.length }
        await page.clock.fastForward(61000)
        check(writes.length === before.writes && captchaRequests.length === before.captcha &&
          reads.slice(before.reads).every(read => !read.path.startsWith('/group-binding/')),
          'Rate limit or retry-after automatically started another operation')
      }
    }
    for (const changes of [
      { version: 2 }, { platform: 'lajiaoliao' }, { stage: lookupAttack }, { category: lookupAttack },
      { observed_at: lookupAttack }, { observed_at: '2026-02-30T12:00:00Z' },
      { observed_at: '2026-11-08' }, { observed_at: '2026-11-08T25:00:00Z' }
    ]) {
      await submitLookupFailure({ data: lookupData(changes) })
      await lookupError().getByText(genericLookup, { exact: true }).waitFor()
      check(!await lookupDetails().count(), 'Malformed required lookup evidence was accepted')
    }
    for (const value of [0, -2, 10000, 1043.5, '1043', { token: lookupSecret }]) {
      await submitLookupFailure({ data: lookupData({ provider_code: value, reason: lookupAttack,
        retry_after_seconds: lookupAttack }) })
      await lookupError().getByText(
        '失败阶段：账号搜索；上游返回码：未提供；原因分类：尚无可用分类', { exact: true }).waitFor()
    }
    for (const retry of [0, 86401, 1.5, '60']) {
      await submitLookupFailure({ data: lookupData({ category: 'rate_limited', provider_code: 1043,
        reason: 'provider_code_classified', retry_after_seconds: retry }) })
      check(!(await lookupDetails().innerText()).includes('建议至少等待'),
        'Invalid retry-after evidence reached the UI')
    }
    await submitLookupFailure({ msg: lookupAttack, data: { ...lookupData({ stage: 'friend_info',
      reason: 'provider_reason_unrecognized', raw: lookupAttack }), token: lookupSecret, msg: lookupAttack } })
    for (const width of [320, 390]) await fit('wsl', width, 'lookup-friend-info')
    await submitLookupFailure({ data: lookupData({ provider_code: -1, reason: 'provider_reason_empty' }) })
    for (const width of [320, 390]) await fit('wsl', width, 'lookup-search-minus-one')
    await submitLookupFailure({ data: lookupData({ category: 'rate_limited', provider_code: 1043,
      reason: 'provider_code_classified', retry_after_seconds: 86400 }) })
    for (const width of [320, 390]) await fit('wsl', width, 'lookup-rate-limited')
    const beforeRefresh = writes.length
    await refresh()
    check(!await lookupError().count() && writes.length === beforeRefresh,
      'Refreshing progress retained a stale lookup diagnosis or resubmitted lookup')
    lookupFailure = undefined
    await close()
    await page.setViewportSize({ width: 1440, height: 960 })
    await navigate(); await begin(); await prepare('wsl')
    sendFailure = lookupData({ category: 'rate_limited', provider_code: 1043 })
    const beforeSendFailure = writes.length
    await confirmFriend(); await completeCaptcha()
    await lookupError().getByText('本次好友申请未完成，不会自动重发，请核对原记录', { exact: true }).waitFor()
    await page.clock.fastForward(1000)
    check(!await lookupDetails().count() && !(await lookupError().innerText()).includes('账号搜索') &&
      writes.length === beforeSendFailure + 1 && writes.at(-1).path.endsWith('/friend-send'),
      'Send failure inherited lookup diagnosis or automatically retried')
    sendFailure = undefined; await close()
    for (const delayed of [false, true]) {
      await page.clock.setSystemTime(new Date())
      expiryDeadline = new Date(Date.now() + 60000).toISOString()
      await navigate(); await begin()
      lookupFailure = { data: lookupData({}) }
      const before = { writes: writes.length, reads: reads.length }
      let lateFailure
      if (delayed) {
        holdPrepare = true
        const began = new Promise(resolve => { startedPrepare = resolve })
        lateFailure = page.waitForResponse(response =>
          new URL(response.url()).pathname === `/api/v1/group-binding/challenges/${latest.id}/friend-target`)
        await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).fill('self-wsl')
        await modal().getByRole('button', { name: '查找我的账号', exact: true }).click(); await began
      } else await submitLookupFailure(lookupFailure)
      await page.clock.fastForward(61000)
      await modal().locator('.onboarding-expired').waitFor()
      if (delayed) {
        releasePrepare(); releasePrepare = undefined; await (await lateFailure).finished()
        await page.waitForFunction(() => !document.querySelector('.onboarding-dialog .el-dialog__footer .is-loading'))
      }
      check(!await lookupError().count() && writes.length === before.writes + 1 &&
        reads.slice(before.reads).every(read => !read.path.startsWith('/group-binding/')),
        'Expiry retained or resurrected a lookup diagnosis or started a request')
      lookupFailure = undefined
      await page.clock.setSystemTime(new Date())
      expiryDeadline = new Date(Date.now() + 600000).toISOString()
      await modal().getByRole('button', { name: '重新开始核验', exact: true }).click()
      await completeCaptcha()
      await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).waitFor()
      check(!await lookupError().count(), 'New challenge inherited an expired lookup diagnosis')
      await close(); expiryDeadline = undefined
    }
    variant = 'normal'; await navigate(); await open()
    await modal().locator('label.el-radio').filter({ hasText: '代客户接入' }).click()
    check(await modal().getByRole('button', { name: '开始核验', exact: true }).isDisabled(),
      'Customer onboarding did not require an explicit resource owner')
    await modal().getByRole('combobox', { name: '群组所属客户', exact: true }).click()
    await page.getByRole('option', { name: '合成客户', exact: true }).click()
    await modal().getByRole('button', { name: '开始核验', exact: true }).click()
    await completeCaptcha()
    await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).waitFor()
    check(writes.at(-1).body.target_tenant_id === customerTenant, 'Selected customer ownership was lost')
    await close(); await open()
    check(!await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).count(),
      'Customer progress leaked into the default own workspace')
    await close()
    variant = 'normal'; await navigate(); await begin()
    holdPrepare = true
    const priorID = latest.id, began = new Promise(resolve => { startedPrepare = resolve })
    await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).fill('self-wsl')
    const late = page.waitForResponse(response =>
      new URL(response.url()).pathname === `/api/v1/group-binding/challenges/${priorID}/friend-target`,
      { timeout: 30000 })
    await modal().getByRole('button', { name: '查找我的账号', exact: true }).click(); await began
    // The top-right close remains usable during an in-flight read.
    await modal().locator('.el-dialog__headerbtn').click()
    await modal().waitFor({ state: 'hidden' })
    await page.getByRole('button', { name: '切换工作台', exact: true }).click()
    await page.getByRole('dialog', { name: '选择平台', exact: true })
      .getByRole('button', { name: '辣椒聊机器人', exact: true }).click()
    await begin()
    releasePrepare(); releasePrepare = undefined; await (await late).finished()
    await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).waitFor()
    check(!await modal().getByText('self-wsl', { exact: true }).count() &&
      !await modal().getByRole('button', { name: '让机器人添加我', exact: true }).count(),
      'Late lookup replaced a different platform dialog')
    await close()
    lookupFailure = { data: lookupData({ category: 'session_expired', provider_code: -1 }) }
    await navigate(); await begin()
    holdPrepare = true
    const rejectedID = latest.id, rejectedBegan = new Promise(resolve => { startedPrepare = resolve })
    const lateRejected = page.waitForResponse(response =>
      new URL(response.url()).pathname === `/api/v1/group-binding/challenges/${rejectedID}/friend-target`,
      { timeout: 30000 })
    await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).fill('self-wsl')
    await modal().getByRole('button', { name: '查找我的账号', exact: true }).click(); await rejectedBegan
    await modal().locator('.el-dialog__headerbtn').click()
    await modal().waitFor({ state: 'hidden' })
    lookupFailure = undefined
    await page.getByRole('button', { name: '切换工作台', exact: true }).click()
    await page.getByRole('dialog', { name: '选择平台', exact: true })
      .getByRole('button', { name: '辣椒聊机器人', exact: true }).click()
    await begin()
    releasePrepare(); releasePrepare = undefined; await (await lateRejected).finished()
    check(!await lookupError().count() && !await lookupDetails().count() &&
      latest.platform === 'lajiaoliao', 'Late lookup failure crossed platform/challenge scope')
    lookupFailure = { data: lookupData({}) }
    await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).fill('self-lajiaoliao')
    await modal().getByRole('button', { name: '查找我的账号', exact: true }).click()
    await lookupError().getByText(genericLookup, { exact: true }).waitFor()
    check(!await lookupDetails().count(), 'WSL-only diagnosis was attached to another platform')
    lookupFailure = undefined; await close()
    for (const platform of platforms) {
      for (const width of viewports) {
        await page.clock.setSystemTime(new Date())
        expiryDeadline = new Date(Date.now() + 60000).toISOString()
        await page.setViewportSize({ width, height: 820 }); await navigate(platform); await begin()
        const original = latest.id, priorKey = writes.at(-1).key
        const before = { reads: reads.length, writes: writes.length }
        await page.clock.fastForward(61000)
        const expired = modal().locator('.onboarding-expired')
        await expired.getByText('已过期，请重新开始核验', { exact: true }).waitFor()
        check(await expired.locator('.el-alert__icon').count() &&
          await modal().locator('.onboarding-state .el-tag--danger').count() &&
          !await modal().getByRole('button', { name: /查找我的账号|让机器人添加我|复制身份核验内容/ }).count() &&
          writes.length === before.writes &&
          reads.slice(before.reads).every(read => !read.path.startsWith('/group-binding/')),
          'Deadline did not expire locally with a red warning')
        const restart = modal().getByRole('button', { name: '重新开始核验', exact: true })
        const box = await restart.boundingBox()
        check(box && box.y >= 0 && box.y + box.height <= 821, 'Expired action left the mobile viewport')
        const file = `output/playwright/onboarding-outbound-${platform}-expired-${width}.png`
        await page.screenshot({ path: file, animations: 'disabled' }); screenshots.push(file)
        await page.clock.setSystemTime(new Date())
        expiryDeadline = new Date(Date.now() + 600000).toISOString()
        await restart.click(); await captchaDialog().waitFor()
        check(writes.length === before.writes, 'Restart created a challenge before captcha')
        await completeCaptcha()
        await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).waitFor()
        check(latest.id !== original && writes.at(-1).key !== priorKey &&
          latest.platform === platform && latest.customer.tenant_id === ownTenant,
          'Restart reused scope or an expired challenge/key')
        await close()
      }
      await page.clock.setSystemTime(new Date())
      expiryDeadline = new Date(Date.now() + 60000).toISOString()
      variant = 'normal'; await navigate(platform); await begin(); await prepare(platform)
      await modal().getByRole('button', { name: '让机器人添加我', exact: true }).click()
      await friendConfirmation().waitFor()
      const beforeFriendConfirmation = writes.length, beforeFriendCaptcha = captchaRequests.length
      await page.clock.fastForward(61000)
      await friendConfirmation().waitFor({ state: 'hidden' })
      check(writes.length === beforeFriendConfirmation && captchaRequests.length === beforeFriendCaptcha,
        'Expiration left a send confirmation active or started a submission')
      await close()
      await page.clock.setSystemTime(new Date())
      expiryDeadline = new Date(Date.now() + 60000).toISOString()
      await navigate(platform); await begin(); await prepare(platform)
      await confirmFriend()
      await captchaDialog().waitFor()
      const beforeCaptcha = writes.length
      await page.clock.fastForward(61000)
      await captchaDialog().waitFor({ state: 'hidden' })
      check(writes.length === beforeCaptcha, 'Expiration submitted a pending captcha operation')
      await close()
      await page.clock.setSystemTime(new Date())
      expiryState = 'proven'; expiryDeadline = new Date(Date.now() + 60000).toISOString()
      await navigate(platform); await begin()
      await modal().getByRole('button', { name: '确认绑定', exact: true }).click()
      const confirmation = page.getByRole('dialog', { name: '确认群归属', exact: true })
      await confirmation.waitFor()
      const beforeConfirmation = writes.length
      await page.clock.setSystemTime(new Date(Date.now() + 61000))
      await page.evaluate(() => window.dispatchEvent(new Event('focus')))
      await confirmation.waitFor({ state: 'hidden' })
      await modal().locator('.onboarding-expired').waitFor()
      check(writes.length === beforeConfirmation, 'Expired confirmation submitted a binding')
      await close(); expiryState = 'waiting'
      await page.clock.setSystemTime(new Date())
      variant = 'expiry_unknown'; expiryDeadline = new Date(Date.now() + 60000).toISOString()
      await navigate(platform); await begin(); await page.clock.fastForward(61000)
      await modal().locator('.onboarding-expired').waitFor()
      check(await modal().getByRole('button', { name: '重新开始核验', exact: true }).isDisabled(),
        'Unknown result was discarded by restart')
      const before = writes.length
      await close(); await page.getByRole('button', { name: '接入群组', exact: true }).click()
      await modal().getByRole('button', { name: '核对原好友申请', exact: true }).click()
      await modal().getByText('已是好友', { exact: true }).waitFor()
      check(writes.length === before + 1 &&
        !await modal().getByRole('button', { name: '重新开始核验', exact: true }).isDisabled(),
        'Expired unknown reconciliation replayed or remained blocked')
      await close(); variant = 'normal'
      for (const state of ['bound', 'review']) {
        await page.clock.setSystemTime(new Date())
        expiryState = state; expiryDeadline = new Date(Date.now() - 60000).toISOString()
        await navigate(platform); await begin()
        await modal().getByText(state === 'bound' ? '绑定成功' : '待平台审核', { exact: true }).waitFor()
        check(!await modal().locator('.onboarding-expired').count(), 'Completed binding became expired')
        await close()
      }
      expiryState = 'waiting'; expiryDeadline = undefined
    }
    await page.setViewportSize({ width: 1440, height: 960 })
    await navigate('wsl', 'billing')
    const usage = page.getByRole('region', { name: '我的实际用量', exact: true })
    const personal = usage.getByRole('region', { name: '本人操作用量', exact: true })
    const backgroundUsage = usage.getByRole('region', { name: '演示空间后台用量', exact: true })
    await page.getByText('超管专属 · 不限额', { exact: true }).waitFor()
    await personal.getByText('0.0125 USDT', { exact: true }).waitFor()
    await backgroundUsage.getByText('0.025 USDT', { exact: true }).waitFor()
    const beforeFees = writes.length; backgroundActual = 46500000
    await usage.getByRole('button', { name: '刷新实际用量', exact: true }).click()
    await backgroundUsage.getByText('0.0465 USDT', { exact: true }).waitFor()
    check(writes.length === beforeFees, 'Fee display or refresh mutated billing')
    role = 'tenant'; await navigate(); await begin()
    check(!await modal().getByRole('radio', { name: '代客户接入', exact: true }).count() &&
      fields(writes.at(-1).body, ['platform']), 'Customer inherited operator resource selection')
    await close(); await checkStorage()
    return { passed: true, synthetic_only: true, platforms, viewports,
      unified_outbound_flow: true, lookup_without_send: true, exact_target_preview: true,
      public_robot_account_hidden: true, explicit_target_confirmation: true,
      cancelled_confirmation_no_send: true, expiry_closes_friend_confirmation: true,
      private_invite_group_proof_binding_preserved: true,
      first_issued_proof_missing_blocks_new_send: true,
      missing_target_safe_error: true,
      rejected_lookup_not_misreported_as_send: true,
      lookup_search_and_friend_info_diagnostics: true, generic_minus_one_and_1001_not_rate_limited: true,
      explicit_1043_rate_manual_only: true, optional_lookup_evidence_preserved: true,
      lookup_reason_empty_unrecognized_filtered_distinct: true, lookup_metadata_allowlist: true,
      lookup_failure_no_extra_requests: true, lookup_diagnosis_mobile_fit: true,
      lookup_diagnosis_cleared_on_expiry_refresh_new_challenge: true,
      late_lookup_failure_isolated: true, lookup_diagnosis_excluded_from_send_and_other_platform: true,
      target_inconsistent_safe_error_no_replay: true, incomplete_read_safe_error_no_replay: true,
      purpose_target_bound_captcha: true, cancelled_captcha_no_send: true, single_send: true,
      existing_friend_private_proof: true, acceptance_not_identity: true, independent_private_proof: true,
      unknown_reload_no_replay: true, unknown_reconcile_original_only: true, late_lookup_isolated: true,
      legacy_history_only: true, no_legacy_friend_note: true, closed_admission_disabled: true,
      closed_admission_search_available: true, closed_admission_private_proof_available: true,
      deadline_updates_without_refresh: true, red_expiry_icon: true, expired_codes_hidden: true,
      restart_requires_new_captcha_and_key: true, expiry_closes_pending_captcha: true,
      expiry_closes_pending_confirmation: true, background_clock_jump_handled: true,
      expired_unknown_reconciled_without_replay: true, completed_review_preserved: true,
      scoped_proofs_not_persisted: true, closed_binding: true, own_workspace_default: true,
      customer_scope_preserved: true, background_human_billing_separate: true,
      writes: writes.length, reads: reads.length, native_calls: 0, model_calls: 0, screenshots }
  } finally {
    releasePrepare?.()
    await page.unroute('**/api/v1/**', handler)
  }
}
