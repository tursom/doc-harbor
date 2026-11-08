<script setup lang="ts">
import { BookOpen, GitBranch, ListChecks, RefreshCw, Settings } from 'lucide-vue-next'
import { useRepos } from '../../composables/useRepos'
import StatusPill from '../ui/StatusPill.vue'
import InlineConfirm from '../ui/InlineConfirm.vue'
import ScanProgress from './ScanProgress.vue'
import { statusLabel } from '../../lib/labels'
const { selectedRepo, tab, tabHref, submitting, scanRunning, scanSelected, cancelScan, cancelling, cancelRequested } = useRepos()
const tabs = [{ tab: 'docs', label: '文档', icon: BookOpen }, { tab: 'history', label: '提交历史', icon: GitBranch }, { tab: 'runs', label: '扫描记录', icon: ListChecks }, { tab: 'repo-settings', label: '仓库设置', icon: Settings }] as const
function scanTime(value: string) {
  return new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
}
</script>
<template>
  <header v-if="selectedRepo" class="repo-header">
    <div class="heading-row">
      <div class="repo-title"><h2>{{ selectedRepo.name }}</h2><StatusPill :status="selectedRepo.latest_scan?.status || '未扫描'" :label="`${statusLabel(selectedRepo.latest_scan?.status || '未扫描')}${selectedRepo.latest_scan?.error_count ? ` · ${selectedRepo.latest_scan.error_count} 个错误` : ''}`" /></div>
      <div class="scan-actions">
        <template v-if="scanRunning">
          <span>后台扫描中</span><button class="btn" :disabled="cancelling || cancelRequested" @click="cancelScan">{{ cancelling ? '请求取消中…' : cancelRequested ? '正在取消…' : '取消扫描' }}</button>
        </template>
        <template v-else>
          <button class="btn" :disabled="submitting" aria-label="立即扫描" title="增量扫描：跳过未变化且已成功扫描的分支" @click="scanSelected()"><RefreshCw :size="14" :class="{ spinning: submitting }" /><span>{{ submitting ? '提交中…' : '立即扫描' }}</span></button>
          <InlineConfirm :key="selectedRepo.id" label="完整重建" confirm-label="按当前文档范围完整重建索引" :busy="submitting" @confirm="scanSelected(true)" />
        </template>
      </div>
    </div>
    <p class="repo-meta"><span class="repo-url" :title="selectedRepo.repo_url">{{ selectedRepo.repo_url }}</span><span v-if="selectedRepo.latest_scan"> · 最近扫描 {{ scanTime(selectedRepo.latest_scan.started_at) }} · {{ selectedRepo.latest_scan.branch_count }} 个分支</span></p>
    <ScanProgress v-if="scanRunning && selectedRepo.latest_scan" :run="selectedRepo.latest_scan" />
    <!-- 扫描页已有错误计数提示与可展开详情；无错误计数的取消/中断原因仍需直接展示。 -->
    <p v-if="selectedRepo.latest_scan?.error_message && (tab !== 'runs' || !selectedRepo.latest_scan.error_count)" class="scan-error">{{ selectedRepo.latest_scan.error_message }}</p>
    <nav class="repo-tabs" aria-label="仓库页面"><a v-for="item in tabs" :key="item.tab" :href="tabHref(item.tab)" :class="{ active: tab === item.tab }" :aria-current="tab === item.tab ? 'page' : undefined"><component :is="item.icon" :size="15" />{{ item.label }}<span v-if="item.tab === 'docs'" class="count">{{ selectedRepo.document_count }}</span></a></nav>
  </header>
</template>
<style scoped>
.repo-header { padding: 14px 24px 0; border-bottom: 1px solid var(--border); }
.heading-row, .repo-title { display: flex; align-items: center; gap: 12px; min-width: 0; }
.heading-row { justify-content: space-between; }
.repo-title { flex-wrap: wrap; }
h2 { font-size: var(--fs-xl); margin: 0; overflow-wrap: anywhere; }
.heading-row > button { flex-shrink: 0; }
.scan-actions { display: flex; flex-wrap: wrap; align-items: center; justify-content: flex-end; gap: 8px; }
.scan-error { margin: 6px 0; color: var(--danger); font-size: var(--fs-sm); overflow-wrap: anywhere; }
.repo-meta { display: flex; flex-wrap: wrap; gap: 4px; margin: 3px 0 4px; color: var(--fg-subtle); font-size: var(--fs-sm); font-variant-numeric: tabular-nums; }
.repo-url { font-family: var(--font-mono); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.repo-tabs { display: flex; gap: 24px; }
.repo-tabs a { display: inline-flex; align-items: center; gap: 6px; padding: 10px 0; color: var(--fg-muted); border-bottom: 2px solid transparent; text-decoration: none; white-space: nowrap; }
.repo-tabs a.active { color: var(--fg); font-weight: 600; border-color: var(--accent); }
.count { background: var(--surface-2); border-radius: 4px; padding: 0 5px; font-size: var(--fs-xs); font-variant-numeric: tabular-nums; }
@media(max-width: 860px) { .repo-header { padding: 16px 16px 0; } .heading-row { align-items: flex-start; flex-wrap: wrap; } .scan-actions { justify-content: flex-start; } .repo-tabs { justify-content: space-between; gap: 10px; font-size: var(--fs-sm); } }
</style>
