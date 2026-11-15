<template>
  <section class="assistant-governance" data-testid="assistant-governance" :aria-busy="loading">
    <div class="governance-toolbar">
      <ElTag type="info" effect="plain">平台管理员</ElTag>
      <span>采样 {{ stamp(health?.sampled_at) }}</span>
      <ElTooltip content="刷新助手状态"
        ><ElButton
          :icon="Refresh"
          text
          aria-label="刷新助手状态"
          :loading="loading"
          :disabled="saving"
          @click="load"
      /></ElTooltip>
    </div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <section class="governance-audit" aria-label="群管助手审计">
      <div class="governance-heading">
        <h2>群管助手审计</h2>
        <ElTag type="info" effect="plain">Web 对话配置</ElTag>
        <small>采样 {{ stamp(auditSampledAt) }}</small>
      </div>
      <p class="audit-window">最近 200 条可见审计中匹配的助手事件，非完整历史。</p>
      <ElEmpty
        v-if="auditEvents === undefined"
        description="当前没有审计读取权限或数据尚未采集"
        :image-size="48"
      />
      <ElTable v-else :data="auditRows" row-key="id" empty-text="当前窗口暂无助手事件">
        <ElTableColumn prop="action" label="事件" min-width="130" />
        <ElTableColumn prop="actor" label="网站操作人" min-width="180" show-overflow-tooltip />
        <ElTableColumn
          prop="resource"
          label="回合 / 建议编号"
          min-width="200"
          show-overflow-tooltip
        />
        <ElTableColumn prop="result" label="结果" min-width="160" />
        <ElTableColumn label="时间" min-width="170">
          <template #default="{ row }">{{ stamp(row.createdAt) }}</template>
        </ElTableColumn>
      </ElTable>
    </section>
    <div class="governance-kpis">
      <div v-for="item in statistics" :key="item.label">
        <ElStatistic
          v-if="typeof item.value === 'number'"
          :title="item.label"
          :value="item.value"
        />
        <template v-else
          ><span class="missing-label">{{ item.label }}</span
          ><strong class="missing-value">未采集</strong></template
        >
        <small>{{ item.note }}</small>
      </div>
    </div>
    <ElAlert
      v-if="health?.unverified_usage > 0"
      title="存在用量待核对调用，请在 AI 网关核对原调用。"
      type="warning"
      :closable="false"
    />
    <div class="governance-sections">
      <section>
        <div class="governance-heading"
          ><h2>Web 群管助手准入</h2
          ><ElTag :type="base?.enabled ? 'success' : 'info'">{{
            base ? (base.enabled ? '新回合已开放' : '新回合已关闭') : '未采集'
          }}</ElTag></div
        >
        <ElSkeleton v-if="loading && !base" :rows="5" animated />
        <ElForm
          v-else-if="base"
          label-position="left"
          label-width="115px"
          :disabled="loading || saving"
        >
          <ElFormItem label="新回合"
            ><ElSwitch v-model="draft.enabled" aria-label="开放助手新回合"
          /></ElFormItem>
          <ElFormItem label="租户灰度"
            ><ElSwitch v-model="draft.tenant_access" aria-label="助手租户灰度"
          /></ElFormItem>
          <ElFormItem label="人工审批"
            ><ElSwitch v-model="draft.approvals_enabled" aria-label="开放助手人工审批"
          /></ElFormItem>
          <ElFormItem label="配置版本"
            ><span>{{ base.version }}</span></ElFormItem
          >
          <ElFormItem label="模型路由"
            ><ElTag type="info">{{ base.model || '未配置' }}</ElTag
            ><ElTag :type="base.configured ? 'success' : 'info'">{{
              base.configured ? '已配置' : '未开放'
            }}</ElTag></ElFormItem
          >
          <div class="governance-actions">
            <ElButton :disabled="!dirty" @click="reset">撤销修改</ElButton>
            <ElButton
              type="primary"
              :icon="Check"
              :loading="saving"
              :disabled="!dirty"
              @click="save"
              >保存准入设置</ElButton
            >
          </div>
        </ElForm>
      </section>
      <section>
        <div class="governance-heading"><h2>模型与运行边界</h2></div>
        <ElDescriptions :column="1" border>
          <ElDescriptionsItem label="调用链">共享 AI 网关</ElDescriptionsItem>
          <ElDescriptionsItem label="回合预算">3 次模型 / 6 次顺序工具 / 90 秒</ElDescriptionsItem>
          <ElDescriptionsItem label="建议执行">本人确认，按平台和动作校验准入</ElDescriptionsItem>
          <ElDescriptionsItem label="原生动作">不授予模型执行能力</ElDescriptionsItem>
          <ElDescriptionsItem label="历史可见性">本人会话与当前授权范围</ElDescriptionsItem>
        </ElDescriptions>
        <ElButton
          v-if="canGateway"
          :icon="Connection"
          class="gateway-link"
          @click="$emit('gateway')"
          >AI 网关</ElButton
        >
      </section>
    </div>
    <ModuleExposure />
    <section class="governance-access">
      <div class="governance-heading"
        ><h2>授权能力</h2><ElTag type="info" effect="plain">显式授权与资源范围取交集</ElTag></div
      >
      <ElTable :data="permissions" row-key="key" empty-text="暂无权限定义">
        <ElTableColumn prop="label" label="能力" min-width="100" />
        <ElTableColumn prop="key" label="权限" min-width="170" />
        <ElTableColumn prop="scope" label="可见与操作范围" min-width="230" />
        <ElTableColumn prop="gate" label="额外条件" min-width="230" />
      </ElTable>
    </section>
  </section>
