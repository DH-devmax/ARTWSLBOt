<template>
  <section class="message-composer" aria-label="消息发送">
    <div v-if="draft.operation" class="composer-state" role="status">
      <span>{{ operationLabel }}</span>
      <ElButton v-if="pending && !busy" text size="small" @click="inspect">核对状态</ElButton>
      <ElButton
        v-if="draft.operation.id && draft.operation.status === 'unknown' && !busy"
        text
        size="small"
        @click="reconcile"
        >查询回显</ElButton
      >
      <ElButton
        v-if="draft.operation.status === 'queued' && context?.enabled"
        text
        size="small"
        :loading="busy"
        @click="continueDispatch"
        >继续提交</ElButton
      >
      <ElButton
        v-if="independentProof && !busy"
        text
        size="small"
        @click="startIndependentProof"
        >使用新认领证明</ElButton
      >
    </div>
    <div v-for="item in retainedUnknowns" :key="item.id" class="composer-evidence" role="status">
      <p>旧发送结果仍未知，原记录保留且不会重发。操作编号：{{ item.id }}</p>
      <ElButton text size="small" :disabled="busy" @click="inspectRetained(item.id)"
        >核对原记录</ElButton
      >
    </div>
    <p v-if="proofRecoveryWindow" class="composer-note">
      本次仅提交独立授权的新挑战；请填写当前认领页面的证明原文，旧证明不能沿用。
    </p>
    <div
      v-if="evidenceDiagnostic"
      class="composer-evidence"
      role="status"
      aria-label="原操作回显诊断"
    >
      <p>{{ evidenceDiagnostic.message }}</p>
      <p>查询结果不能用于判定消息不存在或未发送；请核对原操作，勿重复发送。</p>
      <details
        v-if="evidenceDiagnostic.counts.length || evidenceDiagnostic.sampledAt"
        :key="evidenceRevision"
      >
        <summary>查看本次查询范围</summary>
        <dl>
          <div v-for="count in evidenceDiagnostic.counts" :key="count.key">
            <dt>{{ count.label }}</dt>
            <dd>{{ count.value }}</dd>
          </div>
          <div v-if="evidenceDiagnostic.sampledAt">
            <dt>采样时间（UTC）</dt>
            <dd>
              <time :datetime="evidenceDiagnostic.sampledAt">{{ evidenceDiagnostic.sampledAt }}</time>
            </dd>
          </div>
        </dl>
      </details>
    </div>
    <p v-if="visibleError" class="composer-error" role="alert">{{ visibleError }}</p>
    <template v-if="supportsDraft">
      <ElSelect
        v-if="scene === 'private' && context?.test_windows?.length"
        v-model="selectedWindow"
        size="small"
        aria-label="私信测试授权"
        class="composer-window"
        :disabled="pending || busy || mediaBlocking"
      >
        <ElOption
          v-for="item in context.test_windows"
          :key="item.id"
          :value="item.id"
          :label="`${item.group_name || '隔离测试群'} · 剩余 ${item.remaining} 次`"
        />
      </ElSelect>
      <MediaComposer
        v-if="!textWindow?.proof_kind"
        :key="scope"
        :account="account"
        :workspace="workspace"
        :conversation="conversation"
        :title="title"
        :context="selectedContext"
        :disabled="pending || busy || loading || confirming"
        @blocking="mediaBlocking = $event"
        @refresh="load"
      />
      <div v-if="workspace === 'lajiaoliao' && scene === 'group' && !textWindow?.proof_kind" class="composer-targets">
        <ElSegmented
          :model-value="draft.chiliMode || 'text'"
          :options="[
            { label: '文本', value: 'text' },
            { label: '引用', value: 'quote' },
            { label: '@成员', value: 'mention' }
          ]"
          :disabled="pending || busy"
          aria-label="消息类型"
          @update:model-value="draft.chiliMode = $event as 'text' | 'quote' | 'mention'"
        />
        <ElSelect
          v-if="draft.chiliMode === 'quote'"
          v-model="draft.chiliQuote"
          placeholder="选择引用消息"
          filterable
          :disabled="pending || busy"
          aria-label="引用消息"
        >
          <ElOption v-for="item in choices.quotes" :key="item.id" :value="item.id" :label="item.text" />
        </ElSelect>
        <ElSelect
          v-if="draft.chiliMode === 'mention'"
          v-model="draft.chiliMention"
          placeholder="选择成员"
          :disabled="pending || busy"
          aria-label="提及成员"
        >
          <ElOption
            v-for="item in choices.members"
            :key="item.id"
            :value="item.id"
            :label="`${item.name} · ${item.id}`"
          />
        </ElSelect>
      </div>
      <ElInput
        v-if="supportsText"
        v-model="draft.text"
        type="textarea"
        :rows="2"
        resize="none"
        placeholder="输入消息"
        aria-label="消息内容"
        @keydown="shortcut"
      />
      <div v-if="supportsText" class="composer-actions">
        <span :class="{ 'composer-error': textBytes > limit }"
          >{{ textBytes }} / {{ limit }} 字节</span
        >
        <ElTooltip :content="blocked || '仅向当前授权目标提交纯文本'">
          <span>
            <ElButton
              type="primary"
              :icon="Promotion"
              :loading="busy"
              :disabled="!canSend"
              aria-label="发送消息"
              @click="sendText"
              >发送</ElButton
            >
          </span>
        </ElTooltip>
      </div>
    </template>
    <div class="composer-note">
      <span>{{ blocked || '受控测试 · 提交前重新核验权限' }}</span>
      <ElButton
        text
        size="small"
        :icon="Refresh"
        :loading="loading"
        aria-label="刷新发送条件"
        @click="load"
      />
    </div>
  </section>
