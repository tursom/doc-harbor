import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api } from '../api'
import { buildGraphRows, graphLaneGap, graphLanePadding } from '../lib/git-graph'
import { readPositiveID } from '../lib/navigation'
import type { CommitDetail, CommitSummary } from '../types'
import { useRepos } from './useRepos'

/** 提交页不加载文件列表或任何文档渲染依赖。 */
export function useHistory({ selectedRepo, branches }: Pick<ReturnType<typeof useRepos>, 'selectedRepo' | 'branches'> = useRepos()) {
  const commits = ref<CommitSummary[]>([])
  const selectedCommit = ref<CommitDetail | null>(null)
  const historyBranch = ref('')
  const loading = ref(false)
  const detailLoading = ref(false)
  const error = ref('')
  let listRequest = 0
  let detailRequest = 0
  const graphRows = computed(() => buildGraphRows(commits.value))
  const graphWidth = computed(() => {
    const maxLane = graphRows.value.reduce((max, row) => Math.max(max, row.maxLane), 0)
    return Math.max(46, graphLanePadding * 2 + maxLane * graphLaneGap)
  })

  function updateURL() {
    if (!selectedRepo.value) return
    const params = new URLSearchParams({ repo: String(selectedRepo.value.id) })
    if (historyBranch.value) params.set('branch', historyBranch.value)
    if (selectedCommit.value) params.set('commit', selectedCommit.value.sha)
    const next = `${window.location.pathname}?${params}${window.location.hash}`
    if (next !== `${window.location.pathname}${window.location.search}${window.location.hash}`) window.history.pushState(null, '', next)
  }

  async function openCommit(sha: string, syncURL = true) {
    if (!selectedRepo.value) return
    const request = ++detailRequest
    const repoID = selectedRepo.value.id
    detailLoading.value = true
    selectedCommit.value = null
    error.value = ''
    try {
      const detail = await api.commit(repoID, sha)
      if (request !== detailRequest || repoID !== selectedRepo.value?.id) return
      selectedCommit.value = detail
      if (syncURL) updateURL()
    } catch (err) {
      if (request === detailRequest) error.value = err instanceof Error ? err.message : String(err)
    } finally {
      if (request === detailRequest) detailLoading.value = false
    }
  }

  async function loadHistory(commitSHA = '', syncURL = false) {
    const request = ++listRequest
    detailRequest++
    commits.value = []
    selectedCommit.value = null
    detailLoading.value = false
    error.value = ''
    loading.value = false
    if (!selectedRepo.value) return
    const repoID = selectedRepo.value.id
    loading.value = true
    try {
      const response = await api.history(repoID, historyBranch.value, 120)
      if (request !== listRequest || repoID !== selectedRepo.value?.id) return
      commits.value = response.items
      // 深链接可指向不在最近 120 条内的提交，不能仅在列表中查找。
      const target = commitSHA || commits.value[0]?.sha
      if (target) await openCommit(target, false)
      if (request === listRequest && syncURL) updateURL()
    } catch (err) {
      if (request === listRequest) error.value = err instanceof Error ? err.message : String(err)
    } finally {
      if (request === listRequest) loading.value = false
    }
  }

  async function restoreURL() {
    if (typeof window === 'undefined') return
    const params = new URLSearchParams(window.location.search)
    const repoID = readPositiveID(params, 'repo')
    if (selectedRepo.value && repoID && repoID !== selectedRepo.value.id) {
      listRequest++; detailRequest++
      commits.value = []; selectedCommit.value = null
      return
    }
    const branch = params.get('branch') || ''
    historyBranch.value = branches.value.some((item) => item.ref_name === branch) ? branch : ''
    await loadHistory(params.get('commit') || '')
  }

  watch(selectedRepo, () => { void restoreURL() }, { immediate: true })
  onMounted(() => window.addEventListener('popstate', restoreURL))
  onBeforeUnmount(() => {
    listRequest++; detailRequest++
    window.removeEventListener('popstate', restoreURL)
  })
  return { commits, selectedCommit, historyBranch, loading, detailLoading, error, graphRows, graphWidth,
    openCommit, loadHistory, changeHistoryBranch: () => loadHistory('', true) }
}
