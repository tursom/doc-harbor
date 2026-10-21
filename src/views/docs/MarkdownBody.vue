<script setup lang="ts">
import { ref, toRef, watch } from 'vue'
import EmptyState from '../../components/ui/EmptyState.vue'
import Notice from '../../components/ui/Notice.vue'
import { isHTMLContent } from '../../lib/content-type'
import { useMarkdownPreview, type DocHeading } from '../../composables/useMarkdownPreview'
import { useTheme } from '../../composables/useTheme'
import type { FileContent } from '../../types'

const props = defineProps<{ content: FileContent }>()
const emit = defineEmits<{ headings: [items: DocHeading[]]; activeHeading: [id: string] }>()
const root = ref<HTMLElement | null>(null)
const { isDark } = useTheme()
const { renderedHtml, markdownLoading, markdownError, htmlPreviewSrcdoc, htmlPreviewLoading, htmlPreviewError, headings, activeHeading } = useMarkdownPreview(toRef(props, 'content'), root, isDark)
watch(headings, (value) => emit('headings', value), { immediate: true })
watch(activeHeading, (value) => emit('activeHeading', value), { immediate: true })
</script>

<template>
  <div ref="root" class="preview-body">
    <template v-if="content.previewable && isHTMLContent(content)">
      <iframe v-if="htmlPreviewSrcdoc" class="html-preview-frame" sandbox="allow-scripts" :srcdoc="htmlPreviewSrcdoc" :title="content.title || content.file_path"></iframe>
      <EmptyState v-else-if="htmlPreviewLoading" message="正在加载 HTML 预览" />
      <div v-else-if="htmlPreviewError" class="preview-notice"><EmptyState message="HTML 预览不可用" /><Notice variant="danger">{{ htmlPreviewError }}</Notice></div>
      <EmptyState v-else message="当前文件不支持预览" />
    </template>
    <template v-else>
      <div v-if="renderedHtml" class="markdown-body" v-html="renderedHtml"></div>
      <EmptyState v-else-if="markdownLoading" message="正在加载文档预览" />
      <div v-else-if="markdownError" class="preview-notice"><EmptyState message="文档预览不可用" /><Notice variant="danger">{{ markdownError }}</Notice></div>
      <div v-else><EmptyState message="当前文件不支持预览" /><p class="preview-help">支持 Markdown 和 HTML 预览，其他文件可单文件下载。</p></div>
    </template>
  </div>
</template>

<style scoped>
.preview-body { min-width: 0; }
.preview-notice { padding: 24px; }
.preview-help { color: var(--fg-muted); text-align: center; padding: 0 24px; }
.html-preview-frame { display: block; border: 0; width: 100%; min-height: 70vh; background: var(--preview-bg); color-scheme: light; }
.markdown-body { max-width: 780px; padding: 24px 32px 64px; font-size: var(--fs-lg); line-height: 1.78; overflow-wrap: anywhere; color: var(--fg); }
.markdown-body :deep(> :first-child) { margin-top: 0; }
.markdown-body :deep(h1) { font-size: var(--fs-2xl); line-height: 1.4; }
.markdown-body :deep(h2) { font-size: 19px; line-height: 1.4; margin: 32px 0 10px; padding-top: 4px; scroll-margin-top: 24px; }
.markdown-body :deep(h3) { font-size: 17px; line-height: 1.5; margin: 24px 0 8px; scroll-margin-top: 24px; }
.markdown-body :deep(p) { margin: 0 0 14px; }
.markdown-body :deep(a) { color: var(--accent-text); text-decoration: underline; text-decoration-color: color-mix(in srgb, var(--accent-text) 35%, var(--transparent)); text-underline-offset: 3px; }
.markdown-body :deep(code) { font-family: var(--font-mono); background: var(--surface-2); padding: 1px 5px; border-radius: 4px; font-size: .86em; }
.markdown-body :deep(pre) { max-width: 100%; overflow: auto; padding: 14px 16px; background: var(--surface-2); border: 1px solid var(--border); border-radius: 6px; line-height: 1.6; }
.markdown-body :deep(pre code) { padding: 0; background: none; }
.markdown-body :deep(blockquote) { border-left: 3px solid var(--border-strong); margin: 16px 0; padding-left: 16px; color: var(--fg-muted); }
.markdown-body :deep(hr) { border: 0; border-top: 1px solid var(--border); margin: 24px 0; }
.markdown-body :deep(img) { max-width: 100%; height: auto; }
.markdown-body :deep(.markdown-table-scroll) { overflow-x: auto; margin: 4px 0 18px; border: 1px solid var(--border); border-radius: 6px; }
.markdown-body :deep(table) { border-collapse: collapse; width: 100%; font-size: var(--fs-md); line-height: 1.6; }
.markdown-body :deep(th), .markdown-body :deep(td) { padding: 8px 12px; border-bottom: 1px solid var(--border); text-align: left; vertical-align: top; min-width: 80px; }
.markdown-body :deep(th) { background: var(--surface-2); font-weight: 600; white-space: nowrap; }
.markdown-body :deep(tr:last-child td) { border-bottom: 0; }
.markdown-body :deep(.mermaid-render) { max-width: 100%; overflow-x: auto; margin: 18px 0; }
.markdown-body :deep(.mermaid-render svg) { max-width: 100%; height: auto; }
.markdown-body :deep(.mermaid-error-text) { color: var(--danger); white-space: pre-wrap; font: var(--fs-sm)/1.6 var(--font-ui); }
@media (max-width: 860px) { .markdown-body { padding: 20px 16px 40px; } }
</style>
