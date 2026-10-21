<script setup lang="ts">
import { computed, ref } from 'vue'
import { Bot, ChevronLeft, Copy, Download, ExternalLink, History } from 'lucide-vue-next'
import BranchTag from '../../components/ui/BranchTag.vue'
import CopyField from '../../components/ui/CopyField.vue'
import DropdownMenu from '../../components/ui/DropdownMenu.vue'
import EmptyState from '../../components/ui/EmptyState.vue'
import MarkdownBody from './MarkdownBody.vue'
import { downloadURL, htmlPreviewURL } from '../../api'
import { isHTMLContent } from '../../lib/content-type'
import { formatBytes, formatTime, shortSha } from '../../lib/format'
import type { FileContent } from '../../types'
import type { DocHeading } from '../../composables/useMarkdownPreview'

const props = defineProps<{ content: FileContent | null; defaultBranch: string; viewMode: 'latest' | 'branch'; loading: boolean }>()
const emit = defineEmits<{ back: []; history: []; headings: [items: DocHeading[]]; activeHeading: [id: string] }>()
const showCopyPath = ref(false)
const copied = ref(false)
const canAsk = computed(() => Boolean(props.content && props.content.file_size <= 1024 * 1024))
function askWithCurrentFile() {
  if (!props.content || !canAsk.value) return
  window.location.assign(`/ai/?${new URLSearchParams({ repo: String(props.content.repo_id), version: String(props.content.version_id), scope: 'file', question: `基于当前文件 ${props.content.file_path} 回答：` })}`)
}
async function copyPath() {
  if (!props.content) return
  try { await navigator.clipboard.writeText(props.content.file_path); copied.value = true }
  catch { showCopyPath.value = true }
}
</script>

<template>
  <article class="doc-reader">
    <button class="btn ghost sm back-to-files" type="button" @click="emit('back')"><ChevronLeft :size="15" />文件列表</button>
    <template v-if="content">
      <header class="doc-head">
        <div class="doc-path">{{ content.file_path }}</div>
        <div class="doc-title-row">
          <h3>{{ content.title || content.file_path }}</h3>
          <div class="doc-actions">
            <button class="btn" type="button" :disabled="!canAsk" @click="askWithCurrentFile"><Bot :size="15" />基于本文提问</button>
            <a class="btn icon" :href="downloadURL(content.repo_id, content.version_id)" aria-label="下载" title="下载"><Download :size="16" /></a>
            <DropdownMenu>
              <button type="button" role="menuitem" @click="emit('history')"><History :size="15" />文件历史</button>
              <a v-if="content.previewable && isHTMLContent(content)" role="menuitem" :href="htmlPreviewURL(content.repo_id, content.version_id)" target="_blank" rel="noreferrer"><ExternalLink :size="15" />独立窗口打开</a>
              <button type="button" role="menuitem" @click="copyPath"><Copy :size="15" />{{ copied ? '已复制文件路径' : '复制文件路径' }}</button>
            </DropdownMenu>
          </div>
        </div>
        <div class="meta-line"><BranchTag :label="content.branch" /><code :title="content.source_commit_sha">{{ shortSha(content.source_commit_sha) }}</code><time>{{ formatTime(content.last_commit_time) }}</time><span>{{ formatBytes(content.file_size) }}</span>
          <span v-if="viewMode === 'latest' && content.branch !== defaultBranch" class="selection-note">智能最新选中了 {{ content.branch }} 上的版本</span>
        </div>
        <CopyField v-if="showCopyPath" :value="content.file_path" label="文件路径" />
      </header>
      <MarkdownBody :content="content" @headings="emit('headings', $event)" @active-heading="emit('activeHeading', $event)" />
    </template>
    <EmptyState v-else :message="loading ? '正在加载文档' : '从左侧选择文档'" />
  </article>
</template>

<style scoped>
.doc-reader { min-width: 0; }
.doc-head { padding: 28px 32px 16px; border-bottom: 1px solid var(--border); display: flex; flex-direction: column; gap: 10px; }
.doc-path { font: var(--fs-sm)/1.5 var(--font-mono); color: var(--fg-subtle); overflow-wrap: anywhere; }
.doc-title-row { display: flex; align-items: flex-start; flex-wrap: wrap; gap: 12px 16px; }
.doc-title-row h3 { flex: 1 1 220px; margin: 0; font-size: var(--fs-2xl); line-height: 1.3; text-wrap: balance; overflow-wrap: anywhere; }
.doc-actions { display: flex; flex-wrap: wrap; gap: 6px; }
.meta-line { display: flex; flex-wrap: wrap; align-items: center; gap: 6px 14px; font-size: var(--fs-sm); color: var(--fg-muted); font-variant-numeric: tabular-nums; }
.meta-line code { font-family: var(--font-mono); }
.meta-line :deep(.branch-tag) { max-width: 100%; }
.selection-note { color: var(--fg-subtle); overflow-wrap: anywhere; }
.back-to-files { display: none; }
@media (max-width: 860px) { .back-to-files { display: inline-flex; margin: 12px 16px 0; } .doc-head { padding: 16px; } }
</style>
