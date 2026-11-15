// Synthetic browser regression, also evaluated with { fixtureOnly: true } by older tools.
// Run through run_multi_platform_ui_smoke.mjs; never use a live API or production bypass.
// prettier-ignore
async (page, options = {}) => {
  const base = new URL(page.url()).origin
  if (base !== 'http://127.0.0.1:5173') throw Error('Use the intercepted synthetic UI runner')
  const id = n => `ca640000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const assert = (value, message) => { if (!value) throw Error(message) }
  const same = (a, b) => JSON.stringify(a) === JSON.stringify(b)
  const fields = (body, expected) => same(Object.keys(body).sort(), [...expected].sort())
  const envelope = data => ({ code: 0, msg: '', data })
  const answer = { x: 148, y: 80 }, size = { width: 300, height: 220, tile_width: 44, tile_height: 44 }
  const dialog = () => page.getByRole('dialog', { name: '安全验证', exact: true })
  const slider = () => dialog().getByRole('slider', { name: '拼图位置', exact: true })

  // Small PNG encoder using Node builtins; fixture pixels and answers never enter product sources.
  const png = (width, height, tile = false) => {
    const crc = buffer => {
      let value = 0xffffffff
      for (const byte of buffer) {
        value ^= byte
        for (let bit = 0; bit < 8; bit++) value = (value >>> 1) ^ ((value & 1) ? 0xedb88320 : 0)
      }
      return (value ^ 0xffffffff) >>> 0
    }
    const chunk = (type, data) => {
      const tag = Buffer.from(type), length = Buffer.alloc(4), checksum = Buffer.alloc(4)
      length.writeUInt32BE(data.length)
      checksum.writeUInt32BE(crc(Buffer.concat([tag, data])))
      return Buffer.concat([length, tag, data, checksum])
    }
    const header = Buffer.alloc(13), pixels = Buffer.alloc((width * 4 + 1) * height)
    header.writeUInt32BE(width); header.writeUInt32BE(height, 4); header[8] = 8; header[9] = 6
    for (let y = 0; y < height; y++) for (let x = 0; x < width; x++) {
      const px = tile ? x + answer.x : x, py = tile ? y + answer.y : y
      const hole = !tile && px >= answer.x && px < answer.x + size.tile_width &&
        py >= answer.y && py < answer.y + size.tile_height
      const offset = y * (width * 4 + 1) + 1 + x * 4
      const color = hole ? [38, 45, 48] : (Math.floor(px / 20) + Math.floor(py / 20)) % 2
        ? [42, 162, 123] : [224, 196, 63]
      pixels.set([...color, 255], offset)
    }
    const bytes = Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]),
      chunk('IHDR', header), chunk('IDAT', process.getBuiltinModule('zlib').deflateSync(pixels)),
      chunk('IEND', Buffer.alloc(0))])
    return `data:image/png;base64,${bytes.toString('base64')}`
  }
  const background = png(size.width, size.height), tile = png(size.tile_width, size.tile_height, true)
  const challenges = new Map(), tickets = new Map(), requests = [], consumed = [], holds = new Set()
  let serial = 0, latest, nextTTL, nextHold
  const scopeOf = (path, body) => path.startsWith('/auth/captcha/')
    ? { purpose: 'login', username: body.username }
    : { purpose: body.purpose, platform: body.platform, ...(body.target ? { target: body.target } : {}) }
  const protectedScope = request => {
    const path = new URL(request.url()).pathname.replace('/api/v1', '')
    const platform = request.headers()['x-workspace']
    if (path === '/auth/login') return { purpose: 'login', username: request.postDataJSON()?.username }
    if (path === '/group-binding/challenges') return { purpose: 'group_binding_create', platform }
    const friend = /^\/group-binding\/challenges\/([^/]+)\/(?:friend-verify|friend-approve)$/.exec(path)
    if (friend) return { purpose: 'group_binding_friend', platform, target: friend[1] }
    if (path === '/claims') return { purpose: 'group_claim', platform }
    const demo = /^\/user-center\/groups\/([^/]+)\/demo-claim$/.exec(path)
    if (demo) return { purpose: 'operator_demo_claim', platform, target: demo[1] }
  }
  const holdNext = action => {
    assert(!nextHold && ['challenge', 'verify'].includes(action), 'Invalid or overlapping response hold')
    let started, release, done
    const hold = {
      action, started: new Promise(resolve => { started = resolve }),
      released: new Promise(resolve => { release = resolve }),
      done: new Promise(resolve => { done = resolve }),
      start: () => started(), release: () => release(), finish: () => done()
    }
    holds.add(hold); nextHold = hold
    return hold
  }
  const handle = async route => {
    const request = route.request(), url = new URL(request.url())
    const path = url.pathname.replace('/api/v1', '')
    if (!/^\/(?:auth\/captcha|human-verification)\/(?:challenge|verify)$/.test(path)) return false
    assert(url.origin === base && request.method() === 'POST', 'Captcha escaped its public/authenticated POST contract')
    assert(!url.search, 'Captcha scope appeared in the URL')
    const body = request.postDataJSON(), scope = scopeOf(path, body), action = path.split('/').at(-1)
    const publicLogin = scope.purpose === 'login'
    const scopeFields = publicLogin ? ['username'] : ['purpose', 'platform', ...(body.target ? ['target'] : [])]
    assert(publicLogin ? typeof body.username === 'string' && body.username.trim() :
      ['group_binding_create', 'group_binding_friend', 'group_claim', 'operator_demo_claim'].includes(body.purpose) &&
        ['wsl', 'lajiaoliao'].includes(body.platform), 'Captcha scope was incomplete')
    assert(request.headers()['x-workspace'] === (publicLogin ? 'wsl' : scope.platform), 'Captcha header crossed platforms')
    if (!publicLogin) {
      const targeted = ['group_binding_friend', 'operator_demo_claim'].includes(scope.purpose)
      assert(targeted ? /^[\da-f-]{36}$/.test(body.target || '') : !('target' in body), 'Captcha lost or invented a target')
    }
    requests.push({ path, action, scope, body })
    let response, status = 200
    if (action === 'challenge') {
      assert(fields(body, [...scopeFields, ...(body.previous_id ? ['previous_id'] : [])]), 'Unexpected challenge fields')
      if (body.previous_id) {
        const prior = challenges.get(body.previous_id)
        assert(prior && same(prior.scope, scope), 'Refresh crossed challenge scope')
        prior.invalid = true
      }
      const value = {
        challenge_id: id(++serial), image: { background, tile, ...size, initial_x: 128, initial_y: answer.y },
        expires_at: new Date(Date.now() + (nextTTL ?? 120000)).toISOString()
      }
      nextTTL = undefined
      latest = { ...value, scope, invalid: false }
      challenges.set(value.challenge_id, latest)
      response = envelope(value)
    } else {
      assert(fields(body, [...scopeFields, 'challenge_id', 'x', 'y']), 'Unexpected verify fields')
      const challenge = challenges.get(body.challenge_id)
      assert(challenge && same(challenge.scope, scope), 'Verification crossed challenge scope')
      assert(Number.isInteger(body.x) && Number.isInteger(body.y), 'Verification did not use image coordinates')
      const valid = !challenge.invalid && Date.parse(challenge.expires_at) > Date.now() &&
        Math.abs(body.x - answer.x) <= 1 && body.y === answer.y
      challenge.invalid = true
      if (!valid) { status = 422; response = { code: 1, msg: 'captcha_invalid' } }
      else {
        const ticket = `SYNTHETIC_${String(++serial).padStart(33, '0')}`
        const value = { ticket, expires_at: new Date(Date.now() + 60000).toISOString() }
        tickets.set(ticket, { ...value, scope, used: false })
        response = envelope(value)
      }
    }
    const hold = nextHold?.action === action ? nextHold : undefined
    if (hold) { nextHold = undefined; hold.start(); await hold.released }
    try { await route.fulfill({ status, json: response }) }
    catch (error) {
      if (!request.failure() && !page.isClosed()) throw error
    } finally {
      if (hold) { hold.finish(); holds.delete(hold) }
    }
    return true
  }
  const consume = request => {
    if (request.method() !== 'POST') return
    const expected = protectedScope(request)
    if (!expected) return
    const body = request.postDataJSON()
    const value = expected.purpose === 'login' ? body.captcha_ticket : request.headers()['x-human-verification']
    const ticket = tickets.get(value)
    assert(ticket && !ticket.used && Date.parse(ticket.expires_at) > Date.now() &&
      same(ticket.scope, expected), 'Protected submission omitted, reused or crossed its captcha ticket')
    if (expected.purpose !== 'login')
      assert(!('captcha_ticket' in body), 'Business ticket belongs in X-Human-Verification')
    ticket.used = true; consumed.push({ ...expected, ticket: value })
    return value
  }
  const ready = async () => {
    await dialog().getByRole('button', { name: '验证', exact: true }).and(page.locator(':enabled')).waitFor()
    assert(await dialog().getByRole('img', { name: '拼图背景', exact: true }).evaluate(image =>
      image.complete && image.naturalWidth === 300 && image.naturalHeight === 220), 'Synthetic background did not load')
    assert(await dialog().getByRole('img', { name: '待拼合的拼图', exact: true }).evaluate(image =>
      image.complete && image.naturalWidth === 44 && image.naturalHeight === 44), 'Synthetic tile did not load')
    // Element Plus animates overlay ancestors as well as the dialog itself.
    const modal = await dialog().elementHandle()
    try {
      await page.waitForFunction(element => {
        if (!element?.isConnected) return false
        for (let current = element; current; current = current.parentElement) {
          if ([...current.classList].some(name => /-enter-(?:from|active|to)$/.test(name)) ||
            current.getAnimations().some(animation =>
              animation.pending || !['finished', 'idle'].includes(animation.playState))) return false
        }
        return true
      }, modal, { polling: 'raf' })
    } finally {
      await modal.dispose()
    }
  }
  const position = async (method = 'keyboard', x = answer.x) => {
    await ready()
    const before = requests.filter(request => request.action === 'verify').length
    const control = slider(), current = Number(await control.getAttribute('aria-valuenow'))
    if (method === 'keyboard') {
      await control.focus()
      for (let step = 0; step < Math.abs(x - current); step++)
        await control.press(x > current ? 'ArrowRight' : 'ArrowLeft')
      assert(Number(await control.getAttribute('aria-valuenow')) === x, 'Keyboard did not move the slider')
      assert(requests.filter(request => request.action === 'verify').length === before, 'Keyboard adjustment verified before Enter')
    } else {
      const button = await control.boundingBox(), runway = await dialog().locator('.el-slider__runway').boundingBox()
      assert(button && runway, 'Slider geometry unavailable')
      const start = { x: button.x + button.width / 2, y: button.y + button.height / 2 }
      const end = { x: start.x + runway.width * (x - current) / (size.width - size.tile_width), y: start.y }
      if (method === 'mouse') {
        await page.mouse.move(start.x, start.y); await page.mouse.down()
        await page.mouse.move(end.x, end.y, { steps: 8 }); await page.mouse.up()
      } else if (method === 'touch') {
        const cdp = await page.context().newCDPSession(page)
        try {
          await cdp.send('Emulation.setTouchEmulationEnabled', { enabled: true })
          await cdp.send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: [{ ...start, id: 1 }] })
          await cdp.send('Input.dispatchTouchEvent', { type: 'touchMove', touchPoints: [{ ...end, id: 1 }] })
          await cdp.send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] })
        } finally {
          await cdp.send('Emulation.setTouchEmulationEnabled', { enabled: false })
          await cdp.detach()
        }
      } else throw Error('Unsupported synthetic slider interaction')
    }
  }
  const complete = async (method = 'keyboard') => {
    await position(method)
    if (method === 'keyboard') await slider().press('Enter')
    await dialog().waitFor({ state: 'hidden' })
  }
  const settled = () => page.evaluate(() => new Promise(resolve =>
    requestAnimationFrame(() => requestAnimationFrame(resolve))))
  const patchSession = patch => page.evaluate(patch => {
    const session = document.querySelector('#app')?.__vue_app__?.config.globalProperties.$pinia?._s.get('wsl-session')
    if (!session) throw Error('Synthetic session store was not mounted')
    for (const [key, value] of Object.entries(patch))
      session[key] = key === 'user' && value ? { ...session.user, ...value } : value
  }, patch)
  const refreshSession = async (automatic = false) => {
    const responses = ['/me', '/user-center', '/user-center/groups', '/user-center/usage'].map(path =>
      page.waitForResponse(response => new URL(response.url()).pathname === '/api/v1' + path &&
        response.request().method() === 'GET' && response.ok(), { timeout: automatic ? 20000 : 10000 }))
    if (!automatic)
      await page.getByRole('button', { name: '刷新', exact: true, includeHidden: true }).dispatchEvent('click')
    await Promise.all(responses)
    await page.getByRole('button', { name: '刷新用户中心', exact: true }).and(page.locator(':enabled')).waitFor()
    await settled()
  }
  const preserveOnRefresh = async (refresh = refreshSession) => {
    await position('keyboard', 139)
    const element = await dialog().elementHandle(), challengeID = latest.challenge_id,
      before = requests.length, value = await slider().getAttribute('aria-valuenow')
    try {
      await refresh()
      assert(await dialog().isVisible(), 'Same-identity /me refresh cancelled the open captcha')
      assert(await dialog().evaluate((current, previous) => current === previous, element),
        'Same-identity /me refresh remounted the captcha')
      assert(latest.challenge_id === challengeID && await slider().getAttribute('aria-valuenow') === value &&
        requests.length === before, 'Same-identity /me refresh reset the slider or issued captcha requests')
      assert(await dialog().getByRole('button', { name: '验证', exact: true }).isEnabled(),
        'Same-identity /me refresh invalidated the open captcha')
      await ready()
    } finally { await element.dispose() }
  }
  const fixture = { handle, consume, ready, position, complete, dialog, slider, requests, consumed,
    holdNext, settled, patchSession, refreshSession, preserveOnRefresh,
    expireNext: milliseconds => { nextTTL = milliseconds },
    latest: () => latest, dispose: () => { for (const hold of holds) hold.release() } }
  if (options.fixtureOnly) return fixture

  page.setDefaultTimeout(10000)
  const actor = id(900), tenant = id(901), platforms = ['wsl', 'lajiaoliao'], widths = [320, 390, 768, 1440]
  const user = { id: actor, tenant_id: tenant, tenant_role: 'owner', role: 'user', username: 'synthetic-captcha' }
  const permissions = ['overview:read', 'groups:read', 'claims:create', 'operations:read']
  const workspace = { id: tenant, name: '合成验证码客户', kind: 'tenant_business', owner_id: actor }
  const expires = new Date(Date.now() + 600000).toISOString()
  const plan = { id: id(902), name: 'PRO', active: true, platforms, modules: [],
    group_limit: 10, member_limit: 4, assistant_limit: 10, credit_limit: 1000000, expires_at: expires }
  const records = new Map(), writes = [], failures = [], screenshots = []
  let authenticated = false, failLogin = true, bindingSerial = 950
  const progress = (record, first = false) => {
    const { private_challenge, group_challenge, ...value } = record
    return { ...value, ...(first ? { private_challenge, group_challenge } : {}) }
  }
  const writeCount = path => writes.filter(write => write.path === path).length
  const verifyCount = () => requests.filter(request => request.action === 'verify').length
  const respond = async route => {
    if (await handle(route)) return
    const request = route.request(), url = new URL(request.url()), path = url.pathname.replace('/api/v1', '')
    const platform = request.headers()['x-workspace']
    let data
    if (request.method() === 'GET') {
      if (path === '/me') {
        if (!authenticated) return route.fulfill({ status: 401, json: { code: 1, msg: 'authentication_required' } })
        data = { user, permissions }
      } else if (path === '/workspaces') data = { items: [
        { id: 'wsl', name: '旺商聊机器人' }, { id: 'lajiaoliao', name: '辣椒聊机器人' }
      ] }
      else if (path === '/user-center') data = { workspace, effective_plan: plan, subscription: plan,
        entitlement: { mode: 'tenant_business', active: true, permissions }, permissions,
        quotas: { groups: 10, members: 4, assistant: 10, credits: 1000000 },
        billing_source: 'tenant_wallet', groups: [], claims: [], operations: [], personas: [] }
      else if (path === '/user-center/groups') data = { workspace, effective_plan: plan,
        items: [], total: 0, page: 1, page_size: 20 }
      else if (path === '/user-center/usage') data = { workspace, effective_plan: plan,
        call_count: 0, actual_micro: 0, held_micro: 0, billing_source: 'tenant_wallet' }
      else if (path === '/group-binding/context') data = { mode: 'tenant_business', workspace,
        default_target_tenant_id: tenant, can_onboard_for_customer: false, customers: [], next_offset: -1 }
      else if (/^\/group-binding\/challenges\/[^/]+$/.test(path)) {
        const record = records.get(path.split('/')[3])
        assert(record && record.platform === platform, 'Progress crossed a binding scope')
        data = progress(record)
      } else throw Error('Unmocked synthetic GET: ' + path)
    } else {
      assert(request.method() === 'POST', 'Unexpected synthetic mutation method')
      const body = request.postDataJSON(), ticket = consume(request), key = request.headers()['idempotency-key']
      writes.push({ path, platform, body, ticket, key })
      if (path === '/auth/login') {
        assert(fields(body, ['username', 'password', 'captcha_ticket']) &&
          body.password === 'synthetic-captcha-password', 'Login lost its exact ticket/body contract')
        if (failLogin) return route.fulfill({ status: 401, json: { code: 1, msg: 'invalid_credentials' } })
        authenticated = true; data = { user }
      } else if (path === '/group-binding/challenges') {
        assert(fields(body, ['platform']) && body.platform === platform && key, 'Binding creation lost scope/key')
        const record = { id: id(++bindingSerial), platform, friend_flow: 'incoming_contact', mode: 'tenant_business', workspace,
          customer: { tenant_id: tenant, name: workspace.name }, state: 'waiting', version: 1,
          expires_at: new Date(Date.now() + 600000).toISOString(),
          robot: { id: id(platform === 'wsl' ? 910 : 911), name: `${platform} 合成机器人`,
            native_id: `INTERNAL_ROBOT_${platform}`, add_account: `public-${platform}-captcha`, add_available: true },
          // Legacy response text is deliberately present only to prove that it stays hidden.
          friend_challenge: `群接入 ${platform === 'wsl' ? 'A'.repeat(20) : 'B'.repeat(20)}`,
          can_verify_friend: false,
          group_challenge: '群绑定 SYNTHETIC_CAPTCHA_PROOF', private_challenge: '群接入 SYNTHETIC_PRIVATE',
          identity_proven: false, can_confirm: false, can_cancel: true, can_accept_invitation: false }
        records.set(record.id, record); data = progress(record, true)
      } else if (/^\/group-binding\/challenges\/[^/]+\/friend-candidate$/.test(path)) {
        const record = records.get(path.split('/')[3])
        assert(record && record.platform === platform && record.friend_flow === 'incoming_contact' &&
          record.state === 'waiting' && !record.identity_proven && !record.friend_contact &&
          fields(body, ['version', 'nickname']) && body.version === record.version &&
          body.nickname === `${platform} 合成本人` && key && !ticket && !request.headers()['x-human-verification'],
          'Contact selection changed exact nickname/platform/version/key, supplied time/identity, or requested a captcha')
        record.friend_contact = { id: id(++bindingSerial), version: record.version, status: 'prepared', result_code: '',
          can_approve: true, can_reconcile: false, blocked_reason: '',
          name: body.nickname, requested_at: new Date(Date.now() - 1000).toISOString(),
          native_id: `INTERNAL_CONTACT_${platform}`, request_id: `INTERNAL_REQUEST_${platform}` }
        data = progress(record)
      } else if (/^\/group-binding\/challenges\/[^/]+\/friend-approve$/.test(path)) {
        const record = records.get(path.split('/')[3])
        assert(record && record.platform === platform && record.friend_flow === 'incoming_contact' &&
          record.state === 'waiting' && !record.identity_proven &&
          record.friend_contact?.status === 'prepared' && record.friend_contact.can_approve &&
          fields(body, ['version', 'operation_id']) && body.version === record.version &&
          body.operation_id === record.friend_contact.id && key && ticket,
          'Contact approval changed the selected operation/platform/version/key, omitted captcha, or replayed')
        record.friend_contact = { ...record.friend_contact, status: 'succeeded',
          result_code: 'native_friend_approval_acknowledged', can_approve: false, can_reconcile: false }
        data = progress(record)
      } else throw Error('Unexpected native, invitation or unrelated POST: ' + path)
    }
    await route.fulfill({ json: envelope(data) })
  }
  const handler = async route => {
    try { await respond(route) }
    catch (error) {
      failures.push(error.message)
      await route.fulfill({ status: 500, json: { code: 1, msg: 'synthetic_contract_failed' } })
    }
  }
  const guard = async route => {
    const url = new URL(route.request().url())
    if (url.origin !== base || url.pathname.startsWith('/api/')) {
      failures.push(`Unexpected network destination: ${url.origin}${url.pathname}`)
      return route.abort('blockedbyclient')
    }
    await route.fallback()
  }
  const fit = async label => {
    const { width, height } = page.viewportSize(), modal = await dialog().boundingBox()
    assert(modal && modal.x >= -1 && modal.x + modal.width <= width + 1 &&
      modal.y >= -1 && modal.y + modal.height <= height + 1, 'Captcha dialog overflow: ' + label)
    for (const name of ['刷新拼图', '取消', '验证']) {
      const bounds = await dialog().getByRole('button', { name, exact: true }).boundingBox()
      assert(bounds && bounds.x >= 0 && bounds.x + bounds.width <= width + 1 &&
        bounds.y >= 0 && bounds.y + bounds.height <= height + 1, 'Captcha action clipped: ' + name + '/' + label)
    }
    const image = await dialog().locator('.captcha-image').boundingBox()
    assert(image && Math.abs(image.width / image.height - 300 / 220) < 0.02, 'Puzzle aspect ratio changed')
    assert(await dialog().evaluate(element => element.scrollWidth <= element.clientWidth + 1) &&
      await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'Captcha horizontal overflow')
    const file = `output/playwright/web-captcha-${label}-${width}.png`
    await page.screenshot({ path: file, animations: 'disabled' }); screenshots.push(file)
  }
  const username = () => page.getByRole('textbox', { name: '用户名', exact: true })
  const login = () => page.getByRole('button', { name: '登录', exact: true })
  const cancelCaptcha = async () => {
    await dialog().getByRole('button', { name: '取消', exact: true }).click()
    await dialog().waitFor({ state: 'hidden' }); await settled()
  }
  const wizard = () => page.getByRole('dialog', { name: '接入我的群组', exact: true })
  const openWizard = async () => {
    await page.getByRole('button', { name: '接入群组', exact: true }).click()
    await wizard().getByRole('button', { name: '开始核验', exact: true }).and(page.locator(':enabled')).waitFor()
  }
  const start = () => wizard().getByRole('button', { name: '开始核验', exact: true }).click()
  const prepareFriend = async platform => {
    const record = [...records.values()].at(-1), before = requests.length
    const candidatePath = `/group-binding/challenges/${record.id}/friend-candidate`
    const approvalPath = `/group-binding/challenges/${record.id}/friend-approve`
    await wizard().getByRole('textbox', { name: '本人完整昵称', exact: true }).fill(`${platform} 合成本人`)
    await wizard().getByRole('button', { name: '核对近期好友请求', exact: true }).click()
    await wizard().getByText('已选中请求，尚未接受', { exact: true }).waitFor()
    await wizard().getByText(`${platform} 合成本人`, { exact: true }).waitFor()
    assert(writeCount(candidatePath) === 1 && !writeCount(approvalPath) && requests.length === before &&
      !record.identity_proven && !record.can_accept_invitation, 'Selection approved a contact, requested captcha or proved identity')
    assert(!await wizard().getByRole('button', { name: /^(复制好友核验内容|已发送，开始核验)$/ }).count() &&
      !await wizard().getByRole('button', { name: /查找我的账号|让机器人添加我|确认发送申请/ }).count() &&
      !await wizard().getByRole('textbox', { name: '本人可搜索账号', exact: true }).count() &&
      !await wizard().getByText(record.friend_challenge, { exact: true }).count(),
      'Retired outgoing controls or request-note identity proof remained visible')
    assert(!/INTERNAL_(?:ROBOT|CONTACT|REQUEST)_/.test(await wizard().innerText()),
      'Contact selection exposed native identities')
    return record.friend_contact.id
  }
  const friendConfirmation = () => page.getByRole('dialog', { name: '确认接受好友请求', exact: true })
  const openFriendConfirmation = () => wizard().getByRole('button', { name: '接受此好友请求', exact: true }).click()
  const approveFriend = async () => {
    await openFriendConfirmation()
    await friendConfirmation().getByRole('button', { name: '确认并安全验证', exact: true }).click()
  }
  const assertIdentityPending = async () => {
    assert(!await wizard().getByText('好友身份已核验，可邀请机器人入群', { exact: true }).count() &&
      !await wizard().getByRole('button', { name: /^(确认绑定|提交审核|核对群邀请)$/ }).count() &&
      ['添加与核对', '私聊身份核验'].includes(await wizard().locator('.onboarding-steps .is-current strong').innerText()),
      'Contact selection or approval became authenticated identity or group proof')
  }
  const navigate = async platform => {
    await page.evaluate(({ actor, platform }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`, 'user')
      sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
    }, { actor, platform })
    await page.goto(base + '/#groups'); await page.reload()
    await page.getByRole('button', { name: '刷新用户中心', exact: true }).waitFor()
  }
  const assertNoAutomaticActions = () => assert(writes.every(write =>
    write.path === '/auth/login' || write.path === '/group-binding/challenges' ||
    /^\/group-binding\/challenges\/[^/]+\/(?:friend-candidate|friend-approve)$/.test(write.path)), 'Automatic friend/invitation/native work occurred')
  await page.route('**/*', guard)
  await page.route('**/api/v1/**', handler)
  try {
    await page.evaluate(() => sessionStorage.clear())
    await page.goto(base); await page.reload()
    await page.getByRole('heading', { name: '登录工作空间', exact: true }).waitFor()
    assert(await page.title() === 'A哥后台 - 机器人', 'Login document brand changed')
    await page.getByRole('link', { name: 'A哥后台 - 机器人', exact: true }).waitFor()
    assert(!await page.locator('.login-shell footer,.login-footer').count(), 'Login footer was retained')
    assert(await login().isDisabled(), 'Login enabled without credentials')
    await username().fill('synthetic-captcha')
    await page.getByLabel('密码', { exact: true }).fill('synthetic-captcha-password')
    for (const width of widths) {
      await page.setViewportSize({ width, height: 820 })
      assert(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1), 'Login overflow: ' + width)
      const before = verifyCount()
      await login().click(); await ready(); await fit('login')
      assert(writeCount('/auth/login') === 0 && verifyCount() === before, 'Login posted without proof')
      await cancelCaptcha()
      assert(writeCount('/auth/login') === 0 && verifyCount() === before, 'Cancelling captcha posted login/verify')
      assert(await login().evaluate(element => element === document.activeElement), 'Captcha cancellation lost opener focus')
    }
    await login().click(); await ready()
    const failedID = latest.challenge_id, challengeCount = requests.filter(request => request.action === 'challenge').length
    await position('keyboard', answer.x + 16); await slider().press('Enter')
    await dialog().getByRole('alert').filter({ hasText: '安全验证已失效或未通过，请重新验证后手动提交' }).waitFor()
    assert(await dialog().getByRole('button', { name: '验证', exact: true }).isDisabled(), 'Failed captcha remained reusable')
    await page.waitForTimeout(120)
    assert(!writeCount('/auth/login') && requests.filter(request => request.action === 'challenge').length === challengeCount,
      'Failed verification retried challenge/login automatically')
    await dialog().getByRole('button', { name: '刷新拼图', exact: true }).click(); await ready()
    assert(latest.challenge_id !== failedID && requests.at(-1).body.previous_id === failedID, 'Refresh omitted previous_id')
    await complete()
    await page.locator('.login-form .el-alert').waitFor()
    await login().and(page.locator(':enabled')).waitFor()
    await page.waitForTimeout(120)
    assert(writeCount('/auth/login') === 1, 'Failed website login retried automatically')

    fixture.expireNext(900)
    await login().click(); await ready()
    await dialog().getByText('拼图已到期', { exact: true }).waitFor()
    assert(await dialog().getByRole('button', { name: '验证', exact: true }).isDisabled(), 'Expired challenge remained actionable')
    const expiredID = latest.challenge_id, beforeExpiry = verifyCount()
    await slider().press('Enter')
    assert(verifyCount() === beforeExpiry && writeCount('/auth/login') === 1, 'Expired challenge submitted')
    await dialog().getByRole('button', { name: '刷新拼图', exact: true }).click(); await ready()
    assert(requests.at(-1).body.previous_id === expiredID, 'Expiry refresh lost predecessor')
    await cancelCaptcha()

    for (const action of ['challenge', 'verify']) {
      const hold = holdNext(action), before = writeCount('/auth/login')
      await login().click()
      if (action === 'verify') { await position(); await slider().press('Enter') }
      await hold.started
      // A form-input event exercises invalidation even while the submit temporarily disables inputs.
      await username().evaluate((input, value) => {
        input.value = value; input.dispatchEvent(new Event('input', { bubbles: true }))
      }, `synthetic-renamed-${action}`)
      await dialog().waitFor({ state: 'hidden' })
      hold.release(); await hold.done; await settled()
      assert(writeCount('/auth/login') === before && !await dialog().isVisible(), 'Late response survived username cancellation')
    }
    failLogin = false
    await login().click(); await complete('mouse')
    await page.locator('.platform-shell').waitFor()
    assert(writeCount('/auth/login') === 2 && new Set(consumed.filter(value => value.purpose === 'login')
      .map(value => value.ticket)).size === 2, 'Successful login did not consume exactly one fresh ticket')

    for (const platform of platforms) {
      await navigate(platform); await openWizard()
      const beforeCreate = writeCount('/group-binding/challenges'), beforeVerify = verifyCount()
      await start(); await ready()
      assert(writeCount('/group-binding/challenges') === beforeCreate, 'Binding posted before captcha')
      await cancelCaptcha()
      assert(writeCount('/group-binding/challenges') === beforeCreate && verifyCount() === beforeVerify, 'Cancelled binding submitted')
      await start(); await ready()
      await preserveOnRefresh()
      permissions.reverse()
      await preserveOnRefresh()
      assert(writeCount('/group-binding/challenges') === beforeCreate, 'Session refresh submitted binding creation')
      for (const width of widths) { await page.setViewportSize({ width, height: 820 }); await settled(); await fit(platform) }
      const method = platform === 'wsl' ? 'mouse' : 'touch'
      await complete(method)
      await wizard().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
      await wizard().getByText(`public-${platform}-captcha`, { exact: true }).waitFor()
      await wizard().getByRole('button', { name: '复制机器人加好友账号', exact: true }).waitFor()
      const binding = [...records.values()].at(-1), friendPath = `/group-binding/challenges/${binding.id}/friend-approve`
      assert(writeCount('/group-binding/challenges') === beforeCreate + 1 &&
        consumed.at(-1).purpose === 'group_binding_create' && consumed.at(-1).platform === platform,
        'Binding did not consume its platform ticket')
      const createTicket = consumed.at(-1).ticket
      assert(!writeCount(friendPath) && !binding.friend_contact, 'Creating binding automatically selected or approved a contact')
      const beforePrivate = writes.length, beforePrivateCaptcha = requests.length
      await wizard().locator('.onboarding-friend-paths label').filter({ hasText: '已是好友，私聊核验' }).click()
      assert(await wizard().getByRole('radio', { name: '已是好友，私聊核验', exact: true }).isChecked(),
        'Clicking the visible private-verification label did not select its radio')
      await wizard().getByText(binding.private_challenge, { exact: true }).waitFor()
      await assertIdentityPending()
      assert(writes.length === beforePrivate && requests.length === beforePrivateCaptcha,
        'Already-friend private verification selected or approved a contact')
      await wizard().locator('.onboarding-friend-paths label').filter({ hasText: '我添加机器人' }).click()
      assert(await wizard().getByRole('radio', { name: '我添加机器人', exact: true }).isChecked(),
        'Clicking the visible incoming-contact label did not select its radio')
      const operation = await prepareFriend(platform)
      await assertIdentityPending()
      const beforeConfirmation = requests.length
      await openFriendConfirmation()
      await friendConfirmation().getByText(binding.friend_contact.name, { exact: true }).waitFor()
      assert(!writeCount(friendPath) && requests.length === beforeConfirmation,
        'Selecting a contact opened captcha or approved without confirmation')
      await friendConfirmation().getByRole('button', { name: '取消', exact: true }).click()
      await friendConfirmation().waitFor({ state: 'hidden' })
      assert(!writeCount(friendPath) && requests.length === beforeConfirmation, 'Cancelled confirmation approved a contact')
      await approveFriend(); await ready()
      assert(!writeCount(friendPath), 'Contact approved before captcha')
      await cancelCaptcha()
      assert(!writeCount(friendPath), 'Cancelled approval captcha submitted')

      await approveFriend(); await position('keyboard', answer.x + 16); await slider().press('Enter')
      await dialog().getByRole('alert').filter({ hasText: '安全验证已失效或未通过，请重新验证后手动提交' }).waitFor()
      const failedApprovalChallenges = requests.filter(request => request.action === 'challenge').length
      await settled()
      assert(!writeCount(friendPath) && !binding.identity_proven &&
        await dialog().getByRole('button', { name: '验证', exact: true }).isDisabled() &&
        requests.filter(request => request.action === 'challenge').length === failedApprovalChallenges,
        'Failed approval captcha approved, retried automatically, or established identity')
      await cancelCaptcha()

      fixture.expireNext(900)
      await approveFriend(); await ready()
      await dialog().getByText('拼图已到期', { exact: true }).waitFor()
      const beforeApprovalExpiry = verifyCount(), expiredApprovalID = latest.challenge_id
      await slider().press('Enter')
      assert(!writeCount(friendPath) && verifyCount() === beforeApprovalExpiry &&
        await dialog().getByRole('button', { name: '验证', exact: true }).isDisabled(),
        'Expired approval captcha submitted or reverified')
      await dialog().getByRole('button', { name: '刷新拼图', exact: true }).click(); await ready()
      assert(requests.at(-1).body.previous_id === expiredApprovalID &&
        latest.scope.purpose === 'group_binding_friend' && latest.scope.target === binding.id &&
        latest.scope.platform === platform, 'Approval refresh lost predecessor or challenge/platform scope')
      await preserveOnRefresh(); await complete()
      await wizard().getByText('好友申请已接受', { exact: true }).waitFor()
      await assertIdentityPending()
      await wizard().getByText(binding.private_challenge, { exact: true }).waitFor()
      assert(writeCount(friendPath) === 1 && consumed.at(-1).purpose === 'group_binding_friend' &&
        consumed.at(-1).platform === platform && consumed.at(-1).target === binding.id &&
        consumed.at(-1).ticket !== createTicket && binding.friend_contact.id === operation &&
        !binding.identity_proven && !binding.can_accept_invitation,
        'Contact approval lost its distinct target/platform ticket or became identity proof')
      const beforeProgress = writes.length, beforeProgressCaptcha = requests.length
      await wizard().getByRole('button', { name: '刷新进度', exact: true }).click()
      await wizard().getByRole('button', { name: '刷新进度', exact: true }).and(page.locator(':enabled')).waitFor()
      await settled(); await assertIdentityPending()
      assert(writes.length === beforeProgress && requests.length === beforeProgressCaptcha &&
        !binding.identity_proven && binding.friend_contact.id === operation,
        'Progress read retried friend work or established identity')
      // Only a separate authenticated private-message observation advances identity.
      Object.assign(binding, { identity_proven: true, can_accept_invitation: true, version: binding.version + 1 })
      await wizard().getByRole('button', { name: '刷新进度', exact: true }).click()
      await wizard().getByText('好友身份已核验，可邀请机器人入群', { exact: true }).waitFor()
      await wizard().getByLabel('目标群原生标识', { exact: true }).waitFor()
      assertNoAutomaticActions()
      await wizard().getByRole('button', { name: '关闭', exact: true }).click()
      await wizard().waitFor({ state: 'hidden' })
    }

    for (const stage of ['create', 'friend-approve']) for (const action of ['challenge', 'verify'])
      for (const boundary of ['close', 'platform']) {
      await page.setViewportSize({ width: 1440, height: 820 })
      await navigate('wsl'); await openWizard()
      if (stage === 'friend-approve') {
        await start(); await complete()
        await wizard().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
        await prepareFriend('wsl')
      }
      const before = writes.length, hold = holdNext(action)
      await (stage === 'create' ? start() : approveFriend())
      if (action === 'verify') { await position(); await slider().press('Enter') }
      await hold.started
      if (boundary === 'close') {
        // Exercise the real dialog close handler while its child request is pending.
        await wizard().locator('.el-dialog__headerbtn').dispatchEvent('click')
      } else {
        // A queued workspace click may arrive while a modal is pending.
        await page.getByRole('button', { name: '切换工作台', exact: true }).dispatchEvent('click')
        await page.getByRole('dialog', { name: '选择平台', exact: true })
          .getByRole('button', { name: '辣椒聊机器人', exact: true }).click()
      }
      await wizard().waitFor({ state: 'hidden' }); await dialog().waitFor({ state: 'hidden' })
      hold.release(); await hold.done; await settled()
      assert(writes.length === before, `Late ${stage} ${action} submitted after ${boundary}`)
      assert(!await dialog().isVisible(), 'Late verification reopened captcha')
      if (boundary === 'platform') assert(await page.evaluate(actor =>
        sessionStorage.getItem(`wsl:workspace:${actor}`), actor) === 'lajiaoliao', 'Platform isolation case did not switch')
    }
    const boundaries = [
      ['actor', { user: { id: id(980) } }],
      ['tenant', { user: { tenant_id: id(981) } }],
      ['role', { user: { role: 'superadmin' } }],
      ['tenant-role', { user: { tenant_role: 'viewer' } }],
      ['permissions', { permissions: [...permissions, 'claims:read'] }],
      ['console', { console: 'admin' }],
      ['platform', { workspace: 'lajiaoliao' }],
      ['account', { selectedAccount: id(982) }]
    ]
    for (const stage of ['create', 'friend-approve']) for (const [boundary, patch] of boundaries) {
      await page.setViewportSize({ width: 1440, height: 820 })
      await navigate('wsl'); await openWizard()
      if (stage === 'friend-approve') {
        await start(); await complete()
        await wizard().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
        await prepareFriend('wsl')
      }
      const before = writes.length, hold = holdNext('verify')
      await (stage === 'create' ? start() : approveFriend())
      await position(); await slider().press('Enter'); await hold.started
      await patchSession(patch)
      await dialog().waitFor({ state: 'hidden' })
      hold.release(); await hold.done; await settled()
      assert(writes.length === before && !await dialog().isVisible(), `Late ${stage} captcha submitted after ${boundary} changed`)
    }
    // The retired legacy claim has no entry button. Exercise its actual watcher and cancellation
    // with Vue reactivity rather than adding a product entry or exposing component internals.
    const source = process.getBuiltinModule('fs').readFileSync('web/src/wsl/UserCenter.vue', 'utf8')
    const watchStart = source.indexOf('  watch(', source.indexOf('  async function claim()'))
    const watcher = source.slice(watchStart, source.indexOf('\n  watch(', watchStart + 1))
    const scopeSource = source.match(/  const currentScope = \(\) =>\n    `[^\n]+`/)?.[0]
    const cancelSource = source.match(/  function cancelClaimVerification\(\) \{[\s\S]*?\n  \}/)?.[0]
    assert(watchStart >= 0 && watcher.includes('cancelClaimVerification') && scopeSource && cancelSource,
      'Legacy claim watcher source was not found')
    const require = process.getBuiltinModule('module').createRequire(process.cwd() + '/web/package.json')
    const { reactive, ref, watch, nextTick } = require('vue')
    const legacySession = reactive({ user: { ...user }, permissions: [...permissions],
      console: 'user', workspace: 'wsl', selectedAccount: '' })
    const legacy = { claimOpen: ref(true), claimPlatform: ref('wsl'), claimCode: ref('A'.repeat(43)),
      claimIdentity: ref('synthetic-owner'), claimSaving: ref(true),
      permissions: ref([...permissions]), subscription: ref({ active: true }) }
    let cancellations = 0
    const stop = process.getBuiltinModule('vm').runInNewContext(
      `let claimGeneration = 0;\n${scopeSource}\n${cancelSource}\n${watcher}`,
      { ...legacy, session: legacySession, watch, webCaptcha: ref({ cancel: () => cancellations++ }) })
    try {
      legacySession.user = { ...legacySession.user, username: 'renamed-same-owner' }
      legacySession.permissions = [...legacySession.permissions].reverse()
      legacy.permissions.value = [...legacy.permissions.value]
      legacy.subscription.value = { active: true }
      await nextTick()
      assert(!cancellations && legacy.claimSaving.value, 'Same-identity refresh cancelled legacy claim verification')
      for (const change of [
        () => { legacy.claimCode.value = 'B'.repeat(43) },
        () => { legacy.claimIdentity.value = 'another-synthetic-owner' },
        () => { legacy.claimPlatform.value = 'lajiaoliao' },
        () => { legacySession.user = { ...legacySession.user, id: id(983) } },
        () => { legacySession.user = { ...legacySession.user, tenant_id: id(984) } },
        () => { legacySession.user = { ...legacySession.user, role: 'superadmin' } },
        () => { legacySession.user = { ...legacySession.user, tenant_role: 'viewer' } },
        () => { legacySession.permissions = [...legacySession.permissions, 'claims:read'] },
        () => { legacySession.console = 'admin' },
        () => { legacySession.workspace = 'lajiaoliao' },
        () => { legacySession.selectedAccount = id(985) },
        () => { legacy.permissions.value = legacy.permissions.value.filter(value => value !== 'claims:create') },
        () => { legacy.subscription.value = { active: false } },
        () => { legacy.claimOpen.value = false }
      ]) {
        const before = cancellations
        legacy.claimSaving.value = true
        change(); await nextTick()
        assert(cancellations === before + 1 && !legacy.claimSaving.value,
          'Legacy claim edit, access or scope change did not cancel exactly once')
      }
    } finally { stop() }
    assertNoAutomaticActions()
    assert(await page.evaluate(() => !JSON.stringify({ ...sessionStorage, ...localStorage })
      .match(/SYNTHETIC_\d{33}|synthetic-captcha-password|data:image\/png;base64/)), 'Captcha secrets entered persistent drafts')
    assert(!failures.length, failures.join('; '))
    return { passed: true, synthetic_only: true, platforms, viewports: widths,
      login_brand: true, login_footer_removed: true, no_login_without_proof: true,
      cancelled_without_submission: true, failure_requires_manual_retry: true,
      single_use_tickets: true, image_coordinates: true, mouse: true, touch: true, keyboard: true,
      refresh_previous_id: true, expiry: true, username_late_isolation: true,
      onboarding_create_and_friend: true, incoming_candidate_before_approval: true, approval_not_identity_proof: true,
      no_approval_without_confirmation_and_captcha: true, approval_failure_and_expiry: true,
      existing_friend_private_without_contact_calls: true,
      ticket_header_scopes: true, wizard_late_isolation: true,
      same_identity_refresh_preserves_slider: true, equivalent_permissions_preserved: true,
      scope_cancellation: boundaries.map(([boundary]) => boundary),
      legacy_claim_same_identity_refresh: true, legacy_claim_scope_and_input_cancellation: true,
      no_automatic_friend_or_invitation: true, login_posts: writeCount('/auth/login'),
      captcha_posts: requests.length, protected_posts: consumed.length, native_calls: 0, model_calls: 0, screenshots }
  } finally {
    fixture.dispose()
    await page.unroute('**/api/v1/**', handler)
    await page.unroute('**/*', guard)
  }
}
