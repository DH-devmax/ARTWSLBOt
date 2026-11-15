<template>
  <div
    class="message-center"
    :class="{ 'mobile-thread': tab === 'conversations' && !!current }"
    data-testid="message-center"
  >
    <ElEmpty v-if="!account" description="请在顶部选择一个机器人账号" />
    <template v-else>
      <div class="mc-toolbar">
        <h1 class="mc-sr-only">消息中心</h1>
        <ElTabs v-model="tab" class="mc-tabs">
          <ElTabPane label="对话消息" name="conversations" />
          <ElTabPane :label="`原生群组 · ${directoryTotal}`" name="groups" />
        </ElTabs>
        <div class="mc-toolbar-actions">
          <ElPopover
            v-model:visible="monitorOpen"
            placement="bottom-end"
            trigger="click"
            width="min(420px, calc(100vw - 24px))"
          >
            <template #reference>
              <button
                class="mc-monitor-button"
                :class="monitorTone"
                aria-label="解码监控"
                :aria-description="monitorLabel"
                :aria-expanded="monitorOpen"
                :title="monitorLabel"
                data-testid="decoder-monitor"
              >
                <span class="mc-health-dot"></span>
                <ElIcon class="mc-monitor-icon"><DataAnalysis /></ElIcon>
                <span class="mc-monitor-label">{{ monitorLabel }}</span>
                <ElIcon class="mc-monitor-chevron"><ArrowDown /></ElIcon>
              </button>
            </template>
            <section class="mc-health" aria-label="解码器健康详情">
              <div class="mc-health-heading">
                <strong>消息链路与解码</strong>
                <ElTag size="small" :type="liveState === 'live' ? 'success' : 'info'">{{
                  liveLabel
                }}</ElTag>
              </div>
              <ElAlert
                v-if="receiveIssue"
                :title="receiveIssue.title"
                :description="receiveIssue.detail"
                :type="receiveIssue.type"
                :closable="false"
                show-icon
                data-testid="receive-issue"
              />
              <template v-if="health">
                <dl>
                  <div
                    ><dt>连接</dt><dd>{{ connected ? '在线' : receiveLabel }}</dd></div
                  >
                  <div
                    ><dt>解码状态</dt><dd>{{ healthLabel(health.status) }}</dd></div
                  >
                  <div
                    ><dt>待领取 / 处理中</dt
                    ><dd>{{ health.pending }} / {{ health.processing }}</dd></div
                  >
                  <div
                    ><dt>解码 P95</dt><dd>{{ ms(health.decode_p95_ms) }}</dd></div
                  >
                  <div v-if="health.decode_latency">
                    <dt>解码采样窗口</dt>
                    <dd
                      >{{ health.decode_latency.samples }} /
                      {{ health.decode_latency.sample_limit }} 条 · 最长
                      {{ health.decode_latency.window_seconds / 3600 }} 小时</dd
                    >
                  </div>
                  <div
                    ><dt>原始消息 · 7 天</dt><dd>{{ health.total }}</dd></div
                  >
                  <div
                    ><dt>已解析消息</dt><dd>{{ health.decoded }}</dd></div
                  >
                  <div
                    ><dt>未支持 / 失败 · 7 天</dt
                    ><dd>{{ health.unsupported }} / {{ health.failed }}</dd></div
                  >
                  <div v-if="health.recent_hour"
                    ><dt>近 1 小时未支持 / 失败</dt
                    ><dd>{{ health.recent_hour.unsupported }} / {{ health.recent_hour.failed }}</dd></div
                  >
                  <div v-if="health.transport_notices"
                    ><dt>原生只读通知 · 7 天</dt><dd>{{ health.transport_notices }}</dd></div
                  >
                  <div
                    ><dt>本账号近 60 秒完成</dt><dd>{{ health.processed_60s }}</dd></div
                  >
                  <div
                    ><dt>最早等待消息</dt><dd>{{ date(health.oldest_pending_at) }}</dd></div
                  >
                  <div
                    ><dt>共享工作池 · 活跃 / 上限</dt
                    ><dd>{{ health.pool.active }} / {{ health.pool.workers }}</dd></div
                  >
                  <div
                    ><dt>共享内存队列</dt
                    ><dd>{{ health.pool.queue_depth }} / {{ health.pool.queue_capacity }}</dd></div
                  >
                  <div
                    ><dt>工作池运行</dt
                    ><dd>{{ health.pool.observed ? '运行中' : '不可用' }}</dd></div
                  >
                  <div
                    ><dt>接收队列</dt
                    ><dd>{{ health.receiver.snapshot?.queue_depth ?? '无采样' }}</dd></div
                  >
                  <div>
                    <dt>{{
                      health.receiver.snapshot?.ack_mode === 'sequence_commit_only'
                        ? '可靠提交 · 当前连接'
                        : '已入库 / ACK · 当前连接'
                    }}</dt>
                    <dd
                      >{{ health.receiver.snapshot?.messages ?? '—'
                      }}<template
                        v-if="health.receiver.snapshot?.ack_mode !== 'sequence_commit_only'"
                      >
                        / {{ health.receiver.snapshot?.acks ?? '—' }}</template
                      ></dd
                    >
                  </div>
                  <div
                    ><dt>接收阻断原因</dt
                    ><dd>{{ health.receiver.snapshot?.error || '无' }}</dd></div
                  >
                </dl>
                <h3>解码诊断 · 近 7 天</h3>
                <ElTable :data="reasonRows" size="small" empty-text="无解码异常">
                  <ElTableColumn prop="label" label="阶段 / 类型" />
                  <ElTableColumn prop="count" label="条数" width="64" />
                </ElTable>
                <p class="mc-sampled">指标更新 {{ date(health.sampled_at) }}</p>
              </template>
              <ElEmpty v-else description="暂无解码采样" :image-size="40" />
            </section>
          </ElPopover>
          <ElButton
            text
            :icon="Refresh"
            :loading="loading"
            aria-label="刷新消息中心"
            title="刷新消息与健康指标"
            @click="load"
          />
        </div>
      </div>
      <ElAlert v-if="error" :title="error" type="error" :closable="false" show-icon />
      <div
        v-if="tab === 'conversations'"
        class="mc-conversations"
        :class="{ 'has-thread': !!current }"
      >
        <section class="mc-list" aria-label="原生对话">
          <div class="mc-filter">
            <ElInput
              v-model="search"
              :prefix-icon="Search"
              placeholder="搜索本页对话"
              aria-label="搜索对话"
              clearable
            />
            <ElRadioGroup v-model="scene" size="small" aria-label="对话类型" @change="changeScene">
              <ElRadioButton value="">全部</ElRadioButton>
              <ElRadioButton value="group">群聊</ElRadioButton>
              <ElRadioButton value="private">私聊</ElRadioButton>
            </ElRadioGroup>
          </div>
          <div class="mc-list-scroll">
            <ElEmpty
              v-if="!filtered.length"
              :description="search ? '没有匹配的对话' : '暂无已接收的对话'"
              :image-size="48"
            />
            <button
              v-for="item in filtered"
              :key="item.key"
              class="mc-conversation"
              :class="{ selected: selected === item.key }"
              :aria-pressed="selected === item.key"
              @click="selectConversation(item.key)"
            >
              <MessageImage
                v-if="item.avatar_url"
                :url="item.avatar_url"
                :workspace="workspace"
                :scope="scope"
                :label="item.title"
                avatar
              />
              <span v-else class="mc-avatar" :class="item.scene">
                <ElIcon><ChatDotRound v-if="item.scene === 'group'" /><User v-else /></ElIcon>
              </span>
              <span class="mc-conversation-main">
                <span class="mc-conversation-title"
                  ><strong>{{ item.title }}</strong
                  ><time>{{ shortDate(item.last_at) }}</time></span
                >
                <span class="mc-conversation-preview">{{ preview(item.last) }}</span>
                <span class="mc-conversation-meta"
                  ><span>{{ item.scene === 'group' ? '群聊' : '私聊' }}</span
                  ><span>{{ item.count }} 条记录</span></span
                >
              </span>
            </button>
          </div>
          <ElPagination
            v-if="total > 50"
            small
            layout="prev, pager, next"
            :total="total"
            :page-size="50"
            :current-page="page"
            @current-change="changePage"
          />
        </section>
        <section class="mc-thread" aria-label="解码消息">
          <template v-if="current">
            <div class="mc-thread-heading">
              <ElButton
                class="mc-back"
                text
                :icon="ArrowLeft"
                aria-label="返回会话列表"
                @click="selectConversation('')"
              />
              <MessageImage
                v-if="current.scene === 'group' && current.avatar_url"
                :url="current.avatar_url"
                :workspace="workspace"
                :scope="scope"
                :label="current.title"
                avatar
              />
              <div class="mc-thread-title"
                ><h2
                  :aria-description="`${current.scene === 'group' ? '原生群' : '原生用户'} ${current.business_id} · ${mappingLabel(current.mapping)}`"
                  >{{ current.title }}</h2
                ><span
                  >{{ current.scene === 'group' ? '原生群' : '原生用户' }}
                  {{ current.business_id }}
                  <ElTooltip :content="mappingLabel(current.mapping)">
                    <ElIcon class="mc-mapping" :aria-label="mappingLabel(current.mapping)"
                      ><InfoFilled
                    /></ElIcon> </ElTooltip></span
              ></div>
              <div class="mc-thread-actions">
                <NativeRecalls
                  :account="account"
                  :conversation="selected"
                  :workspace="workspace"
                  :scope="scope"
                />
                <span class="mc-read-only">{{ historyMode ? '历史浏览' : '消息记录' }}</span>
                <ElTooltip v-if="historyMode || !followLatest" content="返回最新消息">
                  <ElButton text :icon="Bottom" aria-label="返回最新消息" @click="resumeLatest" />
                </ElTooltip>
              </div>
            </div>
            <ElAlert v-if="messageError" :title="messageError" type="error" :closable="false" />
            <div
              ref="messageViewport"
              class="mc-messages"
              :aria-busy="messagesLoading"
              @scroll="trackScroll"
            >
              <ElButton
                v-if="nextBefore"
                text
                :loading="messagesLoading"
                @click="loadMessages(true)"
                >加载更早消息</ElButton
              >
              <ElEmpty
                v-if="!messages.length && !messagesLoading"
                description="暂无可显示消息"
                :image-size="48"
              />
              <div class="mc-message-items">
                <template v-for="(item, index) in orderedMessages" :key="item.id">
                  <div
                    v-if="
                      index === 0 ||
                      dayLabel(item.received_at) !==
                        dayLabel(orderedMessages[index - 1].received_at)
                    "
                    class="mc-day"
                    ><time :datetime="item.received_at">{{ dayLabel(item.received_at) }}</time></div
                  >
                  <article class="mc-message">
                    <MessageImage
                      :url="item.media?.sender"
                      :workspace="workspace"
                      :scope="scope"
                      :label="senderLabel(item)"
                      avatar
                    />
                    <div class="mc-message-content">
                      <header
                        ><strong>{{ senderLabel(item) }}</strong
                        ><time :datetime="item.received_at" :title="date(item.received_at)">{{
                          clock(item.received_at)
                        }}</time></header
                      >
                      <div class="mc-bubble">
                        <blockquote v-if="item.content.message.quote">{{
                          messageQuotePreview(item, messages)
                        }}</blockquote>
                        <p v-if="item.content.message.image?.preview_limited" class="mc-unsupported">
                          图片 · 超出预览限制，消息已保留
                        </p>
                        <MessageImage
                          v-else-if="!item.native_view && item.content.message.image"
                          :url="item.media?.image"
                          :workspace="workspace"
                          :scope="scope"
                          :sticker="item.content.message.image.sticker"
                        />
                        <ChiliMessage
                          v-else-if="item.native_view"
                          :view="item.native_view"
                          :workspace="workspace"
                          :scope="scope"
                        />
                        <div v-else-if="item.content.message.notice" class="mc-notice">
                          <ElTag size="small" type="info" effect="plain">{{
                            noticeLabel(item.content.message.notice)
                          }}</ElTag>
                          <p>{{ item.content.message.notice.text || noticeLabel(item.content.message.notice) }}</p>
                          <small v-if="item.content.message.notice.verification === 'transport'">原生通知 · 只读</small>
                        </div>
                        <MessageForward
                          v-else-if="item.content.message.forward"
                          :forward="item.content.message.forward"
                          :links="item.media?.entries || []"
                          :workspace="workspace"
                          :scope="scope"
                        />
                        <MessageVideo
                          v-else-if="item.content.message.video"
                          :video="item.content.message.video"
                          :url="item.media?.video"
                          :poster="item.media?.poster"
                          :workspace="workspace"
                          :scope="scope"
                        />
                        <MessageContact
                          v-else-if="item.content.message.friend_card"
                          :contact="item.content.message.friend_card"
                          :avatar="item.media?.contact"
                          :workspace="workspace"
                          :scope="scope"
                        />
                        <p v-else-if="item.content.message.voice">
                          语音 · {{ item.content.message.voice.duration }} 秒
                        </p>
                        <p v-else-if="item.content.message.file">
                          文件 · {{ item.content.message.file.filename }} ·
                          {{ (item.content.message.file.size / 1048576).toFixed(2) }} MiB
                        </p>
                        <p v-else-if="item.state === 'decoded'">{{
                          item.content.message.text || '（空文本）'
                        }}</p>
                        <span v-else class="mc-unsupported">{{ unsupportedLabel(item) }}</span>
                        <ElTag v-if="item.display_truncated" size="small" type="info" effect="plain"
                          >长消息截断预览</ElTag
                        >
                        <footer v-if="item.content.message.mentions?.length">
                          <span
                            v-for="(mention, mentionIndex) in item.content.message.mentions"
                            :key="mentionIndex"
                            >@{{ mention.name || mention.uid }}</span
                          >
                        </footer>
                      </div>
                    </div>
                  </article>
                </template>
              </div>
            </div>
          </template>
          <div v-else class="mc-thread-empty">
            <ElIcon><ChatDotRound /></ElIcon>
            <p>选择群聊或私聊查看消息</p>
            <span>只读 · 消息保留 7 天</span>
          </div>
          <MessageComposer
            v-show="!!current"
            :account="account"
            :workspace="workspace"
            :conversation="current?.key || ''"
            :scene="current?.scene || ''"
            :title="current?.title || ''"
            :drafts="sendingDrafts"
          />
        </section>
      </div>
      <section v-else-if="tab === 'groups'" class="mc-directory">
        <div class="mc-directory-heading">
          <span>目录更新 {{ date(directory?.sampled_at) }}</span>
          <ElTag v-if="directory?.stale" type="warning">快照过期或尚未同步</ElTag>
          <ElButton :icon="Refresh" :loading="directoryLoading" @click="refreshDirectory"
            >同步群目录</ElButton
          >
        </div>
        <ElAlert
          v-if="directory?.error"
          :title="directoryError(directory.error)"
          type="warning"
          :closable="false"
        />
        <ElTable :data="directory?.groups || []" empty-text="暂无原生群目录">
          <ElTableColumn label="群名称" min-width="190">
            <template #default="{ row }">
              <div class="mc-group-name">
                <MessageImage
                  :url="row.avatar_url"
                  :workspace="workspace"
                  :scope="scope"
                  :label="row.name"
                  avatar
                />
                <span>{{ row.name }}</span>
              </div>
            </template>
          </ElTableColumn>
          <ElTableColumn prop="id" label="业务群 ID" min-width="110" />
          <ElTableColumn prop="team" label="传输群 ID" min-width="130" show-overflow-tooltip />
          <ElTableColumn label="账号角色" width="100"
            ><template #default="{ row }">{{ roleLabel(row.role) }}</template></ElTableColumn
          >
          <ElTableColumn label="授权" width="150"
            ><template #default>原生目录 · 非 SaaS 授权</template></ElTableColumn
          >
          <ElTableColumn label="成员" width="100" fixed="right">
            <template #default="{ row }">
              <ElButton
                link
                type="primary"
                :disabled="directory?.stale"
                :aria-label="`读取${row.name}成员`"
                @click="memberGroup = row"
                >查看成员</ElButton
              >
            </template>
          </ElTableColumn>
        </ElTable>
        <ElPagination
          v-if="directoryTotal > 100"
          layout="prev, pager, next"
          :total="directoryTotal"
          :page-size="100"
          :current-page="groupPage"
          @current-change="changeGroupPage"
        />
      </section>
    </template>
  </div>
  <NativeMembers
    :account="account"
    :workspace="workspace"
    :scope="scope"
    :group="memberGroup"
    @close="memberGroup = null"
  />
