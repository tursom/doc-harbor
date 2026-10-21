<script setup lang="ts">
import { nextTick, onBeforeUnmount, ref, useId, watch } from 'vue'
import { Check, Copy, Eye, EyeOff } from 'lucide-vue-next'

const props = withDefaults(defineProps<{ value: string; masked?: boolean; label?: string; revealable?: boolean }>(), { masked: false, label: '', revealable: true })
const input = ref<HTMLInputElement | null>(null)
const revealed = ref(false)
const copied = ref(false)
const feedback = ref('')
const inputId = useId()
const feedbackId = useId()
let timer: ReturnType<typeof setTimeout> | undefined

// 外层重新要求遮罩或内容被轮换时，不能沿用上一次的显示/复制状态。
watch(() => [props.masked, props.value], () => {
  clearTimeout(timer)
  revealed.value = false
  copied.value = false
  feedback.value = ''
})

async function copy() {
  clearTimeout(timer)
  try {
    await navigator.clipboard.writeText(props.value)
    copied.value = true
    feedback.value = '已复制'
    timer = setTimeout(() => { copied.value = false; feedback.value = '' }, 2000)
  } catch {
    copied.value = false
    // Clipboard 在非安全上下文或权限受限时不可用。显示真实值后选中，交给用户手动复制。
    revealed.value = true
    await nextTick()
    input.value?.focus()
    input.value?.select()
    feedback.value = '已选中文字，请手动复制'
  }
}

onBeforeUnmount(() => clearTimeout(timer))
</script>

<template>
  <div class="copy-field">
    <label v-if="label" :for="inputId" class="copy-field-label">{{ label }}</label>
    <div class="copy-field-row">
      <input :id="inputId" ref="input" :type="masked && !revealed ? 'password' : 'text'" :value="value" readonly :aria-label="label || '待复制内容'" :aria-describedby="feedback ? feedbackId : undefined" autocomplete="off" spellcheck="false" />
      <button v-if="masked && revealable" type="button" class="btn icon ghost" :aria-label="revealed ? '隐藏内容' : '显示内容'" :aria-pressed="revealed" @click="revealed = !revealed">
        <component :is="revealed ? EyeOff : Eye" aria-hidden="true" />
      </button>
      <button type="button" class="btn sm" @click="copy"><component :is="copied ? Check : Copy" aria-hidden="true" />{{ copied ? '已复制' : '复制' }}</button>
    </div>
    <span :id="feedbackId" class="copy-field-feedback" role="status">{{ feedback }}</span>
  </div>
</template>
