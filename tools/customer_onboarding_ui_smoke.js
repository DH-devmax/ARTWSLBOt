// Built product frontend with synthetic responses only; no native or model calls.
// prettier-ignore
async (page) => {
  page.setDefaultTimeout(10000)
  const captcha = await process.getBuiltinModule('vm').runInThisContext(
    `(${process.getBuiltinModule('fs').readFileSync('tools/web_captcha_ui_smoke.js', 'utf8')})`,
    { filename: 'web_captcha_ui_smoke.js' }
  )(page, { fixtureOnly: true })
  const base = 'http://127.0.0.1:5173'
  const id = n => `cf000000-0000-4000-8000-${String(n).padStart(12, '0')}`
  const token = 'A'.repeat(43), password = 'synthetic-only-password'
  const legacyFriendProof = platform => `群接入 ${(platform === 'wsl' ? 'A' : 'B').repeat(20)}`
  const privateProof = platform => `群接入 ${(platform === 'wsl' ? 'p' : 'q').repeat(43)}`
  const now = new Date().toISOString(), expires = new Date(Date.now() + 600000).toISOString()
  const owner = { id: id(1), tenant_id: id(2), owner_id: id(1), username: 'customer-owner', role: 'user', tenant_role: 'owner', tenant_name: '合成客户' }
  const admin = { id: id(9), username: 'synthetic-admin', role: 'superadmin' }
  const reader = { id: id(8), tenant_id: id(2), username: 'customer-reader', role: 'user', tenant_role: 'viewer' }
  const otherOwner = { ...owner, id: id(11), owner_id: id(11), tenant_id: id(12), username: 'second-owner', tenant_name: '第二客户' }
  const space = () => mode === 'admin'
    ? { id: id(90), name: '我的演示工作空间', kind: 'operator_demo', owner_id: admin.id }
    : { id: owner.tenant_id, name: owner.tenant_name, kind: 'tenant_business', owner_id: owner.id }
  const plan = () => mode === 'admin'
    ? { id: 'operator_unlimited', name: '超管专属 · 不限额', active: true, system_only: true,
      permanent: true, unlimited: true, platforms: [], group_limit: null, member_limit: null, assistant_limit: null, credit_limit: null }
    : { id: id(91), name: 'PRO', active: true, system_only: false, permanent: false, unlimited: false,
      group_limit: 300, member_limit: 10, assistant_limit: 300, credit_limit: 1000000,
      expires_at: expires, platforms: ['wsl', 'lajiaoliao'], modules: ['management_assistant', 'moderation', 'replies'] }
  const specs = [
    ['overview:read', '查看工作台', ''],
    ['accounts:read', '查看管理机器人状态', ''],
    ['groups:read', '查看已授权群', ''],
    ['groups:configure', '配置群管与自动化', 'management'],
    ['members:manage', '管理租户成员（仅主账号）', ''],
    ['claims:create', '申请认领群组', ''],
    ['claims:read', '查看群授权申请', ''],
    ['operations:read', '查看操作记录', ''],
    ['assistant:use', '使用群管助手', 'management_assistant'],
    ['assistant:propose', '生成群管配置建议', 'management_assistant'],
    ['assistant:approve', '审批本人群管配置计划', 'management_assistant'],
    ['assistant:action_propose', '生成群管动作建议', 'management_assistant'],
    ['assistant:action_approve', '确认本人群管动作', 'management_assistant'],
    ['group_action:mute_member', '成员禁言', 'moderation'],
    ['group_action:kick_member', '移出成员', 'moderation']
  ].map(([code, label, module]) => ({ code, label, module, assignable: true }))
  const grants = specs.map(s => s.code)
  const ceiling = grants.filter(code => code !== 'group_action:kick_member')
  const action = { id: 'mute_member', name: '成员禁言', native_id: 'mute_member', module: 'moderation', permission: 'group_action:mute_member', product_scope: 'included', native_role: 'admin', implemented: true, production_admitted: false, block_reason: 'production_not_admitted', sources: ['assistant', 'automatic'] }
  const groups = Array.from({ length: 300 }, (_, n) => ({
    group_id: id(100 + n), tenant_id: owner.tenant_id, name: n < 2 ? '测试群' : `合成授权群 ${String(n).padStart(3, '0')}`,
    native_id: `native-group-${n}`, platform: n % 2 ? 'lajiaoliao' : 'wsl', active: true, version: 1,
    primary_account_id: id(1000 + n % 10), robot_nickname: n % 2 ? '辣椒原生机器人' : '旺商原生机器人',
    account_name: n % 2 ? '辣椒原生机器人' : '旺商原生机器人', activity: 10, start_minute: 0, end_minute: 1439,
    preference_version: 0, ai_enabled: false
  }))
  const claims = [], reviewChallenges = new Map(), reviewConflicts = new Set()
  for (let n = 0; n < 2; n++) {
    for (const [k, kind] of ['manual', 'message', 'transfer'].entries()) {
      const group = groups[n], challengeID = id(950 + n * 3 + k)
      const row = { id: id(900 + n * 3 + k), tenant_id: owner.tenant_id, group_id: group.group_id, account_id: group.primary_account_id, group_name: `${kind} · 测试群`, platform: group.platform, native_identity: `synthetic-${kind}`, state: 'pending', created_at: now, proof_method: kind === 'manual' ? 'manual' : kind === 'transfer' ? 'controlled_test' : 'authenticated_message', onboarding_challenge_id: kind === 'manual' ? null : challengeID }
      claims.push(row)
      if (kind !== 'manual') reviewChallenges.set(challengeID, { id: challengeID, platform: group.platform, state: 'review', version: 4, expires_at: expires, robot: { id: group.primary_account_id, name: group.robot_nickname, native_id: 'review-robot-native' }, group: { id: group.group_id, name: '测试群', native_id: group.native_id }, proof_role: kind === 'transfer' ? 'owner' : 'admin', identity_proven: true, can_confirm: false, can_cancel: false, can_accept_invitation: false, blocked_reason: '', transfer_required: kind === 'transfer', ...(kind === 'transfer' ? { transfer: { source_tenant_id: otherOwner.tenant_id, source_tenant_name: otherOwner.tenant_name, binding_version: 7 } } : {}) })
    }
  }
  let mode = 'owner', authenticated = true, previewMode = 'new', groupCount = 300
  let challenge, challengeSerial = 70, contactSerial = 3000, runtimeConflict = false, ceilingConflict = false, releaseRuntime
  let delayReviewID = '', releaseReview
  let delayRuntime = false, runtimeVersion = 1, runtimeEnabled = false, ceilingVersion = 1
  let managementEnabled = false
  let selectedPermissions = [...ceiling], gateState = 'closed', gateVersion = 0, pilotIDs = [], evidence = []
  const writes = [], reads = [], screenshots = [], failures = []
  const members = [{ ...owner, access_version: 1, disabled: false, groups: [], overrides: [] }, { ...reader, access_version: 1, disabled: false, groups: [groups[0].group_id], overrides: [] }]
  const templates = [
    { code: 'assistant_reader', name: '只读', version: 3, permissions: ['assistant:use'] },
    { code: 'assistant_action', name: '群管本人确认', version: 3, permissions: ['assistant:use', 'assistant:action_propose', 'assistant:action_approve'] }
  ]
  const me = () => mode === 'admin' ? admin : mode === 'member' ? reader : owner
  const effective = () => mode === 'admin' ? [...grants, 'users:manage', 'plans:manage', 'workspaces:maibot'] : mode === 'member' ? ['overview:read', 'groups:read', 'assistant:use'] : grants
  const progress = record => ({ ...record, group_challenge: undefined, private_challenge: undefined })
  const contact = record => ({
    id: id(++contactSerial), version: record.version, status: 'prepared', result_code: '',
    can_approve: true, can_reconcile: false,
    name: `${record.platform === 'wsl' ? '旺商聊' : '辣椒聊'}本人`, requested_at: now
  })
  const handler = async route => {
    if (await captcha.handle(route)) return
    const req = route.request(), url = new URL(req.url()), path = url.pathname.replace('/api/v1', '')
    captcha.consume(req)
    const method = req.method(), workspace = req.headers()['x-workspace'] || 'wsl'
    let data = [], status = 200
    if (url.searchParams.has('token')) failures.push('Invitation token appeared in API URL')
    if (method === 'GET') {
      reads.push({ path, workspace, tenant: url.searchParams.get('tenant_id'), group: url.searchParams.get('group_id') })
      if (path === '/me') {
        if (!authenticated) return route.fulfill({ status: 401, json: { code: 1, msg: 'authentication_required' } })
        data = { user: me(), permissions: effective() }
      } else if (path === '/workspaces') data = { items: [{ id: 'user', name: '用户中心' }, { id: 'wsl', name: '旺商聊机器人' }, { id: 'lajiaoliao', name: '辣椒聊机器人' }] }
      else if (path === '/user-center') data = {
        workspace: space(), effective_plan: plan(),
        billing_source: mode === 'admin' ? 'platform_demo' : 'tenant_wallet',
        quotas: { groups: plan().group_limit, members: plan().member_limit, assistant: plan().assistant_limit, credits: plan().credit_limit },
        entitlement: { mode: mode === 'admin' ? 'operator_demo' : 'tenant_business', active: true, permissions: effective() },
        subscription: mode === 'admin' ? null : { ...plan(), ai_group_limit: 300 },
        groups: mode === 'member' ? [groups[0]] : groups.slice(0, groupCount), personas: [], permissions: effective(), claims: [],
        operations: [{ id: id(40), source: 'automatic', kind: 'mute_member', status: 'unknown', result_code: 'operation_pending_reconcile', group_id: groups[0].group_id, account_id: groups[0].primary_account_id, created_at: now, updated_at: now }]
      }
      else if (path === '/user-center/groups') {
        const pageSize = Number(url.searchParams.get('page_size') || 20)
        const currentPage = Number(url.searchParams.get('page') || 1)
        if (pageSize > 50 || currentPage < 1) failures.push('Unbounded group query')
        const available = mode === 'member' ? [groups[0]] : groups.slice(0, groupCount)
        const filtered = available.filter(g => g.platform === url.searchParams.get('platform') &&
          (!url.searchParams.get('search') || g.name.includes(url.searchParams.get('search'))))
        data = { workspace: space(), effective_plan: plan(), items: filtered.slice((currentPage - 1) * pageSize, currentPage * pageSize),
          total: filtered.length, page: currentPage, page_size: pageSize, sampled_at: now }
      }
      else if (path === '/user-center/usage') data = { workspace: space(), effective_plan: plan(),
        billing_source: mode === 'admin' ? 'platform_demo' : 'tenant_wallet',
        unit: 'micro_credit', currency: 'USDT', currency_scale: 1000000000,
        call_count: 0, actual_micro: 0, held_micro: 0, sampled_at: now }
      else if (path === '/group-binding/context') data = {
        mode: mode === 'admin' ? 'operator_demo' : 'tenant_business', workspace: space(),
        default_target_tenant_id: space().id, can_onboard_for_customer: mode === 'admin',
        customers: mode === 'admin' ? [{ tenant_id: owner.tenant_id, name: owner.tenant_name }] : [], next_offset: -1 }
      else if (path === '/users') data = [owner, otherOwner, reader]
      else if (path === '/claims') data = claims.filter(row => row.platform === workspace)
      else if (path === '/groups') data = groups.slice(0, groupCount).filter(g => g.platform === workspace)
      else if (path === '/accounts') data = [{ id: groups[0].primary_account_id, workspace, platform: workspace, purpose: 'management', name: '网站备注', native_nickname: '原生机器人' }]
      else if (path === '/overview') data = { accounts: 1, groups: groupCount, limits: { accounts: 10, groups: 300 } }
      else if (/^\/group-binding\/challenges\/[^/]+$/.test(path)) {
        const challengeID = path.split('/')[3]
        const record = reviewChallenges.get(challengeID) || challenge
        if (record?.id !== challengeID || record.platform !== workspace) throw Error('Challenge read escaped its platform/record')
        data = progress(record)
        if (delayReviewID === challengeID) { delayReviewID = ''; await new Promise(resolve => { releaseReview = resolve }) }
      }
      else if (/^\/groups\/[^/]+\/business-runtime$/.test(path)) {
        const groupID = path.split('/')[2]
        data = { group_id: groupID, tenant_id: owner.tenant_id, enabled: runtimeEnabled, management_enabled: managementEnabled, version: runtimeVersion, can_manage: mode !== 'member', blocked_reason: runtimeEnabled ? '' : 'group_runtime_disabled', sampled_at: now }
        if (delayRuntime) { delayRuntime = false; await new Promise(resolve => { releaseRuntime = resolve }) }
      } else if (/^\/groups\/[^/]+\/capabilities$/.test(path)) data = { group_id: path.split('/')[2], tenant_id: owner.tenant_id, platform: workspace, enabled: runtimeEnabled, version: runtimeVersion, permissions: ceiling, permission_catalog: specs, ceiling, can_manage: mode !== 'member', blocked_reason: runtimeEnabled ? '' : 'group_runtime_disabled', actions: [action], sampled_at: now }
      else if (/^\/tenants\/[^/]+\/capabilities$/.test(path)) data = { tenant_id: path.split('/')[2], permissions: selectedPermissions, ceiling: selectedPermissions, permission_catalog: specs, version: ceilingVersion }
      else if (path === '/capabilities/catalog') data = { platform: workspace, permissions: specs, actions: [action, { ...action, id: 'unsupported_action', name: '尚未实现的动作', implemented: false }], assistant_templates: templates, gate_states: ['closed', 'pilot', 'open'], member_management: 'owner_only' }
      else if (path.startsWith('/capabilities/gates/')) data = { platform: workspace, action: path.split('/')[3], source: path.split('/')[4], state: gateState, version: gateVersion, pilot_group_ids: pilotIDs, evidence_refs: evidence }
      else if (path === '/members' || path === '/member-roles') {
        if (mode === 'admin' && !url.searchParams.get('tenant_id')) failures.push('Admin member query has no selected tenant')
        const tenant = url.searchParams.get('tenant_id') || owner.tenant_id
        data = path === '/members' ? (tenant === owner.tenant_id ? members : [{ ...otherOwner, access_version: 1, groups: [], overrides: [] }]) : {
          tenant_id: tenant, roles: [{ code: 'viewer', name: '只读', permissions: ['groups:read'], version: 1 }],
          permissions: specs, capability_ceiling: ceiling, member_management: 'owner_only', assistant_templates: templates,
          groups: tenant === owner.tenant_id ? groups.slice(0, groupCount) : []
        }
      } else if (path === '/assistant/context') {
        const target = groups.find(g => g.group_id === url.searchParams.get('group_id') && g.platform === workspace)
        data = { platform: workspace, groups: groups.filter(g => g.platform === workspace).slice(0, 30), total: 150, next_offset: 30, permissions: effective(),
          ...(target ? { scope: { platform: workspace, group_id: target.group_id, account_id: target.primary_account_id } } : {}) }
      } else if (path === '/assistant/usage') data = { unit: 'micro_credit', used_or_reserved: 0, timezone: 'Asia/Shanghai' }
    } else {
      const body = req.postDataJSON() || {}
      writes.push({ path, method, workspace, tenant: url.searchParams.get('tenant_id'), body, key: req.headers()['idempotency-key'] })
      if (path === '/customer-invitations/preview') data = { id: id(50), mode: previewMode, state: 'pending', expires_at: expires }
      else if (path === '/customer-invitations/activate') {
        if (Object.keys(body).sort().join() !== 'password,token,username' || body.token !== token) failures.push('Wrong public activation shape')
        data = { state: 'accepted', user_id: id(60), tenant_id: id(61) }; status = 201
      } else if (path === '/customer-invitations/accept') {
        if (!authenticated || previewMode !== 'existing' || Object.keys(body).join() !== 'token') failures.push('Existing invitation bypassed website login')
        data = { state: 'accepted', user_id: owner.id, tenant_id: owner.tenant_id }
      } else if (path === '/auth/login') { authenticated = true; data = { user: me(), permissions: effective() } }
      else if (path === '/auth/logout') { authenticated = false; data = {} }
      else if (path === '/customer-invitations') {
        if (mode !== 'admin' || !req.headers()['idempotency-key'] || 'password' in body || 'username' in body) failures.push('Wrong invitation creation shape')
        data = { id: id(51), mode: body.mode, state: 'pending', expires_at: expires, token }; status = 201
      } else if (path === '/group-binding/challenges') {
        if (Object.keys(body).join() !== 'platform' || body.platform !== workspace || !req.headers()['idempotency-key']) failures.push('Untrusted onboarding identity or missing idempotency key')
        challenge = { id: id(++challengeSerial), platform: workspace, friend_flow: 'incoming_contact', state: 'waiting', version: 1, expires_at: expires,
          mode: 'tenant_business', workspace: space(), customer: { tenant_id: owner.tenant_id, name: owner.tenant_name },
          robot: { id: groups.find(g => g.platform === workspace).primary_account_id, name: workspace === 'wsl' ? '旺商原生机器人' : '辣椒原生机器人', native_id: 'robot-native-id',
            add_account: `public-${workspace}-robot`, add_available: true },
          // A legacy response field must never become visible or an actionable proof.
          friend_challenge: legacyFriendProof(workspace), can_verify_friend: false,
          group_challenge: '群绑定 SYNTHETIC_GROUP_PROOF', private_challenge: privateProof(workspace),
          identity_proven: false, can_confirm: false, can_cancel: true, can_accept_invitation: false, blocked_reason: '' }
        data = challenge; status = 201
      } else if (/^\/group-binding\/challenges\/[^/]+\/friend-candidate$/.test(path)) {
        if (challenge?.id !== path.split('/')[3] || challenge.platform !== workspace ||
          challenge.friend_flow !== 'incoming_contact' || challenge.state !== 'waiting' || challenge.identity_proven ||
          challenge.friend_contact || challenge.friend_attempt || body.version !== challenge.version ||
          Object.keys(body).sort().join() !== 'nickname,version' || body.nickname !== `${workspace === 'wsl' ? '旺商聊' : '辣椒聊'}本人` ||
          !req.headers()['idempotency-key'] || req.headers()['x-human-verification'])
          failures.push('Friend candidate escaped exact nickname/platform/version/key or supplied client time/identity')
        if (challenge.blocked_reason === 'group_binding_pool_identity')
          return route.fulfill({ status: 409, json: { code: 1, msg: 'group_binding_pool_identity' } })
        challenge = { ...challenge, friend_contact: contact(challenge) }
        data = progress(challenge)
      } else if (/^\/group-binding\/challenges\/[^/]+\/friend-approve$/.test(path)) {
        if (challenge?.id !== path.split('/')[3] || challenge.platform !== workspace ||
          challenge.state !== 'waiting' || challenge.identity_proven || challenge.friend_contact?.status !== 'prepared' ||
          !challenge.friend_contact.can_approve || challenge.friend_attempt || body.version !== challenge.version ||
          Object.keys(body).sort().join() !== 'operation_id,version' || body.operation_id !== challenge.friend_contact.id ||
          !req.headers()['idempotency-key'])
          failures.push('Friend approval changed its selected operation/version/key or replayed')
        challenge = { ...challenge, friend_contact: { ...challenge.friend_contact, status: 'unknown',
          result_code: 'group_binding_friend_unknown', can_approve: false, can_reconcile: true } }
        data = progress(challenge)
      } else if (/^\/group-binding\/challenges\/[^/]+\/friend-attempts\/[^/]+\/reconcile$/.test(path)) {
        if (challenge?.id !== path.split('/')[3] || challenge.platform !== workspace ||
          challenge.friend_contact?.id !== path.split('/')[5] || !challenge.friend_contact.can_reconcile ||
          Object.keys(body).length)
          failures.push('Friend reconciliation changed the original operation/platform')
        challenge = { ...challenge, friend_contact: { ...challenge.friend_contact, status: 'succeeded',
          result_code: '', can_approve: false, can_reconcile: false } }
        data = { id: challenge.friend_contact.id, version: challenge.friend_contact.version, status: 'succeeded', result_code: '' }
      } else if (/^\/group-binding\/challenges\/[^/]+\/invitation-prepare$/.test(path)) {
        if (!challenge.identity_proven || !challenge.can_accept_invitation || body.version !== challenge.version || body.native_group_id !== groups.find(g => g.platform === workspace).native_id) failures.push('Invitation lookup bypassed authenticated identity proof')
        data = { native_group_id: body.native_group_id, invitation_id: 'verified-native-invitation', group_name: '测试群', expires_at: expires, version: challenge.version }
      } else if (/^\/group-binding\/challenges\/[^/]+\/invitation-accept$/.test(path)) {
        if (body.invitation_id !== 'verified-native-invitation' || body.version !== challenge.version || !req.headers()['idempotency-key']) failures.push('Invitation submitted without verified preview/idempotency key')
        data = { id: id(75), status: 'unknown', result_code: 'native_transport_unknown' }
        challenge = { ...challenge, can_accept_invitation: false, accept_attempt: data }
      } else if (/^\/group-binding\/challenges\/[^/]+\/accept-attempts\/[^/]+\/reconcile$/.test(path)) {
        data = { id: id(75), status: 'succeeded', result_code: 'native_invitation_accept_acknowledged' }
        challenge = { ...challenge, accept_attempt: data }
      } else if (/^\/group-binding\/challenges\/[^/]+\/(?:transfer-review|review)$/.test(path)) {
        const challengeID = path.split('/')[3], value = reviewChallenges.get(challengeID)
        if (!value || mode !== 'admin' || value.platform !== workspace || body.version !== value.version || typeof body.approve !== 'boolean') failures.push('Proof review escaped challenge/version/admin scope')
        if (value.transfer_required) {
          if (!path.endsWith('/transfer-review') || !req.headers()['idempotency-key'] || body.source_tenant_id !== value.transfer.source_tenant_id || body.binding_version !== value.transfer.binding_version || Object.keys(body).sort().join() !== 'approve,binding_version,source_tenant_id,version') failures.push('Transfer review omitted verified source/version/key')
        } else if (!path.endsWith('/review') || Object.keys(body).sort().join() !== 'approve,version') failures.push('Proof review used legacy or excess fields')
        if (reviewConflicts.delete(challengeID)) {
          value.version++
          return route.fulfill({ status: 409, json: { code: 1, msg: 'group_binding_review_expired' } })
        }
        value.version++; value.state = body.approve ? 'bound' : 'rejected'
        if (body.approve) { runtimeEnabled = managementEnabled = false; runtimeVersion++ }
        claims.find(row => row.onboarding_challenge_id === challengeID).state = body.approve ? 'approved' : 'rejected'
        data = { ...value }
      } else if (/^\/claims\/[^/]+\/review$/.test(path)) {
        const row = claims.find(row => row.id === path.split('/')[2])
        if (mode !== 'admin' || row?.proof_method !== 'manual' || !body.identity_verified) failures.push('Authenticated proof used legacy claim review')
        row.state = body.approve ? 'approved' : 'rejected'; data = {}
      } else if (/^\/group-binding\/challenges\/[^/]+\/confirm$/.test(path)) {
        if (!challenge?.can_confirm || body.version !== challenge.version || !req.headers()['idempotency-key']) failures.push('Group confirmed without server proof/version/key')
        challenge = { ...challenge, state: 'bound', version: challenge.version + 1, can_confirm: false, can_cancel: false }
        runtimeEnabled = managementEnabled = false; runtimeVersion++
        data = challenge
      } else if (/^\/group-binding\/challenges\/[^/]+\/cancel$/.test(path)) {
        if (path.split('/')[3] !== challenge.id || !challenge.can_cancel ||
          body.version !== challenge.version || Object.keys(body).join() !== 'version')
          failures.push('Cancellation lost current challenge/version')
        challenge = { ...challenge, state: 'cancelled', version: challenge.version + 1, can_cancel: false, can_confirm: false }; data = challenge
      } else if (/^\/groups\/[^/]+\/business-runtime$/.test(path)) {
        if (runtimeConflict) { runtimeConflict = false; runtimeVersion++; return route.fulfill({ status: 409, json: { code: 1, msg: 'version_conflict' } }) }
        if (body.version !== runtimeVersion || Object.keys(body).sort().join() !== 'enabled,version' || mode === 'member') failures.push('Runtime CAS/ownership mismatch')
        runtimeEnabled = managementEnabled = body.enabled; runtimeVersion++; data = { group_id: path.split('/')[2], enabled: runtimeEnabled, management_enabled: managementEnabled, version: runtimeVersion, sampled_at: now }
      } else if (/^\/tenants\/[^/]+\/capabilities$/.test(path)) {
        if (ceilingConflict) { ceilingConflict = false; ceilingVersion++; return route.fulfill({ status: 409, json: { code: 1, msg: 'version_conflict' } }) }
        if (mode !== 'admin' || body.version !== ceilingVersion) failures.push('Ceiling CAS/admin mismatch')
        ceilingVersion++; selectedPermissions = body.permissions; data = { tenant_id: owner.tenant_id, permissions: selectedPermissions, ceiling: selectedPermissions, version: ceilingVersion }
      } else if (path.startsWith('/capabilities/gates/')) {
        if (mode !== 'admin' || body.version !== gateVersion || !Array.isArray(body.evidence_refs)) failures.push('Gate metadata/version mismatch')
        gateState = body.state; pilotIDs = body.pilot_group_ids; evidence = body.evidence_refs; gateVersion++
        data = { platform: workspace, action: path.split('/')[3], source: path.split('/')[4], state: gateState, version: gateVersion, pilot_group_ids: pilotIDs, evidence_refs: evidence }
      } else if (path === '/members' || path.startsWith('/members/') || path.startsWith('/member-roles/')) {
        if (mode === 'member' || (mode === 'admin' && !url.searchParams.get('tenant_id')) || 'tenant_id' in body) failures.push('Member mutation escaped owner/tenant scope')
        data = { id: id(80), version: 2 }
      } else failures.push(`Unexpected model/native write: ${method} ${path}`)
    }
    await route.fulfill({ status, json: { code: 0, msg: '', data } })
  }
  const dialog = name => page.getByRole('dialog', { name, exact: true })
  const writeCount = path => writes.filter(w => w.path === path).length
  const navigate = async (key, workspace = 'wsl') => {
    await page.goto(base)
    await page.evaluate(({ actor, workspace, admin }) => {
      sessionStorage.clear()
      sessionStorage.setItem(`wsl:workspace:${actor}`, workspace)
      sessionStorage.setItem(`wsl:console:${actor}`, admin ? 'admin' : 'user')
    }, { actor: me().id, workspace, admin: mode === 'admin' })
    await page.goto(`${base}/#${key}`); await page.reload()
    await page.locator('.platform-shell').waitFor()
  }
  const choose = async (select, label) => {
    await select.locator('xpath=ancestor::div[contains(concat(" ",normalize-space(@class)," ")," el-select__wrapper ")][1]').click()
    await page.locator('.el-select-dropdown:visible').getByRole('option', { name: label, exact: true }).click()
  }
  const screenshot = async (label, width) => {
    await page.setViewportSize({ width, height: 900 })
    await page.waitForTimeout(70)
    if (await page.evaluate(() => document.documentElement.scrollWidth > innerWidth + 1)) throw Error(`Customer UI overflow ${label}/${width}`)
    const file = `output/playwright/customer-${label}-${width}.png`
    await page.screenshot({ path: file, fullPage: true, animations: 'disabled' }); screenshots.push(file)
  }
  const assertFooter = async (modal, buttonName, width) => {
    await page.setViewportSize({ width, height: 769 })
    await page.waitForTimeout(60)
    const button = modal.getByRole('button', { name: buttonName, exact: true })
    const box = await button.boundingBox()
    if (!box || box.x < -1 || box.x + box.width > width + 1 || box.y < -1 || box.y + box.height > 770) throw Error(`Scoped dialog footer hidden ${buttonName}/${width}`)
    if (width === 320 || width === 1440) {
      const file = `output/playwright/customer-dialog-${buttonName}-${width}.png`
      await page.screenshot({ path: file, animations: 'disabled' })
      if (!screenshots.includes(file)) screenshots.push(file)
    }
  }
  const assertNoSecrets = async () => {
    const storage = await page.evaluate(() => JSON.stringify({ local: { ...localStorage }, session: { ...sessionStorage } }))
    if ([token, password, 'SYNTHETIC_GROUP_PROOF', ...['wsl', 'lajiaoliao'].flatMap(platform =>
      [legacyFriendProof(platform), privateProof(platform), `INTERNAL_TARGET_${platform}`, `self-${platform}`])]
      .some(secret => storage.includes(secret)))
      throw Error('Invitation/password/proof persisted in browser storage')
  }
  const assertNoLegacyFriendFlow = async modal => {
    if (await modal.getByRole('radio', { name: '发送好友申请', exact: true }).count() ||
      await modal.getByRole('button', { name: /^(复制好友核验内容|已发送，开始核验)$/ }).count() ||
      await modal.getByText(legacyFriendProof(challenge.platform), { exact: true }).count())
      throw Error('Retired note proof or outgoing controls remained visible')
    if (await modal.getByRole('button', { name: /查找我的账号|让机器人添加我/ }).count() ||
      await modal.getByRole('textbox', { name: '本人可搜索账号', exact: true }).count())
      throw Error('Outgoing account search/application is still available')
  }
  const selectFriendPath = async (modal, label) => {
    await modal.getByRole('radiogroup', { name: '选择好友核验方式', exact: true })
      .locator('label.el-radio-button').filter({ hasText: label }).click()
    if (!await modal.getByRole('radio', { name: label, exact: true }).isChecked())
      throw Error('Customer friend path selection was not retained')
  }
  const refreshOnboarding = async modal => {
    await modal.getByRole('button', { name: '刷新进度', exact: true }).click()
    await page.waitForFunction(() => !document.querySelector('.onboarding-dialog .el-dialog__footer .is-loading'))
  }
  const assertFriendPending = async modal => {
    if (!['添加与核对', '私聊身份核验'].includes(await modal.locator('.onboarding-steps .is-current strong').innerText()) ||
      await modal.getByText('好友身份已核验，可邀请机器人入群', { exact: true }).count() ||
      await modal.getByRole('textbox', { name: '目标群原生标识', exact: true }).count() ||
      await modal.getByRole('button', { name: /^(确认绑定|提交审核|核对群邀请)$/ }).count())
      throw Error('Customer advanced without current authenticated identity proof')
  }
  const beginOnboarding = async platform => {
    await page.getByRole('button', { name: '接入群组', exact: true }).click()
    const modal = dialog('接入我的群组')
    await modal.getByRole('radiogroup', { name: '选择接入平台', exact: true })
      .locator('label.el-radio-button').filter({ hasText: platform === 'wsl' ? '旺商聊' : '辣椒聊' }).click()
    await modal.getByRole('button', { name: '开始核验', exact: true }).click()
    await captcha.complete()
    await modal.getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
    await modal.getByText(`public-${platform}-robot`, { exact: true }).waitFor()
    await modal.getByRole('button', { name: '复制机器人加好友账号', exact: true }).waitFor()
    await assertNoLegacyFriendFlow(modal)
    return modal
  }
  await page.route('**/api/v1/**', handler)
  try {
    authenticated = false
    for (const width of [320, 390, 768, 1440]) {
      await page.setViewportSize({ width, height: 900 })
      await page.goto(`${base}/#customer-invitation?token=${token}`)
      await page.getByRole('textbox', { name: '网站用户名', exact: true }).waitFor()
      if (page.url().includes(token)) throw Error('Invitation token retained in address')
      await assertNoSecrets()
      await screenshot('activation', width)
    }
    await page.getByRole('textbox', { name: '网站用户名', exact: true }).fill('new-customer')
    await page.getByRole('textbox', { name: '设置网站密码', exact: true }).fill(password)
    await page.getByRole('textbox', { name: '确认密码', exact: true }).fill(password)
    if (writeCount('/customer-invitations/activate')) throw Error('Activation autosubmitted')
    await page.getByRole('button', { name: '接受邀请并设置密码', exact: true }).click()
    await page.getByRole('heading', { name: '邀请已接受', exact: true }).waitFor()
    if (writeCount('/customer-invitations/activate') !== 1 || writeCount('/auth/login')) throw Error('Activation repeated or logged in automatically')
    await assertNoSecrets()

    previewMode = 'existing'
    await page.goto(`${base}/?invite=existing#customer-invitation?token=${token}`)
    await page.getByRole('button', { name: '前往登录', exact: true }).waitFor()
    if (writeCount('/customer-invitations/accept')) throw Error('Existing invitation accepted anonymously')
    authenticated = true
    await page.goto(`${base}/?invite=existing-signed-in#customer-invitation?token=${token}`)
    await page.getByRole('button', { name: '接受邀请', exact: true }).click()
    await page.getByRole('heading', { name: '邀请已接受', exact: true }).waitFor()
    if (writeCount('/customer-invitations/accept') !== 1) throw Error('Existing invitation not accepted once')

    await navigate('overview')
    await page.getByRole('heading', { name: '我的群组', exact: true }).waitFor()
    for (const width of [320, 390, 768, 1440]) await screenshot('groups-300', width)
    if (await page.locator('.uc-groups .el-table__body tr').count() > 20 || await page.locator('.uc-group-row').count() > 20) throw Error('300 groups rendered without bounded pagination')
    const cards = page.locator('.uc-group-row')
    await cards.first().waitFor({ state: 'attached' })
    if (!(await cards.first().textContent()).includes('旺商原生机器人') ||
      (await cards.allTextContents()).some(text => text.includes('辣椒原生机器人')))
      throw Error('Group platform or native robot identity leaked')
    await page.locator('.uc-pagination .btn-next').click()
    await page.locator('.uc-group-row').filter({ hasText: '合成授权群 040' }).waitFor({ state: 'attached' })
    const beforePaging = writes.length
    await page.locator('.uc-pagination .btn-prev').click()
    if (writes.length !== beforePaging) throw Error('Pagination submitted a mutation')
    groupCount = 100
    await page.getByRole('button', { name: '刷新用户中心', exact: true }).click()
    await page.getByText('50 个群组', { exact: false }).waitFor()

    for (const platform of ['wsl', 'lajiaoliao']) {
      const targetGroup = groups.find(g => g.platform === platform)
      await page.getByRole('button', { name: '接入群组', exact: true }).click()
      const onboarding = dialog('接入我的群组')
      if (platform === 'lajiaoliao') await onboarding.locator('label.el-radio-button').filter({ hasText: '辣椒聊' }).click()
      const before = writeCount('/group-binding/challenges')
      for (const width of [320, 390, 768, 1440]) await assertFooter(onboarding, '开始核验', width)
      if (writeCount('/group-binding/challenges') !== before) throw Error('Viewing onboarding created challenge')
      await onboarding.getByRole('button', { name: '开始核验', exact: true }).click()
      await captcha.complete()
      if (await onboarding.locator('.onboarding-steps li').count() !== 5) throw Error('Missing five-step onboarding')
      await onboarding.getByRole('heading', { name: '本次接入机器人', exact: true }).waitFor()
      await onboarding.getByText(`public-${platform}-robot`, { exact: true }).waitFor()
      if (await onboarding.getByText('robot-native-id', { exact: true }).count()) throw Error('Internal robot ID was offered for friend search')
      await onboarding.getByRole('region', { name: '群内归属核验', exact: true })
        .getByText('群绑定 SYNTHETIC_GROUP_PROOF', { exact: true }).waitFor()
      if (challenge.identity_proven || challenge.can_confirm ||
        await onboarding.getByRole('button', { name: /^(确认绑定|提交审核|核对群邀请)$/ }).count())
        throw Error('Displaying the group challenge admitted an unproven binding or invitation')
      const beforeFriend = writes.length
      await refreshOnboarding(onboarding)
      await assertNoLegacyFriendFlow(onboarding)
      if (writes.length !== beforeFriend) throw Error('Progress GET submitted friend work')
      await selectFriendPath(onboarding, '已是好友，私聊核验')
      await onboarding.getByText(privateProof(platform), { exact: true }).waitFor()
      await onboarding.getByText(/此内容独立于好友请求/).waitFor()
      await assertFriendPending(onboarding)
      for (const width of [320, 390, 768, 1440]) await assertFooter(onboarding, '刷新进度', width)
      await refreshOnboarding(onboarding); await assertFriendPending(onboarding)
      await selectFriendPath(onboarding, '我添加机器人')
      if (writes.length !== beforeFriend) throw Error('Customer branch selection submitted friend work')
      const previewPath = `/group-binding/challenges/${challenge.id}/friend-candidate`
      const approvePath = `/group-binding/challenges/${challenge.id}/friend-approve`
      const beforePreviewCaptcha = captcha.requests.length
      await onboarding.getByRole('textbox', { name: '本人完整昵称', exact: true }).fill(`${platform === 'wsl' ? '旺商聊' : '辣椒聊'}本人`)
      await onboarding.getByRole('button', { name: '核对近期好友请求', exact: true }).click()
      await onboarding.getByText('已选中请求，尚未接受', { exact: true }).waitFor()
      await onboarding.getByText(`${platform === 'wsl' ? '旺商聊' : '辣椒聊'}本人`, { exact: true }).waitFor()
      if (writeCount(previewPath) !== 1 || writes.length !== beforeFriend + 1 ||
        captcha.requests.length !== beforePreviewCaptcha || writeCount(approvePath) || challenge.identity_proven)
        throw Error('Candidate lookup approved an application, requested captcha or proved identity')
      await assertFriendPending(onboarding)
      const operation = challenge.friend_contact.id
      await onboarding.getByRole('button', { name: '接受此好友请求', exact: true }).click()
      await dialog('确认接受好友请求').getByRole('button', { name: '确认并安全验证', exact: true }).click()
      await captcha.complete()
      await onboarding.getByText('好友审批结果待核对', { exact: true }).waitFor()
      const approvalProof = captcha.consumed.at(-1)
      if (writeCount(approvePath) !== 1 || writes.length !== beforeFriend + 2 || challenge.identity_proven ||
        challenge.friend_contact.id !== operation || approvalProof?.purpose !== 'group_binding_friend' ||
        approvalProof.platform !== platform || approvalProof.target !== challenge.id)
        throw Error('Friend approval lost its challenge captcha/operation or became identity proof')
      await selectFriendPath(onboarding, '已是好友，私聊核验')
      await onboarding.getByText(privateProof(platform), { exact: true }).waitFor()
      await assertFriendPending(onboarding)
      await refreshOnboarding(onboarding); await assertFriendPending(onboarding)
      if (writes.length !== beforeFriend + 2 || writeCount(approvePath) !== 1)
        throw Error('Reading submitted friend progress retried the approval')
      const reconcilePath = `/group-binding/challenges/${challenge.id}/friend-attempts/${operation}/reconcile`
      const beforeReconcileCaptcha = captcha.requests.length
      await onboarding.getByRole('button', { name: '核对原好友审批', exact: true }).click()
      await onboarding.locator('.el-tag').filter({ hasText: '好友申请已接受' }).waitFor()
      if (writeCount(reconcilePath) !== 1 || writeCount(approvePath) !== 1 ||
        challenge.friend_contact.id !== operation || challenge.identity_proven ||
        captcha.requests.length !== beforeReconcileCaptcha)
        throw Error('Friend reconciliation replayed approval or established identity')
      await assertFriendPending(onboarding); await assertNoLegacyFriendFlow(onboarding)
      if (await onboarding.getByRole('button', { name: '确认绑定', exact: true }).count()) throw Error('Confirmation exposed before native proof')
      await assertNoSecrets()
      // Only a separately observed authenticated private message proves the website identity.
      challenge = { ...challenge, identity_proven: true, can_accept_invitation: true, version: challenge.version + 1 }
      await onboarding.getByRole('button', { name: '刷新进度', exact: true }).click()
      await onboarding.getByLabel('目标群原生标识', { exact: true }).fill(targetGroup.native_id)
      await onboarding.getByRole('button', { name: '核对群邀请', exact: true }).click()
      const acceptPath = `/group-binding/challenges/${challenge.id}/invitation-accept`
      if (writeCount(acceptPath)) throw Error('Read invitation accepted it')
      await onboarding.getByRole('button', { name: '接受此群邀请', exact: true }).click()
      const acceptDialog = dialog('确认接受群邀请')
      for (const width of [320, 390, 768, 1440]) await assertFooter(acceptDialog, '确认接受邀请', width)
      await acceptDialog.getByRole('button', { name: '确认接受邀请', exact: true }).click()
      await onboarding.getByRole('button', { name: '核对原邀请操作', exact: true }).click()
      await onboarding.getByText('已接受', { exact: true }).waitFor()
      if (writeCount(acceptPath) !== 1) throw Error('Unknown invitation acceptance replayed')
      challenge = { ...challenge, state: 'proven', proof_role: 'owner', version: challenge.version + 1, group: { id: targetGroup.group_id, name: '测试群', native_id: targetGroup.native_id }, can_confirm: true }
      await onboarding.getByRole('button', { name: '刷新进度', exact: true }).click()
      await onboarding.getByRole('button', { name: '确认绑定', exact: true }).click()
      const confirm = dialog('确认群归属')
      const path = `/group-binding/challenges/${challenge.id}/confirm`
      if (writeCount(path)) throw Error('Opening confirmation submitted binding')
      for (const width of [320, 390, 768, 1440]) await assertFooter(confirm, '确认绑定', width)
      await confirm.getByRole('button', { name: '确认绑定', exact: true }).click()
      await onboarding.getByText('绑定成功', { exact: true }).waitFor()
      const boundRuntimePath = `/groups/${challenge.group.id}/business-runtime`
      const beforeBoundRuntime = writeCount(boundRuntimePath)
      await onboarding.getByRole('button', { name: '运行与权限', exact: true }).click()
      const boundRuntime = dialog('群业务与权限')
      await boundRuntime.getByText(`${platform === 'wsl' ? '旺商聊 · 旺商原生机器人' : '辣椒聊 · 辣椒原生机器人'}`, { exact: true }).waitFor()
      await boundRuntime.locator('.el-switch').waitFor()
      await boundRuntime.getByText('群管理未启用', { exact: true }).waitFor()
      if (writeCount(boundRuntimePath) !== beforeBoundRuntime) throw Error('New binding runtime entry enabled business automatically')
      await boundRuntime.getByRole('button', { name: '关闭', exact: true }).click()
      await assertNoSecrets()
    }

    for (const platform of ['wsl', 'lajiaoliao']) {
      await navigate('groups', platform)
      const onboarding = await beginOnboarding(platform)
      const targetGroup = groups.find(g => g.platform === platform)
      const before = writes.length
      const proof = onboarding.getByRole('region', { name: '群内归属核验', exact: true })
      await proof.getByText('群绑定 SYNTHETIC_GROUP_PROOF', { exact: true }).waitFor()
      for (const width of [320, 390, 768, 1440])
        await assertFooter(onboarding, '刷新进度', width)
      await refreshOnboarding(onboarding)
      if (writes.length !== before || challenge.identity_proven || challenge.can_confirm)
        throw Error('Viewing direct group proof submitted work or proved identity')
      // The server fixture supplies an independently verified group message.
      challenge = { ...challenge, state: 'proven', proof_role: 'owner',
        group: { id: targetGroup.group_id, name: '测试群', native_id: targetGroup.native_id },
        version: challenge.version + 1, can_confirm: true }
      await refreshOnboarding(onboarding)
      await onboarding.getByText('机器人已在群内，群归属证明已核验', { exact: true }).waitFor()
      if (challenge.identity_proven || challenge.can_accept_invitation || writes.length !== before ||
        await proof.count() || await onboarding.getByRole('button', { name: '核对群邀请', exact: true }).count())
        throw Error('Direct group proof fabricated private identity or performed an invitation')
      await onboarding.getByRole('button', { name: '确认绑定', exact: true }).click()
      const confirm = dialog('确认群归属')
      if (writes.length !== before) throw Error('Opening direct confirmation submitted binding')
      await confirm.getByRole('button', { name: '确认绑定', exact: true }).click()
      await onboarding.getByText('绑定成功', { exact: true }).waitFor()
      if (challenge.identity_proven || runtimeEnabled || managementEnabled || writes.length !== before + 1 ||
        writeCount(`/group-binding/challenges/${challenge.id}/confirm`) !== 1)
        throw Error('Direct group confirmation bypassed its single commit or enabled business')
      await assertNoSecrets()
      const completedWrites = writes.length
      await onboarding.locator('.el-dialog__footer')
        .getByRole('button', { name: '完成', exact: true }).click()
      await onboarding.waitFor({ state: 'hidden' })
      if (writes.length !== completedWrites)
        throw Error('Completing the bound group dialog submitted more work')
    }

    for (const platform of ['wsl', 'lajiaoliao']) {
      for (const scenario of ['already_friend', 'historical_approval']) {
        await navigate('groups', platform)
        const onboarding = await beginOnboarding(platform)
        const before = writes.length, captchaBefore = captcha.requests.length
        await selectFriendPath(onboarding, '已是好友，私聊核验')
        await onboarding.getByRole('heading', { name: '私聊核验本人身份', exact: true }).waitFor()
        await onboarding.getByText(privateProof(platform), { exact: true }).waitFor()
        await onboarding.getByText(/此内容独立于好友请求/).waitFor()
        await assertNoLegacyFriendFlow(onboarding)
        if (await onboarding.getByRole('textbox', { name: '本人可搜索账号', exact: true }).count() ||
          await onboarding.getByRole('button', { name: '让机器人添加我', exact: true }).count())
          throw Error(`${scenario}: private path exposed a new friend application`)
        await refreshOnboarding(onboarding); await assertFriendPending(onboarding)
        await onboarding.getByRole('button', { name: '关闭', exact: true }).click()
        await onboarding.waitFor({ state: 'hidden' })
        await page.getByRole('button', { name: '接入群组', exact: true }).click()
        await onboarding.getByRole('heading', { name: '私聊核验本人身份', exact: true }).waitFor()
        if (!await onboarding.getByRole('radio', { name: '已是好友，私聊核验', exact: true }).isChecked())
          throw Error('Closing the dialog lost the scoped private path')
        await onboarding.getByText('身份核验内容仅在首次签发时显示，请使用此前保留的完整内容，或取消本次接入后重新开始。', { exact: true }).waitFor()
        await assertNoSecrets(); await assertFriendPending(onboarding)
        await page.reload()
        await page.getByRole('button', { name: '刷新用户中心', exact: true }).waitFor()
        await page.getByRole('button', { name: '接入群组', exact: true }).click()
        await onboarding.getByRole('heading', { name: '私聊核验本人身份', exact: true }).waitFor()
        await onboarding.getByText('身份核验内容仅在首次签发时显示，请使用此前保留的完整内容，或取消本次接入后重新开始。', { exact: true }).waitFor()
        if (await onboarding.getByRole('button', { name: '复制身份核验内容', exact: true }).count())
          throw Error('Reload replaced the missing private proof with friend text')
        await selectFriendPath(onboarding, '我添加机器人')
        await onboarding.getByRole('textbox', { name: '本人完整昵称', exact: true }).waitFor()
        await selectFriendPath(onboarding, '已是好友，私聊核验')
        if (await onboarding.getByRole('button', { name: '复制身份核验内容', exact: true }).count())
          throw Error('Switching branches restored an undisclosed private proof')
        if (scenario === 'historical_approval') {
          challenge = { ...challenge, friend_flow: undefined, friend_attempt: { id: id(5000 + challengeSerial),
            status: 'succeeded', result_code: 'native_friend_approval_observed' } }
          await refreshOnboarding(onboarding)
          await onboarding.getByRole('heading', { name: '旧版好友操作记录', exact: true }).waitFor()
          await onboarding.getByText('好友申请已接受', { exact: true }).waitFor()
          if (await onboarding.getByRole('radiogroup', { name: '选择好友核验方式', exact: true }).count())
            throw Error('Historical approval allowed a new friend flow')
        } else await refreshOnboarding(onboarding)
        await assertNoLegacyFriendFlow(onboarding)
        await assertFriendPending(onboarding)
        if (writes.length !== before || captcha.requests.length !== captchaBefore)
          throw Error(`${scenario}: switching/reloading/restoring sent friend work or requested a captcha`)
        challenge = { ...challenge, identity_proven: true, can_accept_invitation: true, version: challenge.version + 1 }
        await refreshOnboarding(onboarding)
        await onboarding.getByText('好友身份已核验，可邀请机器人入群', { exact: true }).waitFor()
        await onboarding.getByRole('textbox', { name: '目标群原生标识', exact: true }).waitFor()
        if (await onboarding.getByRole('button', { name: /^(确认绑定|提交审核)$/ }).count() ||
          writes.length !== before) throw Error('Private identity proof bound a group or accepted an invitation')
        await onboarding.getByRole('button', { name: '关闭', exact: true }).click()
        await onboarding.waitFor({ state: 'hidden' })
      }
      for (const state of ['cancelled', 'expired', 'pool_identity']) {
        await navigate('groups', platform)
        const onboarding = await beginOnboarding(platform)
        await selectFriendPath(onboarding, '已是好友，私聊核验')
        const before = writes.length, challengeID = challenge.id
        if (state === 'cancelled') {
          await onboarding.getByRole('button', { name: '取消本次接入', exact: true }).click()
          const cancel = dialog('取消本次接入')
          await cancel.getByRole('button', { name: '保留', exact: true }).click()
          await cancel.waitFor({ state: 'hidden' })
          if (writes.length !== before) throw Error('Viewing or retaining cancellation submitted work')
          await onboarding.getByRole('button', { name: '取消本次接入', exact: true }).click()
          await cancel.getByRole('button', { name: '确认取消', exact: true }).click()
          await onboarding.getByText('已取消', { exact: true }).waitFor()
        } else if (state === 'expired') {
          challenge = { ...challenge, state: 'expired', expires_at: now, can_cancel: false }
          await refreshOnboarding(onboarding)
          await onboarding.locator('.onboarding-expired').getByText('已过期，请重新开始核验', { exact: true }).waitFor()
        } else {
          challenge = { ...challenge, blocked_reason: 'group_binding_pool_identity' }
          await refreshOnboarding(onboarding)
          await onboarding.getByText('请使用未接入号池的本人群主或管理员账号发送核验内容', { exact: true }).waitFor()
          await assertFriendPending(onboarding)
          await selectFriendPath(onboarding, '我添加机器人')
          const beforePoolCaptcha = captcha.requests.length
          await onboarding.getByRole('textbox', { name: '本人完整昵称', exact: true }).fill(`${platform === 'wsl' ? '旺商聊' : '辣椒聊'}本人`)
          await onboarding.getByRole('button', { name: '核对近期好友请求', exact: true }).click()
          await onboarding.locator('.el-alert--error')
            .getByText('请使用未接入号池的本人群主或管理员账号发送核验内容', { exact: true }).waitFor()
          if (challenge.friend_contact || challenge.identity_proven || captcha.requests.length !== beforePoolCaptcha ||
            await onboarding.getByRole('button', { name: '接受此好友请求', exact: true }).count())
            throw Error('Rejected pool identity selected or approved a friend application')
          await selectFriendPath(onboarding, '已是好友，私聊核验')
          await assertFriendPending(onboarding)
        }
        await refreshOnboarding(onboarding)
        if (state !== 'pool_identity' && (
          await onboarding.getByRole('radiogroup', { name: '选择好友核验方式', exact: true }).count() ||
          await onboarding.getByRole('button', { name: /^(复制身份核验内容|复制群归属核验内容|核对近期好友请求|接受此好友请求|确认绑定|核对群邀请)$/ }).count()))
          throw Error('Terminal challenge retained proof text or active controls')
        await assertNoLegacyFriendFlow(onboarding)
        if (writes.length !== before + (state === 'cancelled' || state === 'pool_identity' ? 1 : 0) ||
          writeCount(`/group-binding/challenges/${challengeID}/friend-approve`) ||
          (state === 'cancelled' && writeCount(`/group-binding/challenges/${challengeID}/cancel`) !== 1))
          throw Error('Challenge cancellation/expiry/rejection replayed native work')
        await assertNoSecrets()
        await onboarding.getByRole('button', { name: '关闭', exact: true }).click()
        await onboarding.waitFor({ state: 'hidden' })
      }
    }
    await navigate('groups')
    await page.setViewportSize({ width: 390, height: 900 })
    await cards.first().getByRole('button', { name: '运行与权限', exact: true }).click()
    const runtime = dialog('群业务与权限')
    const toggle = runtime.getByRole('switch', { name: '本群自动业务', exact: true })
    await runtime.locator('.el-switch').waitFor()
    await runtime.locator('.el-switch').click()
    const runtimePath = `/groups/${groups[0].group_id}/business-runtime`
    if (writeCount(runtimePath)) throw Error('Runtime toggle autosaved')
    runtimeConflict = true
    await runtime.getByRole('button', { name: '确认启用本群业务', exact: true }).click()
    await runtime.getByText(/状态已变化，已保留你的选择/).waitFor()
    if (await toggle.getAttribute('aria-checked') !== 'true') throw Error('Runtime conflict lost draft')
    await runtime.getByRole('button', { name: '刷新群运行状态', exact: true }).click()
    await runtime.getByRole('button', { name: '确认启用本群业务', exact: true }).click()
    await runtime.getByText('已启用，按能力准入执行', { exact: true }).waitFor()
    for (const width of [320, 390, 768, 1440]) await assertFooter(runtime, '关闭', width)
    await runtime.getByRole('button', { name: '关闭', exact: true }).click()

    await page.setViewportSize({ width: 390, height: 900 })
    delayRuntime = true
    await cards.first().getByRole('button', { name: '运行与权限', exact: true }).click()
    await runtime.getByRole('button', { name: '关闭', exact: true }).click()
    await cards.nth(1).getByRole('button', { name: '运行与权限', exact: true }).click()
    await runtime.getByText('旺商聊 · 旺商原生机器人', { exact: true }).waitFor()
    if (releaseRuntime) releaseRuntime()
    await page.waitForTimeout(80)
    if (!(await runtime.innerText()).includes('合成授权群 002')) throw Error('Late runtime response leaked across groups')
    await runtime.getByRole('button', { name: '关闭', exact: true }).click()
    await choose(page.getByRole('combobox', { name: '筛选群组平台', exact: true }), '辣椒聊')
    await cards.first().getByText('辣椒聊 · 辣椒原生机器人', { exact: false }).waitFor()
    await cards.first().getByRole('button', { name: '群管助手', exact: true }).click()
    await page.getByRole('region', { name: '助手会话工作台' }).waitFor()
    await page.locator('.assistant-heading').getByText('测试群', { exact: true }).waitFor()
    if (!reads.some(r => r.path === '/assistant/context' && r.workspace === 'lajiaoliao' && r.group === groups[1].group_id)) throw Error('User center did not target authorized platform/group assistant')
    if (writes.some(w => w.path.includes('/assistant/turns'))) throw Error('Viewing assistant invoked model')

    await navigate('members')
    await page.getByRole('button', { name: '角色权限', exact: true }).click()
    const roleDialog = dialog('角色权限')
    await choose(roleDialog.getByLabel('助手权限模板', { exact: true }), '群管本人确认')
    if (await roleDialog.getByRole('checkbox', { name: '成员禁言', exact: true }).isChecked() || await roleDialog.getByRole('checkbox', { name: /移出成员/ }).isChecked()) throw Error('Action template bundled native actions')
    if (!await roleDialog.getByRole('checkbox', { name: /移出成员/ }).isDisabled()) throw Error('Out-of-ceiling action is assignable')
    await roleDialog.locator('label.el-checkbox').filter({ hasText: '成员禁言' }).click()
    for (const width of [320, 390, 768, 1440]) await assertFooter(roleDialog, '保存角色', width)
    await roleDialog.getByRole('button', { name: '保存角色', exact: true }).click()
    await roleDialog.waitFor({ state: 'hidden' })
    const roleWrite = writes.find(w => w.path === '/member-roles/assistant_action')
    if (!roleWrite?.body.permissions.includes('group_action:mute_member') || roleWrite.body.permissions.includes('group_action:kick_member')) throw Error('Independent action assignment failed')
    await page.getByRole('button', { name: '添加成员', exact: true }).click()
    const memberDialog = dialog('添加成员')
    await memberDialog.getByLabel('用户名', { exact: true }).fill('draft-member')
    await memberDialog.getByLabel('网站密码', { exact: true }).fill(password)
    for (const width of [320, 390, 768, 1440]) await assertFooter(memberDialog, '保存成员', width)
    await memberDialog.getByRole('button', { name: '关闭', exact: true }).click()
    await assertNoSecrets()
    await page.getByRole('button', { name: '添加成员', exact: true }).click()
    if (await memberDialog.getByLabel('用户名', { exact: true }).inputValue() !== 'draft-member' || await memberDialog.getByLabel('网站密码', { exact: true }).inputValue()) throw Error('Scoped member draft/password handling failed')
    await memberDialog.getByRole('button', { name: '关闭', exact: true }).click()

    mode = 'member'
    await navigate('overview')
    await page.getByRole('heading', { name: '我的群组', exact: true }).waitFor()
    if (await page.getByRole('button', { name: '接入群组', exact: true }).count() || await page.locator('.nav-item').filter({ hasText: '成员与权限' }).count()) throw Error('Non-owner received management entries')
    await page.setViewportSize({ width: 390, height: 900 })
    await cards.first().getByRole('button', { name: '运行与权限', exact: true }).click()
    await runtime.getByText('当前仅可查看；业务启停由客户负责人管理', { exact: true }).waitFor()
    if (!await runtime.getByRole('switch').isDisabled()) throw Error('Read-only member can toggle group business')
    await runtime.getByRole('button', { name: '关闭', exact: true }).click()

    mode = 'admin'
    await page.setViewportSize({ width: 1440, height: 900 })
    await navigate('users')
    await page.getByRole('button', { name: '邀请客户', exact: true }).click()
    const invite = dialog('邀请客户负责人')
    const inviteBefore = writeCount('/customer-invitations')
    await invite.getByLabel('客户名称', { exact: true }).fill('合成客户名称')
    for (const width of [320, 390, 768, 1440]) await assertFooter(invite, '创建邀请', width)
    if (writeCount('/customer-invitations') !== inviteBefore) throw Error('Opening invitation created it')
    await invite.getByRole('button', { name: '创建邀请', exact: true }).click()
    await invite.getByRole('textbox', { name: '一次性邀请链接', exact: true }).waitFor()
    await assertNoSecrets()
    await invite.getByRole('button', { name: '关闭', exact: true }).click()

    const ownerRow = page.locator('.product-table .el-table__body tr, .el-table__body-wrapper tr').filter({ hasText: 'customer-owner' }).first()
    await ownerRow.getByRole('button', { name: '能力上限', exact: true }).click()
    const capabilities = page.locator('.tenant-capabilities-dialog')
    await capabilities.locator('label.el-checkbox').filter({ hasText: '成员禁言' }).waitFor()
    ceilingConflict = true
    await capabilities.locator('label.el-checkbox').filter({ hasText: '成员禁言' }).click()
    await capabilities.getByRole('button', { name: '保存权限上限', exact: true }).click()
    await capabilities.getByText('上限已变化，草稿已保留。请刷新后核对再保存。', { exact: true }).waitFor()
    await capabilities.getByRole('button', { name: '刷新客户权限上限', exact: true }).click()
    await capabilities.getByRole('button', { name: '保存权限上限', exact: true }).click()
    await page.waitForTimeout(80)
    await capabilities.getByRole('tab', { name: '正式业务准入', exact: true }).click()
    const gateBefore = writes.filter(w => w.path.startsWith('/capabilities/gates/')).length
    await capabilities.getByRole('button', { name: '读取准入', exact: true }).click()
    await choose(capabilities.getByRole('combobox', { name: '业务准入范围', exact: true }), '指定群灰度')
    await choose(capabilities.getByRole('combobox', { name: '灰度群组', exact: true }), '测试群 · 旺商原生机器人')
    await page.keyboard.press('Escape')
    await capabilities.getByRole('textbox', { name: '验收证据引用', exact: true }).fill('docs/test-report.md\nacceptance:WSL-20261105-001')
    for (const width of [320, 390, 768, 1440]) await assertFooter(capabilities, '保存准入', width)
    if (writes.filter(w => w.path.startsWith('/capabilities/gates/')).length !== gateBefore) throw Error('Gate draft autosaved')
    await capabilities.getByRole('button', { name: '保存准入', exact: true }).click()
    await dialog('确认正式业务准入').getByRole('button', { name: '确认保存准入', exact: true }).click()
    await dialog('确认正式业务准入').waitFor({ state: 'hidden' })
    const gateWrite = writes.filter(w => w.path.startsWith('/capabilities/gates/')).at(-1)
    if (gateWrite?.body.state !== 'pilot' || gateWrite.body.pilot_group_ids[0] !== groups[0].group_id || gateWrite.body.evidence_refs.length !== 2) throw Error('Gate pilot/evidence not submitted')
    await capabilities.getByRole('button', { name: '关闭', exact: true }).click()
    await ownerRow.getByRole('button', { name: '成员', exact: true }).click()
    await page.locator('.tenant-members').getByText('customer-reader', { exact: true }).waitFor()
    if (!reads.some(r => r.path === '/member-roles' && r.tenant === owner.tenant_id)) throw Error('Admin member scope missing')
    await choose(page.getByRole('combobox', { name: '选择成员管理客户', exact: true }), '第二客户')
    await page.locator('.tenant-members').getByText('second-owner', { exact: true }).waitFor()
    if (await page.locator('.tenant-members').getByText('customer-reader', { exact: true }).count()) throw Error('Old customer members leaked after tenant switch')
    await screenshot('admin-members', 390)
    for (const workspace of ['wsl', 'lajiaoliao']) {
      await page.setViewportSize({ width: 1440, height: 900 })
      await navigate('claims', workspace)
      const pending = claims.filter(row => row.platform === workspace)
      const claimRow = row => page.locator('.el-table__body-wrapper tr').filter({ hasText: row.group_name }).first()
      const normal = pending.find(row => row.native_identity === 'synthetic-message')
      const transfer = pending.find(row => row.native_identity === 'synthetic-transfer')
      const review = page.locator('.customer-binding-review')
      delayReviewID = normal.onboarding_challenge_id
      await claimRow(normal).getByRole('button', { name: '审核', exact: true }).click()
      await review.getByRole('button', { name: '关闭', exact: true }).click()
      await claimRow(transfer).getByRole('button', { name: '审核', exact: true }).click()
      await review.getByText('转出租户', { exact: true }).waitFor()
      if (releaseReview) { releaseReview(); releaseReview = undefined }
      await page.waitForTimeout(80)
      if (!await review.getByText('转出租户', { exact: true }).count()) throw Error('Late proof read crossed review scope')
      if (!(await review.innerText()).includes('第二客户') || !(await review.innerText()).includes('合成客户') || !(await review.innerText()).includes(groups.find(g => g.platform === workspace).robot_nickname)) throw Error('Transfer review omitted source/target/group/robot')
      const transferButton = review.getByRole('button', { name: '确认转移绑定', exact: true })
      if (!await transferButton.isDisabled()) throw Error('Transfer approval allowed without explicit consent')
      const transferPath = `/group-binding/challenges/${transfer.onboarding_challenge_id}/transfer-review`
      const beforeTransfer = writeCount(transferPath)
      for (const width of [320, 390, 768, 1440]) await assertFooter(review, '确认转移绑定', width)
      if (writeCount(transferPath) !== beforeTransfer) throw Error('Viewing transfer review submitted it')
      await review.locator('label.el-checkbox').click()
      reviewConflicts.add(transfer.onboarding_challenge_id)
      await transferButton.click()
      await review.getByText('审核记录已变化，请刷新后核对；转移确认需要重新勾选。', { exact: true }).waitFor()
      if (!await transferButton.isDisabled()) throw Error('Transfer CAS conflict permitted stale resubmission')
      await review.getByRole('button', { name: '刷新审核记录', exact: true }).click()
      await review.getByText('转出租户', { exact: true }).waitFor()
      if (await review.getByRole('checkbox').isChecked() || !await transferButton.isDisabled()) throw Error('Transfer consent survived version refresh')
      await review.locator('label.el-checkbox').click()
      await transferButton.click()
      await review.getByText('审核已通过，群业务运行保持关闭', { exact: true }).waitFor()
      if (writeCount(transferPath) !== beforeTransfer + 2) throw Error('Transfer review repeated without explicit confirmation')
      const reviewedGroup = groups.find(g => g.platform === workspace)
      const reviewedRuntimePath = `/groups/${reviewedGroup.group_id}/business-runtime`
      const beforeReviewedRuntime = writeCount(reviewedRuntimePath)
      await review.getByRole('button', { name: '运行与权限', exact: true }).click()
      const reviewedRuntime = dialog('群业务与权限')
      await reviewedRuntime.getByText(`${workspace === 'wsl' ? '旺商聊' : '辣椒聊'} · ${reviewedGroup.robot_nickname}`, { exact: true }).waitFor()
      await reviewedRuntime.getByText('群管理未启用', { exact: true }).waitFor()
      if (writeCount(reviewedRuntimePath) !== beforeReviewedRuntime) throw Error('Transfer approval implicitly enabled group business')
      await reviewedRuntime.locator('.el-switch').click()
      if (writeCount(reviewedRuntimePath) !== beforeReviewedRuntime) throw Error('Reviewed group toggle auto-submitted')
      await reviewedRuntime.getByRole('button', { name: '确认启用本群业务', exact: true }).click()
      await reviewedRuntime.getByText('已启用，按能力准入执行', { exact: true }).waitFor()
      await reviewedRuntime.getByText('群管理已启用', { exact: true }).waitFor()
      if (writeCount(reviewedRuntimePath) !== beforeReviewedRuntime + 1) throw Error('Reviewed group runtime enable not explicit/bounded')
      await reviewedRuntime.getByRole('button', { name: '关闭', exact: true }).click()

      await claimRow(normal).getByRole('button', { name: '审核', exact: true }).click()
      await review.getByText('待平台审核', { exact: true }).waitFor()
      const normalPath = `/group-binding/challenges/${normal.onboarding_challenge_id}/review`
      reviewConflicts.add(normal.onboarding_challenge_id)
      await review.getByRole('button', { name: '批准绑定', exact: true }).click()
      await review.getByText('审核记录已变化，请刷新后核对；转移确认需要重新勾选。', { exact: true }).waitFor()
      await review.getByRole('button', { name: '刷新审核记录', exact: true }).click()
      await review.getByText('待平台审核', { exact: true }).waitFor()
      for (const width of [320, 390, 768, 1440]) await assertFooter(review, '批准绑定', width)
      if (workspace === 'lajiaoliao') {
        await review.locator('label.el-radio').filter({ hasText: '拒绝' }).click()
        await review.getByRole('button', { name: '拒绝申请', exact: true }).click()
        await review.getByText('申请已拒绝', { exact: true }).waitFor()
      } else {
        await review.getByRole('button', { name: '批准绑定', exact: true }).click()
        await review.getByText('审核已通过，群业务运行保持关闭', { exact: true }).waitFor()
      }
      if (writeCount(normalPath) !== 2 || writes.some(w => w.path === `/claims/${normal.id}/review`)) throw Error('Authenticated proof used legacy review or replayed')
      if (workspace === 'wsl') {
        const beforeNormalRuntime = writeCount(reviewedRuntimePath)
        await review.getByRole('button', { name: '运行与权限', exact: true }).click()
        await reviewedRuntime.locator('.el-switch').waitFor()
        if (writeCount(reviewedRuntimePath) !== beforeNormalRuntime) throw Error('Proof approval auto-submitted runtime')
        await reviewedRuntime.getByRole('button', { name: '关闭', exact: true }).click()
      } else {
        if (await review.getByRole('button', { name: '运行与权限', exact: true }).count()) throw Error('Rejected binding exposes runtime entry')
        await review.getByRole('button', { name: '关闭', exact: true }).click()
      }

      const manual = pending.find(row => row.proof_method === 'manual')
      await claimRow(manual).getByRole('button', { name: '审核', exact: true }).click()
      const legacy = dialog(`${manual.group_name} · 授权审核`)
      await legacy.locator('label.el-checkbox').click()
      await legacy.getByLabel('审核依据', { exact: true }).fill('合成手工身份审核')
      for (const width of [320, 390, 768, 1440]) await assertFooter(legacy, '保存', width)
      await legacy.getByRole('button', { name: '保存', exact: true }).click()
      await legacy.waitFor({ state: 'hidden' })
      if (writeCount(`/claims/${manual.id}/review`) !== 1) throw Error('Manual claim legacy review changed')
    }
    await assertNoSecrets()
    if (failures.length) throw Error(failures.join('; '))
    return { passed: true, synthetic_only: true, native_actions: 0, model_calls: 0,
      viewports: [320, 390, 768, 1440], groups: [100, 300], public_activation: true, existing_login_required: true,
      customer_friend_paths: ['incoming_contact', 'already_friend', 'historical_approval'], friend_platforms: ['wsl', 'lajiaoliao'],
      incoming_prepare_before_approval: true, incoming_challenge_captcha: true, incoming_reconcile_not_identity: true,
      private_evidence_required: true, historical_friend_approval_not_proof: true, branch_reload_reads_only: true,
      private_proof_not_persisted_or_replaced: true, cancelled_and_expired_isolated: true, pool_identity_rejected: true,
      owner_only_members: true, independent_action_grants: true, runtime_cas: true, late_response_isolated: true,
      admin_explicit_tenant: true, gate_evidence_refs: true, proof_review: true, transfer_review: true, review_cas: true,
      manual_claim_review: true, controlled_test_transfer_review: true,
      bound_runtime_entry: true, reviewed_runtime_enable: true, screenshots }
  } finally {
    if (releaseRuntime) releaseRuntime()
    if (releaseReview) releaseReview()
    await page.unroute('**/api/v1/**', handler)
  }
}
