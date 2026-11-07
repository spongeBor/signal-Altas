<script setup lang="ts">
import { computed, onMounted, onBeforeUnmount, ref } from 'vue'
import { Download, Search, ChevronDown, RotateCcw } from 'lucide-vue-next'
import { request, clock, fmt, snapshot, profiles } from '../composables/useAtlas'
import HistoryCharts from '../components/HistoryCharts.vue'
import ArchiveTopology from '../components/ArchiveTopology.vue'
import { ponLabel } from '../lib/ont'
import type { Device, Radio, Topology } from '../types'
interface ArchiveStatus {
  enabled: boolean
  detail_days: number
  summary_days: number
  earliest?: string
  latest?: string
  last_saved?: string
  error?: string
}
interface Entity {
  mac: string
  name: string
  ip?: string
}
// Query kinds return different, explicitly allowlisted telemetry projections.
type Row = Record<string, any> & { time: string }
interface Page {
  items: Row[]
  next_cursor?: string
  resolution: string
  kind: string
}
interface Replay {
  time: string
  devices: Device[]
  profiles: Row[]
  radios: Radio[]
  topology: Topology | null
}
const status = ref<ArchiveStatus | null>(null),
  entities = ref<Entity[]>([]),
  rows = ref<Row[]>([]),
  cursor = ref(''),
  error = ref(''),
  busy = ref(false),
  snapshotBusy = ref(false)
const detailOpen = ref(false),
  detailLimit = ref(200),
  replayHint = ref('')
function toggleDetails(e: Event) {
  detailOpen.value = (e.target as HTMLDetailsElement).open
}
const view = ref<'query' | 'replay'>('query')
const kind = ref('traffic'),
  resolution = ref('auto'),
  mac = ref(''),
  profile = ref(''),
  from = ref(''),
  to = ref(''),
  replayTime = ref(''),
  replay = ref<Replay | null>(null),
  replayError = ref('')
