export interface Profile {
  id: string
  name: string
  host: string
  username: string
  adapter: 'openwrt' | 'asus' | 'zte' | 'esxi' | 'tcp'
  parent: string
  certificate_sha256?: string
  has_password?: boolean
  password?: string
  clear_password?: boolean
}
export interface Radio {
  ap_id: string
  ap_name: string
  interface: string
  ssid: string
  band: string
  channel: number | null
  width_mhz: number | null
  utilization: number | null
  clients: number
  bss?: { ssid: string; clients: number }[]
}
export interface Station {
  mac: string
  ap_id: string
  interface: string
  ssid: string
  band: string
  rssi: number | null
  tx_mbps: number | null
  rx_mbps: number | null
  width_mhz: number | null
  wifi?: string
  mlo?: boolean
  mlo_links?: number
}
export interface Device extends Partial<Station> {
  mac: string
  name: string
  ip?: string
  addresses?: string[]
  online: boolean
  seen_via: string
  neighbor_state?: string
  ap_name?: string
  category?: string
  physical_device_id?: string
  wifi?: string
  mlo?: boolean
  mlo_links?: number
  collected_at: string
  last_confirmed_at?: string
  label_source?: string
  traffic?: DeviceTraffic
}
export interface DeviceTraffic {
  time: string
  down_bytes: number
  up_bytes: number
  down_mbps: number
  up_mbps: number
}
export interface OpticalModule {
  measurement_source?: string
  status: 'ok' | 'partial' | 'unavailable'
  source: string
  collected_at: string
  note?: string
  temperature_c?: number | null
  voltage_v?: number | null
  bias_ma?: number | null
  rx_dbm?: number | null
  tx_dbm?: number | null
  rx_mw?: number | null
  tx_mw?: number | null
  manufacturer?: string
  part_number?: string
  revision?: string
  date_code?: string
  monitoring_type?: string
  calibration: 'internal' | 'external' | 'unknown'
  ddm_supported?: boolean | null
  module_type?: string
  down_mbps?: number | null
  up_mbps?: number | null
  fec_enabled?: boolean | null
}
export interface ProfileData {
  model?: string
  firmware?: string
  uptime?: number
  wan_up?: boolean
  wan_uptime?: number
  wan_device?: string
  cpu_percent?: number
  memory?: { MemTotal: number; MemAvailable: number }
  link_speeds?: number[]
  radios?: Radio[]
  stations?: Station[]
  optics?: OpticalModule
  pon_state?: string
  chip_temperature?: number
  tables?: Record<string, Record<string, string>[]>
  fec?: Record<string, number>
  settings?: { roaming_thresholds: string[]; airtime_fairness: string[]; region_code: string }
  pnics?: {
    device: string
    mac: string
    driver: string
    link_mbps?: number
    full_duplex?: boolean
  }[]
  vmkernel_interfaces?: {
    device: string
    mac: string
    portgroup: string
    ip?: string
    dhcp?: boolean
  }[]
  vms?: { name: string; power_state: string; network_cards: unknown[]; pci_passthrough: string[] }[]
  vswitches?: { name: string; pnics?: string[] }[]
  portgroups?: { name: string; vlan: number; vswitch: string }[]
  latency_ms?: number
  note?: string
  [key: string]: unknown
}
export interface ProfileResult extends Profile {
  status: 'ok' | 'error' | 'needs_auth'
  error?: string
  collected_at: string
  duration: number
  data: ProfileData
}
export interface PingResult {
  target: string
  host: string
  sent: number
  recv: number
  rtt_avg: number
  rtt_min: number
  rtt_max: number
}
export interface Traffic {
  time: string
  down_mbps: number
  up_mbps: number
}
export interface TopologyNode {
  id: string
  name: string
  host?: string | null
  kind?: string
  mac?: string
  profile_id?: string
  x: number
  y: number
  subtitle?: string
  note?: string
}
export interface TopologyLink {
  from: string
  to: string
  label?: string
  evidence?: string
  kind?: string
}
export interface Topology {
  recorded_at?: string
  nodes: TopologyNode[]
  links: TopologyLink[]
  limitations?: string[]
  [key: string]: unknown
}
export interface WANMeter {
  time: string
  down_mbps: number
  up_mbps: number
  peak_down_mbps: number
  peak_up_mbps: number
  window_seconds: number
  interface: string
  error?: string
}
export interface Snapshot {
  token: string
  storage: string
  archive?: { enabled: boolean; last_saved?: string; error?: string }
  access?: { login_enabled: boolean; user: string }
  credentials?: { persisted: boolean }
  config: { profiles: Profile[]; interval: number }
  state: {
    last_updated: string
    collecting: boolean
    profiles: ProfileResult[]
    devices: Device[]
    radios: Radio[]
    history: Traffic[]
    traffic: Traffic | null
    ping?: PingResult[]
    events: { time: string; mac: string; name: string; text: string }[]
  }
  topology: Topology | null
  wan_meter?: WANMeter | null
}
export interface Candidate {
  host: string
  services: string[]
  configured: boolean
  kind?: string
  title?: string
}
