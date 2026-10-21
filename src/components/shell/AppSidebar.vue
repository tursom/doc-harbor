<script setup lang="ts">
import { Activity, Bot, Plus, Settings, SlidersHorizontal } from 'lucide-vue-next'
import { useRepos } from '../../composables/useRepos'
import ThemeSwitch from './ThemeSwitch.vue'
const emit = defineEmits<{ newRepo: [] }>()
const { repos, selectedRepo, tab, tabHref, repoHref, isGlobalTab } = useRepos()
</script>
<template>
  <div class="sidebar-content">
    <div class="brand"><img src="/icon.svg" alt="" width="28" height="28" /><h1>DocHarbor</h1></div>
    <nav class="global-nav" aria-label="全局功能">
      <a class="ai-link" :class="{ active: tab === 'ai' }" :href="tabHref('ai')" :aria-current="tab === 'ai' ? 'page' : undefined"><Bot :size="16" />AI 问答</a>
    <nav class="repositories" aria-label="仓库">
      <div class="group-heading"><span class="group-label">仓库</span><button class="btn ghost icon sm" aria-label="新增仓库" @click="emit('newRepo')"><Plus :size="15" /></button></div>
      <a v-for="repo in repos" :key="repo.id" :href="repoHref(repo)" :class="{ active: !isGlobalTab && selectedRepo?.id === repo.id }" :aria-current="!isGlobalTab && selectedRepo?.id === repo.id ? 'page' : undefined">
        <span class="scan-dot" :class="repo.latest_scan?.status" :title="repo.latest_scan?.status || '未扫描'"></span><span class="repo-name" :title="repo.name">{{ repo.name }}</span><span class="repo-count">{{ repo.latest_scan?.file_count ?? 0 }}</span>
      </a>
      <p v-if="!repos.length" class="no-repos">添加仓库以开始阅读</p>
    </nav>
      <div class="management">
        <p class="group-label">管理</p>
        <a :class="{ active: tab === 'ai-config' }" :href="tabHref('ai-config')" :aria-current="tab === 'ai-config' ? 'page' : undefined"><SlidersHorizontal :size="15" />AI 供应商</a>
        <a :class="{ active: tab === 'ai-diagnostics' }" :href="tabHref('ai-diagnostics')" :aria-current="tab === 'ai-diagnostics' ? 'page' : undefined"><Activity :size="15" />AI 诊断</a>
        <a :class="{ active: tab === 'settings' }" :href="tabHref('settings')" :aria-current="tab === 'settings' ? 'page' : undefined"><Settings :size="15" />系统设置</a>
      </div>
    </nav>
    <div class="theme-footer"><ThemeSwitch /></div>
  </div>
</template>
<style scoped>
.sidebar-content { display: grid; grid-template-rows: auto auto auto auto 1fr auto; min-height: 100%; padding: 12px 10px; }
.brand { grid-row: 1; display: flex; align-items: center; gap: 9px; padding: 4px 6px 12px; }
h1 { font-size: var(--fs-lg); margin: 0; font-weight: 750; }
/* 导航的 DOM 顺序与视觉一致，Tab 依次经过问答、仓库、管理；仓库有独立命名的导航区域。 */
.global-nav { display: contents; }
.ai-link { grid-row: 2; }
.management { grid-row: 4; padding: 14px 0 12px; }
.repositories { grid-row: 3; padding-top: 14px; overflow-y: auto; min-height: 0; }
a { display: flex; gap: 9px; align-items: center; min-height: 32px; border-radius: 6px; padding: 5px 9px; margin: 2px 0; text-decoration: none; color: var(--fg-muted); }
a:hover { background: var(--surface-2); color: var(--fg); }
a.active { background: var(--surface-3); color: var(--fg); font-weight: 600; }
.group-heading { display: flex; align-items: center; justify-content: space-between; padding: 0 3px 6px 9px; }
.group-label { color: var(--fg-subtle); font-size: var(--fs-xs); font-weight: 600; }
p.group-label { padding: 0 9px; margin: 2px 0 8px; }
.repo-name { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; flex: 1; }
.repo-count { font-family: var(--font-mono); font-size: var(--fs-xs); color: var(--fg-subtle); font-variant-numeric: tabular-nums; }
.scan-dot { width: 6px; height: 6px; border-radius: 50%; background: var(--fg-subtle); flex-shrink: 0; }
.scan-dot.success { background: var(--ok); }.scan-dot.partial_success { background: var(--warn); }.scan-dot.failed { background: var(--danger); }
.no-repos { color: var(--fg-subtle); padding: 0 9px; font-size: var(--fs-sm); }
.theme-footer { grid-row: 6; padding-top: 12px; }
.theme-footer :deep(.theme-switch) { grid-template-columns: 1.45fr 1fr 1fr; }
.theme-footer :deep(.theme-switch button) { white-space: nowrap; }
</style>
