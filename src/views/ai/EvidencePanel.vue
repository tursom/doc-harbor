<script setup lang="ts">
import { nextTick, ref } from 'vue'
import { Check, Circle, LoaderCircle, Minus, X } from 'lucide-vue-next'
import BranchTag from '../../components/ui/BranchTag.vue'
import EmptyState from '../../components/ui/EmptyState.vue'
import StatusPill from '../../components/ui/StatusPill.vue'
import { shortSha } from '../../lib/format'
import type { AIMessageCitation, AIServiceCandidate, Repository } from '../../types'
import { coverageStatus, type EvidenceStep, type EvidenceTab, type NumberedCitation } from './chat'

const props = defineProps<{ citations: NumberedCitation[]; candidates: AIServiceCandidate[]; steps: EvidenceStep[]; streaming: boolean; repos: Repository[] }>()
defineEmits<{ open: [citation: AIMessageCitation] }>()
const tab = ref<EvidenceTab>('citations')
const activeKey = ref('')
const panel = ref<HTMLElement | null>(null)
const tabs: { key: EvidenceTab; label: string }[] = [
  { key: 'citations', label: '引用' }, { key: 'candidates', label: '候选服务' }, { key: 'trace', label: '检索过程' }
]
async function focusCitation(key: string) {
  if (!props.citations.some((item) => item.key === key)) return
  tab.value = 'citations'
  activeKey.value = key
  await nextTick()
  const item = panel.value?.querySelector<HTMLElement>(`[data-evidence-key="${key}"]`)
  item?.scrollIntoView({ block: 'nearest', behavior: 'smooth' })
  item?.focus({ preventScroll: true })
}
function tabKeydown(event: KeyboardEvent, index: number) {
  let next = index
  if (event.key === 'ArrowRight') next = (index + 1) % tabs.length
  else if (event.key === 'ArrowLeft') next = (index + tabs.length - 1) % tabs.length
  else if (event.key === 'Home') next = 0
  else if (event.key === 'End') next = tabs.length - 1
  else return
  event.preventDefault()
  tab.value = tabs[next]!.key
  panel.value?.querySelector<HTMLButtonElement>(`#evidence-tab-${tab.value}`)?.focus()
}
function state(status: string) {
  const mapped = coverageStatus(status)
  if (['success', 'completed'].includes(mapped)) return 'ok'
  if (mapped === 'failed') return 'failed'
  if (['running', 'started'].includes(mapped)) return 'running'
  return 'neutral'
}
defineExpose({ focusCitation })
</script>

<template>
  <aside ref="panel" class="evidence" aria-label="回答证据">
    <div class="evidence-tabs" role="tablist" aria-label="证据类型">
      <button v-for="(item, index) in tabs" :id="`evidence-tab-${item.key}`" :key="item.key" type="button" role="tab" :aria-selected="tab === item.key" :aria-controls="`evidence-panel-${item.key}`" :tabindex="tab === item.key ? 0 : -1" :class="{ active: tab === item.key }" @click="tab = item.key" @keydown="tabKeydown($event, index)">
        {{ item.label }}<span v-if="item.key === 'citations'" class="count">{{ citations.length }}</span><span v-else-if="item.key === 'candidates'" class="count">{{ candidates.length }}</span><LoaderCircle v-else-if="streaming" class="spin" :size="12" aria-label="生成中" />
      </button>
    </div>
    <div v-for="item in tabs" v-show="tab === item.key" :id="`evidence-panel-${item.key}`" :key="item.key" class="evidence-list" role="tabpanel" :aria-labelledby="`evidence-tab-${item.key}`" tabindex="0">
      <template v-if="item.key === 'citations'">
        <button v-for="entry in citations" :key="entry.key" class="evidence-card" :class="{ selected: activeKey === entry.key }" :data-evidence-key="entry.key" type="button" :title="`回答 #${entry.citation.message_id} · 引用 C${entry.number}`" @click="$emit('open', entry.citation)">
          <span class="citation-top"><span class="citation-number">C{{ entry.number }}</span><span>{{ entry.citation.repo_name || repos.find(repo => repo.id === entry.citation.repo_id)?.name || `repo#${entry.citation.repo_id}` }}</span><span>·</span><span>{{ entry.citation.source_scope === 'smart_latest' ? '智能最新' : entry.citation.source_scope === 'branch_candidate' ? '功能分支候选' : entry.citation.source_scope }}</span></span>
          <span class="citation-path">{{ entry.citation.file_path }}</span>
          <span class="citation-meta"><BranchTag :label="entry.citation.branch" neutral /><span class="sha" :title="entry.citation.commit_sha">{{ shortSha(entry.citation.commit_sha) }}</span><span>第 {{ entry.citation.line_start }}-{{ entry.citation.line_end }} 行</span></span>
        </button>
        <EmptyState v-if="!citations.length" message="回答引用会保存在会话消息中" />
      </template>
      <template v-else-if="item.key === 'candidates'">
        <div v-for="candidate in candidates" :key="`${candidate.message_id}:${candidate.repo_id}:${candidate.service_name}`" class="evidence-card">
          <div class="candidate-head"><strong>{{ candidate.service_name }}</strong><StatusPill :status="candidate.confidence" /></div>
          <p>{{ candidate.reason }}</p>
        </div>
        <EmptyState v-if="!candidates.length" message="发送问题后展示服务候选" />
      </template>
      <template v-else>
        <ol v-if="steps.length" class="steps">
          <li v-for="step in steps" :key="step.id" class="step">
            <span class="step-mark" :class="state(step.status)" :title="step.status"><Check v-if="state(step.status) === 'ok'" :size="12" /><X v-else-if="state(step.status) === 'failed'" :size="12" /><LoaderCircle v-else-if="state(step.status) === 'running'" class="spin" :size="12" /><Minus v-else-if="step.status === 'stopped'" :size="12" /><Circle v-else :size="10" /></span>
            <div><strong>{{ step.title }}</strong><p>{{ step.detail }}</p><small v-if="step.meta">{{ step.meta }}</small></div>
          </li>
        </ol>
        <EmptyState v-else message="发送问题后展示检索、路由尝试和证据召回状态" />
      </template>
    </div>
  </aside>
