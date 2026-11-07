<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ArrowDown, Wifi, Network, Activity, ChevronRight } from 'lucide-vue-next'
import { snapshot, devices, profiles, radios, fmt, clock, request } from '../composables/useAtlas'
import TopologyMap from '../components/TopologyMap.vue'
import TrafficChart from '../components/TrafficChart.vue'
import Drawer from '../components/Drawer.vue'
import ProfileDetails from '../components/ProfileDetails.vue'
import OpticalPanel from '../components/OpticalPanel.vue'
import DeviceSummary from '../components/DeviceSummary.vue'
const ont = computed(() => profiles.value.find((p) => p.adapter === 'zte'))
const online = computed(() => devices.value.filter((d) => d.online).length)
const wireless = computed(() => devices.value.filter((d) => d.ap_id).length)
const selected = ref('')
const current = computed(() => profiles.value.find((p) => p.id === selected.value))
const summaryFilter = ref('all')
const summaryFilters = [
  ['all', '全部'],
  ['openwrt', '软路由'],
  ['asus', '无线 AP'],
  ['esxi', '虚拟化'],
  ['tcp', '交换机'],
] as const
const otherProfiles = computed(() => profiles.value.filter((p) => p.adapter !== 'zte'))
const summaryProfiles = computed(() =>
  otherProfiles.value.filter(
    (p) => summaryFilter.value === 'all' || p.adapter === summaryFilter.value,
  ),
)
const wanMeter = computed(() => snapshot.value?.wan_meter || null)
const ping = computed(() => snapshot.value?.state.ping || [])
const pingHistory = ref<{ target: string; time: string; rtt_avg: number | null }[]>([])
const pingByTarget = computed(() => {
  const out = new Map<string, { target: string; time: string; rtt_avg: number | null }[]>()
  for (const row of pingHistory.value) {
    const list = out.get(row.target) || []
    list.push(row)
    out.set(row.target, list)
  }
  for (const list of out.values()) list.sort((a, b) => a.time.localeCompare(b.time))
  return out
})
const loss = (p: { sent: number; recv: number }) =>
  p.sent > 0 ? Math.round(((p.sent - p.recv) / p.sent) * 100) : 0
