// Synthetic conversations only. No native messages or credentials are used.
async (page) => {
  const base = 'http://127.0.0.1:5173'
  const storage = await page.evaluate(() => Object.fromEntries(
    Object.entries(sessionStorage).filter(([key]) => key.startsWith('wsl:'))
  )).catch(() => ({}))
  const a = '81000000-0000-4000-8000-000000000001'
  const b = '81000000-0000-4000-8000-000000000002'
  const key = 'a'.repeat(64), privateKey = 'b'.repeat(64), now = new Date().toISOString()
  const accounts = [{ id: a, name: '消息机器人甲', workspace: 'wsl' }, { id: b, name: '消息机器人乙', workspace: 'wsl' }]
  const me = {
    user: { id: '82000000-0000-4000-8000-000000000001', username: 'synthetic', role: 'superadmin' },
    permissions: ['overview:read', 'accounts:read', 'groups:read', 'assistant:use']
  }
  let viewer = false, blocked = false, writes = 0, hold = false, release, seen, recallReads = 0
  const operationID = '83000000-0000-4000-8000-000000000001'
  const unknownOperation = { id: operationID, kind: 'send_text', status: 'unknown', result_code: 'native_text_result_unknown' }
  const operationPath = `/accounts/${a}/native-text-operations/${operationID}`
  const untrustedDiagnostic = '<img src=x onerror=alert(1)> SYNTHETIC_EVIDENCE_UNTRUSTED'
  let evidenceFixture = false, evidenceState = 'conversation_not_returned', evidenceReads = 0, operationReads = 0
  let holdEvidence = false, evidenceSeen, releaseEvidence
  const content = {
    message: { sender: { id: 7, name: '合成发言者' }, target: { id: 123 }, format: 0,
      text: '<img src=x onerror=alert(1)> 合成消息', mentions: [{ uid: 8, name: '测试成员' }], quote: { text: '合成引用' } },
    native_format: '100', mapping: 'unverified'
  }
  const conversation = { key, scene: 'group', title: '合成对话甲', business_id: '123',
    mapping: 'directory_matched', count: 10, last_at: now, last: content }
  const privateContent = { ...content, message: { ...content.message, text: '合成私聊内容', quote: null, mentions: [] } }
  const health = () => ({
    status: 'degraded', sampled_at: now, total: 12, pending: 0, processing: 0, decoded: 10,
    unsupported: 1, failed: 1, recent_hour: { unsupported: 0, failed: 1 }, transport_notices: 2,
    oldest_pending_at: null, decode_p95_ms: 1.2, processed_60s: 12,
    reasons: { authentication: 1, native_format: 1 },
    pool: { active: 0, workers: 4, queue_depth: 0, queue_capacity: 16, observed: true },
    receiver: { observed: true, enabled: true, snapshot: { state: blocked ? 'blocked' : 'connected',
      error: blocked ? 'native_push_boundary_unknown' : '', blocked_packet: blocked ? '4/1/184' : '',
      messages: 12, acks: 12, queue_depth: 0 } }, native_ready: false, ai_ready: false
  })
  const handler = async (route) => {
    const request = route.request(), url = new URL(request.url())
    const path = url.pathname.replace('/api/v1', ''), id = path.split('/')[2]
    let data = []
    if (path.includes('/native-text-operations/')) {
      if (!evidenceFixture || request.headers()['x-workspace'] !== 'wsl' ||
          ![operationPath, `${operationPath}/reconcile`].includes(path))
        throw new Error('Evidence lookup escaped the synthetic operation scope')
      if (path === `${operationPath}/reconcile` && request.method() === 'POST') {
        if (request.postData() !== '{}') throw new Error('Evidence lookup submitted send parameters')
        evidenceReads++
        const check = { state: evidenceState, durable_records: 2, sequence_heads: 3,
          conversations: 1, requested: 50, returned: 4, sampled_at: now,
          reason: untrustedDiagnostic, message: untrustedDiagnostic }
        if (holdEvidence) {
          holdEvidence = false
          evidenceSeen()
          await new Promise(resolve => { releaseEvidence = resolve })
        }
        await route.fulfill({ json: { code: 0, data: { ...unknownOperation, evidence_check: check }, msg: '' } })
        return
      }
      if (path === operationPath && request.method() === 'GET') {
        operationReads++
        await route.fulfill({ json: { code: 0, data: unknownOperation, msg: '' } })
        return
      }
      throw new Error('Unexpected synthetic operation request')
    }
    if (request.method() !== 'GET') {
      if (request.method() !== 'POST' || !path.endsWith('/directory-refresh')) throw new Error('Unexpected native write')
      writes++
      await route.fulfill({ status: 202, json: { code: 0, data: { state: 'queued' } } })
      return
    }
    if (path.endsWith('/events')) {
      await route.fulfill({ contentType: 'text/event-stream', body:
        `id: 1\nevent: update\ndata: ${JSON.stringify({ account_id: id, workspace: 'wsl', health: health(), reset: false })}\n\n` })
      return
    }
    if (path === '/me') data = viewer ? { user: { ...me.user, role: 'user', tenant_id: 'tenant' }, permissions: me.permissions } : me
    else if (path === '/workspaces') data = { items: [{ id: 'user', name: '用户中心' }, { id: 'wsl', name: '旺商聊机器人' }] }
    else if (path === '/accounts') data = accounts
    else if (path === '/overview') data = { accounts: 2, groups: 0, limits: { accounts: 10, groups: 100, ai_groups: 100 } }
    else if (path === '/user-center') data = { subscription: null, groups: [], claims: [], operations: [], personas: [], permissions: me.permissions, token_usage: null }
    else if (path.endsWith('/decoder-health')) data = health()
    else if (path.endsWith('/sending')) {
      const pending = evidenceFixture && id === a && path.includes(`/conversations/${key}/`)
      data = { enabled: pending, mode: 'isolated_test',
        reason: pending ? '' : 'native_text_acceptance_pending', max_text_bytes: 3000,
        scope: { account_epoch: 1, binding_epoch: 1 },
        recent: pending ? [unknownOperation] : [],
        test_windows: pending ? [{ id: 'synthetic-window', remaining: 1, actions: ['send_text'] }] : [] }
    }
    else if (path.endsWith('/conversations')) {
      if (hold && id === a) { hold = false; seen(); await new Promise(resolve => { release = resolve }) }
      const all = [conversation, { ...conversation, key: privateKey, scene: 'private', title: '合成私聊', business_id: '7', last: privateContent }]
      const rows = id === a ? all.filter(item => !url.searchParams.get('scene') || item.scene === url.searchParams.get('scene')) : []
      data = { items: rows, total: rows.length, directory: { sampled_at: now, stale: false, error: '',
        groups: id === a ? [{ id: '123', team: '456', name: '合成原生群', role: 'unknown' }] : [] }, directory_total: id === a ? 1 : 0 }
    } else if (path.endsWith('/recalls')) {
      if (id !== a || !path.includes(key) || request.headers()['x-workspace'] !== 'wsl')
        throw new Error('Recall read lost account/conversation scope')
      recallReads++
      data = { items: [{ id: '43:0', received_at: now,
        notice: { client_id: 'synthetic-original-id', actor: '<img src=x onerror=alert(1)>' } }],
        scanned: 12, next_before: '', read_only: true }
    } else if (path.endsWith('/messages')) {
      const body = path.includes(privateKey) ? privateContent : content
      data = { items: [
        { id: '48', received_at: now, state: 'unsupported', reason: 'lajiaoliao_content_type',
          content: { ...body, platform: 'lajiaoliao', native_format: '1204' },
          native_view: { kind: 'friend_notice', content_type: 1204, friend_notice: {
            event: 'friend_added', sender: { id: 'synthetic-sender', name: '<img src=x onerror=alert(1)>' },
            recipient: { id: 'synthetic-recipient', name: '合成好友' }
          } } },
        { id: '47', received_at: now, state: 'unsupported', reason: 'lajiaoliao_content_type',
          content: { ...body, platform: 'lajiaoliao', native_format: '2200' },
          native_view: { kind: 'system_notice', content_type: 2200,
            system_notice: { event: 'read_receipt' } } },
        { id: '46', received_at: now, state: 'unsupported', reason: 'lajiaoliao_content_type',
          content: { ...body, platform: 'lajiaoliao', native_format: '1299',
            message: { ...body.message, format: 0, text: '', quote: null, mentions: [] } } },
        { id: '44', received_at: now, state: 'decoded', content: { ...body,
          message: { ...body.message, text: '合成视频引用回复', mentions: [],
            quote: { client_id: 'synthetic-video-client', server_id: '100', text: '' } } } },
        { id: '42', received_at: now, state: 'decoded', content: body },
        { id: '37', received_at: now, state: 'decoded', content: { ...body, mapping: 'transport_notice',
          message: { sender: { id: 0, name: '系统通知' }, target: { id: 123 }, format: 7,
            notice: { kind: 'group_notification', verification: 'transport',
              text: '<img src=x onerror=alert(1)> 合成群通知' } } } },
        { id: '36', received_at: now, state: 'decoded', content: { ...body,
          message: { ...body.message, text: '', quote: null, mentions: [], format: 1,
            image: { width: 10000, height: 10000, size: 10000000, preview_limited: true } } } },
        { id: '35', received_at: now, state: 'decoded', content: { ...body, server_id: '100',
          message: { ...body.message, client_id: 'synthetic-video-client', text: '', quote: null, mentions: [],
            format: 3, video: { asset: '123', width: 720, height: 1280, duration: 12, size: 100 } } } },
        ...[41, 40, 39, 38].map((value, index) => ({
          id: String(value), received_at: new Date(Date.now() - (index + 1) * 60000).toISOString(), state: 'decoded',
          content: { ...body, message: { ...body.message, sender: { id: 10 + index, name: ['测试成员', '消息检查员', '测试成员', '会话记录员'][index] },
            text: ['收到，这里是合成测试会话。', '长消息与换行也要保持完整显示。\n不会作为网页内容执行。', '群聊与私聊分开显示。', '开始本次消息展示检查。'][index],
            quote: null, mentions: [] } }
        }))
      ], next_before: '' }
    }
    await route.fulfill({ json: { code: 0, data, msg: '' } })
  }
  const choose = async name => {
    await page.getByRole('combobox', { name: '机器人面板', exact: true })
      .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option').filter({ hasText: name }).click()
  }
  const settleSidebar = async width => {
    if (width <= 760 && await page.locator('.sidebar.open').count()) await page.locator('.sidebar-scrim').click()
    if (width <= 760) await page.waitForFunction(() => document.querySelector('.sidebar').getBoundingClientRect().right <= 1)
  }
  await page.route('**/api/v1/**', handler)
  try {
    await page.goto(base)
    await page.evaluate(() => { for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key) })
    await page.setViewportSize({ width: 1440, height: 960 })
    await page.goto(base + '/#messages'); await page.reload()
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.addStyleTag({ content: '*,*::before,*::after{transition:none!important;animation:none!important}' })
    const center = page.getByTestId('message-center')
    await center.getByText('请在顶部选择一个机器人账号', { exact: true }).waitFor()
    await choose('消息机器人甲')
    await center.getByRole('button').filter({ hasText: '合成对话甲' }).click()
    await center.locator('article').getByText(content.message.text, { exact: true }).waitFor()
    await center.locator('blockquote').getByText('视频', { exact: true }).waitFor()
    await center.locator('article').getByText('<img src=x onerror=alert(1)> 合成群通知', { exact: true }).waitFor()
    await center.locator('article').getByText('原生通知 · 只读', { exact: true }).waitFor()
    await center.locator('article').getByText('已添加好友', { exact: true }).waitFor()
    await center.locator('article').getByText('回执通知', { exact: true }).waitFor()
    await center.locator('article').getByText(/辣椒聊.*1299/).waitFor()
    await center.locator('article').getByText('图片 · 超出预览限制，消息已保留', { exact: true }).waitFor()
    if (await center.locator('article img').count()) throw new Error('Untrusted message became HTML')
    if (recallReads) throw new Error('Recall scan ran before an explicit read')
    await center.getByRole('button', { name: '撤回通知', exact: true }).click()
    const recalls = page.getByRole('dialog', { name: '撤回通知', exact: true })
    await recalls.getByText('原消息 · synthetic-original-id', { exact: true }).waitFor()
    await recalls.getByText('操作账号 · <img src=x onerror=alert(1)>', { exact: true }).waitFor()
    if (await recalls.locator('img').count() || recallReads !== 1 || writes)
      throw new Error('Recall read rendered HTML or submitted a native action')
    for (const width of [320, 390, 647, 1440]) {
      await page.setViewportSize({ width, height: 779 })
      await recalls.evaluate(el => Promise.all(el.getAnimations({ subtree: true })
        .filter(a => a.effect?.getComputedTiming().iterations !== Infinity).map(a => a.finished.catch(() => {}))))
      if (!await recalls.evaluate(el => el.scrollWidth <= el.clientWidth + 1 && el.getBoundingClientRect().left >= 0 &&
        el.getBoundingClientRect().right <= innerWidth + 1 && el.getBoundingClientRect().bottom <= innerHeight + 1))
        throw new Error('Recall dialog overflow ' + width)
      await page.screenshot({ path: `output/playwright/native-recalls-${width}.png`, fullPage: true })
    }
    await recalls.getByRole('button', { name: '刷新撤回通知', exact: true }).click()
    await recalls.getByText('本次核对 12 条原始记录，扫描有界', { exact: true }).waitFor()
    await recalls.getByRole('button', { name: '关闭', exact: true }).click()
    if (recallReads !== 2 || writes) throw new Error('Recall refresh was missing or performed a write')
    await center.locator('article').getByText(content.message.text, { exact: true }).waitFor()
    await page.setViewportSize({ width: 1440, height: 960 })
    if (await center.getByRole('tab', { name: '解码健康', exact: true }).count()) throw new Error('Health still occupies a full tab')
    const monitor = center.getByRole('button', { name: '解码监控', exact: true })
    const details = page.getByRole('region', { name: '解码器健康详情', exact: true })
    if (await details.isVisible()) throw new Error('Monitoring is not collapsed by default')
    await center.getByRole('tab', { name: '原生群组 · 1', exact: true }).click()
    await center.locator('.mc-group-name').getByText('合成原生群', { exact: true }).waitFor()
    await center.getByRole('button', { name: '同步群目录', exact: true }).click()
    if (writes !== 1) throw new Error('Directory request was not coalesced')
    await page.getByText('群目录已进入后台同步队列', { exact: true }).waitFor({ state: 'visible' })
    await page.getByText('群目录已进入后台同步队列', { exact: true }).waitFor({ state: 'hidden' })
    await center.getByRole('tab', { name: '对话消息', exact: true }).click()
    await center.getByRole('button').filter({ hasText: '合成私聊' }).click()
    await center.locator('article').getByText('合成私聊内容', { exact: true }).waitFor()
    await center.getByRole('button').filter({ hasText: '合成对话甲' }).click()
    await center.locator('article').getByText(content.message.text, { exact: true }).waitFor()
    blocked = true
    await center.getByRole('button', { name: '刷新消息中心', exact: true }).click()
    await monitor.getByText('接收暂停', { exact: true }).waitFor()
    await page.evaluate(() => { if (document.activeElement instanceof HTMLElement) document.activeElement.blur() })
    await page.mouse.move(0, 0)
    await page.getByText('刷新消息与健康指标', { exact: true }).waitFor({ state: 'hidden' })
    for (const size of [{ width: 320, height: 740 }, { width: 390, height: 844 }, { width: 647, height: 779 }, { width: 1440, height: 960 }]) {
      await page.setViewportSize(size)
      await settleSidebar(size.width)
      await page.mouse.move(0, 0)
      await page.waitForTimeout(250)
      const fits = await center.evaluate(el => {
        const r = el.getBoundingClientRect()
        const monitor = el.querySelector('[data-testid="decoder-monitor"]').getBoundingClientRect()
        return r.left >= 0 && r.right <= innerWidth && r.bottom <= innerHeight &&
          monitor.top - r.top < 60 && monitor.right <= r.right &&
          document.documentElement.scrollWidth <= innerWidth && document.documentElement.scrollHeight <= innerHeight
      })
      if (!fits) throw new Error(`Native-style messages do not fit viewport: ${size.width}`)
      if (size.width <= 760) {
        const mobileShell = await center.evaluate(el => {
          const header = document.querySelector('.topbar').getBoundingClientRect()
          const account = document.querySelector('.account-select').getBoundingClientRect()
          const more = document.querySelector('.mobile-overflow').getBoundingClientRect()
          const style = getComputedStyle(el), r = el.getBoundingClientRect()
          const bubble = getComputedStyle(el.querySelector('.mc-bubble'))
          return header.height <= 56 && account.top >= header.top && account.bottom <= header.bottom &&
            more.top >= header.top && more.bottom <= header.bottom && account.right <= more.left &&
            r.left === 0 && r.right === innerWidth && style.borderTopWidth === '0px' &&
            style.borderRadius === '0px' && bubble.borderTopWidth === '0px' &&
            el.querySelector('.mc-thread-heading').getBoundingClientRect().height <= 52 &&
            !el.querySelector('.mc-tabs').getBoundingClientRect().height
        })
        if (!mobileShell) throw new Error(`Mobile shell is still crowded or framed: ${size.width}`)
        if (await page.locator('.topbar-actions').getByRole('button', { name: '群管助手', exact: true }).count())
          throw new Error('Business assistant leaked into administrative message center')
        await page.getByRole('button', { name: '更多操作', exact: true }).click()
        const menu = page.getByRole('menu')
        await menu.getByRole('menuitem', { name: /^(账号登录|注册 \/ 登录)$/ }).waitFor()
        for (const name of ['机器人连接状态', '刷新', '退出登录'])
          await menu.getByRole('menuitem', { name, exact: true }).waitFor()
        await page.screenshot({ path: `output/playwright/messages-menu-${size.width}.png` })
        await menu.getByRole('menuitem', { name: '机器人连接状态', exact: true }).click()
        const connections = page.getByRole('dialog', { name: '机器人连接状态', exact: true })
        await connections.waitFor()
        await connections.getByRole('button', { name: '消息机器人甲', exact: true }).waitFor()
        await page.waitForFunction(() => {
          const el = document.querySelector('.connection-popover')?.closest('.el-drawer')
          if (!el) return false
          const r = el.getBoundingClientRect()
          return r.left >= 0 && r.right <= innerWidth && r.bottom <= innerHeight
        })
        const drawerFits = await connections.evaluate(el => {
          const r = el.getBoundingClientRect()
          return { fits: r.left >= 0 && r.right <= innerWidth && r.bottom <= innerHeight,
            left: r.left, right: r.right, bottom: r.bottom }
        })
        await page.screenshot({ path: `output/playwright/messages-connections-${size.width}.png` })
        if (!drawerFits.fits) throw new Error(`Mobile connection drawer overflows: ${size.width} ${JSON.stringify(drawerFits)}`)
        await connections.locator('.el-drawer__close-btn').click()
        await connections.waitFor({ state: 'hidden' })
        if (size.width === 320) {
          await page.getByRole('button', { name: '更多操作', exact: true }).click()
          const refreshed = page.waitForRequest(request => new URL(request.url()).pathname === '/api/v1/overview')
          await page.getByRole('menuitem', { name: '刷新', exact: true }).click()
          await refreshed
        }
        if (await center.locator('.mc-list').isVisible()) throw new Error('Mobile thread still stacks below conversation list')
        await center.getByRole('button', { name: '返回会话列表', exact: true }).click()
        await center.getByRole('button').filter({ hasText: '合成对话甲' }).waitFor()
        await center.getByRole('tab', { name: '原生群组 · 1', exact: true }).click()
        await center.locator('.mc-group-name').getByText('合成原生群', { exact: true }).waitFor()
        await center.getByRole('tab', { name: '对话消息', exact: true }).click()
        await page.screenshot({ path: `output/playwright/messages-list-${size.width}.png` })
        await center.getByRole('button').filter({ hasText: '合成对话甲' }).click()
        await center.locator('article').getByText(content.message.text, { exact: true }).waitFor()
      } else {
        if (!await center.locator('.mc-list').isVisible()) throw new Error('Desktop conversation list is missing')
        if (!await page.locator('.native-login-entry').isVisible()) throw new Error('Desktop login entry is missing')
        if (await page.getByRole('button', { name: '更多操作', exact: true }).isVisible()) throw new Error('Mobile menu leaked into desktop')
      }
      await page.screenshot({ path: `output/playwright/messages-thread-${size.width}.png` })
      await monitor.click()
      await details.getByTestId('receive-issue').getByText('消息接收已暂停：协议通知尚未兼容', { exact: true }).waitFor()
      await details.getByText('共享工作池 · 活跃 / 上限', { exact: true }).waitFor()
      const popoverFits = await details.evaluate(el => {
        const r = el.closest('.el-popper').getBoundingClientRect()
        return r.left >= 0 && r.top >= 0 && r.right <= innerWidth && r.bottom <= innerHeight
      })
      if (!popoverFits) throw new Error(`Decode popover overflow: ${size.width}`)
      await page.screenshot({ path: `output/playwright/messages-health-${size.width}.png` })
      await monitor.click()
      await details.waitFor({ state: 'hidden' })
    }
    hold = true
    const pending = new Promise(resolve => { seen = resolve })
    await center.getByRole('button', { name: '刷新消息中心', exact: true }).click()
    await pending
    await choose('消息机器人乙')
    release()
    await center.getByText('暂无已接收的对话', { exact: true }).waitFor()
    if (await center.getByText('合成对话甲', { exact: true }).count()) throw new Error('Late account response leaked')
    evidenceFixture = true
    await page.setViewportSize({ width: 1440, height: 960 })
    await choose('消息机器人甲')
    await center.getByRole('button').filter({ hasText: '合成对话甲' }).click()
    const composer = center.getByRole('region', { name: '消息发送', exact: true })
    const diagnostic = composer.getByRole('status', { name: '原操作回显诊断', exact: true })
    const unknownLabel = '提交结果未知，请核对状态或查询回显，勿重复发送'
    await composer.getByText(unknownLabel, { exact: true }).waitFor()
    await composer.getByRole('textbox', { name: '消息内容', exact: true }).fill('合成未决草稿，不得发送')
    const assertNoResend = async expected => {
      if (evidenceReads !== expected || writes !== 1)
        throw new Error('Evidence query retried itself or submitted a native write')
      if (!await composer.getByRole('button', { name: '发送消息', exact: true }).isDisabled() ||
          await composer.getByRole('button', { name: /重发|重新发送|继续提交/ }).count())
        throw new Error('Unresolved operation exposed a resend path')
    }
    if (await diagnostic.count() || evidenceReads)
      throw new Error('Evidence diagnostics or queries appeared before explicit reconciliation')
    const evidenceCases = [
      ['conversation_not_returned', '本次回显查询未返回目标会话，尚未取得原操作的送达证据。'],
      ['history_empty', '本次查询返回的目标会话历史为空，尚未取得原操作的送达证据。'],
      ['message_not_found', '本次有界查询未匹配到原操作的回显，尚未取得送达证据。'],
      ['scan_truncated', '本次回显查询已达扫描上限，结果不完整，尚未取得原操作的送达证据。'],
      ['matched', '本次查询匹配到原操作的回显记录，仍未取得对方送达或已读的证据。']
    ]
    for (const [index, [state, text]] of evidenceCases.entries()) {
      evidenceState = state
      await composer.getByRole('button', { name: '查询回显', exact: true }).click()
      await diagnostic.getByText(text, { exact: true }).waitFor()
      await diagnostic.getByText('查询结果不能用于判定消息不存在或未发送；请核对原操作，勿重复发送。', { exact: true }).waitFor()
      const details = diagnostic.locator('details')
      if (await details.getAttribute('open') !== null || await diagnostic.locator('dl').isVisible())
        throw new Error('Evidence counts were not collapsed by default')
      await details.locator('summary').click()
      for (const [label, count] of [['持久记录数', '2'], ['会话水位数', '3'],
        ['返回会话数', '1'], ['请求记录数', '50'], ['返回记录数', '4']]) {
        const row = diagnostic.locator('dl > div').filter({ has: page.getByText(label, { exact: true }) })
        if (await row.locator('dd').innerText() !== count)
          throw new Error(`Evidence count mismatch: ${label}`)
      }
      if (await diagnostic.locator('time').getAttribute('datetime') !== now ||
          await diagnostic.locator('img').count() || (await diagnostic.innerText()).includes('SYNTHETIC_EVIDENCE_UNTRUSTED'))
        throw new Error('Evidence diagnostics rendered untrusted fields or lost their sampling time')
      if (['conversation_not_returned', 'message_not_found', 'scan_truncated'].includes(state)) {
        for (const width of [320, 390]) {
          await page.setViewportSize({ width, height: 844 })
          await settleSidebar(width)
          if (!await diagnostic.evaluate(el => {
            const r = el.getBoundingClientRect()
            return r.left >= 0 && r.right <= innerWidth && el.scrollWidth <= el.clientWidth + 1
          })) throw new Error(`Evidence diagnostics overflow ${state}/${width}`)
        }
        await page.setViewportSize({ width: 1440, height: 960 })
      }
      await details.locator('summary').click()
      await assertNoResend(index + 1)
    }
    evidenceState = '__proto__'
    await composer.getByRole('button', { name: '查询回显', exact: true }).click()
    await composer.getByRole('button', { name: '查询回显', exact: true }).waitFor()
    if (await diagnostic.count()) throw new Error('Unknown evidence enum was rendered')
    await assertNoResend(6)
    evidenceState = 'message_not_found'
    await composer.getByRole('button', { name: '查询回显', exact: true }).click()
    await diagnostic.waitFor()
    await composer.getByRole('button', { name: '核对状态', exact: true }).click()
    await composer.getByRole('button', { name: '核对状态', exact: true }).waitFor()
    if (await diagnostic.count() || operationReads !== 1)
      throw new Error('Inspecting durable status retained or refreshed one-shot evidence')
    await assertNoResend(7)
    await composer.getByRole('button', { name: '查询回显', exact: true }).click()
    await diagnostic.waitFor()
    await composer.getByRole('button', { name: '刷新发送条件', exact: true }).click()
    await composer.getByRole('button', { name: '查询回显', exact: true }).waitFor()
    if (await diagnostic.count()) throw new Error('Sending refresh retained one-shot evidence')
    await assertNoResend(8)
    for (const destination of ['conversation', 'account']) {
      holdEvidence = true
      const started = new Promise(resolve => { evidenceSeen = resolve })
      await composer.getByRole('button', { name: '查询回显', exact: true }).click()
      await started
      if (destination === 'conversation') {
        await center.getByRole('button').filter({ hasText: '合成私聊' }).click()
        await center.locator('article').getByText('合成私聊内容', { exact: true }).waitFor()
      } else {
        await choose('消息机器人乙')
        await center.getByText('暂无已接收的对话', { exact: true }).waitFor()
      }
      const response = page.waitForResponse(response => new URL(response.url()).pathname === `/api/v1${operationPath}/reconcile`)
      releaseEvidence()
      releaseEvidence = null
      await response
      await page.waitForTimeout(100)
      if (await center.getByRole('status', { name: '原操作回显诊断', exact: true }).count())
        throw new Error(`Late evidence leaked across ${destination}`)
      if (destination === 'account') await choose('消息机器人甲')
      await center.getByRole('button').filter({ hasText: '合成对话甲' }).click()
      await composer.getByText(unknownLabel, { exact: true }).waitFor()
      if (await diagnostic.count() ||
          await composer.getByRole('textbox', { name: '消息内容', exact: true }).inputValue() !== '合成未决草稿，不得发送')
        throw new Error('Late evidence contaminated a retained sending draft')
      await assertNoResend(destination === 'conversation' ? 9 : 10)
    }
    viewer = true
    await page.reload()
    await page.getByRole('heading', { name: '用户中心', exact: true }).waitFor()
    if (await page.getByRole('navigation', { name: '主导航' }).getByRole('button', { name: '消息中心', exact: true }).count())
      throw new Error('Shared private conversation menu leaked to tenant')
    await page.setViewportSize({ width: 320, height: 740 })
    await settleSidebar(320)
    await page.getByRole('button', { name: '更多操作', exact: true }).click()
    const tenantMenu = page.getByRole('menu')
    await tenantMenu.getByRole('menuitem', { name: '退出登录', exact: true }).waitFor()
    for (const name of ['账号登录', '机器人连接状态'])
      if (await tenantMenu.getByRole('menuitem', { name, exact: true }).count())
        throw new Error(`Native controls leaked to tenant mobile menu: ${name}`)
    await page.screenshot({ path: 'output/playwright/messages-user-menu-320.png' })
    await page.keyboard.press('Escape')
    return { passed: true, read_only: true, recall_reads: recallReads, recalls_escaped_and_scoped: true,
      native_style_layout: true, compact_decode_popover: true,
      viewports: 4, mobile_borderless: true, single_row_topbar: true, mobile_overflow_actions: true,
      mobile_list_thread_navigation: true, scope_fencing: true, tenant_menu_hidden: true,
      mobile_tenant_permissions: true, protocol_block_explained: true,
      chili_friend_and_system_notices: true, chili_native_format_preserved: true,
      text_evidence_cases: evidenceCases.length, text_evidence_reads: evidenceReads,
      text_evidence_explicit_only: true, text_evidence_no_resend: true,
      text_evidence_unknown_enum_ignored: true, text_evidence_late_scope_isolation: true }
  } finally {
    if (release) release()
    if (releaseEvidence) releaseEvidence()
    await page.evaluate(saved => {
      for (const key of Object.keys(sessionStorage)) if (key.startsWith('wsl:')) sessionStorage.removeItem(key)
      for (const [key, value] of Object.entries(saved)) sessionStorage.setItem(key, value)
    }, storage)
    await page.setViewportSize({ width: 1440, height: 960 })
    // Unmount the fixture before removing interception; never fall through to live APIs.
    await page.goto('about:blank')
    await page.unroute('**/api/v1/**', handler)
  }
}
