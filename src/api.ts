import type {
  CommitDetail,
  CommitSummary,
  FileContent,
  FileEntry,
  PathEvent,
  RepoRef,
  Repository,
  ScanRun
} from './types'

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
    try {
      const body = await response.json()
      message = body.error || message
    } catch {
      // keep status text
    }
    throw new Error(message)
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
  async history(repoID: number, branch: string, limit = 80) {
    return request<{ items: CommitSummary[] }>(
      `/api/repos/${repoID}/history?${new URLSearchParams({ branch, limit: String(limit) })}`
    )
  },
  async commit(repoID: number, sha: string) {
    return request<CommitDetail>(`/api/repos/${repoID}/commits/${sha}`)
  },
  async scanRuns(repoID: number) {
    return request<{ items: ScanRun[] }>(`/api/repos/${repoID}/scan-runs`)
  }
}

export function downloadURL(repoID: number, versionID: number) {
  return `/api/repos/${repoID}/versions/${versionID}/download`
}

export function blobURL(repoID: number, commit: string, filePath: string) {
  return `/api/repos/${repoID}/blob/download?${new URLSearchParams({
    commit_sha: commit,
    path: filePath,
    inline: '1'
  })}`
}
