<template>
  <ElDialog
    :model-value="true"
    :title="`权限上限 · ${tenant.tenant_name || tenant.username || '客户'}`"
    width="740px"
    class="customer-scope-dialog tenant-capabilities-dialog"
    append-to-body
    align-center
    :close-on-click-modal="false"
    @close="$emit('close')"
  >
    <ElTabs v-model="tab"
      ><ElTabPane label="客户权限上限" name="ceiling" /><ElTabPane
        label="正式业务准入"
        name="gates"
    /></ElTabs>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <template v-if="tab === 'ceiling'">
      <div class="capability-section-heading"
        ><p>负责人只能在此范围内为成员分配权限。</p
        ><ElButton
          text
          :icon="Refresh"
          :loading="loading"
          :disabled="saving"
          aria-label="刷新客户权限上限"
          @click="loadCeiling(true)"
      /></div>
      <ElSkeleton v-if="!ceiling && loading" :rows="6" animated />
      <ElAlert
        v-if="conflict"
        title="上限已变化，草稿已保留。请刷新后核对再保存。"
        type="warning"
        :closable="false"
      />
      <ElCheckboxGroup
        v-if="ceiling"
        v-model="selectedPermissions"
        :disabled="saving || uncertain"
        class="capability-checkboxes"
      >
        <section v-for="section in permissionGroups" :key="section.name"
          ><h3>{{ section.name }}</h3
          ><ElCheckbox v-for="item in section.items" :key="item.code" :value="item.code">{{
            item.label
          }}</ElCheckbox></section
        >
      </ElCheckboxGroup>
    </template>
    <template v-else>
      <div class="capability-gate-scope">
        <ElSelect v-model="gatePlatform" aria-label="准入平台" :disabled="saving"
          ><ElOption
            v-for="p in customerPlatforms"
            :key="p.value"
            :value="p.value"
            :label="p.label"
        /></ElSelect>
        <ElSelect
          v-model="gateAction"
          aria-label="准入动作"
          placeholder="选择动作"
          :disabled="saving"
          ><ElOption
            v-for="action in catalog?.actions || []"
            :key="action.id"
            :value="action.id"
            :label="action.name"
        /></ElSelect>
        <ElSelect
          v-model="gateSource"
          aria-label="准入来源"
          placeholder="选择来源"
          :disabled="saving"
          ><ElOption
            v-for="source in selectedAction?.sources || []"
            :key="source"
            :value="source"
            :label="sourceLabels[source] || source"
        /></ElSelect>
        <ElButton
          :icon="Refresh"
          :loading="gateLoading"
          :disabled="!gateAction || !gateSource || saving"
          @click="loadGate"
          >读取准入</ElButton
        >
      </div>
      <ElAlert
        v-if="!gate"
        title="选择平台、动作与来源后读取准入记录"
        type="info"
        :closable="false"
      />
      <ElForm v-else label-position="top" @submit.prevent="saveGate">
        <div class="gate-record"
          ><ElTag type="info">{{ selectedAction?.name }}</ElTag
          ><span>{{ sourceLabels[gateSource] || gateSource }} · 版本 {{ gate.version }}</span></div
        >
        <ElFormItem label="准入范围"
          ><ElSelect
            v-model="gateState"
            :disabled="saving || uncertain || gateLoading"
            aria-label="业务准入范围"
            ><ElOption label="关闭" value="closed" /><ElOption
              label="指定群灰度"
              value="pilot"
              :disabled="!selectedAction?.implemented" /><ElOption
              label="开放"
              value="open"
              :disabled="!selectedAction?.implemented" /></ElSelect
        ></ElFormItem>
        <ElFormItem v-if="gateState === 'pilot'" label="灰度群组"
          ><ElSelect
            v-model="pilotGroups"
            multiple
            filterable
            collapse-tags
            collapse-tags-tooltip
            :max-collapse-tags="3"
            :disabled="saving || uncertain || gateLoading"
            aria-label="灰度群组"
            ><ElOption
              v-for="group in boundGroups"
              :key="group.group_id"
              :value="group.group_id"
              :label="`${group.name} · ${robotLabel(group)}`" /></ElSelect
        ></ElFormItem>
        <ElFormItem label="验收证据引用"
          ><ElInput
            v-model="evidenceText"
            type="textarea"
            :rows="3"
            :disabled="saving || uncertain || gateLoading"
            aria-label="验收证据引用"
            placeholder="每行一个 docs/ 文档路径或 acceptance:验收编号"
            maxlength="4112"
        /></ElFormItem>
        <p v-if="!selectedAction?.implemented" class="capability-note"
          >本平台尚未实现此能力，保持关闭。</p
        >
        <p class="capability-note"
          >此处维护平台准入，对本平台相同动作与来源的客户共同生效；客户权限上限、套餐和群授权仍独立校验。</p
        >
        <ElAlert
          v-if="gateConflict"
          title="准入记录已变化，草稿已保留，请重新读取后核对。"
          type="warning"
          :closable="false"
        />
      </ElForm>
    </template>
    <template #footer
      ><ElButton :disabled="saving" @click="$emit('close')">关闭</ElButton
      ><ElButton
        v-if="tab === 'ceiling'"
        type="primary"
        :icon="Check"
        :loading="saving"
        :disabled="!ceiling || loading || uncertain"
        @click="saveCeiling"
        >保存权限上限</ElButton
      ><ElButton
        v-else
        type="primary"
        :icon="Check"
        :loading="saving"
        :disabled="
          !gate ||
          gateLoading ||
          uncertain ||
          (gateState === 'pilot' && !pilotGroups.length) ||
          (gateState !== 'closed' && !evidenceRefs.length)
        "
        @click="gateConfirm = true"
        >保存准入</ElButton
      ></template
    >
  </ElDialog>
  <ElDialog
    v-model="gateConfirm"
    title="确认正式业务准入"
    width="460px"
    class="customer-scope-dialog"
    append-to-body
    align-center
    :close-on-click-modal="false"
  >
    <dl class="gate-confirm-details"
      ><dt>平台</dt><dd>{{ platformLabel(gatePlatform) }}</dd
      ><dt>范围</dt><dd>平台级准入</dd><dt>动作</dt><dd>{{ selectedAction?.name }}</dd
      ><dt>来源</dt><dd>{{ sourceLabels[gateSource] || gateSource }}</dd
      ><dt>准入</dt
      ><dd>{{
        gateState === 'pilot'
          ? `指定 ${pilotGroups.length} 个群灰度`
          : gateState === 'open'
            ? '开放'
            : '关闭'
      }}</dd></dl
    >
    <template #footer
      ><ElButton :disabled="saving" @click="gateConfirm = false">取消</ElButton
      ><ElButton type="primary" :loading="saving" @click="saveGate"
        >确认保存准入</ElButton
      ></template
    >
  </ElDialog>
