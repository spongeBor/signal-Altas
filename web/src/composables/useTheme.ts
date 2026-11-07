import { computed, ref } from 'vue'
import { chartColors } from '../lib/historyCharts'

export type SkinId = 'night' | 'graphite' | 'day' | 'paper'
type Base = 'dark' | 'light'

// A skin always keeps one of the two structural themes (the components are
// tuned for those) and adds a palette on top through data-skin.
export const skins: { id: SkinId; name: string; base: Base; swatch: [string, string] }[] = [
  { id: 'night', name: '深空蓝', base: 'dark', swatch: ['#0e1a2a', '#5bcffa'] },
  { id: 'graphite', name: '石墨', base: 'dark', swatch: ['#141417', '#f0b429'] },
  { id: 'day', name: '白昼', base: 'light', swatch: ['#ffffff', '#2563eb'] },
  { id: 'paper', name: '暖纸', base: 'light', swatch: ['#faf6ee', '#b45309'] },
]

const key = 'signal-atlas-skin'
const valid = (v: string | null): v is SkinId =>
  v === 'night' || v === 'graphite' || v === 'day' || v === 'paper'

function savedSkin(): SkinId {
  try {
    const stored = localStorage.getItem(key)
    if (valid(stored)) return stored
    // Migrate the previous binary theme choice.
    if (localStorage.getItem('signal-atlas-theme') === 'light') return 'day'
  } catch {
    /* Private browsing may deny storage. */
  }
  return 'night'
}

export const skin = ref<SkinId>(savedSkin())
export const theme = computed<Base>(() => skins.find((s) => s.id === skin.value)?.base || 'dark')
export const skinName = computed(() => skins.find((s) => s.id === skin.value)?.name || '深空蓝')

function apply() {
  document.documentElement.dataset.theme = theme.value
  document.documentElement.dataset.skin = skin.value
}
apply()

export function setSkin(id: SkinId) {
  skin.value = id
  apply()
  try {
    localStorage.setItem(key, id)
  } catch {
    /* Private browsing may deny storage. */
  }
}

export function toggleTheme() {
  setSkin(theme.value === 'dark' ? 'day' : 'night')
}

const darkChart = {
  text: '#94abc2',
  line: '#23364b',
  split: '#1c2b3e',
  surface: '#101e30',
  ink: '#e5edf7',
  slider: '#091320',
  accent: '#52c8fa',
  handle: '#203a52',
  filler: '#52c8fa20',
  colors: ['#52c8fa', '#47dec0', '#b7a0ff', '#ffc17b', '#fb91ad', '#7baaff', '#c0d2e5', '#dce99c'],
}
const graphiteChart = {
  text: '#a9a49b',
  line: '#2e2e34',
  split: '#242429',
  surface: '#17171b',
  ink: '#f0efec',
  slider: '#0c0c0e',
  accent: '#f0b429',
  handle: '#2a2a30',
  filler: '#f0b42920',
  colors: ['#f0b429', '#7fd1c0', '#b8a1ff', '#ff9f7b', '#f78fb3', '#8fb8ff', '#c8c4bb', '#e0e3a0'],
}
const dayChart = {
  text: '#64748b',
  line: '#e2e8f0',
  split: '#edf1f6',
  surface: '#ffffff',
  ink: '#0f172a',
  slider: '#f1f5f9',
  accent: '#2563eb',
  handle: '#ffffff',
  filler: '#2563eb18',
  colors: chartColors,
}
const paperChart = {
  text: '#7a6e5f',
  line: '#e5dcc9',
  split: '#efe7d6',
  surface: '#fbf7ef',
  ink: '#2f2a24',
  slider: '#f3ecdf',
  accent: '#b45309',
  handle: '#fbf7ef',
  filler: '#b4530920',
  colors: ['#b45309', '#0f766e', '#7c6bb0', '#c2793a', '#b5566d', '#5b7fb0', '#a89a86', '#8b8b3a'],
}

export const chartAppearance = computed(() => {
  switch (skin.value) {
    case 'graphite':
      return graphiteChart
    case 'day':
      return dayChart
    case 'paper':
      return paperChart
    default:
      return darkChart
  }
})
