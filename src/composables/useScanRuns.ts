import { onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../api'
import type { ScanRun } from '../types'
import { useRepos } from './useRepos'

/** 扫描记录随 latest_scan 轮询结果刷新，保留展开行，避免每次进度更新都闪烁。 */
export function useScanRuns({ selectedRepo }: Pick<ReturnType<typeof useRepos>, 'selectedRepo'> = useRepos()) {
  const scanRuns = ref<ScanRun[]>([])
  const expanded = ref<Set<number>>(new Set())
  const loading = ref(false)
  const error = ref('')
  let requestID = 0
  async function loadScanRuns(reset = true) {
    const request = ++requestID
    if (reset) { scanRuns.value = []; expanded.value = new Set() }
    error.value = ''
    loading.value = false
    if (!selectedRepo.value) return
    const repoID = selectedRepo.value.id
    loading.value = reset
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
  watch(() => selectedRepo.value?.latest_scan, () => { void loadScanRuns(false) })
  onBeforeUnmount(() => { requestID++ })
  return { scanRuns, expanded, loading, error, loadScanRuns, canExpand, toggleRun }
}
