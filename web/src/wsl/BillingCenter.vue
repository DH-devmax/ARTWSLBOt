<template>
  <section class="billing-panel">
    <header class="billing-toolbar"
      ><h2>套餐与 AI 积分</h2>
      <ElSelect
        v-if="admin"
        v-model="tenant"
        placeholder="我的工作空间"
        clearable
        filterable
        aria-label="账单租户"
        @change="load"
      >
        <ElOption
          v-for="item in tenants"
          :key="item.tenant_id"
          :value="item.tenant_id"
          :label="item.tenant_name || item.username"
        />
      </ElSelect>
      <ElButton v-if="admin && tenant" :icon="Check" @click="openConfirmation">确认收款</ElButton>
      <ElButton v-if="admin" :icon="Setting" @click="versionsOpen = true">套餐版本</ElButton>
      <ElButton
        v-if="result?.full_bill && !ownWorkspaceBill && !admin"
        :icon="Setting"
        @click="limitsOpen = true"
        >消费上限</ElButton
      >
      <ElButton :icon="Refresh" :loading="loading" aria-label="刷新积分账单" @click="load" />
    </header>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <div v-if="!admin" class="plan-comparison">
      <div v-for="plan in commercialPlans" :key="plan.id"
        ><strong>{{ plan.code }}</strong
        ><span>{{ plan.price_micro_usdt / 1000000 }} USDT / 30天</span
        ><span>{{ plan.group_limit }} 群 · {{ plan.member_limit }} 人</span
        ><span>{{ credits(plan.credits_micro) }} AI 积分</span></div
      >
    </div>
    <template v-if="result">
      <section v-if="ownWorkspaceBill" class="billing-workspace" aria-label="工作空间账单">
        <h3>{{ result.workspace?.name || '我的工作空间' }}</h3>
        <strong>{{ result.effective_plan?.name || '超管专属 · 不限额' }}</strong>
        <ElTag type="success">永久不限额 · 系统专属</ElTag>
        <p>{{ billingSourceLabel(result.billing_source) }}</p>
      </section>
      <div v-if="result.full_bill && !ownWorkspaceBill" class="wallet-summary">
        <div
          ><span>本期积分</span
          ><strong>{{
            result.wallet ? credits(result.wallet.granted_micro) : '无生效账期'
          }}</strong></div
        >
        <div
          ><span>可用</span
          ><strong>{{ result.wallet ? credits(result.wallet.available_micro) : '—' }}</strong></div
        >
        <div
          ><span>已消费</span
          ><strong>{{ result.wallet ? credits(result.wallet.spent_micro) : '—' }}</strong></div
        >
        <div
          ><span>冻结 / 待结算</span
          ><strong>{{ result.wallet ? credits(result.wallet.held_micro) : '—' }}</strong></div
        >
      </div>
      <template v-if="result.full_bill && !ownWorkspaceBill">
        <h3>订阅账期</h3>
        <ElTable :data="result.periods" empty-text="暂无商业订阅账期">
          <ElTableColumn prop="code" label="套餐" width="85" />
          <ElTableColumn label="开始" min-width="165"
            ><template #default="{ row }">{{ date(row.starts_at) }}</template></ElTableColumn
          >
          <ElTableColumn label="结束" min-width="165"
            ><template #default="{ row }">{{ date(row.ends_at) }}</template></ElTableColumn
          >
          <ElTableColumn label="状态" min-width="140"
            ><template #default="{ row }"
              >{{ periodState[row.state] || row.state
              }}<small v-if="row.reason"> · 需调整超额资源</small></template
            ></ElTableColumn
          >
        </ElTable>
      </template>
      <h3>{{
        ownWorkspaceBill ? '工作空间 AI 用量' : result.full_bill ? '租户 AI 用量' : '我的 AI 用量'
      }}</h3>
      <ElTable :data="result.calls || []" empty-text="暂无调用记录">
        <ElTableColumn label="时间" min-width="165"
          ><template #default="{ row }">{{ date(row.created_at) }}</template></ElTableColumn
        >
        <ElTableColumn label="用途" min-width="140"
          ><template #default="{ row }">{{
            row.purpose === 'heatbot'
              ? '热群 AI'
              : row.purpose === 'test'
                ? '平台测试'
                : 'Web 群管助手'
          }}</template></ElTableColumn
        >
        <ElTableColumn label="积分" min-width="110"
          ><template #default="{ row }">{{
            credits(row.status === 'settled' ? row.effective_charged_micro : row.reserved_micro)
          }}</template></ElTableColumn
        >
        <ElTableColumn label="状态" min-width="110"
          ><template #default="{ row }"
            ><ElTag :type="row.status === 'unknown' ? 'warning' : 'info'">{{
              callState[row.status] || row.status
            }}</ElTag></template
          ></ElTableColumn
        >
      </ElTable>
      <template v-if="result.full_bill && !ownWorkspaceBill"
        ><h3>积分流水</h3
        ><ElTable :data="result.entries" empty-text="暂无流水">
          <ElTableColumn label="时间" min-width="165"
            ><template #default="{ row }">{{ date(row.created_at) }}</template></ElTableColumn
          >
          <ElTableColumn label="类型" min-width="90"
            ><template #default="{ row }">{{
              entryState[row.kind] || row.kind
            }}</template></ElTableColumn
          >
          <ElTableColumn label="积分" min-width="110"
            ><template #default="{ row }">{{ credits(row.amount_micro) }}</template></ElTableColumn
          >
        </ElTable></template
      >
    </template>
    <ElEmpty v-else-if="admin && !tenant" description="请选择租户" :image-size="64" />
    <ElDialog
      v-model="confirmOpen"
      title="人工确认 USDT 收款"
      width="min(500px,calc(100vw - 24px))"
      append-to-body
      :close-on-click-modal="false"
    >
      <ElForm label-position="top">
        <ElFormItem label="套餐"
          ><ElSelect v-model="payment.plan_version_id"
            ><ElOption
              v-for="plan in assignablePlans"
              :key="plan.id"
              :label="`${plan.code} v${plan.version} · ${plan.price_micro_usdt / 1000000} USDT`"
              :value="plan.id" /></ElSelect
        ></ElFormItem>
        <ElFormItem label="收款金额"
          ><strong>{{ selectedPlan?.price_micro_usdt / 1000000 || 0 }} USDT</strong></ElFormItem
        >
        <ElFormItem label="收款凭证编号"
          ><ElInput v-model="payment.payment_reference" autocomplete="off" maxlength="120"
        /></ElFormItem>
        <ElCheckbox v-model="paymentVerified">已核实收款与租户归属</ElCheckbox>
        <ElAlert v-if="saveError" :title="saveError" type="error" :closable="false" />
      </ElForm>
      <template #footer
        ><ElButton @click="confirmOpen = false">取消</ElButton
        ><ElButton
          type="primary"
          :disabled="!paymentVerified || !selectedPlan"
          :loading="saving"
          @click="confirm"
          >确认并安排账期</ElButton
        ></template
      >
    </ElDialog>
    <ElDialog
      v-model="versionsOpen"
      title="套餐版本"
      width="min(600px,calc(100vw - 24px))"
      append-to-body
      :close-on-click-modal="false"
    >
      <ElTable :data="plans">
        <ElTableColumn label="套餐" min-width="120"
          ><template #default="{ row }">{{
            systemPlan(row) ? row.name || row.code : `${row.code} v${row.version}`
          }}</template></ElTableColumn
        >
        <ElTableColumn label="上架" width="70"
          ><template #default="{ row }"
            ><ElTag v-if="systemPlan(row)" type="info">系统专属</ElTag
            ><ElSwitch
              v-else
              :model-value="row.enabled"
              :disabled="saving"
              @change="availability(row)" /></template
        ></ElTableColumn>
        <ElTableColumn label="操作" min-width="130"
          ><template #default="{ row }"
            ><ElButton
              v-if="!systemPlan(row) && latest(row)"
              link
              type="primary"
              :loading="saving"
              @click="publish(row)"
              >发布下一版本</ElButton
            ></template
          ></ElTableColumn
        >
      </ElTable>
      <ElAlert v-if="saveError" :title="saveError" type="error" :closable="false" />
    </ElDialog>
    <ElDialog
      v-model="limitsOpen"
      title="本期消费上限"
      width="min(520px,calc(100vw - 24px))"
      append-to-body
      :close-on-click-modal="false"
    >
      <ElForm label-position="top">
        <ElFormItem label="范围"
          ><ElSelect v-model="limit.kind" @change="resetLimitScope"
            ><ElOption label="AI 用途" value="purpose" /><ElOption
              label="授权群"
              value="group" /><ElOption label="成员" value="member" /></ElSelect
        ></ElFormItem>
        <ElFormItem label="对象">
          <ElSelect v-model="limit.scope" @change="selectLimit"
            ><ElOption
              v-for="item in limitOptions"
              :key="item.value"
              :value="item.value"
              :label="item.label"
          /></ElSelect>
        </ElFormItem>
        <ElFormItem label="消费上限（积分）"
          ><ElInputNumber v-model="limit.credits" :min="0" :max="10000" :precision="6"
        /></ElFormItem>
        <ElAlert v-if="saveError" :title="saveError" type="error" :closable="false" />
      </ElForm>
      <template #footer
        ><ElButton @click="limitsOpen = false">取消</ElButton
        ><ElButton type="primary" :disabled="!limit.scope" :loading="saving" @click="saveLimit"
          >保存上限</ElButton
        ></template
      >
    </ElDialog>
  </section>