</template>

<script setup lang="ts">
  import { computed, nextTick, onUnmounted, reactive, ref, watch } from 'vue'
  import { ElMessage } from 'element-plus'
  import {
    ArrowDown,
    ArrowLeft,
    Bottom,
    ChatDotRound,
    DataAnalysis,
    InfoFilled,
    Refresh,
    Search,
    User
  } from '@element-plus/icons-vue'
  import { api, type Row } from './api'
  import { useSession } from './store'
  import { accountLive, type LiveStatus } from './accountLive'
  import NativeMembers from './NativeMembers.vue'
  import MessageComposer from './MessageComposer.vue'
  import MessageImage from './MessageImage.vue'
  import MessageForward from './MessageForward.vue'
  import MessageContact from './MessageContact.vue'
  import MessageVideo from './MessageVideo.vue'
  import ChiliMessage from './ChiliMessage.vue'
  import NativeRecalls from './NativeRecalls.vue'
  import type { SendingDraft } from './messageSending'
  import { messageQuotePreview } from './messageQuote'

  const props = defineProps<{ account: string; workspace: import('./workspaces').Workspace }>()
  const session = useSession()
  const sendingDrafts = reactive(new Map<string, SendingDraft>())
  watch(
    () => session.user?.id,
    () => sendingDrafts.clear()
  )
  const tab = ref('conversations'),
    scene = ref(''),
    search = ref('')
  const page = ref(1),
    groupPage = ref(1),
    total = ref(0),
    directoryTotal = ref(0)
  const health = ref<Row | null>(null),
    directory = ref<Row | null>(null)
  const memberGroup = ref<Row | null>(null)
  const items = ref<Row[]>([]),
    messages = ref<Row[]>([]),
    selected = ref('')
  const nextBefore = ref(''),
    error = ref(''),
    messageError = ref('')
  const historyMode = ref(false)
  const monitorOpen = ref(false),
    followLatest = ref(true),
    messageViewport = ref<HTMLElement | null>(null)
  const messageLimit = 500
  const loading = ref(false),
    messagesLoading = ref(false),
    directoryLoading = ref(false)
  let generation = 0,
    messageGeneration = 0
  let controller = new AbortController(),
    messageController = new AbortController()
  let stopLive: (() => void) | undefined
  let listTimer: ReturnType<typeof setTimeout> | undefined
  let deltaController = new AbortController()
  let deltaRunning = false
  const pendingIDs = new Set<string>()
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
  const scope = computed(() => `${props.workspace}:${props.account}:${session.user?.id}:messages`)
  watch(scope, () => {
    memberGroup.value = null
    monitorOpen.value = false
  })
  const current = computed(() => items.value.find((item) => item.key === selected.value))
  const filtered = computed(() =>
    items.value.filter((item) =>
      `${item.title} ${item.business_id}`.toLowerCase().includes(search.value.toLowerCase())
    )
  )
  const orderedMessages = computed(() => [...messages.value].reverse())
  const connected = computed(
    () => health.value?.receiver.observed && health.value.receiver.snapshot?.state === 'connected'
  )
  const receiveLabel = computed(() =>
    !health.value?.receiver.enabled
      ? '未启用'
      : health.value?.receiver.snapshot?.state === 'blocked'
        ? '接收暂停'
        : !health.value?.receiver.observed
          ? '状态待确认'
          : '未连接'
  )
  const receiveIssue = computed(() => {
    const receiver = health.value?.receiver
    if (!receiver) return null
    const snapshot = receiver.snapshot
    if (!receiver.enabled) {
      return {
        title: '消息接收未启用',
        detail: '账号登录与消息接收独立；已保存的历史消息仍可查看。',
        type: 'info' as const
      }
    }
    if (snapshot?.state !== 'blocked') return null
    const packet = snapshot.blocked_packet ? `协议包 ${snapshot.blocked_packet}。` : ''
    return {
      title:
        snapshot.error === 'native_push_boundary_unknown'
          ? '消息接收已暂停：协议通知尚未兼容'
          : '消息接收已暂停',
      detail:
        snapshot.error === 'native_push_boundary_unknown'
          ? `${packet}原始数据已隔离保存，未跳过恢复水位。此问题与登录是否成功无关，需修复协议解析后恢复接收。`
          : `原因：${snapshot.error || '待诊断'}。登录状态不代表消息连接可用。`,
      type: 'warning' as const
    }
  })
  const monitorLabel = computed(() => {
    if (!health.value) return '解码监控'
    if (receiveIssue.value || !connected.value) return receiveLabel.value
    return (
      {
        healthy: '解码正常',
        idle: '等待消息',
        degraded: '解码异常',
        blocked: '解码积压',
        unavailable: '解码不可用'
      }[health.value.status as string] || '解码监控'
    )
  })
  const monitorTone = computed(() =>
    !health.value
      ? 'neutral'
      : receiveIssue.value || !connected.value || !['healthy', 'idle'].includes(health.value.status)
        ? 'warning'
        : 'healthy'
  )
  const reasons: Record<string, string> = {
    authentication: '认证解密',
    packet_signature: '签名包格式或认证',
    envelope_authentication_missing: '未认证封装（未兼容类型）',
    notice_scope_unresolved: '原生通知尚无唯一群目录对应',
    schema_image: '图片元数据',
    schema_target: '缺少或无效的消息目标',
    schema_notice: '通知结构',
    packet_message: '签名包正文',
    encoding: '编码',
    envelope: '信封',
    compression: '解压',
    schema: '结构',
    routing: '路由绑定',
    message_key_missing: '缺少消息密钥',
    native_format: '未支持的原生类型',
    application_format: '未支持的正文类型',
    sealed_source_invalid: '加密源不可读'
  }
  const reasonRows = computed(() =>
    Object.entries(health.value?.reasons || {}).map(([reason, count]) => ({
      label: reasons[reason] || reason,
      count
    }))
  )
  const noticeLabel = (notice: { kind: string }) =>
    ({
      group_notification: '群通知',
      native_group_notification: '群事件通知',
      group_announcement: '群公告',
      red_envelope: '红包消息',
      red_envelope_tip: '红包提示',
      extension_notification: '扩展通知',
      auto_reply_tip: '自动回复提示',
      tip: '提示',
      game: '游戏消息'
    }[notice.kind] || '通知')
  const date = (value?: string) =>
    value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '无采样'
  const clock = (value: string) => new Date(value).toLocaleTimeString('zh-CN', { hour12: false })
  const dayLabel = (value: string) => new Date(value).toLocaleDateString('zh-CN')
  const shortDate = (value: string) =>
    dayLabel(value) === dayLabel(new Date().toISOString())
      ? new Date(value).toLocaleTimeString('zh-CN', {
          hour12: false,
          hour: '2-digit',
          minute: '2-digit'
        })
      : new Date(value).toLocaleDateString('zh-CN', { month: '2-digit', day: '2-digit' })
  const senderLabel = (item: Row) =>
    String(
      item.content.message.sender.name ||
        item.content.native_sender ||
        item.content.message.sender.id ||
        '原生事件'
    )
  const ms = (value: unknown) => (typeof value === 'number' ? `${value.toFixed(2)} ms` : '无采样')
  const healthLabel = (value: string) =>
    ({
      healthy: '健康',
      degraded: '部分未支持或失败',
      blocked: '积压 / 故障',
      unavailable: '不可用',
      idle: '等待消息'
    })[value] || value
  const roleLabel = (value: string) =>
    ({ owner: '群主', admin: '管理员', member: '成员', unknown: '待核验' })[value] || '待核验'
  const mappingLabel = (value: string) =>
    value === 'directory_matched'
      ? '目录匹配 · 非操作授权'
      : value === 'authenticated_header'
        ? '应用头已认证 · 传输映射待核验'
        : '映射待核验'
  const preview = (value: Row) =>
    value.message?.notice
      ? noticeLabel(value.message.notice)
      : value.message?.image
      ? value.message.image.sticker
        ? '[表情包]'
        : '[图片]'
      : value.message?.video
        ? '[视频]'
        : value.message?.forward
          ? '[聊天记录]'
          : value.message?.friend_card
            ? `[个人名片] ${value.message.friend_card.nickname || ''}`
            : value.message?.format === 0 && value.message?.text
              ? value.message.text
              : '未支持的消息类型'
  const unsupportedLabel = (item: Row) =>
    item.content.platform === 'lajiaoliao'
      ? `辣椒聊原生类型 ${item.content.native_format ?? '未知'} · 暂不解析`
      : item.reason === 'native_format'
        ? `原生类型 ${item.content.native_format}`
        : `正文类型 ${item.content.message.format} · 暂不解析`
  const directoryError = (code: string) =>
    ({
      native_rate_limited: '原生读取限流，保留旧快照并等待重试',
      native_reauth_required: '原生会话需重新验证',
      native_directory_identity: '群身份映射冲突',
      native_directory_shape: '群目录格式不兼容'
    })[code] || '群目录同步失败，保留旧快照'

  function failure(e: unknown) {
    return e instanceof Error ? e.message : '消息中心加载失败'
  }
  function saveView() {
    session.saveDraft(scope.value, {
      tab: tab.value,
      scene: scene.value,
      selected: selected.value,
      page: page.value,
      groupPage: groupPage.value
    })
  }
  async function load() {
    if (controller.signal.aborted) controller = new AbortController()
    if (liveState.value === 'blocked') startLive()
    return loadSnapshot(true, true)
  }
  async function loadSnapshot(metricsNeeded: boolean, threadNeeded: boolean) {
    if (!props.account || loading.value) return
    const version = generation,
      account = props.account,
      workspace = props.workspace
    loading.value = true
    try {
      const [list, metrics] = await Promise.all([
        api<Row>(
          `/accounts/${account}/conversations?scene=${scene.value}&offset=${(page.value - 1) * 50}&group_offset=${(groupPage.value - 1) * 100}`,
          'GET',
          undefined,
          controller.signal,
          workspace
        ),
        metricsNeeded
          ? api<Row>(
              `/accounts/${account}/decoder-health`,
              'GET',
              undefined,
              controller.signal,
              workspace
            )
          : Promise.resolve(null)
      ])
      if (generation !== version) return
      items.value = list.items
      total.value = list.total
      directory.value = list.directory
      directoryTotal.value = list.directory_total
      if (
        metrics &&
        (!health.value || Date.parse(metrics.sampled_at) >= Date.parse(health.value.sampled_at))
      )
        health.value = metrics
      error.value = ''
      if (selected.value && !current.value) {
        selected.value = ''
        messages.value = []
        nextBefore.value = ''
      }
      if (threadNeeded && selected.value && !messagesLoading.value && !historyMode.value)
        void loadMessages()
    } catch (e) {
      if (generation === version && !controller.signal.aborted) error.value = failure(e)
    } finally {
      if (generation === version) loading.value = false
    }
  }
  async function loadMessages(older = false) {
    if (!props.account || !selected.value || messagesLoading.value) return
    const version = ++messageGeneration,
      account = props.account,
      workspace = props.workspace,
      key = selected.value
    messageController.abort()
    messageController = new AbortController()
    messagesLoading.value = true
    const viewport = messageViewport.value,
      previousHeight = viewport?.scrollHeight || 0,
      previousTop = viewport?.scrollTop || 0
    try {
      const result = await api<Row>(
        `/accounts/${account}/conversations/${key}/messages${older ? `?before=${nextBefore.value}` : ''}`,
        'GET',
        undefined,
        messageController.signal,
        workspace
      )
      if (
        version !== messageGeneration ||
        key !== selected.value ||
        account !== props.account ||
        workspace !== props.workspace
      )
        return
      mergeMessages(result.items, older)
      if (older || !nextBefore.value) nextBefore.value = result.next_before
      if (older) historyMode.value = true
      messageError.value = ''
      if (older) {
        await nextTick()
        if (version === messageGeneration && viewport && viewport === messageViewport.value)
          viewport.scrollTop = previousTop + viewport.scrollHeight - previousHeight
      }
    } catch (e) {
      if (version === messageGeneration && !messageController.signal.aborted)
        messageError.value = failure(e)
    } finally {
      if (version === messageGeneration) messagesLoading.value = false
    }
  }
  function mergeMessages(incoming: Row[], older = false) {
    const merged = new Map<string, Row>()
    for (const item of [...messages.value, ...incoming]) merged.set(item.id, item)
    const rows = [...merged.values()]
      .filter((item) => Date.parse(item.received_at) > Date.now() - 7 * 86400000)
      .sort((a, b) => (BigInt(a.id) > BigInt(b.id) ? -1 : 1))
    messages.value = older ? rows.slice(-messageLimit) : rows.slice(0, messageLimit)
    if (!older && rows.length > messageLimit)
      nextBefore.value = messages.value[messages.value.length - 1].id
  }
  function resumeLatest() {
    selectConversation(selected.value)
  }
  function trackScroll() {
    const viewport = messageViewport.value
    if (viewport)
      followLatest.value = viewport.scrollHeight - viewport.scrollTop - viewport.clientHeight < 64
  }
  watch(messages, async () => {
    if (historyMode.value || !followLatest.value) return
    const version = generation,
      key = selected.value
    await nextTick()
    if (version === generation && key === selected.value && messageViewport.value)
      messageViewport.value.scrollTop = messageViewport.value.scrollHeight
  })
  function selectConversation(key: string) {
    deltaController.abort()
    deltaController = new AbortController()
    pendingIDs.clear()
    messageController.abort()
    messageGeneration++
    messagesLoading.value = false
    selected.value = key
    historyMode.value = false
    followLatest.value = true
    messages.value = []
    nextBefore.value = ''
    messageError.value = ''
    saveView()
    void loadMessages()
  }
  function scheduleList() {
    if (listTimer) return
    listTimer = setTimeout(() => {
      listTimer = undefined
      if (!loading.value) void loadSnapshot(false, false)
      else scheduleList()
    }, 3000)
  }
  async function drainDelta() {
    if (deltaRunning || historyMode.value || !selected.value || pendingIDs.size === 0) return
    const key = selected.value,
      version = generation,
      account = props.account,
      workspace = props.workspace,
      signal = deltaController.signal
    const ids = [...pendingIDs]
    pendingIDs.clear()
    deltaRunning = true
    try {
      const result = await api<Row>(
        `/accounts/${account}/conversations/${key}/messages?ids=${ids.join(',')}`,
        'GET',
        undefined,
        signal,
        workspace
      )
      if (version !== generation || key !== selected.value || signal.aborted || historyMode.value)
        return
      mergeMessages(result.items)
      messageError.value = ''
    } catch (e) {
      if (version === generation && !signal.aborted) {
        messageError.value = failure(e)
        // Lost notifications do not lose durable rows; reload this thread.
        void loadMessages()
      }
    } finally {
      deltaRunning = false
      if (pendingIDs.size) void drainDelta()
    }
  }
  function startLive() {
    stopLive?.()
    const version = generation
    if (!props.account) return
    stopLive = accountLive({
      account: props.account,
      workspace: props.workspace,
      view: 'messages',
      status: (value) => {
        if (version !== generation) return
        liveState.value = value
        if (value === 'blocked') {
          generation++
          messageGeneration++
          controller.abort()
          messageController.abort()
          deltaController.abort()
          clearTimeout(listTimer)
          listTimer = undefined
          pendingIDs.clear()
          loading.value = false
          messagesLoading.value = false
          directoryLoading.value = false
          items.value = []
          messages.value = []
          health.value = null
          directory.value = null
          total.value = 0
          directoryTotal.value = 0
          nextBefore.value = ''
          error.value = '实时订阅不可用，请确认当前权限后刷新'
        }
      },
      update: (data) => {
        if (version !== generation) return
        messages.value = messages.value.filter(
          (item) => Date.parse(item.received_at) > Date.now() - 7 * 86400000
        )
        if (data.health) health.value = data.health
        if (data.receiver && health.value) health.value.receiver = data.receiver
        if (data.reset) {
          scheduleList()
          if (selected.value && !historyMode.value) void loadMessages()
        } else if (data.conversations_changed || data.directory_changed) scheduleList()
        for (const id of data.decoded_ids || []) {
          if (!selected.value || historyMode.value) break
          if (pendingIDs.size >= 64) {
            pendingIDs.clear()
            void loadMessages()
            break
          }
          pendingIDs.add(id)
        }
        void drainDelta()
      }
    })
  }
  function changeScene() {
    page.value = 1
    selected.value = ''
    invalidateList()
  }
  function changePage(value: number) {
    page.value = value
    selected.value = ''
    invalidateList()
  }
  function changeGroupPage(value: number) {
    groupPage.value = value
    invalidateList()
  }
  function invalidateList() {
    stopLive?.()
    generation++
    controller.abort()
    controller = new AbortController()
    loading.value = false
    messageGeneration++
    messageController.abort()
    messagesLoading.value = false
    deltaController.abort()
    deltaController = new AbortController()
    pendingIDs.clear()
    historyMode.value = false
    messages.value = []
    nextBefore.value = ''
    saveView()
    void load()
    startLive()
  }
  async function refreshDirectory() {
    const version = generation
    directoryLoading.value = true
    try {
      await api(
        `/accounts/${props.account}/directory-refresh`,
        'POST',
        {},
        controller.signal,
        props.workspace
      )
      if (version === generation) {
        ElMessage.success('群目录已进入后台同步队列')
        await load()
      }
    } catch (e) {
      if (version === generation) error.value = failure(e)
    } finally {
      if (version === generation) directoryLoading.value = false
    }
  }
  watch(
    () => [props.workspace, props.account, session.user?.id],
    () => {
      stopLive?.()
      clearTimeout(listTimer)
      listTimer = undefined
      deltaController.abort()
      deltaController = new AbortController()
      pendingIDs.clear()
      historyMode.value = false
      followLatest.value = true
      generation++
      messageGeneration++
      controller.abort()
      messageController.abort()
      controller = new AbortController()
      messageController = new AbortController()
      loading.value = false
      messagesLoading.value = false
      directoryLoading.value = false
      items.value = []
      messages.value = []
      health.value = null
      directory.value = null
      total.value = 0
      directoryTotal.value = 0
      nextBefore.value = ''
      error.value = ''
      messageError.value = ''
      const saved = session.drafts[scope.value] as Row | undefined
      tab.value = ['conversations', 'groups'].includes(saved?.tab) ? saved!.tab : 'conversations'
      scene.value = ['group', 'private'].includes(saved?.scene) ? saved!.scene : ''
      selected.value = saved?.selected || ''
      page.value = saved?.page || 1
      groupPage.value = saved?.groupPage || 1
      search.value = ''
      void load()
      startLive()
    },
    { immediate: true }
  )
  watch(tab, saveView)
  onUnmounted(() => {
    generation++
    messageGeneration++
    stopLive?.()
    clearTimeout(listTimer)
    deltaController.abort()
    controller.abort()
    messageController.abort()
  })
