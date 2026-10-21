import { onMounted, ref } from 'vue'
import { api } from '../api'

export function useWebhookSecret() {
  const webhookSecret = ref('')
  const webhookSecretVisible = ref(false)
  const webhookSecretLoading = ref(false)
  const configured = ref<boolean | null>(null)
  const banner = ref<{ type: 'ok' | 'danger'; message: string } | null>(null)
  async function loadSecret() {
    webhookSecretLoading.value = true
    try {
      const response = await api.githubWebhookSecret()
      configured.value = response.configured
      webhookSecret.value = response.configured ? response.secret : ''
    } catch (err) { banner.value = { type: 'danger', message: err instanceof Error ? err.message : String(err) } }
    finally { webhookSecretLoading.value = false }
  }
  async function toggleWebhookSecret() {
    if (webhookSecretLoading.value) return
    if (configured.value === null) await loadSecret()
    webhookSecretVisible.value = !webhookSecretVisible.value
  }
  // Secret 在所有仓库间共享；请求失败也可能是响应丢失，先清除旧值，避免复制已失效密钥。
  async function refreshWebhookSecret() {
    if (webhookSecretLoading.value) return
    webhookSecretLoading.value = true
    webhookSecret.value = ''
    webhookSecretVisible.value = false
    configured.value = null
    banner.value = null
    try {
      const response = await api.refreshGithubWebhookSecret()
      configured.value = response.configured
      webhookSecret.value = response.configured ? response.secret : ''
      banner.value = { type: 'ok', message: 'Secret 已更新，请同步更新各仓库的 Webhook 配置。' }
    } catch (err) {
      banner.value = { type: 'danger', message: err instanceof Error ? err.message : String(err) }
    } finally { webhookSecretLoading.value = false }
  }
  onMounted(loadSecret)
  return { webhookSecret, webhookSecretVisible, webhookSecretLoading, configured, banner, toggleWebhookSecret, refreshWebhookSecret, loadSecret }
}
