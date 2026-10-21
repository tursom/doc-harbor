<script setup lang="ts">
import { computed } from 'vue'
import { CircleCheck, CircleX, Info, TriangleAlert } from 'lucide-vue-next'
const props = withDefaults(defineProps<{ variant?: 'info' | 'warn' | 'danger' | 'ok' }>(), { variant: 'info' })
const icon = computed(() => ({ info: Info, warn: TriangleAlert, danger: CircleX, ok: CircleCheck })[props.variant])
</script>

<template>
  <div class="notice" :class="variant" :role="variant === 'danger' ? 'alert' : 'status'">
    <component :is="icon" aria-hidden="true" />
    <div class="notice-content"><slot /></div>
  </div>
</template>
