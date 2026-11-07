<script setup lang="ts">
import { chartAppearance } from '../composables/useTheme'
import { computed } from 'vue'
import AtlasChart from './AtlasChart.vue'
import { opticalKeys, maskLegacyOptics } from '../lib/opticalHistory'
import ArchiveTopology from './ArchiveTopology.vue'
import { fmt, clock } from '../composables/useAtlas'
import {
  lineData,
  groupRows,
  peak,
  trafficMean,
  metric,
  type HistoryRow,
} from '../lib/historyCharts'
const props = defineProps<{
  rows: HistoryRow[]
  kind: string
  hourly: boolean
  from: string
  to: string
}>()
const emit = defineEmits<{ select: [at: string] }>()
const cadence = computed(() => (props.hourly ? 3600000 : 30000))
const groups = computed(() =>
  groupRows(props.rows, (r) =>
    props.kind === 'devices' || props.kind === 'device_traffic'
      ? r.mac
      : props.kind === 'ping'
        ? r.target
        : props.kind === 'profiles'
          ? r.id
          : `${r.ap_id}/${r.interface}`,
  ),
)
const names = computed(() =>
  Object.fromEntries(
    groups.value.map(([id, rs]) => [
      id,
      props.kind === 'radios'
        ? `${rs[0]?.ap_name || rs[0]?.ap_id} · ${rs[0]?.band || rs[0]?.interface}`
        : props.kind === 'ping'
          ? id
          : rs[0]?.name || id,
    ]),
  ),
)
function line(rs: HistoryRow[], key: string, name: string, color: string, dashed = false) {
  return {
    name,
    type: 'line',
    data: lineData(opticalKeys.includes(key) ? maskLegacyOptics(rs) : rs, key, cadence.value),
    showSymbol: rs.length < 3,
    symbolSize: 5,
    connectNulls: false,
    lineStyle: { width: dashed ? 1.5 : 2, type: dashed ? 'dashed' : 'solid' },
    itemStyle: { color },
    emphasis: { focus: 'series' },
    areaStyle: dashed ? undefined : { opacity: 0.035 },
  }
}
function axis(unit: string, min?: number, max?: number) {
  return {
    type: 'value',
    name: unit,
    min,
    max,
    axisLabel: { color: chartAppearance.value.text },
    splitLine: { lineStyle: { color: chartAppearance.value.split, type: 'dashed' } },
  }
}
function options(key: string, unit: string, min?: number, max?: number, peakKey?: string) {
  const series = groups.value
    .filter(([, rs]) => rs.some((r) => metric(r[key]) !== null))
    .flatMap(([id, rs], i) => {
      const color =
        chartAppearance.value.colors[
          groups.value.findIndex(([key]) => key === id) % chartAppearance.value.colors.length
        ]!
      const result = [line(rs, key, names.value[id], color)]
      if (props.hourly && peakKey && rs.some((r) => metric(r[peakKey]) !== null))
        result.push(line(rs, peakKey, names.value[id] + ' · 峰值', color, true))
      return result
    })
  return {
    xAxis: {
      type: 'time',
      min: Date.parse(props.from),
      max: Date.parse(props.to),
      axisLabel: { hideOverlap: true },
      splitLine: { show: false },
      axisLine: { lineStyle: { color: chartAppearance.value.line } },
    },
    yAxis: axis(unit, min, max),
    series,
  }
}
// Download and upload never share one plot. Two y axes side by side give each
// series its own full-height scale, so a 2 Mbps upload curve reaches the same
// height as a 120 Mbps download curve and the reader cannot compare them.
// Stacking keeps exactly one axis per direction while the time axis stays
// shared, and it keeps a single-sample peak from stretching both scales until
// the averages flatten onto the baseline.
function stacked(down: any[], up: any[], downUnit: string, upUnit: string) {
  const from = Date.parse(props.from),
    to = Date.parse(props.to)
  const timeAxis = (gridIndex: number, labels: boolean) => ({
    type: 'time',
    gridIndex,
    min: from,
    max: to,
    axisLabel: labels ? { hideOverlap: true } : { show: false },
    splitLine: { show: false },
    axisLine: { lineStyle: { color: chartAppearance.value.line } },
  })
  return {
    grid: [
      { top: 58, left: 68, right: 34, height: 118 },
      { top: 212, left: 68, right: 34, height: 118 },
    ],
    xAxis: [timeAxis(0, false), timeAxis(1, true)],
    yAxis: [
      { ...axis(downUnit, 0), gridIndex: 0 },
      { ...axis(upUnit, 0), gridIndex: 1 },
    ],
    series: [
      ...down.map((s) => ({ ...s, xAxisIndex: 0, yAxisIndex: 0 })),
      ...up.map((s) => ({ ...s, xAxisIndex: 1, yAxisIndex: 1 })),
    ],
  }
}
const traffic = computed(() => {
  const down = chartAppearance.value.colors[0]!
  const up = chartAppearance.value.colors[1]!
  return stacked(
    [
      line(props.rows, 'down_mbps', props.hourly ? '下载均值' : '下载', down),
      ...(props.hourly && props.rows.some((r) => metric(r.down_peak) !== null)
        ? [line(props.rows, 'down_peak', '下载峰值', down, true)]
        : []),
    ],
    [
      line(props.rows, 'up_mbps', props.hourly ? '上传均值' : '上传', up),
      ...(props.hourly && props.rows.some((r) => metric(r.up_peak) !== null)
        ? [line(props.rows, 'up_peak', '上传峰值', up, true)]
        : []),
    ],
    '下载 / Mbps',
    '上传 / Mbps',
  )
})
// Device traffic keeps bytes per collection window: raw rows carry rates,
// hourly rows carry that hour's accumulated bytes. Convert to one axis per
// resolution instead of mixing a byte count into a rate chart.
function withMegabytes(rows: HistoryRow[]) {
  if (!props.hourly) return rows
  return rows.map((r) => ({
    ...r,
    down_mb: metric(r.down_bytes) === null ? null : r.down_bytes / 1048576,
    up_mb: metric(r.up_bytes) === null ? null : r.up_bytes / 1048576,
  }))
}
const trafficTotal = (key: string) => props.rows.reduce((sum, r) => sum + (metric(r[key]) ?? 0), 0)
function sizeOf(bytes: number): [string, string] {
  const mb = bytes / 1048576
  return mb >= 1024 ? [fmt(mb / 1024, '', 2), 'GB'] : [fmt(mb, '', 1), 'MB']
}
const deviceTraffic = computed(() => {
  const down = props.hourly ? 'down_mb' : 'down_mbps'
  const up = props.hourly ? 'up_mb' : 'up_mbps'
  const unit = props.hourly ? 'MB' : 'Mbps'
  const plotted = groups.value.filter(([, rs]) =>
    rs.some((r) => metric(r.down_bytes) !== null || metric(r.up_bytes) !== null),
  )
  const color = (i: number) =>
    chartAppearance.value.colors[i % chartAppearance.value.colors.length]!
  return stacked(
    plotted.map(([id, rs], i) =>
      line(withMegabytes(rs), down, names.value[id] + ' · 下载', color(i)),
    ),
    plotted.map(([id, rs], i) =>
      line(withMegabytes(rs), up, names.value[id] + ' · 上传', color(i), true),
    ),
    `下载 / ${unit}`,
    `上传 / ${unit}`,
  )
})
const signals = computed(() => options('rssi', 'dBm', -110, 0))
const cpu = computed(() => options('cpu_percent', '%', 0, 100, 'cpu_peak'))
const temperature = computed(() =>
  options('chip_temperature', '°C', 0, undefined, 'temperature_peak'),
)
const opticalPower = computed(() => ({
  yAxis: axis('dBm'),
  series: groups.value
    .filter(([, rs]) =>
      rs.some((r) => metric(r.rx_power_dbm) !== null || metric(r.tx_power_dbm) !== null),
    )
    .flatMap(([id, rs]) => [
      line(rs, 'rx_power_dbm', names.value[id] + ' · 接收光功率', chartAppearance.value.colors[0]!),
      line(rs, 'tx_power_dbm', names.value[id] + ' · 发射光功率', chartAppearance.value.colors[1]!),
    ]),
}))
const opticalTemperature = computed(() => options('optical_temperature_c', '°C'))
const opticalVoltage = computed(() => options('optical_voltage_v', 'V'))
const opticalBias = computed(() => options('optical_bias_ma', 'mA'))
const utilization = computed(() => options('utilization', '%', 0, 100, 'utilization_peak'))
const clients = computed(() => options('clients', '条关联', 0, undefined, 'clients_peak'))
const phy = computed(() => ({
  yAxis: axis('Mbps', 0),
  series: groups.value.flatMap(([id, rs], i) => [
    line(
      rs,
      'tx_mbps',
      names.value[id] + ' · AP → 设备',
      chartAppearance.value.colors[i % chartAppearance.value.colors.length]!,
    ),
    ...(!props.hourly
      ? [
          line(
            rs,
            'rx_mbps',
            names.value[id] + ' · 设备 → AP',
            chartAppearance.value.colors[i % chartAppearance.value.colors.length]!,
            true,
          ),
        ]
      : []),
  ]),
}))
const pingMean = computed(() => {
  const values = props.rows.map((r) => metric(r.rtt_avg)).filter((v): v is number => v !== null)
  return values.length ? values.reduce((a, b) => a + b, 0) / values.length : null
})
const pingLossPct = computed(() => {
  let sent = 0,
    recv = 0
  for (const r of props.rows) {
    sent += Number(r.sent) || 0
    recv += Number(r.recv) || 0
  }
  return sent ? ((sent - recv) * 100) / sent : 0
})
const color = (i: number) =>
  chartAppearance.value.colors[i % chartAppearance.value.colors.length]!
