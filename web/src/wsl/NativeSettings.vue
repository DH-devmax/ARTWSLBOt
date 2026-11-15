<template>
  <ElDialog
    :model-value="open"
    :title="group ? '修改群内昵称' : `${accountOptionLabel(account)} · 账号资料与设置`"
    class="platform-config-dialog native-settings-dialog"
    width="600px"
    align-center
    append-to-body
    :close-on-click-modal="false"
    :before-close="requestClose"
  >
    <div class="settings-identity">
      <MessageImage
        v-if="!group && avatarURL"
        class="settings-identity-avatar"
        :url="avatarURL"
        :workspace="workspace"
        :scope="scope"
        :label="account.name"
        avatar
      />
      <ElIcon v-else class="settings-identity-icon" :size="24"><User /></ElIcon>
      <div class="settings-identity-name">
        <strong>{{ group?.name || accountOptionLabel(account) }}</strong>
        <span>{{
          group
            ? '群内资料'
            : (account.platform === 'lajiaoliao' ? '辣椒聊' : '旺商聊') +
              ' · ' +
              (account.purpose === 'chat' ? '热群账号' : '群管账号')
        }}</span>
      </div>
      <ElTag
        v-if="account.auth_state"
        size="small"
        :type="account.auth_state === 'authenticated' ? 'success' : 'info'"
      >
        {{ account.auth_state === 'authenticated' ? '已登录' : '未登录' }}
      </ElTag>
    </div>
    <div v-if="error" class="settings-error" role="alert">
      <ElIcon><Warning /></ElIcon>
      <span>{{ error }}</span>
      <ElButton
        v-if="needsLogin && !group"
        text
        type="primary"
        :disabled="busy"
        @click="requestReauth"
      >
        重新登录
      </ElButton>
    </div>
    <div v-if="operation || pendingKey" class="settings-operation" role="status">
      <ElIcon :class="{ 'settings-success': operation?.status === 'succeeded' }">
        <CircleCheck v-if="operation?.status === 'succeeded'" /><Warning v-else />
      </ElIcon>
      <div>
        <strong>{{ operationLabel }}</strong>
        <span>请求编号：{{ operation?.id || pendingKey }}</span>
      </div>
      <ElTooltip v-if="pendingKey" content="查询结果">
        <ElButton text :icon="Refresh" aria-label="查询结果" :disabled="busy" @click="inspect" />
      </ElTooltip>
      <ElButton
        v-if="operation?.status === 'unknown' && !securityOperation"
        text
        type="primary"
        :disabled="busy || !pendingVerified"
        @click="reconcile"
        >核对当前值</ElButton
      >
    </div>
    <ElAlert
      v-if="!pendingVerified && !loading"
      title="未能核对待处理请求，暂不能保存设置"
      type="warning"
      :closable="false"
    />
    <div v-loading="loading" class="native-settings-body" :aria-busy="loading">
      <template v-if="snapshot && group">
        <ElAlert
          v-if="snapshot.native_actions_enabled === false"
          title="此账号的原生动作尚未开放；群昵称修改保持关闭"
          type="warning"
          :closable="false"
        />
        <ElAlert
          v-if="snapshot.nickname_locked"
          title="当前群已禁止修改群昵称"
          type="warning"
          :closable="false"
        />
        <dl class="settings-facts">
          <div
            ><dt>成员</dt
            ><dd
              >{{ snapshot.member.name || snapshot.member.id }}
              {{ snapshot.self_id === snapshot.member.id ? '（机器人本人）' : '' }}</dd
            ></div
          >
          <div
            ><dt>原群昵称</dt><dd>{{ snapshot.member.card || '未设置' }}</dd></div
          >
        </dl>
        <ElForm label-position="top" @submit.prevent>
          <ElFormItem label="新群昵称" :error="nameError">
            <ElInput
              v-model="draft.nickname"
              :maxlength="40"
              :disabled="blocked || !available('member_nickname')"
              aria-label="新群昵称"
            />
          </ElFormItem>
          <div class="settings-name-footer">
            <span>{{ draft.nickname.length }} / 20 字符</span>
            <ElButton
              type="primary"
              :icon="Check"
              :disabled="!canSave('member_nickname')"
              :loading="savingKind === 'member_nickname'"
              @click="save('member_nickname')"
              >保存群昵称</ElButton
            >
          </div>
        </ElForm>
      </template>
      <template v-else-if="!group">
        <ElTabs v-model="activeTab" class="settings-tabs" stretch>
          <ElTabPane label="账号资料" name="profile">
            <dl class="settings-facts">
              <div
                ><dt>网站机器人名称</dt><dd>{{ account.name }}</dd></div
              >
              <div
                ><dt>原生账号 ID</dt
                ><dd>{{ snapshot?.settings?.uid || account.native_uid || '未采集' }}</dd></div
              >
              <div
                ><dt>原生头像</dt
                ><dd>{{
                  snapshot?.settings
                    ? snapshot.settings.avatar
                      ? avatarURL
                        ? '当前头像见上方'
                        : '已设置，预览暂不可用'
                      : '未设置'
                    : '未采集'
                }}</dd></div
              >
            </dl>
            <ElForm label-position="top" @submit.prevent>
              <ElFormItem label="原生昵称" :error="nameError">
                <ElInput
                  v-model="draft.nickname"
                  :disabled="blocked || !available('nickname')"
                  :maxlength="40"
                  aria-label="原生昵称"
                  placeholder="请输入原生昵称"
                />
              </ElFormItem>
              <div class="settings-name-footer">
                <span>{{ draft.nickname.length }} / 20 字符</span>
                <div class="settings-save-actions">
                  <ElTooltip content="恢复原生昵称">
                    <ElButton
                      text
                      :icon="RefreshLeft"
                      aria-label="恢复原生昵称"
                      :disabled="blocked || !changed('nickname')"
                      @click="restore('nickname')"
                    />
                  </ElTooltip>
                  <ElButton
                    type="primary"
                    :icon="Check"
                    :disabled="!canSave('nickname')"
                    :loading="savingKind === 'nickname'"
                    @click="save('nickname')"
                    >保存昵称</ElButton
                  >
                </div>
              </div>
            </ElForm>
          </ElTabPane>
          <ElTabPane v-if="securityCapabilities.avatar === true" label="头像" name="security">
            <NativeSecurity
              v-if="open"
              :key="scope"
              ref="securityPanel"
              :account="account"
              :workspace="workspace"
              :scope="scope"
              :snapshot="snapshot"
              :avatar-url="avatarURL"
              :blocked="blocked"
              :supported="securitySupported"
              :capabilities="securityCapabilities"
              :operation="operation"
              @busy="busy = $event"
              @pending="securityPending"
              @operation="acceptOperation"
              @saved="securitySaved"
            />
          </ElTabPane>
        </ElTabs>
      </template>
      <ElEmpty v-else-if="!loading" description="暂未取得群昵称" :image-size="48" />
    </div>
    <template #footer>
      <div class="settings-footer">
        <div class="settings-freshness">
          <span>{{ dirty ? '有未保存修改' : snapshot ? '当前设置已读取' : '尚未读取设置' }}</span>
          <time v-if="snapshot" :datetime="snapshot.sampled_at">{{
            date(snapshot.sampled_at)
          }}</time>
        </div>
        <ElTooltip content="重新读取设置">
          <ElButton
            :icon="Refresh"
            aria-label="重新读取"
            :disabled="loading || busy || confirming"
            @click="refresh"
          />
        </ElTooltip>
        <ElButton @click="requestClose()">关闭</ElButton>
      </div>
    </template>
  </ElDialog>
