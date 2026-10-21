<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue'
import { MessageSquare } from 'lucide-vue-next'
import DrawerPanel from '../components/ui/DrawerPanel.vue'
import EmptyState from '../components/ui/EmptyState.vue'
import Notice from '../components/ui/Notice.vue'
import StatusPill from '../components/ui/StatusPill.vue'
import { useRepos } from '../composables/useRepos'
import { useAIChat } from '../composables/useAIChat'
import type { AISession } from '../types'
import Composer from './ai/Composer.vue'
import EvidencePanel from './ai/EvidencePanel.vue'
import MessageItem from './ai/MessageItem.vue'
import SessionList from './ai/SessionList.vue'
import PiChat from './ai/PiChat.vue'
import { messageNotice } from './ai/chat'

const { repos, selectedRepo, loadAll, tabHref } = useRepos()
const {
  sessions, selectedSession, messages, settings, question, scopeMode, includeBranchCandidates, fileContent,
  loading, streaming, busy, error, candidates, numberedCitations, evidenceChain, notices, messageListRef,
  markdownRenderer, canAskWithCurrentFile, initialize, refresh, selectSession, newSession, sendQuestion,
  stopGeneration, openCitation, syncURL, isPi, legacyReadOnly, buildScope
} = useAIChat(repos, selectedRepo)
const sessionsOpen = ref(false)
const evidencePanel = ref<InstanceType<typeof EvidencePanel> | null>(null)
const piChat = ref<InstanceType<typeof PiChat> | null>(null)
let disposed = false

async function refreshPage() {
  await refresh()
  await piChat.value?.refresh()
}

async function chooseSession(session: AISession) {
  sessionsOpen.value = false
  await selectSession(session)
}
async function createSession() {
  sessionsOpen.value = false
  await newSession()
}
// Shell 的仓库加载准备好 branches 后才发布 selectedRepo；等待共享 loadAll 后恢复文件范围。
onMounted(async () => {
  await loadAll()
  if (disposed) return
  window.addEventListener('popstate', initialize)
  await initialize()
})
onBeforeUnmount(() => {
  disposed = true
  stopGeneration()
  window.removeEventListener('popstate', initialize)
})
</script>

<template>
  <section class="ai-page">
    <header class="ai-header">
      <h2>AI 问答</h2>
      <div class="ai-subtitle"><StatusPill :status="settings?.enabled ? 'enabled' : 'disabled'" /><span>{{ settings?.default_provider_name || '未配置供应商' }} · <span class="model">{{ settings?.default_model || '—' }}</span></span><span class="description">在全部仓库的文档和代码中查阅原文，回答附带引用</span></div>
    </header>
    <div v-if="settings && !settings.enabled" class="page-error"><Notice variant="warn">AI 问答未启用。<a :href="tabHref('ai-config')">前往 AI 供应商配置并启用</a></Notice></div>
    <div v-if="error" class="page-error"><Notice variant="danger">{{ error }} <button class="btn sm" type="button" :disabled="busy" @click="refreshPage">重试</button></Notice></div>
    <div class="ai-layout">
      <aside class="sessions-sidebar"><SessionList :sessions="sessions" :selected-id="selectedSession?.id" :busy="busy" @select="chooseSession" @create="createSession" @refresh="refreshPage" /></aside>
      <PiChat v-if="isPi" ref="piChat" :session="selectedSession" v-model:question="question" v-model:scope="scopeMode" :repo-name="selectedRepo?.name" :file-path="fileContent?.file_path" :can-ask-file="canAskWithCurrentFile" :enabled="!!settings?.enabled" :context-loading="loading" :diagnostics-href="tabHref('ai-diagnostics')" :build-scope="buildScope" :ensure-session="newSession" @sessions="sessionsOpen = true" @completed="refresh" @update:scope="syncURL" />
      <section v-else class="chat" aria-label="AI 对话">
        <div class="mobile-sessions"><button class="btn sm" type="button" @click="sessionsOpen = true"><MessageSquare :size="14" />会话</button><span>{{ selectedSession?.title || '新的 AI 问答' }}</span></div>
        <div v-if="legacyReadOnly" class="legacy-readonly"><Notice>旧版会话仅供查阅。<button class="btn sm" type="button" :disabled="busy" @click="createSession">新建 pi 问答</button></Notice></div>
        <div ref="messageListRef" class="thread" :aria-busy="loading">
          <div class="thread-inner">
            <MessageItem v-for="message in messages" :key="message.id" :message="message" :notice="messageNotice(message, notices)" :citations="numberedCitations" :renderer="markdownRenderer" :streaming="streaming" :diagnostics-href="tabHref('ai-diagnostics')" @citation="evidencePanel?.focusCitation($event)" @stop="stopGeneration" />
            <EmptyState v-if="!messages.length" :message="loading ? '正在读取会话…' : '从全部仓库提问，回答会附带文档和代码引用'" />
          </div>
        </div>
        <Composer v-if="!legacyReadOnly && settings?.enabled" v-model:question="question" v-model:scope="scopeMode" v-model:include-branches="includeBranchCandidates" :repo-name="selectedRepo?.name" :file-path="fileContent?.file_path" :can-ask-file="canAskWithCurrentFile" :busy="busy" :streaming="streaming" @send="sendQuestion" @stop="stopGeneration" @update:scope="syncURL" />
      </section>
      <EvidencePanel v-if="!isPi" ref="evidencePanel" :citations="numberedCitations" :candidates="candidates" :steps="evidenceChain" :streaming="streaming" :repos="repos" @open="openCitation" />
    </div>
    <DrawerPanel :open="sessionsOpen" title="会话" @close="sessionsOpen = false"><SessionList :sessions="sessions" :selected-id="selectedSession?.id" :busy="busy" @select="chooseSession" @create="createSession" @refresh="refreshPage" /></DrawerPanel>
  </section>
