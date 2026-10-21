import { computed, ref } from 'vue'
import { APIRequestError, api } from '../api'
import type { AIProviderTestResult, AISettingsForm, AISettingsProviderSummary, AISettingsSummary } from '../types'

const defaults = (): AISettingsForm => ({ provider_key: '', name: 'DeepSeek', base_url: 'https://api.deepseek.com', model: 'deepseek-v4-flash', api_key: '', timeout_seconds: 60, max_rpm: 60, priority: 10, cost_tier: 'medium' })
export const aiProviderPresets = [
  { key: 'deepseek', label: 'DeepSeek', name: 'DeepSeek', base_url: 'https://api.deepseek.com', model: 'deepseek-v4-flash' },
  { key: 'openai', label: 'OpenAI', name: 'OpenAI', base_url: 'https://api.openai.com/v1', model: '' },
  { key: 'siliconflow', label: '硅基流动', name: '硅基流动', base_url: 'https://api.siliconflow.cn/v1', model: '' },
  { key: 'qwen', label: '通义千问兼容接口', name: '通义千问兼容接口', base_url: 'https://dashscope.aliyuncs.com/compatible-mode/v1', model: '' },
  { key: 'custom', label: '自定义', name: '', base_url: '', model: '' }
]

export function useAISettings() {
  const settings = ref<AISettingsSummary | null>(null)
  const loading = ref(false)
  const operation = ref('')
  const providerBusy = ref('')
  const banner = ref<{ variant: 'info' | 'ok' | 'warn' | 'danger'; message: string } | null>(null)
  const drawerOpen = ref(false)
  const form = ref(defaults())
  const selectedPreset = ref('deepseek')
  const formDirty = ref(false)
  const fieldErrors = ref<Record<string, string>>({})
  const formError = ref('')
  const testResult = ref<AIProviderTestResult | null>(null)
  const deleting = ref<AISettingsProviderSummary | null>(null)
  const replacementKey = ref('')
  const busy = computed(() => loading.value || Boolean(operation.value))
  // 密钥状态必须跟随抽屉正在编辑的供应商，不能借用默认供应商的密钥状态。
  const editedProvider = computed(() => settings.value?.providers.find(p => p.provider_key === form.value.provider_key))
  const hasKey = computed(() => Boolean(form.value.api_key.trim() || editedProvider.value?.api_key_configured))
  const formComplete = computed(() => Boolean(form.value.name.trim() && form.value.base_url.trim() && form.value.model.trim()))
  const canTest = computed(() => !busy.value && hasKey.value && Boolean(form.value.base_url.trim() && form.value.model.trim()))
  const canSave = computed(() => !busy.value && formComplete.value && (settings.value?.enabled || hasKey.value))
  const canEnable = computed(() => !busy.value && Boolean(settings.value?.providers.some(p => p.is_default && p.usable)))
  const replacements = computed(() => (settings.value?.providers || []).filter(p => p.provider_key !== deleting.value?.provider_key && p.usable))
  const deleteBlocked = computed(() => Boolean(deleting.value?.is_default && settings.value?.enabled && !replacements.value.some(p => p.provider_key === replacementKey.value)))

  function handleError(err: unknown, inForm = false) {
    let message = err instanceof Error ? err.message : String(err)
    if (err instanceof APIRequestError) {
      fieldErrors.value = err.fields
      if (err.settings) settings.value = err.settings
      message = Object.entries(err.providerErrors).map(([key, detail]) => `${key}：${detail}`).join('；') || (err.detail ? `${err.message}：${err.detail}` : err.message)
    }
    if (inForm) formError.value = message
    else banner.value = { variant: 'danger', message }
  }

  async function load() {
    if (busy.value) return
    loading.value = true
    banner.value = null
    try { settings.value = await api.aiSettings() } catch (err) { handleError(err) } finally { loading.value = false }
  }

  function openEditor(provider?: AISettingsProviderSummary) {
    if (busy.value) return
    form.value = provider ? { provider_key: provider.provider_key, name: provider.name, base_url: provider.base_url, model: provider.model, api_key: '', timeout_seconds: provider.timeout_seconds || provider.request_timeout_seconds || 60, max_rpm: provider.max_rpm || 60, priority: provider.priority || 10, cost_tier: provider.cost_tier || 'medium' } : defaults()
    selectedPreset.value = provider ? aiProviderPresets.find(p => p.key !== 'custom' && p.name === provider.name && p.base_url === provider.base_url)?.key || 'custom' : 'deepseek'
    formDirty.value = false
    fieldErrors.value = {}
    formError.value = ''
    testResult.value = null
    drawerOpen.value = true
  }

  function closeEditor() {
    if (busy.value) return
    drawerOpen.value = false
    form.value.api_key = ''
  }

  function markDirty() { formDirty.value = true; testResult.value = null }
  function applyPreset() {
    const preset = aiProviderPresets.find(p => p.key === selectedPreset.value)
    if (!preset) return
    // 沿用原表单规则：已经手工修改的非空字段，不被预设静默覆盖。
    for (const key of ['name', 'base_url', 'model'] as const) if (!formDirty.value || !form.value[key].trim()) form.value[key] = preset[key]
    if (!formDirty.value) Object.assign(form.value, { timeout_seconds: 60, max_rpm: 60, priority: 10, cost_tier: 'medium' })
    formDirty.value = false
    testResult.value = null
  }

  async function testProvider(provider?: AISettingsProviderSummary) {
    if (busy.value || (!provider && !canTest.value)) return
    operation.value = 'test'
    providerBusy.value = provider?.provider_key || ''
    fieldErrors.value = {}
    formError.value = ''
    testResult.value = null
    try {
      const result = await api.testAIProvider(provider ? { ...provider, api_key: '' } : { ...form.value })
      if (provider) {
        provider.last_test_status = result.status
        provider.last_test_message = result.message || result.safe_error
        banner.value = { variant: result.status === 'pass' ? 'ok' : 'danger', message: `${provider.name}：${result.message || result.safe_error || '连接测试失败'} · ${result.latency_ms} ms` }
      } else testResult.value = result
    } catch (err) { handleError(err, !provider) } finally { operation.value = ''; providerBusy.value = '' }
  }

  async function save() {
    if (!canSave.value) return
    operation.value = 'save'
    fieldErrors.value = {}
    formError.value = ''
    const enable = !settings.value?.enabled
    try {
      // 旧版路由字段在过渡期仍由后端保存，普通编辑只提交可见字段，避免重置旧配置。
      const { provider_key, name, base_url, model, api_key, timeout_seconds } = form.value
      const payload = { name, base_url, model, api_key, timeout_seconds }
      // 已有项始终 PATCH 自身；默认供应商专用 PUT 会改变路由默认项，不能用于普通编辑。
      const response = provider_key
        ? await api.updateAIProvider(provider_key, { ...payload, test_before_save: enable })
        : settings.value?.providers.length
          ? await api.createAIProvider({ ...payload, test_before_save: enable })
          : await api.saveAIDefaultProvider({ ...defaults(), ...payload, enable })
      settings.value = response.settings
      // 保存与应用是两个接口；先记住新 key，应用失败时再次保存不会重复创建。
      form.value.provider_key = response.provider.provider_key
      form.value.api_key = ''
      let message = response.message
      if (enable && !response.settings.enabled) {
        const applied = await api.applyAISettings({ enabled: true, test_policy: 'default_only' })
        settings.value = applied.settings
        message = applied.message
      }
      drawerOpen.value = false
      banner.value = { variant: 'ok', message }
    } catch (err) { handleError(err, true) } finally { operation.value = '' }
  }

  async function apply() {
    if (!canEnable.value) return
    operation.value = 'apply'
    banner.value = null
    try {
      // 停用会发布新版本；没有草稿时 apply 是空操作，先用默认项创建可测试的草稿。
      if (!settings.value?.enabled && !settings.value?.has_unapplied_changes) {
        const provider = settings.value?.providers.find(p => p.is_default)
        if (!provider) return
        const prepared = await api.updateAIProvider(provider.provider_key, { name: provider.name })
        settings.value = prepared.settings
      }
      const response = await api.applyAISettings({ enabled: true, test_policy: 'default_only' })
      settings.value = response.settings
      banner.value = { variant: 'ok', message: response.message }
    } catch (err) { handleError(err) } finally { operation.value = '' }
  }

  async function disable() {
    if (busy.value || !settings.value?.enabled) return
    operation.value = 'disable'
    try { settings.value = (await api.setAIEnabled(false)).settings; banner.value = { variant: 'info', message: 'AI 问答已停用' } }
    catch (err) { handleError(err) } finally { operation.value = '' }
  }

  async function makeDefault(provider: AISettingsProviderSummary) {
    if (busy.value || provider.is_default || !provider.usable) return
    operation.value = 'default'
    providerBusy.value = provider.provider_key
    try {
      const response = await api.updateAIProvider(provider.provider_key, { make_default: true })
      settings.value = response.settings
      banner.value = { variant: 'info', message: response.message }
    } catch (err) { handleError(err) } finally { operation.value = ''; providerBusy.value = '' }
  }

  function prepareDelete(provider: AISettingsProviderSummary) {
    if (busy.value) return
    deleting.value = provider
    replacementKey.value = replacements.value[0]?.provider_key || ''
    fieldErrors.value = {}
  }

  async function remove() {
    if (busy.value || !deleting.value || deleteBlocked.value) return
    operation.value = 'delete'
    providerBusy.value = deleting.value.provider_key
    try {
      const response = await api.deleteAIProvider(deleting.value.provider_key, deleting.value.is_default ? replacementKey.value : '')
      settings.value = response.settings
      deleting.value = null
      banner.value = { variant: 'info', message: response.message }
    } catch (err) { handleError(err) } finally { operation.value = ''; providerBusy.value = '' }
  }

  return { settings, loading, busy, operation, providerBusy, banner, drawerOpen, form, selectedPreset, fieldErrors, formError, testResult, editedProvider, canTest, canSave, canEnable, deleting, replacementKey, replacements, deleteBlocked, load, openEditor, closeEditor, markDirty, applyPreset, testProvider, save, apply, disable, makeDefault, prepareDelete, remove }
}
