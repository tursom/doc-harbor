<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { Check, Copy } from 'lucide-vue-next'
import { useRepos } from '../composables/useRepos'
import { useHistory } from '../composables/useHistory'
import { decorationBadges, graphCenterY, graphConnectorPath, graphLaneX, graphLineColor, graphRowHeight } from '../lib/git-graph'
import { formatHistoryTime, formatTime, shortCommit } from '../lib/format'
import CopyField from '../components/ui/CopyField.vue'
import EmptyState from '../components/ui/EmptyState.vue'
import Notice from '../components/ui/Notice.vue'

const repos = useRepos()
const { selectedRepo, branches } = repos
const { commits, selectedCommit, historyBranch, loading, detailLoading, error, graphRows, graphWidth, openCommit, changeHistoryBranch } = useHistory(repos)
const subject = computed(() => selectedCommit.value?.message.split('\n')[0] || '')
const body = computed(() => selectedCommit.value?.message.split('\n').slice(1).join('\n').trim() || '')
const refs = computed(() => decorationBadges(selectedCommit.value?.decorations || ''))
const copied = ref(false)
const copyFallback = ref(false)
watch(selectedCommit, () => { copied.value = false; copyFallback.value = false })
async function copySHA() {
  if (!selectedCommit.value) return
  try { await navigator.clipboard.writeText(selectedCommit.value.sha); copied.value = true }
  catch { copyFallback.value = true }
}
</script>

