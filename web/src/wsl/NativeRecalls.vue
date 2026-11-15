<template>
  <ElTooltip content="读取此会话的撤回通知">
    <ElButton text :icon="DocumentDelete" aria-label="撤回通知" @click="open = true" />
  </ElTooltip>
  <ElDialog
    v-model="open"
    title="撤回通知"
    width="640px"
    append-to-body
    class="native-recalls-dialog"
    :close-on-click-modal="false"
  >
    <div class="recall-toolbar">
      <span>最近 7 天 · 只读</span>
      <ElTooltip content="刷新撤回通知">
        <ElButton :icon="Refresh" aria-label="刷新撤回通知" :loading="loading" @click="load()" />
      </ElTooltip>
    </div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" />
    <ElEmpty
      v-if="!loading && !items.length && !error"
      description="此页未找到撤回通知"
      :image-size="48"
    />
    <ul class="recall-list" :aria-busy="loading">
      <li v-for="item in items" :key="item.id">
        <strong>消息已撤回</strong>
        <time>{{ date(item.received_at) }}</time>
        <span :title="item.notice.actor">操作账号 · {{ item.notice.actor }}</span>
        <span :title="item.notice.client_id">原消息 · {{ item.notice.client_id }}</span>
      </li>
    </ul>
    <p v-if="scanned !== null" class="recall-coverage"
      >本次核对 {{ scanned }} 条原始记录，扫描有界</p
    >
    <template #footer>
      <ElButton v-if="nextBefore" :loading="loading" @click="loadOlder">继续核对更早记录</ElButton>
      <ElButton @click="open = false">关闭</ElButton>
    </template>
  </ElDialog>
</template>

<script setup lang="ts">
  import { onUnmounted, ref, watch } from 'vue'
  import { DocumentDelete, Refresh } from '@element-plus/icons-vue'
  import { api, type Row, type Workspace } from './api'

  const props = defineProps<{
    account: string
    conversation: string
    workspace: Workspace
    scope: string
  }>()
  const open = ref(false),
    loading = ref(false),
    error = ref(''),
    items = ref<Row[]>([])
  const nextBefore = ref(''),
    scanned = ref<number | null>(null)
  let generation = 0,
    controller = new AbortController()
  const date = (value: string) => new Date(value).toLocaleString('zh-CN')
  function loadOlder() {
    void load(true)
  }
  async function load(older = false) {
    if (!open.value || loading.value) return
    const version = generation
    loading.value = true
    error.value = ''
    try {
      const result = await api<Row>(
        `/accounts/${props.account}/conversations/${props.conversation}/recalls${older ? `?before=${nextBefore.value}` : ''}`,
        'GET',
        undefined,
        controller.signal,
        props.workspace
      )
      if (version !== generation) return
      const merged = new Map<string, Row>()
      for (const item of [...(older ? items.value : []), ...result.items]) merged.set(item.id, item)
      items.value = [...merged.values()].slice(-500)
      nextBefore.value = result.next_before
      scanned.value = result.scanned
    } catch (cause) {
      if (version === generation && !controller.signal.aborted)
        error.value = (cause as Error).message
    } finally {
      if (version === generation) loading.value = false
    }
  }
  function reset() {
    generation++
    controller.abort()
    controller = new AbortController()
    loading.value = false
    items.value = []
    nextBefore.value = ''
    scanned.value = null
    error.value = ''
  }
  watch(
    () => `${props.scope}:${props.account}:${props.conversation}:${props.workspace}`,
    () => {
      open.value = false
      reset()
    }
  )
  watch(open, (value) => {
    reset()
    if (value) void load()
  })
  onUnmounted(() => {
    generation++
    controller.abort()
  })
</script>

<style scoped>
  .recall-toolbar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    margin-bottom: 16px;
  }
  .recall-toolbar span,
  .recall-coverage {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
  .recall-list {
    list-style: none;
    margin: 0;
    padding: 0;
    max-height: 50vh;
    overflow: auto;
  }
  .recall-list li {
    display: grid;
    grid-template-columns: minmax(0, 1fr);
    gap: 8px;
    padding: 14px 0;
    border-bottom: 1px solid var(--el-border-color-lighter);
  }
  .recall-list span {
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }
  .recall-list time {
    color: var(--el-text-color-secondary);
    font-size: 12px;
  }
</style>
<style>
  .native-recalls-dialog {
    max-width: calc(100vw - 24px);
  }
  .native-recalls-dialog .el-dialog__footer {
    display: flex;
    justify-content: flex-end;
    flex-wrap: wrap;
    gap: 8px;
  }
</style>
