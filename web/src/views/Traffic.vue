<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { ArrowDownToLine, ArrowUpToLine, BarChart3, RefreshCw } from 'lucide-vue-next'
import { clock, devices, fmt, request } from '../composables/useAtlas'

type HistoryRow = { mac: string; name: string; time: string; down_bytes: number; up_bytes: number }
type Usage = { mac: string; name: string; down_bytes: number; up_bytes: number }
type Page = { items: HistoryRow[]; next_cursor?: string }
type RangeKey = '24h' | '7d' | '30d'
const ranges: { key: RangeKey; label: string; hours: number }[] = [
  { key: '24h', label: '24 小时', hours: 24 },
  { key: '7d', label: '7 天', hours: 24 * 7 },
  { key: '30d', label: '30 天', hours: 24 * 30 },
]
const range = ref<RangeKey>('7d')
const rangeLabel = computed(() => ranges.find((r) => r.key === range.value)?.label || '')
const rows = ref<HistoryRow[]>([])
const loading = ref(false)
const error = ref('')
const truncated = ref(false)
function bytes(v?: number) {
  if (v == null || !Number.isFinite(v)) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let n = Math.max(0, v),
    i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n >= 100 || i === 0 ? n.toFixed(0) : n.toFixed(1)} ${units[i]}`
}
const live = computed(() =>
  devices.value
    .filter((d) => d.traffic)
    .map((d) => ({ ...d, total: (d.traffic?.down_mbps || 0) + (d.traffic?.up_mbps || 0) }))
    .sort((a, b) => b.total - a.total),
)
const daily = computed(() => {
  const totals = new Map<string, Usage>()
  for (const r of rows.value) {
    const previous = totals.get(r.mac) || { mac: r.mac, name: r.name, down_bytes: 0, up_bytes: 0 }
    previous.name = r.name || previous.name
    previous.down_bytes += r.down_bytes || 0
    previous.up_bytes += r.up_bytes || 0
    totals.set(r.mac, previous)
  }
  return [...totals.values()]
    .sort((a, b) => b.down_bytes + b.up_bytes - a.down_bytes - a.up_bytes)
    .slice(0, 8)
})
const dailyMax = computed(() => Math.max(1, ...daily.value.map((r) => r.down_bytes + r.up_bytes)))
const latest = computed(() => {
  const times = rows.value.map((r) => Date.parse(r.time)).filter(Number.isFinite)
  return times.length ? clock(new Date(Math.max(...times)).toISOString(), true) : ''
})
let loadSeq = 0
async function load() {
  const seq = ++loadSeq
  loading.value = true
  error.value = ''
  truncated.value = false
  try {
    // Hourly buckets are keyed by their start, so the window must reach the next
    // whole hour to include the bucket still in progress.
    const hours = ranges.find((r) => r.key === range.value)?.hours || 24
    const to = new Date()
    to.setMinutes(0, 0, 0)
    to.setHours(to.getHours() + 1)
    const from = new Date(to.getTime() - hours * 3600000)
    const all: HistoryRow[] = []
    let cursor = ''
    for (let page = 0; page < 20; page++) {
      const q = new URLSearchParams({
        kind: 'device_traffic',
        resolution: 'hour',
        from: from.toISOString(),
        to: to.toISOString(),
        limit: '2000',
      })
      if (cursor) q.set('cursor', cursor)
      const res = await request<Page>('/history?' + q)
      all.push(...res.items)
      if (!res.next_cursor) break
      cursor = res.next_cursor
      if (page === 19) truncated.value = true
    }
    if (seq !== loadSeq) return
    rows.value = all
  } catch (e) {
    if (seq === loadSeq) error.value = (e as Error).message
  } finally {
    if (seq === loadSeq) loading.value = false
  }
}
function pick(key: RangeKey) {
  if (range.value === key) return
  range.value = key
  void load()
}
onMounted(load)
</script>
<template>
  <section class="panel traffic-intro">
    <div class="panel-heading">
      <div>
        <h2>设备流量</h2>
        <p>复用软路由既有 QoS 字节计数；不抓包、不创建规则，也不展示访问内容。</p>
      </div>
      <button class="button subtle" :disabled="loading" @click="load">
        <RefreshCw :size="15" />
        {{ loading ? '读取中…' : '刷新' }}
      </button>
    </div>
    <p class="table-note">
      速率以相邻两次采集差值计算。路由器重启、设备换 IP
      或首次采集会从下一轮重新建立基线，避免产生虚假的流量尖峰。
    </p>
  </section>
  <section class="traffic-grid">
    <article class="panel traffic-panel">
      <div class="panel-heading">
        <div>
          <h2>当前占用</h2>
          <p>按当前采样窗口排序</p>
        </div>
        <BarChart3 :size="20" />
      </div>
      <div v-if="live.length" class="traffic-list">
        <div v-for="d in live.slice(0, 10)" :key="d.mac" class="traffic-row">
          <div>
            <b>{{ d.name }}</b>
            <small>{{ d.ip || d.mac }}</small>
          </div>
          <div class="traffic-rate">
            <span>
              <ArrowDownToLine :size="13" />
              {{ fmt(d.traffic?.down_mbps, ' Mbps', 2) }}
            </span>
            <span>
              <ArrowUpToLine :size="13" />
              {{ fmt(d.traffic?.up_mbps, ' Mbps', 2) }}
            </span>
          </div>
        </div>
      </div>
      <div v-else class="empty">等待完成两轮采集后显示设备速率。</div>
    </article>
    <article class="panel traffic-panel">
      <div class="panel-heading">
        <div>
          <h2>近 {{ rangeLabel }}用量</h2>
          <p>{{ latest ? `小时汇总 · 最新记录 ${latest}` : '小时汇总' }}</p>
        </div>
        <nav class="range-switch" aria-label="统计范围">
          <button
            v-for="r in ranges"
            :key="r.key"
            :class="{ active: range === r.key }"
            :disabled="loading"
            @click="pick(r.key)"
          >
            {{ r.label }}
          </button>
        </nav>
      </div>
      <div v-if="error" class="alert">{{ error }}</div>
      <template v-else>
        <p v-if="truncated" class="table-note">
          这段时间的记录超过单次读取上限，仅统计已加载的最近部分；可缩短范围或稍后再试。
        </p>
        <div v-if="daily.length" class="traffic-list">
          <div v-for="d in daily" :key="d.mac" class="traffic-row traffic-usage">
            <div class="traffic-name">
              <b>{{ d.name }}</b>
              <small>
                {{ bytes(d.down_bytes + d.up_bytes) }} · 下 {{ bytes(d.down_bytes) }} / 上
                {{ bytes(d.up_bytes) }}
              </small>
              <i>
                <span
                  :style="{ width: ((d.down_bytes + d.up_bytes) / dailyMax) * 100 + '%' }"
                ></span>
              </i>
            </div>
          </div>
        </div>
        <div v-else-if="!loading" class="empty">
          暂无小时汇总记录。需软路由启用 nft-qos 监控，且部署后至少积累一个完整小时。
        </div>
      </template>
    </article>
  </section>
</template>
