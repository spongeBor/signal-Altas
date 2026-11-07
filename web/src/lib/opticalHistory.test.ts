import { expect, it } from 'vitest'
import { maskLegacyOptics } from './opticalHistory'
it('keeps legacy values immutable while preventing a false business-power jump', () => {
  const rows = [
    { time: '2026-10-30T23:59:00Z', id: 'ont', rx_power_dbm: -20 },
    { time: '2026-10-31T00:00:00Z', id: 'ont', rx_power_dbm: -19, optical_source: 'zte_qoe_v2' },
  ]
  const masked = maskLegacyOptics(rows)
  expect(masked[0]?.rx_power_dbm).toBeNull()
  expect(masked[1]?.rx_power_dbm).toBe(-19)
  expect(rows[0]?.rx_power_dbm).toBe(-20)
  expect(masked[0]?.time).toBe(rows[0]?.time)
})
