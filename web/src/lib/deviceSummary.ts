import type { ProfileResult, ProfileData, Topology } from '../types'
export interface SummaryMetric {
  label: string
  value: string
  unit?: string
  detail?: string
  text?: boolean
}
export function freshProfile(profile: ProfileResult, now = Date.now()) {
  const age = now - Date.parse(profile.collected_at)
  return profile.status === 'ok' && Number.isFinite(age) && age >= -5000 && age <= 90000
}
export function numeric(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}
function metric(label: string, value: unknown, unit = '', detail = '', digits = 0): SummaryMetric {
  const n = numeric(value)
  return { label, value: n === null ? '—' : n.toFixed(digits), unit, detail }
}
export function linkRate(value: unknown): { value: string; unit: string } {
  const n = numeric(value)
  return n === null || n <= 0
    ? { value: '—', unit: '' }
    : n >= 1000
      ? { value: String(Number((n / 1000).toFixed(2))), unit: 'Gbps' }
      : { value: String(n), unit: 'Mbps' }
}
export function duration(value: unknown) {
  const n = numeric(value)
  if (n === null || n < 0) return '未读取'
  const minutes = Math.floor(n / 60),
    days = Math.floor(minutes / 1440),
    hours = Math.floor(minutes / 60) % 24
  return `${days ? days + '天 ' : ''}${hours}小时 ${minutes % 60}分`
}
export function memoryPercent(memory?: ProfileData['memory']): number | null {
  if (!memory) return null
  const total = numeric(memory.MemTotal),
    free = numeric(memory.MemAvailable)
  return total !== null && free !== null && total > 0 && free >= 0 && free <= total
    ? (1 - free / total) * 100
    : null
}
function count(value: unknown) {
  return Array.isArray(value) ? value.length : null
}
export function powerState(value: string) {
  return (
    ({ poweredOn: '运行中', poweredOff: '已关机', suspended: '已挂起' } as Record<string, string>)[
      value
    ] || `状态 ${value}`
  )
}
export function managementNIC(profile: ProfileResult) {
  const vmk = profile.data.vmkernel_interfaces?.find((v) => v.ip === profile.host)
  if (!vmk) return undefined
  return profile.data.pnics?.find((n) => n.mac.toLowerCase() === vmk.mac.toLowerCase())
}
export function summaryMetrics(profile: ProfileResult, now = Date.now()): SummaryMetric[] {
  const live = freshProfile(profile, now),
    d: ProfileData = live ? profile.data : {}
  if (profile.adapter === 'openwrt') {
    const memory = d.memory
    const rate0 = linkRate(d.link_speeds?.[0]),
      rate1 = linkRate(d.link_speeds?.[1])
    return [
      metric('CPU 使用率', d.cpu_percent, '%', '本轮采样', 1),
      metric(
        '内存使用率',
        memoryPercent(memory),
        '%',
        memory
          ? `可用 ${(memory.MemAvailable / 1024).toFixed(0)} / 总计 ${(memory.MemTotal / 1024).toFixed(0)} MiB`
          : '未读取内存',
        1,
      ),
      {
        label: '宽带连接',
        value: d.wan_up === true ? '已连接' : d.wan_up === false ? '未连接' : '未读取',
        detail: d.wan_device || '连接接口未读取',
        text: true,
      },
      {
        label: '拨号在线时长',
        value: duration(d.wan_uptime),
        text: true,
        detail: '从当前拨号连接建立起计算',
      },
      { label: '网口 eth0', ...rate0, detail: '设备当前协商速率' },
      { label: '网口 eth1', ...rate1, detail: '设备当前协商速率' },
    ]
  }
  if (profile.adapter === 'asus') {
    const stations = d.stations
    const weak = stations?.filter((s) => numeric(s.rssi) !== null && s.rssi! < -75).length
    const unknown = stations?.filter((s) => numeric(s.rssi) === null).length
    return [
      metric('无线关联总数', count(stations), '条', '关联记录不等于实体台数'),
      metric(
        '已测弱信号关联',
        weak,
        '条',
        stations ? `RSSI < −75 dBm；${unknown} 条未返回信号` : '等待无线信号采样',
      ),
    ]
  }
  if (profile.adapter === 'esxi') {
    const nic = live ? managementNIC(profile) : undefined
    const rate = linkRate(nic?.link_mbps)
    const pci = d.vms?.reduce((sum, v) => sum + v.pci_passthrough.length, 0)
    return [
      {
        label: '管理口链路',
        ...rate,
        detail: nic
          ? `${nic.device} · ${nic.full_duplex === true ? '全双工' : nic.full_duplex === false ? '半双工' : '双工未读取'}`
          : '未能对应到管理口物理网卡',
      },
      metric('运行中虚拟机', d.vms?.filter((v) => v.power_state === 'poweredOn').length, '台'),
      metric('虚拟机总数', count(d.vms), '台'),
      metric('PCI 直通设备', pci, '个', '来自虚拟机硬件清单'),
    ]
  }
  if (profile.adapter === 'tcp')
    return [
      {
        label: '管理口状态',
        value: live ? '已连通' : '未确认',
        text: true,
        detail: '仅表示 TCP 管理口可达',
      },
      metric('管理口连接耗时', d.latency_ms, 'ms', '不是交换转发延迟', 1),
    ]
  return []
}
interface SwitchRecord {
  model?: string
  host?: string
  observed_at?: string
  all_ports?: Record<string, string>
  linked_ports?: Record<string, string>
}
export function switchRecord(topology: Topology | null, host: string): SwitchRecord | null {
  const r = topology?.switch
  if (!r || typeof r !== 'object' || Array.isArray(r)) return null
  const value = r as SwitchRecord
  return value.host === host ? value : null
}
export function recordedPorts(record: SwitchRecord | null) {
  return Object.entries(record?.all_ports || record?.linked_ports || {})
    .filter(([port, state]) => /^\d+$/.test(port) && typeof state === 'string')
    .sort((a, b) => Number(a[0]) - Number(b[0]))
    .map(([port, state]) => {
      const match = /^(\d+)(Full|Half)$/.exec(state)
      const rate = linkRate(match ? Number(match[1]) : null)
      return {
        port,
        label: match
          ? `${rate.value} ${rate.unit} · ${match[2] === 'Full' ? '全双工' : '半双工'}`
          : state === '掉线'
            ? '记录时未连接'
            : state,
      }
    })
}
