export interface User {
  id: string
  tenant_id?: string
  username: string
  role: 'superadmin' | 'user'
  tenant_role?: string
}
export interface NativeModerationSnapshot {
  platform?: 'wsl' | 'lajiaoliao'
  self_id: string
  self_role?: string
  nickname_locked: boolean
  native_actions_enabled: boolean
  digest: string
  account_epoch: number
  account_version: number
  deployment_version: number
  sampled_at: string
  group_id: string
  in_group: boolean
  group_name: string
  members: { id: string; transport: string; name: string; card: string; role: string }[]
  policy: { group_id: string; muted: boolean }
  muted_ids: string[]
  mutes_known: boolean
  group_bans?: {
    groupID: string
    userID: string
    nickname: string
    blockedBy: string
    blockedAt: number
  }[]
  bans_known?: boolean
}
export type Row = Record<string, any>
export interface ChiliMessageView {
  kind: string
  content_type: number
  text?: string
  title?: string
  summaries?: string[]
  mentions?: { id: string; name?: string }[]
  quote?: ChiliMessageView
  items?: ChiliMessageView[]
  contact?: { id: string; name?: string }
  media?: { image?: string; video?: string; poster?: string; avatar?: string }
  duration?: number
  size?: number
  truncated?: boolean
  notice?: {
    event: string
    group_id: string
    actor?: { id: string; name?: string }
    targets?: { id: string; name?: string }[]
    seconds?: number
  }
  friend_notice?: {
    event: 'friend_request' | 'friend_approved' | 'friend_added'
    sender: { id: string; name?: string }
    recipient: { id: string; name?: string }
    friend?: { id: string; name?: string }
  }
  system_notice?: {
    event: 'read_receipt'
  }
}
export type { Workspace } from './workspaces'
import type { Workspace } from './workspaces'

