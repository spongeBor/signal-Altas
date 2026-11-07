<script setup lang="ts">
import { computed, ref, watch, onUnmounted } from 'vue'
import { skin, skins, skinName, setSkin } from './composables/useTheme'
import { locale, setLocale, t } from './i18n'
import { Palette } from 'lucide-vue-next'
import { useRoute, useRouter } from 'vue-router'
import {
  RadioTower,
  LayoutDashboard,
  Network,
  Gauge,
  Settings2,
  RefreshCw,
  Download,
  ArrowUpRight,
  History,
  LogOut,
} from 'lucide-vue-next'
import {
  snapshot,
  connectionError,
  notice,
  pending,
  startPolling,
  stopPolling,
  refresh,
  clock,
} from './composables/useAtlas'
// The list groups a device's links, so count physical devices here as well.
const physicalCount = computed(() => {
  const seen = new Set<string>()
  for (const d of snapshot.value?.state.devices || []) seen.add(d.physical_device_id || d.mac)
  return seen.size
})
const route = useRoute()
const skinsOpen = ref(false)
function pickSkin(id: (typeof skins)[number]['id']) {
  setSkin(id)
  skinsOpen.value = false
}
const router = useRouter()
const isLogin = computed(() => route.path === '/login')
async function logout() {
  await fetch('/auth/logout', { method: 'POST', credentials: 'same-origin' })
  snapshot.value = null
  await router.replace('/login')
}
const titles: Record<string, { label: string; desc: string }> = {
  '/history': { label: 'nav.history', desc: 'page.history.desc' },
  '/': { label: 'nav.overview', desc: 'page.overview.desc' },
  '/devices': { label: 'nav.devices', desc: 'page.devices.desc' },
  '/traffic': { label: 'nav.traffic', desc: 'page.traffic.desc' },
  '/connections': { label: 'nav.settings', desc: 'page.settings.desc' },
}
const page = computed(() => {
  const p = titles[route.path] || titles['/']!
  return { label: t(p.label), title: t(p.label), desc: t(p.desc) }
})
const links = computed(() => [
  { path: '/', label: t('nav.overview'), icon: LayoutDashboard },
  { path: '/devices', label: t('nav.devices'), icon: Network },
  { path: '/traffic', label: t('nav.traffic'), icon: Gauge },
  { path: '/history', label: t('nav.history'), icon: History },
  { path: '/connections', label: t('nav.settings'), icon: Settings2 },
])
watch(
  () => route.path,
  (path) => {
    stopPolling()
    if (path !== '/login') startPolling()
  },
  { immediate: true },
)
onUnmounted(stopPolling)
</script>
<template>
  <RouterView v-if="isLogin" />
  <template v-else>
    <aside class="sidebar">
      <RouterLink to="/" class="brand">
        <RadioTower :size="31" stroke-width="1.5" />
        <span>
          Signal Atlas
          <small>{{ t('app.subtitle') }}</small>
        </span>
      </RouterLink>
      <div class="workspace-label">工作空间</div>
      <nav>
        <RouterLink
          v-for="l in links"
          :key="l.path"
          :to="l.path"
          :aria-label="l.label"
          :title="l.label"
          :class="{ active: route.path === l.path }"
        >
          <component :is="l.icon" :size="19" />
          <span>{{ l.label }}</span>
          <b v-if="l.path === '/devices'">{{ physicalCount || '—' }}</b>
        </RouterLink>
      </nav>
      <div class="sidebar-note">
        <i class="dot green"></i>
        <b>{{ t('shell.noteTitle') }}</b>
        <p>{{ t('shell.noteBody') }}</p>
      </div>
      <div class="sidebar-bottom">
        <span class="avatar">SA</span>
        <span>
          我的家庭网络
          <small>
            {{
              snapshot?.storage === 'postgresql'
                ? 'PostgreSQL 已连接'
                : snapshot?.storage === 'sqlite'
                  ? 'SQLite 本地历史'
                  : '文件持久化'
            }}
          </small>
        </span>
      </div>
    </aside>
    <main>
      <header class="topbar">
        <div class="breadcrumb">
          {{ t('nav.workspace') }}
          <span>/</span>
          {{ page.label }}
        </div>
        <div class="topbar-actions">
          <button
            class="button subtle lang-toggle"
            :title="locale === 'zh' ? 'Switch to English' : '切换到中文'"
            @click="setLocale(locale === 'zh' ? 'en' : 'zh')"
          >
            {{ locale === 'zh' ? 'EN' : '中' }}
          </button>
          <div class="skin-picker">
            <button
              class="button subtle theme-toggle"
              :aria-expanded="skinsOpen"
              aria-haspopup="listbox"
              :title="`外观：${skinName}`"
              @click="skinsOpen = !skinsOpen"
            >
              <Palette :size="15" />
              <span>{{ skinName }}</span>
            </button>
            <template v-if="skinsOpen">
              <div class="skin-backdrop" @click="skinsOpen = false"></div>
              <ul class="skin-menu" role="listbox" aria-label="外观皮肤">
                <li v-for="s in skins" :key="s.id">
                  <button
                    role="option"
                    :aria-selected="skin === s.id"
                    :class="{ active: skin === s.id }"
                    @click="pickSkin(s.id)"
                  >
                    <i :style="{ background: s.swatch[0], borderColor: s.swatch[1] }"></i>
                    <span>{{ s.name }}</span>
                  </button>
                </li>
              </ul>
            </template>
          </div>
          <button v-if="snapshot?.access?.login_enabled" class="button subtle" @click="logout">
            <LogOut :size="14" />
            {{ t('topbar.logout') }}
          </button>
          <span class="live-status">
            <i class="dot" :class="connectionError ? 'amber' : 'green'"></i>
            {{
              connectionError
                ? '等待后端连接'
                : snapshot?.state.collecting
                  ? '正在采集'
                  : t('topbar.live')
            }}
          </span>
          <button class="button subtle" :disabled="pending || !snapshot" @click="refresh">
            <RefreshCw :size="14" :class="{ spinning: snapshot?.state.collecting }" />
            {{ t('topbar.refresh') }}
          </button>
        </div>
      </header>
      <section class="page-heading">
        <div>
          <span class="page-kicker">
            SIGNAL ATLAS
            <i></i>
            {{ t('shell.console') }}
          </span>
          <h1>
            {{ page.title }}
          </h1>
          <p>{{ page.desc }}</p>
        </div>
        <div class="last-updated">
          {{ t('shell.lastCollected') }}
          <time>{{ clock(snapshot?.state.last_updated) }}</time>
        </div>
      </section>
      <div v-if="connectionError" class="alert" role="alert">
        {{ connectionError }}。请确认 Go 后端已启动。
      </div>
      <div v-if="snapshot?.archive?.error" class="alert" role="alert">
        {{ snapshot.archive.error }}。
        <RouterLink to="/history">查看历史存储状态</RouterLink>
      </div>
      <RouterView />
      <footer>
        <span>Signal Atlas</span>
        <small>
          只读观测 · {{ snapshot?.storage === 'postgresql' ? 'PostgreSQL' : '文件存储' }} ·
          所有数据来自采样或注明来源的记录
        </small>
        <a href="/api/v1/export" download="signal-atlas-export.json">
          <Download :size="13" />
          导出观测记录
        </a>
      </footer>
    </main>
    <Transition name="toast">
      <div v-if="notice" class="toast" role="status">{{ notice }}</div>
    </Transition>
  </template>
</template>
