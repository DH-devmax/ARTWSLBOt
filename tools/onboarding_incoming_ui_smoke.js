// Synthetic APIs only. Browser coverage runs through run_multi_platform_ui_smoke.mjs.
// Browserless state checks: WSL_UI_NODE_MODULES=/absolute/web/node_modules node tools/onboarding_incoming_ui_smoke.js --unit
// prettier-ignore
(() => {
const smoke = async page => {
  page.setDefaultTimeout(10000)
  await page.clock.install()
  const base = new URL(page.url()).origin
  if (base !== 'http://127.0.0.1:5173') throw Error('Use the intercepted synthetic UI runner')
  const id = n => `f8400000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const platforms = ['wsl', 'lajiaoliao'], viewports = [320, 390, 768, 1440]
  const platformName = p => p === 'wsl' ? '旺商聊' : '辣椒聊'
  const check = (value, message) => { if (!value) throw Error(message) }
  const copy = value => JSON.parse(JSON.stringify(value))
  const fields = (value, names) => Object.keys(value).sort().join() === names.sort().join()
  const privateProof = p => `群接入 ${p === 'wsl' ? 'p'.repeat(43) : 'q'.repeat(43)}`
  const groupProof = '群绑定 SYNTHETIC_INCOMING_GROUP_PROOF'
  const workspace = { id: id(2), name: '合成接入工作空间', kind: 'tenant_business' }
  const operatorSpace = { id: id(6), name: '我的演示工作空间', kind: 'operator_demo' }
  const operatorPlan = { id: 'operator_unlimited', name: '超管专属 · 不限额', system_only: true,
    saleable: false, assignable: false, unlimited: true, permanent: true, group_limit: null,
    member_limit: null, assistant_limit: null, credit_limit: null, platforms: [] }
  let actor = id(1), role = 'user', variant = 'normal', latest, serial = 100, expiryDeadline
  let hold, release
  const heldRequests = new Map()
  const records = new Map(), tickets = new Map(), captchaScopes = new Map()
  const writes = [], reads = [], captchas = [], unexpected = [], screenshots = [], consumed = []
  const expiryRequests = []
  const backgroundReadPaths = new Set(['/me', '/workspaces', '/user-center', '/user-center/groups', '/user-center/usage'])
  const accounts = platforms.map((platform, index) => ({ id: id(30 + index), platform,
    workspace: platform, purpose: 'management', name: `${platformName(platform)}接入机器人`,
    native_nickname: `${platformName(platform)}原生昵称`, auth_state: 'authenticated', version: 1 }))
  const gates = new Map(accounts.map(a => [a.id, { account_id: a.id, platform: a.platform,
    state: 'closed', version: 0, evidence_refs: [] }]))
  const grants = () => ['overview:read', 'groups:read', 'claims:create', 'operations:read',
    ...(role === 'superadmin' ? ['accounts:read', 'workspaces:maibot'] : [])]
  const progress = (record, first = false) => {
    const { secret, ...value } = copy(record)
    return { ...value, ...(first ? { private_challenge: secret, group_challenge: groupProof } : {}),
      friend_challenge: 'LEGACY_NOTE_MUST_NEVER_APPEAR' }
  }
  const pendingContact = record => ({
    id: id(++serial), status: 'prepared', version: 1, result_code: '',
    name: `本人${platformName(record.platform)}`,
    requested_at: variant.startsWith('expired') ?
      new Date(Date.parse(record.requested_at) - 660000).toISOString() : record.requested_at,
    ...(variant.startsWith('expired') ? { action: 'reject_expired', can_reject: variant !== 'expired_closed' } : {}),
    can_approve: variant !== 'closed' && !variant.startsWith('expired'), can_reconcile: false,
    blocked_reason: variant === 'closed' ? 'group_binding_friend_admission_closed' : '',
    native_id: 'SENSITIVE_CONTACT_ID', request_id: 'SENSITIVE_REQUEST_ID'
  })
  const bounded = async (promise, label) => {
    let timer
    try {
      return await Promise.race([promise, new Promise((_, reject) => {
        timer = setTimeout(() => reject(Error(label)), 10000)
      })])
    } finally { clearTimeout(timer) }
  }
  const delay = async (path, request) => {
    if (hold?.path !== path) return
    const pending = hold
    hold = undefined
    heldRequests.set(request, pending)
    pending.start()
    await pending.released
  }
  const holdNext = path => {
    check(!hold, 'Overlapping held fixture requests')
    let start, finish, unblock
    const pending = {
      path, started: new Promise(resolve => { start = resolve }),
      done: new Promise(resolve => { finish = resolve }),
      released: new Promise(resolve => { unblock = resolve }),
      start: () => start(), finish: () => finish(), release: () => unblock()
    }
    hold = pending; release = pending.release
    return pending
  }
  const settleLateRequest = async pending => {
    // Navigation or captcha cancellation may prevent a browser response event.
    await bounded(pending.done, 'Held fixture did not complete after release: ' + pending.path)
    await bounded(page.evaluate(() => new Promise(resolve => requestAnimationFrame(() => requestAnimationFrame(resolve)))),
      'Layout did not resume after held request: ' + pending.path)
  }
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
      const tag = Buffer.from(type), size = Buffer.alloc(4), checksum = Buffer.alloc(4)
      size.writeUInt32BE(data.length); checksum.writeUInt32BE(crc(Buffer.concat([tag, data])))
      return Buffer.concat([size, tag, data, checksum])
    }
    const header = Buffer.alloc(13), pixels = Buffer.alloc((width * 4 + 1) * height)
    header.writeUInt32BE(width); header.writeUInt32BE(height, 4); header[8] = 8; header[9] = 6
    for (let y = 0; y < height; y++) for (let x = 0; x < width; x++)
      pixels.set([80 + x % 100, 100 + y % 100, 170, 255], y * (width * 4 + 1) + 1 + x * 4)
    return 'data:image/png;base64,' + Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
      chunk('IHDR', header), chunk('IDAT', process.getBuiltinModule('zlib').deflateSync(pixels)),
      chunk('IEND', Buffer.alloc(0))]).toString('base64')
  }
  const background = png(300, 220), tile = png(44, 44)
  const captchaScope = req => {
    const body = req.postDataJSON(), platform = req.headers()['x-workspace']
    check(fields(body, ['purpose', 'platform', ...(body.target ? ['target'] : []),
      ...(body.previous_id ? ['previous_id'] : []),
      ...(body.challenge_id ? ['challenge_id', 'x', 'y'] : [])]), 'Unexpected captcha fields')
    check(platforms.includes(platform) && body.platform === platform &&
      ['group_binding_create', 'group_binding_friend'].includes(body.purpose), 'Captcha crossed purpose/platform')
    check(body.purpose === 'group_binding_friend' ? records.get(body.target)?.platform === platform : !body.target,
      'Approval captcha omitted its challenge target')
    return { purpose: body.purpose, platform, ...(body.target ? { target: body.target } : {}) }
  }
  const consume = (req, scope) => {
    const ticket = tickets.get(req.headers()['x-human-verification'])
    check(ticket && !ticket.used && JSON.stringify(ticket.scope) === JSON.stringify(scope),
      'Missing, reused or crossed human-verification ticket')
    ticket.used = true; consumed.push(scope)
  }
  const respond = async route => {
    const req = route.request(), url = new URL(req.url())
    const path = url.pathname.replace('/api/v1', ''), platform = req.headers()['x-workspace'] || 'wsl'
    const now = await page.evaluate(() => Date.now())
    let data = [], status = 200
    if (path === '/human-verification/challenge' || path === '/human-verification/verify') {
      const body = req.postDataJSON(), scope = captchaScope(req)
      captchas.push({ path, scope })
      if (path.endsWith('/challenge')) {
        data = { challenge_id: id(++serial), image: { background, tile, width: 300, height: 220,
          tile_width: 44, tile_height: 44, initial_x: 128, initial_y: 80 },
          expires_at: new Date(now + 120000).toISOString() }
        captchaScopes.set(data.challenge_id, scope)
      } else {
        check(JSON.stringify(captchaScopes.get(body.challenge_id)) === JSON.stringify(scope) &&
          body.x === 148 && body.y === 80, 'Captcha verify changed scope or fixture position')
        captchaScopes.delete(body.challenge_id)
        const token = `SYNTHETIC_${String(++serial).padStart(33, '0')}`
        tickets.set(token, { scope, used: false })
        data = { ticket: token, expires_at: new Date(now + 60000).toISOString() }
      }
    } else if (/^\/system\/group-binding\/friend-gates\/[^/]+$/.test(path)) {
      const account = accounts.find(a => a.id === path.split('/').at(-1))
      check(role === 'superadmin' && account?.platform === platform, 'Incoming gate crossed account/platform')
      const saved = gates.get(account.id)
      if (req.method() === 'GET') reads.push({ path, platform })
      else {
        const body = req.postDataJSON()
        check(req.method() === 'PUT' && fields(body, ['state', 'version', 'evidence_refs']) &&
          body.version === saved.version, 'Incoming gate omitted version fence')
        writes.push({ path, platform, body })
        gates.set(account.id, { ...saved, ...body, version: saved.version + 1 })
      }
      data = copy(gates.get(account.id))
    } else if (req.method() === 'GET') {
      reads.push({ path, platform })
      if (path === '/me') data = { user: { id: actor, username: 'synthetic-incoming', role,
        ...(role === 'user' ? { tenant_id: workspace.id, tenant_role: 'owner' } : {}) }, permissions: grants() }
      else if (path === '/workspaces') data = { items: platforms.map(id => ({ id, name: `${platformName(id)}机器人` })) }
      else if (path === '/user-center') data = {
        workspace: role === 'superadmin' ? operatorSpace : workspace,
        effective_plan: role === 'superadmin' ? operatorPlan : undefined,
        subscription: role === 'superadmin' ? null : { name: 'PRO', active: true, platforms, modules: [], group_limit: 8,
          member_limit: 4, expires_at: new Date(now + 600000).toISOString() },
        entitlement: { mode: role === 'superadmin' ? 'operator_demo' : 'tenant_business', active: true,
          unlimited: role === 'superadmin', permissions: grants() },
        groups: [], permissions: grants(), personas: [], claims: [], operations: [], active_group_count: 0 }
      else if (path === '/user-center/groups') data = { workspace: role === 'superadmin' ? operatorSpace : workspace,
        items: [], total: 0, page_size: 20, customers: [{ tenant_id: workspace.id, name: workspace.name }] }
      else if (path === '/user-center/usage') data = { call_count: 0, actual_micro: 0, held_micro: 0 }
      else if (path === '/test-tools/onboarding/scopes') {
        check(role === 'superadmin', 'Customer requested controlled onboarding scopes')
        data = { items: [], candidates: { accounts: [], groups: [] } }
      }
      else if (path === '/group-binding/context') data = {
        mode: role === 'superadmin' ? 'operator_demo' : 'tenant_business',
        workspace: role === 'superadmin' ? operatorSpace : workspace,
        default_target_tenant_id: role === 'superadmin' ? operatorSpace.id : workspace.id,
        can_onboard_for_customer: role === 'superadmin',
        customers: role === 'superadmin' ? [{ tenant_id: workspace.id, name: workspace.name }] : [], next_offset: -1 }
      else if (/^\/group-binding\/challenges\/[^/]+$/.test(path)) {
        const record = records.get(path.split('/')[3])
        check(record?.platform === platform, 'GET crossed challenge/platform')
        data = progress(record)
      } else if (path === '/accounts') data = accounts.filter(a => a.platform === platform)
      else if (path === '/overview') data = { accounts: 2, groups: 0 }
      else if (path === '/groups' || path === '/operations') data = []
      else if (path.endsWith('/events')) return route.fulfill({ contentType: 'text/event-stream', body: '' })
      else if (/^\/accounts\/[^/]+\/dashboard$/.test(path)) data = {
        account: accounts.find(a => a.id === path.split('/')[2]), sampled_at: new Date(now).toISOString(),
        visibility: 'account', groups: { authorized: 0, management_enabled: 0, ai_enabled: 0, ai_pending: 0 },
        operations: { queued: 0, running: 0, unknown: 0, failed_15m: 0 },
        inbox: { pending: 0, unresolved: 0, quarantined: 0 },
        performance: { per_second: 0, inserted_60s: 0, series: [] },
        health: { status: 'unobserved', reasons: [] }, backlog: { status: 'empty', reasons: [] },
        runtime_observed: false }
      else unexpected.push({ path, method: req.method() })
    } else {
      const body = req.postDataJSON() || {}, key = req.headers()['idempotency-key']
      check(req.method() === 'POST', 'Unexpected mutation method')
      writes.push({ path, platform, body, key })
      if (path === '/group-binding/challenges') {
        consume(req, { purpose: 'group_binding_create', platform })
        check(fields(body, role === 'superadmin' ? ['platform', 'target_tenant_id'] : ['platform']) &&
          body.platform === platform && key, 'Creation scope/key missing')
        const targetSpace = role === 'superadmin' && body.target_tenant_id === operatorSpace.id ? operatorSpace : workspace
        check(role !== 'superadmin' || [workspace.id, operatorSpace.id].includes(body.target_tenant_id),
          'Operator creation omitted explicit resource ownership')
        latest = { id: id(++serial), platform, friend_flow: 'incoming_contact', state: 'waiting',
          version: 1, expires_at: expiryDeadline || new Date(now + 600000).toISOString(),
          requested_at: new Date(now - 1000).toISOString(),
          mode: role === 'superadmin' ? 'operator_demo' : 'tenant_business', workspace: targetSpace,
          customer: { tenant_id: targetSpace.id, name: targetSpace.name },
          robot: { id: accounts[platforms.indexOf(platform)].id, name: `${platformName(platform)}接入机器人`,
            native_id: 'SENSITIVE_ROBOT_ID', add_account: `public-${platform}-robot`, add_available: variant !== 'no_account' },
          secret: variant === 'missing_private' ? '' : privateProof(platform), identity_proven: false,
          can_confirm: false, can_cancel: true, can_accept_invitation: false }
        if (variant.startsWith('old_')) {
          latest.friend_flow = 'outbound'
          latest.friend_outbound = { id: id(++serial), version: 1,
            status: variant === 'old_prepared' ? 'prepared' : 'unknown',
            can_send: true, can_reconcile: variant !== 'old_prepared',
            target: { native_id: 'SENSITIVE_OLD_TARGET', add_account: 'OLD_ACCOUNT_MUST_NOT_APPEAR', name: '历史目标' } }
        }
        records.set(latest.id, latest); data = progress(latest, true); status = 201
      } else {
        const record = records.get(path.split('/')[3])
        check(record?.platform === platform, 'Mutation crossed challenge/platform')
        if (path.endsWith('/friend-candidate')) {
          check(key && fields(body, ['version', 'nickname']) && body.version === record.version &&
            body.nickname === `本人${platformName(platform)}` && !record.friend_contact,
            'Prepare changed exact nickname, supplied time/identity, or replayed')
          const failed = ['ambiguous', 'none', 'processed', 'outside_window', 'time_unavailable', 'incomplete'].includes(variant)
          if (!failed) record.friend_contact = pendingContact(record)
          data = progress(record)
          await delay(path, req)
          if (failed) return route.fulfill({ status: 409, json: { code: 1,
            msg: variant === 'ambiguous' ? 'group_binding_friend_contact_ambiguous' :
              variant === 'processed' ? 'group_binding_friend_contact_processed' :
              variant === 'outside_window' ? 'group_binding_friend_contact_outside_window' :
              variant === 'time_unavailable' ? 'group_binding_friend_contact_time_unavailable' :
              variant === 'incomplete' ? 'group_binding_friend_contact_incomplete' : 'group_binding_friend_contact_not_found' } })
        } else if (path.endsWith('/friend-approve')) {
          consume(req, { purpose: 'group_binding_friend', platform, target: record.id })
          check(key && fields(body, ['version', 'operation_id']) && body.version === record.version &&
            record.friend_contact?.id === body.operation_id && record.friend_contact.status === 'prepared' &&
            record.friend_contact.can_approve, 'Approval omitted the selected operation/version or replayed')
          record.friend_contact.status = ['unknown', 'lost', 'lost_prepared'].includes(variant) ?
            variant === 'lost_prepared' ? 'prepared' : 'unknown' :
            variant === 'submitting' ? 'submitting' : variant === 'failed' ? 'failed' : 'succeeded'
          record.friend_contact.can_approve = variant === 'lost_prepared'
          record.friend_contact.can_reconcile = ['unknown', 'submitting', 'prepared'].includes(record.friend_contact.status)
          record.version++; data = progress(record)
          await delay(path, req)
          if (variant.startsWith('lost')) return route.abort('failed')
        } else if (path.endsWith('/friend-reject')) {
          consume(req, { purpose: 'group_binding_friend', platform, target: record.id })
          const contact = record.friend_contact
          check(key && fields(body, ['version', 'operation_id']) && body.version === record.version &&
            contact?.id === body.operation_id && contact.action === 'reject_expired' &&
            contact.status === 'prepared' && contact.can_reject,
            'Expired rejection changed the original operation/version or replayed')
          contact.status = ['expired_unknown', 'expired_lost'].includes(variant) ? 'unknown' :
            variant === 'expired_submitting' ? 'submitting' : variant === 'expired_lost_prepared' ? 'prepared' :
            ['expired_failed', 'expired_refused'].includes(variant) ? 'failed' : 'succeeded'
          contact.can_approve = false; contact.can_reject = variant === 'expired_lost_prepared'
          contact.can_reconcile = ['submitting', 'unknown', 'prepared'].includes(contact.status)
          contact.result_code = contact.status === 'succeeded' && variant !== 'expired_no_code' ?
            'native_friend_expired_rejection_acknowledged' :
            contact.status === 'failed' ? 'native_friend_expired_rejection_refused' : ''
          if (contact.status === 'succeeded') {
            record.state = 'cancelled'; record.can_cancel = false; record.version++
            record.blocked_reason = 'group_binding_friend_reapply_required'
          } else if (contact.can_reconcile) {
            record.can_cancel = false
          }
          data = progress(record)
          await delay(path, req)
          if (variant.startsWith('expired_lost')) return route.abort('failed')
          if (variant === 'expired_refused') return route.fulfill({ status: 409,
            json: { code: 1, msg: 'native_friend_expired_rejection_refused' } })
        } else if (/\/friend-attempts\/[^/]+\/reconcile$/.test(path)) {
          check(fields(body, []) && record.friend_contact?.id === path.split('/')[5] &&
            record.friend_contact.can_reconcile, 'Reconcile changed the original operation')
          record.friend_contact.status = 'succeeded'; record.friend_contact.can_approve = false
          if (record.friend_contact.action === 'reject_expired') {
            record.friend_contact.can_reject = false
            record.friend_contact.result_code = 'native_friend_expired_rejection_observed'
            record.state = 'cancelled'; record.can_cancel = false; record.version++
            record.blocked_reason = 'group_binding_friend_reapply_required'
          }
          record.friend_contact.can_reconcile = false
          data = { id: record.friend_contact.id, status: 'succeeded', version: 1,
            result_code: record.friend_contact.result_code || '' }
        } else if (/\/outbound-attempts\/[^/]+\/reconcile$/.test(path)) {
          check(key && fields(body, ['version']) && record.friend_outbound?.id === path.split('/')[5],
            'Historical reconcile changed the original operation')
          record.friend_outbound.status = 'friends'; record.friend_outbound.can_reconcile = false
          data = progress(record)
        } else if (path.endsWith('/confirm')) {
          check(key && fields(body, ['version']) && body.version === record.version &&
            record.identity_proven && record.state === 'proven' && record.can_confirm,
            'Binding bypassed private/group proof or version/key')
          record.state = 'bound'; record.can_confirm = record.can_cancel = false; record.version++
          data = progress(record)
        } else if (path.endsWith('/cancel')) {
          check(key === undefined && fields(body, ['version']) && body.version === record.version &&
            record.can_cancel && !['submitting', 'unknown'].includes(record.friend_contact?.status),
            'Cancellation changed its version or cancelled an unresolved mutation')
          if (record.friend_contact?.status === 'prepared') {
            record.friend_contact.status = 'failed'
            record.friend_contact.result_code = 'group_binding_challenge_cancelled'
            record.friend_contact.can_approve = record.friend_contact.can_reject = false
          }
          record.state = 'cancelled'; record.can_cancel = false; record.version++; data = progress(record)
        } else throw Error('Unexpected outgoing/native/model operation: ' + path)
      }
    }
    await delay(path, req)
    try { await route.fulfill({ status, json: { code: 0, msg: '', data } }) }
    catch (error) { if (!/closed|disposed|cancel|intercept|target/i.test(String(error))) throw error }
  }
  const handler = async route => {
    try { await respond(route) }
    finally {
      const pending = heldRequests.get(route.request())
      if (pending) { pending.finish(); heldRequests.delete(route.request()) }
    }
  }
  const modal = () => page.getByRole('dialog', { name: role === 'superadmin' ? '接入群组' : '接入我的群组', exact: true })
  const captcha = () => page.getByRole('dialog', { name: '安全验证', exact: true })
  const confirmation = () => page.getByRole('dialog', { name: '确认接受好友请求', exact: true })
  const navigate = async (platform, clear = true) => {
    await page.evaluate(({ actor, platform, clear }) => {
      if (clear) sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`, 'user')
      sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
    }, { actor, platform, clear })
    await page.goto(`${base}/#groups`); await page.reload()
    // The dedicated CAPTCHA and confirmation cases retain real modal transitions.
    await page.addStyleTag({ content: '*,*::before,*::after{transition:none!important;animation:none!important}' })
    await page.getByRole('button', { name: '刷新用户中心', exact: true }).waitFor()
  }
  const open = async () => {
    await page.getByRole('button', { name: '接入群组', exact: true }).click()
    await modal().waitFor()
  }
  const close = async () => {
    await modal().locator('.el-dialog__headerbtn').click()
    await modal().waitFor({ state: 'hidden' })
  }
  const solve = async () => {
    const slider = captcha().getByRole('slider', { name: '拼图位置', exact: true })
    await captcha().getByRole('button', { name: '验证', exact: true }).and(page.locator(':enabled')).waitFor()
    // The dialog focus trap activates after the overlay's enter transition.
    const element = await captcha().elementHandle()
    try {
      await page.waitForFunction(dialog => {
        if (!dialog?.isConnected) return false
        for (let current = dialog; current; current = current.parentElement) {
          if ([...current.classList].some(name => /-enter-(?:from|active|to)$/.test(name)) ||
              current.getAnimations().some(animation =>
                animation.pending || !['finished', 'idle'].includes(animation.playState))) return false
        }
        return true
      }, element, { polling: 'raf' })
    } finally {
      await element.dispose()
    }
    await slider.focus()
    const current = Number(await slider.getAttribute('aria-valuenow'))
    check(Number.isFinite(current) && current >= 0 && current <= 256, 'Invalid synthetic slider position')
    for (let step = 0; step < Math.abs(148 - current); step++)
      await slider.press(current < 148 ? 'ArrowRight' : 'ArrowLeft')
    check(Number(await slider.getAttribute('aria-valuenow')) === 148,
      'Keyboard did not reach the synthetic puzzle position')
    await slider.press('Enter'); await captcha().waitFor({ state: 'hidden' })
  }
  const begin = async () => {
    await open(); await modal().getByRole('button', { name: '开始核验', exact: true }).click()
    await solve(); await modal().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
  }
  const prepare = async platform => {
    await modal().getByRole('textbox', { name: '本人完整昵称', exact: true }).fill(`本人${platformName(platform)}`)
    await modal().getByRole('button', { name: '核对近期好友请求', exact: true }).click()
  }
  const selected = () => modal().getByText('已选中请求，尚未接受', { exact: true }).waitFor()
  const approve = async () => {
    await modal().getByRole('button', { name: '接受此好友请求', exact: true }).click()
    await confirmation().getByRole('button', { name: '确认并安全验证', exact: true }).click()
  }
  const refresh = async () => {
    await modal().getByRole('button', { name: '刷新进度', exact: true }).click()
    await page.waitForFunction(() => !document.querySelector('.onboarding-dialog .el-dialog__footer .is-loading'))
  }
  const selectPrivate = async (blocked = false) => {
    await modal().locator('.onboarding-friend-paths label').filter({ hasText: '已是好友，私聊核验' }).click()
    if (blocked) await modal().locator('.onboarding-private-blocked[role="alert"]').waitFor()
    else await modal().getByRole('heading', { name: '私聊核验本人身份', exact: true }).waitFor()
  }
  const safe = async () => {
    check(!await modal().getByRole('button', { name: /查找我的账号|让机器人添加我|确认发送申请|复制好友核验内容/ }).count() &&
      !await modal().getByRole('textbox', { name: '本人可搜索账号', exact: true }).count(),
      'Outgoing search/application or note controls remain')
    check(!/SENSITIVE_|OLD_ACCOUNT_MUST_NOT_APPEAR|LEGACY_NOTE_MUST_NEVER_APPEAR/.test(await modal().innerText()),
      'Raw native identity or legacy friend note reached the UI')
    check(!await page.evaluate(() => [...Object.values(sessionStorage), ...Object.values(localStorage)]
      .some(value => /群接入 |SYNTHETIC_INCOMING_GROUP_PROOF|SENSITIVE_|requested_at|public-wsl-robot|public-lajiaoliao-robot/.test(value))),
      'Proof, raw identity or contact metadata entered persisted drafts')
  }
  const identityPending = async () => {
    check(latest.identity_proven !== true &&
      !['proven', 'bound', 'review'].includes(latest.state) &&
      latest.can_confirm !== true && latest.can_accept_invitation !== true,
      'Contact selection/approval became identity or group proof')
    check(!await modal().getByText('好友身份已核验，可邀请机器人入群', { exact: true }).count(),
      'Unproven contact displayed verified identity')
    const gated = modal().getByRole('button', { name: /^(确认绑定|提交审核|核对群邀请)$/ })
    for (let index = 0; index < await gated.count(); index++)
      check(!await gated.nth(index).isVisible() || await gated.nth(index).isDisabled(),
        'Unproven contact enabled group confirmation or invitation')
  }
  const fit = async (platform, width, branch, dialog = modal()) => {
    await page.setViewportSize({ width, height: 820 })
    const layout = await dialog.evaluateHandle((element, width) => ({
      element, width, previous: '', stableFrames: 0
    }), width)
    try {
      // Measure settled geometry, including transitions on the teleported overlay ancestors.
      await page.waitForFunction(state => {
        const { element, width } = state
        const pending = () => { state.previous = ''; state.stableFrames = 0; return false }
        if (!element.isConnected || innerWidth !== width || innerHeight !== 820) return pending()
        for (let current = element; current; current = current.parentElement) {
          const style = getComputedStyle(current)
          if (style.display === 'none' || style.visibility !== 'visible' || Number(style.opacity) === 0 ||
            [...current.classList].some(name => /-(?:enter|leave)-(?:from|active|to)$/.test(name)) ||
            current.getAnimations().some(animation =>
              animation.pending || !['finished', 'idle'].includes(animation.playState))) return pending()
        }
        const footer = element.querySelector('.el-dialog__footer')
        if (!footer) return pending()
        const snapshot = JSON.stringify([element, footer].map(item => {
          const rect = item.getBoundingClientRect()
          return [rect.x, rect.y, rect.width, rect.height, item.clientWidth, item.scrollWidth]
        }).concat([[document.documentElement.clientWidth, document.documentElement.scrollWidth]]))
        state.stableFrames = snapshot === state.previous ? state.stableFrames + 1 : 0
        state.previous = snapshot
        return state.stableFrames >= 2
      }, layout, { polling: 'raf' })
    } finally { await layout.dispose() }
    const box = await dialog.boundingBox(), footer = await dialog.locator('.el-dialog__footer').boundingBox()
    check(box && box.x >= -1 && box.x + box.width <= width + 1 && box.y >= -1 &&
      box.y + box.height <= 821 && footer && footer.y >= 0 && footer.y + footer.height <= 821 &&
      !await dialog.evaluate(el => el.scrollWidth > el.clientWidth + 1) &&
      !await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1),
      `Unbounded ${branch} UI at ${platform}/${width}`)
    check(await dialog.locator('.el-dialog__body').evaluate(el =>
      [...el.querySelectorAll('p, dd, code, .el-alert__title, .el-radio-button__inner')].every(item =>
        item.scrollWidth <= item.clientWidth + 1)), `Text overflow in ${branch}/${width}`)
    const file = `output/playwright/onboarding-incoming-${platform}-${branch}-${width}.png`
    await page.screenshot({ path: file, animations: 'disabled' }); screenshots.push(file)
  }
  await page.route('**/api/v1/**', handler)
  try {
    for (const platform of platforms) {
      variant = 'normal'; await navigate(platform)
      const beforeView = writes.length
      await open(); await close()
      check(writes.length === beforeView, 'Viewing started onboarding')
      await begin()
      await modal().getByText(`public-${platform}-robot`, { exact: true }).waitFor()
      await modal().getByRole('button', { name: '复制机器人加好友账号', exact: true }).waitFor()
      const beforeGroupChallenge = writes.length
      await modal().getByRole('region', { name: '群内归属核验', exact: true })
        .getByText(groupProof, { exact: true }).waitFor()
      await identityPending()
      check(writes.length === beforeGroupChallenge, 'Displaying a group challenge submitted proof')
      check(!await modal().getByText(privateProof(platform), { exact: true }).count(),
        'Incoming branch skipped contact confirmation')
      const beforePrepare = writes.length, beforeCaptcha = captchas.length
      await prepare(platform); await selected()
      check(writes.length === beforePrepare + 1 && writes.at(-1).path.endsWith('/friend-candidate') &&
        captchas.length === beforeCaptcha, 'One prepare approved a friend or opened captcha')
      await identityPending(); await safe()
      for (const width of viewports) await fit(platform, width, 'candidate')
      await modal().getByRole('button', { name: '接受此好友请求', exact: true }).click()
      await confirmation().getByText(`本人${platformName(platform)}`, { exact: true }).waitFor()
      check(writes.length === beforePrepare + 1 && captchas.length === beforeCaptcha,
        'Opening confirmation submitted approval')
      for (const width of viewports) await fit(platform, width, 'confirm', confirmation())
      await confirmation().getByRole('button', { name: '取消', exact: true }).click()
      await approve(); await captcha().waitFor()
      check(writes.length === beforePrepare + 1, 'Approved before captcha')
      for (const width of viewports) await fit(platform, width, 'captcha', captcha())
      await captcha().getByRole('button', { name: '取消', exact: true }).click()
      await captcha().waitFor({ state: 'hidden' })
      check(writes.length === beforePrepare + 1, 'Cancelled captcha approved a request')
      await modal().getByRole('button', { name: '接受此好友请求', exact: true }).click()
      const button = confirmation().getByRole('button', { name: '确认并安全验证', exact: true })
      await button.evaluate(el => { el.click(); el.click() })
      await solve()
      await modal().getByRole('heading', { name: '私聊核验本人身份', exact: true }).waitFor()
      check(writes.length === beforePrepare + 2 && writes.at(-1).path.endsWith('/friend-approve') &&
        consumed.at(-1).purpose === 'group_binding_friend' && consumed.at(-1).target === latest.id &&
        !await modal().getByRole('button', { name: '接受此好友请求', exact: true }).count(),
        'Approval was replayed or lost challenge-scoped captcha')
      await modal().getByText(privateProof(platform), { exact: true }).waitFor()
      await identityPending()
      const beforeRefresh = writes.length
      await refresh(); check(writes.length === beforeRefresh, 'GET refreshed native contact state')
      for (const width of viewports) await fit(platform, width, 'private-after-approve')
      latest.identity_proven = true; latest.version++; await refresh()
      await modal().getByRole('heading', { name: '证明群归属', exact: true }).waitFor()
      await modal().getByText(groupProof, { exact: true }).waitFor()
      check(!await modal().getByRole('button', { name: '确认绑定', exact: true }).count(),
        'Private proof alone bound a group')
      latest.state = 'proven'; latest.can_confirm = true; latest.proof_role = 'owner'; latest.version++
      latest.group = { id: id(50), name: '合成待绑定群', native_id: 'synthetic-public-group' }; await refresh()
      await modal().getByRole('button', { name: '确认绑定', exact: true }).click()
      await page.getByRole('dialog', { name: '确认群归属', exact: true })
        .getByRole('button', { name: '确认绑定', exact: true }).click()
      await modal().getByText('绑定成功', { exact: true }).waitFor()
      await safe(); await close()
      variant = 'normal'; await navigate(platform); await begin()
      const beforePrivate = writes.length, privateCaptcha = captchas.length
      await selectPrivate(); await modal().getByText(privateProof(platform), { exact: true }).waitFor()
      await refresh()
      check(writes.length === beforePrivate && captchas.length === privateCaptcha,
        'Already-friend path searched or approved a contact')
      for (const width of viewports) await fit(platform, width, 'already-friend')
      await close()
      variant = 'expired'; await navigate(platform); await begin()
      check((await modal().innerText()).includes('超过10分钟的旧申请默认单条拒绝'),
        'Expired default rejection was not explained before preparation')
      const expiredBefore = writes.length, expiredCaptchaBefore = captchas.length
      await modal().getByRole('textbox', { name: '本人完整昵称', exact: true }).fill(`本人${platformName(platform)}`)
      await modal().getByRole('button', { name: '核对近期好友请求', exact: true })
        .evaluate(el => { el.click(); el.click() })
      await captcha().waitFor()
      const expiredOperation = latest.friend_contact.id, expiredChallenge = latest.id
      check(writes.length === expiredBefore + 1 && captchas.length === expiredCaptchaBefore + 1 &&
        !await confirmation().isVisible() && !await modal().getByRole('button', { name: '接受此好友请求', exact: true }).count(),
        'Expired selection replayed, required approval or submitted before captcha')
      for (const width of viewports) await fit(platform, width, 'expired-reject-captcha', captcha())
      await captcha().getByRole('button', { name: '取消', exact: true }).click()
      await captcha().waitFor({ state: 'hidden' })
      await modal().getByRole('button', { name: '处理过期申请', exact: true }).and(page.locator(':enabled')).waitFor()
      await modal().getByText('已选中过期申请，尚未拒绝', { exact: true }).waitFor()
      check(!(await modal().innerText()).includes('过期申请拒绝结果待核对') &&
        !await modal().getByRole('button', { name: '核对原好友审批', exact: true }).count(),
        'Unsubmitted rejection was described as an unknown native result')
      await selectPrivate(true)
      await modal().getByText('已选中过期申请，尚未提交拒绝，也未同意此申请。', { exact: true }).waitFor()
      check(!await modal().getByText(privateProof(platform), { exact: true }).count() &&
        !await modal().getByRole('heading', { name: '私聊核验本人身份', exact: true }).count(),
        'Expired selection exposed a private proof that the server cannot accept')
      for (const width of viewports) await fit(platform, width, 'expired-private-blocked')
      await modal().locator('.onboarding-friend-paths').getByText('我添加机器人', { exact: true }).click()
      for (const width of viewports) await fit(platform, width, 'expired-prepared')
      await refresh(); await close(); await navigate(platform, false); await open()
      await modal().getByRole('button', { name: '处理过期申请', exact: true }).and(page.locator(':enabled')).waitFor()
      await page.clock.fastForward(5000); await refresh()
      check(writes.length === expiredBefore + 1 && captchas.length === expiredCaptchaBefore + 1 &&
        latest.friend_contact.id === expiredOperation, 'GET/draft/timer rejected or refreshed an expired plan')
      await modal().getByRole('button', { name: '处理过期申请', exact: true })
        .evaluate(el => { el.click(); el.click() })
      await solve(); await modal().getByText('拒绝已确认', { exact: true }).waitFor()
      check(writes.length === expiredBefore + 2 && writes.at(-1).path.endsWith('/friend-reject') &&
        writes.at(-1).body.operation_id === expiredOperation && latest.id === expiredChallenge &&
        latest.state === 'cancelled' && latest.version === 2 &&
        latest.blocked_reason === 'group_binding_friend_reapply_required' &&
        !(await modal().innerText()).includes('group_binding_friend_reapply_required') &&
        !await modal().getByRole('heading', { name: '私聊核验本人身份', exact: true }).count() &&
        (await modal().innerText()).includes('请重新开始核验，并在有效时间内重新发送好友申请'),
        'Expired rejection lost plan, terminal version or reapplication guidance')
      await modal().getByText(`本人${platformName(platform)}`, { exact: true }).waitFor()
      for (const width of viewports) await fit(platform, width, 'expired-rejected')
      await identityPending(); await safe()
      const rejectedWrites = writes.length
      await refresh(); await close()
      check(writes.length === rejectedWrites, 'Refreshing/closing a rejected record submitted work')
      await navigate(platform); await begin(); await prepare(platform); await solve()
      await modal().getByText('拒绝已确认', { exact: true }).waitFor()
      const rejectedKey = writes.at(-1).key, restartBefore = writes.length
      await modal().getByRole('button', { name: '重新开始核验', exact: true }).click()
      check(writes.length === restartBefore, 'Rejected restart bypassed create captcha')
      await solve(); await modal().getByRole('textbox', { name: '本人完整昵称', exact: true }).waitFor()
      check(writes.length === restartBefore + 1 && writes.at(-1).path === '/group-binding/challenges' &&
        writes.at(-1).key !== rejectedKey, 'Rejected restart reused a mutation or old challenge')
      await close()
      variant = 'expired'; await navigate(platform); await begin()
      await prepare(platform); await captcha().waitFor()
      await captcha().getByRole('button', { name: '取消', exact: true }).click()
      await captcha().waitFor({ state: 'hidden' })
      await selectPrivate(true)
      const cancelledChallenge = latest.id, cancelledOperation = latest.friend_contact.id,
        cancelBefore = writes.length, cancelCaptchaBefore = captchas.length
      await modal().getByRole('button', { name: '取消本次接入', exact: true }).click()
      await page.getByRole('dialog', { name: '取消本次接入', exact: true })
        .getByRole('button', { name: '确认取消', exact: true }).click()
      await modal().getByRole('button', { name: '重新开始核验', exact: true }).and(page.locator(':enabled')).waitFor()
      check(writes.length === cancelBefore + 1 && writes.at(-1).path.endsWith('/cancel') &&
        latest.state === 'cancelled' && latest.friend_contact.id === cancelledOperation &&
        latest.friend_contact.status === 'failed' && captchas.length === cancelCaptchaBefore &&
        !writes.slice(cancelBefore).some(item => /\/friend-(approve|reject)$/.test(item.path)),
        'Cancelling an unsubmitted record sent a native effect or silently started a new challenge')
      await refresh()
      check(writes.length === cancelBefore + 1 && latest.id === cancelledChallenge,
        'Refreshing a cancelled record created a challenge')
      await modal().getByRole('button', { name: '重新开始核验', exact: true }).click()
      check(writes.length === cancelBefore + 1, 'Manual cancellation bypassed create verification')
      await solve(); await selectPrivate()
      await modal().getByText(privateProof(platform), { exact: true }).waitFor()
      check(latest.id !== cancelledChallenge && !latest.friend_contact &&
        writes.length === cancelBefore + 2 && writes.at(-1).path === '/group-binding/challenges',
        'Explicit restart retained an expired plan or did not allow existing-friend verification')
      await close()
      for (const value of ['expired_unknown', 'expired_submitting', 'expired_failed', 'expired_refused',
        'expired_lost', 'expired_lost_prepared', 'expired_no_code', 'expired_closed']) {
        variant = value; await navigate(platform); await begin()
        const before = writes.length, captchaBefore = captchas.length
        await prepare(platform)
        if (value === 'expired_closed') {
          await modal().getByText('已选中过期申请，尚未拒绝', { exact: true }).waitFor()
          check(await modal().getByRole('button', { name: '处理过期申请', exact: true }).isDisabled() &&
            captchas.length === captchaBefore && writes.length === before + 1,
            'Closed expired admission started captcha or rejection')
          await close(); continue
        }
        await solve()
        await page.waitForFunction(() => !document.querySelector('.onboarding-dialog .is-loading'))
        await refresh(); const submitted = writes.length, submittedCaptchas = captchas.length
        if (value === 'expired_no_code') {
          const restart = modal().getByRole('button', { name: '重新开始核验', exact: true })
          check(!await modal().getByText('拒绝已确认', { exact: true }).count() &&
            (!await restart.count() || await restart.isDisabled()),
            'Missing rejection receipt became success or restart')
          if (await restart.count()) {
            await restart.evaluate(button => button.click())
            check(writes.length === submitted && captchas.length === submittedCaptchas,
              'Disabled restart replayed a rejection with no confirmed receipt')
          }
        }
        await close(); await navigate(platform, false); await open()
        await modal().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
        check(writes.length === submitted && captchas.length === submittedCaptchas &&
          !await modal().getByRole('button', { name: /^(处理过期申请|接受此好友请求)$/ }).count() &&
          !await modal().getByText('拒绝已确认', { exact: true }).count() &&
          !await modal().getByText('好友申请已接受', { exact: true }).count(),
          'Failed/unknown rejection replayed or claimed success')
        if (['expired_failed', 'expired_refused'].includes(value)) {
          check((await modal().innerText()).includes('过期申请未处理成功'), 'Refusal lacked failure feedback')
          for (const width of viewports) await fit(platform, width, value)
        } else if (value !== 'expired_no_code') {
          check(latest.state === 'waiting' && !latest.can_cancel &&
            !await modal().getByRole('button', { name: '取消本次接入', exact: true }).count(),
            'Unknown expired rejection could cancel the waiting challenge')
          await modal().getByRole('button', { name: '核对原好友审批', exact: true }).click()
          await modal().getByText('拒绝已确认', { exact: true }).waitFor()
          check(writes.length === submitted + 1 && writes.at(-1).path.endsWith('/reconcile') &&
            latest.state === 'cancelled' && latest.friend_contact.result_code === 'native_friend_expired_rejection_observed' &&
            !await modal().getByRole('button', { name: '重新开始核验', exact: true }).isDisabled(),
            'Rejection reconciliation replayed or lost cancelled progress')
        }
        await identityPending(); await close()
      }
      for (const value of ['ambiguous', 'none', 'processed', 'outside_window', 'time_unavailable', 'incomplete']) {
        variant = value; await navigate(platform); await begin()
        const before = writes.length, captchaBefore = captchas.length
        await prepare(platform); await modal().locator('.onboarding-error').waitFor()
        const message = await modal().locator('.onboarding-error').innerText()
        check(message.includes(value === 'none' ? '机器人本次返回的申请列表' : value === 'ambiguous' ? '多个' :
          value === 'processed' ? '同昵称申请记录已处理' :
          value === 'outside_window' ? '申请时间不在本次接入范围' :
          value === 'incomplete' ? '列表过多' : '时间缺失') &&
          !/native_onboarding_contact_|group_binding_friend_contact_/.test(message) &&
          !await modal().getByRole('button', { name: '接受此好友请求', exact: true }).count(),
          'No-match/ambiguous selection became approvable')
        if (value === 'processed')
          check(message.includes('历史“已同意”不代表当前仍是好友'), 'Processed history was treated as current friendship')
        for (const width of viewports) await fit(platform, width, value + '-error')
        await page.clock.fastForward(5000); await refresh()
        check(writes.length === before + 1 && captchas.length === captchaBefore, 'Prepare retried itself')
        await selectPrivate(); await modal().getByText(privateProof(platform), { exact: true }).waitFor()
        for (const width of viewports) await fit(platform, width, `${value}-private`)
        await identityPending(); await safe(); await close()
      }
      for (const value of ['unknown', 'submitting', 'failed', 'lost', 'lost_prepared']) {
        variant = value; await navigate(platform); await begin(); await prepare(platform); await selected()
        await approve(); await solve()
        await page.waitForFunction(() => !document.querySelector('.onboarding-dialog .is-loading'))
        const before = writes.length
        await refresh(); await close(); await navigate(platform, false); await open()
        await modal().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
        check(writes.length === before && !await modal().getByRole('button', { name: '接受此好友请求', exact: true }).count(),
          'Unknown/failed approval became replayable after reload')
        if (value !== 'failed') {
          await modal().getByRole('button', { name: '核对原好友审批', exact: true }).click()
          await modal().getByRole('heading', { name: '私聊核验本人身份', exact: true }).waitFor()
          check(writes.length === before + 1 && writes.at(-1).path.endsWith('/reconcile'),
            'Reconcile replayed approval')
        } else await selectPrivate()
        await identityPending(); await close()
      }
      for (const value of ['closed', 'no_account', 'missing_private', 'old_prepared', 'old_unknown']) {
        variant = value; await navigate(platform); await begin()
        const before = writes.length, captchaBefore = captchas.length
        if (value === 'closed') {
          await prepare(platform); await selected()
          check(await modal().getByRole('button', { name: '接受此好友请求', exact: true }).isDisabled(),
            'Closed incoming admission enabled approval')
          await selectPrivate()
        } else if (value.startsWith('old_')) {
          await modal().getByRole('heading', { name: '旧版发出好友申请记录', exact: true }).waitFor()
          check(!await modal().getByRole('button', { name: /接受此好友请求|核对近期好友请求/ }).count(),
            'Old prepared outgoing record became actionable')
          if (value === 'old_unknown') await modal().getByRole('button', { name: '核对原好友申请', exact: true }).click()
        } else await selectPrivate()
        if (value === 'missing_private')
          await modal().getByText(/身份核验内容仅在首次签发时显示/).waitFor()
        else await modal().getByText(privateProof(platform), { exact: true }).waitFor()
        check(writes.length === before + (['closed', 'old_unknown'].includes(value) ? 1 : 0) &&
          captchas.length === captchaBefore, 'Fallback/history started an approval or outgoing lookup')
        await safe(); await close()
      }
      variant = 'unknown'; await page.clock.setSystemTime(new Date())
      expiryDeadline = new Date(Date.now() + 60000).toISOString()
      await navigate(platform); await begin(); await prepare(platform); await selected(); await approve(); await solve()
      await modal().getByText('好友审批结果待核对', { exact: true }).waitFor()
      await page.clock.fastForward(61000); await modal().locator('.onboarding-expired').waitFor()
      check(await modal().getByRole('button', { name: '重新开始核验', exact: true }).isDisabled(),
        'Expired unknown approval could be discarded by restart')
      const beforeExpiredReconcile = writes.length
      await close(); await open()
      await modal().getByRole('button', { name: '核对原好友审批', exact: true }).click()
      await modal().getByText('好友申请已接受', { exact: true }).waitFor()
      check(writes.length === beforeExpiredReconcile + 1 &&
        !await modal().getByRole('button', { name: '重新开始核验', exact: true }).isDisabled(),
        'Expired approval reconciliation replayed or failed to clear the pending fence')
      await close()
      // Expiry has a local deadline; no timer polls native APIs or the challenge.
      for (const width of viewports) {
        variant = 'normal'; await page.clock.setSystemTime(new Date())
        expiryDeadline = new Date(Date.now() + 60000).toISOString()
        await page.setViewportSize({ width, height: 820 }); await navigate(platform); await begin()
        const before = writes.length, readBefore = reads.length, captchaBefore = captchas.length,
          unexpectedBefore = unexpected.length, challenge = latest.id
        await page.clock.fastForward(61000)
        await modal().locator('.onboarding-expired').waitFor()
        check(await modal().locator('.onboarding-expired.el-alert--error[role="alert"]').count() &&
          await modal().locator('.onboarding-expired .el-alert__icon').count() &&
          await modal().locator('.onboarding-state .el-tag--danger').count() &&
          !await modal().getByRole('textbox', { name: '本人完整昵称', exact: true }).count() &&
          !await modal().getByRole('button', { name: /复制身份核验内容|复制群归属核验内容/ }).count(),
          'Expiry was not immediate/red or left active proof controls')
        // The app refreshes its shell every 15 seconds; expiry must remain local to the challenge.
        const readDelta = reads.slice(readBefore)
        const expiryRequest = {
          platform, width, background_reads: readDelta.filter(read => backgroundReadPaths.has(read.path)).map(read => read.path),
          challenge_reads: readDelta.filter(read => read.path === `/group-binding/challenges/${challenge}`).length,
          other_reads: readDelta.filter(read => !backgroundReadPaths.has(read.path)).map(read => read.path),
          writes: writes.length - before, captcha_requests: captchas.length - captchaBefore,
          unexpected_requests: unexpected.slice(unexpectedBefore)
        }
        expiryRequests.push(expiryRequest)
        console.log(JSON.stringify({ diagnostic: 'onboarding_expiry_requests', ...expiryRequest }))
        check(expiryRequest.challenge_reads === 0 && expiryRequest.other_reads.length === 0 &&
          expiryRequest.writes === 0 && expiryRequest.captcha_requests === 0 && expiryRequest.unexpected_requests.length === 0,
          'Expiry started challenge/native/captcha work: ' + JSON.stringify(expiryRequest))
        await fit(platform, width, 'expired')
        await page.clock.setSystemTime(new Date()); expiryDeadline = new Date(Date.now() + 600000).toISOString()
        const original = latest.id, key = writes.at(-1).key
        await modal().getByRole('button', { name: '重新开始核验', exact: true }).click()
        check(writes.length === before, 'Restart bypassed new captcha')
        await solve(); await modal().getByRole('textbox', { name: '本人完整昵称', exact: true }).waitFor()
        check(latest.id !== original && writes.at(-1).key !== key, 'Restart reused expired challenge/key')
        await close()
      }
      for (const phase of ['confirmation', 'captcha', 'prepare-success', 'prepare-error', 'captcha-verify', 'approve-response',
        'expired-prepare', 'expired-captcha', 'expired-verify', 'expired-reject-response']) {
        console.log(JSON.stringify({ diagnostic: 'onboarding_expiry_late_case', platform, phase }))
        variant = phase.startsWith('expired-') ? 'expired' : phase === 'prepare-error' ? 'none' : 'normal'
        await page.clock.setSystemTime(new Date()); expiryDeadline = new Date(Date.now() + 60000).toISOString()
        await navigate(platform); await begin()
        let late
        if (phase.startsWith('prepare-') || phase === 'expired-prepare') {
          const path = `/group-binding/challenges/${latest.id}/friend-candidate`
          late = holdNext(path)
          await prepare(platform); await bounded(late.started, 'Prepare did not reach fixture: ' + path)
        } else {
          await prepare(platform)
          if (!phase.startsWith('expired-')) await selected()
          if (phase === 'confirmation') await modal().getByRole('button', { name: '接受此好友请求', exact: true }).click()
          else {
            if (!phase.startsWith('expired-')) await approve()
            await captcha().waitFor()
            if (['captcha-verify', 'approve-response', 'expired-verify', 'expired-reject-response'].includes(phase)) {
              const path = ['captcha-verify', 'expired-verify'].includes(phase) ? '/human-verification/verify' :
                `/group-binding/challenges/${latest.id}/${phase.startsWith('expired-') ? 'friend-reject' : 'friend-approve'}`
              late = holdNext(path)
              const slider = captcha().getByRole('slider', { name: '拼图位置', exact: true })
              await captcha().getByRole('button', { name: '验证', exact: true }).and(page.locator(':enabled')).waitFor()
              await slider.focus()
              for (let x = 128; x < 148; x++) await slider.press('ArrowRight')
              await slider.press('Enter'); await bounded(late.started, 'Approval/captcha did not reach fixture: ' + path)
            }
          }
        }
        const before = writes.length
        await page.clock.fastForward(61000)
        if (phase !== 'expired-reject-response') await modal().locator('.onboarding-expired').waitFor()
        await confirmation().waitFor({ state: 'hidden' }); await captcha().waitFor({ state: 'hidden' })
        if (late) {
          late.release(); release = undefined; await settleLateRequest(late)
          await page.waitForFunction(() => !document.querySelector('.onboarding-dialog .is-loading'))
        }
        check(writes.length === before && !await modal().locator('.onboarding-error').count() &&
          !await modal().getByRole('button', { name: /接受此好友请求|复制身份核验内容/ }).count(),
          `Expiry/late ${phase} resurrected work or proof`)
        await close()
      }
      variant = 'normal'; await page.clock.setSystemTime(new Date())
      expiryDeadline = new Date(Date.now() + 60000).toISOString()
      await navigate(platform); await begin()
      latest.identity_proven = true; latest.state = 'proven'; latest.can_confirm = true; latest.proof_role = 'owner'
      latest.group = { id: id(51), name: '合成时钟跳变群', native_id: 'synthetic-clock-group' }
      await refresh(); await modal().getByRole('button', { name: '确认绑定', exact: true }).click()
      const bindingConfirmation = page.getByRole('dialog', { name: '确认群归属', exact: true })
      const beforeClockJump = writes.length
      await page.clock.setSystemTime(new Date(Date.now() + 61000))
      await page.evaluate(() => window.dispatchEvent(new Event('focus')))
      await bindingConfirmation.waitFor({ state: 'hidden' }); await modal().locator('.onboarding-expired').waitFor()
      check(writes.length === beforeClockJump, 'Background clock jump submitted an expired binding')
      await close()
      for (const state of ['bound', 'review']) {
        await page.clock.setSystemTime(new Date()); expiryDeadline = new Date(Date.now() + 60000).toISOString()
        await navigate(platform); await begin()
        latest.state = state; latest.expires_at = new Date(Date.now() - 60000).toISOString()
        latest.can_cancel = false; latest.can_confirm = false; await refresh()
        await modal().getByText(state === 'bound' ? '绑定成功' : '待平台审核', { exact: true }).waitFor()
        check(!await modal().locator('.onboarding-expired').count(), 'Completed/review state became expired')
        await close()
      }
      expiryDeadline = undefined
    }
    // Closing and changing platform/actor while a prepare or captcha response is held.
    for (const phase of ['candidate', 'captcha', 'expired-candidate', 'expired-captcha', 'expired-reject']) {
      variant = phase.startsWith('expired-') ? 'expired' : 'normal'
      await page.clock.setSystemTime(new Date()); await navigate('wsl'); await begin()
      const prior = latest.id
      let path
      if (phase.endsWith('candidate')) path = `/group-binding/challenges/${prior}/friend-candidate`
      else {
        await prepare('wsl')
        if (!phase.startsWith('expired-')) await selected()
        path = phase === 'expired-reject' ? `/group-binding/challenges/${prior}/friend-reject` : '/human-verification/verify'
      }
      const late = holdNext(path)
      if (phase.endsWith('candidate')) await prepare('wsl')
      else {
        if (!phase.startsWith('expired-')) await approve()
        const slider = captcha().getByRole('slider', { name: '拼图位置', exact: true })
        await captcha().getByRole('button', { name: '验证', exact: true }).and(page.locator(':enabled')).waitFor()
        await slider.focus()
        for (let x = 128; x < 148; x++) await slider.press('ArrowRight')
        await slider.press('Enter')
      }
      await bounded(late.started, 'Scope-switch request did not reach fixture: ' + path)
      if (phase.endsWith('captcha')) await captcha().getByRole('button', { name: '取消', exact: true }).click()
      await close()
      const before = writes.length
      actor = id(5); await navigate('lajiaoliao')
      late.release(); release = undefined; await settleLateRequest(late)
      await begin()
      check(latest.platform === 'lajiaoliao' && latest.id !== prior &&
        !await modal().getByText('本人旺商聊', { exact: true }).count() &&
        writes.length === before + 1, 'Late prepare/captcha crossed actor or platform')
      await safe(); await close(); actor = id(1)
    }
    // Draft nickname and selected branch survive a same-scope close, and never cross platforms.
    variant = 'normal'
    await navigate('wsl'); await begin()
    await modal().getByRole('textbox', { name: '本人完整昵称', exact: true }).fill('尚未提交的昵称')
    await close(); const draftWrites = writes.length; await open()
    await modal().getByRole('textbox', { name: '本人完整昵称', exact: true }).waitFor()
    check(await modal().getByRole('textbox', { name: '本人完整昵称', exact: true }).inputValue() === '尚未提交的昵称' &&
      writes.length === draftWrites, 'Same actor/platform draft was lost or submitted')
    await selectPrivate(); await close(); await open()
    await modal().getByRole('heading', { name: '私聊核验本人身份', exact: true }).waitFor()
    await close(); await navigate('lajiaoliao', false); await begin()
    check(await modal().getByRole('textbox', { name: '本人完整昵称', exact: true }).inputValue() === '',
      'Nickname draft leaked across platforms')
    await close()
    role = 'superadmin'
    for (const platform of platforms) {
      await navigate(platform); await begin()
      check(writes.at(-1).body.target_tenant_id === operatorSpace.id &&
        latest.customer.tenant_id === operatorSpace.id, 'Operator own workspace targeted a customer')
      const ownWrites = writes.length
      await selectPrivate(); await refresh()
      check(writes.length === ownWrites, 'Operator private path searched or submitted work')
      await close(); await navigate(platform); await open()
      await modal().locator('label.el-radio').filter({ hasText: '代客户接入' }).click()
      check(await modal().getByRole('button', { name: '开始核验', exact: true }).isDisabled(),
        'Operator customer onboarding omitted explicit customer selection')
      await modal().getByRole('combobox', { name: '群组所属客户', exact: true }).click()
      await page.getByRole('option', { name: workspace.name, exact: true }).click()
      await modal().getByRole('button', { name: '开始核验', exact: true }).click(); await solve()
      await modal().getByRole('textbox', { name: '本人完整昵称', exact: true }).waitFor()
      check(writes.at(-1).body.target_tenant_id === workspace.id &&
        latest.customer.tenant_id === workspace.id, 'Operator lost selected customer ownership')
      const beforePrepare = writes.length
      await prepare(platform); await selected()
      check(writes.length === beforePrepare + 1 && !latest.identity_proven,
        'Operator contact selection silently approved or proved identity')
      await safe(); await close(); await open()
      await modal().getByRole('button', { name: '开始核验', exact: true }).waitFor()
      check(!await modal().getByText('已选中请求，尚未接受', { exact: true }).count(),
        'Selected-customer challenge leaked into the default own workspace')
      await close()
    }
    // The admin editor uses incoming gates only; opening is a saved-state read.
    role = 'superadmin'
    for (const platform of platforms) {
      const account = accounts.find(a => a.platform === platform)
      await page.evaluate(({ actor, platform, account }) => {
        sessionStorage.clear(); sessionStorage.setItem(`wsl:console:${actor}`, 'admin')
        sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
        sessionStorage.setItem(`wsl:account:${actor}`, account.id)
        sessionStorage.setItem(`wsl:account:${actor}:${platform}`, account.id)
      }, { actor, platform, account })
      await page.goto(base + '/#overview'); await page.reload()
      await page.addStyleTag({ content: '*,*::before,*::after{transition:none!important;animation:none!important}' })
      const dashboard = page.getByTestId('account-dashboard')
      await dashboard.locator('.account-kpis').waitFor()
      await dashboard.getByRole('button', { name: '好友接入准入', exact: true }).waitFor()
      const before = writes.length
      await dashboard.getByRole('button', { name: '好友接入准入', exact: true }).click()
      const gate = page.getByRole('dialog', { name: '来向好友审批准入', exact: true })
      await gate.getByText('版本 0', { exact: true }).waitFor()
      check(writes.length === before && reads.at(-1).path.includes('/friend-gates/'), 'Viewing gate submitted a change')
      check(!/精确查找|发送好友申请|3 次\/小时/.test(await gate.innerText()), 'Outgoing editor remains')
      for (const width of viewports) await fit(platform, width, 'incoming-gate', gate)
      await gate.getByRole('button', { name: '编辑准入', exact: true }).click()
      await gate.getByRole('combobox', { name: '来向好友审批准入状态', exact: true })
        .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
      await page.getByRole('option', { name: '开放', exact: true }).click()
      await gate.getByRole('textbox', { name: '来向好友审批证据引用', exact: true }).fill('acceptance:SYNTHETIC_INCOMING')
      await gate.getByRole('button', { name: '保存准入', exact: true }).click()
      await gate.getByText('来向好友审批准入已保存', { exact: true }).waitFor()
      check(writes.length === before + 1 && writes.at(-1).path.endsWith('/friend-gates/' + account.id),
        'Gate saved an outgoing admission')
      await gate.getByRole('button', { name: '关闭', exact: true }).click()
    }
    check(!unexpected.length, 'Unmocked APIs: ' + JSON.stringify(unexpected))
    check(!writes.some(w => /friend-target|friend-send|friend-verify|outbound-friend-gates/.test(w.path)),
      'Retired outgoing/note endpoint called')
    return { passed: true, synthetic_only: true, platforms, viewports,
      incoming_only: true, prepare_without_approval: true, exact_nickname_server_interval: true,
      confirmation_and_scoped_captcha: true, independent_private_and_group_proofs: true,
      expired_default_single_rejection: true, expired_cancel_resume_same_plan: true,
      expired_get_draft_timer_readonly: true, expired_failure_unknown_no_replay: true,
      expired_rejection_receipts_and_restart: true,
      ambiguous_missing_untrusted_time_and_incomplete_safe: true, existing_friend_no_search: true,
      operator_own_workspace_and_explicit_customer: true,
      unknown_no_replay: true, historical_outbound_readonly: true, incoming_gate_only: true,
      local_expiry_and_late_response_guards: true, expiry_requests: expiryRequests, actor_platform_drafts: true,
      bounded_dialogs: true, native_calls: 0, model_calls: 0,
      writes: writes.length, reads: reads.length, screenshots }
  } finally {
    release?.()
    await page.unroute('**/api/v1/**', handler)
  }
}