function spark(host: string) {
  const rows = (pingByTarget.value.get(ping.value.find((p) => p.host === host)?.target || '') || [])
    .map((r) => r.rtt_avg)
    .filter((v): v is number => v !== null && Number.isFinite(v))
  if (rows.length < 2) return ''
  const max = Math.max(...rows, 1)
  const min = Math.min(...rows)
  const span = Math.max(max - min, 0.5)
  return rows
    .map((v, i) => {
      const x = (i / (rows.length - 1)) * 60
      const y = 18 - ((v - min) / span) * 16
      return `${x.toFixed(1)},${y.toFixed(1)}`
    })
    .join(' ')
}
async function loadPingHistory() {
  const to = new Date()
  const from = new Date(to.getTime() - 24 * 3600 * 1000)
  const q = new URLSearchParams({
    kind: 'ping',
    resolution: 'hour',
    from: from.toISOString(),
    to: to.toISOString(),
    limit: '2000',
  })
  try {
    const res = await request<{ items: { target: string; time: string; rtt_avg: number | null }[] }>(
      '/history?' + q,
    )
    pingHistory.value = res.items
  } catch {
    pingHistory.value = []
  }
}
onMounted(loadPingHistory)
const goodCollectors = computed(
  () =>
    profiles.value.filter(
      (p) => p.status === 'ok' && Date.now() - Date.parse(p.collected_at) < 90000,
    ).length,
)
</script>
<template>
  <section class="metrics-grid">
    <article class="metric">
      <div class="metric-label">
        可确认在线
        <Network :size="17" />
      </div>
      <div class="metric-value">
        {{ online }}
        <small>条 MAC</small>
      </div>
      <p>台账 {{ devices.length }} 条 · 不等于实体台数</p>
    </article>
    <article class="metric">
      <div class="metric-label">
        无线关联
        <Wifi :size="17" />
      </div>
      <div class="metric-value">
        {{ wireless }}
        <small>条关联</small>
      </div>
      <p>{{ radios.length }} 个无线电提供实时数据</p>
    </article>
    <article class="metric">
      <div class="metric-label">
        当前下载流量
        <ArrowDown :size="17" />
      </div>
      <div class="metric-value">
        {{ fmt(snapshot?.state.traffic?.down_mbps, '', 1) }}
        <small>Mbps</small>
      </div>
      <p v-if="wanMeter" class="metric-peak">
        近 {{ wanMeter.window_seconds }} 秒峰值
        <b>{{ fmt(wanMeter.peak_down_mbps, '', 0) }}</b>
        Mbps
      </p>
      <p>上传 {{ fmt(snapshot?.state.traffic?.up_mbps, 'Mbps', 1) }} · 不是测速</p>
    </article>
    <article class="metric">
      <div class="metric-label">
        有效采集连接
        <Activity :size="17" />
      </div>
      <div class="metric-value">
        {{ goodCollectors }}
        <small>/ {{ profiles.length }} 个</small>
      </div>
      <p>当前有有效采样的采集连接</p>
    </article>
  </section>
  <OpticalPanel v-if="ont" :profile="ont" />
  <section class="panel topology-panel">
    <div class="panel-heading">
      <div>
        <h2>网络拓扑</h2>
        <p>把物理连接、管理口与虚拟机承载关系分开看。</p>
      </div>
      <span class="pill">只读观测</span>
    </div>
    <TopologyMap
      v-if="snapshot"
      :topology="snapshot.topology"
      :profiles="profiles"
      :devices="devices"
      :updated="snapshot.state.last_updated"
    />
  </section>
  <section v-if="ping.length" class="panel ping-panel">
    <div class="panel-heading">
      <div>
        <h2>网络时延</h2>
        <p>由路由器发出的 ICMP 探测，含丢包率；右侧是近 24 小时走势。</p>
      </div>
      <span class="pill">2 包 / 轮</span>
    </div>
    <div class="ping-list">
      <div v-for="p in ping" :key="p.host" class="ping-row">
        <div class="ping-name">
          <b>{{ p.target }}</b>
          <small class="mono">{{ p.host }}</small>
        </div>
        <div class="ping-values">
          <span>{{ p.recv ? fmt(p.rtt_avg, ' ms', 2) : '—' }}</span>
          <small>{{ loss(p) }}% 丢包</small>
        </div>
        <svg class="ping-spark" viewBox="0 0 60 20" preserveAspectRatio="none" aria-hidden="true">
          <polyline :points="spark(p.host)" />
        </svg>
      </div>
    </div>
  </section>
  <div class="section-heading">
    <div>
      <h2>基础设备摘要</h2>
      <p class="summary-intro">按设备归集实时数据，历史端口记录单独标明来源。</p>
    </div>
    <RouterLink to="/connections">
      管理采集连接
      <ChevronRight :size="14" />
    </RouterLink>
  </div>
  <nav class="summary-filter" aria-label="设备摘要分类">
    <button
      v-for="[key, label] in summaryFilters"
      :key="key"
      :aria-pressed="summaryFilter === key"
      :class="{ active: summaryFilter === key }"
      @click="summaryFilter = key"
    >
      {{ label }}
      <small>
        {{
          key === 'all'
            ? otherProfiles.length
            : otherProfiles.filter((p) => p.adapter === key).length
        }}
      </small>
    </button>
  </nav>
  <section class="summary-grid">
    <DeviceSummary
      v-for="p in summaryProfiles"
      :key="p.id"
      :profile="p"
      :topology="snapshot?.topology || null"
      @details="selected = $event"
    />
    <div v-if="!summaryProfiles.length" class="empty">这个分类尚无采集连接。</div>
  </section>
  <section class="panel">
    <div class="panel-heading">
      <div>
        <h2>实时流量</h2>
        <p>整个 WAN 接口的流量变化，不等同于测速。</p>
      </div>
    </div>
    <TrafficChart :points="snapshot?.state.history || []" />
  </section>
  <section v-if="snapshot?.state.events.length" class="panel events-panel">
    <div class="panel-heading">
      <div>
        <h2>最近关联变化</h2>
        <p>定时采样可能遗漏短时切换</p>
      </div>
    </div>
    <div v-for="(e, i) in snapshot.state.events.slice(0, 5)" :key="i" class="event-row">
      <time>{{ clock(e.time) }}</time>
      <b>{{ e.name }}</b>
      <span>{{ e.text }}</span>
    </div>
  </section>
  <Drawer v-if="current" @close="selected = ''"><ProfileDetails :profile="current" /></Drawer>
</template>
