<template>
  <div ref="chartRef" class="art-chart-surface" :style="{ height }" v-loading="loading" />
</template>

<script setup lang="ts">
  import { computed } from 'vue'
  import type { BarChartProps, BarDataItem } from '@/types/component/chart'
  import { useChartSurface } from '../useChartSurface'

  defineOptions({ name: 'ArtBarChart' })
  const props = withDefaults(defineProps<BarChartProps>(), {
    height: '256px',
    loading: false,
    isEmpty: false,
    colors: () => ['#4361ee', '#13a584', '#d39525', '#d95a65'],
    borderRadius: 4,
    data: () => [],
    xAxisData: () => [],
    barWidth: '40%',
    stack: false,
    showAxisLabel: true,
    showAxisLine: true,
    showSplitLine: true,
    showTooltip: true,
    showLegend: false,
    legendPosition: 'bottom'
  })
  const series = computed(() =>
    typeof props.data[0] === 'object'
      ? (props.data as BarDataItem[])
      : [{ name: '数量', data: props.data as number[] }]
  )
  const chartRef = useChartSurface(
    () => ({
      animation: false,
      color: props.colors,
      grid: { left: 42, right: 12, top: props.showLegend ? 38 : 16, bottom: 36 },
      tooltip: props.showTooltip
        ? { trigger: 'axis', confine: true, renderMode: 'richText' }
        : undefined,
      legend: props.showLegend ? { top: 0, type: 'scroll' } : undefined,
      xAxis: {
        type: 'category',
        data: props.xAxisData,
        axisTick: { show: false },
        axisLine: { show: props.showAxisLine, lineStyle: { color: '#d8dce5' } },
        axisLabel: { show: props.showAxisLabel, color: '#687084', fontSize: 11, hideOverlap: true }
      },
      yAxis: {
        type: 'value',
        minInterval: 1,
        axisLabel: { show: props.showAxisLabel, color: '#687084' },
        axisLine: { show: false },
        splitLine: { show: props.showSplitLine, lineStyle: { color: '#edf0f4', type: 'dashed' } }
      },
      series: props.isEmpty
        ? []
        : series.value.map((item, index) => ({
            name: item.name,
            type: 'bar',
            data: item.data,
            barWidth: item.barWidth || props.barWidth,
            barMaxWidth: 32,
            stack: props.stack ? item.stack || 'total' : undefined,
            itemStyle: {
              color: props.colors[index % props.colors.length],
              borderRadius: Array.isArray(props.borderRadius)
                ? props.borderRadius
                : [props.borderRadius, props.borderRadius, 0, 0]
            }
          }))
    }),
    [
      () => props.data,
      () => props.xAxisData,
      () => props.colors,
      () => props.isEmpty,
      () => props.showLegend
    ]
  )
</script>

<style scoped>
  .art-chart-surface {
    width: 100%;
    min-width: 0;
  }
</style>
