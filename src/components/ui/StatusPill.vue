<script setup lang="ts">
import { computed } from 'vue'
import { statusLabel, statusVariant } from '../../lib/labels'

const props = defineProps<{
  status: string
  label?: string
  variant?: 'ok' | 'warn' | 'danger' | 'info' | 'neutral'
}>()
// 原始状态保留在 title 中，页面统一通过 labels 模块显示中文。
const text = computed(() => props.label ?? statusLabel(props.status))
const tone = computed(() => props.variant ?? statusVariant(props.status))
</script>

<template>
  <span class="status-pill pill" :class="tone" :title="status">
    <span class="dot" aria-hidden="true"></span>{{ text }}
  </span>
</template>
