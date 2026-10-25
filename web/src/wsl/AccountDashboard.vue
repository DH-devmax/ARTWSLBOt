<template>
  <div class="account-dashboard" :aria-busy="loading" data-testid="account-dashboard">
    <div class="account-dashboard-heading">
      <div>
        <h2>{{ account.name }}</h2>
        <span>{{ workspace === 'maibot' ? '热群机器人' : '旺商聊机器人' }} · 独立账号工作台</span>
      </div>
      <div class="account-dashboard-actions">
        <ElSwitch
          v-if="admin && sample?.receiver"
          :model-value="sample.receiver.enabled"
          :loading="receiverSaving"
          :disabled="receiverSaving || sample.account.auth_state !== 'authenticated'"
          aria-label="消息接收"
          active-text="消息接收"
          @change="toggleReceiver"
        />
        <ElTooltip content="刷新账号指标">
          <ElButton
            text
            :icon="Refresh"
            aria-label="刷新账号指标"
            :loading="loading"
            @click="load"
          />
        </ElTooltip>
        <ElTooltip v-if="admin" content="删除机器人">
          <ElButton
            text
            type="danger"
            :icon="Delete"
            :aria-label="`删除机器人 ${account.name}`"
            @click="$emit('remove', account)"
          />
        </ElTooltip>
      </div>
    </div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElAlert
      v-if="stale && sample"
      title="采样失败，当前显示上一次数据"
      type="warning"
      :closable="false"
    />
    <ElSkeleton v-if="!sample && loading" :rows="9" animated />
    <ElEmpty v-else-if="!sample" description="暂无账号指标" :image-size="64" />
    <template v-else>
      <div class="account-sample-meta">
        <span>采样于 {{ date(sample.sampled_at) }}</span>
        <ElTag v-if="sample.visibility === 'tenant_groups'" type="info" effect="plain">
          仅本租户授权群
        </ElTag>
      </div>
      <div class="account-kpis">
        <div
          ><span>已授权群组</span><strong>{{ sample.groups.authorized }}</strong></div
        >
        <div>
          <span>{{ workspace === 'maibot' ? '已启用聊天群' : '已启用群管' }}</span>
          <strong>{{
            workspace === 'maibot' ? sample.groups.ai_enabled : sample.groups.management_enabled
          }}</strong>
        </div>
        <div>
          <ElTooltip content="最近 60 秒内已入库的去重消息数 / 60；不代表原生接口调用频率">
            <span>业务入库速率 · 60 秒</span>
          </ElTooltip>
          <strong>{{ sample.performance.per_second.toFixed(2) }}<small>条/秒</small></strong>
        </div>
        <div>
          <span>待处理消息 / 操作</span>
          <strong
            >{{ sample.inbox.pending + sample.inbox.unresolved
            }}<small>/ {{ sample.operations.queued }}</small></strong
          >
        </div>
      </div>
      <div class="account-status-grid">
        <section class="account-status-section" aria-label="账号健康度">
          <div class="account-section-heading">
            <h3>账号健康度</h3>
            <ElTag :type="healthType(sample.health.status)">{{
              healthLabel(sample.health.status)
            }}</ElTag>
          </div>
          <dl class="account-status-list">
            <div
              ><dt>原生登录</dt
              ><dd>{{
                sample.account.auth_state === 'authenticated' ? '已登录' : '未登录'
              }}</dd></div
            >
            <div
              ><dt>原生动作</dt
              ><dd>{{ sample.account.connection_enabled ? '已启用' : '未启用' }}</dd></div
            >
            <div
              ><dt>连接记录</dt><dd>{{ connectionLabel(sample.account.runtime_state) }}</dd></div
            >
            <div
              ><dt>最后入库</dt><dd>{{ date(sample.performance.last_inserted_at) }}</dd></div
            >
            <div
              ><dt>AI 已启用 / 待启用</dt
              ><dd>{{ sample.groups.ai_enabled }} / {{ sample.groups.ai_pending }}</dd></div
            >
          </dl>
          <div v-if="!sample.runtime_observed" class="account-observation-note"
            ><ElIcon><InfoFilled /></ElIcon>实时连接、心跳与 ACK 尚无监测数据</div
          >
          <div v-for="reason in visibleReasons" :key="reason" class="account-reason">{{
            reasonLabel(reason)
          }}</div>
        </section>
        <section class="account-status-section" aria-label="堵塞状态">
          <div class="account-section-heading">
            <h3>堵塞与待处理</h3>
            <ElTag :type="healthType(sample.backlog.status)">{{
              backlogLabel(sample.backlog.status)
            }}</ElTag>
          </div>
          <dl class="account-status-list">
            <div
              ><dt>待业务处理</dt><dd>{{ sample.inbox.pending }} 条</dd></div
            >
            <div
              ><dt>待身份解析</dt><dd>{{ sample.inbox.unresolved }} 条</dd></div
            >
            <div
              ><dt>隔离消息</dt
              ><dd :class="{ 'account-value-danger': sample.inbox.quarantined > 0 }"
                >{{ sample.inbox.quarantined }} 条</dd
              ></div
            >
            <div
              ><dt>操作排队 / 执行中</dt
              ><dd>{{ sample.operations.queued }} / {{ sample.operations.running }}</dd></div
            >
            <div
              ><dt>操作结果未知</dt
              ><dd :class="{ 'account-value-danger': sample.operations.unknown > 0 }">{{
                sample.operations.unknown
              }}</dd></div
            >
            <div
              ><dt>近 15 分钟失败操作</dt><dd>{{ sample.operations.failed_15m }}</dd></div
            >
            <div
              ><dt>最久消息等待</dt><dd>{{ age(sample.inbox.oldest_pending_at) }}</dd></div
            >
            <div
              ><dt>最久操作排队</dt><dd>{{ age(sample.operations.oldest_queued_at) }}</dd></div
            >
          </dl>
          <span class="account-observation-note"
            >积压阈值 {{ sample.backlog_threshold_seconds }} 秒 · 数据库待处理队列</span
          >
        </section>
      </div>
      <section
        v-if="admin && sample.receiver"
        class="account-status-section receiver-section"
        aria-label="消息接收运行状态"
      >
        <div class="account-section-heading">
          <h3>消息接收</h3>
          <ElTag :type="sample.runtime_observed ? 'success' : 'info'">
            {{
              sample.runtime_observed
                ? connectionLabel(sample.receiver.snapshot?.state)
                : '暂无实时采样'
            }}
          </ElTag>
        </div>
        <ElAlert
          v-if="sample.receiver.snapshot?.error && sample.receiver.enabled"
          :title="receiverError(sample.receiver.snapshot.error)"
          type="warning"
          :closable="false"
        />
        <dl v-if="sample.receiver.snapshot" class="account-status-list">
          <div
            ><dt>接收速率 · 60 秒</dt
            ><dd>{{
              sample.receiver.snapshot.per_second == null
                ? '暂无监测'
                : `${sample.receiver.snapshot.per_second.toFixed(2)} 条/秒`
            }}</dd></div
          >
          <div
            ><dt>最近心跳</dt><dd>{{ date(sample.receiver.snapshot.heartbeat_at) }}</dd></div
          >
          <div
            ><dt>心跳往返</dt><dd>{{ ms(sample.receiver.snapshot.heartbeat_ms) }}</dd></div
          >
          <div
            ><dt>接收队列</dt
            ><dd
              >{{ sample.receiver.snapshot.queue_depth }} /
              {{ sample.receiver.snapshot.queue_capacity }}</dd
            ></div
          >
          <div
            ><dt>收包 / 消息</dt
            ><dd
              >{{ sample.receiver.snapshot.frames }} / {{ sample.receiver.snapshot.messages }}</dd
            ></div
          >
          <div
            ><dt>重复消息 / ACK</dt
            ><dd
              >{{ sample.receiver.snapshot.duplicates }} / {{ sample.receiver.snapshot.acks }}</dd
            ></div
          >
          <div
            ><dt>隔离包 / 重连</dt
            ><dd
              >{{ sample.receiver.snapshot.quarantined }} /
              {{ sample.receiver.snapshot.reconnects }}</dd
            ></div
          >
          <div
            ><dt>可靠落盘 P95</dt><dd>{{ ms(sample.receiver.snapshot.durable_p95_ms) }}</dd></div
          >
          <div
            ><dt>ACK 写出 P95</dt><dd>{{ ms(sample.receiver.snapshot.ack_p95_ms) }}</dd></div
          >
          <div
            ><dt>最近接包</dt><dd>{{ date(sample.receiver.snapshot.last_received_at) }}</dd></div
          >
          <div
            ><dt>最近 ACK 写出</dt><dd>{{ date(sample.receiver.snapshot.last_ack_at) }}</dd></div
          >
        </dl>
        <p class="account-observation-note"
          >接收事件待群映射；业务处理暂停。计数为本次接收周期，延迟取最近至多 1024 条样本。</p
        >
      </section>
      <section class="account-performance" aria-label="消息性能">
        <div class="account-section-heading">
          <h3>消息性能</h3>
          <span>最近 15 分钟 · 条/分钟 · 当前分钟未结束</span>
        </div>
        <div
          ref="chartElement"
          class="account-message-chart"
          role="img"
          :aria-label="chartLabel"
        ></div>
        <div class="account-latencies">
          <div
            ><span>最近 60 秒入库</span
            ><strong>{{ sample.performance.inserted_60s }}<small>条</small></strong></div
          >
          <div
            ><span>入库延迟 P50</span><strong>{{ ms(sample.performance.p50_ms) }}</strong></div
          >
          <div
            ><span>入库延迟 P95</span><strong>{{ ms(sample.performance.p95_ms) }}</strong></div
          >
          <div
            ><span>入库延迟 P99</span><strong>{{ ms(sample.performance.p99_ms) }}</strong></div
          >
        </div>
        <div class="account-performance-meta">
          <ElTooltip
            content="收包时间至数据库 INSERT 时间，早于事务提交和 ACK；仅统计最近 60 秒非负时间差样本"
          >
            <span
              >有效延迟样本 {{ sample.performance.latency_samples }} · 异常时间样本
              {{ sample.performance.invalid_timing_samples }}</span
            >
          </ElTooltip>
          <span v-if="!sample.runtime_observed">可靠落盘 / ACK 延迟：暂无监测</span>
          <span>原生 HTTP 排队 / 限流：暂无监测</span>
        </div>
      </section>
    </template>
  </div>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import { Delete, InfoFilled, Refresh } from '@element-plus/icons-vue'
  import { init, use, type EChartsType } from 'echarts/core'
  import { BarChart } from 'echarts/charts'
  import { GridComponent, TooltipComponent } from 'echarts/components'
  import { CanvasRenderer } from 'echarts/renderers'
  import { api, type Row } from './api'

  use([BarChart, GridComponent, TooltipComponent, CanvasRenderer])
  const props = defineProps<{
    account: Row
    workspace: 'wsl' | 'maibot'
    owner: string
    admin: boolean
    refreshToken: string
  }>()
  defineEmits<{ remove: [account: Row] }>()
  const sample = ref<Row | null>(null),
    loading = ref(false),
    error = ref(''),
    stale = ref(false)
  const receiverSaving = ref(false)
  const chartElement = ref<HTMLElement>()
  let controller: AbortController | undefined,
    generation = 0
  let chart: EChartsType | undefined, observer: ResizeObserver | undefined
  const scope = () => `${props.owner}:${props.workspace}:${props.account.id}`
  const date = (value?: string) => (value ? new Date(value).toLocaleString('zh-CN') : '暂无记录')
  const ms = (value: number | null) => (value == null ? '暂无样本' : `${value.toFixed(1)} ms`)
  const age = (value?: string) => {
    if (!value || !sample.value) return '无待处理'
    const seconds = Math.max(
      0,
      Math.floor((Date.parse(sample.value.sampled_at) - Date.parse(value)) / 1000)
    )
    return seconds < 60 ? `${seconds} 秒` : `${Math.floor(seconds / 60)} 分 ${seconds % 60} 秒`
  }
  const healthLabel = (status: string) =>
    ({
      attention: '需复核',
      backlogged: '存在积压',
      not_authenticated: '未登录',
      disabled: '接入停用',
      disconnected: '未连接',
      unobserved: '实时健康度未监测',
      healthy: '接收正常'
    })[status] || '暂无监测'
  const backlogLabel = (status: string) =>
    ({
      attention: '需复核',
      backlogged: '存在积压',
      pending: '有待处理',
      empty: '无数据库积压'
    })[status] || '暂无数据'
  const healthType = (status: string) =>
    status === 'healthy'
      ? 'success'
      : status === 'attention'
        ? 'danger'
        : ['backlogged', 'pending'].includes(status)
          ? 'warning'
          : 'info'
  const connectionLabel = (status: string) =>
    ({
      connected: '已连接',
      reconnecting: '重连中',
      connecting: '连接中',
      disconnected: '未连接',
      stopped: '已停止',
      blocked: '已阻断'
    })[status] || '未知'
  const reasonLabel = (reason: string) =>
    ({
      old_pending: '有消息或操作等待超过积压阈值',
      quarantined: '有隔离消息待排查',
      unknown_operations: '有原生操作结果未知，需复核',
      invalid_timing: '有消息时间差异常，已从延迟统计排除',
      receiver_blocked: '接收器已阻断，需核对原因',
      heartbeat_stale: '心跳观测已过期',
      receiver_queue: '接收队列接近上限'
    })[reason] || reason
  const visibleReasons = computed(() =>
    (sample.value?.health.reasons || []).filter((r: string) => r !== 'runtime_unobserved')
  )
  const chartLabel = computed(() =>
    sample.value
      ? `${props.account.name} 最近 15 分钟入库消息共 ${sample.value.performance.series.reduce((sum: number, b: Row) => sum + b.count, 0)} 条`
      : '暂无消息性能数据'
  )

  const receiverError = (code: string) =>
    ({
      im_kicked: '原生连接被踢下线，请先核对其他客户端',
      im_auth_rejected: '消息登录令牌被拒绝，请停用后重新登录',
      native_push_boundary_unknown: '收到无法核验边界的消息包，已隔离并暂停恢复水位',
      receiver_configuration_unavailable: '接收配置或会话不可用',
      receiver_configuration_invalid: '接收配置与已登录会话不匹配',
      native_reauth_required: '原生会话需要重新验证，请先停用接收',
      native_rate_limited: '原生读取限流，等待恢复',
      receiver_database_unavailable: '数据库暂时不可用，未确认未落盘消息',
      im_connection_failed: '消息服务器暂时连接失败，等待重连',
      im_discovery_failed: '消息服务器发现失败，等待重试'
    })[code] || '接收连接异常，正在等待恢复或复核'

  async function toggleReceiver(value: unknown) {
    if (!sample.value?.receiver || receiverSaving.value) return
    const originalScope = scope(),
      workspace = props.workspace,
      accountID = props.account.id
    receiverSaving.value = true
    try {
      await api(
        `/accounts/${accountID}/receiver`,
        'PUT',
        {
          enabled: !!value,
          revision: sample.value.receiver.revision
        },
        undefined,
        workspace
      )
      if (originalScope === scope()) await load()
    } catch (e) {
      if (originalScope === scope()) error.value = (e as Error).message
    } finally {
      receiverSaving.value = false
    }
  }

  async function load() {
    controller?.abort()
    const current = ++generation,
      originalScope = scope(),
      workspace = props.workspace,
      accountID = props.account.id
    const activeController = new AbortController()
    controller = activeController
    loading.value = true
    try {
      const result = await api<Row>(
        `/accounts/${accountID}/dashboard`,
        'GET',
        undefined,
        activeController.signal,
        workspace
      )
      if (current !== generation || originalScope !== scope() || activeController.signal.aborted)
        return
      if (result?.account?.id !== accountID || result.account.workspace !== workspace)
        throw new Error('账号采样范围不匹配')
      sample.value = result
      error.value = ''
      stale.value = false
    } catch (e) {
      if (current !== generation || originalScope !== scope() || activeController.signal.aborted)
        return
      error.value = (e as Error).message
      stale.value = !!sample.value
    } finally {
      if (current === generation) loading.value = false
    }
  }
  function renderChart() {
    if (!chart || !sample.value) return
    const series = sample.value.performance.series as { at: string; count: number }[]
    chart.setOption(
      {
        animation: false,
        grid: { left: 42, right: 12, top: 18, bottom: 30 },
        tooltip: { trigger: 'axis', confine: true },
        xAxis: {
          type: 'category',
          data: series.map((b) =>
            new Date(b.at).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
          ),
          axisTick: { show: false },
          axisLine: { lineStyle: { color: '#d8dce5' } },
          axisLabel: { color: '#687084' }
        },
        yAxis: {
          type: 'value',
          minInterval: 1,
          splitLine: { lineStyle: { color: '#edf0f4' } },
          axisLabel: { color: '#687084' }
        },
        series: [
          {
            name: '入库消息',
            type: 'bar',
            data: series.map((b) => b.count),
            barMaxWidth: 28,
            itemStyle: { color: '#4361ee', borderRadius: [3, 3, 0, 0] }
          }
        ]
      },
      true
    )
  }
  watch(
    [() => props.owner, () => props.workspace, () => props.account.id],
    () => {
      sample.value = null
      error.value = ''
      stale.value = false
      void load()
    },
    { immediate: true }
  )
  watch(
    () => props.refreshToken,
    () => {
      if (!document.hidden) void load()
    }
  )
  watch(
    chartElement,
    (element) => {
      observer?.disconnect()
      chart?.dispose()
      chart = undefined
      if (element) {
        chart = init(element)
        observer = new ResizeObserver(() => chart?.resize())
        observer.observe(element)
        renderChart()
      }
    },
    { flush: 'post' }
  )
  watch(sample, renderChart, { flush: 'post' })
  const onVisible = () => {
    if (!document.hidden) void load()
  }
  onMounted(() => document.addEventListener('visibilitychange', onVisible))
  onBeforeUnmount(() => {
    ++generation
    controller?.abort()
    observer?.disconnect()
    chart?.dispose()
    document.removeEventListener('visibilitychange', onVisible)
  })
