<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { BookOpen, Check, ChevronRight, CircleAlert, FolderSearch, GitBranch, LoaderCircle, Search } from 'lucide-vue-next'
import type { PiTurn } from '../../ai-pi-types'
import { isActive, processSummary, stepLabel, stepParameters, toolName } from './pi-chat'

const props = defineProps<{ turn: PiTurn }>()
const active = computed(() => isActive(props.turn))
const expanded = ref(active.value)
// 默认运行展开、完成折叠；完成后仍可手动展开复核。
watch(active, value => { expanded.value = value })
function icon(tool: string) {
  const name = toolName(tool)
  if (name === 'read') return BookOpen
  if (name.startsWith('search')) return Search
  if (name === 'git_log' || name === 'diff_files' || name === 'list_branches') return GitBranch
  return FolderSearch
}
</script>

<template>
  <details class="pi-process" :open="expanded" @toggle="expanded = ($event.target as HTMLDetailsElement).open">
    <summary><ChevronRight class="chevron" :size="14" /><LoaderCircle v-if="active" class="spinning" :size="14" /><span>{{ processSummary(turn) }}</span></summary>
    <p v-if="!turn.steps.length" class="waiting">{{ turn.status === 'queued' ? '等待可用的问答进程…' : '正在准备检索…' }}</p>
    <ol v-else class="steps">
      <li v-for="step in turn.steps" :key="step.call_id" :class="{ 'step-error': step.is_error }">
        <details class="tool-step">
          <summary>
            <LoaderCircle v-if="step.running && active" class="spinning" :size="15" />
            <CircleAlert v-else-if="step.is_error" :size="15" />
            <component :is="icon(step.tool)" v-else :size="15" />
            <span class="tool-label"><strong>{{ stepLabel(step) }}</strong><span>{{ stepParameters(step) }}</span></span>
            <span class="tool-summary">{{ step.summary || (step.running && active ? '进行中…' : '已结束') }}</span>
            <Check v-if="!step.running && !step.is_error" :size="12" />
          </summary>
          <div class="tool-detail"><h5>调用参数</h5><pre>{{ JSON.stringify(step.args, null, 2) }}</pre><h5>结果预览</h5><pre>{{ step.preview || step.summary || '暂无结果预览' }}</pre></div>
        </details>
      </li>
    </ol>
  </details>
</template>

<style scoped>
.pi-process { border: 1px solid var(--border); border-radius: 8px; background: var(--surface-2); color: var(--fg-muted); font-size: var(--fs-sm); min-width: 0; }
summary { cursor: pointer; list-style: none; display: flex; align-items: center; gap: 8px; padding: 10px 12px; min-width: 0; }
summary::-webkit-details-marker { display: none; }
summary:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; border-radius: 6px; }
summary svg { flex-shrink: 0; }
.chevron { transition: transform .15s; }
details[open] > summary .chevron { transform: rotate(90deg); }
.steps { list-style: none; margin: 0; padding: 0 10px 8px; }
.steps li { border-top: 1px solid var(--border); }
.tool-step > summary { flex-wrap: wrap; padding: 9px 2px; }
.tool-label { flex: 1; min-width: 0; display: flex; flex-wrap: wrap; gap: 4px 8px; overflow-wrap: anywhere; }
.tool-label strong { color: var(--fg); font-weight: 500; }
.tool-summary { max-width: 44%; overflow-wrap: anywhere; font-size: var(--fs-xs); }
.step-error, .step-error .tool-label strong { color: var(--danger); }
.tool-detail { min-width: 0; padding: 0 0 10px; }
h5 { margin: 6px 0; font-size: var(--fs-xs); font-weight: 500; }
pre { margin: 0; padding: 10px; max-height: 320px; max-width: 100%; overflow: auto; background: var(--surface); border: 1px solid var(--border); border-radius: 4px; color: var(--fg-muted); font: var(--fs-xs)/1.6 var(--font-mono); white-space: pre-wrap; overflow-wrap: anywhere; }
.waiting { margin: 0; padding: 0 14px 12px; }
.spinning { animation: spin 1s linear infinite; }
@keyframes spin { to { transform: rotate(360deg); } }
@media (prefers-reduced-motion: reduce) { .spinning { animation: none; } }
@media (max-width: 600px) { .tool-summary { max-width: none; width: 100%; margin-left: 23px; } }
</style>
