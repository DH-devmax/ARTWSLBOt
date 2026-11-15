<template>
  <ElTooltip v-if="!embedded" content="群管助手" placement="bottom">
    <button
      type="button"
      class="assistant-entry"
      aria-label="群管助手"
      aria-haspopup="dialog"
      :aria-expanded="open"
      @click="open = true"
      @keydown.enter.prevent="open = true"
      @keydown.space.prevent="open = true"
    >
      <AdminMark class="assistant-entry-mark" />
      <span class="assistant-entry-text">群管助手</span>
    </button>
  </ElTooltip>
  <AssistantShell v-model:open="open" :embedded="embedded">
    <template #header>
      <div class="assistant-heading">
        <div class="assistant-heading-title">
          <strong>{{ groupName || '群管助手' }}</strong>
          <ElTooltip v-if="groupName" :content="accessReason || accessLabel">
            <button
              type="button"
              class="assistant-ownership-status"
              :aria-label="`认领状态：${accessLabel}${accessReason ? `。${accessReason}` : ''}`"
              @click="helpOpen = true"
              >{{ accessLabel }}</button
            >
          </ElTooltip>
        </div>
        <small v-if="groupName">{{ platformLabel }} · {{ accountName }}</small>
        <small v-else>{{ platformLabel }}</small>
      </div>
      <div class="assistant-header-actions">
        <span class="assistant-round-count">{{ records.length }} / 10 轮</span>
        <ElTooltip content="历史对话">
          <ElButton
            :icon="Clock"
            text
            aria-label="历史对话"
            :disabled="sending || running || deleting"
            @click="historyOpen = true"
          />
        </ElTooltip>
        <ElTooltip content="新对话"
          ><ElButton
            :icon="Plus"
            text
            aria-label="新对话"
            :disabled="sending || running || loading || deleting || !scopeValid"
            :loading="creating"
            @click="startNew"
        /></ElTooltip>
      </div>
    </template>
    <template #sidebar>
      <div class="assistant-scope">
        <div class="assistant-scope-label"><span>选择群组</span></div>
        <ElRadioGroup
          class="assistant-platforms"
          :model-value="platform"
          :disabled="sending || running || deleting || approving || creating"
          aria-label="助手平台"
          @change="selectPlatform"
        >
          <ElRadioButton v-for="item in platforms" :key="item.id" :value="item.id">
            <img :src="item.logo" alt="" width="18" height="18" />
            <span>{{ item.name }}</span>
          </ElRadioButton>
        </ElRadioGroup>
        <ElButton
          class="assistant-mobile-groups"
          text
          :aria-expanded="groupPanelOpen"
          :aria-label="`${platformLabel} · ${groupName || '选择群组'}`"
          @click="groupPanelOpen = !groupPanelOpen"
        >
          <img class="assistant-platform-icon" :src="platformLogo" alt="" width="20" height="20" />
          <span class="assistant-current-group">{{ groupName || '选择群组' }}</span>
        </ElButton>
        <div class="assistant-group-panel" :class="{ collapsed: !groupPanelOpen }">
          <GroupPicker
            :key="platform"
            :groups="pickerGroups"
            :accounts="accounts"
            :selected="group"
            :request="input"
            :disabled="sending || running || deleting"
            :has-more="nextOffset >= 0"
            :loading="contextLoading"
            @select="selectGroup"
            @more="loadContext(true)"
            @search="searchGroups"
          />
        </div>
        <div v-if="embedded" class="assistant-sidebar-history">
          <ConversationHistory
            :items="history"
            :active="sessionID"
            :disabled="sending || running || loading || deleting"
            :can-create="scopeValid"
            :creating="creating"
            @select="chooseConversation"
            @create="startNew"
            @delete="requestDeletion"
          />
        </div>
      </div>
    </template>
    <div v-if="currentSession" class="assistant-conversation-title" aria-live="polite">
      {{ conversationLabel(currentSession) }}
    </div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <div
      ref="threadElement"
      class="assistant-thread"
      role="log"
      aria-label="助手对话记录"
      aria-live="polite"
      @scroll="trackScroll"
    >
      <ElSkeleton v-if="loading && !thread.length" :rows="3" animated />
      <article v-for="row in thread" :key="row.id" class="assistant-turn" :data-turn-id="row.id">
        <ElAlert
          v-if="row.unavailable"
          title="权限已变化，该回合内容不可读取"
          type="info"
          :closable="false"
        />
        <template v-else>
          <Bubble
            v-if="row.input?.trim()"
            class="assistant-user-message"
            placement="end"
            variant="filled"
            max-width="92%"
          >
            <template #content
              ><p class="assistant-question">{{ row.input }}</p></template
            >
          </Bubble>
          <details
            v-if="row.cards.length || (row.id === selectedTurn && running)"
            class="assistant-tool-process"
          >
            <summary>
              <ElIcon v-if="row.id === selectedTurn && running" class="is-loading"
                ><Loading
              /></ElIcon>
              <span>{{ row.id === selectedTurn && running ? '正在处理' : '处理过程' }}</span>
              <small v-if="row.cards.length">{{ row.cards.length }} 个步骤</small>
              <small v-if="row.id === selectedTurn && running">{{
                statusLabels[progress] || progress
              }}</small>
            </summary>
            <ol class="assistant-tool-steps">
              <li v-for="(card, i) in row.cards" :key="i">
                <div class="assistant-tool-step-label"
                  >{{ i + 1 }}. {{ toolLabels[String(card.type)] }}</div
                >
                <ResultCard
                  v-if="!interactiveCard(card)"
                  :card="displayCard(card)"
                  :available-groups="pickerGroups"
                  :accounts="accounts"
                  :disabled="sending || running || deleting"
                  @group="selectGroup"
                />
                <small v-else>{{
                  groupReadOnly
                    ? '当前群只读，建议不可提交。'
                    : '需要你选择或确认的内容显示在下方。'
                }}</small>
              </li>
            </ol>
          </details>
          <template v-for="(card, i) in row.cards.filter(interactiveCard)" :key="i">
            <p
              v-if="
                groupReadOnly &&
                ['propose_group_config_change', 'propose_group_action'].includes(String(card.type))
              "
              class="assistant-readonly-proposal"
              >{{ toolLabels[String(card.type)] }} · 只读：{{ accessReason }}</p
            >
            <ResultCard
              v-else
              :card="card"
              :available-groups="pickerGroups"
              :accounts="accounts"
              :disabled="sending || running || deleting || groupReadOnly"
              @plan="showPlan"
              @group="selectGroup"
              @action="showActionPlan"
              @target="chooseTarget"
            />
          </template>
          <Bubble
            v-if="row.answer.trim()"
            class="assistant-model-answer"
            placement="start"
            no-style
            max-width="100%"
          >
            <template #avatar>
              <span class="assistant-answer-avatar" role="img" aria-label="群管助手">
                <AdminMark class="assistant-answer-mark" />
              </span>
            </template>
            <template #content>
              <div class="assistant-answer-label">群管助手</div>
              <AssistantMarkdown class="assistant-answer" :content="row.answer" />
            </template>
          </Bubble>
          <ElAlert
            v-if="row.result_code && ['failed', 'interrupted', 'expired'].includes(row.status)"
            :title="errorText(row.result_code)"
            type="error"
            :closable="false"
          />
        </template>
        <Bubble
          v-for="outcome in row.outcomes"
          :key="`${outcome.kind}:${outcome.plan_id}`"
          class="assistant-outcome"
          :data-plan-id="outcome.plan_id"
          :data-outcome-status="outcome.status"
          placement="start"
          no-style
          max-width="100%"
        >
          <template #avatar>
            <span class="assistant-answer-avatar" role="img" aria-label="群管助手">
              <AdminMark class="assistant-answer-mark" />
            </span>
          </template>
          <template #content>
            <div class="assistant-answer-label">群管助手</div>
            <ElTag
              v-if="outcome.evidence_state"
              size="small"
              :type="outcome.evidence_state === 'receive_observed' ? 'success' : 'info'"
              >{{ evidenceLabel(outcome.evidence_state) }}</ElTag
            >
            <p class="assistant-answer assistant-outcome-summary">{{ outcome.summary }}</p>
          </template>
        </Bubble>
        <div class="assistant-turn-actions">
          <small v-if="!row.unavailable" class="assistant-turn-state">{{
            statusLabels[row.status] || row.status
          }}</small>
          <ElTooltip
            v-if="replyText(row) && !['queued', 'running'].includes(row.status)"
            :content="copiedTurn === row.id ? '已复制' : '复制回答'"
          >
            <ElButton
              :icon="copiedTurn === row.id ? Check : CopyDocument"
              text
              size="small"
              :aria-label="row.unavailable ? '复制结果总结' : `复制回答 ${row.input || ''}`"
              @click="copyAnswer(row)"
            />
          </ElTooltip>
        </div>
        <ElAlert
          v-if="row.truncated && !row.unavailable"
          title="本回合内容超过展示上限"
          type="info"
          :closable="false"
        />
      </article>
      <div v-if="!loading && !thread.length && !error" class="assistant-welcome">
        <AdminMark class="assistant-welcome-mark" />
        <strong>{{
          groupName ? (groupReadOnly ? '今天想查询什么？' : '今天想管理什么？') : '选择你的群组'
        }}</strong>
        <span>{{ groupName || '暂无选中的授权群' }}</span>
        <div v-if="scopeValid" class="assistant-starters">
          <button
            v-for="prompt in starters"
            :key="prompt"
            type="button"
            :disabled="deleting"
            @click="useStarter(prompt)"
          >
            <ElIcon><ChatLineRound /></ElIcon>{{ prompt }}<ElIcon><ArrowRight /></ElIcon>
          </button>
        </div>
      </div>
    </div>
    <template #footer>
      <div v-if="scrolledUp" class="assistant-scroll-action">
        <ElTooltip content="回到最新消息">
          <ElButton :icon="ArrowDown" circle aria-label="回到最新消息" @click="scrollToBottom" />
        </ElTooltip>
      </div>
      <div class="assistant-sender">
        <AssistantInput
          ref="composer"
          v-model="input"
          maxlength="8000"
          :disabled="sending || running || deleting"
          aria-label="助手问题"
          :placeholder="
            groupName ? `${groupReadOnly ? '查询' : '管理'}「${groupName}」…` : '请先选择授权群组'
          "
          @keydown="sendOnEnter"
        />
        <div class="assistant-compose">
          <div class="assistant-compose-actions">
            <ElTooltip content="助手能力说明">
              <ElButton :icon="Warning" text aria-label="助手能力说明" @click="helpOpen = true" />
            </ElTooltip>
            <ElTooltip content="重新读取"
              ><ElButton
                :icon="Refresh"
                text
                aria-label="重新读取"
                :disabled="!sessionID || sending || loading || deleting"
                @click="loadTurns"
            /></ElTooltip>
            <ElButton v-if="running" :icon="CircleClose" :loading="cancelling" @click="cancelTurn"
              >取消回合</ElButton
            >
            <ElButton
              type="primary"
              :icon="Promotion"
              :loading="sending"
              :disabled="
                running || loading || deleting || !input.trim() || inputBytes > 8000 || !scopeValid
              "
              @click="submit"
              >发送</ElButton
            >
          </div>
        </div>
      </div>
      <div class="assistant-footer-meta">
        <span v-if="usage" class="assistant-usage">
          用量 ·
          {{
            usage.unit === 'micro_credit'
              ? (usage.used_or_reserved / 1000000).toLocaleString('zh-CN', {
                  maximumFractionDigits: 6
                })
              : usage.used_or_reserved
          }}
          {{ usage.unit === 'micro_credit' ? '积分' : 'Token' }}
        </span>
      </div>
    </template>
  </AssistantShell>
  <AssistantHelp
    v-model:open="helpOpen"
    :platform="platform"
    :scope="scope"
    :group-name="groupName"
    :account-name="accountName"
    :actor-key="`${session.user?.id || ''}:${session.user?.tenant_id || ''}`"
    :group-access="groupAccess"
    @access="updateGroupAccess"
  />
  <ElDrawer
    v-model="historyOpen"
    title="历史对话"
    size="min(340px, 100vw)"
    append-to-body
    class="assistant-history-drawer"
  >
    <div class="assistant-history-scope"
      >{{ groupName || '尚未选择群组' }} · {{ platformLabel }}</div
    >
    <ConversationHistory
      :key="key"
      :items="history"
      :active="sessionID"
      :disabled="sending || running || loading || deleting"
      :can-create="scopeValid"
      :creating="creating"
      @select="chooseConversation"
      @create="startNew"
      @delete="requestDeletion"
    />
  </ElDrawer>
  <ElDialog
    v-model="deleteOpen"
    title="删除对话"
    width="min(420px, calc(100vw - 24px))"
    append-to-body
    :close-on-click-modal="false"
    :close-on-press-escape="!deleting"
    :show-close="!deleting"
  >
    <template v-if="deleteTarget">
      <p class="assistant-delete-title">{{ conversationLabel(deleteTarget) }}</p>
      <p class="assistant-delete-scope">{{ groupName }} · {{ platformLabel }}</p>
      <p class="assistant-delete-note"
        >对话内容将被清除，尚待确认的建议同时取消。费用和已提交操作记录保留。</p
      >
      <ElAlert v-if="deleteError" :title="deleteError" type="error" :closable="false" />
    </template>
    <template #footer>
      <ElButton :disabled="deleting" @click="deleteOpen = false">取消</ElButton>
      <ElButton type="danger" :icon="Delete" :loading="deleting" @click="deleteConversation"
        >删除对话</ElButton
      >
    </template>
  </ElDialog>
  <ElDialog
    v-model="planOpen"
    title="配置建议审批"
    width="min(580px, calc(100vw - 24px))"
    append-to-body
    :close-on-click-modal="false"
  >
    <template v-if="plan">
      <ElAlert v-if="groupReadOnly" :title="accessReason" type="info" :closable="false" />
      <ElDescriptions :column="1" border>
        <ElDescriptionsItem label="群">{{ plan.group_id }}</ElDescriptionsItem>
        <ElDescriptionsItem label="基础版本">{{ plan.base_version }}</ElDescriptionsItem>
        <ElDescriptionsItem label="状态">{{ plan.status }}</ElDescriptionsItem>
        <ElDescriptionsItem label="过期时间">{{
          new Date(plan.expires_at).toLocaleString()
        }}</ElDescriptionsItem>
        <ElDescriptionsItem label="执行编号" v-if="plan.execution_id">{{
          plan.execution_id
        }}</ElDescriptionsItem>
      </ElDescriptions>
      <ConfigDiff :before="plan.before" :changes="plan.changes" />
      <ElAlert :title="plan.message" type="info" :closable="false" />
      <ElAlert v-if="planError" :title="planError" type="error" :closable="false" />
    </template>
    <template #footer>
      <ElButton :icon="Refresh" :disabled="!plan || approving" @click="plan && showPlan(plan.id)"
        >核对状态</ElButton
      >
      <ElButton
        v-if="plan?.status === 'pending_approval' && canPropose"
        :disabled="approving"
        @click="cancelPlan"
        >取消建议</ElButton
      >
      <ElButton
        v-if="plan?.status === 'pending_approval' && canApprove"
        type="primary"
        :loading="approving"
        @click="approve"
        >确认保存配置</ElButton
      >
    </template>
  </ElDialog>
  <ElDialog
    v-model="actionOpen"
    title="确认群管动作"
    width="min(560px, calc(100vw - 24px))"
    append-to-body
    :close-on-click-modal="false"
  >
    <template v-if="actionPlan">
      <ElAlert v-if="groupReadOnly" :title="accessReason" type="info" :closable="false" />
      <ElDescriptions :column="1" border>
        <ElDescriptionsItem label="群组">{{ groupName }} · {{ platformLabel }}</ElDescriptionsItem>
        <ElDescriptionsItem label="机器人">{{ accountName }}</ElDescriptionsItem>
        <ElDescriptionsItem label="动作">{{ actionPlan.name }}</ElDescriptionsItem>
        <ElDescriptionsItem v-if="actionPlan.target" label="目标"
          >{{ actionPlan.target.name }} · {{ actionPlan.target.id }}</ElDescriptionsItem
        >
        <ElDescriptionsItem v-if="actionPlan.minutes" label="禁言时长"
          >{{ actionPlan.minutes }} 分钟</ElDescriptionsItem
        >
        <ElDescriptionsItem v-if="actionPlan.text" label="内容"
          ><span class="action-content">{{ actionPlan.text }}</span></ElDescriptionsItem
        >
        <ElDescriptionsItem label="状态">{{ actionStatus }}</ElDescriptionsItem>
        <ElDescriptionsItem v-if="actionPlan.operation_id" label="操作编号">{{
          actionPlan.operation_id
        }}</ElDescriptionsItem>
        <ElDescriptionsItem label="有效期">{{
          new Date(actionPlan.expires_at).toLocaleString()
        }}</ElDescriptionsItem>
      </ElDescriptions>
      <ElAlert
        v-if="actionPlan.result_code"
        :title="messageCode(actionPlan.result_code)"
        type="warning"
        :closable="false"
      />
      <ElAlert v-if="planError" :title="planError" type="error" :closable="false" />
    </template>
    <template #footer>
      <ElButton
        :icon="Refresh"
        :disabled="approving || !actionPlan"
        @click="actionPlan && showActionPlan(actionPlan.id)"
        >核对状态</ElButton
      >
      <ElButton
        v-if="
          actionPlan &&
          !groupReadOnly &&
          ['pending_approval', 'blocked'].includes(actionPlan.status)
        "
        :disabled="approving"
        @click="cancelAction"
        >取消建议</ElButton
      >
      <ElButton
        v-if="
          actionPlan &&
          ['pending_approval', 'blocked'].includes(actionPlan.status) &&
          actionPlan.production_admitted &&
          canApproveAction
        "
        type="primary"
        :loading="approving"
        @click="approveAction"
        >{{ actionConfirmLabel }}</ElButton
      >
    </template>
  </ElDialog>
