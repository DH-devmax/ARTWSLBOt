// Synthetic read-only catalog. No native requests or writes are permitted.
async (page) => {
  const base = new URL(page.url()).origin
  if (!/^http:\/\/127\.0\.0\.1:\d+$/.test(base)) throw new Error('Local UI origin required')
  const actor = 'c1000000-0000-4000-8000-000000000001'
  const names = {
    mute_member: '成员禁言', unmute_member: '成员解禁', mute_group: '全群禁言 / 解除',
    publish_notice: '发布群公告', set_member_card: '修改群内昵称', invite_group: '拉入群组',
    leave_group: '退出群组', send_text: '发送群文本',
    account_settings: '账号资料与隐私设置', send_media: '语音 / 视频 / 文件',
    grant_admin: '设置群管理员', revoke_admin: '撤销群管理员', read_roles: '群主 / 管理员身份核验',
    read_recalls: '撤回通知读取'
  }
  const items = Object.entries(names).map(([id, name]) => ({
    id, name, kind: 'action', category: '群管', support: 'confirmed', integration: 'partial',
    role: '受控权限', summary: '合成能力说明', evidence: '合成协议依据'
  }))
  const acceptance = Object.fromEntries(items.slice(0, 7).map(item => [item.id, {
    verified_at: '2026-10-31T23:28:24+08:00',
    scope: '合成自有账号、唯一隔离群；历史结果，不代表当前所选账号',
    result: item.id === 'set_member_card' ? '仅本人名片修改及恢复已核验。' : '指定场景与独立回读已核验。',
    remaining: '其他成员、自动规则及故障恢复未验收。',
    evidence: 'docs/test-report.md#synthetic-controlled-case'
  }]))
  // A stray metadata key must not inflate the count.
  acceptance.unknown = acceptance.mute_member
  acceptance.account_settings = { ...acceptance.mute_member, result: '仅通话开关修改和恢复已核验。' }
  acceptance.read_roles = { ...acceptance.mute_member, result: '双方角色现读一致。' }
  items.find(item => item.id === 'read_roles').kind = 'read'
  Object.assign(items.find(item => item.id === 'read_recalls'), { kind: 'read', category: '消息' })
  for (const item of items)
    if (['grant_admin', 'revoke_admin'].includes(item.id))
      Object.assign(item, { support: 'review', integration: 'pending' })
  const child = (parent_id, id, name, verified = false, integration = 'partial') => ({
    ...items.find(item => item.id === parent_id), parent_id, id, name, integration,
    acceptance: verified ? acceptance[parent_id] : undefined
  })
  const details = [
    child('set_member_card', 'set_self_card', '修改本人群昵称', true),
    child('set_member_card', 'set_other_card', '修改他人群昵称'),
    child('invite_group', 'invite_group_request', '发送入群邀请', true),
    child('invite_group', 'accept_group_invitation', '本人接受入群邀请', true),
    child('account_settings', 'account_nickname', '修改原生昵称'),
    child('account_settings', 'privacy_calls', '语音 / 视频通话设置', true),
    child('account_settings', 'secondary_security_code', '设置二级安全码', false, 'deferred'),
    child('account_settings', 'legal_identity', '实名认证', false, 'deferred'),
    child('send_media', 'send_voice', '发送群语音'),
    child('send_media', 'send_video', '发送群视频'),
    child('send_media', 'send_file', '发送群文件')
  ]
  let writes = 0
  const handler = async route => {
    const req = route.request(), pathname = new URL(req.url()).pathname.replace('/api/v1', '')
    if (req.method() !== 'GET') {
      writes++
      return route.abort('blockedbyclient')
    }
    const chili = req.headers()['x-workspace'] === 'lajiaoliao'
    let data = []
    if (pathname === '/me') data = { user: { id: actor, role: 'superadmin' }, permissions: ['overview:read', 'accounts:read', 'groups:read'] }
    else if (pathname === '/workspaces') data = { items: [{ id: 'wsl', name: '旺商聊机器人' }, { id: 'lajiaoliao', name: '辣椒聊机器人' }] }
    else if (pathname === '/features/catalog') data = {
      platform: chili ? 'lajiaoliao' : 'wsl', native_ready: false,
      items: [{ id: 'cards', name: '群名片', category: '自动化', status: 'unavailable', fields: [],
        native_dependencies: ['set_member_card'], summary: '按规则自动处理群名片，尚未接入业务执行。' }],
      native_capabilities: items, native_acceptance: chili ? {} : acceptance,
      native_usage: Object.fromEntries([...items, ...details].map(item => [item.id, 'foundation'])),
      native_details: chili ? details.map(({ acceptance, ...item }) => ({
        ...item, integration: item.integration === 'deferred' ? 'deferred' : 'pending'
      })) : details
    }
    await route.fulfill({ json: { code: 0, msg: '', data } })
  }
  const boot = async workspace => {
    await page.evaluate(({ actor, workspace }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:workspace:${actor}`, workspace)
    }, { actor, workspace })
    await page.goto('about:blank')
    await page.goto(base + '/#features')
    await page.getByRole('tab', { name: '原生功能表', exact: true }).click()
    await page.getByTestId('native-capabilities').waitFor()
  }
  await page.route('**/api/v1/**', handler)
  try {
    await page.setViewportSize({ width: 1440, height: 960 })
    await boot('wsl')
    const table = page.getByTestId('native-capabilities')
    if (!(await table.getByTestId('native-controlled-count').innerText()).includes('9') ||
      !(await table.getByTestId('native-production-count').innerText()).includes('0'))
      throw new Error('Controlled evidence and production acceptance counts conflated')
    if (!(await table.getByTestId('native-usable-count').innerText()).includes('10') ||
      !(await table.locator('.native-chart-legend').innerText()).replace(/\s+/g, ' ').includes('指定场景可用 10'))
      throw new Error('Green chart lost independent scoped evidence or double-counted rows')
    if (await table.getByRole('checkbox').count() || await table.getByRole('switch').count())
      throw new Error('Native catalog exposed activation controls')
    if ((await table.getByTestId('native-checklist-count').innerText()).trim() !== '19')
      throw new Error('Capability groups and child checks double-counted')
    const recallRow = table.getByRole('row').filter({ hasText: '撤回通知读取' })
    await recallRow.getByText('已核对支持', { exact: true }).waitFor()
    await recallRow.getByText('部分接入', { exact: true }).waitFor()
    await recallRow.getByText('尚无通过记录', { exact: true }).waitFor()
    const search = table.getByRole('textbox', { name: '搜索原生功能', exact: true })
    await search.fill('撤回通知')
    await recallRow.getByText('部分接入', { exact: true }).waitFor()
    await page.screenshot({ path: 'output/playwright/native-recall-catalog.png', animations: 'disabled' })
    await search.fill('')
    for (const name of ['设置二级安全码', '实名认证'])
      if (await table.getByRole('button', { name: `查看${name}`, exact: true }).count())
        throw new Error('Deferred capability counted in active checklist')
    for (const name of ['设置群管理员', '撤销群管理员', '发送群语音', '发送群视频', '发送群文件', '修改他人群昵称', '本人接受入群邀请'])
      await table.getByRole('button', { name: `查看${name}`, exact: true }).waitFor()
    await table.getByRole('button', { name: '查看设置群管理员', exact: true }).click()
    const pending = page.getByRole('dialog', { name: '设置群管理员', exact: true })
    await pending.getByText('原生底座 · 操作', { exact: true }).waitFor()
    if (await pending.getByRole('button', { name: '进入受控验证 Tools', exact: true }).count())
      throw new Error('Unimplemented admin action exposed execution')
    await pending.getByRole('button', { name: '关闭详情', exact: true }).click()
    await table.getByRole('combobox', { name: '原生接入状态', exact: true })
      .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option', { name: '受控场景已核验', exact: true }).click()
    if (await table.getByRole('row').filter({ hasText: '发送群文本' }).count())
      throw new Error('Unaccepted action included in controlled filter')
    if (await table.getByRole('button', { name: '查看修改他人群昵称', exact: true }).count())
      throw new Error('Self nickname acceptance inherited by other-member child')
    if (await table.getByRole('button', { name: '查看修改原生昵称', exact: true }).count())
      throw new Error('Call-setting evidence inherited by another account setting')
    await table.getByRole('row').filter({ hasText: '语音 / 视频通话设置' })
      .getByText('指定场景可用', { exact: true }).waitFor()
    await table.getByRole('button', { name: '查看群主 / 管理员身份核验', exact: true }).waitFor()
    await table.getByRole('button', { name: '查看修改本人群昵称', exact: true }).click()
    const dialog = page.getByRole('dialog', { name: '修改本人群昵称', exact: true })
    await dialog.getByText('仅本人名片修改及恢复已核验。', { exact: true }).waitFor()
    await dialog.getByText('其他成员、自动规则及故障恢复未验收。', { exact: true }).waitFor()
    await dialog.getByText('合成自有账号、唯一隔离群；历史结果，不代表当前所选账号', { exact: true }).waitFor()
    if (!(await dialog.innerText()).includes('2026/10/31 23:28:24 · Asia/Shanghai'))
      throw new Error('Historical acceptance time drifted with browser timezone')
    for (const width of [320, 390, 647, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await page.waitForTimeout(150)
      await page.screenshot({ path: `output/playwright/native-acceptance-${width}.png`, animations: 'disabled' })
      if (!await dialog.evaluate(el => {
        const box = el.getBoundingClientRect()
        return box.left >= 0 && box.right <= innerWidth + 1 && box.top >= 0 && box.bottom <= innerHeight + 1 &&
          el.scrollWidth <= el.clientWidth + 1 && document.documentElement.scrollWidth <= innerWidth + 1
      })) throw new Error('Acceptance details overflow ' + width)
    }
    await dialog.getByRole('button', { name: '关闭详情', exact: true }).click()
    // Business modules moved to a group-scoped user view. The feature_rules
    // component regression retains their read-only/unimplemented boundary.
    if (await table.getByRole('button', { name: '查看群名片', exact: true }).count())
      throw new Error('Business module leaked into the native foundation catalog')
    await table.getByRole('combobox', { name: '原生接入状态', exact: true })
      .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option', { name: '全部接入状态', exact: true }).click()
    await table.getByRole('button', { name: '查看修改他人群昵称', exact: true }).click()
    const other = page.getByRole('dialog', { name: '修改他人群昵称', exact: true })
    if (await other.getByText('受控结果', { exact: true }).count())
      throw new Error('Parent historical result presented as other-member acceptance')
    await other.getByRole('button', { name: '关闭详情', exact: true }).click()
    await page.setViewportSize({ width: 1440, height: 960 })
    await boot('lajiaoliao')
    if (!(await table.getByTestId('native-controlled-count').innerText()).includes('0'))
      throw new Error('WSL acceptance leaked to Chili')
    await recallRow.getByText('已核对支持', { exact: true }).waitFor()
    await recallRow.getByText('部分接入', { exact: true }).waitFor()
    await recallRow.getByText('尚无通过记录', { exact: true }).waitFor()
    if (!(await table.getByTestId('native-usable-count').innerText()).includes('0') ||
      (await table.locator('.native-chart-legend').innerText()).includes('指定场景可用') ||
      await table.locator('.el-tag--success').count())
      throw new Error('Unverified Chili capability shown as green')
    if (writes) throw new Error('Viewing acceptance caused writes')
    return { passed: true, controlledGroups: 9, usableChecks: 10, productionAccepted: 0, scopedEvidence: true,
      independentChildren: 19, recallSupportSeparatedFromAcceptance: true,
      deferredExcluded: true, businessNativeBoundary: true, pendingReadOnly: true,
      platformIsolation: true, writes, native_actions: 0, viewports: [320, 390, 647, 1440] }
  } finally {
    await page.unroute('**/api/v1/**', handler)
  }
}
