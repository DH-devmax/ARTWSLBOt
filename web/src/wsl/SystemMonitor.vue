<template>
  <section class="data-section runtime-monitor" data-testid="runtime-monitor">
    <div class="monitor-header">
      <div
        ><span class="monitor-platform">{{ workspaceName }}</span
        ><div class="metric-note"
          >快照 {{ stamp(sample.updated_at) }} · 数据库 {{ stamp(sample.database_sampled_at) }}</div
        ></div
      >
      <div class="monitor-toolbar">
        <ElTag
          disable-transitions
          :type="live === '实时连接' && !oldSample ? 'success' : 'info'"
          effect="plain"
          >{{ live }}</ElTag
        >
        <ElTooltip content="刷新当前视图"
          ><ElButton
            text
            :icon="Refresh"
            aria-label="刷新当前视图"
            :loading="loading"
            @click="refresh"
        /></ElTooltip>
      </div>
    </div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" class="console-alert" />
    <ElAlert
      v-if="sample.updated_at && oldSample"
      title="实时快照已过期，正在等待新采样。"
      type="warning"
      :closable="false"
      class="console-alert"
    />
    <ElAlert
      v-else-if="sample.updated_at && !sampled"
      title="数据库采样不可用或已过期，保留上次观测时间。"
      type="warning"
      :closable="false"
      class="console-alert"
    />
    <ElTabs v-model="tab" class="monitor-tabs" @tab-change="tabChanged">
      <ElTabPane label="运行总览" name="overview">
        <div class="monitor-kpis">
          <div
            ><span>在线账号</span
            ><strong
              >{{ sampled ? accounts.filter(online).length : '未采集'
              }}<small v-if="sampled"> / {{ accounts.length }}</small></strong
            ><div class="metric-note">{{
              sampled ? `${boundGroups} 个绑定群` : '等待数据库采样'
            }}</div></div
          >
          <div
            ><span>解码待处理</span><strong>{{ backlogAvailable ? pending : '未采集' }}</strong
            ><div class="metric-note">{{
              backlogStale ? '历史采样，等待更新' : '当前平台全部账号'
            }}</div></div
          >
          <div data-testid="monitor-total-p95"
            ><span>总解码 P95</span
            ><strong>{{ totalLatency ? latency(totalLatency.p95_ms) : '未采集' }}</strong
            ><div class="metric-note">{{
              totalLatency ? `${totalLatency.samples} / 1000 样本 · 最长 24 小时` : '等待有效采样'
            }}</div></div
          >
          <div
            ><span>当前链路异常</span
            ><strong :class="{ 'is-warning': sampled && currentIssues > 0 }">{{
              sampled ? currentIssues : '未采集'
            }}</strong
            ><div class="metric-note">{{
              sampled ? `${historicalIssues} 个账号有历史待排查记录` : '等待数据库采样'
            }}</div></div
          >
        </div>
        <div class="monitor-charts">
          <section class="monitor-plot">
            <div class="plot-heading"
              ><h3>解码延迟分布</h3
              ><span class="metric-note">{{ totalLatency?.samples || 0 }} 条有效观测</span></div
            >
            <div
              v-show="totalLatency?.samples"
              ref="latencyElement"
              class="overview-chart"
              aria-label="当前平台解码延迟分布"
            />
            <ElEmpty
              v-if="!totalLatency?.samples"
              :image-size="52"
              description="暂无有效解码样本"
            />
            <div class="metric-note"
              >P50 {{ latency(totalLatency?.p50_ms) }} · P99
              {{ latency(totalLatency?.p99_ms) }}</div
            >
          </section>
          <section class="monitor-plot">
            <div class="plot-heading"
              ><h3>账号消息速率</h3><span class="metric-note">最近 60 秒 · 条 / 秒</span></div
            >
            <div
              v-show="rateAccounts.length"
              ref="rateElement"
              class="overview-chart"
              aria-label="账号收包及去重入库速率"
            />
            <ElEmpty v-if="!rateAccounts.length" :image-size="52" description="暂无在线接收采样" />
            <div class="metric-note">收包含重复消息；去重入库为新增消息。</div>
          </section>
        </div>
        <div class="plot-heading"
          ><h3>账号状态</h3><span class="metric-note">每账号最多 30 个群</span></div
        >
        <ElTable :data="accounts" empty-text="尚无账号采样">
          <ElTableColumn prop="name" label="机器人" min-width="145" />
          <ElTableColumn label="群绑定" width="100"
            ><template #default="{ row }"
              ><ElTag
                disable-transitions
                :type="row.over_capacity ? 'danger' : 'info'"
                effect="plain"
                >{{ sampled ? `${row.bound_groups} / ${row.group_limit}` : '未采集' }}</ElTag
              ></template
            ></ElTableColumn
          >
          <ElTableColumn label="连接" width="125"
            ><template #default="{ row }"
              ><span :class="{ 'is-success': online(row) }">{{ connection(row) }}</span></template
            ></ElTableColumn
          >
          <ElTableColumn label="当前链路" min-width="210"
            ><template #default="{ row }">
              <span v-if="!sampled" class="metric-note">采样不可用</span
              ><span v-else-if="!currentReasons(row).length" class="metric-note">无已观测异常</span>
              <ElTag
                disable-transitions
                v-for="reason in sampled ? currentReasons(row) : []"
                :key="reason"
                type="warning"
                class="reason-tag"
                effect="plain"
                >{{ reasonName(reason) }}</ElTag
              >
            </template></ElTableColumn
          >
          <ElTableColumn label="历史待排查" min-width="200"
            ><template #default="{ row }">
              <span v-if="!backlogOK(row)" class="metric-note">未采集</span>
              <template v-else
                ><span v-if="row.decode_failed > 0" class="is-warning"
                  >解码失败 {{ row.decode_failed }} 条 · 近 7 天</span
                ><span v-else class="metric-note">无失败记录</span
                ><div v-if="row.receiver?.snapshot?.quarantined > 0" class="metric-note"
                  >接收隔离 {{ row.receiver.snapshot.quarantined }} 条 · 进程累计</div
                ></template
              >
            </template></ElTableColumn
          >
        </ElTable>
        <div class="component-strip"
          ><span v-for="(state, name) in sample.components" :key="name"
            ><i
              :class="[
                'component-dot',
                { observed: componentObserved(String(name), String(state)) }
              ]"
            />{{ componentName(String(name))
            }}<small>{{
              componentObserved(String(name), String(state))
                ? '可观测'
                : state === 'unobserved'
                  ? '未采集'
                  : '不可用'
            }}</small></span
          ></div
        >
      </ElTabPane>
      <ElTabPane label="账号链路" name="accounts">
        <ElSelect v-model="account" clearable placeholder="全部账号" class="monitor-account"
          ><ElOption v-for="a in accounts" :key="a.id" :label="a.name" :value="a.id"
        /></ElSelect>
        <div class="pipeline-caption">收包 → 持久化提交 → ACK 写出 → 解码 → 业务待处理</div>
        <ElTable :data="visibleAccounts" empty-text="暂无链路采样">
          <ElTableColumn prop="name" label="账号" min-width="130" fixed />
          <ElTableColumn label="收包 / 秒" width="110"
            ><template #default="{ row }">{{
              numeric(receiverFresh(row) ? row.receiver.snapshot.per_second : null)
            }}</template></ElTableColumn
          >
          <ElTableColumn label="去重入库 / 秒" width="125"
            ><template #default="{ row }">{{
              numeric(receiverFresh(row) ? row.receiver.snapshot.unique_per_second : null)
            }}</template></ElTableColumn
          >
          <ElTableColumn label="业务 HTTP / 秒" width="140"
            ><template #default="{ row }">{{
              numeric(httpFresh(row) ? row.http.http_per_second : null)
            }}</template></ElTableColumn
          >
          <ElTableColumn label="提交 P95" width="155"
            ><template #default="{ row }"
              >{{ latency(receiverFresh(row) ? row.receiver.snapshot.durable_p95_ms : null)
              }}<div class="metric-note"
                >{{ receiverFresh(row) ? row.receiver.snapshot.latency_samples : '未采集' }} 样本 ·
                60 秒</div
              ></template
            ></ElTableColumn
          >
          <ElTableColumn label="ACK 写出 P95" width="155"
            ><template #default="{ row }"
              >{{ latency(receiverFresh(row) ? row.receiver.snapshot.ack_p95_ms : null)
              }}<div class="metric-note"
                >{{ receiverFresh(row) ? row.receiver.snapshot.ack_samples : '未采集' }} 样本 · 60
                秒</div
              ></template
            ></ElTableColumn
          >
          <ElTableColumn label="接收排队" width="160"
            ><template #default="{ row }"
              >{{ receiverFresh(row) ? row.receiver.snapshot.queue_depth : '未采集' }} /
              {{ bytes(receiverFresh(row) ? row.receiver.snapshot.queue_bytes : null) }}</template
            ></ElTableColumn
          >
          <ElTableColumn label="解码待处理" width="170"
            ><template #default="{ row }"
              >{{ backlogOK(row) ? row.decode_pending : '未采集'
              }}<div class="metric-note"
                >{{ row.backlog_stale ? '历史采样 · ' : '' }}最久
                {{ backlogOK(row) ? age(row.decode_oldest_at) : '未知' }}</div
              ></template
            ></ElTableColumn
          >
          <ElTableColumn label="解码 P95" width="170"
            ><template #default="{ row }"
              >{{
                latency(
                  sampled && row.decode_latency?.available ? row.decode_latency.p95_ms : null
                )
              }}<div class="metric-note"
                >{{ sampled && row.decode_latency?.available ? row.decode_samples : '未采集' }} /
                1000 样本 · 24 小时</div
              ></template
            ></ElTableColumn
          >
          <ElTableColumn label="业务读取" width="185"
            ><template #default="{ row }"
              >{{ httpFresh(row) ? row.http.queue_depth : '未采集' }} / 16 排队 ·
              {{ httpFresh(row) ? (row.http.running ? '执行中' : '空闲') : '未观测'
              }}<div class="metric-note"
                >最久 {{ httpFresh(row) ? age(row.http.oldest_waiting_at) : '未知' }} · 等待
                {{ latency(httpFresh(row) ? row.http.last_wait_ms : null) }}</div
              ></template
            ></ElTableColumn
          >
          <ElTableColumn label="限流冷却" min-width="180"
            ><template #default="{ row }">{{
              !sampled
                ? '未采集'
                : future(row.http?.cooldown_until)
                  ? stamp(row.http.cooldown_until)
                  : '无当前冷却'
            }}</template></ElTableColumn
          >
          <ElTableColumn label="业务操作" width="175"
            ><template #default="{ row }"
              >{{ sampled ? row.operations_queued : '未采集' }} 排队 /
              {{ sampled ? row.operations_unknown : '未采集' }} 未知<div class="metric-note"
                >最久 {{ sampled ? age(row.operation_oldest_at) : '未知' }}</div
              ></template
            ></ElTableColumn
          >
        </ElTable>
        <details class="monitor-cache-diagnostics">
          <summary>读取缓存与基础聊天</summary>
          <ElTable :data="sample.read_caches || []" empty-text="暂无缓存观测">
            <ElTableColumn label="分区" min-width="170">
              <template #default="{ row }">
                {{ row.platform === 'wsl' ? '旺商聊' : '辣椒聊' }} /
                {{ row.purpose === 'chat' ? '聊天' : '管理' }}
              </template>
            </ElTableColumn>
            <ElTableColumn prop="hits" label="命中" min-width="95" />
            <ElTableColumn prop="coalesced" label="请求合并" min-width="100" />
            <ElTableColumn prop="evictions" label="淘汰" min-width="95" />
            <ElTableColumn label="条目 / 容量" min-width="160">
              <template #default="{ row }">{{ row.entries }} / {{ bytes(row.bytes) }}</template>
            </ElTableColumn>
          </ElTable>
          <p v-if="sample.heat_chat" class="metric-note">
            候选 {{ sample.heat_chat.candidates }} · 模型调用 {{ sample.heat_chat.model_calls }} ·
            确认发送 {{ sample.heat_chat.confirmed_sends }} · 阻塞 {{ sample.heat_chat.blocked }} ·
            执行中 {{ sample.heat_chat.active }} / 4
          </p>
        </details>
      </ElTabPane>
      <ElTabPane label="资源趋势" name="resources">
        <div class="trend-toolbar">
          <ElRadioGroup v-model="resourceService" size="small" @change="drawHistory">
            <ElRadioButton value="api">API</ElRadioButton>
            <ElRadioButton value="heatbot">热群引擎</ElRadioButton>
          </ElRadioGroup>
          <span class="metric-note">当前部署 · 仅服务自身</span>
        </div>
        <div class="resource-columns">
          <section>
            <div class="plot-heading"
              ><h3>{{ resourceService === 'api' ? 'API' : '热群引擎' }} 自身</h3
              ><ElTag
                disable-transitions
                :type="resourceFresh ? 'success' : 'info'"
                effect="plain"
                >{{ resourceFresh ? '采样正常' : '未采集 / 已过期' }}</ElTag
              ></div
            >
            <div class="resource-reading"
              ><strong>{{ bytes(resourceFresh ? resource?.memory_bytes : null) }}</strong
              ><span
                >/ {{ bytes(resourceFresh ? resource?.memory_limit_bytes : null) }} 容器内存配额</span
              ></div
            >
            <ElProgress :percentage="memoryPercent" :show-text="false" :stroke-width="6" />
            <dl class="resource-details">
              <div
                ><dt>CPU 配额占用</dt
                ><dd
                  >{{ percent(resourceFresh ? resource?.cpu_quota_percent : null) }}
                  <small
                    >/ {{ numeric(resourceFresh ? resource?.cpu_quota_cores : null) }} 核</small
                  ></dd
                ></div
              >
              <div
                ><dt>进程 RSS / Go 堆</dt
                ><dd
                  >{{ bytes(resourceFresh ? resource?.process_rss_bytes : null) }} /
                  {{ bytes(resourceFresh ? resource?.go_heap_bytes : null) }}</dd
                ></div
              >
              <div
                ><dt>协程 / GC 次数</dt
                ><dd
                  >{{ resourceFresh ? resource?.go_goroutines : '未采集' }} /
                  {{ resourceFresh ? resource?.go_gc_cycles : '未采集' }}</dd
                ></div
              >
              <div
                ><dt>OOM / CPU 节流</dt
                ><dd
                  >{{ resourceFresh ? (resource?.oom_kills ?? '未采集') : '未采集' }} /
                  {{
                    latency(
                      resourceFresh && finite(resource?.cpu_throttled_usec)
                        ? resource.cpu_throttled_usec / 1000
                        : null
                    )
                  }}</dd
                ></div
              >
              <div><dt>进程启动</dt><dd>{{ stamp(resource?.process_started_at) }}</dd></div>
              <div><dt>采样时间</dt><dd>{{ stamp(resource?.sampled_at) }}</dd></div>
              </dl
            ><div class="metric-note">容器内存包含缓存；RSS、Go 堆分别计量，不相加。</div>
          </section>
          <section>
            <div class="plot-heading"
              ><h3>业务数据库</h3
              ><ElTag
                disable-transitions
                :type="databaseStatsFresh ? 'success' : 'info'"
                effect="plain"
                >{{ databaseStatsFresh ? '采样正常' : '未采集 / 已过期' }}</ElTag
              ></div
            >
            <div class="resource-reading"
              ><strong>{{ bytes(databaseStatsFresh ? sample.database_stats?.size_bytes : null) }}</strong
              ><span>本库大小</span></div
            >
            <dl class="resource-details">
              <div><dt>本库连接数</dt><dd>{{ databaseStatsFresh ? sample.database_stats.connections : '未采集' }}</dd></div>
              <div><dt>事务提交 / 回滚</dt><dd>{{ databaseStatsFresh ? sample.database_stats.commits : '未采集' }} / {{ databaseStatsFresh ? sample.database_stats.rollbacks : '未采集' }}</dd></div>
              <div><dt>死锁</dt><dd>{{ databaseStatsFresh ? sample.database_stats.deadlocks : '未采集' }}</dd></div>
              <div><dt>统计查询耗时</dt><dd>{{ latency(databaseStatsFresh ? sample.database_stats.query_ms : null) }}</dd></div>
              <div
                ><dt>采样时间</dt><dd>{{ stamp(sample.database_stats?.sampled_at) }}</dd></div
              >
              <div><dt>统计重置</dt><dd>{{ stamp(sample.database_stats?.stats_reset_at) }}</dd></div> </dl
            ><div class="metric-note"
              >每 60 秒采样本业务库；事务与死锁为重置以来累计值。Web 仅检查可达性。</div
            >
          </section>
        </div>
        <div class="trend-toolbar"
          ><h3>CPU 与内存趋势</h3
          ><ElRadioGroup v-model="hours" size="small" @change="loadHistory"
            ><ElRadioButton :value="1">1 小时</ElRadioButton
            ><ElRadioButton :value="6">6 小时</ElRadioButton
            ><ElRadioButton :value="24">24 小时</ElRadioButton></ElRadioGroup
          ></div
        >
        <ElAlert v-if="historyError" :title="historyError" type="error" :closable="false" />
        <div class="chart-wrap" v-loading="loadingHistory"
          ><div
            ref="chartElement"
            class="resource-chart"
            aria-label="所选服务 CPU 与内存独立刻度趋势"
          /><div v-if="!loadingHistory && !history.length" class="chart-empty">{{
            historyError ? '趋势加载失败，请刷新重试' : '当前时段暂无历史采样'
          }}</div></div
        >
        <div class="metric-note">分钟采样 · 保留 24 小时 · CPU 为容器配额占比 · 缺失或过期采样留空</div>
        <div class="plot-heading"
          ><h3>数据库连接池</h3><span class="metric-note">等待与取消为进程累计值</span></div
        >
        <ElTable :data="pools" empty-text="暂无连接池采样">
          <ElTableColumn label="连接池" min-width="120"
            ><template #default="{ row }">{{ poolName(row.name) }}</template></ElTableColumn
          >
          <ElTableColumn label="使用 / 已建 / 上限" min-width="175"
            ><template #default="{ row }"
              >{{ row.acquired }} / {{ row.total }} / {{ row.maximum }}</template
            ></ElTableColumn
          >
          <ElTableColumn prop="empty_acquires" label="空池等待次数" min-width="140" />
          <ElTableColumn label="获取总耗时" min-width="150"
            ><template #default="{ row }">{{
              latency(row.acquire_wait_total_ms)
            }}</template></ElTableColumn
          >
          <ElTableColumn prop="canceled_acquires" label="取消次数" min-width="110" />
        </ElTable>
      </ElTabPane>
      <ElTabPane label="故障事件" name="incidents">
        <div class="plot-heading"
          ><h3>状态变更</h3
          ><span class="metric-note">最近 7 天 · {{ incidents.length }} 条已加载</span></div
        >
        <ElAlert v-if="incidentError" :title="incidentError" type="error" :closable="false" />
        <ElTable
          :data="incidents"
          v-loading="loadingIncidents"
          :empty-text="incidentError ? '事件加载失败，请刷新重试' : '近七天无已记录事件'"
        >
          <ElTableColumn label="发生时间" min-width="180"
            ><template #default="{ row }">{{ stamp(row.created_at) }}</template></ElTableColumn
          >
          <ElTableColumn label="来源" min-width="120"
            ><template #default="{ row }">{{
              componentName(row.component)
            }}</template></ElTableColumn
          >
          <ElTableColumn label="账号" min-width="130"
            ><template #default="{ row }">{{
              accounts.find((a) => a.id === row.account_id)?.name || row.account_id || '平台'
            }}</template></ElTableColumn
          >
          <ElTableColumn label="状态" width="90"
            ><template #default="{ row }"
              ><ElTag
                disable-transitions
                :type="row.component === 'simon' ? 'info' : row.state === 'resolved' ? 'success' : 'warning'"
                effect="plain"
                >{{ row.component === 'simon' ? '已停用' : row.state === 'resolved' ? '解除' : '记录' }}</ElTag
              ></template
            ></ElTableColumn
          >
          <ElTableColumn label="原因" min-width="220"
            ><template #default="{ row }">{{ reasonName(row.reason) }}</template></ElTableColumn
          >
        </ElTable>
        <ElButton
          v-if="moreIncidents"
          class="incident-more"
          :loading="loadingIncidents"
          @click="loadIncidents(true)"
          >更早事件</ElButton
        >
        <div v-if="incidents.length >= 1000" class="metric-note">已展示最近 1000 条事件。</div>
      </ElTabPane>
    </ElTabs>
  </section>
