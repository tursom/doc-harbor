export type AppTab = 'docs' | 'history' | 'runs' | 'ai' | 'ai-config' | 'ai-diagnostics' | 'settings'

export const pagePaths: Record<AppTab, string> = {
  docs: '/',
  history: '/history/',
  runs: '/scans/',
  ai: '/ai/',
  'ai-config': '/ai/config/',
  'ai-diagnostics': '/ai/diagnostics/',
  settings: '/settings/'
}

const legacyTabs: Partial<Record<string, AppTab>> = {
  history: 'history',
  runs: 'runs',
  ai: 'ai',
  'ai-config': 'ai-config',
  'ai-diagnostics': 'ai-diagnostics',
  settings: 'settings'
}

const pageQueryKeys: Record<AppTab, string[]> = {
  docs: ['repo', 'view', 'branch', 'dir', 'version'],
  history: ['repo', 'branch', 'commit'],
  runs: ['repo'],
  ai: ['repo', 'version', 'session', 'scope', 'question'],
  'ai-config': ['repo'],
  'ai-diagnostics': ['repo'],
  settings: ['repo']
}

export function legacyPageRedirect(value: string | URL): string | null {
  const url = value instanceof URL ? value : new URL(value)
  const page = legacyTabs[url.searchParams.get('tab') || '']
  if (!page) return null

  const params = pickQuery(url.searchParams, pageQueryKeys[page])
  return withQuery(pagePaths[page], params)
}

export function pageURL(page: AppTab, current: URLSearchParams, values: Record<string, string | number | undefined> = {}) {
  const params = new URLSearchParams()
  const repoID = readPositiveID(current, 'repo')
  if (repoID) params.set('repo', String(repoID))
  for (const [key, value] of Object.entries(values)) {
    if (value !== undefined && value !== '') params.set(key, String(value))
  }
  return withQuery(pagePaths[page], params)
}

export function readPositiveID(params: URLSearchParams, key: string): number | undefined {
  const raw = params.get(key) || ''
  if (!/^[1-9]\d*$/.test(raw)) return undefined
  const value = Number(raw)
  return Number.isSafeInteger(value) ? value : undefined
}

function pickQuery(source: URLSearchParams, keys: string[]) {
  const target = new URLSearchParams()
  for (const key of keys) {
    const value = source.get(key)
    if (value !== null && value !== '') target.set(key, value)
  }
  return target
}

function withQuery(path: string, params: URLSearchParams) {
  const query = params.toString()
  return query ? `${path}?${query}` : path
}
