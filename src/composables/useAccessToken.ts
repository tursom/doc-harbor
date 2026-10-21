import { computed, ref } from 'vue'
import { api } from '../api'
import type { AccessTokenResponse } from '../types'

export function useAccessToken() {
  const accessTokenForm = ref({ ttl_seconds: 3600, capabilities: ['ai.history.read'], viewer_key: '' })
  const issuedAccessToken = ref<AccessTokenResponse | null>(null)
  const accessTokenBusy = ref(false)
  const settingsBanner = ref<{ type: 'ok' | 'danger'; message: string } | null>(null)
  const docsOnly = computed(() => accessTokenForm.value.capabilities.length === 1 && accessTokenForm.value.capabilities[0] === 'docs.read')
  const maxTokenTTL = computed(() => docsOnly.value ? 7776000 : 86400)
  const ttlValidation = computed(() => {
    const ttl = Number(accessTokenForm.value.ttl_seconds)
    if (!Number.isInteger(ttl) || ttl < 300 || ttl > maxTokenTTL.value) {
      return docsOnly.value ? '有效期必须在 300 秒至 90 天之间' : '包含 AI 历史或诊断能力时，有效期最长 24 小时；7/30/90 天仅适用于单独的 docs.read'
    }
    return ''
  })
  async function createAccessToken() {
    if (accessTokenBusy.value) return
    const ttl = Number(accessTokenForm.value.ttl_seconds)
    if (ttlValidation.value) {
      settingsBanner.value = { type: 'danger', message: ttlValidation.value }
      return
    }
    if (!accessTokenForm.value.capabilities.length) {
      settingsBanner.value = { type: 'danger', message: '请至少选择一项能力' }
      return
    }
    accessTokenBusy.value = true
    settingsBanner.value = null
    issuedAccessToken.value = null
    try {
      issuedAccessToken.value = await api.createAccessToken({
        ttl_seconds: ttl, capabilities: accessTokenForm.value.capabilities,
        scope: { viewer_key: accessTokenForm.value.viewer_key.trim() || undefined }
      })
      settingsBanner.value = { type: 'ok', message: '访问 Token 已签发' }
    } catch (err) {
      settingsBanner.value = { type: 'danger', message: err instanceof Error ? err.message : String(err) }
    } finally { accessTokenBusy.value = false }
  }
  return { accessTokenForm, issuedAccessToken, accessTokenBusy, settingsBanner, docsOnly, maxTokenTTL, ttlValidation, createAccessToken }
}