<template>
  <section v-if="selectedRepo" class="history-view" aria-label="提交历史">
    <Notice v-if="error" class="history-error" variant="danger">{{ error }}</Notice>
    <div class="history-main">
      <div class="history-toolbar">
        <select v-model="historyBranch" aria-label="分支" @change="changeHistoryBranch"><option value="">全部分支</option><option v-for="branch in branches" :key="branch.ref_name" :value="branch.ref_name">{{ branch.ref_name }}</option></select>
        <span>最近 {{ commits.length }} 条</span>
      </div>
      <div class="git-graph-scroll" :aria-busy="loading">
        <div class="git-graph-table" :style="{ '--graph-width': `${graphWidth}px` }">
          <div class="git-graph-head"><span aria-hidden="true"></span><span>提交说明</span><span class="git-author-cell">作者</span><span>时间</span><span class="git-sha-cell">SHA</span></div>
          <EmptyState v-if="!graphRows.length" :message="loading ? '正在加载提交历史' : '暂无提交历史'" />
          <button v-for="row in graphRows" :key="row.commit.sha" class="git-graph-row" :class="{ active: selectedCommit?.sha === row.commit.sha }" :aria-pressed="selectedCommit?.sha === row.commit.sha" type="button" @click="openCommit(row.commit.sha)">
            <span class="git-graph-cell" :title="row.commit.parents.length ? `parents: ${row.commit.parents.join(', ')}` : 'root commit'">
              <svg :width="graphWidth" :height="graphRowHeight" :viewBox="`0 0 ${graphWidth} ${graphRowHeight}`" aria-hidden="true">
                <line v-for="lane in row.beforeLanes" :key="`before-${lane}`" :x1="graphLaneX(lane)" :x2="graphLaneX(lane)" y1="0" :y2="graphCenterY" :stroke="graphLineColor(lane)" stroke-width="2" stroke-linecap="round" />
                <line v-for="lane in row.afterLanes" :key="`after-${lane}`" :x1="graphLaneX(lane)" :x2="graphLaneX(lane)" :y1="graphCenterY" :y2="graphRowHeight" :stroke="graphLineColor(lane)" stroke-width="2" stroke-linecap="round" />
                <path v-for="lane in row.connectorLanes" :key="`connector-${lane}`" :d="graphConnectorPath(row.lane, lane)" :stroke="graphLineColor(lane)" stroke-width="2" stroke-linecap="round" fill="none" />
                <circle :cx="graphLaneX(row.lane)" :cy="graphCenterY" r="4.5" fill="var(--surface)" :stroke="graphLineColor(row.lane)" stroke-width="2" />
              </svg>
            </span>
            <span class="git-desc-cell" :title="row.commit.decorations ? `${row.commit.decorations} ${row.commit.message}` : row.commit.message">
              <span v-for="badge in row.badges" :key="`${row.commit.sha}:${badge.label}`" class="ref-badge" :class="badge.type">{{ badge.label }}</span>
              <span class="commit-message" :class="{ merge: row.isMerge }">{{ row.commit.message.split('\n')[0] }}</span>
            </span>
            <span class="git-author-cell" :title="row.commit.author_email">{{ row.commit.author }}</span>
            <time class="git-date-cell" :datetime="row.commit.commit_time">{{ formatHistoryTime(row.commit.commit_time) }}</time>
            <span class="git-sha-cell" :title="row.commit.sha">{{ shortCommit(row.commit.sha) }}</span>
          </button>
        </div>
      </div>
    </div>
    <aside class="commit-detail" :aria-busy="detailLoading" aria-label="提交详情">
      <template v-if="selectedCommit">
        <div><h3>{{ subject }}</h3><p v-if="body" class="commit-body">{{ body }}</p></div>
        <dl class="commit-meta">
          <dt>SHA</dt><dd><code :title="selectedCommit.sha">{{ shortCommit(selectedCommit.sha) }}…</code><button class="btn ghost icon sm" type="button" :aria-label="copied ? '已复制完整 SHA' : '复制完整 SHA'" :title="copied ? '已复制完整 SHA' : '复制完整 SHA'" @click="copySHA"><component :is="copied ? Check : Copy" :size="14" /></button></dd>
          <dt>作者</dt><dd :title="selectedCommit.author_email">{{ selectedCommit.author }}</dd>
          <dt>时间</dt><dd>{{ formatTime(selectedCommit.commit_time) }}</dd>
          <dt>父提交</dt><dd><template v-if="selectedCommit.parents.length"><button v-for="parent in selectedCommit.parents" :key="parent" class="parent-commit" type="button" :title="parent" @click="openCommit(parent)">{{ shortCommit(parent) }}</button></template><span v-else>—</span></dd>
          <template v-if="refs.length"><dt>引用</dt><dd><span v-for="badge in refs" :key="badge.label" class="ref-badge" :class="badge.type" :title="badge.label">{{ badge.label }}</span></dd></template>
        </dl>
        <CopyField v-if="copyFallback" :value="selectedCommit.sha" label="完整 SHA" />
        <section><h4>变更文件 · {{ selectedCommit.files.length }}</h4>
          <div v-for="file in selectedCommit.files" :key="`${file.status}:${file.path}:${file.old_path}`" class="change-row"><span class="change-status" :class="file.status.charAt(0)" :title="file.status">{{ file.status.charAt(0) }}</span><code>{{ file.old_path ? `${file.old_path} → ${file.new_path || file.path}` : file.path }}</code></div>
          <p v-if="!selectedCommit.files.length" class="muted">没有文件变更</p>
        </section>
      </template>
      <EmptyState v-else :message="detailLoading ? '正在加载提交详情' : '选择提交查看详情'" />
    </aside>
  </section>
</template>

