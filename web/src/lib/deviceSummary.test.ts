import { describe, it, expect } from 'vitest'
import type { ProfileResult } from '../types'
import {
  summaryMetrics,
  freshProfile,
  memoryPercent,
  managementNIC,
  switchRecord,
  recordedPorts,
} from './deviceSummary'
const now = Date.parse('2026-10-31T00:00:00+08:00')
const profile = (
  adapter: ProfileResult['adapter'],
  data: ProfileResult['data'],
): ProfileResult => ({
  id: 'test',
  name: '设备',
  host: '192.168.1.3',
  username: '',
  adapter,
  parent: '',
  status: 'ok',
  collected_at: new Date(now).toISOString(),
  duration: 0.1,
  data,
})
describe('device summary evidence', () => {
  it('preserves valid zero and distinguishes missing data and expired samples', () => {
    const p = profile('openwrt', { cpu_percent: 0, wan_up: false })
    const m = summaryMetrics(p, now)
    expect(m[0]?.value).toBe('0.0')
    expect(m[1]?.value).toBe('—')
    expect(m[2]?.value).toBe('未连接')
    expect(summaryMetrics(p, now + 91000)[0]?.value).toBe('—')
    expect(freshProfile({ ...p, status: 'error' }, now)).toBe(false)
  })
  it('uses available memory and does not fabricate percentages', () => {
    expect(memoryPercent({ MemTotal: 100, MemAvailable: 0 })).toBe(100)
    expect(memoryPercent({ MemTotal: 0, MemAvailable: 0 })).toBeNull()
    expect(memoryPercent({ MemTotal: 100, MemAvailable: 101 })).toBeNull()
  })
  it('counts only measured weak associations without repeating per-radio counts', () => {
    const p = profile('asus', {
      stations: [
        { mac: 'a', rssi: -90 },
        { mac: 'b', rssi: null },
      ] as any,
      radios: [],
    })
    const m = summaryMetrics(p, now)
    expect(m[0]?.value).toBe('2')
    expect(m[1]?.value).toBe('1')
    expect(m[1]?.detail).toContain('1 条未返回')
  })
  it('does not mistake an unrelated physical NIC for the management link', () => {
    const p = profile('esxi', {
      pnics: [
        { device: 'vmnic0', mac: 'aa', driver: 'x', link_mbps: 1000 },
        { device: 'vmnic1', mac: 'bb', driver: 'x', link_mbps: 2500 },
      ],
      vmkernel_interfaces: [
        { device: 'vmk0', mac: 'bb', ip: '192.168.1.3', portgroup: 'Management' },
      ],
    })
    expect(managementNIC(p)?.link_mbps).toBe(2500)
    p.host = '192.168.1.9'
    expect(managementNIC(p)).toBeUndefined()
  })
  it('keeps historical switch records attached to the right management host', () => {
    const t: any = {
      switch: { host: '192.168.1.10', all_ports: { '1': '2500Full', '2': '掉线' } },
    }
    expect(switchRecord(t, '192.168.1.9')).toBeNull()
    expect(recordedPorts(switchRecord(t, '192.168.1.10'))[1]?.label).toBe('记录时未连接')
  })
})
