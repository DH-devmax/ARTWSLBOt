<template>
  <ElButton v-if="allowed" :icon="Iphone" @click="open">手机搜索认证</ElButton>
  <ElDialog
    :model-value="visible && allowed"
    title="手机搜索认证"
    width="560px"
    class="platform-config-dialog mobile-search-access"
    align-center
    append-to-body
    :close-on-click-modal="false"
    @update:model-value="visible && !$event && close()"
  >
    <div class="mobile-search-account">
      <strong>{{ accountOptionLabel(account) }}</strong>
      <span>旺商聊 · 手机搜索会话与桌面接收独立</span>
    </div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElSkeleton v-if="loading && !deployment" :rows="4" animated />
    <ElDescriptions v-if="deployment && status" :column="1" border>
      <ElDescriptionsItem label="手机搜索配置">{{
        deployment.configured ? '已配置' : '尚未导入'
      }}</ElDescriptionsItem>
      <ElDescriptionsItem label="配置版本">{{ deployment.version }}</ElDescriptionsItem>
      <ElDescriptionsItem label="协议版本">{{
        deployment.protocol_version || '—'
      }}</ElDescriptionsItem>
      <ElDescriptionsItem label="服务地址">{{ deployment.origin || '—' }}</ElDescriptionsItem>
      <ElDescriptionsItem label="加密运行库">{{
        deployment.crypto_ready ? '可用' : '未启用 libsodium 构建'
      }}</ElDescriptionsItem>
      <ElDescriptionsItem label="业务搜索会话">
        <ElTag :type="status.available ? 'success' : 'info'">{{
          status.available ? '可用' : status.configured ? '需要重新认证' : '尚未保存'
        }}</ElTag>
      </ElDescriptionsItem>
      <ElDescriptionsItem label="认证时间">{{
        status.authenticated_at
          ? new Date(status.authenticated_at).toLocaleString('zh-CN')
          : '暂无记录'
      }}</ElDescriptionsItem>
    </ElDescriptions>
    <ElAlert
      v-if="status?.blocked_reason"
      :title="errorText(status.blocked_reason)"
      type="warning"
      :closable="false"
    />
    <ElForm label-position="top" @submit.prevent="save">
      <ElFormItem label="手机搜索协议配置 JSON">
        <input
          ref="fileInput"
          type="file"
          accept=".json,application/json"
          aria-label="手机搜索协议配置 JSON"
          :disabled="loading || saving"
          @change="selectFile"
        />
      </ElFormItem>
      <p class="mobile-search-note">
        配置文件不超过 60KB，仅加密保存。导入后请单独登录此机器人；不会自动发送短信或登录。
      </p>
    </ElForm>
    <p v-if="savedMessage" role="status">{{ savedMessage }}</p>
    <template #footer>
      <div class="mobile-search-actions">
        <ElTooltip content="重新读取手机搜索状态">
          <ElButton
            :icon="Refresh"
            aria-label="重新读取手机搜索状态"
            :loading="loading"
            :disabled="saving"
            @click="load"
          />
        </ElTooltip>
        <ElButton @click="close">关闭</ElButton>
        <ElButton
          :icon="Upload"
          :loading="saving"
          :disabled="!configuration || !fresh || loading"
          @click="save"
          >导入并保存</ElButton
        >
        <ElButton
          type="primary"
          :icon="Key"
          :disabled="!canAuthenticate"
          @click="openAuthentication"
          >打开手机搜索登录</ElButton
        >
      </div>
    </template>
  </ElDialog>
  <NativeLogin
    v-if="allowed"
    v-model="authOpen"
    :account="account"
    :owner="owner"
    :workspace="workspace"
    adapter="mobile_search"
    @updated="emit('updated')"
  />
</template>