</template>
<script setup lang="ts">
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import {
    ElAlert,
    ElButton,
    ElCheckbox,
    ElCheckboxGroup,
    ElDialog,
    ElForm,
    ElFormItem,
    ElInput,
    ElOption,
    ElSelect,
    ElSkeleton,
    ElTabPane,
    ElTabs,
    ElTag
  } from 'element-plus'
  import { Check, Refresh } from '@element-plus/icons-vue'
  import { api, ApiError, type Row } from './api'
  import { useSession } from './store'
  import {
    customerPlatforms,
    platformLabel,
    robotLabel,
    sourceLabels,
    type CustomerPlatform
  } from './customer'
  const props = defineProps<{ tenant: Row; groups?: Row[] }>()
  const emit = defineEmits<{ close: []; saved: [] }>()
  const session = useSession(),
    actor = session.user?.id
  const tenant = { ...props.tenant },
    tenantID = String(tenant.tenant_id || tenant.id || '')
  const tab = ref('ceiling'),
    error = ref(''),
    loading = ref(false),
    saving = ref(false),
    uncertain = ref(false),
    conflict = ref(false)
  const ceiling = ref<Row>(),
    selectedPermissions = ref<string[]>([])
  const gatePlatform = ref<CustomerPlatform>(
    session.workspace === 'lajiaoliao' ? 'lajiaoliao' : 'wsl'
  )
  const catalog = ref<Row>(),
    gateAction = ref(''),
    gateSource = ref(''),
    gate = ref<Row>()
  const gateLoading = ref(false),
    gateConflict = ref(false),
    gateConfirm = ref(false),
    gateState = ref('closed'),
    pilotGroups = ref<string[]>([])
  const evidenceText = ref(''),
    evidenceRefs = computed(() =>
      evidenceText.value
        .split(/\r?\n/)
        .map((value) => value.trim())
        .filter(Boolean)
    )
  const loadedGroups = ref<Row[]>([])
  const selectedAction = computed(() =>
    catalog.value?.actions?.find((item: Row) => item.id === gateAction.value)
  )
  const boundGroups = computed<Row[]>(() => [
    ...new Map(
      [...(props.groups || []), ...loadedGroups.value]
        .filter((group) => group.active && group.platform === gatePlatform.value)
        .map((group) => [group.group_id, group])
    ).values()
  ])
  const scope = `customer:${actor}:${tenantID}:capability-ceiling`
  const gateScope = computed(
    () =>
      `customer:${actor}:${gatePlatform.value}:${gateSource.value}:${gateAction.value}:business-gate`
  )
  const permissionGroups = computed(() => {
    const sections = new Map<string, Row[]>()
    const labels: Record<string, string> = {
      moderation: '群规则与处置',
      replies: '自动回复',
      management_assistant: '群管助手',
      management: '群配置',
      ai: '热群机器人',
      finance: '财务',
      activities: '活动'
    }
    for (const item of ceiling.value?.permission_catalog || []) {
      const label = item.code.startsWith('group_action:')
        ? '原生动作上限'
        : labels[item.module] || '客户工作空间'
      if (!sections.has(label)) sections.set(label, [])
      sections.get(label)!.push(item)
    }
    return [...sections].map(([name, items]) => ({ name, items }))
  })
  let alive = true,
    ceilingRequest = 0,
    catalogRequest = 0,
    gateRequest = 0,
    initialized = false,
    gateInitialized = false
  const current = () => alive && session.user?.id === actor && session.user?.role === 'superadmin'
  async function loadCeiling(preserve = false) {
    if (!current()) return
    const request = ++ceilingRequest
    loading.value = true
    error.value = ''
    try {
      const result = await api<Row>(`/tenants/${tenantID}/capabilities`)
      if (!current() || request !== ceilingRequest) return
      ceiling.value = result
      uncertain.value = false
      if (!preserve) {
        const draft = session.drafts[scope] as Row | undefined
        selectedPermissions.value = [...(draft?.permissions || result.permissions || [])]
        conflict.value = !!draft?.permissions && draft.version !== result.version
      }
      initialized = true
    } catch (e) {
      if (current() && request === ceilingRequest) error.value = (e as Error).message
    } finally {
      if (request === ceilingRequest) loading.value = false
    }
  }
  async function loadCatalog() {
    const request = ++catalogRequest,
      platform = gatePlatform.value
    try {
      const result = await api<Row>('/capabilities/catalog', 'GET', undefined, undefined, platform)
      if (!current() || request !== catalogRequest || platform !== gatePlatform.value) return
      catalog.value = result
      if (!result.actions.some((item: Row) => item.id === gateAction.value))
        gateAction.value = result.actions[0]?.id || ''
    } catch (e) {
      if (current() && request === catalogRequest) error.value = (e as Error).message
    }
  }
  async function loadGate() {
    if (!current() || !gateAction.value || !gateSource.value || saving.value) return
    const request = ++gateRequest,
      platform = gatePlatform.value,
      scopeKey = gateScope.value
    gateLoading.value = true
    error.value = ''
    try {
      const [result, groups] = await Promise.all([
        api<Row>(
          `/capabilities/gates/${gateAction.value}/${gateSource.value}`,
          'GET',
          undefined,
          undefined,
          platform
        ),
        api<Row[]>('/groups', 'GET', undefined, undefined, platform)
      ])
      if (!current() || request !== gateRequest || scopeKey !== gateScope.value) return
      gate.value = result
      loadedGroups.value = groups
      uncertain.value = false
      const draft = session.drafts[scopeKey] as Row | undefined
      gateState.value = draft?.state || result.state
      pilotGroups.value = [...(draft?.pilot_group_ids || result.pilot_group_ids || [])]
      evidenceText.value = (draft?.evidence_refs || result.evidence_refs || []).join('\n')
      gateConflict.value = !!draft?.state && draft.version !== result.version
      gateInitialized = true
    } catch (e) {
      if (current() && request === gateRequest) error.value = (e as Error).message
    } finally {
      if (request === gateRequest) gateLoading.value = false
    }
  }
  async function saveCeiling() {
    if (!current() || !ceiling.value || saving.value || uncertain.value) return
    const request = ++ceilingRequest
    saving.value = true
    error.value = ''
    try {
      const result = await api<Row>(`/tenants/${tenantID}/capabilities`, 'PUT', {
        permissions: selectedPermissions.value,
        version: ceiling.value.version
      })
      if (!current() || request !== ceilingRequest) return
      ceiling.value = { ...ceiling.value, ...result }
      session.saveDraft(scope, {})
      conflict.value = false
      emit('saved')
    } catch (e) {
      if (!current() || request !== ceilingRequest) return
      error.value = (e as Error).message
      conflict.value = e instanceof ApiError && e.status === 409
      uncertain.value = !(e instanceof ApiError)
    } finally {
      if (request === ceilingRequest) saving.value = false
    }
  }
  async function saveGate() {
    if (!current() || !gate.value || saving.value || uncertain.value) return
    const request = ++gateRequest,
      platform = gatePlatform.value,
      scopeKey = gateScope.value
    saving.value = true
    error.value = ''
    try {
      const result = await api<Row>(
        `/capabilities/gates/${gateAction.value}/${gateSource.value}`,
        'PUT',
        {
          state: gateState.value,
          version: gate.value.version,
          pilot_group_ids: gateState.value === 'pilot' ? pilotGroups.value : [],
          evidence_refs: evidenceRefs.value
        },
        undefined,
        platform
      )
      if (!current() || request !== gateRequest || scopeKey !== gateScope.value) return
      gate.value = result
      gateConfirm.value = false
      gateConflict.value = false
      session.saveDraft(scopeKey, {})
      emit('saved')
    } catch (e) {
      if (!current() || request !== gateRequest) return
      gateConfirm.value = false
      error.value = (e as Error).message
      gateConflict.value = e instanceof ApiError && e.status === 409
      uncertain.value = !(e instanceof ApiError)
    } finally {
      if (request === gateRequest) saving.value = false
    }
  }
  watch(
    selectedPermissions,
    () => {
      if (initialized && current())
        session.saveDraft(scope, {
          permissions: selectedPermissions.value,
          version: ceiling.value?.version
        })
    },
    { deep: true }
  )
  watch(
    [gateState, pilotGroups, evidenceText],
    () => {
      if (gateInitialized && current())
        session.saveDraft(gateScope.value, {
          state: gateState.value,
          pilot_group_ids: pilotGroups.value,
          evidence_refs: evidenceRefs.value,
          version: gate.value?.version
        })
    },
    { deep: true }
  )
  watch([gatePlatform, gateAction, gateSource], () => {
    gateRequest++
    gate.value = undefined
    gateLoading.value = false
    gateConflict.value = false
    gateInitialized = false
    uncertain.value = false
  })
  watch(gatePlatform, () => {
    catalog.value = undefined
    void loadCatalog()
  })
  watch(selectedAction, (action) => {
    if (!action?.sources?.includes(gateSource.value)) gateSource.value = action?.sources?.[0] || ''
  })
  watch(tab, (value) => {
    if (value === 'gates' && !catalog.value) void loadCatalog()
  })
  void loadCeiling()
  onBeforeUnmount(() => {
    alive = false
    ceilingRequest++
    catalogRequest++
    gateRequest++
  })
