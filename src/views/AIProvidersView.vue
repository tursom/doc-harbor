<script setup lang="ts">
import { onMounted } from 'vue'
import { Check, Plus, RefreshCw, ShieldCheck } from 'lucide-vue-next'
import StatusPill from '../components/ui/StatusPill.vue'
import EmptyState from '../components/ui/EmptyState.vue'
import Notice from '../components/ui/Notice.vue'
import DrawerPanel from '../components/ui/DrawerPanel.vue'
import DropdownMenu from '../components/ui/DropdownMenu.vue'
import InlineConfirm from '../components/ui/InlineConfirm.vue'
import { aiProviderPresets, useAISettings } from '../composables/useAISettings'

const { settings, loading, busy, operation, banner, drawerOpen, form, selectedPreset, fieldErrors, formError, testResult, editedProvider, canTest, canSave, canEnable, activeRoutes, deleting, replacementKey, replacements, deleteBlocked, load, openEditor, closeEditor, markDirty, applyPreset, testProvider, save, apply, disable, makeDefault, prepareDelete, remove } = useAISettings()
onMounted(load)
</script>

<template>
  <section class="providers-page">
    <header class="page-heading">
      <div><h2>AI 供应商</h2><p>OpenAI 兼容接口。按路由顺序调用，前一个失败时自动切到下一个。</p></div>
      <button class="btn primary" type="button" :disabled="busy" aria-label="新增供应商" @click="openEditor()"><Plus :size="16" /><span class="action-label">新增供应商</span></button>
    </header>
    <div class="page-content">
      <Notice v-if="banner" :variant="banner.variant">{{ banner.message }}<button v-if="!settings" class="btn sm" type="button" :disabled="busy" @click="load">重试</button></Notice>
      <EmptyState v-if="loading && !settings" message="正在加载供应商…" />
      <template v-if="settings">
        <div class="status-strip">
          <StatusPill :status="settings.status" />
          <div class="route-chain"><span class="muted">路由顺序</span><template v-for="(provider, index) in activeRoutes" :key="provider.provider_key"><span v-if="index" aria-hidden="true">→</span><span>#{{ index + 1 }} {{ provider.name }} <code>{{ provider.model }}</code></span></template><span v-if="!activeRoutes.length" class="muted">暂无生效路由</span></div>
          <div class="storage"><span :class="settings.encryption_ready ? 'muted' : 'danger-text'">{{ settings.encryption_ready ? '密钥存储已就绪' : '密钥存储不可用' }}</span><span v-if="settings.last_test" :title="settings.last_test.message">最近测试：<StatusPill :status="settings.last_test.status" /></span></div>
          <InlineConfirm v-if="settings.enabled" label="停用" confirm-label="确认停用" :busy="busy" @confirm="disable"><span>停用后将使用本地检索模式。</span></InlineConfirm>
          <button v-else-if="canEnable || operation === 'apply'" class="btn primary sm" type="button" :disabled="busy" @click="apply">{{ operation === 'apply' ? '启用中…' : '启用' }}</button>
        </div>
        <Notice v-if="settings.has_unapplied_changes" variant="warn">
          <div class="pending-config"><div><strong>有待应用的配置</strong><div class="route-chain"><span>待应用路由顺序：</span><template v-for="(provider, index) in settings.route_providers" :key="provider.provider_key"><span v-if="index">→</span><span>#{{ index + 1 }} {{ provider.name }} <code>{{ provider.model }}</code></span></template><span v-if="!settings.route_providers.length">暂无可用路由，请先完善供应商配置。</span></div></div><button class="btn primary" type="button" :disabled="!canEnable" @click="apply">{{ operation === 'apply' ? '应用中…' : '应用配置' }}</button></div>
        </Notice>
        <EmptyState v-if="!settings.providers.length" message="还没有供应商"><button class="btn primary" type="button" :disabled="busy" @click="openEditor()">新增供应商</button></EmptyState>
        <div v-else class="table-scroll">
          <table class="provider-table"><thead><tr><th>供应商</th><th>模型</th><th class="hide-sm">API Key</th><th class="hide-sm number">路由</th><th>连接</th><th><span class="sr-only">操作</span></th></tr></thead>
            <tbody><template v-for="provider in settings.providers" :key="provider.provider_key"><tr>
              <td><div class="provider-name"><span>{{ provider.name }}</span><StatusPill v-if="provider.is_default" status="default" /></div><div class="provider-url" :title="provider.base_url">{{ provider.base_url }}</div></td>
              <td><code>{{ provider.model || '—' }}</code></td>
              <td class="hide-sm"><span v-if="provider.api_key_configured" class="muted">尾号 <code>{{ provider.api_key_last4 || '****' }}</code></span><StatusPill v-else status="not_configured" /></td>
              <td class="hide-sm number">{{ provider.route_order ? `#${provider.route_order}` : '—' }}</td>
              <td><span v-if="provider.last_test_status === 'pass'" class="test-ok" :title="provider.last_test_message"><Check :size="14" />正常</span><StatusPill v-else :status="provider.last_test_status || 'not_run'" :title="provider.last_test_message || '未测试'" /></td>
              <td><div class="row-actions"><button class="btn sm" type="button" :disabled="busy" @click="openEditor(provider)">编辑</button><DropdownMenu><button type="button" :disabled="busy || !provider.api_key_configured" @click="testProvider(provider)">测试连接</button><button type="button" :disabled="busy || provider.is_default || !provider.usable" @click="makeDefault(provider)">设为默认</button><button class="delete-menu" type="button" :disabled="busy" @click="prepareDelete(provider)">删除</button></DropdownMenu></div></td>
            </tr><tr v-if="deleting?.provider_key === provider.provider_key" class="delete-row"><td colspan="6">
              <div class="delete-confirm"><strong>删除供应商「{{ provider.name }}」</strong><p>删除后，该供应商将从配置中移除。</p>
                <label v-if="provider.is_default && replacements.length" class="replacement">替代默认供应商<select v-model="replacementKey" :disabled="busy"><option v-for="replacement in replacements" :key="replacement.provider_key" :value="replacement.provider_key">{{ replacement.name }} · {{ replacement.model }}</option></select></label>
                <p v-if="deleteBlocked" class="danger-text">当前默认供应商正在服务，请先新增可用供应商或停用 AI 问答。</p>
                <p v-if="fieldErrors.replacement_provider_key" class="danger-text">{{ fieldErrors.replacement_provider_key }}</p>
                <div class="row-actions"><button class="btn sm danger" type="button" :disabled="busy || deleteBlocked" @click="remove">{{ operation === 'delete' ? '删除中…' : '确认删除' }}</button><button class="btn sm" type="button" :disabled="busy" @click="deleting = null">取消</button></div>
              </div>
            </td></tr></template></tbody>
          </table>
        </div>
      </template>
    </div>
    <DrawerPanel :open="drawerOpen" :title="form.provider_key ? '编辑供应商' : '新增供应商'" @close="closeEditor">
      <form id="ai-provider-form" class="provider-form" @submit.prevent="save">
        <fieldset :disabled="busy">
          <label>供应商预设<select v-model="selectedPreset" @change="applyPreset"><option v-for="preset in aiProviderPresets" :key="preset.key" :value="preset.key">{{ preset.label }}</option></select></label>
          <label>供应商名称<input v-model="form.name" required :aria-invalid="Boolean(fieldErrors.name)" @input="markDirty" /><span v-if="fieldErrors.name" class="field-error">{{ fieldErrors.name }}</span></label>
          <label>Base URL<input v-model="form.base_url" class="mono" required placeholder="https://api.deepseek.com" :aria-invalid="Boolean(fieldErrors.base_url)" @input="markDirty" /><span v-if="fieldErrors.base_url" class="field-error">{{ fieldErrors.base_url }}</span></label>
          <label>模型<input v-model="form.model" class="mono" required placeholder="deepseek-v4-flash" :aria-invalid="Boolean(fieldErrors.model)" @input="markDirty" /><span v-if="fieldErrors.model" class="field-error">{{ fieldErrors.model }}</span></label>
          <label>API Key<input v-model="form.api_key" class="mono" type="password" autocomplete="off" :placeholder="editedProvider?.api_key_configured ? '已配置，留空则不修改' : '输入 API Key'" :aria-invalid="Boolean(fieldErrors.api_key)" @input="markDirty" /><span v-if="fieldErrors.api_key" class="field-error">{{ fieldErrors.api_key }}</span><small>加密保存在服务端数据目录，页面不会再显示完整 Key。</small></label>
          <details class="advanced" :open="Boolean(fieldErrors.timeout_seconds || fieldErrors.max_rpm || fieldErrors.cost_tier || fieldErrors.priority)"><summary>高级设置</summary><div class="form-grid">
            <label>超时（秒）<input v-model.number="form.timeout_seconds" type="number" min="5" max="300" required @input="markDirty" /><span v-if="fieldErrors.timeout_seconds" class="field-error">{{ fieldErrors.timeout_seconds }}</span></label>
            <label>RPM 上限<input v-model.number="form.max_rpm" type="number" min="1" max="10000" required @input="markDirty" /><span v-if="fieldErrors.max_rpm" class="field-error">{{ fieldErrors.max_rpm }}</span></label>
            <label>成本等级<select v-model="form.cost_tier" @change="markDirty"><option value="low">低</option><option value="medium">中</option><option value="high">高</option></select><span v-if="fieldErrors.cost_tier" class="field-error">{{ fieldErrors.cost_tier }}</span></label>
            <label>路由优先级<input v-model.number="form.priority" type="number" min="1" max="10000" required @input="markDirty" /><span v-if="fieldErrors.priority" class="field-error">{{ fieldErrors.priority }}</span></label>
          </div></details>
        </fieldset>
        <Notice v-if="formError" variant="danger">{{ formError }}</Notice>
        <div v-if="testResult" class="test-result" :class="testResult.status === 'pass' ? 'test-ok' : 'danger-text'" role="status"><Check v-if="testResult.status === 'pass'" :size="16" /><span>{{ testResult.status === 'pass' ? `连接正常 · ${testResult.latency_ms} ms · ${testResult.model || form.model}` : testResult.safe_error || testResult.message || '连接测试失败' }}</span></div>
      </form>
      <template #footer><div class="drawer-actions"><button class="btn" type="button" :disabled="!canTest" @click="testProvider()"><ShieldCheck :size="15" />{{ operation === 'test' ? '测试中…' : '测试连接' }}</button><span class="spacer"></span><button class="btn" type="button" :disabled="busy" @click="closeEditor">取消</button><button class="btn primary" type="submit" form="ai-provider-form" :disabled="!canSave"><RefreshCw v-if="operation === 'save'" :size="15" />{{ operation === 'save' ? '保存中…' : settings?.enabled ? '保存' : '保存并启用' }}</button></div></template>
    </DrawerPanel>
  </section>
