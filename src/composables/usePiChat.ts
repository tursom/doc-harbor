import { computed, getCurrentScope, onScopeDispose, reactive, ref } from 'vue'
import { piApi } from '../api-pi'
import { APIRequestError } from '../api'
import type { PiRole, PiStreamEvent, PiTurn, PiUsage } from '../ai-pi-types'
import type { AIQuestionScope } from '../types'
import { isActive } from '../views/ai/pi-chat'

const emptyUsage = (): PiUsage => ({ input_tokens: 0, output_tokens: 0, cache_read_tokens: 0 })
const errorText = (err: unknown) => err instanceof Error ? err.message : String(err)

/** pi 会话只在显式点击停止时 abort；切页只关闭 SSE，服务端继续生成。 */
export function usePiChat() {
  const turns = ref<PiTurn[]>([])
  const loading = ref(false)
  const error = ref('')
  const operating = ref(false)
  const streaming = ref(false)
  const activeTurn = computed(() => [...turns.value].reverse().find(isActive))
  const busy = computed(() => loading.value || streaming.value || !!activeTurn.value)
  let sessionID: number | null = null
  let generation = 0
  let localID = 0
  let streamController: AbortController | null = null
  let readController: AbortController | null = null
  let pollTimer: ReturnType<typeof setTimeout> | undefined
  let currentLoad: Promise<void> = Promise.resolve()

  function disconnect() {
    generation++
    clearTimeout(pollTimer)
    streamController?.abort(); readController?.abort()
    streamController = null; readController = null
    streaming.value = false; loading.value = false; operating.value = false
  }
  if (getCurrentScope()) onScopeDispose(disconnect)

  /** 串行轮询，失败后继续恢复；只要最新历史仍 running/queued 就不停止轮询。 */
  function schedulePoll(request: number) {
    clearTimeout(pollTimer)
    if (request !== generation || !activeTurn.value || streaming.value) return
    pollTimer = setTimeout(() => { void readTranscript(request) }, 1500)
  }
  async function readTranscript(request: number) {
    if (!sessionID || request !== generation) return
    const controller = new AbortController()
    readController?.abort(); readController = controller
    try {
      const response = await piApi.transcript(sessionID, controller.signal)
      if (request !== generation || controller.signal.aborted) return
      const previous = turns.value
      const incoming = (response.turns || []).map(turn => {
        const localSupplements = previous.find(t => t.run_id === turn.run_id)?.supplements
        return { ...turn, steps: (turn.steps || []).map(step => ({ ...step, running: isActive(turn) && !step.summary && !step.preview })), citations: turn.citations || [], usage: turn.usage || emptyUsage(), supplements: (turn.supplements?.length || 0) >= (localSupplements?.length || 0) ? turn.supplements : localSupplements }
      })
      // run 的入库与 SSE 有短暂时差；不能用空历史抹掉已收到的流式正文。
      const last = previous.at(-1)
      if (!last || last.run_id <= 0 || incoming.some(t => t.run_id === last.run_id)) turns.value = incoming.length ? incoming : previous.filter(t => t.run_id > 0)
      error.value = ''
    } catch (err) {
      if (request === generation && !controller.signal.aborted) error.value = `读取会话失败：${errorText(err)}`
    } finally {
      if (request === generation && !controller.signal.aborted) schedulePoll(request)
    }
  }
  function openSession(id: number | null) {
    disconnect()
    sessionID = id
    turns.value = []; error.value = ''
    if (!id) { currentLoad = Promise.resolve(); return currentLoad }
    const request = generation
    loading.value = true
    currentLoad = readTranscript(request).finally(() => { if (request === generation) loading.value = false })
    return currentLoad
  }
  async function refresh() {
    if (streaming.value) return
    await readTranscript(generation)
  }

  function applyEvent(turn: PiTurn, event: PiStreamEvent) {
    switch (event.type) {
      case 'queued': turn.status = 'queued'; turn.queue_position = event.position; break
      case 'started': turn.run_id = event.run_id; turn.status = 'running'; turn.started_at = new Date().toISOString(); turn.notice = ''; break
      case 'text': turn.answer += event.delta; break
      case 'tool_start':
        turn.steps.push({ call_id: event.call_id, tool: event.tool, args: event.args || {}, summary: '', is_error: false, preview: '', running: true })
        break
      case 'tool_end': {
        const step = turn.steps.find(s => s.call_id === event.call_id)
        if (step) Object.assign(step, { summary: event.summary, is_error: event.is_error, preview: event.preview, running: false })
        break
      }
      case 'notice': turn.notice = event.message; break
      case 'error': turn.error_message = event.message; turn.status = 'failed'; break
      case 'done':
        if (event.run_id) turn.run_id = event.run_id
        turn.status = event.status; turn.citations = event.citations || []; turn.usage = event.usage || emptyUsage()
        if (event.message) turn.error_message = event.message
        turn.finished_at = new Date().toISOString(); turn.tool_calls = turn.steps.length; turn.notice = ''
        for (const step of turn.steps) step.running = false
        break
    }
  }

  async function sendQuestion(question: string, role: PiRole, scope: AIQuestionScope) {
    await currentLoad
    if (!sessionID || busy.value || !question.trim()) return
    const request = generation
    const id = sessionID
    clearTimeout(pollTimer)
    error.value = ''
    const turn = reactive<PiTurn>({ run_id: --localID, question: question.trim(), role, scope, status: 'queued', steps: [], answer: '', citations: [], usage: emptyUsage(), feedback: null })
    turns.value.push(turn)
    const controller = new AbortController()
    streamController = controller; streaming.value = true
    let ended = false
    try {
      await piApi.streamRun(id, { question: question.trim(), role, scope }, event => {
        if (request !== generation) return
        applyEvent(turn, event)
        if (event.type === 'done') ended = true
      }, controller.signal)
      if (request !== generation) return
      if (!ended && isActive(turn)) turn.notice = '连接已断开，正在同步后台运行结果…'
    } catch (err) {
      if (request !== generation || controller.signal.aborted) return
      if (err instanceof APIRequestError && err.status === 409) {
        // 另一个标签页可能在本地读取历史之后启动了同一会话；以后台运行
        // 为准，把这次输入转为补充说明，不能并发重发一个新 run。
        turns.value = turns.value.filter(item => item !== turn)
        await readTranscript(request)
        if (request !== generation) return
        if (activeTurn.value) {
          if (!await steer(question)) { turn.status = 'failed'; turn.error_message = error.value || errorText(err) }
          return
        }
        turns.value.push(turn)
      }
      if (turn.run_id > 0) turn.notice = '连接已断开，正在同步后台运行结果…'
      else { turn.status = 'failed'; turn.error_message = errorText(err) }
    } finally {
      if (request === generation) {
        streamController = null
        // EOF、网络错误和正常完成均从服务端对账，同时补齐模型、耗时等元数据。
        // 对账期间仍占用发送状态，避免旧历史覆盖刚排队、尚未分配 run_id 的下一问。
        const failedLocal = turn.run_id < 0 && turn.status === 'failed' ? turn : null
        await readTranscript(request)
        if (request === generation) {
          if (failedLocal && !turns.value.some(t => t === failedLocal || t.question === failedLocal.question && isActive(t))) turns.value.push(failedLocal)
          streaming.value = false
          schedulePoll(request)
        }
      }
    }
  }

  async function steer(text: string) {
    if (!sessionID || !activeTurn.value || operating.value || !text.trim()) return false
    const request = generation
    const target = activeTurn.value
    operating.value = true; error.value = ''
    try {
      await piApi.steer(sessionID, text.trim())
      if (request !== generation) return false
      const current = turns.value.find(turn => turn.run_id === target.run_id) || target
      current.supplements = [...(current.supplements || []), text.trim()]
      current.notice = '补充说明已发送，会在当前步骤结束后交给助手。'
      return true
    } catch (err) { if (request === generation) error.value = errorText(err); return false }
    finally { if (request === generation) operating.value = false }
  }
  async function stop() {
    if (!sessionID || !activeTurn.value || operating.value) return
    const request = generation
    operating.value = true; error.value = ''
    try {
      await piApi.abort(sessionID)
      if (request !== generation) return
      if (activeTurn.value) activeTurn.value.notice = '正在停止…'
      if (!streaming.value) await readTranscript(request)
    } catch (err) { if (request === generation) error.value = errorText(err) }
    finally { if (request === generation) operating.value = false }
  }
  async function feedback(turn: PiTurn, rating: 1 | -1, comment = '') {
    if (turn.run_id <= 0) return false
    const request = generation
    try {
      await piApi.feedback(turn.run_id, { rating, ...(comment.trim() ? { comment: comment.trim() } : {}) })
      if (request !== generation) return false
      turn.feedback = { rating, comment: comment.trim() }
      // 历史同步可能已替换展示对象，按稳定 run_id 同步当前列表。
      const current = turns.value.find(item => item.run_id === turn.run_id)
      if (current) current.feedback = turn.feedback
      return true
    } catch (err) { if (request === generation) error.value = `反馈保存失败：${errorText(err)}`; return false }
  }
  return { turns, loading, error, busy, operating, streaming, activeTurn, openSession, ready: () => currentLoad, refresh, sendQuestion, steer, stop, feedback, disconnect }
}
