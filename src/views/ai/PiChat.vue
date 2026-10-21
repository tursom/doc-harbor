<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue'
import { MessageSquare, Sparkles } from 'lucide-vue-next'
import Notice from '../../components/ui/Notice.vue'
import { usePiChat } from '../../composables/usePiChat'
import type { AIQuestionScope, AISession } from '../../types'
import type { PiRole, PiTurn } from '../../ai-pi-types'
import Composer from './Composer.vue'
import PiEvidencePanel from './PiEvidencePanel.vue'
import PiTurnItem from './PiTurnItem.vue'
import { ROLE_STORAGE_KEY, roleExamples } from './pi-chat'

const props = defineProps<{
  session: AISession | null
  question: string
  scope: 'global' | 'current_repo' | 'current_file'
  repoName?: string
  filePath?: string
  canAskFile: boolean
  enabled: boolean
  contextLoading: boolean
  diagnosticsHref: string
  buildScope: () => AIQuestionScope
  ensureSession: () => Promise<AISession | null | undefined>
}>()
const emit = defineEmits<{
  'update:question': [text: string]
  'update:scope': [scope: 'global' | 'current_repo' | 'current_file']
  sessions: []
  completed: []
}>()
const chat = usePiChat()
const { turns, loading, error, busy, activeTurn, operating } = chat
const role = ref<PiRole>('未指定')
const selectedRun = ref<number | null>(null)
const panelTurn = computed(() => turns.value.find(t => t.run_id === selectedRun.value) || turns.value.at(-1))
const thread = ref<HTMLElement | null>(null)
const renderer = shallowRef<((content: string) => string) | null>(null)
const preparing = ref(false)
let disposed = false
// 存储可能被浏览器禁用，角色功能仍可在本页正常使用。
onMounted(async () => {
  try {
    const saved = localStorage.getItem(ROLE_STORAGE_KEY)
    if (saved && ['开发', '产品', '测试', '未指定'].includes(saved)) role.value = saved as PiRole
  } catch { /* 禁用持久化时保留未指定。 */ }
  try {
    const [{ marked }, { default: DOMPurify }] = await Promise.all([import('marked'), import('dompurify')])
    if (!disposed) renderer.value = text => DOMPurify.sanitize(marked.parse(text, { async: false }) as string)
  } catch { /* Markdown 加载失败时仍展示纯文本回答。 */ }
})
onBeforeUnmount(() => { disposed = true })
watch(role, value => { try { localStorage.setItem(ROLE_STORAGE_KEY, value) } catch { /* 不影响提问。 */ } })
watch(() => props.session?.id, id => { selectedRun.value = null; void chat.openSession(id || null) }, { immediate: true })
watch(() => turns.value.length, () => { selectedRun.value = null })
// 只有用户在底部附近时跟随流式文本，允许向上复核历史。
watch(() => turns.value.at(-1)?.answer, async () => {
  const el = thread.value
  if (!el || el.scrollHeight - el.scrollTop - el.clientHeight > 160) return
  await nextTick()
  el.scrollTop = el.scrollHeight
})
watch(() => props.session?.id, async () => { await chat.ready(); await nextTick(); if (thread.value) thread.value.scrollTop = thread.value.scrollHeight })

async function send(retry?: PiTurn) {
  if (disposed || !props.enabled || props.contextLoading || preparing.value || operating.value || loading.value) return
  const text = retry?.question || props.question.trim()
  if (!text) return
  if (activeTurn.value) {
    if (!retry && await chat.steer(text)) emit('update:question', '')
    return
  }
  preparing.value = true
  const expectedID = props.session?.id
  try {
    if (!props.session) {
      const session = await props.ensureSession()
      if (!session || disposed) return
      await nextTick(); await chat.ready()
      // 配置变化时，新会话的引擎可能已不同，由父页面选择对应组件。
      if (session.engine !== 'pi') return
    } else if (props.session.id !== expectedID) return
    if (disposed) return
    if (!retry) emit('update:question', '')
    const task = chat.sendQuestion(text, retry?.role || role.value, retry?.scope || props.buildScope())
    preparing.value = false
    await task
    if (!disposed) emit('completed')
  } finally { preparing.value = false }
}
function retry(turn: PiTurn) {
  // “缩小范围”保留原问题供编辑；普通错误直接以原角色与范围重试。
  if (turn.status === 'timeout' || /工具.*(?:过多|上限|超限)|检索步骤过多|tool.*limit/i.test(turn.error_message || '')) {
    emit('update:question', turn.question)
    nextTick(() => thread.value?.parentElement?.querySelector('textarea')?.focus())
  } else void send(turn)
}
defineExpose({ refresh: chat.refresh })
</script>

