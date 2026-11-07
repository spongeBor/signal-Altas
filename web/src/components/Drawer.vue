<script setup lang="ts">
import { onMounted, onUnmounted } from 'vue'
import { X } from 'lucide-vue-next'
const emit = defineEmits<{ close: [] }>()
function key(e: KeyboardEvent) {
  if (e.key === 'Escape') emit('close')
}
onMounted(() => document.addEventListener('keydown', key))
onUnmounted(() => document.removeEventListener('keydown', key))
</script>
<template>
  <Teleport to="body">
    <div class="drawer-backdrop" @click="emit('close')"></div>
    <section class="drawer" role="dialog" aria-modal="true" aria-label="设备详情">
      <button class="icon-button drawer-close" aria-label="关闭详情" @click="emit('close')">
        <X :size="18" />
      </button>
      <slot />
    </section>
  </Teleport>
</template>
