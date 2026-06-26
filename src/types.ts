export interface Repository {
  id: number
  name: string
  slug: string
  repo_url: string
  default_branch: string
  tracked_branches: string[]
  latest_include_branches: string[]
  latest_exclude_branches: string[]
  stale_branch_days: number
  branch_priority: string[]
  credential_ref: string
  enabled: boolean
  sync_interval_seconds: number
  max_file_size_bytes: number
  created_at: string
  updated_at: string
  scan_paths: ScanPath[]
  latest_scan?: ScanRun
}

export interface ScanPath {
  id?: number
  repo_id?: number
  path: string
  include_globs: string[]
  exclude_globs: string[]
  enabled: boolean
}

export interface RepoRef {
  id: number
  repo_id: number
  ref_type: string
  ref_name: string
  commit_sha: string
  commit_time: string
  last_scanned_at: string
}

export interface ScanRun {
  id: number
  repo_id: number
  trigger_type: string
  status: string
  branch_count: number
  file_count: number
  skipped_count: number
  error_count: number
  started_at: string
  finished_at: string
  error_message: string
  detail_json: string
}

export interface FileEntry {
  kind: 'dir' | 'file'
  name: string
  path: string
  document_id?: number
  version_id?: number
  title?: string
  extension?: string
  file_size?: number
  status?: string
  source_branch?: string
  source_commit_sha?: string
  last_commit_time?: string
  previewable?: boolean
  download_enabled?: boolean
  selection_reason?: string
}

export interface DocVersion {
  id: number
  repo_id: number
  document_id: number
  branch: string
  head_commit_sha: string
  scan_path: string
  file_path: string
  previous_path: string
  dir_path: string
  file_name: string
  extension: string
  mime_type: string
  file_size: number
  blob_sha: string
  status: string
  title: string
  previewable: boolean
  download_enabled: boolean
  last_commit_sha: string
  last_commit_time: string
  delete_commit_sha: string
  delete_commit_time: string
  rename_score: number
  participates_latest: boolean
}

export interface FileContent {
  version_id: number
  document_id: number
  repo_id: number
  branch: string
  file_path: string
  title: string
  extension: string
  mime_type: string
  file_size: number
  blob_sha: string
  source_commit_sha: string
  last_commit_time: string
  previewable: boolean
  download_enabled: boolean
  content?: string
  too_large: boolean
  versions?: DocVersion[]
}

export interface PathEvent {
  id: number
  repo_id: number
  document_id: number
  branch: string
  event_type: string
  old_path: string
  new_path: string
  commit_sha: string
  commit_time: string
  rename_score: number
}

export interface CommitSummary {
  sha: string
  parents: string[]
  author: string
  author_email: string
  commit_time: string
  decorations: string
  message: string
}

export interface CommitFileChange {
  status: string
  path: string
  old_path?: string
  new_path?: string
  rename_score?: number
}

export interface CommitDetail extends CommitSummary {
  files: CommitFileChange[]
}
