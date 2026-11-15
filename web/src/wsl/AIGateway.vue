<template>
  <section class="gateway-panel">
    <header
      ><h2>统一 AI 网关</h2
      ><ElButton :icon="Refresh" :loading="loading" aria-label="刷新 AI 网关" @click="load"
    /></header>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <section class="upstream-section">
      <div class="gateway-tools">
        <h3>上游状态</h3>
        <ElButton
          :icon="Refresh"
          :loading="inspecting"
          :disabled="!rateID || saving || testing"
          @click="inspectUpstream"
          >查询上游</ElButton
        >
        <ElButton
          :icon="Connection"
          :disabled="!rateID || saving || inspecting"
          @click="testOpen = true"
          >单次测试</ElButton
        >
      </div>
      <dl v-if="upstream" class="gateway-upstream-facts">
        <div class="gateway-balance">
          <dt>钱包可用余额</dt>
          <dd>{{
            upstream.remaining
              ? `${upstream.remaining} ${upstream.currency}`
              : stateLabel(upstream.usage_status)
          }}</dd>
        </div>
        <div v-if="upstream.credit_ceiling_micro != null">
          <dt>钱包参考上限（积分）</dt><dd>{{ credits(upstream.credit_ceiling_micro) }}</dd>
        </div>
        <div
          ><dt>USD 记账口径</dt
          ><dd>{{ upstream.usd_usdt_parity ? '1 USD = 1 USDT' : '尚未确认' }}</dd></div
        >
        <div>
          <dt>模型可用性</dt>
          <dd>{{
            upstream.models_status === 'ok'
              ? upstream.model_available
                ? '上游已列出'
                : '未列出此模型'
              : stateLabel(upstream.models_status)
          }}</dd>
        </div>
        <div>
          <dt>费率映射</dt>
          <dd
            >{{ stateLabel(upstream.mapping_status)
            }}{{ upstream.pricing_currency ? ` · ${upstream.pricing_currency}` : '' }}</dd
          >
        </div>
        <div
          ><dt>采样时间</dt
          ><dd>{{ new Date(upstream.checked_at).toLocaleString('zh-CN') }}</dd></div
        >
      </dl>
      <ElEmpty v-else description="尚未查询上游" :image-size="48" />
      <ElAlert v-if="probeMessage" :title="probeMessage" type="info" :closable="false" />
    </section>
    <ElForm label-position="top" class="gateway-form">
      <section class="gateway-form-section" aria-labelledby="gateway-budget-heading">
        <h3 id="gateway-budget-heading">运行与预算</h3>
        <div class="gateway-fields">
          <ElFormItem label="网关"><ElSwitch v-model="form.enabled" /></ElFormItem>
          <ElFormItem label="模型路由"><ElSwitch v-model="form.route_enabled" /></ElFormItem>
          <ElFormItem label="平台自然月预算（积分）">
            <ElInputNumber
              v-model="form.platform_credits"
              :min="0"
              :max="100000000"
              controls-position="right"
            />
          </ElFormItem>
          <ElFormItem label="超管测试月预算（积分）">
            <ElInputNumber
              v-model="form.test_credits"
              :min="0"
              :max="form.platform_credits"
              controls-position="right"
            />
          </ElFormItem>
        </div>
      </section>
      <section class="gateway-form-section" aria-labelledby="gateway-model-heading">
        <h3 id="gateway-model-heading">模型连接</h3>
        <div class="gateway-fields">
          <ElFormItem label="模型 HTTPS 来源">
            <ElInput
              v-model="form.base_url"
              placeholder="https://provider.example/v1"
              autocomplete="off"
            />
          </ElFormItem>
          <ElFormItem label="模型名称">
            <ElInput v-model="form.model" placeholder="留空保留当前模型" autocomplete="off" />
          </ElFormItem>
          <ElFormItem label="独立业务 API Key">
            <ElInput
              v-model="form.api_key"
              type="password"
              show-password
              autocomplete="new-password"
              placeholder="留空保留已配置密钥"
            />
          </ElFormItem>
          <ElFormItem label="路由别名"><ElInput model-value="default" disabled /></ElFormItem>
        </div>
      </section>
      <section class="gateway-form-section" aria-labelledby="gateway-rates-heading">
        <h3 id="gateway-rates-heading">计价与核验</h3>
        <div>
          <div class="gateway-fields">
            <ElFormItem
              v-for="rate in rateFields"
              :key="rate.key"
              :label="`${rate.label}（USDT / 百万 Token）`"
            >
              <ElInputNumber
                v-model="form.rates[rate.key]"
                :min="0"
                :max="1000000"
                :precision="6"
                :step="0.1"
                controls-position="right"
              />
            </ElFormItem>
          </div>
          <ElFormItem>
            <ElCheckbox v-model="form.usd_usdt_parity">USD 按 1:1 USDT 记账</ElCheckbox>
          </ElFormItem>
          <ElFormItem>
            <ElCheckbox v-model="form.verified">已核对模型、输入输出及缓存费率</ElCheckbox>
          </ElFormItem>
        </div>
      </section>
      <ElAlert v-if="saveError" :title="saveError" type="error" :closable="false" />
      <div class="gateway-save"
        ><ElButton type="primary" :loading="saving" :icon="Check" @click="save"
          >保存网关配置</ElButton
        ></div
      >
    </ElForm>
    <h3>调用与对账</h3>
    <ElTable :data="calls" empty-text="暂无模型调用">
      <ElTableColumn label="时间" min-width="165"
        ><template #default="{ row }">{{
          new Date(row.created_at).toLocaleString('zh-CN')
        }}</template></ElTableColumn
      >
      <ElTableColumn label="用途" min-width="100"
        ><template #default="{ row }">{{
          row.purpose === 'heatbot'
            ? '热群 AI'
            : row.purpose === 'test'
              ? '网关测试'
              : row.purpose === 'platform_assistant'
                ? '超管助手'
                : 'Web 群管助手'
        }}</template></ElTableColumn
      >
      <ElTableColumn label="预留 / 已扣积分" min-width="150"
        ><template #default="{ row }"
          >{{ credits(row.reserved_micro) }} / {{ credits(row.charged_micro) }}</template
        ></ElTableColumn
      >
      <ElTableColumn label="状态" min-width="120"
        ><template #default="{ row }">{{
          states[row.status] || row.status
        }}</template></ElTableColumn
      >
      <ElTableColumn label="操作" width="90"
        ><template #default="{ row }"
          ><ElButton v-if="row.status === 'unknown'" link type="primary" @click="openReconcile(row)"
            >对账</ElButton
          ><ElButton
            v-if="row.status === 'settled' && row.effective_charged_micro > 0"
            link
            type="primary"
            @click="openCorrection(row)"
            >纠错</ElButton
          ></template
        ></ElTableColumn
      >
    </ElTable>
    <ElDialog
      v-model="testOpen"
      title="单次网关测试"
      width="min(460px,95vw)"
      append-to-body
      :close-on-click-modal="false"
    >
      <ElForm label-position="top">
        <ElFormItem label="本次预留上限（积分）"
          ><ElInputNumber v-model="testBudget" :min="0.001" :max="100" :precision="3"
        /></ElFormItem>
        <ElDescriptions :column="1">
          <ElDescriptionsItem label="执行范围">1 次请求 · 最多 32 个输出 token</ElDescriptionsItem>
          <ElDescriptionsItem label="记账来源">平台预算 / 超管测试预算</ElDescriptionsItem>
        </ElDescriptions>
        <ElAlert v-if="saveError" :title="saveError" type="error" :closable="false" />
        <ElAlert v-if="testResult" :title="testResult" type="info" :closable="false" />
      </ElForm>
      <template #footer>
        <ElButton @click="testOpen = false">关闭</ElButton>
        <ElButton :icon="Connection" type="primary" :loading="testing" @click="runTest"
          >执行测试</ElButton
        >
      </template>
    </ElDialog>
    <ElDialog
      v-model="reconcileOpen"
      title="录入已核验的供应商用量"
      width="min(500px,calc(100vw - 24px))"
      append-to-body
      :close-on-click-modal="false"
    >
      <ElForm label-position="top">
        <ElFormItem label="输入 Token（含缓存）"
          ><ElInputNumber v-model="usage.prompt_tokens" :min="0" :max="9999999"
        /></ElFormItem>
        <ElFormItem label="输出 Token"
          ><ElInputNumber v-model="usage.completion_tokens" :min="0" :max="9999999"
        /></ElFormItem>
        <ElFormItem label="缓存命中 Token"
          ><ElInputNumber
            v-model="usage.prompt_tokens_details.cached_tokens"
            :min="0"
            :max="usage.prompt_tokens"
        /></ElFormItem>
        <ElFormItem label="缓存写入 Token"
          ><ElInputNumber
            v-model="usage.prompt_tokens_details.cache_write_tokens"
            :min="0"
            :max="usage.prompt_tokens"
        /></ElFormItem>
        <ElFormItem label="核验依据编号"><ElInput v-model="reason" maxlength="200" /></ElFormItem>
        <ElAlert v-if="saveError" :title="saveError" type="error" :closable="false" />
      </ElForm>
      <template #footer
        ><ElButton @click="reconcileOpen = false">取消</ElButton
        ><ElButton type="primary" :loading="saving" @click="reconcile"
          >确认用量结算</ElButton
        ></template
      >
    </ElDialog>
    <ElDialog
      v-model="correctionOpen"
      title="退回已核验的误扣积分"
      width="min(480px,calc(100vw - 24px))"
      append-to-body
      :close-on-click-modal="false"
    >
      <ElForm label-position="top">
        <ElFormItem label="退回积分"
          ><ElInputNumber
            v-model="correction.amount"
            :min="0.000001"
            :max="correction.max"
            :precision="6"
        /></ElFormItem>
        <ElFormItem label="核验依据编号"
          ><ElInput v-model="correction.reason" maxlength="200"
        /></ElFormItem>
        <ElAlert v-if="saveError" :title="saveError" type="error" :closable="false" />
      </ElForm>
      <template #footer
        ><ElButton @click="correctionOpen = false">取消</ElButton
        ><ElButton type="primary" :loading="saving" @click="correct"
          >追加纠错记录</ElButton
        ></template
      >
    </ElDialog>
  </section>
