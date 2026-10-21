<script setup lang="ts">
import { GitBranch } from 'lucide-vue-next'
defineProps<{ label: string; neutral?: boolean }>()
</script>

<template>
  <span class="branch-tag branch" :class="{ neutral }" :title="label">
    <GitBranch :size="12" aria-hidden="true" />
    <span class="branch-tag-label">{{ label }}</span>
  </span>
</template>
