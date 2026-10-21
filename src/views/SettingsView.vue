<script setup lang="ts">
import { ref, watch } from 'vue'
import { Eye, EyeOff } from 'lucide-vue-next'
import { useAccessToken } from '../composables/useAccessToken'
import { useWebhookSecret } from '../composables/useWebhookSecret'
import { formatTime } from '../lib/format'
import CopyField from '../components/ui/CopyField.vue'
import InlineConfirm from '../components/ui/InlineConfirm.vue'
import Notice from '../components/ui/Notice.vue'
const { accessTokenForm, issuedAccessToken, accessTokenBusy, settingsBanner, createAccessToken } = useAccessToken()
const { webhookSecret, webhookSecretVisible, webhookSecretLoading, configured, banner, toggleWebhookSecret, refreshWebhookSecret, loadSecret } = useWebhookSecret()
const ttlChoice = ref('3600')
watch(ttlChoice, (value) => { if (value !== 'custom') accessTokenForm.value.ttl_seconds = Number(value) })
</script>
<template>
  <div class="system-settings">
    <section class="settings-section"><div><h3>访问令牌</h3><p>签发短期 Token 给脚本或排查人员调用受保护的接口。Token 只显示一次。</p></div><div class="fields">
      <form class="fields" @submit.prevent="createAccessToken">
        <div class="field-pair"><label>有效期<select v-model="ttlChoice"><option value="3600">1 小时</option><option value="28800">8 小时</option><option value="86400">24 小时</option><option value="custom">自定义</option></select></label><label v-if="ttlChoice === 'custom'">有效期秒数<input v-model.number="accessTokenForm.ttl_seconds" type="number" min="300" max="86400" required /></label></div>
        <label>Viewer Key 范围<input v-model.trim="accessTokenForm.viewer_key" class="mono" placeholder="留空表示不限制" /></label>
        <fieldset><legend>能力</legend><label class="check"><input v-model="accessTokenForm.capabilities" type="checkbox" value="ai.history.read" />AI 历史读取 <code>ai.history.read</code></label><label class="check"><input v-model="accessTokenForm.capabilities" type="checkbox" value="ai.diagnostics.read" />AI 问答排查 <code>ai.diagnostics.read</code></label></fieldset>
        <div><button class="btn primary" type="submit" :disabled="accessTokenBusy">{{ accessTokenBusy ? '签发中…' : '签发 Token' }}</button></div>
      </form>
      <Notice v-if="settingsBanner" :variant="settingsBanner.type">{{ settingsBanner.message }}</Notice>
      <Notice v-if="issuedAccessToken" variant="warn"><div class="token-result"><strong>复制并保存这个 Token，关闭页面后无法再次查看。</strong><code>{{ issuedAccessToken.token }}</code><CopyField :value="issuedAccessToken.token" label="已签发 Token" /><small>过期时间：{{ formatTime(issuedAccessToken.expires_at) }}<br />Viewer 范围：{{ issuedAccessToken.scope.viewer_key || '不限制' }}</small></div></Notice>
    </div></section>
    <section class="settings-section" id="github-webhook"><div><h3>GitHub Webhook</h3><p>所有仓库共用一个 Secret。轮换后要同步更新每个 GitHub 仓库的 Webhook 配置，否则推送会验签失败。</p></div><div class="fields">
      <template v-if="configured"><CopyField label="Webhook Secret" :value="webhookSecret" :masked="!webhookSecretVisible" :revealable="false" /><div class="secret-actions"><button class="btn" :disabled="webhookSecretLoading" @click="toggleWebhookSecret"><EyeOff v-if="webhookSecretVisible" :size="14" /><Eye v-else :size="14" />{{ webhookSecretVisible ? '隐藏 Secret' : '显示 Secret' }}</button><InlineConfirm label="轮换 Secret" confirm-label="确认轮换" :busy="webhookSecretLoading" @confirm="refreshWebhookSecret"><p>旧 Secret 将立即失效，请更新所有仓库的 Webhook 和 Actions 签名密钥。</p></InlineConfirm></div></template>
      <div v-else-if="configured === false" class="secret-actions"><span class="muted">未配置</span><button class="btn" :disabled="webhookSecretLoading" @click="refreshWebhookSecret">生成 Secret</button></div>
      <div v-else class="secret-actions"><span class="muted">{{ webhookSecretLoading ? '正在读取…' : '未能读取 Secret' }}</span><button v-if="!webhookSecretLoading" class="btn" @click="loadSecret">重试</button></div>
      <Notice v-if="banner" :variant="banner.type">{{ banner.message }}</Notice>
      <small>各仓库的 Payload URL 在对应仓库的「仓库设置」中。</small>
    </div></section>
  </div>
</template>
<style scoped>
.system-settings { max-width: 936px; padding: 0 28px 32px; }
.settings-section { display: grid; grid-template-columns: 240px minmax(0, 1fr); gap: 36px; padding: 28px 0; border-bottom: 1px solid var(--border); }
h3 { font-size: var(--fs-md); margin: 0 0 8px; }
p, small { margin: 0; color: var(--fg-muted); font-size: var(--fs-sm); line-height: 1.65; }
.fields, label { display: grid; gap: 6px; min-width: 0; }
.fields { gap: 16px; }
label, legend { font-size: var(--fs-sm); font-weight: 500; }
.field-pair { display: grid; grid-template-columns: 1fr 1fr; gap: 12px; }
fieldset { margin: 0; padding: 0; border: 0; min-width: 0; }
legend { padding: 0; margin-bottom: 8px; }
.check { display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin: 8px 0; }
.check code { font-size: var(--fs-xs); color: var(--fg-subtle); }
.mono, code { font-family: var(--font-mono); }
.secret-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
.token-result { display: grid; gap: 12px; min-width: 0; }
.token-result > code { overflow-wrap: anywhere; }
@media(max-width: 1080px) { .settings-section { grid-template-columns: 1fr; gap: 20px; } }
@media(max-width: 860px) { .system-settings { padding: 0 16px 24px; } }
</style>
