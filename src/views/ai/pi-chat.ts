import type { PiCitation, PiRole, PiStep, PiTurn } from '../../ai-pi-types'
import type { AIQuestionScope } from '../../types'

export const ROLE_STORAGE_KEY = 'docharbor.ai.role'
export const roleExamples: Record<PiRole, string[]> = {
  开发: ['SGPS 下单接口的请求参数和返回字段是什么？', '用户服务的 token 怎么校验？', '游戏详情接口返回的发货方式怎么排序？'],
  测试: ['CDK 激活失败时，返回的错误码分别代表什么？', '秒杀库存归还失败后会怎么重试？', '礼物订单缺少账号名时会出现什么现象？'],
  产品: ['秒杀库存人工归还有哪些限制条件？', '游戏交易从发货到退款有哪些状态？', '第三方卖家接入有几种方式，区别是什么？'],
  未指定: ['SGPS 下单接口的请求参数和返回字段是什么？', 'CDK 激活失败时，返回的错误码分别代表什么？', '秒杀库存人工归还有哪些限制条件？']
}

export function isActive(turn?: PiTurn) { return turn?.status === 'queued' || turn?.status === 'running' }

/** 只剥离消息开头的协议块，保留用户正文中的 context 示例。 */
export function stripContext(text: string) { return text.replace(/^\s*<context>\s*\n[\s\S]*?\n<\/context>\s*\n?/, '') }

export function scopeLabel(scope: AIQuestionScope, repoName?: string) {
  if (scope.repo_mode === 'current_file') return `当前文件 · ${scope.current_file?.file_path || '文件'}`
  if (scope.repo_mode === 'current_repo') return `当前仓库${repoName ? ` · ${repoName}` : scope.repo_ids?.length ? ` · #${scope.repo_ids.join('、#')}` : ''}`
  return '全部仓库'
}

/** MCP 的 direct 名称可能带 server 前缀；界面统一展示实际工具的中文名称。 */
export function toolName(tool: string) { return tool.split(/[/:.]/).pop()!.replace(/^(?:mcp__?)?docharbor__?/, '') }
export const toolLabels: Record<string, string> = {
  list_repos: '查看仓库', list_branches: '查看分支', list_docs: '浏览文档目录', search_docs: '搜索文档',
  search_code: '搜索代码', read: '阅读', list_dir: '浏览目录', git_log: '查看提交', diff_files: '比较分支'
}
export function stepLabel(step: PiStep) { return toolLabels[toolName(step.tool)] || step.tool }
export function stepParameters(step: PiStep) {
  const a = step.args || {}
  const repo = a.repo || (Array.isArray(a.repos) ? a.repos.join('、') : a.repos)
  const parts = [a.query ? `“${a.query}”` : '', a.path || a.path_glob || '',
    toolName(step.tool) === 'read' ? `第 ${a.start_line || 1}${a.end_line ? `-${a.end_line}` : ' 起'} 行` : '',
    repo ? `${repo}${a.ref ? `@${a.ref}` : ''}` : a.ref || '',
    a.base || a.head ? `${a.base || ''} → ${a.head || ''}` : '']
  return parts.filter(Boolean).join(' · ')
}
export function durationMs(turn: PiTurn) {
  if (turn.duration_ms != null) return turn.duration_ms
  const start = Date.parse(turn.started_at || '')
  const end = Date.parse(turn.finished_at || '')
  return Number.isFinite(start) && Number.isFinite(end) ? Math.max(0, end - start) : null
}
export function processSummary(turn: PiTurn) {
  const files = new Set(turn.steps.filter(s => toolName(s.tool) === 'read').map(s => `${s.args.repo || ''}@${s.args.ref || ''}:${s.args.path || ''}`))
  const searches = turn.steps.filter(s => ['search_docs', 'search_code'].includes(toolName(s.tool))).length
  const duration = durationMs(turn)
  return [`查阅 ${files.size} 个文件`, `搜索 ${searches} 次`, duration == null ? '' : `${Math.round(duration / 1000)} 秒`].filter(Boolean).join(' · ')
}
export function turnStatus(turn: PiTurn) {
  if (turn.status === 'queued') return `排队中，前面还有 ${Math.max(0, (turn.queue_position || 1) - 1)} 个问题`
  if (turn.status === 'running') return '正在查阅并回答…'
  if (turn.status === 'timeout') return turn.error_message || '回答超时，已停止，请缩小问题范围后重试。'
  if (turn.status === 'failed') return turn.error_message || '模型调用失败，请重试。'
  if (turn.status === 'aborted') return turn.error_message || '已停止'
  return turn.error_message || ''
}
export function needsNarrowRetry(turn: PiTurn) {
  return turn.status === 'timeout' || /工具.*(?:过多|上限|超限)|检索步骤过多|tool.*limit/i.test(turn.error_message || '')
}