<script setup lang="ts">
  import { computed, onBeforeUnmount, ref, watch } from 'vue'
  import {
    ElAlert,
    ElButton,
    ElDescriptions,
    ElDescriptionsItem,
    ElDialog,
    ElForm,
    ElFormItem,
    ElSkeleton,
    ElTag,
    ElTooltip
  } from 'element-plus'
  import { Iphone, Key, Refresh, Upload } from '@element-plus/icons-vue'
  import { api, errorText, type Row, type Workspace } from './api'
  import { accountOptionLabel } from './accountIdentity'
  import { useSession } from './store'
  import NativeLogin from './NativeLogin.vue'

  interface Deployment {
    configured: boolean
    version: number
    origin?: string
    protocol_version?: string
    crypto_ready: boolean
  }
  interface SearchStatus {
    available: boolean
    configured: boolean
    version: number
    blocked_reason?: string
    authenticated_at?: string
  }
  const props = defineProps<{ account: Row; workspace: Workspace; owner: string }>()
  const emit = defineEmits<{ updated: [] }>()
  const session = useSession()
  const allowed = computed(
    () =>
      !!props.account.id &&
      props.account.platform === 'wsl' &&
      props.account.purpose === 'management' &&
      props.account.auth_state === 'authenticated' &&
      props.workspace === 'wsl' &&
      session.user?.id === props.owner &&
      session.user?.role === 'superadmin' &&
      session.console === 'admin' &&
      session.workspace === props.workspace
  )
  const scope = computed(
    () =>
      `${props.owner}:${props.workspace}:mobile_search:${props.account.id}:${props.account.version}`
  )
  const visible = ref(false),
    authOpen = ref(false),
    loading = ref(false),
    saving = ref(false),
    fresh = ref(false),
    configuration = ref(''),
    error = ref(''),
    savedMessage = ref('')
  const deployment = ref<Deployment | null>(null),
    status = ref<SearchStatus | null>(null),
    fileInput = ref<HTMLInputElement>()
  let generation = 0,
    selection = 0,
    alive = true
  let controller: AbortController | undefined
  const current = (request: number, key: string) =>
    alive && visible.value && allowed.value && request === generation && key === scope.value
  const canAuthenticate = computed(
    () =>
      allowed.value &&
      fresh.value &&
      !loading.value &&
      !saving.value &&
      !!deployment.value?.configured &&
      !!deployment.value.crypto_ready &&
      !!status.value &&
      status.value.blocked_reason !== 'native_mobile_account_unavailable'
  )
  function clearConfiguration(clearFile = true) {
    selection++
    configuration.value = ''
    if (clearFile && fileInput.value) fileInput.value.value = ''
  }
  function reset() {
    generation++
    controller?.abort()
    controller = undefined
    clearConfiguration()
    deployment.value = null
    status.value = null
    fresh.value = false
    loading.value = false
    saving.value = false
    error.value = ''
    savedMessage.value = ''
  }
  function close() {
    visible.value = false
    authOpen.value = false
    reset()
  }
  async function open() {
    if (!allowed.value) return
    close()
    visible.value = true
    await load()
  }
  async function load() {
    if (!visible.value || !allowed.value || saving.value) return
    controller?.abort()
    controller = new AbortController()
    const request = ++generation,
      key = scope.value,
      account = props.account.id,
      workspace = props.workspace,
      signal = controller.signal
    loading.value = true
    fresh.value = false
    error.value = ''
    try {
      const [config, value] = await Promise.all([
        api<Deployment>('/native-mobile-deployment', 'GET', undefined, signal, workspace),
        api<SearchStatus>(
          `/accounts/${account}/native-mobile-auth`,
          'GET',
          undefined,
          signal,
          workspace
        )
      ])
      if (!current(request, key)) return
      if (
        !Number.isSafeInteger(config.version) ||
        config.version < 0 ||
        !Number.isSafeInteger(value.version) ||
        value.version < 0
      )
        throw new Error(errorText('native_mobile_configuration_invalid'))
      // Retain only the public status projection, never returned configuration.
      deployment.value = {
        configured: config.configured === true,
        version: config.version,
        origin: config.origin,
        protocol_version: config.protocol_version,
        crypto_ready: config.crypto_ready === true
      }
      status.value = {
        available: value.available === true,
        configured: value.configured === true,
        version: value.version,
        blocked_reason: value.blocked_reason,
        authenticated_at: value.authenticated_at
      }
      fresh.value = true
    } catch (e) {
      if (current(request, key)) error.value = (e as Error).message
    } finally {
      if (current(request, key)) loading.value = false
    }
  }
  async function selectFile(event: Event) {
    const file = (event.target as HTMLInputElement).files?.[0]
    clearConfiguration(false)
    error.value = ''
    savedMessage.value = ''
    if (!file) return
    const request = generation,
      key = scope.value,
      selected = selection
    let oversized = file.size > 60000
    try {
      if (oversized) throw new Error('配置文件不能超过 60KB')
      const text = await file.text()
      if (!current(request, key) || selected !== selection) return
      oversized = new TextEncoder().encode(text).byteLength > 60000
      if (oversized) throw new Error('配置文件不能超过 60KB')
      const value = JSON.parse(text)
      if (!value || typeof value !== 'object' || Array.isArray(value))
        throw new Error('invalid_configuration')
      configuration.value = text
    } catch (e) {
      if (current(request, key) && selected === selection) {
        clearConfiguration()
        error.value = oversized
          ? '配置文件不能超过 60KB'
          : errorText('native_mobile_configuration_invalid')
      }
    }
  }
  async function save() {
    if (
      !visible.value ||
      !allowed.value ||
      !fresh.value ||
      !configuration.value ||
      loading.value ||
      saving.value
    )
      return
    const request = generation,
      key = scope.value,
      workspace = props.workspace
    saving.value = true
    error.value = ''
    savedMessage.value = ''
    try {
      await api(
        '/native-mobile-deployment',
        'PUT',
        { version: deployment.value!.version, configuration: configuration.value },
        controller?.signal,
        workspace
      )
      if (!current(request, key)) return
      clearConfiguration()
      saving.value = false
      const reloading = load(),
        refreshed = generation
      await reloading
      if (current(refreshed, key) && fresh.value)
        savedMessage.value = '手机搜索配置已保存，请单独打开登录完成认证'
    } catch (e) {
      if (current(request, key)) {
        fresh.value = false
        error.value = (e as Error).message
      }
    } finally {
      if (current(request, key)) saving.value = false
    }
  }
  function openAuthentication() {
    if (!canAuthenticate.value) return
    visible.value = false
    reset()
    authOpen.value = true
  }
  watch([scope, allowed], close, { flush: 'sync' })
  onBeforeUnmount(() => {
    alive = false
    close()
  })
</script>

<style scoped>
  .mobile-search-account {
    display: grid;
    gap: 6px;
    margin-bottom: 16px;
    overflow-wrap: anywhere;
  }
  .mobile-search-account span,
  .mobile-search-note {
    color: var(--el-text-color-secondary);
    font-size: 12px;
    line-height: 1.6;
  }
  .mobile-search-access :deep(.el-alert),
  .mobile-search-access :deep(.el-descriptions) {
    margin-bottom: 16px;
  }
  .mobile-search-access :deep(.el-descriptions__content) {
    overflow-wrap: anywhere;
    word-break: break-word;
  }
  .mobile-search-access input[type='file'] {
    max-width: 100%;
    min-width: 0;
    font-size: 12px;
  }
  .mobile-search-note {
    margin: 0;
  }
  .mobile-search-actions {
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
  }
  .mobile-search-actions :deep(.el-button + .el-button) {
    margin-left: 0;
  }
  @media (max-width: 480px) {
    .mobile-search-actions > :last-child {
      flex-basis: 100%;
    }
  }
</style>
