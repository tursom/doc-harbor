import { describe, expect, it } from 'vitest'
import { legacyPageRedirect, pageURL, readPositiveID } from './lib/navigation'

describe('static page navigation', () => {
  it('maps legacy tabs to static pages and preserves relevant query parameters', () => {
    expect(legacyPageRedirect('https://docs.example.test/?tab=history&repo=7&branch=release%2F1')).toBe(
      '/history/?repo=7&branch=release%2F1'
    )
    expect(legacyPageRedirect('https://docs.example.test/?tab=runs&repo=7')).toBe('/scans/?repo=7')
    expect(legacyPageRedirect('https://docs.example.test/?tab=ai-config&repo=7')).toBe('/ai/config/?repo=7')
    expect(legacyPageRedirect('https://docs.example.test/?tab=ai&repo=7&version=9&scope=file&question=why')).toBe(
      '/ai/?repo=7&version=9&scope=file&question=why'
    )
    expect(legacyPageRedirect('https://docs.example.test/?repo=7&dir=doc')).toBeNull()
  })

  it('builds page links with the current repository but drops page-local state', () => {
    const current = new URLSearchParams('repo=12&branch=main&dir=doc&version=9')
    expect(pageURL('history', current)).toBe('/history/?repo=12')
    expect(pageURL('ai', current)).toBe('/ai/?repo=12')
    expect(pageURL('docs', current)).toBe('/?repo=12')
  })

  it('only accepts positive integer identifiers', () => {
    expect(readPositiveID(new URLSearchParams('repo=3'), 'repo')).toBe(3)
    expect(readPositiveID(new URLSearchParams('repo=0'), 'repo')).toBeUndefined()
    expect(readPositiveID(new URLSearchParams('repo=1.5'), 'repo')).toBeUndefined()
    expect(readPositiveID(new URLSearchParams('repo=nope'), 'repo')).toBeUndefined()
  })
})
