<script setup lang="ts">
import { theme, toggleTheme } from '../composables/useTheme'
import { t } from '../i18n'
import { Sun, Moon } from 'lucide-vue-next'
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { RadioTower, ArrowRight, ShieldCheck } from 'lucide-vue-next'
const route = useRoute(),
  router = useRouter(),
  username = ref(''),
  password = ref(''),
  busy = ref(false),
  error = ref('')
function next() {
  const n = String(route.query.next || '/history')
  return n.startsWith('/') && !n.startsWith('//') && !n.startsWith('/login') ? n : '/history'
}
async function login() {
  busy.value = true
  error.value = ''
  try {
    const r = await fetch('/auth/login', {
      method: 'POST',
      credentials: 'same-origin',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username: username.value, password: password.value }),
    })
    const d = await r.json()
    if (!r.ok) throw new Error(d.error || '登录未成功')
    password.value = ''
    await router.replace(next())
  } catch (e) {
    error.value = (e as Error).message
  } finally {
    busy.value = false
  }
}
onMounted(async () => {
  try {
    const r = await fetch('/auth/session', { credentials: 'same-origin' })
    if (r.ok && (await r.json()).authenticated) await router.replace(next())
  } catch {
    error.value = '暂时无法连接工作台，请稍后重试'
  }
})
</script>
<template>
  <div class="login-page">
    <button
      class="button subtle login-theme"
      @click="toggleTheme"
      :aria-label="theme === 'dark' ? '切换浅色外观' : '切换深色外观'"
    >
      <Sun v-if="theme === 'dark'" :size="16" />
      <Moon v-else :size="16" />
      {{ theme === 'dark' ? '浅色外观' : '深色外观' }}
    </button>
    <section class="login-card">
      <div class="login-brand">
        <RadioTower :size="33" stroke-width="1.5" />
        <span>
          Signal Atlas
          <small>家庭网络观测台</small>
        </span>
      </div>
      <h1>{{ t('login.title') }}</h1>
      <p class="login-desc">{{ t('login.desc') }}</p>
      <form @submit.prevent="login">
        <label class="field">
          <span>{{ t('login.user') }}</span>
          <input v-model="username" name="username" autocomplete="username" required autofocus />
        </label>
        <label class="field">
          <span>{{ t('login.password') }}</span>
          <input
            v-model="password"
            name="password"
            type="password"
            autocomplete="current-password"
            required
          />
        </label>
        <div v-if="error" class="login-error" role="alert">{{ error }}</div>
        <button class="button primary" :disabled="busy" type="submit">
          {{ busy ? t('login.busy') : t('login.submit') }}
          <ArrowRight :size="16" />
        </button>
      </form>
      <p class="login-note">
        <ShieldCheck :size="16" />
        {{ t('login.note') }}
      </p>
    </section>
  </div>
</template>
