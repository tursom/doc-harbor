<script setup lang="ts">
import { computed } from 'vue'
import { Plus, RefreshCw } from 'lucide-vue-next'
import type { AISession } from '../../types'
import { formatTime } from '../../lib/format'
import EmptyState from '../../components/ui/EmptyState.vue'
import { groupSessions, sessionTime } from './chat'

const props = defineProps<{ sessions: AISession[]; selectedId?: number; busy: boolean }>()
defineEmits<{ select: [session: AISession]; create: []; refresh: [] }>()
const groups = computed(() => groupSessions(props.sessions))
</script>

<template>
  <section class="session-pane" aria-label="AI 会话列表">
    <header class="session-head">
      <h3>会话</h3>
      <button class="btn ghost icon sm" type="button" aria-label="刷新 AI 问答" :disabled="busy" @click="$emit('refresh')"><RefreshCw :size="13" /></button>
      <button class="btn sm" type="button" :disabled="busy" @click="$emit('create')"><Plus :size="13" />新会话</button>
    </header>
    <div class="session-list">
      <section v-for="group in groups" :key="group.day" :aria-label="group.day">
        <h4 class="session-day">{{ group.day }}</h4>
        <button v-for="session in group.items" :key="session.id" class="session-row" :class="{ active: selectedId === session.id }" type="button" :aria-current="selectedId === session.id ? 'true' : undefined" @click="$emit('select', session)">
          <span :title="session.title">{{ session.title || '新的 AI 问答' }}</span>
          <time :datetime="session.updated_at" :title="formatTime(session.updated_at)">{{ sessionTime(session.updated_at) }}</time>
        </button>
      </section>
      <EmptyState v-if="!sessions.length" :message="busy ? '正在读取会话…' : '暂无问答历史'" />
    </div>
  </section>
</template>

<style scoped>
.session-pane { display: flex; flex-direction: column; min-height: 0; height: 100%; min-width: 0; }
.session-head { display: flex; align-items: center; gap: 4px; padding: 12px 10px 8px 16px; }
.session-head h3 { flex: 1; margin: 0; color: var(--fg-muted); font-size: var(--fs-sm); }
.session-list { flex: 1; overflow-y: auto; padding: 0 6px 12px; }
.session-day { margin: 0; padding: 12px 10px 4px; font-size: var(--fs-xs); color: var(--fg-subtle); }
.session-row { display: flex; flex-direction: column; gap: 1px; width: 100%; padding: 7px 10px; border: 0; border-radius: 6px; background: none; color: var(--fg); text-align: left; cursor: pointer; }
.session-row:hover { background: var(--surface-2); }
.session-row.active { background: var(--surface-3); }
.session-row span { max-width: 100%; overflow: hidden; text-overflow: ellipsis; white-space: nowrap; }
.session-row time { color: var(--fg-subtle); font-size: var(--fs-xs); font-variant-numeric: tabular-nums; }
.session-row:focus-visible { outline: 2px solid var(--accent); outline-offset: -2px; }
</style>
