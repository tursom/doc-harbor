import { describe, expect, it } from 'vitest'
import { formatBytes, formatDetailJSON, formatDuration, formatHistoryTime, formatSyncInterval, formatTime, shortCommit, shortSha } from './format'

describe('格式化', () => {
  it('保留缺省值与无法解析的日期', () => {
    expect(formatTime()).toBe('-')
    expect(formatTime('invalid')).toBe('invalid')
    expect(formatHistoryTime('invalid')).toBe('invalid')
    expect(formatHistoryTime('2026-10-21T12:00:00Z')).toMatch(/21 Oct 2026/)
  })
  it('大小边界与 SHA 长度', () => {
    expect([undefined, 0, 1023, 1024, 1048576].map(formatBytes)).toEqual(['0 B', '0 B', '1023 B', '1.0 KB', '1.0 MB'])
    expect(shortSha('1234567890abcdef')).toBe('1234567890ab')
    expect(shortCommit('1234567890abcdef')).toBe('12345678')
    expect(shortSha()).toBe('-')
  })
  it('耗时包含进行中、亚秒与无效数据', () => {
    expect(formatDuration('2026-01-01T00:00:00Z')).toBe('进行中')
    expect(formatDuration('2026-01-01T00:00:00Z', '2026-01-01T00:00:01.500Z')).toBe('1.5 秒')
    expect(formatDuration('invalid', 'invalid')).toBe('-')
    expect(formatDuration('2026-01-02', '2026-01-01')).toBe('-')
  })
  it('同步间隔与扫描明细', () => {
    expect([-1, 30, 60, 3600].map(formatSyncInterval)).toEqual(['已关闭', '每 30 秒', '每 1 分钟', '每 60 分钟'])
    expect(formatDetailJSON('{"errors":2}')).toBe('{\n  "errors": 2\n}')
    expect(formatDetailJSON('原始错误')).toBe('原始错误')
  })
})
