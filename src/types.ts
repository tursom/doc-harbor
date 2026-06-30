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

export interface AIProvider {
  provider_key: string
  name: string
  priority: number
  provider_type: string
  base_url: string
  api_key_secret_id: number
  model: string
  cost_tier: string
  request_timeout_seconds: number
  max_rpm: number
  secret_configured?: boolean
  secret_last4?: string
  secret_fingerprint?: string
}

export interface AIRouting {
  default_task_class: string
  task_classes: Record<string, { providers: string[]; fallback_task_class: string }>
  escalation: Record<string, string>
}

export interface AIConfigData {
  enabled: boolean
  viewer: { header_candidates: string[] }
  history: { retention_days: number }
  chat: {
    timeout_seconds: number
    max_context_chunks: number
    routing: AIRouting
    providers: AIProvider[]
  }
  indexing: {
    default_scan_roots: string[]
    exclude_globs: string[]
    max_file_size: number
  }
  memory: {
    enabled: boolean
    use: boolean
    generate: boolean
    review_required: boolean
    max_context_items: number
    min_confidence: number
    retention_days: number
  }
}

export interface AIConfigVersion {
  id: number
  version: number
  status: string
  config_hash: string
  config: AIConfigData
  secret_refs: number[]
  validation_status: string
  validation_report_json: string
  created_by_viewer: string
  published_by_viewer: string
  created_at: string
  updated_at: string
  published_at: string
  superseded_at: string
  error_message: string
}

export interface AISecret {
  id: number
  name: string
  secret_type: string
  fingerprint: string
  last4: string
  created_at: string
  updated_at: string
}

export type AISettingsStatus = 'not_configured' | 'ready_to_test' | 'ready_disabled' | 'enabled' | 'error'
export type AICostTier = 'low' | 'medium' | 'high'

export interface AISettingsRouteProvider {
  provider_key: string
  name: string
  model: string
  priority: number
}

export interface AISettingsProviderSummary {
  provider_key: string
  name: string
  provider_type: string
  base_url: string
  model: string
  api_key_configured: boolean
  api_key_last4?: string
  is_default: boolean
  route_order: number
  usable: boolean
  last_test_status?: 'pass' | 'fail' | 'not_run'
  last_test_message?: string
  timeout_seconds: number
  request_timeout_seconds?: number
  max_rpm: number
  priority: number
  cost_tier: AICostTier
}

export interface AIProviderTestSummary {
  status: 'pass' | 'fail'
  message: string
  tested_at?: string
  latency_ms: number
}

export interface AISettingsSummary {
  enabled: boolean
  status: AISettingsStatus
  default_provider_key: string
  default_provider_name: string
  default_model: string
  route_provider_keys: string[]
  route_providers: AISettingsRouteProvider[]
  active_route_provider_keys: string[]
  has_unapplied_changes: boolean
  encryption_ready: boolean
  editable_status?: string
  last_test?: AIProviderTestSummary
  providers: AISettingsProviderSummary[]
}

export interface AISettingsForm {
  provider_key: string
  name: string
  base_url: string
  model: string
  api_key: string
  timeout_seconds: number
  max_rpm: number
  priority: number
  cost_tier: AICostTier
}

export interface AIProviderTestResult {
  status: 'pass' | 'fail'
  message: string
  model: string
  latency_ms: number
  safe_error?: string
}

export interface APIErrorPayload {
  error?: { code?: string; message?: string; detail?: string }
  fields?: Record<string, string>
  provider_errors?: Record<string, string>
  settings?: AISettingsSummary
}

export interface AINotice {
  type: 'ai_disabled' | 'provider_error' | 'no_evidence'
  message: string
}

export interface AIQuestionScope {
  repo_mode: string
  repo_ids: number[]
  source_mode: string
  file_types: string[]
  current_file?: {
    repo_id: number
    version_id: number
    branch: string
    commit_sha: string
    file_path: string
  }
}

export interface AISession {
  id: number
  title: string
  viewer_key: string
  scope_json: string
  created_at: string
  updated_at: string
  archived_at: string
}

export interface AccessTokenResponse {
  token: string
  expires_at: string
  capabilities: string[]
  scope: {
    viewer_key?: string
  }
}

export interface AIMessage {
  id: number
  session_id: number
  role: 'user' | 'assistant' | 'system'
  content: string
  model: string
  provider_name: string
  model_route_json: string
  prompt_tokens: number
  completion_tokens: number
  latency_ms: number
  status: string
  error_message: string
  created_at: string
}

