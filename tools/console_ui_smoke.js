// Synthetic website state only. Any model, transport or unrelated write fails.
async (page) => {
  const base = 'http://127.0.0.1:5173'
  const uid = 'ae000000-0000-4000-8000-000000000001'
  const aid = 'ae000000-0000-4000-8000-000000000002'
  const gid = 'ae000000-0000-4000-8000-000000000003'
  const permissions = [
    'overview:read',
    'accounts:read',
    'groups:read',
    'operations:read',
    'audit:read',
    'claims:read',
    'plans:manage',
    'users:manage',
    'tests:manage',
    'workspaces:maibot',
    'assistant:use',
    'assistant:propose',
    'assistant:approve'
  ]
  let tenant = false,
    unavailable = false,
    settingsReads = 0
  const featurePlatforms = []
  let settings = {
    version: 7,
    enabled: false,
    tenant_access: false,
    approvals_enabled: false,
    configured: false,
    monthly_tokens: 0,
    model: 'default',
    base_url: ''
  }
  const writes = []
  const stamp = new Date().toISOString()
  const group = {
    group_id: gid,
    name: '合成授权群',
    active: true,
    primary_account_id: aid,
    management_enabled: true,
    ai_enabled: false,
    ai_requested: true
  }
  const pattern = '**/api/v1/**'
  const handler = async (route) => {
    const request = route.request(),
      url = new URL(request.url()),
      path = url.pathname.replace('/api/v1', '')
    const workspace = request.headers()['x-workspace'] || 'wsl'
    let data = []
    if (request.method() !== 'GET') {
      if (path !== '/assistant/settings' || request.method() !== 'PUT' || tenant)
        throw new Error('Unexpected console mutation')
      const body = request.postDataJSON()
      if (body.version !== settings.version || 'api_key' in body)
        throw new Error('Settings CAS or key boundary lost')
      writes.push(body)
      settings = { ...settings, ...body, version: settings.version + 1 }
      data = settings
    } else if (path === '/me')
      data = {
        user: {
          id: uid,
          username: 'synthetic',
          role: tenant ? 'user' : 'superadmin',
          tenant_id: tenant ? gid : ''
        },
        permissions
      }
    else if (path === '/workspaces')
      data = {
        items: [
          { id: 'user', name: '用户中心' },
          { id: 'wsl', name: '旺商聊机器人' },
          { id: 'lajiaoliao', name: '辣椒聊机器人' }
        ]
      }
    else if (path === '/accounts')
      data =
        workspace === 'wsl'
          ? [
              {
                id: aid,
                name: '合成机器人',
                workspace: 'wsl',
                platform: 'wsl',
                auth_state: 'authenticated',
                runtime_state: 'connected'
              }
            ]
          : []
    else if (path === '/groups') data = workspace === 'wsl' ? [group] : []
    else if (path === '/operations')
      data = ['succeeded', 'failed', 'unknown', 'queued'].map((status, index) => ({
        id: `${uid}-${index}`,
        account_id: aid,
        group_id: gid,
        kind: 'fixture',
        status,
        created_at: stamp
      }))
    else if (path === '/overview')
      data = {
        accounts: 1,
        groups: 1,
        ai_groups: 0,
        pending_claims: 0,
        limits: { accounts: 10, groups: 300, ai_groups: 300 },
        native_ready: false,
        ai_ready: false
      }
    else if (path === '/assistant/settings') {
      settingsReads++
      if (unavailable)
        return route.fulfill({
          status: 503,
          json: { code: 1, msg: 'database_unavailable' }
        })
      data = settings
    } else if (path === '/assistant/health')
      data = {
        sampled_at: stamp,
        active_turns: 1,
        concurrency_limit: 2,
        persisted_active_turns: 1,
        pending_usage: 3,
        unverified_usage: 2
      }
    else if (path === '/assistant/usage')
      data = {
        unit: 'micro_credit',
        used_or_reserved: 0,
        timezone: 'Asia/Shanghai'
      }
    else if (path === '/audit')
      data = [
        {
          id: 1001, action: 'assistant_plan_committed', actor_id: uid, resource_id: gid,
          created_at: stamp,
          detail: { version: 8, execution_id: aid, input: 'PRIVATE_PROMPT_SENTINEL', api_key: 'PRIVATE_KEY_SENTINEL' }
        },
        {
          id: 1000, action: 'assistant_turn_finished', actor_id: uid, resource_id: aid,
          created_at: stamp, detail: { status: 'failed', text: 'PRIVATE_REPLY_SENTINEL' }
        },
        { id: 999, action: 'unrelated_fixture', detail: { input: 'UNRELATED_SENTINEL' } }
      ]
    else if (path === '/features/catalog' || path === `/groups/${gid}/features`) {
      featurePlatforms.push(workspace)
      const definition = {
        id: 'fixture-service',
        name: '合成配置功能',
        category: '群管',
        status: 'native_pending',
        fields: [{ key: 'enabled', type: 'boolean', label: '启用配置' }],
        native_dependencies: []
      }
      data = {
        platform: workspace,
        native_usage: {
          'lajiao-read': 'foundation',
          'fixture-read': 'foundation',
          'fixture-action': 'foundation',
          'fixture-pending': 'foundation'
        },
        group_id: gid,
        account_id: aid,
        version: 1,
        items:
          path === '/features/catalog'
            ? [definition]
            : [{
                definition,
                values: { enabled: true },
                entitled: true,
                editable_fields: [],
                effective_enabled: false
              }],
        native_capabilities:
          workspace === 'lajiaoliao'
            ? [
                {
                  id: 'lajiao-read',
                  name: '辣椒合成读取',
                  category: '读取',
                  kind: 'read',
                  support: 'confirmed',
                  integration: 'partial',
                  role: '成员',
                  summary: '合成',
                  evidence: 'fixture'
                }
              ]
            : [
                {
                  id: 'fixture-read',
                  name: '合成读取',
                  category: '读取',
                  kind: 'read',
                  support: 'confirmed',
                  integration: 'implemented',
                  role: '成员',
                  summary: '合成',
                  evidence: 'fixture'
                },
                {
                  id: 'fixture-action',
                  name: '合成操作',
                  category: '群管',
                  kind: 'action',
                  support: 'confirmed',
                  integration: 'partial',
                  role: '管理员',
                  summary: '合成',
                  evidence: 'fixture'
                },
                {
                  id: 'fixture-pending',
                  name: '待接入能力',
                  category: '群管',
                  kind: 'action',
                  support: 'review',
                  integration: 'pending',
                  role: '管理员',
                  summary: '合成',
                  evidence: 'fixture'
                }
              ]
      }
    }
    await route.fulfill({ json: { code: 0, msg: '', data } })
  }
  const original = await page
    .evaluate(() => Object.fromEntries(Object.entries(sessionStorage)))
    .catch(() => ({}))
  await page.route(pattern, handler)
  const screen = async (name, width) => {
    await page.setViewportSize({ width, height: 960 })
    await page.evaluate(async () => {
      await Promise.all(
        document
          .getAnimations()
          .filter((animation) => animation.effect?.getComputedTiming().iterations !== Infinity)
          .map((animation) => animation.finished.catch(() => {}))
      )
    })
    const fits = await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth + 1)
    if (!fits) throw new Error(`${name} overflow at ${width}`)
    await page.screenshot({
      path: `output/playwright/console-${name}-${width}.png`,
      fullPage: true,
      animations: 'disabled'
    })
  }
  try {
    await page.goto(base)
    await page.evaluate(() => sessionStorage.clear())
    await page.goto(base + '/#overview')
    await page.reload()
    await page.getByRole('heading', { name: '群管机器人状态', exact: true }).waitFor()
    const nav = page.getByRole('navigation', { name: '主导航' })
    for (const name of ['工作空间', '群管与服务', '记录与分析', '组织与订阅', '平台运维'])
      await nav.getByRole('heading', { name, exact: true }).waitFor({ state: 'attached' })
    if (
      await nav
        .locator('section')
        .evaluateAll((sections) => sections.some((section) => !section.querySelector('button')))
    )
      throw new Error('Empty navigation section')
    await page.getByRole('region', { name: '工作台指标分析' }).waitFor()
    await page.waitForFunction(
      () => document.querySelectorAll('.workspace-insights canvas').length === 2
    )
    for (const width of [320, 390, 647, 1440]) {
      await screen('overview', width)
      const nonblank = await page.locator('.workspace-insights canvas').evaluateAll((canvases) =>
        canvases.every((canvas) => {
          const pixels = canvas
            .getContext('2d')
            .getImageData(0, 0, canvas.width, canvas.height).data
          let colored = 0
          for (let i = 0; i < pixels.length; i += 4)
            if (
              pixels[i + 3] &&
              Math.max(pixels[i], pixels[i + 1], pixels[i + 2]) -
                Math.min(pixels[i], pixels[i + 1], pixels[i + 2]) >
                35
            )
              colored++
          return colored > 100
        })
      )
      if (!nonblank) throw new Error('Empty Art chart')
    }
    await page.goto(base + '/#features')
    await page.waitForTimeout(500)
    await page.getByTestId('feature-center').waitFor()
    await page.getByRole('tab', { name: '原生功能表', exact: true }).click()
    await page.getByRole('heading', { name: '原生底座 · 独立验收项进度', exact: true }).waitFor()
    await page.getByRole('row').filter({ hasText: '合成操作' }).waitFor()
    await screen('features', 390)
    await screen('features', 1440)
    // Business configuration is entered from the scoped user view.
    await page.goto(base + '/#groups')
    await page.getByRole('row').filter({ hasText: group.name })
      .getByRole('button', { name: '进入用户中心', exact: true }).click()
    const summary = page.getByLabel('业务功能概况', { exact: true })
    for (const [label, value] of [['可见业务功能', '1'], ['套餐包含', '1'], ['已保存启用', '1'], ['实际链路启用', '0']]) {
      const actual = await summary.locator('div').filter({
        has: page.getByText(label, { exact: true })
      }).locator('strong').innerText()
      if (actual !== value) throw new Error(`Feature status mismatch: ${label}=${actual}`)
    }
    await page.getByRole('button', { name: '切换到管理后台', exact: true }).click()
    await page.goto(base + '/#features')
    await page.getByRole('tab', { name: '原生功能表', exact: true }).click()
    await page.getByRole('button', { name: '切换工作台', exact: true }).click()
    await page
      .getByRole('dialog', { name: '选择平台', exact: true })
      .getByRole('button', { name: '辣椒聊机器人', exact: true })
      .click()
    await page.goto(base + '/#features')
    await page.getByRole('tab', { name: '原生功能表', exact: true }).click()
    await page.getByRole('row').filter({ hasText: '辣椒合成读取' }).waitFor()
    if (
      !featurePlatforms.includes('lajiaoliao') ||
      (await page.getByRole('row').filter({ hasText: '合成操作' }).count())
    )
      throw new Error('Feature catalog crossed platform')
    await page.getByRole('button', { name: '切换工作台', exact: true }).click()
    await page
      .getByRole('dialog', { name: '选择平台', exact: true })
      .getByRole('button', { name: '旺商聊机器人', exact: true })
      .click()
    await page.getByRole('button', { name: '切换到用户中心', exact: true }).click()
    await page.goto(base + '/#assistant')
    const workspace = page.getByRole('region', { name: '助手会话工作台' })
    await workspace.waitFor()
    if (settingsReads || writes.length)
      throw new Error('Conversation read admin settings or submitted automatically')
    if (await workspace.getByRole('switch').count())
      throw new Error('Platform switches leaked into conversation')
    for (const width of [320, 390, 647, 1440]) await screen('assistant', width)
    await page.getByRole('button', { name: '切换到管理后台', exact: true }).click()
    await page.goto(base + '/#assistant-admin')
    const admin = page.getByTestId('assistant-governance')
    await admin.getByRole('row').filter({ hasText: '人工确认保存' }).waitFor()
    await admin.getByRole('row').filter({ hasText: '对话回合结束' }).waitFor()
    const auditText = await admin.innerText()
    if (['PRIVATE_PROMPT_SENTINEL', 'PRIVATE_KEY_SENTINEL', 'PRIVATE_REPLY_SENTINEL', 'UNRELATED_SENTINEL'].some(value => auditText.includes(value)))
      throw new Error('Assistant audit exposed private bodies or unrelated detail')
    await admin.getByText('版本 8 · 配置已保存', { exact: true }).waitFor()
    await admin
      .getByText('存在用量待核对调用，请在 AI 网关核对原调用。', {
        exact: true
      })
      .waitFor()
    for (const width of [320, 390, 647, 1440]) await screen('governance', width)
    if (writes.length) throw new Error('Navigation changed settings')
    await admin
      .getByRole('switch', { name: '助手租户灰度', exact: true })
      .locator('xpath=..')
      .click()
    await admin.getByRole('button', { name: '保存准入设置', exact: true }).click()
    await page.getByText('准入设置已保存', { exact: true }).waitFor()
    if (writes.length !== 1 || writes[0].enabled || !writes[0].tenant_access)
      throw new Error('Unexpected settings update')
    unavailable = true
    await admin.getByRole('button', { name: '刷新助手状态', exact: true }).click()
    await admin.getByText('未采集', { exact: true }).first().waitFor()
    if (await admin.locator('.el-statistic').count())
      throw new Error('Missing statistics became zero')
    tenant = true
    unavailable = false
    const reads = settingsReads
    await page.reload()
    await page.locator('.page-heading h1').waitFor()
    if (await nav.getByRole('heading', { name: '平台运维', exact: true }).count())
      throw new Error('Admin section leaked to tenant')
    for (const name of ['助手审计', '受控测试', 'AI 网关', '用户管理', 'SaaS 套餐'])
      if (await nav.getByRole('button', { name, exact: true }).count())
        throw new Error('Admin navigation leaked')
    if (settingsReads !== reads) throw new Error('Forbidden admin deep link fetched settings')
    if (writes.length !== 1) throw new Error('Role change performed a mutation')
    return {
      passed: true,
      synthetic_only: true,
      viewports: [320, 390, 647, 1440],
      native_writes: 0,
      model_calls: 0,
      settings_writes: writes.length,
      chart_pixels_checked: true,
      tenant_admin_separation: true
    }
  } finally {
    await page.unroute(pattern, handler)
    await page.evaluate((values) => {
      sessionStorage.clear()
      for (const [key, value] of Object.entries(values)) sessionStorage.setItem(key, value)
    }, original)
  }
}
