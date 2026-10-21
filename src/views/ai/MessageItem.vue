<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { Activity, Bot, Check, Copy, Square } from 'lucide-vue-next'
import Notice from '../../components/ui/Notice.vue'
import type { AIMessage, AINotice } from '../../types'
import { decorateCitations, messageStatusLabel, type NumberedCitation } from './chat'

const props = defineProps<{
  message: AIMessage
  notice: AINotice | null
  citations: NumberedCitation[]
  renderer: ((content: string) => string) | null
  streaming: boolean
  diagnosticsHref: string
}>()
const emit = defineEmits<{ citation: [key: string]; stop: [] }>()
const copied = ref(false)
const copyHint = ref('')
const contentElement = ref<HTMLElement | null>(null)
let copyTimer: ReturnType<typeof setTimeout> | undefined
const html = computed(() => {
  if (!props.renderer) return ''
  const rendered = props.renderer(props.message.content)
  return typeof document === 'undefined' ? rendered : decorateCitations(rendered, props.message.id, props.citations)
})
const status = computed(() => messageStatusLabel(props.message))
function onContentClick(event: MouseEvent) {
  const button = (event.target as HTMLElement).closest<HTMLButtonElement>('button[data-citation-key]')
  if (button?.dataset.citationKey) emit('citation', button.dataset.citationKey)
}
async function copy() {
  copyHint.value = ''
  try {
    await navigator.clipboard.writeText(props.message.content)
    copied.value = true
    clearTimeout(copyTimer)
    copyTimer = setTimeout(() => { copied.value = false }, 1800)
  } catch {
    // 浏览器未授权剪贴板时选中可见回答，允许用户直接使用系统复制快捷键。
    if (contentElement.value) {
      const range = document.createRange()
      range.selectNodeContents(contentElement.value)
      const selection = window.getSelection()
      selection?.removeAllRanges()
      selection?.addRange(range)
    }
    copyHint.value = '已选中回答，请使用系统复制快捷键'
  }
}
onBeforeUnmount(() => clearTimeout(copyTimer))
</script>

<template>
  <article class="message" :class="message.role" :aria-label="message.role === 'user' ? '提问' : '回答'">
    <template v-if="message.role === 'user'"><div class="question">{{ message.content }}</div></template>
    <template v-else>
      <header class="answer-head"><span class="avatar"><Bot :size="14" /></span><span>{{ message.provider_name || 'DocHarbor' }} · <span class="model">{{ message.model || '等待回答' }}</span></span></header>
      <div ref="contentElement" class="answer-body" @click="onContentClick">
        <div v-if="renderer" v-html="html"></div>
        <div v-else class="plain-content">{{ message.content }}</div>
      </div>
      <Notice v-if="notice" :variant="notice.type === 'ai_disabled' ? 'info' : 'warn'">{{ notice.message }}</Notice>
      <div v-if="status" class="message-state" :class="message.status" role="status">
        <span>{{ status }}</span>
        <button v-if="streaming && message.status === 'streaming'" class="btn sm ghost" type="button" @click="$emit('stop')"><Square :size="12" />停止</button>
        <small v-if="message.error_message">{{ message.error_message }}</small>
      </div>
      <footer class="answer-actions">
        <button class="btn ghost sm" type="button" :disabled="!message.content" @click="copy"><Check v-if="copied" :size="14" /><Copy v-else :size="14" />{{ copied ? '已复制' : '复制' }}</button>
        <a class="btn ghost sm" :href="diagnosticsHref"><Activity :size="14" />查看诊断</a>
        <span v-if="copyHint" role="status">{{ copyHint }}</span>
      </footer>
    </template>
  </article>
</template>

<style scoped>
.message { min-width: 0; display: flex; flex-direction: column; gap: 8px; color: var(--fg); }
.message.user { align-self: flex-end; max-width: 85%; }
.question { padding: 9px 14px; border-radius: 12px 12px 4px 12px; background: var(--surface-2); font-size: var(--fs-lg); line-height: 1.6; white-space: pre-wrap; overflow-wrap: anywhere; }
.answer-head { display: flex; align-items: center; gap: 8px; font-size: var(--fs-sm); color: var(--fg-muted); overflow-wrap: anywhere; }
.avatar { flex-shrink: 0; width: 22px; height: 22px; border-radius: 6px; background: var(--accent-subtle); color: var(--accent-text); display: grid; place-items: center; }
.model { font-family: var(--font-mono); }
.answer-body { min-width: 0; font-size: var(--fs-lg); line-height: 1.75; overflow-wrap: anywhere; }
.plain-content { white-space: pre-wrap; }
.answer-body :deep(p) { margin: 0 0 10px; }
.answer-body :deep(ul), .answer-body :deep(ol) { margin: 0 0 10px; padding-left: 22px; }
.answer-body :deep(li) { margin: 2px 0; }
.answer-body :deep(h1), .answer-body :deep(h2), .answer-body :deep(h3) { margin: 18px 0 10px; line-height: 1.4; font-size: 19px; }
.answer-body :deep(code) { background: var(--surface-2); border-radius: 4px; padding: 1px 5px; font: 0.86em/1.6 var(--font-mono); }
.answer-body :deep(pre) { max-width: 100%; overflow-x: auto; padding: 12px; background: var(--surface-2); border-radius: 6px; }
.answer-body :deep(pre code) { padding: 0; }
.answer-body :deep(a) { color: var(--accent-text); text-decoration: underline; text-decoration-color: var(--border-strong); text-underline-offset: 3px; }
.answer-body :deep(table) { display: block; overflow-x: auto; max-width: 100%; border-collapse: collapse; margin: 12px 0; }
.answer-body :deep(th), .answer-body :deep(td) { border: 1px solid var(--border); padding: 6px 10px; }
.answer-body :deep(th) { background: var(--surface-2); }
.answer-body :deep(blockquote) { margin: 12px 0; padding-left: 12px; border-left: 3px solid var(--border-strong); color: var(--fg-muted); }
.answer-body :deep(img) { max-width: 100%; height: auto; }
.answer-body :deep(hr) { border: 0; border-top: 1px solid var(--border); }
.answer-body :deep(.citation-mark) { display: inline-flex; align-items: center; padding: 2px 4px; margin: 0 1px; border: 0; border-radius: 4px; background: var(--accent-subtle); color: var(--accent-text); font: 600 10.5px/1.2 var(--font-mono); vertical-align: 2px; }
.answer-body :deep(button.citation-mark) { cursor: pointer; }
.answer-body :deep(button.citation-mark:hover) { background: var(--accent); color: var(--accent-fg); }
.answer-body :deep(button.citation-mark:focus-visible) { outline: 2px solid var(--accent); outline-offset: 2px; }
.message-state { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; color: var(--fg-muted); font-size: var(--fs-sm); }
.message-state.failed { color: var(--danger); }
.message-state.partial { color: var(--warn); }
.message-state small { width: 100%; overflow-wrap: anywhere; }
.answer-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 4px; color: var(--fg-muted); }
.answer-actions > span { font-size: var(--fs-xs); }
</style>
