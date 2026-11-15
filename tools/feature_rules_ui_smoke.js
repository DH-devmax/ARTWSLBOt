// Real Vue/Element Plus components, in-memory APIs, no login or native traffic.
// WSL_UI_NODE_MODULES=/absolute/existing/node_modules \
// WSL_PLAYWRIGHT_MODULE=/absolute/playwright/index.mjs node tools/feature_rules_ui_smoke.js
const fs = require('node:fs')
const path = require('node:path')
const { createRequire } = require('node:module')
const { pathToFileURL } = require('node:url')

async function main() {
  const root = path.resolve(__dirname, '..')
  const deps = process.env.WSL_UI_NODE_MODULES || path.join(root, 'web/node_modules')
  if (!path.isAbsolute(deps)) throw Error('WSL_UI_NODE_MODULES must be absolute')
  const dependency = createRequire(path.join(deps, 'package.json'))
  const compiler = dependency('@vue/compiler-sfc')
  compiler.registerTS(() => dependency('typescript'))
  const esbuild = createRequire(dependency.resolve('vite'))('esbuild')
  const { chromium } = await import(
    process.env.WSL_PLAYWRIGHT_MODULE || pathToFileURL(dependency.resolve('playwright')).href
  )
  const output = process.env.WSL_UI_SCREENSHOT_DIR || path.join(root, 'output/playwright')
  if (!path.isAbsolute(output)) throw Error('WSL_UI_SCREENSHOT_DIR must be absolute')
  await fs.promises.mkdir(output, { recursive: true })
  const entry = `
    import { createApp, h, reactive, ref, nextTick } from 'vue'
    import { createPinia } from 'pinia'
    import ElementPlus from 'element-plus'
    import FeatureCenter from './FeatureCenter.vue'
    import { useSession } from './store'
    import 'element-plus/dist/index.css'
    import './theme/lumenwave/system.css'
    import './style.css'
    import './theme/product.css'
    const app = createApp({ setup() {
      const session = useSession(), visible = ref(false)
      const state = reactive({ account: '', groups: [], view: 'modules', initialGroup: '' })
      const originalFetch = window.fetch.bind(window)
      window.fetch = (url, init) => {
        if (init?.signal) init.signal.addEventListener('abort', () => window.__featureRules.aborts++)
        return originalFetch(url, window.__featureRules.ignoreAbort ? { ...init, signal: undefined } : init)
      }
      window.__featureRules = {
        ignoreAbort: false, aborts: 0,
        async configure(value, user) {
          visible.value = false; await nextTick()
          sessionStorage.clear(); session.setUser(user, ['groups:read'])
          session.panels = ['wsl', 'lajiaoliao'].map(id => ({ id, name: id }))
          session.selectWorkspace('wsl'); session.selectAccount(value.account)
          Object.assign(state, value); visible.value = true; await nextTick()
        },
        async actor(user) { session.setUser(user, ['groups:read']); await nextTick() },
        async platform(value) { session.selectWorkspace(value); await nextTick() },
        async group(value) { state.initialGroup = value; await nextTick() },
        async account(value) { state.account = value; session.selectAccount(value); await nextTick() },
        async hide() { visible.value = false; await nextTick() },
        async show() { visible.value = true; await nextTick() },
        async restoreDraft(value) {
          visible.value = false; await nextTick()
          session.saveDraft(\`\${session.workspace}:\${session.user.id}:\${session.user.tenant_id || 'platform'}:\${state.account}:features:\${state.initialGroup || state.groups[0].group_id}\`, value)
          visible.value = true; await nextTick()
        },
        drafts() { return JSON.parse(JSON.stringify(session.drafts)) }
      }
      return () => h('main', { style: 'padding:12px;max-width:1100px;margin:auto;min-width:0' },
        visible.value ? h(FeatureCenter, {
          ...state, groups: state.groups.filter(group => group.primary_account_id === state.account)
        }) : null)
    } })
    app.use(createPinia()).use(ElementPlus).mount('#app')
  `
  let component = 0
  const built = await esbuild.build({
    stdin: {
      contents: entry,
      resolveDir: path.join(root, 'web/src/wsl'),
      loader: 'js'
    },
    bundle: true,
    write: false,
    outfile: path.join(root, 'synthetic-feature-rules.js'),
    format: 'iife',
    platform: 'browser',
    nodePaths: [deps],
    logLevel: 'silent',
    alias: { '@': path.join(root, 'web/src') },
    define: {
      'process.env.NODE_ENV': '"production"',
      __VUE_OPTIONS_API__: 'true',
      __VUE_PROD_DEVTOOLS__: 'false'
    },
    plugins: [
      {
        name: 'synthetic-feature-components',
        setup(build) {
          build.onResolve(
            {
              filter: /^(vue|pinia|element-plus|@element-plus\/icons-vue)(\/.*)?$/
            },
            (args) => ({ path: dependency.resolve(args.path) })
          )
          build.onLoad({ filter: /\.vue$/ }, (args) => {
            const parsed = compiler.parse(fs.readFileSync(args.path, 'utf8'), {
              filename: args.path
            })
            if (parsed.errors.length) throw parsed.errors[0]
            const id = `feature-rules-${++component}`,
              descriptor = parsed.descriptor
            const script = compiler.compileScript(descriptor, {
              id,
              genDefaultAs: '__component',
              inlineTemplate: true
            })
            const styles = descriptor.styles
              .map((style) => {
                const result = compiler.compileStyle({
                  source: style.content,
                  filename: args.path,
                  id: `data-v-${id}`,
                  scoped: style.scoped
                })
                if (result.errors.length) throw result.errors[0]
                return result.code
              })
              .join('\n')
            return {
              loader: 'ts',
              contents:
                script.content +
                `
            __component.__scopeId = ${JSON.stringify(`data-v-${id}`)};
            const style = document.createElement('style');
            style.textContent = ${JSON.stringify(styles)}; document.head.appendChild(style);
            export default __component;`
            }
          })
        }
      }
    ]
  })
  const assets = new Map(
    built.outputFiles.map((file) => [path.extname(file.path), Buffer.from(file.contents)])
  )
  const browser = await chromium.launch({
    headless: true,
    channel: process.env.WSL_BROWSER_CHANNEL || 'chrome',
    ...(process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH
      ? { executablePath: process.env.PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH }
      : {})
  })
  const page = await browser.newPage({
    viewport: { width: 1440, height: 960 }
  })
  page.setDefaultTimeout(5000)
  const errors = [],
    unexpected = [],
    previews = [],
    saves = [],
    screenshots = [],
    checks = []
  page.on('pageerror', (error) => errors.push(error.message))
  const check = (condition, message) => {
    if (!condition) throw Error(message)
  }
  const clone = (value) => JSON.parse(JSON.stringify(value))
  const id = (n) => `fb000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const user = {
    id: id(99),
    tenant_id: id(98),
    username: 'synthetic-rules',
    role: 'user'
  }
  const state = {
    account: id(1),
    view: 'modules',
    initialGroup: '',
    groups: [
      { group_id: id(11), primary_account_id: id(1), name: '规则合成群一' },
      { group_id: id(12), primary_account_id: id(1), name: '规则合成群二' },
      { group_id: id(13), primary_account_id: id(2), name: '规则合成群三' }
    ]
  }
  const definitions = [
    {
      id: 'keyword_replies',
      name: '群内关键词回复',
      category: '回复',
      fields: [
        { key: 'enabled', label: '启用配置', type: 'boolean' },
        { key: 'admin_only', label: '仅群管理员触发', type: 'boolean' },
        {
          key: 'rules',
          label: '关键词与回复',
          type: 'keyword_rules',
          limit: 100
        },
        {
          key: 'cooldown_seconds',
          label: '回复冷却（秒）',
          type: 'integer',
          limit: 86400
        }
      ]
    },
    {
      id: 'blocked_words',
      name: '拦截词',
      category: '群管',
      fields: [
        { key: 'enabled', label: '启用配置', type: 'boolean' },
        { key: 'words', label: '拦截词', type: 'string_list', limit: 200 },
        {
          key: 'rules',
          label: '组合拦截规则',
          type: 'blocked_rules',
          limit: 20
        }
      ]
    },
    ...['blacklist', 'exemptions'].map((key, index) => ({
      id: key,
      name: index ? '豁免名单' : '业务黑名单',
      category: '群管',
      fields: [
        { key: 'enabled', label: '启用配置', type: 'boolean' },
        { key: 'members', label: '成员 UID', type: 'string_list', limit: 2000 }
      ]
    })),
    { id: 'image_replies', name: '图片回复', category: '回复', fields: [] }
  ].map((definition) => ({
    ...definition,
    status: definition.fields.length ? 'implemented' : 'unavailable',
    summary:
      definition.id === 'keyword_replies'
        ? '按保存顺序选择首条已启用的匹配规则。'
        : '合成规则配置。',
    native_dependencies: []
  }))
  const initial = () => ({
    keyword_replies: {
      enabled: true,
      admin_only: false,
      cooldown_seconds: 30,
      rules: [
        { keyword: '你好', reply: '旧规则回复' },
        {
          keyword: '你好世界',
          reply: '精确规则回复',
          match: 'exact',
          disabled: false
        }
      ]
    },
    blocked_words: {
      enabled: true,
      words: ['基础拦截'],
      rules: [{ words: ['测试', '广告'], name: '组合拦截' }]
    },
    blacklist: { enabled: true, members: ['member-muted'] },
    exemptions: { enabled: false, members: [] },
    image_replies: {}
  })
  const records = new Map()
  let readonly = false,
    holdPreview = false,
    holdSave = false,
    badResponse = false,
    conflict = false
  const held = []
  const record = (platform, group) => {
    const key = `${platform}:${group}`
    if (!records.has(key)) records.set(key, { version: 1, values: initial() })
    return records.get(key)
  }
  const decisionFor = (values, body) => {
    const { text, sender, role } = body
    const blocked = values.blocked_words
    if (
      role === 'member' &&
      blocked.enabled &&
      (blocked.words.some((word) => text.includes(word)) ||
        blocked.rules.some(
          (rule) => !rule.disabled && rule.words.every((word) => text.includes(word))
        ))
    )
      return {
        rule: 'blocked_words',
        action: 'recall_message',
        target: sender || 'preview-member',
        suppress_heat: true
      }
    if (role === 'member' && values.blacklist.members.includes(sender))
      return {
        rule: 'blacklist',
        action: 'mute_member',
        target: sender,
        minutes: 10,
        suppress_heat: true
      }
    const replies = values.keyword_replies
    const match =
      replies.enabled &&
      (!replies.admin_only || role !== 'member') &&
      replies.rules.find(
        (rule) =>
          !rule.disabled &&
          (rule.match === 'exact' ? text === rule.keyword : text.includes(rule.keyword))
      )
    return match
      ? {
          rule: 'keyword_replies',
          action: 'send_text',
          text: match.reply,
          suppress_heat: true
        }
      : { suppress_heat: false }
  }
  const release = async () => {
    const pending = held.splice(0)
    for (const { route, data } of pending) await route.fulfill({ json: { code: 0, data, msg: '' } })
    await page.waitForTimeout(80)
  }
  const until = async (test, message) => {
    for (let i = 0; i < 100; i++) {
      if (test()) return
      await page.waitForTimeout(25)
    }
    throw Error(message)
  }
  await page.route('**/*', async (route) => {
    const request = route.request(),
      url = new URL(request.url())
    const fulfill = (data) => route.fulfill({ json: { code: 0, data, msg: '' } })
    if (url.origin !== 'http://127.0.0.1:5173') {
      unexpected.push(request.url())
      return route.abort('blockedbyclient')
    }
    if (url.pathname === '/')
      return route.fulfill({
        contentType: 'text/html',
        body: '<!doctype html><html lang="zh-CN"><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1"><link rel="stylesheet" href="/fixture.css"><div id="app"></div><script src="/fixture.js"></script></html>'
      })
    if (url.pathname === '/fixture.js')
      return route.fulfill({
        contentType: 'text/javascript',
        body: assets.get('.js')
      })
    if (url.pathname === '/fixture.css')
      return route.fulfill({
        contentType: 'text/css',
        body: assets.get('.css')
      })
    if (url.pathname === '/favicon.ico') return route.fulfill({ status: 204 })
    const endpoint = url.pathname.replace('/api/v1', '')
    const match = endpoint.match(/^\/groups\/([^/]+)\/features(\/preview)?$/)
    if (match) {
      const group = state.groups.find((group) => group.group_id === match[1])
      const platform = request.headers()['x-workspace']
      check(group && ['wsl', 'lajiaoliao'].includes(platform), 'Request lost group/platform scope')
      const current = record(platform, group.group_id)
      if (request.method() === 'GET' && !match[2])
        return fulfill({
          group_id: group.group_id,
          account_id: group.primary_account_id,
          platform,
          version: current.version,
          native_capabilities: [],
          items: definitions.map((definition) => ({
            definition,
            values: clone(current.values[definition.id]),
            entitled: true,
            effective_enabled: false,
            editable_fields: readonly ? [] : definition.fields.map((field) => field.key)
          }))
        })
      const body = request.postDataJSON()
      if (request.method() === 'POST' && match[2]) {
        previews.push({ platform, group: group.group_id, body: clone(body) })
        check(
          Object.keys(body.changes).length <= 1 &&
            Object.keys(body.changes).every((key) => key === body.feature),
          'Preview included another feature draft'
        )
        check(
          !readonly || !Object.keys(body.changes).length,
          'Readonly preview submitted draft fields'
        )
        const values = clone(current.values)
        if (body.changes[body.feature])
          Object.assign(values[body.feature], body.changes[body.feature])
        const data = {
          group_id: group.group_id,
          platform,
          version: body.version,
          feature: body.feature,
          validation_level: 'simulation',
          applied: false,
          native_submitted: false,
          decision: decisionFor(values, body),
          cooldown_seconds: values.keyword_replies.cooldown_seconds,
          sampled_at: '2026-11-14T02:00:00Z'
        }
        if (badResponse) {
          badResponse = false
          data.native_submitted = true
        }
        if (holdPreview) {
          holdPreview = false
          held.push({ route, data })
          return
        }
        return fulfill(data)
      }
      if (request.method() === 'PUT' && !match[2]) {
        check(!readonly, 'Readonly user saved configuration')
        check(Object.keys(body.changes).length === 1, 'Save included multiple features')
        saves.push({ platform, group: group.group_id, body: clone(body) })
        if (conflict) {
          conflict = false
          current.version++
          return route.fulfill({
            status: 409,
            json: { code: 1, msg: 'version_conflict' }
          })
        }
        check(body.version === current.version, 'Save lost its version fence')
        for (const [feature, fields] of Object.entries(body.changes))
          Object.assign(current.values[feature], fields)
        const data = { version: ++current.version }
        if (holdSave) {
          holdSave = false
          held.push({ route, data })
          return
        }
        return fulfill(data)
      }
    }
    unexpected.push(`${request.method()} ${endpoint}`)
    return route.fulfill({
      status: 500,
      json: { code: 1, msg: 'unmocked_api' }
    })
  })
  const dialog = () => page.locator('.feature-detail:visible')
  const result = () => dialog().getByRole('status', { name: '规则试算结果', exact: true })
  const previewButton = () => dialog().getByRole('button', { name: '试算规则', exact: true })
  const saveButton = () => dialog().getByRole('button', { name: '保存配置', exact: true })
  const choose = async (label, name) => {
    const input = page.getByRole('combobox', { name: label, exact: true })
    await input.locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    const controls = await input.getAttribute('aria-controls')
    await page.locator(`[id="${controls}"]`).getByRole('option', { name, exact: true }).click()
  }
  const open = async (name) => {
    await page
      .getByTestId('feature-center')
      .getByRole('row')
      .filter({ hasText: name })
      .getByRole('button', {
        name: name === '图片回复' ? '查看详情' : '详情配置',
        exact: true
      })
      .click()
    await dialog().waitFor()
  }
  const close = async () => {
    await dialog().getByRole('button', { name: '返回功能列表', exact: true }).click()
    await dialog().waitFor({ state: 'hidden' })
  }
  const trial = async (text) => {
    await dialog().getByRole('textbox', { name: '试算文本', exact: true }).fill(text)
    await previewButton().click()
    await result().waitFor()
  }
  const noResult = async (message) => check((await result().count()) === 0, message)
  const restored = async (label, text) => {
    check(await saveButton().isDisabled(), `${label}: restored values left save enabled`)
    check(
      (await dialog().getByRole('button', { name: '放弃修改', exact: true }).count()) === 0,
      `${label}: restored values retained feature changes`
    )
    const drafts = await page.evaluate(() => window.__featureRules.drafts())
    check(
      Object.values(drafts).every((value) => !value?.values),
      `${label}: restored values retained a stored draft`
    )
    await trial(text)
    check(
      Object.keys(previews.at(-1).body.changes).length === 0,
      `${label}: restored preview submitted changes`
    )
    check(saves.length === 0, `${label}: restoring values performed PUT`)
  }
  const configure = async () => {
    await page.evaluate(({ state, user }) => window.__featureRules.configure(state, user), {
      state,
      user
    })
    await page
      .getByTestId('feature-center')
      .getByRole('button', { name: '详情配置' })
      .first()
      .waitFor()
  }
  const cachedDraft = async (group = id(11)) => {
    const drafts = await page.evaluate(() => window.__featureRules.drafts())
    return drafts[`wsl:${user.id}:${user.tenant_id}:${id(1)}:features:${group}`]
  }
  const adoptVersion = () =>
    dialog()
      .getByRole('button', { name: /^已核对，采用版本/ })
      .click()
  try {
    await page.goto('http://127.0.0.1:5173')
    await configure()
    await open('群内关键词回复')
    check(
      (await dialog()
        .getByRole('spinbutton', { name: '回复冷却（秒）', exact: true })
        .inputValue()) === '30',
      'Default cooldown is not 30'
    )
    check(
      await dialog().getByRole('checkbox', { name: '规则 1 启用', exact: true }).isChecked(),
      'Legacy omitted disabled did not default to enabled'
    )
    await trial('你好小组')
    check(
      previews.at(-1).body.version === 1 &&
        !Object.keys(previews.at(-1).body.changes).length &&
        previews.at(-1).body.role === 'member',
      'Saved preview created changes or lost version/role'
    )
    check(
      (await result().innerText()).includes('旧规则回复') && saves.length === 0,
      'Preview did not display server reply or performed PUT'
    )
    await choose('匹配模式 1', '完全匹配')
    await choose('匹配模式 1', '包含')
    await restored('Legacy match restored before enable', '你好小组')
    for (let toggle = 0; toggle < 2; toggle++)
      await dialog()
        .getByRole('checkbox', { name: '规则 1 启用', exact: true })
        .locator('xpath=ancestor::label')
        .click()
    await restored('Legacy enable restored after match', '你好小组')
    await choose('匹配模式 2', '包含')
    await choose('匹配模式 2', '完全匹配')
    for (let toggle = 0; toggle < 2; toggle++)
      await dialog()
        .getByRole('checkbox', { name: '规则 2 启用', exact: true })
        .locator('xpath=ancestor::label')
        .click()
    await restored('Explicit match and enable restored', '你好世界')
    await close()
    await open('群内关键词回复')
    await restored('Reopened keyword rules', '你好小组')
    await close()
    await open('拦截词')
    for (let toggle = 0; toggle < 2; toggle++)
      await dialog()
        .getByRole('checkbox', { name: '组合规则 1 启用', exact: true })
        .locator('xpath=ancestor::label')
        .click()
    await restored('Blocked enable restored', '测试含广告')
    await dialog().getByRole('button', { name: '删除组合规则 1', exact: true }).click()
    await dialog().getByRole('button', { name: '添加组合规则', exact: true }).click()
    await dialog().getByRole('textbox', { name: '组合规则名称 1', exact: true }).fill('组合拦截')
    await dialog()
      .getByRole('textbox', { name: '组合词（一行一个） 1', exact: true })
      .fill('测试\n广告')
    await restored('Blocked values restored with different property order', '测试含广告')
    await close()
    await open('群内关键词回复')
    checks.push('restored match/enable and property order leave no changes, stored drafts or PUT')
    await dialog()
      .getByRole('checkbox', { name: '规则 1 启用', exact: true })
      .locator('xpath=ancestor::label')
      .click()
    await noResult('Rule edit retained the old result')
    await trial('你好世界')
    check(
      (await result().innerText()).includes('精确规则回复'),
      'Disabled first rule or exact matching was ignored'
    )
    await trial('你好世界小组')
    check(
      (await result().innerText()).includes('未命中') &&
        (await result().innerText()).includes('无动作'),
      'Exact rule matched a substring or disabled rule still replied'
    )
    await dialog()
      .getByRole('checkbox', { name: '规则 1 启用', exact: true })
      .locator('xpath=ancestor::label')
      .click()
    await choose('匹配模式 1', '完全匹配')
    await choose('匹配模式 1', '包含')
    check(await saveButton().isDisabled(), 'Legacy defaults created a phantom draft')
    await dialog().getByRole('button', { name: '添加规则', exact: true }).click()
    check(await saveButton().isDisabled(), 'New empty reply rule could be saved')
    await dialog().getByRole('button', { name: '删除规则 3', exact: true }).click()
    await dialog()
      .getByRole('checkbox', { name: '规则 1 启用', exact: true })
      .locator('xpath=ancestor::label')
      .click()
    await choose('匹配模式 2', '包含')
    await dialog().getByRole('button', { name: '上移规则 2', exact: true }).click()
    await dialog().getByRole('textbox', { name: '回复内容 1', exact: true }).fill('草稿首条回复')
    await dialog().getByRole('spinbutton', { name: '回复冷却（秒）', exact: true }).fill('90')
    await dialog().getByRole('spinbutton', { name: '回复冷却（秒）', exact: true }).press('Tab')
    await trial('你好世界小组')
    const keyword = previews.at(-1).body.changes.keyword_replies
    check(
      keyword.rules[0].keyword === '你好世界' &&
        keyword.rules[0].match === 'contains' &&
        keyword.rules[1].disabled === true &&
        keyword.cooldown_seconds === 90,
      'Rule enable/mode/order/cooldown draft was lost'
    )
    check(
      (await result().innerText()).includes('草稿首条回复') && saves.length === 0,
      'Draft preview performed a save or lost the reply'
    )
    await close()
    await open('拦截词')
    await noResult('Detail switch retained a preview')
    check(
      (await dialog().getByRole('textbox', { name: '拦截词', exact: true }).inputValue()) ===
        '基础拦截',
      'Original blocked words disappeared'
    )
    await dialog()
      .getByRole('checkbox', { name: '组合规则 1 启用', exact: true })
      .locator('xpath=ancestor::label')
      .click()
    await dialog().getByRole('button', { name: '添加组合规则', exact: true }).click()
    await dialog().getByRole('textbox', { name: '组合规则名称 2', exact: true }).fill('新增组合')
    const words = dialog().getByRole('textbox', {
      name: '组合词（一行一个） 2',
      exact: true
    })
    for (const invalid of ['促销', '一\n二\n三\n四\n五\n六', '重复\n重复']) {
      await words.fill(invalid)
      check(await saveButton().isDisabled(), 'Invalid combination could be saved')
    }
    await words.fill('促销\n链接')
    await trial('促销附带链接')
    const blocked = previews.at(-1).body.changes.blocked_words
    check(
      Array.isArray(blocked.rules[1].words) &&
        blocked.rules[1].words.join('|') === '促销|链接' &&
        blocked.rules[0].disabled === true &&
        !('words' in blocked),
      'Blocked draft lost word arrays, disable state or preserved base words'
    )
    const drafts = await page.evaluate(() => window.__featureRules.drafts())
    const stored = Object.values(drafts).find((value) => value?.values?.blocked_words)
    check(
      Array.isArray(stored.values.blocked_words.rules[1].words),
      'Editing model is not an array'
    )
    await dialog().getByRole('button', { name: '添加组合规则', exact: true }).click()
    await dialog().getByRole('button', { name: '删除组合规则 3', exact: true }).click()
    await close()
    await open('群内关键词回复')
    check(
      (await dialog().getByRole('textbox', { name: '回复内容 1', exact: true }).inputValue()) ===
        '草稿首条回复' &&
        !(await dialog().getByRole('checkbox', { name: '规则 2 启用', exact: true }).isChecked()),
      'Closing the dialog discarded rule drafts'
    )
    await trial('你好世界')
    await saveButton().click()
    await dialog().getByText('配置已保存；原生执行尚未开放', { exact: true }).waitFor()
    await noResult('Save retained old preview')
    check(
      Object.keys(saves.at(-1).body.changes).join() === 'keyword_replies',
      'Saving keyword rules submitted blocked drafts'
    )
    await close()
    await open('拦截词')
    check(
      (await words.inputValue()) === '促销\n链接',
      'Independent feature save lost another draft'
    )
    await trial('促销附带链接')
    check(
      previews.at(-1).body.version === 2 &&
        Object.keys(previews.at(-1).body.changes).join() === 'blocked_words',
      'Remaining draft did not rebase to the saved version'
    )
    await close()
    checks.push(
      'legacy-defaults; per-rule enable/match/order; array drafts; independent feature save'
    )

    // Ignore cancellation deliberately: sequence and scope guards must reject actual late responses.
    await page.evaluate(() => {
      window.__featureRules.ignoreAbort = true
    })
    await open('群内关键词回复')
    await trial('你好世界')
    holdPreview = true
    await previewButton().click()
    await until(() => held.length === 1, 'Deferred preview did not start')
    await dialog().getByRole('textbox', { name: '回复内容 1', exact: true }).fill('隔离后的新回复')
    await release()
    await noResult('Late preview survived a draft edit')
    holdPreview = true
    await previewButton().click()
    await until(() => held.length === 1, 'Second deferred preview did not start')
    await dialog().getByRole('textbox', { name: '试算文本', exact: true }).fill('你好世界，第二次')
    await previewButton().click()
    await result().waitFor()
    await release()
    check(
      (await result().innerText()).includes('隔离后的新回复'),
      'Old sequence overwrote the latest result'
    )
    for (const change of ['detail', 'group', 'account', 'platform', 'actor', 'exit']) {
      holdPreview = true
      await previewButton().click()
      await until(() => held.length === 1, 'Scope preview did not start: ' + change)
      if (change === 'detail') await close()
      if (change === 'group')
        await page.evaluate((value) => window.__featureRules.group(value), id(12))
      if (change === 'account') {
        await page.evaluate((value) => window.__featureRules.account(value), id(2))
      }
      if (change === 'platform')
        await page.evaluate(() => window.__featureRules.platform('lajiaoliao'))
      if (change === 'actor')
        await page.evaluate((value) => window.__featureRules.actor(value), {
          ...user,
          id: id(199)
        })
      if (change === 'exit') {
        await page.evaluate(() => window.__featureRules.hide())
        await release()
        await page.evaluate(() => window.__featureRules.show())
      } else await release()
      await page
        .getByTestId('feature-center')
        .getByRole('button', { name: '详情配置' })
        .first()
        .waitFor()
      await open(change === 'detail' ? '拦截词' : '群内关键词回复')
      await noResult('Late preview survived ' + change)
      if (change === 'detail') {
        await close()
        await open('群内关键词回复')
      }
      if (change === 'actor')
        check(
          (await dialog()
            .getByRole('textbox', { name: '回复内容 1', exact: true })
            .inputValue()) !== '隔离后的新回复',
          'Actor acquired another actor draft'
        )
      await trial('你好世界')
    }
    check(
      (await page.evaluate(() => window.__featureRules.aborts)) >= 8,
      'Requests were not aborted'
    )
    badResponse = true
    await previewButton().click()
    await dialog().getByText('试算响应与当前配置不一致，请重新试算', { exact: true }).waitFor()
    await noResult('Non-simulation response was shown as proof')
    await close()
    checks.push(
      'draft/input/sequence/detail/group/account/platform/actor/unmount guards; response contract'
    )

    readonly = true
    await configure()
    await open('群内关键词回复')
    check(
      (await saveButton().count()) === 0 &&
        (await dialog().getByRole('textbox', { name: '关键词 1', exact: true }).isDisabled()),
      'Readonly user acquired an editor action'
    )
    await trial('你好世界')
    check(!Object.keys(previews.at(-1).body.changes).length, 'Readonly saved preview had changes')
    await close()
    await open('图片回复')
    check(
      (await dialog().locator('.business-rule-editor').count()) === 0 &&
        (await previewButton().count()) === 0 &&
        (await saveButton().count()) === 0,
      'Fieldless unavailable feature acquired fake controls'
    )
    await close()
    readonly = false
    await page.getByRole('button', { name: '重新读取功能配置', exact: true }).click()
    await open('拦截词')
    await dialog().getByRole('textbox', { name: '拦截词', exact: true }).fill('权限收回前的草稿')
    await close()
    readonly = true
    await page.getByRole('button', { name: '重新读取功能配置', exact: true }).click()
    await open('拦截词')
    await dialog().getByRole('textbox', { name: '试算文本', exact: true }).fill('权限收回前的草稿')
    check(await previewButton().isDisabled(), 'Restored unauthorized draft could be previewed')
    await dialog().getByRole('button', { name: '放弃修改', exact: true }).click()
    await previewButton().click()
    await result().waitFor()
    check(
      !Object.keys(previews.at(-1).body.changes).length,
      'Readonly discard did not restore saved trial'
    )
    await close()
    checks.push('readonly saved preview; revoked field grants; fieldless feature')

    readonly = false
    await configure()
    await open('群内关键词回复')
    await dialog().getByRole('textbox', { name: '回复内容 1', exact: true }).fill('冲突草稿')
    conflict = true
    await saveButton().click()
    await dialog().getByText('配置已变化，请刷新后重新编辑', { exact: true }).waitFor()
    await close()
    await page.getByRole('button', { name: '重新读取功能配置', exact: true }).click()
    await open('群内关键词回复')
    check(
      (await dialog().getByRole('textbox', { name: '回复内容 1', exact: true }).inputValue()) ===
        '冲突草稿' && (await saveButton().isDisabled()),
      'Version conflict lost or admitted stale draft'
    )
    await dialog()
      .getByRole('button', { name: /^已核对，采用版本/ })
      .click()
    await trial('你好世界')
    await dialog().getByRole('textbox', { name: '试算文本', exact: true }).fill('汉'.repeat(5462))
    check(await previewButton().isDisabled(), 'UTF-8 byte limit used character count')
    await dialog().getByRole('textbox', { name: '试算文本', exact: true }).fill('a'.repeat(16384))
    check(!(await previewButton().isDisabled()), '16384-byte text was rejected')
    await trial('你好世界')
    await choose('模拟角色', '管理员')
    await dialog()
      .getByRole('textbox', { name: '模拟成员 ID（可选）', exact: true })
      .fill('simulation-member')
    await previewButton().click()
    await result().waitFor()
    check(
      previews.at(-1).body.role === 'admin' && previews.at(-1).body.sender === 'simulation-member',
      'Role or optional member ID was lost'
    )
    await choose('模拟角色', '群主')
    await previewButton().click()
    await result().waitFor()
    check(previews.at(-1).body.role === 'owner', 'Owner simulation was lost')
    checks.push('version conflict; UTF-8 byte bound; simulated roles and sender')

    await dialog()
      .getByRole('textbox', { name: '回复内容 1', exact: true })
      .fill('长回复' + 'x'.repeat(350))
    await trial('你好世界')
    const capture = async (kind, width, bottom = false) => {
      await page.setViewportSize({ width, height: 900 })
      await dialog()
        .locator('.el-dialog__body')
        .evaluate((element, bottom) => {
          element.scrollTop = bottom ? element.scrollHeight : 0
        }, bottom)
      await page.waitForTimeout(60)
      const bounds = await dialog().boundingBox()
      check(
        bounds &&
          bounds.x >= 0 &&
          bounds.y >= 0 &&
          bounds.x + bounds.width <= width + 1 &&
          bounds.y + bounds.height <= 901,
        'Dialog overflow: ' + width
      )
      check(
        await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1),
        'Page overflow: ' + width
      )
      check(
        await dialog()
          .locator('.el-dialog__body')
          .evaluate((element) => element.scrollWidth <= element.clientWidth + 1),
        'Dialog body overflow: ' + width
      )
      for (const button of await dialog().locator('.feature-detail-actions button').all()) {
        const box = await button.boundingBox()
        check(
          box &&
            box.x >= 0 &&
            box.x + box.width <= width + 1 &&
            box.y + box.height <= 901 &&
            box.height >= 36,
          'Footer action clipped: ' + width
        )
      }
      const filename = path.join(output, `feature-rules-${kind}-${width}.png`)
      await page.screenshot({
        path: filename,
        fullPage: true,
        animations: 'disabled'
      })
      screenshots.push(filename)
    }
    for (const width of [320, 390, 768, 1440]) {
      await capture('keyword', width)
      await capture('preview', width, true)
    }
    await close()
    await open('拦截词')
    await trial('测试广告')
    for (const width of [320, 390, 768, 1440]) await capture('blocked', width)
    await close()

    records.clear()
    await configure()
    await open('拦截词')
    await dialog().getByRole('textbox', { name: '拦截词', exact: true }).fill('仅本地拦截草稿')
    await close()
    const concurrent = record('wsl', id(11))
    concurrent.values.keyword_replies.rules[0].reply = '别人保存的新回复'
    concurrent.version++
    await page.getByRole('button', { name: '重新读取功能配置', exact: true }).click()
    await open('群内关键词回复')
    check(
      (await dialog().getByRole('textbox', { name: '回复内容 1', exact: true }).inputValue()) ===
        '别人保存的新回复' &&
        (await saveButton().isDisabled()) &&
        (await previewButton().isDisabled()),
      'Restoring a blocked draft rolled back another feature or bypassed version adoption'
    )
    await adoptVersion()
    await trial('你好小组')
    check(
      !Object.keys(previews.at(-1).body.changes).length &&
        (await result().innerText()).includes('别人保存的新回复'),
      'Untouched keyword preview included an old reply'
    )
    await dialog().getByRole('spinbutton', { name: '回复冷却（秒）', exact: true }).fill('60')
    await dialog().getByRole('spinbutton', { name: '回复冷却（秒）', exact: true }).press('Tab')
    await saveButton().click()
    await dialog().getByText('配置已保存；原生执行尚未开放', { exact: true }).waitFor()
    check(
      Object.keys(saves.at(-1).body.changes.keyword_replies).join() === 'cooldown_seconds' &&
        concurrent.values.keyword_replies.rules[0].reply === '别人保存的新回复',
      'Saving a keyword field resubmitted or overwrote an untouched reply'
    )
    let cached = await cachedDraft()
    check(
      Object.keys(cached.values).join() === 'blocked_words' &&
        Object.keys(cached.baseline).join() === 'blocked_words' &&
        Object.keys(cached.values.blocked_words).join() === 'words' &&
        cached.version === concurrent.version,
      'Independent save retained a full snapshot or saved-feature patch'
    )
    await close()
    await page.getByRole('button', { name: '重新读取功能配置', exact: true }).click()
    await open('拦截词')
    check(
      (await dialog().getByRole('textbox', { name: '拦截词', exact: true }).inputValue()) ===
        '仅本地拦截草稿',
      'Independent save lost the remaining draft'
    )
    await trial('你好小组')
    check(
      Object.keys(previews.at(-1).body.changes).join() === 'blocked_words' &&
        (await result().innerText()).includes('别人保存的新回复'),
      'Remaining draft leaked another feature or lost its new baseline'
    )
    await close()
    checks.push(
      'dirty-field restore preserves concurrent edits; explicit version adoption; sparse remaining save'
    )

    const legacyValues = clone(concurrent.values)
    for (const definition of definitions)
      for (const field of definition.fields)
        if (field.type === 'string_list')
          legacyValues[definition.id][field.key] = legacyValues[definition.id][field.key].join('\n')
    legacyValues.blocked_words.words = '同版本旧草稿'
    const legacy = { version: concurrent.version, values: legacyValues }
    await page.evaluate((value) => window.__featureRules.restoreDraft(value), legacy)
    await open('拦截词')
    cached = await cachedDraft()
    check(
      (await dialog().getByRole('textbox', { name: '拦截词', exact: true }).inputValue()) ===
        '同版本旧草稿' &&
        cached.baseline &&
        Object.keys(cached.values).join() === 'blocked_words',
      'Same-version legacy draft did not migrate to a sparse patch'
    )
    await close()
    concurrent.values.keyword_replies.rules[0].reply = '再更新的回复'
    concurrent.version++
    const savesBeforeLegacy = saves.length
    await page.evaluate((value) => window.__featureRules.restoreDraft(value), legacy)
    await page
      .getByText('旧版草稿缺少字段基线且配置版本已变化，已失效；请重新编辑。', { exact: true })
      .waitFor()
    await open('群内关键词回复')
    await trial('你好小组')
    check(
      !Object.keys(previews.at(-1).body.changes).length &&
        (await result().innerText()).includes('再更新的回复') &&
        !(await cachedDraft()) &&
        saves.length === savesBeforeLegacy,
      'Stale legacy full snapshot was restored, retained or submitted'
    )
    await close()
    checks.push(
      'same-version legacy migration; explicit invalidation of stale baseline-free drafts'
    )

    records.clear()
    await configure()
    await open('拦截词')
    await dialog().getByRole('textbox', { name: '拦截词', exact: true }).fill('迟到保存保留草稿')
    await close()
    await open('群内关键词回复')
    await dialog()
      .getByRole('textbox', { name: '回复内容 1', exact: true })
      .fill('迟到的已保存回复')
    holdSave = true
    await saveButton().click()
    await until(() => held.length === 1, 'Deferred save did not start')
    await page.evaluate((value) => window.__featureRules.group(value), id(12))
    await open('群内关键词回复')
    await release()
    cached = await cachedDraft()
    check(
      Object.keys(cached.values).join() === 'blocked_words' &&
        Object.keys(cached.baseline).join() === 'blocked_words' &&
        cached.version === 2 &&
        (await dialog().getByRole('textbox', { name: '回复内容 1', exact: true }).inputValue()) ===
          '旧规则回复',
      'Late save lost remaining patches or crossed into the current group'
    )
    await close()
    await page.evaluate((value) => window.__featureRules.group(value), id(11))
    await open('群内关键词回复')
    await trial('你好小组')
    check(
      !Object.keys(previews.at(-1).body.changes).length &&
        (await result().innerText()).includes('迟到的已保存回复'),
      'Late save resurrected the saved feature draft'
    )
    await dialog().getByRole('textbox', { name: '回复内容 1', exact: true }).fill('第二次迟到保存')
    holdSave = true
    await saveButton().click()
    await until(() => held.length === 1, 'Second deferred save did not start')
    await page.evaluate((value) => window.__featureRules.group(value), id(12))
    await open('群内关键词回复')
    await close()
    await page.evaluate((value) => window.__featureRules.group(value), id(11))
    await open('拦截词')
    await adoptVersion()
    await dialog().getByRole('textbox', { name: '拦截词', exact: true }).fill('迟到期间的新草稿')
    const newerDraft = await cachedDraft()
    await release()
    check(
      JSON.stringify(await cachedDraft()) === JSON.stringify(newerDraft) &&
        newerDraft.values.blocked_words.words.join() === '迟到期间的新草稿' &&
        newerDraft.version === 3,
      'Late save overwrote a newer draft or its adopted version'
    )
    await close()
    checks.push(
      'late save trims only the saved patch; group isolation; newer draft/version preserved'
    )

    const current = record('wsl', id(11))
    current.values.keyword_replies.rules = Array.from({ length: 100 }, (_, index) => ({
      keyword: `limit-${index}`,
      reply: '合成上限'
    }))
    current.values.blocked_words.rules = Array.from({ length: 20 }, (_, index) => ({
      name: `limit-${index}`,
      words: ['合成', String(index)]
    }))
    await configure()
    await open('群内关键词回复')
    check(
      await dialog().getByRole('button', { name: '添加规则', exact: true }).isDisabled(),
      'Keyword rule count exceeded 100'
    )
    await close()
    await open('拦截词')
    check(
      await dialog().getByRole('button', { name: '添加组合规则', exact: true }).isDisabled(),
      'Blocked rule count exceeded 20'
    )
    check(!errors.length && !unexpected.length, 'Runtime errors or unexpected network traffic')
    checks.push('100/20 rule limits; 320/390/768/1440 layout and screenshots')
    console.log(
      JSON.stringify(
        {
          check: 'feature_rules_ui_smoke.js',
          passed: true,
          checks,
          synthetic_previews: previews.length,
          synthetic_saves: saves.length,
          real_native_requests: 0,
          unexpected_requests: unexpected,
          browser_errors: errors,
          viewports: [320, 390, 768, 1440],
          screenshots
        },
        null,
        2
      )
    )
  } catch (error) {
    await page.screenshot({
      path: path.join(output, 'feature-rules-failure.png'),
      fullPage: true
    })
    console.error(
      JSON.stringify({
        browser_errors: errors,
        unexpected,
        ui: await page.locator('body').innerText()
      })
    )
    throw error
  } finally {
    await release().catch(() => {})
    await browser.close()
  }
}

if (require.main === module)
  main().catch((error) => {
    console.error(error)
    process.exitCode = 1
  })
