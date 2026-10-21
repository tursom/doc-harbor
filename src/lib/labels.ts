export type StatusVariant = 'ok' | 'warn' | 'danger' | 'info' | 'neutral'

// 所有页面共用同一套状态文案；未知状态保留服务端原值，避免掩盖新增状态。
const statuses: Record<string, readonly [string, StatusVariant]> = {
  success: ['成功', 'ok'], partial_success: ['部分成功', 'warn'], failed: ['失败', 'danger'], running: ['扫描中', 'info'],
  succeeded: ['通过', 'ok'], completed_with_gaps: ['有缺口', 'warn'], insufficient_evidence: ['证据不足', 'info'],
  high: ['高', 'ok'], medium: ['中', 'warn'], low: ['低', 'neutral'],
  covered: ['已覆盖', 'ok'], partial: ['部分', 'warn'], missing: ['缺失', 'danger'],
  missing_required: ['缺少必需证据', 'danger'], forbidden: ['含禁止证据', 'danger'], conflict: ['证据冲突', 'danger'],
  verification_failed: ['校验失败', 'danger'], pass: ['通过', 'ok'], fail: ['失败', 'danger'],
  not_run: ['未测试', 'neutral'], not_scanned: ['未扫描', 'neutral'],
  enabled: ['AI 问答已启用', 'ok'], ready_disabled: ['配置可用，尚未启用', 'neutral'],
  ready_to_test: ['可以测试连接', 'info'], not_configured: ['未配置', 'warn'], error: ['配置错误', 'danger'],
  disabled: ['已停用', 'neutral'], active: ['有效', 'ok'], deleted: ['已删除', 'danger'],
  renamed: ['已重命名', 'info'], moved: ['已移动', 'info'], default: ['默认', 'info'],
  pending: ['等待中', 'neutral'], started: ['已开始', 'info'], streaming: ['生成中', 'info'],
  skipped: ['已跳过', 'neutral'], cancelled: ['已取消', 'neutral'], completed: ['已完成', 'ok']
}

export function statusLabel(status: string): string {
  return statuses[status]?.[0] ?? status
}

export function statusVariant(status: string): StatusVariant {
  return statuses[status]?.[1] ?? 'neutral'
}

export function triggerLabel(trigger: string): string {
  const labels: Record<string, string> = { startup: '服务启动', scheduled: '定时', manual: '手动', webhook: 'Webhook' }
  return Object.hasOwn(labels, trigger) ? labels[trigger] : trigger
}

// 诊断中的机器标识仍通过 title 和原始 JSON 可查，界面说明优先展示中文。
export function diagnosticLabel(value?: string): string {
  if (!value) return '—'
  const labels: Record<string, string> = {
    initial: '首次检索', missing_contract_keys: '补充契约缺失项', explain_structure: '解释结构',
    state_gap_in_answer: '在回答中说明缺口', complete_with_gaps: '说明缺口后完成',
    retrieve_missing_contract_keys: '检索契约缺失项', resolve_contract_conflict: '解决证据冲突',
    remove_forbidden_evidence: '移除禁止证据', review_recommended_contract_gaps: '检查建议证据缺口',
    legacy_answer: '生成回答', contract_checker: '检查证据契约', rewrite_answer: '重新撰写回答',
    retrieve_more: '继续检索', block_answer: '阻止回答', planner: '规划器', retriever: '检索器',
    writer: '回答生成器', verifier: '校验器'
  }
  return Object.hasOwn(labels, value) ? labels[value] : statusLabel(value)
}
