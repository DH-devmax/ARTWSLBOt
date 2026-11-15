<template>
  <ElDialog
    :model-value="true"
    title="群业务与权限"
    width="680px"
    class="customer-scope-dialog group-business-dialog"
    append-to-body
    align-center
    :close-on-click-modal="false"
    @close="$emit('close')"
  >
    <div class="group-business-heading"
      ><div
        ><strong>{{ group.name }}</strong
        ><p
          >{{ platformLabel(group.platform) }} · {{ robotLabel({ ...group, ...capabilities }) }}</p
        ></div
      ><ElButton
        text
        :icon="Refresh"
        :loading="loading"
        :disabled="saving"
        aria-label="刷新群运行状态"
        @click="load(true)"
    /></div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElSkeleton v-if="loading && !runtime" :rows="5" animated />
    <template v-if="runtime">
      <div class="group-runtime-status"
        ><ElTag :type="runtime.runtime_state === 'running' ? 'success' : 'info'">{{
          managementPending
            ? '等待群管理启用'
            : runtimeLabels[runtime.runtime_state] ||
              (runtime.enabled ? '已启用，按能力准入执行' : '已停用')
        }}</ElTag
        ><ElTag
          v-if="typeof runtime.management_enabled === 'boolean'"
          :type="runtime.management_enabled ? 'success' : 'info'"
          >{{ runtime.management_enabled ? '群管理已启用' : '群管理未启用' }}</ElTag
        ><span>{{ customerDate(runtime.sampled_at) }}</span></div
      >
      <ElAlert
        v-if="runtime.blocked_reason"
        :title="customerReason(runtime.blocked_reason)"
        type="warning"
        :closable="false"
      />
      <section class="group-runtime-section">
        <div class="group-runtime-toggle"
          ><div><h3>本群自动业务</h3><p>拦截词、业务黑名单、豁免名单、关键词文本回复</p></div
          ><ElSwitch
            v-model="enabled"
            :disabled="!canManage || saving || uncertain || loading"
            aria-label="本群自动业务"
        /></div>
        <ElAlert
          v-if="!canManage"
          title="当前仅可查看；业务启停由客户负责人管理"
          type="info"
          :closable="false"
        />
        <p v-if="conflict" class="runtime-conflict"
          >状态已变化，已保留你的选择。当前服务端：{{
            runtime.enabled ? '已启用' : '已停用'
          }}。核对后可重新保存。</p
        >
      </section>
      <section class="group-runtime-section">
        <h3>本群有效能力</h3>
        <div v-if="permissions.length" class="runtime-permissions"
          ><ElTag
            v-for="permission in permissions"
            :key="permission.code || permission"
            type="info"
            effect="plain"
            >{{ permission.label || permission.name || permission }}</ElTag
          ></div
        >
        <ElEmpty v-if="!actions.length" description="暂无可用的群管动作" :image-size="40" />
        <div v-for="action in actions" :key="action.id" class="runtime-action-row"
          ><span>{{ action.name || actionLabels[action.id] }}</span
          ><ElTag :type="action.production_admitted ? 'success' : 'info'" size="small">{{
            action.production_admitted ? '可用' : '不可用'
          }}</ElTag
          ><small v-if="action.block_reason">{{ customerReason(action.block_reason) }}</small></div
        >
      </section>
    </template>
    <template #footer
      ><ElButton :disabled="saving" @click="$emit('close')">关闭</ElButton
      ><ElButton
        v-if="canManage"
        type="primary"
        :icon="Check"
        :loading="saving"
        :disabled="
          !runtime || loading || uncertain || (enabled === runtime.enabled && !managementPending)
        "
        @click="save"
        >{{ enabled ? '确认启用本群业务' : '保存停用' }}</ElButton
      ></template
    >
  </ElDialog>
