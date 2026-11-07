import { computed, ref, shallowRef } from 'vue'
import type { Snapshot } from '../types'
export const snapshot = shallowRef<Snapshot | null>(null)
export const connectionError = ref('')
export const notice = ref('')
export const pending = ref(false)
let timer: ReturnType<typeof setTimeout> | undefined
let poll: ReturnType<typeof setInterval> | undefined
let loading = false
export function toast(text: string) {
  notice.value = text
  if (timer) clearTimeout(timer)
  timer = setTimeout(() => (notice.value = ''), 4500)
}
export async function request<T = unknown>(
  path: string,
  method = 'GET',
  body?: unknown,
): Promise<T> {
  const headers: Record<string, string> = {}
  if (method !== 'GET') headers['X-Atlas-Token'] = snapshot.value?.token || ''
  const form = body instanceof FormData
  if (body !== undefined && !form) headers['Content-Type'] = 'application/json'
  const res = await fetch('/api/v1' + path, {
    method,
    headers,
    body: body === undefined ? undefined : form ? body : JSON.stringify(body),
    credentials: 'same-origin',
  })
  if (res.status === 401 && window.location.pathname !== '/login')
    window.location.replace(
      '/login?next=' + encodeURIComponent(window.location.pathname + window.location.search),
    )
  let data: any
  try {
    data = await res.json()
  } catch {
    throw new Error('后端未返回有效数据，请检查Go服务是否已启动')
  }
  if (!res.ok) throw new Error(data.error || `请求失败（${res.status}）`)
  return data as T
}
export async function load() {
  if (loading) return
  loading = true
  try {
    snapshot.value = await request<Snapshot>('/state')
    connectionError.value = ''
  } catch (e) {
    connectionError.value = (e as Error).message
  } finally {
    loading = false
  }
}
export function startPolling() {
  void load()
  poll = setInterval(() => void load(), 5000)
}
export function stopPolling() {
  if (poll) clearInterval(poll)
}
export async function mutate(path: string, method: string, body?: unknown, message = '已保存') {
  pending.value = true
  try {
    await request(path, method, body)
    await load()
    toast(message)
    return true
  } catch (e) {
    toast((e as Error).message)
    return false
  } finally {
    pending.value = false
  }
}
export async function refresh() {
  await mutate('/refresh', 'POST', undefined, '已请求重新采集，结果将在完成后更新')
}
export const devices = computed(() => snapshot.value?.state.devices || [])
export const profiles = computed(() => snapshot.value?.state.profiles || [])
export const radios = computed(() => snapshot.value?.state.radios || [])
export function fmt(n: number | null | undefined, suffix = '', digits = 0) {
  return typeof n === 'number' && Number.isFinite(n) ? n.toFixed(digits) + suffix : '—'
}
export function clock(value?: string, full = false) {
  const d = value ? new Date(value) : null
  return !d || !Number.isFinite(d.getTime()) || d.getFullYear() < 2000
    ? '尚未采集'
    : d.toLocaleString(
        'zh-CN',
        full
          ? { hour12: false }
          : { hour: '2-digit', minute: '2-digit', second: '2-digit', hour12: false },
      )
}
export function uptime(n?: number) {
  if (n == null) return '—'
  const d = Math.floor(n / 86400),
    h = Math.floor((n % 86400) / 3600),
    m = Math.floor((n % 3600) / 60)
  return `${d ? d + '天 ' : ''}${h}小时 ${m}分`
}
export function strength(n: number | null | undefined) {
  return n == null ? '未知' : n >= -55 ? '很强' : n >= -67 ? '良好' : n >= -75 ? '偏弱' : '很弱'
}
export function signalColor(n: number) {
  return n >= -55 ? '#4d956d' : n >= -67 ? '#8ab66c' : n >= -75 ? '#d0b767' : '#cf8b69'
}