</template>

<style scoped>
.providers-page { position: relative; min-width: 0; color: var(--fg); }
.page-heading { display: flex; align-items: center; justify-content: space-between; gap: 16px; padding: 16px 24px; border-bottom: 1px solid var(--border); }
h2 { margin: 0; font-size: var(--fs-xl); } .page-heading p { margin: 3px 0 0; color: var(--fg-muted); font-size: var(--fs-sm); }
.page-content { display: grid; gap: 24px; padding: 20px 24px; min-width: 0; }
.status-strip { display: flex; flex-wrap: wrap; align-items: center; gap: 16px; border: 1px solid var(--border); border-radius: 8px; padding: 14px 16px; }
.route-chain { display: flex; flex: 1; flex-wrap: wrap; align-items: center; gap: 6px; min-width: 0; font-size: var(--fs-sm); overflow-wrap: anywhere; }
.route-chain code { color: var(--fg-subtle); } .storage { display: flex; flex-wrap: wrap; gap: 8px; font-size: var(--fs-xs); }
.pending-config { display: flex; justify-content: space-between; align-items: center; flex-wrap: wrap; gap: 12px; } .pending-config > div { min-width: 0; flex: 1; }
:deep(.notice-content) { flex: 1; min-width: 0; }
.table-scroll { overflow-x: auto; border: 1px solid var(--border); border-radius: 8px; }
.provider-table { width: 100%; border-collapse: collapse; text-align: left; font-size: var(--fs-sm); }
th { background: var(--surface-2); color: var(--fg-muted); font-weight: 600; } th, td { padding: 6px 12px; border-bottom: 1px solid var(--border); } tbody tr:last-child td { border-bottom: 0; }
.provider-name { display: flex; gap: 6px; align-items: center; font-size: var(--fs-md); } .provider-url { max-width: 300px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; font-family: var(--font-mono); font-size: var(--fs-xs); color: var(--fg-subtle); }
code, .mono { font-family: var(--font-mono); } td code { overflow-wrap: anywhere; } .number { text-align: right; font-variant-numeric: tabular-nums; }
.row-actions { display: flex; align-items: center; gap: 6px; } .muted, small { color: var(--fg-muted); } .test-ok { display: inline-flex; align-items: center; gap: 5px; color: var(--ok); } .danger-text, .field-error { color: var(--danger); }
/* 菜单以页面为定位容器，避免最后一行的浮层被横向滚动表格裁掉。 */
:deep(.dropdown) { position: static; }
:deep(.dropdown-menu) { top: auto; right: 24px; margin-top: 30px; }
.delete-menu { border-top: 1px solid var(--border); color: var(--danger); } .delete-row td { background: var(--surface-2); } .delete-confirm { padding: 10px 0; } .delete-confirm p { margin: 6px 0; } .replacement { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; margin: 12px 0; }
.test-ok { white-space: nowrap; }
.provider-form { display: grid; gap: 16px; } fieldset { border: 0; padding: 0; margin: 0; display: grid; gap: 16px; min-width: 0; } label { display: grid; gap: 6px; } input, select { width: 100%; min-width: 0; height: 32px; border: 1px solid var(--border-strong); border-radius: 6px; padding: 0 9px; background: var(--surface); color: var(--fg); font: inherit; } .replacement select { width: auto; max-width: 100%; }
.advanced summary { cursor: pointer; color: var(--fg-muted); } .form-grid { display: grid; grid-template-columns: repeat(2, minmax(0, 1fr)); gap: 16px; padding-top: 16px; } small, .field-error { font-size: var(--fs-xs); }
.drawer-actions { display: flex; flex-wrap: wrap; align-items: center; gap: 8px; width: 100%; } .spacer { flex: 1; } .test-result { overflow-wrap: anywhere; font-size: var(--fs-sm); }
.sr-only { position: absolute; width: 1px; height: 1px; overflow: hidden; clip-path: inset(50%); }
@media (max-width: 860px) { .page-heading { padding: 16px; } .page-content { padding: 16px; gap: 16px; } .action-label, .hide-sm { display: none; } th, td { padding: 8px; } .provider-url { max-width: 130px; } .provider-name { flex-wrap: wrap; } .provider-table { min-width: 420px; } .status-strip { gap: 12px; } .status-strip > .route-chain { flex-basis: 100%; order: 2; } .status-strip > .storage { flex: 1; } .pending-config > div { flex-basis: 100%; } }
</style>
