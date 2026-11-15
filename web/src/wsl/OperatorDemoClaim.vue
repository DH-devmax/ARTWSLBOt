<template>
  <ElDialog
    :model-value="true"
    title="测试认领"
    width="480px"
    class="operator-demo-claim"
    append-to-body
    align-center
    :close-on-click-modal="false"
    @close="close"
  >
    <div class="demo-claim-heading">
      <ElTag type="info" effect="plain">超管演示 · 10 分钟</ElTag>
      <strong>{{ group.name }}</strong>
    </div>
    <dl class="demo-claim-scope">
      <dt>所属客户</dt><dd>{{ group.customer_name || group.tenant_name || '—' }}</dd> <dt>平台</dt
      ><dd>{{ platformLabel(platform) }}</dd> <dt>群组</dt><dd>{{ group.name }}</dd> <dt>机器人</dt
      ><dd>{{ robotLabel(group) }}</dd>
    </dl>
    <p class="demo-claim-description">
      仅用于本人 10 分钟测试，不开通套餐，不改动客户群绑定或业务运行状态。
    </p>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElSkeleton v-if="loading" :rows="2" animated />
    <div v-else-if="claim" class="demo-claim-state" aria-live="polite">
      <ElTag :type="active ? 'success' : 'info'">{{ stateLabel }}</ElTag>
      <dl v-if="claim.expires_at || claim.robot_role" class="demo-claim-scope">
        <template v-if="claim.expires_at">
          <dt>到期时间</dt><dd>{{ customerDate(claim.expires_at) }}</dd>
        </template>
        <template v-if="claim.robot_role">
          <dt>机器人角色</dt><dd>{{ roleLabel(claim.robot_role) }}</dd>
        </template>
      </dl>
      <ElAlert
        v-if="claim.blocked_reason"
        :title="reason(claim.blocked_reason)"
        type="warning"
        :closable="false"
      />
    </div>
    <ElAlert
      v-if="uncertain"
      :title="claim?.state === 'checking' ? '正在核验，请刷新进度。' : '结果待核对，请刷新进度。'"
      type="warning"
      :closable="false"
    />
    <template #footer>
      <ElButton @click="close">关闭</ElButton>
      <ElButton :icon="Refresh" :loading="loading" :disabled="busy" @click="read">
        刷新进度
      </ElButton>
      <ElButton
        v-if="active"
        :icon="CircleClose"
        :loading="busy"
        :disabled="loading || uncertain"
        @click="cancel"
      >
        解除测试认领
      </ElButton>
      <ElButton
        v-else
        type="primary"
        :icon="CircleCheck"
        :loading="busy"
        :disabled="loading || !claim || uncertain || !allowed || claim.state === 'checking'"
        @click="verify"
      >
        核验测试群
      </ElButton>
    </template>
  </ElDialog>
  <WebCaptcha ref="webCaptcha" />
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import { ElAlert, ElButton, ElDialog, ElSkeleton, ElTag } from 'element-plus'
  import { CircleCheck, CircleClose, Refresh } from '@element-plus/icons-vue'
  import { api, ApiError, type Row } from './api'
  import { useSession } from './store'
  import WebCaptcha from './WebCaptcha.vue'
  import {
    customerDate,
    customerReason,
    groupKey,
    platformLabel,
    robotLabel,
    type CustomerPlatform
  } from './customer'

  type DemoClaim = {
    id: string
    group_id: string
    platform: CustomerPlatform
    state: 'active' | 'cancelled' | 'expired' | 'stale' | 'unverified' | 'checking' | 'failed'
    version: number
    expires_at?: string
    robot_role?: string
    blocked_reason?: string
  }
  const props = defineProps<{ group: Row; isDemo: boolean }>()
  const emit = defineEmits<{ close: []; changed: [] }>()
  const session = useSession(),
    group = { ...props.group },
    id = groupKey(group),
    platform = group.platform as CustomerPlatform,
    actor = session.user?.id,
    tenant = session.user?.tenant_id,
    account = session.selectedAccount,
    view = session.console,
    endpoint = `/user-center/groups/${encodeURIComponent(id)}/demo-claim`
  const claim = ref<DemoClaim>(),
    loading = ref(true),
    busy = ref(false),
    uncertain = ref(false),
    error = ref(''),
    now = ref(Date.now())
  const webCaptcha = ref<InstanceType<typeof WebCaptcha>>()
  let controller: AbortController | undefined,
    expiryTimer: ReturnType<typeof setTimeout> | undefined,
    sequence = 0,
    alive = true
  const allowed = computed(
    () =>
      props.isDemo &&
      session.user?.role === 'superadmin' &&
      actor === session.user.id &&
      tenant === session.user.tenant_id &&
      account === session.selectedAccount &&
      view === session.console &&
      platform === session.workspace &&
      props.group.active === true &&
      props.group.platform === platform &&
      groupKey(props.group) === id &&
      props.group.version === group.version
  )
  const current = (request: number) =>
    alive && allowed.value && sequence === request && !controller?.signal.aborted
  const active = computed(
    () =>
      claim.value?.state === 'active' &&
      (!claim.value.expires_at || new Date(claim.value.expires_at).getTime() > now.value)
  )
  const stateLabel = computed(() =>
    claim.value?.state === 'active'
      ? active.value
        ? '已核验'
        : '已过期'
      : {
          cancelled: '已解除',
          expired: '已过期',
          stale: '需重新核验',
          unverified: '待核验',
          checking: '核验中',
          failed: '核验失败'
        }[claim.value?.state || 'unverified']
  )
  const roleLabel = (role: string) =>
    ({ owner: '群主', admin: '管理员', member: '普通成员' })[role] || '待核验'
  const reason = (code: string) =>
    (
      ({
        operator_demo_claim_required: '请先核验本群的测试认领',
        operator_demo_claim_expired: '测试认领已到期，请重新核验',
        operator_demo_claim_role_required: '机器人需要是本群群主或管理员，才可测试认领',
        operator_demo_claim_scope_changed: '群授权或机器人状态已变化，请关闭后重新核验',
        operator_demo_claim_commit_unknown: '核验提交结果待确认，请刷新进度'
      }) as Record<string, string>
    )[code] || customerReason(code)
  function apply(value: DemoClaim) {
    if (
      value.group_id !== id ||
      value.platform !== platform ||
      !['active', 'cancelled', 'expired', 'stale', 'unverified', 'checking', 'failed'].includes(
        value.state
      ) ||
      !Number.isInteger(value.version) ||
      (value.state === 'active' && !value.id)
    )
      throw new Error(reason('operator_demo_claim_scope_changed'))
    claim.value = value
    uncertain.value =
      value.state === 'checking' || value.blocked_reason === 'operator_demo_claim_commit_unknown'
    now.value = Date.now()
    clearTimeout(expiryTimer)
    if (value.state === 'active' && value.expires_at) {
      const remaining = new Date(value.expires_at).getTime() - now.value
      if (remaining > 0 && Number.isFinite(remaining))
        expiryTimer = setTimeout(() => (now.value = Date.now()), remaining + 20)
    }
  }
  function showError(value: unknown) {
    error.value =
      value instanceof ApiError
        ? reason(value.code)
        : value instanceof Error && value.message === reason('operator_demo_claim_scope_changed')
          ? value.message
          : '请求未完成，请查询当前测试认领'
  }
  async function read() {
    if (!allowed.value || busy.value) return
    const request = ++sequence
    controller?.abort()
    controller = new AbortController()
    loading.value = true
    error.value = ''
    try {
      const next = await api<DemoClaim>(endpoint, 'GET', undefined, controller.signal, platform)
      if (!current(request)) return
      apply(next)
    } catch (e) {
      if (current(request)) showError(e)
    } finally {
      if (current(request)) loading.value = false
    }
  }
  async function mutate(path: string, body: Row) {
    if (!allowed.value || loading.value || busy.value || uncertain.value) return
    const request = ++sequence,
      requestKey = crypto.randomUUID()
    controller?.abort()
    controller = new AbortController()
    busy.value = true
    error.value = ''
    try {
      let captchaTicket: string | undefined
      if (path === endpoint) {
        const ticket = await webCaptcha.value?.open({
          purpose: 'operator_demo_claim',
          platform,
          target: id
        })
        if (!ticket || !current(request)) return
        captchaTicket = ticket
      }
      const next = await api<DemoClaim>(
        path,
        'POST',
        body,
        controller.signal,
        platform,
        requestKey,
        captchaTicket
      )
      if (!current(request)) return
      apply(next)
      emit('changed')
    } catch (e) {
      if (!current(request)) return
      uncertain.value = !(e instanceof ApiError) || e.code === 'operator_demo_claim_commit_unknown'
      showError(e)
    } finally {
      if (current(request)) busy.value = false
    }
  }
  function verify() {
    if (claim.value && claim.value.state !== 'checking' && !active.value)
      void mutate(endpoint, { binding_version: group.version })
  }
  function cancel() {
    if (!active.value || !claim.value?.id) return
    void mutate(`${endpoint}/${encodeURIComponent(claim.value.id)}/cancel`, {
      version: claim.value.version
    })
  }
  function stop() {
    alive = false
    sequence++
    controller?.abort()
    webCaptcha.value?.cancel()
    clearTimeout(expiryTimer)
  }
  function close() {
    stop()
    emit('close')
  }
  watch(
    allowed,
    (value) => {
      if (!value) close()
    },
    { flush: 'sync' }
  )
  onBeforeUnmount(stop)
  void read()
