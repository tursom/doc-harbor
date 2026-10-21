import { ref } from 'vue'
import { api } from '../api'
import type { AccessTokenResponse } from '../types'

export function useAccessToken() {
  const accessTokenForm = ref({ ttl_seconds: 3600, capabilities: ['ai.history.read'], viewer_key: '' })
  const issuedAccessToken = ref<AccessTokenResponse | null>(null)
  const accessTokenBusy = ref(false)
  const settingsBanner = ref<{ type: 'ok' | 'danger'; message: string } | null>(null)
  async function createAccessToken() {
    if (accessTokenBusy.value) return
    const ttl = Number(accessTokenForm.value.ttl_seconds)
    if (!Number.isInteger(ttl) || ttl < 300 || ttl > 86400) {
      settingsBanner.value = { type: 'danger', message: '有效期必须在 300–86400 秒之间' }
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
  return { accessTokenForm, issuedAccessToken, accessTokenBusy, settingsBanner, createAccessToken }
}
