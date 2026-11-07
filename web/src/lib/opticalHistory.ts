import type { HistoryRow } from './historyCharts'
export const opticalKeys = [
  'rx_power_dbm',
  'tx_power_dbm',
  'optical_temperature_c',
  'optical_voltage_v',
  'optical_bias_ma',
]
export function isBusinessOptics(row: HistoryRow) {
  return row.optical_source === 'zte_qoe_v2'
}
export function maskLegacyOptics(rows: HistoryRow[]) {
  return rows.map((row) =>
    isBusinessOptics(row)
      ? row
      : { ...row, ...Object.fromEntries(opticalKeys.map((key) => [key, null])) },
  )
}
