import type {
  AIMessage,
  AIMessageCitation,
  AIProviderTestResult,
  AIQuestionResult,
  AIQuestionScope,
  AIServiceCandidate,
  AISettingsForm,
  AISettingsProviderSummary,
  AISettingsSummary,
  APIErrorPayload,
  AISession,
  CommitDetail,
  CommitSummary,
  FileContent,
  FileEntry,
  PathEvent,
  RepoRef,
  Repository,
  ScanRun
} from './types'

export class APIRequestError extends Error {
  status: number
  code: string
  detail: string
  fields: Record<string, string>
  providerErrors: Record<string, string>
  settings?: AISettingsSummary

  constructor(status: number, message: string, payload: APIErrorPayload = {}) {
    super(message)
    this.name = 'APIRequestError'
    this.status = status
    this.code = payload.error?.code || ''
    this.detail = payload.error?.detail || ''
    this.fields = payload.fields || {}
    this.providerErrors = payload.provider_errors || {}
    this.settings = payload.settings
  }
}

async function request<T>(path: string, options: RequestInit = {}): Promise<T> {
  const response = await fetch(path, {
    ...options,
    headers: {
      'Content-Type': 'application/json',
      ...(options.headers || {})
    }
  })
  if (!response.ok) {
    let message = response.statusText
    let payload: APIErrorPayload = {}
    try {
      const body = await response.json()
      if (typeof body.error === 'string') {
        message = body.error
      } else {
        payload = body
        message = body.error?.message || message
      }
    } catch {
      // keep status text
    }
    throw new APIRequestError(response.status, message, payload)
  }
  if (response.status === 204) {
    return undefined as T
  }
  return response.json()
}