<template>
  <section class="pi-chat" aria-label="pi AI 对话">
    <div class="mobile-sessions"><button class="btn sm" type="button" @click="$emit('sessions')"><MessageSquare :size="14" />会话</button><span>{{ session?.title || '新的 AI 问答' }}</span></div>
    <div v-if="error" class="pi-error"><Notice variant="danger">{{ error }} <button class="btn sm" type="button" :disabled="busy" @click="chat.refresh">重新读取</button></Notice></div>
    <div ref="thread" class="pi-thread" :aria-busy="loading">
      <div class="pi-thread-inner">
        <PiTurnItem v-for="turn in turns" :key="turn.run_id" :turn="turn" :renderer="renderer" :repo-name="repoName" :diagnostics-href="diagnosticsHref" :can-retry="enabled && !busy && !preparing" :feedback="chat.feedback" @retry="retry" @focus="selectedRun = turn.run_id" />
        <p v-if="loading && !turns.length" class="loading" role="status">正在读取会话…</p>
        <div v-else-if="!turns.length" class="pi-empty">
          <Sparkles :size="28" /><h3>从文档和代码中找到答案</h3><p>助手会查阅原文，展示过程，并为关键结论标注引用。</p>
          <button v-for="example in roleExamples[role]" :key="example" type="button" :disabled="!enabled" @click="$emit('update:question', example)">{{ example }}</button>
        </div>
      </div>
    </div>
    <Composer v-if="enabled" :question="question" :scope="scope" :include-branches="false" :repo-name="repoName" :file-path="filePath" :can-ask-file="canAskFile" :busy="busy || preparing || operating || contextLoading" :streaming="!!activeTurn" pi :role="role" @update:question="$emit('update:question', $event)" @update:scope="$emit('update:scope', $event)" @update:role="role = $event" @send="send()" @stop="chat.stop" />
  </section>
  <PiEvidencePanel :turn="panelTurn" :turns="turns" @select="selectedRun = $event" />
</template>

<style scoped>
.pi-chat { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
.pi-thread { flex: 1; overflow-y: auto; padding: 24px 32px; min-height: 180px; }
.pi-thread-inner { max-width: 720px; margin: 0 auto; display: flex; flex-direction: column; gap: 28px; }
.pi-error { padding: 12px 24px 0; }
.loading { color: var(--fg-muted); text-align: center; font-size: var(--fs-sm); }
.pi-empty { padding: 36px 0; display: flex; flex-direction: column; gap: 10px; align-items: center; color: var(--fg-muted); text-align: center; }
.pi-empty > svg { color: var(--accent-text); }
.pi-empty h3 { margin: 8px 0 0; color: var(--fg); font-size: var(--fs-xl); }
.pi-empty p { margin: 0 0 14px; line-height: 1.8; font-size: var(--fs-sm); }
.pi-empty button { width: 100%; max-width: 480px; padding: 13px 16px; border: 1px solid var(--border); border-radius: 8px; background: var(--surface); color: var(--fg-muted); text-align: left; font: var(--fs-md)/1.6 var(--font-ui); cursor: pointer; overflow-wrap: anywhere; }
.pi-empty button:hover { background: var(--surface-2); border-color: var(--border-strong); color: var(--accent-text); }
.mobile-sessions { display: none; }
@media (max-width: 1280px) { .pi-thread { padding: 24px; } }
@media (max-width: 1080px) { .mobile-sessions { display: flex; align-items: center; gap: 10px; padding: 8px 16px; border-bottom: 1px solid var(--border); min-width: 0; } .mobile-sessions button { flex-shrink: 0; } .mobile-sessions span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--fg-muted); font-size: var(--fs-sm); } }
@media (max-width: 860px) { .pi-chat { min-height: 65dvh; } .pi-thread { overflow: visible; padding: 20px max(16px, env(safe-area-inset-right, 0px)) 20px max(16px, env(safe-area-inset-left, 0px)); } .pi-empty { padding: 24px 0; } }
</style>
