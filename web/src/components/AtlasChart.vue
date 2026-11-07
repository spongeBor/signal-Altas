<script setup lang="ts">
import { chartAppearance } from '../composables/useTheme'
import { onMounted, onBeforeUnmount, ref, watch } from 'vue'
import { init, use, type EChartsCoreOption, type EChartsType } from 'echarts/core'
import { LineChart, ScatterChart } from 'echarts/charts'
import {
  GridComponent,
  TooltipComponent,
  LegendComponent,
  DataZoomComponent,
  MarkLineComponent,
} from 'echarts/components'
import { SVGRenderer } from 'echarts/renderers'
use([
  LineChart,
  ScatterChart,
  GridComponent,
  TooltipComponent,
  LegendComponent,
  DataZoomComponent,
  MarkLineComponent,
  SVGRenderer,
])
const props = withDefaults(
  defineProps<{ option: EChartsCoreOption; label: string; height?: number }>(),
  { height: 320 },
)
const emit = defineEmits<{ select: [at: string] }>()
const host = ref<HTMLElement>()
let chart: EChartsType | undefined, observer: ResizeObserver | undefined
function draw() {
  const t = chartAppearance.value
  // An option may describe several stacked plots sharing one time axis (traffic
  // splits download from upload). The zoom controls must name every x axis or
  // they would only drive the first one and the panels would drift apart.
  const xCount = Array.isArray((props.option as { xAxis?: unknown[] }).xAxis)
    ? (props.option as { xAxis: unknown[] }).xAxis.length
    : 1
  const zoomAxis = Array.from({ length: xCount }, (_, i) => i)
  chart?.setOption(
    {
      color: t.colors,
      animation: false,
      textStyle: {
        fontFamily: 'Inter, -apple-system, PingFang SC, sans-serif',
        fontSize: 12,
        color: t.text,
      },
      grid: { top: 65, left: 62, right: 30, bottom: 76 },
      tooltip: {
        trigger: 'axis',
        renderMode: 'richText',
        confine: true,
        valueFormatter: (v: unknown) =>
          typeof v === 'number' ? v.toLocaleString('zh-CN', { maximumFractionDigits: 2 }) : '—',
        backgroundColor: t.surface,
        borderColor: t.line,
        textStyle: { color: t.ink, fontSize: 12 },
        axisPointer: { type: 'line', lineStyle: { color: t.accent, type: 'dashed' } },
      },
      legend: {
        top: 6,
        left: 16,
        right: 16,
        type: 'scroll',
        itemWidth: 12,
        itemHeight: 8,
        textStyle: { color: t.text, fontSize: 12 },
      },
      xAxis: {
        type: 'time',
        axisLine: { lineStyle: { color: t.line } },
        axisTick: { show: false },
        splitLine: { show: false },
        axisLabel: { hideOverlap: true, color: t.text },
      },
      yAxis: {
        type: 'value',
        nameLocation: 'end',
        nameTextStyle: { padding: [0, 0, 0, 10], color: t.text },
        axisLabel: { color: t.text },
        splitLine: { lineStyle: { color: t.split, type: 'dashed' } },
      },
      dataZoom: [
        { type: 'inside', filterMode: 'none', zoomOnMouseWheel: 'ctrl', xAxisIndex: zoomAxis },
        {
          type: 'slider',
          bottom: 16,
          height: 20,
          xAxisIndex: zoomAxis,
          borderColor: 'transparent',
          backgroundColor: t.slider,
          fillerColor: t.filler,
          handleStyle: { color: t.handle, borderColor: t.accent },
          textStyle: { fontSize: 10 },
          brushSelect: false,
        },
      ],
      ...props.option,
    },
    { notMerge: true },
  )
}
function reset() {
  chart?.dispatchAction({ type: 'dataZoom', start: 0, end: 100 })
}
onMounted(() => {
  if (!host.value) return
  chart = init(host.value, undefined, { renderer: 'svg' })
  chart.on('click', (e: any) => {
    if (e.data?.sourceAt) emit('select', e.data.sourceAt)
  })
  observer = new ResizeObserver(() => chart?.resize())
  observer.observe(host.value)
  draw()
})
watch([() => props.option, chartAppearance], draw, { deep: true })
onBeforeUnmount(() => {
  observer?.disconnect()
  chart?.dispose()
})
</script>
<template>
  <div class="atlas-chart-wrap">
    <div
      ref="host"
      class="atlas-chart"
      role="img"
      :aria-label="label"
      :style="{ height: height + 'px' }"
    ></div>
    <div class="chart-controls">
      <span>悬停查看数值 · 拖动底部选取时间</span>
      <button type="button" @click="reset">重置缩放</button>
    </div>
  </div>
</template>
