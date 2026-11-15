<template>
  <div class="account-dashboard" :aria-busy="loading" data-testid="account-dashboard">
    <div class="account-dashboard-heading">
      <div>
        <h2>{{ accountOptionLabel(account) }}</h2>
        <span
          >{{
            workspace === 'maibot'
              ? '热群机器人'
              : workspace === 'lajiaoliao'
                ? '辣椒聊机器人'
                : '旺商聊机器人'
          }}
          · {{ account.platform === 'lajiaoliao' ? '辣椒聊' : '旺商聊' }} · 独立账号工作台</span
        >
      </div>
      <div class="account-dashboard-actions">
        <MobileSearchAccess
          v-if="admin && account.platform === 'wsl'"
          :account="account"
          :workspace="workspace"
          :owner="owner"
          @updated="load"
        />
        <ElButton v-if="friendGateAllowed" :icon="UserFilled" @click="friendGateOpen = true"
          >好友接入准入</ElButton
        >
        <ElButton v-if="admin" :icon="Setting" @click="settingsOpen = true">账号资料</ElButton>
        <ElButton v-if="admin" :icon="ChatDotRound" @click="emit('messages')">消息与解码</ElButton>
        <ElSwitch
          v-if="admin && sample?.receiver"
          :model-value="sample.receiver.enabled"
          :loading="receiverSaving"
          :disabled="receiverSaving || sample.account.auth_state !== 'authenticated'"
          aria-label="消息接收"
          active-text="消息接收"
          @change="toggleReceiver"
        />
        <ElButton
          v-if="admin && sample?.receiver"
          :icon="Key"
          :loading="authPreparing"
          :disabled="receiverSaving || stale"
          @click="emit('reauth', { ...account })"
          >重新登录</ElButton
        >
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
    <NativeSettings
      v-if="admin"
      :open="settingsOpen"
      :account="account"
      :workspace="workspace"
      :scope="scope()"
      @close="settingsOpen = false"
      @reauth="emit('reauth', { ...account })"
    />
    <OnboardingFriendGate
      v-if="friendGateAllowed"
      :open="friendGateOpen"
      :account="account"
      :workspace="workspace"
      :owner="owner"
      @close="friendGateOpen = false"
    />
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
        <ElTag :type="healthType(sample.health.status)">{{
          healthLabel(sample.health.status)
        }}</ElTag>
        <span>指标更新 {{ date(sample.sampled_at) }}</span>
        <ElTag :type="liveState === 'live' ? 'success' : 'info'">{{ liveLabel }}</ElTag>
        <ElTag v-if="sample.visibility === 'tenant_groups'" type="info" effect="plain">
          仅本租户授权群
        </ElTag>
      </div>
      <div class="account-kpis">
        <div
          ><span>已授权群组</span><strong>{{ sample.groups.authorized }}</strong></div
        >
        <div>
          <ElTooltip
            :content="
              admin
                ? '最近 60 秒可靠入库的原始群聊与私聊消息数 / 60，已去重'
                : '最近 60 秒本租户授权群的业务入库数 / 60'
            "
          >
            <span>{{ admin ? '消息吞吐' : '业务入库速率' }} · 60 秒</span>
          </ElTooltip>
          <strong>{{ throughput.per_second.toFixed(2) }}<small>条/秒</small></strong>
        </div>
        <div>
          <ElTooltip
            :content="
              admin
                ? '源数据加载、解密、解析及投影加密耗时；不含排队和结果 UPDATE'
                : '收到业务消息至 INSERT 的时间，不代表事务提交或 ACK'
            "
          >
            <span>{{ admin ? '解码 P95' : '业务 INSERT P95' }}</span>
          </ElTooltip>
          <strong class="latency-value">{{
            ms(admin ? sample.decode?.latency?.p95_ms : sample.performance.p95_ms)
          }}</strong>
        </div>
        <div>
          <span>{{ admin ? '待解码 / 待操作' : '待处理消息 / 操作' }}</span>
          <strong
            >{{
              admin
                ? (sample.decode?.pending ?? '未采集')
                : sample.inbox.pending + sample.inbox.unresolved
            }}<small>/ {{ sample.operations.queued }}</small></strong
          >
        </div>
      </div>
      <div v-for="reason in visibleReasons" :key="reason" class="account-reason">{{
        reasonLabel(reason)
      }}</div>
      <div class="account-chart-grid">
        <section class="account-performance" aria-label="消息性能">
          <div class="account-section-heading">
            <h3>{{ admin ? '消息吞吐趋势' : '业务入库趋势' }}</h3>
            <span>15 分钟 · 条/分钟</span>
          </div>
          <div class="account-message-chart" role="img" :aria-label="chartLabel">
            <ArtBarChart
              :data="messageCounts"
              :x-axis-data="messageTimes"
              :colors="['#7a5bff']"
              height="210px"
            />
          </div>
          <div class="account-performance-meta">
            <span>近 60 秒 {{ throughput.inserted_60s }} 条</span>
            <span
              >最近入库
              {{
                date(
                  admin
                    ? sample.receiver?.snapshot?.last_committed_at
                    : sample.performance.last_inserted_at
                )
              }}</span
            >
          </div>
        </section>
        <section v-if="admin" class="account-performance" aria-label="解码延迟分布">
          <div class="account-section-heading">
            <h3>解码延迟分布</h3>
            <ElTag :type="sample.decode?.latency?.available ? 'info' : 'warning'">{{
              sample.decode?.latency?.available ? '最近 1000 条 · 最长 24 小时' : '采样暂不可用'
            }}</ElTag>
          </div>
          <ArtBarChart
            v-if="sample.decode?.latency?.samples"
            :data="sample.decode.latency.histogram"
            :x-axis-data="['≤0.5', '0.5–1', '1–2', '2–5', '5–10', '>10 ms']"
            :colors="['#148767']"
            height="210px"
          />
          <ElEmpty v-else description="暂无有效解码样本" :image-size="48" class="latency-empty" />
          <div class="account-latencies">
            <div v-for="metric in ['p50', 'p95', 'p99']" :key="metric">
              <span>{{ metric.toUpperCase() }}</span
              ><strong>{{ ms(sample.decode?.latency?.[`${metric}_ms`]) }}</strong>
            </div>
          </div>
        </section>
      </div>
      <div v-if="admin" class="sample-coverage" aria-label="解码采样覆盖">
        <div class="coverage-heading">
          <strong>有效样本 {{ sample.decode?.latency?.samples ?? 0 }} / 1000</strong>
          <span>{{
            sample.decode?.latency?.samples >= 1000 ? '已达到样本目标' : '样本收集中'
          }}</span>
        </div>
        <ElProgress
          :percentage="Math.min(100, (sample.decode?.latency?.samples ?? 0) / 10)"
          :show-text="false"
          :stroke-width="5"
          color="#148767"
        />
        <div class="account-performance-meta">
          <span
            >覆盖 {{ date(sample.decode?.latency?.from) }} 至
            {{ date(sample.decode?.latency?.to) }}</span
          >
          <span>采样 {{ date(sample.decode?.sampled_at) }}</span>
          <span>达到 1000 条不等于全天稳定性验收</span>
        </div>
      </div>
      <ElCollapse v-model="diagnostics" class="account-diagnostics">
        <ElCollapseItem title="运行诊断" name="diagnostics">
          <ElDescriptions
            v-if="admin && sample.capacity"
            :column="1"
            border
            class="console-summary"
          >
            <ElDescriptionsItem label="账号绑定容量"
              ><ElTag :type="sample.capacity.over_capacity ? 'danger' : 'info'"
                >{{ sample.capacity.bound }} / {{ sample.capacity.limit }}</ElTag
              >
              · 功能停用不释放名额</ElDescriptionsItem
            >
            <ElDescriptionsItem label="原生业务读取"
              >{{ sample.http?.observed ? `${sample.http.queue_depth} / 16 排队` : '未采集' }} ·
              {{
                !sample.http?.observed ? '状态未观测' : sample.http.running ? '执行中' : '空闲'
              }}</ElDescriptionsItem
            >
            <ElDescriptionsItem label="读取等待 / 执行耗时"
              >{{
                !sample.http?.observed || sample.http.last_wait_ms == null
                  ? '无样本'
                  : `${sample.http.last_wait_ms.toFixed(1)} ms`
              }}
              /
              {{
                !sample.http?.observed || sample.http.last_request_ms == null
                  ? '无样本'
                  : `${sample.http.last_request_ms.toFixed(1)} ms`
              }}</ElDescriptionsItem
            >
            <ElDescriptionsItem label="读取冷却截止">{{
              !sample.http?.observed
                ? '未采集'
                : sample.http.cooldown_until
                  ? date(sample.http.cooldown_until)
                  : '无当前冷却'
            }}</ElDescriptionsItem>
            <ElDescriptionsItem label="业务 HTTP · 最近 60 秒">{{
              sample.http?.observed ? `${sample.http.http_per_second.toFixed(2)} 次/秒` : '未采集'
            }}</ElDescriptionsItem>
            <ElDescriptionsItem label="解码待处理 / 失败">{{
              sample.decode ? `${sample.decode.pending} / ${sample.decode.failed}` : '未采集'
            }}</ElDescriptionsItem>
            <ElDescriptionsItem label="解码最久等待">{{
              sample.decode ? age(sample.decode.oldest_at) : '未采集'
            }}</ElDescriptionsItem>
            <ElDescriptionsItem label="解码 P95 · 最近 1000 条 / 24 小时">
              {{ ms(sample.decode?.p95_ms) }} · {{ sample.decode?.samples ?? 0 }} 样本 · 采样
              {{ date(sample.decode?.sampled_at) }}
            </ElDescriptionsItem>
            <ElDescriptionsItem v-if="sample.decode?.diagnostics" label="本次进程分阶段 P95">
              <div v-for="stage in sample.decode.diagnostics.stages" :key="stage.name">
                {{ decodeStageLabel(stage.name) }}：{{ ms(stage.p95_ms) }}
              </div>
              <span>
                {{ sample.decode.diagnostics.samples }} / 1000 样本 · 最长 24 小时 · 采样
                {{ date(sample.decode.diagnostics.sampled_at) }}
              </span>
              <div v-if="!sample.decode.diagnostics.samples">暂无本次进程样本</div>
            </ElDescriptionsItem>
            <ElDescriptionsItem label="原生昵称">{{
              account.native_nickname || '未采集'
            }}</ElDescriptionsItem>
            <ElDescriptionsItem label="网站机器人名称">{{ account.name }}</ElDescriptionsItem>
          </ElDescriptions>
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
                  ><dt>连接记录</dt
                  ><dd>{{ connectionLabel(sample.account.runtime_state) }}</dd></div
                >
                <div
                  ><dt>最后入库</dt><dd>{{ date(sample.performance.last_inserted_at) }}</dd></div
                >
              </dl>
              <div v-if="!sample.runtime_observed" class="account-observation-note"
                ><ElIcon><InfoFilled /></ElIcon>实时连接、心跳与 ACK 尚无监测数据</div
              >
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
              <ElTag
                :type="
                  sample.runtime_observed &&
                  sample.receiver.snapshot?.state === 'connected' &&
                  sample.receiver.snapshot?.synced_at
                    ? 'success'
                    : 'info'
                "
              >
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
            <ElButton
              v-if="sample.receiver.snapshot?.state === 'blocked'"
              :icon="Connection"
              :loading="receiverSaving"
              :disabled="receiverSaving || stale"
              @click="toggleReceiver(true)"
              >重新连接</ElButton
            >
            <dl v-if="sample.receiver.snapshot?.upstream_code" class="account-status-list">
              <div
                ><dt>上游结果码</dt><dd>{{ sample.receiver.snapshot.upstream_code }}</dd></div
              >
              <div
                ><dt>失败阶段</dt
                ><dd>{{
                  sample.receiver.snapshot.error_stage === 'im_login'
                    ? '消息协议登录'
                    : '消息令牌换票'
                }}</dd></div
              >
            </dl>
            <dl v-if="sample.receiver.snapshot" class="account-status-list">
              <div
                ><dt>首次同步完成</dt><dd>{{ date(sample.receiver.snapshot.synced_at) }}</dd></div
              >
              <div v-if="sample.receiver.snapshot.retry_at && sample.receiver.enabled"
                ><dt>下次重试</dt><dd>{{ date(sample.receiver.snapshot.retry_at) }}</dd></div
              >
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
                  >{{ sample.receiver.snapshot.frames }} /
                  {{ sample.receiver.snapshot.messages }}</dd
                ></div
              >
              <div
                ><dt>重复消息 / ACK</dt
                ><dd
                  >{{ sample.receiver.snapshot.duplicates }} /
                  {{ sample.receiver.snapshot.acks }}</dd
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
                ><dt>可靠落盘 P95</dt
                ><dd
                  >{{ ms(sample.receiver.snapshot.durable_p95_ms) }} ·
                  {{ sample.receiver.snapshot.latency_samples ?? 0 }} 样本</dd
                ></div
              >
              <div v-if="sample.receiver.snapshot.ack_mode !== 'sequence_commit_only'"
                ><dt>ACK 写出 P95</dt
                ><dd
                  >{{ ms(sample.receiver.snapshot.ack_p95_ms) }} ·
                  {{ sample.receiver.snapshot.ack_samples ?? 0 }} 样本</dd
                ></div
              >
              <div
                ><dt>最近接包</dt
                ><dd>{{ date(sample.receiver.snapshot.last_received_at) }}</dd></div
              >
              <div
                ><dt>最近 ACK 写出</dt
                ><dd>{{ date(sample.receiver.snapshot.last_ack_at) }}</dd></div
              >
            </dl>
            <p class="account-observation-note"
              >原始事件保留，未启用业务消费。计数为本次接收周期；延迟取最近 60 秒、 每阶段至多 8192
              条样本。ACK 仅表示写出成功。</p
            >
          </section>
        </ElCollapseItem>
      </ElCollapse>
    </template>
  </div>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import {
    ChatDotRound,
    Connection,
    Delete,
    InfoFilled,
    Key,
    Refresh,
    Setting,
    UserFilled
  } from '@element-plus/icons-vue'
  import ArtBarChart from '../components/core/charts/art-bar-chart/index.vue'
  import { api, type Row } from './api'
  import { accountLive, type LiveStatus } from './accountLive'
  import { accountOptionLabel } from './accountIdentity'
  import NativeSettings from './NativeSettings.vue'
  import OnboardingFriendGate from './OnboardingFriendGate.vue'
  import MobileSearchAccess from './MobileSearchAccess.vue'

  const props = defineProps<{
    account: Row
    workspace: import('./workspaces').Workspace
    owner: string
    admin: boolean
    refreshToken: string
    authPreparing?: boolean
  }>()
  const decodeStageLabel = (name: string) =>
    ({
      queue: '任务排队',
      source_load: '密封源读取',
      vault_json: '解封与 JSON',
      protocol: '协议解析',
      projection_seal: '投影密封',
      persist: '结果写回',
      service_total: '完整服务'
    })[name] || name
  const emit = defineEmits<{ remove: [account: Row]; reauth: [account: Row]; messages: [] }>()
  const sample = ref<Row | null>(null),
    loading = ref(false),
    error = ref(''),
    stale = ref(false)
  const receiverSaving = ref(false)
  const settingsOpen = ref(false)
  const friendGateOpen = ref(false)
  const friendGateAllowed = computed(
    () =>
      props.admin &&
      props.account.purpose === 'management' &&
      (props.workspace === 'wsl' || props.workspace === 'lajiaoliao') &&
      props.account.platform === props.workspace
  )
  const diagnostics = ref<string[]>([])
  let controller: AbortController | undefined,
    generation = 0
  let stopLive: (() => void) | undefined
  const liveState = ref<LiveStatus>('connecting')
  const liveLabel = computed(
    () =>
      ({
        connecting: '正在订阅',
        live: '实时订阅',
        reconnecting: '正在重连',
        paused: '订阅已暂停',
        blocked: '订阅不可用'
      })[liveState.value]
  )
  const scope = () => `${props.owner}:${props.workspace}:${props.account.id}`
  watch(scope, () => {
    settingsOpen.value = false
    friendGateOpen.value = false
    diagnostics.value = []
  })
  watch(
    () => sample.value?.receiver?.snapshot?.state,
    (state) => {
      if (state === 'blocked') diagnostics.value = ['diagnostics']
    }
  )
  const date = (value?: string) => (value ? new Date(value).toLocaleString('zh-CN') : '暂无记录')
  const ms = (value?: number | null) => (value == null ? '暂无样本' : `${value.toFixed(2)} ms`)
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
      synchronizing: '首次同步中',
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
      receiver_sync_pending: '首次同步尚未可靠提交',
      heartbeat_stale: '心跳观测已过期',
      receiver_queue: '接收队列接近上限'
    })[reason] || reason
  const visibleReasons = computed(() =>
    (sample.value?.health.reasons || []).filter((r: string) => r !== 'runtime_unobserved')
  )
  const chartLabel = computed(() =>
    sample.value
      ? `${props.account.name} 最近 15 分钟入库消息共 ${throughput.value.series.reduce((sum: number, b: Row) => sum + b.count, 0)} 条`
      : '暂无消息性能数据'
  )
  const throughput = computed(
    () =>
      sample.value?.native_performance ||
      sample.value?.performance || { per_second: 0, inserted_60s: 0, series: [] }
  )
  const messageCounts = computed(() => throughput.value.series.map((row: Row) => row.count))
  const messageTimes = computed(() =>
    throughput.value.series.map((row: Row) =>
      new Date(row.at).toLocaleTimeString('zh-CN', {
        timeZone: 'Asia/Shanghai',
        hour: '2-digit',
        minute: '2-digit'
      })
    )
  )

  const receiverError = (code: string) =>
    ({
      im_kicked: '原生连接被踢下线，请先核对其他客户端',
      im_auth_rejected: '消息协议认证被拒绝，已停止自动重试，请核对上游结果码与协议配置',
      native_push_boundary_unknown: '收到无法核验边界的消息包，已隔离并暂停恢复水位',
      receiver_configuration_unavailable: '接收配置或会话不可用',
      receiver_configuration_invalid: '接收配置与已登录会话不匹配',
      native_reauth_required: '原生会话需要重新验证，请先停用接收',
      native_session_renewal_failed: '消息令牌换票失败，已暂停，请核对会话和接入配置',
      native_unknown_result: '换票结果未知，已暂停自动重试，请核对官方客户端状态',
      native_rate_limited: '原生读取限流，等待恢复',
      receiver_database_unavailable: '数据库暂时不可用，未确认未落盘消息',
      im_connection_failed: '消息服务器暂时连接失败，等待重连',
      im_discovery_failed: '消息服务器发现失败，等待重试',
      im_reply_timeout: '消息服务器回复超时，等待重连',
      im_sync_timeout: '首次同步尚未可靠提交，等待重连'
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
    if (liveState.value === 'blocked') startLive()
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
  function startLive() {
    stopLive?.()
    const originalScope = scope()
    stopLive = accountLive({
      account: props.account.id,
      workspace: props.workspace,
      view: 'dashboard',
      status: (value) => {
        if (originalScope !== scope()) return
        liveState.value = value
        if (value === 'blocked') {
          ++generation
          controller?.abort()
          sample.value = null
          error.value = '实时订阅不可用，请确认当前权限后刷新'
          loading.value = false
        }
      },
      update: (data) => {
        if (originalScope !== scope()) return
        if (data.dashboard) {
          // A late initial GET must not overwrite a newer stream snapshot.
          ++generation
          controller?.abort()
          loading.value = false
          sample.value = data.dashboard
        }
        if (sample.value && data.receiver) {
          sample.value.receiver = data.receiver
          sample.value.runtime_observed = data.receiver.observed
          if (data.receiver.snapshot)
            sample.value.account.runtime_state = data.receiver.snapshot.state
        }
        if (sample.value && data.health) sample.value.health = data.health
        if (sample.value && data.backlog) sample.value.backlog = data.backlog
        error.value = ''
        stale.value = false
      }
    })
  }
  watch(
    [() => props.owner, () => props.workspace, () => props.account.id],
    () => {
      sample.value = null
      error.value = ''
      stale.value = false
      startLive()
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
  const onVisible = () => {
    if (!document.hidden) void load()
  }
  onMounted(() => document.addEventListener('visibilitychange', onVisible))
  onBeforeUnmount(() => {
    stopLive?.()
    ++generation
    controller?.abort()
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
  .account-dashboard-actions {
    flex-wrap: wrap;
    max-width: 100%;
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
    color: var(--ui-muted);
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
    border: 1px solid var(--ui-line);
    background: #fff;
  }
  .account-kpis > div {
    padding: 20px;
    border-right: 1px solid var(--ui-line-soft);
    min-width: 0;
  }
  .account-kpis > div:last-child {
    border-right: 0;
  }
  .account-kpis span,
  .account-latencies span {
    display: block;
    color: var(--ui-muted);
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
  .account-kpis .latency-value {
    font-size: 24px;
  }
  .account-chart-grid {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 28px;
  }
  .account-chart-grid:has(> :only-child) {
    grid-template-columns: minmax(0, 1fr);
  }
  .latency-empty {
    height: 210px;
  }
  .sample-coverage {
    padding: 20px 0;
    border-bottom: 1px solid var(--ui-line);
  }
  .coverage-heading {
    display: flex;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 8px;
    margin-bottom: 12px;
    font-size: 12px;
  }
  .coverage-heading > span {
    color: var(--ui-muted);
  }
  .account-diagnostics {
    margin-top: 20px;
  }
  .account-kpis small,
  .account-latencies small {
    font-size: 12px;
    margin-left: 6px;
    color: var(--ui-muted);
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
    border-bottom: 1px solid var(--ui-line-soft);
    font-size: 13px;
  }
  .account-status-list dt {
    color: var(--ui-muted);
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
    grid-template-columns: repeat(3, minmax(0, 1fr));
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
    .account-chart-grid {
      grid-template-columns: minmax(0, 1fr);
    }
    .account-kpis {
      grid-template-columns: repeat(2, minmax(0, 1fr));
    }
    .account-kpis > div {
      border-bottom: 1px solid var(--ui-line-soft);
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
