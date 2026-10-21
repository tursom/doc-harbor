import type { AIMessage, AIMessageCitation, AINotice, AISession } from '../../types'

export type EvidenceTab = 'citations' | 'candidates' | 'trace'
export interface EvidenceStep {
  id: string
  kind: 'stage' | 'provider'
  title: string
  detail: string
  status: string
  meta?: string
}
export interface NumberedCitation {
  citation: AIMessageCitation
  number: number
  key: string
}

/** 后端按 Evidence 顺序逐条 INSERT，但历史接口按 score 排序。
 * 因此必须在每条回答内按自增 id 恢复原始顺序，不能使用响应数组下标或全会话序号。
 * 缺少持久化 id 的异常数据不参与编号，避免伪造可跳转的对应关系。
 */
export function numberCitations(citations: AIMessageCitation[]): NumberedCitation[] {
  const groups = new Map<number, AIMessageCitation[]>()
  for (const citation of citations) {
    if (citation.id <= 0 || citation.message_id <= 0) continue
    const group = groups.get(citation.message_id) || []
    if (!group.some((item) => item.id === citation.id)) group.push(citation)
    groups.set(citation.message_id, group)
  }
  return [...groups.entries()].sort(([a], [b]) => b - a).flatMap(([messageID, group]) =>
    [...group].sort((a, b) => a.id - b.id).map((citation, index) => ({
      citation, number: index + 1, key: `${messageID}:${citation.id}`
    }))
  )
}

/** 在已净化的 HTML 文本节点上装饰引用；不改代码块、链接或 HTML 属性。 */
export function decorateCitations(html: string, messageID: number, citations: NumberedCitation[]) {
  const template = document.createElement('template')
  template.innerHTML = html
  const walker = document.createTreeWalker(template.content, NodeFilter.SHOW_TEXT)
  const texts: Text[] = []
  while (walker.nextNode()) texts.push(walker.currentNode as Text)
  for (const text of texts) {
    if (text.parentElement?.closest('code, pre, a, button, script, style')) continue
    const matches = [...text.data.matchAll(/\[C([1-9]\d*)\]/g)]
    if (!matches.length) continue
    const fragment = document.createDocumentFragment()
    let offset = 0
    for (const match of matches) {
      fragment.append(text.data.slice(offset, match.index))
      const citation = citations.find((item) => item.citation.message_id === messageID && item.number === Number(match[1]))
      const badge = document.createElement(citation ? 'button' : 'span')
      badge.className = 'citation-mark'
      badge.textContent = match[0]
      if (citation) {
        badge.setAttribute('type', 'button')
        badge.dataset.citationKey = citation.key
        badge.setAttribute('aria-label', `查看引用 C${match[1]}`)
        badge.title = citation.citation.file_path
      }
      fragment.append(badge)
      offset = match.index! + match[0].length
    }
    fragment.append(text.data.slice(offset))
    text.replaceWith(fragment)
  }
  return template.innerHTML
}

export function shouldSendOnEnter(event: Pick<KeyboardEvent, 'key' | 'shiftKey' | 'isComposing' | 'keyCode'>, composing = false) {
  return event.key === 'Enter' && !event.shiftKey && !event.isComposing && event.keyCode !== 229 && !composing
}

/** 使用本地日期分组，与 HH:mm 和完整时间的显示时区一致。 */
export function groupSessions(sessions: AISession[]) {
  const groups = new Map<string, AISession[]>()
  const sorted = [...sessions].sort((a, b) => (Date.parse(b.updated_at) || 0) - (Date.parse(a.updated_at) || 0))
  for (const session of sorted) {
    const date = new Date(session.updated_at)
    const day = Number.isNaN(date.getTime()) ? '未知日期' :
      `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`
    groups.set(day, [...(groups.get(day) || []), session])
  }
  return [...groups].map(([day, items]) => ({ day, items }))
}

export function sessionTime(value: string) {
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '—' : date.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', hour12: false })
}

export function messageNotice(message: AIMessage, notices: Record<number, AINotice> = {}): AINotice | null {
  if (message.role !== 'assistant') return null
  if (notices[message.id]) return notices[message.id]
  try {
    const route = JSON.parse(message.model_route_json || '{}') as { reason?: string }
    if (route.reason === 'ai_disabled') return { type: 'ai_disabled', message: 'AI 问答尚未启用，已返回本地 Git 检索摘要。' }
    if (route.reason === 'provider_error') return { type: 'provider_error', message: '模型调用失败，已返回本地 Git 检索摘要。' }
    if (route.reason === 'no_evidence') return { type: 'no_evidence', message: '未找到可支撑回答的 Git 证据。' }
  } catch { /* 旧消息可能没有有效的路由 JSON，此时无需额外提示。 */ }
  return null
}

export function messageStatusLabel(message: AIMessage) {
  if (message.role !== 'assistant') return ''
  switch (message.status) {
    case 'streaming': return '生成中'
    case 'partial': return '回答中断，已保存部分内容'
    case 'failed': return '回答失败'
    case 'stopped': return '已停止生成'
    default: return ''
  }
}

export function coverageStatus(status: string) {
  if (['covered', 'pass', 'succeeded'].includes(status)) return 'success'
  if (['missing', 'missing_required', 'failed', 'forbidden', 'conflict', 'verification_failed', 'fail'].includes(status)) return 'failed'
  if (['partial', 'completed_with_gaps'].includes(status)) return 'partial'
  return status
}

export function coverageDeltaText(delta?: Record<string, string>) {
  return delta && Object.keys(delta).length ? Object.entries(delta).map(([key, status]) => `${key}:${status}`).join(' · ') : '无变化'
}
