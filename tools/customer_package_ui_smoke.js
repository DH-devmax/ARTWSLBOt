// Built release assets; synthetic package/permission responses only.
async (page) => {
  const base = new URL(page.url()).origin
  const id = n => `d5900000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const actor = id(1), tenant = id(2), screenshots = [], cases = []
  let mode = 'PLUS', platform = 'wsl', reads = 0
  const active = () => ['PLUS', 'PRO', 'restricted'].includes(mode)
  const assistant = () => mode === 'PRO'
  const permissions = () => [
    'overview:read', 'groups:read', 'billing:read',
    ...(active() ? ['claims:create', 'groups:moderation'] : []),
    ...(assistant() ? ['assistant:use'] : [])
  ]
  const group = () => ({
    group_id: id(platform === 'wsl' ? 10 : 11), platform, name: `${platform} 套餐验证群`,
    active: true, tenant_id: tenant, customer_name: '合成客户',
    robot_nickname: `${platform} 原生机器人`, primary_account_id: id(20), version: 1,
    ownership_state: 'unverified', business_enabled: false, ai_enabled: false
  })
  const handler = async route => {
    const request = route.request(), url = new URL(request.url())
    const path = url.pathname.replace('/api/v1', '')
    if (request.method() !== 'GET') throw Error('Package display submitted a mutation: ' + path)
    if (path.startsWith('/assistant')) throw Error('Closed/display-only package started an assistant request')
    reads++
    let data = []
    const tier = mode === 'PLUS' ? 'PLUS' : 'PRO'
    if (path === '/me') data = {
      user: { id: actor, tenant_id: tenant, username: 'synthetic-customer',
        role: 'user', tenant_role: 'owner' },
      permissions: permissions()
    }
    else if (path === '/workspaces') data = { items: [
      { id: 'user', name: '用户中心' },
      { id: 'wsl', name: '旺商聊机器人' },
      { id: 'lajiaoliao', name: '辣椒聊机器人' }
    ] }
    else if (path === '/user-center') data = {
      platform_admin: false, permissions: permissions(),
      entitlement: { mode: 'tenant_business', name: mode === 'none' ? '未开通套餐' : tier,
        active: active(), unlimited: false, billing_source: 'tenant_wallet', permissions: permissions() },
      subscription: mode === 'none' ? null : {
        name: tier, active: active(), enabled: true,
        expires_at: new Date(Date.now() + (active() ? 3600000 : -3600000)).toISOString(),
        group_limit: tier === 'PLUS' ? 1 : 3, member_limit: tier === 'PLUS' ? 3 : 10,
        platforms: ['wsl', 'lajiaoliao'],
        modules: ['moderation', ...(tier === 'PRO' ? ['management_assistant'] : [])]
      },
      groups: [group()], group_count: 1, active_group_count: 1, personas: [], operations: [], claims: []
    }
    else if (path === '/user-center/groups') {
      if (url.searchParams.get('platform') !== platform) throw Error('Group query crossed platforms')
      data = { items: [group()], total: 1, limit: 20, offset: 0, sampled_at: new Date().toISOString() }
    }
    else if (path === '/user-center/usage') data = {
      call_count: 0, actual_micro: 0, held_micro: 0, unlimited: false, billing_source: 'tenant_wallet'
    }
    await route.fulfill({ json: { code: 0, msg: '', data } })
  }
  await page.route('**/api/v1/**', handler)
  try {
    for (platform of ['wsl', 'lajiaoliao']) {
      for (mode of ['PLUS', 'PRO', 'restricted', 'expired', 'none']) {
        for (const width of [320, 390, 768, 1440]) {
          await page.setViewportSize({ width, height: 900 })
          await page.evaluate(({ actor, platform }) => {
            sessionStorage.clear()
            sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
          }, { actor, platform })
          await page.goto(base + '/#groups')
          await page.reload()
          const listing = page.locator('.uc-mobile-groups:visible, .uc-groups:visible')
          const button = listing.getByRole('button', { name: '群管助手', exact: true })
          await button.waitFor()
          if (await button.isEnabled() !== assistant())
            throw Error(`Assistant ignored package/permission: ${platform}/${mode}/${width}`)
          const center = page.locator('.user-center')
          if (await center.getByRole('button', { name: '接入群组', exact: true }).count() !== (active() ? 1 : 0))
            throw Error(`Expired/missing package can onboard: ${mode}`)
          if (await center.getByRole('button', { name: '测试认领', exact: true }).count() ||
            await center.getByRole('combobox', { name: '筛选所属客户', exact: true }).count() ||
            await page.getByRole('button', { name: '切换到管理后台', exact: true }).count())
            throw Error('Customer received operator controls')
          if (mode === 'expired')
            await center.getByText('套餐未生效或已到期，暂不可申请授权或修改热群配置', { exact: true }).waitFor()
          if (mode === 'none')
            await center.getByText('尚未开通套餐，请联系平台管理员', { exact: true }).waitFor()
          if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1))
            throw Error(`Package UI overflow: ${platform}/${mode}/${width}`)
          cases.push({ platform, mode, width, passed: true })
          if (['PLUS', 'PRO', 'expired'].includes(mode) && [390, 1440].includes(width)) {
            const screenshot = `output/playwright/customer-package-${platform}-${mode}-${width}.png`
            await page.screenshot({ path: screenshot, animations: 'disabled' })
            screenshots.push(screenshot)
          }
        }
      }
    }
    return { passed: true, cases, screenshots, reads, synthetic_only: true, paid_calls: 0, native_actions: 0 }
  } finally {
    await page.unroute('**/api/v1/**', handler)
  }
}
