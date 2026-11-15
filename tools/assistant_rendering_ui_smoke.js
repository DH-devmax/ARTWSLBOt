// Synthetic APIs only; never contact models or native services.
// prettier-ignore
async (page) => {
  const origin = new URL(page.url()).origin
  const id = n => `d6800000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const now = new Date().toISOString(), actor = id(1), tenant = id(2)
  const permissions = ['overview:read', 'accounts:read', 'groups:read', 'assistant:use',
    'assistant:propose', 'assistant:approve', 'assistant:action_propose', 'assistant:action_approve']
  const check = (condition, reason) => { if (!condition) throw Error(reason) }
  const requests = [], screenshots = []
  const answer = ['## 本群配置总结', '', '**查询完成**，尚未执行任何动作。', '',
    '- 读取群配置', '- 等待本人确认', '', '1. 核对当前群', '2. 保存建议', '',
    '| 模块 | 配置字段 | 状态 | 影响 |', '| --- | --- | --- | --- |',
    '| 拦截词 | `words`、`enabled` | 已保存 | 不代表业务已启用 |',
    '| 关键词回复 | `rules`、`admin_only` | 待确认 | 仅当前授权群 |', '',
    '> 此处是查询快照，不是执行回执。', '', '```json',
    '{"sample":"abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"}', '```', '',
    '[项目说明](https://example.com/docs)', '',
    '<img src="https://example.com/leak" onerror="window.__assistantInjected=true">',
    '<script>window.__assistantInjected=true</script>',
    '[危险链接](javascript:alert(1))', '![禁用图片](https://example.com/pixel)'].join('\n')
  let serial = 100, live
  const accounts = ['wsl', 'lajiaoliao'].map((platform, index) => ({
    id: id(10 + index), name: '合成账号', native_nickname: '合成机器人',
    platform, workspace: platform, purpose: 'management'
  }))
  const group = platform => ({ group_id: id(platform === 'wsl' ? 20 : 30), name: '测试群',
    platform, active: true, primary_account_id: accounts.find(a => a.platform === platform).id,
    robot_nickname: '合成机器人', native_id: `synthetic-${platform}` })
  const scope = platform => ({ platform, kind: 'group', group_id: group(platform).group_id,
    account_id: group(platform).primary_account_id })
  const sessions = ['wsl', 'lajiaoliao'].map((platform, index) => ({
    id: id(40 + index), scope: scope(platform), title: '富文本与工具顺序', is_empty: false, created_at: now
  }))
  const card = (type, platform, data) => ({ type, scope: scope(platform), sampled_at: now,
    verification: 'query_snapshot', truncated: false, data })
  const cards = platform => [
    card('get_bot_status', platform, [{ name: '合成机器人', runtime_state: 'online', authorized_groups: 1 }]),
    card('get_group_config', platform, { version: 3, values: { blocked_words: { words: ['测试词'], enabled: false } } }),
    card('find_group_members', platform, [{ id: 'synthetic-member', name: '合成成员', role: 'member', token: 'synthetic-token' }]),
    card('propose_group_action', platform, { id: id(70), summary: '待本人确认，尚未发送。' })
  ]
  const turns = platform => [{ id: id(platform === 'wsl' ? 50 : 60), session_id: sessions.find(s => s.scope.platform === platform).id,
    input: '查看群配置并整理建议', status: 'succeeded', result_code: '', answer, cards: cards(platform), cursor: 4,
    outcomes: [{ plan_id: id(80), kind: 'action', status: 'unknown',
      summary: '发送结果未知，保留原操作编号，不会自动重发。', operation_id: id(81), evidence_state: 'unknown' }]
  }, { id: id(platform === 'wsl' ? 51 : 61), session_id: sessions.find(s => s.scope.platform === platform).id,
    input: '失败示例', status: 'failed', result_code: 'assistant_scope_unavailable', answer: '', cards: cards(platform).slice(0, 1), cursor: 1 }]
  await page.route('**/*', async route => {
    const request = route.request(), url = new URL(request.url())
    if (url.origin !== origin) { requests.push(url.href); return route.abort('blockedbyclient') }
    if (!url.pathname.startsWith('/api/')) return route.fallback()
    const path = url.pathname.replace('/api/v1', ''), platform = request.headers()['x-workspace'] || 'wsl'
    let data = [], status = 200
    if (request.method() !== 'GET') {
      check(request.method() === 'POST' && /^\/assistant\/sessions\/[^/]+\/turns$/.test(path),
        `Unexpected mutation: ${request.method()} ${path}`)
      live = { id: id(serial++), session_id: path.split('/')[3], input: request.postDataJSON().input,
        status: 'queued', answer: '', cards: [], cursor: 0, result_code: '' }
      data = live; status = 202
    } else if (path === '/me') data = { user: { id: actor, tenant_id: tenant, username: 'synthetic-render-user', role: 'custom' }, permissions }
    else if (path === '/workspaces') data = { items: [
      { id: 'wsl', name: '旺商聊机器人' }, { id: 'lajiaoliao', name: '辣椒聊机器人' }
    ] }
    else if (path === '/accounts') data = accounts.filter(a => a.platform === platform)
    else if (path === '/groups') data = [group(platform)]
    else if (path === '/assistant/context') data = { platform, groups: [group(platform)], permissions, total: 1, next_offset: -1 }
    else if (path === '/assistant/usage') data = { unit: 'micro_credit', used_or_reserved: 0, timezone: 'Asia/Shanghai' }
    else if (path === '/assistant/sessions') data = sessions.filter(s => s.scope.platform === platform)
    else if (/^\/assistant\/sessions\/[^/]+\/transcript$/.test(path)) data = [...turns(platform),
      ...(live?.session_id === path.split('/')[3] ? [live] : [])]
    else if (/^\/assistant\/turns\/[^/]+\/events$/.test(path)) {
      const entries = [
        { kind: 'text_delta', data: { text: '## 流式总结\n\n先到达的文字也应排在工具过程之后。' } },
        { kind: 'tool_started', data: { name: 'get_bot_status' } },
        ...cards(platform).slice(0, 2).map(item => ({ kind: 'tool_result', data: item })),
        { kind: 'turn_finished', data: { status: 'succeeded', code: '' } }
      ].map((event, index) => ({ ...event, turn_id: live.id, seq: index + 1 }))
      live.status = 'succeeded'; live.answer = entries[0].data.text; live.cards = cards(platform).slice(0, 2)
      return route.fulfill({ contentType: 'text/event-stream; charset=utf-8',
        body: entries.map(e => `id: ${e.seq}\nevent: ${e.kind}\ndata: ${JSON.stringify(e)}\n\n`).join('') })
    } else if (/^\/assistant\/turns\/[^/]+$/.test(path)) data = live
    await route.fulfill({ status, json: { code: 0, msg: 'ok', data } })
  })
  for (const platform of ['wsl', 'lajiaoliao']) {
    for (const width of [320, 390, 768, 1440]) {
      live = undefined
      await page.setViewportSize({ width, height: 900 })
      await page.goto(origin + '/#assistant')
      await page.reload()
      const region = page.getByRole('region', { name: '助手会话工作台' })
      await region.getByRole('radiogroup', { name: '助手平台', exact: true }).locator('label')
        .filter({ hasText: platform === 'wsl' ? '旺商聊' : '辣椒聊' }).click()
      const chooser = region.locator('.assistant-mobile-groups')
      if (await chooser.isVisible() && await chooser.getAttribute('aria-expanded') !== 'true') await chooser.click()
      await region.getByRole('button', { name: /选择群组 测试群/ }).click()
      const row = region.locator(`[data-turn-id="${turns(platform)[0].id}"]`)
      const markdown = row.locator('.assistant-markdown'), details = row.locator('details')
      await markdown.locator('h2').getByText('本群配置总结', { exact: true }).waitFor()
      check(await details.getAttribute('open') === null, 'History tool process was not collapsed')
      check(!(await details.locator('.assistant-result').first().isVisible()), 'Collapsed query cards remained visible')
      check(await row.getByRole('button', { name: '查看动作与影响', exact: true }).isVisible(), 'Confirmation hidden inside process')
      check(await row.getByRole('button', { name: '选择成员', exact: true }).isVisible(), 'Member choice hidden inside process')
      check(await row.locator('.assistant-outcome-summary').isVisible(), 'Unknown outcome hidden')
      check(await region.locator(`[data-turn-id="${turns(platform)[1].id}"] .el-alert--error`).isVisible(), 'Failure hidden')
      check(await markdown.locator('strong, ul, ol, blockquote, pre code, table, th').count() >= 9, 'Markdown structures missing')
      check(await markdown.locator('script, img, iframe, [onerror], a[href^="javascript:"]').count() === 0, 'Unsafe Markdown became active DOM')
      check(await markdown.locator('a').first().getAttribute('rel') === 'noopener noreferrer', 'External link missing isolation')
      check(await row.evaluate(el => {
        const process = el.querySelector('details'), text = el.querySelector('.assistant-model-answer')
        return !!(process.compareDocumentPosition(text) & Node.DOCUMENT_POSITION_FOLLOWING)
      }), 'Final answer preceded tools')
      await details.locator('summary').click()
      const labels = await details.locator('.assistant-tool-step-label').allTextContents()
      check(labels.join('|') === '1. 账号状态|2. 群配置|3. 选择群成员|4. 群管动作建议', 'Tool order changed')
      await details.locator('summary').click()
      await details.locator('summary').focus()
      await page.keyboard.press('Enter')
      check(await details.getAttribute('open') !== null, 'Keyboard could not expand tool process')
      await page.keyboard.press('Enter')
      check(await details.getAttribute('open') === null, 'Keyboard could not collapse tool process')
      await details.scrollIntoViewIfNeeded()
      const processScreenshot = `output/playwright/assistant-process-${platform}-${width}.png`
      await page.screenshot({ path: processScreenshot, fullPage: true }); screenshots.push(processScreenshot)
      const dimensions = await markdown.evaluate(el => {
        const table = el.querySelector('.assistant-table-scroll'), code = el.querySelector('pre')
        return { viewport: document.documentElement.clientWidth, page: document.documentElement.scrollWidth,
          table: table.clientWidth, tableScroll: table.scrollWidth, parent: el.clientWidth, code: code.clientWidth,
          injected: !!window.__assistantInjected }
      })
      check(dimensions.page <= dimensions.viewport + 1, `Page overflow at ${platform}/${width}: ${JSON.stringify(dimensions)}`)
      check(dimensions.table <= dimensions.parent && dimensions.code <= dimensions.parent, 'Rich content escaped its container')
      if (width <= 390) check(dimensions.tableScroll > dimensions.table, 'Mobile table did not get local scrolling')
      check(!dimensions.injected, 'Raw HTML executed')
      await markdown.scrollIntoViewIfNeeded()
      const screenshot = `output/playwright/assistant-rendering-${platform}-${width}.png`
      await page.screenshot({ path: screenshot, fullPage: true }); screenshots.push(screenshot)
    }
  }
  const region = page.getByRole('region', { name: '助手会话工作台' })
  await region.getByRole('textbox', { name: '助手问题', exact: true }).fill('流式顺序验收')
  await region.getByRole('button', { name: '发送', exact: true }).click()
  const streamed = region.locator('.assistant-turn').last()
  await streamed.locator('.assistant-markdown h2').getByText('流式总结').waitFor()
  await streamed.locator('.assistant-turn-state').getByText('本轮结束', { exact: true }).waitFor()
  check(await streamed.locator('details').getAttribute('open') === null, 'Streaming process expanded itself')
  check(await streamed.evaluate(el => !!(el.querySelector('details').compareDocumentPosition(
    el.querySelector('.assistant-model-answer')) & Node.DOCUMENT_POSITION_FOLLOWING)), 'Streaming text preceded tools')
  check(requests.length === 0, `Markdown triggered external requests: ${requests.join(',')}`)
  return { platforms: 2, viewports: [320, 390, 768, 1440], safe_markdown: true,
    tools_collapsed: true, actual_tool_order: true, final_summary_last: true, decisions_visible: true, screenshots }
}
