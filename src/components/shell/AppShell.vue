<script setup lang="ts">
import { computed, nextTick, ref } from 'vue'
import type { AppTab } from '../../lib/navigation'
import { provideRepos } from '../../composables/useRepos'
import AppSidebar from './AppSidebar.vue'
import MobileBar from './MobileBar.vue'
import PageHeader from './PageHeader.vue'
import RepoHeader from './RepoHeader.vue'
import NewRepoDialog from './NewRepoDialog.vue'
import DrawerPanel from '../ui/DrawerPanel.vue'
import EmptyState from '../ui/EmptyState.vue'
import Notice from '../ui/Notice.vue'
const props = defineProps<{ tab: AppTab }>()
const { selectedRepo, isGlobalTab, loaded, error } = provideRepos(props.tab)
const showMenu = ref(false)
const showNewRepo = ref(false)
const titles: Record<AppTab, string> = { docs: '文档', history: '提交历史', runs: '扫描记录', 'repo-settings': '仓库设置', ai: 'AI 问答', 'ai-config': 'AI 供应商', 'ai-diagnostics': 'AI 诊断', settings: '系统设置' }
const descriptions: Partial<Record<AppTab, string>> = { ai: '在全部仓库的智能最新文档和代码中检索，回答附带引用', 'ai-config': '管理全局模型供应商和问答路由', 'ai-diagnostics': '排查检索过程、证据契约和回答校验', settings: '访问令牌和远程集成' }
const title = computed(() => !isGlobalTab.value && selectedRepo.value ? selectedRepo.value.name : titles[props.tab])
async function openNewRepo() {
  // 先卸载移动导航，再打开弹窗，避免两个焦点陷阱同时争夺焦点。
  showMenu.value = false
  await nextTick()
  showNewRepo.value = true
}
</script>
<template>
  <div class="app-shell" :class="`${props.tab}-shell`">
    <aside class="desktop-sidebar"><AppSidebar @new-repo="openNewRepo" /></aside>
    <MobileBar :title="title" @menu="showMenu = true" />
    <DrawerPanel :open="showMenu" title="导航" class="left mobile-navigation" @close="showMenu = false"><AppSidebar @new-repo="openNewRepo" /></DrawerPanel>
    <main class="workspace">
      <RepoHeader v-if="!isGlobalTab && selectedRepo" />
      <PageHeader v-else-if="!['ai', 'ai-config', 'ai-diagnostics'].includes(props.tab)" :title="titles[props.tab]" :description="descriptions[props.tab]"><slot name="actions" /></PageHeader>
      <Notice v-if="error" variant="danger" class="shell-error">{{ error }}</Notice>
      <section v-if="!isGlobalTab && loaded && !selectedRepo" class="empty-repositories"><EmptyState message="还没有仓库"><p>添加 Git 仓库后，DocHarbor 会同步并扫描文档目录</p><button class="btn primary" @click="openNewRepo">新增仓库</button></EmptyState></section>
      <slot v-else />
    </main>
    <NewRepoDialog :open="showNewRepo" @close="showNewRepo = false" />
  </div>
</template>
<style scoped>
.app-shell { display: grid; grid-template-columns: 232px minmax(0, 1fr); min-height: 100dvh; }
.desktop-sidebar { position: sticky; top: 0; height: 100dvh; border-right: 1px solid var(--border); background: var(--bg); overflow: auto; }
.workspace { min-width: 0; background: var(--surface); padding-bottom: env(safe-area-inset-bottom); }
.ai-shell .workspace, .docs-shell .workspace, .history-shell .workspace { display: flex; flex-direction: column; height: 100dvh; min-height: 0; }
.workspace > :deep(header) { flex-shrink: 0; }
.shell-error { margin: 16px; }
.empty-repositories { padding: 64px 20px; }
.empty-repositories p { color: var(--fg-muted); }
:global(.mobile-navigation.drawer-panel) { margin-left: 0; margin-right: auto; width: min(264px, 100vw); }
@media(max-width: 1080px) { .history-shell .workspace { height: auto; } }
@media(max-width: 860px) { .app-shell { display: block; } .desktop-sidebar { display: none; } .ai-shell .workspace, .docs-shell .workspace { height: auto; } }
</style>
