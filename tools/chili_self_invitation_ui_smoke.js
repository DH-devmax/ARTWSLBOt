// Synthetic APIs only; the common runner blocks all native network requests.
async (page) => {
  const base = 'http://127.0.0.1:5173'
  const id = n => `73000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const actor = id(99), account = id(1), group = id(2), window = id(3)
  const fingerprint = 'a'.repeat(64)
  let present = false, used = 0, writes = 0
  const operation = { id: id(4), kind: 'group_accept', status: 'succeeded',
    result_code: 'native_invitation_accept_acknowledged', test_window_id: window, group_id: group }
  const handler = async route => {
    const req = route.request(), path = new URL(req.url()).pathname.replace('/api/v1', '')
    let data = []
    if (path === '/me') data = { user: { id: actor, role: 'superadmin', username: 'synthetic' },
      permissions: ['overview:read', 'accounts:read', 'groups:read', 'tests:manage'] }
    else if (path === '/workspaces') data = { items: [{ id: 'lajiaoliao', name: '辣椒聊机器人' }] }
    else if (path === '/accounts') data = [{ id: account, name: '受邀本人',
      platform: 'lajiaoliao', workspace: 'lajiaoliao', purpose: 'management',
      testing_enabled: true, connection_enabled: true, epoch: 1, version: 1 }]
    else if (path === '/groups') data = [{ group_id: group, native_id: 'group-string',
      platform: 'lajiaoliao', name: '辣椒隔离群', management_enabled: false, ai_enabled: false }]
    else if (path === '/test-windows') data = [{ id: window, group_id: group, account_ids: [account],
      target_ids: ['self-string'], actions: ['group_accept'], admission_mode: 'native_debug',
      expires_at: new Date(Date.now() + 300000).toISOString(), budget: 1, used }]
    else if (path === '/test-tools') data = { tools: [{ name: 'run_social', description: '接受本人邀请' }] }
    else if (path.endsWith('/receiver')) data = { enabled: true, observed: true,
      observed_at: new Date().toISOString(), snapshot: { state: 'connected' } }
    else if (path === '/test-tools/operations') data = writes ? [operation] : []
    else if (path === '/test-tools/call') {
      const { name, arguments: args } = req.postDataJSON()
      if (req.headers()['x-workspace'] !== 'lajiaoliao' || args.account_id !== account)
        throw Error('Chili invitation lost platform/account scope')
      if (name === 'read_membership') {
        if (args.test_window_id !== window) throw Error('Invitation read escaped its grant')
        data = { scope: { remaining: 1 - used }, snapshot: {
          platform: 'lajiaoliao', self_id: 'self-string', group_id: 'group-string',
          self_role: present ? 'member' : '', in_group: present, directory_known: true,
          members_known: present, friends_known: false, member_ids: present ? ['self-string'] : [],
          invitations_known: !present, invitations: present ? [] : [{
            id: fingerprint, group_id: 'group-string', type: 'INVITATION',
            state: 'MEMBER_STATE_INVITED', applicant: { id: 'self-string' },
            inviter: { id: 'owner-string', name: '自有群主' },
          }], sampled_at: new Date().toISOString(),
        } }
      } else if (name === 'run_social') {
        if (Object.keys(args).sort().join(',') !== 'account_id,action,target_id,test_window_id,text' ||
          args.test_window_id !== window || args.action !== 'group_accept' ||
          args.target_id !== 'self-string' || args.text !== fingerprint ||
          !req.headers()['idempotency-key'] || used !== 0)
          throw Error('Self invitation confirmation changed identity, record or budget')
        writes++; used++; present = true
        data = { operation }
      } else throw Error('Unexpected invitation tool: ' + name)
    } else if (req.method() !== 'GET') throw Error('Unexpected mutation: ' + path)
    await route.fulfill({ json: { code: 0, msg: '', data } })
  }
  await page.route('**/api/v1/**', handler)
  try {
    await page.evaluate(({ actor, account }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:workspace:${actor}`, 'lajiaoliao')
      sessionStorage.setItem(`wsl:account:${actor}:lajiaoliao`, account)
    }, { actor, account })
    await page.goto(base + '/#tests')
    await page.reload()
    await page.locator('.tools-grant').click()
    await page.getByText('消息接收：已连接', { exact: true }).waitFor()
    const run = page.getByRole('button', { name: '确认验证此动作', exact: true })
    if (!await run.isDisabled()) throw Error('Invitation submitted without current evidence')
    await page.getByRole('button', { name: '读取入退群状态', exact: true }).click()
    await page.getByText('本人在群：否', { exact: true }).waitFor()
    await page.getByRole('combobox', { name: '本人的待处理邀请' }).press('ArrowDown')
    await page.getByRole('option', { name: '自有群主 · ' + fingerprint, exact: true }).click()
    for (const width of [320, 390, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      if (!await page.locator('.test-tools').evaluate(el => el.scrollWidth <= el.clientWidth + 1))
        throw Error('Chili invitation overflow ' + width)
      await page.screenshot({ path: `output/playwright/chili-self-invitation-${width}.png`, fullPage: true })
    }
    await run.click()
    await page.getByRole('button', { name: '取消', exact: true }).click()
    if (writes) throw Error('Closing confirmation accepted invitation')
    await run.click()
    await page.getByRole('button', { name: '确认提交', exact: true }).click()
    await page.getByText('本人在群：是', { exact: true }).waitFor()
    await page.getByText('只读核对：已观察到期望状态。', { exact: true }).waitFor()
    if (writes !== 1 || !await run.isDisabled()) throw Error('Acceptance repeated or missed membership evidence')
    return { passed: true, platform: 'lajiaoliao', string_identity: true, synthetic_submissions: 1,
      viewports: 4, native_requests: 0 }
  } finally { await page.unroute('**/api/v1/**', handler) }
}