</script>
<style scoped>
  .capability-section-heading {
    display: flex;
    align-items: center;
    gap: 12px;
    padding-bottom: 12px;
  }
  .capability-section-heading p {
    flex: 1;
    color: var(--el-text-color-secondary);
    font-size: 13px;
  }
  .capability-checkboxes {
    display: flex;
    flex-direction: column;
  }
  .capability-checkboxes section {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 4px 16px;
    padding-block: 16px;
    border-bottom: 1px solid var(--el-border-color-light);
  }
  .capability-checkboxes h3 {
    grid-column: 1 / -1;
    font-size: 14px;
    margin: 0 0 8px;
  }
  .capability-checkboxes :deep(.el-checkbox) {
    margin: 0;
    height: auto;
    min-height: 36px;
    align-items: center;
  }
  .capability-checkboxes :deep(.el-checkbox__label) {
    white-space: normal;
    overflow-wrap: anywhere;
  }
  .capability-gate-scope {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
    gap: 12px;
    padding: 16px 0 24px;
  }
  .capability-gate-scope > .el-button {
    margin: 0;
    justify-self: start;
  }
  .gate-record {
    display: flex;
    gap: 12px;
    flex-wrap: wrap;
    align-items: center;
    color: var(--el-text-color-secondary);
    font-size: 12px;
    margin-bottom: 24px;
  }
  .capability-note {
    color: var(--el-text-color-secondary);
    font-size: 13px;
  }
  .gate-confirm-details {
    display: grid;
    grid-template-columns: 64px minmax(0, 1fr);
    gap: 12px;
  }
  .gate-confirm-details dt {
    color: var(--el-text-color-secondary);
  }
  .gate-confirm-details dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .el-alert {
    margin-bottom: 16px;
  }
  @media (max-width: 480px) {
    .capability-checkboxes section,
    .capability-gate-scope {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
