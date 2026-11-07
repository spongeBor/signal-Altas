import { describe, expect, it } from 'vitest'
import { lineData, groupRows, trafficMean, peak } from './historyCharts'
const at = (seconds: number) => new Date(seconds * 1000).toISOString()
describe('archive chart evidence', () => {
  it('sorts timestamps and preserves missing values and cycle gaps', () => {
    const data = lineData(
      [
        { time: at(300), rssi: -60 },
        { time: at(0), rssi: -70 },
        { time: at(30), rssi: null },
      ],
      'rssi',
      30000,
    )
    expect(data.map((d) => d.value[1])).toEqual([-70, null, null, -60])
    expect(data.at(-1)?.sourceAt).toBe(at(300))
  })
  it('does not conflate entities or coerce strings and missing metrics to zero', () => {
    const rows = [
      { time: at(0), id: 'ont', cpu: 0 },
      { time: at(0), id: 'router', cpu: '20' },
    ]
    expect(groupRows(rows, (r) => r.id)).toHaveLength(2)
    expect(lineData(rows, 'cpu', 30000).map((d) => d.value[1])).toEqual([0, null])
    expect(peak(rows, 'temperature')).toBeNull()
  })
  it('weights traffic hourly means only by their sample counts', () => {
    expect(
      trafficMean(
        [
          { time: at(0), down: 10, samples: 1 },
          { time: at(3600), down: 20, samples: 3 },
          { time: at(7200), down: null, samples: 100 },
        ],
        'down',
        true,
      ),
    ).toBe(17.5)
  })
})
