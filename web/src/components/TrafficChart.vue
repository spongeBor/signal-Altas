<script setup lang="ts">
import { chartAppearance } from '../composables/useTheme'
import { computed } from 'vue'
import type { Traffic } from '../types'
import AtlasChart from './AtlasChart.vue'
import { lineData } from '../lib/historyCharts'
const props = defineProps<{ points: Traffic[] }>()
const option = computed(() => ({
  grid: { top: 48, left: 58, right: 20, bottom: 68 },
  yAxis: {
    type: 'value',
    name: 'Mbps',
    min: 0,
    splitLine: { lineStyle: { color: chartAppearance.value.split, type: 'dashed' } },
  },
  series: [
    {
      name: '下载',
      type: 'line',
      showSymbol: false,
      data: lineData(props.points, 'down_mbps', 30000),
      connectNulls: false,
      areaStyle: { opacity: 0.06 },
    },
    {
      name: '上传',
      type: 'line',
      showSymbol: false,
      data: lineData(props.points, 'up_mbps', 30000),
      connectNulls: false,
    },
  ],
}))
</script>
<template>
  <AtlasChart
    v-if="points.length"
    :option="option"
    :height="250"
    label="实时WAN上下行流量，单位Mbps"
  />
  <div v-else class="empty small">完成两轮采集后显示流量趋势</div>
</template>
