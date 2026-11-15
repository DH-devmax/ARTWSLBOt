<template>
  <section class="feature-center" data-testid="feature-center" v-loading="loading">
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElAlert v-if="draftNotice" :title="draftNotice" type="warning" :closable="false" />
    <NativeCapabilities
      :items="capabilities"
      :details="nativeDetails"
      :acceptance="acceptance"
      :demonstrations="demonstrations"
      :usage="nativeUsage"
      :audience="view === 'manual' ? 'manual' : 'foundation'"
      :modules="rows.map((row) => row.definition)"
      :visible="view === 'native' || view === 'manual'"
      :scope="scope"
      :account="account"
      :groups="groups"
      v-model:selected="capabilityID"
      @tools="emit('detail', 'tests')"
    />
    <template v-if="view === 'modules'">
      <div class="feature-health" aria-label="业务功能概况">
        <div
          ><span>可见业务功能</span><strong>{{ rows.length }}</strong></div
        >
        <div
          ><span>套餐包含</span
          ><strong>{{
            groupID ? rows.filter((row) => row.entitled).length : '未选择群'
          }}</strong></div
        >
        <div
          ><span>已保存启用</span
          ><strong>{{
            base ? rows.filter((row) => row.values?.enabled === true).length : '未读取'
          }}</strong></div
        >
        <div
          ><span>实际链路启用</span
          ><strong>{{
            base ? rows.filter((row) => row.effective_enabled).length : '未读取'
          }}</strong></div
        >
      </div>
      <div class="feature-toolbar">
        <ElSelect v-model="groupID" aria-label="功能配置群组" placeholder="选择授权群组" filterable>
          <ElOption
            v-for="group in groups"
            :key="group.group_id"
            :label="group.name"
            :value="group.group_id"
          />
        </ElSelect>
        <ElInput
          v-model="search"
          aria-label="搜索功能"
          placeholder="搜索功能"
          clearable
          :prefix-icon="Search"
        />
        <ElSelect v-model="category" aria-label="业务模块分类" placeholder="全部分类">
          <ElOption label="全部分类" value="" />
          <ElOption v-for="name in categories" :key="name" :label="name" :value="name" />
        </ElSelect>
        <ElButton :icon="Refresh" aria-label="重新读取功能配置" :disabled="saving" @click="load"
          >刷新</ElButton
        >
      </div>
      <div class="feature-meta">
        <span>{{ selectedGroup?.name || '尚无授权群组' }}</span>
        <span v-if="base">配置版本 {{ base.version }}<span v-if="dirty"> · 未保存</span></span>
      </div>
      <ElAlert
        v-if="base && draftVersion !== base.version"
        title="配置版本已变化，草稿已保留。核对当前输入后才能采用最新版本。"
        type="warning"
        :closable="false"
      />
      <ElAlert v-if="saved" title="配置已保存；原生执行尚未开放" type="success" :closable="false" />
      <div class="feature-table-wrap">
        <ElTable :data="visibleRows" row-key="definition.id" empty-text="无匹配功能">
          <ElTableColumn label="功能" min-width="190">
            <template #default="{ row }"
              ><strong>{{ row.definition.name }}</strong
              ><div class="feature-category"
                >业务模块 · {{ row.definition.category }}</div
              ></template
            >
          </ElTableColumn>
          <ElTableColumn label="依赖原生功能" min-width="255">
            <template #default="{ row }">
              <div v-if="row.definition.native_dependencies?.length" class="dependency-links">
                <ElButton
                  v-for="id in row.definition.native_dependencies"
                  :key="id"
                  link
                  type="primary"
                  :aria-label="`${row.definition.name}依赖${capabilityName(id)}`"
                  @click="capabilityID = id"
                  >{{ capabilityName(id) }}</ElButton
                >
              </div>
              <span v-else>无</span>
            </template>
          </ElTableColumn>
          <ElTableColumn label="启用配置" width="100" align="center">
            <template #default="{ row }">
              <ElTag
                v-if="hasField(row, 'enabled')"
                :type="row.values.enabled ? 'success' : 'info'"
                >{{ row.values.enabled ? '已开启' : '已关闭' }}</ElTag
              >
              <span v-else>无</span>
              <div v-if="changes[row.definition.id]" class="feature-category">有草稿</div>
            </template>
          </ElTableColumn>
          <ElTableColumn label="运行状态" min-width="175"
            ><template #default="{ row }">
              <ElTag :type="row.definition.status === 'web_available' ? 'success' : 'info'">{{
                statusLabel(row.definition.status)
              }}</ElTag>
              <div v-if="row.definition.native_dependencies?.length" class="feature-category">
                {{ row.effective_enabled ? '原生链路已启用' : '原生链路未开放' }}
              </div>
              <div v-if="groupID && !row.entitled" class="feature-category">套餐未包含</div>
            </template></ElTableColumn
          >
          <ElTableColumn label="配置 / 管理" width="120">
            <template #default="{ row }">
              <ElButton
                v-if="(row.definition.fields || []).length"
                link
                type="primary"
                :disabled="!base"
                @click="detailID = row.definition.id"
                >详情配置</ElButton
              >
              <ElButton
                v-else-if="row.definition.detail && detailAllowed(row)"
                link
                type="primary"
                @click="openDetail(row)"
                >进入管理</ElButton
              >
              <ElButton v-else link type="primary" @click="detailID = row.definition.id"
                >查看详情</ElButton
              >
            </template>
          </ElTableColumn>
        </ElTable>
      </div>
    </template>
    <ElDialog
      :model-value="!!detail"
      :title="detail?.definition.name || '详情配置'"
      class="platform-config-dialog feature-detail"
      style="--el-component-size: 36px; --el-border-radius-base: 8px; border-radius: 8px"
      align-center
      width="620px"
      :close-on-click-modal="false"
      :close-on-press-escape="!saving"
      :show-close="!saving"
      @close="detailID = ''"
    >
      <ElAlert v-if="error" :title="error" type="error" :closable="false" />
      <ElAlert v-if="saved" title="配置已保存；原生执行尚未开放" type="success" :closable="false" />
      <template v-if="base && draftVersion !== base.version">
        <ElAlert
          title="配置版本已变化，草稿已保留。核对当前输入后才能采用最新版本。"
          type="warning"
          :closable="false"
        />
        <ElButton @click="draftVersion = base.version"
          >已核对，采用版本 {{ base.version }}</ElButton
        >
      </template>
      <div v-if="detail" class="feature-detail-content">
        <div class="feature-meta">
          <span>{{ selectedGroup?.name }}</span>
          <span v-if="base">配置版本 {{ base.version }}</span>
        </div>
        <p class="feature-description">{{ detail.definition.summary }}</p>
        <div class="feature-dependencies">
          <span>依赖原生功能</span>
          <div v-if="detail.definition.native_dependencies?.length" class="dependency-links">
            <ElButton
              v-for="id in detail.definition.native_dependencies"
              :key="id"
              link
              type="primary"
              @click="inspectCapability(id)"
              >{{ capabilityName(id) }}</ElButton
            >
          </div>
          <span v-else>无</span>
        </div>
        <ElForm v-if="detail.definition.fields?.length" label-position="top" @submit.prevent>
          <template v-for="field in detail.definition.fields" :key="field.key">
            <ElFormItem :label="field.label">
              <ElCheckbox
                v-if="field.type === 'boolean'"
                v-model="draft[detailID][field.key]"
                :disabled="!editable(detail, field.key)"
                :aria-label="field.label"
                >{{ field.label }}</ElCheckbox
              >
              <ElInput
                v-else-if="field.type === 'string_list'"
                v-model="draft[detailID][field.key]"
                type="textarea"
                :rows="7"
                :aria-label="field.label"
                :disabled="!editable(detail, field.key)"
              />
              <ElInputNumber
                v-else-if="field.type === 'integer'"
                v-model="draft[detailID][field.key]"
                :aria-label="field.label"
                :min="detailID === 'keyword_replies' && field.key === 'cooldown_seconds' ? 30 : 0"
                :max="field.limit"
                :precision="0"
                :step="1"
                :disabled="!editable(detail, field.key)"
              />
              <BusinessRuleEditor
                v-else-if="field.type === 'keyword_rules' || field.type === 'blocked_rules'"
                v-model="draft[detailID][field.key]"
                :type="field.type"
                :limit="field.limit"
                :disabled="!editable(detail, field.key)"
              />
            </ElFormItem>
          </template>
        </ElForm>
        <ElAlert
          v-if="detailIssue && !previewAvailable"
          :title="detailIssue"
          type="warning"
          :closable="false"
        />
        <FeatureRulePreview
          v-if="previewAvailable"
          :key="`${scope}:${groupID}:${detailID}`"
          :result="previewResult"
          :names="featureNames"
          :loading="previewLoading"
          :disabled="loading || saving"
          :reason="previewReason"
          :error="previewError"
          @invalidate="clearPreview"
          @preview="preview"
        />
      </div>
      <template #footer>
        <div class="feature-detail-actions">
          <ElButton :disabled="saving" @click="detailID = ''">返回功能列表</ElButton>
          <ElButton v-if="changes[detailID]" :disabled="saving" @click="discard">放弃修改</ElButton>
          <ElButton
            v-if="detail?.editable_fields?.length"
            type="primary"
            :icon="Check"
            :loading="saving"
            :disabled="
              !changes[detailID] ||
              !!detailIssue ||
              loading ||
              !base ||
              draftVersion !== base.version
            "
            @click="save"
            >保存配置</ElButton
          >
        </div>
      </template>
    </ElDialog>
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import { Check, Refresh, Search } from '@element-plus/icons-vue'
  import { ElInputNumber } from 'element-plus'
  import { api, type Row } from './api'
  import { useSession } from './store'
  import NativeCapabilities from './NativeCapabilities.vue'
  import BusinessRuleEditor from './BusinessRuleEditor.vue'
  import FeatureRulePreview from './FeatureRulePreview.vue'
  import type { ControlledAcceptance, NativeCapability, NativeDetail } from './featureCatalog'

  const props = defineProps<{
    groups: Row[]
    account: string
    initialGroup?: string
    view: 'native' | 'manual' | 'modules'
  }>()
  const emit = defineEmits<{
    detail: [page: string, group?: string]
    ai: [group: Row]
    updated: []
    selected: [group: string]
  }>()
  const session = useSession()
  const groupID = ref(''),
    search = ref(''),
    category = ref(''),
    capabilityID = ref(''),
    detailID = ref(''),
    error = ref(''),
    draftNotice = ref('')
  const loading = ref(false),
    saving = ref(false),
    saved = ref(false)
  const base = ref<Row | null>(null),
    capabilities = ref<NativeCapability[]>([]),
    nativeDetails = ref<NativeDetail[]>([]),
    nativeUsage = ref<Record<string, string>>({}),
    acceptance = ref<Record<string, ControlledAcceptance>>({}),
    demonstrations = ref<Record<string, ControlledAcceptance>>({}),
    rows = ref<Row[]>([]),
    draft = ref<Record<string, Row>>({})
  const draftVersion = ref(0)
  const previewResult = ref<Row | null>(null),
    previewLoading = ref(false),
    previewError = ref(''),
    draftRevision = ref(0)
  let previewSequence = 0,
    previewController: AbortController | undefined
  let generation = 0,
    activeScope = '',
    activeActor = '',
    controller: AbortController | undefined
  const actor = computed(() => `${session.user?.id}:${session.user?.tenant_id || 'platform'}`)
  const scope = computed(
    () =>
      `${session.workspace}:${session.user?.id}:${session.user?.tenant_id || 'platform'}:${props.account}:features`
  )
  const selectedGroup = computed(() =>
    props.groups.find((group) => group.group_id === groupID.value)
  )
  const categories = computed(() => [...new Set(rows.value.map((row) => row.definition.category))])
  const capabilityName = (id: string) =>
    capabilities.value.find((item) => item.id === id)?.name || '待核验能力'
  function inspectCapability(id: string) {
    detailID.value = ''
    capabilityID.value = id
  }
  const visibleRows = computed(() =>
    rows.value.filter(
      (row) =>
        (!category.value || row.definition.category === category.value) &&
        `${row.definition.name} ${row.definition.category} ${(row.definition.native_dependencies || []).map(capabilityName).join(' ')}`.includes(
          search.value.trim()
        )
    )
  )
  const detail = computed(() => rows.value.find((row) => row.definition.id === detailID.value))
  const equal = (left: unknown, right: unknown) => JSON.stringify(left) === JSON.stringify(right)
  const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value))
  const changes = computed(() => {
    const out: Record<string, Row> = {}
    if (!base.value) return out
    for (const row of rows.value)
      for (const field of row.definition.fields || []) {
        const id = row.definition.id
        const value =
          field.type === 'string_list'
            ? lines(draft.value[id]?.[field.key] || '')
            : draft.value[id]?.[field.key]
        if (draft.value[id] && !sameFieldValue(field.type, value, row.values[field.key])) {
          ;(out[id] ||= {})[field.key] = value
        }
      }
    return out
  })
  const dirty = computed(() => Object.keys(changes.value).length > 0)
  function sameFieldValue(type: string, left: unknown, right: unknown) {
    const comparable = (value: unknown) => {
      if (!Array.isArray(value)) return value
      if (type === 'keyword_rules')
        return value.map((rule) => ({
          keyword: rule.keyword,
          reply: rule.reply,
          match: rule.match || 'contains',
          disabled: rule.disabled === true
        }))
      if (type === 'blocked_rules')
        return value.map((rule) => ({
          name: rule.name,
          words: rule.words,
          disabled: rule.disabled === true
        }))
      return value
    }
    return equal(comparable(left), comparable(right))
  }
  const featureNames = computed(
    () =>
      Object.fromEntries(
        rows.value.map((row) => [row.definition.id, row.definition.name])
      ) as Record<string, string>
  )
  const previewAvailable = computed(
    () =>
      !!base.value &&
      !!detail.value &&
      ['blocked_words', 'blacklist', 'exemptions', 'keyword_replies'].includes(detailID.value)
  )
  const detailIssue = computed(() => {
    if (!detail.value) return ''
    const fields = changes.value[detailID.value] || {}
    if (Object.keys(fields).some((key) => !detail.value!.editable_fields?.includes(key)))
      return '草稿包含当前无权修改的字段，请放弃修改后试算已保存配置'
    return validateFields(detail.value, fields)
  })
  const previewReason = computed(() =>
    draftVersion.value !== base.value?.version
      ? '配置版本已变化，请核对并采用最新版本后试算'
      : detailIssue.value
  )
  const previewContext = computed(() =>
    JSON.stringify([
      scope.value,
      groupID.value,
      draftVersion.value,
      base.value?.version,
      detailID.value,
      draftRevision.value,
      session.permissions,
      props.view,
      loading.value,
      saving.value
    ])
  )
  const hasField = (row: Row, field: string) =>
    (row.definition.fields || []).some((item: Row) => item.key === field)
  const editable = (row: Row, field: string) =>
    !loading.value && !saving.value && (row.editable_fields || []).includes(field)
  const lines = (value: string) =>
    value
      .split('\n')
      .map((line) => line.trim())
      .filter(Boolean)
  const statusLabel = (status: string) =>
    ({
      native_pending: '仅配置 · 待接入',
      web_available: 'Web 基础管理可用',
      request_only: '仅保存申请',
      implemented: '已实现 · 运行状态见群配置',
      unavailable: '待迁移'
    })[status] || '无'
  function detailAllowed(row: Row) {
    return session.permissions.includes(
      (
        { activities: 'activities:read', ledger: 'ledger:read', 'group-ai': 'groups:ai' } as Record<
          string,
          string
        >
      )[row.definition.detail]
    )
  }
  function openDetail(row: Row) {
    if (row.definition.detail === 'group-ai') {
      if (selectedGroup.value) emit('ai', selectedGroup.value)
    } else emit('detail', row.definition.detail, groupID.value)
  }
  function draftSnapshot() {
    if (!dirty.value) return null
    const baseline: Record<string, Row> = {}
    for (const row of rows.value) {
      const fields = changes.value[row.definition.id]
      if (fields)
        baseline[row.definition.id] = Object.fromEntries(
          Object.keys(fields).map((key) => [key, row.values[key]])
        )
    }
    return clone({ version: draftVersion.value, values: changes.value, baseline })
  }
  function persist() {
    if (activeScope && base.value && activeActor === actor.value)
      session.saveDraft(activeScope, draftSnapshot())
  }
  async function load() {
    persist()
    const current = ++generation,
      id = groupID.value,
      workspace = session.workspace,
      scopeAtStart = scope.value
    controller?.abort()
    controller = new AbortController()
    detailID.value = ''
    capabilityID.value = ''
    base.value = null
    capabilities.value = []
    nativeDetails.value = []
    demonstrations.value = {}
    nativeUsage.value = {}
    acceptance.value = {}
    activeScope = ''
    activeActor = ''
    rows.value = []
    draft.value = {}
    error.value = ''
    draftNotice.value = ''
    saved.value = false
    loading.value = true
    saving.value = false
    try {
      const result = await api<Row>(
        id ? `/groups/${id}/features` : '/features/catalog',
        'GET',
        undefined,
        controller.signal,
        workspace
      )
      if (current !== generation || scopeAtStart !== scope.value || id !== groupID.value) return
      if (result.platform && result.platform !== workspace)
        throw new Error('功能目录平台与当前工作台不一致')
      if (id && (result.group_id !== id || (props.account && result.account_id !== props.account)))
        throw new Error('群组不属于当前机器人面板')
      capabilities.value = result.native_capabilities || []
      nativeDetails.value = result.native_details || []
      nativeUsage.value = result.native_usage || {}
      acceptance.value = result.native_acceptance || {}
      demonstrations.value = result.native_demonstrations || {}
      rows.value = id
        ? result.items
        : (result.items || []).map((definition: Row) => ({
            definition,
            values: {},
            editable_fields: []
          }))
      draft.value = Object.fromEntries(
        rows.value.map((row) => {
          const values = clone(row.values)
          for (const field of row.definition.fields || [])
            if (field.type === 'string_list')
              values[field.key] = (values[field.key] || []).join('\n')
            else if (field.type === 'keyword_rules' || field.type === 'blocked_rules')
              values[field.key] ||= []
          return [row.definition.id, values]
        })
      )
      if (id) {
        base.value = result
        activeScope = `${scope.value}:${id}`
        activeActor = actor.value
        const stored = session.drafts[activeScope] as Row | undefined
        draftVersion.value = result.version
        if (stored?.values && !stored.baseline && stored.version !== result.version) {
          session.saveDraft(activeScope, null)
          draftNotice.value = '旧版草稿缺少字段基线且配置版本已变化，已失效；请重新编辑。'
        } else if (stored?.values) {
          for (const row of rows.value)
            for (const field of row.definition.fields || []) {
              const value = stored.values[row.definition.id]?.[field.key]
              if (value === undefined) continue
              const previous = stored.baseline
                ? stored.baseline[row.definition.id]?.[field.key]
                : row.values[field.key]
              const restored =
                !stored.baseline && field.type === 'string_list' ? lines(value) : value
              if (previous !== undefined && !sameFieldValue(field.type, restored, previous))
                draft.value[row.definition.id][field.key] = clone(
                  field.type === 'string_list' ? restored.join('\n') : restored
                )
            }
          draftVersion.value = stored.version
          persist()
        }
      }
    } catch (exception) {
      if (current === generation && (exception as Error).name !== 'AbortError')
        error.value = (exception as Error).message
    } finally {
      if (current === generation) loading.value = false
    }
  }
  async function save() {
    if (
      !base.value ||
      !dirty.value ||
      loading.value ||
      saving.value ||
      detailIssue.value ||
      draftVersion.value !== base.value.version
    )
      return
    const current = generation,
      id = groupID.value,
      scopeAtStart = activeScope,
      scopeKey = scope.value,
      workspace = session.workspace,
      owner = actor.value
    const feature = detailID.value
    if (!changes.value[feature]) return
    const body = clone({
      version: draftVersion.value,
      changes: { [feature]: changes.value[feature] }
    })
    const draftAtStart = draftSnapshot()!
    persist()
    saving.value = true
    error.value = ''
    saved.value = false
    try {
      const result = await api<Row>(`/groups/${id}/features`, 'PUT', body, undefined, workspace)
      if (owner === actor.value && equal(session.drafts[scopeAtStart], draftAtStart)) {
        const remaining = clone(draftAtStart)
        delete remaining.values[feature]
        delete remaining.baseline[feature]
        remaining.version = result.version
        session.saveDraft(scopeAtStart, Object.keys(remaining.values).length ? remaining : null)
      }
      if (current !== generation || scopeKey !== scope.value || id !== groupID.value) return
      for (const row of rows.value)
        if (body.changes[row.definition.id])
          row.values = { ...row.values, ...body.changes[row.definition.id] }
      base.value!.version = draftVersion.value = result.version
      persist()
      saved.value = true
      emit('updated')
    } catch (exception) {
      if (current === generation) error.value = (exception as Error).message
    } finally {
      if (current === generation) saving.value = false
    }
  }
  function discard() {
    if (!detail.value || saving.value) return
    const values = clone(detail.value.values)
    for (const field of detail.value.definition.fields || [])
      if (field.type === 'string_list') values[field.key] = (values[field.key] || []).join('\n')
    draft.value[detailID.value] = values
    error.value = ''
    persist()
  }
  function validateFields(row: Row, fields: Row) {
    const size = (value: string) => new TextEncoder().encode(value).length
    const validText = (value: unknown, limit: number) =>
      typeof value === 'string' && !!value.trim() && size(value) <= limit
    for (const field of row.definition.fields || []) {
      if (!(field.key in fields)) continue
      const value = fields[field.key]
      if (
        field.type === 'integer' &&
        (!Number.isInteger(value) ||
          value < (field.key === 'cooldown_seconds' ? 30 : 0) ||
          value > field.limit)
      )
        return `${field.label}须为 ${field.key === 'cooldown_seconds' ? 30 : 0} 至 ${field.limit} 的整数`
      if (field.type === 'keyword_rules') {
        if (!Array.isArray(value) || value.length > Math.min(field.limit || 100, 100))
          return '关键词回复最多 100 条规则'
        for (const [index, rule] of value.entries()) {
          if (
            !validText(rule.keyword, 200) ||
            typeof rule.reply !== 'string' ||
            !rule.reply ||
            size(rule.reply) > 4000 ||
            !['', 'contains', 'exact'].includes(rule.match || '')
          )
            return `规则 ${index + 1} 须填写关键词（最多 200 bytes）、回复（最多 4000 bytes）及有效匹配模式`
        }
      }
      if (field.type === 'blocked_rules') {
        if (!Array.isArray(value) || value.length > Math.min(field.limit || 20, 20))
          return '组合拦截最多 20 条规则'
        for (const [index, rule] of value.entries()) {
          if (
            !validText(rule.name, 120) ||
            !Array.isArray(rule.words) ||
            rule.words.length < 2 ||
            rule.words.length > 5 ||
            rule.words.some((word: unknown) => !validText(word, 200)) ||
            new Set(rule.words).size !== rule.words.length
          )
            return `组合规则 ${index + 1} 须填写名称（最多 120 bytes）和 2 至 5 个不重复的非空词（每词最多 200 bytes）`
        }
      }
    }
    return ''
  }
  function clearPreview() {
    previewSequence++
    previewController?.abort()
    previewController = undefined
    previewResult.value = null
    previewError.value = ''
    previewLoading.value = false
  }
  async function preview(input: { text: string; sender: string; role: string }) {
    if (!previewAvailable.value || previewReason.value || loading.value || saving.value) return
    clearPreview()
    const size = (value: string) => new TextEncoder().encode(value).length
    if (
      !input.text ||
      size(input.text) > 16384 ||
      size(input.sender) > 200 ||
      !['member', 'admin', 'owner'].includes(input.role)
    ) {
      previewError.value = '请核对试算文本、模拟成员 ID 和模拟角色'
      return
    }
    const current = generation,
      sequence = previewSequence,
      context = previewContext.value,
      id = groupID.value,
      feature = detailID.value,
      workspace = session.workspace,
      version = draftVersion.value
    const request = new AbortController()
    previewController = request
    const isCurrent = () =>
      sequence === previewSequence &&
      current === generation &&
      context === previewContext.value &&
      !request.signal.aborted
    const fields = changes.value[feature]
    previewLoading.value = true
    try {
      const result = await api<Row>(
        `/groups/${id}/features/preview`,
        'POST',
        clone({
          version,
          feature,
          changes: fields ? { [feature]: fields } : {},
          ...input
        }),
        request.signal,
        workspace
      )
      if (!isCurrent()) return
      if (
        result.group_id !== id ||
        result.platform !== workspace ||
        result.version !== version ||
        result.feature !== feature ||
        result.validation_level !== 'simulation' ||
        result.applied !== false ||
        result.native_submitted !== false ||
        !result.decision ||
        typeof result.decision !== 'object'
      )
        throw new Error('试算响应与当前配置不一致，请重新试算')
      previewResult.value = result
    } catch (exception) {
      if (isCurrent() && (exception as Error).name !== 'AbortError')
        previewError.value = (exception as Error).message
    } finally {
      if (isCurrent()) {
        previewLoading.value = false
        previewController = undefined
      }
    }
  }
  watch(previewContext, clearPreview, { flush: 'sync' })
  watch(draft, () => draftRevision.value++, { deep: true, flush: 'sync' })
  watch(
    draft,
    () => {
      saved.value = false
      persist()
    },
    { deep: true }
  )
  watch(groupID, (id) => {
    session.saveDraft(`${scope.value}:selection`, id)
    emit('selected', id)
    void load()
  })
  watch(
    () =>
      `${scope.value}:${session.permissions.join(',')}:${props.initialGroup || ''}:${props.groups.map((group) => group.group_id).join(',')}`,
    () => {
      const remembered = session.drafts[`${scope.value}:selection`] as string | undefined
      const next =
        [props.initialGroup, remembered, groupID.value].find((id) =>
          props.groups.some((group) => group.group_id === id)
        ) ||
        props.groups[0]?.group_id ||
        ''
      if (next !== groupID.value) groupID.value = next
      else void load()
    },
    { immediate: true }
  )
  watch(
    () => props.view,
    () => {
      detailID.value = ''
      capabilityID.value = ''
    }
  )
  onBeforeUnmount(() => {
    clearPreview()
    persist()
    generation++
    controller?.abort()
  })
