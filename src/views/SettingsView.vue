<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { Eye, EyeOff } from 'lucide-vue-next'
import { useAccessToken } from '../composables/useAccessToken'
import { useWebhookSecret } from '../composables/useWebhookSecret'
import { formatTime } from '../lib/format'
import CopyField from '../components/ui/CopyField.vue'
import InlineConfirm from '../components/ui/InlineConfirm.vue'
import Notice from '../components/ui/Notice.vue'
import { mcpClientConfigs } from '../lib/mcp-config'
const { accessTokenForm, issuedAccessToken, accessTokenBusy, settingsBanner, docsOnly, maxTokenTTL, ttlValidation, createAccessToken } = useAccessToken()
const { webhookSecret, webhookSecretVisible, webhookSecretLoading, configured, banner, toggleWebhookSecret, refreshWebhookSecret, loadSecret } = useWebhookSecret()
const ttlChoice = ref('3600')
const mcpAddress = ref('/api/mcp')
const mcpConfigs = computed(() => mcpClientConfigs(mcpAddress.value))
onMounted(() => { mcpAddress.value = new URL('/api/mcp', window.location.origin).href })
watch(ttlChoice, (value) => { if (value !== 'custom') accessTokenForm.value.ttl_seconds = Number(value) })
</script>
<template>
  <div class="system-settings">
    <section id="access-tokens" class="settings-section"><div><h3>访问令牌</h3><p>为脚本、诊断或 MCP 客户端签发访问 Token。Token 只显示一次。</p></div><div class="fields">
      <form class="fields" @submit.prevent="createAccessToken">
        <div class="field-pair"><label>有效期<select v-model="ttlChoice" aria-label="有效期"><option value="3600">1 小时</option><option value="28800">8 小时</option><option value="86400">24 小时</option><option value="604800" :disabled="!docsOnly">7 天（仅 docs.read）</option><option value="2592000" :disabled="!docsOnly">30 天（仅 docs.read）</option><option value="7776000" :disabled="!docsOnly">90 天（仅 docs.read）</option><option value="custom">自定义</option></select></label><label v-if="ttlChoice === 'custom'">有效期秒数<input v-model.number="accessTokenForm.ttl_seconds" type="number" min="300" :max="maxTokenTTL" required /></label></div>
        <label>Viewer Key 范围<input v-model.trim="accessTokenForm.viewer_key" class="mono" placeholder="留空表示不限制" /></label>
        <fieldset><legend>能力</legend><label class="check"><input v-model="accessTokenForm.capabilities" type="checkbox" value="ai.history.read" />AI 历史读取 <code>ai.history.read</code></label><label class="check"><input v-model="accessTokenForm.capabilities" type="checkbox" value="ai.diagnostics.read" />AI 问答排查 <code>ai.diagnostics.read</code></label><label class="check"><input v-model="accessTokenForm.capabilities" type="checkbox" value="docs.read" />文档与代码只读访问 <code>docs.read</code></label></fieldset>
        <small>单独选择 <code>docs.read</code> 可签发最长 90 天的令牌；AI 历史和诊断能力最长 24 小时。Viewer Key 仅限制 AI 历史及诊断，不限制 MCP 仓库范围。</small>
        <Notice v-if="ttlValidation" variant="warn">{{ ttlValidation }}</Notice>
        <div><button class="btn primary" type="submit" :disabled="accessTokenBusy || Boolean(ttlValidation) || !accessTokenForm.capabilities.length">{{ accessTokenBusy ? '签发中…' : '签发 Token' }}</button></div>
      </form>
      <Notice v-if="settingsBanner" :variant="settingsBanner.type">{{ settingsBanner.message }}</Notice>
      <Notice v-if="issuedAccessToken" variant="warn"><div class="token-result"><strong>复制并保存这个 Token，关闭页面后无法再次查看。</strong><code>{{ issuedAccessToken.token }}</code><CopyField :value="issuedAccessToken.token" label="已签发 Token" /><small>过期时间：{{ formatTime(issuedAccessToken.expires_at) }}<br />Viewer 范围：{{ issuedAccessToken.scope.viewer_key || '不限制' }}</small><a v-if="issuedAccessToken.capabilities.includes('ai.diagnostics.read')" href="/ai/diagnostics/">前往 AI 诊断</a></div></Notice>
    </div></section>
    <section id="mcp" class="settings-section"><div><h3>MCP 接入</h3><p>让 pi、Codex 或 Claude Code 只读搜索和阅读公司仓库中的文档与代码。</p></div><div class="fields">
      <CopyField :value="mcpAddress" label="MCP 地址" />
      <p>签发带 <code>docs.read</code> 的令牌，在客户端环境中设置 <code>DOCHARBOR_TOKEN</code>，再复制对应配置。pi 使用 direct 暴露工具。</p>
      <details open><summary>pi</summary><CopyField :value="mcpConfigs.pi" label="pi 命令" /></details>
      <details><summary>Codex · ~/.codex/config.toml</summary><CopyField :value="mcpConfigs.codex" label="Codex 配置" /><pre>{{ mcpConfigs.codex }}</pre></details>
      <details><summary>Claude Code</summary><CopyField :value="mcpConfigs.claude" label="Claude Code 命令" /></details>
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
.system-settings a { color: var(--accent-text); } details { min-width: 0; } summary { cursor: pointer; margin-bottom: 10px; font-size: var(--fs-sm); font-weight: 500; } pre { margin: 10px 0 0; padding: 12px; max-width: 100%; overflow: auto; border-radius: 6px; background: var(--surface-2); font: var(--fs-xs)/1.6 var(--font-mono); } .fields > p { overflow-wrap: anywhere; }
.secret-actions { display: flex; align-items: center; flex-wrap: wrap; gap: 10px; }
.token-result { display: grid; gap: 12px; min-width: 0; }
.token-result > code { overflow-wrap: anywhere; }
@media(max-width: 1080px) { .settings-section { grid-template-columns: 1fr; gap: 20px; } }
@media(max-width: 860px) { .system-settings { padding: 0 16px 24px; } }
</style>
