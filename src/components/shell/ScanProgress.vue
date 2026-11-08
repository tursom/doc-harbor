<script setup lang="ts">
import { computed } from 'vue'
import type { ScanRun } from '../../types'
import { scanProgress, scanStageLabel } from '../../lib/scan-progress'
const props = defineProps<{ run: ScanRun }>()
const progress = computed(() => scanProgress(props.run))
</script>
<template>
  <div class="scan-progress" role="status" aria-live="polite">
    <span v-if="progress"><strong>{{ scanStageLabel(progress.stage) }}</strong><span v-if="progress.branch"> · 分支 {{ progress.branch }}</span> · 准备文件 {{ progress.processed_files }} / {{ progress.total_files || '待统计' }}</span>
    <span v-else-if="run.status === 'running'">后台扫描已开始，等待进度</span>
    <span>已提交 {{ run.file_count }} 个文件</span>
    <small v-if="run.status === 'running'">准备进度不代表已写入索引；文件总数随扫描阶段更新。</small>
  </div>
</template>
<style scoped>
.scan-progress { display: flex; flex-wrap: wrap; align-items: baseline; gap: 4px 16px; font-size: var(--fs-sm); color: var(--fg-muted); overflow-wrap: anywhere; font-variant-numeric: tabular-nums; }
small { flex-basis: 100%; color: var(--fg-subtle); }
</style>
