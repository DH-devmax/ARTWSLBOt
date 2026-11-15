<template>
  <ElDialog
    :model-value="modelValue"
    :title="`${current?.name || (workspace === 'maibot' ? '添加热群机器人' : '添加群管机器人')} · 辣椒聊登录`"
    width="520px"
    class="platform-config-dialog"
    align-center
    append-to-body
    :close-on-click-modal="false"
    :close-on-press-escape="!busy"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <ElTabs v-model="purpose" :before-leave="switchPurpose">
      <ElTabPane label="原生登录" name="login" :disabled="busy || verifying" />
      <ElTabPane
        label="短信注册 / 登录"
        name="signup"
        :disabled="busy || verifying || !!current?.native_uid"
      />
    </ElTabs>
    <ElAlert
      v-if="purpose === 'signup'"
      title="未注册手机号将创建账号，已注册手机号将登录。提交后保存会话，消息接收需另行开启。"
      type="info"
      :closable="false"
    />
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElButton v-if="reviewRequired" :disabled="busy" type="warning" @click="review"
      >核对后重置本站会话</ElButton
    >
    <ElAlert
      v-if="success"
      title="会话已加密保存；消息接收尚未开启"
      type="success"
      :closable="false"
    />
    <ElForm
      v-else
      label-position="top"
      @submit.prevent="purpose === 'signup' ? finishSignup() : prepare()"
    >
      <ElFormItem v-if="!current" label="机器人名称">
        <ElInput v-model="name" maxlength="100" :disabled="busy" />
      </ElFormItem>
      <ElFormItem label="辣椒聊手机号">
        <ElInput
          v-model="phone"
          inputmode="tel"
          maxlength="11"
          autocomplete="off"
          :disabled="busy || verifying"
        />
      </ElFormItem>
      <ElFormItem v-if="purpose === 'login'" label="原生账号密码">
        <ElInput
          v-model="password"
          type="password"
          show-password
          maxlength="256"
          autocomplete="off"
          :disabled="busy || verifying"
        />
      </ElFormItem>
      <template v-else>
        <ElAlert
          v-if="smsReady"
          title="已发送短信。验证码绑定发送时的手机号，修改输入不会更换本次目标；更换手机号请重新发送。"
          type="info"
          :closable="false"
        />
        <ElCheckbox v-model="agreed" :disabled="busy || verifying">
          同意辣椒聊协议与隐私政策，并授权使用此手机号注册或登录
        </ElCheckbox>
        <p class="agreement-links">
          <a
            href="https://www.lajiaoliao.com/legal/terms/"
            target="_blank"
            rel="noopener noreferrer"
            >用户协议</a
          >
          <a
            href="https://www.lajiaoliao.com/legal/privacy/"
            target="_blank"
            rel="noopener noreferrer"
            >隐私政策</a
          >
        </p>
        <ElFormItem label="短信验证码">
          <ElInput
            v-model="code"
            inputmode="numeric"
            maxlength="6"
            autocomplete="one-time-code"
            :disabled="busy || verifying"
          >
            <template #append>
              <ElButton
                :disabled="busy || verifying || reviewRequired || !agreed || resendSeconds > 0"
                @click="prepare"
              >
                {{ resendSeconds > 0 ? `${resendSeconds} 秒后重发` : '发送验证码' }}
              </ElButton>
            </template>
          </ElInput>
        </ElFormItem>
      </template>
      <ElButton
        native-type="submit"
        type="primary"
        :icon="Key"
        :loading="busy"
        :disabled="
          verifying ||
          reviewRequired ||
          (purpose === 'signup' && (!agreed || !smsReady || !/^\d{6}$/.test(code)))
        "
        @click.prevent="purpose === 'signup' ? finishSignup() : prepare()"
      >
        {{ purpose === 'signup' ? '确认注册 / 登录' : '验证并登录' }}
      </ElButton>
    </ElForm>
    <template #footer
      ><ElButton :disabled="busy" @click="emit('update:modelValue', false)"
        >关闭</ElButton
      ></template
    >
  </ElDialog>
  <ElDialog
    :model-value="verifying"
    title="官方安全验证"
    width="440px"
    class="platform-config-dialog"
    align-center
    append-to-body
    :close-on-click-modal="false"
    @update:model-value="
      (value) => {
        if (!value && verifying) cancelCaptcha()
      }
    "
  >
    <iframe
      v-if="modelValue"
      ref="captcha"
      src="/lajiaoliao-captcha.html"
      sandbox="allow-scripts"
      title="辣椒聊官方人机验证"
      class="captcha-frame"
      @load="startCaptcha"
    />
    <template #footer><ElButton @click="cancelCaptcha">取消验证</ElButton></template>
  </ElDialog>
