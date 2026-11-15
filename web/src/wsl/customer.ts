import { errorText, type Row } from './api'

export type CustomerPlatform = 'wsl' | 'lajiaoliao'
export const customerPlatforms: { value: CustomerPlatform; label: string; image: string }[] = [
  { value: 'wsl', label: '旺商聊', image: '/wsl-logo.png' },
  { value: 'lajiaoliao', label: '辣椒聊', image: '/lajiaoliao-logo.png' }
]
export const platformLabel = (value?: string) =>
  customerPlatforms.find((item) => item.value === value)?.label || value || '—'
export const customerDate = (value?: string) =>
  value
    ? new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
    : '—'
export const groupKey = (group: Row) => String(group.group_id || group.id || '')
export const robotLabel = (group?: Row) =>
  String(
    group?.robot?.native_name ||
      group?.robot?.native_nickname ||
      group?.robot?.name ||
      group?.robot_nickname ||
      group?.native_nickname ||
      group?.account_nickname ||
      group?.account_name ||
      '尚未分配'
  )
export const robotAddAccount = (challenge?: Row) =>
  challenge?.robot?.add_available === true && typeof challenge.robot.add_account === 'string'
    ? challenge.robot.add_account.trim()
    : ''
export interface ControlledOnboardingIdentity {
  id: string
  name: string
  native_id: string
}
export interface ControlledOnboardingScope {
  id: string
  platform: CustomerPlatform
  actor_id: string
  tenant_id: string
  account_id: string
  claimant_account_id: string
  group_id: string
  robot: ControlledOnboardingIdentity
  claimant: ControlledOnboardingIdentity
  group: ControlledOnboardingIdentity
  state: 'preparing' | 'active' | 'expired' | 'closed' | 'unknown'
  blocked_reason: string
  version: number
  expires_at: string
  native_attempts_used: number
  model_calls_used: number
  reserved_usdt: string
  human_verification_required?: boolean
}
export const controlledOnboardingSelectionKey = (
  actor: string,
  tenant: string | undefined,
  platform: string
) => `wsl:controlled-onboarding:${actor}:${tenant || 'operator'}:${platform}`
export function controlledOnboardingScope(value: unknown): ControlledOnboardingScope {
  const item = value as ControlledOnboardingScope | undefined
  const identity = (entry?: ControlledOnboardingIdentity) =>
    entry &&
    [entry.id, entry.name, entry.native_id].every(
      (field) => typeof field === 'string' && Boolean(field.trim())
    )
  if (
    !item ||
    !['wsl', 'lajiaoliao'].includes(item.platform) ||
    ![
      item.id,
      item.actor_id,
      item.tenant_id,
      item.account_id,
      item.claimant_account_id,
      item.group_id
    ].every((field) => typeof field === 'string' && Boolean(field)) ||
    !identity(item.robot) ||
    !identity(item.claimant) ||
    !identity(item.group) ||
    item.robot.id !== item.account_id ||
    item.claimant.id !== item.claimant_account_id ||
    item.group.id !== item.group_id ||
    item.account_id === item.claimant_account_id ||
    item.robot.native_id === item.claimant.native_id ||
    !['preparing', 'active', 'expired', 'closed', 'unknown'].includes(item.state) ||
    !Number.isSafeInteger(item.version) ||
    item.version < 1 ||
    typeof item.expires_at !== 'string' ||
    !Number.isFinite(Date.parse(item.expires_at)) ||
    ![item.native_attempts_used, item.model_calls_used].every(
      (count) => Number.isSafeInteger(count) && count >= 0
    ) ||
    typeof item.reserved_usdt !== 'string' ||
    (item.human_verification_required !== undefined &&
      typeof item.human_verification_required !== 'boolean') ||
    !/^(?:0|[1-9]\d*)\.\d{6}$/.test(item.reserved_usdt)
  )
    throw new Error('受控接入范围暂无法核对，请刷新原记录')
  return {
    ...item,
    blocked_reason: typeof item.blocked_reason === 'string' ? item.blocked_reason : ''
  }
}
export const controlledOnboardingExpired = (item: ControlledOnboardingScope, now: number) =>
  item.state === 'expired' || Date.parse(item.expires_at) <= now
export const controlledOnboardingActive = (item: ControlledOnboardingScope, now: number) =>
  item.state === 'active' && !item.blocked_reason && !controlledOnboardingExpired(item, now)
export const controlledOnboardingSameScope = (
  left: ControlledOnboardingScope,
  right: ControlledOnboardingScope
) =>
  (
    [
      'id',
      'platform',
      'actor_id',
      'tenant_id',
      'account_id',
      'claimant_account_id',
      'group_id',
      'expires_at'
    ] as const
  ).every((key) => left[key] === right[key]) &&
  left.robot.native_id === right.robot.native_id &&
  left.claimant.native_id === right.claimant.native_id &&
  left.group.native_id === right.group.native_id
