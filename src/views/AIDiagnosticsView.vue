<script setup lang="ts">
import { onMounted } from 'vue'
import { KeyRound, RefreshCw, Search } from 'lucide-vue-next'
import StatusPill from '../components/ui/StatusPill.vue'
import BranchTag from '../components/ui/BranchTag.vue'
import EmptyState from '../components/ui/EmptyState.vue'
import Notice from '../components/ui/Notice.vue'
import { useDiagnostics, listText, coverageDeltaText, stepJSON, seconds } from '../composables/useDiagnostics'
import { formatTime } from '../lib/format'
import { diagnosticLabel, statusLabel } from '../lib/labels'

const { token, status, query, runs, detail, loading, detailLoading, error, selectedID, nextCursor, workflow, taskFrame, contract, coverage, coverageItems, emptyMessage, loadRuns, openRun } = useDiagnostics()
// 此页只加载自身数据；未输入 Token 时不发送诊断请求。
onMounted(() => { if (token.value.trim()) void loadRuns() })
</script>

<template>
  <section class="diagnostics-page">
    <header class="page-heading"><h2>AI 诊断</h2><p>查看每次问答的任务框架、检索轮次、证据覆盖和校验结果</p></header>
    <form class="toolbar" @submit.prevent="loadRuns()">
      <label class="search-field token-field"><KeyRound :size="15" /><input v-model.trim="token" type="password" autocomplete="off" placeholder="诊断 Token" aria-label="诊断 Token" /></label>
      <select v-model="status" aria-label="状态"><option value="">全部状态</option><option v-for="value in ['succeeded', 'completed_with_gaps', 'insufficient_evidence', 'failed']" :key="value" :value="value">{{ statusLabel(value) }}</option></select>
      <label class="search-field query-field"><Search :size="15" /><input v-model="query" placeholder="按问题关键词筛选" aria-label="关键词" /></label>
      <button class="btn" type="submit" :disabled="loading || !token.trim()"><RefreshCw :size="15" />{{ loading ? '读取中…' : '读取' }}</button>
      <p class="token-help">需要 <code>ai.diagnostics.read</code> 能力的 Token，在<a href="/settings/">系统设置</a>中签发</p>
    </form>
    <div v-if="error" class="error-notice"><Notice variant="danger">{{ error }}</Notice></div>
    <div class="diagnostics-layout">
      <aside class="run-list" aria-label="诊断 Run 列表">
        <button v-for="item in runs" :key="item.run.id" class="run-row" :class="{ active: selectedID === item.run.id }" type="button" :aria-pressed="selectedID === item.run.id" @click="openRun(item.run.id)">
          <span class="run-meta"><StatusPill :status="item.run.status" /><code>#{{ item.run.id }}</code><time :title="formatTime(item.run.started_at)">{{ formatTime(item.run.started_at) }} · {{ seconds(item.duration_ms) }}</time></span>
          <span class="question" :title="item.user_question || item.session.title">{{ item.user_question || item.session.title }}</span>
        </button>
        <EmptyState v-if="!runs.length" :message="loading ? '正在读取 Run…' : emptyMessage" />
        <button v-if="nextCursor" class="btn more-runs" type="button" :disabled="loading" @click="loadRuns(true)">{{ loading ? '读取中…' : '读取更多' }}</button>
      </aside>
      <article class="diagnostics-detail" :aria-busy="detailLoading">
        <EmptyState v-if="detailLoading" message="正在读取诊断详情…" />
        <template v-else-if="detail">
          <header class="detail-heading"><div><h3>Run #{{ detail.run.id }}</h3><p>{{ detail.user_message.content }}</p></div><StatusPill :status="detail.run.verification_status || detail.run.status" /></header>
          <Notice v-if="detail.run.error_message" variant="danger">{{ detail.run.error_message }}</Notice>
          <div class="stage-grid">
            <section><h4>任务框架</h4><strong :title="taskFrame?.intent || detail.run.intent">{{ diagnosticLabel(taskFrame?.intent || detail.run.intent) }}</strong><small>{{ taskFrame?.user_goal || '暂无任务说明' }}</small><small v-if="taskFrame?.target_artifacts?.length">目标：{{ listText(taskFrame.target_artifacts) }}</small></section>
            <section><h4>证据契约</h4><strong>{{ contract?.contract_id || '—' }}</strong><small :title="`必需：${listText(contract?.required_keys)}；建议：${listText(contract?.recommended_keys)}`">必需 {{ contract?.required_keys?.length || 0 }} 项 · 建议 {{ contract?.recommended_keys?.length || 0 }} 项</small><small>必需：{{ listText(contract?.required_keys) }}</small><small>建议：{{ listText(contract?.recommended_keys) }}</small></section>
            <section><h4>证据包</h4><strong>{{ workflow?.evidence_bundle?.bundle_id || '—' }}</strong><small>{{ workflow?.evidence_bundle?.group_count ?? 0 }} 组 · 排除 {{ workflow?.evidence_bundle?.excluded_count ?? 0 }} 条</small></section>
            <section><h4>回答校验</h4><strong :title="workflow?.verification_report?.status || detail.run.verification_status">{{ diagnosticLabel(workflow?.verification_report?.status || detail.run.verification_status) }}</strong><small :title="workflow?.verification_report?.next_action">下一步：{{ diagnosticLabel(workflow?.verification_report?.next_action) }}</small><small v-if="workflow?.verification_report?.failed_checks?.length">未通过：{{ listText(workflow.verification_report.failed_checks) }}</small></section>
          </div>
          <section class="diagnostic-block"><h4>检索轮次 · {{ workflow?.retrieval_rounds?.length || 0 }}</h4>
            <div v-if="workflow?.retrieval_rounds?.length" class="table-scroll"><table><thead><tr><th>轮次</th><th>原因</th><th>查询</th><th class="number">新证据</th></tr></thead><tbody><template v-for="round in workflow.retrieval_rounds" :key="round.round"><tr><td><code>R{{ round.round }}</code></td><td :title="round.reason">{{ diagnosticLabel(round.reason) }}</td><td>{{ (round.searches || []).map(search => search.query).join(' · ') || '—' }}</td><td class="number">{{ round.new_evidence_count }}</td></tr><tr class="round-extra"><td colspan="4"><div>缺失键：<code>{{ listText(round.missing_contract_keys) }}</code></div><div>覆盖变化：{{ coverageDeltaText(round.coverage_delta) }}</div></td></tr></template></tbody></table></div>
            <EmptyState v-else message="暂无检索轮次" />
          </section>
          <section class="diagnostic-block"><div class="section-heading"><h4>契约覆盖</h4><StatusPill v-if="coverage?.status" :status="coverage.status" /></div><div v-for="item in coverageItems" :key="item.key" class="coverage-row"><StatusPill :status="item.status" /><code>{{ item.key }}</code><small>{{ item.reason || item.missing_detail || '暂无说明' }}</small></div><EmptyState v-if="!coverageItems.length" message="暂无契约覆盖信息" /></section>
          <section class="diagnostic-block"><h4>步骤 · {{ detail.steps?.length || 0 }}</h4><details v-for="step in detail.steps || []" :key="step.id" class="step"><summary><StatusPill :status="step.status" /><strong :title="step.agent_name">{{ diagnosticLabel(step.agent_name) }}</strong><code>{{ step.step_type }}</code><span class="step-duration">{{ step.latency_ms }} ms</span></summary><p v-if="step.error_message" class="step-error">{{ step.error_message }}</p><pre>{{ stepJSON(step) }}</pre></details><EmptyState v-if="!detail.steps?.length" message="暂无步骤记录" /></section>
          <section class="diagnostic-block"><h4>数据源 · {{ detail.data_sources?.repositories?.length || 0 }}</h4><div v-for="source in detail.data_sources?.repositories || []" :key="source.id" class="source-row"><div class="source-heading"><strong>{{ source.name }}</strong><BranchTag :label="source.default_target?.branch || source.default_branch" neutral /><StatusPill :status="source.latest_scan?.status || 'not_scanned'" /></div><small>扫描目录：<code>{{ (source.scan_paths || []).map(path => path.path).join(', ') || '—' }}</code></small></div><EmptyState v-if="!detail.data_sources?.repositories?.length" message="暂无数据源" /></section>
        </template>
        <EmptyState v-else :message="runs.length ? '选择 Run 查看诊断详情' : emptyMessage" />
      </article>
    </div>
  </section>