const unit = async () => {
  const fs = process.getBuiltinModule('fs'), path = process.getBuiltinModule('path')
  const { createRequire } = process.getBuiltinModule('module')
  const deps = process.env.WSL_UI_NODE_MODULES
  if (!deps || !path.isAbsolute(deps)) throw Error('WSL_UI_NODE_MODULES must name existing test dependencies')
  const require = createRequire(path.join(deps, 'package.json'))
  const vue = require('vue'), ts = require('typescript'), compiler = require('@vue/compiler-sfc')
  const root = path.resolve(path.dirname(process.argv[1]), '..')
  const assert = process.getBuiltinModule('assert/strict')
  const source = fs.readFileSync(path.join(root, 'web/src/wsl/CustomerOnboarding.vue'), 'utf8')
  const descriptor = compiler.parse(source).descriptor
  const compiled = compiler.compileScript(descriptor, { id: 'incoming-unit' }).content
  const template = compiler.compileTemplate({ source: descriptor.template.content, filename: 'CustomerOnboarding.vue',
    id: 'incoming-unit', compilerOptions: { bindingMetadata: compiler.compileScript(descriptor, { id: 'incoming-unit' }).bindings } })
  assert.deepEqual(template.errors, [])
  class ApiError extends Error {
    constructor(code, status = 409) { super(code); this.code = code; this.status = status }
  }
  const transpile = source => ts.transpileModule(source, { compilerOptions: {
    target: ts.ScriptTarget.ES2022, module: ts.ModuleKind.CommonJS, esModuleInterop: true } }).outputText
  const customerModule = { exports: {} }
  new Function('require', 'module', 'exports', transpile(fs.readFileSync(path.join(root, 'web/src/wsl/customer.ts'), 'utf8')))(
    name => { assert.equal(name, './api'); return { errorText: code => code } }, customerModule, customerModule.exports)
  const { onboardingFriendContact, robotAddAccount } = customerModule.exports
  const deferred = () => { let resolve; const promise = new Promise(done => { resolve = done }); return { promise, resolve } }
  const clone = value => JSON.parse(JSON.stringify(value))
  let assertions = 0
  const verified = () => assertions++
  const harness = async (platform = 'wsl', existingSession, action = 'approve', seed) => {
    let now = Date.parse('2026-11-08T12:00:00Z'), serial = 1, state, destroyed = false
    const timers = new Map(), writes = [], reads = [], captcha = [], events = []
    const session = existingSession || vue.reactive({ user: { id: 'actor', tenant_id: 'tenant', role: 'user', tenant_role: 'owner' },
      permissions: ['claims:create'], console: 'user', workspace: platform, selectedAccount: '', drafts: {},
      saveDraft(key, value) { this.drafts[key] = clone(value) } })
    const record = seed ? clone(seed) : { id: `challenge-${platform}`, platform, version: 1, state: 'waiting', friend_flow: 'incoming_contact',
      expires_at: new Date(now + 60000).toISOString(), robot: { id: 'robot', name: '机器人', native_id: 'PRIVATE_ROBOT',
        add_account: 'public-robot', add_available: true }, can_cancel: true, identity_proven: false }
    const contact = () => ({ id: 'operation', version: 1, status: 'prepared', name: '本人昵称',
      requested_at: new Date(now - (action === 'reject_expired' ? 660000 : 1000)).toISOString(),
      ...(action === 'reject_expired' ? { action, can_reject: true } : {}),
      can_approve: action === 'approve', can_reconcile: false })
    let apiHandler = async (url, method) => {
      if (method === 'GET') return url.startsWith('/group-binding/context') ? { customers: [], next_offset: -1 } : clone(record)
      if (url.endsWith('/friend-candidate')) { record.friend_contact = contact(); return clone(record) }
      if (url.endsWith('/friend-approve')) {
        record.friend_contact.status = 'succeeded'; record.friend_contact.can_approve = false; record.version++
        return clone(record)
      }
      if (url.endsWith('/friend-reject')) {
        record.friend_contact.status = 'succeeded'; record.friend_contact.can_reject = false
        record.friend_contact.result_code = 'native_friend_expired_rejection_acknowledged'
        record.state = 'cancelled'; record.can_cancel = false; record.version++
        record.blocked_reason = 'group_binding_friend_reapply_required'
        return clone(record)
      }
      if (url.endsWith('/reconcile')) return { id: 'operation', status: 'succeeded', version: 1 }
      if (url === '/group-binding/challenges') return { ...clone(record), private_challenge: 'PRIVATE_PROOF', group_challenge: 'GROUP_PROOF' }
      throw Error('Unexpected unit API: ' + url)
    }
    let ticketHandler = async () => 'ticket'
    class ClockDate extends Date { constructor(...args) { super(...(args.length ? args : [now])) } static now() { return now } }
    const module = { exports: {} }
    const imports = name => {
      if (name === 'vue') return vue
      if (name === './api') return { ApiError, api: async (url, method = 'GET', body, signal, workspace, key, ticket) => {
        (method === 'GET' ? reads : writes).push({ url, method, body, workspace, key, ticket })
        return apiHandler(url, method, body)
      } }
      if (name === './customer') return customerModule.exports
      if (name === './store') return { useSession: () => session }
      return {}
    }
    new Function('require', 'module', 'exports', 'Date', 'setTimeout', 'clearTimeout', 'document', 'window',
      transpile(compiled))(imports, module, module.exports, ClockDate,
      (fn, delay) => { const id = serial++; timers.set(id, { at: now + delay, fn }); return id },
      id => timers.delete(id), { addEventListener() {}, removeEventListener() {} },
      { addEventListener() {}, removeEventListener() {} })
    const component = module.exports.default, setup = component.setup
    component.setup = (props, ctx) => { state = setup(props, ctx); return state }
    component.render = () => null
    const renderer = vue.createRenderer({ createComment: () => ({}), insert() {}, remove() {},
      createElement: () => ({}), createText: () => ({}), setText() {}, setElementText() {},
      parentNode: () => null, nextSibling: () => null, patchProp() {} })
    const app = renderer.createApp(component, { platforms: ['wsl', 'lajiaoliao'], initialPlatform: platform,
      onClose: () => events.push('close') })
    app.mount({})
    state.webCaptcha.value = { open: context => { captcha.push(context); return ticketHandler(context) },
      cancel() { events.push('captcha-cancel') } }
    await vue.nextTick()
    state.applyProgress({ ...clone(record), private_challenge: 'PRIVATE_PROOF', group_challenge: 'GROUP_PROOF' })
    await vue.nextTick()
    const stop = () => { if (!destroyed) { app.unmount(); destroyed = true } }
    const advance = async ms => {
      now += ms
      for (let n = 0; n < 100; n++) {
        const next = [...timers].find(([, timer]) => timer.at <= now)
        if (!next) break
        timers.delete(next[0]); next[1].fn(); await vue.nextTick()
      }
      await vue.nextTick()
    }
    return { state, session, record, contact, reads, writes, captcha, events, stop, advance,
      api: fn => { apiHandler = fn }, ticket: fn => { ticketHandler = fn },
      prepare: async () => { state.friendNickname.value = '本人昵称'; await state.prepareFriendCandidate(); await vue.nextTick() },
      approve: async () => { state.friendApproveOpen.value = true; await state.approveFriend(); await vue.nextTick() },
      reject: async () => { await state.rejectExpiredFriend(); await vue.nextTick() } }
  }
  for (const platform of ['wsl', 'lajiaoliao']) {
    const h = await harness(platform)
    try {
      assert.equal(h.writes.length, 0); assert.equal(h.captcha.length, 0); verified()
      assert.equal(h.state.addAccount.value, 'public-robot')
      assert.equal(robotAddAccount({ robot: { native_id: 'PRIVATE', add_account: 'hidden', add_available: false } }), ''); verified()
      await h.prepare()
      assert.equal(h.writes.length, 1); assert.match(h.writes[0].url, /\/friend-candidate$/)
      assert.deepEqual(h.writes[0].body, { version: 1, nickname: '本人昵称' })
      assert.ok(h.writes[0].key); assert.equal(h.writes[0].workspace, platform)
      assert.equal(h.captcha.length, 0); assert.equal(h.state.friendVerified.value, false); verified()
      await h.state.approveFriend(); assert.equal(h.writes.length, 1); verified()
      const pending = deferred(); h.ticket(() => pending.promise)
      h.state.friendApproveOpen.value = true
      const approval = h.state.approveFriend()
      await h.state.approveFriend()
      assert.equal(h.writes.length, 1); assert.equal(h.captcha.length, 1)
      assert.deepEqual(h.captcha[0], { purpose: 'group_binding_friend', platform, target: h.record.id }); verified()
      pending.resolve('ticket'); await approval; await vue.nextTick()
      assert.equal(h.writes.length, 2)
      assert.deepEqual(h.writes[1].body, { version: 1, operation_id: 'operation' })
      assert.equal(h.writes[1].ticket, 'ticket'); assert.ok(h.writes[1].key)
      assert.equal(h.state.friendVerified.value, false); assert.equal(h.state.showPrivateProof.value, true)
      assert.equal(h.state.privateChallenge.value, 'PRIVATE_PROOF'); verified()
      await h.approve(); assert.equal(h.writes.length, 2); verified()
      await h.state.fetchProgress(h.record.id); assert.equal(h.writes.length, 2); verified()
      assert.doesNotMatch(JSON.stringify(h.session.drafts), /PRIVATE_PROOF|GROUP_PROOF|requested_at|PRIVATE_ROBOT/); verified()
    } finally { h.stop() }
    const expired = await harness(platform, undefined, 'reject_expired')
    try {
      expired.ticket(async () => null)
      await expired.prepare()
      assert.equal(expired.writes.length, 1); assert.equal(expired.captcha.length, 1)
      assert.deepEqual(expired.captcha[0], { purpose: 'group_binding_friend', platform, target: expired.record.id })
      assert.equal(expired.state.friendContact.value.status, 'prepared')
      assert.equal(expired.state.friendContact.value.action, 'reject_expired')
      assert.equal(expired.state.friendSubmitted.value, false)
      assert.equal(expired.state.canRejectExpiredFriend.value, true)
      assert.equal(expired.state.canApproveFriend.value, false)
      assert.equal(expired.state.showPrivateProof.value, false)
      assert.equal(expired.state.pendingOperation.value, false)
      assert.match(expired.state.expiredRejectionMessage.value, /尚未提交拒绝.*未同意/)
      assert.doesNotMatch(expired.state.expiredRejectionMessage.value, /结果待核对/); verified()
      await expired.state.approveFriend(); await expired.prepare()
      await expired.state.fetchProgress(expired.record.id); await expired.advance(1000)
      assert.equal(expired.writes.length, 1); assert.equal(expired.captcha.length, 1); verified()
      const pending = deferred(); expired.ticket(() => pending.promise)
      const rejection = expired.reject(); await expired.reject()
      assert.equal(expired.captcha.length, 2); assert.equal(expired.writes.length, 1); verified()
      pending.resolve('expired-ticket'); await rejection
      assert.equal(expired.writes.length, 2); assert.match(expired.writes[1].url, /\/friend-reject$/)
      assert.deepEqual(expired.writes[1].body, { version: 1, operation_id: 'operation' })
      assert.equal(expired.writes[1].ticket, 'expired-ticket'); assert.ok(expired.writes[1].key)
      assert.equal(expired.state.challenge.value.state, 'cancelled')
      assert.equal(expired.state.challenge.value.version, 2)
      assert.equal(expired.state.challenge.value.blocked_reason, 'group_binding_friend_reapply_required')
      assert.match(customerModule.exports.customerReason('group_binding_friend_reapply_required'), /重新开始核验.*有效时间/)
      assert.equal(expired.state.expiredRejectionConfirmed.value, true)
      assert.equal(expired.state.contactLabel('succeeded'), '拒绝已确认')
      assert.match(expired.state.expiredRejectionMessage.value, /重新开始核验.*有效时间内重新发送好友申请/)
      assert.equal(expired.state.friendVerified.value, false); assert.equal(expired.state.showPrivateProof.value, false)
      assert.equal(expired.state.privateChallenge.value, '')
      assert.equal(expired.state.friendContact.value.name, '本人昵称'); verified()
      await expired.reject(); await expired.approve(); await expired.state.fetchProgress(expired.record.id)
      expired.state.close()
      assert.equal(expired.writes.length, 2); assert.equal(expired.captcha.length, 2)
      assert.equal(expired.state.pendingOperation.value, false); verified()
    } finally { expired.stop() }
    const savedExpired = await harness(platform, undefined, 'reject_expired')
    savedExpired.ticket(async () => null); await savedExpired.prepare()
    const savedRecord = clone(savedExpired.record); savedExpired.stop()
    const restoredExpired = await harness(platform, savedExpired.session, 'reject_expired', savedRecord)
    try {
      await restoredExpired.state.fetchProgress(savedRecord.id); await restoredExpired.advance(1000)
      assert.equal(restoredExpired.writes.length, 0); assert.equal(restoredExpired.captcha.length, 0)
      assert.equal(restoredExpired.state.canRejectExpiredFriend.value, true)
      assert.equal(restoredExpired.state.friendContact.value.id, savedRecord.friend_contact.id)
      restoredExpired.state.friendPath.value = 'private'; await vue.nextTick()
      assert.equal(restoredExpired.state.expiredContactSelected.value, true)
      assert.equal(restoredExpired.state.showPrivateProof.value, false)
      assert.equal(restoredExpired.state.pendingOperation.value, false)
      assert.match(restoredExpired.state.expiredRejectionMessage.value, /尚未提交拒绝/)
      assert.equal(restoredExpired.writes.length, 0); assert.equal(restoredExpired.captcha.length, 0)
      assert.doesNotMatch(JSON.stringify(restoredExpired.session.drafts), /requested_at|reject_expired|PRIVATE_PROOF/)
      verified()
    } finally { restoredExpired.stop() }
    for (const result of ['failed', 'unknown', 'submitting', 'lost', 'lost_prepared', 'refused', 'missing_receipt']) {
      const h = await harness(platform, undefined, 'reject_expired')
      try {
        h.api(async (url, method) => {
          if (method === 'GET') return clone(h.record)
          if (url.endsWith('/friend-candidate')) { h.record.friend_contact = h.contact(); return clone(h.record) }
          assert.match(url, /\/friend-reject$/)
          h.record.friend_contact.status = result === 'lost_prepared' ? 'prepared' :
            result === 'lost' ? 'unknown' : result === 'refused' ? 'failed' :
            result === 'missing_receipt' ? 'succeeded' : result
          h.record.friend_contact.can_reject = result === 'lost_prepared'
          h.record.friend_contact.can_reconcile = ['unknown', 'submitting', 'lost', 'lost_prepared'].includes(result)
          if (h.record.friend_contact.can_reconcile) h.record.can_cancel = false
          h.record.friend_contact.result_code = ['failed', 'refused'].includes(result) ? 'native_refusal' : ''
          if (result === 'missing_receipt') { h.record.state = 'cancelled'; h.record.version++ }
          if (result === 'lost' || result === 'lost_prepared') throw Error('lost rejection response')
          if (result === 'refused') throw new ApiError('native_refusal')
          return clone(h.record)
        })
        await h.prepare()
        assert.equal(h.writes.length, 2); assert.equal(h.captcha.length, 1)
        assert.equal(h.state.friendSubmitted.value, true)
        assert.equal(h.state.canRejectExpiredFriend.value, false); assert.equal(h.state.canApproveFriend.value, false)
        assert.equal(h.state.expiredRejectionConfirmed.value, false); assert.equal(h.state.showPrivateProof.value, false)
        assert.notEqual(h.state.contactLabel(h.state.friendContact.value.status), '好友申请已接受')
        await h.state.fetchProgress(h.record.id); await h.reject(); await h.approve(); await h.prepare()
        assert.equal(h.writes.length, 2); assert.equal(h.captcha.length, 1); verified()
        if (['failed', 'refused'].includes(result)) {
          assert.match(h.state.expiredRejectionMessage.value, /未处理成功.*未同意.*不会自动重试/)
        } else if (result === 'missing_receipt') {
          assert.equal(h.state.pendingOperation.value, true)
          assert.match(h.state.expiredRejectionMessage.value, /结果待核对/)
          assert.equal(h.session.drafts[h.state.scope.value].id, h.record.id)
          h.state.saveProgressDraft()
          assert.equal(h.session.drafts[h.state.scope.value].friend_submitted, true)
          h.state.restart(); assert.equal(h.captcha.length, 1); assert.equal(h.writes.length, 2)
        } else {
          assert.equal(h.state.canReconcileContact.value, true)
          assert.match(h.state.expiredRejectionMessage.value, /结果待核对/)
          assert.doesNotMatch(h.state.expiredRejectionMessage.value, /尚未提交拒绝/)
          assert.equal(h.state.challenge.value.state, 'waiting')
          assert.equal(h.state.challenge.value.can_cancel, false)
          await h.state.mutate('cancel')
          assert.equal(h.writes.length, 2)
          h.api(async (url, method) => {
            if (method === 'GET') return clone(h.record)
            assert.match(url, /\/friend-attempts\/operation\/reconcile$/)
            h.record.friend_contact.status = 'succeeded'
            h.record.friend_contact.result_code = 'native_friend_expired_rejection_observed'
            h.record.friend_contact.can_reject = h.record.friend_contact.can_reconcile = false
            h.record.state = 'cancelled'; h.record.can_cancel = false; h.record.version++
            h.record.blocked_reason = 'group_binding_friend_reapply_required'
            return { id: 'operation', status: 'succeeded', result_code: h.record.friend_contact.result_code }
          })
          await h.state.reconcileFriend('contact')
          assert.equal(h.writes.length, 3); assert.equal(h.captcha.length, 1)
          assert.equal(h.state.challenge.value.state, 'cancelled')
          assert.equal(h.state.challenge.value.version, 2)
          assert.equal(h.state.expiredRejectionConfirmed.value, true)
          assert.equal(h.state.pendingOperation.value, false)
        }
        verified()
      } finally { h.stop() }
    }
    for (const phase of ['prepare', 'captcha', 'version', 'operation', 'action', 'name', 'time',
      'role', 'actor', 'workspace', 'closed', 'response']) {
      const h = await harness(platform, undefined, 'reject_expired')
      try {
        const pending = deferred()
        if (phase === 'prepare') h.api(() => pending.promise)
        else if (phase === 'response') {
          h.api(async url => {
            if (url.endsWith('/friend-candidate')) { h.record.friend_contact = h.contact(); return clone(h.record) }
            return pending.promise
          })
        } else h.ticket(() => pending.promise)
        h.state.friendNickname.value = '本人昵称'
        const action = h.state.prepareFriendCandidate()
        for (let n = 0; n < 4; n++) await vue.nextTick()
        const before = h.writes.length
        if (phase === 'version') h.state.challenge.value.version++
        else if (phase === 'operation') h.state.challenge.value.friend_contact.id = 'changed'
        else if (phase === 'action') h.state.challenge.value.friend_contact.action = 'approve'
        else if (phase === 'name') h.state.challenge.value.friend_contact.name = 'changed'
        else if (phase === 'time') h.state.challenge.value.friend_contact.requested_at = '2026-11-08T11:00:00Z'
        else if (phase === 'role') h.session.user.tenant_role = 'viewer'
        else if (phase === 'actor') h.session.user.id = 'other-actor'
        else if (phase === 'workspace') h.session.workspace = platform === 'wsl' ? 'lajiaoliao' : 'wsl'
        else if (phase === 'closed') h.state.close()
        else await h.advance(61000)
        if (phase === 'prepare') pending.resolve({ ...clone(h.record), friend_contact: h.contact() })
        else if (phase === 'response') {
          h.session.user.id = 'changed-after-submit'
          pending.resolve({ ...clone(h.record), state: 'cancelled', version: 2,
            friend_contact: { ...h.contact(), status: 'succeeded', result_code: 'native_friend_expired_rejection_acknowledged' } })
        } else pending.resolve('late-ticket')
        await action; await vue.nextTick()
        assert.equal(h.writes.length, before)
        assert.equal(h.state.expiredRejectionConfirmed.value, false)
        if (phase === 'prepare') assert.equal(h.captcha.length, 0)
        verified()
      } finally { h.stop() }
    }
    for (const [code, message] of [
      ['group_binding_friend_contact_not_found', /机器人本次返回的申请列表.*昵称完全一致.*核对申请发给了本次机器人.*确认已是好友后/],
      ['group_binding_friend_contact_processed', /同昵称申请记录已处理.*历史“已同意”不代表当前仍是好友.*重新发送申请/],
      ['group_binding_friend_contact_ambiguous', /多个.*不要猜选/],
      ['group_binding_friend_contact_outside_window', /待处理申请.*申请时间不在本次接入范围.*尚未同意.*刷新页面不会更新原申请时间/],
      ['group_binding_friend_contact_time_unavailable', /时间缺失.*无法安全选择/],
      ['group_binding_friend_contact_incomplete', /列表过多.*不会翻页或批量处理.*私聊核验.*超级管理员/]
    ]) {
      const h = await harness(platform)
      try {
        h.api(async () => { throw new ApiError(code) }); await h.prepare()
        assert.equal(h.state.friendContact.value, undefined); assert.equal(h.writes.length, 1)
        assert.equal(h.captcha.length, 0); assert.equal(h.state.uncertain.value, false)
        assert.match(h.state.error.value, message)
        assert.doesNotMatch(h.state.error.value, /native_onboarding_contact_|group_binding_friend_contact_/)
        h.state.friendPath.value = 'private'; await vue.nextTick()
        assert.equal(h.state.showPrivateProof.value, true); assert.equal(h.writes.length, 1); verified()
      } finally { h.stop() }
    }
    for (const invalid of ['nickname', 'time', 'version', 'identity', 'action']) {
      const h = await harness(platform)
      try {
        h.api(async () => {
          const value = h.contact()
          if (invalid === 'nickname') value.name = '另一个昵称'
          if (invalid === 'time') value.requested_at = ''
          if (invalid === 'version') value.version = 0
          if (invalid === 'identity') value.id = ''
          if (invalid === 'action') value.action = 'reject_all'
          return { ...clone(h.record), friend_contact: value }
        })
        await h.prepare()
        assert.equal(h.state.friendContact.value, undefined); assert.equal(h.state.canApproveFriend.value, false)
        assert.equal(h.captcha.length, 0); assert.equal(h.writes.length, 1)
        assert.equal(h.state.uncertain.value, true); verified()
      } finally { h.stop() }
    }
    const cancelledCaptcha = await harness(platform)
    try {
      await cancelledCaptcha.prepare()
      cancelledCaptcha.ticket(async () => null); await cancelledCaptcha.approve()
      assert.equal(cancelledCaptcha.writes.length, 1)
      assert.equal(cancelledCaptcha.state.friendSubmitted.value, false)
      assert.equal(cancelledCaptcha.state.canApproveFriend.value, true); verified()
      const before = cancelledCaptcha.events.length
      cancelledCaptcha.session.user = { ...cancelledCaptcha.session.user, username: 'same-actor-renamed' }
      cancelledCaptcha.session.permissions = [...cancelledCaptcha.session.permissions].reverse()
      await vue.nextTick()
      assert.equal(cancelledCaptcha.events.length, before); verified()
    } finally { cancelledCaptcha.stop() }
    for (const phase of ['prepare-success', 'prepare-error', 'confirm', 'captcha', 'captcha-version', 'captcha-operation', 'captcha-role']) {
      const h = await harness(platform)
      try {
        const pending = deferred()
        let action
        if (phase.startsWith('prepare')) {
          h.api(() => pending.promise); h.state.friendNickname.value = '本人昵称'; action = h.state.prepareFriendCandidate()
        } else {
          await h.prepare(); h.state.friendApproveOpen.value = true
          if (phase !== 'confirm') { h.ticket(() => pending.promise); action = h.state.approveFriend() }
        }
        const before = h.writes.length
        if (phase === 'captcha-version') h.state.challenge.value.version++
        else if (phase === 'captcha-operation') h.state.challenge.value.friend_contact.id = 'changed'
        else if (phase === 'captcha-role') h.session.user.tenant_role = 'viewer'
        else await h.advance(61000)
        if (phase === 'prepare-error') pending.resolve(Promise.reject(new ApiError('group_binding_friend_contact_not_found')))
        else if (phase.startsWith('prepare')) pending.resolve({ ...clone(h.record), friend_contact: h.contact() })
        else pending.resolve('late-ticket')
        await action; await vue.nextTick()
        assert.equal(h.writes.length, before)
        assert.equal(h.state.friendApproveOpen.value, false)
        if (!phase.startsWith('captcha-')) {
          assert.equal(h.state.expired.value, true); assert.equal(h.state.privateChallenge.value, '')
          assert.equal(h.state.groupChallenge.value, ''); assert.equal(h.state.error.value, '')
          assert.equal(h.state.friendContact.value?.status === 'succeeded', false)
        }
        verified()
      } finally { h.stop() }
    }
    const unknown = await harness(platform)
    try {
      await unknown.prepare()
      unknown.api(async () => { throw Error('lost response') }); await unknown.approve()
      assert.equal(unknown.state.friendSubmitted.value, true); assert.equal(unknown.state.canApproveFriend.value, false)
      unknown.record.friend_contact.can_reconcile = true
      unknown.api(async () => clone(unknown.record)); await unknown.state.fetchProgress(unknown.record.id)
      assert.equal(unknown.state.uncertain.value, false); assert.equal(unknown.state.canApproveFriend.value, false)
      assert.equal(unknown.state.canReconcileContact.value, true)
      await unknown.advance(61000); assert.equal(unknown.state.pendingOperation.value, true)
      unknown.api(async () => ({ id: 'operation', status: 'succeeded', version: 1 }))
      await unknown.state.reconcileFriend('contact')
      assert.equal(unknown.state.pendingOperation.value, false)
      assert.equal(unknown.state.friendVerified.value, false); assert.equal(unknown.state.canApproveFriend.value, false)
      assert.equal(unknown.writes.filter(w => w.url.endsWith('/friend-approve')).length, 1); verified()
    } finally { unknown.stop() }
    for (const change of ['actor', 'platform', 'closed']) {
      const h = await harness(platform)
      try {
        const pending = deferred(); h.api(() => pending.promise)
        h.state.friendNickname.value = '本人昵称'; const late = h.state.prepareFriendCandidate()
        if (change === 'actor') h.session.user.id = 'other'
        else if (change === 'platform') h.session.workspace = platform === 'wsl' ? 'lajiaoliao' : 'wsl'
        else h.state.close()
        pending.resolve({ ...clone(h.record), friend_contact: h.contact() }); await late; await vue.nextTick()
        assert.equal(h.state.friendContact.value, undefined); assert.equal(h.writes.length, 1)
        assert.equal(h.state.privateChallenge.value, ''); verified()
      } finally { h.stop() }
    }
    const historical = await harness(platform)
    try {
      historical.state.applyProgress({ ...clone(historical.record), friend_flow: 'outbound',
        friend_outbound: { id: 'old', status: 'prepared', can_send: true, target: { native_id: 'PRIVATE_TARGET' } } })
      historical.state.friendNickname.value = '本人昵称'; await historical.state.prepareFriendCandidate(); await historical.approve()
      assert.equal(historical.writes.length, 0); assert.equal(historical.captcha.length, 0)
      assert.equal(historical.state.showPrivateProof.value, true); verified()
      historical.state.friendSubmitted.value = true
      historical.state.challenge.value.friend_outbound.can_reconcile = true
      assert.equal(historical.state.pendingOperation.value, true)
      assert.equal(historical.state.canReconcileOutbound.value, true)
      historical.api(async () => ({ ...clone(historical.state.challenge.value),
        friend_outbound: { ...historical.state.challenge.value.friend_outbound, status: 'friends', can_reconcile: false } }))
      await historical.state.reconcileOutboundFriend()
      assert.equal(historical.state.pendingOperation.value, false)
      assert.equal(historical.writes.length, 1); assert.match(historical.writes[0].url, /\/outbound-attempts\/old\/reconcile$/)
      assert.equal(historical.captcha.length, 0); assert.equal(historical.state.friendVerified.value, false); verified()
    } finally { historical.stop() }
    const draft = await harness(platform)
    draft.state.friendNickname.value = '未提交昵称'; await vue.nextTick(); draft.stop()
    const reopened = await harness(platform, draft.session)
    try {
      assert.equal(reopened.state.friendNickname.value, '未提交昵称')
      assert.equal(reopened.writes.length, 0); verified()
    } finally { reopened.stop() }
    for (const state of ['bound', 'review']) {
      const h = await harness(platform)
      try {
        h.state.applyProgress({ ...clone(h.record), state }); await h.advance(61000)
        assert.equal(h.state.expired.value, false); assert.equal(h.state.terminal.value, true)
        h.state.close()
        if (state === 'bound') assert.equal(h.session.drafts[h.state.scope.value].id, undefined)
        verified()
      } finally { h.stop() }
    }
  }
  assert.throws(() => onboardingFriendContact({ id: 'bad', status: 'prepared', version: 1 }), /好友请求/)
  const legacyContact = { id: 'legacy', status: 'prepared', version: 1, name: '本人昵称',
    requested_at: '2026-11-08T12:00:00Z', can_approve: true }
  assert.equal(onboardingFriendContact(legacyContact).action, 'approve')
  assert.equal(onboardingFriendContact(legacyContact).can_reject, false)
  assert.equal(onboardingFriendContact({ ...legacyContact, action: 'approve', can_reject: true }).action, 'approve')
  for (const action of [null, '', 'reject', 'reject_all', 1])
    assert.throws(() => onboardingFriendContact({ ...legacyContact, action }), /好友请求/)
  verified()
  assert.equal(onboardingFriendContact(undefined), undefined); verified()
  assert.equal(/\/friend-(?:target|send)\b|group_binding_friend_send|can_send|可搜索账号/.test(source), false)
  const gate = fs.readFileSync(path.join(root, 'web/src/wsl/OnboardingFriendGate.vue'), 'utf8')
  assert.match(gate, /outbound\?: boolean/)
  assert.match(gate, /outbound: false/)
  assert.match(gate, /props\.outbound \? 'outbound-friend-gates' : 'friend-gates'/)
  assert.match(gate, /props\.outbound \? 'outbound' : 'incoming'/)
  assert.equal(/\/friend-(?:target|send)\b/.test(gate), false)
  assert.match(gate, /来向好友审批准入/); verified()
  console.log(JSON.stringify({ check: 'onboarding_incoming_ui_smoke.js --unit', passed: true,
    browser_launched: false, synthetic_only: true, platforms: ['wsl', 'lajiaoliao'], cases: assertions,
    vue_template_compiled: true }))
}
if (process.argv[1]?.endsWith('/onboarding_incoming_ui_smoke.js') && process.argv.includes('--unit'))
  unit().catch(error => { console.error(error); process.exitCode = 1 })
return smoke
})()
