<template>
  <section class="data-section">
    <div class="section-title"
      ><h2>系统监控 · Simon</h2
      ><ElTooltip content="刷新监控"
        ><ElButton
          text
          :icon="Refresh"
          aria-label="刷新监控"
          :loading="loading"
          @click="load" /></ElTooltip
    ></div>
    <ElAlert v-if="error" class="console-alert" :title="error" type="error" :closable="false" />
    <ElEmpty v-if="!sample.available" description="监控尚未连接" :image-size="64" />
    <template v-else>
      <ElAlert
        v-if="sample.stale"
        class="console-alert"
        title="监控暂时不可达，显示上一次采样"
        type="warning"
        :closable="false"
      />
      <ElDescriptions :column="1" border class="console-summary">
        <ElDescriptionsItem label="CPU"
          >{{ sample.cpu_count }} 核 ·
          {{ Number(sample.cpu_usage).toFixed(1) }}%</ElDescriptionsItem
        >
        <ElDescriptionsItem label="内存"
          >{{ gb(sample.used_memory_bytes) }} / {{ gb(sample.memory_bytes) }} GB</ElDescriptionsItem
        >
        <ElDescriptionsItem label="系统负载">{{
          sample.load_average?.join(' / ') || '—'
        }}</ElDescriptionsItem>
        <ElDescriptionsItem label="运行时长"
          >{{ Math.floor(sample.uptime_seconds / 3600) }} 小时</ElDescriptionsItem
        >
        <ElDescriptionsItem label="采样时间">{{
          new Date(sample.sampled_at).toLocaleString('zh-CN')
        }}</ElDescriptionsItem>
        <ElDescriptionsItem label="采样边界">容器可见的系统视图，非宿主全量监控</ElDescriptionsItem>
        <ElDescriptionsItem label="文件 / 进程环境 / Docker 控制">不开放</ElDescriptionsItem>
      </ElDescriptions>
    </template>
  </section>
</template>
<script setup lang="ts">
  import { onMounted, onBeforeUnmount, ref } from 'vue'
  import { Refresh } from '@element-plus/icons-vue'
  import { api, type Row } from './api'
  import { useSession } from './store'
  const session = useSession(),
    sample = ref<Row>({}),
    loading = ref(false),
    error = ref('')
  const controller = new AbortController()
  let interval: ReturnType<typeof setInterval> | undefined
  const gb = (value: number) => (value / 1024 ** 3).toFixed(2)
  async function load() {
    if (loading.value) return
    loading.value = true
    try {
      const result = await api<Row>(
        '/system/monitor',
        'GET',
        undefined,
        controller.signal,
        session.workspace
      )
      if (!controller.signal.aborted) {
        sample.value = result
        error.value = ''
      }
    } catch (e) {
      if (!controller.signal.aborted) error.value = (e as Error).message
    } finally {
      loading.value = false
    }
  }
  onMounted(() => {
    void load()
    interval = setInterval(load, 10000)
  })
  onBeforeUnmount(() => {
    controller.abort()
    clearInterval(interval)
  })
</script>