export const api = {
  async repos() {
    return request<{ items: Repository[] }>('/api/repos')
  },
  async createRepo(repo: Partial<Repository>) {
    return request<Repository>('/api/repos', { method: 'POST', body: JSON.stringify(repo) })
  },
  async updateRepo(id: number, repo: Partial<Repository>) {
    return request<Repository>(`/api/repos/${id}`, { method: 'PATCH', body: JSON.stringify(repo) })
  },
  async scanRepo(id: number) {
    return request<ScanRun>(`/api/repos/${id}/scan`, { method: 'POST', body: '{}' })
  },
  async branches(id: number) {
    return request<{ items: RepoRef[] }>(`/api/repos/${id}/branches`)
  },
  async files(id: number, params: Record<string, string>) {
    return request<{ items: FileEntry[] }>(`/api/repos/${id}/files?${new URLSearchParams(params)}`)
  },
  async tree(id: number, params: Record<string, string>) {
    return request<{ items: FileEntry[] }>(`/api/repos/${id}/tree?${new URLSearchParams(params)}`)
  },
  async search(id: number, q: string) {
    return request<{ items: FileEntry[] }>(`/api/repos/${id}/search?${new URLSearchParams({ q })}`)
  },
  async content(repoID: number, versionID: number) {
    return request<FileContent>(`/api/repos/${repoID}/versions/${versionID}/content`)
  },
  async versions(repoID: number, documentID: number) {
    return request<{ items: FileContent['versions'] }>(`/api/repos/${repoID}/documents/${documentID}/versions`)
  },
  async docHistory(repoID: number, documentID: number) {
    return request<{ document: unknown; versions: FileContent['versions']; events: PathEvent[] }>(
      `/api/repos/${repoID}/documents/${documentID}/history`
    )
  },
  async history(repoID: number, branch = '', limit = 80) {
    const params = new URLSearchParams({ limit: String(limit) })
    if (branch) params.set('branch', branch)
    return request<{ items: CommitSummary[] }>(`/api/repos/${repoID}/history?${params}`)
  },
  async commit(repoID: number, sha: string) {
    return request<CommitDetail>(`/api/repos/${repoID}/commits/${sha}`)
  },
  async scanRuns(repoID: number) {
    return request<{ items: ScanRun[] }>(`/api/repos/${repoID}/scan-runs`)
  },
  async githubWebhookSecret() {
    return request<{ configured: boolean; secret: string }>('/api/webhooks/github/secret')
  },
  async aiSettings() {
    return request<AISettingsSummary>('/api/ai/settings')
  },
  async saveAIDefaultProvider(payload: AISettingsForm & { enable: boolean }) {
    return request<{
      enabled: boolean
      provider: AISettingsProviderSummary
      settings: AISettingsSummary
      message: string
    }>('/api/ai/settings/default-provider', { method: 'PUT', body: JSON.stringify(payload) })
  },
  async testAIProvider(payload: Partial<AISettingsForm>) {
    return request<AIProviderTestResult>('/api/ai/providers/test', {
      method: 'POST',
      body: JSON.stringify({
        provider_key: payload.provider_key || '',
        name: payload.name || '',
        base_url: payload.base_url || '',
        model: payload.model || '',
        api_key: payload.api_key || '',
        timeout_seconds: payload.timeout_seconds || 20
      })
    })
  },
  async setAIEnabled(enabled: boolean) {
    return request<{ settings: AISettingsSummary }>('/api/ai/settings/enabled', {
      method: 'PATCH',
      body: JSON.stringify({ enabled })
    })
  },
  async applyAISettings(payload: { enabled?: boolean; test_policy?: 'default_only' | 'changed_routable' | 'all_routable' } = {}) {
    return request<{ enabled: boolean; settings: AISettingsSummary; message: string }>('/api/ai/settings/apply', {
      method: 'POST',
      body: JSON.stringify(payload)
    })
  },
  async createAIProvider(payload: Partial<AISettingsForm> & { test_before_save?: boolean }) {
    return request<{
      provider: AISettingsProviderSummary
      settings: AISettingsSummary
      message: string
    }>('/api/ai/providers', { method: 'POST', body: JSON.stringify(payload) })
  },
  async updateAIProvider(providerKey: string, payload: Partial<AISettingsForm> & { make_default?: boolean; test_before_save?: boolean }) {
    return request<{
      provider: AISettingsProviderSummary
      settings: AISettingsSummary
      message: string
    }>(`/api/ai/providers/${providerKey}`, { method: 'PATCH', body: JSON.stringify(payload) })
  },
  async deleteAIProvider(providerKey: string, replacementProviderKey = '') {
    const query = replacementProviderKey ? `?${new URLSearchParams({ replacement_provider_key: replacementProviderKey })}` : ''
    return request<{ deleted_provider_key: string; settings: AISettingsSummary; message: string }>(`/api/ai/providers/${providerKey}${query}`, {
      method: 'DELETE'
    })
  },
  async aiSessions(params: Record<string, string> = {}) {
    const query = new URLSearchParams(params)
    return request<{ items: AISession[] }>(`/api/ai/sessions${query.toString() ? `?${query}` : ''}`)
  },
  async createAISession(payload: { title: string; scope: AIQuestionScope }) {
    return request<AISession>('/api/ai/sessions', { method: 'POST', body: JSON.stringify(payload) })
  },
  async aiMessages(sessionID: number) {
    return request<{
      items: AIMessage[]
      service_candidates: AIServiceCandidate[]
      citations: AIMessageCitation[]
    }>(`/api/ai/sessions/${sessionID}/messages`)
  },
  async askAI(sessionID: number, question: string, scope: AIQuestionScope) {
    return request<AIQuestionResult>(`/api/ai/sessions/${sessionID}/messages`, {
      method: 'POST',
      body: JSON.stringify({ question, scope_override: scope })
    })
  }
}

export function downloadURL(repoID: number, versionID: number) {
  return `/api/repos/${repoID}/versions/${versionID}/download`
}

export function blobURL(repoID: number, commit: string, filePath: string, inline = true) {
  return `/api/repos/${repoID}/blob/download?${new URLSearchParams({
    commit_sha: commit,
    path: filePath,
    inline: inline ? '1' : '0'
  })}`
}
