import { afterEach, describe, expect, it, vi } from 'vitest'
import { api } from '../api'
import type { AISettingsSummary } from '../types'
import { useAISettings } from './useAISettings'

afterEach(() => vi.restoreAllMocks())
const settings = { enabled: true, providers: [{ provider_key: 'primary', name: '原模型', base_url: 'https://model.example.test', model: 'demo', timeout_seconds: 60, max_rpm: 99, priority: 7, cost_tier: 'high', api_key_configured: true, usable: true, is_default: true }] } as AISettingsSummary
describe('provider settings', () => {
  it('does not reset hidden legacy routing fields when editing a provider', async () => {
    const update = vi.spyOn(api, 'updateAIProvider').mockResolvedValue({ provider: settings.providers[0]!, settings, message: '已保存' })
    const form = useAISettings()
    form.settings.value = settings
    form.openEditor(settings.providers[0])
    form.form.value.name = '更新名称'
    await form.save()
    expect(update).toHaveBeenCalledWith('primary', { name: '更新名称', base_url: 'https://model.example.test', model: 'demo', api_key: '', timeout_seconds: 60, test_before_save: false })
    expect(form.drawerOpen.value).toBe(false)
  })
  it('prepares a draft before re-enabling a disabled provider', async () => {
    const disabled = { ...settings, enabled: false, has_unapplied_changes: false }
    const update = vi.spyOn(api, 'updateAIProvider').mockResolvedValue({ provider: settings.providers[0]!, settings: { ...disabled, has_unapplied_changes: true }, message: '已保存' })
    const apply = vi.spyOn(api, 'applyAISettings').mockResolvedValue({ enabled: true, settings, message: '已启用' })
    const form = useAISettings()
    form.settings.value = disabled
    await form.apply()
    expect(update).toHaveBeenCalledWith('primary', { name: '原模型' })
    expect(apply).toHaveBeenCalledWith({ enabled: true, test_policy: 'default_only' })
    expect(form.settings.value?.enabled).toBe(true)
  })
})
