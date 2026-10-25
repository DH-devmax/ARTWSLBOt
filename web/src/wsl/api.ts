export interface User {
  id: string
  tenant_id?: string
  username: string
  role: 'superadmin' | 'user'
}
export type Row = Record<string, any>

const errors: Record<string, string> = {
  invalid_credentials: '账号或密码不正确',
  login_rate_limited: '登录尝试过多，请稍后重试',
  login_busy: '登录服务繁忙',
  version_conflict: '配置已变化，请刷新后重新编辑',
  username_exists: '用户名已存在',
  account_limit_reached: '已达到 10 个账号上限',
  native_compatibility_not_verified: '原生协议兼容尚未验收，不能启用或发送操作',
  ai_isolation_not_verified: 'AI 群级隔离尚未验收，不能启用',
  fresh_native_role_required: '缺少最新原生管理员角色确认',
  fresh_test_account_role_required: '测试账号尚未完成连接及角色核验',
  code_invalid_or_expired: '授权码无效、已使用或已过期',
  active_subscription_required: '租户尚未开通有效套餐',
  group_quota_exceeded: '群组名额不足',
  group_already_owned: '群组已被认领',
  csrf_rejected: '会话校验失败，请重新登录',
  module_not_in_plan: '套餐未包含该功能',
  invalid_request: '请求格式不正确',
  operation_already_started: '操作已开始，不能取消',
  identity_review_required: '请完成原生身份核验并填写审核依据',
  isolated_test_group_required: '必须选择经过标记的隔离测试群',
  insufficient_participants: '报名人数不足',
  activity_not_ready: '活动尚未到开奖时间、已开奖或版本已变化',
  ledger_reference_exists: '此业务流水号已存在',
  subscription_below_current_usage: '套餐名额小于当前使用量，请先调整群授权',
  database_unavailable: '数据库暂时不可用，请稍后重试',
  authentication_required: '会话已失效，请重新登录',
  superadmin_required: '此功能仅限超管使用'
}

