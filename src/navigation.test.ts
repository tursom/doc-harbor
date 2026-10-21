import { describe, expect, it } from 'vitest'
import { legacyPageRedirect, pagePaths, pageURL, readPositiveID } from './lib/navigation'

describe('static page navigation', () => {
  it('maps legacy tabs to static pages and preserves relevant query parameters', () => {
    expect(legacyPageRedirect('https://docs.example.test/?tab=history&repo=7&branch=release%2F1')).toBe(
      '/history/?repo=7&branch=release%2F1'
    )
    expect(legacyPageRedirect('https://docs.example.test/?tab=runs&repo=7')).toBe('/scans/?repo=7')
    expect(legacyPageRedirect('https://docs.example.test/?tab=repo-settings&repo=7&branch=main&version=9')).toBe(
      '/repo-settings/?repo=7'
    )
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
    expect(pageURL('repo-settings', current)).toBe('/repo-settings/?repo=12')
    expect(pageURL('repo-settings', new URLSearchParams())).toBe('/repo-settings/')
  })

  it('keeps all eight page paths and explicit deep-link state stable', () => {
    expect(pagePaths).toEqual({
      docs: '/', history: '/history/', runs: '/scans/', 'repo-settings': '/repo-settings/',
      ai: '/ai/', 'ai-config': '/ai/config/', 'ai-diagnostics': '/ai/diagnostics/', settings: '/settings/'
    })
    const current = new URLSearchParams('repo=7')
    expect(pageURL('docs', current, { view: 'branch', branch: 'release/1', dir: 'docs', version: 42 })).toBe(
      '/?repo=7&view=branch&branch=release%2F1&dir=docs&version=42'
    )
    expect(pageURL('history', current, { commit: 'target-sha' })).toBe('/history/?repo=7&commit=target-sha')
    expect(pageURL('ai', current, { session: 31, scope: 'file', version: 42, question: '如何发布？' })).toBe(
      '/ai/?repo=7&session=31&scope=file&version=42&question=%E5%A6%82%E4%BD%95%E5%8F%91%E5%B8%83%EF%BC%9F'
    )
    expect(legacyPageRedirect('https://docs.example.test/?tab=history&repo=7&commit=target-sha&version=42')).toBe(
      '/history/?repo=7&commit=target-sha'
    )
    expect(legacyPageRedirect('https://docs.example.test/?tab=ai&repo=7&session=31&scope=file&version=42&question=why')).toBe(
      '/ai/?repo=7&version=42&session=31&scope=file&question=why'
    )
  })

  it('only accepts positive integer identifiers', () => {
    expect(readPositiveID(new URLSearchParams('repo=3'), 'repo')).toBe(3)
    expect(readPositiveID(new URLSearchParams('repo=0'), 'repo')).toBeUndefined()
    expect(readPositiveID(new URLSearchParams('repo=1.5'), 'repo')).toBeUndefined()
    expect(readPositiveID(new URLSearchParams('repo=nope'), 'repo')).toBeUndefined()
  })
})
