<script setup lang="ts">
import { computed, ref } from 'vue'
import { CircleAlert, ExternalLink, FileText } from 'lucide-vue-next'
import type { PiTurn } from '../../ai-pi-types'
import { citationURL, durationMs, groupPiCitations } from './pi-chat'

const props = defineProps<{ turn?: PiTurn; turns: PiTurn[] }>()
defineEmits<{ select: [runID: number] }>()
const tab = ref<'citations' | 'usage'>('citations')
const groups = computed(() => groupPiCitations(props.turn?.citations || []))
const duration = computed(() => props.turn ? durationMs(props.turn) : null)
const statusLabels = { verified: '已核实', path_only: '行范围未核实', unverified: '未核实' }
</script>

<template>
  <aside class="pi-evidence" aria-label="引用与本次用量">
    <div class="evidence-tabs" role="tablist" aria-label="回答详情">
      <button id="pi-citations-tab" role="tab" type="button" :aria-selected="tab === 'citations'" aria-controls="pi-citations-panel" @click="tab = 'citations'">引用 <span>{{ groups.length }}</span></button>
      <button id="pi-usage-tab" role="tab" type="button" :aria-selected="tab === 'usage'" aria-controls="pi-usage-panel" @click="tab = 'usage'">本次用量</button>
    </div>
    <select v-if="turns.length > 1" class="turn-select" aria-label="查看哪轮回答的引用和用量" :value="turn?.run_id" @change="$emit('select', Number(($event.target as HTMLSelectElement).value))"><option v-for="(item, index) in turns" :key="item.run_id" :value="item.run_id">{{ index + 1 }}. {{ item.question.slice(0, 40) }}</option></select>
    <section v-if="tab === 'citations'" id="pi-citations-panel" class="evidence-content" role="tabpanel" aria-labelledby="pi-citations-tab">
      <p class="panel-help">本轮引用按文件合并；点击行号在新标签页核对原文。</p>
      <article v-for="group in groups" :key="group.key" class="citation-file" :class="{ warn: group.status !== 'verified' }">
        <h4><FileText :size="14" /><span :title="group.path">{{ group.path.split('/').pop() }}</span></h4>
        <p class="citation-repo">{{ group.repo }}@{{ group.ref }}</p>
        <p class="citation-path">{{ group.path }}</p>
        <span class="citation-status"><CircleAlert v-if="group.status !== 'verified'" :size="12" />{{ statusLabels[group.status] }}</span>
        <div class="citation-ranges"><template v-for="item in group.items" :key="item.key"><a v-if="citationURL(item.url)" :href="citationURL(item.url)" target="_blank" rel="noopener noreferrer" :class="{ 'range-warn': item.status !== 'verified' }" :title="`${item.key}${item.status !== 'verified' ? '\n这段内容没有在本次检索中读到，请自行核对' : ''}`">L{{ item.line_start }}{{ item.line_end !== item.line_start ? `-${item.line_end}` : '' }}<ExternalLink :size="11" /></a><span v-else class="range-warn">L{{ item.line_start }}-{{ item.line_end }} · 暂无链接</span></template></div>
        <p v-if="group.status !== 'verified'" class="citation-warning">这段内容没有在本次检索中读到，请自行核对</p>
      </article>
      <p v-if="!groups.length" class="panel-empty">{{ turn && ['queued', 'running'].includes(turn.status) ? '回答完成后显示核实引用' : '本轮暂无引用' }}</p>
    </section>
    <section v-else id="pi-usage-panel" class="evidence-content" role="tabpanel" aria-labelledby="pi-usage-tab">
      <dl v-if="turn" class="usage-list">
        <template v-if="turn.model"><dt>模型</dt><dd class="mono">{{ turn.model }}</dd></template>
        <template v-if="turn.provider_key"><dt>供应商</dt><dd>{{ turn.provider_key }}</dd></template>
        <template v-if="duration != null"><dt>耗时</dt><dd>{{ (duration / 1000).toFixed(1) }} 秒</dd></template>
        <dt>工具调用</dt><dd>{{ turn.tool_calls ?? turn.steps.length }} 次</dd>
        <dt>输入 token</dt><dd>{{ turn.usage.input_tokens.toLocaleString() }}</dd>
        <dt>输出 token</dt><dd>{{ turn.usage.output_tokens.toLocaleString() }}</dd>
        <template v-if="turn.usage.cache_read_tokens"><dt>缓存读取 token</dt><dd>{{ turn.usage.cache_read_tokens.toLocaleString() }}</dd></template>
        <template v-if="turn.usage.cost != null"><dt>费用</dt><dd>${{ turn.usage.cost.toFixed(6) }}</dd></template>
      </dl>
      <p v-else class="panel-empty">提问后显示本次用量</p>
    </section>
  </aside>
