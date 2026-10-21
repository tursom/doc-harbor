// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import { marked } from 'marked'
import DOMPurify from 'dompurify'
import type { PiCitation, PiStep, PiTurn } from '../../ai-pi-types'
import { decoratePiCitations, groupPiCitations, parseCitationKeys, processSummary, stepLabel, stepParameters, stripContext, turnStatus } from './pi-chat'

const key = '游戏服务@main:doc/库存 方案.md#L40-88'
const citation: PiCitation = { key, repo: '游戏服务', ref: 'main', path: 'doc/库存 方案.md', line_start: 40, line_end: 88, status: 'verified', url: '/?repo=1&version=2' }
const step = (tool: string, args: Record<string, unknown>): PiStep => ({ call_id: tool, tool, args, summary: '', is_error: false, preview: '' })
const render = (text: string, citations = [citation], done = true) => decoratePiCitations(DOMPurify.sanitize(marked.parse(text, { async: false }) as string), citations, done)
const asDOM = (html: string) => { const el = document.createElement('div'); el.innerHTML = html; return el }

describe('pi Markdown citations', () => {
  it('parses Chinese paths and a single line or range without swallowing paragraphs', () => {
    expect(parseCitationKeys(`[${key}] [服务@release:src/a.go#L9]`)).toMatchObject([{ path: 'doc/库存 方案.md', start: 40, end: 88 }, { start: 9, end: 9 }])
    expect(parseCitationKeys('[服务@main:\nhello#L9]')).toEqual([])
  })
  it('opens verified source URLs in a new tab with filename labels', () => {
    const badge = asDOM(render(`规则 [${key}]`)).querySelector('a')!
    expect(badge.textContent).toBe('库存 方案.md#L40-88')
    expect(badge.getAttribute('href')).toBe('/?repo=1&version=2')
    expect(badge.target).toBe('_blank')
    expect(badge.rel).toContain('noopener')
    expect(badge.title).toBe(key)
    expect(badge.classList.contains('warn')).toBe(false)
  })
  it('keeps streaming citations neutral, then warns for unverified or missing verification results', () => {
    expect(asDOM(render(`[${key}]`, [], false)).querySelector('.warn')).toBeNull()
    for (const status of ['path_only', 'unverified'] as const) {
      const badge = asDOM(render(`[${key}]`, [{ ...citation, status }])).querySelector('.warn')!
      expect(badge.getAttribute('title')).toContain('这段内容没有在本次检索中读到')
    }
    expect(asDOM(render(`[${key}]`, [])).querySelector('.warn')).not.toBeNull()
  })
  it('does not decorate inline code, fenced code, links or attributes and rejects executable URLs', () => {
    const html = render(`正文 [${key}]\n\n\`[${key}]\`\n\n\`\`\`text\n[${key}]\n\`\`\`\n\n[已有链接 [${key}]](https://example.com)\n\n<img src=x onerror="alert(1)" title="[${key}]">`, [{ ...citation, url: 'javascript:alert(1)' }])
    const dom = asDOM(html)
    expect(dom.querySelectorAll('.pi-citation')).toHaveLength(1)
    expect(dom.querySelectorAll('code')).toHaveLength(2)
    expect(dom.querySelector('code')!.textContent).toContain(`[${key}]`)
    expect(html).not.toMatch(/javascript:|onerror/)
  })
  it('groups multiple line ranges per file while keeping refs distinct and the weakest status', () => {
    const groups = groupPiCitations([citation, { ...citation, key: 'second', line_start: 100, line_end: 120, status: 'path_only' }, { ...citation, key: 'third', ref: 'release' }, citation])
    expect(groups).toHaveLength(2)
    expect(groups[0]!.items).toHaveLength(2)
    expect(groups[0]!.status).toBe('path_only')
  })
})

describe('pi process and question presentation', () => {
  it('summarizes distinct files, search calls and server duration with actual MCP tool names', () => {
    const turn = { steps: [step('mcp_docharbor_read', { repo: '游戏', path: 'a.go' }), step('read', { repo: '游戏', path: 'a.go', start_line: 100 }), step('read', { repo: '用户', path: 'a.go' }), step('mcp_docharbor_search_code', {}), step('search_docs', {})], duration_ms: 32000 } as PiTurn
    expect(processSummary(turn)).toBe('查阅 2 个文件 · 搜索 2 次 · 32 秒')
    expect(stepLabel(turn.steps[0]!)).toBe('阅读')
    expect(stepParameters(step('read', { path: 'core/订单.go', start_line: 40, end_line: 120 }))).toContain('core/订单.go · 第 40-120 行')
    expect(stepParameters(step('search_code', { query: 'createOrder', repos: ['游戏服务'], ref: 'main' }))).toBe('“createOrder” · 游戏服务@main')
  })
  it('removes only a leading protocol context and retains user examples verbatim', () => {
    const question = '说明\n<context>\nrole: 测试\n</context>'
    expect(stripContext(`<context>\nrole: 开发\nscope: 全部仓库\n</context>\n\n${question}`)).toBe(question)
    expect(stripContext(question)).toBe(question)
  })
  it('explains queue position, timeout and tool-limit reasons', () => {
    expect(turnStatus({ status: 'queued', queue_position: 3 } as PiTurn)).toContain('前面还有 2 个问题')
    expect(turnStatus({ status: 'timeout' } as PiTurn)).toContain('缩小问题范围')
    expect(turnStatus({ status: 'aborted', error_message: '检索步骤过多，已停止' } as PiTurn)).toContain('检索步骤过多')
  })
})
