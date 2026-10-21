import { describe, expect, it } from 'vitest'
import { diagnosticLabel, statusLabel, statusVariant, triggerLabel } from './labels'

describe('统一中文标签', () => {
  it.each([
    ['success', '成功', 'ok'], ['partial_success', '部分成功', 'warn'], ['failed', '失败', 'danger'], ['running', '扫描中', 'info'],
    ['succeeded', '通过', 'ok'], ['completed_with_gaps', '有缺口', 'warn'], ['insufficient_evidence', '证据不足', 'info'],
    ['high', '高', 'ok'], ['medium', '中', 'warn'], ['low', '低', 'neutral'],
    ['covered', '已覆盖', 'ok'], ['partial', '部分', 'warn'], ['missing', '缺失', 'danger']
  ])('%s 显示 %s 并使用 %s 颜色', (status, label, variant) => {
    expect(statusLabel(status)).toBe(label)
    expect(statusVariant(status)).toBe(variant)
  })
  it.each([['startup', '服务启动'], ['scheduled', '定时'], ['manual', '手动'], ['webhook', 'Webhook']])('翻译触发方式 %s', (value, label) => {
    expect(triggerLabel(value)).toBe(label)
  })
  it('未知值保留原值且使用中性色', () => {
    for (const value of ['future_status', '', 'constructor', 'toString']) {
      expect(statusLabel(value)).toBe(value)
      expect(statusVariant(value)).toBe('neutral')
      expect(triggerLabel(value)).toBe(value)
    }
  })
  it('诊断操作中文化且不替换任意契约 key', () => {
    expect(diagnosticLabel('retrieve_more')).toBe('继续检索')
    expect(diagnosticLabel('token.payload_fields')).toBe('token.payload_fields')
    expect(diagnosticLabel()).toBe('—')
  })
})
