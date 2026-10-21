<script setup lang="ts">
import { nextTick, onMounted, ref, watch } from 'vue'
import { FileText, Send, Square } from 'lucide-vue-next'
import { shouldSendOnEnter } from './chat'
import type { PiRole } from '../../ai-pi-types'

const props = defineProps<{
  question: string
  scope: 'global' | 'current_repo' | 'current_file'
  includeBranches: boolean
  repoName?: string
  filePath?: string
  canAskFile: boolean
  busy: boolean
  streaming: boolean
  pi?: boolean
  role?: PiRole
}>()
const emit = defineEmits<{
  'update:question': [value: string]
  'update:scope': [value: 'global' | 'current_repo' | 'current_file']
  'update:includeBranches': [value: boolean]
  send: []
  stop: []
  'update:role': [value: PiRole]
}>()
const textarea = ref<HTMLTextAreaElement | null>(null)
let composing = false

/** 先清高度再量 scrollHeight，使删除文字、发送清空时也能回缩到两行。 */
async function resize() {
  await nextTick()
  if (!textarea.value) return
  const element = textarea.value
  const lineHeight = Number.parseFloat(getComputedStyle(element).lineHeight) || 24
  element.style.height = 'auto'
  element.style.height = `${Math.max(lineHeight * 2, Math.min(element.scrollHeight, lineHeight * 8))}px`
  element.style.overflowY = element.scrollHeight > lineHeight * 8 ? 'auto' : 'hidden'
}
watch(() => props.question, resize)
onMounted(resize)
function onKeydown(event: KeyboardEvent) {
  if (!shouldSendOnEnter(event, composing)) return
  event.preventDefault()
  if ((!props.busy || (props.pi && props.streaming)) && props.question.trim()) emit('send')
}
</script>

<template>
  <div class="composer-wrap">
    <div v-if="pi" class="role-row" role="group" aria-label="提问角色">
      <span>角色</span>
      <button v-for="item in (['开发', '产品', '测试'] as const)" :key="item" type="button" :aria-pressed="role === item" :class="{ selected: role === item }" @click="$emit('update:role', role === item ? '未指定' : item)">{{ item }}</button>
      <small v-if="!role || role === '未指定'">未指定</small>
    </div>
    <div v-if="scope === 'current_file' && filePath" class="scope-file" :title="filePath"><FileText :size="13" /><span>{{ filePath }}</span></div>
    <form class="composer" @submit.prevent="(!busy || (pi && streaming)) && question.trim() && $emit('send')">
      <textarea ref="textarea" :value="question" rows="2" aria-label="提问内容" :placeholder="pi && streaming ? '补充说明，会在当前步骤结束后交给助手' : '从全部仓库提问，或继续追问…'" @input="$emit('update:question', ($event.target as HTMLTextAreaElement).value)" @keydown="onKeydown" @compositionstart="composing = true" @compositionend="composing = false"></textarea>
      <div class="composer-row">
        <select :value="scope" aria-label="检索范围" :disabled="busy" @change="$emit('update:scope', ($event.target as HTMLSelectElement).value as typeof scope)">
          <option value="global">全部仓库</option>
          <option value="current_repo" :disabled="!repoName">当前仓库{{ repoName ? `：${repoName}` : '' }}</option>
          <option value="current_file" :disabled="!canAskFile">当前文件</option>
        </select>
        <label v-if="!pi" class="branch-check"><input type="checkbox" :checked="includeBranches" :disabled="busy" @change="$emit('update:includeBranches', ($event.target as HTMLInputElement).checked)" />包含功能分支</label>
        <span class="shortcut"><kbd>Enter</kbd> {{ pi && streaming ? '补充说明' : '发送' }} · <kbd>Shift</kbd>+<kbd>Enter</kbd> 换行</span>
        <button v-if="streaming" class="btn icon sm" type="button" aria-label="停止生成" title="停止生成" @click="$emit('stop')"><Square :size="14" /></button>
        <button v-else class="btn primary icon sm" type="submit" :disabled="busy || !question.trim()" aria-label="发送" title="发送"><Send :size="15" /></button>
      </div>
    </form>
  </div>
</template>

<style scoped>
.composer-wrap { padding: 12px 32px calc(16px + env(safe-area-inset-bottom, 0px)); background: var(--surface); }
.role-row { max-width: 720px; margin: 0 auto 10px; display: flex; align-items: center; gap: 4px; font-size: var(--fs-sm); color: var(--fg-muted); }
.role-row > span { margin-right: 6px; }
.role-row button { border: 1px solid var(--border); padding: 4px 12px; background: var(--surface-2); color: var(--fg-muted); cursor: pointer; border-radius: 6px; font: inherit; }
.role-row button.selected { background: var(--accent-subtle); color: var(--accent-text); border-color: var(--accent); }
.role-row small { margin-left: 6px; }
.scope-file { display: flex; align-items: center; gap: 6px; max-width: 720px; margin: 0 auto 8px; color: var(--accent-text); font: var(--fs-xs)/1.5 var(--font-mono); }
.scope-file span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.scope-file svg { flex-shrink: 0; }
.composer { max-width: 720px; margin: 0 auto; padding: 10px 10px 8px 14px; display: flex; flex-direction: column; gap: 6px; border: 1px solid var(--border-strong); border-radius: 12px; background: var(--surface); }
.composer:focus-within { border-color: var(--accent); box-shadow: 0 0 0 3px var(--accent-subtle); }
.composer textarea { width: 100%; min-height: 48px; max-height: 192px; border: 0; outline: none; resize: none; background: none; color: var(--fg); font: var(--fs-lg)/24px var(--font-ui); padding: 0; box-sizing: border-box; }
.composer textarea::placeholder { color: var(--fg-subtle); }
.composer-row { display: flex; align-items: center; gap: 8px; flex-wrap: wrap; min-width: 0; }
.composer-row select { flex: 0 1 auto; width: auto; max-width: 100%; min-width: 0; min-height: 26px; height: 26px; border: 1px solid var(--border); border-radius: 999px; background: var(--surface-2); color: var(--fg); padding: 0 8px; font-size: var(--fs-sm); }
.branch-check { display: flex; align-items: center; gap: 4px; font-size: var(--fs-sm); color: var(--fg-muted); white-space: nowrap; }
.branch-check input { accent-color: var(--accent); margin: 0; }
.shortcut { flex: 1; text-align: right; color: var(--fg-subtle); font-size: var(--fs-xs); white-space: nowrap; }
kbd { font: inherit; font-family: var(--font-mono); border: 1px solid var(--border); border-radius: 4px; padding: 1px 3px; }
.composer-row > button { margin-left: auto; flex-shrink: 0; }
@media (max-width: 1280px) { .shortcut { display: none; } }
@media (max-width: 860px) { .composer-wrap { position: sticky; bottom: 0; z-index: 2; padding-left: max(16px, env(safe-area-inset-left, 0px)); padding-right: max(16px, env(safe-area-inset-right, 0px)); } }
</style>
