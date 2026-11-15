<template>
  <section v-if="allowed" class="controlled-onboarding" aria-label="受控接入">
    <div class="controlled-toolbar">
      <h4>受控接入</h4>
      <div class="controlled-actions">
        <ElButton :icon="Refresh" :loading="loading" :disabled="busy" @click="load">
          刷新接入范围
        </ElButton>
        <ElButton :icon="Plus" :disabled="busy || uncertain" @click="openCreate()">
          创建接入范围
        </ElButton>
      </div>
    </div>
    <p class="controlled-hint">
      仅限本人选定的机器人、申请人和隔离群；默认6小时自动过期，可随时关闭。
    </p>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElAlert
      v-if="uncertain"
      title="创建结果待核对，请刷新列表核对原范围，不要重复创建。"
      type="warning"
      :closable="false"
    />
    <ElEmpty
      v-if="!loading && !items.length"
      description="暂无本人创建的接入范围"
      :image-size="48"
    />
    <ul class="controlled-list">
      <li v-for="item in items" :key="item.id" class="controlled-item">
        <div class="controlled-toolbar">
          <strong>{{ item.group.name }}</strong>
          <ElTag :type="tagType(item)">{{ controlledOnboardingState(item, clock) }}</ElTag>
        </div>
        <dl class="controlled-details">
          <dt>机器人</dt><dd>{{ item.robot.name }} · {{ item.robot.native_id }}</dd> <dt>申请人</dt
          ><dd>{{ item.claimant.name }} · {{ item.claimant.native_id }}</dd> <dt>隔离群</dt
          ><dd>{{ item.group.name }} · {{ item.group.native_id }}</dd> <dt>有效至</dt
          ><dd>{{ customerDate(item.expires_at) }}</dd>
          <dt>已用次数</dt>
          <dd>原生 {{ item.native_attempts_used }} 次 · 模型 {{ item.model_calls_used }} 次</dd>
          <dt>预留费用</dt><dd>{{ item.reserved_usdt }} USDT</dd>
        </dl>
        <ElAlert
          v-if="controlledOnboardingExpired(item, clock)"
          class="controlled-onboarding-expired"
          :title="
            item.state === 'unknown'
              ? '范围已到期，原操作结果仍待核对，请查询此范围'
              : '接入范围已过期，继续测试需创建新范围'
          "
          type="error"
          show-icon
          :closable="false"
          role="alert"
        >
          <template #icon><WarningFilled /></template>
        </ElAlert>
        <ElAlert
          v-if="item.blocked_reason"
          :title="customerReason(item.blocked_reason)"
          type="warning"
          :closable="false"
        />
        <div class="controlled-actions">
          <ElButton :icon="Refresh" :disabled="busy || loading" @click="readScope(item)"
            >查询此范围</ElButton
          >
          <ElButton
            v-if="controlledOnboardingActive(item, clock)"
            type="primary"
            :icon="Right"
            :disabled="busy || loading"
            @click="enterWizard(item)"
            >在用户中心接入</ElButton
          >
          <ElButton
            v-if="controlledOnboardingExpired(item, clock) && item.state !== 'unknown'"
            :icon="Plus"
            :disabled="busy || uncertain"
            @click="openCreate(item)"
            >创建新接入范围</ElButton
          >
          <ElButton
            v-else-if="item.state === 'closed'"
            :icon="Plus"
            :disabled="busy || uncertain"
            @click="openCreate(item)"
            >重新开启接入范围</ElButton
          >
          <ElButton
            v-if="['preparing', 'active'].includes(item.state)"
            :icon="Close"
            :disabled="busy || loading || controlledOnboardingExpired(item, clock)"
            @click="closeTarget = { ...item }"
            >关闭接入范围</ElButton
          >
        </div>
      </li>
    </ul>
    <ElDialog
      v-model="createOpen"
      title="创建受控接入范围"
      width="560px"
      class="controlled-onboarding-dialog"
      append-to-body
      align-center
      :close-on-click-modal="false"
      :close-on-press-escape="!busy"
      :show-close="!busy"
    >
      <ElAlert v-if="dialogError" :title="dialogError" type="error" :closable="false" />
      <ElForm label-position="top" @submit.prevent="reviewCreate">
        <ElFormItem label="执行机器人">
          <ElSelect
            v-model="draft.account_id"
            aria-label="接入执行机器人"
            popper-class="controlled-onboarding-options"
            :disabled="busy || loading"
          >
            <ElOption
              v-for="item in candidates.accounts"
              :key="item.id"
              :value="item.id"
              :label="identityLabel(item)"
            />
          </ElSelect>
        </ElFormItem>
        <ElFormItem label="申请人账号">
          <ElSelect
            v-model="draft.claimant_account_id"
            aria-label="接入申请人账号"
            popper-class="controlled-onboarding-options"
            :disabled="busy || loading"
          >
            <ElOption
              v-for="item in candidates.accounts"
              :key="item.id"
              :value="item.id"
              :label="identityLabel(item)"
              :disabled="item.id === draft.account_id || item.native_id === robot?.native_id"
            />
          </ElSelect>
        </ElFormItem>
        <ElFormItem label="隔离群">
          <ElSelect
            v-model="draft.group_id"
            aria-label="接入隔离群"
            popper-class="controlled-onboarding-options"
            filterable
            :disabled="busy || loading"
          >
            <ElOption
              v-for="item in candidates.groups"
              :key="item.id"
              :value="item.id"
              :label="identityLabel(item)"
            />
          </ElSelect>
        </ElFormItem>
        <ElFormItem label="有效期（分钟）">
          <ElInputNumber
            v-model="draft.minutes"
            aria-label="接入有效期分钟"
            :min="1"
            :max="360"
            :step="1"
            step-strictly
            :disabled="busy"
          />
        </ElFormItem>
      </ElForm>
      <ElAlert
        v-if="robot && !robotTestingEnabled"
        title="请先在测试工具中启用执行机器人的受控测试开关，再创建接入范围。"
        type="warning"
        :closable="false"
      />
      <ElAlert
        v-if="claimant && !claimantTestingEnabled"
        title="请先核对并启用申请人账号的受控测试开关，再创建接入范围。"
        type="warning"
        :closable="false"
      />
      <p class="controlled-hint"
        >最长6小时，可提前关闭。两端账号均须启用受控测试，申请人须与机器人不同，并为所选隔离群的当前群主或管理员。</p
      >
      <template #footer>
        <ElButton :disabled="busy" @click="createOpen = false">取消</ElButton>
        <ElButton type="primary" :icon="Checked" :disabled="!canCreate" @click="reviewCreate">
          核对接入范围
        </ElButton>
      </template>
    </ElDialog>
    <ElDialog
      v-model="confirmOpen"
      title="确认受控接入范围"
      width="500px"
      class="controlled-onboarding-dialog controlled-onboarding-confirm"
      append-to-body
      align-center
      :close-on-click-modal="false"
      :close-on-press-escape="!busy"
      :show-close="!busy"
    >
      <dl class="controlled-details">
        <dt>平台</dt><dd>{{ platformLabel(platform) }}</dd> <dt>机器人</dt
        ><dd>{{ robot ? identityLabel(robot) : '—' }}</dd> <dt>申请人</dt
        ><dd>{{ claimant ? identityLabel(claimant) : '—' }}</dd> <dt>隔离群</dt
        ><dd>{{ group ? identityLabel(group) : '—' }}</dd> <dt>有效期</dt
        ><dd>{{ draft.minutes }}分钟，自服务端创建时起计算，以返回的到期时间为准</dd>
      </dl>
      <p class="controlled-hint"
        >确认后仅建立本次测试范围。有效范围内接入免滑块，好友处理仍需本人确认。</p
      >
      <ElAlert v-if="dialogError" :title="dialogError" type="error" :closable="false" />
      <template #footer>
        <ElButton :disabled="busy" @click="confirmOpen = false">返回修改</ElButton>
        <ElButton
          type="primary"
          :icon="Checked"
          :loading="busy"
          :disabled="!canCreate"
          @click="create"
        >
          确认创建范围
        </ElButton>
      </template>
    </ElDialog>
    <ElDialog
      :model-value="Boolean(closeTarget)"
      title="关闭受控接入范围"
      width="460px"
      class="controlled-onboarding-dialog"
      append-to-body
      align-center
      :close-on-click-modal="false"
      :close-on-press-escape="!busy"
      :show-close="!busy"
      @close="closeTarget = undefined"
    >
      <dl v-if="closeTarget" class="controlled-details">
        <dt>机器人</dt><dd>{{ closeTarget.robot.name }}</dd> <dt>申请人</dt
        ><dd>{{ closeTarget.claimant.name }}</dd> <dt>隔离群</dt
        ><dd>{{ closeTarget.group.name }}</dd> <dt>有效至</dt
        ><dd>{{ customerDate(closeTarget.expires_at) }}</dd>
      </dl>
      <p class="controlled-hint"
        >关闭后停止本范围的接入和业务，已有结果仍可查询。重新开启将创建新范围并重新核验。</p
      >
      <ElAlert v-if="dialogError" :title="dialogError" type="error" :closable="false" />
      <template #footer>
        <ElButton :disabled="busy" @click="closeTarget = undefined">保留</ElButton>
        <ElButton
          type="danger"
          :icon="Close"
          :loading="busy"
          :disabled="!canClose"
          @click="closeScope"
        >
          确认关闭范围
        </ElButton>
      </template>
    </ElDialog>
  </section>