export const controlledOnboardingState = (item: ControlledOnboardingScope, now: number) =>
  item.state === 'unknown'
    ? '结果待核对'
    : item.state === 'closed'
      ? '已关闭'
      : controlledOnboardingExpired(item, now)
        ? '已过期'
        : {
            preparing: '准备中',
            active: '有效',
            expired: '已过期',
            closed: '已关闭',
            unknown: '结果待核对'
          }[item.state]
export interface OnboardingFriendContact {
  id: string
  action: 'approve' | 'reject_expired'
  status: 'prepared' | 'submitting' | 'succeeded' | 'failed' | 'unknown'
  result_code: string
  version: number
  name: string
  requested_at: string
  can_approve: boolean
  can_reject: boolean
  can_reconcile: boolean
  blocked_reason: string
}
export function onboardingFriendContact(value: unknown): OnboardingFriendContact | undefined {
  if (value == null) return undefined
  const contact = value as OnboardingFriendContact
  if (
    typeof contact.id !== 'string' ||
    !contact.id ||
    (contact.action !== undefined && !['approve', 'reject_expired'].includes(contact.action)) ||
    !['prepared', 'submitting', 'succeeded', 'failed', 'unknown'].includes(contact.status) ||
    !Number.isSafeInteger(contact.version) ||
    contact.version < 1 ||
    typeof contact.name !== 'string' ||
    !contact.name.trim() ||
    typeof contact.requested_at !== 'string' ||
    !Number.isFinite(Date.parse(contact.requested_at))
  )
    throw new Error('好友请求记录暂无法核对，请刷新本次接入进度')
  return {
    id: contact.id,
    action: contact.action === undefined ? 'approve' : contact.action,
    status: contact.status,
    result_code: typeof contact.result_code === 'string' ? contact.result_code : '',
    version: contact.version,
    name: contact.name,
    requested_at: contact.requested_at,
    can_approve: contact.can_approve === true,
    can_reject: contact.can_reject === true,
    can_reconcile: contact.can_reconcile === true,
    blocked_reason: typeof contact.blocked_reason === 'string' ? contact.blocked_reason : ''
  }
}
export const billingSourceLabel = (source?: string) =>
  ({
    platform_workspace: '工作空间后台用量 · 平台承担，不扣客户积分',
    platform_demo: '超管本人演示 · 平台承担，不扣客户积分',
    tenant_wallet: '客户套餐与共享积分钱包',
    shared_gateway: '共享模型网关'
  })[source || ''] || '费用归属待核对'
export const operatorPlan = (value?: Row | null) =>
  value?.effective_plan
    ? value.effective_plan.id === 'operator_unlimited' && value.effective_plan.system_only === true
    : value?.entitlement?.mode === 'operator_demo'
