// Real TestTools/Element Plus lifecycle, synthetic APIs only; no server or native connection.
// WSL_UI_NODE_MODULES=/absolute/web/node_modules WSL_PLAYWRIGHT_MODULE=/absolute/playwright/index.mjs \
//   node tools/test_tools_confirmation_ui_smoke.js
const fs = require('node:fs')
const path = require('node:path')
const { createRequire } = require('node:module')
const { pathToFileURL } = require('node:url')

async function createTestToolsBrowser({ messages = false, gates = false } = {}) {
  const root = path.resolve(__dirname, '..'), product = path.join(root, 'web/src/wsl')
  const deps = process.env.WSL_UI_NODE_MODULES || path.join(root, 'web/node_modules')
  if (!path.isAbsolute(deps)) throw Error('WSL_UI_NODE_MODULES must be absolute')
  const dependency = createRequire(path.join(deps, 'package.json'))
  const compiler = dependency('@vue/compiler-sfc')
  const esbuild = createRequire(dependency.resolve('vite'))('esbuild')
  const playwright = await import(process.env.WSL_PLAYWRIGHT_MODULE ||
    pathToFileURL(dependency.resolve('playwright')).href)
  const entry = `
    import { createApp, h, reactive, ref, nextTick } from 'vue'
    import { createPinia } from 'pinia'
    import TestTools from './TestTools.vue'
    ${gates ? "import OnboardingFriendGate from './OnboardingFriendGate.vue'" : ''}
    ${messages ? "import MessageCenter from './MessageCenter.vue'; import ElementPlus from 'element-plus'" : ''}
    import { useSession } from './store'
    import 'element-plus/dist/index.css'
    const app = createApp({ setup() {
      const session = useSession(), visible = ref(false), messageView = ref(false), gateMode = ref(null)
      ${messages ? "window.addEventListener('hashchange', () => { messageView.value = location.hash === '#messages' })" : ''}
      const state = reactive({ workspace: 'wsl', account: '', accounts: [], groups: [], windows: [] })
      window.__testTools = {
        state,
        async configure(value, user) {
          visible.value = false; await nextTick()
          gateMode.value = null
          messageView.value = false; location.hash = ''
          sessionStorage.clear(); session.setUser(user)
          session.panels = [{ id: value.workspace, name: value.workspace }]
          session.selectWorkspace(value.workspace); session.selectAccount(value.account)
          Object.assign(state, value); visible.value = true; await nextTick()
        },
        async actor(user) { session.setUser(user); await nextTick() },
        ${gates ? "async gate(mode) { gateMode.value = mode; await nextTick() }," : ''}
        async hide() { visible.value = false; await nextTick() },
        async show() { visible.value = true; await nextTick() }
      }
      return () => visible.value ? ${gates
        ? "gateMode.value !== null ? h(OnboardingFriendGate, { open: true, account: state.accounts.find(a => a.id === state.account), workspace: state.workspace, owner: session.user?.id, ...(gateMode.value === 'outbound' ? { outbound: true } : {}), onClose: () => { gateMode.value = null } }) :"
        : ''} ${messages
        ? "messageView.value ? h(MessageCenter, { account: session.selectedAccount, workspace: state.workspace }) : h(TestTools, state)"
        : 'h(TestTools, state)'} : null
    } })
    app.use(createPinia())${messages ? '.use(ElementPlus)' : ''}.mount('#app')
  `
  let component = 0
  const built = await esbuild.build({
    stdin: { contents: entry, resolveDir: product, loader: 'js' },
    bundle: true, write: false, outfile: path.join(root, 'synthetic-confirmation.js'),
    format: 'iife', platform: 'browser', nodePaths: [deps], logLevel: 'silent',
    define: { 'process.env.NODE_ENV': '"production"', __VUE_OPTIONS_API__: 'true',
      __VUE_PROD_DEVTOOLS__: 'false' },
    plugins: [{
      name: 'synthetic-confirmation-components',
      setup(build) {
        build.onResolve({ filter: /^(vue|pinia|element-plus|@element-plus\/icons-vue)(\/.*)?$/ },
          args => ({ path: dependency.resolve(args.path) }))
        build.onLoad({ filter: /\.vue$/ }, args => {
          const parsed = compiler.parse(fs.readFileSync(args.path, 'utf8'), { filename: args.path })
          if (parsed.errors.length) throw parsed.errors[0]
          const id = `confirmation-${++component}`, descriptor = parsed.descriptor
          const script = compiler.compileScript(descriptor,
            { id, genDefaultAs: '__component', inlineTemplate: true })
          const styles = descriptor.styles.map(style => {
            const result = compiler.compileStyle({ source: style.content, filename: args.path,
              id: `data-v-${id}`, scoped: style.scoped })
            if (result.errors.length) throw result.errors[0]
            return result.code
          }).join('\n')
          return { loader: 'ts', contents: script.content + `
            __component.__scopeId = ${JSON.stringify(`data-v-${id}`)};
            const style = document.createElement('style');
            style.textContent = ${JSON.stringify(styles)}; document.head.appendChild(style);
            export default __component;` }
        })
      }
    }]
  })
  const assets = new Map(built.outputFiles.map(file =>
    [path.extname(file.path), Buffer.from(file.contents)]))
  const browser = await playwright.chromium.launch({
    headless: true, channel: process.env.WSL_BROWSER_CHANNEL || 'chrome',
    ...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
      ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH } : {})
  })
  const page = await browser.newPage()
  page.setDefaultTimeout(3000)
  return { browser, page, assets }
}