</template>
<script setup lang="ts">
  import { computed, onMounted, onUnmounted, reactive, ref, watch } from 'vue'
  import { Check, Refresh, Setting } from '@element-plus/icons-vue'
  import { api, type Row } from './api'
  import { useSession } from './store'
  import { billingSourceLabel, operatorPlan } from './customer'
  const props = defineProps<{ users?: Row[] }>()
  const session = useSession(),
    admin = computed(() => session.user?.role === 'superadmin')
  const tenants = computed(() => [
    ...new Map((props.users || []).filter((u) => u.tenant_id).map((u) => [u.tenant_id, u])).values()
  ])
  const tenant = ref(''),
    plans = ref<Row[]>([]),
    result = ref<Row | null>(null),
    error = ref(''),
    saveError = ref(''),
    loading = ref(false),
    saving = ref(false),
    confirmOpen = ref(false),
    limitsOpen = ref(false),
    versionsOpen = ref(false),
    paymentVerified = ref(false)
  const payment = reactive({ plan_version_id: '', payment_reference: '', idempotency_key: '' })
  const limit = reactive({ kind: 'purpose', scope: '', credits: 0, version: 0 })
  const groups = ref<Row[]>([]),
    members = ref<Row[]>([])
  const systemPlan = (plan: Row) => plan.system_only === true || plan.id === 'operator_unlimited'
  const commercialPlans = computed(() =>
    plans.value.filter((plan) => !systemPlan(plan) && plan.saleable !== false)
  )
  const assignablePlans = computed(() =>
    commercialPlans.value.filter((plan) => plan.enabled === true && plan.assignable !== false)
  )
  const ownWorkspaceBill = computed(() => operatorPlan(result.value))
  const selectedPlan = computed(() =>
    assignablePlans.value.find((p) => p.id === payment.plan_version_id)
  )
  function resetLimitScope() {
    limit.scope = ''
    limit.version = 0
  }
  const limitOptions = computed(() =>
    limit.kind === 'purpose'
      ? [
          { value: 'assistant', label: 'Web 群管助手' },
          { value: 'heatbot', label: '热群 AI' }
        ]
      : limit.kind === 'group'
        ? groups.value.map((g) => ({ value: g.group_id, label: g.name }))
        : members.value.map((m) => ({ value: m.id, label: m.username }))
  )
  const periodState: Record<string, string> = {
    scheduled: '下一期',
    active: '生效中',
    expired: '已结束',
    blocked: '待调整'
  }
  const callState: Record<string, string> = {
    reserved: '预留',
    submitted: '调用中',
    settled: '已结算',
    released: '已释放',
    unknown: '待对账 / 冻结'
  }
  const entryState: Record<string, string> = {
    grant: '账期发放',
    charge: '调用消费',
    release: '取消释放',
    adjustment: '追加调整'
  }
  let generation = 0
  let controller: AbortController | undefined
  function credits(micro?: number | null) {
    return typeof micro === 'number' && Number.isFinite(micro)
      ? (micro / 1000000).toLocaleString('zh-CN', { maximumFractionDigits: 6 })
      : '—'
  }
  function date(s: string) {
    return s ? new Date(s).toLocaleString('zh-CN') : '—'
  }
  async function load() {
    const current = ++generation
    controller?.abort()
    controller = new AbortController()
    const signal = controller.signal
    loading.value = true
    error.value = ''
    try {
      const nextPlans = await api<Row[]>('/billing/plans', 'GET', undefined, signal)
      if (current !== generation) return
      plans.value = nextPlans
      const data = await api<Row>(
        `/billing${admin.value && tenant.value ? `?tenant_id=${encodeURIComponent(tenant.value)}` : ''}`,
        'GET',
        undefined,
        signal
      )
      if (current === generation) result.value = data
      if (!admin.value && data.full_bill) {
        const [u, m] = await Promise.all([
          api<Row>('/user-center', 'GET', undefined, signal),
          api<Row[]>('/members', 'GET', undefined, signal)
        ])
        if (current === generation) {
          groups.value = u.groups || []
          members.value = m
        }
      }
    } catch (e) {
      if (!signal.aborted) error.value = (e as Error).message
    } finally {
      if (current === generation) loading.value = false
    }
  }
  function openConfirmation() {
    Object.assign(payment, {
      plan_version_id: assignablePlans.value[0]?.id || '',
      payment_reference: '',
      idempotency_key: crypto.randomUUID()
    })
    paymentVerified.value = false
    saveError.value = ''
    confirmOpen.value = true
  }
  function latest(row: Row) {
    return !plans.value.some((p) => p.code === row.code && p.version > row.version)
  }
  async function publish(row: Row) {
    if (systemPlan(row) || saving.value) return
    saving.value = true
    saveError.value = ''
    try {
      await api('/billing/plans', 'POST', { code: row.code, base_version: row.version })
      await load()
    } catch (e) {
      saveError.value = (e as Error).message
    } finally {
      saving.value = false
    }
  }
  async function availability(row: Row) {
    if (systemPlan(row) || saving.value) return
    saving.value = true
    saveError.value = ''
    try {
      await api(`/billing/plans/${row.id}/availability`, 'PUT', {
        enabled: !row.enabled,
        expected_enabled: row.enabled
      })
      await load()
    } catch (e) {
      saveError.value = (e as Error).message
    } finally {
      saving.value = false
    }
  }
  async function confirm() {
    if (!tenant.value || !selectedPlan.value || !paymentVerified.value || saving.value) return
    saving.value = true
    saveError.value = ''
    try {
      await api('/billing/confirm', 'POST', {
        ...payment,
        tenant_id: tenant.value,
        amount_micro_usdt: selectedPlan.value.price_micro_usdt
      })
      confirmOpen.value = false
      await load()
    } catch (e) {
      saveError.value = (e as Error).message
    } finally {
      saving.value = false
    }
  }
  function selectLimit() {
    const item = result.value?.limits.find(
      (l: Row) => l.kind === limit.kind && l.scope === limit.scope
    )
    limit.credits = item ? item.limit_micro / 1000000 : 10000
    limit.version = item?.version || 0
    saveError.value = ''
  }
  async function saveLimit() {
    saving.value = true
    saveError.value = ''
    try {
      await api('/billing/limits', 'PUT', {
        kind: limit.kind,
        scope: limit.scope,
        limit_micro: Math.round(limit.credits * 1000000),
        version: limit.version
      })
      await load()
      selectLimit()
      limitsOpen.value = false
    } catch (e) {
      saveError.value = (e as Error).message
    } finally {
      saving.value = false
    }
  }
  watch(
    () => session.user?.id,
    () => {
      result.value = null
      tenant.value = ''
      void load()
    }
  )
  onMounted(load)
  onUnmounted(() => {
    generation++
    controller?.abort()
  })
