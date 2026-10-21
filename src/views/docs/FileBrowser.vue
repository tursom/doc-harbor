<script setup lang="ts">
import { computed } from 'vue'
import { ChevronUp, FileText, Folder, Search } from 'lucide-vue-next'
import EmptyState from '../../components/ui/EmptyState.vue'
import type { FileEntry, RepoRef } from '../../types'
import { searchResultDir } from '../../composables/useDocs'

const props = defineProps<{
  files: FileEntry[]; branches: RepoRef[]; defaultBranch: string; viewMode: 'latest' | 'branch'; selectedBranch: string
  currentDir: string; searchText: string; versionID?: number; loading: boolean
  breadcrumbs: { label: string; path: string }[]; canGoParent: boolean
}>()
const emit = defineEmits<{
  view: [mode: 'latest' | 'branch']; branch: [branch: string]; searchText: [text: string]; search: []
  directory: [path: string]; open: [entry: FileEntry]; parent: []
}>()
const directories = computed(() => props.files.filter((entry) => entry.kind === 'dir').length)
</script>

<template>
  <aside class="file-browser" aria-label="文件列表">
    <div class="files-tools">
      <div class="view-controls">
        <div class="seg" aria-label="文档视图">
          <button type="button" :class="{ active: viewMode === 'latest' }" :aria-pressed="viewMode === 'latest'" @click="emit('view', 'latest')">智能最新</button>
          <button type="button" :class="{ active: viewMode === 'branch' }" :aria-pressed="viewMode === 'branch'" @click="emit('view', 'branch')">按分支</button>
        </div>
        <select v-if="viewMode === 'branch'" :value="selectedBranch" aria-label="分支" @change="emit('branch', ($event.target as HTMLSelectElement).value)">
          <option v-for="branch in branches" :key="branch.ref_name" :value="branch.ref_name">{{ branch.ref_name }}</option>
        </select>
      </div>
      <form class="file-search" role="search" @submit.prevent="emit('search')">
        <Search :size="14" aria-hidden="true" />
        <input :value="searchText" aria-label="搜索标题或路径" placeholder="搜索标题或路径" @input="emit('searchText', ($event.target as HTMLInputElement).value)" />
      </form>
    </div>
    <div class="crumbs">
      <button class="btn ghost icon sm" type="button" aria-label="上一级" title="上一级" :disabled="!canGoParent" @click="emit('parent')"><ChevronUp :size="14" /></button>
      <nav aria-label="目录路径">
        <button type="button" @click="emit('directory', '.')">根目录</button>
        <template v-for="crumb in breadcrumbs" :key="crumb.path"><span>/</span><button type="button" @click="emit('directory', crumb.path)">{{ crumb.label }}</button></template>
        <template v-if="currentDir === searchResultDir"><span>/</span><span>搜索结果</span></template>
      </nav>
    </div>
    <div class="file-list" :aria-busy="loading">
      <EmptyState v-if="loading" message="正在加载文件列表" />
      <EmptyState v-else-if="!files.length" :message="currentDir === searchResultDir ? '没有匹配的文档' : '此目录没有文档'" />
      <button v-for="entry in files" :key="`${entry.kind}:${entry.path}:${entry.version_id || 0}`" class="file-row" :class="{ active: entry.kind === 'file' && versionID === entry.version_id }" type="button" :title="entry.path" @click="emit('open', entry)">
        <Folder v-if="entry.kind === 'dir'" :size="16" aria-hidden="true" /><FileText v-else :size="16" aria-hidden="true" />
        <!-- 标题和来源分支分行排列，避免长分支名挤占标题；目录仅显示名称。 -->
        <span class="file-row-details">
          <span class="file-row-title" :title="entry.kind === 'dir' ? entry.name : entry.title || entry.name">{{ entry.kind === 'dir' ? entry.name : entry.title || entry.name }}</span>
          <small v-if="entry.kind === 'file' && viewMode === 'latest' && entry.source_branch && entry.source_branch !== defaultBranch" class="file-row-branch" :title="entry.source_branch">{{ entry.source_branch }}</small>
        </span>
      </button>
    </div>
    <footer>{{ directories }} 个目录 · {{ files.length - directories }} 个文件</footer>
  </aside>
</template>

<style scoped>
.file-browser { display: flex; flex-direction: column; min-width: 0; min-height: 0; border-right: 1px solid var(--border); background: var(--surface); }
.files-tools { display: flex; flex-direction: column; gap: 8px; padding: 12px; border-bottom: 1px solid var(--border); }
.view-controls { display: flex; gap: 6px; align-items: center; min-width: 0; }
.seg { flex-shrink: 0; }
.view-controls select { flex: 1; min-width: 0; width: 0; height: 26px; padding: 0 4px; font: var(--fs-sm)/1.5 var(--font-mono); }
.file-search { display: flex; align-items: center; gap: 6px; border: 1px solid var(--border-strong); border-radius: 6px; padding: 0 8px; color: var(--fg-subtle); }
.file-search:focus-within { border-color: var(--accent); }
.file-search input { border: 0; outline: 0; background: none; width: 100%; min-width: 0; height: 30px; padding: 0; color: var(--fg); }
.crumbs { display: flex; align-items: flex-start; gap: 2px; padding: 8px 8px 6px; font-size: var(--fs-sm); color: var(--fg-muted); }
.crumbs nav { display: flex; align-items: center; flex-wrap: wrap; gap: 2px; min-width: 0; }
.crumbs nav button { border: 0; background: none; padding: 3px 4px; color: inherit; overflow-wrap: anywhere; }
.crumbs nav button:hover { background: var(--surface-2); color: var(--fg); }
.file-list { flex: 1; overflow-y: auto; min-width: 0; padding: 0 6px 12px; }
.file-row { display: flex; align-items: flex-start; gap: 8px; width: 100%; min-width: 0; min-height: 30px; padding: 5px 8px; border: 0; border-radius: 6px; background: none; text-align: left; color: var(--fg); }
.file-row > svg { flex: 0 0 16px; margin-top: 2px; color: var(--fg-subtle); }
.file-row:hover { background: var(--surface-2); }
.file-row.active { background: var(--accent-subtle); }
.file-row.active .file-row-title { font-weight: 600; }
/* 不能把分支放在横向 flex 项：这三个类的几何尺寸由端到端测试锁定。 */
.file-row-details { display: flex; flex-direction: column; align-items: flex-start; gap: 3px; min-width: 0; flex: 1; }
.file-row-title { display: block; width: 100%; min-width: 0; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; line-height: 20px; }
.file-row-branch { display: block; box-sizing: border-box; max-width: 100%; min-width: 0; padding: 1px 5px; border-radius: 4px; white-space: nowrap; overflow: hidden; text-overflow: ellipsis; font: var(--fs-xs)/1.5 var(--font-mono); color: var(--accent-text); background: var(--accent-subtle); }
footer { padding: 8px 14px; padding-bottom: max(8px, env(safe-area-inset-bottom)); border-top: 1px solid var(--border); font-size: var(--fs-xs); color: var(--fg-subtle); }
@media (max-width: 860px) { .file-browser { border-right: 0; } }
</style>
