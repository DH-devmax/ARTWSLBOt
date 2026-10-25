<template>
  <ElDialog
    :model-value="modelValue"
    :title="
      currentAccount
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
    @close="clearInput"
  >
    <ElTabs v-model="purpose" :before-leave="prepareSwitch" class="native-auth-tabs">
      <ElTabPane label="原生登录" name="login" :disabled="locked" />
      <ElTabPane label="新账号注册" name="signup" :disabled="locked || !canSignup" />
    </ElTabs>
    <p v-if="!canSignup && !signup" class="password-policy">
      已绑定身份、已注册或开户结果待复核的账号面板不能再次注册。
    </p>
    <ElAlert
      v-if="error || flow.error"
      :title="error || errorText(flow.error)"
      type="error"
      :closable="false"
      class="flow-alert"
    />
    <ElAlert
      v-if="!deployment.configured"
      title="尚未导入原生协议配置"
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
    <ElDescriptions v-if="flow.status === 'authenticated'" :column="1" border>
      <ElDescriptionsItem label="原生 UID">{{ flow.native_uid }}</ElDescriptionsItem>
      <ElDescriptionsItem label="昵称">{{ flow.nickname || '—' }}</ElDescriptionsItem>
      <ElDescriptionsItem label="消息接收">尚未连接</ElDescriptionsItem>
    </ElDescriptions>
    <ElDescriptions v-else-if="flow.status === 'registered'" :column="1" border>
      <ElDescriptionsItem label="旺商聊账号">{{ flow.registered_account }}</ElDescriptionsItem>
      <ElDescriptionsItem label="登录状态">尚未登录</ElDescriptionsItem>
    </ElDescriptions>
    <ElForm v-else-if="ready" label-position="top" @submit.prevent="submit">
      <ElFormItem v-if="!currentAccount" label="机器人名称">
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
        <ElFormItem label="昵称">
          <ElInput v-model="nickname" autocomplete="off" maxlength="20" :disabled="busy" />
        </ElFormItem>
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
        v-if="flow.status === 'registered'"
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

  const props = defineProps<{
    modelValue: boolean
    account: Row | null
    owner: string
    workspace: 'wsl' | 'maibot'
  }>()
  const emit = defineEmits<{ 'update:modelValue': [boolean]; updated: []; created: [Row] }>()
  const api = <T,>(path: string, method = 'GET', body?: unknown) =>
    request<T>(path, method, body, undefined, props.workspace)
  const createdAccount = ref<Row | null>(null)
  const currentAccount = computed(() => props.account || createdAccount.value)
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
    nickname = ref(''),
    agreed = ref(false),
    code = ref(''),
    error = ref(''),
    busy = ref(false),
    now = ref(Date.now())
  const captchaHost = ref<HTMLElement>()
  let generation = 0,
    polling = false,
    captcha: any
  let scriptPromise: Promise<void> | undefined
  const signup = computed(() => purpose.value === 'signup')
  const locked = computed(() => initializing.value || busy.value || flow.value.status === 'working')
  const canSignup = computed(
    () =>
      !panel.value.native_uid &&
      !panel.value.connection_enabled &&
      !['submitted', 'registered'].includes(panel.value.signup_state) &&
      (!currentAccount.value || panel.value.auth_state === 'not_authenticated') &&
      !['authenticated', 'registered'].includes(flow.value.status)
  )
  const terminal = computed(() => ['authenticated', 'registered'].includes(flow.value.status))
  const signupPasswordCharacters = new Set(
    "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ×÷`¬!£$%^*()~=#{}[];':,./?-_+<>@&"
  )
  const scope = () => `${props.owner}:${props.workspace}:${currentAccount.value?.id || 'new'}`
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
  const statusLabel = computed(() =>
    initializing.value
      ? '正在准备验证'
      : expired.value && !terminal.value
        ? '已过期'
        : signup.value && flow.value.status === 'pending'
          ? '等待注册'
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
      connection_pending: '重试取得 IM 凭据'
    }
    return labels[flow.value.status] || '提交'
  })
  function clearInput() {
    password.value = ''
    code.value = ''
    nativeAccount.value = ''
    nickname.value = ''
    agreed.value = false
    captcha?.destroy?.()
    captcha = undefined
    busy.value = false
  }
  function endpoint() {
    return `/accounts/${currentAccount.value?.id}/native-auth/${flow.value.transaction_id}`
  }
  async function initialize(current: number, target = purpose.value, restore = true) {
    if (!props.owner) return false
    const originalScope = scope(),
      accountID = currentAccount.value?.id
    initializing.value = true
    error.value = ''
    try {
      const [config, accounts] = await Promise.all([
        api<Row>('/native-deployment'),
        api<Row[]>('/accounts')
      ])
      if (current !== generation) return false
      deployment.value = config
      if (!accountID) {
        panel.value = {}
        flow.value = { status: 'pending' }
        return true
      }
      const account = accounts.find((a) => a.id === accountID)
      if (!account) throw new Error('账号不存在')
      panel.value = account
      if (!config.configured || !config.crypto_ready) return true
      const id = restore ? handles.get(originalScope) : undefined
      if (id) {
        try {
          const value = await api<Row>(`/accounts/${accountID}/native-auth/${id}`)
          if (current !== generation) return false
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
        target === 'signup' ? '/native-signup' : `/accounts/${account.id}/native-auth`,
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
    () => [props.modelValue, props.account?.id, props.owner, props.workspace],
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
      flow.value = value
      if (['authenticated', 'registered'].includes(value.status)) emit('updated')
    } catch (e) {
      if (current === generation) error.value = (e as Error).message
    } finally {
      polling = false
    }
  }
  const timer = setInterval(() => {
    now.value = Date.now()
    void poll()
  }, 1000)
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
    if (!currentAccount.value && !robotName.value.trim()) {
      error.value = '请填写机器人名称'
      return
    }
    if (signup.value) {
      if (!agreed.value || !nickname.value.trim()) {
        error.value = '请填写昵称并确认旺商聊协议与手机号授权'
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
      ...(signup.value ? { nickname: nickname.value, agreed: agreed.value } : {})
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
      const browser = window as any
      if (!browser.initNECaptcha) {
        scriptPromise ??= new Promise<void>((resolve, reject) => {
          const script = document.createElement('script')
          script.src = 'https://cstaticdun.126.net/load.min.js'
          script.async = true
          const timeout = setTimeout(() => {
            script.remove()
            reject(new Error('人机验证加载超时'))
          }, 15000)
          script.onload = () => {
            clearTimeout(timeout)
            resolve()
          }
          script.onerror = () => {
            clearTimeout(timeout)
            script.remove()
            reject(new Error('人机验证加载失败'))
          }
          document.head.append(script)
        }).catch((e) => {
          scriptPromise = undefined
          throw e
        })
        await scriptPromise
      }
      if (current !== generation) return
      captcha?.destroy?.()
      await nextTick()
      browser.initNECaptcha(
        {
          captchaId: flow.value.captcha_id,
          // SDK v2 uses element as the popup container, not just a form anchor.
          element: captchaHost.value,
          mode: 'popup',
          width: `${Math.max(1, Math.min(320, (window.visualViewport?.width || window.innerWidth) - 64))}px`,
          apiVersion: 2,
          onClose: () => {
            if (!consumed) {
              consumed = true
              if (current === generation) busy.value = false
              input.password = ''
              input.code = ''
            }
          },
          onVerify: (failure: unknown, data: { validate: string }) => {
            if (current !== generation || consumed) return
            if (failure || !data?.validate) {
              error.value = '人机验证未通过'
              return
            }
            void post(data.validate)
          }
        },
        (instance: any) => {
          if (current !== generation) {
            instance.destroy?.()
            return
          }
          captcha = instance
          instance.verify()
        },
        () => {
          consumed = true
          input.password = ''
          input.code = ''
          if (current === generation) {
            error.value = '人机验证初始化失败'
            busy.value = false
          }
        }
      )
    } catch (e) {
      consumed = true
      input.password = ''
      input.code = ''
      if (current === generation) {
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