</template>
<script setup lang="ts">
  import { computed, nextTick, onUnmounted, ref, watch } from 'vue'
  import {
    ChatLineRound,
    Plus,
    Loading,
    Refresh,
    Promotion,
    CircleClose,
    Clock,
    Delete,
    Check,
    CopyDocument,
    ArrowDown,
    ArrowRight,
    Warning
  } from '@element-plus/icons-vue'
  import { Bubble } from 'vue-element-plus-x'
  import AdminMark from '../AdminMark.vue'
  import { api as request, ApiError, errorText, type Row } from '../api'
  import { useSession } from '../store'
  import { accountOptionLabel } from '../accountIdentity'
  import {
    cardSchema,
    object,
    outcomeSchema,
    stream as readStream,
    toolLabels,
    write as requestWrite,
    type ActionPlan,
    type Plan,
    type Scope,
    type Turn,
    type TranscriptTurn,
    type TranscriptOutcome
  } from './client'
  import ResultCard from './ResultCard.vue'
  import AssistantMarkdown from './AssistantMarkdown.vue'
  import AssistantHelp from './AssistantHelp.vue'
  import AssistantInput from './AssistantInput.vue'
  import AssistantShell from './AssistantShell.vue'
  import GroupPicker from './GroupPicker.vue'
  import ConfigDiff from './ConfigDiff.vue'
  import ConversationHistory from './ConversationHistory.vue'
  import {
    groupAccessLabel,
    groupAccessReadOnly,
    groupAccessReason,
    type GroupAccess
  } from './types'
  const props = defineProps<{
    accounts: Row[]
    availableGroups: Row[]
    embedded?: boolean
    initialGroup?: string
  }>()
  const emit = defineEmits<{ platform: [platform: 'wsl' | 'lajiaoliao'] }>()
  const statusLabels: Record<string, string> = {
    queued: '排队中',
    running: '生成中',
    succeeded: '本轮结束',
    failed: '失败',
    cancelled: '已取消',
    expired: '已过期',
    interrupted: '已中断'
  }
  const session = useSession()
  const platform = computed<'wsl' | 'lajiaoliao'>(() =>
    session.workspace === 'lajiaoliao' ? 'lajiaoliao' : 'wsl'
  )
  const platformLabel = computed(() => (platform.value === 'wsl' ? '旺商聊' : '辣椒聊'))
  const platformLogo = computed(() =>
    platform.value === 'wsl' ? '/wsl-logo.png' : '/lajiaoliao-logo.png'
  )
  const platforms = computed(() =>
    [
      { id: 'wsl', name: '旺商聊', logo: '/wsl-logo.png' },
      { id: 'lajiaoliao', name: '辣椒聊', logo: '/lajiaoliao-logo.png' }
    ].filter((item) => session.panels.some((panel) => panel.id === item.id))
  )
  const api = <T,>(path: string, method = 'GET', body?: unknown, signal?: AbortSignal) =>
    request<T>(path, method, body, signal, platform.value)
  const write = <T,>(path: string, body: unknown, key: string, signal?: AbortSignal) =>
    requestWrite<T>(path, body, key, signal, platform.value)
  const stream = (
    id: string,
    after: number,
    signal: AbortSignal,
    emit: (event: import('./client').StreamEvent) => void
  ) => readStream(id, after, signal, emit, platform.value)
  const canApprove = computed(
    () => !groupReadOnly.value && session.permissions.includes('assistant:approve')
  )
  const canPropose = computed(
    () => !groupReadOnly.value && session.permissions.includes('assistant:propose')
  )
  const groupPanelOpen = ref(false),
    helpOpen = ref(false)
  const canApproveAction = computed(
    () => !groupReadOnly.value && session.permissions.includes('assistant:action_approve')
  )
  const actionPlan = ref<ActionPlan>(),
    actionOpen = ref(false)
  const scopedGroups = ref<Row[]>([]),
    nextOffset = ref(-1),
    contextLoading = ref(false)
  const groupSearch = ref('')
  let contextSequence = 0,
    contextAbort: AbortController | undefined,
    searchTimer: ReturnType<typeof setTimeout> | undefined,
    claimSequence = 0,
    claimAbort: AbortController | undefined,
    claimTimer: ReturnType<typeof setTimeout> | undefined
  const messageCode = (code: string) =>
    ({
      production_acceptance_required: '此平台的生产准入尚未完成，当前仅可查看建议',
      durable_action_not_implemented: '此平台的持久动作尚未接通',
      permission_denied: '本人没有此动作权限'
    })[code] || errorText(code)
  const evidenceLabel = (state: string) =>
    ({
      receive_observed: '接收侧已观测 · 不代表已读',
      platform_receipt: '平台成功回执 · 送达待核对',
      awaiting_confirmation: '待本人确认',
      unknown: '结果待核对',
      failed: '失败',
      cancelled: '已取消',
      submitting: '提交中',
      invalidated: '建议已失效'
    })[state] || state
  const actionStatus = computed(
    () =>
      ({
        blocked: '尚未开放',
        pending_approval: '等待本人确认',
        approved: '已确认',
        submitting: '提交中',
        succeeded: '已完成',
        failed: '失败',
        unknown: '结果待核对',
        cancelled: '已取消',
        expired: '已过期',
        invalidated: '建议已失效'
      })[actionPlan.value?.status || ''] || ''
  )
  const actionConfirmLabel = computed(
    () =>
      ({
        mute_member: '确认禁言',
        unmute_member: '确认解禁',
        mute_group: '确认全群禁言',
        unmute_group: '确认解除禁言',
        kick_member: '确认移出成员',
        publish_notice: '确认发布公告',
        send_text: '确认发送',
        group_info: '确认修改群名称',
        member_nickname: '确认修改群名片',
        recall_message: '确认撤回'
      })[actionPlan.value?.action || ''] || '确认操作'
  )
  const open = ref(!!props.embedded),
    group = ref(''),
    assistantAccount = ref(session.selectedAccount),
    sessionID = ref(''),
    selectedTurn = ref('')
  const history = ref<Row[]>([]),
    turns = ref<Turn[]>([]),
    records = ref<TranscriptTurn[]>([]),
    usage = ref<Row>()
  const deleteOpen = ref(false),
    deleting = ref(false),
    deleteError = ref(''),
    deleteTarget = ref<Row>(),
    copiedTurn = ref(''),
    scrolledUp = ref(false)
  const loading = ref(false),
    historyOpen = ref(false),
    creating = ref(false),
    composer = ref<{ focus: () => void }>(),
    threadElement = ref<HTMLElement>()
  let followBottom = true
  const question = ref(''),
    answer = ref(''),
    progress = ref(''),
    input = ref(''),
    error = ref('')
  const cards = ref<Record<string, unknown>[]>([])
  const starters = computed(() => [
    '查看群配置',
    '查看本群可用的管理功能',
    ...(groupReadOnly.value ? [] : ['给我一条群公告建议'])
  ])
  const interactiveCard = (card: Record<string, unknown>) =>
    [
      'propose_group_config_change',
      'propose_group_action',
      'find_group_members',
      'find_group_messages'
    ].includes(String(card.type))
  const running = ref(false),
    sending = ref(false),
    cancelling = ref(false)
  const plan = ref<Plan>(),
    planOpen = ref(false),
    approving = ref(false),
    planError = ref('')
  let sequence = 0,
    streamAbort: AbortController | undefined,
    viewAbort: AbortController | undefined,
    transcriptAbort: AbortController | undefined,
    transcriptSequence = 0,
    pending: { input: string; key: string; session: string; target?: string } | undefined,
    selectedTarget: string | undefined
  const drafts = new Map<string, string>()
  const draftActor = session.user?.id
  const draftKey = computed(
    () =>
      `assistant-draft:${session.user?.id}:${session.user?.tenant_id || 'platform'}:${platform.value}:${assistantAccount.value || 'none'}:${group.value || 'none'}`
  )
  const scope = computed<Scope>(() =>
    group.value && assistantAccount.value
      ? {
          platform: platform.value,
          kind: 'group',
          account_id: assistantAccount.value,
          group_id: group.value
        }
      : assistantAccount.value
        ? { platform: platform.value, kind: 'account', account_id: assistantAccount.value }
        : { platform: platform.value, kind: 'overview' }
  )
  const key = computed(
    () =>
      `${platform.value}:assistant:${session.user?.id}:${session.user?.tenant_id || 'platform'}:${JSON.stringify(scope.value)}`
  )
  const groups = computed(() =>
    scopedGroups.value.filter(
      (g) =>
        g.active &&
        typeof g.primary_account_id === 'string' &&
        !props.accounts.some(
          (a) => a.id === g.primary_account_id && a.workspace && a.workspace !== platform.value
        )
    )
  )
  // Decorate selection labels without changing the canonical group names or scope.
  const pickerGroups = computed(() =>
    groups.value.map((item) => ({
      ...item,
      name: `${item.name} · ${groupAccessLabel(item as GroupAccess)}`
    }))
  )
  const groupAccess = computed<GroupAccess | undefined>(() =>
    groups.value.find((item) => item.group_id === group.value)
  )
  const groupReadOnly = computed(() => groupAccessReadOnly(groupAccess.value))
  const accessLabel = computed(() => groupAccessLabel(groupAccess.value))
  const accessReason = computed(() => errorText(groupAccessReason(groupAccess.value)))
  const accountName = computed(() =>
    assistantAccount.value
      ? groups.value.find((g) => g.group_id === group.value)?.robot_nickname ||
        accountOptionLabel(props.accounts.find((a) => a.id === assistantAccount.value)) ||
        '群机器人'
      : '工作台总览'
  )
  const groupName = computed(
    () => groups.value.find((item) => item.group_id === group.value)?.name || ''
  )
  const scopeValid = computed(
    () =>
      !!group.value &&
      groups.value.some(
        (item) =>
          item.group_id === group.value && item.primary_account_id === assistantAccount.value
      )
  )
  const inputBytes = computed(() => new TextEncoder().encode(input.value).length)
  const currentSession = computed(() => history.value.find((item) => item.id === sessionID.value))
  const conversationLabel = (item: Row) =>
    item.is_empty ? '新对话' : item.title || '历史对话（内容已失效）'
  const thread = computed(() =>
    records.value.map((item) => {
      const visible =
        item.id === selectedTurn.value && !item.unavailable
          ? {
              ...item,
              input: question.value || item.input,
              answer: answer.value,
              cards: cards.value
            }
          : item
      return { ...visible, outcomes: turnOutcomes(visible) }
    })
  )
  function turnOutcomes(row: TranscriptTurn): TranscriptOutcome[] {
    // An authoritative empty array also replaces provisional tool summaries.
    const items = Array.isArray(row.outcomes)
      ? row.outcomes.filter(outcomeSchema)
      : row.unavailable
        ? []
        : row.cards.flatMap((card) => {
            if (
              !['propose_group_config_change', 'propose_group_action'].includes(
                String(card.type)
              ) ||
              !object(card.data)
            )
              return []
            const item = {
              plan_id: card.data.id,
              kind: card.type === 'propose_group_action' ? 'action' : 'configuration',
              status: card.data.status,
              summary: card.data.summary,
              operation_id: card.data.operation_id
            }
            return outcomeSchema(item) ? [item] : []
          })
    return [...new Map(items.map((item) => [`${item.kind}:${item.plan_id}`, item])).values()]
  }
  function replyText(row: TranscriptTurn) {
    return [
      ...new Set([
        ...(row.unavailable ? [] : [row.answer.trim()]),
        ...turnOutcomes(row).map((item) => item.summary.trim())
      ])
    ]
      .filter(Boolean)
      .join('\n\n')
  }
  function trackScroll() {
    const el = threadElement.value
    if (el) {
      const remaining = el.scrollHeight - el.scrollTop - el.clientHeight
      followBottom = remaining < 70
      scrolledUp.value = remaining >= 140
    }
  }
  function scrollToBottom() {
    if (threadElement.value) threadElement.value.scrollTop = threadElement.value.scrollHeight
    scrolledUp.value = false
    followBottom = true
  }
  function useStarter(value: string) {
    input.value = value
    void nextTick(() => composer.value?.focus())
  }
  function updateGroupAccess(access: GroupAccess, selectedScope: Scope) {
    if (
      selectedScope.platform !== platform.value ||
      selectedScope.group_id !== group.value ||
      selectedScope.account_id !== assistantAccount.value
    )
      return
    scopedGroups.value = scopedGroups.value.map((item) =>
      item.group_id === group.value ? { ...item, ...access } : item
    )
  }
  watch(
    [
      open,
      key,
      () => session.user?.role,
      () => groupAccess.value?.access_mode,
      () => groupAccess.value?.ownership_state
    ],
    async () => {
      const epoch = ++claimSequence
      claimAbort?.abort()
      clearTimeout(claimTimer)
      if (
        !open.value ||
        !scopeValid.value ||
        session.user?.role !== 'superadmin' ||
        groupAccess.value?.access_mode !== 'manage' ||
        groupAccess.value.ownership_state === 'active'
      )
        return
      const selectedScope = { ...scope.value }
      const selectedKey = key.value
      const access = { ...groupAccess.value }
      const controller = new AbortController()
      claimAbort = controller
      const expire = () => {
        if (
          epoch !== claimSequence ||
          controller.signal.aborted ||
          !open.value ||
          selectedKey !== key.value
        )
          return
        updateGroupAccess(
          { ...access, access_mode: 'read_only', blocked_reason: 'operator_demo_claim_required' },
          selectedScope
        )
      }
      // Context has no claim deadline. Bound stale management even if its
      // database-only claim lookup fails; never renew the claim from this view.
      claimTimer = setTimeout(expire, 60_000)
      try {
        const claim = await api<{
          group_id: string
          platform: string
          state: string
          expires_at?: string
        }>(
          `/user-center/groups/${encodeURIComponent(selectedScope.group_id!)}/demo-claim`,
          'GET',
          undefined,
          controller.signal
        )
        if (
          epoch !== claimSequence ||
          controller.signal.aborted ||
          claim.group_id !== selectedScope.group_id ||
          claim.platform !== selectedScope.platform
        )
          return
        const remaining =
          claim.state === 'active' ? Date.parse(claim.expires_at || '') - Date.now() - 3000 : 0
        clearTimeout(claimTimer)
        claimTimer = setTimeout(
          expire,
          Number.isFinite(remaining) ? Math.min(600_000, Math.max(0, remaining)) : 60_000
        )
      } catch {
        // The bounded fallback still rechecks current scope access.
      }
    }
  )
  function displayCard(card: Record<string, unknown>) {
    if (!groupReadOnly.value || card.type !== 'get_group_capabilities' || !object(card.data))
      return card
    return {
      ...card,
      data: {
        ...card.data,
        actions: Array.isArray(card.data.actions)
          ? card.data.actions
              .filter(object)
              .map((action) => ({ ...action, production_admitted: false }))
          : []
      }
    }
  }
  async function copyAnswer(row: TranscriptTurn) {
    const text = replyText(row)
    if (!text) return
    const view = captureView()
    try {
      await navigator.clipboard.writeText(text)
      if (!isCurrentView(view)) return
      copiedTurn.value = row.id
      window.setTimeout(() => {
        if (copiedTurn.value === row.id) copiedTurn.value = ''
      }, 1600)
    } catch {
      if (isCurrentView(view)) error.value = '复制失败，请稍后重试'
    }
  }
  watch(
    () => [answer.value, cards.value.length, records.value],
    async () => {
      await nextTick()
      if (followBottom && threadElement.value)
        threadElement.value.scrollTop = threadElement.value.scrollHeight
    }
  )
  function selectGroup(id: string) {
    if (sending.value || running.value || deleting.value) return
    const target = groups.value.find((item) => item.group_id === id)
    if (!target) return
    if (!group.value && input.value) {
      const targetScope = {
        platform: platform.value,
        kind: 'group',
        account_id: target.primary_account_id,
        group_id: id
      }
      drafts.set(
        `${platform.value}:assistant:${session.user?.id}:${session.user?.tenant_id || 'platform'}:${JSON.stringify(targetScope)}`,
        input.value
      )
    }
    assistantAccount.value = target.primary_account_id
    group.value = id
    groupPanelOpen.value = false
    void loadContext()
  }
  function selectPlatform(value: string | number | boolean | undefined) {
    if (
      (value !== 'wsl' && value !== 'lajiaoliao') ||
      value === platform.value ||
      !platforms.value.some((item) => item.id === value) ||
      sending.value ||
      running.value ||
      deleting.value ||
      approving.value ||
      creating.value
    )
      return
    session.saveDraft(draftKey.value, { input: input.value })
    emit('platform', value)
  }
  function chooseConversation(item: Row) {
    if (
      sending.value ||
      running.value ||
      deleting.value ||
      !history.value.some((row) => row.id === item.id)
    )
      return
    historyOpen.value = false
    sessionID.value = item.id
    void loadTurns()
  }
  function requestDeletion(item: Row) {
    if (sending.value || running.value || loading.value || deleting.value) return
    deleteTarget.value = item
    deleteError.value = ''
    deleteOpen.value = true
  }
  async function deleteConversation() {
    const item = deleteTarget.value
    if (!item || deleting.value) return
    const capturedKey = key.value
    const epoch = sequence
    let finalEpoch = epoch
    deleting.value = true
    deleteError.value = ''
    try {
      await api(`/assistant/sessions/${item.id}`, 'DELETE')
      if (capturedKey !== key.value || epoch !== sequence) return
      history.value = history.value.filter((row) => row.id !== item.id)
      if (sessionID.value === item.id) {
        sequence++
        finalEpoch = sequence
        streamAbort?.abort()
        viewAbort?.abort()
        sessionID.value = ''
        records.value = []
        turns.value = []
        question.value = ''
        answer.value = ''
        cards.value = []
        error.value = ''
        selectedTurn.value = ''
        progress.value = ''
        pending = undefined
        selectedTarget = undefined
        planOpen.value = false
        actionOpen.value = false
        sessionStorage.removeItem(key.value)
      }
      deleteOpen.value = false
      deleteTarget.value = undefined
      historyOpen.value = false
    } catch (e) {
      if (capturedKey === key.value && epoch === sequence) deleteError.value = message(e)
    } finally {
      if (capturedKey === key.value && finalEpoch === sequence) deleting.value = false
    }
  }
  async function startNew() {
    if (
      sending.value ||
      running.value ||
      loading.value ||
      creating.value ||
      deleting.value ||
      !scopeValid.value
    )
      return
    const capturedKey = key.value
    const previousEmpty = currentSession.value?.is_empty ? sessionID.value : ''
    creating.value = true
    loading.value = true
    error.value = ''
    historyOpen.value = false
    planOpen.value = false
    try {
      await newSession()
      if (capturedKey === key.value && sessionID.value && !error.value) {
        if (sessionID.value !== previousEmpty) input.value = ''
        progress.value = ''
        followBottom = true
        await nextTick()
        composer.value?.focus()
      }
    } finally {
      if (capturedKey === key.value) {
        creating.value = false
        loading.value = false
      }
    }
  }
  function sendOnEnter(event: Event | KeyboardEvent) {
    if (!(event instanceof KeyboardEvent)) return
    if (event.key !== 'Enter' || event.shiftKey || event.isComposing) return
    event.preventDefault()
    if (
      !sending.value &&
      !running.value &&
      !deleting.value &&
      !loading.value &&
      scopeValid.value &&
      input.value.trim() &&
      inputBytes.value <= 8000
    )
      void submit()
  }
  const message = (e: unknown) => (e instanceof Error ? errorText(e.message) : '助手暂不可用')
  function showFailure(e: unknown, approval = false) {
    const scopeChanged =
      (e instanceof ApiError && [401, 403, 404, 410].includes(e.status)) ||
      (e instanceof Error && e.message.includes('权限已变化'))
    if (scopeChanged) {
      clear()
      drafts.delete(key.value)
      session.saveDraft(draftKey.value, {})
      input.value = ''
    }
    if (approval && !scopeChanged) planError.value = message(e)
    else error.value = message(e)
  }
  function captureView() {
    return {
      epoch: sequence,
      key: key.value,
      session: sessionID.value,
      identity: JSON.stringify([
        session.user?.id,
        session.user?.tenant_id,
        session.user?.role,
        session.permissions.slice().sort()
      ])
    }
  }
  function isCurrentView(view: ReturnType<typeof captureView>) {
    return (
      open.value &&
      view.epoch === sequence &&
      view.key === key.value &&
      view.session === sessionID.value &&
      view.identity === captureView().identity
    )
  }
  function replaceTranscript(items: TranscriptTurn[]) {
    records.value = items.slice(-10).map((item) => ({
      ...item,
      input: item.unavailable ? undefined : item.input,
      answer: item.unavailable || typeof item.answer !== 'string' ? '' : item.answer,
      cards: item.unavailable ? [] : (item.cards || []).filter(cardSchema),
      outcomes: Array.isArray(item.outcomes) ? item.outcomes.filter(outcomeSchema) : undefined
    }))
    turns.value = [...records.value].reverse()
    const current = records.value.find((item) => item.id === selectedTurn.value)
    if (selectedTurn.value) {
      question.value = current?.input || ''
      answer.value = current?.answer || ''
      cards.value = [...(current?.cards || [])]
      running.value =
        !!current && !current.unavailable && ['queued', 'running'].includes(current.status)
      progress.value = current?.status || ''
      if (!current) selectedTurn.value = ''
    }
    for (const row of records.value) {
      for (const card of row.cards) {
        if (!object(card.data)) continue
        if (card.type === 'propose_group_config_change' && card.data.id === plan.value?.id)
          plan.value = { ...plan.value, ...card.data } as Plan
        if (card.type === 'propose_group_action' && card.data.id === actionPlan.value?.id)
          actionPlan.value = { ...actionPlan.value, ...card.data } as ActionPlan
      }
      for (const outcome of row.outcomes || []) {
        if (outcome.kind === 'configuration' && outcome.plan_id === plan.value?.id)
          plan.value = { ...plan.value, status: outcome.status, summary: outcome.summary }
        if (outcome.kind === 'action' && outcome.plan_id === actionPlan.value?.id)
          actionPlan.value = {
            ...actionPlan.value,
            status: outcome.status,
            summary: outcome.summary,
            operation_id: outcome.operation_id || actionPlan.value.operation_id
          }
      }
    }
  }
  async function refreshTranscript(view = captureView()) {
    if (!view.session || !isCurrentView(view)) return
    const generation = ++transcriptSequence
    transcriptAbort?.abort()
    const controller = new AbortController()
    transcriptAbort = controller
    try {
      const items = await api<TranscriptTurn[]>(
        `/assistant/sessions/${view.session}/transcript`,
        'GET',
        undefined,
        controller.signal
      )
      if (!controller.signal.aborted && generation === transcriptSequence && isCurrentView(view)) {
        replaceTranscript(items)
        await loadContext()
      }
    } catch (e) {
      if (!controller.signal.aborted && generation === transcriptSequence && isCurrentView(view))
        showFailure(e)
    }
  }
  function clear() {
    sequence++
    streamAbort?.abort()
    viewAbort?.abort()
    transcriptAbort?.abort()
    transcriptSequence++
    question.value = ''
    answer.value = ''
    progress.value = ''
    cards.value = []
    running.value = false
    error.value = ''
    selectedTurn.value = ''
    turns.value = []
    records.value = []
    history.value = []
    usage.value = undefined
    sessionID.value = ''
    pending = undefined
    selectedTarget = undefined
    planOpen.value = false
    actionOpen.value = false
    actionPlan.value = undefined
    plan.value = undefined
    planError.value = ''
    approving.value = false
    cancelling.value = false
    loading.value = false
    creating.value = false
    historyOpen.value = false
    deleteOpen.value = false
    deleting.value = false
    deleteError.value = ''
    deleteTarget.value = undefined
    copiedTurn.value = ''
    scrolledUp.value = false
    followBottom = true
  }
  watch(
    [
      () => session.selectedAccount,
      () => session.workspace,
      () => session.user?.id,
      () => session.user?.tenant_id,
      () => session.user?.role,
      () => session.permissions.slice().sort().join('|')
    ],
    () => {
      if (!session.user) drafts.clear()
      clear()
      contextSequence++
      contextAbort?.abort()
      clearTimeout(searchTimer)
      groupSearch.value = ''
      groupPanelOpen.value = false
      assistantAccount.value = session.selectedAccount
      group.value = ''
      scopedGroups.value = []
      if (open.value) void loadContext()
    }
  )
  watch([key, draftKey], async ([value, draft], [old, oldDraft]) => {
    if (old && session.user?.id === draftActor) {
      drafts.set(old, input.value)
      session.saveDraft(oldDraft, { input: input.value })
    }
    clear()
    const saved = session.drafts[draft] as { input?: string } | undefined
    input.value = drafts.get(value) ?? (typeof saved?.input === 'string' ? saved.input : '')
    if (open.value && scopeValid.value) await load()
  })
  watch(scopeValid, (valid) => {
    if (!valid) {
      clear()
      error.value = '该群的授权或绑定已变化，请重新选择群组'
    }
  })
  watch(
    open,
    async (value) => {
      if (value) {
        await loadContext()
        if (scopeValid.value) await load()
      } else {
        sequence++
        streamAbort?.abort()
        viewAbort?.abort()
        transcriptAbort?.abort()
      }
    },
    { immediate: true }
  )
  async function loadContext(more = false) {
    const generation = ++contextSequence,
      selectedPlatform = platform.value,
      actor = session.user?.id,
      selectedGroup = group.value
    contextAbort?.abort()
    const controller = new AbortController()
    contextAbort = controller
    contextLoading.value = true
    try {
      const query = new URLSearchParams({
        offset: String(more ? nextOffset.value : 0),
        limit: '30',
        search: groupSearch.value
      })
      const requestedGroup = group.value || props.initialGroup || ''
      if (requestedGroup) query.set('group_id', requestedGroup)
      const context = await api<Row>(
        `/assistant/context?${query}`,
        'GET',
        undefined,
        controller.signal
      )
      if (
        controller.signal.aborted ||
        generation !== contextSequence ||
        selectedPlatform !== platform.value ||
        actor !== session.user?.id
      )
        return
      if (selectedGroup !== group.value) {
        await loadContext()
        return
      }
      const withAccess = (item: Row) => ({
        ...item,
        ...(context.scope?.platform === selectedPlatform &&
        context.scope?.group_id === item.group_id &&
        context.scope?.account_id === item.primary_account_id
          ? context.group_access
          : {}),
        active: true
      })
      const items = (context.groups || []).map(withAccess)
      if (!group.value && props.initialGroup && context.scope?.group_id === props.initialGroup) {
        const initial = props.availableGroups.find(
          (item) =>
            item.group_id === props.initialGroup &&
            item.platform === selectedPlatform &&
            item.primary_account_id === context.scope.account_id &&
            item.active
        )
        if (initial && !items.some((item: Row) => item.group_id === initial.group_id))
          items.push(withAccess(initial))
      }
      const selected = scopedGroups.value.find((g) => g.group_id === group.value)
      scopedGroups.value = [
        ...new Map<Row['group_id'], Row>([
          ...(more ? scopedGroups.value : []).map((g) => [g.group_id, g] as [string, Row]),
          ...items.map((g: Row) => [g.group_id, g] as [string, Row]),
          ...(selected &&
          context.scope?.group_id === selected.group_id &&
          !items.some((item: Row) => item.group_id === selected.group_id)
            ? [[selected.group_id, withAccess(selected)] as [string, Row]]
            : [])
        ]).values()
      ]
      nextOffset.value = context.next_offset
      if (!group.value && props.initialGroup && context.scope?.group_id === props.initialGroup)
        selectGroup(props.initialGroup)
    } catch (e) {
      if (generation === contextSequence && !controller.signal.aborted) {
        clear()
        scopedGroups.value = []
        error.value = message(e)
      }
    } finally {
      if (generation === contextSequence) contextLoading.value = false
    }
  }
  function searchGroups(value: string) {
    groupSearch.value = value
    clearTimeout(searchTimer)
    searchTimer = setTimeout(() => void loadContext(), 250)
  }
  async function load() {
    const epoch = ++sequence
    streamAbort?.abort()
    viewAbort?.abort()
    transcriptAbort?.abort()
    viewAbort = new AbortController()
    loading.value = true
    error.value = ''
    try {
      const query = new URLSearchParams(scope.value as unknown as Record<string, string>)
      const [items, meter] = await Promise.all([
        api<Row[]>(`/assistant/sessions?${query}`, 'GET', undefined, viewAbort.signal),
        api<Row>('/assistant/usage', 'GET', undefined, viewAbort.signal)
      ])
      if (epoch !== sequence) return
      history.value = items
      usage.value = meter
      const saved = sessionStorage.getItem(key.value)
      sessionID.value = items.find((item) => item.id === saved)?.id || items[0]?.id || ''
      if (sessionID.value) await loadTurns()
    } catch (e) {
      if (epoch === sequence) showFailure(e)
    } finally {
      if (epoch === sequence) loading.value = false
    }
  }
  async function newSession() {
    const epoch = ++sequence
    streamAbort?.abort()
    viewAbort?.abort()
    transcriptAbort?.abort()
    viewAbort = new AbortController()
    running.value = false
    try {
      const item = await write<Row>('/sessions', scope.value, crypto.randomUUID(), viewAbort.signal)
      if (epoch !== sequence) return
      sessionID.value = item.id
      history.value = [item, ...history.value.filter((row) => row.id !== item.id && !row.is_empty)]
      turns.value = []
      records.value = []
      selectedTurn.value = ''
      question.value = ''
      answer.value = ''
      cards.value = []
      pending = undefined
      selectedTarget = undefined
      sessionStorage.setItem(key.value, item.id)
    } catch (e) {
      if (epoch === sequence) showFailure(e)
    }
  }
  async function loadTurns() {
    selectedTarget = undefined
    const epoch = ++sequence
    streamAbort?.abort()
    viewAbort?.abort()
    transcriptAbort?.abort()
    viewAbort = new AbortController()
    loading.value = true
    question.value = ''
    answer.value = ''
    cards.value = []
    records.value = []
    selectedTurn.value = ''
    running.value = false
    try {
      sessionStorage.setItem(key.value, sessionID.value)
      const items = await api<TranscriptTurn[]>(
        `/assistant/sessions/${sessionID.value}/transcript`,
        'GET',
        undefined,
        viewAbort.signal
      )
      if (epoch !== sequence) return
      replaceTranscript(items)
      await loadContext()
      if (epoch !== sequence) return
      loading.value = false
      const latest = records.value.at(-1)
      if (latest && !latest.unavailable) await readTurn(latest.id)
    } catch (e) {
      if (epoch === sequence) showFailure(e)
    } finally {
      if (epoch === sequence) loading.value = false
    }
  }
  async function readTurn(id: string) {
    if (!id) return
    const epoch = ++sequence
    const view = captureView()
    streamAbort?.abort()
    streamAbort = new AbortController()
    const cached = records.value.find((item) => item.id === id)
    question.value = cached?.input || ''
    answer.value = cached?.answer || ''
    cards.value = [...(cached?.cards || [])]
    error.value = ''
    selectedTurn.value = id
    if (cached && !['queued', 'running'].includes(cached.status)) {
      running.value = false
      progress.value = cached.status
      if (cached.result_code) error.value = errorText(cached.result_code)
      return
    }
    try {
      const current = await api<Turn>(
        `/assistant/turns/${id}`,
        'GET',
        undefined,
        streamAbort.signal
      )
      if (epoch !== sequence) return
      question.value = current.input || ''
      turns.value = turns.value.map((item) => (item.id === current.id ? current : item))
      running.value = ['queued', 'running'].includes(current.status)
      progress.value = current.status
      let lastSeq = cached?.cursor || 0
      const signal = streamAbort.signal
      const receive = (event: import('./client').StreamEvent) => {
        if (epoch !== sequence || !object(event.data)) return
        lastSeq = event.seq
        const record = records.value.find((item) => item.id === id)
        if (record) record.cursor = event.seq
        if (event.kind === 'text_delta' && typeof event.data.text === 'string')
          answer.value += event.data.text
        if (event.kind === 'tool_started')
          progress.value = toolLabels[String(event.data.name)] || '查询中'
        if (event.kind === 'tool_result' && cardSchema(event.data)) {
          cards.value.push(event.data)
          const data = event.data.data
          if (
            object(data) &&
            data.group_id === scope.value.group_id &&
            data.account_id === scope.value.account_id &&
            object(data.group_access) &&
            data.group_access.access_mode === 'read_only'
          )
            updateGroupAccess(data.group_access as GroupAccess, { ...scope.value })
        }
        if (event.kind === 'turn_finished') {
          running.value = false
          progress.value = String(event.data.status || '')
          const row = turns.value.find((item) => item.id === id)
          if (row) row.status = progress.value
          if (record) record.status = progress.value
          if (event.data.code) error.value = errorText(String(event.data.code))
        }
      }
      for (let attempt = 0; attempt < 3; attempt++) {
        try {
          await stream(id, lastSeq, signal, receive)
          break
        } catch (e) {
          if (
            signal.aborted ||
            epoch !== sequence ||
            attempt === 2 ||
            (e instanceof Error && (e.message.includes('权限') || e.message.includes('序号')))
          )
            throw e
          // Only resubscribe to this existing turn; never recreate or charge a turn.
          await new Promise((resolve) => setTimeout(resolve, 500 * (attempt + 1)))
          const status = await api<Turn>(`/assistant/turns/${id}`, 'GET', undefined, signal)
          if (epoch !== sequence) return
          running.value = ['queued', 'running'].includes(status.status)
        }
      }
      if (epoch === sequence) {
        const latest = await api<Turn>(`/assistant/turns/${id}`, 'GET', undefined, signal)
        if (epoch !== sequence) return
        running.value = ['queued', 'running'].includes(latest.status)
        progress.value = latest.status
        const record = records.value.find((item) => item.id === id)
        if (record) Object.assign(record, latest, { answer: answer.value, cards: [...cards.value] })
        if (latest.result_code) error.value = errorText(latest.result_code)
        if (!running.value) await refreshTranscript(view)
      }
    } catch (e) {
      if (epoch === sequence && !(e instanceof DOMException && e.name === 'AbortError'))
        showFailure(e)
    }
  }
  async function submit() {
    if (
      sending.value ||
      running.value ||
      deleting.value ||
      loading.value ||
      !scopeValid.value ||
      !input.value.trim() ||
      inputBytes.value > 8000
    )
      return
    const capturedKey = key.value
    sending.value = true
    error.value = ''
    try {
      if (!sessionID.value) await newSession()
      if (!sessionID.value || capturedKey !== key.value) return
      if (
        !pending ||
        pending.input !== input.value ||
        pending.session !== sessionID.value ||
        pending.target !== selectedTarget
      )
        pending = {
          input: input.value,
          key: crypto.randomUUID(),
          session: sessionID.value,
          target: selectedTarget
        }
      const submittedInput = pending.input
      const result = await write<Turn>(
        `/sessions/${pending.session}/turns`,
        { input: pending.input, target_token: pending.target || '' },
        pending.key
      )
      if (capturedKey !== key.value) return
      const conversation = currentSession.value
      if (conversation?.is_empty) {
        const title = Array.from(submittedInput.trim().replace(/\s+/gu, ' '))
        conversation.title = title.slice(0, 32).join('') + (title.length > 32 ? '…' : '')
        conversation.is_empty = false
      }
      pending = undefined
      selectedTarget = undefined
      input.value = ''
      turns.value.unshift(result)
      records.value = [
        ...records.value,
        { ...result, input: submittedInput, answer: '', cards: [], cursor: 0 }
      ].slice(-10)
      followBottom = true
      void readTurn(result.id)
    } catch (e) {
      if (capturedKey === key.value) showFailure(e)
    } finally {
      sending.value = false
    }
  }
  async function cancelTurn() {
    if (cancelling.value || !selectedTurn.value) return
    const id = selectedTurn.value
    const view = captureView()
    cancelling.value = true
    try {
      await write(`/turns/${id}/cancel`, {}, crypto.randomUUID())
    } catch (e) {
      if (isCurrentView(view)) showFailure(e)
    } finally {
      if (isCurrentView(view)) {
        await refreshTranscript(view)
        if (isCurrentView(view)) cancelling.value = false
      }
    }
  }
  async function showPlan(id: string) {
    const view = captureView()
    planError.value = ''
    try {
      const value = await api<Plan>(`/assistant/plans/${id}`)
      if (
        isCurrentView(view) &&
        value.session_id === view.session &&
        value.group_id === group.value &&
        value.account_id === assistantAccount.value &&
        (!value.platform || value.platform === platform.value)
      ) {
        plan.value = value
        planOpen.value = true
        await refreshTranscript(view)
      }
    } catch (e) {
      if (isCurrentView(view)) showFailure(e)
    }
  }
  async function showActionPlan(id: string) {
    const view = captureView()
    planError.value = ''
    try {
      const value = await api<ActionPlan>(`/assistant/action-plans/${id}`)
      if (
        !isCurrentView(view) ||
        value.session_id !== view.session ||
        value.scope.group_id !== group.value ||
        value.scope.account_id !== assistantAccount.value ||
        value.scope.platform !== platform.value
      )
        return
      actionPlan.value = value
      actionOpen.value = true
      await refreshTranscript(view)
    } catch (e) {
      if (isCurrentView(view)) showFailure(e)
    }
  }
  async function chooseTarget(token: string) {
    if (!sessionID.value || running.value || sending.value || groupReadOnly.value) return
    const epoch = sequence
    try {
      const value = await write<Row>(
        `/sessions/${sessionID.value}/target`,
        { token },
        crypto.randomUUID()
      )
      if (epoch === sequence) {
        selectedTarget = token
        input.value =
          `${input.value.trim()} ${value.name}（${value.role === 'message' ? '消息' : '成员'} ${value.id}，已选择）`.trim()
      }
    } catch (e) {
      if (epoch === sequence) showFailure(e)
    }
  }
  async function approveAction() {
    if (!actionPlan.value || approving.value || !canApproveAction.value) return
    const view = captureView()
    const selected = actionPlan.value
    approving.value = true
    planError.value = ''
    try {
      const value = await write<ActionPlan>(
        `/action-plans/${selected.id}/approve`,
        { digest: selected.digest, version: selected.version },
        selected.id
      )
      if (isCurrentView(view)) actionPlan.value = value
    } catch (e) {
      if (isCurrentView(view)) showFailure(e, true)
    } finally {
      if (isCurrentView(view)) {
        await refreshTranscript(view)
        if (isCurrentView(view)) approving.value = false
      }
    }
  }
  async function cancelAction() {
    if (!actionPlan.value || approving.value || groupReadOnly.value) return
    const view = captureView()
    const selected = actionPlan.value
    approving.value = true
    planError.value = ''
    try {
      const value = await write<ActionPlan>(`/action-plans/${selected.id}/cancel`, {}, selected.id)
      if (isCurrentView(view)) actionPlan.value = value
    } catch (e) {
      if (isCurrentView(view)) showFailure(e, true)
    } finally {
      if (isCurrentView(view)) {
        await refreshTranscript(view)
        if (isCurrentView(view)) approving.value = false
      }
    }
  }
  async function approve() {
    if (!plan.value || approving.value || !canApprove.value) return
    const view = captureView()
    const selected = plan.value
    approving.value = true
    planError.value = ''
    try {
      const result = await write<Plan>(
        `/plans/${selected.id}/approve`,
        { digest: selected.digest, version: selected.version },
        selected.id
      )
      if (isCurrentView(view)) plan.value = result
    } catch (e) {
      if (isCurrentView(view)) showFailure(e, true)
    } finally {
      if (isCurrentView(view)) {
        await refreshTranscript(view)
        if (isCurrentView(view)) approving.value = false
      }
    }
  }
  async function cancelPlan() {
    if (!plan.value || approving.value || !canPropose.value) return
    const view = captureView()
    const selected = plan.value
    approving.value = true
    planError.value = ''
    try {
      const value = await write<Plan>(`/plans/${selected.id}/cancel`, {}, selected.id)
      if (isCurrentView(view)) plan.value = value
    } catch (e) {
      if (isCurrentView(view)) showFailure(e, true)
    } finally {
      if (isCurrentView(view)) {
        await refreshTranscript(view)
        if (isCurrentView(view)) approving.value = false
      }
    }
  }
  onUnmounted(() => {
    sequence++
    streamAbort?.abort()
    viewAbort?.abort()
    transcriptAbort?.abort()
    drafts.clear()
    contextSequence++
    contextAbort?.abort()
    clearTimeout(searchTimer)
    claimSequence++
    claimAbort?.abort()
    clearTimeout(claimTimer)
  })
