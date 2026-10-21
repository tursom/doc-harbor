// @vitest-environment jsdom
import { describe, expect, it } from 'vitest'
import type { AIMessage, AIMessageCitation, AISession } from '../../types'
import { decorateCitations, groupSessions, messageNotice, numberCitations, shouldSendOnEnter } from './chat'

const citation = (id: number, message_id: number, score = 0) => ({ id, message_id, score, file_path: `docs/${id}.md` }) as AIMessageCitation

describe('AI citation mapping', () => {
  it('restores insertion order inside each message despite history score ordering', () => {
    const input = [citation(103, 20, 99), citation(101, 20, 10), citation(70, 10, 20), citation(69, 10, 10)]
    const numbered = numberCitations(input)
    expect(numbered.map(({ citation: item, number, key }) => [item.id, number, key])).toEqual([
      [101, 1, '20:101'], [103, 2, '20:103'], [69, 1, '10:69'], [70, 2, '10:70']
    ])
    expect(input[0]!.id).toBe(103)
  })

  it('deduplicates persistent citations and does not invent IDs', () => {
    expect(numberCitations([citation(2, 5), citation(2, 5), citation(0, 5), citation(9, 0)])).toHaveLength(1)
  })

  it('links only matching message citations and leaves code, links, and attributes intact', () => {
    const html = '<p title="[C1]">事实 [C1] [C2] [C99]</p><pre><code>[C1]</code></pre><a href="/[C1]">[C1]</a><code>[C2]</code>'
    const decorated = decorateCitations(html, 20, numberCitations([citation(70, 10), citation(101, 20), citation(103, 20)]))
    const root = document.createElement('div')
    root.innerHTML = decorated
    expect([...root.querySelectorAll('button')].map((button) => button.dataset.citationKey)).toEqual(['20:101', '20:103'])
    expect(root.querySelector('span.citation-mark')?.textContent).toBe('[C99]')
    expect(root.querySelector('pre')?.textContent).toBe('[C1]')
    expect(root.querySelector('a')?.innerHTML).toBe('[C1]')
    expect(root.querySelector('p')?.title).toBe('[C1]')
  })
})

describe('AI composer keyboard', () => {
  const enter = { key: 'Enter', shiftKey: false, isComposing: false, keyCode: 13 }
  it('sends Enter but preserves Shift+Enter and every IME Enter signal', () => {
    expect(shouldSendOnEnter(enter)).toBe(true)
    expect(shouldSendOnEnter({ ...enter, shiftKey: true })).toBe(false)
    expect(shouldSendOnEnter({ ...enter, isComposing: true })).toBe(false)
    expect(shouldSendOnEnter({ ...enter, keyCode: 229 })).toBe(false)
    expect(shouldSendOnEnter(enter, true)).toBe(false)
    expect(shouldSendOnEnter({ ...enter, key: 'a' })).toBe(false)
  })
})

describe('AI history presentation', () => {
  it('groups sessions by local date and sorts newest first', () => {
    const sessions = [
      { id: 1, updated_at: '2026-06-27T10:00:00' },
      { id: 2, updated_at: '2026-06-28T09:00:00' },
      { id: 3, updated_at: '2026-06-28T11:00:00' }
    ] as AISession[]
    expect(groupSessions(sessions).map((group) => [group.day, group.items.map((item) => item.id)])).toEqual([
      ['2026-06-28', [3, 2]], ['2026-06-27', [1]]
    ])
  })

  it('restores notices from saved model routing and tolerates old malformed JSON', () => {
    const message = { id: 1, role: 'assistant', model_route_json: '{"reason":"ai_disabled"}' } as AIMessage
    expect(messageNotice(message)?.type).toBe('ai_disabled')
    expect(messageNotice({ ...message, model_route_json: 'invalid' })).toBeNull()
    expect(messageNotice({ ...message, role: 'user' })).toBeNull()
  })
})