const errors: Record<string, string> = {
  captcha_required: '请先完成安全验证',
  captcha_invalid: '安全验证已失效或未通过，请重新验证后手动提交',
  captcha_rate_limited: '安全验证请求过于频繁，请稍后重试',
  captcha_busy: '安全验证服务繁忙，请稍后重试',
  captcha_unavailable: '安全验证暂时不可用，请刷新重试',
  customer_invitation_invalid: '邀请链接无效、已使用或已过期',
  customer_invitation_invalid_or_expired: '邀请链接无效或已过期，请联系管理员',
  customer_invitation_website_login_required: '请先登录本人网站账号，再接受此邀请',
  customer_invitation_not_pending: '此邀请已使用、过期或撤销',
  customer_invitation_owner_required: '已有账号必须为有效客户负责人',
  customer_invitation_activation_invalid: '请填写正确的网站用户名',
  customer_invitation_password_invalid: '密码不符合要求，请重新设置',
  customer_invitation_username_unavailable: '此用户名已被使用，请更换网站用户名',
  invalid_business_gate: '请核对准入状态、灰度群及有效验收证据引用',
  business_pilot_platform_mismatch: '灰度群不属于当前平台',
  invalid_capabilities: '权限上限包含不支持的项目',
  group_runtime_disabled: '本群业务运行已关闭',
  customer_invitation_expired: '邀请链接已过期，请联系管理员重新邀请',
  customer_invitation_used: '此邀请已接受，请直接登录',
  customer_invitation_login_required: '已有网站账号，请先登录本人账号再接受邀请',
  customer_invitation_actor_mismatch: '当前登录账号与受邀账号不一致',
  customer_invitation_conflict: '客户或账号信息已变化，请重新核对',
  invalid_customer_invitation: '请填写客户名称及正确的网站账号信息',
  group_binding_customer_required: '请先选择群组所属客户',
  group_binding_customer_not_found: '所选客户不存在或没有有效负责人，请重新选择',
  group_binding_target_forbidden: '客户账号只能接入自己的群组',
  group_binding_context_invalid: '群组接入范围已变化，请重新打开接入窗口',
  controlled_onboarding_scope_invalid: '请核对机器人、申请人、隔离群和1至360分钟（最长6小时）的有效期',
  controlled_onboarding_selection_unavailable:
    '请确认两端账号均已认证并启用受控测试，且选择了同平台隔离群',
  controlled_onboarding_scope_not_found: '未找到本人当前平台的受控接入范围，请刷新列表',
  controlled_onboarding_scope_inactive: '此受控接入范围已不可用，请核对原记录；过期后需创建新范围',
  controlled_onboarding_scope_expired: '受控接入范围已过期，继续测试需创建新范围',
  controlled_onboarding_scope_closed: '此受控接入范围已关闭',
  controlled_onboarding_scope_changed: '受控接入账号、权限或群组条件已变化，请查询原范围',
  controlled_onboarding_scope_pending: '已有未完成的受控接入记录，请先查询原范围，不要重复创建',
  controlled_onboarding_own_workspace_required:
    '受控接入仅限本人工作空间，不能代客户接入或恢复旧绑定',
  controlled_onboarding_claimant_mismatch: '本次消息发送人或群组与固定的受控申请人、隔离群不一致',
  controlled_onboarding_role_changed: '受控申请人的群主或管理员角色已变化，请核对原范围',
  controlled_onboarding_native_budget_exhausted: '本范围的原生操作预算已用完，请查询原记录',
  controlled_onboarding_model_budget_exhausted: '本范围的模型调用预算已用完，请查询原记录',
  group_binding_add_account_unavailable: '此机器人暂未提供可搜索的公开加好友账号，请联系平台管理员',
  group_binding_friend_flow_retired: '此记录使用旧版接入方式，请继续私聊核验或重新开始',
  group_binding_outbound_invalid: '请核对本人可搜索账号及本次接入版本后再提交',
  group_binding_outbound_account_invalid: '请填写本人在当前平台的完整可搜索账号',
  group_binding_outbound_target_not_found: '未找到此账号，请在官方 App 核对本人可搜索账号',
  group_binding_outbound_target_ambiguous: '此账号的查找结果不唯一，请核对完整账号后重新查询',
  group_binding_outbound_target_changed: '本次接收好友申请的账号已固定，请刷新原接入记录核对',
  group_binding_outbound_target_inconsistent:
    '平台返回的账号资料与精确查找结果不一致，未发送好友申请；请核对原记录或联系管理员',
  group_binding_outbound_scope_changed: '本人、机器人或接入范围已变化，请刷新原记录核对',
  group_binding_outbound_self: '请填写本人的可搜索账号，不能填写本次机器人的账号',
  group_binding_outbound_read_incomplete: '尚未取得完整好友状态，未发送申请，请稍后手动核对',
  group_binding_outbound_response_invalid: '好友接口响应暂无法核验，请查询原记录，不会自动重发',
  group_binding_outbound_admission_closed:
    '此机器人的好友申请发送尚未开放，账号查找与已有好友的私聊核验仍可使用',
  group_binding_outbound_not_sendable: '本次好友申请尚不满足发送条件，请刷新原记录核对',
  group_binding_outbound_operation_unresolved:
    '本次好友申请结果待确认，只能核对原操作，不要再次发送',
  group_binding_outbound_attempt_not_found: '未找到本次好友申请原记录，请刷新接入进度核对',
  group_binding_outbound_submission_missing: '尚未取得本次好友申请的提交结果，请核对原记录',
  group_binding_outbound_pending: '已有发给此账号的好友申请，请在官方 App 核对原申请',
  group_binding_outbound_incoming_pending: '已有本人发来的待处理好友申请，请联系平台管理员核对',
  group_binding_outbound_already_friend: '已是好友，请发送本次独立私聊核验内容',
  group_binding_outbound_policy: '此账号的好友策略暂不支持本次申请，请在官方 App 核对',
  group_binding_outbound_blocked: '原生平台暂不允许添加此账号，请在官方 App 核对',
  group_binding_outbound_rejected: '本次好友申请未完成，不会自动重发，请核对原记录',
  group_binding_outbound_parameters_rejected:
    '平台未接受账号查找参数，请核对本人可搜索账号后手动操作',
  group_binding_outbound_security_required:
    '账号查找需要平台安全验证，请联系超管在官方 App 手动核对',
  group_binding_outbound_client_version: '平台不接受当前查找客户端版本，请联系超管核对',
  group_binding_outbound_ip_restricted: '平台限制了账号查找的网络来源，请联系超管核对',
  group_binding_outbound_lookup_forbidden: '机器人当前无权查找账号，请联系超管核对权限',
  group_binding_outbound_account_unavailable:
    '查找使用的机器人账号暂不可用，请联系超管核对账号状态',
  group_binding_outbound_rate_limited: '好友申请操作过于频繁，请稍后手动核对，不会自动重发',
  group_binding_outbound_busy: '机器人正在处理其他读取，请稍后手动核对',
  group_binding_outbound_cancelled: '本次好友操作已中断，请刷新原记录核对',
  group_binding_outbound_timeout: '好友接口等待超时，请核对原记录，不会自动重发',
  group_binding_outbound_unavailable: '当前机器人暂不能核对好友状态，请联系平台管理员',
  native_friend_request_acknowledged: '平台已确认好友申请提交，请在 App 接受后继续私聊核验',
  native_friend_request_observed: '已核对到本次好友申请，请在 App 接受后继续私聊核验',
  native_friend_relationship_observed: '已核对到好友关系，请继续完成独立私聊身份核验',
  native_friend_request_rejected: '已核对到本次好友申请被拒绝，可取消本次接入后重新开始',
  submission_deadline_elapsed: '原操作提交期限已结束，请核对原申请，不会重新发送',
  native_setting_result_unknown: '原生操作结果尚未确认，请核对原记录，不会自动重试',
  native_setting_unavailable: '本次原生操作尚未完成，请查看原记录，不会自动重试',
  process_interrupted: '服务重启中断了原操作，请查询原记录，不会自动重试',
  business_account_cooling: '机器人正在处理其他操作，请稍后手动核对，不会自动提交',
  group_binding_friend_challenge_unavailable:
    '本次好友核验内容暂不可用，请刷新进度或联系平台管理员',
  group_binding_friend_challenge_invalid: '好友核验内容不正确，请使用本次接入提供的完整内容',
  group_binding_friend_request_not_found:
    '未找到本次待处理好友申请。若已直接成为好友，请选择“已是好友或无需验证”，发送独立私聊核验内容',
  group_binding_friend_request_ambiguous: '匹配到多个好友申请，请联系平台管理员核对',
  group_binding_friend_response_invalid:
    '好友申请目录格式暂不兼容，未接受任何申请，请联系平台管理员',
  group_binding_friend_directory_no_data: '平台未返回好友申请目录数据，未接受任何申请',
  group_binding_friend_directory_missing: '平台响应缺少好友申请列表，未接受任何申请',
  group_binding_friend_directory_count: '平台好友申请列表与分页信息不一致，未接受任何申请',
  group_binding_friend_directory_recipient: '好友申请目录的接收账号与本次机器人不一致，未接受任何申请',
  group_binding_friend_directory_record: '平台好友申请记录格式暂不兼容，未接受任何申请',
  group_binding_friend_application_changed:
    '好友申请状态已变化，请刷新原进度核对，不要重复发送申请',
  group_binding_friend_identity_changed: '好友申请人与本次已核验身份不一致，未接受申请',
  group_binding_friend_required: '请先添加本次机器人为好友，或使用已有好友的私聊核验',
  group_binding_friend_already_exists: '已是好友，请私聊本次机器人发送身份核验内容',
  group_binding_friend_unknown: '好友核验结果待确认，请核对原操作，不要重新提交',
  group_binding_friend_submission_limit: '本次好友核验已有提交记录，请核对原操作',
  group_binding_friend_attempt_not_found: '未找到本次好友核验原操作，请刷新进度核对',
  group_binding_friend_unavailable: '当前平台暂不可核验好友申请，请联系平台管理员',
  group_binding_friend_accept_acknowledged: '好友申请已接受，请在官方 App 确认后邀请机器人入群',
  group_binding_friend_verified: '本次好友身份已核验，可邀请机器人入群',
  group_binding_challenge_expired: '本次接入证明已过期，请重新发起',
  group_binding_challenge_not_found: '本次接入记录不存在或不属于本人',
  group_binding_identity_unverified: '尚未收到本人发送的身份核验消息',
  group_binding_pool_identity: '请使用未接入号池的本人群主或管理员账号发送核验内容',
  group_binding_identity_changed: '核验消息发送账号与此前不一致，请使用此前核验的本人账号发送',
  group_binding_owner_required: '当前群内身份为管理员，需要由平台审核群归属',
  group_binding_invitation_unknown: '接受邀请的结果尚未确认，请查询原记录',
  group_binding_capacity_exceeded: '当前套餐或机器人群名额已满',
  group_binding_scope_changed: '群归属或机器人绑定已变化，请重新核验',
  group_binding_robot_unavailable: '当前没有连接正常且有可用群名额的管理机器人',
  group_binding_identity_required: '请先私聊本次机器人，完成邀请人身份核验',
  group_binding_private_identity_required: '尚未收到本人发给机器人的独立私聊核验消息',
  group_binding_native_proof_required: '尚未取得本人在目标群发送的有效群归属证明',
  group_ownership_proof_required: '请先完成本群认领，再使用群配置和助手',
  group_ownership_reproof_required: '本群归属需要重新核验，暂时不能修改',
  group_access_changed: '本群权限已变化，请重新读取后核对',
  operator_demo_claim_required: '请先在用户中心为本群完成测试认领',
  operator_demo_claim_expired: '测试认领已到期，请重新核验本群',
  operator_demo_claim_scope_changed: '群绑定、账号或权限已变化，请重新核验本群',
  operator_demo_claim_role_required: '当前绑定机器人需要在本群具有群主或管理员身份',
  operator_demo_claim_pending: '本群正在核验，请查看原操作进度',
  operator_demo_claim_native_unavailable:
    '当前无法读取本群原生角色，请核对机器人连接后手动重新核验',
  operator_demo_claim_interrupted: '上次核验已中断，请手动重新核验',
  operator_demo_claim_commit_unknown: '核验保存结果尚不明确，请查看原操作进度',
  operator_demo_claim_not_found: '未找到本人的测试认领记录',
  group_binding_native_proof_unavailable:
    '当前消息尚未证明群归属，请使用未接入号池的本人群主或管理员账号在群内重新发送本次完整核验内容；刷新进度仅查看结果',
  group_binding_native_role_required:
    '未能确认本人在该群的群主或管理员身份，请在官方 App 核对后，使用未接入号池的本人群主或管理员账号重新发送核验内容',
  group_binding_native_role_changed: '原生群角色已变化，请重新核验群归属',
  group_binding_receiver_unavailable: '本次机器人接收连接尚未就绪',
  group_binding_evidence_changed: '身份或群归属证据已变化，请重新核对本次接入',
  group_binding_group_changed: '核验到的群与本次接入目标不一致，请重新核对',
  group_binding_group_name_invalid: '原生群名称暂不符合要求，请在官方 App 核对',
  group_binding_invitation_unavailable: '未取得可核验的本人群邀请，请确认已邀请本次机器人',
  group_binding_invitation_changed: '该邀请已处理、过期或目标发生变化，请重新核对',
  group_binding_accept_invalid: '请填写有效群标识，并先核对对应群邀请',
  group_binding_accept_expired: '本次邀请核验已过期，请重新读取邀请',
  group_binding_submission_limit: '本次邀请已有提交记录，请核对原操作，不要重新发送',
  group_binding_submission_missing: '未找到本次邀请的原操作记录',
  group_binding_operation_unresolved: '本次接入仍有结果未明的操作，请先核对原记录',
  group_binding_challenge_not_confirmable: '当前接入尚不满足确认条件，请刷新进度',
  group_binding_challenge_not_cancellable: '此接入记录已结束，无法取消',
  group_binding_restore_not_allowed: '此群不能直接恢复授权，需要重新核验或联系平台',
  group_binding_transfer_review_required: '此群涉及客户归属转移，需要由平台审核',
  group_binding_transfer_scope_changed: '转出租户或当前群绑定已变化，请刷新后重新核对',
  group_binding_review_expired: '审核记录已变化或审核期限已过，请刷新原记录',
  group_business_runtime_conflict: '运行状态已被更新，草稿已保留，请刷新后核对',
  group_business_runtime_blocked: '本群尚未满足业务运行条件',
  tenant_capability_conflict: '权限上限已变化，草稿已保留，请刷新后核对',
  tenant_capability_denied: '该能力超出平台为本客户开通的权限上限',
  native_production_not_admitted: '本平台、群组及动作尚未通过正式准入',
  invalid_member: '成员信息或角色不正确',
  invalid_role: '角色不存在',
  tenant_owner_required: '仅租户主账号可以执行',
  tenant_owner_confirmation_required: '请先由超管确认租户主账号',
  owner_or_self_protected: '不能停用或更改主账号及本人角色',
  last_tenant_owner: '不能停用最后一个主账号',
  member_limit_reached: '套餐成员名额已满',
  permission_delegation_denied: '不能授予超出本人范围的权限',
  payment_amount_mismatch: '收款金额与套餐价格不一致',
  payment_already_confirmed: '此收款或订单已确认',
  plan_below_current_usage: '群组、热群或成员数量超出所选套餐，请先调整',
  billing_confirmation_required: '商业套餐请通过人工收款确认开通',
  ai_gateway_disabled: 'AI 网关未开放',
  platform_assistant_disabled: '超管助手未开放，或设置版本已变化',
  platform_assistant_budget_exhausted: '超管助手独立月预算不足',
  invalid_platform_assistant_settings: '请核对超管助手预算和启用状态',
  platform_assistant_invalid_request: '问题为空、超过 2000 字节或请求编号不正确',
  platform_assistant_busy: '已有执行中或待对账的超管助手回合，请先查询原记录',
  platform_assistant_session_limit: '24 小时内的超管助手会话数已达上限',
  platform_assistant_session_not_found: '此超管会话不存在、不属于本人或已过期',
  platform_assistant_not_running: '此超管助手回合已结束或不存在',
  gateway_not_configured: '请先保存业务模型配置',
  gateway_origin_or_v1_required: '来源须为 HTTPS 根地址或 /v1 地址',
  invalid_gateway_model: '业务模型来源、名称或密钥不正确',
  new_origin_requires_business_key: '更改上游来源时须重新提供业务密钥',
  model_route_changed: '模型配置已变化，请刷新后核对',
  invalid_model_rates: '模型费率不正确，不能将未核验价格作为免费费率',
  invalid_gateway_test: '请核对测试编号、当前模型及单次预留上限',
  test_call_budget_exhausted: '单次预留上限不足，本次未调用模型',
  gateway_test_unresolved: '已有执行中或结果未知的网关测试，请先查询对账',
  model_not_verified: '模型或费率尚未核验',
  model_rates_not_verified: '启用前请确认费率并设置平台预算',
  ai_credits_exhausted: '本期 AI 积分不足',
  ai_credit_sub_limit: '已达到用途、群或成员消费上限',
  platform_ai_budget_exhausted: '平台 AI 预算不足',
  test_ai_budget_exhausted: '超管测试预算不足',
  active_credit_period_required: '尚无已生效的 AI 积分账期',
  use_ai_gateway_configuration: '模型与费用请在统一 AI 网关中配置',
  ai_call_not_unknown: '此调用不处于待对账状态',
  ai_reconciliation_budget_conflict: '实际费用超出可用预算，需先核对账本',
  ai_gateway_busy: 'AI 请求队列已满，请稍后重试',
  native_social_request_invalid: '好友或群操作的目标、字段不正确',
  native_secondary_code_invalid: '二级安全码须为六位数字',
  native_capability_deferred: '此功能已暂缓接入',
  native_capability_not_in_product: '此功能不接入本产品',
  native_secondary_already_set: '此账号已设置安全码，请在官方 App 修改或找回',
  native_security_status_invalid: '原生安全码状态格式不兼容，未推断为未设置',
  native_identity_status_invalid: '实名认证状态格式不兼容，请在官方 App 核对',
  native_social_endpoint_required: '请从好友与邀请原生操作面板提交',
  native_social_limit_or_shape: '好友列表超出安全上限或返回格式不兼容，未展示部分数据',
  native_social_shape: '原生好友身份、状态或名单格式不兼容',
  native_social_application_changed: '申请已处理、方向不符或申请人已变化，请重新读取',
  native_social_friend_required:
    '目标不在当前好友名单、是机器人本人，或不符合本次邀请条件，未提交操作',
  native_social_target_invalid: '好友目标 ID 无效或为机器人本人，未提交操作',
  native_friend_delete_single_scope_required:
    '删除好友需独立原生调试窗口、单个机器人、单个目标和一次预算',
  native_friend_delete_controlled_only: '删除好友仅限超管的独立原生调试授权窗口',
  native_friend_delete_receiver_required:
    '删除好友前请先恢复当前机器人已有会话的接收连接，确认连接就绪',
  native_friend_delete_acknowledged: '平台已确认删除好友关系，未提交退群或拉黑',
  native_friend_approve_single_scope_required:
    '同意好友申请需独立原生调试窗口、单个机器人、单个申请人、固定申请编号和一次预算',
  native_friend_approve_controlled_only: '此人工同意入口仅限超管的指定申请受控窗口',
  native_friend_approve_receiver_required: '请先恢复当前机器人的已有接收连接，再核验这条好友申请',
  native_friend_application_required: '请使用授权窗口绑定的好友申请编号',
  native_friend_decision_acknowledged: '平台已确认处理此条好友申请，实际好友关系以回读为准',
  native_social_role_required: '本次好友或群操作只支持已核验的群主或管理员',
  native_social_leave_owner_or_absent: '机器人不在群、角色未核验或是群主，不能直接退群',
  native_social_stop_group_first: '请先关闭此群的群管及热群服务，再执行退群',
  native_social_not_in_plan: '群所属租户的有效套餐未开通旺商聊好友与邀请能力',
  native_social_evidence_required: '未取得当前好友或群角色证据，未放行操作',
  native_social_unavailable: '当前接入器暂不支持此好友或群操作',
  native_invitation_self_required: '仅能接受当前账号在指定群中尚待处理的本人邀请，请重新读取',
  native_invitation_accept_acknowledged: '上游已确认接受邀请，实际入群仍以完整成员名单回读为准',
  native_invitation_requested: '上游已确认邀请请求，实际入群仍需核验',
  native_security_flow_required: '请按账号安全流程分步操作',
  native_security_busy: '账号安全操作繁忙，请稍后再试',
  native_security_manual_review_required:
    '安全操作结果未知，请核对官方客户端；系统不会重试或用登录验证密码',
  native_security_flow_expired: '此验证已过期、账号已变化或验证步骤不匹配，请重新开始',
  native_security_sms_cooldown: '此账号刚刚发送过验证短信，请满 60 秒后再试',
  native_security_code_invalid: '请输入 6 位短信验证码',
  native_security_phone_invalid: '请输入有效的 11 位新手机号并完成人机验证',
  native_security_password_invalid: '新密码须为 6–16 位，至少两种字符，并且不同于旧密码',
  native_avatar_invalid: '头像须为不超过 1 MB、4096 像素和 400 万像素的 JPG / PNG',
  lajiaoliao_avatar_invalid: '辣椒聊头像格式校验未通过，请选择 JPG / PNG',
  lajiaoliao_avatar_contract: '辣椒聊头像上传契约不兼容，已停止本次操作',
  lajiaoliao_avatar_origin_unverified: '辣椒聊返回了未核验的头像来源，已停止本次操作',
  lajiaoliao_avatar_upload_rejected: '辣椒聊未接受本次头像上传，请核对原请求结果',
  lajiaoliao_avatar_upload_unconfirmed: '头像文件上传未获确认，尚未提交头像设置；本次操作已停止',
  native_setting_request_invalid: '设置字段、版本或旧值摘要不正确',
  lajiaoliao_native_role_required: '辣椒聊当前原生角色或目标层级不允许此操作，请重新读取成员',
  lajiaoliao_invalid_action: '辣椒聊操作字段或内容超出本平台限制',
  lajiaoliao_action_unavailable: '此项辣椒聊原生能力尚未接入',
  lajiaoliao_result_unknown: '辣椒聊请求结果未知，请查询原操作并只读核对，不会自动重发',
  lajiaoliao_request_rejected: '辣椒聊未接受本次操作，请在官方客户端核对',
  lajiaoliao_rate_limited: '辣椒聊限制了请求频率，请等待冷却，不会自动重试',
  lajiaoliao_token_expired: '辣椒聊原生会话已过期，请核对账号连接',
  lajiaoliao_reauth_required: '辣椒聊要求重新认证，请手动处理',
  lajiaoliao_kicked: '辣椒聊连接被其他设备退出，请核对官方客户端',
  lajiaoliao_receive_queue_full: '辣椒聊接收队列已满，连接已停止；请联系管理员核对接收状态，部分即时通知可能缺失',
  lajiaoliao_receive_persistence_failed: '辣椒聊即时推送未能持久保存，连接已停止；请联系管理员检查数据库，不能据此确认认领或消息送达',
  lajiaoliao_profile_shape: '辣椒聊资料响应不完整，未提交修改',
  lajiaoliao_password_invalid: '辣椒聊密码不符合要求，请核对官方客户端的密码规则',
  lajiaoliao_account_not_found: '辣椒聊未找到此账号',
  lajiaoliao_phone_registered: '该手机号已在辣椒聊注册或被占用',
  lajiaoliao_code_invalid: '辣椒聊验证码错误',
  lajiaoliao_code_expired: '辣椒聊验证码已过期，请手动重新申请',
  lajiaoliao_code_attempts_exceeded: '辣椒聊验证码验证次数已达上限，请等待后手动处理',
  lajiaoliao_code_used: '辣椒聊验证码已经使用',
  lajiaoliao_operation_restricted: '辣椒聊限制了本次操作，请在官方客户端核对',
  lajiaoliao_bound_phone_unavailable: '未能核验辣椒聊当前绑定的中国大陆手机号，未发送短信',
  lajiaoliao_captcha_required: '请完成人机验证后再手动提交',
  lajiaoliao_group_shape: '辣椒聊群状态响应不完整，未提交修改',
  lajiaoliao_friend_shape: '辣椒聊好友或申请列表不完整，未提交审批',
  lajiaoliao_membership_unverified: '未取得辣椒聊当前成员和角色证据',
  lajiaoliao_group_request_shape: '辣椒聊入群申请响应无法核验',
  lajiaoliao_invitation_shape: '辣椒聊本人邀请列表不完整或格式无法核验，未提交接受',
  lajiaoliao_invitation_self_required: '当前账号没有唯一匹配的待处理本人邀请，请重新读取',
  lajiaoliao_message_content_shape: '辣椒聊消息结构尚未兼容，原始记录保留',
  lajiaoliao_media_reference_unsupported: '此资源地址或大小暂不支持预览',
  lajiaoliao_media_unavailable: '辣椒聊媒体暂不可用，可稍后手动重试',
  lajiaoliao_media_expired: '原始媒体已不存在或已过期，请由发送者重新提供',
  lajiaoliao_media_access_denied: '上游不允许读取此媒体资源',
  lajiaoliao_media_invalid: '辣椒聊媒体大小、尺寸或格式校验未通过',
  native_message_readback_required: '结果仍需核对原消息回显或撤回通知，请保留原操作，勿重新发送',
  native_message_reference_expired: '消息已超出当前撤回验证范围，请重新读取近期消息',
  native_setting_name_invalid: '昵称最多 20 个 UTF-16 字符，不能含控制字符或首尾空格',
  native_setting_account_changed: '原生账号、操作开关或版本已变化，请重新读取',
  native_setting_snapshot_changed: '原生旧值、账号或接入配置已变化，请重新读取后确认',
  native_setting_group_binding_required: '此群尚未有效授权给当前机器人，或群管理已停用',
  native_setting_cards_not_in_plan: '群所属租户的有效套餐未开通旺商聊名片能力',
  native_setting_role_required: '当前角色、目标角色或群昵称修改限制不允许此操作',
  native_setting_role_unverified: '未取得完整的群昵称修改限制，未放行修改',
  native_setting_pending: '此账号或群已有处理中或结果未知的原生操作，请先查询核对',
  native_setting_identity: '账号或群身份不一致，未放行设置',
  native_setting_shape: '原生账号设置响应格式不兼容',
  native_setting_evidence_expired: '设置证据已过期，未自动重试',
  native_settings_unavailable: '原生设置暂不可用，未自动重试',
  native_media_unavailable: '原生媒体暂不可用，请稍后手动重试',
  native_media_busy: '媒体读取繁忙，请稍后重试',
  native_media_not_found: '媒体不存在、已过期或不属于当前会话',
  native_media_invalid: '媒体大小、尺寸或格式未通过校验',
  native_media_acceptance_pending: '媒体发送尚未完成原生隔离验收，暂未开放',
  native_media_platform_unavailable: '当前平台或会话的此类媒体发送尚未接入',
  native_upload_invalid: '文件、封面或媒体类型不正确',
  native_upload_audio_invalid: '语音须为不超过 300 秒、可核验完整帧的 MP3 或 16 位 PCM WAV',
  native_upload_video_invalid: '视频须为不超过 1 小时、可核验尺寸与时长的非分片 MP4',
  native_upload_too_large: '文件超过上传大小限制',
  native_upload_busy: '媒体中转繁忙，请稍后重试',
  native_upload_unavailable: '上传凭证已过期、失效或不属于当前账号和群',
  native_upload_scope_changed: '账号、群、权限或测试窗口已变化，未继续提交',
  native_upload_already_used: '此上传凭证已用于另一发送意图，请核对原操作',
  native_upload_result_unknown: '上传结果未知，不会自动重传',
  native_upload_rejected: '上游拒绝了媒体上传；旧记录未保留具体返回码',
  native_upload_http_rejected: '上游拒绝了媒体上传，不会自动重传',
  native_upload_rate_limited: '上游限制了上传频率，请停止操作并稍后手动核对',
  lajiaoliao_media_format_unavailable:
    '媒体格式不支持；图片使用 JPEG/PNG，语音使用 PCM WAV/MP3，视频使用 MP4 和 JPEG 封面',
  lajiaoliao_media_contract: '辣椒聊上传契约不兼容，已停止当前操作',
  lajiaoliao_media_origin_unverified: '辣椒聊返回了尚未核验的资源来源，已停止当前操作',
  lajiaoliao_media_upload_rejected: '辣椒聊未接受本次媒体上传，请核对原操作',
  native_video_too_large: '视频超过 16 MiB，仅显示信息与封面',
  native_media_scope_changed: '账号、权限或原消息已变化，请重新打开会话',
  native_media_origin_unreviewed: '此部署的媒体下载线路尚未核验',
  native_media_reference_unsupported: '原生资源引用格式尚未支持',
  invalid_media_reference: '图片引用不属于有效消息',
  native_text_acceptance_pending: '文本发送尚未通过隔离实测和 nonce 历史核验，暂未开放真实发送',
  native_text_group_unbound: '此原生群尚未授权绑定到当前机器人，不能发送',
  native_private_text_unavailable: '私聊文本发送尚未接入',
  native_private_target_unavailable: '私信目标证据缺失、过期或与当前账号不符，请刷新会话',
  native_private_target_changed: '私信目标证据已变化，当前操作未提交',
  native_private_message_invalid: '当前私信目标或消息条件未通过校验，不能提交新消息；请核对当前会话和证明授权',
  native_sending_context_unavailable: '未取得当前会话的发送条件，不能提交新消息；原发送记录仍保留',
  native_sending_result_unavailable: '暂时无法核对本次操作结果，请保留原记录；不会自动重发',
  native_text_platform_unavailable: '当前平台或账号用途的文本发送尚未接入',
  native_text_test_window_required: '请先在受控测试中创建本群的有效发送窗口',
  native_text_test_window_invalid: '测试窗口已过期、取消、次数用尽或不属于当前用户',
  native_text_test_target_invalid: '@ 成员不在本次测试明确授权的目标范围内',
  native_text_reference_unavailable: '引用原消息已过期、不可验证或不属于当前账号和群，未提交',
  native_text_reference_changed: '引用原消息的证据已变化，未提交，请重新选择',
  invalid_message_reference: '引用消息编号无效，未提交',
  native_text_account_changed: '账号登录、接入、测试开关或版本已变化，未放行发送',
  native_text_binding_changed: '群绑定、管理开关或版本已变化，请重新核对',
  native_text_connection_changed: '账号连接已变化或心跳不新鲜，未放行发送',
  native_text_identity_unverified: '原生身份尚未核验，未放行发送',
  native_text_unresolved_submission: '此账号存在执行中或结果未知的操作，请先对账',
  native_text_queue_full: '本账号发送队列已满',
  native_text_evidence_unavailable: '暂时无法读取回显证据，未重发消息',
  native_text_evidence_timeout: '回显核对超时，原发送结果仍待确认，未重发消息',
  native_text_evidence_cancelled: '回显核对已取消，原发送结果未改变',
  native_text_evidence_gap: '平台历史消息存在缺口，暂不能确认原发送，未重发消息',
  native_text_evidence_route_invalid: '平台历史消息范围未通过校验，未采信结果或重发消息',
  native_text_evidence_not_found: '没有可核对的发送证据',
  native_text_evidence_conflict: '回显证据冲突，仍需复核，未重发消息',
  onboarding_controlled_proof_window_invalid: '本次证明授权已过期、关闭或范围已变化；原发送仍可核对，不会自动重发',
  native_text_transport_unavailable: '受控发送通道尚未就绪',
  invalid_text_message: '消息为空或超出纯文本发送大小限制',
  invalid_operation: '发送请求字段或版本不正确',
  idempotency_conflict: '同一请求编号的内容已变化，未重复提交',
  operation_not_found: '未找到本用户在当前会话的发送记录',
  conversation_not_found: '当前账号的会话不存在或已过保留期限',
  native_member_group: '原生群 ID 不正确',
  native_member_directory_required: '请先同步当前账号的群目录，再读取成员',
  native_member_scope_changed: '账号会话、工作台或权限已变化，请重新打开成员面板',
  native_member_shape: '原生成员响应格式不兼容，未展示部分名单',
  native_member_identity: '成员身份映射冲突，未展示部分名单',
  native_member_role: '成员角色证据冲突，未放行群管操作',
  native_member_cursor: '成员分页游标不一致，未展示部分名单',
  native_member_incomplete: '成员分页未完成且缺少后续游标',
  native_member_limit: '成员读取达到安全上限，未展示部分名单',
  native_members_unavailable: '成员读取失败或已取消，未自动重试',
  native_read_owner_unavailable: '账号读取协调者尚未就绪',
  native_read_queue_full: '账号读取队列已满，请稍后手动重试',
  native_text_route_unverified: '未取得完整且一致的群身份、映射和禁言证据，未放行后续操作',
  native_role_evidence_required: '缺少当前连接的可信群身份信息，未更新角色',
  native_role_account_changed: '账号身份或登录版本已变化，请重新核验',
  native_role_deployment_changed: '接入协议配置已变化，请重新核验',
  native_role_connection_changed: '账号连接、租约或心跳已变化，请连接稳定后重新核验',
  native_role_evidence_expired: '群身份信息等待期间已过期，请重新核验',
  native_role_evidence_superseded: '已有更新的群身份信息，未用旧结果覆盖',
  native_role_unavailable: '暂时无法核验当前群身份，未自动重试',
  account_group_limit_reached: '此账号已绑定 30 个群，请选择其他账号',
  receiver_unobserved: '接收连接尚未取得新鲜观测',
  assignment_target_ineligible: '目标账号的登录、角色或运行状态不满足条件，请刷新预览',
  assignment_operations_pending: '此群有执行中或结果未知的操作，请先复核',
  assignment_commit_unknown: '绑定提交结果未知，请使用同一请求核对，勿新建迁移',
  idempotency_key_conflict: '此请求编号已用于其他内容',
  invalid_monitor_range: '历史查询范围必须在 24 小时以内',
  monitor_stream_busy: '监控订阅已满，请关闭其他监控页面',
  assistant_disabled: '群管助手未开放，或模型尚未配置',
  assistant_invalid_input: '问题为空或超过 8000 字节',
  assistant_session_limit: '七天内会话数量达到上限',
  assistant_session_busy: '此对话仍在生成或提交操作，请先停止生成或等待操作完成再删除',
  assistant_daily_limit: '今日回合数量达到上限',
  assistant_budget_exhausted: '助手 Token 额度不足，未发起模型请求',
  assistant_busy: '助手并发已满，请稍后重试',
  assistant_admin_preview: '助手当前仅向超管开放',
  assistant_not_configured: '请先配置助手专属模型',
  assistant_model_failed: '模型请求失败，未自动重试',
  assistant_model_rate_limited: '模型服务限流，未自动重试',
  assistant_invalid_model: '模型地址或参数不正确',
  invalid_assistant_model: '模型必须使用有效的公网 HTTPS 来源',
  assistant_history_scope_changed: '资源或权限已变化，该回合正文已停止交付，请新建回合',
  assistant_module_closed: '此平台尚未开放该助手业务能力，请联系负责人核对开放范围',
  assistant_module_policy_changed: '助手开放范围已变化，旧建议不能继续提交，请重新查询后生成建议',
  assistant_invalid_module_policy: '助手开放目录不匹配，请刷新后重新选择',
  assistant_history_pruned: '该回合已超出最近 10 轮，正文已清理',
  assistant_scope_mismatch: '目标超出当前助手范围',
  assistant_group_scope_required: '请先选择本人获授权的群组再提问',
  assistant_approvals_disabled: '人工审批入口未开放',
  assistant_commit_unknown: '提交结果未知，请按原计划编号核对状态，不要重复创建计划',
  assistant_scope_changed: '账号或群授权已变化，建议已失效',
  assistant_plan_not_pending: '建议已过期、取消或不再等待审批',
  assistant_plan_mismatch: '审批摘要与服务端计划不一致',
  assistant_action_invalid: '动作字段或目标不正确，请核对本平台支持的格式与范围',
  assistant_action_unavailable: '此平台的该动作尚未接通',
  assistant_action_executor_unavailable: '此动作的持久执行通道尚未接通',
  assistant_action_production_blocked: '此平台的动作尚未通过生产准入，建议未提交',
  assistant_member_lookup_unavailable: '当前无法查询群成员，请稍后重新读取',
  assistant_member_lookup_incomplete: '尚未取得完整成员证据，不能据此选择目标',
  assistant_target_invalid: '成员候选无效，请重新查询',
  assistant_target_expired: '成员候选已过期或授权已变化，请重新查询',
  assistant_target_choice_required: '存在多个匹配成员，请先点击选择具体成员',
  assistant_invalid_cursor: '群列表分页范围不正确，请重新读取',
  assistant_plan_not_found: '此建议不存在、不属于本人或不在当前平台',
  assistant_session_not_found: '此对话不存在、不属于本人或不在当前平台',
  assistant_truncated_config: '配置超出安全读取范围，不能生成可执行建议',
  assistant_limit: '本回合达到执行或结果大小限制',
  assistant_cancelled: '回合已取消',
  assistant_expired: '回合已超时',
  assistant_restart_interrupted: '服务重启中断此回合，不会自动重新生成或扣费',
  assistant_unavailable: '助手暂不可用',
  invalid_credentials: '账号或密码不正确',
  login_rate_limited: '登录尝试过多，请稍后重试',
  login_busy: '登录服务繁忙',
  version_conflict: '配置已变化，请刷新后重新编辑',
  username_exists: '用户名已存在',
  account_limit_reached: '已达到 10 个账号上限',
  native_compatibility_not_verified: '原生协议兼容尚未验收，不能启用或发送操作',
  ai_isolation_not_verified: 'AI 群级隔离尚未验收，不能启用',
  fresh_native_role_required: '发送账号在本群的角色信息需要重新核验。请刷新该账号的群资料，确认仍为群主或管理员后重试。',
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
  superadmin_required: '此功能仅限超管使用',
  native_login_required: '请先完成此机器人原生登录',
  receiver_unavailable: '消息接收服务尚未启动',
  native_im_profile_missing: '尚未导入原生 IM 配置',
  native_im_profile_invalid: 'IM 配置格式不正确',
  native_disconnect_before_profile: '请先停用所有账号的消息接收，再更新 IM 配置'
}

