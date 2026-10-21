import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '../api'
import { readPositiveID } from '../lib/navigation'
import type { FileContent, FileEntry, PathEvent, Repository } from '../types'
import { useRepos } from './useRepos'

export const searchResultDir = '搜索结果'

/** 文档页只拥有浏览、搜索和当前文件状态；仓库由 Shell 注入。 */
export function useDocs({ selectedRepo, branches }: Pick<ReturnType<typeof useRepos>, 'selectedRepo' | 'branches'> = useRepos()) {
  const files = ref<FileEntry[]>([])
  const fileContent = ref<FileContent | null>(null)
  const docEvents = ref<PathEvent[]>([])
  const viewMode = ref<'latest' | 'branch'>('latest')
  const selectedBranch = ref('')
  const currentDir = ref('.')
  const searchText = ref('')
  const error = ref('')
  const eventsError = ref('')
  const listLoading = ref(false)
  const contentLoading = ref(false)
  const eventsLoading = ref(false)
  const reading = ref(false)
  let listRequest = 0
  let contentRequest = 0
  let restoreRequest = 0

  const breadcrumbs = computed(() => {
    if (currentDir.value === '.' || currentDir.value === searchResultDir) return []
    const parts = currentDir.value.split('/').filter(Boolean)
    return parts.map((label, index) => ({ label, path: parts.slice(0, index + 1).join('/') }))
  })
  const canGoParent = computed(() => currentDir.value !== '.' && currentDir.value !== searchResultDir)
  // 稳定排序只将目录提前，同类条目仍保留接口顺序。
  const sortedFiles = computed(() => [...files.value].sort((a, b) => Number(b.kind === 'dir') - Number(a.kind === 'dir')))

  function clearContent() {
    contentRequest++
    fileContent.value = null
    docEvents.value = []
    eventsError.value = ''
    contentLoading.value = false
    eventsLoading.value = false
    reading.value = false
  }

  function updateURL() {
    if (!selectedRepo.value || typeof window === 'undefined') return
    const params = new URLSearchParams({ repo: String(selectedRepo.value.id), view: viewMode.value })
    if (viewMode.value === 'branch' && selectedBranch.value) params.set('branch', selectedBranch.value)
    if (currentDir.value !== searchResultDir) params.set('dir', currentDir.value)
    if (fileContent.value) params.set('version', String(fileContent.value.version_id))
    const next = `${window.location.pathname}?${params}${window.location.hash}`
    if (next !== `${window.location.pathname}${window.location.search}${window.location.hash}`) window.history.pushState(null, '', next)
  }

  async function loadFiles(dir: string, syncURL = true) {
    if (!selectedRepo.value) return
    if (syncURL) restoreRequest++
    const request = ++listRequest
    const repoID = selectedRepo.value.id
    clearContent()
    files.value = []
    currentDir.value = dir
    searchText.value = ''
    error.value = ''
    listLoading.value = true
    // 立即记录用户选择，失败时也允许前进/后退恢复到该目录。
    if (syncURL) updateURL()
    try {
      const params: Record<string, string> = { view: viewMode.value, dir }
      if (viewMode.value === 'branch') params.branch = selectedBranch.value
      const response = await api.files(repoID, params)
      if (request === listRequest && repoID === selectedRepo.value?.id) files.value = response.items
    } catch (err) {
      if (request === listRequest) error.value = errorMessage(err)
    } finally {
      if (request === listRequest) listLoading.value = false
    }
  }

  async function runSearch() {
    const query = searchText.value.trim()
    if (!query) return loadFiles(currentDir.value === searchResultDir ? repoEntryDir(selectedRepo.value) : currentDir.value)
    if (!selectedRepo.value) return
    restoreRequest++
    const request = ++listRequest
    const repoID = selectedRepo.value.id
    clearContent()
    files.value = []
    listLoading.value = true
    error.value = ''
    try {
      const response = await api.search(repoID, query)
      if (request !== listRequest || repoID !== selectedRepo.value?.id) return
      files.value = response.items
      currentDir.value = searchResultDir
      updateURL()
    } catch (err) {
      if (request === listRequest) error.value = errorMessage(err)
    } finally {
      if (request === listRequest) listLoading.value = false
    }
  }

  async function openVersion(versionID: number, syncURL = true) {
    if (!selectedRepo.value) return
    if (syncURL) restoreRequest++
    const request = ++contentRequest
    const repoID = selectedRepo.value.id
    fileContent.value = null
    docEvents.value = []
    eventsError.value = ''
    eventsLoading.value = false
    error.value = ''
    contentLoading.value = true
    reading.value = true
    try {
      const content = await api.content(repoID, versionID)
      if (request !== contentRequest || repoID !== selectedRepo.value?.id) return
      fileContent.value = content
      if (syncURL) updateURL()
      // 路径事件与正文分开报错，事件接口失败不能阻止已加载文档阅读。
      eventsLoading.value = true
      try {
        const history = await api.docHistory(repoID, content.document_id)
        if (request === contentRequest && repoID === selectedRepo.value?.id) docEvents.value = Array.isArray(history.events) ? history.events : []
      } catch (err) {
        if (request === contentRequest) eventsError.value = errorMessage(err)
      } finally {
        if (request === contentRequest) eventsLoading.value = false
      }
    } catch (err) {
      if (request === contentRequest) error.value = errorMessage(err)
    } finally {
      if (request === contentRequest) contentLoading.value = false
    }
  }

  function openFile(entry: FileEntry) {
    return entry.kind === 'dir' ? loadFiles(entry.path) : entry.version_id ? openVersion(entry.version_id) : undefined
  }
  function loadBranchFiles() { return loadFiles(repoEntryDir(selectedRepo.value)) }
  function switchView(mode: 'latest' | 'branch') { viewMode.value = mode; return loadBranchFiles() }
  function goParent() { if (canGoParent.value) return loadFiles(parentDir(currentDir.value)) }
  function backToFiles() { restoreRequest++; clearContent(); updateURL() }

  async function restoreURL() {
    const request = ++restoreRequest
    listRequest++
    clearContent()
    files.value = []
    error.value = ''
    listLoading.value = false
    if (!selectedRepo.value || typeof window === 'undefined') return
    const params = new URLSearchParams(window.location.search)
    const repoID = readPositiveID(params, 'repo')
    // 跨仓库 popstate 先由 Shell 准备分支，随后 selectedRepo 的 watch 再恢复本页。
    if (repoID && repoID !== selectedRepo.value.id) return
    viewMode.value = params.get('view') === 'branch' ? 'branch' : 'latest'
    const branch = params.get('branch') || selectedRepo.value.default_branch
    selectedBranch.value = branches.value.find((item) => item.ref_name === branch)?.ref_name
      || branches.value.find((item) => item.ref_name === selectedRepo.value?.default_branch)?.ref_name
      || branches.value[0]?.ref_name || ''
    const dir = params.has('dir') ? params.get('dir') || '.' : repoEntryDir(selectedRepo.value)
    const versionID = readPositiveID(params, 'version')
    await loadFiles(dir, false)
    if (request !== restoreRequest) return
    if (versionID) await openVersion(versionID, false)
  }

  watch(selectedRepo, () => { void restoreURL() }, { immediate: true })
  onMounted(() => window.addEventListener('popstate', restoreURL))
  onBeforeUnmount(() => {
    listRequest++; contentRequest++; restoreRequest++
    window.removeEventListener('popstate', restoreURL)
  })
  return { files: sortedFiles, fileContent, docEvents, viewMode, selectedBranch, currentDir, searchText,
    error, eventsError, listLoading, contentLoading, eventsLoading, reading, breadcrumbs, canGoParent,
    loadFiles, openFile, openVersion, loadBranchFiles, switchView, goParent, runSearch, backToFiles }
}

export function parentDir(path: string) {
  const parts = path.split('/').filter(Boolean)
  parts.pop()
  return parts.length ? parts.join('/') : '.'
}

export function repoEntryDir(repo: Repository | null | undefined) {
  const enabledDirs = (repo?.scan_paths || []).filter((scanPath) => scanPath.enabled).map((scanPath) => normalizeBrowserDir(scanPath.path))
  return enabledDirs.length === 1 && enabledDirs[0] !== '.' ? enabledDirs[0] : '.'
}

function normalizeBrowserDir(value: string) {
  const cleaned = value.trim().replace(/\\/g, '/').replace(/^\/+/, '').replace(/\/+$/, '')
  return cleaned || '.'
}
function errorMessage(err: unknown) { return err instanceof Error ? err.message : String(err) }
