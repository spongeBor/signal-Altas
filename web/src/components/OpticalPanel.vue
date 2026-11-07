<script setup lang="ts">
import { computed } from 'vue'
import { Cable, ArrowDown, ArrowUp, Thermometer, Zap, Activity } from 'lucide-vue-next'
import type { ProfileResult } from '../types'
import { fmt, clock, uptime } from '../composables/useAtlas'
import { ponLabel } from '../lib/ont'
const props = withDefaults(defineProps<{ profile: ProfileResult; compact?: boolean }>(), {
  compact: false,
})
const available = computed(
  () =>
    props.profile.status === 'ok' && Date.now() - Date.parse(props.profile.collected_at) < 90000,
)
const optics = computed(() =>
  available.value && props.profile.data.optics?.measurement_source === 'zte_qoe_v2'
    ? props.profile.data.optics
    : undefined,
)
const metrics = computed(() => [
  {
    label: '接收光功率',
    value: fmt(optics.value?.rx_dbm, '', 1),
    unit: 'dBm',
    note: '光模块收到的光信号',
    icon: ArrowDown,
  },
  {
    label: '发射光功率',
    value: fmt(optics.value?.tx_dbm, '', 1),
    unit: 'dBm',
    note: '光模块发出的光信号',
    icon: ArrowUp,
  },
  {
    label: '光模块温度',
    value: fmt(optics.value?.temperature_c, '', 0),
    unit: '°C',
    note: '光模块内部传感器',
    icon: Thermometer,
  },
  {
    label: '光猫芯片温度',
    value: fmt(available.value ? props.profile.data.chip_temperature : undefined, '', 1),
    unit: '°C',
    note: '主芯片传感器，与模块不同',
    icon: Thermometer,
  },
  {
    label: '模块供电电压',
    value: fmt(optics.value?.voltage_v, '', 3),
    unit: 'V',
    note: '光模块内部供电',
    icon: Zap,
  },
  {
    label: '激光器偏置电流',
    value: fmt(optics.value?.bias_ma, '', 3),
    unit: 'mA',
    note: '激光发射器的偏置电流',
    icon: Activity,
  },
])
function rate(value?: number | null) {
  return value == null ? '未读取' : value >= 1000 ? `${value / 1000} Gbps` : `${value} Mbps`
}
const details = computed(() => [
  ['光猫硬件版本', props.profile.data.tables?.DevInfo?.[0]?.HardwareVer || '未读取'],
  ['光模块厂商标识', optics.value?.manufacturer || '未读取'],
  ['光模块型号', optics.value?.part_number || '未读取'],
  ['模块版本', optics.value?.revision || '未读取'],
  ['模块日期码', optics.value?.date_code || '未读取'],
  [
    '模块类别',
    optics.value?.module_type === 'XG_BOSA'
      ? 'XG-PON 光收发组件（XG_BOSA）'
      : optics.value?.module_type || '未读取',
  ],
  [
    '数字诊断监测',
    optics.value?.ddm_supported == null ? '未读取' : optics.value.ddm_supported ? '支持' : '不支持',
  ],
  [
    '校准方式',
    optics.value?.calibration === 'internal'
      ? '内部校准'
      : optics.value?.calibration === 'external'
        ? '外部校准，数值未换算'
        : '未确认',
  ],
  ['诊断能力码', optics.value?.monitoring_type || '未读取'],
  ['模块波长 / 告警阈值', '当前读取接口未提供'],
  ['光猫连续运行时间', available.value ? uptime(props.profile.data.uptime) : '未读取'],
])
</script>
<template>
  <section class="panel optical-panel" :class="{ compact }">
    <div class="panel-heading">
      <div>
        <h2>
          <Cable :size="18" />
          光猫与光模块
        </h2>
        <p>
          {{ profile.data.model || '光猫' }} · {{ profile.host }} ·
          {{ profile.data.firmware || '固件未读取' }}
        </p>
      </div>
      <span class="tag" :class="{ dim: !available }">
        {{ available ? ponLabel(profile.data.pon_state) : '等待有效采集' }}
      </span>
    </div>
    <div v-if="!available" class="optical-warning">
      {{ profile.error || '当前未取得有效数据，不能据此判断光模块状态。' }} 最近采集：{{
        clock(profile.collected_at, true)
      }}
    </div>
    <div v-else-if="optics?.status !== 'ok'" class="optical-warning">
      {{ optics?.note || '光模块补充信息尚未读取；未知指标显示为破折号。' }}
    </div>
    <div class="optical-metrics">
      <article v-for="m in metrics" :key="m.label">
        <div>
          <span>{{ m.label }}</span>
          <component :is="m.icon" :size="16" />
        </div>
        <b>
          {{ m.value }}
          <small>{{ m.unit }}</small>
        </b>
        <p>{{ m.note }}</p>
      </article>
    </div>
    <div class="optical-link">
      <span>
        光口下行
        <b>{{ rate(optics?.down_mbps) }}</b>
      </span>
      <span>
        光口上行
        <b>{{ rate(optics?.up_mbps) }}</b>
      </span>
      <span>
        前向纠错（FEC）
        <b>
          {{ optics?.fec_enabled == null ? '未读取' : optics.fec_enabled ? '已启用' : '未启用' }}
        </b>
      </span>
    </div>
    <details class="optical-details" :open="compact">
      <summary>模块资料与采集说明</summary>
      <div class="optical-specs">
        <div v-for="[label, value] in details" :key="label">
          <span>{{ label }}</span>
          <b>{{ value }}</b>
        </div>
      </div>
      <p class="chart-note">
        功率、电压及模块温度使用厂家光学管理服务的业务值，温度按后台整数显示；偏置电流来自底层同一传感器。芯片温度来自独立传感器。光口速率属于物理链路能力，不是宽带测速；未读取模块告警阈值，不依据通用阈值判断正常或故障。
      </p>
      <p class="chart-note">
        数据来源：{{ optics?.source || '等待光模块采集' }}
        <br />
        模块采集时间：{{ clock(optics?.collected_at, true) }}
        <br />
        缺失字段不补零，历史接入前的数据不补造。
      </p>
    </details>
  </section>
</template>
