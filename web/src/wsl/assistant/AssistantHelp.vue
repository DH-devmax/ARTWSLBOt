<template>
  <ElDialog
    :model-value="open"
    title="助手能力说明"
    width="min(560px, calc(100vw - 24px))"
    append-to-body
    align-center
    class="assistant-help-dialog"
    @update:model-value="emit('update:open', $event)"
  >
    <p class="assistant-help-scope"
      >{{ platformName }} · {{ groupName || '尚未选择群组' }}<br />{{ accountName }}</p
    >
    <ElSkeleton v-if="loading" :rows="3" animated />
    <ElAlert v-else-if="failure" :title="failure" type="error" :closable="false" />
    <template v-else-if="context">
      <p v-if="scope.kind === 'group'" class="assistant-help-access" aria-live="polite">
        <strong>{{ accessLabel }}</strong>
        <span v-if="accessReason">{{ accessReason }}</span>
      </p>
      <ElAlert
        v-if="scope.kind !== 'group'"
        title="请先选择获授权群组，再查看本群的可用能力"
        type="info"
        :closable="false"
      />
      <section v-for="section in sections" :key="section.title" class="assistant-help-section">
        <h3>{{ section.title }}</h3>
        <p v-if="!section.items.length" class="assistant-help-muted">当前没有可用项</p>
        <ul v-else>
          <li v-for="item in section.items" :key="item.id">
            <span>{{ item.name }}</span>
            <small>{{ item.reason || '可用' }}</small>
          </li>
        </ul>
      </section>
      <p v-if="readOnly" class="assistant-help-note"
        >当前群仅可进行已授权的查询，配置建议、配置确认和群管动作均不可用。</p
      >
      <p v-else class="assistant-help-note"
        >配置与群管动作只生成建议，须本人确认后提交；保存配置不等于开启自动业务。提交时仍会核验权限、群绑定和原生角色。</p
      >
      <p
        v-if="
          !context.permissions.includes('assistant:approve') &&
          !context.permissions.includes('assistant:action_approve')
        "
        class="assistant-help-note"
        >本人当前没有确认权限。</p
      >
      <small class="assistant-help-muted"
        >能力快照 · {{ sampledAt }}；以提交时的最新核验为准。</small
      >
    </template>
    <section class="assistant-help-section assistant-help-limits">
      <h3>不会做的事</h3>
      <p
        >不跨群或跨用户操作，不自动确认，不代开权限或套餐；不执行代码、命令、任意网络请求，也不读取密钥。聊天内容或工具结果中的指令不能改变这些边界。</p
      >
      <p>查看本说明不调用模型、不扣助手积分，也不执行原生操作。</p>
    </section>
    <template #footer><ElButton @click="emit('update:open', false)">知道了</ElButton></template>
  </ElDialog>