</template>
<script setup lang="ts">
  import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
  import {
    ElAlert,
    ElButton,
    ElDialog,
    ElEmpty,
    ElForm,
    ElFormItem,
    ElInputNumber,
    ElOption,
    ElSelect,
    ElTag
  } from 'element-plus'
  import { Checked, Close, Plus, Refresh, Right, WarningFilled } from '@element-plus/icons-vue'
  import { api, ApiError, type Row } from './api'
  import { useSession } from './store'
  import {
    controlledOnboardingActive,
    controlledOnboardingExpired,
    controlledOnboardingScope,
    controlledOnboardingSameScope,
    controlledOnboardingState,
    customerDate,
    customerReason,
    platformLabel,
    type ControlledOnboardingScope,
    type ControlledOnboardingIdentity
  } from './customer'

  const props = defineProps<{
    platform: string
    accountId: string
    accounts: Row[]
    disabled?: boolean
  }>()
  const emit = defineEmits<{
    onboard: [scope: ControlledOnboardingScope]
    candidates: [groups: ControlledOnboardingIdentity[]]
  }>()
  type Candidate = ControlledOnboardingIdentity & { platform?: string }
  type Draft = {
    account_id: string
    claimant_account_id: string
    group_id: string
    minutes: number
  }
  const session = useSession()
  const allowed = computed(
    () => session.user?.role === 'superadmin' && ['wsl', 'lajiaoliao'].includes(props.platform)
  )
  const identityScope = computed(
    () =>
      `${session.user?.id}:${session.user?.tenant_id || 'operator'}:${session.user?.role}:${session.user?.tenant_role}:${JSON.stringify([...session.permissions].sort())}:${props.platform}:${session.console}`
  )
  const draftScope = computed(
    () => `controlled-onboarding:${identityScope.value}:${props.accountId}:draft`
  )
  const items = ref<ControlledOnboardingScope[]>([])
  const candidates = ref<{ accounts: Candidate[]; groups: Candidate[] }>({
    accounts: [],
    groups: []
  })
  const additionalAccounts = ref<Row[]>([])
  const loading = ref(false),
    busy = ref(false),
    uncertain = ref(false)
  const error = ref(''),
    dialogError = ref('')
  const createOpen = ref(false),
    confirmOpen = ref(false)
  const closeTarget = ref<ControlledOnboardingScope>()
  const draft = ref<Draft>({ account_id: '', claimant_account_id: '', group_id: '', minutes: 360 })
  const clock = ref(Date.now())
  let alive = true,
    generation = 0,
    reads = 0,
    createKey = crypto.randomUUID()
  let expiryTimer: ReturnType<typeof setTimeout> | undefined
  let controller: AbortController | undefined
  let uncertainInput: Draft | undefined
  let priorIDs = new Set<string>()
  const robot = computed(() =>
    candidates.value.accounts.find((item) => item.id === draft.value.account_id)
  )
  const claimant = computed(() =>
    candidates.value.accounts.find((item) => item.id === draft.value.claimant_account_id)
  )
  const group = computed(() =>
    candidates.value.groups.find((item) => item.id === draft.value.group_id)
  )
  const knownAccounts = computed(() => [...props.accounts, ...additionalAccounts.value])
  const robotTestingEnabled = computed(() =>
    knownAccounts.value.some(
      (item) =>
        item.id === robot.value?.id &&
        item.platform === props.platform &&
        item.testing_enabled === true
    )
  )
  const claimantTestingEnabled = computed(() =>
    knownAccounts.value.some(
      (item) =>
        item.id === claimant.value?.id &&
        item.platform === props.platform &&
        item.testing_enabled === true
    )
  )
  const canCreate = computed(
    () =>
      allowed.value &&
      !props.disabled &&
      !busy.value &&
      !loading.value &&
      !uncertain.value &&
      robotTestingEnabled.value &&
      claimantTestingEnabled.value &&
      Boolean(robot.value && claimant.value && group.value) &&
      robot.value?.id !== claimant.value?.id &&
      robot.value?.native_id !== claimant.value?.native_id &&
      Number.isInteger(draft.value.minutes) &&
      draft.value.minutes >= 1 &&
      draft.value.minutes <= 360
  )
  const canClose = computed(() => {
    const target = closeTarget.value
    const latest = items.value.find((item) => item.id === target?.id)
    return (
      target &&
      latest &&
      !busy.value &&
      !loading.value &&
      !props.disabled &&
      allowed.value &&
      ['preparing', 'active'].includes(latest.state) &&
      !controlledOnboardingExpired(latest, clock.value) &&
      latest.version === target.version &&
      controlledOnboardingSameScope(latest, target)
    )
  })
  const identityLabel = (item: ControlledOnboardingIdentity) => `${item.name} · ${item.native_id}`
  const tagType = (item: ControlledOnboardingScope) =>
    controlledOnboardingExpired(item, clock.value)
      ? 'danger'
      : item.state === 'active' && !item.blocked_reason
        ? 'success'
        : 'info'
  const current = (started: string, request: number) =>
    alive && allowed.value && identityScope.value === started && generation === request
  function owned(value: unknown) {
    const item = controlledOnboardingScope(value)
    if (item.actor_id !== session.user?.id || item.platform !== props.platform)
      throw new Error('接入范围已变化，请刷新本人当前平台的记录')
    return item
  }
  function updateExpiry() {
    clearTimeout(expiryTimer)
    clock.value = Date.now()
    const deadlines = items.value
      .map((item) => Date.parse(item.expires_at))
      .filter((time) => time > clock.value)
    if (deadlines.length)
      expiryTimer = setTimeout(
        updateExpiry,
        Math.min(Math.min(...deadlines) - clock.value, 2147483647)
      )
  }
  async function load() {
    if (!allowed.value || busy.value) return
    const started = identityScope.value,
      request = generation,
      read = ++reads
    controller?.abort()
    controller = new AbortController()
    loading.value = true
    additionalAccounts.value = []
    error.value = ''
    try {
      const result = await api<Row>(
        `/test-tools/onboarding/scopes?platform=${props.platform}`,
        'GET',
        undefined,
        controller.signal,
        props.platform as 'wsl' | 'lajiaoliao'
      )
      if (!current(started, request) || read !== reads) return
      if (
        !Array.isArray(result.items) ||
        !Array.isArray(result.candidates?.accounts) ||
        !Array.isArray(result.candidates?.groups)
      )
        throw new Error('接入候选范围暂无法核对，请重新刷新')
      const parseCandidates = (values: Candidate[], impliedPlatform = false) =>
        values.filter(
          (item) =>
            (item.platform === props.platform ||
              (impliedPlatform && item.platform === undefined)) &&
            [item.id, item.name, item.native_id].every(
              (value) => typeof value === 'string' && Boolean(value.trim())
            )
        )
      const next = result.items
        .map(controlledOnboardingScope)
        .filter(
          (item: ControlledOnboardingScope) =>
            item.actor_id === session.user?.id && item.platform === props.platform
        )
      for (const item of next) {
        const previous = items.value.find((value) => value.id === item.id)
        if (
          previous &&
          (!controlledOnboardingSameScope(previous, item) || item.version < previous.version)
        )
          throw new Error('原接入范围不能变更或续期，请查询原记录或创建新范围')
      }
      items.value = next
      candidates.value = {
        accounts: parseCandidates(result.candidates.accounts),
        groups: parseCandidates(result.candidates.groups, true)
      }
      emit('candidates', candidates.value.groups)
      if (
        session.permissions.includes('workspaces:maibot') &&
        candidates.value.accounts.some(
          (item) => !props.accounts.some((account) => account.id === item.id)
        )
      ) {
        const accounts = await api<Row[]>(
          '/accounts',
          'GET',
          undefined,
          controller.signal,
          'maibot'
        )
        if (!current(started, request) || read !== reads) return
        if (!Array.isArray(accounts)) throw new Error('申请人测试开关暂无法核对，请刷新')
        additionalAccounts.value = accounts.filter(
          (account) =>
            account.platform === props.platform &&
            candidates.value.accounts.some((item) => item.id === account.id)
        )
      } else additionalAccounts.value = []
      if (uncertainInput) {
        const found = next.filter(
          (item: ControlledOnboardingScope) =>
            !priorIDs.has(item.id) &&
            item.account_id === uncertainInput!.account_id &&
            item.claimant_account_id === uncertainInput!.claimant_account_id &&
            item.group_id === uncertainInput!.group_id
        )
        if (found.length === 1) {
          uncertain.value = false
          uncertainInput = undefined
          session.saveDraft(`${draftScope.value}:pending`, {})
        }
      }
      updateExpiry()
    } catch (e) {
      if (
        current(started, request) &&
        read === reads &&
        !(e instanceof Error && e.name === 'AbortError')
      )
        error.value = (e as Error).message
    } finally {
      if (current(started, request) && read === reads) loading.value = false
    }
  }
  async function readScope(item: ControlledOnboardingScope) {
    if (busy.value || loading.value || !allowed.value) return
    const started = identityScope.value,
      request = generation,
      read = ++reads
    loading.value = true
    error.value = ''
    try {
      const result = owned(
        await api(
          `/test-tools/onboarding/scopes/${encodeURIComponent(item.id)}`,
          'GET',
          undefined,
          undefined,
          item.platform
        )
      )
      if (!current(started, request) || read !== reads) return
      if (!controlledOnboardingSameScope(item, result) || result.version < item.version)
        throw new Error('接入范围版本已变化，请刷新原列表')
      items.value = items.value.map((value) => (value.id === result.id ? result : value))
      updateExpiry()
    } catch (e) {
      if (current(started, request) && read === reads) error.value = (e as Error).message
    } finally {
      if (current(started, request) && read === reads) loading.value = false
    }
  }
  function openCreate(previous?: ControlledOnboardingScope) {
    if (!allowed.value || busy.value || uncertain.value) return
    dialogError.value = ''
    if (previous)
      draft.value = {
        account_id: previous.account_id,
        claimant_account_id: previous.claimant_account_id,
        group_id: previous.group_id,
        minutes: 360
      }
    createKey = crypto.randomUUID()
    confirmOpen.value = false
    createOpen.value = true
    void load()
  }
  function reviewCreate() {
    if (!canCreate.value) return
    dialogError.value = ''
    confirmOpen.value = true
  }
  async function create() {
    if (!confirmOpen.value || !canCreate.value) return
    const started = identityScope.value,
      request = generation,
      key = createKey
    const input = { platform: props.platform as 'wsl' | 'lajiaoliao', ...draft.value }
    priorIDs = new Set(items.value.map((item) => item.id))
    busy.value = true
    dialogError.value = ''
    try {
      const raw = await api(
        '/test-tools/onboarding/scopes',
        'POST',
        input,
        undefined,
        input.platform,
        key
      )
      if (!current(started, request)) return
      const result = owned(raw)
      if (
        result.account_id !== input.account_id ||
        result.claimant_account_id !== input.claimant_account_id ||
        result.group_id !== input.group_id
      )
        throw new Error('创建结果范围不一致，请刷新列表核对')
      items.value = [result, ...items.value.filter((item) => item.id !== result.id)]
      createOpen.value = confirmOpen.value = false
      createKey = crypto.randomUUID()
      updateExpiry()
    } catch (e) {
      if (!current(started, request)) return
      uncertain.value = !(e instanceof ApiError) || e.status >= 500
      if (uncertain.value) {
        uncertainInput = { ...draft.value }
        session.saveDraft(`${draftScope.value}:pending`, {
          input: uncertainInput,
          ids: [...priorIDs]
        })
        createOpen.value = confirmOpen.value = false
      } else dialogError.value = (e as Error).message
    } finally {
      if (current(started, request)) busy.value = false
    }
  }
  async function closeScope() {
    updateExpiry()
    if (!canClose.value || !closeTarget.value) return
    const item = { ...closeTarget.value },
      started = identityScope.value,
      request = generation
    busy.value = true
    dialogError.value = ''
    try {
      const raw = await api(
        `/test-tools/onboarding/scopes/${encodeURIComponent(item.id)}/close`,
        'POST',
        { version: item.version },
        undefined,
        item.platform
      )
      if (!current(started, request)) return
      const result = owned(raw)
      if (!controlledOnboardingSameScope(item, result) || result.version < item.version)
        throw new Error('关闭结果待核对，请查询原范围')
      items.value = items.value.map((value) => (value.id === result.id ? result : value))
      closeTarget.value = undefined
      updateExpiry()
    } catch (e) {
      if (current(started, request)) {
        error.value = (e as Error).message
        closeTarget.value = undefined
      }
    } finally {
      if (current(started, request)) busy.value = false
    }
  }
  function enterWizard(item: ControlledOnboardingScope) {
    updateExpiry()
    if (
      !allowed.value ||
      busy.value ||
      loading.value ||
      !controlledOnboardingActive(item, clock.value) ||
      item.actor_id !== session.user?.id ||
      item.platform !== props.platform
    )
      return
    emit('onboard', item)
  }
  watch(
    draft,
    (value) => {
      if (!allowed.value || busy.value) return
      confirmOpen.value = false
      createKey = crypto.randomUUID()
      session.saveDraft(draftScope.value, value)
    },
    { deep: true, flush: 'sync' }
  )
  watch(
    draftScope,
    () => {
      generation++
      reads++
      controller?.abort()
      clearTimeout(expiryTimer)
      items.value = []
      candidates.value = { accounts: [], groups: [] }
      emit('candidates', [])
      additionalAccounts.value = []
      createOpen.value = confirmOpen.value = loading.value = busy.value = false
      closeTarget.value = undefined
      error.value = dialogError.value = ''
      const saved = session.drafts[draftScope.value] as Draft | undefined
      draft.value = {
        account_id: saved?.account_id || props.accountId,
        claimant_account_id: saved?.claimant_account_id || '',
        group_id: saved?.group_id || '',
        minutes:
          Number.isInteger(saved?.minutes) && saved!.minutes >= 1 && saved!.minutes <= 360
            ? saved!.minutes
            : 360
      }
      const pending = session.drafts[`${draftScope.value}:pending`] as Row | undefined
      uncertain.value = Boolean(pending?.input)
      uncertainInput = pending?.input
      priorIDs = new Set(pending?.ids || [])
      createKey = crypto.randomUUID()
      void load()
    },
    { immediate: true, flush: 'sync' }
  )
  watch(canCreate, (value) => {
    if (!value && !busy.value) confirmOpen.value = false
  })
  onMounted(() => {
    document.addEventListener('visibilitychange', updateExpiry)
    window.addEventListener('focus', updateExpiry)
  })
  onBeforeUnmount(() => {
    alive = false
    generation++
    controller?.abort()
    clearTimeout(expiryTimer)
    document.removeEventListener('visibilitychange', updateExpiry)
    window.removeEventListener('focus', updateExpiry)
  })