async function main() {
  const { browser, page, assets } = await createTestToolsBrowser()
  const errors = [], unexpected = [], checks = []
  page.on('pageerror', error => errors.push(error.message))
  const check = (condition, message) => { if (!condition) throw Error(message) }
  const id = n => `f9100000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const start = Date.now(), user = { id: id(1), username: 'synthetic-confirmation', role: 'superadmin' }
  let elapsed = 0, fixture, challenge, writes = 0, reads = 0
  const now = () => start + elapsed
  const advance = async milliseconds => {
    elapsed += milliseconds
    await page.clock.setFixedTime(new Date(now()))
    await page.clock.fastForward(milliseconds)
  }
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
    if (api === '/test-tools/onboarding/scopes' && req.method() === 'GET')
      return fulfill({ items: [], candidates: { accounts: [], groups: [] } })
    if (/^\/accounts\/[^/]+\/receiver$/.test(api) && req.method() === 'GET')
      return fulfill({ enabled: true, revision: 1, observed: true, snapshot: { state: 'connected' } })
    if (api === '/test-tools/operations' && req.method() === 'GET') return fulfill([])
    if (api === `/group-binding/challenges/${id(5)}` && req.method() === 'GET')
      return fulfill(challenge)
    if (api === '/test-tools/call' && req.method() === 'POST') {
      const { name, arguments: args } = req.postDataJSON()
      check(req.headers()['x-workspace'] === fixture.workspace &&
        args.account_id === fixture.account && args.test_window_id === fixture.windows[0].id,
        'Tool escaped the selected platform/account/window')
      if (name === 'read_membership' || name === 'read_group') {
        reads++
        return fulfill({ snapshot: {
          platform: fixture.workspace, self_id: '7', self_role: 'admin', in_group: true,
          group_id: '123', group_name: '合成隔离群', directory_known: true, members_known: true,
          friends_known: true, friends: [{ id: '8', name: '合成好友' }], member_ids: ['7', '8'],
          account_epoch: 1, account_version: 1, sampled_at: new Date(now()).toISOString(),
          members: [{ id: '8', name: '合成好友', role: 'member' }],
          policy: { muted: false }, mutes_known: true, muted_ids: [],
          applications: [{ id: 'application-8', user: { id: '8', name: '合成好友' },
            direction: 'FROM_HIM', state: 'FRIENDLOG_STATE_PROCESSING' }]
        } })
      }
      if (['run_social', 'run_moderation'].includes(name)) {
        check(args.action === fixture.windows[0].actions[0] && args.target_id === '8' &&
          req.headers()['idempotency-key'] &&
          !['tenant_id', 'role', 'user_id'].some(field => field in args), 'Mutation widened its scope')
        writes++
        return fulfill({ operation: { id: id(6), status: 'failed', result_code: 'synthetic_rejected' } })
      }
    }
    if (api === `/accounts/${fixture?.account}` && req.method() === 'PATCH') {
      check(req.postDataJSON().version === 1, 'Account change lost its version fence')
      writes++
      return fulfill({})
    }
    if (api === `/test-tools/onboarding-friend/challenges/${id(5)}/friend-send` &&
      req.method() === 'POST') {
      const body = req.postDataJSON()
      check(body.test_window_id === fixture.windows[0].id && body.version === challenge.version &&
        req.headers()['x-workspace'] === fixture.workspace && req.headers()['idempotency-key'],
        'Friend request widened its scope')
      writes++
      return fulfill({ ...challenge, friend_outbound: { ...challenge.friend_outbound, status: 'submitted' } })
    }
    unexpected.push(`${req.method()} ${pathname}`)
    return route.fulfill({ status: 500, json: { code: 1, msg: 'unmocked_api' } })
  })
  const tool = page.locator('.test-tools')
  const refresh = tool.getByRole('button', { name: '刷新检查', exact: true })
  const run = tool.getByRole('button', { name: '确认验证此动作', exact: true })
  const confirmation = () => page.locator('.el-message-box:visible')
  const idle = () => refresh.and(page.locator(':enabled')).waitFor()
  const choose = async (name, label) => {
    await page.getByRole('combobox', { name, exact: true })
      .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option', { name: label, exact: true }).click()
  }
  const configure = async (platform, action = 'friend_delete', seconds = 300) => {
    fixture = {
      workspace: platform, account: id(2),
      accounts: [2, 7].map(n => ({ id: id(n), name: `合成账号${n}`, platform, purpose: 'management',
        testing_enabled: true, connection_enabled: true, epoch: 1, version: 1 })),
      groups: [{ group_id: id(3), native_id: '123', name: '合成隔离群', platform }],
      windows: [{ id: id(4), group_id: id(3), account_ids: [id(2)], target_ids: ['8'],
        admission_mode: 'native_debug', actions: [action], budget: 1, used: 0,
        friend_application_id: action === 'friend_approve' ? 'application-8' : '',
        expires_at: new Date(now() + seconds * 1000).toISOString(), cancelled: false }]
    }
    challenge = { id: id(5), version: 1, platform, scope: { actor_id: user.id },
      robot: { id: id(2) }, mode: 'operator_demo', friend_flow: 'incoming_contact', state: 'waiting',
      expires_at: new Date(now() + 300000).toISOString(), customer: { name: '合成申请人' },
      friend_outbound: { id: id(8), status: 'prepared',
        target: { native_id: '8', add_account: 'synthetic_friend', name: '合成好友' } } }
    if (action === 'onboarding_friend_request') {
      fixture.windows[0].target_ids = [
        `onboarding_challenge:${challenge.id}:1`, 'onboarding_account:synthetic_friend', 'onboarding_target:8'
      ]
    }
    await page.evaluate(({ fixture, user }) => window.__testTools.configure(fixture, user), { fixture, user })
    await idle()
  }
  const read = async action => {
    await tool.getByRole('button', { name: action === 'mute_member'
      ? '读取当前群状态' : action === 'friend_approve'
        ? '读取指定好友申请与群状态' : '读取当前好友与群状态', exact: true }).click()
    await idle()
  }
  const ready = async action => {
    await tool.locator('.tools-grant').click()
    await read(action)
    await choose(action === 'friend_delete' ? '授权目标好友' :
      action === 'friend_approve' ? '指定好友申请人' : '授权目标成员', '合成好友 · 8')
    await run.and(page.locator(':enabled')).waitFor()
  }
  const openFriend = async () => {
    await tool.locator('.tools-grant').click()
    await page.getByRole('button', { name: '读取原记录', exact: true }).click()
    await idle()
    await page.getByRole('button', { name: '确认发送好友申请', exact: true }).click()
    await confirmation().waitFor()
  }
  const cancelled = async (name, action) => {
    await confirmation().waitFor({ state: 'hidden' })
    // A hidden MessageBox alone passed the old smoke despite its pending Promise.
    await idle()
    check(writes === 0, `${name} submitted without renewed human confirmation`)
    if (action) check(await run.isDisabled(), `${name} retained executable stale evidence`)
    checks.push(name)
  }
  try {
    await page.goto('http://127.0.0.1:5173')
    for (const platform of ['wsl', 'lajiaoliao']) {
      for (const action of ['friend_delete', 'friend_approve', 'mute_member']) {
        await configure(platform, action); await ready(action)
        await run.click()
        const before = reads
        // The snapshot remains usable immediately before its original 30-second TTL.
        await advance(29000)
        check(await confirmation().isVisible() && await refresh.isDisabled(),
          `${platform}/${action} shortened the evidence TTL or failed to freeze confirmation`)
        await advance(1000)
        await cancelled(`${platform}/${action}/30s-expiry`, action)
        check(reads === before, 'Expiry automatically refreshed native evidence')
        await read(action)
        await run.and(page.locator(':enabled')).waitFor()
        check(writes === 0, 'Refreshing evidence submitted the expired confirmation')
        await run.click()
        await confirmation().getByRole('button', { name: '取消', exact: true }).click()
        await cancelled(`${platform}/${action}/user-cancel`)
        await run.click()
        await confirmation().getByRole('button', { name: '确认提交', exact: true }).click()
        await idle()
        check(writes === 1, 'A fresh explicit confirmation did not submit exactly once')
        writes = 0
      }
      for (const change of ['account', 'actor', 'workspace', 'epoch', 'version', 'revoke', 'unmount']) {
        await configure(platform); await ready('friend_delete'); await run.click()
        await page.evaluate(async ({ change, user }) => {
          const harness = window.__testTools, state = harness.state
          if (change === 'account') state.account = state.accounts[1].id
          if (change === 'workspace') state.workspace = state.workspace === 'wsl' ? 'lajiaoliao' : 'wsl'
          if (change === 'actor') await harness.actor({ ...user, id: 'synthetic-other-actor' })
          if (change === 'epoch' || change === 'version') state.accounts[0][change]++
          if (change === 'revoke') state.windows[0].cancelled = true
          if (change === 'unmount') { await harness.hide(); await harness.show() }
        }, { change, user })
        await cancelled(`${platform}/delete/${change}`)
      }
      await configure(platform, 'friend_delete', 20); await ready('friend_delete'); await run.click()
      await advance(18000)
      await cancelled(`${platform}/delete/window-expiry`, 'friend_delete')
      for (const dismissal of ['cancel', 'escape', 'close', 'scope', 'unmount']) {
        await configure(platform)
        await tool.locator('.tools-account').first().locator('.el-switch').first().click()
        await confirmation().waitFor()
        if (dismissal === 'cancel') await confirmation().getByRole('button', { name: '取消', exact: true }).click()
        if (dismissal === 'escape') await page.keyboard.press('Escape')
        if (dismissal === 'close') await confirmation().getByRole('button', { name: /close/i }).click()
        if (dismissal === 'scope') await page.evaluate(() => {
          window.__testTools.state.account = window.__testTools.state.accounts[1].id
        })
        if (dismissal === 'unmount') await page.evaluate(async () => {
          await window.__testTools.hide(); await window.__testTools.show()
        })
        await cancelled(`${platform}/account-confirm/${dismissal}`)
      }
      for (const change of ['cancel', 'expiry', 'scope', 'version', 'unmount']) {
        await configure(platform, 'onboarding_friend_request', change === 'expiry' ? 20 : 300)
        await openFriend()
        if (change === 'cancel') await confirmation().getByRole('button', { name: '取消', exact: true }).click()
        if (change === 'expiry') await advance(17000)
        if (change === 'scope') await page.evaluate(() => {
          window.__testTools.state.account = window.__testTools.state.accounts[1].id
        })
        if (change === 'version') await page.evaluate(() => window.__testTools.state.accounts[0].version++)
        if (change === 'unmount') await page.evaluate(async () => {
          await window.__testTools.hide(); await window.__testTools.show()
        })
        await cancelled(`${platform}/friend-confirm/${change}`)
      }
    }
    check(!errors.length && !unexpected.length, JSON.stringify({ errors, unexpected }))
    console.log(JSON.stringify({ check: path.basename(__filename), passed: true,
      synthetic_only: true, native_requests: 0, external_requests: 0, checks }))
  } catch (error) {
    console.error(JSON.stringify({ completed: checks, errors, unexpected,
      ui: await page.locator('body').innerText() }))
    throw error
  } finally { await browser.close() }
}

module.exports = { createTestToolsBrowser }
if (require.main === module)
  main().catch(error => { console.error(error.stack || error); process.exitCode = 1 })
