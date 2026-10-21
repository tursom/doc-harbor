<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, watch } from 'vue'
import { useRepos } from '../composables/useRepos'
import { useRepoForm } from '../composables/useRepoForm'
import CopyField from '../components/ui/CopyField.vue'
import Notice from '../components/ui/Notice.vue'
import Switch from '../components/ui/Switch.vue'
const { selectedRepo, loadAll } = useRepos()
const { form, trackedBranchesText, latestIncludeText, latestExcludeText, branchPriorityText, scanPathsText, fieldErrors, error, saving, dirty, autoSync, reset, save } = useRepoForm()
const githubWebhookURL = computed(() => selectedRepo.value && typeof window !== 'undefined' ? `${window.location.origin}/api/webhooks/github/${selectedRepo.value.id}` : '')
watch(selectedRepo, (repo, old) => {
  // 扫描刷新同一个仓库时保留未保存的输入；切换仓库则载入对应配置。
  if (!dirty.value || repo?.id !== old?.id) reset(repo)
}, { immediate: true })
async function saveChanges() { if (await save()) await loadAll() }
function beforeUnload(event: BeforeUnloadEvent) {
  if (!dirty.value) return
  event.preventDefault()
  event.returnValue = ''
}
onMounted(() => window.addEventListener('beforeunload', beforeUnload))
onBeforeUnmount(() => window.removeEventListener('beforeunload', beforeUnload))
</script>
<template>
  <form v-if="selectedRepo" class="repository-settings" novalidate @submit.prevent="saveChanges">
    <section class="settings-section"><div><h3>基本信息</h3><p>显示名称和 Git 地址。Slug 用于链接，留空时按名称生成。</p></div><div class="fields">
      <label>名称<input v-model="form.name" required /><small v-if="fieldErrors.name" class="field-error">{{ fieldErrors.name }}</small></label>
      <label>Slug<input v-model="form.slug" class="mono" placeholder="留空按名称生成" /><small v-if="fieldErrors.slug" class="field-error">{{ fieldErrors.slug }}</small></label>
      <label>Git URL<input v-model="form.repo_url" class="mono" required /><small v-if="fieldErrors.repo_url" class="field-error">{{ fieldErrors.repo_url }}</small><small>私有仓库的凭据放在服务端 credentials/ 目录，不写入数据库。</small></label>
    </div></section>
    <section class="settings-section"><div><h3>同步</h3><p>DocHarbor 维护一份 bare mirror，按周期拉取并扫描追踪分支。</p></div><div class="fields">
      <label class="switch-row"><Switch v-model="autoSync" aria-label="自动拉取" />自动拉取</label><small>关闭后不在启动时和按周期拉取，仍可手动扫描或由 Webhook 触发。</small>
      <div class="field-pair"><label>默认分支<input v-model="form.default_branch" class="mono" /><small v-if="fieldErrors.default_branch" class="field-error">{{ fieldErrors.default_branch }}</small></label><label v-if="autoSync">拉取间隔秒<input v-model.number="form.sync_interval_seconds" type="number" min="60" /><small v-if="fieldErrors.sync_interval_seconds" class="field-error">{{ fieldErrors.sync_interval_seconds }}</small></label></div>
      <label>追踪分支<input v-model="trackedBranchesText" class="mono" /><small v-if="fieldErrors.tracked_branches" class="field-error">{{ fieldErrors.tracked_branches }}</small><small>逗号分隔，支持通配符</small></label>
    </div></section>
    <section class="settings-section"><div><h3>智能最新</h3><p>每篇文档在参与分支中取最近修改的有效版本。同一时间有多个版本时，按分支优先级决定。</p></div><div class="fields">
      <label>参与分支（include）<input v-model="latestIncludeText" class="mono" /><small v-if="fieldErrors.latest_include_branches" class="field-error">{{ fieldErrors.latest_include_branches }}</small><small>逗号分隔，支持通配符</small></label>
      <label>排除分支（exclude）<input v-model="latestExcludeText" class="mono" /><small v-if="fieldErrors.latest_exclude_branches" class="field-error">{{ fieldErrors.latest_exclude_branches }}</small><small>逗号分隔，支持通配符</small></label>
      <label>分支优先级<input v-model="branchPriorityText" class="mono" /><small v-if="fieldErrors.branch_priority" class="field-error">{{ fieldErrors.branch_priority }}</small><small>逗号分隔，支持通配符</small></label>
    </div></section>
    <section class="settings-section"><div><h3>扫描目录</h3><p>只索引这些目录下的文件，每行一个，相对仓库根目录。</p></div><div class="fields"><label>扫描目录<textarea v-model="scanPathsText" class="mono" rows="4" /><small v-if="fieldErrors.scan_paths" class="field-error">{{ fieldErrors.scan_paths }}</small></label></div></section>
    <section class="settings-section"><div><h3>GitHub Webhook</h3><p>在 GitHub 仓库的 Settings → Webhooks 中填入此地址，push 后立即触发扫描。</p></div><div class="fields"><CopyField label="Payload URL" :value="githubWebhookURL" /><Notice variant="info">Secret 由所有仓库共用，在 <a href="/settings/">系统设置 → GitHub Webhook</a> 中查看或轮换。</Notice></div></section>
    <div v-if="dirty || error" class="save-bar" role="status"><span :class="{ 'field-error': error }">{{ error || '有未保存的修改' }}</span><div><button class="btn" type="button" :disabled="saving" @click="reset(selectedRepo)">放弃</button><button class="btn primary" type="submit" :disabled="saving">{{ saving ? '保存中…' : '保存' }}</button></div></div>
  </form>
</template>
<style scoped>
.repository-settings { max-width: 936px; padding: 0 28px 32px; }
.settings-section { display: grid; grid-template-columns: 240px minmax(0, 1fr); gap: 36px; padding: 28px 0; border-bottom: 1px solid var(--border); }
h3 { margin: 0 0 8px; font-size: var(--fs-md); }
p, small { color: var(--fg-muted); font-size: var(--fs-sm); font-weight: 400; line-height: 1.65; }
p { margin: 0; }
.fields, label { display: grid; gap: 6px; min-width: 0; }
.fields { gap: 16px; }
label { font-size: var(--fs-sm); font-weight: 500; }
.mono { font-family: var(--font-mono); }
.switch-row { display: flex; align-items: center; gap: 10px; }
.field-pair { display: grid; grid-template-columns: 1fr 1fr; gap: 16px; }
.field-error { color: var(--danger); }
.save-bar { position: sticky; bottom: env(safe-area-inset-bottom); background: var(--surface); border: 1px solid var(--border-strong); border-radius: 8px; padding: 12px 16px; display: flex; align-items: center; justify-content: space-between; gap: 16px; margin-top: 20px; }
.save-bar > div { display: flex; gap: 8px; flex-shrink: 0; }
@media(max-width: 1080px) { .settings-section { grid-template-columns: 1fr; gap: 20px; } }
@media(max-width: 860px) { .repository-settings { padding: 0 16px 24px; } .settings-section { padding: 22px 0; } .save-bar { flex-wrap: wrap; } }
</style>
