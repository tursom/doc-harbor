<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { KeyRound, RefreshCw, Search } from 'lucide-vue-next'
import StatusPill from '../components/ui/StatusPill.vue'
import EmptyState from '../components/ui/EmptyState.vue'
import Notice from '../components/ui/Notice.vue'
import { useDiagnostics, diagnosticStatuses, diagnosticStatusLabel, diagnosticStatusVariant, runDuration, runTokens, feedbackLabel } from '../composables/useDiagnostics'
import { formatTime } from '../lib/format'
import type { PiCitation } from '../ai-pi-types'

const { token, rememberToken, storageError, status, query, negativeOnly, runs, detail, loading, detailLoading, error, detailError, selectedID, nextCursor, emptyMessage, loadRuns, openRun } = useDiagnostics()
const turn = computed(() => detail.value?.transcript)
const usage = computed(() => turn.value?.usage || detail.value?.run)
const feedback = computed(() => detail.value?.run.feedback || turn.value?.feedback)
const citations = computed<PiCitation[]>(() => {
  if (turn.value) return turn.value.citations
  // 正文暂不可用时仍展示运行记录中已经持久化的核实结果。
  try { const value: unknown = JSON.parse(detail.value?.run.citations_json || '[]'); return Array.isArray(value) ? value : [] }
  catch { return [] }
})
const answerHTML = ref('')
let renderRequest = 0
// Markdown 只在有回答时加载；清空旧 HTML 并校验序号，防止切换令牌后闪回旧回答。
watch(() => turn.value?.answer, async (answer) => {
  const request = ++renderRequest
  answerHTML.value = ''
  if (!answer) return
  try {
    const [{ marked }, { default: DOMPurify }] = await Promise.all([import('marked'), import('dompurify')])
    const html = DOMPurify.sanitize(await marked.parse(answer, { async: false }), { USE_PROFILES: { html: true } })
    if (request === renderRequest) answerHTML.value = html
  } catch { /* 加载失败时由模板显示完整原文，诊断内容不会因此丢失。 */ }
}, { immediate: true, flush: 'sync' })

const citationLabels = { verified: '已核实', path_only: '仅文件匹配', unverified: '未核实' }
const toolLabels: Record<string, string> = { list_repos: '查看仓库', list_branches: '查看分支', list_docs: '浏览文档目录', search_docs: '搜索文档', search_code: '搜索代码', read: '阅读文件', list_dir: '浏览目录', git_log: '查看提交', diff_files: '比较分支' }
function toolLabel(tool: string) {
  // 实际会话同时存在单下划线和双下划线的 MCP 名称；完整工具名保留在悬停提示中。
  return toolLabels[tool.replace(/^mcp(?:__|_)docharbor(?:__|_)/, '')] || tool
}
function citationURL(value: string) {
  // 仅允许站内及 HTTP 文档链接；Markdown 的 URL 则统一经过 DOMPurify。
  try { const url = new URL(value, window.location.origin); return ['http:', 'https:'].includes(url.protocol) ? url.href : undefined }
  catch { return undefined }
}
function json(value: unknown) { return JSON.stringify(value, null, 2) }
</script>