</template>

<style scoped>
.ai-page { display: flex; flex-direction: column; min-height: 0; min-width: 0; flex: 1; height: 100dvh; background: var(--surface); color: var(--fg); }
.ai-header { padding: 14px 24px; border-bottom: 1px solid var(--border); }
.ai-header h2 { margin: 0 0 4px; font-size: var(--fs-xl); }
.ai-subtitle { display: flex; align-items: center; flex-wrap: wrap; gap: 8px 12px; color: var(--fg-muted); font-size: var(--fs-sm); overflow-wrap: anywhere; }
.model { font-family: var(--font-mono); }
.description { color: var(--fg-subtle); }
.ai-subtitle a { color: var(--accent-text); }
.page-error { padding: 10px 24px; }
.page-error a { color: var(--accent-text); }
.legacy-readonly { padding: 12px 24px 0; }
.ai-layout { display: grid; grid-template-columns: 240px minmax(0, 1fr) 320px; flex: 1; min-height: 0; }
.sessions-sidebar { border-right: 1px solid var(--border); min-height: 0; min-width: 0; }
.chat { display: flex; flex-direction: column; min-width: 0; min-height: 0; }
.thread { flex: 1; overflow-y: auto; padding: 24px 32px; min-height: 200px; }
.thread-inner { max-width: 720px; margin: 0 auto; display: flex; flex-direction: column; gap: 22px; }
.mobile-sessions { display: none; }
@media (max-width: 1280px) { .ai-layout { grid-template-columns: 220px minmax(0, 1fr) 280px; } .thread { padding: 24px; } }
@media (max-width: 1080px) {
  .ai-layout { grid-template-columns: minmax(0, 1fr) 280px; }
  .sessions-sidebar { display: none; }
  .mobile-sessions { display: flex; align-items: center; gap: 10px; padding: 8px 16px; border-bottom: 1px solid var(--border); min-width: 0; }
  .mobile-sessions button { flex-shrink: 0; }
  .mobile-sessions span { overflow: hidden; text-overflow: ellipsis; white-space: nowrap; color: var(--fg-muted); font-size: var(--fs-sm); }
}
@media (max-width: 860px) {
  .ai-page { height: auto; min-height: 100%; }
  .ai-header { padding: 14px max(16px, env(safe-area-inset-right, 0px)) 14px max(16px, env(safe-area-inset-left, 0px)); }
  .ai-layout { display: flex; flex-direction: column; }
  .chat { min-height: 65dvh; }
  .thread { overflow: visible; padding: 20px max(16px, env(safe-area-inset-right, 0px)) 20px max(16px, env(safe-area-inset-left, 0px)); }
  .page-error { padding: 10px 16px; }
}
</style>