</script>
<style scoped>
  .controlled-onboarding {
    border-block: 1px solid var(--el-border-color-light);
    padding: 16px 0;
    margin: 16px 0;
    min-width: 0;
  }
  .controlled-toolbar,
  .controlled-actions {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
    min-width: 0;
  }
  .controlled-toolbar {
    justify-content: space-between;
  }
  h4 {
    margin: 0;
    font-size: 14px;
    font-weight: 600;
  }
  .controlled-hint {
    color: var(--el-text-color-secondary);
    font-size: 13px;
    line-height: 1.7;
    overflow-wrap: anywhere;
  }
  .controlled-list {
    padding: 0;
    margin: 0;
    list-style: none;
    display: grid;
    gap: 16px;
  }
  .controlled-item {
    min-width: 0;
    padding: 16px 0;
    border-top: 1px solid var(--el-border-color-light);
  }
  .controlled-toolbar strong {
    min-width: 0;
    overflow-wrap: anywhere;
    font-size: 14px;
  }
  .controlled-details {
    display: grid;
    grid-template-columns: 68px minmax(0, 1fr);
    gap: 10px;
    font-size: 13px;
    line-height: 1.7;
  }
  .controlled-details dt {
    color: var(--el-text-color-secondary);
  }
  .controlled-details dd {
    margin: 0;
    overflow-wrap: anywhere;
  }
  .controlled-actions .el-button {
    margin: 0;
    height: auto;
    min-height: 36px;
    max-width: 100%;
    white-space: normal;
    line-height: 1.5;
  }
  .el-alert {
    margin: 12px 0;
  }