<template>
  <section class="diagnostics-page">
    <header class="page-heading"><h2>AI 诊断</h2><p>查看问答的完整工具调用、最终回答、引用核实和运行用量</p></header>
    <form class="toolbar" @submit.prevent="loadRuns()">
      <label class="search-field token-field"><KeyRound :size="15" /><input v-model.trim="token" type="password" autocomplete="off" placeholder="诊断令牌" aria-label="诊断令牌" /></label>
      <select v-model="status" aria-label="状态"><option value="">全部状态</option><option v-for="value in diagnosticStatuses" :key="value" :value="value">{{ diagnosticStatusLabel(value) }}</option></select>
      <label class="search-field query-field"><Search :size="15" /><input v-model="query" placeholder="按问题关键词筛选" aria-label="关键词" /></label>
      <label class="check"><input v-model="negativeOnly" type="checkbox" />只看 👎</label>
      <button class="btn" type="submit" :disabled="loading || !token.trim()"><RefreshCw :size="15" />{{ loading ? '读取中…' : '读取' }}</button>
      <div class="token-help"><label class="check"><input v-model="rememberToken" type="checkbox" />仅在当前标签页会话内记住令牌</label><span>默认不保存令牌。需要 <code>ai.diagnostics.read</code> 能力，可在<a href="/settings/#access-tokens" target="_blank" rel="noopener noreferrer">系统设置</a>中签发，再回到本页读取。</span></div>
    </form>
    <div v-if="storageError" class="error-notice"><Notice variant="warn">{{ storageError }}</Notice></div>
    <div v-if="!token.trim() && selectedID" class="error-notice"><Notice variant="info">已保留目标 Run #{{ selectedID }}。请先输入诊断令牌；如需签发，请打开<a href="/settings/#access-tokens" target="_blank" rel="noopener noreferrer">系统设置</a>，完成后回到本页读取。</Notice></div>
    <div v-if="error" class="error-notice" role="alert"><Notice variant="danger">{{ error }} <a href="/settings/#access-tokens" target="_blank" rel="noopener noreferrer">打开系统设置</a></Notice></div>
    <div class="diagnostics-layout">
      <aside class="run-list" aria-label="诊断运行列表" :aria-busy="loading">
        <button v-for="run in runs" :key="run.id" class="run-row" :class="{ active: selectedID === run.id }" type="button" :aria-pressed="selectedID === run.id" @click="openRun(run.id)">
          <span class="run-meta"><StatusPill :status="run.status" :label="diagnosticStatusLabel(run.status)" :variant="diagnosticStatusVariant(run.status)" /><code>#{{ run.id }}</code><time>{{ formatTime(run.started_at) }}</time></span>
          <span class="question" :title="run.question">{{ run.question || '未记录问题' }}</span>
          <span class="run-stats"><span>模型 {{ run.model || '—' }}</span><span>{{ runDuration(run) }} · {{ run.tool_calls }} 次工具调用</span><span>{{ runTokens(run).toLocaleString() }} token · {{ feedbackLabel(run.feedback?.rating) }}</span></span>
        </button>
        <EmptyState v-if="!runs.length" :message="loading ? '正在读取运行记录…' : emptyMessage" />
        <button v-if="nextCursor" class="btn more-runs" type="button" :disabled="loading" @click="loadRuns(true)">{{ loading ? '读取中…' : '读取更多' }}</button>
      </aside>
      <article class="diagnostics-detail" :aria-busy="detailLoading">
        <Notice v-if="detailError" variant="danger" role="alert">{{ detailError }} <a href="/settings/#access-tokens" target="_blank" rel="noopener noreferrer">打开系统设置</a><button class="btn retry" type="button" @click="selectedID && openRun(selectedID)">重试详情</button></Notice>
        <EmptyState v-else-if="detailLoading" message="正在读取诊断详情…" />
        <template v-else-if="detail">
          <header class="detail-heading"><div><h3>Run #{{ detail.run.id }}</h3><p class="question-text">{{ detail.run.question }}</p><small>会话 #{{ detail.session.id }} · {{ detail.session.title }} · {{ detail.run.role }}</small></div><StatusPill :status="detail.run.status" :label="diagnosticStatusLabel(detail.run.status)" :variant="diagnosticStatusVariant(detail.run.status)" /></header>
          <Notice v-if="detail.run.error_message" variant="danger">{{ detail.run.error_message }}</Notice>
          <dl class="usage-grid" aria-label="本次用量">
            <div><dt>模型</dt><dd>{{ detail.run.model || '—' }}<small>{{ detail.run.provider_key }}</small></dd></div>
            <div><dt>耗时 / 工具调用</dt><dd>{{ runDuration(detail.run) }} / {{ detail.run.tool_calls }} 次</dd></div>
            <div><dt>输入 / 输出 token</dt><dd>{{ usage?.input_tokens.toLocaleString() }} / {{ usage?.output_tokens.toLocaleString() }}</dd></div>
            <div><dt>缓存读取 token</dt><dd>{{ usage?.cache_read_tokens.toLocaleString() }}</dd></div>
            <div v-if="turn?.usage.cache_write_tokens !== undefined"><dt>缓存写入 token</dt><dd>{{ turn.usage.cache_write_tokens.toLocaleString() }}</dd></div>
            <div v-if="usage?.cost !== undefined"><dt>费用</dt><dd>{{ usage.cost.toFixed(6) }}</dd></div>
          </dl>
          <section class="diagnostic-block"><h4>用户反馈</h4><p>{{ feedbackLabel(feedback?.rating) }}</p><p v-if="feedback?.comment" class="plain-text">{{ feedback.comment }}</p></section>
          <Notice v-if="!turn" variant="warn">此运行暂无可回放的过程记录，以下仍可查看已保存的引用与 stderr。</Notice>
          <section class="diagnostic-block"><h4>工具调用 · {{ turn?.steps.length || 0 }}</h4>
            <details v-for="(step, index) in turn?.steps || []" :key="`${detail.run.id}:${step.call_id}:${index}`" class="step">
              <summary><span>{{ index + 1 }}.</span><strong :title="step.tool">{{ toolLabel(step.tool) }}</strong><span class="step-summary">{{ step.summary || '暂无摘要' }}</span><StatusPill :status="step.is_error ? 'failed' : 'succeeded'" :label="step.is_error ? '工具错误' : '已完成'" :variant="step.is_error ? 'danger' : 'ok'" /></summary>
              <h5>调用参数</h5><pre>{{ json(step.args) }}</pre><h5>完整结果</h5><pre class="tool-result">{{ step.preview || '工具未返回文本结果' }}</pre>
            </details>
            <EmptyState v-if="!turn?.steps.length" message="暂无工具调用记录" />
          </section>
          <section class="diagnostic-block"><h4>最终回答</h4><div v-if="answerHTML" class="answer-markdown" v-html="answerHTML"></div><pre v-else-if="turn?.answer" class="answer-raw">{{ turn.answer }}</pre><EmptyState v-else message="暂无最终回答" /></section>
          <section class="diagnostic-block"><h4>引用核实 · {{ citations.length }}</h4>
            <div v-if="citations.length" class="table-scroll"><table><thead><tr><th>引用</th><th>核实状态</th><th>行范围</th></tr></thead><tbody><tr v-for="(citation, index) in citations" :key="`${citation.key}:${index}`"><td><a v-if="citation.url && citationURL(citation.url)" :href="citationURL(citation.url)" target="_blank" rel="noopener noreferrer">{{ citation.key }}</a><code v-else>{{ citation.key }}</code></td><td><StatusPill :status="citation.status" :label="citationLabels[citation.status]" :variant="citation.status === 'verified' ? 'ok' : 'warn'" /><small v-if="citation.status !== 'verified'">这段内容没有在本次检索中读到，请自行核对。</small></td><td>L{{ citation.line_start }}–{{ citation.line_end }}</td></tr></tbody></table></div>
            <EmptyState v-else message="暂无引用核实记录" />
          </section>
          <section class="diagnostic-block"><h4>原始消息 · {{ turn?.messages.length || 0 }}</h4><details v-if="turn?.messages.length" :key="detail.run.id" class="step"><summary>展开完整原始消息（包含 context、补充说明与工具数据）</summary><pre>{{ json(turn.messages) }}</pre></details><EmptyState v-else message="暂无原始消息" /></section>
          <section class="diagnostic-block"><h4>stderr 尾部</h4><pre v-if="detail.run.stderr_tail" class="stderr">{{ detail.run.stderr_tail }}</pre><EmptyState v-else message="暂无 stderr 输出" /></section>
        </template>
        <EmptyState v-else-if="!detailError" :message="selectedID ? `等待读取 Run #${selectedID}` : runs.length ? '选择运行记录查看详情' : emptyMessage" />
      </article>
    </div>
  </section>
