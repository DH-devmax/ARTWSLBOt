import { onBeforeUnmount, onMounted, ref, watch, type WatchSource } from 'vue'
import { init, use, type EChartsCoreOption, type EChartsType } from 'echarts/core'
import { BarChart, PieChart } from 'echarts/charts'
import {
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent
} from 'echarts/components'
import { CanvasRenderer } from 'echarts/renderers'

use([
  BarChart,
  PieChart,
  GridComponent,
  LegendComponent,
  TitleComponent,
  TooltipComponent,
  CanvasRenderer
])

// Core charts can also run in product shells without the framework demo stores.
export function useChartSurface(options: () => EChartsCoreOption, sources: WatchSource[]) {
  const chartRef = ref<HTMLElement>()
  let chart: EChartsType | undefined
  let observer: ResizeObserver | undefined
  let frame = 0
  const render = () => {
    cancelAnimationFrame(frame)
    frame = requestAnimationFrame(() => {
      const element = chartRef.value
      if (!element || !element.clientWidth || !element.clientHeight) return
      chart ||= init(element)
      chart.resize()
      chart.setOption(options(), { notMerge: true })
    })
  }
  watch(sources, render, { deep: true, flush: 'post' })
  onMounted(() => {
    observer = new ResizeObserver(render)
    if (chartRef.value) observer.observe(chartRef.value)
    render()
  })
  onBeforeUnmount(() => {
    cancelAnimationFrame(frame)
    observer?.disconnect()
    chart?.dispose()
  })
  return chartRef
}
