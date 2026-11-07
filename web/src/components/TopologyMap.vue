<script setup lang="ts">
import { computed, ref } from 'vue'
import type { Topology, TopologyNode, ProfileResult, Device } from '../types'
import { clock } from '../composables/useAtlas'
import Drawer from './Drawer.vue'
import ProfileDetails from './ProfileDetails.vue'
const props = defineProps<{
  topology: Topology | null
  profiles: ProfileResult[]
  devices: Device[]
  updated: string
}>()
const selected = ref<TopologyNode | null>(null)
const graph = computed<Topology>(() => {
  if (props.topology?.nodes?.length) return props.topology
  const ps = props.profiles,
    levels: Record<number, TopologyNode[]> = {}
  function level(p: ProfileResult, seen = new Set<string>()): number {
    if (seen.has(p.id)) return 0
    seen.add(p.id)
    const parent = ps.find((x) => x.id === p.parent)
    return parent ? Math.min(4, 1 + level(parent, seen)) : 0
  }
  for (const p of ps) {
    const l = level(p)
    ;(levels[l] ??= []).push({
      id: p.id,
      name: p.name,
      host: p.host,
      profile_id: p.id,
      x: 85 + l * 180,
      y: 0,
    })
  }
  const nodes = Object.values(levels).flatMap((arr) =>
    arr.map((n, i) => ({ ...n, y: 55 + i * 100 })),
  )
  return {
    nodes,
    links: ps.filter((p) => p.parent).map((p) => ({ from: p.parent, to: p.id, label: '配置关系' })),
  }
})
const bounds = computed(() => ({
  w: Math.max(650, ...graph.value.nodes.map((n) => n.x + 95)),
  h: Math.max(230, ...graph.value.nodes.map((n) => n.y + 65)),
}))
const lookup = computed(() => new Map(graph.value.nodes.map((n) => [n.id, n])))
const edges = computed(() =>
  graph.value.links
    .filter((e) => e.kind !== 'hosting' && lookup.value.has(e.from) && lookup.value.has(e.to))
    .map((e) => {
      const a = lookup.value.get(e.from)!,
        b = lookup.value.get(e.to)!,
        m = (a.x + b.x) / 2
      return {
        ...e,
        path: `M${a.x + 69} ${a.y} C${m} ${a.y} ${m} ${b.y} ${b.x - 69} ${b.y}`,
        x: m,
        y:
          a.y === b.y || (Math.abs(a.y - b.y) >= 25 && Math.abs(a.y - b.y) < 50)
            ? Math.min(a.y, b.y) - 44
            : (a.y + b.y) / 2 - 7,
      }
    }),
)
function profile(n: TopologyNode) {
  return props.profiles.find((p) => p.id === n.profile_id || p.host === n.host)
}
const selectedProfile = computed(() => (selected.value ? profile(selected.value) : undefined))
function live(n: TopologyNode) {
  if (Date.now() - Date.parse(props.updated) > 90000) return false
  const p = profile(n)
  return p ? p.status === 'ok' : props.devices.some((d) => d.mac === n.mac && d.online)
}
const selectedLinks = computed(() =>
  selected.value
    ? graph.value.links.filter((e) => e.from === selected.value!.id || e.to === selected.value!.id)
    : [],
)
</script>
<template>
  <div v-if="!graph.nodes.length" class="empty">添加采集连接后生成拓扑</div>
  <div v-else class="topology-map">
    <svg :viewBox="`0 0 ${bounds.w} ${bounds.h}`" role="img" aria-label="家庭网络连接结构">
      <g v-for="(e, i) in edges" :key="i">
        <path :d="e.path" fill="none" stroke="#adc7b4" stroke-width="2" />
        <text :x="e.x" :y="e.y" text-anchor="middle" class="edge-label">{{ e.label }}</text>
      </g>
      <g
        v-for="n in graph.nodes"
        :key="n.id"
        :transform="`translate(${n.x - 69},${n.y - 32})`"
        class="topology-node"
        role="button"
        tabindex="0"
        :aria-label="`查看${n.name}`"
        @click="selected = n"
        @keydown.enter="selected = n"
        @keydown.space.prevent="selected = n"
      >
        <rect width="138" height="64" rx="10" />
        <circle
          cx="123"
          cy="11"
          r="3"
          :fill="live(n) ? '#71a682' : n.kind === 'switch-unaddressed' ? '#afc7b4' : '#d6ad69'"
        />
        <text x="11" y="26" class="node-name">{{ n.name }}</text>
        <text x="11" y="46" class="node-address">{{ n.host || n.subtitle || '管理地址未核' }}</text>
      </g>
    </svg>
  </div>
  <div class="map-legend">
    <span>
      <i class="dot green"></i>
      当前有应答
    </span>
    <span>
      <i class="dot amber"></i>
      暂无实时确认
    </span>
    <span class="muted">
      {{
        graph.recorded_at
          ? '连线记录更新：' + clock(graph.recorded_at, true)
          : '连线来自采集连接配置'
      }}
    </span>
  </div>
  <p v-if="graph.recorded_at" class="hint compact">
    ESXi 的管理网口与软路由直通网卡分开。点击节点查看实时状态与连接来源。
  </p>
  <Drawer v-if="selected" @close="selected = null">
    <ProfileDetails v-if="selectedProfile" :profile="selectedProfile" />
    <template v-else>
      <div class="eyebrow">CONNECTION RECORD</div>
      <h2>{{ selected.name }}</h2>
      <p class="muted mono">{{ selected.host || selected.subtitle || '管理地址未核' }}</p>
      <p class="hint">{{ selected.note || '连接关系来自本轮核对' }}</p>
    </template>
    <h3>连接依据</h3>
    <div v-for="(e, i) in selectedLinks" :key="i" class="subcard">
      <b>{{ lookup.get(e.from)?.name }} → {{ lookup.get(e.to)?.name }}</b>
      <p>{{ e.label }} · {{ e.evidence || '配置关系' }}</p>
    </div>
  </Drawer>
</template>
