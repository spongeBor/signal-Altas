<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Search, ChevronRight, Wifi, Cable, Trash2 } from 'lucide-vue-next'
import type { Device } from '../types'
import {
  devices,
  fmt,
  clock,
  strength,
  signalColor,
  mutate,
  pending,
  request,
} from '../composables/useAtlas'
import Drawer from '../components/Drawer.vue'
const query = ref(''),
  filter = ref('all'),
  selected = ref(''),
  name = ref('')
const lastSeen = (value?: string) => {
  if (!value) return ''
  const d = new Date(value)
  if (!Number.isFinite(d.getTime())) return ''
  const sameDay = d.toDateString() === new Date().toDateString()
  return d.toLocaleString('zh-CN', {
    hour12: false,
    ...(sameDay ? {} : { month: '2-digit', day: '2-digit' }),
    hour: '2-digit',
    minute: '2-digit',
  })
}
interface Group {
  key: string
  links: Device[]
  primary: Device
  name: string
  online: boolean
  ips: string[]
}
const groupKey = (d: Device) => d.physical_device_id || d.mac
// One physical device can expose several MACs (wired NIC, Wi-Fi, rotation).
// Strip the shared suffix so "PS5（有线）" and "PS5（Wi-Fi）" read as "PS5".
function groupName(links: Device[]) {
  const names = links.map((d) => (d.name || '').trim()).filter(Boolean)
  if (links.length < 2 || names.length !== links.length) return links[0].name || ''
  let prefix = names[0]
  for (const n of names.slice(1)) {
    let i = 0
    while (i < prefix.length && i < n.length && prefix[i] === n[i]) i++
    prefix = prefix.slice(0, i)
  }
  prefix = prefix.replace(/[\s（(·:：-]+$/u, '').trim()
  return prefix.length >= 2 ? prefix : links[0].name || ''
}
const groups = computed<Group[]>(() => {
  const map = new Map<string, Device[]>()
  for (const d of devices.value) {
    const key = groupKey(d)
    const list = map.get(key)
    if (list) list.push(d)
    else map.set(key, [d])
  }
  const out: Group[] = []
  for (const [key, all] of map) {
    const links = [...all].sort(
      (a, b) => Number(b.online) - Number(a.online) || Number(!!b.ap_id) - Number(!!a.ap_id),
    )
    out.push({
      key,
      links,
      primary: links[0],
      name: groupName(links),
      online: links.some((d) => d.online),
      ips: [...new Set(links.map((d) => d.ip).filter((ip): ip is string => !!ip))],
    })
  }
  return out
})
type Uptime = { samples: number; confirmed: number }
const uptime7 = ref<Record<string, Uptime>>({})
const uptime30 = ref<Record<string, Uptime>>({})
async function loadUptime(days: number) {
  try {
    const res = await request<{ items: { mac: string; samples: number; confirmed: number }[] }>(
      `/history/uptime?days=${days}`,
    )
    const map: Record<string, Uptime> = {}
    for (const row of res.items) map[row.mac] = { samples: row.samples, confirmed: row.confirmed }
    if (days === 7) uptime7.value = map
    else uptime30.value = map
  } catch {
    /* 在线率是补充信息，读取失败不影响设备列表 */
  }
}
function rate(map: Record<string, Uptime>, links: { mac: string }[]) {
  let samples = 0,
    confirmed = 0
  for (const l of links) {
    const u = map[l.mac]
    if (!u) continue
    samples += u.samples
    confirmed += u.confirmed
  }
  if (!samples) return null
  return Math.round((confirmed / samples) * 1000) / 10
}
const groupRate = (g: { links: { mac: string }[]; primary: { ap_id?: string } }) =>
  g.primary.ap_id ? rate(uptime7.value, g.links) : null
const bandStats = computed(() => {
  const online = devices.value.filter((d) => d.online && d.ap_id && d.band)
  const counts = new Map<string, number>()
  for (const d of online) counts.set(d.band!, (counts.get(d.band!) || 0) + 1)
  const total = online.length || 1
  return [...counts.entries()]
    .sort((a, b) => b[1] - a[1])
    .map(([band, count]) => ({ band, count, pct: Math.round((count / total) * 100) }))
})
// A device that already hears the AP well but stays on 2.4 GHz can usually move
// to 5 GHz without losing range.
const suggestCats = new Set(['手机', '平板', '电脑', '观影设备', '游戏设备'])
const suggest5g = computed(() =>
  devices.value
    .filter(
      (d) =>
        d.online &&
        d.ap_id &&
        d.band === '2.4 GHz' &&
        d.rssi != null &&
        d.rssi >= -60 &&
        suggestCats.has(d.category || ''),
    )
    .sort((a, b) => (b.rssi ?? -99) - (a.rssi ?? -99))
    .slice(0, 6),
)
const filtered = computed(() =>
  groups.value.filter((g) => {
    const text = g.links
      .map((d) => `${d.name || ''} ${d.ip || ''} ${d.mac} ${d.category || ''}`)
      .join(' ')
      .toLowerCase()
    return (
      text.includes(query.value.toLowerCase()) &&
      (filter.value === 'all' ||
        (filter.value === 'wireless' && g.links.some((d) => d.ap_id)) ||
        (filter.value === 'online' && g.online) ||
        (filter.value === 'unconfirmed' && !g.online))
    )
  }),
)
type UsageRow = { time: string; down_bytes: number; up_bytes: number }
const usageRange = ref<'7d' | '30d'>('7d')
const usageRows = ref<UsageRow[]>([])
const usageLoading = ref(false)
const usageError = ref('')
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
const usageDaily = computed(() => {
  const byDay = new Map<string, { day: string; down: number; up: number }>()
  for (const r of usageRows.value) {
    const d = new Date(r.time)
    if (!Number.isFinite(d.getTime())) continue
    const key = d.toLocaleDateString('sv-SE')
    const cur = byDay.get(key) || { day: key, down: 0, up: 0 }
    cur.down += r.down_bytes || 0
    cur.up += r.up_bytes || 0
    byDay.set(key, cur)
  }
  return [...byDay.values()]
    .sort((a, b) => a.day.localeCompare(b.day))
    .map((d) => ({ ...d, total: d.down + d.up }))
})
const usageMax = computed(() => Math.max(1, ...usageDaily.value.map((d) => d.total)))
const usageTotal = computed(() => usageDaily.value.reduce((sum, d) => sum + d.total, 0))
const usagePeak = computed(() =>
  usageDaily.value.reduce<{ day: string; total: number } | undefined>(
    (best, d) => (!best || d.total > best.total ? d : best),
    undefined,
  ),
)
async function loadUsage() {
  const g = selectedGroup.value
  if (!g) return
  usageLoading.value = true
  usageError.value = ''
  try {
    const hours = usageRange.value === '7d' ? 24 * 7 : 24 * 30
    const to = new Date()
    const from = new Date(to.getTime() - hours * 3600000)
    const merged = new Map<string, UsageRow>()
    for (const link of g.links.slice(0, 2)) {
      if (!link.mac) continue
      const q = new URLSearchParams({
        kind: 'device_traffic',
        resolution: 'hour',
        mac: link.mac,
        from: from.toISOString(),
        to: to.toISOString(),
        limit: '2000',
      })
      const res = await request<{ items: UsageRow[] }>('/history?' + q)
      for (const r of res.items || []) {
        const cur = merged.get(r.time) || { time: r.time, down_bytes: 0, up_bytes: 0 }
        cur.down_bytes += r.down_bytes || 0
        cur.up_bytes += r.up_bytes || 0
        merged.set(r.time, cur)
      }
    }
    usageRows.value = [...merged.values()]
  } catch (e) {
    usageError.value = (e as Error).message
  } finally {
    usageLoading.value = false
  }
}
onMounted(() => {
  void loadUptime(7)
  void loadUptime(30)
})
watch(selected, () => {
  usageRows.value = []
  if (selected.value) void loadUsage()
})
watch(usageRange, () => void loadUsage())
type SortKey = 'default' | 'name' | 'rssi' | 'rate' | 'uptime'
const sortKey = ref<SortKey>('default')
const sortDir = ref<1 | -1>(1)
const sortLabels: { key: SortKey; label: string }[] = [
  { key: 'default', label: '默认顺序' },
  { key: 'name', label: '设备名称' },
  { key: 'rssi', label: 'AP 收信号' },
  { key: 'rate', label: '协商速率' },
  { key: 'uptime', label: '在线率' },
]
function sortBy(key: Exclude<SortKey, 'default'>) {
  if (sortKey.value === key) {
    sortDir.value = sortDir.value === 1 ? -1 : 1
  } else {
    sortKey.value = key
    sortDir.value = key === 'name' ? 1 : -1
  }
}
function pickSort(value: SortKey) {
  sortKey.value = value
  sortDir.value = value === 'name' || value === 'rssi' ? (value === 'name' ? 1 : -1) : -1
}
const sorted = computed(() => {
  const list = [...filtered.value]
  if (sortKey.value === 'default') return list
  const value = (g: (typeof list)[number]) => {
    switch (sortKey.value) {
      case 'name':
        return g.name
      case 'rssi':
        return g.primary.rssi
      case 'rate':
        return g.primary.tx_mbps
      default:
        return groupRate(g)
    }
  }
  list.sort((a, b) => {
    const av = value(a),
      bv = value(b)
    // 缺失值始终排在最后，不管升序还是降序。
    if (av == null && bv == null) return 0
    if (av == null) return 1
    if (bv == null) return -1
    if (typeof av === 'string' || typeof bv === 'string')
      return String(av).localeCompare(String(bv), 'zh-CN') * sortDir.value
    return (av - bv) * sortDir.value
  })
  return list
})
const selectedGroup = computed(() => groups.value.find((g) => g.key === selected.value))
const current = computed(() => selectedGroup.value?.primary)
const otherAddresses = computed(() => {
  const d = current.value
  return d?.addresses?.filter((x) => x !== d.ip) || []
})
const linkLabel = (d: Device) => {
  if (d.ap_id) return `无线 · ${d.band || ''}${d.width_mhz ? ' ' + d.width_mhz + 'MHz' : ''}`
  return d.online ? '有线 / 邻居可达' : '未确认连接'
}
function open(key: string) {
  selected.value = key
  name.value = selectedGroup.value?.name || ''
}
async function forgetDevice(mac: string) {
  if (!window.confirm('从台账移除这条记录？历史采样会保留。')) return
  if (await mutate('/entities/forget', 'POST', { mac }, '已从台账移除')) selected.value = ''
}
async function rename() {
  const g = selectedGroup.value
  if (!g) return
  let saved = true
  for (const d of g.links) {
    saved = (await mutate('/labels', 'PUT', { mac: d.mac, name: name.value }, '设备名称已保存')) && saved
  }
  if (saved) selected.value = ''
}
</script>
<template>
  <section v-if="bandStats.length" class="panel band-panel">
    <div class="panel-heading">
      <div>
        <h2>频段分布</h2>
        <p>在线无线设备按频段统计；信号好却仍挂在 2.4G 的会单独列出来。</p>
      </div>
      <span class="pill">{{ bandStats.reduce((n, b) => n + b.count, 0) }} 台无线</span>
    </div>
    <div class="band-bars">
      <div v-for="b in bandStats" :key="b.band" class="band-bar">
        <span>{{ b.band }}</span>
        <i><b :style="{ width: b.pct + '%' }"></b></i>
        <small>{{ b.count }} 台</small>
      </div>
    </div>
    <div v-if="suggest5g.length" class="band-suggest">
      <b>建议迁到 5G（信号强、仍在 2.4G 的电脑/手机/平板/影音设备）</b>
      <span v-for="d in suggest5g" :key="d.mac">
        {{ d.name || d.mac }} · {{ d.rssi }} dBm
      </span>
    </div>
    <p v-else class="table-note">当前没有需要迁移的电脑/手机/平板/影音设备。</p>
  </section>
  <section class="panel">
    <div class="panel-heading">
      <div>
        <h2>设备与连接</h2>
        <p>按 MAC 合并租约、邻居与 AP 数据；同一实体设备的多条链路合并为一条记录。</p>
      </div>
      <span class="pill">{{ filtered.length }} 条记录</span>
    </div>
    <div class="table-toolbar">
      <label class="search">
        <Search :size="16" />
        <input v-model="query" placeholder="搜索名称、IP、MAC或分类" aria-label="搜索设备" />
      </label>
      <select v-model="filter" aria-label="筛选设备">
        <option value="all">全部记录</option>
        <option value="online">可确认在线</option>
        <option value="wireless">无线关联</option>
        <option value="unconfirmed">未确认连接</option>
      </select>
      <select :value="sortKey" aria-label="排序方式" @change="pickSort(($event.target as HTMLSelectElement).value as SortKey)">
        <option v-for="s in sortLabels" :key="s.key" :value="s.key">{{ s.label }}</option>
      </select>
    </div>
    <div class="table-scroll">
      <table class="device-table">
        <thead>
          <tr>
            <th>
              <button class="sort-head" @click="sortBy('name')">
                设备<span v-if="sortKey === 'name'">{{ sortDir === 1 ? '↑' : '↓' }}</span>
              </button>
            </th>
            <th>IP 地址</th>
            <th>当前连接</th>
            <th>
              <button class="sort-head" @click="sortBy('rssi')">
                AP 收信号<span v-if="sortKey === 'rssi'">{{ sortDir === 1 ? '↑' : '↓' }}</span>
              </button>
            </th>
            <th>
              <button class="sort-head" @click="sortBy('rate')">
                AP → 设备协商<span v-if="sortKey === 'rate'">{{ sortDir === 1 ? '↑' : '↓' }}</span>
              </button>
            </th>
            <th>状态</th>
            <th>
              <button class="sort-head" @click="sortBy('uptime')">
                在线率<span v-if="sortKey === 'uptime'">{{ sortDir === 1 ? '↑' : '↓' }}</span>
              </button>
            </th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          <tr
            v-for="g in sorted"
            :key="g.key"
            tabindex="0"
            @click="open(g.key)"
            @keydown.enter="open(g.key)"
          >
            <td data-label="设备">
              <b>{{ g.name }}</b>
              <small class="mono">{{ g.primary.mac }}</small>
              <small v-if="g.links.length > 1" class="mono">{{ g.links.length }} 条链路</small>
            </td>
            <td class="mono" data-label="IP">
              {{ g.ips[0] || '—' }}
              <small v-if="g.ips.length > 1" class="mono">+{{ g.ips.length - 1 }}</small>
            </td>
            <td data-label="连接">
              <span class="connection-type">
                <Wifi v-if="g.primary.ap_id" :size="14" />
                <Cable v-else-if="g.primary.online" :size="14" />
                {{ g.primary.ap_name || g.primary.seen_via }}
              </span>
              <small v-if="g.primary.ap_id">
                {{
                  [
                    g.primary.ssid,
                    g.primary.band,
                    g.primary.width_mhz ? g.primary.width_mhz + 'MHz' : '',
                    g.primary.wifi,
                    g.primary.mlo ? (g.primary.mlo_links ? `MLO ${g.primary.mlo_links} 链路` : 'MLO') : '',
                  ]
                    .filter(Boolean)
                    .join(' · ')
                }}
              </small>
              <small v-else>{{ g.primary.online ? '有线接入' : '无实时无线关联' }}</small>
            </td>
            <td data-label="信号">
              <span
                class="signal-value"
                :style="{ color: g.primary.rssi != null ? signalColor(g.primary.rssi) : undefined }"
              >
                {{ fmt(g.primary.rssi, ' dBm') }}
              </span>
              <small>{{ strength(g.primary.rssi) }}</small>
            </td>
            <td data-label="协商">{{ fmt(g.primary.tx_mbps, ' Mbps', 1) }}</td>
            <td data-label="状态">
              <span class="tag" :class="{ dim: !g.online }">
                {{
                  g.primary.ap_id
                    ? '无线关联'
                    : g.online
                      ? g.primary.seen_via.includes('管理')
                        ? '管理口应答'
                        : '邻居可达'
                      : g.primary.last_confirmed_at
                        ? '间歇在线'
                        : '待确认'
                }}
              </span>
              <small v-if="!g.online && g.primary.last_confirmed_at" class="last-confirmed">
                最近 {{ lastSeen(g.primary.last_confirmed_at) }}
              </small>
            </td>
            <td data-label="在线率">
              <span v-if="groupRate(g) != null">{{ groupRate(g) }}%</span>
              <small v-else class="muted">—</small>
            </td>
            <td class="row-chevron"><ChevronRight :size="14" /></td>
          </tr>
        </tbody>
      </table>
      <div v-if="!filtered.length" class="empty">没有匹配的设备</div>
    </div>
    <p class="table-note">
      协商速率不是实际网速。没有本轮应答的旧租约或台账记录，不会被当作当前在线；电池类设备只在唤醒时
      短暂关联，会显示“间歇在线”并给出最近一次确认时间；同一实体设备的多个 MAC 会合并为一条记录。
    </p>
  </section>
  <Drawer v-if="current" @close="selected = ''">
    <div class="eyebrow">DEVICE DETAILS</div>
    <h2>{{ selectedGroup?.name }}</h2>
    <p class="muted mono">{{ current.mac }}</p>
    <div class="detail-list">
      <div>
        <span>IP地址</span>
        <b>{{ current.ip || '未获取当前地址' }}</b>
      </div>
      <div>
        <span>连接证据</span>
        <b>{{ current.seen_via }}</b>
      </div>
      <div>
        <span>接入点</span>
        <b>{{ current.ap_name || '无实时AP关联' }}</b>
      </div>
      <div>
        <span>MLO 多链路</span>
        <b>{{ current.mlo ? `是（${current.mlo_links || 1} 条链路）` : '否' }}</b>
      </div>
      <div>
        <span>Wi-Fi 版本</span>
        <b>{{ current.wifi || (current.ap_id ? '—' : '有线接入') }}</b>
      </div>
      <div>
        <span>Wi-Fi 名称</span>
        <b>{{ current.ssid || (current.online ? '有线接入' : '—') }}</b>
      </div>
      <div>
        <span>频段 / 带宽</span>
        <b>{{ current.band || '—' }} / {{ fmt(current.width_mhz, 'MHz') }}</b>
      </div>
      <div>
        <span>AP 收信号（设备→AP）</span>
        <b>{{ fmt(current.rssi, 'dBm') }}</b>
      </div>
      <div>
        <span>AP → 设备协商</span>
        <b>{{ fmt(current.tx_mbps, 'Mbps', 1) }}</b>
      </div>
      <div>
        <span>设备 → AP协商</span>
        <b>{{ fmt(current.rx_mbps, 'Mbps', 1) }}</b>
      </div>
      <div>
        <span>采样时间</span>
        <b>{{ clock(current.collected_at, true) }}</b>
      </div>
      <div v-if="current.last_confirmed_at">
        <span>最近确认在线</span>
        <b>{{ clock(current.last_confirmed_at, true) }}</b>
      </div>
      <div v-if="selectedGroup?.primary.ap_id && rate(uptime7, selectedGroup.links) != null">
        <span>近 7 天在线率</span>
        <b>{{ rate(uptime7, selectedGroup.links) }}%</b>
      </div>
      <div v-if="selectedGroup?.primary.ap_id && rate(uptime30, selectedGroup.links) != null">
        <span>近 30 天在线率</span>
        <b>{{ rate(uptime30, selectedGroup.links) }}%</b>
      </div>
      <div v-if="current.category">
        <span>分类</span>
        <b>{{ current.category }}</b>
      </div>
    </div>
    <template v-if="selectedGroup && selectedGroup.links.length > 1">
      <h3>物理链路</h3>
      <ul class="link-list">
        <li v-for="d in selectedGroup.links" :key="d.mac">
          <div>
            <b>{{ d.name || d.mac }}</b>
            <small class="mono">{{ d.mac }}</small>
          </div>
          <div class="link-meta">
            <span class="tag" :class="{ dim: !d.online }">{{ d.online ? '在线' : '未在线' }}</span>
            <span>{{ linkLabel(d) }}</span>
            <span class="mono">{{ d.ip || '—' }}</span>
            <span v-if="d.ap_id">{{ d.ssid }} · {{ fmt(d.rssi, ' dBm') }} · {{ fmt(d.tx_mbps, ' Mbps', 1) }}</span>
          </div>
        </li>
      </ul>
    </template>
    <p v-if="current.rssi != null" class="hint">
      这是 AP 收到该设备的信号。设备自己收到的信号（AP→设备）只能在设备终端查看，通常比这个高几 dB。
    </p>
    <p v-if="current.addresses && current.addresses.length > 1" class="hint mono">
      其他地址：{{ otherAddresses.join(' / ') }}
    </p>
    <h3>近期流量</h3>
    <nav class="range-switch" aria-label="流量范围">
      <button :class="{ active: usageRange === '7d' }" @click="usageRange = '7d'">近 7 天</button>
      <button :class="{ active: usageRange === '30d' }" @click="usageRange = '30d'">近 30 天</button>
    </nav>
    <p v-if="usageError" class="alert">{{ usageError }}</p>
    <div v-else-if="usageLoading" class="muted">读取中…</div>
    <template v-else-if="usageDaily.length">
      <div class="usage-bars">
        <div
          v-for="d in usageDaily"
          :key="d.day"
          class="usage-bar"
          :title="`${d.day}  ${bytes(d.total)}`"
        >
          <i :style="{ height: (d.total / usageMax) * 100 + '%' }"></i>
        </div>
      </div>
      <p class="hint">
        合计 {{ bytes(usageTotal) }} · 日均 {{ bytes(usageTotal / usageDaily.length) }}
        <template v-if="usagePeak">
          · 最高 {{ usagePeak.day.slice(5) }}（{{ bytes(usagePeak.total) }}）
        </template>
      </p>
    </template>
    <p v-else class="muted">
      近 {{ usageRange === '7d' ? 7 : 30 }} 天没有该设备的流量记录。
    </p>
    <h3>设备名称</h3>
    <label class="field">
      <span>便于识别的名称</span>
      <input v-model="name" maxlength="80" @keydown.enter="rename" />
    </label>
    <button class="button primary" :disabled="pending || !name.trim()" @click="rename">
      保存名称
    </button>
    <template v-if="!current.online">
      <h3>台账</h3>
      <p class="hint compact">
        确认这台设备不再使用，可以只移除台账记录；历史采样仍然保留，设备再次出现会重新登记。
      </p>
      <button class="button subtle danger" :disabled="pending" @click="forgetDevice(current.mac)">
        <Trash2 :size="14" />
        从台账移除
      </button>
    </template>
  </Drawer>
</template>
