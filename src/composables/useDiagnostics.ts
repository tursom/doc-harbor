import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { api } from '../api'
import { statusLabel } from '../lib/labels'
import type { AIContractCoverageItem, AIDiagnosticsRunDetailResponse, AIDiagnosticsRunSummary, AIDiagnosticsStep } from '../types'

export function useDiagnostics() {
  const token = ref('')
  const status = ref('')
  const query = ref('')
  const runs = ref<AIDiagnosticsRunSummary[]>([])
  const detail = ref<AIDiagnosticsRunDetailResponse | null>(null)
  const loading = ref(false)
  const detailLoading = ref(false)
  const error = ref('')
  const selectedID = ref<number | null>(null)
  const nextCursor = ref('')
  const hasRead = ref(false)
  let listRequest = 0
  let detailRequest = 0
  let lastFilter: Record<string, string> = {}
  const workflow = computed(() => detail.value?.agent_workflow)
  const taskFrame = computed(() => workflow.value?.task_frame || detail.value?.task_frame)
  const contract = computed(() => workflow.value?.evidence_contract || detail.value?.evidence_contract)
  const coverage = computed(() => workflow.value?.contract_coverage || detail.value?.contract_coverage)
  const coverageItems = computed<AIContractCoverageItem[]>(() => {
    if (coverage.value?.items?.length) return coverage.value.items
    const report = coverage.value
    return Object.entries(report?.coverage || {}).map(([key, value]) => ({ key, status: value, requirement: report?.missing_required?.includes(key) ? 'required' : 'recommended', evidence_ids: [], reason: report?.details?.[key] || '', missing_detail: report?.details?.[key] || '', confidence: 0 }))
  })
  const emptyMessage = computed(() => !token.value.trim() || !hasRead.value ? '输入诊断 Token 后读取 Run' : '没有符合条件的 Run')

  function invalidate() {
    // Token 改变后使旧响应失效，避免把前一个 Token 的详情误显示为当前结果。
    listRequest++; detailRequest++
    runs.value = []; detail.value = null; selectedID.value = null; nextCursor.value = ''
    loading.value = false; detailLoading.value = false; hasRead.value = false; error.value = ''
  }
  watch(token, invalidate, { flush: 'sync' })
  onBeforeUnmount(() => { listRequest++; detailRequest++ })

  async function openRun(id: number) {
    if (!token.value.trim()) return
    const request = ++detailRequest
    selectedID.value = id
    detail.value = null
    detailLoading.value = true
    error.value = ''
    try {
      const response = await api.aiDiagnosticsRun(token.value.trim(), id)
      if (request === detailRequest) detail.value = response
    } catch (err) {
      if (request === detailRequest) error.value = err instanceof Error ? err.message : String(err)
    } finally { if (request === detailRequest) detailLoading.value = false }
  }

  async function loadRuns(more = false) {
    if (!token.value.trim() || loading.value || (more && !nextCursor.value)) return
    const request = ++listRequest
    if (!more) {
      detailRequest++; detail.value = null; selectedID.value = null; detailLoading.value = false
      runs.value = []; nextCursor.value = ''; hasRead.value = false
      lastFilter = { limit: '50', ...(status.value ? { status: status.value } : {}), ...(query.value.trim() ? { q: query.value.trim() } : {}) }
    }
    loading.value = true
    error.value = ''
    try {
      const response = await api.aiDiagnosticsRuns(token.value.trim(), { ...lastFilter, ...(more ? { cursor: nextCursor.value } : {}) })
      if (request !== listRequest) return
      runs.value = more ? [...runs.value, ...(response.items || []).filter(item => !runs.value.some(existing => existing.run.id === item.run.id))] : response.items || []
      nextCursor.value = response.next_cursor || ''
      hasRead.value = true
      if (!more && runs.value[0]) await openRun(runs.value[0].run.id)
    } catch (err) {
      if (request === listRequest) error.value = err instanceof Error ? err.message : String(err)
    } finally { if (request === listRequest) loading.value = false }
  }

  return { token, status, query, runs, detail, loading, detailLoading, error, selectedID, nextCursor, workflow, taskFrame, contract, coverage, coverageItems, emptyMessage, loadRuns, openRun }
}

export function listText(values?: string[]) { return values?.filter(Boolean).join('、') || '—' }
export function coverageDeltaText(delta?: Record<string, string>) {
  return Object.entries(delta || {}).map(([key, status]) => `${key}：${statusLabel(status)}`).join(' · ') || '无变化'
}
export function stepJSON(step: AIDiagnosticsStep) { return JSON.stringify(step.summary || { input: step.input, output: step.output }, null, 2) }
export function seconds(milliseconds: number) { return `${(milliseconds / 1000).toFixed(1)} 秒` }
