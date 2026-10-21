import { computed, ref } from 'vue'
import { APIRequestError, api } from '../api'
import type { Repository } from '../types'

export function splitList(value: string) {
  return value.split(/[\n,]/).map((part) => part.trim()).filter(Boolean)
}

/** 新增和设置页共用字段转换，保留已有仓库的非界面字段。 */
export function useRepoForm() {
  const form = ref<Partial<Repository>>({})
  const trackedBranchesText = ref('*')
  const latestIncludeText = ref('*')
  const latestExcludeText = ref('')
  const branchPriorityText = ref('')
  const scanPathsText = ref('doc, docs')
  const fieldErrors = ref<Record<string, string>>({})
  const error = ref('')
  const saving = ref(false)
  const editingID = ref<number | null>(null)
  const baseline = ref('')
  const snapshot = () => JSON.stringify([form.value, trackedBranchesText.value, latestIncludeText.value, latestExcludeText.value, branchPriorityText.value, scanPathsText.value])
  const dirty = computed(() => snapshot() !== baseline.value)
  const autoSync = computed({
    get: () => form.value.sync_interval_seconds !== -1,
    set: (enabled: boolean) => { form.value.sync_interval_seconds = enabled ? 3600 : -1 }
  })

  function reset(repo?: Repository | null) {
    editingID.value = repo?.id || null
    form.value = repo ? { ...repo } : {
      name: '', slug: '', repo_url: '', default_branch: 'main',
      sync_interval_seconds: 3600, max_file_size_bytes: 2097152
    }
    trackedBranchesText.value = repo?.tracked_branches?.join(', ') ?? '*'
    latestIncludeText.value = repo?.latest_include_branches?.join(', ') ?? '*'
    latestExcludeText.value = repo?.latest_exclude_branches?.join(', ') ?? 'archive/*, tmp/*, dependabot/*, backup/*'
    branchPriorityText.value = repo?.branch_priority?.join(', ') ?? 'main, master, release/*, develop, feature/*'
    scanPathsText.value = repo ? repo.scan_paths.map((item) => item.path).join('\n') : 'doc, docs'
    fieldErrors.value = {}
    error.value = ''
    baseline.value = snapshot()
  }

  function validate() {
    const errors: Record<string, string> = {}
    if (!form.value.name?.trim()) errors.name = '请输入名称'
    if (!form.value.repo_url?.trim()) errors.repo_url = '请输入 Git URL'
    if (!form.value.default_branch?.trim()) errors.default_branch = '请输入默认分支'
    if (autoSync.value && (!Number.isFinite(Number(form.value.sync_interval_seconds)) || Number(form.value.sync_interval_seconds) < 60)) {
      errors.sync_interval_seconds = '拉取间隔至少为 60 秒'
    }
    if (!splitList(scanPathsText.value).length) errors.scan_paths = '请填写至少一个扫描目录'
    fieldErrors.value = errors
    return !Object.keys(errors).length
  }

  async function save(): Promise<Repository | null> {
    if (saving.value || !validate()) return null
    saving.value = true
    error.value = ''
    try {
      const payload: Partial<Repository> = {
        ...form.value,
        name: form.value.name!.trim(), repo_url: form.value.repo_url!.trim(),
        tracked_branches: splitList(trackedBranchesText.value),
        latest_include_branches: splitList(latestIncludeText.value),
        latest_exclude_branches: splitList(latestExcludeText.value),
        branch_priority: splitList(branchPriorityText.value),
        enabled: form.value.enabled ?? true,
        scan_paths: splitList(scanPathsText.value).map((path) => {
          // 设置页只编辑目录名；已有目录的 glob 配置仍交给后端原有逻辑处理。
          const existing = form.value.scan_paths?.find((item) => item.path === path)
          return existing || { path, include_globs: [], exclude_globs: [], enabled: true }
        })
      }
      const repo = editingID.value ? await api.updateRepo(editingID.value, payload) : await api.createRepo(payload)
      reset(repo)
      return repo
    } catch (err) {
      if (err instanceof APIRequestError) fieldErrors.value = err.fields
      error.value = err instanceof Error ? err.message : String(err)
      return null
    } finally { saving.value = false }
  }
  reset()
  return { form, trackedBranchesText, latestIncludeText, latestExcludeText, branchPriorityText, scanPathsText, fieldErrors, error, saving, dirty, autoSync, reset, save }
}