export function citationURL(url: string) {
  // 引用 URL 来自服务端；仅保留应用内地址和普通网页地址。
  if (/^\/(?!\/)/.test(url) || /^https?:\/\//i.test(url)) return url
  return ''
}
const citationPattern = /\[([^\[\]@\n]+)@([^:\]\n]+):([^\]#\n]+)#L(\d+)(?:-(\d+))?\]/g
export function parseCitationKeys(text: string) {
  return [...text.matchAll(citationPattern)].map(m => ({ key: m[0].slice(1, -1), repo: m[1]!, ref: m[2]!, path: m[3]!, start: Number(m[4]), end: Number(m[5] || m[4]) }))
}

/** 在净化后的 Markdown 文本节点上加标签，代码、链接、属性均不参与转换。 */
export function decoratePiCitations(html: string, citations: PiCitation[], completed: boolean) {
  const template = document.createElement('template')
  template.innerHTML = html
  const walker = document.createTreeWalker(template.content, NodeFilter.SHOW_TEXT)
  const nodes: Text[] = []
  while (walker.nextNode()) nodes.push(walker.currentNode as Text)
  for (const node of nodes) {
    if (node.parentElement?.closest('code, pre, a, button, script, style')) continue
    const matches = [...node.data.matchAll(citationPattern)]
    if (!matches.length) continue
    const fragment = document.createDocumentFragment()
    let offset = 0
    for (const match of matches) {
      fragment.append(node.data.slice(offset, match.index))
      const key = match[0].slice(1, -1)
      const citation = citations.find(c => c.key === key || c.key === match[0])
      const warn = completed && citation?.status !== 'verified'
      const href = citation ? citationURL(citation.url) : ''
      const badge = document.createElement(href ? 'a' : 'span')
      badge.className = `pi-citation${warn ? ' warn' : ''}`
      badge.textContent = `${match[3]!.split('/').pop()}#L${match[4]}${match[5] ? `-${match[5]}` : ''}`
      badge.title = `${key}${warn ? '\n这段内容没有在本次检索中读到，请自行核对' : ''}`
      if (href) {
        badge.setAttribute('href', href); badge.setAttribute('target', '_blank'); badge.setAttribute('rel', 'noopener noreferrer')
      }
      fragment.append(badge)
      offset = match.index! + match[0].length
    }
    fragment.append(node.data.slice(offset))
    node.replaceWith(fragment)
  }
  return template.innerHTML
}

/** 同仓库、同 ref、同路径合并，保留各个引用范围与最弱核实状态。 */
export function groupPiCitations(citations: PiCitation[]) {
  const groups = new Map<string, { key: string; repo: string; ref: string; path: string; status: PiCitation['status']; items: PiCitation[] }>()
  const severity = { verified: 0, path_only: 1, unverified: 2 }
  for (const citation of citations) {
    const key = JSON.stringify([citation.repo, citation.ref, citation.path])
    const group = groups.get(key) || { key, repo: citation.repo, ref: citation.ref, path: citation.path, status: citation.status, items: [] }
    if (!group.items.some(c => c.key === citation.key)) group.items.push(citation)
    if (severity[citation.status] > severity[group.status]) group.status = citation.status
    groups.set(key, group)
  }
  return [...groups.values()]
}
