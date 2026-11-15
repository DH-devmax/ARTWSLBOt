<template>
  <ElDialog
    :model-value="true"
    title="群账号分配"
    width="760px"
    class="group-assignment"
    align-center
    :close-on-click-modal="false"
    @close="emit('close')"
  >
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElSkeleton v-if="!preview && loading" :rows="5" animated />
    <template v-if="preview">
      <p class="assignment-meta"
        >{{ group.name }} · {{ workspace === 'wsl' ? '旺商聊机器人' : '热群机器人' }} · 版本
        {{ preview.version }} · {{ new Date(preview.sampled_at).toLocaleString('zh-CN') }}</p
      >
      <ElAlert
        title="仅保存稳定绑定，不自动邀请、登录或执行群管操作；旧排队操作将取消。"
        type="info"
        :closable="false"
      />
      <ElTable :data="preview.candidates" max-height="340" empty-text="没有对应工作台账号">
        <ElTableColumn label="选择" width="62"
          ><template #default="{ row }"
            ><ElRadio
              v-model="target"
              :value="row.id"
              :disabled="!row.eligible || uncertain || saving"
              :aria-label="`分配给 ${row.name}`"
              ><span /></ElRadio></template
        ></ElTableColumn>
        <ElTableColumn label="网站机器人名称" min-width="155"
          ><template #default="{ row }"
            >{{ row.name
            }}<ElTag v-if="row.id === preview.source_account_id" size="small">当前</ElTag
            ><ElTag v-if="row.id === preview.recommended_account_id" size="small" type="success"
              >推荐</ElTag
            ></template
          ></ElTableColumn
        >
        <ElTableColumn label="绑定群" width="90"
          ><template #default="{ row }">{{ row.bound_groups }} / 30</template></ElTableColumn
        >
        <ElTableColumn label="依据 / 阻塞原因" min-width="250"
          ><template #default="{ row }"
            ><span v-if="row.eligible"
              >角色 {{ row.native_role }} · 等待 {{ row.http_wait_ms.toFixed(1) }} ms · 接收
              {{ row.receive_per_second.toFixed(1) }}/秒</span
            ><span v-else>{{ row.reasons.map(errorText).join('；') }}</span></template
          ></ElTableColumn
        >
      </ElTable>
      <p class="assignment-meta"
        >当前：{{ name(preview.source_account_id) }} → 目标：{{ name(target) }}</p
      >
    </template>
    <template #footer>
      <ElButton :disabled="saving" @click="emit('close')">关闭</ElButton>
      <ElButton :disabled="saving || uncertain" :loading="loading" @click="load">刷新预览</ElButton>
      <ElButton
        type="primary"
        :disabled="!target || loading || (!uncertain && target === preview?.source_account_id)"
        :loading="saving"
        @click="save"
        >{{ uncertain ? '核对同一提交' : '确认分配' }}</ElButton
      >
    </template>
  </ElDialog>
</template>
<script setup lang="ts">
  import { onBeforeUnmount, onMounted, ref } from 'vue'
  import { api, ApiError, errorText, type Row } from './api'
  import { useSession } from './store'
  const props = defineProps<{ group: Row; workspace: import('./workspaces').Workspace }>()
  const emit = defineEmits<{ close: []; saved: [] }>()
  const preview = ref<Row>(),
    target = ref(''),
    error = ref(''),
    loading = ref(false),
    saving = ref(false),
    uncertain = ref(false)
  const controller = new AbortController()
  const workspace = props.workspace,
    group = { ...props.group }
  let key = '',
    payload: Row | undefined
  const pendingKey = `wsl:${useSession().user?.id}:${workspace}:${group.group_id || group.id}:assignment-pending`
  try {
    const stored = JSON.parse(sessionStorage.getItem(pendingKey) || 'null')
    if (
      stored &&
      typeof stored.key === 'string' &&
      stored.payload?.target_account_id &&
      Date.now() - stored.at < 6 * 86400000
    ) {
      key = stored.key
      payload = stored.payload
      target.value = payload!.target_account_id
      uncertain.value = true
      error.value = '有尚未核对的提交，使用原请求编号核对，不会创建第二次迁移'
    }
  } catch {
    sessionStorage.removeItem(pendingKey)
  }
  const name = (id: string) =>
    preview.value?.candidates.find((c: Row) => c.id === id)?.name || (id ? id : '未绑定')
  async function load() {
    loading.value = true
    try {
      const value = await api<Row>(
        `/groups/${group.group_id || group.id}/assignment`,
        'GET',
        undefined,
        controller.signal,
        workspace
      )
      if (!controller.signal.aborted) {
        preview.value = value
        if (!uncertain.value) {
          target.value = ''
          error.value = ''
          key = ''
          payload = undefined
        }
      }
    } catch (e) {
      if (!controller.signal.aborted) error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }
  async function save() {
    if (!preview.value || saving.value) return
    if (!uncertain.value) {
      const candidate = preview.value.candidates.find((c: Row) => c.id === target.value)
      if (!candidate?.eligible) return
      key = crypto.randomUUID()
      payload = {
        target_account_id: candidate.id,
        source_account_id: preview.value.source_account_id,
        source_epoch: preview.value.source_epoch,
        target_epoch: candidate.epoch,
        version: preview.value.version,
        binding_epoch: preview.value.binding_epoch
      }
    }
    saving.value = true
    try {
      sessionStorage.setItem(pendingKey, JSON.stringify({ key, payload, at: Date.now() }))
      await api(
        `/groups/${group.group_id || group.id}/assignment`,
        'POST',
        payload,
        controller.signal,
        workspace,
        key
      )
      if (!controller.signal.aborted) {
        uncertain.value = false
        sessionStorage.removeItem(pendingKey)
        emit('saved')
        emit('close')
      }
    } catch (e) {
      if (!controller.signal.aborted) {
        error.value = (e as Error).message
        uncertain.value = !(e instanceof ApiError) || e.code === 'assignment_commit_unknown'
        if (!uncertain.value) sessionStorage.removeItem(pendingKey)
      }
    } finally {
      saving.value = false
    }
  }
  onMounted(load)
  onBeforeUnmount(() => controller.abort())
</script>
<style>
  .group-assignment {
    max-width: calc(100vw - 24px);
    max-height: calc(100dvh - 24px);
    display: flex;
    flex-direction: column;
    margin: 12px auto;
  }
  .group-assignment .el-dialog__body {
    overflow-y: auto;
    min-height: 0;
  }
  .group-assignment .el-dialog__footer {
    flex-shrink: 0;
    display: flex;
    flex-wrap: wrap;
    justify-content: flex-end;
    gap: 8px;
  }
  .group-assignment .el-dialog__footer .el-button {
    margin-left: 0;
  }
  .assignment-meta {
    font-size: 13px;
    overflow-wrap: anywhere;
    line-height: 1.8;
  }
</style>