</script>

<style scoped>
  .message-center {
    display: flex;
    flex: 1;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    border: 1px solid var(--el-border-color-light);
    border-radius: 6px;
    overflow: hidden;
    background: var(--el-bg-color);
  }
  .mc-sr-only {
    position: absolute;
    width: 1px;
    height: 1px;
    overflow: hidden;
    clip-path: inset(50%);
    white-space: nowrap;
  }
  .message-center > :deep(.el-empty) {
    flex: 1;
  }
  .mc-toolbar {
    display: flex;
    align-items: center;
    flex-shrink: 0;
    justify-content: space-between;
    gap: 8px;
    min-height: 54px;
    padding: 0 12px 0 18px;
    border-bottom: 1px solid var(--el-border-color-light);
  }
  .mc-toolbar-actions,
  .mc-thread-actions {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-shrink: 0;
  }
  .mc-toolbar-actions > :deep(.el-button) {
    width: 32px;
    height: 32px;
    margin: 0;
    padding: 0;
  }
  .mc-tabs {
    min-width: 0;
  }
  .mc-tabs :deep(.el-tabs__header) {
    margin: 0;
  }
  .mc-tabs :deep(.el-tabs__nav-wrap::after),
  .mc-tabs :deep(.el-tabs__content) {
    display: none;
  }
  .mc-tabs :deep(.el-tabs__item) {
    height: 53px;
    padding: 0 12px;
    font-size: 13px;
  }
  .mc-monitor-button {
    display: inline-flex;
    align-items: center;
    gap: 7px;
    height: 30px;
    max-width: 150px;
    padding: 0 8px;
    border: 0;
    border-radius: 4px;
    background: transparent;
    color: var(--el-text-color-secondary);
    font-size: 12px;
    cursor: pointer;
  }
  .mc-monitor-button:hover {
    background: var(--el-fill-color-light);
  }
  .mc-monitor-button:focus-visible,
  .mc-conversation:focus-visible {
    outline: 2px solid var(--el-color-primary);
    outline-offset: -2px;
  }
  .mc-monitor-label {
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .mc-health-dot {
    flex: 0 0 6px;
    width: 6px;
    height: 6px;
    border-radius: 50%;
    background: var(--el-text-color-placeholder);
  }
  .mc-monitor-button.healthy .mc-health-dot {
    background: var(--el-color-success);
  }
  .mc-monitor-button.warning {
    color: var(--el-color-warning-dark-2);
  }
  .mc-monitor-button.warning .mc-health-dot {
    background: var(--el-color-warning);
  }
  .mc-conversations {
    display: grid;
    grid-template-columns: 280px minmax(0, 1fr);
    flex: 1;
    min-height: 0;
  }
  .mc-list {
    display: flex;
    flex-direction: column;
    border-right: 1px solid var(--el-border-color-light);
    min-width: 0;
    min-height: 0;
    background: var(--el-fill-color-extra-light);
  }
  .mc-filter {
    display: grid;
    flex-shrink: 0;
    gap: 12px;
    padding: 16px 14px 12px;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .mc-filter :deep(.el-input__wrapper) {
    box-shadow: none;
    background: var(--el-fill-color);
  }
  .mc-filter :deep(.el-radio-group) {
    display: flex;
  }
  .mc-filter :deep(.el-radio-button) {
    flex: 1;
  }
  .mc-filter :deep(.el-radio-button__inner) {
    width: 100%;
  }
  .mc-list-scroll {
    flex: 1;
    min-height: 0;
    overflow: auto;
    overscroll-behavior: contain;
  }
  .mc-conversation {
    display: flex;
    align-items: center;
    gap: 12px;
    width: 100%;
    min-height: 88px;
    padding: 13px 14px;
    text-align: left;
    border: 0;
    border-left: 3px solid transparent;
    background: transparent;
    color: var(--el-text-color-primary);
    cursor: pointer;
  }
  .mc-conversation:hover {
    background: var(--el-fill-color-light);
  }
  .mc-conversation.selected {
    background: var(--el-color-primary-light-9);
    border-left-color: var(--el-color-primary);
  }
  .mc-avatar {
    display: grid;
    place-items: center;
    flex: 0 0 42px;
    width: 42px;
    height: 42px;
    border-radius: 8px;
    background: var(--el-color-primary-light-8);
    color: var(--el-color-primary-dark-2);
    font-size: 21px;
  }
  .mc-avatar.private {
    background: var(--el-color-success-light-8);
    color: var(--el-color-success-dark-2);
  }
  .mc-conversation-main {
    display: grid;
    flex: 1;
    gap: 6px;
    min-width: 0;
  }
  .mc-conversation-title,
  .mc-conversation-meta {
    display: flex;
    justify-content: space-between;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .mc-conversation-title strong,
  .mc-conversation-preview {
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .mc-conversation-title strong {
    font-size: 13px;
    font-weight: 600;
  }
  .mc-conversation-preview {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .mc-conversation-title time,
  .mc-conversation-meta {
    flex-shrink: 0;
    color: var(--el-text-color-placeholder);
    font-size: 10px;
  }
  .mc-thread {
    display: flex;
    flex-direction: column;
    min-width: 0;
    min-height: 0;
    background: var(--el-fill-color-lighter);
  }
  .mc-thread-heading {
    display: flex;
    align-items: center;
    gap: 12px;
    flex-shrink: 0;
    min-height: 76px;
    padding: 12px 22px;
    border-bottom: 1px solid var(--el-border-color-light);
    background: var(--el-bg-color);
  }
  .mc-thread-title {
    flex: 1;
    min-width: 0;
  }
  .mc-thread-heading h2 {
    margin: 0 0 6px;
    font-size: 16px;
    font-weight: 600;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .mc-thread-title > span {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
    overflow-wrap: anywhere;
  }
  .mc-mapping {
    flex-shrink: 0;
    color: var(--el-text-color-placeholder);
  }
  .mc-read-only {
    color: var(--el-text-color-placeholder);
    font-size: 11px;
  }
  .mc-back {
    display: none;
  }
  .mc-messages {
    flex: 1;
    min-height: 0;
    padding: 12px 24px 24px;
    overflow: auto;
    overscroll-behavior: contain;
  }
  .mc-messages > :deep(.el-button) {
    display: block;
    margin: 0 auto 10px;
  }
  .mc-message-items {
    display: flex;
    flex-direction: column;
    gap: 20px;
  }
  .mc-day {
    text-align: center;
    color: var(--el-text-color-placeholder);
    font-size: 11px;
    padding: 4px 0;
  }
  .mc-message {
    display: flex;
    align-items: flex-start;
    gap: 10px;
    overflow-wrap: anywhere;
  }
  .mc-sender-avatar {
    display: grid;
    flex: 0 0 34px;
    place-items: center;
    width: 34px;
    height: 34px;
    border: 1px solid var(--el-border-color-light);
    border-radius: 6px;
    background: var(--el-bg-color);
    color: var(--el-text-color-regular);
    font-size: 13px;
  }
  .mc-message-content {
    min-width: 0;
    max-width: min(82%, 760px);
  }
  .mc-message header {
    display: flex;
    flex-wrap: wrap;
    align-items: baseline;
    gap: 8px;
    margin-bottom: 6px;
    font-size: 12px;
    color: var(--el-text-color-secondary);
  }
  .mc-message header strong {
    font-weight: 400;
  }
  .mc-message time {
    font-size: 10px;
    color: var(--el-text-color-placeholder);
  }
  .mc-bubble {
    display: inline-block;
    max-width: 100%;
    padding: 10px 13px;
    border: 1px solid var(--el-border-color-lighter);
    border-radius: 0 8px 8px;
    background: var(--el-bg-color);
    color: var(--el-text-color-primary);
  }
  .mc-message p {
    white-space: pre-wrap;
    line-height: 1.7;
    margin: 0;
    font-size: 14px;
  }
  .mc-message blockquote {
    white-space: pre-wrap;
    margin: 0 0 8px;
    padding: 6px 10px;
    border-left: 3px solid var(--el-border-color);
    background: var(--el-fill-color-extra-light);
    color: var(--el-text-color-secondary);
    font-size: 12px;
    line-height: 1.6;
  }
  .mc-unsupported {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .mc-message footer {
    display: flex;
    flex-wrap: wrap;
    gap: 8px;
    color: var(--el-color-primary);
    font-size: 12px;
    margin-top: 8px;
  }
  .mc-thread-empty {
    display: flex;
    flex: 1;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 12px;
    padding: 24px;
    text-align: center;
    color: var(--el-text-color-placeholder);
  }
  .mc-thread-empty > .el-icon {
    font-size: 48px;
    color: var(--el-border-color);
  }
  .mc-thread-empty p {
    margin: 0;
    font-size: 13px;
  }
  .mc-thread-empty span {
    font-size: 11px;
  }
  .mc-directory {
    flex: 1;
    min-height: 0;
    overflow: auto;
    padding: 0 18px 16px;
  }
  .mc-group-name {
    display: flex;
    min-width: 0;
    align-items: center;
    gap: 10px;
  }
  .mc-group-name > span {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .mc-directory-heading {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 12px;
    margin: 12px 0;
    font-size: 13px;
    color: var(--el-text-color-secondary);
  }
  .mc-health {
    max-height: min(70dvh, 640px);
    overflow: auto;
    overscroll-behavior: contain;
  }
  .mc-health-heading {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: 12px;
    padding-bottom: 12px;
    font-size: 13px;
  }
  .mc-health dl {
    display: grid;
    margin: 0 0 12px;
  }
  .mc-health dl > div {
    display: flex;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 8px;
    padding: 9px 0;
    border-bottom: 1px solid var(--el-border-color-lighter);
    font-size: 12px;
  }
  .mc-health dt {
    color: var(--el-text-color-secondary);
  }
  .mc-health dd {
    margin: 0;
    overflow-wrap: anywhere;
    text-align: right;
  }
  .mc-health h3 {
    margin: 14px 0 8px;
    font-size: 13px;
  }
  .mc-sampled {
    color: var(--el-text-color-placeholder);
    font-size: 11px;
    margin: 12px 0 0;
  }
  :deep(.el-pagination) {
    padding: 12px;
    max-width: 100%;
    flex-wrap: wrap;
  }
  @media (max-width: 760px) {
    .message-center {
      position: relative;
      border: 0;
      border-radius: 0;
    }
    .mc-toolbar {
      gap: 4px;
      min-height: 44px;
      padding: 0 8px 0 16px;
      border: 0;
      background: #ededed;
    }
    .mc-tabs :deep(.el-tabs__item) {
      height: 44px;
      padding: 0 12px;
      font-size: 14px;
      color: #666;
    }
    .mc-tabs :deep(.el-tabs__item.is-active) {
      color: #07a45a;
    }
    .mc-tabs :deep(.el-tabs__active-bar) {
      background: #07a45a;
    }
    .mc-toolbar-actions {
      gap: 0;
    }
    .mc-toolbar-actions > :deep(.el-button) {
      width: 36px;
      height: 40px;
    }
    .mc-monitor-button {
      width: 40px;
      height: 40px;
      justify-content: center;
      gap: 5px;
      padding: 0;
      font-size: 16px;
    }
    .mc-monitor-label,
    .mc-monitor-chevron {
      display: none;
    }
    .mobile-thread .mc-toolbar {
      min-height: 0;
      height: 0;
      padding: 0;
    }
    .mobile-thread .mc-tabs {
      display: none;
    }
    .mobile-thread .mc-toolbar-actions {
      position: absolute;
      top: 4px;
      right: 8px;
      z-index: 2;
    }
    .mc-conversations {
      grid-template-columns: minmax(0, 1fr);
    }
    .mc-list {
      border: 0;
      background: #fff;
    }
    .mc-filter {
      gap: 8px;
      padding: 8px 16px;
      border: 0;
      background: #ededed;
    }
    .mc-filter :deep(.el-input__wrapper) {
      min-height: 34px;
      background: #fff;
      border-radius: 5px;
    }
    .mc-filter :deep(.el-radio-button__inner) {
      border: 0;
      border-radius: 4px;
      box-shadow: none;
      background: transparent;
      color: #777;
    }
    .mc-filter :deep(.el-radio-button.is-active .el-radio-button__inner) {
      background: #fff;
      color: #078446;
    }
    .mc-conversation {
      min-height: 76px;
      padding: 10px 16px;
      border: 0;
      gap: 12px;
    }
    .mc-conversation.selected {
      background: #f2f2f2;
    }
    .mc-conversation-main {
      position: relative;
      gap: 7px;
      padding: 8px 0;
    }
    .mc-conversation-main::after {
      position: absolute;
      right: -16px;
      bottom: -10px;
      left: 0;
      height: 1px;
      background: #f0f0f0;
      content: '';
    }
    .mc-conversation-title strong {
      font-size: 16px;
      font-weight: 500;
    }
    .mc-conversation-preview {
      font-size: 13px;
    }
    .mc-conversation-meta {
      display: none;
    }
    .mc-avatar {
      flex-basis: 48px;
      width: 48px;
      height: 48px;
      border-radius: 5px;
    }
    .mc-thread {
      background: #ededed;
    }
    .mc-conversations.has-thread .mc-list,
    .mc-conversations:not(.has-thread) .mc-thread {
      display: none;
    }
    .mc-back {
      display: inline-flex;
      flex-shrink: 0;
      width: 36px;
      height: 40px;
      padding: 0;
      margin: 0;
    }
    .mc-thread-heading,
    .mc-messages {
      padding-left: 12px;
      padding-right: 12px;
    }
    .mc-thread-heading {
      min-height: 48px;
      height: 48px;
      gap: 4px;
      padding: 0 88px 0 8px;
      border: 0;
      background: #ededed;
    }
    .mc-thread-heading h2 {
      margin: 0;
      font-size: 16px;
      font-weight: 500;
    }
    .mc-thread-title > span,
    .mc-read-only {
      display: none;
    }
    .mc-thread-actions {
      gap: 3px;
    }
    .mc-messages {
      padding: 12px 12px 20px;
    }
    .mc-message-items {
      gap: 18px;
    }
    .mc-sender-avatar {
      flex-basis: 38px;
      width: 38px;
      height: 38px;
      border: 0;
      border-radius: 4px;
      background: #d6e8dc;
      color: #426350;
    }
    .mc-message header {
      gap: 6px;
      margin-bottom: 5px;
      font-size: 11px;
    }
    .mc-bubble {
      position: relative;
      padding: 9px 12px;
      border: 0;
      border-radius: 4px;
      background: #fff;
    }
    .mc-bubble::before {
      position: absolute;
      top: 12px;
      left: -5px;
      width: 10px;
      height: 10px;
      transform: rotate(45deg);
      background: #fff;
      content: '';
    }
    .mc-message p {
      font-size: 15px;
      line-height: 1.6;
    }
    .mc-message-content {
      max-width: calc(100% - 58px);
    }
    .mc-directory {
      padding: 0 12px 12px;
    }
    .mc-directory-heading {
      gap: 8px;
      font-size: 12px;
    }
  }
</style>
