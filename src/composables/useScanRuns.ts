import { onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../api'
import type { ScanRun } from '../types'
import { useRepos } from './useRepos'

/** 扫描页仅随共享仓库变更刷新，不保存其他页面的状态。 */
export function useScanRuns({ selectedRepo }: Pick<ReturnType<typeof useRepos>, 'selectedRepo'> = useRepos()) {
  const scanRuns = ref<ScanRun[]>([])
  const expanded = ref<Set<number>>(new Set())
  const loading = ref(false)
  const error = ref('')
  let requestID = 0
  async function loadScanRuns() {
    const request = ++requestID
    scanRuns.value = []
    expanded.value = new Set()
    error.value = ''
    loading.value = false
    if (!selectedRepo.value) return
    const repoID = selectedRepo.value.id
    loading.value = true
    try {
      const response = await api.scanRuns(repoID)
      if (request === requestID && repoID === selectedRepo.value?.id) scanRuns.value = response.items
    } catch (err) {
      if (request === requestID) error.value = err instanceof Error ? err.message : String(err)
    } finally {
      if (request === requestID) loading.value = false
    }
  }
  function canExpand(run: ScanRun) { return run.error_count > 0 || Boolean(run.error_message) }
  function toggleRun(run: ScanRun) {
    if (!canExpand(run)) return
    const next = new Set(expanded.value)
    if (next.has(run.id)) next.delete(run.id)
    else next.add(run.id)
    expanded.value = next
  }
  watch(selectedRepo, () => { void loadScanRuns() }, { immediate: true })
  onBeforeUnmount(() => { requestID++ })
  return { scanRuns, expanded, loading, error, loadScanRuns, canExpand, toggleRun }
}
