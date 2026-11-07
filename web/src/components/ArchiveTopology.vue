<script setup lang="ts">
import { computed } from 'vue'
import type { Topology } from '../types'
const props = defineProps<{ topology: Topology }>()
const nodes = computed(() => props.topology.nodes || [])
const edges = computed(() =>
  (props.topology.links || [])
    .filter((e) => e.kind !== 'hosting')
    .map((e) => ({
      ...e,
      a: nodes.value.find((n) => n.id === e.from),
      b: nodes.value.find((n) => n.id === e.to),
    }))
    .filter((e) => e.a && e.b),
)
const width = computed(() => Math.max(650, ...nodes.value.map((n) => n.x + 95)))
const height = computed(() => Math.max(240, ...nodes.value.map((n) => n.y + 65)))
</script>
<template>
  <div class="archive-topology">
    <svg :viewBox="`0 0 ${width} ${height}`" role="img" aria-label="所选时间保存的拓扑关系">
      <g v-for="(e, i) in edges" :key="i">
        <path
          :d="`M${e.a!.x + 69},${e.a!.y} C${(e.a!.x + e.b!.x) / 2},${e.a!.y} ${(e.a!.x + e.b!.x) / 2},${e.b!.y} ${e.b!.x - 69},${e.b!.y}`"
          fill="none"
          stroke="#94a3b8"
          stroke-width="2"
        />
        <text
          :x="(e.a!.x + e.b!.x) / 2"
          :y="Math.min(e.a!.y, e.b!.y) - 40"
          text-anchor="middle"
          class="chart-text"
        >
          {{ e.label }}
        </text>
        <title>{{ e.evidence }}</title>
      </g>
      <g v-for="n in nodes" :key="n.id" :transform="`translate(${n.x - 69},${n.y - 30})`">
        <rect width="138" height="60" rx="10" fill="#fff" stroke="#cbd5e1" />
        <text x="69" y="24" text-anchor="middle" font-size="11">{{ n.name }}</text>
        <text x="69" y="44" text-anchor="middle" class="chart-text">
          {{ n.host || n.subtitle }}
        </text>
        <title>{{ n.note }}</title>
      </g>
    </svg>
    <p class="table-note">
      这是当时保存的连接关系；线缆证据以原记录时间为准，不代表该时刻所有节点都在线。
    </p>
  </div>
</template>