</script>

<style scoped>
  .feature-health {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 16px;
    border-bottom: 1px solid var(--ui-line);
    padding-bottom: 20px;
    margin-bottom: 20px;
  }
  .feature-health > div {
    display: grid;
    gap: 8px;
  }
  .feature-health span {
    color: var(--ui-muted);
    font-size: 12px;
  }
  .feature-health strong {
    font-size: 21px;
  }
  @media (max-width: 700px) {
    .feature-health {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
  }
  .feature-center {
    min-width: 0;
  }
  .feature-toolbar {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
    align-items: center;
  }
  .feature-toolbar .el-select {
    width: 240px;
    max-width: 100%;
  }
  .feature-toolbar .el-input {
    width: 220px;
    max-width: 100%;
  }
  .feature-toolbar .el-select:last-of-type {
    width: 170px;
  }
  .feature-toolbar .el-button + .el-button {
    margin-left: 0;
  }
  .feature-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 16px;
    align-items: center;
    margin: 18px 0;
    color: #687386;
    font-size: 13px;
  }
  .feature-category {
    margin-top: 4px;
    color: #687386;
    font-size: 12px;
  }
  .feature-table-wrap {
    width: 100%;
    overflow-x: auto;
    margin-top: 16px;
  }
  .feature-table-wrap .el-table {
    min-width: 840px;
  }
  .dependency-links {
    display: flex;
    flex-wrap: wrap;
    gap: 2px 12px;
    align-items: center;
  }
  .dependency-links .el-button {
    margin-left: 0;
    white-space: normal;
    text-align: left;
  }
  .feature-description {
    color: #505b6c;
    line-height: 1.7;
    margin: 0 0 20px;
  }
  .feature-detail-content {
    min-width: 0;
  }
  .feature-detail-content :deep(.el-input__wrapper),
  .feature-detail-content :deep(.el-select__wrapper) {
    min-height: 36px;
    border-radius: 8px;
  }
  .feature-detail-content :deep(.el-textarea__inner) {
    border-radius: 8px;
  }
  .feature-detail-content :deep(.el-input-number) {
    max-width: 100%;
  }
  .feature-dependencies {
    display: grid;
    gap: 8px;
    margin-bottom: 18px;
    font-size: 13px;
  }
  .feature-dependencies > span:first-child {
    color: var(--ui-muted);
  }
  .feature-detail-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
  }
  .feature-detail-actions .el-button {
    min-height: 36px;
    margin: 0;
    border-radius: 8px;
  }
  @media (max-width: 600px) {
    .feature-toolbar .el-input,
    .feature-toolbar .el-select,
    .feature-toolbar .el-select:last-of-type {
      width: 100%;
    }
  }
</style>