</script>
<style scoped>
  .billing-panel {
    min-width: 0;
  }
  .billing-toolbar {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin-bottom: 20px;
  }
  .billing-toolbar h2 {
    font-size: 18px;
    margin: 0 auto 0 0;
  }
  .billing-toolbar .el-select {
    width: 200px;
    max-width: 100%;
  }
  .plan-comparison {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    border-bottom: 1px solid #e8eaed;
    margin-bottom: 20px;
  }
  .billing-workspace {
    padding-block: 16px;
    border-block: 1px solid var(--el-border-color-light);
  }
  .billing-workspace h3 {
    margin-top: 0;
  }
  .billing-workspace strong {
    display: block;
    margin-bottom: 12px;
    overflow-wrap: anywhere;
  }
  .billing-workspace p {
    color: var(--el-text-color-secondary);
    overflow-wrap: anywhere;
  }
  .plan-comparison > div {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 16px;
  }
  .plan-comparison strong {
    font-size: 18px;
  }
  .wallet-summary {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 16px;
    margin: 20px 0;
  }
  .wallet-summary > div {
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .wallet-summary span {
    color: #697080;
    font-size: 13px;
  }
  .wallet-summary strong {
    font-size: 20px;
    overflow-wrap: anywhere;
  }
  h3 {
    font-size: 16px;
    margin: 24px 0 12px;
  }
  .el-form .el-select {
    width: 100%;
  }
  @media (max-width: 647px) {
    .wallet-summary {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .plan-comparison > div {
      padding: 10px;
      font-size: 13px;
    }
    .billing-toolbar .el-select {
      width: 100%;
    }
  }
  @media (max-width: 359px) {
    .plan-comparison {
      grid-template-columns: 1fr;
    }
  }
</style>
