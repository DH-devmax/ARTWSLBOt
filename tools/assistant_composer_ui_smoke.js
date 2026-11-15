// Synthetic APIs only. Opening help and editing text must never call a model or native service.
// prettier-ignore
async (page) => {
  const origin = new URL(page.url()).origin
  const id = n => `d6900000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const check = (condition, message) => { if (!condition) throw Error(message) }
  const permissions = ['overview:read', 'accounts:read', 'groups:read', 'assistant:use', 'assistant:propose', 'assistant:approve']
  const accounts = ['wsl', 'lajiaoliao'].map((platform, i) => ({ id: id(10+i), name: '合成机器人', native_nickname: '合成机器人', platform, workspace: platform, purpose: 'management' }))
  const group = platform => ({ group_id: id(platform === 'wsl' ? 20 : 30), name: platform === 'wsl' ? '旺商测试群' : '辣椒测试群', platform, active: true, primary_account_id: accounts.find(a => a.platform === platform).id, robot_nickname: '合成机器人' })
  const scope = platform => ({ platform, kind: 'group', group_id: group(platform).group_id, account_id: group(platform).primary_account_id })
  const context = platform => ({ platform, scope: scope(platform), groups: [group(platform)], permissions, total: 1, next_offset: -1, sampled_at: new Date().toISOString(),
    capabilities: [
      { id: 'workspace.query.get_group_capabilities', name: '群与机器人查询', tool: 'get_group_capabilities', category: 'query', business_permission: 'groups:read', enabled: true, implemented: true },
      { id: 'blocked_words.config_propose', name: platform === 'wsl' ? '旺商拦截词' : '辣椒拦截词', tool: 'propose_group_config_change', category: 'config_propose', business_permission: 'assistant:propose', enabled: true, implemented: true },
      { id: 'moderation.action_propose.send_text', name: '发送群文本', tool: 'propose_group_action', action: 'send_text', category: 'action_propose', business_permission: 'group_action:send_text', enabled: false, implemented: true, blocked_reason: 'assistant_module_closed' }
    ], actions: [{ id: 'send_text', production_admitted: false, block_reason: 'assistant_module_closed' }] })
  const mutations = [], foreign = [], measurements = [], screenshots = []
  let delay = false, release, delayed = false
  await page.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url())
    if (url.origin !== origin) { foreign.push(url.href); return route.abort('blockedbyclient') }
    if (!url.pathname.startsWith('/api/')) return route.fallback()
    const path = url.pathname.replace('/api/v1', ''), platform = request.headers()['x-workspace'] || 'wsl'
    if (request.method() !== 'GET') { mutations.push(path); return route.fulfill({ status: 500, json: { code: 1, msg: 'unexpected_mutation' } }) }
    let data = []
    if (path === '/me') data = { user: { id: id(1), tenant_id: id(2), username: 'synthetic-composer', role: 'custom' }, permissions }
    else if (path === '/workspaces') data = { items: [{ id: 'wsl', name: '旺商聊' }, { id: 'lajiaoliao', name: '辣椒聊' }] }
    else if (path === '/accounts') data = accounts.filter(a => a.platform === platform)
    else if (path === '/groups') data = [group(platform)]
    else if (path === '/assistant/context') {
      data = context(platform)
      if (delay && url.searchParams.get('limit') === '1') { delay = false; delayed = true; await new Promise(resolve => { release = resolve }) }
    } else if (path === '/assistant/usage') data = { unit: 'micro_credit', used_or_reserved: 82666000 }
    try { await route.fulfill({ json: { code: 0, msg: 'ok', data } }) } catch (error) { if (!delayed) throw error }
  })
  const choose = async platform => {
    const region = page.getByRole('region', { name: '助手会话工作台' })
    await region.getByRole('radiogroup', { name: '助手平台', exact: true }).locator('label').filter({ hasText: platform === 'wsl' ? '旺商聊' : '辣椒聊' }).click()
    const button = region.locator('.assistant-mobile-groups')
    if (await button.isVisible() && await button.getAttribute('aria-expanded') !== 'true') await button.click()
    await region.getByRole('button', { name: new RegExp('选择群组 ' + group(platform).name) }).click()
    return region
  }
  for (const platform of ['wsl', 'lajiaoliao']) for (const width of [320, 390, 768, 1440]) {
    await page.setViewportSize({ width, height: width === 320 ? 600 : 779 })
    await page.goto(origin + '/#assistant'); await page.reload()
    const region = await choose(platform), input = region.getByRole('textbox', { name: '助手问题', exact: true })
    await input.fill('')
    await region.locator('.assistant-usage').waitFor()
    const initial = await region.evaluate(el => ({
      header: el.querySelector('header').getBoundingClientRect().height, sidebar: el.querySelector('aside').getBoundingClientRect().height,
      footer: el.querySelector('footer').getBoundingClientRect().height, page: document.documentElement.scrollWidth, viewport: document.documentElement.clientWidth,
      input: el.querySelector('textarea').clientHeight
    }))
    check(initial.page <= initial.viewport + 1, 'Composer caused horizontal page overflow')
    check(initial.footer <= 120, `Empty footer too tall: ${JSON.stringify(initial)}`)
    if (width < 768) check(initial.header + initial.sidebar <= 100, `Mobile heading too tall: ${JSON.stringify(initial)}`)
    check(await region.locator('.assistant-permissions').count() === 0, 'Persistent permission labels remain')
    const info = region.getByRole('button', { name: '助手能力说明', exact: true }), refresh = region.getByRole('button', { name: '重新读取', exact: true })
    check((await info.boundingBox()).x < (await refresh.boundingBox()).x, 'Help icon is not before refresh')
    const heights = []
    for (const lines of [1, 3, 5, 8]) {
      await input.fill(Array.from({ length: lines }, () => '输入测试').join(String.fromCharCode(10)))
      await page.waitForFunction(n => { const el = document.querySelector('.assistant-sender textarea'); return el?.value.split(String.fromCharCode(10)).length === n }, lines)
      await page.waitForTimeout(50)
      heights.push(await input.evaluate(el => ({ height: el.clientHeight, scroll: el.scrollHeight, line: Number.parseFloat(getComputedStyle(el).lineHeight) })))
    }
    check(heights[1].height > heights[0].height && heights[2].height > heights[1].height, 'Input did not grow through five lines')
    check(Math.abs(heights[3].height - heights[2].height) <= 2 && heights[3].scroll > heights[3].height, 'Input beyond five lines did not scroll locally')
    check(heights[3].height <= heights[3].line * 5 + 4, 'Input exceeds five visible lines')
    await input.fill('')
    await info.click()
    const dialog = page.getByRole('dialog', { name: '助手能力说明', exact: true })
    await dialog.getByText(platform === 'wsl' ? '旺商拦截词' : '辣椒拦截词', { exact: true }).waitFor()
    check((await dialog.locator('.assistant-help-scope').innerText()).includes('合成机器人'), 'Bound robot nickname was replaced with an unbound fallback')
    await dialog.evaluate(async el => {
      for (let node = el; node; node = node.parentElement)
        await Promise.all(node.getAnimations().filter(animation => animation.effect?.getTiming().iterations !== Infinity)
          .map(animation => animation.finished.catch(() => {})))
    })
    check(await dialog.getByText('发送群文本', { exact: true }).isVisible(), 'Blocked action missing')
    check(await dialog.getByText('本人未获此项权限', { exact: true }).isVisible(), 'Missing action permission was not explained')
    check((await dialog.innerText()).includes('不自动确认') && (await dialog.innerText()).includes('不扣助手积分'), 'Boundaries missing')
    const closeBox = await dialog.getByRole('button', { name: '知道了', exact: true }).boundingBox()
    check(closeBox.y + closeBox.height <= page.viewportSize().height, 'Help close action fell below the viewport')
    if (width === 320) {
      const helpShot = `output/playwright/assistant-help-${platform}-${width}.png`
      await page.screenshot({ path: helpShot, fullPage: true }); screenshots.push(helpShot)
    }
    const shot = `output/playwright/assistant-composer-${platform}-${width}.png`
    await dialog.getByRole('button', { name: '知道了', exact: true }).click()
    await dialog.waitFor({ state: 'hidden' })
    await page.screenshot({ path: shot, fullPage: true }); screenshots.push(shot)
    measurements.push({ platform, width, initial, heights })
  }
  await page.setViewportSize({ width: 390, height: 779 })
  const region = await choose('wsl')
  delay = true
  await region.getByRole('button', { name: '助手能力说明', exact: true }).click()
  await page.waitForFunction(() => document.querySelector('.assistant-help-dialog .el-skeleton'))
  await page.keyboard.press('Escape')
  await page.getByRole('dialog', { name: '助手能力说明', exact: true }).waitFor({ state: 'hidden' })
  await choose('lajiaoliao')
  await region.getByRole('button', { name: '助手能力说明', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '助手能力说明', exact: true })
  await dialog.getByText('辣椒拦截词', { exact: true }).waitFor()
  check(delayed && release, 'Late response scenario did not begin')
  release(); await page.waitForTimeout(100)
  check(!(await dialog.innerText()).includes('旺商拦截词'), 'Late help response crossed platform scope')
  await dialog.getByRole('button', { name: '知道了', exact: true }).click()
  check(mutations.length === 0 && foreign.length === 0, `Unexpected requests: ${JSON.stringify({ mutations, foreign })}`)
  return { platforms: 2, viewports: [320, 390, 768, 1440], max_visible_lines: 5, readonly_help: true, late_scope_isolated: true, mutations, measurements, screenshots }
}
