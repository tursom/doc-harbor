import type { AIQuestionScope, AISession } from './types'

/** pi 引擎的公开协议与旧版消息协议分别定义，避免同名 SSE 事件的字段被混用。 */
export type PiRole = '开发' | '产品' | '测试' | '未指定'
export type PiRunStatus = 'queued' | 'running' | 'succeeded' | 'aborted' | 'timeout' | 'failed'
export interface PiCitation {
  key: string; repo: string; ref: string; path: string
  line_start: number; line_end: number
  status: 'verified' | 'path_only' | 'unverified'
  url: string; repo_id?: number; version_id?: number; commit_sha?: string
}
export interface PiUsage {
  input_tokens: number; output_tokens: number; cache_read_tokens: number
  cache_write_tokens?: number; total_tokens?: number; cost?: number
}
export interface PiStep {
  call_id: string; tool: string; args: Record<string, unknown>
  summary: string; is_error: boolean; preview: string
  running?: boolean
}
export interface PiFeedback { rating: 1 | -1; comment: string; created_at?: string }
export interface PiTurn {
  run_id: number; question: string; role: PiRole; scope: AIQuestionScope
  status: PiRunStatus; steps: PiStep[]; answer: string; citations: PiCitation[]
  usage: PiUsage; feedback: PiFeedback | null
  model?: string; provider_key?: string; started_at?: string; finished_at?: string
  duration_ms?: number; tool_calls?: number; error_message?: string; scope_label?: string
  // 以下为浏览器运行态；服务端历史响应不依赖这些字段。
  queue_position?: number; notice?: string; supplements?: string[]
}
export interface PiRun {
  id: number; session_id: number; status: PiRunStatus; question: string; role: PiRole; scope_json: string
  provider_key: string; model: string; tool_calls: number; input_tokens: number; output_tokens: number
  cache_read_tokens: number; cost: number; citations_json: string; error_message: string
  stderr_tail?: string; started_at: string; finished_at: string; feedback?: PiFeedback | null
}
export interface PiDiagnosticTurn extends PiTurn { messages: unknown[] }
export interface PiDiagnosticDetail { run: PiRun; session: AISession; transcript: PiDiagnosticTurn | null }
export type PiStreamEvent =
  | { type: 'queued'; position: number }
  | { type: 'started'; run_id: number }
  | { type: 'text'; delta: string }
  | { type: 'tool_start'; call_id: string; tool: string; args: Record<string, unknown> }
  | { type: 'tool_end'; call_id: string; summary: string; is_error: boolean; preview: string }
  | { type: 'notice'; message: string }
  | { type: 'done'; run_id?: number; status: PiRunStatus; citations: PiCitation[]; usage: PiUsage; message?: string }
  | { type: 'error'; message: string }
