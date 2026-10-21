<script setup lang="ts">
import { Inbox } from 'lucide-vue-next'
defineProps<{ message: string }>()
</script>

<template>
  <div class="empty-state empty">
    <slot name="icon"><Inbox aria-hidden="true" /></slot>
    <p>{{ message }}</p>
    <div v-if="$slots.default" class="empty-state-actions"><slot /></div>
  </div>
</template>