Object.assign(errors, {
  native_test_window_invalid: '授权窗口无效、已过期、已撤销、次数已用完，或账号/套餐已变化',
  native_test_receiver_required: '请先启用消息接收，等待连接、同步和心跳恢复后，再启用动作准入',
  invalid_test_scope: '请明确选择测试群、账号、有效期和次数上限',
  invalid_test_action: '允许动作不正确或有重复',
  invalid_test_accounts: '测试账号不正确或有重复',
  invalid_test_targets: '目标 UID 不正确或有重复',
  native_test_mute_limit: '受控验证禁言最多5分钟',
  native_moderation_target_protected: '不能操作机器人本人、群主、同级管理员或未核验成员',
  native_moderation_role_required: '缺少当前原生群主或管理员证据',
  native_moderation_evidence_required: '当前群角色证据不完整或已过期，请重新读取',
  native_moderation_shape: '原生群状态响应不完整，无法安全核验',
  native_moderation_acknowledged: '上游已确认操作，实际效果尚待核对',
  desired_state_observed: '只读核对已观察到期望状态，不代表重复提交',
  native_setting_pending: '此账号或群已有未决操作，请先查询并核对原结果',
  native_setting_snapshot_changed: '原生状态、凭据或版本已变化，本次未提交',
  operation_not_found: '尚未查到原操作，请保留请求编号，等待后再查询',
  maibot_section_not_found: '热群配置模块不存在',
  invalid_maibot_settings: '热群配置不正确，请核对时段、范围、HTTPS 模型地址及保留期限',
  maibot_settings_unreadable: '热群配置无法读取，请核对主密钥',
  invalid_workspace: '工作台不存在',
  workspace_route_forbidden: '此功能不属于当前工作台',
  permission_denied: '当前用户没有此操作权限，或套餐未包含该功能',
  invalid_feature_settings: '功能字段、类型或内容超出允许范围',
  use_feature_settings: '独立功能开关请在功能管理中修改',
  group_authorization_inactive: '群授权已撤销，不能修改功能配置',
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
  native_mobile_configuration_missing: '尚未导入手机搜索协议配置，请先由超管完成配置',
  native_mobile_configuration_invalid:
    '手机搜索协议配置格式、密钥或加密存储无效，请联系平台管理员核对',
  native_mobile_configuration_changed: '手机搜索协议配置已变化，请重新读取配置并重新认证',
  native_mobile_required: '此入口需要手机搜索协议配置，请勿导入桌面协议配置',
  native_mobile_reauth_required:
    '手机搜索会话已失效，请联系超管为此机器人手动重新认证；不会自动登录',
  native_mobile_identity_mismatch:
    '手机登录身份与此机器人不一致，业务搜索会话未保存，请使用同一原生账号',
  native_mobile_account_unavailable: '此旺商聊管理机器人尚不可用，请先完成桌面登录或重新选择账号',
  native_mobile_account_changed:
    '机器人账号或桌面会话已变化，本次手机搜索会话未保存，请重新打开认证',
  native_mobile_search_changed:
    '手机搜索会话或配置已变化，请重新查找本人账号，不会自动发送好友申请',
  native_mobile_search_required:
    '此机器人的手机搜索尚未配置或认证，请联系平台管理员；已有好友可继续私聊核验',
  native_check_busy: '正在验证桌面配置，请稍后查看结果',
  native_check_cooldown: '配置验证冷却中，请至少等待 30 秒后重试',
  native_check_stale: '验证期间配置或网站权限已变化，请重新打开配置面板',
  native_desktop_required: '仅支持桌面端 2.8.3 协议，移动端接入已移除',
  native_desktop_configuration_required: '移动端接入已移除，请先导入桌面端协议配置',
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
  native_verification_rejected: '旺商聊未通过验证码校验，请核对短信验证码并重新完成人机验证',
  native_verification_expired: '旺商聊返回验证码已过期，请核对当前验证步骤，不会自动重新提交',
  native_rate_limited: '旺商聊限制了请求频率，请等待本地冷却后再核对；不会自动重试',
  native_sms_rate_limited: '旺商聊限制了短信请求频率，请暂缓申请；不会自动重发',
  native_captcha_rejected: '旺商聊未通过本次人机或安全验证，请重新完成验证后手动提交',
  native_request_parameters_rejected: '旺商聊返回请求参数不符合要求，请核对当前接入契约',
  native_request_parameters_missing: '旺商聊返回请求缺少必要参数，请核对当前接入契约',
  native_client_version_rejected: '旺商聊拒绝了当前客户端版本，请先核对官方版本要求',
  native_ip_restricted: '旺商聊限制了当前出口 IP 的请求，请联系平台核对；不会自动切换线路重试',
  native_login_rejected_unspecified: '旺商聊拒绝了登录请求，尚无法确认具体原因',
  native_signup_rejected_unspecified:
    '旺商聊拒绝了注册请求，当前尚未确认具体原因；不会自动重复开户',
  native_sms_rejected_unspecified: '旺商聊拒绝了短信验证码请求，当前尚未确认具体原因；不会自动重发',
  native_signup_phone_registered: '旺商聊返回此手机号已注册，请切换原生登录，不会再次开户',
  native_phone_range_restricted: '旺商聊明确提示该号段不支持注册，本次请求已停止',
  native_phone_registration_restricted: '旺商聊明确提示该手机号不支持注册，本次请求已停止',
  native_phone_not_found: '旺商聊未找到对应手机号',
  native_security_invalid: '安全码不正确',
  native_security_locked: '安全码验证暂时锁定，请在官方客户端核对',
  native_transport_unknown: '请求结果未知，请先核对官方客户端，不会自动重试',
  native_origin_rejected: '官方域名解析到非公网地址，安全策略已阻止连接，请检查服务端 DNS',
  native_dns_failed: '服务端无法解析官方域名，未发送请求，请检查 DNS',
  native_dns_fake_ip: '服务端 DNS 返回代理映射地址，未发送请求，请配置真实 DNS 或启用加密 DNS 回退',
  native_dns_fallback_failed: '加密 DNS 未取得可用的公网地址，未发送请求，请检查服务端网络',
  native_login_binding: '登录响应未通过密钥交换绑定校验',
  native_mobile_login_response_invalid:
    '手机登录响应的身份或安全字段无法核验，搜索会话未保存；不会自动重试',
  native_account_changed: '账号配置已变化，此次登录未保存',
  native_deployment_changed: '协议配置已变化，请重新登录',
  native_site_session_expired: '网站会话已失效，此次原生登录未保存',
  native_identity_already_bound: '此旺商聊身份已经绑定到另一个账号面板',
  native_disconnect_before_login: '请先关闭此账号的“消息接收”，再进行原生登录',
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

const evidenceStages: Record<string, string> = {
  input: '检查原操作',
  account: '核对账号',
  durable_read: '读取已接收记录',
  durable_open: '读取已接收记录',
  durable_match: '核对已接收记录',
  connection: '核对接收连接',
  schedule: '安排回显核对',
  history_guard: '核对历史读取范围',
  sequences: '读取平台会话水位',
  conversation: '核对历史会话',
  pull: '读取平台历史',
  history_validate: '核对历史消息',
  final_fence: '复核当前范围',
  history_empty: '核对平台历史返回',
  history_partial: '核对平台历史数量',
  history_scope: '核对平台历史范围',
  history_record: '核对平台历史字段',
  history_sequence: '核对平台历史序列'
}
const evidenceReasons: Record<string, string> = {
  owner_changed: '连接或授权范围已变化',
  route_changed: '消息范围未通过校验',
  busy: '只读查询通道繁忙',
  rate_limited: '平台暂时限制查询频率',
  timeout: '查询超时',
  cancelled: '查询已取消',
  reauth_required: '原生登录状态已失效',
  disconnected: '原生连接中断或未返回结果',
  rejected: '平台拒绝了只读查询',
  protocol: '平台响应暂无法核验',
  gap: '历史消息存在缺口',
  conflict: '消息证据冲突',
  unavailable: '暂时无法取得记录'
}
for (const [stage, label] of Object.entries(evidenceStages))
  for (const [reason, text] of Object.entries(evidenceReasons))
    errors[`lajiaoliao_evidence_${stage}_${reason}`] =
      `${label}时，${text}。原发送结果仍待核对，未重发消息`
for (const [stage, text] of Object.entries({
  history_empty: '平台未返回本次请求范围内的历史消息',
  history_partial: '平台返回的历史消息数量不足或超出请求范围',
  history_scope: '平台返回的历史会话与原操作不一致',
  history_record: '平台历史含有空记录、无效消息标识或超限内容',
  history_sequence: '平台返回的历史消息序列重复或不连续'
}))
  errors[`lajiaoliao_evidence_${stage}_gap`] =
    `${text}。原发送结果仍待核对，未重发消息`

export function errorText(code: string) {
  if (code === 'assistant_invalid_tool') return '模型调用了当前不可用的查询工具；本轮未执行变更'
  if (code === 'assistant_invalid_tool_arguments')
    return '模型返回的工具参数不符合要求；本轮已停止，未执行变更'
  if (code === 'assistant_invalid_tool_selector')
    return '模型返回的群或机器人选择参数无效；本轮已停止，未执行变更'
  const upload = /^native_upload_business_rejected_([1-9][0-9]{0,5})$/.exec(code)
  if (upload) return `上游拒绝了媒体上传，业务码 ${upload[1]}；不会自动重传`
  return errors[code] || code || ''
}

const lookupCategories = [
  'rate_limited',
  'session_expired',
  'parameters_rejected',
  'security_verification',
  'client_version',
  'ip_restricted',
  'permission_denied',
  'account_unavailable',
  'upstream_rejected',
  'transport_unavailable',
  'response_invalid'
] as const
const lookupIdentityReasonLabels = {
  friend_info_uid_missing: '好友资料未返回 UID',
  friend_info_uid_null: '好友资料 UID 为 null',
  friend_info_uid_zero: '好友资料 UID 为默认零',
  friend_info_uid_invalid: '好友资料 UID 格式无效',
  friend_info_uid_mismatch: '好友资料 UID 与固定目标不一致',
  friend_info_account_missing: '好友资料未返回 accountId',
  friend_info_account_null: '好友资料 accountId 为 null',
  friend_info_account_zero: '好友资料 accountId 为默认零',
  friend_info_account_invalid: '好友资料 accountId 格式无效',
  friend_info_account_mismatch: '好友资料 accountId 与固定账号不一致'
} as const
const lookupReasons = [
  'provider_code_classified',
  'provider_reason_classified',
  'provider_reason_empty',
  'provider_reason_unrecognized',
  'provider_reason_oversized',
  'provider_reason_nontext',
  'http_status',
  'network_timeout',
  'network_unavailable',
  'connection_closed',
  ...(Object.keys(lookupIdentityReasonLabels) as (keyof typeof lookupIdentityReasonLabels)[])
] as const
export interface LookupDiagnostic {
  version: 1
  platform: 'wsl'
  stage: 'search' | 'friend_info'
  category: (typeof lookupCategories)[number]
  provider_code?: number
  reason?: (typeof lookupReasons)[number]
  observed_at: string
  retry_after_seconds?: number
}
function safeLookupDiagnostic(value: unknown): LookupDiagnostic | undefined {
  if (!value || typeof value !== 'object' || Array.isArray(value)) return
  const item = value as Record<string, unknown>
  if (
    item.version !== 1 ||
    item.platform !== 'wsl' ||
    (item.stage !== 'search' && item.stage !== 'friend_info') ||
    !lookupCategories.includes(item.category as LookupDiagnostic['category']) ||
    typeof item.observed_at !== 'string' ||
    item.observed_at.length > 64 ||
    !/^\d{4}-(0[1-9]|1[0-2])-(0[1-9]|[12]\d|3[01])[Tt]([01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d+)?(?:[Zz]|[+-]([01]\d|2[0-3]):[0-5]\d)$/.test(
      item.observed_at
    ) ||
    !Number.isFinite(Date.parse(item.observed_at))
  )
    return
  const [year, month, day] = item.observed_at.slice(0, 10).split('-').map(Number)
  const days = [
    31,
    year % 4 === 0 && (year % 100 !== 0 || year % 400 === 0) ? 29 : 28,
    31,
    30,
    31,
    30,
    31,
    31,
    30,
    31,
    30,
    31
  ]
  if (day > days[month - 1]) return
  const diagnostic: LookupDiagnostic = {
    version: 1,
    platform: 'wsl',
    stage: item.stage,
    category: item.category as LookupDiagnostic['category'],
    observed_at: item.observed_at
  }
  if (
    typeof item.provider_code === 'number' &&
    Number.isInteger(item.provider_code) &&
    (item.provider_code === -1 || (item.provider_code >= 1 && item.provider_code <= 9999))
  )
    diagnostic.provider_code = item.provider_code
  if (
    lookupReasons.includes(item.reason as NonNullable<LookupDiagnostic['reason']>) &&
    (!Object.prototype.hasOwnProperty.call(lookupIdentityReasonLabels, item.reason as string) ||
      (item.stage === 'friend_info' && item.category === 'response_invalid'))
  )
    diagnostic.reason = item.reason as LookupDiagnostic['reason']
  if (
    diagnostic.category === 'rate_limited' &&
    typeof item.retry_after_seconds === 'number' &&
    Number.isInteger(item.retry_after_seconds) &&
    item.retry_after_seconds >= 1 &&
    item.retry_after_seconds <= 86400
  )
    diagnostic.retry_after_seconds = item.retry_after_seconds
  return diagnostic
}

export class ApiError extends Error {
  public readonly lookupDiagnostic?: LookupDiagnostic

  constructor(
    public status: number,
    public code: string,
    lookupDiagnostic?: unknown
  ) {
    super(errors[code] || code)
    this.lookupDiagnostic = safeLookupDiagnostic(lookupDiagnostic)
    const reason = this.lookupDiagnostic?.reason
    if (
      code === 'group_binding_outbound_target_inconsistent' &&
      reason &&
      Object.prototype.hasOwnProperty.call(lookupIdentityReasonLabels, reason)
    )
      this.message += `；${lookupIdentityReasonLabels[reason as keyof typeof lookupIdentityReasonLabels]}`
  }
}

export function sendingAdmissionErrorText(cause: unknown) {
  const code = cause instanceof ApiError ? cause.code : cause
  return typeof code === 'string' && Object.prototype.hasOwnProperty.call(errors, code)
    ? errors[code]
    : errors.native_sending_context_unavailable
}

export function sendingResultErrorText(message: unknown, admissionError = '') {
  if (typeof message === 'string' && Object.values(errors).includes(message)) return message
  return admissionError || errors.native_sending_result_unavailable
}

export async function api<T>(
  path: string,
  method = 'GET',
  body?: unknown,
  signal?: AbortSignal,
  workspace: Workspace = 'wsl',
  requestKey?: string,
  captchaTicket?: string
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
      ...(body instanceof FormData ? {} : { 'Content-Type': 'application/json' }),
      'X-Workspace': workspace === 'user' ? 'wsl' : workspace,
      'X-CSRF-Token': csrf,
      ...(captchaTicket ? { 'X-Human-Verification': captchaTicket } : {}),
      ...(requestKey ? { 'Idempotency-Key': requestKey } : {}),
      ...(method === 'POST' && path === '/operations'
        ? { 'Idempotency-Key': crypto.randomUUID() }
        : {})
    },
    body: body === undefined ? undefined : body instanceof FormData ? body : JSON.stringify(body)
  })
  const result = await response.json()
  if (!response.ok || result.code !== 0) {
    const lookup =
      method === 'POST' &&
      /^\/(?:group-binding|test-tools\/onboarding-friend)\/challenges\/[^/]+\/friend-target$/.test(
        path
      )
    const sending =
      method === 'GET' && /^\/accounts\/[^/]+\/conversations\/[^/]+\/sending$/.test(path)
    // These reads expose fixed classifications only, never upstream prose.
    const code =
      (lookup || sending) &&
      (typeof result.msg !== 'string' || !Object.prototype.hasOwnProperty.call(errors, result.msg))
        ? lookup
          ? 'group_binding_outbound_rejected'
          : 'native_sending_context_unavailable'
        : result.msg
    throw new ApiError(
      response.status,
      code,
      lookup && workspace === 'wsl' ? result.data?.lookup_diagnostic : undefined
    )
  }
  return result.data as T
}