</template>
<script setup lang="ts">
  import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
  import { ElMessage } from 'element-plus'
  import { Check, Connection, Refresh } from '@element-plus/icons-vue'
  import { api, errorText, type Row } from '../api'
  import { useSession } from '../store'
  import ModuleExposure from './ModuleExposure.vue'

  defineEmits<{ gateway: [] }>()
  const props = defineProps<{ auditEvents?: Row[]; auditSampledAt: string }>()
  const session = useSession()
  const owner = session.user?.id
  const canGateway = computed(() => session.permissions.includes('plans:manage'))
  const loading = ref(false),
    saving = ref(false),
    error = ref('')
  const health = ref<Row>(),
    base = ref<Row>()
  const draft = reactive({ enabled: false, tenant_access: false, approvals_enabled: false })
  let generation = 0
  const controller = new AbortController()
  const keys = ['enabled', 'tenant_access', 'approvals_enabled'] as const
  const dirty = computed(() => !!base.value && keys.some((key) => draft[key] !== base.value?.[key]))
  const statistics = computed(() => [
    {
      label: '当前活跃回合',
      value: health.value?.active_turns,
      note: `并发上限 ${health.value?.concurrency_limit ?? '未采集'}`
    },
    { label: '持久化待完成回合', value: health.value?.persisted_active_turns, note: '数据库观察' },
    { label: '待结算调用', value: health.value?.pending_usage, note: '预留 / 已提交' },
    { label: '用量待核对', value: health.value?.unverified_usage, note: '未知用量继续冻结' }
  ])
  const auditActions: Record<string, string> = {
    assistant_turn_finished: '对话回合结束',
    assistant_plan_proposed: '生成配置建议',
    assistant_plan_committed: '人工确认保存',
    assistant_action_proposed: '生成群管建议',
    assistant_action_approved: '本人确认群管动作',
    assistant_action_cancelled: '取消群管建议',
    assistant_action_finished: '群管动作结果',
    assistant_settings_updated: '服务准入变更'
  }
  const auditStatuses: Record<string, string> = {
    succeeded: '完成',
    failed: '失败',
    cancelled: '已取消',
    expired: '已过期',
    blocked: '准入阻断',
    approved: '已确认',
    unknown: '待核对'
  }
  const auditRows = computed(() =>
    (props.auditEvents || [])
      .filter((event) => Object.hasOwn(auditActions, event.action))
      .map((event) => ({
        id: event.id,
        action: auditActions[event.action],
        actor: event.actor_id || '未提供',
        resource: event.resource_id || '未提供',
        result: event.action.startsWith('assistant_action_')
          ? auditStatuses[event.detail?.status] || '状态未提供'
          : event.action === 'assistant_turn_finished'
            ? auditStatuses[event.detail?.status] || '状态未提供'
            : event.action === 'assistant_plan_proposed'
              ? `基于版本 ${event.detail?.base_version ?? '未提供'}`
              : `版本 ${event.detail?.version ?? '未提供'}${event.action === 'assistant_plan_committed' ? ' · 配置已保存' : ''}`,
        createdAt: event.created_at
      }))
  )
  const stamp = (value?: string) =>
    value
      ? new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
      : '未采集'
  const permissions = [
    {
      key: 'assistant:use',
      label: '查询与会话',
      scope: '本人会话、已授权账号与群的安全投影',
      gate: '旺商聊或辣椒聊 / 套餐 / 服务准入 / 当前群授权'
    },
    {
      key: 'assistant:propose',
      label: '生成建议',
      scope: '指定群的白名单配置差异',
      gate: '显式建议权限 / 最新版本 / 未截断配置'
    },
    {
      key: 'assistant:approve',
      label: '人工确认',
      scope: '本人有效建议，仅保存配置',
      gate: '字段权限 / 套餐模块 / 新鲜原生管理员角色'
    },
    {
      key: 'assistant:action_propose',
      label: '群管建议',
      scope: '本人当前群、明确目标和已实现动作',
      gate: '逐动作 group_action 权限 / 不含测试授权'
    },
    {
      key: 'assistant:action_approve',
      label: '群管本人确认',
      scope: '本人密封计划、单群、10分钟有效',
      gate: '套餐 / 当前角色 / epoch与版本 / 平台生产准入'
    },
    {
      key: 'superadmin',
      label: '服务治理',
      scope: '平台开关、运行计数与网关管理',
      gate: '不自动获得他人的会话正文或原生动作权限'
    }
  ]
  function reset() {
    if (base.value) for (const key of keys) draft[key] = base.value[key] === true
  }
  async function load() {
    const current = ++generation
    loading.value = true
    error.value = ''
    try {
      const [settings, state] = await Promise.all([
        api<Row>('/assistant/settings', 'GET', undefined, controller.signal, 'wsl'),
        api<Row>('/assistant/health', 'GET', undefined, controller.signal, 'wsl')
      ])
      if (current !== generation || owner !== session.user?.id) return
      base.value = settings
      health.value = state
      reset()
    } catch (e) {
      if (current === generation && !controller.signal.aborted) {
        base.value = undefined
        health.value = undefined
        error.value = errorText((e as Error).message)
      }
    } finally {
      if (current === generation) loading.value = false
    }
  }
  async function save() {
    if (!base.value || saving.value || !dirty.value) return
    const current = generation
    const body = {
      ...draft,
      version: base.value.version,
      monthly_tokens: base.value.monthly_tokens,
      model: base.value.model,
      base_url: base.value.base_url
    }
    saving.value = true
    error.value = ''
    try {
      const result = await api<Row>('/assistant/settings', 'PUT', body, controller.signal, 'wsl')
      if (current !== generation || owner !== session.user?.id) return
      base.value = result
      reset()
      ElMessage.success('准入设置已保存')
    } catch (e) {
      if (current === generation && !controller.signal.aborted)
        error.value = errorText((e as Error).message)
    } finally {
      saving.value = false
    }
  }
  onMounted(load)
  onUnmounted(() => {
    generation++
    controller.abort()
    base.value = undefined
    health.value = undefined
  })
