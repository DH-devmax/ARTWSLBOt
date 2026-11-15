// All endpoints are synthetic; this check never contacts a native platform.
// prettier-ignore
(() => {
const smoke = async page => {
  page.setDefaultTimeout(10000)
  const id = n => `f8600000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const actor = id(1), account = id(2), group = id(3), windowID = id(4), application = '1700000000123'
  const applicant = '86002', self = '86001', name = '合成自有申请人'
  const check = (value, message) => { if (!value) throw Error(message) }
  let platform = 'wsl', variant = '', approved = false, used = 0, submits = 0, serial = 10
  const operations = [], creations = [], unexpected = [], screenshots = []
  const robot = () => ({
    id: account, name: '合成审批机器人', native_nickname: '原生审批机器人',
    platform, workspace: platform, purpose: 'management', auth_state: 'authenticated',
    testing_enabled: true, connection_enabled: true, epoch: 1, version: 1
  })
  const nativeGroup = () => ({
    group_id: group, native_id: '86003', name: '合成好友审批隔离群', platform,
    primary_account_id: account, management_enabled: false, ai_enabled: false
  })
  const grant = () => ({
    id: windowID, group_id: group, account_ids: [account], target_ids: [applicant],
    actions: ['friend_approve'], friend_application_id: application, admission_mode: 'native_debug',
    budget: 1, used, cancelled: variant === 'cancelled',
    expires_at: new Date(Date.now() + (variant === 'expired' ? -10000 : 300000)).toISOString(),
    ...(variant === 'mixed' ? { actions: ['friend_approve', 'group_invite'] } : {}),
    ...(variant === 'package' ? { admission_mode: 'package' } : {}),
    ...(variant === 'budget' ? { budget: 2 } : {}),
    ...(variant === 'missing_id' ? { friend_application_id: '' } : {})
  })
  const snapshot = () => {
    const row = {
      id: variant === 'wrong_id' ? '1700000000124' : application,
      user: { id: variant === 'wrong_target' ? '86004' : applicant, name },
      direction: variant === 'outgoing' ? 'FROM_ME' : 'FROM_HIM',
      state: approved || variant === 'processed' ? 'FRIENDLOG_STATE_AGREE' : 'FRIENDLOG_STATE_PROCESSING'
    }
    return {
      platform, self_id: self, self_role: variant === 'member' ? 'member' : 'admin',
      group_id: variant === 'wrong_group' ? '86004' : '86003',
      account_epoch: variant === 'epoch' ? 2 : 1, account_version: 1,
      directory_known: true, members_known: true, friends_known: true,
      in_group: true, member_ids: [self, applicant], invitations: [],
      applications: [row, ...(variant === 'ambiguous' ? [{ ...row, id: '1700000000124' }] : [])],
      friends: approved ? [{ id: applicant, name }] : [],
      sampled_at: new Date(Date.now() - (variant === 'stale' ? 35000 : 50)).toISOString()
    }
  }
  await page.route('**/api/v1/**', async route => {
    const request = route.request(), url = new URL(request.url())
    const path = url.pathname.replace('/api/v1', ''), method = request.method()
    const requestedPlatform = request.headers()['x-workspace'] || platform
    let data = []
    if (method === 'GET' && path === '/me')
      data = { user: { id: actor, username: 'synthetic-approval', role: 'superadmin' },
        permissions: ['overview:read', 'accounts:read', 'groups:read', 'tests:manage'] }
    else if (method === 'GET' && path === '/workspaces')
      data = { items: ['wsl', 'lajiaoliao'].map(id => ({ id, name: id === 'wsl' ? '旺商聊' : '辣椒聊' })) }
    else if (method === 'GET' && path === '/accounts') data = [robot()]
    else if (method === 'GET' && path === '/groups') data = [nativeGroup()]
    else if (method === 'GET' && path === '/overview') data = { accounts: 1, groups: 1, ai_groups: 0, pending_claims: 0 }
    else if (method === 'GET' && path === '/test-windows') data = [grant()]
    else if (method === 'GET' && path === '/test-tools/onboarding/scopes') data = { items: [] }
    else if (method === 'POST' && path === '/test-windows') {
      const input = request.postDataJSON()
      check(input.actions.join() === 'friend_approve' && input.account_ids.join() === account &&
        input.target_ids.join() === applicant && input.friend_application_id === application &&
        input.admission_mode === 'native_debug' && input.budget === 1 &&
        input.group_id === group && input.confirm_isolated && input.seconds <= 300,
        'Approval window widened its application or account/target/budget')
      creations.push(input); data = grant()
    } else if (method === 'GET' && path === '/test-tools')
      data = { tools: ['read_membership', 'run_social', 'inspect_operation'].map(name => ({ name })) }
    else if (method === 'GET' && path === '/test-tools/operations') data = operations
    else if (method === 'GET' && /^\/accounts\/[^/]+\/receiver$/.test(path))
      data = { enabled: true, observed: true, revision: 1, snapshot: { state: 'connected' } }
    else if (method === 'POST' && path === '/test-tools/call') {
      const input = request.postDataJSON(), args = input.arguments
      check(requestedPlatform === platform && args.account_id === account, 'Tool escaped platform/account')
      if (input.name === 'read_membership') {
        check(args.test_window_id === windowID, 'Read escaped persisted window')
        data = { snapshot: snapshot(), scope: { remaining: 1 - used }, read_only: true }
      } else if (input.name === 'run_social') {
        check(!variant && !used && args.test_window_id === windowID &&
          args.action === 'friend_approve' && args.target_id === applicant &&
          args.text === application && request.headers()['idempotency-key'] &&
          Object.keys(args).sort().join() === ['account_id', 'test_window_id', 'action', 'target_id', 'text'].sort().join(),
          'Approval lost exact ID, target, confirmation or one-attempt scope')
        used++; submits++; approved = true
        const operation = {
          id: id(++serial), kind: 'friend_approve', status: 'succeeded',
          result_code: 'native_friend_decision_acknowledged', test_window_id: windowID,
          group_id: group, created_at: new Date().toISOString()
        }
        operations.push(operation); data = { operation }
      } else if (input.name === 'inspect_operation')
        data = { operation: operations.find(op => op.id === args.operation_id), read_only: true }
      else throw Error('Unexpected approval tool ' + input.name)
    } else {
      unexpected.push({ path, method })
      return route.fulfill({ status: 500, json: { code: 1, msg: 'unmocked_api' } })
    }
    await route.fulfill({ json: { code: 0, msg: '', data } })
  })
  const tool = () => page.locator('.test-tools')
  const run = () => tool().getByRole('button', { name: '确认验证此动作', exact: true })
  const idle = () => page.waitForFunction(() => !document.querySelector('.test-tools .is-loading'))
  const navigate = async (value = '') => {
    variant = value
    await page.evaluate(({ actor, platform, account }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:console:${actor}`, 'admin')
      sessionStorage.setItem(`wsl:workspace:${actor}`, platform)
      sessionStorage.setItem(`wsl:account:${actor}:${platform}`, account)
    }, { actor, platform, account })
    await page.goto(`http://127.0.0.1:5173/?approval=${++serial}#tests`)
    await tool().locator('.tools-grant').first().waitFor(); await idle()
    await tool().locator('.tools-grant').first().click()
    await run().waitFor()
  }
  const choose = async (label, option, parent = tool()) => {
    await parent.getByRole('combobox', { name: label, exact: true })
      .locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
    await page.getByRole('option', { name: option, exact: true }).click()
    await page.getByRole('listbox').waitFor({ state: 'hidden' })
  }
  const read = async () => {
    await tool().getByRole('button', { name: '读取指定好友申请与群状态', exact: true }).click()
    await idle()
  }
  const fit = async (width, stage) => {
    await page.setViewportSize({ width, height: 820 })
    await page.waitForFunction(() => document.documentElement.scrollWidth <= innerWidth + 1)
    const dialog = page.getByRole('dialog', { name: '确认同意指定申请', exact: true })
    if (await dialog.isVisible()) {
      await page.waitForFunction(() => {
        const element = document.querySelector('.el-message-box')
        const box = element?.getBoundingClientRect()
        return box && box.x >= 0 && box.right <= innerWidth + 1 && box.bottom <= innerHeight
      })
    }
    const path = `output/playwright/friend-approve-${platform}-${stage}-${width}.png`
    await page.screenshot({ path, fullPage: true, animations: 'disabled' })
    screenshots.push(path)
  }
  await page.goto('http://127.0.0.1:5173/')
  for (platform of ['wsl', 'lajiaoliao']) {
    approved = false; used = 0; operations.length = 0
    const before = submits
    await navigate()
    check(await run().isDisabled(), 'Opening an approval enabled submission without fresh evidence')
    await read(); await choose('指定好友申请人', `${name} · ${applicant}`)
    await run().and(page.locator(':enabled')).waitFor()
    check(submits === before, 'Reading an application approved it')
    for (const width of [320, 390, 768, 1440]) await fit(width, 'ready')
    await run().click()
    const confirmation = page.getByRole('dialog', { name: '确认同意指定申请', exact: true })
    const content = await confirmation.innerText()
    check(content.includes(application) && content.includes(applicant) && content.includes(robot().native_nickname) &&
      content.includes(nativeGroup().name) && content.includes('不认领群组、不启用业务'),
      'Approval confirmation omitted scope or effect')
    for (const width of [320, 390, 768, 1440]) await fit(width, 'confirm')
    await confirmation.getByRole('button', { name: '取消', exact: true }).click()
    await confirmation.waitFor({ state: 'hidden' })
    check(submits === before, 'Cancel submitted an approval')
    await run().click()
    await confirmation.getByRole('button', { name: '确认提交', exact: true }).click()
    await tool().getByText('只读核对：已观察到期望状态。', { exact: true }).waitFor(); await idle()
    check(submits === before + 1 && used === 1 && await run().isDisabled(), 'Approval replayed or missed readback')
    await navigate()
    check(await run().isDisabled() && submits === before + 1, 'Reload replayed approval')
    used = 0; approved = false; operations.length = 0
    await navigate()
    await tool().getByRole('button', { name: '创建授权窗口', exact: true }).click()
    const editor = page.getByRole('dialog', { name: '创建授权窗口', exact: true })
    await editor.locator('label.el-checkbox').filter({ hasText: '同意指定好友申请（独立授权，一次尝试）' }).click()
    await choose('业务操作群组', nativeGroup().name, editor)
    await editor.getByRole('textbox', { name: '好友申请人 UID', exact: true }).fill(applicant)
    await editor.getByRole('textbox', { name: '好友申请编号', exact: true }).fill(application)
    await editor.locator('label.el-checkbox').filter({ hasText: '确认所选群为隔离测试群' }).click()
    await editor.getByRole('button', { name: '保存', exact: true }).click()
    await editor.waitFor({ state: 'hidden' })
    check(creations.at(-1)?.friend_application_id === application && submits === before + 1,
      'Creating a grant approved a friend')
    for (const value of ['wrong_id', 'wrong_target', 'outgoing', 'processed', 'member', 'epoch',
      'wrong_group', 'stale', 'missing_id', 'mixed', 'package', 'budget', 'expired', 'cancelled',
      ...(platform === 'lajiaoliao' ? ['ambiguous'] : [])]) {
      await navigate(value)
      if (!['missing_id', 'mixed', 'package', 'budget', 'expired', 'cancelled'].includes(value)) {
        await read()
        const input = tool().getByRole('combobox', { name: '指定好友申请人', exact: true })
        await input.locator('xpath=ancestor::div[contains(@class,"el-select__wrapper")]').click()
        const option = page.getByRole('option').filter({ hasText: applicant }).first()
        if (await option.evaluate(element => element.classList.contains('is-disabled'))) await page.keyboard.press('Escape')
        else await option.click()
      }
      check(await run().isDisabled() && submits === before + 1, 'Unsafe approval enabled: ' + value)
    }
  }
  check(!unexpected.length, 'Unmocked APIs: ' + JSON.stringify(unexpected))
  return { passed: true, synthetic_only: true, platforms: 2, approvals: submits, windows_created: creations.length, screenshots }
}
return smoke
})()
