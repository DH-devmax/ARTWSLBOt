// Synthetic account/configuration APIs only; real native and model writes are blocked.
// prettier-ignore
async (page) => {
  const base = 'http://127.0.0.1:5173'
  if (new URL(page.url()).origin !== base) throw new Error('Local console only')
  const realMe = (await (await page.request.get(base + '/api/v1/me')).json()).data
  const original = await page.evaluate(() => Object.fromEntries(Object.entries(sessionStorage).filter(([key]) => key.startsWith('wsl:'))))
  const id = (n) => `30000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const accounts = [1, 2].map((n) => ({ id: id(n), name: `热群${n}`, version: 1, workspace: 'maibot', auth_state: 'not_authenticated', runtime_state: 'disconnected' }))
  const settings = new Map()
  const defaults = (account, section) => ({
    models: { endpoint: 'https://synthetic.invalid/v1', chat_model: `model-${account}`, vision_model: '', embedding_model: '', has_api_key: account === id(1) },
    chat: { enabled: false, reply_probability: 0.1, cooldown_seconds: 30, start_hour: 0, end_hour: 24 },
    learning: { enabled: false, expressions: false, jargon: false, derived_retention_days: 30, cross_group: false },
    memory: { enabled: false, top_k: 5, raw_retention_days: 7, derived_retention_days: 30, cross_group: false }
  })[section]
  const writes = []
  let conflict = false
  const pattern = '**/api/v1/**'
  const handler = async (route) => {
    const request = route.request(), url = new URL(request.url())
    const path = url.pathname.replace('/api/v1', ''), method = request.method()
    const workspace = request.headers()['x-workspace'] || 'wsl'
    const match = path.match(/^\/accounts\/([^/]+)\/maibot-settings\/([^/]+)$/)
    let data = []
    if (method === 'GET') {
      if (path === '/me') data = realMe
      else if (path === '/accounts') data = workspace === 'maibot' ? accounts : []
      else if (path === '/overview') data = { accounts: 2, groups: 0, ai_groups: 0, limits: { accounts: 10, groups: 100, ai_groups: 100 } }
      else if (match) {
        if (workspace !== 'maibot') throw new Error('Configuration fetched under wrong workspace')
        const key = `${match[1]}:${match[2]}`
        data = settings.get(key) || { version: 0, config: defaults(match[1], match[2]), applied: false }
      } else if (path === '/maibot/capabilities') data = {
        items: [{ id: 'memory', name: '长期记忆', status: 'engine_pending', scope: 'group_required' }]
      }
    } else if (method === 'PUT' && match && workspace === 'maibot') {
      const body = request.postDataJSON()
      writes.push({ path, workspace, body })
      if (conflict) {
        conflict = false
        await route.fulfill({ status: 409, json: { code: 1, msg: 'version_conflict' } })
        return
      }
      const config = { ...body.config }
      delete config.api_key
      delete config.clear_api_key
      data = { version: body.version + 1, config, applied: false, runtime_ready: false }
      settings.set(`${match[1]}:${match[2]}`, data)
    } else {
      await route.abort('blockedbyclient')
      throw new Error(`Unexpected native/business/model write: ${method} ${path}`)
    }
    await route.fulfill({ json: { code: 0, data, msg: '' } })
  }
  const choose = async (account) => {
    await page.getByRole('combobox', { name: '机器人面板', exact: true })
      .locator('xpath=ancestor::div[contains(@class, "el-select__wrapper")]').click()
    await page.getByRole('option', { name: account }).click()
  }
  const navigate = async (name) => page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name, exact: true }).click()
  await page.route(pattern, handler)
  try {
    await page.evaluate(({ user, account }) => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
      sessionStorage.setItem(`wsl:workspace:${user}`, 'maibot')
      sessionStorage.setItem(`wsl:account:${user}:maibot`, account)
    }, { user: realMe.user.id, account: id(1) })
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.goto(base + '/#overview')
    await page.reload()
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addStyleTag({ content: '*,*::before,*::after{transition:none!important;animation:none!important}' })
    await page.locator('.brand-name').filter({ hasText: '热群机器人' }).waitFor()
    await navigate('模型服务')
    const chatModel = page.getByLabel('聊天模型', { exact: true })
    await chatModel.waitFor()
    await chatModel.fill('account-one-draft')
    await page.getByLabel('API 密钥（已保存；留空保持不变）', { exact: true }).fill('synthetic-never-persisted-key')
    await choose('热群2')
    await page.waitForFunction((value) => [...document.querySelectorAll('input')].some((input) => input.value === value), `model-${id(2)}`)
    if (await chatModel.inputValue() === 'account-one-draft') throw new Error('Account draft leaked')
    await choose('热群1')
    await page.waitForFunction(() => [...document.querySelectorAll('input')].some((input) => input.value === 'account-one-draft'))
    if (await page.getByLabel('API 密钥（已保存；留空保持不变）', { exact: true }).inputValue()) throw new Error('Secret survived an account switch')
    if (await page.evaluate(() => JSON.stringify(sessionStorage).includes('synthetic-never-persisted-key'))) throw new Error('Secret saved into browser storage')
    conflict = true
    await page.getByRole('button', { name: '保存配置', exact: true }).click()
    await page.getByText('配置已变化，请刷新后重新编辑', { exact: true }).waitFor()
    if (await chatModel.inputValue() !== 'account-one-draft') throw new Error('Conflict discarded user input')
    await page.getByRole('button', { name: '重新读取已保存配置', exact: true }).click()
    await page.waitForFunction((value) => [...document.querySelectorAll('input')].some((input) => input.value === value), `model-${id(1)}`)
    await chatModel.fill('saved-model-one')
    await page.getByRole('button', { name: '保存配置', exact: true }).click()
    await page.getByText('配置已保存，运行待验收', { exact: true }).waitFor()
    if (writes.length !== 2 || writes.some((write) => !write.path.includes(id(1)))) throw new Error('Save changed the wrong account')
    await navigate('长期记忆')
    await page.getByText('租户 / 群 / 机器人身份', { exact: true }).waitFor()
    await page.waitForFunction(() => !document.querySelector('.ai-console .el-loading-mask'))
    await page.waitForFunction(() => !document.querySelector('.el-message'))
    await page.screenshot({ path: 'output/playwright/go-console-memory-desktop.png', fullPage: true, animations: 'disabled' })
    for (const viewport of [{ width: 320, height: 740 }, { width: 390, height: 844 }, { width: 647, height: 779 }]) {
      await page.setViewportSize(viewport)
      if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth)) throw new Error(`Console overflow ${viewport.width}`)
      await page.getByRole('button', { name: '展开导航', exact: true }).click()
      await page.waitForFunction(() => Math.abs(document.querySelector('.sidebar').getBoundingClientRect().left) < 1)
      const contained = await page.evaluate(() => {
        const brand = document.querySelector('.brand').getBoundingClientRect()
        const toggle = document.querySelector('.workspace-switch').getBoundingClientRect()
        return toggle.left >= brand.left && toggle.right <= brand.right + 1
      })
      if (!contained) throw new Error('Workspace switch left the sidebar brand')
      if (!await page.locator('.brand-logo').evaluate((image) => image.complete && image.naturalWidth > 0)) throw new Error('Logo not rendered')
      await page.screenshot({ path: `output/playwright/go-console-sidebar-${viewport.width}.png`, fullPage: true, animations: 'disabled' })
      await page.getByRole('button', { name: '切换工作台', exact: true }).click()
      await page.locator('.brand-name').filter({ hasText: '旺商聊机器人' }).waitFor()
      await page.getByRole('button', { name: '切换工作台', exact: true }).click()
      await page.locator('.brand-name').filter({ hasText: '热群机器人' }).waitFor()
      await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '引擎与升级', exact: true }).scrollIntoViewIfNeeded()
      if (!await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '引擎与升级', exact: true }).isVisible()) throw new Error('Long sidebar cannot reach final section')
      await page.locator('.sidebar-scrim').click({ position: { x: 250, y: 300 } })
    }
    return { passed: true, realWrites: 0, syntheticConfigWrites: writes.length, secretDrafts: false, conflictInputPreserved: true, sidebarSwitch: true, mobileViewports: [320, 390, 647] }
  } finally {
    await page.unroute(pattern, handler)
    await page.evaluate((saved) => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
      for (const [key, value] of Object.entries(saved)) sessionStorage.setItem(key, value)
    }, original)
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.goto(base + '/#overview')
    await page.reload()
  }
}