</template>

<style scoped>
.evidence { display: flex; flex-direction: column; min-height: 0; min-width: 0; border-left: 1px solid var(--border); }
.evidence-tabs { display: flex; gap: 2px; padding: 0 8px; border-bottom: 1px solid var(--border); flex-shrink: 0; }
.evidence-tabs button { display: inline-flex; align-items: center; justify-content: center; gap: 4px; flex: 1; white-space: nowrap; padding: 12px 4px 10px; border: 0; border-bottom: 2px solid var(--surface); background: none; color: var(--fg-muted); font-size: var(--fs-sm); cursor: pointer; }
.evidence-tabs button.active { border-bottom-color: var(--accent); color: var(--fg); font-weight: 600; }
.count { padding: 0 4px; border-radius: 999px; background: var(--surface-2); font-size: var(--fs-xs); color: var(--fg-subtle); font-variant-numeric: tabular-nums; }
.evidence-list { display: flex; flex-direction: column; gap: 8px; overflow-y: auto; padding: 12px; }
.evidence-card { display: flex; flex-direction: column; gap: 4px; width: 100%; min-width: 0; padding: 10px 12px; border: 1px solid var(--border); border-radius: 6px; background: var(--surface); color: var(--fg); text-align: left; box-sizing: border-box; }
button.evidence-card { cursor: pointer; }
button.evidence-card:hover { border-color: var(--border-strong); background: var(--surface-2); }
.evidence-card.selected { border-color: var(--accent); background: var(--accent-subtle); }
.citation-top { display: flex; align-items: center; flex-wrap: wrap; gap: 4px; font-size: var(--fs-xs); color: var(--fg-subtle); overflow-wrap: anywhere; }
.citation-number { padding: 1px 4px; border-radius: 4px; background: var(--accent-subtle); color: var(--accent-text); font: 600 10.5px/1.3 var(--font-mono); }
.citation-path { font: var(--fs-sm)/1.45 var(--font-mono); overflow-wrap: anywhere; }
.citation-meta { display: flex; align-items: center; gap: 6px; flex-wrap: wrap; min-width: 0; font-size: var(--fs-xs); color: var(--fg-subtle); font-variant-numeric: tabular-nums; }
.sha { font-family: var(--font-mono); }
.candidate-head { display: flex; align-items: center; justify-content: space-between; flex-wrap: wrap; gap: 6px; overflow-wrap: anywhere; }
.evidence-card p { margin: 0; color: var(--fg-muted); font-size: var(--fs-sm); overflow-wrap: anywhere; }
.steps { list-style: none; padding: 0; margin: 0; display: flex; flex-direction: column; gap: 16px; }
.step { display: grid; grid-template-columns: 18px minmax(0, 1fr); gap: 10px; }
.step-mark { width: 18px; height: 18px; border-radius: 50%; display: grid; place-items: center; background: var(--surface-2); color: var(--fg-muted); }
.step-mark.ok { background: var(--ok-bg); color: var(--ok); }
.step-mark.failed { background: var(--danger-bg); color: var(--danger); }
.step-mark.running { background: var(--info-bg); color: var(--info); }
.step strong { font-size: var(--fs-sm); }
.step p, .step small { margin: 2px 0 0; font-size: var(--fs-sm); color: var(--fg-muted); overflow-wrap: anywhere; }
.step small { color: var(--fg-subtle); font-size: var(--fs-xs); }
button:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
.spin { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .spin { animation: none; } }
@media (max-width: 860px) { .evidence { border-left: 0; border-top: 1px solid var(--border); } .evidence-list { max-height: 420px; } }
</style>
