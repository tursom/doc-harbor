<template>
  <main class="html-viewer-shell">
    <div v-if="error" class="html-viewer-state">
      <FileDown :size="30" />
      <h1>HTML 预览不可用</h1>
      <p>{{ error }}</p>
      <a class="command" :href="backURL">返回文档</a>
    </div>
    <iframe
      v-else-if="content && srcdoc"
      class="html-viewer-frame"
      sandbox="allow-scripts"
      :srcdoc="srcdoc"
      :title="content.title || content.file_path"
    ></iframe>
    <div v-else class="html-viewer-state">
      <BookOpen :size="30" />
      <p>正在加载 HTML 预览</p>
    </div>
  </main>
</template>

<script setup lang="ts">
import { BookOpen, FileDown } from 'lucide-vue-next'
import { computed, onMounted, ref } from 'vue'
import { api } from '../api'
import type { FileContent } from '../types'
import { isHTMLContent } from '../lib/content-type'
import { loadHTMLPreviewResource } from '../lib/html-preview-resource'

const content = ref<FileContent | null>(null)
const srcdoc = ref('')
const error = ref('')
let buildRequest = 0

const backURL = computed(() => {
  if (content.value) {
    return `/?${new URLSearchParams({
      repo: String(content.value.repo_id),
      version: String(content.value.version_id),
      view: 'latest',
      dir: dirNameFromPath(content.value.file_path)
    })}`
  }
  const params = new URLSearchParams(typeof window !== 'undefined' ? window.location.search : '')
  const repoID = Number(params.get('repo') || 0)
  const versionID = Number(params.get('version') || 0)
  return repoID > 0 && versionID > 0
    ? `/?${new URLSearchParams({ repo: String(repoID), version: String(versionID), view: 'latest' })}`
    : '/'
})

onMounted(load)

async function load() {
  const params = new URLSearchParams(window.location.search)
  const repoID = Number(params.get('repo') || 0)
  const versionID = Number(params.get('version') || 0)
  if (repoID <= 0 || versionID <= 0) {
    error.value = '缺少 repo 或 version 参数'
    return
  }
  try {
    const result = await api.content(repoID, versionID)
    content.value = result
    if (!result.previewable) throw new Error('当前文件不可预览')
    if (!isHTMLContent(result)) throw new Error('当前文件不是 HTML 文件')
    if (!result.content) throw new Error('HTML 内容为空')
    await rebuild(result)
  } catch (err) {
    error.value = err instanceof Error ? err.message : String(err)
  }
}

async function rebuild(value: FileContent) {
  const requestID = ++buildRequest
  const { buildHTMLPreviewSrcdoc } = await import('../html-preview')
  const result = await buildHTMLPreviewSrcdoc(value, loadHTMLPreviewResource)
  if (requestID === buildRequest) srcdoc.value = result
}

function dirNameFromPath(filePath: string) {
  const parts = filePath.split('/').filter(Boolean)
  parts.pop()
  return parts.length ? parts.join('/') : '.'
}
</script>
