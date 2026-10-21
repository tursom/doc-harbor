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
  let selectionRequest = 0
  let loading: Promise<void> | null = null
  const isGlobalTab = computed(() => isGlobalPage(tab.value))

  async function selectRepo(repo: Repository | null) {
    const request = ++selectionRequest
    // 先准备分支，再发布仓库，避免视图恢复分支深链接时读到旧仓库的列表。
    const refs = repo && !isGlobalTab.value ? await api.branches(repo.id) : { items: [] }
    if (request !== selectionRequest) return
    branches.value = refs.items || []
    selectedRepo.value = repo
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
  async function scanSelected() {
    if (!selectedRepo.value || busy.value) return
    busy.value = true
    error.value = ''
    try {
      await api.scanRepo(selectedRepo.value.id)
      await loadAll()
    } catch (err) {
      error.value = err instanceof Error ? err.message : String(err)
    } finally {
      busy.value = false
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
    window.addEventListener('popstate', restoreRepo)
  })
  onBeforeUnmount(() => window.removeEventListener('popstate', restoreRepo))
  return { tab, repos, selectedRepo, branches, busy, error, loaded, isGlobalTab, loadAll, selectRepo, tabHref, repoHref, scanSelected }
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
