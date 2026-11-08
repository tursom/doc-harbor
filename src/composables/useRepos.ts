import { computed, inject, onBeforeUnmount, onMounted, provide, ref, type InjectionKey } from 'vue'
import { api } from '../api'
import { pageURL, readPositiveID, type AppTab } from '../lib/navigation'
import type { Repository, RepoRef } from '../types'

export const isGlobalPage = (tab: AppTab) => ['ai', 'ai-config', 'ai-diagnostics', 'settings'].includes(tab)

/** 每个静态页面只有一个 Shell；用 provide 隔离 SSR 请求，同时让页头和视图共享仓库。 */
function createRepos(initialTab: AppTab) {
  const tab = ref(initialTab)
  const repos = ref<Repository[]>([])
  const selectedRepo = ref<Repository | null>(null)
  const branches = ref<RepoRef[]>([])
  const busy = ref(false)
  const error = ref('')
  const loaded = ref(false)
  const submitting = ref(false)
  const cancelling = ref(false)
  const cancelRequested = ref(false)
  // 只在一次任务进入终态、分支已刷新后通知文档页，进度变化不会触发阅读状态恢复。
  const scanRevision = ref(0)
  let selectionRequest = 0
  let stateRequest = 0
  let disposed = false
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let loading: Promise<void> | null = null
  const isGlobalTab = computed(() => isGlobalPage(tab.value))
  const scanRunning = computed(() => selectedRepo.value?.latest_scan?.status === 'running')

  async function selectRepo(repo: Repository | null) {
    const request = ++selectionRequest
    stateRequest++
    // 先准备分支，再发布仓库，避免视图恢复分支深链接时读到旧仓库的列表。
    const refs = repo && !isGlobalTab.value ? await api.branches(repo.id) : { items: [] }
    if (disposed || request !== selectionRequest) return
    stateRequest++
    branches.value = refs.items || []
    if (repo && repo.id === selectedRepo.value?.id) Object.assign(selectedRepo.value, repo)
    else selectedRepo.value = repo
    cancelRequested.value = false
    submitting.value = false
    cancelling.value = false
    busy.value = false
  }

  function loadAll(): Promise<void> {
    // 子视图恢复文件范围和 Shell 首次挂载会同时请求仓库，共享进行中的读取即可。
    if (!loading) loading = fetchRepos().finally(() => { loading = null })
    return loading
  }
  async function fetchRepos() {
    error.value = ''
    try {
      const response = await api.repos()
      if (disposed) return
      repos.value = response.items || []
      const id = readPositiveID(new URLSearchParams(window.location.search), 'repo')
      const requested = repos.value.find((repo) => repo.id === id)
      const current = repos.value.find((repo) => repo.id === selectedRepo.value?.id)
      await selectRepo(requested || current || (isGlobalTab.value ? null : repos.value[0]) || null)
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err)
    } finally {
      loaded.value = true
    }
  }

  function tabHref(nextTab: AppTab) {
    return pageURL(nextTab, new URLSearchParams(), { repo: selectedRepo.value?.id })
  }
  function repoHref(repo: Repository) {
    const current = typeof window === 'undefined' ? new URLSearchParams() : new URLSearchParams(window.location.search)
    // 文档内换仓库延续按分支模式，但目录、版本和具体分支须按新仓库重新选择。
    return pageURL(isGlobalTab.value ? 'docs' : tab.value, new URLSearchParams(), {
      repo: repo.id,
      view: tab.value === 'docs' && current.get('view') === 'branch' ? 'branch' : undefined
    })
  }
  /** 任务提交只等 202，不等待扫描完成；后续状态与统计由轮询持续读取。 */
  async function scanSelected(force = false) {
    if (!selectedRepo.value || submitting.value || scanRunning.value) return
    const repo = selectedRepo.value
    const selection = selectionRequest
    stateRequest++ // 作废提交前发出的轮询，避免旧 latest_scan 抹掉刚返回的 running。
    submitting.value = true
    busy.value = true
    error.value = ''
    try {
      const run = await api.scanRepo(repo.id, force)
      if (disposed || selection !== selectionRequest) return
      const previous = repo.latest_scan
      repo.latest_scan = run
      cancelRequested.value = false
      if (run.status !== 'running' && (run.id !== previous?.id || previous?.status === 'running')) await refreshIndex(repo, selection)
    } catch (err) {
      if (!disposed && selection === selectionRequest) error.value = err instanceof Error ? err.message : String(err)
    } finally {
      if (!disposed && selection === selectionRequest) { busy.value = false; submitting.value = false }
    }
  }
  async function cancelScan() {
    if (!selectedRepo.value || !scanRunning.value || cancelling.value || cancelRequested.value) return
    const repo = selectedRepo.value
    const selection = selectionRequest
    const runID = repo.latest_scan?.id
    stateRequest++
    cancelling.value = true
    error.value = ''
    try {
      const run = await api.cancelScan(repo.id)
      if (disposed || selection !== selectionRequest || repo.latest_scan?.id !== runID) return
      repo.latest_scan = run
      // 接口确认收到取消请求后，任务仍可能在写入或清理；不提前伪造 cancelled。
      cancelRequested.value = run.status === 'running'
      if (run.status !== 'running') await refreshIndex(repo, selection)
    } catch (err) {
      if (!disposed && selection === selectionRequest) error.value = err instanceof Error ? err.message : String(err)
    } finally {
      if (!disposed && selection === selectionRequest) cancelling.value = false
    }
  }

  async function refreshIndex(repo: Repository, selection: number) {
    const refs = await api.branches(repo.id)
    if (disposed || selection !== selectionRequest || repo.id !== selectedRepo.value?.id) return
    branches.value = refs.items || []
    scanRevision.value++
  }

  /** 串行定时读取也能发现定时/Webhook 任务；页面重新打开时直接以持久化记录为准。 */
  async function pollSelected() {
    const repo = selectedRepo.value
    const selection = selectionRequest
    const request = ++stateRequest
    try {
      if (!repo || submitting.value || cancelling.value) return
      const response = await api.repos()
      if (disposed || selection !== selectionRequest || request !== stateRequest || repo !== selectedRepo.value) return
      const current = response.items.find((item) => item.id === repo.id)
      if (!current) return
      const previous = repo.latest_scan
      const next = current.latest_scan
      const finished = next && next.status !== 'running' && (next.id !== previous?.id || previous?.status === 'running')
      repos.value = response.items.map((item) => item.id === repo.id ? repo : item)
      // 保留对象身份：useDocs/useHistory 的仓库 watch 不应因每次进度更新而重新打开文档。
      Object.assign(repo, current)
      if (!next || next.id !== previous?.id || next.status !== 'running') cancelRequested.value = false
      if (finished) await refreshIndex(repo, selection)
    } catch (err) {
      if (!disposed && selection === selectionRequest && request === stateRequest) error.value = err instanceof Error ? err.message : String(err)
    } finally {
      if (!disposed) pollTimer = setTimeout(() => { void pollSelected() }, 1500)
    }
  }
  async function restoreRepo() {
    const id = readPositiveID(new URLSearchParams(window.location.search), 'repo')
    if (id === selectedRepo.value?.id) return
    const repo = repos.value.find((item) => item.id === id)
    if (repo) {
      try { await selectRepo(repo) } catch (err) { error.value = String(err) }
    }
  }
  onMounted(() => {
    void loadAll()
    pollTimer = setTimeout(() => { void pollSelected() }, 1500)
    window.addEventListener('popstate', restoreRepo)
  })
  onBeforeUnmount(() => {
    disposed = true
    selectionRequest++; stateRequest++
    clearTimeout(pollTimer)
    window.removeEventListener('popstate', restoreRepo)
  })
  return { tab, repos, selectedRepo, branches, busy, error, loaded, isGlobalTab, loadAll, selectRepo, tabHref, repoHref,
    scanSelected, scanRunning, submitting, cancelling, cancelRequested, cancelScan, scanRevision }
}

type RepoContext = ReturnType<typeof createRepos>
const repoKey: InjectionKey<RepoContext> = Symbol('repositories')
export function provideRepos(tab: AppTab) {
  const context = createRepos(tab)
  provide(repoKey, context)
  return context
}
export function useRepos() {
  const context = inject(repoKey)
  if (!context) throw new Error('useRepos 必须在 AppShell 内调用')
  return context
}
