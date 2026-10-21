<script setup lang="ts">
import { computed } from 'vue'
import { ChevronDown, ChevronRight } from 'lucide-vue-next'
import { useRepos } from '../composables/useRepos'
import { useScanRuns } from '../composables/useScanRuns'
import { formatDetailJSON, formatDuration, formatSyncInterval, formatTime } from '../lib/format'
import { triggerLabel } from '../lib/labels'
import StatusPill from '../components/ui/StatusPill.vue'
import Notice from '../components/ui/Notice.vue'
import EmptyState from '../components/ui/EmptyState.vue'

const repos = useRepos()
const { selectedRepo } = repos
const { scanRuns, expanded, loading, error, canExpand, toggleRun } = useScanRuns(repos)
const latest = computed(() => selectedRepo.value?.latest_scan)
const scanPaths = computed(() => selectedRepo.value?.scan_paths.map((path) => path.path).join(', ') || '—')
</script>

<template>
  <section v-if="selectedRepo" class="scans-view" aria-label="扫描记录">
    <div class="scan-summary">
      <div><span>最近一次</span><StatusPill :status="latest?.status || 'not_scanned'" /></div>
      <div><span>自动拉取</span><strong>{{ formatSyncInterval(selectedRepo.sync_interval_seconds) }}</strong></div>
      <div><span>追踪分支</span><strong>{{ latest?.branch_count ?? 0 }} 个</strong></div>
      <div><span>扫描目录</span><strong class="scan-paths" :title="scanPaths">{{ scanPaths }}</strong></div>
    </div>
    <Notice v-if="latest && latest.error_count > 0" variant="warn"><strong>最近一次扫描有 {{ latest.error_count }} 个错误。</strong> {{ latest.file_count }} 个文件已更新索引。展开该行查看错误详情。</Notice>
    <Notice v-if="error" variant="danger">{{ error }}</Notice>
    <EmptyState v-if="loading || !scanRuns.length" :message="loading ? '正在加载扫描记录' : '暂无扫描记录'" />
    <div v-else class="scan-table-scroll">
      <table class="scan-table">
        <thead><tr><th>状态</th><th>触发方式</th><th class="num hide-sm">分支</th><th class="num">文件</th><th class="num hide-sm">跳过</th><th class="num">错误</th><th>开始时间</th><th class="num">耗时</th></tr></thead>
        <tbody>
          <template v-for="run in scanRuns" :key="run.id">
            <tr :class="{ expandable: canExpand(run), expanded: expanded.has(run.id) }" @click="toggleRun(run)">
              <td><button v-if="canExpand(run)" class="expand-run" type="button" :aria-label="`${expanded.has(run.id) ? '收起' : '展开'}扫描 #${run.id} 错误详情`" :aria-expanded="expanded.has(run.id)" :aria-controls="`scan-detail-${run.id}`" @click.stop="toggleRun(run)"><component :is="expanded.has(run.id) ? ChevronDown : ChevronRight" :size="13" /><StatusPill :status="run.status" /></button><StatusPill v-else :status="run.status" /></td>
              <td :title="run.trigger_type">{{ triggerLabel(run.trigger_type) }}</td>
              <td class="num hide-sm" :class="{ zero: !run.branch_count }">{{ run.branch_count }}</td>
              <td class="num" :class="{ zero: !run.file_count }">{{ run.file_count }}</td>
              <td class="num hide-sm" :class="{ zero: !run.skipped_count }">{{ run.skipped_count }}</td>
              <td class="num" :class="run.error_count > 0 ? 'bad' : 'zero'">{{ run.error_count }}</td>
              <td class="scan-time">{{ formatTime(run.started_at) }}</td><td class="num">{{ formatDuration(run.started_at, run.finished_at) }}</td>
            </tr>
            <tr v-if="expanded.has(run.id)" :id="`scan-detail-${run.id}`" class="scan-detail"><td colspan="8"><div class="scan-detail-content"><p v-if="run.error_message">{{ run.error_message }}</p><pre v-if="run.detail_json">{{ formatDetailJSON(run.detail_json) }}</pre><p v-if="!run.error_message && !run.detail_json">本次扫描未返回错误详情</p></div></td></tr>
          </template>
        </tbody>
      </table>
    </div>
  </section>
</template>

<style scoped>
.scans-view { padding: 20px 24px max(24px, env(safe-area-inset-bottom)); display: flex; flex-direction: column; gap: 20px; min-width: 0; background: var(--surface); }
.scan-summary { display: grid; grid-template-columns: repeat(4, minmax(0, 1fr)); border: 1px solid var(--border); border-radius: 8px; }
.scan-summary > div { display: flex; align-items: flex-start; flex-direction: column; gap: 4px; padding: 12px 16px; border-right: 1px solid var(--border); min-width: 0; }
.scan-summary > div:last-child { border-right: 0; }
.scan-summary > div > span:first-child { color: var(--fg-muted); font-size: var(--fs-sm); }
.scan-summary strong { font-size: var(--fs-md); font-variant-numeric: tabular-nums; }
.scan-paths { font-family: var(--font-mono); overflow-wrap: anywhere; }
.scan-table-scroll { overflow-x: auto; border: 1px solid var(--border); border-radius: 8px; min-width: 0; }
.scan-table { width: 100%; border-collapse: collapse; font-size: var(--fs-md); }
th { text-align: left; background: var(--surface-2); font-weight: 600; font-size: var(--fs-sm); color: var(--fg-muted); }
th, td { padding: 8px 12px; height: 38px; border-bottom: 1px solid var(--border); white-space: nowrap; }
td { color: var(--fg-muted); }
tbody > tr:last-child td { border-bottom: 0; }
.num { text-align: right; font-variant-numeric: tabular-nums; }
.zero { color: var(--fg-subtle); }
.bad { color: var(--danger); font-weight: 600; }
.scan-time { font-variant-numeric: tabular-nums; }
.expandable { cursor: pointer; }
.expandable:hover, .expanded { background: var(--surface-2); }
.expand-run { display: inline-flex; align-items: center; gap: 4px; padding: 0; border: 0; background: none; color: var(--fg-muted); }
.scan-detail td { padding: 0; background: var(--surface-2); white-space: normal; }
.scan-detail-content { padding: 12px 16px; max-width: 100%; }
.scan-detail-content p { white-space: pre-wrap; overflow-wrap: anywhere; margin: 0 0 8px; color: var(--danger); }
.scan-detail-content pre { margin: 0; overflow-x: auto; max-width: 100%; font: var(--fs-sm)/1.7 var(--font-mono); color: var(--fg-muted); }
@media (max-width: 860px) { .scans-view { padding: 16px 12px max(24px, env(safe-area-inset-bottom)); gap: 16px; } .scan-summary { grid-template-columns: repeat(2, minmax(0, 1fr)); } .scan-summary > div:nth-child(2) { border-right: 0; } .scan-summary > div:nth-child(-n+2) { border-bottom: 1px solid var(--border); } .hide-sm { display: none; } }
</style>
