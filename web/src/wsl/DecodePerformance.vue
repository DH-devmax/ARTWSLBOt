<template>
  <section v-if="latency" class="decode-performance" aria-label="工作台总解码性能">
    <div class="decode-summary">
      <div>
        <ElTooltip
          content="当前工作台所有账号最新 1000 条有效解码耗时合并计算，最长回看 24 小时；包含源加载和投影加密，不含队列及结果 UPDATE"
        >
          <h3>总解码 P95</h3>
        </ElTooltip>
        <strong class="total-p95">{{ ms(latency.p95_ms) }}</strong>
        <ElTag :type="latency.available ? 'success' : 'warning'" effect="plain">{{
          latency.available ? '已采样' : '采样暂不可用'
        }}</ElTag>
      </div>
      <div class="decode-coverage">
        <div
          ><span>有效样本</span
          ><strong>{{ latency.samples }} / {{ latency.sample_limit }}</strong></div
        >
        <ElProgress
          :percentage="Math.min(100, (latency.samples / latency.sample_limit) * 100)"
          :show-text="false"
          :stroke-width="5"
          color="#148767"
        />
        <p>{{ stamp(latency.from) }} 至 {{ stamp(latency.to) }}</p>
        <p>采样 {{ stamp(latency.sampled_at) }}</p>
      </div>
      <div class="decode-histogram" role="img" aria-label="工作台解码延迟分布">
        <ArtBarChart
          v-if="latency.samples"
          :data="latency.histogram"
          :x-axis-data="['≤0.5', '0.5–1', '1–2', '2–5', '5–10', '>10 ms']"
          :colors="['#148767']"
          height="160px"
        />
        <ElEmpty v-else description="暂无有效解码样本" :image-size="40" />
      </div>
    </div>
  </section>
</template>

<script setup lang="ts">
  import ArtBarChart from '../components/core/charts/art-bar-chart/index.vue'
  import type { Row } from './api'
  defineProps<{ latency?: Row }>()
  const ms = (value?: number | null) => (value == null ? '暂无样本' : `${value.toFixed(2)} ms`)
  const stamp = (value?: string) =>
    value && !value.startsWith('0001-')
      ? new Date(value).toLocaleString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
      : '暂无记录'
</script>

<style scoped>
  .decode-performance {
    padding: 0 0 20px;
    border-bottom: 1px solid var(--ui-line);
    margin-bottom: 20px;
    min-width: 0;
  }
  .decode-summary {
    display: grid;
    grid-template-columns: minmax(150px, 0.8fr) minmax(200px, 1fr) minmax(0, 1.6fr);
    gap: 24px;
    align-items: center;
  }
  .decode-summary > div {
    min-width: 0;
  }
  h3 {
    margin: 0;
    font-size: 13px;
    font-weight: 500;
    color: #687084;
  }
  .total-p95 {
    display: block;
    font-size: 28px;
    margin: 12px 0;
    overflow-wrap: anywhere;
  }
  .decode-coverage > div:first-child {
    display: flex;
    justify-content: space-between;
    gap: 8px;
    flex-wrap: wrap;
    margin-bottom: 10px;
  }
  .decode-coverage {
    font-size: 12px;
  }
  p {
    color: var(--ui-muted);
    margin: 8px 0 0;
    line-height: 1.6;
    overflow-wrap: anywhere;
  }
  .decode-histogram {
    height: 160px;
  }
  @media (max-width: 1000px) {
    .decode-summary {
      grid-template-columns: minmax(0, 1fr) minmax(0, 1.2fr);
    }
    .decode-histogram {
      grid-column: 1 / -1;
    }
  }
  @media (max-width: 500px) {
    .decode-summary {
      grid-template-columns: minmax(0, 1fr);
      gap: 16px;
    }
  }
</style>