const executed = ref<{
  kind: string
  resolution: string
  from: string
  to: string
  mac: string
  profile: string
} | null>(null)
function local(t: number) {
  const d = new Date(t)
  return new Date(t - d.getTimezoneOffset() * 60000).toISOString().slice(0, 16)
}
function preset(hours: number) {
  to.value = local(Date.now() + 60000)
  from.value = local(Date.now() - hours * 3600000)
  if (['events', 'changes'].includes(kind.value)) resolution.value = 'raw'
  else if (hours > 24 * 2) resolution.value = 'auto'
}
function applyPreset(hours: number) {
  preset(hours)
  void search(false)
}
preset(24)
replayTime.value = local(Date.now())
const summary = computed(() => executed.value?.resolution === 'hour')
const selectedKind = computed(() => executed.value?.kind || kind.value)
const types = [
  ['traffic', '宽带流量'],
  ['device_traffic', '设备流量'],
  ['devices', '设备轨迹'],
  ['profiles', '光猫与路由状态'],
  ['radios', '无线环境'],
  ['ping', '网络时延'],
  ['events', '连接与采集事件'],
  ['changes', '名称与拓扑修改'],
]
const kindLabel = computed(() => types.find((t) => t[0] === selectedKind.value)?.[1] || '历史记录')
function chooseKind(value: string | undefined) {
  if (!value) return
  kind.value = value
  changedKind()
}
function changedKind() {
  cursor.value = ''
  if (['events', 'changes'].includes(kind.value)) resolution.value = 'raw'
  else resolution.value = 'auto'
  if (kind.value === 'devices' && !mac.value)
    mac.value =
      entities.value.find((d) => /iPhone/.test(d.name))?.mac || entities.value[0]?.mac || ''
  void search(false)
}
function queryValues() {
  let a = new Date(from.value),
    b = new Date(to.value)
  if (!Number.isFinite(a.getTime()) || !Number.isFinite(b.getTime()) || a >= b)
    throw new Error('请选择有效的开始、结束时间')
  if (b.getTime() - a.getTime() > 366 * 86400000) throw new Error('单次最多查询366天')
  const precision = ['events', 'changes'].includes(kind.value)
    ? 'raw'
    : resolution.value === 'auto'
      ? b.getTime() - a.getTime() > 48 * 3600000
        ? 'hour'
        : 'raw'
      : resolution.value
  if (precision === 'hour') {
    a = new Date(Math.floor(a.getTime() / 3600000) * 3600000)
    b = new Date(Math.ceil(b.getTime() / 3600000) * 3600000)
  }
  return {
    kind: kind.value,
    resolution: precision,
    from: a.toISOString(),
    to: b.toISOString(),
    mac: ['device_traffic', 'devices', 'events', 'changes'].includes(kind.value) ? mac.value : '',
    profile: ['profiles', 'radios', 'events'].includes(kind.value) ? profile.value : '',
  }
}
let generation = 0
async function search(append = false) {
  const version = ++generation
  busy.value = true
  error.value = ''
  try {
    const q = append ? executed.value! : queryValues()
    let next = append ? cursor.value : ''
    let result: Row[] = append ? [...rows.value] : []
    const initialLength = result.length
    do {
      const params = new URLSearchParams({
        kind: q.kind,
        resolution: q.resolution,
        from: q.from,
        to: q.to,
        limit: '2000',
      })
      if (q.mac) params.set('mac', q.mac)
      if (q.profile) params.set('profile_id', q.profile)
      if (next) params.set('cursor', next)
      const page = await request<Page>('/history?' + params)
      if (version !== generation) return
      result.push(...page.items)
      next = page.next_cursor || ''
    } while (next && result.length - initialLength < 20000)
    rows.value = result
    if (!append) detailLimit.value = 200
    cursor.value = next
    executed.value = q
  } catch (e) {
    if (version !== generation) return
    error.value = (e as Error).message
    if (!append) {
      rows.value = []
      executed.value = null
      cursor.value = ''
    }
  } finally {
    if (version === generation) busy.value = false
  }
}
async function initialize() {
  try {
    status.value = await request<ArchiveStatus>('/history/status')
    if (status.value.enabled) {
      entities.value = (await request<{ items: Entity[] }>('/history/devices')).items
      await search()
    }
  } catch (e) {
    error.value = (e as Error).message
  }
}
async function play(exactAt?: string) {
  snapshotBusy.value = true
  replayError.value = ''
  replay.value = null
  try {
    const at = new Date(exactAt || replayTime.value)
    if (!Number.isFinite(at.getTime())) throw new Error('请选择回放时间')
    replay.value = await request<Replay>(
      '/history/snapshot?' + new URLSearchParams({ at: exactAt || at.toISOString() }),
    )
  } catch (e) {
    replayError.value = (e as Error).message
  } finally {
    snapshotBusy.value = false
  }
}
function replayAt(at: string) {
  replayHint.value = summary.value
    ? '所选点是小时汇总的起点。这里仅回放该时刻附近的明细，不代表整小时；较早明细可能已过保留期。'
    : ''
  view.value = 'replay'
  replayTime.value = local(Date.parse(at))
  void play(at)
}
function exportRows() {
  const data = {
    query: executed.value,
    items: rows.value,
    next_cursor: cursor.value || undefined,
    exported_at: new Date().toISOString(),
  }
  const a = document.createElement('a'),
    url = URL.createObjectURL(
      new Blob([JSON.stringify(data, null, 2)], { type: 'application/json' }),
    )
  a.href = url
  a.download = 'signal-atlas-history-' + Date.now() + '.json'
  a.click()
  URL.revokeObjectURL(url)
}
function changeLabel(r: Row) {
  return r.kind === 'rename'
    ? `${r.before?.name || '未命名'} → ${r.after?.name}`
    : r.kind === 'topology'
      ? `拓扑保存：${r.after?.nodes?.length || 0}个节点，${r.after?.links?.length || 0}条关系`
      : '迁入时的设备名称基线'
}
const eventLabels: Record<string, string> = {
  association: '接入点变化',
  connection: '连接证据变化',
  collector: '采集状态',
  wan: 'WAN状态',
  pon: 'PON状态',
}
onMounted(initialize)
onBeforeUnmount(() => {
  generation++
})
</script>
<template>
  <div class="history-page">
    <div v-if="error" class="alert" role="alert">{{ error }}</div>
    <section class="panel history-intro">
      <div class="panel-heading">
        <div>
          <h2>历史归档</h2>
          <p v-if="status?.enabled">
            详细记录保留{{ status.detail_days }}天，小时汇总保留{{
              status.summary_days
            }}天；名称和拓扑修改单独保留。
          </p>
          <p v-else>尚未启用长期历史。开发时在项目 .env 配置应用专用的 PostgreSQL，再启动后端。</p>
        </div>
        <span class="tag" :class="{ dim: !status?.enabled }">
          {{ status?.enabled ? '已启用' : '未启用' }}
        </span>
      </div>
      <p v-if="status?.enabled" class="history-coverage">
        <span>
          最早可查询
          <b>{{ clock(status.earliest, true) }}</b>
        </span>
        <span>
          最近保存
          <b>
            {{ clock(snapshot?.archive?.last_saved || status.last_saved || status.latest, true) }}
          </b>
        </span>
      </p>
      <div v-if="status?.error" class="alert">{{ status.error }}</div>
    </section>
    <template v-if="status?.enabled">
      <nav class="history-tabs" aria-label="历史视图">
        <button
          :class="{ active: view === 'query' }"
          :aria-pressed="view === 'query'"
          @click="view = 'query'"
        >
          历史查询
        </button>
        <button
          :class="{ active: view === 'replay' }"
          :aria-pressed="view === 'replay'"
          @click="view = 'replay'"
        >
          状态回放
        </button>
      </nav>
      <template v-if="view === 'query'">
        <section class="panel history-query-panel">
          <nav class="history-kinds" aria-label="历史指标">
            <button
              v-for="t in types"
              :key="t[0]"
              :class="{ active: kind === t[0] }"
              :aria-pressed="kind === t[0]"
              @click="chooseKind(t[0])"
            >
              {{ t[1] }}
            </button>
          </nav>
          <form id="history-query-form" class="history-filters" @submit.prevent="search(false)">
            <div class="history-primary-fields">
              <label class="field">
                <span>开始时间</span>
                <input v-model="from" type="datetime-local" required />
              </label>
              <label class="field">
                <span>结束时间</span>
                <input v-model="to" type="datetime-local" required />
              </label>
              <label class="field">
                <span>精度</span>
                <select v-model="resolution" :disabled="['events', 'changes'].includes(kind)">
                  <option value="auto">自动（短期明细 / 长期小时）</option>
                  <option value="raw">详细记录</option>
                  <option value="hour">小时汇总</option>
                </select>
              </label>
            </div>
            <div v-if="kind !== 'traffic'" class="history-filter-actions">
              <div class="history-target-fields">
                <label
                  v-if="['device_traffic', 'devices', 'events', 'changes'].includes(kind)"
                  class="field history-device-filter"
                >
                  <span>设备</span>
                  <select v-model="mac">
                    <option value="">全部设备</option>
                    <option v-for="d in entities" :key="d.mac" :value="d.mac">
                      {{ d.name }} · {{ d.ip || d.mac }}
                    </option>
                  </select>
                </label>
                <label v-if="['profiles', 'radios', 'events'].includes(kind)" class="field">
                  <span>采集连接</span>
                  <select v-model="profile">
                    <option value="">全部连接</option>
                    <option v-for="p in profiles" :key="p.id" :value="p.id">{{ p.name }}</option>
                  </select>
                </label>
              </div>
            </div>
          </form>
          <div class="history-presets">
            <span>快捷范围</span>
            <button class="button subtle" @click="applyPreset(1)">最近1小时</button>
            <button class="button subtle" @click="applyPreset(24)">24小时</button>
            <button class="button subtle" @click="applyPreset(24 * 7)">7天</button>
            <button class="button subtle" @click="applyPreset(24 * 30)">30天</button>
            <button class="button subtle" @click="applyPreset(24 * 365)">1年</button>
            <button class="button primary" :disabled="busy" type="submit" form="history-query-form">
              <Search :size="15" />
              {{ busy ? '查询中…' : '查询历史' }}
            </button>
          </div>
          <p class="table-note">
            小时汇总按整点统计均值、峰值和样本数，不能还原某一秒的状态。长时间没有采集的数据会留空；未确认连接不等同设备离线。
          </p>
        </section>
        <section v-if="executed" class="panel history-results">
          <div class="panel-heading">
            <div>
              <h2>{{ kindLabel }}</h2>
              <p>
                {{ clock(executed.from, true) }} — {{ clock(executed.to, true) }} ·
                {{ summary ? '小时汇总' : '详细记录' }}
              </p>
            </div>
            <button class="button subtle" :disabled="!rows.length" @click="exportRows">
              <Download :size="15" />
              导出当前结果
            </button>
          </div>
          <p v-if="cursor" class="alert history-partial">
            当前为部分结果：已加载
            {{ rows.length.toLocaleString() }}
            条最近记录，图表尚未覆盖整个查询范围。可继续加载、缩短时间或选择单台设备。
          </p>
          <HistoryCharts
            :rows="rows"
            :kind="selectedKind"
            :hourly="summary"
            :from="executed.from"
            :to="executed.to"
            @select="replayAt"
          />
          <details class="history-details" @toggle="toggleDetails">
            <summary>
              数据明细
              <span>{{ rows.length.toLocaleString() }} 条已加载记录</span>
            </summary>
            <div v-if="detailOpen" class="table-scroll history-scroll">
              <table class="history-table">
                <thead>
                  <tr>
                    <th>时间</th>
                    <template v-if="selectedKind === 'traffic'">
                      <th>下载{{ summary ? '均值' : '' }}</th>
                      <th>上传{{ summary ? '均值' : '' }}</th>
                      <th v-if="summary">下载 / 上传峰值</th>
                    </template>
                    <template v-else-if="selectedKind === 'device_traffic'">
                      <th>设备</th>
                      <th>下载{{ summary ? '累计' : '' }}</th>
                      <th>上传{{ summary ? '累计' : '' }}</th>
                      <th>{{ summary ? '下载 / 上传峰值' : '下载 / 上传速率' }}</th>
                    </template>
                    <template v-else-if="selectedKind === 'devices'">
                      <th>设备</th>
                      <th>连接证据</th>
                      <th>{{ summary ? '小时内AP / 频段' : '接入点 / 频段' }}</th>
                      <th>AP 收信号{{ summary ? '均值' : '' }}</th>
                      <th>AP → 设备协商</th>
                    </template>
                    <template v-else-if="selectedKind === 'profiles'">
                      <th>设备</th>
                      <th>采集状态</th>
                      <th>CPU{{ summary ? '均值' : '' }}</th>
                      <th>芯片温度{{ summary ? '均值' : '' }}</th>
                      <th v-if="!summary">广域网 / 光口状态</th>
                      <th v-if="!summary">光模块明细</th>
                    </template>
                    <template v-else-if="selectedKind === 'radios'">
                      <th>AP / 接口</th>
                      <th>网络</th>
                      <th>信道 / 带宽</th>
                      <th>利用率{{ summary ? '均值' : '' }}</th>
                      <th>关联数{{ summary ? '均值' : '' }}</th>
                    </template>
                    <template v-else-if="selectedKind === 'ping'">
                      <th>目标</th>
                      <th>平均时延</th>
                      <th>峰值</th>
                      <th>丢包</th>
                    </template>
                    <template v-else-if="selectedKind === 'events'">
                      <th>类别</th>
                      <th>设备</th>
                      <th>记录</th>
                    </template>
                    <template v-else>
                      <th>类别</th>
                      <th>对象</th>
                      <th>修改内容</th>
                    </template>
                    <th v-if="summary">样本</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-for="(r, i) in rows.slice(0, detailLimit)" :key="r.time + '-' + i">
                    <td class="mono">{{ clock(r.time, true) }}</td>
                    <template v-if="selectedKind === 'traffic'">
                      <td>{{ fmt(r.down_mbps, ' Mbps', 1) }}</td>
                      <td>{{ fmt(r.up_mbps, ' Mbps', 1) }}</td>
                      <td v-if="summary">
                        {{ fmt(r.down_peak, '', 1) }} / {{ fmt(r.up_peak, ' Mbps', 1) }}
                      </td>
                    </template>
                    <template v-else-if="selectedKind === 'device_traffic'">
                      <td>
                        <b>{{ r.name }}</b>
                        <small class="mono">{{ r.ip || r.mac }}</small>
                      </td>
                      <td>{{ fmt(r.down_bytes / 1024 / 1024, ' MB', 1) }}</td>
                      <td>{{ fmt(r.up_bytes / 1024 / 1024, ' MB', 1) }}</td>
                      <td>
                        {{ fmt(summary ? r.down_peak : r.down_mbps, '', 2) }} /
                        {{ fmt(summary ? r.up_peak : r.up_mbps, ' Mbps', 2) }}
                      </td>
                    </template>
                    <template v-else-if="selectedKind === 'devices'">
                      <td>
                        <b>{{ r.name }}</b>
                        <small class="mono">{{ r.ip || r.mac }}</small>
                      </td>
                      <td>
                        {{
                          summary
                            ? `确认连接 ${r.confirmed_samples}/${r.samples}`
                            : r.online
                              ? r.seen_via
                              : '本轮未确认连接'
                        }}
                      </td>
                      <td>
                        {{ summary ? r.aps?.join(' / ') || '无AP记录' : r.ap_name || '无AP关联' }}
                        <small>
                          {{ summary ? r.bands?.join(' / ') : r.band
                          }}{{ !summary && r.width_mhz ? ' · ' + r.width_mhz + 'MHz' : '' }}
                        </small>
                      </td>
                      <td>
                        {{ fmt(r.rssi, ' dBm', 1) }}
                        <small v-if="summary">
                          {{ fmt(r.rssi_min, '', 0) }} ～ {{ fmt(r.rssi_max, ' dBm', 0) }}
                        </small>
                      </td>
                      <td>{{ fmt(r.tx_mbps, ' Mbps', 1) }}</td>
                    </template>
                    <template v-else-if="selectedKind === 'profiles'">
                      <td>{{ r.name }}</td>
                      <td>
                        {{
                          summary
                            ? `成功 ${r.ok_samples}/${r.samples}`
                            : r.status === 'ok'
                              ? '正常'
                              : r.status === 'needs_auth'
                                ? '等待凭据'
                                : '采集失败'
                        }}
                      </td>
                      <td>
                        {{ fmt(r.cpu_percent, '%', 1) }}
                        <small v-if="summary">峰值 {{ fmt(r.cpu_peak, '%', 1) }}</small>
                      </td>
                      <td>
                        {{ fmt(r.chip_temperature, '℃', 1) }}
                        <small v-if="summary">峰值 {{ fmt(r.temperature_peak, '℃', 1) }}</small>
                      </td>
                      <td v-if="!summary">
                        {{ r.wan_up == null ? '—' : r.wan_up ? '广域网已连接' : '广域网未连接' }} /
                        {{ r.pon_state ? ponLabel(r.pon_state) : '—' }}
                      </td>
                      <td v-if="!summary">
                        <small v-if="r.rx_power_dbm != null || r.tx_power_dbm != null">
                          {{
                            r.optical_source === 'zte_qoe_v2' ? '厂家业务口径' : '底层诊断 · 旧口径'
                          }}
                        </small>
                        收光 {{ fmt(r.rx_power_dbm, ' dBm', 1) }} / 发光
                        {{ fmt(r.tx_power_dbm, ' dBm', 1) }}
                        <small>
                          模块 {{ fmt(r.optical_temperature_c, '°C', 1) }} ·
                          {{ fmt(r.optical_voltage_v, ' V', 4) }} ·
                          {{ fmt(r.optical_bias_ma, ' mA', 3) }}
                        </small>
                      </td>
                    </template>
                    <template v-else-if="selectedKind === 'radios'">
                      <td>
                        {{ r.ap_name || r.ap_id }}
                        <small>{{ r.interface }}</small>
                      </td>
                      <td>
                        {{ r.ssid }}
                        <small>{{ r.band }}</small>
                      </td>
                      <td>
                        {{
                          summary
                            ? '汇总不保留逐次信道'
                            : `${r.channel ?? '—'} / ${r.width_mhz ?? '—'}MHz`
                        }}
                      </td>
                      <td>
                        {{ fmt(r.utilization, '%', 1) }}
                        <small v-if="summary">峰值 {{ fmt(r.utilization_peak, '%', 1) }}</small>
                      </td>
                      <td>{{ fmt(r.clients, '', 1) }}</td>
                    </template>
                    <template v-else-if="selectedKind === 'ping'">
                      <td>
                        <b>{{ r.target }}</b>
                        <small class="mono">{{ r.host }}</small>
                      </td>
                      <td>{{ fmt(r.rtt_avg, ' ms', 2) }}</td>
                      <td>{{ fmt(summary ? r.rtt_peak : r.rtt_max, ' ms', 2) }}</td>
                      <td>{{ fmt(r.loss, '%', 1) }}</td>
                    </template>
                    <template v-else-if="selectedKind === 'events'">
                      <td>{{ eventLabels[r.kind] || r.kind }}</td>
                      <td>{{ r.name || r.mac || r.profile_id }}</td>
                      <td>{{ r.text }}</td>
                    </template>
                    <template v-else>
                      <td>
                        {{
                          r.kind === 'rename'
                            ? '设备改名'
                            : r.kind === 'topology'
                              ? '拓扑保存'
                              : '迁入基线'
                        }}
                      </td>
                      <td class="mono">{{ r.subject }}</td>
                      <td>
                        {{ changeLabel(r) }}
                        <details v-if="r.kind === 'topology' && r.after?.nodes">
                          <summary>查看这次保存的拓扑</summary>
                          <ArchiveTopology :topology="r.after" />
                        </details>
                      </td>
                    </template>
                    <td v-if="summary">{{ r.samples }}</td>
                  </tr>
                </tbody>
              </table>
              <div v-if="!rows.length && !busy" class="empty">
                此范围没有记录。更早的数据不会自动补造；超过明细期限请切换小时汇总。
              </div>
            </div>
            <div v-if="detailOpen && rows.length > detailLimit" class="history-pagination">
              <span>明细已展示 {{ detailLimit }} / {{ rows.length }} 条</span>
              <button class="button subtle" @click="detailLimit += 200">再展示 200 条明细</button>
            </div>
          </details>
          <div class="history-pagination">
            <span>
              已加载 {{ rows.length.toLocaleString() }} 条{{ cursor ? '，还有更多记录' : '' }}
            </span>
            <button v-if="cursor" class="button subtle" :disabled="busy" @click="search(true)">
              <ChevronDown :size="15" />
              继续加载
            </button>
          </div>
        </section>
      </template>
      <section v-else class="panel history-replay">
        <div class="panel-heading">
          <div>
            <h2>按时间回放状态</h2>
            <p>显示所选时间之前5分钟内最近一轮明细，保留当时的设备名称与证据。</p>
          </div>
        </div>
        <form class="history-replay-form" @submit.prevent="play()">
          <label class="field">
            <span>回放时间</span>
            <input v-model="replayTime" type="datetime-local" required />
          </label>
          <button class="button primary" :disabled="snapshotBusy">
            <RotateCcw :size="15" />
            {{ snapshotBusy ? '读取中…' : '查看当时状态' }}
          </button>
        </form>
        <p v-if="replayHint" class="hint">{{ replayHint }}</p>
        <div v-if="replayError" class="alert" role="alert">{{ replayError }}</div>
        <template v-if="replay">
          <p class="hint">
            实际记录时间：{{ clock(replay.time, true) }}。这里展示历史，不影响当前网络。
          </p>
          <ArchiveTopology v-if="replay.topology" :topology="replay.topology" />
          <div class="history-replay-profiles">
            <div v-for="p in replay.profiles" :key="p.id">
              <b>{{ p.name }}</b>
              <span>
                {{
                  p.status === 'ok'
                    ? '采集正常'
                    : p.status === 'needs_auth'
                      ? '等待凭据'
                      : '采集失败'
                }}
              </span>
              <small>
                CPU {{ fmt(p.cpu_percent, '%', 1) }} · 芯片温度
                {{ fmt(p.chip_temperature, '℃', 1) }}
              </small>
              <small v-if="p.optical_temperature_c != null || p.rx_power_dbm != null">
                {{ p.optical_source === 'zte_qoe_v2' ? '厂家业务口径' : '底层诊断 · 旧口径' }} ·
                光模块 {{ fmt(p.optical_temperature_c, '°C', 1) }} · 收光
                {{ fmt(p.rx_power_dbm, ' dBm', 2) }} · 发光 {{ fmt(p.tx_power_dbm, ' dBm', 2) }}
              </small>
            </div>
          </div>
          <div class="table-scroll history-scroll">
            <table>
              <thead>
                <tr>
                  <th>当时设备名</th>
                  <th>当时IP</th>
                  <th>连接证据</th>
                  <th>AP / 频段</th>
                  <th>AP 收信号</th>
                </tr>
              </thead>
              <tbody>
                <tr v-for="d in replay.devices" :key="d.mac">
                  <td>
                    {{ d.name }}
                    <small class="mono">{{ d.mac }}</small>
                  </td>
                  <td>{{ d.ip || '—' }}</td>
                  <td>{{ d.online ? d.seen_via : '本轮未确认连接' }}</td>
                  <td>{{ d.ap_name || '—' }} / {{ d.band || '—' }}</td>
                  <td>{{ fmt(d.rssi, ' dBm', 0) }}</td>
                </tr>
              </tbody>
            </table>
          </div>
        </template>
      </section>
    </template>
  </div>
</template>