</template>
<script setup lang="ts">
  import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import { Refresh } from '@element-plus/icons-vue'
  import * as echarts from 'echarts/core'
  import { BarChart, LineChart } from 'echarts/charts'
  import { GridComponent, LegendComponent, TooltipComponent } from 'echarts/components'
  import { CanvasRenderer } from 'echarts/renderers'
  import { api, type Row } from './api'
  import { validMonitorSnapshot, subscribeMonitor } from './monitorLive'
  import { useSession } from './store'
  echarts.use([
    BarChart,
    LineChart,
    GridComponent,
    LegendComponent,
    TooltipComponent,
    CanvasRenderer
  ])
  const workspace = useSession().workspace
  const workspaceName =
    workspace === 'lajiaoliao' ? '辣椒聊' : workspace === 'maibot' ? '热群机器人' : '旺商聊'
  const sample = ref<Row>({ accounts: [] }),
    error = ref(''),
    live = ref('连接中'),
    loading = ref(false)
  const resourceService = ref('api')
  const resource = computed(() => sample.value.services?.[resourceService.value]?.resources)
  const tab = ref('overview'),
    account = ref(''),
    now = ref(Date.now()),
    hours = ref(1)
  const incidents = ref<Row[]>([]),
    moreIncidents = ref(false),
    loadingIncidents = ref(false),
    incidentError = ref('')
  const history = ref<Row[]>([]),
    loadingHistory = ref(false),
    historyError = ref('')
  const chartElement = ref<HTMLElement>(),
    latencyElement = ref<HTMLElement>(),
    rateElement = ref<HTMLElement>()
  const controller = new AbortController()
  let stop: (() => void) | undefined, clock: ReturnType<typeof setInterval>
  let historyController: AbortController | undefined,
    historyVersion = 0,
    historyMinute = -1,
    historyEnd = 0,
    historyAttempt = 0
  let chart: ReturnType<typeof echarts.init> | undefined
  let latencyChart: ReturnType<typeof echarts.init> | undefined,
    rateChart: ReturnType<typeof echarts.init> | undefined
  const observer = new ResizeObserver(() => {
    chart?.resize()
    latencyChart?.resize()
    rateChart?.resize()
  })
  const accounts = computed<Row[]>(() => sample.value.accounts || [])
  const date = (value: unknown) =>
    typeof value === 'string' && !value.startsWith('0001-') ? Date.parse(value) : NaN
  const fresh = (value: unknown, seconds: number) => {
    const age = now.value - date(value)
    return Number.isFinite(age) && age >= -5000 && age < seconds * 1000
  }
  const finite = (value: unknown): value is number =>
    typeof value === 'number' && Number.isFinite(value) && value >= 0
  const oldSample = computed(() => !fresh(sample.value.updated_at, 15))
  const sampled = computed(
    () =>
      !oldSample.value &&
      sample.value.database_available === true &&
      fresh(sample.value.database_sampled_at, 45)
  )
  const serviceFresh = (name: string) => !oldSample.value &&
    sample.value.services?.[name]?.available === true && !sample.value.services?.[name]?.stale &&
    fresh(sample.value.services?.[name]?.sampled_at, 15)
  const resourceFresh = computed(() => serviceFresh(resourceService.value))
  const databaseStatsFresh = computed(() => !oldSample.value &&
    sample.value.database_stats?.available === true && !sample.value.database_stats?.stale &&
    fresh(sample.value.database_stats?.sampled_at, 120))
  const receiverFresh = (a: Row) =>
    !oldSample.value && a.receiver?.observed && fresh(a.receiver.snapshot?.sampled_at, 15)
  const httpFresh = (a: Row) => !oldSample.value && a.http?.observed && fresh(a.http.sampled_at, 15)
  const online = (a: Row) => receiverFresh(a) && a.receiver.snapshot.state === 'connected'
  const backlogOK = (a: Row) =>
    sampled.value && a.backlog_available !== false && finite(a.decode_pending)
  const backlogAvailable = computed(() => sampled.value && accounts.value.every(backlogOK))
  const backlogStale = computed(() => accounts.value.some((a) => a.backlog_stale))
  const pending = computed(() => accounts.value.reduce((n, a) => n + a.decode_pending, 0))
  const boundGroups = computed(() => accounts.value.reduce((n, a) => n + a.bound_groups, 0))
  const currentReasons = (a: Row) =>
    (a.reasons || []).filter((r: string) => !['decode_failed', 'receiver_quarantined'].includes(r))
  const currentIssues = computed(
    () => accounts.value.filter((a) => currentReasons(a).length).length
  )
  const historicalIssues = computed(
    () =>
      accounts.value.filter((a) => a.decode_failed > 0 || a.receiver?.snapshot?.quarantined > 0)
        .length
  )
  const totalLatency = computed(() =>
    sampled.value && sample.value.decode_latency?.available ? sample.value.decode_latency : null
  )
  const visibleAccounts = computed(() =>
    accounts.value.filter((a) => !account.value || a.id === account.value)
  )
  const rateAccounts = computed(() => accounts.value.filter(receiverFresh))
  const pools = computed(() =>
    !oldSample.value
      ? Object.entries(sample.value.database_pools || {}).map(([name, value]) => ({
          name,
          ...(value as Row)
        }))
      : []
  )
  const ratio = (used: unknown, limit: unknown) =>
    finite(used) && finite(limit) && limit > 0 ? Math.min(100, (used / limit) * 100) : 0
  const memoryPercent = computed(() =>
    resourceFresh.value
      ? ratio(resource.value?.memory_bytes, resource.value?.memory_limit_bytes)
      : 0
  )
  const stamp = (value?: string) =>
    Number.isFinite(date(value))
      ? new Date(value!).toLocaleString('zh-CN', { hour12: false })
      : '未采集'
  const numeric = (value: unknown) => (finite(value) ? value.toFixed(1) : '未采集')
  const latency = (value: unknown) => (finite(value) ? `${value.toFixed(1)} ms` : '无样本')
  const percent = (value: unknown) => (finite(value) ? `${value.toFixed(1)}%` : '未采集')
  const bytes = (value: unknown) =>
    finite(value) ? `${(value / 1024 ** 2).toFixed(1)} MiB` : '未采集'
  const age = (value?: string) =>
    !value || value.startsWith('0001-')
      ? '无等待'
      : Number.isFinite(date(value))
        ? `${Math.max(0, (now.value - date(value)) / 1000).toFixed(0)} 秒`
        : '未知'
  const future = (value: unknown) => date(value) > now.value
  const connection = (a: Row) =>
    !sampled.value
      ? '采样不可用'
      : !a.receiver?.enabled
        ? '接收关闭'
        : !receiverFresh(a)
          ? '观测过期'
          : online(a)
            ? '已连接'
            : a.receiver.snapshot.state === 'blocked'
              ? '已阻断'
              : '未连接'
  const components: Record<string, string> = {
    api: 'API',
    web: 'Web',
    heatbot: '热群引擎',
    postgres: 'PostgreSQL',
    database: '数据库',
    simon: 'Simon（已停用来源）',
    receiver: '接收链路',
    http: '业务读取',
    decoder: '解码器',
    backlog: '排队'
  }
  const componentName = (name: string) => components[name] || name
  const componentObserved = (name: string, state: string) =>
    state === 'observed' &&
    !oldSample.value &&
    (name === 'postgres' ? sampled.value : serviceFresh(name))
  const poolName = (name: string) =>
    (({ platform: '平台', tenant: '租户', monitor: '监控' }) as Record<string, string>)[name] ||
    name
  const reasons: Record<string, string> = {
    account_over_capacity: '群绑定超限',
    receiver_unobserved: '接收观测过期',
    receiver_blocked: '接收阻断',
    receiver_disconnected: '连接断开',
    receiver_sync_pending: '首次同步待完成',
    heartbeat_stale: '心跳超过 40 秒',
    receive_queue_pressure: '接收队列压力',
    receiver_quarantined: '接收隔离记录（累计）',
    http_rate_limited: '原生读取限流',
    backlog_oldest: '等待超过 30 秒',
    decode_failed: '近七天有解码失败记录',
    operation_unknown: '操作结果未知',
    simon_unavailable: '已停用来源的历史事件',
    web_monitor_unavailable: 'Web 静态服务不可达',
    heatbot_monitor_unavailable: '热群引擎不可达或采样异常',
    api_monitor_unavailable: 'API 自身采样异常',
    database_unavailable: '数据库采集失败',
    database_stats_unavailable: '本库统计采集失败',
    decoder_database_unavailable: '解码器数据库故障',
    monitor_event_gap: '事件队列已满，部分变更未保留'
  }
  const reasonName = (reason: string) => reasons[reason] || reason
  function accept(value: Row) {
    if (!validMonitorSnapshot(value, workspace)) throw new Error('监控数据范围或采样时间不匹配')
    if (date(value.updated_at) < date(sample.value.updated_at)) return
    sample.value = value
    if (account.value && !value.accounts.some((a: Row) => a.id === account.value))
      account.value = ''
  }
  async function load() {
    if (loading.value || controller.signal.aborted) return
    loading.value = true
    try {
      const value = await api<Row>(
        '/system/monitor',
        'GET',
        undefined,
        controller.signal,
        workspace
      )
      if (!controller.signal.aborted) {
        accept(value)
        error.value = ''
      }
    } catch (e) {
      if (!controller.signal.aborted) error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }
  async function refresh() {
    await Promise.all([
      load(),
      tab.value === 'resources'
        ? loadHistory()
        : tab.value === 'incidents'
          ? loadIncidents()
          : Promise.resolve()
    ])
  }
  async function loadHistory() {
    historyController?.abort()
    const request = new AbortController()
    historyController = request
    const version = ++historyVersion,
      selectedHours = hours.value
    const end = new Date(),
      start = new Date(end.getTime() - selectedHours * 3600000)
    historyAttempt = Date.now()
    loadingHistory.value = true
    history.value = []
    historyError.value = ''
    void drawHistory()
    try {
      const data = await api<Row[]>(
        `/system/monitor/history?start=${encodeURIComponent(start.toISOString())}&end=${encodeURIComponent(end.toISOString())}`,
        'GET',
        undefined,
        request.signal,
        workspace
      )
      if (!controller.signal.aborted && version === historyVersion) {
        if (!Array.isArray(data) || data.length > 1441) throw new Error('历史采样格式不匹配')
        history.value = data
        historyEnd = Math.floor(end.getTime() / 60000)
        historyMinute = historyEnd
        await drawHistory()
      }
    } catch (e) {
      if (!controller.signal.aborted && !request.signal.aborted && version === historyVersion)
        historyError.value = (e as Error).message
    } finally {
      if (version === historyVersion) loadingHistory.value = false
    }
  }
  async function drawHistory() {
    await nextTick()
    if (!chartElement.value || tab.value !== 'resources' || controller.signal.aborted) return
    if (!chart) {
      chart = echarts.init(chartElement.value)
      observer.observe(chartElement.value)
    }
    const indexed = new Map(
      history.value.map((row) => [Math.floor(date(row.minute) / 60000), row.sample])
    )
    const end = historyEnd || Math.floor(now.value / 60000)
    const points = Array.from(
      { length: hours.value * 60 + 1 },
      (_, i) => end - hours.value * 60 + i
    )
    const labels = points.map((t) =>
      new Date(t * 60000).toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' })
    )
    chart.setOption(
      {
        animation: false,
        color: ['#3f6fd1', '#167c80', '#8b65bf', '#cf8b30'],
        legend: { bottom: 0, data: ['CPU 配额占用', '容器内存', '进程 RSS', 'Go 堆'] },
        tooltip: {
          trigger: 'axis',
          renderMode: 'richText',
          valueFormatter: (v: unknown) => (finite(v) ? v.toFixed(1) : '无样本')
        },
        grid: [
          { left: 58, right: 12, top: 30, height: 105 },
          { left: 58, right: 12, top: 210, height: 105 }
        ],
        xAxis: [0, 1].map((index) => ({
          type: 'category',
          gridIndex: index,
          data: labels,
          boundaryGap: false,
          axisLabel: { hideOverlap: true }
        })),
        yAxis: [0, 1].map((index) => ({
          type: 'value',
          gridIndex: index,
          min: 0,
          name: index === 0 ? 'CPU · %' : '内存 · MiB',
          axisLabel: {
            formatter: (v: number) => (v >= 1000 ? `${(v / 1000).toFixed(1)}k` : String(v))
          }
        })),
        series: [
          ['CPU 配额占用', 'cpu_quota_percent', 0],
          ['容器内存', 'memory_bytes', 1],
          ['进程 RSS', 'process_rss_bytes', 1],
          ['Go 堆', 'go_heap_bytes', 1]
        ].map(([name, key, index]) => ({
          name,
          type: 'line',
          xAxisIndex: index,
          yAxisIndex: index,
          showSymbol: points.length <= 100,
          symbolSize: 4,
          connectNulls: false,
          lineStyle: { width: 2 },
          areaStyle: { opacity: 0.06 },
          data: points.map((t) => {
            const s = indexed.get(t)
            const service = s?.services?.[resourceService.value]
            const r = service?.resources || (resourceService.value === 'api' ? s?.api : null)
            const v = r?.[String(key)]
            const sampledAt = date(r?.sampled_at)
            const minute = t * 60000
            return finite(v) && r?.available !== false && !r?.stale &&
              service?.available !== false && !service?.stale &&
              Number.isFinite(sampledAt) && sampledAt >= minute - 15000 && sampledAt < minute + 65000
              ? Number(index) === 0 ? v : v / 1024 ** 2
              : null
          })
        }))
      },
      true
    )
    chart.resize()
  }
  async function drawOverview() {
    await nextTick()
    if (tab.value !== 'overview' || controller.signal.aborted) return
    if (latencyElement.value && totalLatency.value?.samples) {
      if (!latencyChart) {
        latencyChart = echarts.init(latencyElement.value)
        observer.observe(latencyElement.value)
      }
      latencyChart.setOption(
        {
          animation: false,
          color: ['#3f6fd1'],
          grid: { left: 42, right: 10, top: 20, bottom: 30 },
          tooltip: { trigger: 'axis', renderMode: 'richText' },
          xAxis: {
            type: 'category',
            data: ['≤0.5', '0.5–1', '1–2', '2–5', '5–10', '>10'],
            axisLabel: { interval: 0, fontSize: 10 }
          },
          yAxis: { type: 'value', minInterval: 1 },
          series: [
            {
              name: '样本数 · ms 区间',
              type: 'bar',
              barMaxWidth: 32,
              data: totalLatency.value.histogram
            }
          ]
        },
        true
      )
      latencyChart.resize()
    } else latencyChart?.clear()
    if (rateElement.value && rateAccounts.value.length) {
      if (!rateChart) {
        rateChart = echarts.init(rateElement.value)
        observer.observe(rateElement.value)
      }
      rateChart.setOption(
        {
          animation: false,
          color: ['#3f6fd1', '#167c80'],
          grid: { left: 90, right: 15, top: 30, bottom: 25 },
          legend: { top: 0, data: ['收包', '去重入库'] },
          tooltip: { trigger: 'axis', renderMode: 'richText' },
          xAxis: { type: 'value', min: 0 },
          yAxis: {
            type: 'category',
            inverse: true,
            data: rateAccounts.value.map((a) => a.name),
            axisLabel: { width: 76, overflow: 'truncate' }
          },
          series: [
            ['收包', 'per_second'],
            ['去重入库', 'unique_per_second']
          ].map(([name, field]) => ({
            name,
            type: 'bar',
            barMaxWidth: 16,
            data: rateAccounts.value.map((a) =>
              finite(a.receiver.snapshot[field]) ? a.receiver.snapshot[field] : null
            )
          }))
        },
        true
      )
      rateChart.resize()
    } else rateChart?.clear()
  }
  async function loadIncidents(append = false) {
    if (loadingIncidents.value || controller.signal.aborted) return
    loadingIncidents.value = true
    const last = incidents.value.at(-1)
    try {
      const rows = await api<Row[]>(
        `/system/monitor/incidents${append && last ? `?before=${last.id}` : ''}`,
        'GET',
        undefined,
        controller.signal,
        workspace
      )
      if (!controller.signal.aborted) {
        if (!Array.isArray(rows) || rows.length > 100) throw new Error('事件数据格式不匹配')
        const combined = append ? [...incidents.value, ...rows] : rows
        incidents.value = [...new Map(combined.map((row) => [String(row.id), row])).values()].slice(
          0,
          1000
        )
        moreIncidents.value = rows.length === 100 && incidents.value.length < 1000
        incidentError.value = ''
      }
    } catch (e) {
      if (!controller.signal.aborted) incidentError.value = (e as Error).message
    } finally {
      loadingIncidents.value = false
    }
  }
  function tabChanged() {
    if (tab.value === 'resources') void loadHistory()
    else {
      historyController?.abort()
      ++historyVersion
      loadingHistory.value = false
    }
    if (tab.value === 'incidents') void loadIncidents()
    if (tab.value === 'overview') void drawOverview()
  }
  watch([sampled, sample, rateAccounts], () => {
    void drawOverview()
  })
  onMounted(() => {
    void load()
    clock = setInterval(() => {
      now.value = Date.now()
    }, 1000)
    stop = subscribeMonitor(
      workspace,
      (value) => {
        try {
          accept(value)
          error.value = ''
        } catch {
          return
        }
        if (
          tab.value === 'resources' &&
          !loadingHistory.value &&
          Date.now() - historyAttempt >= 30000 &&
          Math.floor(Date.now() / 60000) !== historyMinute
        )
          void loadHistory()
      },
      (value) => {
        live.value = value
      },
      () => {
        controller.abort()
        historyController?.abort()
        sample.value = { accounts: [] }
        history.value = []
        incidents.value = []
        chart?.clear()
        latencyChart?.clear()
        rateChart?.clear()
        error.value = '监控权限或会话已失效'
      }
    )
  })
  onBeforeUnmount(() => {
    controller.abort()
    historyController?.abort()
    stop?.()
    clearInterval(clock)
    observer.disconnect()
    chart?.dispose()
    latencyChart?.dispose()
    rateChart?.dispose()
  })
</script>
<style scoped>
  .runtime-monitor {
    min-width: 0;
  }
  .monitor-header,
  .monitor-toolbar,
  .plot-heading,
  .trend-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    flex-wrap: wrap;
  }
  .monitor-header {
    padding: 18px 20px 10px;
  }
  .monitor-platform {
    display: block;
    font-weight: 600;
    margin-bottom: 5px;
  }
  .monitor-toolbar {
    justify-content: flex-end;
  }
  .metric-note,
  .resource-details small {
    color: var(--el-text-color-secondary);
    font-size: 12px;
    line-height: 1.7;
  }
  .monitor-tabs {
    padding: 0 20px 20px;
  }
  .monitor-kpis {
    display: grid;
    grid-template-columns: repeat(4, minmax(0, 1fr));
    border-bottom: 1px solid var(--el-border-color-light);
  }
  .monitor-kpis > div {
    min-width: 0;
    padding: 18px 12px 20px 0;
  }
  .monitor-kpis span {
    color: var(--el-text-color-secondary);
    font-size: 13px;
  }
  .monitor-kpis strong {
    display: block;
    font-size: 24px;
    font-weight: 600;
    margin: 8px 0 4px;
    overflow-wrap: anywhere;
  }
  .monitor-kpis small {
    font-size: 13px;
    font-weight: 400;
  }
  .monitor-charts,
  .resource-columns {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 28px;
  }
  .monitor-charts {
    padding: 8px 0 22px;
    border-bottom: 1px solid var(--el-border-color-light);
  }
  .monitor-plot,
  .resource-columns > section {
    min-width: 0;
  }
  .plot-heading {
    min-height: 46px;
  }
  h3 {
    font-size: 14px;
    font-weight: 600;
    margin: 14px 0;
  }
  .overview-chart {
    width: 100%;
    height: 200px;
  }
  .monitor-plot :deep(.el-empty) {
    height: 200px;
    padding: 12px;
  }
  .component-strip {
    display: flex;
    flex-wrap: wrap;
    gap: 12px 20px;
    padding: 18px 0 0;
  }
  .component-strip > span {
    display: inline-flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
  }
  .component-strip small {
    color: var(--el-text-color-secondary);
  }
  .component-dot {
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--el-text-color-placeholder);
  }
  .component-dot.observed {
    background: var(--el-color-success);
  }
  .reason-tag {
    margin: 3px 5px 3px 0;
    white-space: normal;
    height: auto;
  }
  .is-warning {
    color: var(--el-color-warning);
  }
  .is-success {
    color: var(--el-color-success);
  }
  .monitor-account {
    width: min(100%, 260px);
    margin: 4px 0 12px;
  }
  .pipeline-caption {
    color: var(--el-text-color-secondary);
    font-size: 13px;
    padding-bottom: 14px;
    line-height: 1.8;
  }
  .resource-reading {
    display: flex;
    align-items: baseline;
    flex-wrap: wrap;
    gap: 8px;
    margin: 8px 0 14px;
  }
  .resource-reading strong {
    font-size: 22px;
    font-weight: 600;
  }
  .resource-reading span {
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
  .resource-details {
    margin: 12px 0;
    font-size: 13px;
  }
  .resource-details > div {
    display: flex;
    justify-content: space-between;
    gap: 12px;
    padding: 8px 0;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .resource-details dt {
    color: var(--el-text-color-secondary);
    flex-shrink: 0;
  }
  .resource-details dd {
    margin: 0;
    text-align: right;
    overflow-wrap: anywhere;
  }
  .trend-toolbar {
    margin-top: 24px;
    border-top: 1px solid var(--el-border-color-light);
    padding-top: 10px;
  }
  .chart-wrap {
    position: relative;
  }
  .resource-chart {
    width: 100%;
    height: 400px;
    margin-top: 12px;
  }
  .chart-empty {
    position: absolute;
    inset: 0;
    display: grid;
    place-content: center;
    color: var(--el-text-color-secondary);
    font-size: 13px;
    background: var(--el-bg-color);
  }
  .incident-more {
    margin: 16px 0;
  }
  @media (max-width: 900px) {
    .monitor-charts {
      grid-template-columns: minmax(0, 1fr);
      gap: 8px;
    }
  }
  @media (max-width: 700px) {
    .monitor-kpis {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .resource-columns {
      grid-template-columns: minmax(0, 1fr);
      gap: 16px;
    }
    .monitor-tabs {
      padding: 0 10px 16px;
    }
    .monitor-header {
      padding: 14px 10px 8px;
      align-items: flex-start;
    }
    .monitor-header .metric-note {
      max-width: 230px;
      overflow-wrap: anywhere;
    }
    .monitor-kpis strong {
      font-size: 22px;
    }
    .trend-toolbar {
      gap: 4px;
    }
  }
</style>