export interface AIServiceCandidate {
  id: number
  run_id: number
  message_id: number
  repo_id: number
  repo_name?: string
  service_name: string
  matched_terms: string[]
  confidence: string
  reason: string
  score: number
  evidence_count: number
  created_at: string
}

export interface AIMessageCitation {
  id: number
  message_id: number
  repo_id: number
  repo_name?: string
  version_id: number
  source_scope: string
  branch: string
  commit_sha: string
  file_path: string
  line_start: number
  line_end: number
  quote_text: string
  score: number
  created_at: string
}

export interface AIAgentRun {
  id: number
  session_id: number
  user_message_id: number
  assistant_message_id: number
  status: string
  current_state: string
  intent: string
  scope_json: string
  retrieval_plan_json: string
  service_candidate_count: number
  evidence_count: number
  code_evidence_count: number
  memory_count: number
  unconfirmed_count: number
  verification_status: string
  verification_report_json: string
  checkpoint_json: string
  index_snapshot_id: number
  config_version: number
  config_hash: string
  model: string
  provider_name: string
  provider_failover_json: string
  model_route_json: string
  escalation_count: number
  estimated_cost_json: string
  started_at: string
  finished_at: string
  error_message: string
}

export interface AIDiagnosticsIndexingSummary {
  default_scan_roots: string[]
  exclude_globs: string[]
  max_file_size: number
}

export interface AIDiagnosticsScanPath {
  id: number
  path: string
  include_globs: string[]
  exclude_globs: string[]
  created_at: string
  updated_at: string
}

export interface AIDiagnosticsBranchTarget {
  branch: string
  commit_sha: string
  commit_time: string
  last_scanned_at: string
  source_scope: string
}

export interface AIDiagnosticsScanRun {
  id: number
  trigger_type: string
  status: string
  branch_count: number
  file_count: number
  skipped_count: number
  error_count: number
  started_at: string
  finished_at: string
}

export interface AIDiagnosticsRepositorySource {
  id: number
  name: string
  slug: string
  enabled: boolean
  default_branch: string
  tracked_branches: string[]
  latest_include_branches: string[]
  latest_exclude_branches: string[]
  stale_branch_days: number
  branch_priority: string[]
  sync_interval_seconds: number
  max_file_size_bytes: number
  scan_paths: AIDiagnosticsScanPath[]
  default_target?: AIDiagnosticsBranchTarget
  candidate_targets: AIDiagnosticsBranchTarget[]
  latest_scan?: AIDiagnosticsScanRun
  created_at: string
  updated_at: string
}

export interface AIDiagnosticsDataSources {
  scope: AIQuestionScope
  indexing: AIDiagnosticsIndexingSummary
  repositories: AIDiagnosticsRepositorySource[]
  current_file?: AIQuestionScope['current_file']
}

export interface AIQuestionResult {
  run: AIAgentRun
  message: AIMessage
  service_candidates: AIServiceCandidate[]
  citations: AIMessageCitation[]
  notice?: AINotice
}

export interface AIStreamStageEvent {
  type: 'stage'
  stage: string
  status: string
  message: string
  evidence_count?: number
  candidate_count?: number
}

export interface AIStreamProviderAttemptEvent {
  type: 'provider_attempt'
  attempt: number
  task_class?: string
  provider_key: string
  provider: string
  model: string
  priority: number
  status: string
  error?: string
}

export type AIStreamEvent =
  | { type: 'user_message'; message: AIMessage }
  | { type: 'run_started'; run: AIAgentRun; assistant_message: AIMessage }
  | AIStreamStageEvent
  | { type: 'service_candidates'; items: AIServiceCandidate[] }
  | { type: 'citations'; items: AIMessageCitation[] }
  | AIStreamProviderAttemptEvent
  | { type: 'answer_delta'; message_id: number; delta: string }
  | {
      type: 'message_done'
      run: AIAgentRun
      message: AIMessage
      service_candidates: AIServiceCandidate[]
      citations: AIMessageCitation[]
      notice?: AINotice
      usage?: { prompt_tokens: number; completion_tokens: number }
    }
  | { type: 'error'; message: string; partial_message_id?: number; assistant_message?: AIMessage }
  | { type: 'done'; ok?: boolean }
