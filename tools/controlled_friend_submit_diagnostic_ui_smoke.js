// Real TestTools/Element Plus via the unchanged synthetic confirmation harness.
// WSL_UI_NODE_MODULES=/absolute/web/node_modules WSL_PLAYWRIGHT_MODULE=/absolute/playwright/index.mjs \
//   node tools/controlled_friend_submit_diagnostic_ui_smoke.js
const assert = require('node:assert/strict')
const path = require('node:path')
const { createTestToolsBrowser } = require('./test_tools_confirmation_ui_smoke')

async function main() {
  const { browser, page, assets } = await createTestToolsBrowser()
  const errors = [], unexpected = [], mutations = [], reconciles = [], reads = [], checks = []
  page.on('pageerror', error => errors.push(error.message))
  const origin = 'http://127.0.0.1:5173'
  const id = n => `f9400000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const user = { id: id(1), username: 'synthetic-submit-diagnostic', role: 'superadmin' }
  const observedAt = '2026-11-14T00:30:12.345Z'
  const historyNotice = '此历史记录未保留具体上游返回码，无法还原；不会自动重发'
  const privateBody = 'SYNTHETIC_SUBMIT_PRIVATE_BODY'
  const privateToken = 'SYNTHETIC_SUBMIT_PRIVATE_TOKEN'
  const attack = `<img src="data:invalid" onerror="window.__diagnosticExecuted=true">${privateBody}`
  const start = Date.now()
  let elapsed = 0, fixture, challenge, allowReconcile = false
  const now = () => start + elapsed
  const validDiagnostic = platform => ({
    version: 1, platform, stage: 'friend_apply', category: 'upstream_rejected',
    provider_code: 1001, observed_at: observedAt
  })
  await page.clock.install({ time: new Date(start) })
  await page.clock.setFixedTime(new Date(start))
  await page.route('**/*', async route => {
    const req = route.request(), url = new URL(req.url())
    const fulfill = data => route.fulfill({ json: { code: 0, data, msg: '' } })
    if (url.origin !== origin) {
      unexpected.push(req.url())
      return route.abort('blockedbyclient')
    }
    if (url.pathname === '/') return route.fulfill({ contentType: 'text/html',
      body: '<!doctype html><meta charset="UTF-8"><link rel="stylesheet" href="/fixture.css"><div id="app"></div><script src="/fixture.js"></script>' })
    if (url.pathname === '/fixture.js') return route.fulfill({
      contentType: 'text/javascript', body: assets.get('.js') })
    if (url.pathname === '/fixture.css') return route.fulfill({
      contentType: 'text/css', body: assets.get('.css') })
    const api = url.pathname.replace('/api/v1', '')
    if (req.headers()['x-workspace'] !== fixture?.workspace) {
      unexpected.push(`${req.method()} ${api}: workspace mismatch`)
      return route.abort('blockedbyclient')
    }
    if (req.method() !== 'GET') {
      if (allowReconcile && req.method() === 'POST' &&
        api === `/group-binding/challenges/${challenge.id}/outbound-attempts/${id(6)}/reconcile`) {
        allowReconcile = false
        reconciles.push({ api, body: req.postDataJSON(), key: req.headers()['idempotency-key'] })
        return fulfill(challenge)
      }
      mutations.push(`${req.method()} ${api}`)
      return route.abort('blockedbyclient')
    }
    reads.push(api)
    if (api === '/test-tools/onboarding/scopes')
      return fulfill({ items: [], candidates: { accounts: [], groups: [] } })
    if (api === `/accounts/${id(2)}/receiver`)
      return fulfill({ enabled: true, revision: 1, observed: true, snapshot: { state: 'connected' } })
    if (api === '/test-tools/operations') return fulfill([])
    if (api === `/group-binding/challenges/${id(5)}`) return fulfill(challenge)
    unexpected.push(`${req.method()} ${api}`)
    return route.abort('blockedbyclient')
  })
  const tool = page.locator('.test-tools')
  const dialog = page.getByRole('dialog', { name: '受控搜索加好友', exact: true })
  const operation = dialog.locator('[aria-label="受控好友原操作"]')
  const diagnosis = operation.locator('[aria-label="受控好友提交诊断"]')
  const read = dialog.getByRole('button', { name: '读取原记录', exact: true })
  const send = dialog.getByRole('button', { name: '确认发送好友申请', exact: true })
  const idle = () => tool.getByRole('button', { name: '刷新检查', exact: true })
    .and(page.locator(':enabled')).waitFor()
  const reread = async () => {
    const response = page.waitForResponse(response =>
      new URL(response.url()).pathname === `/api/v1/group-binding/challenges/${id(5)}` &&
      response.request().method() === 'GET')
    await read.click()
    await response
    await read.and(page.locator(':enabled')).waitFor()
  }
  const configure = async platform => {
    fixture = {
      workspace: platform, account: id(2),
      accounts: [{ id: id(2), name: '合成诊断账号', platform, purpose: 'management',
        testing_enabled: true, connection_enabled: true, epoch: 1, version: 1 }],
      groups: [{ group_id: id(3), native_id: '123', name: '合成隔离群', platform }],
      windows: [{ id: id(4), group_id: id(3), account_ids: [id(2)],
        target_ids: [`onboarding_challenge:${id(5)}:11`, 'onboarding_account:synthetic_target',
          'onboarding_target:8002'],
        admission_mode: 'native_debug', actions: ['onboarding_friend_search', 'onboarding_friend_request'],
        budget: 1, used: 1, expires_at: new Date(now() + 300000).toISOString(), cancelled: false }]
    }
    challenge = {
      id: id(5), version: 11, platform, scope: { actor_id: user.id }, robot: { id: id(2) },
      customer: { name: '合成接入记录' }, mode: 'operator_demo', friend_flow: 'incoming_contact',
      state: 'waiting', identity_proven: false, expires_at: fixture.windows[0].expires_at,
      friend_outbound: { id: id(6), status: 'failed', code: 'native_business_rejected',
        can_reconcile: false, target: { native_id: '8002', add_account: 'synthetic_target',
          name: '合成目标' }, submit_diagnostic: validDiagnostic(platform) }
    }
    await page.evaluate(({ fixture, user }) => window.__testTools.configure(fixture, user),
      { fixture, user })
    await idle()
    await tool.locator('.tools-grant').click()
    await reread()
    await operation.waitFor()
  }
  const display = async (value, status = 'failed') => {
    challenge.friend_outbound.submit_diagnostic = value
    challenge.friend_outbound.status = status
    challenge.friend_outbound.can_reconcile = status === 'unknown'
    await reread()
  }
  const noPrivateDOM = async () => {
    const html = await page.locator('body').evaluate(element => element.outerHTML)
    assert.ok(!html.includes(privateBody) && !html.includes(privateToken),
      'Private diagnostic fields entered the DOM')
    assert.equal(await page.evaluate(() => window.__diagnosticExecuted === true), false)
    assert.equal(await operation.locator('[onerror]').count(), 0)
  }
  const shown = async (value, status = 'failed') => {
    await display(value, status)
    assert.equal(await diagnosis.count(), 1)
    assert.match(await diagnosis.innerText(), /失败阶段：好友申请/)
    assert.match(await diagnosis.innerText(), new RegExp(`上游返回码：${value.provider_code}(?:\\s|$)`))
    assert.match(await diagnosis.innerText(), /此诊断仅供排查，不代表授权或提交结论。/)
    const time = diagnosis.locator('time')
    assert.equal(await time.getAttribute('datetime'), value.observed_at)
    assert.equal(await time.innerText(), await page.evaluate(value =>
      new Date(value).toLocaleString('zh-CN', { hour12: false }), value.observed_at))
    assert.ok(!(await operation.innerText()).includes(historyNotice))
    await noPrivateDOM()
  }
  const hidden = async value => {
    await display(value)
    assert.equal(await diagnosis.count(), 0)
    const text = await operation.innerText()
    assert.ok(text.includes(historyNotice))
    assert.match(text, /本次尝试失败/)
    assert.doesNotMatch(text, /请核对平台返回码|上游返回码：|采样时间：/)
    assert.equal(await send.isDisabled(), true)
    await noPrivateDOM()
  }
  const fit = async label => {
    const dimensions = await dialog.evaluate(element => {
      const body = element.querySelector('.el-dialog__body')
      const footer = element.querySelector('.el-dialog__footer').getBoundingClientRect()
      const rect = element.getBoundingClientRect()
      return { width: innerWidth, height: innerHeight, left: rect.left, right: rect.right,
        footerBottom: footer.bottom, footerTop: footer.top,
        horizontal: element.scrollWidth > element.clientWidth + 1 ||
          body.scrollWidth > body.clientWidth + 1 }
    })
    assert.ok(dimensions.left >= 0 && dimensions.right <= dimensions.width + 1 &&
      dimensions.footerBottom <= dimensions.height + 1 && dimensions.footerTop >= 0 &&
      !dimensions.horizontal, `${label}: ${JSON.stringify(dimensions)}`)
    assert.equal(await dialog.getByRole('button', { name: '关闭', exact: true }).isVisible(), true)
    assert.equal(await send.isVisible(), true)
  }

  try {
    await page.goto(origin)
    for (const platform of ['wsl', 'lajiaoliao']) {
      await configure(platform)
      const valid = validDiagnostic(platform)
      for (const category of ['upstream_rejected', 'rate_limited', 'session_expired',
        'transport_unavailable', 'response_invalid'])
        await shown({ ...valid, category })
      for (const provider_code of [1, 9999]) await shown({ ...valid, provider_code })
      for (const observed_at of ['2024-02-29T23:59:59Z', '2026-11-14T08:30:12.345+08:00'])
        await shown({ ...valid, observed_at })
      checks.push(`${platform}: fixed stage, code, ISO sample and diagnostic-only notice`)

      await page.evaluate(() => { window.__testTools.state.windows[0].used = 0 })
      await shown(valid)
      assert.equal(await send.isDisabled(), true, 'Diagnosis authorized a failed operation')
      for (const [status, label] of [
        ['prepared', '目标已确认'], ['submitting', '提交中'],
        ['submitted', '申请已提交'], ['friends', '已观察到好友关系']
      ]) {
        await display(valid, status)
        assert.equal(await diagnosis.count(), 0, `${status} displayed a failure diagnosis`)
        const text = await operation.innerText()
        assert.ok(text.includes(label))
        assert.ok(!text.includes(historyNotice))
        assert.doesNotMatch(text, /失败阶段：|上游返回码：|采样时间：/)
        assert.equal(await send.isEnabled(), status === 'prepared',
          'Diagnosis changed existing admission')
        assert.equal(await page.locator('.el-message-box:visible').count(), 0)
        assert.deepEqual(mutations, [], 'Viewing a non-failure automatically submitted')
        await noPrivateDOM()
      }
      await page.evaluate(() => { window.__testTools.state.windows[0].used = 1 })
      checks.push(`${platform}: non-failure states hide diagnosis and retain original admission`)

      const invalid = [
        undefined, null, false, 1, attack, [], {}, { ...valid, version: 2 },
        { ...valid, version: '1' }, { ...valid, stage: 'search' },
        { ...valid, stage: attack }, { ...valid, category: 'parameters_rejected' },
        { ...valid, category: 'constructor' }, { ...valid, category: attack },
        { ...valid, platform: platform === 'wsl' ? 'lajiaoliao' : 'wsl' },
        { ...valid, platform: 'maibot' }, { ...valid, platform: '' },
        { ...valid, body: attack }, { ...valid, token: privateToken },
        { ...valid, body: { token: privateToken, message: attack } },
        ...[-1, 0, 10000, 1.5, '1001', null, true, {}, attack]
          .map(provider_code => ({ ...valid, provider_code })),
        ...['', attack, '2026-11-14', '2026-11-14T08:30:12', '2026-02-30T00:00:00Z',
          '2025-02-29T00:00:00Z', '2026-13-01T00:00:00Z', '2026-11-14T24:01:00Z']
          .map(observed_at => ({ ...valid, observed_at })),
        ...Object.keys(valid).map(field => {
          const value = { ...valid }
          delete value[field]
          return value
        })
      ]
      for (const value of invalid) await hidden(value)
      checks.push(`${platform}: ${invalid.length} invalid or historical diagnostics hidden; no private DOM`)

      for (const [code, message] of [
        ['native_test_window_invalid', '授权窗口无效、已过期、已撤销、次数已用完，或账号/套餐已变化'],
        ['permission_denied', '当前用户没有此操作权限，或套餐未包含该功能'],
        ['version_conflict', '配置已变化，请刷新后重新编辑'],
        ['native_friend_request_rejected', '原生已拒绝本次申请'],
        ['', '']
      ]) {
        challenge.friend_outbound.code = code
        for (const value of [undefined, { ...valid, provider_code: 0 }]) {
          await display(value)
          assert.equal(await diagnosis.count(), 0)
          const text = await operation.innerText()
          assert.ok(text.includes(message), `Local failure explanation hidden: ${code}`)
          assert.match(text, /本次尝试失败/)
          assert.ok(!text.includes(historyNotice), `Local failure mislabeled historical: ${code}`)
          assert.doesNotMatch(text, /失败阶段：|上游返回码：|采样时间：/)
          assert.equal(await send.isDisabled(), true)
        }
      }
      checks.push(`${platform}: local authorization/version failures remain visible without historical notice`)

      for (const [code, message] of [
        ['native_business_rejected', '旺商聊未接受此操作，请核对平台返回码'],
        ['native_http_rejected', '旺商聊拒绝了此请求'],
        ['native_rate_limited', '旺商聊限制了请求频率，请等待本地冷却后再核对；不会自动重试'],
        ['native_reauth_required', '原生会话已失效，请取消后重新登录'],
        ['native_transport_unknown', '请求结果未知，请先核对官方客户端，不会自动重试'],
        ['native_response_invalid', '平台响应无法核验，结果未知，不会自动重试']
      ]) {
        challenge.friend_outbound.code = code
        await hidden(undefined)
        assert.equal((await operation.innerText()).includes(message), code !== 'native_business_rejected',
          `Incorrect historical explanation visibility: ${code}`)
        await shown(valid)
        assert.ok((await operation.innerText()).includes(message),
          `Valid diagnosis hid the original explanation: ${code}`)
      }
      challenge.friend_outbound.code = 'native_business_rejected'
      checks.push(`${platform}: historical notice limited to six upstream errors; useful explanations retained`)

      await shown(valid, 'unknown')
      assert.match(await operation.innerText(), /结果未知/)
      const reconcile = operation.getByRole('button', { name: '只读核对原结果', exact: true })
      assert.equal(await reconcile.isVisible(), true)
      assert.equal(await send.isDisabled(), true)
      const before = reconciles.length
      allowReconcile = true
      await reconcile.click()
      await idle()
      assert.equal(reconciles.length, before + 1)
      assert.deepEqual(reconciles.at(-1).body, { version: 11 })
      assert.ok(reconciles.at(-1).key)
      assert.match(await operation.innerText(), /结果未知/)
      await display(undefined, 'unknown')
      assert.equal(await diagnosis.count(), 0)
      assert.ok(!(await operation.innerText()).includes(historyNotice))
      assert.equal(await reconcile.isVisible(), true)
      checks.push(`${platform}: unknown preserved; only explicit original read-only reconciliation`)

      for (const width of [320, 390, 768, 1440]) {
        await page.setViewportSize({ width, height: 740 })
        await shown(valid)
        await diagnosis.scrollIntoViewIfNeeded()
        await fit(`${platform}/${width}/diagnosis`)
        await hidden(undefined)
        await operation.getByText(historyNotice, { exact: true }).scrollIntoViewIfNeeded()
        await fit(`${platform}/${width}/history`)
      }
      checks.push(`${platform}: diagnostic and historical mobile/desktop layouts fit`)

      await shown(valid)
      const savedWindows = await page.evaluate(() => JSON.stringify(window.__testTools.state.windows))
      assert.match(await dialog.locator('[aria-label="受控好友授权"]').innerText(), /1\s*\/\s*1 次/)
      const readsBefore = reads.length
      elapsed += 301000
      await page.clock.setFixedTime(new Date(now()))
      await page.clock.fastForward(301000)
      assert.equal(reads.length, readsBefore, 'Timer refreshed or executed native state')
      assert.equal(await send.isDisabled(), true)
      assert.equal(await page.evaluate(() => JSON.stringify(window.__testTools.state.windows)), savedWindows)
      await dialog.getByRole('button', { name: '关闭', exact: true }).click()
      await dialog.waitFor({ state: 'hidden' })
      await tool.getByRole('button', { name: '受控搜索加好友', exact: true }).click()
      await diagnosis.waitFor()
      assert.equal(reads.length, readsBefore, 'Reopening automatically queried or submitted the original operation')
      assert.equal(await send.isDisabled(), true)
      assert.equal(await dialog.getByRole('button', { name: /重试|重发/ }).count(), 0)
      assert.equal(await page.locator('.el-message-box:visible').count(), 0)
      fixture.workspace = platform === 'wsl' ? 'lajiaoliao' : 'wsl'
      await page.evaluate(() => {
        window.__testTools.state.workspace =
          window.__testTools.state.workspace === 'wsl' ? 'lajiaoliao' : 'wsl'
      })
      await idle()
      assert.equal(await page.locator('[aria-label="受控好友提交诊断"]').count(), 0)
      checks.push(`${platform}: budget and expiry preserved; no automatic execution or stale platform diagnosis`)
    }
    assert.deepEqual(mutations, [], 'Viewing diagnostics attempted a mutation')
    assert.deepEqual(unexpected, [], 'All browser requests must be intercepted synthetic fixtures')
    assert.deepEqual(errors, [])
    console.log(JSON.stringify({ check: path.basename(__filename), passed: true,
      synthetic_only: true, native_requests: 0, external_requests: 0,
      automatic_mutations: mutations.length, explicit_read_only_reconciles: reconciles.length, checks }))
  } finally {
    await browser.close()
  }
}

main().catch(error => { console.error(error.stack || error); process.exitCode = 1 })
