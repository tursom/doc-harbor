<script setup lang="ts">
import { BookOpen, GitBranch, ListChecks, RefreshCw, Settings } from 'lucide-vue-next'
import { useRepos } from '../../composables/useRepos'
import StatusPill from '../ui/StatusPill.vue'
import { statusLabel } from '../../lib/labels'
const { selectedRepo, tab, tabHref, busy, scanSelected } = useRepos()
const tabs = [{ tab: 'docs', label: '文档', icon: BookOpen }, { tab: 'history', label: '提交历史', icon: GitBranch }, { tab: 'runs', label: '扫描记录', icon: ListChecks }, { tab: 'repo-settings', label: '仓库设置', icon: Settings }] as const
function scanTime(value: string) {
  return new Date(value).toLocaleString('zh-CN', { month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hour12: false })
}
</script>
<template>
  <header v-if="selectedRepo" class="repo-header">
    <div class="heading-row">
      <div class="repo-title"><h2>{{ selectedRepo.name }}</h2><StatusPill :status="selectedRepo.latest_scan?.status || '未扫描'" :label="`${statusLabel(selectedRepo.latest_scan?.status || '未扫描')}${selectedRepo.latest_scan?.error_count ? ` · ${selectedRepo.latest_scan.error_count} 个错误` : ''}`" /></div>
      <button class="btn" :disabled="busy" aria-label="立即扫描" @click="scanSelected"><RefreshCw :size="14" :class="{ spinning: busy }" /><span>{{ busy ? '扫描中…' : '立即扫描' }}</span></button>
    </div>
    <p class="repo-meta"><span class="repo-url" :title="selectedRepo.repo_url">{{ selectedRepo.repo_url }}</span><span v-if="selectedRepo.latest_scan"> · 最近扫描 {{ scanTime(selectedRepo.latest_scan.started_at) }} · {{ selectedRepo.latest_scan.branch_count }} 个分支</span></p>
    <nav class="repo-tabs" aria-label="仓库页面"><a v-for="item in tabs" :key="item.tab" :href="tabHref(item.tab)" :class="{ active: tab === item.tab }" :aria-current="tab === item.tab ? 'page' : undefined"><component :is="item.icon" :size="15" />{{ item.label }}<span v-if="item.tab === 'docs'" class="count">{{ selectedRepo.latest_scan?.file_count || 0 }}</span></a></nav>
  </header>
</template>
<style scoped>
.repo-header { padding: 14px 24px 0; border-bottom: 1px solid var(--border); }
.heading-row, .repo-title { display: flex; align-items: center; gap: 12px; min-width: 0; }
.heading-row { justify-content: space-between; }
.repo-title { flex-wrap: wrap; }
h2 { font-size: var(--fs-xl); margin: 0; overflow-wrap: anywhere; }
.heading-row > button { flex-shrink: 0; }
.repo-meta { display: flex; flex-wrap: wrap; gap: 4px; margin: 3px 0 4px; color: var(--fg-subtle); font-size: var(--fs-sm); font-variant-numeric: tabular-nums; }
.repo-url { font-family: var(--font-mono); overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.repo-tabs { display: flex; gap: 24px; }
.repo-tabs a { display: inline-flex; align-items: center; gap: 6px; padding: 10px 0; color: var(--fg-muted); border-bottom: 2px solid transparent; text-decoration: none; white-space: nowrap; }
.repo-tabs a.active { color: var(--fg); font-weight: 600; border-color: var(--accent); }
.count { background: var(--surface-2); border-radius: 4px; padding: 0 5px; font-size: var(--fs-xs); font-variant-numeric: tabular-nums; }
@media(max-width: 860px) { .repo-header { padding: 16px 16px 0; } .heading-row > button span { display: none; } .repo-tabs { justify-content: space-between; gap: 10px; font-size: var(--fs-sm); } }
</style>
