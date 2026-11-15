<template>
  <ElDialog
    :model-value="true"
    title="单次群管操作"
    width="560px"
    class="uc-dialog user-group-action"
    append-to-body
    align-center
    :close-on-click-modal="false"
    @close="$emit('close')"
  >
    <div class="action-scope">
      <strong>{{ group.name }}</strong>
      <span>{{ platformLabel(group.platform) }} · {{ robotLabel(group) }}</span>
      <small
        >所属客户：{{
          plan?.customer_name || group.customer_name || group.tenant_name || '—'
        }}</small
      >
      <ElTag v-if="plan?.execution_mode === 'operator_demo'" type="info">超管演示 · 单次操作</ElTag>
    </div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElSkeleton v-if="loading" :rows="3" animated />
    <template v-else-if="!plan">
      <ElEmpty v-if="!actions.length" description="当前群暂无已接入的页面动作" :image-size="40" />
      <ElForm v-else label-position="top" @submit.prevent="createPlan">
        <ElFormItem label="动作">
          <ElSelect v-model="action" aria-label="群管动作" :disabled="busy || uncertain">
            <ElOption
              v-for="item in actions"
              :key="item.id"
              :value="item.id"
              :label="item.name || actionLabels[item.id] || item.id"
            />
          </ElSelect>
        </ElFormItem>
        <ElAlert
          v-if="selectedAction?.production_admitted !== true"
          :title="customerReason(selectedAction?.block_reason || 'production_not_admitted')"
          type="warning"
          :closable="false"
        />
        <ElFormItem
          v-if="needsTarget"
          :label="action === 'recall_message' ? '选择消息' : '选择成员'"
        >
          <ElSelect
            v-model="targetToken"
            filterable
            remote
            :remote-method="searchTargets"
            :loading="loadingTargets"
            :disabled="busy || uncertain"
            placeholder="搜索并核对目标"
            aria-label="群管目标"
          >
            <ElOption
              v-for="item in targets"
              :key="item.token"
              :value="item.token"
              :label="`${item.name} · ${item.id}`"
            />
          </ElSelect>
        </ElFormItem>
        <ElFormItem v-if="needsText" :label="textLabel">
          <ElInput
            v-model="text"
            :type="action === 'group_info' || action === 'member_nickname' ? 'text' : 'textarea'"
            :rows="3"
            maxlength="4000"
            show-word-limit
            :aria-label="textLabel"
            :disabled="busy || uncertain"
          />
        </ElFormItem>
        <ElFormItem v-if="action === 'mute_member'" label="禁言时长（分钟）">
          <ElInputNumber v-model="minutes" :min="1" :max="1440" :disabled="busy || uncertain" />
        </ElFormItem>
      </ElForm>
    </template>
    <template v-else>
      <div class="action-plan">
        <strong>{{
          plan.name || plan.action_name || actionLabels[plan.action] || plan.action
        }}</strong>
        <dl>
          <dt>群组</dt><dd>{{ plan.group_name || group.name }}</dd> <dt>平台</dt
          ><dd>{{ platformLabel(plan.scope?.platform || group.platform) }}</dd> <dt>机器人</dt
          ><dd>{{ plan.robot_nickname || robotLabel(group) }}</dd> <dt v-if="plan.target">目标</dt
          ><dd v-if="plan.target">{{ plan.target.name }} · {{ plan.target.id }}</dd>
          <dt v-if="plan.text">内容</dt
          ><dd v-if="plan.text" class="action-content">{{ plan.text }}</dd>
          <dt v-if="plan.minutes">时长</dt><dd v-if="plan.minutes">{{ plan.minutes }} 分钟</dd>
          <dt v-if="plan.expires_at">有效期至</dt
          ><dd v-if="plan.expires_at">{{ customerDate(plan.expires_at) }}</dd> <dt>状态</dt
          ><dd>{{
            plan.status === 'blocked' && plan.production_admitted === true
              ? '等待本人确认'
              : stateLabel(plan.status)
          }}</dd>
          <dt>操作编号</dt><dd>{{ plan.operation_id || plan.operation || plan.id }}</dd>
        </dl>
        <ElAlert
          v-if="plan.production_admitted !== true"
          :title="
            customerReason(plan.blocked_reason || plan.result_code || 'production_not_admitted')
          "
          type="warning"
          :closable="false"
        />
        <ElAlert
          v-if="isUnknown || uncertain"
          title="本次结果待核对，请查询原计划。"
          type="warning"
          :closable="false"
        />
      </div>
    </template>
    <template #footer>
      <ElButton @click="$emit('close')">关闭</ElButton>
      <template v-if="!plan">
        <ElButton type="primary" :loading="busy" :disabled="!canCreate" @click="createPlan"
          >核对操作计划</ElButton
        >
      </template>
      <template v-else>
        <ElButton :icon="Refresh" :loading="busy" @click="readPlan">查询原计划</ElButton>
        <ElButton v-if="canCancel" :disabled="busy" @click="cancelPlan">取消计划</ElButton>
        <ElButton v-if="canStartNew" :disabled="busy" @click="startNew">新的单次操作</ElButton>
        <ElButton v-if="canApprove" type="primary" :loading="busy" @click="approvePlan">{{
          confirmLabel
        }}</ElButton>
      </template>
    </template>
  </ElDialog>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import {
    ElAlert,
    ElButton,
    ElDialog,
    ElEmpty,
    ElForm,
    ElFormItem,
    ElInput,
    ElInputNumber,
    ElOption,
    ElSelect,
    ElSkeleton,
    ElTag
  } from 'element-plus'
  import { Refresh } from '@element-plus/icons-vue'
  import { api, type Row } from './api'
  import { useSession } from './store'
  import {
    actionLabels,
    customerDate,
    customerReason,
    groupKey,
    platformLabel,
    robotLabel,
    type CustomerPlatform
  } from './customer'

  const props = defineProps<{ group: Row }>()
  const emit = defineEmits<{ close: []; updated: [] }>()
  const session = useSession()
  const group = { ...props.group },
    id = groupKey(group),
    platform = group.platform as CustomerPlatform,
    actor = session.user?.id,
    view = session.console
  const scope = `user:${actor}:${group.tenant_id || session.user?.tenant_id || 'platform'}:${platform}:${group.primary_account_id || group.account_id || 'bound'}:${id}:manual-action`
  const draft = session.drafts[scope] as Row | undefined
  const action = ref(String(draft?.action || '')),
    text = ref(String(draft?.text || '')),
    minutes = ref(Number(draft?.minutes) || 1),
    targetToken = ref(''),
    targetSearch = ref('')
  const actions = ref<Row[]>([]),
    targets = ref<Row[]>([]),
    plan = ref<Row | null>(draft?.plan_id ? { id: draft.plan_id } : null)
  const error = ref(''),
    loading = ref(false),
    loadingTargets = ref(false),
    busy = ref(false),
    uncertain = ref(Boolean(draft?.uncertain)),
    attempted = ref(Boolean(draft?.approval_key))
  const controller = new AbortController()
  let targetController: AbortController | undefined,
    targetGeneration = 0,
    targetTimer: ReturnType<typeof setTimeout> | undefined,
    alive = true
  let approvalKey = String(draft?.approval_key || '')
  const current = () =>
    alive &&
    actor === session.user?.id &&
    view === session.console &&
    platform === session.workspace &&
    !controller.signal.aborted
  const selectedAction = computed(() => actions.value.find((item) => item.id === action.value))
  const needsTarget = computed(() =>
    ['member_nickname', 'mute_member', 'unmute_member', 'kick_member', 'recall_message'].includes(
      action.value
    )
  )
  const needsText = computed(() =>
    ['group_info', 'publish_notice', 'member_nickname', 'send_text'].includes(action.value)
  )
  const textLabel = computed(
    () =>
      ({
        group_info: '群名称',
        publish_notice: '公告内容',
        member_nickname: '群名片',
        send_text: '消息内容'
      })[action.value] || '内容'
  )
  const isUnknown = computed(() =>
    ['unknown', 'running', 'queued', 'submitted', 'approved', 'submitting'].includes(
      plan.value?.status
    )
  )
  const pending = computed(
    () =>
      ['pending_approval', 'pending', 'proposed', 'ready'].includes(plan.value?.status) ||
      (plan.value?.status === 'blocked' && plan.value?.production_admitted === true)
  )
  const canCreate = computed(() =>
    Boolean(
      !busy.value &&
        !uncertain.value &&
        selectedAction.value &&
        (!needsTarget.value || targetToken.value) &&
        (!needsText.value || text.value.trim())
    )
  )
  const canApprove = computed(
    () =>
      pending.value &&
      plan.value?.production_admitted === true &&
      Boolean(plan.value?.digest) &&
      Number.isInteger(plan.value?.version) &&
      !uncertain.value &&
      !attempted.value
  )
  const canCancel = computed(
    () =>
      (pending.value || plan.value?.status === 'blocked') && !uncertain.value && !attempted.value
  )
  const canStartNew = computed(() =>
    ['cancelled', 'expired', 'failed', 'succeeded'].includes(plan.value?.status)
  )
  const confirmations: Record<string, string> = {
    mute_member: '确认禁言',
    unmute_member: '确认解禁',
    mute_group: '确认全群禁言',
    unmute_group: '确认解除全群禁言',
    kick_member: '确认移出成员',
    send_text: '确认发送',
    recall_message: '确认撤回',
    group_info: '确认修改群名称',
    publish_notice: '确认发布公告',
    member_nickname: '确认修改名片'
  }
  const confirmLabel = computed(() => confirmations[String(plan.value?.action || '')] || '确认操作')
  const stateLabel = (state?: string) =>
    ({
      pending_approval: '等待本人确认',
      pending: '等待本人确认',
      proposed: '等待本人确认',
      ready: '等待本人确认',
      blocked: '未通过准入',
      approved: '已确认',
      queued: '排队中',
      running: '执行中',
      succeeded: '已完成',
      submitted: '已提交',
      failed: '失败',
      cancelled: '已取消',
      expired: '已过期',
      unknown: '结果未知'
    })[state || ''] ||
    state ||
    '待查询'
  function saveDraft() {
    if (!current()) return
    session.saveDraft(scope, {
      action: action.value,
      text: text.value,
      minutes: minutes.value,
      plan_id: plan.value?.id || '',
      approval_key: approvalKey,
      uncertain: uncertain.value
    })
  }
  async function loadCapabilities() {
    loading.value = true
    try {
      const data = await api<Row>(
        `/groups/${id}/capabilities`,
        'GET',
        undefined,
        controller.signal,
        platform
      )
      if (!current()) return
      const list = Array.isArray(data.manual_actions) ? data.manual_actions : []
      actions.value = list.filter(
        (item: Row) =>
          item.implemented !== false &&
          (!Array.isArray(item.sources) || item.sources.includes('manual'))
      )
      if (!actions.value.some((item) => item.id === action.value))
        action.value = actions.value[0]?.id || ''
      if (plan.value?.id) await readPlan()
    } catch (e) {
      if (current()) error.value = (e as Error).message
    } finally {
      if (current()) loading.value = false
    }
  }
  function searchTargets(value: string) {
    targetSearch.value = value
    targetController?.abort()
    targetGeneration++
    clearTimeout(targetTimer)
    targetTimer = setTimeout(() => void loadTargets(), 250)
  }
  async function loadTargets() {
    if (!needsTarget.value || !current() || plan.value || !targetSearch.value.trim()) return
    targetController?.abort()
    const request = new AbortController(),
      generation = ++targetGeneration,
      chosenAction = action.value
    targetController = request
    loadingTargets.value = true
    try {
      const query = new URLSearchParams({ action: chosenAction, search: targetSearch.value })
      const data = await api<Row>(
        `/groups/${id}/action-targets?${query}`,
        'GET',
        undefined,
        request.signal,
        platform
      )
      if (current() && generation === targetGeneration && chosenAction === action.value)
        targets.value = (data.items || []).filter(
          (item: Row) => typeof item.token === 'string' && item.token
        )
    } catch (e) {
      if (current() && !request.signal.aborted) error.value = (e as Error).message
    } finally {
      if (current() && generation === targetGeneration) loadingTargets.value = false
    }
  }
  async function createPlan() {
    if (!canCreate.value || !current()) return
    busy.value = true
    error.value = ''
    try {
      const data = await api<Row>(
        `/groups/${id}/action-plans`,
        'POST',
        {
          action: action.value,
          ...(needsTarget.value ? { target_token: targetToken.value } : {}),
          ...(needsText.value ? { text: text.value.trim() } : {}),
          ...(action.value === 'mute_member' ? { minutes: minutes.value } : {})
        },
        controller.signal,
        platform
      )
      if (current()) {
        plan.value = data
        saveDraft()
      }
    } catch (e) {
      if (current()) {
        error.value = (e as Error).message
        saveDraft()
      }
    } finally {
      if (current()) busy.value = false
    }
  }
  async function readPlan() {
    if (!plan.value?.id || !current()) return
    busy.value = true
    error.value = ''
    try {
      const data = await api<Row>(
        `/groups/${id}/action-plans/${plan.value.id}`,
        'GET',
        undefined,
        controller.signal,
        platform
      )
      if (current()) {
        plan.value = data
        uncertain.value = false
        saveDraft()
      }
    } catch (e) {
      if (current()) error.value = (e as Error).message
    } finally {
      if (current()) busy.value = false
    }
  }
  async function approvePlan() {
    if (!canApprove.value || busy.value || !current()) return
    approvalKey ||= crypto.randomUUID()
    attempted.value = true
    saveDraft()
    busy.value = true
    error.value = ''
    try {
      const data = await api<Row>(
        `/groups/${id}/action-plans/${plan.value!.id}/approve`,
        'POST',
        {
          digest: plan.value!.digest,
          version: plan.value!.version,
          key: approvalKey
        },
        controller.signal,
        platform,
        approvalKey
      )
      if (current()) {
        plan.value = data
        saveDraft()
        emit('updated')
      }
    } catch (e) {
      if (current()) {
        uncertain.value = true
        error.value = `${(e as Error).message}；请查询原计划`
        saveDraft()
      }
    } finally {
      if (current()) busy.value = false
    }
  }
  async function cancelPlan() {
    if (!canCancel.value || busy.value || !current()) return
    busy.value = true
    error.value = ''
    try {
      const data = await api<Row>(
        `/groups/${id}/action-plans/${plan.value!.id}/cancel`,
        'POST',
        {},
        controller.signal,
        platform
      )
      if (current()) {
        plan.value = data
        saveDraft()
        emit('updated')
      }
    } catch (e) {
      if (current()) error.value = (e as Error).message
    } finally {
      if (current()) busy.value = false
    }
  }
  function startNew() {
    if (!canStartNew.value || busy.value) return
    plan.value = null
    approvalKey = ''
    attempted.value = false
    uncertain.value = false
    targetToken.value = ''
    targets.value = []
    error.value = ''
    saveDraft()
  }
  watch([action, text, minutes], saveDraft)
  watch(action, () => {
    targetController?.abort()
    targetGeneration++
    targetToken.value = ''
    targetSearch.value = ''
    targets.value = []
  })
  void loadCapabilities()
  onBeforeUnmount(() => {
    saveDraft()
    alive = false
    controller.abort()
    targetController?.abort()
    clearTimeout(targetTimer)
  })
</script>

<style scoped>
  .action-scope {
    display: grid;
    gap: 7px;
    margin-bottom: 20px;
    overflow-wrap: anywhere;
  }
  .action-scope span,
  .action-scope small,
  .action-plan dt {
    color: var(--el-text-color-secondary);
  }
  .action-scope .el-tag {
    justify-self: start;
  }
  .action-plan dl {
    display: grid;
    grid-template-columns: 70px minmax(0, 1fr);
    gap: 12px;
  }
  .action-plan dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .action-content {
    white-space: pre-wrap;
    max-height: 200px;
    overflow-y: auto;
  }
  .el-select {
    width: 100%;
  }
  .el-alert {
    margin-bottom: 12px;
  }
  :global(.user-group-action .el-dialog__footer) {
    display: flex;
    justify-content: flex-end;
    flex-wrap: wrap;
    gap: 8px;
  }
  :global(.user-group-action .el-dialog__footer .el-button + .el-button) {
    margin-left: 0;
  }
</style>
