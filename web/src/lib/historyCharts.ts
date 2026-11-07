export type HistoryRow = Record<string, any> & { time: string }
export const chartColors = [
  '#2563eb',
  '#0d9488',
  '#8b5cf6',
  '#d97706',
  '#e11d48',
  '#0284c7',
  '#475569',
  '#a16207',
]
export function metric(value: unknown): number | null {
  return typeof value === 'number' && Number.isFinite(value) ? value : null
}
// A missing cycle or missing metric must not be drawn as a continuous measurement.
export function lineData(rows: HistoryRow[], key: string, cadence: number) {
  const data: { value: [number, number | null]; sourceAt: string }[] = []
  let previous: number | undefined
  for (const row of [...rows].sort((a, b) => Date.parse(a.time) - Date.parse(b.time))) {
    const time = Date.parse(row.time)
    if (!Number.isFinite(time)) continue
    if (previous !== undefined && time - previous > cadence * 3)
      data.push({ value: [previous + cadence, null], sourceAt: '' })
    data.push({ value: [time, metric(row[key])], sourceAt: row.time })
    previous = time
  }
  return data
}
export function groupRows(rows: HistoryRow[], key: (row: HistoryRow) => string) {
  const groups = new Map<string, HistoryRow[]>()
  for (const row of rows) {
    const id = key(row)
    const group = groups.get(id) || []
    group.push(row)
    groups.set(id, group)
  }
  return [...groups.entries()]
}
export function peak(rows: HistoryRow[], key: string, summaryKey = key) {
  const values = rows
    .map((r) => metric(r[summaryKey]) ?? metric(r[key]))
    .filter((v): v is number => v !== null)
  return values.length ? Math.max(...values) : null
}
export function trafficMean(rows: HistoryRow[], key: string, hourly: boolean) {
  let total = 0,
    weight = 0
  for (const row of rows) {
    const value = metric(row[key])
    const n = hourly ? metric(row.samples) : 1
    if (value !== null && n !== null && n > 0) {
      total += value * n
      weight += n
    }
  }
  return weight ? total / weight : null
}
