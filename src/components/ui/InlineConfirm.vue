<script setup lang="ts">
import { nextTick, ref, watch } from 'vue'

const props = withDefaults(defineProps<{ label?: string; confirmLabel?: string; busy?: boolean }>(), {
  label: '删除', confirmLabel: '确认删除', busy: false
})
const emit = defineEmits<{ confirm: [] }>()
const confirming = ref(false)
const submitted = ref(false)
const trigger = ref<HTMLButtonElement | null>(null)
const confirmation = ref<HTMLElement | null>(null)

async function begin() {
  confirming.value = true
  await nextTick()
  // 附加选择（例如默认供应商替代项）优先获得焦点。
  confirmation.value?.querySelector<HTMLElement>('select, input, textarea, button')?.focus()
}

async function cancel() {
  if (props.busy) return
  confirming.value = false
  submitted.value = false
  await nextTick()
  trigger.value?.focus()
}

async function confirm() {
  if (props.busy || submitted.value) return
  submitted.value = true
  emit('confirm')
  await nextTick()
  // 异步操作由 busy 控制；同步操作完成后立即回到原按钮。
  if (!props.busy) await cancel()
}

watch(() => props.busy, (busy, wasBusy) => {
  if (wasBusy && !busy && submitted.value) void cancel()
})
</script>

<template>
  <span class="inline-confirm">
    <button v-if="!confirming" ref="trigger" type="button" class="btn sm danger" :disabled="busy" @click="begin">{{ label }}</button>
    <span v-else ref="confirmation" class="inline-confirm" role="group" :aria-label="confirmLabel" :aria-busy="busy" @keydown.esc.stop.prevent="cancel">
      <span class="inline-confirm-prompt">{{ confirmLabel }}？</span>
      <span v-if="$slots.default" class="inline-confirm-extra"><slot /></span>
      <button type="button" class="btn sm danger" :disabled="busy || submitted" @click="confirm">{{ busy ? '处理中…' : label }}</button>
      <button type="button" class="btn sm ghost" :disabled="busy" @click="cancel">取消</button>
    </span>
  </span>
</template>