const pingRTT = computed(() => ({
  yAxis: axis('ms', 0),
  series: groups.value.flatMap(([id, rs], i) => [
    line(rs, 'rtt_avg', names.value[id] + ' · 均值', color(i)),
    line(rs, 'rtt_peak', names.value[id] + ' · 峰值', color(i), true),
  ]),
}))
const pingLoss = computed(() => ({
  yAxis: axis('%', 0, 100),
  series: groups.value.map(([id, rs], i) => line(rs, 'loss', names.value[id], color(i))),
}))
const stats = computed(() =>
  props.kind === 'traffic'
    ? [
        ['平均下载', fmt(trafficMean(props.rows, 'down_mbps', props.hourly), '', 2), 'Mbps'],
        [
          '下载峰值（2 秒采样）',
          fmt(peak(props.rows, 'down_mbps', 'down_peak'), '', 1),
          'Mbps',
        ],
        ['平均上传', fmt(trafficMean(props.rows, 'up_mbps', props.hourly), '', 2), 'Mbps'],
        [
          '上传峰值（2 秒采样）',
          fmt(peak(props.rows, 'up_mbps', 'up_peak'), '', 1),
          'Mbps',
        ],
        ['有效记录', props.rows.length.toLocaleString(), props.hourly ? '个小时桶' : '次采样'],
      ]
    : props.kind === 'ping'
      ? [
          ['探测目标', groups.value.length, '个'],
          ['平均时延', fmt(pingMean.value, ' ms', 2), ''],
          ['最高时延', fmt(peak(props.rows, 'rtt_avg', 'rtt_peak'), ' ms', 2), ''],
          ['丢包率', fmt(pingLossPct.value, '%', 2), ''],
          ['记录', props.rows.length.toLocaleString(), props.hourly ? '个小时桶' : '次采样'],
        ]
      : props.kind === 'profiles'
      ? [
          ['采集连接', groups.value.length, '个'],
          [
            'CPU 采样峰值',
            fmt(peak(props.rows, 'cpu_percent', props.hourly ? 'cpu_peak' : 'cpu_percent'), '', 1),
            '%',
          ],
          [
            '最高芯片温度',
            fmt(
              peak(
                props.rows,
                'chip_temperature',
                props.hourly ? 'temperature_peak' : 'chip_temperature',
              ),
              '',
              1,
            ),
            '°C',
          ],
          ['记录数', props.rows.length.toLocaleString(), '条'],
        ]
      : props.kind === 'devices'
        ? [
            ['设备接口', groups.value.length, '条 MAC'],
            [
              '最强采样信号',
              fmt(peak(props.rows, 'rssi', props.hourly ? 'rssi_max' : 'rssi'), '', 0),
              'dBm',
            ],
            [
              '最高下行协商',
              fmt(peak(props.rows, 'tx_mbps', props.hourly ? 'tx_peak' : 'tx_mbps'), '', 0),
              'Mbps',
            ],
            ['记录数', props.rows.length.toLocaleString(), '条'],
          ]
        : props.kind === 'radios'
          ? [
              ['无线接口', groups.value.length, '个'],
              [
                '最高利用率',
                fmt(
                  peak(
                    props.rows,
                    'utilization',
                    props.hourly ? 'utilization_peak' : 'utilization',
                  ),
                  '',
                  1,
                ),
                '%',
              ],
              [
                '最高关联数',
                fmt(peak(props.rows, 'clients', props.hourly ? 'clients_peak' : 'clients'), '', 0),
                '条关联',
              ],
              ['记录数', props.rows.length.toLocaleString(), '条'],
            ]
          : props.kind === 'device_traffic'
            ? [
                ['有记录设备', groups.value.length, '台'],
                ['下载合计', ...sizeOf(trafficTotal('down_bytes'))],
                ['上传合计', ...sizeOf(trafficTotal('up_bytes'))],
                [
                  '最高下载速率',
                  fmt(
                    peak(props.rows, 'down_mbps', 'down_peak'),
                    '',
                    2,
                  ),
                  'Mbps',
                ],
              ]
            : [],
)
const labels: Record<string, string> = {
  association: '接入点变化',
  connection: '连接证据变化',
  collector: '采集状态',
  wan: 'WAN 状态',
  pon: 'PON 状态',
  rename: '设备改名',
  topology: '拓扑保存',
  labels_baseline: '迁入基线',
}
const events = computed(() => {
  const categories = [...new Set(props.rows.map((r) => labels[r.kind] || r.kind))]
  return {
    grid: { top: 24, left: 112, right: 30, bottom: 76 },
    yAxis: {
      type: 'category',
      data: categories,
      axisTick: { show: false },
      axisLine: { show: false },
    },
    series: [
      {
        type: 'scatter',
        symbolSize: 9,
        data: props.rows.map((r) => ({
          value: [Date.parse(r.time), labels[r.kind] || r.kind],
          sourceAt: r.time,
        })),
        itemStyle: { color: chartAppearance.value.accent, opacity: 0.7 },
      },
    ],
  }
})
// Sampling lanes preserve uncertainty and never invent the order of hourly AP sets.
const lanes = computed(() =>
  groups.value.map(([id, rs]) => {
    const sorted = [...rs].sort((a, b) => Date.parse(a.time) - Date.parse(b.time))
    const segments: { start: number; end: number; label: string; color: string; at: string }[] = []
    const a = Date.parse(props.from),
      b = Date.parse(props.to)
    const apNames = [...new Set(sorted.map((r) => r.ap_name || r.ap_id).filter(Boolean))]
    for (let i = 0; i < sorted.length; i++) {
      const r = sorted[i]!,
        t = Date.parse(r.time)
      const next = Date.parse(sorted[i + 1]?.time || '')
      const end = Math.min(
        b,
        Number.isFinite(next) && next - t <= cadence.value * 3 ? next : t + cadence.value,
      )
      if (end <= a || t >= b) continue
      const healthy =
        props.kind === 'profiles'
          ? props.hourly
            ? r.ok_samples === r.samples
            : r.status === 'ok'
          : props.hourly
            ? r.confirmed_samples > 0
            : r.online
      const label =
        props.kind === 'profiles'
          ? props.hourly
            ? `采集成功 ${r.ok_samples}/${r.samples}`
            : labels[r.status] ||
              (r.status === 'ok' ? '采集正常' : r.status === 'needs_auth' ? '等待凭据' : '采集失败')
          : props.hourly
            ? `${r.aps?.join(' / ') || '无AP记录'} · 确认连接 ${r.confirmed_samples}/${r.samples}（小时内不表示先后）`
            : healthy
              ? `${r.ap_name || '无AP关联'} · ${r.band || r.seen_via || ''}`
              : '本轮未确认连接'
      const color = !healthy
        ? '#cbd5e1'
        : props.kind === 'profiles'
          ? '#0d9488'
          : chartAppearance.value.colors[
              Math.max(0, apNames.indexOf(r.ap_name || r.ap_id)) %
                chartAppearance.value.colors.length
            ]!
      const start = Math.max(a, t)
      const last = segments.at(-1)
      if (last && last.label === label && last.end === start && last.color === color) last.end = end
      else segments.push({ start, end, label, color, at: r.time })
    }
    return {
      id,
      name: names.value[id],
      segments: segments.map((s) => ({
        ...s,
        left: ((s.start - a) / (b - a)) * 100,
        width: ((s.end - s.start) / (b - a)) * 100,
      })),
    }
  }),
)
function hasSeries(option: any) {
  return option.series?.some((s: any) => s.data?.some((d: any) => d.value?.[1] != null))
}
</script>
<template>
  <div v-if="!rows.length" class="empty history-empty">
    <b>这个时间范围没有记录</b>
    <span>可更换时间或切换小时汇总；缺失的数据不会自动补造。</span>
  </div>
  <template v-else>
    <div v-if="stats.length" class="history-stats">
      <div v-for="s in stats" :key="String(s[0])">
        <span>{{ s[0] }}</span>
        <b>
          {{ s[1] }}
          <small>{{ s[2] }}</small>
        </b>
      </div>
    </div>
    <div v-if="kind === 'traffic'" class="chart-section">
      <div class="chart-heading">
        <h3>宽带上下行流量</h3>
        <span>
          {{ hourly ? '上图下载 · 下图上传 · 小时均值' : '上图下载 · 下图上传 · 30 秒采样' }}
        </span>
      </div>
      <AtlasChart
        :option="traffic"
        :height="400"
        label="历史下载与上传流量，单位Mbps"
        @select="emit('select', $event)"
      />
      <p class="chart-note">
        下载在上、上传在下，各用一根坐标轴和独立刻度，时间轴对齐；把两条曲线画在同一张图里会让 2
        Mbps 的上传看起来和 120 Mbps
        的下载一样高。采样峰值在下方指标卡中单列，避免单次突发把两条均值压平。
      </p>
    </div>
    <div v-if="kind === 'ping'" class="chart-section">
      <div class="chart-heading">
        <h3>探测时延</h3>
        <span>{{ hourly ? '实线为小时均值，虚线为该小时最高值' : '每次探测的平均时延' }}</span>
      </div>
      <AtlasChart
        v-if="hasSeries(pingRTT)"
        :option="pingRTT"
        :height="320"
        label="各探测目标的时延"
        @select="emit('select', $event)"
      />
      <div class="chart-heading" style="margin-top: 20px">
        <h3>丢包率</h3>
        <span>按探测目标统计</span>
      </div>
      <AtlasChart
        v-if="hasSeries(pingLoss)"
        :option="pingLoss"
        :height="240"
        label="各探测目标的丢包率"
        @select="emit('select', $event)"
      />
      <p class="chart-note">
        探测由路由器发出：网关、接入点和两个公网地址。时延异常升高或出现丢包时，先看是局域网段还是公网段。
      </p>
    </div>
    <div v-if="kind === 'device_traffic'" class="chart-section">
      <div class="chart-heading">
        <h3>设备上下行流量</h3>
        <span>{{ hourly ? '每小时累计字节 · 上下分图' : '按采集窗口的速率 · 缺口留空' }}</span>
      </div>
      <AtlasChart
        v-if="hasSeries(deviceTraffic)"
        :option="deviceTraffic"
        :height="400"
        label="按设备区分的下载与上传流量"
        @select="emit('select', $event)"
      />
      <div v-else class="empty small">所选范围没有设备流量记录。</div>
      <p class="chart-note">
        来自路由器既有 QoS 规则的每地址字节计数，不抓包、不含访问内容。首次采集、路由器重启或设备换
        IP 的基线轮次不计入用量，缺失窗口留空不补零。下载在上、上传在下，各自刻度。
      </p>
    </div>
    <template v-if="kind === 'devices'">
      <div class="chart-section">
        <div class="chart-heading">
          <h3>无线信号强度</h3>
          <span>AP 收到的终端信号 · {{ hourly ? '小时均值' : '详细记录' }}</span>
        </div>
        <AtlasChart
          v-if="hasSeries(signals)"
          :option="signals"
          label="按设备区分的 AP 侧接收信号（设备→AP），单位dBm"
          @select="emit('select', $event)"
        />
        <div v-else class="empty small">所选设备没有无线信号记录。</div>
      </div>
      <div class="chart-section">
        <div class="chart-heading">
          <h3>无线协商速率</h3>
          <span>链路协商能力，不是实际吞吐</span>
        </div>
        <AtlasChart
          v-if="hasSeries(phy)"
          :option="phy"
          label="无线双向协商速率，单位Mbps"
          @select="emit('select', $event)"
        />
        <div v-else class="empty small">没有协商速率记录。</div>
      </div>
    </template>
    <div v-if="kind === 'profiles' || kind === 'radios'" class="chart-grid">
      <template v-if="kind === 'profiles'">
        <div class="chart-section">
          <div class="chart-heading">
            <h3>CPU 使用率</h3>
            <span>{{ hourly ? '均值与峰值' : '详细记录' }}</span>
          </div>
          <AtlasChart
            v-if="hasSeries(cpu)"
            :option="cpu"
            label="各设备CPU使用率，单位百分比"
            @select="emit('select', $event)"
          />
          <div v-else class="empty small">没有 CPU 记录</div>
        </div>
        <div class="chart-section">
          <div class="chart-heading">
            <h3>芯片温度</h3>
            <span>来自设备芯片传感器</span>
          </div>
          <AtlasChart
            v-if="hasSeries(temperature)"
            :option="temperature"
            label="各设备芯片温度，单位摄氏度"
            @select="emit('select', $event)"
          />
          <div v-else class="empty small">没有温度记录</div>
        </div>
      </template>
      <template v-else>
        <div class="chart-section">
          <div class="chart-heading">
            <h3>信道利用率</h3>
            <span>{{ hourly ? '均值与峰值' : '按无线接口区分' }}</span>
          </div>
          <AtlasChart
            v-if="hasSeries(utilization)"
            :option="utilization"
            label="各无线接口信道利用率，单位百分比"
            @select="emit('select', $event)"
          />
          <div v-else class="empty small">设备未提供信道利用率</div>
        </div>
        <div class="chart-section">
          <div class="chart-heading">
            <h3>无线关联数</h3>
            <span>接口关联记录，不等于实体台数</span>
          </div>
          <AtlasChart :option="clients" label="各无线接口关联数" @select="emit('select', $event)" />
        </div>
      </template>
    </div>
    <template v-if="kind === 'profiles'">
      <p
        v-if="
          !hourly && rows.some((r) => r.rx_power_dbm != null && r.optical_source !== 'zte_qoe_v2')
        "
        class="chart-note"
      >
        此范围含旧版底层诊断记录，已从业务口径曲线中排除；原值保留在明细和回放，并标注旧口径。
      </p>
      <div v-if="!hourly && hasSeries(opticalPower)" class="chart-section">
        <div class="chart-heading">
          <h3>接收与发射光功率</h3>
          <span>光模块遥测 · 单位 dBm</span>
        </div>
        <AtlasChart
          :option="opticalPower"
          label="光模块接收与发射光功率，单位dBm"
          @select="emit('select', $event)"
        />
      </div>
      <div v-if="!hourly" class="chart-grid">
        <div
          v-for="c in [
            { title: '光模块温度', unit: '°C', option: opticalTemperature },
            { title: '光模块供电电压', unit: 'V', option: opticalVoltage },
            { title: '激光器偏置电流', unit: 'mA', option: opticalBias },
          ].filter((c) => hasSeries(c.option))"
          :key="c.title"
          class="chart-section"
        >
          <div class="chart-heading">
            <h3>{{ c.title }}</h3>
            <span>{{ c.unit }} · 光模块独立指标</span>
          </div>
          <AtlasChart
            :option="c.option"
            :label="c.title + '，单位' + c.unit"
            @select="emit('select', $event)"
          />
        </div>
      </div>
      <p v-if="hourly" class="chart-note">
        光模块补充指标只保存在明细记录中，当前小时汇总仅包含原有CPU和芯片温度；请切换“详细记录”查询光学趋势。
      </p>
      <p v-else-if="!hasSeries(opticalPower)" class="chart-note">
        当前范围没有光功率明细。光学指标自接入后开始记录，缺失或更早的数据不补造。
      </p>
    </template>
    <div v-if="['devices', 'profiles'].includes(kind)" class="chart-section">
      <div class="chart-heading">
        <h3>{{ kind === 'devices' ? '接入点与连接采样' : '采集状态时间线' }}</h3>
        <span>
          {{
            hourly
              ? '每段代表一个小时桶，AP 集合不表示切换顺序'
              : '按相邻采样展示；灰色为未确认或异常，较长缺口留空'
          }}
        </span>
      </div>
      <div class="sampling-lanes">
        <div v-for="lane in lanes" :key="lane.id" class="sampling-lane">
          <span :title="lane.name">{{ lane.name }}</span>
          <div class="lane-track">
            <button
              v-for="s in lane.segments"
              :key="s.start"
              :style="{ left: s.left + '%', width: s.width + '%', background: s.color }"
              :title="`${clock(new Date(s.start).toISOString(), true)} — ${clock(new Date(s.end).toISOString(), true)} · ${s.label}`"
              :aria-label="`${lane.name}，${s.label}，${clock(s.at, true)}，查看当时状态`"
              @click="emit('select', s.at)"
            ></button>
          </div>
        </div>
        <div class="lane-axis">
          <span>{{ clock(from, true) }}</span>
          <span>{{ clock(to, true) }}</span>
        </div>
      </div>
    </div>
    <template v-if="['events', 'changes'].includes(kind)">
      <div class="chart-section">
        <div class="chart-heading">
          <h3>事件分布</h3>
          <span>共 {{ rows.length }} 条 · 点击记录回放</span>
        </div>
        <AtlasChart
          :option="events"
          :height="280"
          label="按类别展示事件发生时间"
          @select="emit('select', $event)"
        />
      </div>
      <div class="audit-feed">
        <article v-for="r in rows.slice(0, 100)" :key="r.id">
          <i></i>
          <div>
            <div class="audit-heading">
              <b>{{ labels[r.kind] || r.kind }}</b>
              <time>{{ clock(r.time, true) }}</time>
            </div>
            <p v-if="kind === 'events'">{{ r.name || r.profile_id || r.mac }} · {{ r.text }}</p>
            <p v-else-if="r.kind === 'rename'">
              {{ r.before?.name || '未命名' }}
              <span class="rename-arrow">→</span>
              <b>{{ r.after?.name }}</b>
              <small>{{ r.subject }}</small>
            </p>
            <p v-else-if="r.kind === 'topology'">
              保存 {{ r.after?.nodes?.length || 0 }} 个节点、{{ r.after?.links?.length || 0 }}
              条连接
            </p>
            <p v-else>迁入时的设备名称基线</p>
            <details v-if="r.kind === 'topology' && r.after?.nodes">
              <summary>查看当时拓扑</summary>
              <ArchiveTopology :topology="r.after" />
            </details>
            <button class="text-button" @click="emit('select', r.time)">回放此时状态 →</button>
          </div>
        </article>
        <p v-if="rows.length > 100" class="chart-note">
          时间线显示最近 100 条，完整已加载记录可在下方明细查看或导出。
        </p>
      </div>
    </template>
  </template>
</template>