</script>

<style scoped>
  .account-dashboard {
    min-width: 0;
  }
  .receiver-section {
    margin-top: 20px;
  }
  .account-dashboard-heading,
  .account-section-heading,
  .account-sample-meta,
  .account-dashboard-actions {
    display: flex;
    align-items: center;
    gap: 12px;
  }
  .account-dashboard-heading,
  .account-section-heading {
    justify-content: space-between;
  }
  .account-dashboard-heading {
    margin-bottom: 18px;
    flex-wrap: wrap;
  }
  .account-dashboard-heading h2 {
    margin: 0 0 6px;
    font-size: 20px;
    overflow-wrap: anywhere;
  }
  .account-dashboard-heading span,
  .account-sample-meta,
  .account-section-heading > span,
  .account-performance-meta,
  .account-observation-note {
    color: #727b8e;
    font-size: 12px;
  }
  .account-dashboard .el-alert {
    margin-bottom: 14px;
  }
  .account-sample-meta {
    margin: 16px 0;
    flex-wrap: wrap;
  }
  .account-kpis {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    border: 1px solid #e4e8ef;
    background: #fff;
  }
  .account-kpis > div {
    padding: 20px;
    border-right: 1px solid #edf0f4;
    min-width: 0;
  }
  .account-kpis > div:last-child {
    border-right: 0;
  }
  .account-kpis span,
  .account-latencies span {
    display: block;
    color: #727b8e;
    font-size: 13px;
    line-height: 1.5;
  }
  .account-kpis strong {
    display: block;
    font-size: 28px;
    margin-top: 14px;
    overflow-wrap: anywhere;
    font-weight: 600;
  }
  .account-kpis small,
  .account-latencies small {
    font-size: 12px;
    margin-left: 6px;
    color: #727b8e;
    font-weight: 400;
  }
  .account-status-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    margin-top: 24px;
    gap: 28px;
  }
  .account-status-section {
    min-width: 0;
    border-top: 2px solid #dfe4ed;
    padding-top: 16px;
  }
  .account-section-heading {
    flex-wrap: wrap;
    min-height: 26px;
  }
  .account-section-heading h3 {
    font-size: 15px;
    margin: 0;
  }
  .account-status-list {
    margin: 12px 0;
  }
  .account-status-list > div {
    display: grid;
    grid-template-columns: minmax(110px, 1fr) minmax(0, 1.4fr);
    gap: 12px;
    padding: 9px 0;
    border-bottom: 1px solid #e8ecf2;
    font-size: 13px;
  }
  .account-status-list dt {
    color: #727b8e;
  }
  .account-status-list dd {
    margin: 0;
    text-align: right;
    overflow-wrap: anywhere;
  }
  .account-observation-note {
    display: flex;
    align-items: flex-start;
    gap: 6px;
    line-height: 1.6;
  }
  .account-observation-note .el-icon {
    flex-shrink: 0;
    margin-top: 3px;
  }
  .account-value-danger,
  .account-reason {
    color: #c34442;
  }
  .account-reason {
    margin-top: 8px;
    font-size: 12px;
  }
  .account-performance {
    margin-top: 26px;
    border-top: 2px solid #dfe4ed;
    padding-top: 16px;
  }
  .account-message-chart {
    height: 210px;
    width: 100%;
    margin: 12px 0;
  }
  .account-latencies {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    gap: 16px;
  }
  .account-latencies strong {
    display: block;
    font-size: 18px;
    margin-top: 10px;
    overflow-wrap: anywhere;
    font-weight: 500;
  }
  .account-performance-meta {
    display: flex;
    flex-wrap: wrap;
    gap: 10px 24px;
    margin-top: 20px;
    line-height: 1.7;
  }
  @media (max-width: 900px) {
    .account-kpis {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .account-kpis > div {
      border-bottom: 1px solid #edf0f4;
    }
    .account-status-grid {
      gap: 24px;
    }
  }
  @media (max-width: 600px) {
    .account-status-grid {
      grid-template-columns: minmax(0, 1fr);
    }
    .account-kpis > div {
      padding: 16px 12px;
    }
    .account-kpis strong {
      font-size: 24px;
    }
    .account-latencies {
      grid-template-columns: repeat(2, minmax(0, 1fr));
      gap: 20px 14px;
    }
    .account-dashboard-actions {
      flex-wrap: wrap;
      gap: 4px;
    }
    .account-dashboard-heading {
      gap: 16px;
    }
  }
</style>
