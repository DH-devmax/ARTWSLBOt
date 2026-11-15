<template>
  <ElDialog
    :model-value="visible"
    title="安全验证"
    width="380px"
    class="web-captcha-dialog"
    append-to-body
    align-center
    :close-on-click-modal="false"
    :before-close="cancel"
    @closed="restoreFocus"
  >
    <section class="web-captcha" aria-label="图像拼图验证" :aria-busy="loading || checking">
      <p class="captcha-hint">拖动滑块，完成拼图</p>
      <div
        class="captcha-image"
        :style="{
          aspectRatio: `${challenge?.image.width || 300} / ${challenge?.image.height || 220}`
        }"
      >
        <template v-if="challenge && !expired">
          <img
            :key="`${challenge.challenge_id}:background`"
            :src="challenge.image.background"
            alt="拼图背景"
            draggable="false"
            @load="backgroundLoaded = true"
            @error="imageError"
          />
          <img
            :key="`${challenge.challenge_id}:tile`"
            class="captcha-tile"
            :src="challenge.image.tile"
            alt="待拼合的拼图"
            draggable="false"
            :style="tileStyle"
            @load="tileLoaded = true"
            @error="imageError"
          />
        </template>
        <span v-else role="status">{{
          loading ? '正在加载拼图…' : expired ? '拼图已到期' : '拼图暂时不可用'
        }}</span>
      </div>
      <ElSlider
        v-model="position"
        :min="0"
        :max="challenge ? challenge.image.width - challenge.image.tile_width : 300"
        :step="1"
        :show-tooltip="false"
        :disabled="!canVerify"
        aria-label="拼图位置"
        @change="positionChanged"
        @keydown.capture="sliderKeydown"
        @pointerdown.capture="keyboard = false"
        @touchstart.capture="keyboard = false"
      />
      <div class="captcha-status">
        <span role="status" aria-live="polite">{{ status }}</span>
        <ElTooltip content="刷新拼图">
          <ElButton
            text
            :icon="Refresh"
            :disabled="loading || checking"
            aria-label="刷新拼图"
            @click="refresh"
          />
        </ElTooltip>
      </div>
      <ElAlert v-if="error" :title="error" type="error" :closable="false" role="alert" />
    </section>
    <template #footer>
      <div class="captcha-actions">
        <ElButton @click="cancel">取消</ElButton>
        <ElButton type="primary" :loading="checking" :disabled="!canVerify" @click="verify">
          验证
        </ElButton>
      </div>
    </template>
  </ElDialog>
</template>