</template>

<script setup lang="ts">
  import { computed, onUnmounted, ref, watch } from 'vue'
  import { Check, CircleCheck, Refresh, RefreshLeft, User, Warning } from '@element-plus/icons-vue'
  import { ElMessageBox } from 'element-plus'
  import { api, ApiError, type Row, type Workspace } from './api'
  import { accountOptionLabel } from './accountIdentity'
  import NativeSecurity from './NativeSecurity.vue'
  import MessageImage from './MessageImage.vue'
  import {
    restoreSetting,
    settingAvailable,
    settingChange,
    settingChanged,
    settingLabels,
    settingNameError,
    settingsDraft,
    settingValue,
    type SettingKind
  } from './nativeSettings'

  const props = defineProps<{
    open: boolean
    account: Row
    workspace: Workspace
    scope: string
    group?: Row | null
    target?: string
  }>()
  const emit = defineEmits<{ close: []; saved: []; reauth: [] }>()
  const snapshot = ref<Row | null>(null),
    baseline = ref<Row | null>(null),
    operation = ref<Row | null>(null)
  const draft = ref(settingsDraft(null)),
    activeTab = ref('profile')
  const loading = ref(false),
    busy = ref(false),
    confirming = ref(false)
  const error = ref(''),
    errorCode = ref(''),
    pendingKey = ref(''),
    pendingVerified = ref(false)
  const savingKind = ref<SettingKind | ''>('')
  const securityPanel = ref<InstanceType<typeof NativeSecurity>>()
  const securitySupported = ref(false)
  const securityCapabilities = ref<Row>({})
  const avatarURL = ref('')
  const securityOperation = computed(
    () =>
      !!operation.value?.flow ||
      /^(password|phone)|^secondary_security_code$/.test(operation.value?.kind || '')
  )
  const blocked = computed(
    () =>
      busy.value ||
      loading.value ||
      confirming.value ||
      !snapshot.value ||
      !!pendingKey.value ||
      !pendingVerified.value
  )
  const kinds = (): SettingKind[] => (props.group ? ['member_nickname'] : ['nickname'])
  const available = (kind: SettingKind) => settingAvailable(kind, snapshot.value)
  const changed = (kind: SettingKind) => settingChanged(kind, draft.value, baseline.value)
  const dirty = computed(() => kinds().some(changed) || !!securityPanel.value?.dirty)
  const nameError = computed(() =>
    baseline.value
      ? props.account.platform === 'lajiaoliao' &&
        props.group &&
        Array.from(draft.value.nickname).length > 15
        ? '群昵称超过 15 字符限制'
        : settingNameError(draft.value.nickname, !!props.group)
      : ''
  )
  const canSave = (kind: SettingKind) =>
    !blocked.value &&
    changed(kind) &&
    (!['nickname', 'member_nickname'].includes(kind) || !nameError.value)
  const needsLogin = computed(() =>
    ['native_reauth_required', 'native_login_required', 'native_session_expired'].includes(
      errorCode.value
    )
  )
  const operationLabel = computed(() => {
    const platform = props.account.platform === 'lajiaoliao' ? '辣椒聊' : '旺商聊'
    if (!operation.value) return '请求结果尚未确认，请按原编号查询'
    if (
      operation.value.status === 'succeeded' &&
      operation.value.kind === 'secondary_security_code'
    )
      return '二级安全码首次设置已获旺商聊确认'
    if (operation.value.status === 'succeeded' && operation.value.flow)
      return operation.value.flow.stage === 'completed'
        ? `${platform}已确认修改，请重新登录此机器人`
        : '本步验证已确认，请继续账号安全流程'
    if (operation.value.status === 'succeeded')
      return operation.value.result_code === 'desired_state_observed'
        ? '已核对：当前值与期望一致；不能证明原请求只执行过一次'
        : `${platform}已确认保存；其他客户端显示可能稍有延迟`
    if (operation.value.status === 'failed')
      return `未保存：${new ApiError(409, operation.value.result_code).message}`
    return operation.value.status === 'unknown'
      ? securityOperation.value
        ? '验证或安全修改结果未知，未自动重试。请核对官方客户端和原请求编号。'
        : '请求结果未知，未自动重试。请核对当前值。'
      : '请求处理中，请查询结果'
  })
  let generation = 0
  let controller: AbortController | undefined
  const storageKey = () => `wsl:setting:${props.scope}`
  const date = (value: string) => new Date(value).toLocaleString('zh-CN', { hour12: false })
  const base = () => `/accounts/${props.account.id}`
  function remember() {
    try {
      if (pendingKey.value) sessionStorage.setItem(storageKey(), pendingKey.value)
      else sessionStorage.removeItem(storageKey())
    } catch {
      // The server's pending query also recovers requests without browser storage.
    }
  }
  function securityPending(key: string) {
    pendingKey.value = key
    operation.value = null
    remember()
  }
  async function securitySaved() {
    emit('saved')
    busy.value = false
    await load(true)
  }
  function showError(cause: unknown) {
    error.value = cause instanceof Error ? cause.message : '设置暂不可用'
    errorCode.value = cause instanceof ApiError ? cause.code : ''
  }
  function acceptOperation(value: Row) {
    if (
      !value ||
      typeof value.id !== 'string' ||
      !['preparing', 'submitting', 'succeeded', 'failed', 'unknown'].includes(value.status)
    )
      throw new Error('操作结果格式不兼容，请按原请求编号核对')
    operation.value = value
    if (['succeeded', 'failed'].includes(value.status)) {
      pendingKey.value = ''
      remember()
    }
  }
  function restore(kind: SettingKind) {
    if (snapshot.value) restoreSetting(kind, draft.value, snapshot.value)
  }
  async function confirm(message: string, title: string, action: string): Promise<boolean> {
    if (confirming.value) return false
    const version = generation
    confirming.value = true
    try {
      await ElMessageBox.confirm(message, title, {
        confirmButtonText: action,
        cancelButtonText: '取消',
        closeOnClickModal: false,
        customClass: 'native-settings-confirm',
        type: 'warning'
      })
      return version === generation && props.open
    } catch {
      return false
    } finally {
      if (version === generation) confirming.value = false
    }
  }
  async function requestClose(done?: () => void) {
    if (confirming.value) return
    if (
      dirty.value &&
      !(await confirm('未保存的修改将丢弃，确认关闭？', '关闭账号设置', '丢弃并关闭'))
    )
      return
    if (done) done()
    emit('close')
  }
  async function requestReauth() {
    if (
      dirty.value &&
      !(await confirm('未保存的修改将丢弃，确认前往重新登录？', '重新登录', '前往登录'))
    )
      return
    emit('close')
    emit('reauth')
  }
  async function refresh() {
    if (loading.value || busy.value || confirming.value) return
    if (
      dirty.value &&
      !(await confirm('重新读取会丢弃未保存的修改，是否继续？', '重新读取设置', '重新读取'))
    )
      return
    await load()
  }
  async function load(preserveDraft = false, savedKind?: SettingKind) {
    const version = ++generation
    controller?.abort()
    controller = new AbortController()
    const signal = controller.signal
    const previous = snapshot.value
    const edited = preserveDraft
      ? kinds().filter((kind) => kind !== savedKind && changed(kind))
      : []
    loading.value = true
    pendingVerified.value = false
    error.value = ''
    errorCode.value = ''
    const query = props.group
      ? `?${new URLSearchParams({ group_id: props.group.id, target_id: props.target || '' })}`
      : ''
    // A failed profile read must not hide an already submitted operation.
    const [settingsResult, pendingResult] = await Promise.allSettled([
      api<Row>(`${base()}/native-settings${query}`, 'GET', undefined, signal, props.workspace),
      api<Row>(`${base()}/native-setting-operations`, 'GET', undefined, signal, props.workspace)
    ])
    if (version !== generation || signal.aborted) return
    if (settingsResult.status === 'fulfilled') {
      try {
        const result = settingsResult.value,
          value = result.snapshot
        securityCapabilities.value = result.security || {}
        securitySupported.value =
          result.security?.password === true ||
          result.security?.phone === true ||
          result.security?.avatar === true
        if (
          result.native_ready !== false ||
          !value ||
          !/^[0-9a-f]{64}$/.test(value.digest) ||
          !Number.isInteger(value.account_epoch) ||
          !Number.isInteger(value.account_version) ||
          !Number.isFinite(Date.parse(value.sampled_at)) ||
          typeof value.self_id !== 'string' ||
          (props.group
            ? value.member?.id !== props.target || typeof value.member.card !== 'string'
            : value.settings?.uid !== value.self_id || typeof value.settings?.nickname !== 'string')
        )
          throw new Error('账号设置格式不兼容')
        snapshot.value = value
        avatarURL.value =
          !props.group &&
          typeof result.avatar_url === 'string' &&
          result.avatar_url.startsWith(`/api/v1${base()}/native-avatar/`)
            ? result.avatar_url
            : ''
        baseline.value = value
        const fresh = settingsDraft(value)
        for (const kind of kinds()) {
          if (!edited.includes(kind) || !settingAvailable(kind, value))
            restoreSetting(kind, draft.value, value)
        }
        if (!preserveDraft || !previous) draft.value = fresh
      } catch (cause) {
        showError(cause)
        snapshot.value = null
        avatarURL.value = ''
      }
    } else {
      showError(settingsResult.reason)
      // Keep drafts visible, but never authorize a write from an old snapshot.
      snapshot.value = null
      avatarURL.value = ''
    }
    if (pendingResult.status === 'fulfilled') {
      try {
        if (!Array.isArray(pendingResult.value.pending)) throw new Error('待处理请求格式不兼容')
        if (pendingResult.value.pending.length) {
          const pending = pendingResult.value.pending[0]
          if (typeof pending.key !== 'string' || !pending.key) throw new Error('待处理请求缺少编号')
          pendingKey.value = pending.key
          acceptOperation(pending)
          remember()
        }
        pendingVerified.value = true
      } catch (cause) {
        showError(cause)
      }
    } else if (!error.value) showError(pendingResult.reason)
    loading.value = false
    if (pendingKey.value && pendingVerified.value) await inspect()
  }
  async function inspect() {
    if (!pendingKey.value || busy.value) return
    const version = generation,
      key = pendingKey.value
    busy.value = true
    try {
      const result = await api<Row>(
        `${base()}/native-setting-operations?key=${encodeURIComponent(key)}`,
        'GET',
        undefined,
        controller?.signal,
        props.workspace
      )
      if (version === generation && key === pendingKey.value) acceptOperation(result.operation)
    } catch (cause) {
      if (version !== generation) return
      if (cause instanceof ApiError && cause.status === 404) {
        pendingKey.value = ''
        remember()
      }
      showError(cause)
    } finally {
      if (version === generation) busy.value = false
    }
  }
  async function reconcile() {
    if (busy.value || !operation.value?.id || !pendingVerified.value) return
    const version = generation
    busy.value = true
    try {
      const result = await api<Row>(
        `${base()}/native-setting-operations/${operation.value.id}/reconcile`,
        'POST',
        {},
        controller?.signal,
        props.workspace
      )
      if (version !== generation) return
      acceptOperation(result.operation)
      if (result.operation.status === 'succeeded') {
        busy.value = false
        await load(true)
      }
    } catch (cause) {
      if (version === generation) showError(cause)
    } finally {
      if (version === generation) busy.value = false
    }
  }
  async function save(kind: SettingKind) {
    if (!canSave(kind) || !snapshot.value) return
    const version = generation,
      original = snapshot.value
    const change = settingChange(kind, draft.value)
    if (kind === 'member_nickname')
      Object.assign(change, { group_id: props.group?.id, target_id: props.target })
    const before = settingValue(kind, settingsDraft(original)),
      after = settingValue(kind, draft.value)
    if (
      !(await confirm(`${settingLabels[kind]}：${before} → ${after}`, '确认原生修改', '确认修改'))
    )
      return
    if (version !== generation || !canSave(kind)) return
    busy.value = true
    savingKind.value = kind
    error.value = ''
    operation.value = null
    pendingKey.value = crypto.randomUUID()
    remember()
    try {
      const result = await api<Row>(
        `${base()}/native-setting-operations`,
        'POST',
        {
          change,
          digest: original.digest,
          account_epoch: original.account_epoch,
          account_version: original.account_version
        },
        controller?.signal,
        props.workspace,
        pendingKey.value
      )
      if (version !== generation) return
      acceptOperation(result.operation)
      if (result.operation.status === 'succeeded') {
        emit('saved')
        if (!props.group) {
          busy.value = false
          savingKind.value = ''
          await load(true, kind)
        }
      }
    } catch (cause) {
      if (version !== generation) return
      showError(cause)
      busy.value = false
      await inspect()
    } finally {
      if (version === generation) {
        busy.value = false
        savingKind.value = ''
      }
    }
  }
  watch(
    () => `${props.open}:${props.scope}:${props.group?.id || ''}:${props.target || ''}`,
    () => {
      generation++
      controller?.abort()
      if (confirming.value) ElMessageBox.close()
      busy.value = false
      confirming.value = false
      savingKind.value = ''
      operation.value = null
      snapshot.value = null
      avatarURL.value = ''
      baseline.value = null
      draft.value = settingsDraft(null)
      activeTab.value = 'profile'
      loading.value = false
      pendingVerified.value = false
      error.value = ''
      try {
        pendingKey.value = sessionStorage.getItem(storageKey()) || ''
      } catch {
        pendingKey.value = ''
      }
      if (props.open) void load()
    },
    { immediate: true }
  )
  onUnmounted(() => {
    generation++
    controller?.abort()
    if (confirming.value) ElMessageBox.close()
  })
