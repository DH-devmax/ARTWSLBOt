<template>
  <ElDialog
    :model-value="open && allowed"
    :title="gateTitle"
    width="620px"
    class="platform-config-dialog onboarding-friend-gate"
    align-center
    append-to-body
    :close-on-click-modal="false"
    @update:model-value="!$event && close()"
  >
    <div class="friend-gate-identity">
      <img v-if="platform" :src="workspaceLogos[platform]" :alt="platformName" />
      <div>
        <strong>{{ accountOptionLabel(account) }}</strong>
        <span>{{ platformName }} · 管理机器人</span>
      </div>
    </div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElSkeleton v-if="!saved && loading" :rows="4" animated />
    <section v-if="saved" class="friend-gate-saved" aria-label="已保存准入">
      <div class="friend-gate-record">
        <strong>已保存状态</strong>
        <ElTag :type="saved.state === 'open' ? 'success' : 'info'">{{
          saved.state === 'open' ? '开放' : '关闭'
        }}</ElTag>
        <span>版本 {{ saved.version }}</span>
      </div>
      <ul v-if="saved.evidence_refs.length" aria-label="已保存证据引用">
        <li v-for="item in saved.evidence_refs" :key="item">{{ item }}</li>
      </ul>
      <p v-else class="friend-gate-note">尚无证据引用</p>
    </section>
    <div class="friend-gate-explanation">
      <template v-if="outbound">
        <p>开放后，当前账号可在有效受控授权内，经人工确认发送单次好友申请。</p>
        <p>保存准入不会创建授权、搜索账号或发送申请。</p>
      </template>
      <template v-else>
        <p
          >用户先在官方 App
          添加机器人，再主动核对本次接入时间范围内、昵称完全一致的一条待处理请求。</p
        >
        <p>开放后，仅允许用户人工确认并通过本次接入专属安全验证的单次来向好友审批。</p>
        <p>昵称和申请时间仅用于选择联系请求，不能作为本人身份核验的证据。</p>
      </template>
      <p>好友关系不能证明本人身份或群归属，仍需独立私聊和群内核验。</p>
      <p class="friend-gate-note">保存准入不代表原生验收通过，也不改变就绪状态。</p>
    </div>
    <ElAlert
      v-if="conflict"
      title="准入记录已变化，草稿已保留。请重新读取并核对当前版本。"
      type="warning"
      :closable="false"
    />
    <ElButton
      v-if="conflict"
      :disabled="!fresh || loading || saving"
      :icon="Check"
      @click="useCurrentVersion"
      >使用当前版本保留草稿</ElButton
    >
    <ElForm
      v-if="editing && draft && saved"
      class="friend-gate-editor"
      label-position="top"
      @submit.prevent="save"
    >
      <ElFormItem label="准入状态">
        <ElSelect
          v-model="draft.state"
          :aria-label="`${gateTitle}状态`"
          :disabled="loading || saving"
        >
          <ElOption value="closed" label="关闭" />
          <ElOption value="open" label="开放" />
        </ElSelect>
      </ElFormItem>
      <ElFormItem label="证据引用" :error="evidenceError">
        <ElInput
          v-model="draft.evidenceText"
          type="textarea"
          :rows="3"
          :maxlength="4112"
          :disabled="loading || saving"
          :aria-label="outbound ? '发送好友申请证据引用' : '来向好友审批证据引用'"
          placeholder="每行一个 docs/ 文档路径或 acceptance:验收编号"
        />
      </ElFormItem>
      <p class="friend-gate-note"
        >最多 16 条，每条 256 字节；支持 docs/ 文档路径（可带 #锚点）或
        acceptance:验收编号，开放时必填。</p
      >
    </ElForm>
    <p v-else-if="draft" class="friend-gate-note">有未保存草稿</p>
    <p v-if="savedMessage" role="status">{{ savedMessage }}</p>
    <template #footer>
      <div class="friend-gate-footer">
        <ElTooltip :content="`重新读取${gateTitle}`">
          <ElButton
            :icon="Refresh"
            :aria-label="`重新读取${gateTitle}`"
            :loading="loading"
            :disabled="saving"
            @click="load"
          />
        </ElTooltip>
        <ElButton @click="close">关闭</ElButton>
        <ElButton
          v-if="!editing"
          :icon="Edit"
          :disabled="!saved || !fresh || loading || saving"
          @click="edit"
          >编辑准入</ElButton
        >
        <ElButton
          v-else
          type="primary"
          :icon="Check"
          :loading="saving"
          :disabled="!canSave"
          @click="save"
          >保存准入</ElButton
        >
      </div>
    </template>
  </ElDialog>
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import {
    ElAlert,
    ElButton,
    ElDialog,
    ElForm,
    ElFormItem,
    ElInput,
    ElOption,
    ElSelect,
    ElSkeleton,
    ElTag,
    ElTooltip
  } from 'element-plus'
  import { Check, Edit, Refresh } from '@element-plus/icons-vue'
  import { api, ApiError, type Row, type Workspace } from './api'
  import { accountOptionLabel } from './accountIdentity'
  import { useSession } from './store'
  import { workspaceLogos } from './workspaces'

  type Platform = 'wsl' | 'lajiaoliao'
  type State = 'closed' | 'open'
  interface Gate {
    platform: Platform
    account_id: string
    state: State
    version: number
    evidence_refs: string[]
  }
  interface Draft {
    state: State
    version: number
    evidenceText: string
    conflict: boolean
  }
  const props = withDefaults(
    defineProps<{
      open: boolean
      account: Row
      workspace: Workspace
      owner: string
      outbound?: boolean
    }>(),
    { outbound: false }
  )
  const emit = defineEmits<{ close: [] }>()
  const session = useSession()
  const gateTitle = computed(() => (props.outbound ? '发送好友申请准入' : '来向好友审批准入'))
  const gatePath = computed(
    () => `/system/group-binding/${props.outbound ? 'outbound-friend-gates' : 'friend-gates'}`
  )
  const platform = computed<Platform | null>(() =>
    (props.workspace === 'wsl' || props.workspace === 'lajiaoliao') &&
    props.account.platform === props.workspace
      ? props.workspace
      : null
  )
  const platformName = computed(() => (platform.value === 'lajiaoliao' ? '辣椒聊' : '旺商聊'))
  const allowed = computed(
    () =>
      !!platform.value &&
      !!props.account.id &&
      props.account.purpose === 'management' &&
      session.user?.id === props.owner &&
      session.user?.role === 'superadmin' &&
      session.console === 'admin' &&
      session.workspace === props.workspace
  )
  const scope = computed(
    () =>
      `${props.owner}:${platform.value}:${props.account.id}:${props.outbound ? 'outbound' : 'incoming'}-friend-gate`
  )
  const saved = ref<Gate | null>(null),
    draft = ref<Draft | null>(null)
  const loading = ref(false),
    saving = ref(false),
    editing = ref(false),
    fresh = ref(false),
    error = ref(''),
    savedMessage = ref('')
  let alive = true,
    generation = 0,
    activeScope = ''
  let controller: AbortController | undefined
  const refs = computed(() =>
    (draft.value?.evidenceText || '')
      .split(/\r?\n/)
      .map((line) => line.trim())
      .filter(Boolean)
  )
  const evidenceError = computed(() => {
    if (draft.value?.state === 'open' && !refs.value.length) return '开放准入须填写证据引用'
    if (refs.value.length > 16) return '最多填写 16 条证据引用'
    if (new Set(refs.value).size !== refs.value.length) return '证据引用不能重复'
    if (
      refs.value.some(
        (item) =>
          new TextEncoder().encode(item).length > 256 ||
          (!/^acceptance:[A-Z0-9][A-Za-z0-9_-]{0,95}$/.test(item) &&
            (!/^docs\/[a-zA-Z0-9_/-]+\.md(?:#[\p{L}\p{Nd}_-]{1,80})?$/u.test(item) ||
              item.split('#')[0].includes('//')))
      )
    )
      return '请填写有效文档路径或验收编号，每条不超过 256 字节'
    return ''
  })
  const conflict = computed(
    () => !!draft.value && (draft.value.conflict || draft.value.version !== saved.value?.version)
  )
  const dirty = computed(
    () =>
      !!draft.value &&
      !!saved.value &&
      (draft.value.state !== saved.value.state ||
        JSON.stringify(refs.value) !== JSON.stringify(saved.value.evidence_refs))
  )
  const current = (request: number, key: string) =>
    alive &&
    props.open &&
    allowed.value &&
    request === generation &&
    key === scope.value &&
    key === activeScope
  const canSave = computed(
    () =>
      current(generation, scope.value) &&
      editing.value &&
      fresh.value &&
      dirty.value &&
      !conflict.value &&
      !evidenceError.value &&
      !loading.value &&
      !saving.value
  )
  function persist() {
    if (current(generation, scope.value))
      session.saveDraft(activeScope, dirty.value || conflict.value ? draft.value : null)
  }
  function checked(result: Gate, account: string, targetPlatform: Platform): Gate {
    if (
      result?.platform !== targetPlatform ||
      result.account_id !== account ||
      !['closed', 'open'].includes(result.state) ||
      !Number.isSafeInteger(result.version) ||
      result.version < 0 ||
      !Array.isArray(result.evidence_refs) ||
      !result.evidence_refs.every((item) => typeof item === 'string')
    )
      throw new Error('准入记录与当前机器人范围不匹配，请重新读取')
    return result
  }
  async function load() {
    if (!current(generation, scope.value) || saving.value || !platform.value) return
    controller?.abort()
    const request = ++generation,
      key = activeScope,
      account = String(props.account.id),
      targetPlatform = platform.value,
      abort = new AbortController()
    controller = abort
    loading.value = true
    fresh.value = false
    error.value = ''
    savedMessage.value = ''
    try {
      const result = await api<Gate>(
        `${gatePath.value}/${encodeURIComponent(account)}`,
        'GET',
        undefined,
        abort.signal,
        targetPlatform
      )
      if (!current(request, key)) return
      saved.value = checked(result, account, targetPlatform)
      draft.value ||= (session.drafts[key] as Draft | null) || null
      fresh.value = true
    } catch (exception) {
      if (current(request, key)) error.value = (exception as Error).message
    } finally {
      if (current(request, key)) loading.value = false
    }
  }
  function edit() {
    if (!saved.value || !fresh.value || loading.value || saving.value) return
    draft.value ||= {
      state: saved.value.state,
      version: saved.value.version,
      evidenceText: saved.value.evidence_refs.join('\n'),
      conflict: false
    }
    editing.value = true
    savedMessage.value = ''
  }
  function useCurrentVersion() {
    if (!draft.value || !saved.value || !fresh.value || loading.value || saving.value) return
    draft.value.version = saved.value.version
    draft.value.conflict = false
    error.value = ''
  }
  async function save() {
    if (!canSave.value || !draft.value || !saved.value || !platform.value) return
    const request = ++generation,
      key = activeScope,
      account = String(props.account.id),
      targetPlatform = platform.value,
      abort = new AbortController()
    controller = abort
    saving.value = true
    error.value = ''
    savedMessage.value = ''
    try {
      const result = await api<Gate>(
        `${gatePath.value}/${encodeURIComponent(account)}`,
        'PUT',
        { state: draft.value.state, version: saved.value.version, evidence_refs: [...refs.value] },
        abort.signal,
        targetPlatform
      )
      if (!current(request, key)) return
      saved.value = checked(result, account, targetPlatform)
      draft.value = null
      editing.value = false
      savedMessage.value = `${gateTitle.value}已保存`
    } catch (exception) {
      if (!current(request, key)) return
      if (exception instanceof ApiError && exception.status === 409) {
        draft.value!.conflict = true
        fresh.value = false
      } else if (!(exception instanceof ApiError)) {
        fresh.value = false
      }
      error.value =
        exception instanceof ApiError && exception.code === 'group_binding_friend_gate_invalid'
          ? '请核对准入状态及证据引用'
          : (exception as Error).message
    } finally {
      if (current(request, key)) saving.value = false
    }
  }
  function close() {
    generation++
    controller?.abort()
    activeScope = ''
    emit('close')
  }
  watch(draft, persist, { deep: true, flush: 'sync' })
  watch(
    () => [props.open, scope.value, allowed.value],
    () => {
      generation++
      controller?.abort()
      activeScope = ''
      saved.value = null
      draft.value = null
      loading.value = saving.value = editing.value = fresh.value = false
      error.value = savedMessage.value = ''
      if (props.open && allowed.value) {
        activeScope = scope.value
        void load()
      }
    },
    { immediate: true, flush: 'sync' }
  )
  onBeforeUnmount(() => {
    alive = false
    generation++
    controller?.abort()
  })
</script>

<style scoped>
  .friend-gate-identity {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 20px;
  }
  .friend-gate-identity img {
    width: 36px;
    height: 36px;
    object-fit: contain;
  }
  .friend-gate-identity > div {
    display: grid;
    min-width: 0;
    gap: 4px;
    overflow-wrap: anywhere;
  }
  .friend-gate-identity span,
  .friend-gate-note,
  .friend-gate-record > span {
    color: var(--el-text-color-secondary);
    font-size: 13px;
  }
  .friend-gate-saved {
    padding-block: 16px;
    border-block: 1px solid var(--el-border-color-light);
  }
  .friend-gate-record,
  .friend-gate-footer {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }
  .friend-gate-saved ul {
    padding-left: 20px;
    overflow-wrap: anywhere;
  }
  .friend-gate-explanation {
    margin-block: 16px;
    line-height: 1.6;
  }
  .friend-gate-explanation p,
  .friend-gate-note {
    margin-block: 8px;
  }
  .friend-gate-limits {
    display: grid;
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 4px 16px;
    padding-left: 20px;
    font-size: 13px;
  }
  .friend-gate-editor {
    margin-top: 20px;
  }
  .el-alert {
    margin-bottom: 12px;
  }
  .friend-gate-footer {
    justify-content: flex-end;
  }
  .friend-gate-footer .el-button {
    margin-left: 0;
  }
  @media (max-width: 480px) {
    .friend-gate-limits {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