</template>

<script setup lang="ts">
  import { computed, onUnmounted, ref, watch } from 'vue'
  import { ElMessageBox } from 'element-plus'
  import { Promotion, Refresh } from '@element-plus/icons-vue'
  import { api, ApiError, errorText, sendingAdmissionErrorText, sendingResultErrorText } from './api'
  import { useSession } from './store'
  import MediaComposer from './MediaComposer.vue'
  import type { Workspace } from './workspaces'
  import { sendingVersions, textEvidenceDiagnostic } from './messageSending'
  import type {
    SendingDraft as Draft,
    TextOperation as Operation,
    SendingScope,
    SendingWindow
  } from './messageSending'

  interface SendingContext {
    enabled: boolean
    reason: string
    max_text_bytes: number
    scope?: SendingScope
    test_windows?: SendingWindow[]
    recent?: Operation[]
    media_enabled?: boolean
    media_kinds?: string[]
    media_limits?: Record<string, number>
    recent_uploads?: import('./MediaComposer.vue').UploadState[]
  }
  const props = defineProps<{
    account: string
    workspace: Workspace
    conversation: string
    scene: string
    title: string
    drafts: Map<string, Draft>
  }>()
  const session = useSession()
  const scope = computed(
    () =>
      `${session.user?.id}:${session.user?.tenant_id || 'platform'}:${props.workspace}:${props.account}:${props.conversation}`
  )
  const draft = computed(() => {
    if (!props.drafts.has(scope.value)) {
      if (props.drafts.size >= 32) props.drafts.delete(props.drafts.keys().next().value!)
      props.drafts.set(scope.value, { text: '', key: '', submittedText: '', operation: null })
    }
    return props.drafts.get(scope.value)!
  })
  const context = ref<SendingContext | null>(null)
  const choices = ref<{ members: { id: string; name: string }[]; quotes: { id: string; text: string }[] }>({
    members: [], quotes: []
  })
  const mediaBlocking = ref(false)
  const selectedWindow = ref('')
  const proofRecoveryWindow = ref('')
  const retainedProofID = ref('')
  const selectedContext = computed(
    () =>
      context.value && {
        ...context.value,
        test_windows:
          props.scene === 'private' || proofRecoveryWindow.value
            ? context.value.test_windows?.filter((item) => item.id === selectedWindow.value)
            : context.value.test_windows
      }
  )
  const loading = ref(false),
    busy = ref(false),
    confirming = ref(false),
    error = ref('')
  const admissionError = ref('')
  const visibleError = computed(() => error.value
    ? sendingResultErrorText(error.value, admissionError.value)
    : admissionError.value)
  const evidenceDiagnostic = ref<ReturnType<typeof textEvidenceDiagnostic>>(null)
  const evidenceRevision = ref(0)
  let generation = 0
  let scopeRevision = 0
  let controller = new AbortController()
  const base = computed(() => `/accounts/${props.account}/conversations/${props.conversation}`)
  const recoveryBase = computed(() => `/accounts/${props.account}/native-text-operations`)
  const supportsDraft = computed(
    () =>
      ['wsl', 'lajiaoliao'].includes(props.workspace) && ['group', 'private'].includes(props.scene)
  )
  const supportsText = computed(() => supportsDraft.value)
  const textBytes = computed(() => new TextEncoder().encode(draft.value.text).length)
  const limit = computed(() => context.value?.max_text_bytes || 3000)
  const textWindow = computed(() =>
    selectedContext.value?.test_windows?.find(
      (item) =>
        item.remaining > 0 &&
        (!item.actions ||
          item.actions.includes(props.scene === 'private' ? 'send_private_text' : 'send_text'))
    )
  )
  const pending = computed(
    () =>
      !!draft.value.operation &&
      ['preparing', 'queued', 'running', 'unknown', 'request_unknown'].includes(
        draft.value.operation.status
      )
  )
  const independentProof = computed(() => {
    const previous = draft.value.operation
    if (props.workspace !== 'lajiaoliao' || !context.value?.enabled ||
      previous?.status !== 'unknown' || !previous.id || loading.value || mediaBlocking.value) return
    const oldWindow = previous.test_window_id ||
      context.value.recent?.find(item => item.id === previous.id)?.test_window_id
    if (!oldWindow) return
    return context.value.test_windows?.find(item => item.remaining === 1 &&
      item.proof_kind === props.scene && item.id !== oldWindow)
  })
  const retainedUnknowns = computed(() => {
    if (!retainedProofID.value) return []
    const recent = context.value?.recent || []
    const retained = recent.filter(item => item.status === 'unknown' &&
      item.id !== draft.value.operation?.id)
    if (retainedProofID.value !== draft.value.operation?.id &&
      !recent.some(item => item.id === retainedProofID.value)) {
      retained.unshift({ id: retainedProofID.value, status: 'unknown', result_code: '' })
    }
    return retained
  })
  const blocked = computed(() => {
    if (!supportsDraft.value) return errorText('native_text_platform_unavailable')
    if (loading.value) return '正在核对发送条件'
    if (!context.value) return '未取得发送条件，不会提交消息'
    if (context.value.reason) return sendingAdmissionErrorText(context.value.reason)
    return context.value.enabled ? '' : sendingAdmissionErrorText(null)
  })
  const canSend = computed(
    () =>
      supportsText.value &&
      !!context.value?.enabled &&
      !!textWindow.value &&
      !!draft.value.text.trim() &&
      textBytes.value <= limit.value &&
      !pending.value &&
      !busy.value &&
      !confirming.value &&
      !loading.value &&
      !mediaBlocking.value &&
      (props.workspace !== 'lajiaoliao' || props.scene !== 'group' ||
        (draft.value.chiliMode !== 'quote' || choices.value.quotes.some(item => item.id === draft.value.chiliQuote)) &&
        (draft.value.chiliMode !== 'mention' || choices.value.members.some(item => item.id === draft.value.chiliMention)))
  )
  const operationLabel = computed(
    () =>
      ({
        preparing: '正在保存发送意图',
        queued: '意图已保存，尚未提交原生消息',
        running: '已进入提交阶段，勿重复发送',
        unknown: '提交结果未知，请核对状态或查询回显，勿重复发送',
        request_unknown: '请求结果待核对，不会自动重试',
        succeeded: '原生服务端已接受，不代表成员已送达或已读',
        failed: '发送失败，草稿保留',
        not_submitted: '未找到已保存意图，尚未提交原生消息',
        cancelled: '发送已取消',
        expired: '发送意图已过期'
      })[draft.value.operation?.status || ''] || '发送状态待核对'
  )

  async function load() {
    const turn = ++generation,
      captured = scopeRevision,
      capturedScope = scope.value,
      workspace = props.workspace,
      scene = props.scene,
      conversation = props.conversation,
      path = base.value,
      recoveryPath = recoveryBase.value,
      item = draft.value
    evidenceRevision.value++
    evidenceDiagnostic.value = null
    controller.abort()
    const request = new AbortController()
    controller = request
    const current = () => turn === generation && captured === scopeRevision &&
      capturedScope === scope.value && controller === request && !request.signal.aborted
    context.value = null
    choices.value = { members: [], quotes: [] }
    loading.value = true
    error.value = ''
    admissionError.value = ''
    if (!conversation) {
      loading.value = false
      return
    }
    try {
      const out = await api<SendingContext>(
        `${path}/sending`,
        'GET',
        undefined,
        request.signal,
        workspace
      )
      if (!current()) return
      context.value = out
      if (!out.test_windows?.some(item => item.id === proofRecoveryWindow.value &&
        item.proof_kind === scene && item.remaining === 1)) proofRecoveryWindow.value = ''
      if (!out.test_windows?.some((item) => item.id === selectedWindow.value)) {
        selectedWindow.value = out.test_windows?.[0]?.id || ''
      }
      if (!item.operation && !proofRecoveryWindow.value) {
        item.operation =
          out.recent?.find(
            (item) =>
              item.kind === 'send_text' && ['queued', 'running', 'unknown'].includes(item.status)
          ) || null
      }
      if (workspace === 'lajiaoliao' && scene === 'group' && textWindow.value) {
        const targets = await api<typeof choices.value>(
          `${path}/sending-targets?window=${encodeURIComponent(textWindow.value.id)}`,
          'GET', undefined, request.signal, workspace
        )
        if (!current()) return
        choices.value = targets
      }
    } catch (e) {
      if (!current()) return
      context.value = null
      admissionError.value = sendingAdmissionErrorText(e)
      if (scene === 'private' && !item.operation) {
        try {
          const recovery = await api<{ items: Operation[] }>(
            `${recoveryPath}?conversation=${encodeURIComponent(conversation)}`,
            'GET', undefined, request.signal, workspace
          )
          if (!current()) return
          if (!item.operation) item.operation = recovery.items?.[0] || null
        } catch {
          // Keep the original admission error; a failed lookup grants no retry.
        }
      }
    } finally {
      if (current()) loading.value = false
    }
  }

  function startIndependentProof() {
    const window = independentProof.value
    if (!window || busy.value || confirming.value) return
    retainedProofID.value = draft.value.operation!.id
    selectedWindow.value = window.id
    proofRecoveryWindow.value = window.id
    Object.assign(draft.value, {
      text: '', key: '', submittedText: '', operation: null,
      chiliMode: 'text', chiliQuote: '', chiliMention: ''
    })
    error.value = ''
    evidenceDiagnostic.value = null
  }

  async function inspectRetained(id: string) {
    if (busy.value || !retainedUnknowns.value.some(item => item.id === id)) return
    const captured = scopeRevision
    busy.value = true
    error.value = ''
    try {
      const operation = await api<Operation>(`${recoveryBase.value}/${id}`, 'GET',
        undefined, undefined, props.workspace)
      if (captured !== scopeRevision) return
      const previous = context.value?.recent?.find(item => item.id === id)
      if (previous && operation.id === id) Object.assign(previous, operation)
    } catch (e) {
      if (captured === scopeRevision) error.value = (e as Error).message
    } finally {
      if (captured === scopeRevision) busy.value = false
    }
  }

  async function confirmTarget() {
    if (confirming.value) return false
    confirming.value = true
    try {
      await ElMessageBox.confirm(
        `向“${props.title}”${draft.value.chiliMode === 'quote' ? '发送引用回复' : draft.value.chiliMode === 'mention' ? '@选定成员并发送消息' : '发送文本消息'}？`,
        '确认发送',
        { type: 'warning', confirmButtonText: '确认发送', cancelButtonText: '取消' }
      )
      return true
    } catch {
      return false
    } finally {
      confirming.value = false
    }
  }

  async function sendText() {
    if (!canSend.value) return
    const captured = scopeRevision
    if (!(await confirmTarget()) || captured !== scopeRevision || !canSend.value) return
    const item = draft.value,
      text = item.text,
      workspace = props.workspace,
      path = base.value
    const snapshot = context.value!
    item.key = crypto.randomUUID()
    item.submittedText = text
    item.operation = { id: '', status: 'preparing', result_code: '' }
    busy.value = true
    error.value = ''
    try {
      item.operation = await api<Operation>(
        `${path}/text-operations`,
        'POST',
        {
          text,
          ...(workspace === 'lajiaoliao' && props.scene === 'group' ? {
            reply_to: item.chiliMode === 'quote' ? item.chiliQuote : '',
            chili_mention_id: item.chiliMode === 'mention' ? item.chiliMention : ''
          } : {}),
          test_window_id: textWindow.value!.id,
          ...sendingVersions(snapshot.scope, textWindow.value)
        },
        undefined,
        workspace,
        item.key
      )
      if (captured !== scopeRevision) return
      await dispatch(item, path, workspace, captured)
    } catch (e) {
      if (!(e instanceof ApiError) || e.status >= 500 || e.status === 408) {
        item.operation = {
          id: item.operation?.id || '',
          status: 'request_unknown',
          result_code: ''
        }
      } else if (!item.operation?.id) {
        item.operation = { id: '', status: 'failed', result_code: e.code }
      }
      if (captured === scopeRevision) error.value = (e as Error).message
    } finally {
      if (captured === scopeRevision) busy.value = false
    }
  }

  async function dispatch(item: Draft, path: string, workspace: Workspace, captured: number) {
    if (item.operation?.status !== 'queued') return
    const id = item.operation.id
    item.operation = { ...item.operation, status: 'running' }
    try {
      item.operation = await api<Operation>(
        `${path}/text-operations/${id}/dispatch`,
        'POST',
        {},
        undefined,
        workspace
      )
    } catch (e) {
      item.operation = { id, status: 'request_unknown', result_code: '' }
      throw e
    }
    if (item.operation.status === 'succeeded' && item.text === item.submittedText) item.text = ''
    if (captured === scopeRevision) await load()
  }

  async function continueDispatch() {
    if (busy.value || draft.value.operation?.status !== 'queued' || !context.value?.enabled) return
    const captured = scopeRevision
    if (!(await confirmTarget()) || captured !== scopeRevision || !context.value?.enabled) return
    busy.value = true
    error.value = ''
    try {
      await dispatch(draft.value, base.value, props.workspace, captured)
    } catch (e) {
      if (captured === scopeRevision) error.value = (e as Error).message
    } finally {
      if (captured === scopeRevision) busy.value = false
    }
  }

  async function inspect() {
    if (busy.value) return
    const captured = scopeRevision,
      item = draft.value,
      workspace = props.workspace,
      path = recoveryBase.value
    const id = item.operation?.id
    if (!id && !item.key) return
    evidenceRevision.value++
    evidenceDiagnostic.value = null
    busy.value = true
    error.value = ''
    try {
      item.operation = await api<Operation>(
        `${path}${id ? `/${id}` : `?key=${encodeURIComponent(item.key)}`}`,
        'GET',
        undefined,
        undefined,
        workspace
      )
      if (item.operation.status === 'succeeded' && item.text === item.submittedText) item.text = ''
    } catch (e) {
      if (!id && e instanceof ApiError && e.code === 'operation_not_found') {
        item.operation = { id: '', status: 'not_submitted', result_code: '' }
      } else if (captured === scopeRevision) error.value = (e as Error).message
    } finally {
      if (captured === scopeRevision) busy.value = false
    }
  }

  async function reconcile() {
    if (busy.value || draft.value.operation?.status !== 'unknown') return
    const captured = scopeRevision,
      capturedScope = scope.value,
      item = draft.value,
      path = `${recoveryBase.value}/${item.operation!.id}/reconcile`,
      workspace = props.workspace,
      revision = ++evidenceRevision.value
    evidenceDiagnostic.value = null
    busy.value = true
    error.value = ''
    try {
      const { evidence_check, ...operation } = await api<Operation>(
        path,
        'POST',
        {},
        undefined,
        workspace
      )
      if (captured !== scopeRevision || capturedScope !== scope.value || revision !== evidenceRevision.value) return
      // This one-shot diagnostic is not part of the retained sending draft.
      item.operation = operation
      evidenceDiagnostic.value = textEvidenceDiagnostic(evidence_check)
      if (item.operation.status === 'succeeded' && item.text === item.submittedText) item.text = ''
    } catch (e) {
      if (captured === scopeRevision && capturedScope === scope.value && revision === evidenceRevision.value)
        error.value = (e as Error).message
    } finally {
      if (captured === scopeRevision) busy.value = false
    }
  }

  function shortcut(event: Event | KeyboardEvent) {
    if (
      event instanceof KeyboardEvent &&
      event.key === 'Enter' &&
      (event.ctrlKey || event.metaKey) &&
      !event.isComposing
    ) {
      event.preventDefault()
      void sendText()
    }
  }
  watch(
    scope,
    () => {
      scopeRevision++
      mediaBlocking.value = false
      selectedWindow.value = ''
      proofRecoveryWindow.value = ''
      retainedProofID.value = ''
      busy.value = false
      void load()
    },
    { immediate: true }
  )
  onUnmounted(() => {
    scopeRevision++
    generation++
    controller.abort()
  })
