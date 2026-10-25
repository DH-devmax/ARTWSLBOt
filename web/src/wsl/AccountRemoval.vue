<template>
  <ElDialog
    :model-value="modelValue"
    title="删除机器人"
    class="account-removal-dialog"
    width="560px"
    align-center
    append-to-body
    :close-on-click-modal="false"
    :before-close="close"
    @update:model-value="emit('update:modelValue', $event)"
  >
    <div v-loading="loading" class="removal-body">
      <ElAlert v-if="error" :title="error" type="error" :closable="false" />
      <template v-if="preview">
        <p class="removal-target">{{ preview.name }}</p>
        <p>删除此机器人接入，释放账号名额。此操作无法撤销。</p>
        <dl class="removal-counts">
          <template v-for="[key, label] in cleanupLabels" :key="key">
            <dt>{{ label }}</dt
            ><dd>{{ preview.cleanup[key] || 0 }}</dd>
          </template>
        </dl>
        <p>加密登录会话、人设、模型密钥和账号配置一并清除；注册、登录验证和共享测试窗口终止。</p>
        <ElAlert type="warning" :closable="false" title="群归属和业务证据保留">
          <p>
            {{ preview.retained.group_ownership }} 个群归属、
            {{ preview.retained.ledger_entries }} 条财务记录、
            {{ preview.retained.activities }} 个活动保留。群管机器人删除后关联群停用；
            热群机器人删除后仅停用关联 AI。其他机器人及共享群数据保留。
          </p>
        </ElAlert>
        <p class="removal-note"
          >仅移除本平台接入，不注销旺商聊官方账号。历史离线备份不在本次清理范围内；保留最小删除审计。</p
        >
        <ElAlert
          v-for="blocker in preview.blockers"
          :key="blocker"
          :title="errorText(blocker)"
          type="error"
          :closable="false"
        />
        <ElForm label-position="top" @submit.prevent>
          <ElFormItem label="输入机器人名称确认删除">
            <ElInput v-model="confirmName" autocomplete="off" :disabled="deleting" />
          </ElFormItem>
        </ElForm>
      </template>
    </div>
    <template #footer>
      <ElButton :disabled="deleting || loading" @click="load">重新核对</ElButton>
      <ElButton :disabled="deleting" @click="close()">取消</ElButton>
      <ElButton type="danger" :loading="deleting" :disabled="!canRemove" @click="remove"
        >删除并清理</ElButton
      >
    </template>
  </ElDialog>
</template>

<script setup lang="ts">
  import { computed, onUnmounted, ref, watch } from 'vue'
  import { api, errorText, type Row } from './api'

  const props = defineProps<{
    modelValue: boolean
    account: Row | null
    owner: string
    workspace: 'wsl' | 'maibot'
  }>()
  const emit = defineEmits<{
    'update:modelValue': [value: boolean]
    removed: [value: Row, owner: string]
  }>()
  const preview = ref<Row | null>(null)
  const confirmName = ref('')
  const error = ref('')
  const loading = ref(false)
  const deleting = ref(false)
  let controller: AbortController | undefined
  let generation = 0
  const cleanupLabels = [
    ['messages', '账号消息'],
    ['native_events', '原生接收事件'],
    ['receiver', '消息接收配置与水位'],
    ['memberships', '群成员关系'],
    ['claim_codes', '授权码'],
    ['claims', '认领申请'],
    ['signup_attempts', '注册记录'],
    ['operations', '账号操作'],
    ['test_windows', '独立测试窗口'],
    ['shared_test_windows', '终止并移除账号的共享测试窗口'],
    ['management_groups', '停用的管理群'],
    ['ai_groups', '停用的 AI 群']
  ]
  const canRemove = computed(
    () =>
      !!preview.value &&
      !loading.value &&
      !deleting.value &&
      !preview.value.blockers.length &&
      confirmName.value === preview.value.name
  )
  function close(done?: () => void) {
    if (deleting.value) return
    controller?.abort()
    generation++
    if (done) done()
    else emit('update:modelValue', false)
  }
  async function load() {
    controller?.abort()
    const current = ++generation
    const { account, workspace, owner } = props
    preview.value = null
    error.value = ''
    if (!props.modelValue || !account) return
    controller = new AbortController()
    const signal = controller.signal
    loading.value = true
    try {
      const result = await api<Row>(
        `/accounts/${account.id}/removal-preview`,
        'GET',
        undefined,
        signal,
        workspace
      )
      if (current !== generation || owner !== props.owner || signal.aborted) return
      preview.value = result
    } catch (e) {
      if (current === generation && !signal.aborted) error.value = (e as Error).message
    } finally {
      if (current === generation) loading.value = false
    }
  }
  async function remove() {
    if (!canRemove.value || !preview.value) return
    const { owner, workspace } = props
    const target = preview.value
    const current = generation
    deleting.value = true
    error.value = ''
    try {
      const result = await api<Row>(
        `/accounts/${target.id}`,
        'DELETE',
        {
          version: target.version,
          confirm_name: confirmName.value
        },
        undefined,
        workspace
      )
      emit('removed', result, owner)
      if (current === generation) emit('update:modelValue', false)
    } catch (e) {
      if (current === generation) error.value = (e as Error).message
    } finally {
      deleting.value = false
    }
  }
  watch(
    () => [props.modelValue, props.account?.id, props.workspace, props.owner],
    () => {
      confirmName.value = ''
      void load()
    }
  )
  onUnmounted(() => {
    controller?.abort()
    generation++
  })
</script>

<style>
  .account-removal-dialog.el-dialog {
    width: min(560px, calc(100vw - 24px));
    max-height: calc(100dvh - 24px);
    display: flex;
    flex-direction: column;
    margin: 12px auto;
  }
  .account-removal-dialog .el-dialog__body {
    overflow-y: auto;
    min-height: 0;
  }
  .account-removal-dialog .el-dialog__header,
  .account-removal-dialog .el-dialog__footer {
    flex-shrink: 0;
  }
  .removal-body {
    overflow-wrap: anywhere;
  }
  .removal-body p {
    margin: 12px 0;
    line-height: 1.6;
  }
  .removal-target {
    font-weight: 600;
    color: var(--el-text-color-primary);
  }
  .removal-note {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .removal-counts {
    display: grid;
    grid-template-columns: minmax(0, 1fr) auto;
    gap: 8px 16px;
  }
  .removal-counts dt,
  .removal-counts dd {
    margin: 0;
  }
  .removal-body .el-alert {
    margin: 12px 0;
  }
  .account-removal-dialog .el-dialog__footer {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
    justify-content: flex-end;
  }
  .account-removal-dialog .el-dialog__footer .el-button {
    margin: 0;
  }
</style>