</script>

<style>
  .operator-demo-claim {
    display: flex;
    flex-direction: column;
    max-width: calc(100vw - 24px);
    max-height: calc(100dvh - 32px);
    margin: 16px auto;
    overflow: hidden;
  }
  .operator-demo-claim > .el-dialog__body {
    min-height: 0;
    overflow-y: auto;
  }
  .operator-demo-claim > .el-dialog__header,
  .operator-demo-claim > .el-dialog__footer {
    flex-shrink: 0;
  }
  .operator-demo-claim > .el-dialog__footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
  }
  .operator-demo-claim > .el-dialog__footer .el-button {
    margin: 0;
  }
</style>

<style scoped>
  .demo-claim-heading {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: 12px;
  }
  .demo-claim-heading strong {
    font-size: 17px;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .demo-claim-scope {
    display: grid;
    grid-template-columns: 76px minmax(0, 1fr);
    gap: 12px;
    margin: 20px 0;
  }
  .demo-claim-scope dt,
  .demo-claim-description {
    color: var(--el-text-color-secondary);
  }
  .demo-claim-scope dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .demo-claim-description {
    margin: 0 0 20px;
    padding-bottom: 20px;
    border-bottom: 1px solid var(--el-border-color-light);
    font-size: 13px;
    line-height: 1.7;
  }
  .demo-claim-state {
    margin-top: 16px;
  }
</style>
