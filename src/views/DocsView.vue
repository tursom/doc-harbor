<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRepos } from '../composables/useRepos'
import { useDocs } from '../composables/useDocs'
import type { DocHeading } from '../composables/useMarkdownPreview'
import Notice from '../components/ui/Notice.vue'
import FileBrowser from './docs/FileBrowser.vue'
import DocReader from './docs/DocReader.vue'
import DocRail from './docs/DocRail.vue'

const repos = useRepos()
const { selectedRepo, branches } = repos
const { files, fileContent, docEvents, viewMode, selectedBranch, currentDir, searchText, error, eventsError, listLoading, contentLoading, eventsLoading, reading, breadcrumbs, canGoParent, loadFiles, openFile, openVersion, loadBranchFiles, switchView, goParent, runSearch, backToFiles } = useDocs(repos)
const headings = ref<DocHeading[]>([])
const activeHeading = ref('')
const readingArea = ref<HTMLElement | null>(null)
const rail = ref<InstanceType<typeof DocRail> | null>(null)
watch(fileContent, () => { headings.value = []; activeHeading.value = ''; readingArea.value?.scrollTo({ top: 0 }) })
function changeBranch(branch: string) { selectedBranch.value = branch; void loadBranchFiles() }
function goHeading(id: string) {
  const element = [...(readingArea.value?.querySelectorAll<HTMLElement>('[id]') || [])].find((item) => item.id === id)
  element?.scrollIntoView({ behavior: 'smooth', block: 'start' })
  activeHeading.value = id
}
</script>

<template>
  <section v-if="selectedRepo" class="docs-view" :class="{ 'has-file': reading }" aria-label="文档">
    <Notice v-if="error" class="docs-error" variant="danger">{{ error }}</Notice>
    <FileBrowser :files="files" :branches="branches" :default-branch="selectedRepo.default_branch" :view-mode="viewMode" :selected-branch="selectedBranch" :current-dir="currentDir" :search-text="searchText" :version-i-d="fileContent?.version_id" :loading="listLoading" :breadcrumbs="breadcrumbs" :can-go-parent="canGoParent" @view="switchView" @branch="changeBranch" @search-text="searchText = $event" @search="runSearch" @directory="loadFiles" @open="openFile" @parent="goParent" />
    <div ref="readingArea" class="reading-area">
      <DocReader :key="fileContent?.version_id || 'empty'" :content="fileContent" :default-branch="selectedRepo.default_branch" :view-mode="viewMode" :loading="contentLoading" @back="backToFiles" @history="rail?.scrollEvents()" @headings="headings = $event" @active-heading="activeHeading = $event" />
      <DocRail v-if="fileContent" ref="rail" :content="fileContent" :events="docEvents" :events-error="eventsError" :events-loading="eventsLoading" :headings="headings" :active-heading="activeHeading" @heading="goHeading" @version="openVersion" />
    </div>
  </section>
</template>

<style scoped>
.docs-view { display: grid; grid-template-columns: 300px minmax(0, 1fr); flex: 1; height: 100%; min-height: 0; min-width: 0; background: var(--surface); }
.docs-error { grid-column: 1 / -1; }
.reading-area { display: grid; grid-template-columns: minmax(0, 1fr) 232px; align-items: start; min-width: 0; min-height: 0; height: 100%; overflow-y: auto; }
.reading-area:not(:has(.doc-rail)) { grid-template-columns: minmax(0, 1fr); }
@media (max-width: 1280px) { .reading-area { grid-template-columns: minmax(0, 1fr); display: block; } }
@media (max-width: 860px) {
  .docs-view { display: flex; flex-direction: column; height: auto; min-height: 100%; }
  .docs-view.has-file :deep(.file-browser) { display: none; }
  .docs-view:not(.has-file) .reading-area { display: none; }
  .reading-area { display: block; height: auto; overflow: visible; }
  .docs-view :deep(.file-browser) { flex: 1; }
}
</style>
