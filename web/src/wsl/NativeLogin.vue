<template>
  <ElDialog
    :model-value="modelValue"
    :title="
      mobileSearch
        ? `${currentAccount?.name || '旺商聊'} · 手机搜索认证`
        : currentAccount
          ? `${currentAccount.name} · 注册 / 登录`
          : `添加${workspace === 'maibot' ? '热群' : '旺商聊'}机器人`
    "
    class="native-auth-dialog"
    width="560px"
    align-center
    append-to-body
    :close-on-click-modal="false"
    :close-on-press-escape="!busy"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <ElTabs
      v-if="!mobileSearch"
      v-model="purpose"
      :before-leave="prepareSwitch"
      class="native-auth-tabs"
    >
      <ElTabPane label="原生登录" name="login" :disabled="locked" />
      <ElTabPane label="新账号注册" name="signup" :disabled="locked || !canSignup" />
    </ElTabs>
    <p v-if="!mobileSearch && !canSignup && !signup" class="password-policy">
      已绑定身份、已注册或开户结果待复核的账号面板不能再次注册。
    </p>
    <ElAlert
      v-if="error || flow.error"
      :title="error || errorText(flow.error)"
      :description="error ? '' : diagnosticText"
      type="error"
      :closable="false"
      class="flow-alert"
    />
    <ElAlert
      v-if="mobileSearch && !mobileAccountAllowed"
      :title="errorText('native_mobile_account_unavailable')"
      type="warning"
      :closable="false"
      class="flow-alert"
    />
    <ElAlert
      v-else-if="!deployment.configured"
      :title="
        mobileSearch
          ? '尚未导入手机搜索协议配置'
          : deployment.migration_required
            ? '移动端接入已移除，请先导入桌面端协议配置'
            : '尚未导入桌面端协议配置'
      "
      type="warning"
      :closable="false"
      class="flow-alert"
    />
    <ElAlert
      v-else-if="!deployment.crypto_ready"
      title="当前服务未使用 libsodium 构建，不能提交原生登录"
      type="warning"
      :closable="false"
      class="flow-alert"
    />
    <div class="auth-status">
      <ElTag :type="terminal ? 'success' : 'info'">{{ statusLabel }}</ElTag>
      <span v-if="flow.transaction_id && !terminal">本次验证 {{ expirySeconds }} 秒后过期</span>
    </div>
    <ElAlert
      v-if="deployment.configured"
      :title="mobileSearch ? '独立手机搜索会话' : '桌面端接入'"
      :description="
        mobileSearch
          ? '仅为此机器人保存业务搜索会话，与桌面消息接收独立。登录和短信均需本人手动提交。'
          : '仅使用桌面业务协议；消息连接使用 Web IM。多端同时在线仍需独立验收。'
      "
      type="info"
      :closable="false"
      class="flow-alert"
    />
    <ElDescriptions v-if="flow.status === 'authenticated'" :column="1" border>
      <ElDescriptionsItem label="原生 UID">{{ flow.native_uid }}</ElDescriptionsItem>
      <ElDescriptionsItem label="昵称">{{ flow.nickname || '—' }}</ElDescriptionsItem>
      <ElDescriptionsItem v-if="mobileSearch" label="业务搜索会话">已保存</ElDescriptionsItem>
      <ElDescriptionsItem v-else label="消息接收">尚未连接</ElDescriptionsItem>
    </ElDescriptions>
    <ElDescriptions v-else-if="!mobileSearch && flow.status === 'registered'" :column="1" border>
      <ElDescriptionsItem label="旺商聊账号">{{ flow.registered_account }}</ElDescriptionsItem>
      <ElDescriptionsItem label="登录状态">尚未登录</ElDescriptionsItem>
    </ElDescriptions>
    <ElForm v-else-if="ready" label-position="top" @submit.prevent="submit">
      <ElFormItem v-if="!mobileSearch && !currentAccount" label="机器人名称">
        <ElInput v-model="robotName" maxlength="100" autocomplete="off" :disabled="locked" />
      </ElFormItem>
      <template v-if="signup && ['pending', 'failed', 'signup_sms_required'].includes(flow.status)">
        <ElFormItem v-if="flow.status !== 'signup_sms_required'" label="注册手机号">
          <ElInput
            v-model="nativeAccount"
            inputmode="tel"
            autocomplete="off"
            maxlength="11"
            :disabled="busy"
          />
        </ElFormItem>
        <p class="password-policy">原生昵称不在此注册步骤设置，机器人名称仅为网站备注。</p>
        <template v-if="flow.status === 'signup_sms_required'">
          <ElFormItem label="注册短信验证码">
            <ElInput
              v-model="code"
              inputmode="numeric"
              maxlength="6"
              autocomplete="one-time-code"
              :disabled="busy"
            />
          </ElFormItem>
          <ElFormItem label="设置原生账号密码">
            <ElInput
              v-model="password"
              type="password"
              show-password
              autocomplete="new-password"
              maxlength="16"
              :disabled="busy"
            />
          </ElFormItem>
          <p class="password-policy">6–16 位，不能仅含字母、数字或特殊字符。</p>
        </template>
        <ElCheckbox v-model="agreed" :disabled="busy" class="signup-consent">
          我已阅读并同意旺商聊用户协议和隐私政策，并确认有权使用此手机号。
        </ElCheckbox>
      </template>
      <template v-if="!signup && ['pending', 'failed'].includes(flow.status)">
        <ElFormItem label="登录方式">
          <ElRadioGroup v-model="method" :disabled="busy"
            ><ElRadioButton value="password">密码登录</ElRadioButton
            ><ElRadioButton value="sms">短信登录</ElRadioButton></ElRadioGroup
          >
        </ElFormItem>
        <ElFormItem :label="method === 'sms' ? '手机号' : '账号 / 手机号'">
          <ElInput v-model="nativeAccount" autocomplete="off" maxlength="128" :disabled="busy" />
        </ElFormItem>
        <ElFormItem v-if="method === 'password'" label="原生账号密码">
          <ElInput
            v-model="password"
            type="password"
            show-password
            autocomplete="off"
            maxlength="1024"
            :disabled="busy"
          />
        </ElFormItem>
      </template>
      <ElFormItem v-if="flow.status === 'sms_required'" label="短信验证码">
        <ElInput
          v-model="code"
          inputmode="numeric"
          maxlength="6"
          autocomplete="one-time-code"
          :disabled="busy"
        />
      </ElFormItem>
      <ElFormItem v-if="flow.status === 'security_required'" label="六位安全码">
        <ElInput
          v-model="code"
          type="password"
          inputmode="numeric"
          maxlength="6"
          autocomplete="off"
          :disabled="busy"
        />
      </ElFormItem>
      <ElAlert
        v-if="flow.status === 'unknown'"
        title="上游请求结果未知，未自动重试。请先核对官方客户端状态。"
        type="warning"
        :closable="false"
      />
      <ElAlert
        v-if="flow.status === 'security_setup_required'"
        title="请先在官方客户端完成安全码或手机号设置。"
        type="warning"
        :closable="false"
      />
      <div class="auth-actions">
        <ElButton
          v-if="
            [
              'pending',
              'failed',
              'sms_required',
              'signup_sms_required',
              'security_required',
              'connection_pending'
            ].includes(flow.status)
          "
          native-type="submit"
          type="primary"
          :icon="Key"
          :loading="busy || flow.status === 'working'"
          :disabled="locked || submitWaitSeconds > 0 || expired || (signup && !agreed)"
        >
          {{ submitLabel }}{{ submitWaitSeconds > 0 ? ` (${submitWaitSeconds}s)` : '' }}
        </ElButton>
        <ElButton
          v-if="flow.status === 'sms_required' || flow.status === 'signup_sms_required'"
          :disabled="busy || resendSeconds > 0 || retrySeconds > 0 || expired"
          @click="send(signup ? 'signup_resend' : 'sms_resend')"
        >
          重新发送{{ resendSeconds > 0 ? ` (${resendSeconds}s)` : '' }}
        </ElButton>
      </div>
    </ElForm>
    <template #footer>
      <ElButton v-if="flow.transaction_id && !terminal" :disabled="locked" @click="cancel"
        >取消验证</ElButton
      >
      <ElButton
        v-if="!mobileSearch && flow.status === 'registered'"
        type="primary"
        :disabled="locked"
        @click="loginRegistered"
        >登录新账号</ElButton
      >
      <ElButton
        v-if="
          !terminal &&
          (expired ||
            ['cancelled', 'expired', 'unknown'].includes(flow.status) ||
            (mobileSearch && flow.status === 'connection_pending') ||
            (currentAccount &&
              !flow.transaction_id &&
              deployment.configured &&
              deployment.crypto_ready))
        "
        :disabled="locked"
        @click="restart"
        >重新开始</ElButton
      >
      <ElButton @click="emit('update:modelValue', false)">关闭面板</ElButton>
    </template>
  </ElDialog>
  <Teleport to="body">
    <div ref="captchaHost" class="native-captcha-layer"></div>
  </Teleport>
