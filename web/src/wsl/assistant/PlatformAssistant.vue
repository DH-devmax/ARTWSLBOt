<template>
  <section class="platform-assistant">
    <div class="section-title">
      <h2>超管助手</h2>
      <div class="platform-actions">
        <ElTag type="info">平台只读</ElTag>
        <ElTooltip content="刷新状态">
          <ElButton :icon="Refresh" :loading="loading" aria-label="刷新超管助手" @click="refresh" />
        </ElTooltip>
      </div>
    </div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElDescriptions v-if="snapshot" :column="2" border class="platform-facts">
      <ElDescriptionsItem label="机器人">{{ snapshot.accounts }}</ElDescriptionsItem>
      <ElDescriptionsItem label="已认证">{{ snapshot.authenticated_accounts }}</ElDescriptionsItem>
      <ElDescriptionsItem label="授权群">{{ snapshot.authorized_groups }}</ElDescriptionsItem>
      <ElDescriptionsItem label="未知模型调用">{{ snapshot.unknown_model_calls }}</ElDescriptionsItem>
      <ElDescriptionsItem label="模型路由">
        <ElTag :type="snapshot.gateway_enabled && snapshot.verified_route ? 'success' : 'warning'">
          {{ snapshot.gateway_enabled && snapshot.verified_route ? '已核验开放' : '尚未开放' }}
        </ElTag>
      </ElDescriptionsItem>
      <ElDescriptionsItem label="采样时间">{{ time(snapshot.sampled_at) }}</ElDescriptionsItem>
    </ElDescriptions>
    <ElForm v-if="!loading && snapshot" label-position="top" class="platform-settings" @submit.prevent="save">
      <ElFormItem label="超管助手">
        <ElSwitch v-model="settings.enabled" aria-label="超管助手开关" :disabled="busy || loading" />
      </ElFormItem>
      <ElFormItem label="独立自然月预算（积分）">
        <ElInputNumber v-model="budget" :min="0" :max="100000" :precision="3" :disabled="busy || loading"
          aria-label="超管助手月预算" controls-position="right" />
      </ElFormItem>
      <ElButton :icon="Check" :disabled="busy || loading" :loading="saving" native-type="submit">保存设置</ElButton>
    </ElForm>
    <div class="section-title">
      <h3>本人平台会话</h3>
      <div class="platform-actions">
        <ElTooltip content="查询原回合">
          <ElButton :icon="Refresh" aria-label="查询超管助手回合" :disabled="!sessionID" @click="loadTurns" />
        </ElTooltip>
        <ElButton :icon="Plus" :disabled="busy || unresolved" @click="newSession">新建会话</ElButton>
      </div>
    </div>
    <div class="platform-transcript" aria-live="polite">
      <ElEmpty v-if="!turns.length" description="暂无平台会话" :image-size="64" />
      <article v-for="turn in turns" :key="turn.id" class="platform-turn">
        <div class="turn-heading"><strong>本人</strong><time>{{ time(turn.created_at) }}</time></div>
        <p>{{ turn.input }}</p>
        <div class="turn-heading">
          <strong>超管助手</strong>
          <ElTag :type="turn.status === 'completed' ? 'success' : turn.status === 'unknown' ? 'warning' : 'info'">
            {{ statusLabel(turn.status) }}
          </ElTag>
        </div>
        <p v-if="turn.text">{{ turn.text }}</p>
      </article>
    </div>
    <ElForm label-position="top" @submit.prevent="ask">
      <ElFormItem label="平台问题">
        <ElInput v-model="question" type="textarea" :rows="3" :maxlength="600" :disabled="busy || unresolved"
          aria-label="平台问题" />
      </ElFormItem>
      <div class="platform-actions">
        <ElButton v-if="busy" :icon="Close" @click="cancel">取消回合</ElButton>
        <ElButton type="primary" :icon="Position" native-type="submit" :loading="busy"
          :disabled="loading || saving || unresolved || !settings.enabled || !snapshot?.gateway_enabled ||
            !snapshot?.verified_route || !question.trim() || budget <= 0">发送</ElButton>
      </div>
    </ElForm>
  </section>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref } from 'vue'
  import { Check, Close, Plus, Position, Refresh } from '@element-plus/icons-vue'
  import { ElAlert, ElButton, ElDescriptions, ElDescriptionsItem, ElEmpty, ElForm, ElFormItem,
    ElInput, ElInputNumber, ElSwitch, ElTag, ElTooltip } from 'element-plus'
  import { api, type Row } from '../api'
  import { useSession } from '../store'

  interface Turn {
    id: string
    idempotency_key: string
    input: string
    text: string
    status: string
    created_at: string
  }
  const session = useSession()
  const settings = ref({ enabled: false, monthly_limit_micro: 0, version: 0 })
  const budget = ref(0)
  const snapshot = ref<Row | null>(null)
  const sessionID = ref('')
  const turns = ref<Turn[]>([])
  const question = ref('')
  const error = ref('')
  const loading = ref(true)
  const saving = ref(false)
  const busy = ref(false)
  const pendingKey = ref('')
  const uncertain = ref(false)
  const unresolved = computed(() => uncertain.value || turns.value.some(t => ['running', 'unknown'].includes(t.status)))
  const controller = new AbortController()
  const cacheKey = `platform-assistant:${session.user?.id}:session`
  let live = true
  const time = (value: string) => value ? new Date(value).toLocaleString() : '未提供'
  const statusLabel = (value: string) => ({
    completed: '已完成', running: '执行中', unknown: '未知 / 冻结',
    failed: '未完成', canceled: '已取消'
  }[value] || value)
  function showError(e: unknown) {
    if (live) error.value = e instanceof Error ? e.message : '超管助手暂不可用'
  }
  async function loadTurns() {
    if (!sessionID.value) return
    try {
      const out = await api<Turn[]>(`/platform-assistant/sessions/${sessionID.value}/turns`, 'GET', undefined, controller.signal)
      if (live) {
        turns.value = out
        if (out.some(t => t.idempotency_key === pendingKey.value)) {
          uncertain.value = false
          pendingKey.value = ''
          sessionStorage.removeItem(`${cacheKey}:pending`)
        }
      }
    } catch (e) { showError(e) }
  }
  async function refresh() {
    loading.value = true
    error.value = ''
    try {
      const out = await api<Row>('/platform-assistant', 'GET', undefined, controller.signal)
      if (!live) return
      settings.value = out.settings
      budget.value = out.settings.monthly_limit_micro / 1e6
      snapshot.value = out.snapshot
    } catch (e) { snapshot.value = null; showError(e) }
    finally { if (live) loading.value = false }
  }
  async function save() {
    saving.value = true
    error.value = ''
    try {
      const out = await api<typeof settings.value>('/platform-assistant/settings', 'PUT',
        { ...settings.value, monthly_limit_micro: Math.round(budget.value * 1e6) }, controller.signal)
      if (live) settings.value = out
    } catch (e) { showError(e) }
    finally { if (live) saving.value = false }
  }
  async function newSession() {
    error.value = ''
    try {
      const out = await api<{ id: string }>('/platform-assistant/sessions', 'POST', {}, controller.signal)
      if (!live) return
      sessionID.value = out.id
      sessionStorage.setItem(cacheKey, out.id)
      turns.value = []
    } catch (e) { showError(e) }
  }
  async function ask() {
    if (busy.value || unresolved.value || !question.value.trim()) return
    busy.value = true
    error.value = ''
    try {
      if (!sessionID.value) await newSession()
      if (!sessionID.value) return
      pendingKey.value = crypto.randomUUID()
      sessionStorage.setItem(`${cacheKey}:pending`, pendingKey.value)
      await api(`/platform-assistant/sessions/${sessionID.value}/turns`, 'POST',
        { input: question.value, idempotency_key: pendingKey.value }, controller.signal)
      if (live) question.value = ''
    } catch (e) { uncertain.value = true; showError(e) }
    finally {
      if (live) { await loadTurns(); busy.value = false }
    }
  }
  async function cancel() {
    if (!sessionID.value || !pendingKey.value) return
    try {
      await api(`/platform-assistant/sessions/${sessionID.value}/turns/${pendingKey.value}/cancel`,
        'POST', {}, controller.signal)
    } catch (e) { showError(e) }
  }
  onMounted(async () => {
    sessionID.value = sessionStorage.getItem(cacheKey) || ''
    pendingKey.value = sessionStorage.getItem(`${cacheKey}:pending`) || ''
    uncertain.value = !!pendingKey.value
    await refresh()
    await loadTurns()
  })
  onBeforeUnmount(() => { live = false; controller.abort() })
</script>

<style scoped>
  .platform-assistant { display: grid; gap: 20px; min-width: 0; }
  .platform-actions { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; }
  .platform-settings { display: flex; align-items: end; gap: 16px; flex-wrap: wrap; border-bottom: 1px solid var(--el-border-color-lighter); padding-bottom: 16px; }
  .platform-settings .el-form-item { margin-bottom: 0; }
  .platform-transcript { max-height: 560px; overflow: auto; min-width: 0; }
  .platform-turn { border-bottom: 1px solid var(--el-border-color-lighter); padding: 16px 0; }
  .turn-heading { display: flex; justify-content: space-between; gap: 12px; align-items: center; }
  .turn-heading time { color: var(--el-text-color-secondary); font-size: 12px; }
  .platform-turn p { white-space: pre-wrap; overflow-wrap: anywhere; margin: 12px 0 20px; }
  @media (max-width: 640px) {
    .platform-facts :deep(.el-descriptions__label) { min-width: 60px; overflow-wrap: anywhere; }
    .platform-facts :deep(.el-descriptions__content) { overflow-wrap: anywhere; }
    .platform-settings { align-items: stretch; }
    .platform-settings .el-form-item { width: 100%; }
  }
</style>