</template>

<style scoped>
.diagnostics-page { min-width: 0; color: var(--fg); } .page-heading { padding: 16px 24px; border-bottom: 1px solid var(--border); } h2 { margin: 0; font-size: var(--fs-xl); } .page-heading p { margin: 3px 0 0; color: var(--fg-muted); font-size: var(--fs-sm); }
.toolbar { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; padding: 10px 24px; border-bottom: 1px solid var(--border); } .search-field { display: flex; gap: 8px; align-items: center; padding: 0 9px; border: 1px solid var(--border-strong); border-radius: 6px; color: var(--fg-subtle); min-width: 0; background: var(--surface); }
.token-field { width: 260px; } .query-field { width: 220px; } .search-field input { width: 100%; min-width: 0; border: 0; height: 30px; color: var(--fg); background: transparent; font: inherit; } .token-field input { font-family: var(--font-mono); } select { width: auto; height: 32px; padding: 0 8px; border: 1px solid var(--border-strong); border-radius: 6px; background: var(--surface); color: var(--fg); font: inherit; }
.token-help { margin: 0 0 0 auto; font-size: var(--fs-xs); color: var(--fg-subtle); overflow-wrap: anywhere; } a { color: var(--accent-text); } .error-notice { padding: 12px 24px; }
.diagnostics-layout { display: grid; grid-template-columns: 320px minmax(0, 1fr); min-height: 520px; } .run-list { min-width: 0; border-right: 1px solid var(--border); padding: 8px; } .run-row { display: grid; gap: 5px; width: 100%; text-align: left; border: 0; border-radius: 6px; background: transparent; color: var(--fg); padding: 10px; cursor: pointer; font: inherit; } .run-row:hover { background: var(--surface-2); } .run-row.active { background: var(--surface-3); }
.run-meta { display: flex; align-items: center; flex-wrap: wrap; gap: 7px; color: var(--fg-subtle); font-size: var(--fs-xs); } time { font-variant-numeric: tabular-nums; } .question { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; } .more-runs { margin: 12px; }
.diagnostics-detail { min-width: 0; padding: 24px; } .detail-heading { display: flex; align-items: flex-start; gap: 16px; margin-bottom: 24px; } .detail-heading > div { flex: 1; min-width: 0; } h3 { margin: 0; font-size: var(--fs-lg); } .detail-heading p { margin: 4px 0 0; color: var(--fg-muted); overflow-wrap: anywhere; }
.stage-grid { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border: 1px solid var(--border); border-radius: 8px; overflow: hidden; margin: 20px 0; } .stage-grid section { display: flex; flex-direction: column; align-items: flex-start; gap: 5px; padding: 12px 14px; border-right: 1px solid var(--border); min-width: 0; overflow-wrap: anywhere; } .stage-grid section:last-child { border-right: 0; } h4 { margin: 0; color: var(--fg-muted); font-size: var(--fs-sm); font-weight: 600; } .stage-grid strong { font: 600 var(--fs-sm) var(--font-mono); } small { font-size: var(--fs-xs); color: var(--fg-muted); }
.diagnostic-block { margin-top: 24px; } .diagnostic-block > h4, .section-heading { margin-bottom: 10px; } .section-heading, .source-heading { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
.table-scroll { overflow-x: auto; border: 1px solid var(--border); border-radius: 8px; } table { border-collapse: collapse; width: 100%; text-align: left; font-size: var(--fs-sm); } th, td { padding: 8px 12px; border-bottom: 1px solid var(--border); } th { background: var(--surface-2); color: var(--fg-muted); font-weight: 600; } td { overflow-wrap: anywhere; } tr:last-child td { border-bottom: 0; } .number { text-align: right; font-variant-numeric: tabular-nums; white-space: nowrap; } .round-extra { color: var(--fg-muted); font-size: var(--fs-xs); } .round-extra td { background: var(--surface-2); }
code, pre { font-family: var(--font-mono); } .coverage-row { display: grid; grid-template-columns: 86px minmax(100px, 1fr) minmax(0, 2fr); align-items: start; gap: 10px; padding: 8px 0; border-bottom: 1px solid var(--border); overflow-wrap: anywhere; } .coverage-row code { font-size: var(--fs-xs); } .coverage-row:last-child { border-bottom: 0; }
.step { margin-top: 8px; border: 1px solid var(--border); border-radius: 6px; overflow: hidden; } .step summary { display: flex; flex-wrap: wrap; align-items: center; gap: 10px; padding: 8px 12px; cursor: pointer; font-size: var(--fs-sm); } .step summary code { overflow-wrap: anywhere; color: var(--fg-muted); min-width: 0; } .step-duration { margin-left: auto; color: var(--fg-subtle); font-variant-numeric: tabular-nums; } pre { max-width: 100%; overflow-x: auto; margin: 0; padding: 14px; background: var(--surface-2); border-top: 1px solid var(--border); font-size: var(--fs-xs); } .step-error { color: var(--danger); margin: 8px 12px; overflow-wrap: anywhere; }
.source-row { padding: 12px 0; border-bottom: 1px solid var(--border); overflow-wrap: anywhere; } .source-heading { margin-bottom: 5px; }
@media (max-width: 1080px) { .diagnostics-layout { grid-template-columns: 260px minmax(0, 1fr); } .stage-grid { grid-template-columns: repeat(2, minmax(0, 1fr)); } .stage-grid section:nth-child(2) { border-right: 0; } .stage-grid section:nth-child(-n+2) { border-bottom: 1px solid var(--border); } }
@media (max-width: 860px) { .page-heading { padding: 16px; } .toolbar { padding: 12px 16px; } .token-field { width: 100%; } .query-field { flex: 1; width: auto; } .token-help { width: 100%; margin-top: 4px; } .diagnostics-layout { grid-template-columns: minmax(0, 1fr); min-height: 0; } .run-list { border-right: 0; border-bottom: 1px solid var(--border); max-height: 300px; overflow-y: auto; } .diagnostics-detail { padding: 20px 16px; } .coverage-row { grid-template-columns: 76px minmax(0, 1fr); } .coverage-row small { grid-column: 2; } .table-scroll table { min-width: 440px; } .stage-grid section { padding: 12px; } }
</style>
