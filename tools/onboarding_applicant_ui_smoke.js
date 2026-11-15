// Real TestTools/Element Plus, shared confirmation harness, synthetic APIs only.
// WSL_UI_NODE_MODULES=/absolute/web/node_modules WSL_PLAYWRIGHT_MODULE=/absolute/playwright/index.mjs \
//   node tools/onboarding_applicant_ui_smoke.js
const assert = require('node:assert/strict')
const path = require('node:path')
const { createTestToolsBrowser } = require('./test_tools_confirmation_ui_smoke')

async function main() {
  const { browser, page, assets } = await createTestToolsBrowser({ messages: true })
  const errors = [], unexpected = [], checks = []
  const checkpoint = value => {
    checks.push(value)
    console.log(JSON.stringify({ check: path.basename(__filename), stage: value }))
  }
  page.on('pageerror', error => errors.push(error.message))
  const id = n => `f9200000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const user = { id: id(1), username: 'synthetic-applicant', role: 'superadmin' }
  const proofTexts = { private: `群接入 ${'P'.repeat(43)}`, group: `群绑定 ${'G'.repeat(43)}` }
  const conversationKeys = { private: 'a'.repeat(64), group: 'b'.repeat(64), other: 'c'.repeat(64) }
  const start = Date.now()
  let elapsed = 0, fixture, challenge, posts, claimant, sendStatus, heldRead, candidateGroups
  let proofWindows, proofResponsePatch, abortProof, heldProof, proofTTL, textOperation, textRequests, textSendStatus
  let recoveryOnly = false, recoveryReads = 0
  let retiredOperation = null
  let evidenceFailure = 'lajiaoliao_evidence_sequences_rejected'
  const now = () => start + elapsed
  await page.clock.install({ time: new Date(start) })
  await page.clock.setFixedTime(new Date(start))
  await page.route('**/*', async route => {
    const req = route.request(), url = new URL(req.url()), pathname = url.pathname
    const fulfill = data => route.fulfill({ json: { code: 0, data, msg: '' } })
    if (url.origin !== 'http://127.0.0.1:5173') {
      unexpected.push(req.url()); return route.abort('blockedbyclient')
    }
    if (pathname === '/') return route.fulfill({ contentType: 'text/html',
      body: '<!doctype html><meta charset="UTF-8"><link rel="stylesheet" href="/fixture.css"><div id="app"></div><script src="/fixture.js"></script>' })
    if (pathname === '/fixture.js') return route.fulfill({
      contentType: 'text/javascript', body: assets.get('.js') })
    if (pathname === '/fixture.css') return route.fulfill({
      contentType: 'text/css', body: assets.get('.css') })
    const api = pathname.replace('/api/v1', '')
    if (req.method() === 'POST' && api === `/accounts/${id(2)}/native-text-operations/${id(20)}/reconcile`) {
      assert.equal(req.headers()['x-workspace'], 'lajiaoliao')
      assert.deepEqual(req.postDataJSON(), {})
      posts.push({ api, body: {} })
      return route.fulfill({ status: 503, json: {
        code: 503, msg: evidenceFailure
      } })
    }
    if (api === '/test-tools/onboarding/scopes' && req.method() === 'GET') {
      assert.equal(req.headers()['x-workspace'], fixture.workspace)
      assert.equal(url.searchParams.get('platform'), fixture.workspace)
      return fulfill({ items: [], candidates: { accounts: [], groups: candidateGroups } })
    }
    if (/^\/accounts\/[^/]+\/receiver$/.test(api) && req.method() === 'GET')
      return fulfill({ enabled: true, revision: 1, observed: true, snapshot: { state: 'connected' } })
    if (api === '/test-tools/operations' && req.method() === 'GET') return fulfill([])
    if (api.startsWith(`/accounts/${id(2)}/`) && req.method() === 'GET') {
      assert.equal(req.headers()['x-workspace'], fixture.workspace)
      if (api.endsWith('/events'))
        return route.fulfill({ contentType: 'text/event-stream', body: '' })
      if (api.endsWith('/decoder-health')) return fulfill({
        account_id: id(2), sampled_at: new Date(now()).toISOString(), status: 'healthy',
        receiver: { enabled: true, observed: true, snapshot: { state: 'connected' } },
        intake: {}, projection: {}, recent: [], counts: {}, retention_days: 7
      })
      if (api.endsWith('/conversations')) {
        const scene = url.searchParams.get('scene')
        const items = ['private', 'group', 'other'].map(kind => ({
          key: conversationKeys[kind], scene: kind === 'group' ? 'group' : 'private',
          title: kind === 'private' ? '合成接收机器人私聊' : kind === 'group' ? '合成隔离群会话' : '无关私聊',
          business_id: kind === 'private' ? '7001' : kind === 'group' ? '126' : '9999',
          transport: 'persisted-route', last_at: new Date(now()).toISOString(), count: 1,
          last: { outgoing: false, message: { text: '已保存会话证据' } }
        })).filter(item => !scene || item.scene === scene)
        return fulfill({ items, total: items.length, directory: { groups: [] }, directory_total: 0 })
      }
      const kind = Object.keys(conversationKeys).find(kind => api.includes(`/${conversationKeys[kind]}/`))
      if (kind && api.endsWith('/messages')) return fulfill({ items: [{
        id: kind === 'group' ? '502' : '501', state: 'decoded', received_at: new Date(now()).toISOString(),
        content: { platform: fixture.workspace, scene: kind === 'group' ? 'group' : 'private',
          outgoing: false, native_name: '合成接收机器人', native_target: kind === 'group' ? '126' : '7001',
          message: { text: '已持久化真实源事件', sender: { id: 7001, name: '合成接收机器人' },
            target: { id: kind === 'group' ? 126 : 8002, name: '合成申请账号' } } }
      }], next_before: '', more: false })
      if (kind && api.endsWith('/sending')) {
        if (recoveryOnly) return route.fulfill({ status: 409, json: {
          code: 'onboarding_controlled_proof_window_invalid', msg: '原测试范围已失效'
        } })
        const window = proofWindows.get(kind)?.window
        const available = window && window.remaining > 0 && Date.parse(window.expires_at) > now() + 3000
        return fulfill({ enabled: !!available, reason: available ? '' : 'native_text_test_window_required',
          scope: { platform: fixture.workspace, scene: kind === 'group' ? 'group' : 'private',
            group_id: kind === 'group' ? id(3) : '', account_epoch: 1, binding_epoch: kind === 'group' ? 1 : 0 },
          test_windows: available ? [{ ...window, actions: [kind === 'group' ? 'send_text' : 'send_private_text'] }] : [],
          recent: [textOperation, retiredOperation].filter(Boolean), max_text_bytes: 3000,
          media_enabled: false, upload_enabled: false, media_kinds: [], recent_uploads: [] })
      }
      if (kind && api.endsWith('/sending-targets')) return fulfill({ members: [], quotes: [] })
      if (api.endsWith('/native-text-operations') && url.searchParams.has('conversation')) {
        assert.equal(url.searchParams.get('conversation'), conversationKeys.private)
        recoveryReads++
        return fulfill({ items: textOperation ? [textOperation] : [] })
      }
      if (retiredOperation && api.endsWith('/native-text-operations/' + retiredOperation.id)) {
        recoveryReads++
        return fulfill(retiredOperation)
      }
      if (api.includes('/native-text-operations/') && textOperation) return fulfill(textOperation)
    }
    if (api === `/group-binding/challenges/${challenge.id}` && req.method() === 'GET') {
      const response = structuredClone(challenge)
      if (heldRead) await heldRead.promise
      return fulfill(response)
    }
    if (req.method() === 'POST') {
      const body = req.postDataJSON()
      assert.equal(req.headers()['x-workspace'], fixture.workspace)
      assert.ok(!['tenant_id', 'actor_id', 'account_id', 'robot', 'owner'].some(key => key in body))
      posts.push({ api, body, key: req.headers()['idempotency-key'] })
      if (api.startsWith(`/accounts/${id(2)}/conversations/`)) {
        const kind = ['private', 'group'].find(kind => api.includes(`/${conversationKeys[kind]}/`))
        assert.ok(kind, 'Text send did not select an existing source conversation')
        const window = proofWindows.get(kind)?.window
        assert.ok(window)
        if (api.endsWith('/text-operations')) {
          assert.ok(!textOperation, 'Proof message staging was replayed')
          assert.deepEqual(body, { text: proofTexts[kind], test_window_id: window.id,
            account_epoch: 1, binding_epoch: 1,
            ...(fixture.workspace === 'lajiaoliao' && kind === 'group'
              ? { reply_to: '', chili_mention_id: '' } : {}) })
          assert.ok(req.headers()['idempotency-key'])
          textRequests.push({ phase: 'stage', kind, api, body })
          textOperation = { id: id(20), status: 'queued', result_code: '',
            kind: 'send_text', test_window_id: window.id }
          return fulfill(textOperation)
        }
        if (api.endsWith(`/text-operations/${id(20)}/dispatch`)) {
          assert.equal(textOperation?.status, 'queued')
          assert.deepEqual(body, {})
          textRequests.push({ phase: 'dispatch', kind, api, body })
          window.remaining = 0
          textOperation = { ...textOperation, status: textSendStatus }
          return fulfill(textOperation)
        }
      }
      if (api === `/test-tools/onboarding-friend/challenges/${challenge.id}/proof-windows`) {
        const kind = body.kind, requestKey = req.headers()['idempotency-key']
        assert.ok(claimant && ['private', 'group'].includes(kind))
        assert.deepEqual(body, {
          version: challenge.version, kind, text: proofTexts[kind], seconds: 300
        })
        assert.equal(kind === 'group', challenge.identity_proven)
        assert.match(requestKey, /^[^\s]{8,100}$/)
        let entry = proofWindows.get(kind)
        if (entry) {
          assert.equal(requestKey, entry.key, 'Retry replaced the proof window idempotency key')
          assert.deepEqual(body, entry.body, 'Retry changed the original proof request')
        } else {
          entry = { key: requestKey, body, window: {
            id: id(kind === 'private' ? 14 : 15), account_id: id(2), proof_kind: kind,
            challenge_version: challenge.version, group_id: id(3), native_group: '126',
            account_epoch: 1, binding_epoch: 1,
            expires_at: new Date(now() + proofTTL).toISOString(),
            remaining: 1, admission_mode: 'native_debug'
          } }
          proofWindows.set(kind, entry)
        }
        if (abortProof) { abortProof = false; return route.abort('failed') }
        const response = structuredClone(entry.window)
        proofResponsePatch?.(response)
        if (heldProof) await heldProof.promise
        return fulfill(response)
      }
      if (api === '/test-tools/onboarding-friend/windows') {
        assert.deepEqual(body, {
          challenge_id: challenge.id, version: challenge.version, group_id: id(3),
          account: claimant ? 'synthetic_receiver' : 'synthetic_peer',
          target_id: claimant ? '7001' : '8002',
          seconds: 300, budget: 1, confirm_isolated: true,
          ...(claimant ? { as_claimant: true } : {})
        })
        return fulfill({ id: id(4), admission_mode: 'native_debug', budget: 1, used: 0,
          expires_at: new Date(now() + 300000).toISOString() })
      }
      if (api === `/test-tools/onboarding-friend/challenges/${challenge.id}/friend-target`) {
        assert.deepEqual(body, { test_window_id: id(4), version: challenge.version,
          account: claimant ? 'synthetic_receiver' : 'synthetic_peer' })
        assert.ok(req.headers()['idempotency-key'])
        challenge = { ...challenge, friend_outbound: { id: id(9), status: 'prepared',
          target: { native_id: claimant ? '7001' : '8002',
            add_account: body.account, name: '合成好友目标' } } }
        return fulfill(challenge)
      }
      if (api === `/test-tools/onboarding-friend/challenges/${challenge.id}/friend-send`) {
        assert.deepEqual(body, { test_window_id: id(4), version: challenge.version })
        assert.ok(req.headers()['idempotency-key'])
        challenge = { ...challenge,
          friend_outbound: { ...challenge.friend_outbound, status: sendStatus } }
        return fulfill(challenge)
      }
    }
    unexpected.push(`${req.method()} ${pathname}`)
    return route.fulfill({ status: 500, json: { code: 1, msg: 'unmocked_api' } })
  })
  const tool = page.locator('.test-tools')
  const dialog = page.getByRole('dialog', { name: '受控搜索加好友', exact: true })
  const account = dialog.getByRole('textbox', { name: '受控好友搜索账号', exact: true })
  const target = dialog.getByRole('textbox', { name: '受控好友目标原生ID', exact: true })
  const record = dialog.getByRole('textbox', { name: '受控好友接入记录', exact: true })
  const newRecord = dialog.getByRole('button', { name: '新接入记录', exact: true })
  const read = dialog.getByRole('button', { name: '读取原记录', exact: true })
  const create = dialog.getByRole('button', { name: '创建本次授权', exact: true })
  const search = dialog.getByRole('button', { name: '搜索授权目标', exact: true })
  const send = dialog.getByRole('button', { name: '确认发送好友申请', exact: true })
  const proofInput = kind => dialog.getByRole('textbox', {
    name: kind === 'private' ? '私聊证明原文' : '群证明原文', exact: true
  })
  const proofButton = kind => dialog.getByRole('button', {
    name: kind === 'private' ? '创建私聊证明窗口' : '创建群证明窗口', exact: true
  })
  const proofID = kind => dialog.getByRole('textbox', {
    name: kind === 'private' ? '私聊证明窗口ID' : '群证明窗口ID', exact: true
  })
  const confirmation = page.locator('.el-message-box:visible')
  const idle = () => tool.getByRole('button', { name: '刷新检查', exact: true })
    .and(page.locator(':enabled')).waitFor()
  const choose = async (label, value) => {
    await page.getByRole('combobox', { name: label, exact: true })
      .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option', { name: value, exact: true }).click()
  }
  const configure = async (platform, options = {}) => {
    claimant = options.receiver !== true
    posts = []; sendStatus = 'submitted'; heldRead = null
    proofWindows = new Map(); proofResponsePatch = null; abortProof = false; heldProof = null
    proofTTL = 300000
    textOperation = null; textRequests = []; textSendStatus = 'succeeded'
    retiredOperation = null
    recoveryOnly = false; recoveryReads = 0
    fixture = {
      workspace: platform, account: claimant ? id(2) : id(6),
      accounts: [2, 6].map(n => ({ id: id(n),
        name: n === 2 ? '合成申请账号' : '合成接收机器人', platform, purpose: 'management',
        native_uid: n === 2 ? '8002' : '7001', testing_enabled: true,
        connection_enabled: true, epoch: 1, version: 1 })),
      groups: [3, 13].map(n => ({ group_id: id(n), native_id: String(123 + n),
        name: n === 3 ? '合成隔离群' : '另一隔离群', platform })),
      windows: []
    }
    candidateGroups = options.candidateOnly
      ? fixture.groups.filter(group => group.group_id === id(3))
        .map(({ group_id, ...group }) => ({ ...group, id: group_id }))
      : []
    if (options.candidateOnly) fixture.groups = []
    const expires = new Date(now() + 300000).toISOString()
    challenge = {
      id: id(5), version: 11, platform, scope: { actor_id: user.id, tenant_id: id(8) },
      robot: { id: id(6), name: '合成接收机器人', native_id: '7001',
        add_available: true, add_account: 'synthetic_receiver' },
      customer: { name: '合成申请人' }, mode: 'operator_demo', friend_flow: 'incoming_contact',
      state: 'waiting', identity_proven: false, expires_at: expires,
      controlled_scope: {
        id: id(7), platform, actor_id: user.id, tenant_id: id(8), account_id: id(6),
        claimant_account_id: id(2), group_id: id(3),
        robot: { id: id(6), name: '合成接收机器人', native_id: '7001' },
        claimant: { id: id(2), name: '合成申请账号', native_id: '8002' },
        group: { id: id(3), name: '合成隔离群', native_id: '126' },
        state: 'active', blocked_reason: '', version: 3, expires_at: expires,
        native_attempts_used: 0, model_calls_used: 0, reserved_usdt: '0.000000'
      }
    }
    options.change?.(challenge)
    await page.evaluate(({ fixture, user }) => window.__testTools.configure(fixture, user),
      { fixture, user })
    await idle()
    await tool.getByRole('button', { name: '受控搜索加好友', exact: true }).click()
    await choose('受控好友隔离群组', '合成隔离群')
    await dialog.getByRole('textbox', { name: '受控好友接入记录', exact: true }).fill(id(5))
    await read.click(); await idle()
  }
  const prepare = async () => {
    await dialog.getByText('确认此群为自有隔离测试群', { exact: true }).click()
    await create.click(); await idle()
    await search.and(page.locator(':enabled')).waitFor()
    await search.click(); await idle()
    await send.and(page.locator(':enabled')).waitFor()
  }
  const friendStorageKey = platform => `wsl:controlled-friend:${user.id}:${platform}:${id(2)}:${id(3)}`
  const savedFriend = platform => page.evaluate(key =>
    JSON.parse(sessionStorage.getItem(key)), friendStorageKey(platform))
  const remountFriend = async () => {
    await page.evaluate(async () => { await window.__testTools.hide(); await window.__testTools.show() })
    await idle()
    await tool.getByRole('button', { name: '受控搜索加好友', exact: true }).click()
    await choose('受控好友隔离群组', '合成隔离群')
  }
  try {
    await page.goto('http://127.0.0.1:5173')
    // Repeated scope fixtures test cancellation/state, while the confirmation
    // smoke retains real Element Plus transitions and close-promise coverage.
    await page.addStyleTag({ content: '*,*::before,*::after{transition:none!important;animation:none!important}' })
    for (const platform of ['wsl', 'lajiaoliao']) {
      await configure(platform)
      assert.equal(posts.length, 0, 'Opening/reading the original challenge submitted a write')
      assert.equal(await account.inputValue(), 'synthetic_receiver')
      assert.equal(await target.inputValue(), '7001')
      assert.ok(await target.isDisabled())
      const direction = await dialog.getByLabel('受控好友申请方向').innerText()
      assert.ok(direction.includes('申请账号 合成申请账号') && direction.includes('接收机器人 合成接收机器人'))
      assert.ok(await create.isDisabled(), 'Isolated-group confirmation was skipped')
      await account.fill('manual_receiver')
      await read.click(); await idle()
      assert.equal(await account.inputValue(), 'manual_receiver', 'Reading replaced an unsaved search account')
      await account.fill('synthetic_receiver')
      await prepare()
      assert.equal(challenge.robot.id, id(6), 'Applicant operation changed the original receiver')
      assert.equal(challenge.scope.actor_id, user.id, 'Applicant operation changed the original owner')
      await send.click(); await confirmation.waitFor()
      await confirmation.getByRole('button', { name: '取消', exact: true }).click()
      await idle()
      assert.equal(posts.filter(row => row.api.endsWith('/friend-send')).length, 0)
      await send.click(); await confirmation.waitFor()
      sendStatus = 'unknown'
      await confirmation.getByRole('button', { name: '确认发送', exact: true }).click()
      await idle()
      assert.equal(posts.filter(row => row.api.endsWith('/friend-send')).length, 1)
      assert.ok(await send.isDisabled())
      await read.click(); await idle()
      assert.equal(posts.filter(row => row.api.endsWith('/friend-send')).length, 1,
        'Reading an unknown result replayed the original submission')
      assert.ok(await send.isDisabled())
      checkpoint(`${platform}/claimant-direction-fixed-target-budget1-cancel-unknown`)

      for (const state of ['cancelled', 'expired']) {
        await configure(platform); await prepare()
        const oldChallenge = structuredClone(challenge)
        const original = await savedFriend(platform)
        const oldWindow = { ...original.window, used: 0,
          ...(state === 'cancelled' ? { cancelled: true } : {
            expires_at: new Date(now() - 1000).toISOString()
          }) }
        // Reproduce v119 storage, which kept only the window ID and keys.
        await page.evaluate(({ key, oldWindow, original }) => {
          window.__testTools.state.windows = [oldWindow]
          sessionStorage.removeItem(`${key}:${original.challengeID}`)
          sessionStorage.setItem(key, JSON.stringify({
            challengeID: original.challengeID, windowID: oldWindow.id,
            prepareKey: original.prepareKey, sendKey: original.sendKey
          }))
        }, { key: friendStorageKey(platform), oldWindow, original })
        await remountFriend()
        assert.equal(await record.inputValue(), id(5))
        assert.ok(await record.isEnabled(), 'A restored old window locked the record input')
        await read.click(); await idle()
        assert.ok((await dialog.getByLabel('受控好友授权').innerText())
          .includes(state === 'cancelled' ? '已取消' : '已过期'))
        assert.ok(await send.isDisabled())
        await newRecord.click()
        assert.equal(await record.inputValue(), '')
        assert.equal(await dialog.getByLabel('受控好友授权').count(), 0)
        assert.equal(posts.length, 2, 'Choosing a new record created or renewed a window')
        challenge = { ...structuredClone(oldChallenge), id: id(25), version: 12 }
        delete challenge.friend_outbound
        await record.fill(challenge.id); await read.click(); await idle()
        assert.equal(await account.inputValue(), 'synthetic_receiver')
        assert.equal(await dialog.getByLabel('受控好友授权').count(), 0)
        await dialog.getByText('确认此群为自有隔离测试群', { exact: true }).click()
        assert.ok(await create.isEnabled(), 'An old window blocked an independent new challenge')
        await remountFriend()
        assert.equal(await record.inputValue(), id(25), 'Remount reselected the old cancelled/expired record')
        await read.click(); await idle()
        assert.equal(await dialog.getByLabel('受控好友授权').count(), 0)
        challenge = oldChallenge
        await record.fill(challenge.id); await read.click(); await idle()
        assert.ok((await dialog.getByLabel('受控好友授权').innerText()).includes(id(4)))
        assert.ok(await search.isDisabled())
        assert.ok(await send.isDisabled())
        assert.equal((await savedFriend(platform)).prepareKey, original.prepareKey)
        assert.equal(posts.length, 2, 'Restoring the original record resumed or replayed its request')
        checkpoint(`${platform}/${state}-legacy-window-new-record-remount-no-renewal`)
      }

      await configure(platform)
      await proofInput('private').fill(proofTexts.private)
      await proofButton('private').click(); await idle()
      await prepare(); await send.click(); await confirmation.waitFor()
      sendStatus = 'unknown'
      await confirmation.getByRole('button', { name: '确认发送', exact: true }).click()
      await idle()
      const oldUnknown = structuredClone(challenge)
      const originalFriend = await savedFriend(platform)
      const originalPosts = structuredClone(posts)
      assert.ok(originalFriend.prepareKey && originalFriend.sendKey)
      await newRecord.click()
      challenge = { ...structuredClone(oldUnknown), id: id(25), version: 12 }
      delete challenge.friend_outbound
      await record.fill(challenge.id); await read.click(); await idle()
      assert.equal(await proofInput('private').inputValue(), '')
      assert.equal(await proofID('private').count(), 0)
      assert.equal(await dialog.getByLabel('受控好友原操作').count(), 0)
      await choose('受控好友隔离群组', '另一隔离群')
      assert.equal(await record.inputValue(), '', 'Another group inherited the selected challenge')
      assert.equal(await dialog.getByLabel('受控好友授权').count(), 0)
      await choose('受控好友隔离群组', '合成隔离群')
      assert.equal(await record.inputValue(), id(25))
      await page.evaluate(() => { window.__testTools.state.account = window.__testTools.state.accounts[1].id })
      await idle()
      await tool.getByRole('button', { name: '受控搜索加好友', exact: true }).click()
      assert.equal(await record.inputValue(), '', 'Another account inherited the selected challenge')
      assert.equal(await dialog.getByLabel('受控好友授权').count(), 0)
      assert.equal(await proofID('private').count(), 0)
      await page.evaluate(() => { window.__testTools.state.account = window.__testTools.state.accounts[0].id })
      await idle()
      await tool.getByRole('button', { name: '受控搜索加好友', exact: true }).click()
      assert.equal(await record.inputValue(), id(25))
      challenge = oldUnknown
      await record.fill(challenge.id); await read.click(); await idle()
      await dialog.getByLabel('受控好友原操作').getByText(`操作：${id(9)}`, { exact: true }).waitFor()
      assert.equal(await proofID('private').inputValue(), id(14))
      assert.ok(await proofInput('private').isDisabled())
      assert.ok(await search.isDisabled())
      assert.ok(await send.isDisabled())
      await remountFriend(); await read.click(); await idle()
      const restoredFriend = await savedFriend(platform)
      assert.equal(restoredFriend.prepareKey, originalFriend.prepareKey)
      assert.equal(restoredFriend.sendKey, originalFriend.sendKey)
      assert.deepEqual(restoredFriend.proofs, originalFriend.proofs)
      assert.ok((await dialog.getByLabel('受控好友原操作').innerText()).includes('结果未知'))
      assert.ok(await send.isDisabled(), 'Restored unknown allowed the original submission again')
      assert.deepEqual(posts, originalPosts, 'Switching/reading an unknown operation submitted new work')
      checkpoint(`${platform}/unknown-record-id-readback-keys-proofs-group-account-isolation-no-replay`)

      await configure(platform, { candidateOnly: true })
      assert.equal(fixture.groups.length, 0, 'Candidate-only regression populated props.groups')
      assert.deepEqual(candidateGroups.map(group => group.id), [id(3)])
      assert.equal((await dialog.getByRole('combobox', {
        name: '受控好友隔离群组', exact: true
      }).locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]')
        .innerText()).trim(), '合成隔离群', 'Friend dialog did not select the controlled candidate group')
      assert.equal(posts.length, 0, 'Selecting a controlled candidate submitted a write')
      await prepare()
      await send.click(); await confirmation.waitFor()
      await confirmation.getByRole('button', { name: '确认发送', exact: true }).click()
      await idle()
      assert.deepEqual(posts.map(row => row.api), [
        '/test-tools/onboarding-friend/windows',
        `/test-tools/onboarding-friend/challenges/${challenge.id}/friend-target`,
        `/test-tools/onboarding-friend/challenges/${challenge.id}/friend-send`
      ])
      assert.equal(posts[0].body.group_id, id(3))
      assert.equal(posts[0].body.as_claimant, true)
      assert.equal(challenge.robot.id, id(6), 'Controlled candidate changed the receiving robot')
      assert.equal(challenge.scope.actor_id, user.id, 'Controlled candidate changed the original owner')
      assert.ok(await send.isDisabled(), 'Candidate-only applicant could resend the same request')
      checkpoint(`${platform}/claimant-controlled-candidate-empty-props-groups`)

      await configure(platform, { receiver: true })
      assert.equal(await dialog.getByLabel('受控好友申请方向').count(), 0)
      assert.ok(await target.isEnabled(), 'Old receiver mode lost its target input')
      assert.equal(await account.inputValue(), '', 'Applicant prefill leaked into receiver mode')
      await account.fill('synthetic_peer'); await target.fill('8002')
      await prepare()
      await send.click(); await confirmation.waitFor()
      await confirmation.getByRole('button', { name: '确认发送', exact: true }).click()
      await idle()
      assert.equal(posts.length, 3)
      assert.ok(!('as_claimant' in posts[0].body), 'Old receiver mode was changed to applicant mode')
      checkpoint(`${platform}/legacy-receiver`)

      for (const [name, change] of [
        ['missing-scope', c => { delete c.controlled_scope }],
        ['inactive', c => { c.controlled_scope.state = 'preparing' }],
        ['closed', c => { c.controlled_scope.state = 'closed' }],
        ['expired', c => { c.controlled_scope.expires_at = new Date(now() - 1).toISOString() }],
        ['blocked', c => { c.controlled_scope.blocked_reason = 'synthetic_blocked' }],
        ['scope-actor', c => { c.controlled_scope.actor_id = id(10) }],
        ['challenge-actor', c => { c.scope.actor_id = id(10) }],
        ['claimant', c => { c.controlled_scope.claimant.id = c.controlled_scope.claimant_account_id = id(10) }],
        ['group', c => { c.controlled_scope.group.id = c.controlled_scope.group_id = id(13) }],
        ['scope-platform', c => { c.controlled_scope.platform = platform === 'wsl' ? 'lajiaoliao' : 'wsl' }],
        ['challenge-platform', c => { c.platform = platform === 'wsl' ? 'lajiaoliao' : 'wsl' }],
        ['receiver', c => { c.controlled_scope.robot.id = c.controlled_scope.account_id = id(10) }],
        ['receiver-native-id', c => { c.controlled_scope.robot.native_id = 'wrong-recipient' }],
        ['challenge-mode', c => { c.mode = 'customer' }]
      ]) {
        await configure(platform, { change })
        assert.equal(await account.count(), 0, `${name} was recognized as a test applicant`)
        assert.ok(await send.isDisabled())
        assert.equal(posts.length, 0, `${name} caused a write`)
        checkpoint(`${platform}/reject-${name}`)
      }

      await configure(platform, { change: c => { c.robot.add_available = false } })
      assert.equal(await account.inputValue(), '', 'Unavailable robot add account was prefilled')
      assert.equal(await target.inputValue(), '7001')
      await dialog.getByText('确认此群为自有隔离测试群', { exact: true }).click()
      assert.ok(await create.isDisabled())
      await account.fill('synthetic_receiver')
      assert.ok(await create.isEnabled())
      checkpoint(`${platform}/add-account-availability`)

      await configure(platform, { change: c => {
        c.controlled_scope.expires_at = new Date(now() + 6000).toISOString()
      } })
      await prepare(); await send.click(); await confirmation.waitFor()
      elapsed += 3000
      await page.clock.setFixedTime(new Date(now())); await page.clock.fastForward(3000)
      await confirmation.waitFor({ state: 'hidden' }); await idle()
      assert.ok(await send.isDisabled())
      assert.ok(await target.isDisabled(), 'Expired applicant target became editable')
      assert.equal(posts.filter(row => row.api.endsWith('/friend-send')).length, 0)
      checkpoint(`${platform}/scope-expiry-settles-confirmation`)

      await configure(platform); await prepare()
      challenge.controlled_scope.state = 'closed'
      await read.click(); await idle()
      assert.equal(await account.count(), 0, 'A revoked response retained the old active challenge')
      assert.ok(await send.isDisabled())
      assert.equal(posts.filter(row => row.api.endsWith('/friend-send')).length, 0)
      checkpoint(`${platform}/revoked-read-invalidates-draft`)

      await configure(platform); await prepare(); await send.click(); await confirmation.waitFor()
      await page.evaluate(() => { window.__testTools.state.account = window.__testTools.state.accounts[1].id })
      await confirmation.waitFor({ state: 'hidden' }); await idle()
      assert.equal(posts.filter(row => row.api.endsWith('/friend-send')).length, 0)
      assert.equal(await dialog.getByLabel('受控好友申请方向').count(), 0)
      checkpoint(`${platform}/account-switch-cancels`)

      await configure(platform)
      heldRead = {}
      heldRead.promise = new Promise(resolve => { heldRead.release = resolve })
      const pendingRead = page.waitForRequest(req =>
        req.method() === 'GET' && new URL(req.url()).pathname.endsWith(`/challenges/${id(5)}`))
      await read.click(); await pendingRead
      await page.evaluate(() => { window.__testTools.state.account = window.__testTools.state.accounts[1].id })
      heldRead.release(); heldRead = null
      await idle()
      assert.equal(await account.count(), 0, 'Late applicant response leaked across accounts')
      assert.equal(posts.length, 0)
      checkpoint(`${platform}/late-read-isolation`)

      await configure(platform)
      heldRead = {}
      heldRead.promise = new Promise(resolve => { heldRead.release = resolve })
      const pendingRecordRead = page.waitForRequest(req =>
        req.method() === 'GET' && new URL(req.url()).pathname.endsWith(`/challenges/${id(5)}`))
      const recordResponse = page.waitForResponse(response =>
        new URL(response.url()).pathname.endsWith(`/challenges/${id(5)}`))
      await read.click(); await pendingRecordRead
      await newRecord.click()
      challenge = { ...structuredClone(challenge), id: id(25), version: 12 }
      await record.fill(challenge.id)
      heldRead.release(); heldRead = null
      await (await recordResponse).finished(); await idle()
      assert.equal(await record.inputValue(), id(25))
      assert.equal(await account.count(), 0, 'Late original read populated the newly selected record')
      await read.click(); await idle()
      assert.equal(await account.inputValue(), 'synthetic_receiver')
      assert.equal(posts.length, 0)
      checkpoint(`${platform}/new-record-late-read-isolation`)

      await configure(platform)
      assert.ok(!('private_challenge' in challenge) && !('group_challenge' in challenge),
        'Proof regression must not rely on plaintext from GET challenge')
      assert.equal(await proofInput('private').inputValue(), '')
      assert.ok(await proofButton('private').isDisabled())
      for (const text of ['群接入 short', proofTexts.group, `${proofTexts.private} `]) {
        await proofInput('private').fill(text)
        assert.ok(await proofButton('private').isDisabled(), 'Incomplete or changed proof text was admitted')
      }
      await proofInput('private').fill(proofTexts.private)
      await proofInput('group').fill(proofTexts.group)
      assert.ok(await proofButton('private').isEnabled())
      assert.ok(await proofButton('group').isDisabled(), 'Group proof skipped the real private identity stage')
      assert.equal(posts.length, 0, 'Pasting proof text submitted a request')
      proofTTL = 10000
      await proofButton('private').click(); await idle()
      assert.equal(await proofID('private').inputValue(), id(14))
      assert.ok(await proofInput('private').isDisabled())
      assert.ok(await proofButton('private').isDisabled())
      assert.ok((await dialog.getByLabel('私聊证明授权').innerText()).includes(id(2)))
      assert.equal(posts.length, 1, 'Preparing a proof window sent a message')
      await read.click(); await idle()
      assert.equal(posts.length, 1, 'Reading a proof window sent a message or renewed its budget')
      elapsed += 11000
      await page.clock.setFixedTime(new Date(now())); await page.clock.fastForward(11000)
      assert.ok((await dialog.getByLabel('私聊证明授权').innerText()).includes('已过期'))
      assert.ok(await proofButton('private').isDisabled(), 'An expired proof window could be replaced')
      challenge = { ...challenge, identity_proven: true, state: 'proven', version: challenge.version + 1 }
      await read.click(); await idle()
      assert.ok(await proofButton('private').isDisabled())
      assert.ok(await proofButton('group').isEnabled())
      assert.ok((await dialog.getByLabel('私聊证明授权').innerText()).includes('阶段已变化'))
      await proofButton('group').click(); await idle()
      assert.equal(await proofID('group').inputValue(), id(15))
      assert.equal(posts.length, 2, 'Group proof preparation dispatched a native message')
      assert.notEqual(posts[0].key, posts[1].key)
      const source = await dialog.getByLabel('群证明授权').innerText()
      assert.ok(source.includes(id(2)) && source.includes('126') && source.includes('未发送'))
      await dialog.getByLabel('群证明授权')
        .getByRole('button', { name: '进入该账号消息中心', exact: true }).click()
      assert.equal(new URL(page.url()).hash, '#messages')
      assert.equal(await page.evaluate(({ actor, platform }) =>
        sessionStorage.getItem(`wsl:account:${actor}:${platform}`), { actor: user.id, platform }), id(2))
      assert.equal(posts.length, 2, 'Opening the message center sent proof text')
      await page.evaluate(() => { location.hash = '' })
      checkpoint(`${platform}/proof-pasted-text-phase-budget1-no-send-message-entry`)

      await configure(platform)
      await proofInput('private').fill(proofTexts.private)
      await proofButton('private').click(); await idle()
      const recordInput = dialog.getByRole('textbox', { name: '受控好友接入记录', exact: true })
      const oldChallenge = structuredClone(challenge), oldWindows = proofWindows
      const oldRequest = structuredClone(posts[0])
      challenge = { ...structuredClone(challenge), id: id(25), version: 12 }
      proofWindows = new Map()
      await recordInput.fill(challenge.id)
      assert.equal(await proofInput('private').count(), 0,
        'Changing the record retained the previous challenge or tools')
      assert.ok(await send.isDisabled())
      assert.equal(posts.length, 1, 'Changing the record submitted a request')
      await read.click(); await idle()
      assert.equal(await proofInput('private').inputValue(), '')
      assert.equal(await proofID('private').count(), 0)
      await proofInput('private').fill(proofTexts.private)
      await proofButton('private').click(); await idle()
      assert.equal(posts.length, 2)
      assert.notEqual(posts[1].key, oldRequest.key)
      assert.ok(posts[1].api.includes(`/challenges/${id(25)}/`))
      challenge = oldChallenge; proofWindows = oldWindows
      await recordInput.fill(challenge.id)
      await read.click(); await idle()
      assert.equal(await proofInput('private').inputValue(), proofTexts.private)
      assert.equal(await proofID('private').inputValue(), id(14))
      assert.ok(await proofButton('private').isDisabled())
      assert.deepEqual(posts[0], oldRequest)
      assert.equal(posts.length, 2, 'Returning to the old record renewed or replayed its proof')
      checkpoint(`${platform}/switch-challenge-preserves-original-proof-no-replay`)

      for (const kind of ['private', 'group']) {
        await configure(platform, { change: c => {
          if (kind === 'group') { c.identity_proven = true; c.state = 'proven' }
        } })
        await proofInput(kind).fill(proofTexts[kind])
        await proofButton(kind).click(); await idle()
        await dialog.getByLabel(kind === 'private' ? '私聊证明授权' : '群证明授权')
          .getByRole('button', { name: '进入该账号消息中心', exact: true }).click()
        const center = page.getByTestId('message-center')
        await center.waitFor()
        assert.equal(textRequests.length, 0, 'Opening source conversations sent proof text')
        if (kind === 'private') {
          await center.locator('.mc-conversation').filter({ hasText: '无关私聊' }).click()
          await center.getByRole('textbox', { name: '消息内容', exact: true }).fill(proofTexts.private)
          assert.ok(await center.getByRole('button', { name: '发送消息', exact: true }).isDisabled(),
            'An unrelated private conversation inherited the receiver proof grant')
        }
        await center.locator('.mc-conversation')
          .filter({ hasText: kind === 'private' ? '合成接收机器人私聊' : '合成隔离群会话' }).click()
        await center.getByText('已持久化真实源事件', { exact: true }).waitFor()
        const input = center.getByRole('textbox', { name: '消息内容', exact: true })
        const sendText = center.getByRole('button', { name: '发送消息', exact: true })
        await input.fill(proofTexts[kind])
        await sendText.and(page.locator(':enabled')).waitFor()
        await sendText.click(); await confirmation.waitFor()
        await confirmation.getByRole('button', { name: '取消', exact: true }).click()
        assert.equal(textRequests.length, 0)
        await sendText.and(page.locator(':enabled')).waitFor()
        await sendText.click(); await confirmation.waitFor()
        textSendStatus = kind === 'private' ? 'unknown' : 'succeeded'
        await confirmation.getByRole('button', { name: '确认发送', exact: true }).click()
        await center.getByText(kind === 'private'
          ? '提交结果未知，请核对状态或查询回显，勿重复发送'
          : '原生服务端已接受，不代表成员已送达或已读', { exact: true }).waitFor()
        assert.deepEqual(textRequests.map(item => item.phase), ['stage', 'dispatch'])
        assert.ok(textRequests.every(item => item.api.includes(`/accounts/${id(2)}/conversations/${conversationKeys[kind]}/`)))
        assert.ok(await sendText.isDisabled(), 'Spent/unknown proof window allowed another send')
        if (kind === 'private') {
          await center.getByRole('button', { name: '核对状态', exact: true }).click()
          assert.equal(textRequests.length, 2, 'Inspecting an unknown proof replayed its send')
          recoveryOnly = true
          await page.evaluate(() => { location.hash = '' })
          await center.waitFor({ state: 'hidden' })
          await page.evaluate(() => { location.hash = '#messages' })
          await center.waitFor()
          await center.locator('.mc-conversation').filter({ hasText: '合成接收机器人私聊' }).click()
          await center.getByText('提交结果未知，请核对状态或查询回显，勿重复发送', { exact: true }).waitFor()
          await center.getByRole('button', { name: '核对状态', exact: true }).click()
          assert.ok(recoveryReads > 0, 'Expired admission hid an unknown operation after remount')
          assert.equal(textRequests.length, 2, 'Historical recovery submitted or replayed a proof')
          assert.ok(await center.getByRole('button', { name: '发送消息', exact: true }).isDisabled())
          if (platform === 'lajiaoliao') {
            const cases = [
              ['sequences_rejected', '读取平台会话水位时，平台拒绝了只读查询'],
              ['history_empty_gap', '平台未返回本次请求范围内的历史消息'],
              ['history_partial_gap', '平台返回的历史消息数量不足或超出请求范围'],
              ['history_scope_gap', '平台返回的历史会话与原操作不一致'],
              ['history_record_gap', '平台历史含有空记录、无效消息标识或超限内容'],
              ['history_sequence_gap', '平台返回的历史消息序列重复或不连续']
            ]
            for (const [index, [suffix, text]] of cases.entries()) {
              evidenceFailure = `lajiaoliao_evidence_${suffix}`
              await center.getByRole('button', { name: '查询回显', exact: true }).click()
              await center.getByText(`${text}。原发送结果仍待核对，未重发消息`, { exact: true }).waitFor()
              assert.equal(textRequests.length, 2, 'Failed evidence lookup replayed a send')
              assert.equal(posts.filter(item => item.api.endsWith('/reconcile')).length, index + 1,
                'Failed evidence lookup retried itself')
            }
            assert.ok(await center.getByRole('button', { name: '发送消息', exact: true }).isDisabled())
            checkpoint(`${platform}/proof-evidence-safe-stage-failure-no-resend`)
          }
        }
        checkpoint(`${platform}/proof-${kind}-source-conversation-event-existing-stage-dispatch`)
      }

      if (platform === 'lajiaoliao') {
        await configure(platform)
        await proofInput('private').fill(proofTexts.private)
        await proofButton('private').click(); await idle()
        retiredOperation = { id: id(21), kind: 'send_text', status: 'unknown',
          result_code: 'native_text_result_unknown', test_window_id: id(22) }
        await dialog.getByLabel('私聊证明授权')
          .getByRole('button', { name: '进入该账号消息中心', exact: true }).click()
        const center = page.getByTestId('message-center')
        await center.locator('.mc-conversation').filter({ hasText: '合成接收机器人私聊' }).click()
        const input = center.getByRole('textbox', { name: '消息内容', exact: true })
        const sendText = center.getByRole('button', { name: '发送消息', exact: true })
        await input.fill('旧证明不能沿用')
        assert.ok(await sendText.isDisabled(), 'Old unknown was silently ignored')
        await center.getByRole('button', { name: '使用新认领证明', exact: true }).click()
        assert.equal(await input.inputValue(), '', 'Retired proof text was copied into a new challenge')
        assert.equal(posts.length, 1, 'Choosing a new proof submitted a message')
        await center.getByText(/旧发送结果仍未知/).waitFor()
        await center.getByRole('button', { name: '核对原记录', exact: true }).click()
        assert.equal(recoveryReads, 1)
        assert.equal(textRequests.length, 0)
        await center.getByRole('button', { name: '刷新发送条件', exact: true }).click()
        await input.fill(proofTexts.private)
        await sendText.and(page.locator(':enabled')).waitFor()
        await sendText.click(); await confirmation.waitFor()
        await confirmation.getByRole('button', { name: '确认发送', exact: true }).click()
        await center.getByText('原生服务端已接受，不代表成员已送达或已读', { exact: true }).waitFor()
        assert.deepEqual(textRequests.map(item => item.phase), ['stage', 'dispatch'])
        await center.getByText(/旧发送结果仍未知/).waitFor()
        await center.getByRole('button', { name: '核对原记录', exact: true }).click()
        assert.equal(recoveryReads, 2)
        assert.equal(retiredOperation.status, 'unknown')
        assert.equal(textRequests.length, 2, 'Inspecting the retired record replayed a send')
        checkpoint(`${platform}/independent-new-proof-retains-old-unknown-no-replay`)
      }

      await configure(platform)
      await proofInput('private').fill(proofTexts.private)
      abortProof = true
      await proofButton('private').click(); await idle()
      const original = structuredClone(posts[0])
      assert.equal(await proofID('private').count(), 0)
      assert.ok(await proofInput('private').isDisabled(), 'Unknown proof request became editable')
      await page.evaluate(async () => { await window.__testTools.hide(); await window.__testTools.show() })
      await idle()
      await tool.getByRole('button', { name: '受控搜索加好友', exact: true }).click()
      await choose('受控好友隔离群组', '合成隔离群')
      await read.and(page.locator(':enabled')).waitFor()
      await read.click(); await idle()
      assert.equal(await proofInput('private').inputValue(), proofTexts.private)
      assert.equal(posts.length, 1, 'Remount automatically retried a proof request')
      await dialog.getByRole('button', { name: '重试原私聊窗口请求', exact: true }).click()
      await idle()
      assert.equal(await proofID('private').inputValue(), id(14))
      assert.deepEqual(posts[1], original, 'Restored proof retry changed key or payload')
      checkpoint(`${platform}/proof-unknown-remount-same-key`)

      for (const [name, patch] of [
        ['sender', w => { w.account_id = id(6) }],
        ['group', w => { w.group_id = id(13) }],
        ['native-group', w => { w.native_group = 'wrong-group' }],
        ['phase', w => { w.challenge_version++ }],
        ['kind', w => { w.proof_kind = 'group' }],
        ['epoch', w => { w.account_epoch++ }],
        ['admission', w => { w.admission_mode = 'package' }]
      ]) {
        await configure(platform)
        await proofInput('private').fill(proofTexts.private)
        proofResponsePatch = patch
        await proofButton('private').click(); await idle()
        assert.equal(await proofID('private').count(), 0, `${name} response escaped its proof scope`)
        assert.equal(await dialog.getByRole('button', { name: '进入该账号消息中心', exact: true }).count(), 0)
        assert.equal(posts.length, 1)
        checkpoint(`${platform}/proof-reject-response-${name}`)
      }

      await configure(platform)
      await proofInput('private').fill(proofTexts.private)
      heldProof = {}
      heldProof.promise = new Promise(resolve => { heldProof.release = resolve })
      const pendingProof = page.waitForRequest(req => req.url().endsWith('/proof-windows'))
      const proofResponse = page.waitForResponse(response => response.url().endsWith('/proof-windows'))
      await proofButton('private').click(); await pendingProof
      await page.evaluate(() => { window.__testTools.state.account = window.__testTools.state.accounts[1].id })
      heldProof.release(); heldProof = null
      await (await proofResponse).finished(); await idle()
      assert.equal(await proofID('private').count(), 0, 'Late proof response leaked across accounts')
      assert.equal(posts.length, 1)
      checkpoint(`${platform}/proof-late-response-isolation`)

      await configure(platform)
      await proofInput('private').fill(proofTexts.private)
      heldProof = {}
      heldProof.promise = new Promise(resolve => { heldProof.release = resolve })
      const pendingOldProof = page.waitForRequest(req => req.url().endsWith('/proof-windows'))
      const oldProofResponse = page.waitForResponse(response => response.url().endsWith('/proof-windows'))
      await proofButton('private').click(); await pendingOldProof
      const originalProofRequest = structuredClone(posts[0])
      await newRecord.click()
      challenge = { ...structuredClone(challenge), id: id(25), version: 12 }
      await record.fill(challenge.id)
      heldProof.release(); heldProof = null
      await (await oldProofResponse).finished(); await idle()
      assert.equal(await proofID('private').count(), 0, 'Late old proof polluted the new record')
      await read.click(); await idle()
      assert.equal(await proofInput('private').inputValue(), '')
      assert.equal(await proofID('private').count(), 0)
      const retained = await page.evaluate(key =>
        JSON.parse(sessionStorage.getItem(key)), `${friendStorageKey(platform)}:${id(5)}`)
      assert.equal(retained.proofs[`${id(5)}:private`].key, originalProofRequest.key)
      assert.equal(retained.proofs[`${id(5)}:private`].window, null)
      assert.equal(posts.length, 1, 'New selection retried the original pending proof request')
      checkpoint(`${platform}/new-record-late-proof-isolation-preserves-original-key`)

      await page.setViewportSize({ width: 390, height: 844 })
      await configure(platform)
      await proofInput('private').fill(proofTexts.private)
      const bounds = await dialog.boundingBox()
      const footer = await send.boundingBox()
      assert.ok(bounds.x >= 0 && bounds.x + bounds.width <= 391 && bounds.y >= 0)
      assert.ok(footer.y >= bounds.y && footer.y + footer.height <= 845)
      checkpoint(`${platform}/mobile-dialog-actions`)
      await page.setViewportSize({ width: 1280, height: 720 })
    }
    assert.deepEqual({ errors, unexpected }, { errors: [], unexpected: [] })
    console.log(JSON.stringify({ check: path.basename(__filename), passed: true,
      synthetic_only: true, native_requests: 0, external_requests: 0, checks }))
  } catch (error) {
    console.error(JSON.stringify({ completed: checks, errors, unexpected,
      ui: await page.locator('body').innerText() }))
    throw error
  } finally { await browser.close() }
}

main().catch(error => { console.error(error.stack || error); process.exitCode = 1 })