<script setup lang="ts">
  import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue'
  import { ElAlert, ElButton, ElDialog, ElSlider, ElTooltip } from 'element-plus'
  import { Refresh } from '@element-plus/icons-vue'
  import { api, errorText } from './api'
  import { useSession } from './store'

  type BusinessPurpose =
    | 'group_binding_create'
    | 'group_binding_friend'
    | 'group_binding_friend_send'
    | 'group_claim'
    | 'operator_demo_claim'
  type CaptchaContext =
    | { purpose: 'login'; username: string }
    | { purpose: BusinessPurpose; platform: 'wsl' | 'lajiaoliao'; target?: string }
  interface Challenge {
    challenge_id: string
    image: {
      background: string
      tile: string
      width: number
      height: number
      tile_width: number
      tile_height: number
      initial_x: number
      initial_y: number
    }
    expires_at: string
  }
  const session = useSession()
  const visible = ref(false),
    loading = ref(false),
    checking = ref(false),
    expired = ref(false),
    needsRefresh = ref(false),
    keyboard = ref(false),
    backgroundLoaded = ref(false),
    tileLoaded = ref(false),
    position = ref(0),
    error = ref(''),
    challenge = ref<Challenge>()
  let context: CaptchaContext | undefined,
    resolve: ((ticket: string | null) => void) | undefined,
    controller: AbortController | undefined,
    expiryTimer: ReturnType<typeof setTimeout> | undefined,
    previousID: string | undefined,
    opener: HTMLElement | undefined,
    revision = 0
  const canVerify = computed(
    () =>
      visible.value &&
      !!challenge.value &&
      backgroundLoaded.value &&
      tileLoaded.value &&
      !loading.value &&
      !checking.value &&
      !expired.value &&
      !needsRefresh.value
  )
  const status = computed(() =>
    checking.value
      ? '正在验证…'
      : loading.value ||
          (challenge.value && (!backgroundLoaded.value || !tileLoaded.value) && !needsRefresh.value)
        ? '正在加载拼图…'
        : expired.value || needsRefresh.value
          ? '请刷新后重新验证'
          : keyboard.value
            ? '调整后按回车或点击验证'
            : '滑动完成后自动验证'
  )
  const tileStyle = computed(() => {
    const image = challenge.value?.image
    return image
      ? {
          width: `${(image.tile_width / image.width) * 100}%`,
          height: `${(image.tile_height / image.height) * 100}%`,
          left: `${(position.value / image.width) * 100}%`,
          top: `${(image.initial_y / image.height) * 100}%`
        }
      : {}
  })
  function finish(ticket: string | null) {
    const pending = resolve
    resolve = undefined
    revision++
    controller?.abort()
    clearTimeout(expiryTimer)
    context = undefined
    previousID = undefined
    challenge.value = undefined
    visible.value = loading.value = checking.value = false
    pending?.(ticket)
  }
  function cancel() {
    finish(null)
  }
  function open(value: CaptchaContext): Promise<string | null> {
    const returnTo = visible.value
      ? opener
      : document.activeElement instanceof HTMLElement
        ? document.activeElement
        : undefined
    cancel()
    opener = returnTo
    context = { ...value }
    visible.value = true
    const result = new Promise<string | null>((done) => (resolve = done))
    void refresh()
    return result
  }
  async function restoreFocus() {
    await nextTick()
    if (!visible.value && opener?.isConnected) opener.focus({ preventScroll: true })
  }
  function scopeBody(value: CaptchaContext) {
    return value.purpose === 'login'
      ? { username: value.username }
      : {
          purpose: value.purpose,
          platform: value.platform,
          ...(value.target ? { target: value.target } : {})
        }
  }
  function endpoint(value: CaptchaContext) {
    return value.purpose === 'login' ? '/auth/captcha' : '/human-verification'
  }
  function expire() {
    revision++
    controller?.abort()
    clearTimeout(expiryTimer)
    loading.value = checking.value = false
    expired.value = true
    error.value = '拼图已到期，请刷新后重新验证'
  }
  function imageError() {
    if (!visible.value || !challenge.value) return
    revision++
    controller?.abort()
    clearTimeout(expiryTimer)
    checking.value = false
    needsRefresh.value = true
    error.value = errorText('captcha_unavailable')
  }
  async function refresh() {
    if (!context || !visible.value) return
    const frozen = context,
      request = ++revision
    controller?.abort()
    clearTimeout(expiryTimer)
    const active = new AbortController()
    controller = active
    const timeout = setTimeout(() => active.abort(), 15000)
    challenge.value = undefined
    expired.value = needsRefresh.value = checking.value = keyboard.value = false
    backgroundLoaded.value = tileLoaded.value = false
    loading.value = true
    error.value = ''
    try {
      const value = await api<Challenge>(
        `${endpoint(frozen)}/challenge`,
        'POST',
        { ...scopeBody(frozen), ...(previousID ? { previous_id: previousID } : {}) },
        active.signal,
        frozen.purpose === 'login' ? 'wsl' : frozen.platform
      )
      if (request !== revision || !visible.value) return
      if (active.signal.aborted) throw new Error('拼图加载超时，请刷新重试')
      const image = value.image,
        deadline = Date.parse(value.expires_at)
      if (
        !value.challenge_id ||
        !image?.background ||
        !image.tile ||
        ![image.width, image.height, image.tile_width, image.tile_height].every(
          (size) => Number.isFinite(size) && size > 0
        ) ||
        image.tile_width >= image.width ||
        image.tile_height > image.height ||
        !Number.isFinite(image.initial_x) ||
        !Number.isFinite(image.initial_y) ||
        image.initial_x < 0 ||
        image.initial_x > image.width - image.tile_width ||
        image.initial_y < 0 ||
        image.initial_y > image.height - image.tile_height ||
        !Number.isFinite(deadline)
      )
        throw new Error(errorText('captcha_unavailable'))
      previousID = value.challenge_id
      challenge.value = value
      position.value = image.initial_x
      if (deadline <= Date.now()) expire()
      else expiryTimer = setTimeout(expire, deadline - Date.now())
    } catch (e) {
      if (request !== revision) return
      needsRefresh.value = true
      error.value = active.signal.aborted ? '拼图加载超时，请刷新重试' : (e as Error).message
    } finally {
      clearTimeout(timeout)
      if (request === revision) loading.value = false
    }
  }
  async function verify() {
    if (!canVerify.value || !challenge.value || !context) return
    const value = challenge.value,
      frozen = context,
      request = revision
    if (Date.parse(value.expires_at) <= Date.now()) {
      expire()
      return
    }
    checking.value = true
    const active = new AbortController()
    controller = active
    const timeout = setTimeout(() => active.abort(), 15000)
    try {
      const result = await api<{ ticket: string; expires_at: string }>(
        `${endpoint(frozen)}/verify`,
        'POST',
        {
          ...scopeBody(frozen),
          challenge_id: value.challenge_id,
          x: Math.round(position.value),
          y: value.image.initial_y
        },
        active.signal,
        frozen.purpose === 'login' ? 'wsl' : frozen.platform
      )
      if (request !== revision || !visible.value) return
      if (active.signal.aborted) throw new Error('验证超时，请刷新重试')
      if (
        typeof result.ticket !== 'string' ||
        !result.ticket ||
        !Number.isFinite(Date.parse(result.expires_at)) ||
        Date.parse(result.expires_at) <= Date.now()
      )
        throw new Error(errorText('captcha_invalid'))
      finish(result.ticket)
    } catch (e) {
      if (request !== revision) return
      needsRefresh.value = true
      error.value = active.signal.aborted ? '验证超时，请刷新重试' : (e as Error).message
    } finally {
      clearTimeout(timeout)
      if (request === revision) checking.value = false
    }
  }
  function positionChanged() {
    if (!keyboard.value) void verify()
  }
  function sliderKeydown(event: KeyboardEvent) {
    keyboard.value = true
    if (event.key === 'Enter') {
      event.preventDefault()
      event.stopPropagation()
      void verify()
    }
  }
  watch(
    [
      () => session.user?.id,
      () => session.user?.tenant_id,
      () => session.user?.role,
      () => session.user?.tenant_role,
      () => JSON.stringify([...session.permissions].sort()),
      () => session.console,
      () => session.workspace,
      () => session.selectedAccount
    ],
    cancel,
    { flush: 'sync' }
  )
  onBeforeUnmount(cancel)
  defineExpose({ open, cancel })