</template>
<script setup lang="ts">
  import { computed, onUnmounted, ref, watch } from 'vue'
  import {
    ElAlert,
    ElButton,
    ElDialog,
    ElForm,
    ElFormItem,
    ElInput,
    ElMessageBox,
    ElTabs,
    ElTabPane,
    ElCheckbox
  } from 'element-plus'
  import { Key } from '@element-plus/icons-vue'
  import { api, type Row } from './api'
  import type { Workspace } from './workspaces'
  const props = defineProps<{
    modelValue: boolean
    account: Row | null
    owner: string
    workspace: Workspace
  }>()
  const emit = defineEmits<{ 'update:modelValue': [boolean]; updated: []; created: [Row] }>()
  const created = ref<Row | null>(null),
    current = computed(() => props.account || created.value)
  const name = ref(''),
    phone = ref(''),
    password = ref(''),
    error = ref('')
  const busy = ref(false),
    verifying = ref(false),
    success = ref(false),
    requestID = ref('')
  const reviewRequired = ref(false)
  const purpose = ref<'login' | 'signup'>('login')
  const agreed = ref(false),
    code = ref(''),
    smsReady = ref(false)
  const resendAt = ref(0),
    now = ref(Date.now())
  const resendSeconds = computed(() => Math.max(0, Math.ceil((resendAt.value - now.value) / 1000)))
  let clock: ReturnType<typeof setInterval> | undefined
  const captcha = ref<HTMLIFrameElement>()
  let generation = 0,
    challenge: Row = {},
    verificationTimer: ReturnType<typeof setTimeout> | undefined
  const errors: Record<string, string> = {
    lajiaoliao_captcha_required: '请完成官方人机验证',
    lajiaoliao_login_review_required:
      '上次登录结果待核对，请先核对官方客户端，再清除会话后重新登录',
    lajiaoliao_result_unknown: '上游请求结果未知，未自动重试，请核对官方客户端状态',
    lajiaoliao_device_confirmation_required: '官方要求确认同类设备切换，当前未自动抢占登录',
    lajiaoliao_request_rejected: '官方拒绝登录，请核对账号和验证结果',
    native_identity_already_connected: '此原生身份已在另一个账号池接入',
    native_login_in_progress: '登录仍在进行中，请等待本次结果后再核对',
    version_conflict: '账号状态已变化，请关闭后重新打开',
    lajiaoliao_sms_cooldown: '短信发送冷却中，请稍后再试',
    lajiaoliao_sms_flow_expired: '短信流程已过期或不属于当前网站会话，请重新获取验证码',
    lajiaoliao_signup_existing_identity: '此面板已有原生身份，请使用原生登录',
    lajiaoliao_signup_unavailable: '短信注册尚不可用'
  }
  function showFailure(e: unknown) {
    const code = (e as Error).message
    reviewRequired.value = [
      'lajiaoliao_login_review_required',
      'lajiaoliao_result_unknown',
      'lajiaoliao_device_confirmation_required'
    ].includes(code)
    error.value = errors[code] || code
  }
  function clear() {
    stopCaptcha()
    generation++
    busy.value = false
    verifying.value = false
    phone.value = ''
    password.value = ''
    code.value = ''
    agreed.value = false
    smsReady.value = false
    resendAt.value = 0
    clearInterval(clock)
    clock = undefined
    challenge = {}
    clearTimeout(verificationTimer)
  }
  watch(
    () => [props.modelValue, props.account?.id, props.workspace, props.owner],
    () => {
      clear()
      if (props.modelValue) {
        created.value = null
        name.value = ''
        error.value = ''
        success.value = false
        reviewRequired.value = false
        purpose.value = 'login'
        now.value = Date.now()
        clock = setInterval(() => {
          if (!verifying.value && resendAt.value > now.value) now.value = Date.now()
        }, 1000)
        if (current.value) void restoreSMS(generation)
      }
    },
    { immediate: true }
  )
  async function restoreSMS(gen: number) {
    try {
      const result = await api<Row>(
        `/accounts/${current.value!.id}/lajiaoliao-signup`,
        'GET',
        undefined,
        undefined,
        props.workspace
      )
      if (gen !== generation) return
      smsReady.value = result.sms_ready === true
      resendAt.value = Date.parse(result.resend_at || '') || 0
      if (result.review_required) {
        reviewRequired.value = true
        error.value = errors.lajiaoliao_login_review_required
      }
    } catch (e) {
      if (gen === generation) showFailure(e)
    }
  }
  function cancelCaptcha() {
    stopCaptcha()
    verifying.value = false
    password.value = ''
    clearTimeout(verificationTimer)
  }
  function stopCaptcha() {
    captcha.value?.contentWindow?.postMessage(
      { source: 'artwsl-lajiao-login', id: requestID.value, action: 'cancel' },
      '*'
    )
  }
  function switchPurpose() {
    if (busy.value || verifying.value || reviewRequired.value) return false
    password.value = ''
    code.value = ''
    error.value = ''
    agreed.value = false
    return true
  }
  function startCaptcha() {
    if (!verifying.value) return
    captcha.value?.contentWindow?.postMessage(
      { source: 'artwsl-lajiao-login', id: requestID.value, challenge },
      '*'
    )
  }
  async function prepare() {
    if (
      busy.value ||
      reviewRequired.value ||
      !/^1\d{10}$/.test(phone.value) ||
      (purpose.value === 'login' ? !password.value : !agreed.value || resendSeconds.value > 0) ||
      (!current.value && !name.value.trim())
    ) {
      error.value = '请填写机器人名称、11位手机号和密码'
      return
    }
    const gen = generation,
      workspace = props.workspace
    busy.value = true
    error.value = ''
    try {
      if (!current.value) {
        const result = await api<Row>(
          '/accounts',
          'POST',
          { name: name.value.trim(), platform: 'lajiaoliao' },
          undefined,
          workspace
        )
        if (gen !== generation) return
        created.value = {
          id: result.id,
          name: name.value.trim(),
          version: 1,
          platform: 'lajiaoliao'
        }
        emit('created', created.value)
      }
      const response = await api<Row>(
        `/accounts/${current.value!.id}/lajiaoliao-challenge`,
        'POST',
        {},
        undefined,
        workspace
      )
      if (gen !== generation) return
      challenge = response.challenge
      if (challenge.required) {
        requestID.value = crypto.randomUUID()
        verifying.value = true
        queueMicrotask(startCaptcha)
        verificationTimer = setTimeout(() => {
          cancelCaptcha()
          error.value = '人机验证已超时，请重新发起'
        }, 120000)
      } else await submit('', gen)
    } catch (e) {
      if (gen === generation) showFailure(e)
    } finally {
      if (gen === generation) busy.value = false
    }
  }
  async function submit(proof: string, gen: number) {
    if (gen !== generation || !props.modelValue || !current.value) return
    busy.value = true
    try {
      const registering = purpose.value === 'signup'
      const result = await api<Row>(
        `/accounts/${current.value.id}/${registering ? 'lajiaoliao-signup-sms' : 'lajiaoliao-login'}`,
        'POST',
        {
          version: current.value.version,
          phone: phone.value,
          ...(registering ? { agreed: agreed.value } : { password: password.value }),
          captcha_verify_param: proof
        },
        undefined,
        props.workspace
      )
      if (gen !== generation) return
      if (registering) {
        smsReady.value = true
        resendAt.value = Date.parse(result.resend_at)
        now.value = Date.now()
      } else {
        success.value = true
        emit('updated')
      }
    } catch (e) {
      if (gen === generation) showFailure(e)
    } finally {
      if (gen === generation) {
        busy.value = false
        password.value = ''
      }
    }
  }
  async function finishSignup() {
    if (
      busy.value ||
      reviewRequired.value ||
      verifying.value ||
      !current.value ||
      !smsReady.value ||
      !agreed.value ||
      !/^\d{6}$/.test(code.value)
    )
      return
    const gen = generation
    busy.value = true
    error.value = ''
    try {
      await api(
        `/accounts/${current.value.id}/lajiaoliao-signup`,
        'POST',
        { version: current.value.version, code: code.value, agreed: agreed.value },
        undefined,
        props.workspace
      )
      if (gen !== generation) return
      success.value = true
      emit('updated')
    } catch (e) {
      if (gen === generation) showFailure(e)
    } finally {
      if (gen === generation) {
        busy.value = false
        code.value = ''
      }
    }
  }
  async function review() {
    if (busy.value || !current.value) return
    const gen = generation,
      account = { ...current.value },
      workspace = props.workspace
    busy.value = true
    try {
      await ElMessageBox.confirm(
        '确认已核对官方客户端及其他管理端状态？只清除本站会话和待复核标记，不自动退出官方客户端，也不会重新登录。',
        '人工核对登录结果',
        { type: 'warning', confirmButtonText: '已核对，重置本站会话', cancelButtonText: '取消' }
      )
      if (gen !== generation) return
      await api(
        `/accounts/${account.id}/forget-session`,
        'POST',
        { version: account.version },
        undefined,
        workspace
      )
      if (gen !== generation) return
      emit('updated')
      emit('update:modelValue', false)
    } catch (e) {
      if (gen === generation && e instanceof Error) showFailure(e)
    } finally {
      if (gen === generation) {
        busy.value = false
        password.value = ''
      }
    }
  }
  function receive(event: MessageEvent) {
    const data = event.data
    if (
      !verifying.value ||
      event.source !== captcha.value?.contentWindow ||
      event.origin !== 'null' ||
      data?.source !== 'artwsl-lajiao-captcha' ||
      (data.type !== 'ready' && data.id !== requestID.value)
    )
      return
    if (data.type === 'ready') startCaptcha()
    else if (
      data.type === 'success' &&
      typeof data.value === 'string' &&
      data.value.length > 0 &&
      data.value.length <= 32768
    ) {
      verifying.value = false
      now.value = Date.now()
      clearTimeout(verificationTimer)
      void submit(data.value, generation)
    } else if (data.type === 'failed' || data.type === 'cancelled') {
      cancelCaptcha()
      error.value = '官方安全验证未完成，请重新发起'
    }
  }
  window.addEventListener('message', receive)
  onUnmounted(() => {
    clear()
    window.removeEventListener('message', receive)
  })
</script>
<style scoped>
  .el-alert {
    margin-bottom: 16px;
  }
  .captcha-frame {
    width: 100%;
    height: 420px;
    max-height: calc(100dvh - 200px);
    border: 0;
  }
  .agreement-links {
    display: flex;
    gap: 16px;
    margin: 8px 0 16px;
  }
  .el-checkbox {
    white-space: normal;
    height: auto;
  }
  .el-checkbox :deep(.el-checkbox__label) {
    white-space: normal;
    line-height: 1.5;
  }
</style>
