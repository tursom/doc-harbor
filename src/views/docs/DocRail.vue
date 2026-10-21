<script setup lang="ts">
import { ref } from 'vue'
import BranchTag from '../../components/ui/BranchTag.vue'
import StatusPill from '../../components/ui/StatusPill.vue'
import Notice from '../../components/ui/Notice.vue'
import { formatTime, shortSha } from '../../lib/format'
import type { FileContent, PathEvent } from '../../types'
import type { DocHeading } from '../../composables/useMarkdownPreview'

defineProps<{ content: FileContent; events: PathEvent[]; eventsError: string; eventsLoading: boolean; headings: DocHeading[]; activeHeading: string }>()
const emit = defineEmits<{ heading: [id: string]; version: [id: number] }>()
const eventsSection = ref<HTMLElement | null>(null)
defineExpose({ scrollEvents: () => { eventsSection.value?.scrollIntoView({ behavior: 'smooth', block: 'start' }); eventsSection.value?.focus({ preventScroll: true }) } })
</script>

<template>
  <aside class="doc-rail" aria-label="文档详情">
    <section v-if="headings.length >= 2">
      <h4>本页目录</h4>
      <nav class="toc" aria-label="本页目录">
        <a v-for="heading in headings" :key="heading.id" :href="`#${heading.id}`" :class="{ active: heading.id === activeHeading, subheading: heading.level === 3 }" :aria-current="heading.id === activeHeading ? 'location' : undefined" @click.prevent="emit('heading', heading.id)">{{ heading.text }}</a>
      </nav>
    </section>
    <section>
      <h4>分支版本</h4>
      <div class="versions">
        <button v-for="version in content.versions || []" :key="version.id" class="version" type="button" :disabled="version.id === content.version_id" :title="version.file_path" @click="emit('version', version.id)">
          <span class="version-head"><BranchTag :label="version.branch" /><StatusPill :status="version.status" /><span v-if="version.id === content.version_id" class="current">当前</span></span>
          <small>{{ shortSha(version.last_commit_sha) }} · {{ formatTime(version.last_commit_time) }}</small>
        </button>
        <p v-if="!content.versions?.length">暂无分支版本</p>
      </div>
    </section>
    <section ref="eventsSection" class="path-events" tabindex="-1">
      <h4>路径事件</h4>
      <Notice v-if="eventsError" variant="warn">路径事件加载失败：{{ eventsError }}</Notice>
      <p v-else-if="eventsLoading">正在加载路径事件</p>
      <p v-else-if="!events.length">没有删除、移动或重命名记录</p>
      <div v-for="event in events" :key="event.id" class="path-event">
        <div><StatusPill :status="event.event_type" /><BranchTag :label="event.branch" neutral /></div>
        <code>{{ event.old_path && event.new_path ? `${event.old_path} → ${event.new_path}` : event.old_path || event.new_path }}</code>
        <small>{{ shortSha(event.commit_sha) }} · {{ formatTime(event.commit_time) }}</small>
      </div>
    </section>
  </aside>
</template>

<style scoped>
.doc-rail { position: sticky; top: 0; align-self: start; min-width: 0; max-height: 100%; overflow-y: auto; border-left: 1px solid var(--border); padding: 20px 16px; display: flex; flex-direction: column; gap: 24px; font-size: var(--fs-sm); }
h4 { margin: 0 0 8px; font-size: var(--fs-xs); font-weight: 600; color: var(--fg-subtle); }
p { margin: 0; color: var(--fg-subtle); }
.toc { display: flex; flex-direction: column; gap: 1px; border-left: 1px solid var(--border); }
.toc a { padding: 3px 0 3px 12px; margin-left: -1px; border-left: 2px solid var(--transparent); color: var(--fg-muted); line-height: 1.45; text-decoration: none; overflow-wrap: anywhere; }
.toc a:hover { color: var(--fg); }
.toc a.active { color: var(--accent-text); border-left-color: var(--accent); font-weight: 600; }
.toc a.subheading { padding-left: 22px; }
.versions { display: flex; flex-direction: column; gap: 10px; }
.version { display: flex; flex-direction: column; gap: 4px; width: 100%; min-width: 0; text-align: left; padding: 0; border: 0; background: none; color: var(--fg); }
.version:disabled { opacity: 1; cursor: default; }
.version-head, .path-event > div { display: flex; flex-wrap: wrap; align-items: center; gap: 6px; min-width: 0; }
.version-head :deep(.branch-tag), .path-event :deep(.branch-tag) { max-width: 100%; }
.current { font-size: var(--fs-xs); color: var(--ok); background: var(--ok-bg); border-radius: 4px; padding: 1px 5px; }
small { color: var(--fg-subtle); font-size: var(--fs-xs); font-variant-numeric: tabular-nums; overflow-wrap: anywhere; }
.path-events { scroll-margin-top: 16px; }
.path-event { display: flex; flex-direction: column; gap: 5px; margin-bottom: 12px; }
.path-event code { font-family: var(--font-mono); font-size: var(--fs-xs); overflow-wrap: anywhere; }
@media (max-width: 1280px) { .doc-rail { position: static; max-height: none; overflow: visible; border-left: 0; border-top: 1px solid var(--border); padding: 24px 32px; } }
@media (max-width: 860px) { .doc-rail { padding: 20px 16px max(24px, env(safe-area-inset-bottom)); } }
</style>
