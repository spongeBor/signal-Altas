<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Plus, Trash2, Save, Search, ShieldCheck, Database, KeyRound, Sparkles } from 'lucide-vue-next'
import type { Profile, Candidate } from '../types'
import { snapshot, devices, profiles, radios, mutate, request, pending, toast } from '../composables/useAtlas'
const hostkeys = ref<{ host: string; fingerprint: string; key_type: string }[]>([])
async function loadHostkeys() {
  try {
    const r = await request<{ pending: { host: string; fingerprint: string; key_type: string }[] }>('/hostkeys')
    hostkeys.value = r.pending || []
  } catch {
    hostkeys.value = []
  }
}
async function trustHost(host: string) {
  await mutate('/hostkeys/trust', 'POST', { host }, '已信任指纹，下一轮采集生效')
  await loadHostkeys()
}
onMounted(loadHostkeys)
watch(
  () => snapshot.value?.state?.last_updated,
  () => loadHostkeys(),
)
const drafts = ref<Profile[]>([]),
  interval = ref(30),
  dirty = ref(false),
  scanning = ref(false),
  candidates = ref<Candidate[]>([]),
  topologyText = ref('')
watch(
  () => snapshot.value?.config,
  (c) => {
    if (c && !dirty.value) {
      drafts.value = c.profiles.map((p) => ({ ...p, password: '' }))
      interval.value = c.interval
    }
  },
  { immediate: true },
)
watch(
  () => snapshot.value?.topology,
  (t) => {
    if (!topologyText.value && t) topologyText.value = JSON.stringify(t, null, 2)
  },
  { immediate: true },
)
type Check = { level: 'error' | 'warn' | 'info'; title: string; detail: string; items: string[] }
// Everything here is derived from the current snapshot; nothing is fetched or
// written, so the panel always matches what the collector just saw.
const checks = computed<Check[]>(() => {
  const out: Check[] = []
  const failed = profiles.value.filter((p) => p.status !== 'ok' && p.status !== 'needs_auth')
  if (failed.length)
    out.push({
      level: 'error',
      title: '采集连接异常',
      detail: '这些连接最近一次采集失败，检查网络、账号或设备是否在线。',
      items: failed.map((p) => `${p.name} · ${p.host} · ${p.error || p.status}`),
    })
  const auth = profiles.value.filter((p) => p.status === 'needs_auth')
  if (auth.length)
    out.push({
      level: 'warn',
      title: '缺少管理凭据',
      detail: '这些连接还没有输入密码，采集不到数据。',
      items: auth.map((p) => `${p.name} · ${p.host}`),
    })
  const unnamed = devices.value.filter(
    (d) => d.online && (!d.name || /未命名|待确认|未核|候选/.test(d.name)),
  )
  if (unnamed.length)
    out.push({
      level: 'warn',
      title: '在线但身份未确认',
      detail: '建议在「设备与连接」里补上名称和分类。',
      items: unnamed.map((d) => `${d.name || d.mac} · ${d.ip || d.mac}`),
    })
  const uncategorized = devices.value.filter((d) => d.online && !d.category)
  if (uncategorized.length)
    out.push({
      level: 'info',
      title: '在线但没有分类',
      detail: '分类用于总览分组，未分类的设备会落到“全部”里。',
      items: uncategorized.slice(0, 8).map((d) => `${d.name || d.mac} · ${d.ip || d.mac}`),
    })
  // RSSI here is the AP's uplink view. A low uplink alone does not mean the
  // device feels slow — its downlink can be much stronger — so only flag links
  // where the negotiated rate has collapsed as well.
  const weak = devices.value.filter((d) => {
    if (!d.online || !d.ap_id || d.rssi == null) return false
    const floor = d.band === '2.4 GHz' ? 60 : 400
    const slow = d.tx_mbps != null && d.tx_mbps < floor
    return d.rssi < -85 || (d.rssi < -75 && slow)
  })
  if (weak.length)
    out.push({
      level: 'warn',
      title: '无线链路偏弱',
      detail:
        'AP 收到的信号偏低，且协商速率也明显掉档；只有信号低、速率正常的不再提示（下行通常远好于上行）。',
      items: weak
        .slice(0, 8)
        .map((d) => `${d.name || d.mac} · ${d.rssi} dBm · ${Math.round(d.tx_mbps || 0)} Mbps · ${d.band || ''}`),
    })
  const ont = profiles.value.find((p) => p.adapter === 'zte')
  const rx = ont?.data?.optics?.rx_dbm
  if (typeof rx === 'number' && rx < -25)
    out.push({
      level: 'warn',
      title: '接收光功率偏低',
      detail: `光模块接收 ${rx} dBm，低于 -25 dBm；建议检查光纤弯折或接头。`,
      items: [`${ont?.name || '光猫'} · 接收 ${rx} dBm`],
    })
  const busy = radios.value.filter((r) => r.utilization != null && r.utilization > 70)
  if (busy.length)
    out.push({
      level: 'warn',
      title: '无线信道繁忙',
      detail: '信道利用率超过 70%，同频设备可能出现延迟和重传。',
      items: busy.map(
        (r) => `${r.ap_name || r.ap_id} · ${r.band} · 信道 ${r.channel ?? '—'} · ${r.utilization}%`,
      ),
    })
  const stale = devices.value.filter(
    (d) => d.last_confirmed_at && Date.now() - Date.parse(d.last_confirmed_at) > 30 * 86400000,
  )
  if (stale.length)
    out.push({
      level: 'info',
      title: '30 天未确认的记录',
      detail: '台账里的旧记录，确认不再使用可以清理。',
      items: stale.slice(0, 8).map((d) => `${d.name || d.mac} · ${d.ip || d.mac}`),
    })
  // Infrastructure hosts legitimately expose several MACs on one address, so
  // only report an address when it maps to more than one physical device.
  const hosts = new Set(profiles.value.map((p) => p.host))
  const byIP = new Map<string, typeof devices.value>()
  for (const d of devices.value) {
    if (!d.ip || hosts.has(d.ip)) continue
    const list = byIP.get(d.ip) || []
    list.push(d)
    byIP.set(d.ip, list)
  }
  const duplicates = [...byIP.entries()].filter(
    ([, list]) => new Set(list.map((d) => d.physical_device_id || d.mac)).size > 1,
  )
  if (duplicates.length)
    out.push({
      level: 'error',
      title: 'IP 地址重复',
      detail: '同一 IP 出现在多台设备上，可能是固定绑定冲突。',
      items: duplicates.map(([ip, list]) => `${ip}（${list.length} 个 MAC）`),
    })
  return out
})
type SystemStats = {
  rss_bytes: number
  goroutines: number
  uptime_seconds: number
  storage: string
  archive_enabled: boolean
  last_maintenance?: string
  last_saved?: string
  database?: {
    database_bytes: number
    traffic_rows: number
    traffic_rows_24h: number
    device_hourly_rows: number
    largest_table?: string
    largest_bytes?: number
  }
}
const system = ref<SystemStats | null>(null)
function size(bytes?: number) {
  if (!bytes) return '—'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let n = bytes,
    i = 0
  while (n >= 1024 && i < units.length - 1) {
    n /= 1024
    i++
  }
  return `${n >= 100 || i === 0 ? n.toFixed(0) : n.toFixed(1)} ${units[i]}`
}
function duration(seconds?: number) {
  if (!seconds) return '—'
  const d = Math.floor(seconds / 86400),
    h = Math.floor((seconds % 86400) / 3600),
    m = Math.floor((seconds % 3600) / 60)
  return d > 0 ? `${d} 天 ${h} 小时` : h > 0 ? `${h} 小时 ${m} 分` : `${m} 分钟`
}
function moment(value?: string) {
  return value ? new Date(value).toLocaleString('zh-CN', { hour12: false }) : '尚未运行'
}
async function loadSystem() {
  try {
    system.value = await request<SystemStats>('/system')
  } catch {
    system.value = null
  }
}
onMounted(loadSystem)
const persisted = computed(() => snapshot.value?.credentials?.persisted === true)
const adapters = [
  ['openwrt', 'OpenWrt · SSH'],
  ['asus', 'ASUS / AiMesh · SSH'],
  ['zte', 'ZTE 光猫 · Telnet'],
  ['demo', '演示数据 · 无真实设备'],
  ['esxi', 'VMware ESXi · API'],
  ['tcp', '管理口连通性'],
]
const kindNames: Record<string, string> = {
  openwrt: 'OpenWrt 路由',
  asus: 'ASUS 路由/AP',
  esxi: 'VMware ESXi',
  zte: '光猫（Telnet）',
  tcp: '仅端口检查',
}
function kindLabel(kind: string) {
  return kindNames[kind] || kind
}
async function autoTopology() {
  try {
    const r = await request<{ topology: unknown }>('/topology/auto', 'POST')
    topologyText.value = JSON.stringify(r.topology, null, 2)
    toast('已按当前采集生成拓扑，可继续手工调整后保存')
  } catch (e) {
    toast((e as Error).message)
  }
}
function addCandidate(c: Candidate) {
  drafts.value.push({
    id: 'device-' + crypto.randomUUID().slice(0, 8),
    name: c.kind ? kindLabel(c.kind) : c.host,
    host: c.host,
    username: 'root',
    adapter: (c.kind || 'tcp') as Profile['adapter'],
    parent: '',
    password: '',
  })
  dirty.value = true
  toast(`已加入 ${c.host}，填好密码后保存`)
}
function add() {
  drafts.value.push({
    id: 'device-' + crypto.randomUUID().slice(0, 8),
    name: '新设备',
    host: '',
    username: 'root',
    adapter: 'asus',
    parent: '',
    password: '',
  })
  dirty.value = true
}
function remove(id: string) {
  drafts.value = drafts.value.filter((p) => p.id !== id)
  dirty.value = true
}
async function save() {
  if (
    await mutate(
      '/config',
      'PUT',
      { profiles: drafts.value, interval: interval.value },
      persisted.value ? '连接配置已保存；密码已加密保存到数据卷' : '连接配置已保存；密码只保留在当前运行内存',
    )
  ) {
    dirty.value = false
    drafts.value = snapshot.value!.config.profiles.map((p) => ({ ...p, password: '' }))
  }
}
async function discover() {
  scanning.value = true
  try {
    const v = await request<{ candidates: Candidate[] }>('/discover', 'POST')
    candidates.value = v.candidates
    toast('已检查当前已知内网地址的管理端口')
  } catch (e) {
    toast((e as Error).message)
  } finally {
    scanning.value = false
  }
}
async function saveTopology() {
  try {
    const doc = JSON.parse(topologyText.value)
    await mutate('/topology', 'PUT', doc, '已核对拓扑记录已保存')
  } catch {
    toast('拓扑JSON格式无效')
  }
}
</script>
<template>
  <div class="settings-intro">
    <div class="info-banner">
      <ShieldCheck :size="21" />
      <div>
        <b>连接用于只读采集</b>
        <p>
          不会通过页面修改光猫、路由器或AP配置。SSH严格核对主机指纹，ESXi支持固定证书SHA256指纹。
        </p>
      </div>
    </div>
    <div class="info-banner">
      <Database :size="21" />
      <div>
        <b>
          {{
            snapshot?.storage === 'postgresql'
              ? 'PostgreSQL 持久化'
              : snapshot?.storage === 'sqlite'
                ? 'SQLite 本地历史'
                : '文件持久化'
          }}
        </b>
        <p>
          {{
            snapshot?.storage === 'postgresql'
              ? '配置与采样存放在signal_atlas独立schema。'
              : snapshot?.storage === 'sqlite'
                ? '历史保存在数据目录的 history.db，无需额外数据库服务。'
                : '当前无需数据库，配置与采样保存在持久化数据目录。'
          }}密码不写入这些存储。
        </p>
      </div>
    </div>
  </div>
  <section class="panel">
    <div class="panel-heading">
      <div>
        <h2>体检</h2>
        <p>根据当前快照自动检查，不需要额外扫描；全部通过时不会列出任何条目。</p>
      </div>
      <span class="pill">{{ checks.length ? checks.length + ' 项待处理' : '正常' }}</span>
    </div>
    <div v-if="!checks.length" class="empty">当前没有需要处理的问题。</div>
    <ul v-else class="check-list">
      <li v-for="c in checks" :key="c.title" :class="c.level">
        <div>
          <b>{{ c.title }}</b>
          <p>{{ c.detail }}</p>
        </div>
        <small v-for="item in c.items" :key="item">{{ item }}</small>
      </li>
    </ul>
  </section>
  <section class="panel">
    <div class="panel-heading">
      <div>
        <h2>系统状态</h2>
        <p>API 进程与历史数据库的体量，用于长期运行维护。</p>
      </div>
      <button class="button subtle" @click="loadSystem">刷新</button>
    </div>
    <div v-if="system" class="detail-list system-stats">
      <div>
        <span>数据库大小</span>
        <b>{{ size(system.database?.database_bytes) }}</b>
      </div>
      <div>
        <span>流量明细行数</span>
        <b>
          {{ (system.database?.traffic_rows ?? 0).toLocaleString() }}
          <small class="muted">24 小时新增 {{ (system.database?.traffic_rows_24h ?? 0).toLocaleString() }}</small>
        </b>
      </div>
      <div>
        <span>设备小时汇总</span>
        <b>{{ (system.database?.device_hourly_rows ?? 0).toLocaleString() }} 行</b>
      </div>
      <div v-if="system.database?.largest_table">
        <span>最大表</span>
        <b>
          {{ system.database.largest_table }}
          <small class="muted">{{ size(system.database.largest_bytes) }}</small>
        </b>
      </div>
      <div>
        <span>API 内存占用</span>
        <b>{{ size(system.rss_bytes) }} · {{ system.goroutines }} 协程</b>
      </div>
      <div>
        <span>运行时长</span>
        <b>{{ duration(system.uptime_seconds) }}</b>
      </div>
      <div>
        <span>最近历史维护</span>
        <b>{{ moment(system.last_maintenance) }}</b>
      </div>
    </div>
    <div v-else class="empty">暂时读取不到系统状态。</div>
  </section>
  <section v-if="!drafts.length" class="panel onboarding-panel">
    <div class="panel-heading">
      <div>
        <h2>开始配置</h2>
        <p>
          还没有采集连接。先扫描本网段，识别出路由、AP、光猫后点「添加」，填好密码保存；
          首次连接的设备还需要在页面上确认一次 SSH 指纹。
        </p>
      </div>
      <button class="button" :disabled="scanning || !snapshot" @click="discover">
        <Search :size="14" />
        {{ scanning ? '扫描中…' : '扫描本网段' }}
      </button>
    </div>
    <ol class="onboarding-steps">
      <li>扫描网段，选择识别出的设备</li>
      <li>填写管理密码并保存</li>
      <li>确认指纹，等待第一轮采集</li>
    </ol>
  </section>
  <section v-if="hostkeys.length" class="panel hostkey-panel">
    <div class="panel-heading">
      <div>
        <h2>待确认的 SSH 指纹</h2>
        <p>这些设备首次连接，确认主机指纹后才会开始采集。只有在你确认它就是该设备时才点信任。</p>
      </div>
      <span class="tag amber">{{ hostkeys.length }}</span>
    </div>
    <ul class="hostkey-list">
      <li v-for="k in hostkeys" :key="k.host">
        <span class="hostkey-host">{{ k.host }}</span>
        <code>{{ k.fingerprint }}</code>
        <button class="button subtle" @click="trustHost(k.host)">信任</button>
      </li>
    </ul>
  </section>
  <section class="panel">
    <div class="panel-heading">
      <div>
        <h2>采集连接</h2>
        <p>修改后点击保存。留空密码会保留{{ persisted ? '已保存' : '当前内存中的' }}的已有凭据。</p>
      </div>
      <button class="button secondary" @click="add">
        <Plus :size="14" />
        添加设备
      </button>
    </div>
    <div class="connections-grid">
      <article v-for="p in drafts" :key="p.id" class="connection-card">
        <div class="connection-title">
          <input v-model="p.name" aria-label="设备名称" @input="dirty = true" />
          <button class="icon-button" :aria-label="`移除${p.name}采集连接`" @click="remove(p.id)">
            <Trash2 :size="15" />
          </button>
        </div>
        <div class="connection-fields">
          <label class="field">
            <span>采集方式</span>
            <select v-model="p.adapter" @change="dirty = true">
              <option v-for="[value, label] in adapters" :key="value" :value="value">
                {{ label }}
              </option>
            </select>
          </label>
          <label class="field">
            <span>管理地址</span>
            <input v-model="p.host" placeholder="192.168.1.1" @input="dirty = true" />
          </label>
          <template v-if="p.adapter !== 'tcp'">
            <label class="field">
              <span>管理用户名</span>
              <input v-model="p.username" autocomplete="off" @input="dirty = true" />
            </label>
            <label class="field">
              <span>密码 · {{ persisted ? '已加密保存' : '仅内存' }}</span>
              <input
                v-model="p.password"
                type="password"
                autocomplete="off"
                :placeholder="
                  p.has_password
                    ? persisted
                      ? '已保存，留空保留'
                      : '已在内存，留空保留'
                    : '输入管理密码'
                "
                @input="dirty = true"
              />
            </label>
          </template>
          <label v-if="p.adapter === 'esxi'" class="field full">
            <span>服务器证书 SHA256（可选，支持自签证书固定校验）</span>
            <input
              v-model="p.certificate_sha256"
              class="mono"
              placeholder="64位指纹；留空使用系统信任"
              @input="dirty = true"
            />
          </label>
          <label class="field full">
            <span>逻辑上级</span>
            <select v-model="p.parent" @change="dirty = true">
              <option value="">无上级 / 入户设备</option>
              <option
                v-for="other in drafts.filter((x) => x.id !== p.id)"
                :key="other.id"
                :value="other.id"
              >
                {{ other.name }}
              </option>
            </select>
          </label>
        </div>
        <div class="credential-state">
          <KeyRound :size="12" />
          {{
            p.adapter === 'tcp'
              ? '仅检查端口，不需要密码'
              : p.has_password
                ? persisted
                  ? '凭据已加密保存'
                  : '凭据已在当前运行内存'
                : '尚未提供密码'
          }}
          <label v-if="p.has_password" class="check">
            <input v-model="p.clear_password" type="checkbox" @change="dirty = true" />
            {{ persisted ? '清除已保存的密码' : '清除内存密码' }}
          </label>
        </div>
      </article>
      <div v-if="!drafts.length" class="empty">从添加一个管理设备开始</div>
    </div>
    <div class="settings-actions">
      <label class="interval-field">
        采集周期
        <input v-model.number="interval" type="number" min="20" max="300" @input="dirty = true" />
        秒
      </label>
      <span v-if="dirty" class="muted">有未保存更改</span>
      <button class="button primary" :disabled="pending" @click="save">
        <Save :size="14" />
        保存并连接
      </button>
    </div>
  </section>
  <section class="panel discovery-panel">
    <div class="panel-heading">
      <div>
        <h2>管理端口发现</h2>
        <p>扫描本网段的管理端口并识别设备类型；不扫描公网、不尝试密码。</p>
      </div>
      <button class="button secondary" :disabled="scanning || !snapshot" @click="discover">
        <Search :size="14" />
        {{ scanning ? '扫描中…' : '扫描本网段' }}
      </button>
    </div>
    <div v-if="candidates.length" class="candidate-grid">
      <div v-for="c in candidates" :key="c.host" class="candidate">
        <b class="mono">{{ c.host }}</b>
        <small>
          {{ c.services.join(' / ') }}
          <template v-if="c.kind"> · {{ kindLabel(c.kind) }}</template>
          {{ c.configured ? ' · 已配置' : '' }}
        </small>
        <button v-if="!c.configured" class="button subtle" @click="addCandidate(c)">添加</button>
      </div>
    </div>
    <p v-else class="hint compact">扫描结果会列在这里；无开放管理端口不代表设备离线。</p>
  </section>
  <section class="panel control-panel">
    <details>
      <summary>高级：维护已核对拓扑</summary>
      <p class="hint">
        连接配置的上级关系用于自动生成逻辑图；已有实测拓扑时优先显示实测记录。可在这里维护节点、线缆速率与来源说明，不能凭扫描自动识别没有管理地址的交换机。
      </p>
      <textarea
        v-model="topologyText"
        class="json-editor"
        aria-label="已核对拓扑JSON"
        spellcheck="false"
      ></textarea>
      <div class="button-row">
        <button class="button secondary" :disabled="pending" @click="autoTopology">
          <Sparkles :size="14" />
          按采集自动生成
        </button>
        <button class="button secondary" :disabled="pending || !topologyText" @click="saveTopology">
          保存拓扑记录
        </button>
      </div>
    </details>
  </section>
</template>