</script>
<style scoped>
  button.assistant-entry {
    display: inline-flex;
    flex: 0 0 auto;
    align-items: center;
    justify-content: center;
    gap: 8px;
    width: 116px;
    height: 36px;
    padding: 0 12px;
    border: 1px solid var(--ui-line);
    border-radius: 6px;
    background: var(--ui-paper);
    color: var(--ui-ink);
    font: 600 13px/1 var(--el-font-family);
    letter-spacing: 0;
    white-space: nowrap;
    cursor: pointer;
    transition:
      border-color 120ms ease,
      background-color 120ms ease;
  }
  .assistant-entry .assistant-entry-mark {
    width: 22px;
    height: 22px;
    flex: 0 0 22px;
  }
  button.assistant-entry:hover:not(:disabled, [aria-disabled='true']) {
    border-color: var(--ui-ink);
    background: var(--ui-soft);
  }
  button.assistant-entry:active:not(:disabled, [aria-disabled='true']) {
    background: var(--ui-soft-deep);
  }
  button.assistant-entry:focus-visible {
    outline: 2px solid var(--ui-ink);
    outline-offset: 2px;
    box-shadow: var(--ui-focus);
  }
  button.assistant-entry:disabled,
  button.assistant-entry[aria-disabled='true'] {
    border-color: var(--ui-line);
    background: var(--ui-soft);
    color: var(--el-disabled-text-color);
    cursor: not-allowed;
    opacity: 0.6;
  }
  .assistant-mobile-groups {
    display: none;
  }
  .assistant-header-actions {
    display: inline-flex;
    align-items: center;
    flex: 0 0 auto;
    gap: 4px;
  }
  .assistant-header-actions :deep(.el-button) {
    width: 32px;
    height: 32px;
    padding: 0;
    margin-left: 0;
    border-radius: 6px;
  }
  .assistant-round-count {
    margin-right: 8px;
    color: var(--ui-muted);
    font-size: 12px;
    font-variant-numeric: tabular-nums;
    white-space: nowrap;
  }
  .assistant-scope-label {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: 8px;
    padding: 0 8px 10px;
    color: var(--ui-ink);
    font-size: 12px;
    font-weight: 600;
  }
  .assistant-scope-label small {
    color: var(--ui-muted);
    font-size: 11px;
    font-weight: 400;
  }
  .assistant-platforms {
    display: flex;
    flex-wrap: nowrap;
    width: 100%;
    margin-bottom: 10px;
  }
  .assistant-platforms :deep(.el-radio-button) {
    flex: 1;
    min-width: 0;
  }
  .assistant-platforms :deep(.el-radio-button__inner) {
    display: flex;
    align-items: center;
    justify-content: center;
    gap: 6px;
    width: 100%;
    min-height: 36px;
    padding: 8px 6px;
    font-size: 12px;
  }
  .assistant-platforms img {
    flex: 0 0 18px;
    width: 18px;
    height: 18px;
    object-fit: contain;
    border-radius: 3px;
  }
  .assistant-history-scope {
    margin: 0 0 16px;
    padding-bottom: 12px;
    border-bottom: 1px solid var(--ui-line);
    color: var(--ui-muted);
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .assistant-turn-actions {
    display: flex;
    align-items: center;
    gap: 4px;
    min-height: 28px;
  }
  .assistant-answer-label {
    margin-bottom: 6px;
    color: var(--ui-muted);
    font-size: 11px;
    font-weight: 600;
  }
  .assistant-answer-avatar {
    display: inline-flex;
    align-items: center;
    justify-content: center;
    width: 32px;
    height: 32px;
    flex: 0 0 32px;
  }
  .assistant-answer-avatar .assistant-answer-mark {
    width: 26px;
    height: 26px;
    flex: 0 0 26px;
  }
  .assistant-scroll-action {
    position: absolute;
    right: 28px;
    top: -22px;
    z-index: 2;
  }
  .assistant-footer-meta {
    display: flex;
    justify-content: space-between;
    flex-wrap: wrap;
    gap: 12px;
    width: min(100%, 800px);
    margin-inline: auto;
    padding: 3px 2px 0;
    color: var(--ui-muted);
    font-size: 11px;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
  .assistant-footer-meta .assistant-usage {
    padding: 0;
    font-size: inherit;
  }
  .assistant-starters {
    display: grid;
    gap: 8px;
    width: min(380px, 100%);
    margin-top: 14px;
  }
  .assistant-starters button {
    display: flex;
    align-items: center;
    gap: 8px;
    min-height: 42px;
    padding: 10px 12px;
    border: 1px solid var(--ui-line);
    border-radius: 6px;
    background: var(--ui-paper);
    color: var(--ui-text);
    font: inherit;
    font-size: 13px;
    line-height: 1.5;
    letter-spacing: 0;
    text-align: left;
    cursor: pointer;
    overflow-wrap: anywhere;
    transition:
      border-color 120ms ease,
      background-color 120ms ease;
  }
  .assistant-starters button:hover:not(:disabled) {
    border-color: var(--ui-ink);
    color: var(--ui-ink);
    background: var(--ui-soft);
  }
  .assistant-starters button:focus-visible {
    outline: 2px solid var(--ui-ink);
    outline-offset: 2px;
    box-shadow: var(--ui-focus);
  }
  .assistant-starters button:disabled {
    color: var(--el-disabled-text-color);
    background: var(--ui-soft);
    cursor: not-allowed;
  }
  .assistant-starters button .el-icon {
    flex: 0 0 16px;
    color: var(--ui-muted);
  }
  .assistant-starters button .el-icon:last-child {
    margin-left: auto;
  }
  .assistant-delete-title {
    margin: 0 0 4px;
    color: var(--ui-ink);
    font-size: 16px;
    font-weight: 600;
    overflow-wrap: anywhere;
  }
  .assistant-delete-scope,
  .assistant-delete-note {
    margin: 0 0 12px;
    color: var(--ui-muted);
    font-size: 13px;
  }
  .assistant-sidebar-history {
    margin-top: 20px;
    border-top: 1px solid var(--ui-line);
    padding-top: 16px;
  }
  .action-content {
    white-space: pre-wrap;
    overflow-wrap: anywhere;
  }
  .assistant-heading {
    display: flex;
    flex-direction: column;
    gap: 3px;
    min-width: 0;
    flex: 1;
    overflow-wrap: anywhere;
    text-align: left;
    letter-spacing: 0;
  }
  .assistant-heading strong {
    color: var(--ui-ink);
    font-size: 16px;
    font-weight: 600;
    line-height: 1.4;
  }
  .assistant-heading-title {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .assistant-heading-title strong {
    display: block;
    flex: 1;
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .assistant-ownership-status {
    flex: 0 0 auto;
    max-width: 100%;
    border: 0;
    padding: 2px 0;
    background: transparent;
    color: var(--ui-muted);
    font: inherit;
    font-size: 11px;
    line-height: 1.5;
    letter-spacing: 0;
    cursor: pointer;
  }
  .assistant-ownership-status:focus-visible {
    outline: 2px solid var(--ui-ink);
    outline-offset: 2px;
  }
  .assistant-readonly-proposal {
    color: var(--ui-muted);
    font-size: 12px;
    line-height: 1.7;
    overflow-wrap: anywhere;
  }
  .assistant-heading small {
    display: block;
    color: var(--ui-muted);
    font-size: 12px;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .assistant-scope {
    min-width: 0;
    flex-shrink: 0;
    width: 100%;
    letter-spacing: 0;
  }
  .assistant-mobile-groups :deep(> span) {
    gap: 8px;
    max-width: 100%;
  }
  .assistant-platform-icon {
    display: block;
    width: 20px;
    height: 20px;
    flex: 0 0 20px;
    object-fit: contain;
    border-radius: 4px;
  }
  .assistant-current-group {
    min-width: 0;
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .assistant-turn {
    min-width: 0;
    width: min(100%, 800px);
    margin-inline: auto;
    padding-bottom: 24px;
  }
  .assistant-turn:last-child {
    padding-bottom: 4px;
  }
  .assistant-thread :deep(.elx-bubble) {
    min-width: 0;
    margin: 12px 0;
    --elx-bubble-radius: 8px;
  }
  .assistant-thread :deep(.assistant-user-message) {
    --elx-bubble-bg: var(--ui-soft);
    color: var(--ui-ink);
  }
  .assistant-thread :deep(.elx-bubble__content) {
    min-width: 0;
    border-radius: 8px;
  }
  .assistant-turn-state {
    display: block;
    color: var(--ui-muted);
    font-size: 11px;
    padding-top: 6px;
  }
  .assistant-usage {
    font-size: 12px;
    color: var(--ui-muted);
    padding: 12px 0;
    overflow-wrap: anywhere;
  }
  .assistant-thread {
    flex: 1;
    overflow-y: auto;
    overflow-x: hidden;
    overscroll-behavior: contain;
    min-width: 0;
    min-height: 80px;
    padding: 16px 0;
    letter-spacing: 0;
  }
  .assistant-welcome {
    display: flex;
    flex-direction: column;
    align-items: center;
    justify-content: center;
    gap: 10px;
    min-height: 100%;
    height: auto;
    padding: 24px 8px;
    color: var(--ui-muted);
    text-align: center;
  }
  .assistant-welcome .assistant-welcome-mark {
    width: 44px;
    height: 44px;
    flex: 0 0 44px;
    margin-bottom: 8px;
  }
  .assistant-welcome strong {
    color: var(--ui-ink);
    font-size: 20px;
    font-weight: 600;
    line-height: 1.4;
    overflow-wrap: anywhere;
  }
  .assistant-welcome > span {
    max-width: 100%;
    font-size: 13px;
    line-height: 1.5;
    overflow-wrap: anywhere;
  }
  .assistant-conversation-title {
    font-size: 13px;
    color: var(--ui-muted);
    overflow-wrap: anywhere;
    padding: 0 0 8px;
  }
  .assistant-question {
    margin: 0;
    white-space: pre-wrap;
    overflow-wrap: anywhere;
    font-size: 14px;
    line-height: 1.7;
  }
  .assistant-answer {
    margin: 0;
    overflow-wrap: anywhere;
    color: var(--ui-text);
    font-size: 14px;
    line-height: 1.7;
  }
  .assistant-outcome-summary {
    white-space: pre-wrap;
  }
  .assistant-progress {
    display: flex;
    align-items: center;
    gap: 8px;
    color: var(--ui-muted);
    font-size: 13px;
  }
  .assistant-tool-process {
    min-width: 0;
    margin: 8px 0 16px;
    color: var(--ui-muted);
    font-size: 12px;
  }
  .assistant-tool-process > summary {
    cursor: pointer;
    padding: 8px 0;
    line-height: 1.6;
    overflow-wrap: anywhere;
  }
  .assistant-tool-process > summary > * {
    margin-right: 8px;
    vertical-align: middle;
  }
  .assistant-tool-process > summary:focus-visible {
    outline: 2px solid var(--el-color-primary);
    outline-offset: 3px;
    border-radius: 4px;
  }
  .assistant-tool-steps {
    list-style: none;
    margin: 0;
    padding: 4px 0 4px 12px;
    border-left: 1px solid var(--ui-line);
  }
  .assistant-tool-steps > li + li {
    margin-top: 12px;
  }
  .assistant-tool-step-label {
    font-weight: 600;
    color: var(--ui-text);
    overflow-wrap: anywhere;
  }
  .assistant-compose {
    display: flex;
    gap: 6px;
    align-items: center;
    justify-content: space-between;
    flex-wrap: wrap;
    margin-top: 4px;
  }
  .assistant-compose-actions {
    display: flex;
    align-items: center;
    flex-wrap: nowrap;
    gap: 4px;
    margin-left: auto;
  }
  .assistant-sender {
    border: 1px solid var(--ui-line);
    border-radius: 8px;
    width: min(100%, 800px);
    margin-inline: auto;
    padding: 8px 10px;
    background: var(--ui-paper);
    min-width: 0;
    letter-spacing: 0;
  }
  .assistant-sender:focus-within {
    border-color: var(--ui-ink);
    box-shadow: var(--ui-focus);
  }
  .assistant-compose :deep(.el-button + .el-button) {
    margin-left: 0;
  }
  .assistant-compose-actions :deep(.el-button) {
    height: 32px;
    min-height: 32px;
    min-width: 32px;
    padding: 6px 10px;
  }
  .assistant-compose-actions :deep(.el-button.is-text) {
    padding: 6px;
  }
  @media (min-width: 768px) and (max-width: 1100px) {
    .assistant-round-count {
      display: none;
    }
  }
  @media (max-width: 767px) {
    button.assistant-entry {
      width: 36px;
      padding: 0;
    }
    .assistant-entry-text {
      display: none;
    }
    .assistant-mobile-groups {
      display: inline-flex;
      max-width: 100%;
      min-width: 0;
      min-height: 36px;
      padding-inline: 0;
    }
    .assistant-platforms {
      width: 180px;
      max-width: 50%;
      flex: 0 0 auto;
      margin-bottom: 0;
    }
    .assistant-platforms :deep(.el-radio-button__inner) {
      min-height: 32px;
      padding: 6px 4px;
      gap: 4px;
    }
    .assistant-scope {
      display: flex;
      align-items: center;
      flex-wrap: wrap;
      gap: 4px 10px;
    }
    .assistant-group-panel {
      flex: 0 0 100%;
      min-width: 0;
    }
    .assistant-mobile-groups {
      flex: 1;
      justify-content: flex-start;
      margin-left: 0;
    }
    .assistant-mobile-groups :deep(> span) {
      min-width: 0;
      overflow: hidden;
      text-overflow: ellipsis;
      white-space: nowrap;
    }
    .assistant-group-panel.collapsed,
    .assistant-sidebar-history {
      display: none;
    }
    .assistant-heading strong {
      display: block;
      white-space: nowrap;
      overflow: hidden;
      text-overflow: ellipsis;
      font-size: 15px;
    }
    .assistant-heading-title {
      gap: 4px;
    }
    .assistant-ownership-status {
      font-size: 10px;
    }
    .assistant-heading small,
    .assistant-round-count,
    .assistant-scope-label {
      display: none;
    }
    .assistant-footer-meta {
      gap: 4px 12px;
      font-size: 10px;
      padding-top: 3px;
    }
    .assistant-scroll-action {
      right: 20px;
    }
    .assistant-thread {
      min-height: 0;
    }
    .assistant-welcome {
      padding: 20px 0;
    }
    .assistant-welcome strong {
      font-size: 18px;
    }
  }
  @media (prefers-reduced-motion: reduce) {
    button.assistant-entry,
    .assistant-starters button {
      transition: none;
    }
  }
</style>