</template>

<script setup lang="ts">
  import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
  import { Key } from '@element-plus/icons-vue'
  import { api as request, ApiError, errorText, type Row } from './api'
  import { nativeCaptcha } from './nativeCaptcha'

  const props = withDefaults(
    defineProps<{
      modelValue: boolean
      account: Row | null
      owner: string
      workspace: import('./workspaces').Workspace
      adapter?: 'desktop' | 'mobile_search'
    }>(),
    { adapter: 'desktop' }
  )
  const emit = defineEmits<{ 'update:modelValue': [boolean]; updated: []; created: [Row] }>()
  const api = <T,>(path: string, method = 'GET', body?: unknown) =>
    request<T>(path, method, body, undefined, props.workspace)
  const mobileSearch = computed(() => props.adapter === 'mobile_search')
  const mobileAccountAllowed = computed(
    () =>
      !!props.account?.id &&
      props.account.platform === 'wsl' &&
      props.account.purpose === 'management' &&
      props.account.auth_state === 'authenticated' &&
      props.workspace === 'wsl'
  )
  const authPath = (id: string) =>
    `/accounts/${id}/${mobileSearch.value ? 'native-mobile-auth' : 'native-auth'}`
  const deploymentPath = () =>
    mobileSearch.value ? '/native-mobile-deployment' : '/native-deployment'
  const createdAccount = ref<Row | null>(null)
  const currentAccount = computed(
    () => props.account || (mobileSearch.value ? null : createdAccount.value)
  )
  const robotName = ref('')
  const handles = new Map<string, string>()
  const flow = ref<Row>({}),
    deployment = ref<Row>({}),
    panel = ref<Row>({}),
    purpose = ref<'login' | 'signup'>('login'),
    initializing = ref(false),
    method = ref('password')
  const nativeAccount = ref(''),
    password = ref(''),
    agreed = ref(false),
    code = ref(''),
    error = ref(''),
    busy = ref(false),
    now = ref(Date.now())
  const captchaHost = ref<HTMLElement>()
  let generation = 0,
    polling = false,
    verification: AbortController | undefined,
    timer: ReturnType<typeof setInterval> | undefined
  const signup = computed(() => !mobileSearch.value && purpose.value === 'signup')
  const locked = computed(() => initializing.value || busy.value || flow.value.status === 'working')
  const canSignup = computed(
    () =>
      !mobileSearch.value &&
      !panel.value.native_uid &&
      !panel.value.connection_enabled &&
      !['submitted', 'registered'].includes(panel.value.signup_state) &&
      (!currentAccount.value || panel.value.auth_state === 'not_authenticated') &&
      !['authenticated', 'registered'].includes(flow.value.status)
  )
  const terminal = computed(
    () =>
      flow.value.status === 'authenticated' ||
      (!mobileSearch.value && flow.value.status === 'registered')
  )
  const signupPasswordCharacters = new Set(
    "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ×÷`¬!£$%^*()~=#{}[];':,./?-_+<>@&"
  )
  const scope = () =>
    `${props.owner}:${props.workspace}:${props.adapter}:${
      mobileSearch.value ? `${props.account?.version}:${deployment.value.version}:` : ''
    }${currentAccount.value?.id || 'new'}`
  const seconds = (value: string) =>
    Math.max(0, Math.ceil((Date.parse(value) - now.value) / 1000)) || 0
  const retrySeconds = computed(() => seconds(flow.value.retry_at))
  const resendSeconds = computed(() => seconds(flow.value.resend_at))
  const submitWaitSeconds = computed(() =>
    Math.max(
      retrySeconds.value,
      ['pending', 'failed'].includes(flow.value.status) && (signup.value || method.value === 'sms')
        ? resendSeconds.value
        : 0
    )
  )
  const expirySeconds = computed(() => seconds(flow.value.expires_at))
  const expired = computed(() => !!flow.value.expires_at && expirySeconds.value === 0)
  const ready = computed(
    () =>
      !initializing.value &&
      (!mobileSearch.value || mobileAccountAllowed.value) &&
      deployment.value.configured &&
      deployment.value.crypto_ready &&
      (!!flow.value.transaction_id || !currentAccount.value)
  )
  const statuses: Record<string, string> = {
    pending: '等待登录',
    working: '正在验证',
    sms_required: '等待短信验证码',
    signup_sms_required: '等待注册短信验证码',
    registered: '注册成功，尚未登录',
    security_required: '等待安全码',
    connection_pending: '正在取得 IM 凭据',
    authenticated: '已登录，会话已加密保存',
    failed: '登录未完成',
    unknown: '请求结果未知',
    security_setup_required: '需要官方客户端设置',
    cancelled: '已取消',
    expired: '已过期'
  }
  const diagnosticText = computed(() => {
    const details: Record<string, string> = {
      network_timeout: '网络请求超时，未取得可核验的上游结果。',
      connection_closed: '上游连接提前关闭，未取得可核验的结果。',
      network_unavailable: '连接未正常完成，无法确认上游是否处理了请求。',
      login_identity_invalid: '手机响应缺少有效的原生身份或登录凭据，未保存会话。',
      login_account_mismatch: '手机响应的可搜索账号与本次填写账号不一致，未保存会话。',
      login_phone_invalid: '手机响应的手机号资料格式无法核验，未保存会话。',
      login_phone_mismatch: '手机响应的完整手机号与本次填写手机号不一致，未保存会话。',
      login_security_flags_invalid: '手机响应的安全验证状态格式无效，未保存会话。',
      provider_code_classified: '原因依据：已核对的业务错误码定义。',
      provider_reason_classified: '原因依据：上游明确原因文案的安全分类，未展示原文。',
      provider_reason_empty: '上游原因文本为空，不能据此判断号码或号段是否受限。',
      provider_reason_unrecognized:
        '上游返回了原因文本，但尚未匹配到安全分类；原文已隐藏，不能据此判断号段限制。',
      provider_reason_nontext: '上游原因不是文本，已保留业务码；内容未展示，不能据此判断号段限制。',
      provider_reason_oversized:
        '上游原因超过安全分类长度，已保留业务码；内容未展示，不能据此判断号段限制。'
    }
    const stages: Record<string, string> = {
      password_login: '失败阶段：密码登录',
      device_verification: '失败阶段：换设备验证',
      login_sms: '失败阶段：登录短信',
      sms_login: '失败阶段：短信登录',
      security_verification: '失败阶段：安全码验证',
      business_credentials: '失败阶段：读取业务搜索凭据',
      im_credentials: mobileSearch.value
        ? '失败阶段：保存业务搜索会话'
        : '失败阶段：取得消息连接凭据（前面的身份验证已完成）',
      session_save: '失败阶段：保存本地会话（上游已返回登录凭据）',
      signup_sms: '失败阶段：注册短信',
      signup_submit: '失败阶段：原生注册',
      signup_record: '失败阶段：保存注册结果'
    }
    return [
      stages[flow.value.failure_stage],
      details[flow.value.diagnostic],
      flow.value.provider_code ? `上游返回码：${flow.value.provider_code}` : '',
      flow.value.diagnostic_version ? `诊断版本：${flow.value.diagnostic_version}` : '',
      flow.value.failure_stage === 'signup_sms' && !flow.value.diagnostic
        ? '此条结果未包含原因分类，无法判断上游原因是否为空；不会自动重发。'
        : ''
    ]
      .filter(Boolean)
      .join('；')
  })
  const statusLabel = computed(() =>
    initializing.value
      ? '正在准备验证'
      : expired.value && !terminal.value
        ? '已过期'
        : mobileSearch.value && flow.value.status === 'authenticated'
          ? '业务搜索会话已保存'
          : mobileSearch.value && flow.value.status === 'connection_pending'
            ? '业务搜索会话尚未保存'
            : signup.value && flow.value.status === 'pending'
              ? '等待注册'
              : signup.value && flow.value.status === 'failed'
                ? '注册未完成'
                : statuses[flow.value.status] || '未开始'
  )
  const submitLabel = computed(() => {
    if (signup.value)
      return flow.value.status === 'signup_sms_required' ? '验证并注册新账号' : '验证并发送注册短信'
    const labels: Record<string, string> = {
      pending: method.value === 'sms' ? '验证并发送短信' : '验证并登录',
      failed: method.value === 'sms' ? '验证并发送短信' : '验证并登录',
      sms_required: '验证短信并登录',
      security_required: '提交安全码',
      connection_pending: mobileSearch.value ? '重试保存搜索会话' : '重试取得 IM 凭据'
    }
    return labels[flow.value.status] || '提交'
  })
  function clearInput() {
    password.value = ''
    code.value = ''
    nativeAccount.value = ''
    agreed.value = false
    verification?.abort()
    verification = undefined
    busy.value = false
  }
  function endpoint() {
    return `${authPath(currentAccount.value?.id)}/${flow.value.transaction_id}`
  }
  async function initialize(current: number, target = purpose.value, restore = true) {
    if (!props.owner) return false
    if (mobileSearch.value && (!mobileAccountAllowed.value || target !== 'login')) {
      error.value = errorText('native_mobile_account_unavailable')
      return false
    }
    const accountID = currentAccount.value?.id
    initializing.value = true
    error.value = ''
    try {
      const [config, accounts] = await Promise.all([
        api<Row>(deploymentPath()),
        api<Row[]>('/accounts')
      ])
      if (current !== generation) return false
      deployment.value = config
      const originalScope = scope()
      if (!accountID) {
        panel.value = {}
        flow.value = { status: 'pending' }
        return true
      }
      const account = accounts.find((a) => a.id === accountID)
      if (!account) throw new Error('账号不存在')
      if (
        mobileSearch.value &&
        (account.platform !== 'wsl' ||
          account.purpose !== 'management' ||
          account.auth_state !== 'authenticated')
      )
        throw new Error(errorText('native_mobile_account_unavailable'))
      panel.value = account
      if (!config.configured || !config.crypto_ready) return true
      const id = restore ? handles.get(originalScope) : undefined
      if (id) {
        try {
          const value = await api<Row>(`${authPath(accountID)}/${id}`)
          if (current !== generation) return false
          if (mobileSearch.value && value.purpose === 'signup') {
            handles.delete(originalScope)
            return initialize(current, 'login', false)
          }
          purpose.value = value.purpose === 'signup' ? 'signup' : 'login'
          flow.value = value
          if (purpose.value === 'login')
            nativeAccount.value = account.registered_account || nativeAccount.value
          return true
        } catch (e) {
          if (!(e instanceof ApiError) || ![404, 410].includes(e.status)) throw e
          if (current !== generation) return false
          handles.delete(originalScope)
        }
      }
      const value = await api<Row>(
        target === 'signup' ? '/native-signup' : authPath(account.id),
        'POST',
        {
          version: account.version,
          ...(target === 'signup' ? { account_id: account.id } : {})
        }
      )
      if (current === generation) {
        handles.set(originalScope, value.transaction_id)
        flow.value = value
        if (target === 'login')
          nativeAccount.value = account.registered_account || nativeAccount.value
        return true
      }
    } catch (e) {
      if (current === generation) error.value = (e as Error).message
    } finally {
      if (current === generation) initializing.value = false
    }
    return false
  }
  async function prepareSwitch(name: string | number) {
    if (mobileSearch.value && name !== 'login') return false
    if (name === purpose.value) return true
    if (locked.value || (name === 'signup' && !canSignup.value)) return false
    if (flow.value.status === 'unknown') {
      error.value = errorText('native_auth_review_required')
      return false
    }
    if (name !== 'login' && name !== 'signup') return false
    const current = ++generation
    clearInput()
    // The server atomically replaces our idle transaction; other owners remain locked out.
    return initialize(current, name, false)
  }
  async function loginRegistered() {
    if (await prepareSwitch('login')) purpose.value = 'login'
  }
  watch(
    () => [
      props.modelValue,
      props.account?.id,
      props.owner,
      props.workspace,
      props.adapter,
      mobileSearch.value ? props.account?.version : undefined
    ],
    () => {
      const current = ++generation
      clearInput()
      createdAccount.value = null
      robotName.value = ''
      initializing.value = false
      flow.value = {}
      deployment.value = {}
      panel.value = props.account || {}
      purpose.value = 'login'
      clearInterval(timer)
      timer = undefined
      now.value = Date.now()
      if (props.modelValue) {
        timer = setInterval(() => {
          if (!verification) now.value = Date.now()
          void poll()
        }, 1000)
      }
      if (props.modelValue) void initialize(current, 'login')
    }
  )
  async function poll() {
    if (
      polling ||
      !props.modelValue ||
      !flow.value.transaction_id ||
      flow.value.status !== 'working'
    )
      return
    polling = true
    const current = generation
    try {
      const value = await api<Row>(endpoint())
      if (current !== generation) return
      error.value = ''
      flow.value = value
      if (['authenticated', 'registered'].includes(value.status)) emit('updated')
    } catch (e) {
      if (current === generation) error.value = (e as Error).message
    } finally {
      polling = false
    }
  }
  onUnmounted(() => {
    generation++
    clearInterval(timer)
    clearInput()
    handles.clear()
  })
  function forgetAccount(id: string) {
    for (const key of handles.keys()) if (key.endsWith(`:${id}`)) handles.delete(key)
    if (currentAccount.value?.id !== id) return
    generation++
    clearInput()
    flow.value = {}
    panel.value = {}
    createdAccount.value = null
    robotName.value = ''
    error.value = ''
    emit('update:modelValue', false)
  }
  defineExpose({ forgetAccount })

  async function submit() {
    if (signup.value) {
      await send(flow.value.status === 'signup_sms_required' ? 'signup_submit' : 'signup_sms')
      return
    }
    const action =
      flow.value.status === 'sms_required'
        ? 'sms_verify'
        : flow.value.status === 'security_required'
          ? 'security_verify'
          : flow.value.status === 'connection_pending'
            ? 'retry_finish'
            : method.value === 'sms'
              ? 'sms_request'
              : 'password'
    await send(action)
  }
  async function send(action: string) {
    if (locked.value) return
    if (
      mobileSearch.value &&
      (!mobileAccountAllowed.value ||
        !flow.value.transaction_id ||
        ![
          'password',
          'sms_request',
          'sms_resend',
          'sms_verify',
          'security_verify',
          'retry_finish'
        ].includes(action))
    ) {
      error.value = errorText('native_mobile_account_unavailable')
      return
    }
    if (!currentAccount.value && !robotName.value.trim()) {
      error.value = '请填写机器人名称'
      return
    }
    if (signup.value) {
      if (!agreed.value) {
        error.value = '请确认旺商聊协议与手机号授权'
        return
      }
      if (action === 'signup_sms' && !/^1[0-9]{10}$/.test(nativeAccount.value)) {
        error.value = '请输入 11 位手机号'
        return
      }
      if (
        action === 'signup_submit' &&
        (!/^[0-9]{6}$/.test(code.value) ||
          !password.value.trim() ||
          ![...password.value].every((character) => signupPasswordCharacters.has(character)) ||
          !/^(?![a-zA-Z]+$)(?!\d+$)(?![^\da-zA-Z\s]+$).{6,16}$/.test(password.value))
      ) {
        error.value = '请填写六位短信验证码及符合要求的新密码'
        return
      }
    }
    if (
      ['password', 'sms_request'].includes(action) &&
      (!nativeAccount.value || (action === 'password' && !password.value))
    ) {
      error.value = '请填写原生账号和对应登录信息'
      return
    }
    if (['sms_verify', 'security_verify'].includes(action) && !/^[0-9]{6}$/.test(code.value)) {
      error.value = '请输入六位数字'
      return
    }
    const current = generation
    const input: Row = {
      action,
      account: nativeAccount.value,
      password: action === 'password' || action === 'signup_submit' ? password.value : '',
      code: code.value,
      ...(signup.value ? { agreed: agreed.value } : {})
    }
    busy.value = true
    error.value = ''
    try {
      if (!currentAccount.value) {
        const account = await api<Row>('/accounts', 'POST', { name: robotName.value.trim() })
        if (current !== generation) {
          input.password = ''
          input.code = ''
          return
        }
        createdAccount.value = {
          ...account,
          name: robotName.value.trim(),
          version: 1,
          workspace: props.workspace,
          auth_state: 'not_authenticated'
        }
        emit('created', createdAccount.value)
        if (!(await initialize(current, purpose.value, false))) {
          input.password = ''
          input.code = ''
          busy.value = false
          return
        }
      }
    } catch (e) {
      if (current === generation) {
        error.value = (e as Error).message
        busy.value = false
      }
      input.password = ''
      input.code = ''
      return
    }
    if (current !== generation) {
      input.password = ''
      input.code = ''
      return
    }
    const originalEndpoint = endpoint()
    let consumed = false
    const post = async (validate = '') => {
      if (current !== generation || consumed) return
      consumed = true
      error.value = ''
      try {
        const value = await api<Row>(originalEndpoint, 'POST', { ...input, captcha: validate })
        if (current === generation) flow.value = value
      } catch (e) {
        if (current === generation) error.value = (e as Error).message
      } finally {
        input.password = ''
        input.code = ''
        if (current === generation) {
          password.value = ''
          code.value = ''
          busy.value = false
        }
      }
    }
    if (action === 'security_verify' || action === 'retry_finish') {
      await post()
      return
    }
    try {
      await nextTick()
      if (current !== generation || !captchaHost.value) return
      const controller = new AbortController()
      verification = controller
      const validate = await nativeCaptcha(
        captchaHost.value,
        flow.value.captcha_id,
        controller.signal,
        () => {
          if (current === generation) error.value = '人机验证未通过'
        }
      )
      if (verification === controller) verification = undefined
      now.value = Date.now()
      await post(validate)
    } catch (e) {
      consumed = true
      input.password = ''
      input.code = ''
      if (current === generation) {
        verification = undefined
        now.value = Date.now()
        error.value = (e as Error).message
        busy.value = false
      }
    }
  }
  async function cancel() {
    if (locked.value || !flow.value.transaction_id) return false
    const current = generation
    busy.value = true
    try {
      const value = await api<Row>(endpoint(), 'DELETE')
      if (current === generation) {
        handles.delete(scope())
        flow.value = value
        clearInput()
        return true
      }
    } catch (e) {
      if (current === generation) error.value = (e as Error).message
    } finally {
      if (current === generation) busy.value = false
    }
    return false
  }
  async function restart() {
    if (locked.value) return
    if (flow.value.transaction_id && !(await cancel())) return
    await initialize(++generation, purpose.value, false)
  }
</script>

<style scoped>
  .native-captcha-layer {
    position: fixed;
    inset: 0;
    z-index: 10000;
    pointer-events: none;
  }
  .native-captcha-layer :deep(.yidun_popup) {
    pointer-events: auto;
  }
  .native-captcha-layer :deep(.yidun_modal) {
    max-width: calc(100vw - 32px);
    max-height: calc(100vh - 32px);
    max-height: calc(100dvh - 32px);
    overflow: auto;
  }
  .flow-alert {
    margin-bottom: 16px;
  }
  .auth-status {
    display: flex;
    gap: 14px;
    flex-wrap: wrap;
    align-items: center;
    margin: 18px 0;
  }
  .auth-status > span:last-child {
    font-size: 12px;
    color: #838a98;
  }
  .auth-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 10px;
    margin-top: 18px;
  }
  .signup-consent {
    white-space: normal;
    height: auto;
    align-items: flex-start;
  }
  .signup-consent :deep(.el-checkbox__label) {
    white-space: normal;
    line-height: 1.6;
  }
  .password-policy {
    margin: -8px 0 16px;
    color: #606266;
    font-size: 12px;
  }
</style>
