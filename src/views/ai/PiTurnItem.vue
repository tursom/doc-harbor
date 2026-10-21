<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue'
import { Activity, Check, Copy, ThumbsDown, ThumbsUp } from 'lucide-vue-next'
import type { PiTurn } from '../../ai-pi-types'
import PiProcess from './PiProcess.vue'
import { decoratePiCitations, isActive, needsNarrowRetry, scopeLabel, turnStatus } from './pi-chat'

const props = defineProps<{
  turn: PiTurn
  renderer: ((content: string) => string) | null
  diagnosticsHref: string
  repoName?: string
  canRetry: boolean
  feedback: (turn: PiTurn, rating: 1 | -1, comment?: string) => Promise<boolean>
}>()
defineEmits<{ retry: [turn: PiTurn]; focus: [] }>()
const html = computed(() => props.renderer ? decoratePiCitations(props.renderer(props.turn.answer), props.turn.citations, !isActive(props.turn)) : '')
const active = computed(() => isActive(props.turn))
const status = computed(() => turnStatus(props.turn))
const diagnosticsURL = computed(() => `${props.diagnosticsHref}${props.diagnosticsHref.includes('?') ? '&' : '?'}run=${props.turn.run_id}`)
const copied = ref(false)
const copyError = ref('')
const commentOpen = ref(false)
const comment = ref(props.turn.feedback?.comment || '')
const savingFeedback = ref(false)
let copyTimer: ReturnType<typeof setTimeout> | undefined
onBeforeUnmount(() => clearTimeout(copyTimer))
async function copy() {
  try {
    await navigator.clipboard.writeText(props.turn.answer)
    copyError.value = ''; copied.value = true
    clearTimeout(copyTimer); copyTimer = setTimeout(() => { copied.value = false }, 2000)
  } catch { copyError.value = '复制失败，请选中回答后复制。' }
}
async function rate(rating: 1 | -1, withComment = false) {
  if (savingFeedback.value) return
  savingFeedback.value = true
  const saved = await props.feedback(props.turn, rating, withComment ? comment.value : '')
  savingFeedback.value = false
  if (saved) commentOpen.value = rating === -1 && !withComment
}
</script>

<template>
  <!-- transcript.question 已由服务端保留为用户原文，不能再次剥掉用户自行输入的 context 示例。 -->
  <article class="pi-turn" :aria-label="`问题：${turn.question}`" @click="$emit('focus')" @focusin="$emit('focus')">
    <div class="question-row"><div class="question-bubble">{{ turn.question }}</div></div>
    <div class="question-tags"><span>{{ turn.role || '未指定' }}</span><span>{{ turn.scope_label || scopeLabel(turn.scope, repoName) }}</span></div>
    <div v-for="(text, i) in turn.supplements || []" :key="i" class="supplement">补充说明：{{ text }}</div>
    <PiProcess :turn="turn" />
    <div v-if="turn.notice" class="turn-notice" role="status">{{ turn.notice }}</div>
    <div v-if="turn.answer" class="pi-answer">
      <div v-if="renderer" class="markdown-body" v-html="html"></div>
      <pre v-else class="plain-answer">{{ turn.answer }}</pre>
    </div>
    <div v-if="status" class="turn-status" :class="{ failure: ['failed', 'timeout'].includes(turn.status) || needsNarrowRetry(turn) }" :role="active ? 'status' : undefined">
      <span>{{ status }}</span>
      <button v-if="!active && ['failed', 'timeout', 'aborted'].includes(turn.status)" type="button" class="btn sm" :disabled="!canRetry" @click="$emit('retry', turn)">{{ needsNarrowRetry(turn) ? '缩小范围后重试' : '重试' }}</button>
    </div>
    <div v-if="!active && turn.answer" class="answer-actions">
      <button type="button" class="btn ghost sm" @click="copy"><Check v-if="copied" :size="14" /><Copy v-else :size="14" />{{ copied ? '已复制' : '复制' }}</button>
      <button type="button" class="btn ghost icon sm" aria-label="有帮助" :aria-pressed="turn.feedback?.rating === 1" :disabled="savingFeedback || turn.run_id <= 0" @click="rate(1)"><ThumbsUp :size="14" /></button>
      <button type="button" class="btn ghost icon sm" aria-label="没有帮助" :aria-pressed="turn.feedback?.rating === -1" :disabled="savingFeedback || turn.run_id <= 0" @click="turn.feedback?.rating === -1 ? commentOpen = !commentOpen : rate(-1)"><ThumbsDown :size="14" /></button>
      <a v-if="turn.run_id > 0" class="btn ghost sm" :href="diagnosticsURL" target="_blank" rel="noopener noreferrer" title="查看完整过程记录；如需诊断令牌，请在系统设置中签发 ai.diagnostics.read"><Activity :size="14" />查看过程记录</a>
      <span v-if="turn.feedback && !commentOpen" class="feedback-saved" role="status">反馈已保存</span>
    </div>
    <form v-if="commentOpen" class="feedback-form" @submit.prevent="rate(-1, true)">
      <label>补充意见（可选）<textarea v-model="comment" rows="2" placeholder="哪里不准确，或缺少了什么？" aria-label="反馈意见"></textarea></label>
      <div><button class="btn sm" type="button" :disabled="savingFeedback" @click="commentOpen = false">跳过说明</button><button class="btn primary sm" type="submit" :disabled="savingFeedback">{{ savingFeedback ? '保存中…' : '保存意见' }}</button></div>
    </form>
    <p v-if="copyError" class="copy-error" role="status">{{ copyError }}</p>
  </article>
