// Real TestTools and shared gate editor; synthetic HTTP only, no native calls.
// WSL_UI_NODE_MODULES=/absolute/web/node_modules WSL_PLAYWRIGHT_MODULE=/absolute/playwright/index.mjs \
//   node tools/onboarding_outbound_gate_ui_smoke.js
const assert = require('node:assert/strict')
const path = require('node:path')
const { createTestToolsBrowser } = require('./test_tools_confirmation_ui_smoke')

async function main() {
  const { browser, page, assets } = await createTestToolsBrowser({ gates: true })
  const errors = [], unexpected = [], reads = [], writes = [], checks = []
  const id = n => `f9300000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const user = { id: id(1), username: 'synthetic-outbound-gate', role: 'superadmin' }
  const records = new Map()
  let fixture, held
  const key = (platform, account, kind) => `${platform}:${account}:${kind}`
  const record = (kind = 'outbound', account = fixture.account) =>
    records.get(key(fixture.workspace, account, kind))
  page.on('pageerror', error => errors.push(error.message))
  // Deliver held responses even after cancellation to exercise the scope fence.
  await page.addInitScript(() => {
    const fetch = window.fetch
    window.fetch = (input, options) => fetch(input,
      String(input).includes('/system/group-binding/')
        ? { ...options, signal: undefined } : options)
  })
  await page.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url())
    const api = url.pathname.replace('/api/v1', '')
    const fulfill = data => route.fulfill({ json: { code: 0, data, msg: '' } })
    if (url.origin !== 'http://127.0.0.1:5173') {
      unexpected.push(request.url()); return route.abort('blockedbyclient')
    }
    if (url.pathname === '/') return route.fulfill({ contentType: 'text/html',
      body: '<!doctype html><meta charset="UTF-8"><link rel="stylesheet" href="/fixture.css"><div id="app"></div><script src="/fixture.js"></script>' })
    if (url.pathname === '/fixture.js') return route.fulfill({
      contentType: 'text/javascript', body: assets.get('.js') })
    if (url.pathname === '/fixture.css') return route.fulfill({
      contentType: 'text/css', body: assets.get('.css') })
    if (/^\/(?:wsl|lajiaoliao)-logo\.png$/.test(url.pathname))
      return route.fulfill({ contentType: 'image/png',
        body: Buffer.from('iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+j4YQAAAAASUVORK5CYII=', 'base64') })
    const match = /^\/system\/group-binding\/(outbound-friend-gates|friend-gates)\/([^/]+)$/.exec(api)
    if (match) {
      const kind = match[1] === 'friend-gates' ? 'incoming' : 'outbound'
      const platform = request.headers()['x-workspace'], account = match[2]
      assert.equal(platform, fixture.workspace)
      assert.ok(fixture.accounts.some(item => item.id === account &&
        item.platform === platform && item.purpose === 'management'))
      const previous = records.get(key(platform, account, kind))
      assert.ok(previous)
      let response = structuredClone(previous)
      if (request.method() === 'GET') reads.push({ kind, platform, account })
      else {
        assert.equal(request.method(), 'PUT')
        const body = request.postDataJSON()
        assert.deepEqual(Object.keys(body).sort(), ['evidence_refs', 'state', 'version'])
        assert.ok(['open', 'closed'].includes(body.state))
        assert.ok(body.state !== 'open' || body.evidence_refs.length > 0)
        writes.push({ kind, platform, account, body })
        if (body.version !== previous.version)
          return route.fulfill({ status: 409, json: { code: 1, msg: 'version_conflict' } })
        response = { ...previous, ...body, version: previous.version + 1 }
        records.set(key(platform, account, kind), response)
      }
      const pending = held
      if (pending) {
        held = null
        pending.started()
        await pending.wait
        response = pending.response || response
      }
      await fulfill(response)
      pending?.finished()
      return
    }
    if (request.method() === 'GET') {
      if (api === '/test-tools/onboarding/scopes')
        return fulfill({ items: [], candidates: { accounts: [], groups: [] } })
      if (/^\/accounts\/[^/]+\/receiver$/.test(api))
        return fulfill({ enabled: true, revision: 1, observed: true, snapshot: { state: 'connected' } })
      if (api === '/test-tools/operations') return fulfill([])
    }
    unexpected.push(`${request.method()} ${api}`)
    return route.fulfill({ status: 500, json: { code: 1, msg: 'unmocked_api' } })
  })
  const tool = page.locator('.test-tools')
  const parent = page.getByRole('dialog', { name: '受控搜索加好友', exact: true })
  const modal = (kind = 'outbound') => page.getByRole('dialog', {
    name: kind === 'outbound' ? '发送好友申请准入' : '来向好友审批准入', exact: true
  })
  const evidence = (kind = 'outbound') => modal(kind).getByRole('textbox', {
    name: kind === 'outbound' ? '发送好友申请证据引用' : '来向好友审批证据引用', exact: true
  })
  const save = (kind = 'outbound') => modal(kind).getByRole('button', { name: '保存准入', exact: true })
  const saved = (kind = 'outbound') => modal(kind).getByRole('region', { name: '已保存准入', exact: true })
  const done = name => checks.push(`${fixture.workspace}/${name}`)
  const idle = () => tool.getByRole('button', { name: '刷新检查', exact: true })
    .and(page.locator(':enabled')).waitFor()
  const open = async () => {
    if (!await parent.isVisible()) {
      await idle()
      await tool.getByRole('button', { name: '受控搜索加好友', exact: true }).click()
    }
    await parent.getByRole('button', { name: '发送准入', exact: true }).click()
    await saved().getByText(`版本 ${record().version}`, { exact: true }).waitFor()
  }
  const edit = async (kind = 'outbound') => {
    await modal(kind).getByRole('button', { name: '编辑准入', exact: true }).click()
    await evidence(kind).waitFor()
  }
  const state = async (value, kind = 'outbound') => {
    const label = kind === 'outbound' ? '发送好友申请准入状态' : '来向好友审批准入状态'
    await modal(kind).getByRole('combobox', { name: label, exact: true })
      .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option', { name: value === 'open' ? '开放' : '关闭', exact: true }).click()
  }
  const close = async (kind = 'outbound') => {
    const before = [reads.length, writes.length]
    await modal(kind).getByRole('button', { name: '关闭', exact: true }).click()
    await modal(kind).waitFor({ state: 'hidden' })
    assert.deepEqual([reads.length, writes.length], before)
  }
  const saveDone = async (kind = 'outbound') => {
    await save(kind).click()
    await modal(kind).getByRole('status').getByText(
      `${kind === 'outbound' ? '发送好友申请' : '来向好友审批'}准入已保存`, { exact: true }).waitFor()
  }
  const configure = async platform => {
    fixture = { workspace: platform, account: id(2),
      accounts: [2, 7].map(n => ({ id: id(n), name: `合成管理账号${n}`, platform,
        purpose: 'management', testing_enabled: true, connection_enabled: true, epoch: 1, version: 1 })),
      groups: [], windows: [] }
    for (const account of fixture.accounts)
      for (const kind of ['incoming', 'outbound'])
        records.set(key(platform, account.id, kind), { platform, account_id: account.id,
          state: 'closed', version: 0, evidence_refs: [] })
    await page.evaluate(({ fixture, user }) => window.__testTools.configure(fixture, user), { fixture, user })
    await idle()
  }
  try {
    await page.goto('http://127.0.0.1:5173')
    for (const platform of ['wsl', 'lajiaoliao']) {
      await configure(platform)
      const before = [reads.length, writes.length]
      await tool.getByRole('button', { name: '受控搜索加好友', exact: true }).click()
      assert.deepEqual([reads.length, writes.length], before); done('no-eager-gate-read-or-write')
      await open()
      assert.equal(reads.length, before[0] + 1)
      assert.equal(writes.length, before[1])
      assert.equal(await evidence().count(), 0)
      await saved().getByText('关闭', { exact: true }).waitFor()
      await close(); done('view-and-close-read-only')
      await open(); await edit(); await state('open')
      assert.equal(await save().isDisabled(), true)
      await evidence().fill('https://invalid.example/evidence')
      assert.equal(await save().isDisabled(), true)
      await evidence().fill(`acceptance:OUTBOUND_${platform}`)
      await close()
      assert.equal(writes.length, before[1]); done('draft-close-does-not-open')
      await open(); await edit()
      assert.equal(await evidence().inputValue(), `acceptance:OUTBOUND_${platform}`)
      done('reopen-retains-scoped-draft')
      await saveDone()
      assert.deepEqual(writes.at(-1), { kind: 'outbound', platform, account: id(2), body: {
        state: 'open', version: 0, evidence_refs: [`acceptance:OUTBOUND_${platform}`] } })
      done('explicit-open-exact-path-version-body')
      const afterOpen = writes.length
      await modal().getByRole('button', { name: '重新读取发送好友申请准入', exact: true }).click()
      await saved().getByText('版本 1', { exact: true }).waitFor()
      assert.equal(writes.length, afterOpen); done('refresh-does-not-submit')
      await edit(); await state('closed'); await evidence().fill(''); await saveDone()
      assert.deepEqual(writes.at(-1).body, { state: 'closed', version: 1, evidence_refs: [] })
      done('explicit-close-with-version')
      await edit(); await state('open'); await evidence().fill('acceptance:CONFLICT_DRAFT')
      record().version++
      await save().click()
      await modal().getByText('准入记录已变化，草稿已保留。请重新读取并核对当前版本。', { exact: true }).waitFor()
      assert.equal(await save().isDisabled(), true)
      assert.equal(await evidence().inputValue(), 'acceptance:CONFLICT_DRAFT')
      done('conflict-blocks-stale-save-retains-draft')
      const attempts = writes.length
      await modal().getByRole('button', { name: '重新读取发送好友申请准入', exact: true }).click()
      const adopt = modal().getByRole('button', { name: '使用当前版本保留草稿', exact: true })
      await adopt.and(page.locator(':enabled')).waitFor()
      assert.equal(await save().isDisabled(), true)
      assert.equal(writes.length, attempts)
      await adopt.click(); await saveDone()
      assert.equal(writes.at(-1).body.version, 3); done('conflict-requires-read-adopt-explicit-save')
      await edit(); await evidence().fill('acceptance:OUTBOUND_ONLY'); await close()
      await parent.getByRole('button', { name: '关闭', exact: true }).click()
      await page.evaluate(() => window.__testTools.gate('incoming'))
      await saved('incoming').getByText('版本 0', { exact: true }).waitFor()
      assert.equal(reads.at(-1).kind, 'incoming')
      await edit('incoming')
      assert.equal(await evidence('incoming').inputValue(), '')
      await state('open', 'incoming'); await evidence('incoming').fill('acceptance:INCOMING_ONLY')
      await saveDone('incoming')
      assert.deepEqual(writes.at(-1), { kind: 'incoming', platform, account: id(2), body: {
        state: 'open', version: 0, evidence_refs: ['acceptance:INCOMING_ONLY'] } })
      await close('incoming'); done('default-incoming-path-labels-and-draft-isolation')
      await open(); await edit()
      assert.equal(await evidence().inputValue(), 'acceptance:OUTBOUND_ONLY')
      await close(); done('incoming-save-preserves-outbound-draft')
      let release, started, finished
      const pending = { wait: new Promise(resolve => { release = resolve }),
        ready: new Promise(resolve => { started = resolve }),
        done: new Promise(resolve => { finished = resolve }),
        started: () => started(), finished: () => finished(),
        response: { ...record(), version: 999, evidence_refs: ['acceptance:LATE'] } }
      held = pending
      await parent.getByRole('button', { name: '发送准入', exact: true }).click()
      await pending.ready
      await page.evaluate(id => { window.__testTools.state.account = id }, id(7))
      await modal().waitFor({ state: 'hidden' })
      fixture.account = id(7)
      await open(); await edit(); await evidence().fill('acceptance:OTHER_ACCOUNT')
      release(); await pending.done
      await page.evaluate(() => new Promise(requestAnimationFrame))
      assert.equal(await evidence().inputValue(), 'acceptance:OTHER_ACCOUNT')
      assert.equal(await modal().getByText('acceptance:LATE', { exact: true }).count(), 0)
      done('account-switch-closes-and-isolates-late-response')
      await page.setViewportSize({ width: 390, height: 844 })
      await modal().evaluate(async element => {
        await Promise.all(element.getAnimations({ subtree: true })
          .filter(animation => animation.effect?.getComputedTiming().iterations !== Infinity)
          .map(animation => animation.finished.catch(() => {})))
      })
      // The product global dialog rules are covered by its built-app smoke;
      // this isolated harness still verifies the footer actions remain present.
      assert.equal(await save().isVisible(), true)
      await close(); await page.setViewportSize({ width: 1280, height: 900 })
      done('mobile-actions-and-close')
      const actorWrites = writes.length
      await open()
      await page.evaluate(user => window.__testTools.actor({ ...user, id: 'other-actor', role: 'user' }), user)
      await modal().waitFor({ state: 'hidden' })
      assert.equal(writes.length, actorWrites); done('actor-revocation-closes-without-write')
    }
    assert.deepEqual({ errors, unexpected }, { errors: [], unexpected: [] })
    console.log(JSON.stringify({ check: path.basename(__filename), passed: true,
      synthetic_only: true, native_requests: 0, external_requests: 0, cases: checks.length, checks }))
  } catch (error) {
    console.error(JSON.stringify({ completed: checks, errors, unexpected, ui: await page.locator('body').innerText() }))
    throw error
  } finally { await browser.close() }
}

if (require.main === module)
  main().catch(error => { console.error(error.stack || error); process.exitCode = 1 })