<style scoped>
.history-view { display: grid; grid-template-columns: minmax(0, 1fr) 380px; flex: 1; min-width: 0; min-height: 0; height: 100%; background: var(--surface); }
.history-error { grid-column: 1 / -1; }
.history-main { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
.history-toolbar { display: flex; align-items: center; justify-content: space-between; gap: 12px; padding: 10px 24px; border-bottom: 1px solid var(--border); }
.history-toolbar select { width: auto; max-width: min(320px, 75%); height: 30px; font-family: var(--font-mono); }
.history-toolbar > span { color: var(--fg-subtle); font-size: var(--fs-sm); white-space: nowrap; }
.git-graph-scroll { flex: 1; min-width: 0; overflow: auto; }
.git-graph-table { min-width: calc(var(--graph-width) + 470px); }
.git-graph-head, .git-graph-row { display: grid; grid-template-columns: var(--graph-width) minmax(120px, 1fr) 82px 160px 86px; align-items: center; width: 100%; text-align: left; }
.git-graph-head { position: sticky; top: 0; z-index: 1; min-height: 36px; font-size: var(--fs-sm); font-weight: 600; background: var(--surface); color: var(--fg-muted); border-bottom: 1px solid var(--border); }
.git-graph-row { height: 34px; padding: 0; border: 0; box-shadow: inset 0 -1px var(--border); border-radius: 0; background: none; color: var(--fg); font-size: var(--fs-md); }
.git-graph-row:hover { background: var(--surface-2); }
.git-graph-row.active { background: var(--accent-subtle); }
.git-graph-row > :not(:first-child), .git-graph-head > :not(:first-child) { min-width: 0; padding-right: 12px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.git-graph-cell, .git-graph-cell svg { display: block; }
.git-desc-cell { line-height: 34px; }
.ref-badge { display: inline-block; max-width: 100%; vertical-align: middle; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; border-radius: 4px; padding: 1px 5px; margin-right: 6px; font: var(--fs-xs)/1.5 var(--font-mono); color: var(--info); background: var(--info-bg); }
.ref-badge.head { font-weight: 700; border: 1px solid var(--info); }
.ref-badge.tag { border-radius: 8px; }
.ref-badge.remote { border: 1px dashed var(--info); }
.ref-badge.stash { color: var(--warn); background: var(--warn-bg); }
.commit-message.merge, .git-author-cell, .git-date-cell, .git-sha-cell { color: var(--fg-muted); }
.git-date-cell { font-size: var(--fs-xs); font-variant-numeric: tabular-nums; }
.git-sha-cell { font: var(--fs-sm)/1.5 var(--font-mono); }
.commit-detail { min-width: 0; border-left: 1px solid var(--border); overflow-y: auto; padding: 20px; display: flex; flex-direction: column; gap: 22px; }
h3 { margin: 0; font-size: var(--fs-lg); line-height: 1.45; overflow-wrap: anywhere; }
.commit-body { white-space: pre-wrap; color: var(--fg-muted); margin: 8px 0 0; font-size: var(--fs-sm); line-height: 1.7; overflow-wrap: anywhere; }
.commit-meta { display: grid; grid-template-columns: 64px minmax(0, 1fr); gap: 8px 10px; margin: 0; font-size: var(--fs-sm); }
.commit-meta dt { color: var(--fg-subtle); }
.commit-meta dd { display: flex; align-items: center; flex-wrap: wrap; gap: 4px; margin: 0; min-width: 0; overflow-wrap: anywhere; font-variant-numeric: tabular-nums; }
code, .parent-commit { font-family: var(--font-mono); }
.parent-commit { color: var(--accent-text); border: 0; background: none; padding: 0 4px 0 0; }
h4 { margin: 0 0 8px; font-size: var(--fs-xs); color: var(--fg-subtle); }
.change-row { display: flex; gap: 8px; padding: 6px 0; border-bottom: 1px solid var(--border); align-items: flex-start; font-size: var(--fs-sm); }
.change-row code { min-width: 0; overflow-wrap: anywhere; }
.change-status { flex: 0 0 18px; text-align: center; height: 18px; border-radius: 4px; font: 600 var(--fs-xs)/18px var(--font-mono); color: var(--fg-muted); background: var(--surface-2); }
.change-status.A { color: var(--ok); background: var(--ok-bg); }.change-status.M { color: var(--warn); background: var(--warn-bg); }.change-status.D { color: var(--danger); background: var(--danger-bg); }.change-status.R { color: var(--info); background: var(--info-bg); }
.muted { color: var(--fg-subtle); }
@media (max-width: 1080px) { .history-view { display: block; height: auto; } .git-graph-scroll { max-height: 60vh; } .commit-detail { border-left: 0; border-top: 1px solid var(--border); } }
@media (max-width: 860px) { .git-author-cell, .git-sha-cell { display: none; } .git-graph-table { min-width: calc(var(--graph-width) + 290px); } .git-graph-head, .git-graph-row { grid-template-columns: var(--graph-width) minmax(120px, 1fr) 158px; } .history-toolbar { padding: 10px 16px; } .commit-detail { padding: 20px 16px max(24px, env(safe-area-inset-bottom)); } }
</style>