</template>

<style scoped>
.pi-turn { min-width: 0; }
.question-row { display: flex; justify-content: flex-end; }
.question-bubble { max-width: 90%; background: var(--accent-subtle); color: var(--fg); padding: 12px 16px; border-radius: 12px 12px 2px 12px; white-space: pre-wrap; overflow-wrap: anywhere; font-size: var(--fs-lg); line-height: 1.6; }
.question-tags { display: flex; flex-wrap: wrap; justify-content: flex-end; gap: 5px; margin: 7px 0 18px; color: var(--fg-muted); font-size: var(--fs-xs); }
.question-tags span { border: 1px solid var(--border); border-radius: 4px; padding: 1px 6px; overflow-wrap: anywhere; max-width: 100%; }
.supplement { margin: 0 0 10px auto; padding: 8px 12px; max-width: 90%; border-left: 2px solid var(--accent); background: var(--accent-subtle); white-space: pre-wrap; overflow-wrap: anywhere; color: var(--fg-muted); font-size: var(--fs-sm); }
.turn-notice { margin: 12px 0; padding: 8px 12px; background: var(--info-bg); color: var(--info); border-radius: 6px; font-size: var(--fs-sm); overflow-wrap: anywhere; }
.pi-answer { margin-top: 18px; min-width: 0; overflow-wrap: anywhere; line-height: 1.8; }
.plain-answer { white-space: pre-wrap; font: inherit; }
.turn-status { margin-top: 12px; display: flex; align-items: center; flex-wrap: wrap; gap: 8px; color: var(--fg-muted); font-size: var(--fs-sm); overflow-wrap: anywhere; }
.failure { color: var(--danger); }
.answer-actions { display: flex; flex-wrap: wrap; gap: 4px; align-items: center; margin-top: 12px; }
.answer-actions button[aria-pressed="true"] { color: var(--accent-text); background: var(--accent-subtle); }
.feedback-saved { font-size: var(--fs-xs); color: var(--fg-subtle); margin-left: 4px; }
.feedback-form { margin-top: 10px; padding: 12px; border-radius: 8px; background: var(--surface-2); font-size: var(--fs-sm); color: var(--fg-muted); }
.feedback-form textarea { display: block; width: 100%; box-sizing: border-box; margin: 6px 0 10px; resize: vertical; background: var(--surface); color: var(--fg); border: 1px solid var(--border-strong); border-radius: 6px; padding: 8px; font: inherit; }
.feedback-form > div { display: flex; justify-content: flex-end; gap: 8px; }
.copy-error { color: var(--danger); font-size: var(--fs-sm); }
.markdown-body :deep(> :first-child) { margin-top: 0; }
.markdown-body :deep(pre) { max-width: 100%; overflow-x: auto; padding: 12px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface-2); font: var(--fs-sm)/1.7 var(--font-mono); }
.markdown-body :deep(code) { font-family: var(--font-mono); font-size: .9em; background: var(--surface-2); border-radius: 3px; padding: 2px 4px; overflow-wrap: anywhere; }
.markdown-body :deep(pre code) { padding: 0; background: none; overflow-wrap: normal; }
.markdown-body :deep(table) { display: block; max-width: 100%; overflow: auto; border-collapse: collapse; font-size: var(--fs-sm); }
.markdown-body :deep(th), .markdown-body :deep(td) { border: 1px solid var(--border); padding: 7px 10px; min-width: 70px; }
.markdown-body :deep(blockquote) { margin-left: 0; border-left: 3px solid var(--border-strong); padding-left: 12px; color: var(--fg-muted); }
.markdown-body :deep(img) { max-width: 100%; }
.markdown-body :deep(a) { color: var(--accent-text); }
.markdown-body :deep(.pi-citation) { display: inline; padding: 1px 5px; border: 1px solid var(--border-strong); border-radius: 4px; background: var(--surface-2); color: var(--accent-text); text-decoration: none; font: var(--fs-xs)/1.8 var(--font-mono); overflow-wrap: anywhere; box-decoration-break: clone; }
.markdown-body :deep(.pi-citation.warn) { color: var(--warn); background: var(--warn-bg); border-color: var(--warn); }
</style>