</script>

<style scoped>
  .message-composer {
    flex-shrink: 0;
    min-width: 0;
    padding: 12px 18px 8px;
    background: var(--el-bg-color);
    border-top: 1px solid var(--el-border-color-light);
  }
  .message-composer :deep(.el-textarea__inner) {
    box-shadow: none;
    padding: 8px 0;
    background: transparent;
    font-size: 14px;
  }
  .composer-window {
    width: 100%;
    max-width: 320px;
    margin-bottom: 6px;
  }
  .composer-targets {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
    margin: 8px 0;
  }
  .composer-targets :deep(.el-select) {
    flex: 1 1 180px;
    min-width: 0;
    max-width: 420px;
  }
  .composer-actions,
  .composer-note,
  .composer-state {
    display: flex;
    align-items: center;
    gap: 8px;
    min-width: 0;
  }
  .composer-actions {
    justify-content: space-between;
    font-size: 11px;
    color: var(--el-text-color-placeholder);
  }
  .composer-actions :deep(.el-button) {
    min-width: 70px;
  }
  .composer-note {
    justify-content: space-between;
    gap: 4px;
    margin-top: 5px;
    color: var(--el-text-color-secondary);
    font-size: 11px;
    line-height: 1.6;
  }
  .composer-note > span {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .composer-note :deep(.el-button) {
    flex-shrink: 0;
    margin: 0;
    padding: 4px;
  }
  .composer-state {
    flex-wrap: wrap;
    font-size: 12px;
    line-height: 1.6;
    color: var(--el-text-color-secondary);
  }
  .composer-state :deep(.el-button) {
    margin: 0;
    padding: 4px;
  }
  .composer-evidence {
    margin: 6px 0;
    padding: 8px 10px;
    border-left: 2px solid var(--el-color-warning);
    background: var(--el-fill-color-light);
    color: var(--el-text-color-secondary);
    font-size: 12px;
    line-height: 1.6;
    overflow-wrap: anywhere;
  }
  .composer-evidence p {
    margin: 0;
  }
  .composer-evidence summary {
    margin-top: 4px;
    cursor: pointer;
    color: var(--el-text-color-primary);
  }
  .composer-evidence dl {
    margin: 4px 0 0;
  }
  .composer-evidence dl > div {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    column-gap: 12px;
  }
  .composer-evidence dd {
    margin: 0;
    min-width: 0;
  }
  .composer-error {
    margin: 4px 0;
    color: var(--el-color-danger);
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  @media (max-width: 760px) {
    .message-composer {
      padding: 8px 12px calc(6px + env(safe-area-inset-bottom));
      border: 0;
      background: #f7f7f7;
    }
    .message-composer :deep(.el-textarea__inner) {
      padding: 8px 10px;
      border-radius: 4px;
      background: #fff;
      font-size: 15px;
    }
    .composer-actions {
      margin-top: 6px;
    }
    .composer-actions :deep(.el-button--primary) {
      background: #079b55;
      border-color: #079b55;
    }
    .composer-actions :deep(.el-button.is-disabled) {
      background: #e5e5e5;
      border-color: #e5e5e5;
      color: #999;
    }
    .composer-note {
      font-size: 10px;
    }
  }
</style>
