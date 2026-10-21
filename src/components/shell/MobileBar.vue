<script setup lang="ts">
import { Menu } from 'lucide-vue-next'
defineProps<{ title: string }>()
defineEmits<{ menu: [] }>()
</script>
<template><div class="mobile-bar"><button class="btn ghost icon" aria-label="菜单" @click="$emit('menu')"><Menu :size="19" /></button><span>{{ title }}</span></div></template>
<style scoped>
.mobile-bar { display: none; }
@media(max-width: 860px) { .mobile-bar { display: flex; align-items: center; gap: 10px; padding: max(8px, env(safe-area-inset-top)) max(12px, env(safe-area-inset-right)) 8px max(12px, env(safe-area-inset-left)); background: var(--bg); border-bottom: 1px solid var(--border); min-height: 48px; } span { overflow: hidden; white-space: nowrap; text-overflow: ellipsis; font-weight: 600; } }
</style>
