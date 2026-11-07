<script setup lang="ts">
import { computed } from 'vue'
import { Cpu, Radio, Server, Network, ArrowUpRight } from 'lucide-vue-next'
import type { ProfileResult, Topology } from '../types'
import { clock, fmt } from '../composables/useAtlas'
import {
  summaryMetrics,
  freshProfile,
  duration,
  powerState,
  switchRecord,
  recordedPorts,
} from '../lib/deviceSummary'
const props = defineProps<{ profile: ProfileResult; topology: Topology | null }>()
const emit = defineEmits<{ details: [id: string] }>()
const fresh = computed(() => freshProfile(props.profile))
const metrics = computed(() => summaryMetrics(props.profile))
const radios = computed(() => (fresh.value ? props.profile.data.radios || [] : []))
const vms = computed(() => (fresh.value ? props.profile.data.vms || [] : []))
const record = computed(() =>
  props.profile.adapter === 'tcp' ? switchRecord(props.topology, props.profile.host) : null,
)
const ports = computed(() => recordedPorts(record.value))
const icon = computed(
  () =>
    ({ openwrt: Cpu, asus: Radio, esxi: Server, tcp: Network })[
      props.profile.adapter as 'openwrt' | 'asus' | 'esxi' | 'tcp'
    ] || Server,
)
const source = computed(() =>
  props.profile.adapter === 'esxi'
    ? 'ESXi 认证只读接口'
    : props.profile.adapter === 'tcp'
      ? 'TCP 管理端口探测'
      : '设备只读采集（SSH）',
)
const status = computed(() =>
  fresh.value
    ? '本轮采集正常'
    : props.profile.status === 'needs_auth'
      ? '待输入采集凭据'
      : props.profile.status === 'error'
        ? '本轮采集失败'
        : '采样已过期',
)
</script>
<template>
  <section class="panel device-summary" :class="'summary-' + profile.adapter">
    <div class="panel-heading">
      <div>
        <h2>
          <component :is="icon" :size="18" />
          {{ profile.name }}
        </h2>
        <p>
          {{
            profile.adapter === 'tcp'
              ? record?.model || '管理服务探测'
              : profile.data.model || '型号未读取'
          }}
          ·
          <span class="mono">{{ profile.host }}</span>
        </p>
      </div>
      <span class="tag" :class="{ dim: !fresh }">{{ status }}</span>
    </div>
    <p v-if="!fresh" class="summary-warning">
      {{ profile.error || '没有本轮有效观测，未知指标不填零，也不等同于设备离线。' }}
    </p>
    <div class="summary-metrics">
      <article v-for="m in metrics" :key="m.label">
        <span>{{ m.label }}</span>
        <b :class="{ text: m.text }">
          {{ m.value }}
          <small v-if="m.unit">{{ m.unit }}</small>
        </b>
        <p v-if="m.detail">{{ m.detail }}</p>
      </article>
    </div>
    <div v-if="profile.adapter === 'asus' && radios.length" class="summary-radios">
      <article v-for="r in radios" :key="r.interface">
        <div>
          <b>
            {{ r.band }}
            <small>{{ r.ssid }}</small>
          </b>
          <span>{{ r.clients }} 条关联</span>
        </div>
        <small v-if="r.bss && r.bss.length > 1" class="radio-bss">
          {{ r.bss.map((b) => `${b.ssid} ${b.clients}`).join(' · ') }}
        </small>
        <div class="radio-facts">
          <span>
            信道
            <b>{{ r.channel ?? '未读取' }}</b>
          </span>
          <span>
            带宽
            <b>{{ r.width_mhz == null ? '未读取' : r.width_mhz + ' MHz' }}</b>
          </span>
          <span>
            利用率
            <b>{{ fmt(r.utilization, '%', 0) }}</b>
          </span>
        </div>
        <small class="muted">{{ r.interface }} · 利用率由 AP 报告，仅代表本轮采样</small>
      </article>
    </div>
    <div v-if="profile.adapter === 'esxi' && fresh" class="summary-esxi">
      <div class="summary-row" v-for="v in vms" :key="v.name">
        <span>{{ v.name }}</span>
        <b>{{ powerState(v.power_state) }} · {{ v.pci_passthrough.length }} 个直通设备</b>
      </div>
      <p>
        虚拟交换机 {{ profile.data.vswitches?.length ?? '—' }} 个 · 端口组
        {{ profile.data.portgroups?.length ?? '—' }} 个
      </p>
      <small>管理网口与虚拟机直通网卡分别统计。</small>
    </div>
    <div v-if="profile.adapter === 'tcp'" class="summary-switch-record">
      <div>
        <b>端口核对记录</b>
        <span class="tag dim">历史记录</span>
      </div>
      <p>
        {{ record?.observed_at ? clock(record.observed_at, true) : '尚无已登记的端口记录' }} ·
        当前采集器仅检查管理口
      </p>
      <div v-if="ports.length" class="recorded-port-grid">
        <div v-for="p in ports" :key="p.port">
          <span>{{ p.port }} 号口</span>
          <b>{{ p.label }}</b>
        </div>
      </div>
      <p>端口速率不是本轮读取，不能用它判断当前端口状态。</p>
    </div>
    <details
      v-if="profile.adapter === 'asus' && fresh && profile.data.settings"
      class="summary-extra"
    >
      <summary>查看无线设置摘要</summary>
      <div class="summary-row">
        <span>漫游阈值（2.4 / 5 GHz）</span>
        <b>
          {{
            profile.data.settings.roaming_thresholds
              .map((v) => (v === '' ? '未读取' : v === '0' ? '未启用' : v + ' dBm'))
              .join(' / ')
          }}
        </b>
      </div>
      <div class="summary-row">
        <span>传输公平性（2.4 / 5 GHz）</span>
        <b>
          {{
            profile.data.settings.airtime_fairness
              .map((v) => (v === '0' ? '关闭' : v === '1' ? '开启' : '未读取'))
              .join(' / ')
          }}
        </b>
      </div>
      <div class="summary-row">
        <span>地区代码</span>
        <b>{{ profile.data.settings.region_code || '未读取' }}</b>
      </div>
    </details>
    <div class="summary-footer">
      <div>
        <span>{{ source }}</span>
        <small>
          {{ clock(profile.collected_at, true) }}
          <template v-if="profile.adapter !== 'tcp' && fresh && profile.data.uptime != null">
            · 已运行 {{ duration(profile.data.uptime) }}
          </template>
          <template v-if="profile.data.firmware">· {{ profile.data.firmware }}</template>
        </small>
      </div>
      <button class="text-button" @click="emit('details', profile.id)">
        更多详情
        <ArrowUpRight :size="13" />
      </button>
    </div>
  </section>
</template>