</style>
<style>
  .controlled-onboarding-options {
    max-width: calc(100vw - 24px);
    overflow: hidden;
  }
  .controlled-onboarding-options .el-select-dropdown {
    min-width: 0 !important;
    max-width: 100%;
  }
  .controlled-onboarding-options .el-select-dropdown__item {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
  }
  .controlled-onboarding-dialog {
    max-width: calc(100vw - 24px);
    max-height: calc(100dvh - 24px);
    margin: 12px auto;
    display: flex;
    flex-direction: column;
    border-radius: 8px;
  }
  .controlled-onboarding-dialog > .el-dialog__body {
    min-height: 0;
    overflow-y: auto;
    overscroll-behavior: contain;
  }
  .controlled-onboarding-dialog > .el-dialog__header,
  .controlled-onboarding-dialog > .el-dialog__footer {
    flex-shrink: 0;
  }
  .controlled-onboarding-dialog > .el-dialog__footer {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
  }
  .controlled-onboarding-dialog .el-button {
    margin: 0;
    height: auto;
    min-height: 36px;
    max-width: 100%;
    white-space: normal;
    line-height: 1.5;
  }
  .controlled-onboarding-dialog .el-select,
  .controlled-onboarding-dialog .el-input-number {
    width: 100%;
  }
  .controlled-onboarding-dialog .el-alert__content {
    min-width: 0;
    overflow-wrap: anywhere;
  }
  .controlled-onboarding-dialog .el-alert__title {
    white-space: normal;
    overflow-wrap: anywhere;
  }
</style>