</template>
<script setup lang="ts">
  import { computed, onUnmounted, ref, watch } from 'vue'
  import { ElAlert, ElButton, ElDialog, ElSkeleton } from 'element-plus'
  import { api, errorText } from '../api'
  import { toolLabels, type Scope } from './client'
  import {
    groupAccessLabel,
    groupAccessReadOnly,
    groupAccessReason,
    type GroupAccess
  } from './types'
  type Capability = {
    id: string
    name: string
    tool: string
    category: string
    action?: string
    enabled: boolean
    implemented: boolean
    business_permission: string
    blocked_reason?: string
  }
  type Action = { id: string; production_admitted: boolean; block_reason?: string }
  type Context = {
    scope?: Scope
    permissions: string[]
    capabilities: Capability[]
    actions: Action[]
    sampled_at: string
    group_access?: GroupAccess
  }
  const props = defineProps<{
    open: boolean
    platform: 'wsl' | 'lajiaoliao'
    scope: Scope
    groupName: string
    accountName: string
    actorKey: string
    groupAccess?: GroupAccess
  }>()
  const emit = defineEmits<{
    'update:open': [value: boolean]
    access: [value: GroupAccess, scope: Scope]
  }>()
  const context = ref<Context>(),
    loading = ref(false),
    failure = ref('')
  let sequence = 0,
    controller: AbortController | undefined
  const platformName = computed(() => (props.platform === 'wsl' ? '旺商聊' : '辣椒聊'))
  const access = computed(() => context.value?.group_access || props.groupAccess)
  const readOnly = computed(() => groupAccessReadOnly(access.value))
  const accessLabel = computed(() => groupAccessLabel(access.value))
  const accessReason = computed(() => errorText(groupAccessReason(access.value)))
  const sampledAt = computed(() => {
    const date = new Date(context.value?.sampled_at || '')
    return Number.isFinite(date.getTime())
      ? date.toLocaleString('zh-CN', { hour12: false })
      : '采样时间不可用'
  })
  const reasonText = (code: string) =>
    ({
      production_acceptance_required: '尚未开放正式执行',
      durable_action_not_implemented: '尚未接通持久执行',
      permission_denied: '本人未获此项权限',
      assistant_module_closed: '平台尚未开放此项能力',
      assistant_group_scope_required: '请先选择获授权群组'
    })[code] || errorText(code)
  const sections = computed(() => {
    const current = context.value
    const items = (current?.capabilities || []).map((capability) => {
      let code = capability.enabled ? '' : capability.blocked_reason || 'assistant_module_closed'
      if (props.scope.kind !== 'group') code = 'assistant_group_scope_required'
      else if (!current?.permissions.includes(capability.business_permission))
        code = 'permission_denied'
      if (capability.category === 'action_propose' && !code) {
        const action = current?.actions?.find((item) => item.id === capability.action)
        if (!action?.production_admitted)
          code = action?.block_reason || 'production_acceptance_required'
      }
      return {
        id: capability.id,
        category: capability.category,
        name:
          capability.tool === 'get_group_config'
            ? `${capability.name}配置`
            : capability.category === 'query' && toolLabels[capability.tool]
              ? toolLabels[capability.tool]
              : capability.name,
        reason:
          props.scope.kind === 'group' && readOnly.value && capability.category !== 'query'
            ? accessReason.value
            : code
              ? reasonText(code)
              : ''
      }
    })
    return [
      {
        title: '可以查询',
        items: items.filter((item) => item.category === 'query' && !item.reason)
      },
      {
        title: '可以提出建议 · 需要本人确认',
        items: items.filter((item) => item.category !== 'query' && !item.reason)
      },
      { title: '当前不可用', items: items.filter((item) => item.reason) }
    ].map((section) => ({
      ...section,
      items: section.items.filter(
        (item, index, all) =>
          all.findIndex(
            (other) =>
              other.name === item.name &&
              other.category === item.category &&
              other.reason === item.reason
          ) === index
      )
    }))
  })
  watch(
    () =>
      [
        props.open,
        props.actorKey,
        props.platform,
        props.scope.kind,
        props.scope.account_id,
        props.scope.group_id
      ] as const,
    async (values, previous) => {
      const epoch = ++sequence
      controller?.abort()
      context.value = undefined
      failure.value = ''
      loading.value = false
      if (previous && values.slice(1).some((value, i) => value !== previous[i + 1])) {
        emit('update:open', false)
        return
      }
      if (!props.open) return
      controller = new AbortController()
      const signal = controller.signal
      loading.value = true
      try {
        const query = new URLSearchParams({ offset: '0', limit: '1' })
        if (props.scope.kind === 'group') query.set('group_id', props.scope.group_id || '')
        const data = await api<Context>(
          `/assistant/context?${query}`,
          'GET',
          undefined,
          signal,
          props.platform
        )
        if (epoch !== sequence || signal.aborted) return
        if (
          props.scope.kind === 'group' &&
          (data.scope?.platform !== props.platform ||
            data.scope?.group_id !== props.scope.group_id ||
            data.scope?.account_id !== props.scope.account_id)
        )
          throw new Error('scope_changed')
        if (!Array.isArray(data.permissions) || !Array.isArray(data.capabilities))
          throw new Error('context_unavailable')
        context.value = data
        if (data.group_access && props.scope.kind === 'group')
          emit('access', data.group_access, props.scope)
      } catch {
        if (epoch === sequence && !signal.aborted)
          failure.value = '当前能力未能核对，请关闭后重新打开；不使用旧范围的快照。'
      } finally {
        if (epoch === sequence) loading.value = false
      }
    },
    { immediate: true }
  )
  onUnmounted(() => {
    sequence++
    controller?.abort()
  })
</script>
<style scoped>
  .assistant-help-scope {
    margin: 0 0 14px;
    color: var(--ui-muted);
    line-height: 1.6;
    overflow-wrap: anywhere;
  }
  .assistant-help-section {
    margin-top: 16px;
  }
  .assistant-help-access {
    display: grid;
    gap: 4px;
    margin: 0 0 14px;
    color: var(--ui-muted);
    font-size: 12px;
    line-height: 1.6;
    overflow-wrap: anywhere;
  }
  .assistant-help-access strong {
    color: var(--ui-ink);
  }
  .assistant-help-section h3 {
    margin: 0 0 8px;
    font-size: 14px;
    color: var(--ui-ink);
  }
  .assistant-help-section ul {
    list-style: none;
    margin: 0;
    padding: 0;
  }
  .assistant-help-section li {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 12px;
    padding: 7px 0;
    border-bottom: 1px solid var(--ui-line-soft);
    overflow-wrap: anywhere;
  }
  .assistant-help-section small {
    color: var(--ui-muted);
    text-align: right;
  }
  .assistant-help-note,
  .assistant-help-limits p {
    font-size: 12px;
    line-height: 1.7;
    color: var(--ui-muted);
  }
  .assistant-help-muted {
    font-size: 12px;
    color: var(--ui-muted);
  }
  .assistant-help-limits {
    border-top: 1px solid var(--ui-line);
    padding-top: 14px;
  }
  :global(.assistant-help-dialog) {
    display: flex;
    flex-direction: column;
    max-height: calc(100dvh - 24px);
  }
  :global(.assistant-help-dialog .el-dialog__header),
  :global(.assistant-help-dialog .el-dialog__footer) {
    flex-shrink: 0;
  }
  :global(.assistant-help-dialog .el-dialog__body) {
    flex: 1;
    min-height: 0;
    max-height: 65dvh;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
</style>