</script>

<style scoped>
  :global(.web-captcha-dialog) {
    display: flex;
    flex-direction: column;
    max-width: calc(100vw - 24px);
    max-height: calc(100dvh - 24px);
    margin: 12px auto;
  }
  :global(.web-captcha-dialog > .el-dialog__body) {
    min-height: 0;
    overflow-y: auto;
  }
  :global(.web-captcha-dialog > .el-dialog__header),
  :global(.web-captcha-dialog > .el-dialog__footer) {
    flex-shrink: 0;
  }
  .captcha-hint {
    margin: 0 0 14px;
    color: var(--el-text-color-secondary);
    font-size: 13px;
  }
  .captcha-image {
    position: relative;
    display: grid;
    place-items: center;
    width: 100%;
    overflow: hidden;
    border-radius: 6px;
    background: var(--el-fill-color-light);
    color: var(--el-text-color-secondary);
    font-size: 13px;
  }
  .captcha-image img {
    display: block;
    width: 100%;
    height: 100%;
    user-select: none;
    pointer-events: none;
  }
  .captcha-image .captcha-tile {
    position: absolute;
  }
  .web-captcha :deep(.el-slider) {
    box-sizing: border-box;
    width: calc(100% - 24px);
    margin: 18px 12px 8px;
    touch-action: pan-y;
  }
  .web-captcha :deep(.el-slider__button-wrapper:focus-visible) {
    border-radius: 50%;
    outline: 2px solid var(--el-color-primary);
    outline-offset: 2px;
  }
  .captcha-status {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 8px;
    min-height: 40px;
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .captcha-status span {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .captcha-status .el-button {
    flex-shrink: 0;
    width: 40px;
    height: 40px;
    padding: 8px;
  }
  .web-captcha :deep(.el-alert__title) {
    overflow-wrap: anywhere;
  }
  .captcha-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
  }
  .captcha-actions .el-button {
    min-width: 72px;
    margin: 0;
  }
</style>
