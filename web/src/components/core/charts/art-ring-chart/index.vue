<template>
  <div ref="chartRef" class="art-chart-surface" :style="{ height }" v-loading="loading" />
</template>

<script setup lang="ts">
  import type { RingChartProps } from '@/types/component/chart'
  import { useChartSurface } from '../useChartSurface'

  defineOptions({ name: 'ArtRingChart' })
  const props = withDefaults(defineProps<RingChartProps>(), {
    height: '256px',
    loading: false,
    isEmpty: false,
    colors: () => ['#13a584', '#d95a65', '#d39525', '#4361ee', '#9099aa'],
    data: () => [],
    radius: () => ['52%', '72%'],
    borderRadius: 4,
    centerText: '',
    showLabel: false,
    showTooltip: true,
    showLegend: false,
    legendPosition: 'bottom'
  })
  const chartRef = useChartSurface(
    () => ({
      animation: false,
      color: props.colors,
      tooltip: props.showTooltip
        ? { trigger: 'item', confine: true, renderMode: 'richText', formatter: '{b}: {c} ({d}%)' }
        : undefined,
      legend: props.showLegend
        ? { bottom: 0, type: 'scroll', textStyle: { color: '#687084', fontSize: 11 } }
        : undefined,
      title: {
        text: props.centerText,
        left: 'center',
        top: props.showLegend ? '37%' : '43%',
        textStyle: { color: '#303847', fontSize: 18, fontWeight: 600 }
      },
      series: [
        {
          type: 'pie',
          radius: props.radius,
          center: ['50%', props.showLegend ? '44%' : '50%'],
          data: props.isEmpty ? [] : props.data,
          stillShowZeroSum: false,
          label: { show: props.showLabel, color: '#687084' },
          labelLine: { show: props.showLabel },
          itemStyle: { borderRadius: props.borderRadius, borderColor: '#fff', borderWidth: 2 }
        }
      ]
    }),
    [
      () => props.data,
      () => props.centerText,
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
