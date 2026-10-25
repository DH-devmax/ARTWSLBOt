<template>
  <section class="data-section ai-console" v-loading="loading">
    <div class="section-title">
      <h2>{{ title }}</h2>
      <ElTag type="warning" effect="plain">Go 引擎迁移中 · 未开放发言</ElTag>
    </div>
    <ElAlert v-if="error" class="console-alert" :title="error" type="error" :closable="false" />
    <template v-if="configurable">
      <ElEmpty v-if="!account" description="尚未选择机器人面板" :image-size="64" />
      <ElForm
        v-else-if="loaded"
        label-position="top"
        class="console-form"
        :disabled="saving"
        @submit.prevent="save"
      >
        <template v-if="section === 'chat'">
          <ElFormItem label="申请启用聊天"><ElSwitch v-model="form.enabled" /></ElFormItem>
          <div class="console-fields">
            <ElFormItem label="回复概率"
              ><ElInputNumber
                v-model="form.reply_probability"
                :min="0"
                :max="1"
                :step="0.05"
                :precision="2"
            /></ElFormItem>
            <ElFormItem label="回复冷却（秒）"
              ><ElInputNumber v-model="form.cooldown_seconds" :min="5" :max="3600"
            /></ElFormItem>
            <ElFormItem label="活跃起始小时"
              ><ElInputNumber v-model="form.start_hour" :min="0" :max="23"
            /></ElFormItem>
            <ElFormItem label="活跃结束小时"
              ><ElInputNumber v-model="form.end_hour" :min="1" :max="24"
            /></ElFormItem>
          </div>
          <ElDescriptions :column="1" border
            ><ElDescriptionsItem label="时区">Asia/Shanghai</ElDescriptionsItem
            ><ElDescriptionsItem label="实际生效"
              >未生效，等待引擎与群授权验收</ElDescriptionsItem
            ></ElDescriptions
          >
        </template>
        <template v-else-if="section === 'models'">
          <ElFormItem label="服务地址（HTTPS）"
            ><ElInput
              v-model="form.endpoint"
              maxlength="500"
              placeholder="https://provider.example/v1"
          /></ElFormItem>
          <ElFormItem label="聊天模型"
            ><ElInput v-model="form.chat_model" maxlength="200"
          /></ElFormItem>
          <div class="console-fields">
            <ElFormItem label="视觉模型"
              ><ElInput v-model="form.vision_model" maxlength="200"
            /></ElFormItem>
            <ElFormItem label="向量模型"
              ><ElInput v-model="form.embedding_model" maxlength="200"
            /></ElFormItem>
          </div>
          <ElFormItem :label="form.has_api_key ? 'API 密钥（已保存；留空保持不变）' : 'API 密钥'">
            <ElInput
              v-model="form.api_key"
              type="password"
              show-password
              autocomplete="off"
              maxlength="4096"
            />
          </ElFormItem>
          <ElCheckbox v-model="form.clear_api_key">清除已保存的 API 密钥</ElCheckbox>
          <ElDescriptions :column="1" border
            ><ElDescriptionsItem label="调用上限">全局 4 / 每群 1</ElDescriptionsItem
            ><ElDescriptionsItem label="模型调用"
              >未开放，不自动检测或调用供应商</ElDescriptionsItem
            ></ElDescriptions
          >
        </template>
        <template v-else-if="section === 'learning'">
          <ElFormItem label="申请启用学习"><ElSwitch v-model="form.enabled" /></ElFormItem>
          <ElFormItem label="表达学习"><ElSwitch v-model="form.expressions" /></ElFormItem>
          <ElFormItem label="黑话学习"><ElSwitch v-model="form.jargon" /></ElFormItem>
          <ElFormItem label="派生数据保留（天）"
            ><ElInputNumber v-model="form.derived_retention_days" :min="1" :max="90"
          /></ElFormItem>
          <ElDescriptions :column="1" border
            ><ElDescriptionsItem label="跨群学习">禁止</ElDescriptionsItem
            ><ElDescriptionsItem label="人物、表达、黑话数据"
              >等待 Go 群级存储迁移</ElDescriptionsItem
            ></ElDescriptions
          >
        </template>
        <template v-else-if="section === 'memory'">
          <ElFormItem label="申请启用长期记忆"><ElSwitch v-model="form.enabled" /></ElFormItem>
          <div class="console-fields">
            <ElFormItem label="检索条数"
              ><ElInputNumber v-model="form.top_k" :min="1" :max="20"
            /></ElFormItem>
            <ElFormItem label="派生记忆保留（天）"
              ><ElInputNumber v-model="form.derived_retention_days" :min="1" :max="90"
            /></ElFormItem>
          </div>
          <ElDescriptions :column="1" border
            ><ElDescriptionsItem label="原始对话保留">7 天</ElDescriptionsItem
            ><ElDescriptionsItem label="隔离边界">租户 / 群 / 机器人身份</ElDescriptionsItem
            ><ElDescriptionsItem label="跨群检索 / 空作用域回退"
              >禁止</ElDescriptionsItem
            ></ElDescriptions
          >
        </template>
        <div class="console-save">
          <ElButton type="primary" native-type="submit" :loading="saving" :icon="Check"
            >保存配置</ElButton
          >
          <ElTooltip content="重新读取已保存配置，丢弃当前输入">
            <ElButton :icon="Refresh" aria-label="重新读取已保存配置" @click="reloadSaved" />
          </ElTooltip>
          <span class="muted">版本 {{ version }} · 配置已保存不代表引擎已生效</span>
        </div>
      </ElForm>
    </template>
    <ElTable v-else-if="section === 'logs'" :data="events" empty-text="暂无配置操作记录">
      <ElTableColumn prop="action" label="事件" min-width="180" />
      <ElTableColumn prop="detail.section" label="配置模块" />
      <ElTableColumn prop="detail.version" label="版本" />
      <ElTableColumn prop="created_at" label="时间" min-width="200" />
    </ElTable>
    <template v-else>
      <ElDescriptions :column="1" border class="console-summary">
        <ElDescriptionsItem label="聊天引擎">Go（替换 Python 运行时）</ElDescriptionsItem>
        <ElDescriptionsItem label="真实发言">关闭</ElDescriptionsItem>
        <ElDescriptionsItem label="模型并发"
          >全局 4 / 每群 1 / 每群一个待生成候选</ElDescriptionsItem
        >
        <ElDescriptionsItem label="原生群管 / 财务 / 主机执行">无权限</ElDescriptionsItem>
        <ElDescriptionsItem label="插件执行">禁用；未完成作用域适配</ElDescriptionsItem>
      </ElDescriptions>
      <ElTable :data="capabilities" empty-text="暂无能力清单">
        <ElTableColumn prop="name" label="能力" min-width="170" />
        <ElTableColumn label="状态" min-width="180"
          ><template #default="{ row }"
            ><ElTag :type="row.status === 'configuration_only' ? 'info' : 'warning'">{{
              statusLabels[row.status] || row.status
            }}</ElTag></template
          ></ElTableColumn
        >
        <ElTableColumn label="作用域" min-width="150"
          ><template #default="{ row }">{{
            scopeLabels[row.scope] || row.scope
          }}</template></ElTableColumn
        >
      </ElTable>
    </template>
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import { Check, Refresh } from '@element-plus/icons-vue'
  import { ElMessage } from 'element-plus'
  import { api, type Row } from './api'
  import { useSession } from './store'

  const props = defineProps<{ account: string; section: string; title: string }>()
  const emit = defineEmits<{ updated: [] }>()
  const session = useSession()
  const configurable = computed(() =>
    ['chat', 'models', 'learning', 'memory'].includes(props.section)
  )
  const loading = ref(false),
    loaded = ref(false),
    saving = ref(false),
    error = ref('')
  const form = ref<Row>({}),
    version = ref(0),
    capabilities = ref<Row[]>([]),
    events = ref<Row[]>([])
  const statusLabels: Record<string, string> = {
    configuration_only: '配置已迁移 · 运行待验收',
    engine_pending: 'Go 能力迁移待完成',
    isolation_blocked: '隔离验收前禁用',
    runbook_only: '迁移手册已提供',
    docker_only: '仅 Docker 发布'
  }
  const scopeLabels: Record<string, string> = {
    account: '独立账号',
    group_required: '强制群级隔离',
    superadmin: '仅超管'
  }
  let generation = 0,
    controller: AbortController | undefined,
    draftKey = '',
    draftOwner = ''
  const clone = <T,>(value: T): T => JSON.parse(JSON.stringify(value))
  function persistDraft() {
    if (!draftKey || !loaded.value || draftOwner !== session.user?.id) return
    const safe = clone(form.value)
    delete safe.api_key
    delete safe.clear_api_key
    session.saveDraft(draftKey, { version: version.value, config: safe })
  }
  async function reloadSaved() {
    if (draftKey) session.saveDraft(draftKey, null)
    loaded.value = false
    await load()
  }
  async function load() {
    persistDraft()
    const current = ++generation
    controller?.abort()
    controller = new AbortController()
    const signal = controller.signal
    draftKey = ''
    form.value = {}
    error.value = ''
    loaded.value = false
    saving.value = false
    loading.value = true
    const account = props.account,
      section = props.section,
      owner = session.user?.id
    try {
      if (configurable.value && account) {
        const result = await api<Row>(
          `/accounts/${account}/maibot-settings/${section}`,
          'GET',
          undefined,
          signal,
          'maibot'
        )
        if (current !== generation || owner !== session.user?.id) return
        draftKey = `maibot:settings:${account}:${section}`
        draftOwner = owner || ''
        const draft = session.drafts[draftKey] as Row | undefined
        form.value = clone(draft?.config || result.config)
        // Server state, never an old draft, determines whether a secret exists.
        if (section === 'models') form.value.has_api_key = result.config.has_api_key
        version.value = draft?.version ?? result.version
        if (draft && draft.version !== result.version)
          error.value = '配置版本已变化；当前保留旧草稿，保存会拒绝覆盖新版本'
        loaded.value = true
      } else if (section === 'logs') {
        const result = await api<Row[]>(
          `/maibot/events${account ? `?account_id=${account}` : ''}`,
          'GET',
          undefined,
          signal,
          'maibot'
        )
        if (current === generation && owner === session.user?.id) events.value = result
      } else if (!configurable.value) {
        const result = await api<Row>('/maibot/capabilities', 'GET', undefined, signal, 'maibot')
        if (current === generation && owner === session.user?.id) capabilities.value = result.items
      }
    } catch (e) {
      if (!signal.aborted && current === generation) error.value = (e as Error).message
    } finally {
      if (current === generation) loading.value = false
    }
  }
  async function save() {
    const current = generation,
      account = props.account,
      section = props.section,
      owner = session.user?.id
    saving.value = true
    error.value = ''
    try {
      const result = await api<Row>(
        `/accounts/${account}/maibot-settings/${section}`,
        'PUT',
        { version: version.value, config: form.value },
        undefined,
        'maibot'
      )
      if (current !== generation || account !== props.account || owner !== session.user?.id) return
      form.value = clone(result.config)
      version.value = result.version
      persistDraft()
      ElMessage.success('配置已保存，运行待验收')
      emit('updated')
    } catch (e) {
      if (current === generation) error.value = (e as Error).message
    } finally {
      if (current === generation) saving.value = false
    }
  }
  watch(() => [props.account, props.section, session.user?.id], load, { immediate: true })
  onBeforeUnmount(() => {
    persistDraft()
    generation++
    controller?.abort()
    form.value = {}
  })
</script>