</template>
<script setup lang="ts">
  import { onMounted, onUnmounted, reactive, ref } from 'vue'
  import { Refresh, Check, Connection } from '@element-plus/icons-vue'
  import { api, type Row } from './api'
  const loading = ref(false),
    saving = ref(false),
    error = ref(''),
    saveError = ref(''),
    calls = ref<Row[]>([]),
    reconcileOpen = ref(false),
    call = ref(''),
    reason = ref('')
  const form = reactive<Row>({
    enabled: false,
    route_enabled: false,
    platform_credits: 0,
    test_credits: 0,
    base_url: '',
    model: '',
    api_key: '',
    usd_usdt_parity: false,
    verified: false,
    version: 0,
    rates: { input: 0, output: 0, cache_read: 0, cache_write: 0 }
  })
  const upstream = ref<Row | null>(null),
    rateID = ref('')
  const inspecting = ref(false),
    testing = ref(false),
    testOpen = ref(false)
  const testBudget = ref(1),
    testResult = ref(''),
    probeMessage = ref('')
  let testKey = ''
  const stateLabel = (value: string) =>
    (
      ({
        ok: '查询成功',
        unavailable: '暂不可用',
        access_denied: '上游拒绝访问',
        unsupported: '上游未提供此接口',
        invalid_response: '返回格式不兼容',
        unsupported_schema: '额度格式尚未适配',
        rates_available: '已生成费率草稿，待确认保存',
        model_price_missing: '上游未提供此模型报价',
        incomplete_prices: '报价缺少完整缓存费率',
        currency_conversion_required: '待确认到 USDT 的换算依据',
        ambiguous_model_prices: '同名模型存在多组报价',
        model_not_available: '模型列表尚未确认此模型',
        unsupported_cache_tiers: '缓存分档尚未适配'
      }) as Record<string, string>
    )[value] || '未知'
  const rateFields = [
    { key: 'input', label: '普通输入' },
    { key: 'output', label: '输出' },
    { key: 'cache_read', label: '缓存命中' },
    { key: 'cache_write', label: '缓存写入' }
  ]
  const states: Record<string, string> = {
    reserved: '已预留',
    submitted: '调用中',
    settled: '已结算',
    released: '已释放',
    unknown: '未知 / 冻结'
  }
  const usage = reactive({
    prompt_tokens: 0,
    completion_tokens: 0,
    prompt_tokens_details: { cached_tokens: 0, cache_write_tokens: 0 }
  })
  const controller = new AbortController()
  const correctionOpen = ref(false),
    correction = reactive({ call: '', amount: 0, max: 0, reason: '', idempotency_key: '' })
  function credits(v: number) {
    return (v / 1000000).toLocaleString('zh-CN', { maximumFractionDigits: 6 })
  }
  async function load() {
    loading.value = true
    error.value = ''
    try {
      const data = await api<Row>('/ai-gateway', 'GET', undefined, controller.signal)
      if (controller.signal.aborted) return
      const route = data.runtime.routes[0]
      rateID.value = route?.rate_id || ''
      upstream.value = data.runtime.upstream || null
      form.enabled = data.settings.enabled
      form.platform_credits = data.settings.platform_limit_micro / 1000000
      form.test_credits = data.settings.test_limit_micro / 1000000
      form.version = data.settings.version
      form.base_url = data.provider?.base_url || ''
      form.model = data.provider?.model || ''
      form.usd_usdt_parity = data.provider?.usd_usdt_parity === true
      form.route_enabled = route?.enabled || false
      form.verified = route?.verified || false
      for (const rate of rateFields) form.rates[rate.key] = (route?.rates[rate.key] || 0) / 1000000
      calls.value = data.runtime.calls
    } catch (e) {
      if (!controller.signal.aborted) error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }
  async function save() {
    saving.value = true
    saveError.value = ''
    try {
      const rates = Object.fromEntries(
        rateFields.map((r) => [r.key, Math.round(form.rates[r.key] * 1000000)])
      )
      await api('/ai-gateway', 'PUT', {
        settings: {
          enabled: form.enabled,
          platform_limit_micro: Math.round(form.platform_credits * 1000000),
          test_limit_micro: Math.round(form.test_credits * 1000000),
          version: form.version
        },
        route: {
          alias: 'default',
          version: 0,
          enabled: form.route_enabled,
          verified: form.verified,
          usd_usdt_parity: form.usd_usdt_parity,
          rates,
          model: { base_url: form.base_url, model: form.model, api_key: form.api_key }
        }
      })
      form.api_key = ''
      await load()
    } catch (e) {
      saveError.value = (e as Error).message
    } finally {
      saving.value = false
    }
  }
  async function inspectUpstream() {
    inspecting.value = true
    saveError.value = ''
    probeMessage.value = ''
    try {
      const result = await api<Row>(
        '/ai-gateway/inspect',
        'POST',
        { rate_id: rateID.value },
        controller.signal
      )
      if (controller.signal.aborted) return
      upstream.value = result
      if (result.mapped_rates) {
        for (const key of ['input', 'output', 'cache_read', 'cache_write'])
          form.rates[key] = result.mapped_rates[key] / 1000000
        form.verified = false
        probeMessage.value = '费率草稿已更新，待核对保存'
      } else {
        probeMessage.value = stateLabel(result.mapping_status)
      }
    } catch (e) {
      if (!controller.signal.aborted) saveError.value = (e as Error).message
    } finally {
      inspecting.value = false
    }
  }
  async function runTest() {
    testing.value = true
    saveError.value = ''
    if (!testKey) testKey = crypto.randomUUID()
    try {
      const result = await api<Row>(
        '/ai-gateway/test',
        'POST',
        {
          rate_id: rateID.value,
          idempotency_key: testKey,
          max_cost_micro: Math.round(testBudget.value * 1000000)
        },
        controller.signal
      )
      if (controller.signal.aborted) return
      testResult.value = `记录 ${result.id} · ${states[result.status] || result.status} · 扣费 ${credits(result.charged_micro)} 积分${result.result === 'generation_failed' ? ' · 文本生成未成功' : ''}`
      if (result.status === 'settled' || result.status === 'released') testKey = ''
      await load()
    } catch (e) {
      if (!controller.signal.aborted) saveError.value = (e as Error).message
    } finally {
      testing.value = false
    }
  }
  function openReconcile(row: Row) {
    call.value = row.id
    reason.value = ''
    saveError.value = ''
    usage.prompt_tokens = 0
    usage.completion_tokens = 0
    usage.prompt_tokens_details.cached_tokens = 0
    usage.prompt_tokens_details.cache_write_tokens = 0
    reconcileOpen.value = true
  }
  async function reconcile() {
    saving.value = true
    saveError.value = ''
    try {
      await api(`/ai-gateway/calls/${call.value}/reconcile`, 'POST', {
        usage: { ...usage, total_tokens: usage.prompt_tokens + usage.completion_tokens },
        reason: reason.value
      })
      reconcileOpen.value = false
      await load()
    } catch (e) {
      saveError.value = (e as Error).message
    } finally {
      saving.value = false
    }
  }
  function openCorrection(row: Row) {
    Object.assign(correction, {
      call: row.id,
      amount: 0,
      max: row.effective_charged_micro / 1000000,
      reason: '',
      idempotency_key: crypto.randomUUID()
    })
    saveError.value = ''
    correctionOpen.value = true
  }
  async function correct() {
    saving.value = true
    saveError.value = ''
    try {
      await api(`/ai-gateway/calls/${correction.call}/adjust`, 'POST', {
        amount_micro: Math.round(correction.amount * 1000000),
        reason: correction.reason,
        idempotency_key: correction.idempotency_key
      })
      correctionOpen.value = false
      await load()
    } catch (e) {
      saveError.value = (e as Error).message
    } finally {
      saving.value = false
    }
  }
  onMounted(load)
  onUnmounted(() => controller.abort())
</script>
<style scoped>
  .gateway-panel {
    min-width: 0;
  }
  .upstream-section {
    margin-bottom: 24px;
  }
  .gateway-upstream-facts {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 24px 32px;
    margin: 20px 0;
    padding: 22px 24px;
    background: #f8faf8;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 6px;
  }
  .gateway-upstream-facts > div {
    min-width: 0;
  }
  .gateway-upstream-facts dt {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .gateway-upstream-facts dd {
    margin: 8px 0 0;
    font-size: 13px;
    font-weight: 500;
    overflow-wrap: anywhere;
    font-variant-numeric: tabular-nums;
  }
  .gateway-balance dd {
    font-size: 21px;
    line-height: 1.4;
  }
  .gateway-form-section {
    display: grid;
    grid-template-columns: 120px minmax(0, 1fr);
    gap: 28px;
    padding: 24px 0 6px;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .gateway-form-section:first-child {
    padding-top: 0;
  }
  .gateway-form-section:last-of-type {
    border: 0;
  }
  .gateway-form-section h3 {
    margin: 0;
    padding-top: 4px;
    font-size: 14px;
    font-weight: 600;
  }
  .gateway-save {
    display: flex;
    justify-content: flex-end;
  }
  .gateway-tools {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
    margin-bottom: 12px;
  }
  .gateway-tools h3 {
    margin: 0 auto 0 0;
  }
  .upstream-section :deep(.el-descriptions__content) {
    overflow-wrap: anywhere;
    word-break: break-word;
  }
  .gateway-panel > header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 20px;
  }
  h2 {
    font-size: 18px;
    margin: 0;
  }
  h3 {
    font-size: 16px;
    margin: 28px 0 12px;
  }
  .gateway-form {
    max-width: 900px;
  }
  .gateway-fields {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 0 20px;
  }
  .gateway-fields .el-input-number {
    width: 100%;
  }
  .gateway-panel :deep(.el-checkbox__label) {
    white-space: normal;
    overflow-wrap: anywhere;
  }
  @media (max-width: 647px) {
    .gateway-fields {
      grid-template-columns: 1fr;
    }
    .gateway-upstream-facts {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 24px 16px;
      padding: 18px;
    }
    .gateway-form-section {
      grid-template-columns: minmax(0, 1fr);
      gap: 20px;
    }
  }
</style>