</script>

<style scoped>
  .settings-identity {
    display: flex;
    align-items: center;
    gap: 12px;
    margin-bottom: 16px;
  }
  .settings-identity-icon,
  .settings-identity-avatar {
    width: 44px;
    height: 44px;
    flex-shrink: 0;
    background: #edf4ff;
    color: #3664ca;
    border-radius: 8px;
  }
  .settings-identity-name {
    display: flex;
    flex: 1;
    min-width: 0;
    flex-direction: column;
    gap: 4px;
  }
  .settings-identity-name strong {
    font-size: 15px;
    overflow-wrap: anywhere;
  }
  .settings-identity-name span {
    font-size: 12px;
    color: #687386;
  }
  .native-settings-body {
    min-width: 0;
    min-height: 130px;
  }
  .native-settings-body > .el-alert {
    margin-bottom: 12px;
  }
  .settings-error,
  .settings-operation {
    display: flex;
    gap: 8px;
    align-items: flex-start;
    padding: 10px 12px;
    background: #fff5f1;
    border-radius: 6px;
    margin-bottom: 12px;
    color: #a64028;
    font-size: 12px;
    overflow-wrap: anywhere;
  }
  .settings-error > span {
    flex: 1;
    min-width: 0;
  }
  .settings-error :deep(.el-button),
  .settings-operation :deep(.el-button) {
    flex-shrink: 0;
    margin: 0;
    height: auto;
    min-height: 24px;
    padding: 2px;
    white-space: normal;
  }
  .settings-operation {
    flex-wrap: wrap;
    background: #f4f7fa;
    color: #5b6678;
  }
  .settings-operation > div {
    flex: 1;
    min-width: 140px;
    display: grid;
    gap: 4px;
  }
  .settings-operation strong {
    font-weight: 500;
    color: #354154;
    line-height: 1.6;
  }
  .settings-operation span {
    font-size: 11px;
  }
  .settings-success {
    color: #1c8a68;
  }
  .settings-tabs :deep(.el-tabs__header) {
    margin: 0;
  }
  .settings-tabs :deep(.el-tabs__nav-wrap::after) {
    height: 1px;
    background-color: #e9edf2;
  }
  .settings-facts {
    margin: 0 0 16px;
  }
  .settings-facts > div {
    display: grid;
    grid-template-columns: 122px minmax(0, 1fr);
    gap: 12px;
    padding: 12px 0;
    border-bottom: 1px solid var(--ui-line-soft);
    font-size: 13px;
    line-height: 1.6;
  }
  .settings-facts dt {
    color: #687386;
  }
  .settings-facts dd {
    margin: 0;
    color: #273143;
    overflow-wrap: anywhere;
  }
  .settings-facts small {
    color: #8a929e;
    margin-left: 8px;
    font-size: 11px;
  }
  .native-settings-body :deep(.el-form-item) {
    margin-bottom: 8px;
  }
  .native-settings-body :deep(.el-form-item__label) {
    padding: 0 0 5px;
  }
  .settings-name-footer,
  .settings-save-actions {
    display: flex;
    gap: 8px;
    justify-content: space-between;
    align-items: center;
  }
  .settings-name-footer > span {
    color: #87909e;
    font-size: 11px;
  }
  .settings-security {
    display: flex;
    align-items: center;
    gap: 8px;
    color: #687386;
    border-top: 1px solid var(--ui-line-soft);
    padding-top: 16px;
    margin-top: 16px;
    font-size: 12px;
  }
  .settings-security > span {
    flex: 1;
  }
  .settings-security small {
    float: right;
    color: #8a929e;
    font-size: 11px;
  }
  .settings-row {
    display: flex;
    justify-content: space-between;
    gap: 16px;
    align-items: center;
    padding: 18px 0;
    border-bottom: 1px solid var(--ui-line-soft);
  }
  .settings-row:last-child {
    border: 0;
  }
  .settings-row-label {
    display: grid;
    gap: 5px;
    font-size: 13px;
    flex-shrink: 0;
  }
  .settings-row-label small {
    font-size: 11px;
    color: #8a929e;
  }
  .settings-row-controls {
    display: flex;
    align-items: center;
    justify-content: flex-end;
    gap: 10px;
    min-width: 0;
  }
  .settings-row-controls :deep(.el-select) {
    width: 166px;
    min-width: 0;
  }
  .settings-row-controls :deep(.el-button) {
    flex-shrink: 0;
    margin: 0;
  }
  .settings-search-options {
    display: flex;
    flex-wrap: wrap;
    gap: 12px;
  }
  .settings-search-options :deep(.el-checkbox) {
    margin: 0;
  }
  .settings-toggle-value {
    font-size: 12px;
    color: #687386;
  }
  .settings-footer {
    display: flex;
    gap: 8px;
    align-items: center;
    text-align: left;
  }
  .settings-footer :deep(.el-button) {
    margin: 0;
    flex-shrink: 0;
  }
  .settings-freshness {
    min-width: 0;
    flex: 1;
    display: grid;
    gap: 3px;
    font-size: 11px;
    color: #687386;
    overflow-wrap: anywhere;
  }
  .settings-freshness > span {
    color: #354154;
  }
  @media (max-width: 480px) {
    .settings-identity {
      gap: 10px;
      margin-bottom: 12px;
    }
    .settings-facts > div {
      grid-template-columns: 108px minmax(0, 1fr);
      gap: 8px;
    }
    .settings-facts small {
      display: block;
      margin-left: 0;
    }
    .settings-row {
      align-items: stretch;
      flex-direction: column;
      gap: 8px;
      padding: 14px 0;
    }
    .settings-row-controls {
      justify-content: flex-start;
    }
    .settings-row-controls :deep(.el-select),
    .settings-search-options {
      flex: 1;
      width: auto;
    }
    .settings-row-controls > :last-child {
      margin-left: auto;
    }
    .settings-toggle-value {
      flex: 1;
    }
  }
</style>