</script>
<style scoped>
  .assistant-governance {
    min-width: 0;
  }
  .governance-toolbar,
  .governance-heading {
    display: flex;
    align-items: center;
    gap: 10px;
    flex-wrap: wrap;
  }
  .governance-toolbar {
    margin-bottom: 18px;
    font-size: 12px;
    color: var(--ui-muted);
  }
  .governance-audit {
    padding-bottom: 24px;
  }
  .governance-heading small,
  .audit-window {
    color: var(--wsl-text-muted, #697386);
    font-size: 12px;
  }
  .audit-window {
    margin: 8px 0 16px;
  }
  .governance-toolbar .el-button {
    margin-left: auto;
  }
  .governance-kpis {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 20px;
    border-block: 1px solid var(--ui-line);
    padding: 22px 0;
    margin-bottom: 24px;
  }
  .governance-kpis small {
    font-size: 11px;
    color: var(--ui-muted);
  }
  .missing-label {
    display: block;
    color: var(--ui-muted);
    font-size: 13px;
    margin-bottom: 6px;
  }
  .missing-value {
    display: block;
    font-size: 20px;
    line-height: 28px;
  }
  .governance-sections {
    display: grid;
    grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr);
    gap: 28px;
  }
  .governance-sections > section {
    min-width: 0;
  }
  .governance-heading {
    margin-bottom: 18px;
  }
  h2 {
    margin: 0;
    font-size: 15px;
  }
  .governance-actions {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
  }
  .governance-actions .el-button + .el-button {
    margin-left: 0;
  }
  .gateway-link {
    margin-top: 16px;
  }
  .governance-access {
    border-top: 1px solid var(--ui-line);
    padding-top: 24px;
    margin-top: 28px;
  }
  @media (max-width: 900px) {
    .governance-kpis {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .governance-sections {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
