<script setup lang="ts">
import type { ProfileResult } from '../types'
import { computed } from 'vue'
import OpticalPanel from './OpticalPanel.vue'
import { ontTables, ontFields, fecLabels, ontValue } from '../lib/ont'
import { fmt, clock, uptime } from '../composables/useAtlas'
const props = defineProps<{ profile: ProfileResult }>()
const visibleOntTables = computed(() =>
  Object.fromEntries(
    Object.entries(props.profile.data.tables || {}).filter(([key]) => key !== 'DevInfo'),
  ),
)
</script>
<template>
  <template v-if="profile.adapter !== 'zte'">
    <div class="eyebrow">基础设备</div>
    <h2>{{ profile.name }}</h2>
    <p class="muted mono">{{ profile.host }}</p>
    <div class="detail-list">
      <div>
        <span>采集状态</span>
        <b>
          {{
            profile.status === 'ok'
              ? '正常'
              : profile.status === 'needs_auth'
                ? '待输入凭据'
                : profile.error
          }}
        </b>
      </div>
      <div>
        <span>型号</span>
        <b>{{ profile.data.model || '—' }}</b>
      </div>
      <div>
        <span>固件</span>
        <b>{{ profile.data.firmware || '—' }}</b>
      </div>
      <div>
        <span>最近采集</span>
        <b>{{ clock(profile.collected_at, true) }}</b>
      </div>
      <div v-if="profile.data.uptime != null">
        <span>运行时间</span>
        <b>{{ uptime(profile.data.uptime) }}</b>
      </div>
    </div>
  </template>
  <template v-if="profile.adapter === 'openwrt'">
    <h3>软路由</h3>
    <div class="detail-list">
      <div>
        <span>WAN状态</span>
        <b>{{ profile.data.wan_up ? '已连接' : '未确认连接' }}</b>
      </div>
      <div>
        <span>CPU占用</span>
        <b>{{ fmt(profile.data.cpu_percent, '%', 1) }}</b>
      </div>
      <div>
        <span>网口 eth0 / eth1</span>
        <b>{{ profile.data.link_speeds?.map((n) => n + 'Mbps').join(' / ') || '—' }}</b>
      </div>
      <div>
        <span>WAN连接时长</span>
        <b>{{ uptime(profile.data.wan_uptime) }}</b>
      </div>
      <div>
        <span>WAN 地址</span>
        <b>
          {{ profile.data.wan_ip || '未获取' }}
          <small v-if="profile.data.wan_cgnat" class="muted">（运营商内网 / CGNAT）</small>
        </b>
      </div>
    </div>
  </template>
  <template v-if="profile.adapter === 'asus'">
    <h3>无线电</h3>
    <div v-for="r in profile.data.radios" :key="r.interface" class="subcard">
      <b>{{ r.ssid }}</b>
      <p>{{ r.band }} · {{ r.width_mhz }}MHz · 信道 {{ r.channel }}</p>
      <p>{{ r.clients }} 条关联 · 信道利用率 {{ fmt(r.utilization, '%') }}</p>
    </div>
    <div class="detail-list">
      <div>
        <span>漫游阈值</span>
        <b>{{ profile.data.settings?.roaming_thresholds.join(' / ') || '—' }}</b>
      </div>
      <div>
        <span>传输公平性</span>
        <b>{{ profile.data.settings?.airtime_fairness.join(' / ') || '—' }}</b>
      </div>
    </div>
  </template>
  <template v-if="profile.adapter === 'zte'">
    <OpticalPanel :profile="profile" compact />
    <h3 v-if="profile.data.fec">前向纠错统计（FEC）</h3>
    <div v-if="profile.data.fec" class="detail-list">
      <div v-for="(value, key) in profile.data.fec" :key="key">
        <span>{{ fecLabels[key] || key }}</span>
        <b>{{ value }}</b>
      </div>
    </div>
    <div v-for="(rows, key) in visibleOntTables" :key="key" class="subcard">
      <b>{{ ontTables[key] || key }}</b>
      <div v-for="(row, i) in rows" :key="i" class="ont-table-row">
        <small v-if="rows.length > 1">连接 {{ i + 1 }}</small>
        <div v-for="(v, k) in row" :key="k">
          <span>{{ ontFields[k] || k }}</span>
          <b>{{ ontValue(k, v) }}</b>
        </div>
      </div>
    </div>
  </template>
  <template v-if="profile.adapter === 'esxi'">
    <h3>物理网口</h3>
    <div v-for="p in profile.data.pnics" :key="p.device" class="subcard">
      <b>{{ p.device }} · {{ p.link_mbps ? `${p.link_mbps}Mbps` : '未确认链路' }}</b>
      <p class="mono">{{ p.mac }}</p>
    </div>
    <h3>管理口</h3>
    <div v-for="p in profile.data.vmkernel_interfaces" :key="p.device" class="subcard">
      <b>{{ p.device }} · {{ p.ip }}</b>
      <p>{{ p.portgroup }} · {{ p.dhcp ? 'DHCP' : '静态地址' }}</p>
    </div>
    <h3>虚拟机</h3>
    <div v-for="vm in profile.data.vms" :key="vm.name" class="subcard">
      <b>{{ vm.name }} · {{ vm.power_state }}</b>
      <p>{{ vm.network_cards.length }}张虚拟网卡 · {{ vm.pci_passthrough.length }}张PCI直通设备</p>
    </div>
  </template>
  <p v-if="profile.data.note" class="hint">{{ profile.data.note }}</p>
  <a
    class="button secondary"
    :href="`${profile.adapter === 'esxi' ? 'https' : 'http'}://${profile.host}${profile.adapter === 'esxi' ? '/ui/' : ''}`"
    target="_blank"
    rel="noopener noreferrer"
  >
    打开管理后台 ↗
  </a>
</template>
