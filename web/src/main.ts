import { createApp } from 'vue'
import { createRouter, createWebHistory } from 'vue-router'
import App from './App.vue'
import './style.css'
import './theme.css'
const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: '/login', component: () => import('./views/Login.vue') },
    { path: '/', component: () => import('./views/Overview.vue') },
    { path: '/devices', component: () => import('./views/Devices.vue') },
    { path: '/traffic', component: () => import('./views/Traffic.vue') },
    { path: '/connections', component: () => import('./views/Connections.vue') },
    { path: '/history', component: () => import('./views/History.vue') },
    { path: '/:pathMatch(.*)*', redirect: '/' },
  ],
})
// Every deploy replaces all hashed chunks at once. A tab opened before it keeps
// running the old shell, so its next route change asks for a chunk that no
// longer exists and the page dies with "Failed to fetch dynamically imported
// module". Reload into the current shell instead of leaving a blank screen; the
// stored path stops a loop when the chunk is genuinely missing.
const CHUNK_RETRY = 'signal-atlas:chunk-retry'
router.onError((error, to) => {
  const message = String((error as Error)?.message || error)
  if (!/dynamically imported module|Importing a module script failed/i.test(message)) return
  const path = to?.fullPath || location.pathname
  if (sessionStorage.getItem(CHUNK_RETRY) === path) return
  sessionStorage.setItem(CHUNK_RETRY, path)
  location.assign(path)
})
router.afterEach(() => sessionStorage.removeItem(CHUNK_RETRY))
createApp(App).use(router).mount('#app')