export const actionLabels: Record<string, string> = {
  group_info: '修改群名称',
  publish_notice: '发布群公告',
  member_nickname: '修改成员名片',
  mute_member: '成员禁言',
  unmute_member: '成员解禁',
  mute_group: '全群禁言',
  unmute_group: '解除全群禁言',
  kick_member: '移出成员',
  send_text: '发送群文本',
  recall_message: '撤回消息'
}
export const sourceLabels: Record<string, string> = {
  controlled: '受控测试',
  controlled_test: '受控测试',
  control: '页面操作',
  manual: '页面操作',
  assistant: '群管助手',
  automatic_rules: '自动群管',
  automation: '自动群管',
  automatic: '自动群管',
  business_rules: '自动群管',
  onboarding: '群接入向导',
  heatbot: '热群机器人',
  heat_chat: '热群机器人'
}
export const runtimeLabels: Record<string, string> = {
  running: '运行中',
  enabled: '已启用',
  disabled: '已停用',
  stopped: '已停用',
  paused: '已暂停',
  blocked: '等待运行条件',
  closed: '未开放',
  pending: '待处理'
}
export function customerReason(code?: string) {
  if (!code) return ''
  const known: Record<string, string> = {
    subscription_inactive: '套餐未生效或已到期',
    package_required: '当前套餐未包含此能力',
    group_not_enabled: '本群尚未启用业务',
    business_runtime_disabled: '本群业务已停用',
    group_business_disabled: '本群业务已停用',
    group_runtime_disabled: '本群业务已停用',
    group_binding_inactive: '群授权已撤销',
    native_role_unavailable: '原生群角色尚未核验',
    platform_not_admitted: '本平台尚未开放该能力',
    production_not_admitted: '该动作尚未通过正式准入',
    capability_not_enabled: '平台尚未向本客户开通该能力',
    capability_ceiling_denied: '此能力未包含在客户权限上限中',
    receiver_unavailable: '机器人接收连接尚未就绪',
    permission_denied: '当前成员没有此权限',
    group_binding_friend_contact_not_found:
      '机器人本次返回的申请列表中，没有本次接入时间范围内、昵称完全一致的待处理请求。请在官方 App 核对申请发给了本次机器人、本人完整昵称及发送时间。确认已是好友后，才选择「已是好友，私聊核验」。',
    group_binding_friend_contact_outside_window:
      '找到了昵称一致的待处理申请，但平台记录的申请时间不在本次接入范围内，尚未同意。刷新页面不会更新原申请时间，请先在官方 App 核对旧申请；确已成为好友后再选择「已是好友，私聊核验」。',
    group_binding_friend_contact_processed:
      '机器人本次返回的同昵称申请记录已处理，没有可同意的待处理申请。历史“已同意”不代表当前仍是好友。请先在官方 App 核对双方好友状态；不是好友时，在本次有效期内重新发送申请；确认已是好友后，再选择「已是好友，私聊核验」。',
    group_binding_friend_contact_ambiguous:
      '匹配到多个同昵称的近期请求，无法唯一选择，未接受任何申请。请核对完整昵称，不要猜选；已是好友可选择私聊核验。',
    group_binding_friend_contact_time_unavailable:
      '好友请求的原生申请时间缺失或无法确认可信，无法安全选择，未接受任何申请。已是好友可选择「已是好友，私聊核验」。',
    group_binding_friend_contact_incomplete:
      '好友申请列表过多，无法安全定位唯一的一条请求，未接受任何申请。不会翻页或批量处理；已是好友请选择「已是好友，私聊核验」，否则请联系超级管理员。',
    group_binding_friend_request_not_found:
      '未找到本次接入时间范围内、昵称完全一致的待处理请求。可能已是好友，可选择「已是好友，私聊核验」；也可在官方 App 核对后手动重查。',
    group_binding_friend_candidate_not_found:
      '未找到本次接入时间范围内、昵称完全一致的待处理请求。可能已是好友，可选择「已是好友，私聊核验」。',
    group_binding_friend_request_ambiguous:
      '匹配到多个同昵称的近期请求，无法唯一选择，未接受任何申请。请核对昵称，或已是好友时选择私聊核验。',
    group_binding_friend_candidate_ambiguous:
      '匹配到多个同昵称的近期请求，无法唯一选择，未接受任何申请。请核对昵称，或已是好友时选择私聊核验。',
    group_binding_friend_nickname_invalid: '请填写本人在当前平台的完整昵称',
    group_binding_contact_invalid: '请核对本人完整昵称及本次接入版本后手动操作',
    group_binding_friend_admission_closed: '此机器人的来向好友审批尚未开放，已有好友仍可私聊核验',
    group_binding_friend_not_approvable: '此请求当前不能审批，请刷新本次进度核对',
    group_binding_friend_operation_unresolved: '原好友审批结果待核对，请核对原操作，不要再次提交',
    group_binding_friend_rate_limited: '好友审批操作过于频繁，请稍后手动核对，不会自动提交',
    group_binding_friend_attempt_expired: '原好友审批已超过提交期限，请刷新本次进度核对',
    group_binding_friend_reapply_required: '请重新开始核验，并在有效时间内重新发送好友申请。',
    group_binding_friend_native_unavailable: '机器人暂不能核对好友请求，请稍后手动刷新原记录',
    group_binding_friend_submission_missing: '好友审批提交结果待核对，请查询原操作，不要再次提交',
    group_binding_friend_accept_acknowledged: '好友申请已接受，请继续独立私聊核验本人身份',
    native_friend_approval_acknowledged: '好友申请已接受，请继续独立私聊核验本人身份',
    native_friend_approval_observed: '已核对到原好友审批结果，请继续独立私聊核验本人身份',
    native_friend_approval_rejected: '已核对到原好友申请被拒绝，本次操作已结束；不会再次提交',
    native_friend_expired_rejection_acknowledged:
      '拒绝已确认。请重新开始核验，并在有效时间内重新发送好友申请。',
    native_friend_expired_rejection_observed:
      '拒绝已确认。请重新开始核验，并在有效时间内重新发送好友申请。',
    group_binding_contact_approval_not_required: '已收到本人的独立核验消息，无需继续接受原好友申请',
    group_binding_friend_verified: '好友联系请求已处理，仍需独立私聊核验本人身份'
  }
  return known[code] || errorText(code)
}
