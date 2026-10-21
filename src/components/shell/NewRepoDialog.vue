<script setup lang="ts">
import { ref, watch } from 'vue'
import { api } from '../../api'
import { useRepoForm } from '../../composables/useRepoForm'
import ModalDialog from '../ui/ModalDialog.vue'
import Notice from '../ui/Notice.vue'
import Switch from '../ui/Switch.vue'
import type { Repository } from '../../types'
const props = defineProps<{ open: boolean }>()
const emit = defineEmits<{ close: [] }>()
const { form, trackedBranchesText, latestIncludeText, latestExcludeText, branchPriorityText, scanPathsText, fieldErrors, error, saving, autoSync, reset, save } = useRepoForm()
const createdRepo = ref<Repository | null>(null)
const scanning = ref(false)
watch(() => props.open, (open) => { if (open) { reset(); createdRepo.value = null } })
async function submit() {
  if (scanning.value) return
  const created = createdRepo.value || await save()
  if (!created) return
  // 创建成功之后即使扫描失败，也不能重复 POST 仓库；重试只执行扫描。
  createdRepo.value = created
  scanning.value = true
  try {
    await api.scanRepo(created.id)
    window.location.assign(`/?repo=${created.id}`)
  } catch (err) {
    error.value = `仓库已添加，扫描未完成：${err instanceof Error ? err.message : String(err)}`
  } finally { scanning.value = false }
}
</script>
<template>
  <ModalDialog :open="open" title="新增仓库" @close="emit('close')">
    <form id="new-repository" class="repo-form" novalidate @submit.prevent="submit">
      <Notice v-if="error" variant="danger">{{ error }}<a v-if="createdRepo" :href="`/?repo=${createdRepo.id}`">前往仓库</a></Notice>
      <fieldset :disabled="saving || scanning || !!createdRepo">
        <label>Git URL<input v-model="form.repo_url" class="mono" required placeholder="https://github.com/org/repo.git" /><small v-if="fieldErrors.repo_url" class="field-error">{{ fieldErrors.repo_url }}</small></label>
        <label>名称<input v-model="form.name" required /><small v-if="fieldErrors.name" class="field-error">{{ fieldErrors.name }}</small></label>
        <label>默认分支<input v-model="form.default_branch" class="mono" required /><small v-if="fieldErrors.default_branch" class="field-error">{{ fieldErrors.default_branch }}</small></label>
        <label>扫描目录<textarea v-model="scanPathsText" class="mono" rows="2" /><small v-if="fieldErrors.scan_paths" class="field-error">{{ fieldErrors.scan_paths }}</small><small>逗号或换行分隔，相对仓库根目录。</small></label>
        <details><summary>更多选项</summary><div class="advanced-fields">
          <label>Slug<input v-model="form.slug" class="mono" placeholder="留空按名称生成" /><small v-if="fieldErrors.slug" class="field-error">{{ fieldErrors.slug }}</small></label>
          <label class="switch-row"><Switch v-model="autoSync" aria-label="自动拉取" />自动拉取 Git 仓库</label>
          <small>关闭后不在启动时和按周期拉取，仍可手动扫描或由 Webhook 触发。</small>
          <label v-if="autoSync">拉取间隔秒<input v-model.number="form.sync_interval_seconds" type="number" min="60" /><small v-if="fieldErrors.sync_interval_seconds" class="field-error">{{ fieldErrors.sync_interval_seconds }}</small></label>
          <label>追踪分支<input v-model="trackedBranchesText" class="mono" /><small>逗号分隔，支持通配符</small></label>
          <label>参与分支（include）<input v-model="latestIncludeText" class="mono" /><small>逗号分隔，支持通配符</small></label>
          <label>排除分支（exclude）<input v-model="latestExcludeText" class="mono" /><small>逗号分隔，支持通配符</small></label>
          <label>分支优先级<input v-model="branchPriorityText" class="mono" /><small>逗号分隔，支持通配符</small></label>
        </div></details>
      </fieldset>
    </form>
    <template #footer><button class="btn" type="button" @click="emit('close')">取消</button><button class="btn primary" form="new-repository" type="submit" :disabled="saving || scanning">{{ scanning ? '扫描中…' : saving ? '添加中…' : createdRepo ? '重试扫描' : '添加并开始扫描' }}</button></template>
  </ModalDialog>
</template>
<style scoped>
.repo-form, fieldset, .advanced-fields { display: grid; gap: 16px; }
fieldset { border: 0; padding: 0; margin: 0; min-width: 0; }
label { display: grid; gap: 6px; font-size: var(--fs-sm); font-weight: 500; }
small { font-size: var(--fs-sm); color: var(--fg-subtle); font-weight: 400; }
.mono { font-family: var(--font-mono); }
.switch-row { display: flex; align-items: center; gap: 10px; }
summary { cursor: pointer; color: var(--fg-muted); padding: 8px 0; }
.advanced-fields { padding-top: 12px; }
.field-error { color: var(--danger); }
</style>
