import { computed, nextTick, ref, shallowRef, type Ref } from 'vue'
import { api } from '../api'
import type { AIMessage, AIMessageCitation, AINotice, AIQuestionScope, AIServiceCandidate, AISettingsSummary, AIStreamEvent, AISession, FileContent, Repository } from '../types'
import { coverageDeltaText, coverageStatus, numberCitations, type EvidenceStep } from '../views/ai/chat'

/** 每个 AI 页面实例独立管理会话。取消/切页时递增代数，阻止晚到响应污染新会话。 */
export function useAIChat(repos: Ref<Repository[]>, selectedRepo: Ref<Repository | null>) {
  const sessions = ref<AISession[]>([])
  const selectedSession = ref<AISession | null>(null)
  const messages = ref<AIMessage[]>([])
  const settings = ref<AISettingsSummary | null>(null)
  const question = ref('')
  const scopeMode = ref<'global' | 'current_repo' | 'current_file'>('global')
  const includeBranchCandidates = ref(true)
  const fileContent = ref<FileContent | null>(null)
  const loading = ref(false)
  const streaming = ref(false)
  const error = ref('')
  const candidates = ref<AIServiceCandidate[]>([])
  const citations = ref<AIMessageCitation[]>([])
  const evidenceChain = ref<EvidenceStep[]>([])
  const notices = ref<Record<number, AINotice>>({})
  const messageListRef = ref<HTMLElement | null>(null)
  // 初始不渲染原始 HTML；Markdown 依赖只在客户端需要回答时加载。
  const markdownRenderer = shallowRef<((content: string) => string) | null>(null)
  const numberedCitations = computed(() => numberCitations(citations.value))
  const canAskWithCurrentFile = computed(() => Boolean(fileContent.value && fileContent.value.file_size <= 1024 * 1024))
  const busy = computed(() => loading.value || streaming.value)
  let generation = 0
  let stepSequence = 0
  let controller: AbortController | null = null

  function clearConversation() {
    messages.value = []
    candidates.value = []
    citations.value = []
    evidenceChain.value = []
    notices.value = {}
  }

  function stopGeneration() {
    generation++
    controller?.abort()
    controller = null
    if (streaming.value) {
      for (const message of messages.value) {
        // 中止只确认浏览器停止接收，不能声称后端已完整保存当前缓冲区。
        if (message.status === 'streaming') message.status = 'stopped'
      }
      for (const step of evidenceChain.value) {
        if (['running', 'started'].includes(step.status)) step.status = 'stopped'
      }
    }
    streaming.value = false
    loading.value = false
  }

  async function ensureMarkdown() {
    if (markdownRenderer.value) return
    const [{ marked }, { default: DOMPurify }] = await Promise.all([import('marked'), import('dompurify')])
    markdownRenderer.value = (content) => DOMPurify.sanitize(marked.parse(content || '', { async: false }) as string)
  }

  function syncURL() {
    const url = new URL(window.location.href)
    if (selectedSession.value) url.searchParams.set('session', String(selectedSession.value.id))
    else url.searchParams.delete('session')
    url.searchParams.set('scope', scopeMode.value === 'current_file' ? 'file' : scopeMode.value === 'current_repo' ? 'repo' : 'global')
    if (selectedRepo.value) url.searchParams.set('repo', String(selectedRepo.value.id))
    if (fileContent.value) url.searchParams.set('version', String(fileContent.value.version_id))
    url.searchParams.delete('question')
    if (url.href !== window.location.href) window.history.pushState(null, '', url)
  }

  async function loadMessages(sessionID: number, request: number) {
    const response = await api.aiMessages(sessionID)
    if (request !== generation) return
    messages.value = response.items || []
    candidates.value = response.service_candidates || []
    citations.value = response.citations || []
    await scrollToBottom()
  }

  /** URL 显式会话不受历史列表的 50 条窗口限制；即便不在列表也直接读取消息。 */
  async function initialize() {
    stopGeneration()
    const request = generation
    loading.value = true
    error.value = ''
    clearConversation()
    selectedSession.value = null
    fileContent.value = null
    const params = new URLSearchParams(window.location.search)
    question.value = params.get('question') || ''
    scopeMode.value = 'global'
    try {
      const [config, response] = await Promise.all([api.aiSettings(), api.aiSessions({ limit: '50' }), ensureMarkdown()])
      if (request !== generation) return
      settings.value = config
      sessions.value = response.items || []
      const repoID = Number(params.get('repo'))
      const versionID = Number(params.get('version'))
      if (repoID > 0 && versionID > 0) {
        const content = await api.content(repoID, versionID)
        if (request !== generation) return
        fileContent.value = content
      }
      const scope = params.get('scope')
      if ((scope === 'file' || scope === 'current_file') && canAskWithCurrentFile.value) {
        scopeMode.value = 'current_file'
        question.value = params.get('question') || `基于当前文件 ${fileContent.value!.file_path} 回答：`
      } else if ((scope === 'repo' || scope === 'current_repo') && selectedRepo.value) {
        scopeMode.value = 'current_repo'
      }
      const sessionID = Number(params.get('session'))
      const session = sessionID > 0 ? sessions.value.find((item) => item.id === sessionID) || {
        id: sessionID, title: `会话 #${sessionID}`, viewer_key: '', scope_json: '', created_at: '', updated_at: '', archived_at: ''
      } : sessions.value[0]
      if (session) {
        selectedSession.value = session
        await loadMessages(session.id, request)
      }
    } catch (err) {
      if (request === generation) error.value = errorText(err)
    } finally {
      if (request === generation) loading.value = false
    }
  }

  async function refresh() {
    if (busy.value) return
    const request = ++generation
    loading.value = true
    error.value = ''
    try {
      const [config, response] = await Promise.all([api.aiSettings(), api.aiSessions({ limit: '50' }), ensureMarkdown()])
      if (request !== generation) return
      settings.value = config
      sessions.value = response.items || []
      if (selectedSession.value) {
        selectedSession.value = sessions.value.find((item) => item.id === selectedSession.value?.id) || selectedSession.value
        await loadMessages(selectedSession.value.id, request)
      }
    } catch (err) {
      if (request === generation) error.value = errorText(err)
    } finally {
      if (request === generation) loading.value = false
    }
  }

  async function selectSession(session: AISession) {
    stopGeneration()
    const request = generation
    loading.value = true
    error.value = ''
    selectedSession.value = session
    clearConversation()
    syncURL()
    try {
      await Promise.all([ensureMarkdown(), loadMessages(session.id, request)])
    } catch (err) {
      if (request === generation) error.value = errorText(err)
    } finally {
      if (request === generation) loading.value = false
    }
  }

  async function createSession(request: number) {
    const session = await api.createAISession({ title: '新的 AI 问答', scope: buildScope() })
    if (request !== generation) return null
    selectedSession.value = session
    sessions.value = [session, ...sessions.value.filter((item) => item.id !== session.id)]
    clearConversation()
    syncURL()
    return session
  }

  async function newSession() {
    stopGeneration()
    const request = generation
    loading.value = true
    error.value = ''
    try {
      await createSession(request)
    } catch (err) {
      if (request === generation) error.value = errorText(err)
    } finally {
      if (request === generation) loading.value = false
    }
  }

  function buildScope(): AIQuestionScope {
    let mode = scopeMode.value
    if (mode === 'current_repo' && !selectedRepo.value) mode = 'global'
    if (mode === 'current_file' && !canAskWithCurrentFile.value) mode = 'global'
    const scope: AIQuestionScope = {
      repo_mode: mode, repo_ids: [], file_types: ['all'],
      source_mode: includeBranchCandidates.value ? 'smart_latest_with_branch_candidates' : 'smart_latest'
    }
    if (mode === 'current_repo' && selectedRepo.value) scope.repo_ids = [selectedRepo.value.id]
    if (mode === 'current_file' && fileContent.value) {
      const file = fileContent.value
      scope.repo_ids = [file.repo_id]
      scope.current_file = { repo_id: file.repo_id, version_id: file.version_id, branch: file.branch, commit_sha: file.source_commit_sha, file_path: file.file_path }
    }
    return scope
  }

  async function sendQuestion() {
    if (!question.value.trim() || busy.value) return
    const request = ++generation
    const text = question.value.trim()
    streaming.value = true
    error.value = ''
    try {
      await ensureMarkdown()
      if (request !== generation) return
      if (!selectedSession.value) await createSession(request)
      if (request !== generation || !selectedSession.value) return
      question.value = ''
      candidates.value = []
      evidenceChain.value = []
      syncURL()
      const streamController = new AbortController()
      controller = streamController
      await api.streamAI(selectedSession.value.id, text, buildScope(), async (event) => {
        if (request !== generation) return
        handleEvent(event)
        await scrollToBottom()
      }, streamController.signal)
      if (request !== generation) return
      // 正常 EOF 也可能缺少 message_done；不让已断开的连接继续显示“生成中”。
      for (const message of messages.value) {
        if (message.status === 'streaming') {
          message.status = message.content ? 'partial' : 'failed'
          message.error_message = '连接已结束，未收到完整回答。'
        }
      }
      finishSteps('stopped')
      const response = await api.aiSessions({ limit: '50' })
      if (request !== generation) return
      sessions.value = response.items || []
      selectedSession.value = sessions.value.find((item) => item.id === selectedSession.value?.id) || selectedSession.value
    } catch (err) {
      if (request !== generation || (err instanceof Error && err.name === 'AbortError')) return
      error.value = errorText(err)
      // 未收到 user_message 时保留问题，便于网络失败后重试。
      if (!messages.value.some((item) => item.role === 'user' && item.content === text)) question.value = text
      for (const message of messages.value) {
        if (message.status === 'streaming') {
          message.status = message.content ? 'partial' : 'failed'
          message.error_message = error.value
        }
      }
      finishSteps('failed')
    } finally {
      if (request === generation) {
        controller = null
        streaming.value = false
      }
    }
  }

  function upsertMessage(message: AIMessage) {
    const index = messages.value.findIndex((item) => item.id === message.id)
    if (index >= 0) messages.value[index] = message
    else messages.value.push(message)
  }

  function addStep(item: Omit<EvidenceStep, 'id'>) {
    evidenceChain.value.push({ id: String(++stepSequence), ...item })
  }

  function mergeCitations(items: AIMessageCitation[], messageID?: number) {
    const ids = new Set(items.map((item) => item.message_id))
    if (messageID) ids.add(messageID)
    // 保留同一会话旧回答的引用，让追问后仍可点击之前的 C1。
    citations.value = [...citations.value.filter((item) => !ids.has(item.message_id)), ...items]
  }

  function handleEvent(event: AIStreamEvent) {
    const stage = (title: string, detail: string, status = 'success', meta = '') => addStep({ kind: 'stage', title, detail, status, meta })
    switch (event.type) {
      case 'user_message':
        upsertMessage(event.message)
        stage('问题已入库', '用户问题已保存到当前会话。')
        break
      case 'run_started':
        upsertMessage(event.assistant_message)
        stage('运行已开始', `run #${event.run.id} 已创建，等待检索和模型路由。`, 'running')
        break
      case 'task_frame':
        stage('任务帧', `${event.task_frame.intent} · ${event.task_frame.answer_shape}`, 'success', event.task_frame.target_artifacts.slice(0, 4).join(' · '))
        break
      case 'contract':
        stage('证据契约', `${event.contract.contract_id} · required: ${event.contract.required_keys.join(', ')}`, 'success', event.contract.recommended_keys?.join(' · '))
        break
      case 'retrieval_round':
        stage(`检索 R${event.round.round}`, event.round.reason, 'success', `新增 ${event.round.new_evidence_count} · ${coverageDeltaText(event.round.coverage_delta)}`)
        break
      case 'coverage':
        stage('契约覆盖', `${event.coverage.status} · next: ${event.coverage.next_action}`, coverageStatus(event.coverage.status), `缺口 ${event.coverage.unconfirmed_count ?? event.coverage.missing_required?.length ?? 0} · 证据组 ${event.evidence_bundle?.group_count ?? 0}`)
        break
      case 'stage': {
        const title = ({ retrieve_smart_latest: '检索与证据召回', model_call: '模型调用', verify_answer: '答案校验' } as Record<string, string>)[event.stage] || event.stage || '阶段状态'
        // 同一阶段的结束事件回填进行中标记，避免完成后仍显示转圈。
        for (const item of evidenceChain.value) {
          if (item.title === title && ['running', 'started'].includes(item.status)) item.status = event.status
        }
        stage(title, event.message, event.status, [event.candidate_count ? `候选 ${event.candidate_count}` : '', event.evidence_count ? `引用 ${event.evidence_count}` : ''].filter(Boolean).join(' · '))
        break
      }
      case 'service_candidates': candidates.value = event.items || []; break
      case 'citations': mergeCitations(event.items || []); break
      case 'provider_attempt': {
        const title = event.provider || event.provider_key
        for (const item of evidenceChain.value) {
          if (item.kind === 'provider' && item.title === title && ['running', 'started'].includes(item.status)) item.status = event.status
        }
        const label = ({ started: '开始尝试', running: '进行中', succeeded: '成功', success: '成功', failed: '失败', skipped: '跳过' } as Record<string, string>)[event.status] || event.status
        addStep({ kind: 'provider', title, detail: event.error ? `${label}：${event.error}` : label, status: event.status, meta: `${event.model || '-'} · 优先级 ${event.priority}` })
        break
      }
      case 'verification':
        stage('答案校验', `${event.report.status} · ${event.report.reason || event.report.next_action}`, coverageStatus(event.report.status), event.report.failed_checks?.join(' · '))
        break
      case 'answer_delta': {
        let message = messages.value.find((item) => item.id === event.message_id)
        if (!message) {
          message = { id: event.message_id, session_id: selectedSession.value?.id || 0, role: 'assistant', content: '', model: '', provider_name: '', model_route_json: '', prompt_tokens: 0, completion_tokens: 0, latency_ms: 0, status: 'streaming', error_message: '', created_at: '' }
          messages.value.push(message)
          message = messages.value[messages.value.length - 1]!
        }
        message.content += event.delta
        message.status = 'streaming'
        break
      }
      case 'message_done':
        upsertMessage(event.message)
        candidates.value = event.service_candidates || []
        mergeCitations(event.citations || [], event.message.id)
        if (event.notice) notices.value[event.message.id] = event.notice
        finishSteps('success')
        stage('回答完成', `答案已保存，输入 ${event.usage?.prompt_tokens || 0} tokens，输出 ${event.usage?.completion_tokens || 0} tokens。`)
        break
      case 'error': {
        if (event.assistant_message) upsertMessage(event.assistant_message)
        else {
          const message = messages.value.find((item) => item.id === event.partial_message_id)
          if (message) {
            message.status = message.content ? 'partial' : 'failed'
            message.error_message = event.message
          }
        }
        error.value = event.message
        finishSteps('failed')
        stage('流式回答中断', event.message, 'failed')
        break
      }
      case 'done': break
    }
  }

  function finishSteps(status: string) {
    for (const item of evidenceChain.value) {
      if (['running', 'started'].includes(item.status)) item.status = status
    }
  }

  async function scrollToBottom() {
    await nextTick()
    if (messageListRef.value) messageListRef.value.scrollTop = messageListRef.value.scrollHeight
  }

  function openCitation(citation: AIMessageCitation) {
    if (!repos.value.some((repo) => repo.id === citation.repo_id)) {
      error.value = `未找到引用所属仓库：${citation.repo_name || citation.repo_id}`
      return
    }
    const dir = citation.file_path.split('/').slice(0, -1).join('/') || '.'
    const params = new URLSearchParams({ repo: String(citation.repo_id), view: 'latest', dir })
    if (citation.version_id > 0) params.set('version', String(citation.version_id))
    window.location.assign(`/?${params}`)
  }

  return {
    sessions, selectedSession, messages, settings, question, scopeMode, includeBranchCandidates, fileContent,
    loading, streaming, busy, error, candidates, citations, numberedCitations, evidenceChain, notices,
    messageListRef, markdownRenderer, canAskWithCurrentFile, initialize, refresh, selectSession, newSession,
    sendQuestion, stopGeneration, buildScope, openCitation, syncURL
  }
}

function errorText(err: unknown) { return err instanceof Error ? err.message : String(err) }