</template>
<script setup lang="ts">
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import { ElAlert, ElButton, ElDialog, ElEmpty, ElSkeleton, ElSwitch, ElTag } from 'element-plus'
  import { Check, Refresh } from '@element-plus/icons-vue'
  import { api, ApiError, type Row } from './api'
  import { useSession } from './store'
  import {
    actionLabels,
    customerDate,
    customerReason,
    groupKey,
    platformLabel,
    robotLabel,
    runtimeLabels,
    type CustomerPlatform
  } from './customer'
  const props = defineProps<{ group: Row }>()
  const emit = defineEmits<{ close: []; saved: [] }>()
  const session = useSession()
  const actor = session.user?.id,
    tenant = session.user?.tenant_id
  const group = { ...props.group },
    id = groupKey(group),
    platform = group.platform as CustomerPlatform
  const scope = `customer:${actor}:${tenant}:${platform}:${group.primary_account_id || group.account_id || 'bound'}:${id}:business-runtime`
  const runtime = ref<Row>(),
    capabilities = ref<Row>(),
    enabled = ref(false),
    conflict = ref(false)
  const loading = ref(false),
    saving = ref(false),
    error = ref(''),
    uncertain = ref(false)
  const canManage = computed(
    () =>
      (session.user?.role === 'superadmin' || session.user?.tenant_role === 'owner') &&
      runtime.value?.can_manage === true &&
      capabilities.value?.can_manage === true
  )
  const permissions = computed<Row[]>(() => {
    const catalog = new Map<string, Row>(
      (capabilities.value?.permission_catalog || []).map((item: Row) => [item.code, item])
    )
    return (capabilities.value?.permissions || []).map((code: string) => ({
      code,
      label: catalog.get(code)?.label || code
    }))
  })
  const actions = computed<Row[]>(() => capabilities.value?.actions || [])
  const managementPending = computed(
    () => runtime.value?.enabled === true && runtime.value.management_enabled === false
  )
  let alive = true,
    sequence = 0,
    initialized = false
  const current = (request: number) =>
    alive &&
    sequence === request &&
    session.user?.id === actor &&
    session.user?.tenant_id === tenant
  async function load(preserve = false) {
    const request = ++sequence
    loading.value = true
    error.value = ''
    try {
      const [c, r] = await Promise.all([
        api<Row>(`/groups/${id}/capabilities`, 'GET', undefined, undefined, platform),
        api<Row>(`/groups/${id}/business-runtime`, 'GET', undefined, undefined, platform)
      ])
      if (!current(request)) return
      capabilities.value = c
      runtime.value = r
      uncertain.value = false
      if (!preserve) {
        const draft = session.drafts[scope] as Row | undefined
        enabled.value = typeof draft?.enabled === 'boolean' ? draft.enabled : r.enabled
        conflict.value = !!draft && draft.version !== r.version
      } else conflict.value = enabled.value !== r.enabled
      initialized = true
    } catch (e) {
      if (current(request)) error.value = (e as Error).message
    } finally {
      if (current(request)) loading.value = false
    }
  }
  async function save() {
    if (!canManage.value || !runtime.value || saving.value || uncertain.value) return
    const request = ++sequence,
      version = runtime.value.version
    let saved = false
    saving.value = true
    error.value = ''
    try {
      const next = await api<Row>(
        `/groups/${id}/business-runtime`,
        'PUT',
        { version, enabled: enabled.value },
        undefined,
        platform
      )
      if (!current(request)) return
      runtime.value = { ...runtime.value, ...next }
      enabled.value = next.enabled
      conflict.value = false
      session.saveDraft(scope, {})
      emit('saved')
      saved = true
    } catch (e) {
      if (!current(request)) return
      error.value = (e as Error).message
      conflict.value = e instanceof ApiError && e.status === 409
      uncertain.value = !(e instanceof ApiError)
    } finally {
      if (current(request)) saving.value = false
    }
    if (saved && current(request)) await load(true)
  }
  watch(enabled, (value) => {
    if (initialized && session.user?.id === actor)
      session.saveDraft(scope, { enabled: value, version: runtime.value?.version })
  })
  void load()
  onBeforeUnmount(() => {
    alive = false
    sequence++
  })
</script>
<style scoped>
  .group-business-heading {
    display: flex;
    gap: 12px;
    align-items: flex-start;
    padding-bottom: 20px;
  }
  .group-business-heading > div {
    flex: 1;
    min-width: 0;
  }
  .group-business-heading strong {
    font-size: 16px;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .group-business-heading p,
  .group-runtime-toggle p {
    color: var(--el-text-color-secondary);
    font-size: 13px;
    margin: 6px 0 0;
  }
  .group-runtime-status {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
    padding-block: 4px 16px;
  }
  .group-runtime-status > span {
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
  .group-runtime-section {
    padding: 20px 0;
    border-top: 1px solid var(--el-border-color-light);
  }
  .group-runtime-section h3 {
    margin: 0 0 12px;
    font-size: 14px;
  }
  .group-runtime-toggle {
    display: flex;
    gap: 20px;
    align-items: center;
    justify-content: space-between;
  }
  .group-runtime-toggle h3 {
    margin: 0;
  }
  .group-runtime-toggle > div {
    min-width: 0;
  }
  .group-runtime-toggle .el-switch {
    flex-shrink: 0;
  }
  .runtime-permissions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    padding-bottom: 16px;
  }
  .runtime-action-row {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    align-items: center;
    gap: 8px;
    padding: 12px 0;
    border-top: 1px solid var(--el-border-color-lighter);
  }
  .runtime-action-row small {
    grid-column: 1 / -1;
    color: var(--el-text-color-secondary);
    overflow-wrap: anywhere;
  }
  .runtime-conflict {
    color: var(--el-color-warning);
    font-size: 13px;
  }
  .el-alert {
    margin-bottom: 16px;
  }
</style>
