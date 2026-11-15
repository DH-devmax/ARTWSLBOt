// Synthetic component browser check; no app build, server, native account or model execution.
// WSL_UI_NODE_MODULES=/absolute/web/node_modules WSL_PLAYWRIGHT_MODULE=/absolute/playwright/index.js \
//   node tools/controlled_onboarding_ui_smoke.js --source
// Optional WSL_UI_SCREENSHOT_DIR saves synthetic 320/1440 evidence outside source baselines.
// prettier-ignore
(() => {
  const smoke = async (page, saveScreenshot) => {
    const check = (value, message) => { if (!value) throw Error(message) }
    const copy = value => JSON.parse(JSON.stringify(value))
    const id = n => `f9000000-0000-4000-8000-${String(n).padStart(12, '0')}`
    const widths = [320, 390, 768, 1440], platforms = ['wsl', 'lajiaoliao']
    const base = 'http://127.0.0.1:5173'
    let actor = id(1), serial = 100, latest, hold, release, now = Date.now(), contactAction = 'approve'
    const tenant = id(2), scopes = new Map(), challenges = new Map(), tickets = new Map(), captcha = new Map()
    const writes = [], reads = [], unexpected = [], screenshots = [], candidateCases = [], candidateLayoutFailures = []
    const accounts = platforms.flatMap((platform, index) => [
      { id: id(10 + index * 10), platform, purpose: 'management', version: 1,
        name: `${platform}机器人`, native_id: `robot-${platform}`, testing_enabled: false,
        connection_enabled: true, auth_state: 'authenticated' },
      { id: id(11 + index * 10), platform, purpose: index === 1 ? 'chat' : 'management', version: 1,
        name: `${platform}申请人`, native_id: `claimant-${platform}`, testing_enabled: false,
        connection_enabled: true, auth_state: 'authenticated' }
    ])
    const groups = platforms.map((platform, index) => ({
      id: id(30 + index), name: `${platform}隔离群`, native_id: `isolated-${platform}`, platform
    }))
    const candidateGroups = new Map(platforms.map(platform =>
      [platform, groups.filter(item => item.platform === platform)]))
    const robot = platform => accounts.find(item => item.platform === platform)
    const claimant = platform => accounts.find(item => item.platform === platform && item.id !== robot(platform).id)
    const identity = item => ({ id: item.id, name: item.name, native_id: item.native_id })
    const liveScope = (item, platform) => item && item.state === 'active' &&
      !item.blocked_reason && Date.parse(item.expires_at) > now &&
      item.actor_id === actor && item.tenant_id === tenant && item.platform === platform &&
      item.account_id === robot(platform).id && item.claimant_account_id === claimant(platform).id
    const projectScope = item => {
      const value = copy(item)
      if ('human_verification_required' in item)
        value.human_verification_required = !(item.state === 'active' && !item.blocked_reason &&
          Date.parse(item.expires_at) > now)
      return value
    }
    const scope = (platform, state = 'active', owner = actor,
      selectedGroup = groups.find(item => item.platform === platform)) => ({
      id: id(++serial), platform, actor_id: owner, tenant_id: tenant,
      account_id: robot(platform).id, claimant_account_id: claimant(platform).id,
      group_id: selectedGroup.id,
      robot: identity(robot(platform)), claimant: identity(claimant(platform)),
      group: identity(selectedGroup),
      state, blocked_reason: '', version: 1, human_verification_required: state !== 'active',
      expires_at: new Date(now + (state === 'expired' ? -1000 : 21600000)).toISOString(),
      native_attempts_used: 0, model_calls_used: 0, reserved_usdt: '0.000000'
    })
    const seed = platform => {
      const items = ['preparing', 'active', 'expired', 'closed', 'unknown'].map(state => scope(platform, state))
      items.find(item => item.state === 'unknown').expires_at = new Date(now - 1000).toISOString()
      items.push(scope(platform, 'active', id(9)), scope(platform === 'wsl' ? 'lajiaoliao' : 'wsl'))
      scopes.set(platform, items)
    }
    const fields = (value, names) => Object.keys(value).sort().join() === [...names].sort().join()
    const captchaCalls = () => writes.filter(item => item.path.startsWith('/human-verification/'))
    const friendMutations = () => writes.filter(item => /\/friend-(approve|reject)$/.test(item.path))
    const admitHuman = (request, platform, purpose, target, selected) => {
      const ticket = request.headers()['x-human-verification']
      if (liveScope(selected, platform) && projectScope(selected).human_verification_required === false) {
        check(ticket === undefined, 'Exempt controlled mutation supplied a human-verification ticket')
        return
      }
      const verified = tickets.get(ticket)
      check(verified?.purpose === purpose && verified.platform === platform && verified.target === target,
        'Non-exempt mutation bypassed or crossed its scoped captcha')
      tickets.delete(ticket)
    }
    const fixture = async route => {
      const request = route.request(), url = new URL(request.url())
      const path = url.pathname.replace('/api/v1', ''), platform = request.headers()['x-workspace'] || 'wsl'
      const method = request.method(), body = request.postDataJSON(), key = request.headers()['idempotency-key']
      const record = method === 'GET' ? reads : writes
      record.push({ path, method, platform, body, key,
        has_human_verification: request.headers()['x-human-verification'] !== undefined })
      let data
      try {
        if (hold?.path === path) {
          const pending = hold; hold = undefined; pending.started()
          await pending.promise
        }
        if (path === '/me') data = {
          user: { id: actor, username: 'synthetic', role: 'superadmin', tenant_id: tenant, tenant_role: 'owner' },
          permissions: ['claims:create', 'overview:read', 'groups:read', 'accounts:read', 'workspaces:maibot']
        }
        else if (path === '/accounts' && method === 'GET') data = copy(accounts.filter(item =>
          platform === 'maibot' ? item.purpose === 'chat' : item.purpose === 'management' && item.platform === platform))
        else if (/^\/accounts\/[^/]+\/receiver$/.test(path) && method === 'GET')
          data = { enabled: true, revision: 1, snapshot: { state: 'running' } }
        else if (path === '/test-tools/operations' && method === 'GET') data = []
        else if (path === '/group-binding/context' && method === 'GET')
          data = { mode: 'operator_demo', workspace: { id: tenant, name: '合成工作空间' },
            default_target_tenant_id: tenant, can_onboard_for_customer: true,
            customers: [{ tenant_id: id(3), name: '合成客户' }], next_offset: -1 }
        else if (path === '/test-tools/onboarding/scopes' && method === 'GET') {
          check(url.searchParams.get('platform') === platform, 'Scope list platform header/query differed')
          data = { items: (scopes.get(platform) || []).map(projectScope), candidates: {
            accounts: accounts.map(item => ({ ...identity(item), platform: item.platform })),
            groups: candidateGroups.get(platform).map(identity) } }
        } else if (path === '/test-tools/onboarding/scopes' && method === 'POST') {
          const selectedGroup = candidateGroups.get(platform).find(item => item.id === body.group_id)
          check(key && body.platform === platform && body.account_id === robot(platform).id &&
            robot(platform).testing_enabled && claimant(platform).testing_enabled &&
            body.claimant_account_id === claimant(platform).id &&
            body.account_id !== body.claimant_account_id &&
            selectedGroup &&
            body.minutes === 360 && Object.keys(body).length === 5,
          'Scope creation escaped candidates, switch, duration or idempotency fence')
          const created = scope(platform, 'active', actor, selectedGroup)
          scopes.get(platform).unshift(created)
          data = projectScope(created)
        } else if (/^\/test-tools\/onboarding\/scopes\/[^/]+(?:\/close)?$/.test(path)) {
          const item = scopes.get(platform).find(item => item.id === path.split('/')[4])
          check(item && item.actor_id === actor, 'Scope lookup escaped actor/platform')
          if (method === 'POST') {
            check(path.endsWith('/close') && Object.keys(body).join() === 'version' &&
              body.version === item.version && item.state === 'active', 'Close lost version fence')
            item.version++; item.state = 'closed'
          } else check(method === 'GET', 'Unexpected scope mutation')
          data = projectScope(item)
        } else if (path === '/human-verification/challenge' && method === 'POST') {
          check(body.platform === platform &&
            ['group_binding_create', 'group_binding_friend'].includes(body.purpose), 'Captcha lost its scope')
          const target = body.target && challenges.get(body.target)
          check(body.purpose === 'group_binding_create' ? !body.target : target?.platform === platform,
            'Friend captcha lost its challenge target')
          const selected = body.purpose === 'group_binding_friend' && target?.controlled_scope &&
            scopes.get(platform).find(item => item.id === target.controlled_scope.id)
          check(!selected || !liveScope(selected, platform) ||
            projectScope(selected).human_verification_required !== false,
            'Exempt controlled contact unexpectedly requested captcha')
          const challengeID = id(++serial)
          captcha.set(challengeID, copy(body))
          const png = 'data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j7X8AAAAASUVORK5CYII='
          data = { challenge_id: challengeID, image: { background: png, tile: png, width: 300, height: 220,
            tile_width: 40, tile_height: 40, initial_x: 0, initial_y: 20 },
          expires_at: new Date(now + 120000).toISOString() }
        } else if (path === '/human-verification/verify' && method === 'POST') {
          const original = captcha.get(body.challenge_id)
          check(original?.purpose === body.purpose && original.platform === body.platform &&
            original.target === body.target, 'Captcha verification escaped scope')
          const ticket = `synthetic-ticket-${++serial}`; tickets.set(ticket, original); captcha.delete(body.challenge_id)
          data = { ticket, expires_at: new Date(now + 60000).toISOString() }
        } else if (path === '/group-binding/challenges' && method === 'POST') {
          const selected = body.controlled_scope_id &&
            scopes.get(platform).find(item => item.id === body.controlled_scope_id)
          check(key && body.platform === platform && body.target_tenant_id === tenant &&
            fields(body, ['platform', 'target_tenant_id', ...(body.controlled_scope_id ? ['controlled_scope_id'] : [])]) &&
            (!body.controlled_scope_id || liveScope(selected, platform)),
          'Challenge crossed platform, actor or tenant')
          admitHuman(request, platform, 'group_binding_create', undefined, selected)
          latest = { id: id(++serial), platform, mode: 'operator_demo', friend_flow: 'incoming_contact',
            customer: { tenant_id: tenant, name: '合成工作空间' }, workspace: { id: tenant, name: '合成工作空间' },
            robot: { ...identity(robot(platform)), add_account: `public-${platform}`, add_available: true },
            state: 'waiting', version: 1, can_cancel: true, can_confirm: false, identity_proven: false,
            proof_kind: selected ? 'controlled_test' : 'authenticated_message',
            ...(selected ? { controlled_scope: projectScope(selected) } : {}),
            expires_at: new Date(now + (selected ? 600000 : 60000)).toISOString() }
          challenges.set(latest.id, latest)
          data = { ...copy(latest), private_challenge: 'SYNTHETIC_PRIVATE_PROOF', group_challenge: 'SYNTHETIC_GROUP_PROOF' }
        } else if (/^\/group-binding\/challenges\/[^/]+\/friend-(candidate|approve|reject)$/.test(path) &&
          method === 'POST') {
          const item = challenges.get(path.split('/')[3])
          const selected = item?.controlled_scope &&
            scopes.get(platform).find(value => value.id === item.controlled_scope.id)
          check(key && item?.platform === platform && item.state === 'waiting' &&
            Date.parse(item.expires_at) > now && body.version === item.version &&
            (!item.controlled_scope || liveScope(selected, platform) &&
              item.controlled_scope.account_id === selected.account_id &&
              item.controlled_scope.claimant_account_id === selected.claimant_account_id &&
              item.controlled_scope.group_id === selected.group_id),
          'Friend action crossed its live controlled scope or challenge version')
          if (path.endsWith('/friend-candidate')) {
            check(fields(body, ['version', 'nickname']) && body.nickname === claimant(platform).name &&
              !item.friend_contact && request.headers()['x-human-verification'] === undefined,
            'Friend preparation changed exact name, replayed or consumed a captcha ticket')
            item.friend_contact = { id: id(++serial), action: contactAction, status: 'prepared',
              version: item.version, name: body.nickname, result_code: '',
              requested_at: new Date(now - (contactAction === 'reject_expired' ? 660000 : 1000)).toISOString(),
              can_approve: contactAction === 'approve', can_reject: contactAction === 'reject_expired',
              can_reconcile: false, blocked_reason: '' }
          } else {
            const contact = item.friend_contact, reject = path.endsWith('/friend-reject')
            check(fields(body, ['version', 'operation_id']) && contact?.id === body.operation_id &&
              contact.version === item.version && contact.status === 'prepared' &&
              contact.action === (reject ? 'reject_expired' : 'approve') &&
              (reject ? contact.can_reject : contact.can_approve),
            'Friend mutation changed the sealed operation ID/action or replayed')
            admitHuman(request, platform, 'group_binding_friend', item.id, selected)
            contact.status = 'succeeded'; contact.can_approve = contact.can_reject = false
            contact.result_code = reject ? 'native_friend_expired_rejection_acknowledged' :
              'native_friend_approval_acknowledged'
            item.version++
            if (reject) {
              item.state = 'cancelled'; item.can_cancel = false
              item.blocked_reason = 'group_binding_friend_reapply_required'
            }
          }
          data = copy(item)
        } else if (/^\/group-binding\/challenges\/[^/]+\/friend-attempts\/[^/]+\/reconcile$/.test(path) &&
          method === 'POST') {
          const item = challenges.get(path.split('/')[3]), operation = path.split('/')[5]
          const selected = item?.controlled_scope &&
            scopes.get(platform).find(value => value.id === item.controlled_scope.id)
          const contact = item?.friend_contact
          check(item?.platform === platform && selected?.actor_id === actor &&
            selected.tenant_id === tenant && selected.platform === platform &&
            selected.account_id === item.controlled_scope.account_id &&
            selected.claimant_account_id === item.controlled_scope.claimant_account_id &&
            selected.group_id === item.controlled_scope.group_id &&
            selected.state === 'unknown' && contact?.id === operation &&
            contact.action === 'approve' && contact.status === 'unknown' && contact.can_reconcile === true &&
            fields(body, []) && key === undefined && request.headers()['x-human-verification'] === undefined,
          'Readback crossed the original unknown contact/scope or supplied mutation credentials')
          contact.status = 'succeeded'; contact.can_reconcile = false
          contact.result_code = 'native_friend_approval_acknowledged'
          item.controlled_scope = projectScope(selected)
          data = copy(item)
        } else if (/^\/group-binding\/challenges\/[^/]+$/.test(path) && method === 'GET') {
          const item = challenges.get(path.split('/')[3])
          check(item?.platform === platform, 'Progress read crossed platform')
          data = copy(item)
        } else {
          unexpected.push({ path, method })
          throw Error('Unmocked API: ' + method + ' ' + path)
        }
        await route.fulfill({ json: { code: 0, data } })
      } catch (error) {
        unexpected.push({ path, error: error.message })
        await route.fulfill({ status: 409, json: { code: 1, msg: error.message } })
      }
    }
    const modal = () => page.getByRole('dialog', { name: '接入群组', exact: true })
    const editor = () => page.getByRole('dialog', { name: '创建受控接入范围', exact: true })
    const confirmation = () => page.getByRole('dialog', { name: '确认受控接入范围', exact: true })
    const friendConfirmation = action => page.getByRole('dialog', {
      name: action === 'reject_expired' ? '确认拒绝过期请求' : '确认接受好友请求', exact: true
    })
    const captchaDialog = () => page.getByRole('dialog', { name: '安全验证', exact: true })
    const row = status => page.locator('.controlled-item').filter({ has: page.getByText(status, { exact: true }) }).first()
    const select = async (dialog, name, label) => {
      await dialog.getByRole('combobox', { name, exact: true })
        .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
      await page.getByRole('option', { name: label, exact: true }).click()
    }
    const capture = async (state, platform, width) => {
      const image = await page.screenshot({ animations: 'disabled' })
      check(image.length > 3000, 'Blank browser screenshot')
      const artifact = await saveScreenshot?.({ state, platform, width }, image)
      screenshots.push({ state, platform, width, bytes: image.length, ...(artifact ? { path: artifact } : {}) })
    }
    const fit = async (target, state, platform, sizes = widths) => {
      for (const width of sizes) {
        await page.setViewportSize({ width, height: 820 })
        await capture(state, platform, width)
        const bounds = await target.evaluate(container => {
          const element = container.matches('.el-dialog') ? container : container.querySelector('.el-dialog')
          if (!element) return { valid: false, missing_dialog: true }
          const rect = element.getBoundingClientRect(), footer = element.querySelector('.el-dialog__footer')
          const actions = footer ? footer.getBoundingClientRect() : rect
          const valid = rect.x >= -1 && rect.right <= innerWidth + 1 && rect.top >= -1 &&
            rect.bottom <= innerHeight + 1 && actions.bottom <= innerHeight + 1 &&
            element.scrollWidth <= element.clientWidth + 1 &&
            [...element.querySelectorAll('button')].every(button => button.scrollWidth <= button.clientWidth + 2)
          return { valid, rect: rect.toJSON(), actions: actions.toJSON(), scrollWidth: element.scrollWidth,
            clientWidth: element.clientWidth, buttons: [...element.querySelectorAll('button')].map(button => ({
              text: button.textContent, scrollWidth: button.scrollWidth, clientWidth: button.clientWidth })) }
        })
        check(bounds.valid, `${state}/${platform}/${width}: dialog or actions overflowed ${JSON.stringify(bounds)}`)
      }
    }
    const returnToEditor = async () => {
      await confirmation().getByRole('button', { name: '返回修改' }).click()
      // Keep Vue's transition deadline in step with the fixture's installed clock.
      await advance(350)
      await confirmation().waitFor({ state: 'hidden' })
      await editor().waitFor({ state: 'visible' })
    }
    const solve = async () => {
      await captchaDialog().getByRole('button', { name: '验证', exact: true }).and(page.locator(':enabled')).waitFor()
      await captchaDialog().getByRole('button', { name: '验证', exact: true }).click()
      await captchaDialog().waitFor({ state: 'hidden' })
    }
    const openWizard = async () => {
      await page.getByRole('button', { name: '打开接入向导', exact: true }).click()
      await modal().waitFor()
      await modal().getByRole('button', { name: '开始核验', exact: true }).and(page.locator(':enabled')).waitFor()
    }
    const closeWizard = async () => {
      await modal().getByRole('button', { name: '关闭', exact: true }).click()
      await modal().waitFor({ state: 'hidden' })
    }
    const resumeWizard = async () => {
      await page.getByRole('button', { name: '打开接入向导', exact: true }).click()
      await modal().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
      await page.waitForFunction(() => !document.querySelector('.onboarding-dialog .is-loading'))
    }
    const prepareContact = async (platform, action = 'approve') => {
      contactAction = action
      await modal().getByRole('textbox', { name: '本人完整昵称', exact: true }).fill(claimant(platform).name)
      await modal().getByRole('button', { name: '核对近期好友请求', exact: true }).click()
    }
    const holdNext = path => {
      let started, done
      const pending = { path, promise: new Promise(resolve => { done = resolve }),
        start: new Promise(resolve => { started = resolve }), started: () => started(), release: () => done() }
      hold = pending; release = pending.release
      return pending
    }
    const advance = async milliseconds => { now += milliseconds; await page.clock.fastForward(milliseconds) }
    page.setDefaultTimeout(10000)
    await page.clock.install({ time: new Date(now) })
    await page.route('**/api/v1/**', fixture)
    try {
      for (const platform of platforms) {
        seed(platform)
        await page.goto(base, { waitUntil: 'domcontentloaded', timeout: 20000 })
        await page.waitForFunction(() => Boolean(window.__controlledHarness))
        await page.evaluate(platform => window.__controlledHarness.platform(platform), platform)
        await row('有效').waitFor()
        check(await page.locator('.controlled-item').count() === 5, 'Scope list leaked another actor/platform')
        check(scopes.get(platform).every(item => projectScope(item).human_verification_required ===
          !(item.state === 'active' && !item.blocked_reason && Date.parse(item.expires_at) > now)),
        'Scope fixture did not project captcha exemption only while live and active')
        for (const state of ['准备中', '有效', '已过期', '已关闭', '结果待核对']) await row(state).waitFor()
        const expiredClose = row('已过期').getByRole('button', { name: '关闭接入范围' })
        check(!await row('准备中').getByRole('button', { name: '在用户中心接入' }).count() &&
          !await row('结果待核对').getByRole('button', { name: '关闭接入范围' }).count() &&
          !await row('结果待核对').getByRole('button', { name: '创建新接入范围' }).count() &&
          !await row('已关闭').getByRole('button', { name: '在用户中心接入' }).count() &&
          !await row('已过期').getByRole('button', { name: '在用户中心接入' }).count() &&
          (!await expiredClose.count() || await expiredClose.isDisabled()),
        'Inactive/unknown/expired scope exposed mutations')
        const before = writes.length
        await page.getByRole('button', { name: '创建接入范围', exact: true }).click()
        check(await editor().getByRole('spinbutton').inputValue() === '360' &&
          await editor().getByRole('spinbutton').getAttribute('aria-valuemax') === '360',
        'New scope did not default to the six-hour maximum')
        await select(editor(), '接入申请人账号', `${platform}申请人 · claimant-${platform}`)
        await select(editor(), '接入隔离群', `${platform}隔离群 · isolated-${platform}`)
        check(await editor().getByRole('button', { name: '核对接入范围' }).isDisabled(),
          'Testing-disabled robot could create a scope')
        await page.evaluate(() => window.__controlledHarness.testing(true))
        robot(platform).testing_enabled = true
        check(await editor().getByRole('button', { name: '核对接入范围' }).isDisabled(),
          'Testing-disabled claimant could create a scope')
        await page.evaluate(() => window.__controlledHarness.claimantTesting(true))
        claimant(platform).testing_enabled = true
        await editor().getByRole('button', { name: '取消', exact: true }).click()
        await page.getByRole('button', { name: '创建接入范围', exact: true }).click()
        await editor().getByRole('button', { name: '核对接入范围' }).and(page.locator(':enabled')).waitFor()
        await editor().getByRole('button', { name: '核对接入范围' }).click()
        await confirmation().getByText(`${platform}申请人 · claimant-${platform}`, { exact: true }).waitFor()
        check(writes.length === before, 'Viewing/editing/confirming created scope or native effects')
        await fit(confirmation(), 'scope-confirmation', platform)
        await returnToEditor()
        await editor().getByRole('button', { name: '核对接入范围' }).click()
        await confirmation().getByRole('button', { name: '确认创建范围' }).click()
        await editor().waitFor({ state: 'hidden' })
        check(writes.length === before + 1 && writes.at(-1).path === '/test-tools/onboarding/scopes',
          'Scope creation performed extra actions')
        const created = scopes.get(platform)[0]
        const statusBefore = writes.length
        created.state = 'unknown'
        created.blocked_reason = 'group_binding_operation_unresolved'
        created.native_attempts_used = 2; created.model_calls_used = 1; created.reserved_usdt = '0.123456'
        await row('有效').getByRole('button', { name: '查询此范围' }).click()
        await row('结果待核对').getByText('0.123456 USDT', { exact: true }).waitFor()
        check(!await row('结果待核对').getByRole('button', { name: '在用户中心接入' }).count() &&
          !await row('结果待核对').getByRole('button', { name: '关闭接入范围' }).count(),
          'Unknown result still admitted a mutation')
        created.state = 'active'; created.blocked_reason = ''
        await row('结果待核对').getByRole('button', { name: '查询此范围' }).click()
        await row('有效').getByText('0.123456 USDT', { exact: true }).waitFor()
        check(writes.length === statusBefore && reads.at(-1).path.endsWith('/' + created.id),
          'Single-scope status read triggered a native call or lost its identity')
        await row('有效').getByRole('button', { name: '在用户中心接入' }).click()
        await openWizard()
        const notice = modal().locator('.onboarding-controlled-notice')
        await notice.getByText(`${platform}申请人 · claimant-${platform}`, { exact: true }).waitFor()
        check(await modal().getByRole('radio', { name: '代客户接入', exact: true }).isDisabled(),
          'Controlled scope allowed customer targeting')
        check(!/请使用未接入号池/.test(await modal().innerText()), 'Controlled scope retained ordinary-only warning')
        await fit(modal(), 'scope-selected', platform)
        const beforeChallenge = writes.filter(item => item.path === '/group-binding/challenges').length
        const controlledCaptchaBefore = captchaCalls().length
        await modal().getByRole('button', { name: '开始核验' }).click()
        await modal().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
        check(writes.filter(item => item.path === '/group-binding/challenges').length === beforeChallenge + 1 &&
          writes.findLast(item => item.path === '/group-binding/challenges').body.controlled_scope_id === created.id &&
          !writes.findLast(item => item.path === '/group-binding/challenges').has_human_verification &&
          captchaCalls().length === controlledCaptchaBefore && !await captchaDialog().isVisible(),
          'Live controlled creation lost its scope, requested captcha or supplied a ticket')
        check(!('restore_group_id' in writes.findLast(item => item.path === '/group-binding/challenges').body) &&
          !('binding_version' in writes.findLast(item => item.path === '/group-binding/challenges').body),
          'Controlled creation required a restored group binding')

        const beforePrepare = writes.length, beforeApprove = friendMutations().length
        await prepareContact(platform)
        await modal().getByText('已选中请求，尚未接受', { exact: true }).waitFor()
        const approvalOperation = latest.friend_contact.id, approvalVersion = latest.version
        check(writes.length === beforePrepare + 1 && writes.at(-1).path.endsWith('/friend-candidate') &&
          friendMutations().length === beforeApprove && captchaCalls().length === controlledCaptchaBefore &&
          !await friendConfirmation('approve').isVisible() && !await captchaDialog().isVisible(),
        'Controlled preparation submitted approval, opened captcha or skipped manual selection')
        await modal().getByRole('button', { name: '接受此好友请求', exact: true }).click()
        await friendConfirmation('approve').getByRole('button', { name: '确认接受', exact: true }).waitFor()
        check(writes.length === beforePrepare + 1 && captchaCalls().length === controlledCaptchaBefore,
          'Opening controlled approval confirmation submitted the selected request')
        await fit(friendConfirmation('approve'), 'controlled-friend-approve-confirmation', platform)
        await friendConfirmation('approve').getByRole('button', { name: '取消', exact: true }).click()
        await friendConfirmation('approve').waitFor({ state: 'hidden' })
        check(friendMutations().length === beforeApprove && latest.friend_contact.status === 'prepared',
          'Cancelling controlled approval confirmation mutated the prepared request')
        await modal().getByRole('button', { name: '接受此好友请求', exact: true }).click()
        await friendConfirmation('approve').getByRole('button', { name: '确认接受', exact: true })
          .evaluate(button => { button.click(); button.click() })
        await modal().getByText('好友申请已接受', { exact: true }).waitFor()
        check(friendMutations().length === beforeApprove + 1 &&
          friendMutations().at(-1).path.endsWith('/friend-approve') &&
          fields(friendMutations().at(-1).body, ['version', 'operation_id']) &&
          friendMutations().at(-1).body.operation_id === approvalOperation &&
          friendMutations().at(-1).body.version === approvalVersion &&
          !friendMutations().at(-1).has_human_verification &&
          latest.friend_contact.id === approvalOperation && !latest.identity_proven &&
          captchaCalls().length === controlledCaptchaBefore && !await captchaDialog().isVisible(),
        'Controlled approval replayed, changed its sealed operation, requested captcha or became identity proof')

        // Project an unresolved submission through GET; readback must not reactivate its scope.
        const readbackBefore = writes.length, readbackVersion = latest.version,
          readbackChallenge = latest.id,
          readbackPath = `/group-binding/challenges/${latest.id}/friend-attempts/${approvalOperation}/reconcile`,
          readbackUsage = [created.native_attempts_used, created.model_calls_used, created.reserved_usdt]
        created.state = 'unknown'; created.blocked_reason = 'group_binding_operation_unresolved'; created.version++
        latest.controlled_scope = projectScope(created)
        Object.assign(latest.friend_contact, { status: 'unknown', result_code: '',
          can_approve: false, can_reject: false, can_reconcile: true })
        await modal().getByRole('button', { name: '刷新进度', exact: true }).click()
        const readbackButton = modal().getByRole('button', { name: '核对原好友审批', exact: true })
        await readbackButton.and(page.locator(':enabled')).waitFor()
        check(writes.length === readbackBefore &&
          reads.at(-1).path === `/group-binding/challenges/${readbackChallenge}` &&
          latest.friend_contact.id === approvalOperation && latest.controlled_scope.state === 'unknown' &&
          latest.controlled_scope.human_verification_required === true &&
          !await captchaDialog().isVisible(),
        'Unknown progress GET submitted an action or lost the original operation/readback')
        for (const name of ['核对近期好友请求', '接受此好友请求', '处理过期申请', '取消本次接入',
          '重新开始核验', '确认绑定'])
          check(!await modal().getByRole('button', { name, exact: true }).and(page.locator(':enabled')).count(),
            'Unknown controlled scope exposed mutation: ' + name)
        await fit(modal(), 'controlled-unknown-contact-readback', platform)
        await readbackButton.click()
        await modal().getByText('好友申请已接受', { exact: true }).waitFor()
        await readbackButton.waitFor({ state: 'hidden' })
        check(writes.length === readbackBefore + 1 && writes.at(-1).path === readbackPath &&
          fields(writes.at(-1).body, []) && writes.at(-1).key === undefined &&
          !writes.at(-1).has_human_verification &&
          friendMutations().length === beforeApprove + 1 && captchaCalls().length === controlledCaptchaBefore &&
          latest.id === readbackChallenge && latest.version === readbackVersion &&
          latest.friend_contact.id === approvalOperation && latest.friend_contact.status === 'succeeded' &&
          latest.controlled_scope.state === 'unknown' && created.state === 'unknown' &&
          JSON.stringify(readbackUsage) === JSON.stringify([
            created.native_attempts_used, created.model_calls_used, created.reserved_usdt]) &&
          !await captchaDialog().isVisible(),
        'Unknown contact readback replayed native approval/captcha, changed its operation or reactivated the scope')
        await modal().getByRole('button', { name: '刷新进度', exact: true }).click()
        await page.waitForFunction(() => !document.querySelector('.onboarding-dialog .is-loading'))
        check(writes.length === readbackBefore + 1 &&
          reads.at(-1).path === `/group-binding/challenges/${readbackChallenge}` &&
          latest.controlled_scope.state === 'unknown' && created.state === 'unknown' &&
          !await modal().getByRole('button', { name: '取消本次接入', exact: true }).count() &&
          captchaCalls().length === controlledCaptchaBefore,
        'Fresh GET after readback mutated or silently cleared the unknown scope')

        // Start the remaining independent active-scope scenarios with an explicit fixture transition.
        created.state = 'active'; created.blocked_reason = ''; created.version++
        latest.controlled_scope = projectScope(created)
        latest.identity_proven = true; latest.can_accept_invitation = true; latest.version++
        const progressBefore = writes.length
        await modal().getByRole('button', { name: '刷新进度' }).click()
        await page.waitForFunction(() => !document.querySelector('.onboarding-dialog .is-loading'))
        check(writes.length === progressBefore, 'Progress read triggered a native mutation')
        const nativeGroup = modal().locator('.el-form-item').filter({ hasText: '目标群原生标识' }).locator('input')
        await nativeGroup.waitFor()
        check(await nativeGroup.inputValue() === created.group.native_id && await nativeGroup.isDisabled(),
          'Controlled progress did not populate and lock its native group without initialGroup')
        await fit(modal(), 'controlled-progress', platform)
        await advance(600001)
        await modal().locator('.onboarding-expired').waitFor()
        check(!await modal().locator('.onboarding-controlled-expired').count(),
          'Ten-minute challenge expiry incorrectly expired six-hour scope')
        const expiredChallenge = latest.id
        await modal().getByRole('button', { name: '重新开始核验', exact: true }).click()
        await modal().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
        check(latest.id !== expiredChallenge && latest.controlled_scope.id === created.id &&
          !writes.findLast(item => item.path === '/group-binding/challenges').has_human_verification &&
          captchaCalls().length === controlledCaptchaBefore && !await captchaDialog().isVisible(),
        'Controlled restart changed its still-active scope or requested captcha/ticket')

        const beforeRejectPrepare = writes.length, beforeReject = friendMutations().length
        await prepareContact(platform, 'reject_expired')
        await friendConfirmation('reject_expired').getByRole('button', { name: '确认拒绝', exact: true }).waitFor()
        const rejectionOperation = latest.friend_contact.id, rejectionVersion = latest.version,
          rejectionChallenge = latest.id
        check(writes.length === beforeRejectPrepare + 1 && writes.at(-1).path.endsWith('/friend-candidate') &&
          friendMutations().length === beforeReject && captchaCalls().length === controlledCaptchaBefore &&
          latest.friend_contact.status === 'prepared' && !await captchaDialog().isVisible(),
        'Controlled expired selection bypassed second confirmation or opened captcha')
        await fit(friendConfirmation('reject_expired'), 'controlled-friend-reject-confirmation', platform)
        await friendConfirmation('reject_expired').getByRole('button', { name: '取消', exact: true }).click()
        await friendConfirmation('reject_expired').waitFor({ state: 'hidden' })
        await modal().getByText('已选中过期申请，尚未拒绝', { exact: true }).waitFor()
        check(!(await modal().innerText()).includes('过期申请拒绝结果待核对') &&
          !await modal().getByRole('button', { name: '核对原好友审批', exact: true }).count(),
          'An unsubmitted expired plan was presented as an unknown native result')
        await modal().locator('.onboarding-friend-paths').getByText('已是好友，私聊核验', { exact: true }).click()
        await modal().locator('.onboarding-private-blocked[role="alert"]').waitFor()
        await modal().getByText('已选中过期申请，尚未提交拒绝，也未同意此申请。', { exact: true }).waitFor()
        check(!await modal().getByRole('heading', { name: '私聊核验本人身份', exact: true }).count() &&
          !await modal().getByText('SYNTHETIC_PRIVATE_PROOF', { exact: true }).count(),
        'A prepared expired plan exposed a private-proof path rejected by the server')
        await fit(modal(), 'controlled-expired-private-blocked', platform)
        await modal().getByRole('button', { name: '刷新进度', exact: true }).click()
        await page.waitForFunction(() => !document.querySelector('.onboarding-dialog .is-loading'))
        await closeWizard(); await resumeWizard()
        check(writes.length === beforeRejectPrepare + 1 && friendMutations().length === beforeReject &&
          captchaCalls().length === controlledCaptchaBefore &&
          !await friendConfirmation('reject_expired').isVisible() && !await captchaDialog().isVisible() &&
          latest.friend_contact.id === rejectionOperation,
        'GET or draft restoration submitted rejection, opened confirmation/captcha or changed the prepared plan')
        await modal().locator('.onboarding-private-blocked[role="alert"]').waitFor()
        await modal().locator('.onboarding-friend-paths').getByText('我添加机器人', { exact: true }).click()
        await modal().getByRole('button', { name: '处理过期申请', exact: true }).click()
        await friendConfirmation('reject_expired').getByRole('button', { name: '确认拒绝', exact: true }).waitFor()
        check(friendMutations().length === beforeReject, 'Resuming expired preparation submitted before confirmation')
        await friendConfirmation('reject_expired').getByRole('button', { name: '确认拒绝', exact: true })
          .evaluate(button => { button.click(); button.click() })
        await modal().getByText('拒绝已确认', { exact: true }).waitFor()
        check(friendMutations().length === beforeReject + 1 &&
          friendMutations().at(-1).path.endsWith('/friend-reject') &&
          fields(friendMutations().at(-1).body, ['version', 'operation_id']) &&
          friendMutations().at(-1).body.operation_id === rejectionOperation &&
          friendMutations().at(-1).body.version === rejectionVersion &&
          !friendMutations().at(-1).has_human_verification &&
          latest.friend_contact.id === rejectionOperation && latest.state === 'cancelled' &&
          latest.version === rejectionVersion + 1 && latest.friend_contact.can_reject === false &&
          !await modal().getByRole('button', { name: '重新开始核验', exact: true }).isDisabled() &&
          captchaCalls().length === controlledCaptchaBefore && !await captchaDialog().isVisible(),
        'Controlled rejection changed operation/version, replayed, needed captcha or blocked restart')
        await fit(modal(), 'controlled-friend-rejected', platform)
        const rejectedCreateKey = writes.findLast(item => item.path === '/group-binding/challenges').key
        await modal().getByRole('button', { name: '重新开始核验', exact: true }).click()
        await modal().getByRole('textbox', { name: '本人完整昵称', exact: true }).waitFor()
        check(latest.id !== rejectionChallenge && latest.controlled_scope.id === created.id &&
          writes.findLast(item => item.path === '/group-binding/challenges').key !== rejectedCreateKey &&
          !writes.findLast(item => item.path === '/group-binding/challenges').has_human_verification &&
          captchaCalls().length === controlledCaptchaBefore && !await captchaDialog().isVisible(),
        'Restart after controlled rejection reused a challenge/key or requested captcha')
        const beforeExpiry = writes.length
        const scopeExpiryCaptcha = captchaCalls().length
        await advance(5 * 60 * 60 * 1000)
        check(!await modal().locator('.onboarding-controlled-expired').count(),
          'Six-hour scope expired early after the old thirty-minute boundary')
        await advance(Date.parse(created.expires_at) - now + 1)
        await modal().locator('.onboarding-controlled-expired.el-alert--error[role="alert"]').waitFor()
        check(await modal().locator('.onboarding-controlled-expired .el-alert__icon').count() &&
          !await modal().getByRole('button', { name: '重新开始核验' }).count() &&
          !await modal().getByRole('button', { name: '确认绑定' }).count() &&
          !await modal().getByRole('button', { name: '核对近期好友请求' }).count(),
        'Scope expiry did not immediately block challenge mutations')
        check(writes.length === beforeExpiry && captchaCalls().length === scopeExpiryCaptcha,
          'Scope timer submitted a mutation or opened captcha')
        await fit(modal(), 'scope-expired', platform)
        await modal().getByRole('button', { name: '创建新接入范围', exact: true }).click()
        await page.locator('.controlled-onboarding').waitFor()
        const expiredID = created.id, renewalBefore = writes.length
        await row('已过期').getByRole('button', { name: '创建新接入范围', exact: true }).click()
        await editor().getByRole('button', { name: '核对接入范围' }).click()
        await confirmation().getByRole('button', { name: '确认创建范围' }).click()
        await editor().waitFor({ state: 'hidden' })
        check(scopes.get(platform)[0].id !== expiredID &&
          writes.length === renewalBefore + 1 && writes.at(-1).path === '/test-tools/onboarding/scopes',
        'Renewal revived an old scope')
        await row('有效').getByRole('button', { name: '关闭接入范围', exact: true }).click()
        const closeDialog = page.getByRole('dialog', { name: '关闭受控接入范围', exact: true })
        await fit(closeDialog, 'close-confirmation', platform)
        await closeDialog.getByRole('button', { name: '确认关闭范围' }).click()
        await closeDialog.waitFor({ state: 'hidden' })
        check(writes.at(-1).path.endsWith('/close') &&
          projectScope(scopes.get(platform)[0]).human_verification_required === true,
        'Close called a different endpoint or retained a live exemption')
        const closed = scopes.get(platform)[0], closedDeadline = closed.expires_at,
          reopenBefore = writes.length, closedKey = writes.findLast(item =>
            item.path === '/test-tools/onboarding/scopes').key
        await row('已关闭').first().getByRole('button', { name: '重新开启接入范围', exact: true }).click()
        check(await editor().getByRole('spinbutton').inputValue() === '360' &&
          writes.length === reopenBefore,
        'Opening the reopen editor changed the old scope or lost the six-hour default')
        await editor().getByRole('button', { name: '核对接入范围' }).click()
        await confirmation().getByRole('button', { name: '确认创建范围' }).click()
        await editor().waitFor({ state: 'hidden' })
        const reopened = scopes.get(platform)[0]
        check(reopened.id !== closed.id && reopened.state === 'active' && closed.state === 'closed' &&
          closed.expires_at === closedDeadline && reopened.native_attempts_used === 0 &&
          writes.length === reopenBefore + 1 && writes.at(-1).path === '/test-tools/onboarding/scopes' &&
          writes.at(-1).key !== closedKey &&
          reopened.account_id === closed.account_id && reopened.claimant_account_id === closed.claimant_account_id &&
          reopened.group_id === closed.group_id,
        'Explicit reopen revived old scope, reused its key or changed its account/group selection')
        await row('有效').getByRole('button', { name: '关闭接入范围', exact: true }).click()
        await closeDialog.getByRole('button', { name: '确认关闭范围' }).click()
        await closeDialog.waitFor({ state: 'hidden' })
        check(reopened.state === 'closed', 'Reopened scope could not be manually switched off')

        // Normal entry stays explicit and sends no controlled_scope_id.
        await page.evaluate(() => window.__controlledHarness.userCenter())
        await page.getByRole('button', { name: '打开接入向导' }).click()
        await modal().waitFor()
        await select(modal(), '接入范围', '普通接入（本人非号池账号）')
        await modal().getByText(/请使用未接入号池的本人群主或管理员账号/).waitFor()
        const ordinaryBefore = writes.filter(item => item.path === '/group-binding/challenges').length,
          ordinaryCaptcha = captchaCalls().length
        await modal().getByRole('button', { name: '开始核验' }).click()
        await captchaDialog().waitFor()
        check(writes.filter(item => item.path === '/group-binding/challenges').length === ordinaryBefore &&
          captchaCalls().length === ordinaryCaptcha + 1, 'Ordinary creation bypassed captcha')
        await solve()
        await modal().getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
        check(!('controlled_scope_id' in writes.findLast(item => item.path === '/group-binding/challenges').body) &&
          writes.findLast(item => item.path === '/group-binding/challenges').has_human_verification &&
          captchaCalls().length === ordinaryCaptcha + 2 &&
          !await modal().locator('.onboarding-controlled-notice').count(),
        'Normal flow inherited scope or omitted its captcha ticket')
        await fit(modal(), 'ordinary-warning', platform)
        await prepareContact(platform)
        await modal().getByText('已选中请求，尚未接受', { exact: true }).waitFor()
        const ordinaryFriendBefore = friendMutations().length
        await modal().getByRole('button', { name: '接受此好友请求', exact: true }).click()
        await friendConfirmation('approve').getByRole('button', { name: '确认并安全验证', exact: true }).click()
        await captchaDialog().waitFor()
        check(friendMutations().length === ordinaryFriendBefore &&
          captchaCalls().length === ordinaryCaptcha + 3, 'Ordinary contact approval bypassed captcha')
        await solve()
        await modal().getByText('好友申请已接受', { exact: true }).waitFor()
        check(friendMutations().length === ordinaryFriendBefore + 1 &&
          friendMutations().at(-1).has_human_verification && captchaCalls().length === ordinaryCaptcha + 4,
        'Ordinary contact approval omitted its challenge-scoped ticket')
        await advance(60001)
        await modal().locator('.onboarding-expired.el-alert--error[role="alert"]').waitFor()
        check(await modal().getByRole('button', { name: '重新开始核验' }).count(),
          'Ordinary challenge expiry lost restart')
        await fit(modal(), 'ordinary-expired', platform)
        await closeWizard()
      }
      // Large candidate sets remain scrollable; equal names never select a different ID.
      for (const platform of platforms) {
        for (const count of [100, 300]) {
          const options = Array.from({ length: count }, (_, index) => ({
            id: id(10000 + platforms.indexOf(platform) * 1000 + count * 2 + index),
            name: index === 0 || index === count - 1 ? '同名隔离群' : `${platform}候选隔离群${index + 1}`,
            native_id: `isolated-${platform}-${count}-${index + 1}`
          }))
          const other = options[0], target = options.at(-1)
          check(other.name === target.name && other.id !== target.id && other.native_id !== target.native_id,
            'Same-name candidate fixture did not distinguish immutable IDs')
          candidateGroups.set(platform, options)
          seed(platform)
          await page.goto(base, { waitUntil: 'domcontentloaded', timeout: 20000 })
          await page.waitForFunction(() => Boolean(window.__controlledHarness))
          await page.evaluate(platform => window.__controlledHarness.platform(platform), platform)
          await row('有效').waitFor()
          const before = writes.length
          await page.getByRole('button', { name: '创建接入范围', exact: true }).click()
          await select(editor(), '接入申请人账号', `${platform}申请人 · claimant-${platform}`)
          for (const width of widths) {
            await fit(editor(), `candidates-${count}-editor`, platform, [width])
            const picker = editor().getByRole('combobox', { name: '接入隔离群', exact: true })
            await picker.locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
            const listID = await picker.getAttribute('aria-controls')
            check(listID, 'Group combobox did not identify its candidate list')
            const list = page.locator(`[id=${JSON.stringify(listID)}]`)
            await list.getByRole('option', { name: `${target.name} · ${target.native_id}`, exact: true }).waitFor()
            const renderedCount = await list.getByRole('option').count()
            check(renderedCount === count,
              `${platform}/${count}/${width}: expected ${count} group candidates, got ${renderedCount}`)
            const targetOption = list.getByRole('option', { name: `${target.name} · ${target.native_id}`, exact: true })
            await targetOption.scrollIntoViewIfNeeded()
            await capture(`candidates-${count}-dropdown`, platform, width)
            const bounds = await list.evaluate(element => {
              const popup = element.closest('.el-select-dropdown')
              const wrap = popup.querySelector('.el-select-dropdown__wrap')
              const rect = popup.getBoundingClientRect()
              return { valid: rect.x >= -1 && rect.right <= innerWidth + 1 && rect.y >= -1 &&
                rect.bottom <= innerHeight + 1 && wrap.clientHeight <= 274 &&
                wrap.scrollHeight > wrap.clientHeight && wrap.clientHeight > 0,
              rect: rect.toJSON(), list_height: wrap.clientHeight, content_height: wrap.scrollHeight }
            })
            if (!bounds.valid) candidateLayoutFailures.push({ platform, count, width, ...bounds })
            check(await list.evaluate(element => element.closest('.el-select-dropdown')
              .querySelector('.el-select-dropdown__wrap').scrollTop > 0),
            `${platform}/${count}/${width}: final candidate was not reachable by list scrolling`)
            await picker.fill(target.name)
            await advance(350)
            check(await list.getByRole('option').count() === 2,
              `${platform}/${count}/${width}: same-name filtering lost one candidate`)
            await list.getByRole('option', { name: `${other.name} · ${other.native_id}`, exact: true }).waitFor()
            await picker.fill(target.native_id)
            await advance(350)
            check(await list.getByRole('option').count() === 1,
              `${platform}/${count}/${width}: native-ID filtering was not exact`)
            await list.getByRole('option', { name: `${target.name} · ${target.native_id}`, exact: true }).click()
            await editor().getByRole('button', { name: '核对接入范围' }).and(page.locator(':enabled')).waitFor()
            await editor().getByRole('button', { name: '核对接入范围' }).click()
            await confirmation().getByText(`${target.name} · ${target.native_id}`, { exact: true }).waitFor()
            check(!await confirmation().getByText(`${other.name} · ${other.native_id}`, { exact: true }).count() &&
              writes.length === before, 'Filtering or viewing confirmation submitted or changed the selected group')
            await fit(confirmation(), `candidates-${count}-selected`, platform, [width])
            candidateCases.push({ platform, count, width, selected_group_id: target.id,
              selected_native_id: target.native_id, list_height: bounds.list_height,
              content_height: bounds.content_height, exact_same_name_selection: true })
            if (width !== widths.at(-1)) await returnToEditor()
          }
          await confirmation().getByRole('button', { name: '确认创建范围' }).click()
          await editor().waitFor({ state: 'hidden' })
          check(writes.length === before + 1 && writes.at(-1).path === '/test-tools/onboarding/scopes' &&
            writes.at(-1).body.group_id === target.id &&
            scopes.get(platform)[0].group.id === target.id &&
            scopes.get(platform)[0].group.native_id === target.native_id,
          `${platform}/${count}: posted scope selected another same-name group`)
        }
        candidateGroups.set(platform, groups.filter(item => item.platform === platform))
      }
      // Older servers omit the exemption field; expiry during the fallback captcha must block creation.
      now = await page.evaluate(() => Date.now())
      seed('wsl')
      const legacyScope = scopes.get('wsl').find(item => item.state === 'active')
      legacyScope.expires_at = new Date(now + 30000).toISOString()
      delete legacyScope.human_verification_required
      check(!('human_verification_required' in projectScope(legacyScope)),
        'Legacy fixture accidentally projected an explicit exemption')
      await page.evaluate(() => window.__controlledHarness.admin())
      await page.evaluate(() => window.__controlledHarness.platform('wsl'))
      await row('有效').getByRole('button', { name: '在用户中心接入' }).click()
      await openWizard()
      const duringCaptcha = writes.filter(item => item.path === '/group-binding/challenges').length
      const fallbackCaptcha = captchaCalls().length
      await modal().getByRole('button', { name: '开始核验' }).click()
      await captchaDialog().waitFor()
      check(captchaCalls().length === fallbackCaptcha + 1 &&
        captchaCalls().at(-1).body.purpose === 'group_binding_create' &&
        writes.filter(item => item.path === '/group-binding/challenges').length === duringCaptcha,
      'Missing exemption field failed to preserve the backward-compatible captcha')
      await advance(30001)
      await modal().locator('.onboarding-controlled-expired').waitFor()
      await captchaDialog().waitFor({ state: 'hidden' })
      check(writes.filter(item => item.path === '/group-binding/challenges').length === duringCaptcha &&
        captchaCalls().length === fallbackCaptcha + 1,
        'Scope expiry during captcha submitted a challenge')
      await closeWizard()
      await page.evaluate(() => window.__controlledHarness.admin())

      // Held list responses cannot cross an actor/platform change.
      const delayed = holdNext('/test-tools/onboarding/scopes')
      await page.getByRole('button', { name: '刷新接入范围' }).click()
      await delayed.start
      actor = id(8); seed('lajiaoliao')
      await page.evaluate(actor => window.__controlledHarness.actor(actor, 'lajiaoliao'), actor)
      delayed.release()
      await row('有效').waitFor()
      check(!/robot-wsl|claimant-wsl|isolated-wsl/.test(await page.locator('.controlled-onboarding').innerText()),
        'Late scope response crossed actor/platform')
      check(!unexpected.length, 'Unexpected APIs: ' + JSON.stringify(unexpected))
      check(!writes.some(item => /test-tools\/call|invitation-|\/confirm$/.test(item.path) ||
        /\/friend-/.test(item.path) && !/\/friend-(candidate|approve|reject)$/.test(item.path) &&
          !/^\/group-binding\/challenges\/[^/]+\/friend-attempts\/[^/]+\/reconcile$/.test(item.path)) &&
        friendMutations().length === platforms.length * 3 &&
        writes.filter(item => /\/friend-attempts\/[^/]+\/reconcile$/.test(item.path)).length === platforms.length &&
        writes.filter(item => item.path.endsWith('/friend-candidate')).length === platforms.length * 3,
      'Synthetic scope UI called an unexpected action or replayed a friend interaction')
      return { passed: candidateLayoutFailures.length === 0, synthetic_only: true, source_components_only: true, platforms, widths,
        scope_states: ['preparing', 'active', 'expired', 'closed', 'unknown'],
        testing_switch_and_distinct_claimant: true, scoped_captcha_exemption: true,
        controlled_create_and_restart_without_captcha_or_ticket: true,
        ordinary_create_and_contact_captcha_required: true,
        missing_exemption_field_captcha_fallback: true,
        controlled_friend_second_confirmation: true, controlled_expired_rejection_second_confirmation: true,
        friend_sealed_operation_id_preserved: true, prepared_get_and_restore_readonly: true,
        matching_unknown_scope_original_contact_readback: true, readback_keeps_scope_unknown: true,
        duplicate_friend_click_no_replay: true,
        controlled_native_group_without_restore: true,
        candidate_counts: [100, 300], bounded_candidate_lists: candidateLayoutFailures.length === 0,
        candidate_layout_failures: candidateLayoutFailures,
        exact_same_name_group_selection: true, candidate_cases: candidateCases,
        chat_pool_claimant_switch: true, six_decimal_cost_and_unknown_readonly: true,
        scope_and_challenge_expiry: true, six_hour_default_and_expiry: true,
        explicit_close_and_fresh_reopen: true, renewal_creates_new_id: true,
        ordinary_non_pool_warning: true, progress_get_only: true, actor_platform_isolation: true,
        native_calls: 0, model_calls: 0, writes: writes.length, reads: reads.length, screenshots }
    } finally {
      release?.()
      await page.unroute('**/api/v1/**', fixture)
    }
  }

  const sourceCheck = async () => {
    const fs = require('node:fs'), path = require('node:path'), { createRequire } = require('node:module')
    const deps = process.env.WSL_UI_NODE_MODULES
    if (!deps || !path.isAbsolute(deps)) throw Error('Set WSL_UI_NODE_MODULES to existing web dependencies')
    const dependency = createRequire(path.join(deps, 'package.json'))
    const compiler = dependency('@vue/compiler-sfc')
    const esbuild = createRequire(dependency.resolve('vite'))('esbuild')
    const importedPlaywright = await import(process.env.WSL_PLAYWRIGHT_MODULE ||
      require.resolve('playwright'))
    const playwright = importedPlaywright.default || importedPlaywright
    const screenshotDirectory = process.env.WSL_UI_SCREENSHOT_DIR
    if (screenshotDirectory && !path.isAbsolute(screenshotDirectory))
      throw Error('WSL_UI_SCREENSHOT_DIR must be an absolute artifact directory')
    const saveScreenshot = screenshotDirectory ? async (metadata, image) => {
      if (![320, 1440].includes(metadata.width) ||
        !(['scope-confirmation', 'scope-selected', 'scope-expired'].includes(metadata.state) ||
          /^candidates-(100|300)-(dropdown|selected)$/.test(metadata.state))) return
      await fs.promises.mkdir(screenshotDirectory, { recursive: true })
      const artifact = path.join(screenshotDirectory, `${metadata.platform}-${metadata.state}-${metadata.width}.png`)
      await fs.promises.writeFile(artifact, image)
      return artifact
    } : undefined
    const root = path.resolve(__dirname, '..'), product = path.join(root, 'web/src/wsl')
    const entry = `
      import { createApp, h, ref } from 'vue'
      import { createPinia } from 'pinia'
      import { ElButton } from 'element-plus'
      import TestTools from './TestTools.vue'
      import CustomerOnboarding from './CustomerOnboarding.vue'
      import { useSession } from './store'
      import { api } from './api'
      import { controlledOnboardingSelectionKey } from './customer'
      import 'element-plus/dist/index.css'
      import './theme/lumenwave/system.css'
      import './style.css'
      import './theme/product.css'
      const app = createApp({ setup() {
        const session = useSession(), accounts = ref([]), open = ref(false)
        const ready = (async () => {
          const me = await api('/me'); session.setUser(me.user, me.permissions)
          session.panels = ['wsl', 'lajiaoliao'].map(id => ({ id, name: id }))
          accounts.value = (await Promise.all(['wsl', 'lajiaoliao'].map(platform =>
            api('/accounts', 'GET', undefined, undefined, platform)))).flat()
          session.selectConsole('admin'); session.selectWorkspace('wsl')
          session.selectAccount(accounts.value[0].id)
          window.__controlledHarness = {
            platform(platform) { open.value = false; session.selectWorkspace(platform);
              session.selectAccount(accounts.value.find(item => item.platform === platform).id) },
            testing(value) { accounts.value.find(item => item.id === session.selectedAccount).testing_enabled = value },
            claimantTesting(value) { const item = accounts.value.find(item => item.platform === session.workspace &&
              item.id !== session.selectedAccount); if (item) item.testing_enabled = value },
            userCenter() { session.selectConsole('user') },
            admin() { open.value = false; session.selectConsole('admin') },
            actor(id, platform) { open.value = false; session.setUser({ ...session.user, id }, session.permissions);
              session.selectConsole('admin'); this.platform(platform) }
          }
        })()
        return () => h('main', { style: 'padding:12px;max-width:1100px;margin:auto' }, [
          session.user && session.console === 'admin' ? h(TestTools, {
            key: session.user.id + session.workspace + session.selectedAccount,
            workspace: session.workspace, account: session.selectedAccount,
            accounts: accounts.value, groups: [], windows: [],
            onOnboard(scope) {
              sessionStorage.setItem(controlledOnboardingSelectionKey(session.user.id,
                session.user.tenant_id, scope.platform), scope.id)
              session.selectConsole('user')
            }
          }) : session.user ? h(ElButton, { onClick: () => open.value = true }, () => '打开接入向导') : null,
          open.value ? h(CustomerOnboarding, {
            key: session.user.id + session.workspace, platforms: ['wsl', 'lajiaoliao'],
            initialPlatform: session.workspace, operatorDemo: true, onClose: () => open.value = false,
            onControlledTools(platform) { session.selectConsole('admin'); session.selectWorkspace(platform) }
          }) : null
        ])
      } })
      app.use(createPinia()).mount('#app')
    `
    let componentID = 0
    const built = await esbuild.build({
      stdin: { contents: entry, resolveDir: product, loader: 'js' }, bundle: true, write: false,
      outfile: path.join(root, 'synthetic.js'), format: 'iife', platform: 'browser',
      define: { 'process.env.NODE_ENV': '"production"', __VUE_OPTIONS_API__: 'true', __VUE_PROD_DEVTOOLS__: 'false' },
      nodePaths: [deps], logLevel: 'silent',
      plugins: [{
        name: 'synthetic-product-components',
        setup(build) {
          build.onResolve({ filter: /^(vue|pinia|element-plus|@element-plus\/icons-vue)(\/.*)?$/ },
            args => ({ path: dependency.resolve(args.path) }))
          build.onLoad({ filter: /\.vue$/ }, args => {
            const parsed = compiler.parse(fs.readFileSync(args.path, 'utf8'), { filename: args.path })
            if (parsed.errors.length) throw parsed.errors[0]
            const id = `controlled-source-${++componentID}`, descriptor = parsed.descriptor
            const script = compiler.compileScript(descriptor, { id, genDefaultAs: '__component', inlineTemplate: true })
            const styles = descriptor.styles.map(style => {
              const result = compiler.compileStyle({ source: style.content, filename: args.path, id: `data-v-${id}`, scoped: style.scoped })
              if (result.errors.length) throw result.errors[0]
              return result.code
            }).join('\n')
            return { loader: 'ts', contents: script.content +
              `\n__component.__scopeId = ${JSON.stringify(`data-v-${id}`)};
              const style = document.createElement('style'); style.textContent = ${JSON.stringify(styles)};
              document.head.appendChild(style); export default __component;` }
          })
        }
      }]
    })
    const javascript = Buffer.from(built.outputFiles.find(file => file.path.endsWith('.js')).contents)
    const css = Buffer.from(built.outputFiles.find(file => file.path.endsWith('.css')).contents)
    const browser = await playwright.chromium.launch({
      headless: true, channel: process.env.WSL_BROWSER_CHANNEL || 'chrome',
      ...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH } : {})
    })
    const page = await browser.newPage(), errors = []
    page.on('pageerror', error => errors.push(error.message))
    await page.route('**/*', async route => {
      const url = new URL(route.request().url())
      if (url.origin !== 'http://127.0.0.1:5173') return route.abort('blockedbyclient')
      if (url.pathname === '/') return route.fulfill({ contentType: 'text/html',
        body: '<!doctype html><html lang="zh-CN"><meta charset="UTF-8"><link rel="stylesheet" href="/synthetic.css"><div id="app"></div><script src="/synthetic.js"></script></html>' })
      if (url.pathname === '/synthetic.js') return route.fulfill({ contentType: 'text/javascript', body: javascript })
      if (url.pathname === '/synthetic.css') return route.fulfill({ contentType: 'text/css', body: css })
      if (['/wsl-logo.png', '/lajiaoliao-logo.png'].includes(url.pathname))
        return route.fulfill({ contentType: 'image/png', body: fs.readFileSync(path.join(root, 'web/public', url.pathname)) })
      return route.fulfill({ status: 500, json: { code: 1, msg: 'unmocked_api' } })
    })
    try {
      const result = await smoke(page, saveScreenshot)
      if (errors.length) throw Error('Browser runtime errors: ' + errors.join('; '))
      console.log(JSON.stringify({ check: 'controlled_onboarding_ui_smoke.js --source', ...result }))
      if (!result.passed) process.exitCode = 1
    } catch (error) {
      const { failureDetails } = await import('./ci/browser-contract.mjs')
      const failure = failureDetails(error, 'tools/controlled_onboarding_ui_smoke.js')
      const context = String(error.message).split('\n').filter(line =>
        /waiting for getByRole|intercepts pointer events|element is not (visible|stable|enabled)|outside of the viewport/.test(line))
        .slice(0, 4).map(line => failureDetails(new Error(line.trim()), '').message)
      console.error('Error: ' + JSON.stringify({ ...failure, context }))
      if (screenshotDirectory)
        await page.screenshot({ path: path.join(screenshotDirectory, 'controlled-onboarding-current-failure.png'),
          animations: 'disabled' }).catch(() => {})
      throw error
    } finally { await browser.close() }
  }
  if (typeof process !== 'undefined' && process.argv[1]?.endsWith('controlled_onboarding_ui_smoke.js') &&
    process.argv.includes('--source'))
    sourceCheck().catch(error => { console.error(error); process.exitCode = 1 })
  return smoke
})()
