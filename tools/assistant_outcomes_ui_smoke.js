// Synthetic APIs only. Run with the project's built-asset Playwright harness.
// prettier-ignore
async (page) => {
  const origin = new URL(page.url()).origin
  const id = n => `d6000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const originalActor = id(1), tenant = id(2), now = new Date().toISOString()
  const permissions = ['overview:read', 'accounts:read', 'groups:read', 'assistant:use',
    'assistant:propose', 'assistant:approve', 'assistant:action_propose', 'assistant:action_approve']
  let actor = originalActor, serial = 100, delayTranscript
  const sessions = [], turns = new Map(), plans = new Map(), events = new Map()
  const writes = [], transcriptReads = [], screenshots = [], errors = [], blocked = [], gates = []
  const accounts = ['wsl', 'lajiaoliao'].map((platform, index) => ({
    id: id(10 + index), name: '合成账号', native_nickname: '合成机器人', platform,
    workspace: platform, purpose: 'management'
  }))
  const groups = platform => ['测试群', '另一测试群'].map((name, index) => ({
    group_id: id((platform === 'wsl' ? 20 : 30) + index), name, platform, active: true,
    primary_account_id: accounts.find(item => item.platform === platform).id,
    robot_nickname: '合成机器人', native_id: `synthetic-${platform}-${index}`
  }))
  const clone = value => JSON.parse(JSON.stringify(value))
  const check = (condition, reason) => { if (!condition) throw Error(reason) }
  const region = () => page.getByRole('region', { name: '助手会话工作台' })
  const row = turn => region().locator(`[data-turn-id="${turn.id}"]`)
  const outcome = (plan, summary = plan.summary) => ({
    plan_id: plan.id, kind: plan.kind, status: plan.status, summary,
    ...(plan.operation_id ? { operation_id: plan.operation_id } : {})
  })
  const toolCard = (plan, scope, summary = plan.summary) => ({
    type: plan.kind === 'configuration' ? 'propose_group_config_change' : 'propose_group_action',
    scope, sampled_at: now, verification: 'suggestion_only', truncated: false,
    data: { ...clone(plan), summary }
  })
  function transcript(sessionID) {
    return [...turns.values()].filter(turn => turn.session_id === sessionID).map(turn => {
      const plan = plans.get(turn.plan_id)
      return {
        ...clone(turn),
        cards: turn.unavailable ? clone(turn.cards || []) : plan ? [toolCard(plan, plan.scope)] : [],
        outcomes: plan ? [outcome(plan), outcome(plan)] : [],
        cursor: events.get(turn.id)?.length || 0
      }
    })
  }
  function deferTranscript(sessionID) {
    let requested, release
    const reached = new Promise(resolve => { requested = resolve })
    const released = new Promise(resolve => { release = resolve })
    const gate = { sessionID, requested, released, release, reached }
    gates.push(gate)
    delayTranscript = gate
    return gate
  }
  function seedSession(platform, groupIndex = 1) {
    const scope = { platform, kind: 'group', group_id: groups(platform)[groupIndex].group_id,
      account_id: accounts.find(item => item.platform === platform).id }
    const item = { id: id(serial++), scope, actor, title: '另一群自己的记录',
      is_empty: false, created_at: now }
    sessions.push(item)
    const turn = { id: id(serial++), session_id: item.id, status: 'succeeded',
      result_code: '', input: '另一群提问', answer: `另一群自己的回答 ${platform}`, cards: [] }
    turns.set(turn.id, turn)
    return item
  }
  const onError = error => errors.push(error.message)
  page.on('pageerror', onError)
  await page.addInitScript(() => {
    Object.defineProperty(navigator, 'clipboard', { configurable: true,
      value: { writeText: async text => { window.__assistantOutcomeCopied = text } } })
  })
  const handler = async route => {
    const request = route.request(), url = new URL(request.url())
    if (url.origin !== origin) {
      blocked.push(url.origin)
      return route.abort('blockedbyclient')
    }
    if (!url.pathname.startsWith('/api/')) return route.fallback()
    const path = url.pathname.replace('/api/v1', '')
    const platform = request.headers()['x-workspace'] || 'wsl'
    let data = [], status = 200
    if (request.method() !== 'GET') {
      check(request.method() === 'POST' && path.startsWith('/assistant/'),
        `Unexpected write: ${request.method()} ${path}`)
      check(!!request.headers()['idempotency-key'], 'Missing operation idempotency key')
      const body = request.postDataJSON()
      writes.push(path)
      if (path === '/assistant/sessions') {
        check(body.platform === platform, 'Session crossed platforms')
        check(groups(platform).some(group => group.group_id === body.group_id &&
          group.primary_account_id === body.account_id), 'Session changed its account binding')
        data = { id: id(serial++), scope: body, actor, is_empty: true, title: '', created_at: now }
        sessions.push(data)
      } else if (/^\/assistant\/sessions\/[^/]+\/turns$/.test(path)) {
        const session = sessions.find(item => item.id === path.split('/')[3] && item.actor === actor)
        check(!!session, 'Turn lost website actor scope')
        const empty = body.input === '空回答'
        const kind = body.input.includes('配置') ? 'configuration' : 'action'
        const plan = empty ? undefined : {
          id: id(serial++), session_id: session.id, scope: session.scope, platform,
          account_id: session.scope.account_id, group_id: session.scope.group_id,
          kind, action: 'send_text', name: '发送群文本', text: '只用于合成验收的一条文本',
          status: 'pending_approval', version: 1, base_version: 1, digest: 'synthetic-server-digest',
          summary: `${platform} ${body.input}：建议已生成，等待本人确认，尚未执行。`,
          message: '仅为建议，尚未保存配置', production_admitted: true, result_code: '',
          expires_at: new Date(Date.now() + 600000).toISOString(), applied: false,
          before: { blocked_words: { words: [] } }, changes: { blocked_words: { words: ['测试词'] } }
        }
        if (plan) plans.set(plan.id, plan)
        const turn = { id: id(serial++), session_id: session.id, status: 'running',
          result_code: '', input: body.input, answer: body.input === '动作成功' ? '已整理好建议，请核对本群与具体动作。' : '',
          cards: [], plan_id: plan?.id, created_at: now }
        turns.set(turn.id, turn)
        session.is_empty = false
        session.title ||= body.input
        const list = [{ kind: 'turn_started', data: { status: 'running' } }]
        if (turn.answer) list.push({ kind: 'text_delta', data: { text: turn.answer } })
        if (plan) list.push({ kind: 'tool_result', data: toolCard(plan, session.scope,
          `流式待确认摘要 ${platform} ${body.input}`) })
        list.push({ kind: 'turn_finished', data: { status: 'succeeded', code: '' } })
        events.set(turn.id, list.map((event, index) => ({ ...event, turn_id: turn.id, seq: index + 1 })))
        data = { ...turn, status: 'queued' }
        status = 202
      } else if (/^\/assistant\/(?:action-plans|plans)\/[^/]+\/(?:approve|cancel)$/.test(path)) {
        const plan = plans.get(path.split('/')[3])
        check(!!plan && plan.scope.platform === platform, 'Plan crossed platforms')
        check(plan.status === 'pending_approval', 'UI retried a submitted plan')
        if (path.endsWith('/approve')) {
          check(body.digest === plan.digest && body.version === plan.version, 'Approval lost plan fence')
          const turn = [...turns.values()].find(item => item.plan_id === plan.id)
          plan.status = plan.kind === 'configuration' ? 'committed' :
            turn.input === '动作未知' ? 'unknown' : turn.input === '动作失败' ? 'failed' : 'succeeded'
          plan.summary = plan.kind === 'configuration'
            ? `${platform} 测试群的拦截词配置已保存；业务运行状态没有改变。`
            : plan.status === 'unknown'
              ? `${platform} 测试群的发送结果待核对，保留原操作编号，不会自动重发。`
              : plan.status === 'failed'
                ? `${platform} 测试群的发送失败，未收到成功回执，没有自动重试。`
                : `${platform} 已向测试群发送一条文本，并收到平台成功回执。没有修改群配置。`
          plan.message = plan.summary
          plan.operation_id = plan.kind === 'action' ? id(serial++) : undefined
        } else {
          plan.status = 'cancelled'
          plan.summary = `${platform} 已取消${plan.kind === 'configuration' ? '配置建议' : '发送建议'}，没有执行该操作。`
        }
        plan.version++
        data = clone(plan)
      } else throw Error(`Unexpected model/native work or retry: ${path}`)
    } else if (path === '/me') data = { user: {
      id: actor, tenant_id: tenant,
      username: actor === originalActor ? 'synthetic-outcome-user' : 'synthetic-outcome-user-new',
      role: 'custom'
    }, permissions }
    else if (path === '/workspaces') data = { items: [
      { id: 'wsl', name: '旺商聊机器人' }, { id: 'lajiaoliao', name: '辣椒聊机器人' }
    ] }
    else if (path === '/accounts') data = accounts.filter(item => item.platform === platform)
    else if (path === '/groups') data = groups(platform)
    else if (path === '/assistant/context') data = {
      platform, groups: groups(platform), permissions, total: 2, next_offset: -1
    }
    else if (path === '/assistant/usage') data = { unit: 'micro_credit', used_or_reserved: 0, timezone: 'Asia/Shanghai' }
    else if (path === '/assistant/sessions') data = sessions.filter(item => item.actor === actor &&
      item.scope.platform === platform && item.scope.group_id === url.searchParams.get('group_id'))
    else if (/^\/assistant\/sessions\/[^/]+\/transcript$/.test(path)) {
      const sessionID = path.split('/')[3]
      check(sessions.some(item => item.id === sessionID && item.actor === actor &&
        item.scope.platform === platform), 'Transcript requested across website actor/platform')
      transcriptReads.push(sessionID)
      data = transcript(sessionID)
      if (delayTranscript?.sessionID === sessionID) {
        const gate = delayTranscript
        delayTranscript = undefined
        gate.requested()
        await gate.released
      }
    } else if (/^\/assistant\/(?:action-plans|plans)\/[^/]+$/.test(path)) {
      data = clone(plans.get(path.split('/')[3]))
    } else if (/^\/assistant\/turns\/[^/]+\/events$/.test(path)) {
      const turnID = path.split('/')[3], turn = turns.get(turnID)
      turn.status = 'succeeded'
      const list = (events.get(turnID) || []).filter(event => event.seq > Number(url.searchParams.get('after') || 0))
      await route.fulfill({ contentType: 'text/event-stream; charset=utf-8',
        body: list.map(event => `id: ${event.seq}\nevent: ${event.kind}\ndata: ${JSON.stringify(event)}\n\n`).join('') })
      return
    } else if (/^\/assistant\/turns\/[^/]+$/.test(path)) data = clone(turns.get(path.split('/')[3]))
    await route.fulfill({ status, json: { code: 0, msg: 'ok', data } })
  }
  await page.route('**/*', handler)
  const responseForTranscript = () => page.waitForResponse(response =>
    /\/assistant\/sessions\/[^/]+\/transcript$/.test(new URL(response.url()).pathname))
  const waitGate = gate => Promise.race([gate.reached,
    new Promise((_, reject) => setTimeout(() => reject(Error('Transcript refresh never arrived')), 10000))])
  const expandGroups = async () => {
    const button = region().locator('.assistant-mobile-groups')
    if (await button.isVisible() && await button.getAttribute('aria-expanded') !== 'true') await button.click()
  }
  const selectGroup = async index => {
    await expandGroups()
    await region().getByRole('button', { name: index ? /选择群组 另一测试群/ : /选择群组 测试群/ }).click()
  }
  const closeDialog = async dialog => {
    await page.keyboard.press('Escape')
    await dialog.waitFor({ state: 'hidden' })
  }
  const ask = async input => {
    const response = page.waitForResponse(response => response.request().method() === 'POST' &&
      /\/assistant\/sessions\/[^/]+\/turns$/.test(new URL(response.url()).pathname))
    await region().getByRole('textbox', { name: '助手问题', exact: true }).fill(input)
    await region().getByRole('button', { name: '发送', exact: true }).click()
    const turn = turns.get((await (await response).json()).data.id)
    const plan = plans.get(turn.plan_id)
    await row(turn).locator('.assistant-turn-state').getByText('本轮结束', { exact: true }).waitFor()
    return { turn, plan }
  }
  const waitSummary = async (turn, summary) =>
    row(turn).locator('.assistant-outcome-summary').getByText(summary, { exact: true }).waitFor()
  const openPlan = async (turn, plan) => {
    const refresh = responseForTranscript()
    await row(turn).getByRole('button', {
      name: plan.kind === 'configuration' ? '查看配置差异' : '查看动作与影响', exact: true
    }).click()
    await refresh
    const dialog = page.getByRole('dialog', {
      name: plan.kind === 'configuration' ? '配置建议审批' : '确认群管动作', exact: true
    })
    await dialog.waitFor()
    return dialog
  }
  const act = async (turn, plan, label) => {
    const dialog = await openPlan(turn, plan)
    const before = transcriptReads.length, refresh = responseForTranscript()
    await dialog.getByRole('button', { name: label, exact: true }).click()
    await refresh
    await waitSummary(turn, plan.summary)
    check(transcriptReads.length === before + 1, 'Plan mutation did not use one bounded transcript refresh')
    check(await row(turn).locator('.assistant-outcome-summary').count() === 1, 'Outcome was duplicated')
    return dialog
  }
  try {
    for (const platform of ['wsl', 'lajiaoliao']) {
      actor = originalActor
      const otherSession = seedSession(platform)
      await page.evaluate(({ actor, platform }) => {
        sessionStorage.clear()
        sessionStorage.setItem(`wsl:console:${actor}`, 'user')
        sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
      }, { actor, platform })
      await page.setViewportSize({ width: 1440, height: 960 })
      await page.goto(origin + '/#assistant')
      await page.reload()
      await selectGroup(0)
      await region().locator('.assistant-welcome').waitFor()
      const empty = await ask('空回答')
      await region().getByRole('button', { name: '重新读取', exact: true }).click()
      await row(empty.turn).waitFor()
      check(await row(empty.turn).locator('.assistant-model-answer,.assistant-outcome').count() === 0,
        'Empty answer rendered an empty assistant bubble')
      const current = sessions.find(item => item.id === empty.turn.session_id)
      const gate = deferTranscript(current.id)
      const config = await ask('配置保存')
      await waitGate(gate)
      await waitSummary(config.turn, `流式待确认摘要 ${platform} 配置保存`)
      check(await row(config.turn).locator('.assistant-model-answer').count() === 0,
        'Tool-only turn rendered an empty model answer')
      const postsBeforeRefresh = writes.length
      gate.release()
      await waitSummary(config.turn, config.plan.summary)
      check(await row(config.turn).getByText(`流式待确认摘要 ${platform} 配置保存`, { exact: true }).count() === 0,
        'Live tool summary was not replaced')
      check(writes.length === postsBeforeRefresh, 'Refreshing a turn posted new work')
      let dialog = await act(config.turn, config.plan, '确认保存配置')
      await closeDialog(dialog)
      const action = await ask('动作成功')
      await waitSummary(action.turn, action.plan.summary)
      dialog = await act(action.turn, action.plan, '确认发送')
      await closeDialog(dialog)
      await row(action.turn).getByRole('button', { name: /^复制回答/ }).click()
      const copied = await page.evaluate(() => window.__assistantOutcomeCopied)
      check(copied === `${action.turn.answer}\n\n${action.plan.summary}`, 'Copy omitted outcome or model answer')
      const unknown = await ask('动作未知')
      await waitSummary(unknown.turn, unknown.plan.summary)
      dialog = await act(unknown.turn, unknown.plan, '确认发送')
      check(await dialog.getByRole('button', { name: '确认发送', exact: true }).count() === 0,
        'Unknown result still exposed approval')
      const beforeReconcile = writes.length, readsBeforeIdle = transcriptReads.length
      unknown.plan.summary = `${platform} 已核对原操作，结果仍未知，请保留操作编号，没有重发。`
      await page.waitForTimeout(200)
      check(transcriptReads.length === readsBeforeIdle, 'Unknown result started automatic polling')
      const reconciled = responseForTranscript()
      await dialog.getByRole('button', { name: '核对状态', exact: true }).click()
      await reconciled
      await waitSummary(unknown.turn, unknown.plan.summary)
      check(writes.length === beforeReconcile, 'Reconciling unknown result posted work')
      await closeDialog(dialog)
      for (const input of ['取消动作', '取消配置', '动作失败']) {
        const item = await ask(input)
        await waitSummary(item.turn, item.plan.summary)
        dialog = await act(item.turn, item.plan, input === '动作失败' ? '确认发送' : '取消建议')
        await closeDialog(dialog)
      }
      const beforeReload = writes.length, refreshed = responseForTranscript()
      await page.reload()
      await selectGroup(0)
      await refreshed
      await waitSummary(action.turn, action.plan.summary)
      check(writes.length === beforeReload, 'Reload replayed a model/approval request')
      check(await row(action.turn).locator('.assistant-turn-state').innerText() === '本轮结束',
        'Turn completion claimed a native operation succeeded')
      for (const width of [320, 390, 768, 1440]) {
        await page.setViewportSize({ width, height: 900 })
        await row(action.turn).locator('.assistant-outcome-summary').scrollIntoViewIfNeeded()
        check(!await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1),
          `Outcome page overflow ${platform} ${width}`)
        const textFits = await row(action.turn).locator('.assistant-outcome-summary').evaluate(element =>
          element.scrollWidth <= element.clientWidth + 1 && element.getBoundingClientRect().right <= innerWidth + 1)
        check(textFits, `Outcome text overflow ${platform} ${width}`)
        const send = await region().getByRole('button', { name: '发送', exact: true }).boundingBox()
        check(send && send.x >= 0 && send.x + send.width <= width + 1 && send.y + send.height <= 900,
          `Outcome composer hidden ${platform} ${width}`)
        const image = `output/playwright/assistant-outcomes/${platform}-${width}.png`
        await page.screenshot({ path: image, animations: 'disabled' })
        screenshots.push(image)
      }
      // A changed historical fingerprint hides all old bodies/cards, while a newly
      // authorized server outcome remains readable and copyable.
      config.turn.unavailable = true
      config.turn.input = 'SECRET_OLD_INPUT'
      config.turn.answer = 'SECRET_OLD_ANSWER'
      config.turn.cards = [{ ...toolCard(config.plan, config.plan.scope), data: { id: 'SECRET_OLD_CARD' } }]
      config.plan.summary = `${platform} 本次重新核验：该配置已保存。`
      const reloadUnavailable = responseForTranscript()
      await region().getByRole('button', { name: '重新读取', exact: true }).click()
      await reloadUnavailable
      await row(config.turn).getByText('权限已变化，该回合内容不可读取', { exact: true }).waitFor()
      await waitSummary(config.turn, config.plan.summary)
      check(!await region().locator('.assistant-thread').innerText().then(text => text.includes('SECRET_OLD_')),
        'Unavailable historical body leaked')
      check(await row(config.turn).locator('.assistant-result,.assistant-user-message,.assistant-model-answer').count() === 0,
        'Unavailable history exposed old cards or bubbles')
      await row(config.turn).getByRole('button', { name: '复制结果总结', exact: true }).click()
      check(await page.evaluate(() => window.__assistantOutcomeCopied) === config.plan.summary,
        'Copy leaked unavailable history')
      // Return more than ten rows deliberately: the UI must still retain ten.
      for (let n = 0; n < 12; n++) {
        const turn = { id: id(serial++), session_id: current.id, status: 'succeeded', result_code: '',
          input: `滚动历史 ${n}`, answer: `历史回答 ${n}`, cards: [], created_at: now }
        turns.set(turn.id, turn)
      }
      const tenRows = responseForTranscript()
      await region().getByRole('button', { name: '重新读取', exact: true }).click()
      await tenRows
      await region().getByText('历史回答 11', { exact: true }).waitFor()
      check(await region().locator('.assistant-turn').count() === 10, 'History exceeded ten turns')
      check(await region().getByText('历史回答 0', { exact: true }).count() === 0, 'Oldest history was retained')
      await page.setViewportSize({ width: 1440, height: 960 })
      const lateGroup = deferTranscript(current.id)
      await region().getByRole('button', { name: '重新读取', exact: true }).click()
      await waitGate(lateGroup)
      await selectGroup(1)
      await region().getByText(`另一群自己的回答 ${platform}`, { exact: true }).waitFor()
      lateGroup.release()
      await page.waitForTimeout(100)
      check(await region().getByText('历史回答 11', { exact: true }).count() === 0, 'Late transcript crossed groups')
      const otherPlatform = platform === 'wsl' ? 'lajiaoliao' : 'wsl'
      const latePlatform = deferTranscript(otherSession.id)
      await region().getByRole('button', { name: '重新读取', exact: true }).click()
      await waitGate(latePlatform)
      await region().getByRole('radiogroup', { name: '助手平台', exact: true }).locator('label')
        .filter({ hasText: otherPlatform === 'wsl' ? '旺商聊' : '辣椒聊' }).click()
      await page.waitForFunction(({ actor, platform }) =>
        sessionStorage.getItem(`wsl:workspace:${actor}`) === platform, { actor, platform: otherPlatform })
      latePlatform.release()
      await page.waitForTimeout(100)
      check(await region().getByText(`另一群自己的回答 ${platform}`, { exact: true }).count() === 0,
        'Late transcript crossed platforms')
      await region().getByRole('radiogroup', { name: '助手平台', exact: true }).locator('label')
        .filter({ hasText: platform === 'wsl' ? '旺商聊' : '辣椒聊' }).click()
      await selectGroup(1)
      await region().getByText(`另一群自己的回答 ${platform}`, { exact: true }).waitFor()
      const lateIdentity = deferTranscript(otherSession.id)
      await region().getByRole('button', { name: '重新读取', exact: true }).click()
      await waitGate(lateIdentity)
      actor = id(3)
      const identityChanged = page.waitForResponse(response =>
        new URL(response.url()).pathname === '/api/v1/me')
      await page.locator('.topbar-actions').getByRole('button', { name: '刷新', exact: true }).click()
      await identityChanged
      await page.locator('.user-menu').getByText('synthetic-outcome-user-new', { exact: true }).waitFor()
      // Actor changes reset the rendered page without changing the hash.
      // A same-hash goto does nothing; use the real entry after the reset.
      await region().waitFor({ state: 'hidden' })
      await page.locator('#primary-sidebar > .sidebar-assistant').click()
      await region().waitFor()
      await selectGroup(1)
      await region().locator('.assistant-welcome').waitFor()
      lateIdentity.release()
      await page.waitForTimeout(100)
      check(await region().getByText(`另一群自己的回答 ${platform}`, { exact: true }).count() === 0,
        'Late transcript crossed website actors')
      check(writes.length === beforeReload, 'History/scope changes created model or native work')
    }
    check(errors.length === 0, `Browser errors: ${errors.join('; ')}`)
    return {
      passed: true, synthetic_only: true, platforms: ['wsl', 'lajiaoliao'],
      model_calls: 0, native_requests: 0, external_requests: 0, blocked_external_attempts: blocked.length,
      synthetic_turn_posts: writes.filter(path => path.endsWith('/turns')).length,
      synthetic_approvals: writes.filter(path => path.endsWith('/approve')).length,
      synthetic_cancels: writes.filter(path => path.endsWith('/cancel')).length,
      transcript_reads: transcriptReads.length, empty_bubbles_removed: true,
      live_summary_replaced: true, server_outcomes_deduplicated: true, copy_includes_outcome: true,
      unknown_manual_only: true, reload_without_replay: true, unavailable_history_redacted: true,
      reauthorized_outcome_visible: true, ten_turns: true, late_group_platform_actor_isolated: true,
      viewports: [320, 390, 768, 1440], screenshots
    }
  } finally {
    for (const gate of gates) gate.release()
    page.off('pageerror', onError)
    await page.unroute('**/*', handler)
  }
}