Object.assign(errors, {
  maibot_section_not_found: '热群配置模块不存在',
  invalid_maibot_settings: '热群配置不正确，请核对时段、范围、HTTPS 模型地址及保留期限',
  maibot_settings_unreadable: '热群配置无法读取，请核对主密钥',
  invalid_workspace: '工作台不存在',
  workspace_route_forbidden: '此功能不属于当前工作台',
  permission_denied: '当前用户没有此操作权限，或套餐未包含该功能',
  invalid_permissions: '权限配置不正确',
  ai_group_quota_exceeded: 'AI 群组名额不足',
  ai_binding_requires_review: '旧版聊天账号关联需由超管核对，不能自动复用群管账号',
  account_not_found: '此账号不属于当前工作台或已失效',
  account_removal_name_mismatch: '机器人名称不匹配，未执行删除',
  account_removal_signup_pending: '有已提交但结果未确认的注册，请先核对官方客户端并复核注册记录',
  account_removal_operations_pending: '有关联操作执行中或结果未知，请先复核，未执行删除',
  account_removal_busy: '关联数据正在变更，请稍后重新核对删除范围',
  account_removal_dependencies_changed: '有关联记录仍需保留，本次删除已全部回滚；请先复核依赖',
  group_not_found: '群组不存在或未授权',
  group_robot_removed: '群管机器人已删除，此群归属和历史记录保留，服务已停用',
  native_deployment_missing: '尚未导入原生协议配置',
  native_deployment_invalid: '协议配置格式或密钥不正确',
  native_deployment_unreadable: '协议配置无法解密，请核对服务端主密钥',
  native_crypto_unavailable: '当前服务未使用 libsodium 构建',
  native_auth_account_busy: '此账号的注册或登录正在由另一位超管操作',
  native_auth_busy: '当前验证步骤正在执行，请等待结果后再切换',
  native_auth_review_required: '上次请求结果未知，请先核对官方客户端状态，不能直接切换注册与登录',
  native_auth_rate_limited: '登录尝试次数过多，请稍后重试',
  native_auth_limit: '同时登录的账号数量达到上限',
  native_auth_not_found: '登录事务不存在或已取消',
  native_auth_expired: '登录事务已过期',
  native_auth_cooldown: '尚在冷却时间，请稍后重试',
  native_auth_state_conflict: '当前状态不接受此操作',
  native_invalid_input: '原生登录信息格式不正确',
  native_captcha_required: '请先完成人机验证',
  native_invalid_credentials: '旺商聊账号或密码不正确',
  native_invalid_sms: '短信验证码不正确',
  native_sms_expired: '短信验证码已过期',
  native_rate_limited: '旺商聊接口限流，请等待倒计时结束',
  native_login_rejected_unspecified: '旺商聊拒绝了登录请求，未提供明确原因',
  native_phone_not_found: '旺商聊未找到对应手机号',
  native_security_invalid: '安全码不正确',
  native_security_locked: '安全码验证暂时锁定，请在官方客户端核对',
  native_transport_unknown: '请求结果未知，请先核对官方客户端，不会自动重试',
  native_login_binding: '登录响应未通过密钥交换绑定校验',
  native_account_changed: '账号配置已变化，此次登录未保存',
  native_deployment_changed: '协议配置已变化，请重新登录',
  native_site_session_expired: '网站会话已失效，此次原生登录未保存',
  native_identity_already_bound: '此旺商聊身份已经绑定到另一个账号面板',
  native_disconnect_before_login: '请先停止此账号接入，再进行原生登录',
  native_reauth_required: '原生会话已失效，请取消后重新登录',
  native_security_required: '旺商聊要求验证六位安全码',
  native_security_setup_required: '请先在官方客户端设置安全码',
  native_security_phone_required: '请先在官方客户端完成手机号验证',
  native_account_clearing: '账号处于注销处理中，请在官方客户端核对',
  native_response_invalid: '平台响应无法核验，结果未知，不会自动重试',
  native_http_rejected: '旺商聊拒绝了此请求',
  native_business_rejected: '旺商聊未接受此操作，请核对平台返回码',
  native_auth_failed: '登录未完成，请核对状态后重新操作',
  native_login_identity: '登录响应缺少可核验的原生身份',
  native_nim_token_invalid: 'IM 凭据未通过校验，此次登录未保存',
  native_sms_challenge_invalid: '平台短信验证响应不完整',
  native_request_id_unavailable: '请求编号无法可靠保存，未向旺商聊提交请求',
  native_request_id_exhausted: '原生请求编号达到安全边界，已停止提交',
  native_signup_consent_required: '请先确认旺商聊协议及手机号使用授权',
  native_signup_review_required: '上次开户已提交但结果未确认，请先在官方客户端核对；禁止重复开户',
  native_signup_already_registered: '此面板已注册旺商聊账号，请使用原生登录',
  native_signup_empty_panel_required: '注册需要未绑定原生身份且未连接的空账号面板',
  native_signup_identity_mismatch: '登录身份与此面板已注册的旺商聊账号不一致',
  native_signup_record_pending: '开户结果尚未可靠记录，已阻止重复提交，请核对官方客户端',
  native_signup_unavailable: '开户服务不可用'
})

export function errorText(code: string) {
  return errors[code] || code || ''
}

export class ApiError extends Error {
  constructor(
    public status: number,
    public code: string
  ) {
    super(errors[code] || code)
  }
}

export async function api<T>(
  path: string,
  method = 'GET',
  body?: unknown,
  signal?: AbortSignal,
  workspace: 'wsl' | 'maibot' = 'wsl'
): Promise<T> {
  const csrf =
    document.cookie
      .split('; ')
      .find((v) => v.startsWith('wsl_csrf='))
      ?.split('=')[1] || ''
  const response = await fetch(`/api/v1${path}`, {
    method,
    credentials: 'same-origin',
    signal,
    headers: {
      'Content-Type': 'application/json',
      'X-Workspace': workspace,
      'X-CSRF-Token': csrf,
      ...(method === 'POST' && path === '/operations'
        ? { 'Idempotency-Key': crypto.randomUUID() }
        : {})
    },
    body: body === undefined ? undefined : JSON.stringify(body)
  })
  const result = await response.json()
  if (!response.ok || result.code !== 0) throw new ApiError(response.status, result.msg)
  return result.data as T
}
