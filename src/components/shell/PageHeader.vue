<script setup lang="ts">
defineProps<{ title: string; description?: string }>()
</script>
<template>
  <header class="page-header">
    <div><h2>{{ title }}</h2><p v-if="description">{{ description }}</p><slot name="description" /></div>
    <slot />
  </header>
</template>
<style scoped>
.page-header { min-height: 76px; padding: 14px 24px; display: flex; align-items: center; justify-content: space-between; gap: 16px; border-bottom: 1px solid var(--border); }
.page-header > div { min-width: 0; }
h2 { margin: 0; font-size: var(--fs-xl); font-weight: 650; }
p { margin: 6px 0 0; color: var(--fg-muted); font-size: var(--fs-sm); }
@media(max-width: 860px) { .page-header { padding: 18px 16px; flex-wrap: wrap; } }
</style>
