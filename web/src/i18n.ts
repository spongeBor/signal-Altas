import { ref } from 'vue'

// The UI is Chinese-first; the shell and the sign-in page are translated so an
// English reader can get around. Panel-level copy follows as keys are added.
export type Locale = 'zh' | 'en'

const storageKey = 'signal-atlas-locale'
const saved = (): Locale => {
  try {
    return localStorage.getItem(storageKey) === 'en' ? 'en' : 'zh'
  } catch {
    return 'zh'
  }
}

export const locale = ref<Locale>(saved())

const zh: Record<string, string> = {
  'app.subtitle': '家庭网络观测台',
  'nav.workspace': '工作空间',
  'nav.overview': '网络总览',
  'nav.devices': '设备与连接',
  'nav.traffic': '设备流量',
  'nav.history': '历史与回溯',
  'nav.settings': '采集与设置',
  'page.overview.desc': '从入户光纤到无线终端，把状态和连接依据放在一起。',
  'page.devices.desc': '按实际关联和当前应答识别连接，保留历史记录的边界。',
  'page.traffic.desc': '查看当前带宽占用和按设备累计的流量，不读取访问内容。',
  'page.history.desc': '按日期查看流量、无线信号、接入点变化与维护记录。',
  'page.settings.desc': '维护只读采集、管理地址与采样周期。',
  'topbar.logout': '退出登录',
  'topbar.live': '持续观测中',
  'topbar.liveError': '连接中断',
  'topbar.refresh': '刷新数据',
  'login.title': '登录你的网络工作台',
  'login.desc': '查看当前连接，回溯过去的网络状态。',
  'login.user': '账号',
  'login.password': '密码',
  'login.busy': '正在登录…',
  'login.submit': '登录工作台',
  'login.note': '内网与外网使用同一账号，登录后可使用完整功能。',
  'shell.noteTitle': '家庭网络 · 只读采集',
  'shell.noteBody': '管理密码仅在内存中。拓扑、标签与覆盖采样持久保存，历史可按时间查询。',
  'shell.console': '家庭网络控制台',
  'shell.lastCollected': '最近采集',
}

const en: Record<string, string> = {
  'app.subtitle': 'Home network observatory',
  'nav.workspace': 'Workspace',
  'nav.overview': 'Overview',
  'nav.devices': 'Devices',
  'nav.traffic': 'Traffic',
  'nav.history': 'History',
  'nav.settings': 'Collectors',
  'page.overview.desc': 'State and evidence from the fibre line down to every wireless client.',
  'page.devices.desc': 'Devices merged from leases, neighbours and AP associations, with history kept.',
  'page.traffic.desc': 'Current bandwidth and per-device totals; payloads are never inspected.',
  'page.history.desc': 'Traffic, signal, access-point changes and maintenance records by date.',
  'page.settings.desc': 'Read-only collectors, management addresses and the sampling period.',
  'topbar.logout': 'Sign out',
  'topbar.live': 'Collecting',
  'topbar.liveError': 'Disconnected',
  'topbar.refresh': 'Refresh',
  'login.title': 'Sign in to your console',
  'login.desc': 'See the current connections and look back at past network state.',
  'login.user': 'Account',
  'login.password': 'Password',
  'login.busy': 'Signing in…',
  'login.submit': 'Sign in',
  'login.note': 'The same account is used on the LAN and over the internet.',
  'shell.noteTitle': 'Home network · read-only',
  'shell.noteBody': 'Device passwords stay in memory; topology, labels and samples persist and history is queryable.',
  'shell.console': 'Home network console',
  'shell.lastCollected': 'Last collected',
}

export function t(key: string): string {
  return (locale.value === 'en' ? en[key] : zh[key]) ?? zh[key] ?? key
}

export function setLocale(next: Locale) {
  locale.value = next
  try {
    localStorage.setItem(storageKey, next)
  } catch {
    /* private browsing */
  }
}