</template>

<style scoped>
.pi-evidence { border-left: 1px solid var(--border); background: var(--surface); display: flex; flex-direction: column; min-width: 0; min-height: 0; overflow: hidden; }
.evidence-tabs { display: flex; border-bottom: 1px solid var(--border); padding: 0 12px; flex-shrink: 0; }
.evidence-tabs button { flex: 1; border: 0; border-bottom: 2px solid transparent; background: none; color: var(--fg-muted); padding: 14px 8px; cursor: pointer; font: var(--fs-sm)/1.5 var(--font-ui); white-space: nowrap; }
.evidence-tabs button[aria-selected="true"] { border-bottom-color: var(--accent); color: var(--accent-text); }
.evidence-tabs span { background: var(--surface-2); border-radius: 4px; padding: 1px 5px; margin-left: 3px; font-size: var(--fs-xs); }
.turn-select { margin: 12px 14px 0; min-width: 0; max-width: calc(100% - 28px); background: var(--surface-2); color: var(--fg); border: 1px solid var(--border); border-radius: 6px; padding: 6px; font: var(--fs-sm)/1.5 var(--font-ui); }
.evidence-content { overflow-y: auto; min-height: 0; padding: 14px; }
.panel-help, .panel-empty { color: var(--fg-subtle); font-size: var(--fs-sm); line-height: 1.7; margin: 0 0 14px; }
.panel-empty { margin-top: 20px; text-align: center; }
.citation-file { border: 1px solid var(--border); border-radius: 8px; padding: 12px; margin-bottom: 10px; min-width: 0; }
.citation-file h4 { display: flex; gap: 6px; margin: 0; color: var(--fg); font-size: var(--fs-sm); }
.citation-file h4 svg { flex-shrink: 0; }
.citation-file h4 span { overflow-wrap: anywhere; min-width: 0; }
.citation-repo, .citation-path { margin: 5px 0; color: var(--fg-muted); font: var(--fs-xs)/1.6 var(--font-mono); overflow-wrap: anywhere; }
.citation-path { color: var(--fg-subtle); }
.citation-status { display: inline-flex; align-items: center; gap: 4px; margin: 3px 0; color: var(--ok); font-size: var(--fs-xs); }
.warn .citation-status, .citation-warning, .citation-ranges .range-warn { color: var(--warn); }
.citation-warning { margin: 8px 0 0; font-size: var(--fs-xs); line-height: 1.6; }
.citation-ranges { display: flex; gap: 6px; flex-wrap: wrap; margin-top: 6px; font: var(--fs-xs)/1.5 var(--font-mono); }
.citation-ranges a { display: inline-flex; align-items: center; gap: 4px; color: var(--accent-text); background: var(--surface-2); padding: 2px 5px; border-radius: 4px; text-decoration: none; }
.usage-list { display: grid; grid-template-columns: auto minmax(0, 1fr); gap: 14px 10px; font-size: var(--fs-sm); margin: 4px 0; }
.usage-list dt { color: var(--fg-muted); }
.usage-list dd { margin: 0; color: var(--fg); text-align: right; overflow-wrap: anywhere; }
.mono { font-family: var(--font-mono); }
@media (max-width: 860px) { .pi-evidence { border-left: 0; border-top: 1px solid var(--border); overflow: visible; } .evidence-content { overflow: visible; } }
</style>
