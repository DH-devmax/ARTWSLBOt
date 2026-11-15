<template>
  <section class="workspace-insights" aria-label="工作台指标分析">
    <DecodePerformance :latency="overview.decode_latency" />
    <div class="section-title insights-title">
      <h2>群服务与操作</h2>
      <span class="insights-meta">页面同步 {{ stamp(sampledAt) }}</span>
    </div>
    <div class="insights-grid">
      <div class="insights-chart">
        <div class="insights-subheading"
          ><h3>群服务配置</h3><ElTag type="info" effect="plain">当前授权列表</ElTag></div
        >
        <ArtBarChart
          :data="groupCounts"
          :x-axis-data="['已授权', '群管开启', '热群启用', '热群待启']"
          :colors="['#7a5bff']"
          height="210px"
        />
        <p class="insights-meta">返回 {{ groups.length }} 个群 · 启用配置不代表运行验收通过</p>
      </div>
      <div class="insights-chart">
        <div class="insights-subheading"
          ><h3>操作记录分布</h3><ElTag type="info" effect="plain">当前返回列表</ElTag></div
        >
        <template v-if="operations?.length">
          <ArtRingChart
            :data="operationCounts"
            :center-text="String(operations.length)"
            height="190px"
          />
          <div class="insights-legend" aria-label="操作记录图例">
            <span v-for="item in operationCounts" :key="item.name">
              <i :style="{ backgroundColor: item.itemStyle.color }"></i>
              {{ item.name }}<b>{{ item.value }}</b>
            </span>
          </div>
        </template>
        <ElEmpty
          v-else
          :description="operations ? '暂无可见操作记录' : '未提供操作记录'"
          :image-size="56"
          class="insights-empty"
        />
        <p v-if="operations" class="insights-meta"
          >{{ operations.length }} 条记录 · 非历史累计或全平台成功率</p
        >
        <p v-else class="insights-meta">当前工作台未提供该指标</p>
      </div>
    </div>
    <div class="insights-capacity" aria-label="准入额度">
      <div v-for="item in quotas" :key="item.label">
        <div
          ><span>{{ item.label }}</span
          ><strong>{{ format(item.value) }} / {{ format(item.limit) }}</strong></div
        >
        <ElProgress
          v-if="known(item.value) && known(item.limit) && item.limit > 0"
          :percentage="Math.min(100, Math.max(0, (item.value / item.limit) * 100))"
          :show-text="false"
          :stroke-width="5"
          :color="item.value > item.limit ? '#d95a65' : item.color"
        />
        <span v-else class="insights-meta">额度未提供</span>
      </div>
    </div>
    <p class="insights-meta">额度为服务准入上限；账号和群在两平台共享配额。</p>
  </section>
</template>

<script setup lang="ts">
  import { computed } from 'vue'
  import ArtBarChart from '../components/core/charts/art-bar-chart/index.vue'
  import ArtRingChart from '../components/core/charts/art-ring-chart/index.vue'
  import type { Row } from './api'
  import DecodePerformance from './DecodePerformance.vue'

  const props = defineProps<{
    groups: Row[]
    operations?: Row[]
    overview: Row
    sampledAt: string
  }>()
  const active = computed(() => props.groups.filter((group) => group.active))
  const groupCounts = computed(() => [
    active.value.length,
    active.value.filter((group) => group.management_enabled).length,
    active.value.filter((group) => group.ai_enabled).length,
    active.value.filter((group) => group.ai_requested && !group.ai_enabled).length
  ])
  const operationCounts = computed(() =>
    [
      {
        name: '成功',
        value: props.operations?.filter((row) => row.status === 'succeeded').length || 0
      },
      {
        name: '失败',
        value: props.operations?.filter((row) => row.status === 'failed').length || 0
      },
      {
        name: '结果未知',
        value: props.operations?.filter((row) => row.status === 'unknown').length || 0
      },
      {
        name: '等待 / 执行',
        value:
          props.operations?.filter((row) => ['queued', 'running'].includes(row.status)).length || 0
      },
      {
        name: '其他状态',
        value:
          props.operations?.filter(
            (row) => !['succeeded', 'failed', 'unknown', 'queued', 'running'].includes(row.status)
          ).length || 0
      }
    ]
      .map((item, index) => ({
        ...item,
        itemStyle: { color: ['#4c9475', '#cd7478', '#c2a35a', '#6e8eb6', '#a3aa9f'][index] }
      }))
      .filter((item) => item.value > 0)
  )
  const quotas = computed(() => [
    {
      label: '账号名额',
      value: props.overview.accounts,
      limit: props.overview.limits?.accounts,
      color: '#7a5bff'
    },
    {
      label: '授权群名额',
      value: props.overview.groups,
      limit: props.overview.limits?.groups,
      color: '#6f9c82'
    },
    {
      label: '热群名额',
      value: props.overview.ai_groups,
      limit: props.overview.limits?.ai_groups,
      color: '#b6ac5b'
    }
  ])
  const known = (value: unknown): value is number =>
    typeof value === 'number' && Number.isFinite(value)
  const format = (value: unknown) => (known(value) ? value.toLocaleString('zh-CN') : '未提供')
  const stamp = (value: string) =>
    value
      ? new Date(value).toLocaleTimeString('zh-CN', { timeZone: 'Asia/Shanghai', hour12: false })
      : '尚未同步'
</script>

<style scoped>
  .workspace-insights {
    border-block: 1px solid var(--ui-line);
    padding: 20px 0;
    margin: 20px 0;
    min-width: 0;
  }
  .insights-title {
    padding: 0 0 16px;
  }
  .insights-grid {
    display: grid;
    grid-template-columns: minmax(0, 1.3fr) minmax(0, 1fr);
    gap: 24px;
  }
  .insights-subheading {
    display: flex;
    align-items: center;
    flex-wrap: wrap;
    gap: 8px;
  }
  h3 {
    margin: 0;
    font-size: 13px;
    font-weight: 600;
  }
  .insights-meta {
    font-size: 12px;
    line-height: 1.7;
    color: var(--ui-muted);
    overflow-wrap: anywhere;
  }
  .insights-empty {
    height: 210px;
  }
  .insights-legend {
    display: flex;
    flex-wrap: wrap;
    justify-content: center;
    gap: 8px 16px;
    color: var(--el-text-color-secondary);
    font-size: 11px;
  }
  .insights-legend > span {
    display: flex;
    align-items: center;
    gap: 6px;
  }
  .insights-legend i {
    flex-shrink: 0;
    width: 7px;
    height: 7px;
    border-radius: 2px;
  }
  .insights-legend b {
    color: var(--el-text-color-primary);
    font-weight: 500;
  }
  .insights-capacity {
    display: grid;
    grid-template-columns: repeat(3, minmax(0, 1fr));
    gap: 24px;
    margin: 22px 0 10px;
  }
  .insights-capacity > div > div:first-child {
    display: flex;
    flex-wrap: wrap;
    justify-content: space-between;
    gap: 8px;
    font-size: 12px;
    margin-bottom: 9px;
  }
  .insights-capacity strong {
    font-size: 12px;
  }
  @media (max-width: 700px) {
    .insights-grid,
    .insights-capacity {
      grid-template-columns: minmax(0, 1fr);
    }
    .insights-capacity {
      gap: 16px;
    }
  }
</style>