</template>

<style scoped>
.diagnostics-page { min-width: 0; color: var(--fg); overflow-wrap: anywhere; }
.page-heading { padding: 16px 24px; border-bottom: 1px solid var(--border); } h2 { margin: 0; font-size: var(--fs-xl); } .page-heading p { margin: 3px 0 0; color: var(--fg-muted); font-size: var(--fs-sm); }
.toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 10px 24px; border-bottom: 1px solid var(--border); }
.search-field { display: flex; gap: 8px; align-items: center; padding: 0 9px; border: 1px solid var(--border-strong); border-radius: 6px; color: var(--fg-subtle); min-width: 0; background: var(--surface); }
.search-field svg { flex-shrink: 0; } .token-field { width: 250px; } .query-field { width: 220px; } .search-field input { width: 100%; min-width: 0; border: 0; height: 30px; color: var(--fg); background: transparent; font: inherit; } .token-field input { font-family: var(--font-mono); }
select { width: auto; max-width: 100%; height: 32px; padding: 0 8px; border: 1px solid var(--border-strong); border-radius: 6px; background: var(--surface); color: var(--fg); font: inherit; }
.check { display: inline-flex; align-items: center; gap: 6px; font-size: var(--fs-sm); } .check input { flex-shrink: 0; } .toolbar > .check { flex: 0 0 auto; white-space: nowrap; } .token-help { display: flex; flex-wrap: wrap; gap: 8px 16px; width: 100%; color: var(--fg-muted); font-size: var(--fs-xs); } .token-help .check { font-size: inherit; }
a { color: var(--accent-text); } .error-notice { padding: 12px 24px; } .retry { margin: 8px; }
.diagnostics-layout { display: grid; grid-template-columns: 320px minmax(0, 1fr); min-height: 520px; } .run-list { min-width: 0; border-right: 1px solid var(--border); padding: 8px; }
.run-row { display: grid; gap: 6px; width: 100%; text-align: left; border: 0; border-radius: 6px; background: transparent; color: var(--fg); padding: 10px; cursor: pointer; font: inherit; } .run-row:hover { background: var(--surface-2); } .run-row.active { background: var(--surface-3); } .run-row > * { min-width: 0; }
.run-meta { display: flex; align-items: center; flex-wrap: wrap; gap: 7px; color: var(--fg-subtle); font-size: var(--fs-xs); } time { font-variant-numeric: tabular-nums; } .question { display: -webkit-box; -webkit-line-clamp: 2; -webkit-box-orient: vertical; overflow: hidden; } .run-stats { display: grid; gap: 3px; color: var(--fg-muted); font-size: var(--fs-xs); } .more-runs { margin: 12px; }
.diagnostics-detail { min-width: 0; padding: 24px; } .detail-heading { display: flex; align-items: flex-start; flex-wrap: wrap; gap: 16px; margin-bottom: 20px; } .detail-heading > div { flex: 1; min-width: 0; } h3 { margin: 0; font-size: var(--fs-lg); } .detail-heading p { margin: 6px 0; } .question-text, .plain-text { white-space: pre-wrap; } small { display: block; font-size: var(--fs-xs); color: var(--fg-muted); }
.usage-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 1px; background: var(--border); border: 1px solid var(--border); border-radius: 8px; overflow: hidden; margin: 20px 0; } .usage-grid > div { min-width: 0; background: var(--surface); padding: 12px; } dt { font-size: var(--fs-xs); color: var(--fg-muted); } dd { margin: 5px 0 0; font-size: var(--fs-sm); font-variant-numeric: tabular-nums; }
.diagnostic-block { margin-top: 24px; min-width: 0; } h4 { margin: 0 0 10px; color: var(--fg-muted); font-size: var(--fs-sm); } h5 { margin: 12px; font-size: var(--fs-xs); color: var(--fg-muted); }
.step { margin-top: 8px; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; } .step summary { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 10px 12px; cursor: pointer; font-size: var(--fs-sm); } .step-summary { color: var(--fg-muted); flex: 1 1 160px; min-width: 0; }
code, pre { font-family: var(--font-mono); } pre { box-sizing: border-box; max-width: 100%; max-height: 420px; overflow: auto; white-space: pre-wrap; overflow-wrap: anywhere; margin: 0; padding: 14px; background: var(--surface-2); border-radius: 6px; font-size: var(--fs-xs); line-height: 1.65; } .step pre { border-radius: 0; } .answer-raw { max-height: none; }
.table-scroll { max-width: 100%; overflow-x: auto; border: 1px solid var(--border); border-radius: 8px; } table { border-collapse: collapse; width: 100%; min-width: 460px; table-layout: fixed; text-align: left; font-size: var(--fs-sm); } th, td { padding: 8px 12px; border-bottom: 1px solid var(--border); vertical-align: top; } th { background: var(--surface-2); color: var(--fg-muted); font-weight: 600; } th:first-child { width: 44%; } th:last-child { width: 20%; } td small { margin-top: 6px; } tr:last-child td { border-bottom: 0; }
.answer-markdown { line-height: 1.7; font-size: var(--fs-sm); } .answer-markdown :deep(pre) { max-width: 100%; overflow: auto; padding: 12px; background: var(--surface-2); border-radius: 6px; white-space: pre-wrap; overflow-wrap: anywhere; } .answer-markdown :deep(img) { max-width: 100%; } .answer-markdown :deep(table) { display: block; max-width: 100%; overflow-x: auto; border-collapse: collapse; } .answer-markdown :deep(td), .answer-markdown :deep(th) { padding: 6px 10px; border: 1px solid var(--border); } .answer-markdown :deep(a) { color: var(--accent-text); } .answer-markdown :deep(blockquote) { margin-left: 0; padding-left: 12px; border-left: 3px solid var(--border-strong); color: var(--fg-muted); }
@media (max-width: 1080px) { .diagnostics-layout { grid-template-columns: 280px minmax(0, 1fr); } }
@media (max-width: 860px) { .page-heading { padding: 16px; } .toolbar { padding: 12px 16px; } .token-field { box-sizing: border-box; width: 100%; } .query-field { flex: 1 1 160px; width: auto; } .token-help { gap: 8px; } .error-notice { padding: 12px 16px; } .diagnostics-layout { grid-template-columns: minmax(0, 1fr); min-height: 0; } .run-list { border-right: 0; border-bottom: 1px solid var(--border); max-height: 340px; overflow-y: auto; } .diagnostics-detail { padding: 20px 16px; } .detail-heading { gap: 8px; } }
@media (max-width: 420px) { .usage-grid { grid-template-columns: minmax(0, 1fr); } }
</style>
