import { describe, expect, it } from 'vitest'
import { scanProgress, scanStageLabel } from './scan-progress'
import type { ScanRun } from '../types'
describe('扫描进度兼容历史详情', () => {
  it('容忍缺失、损坏及未知阶段，不把准备数解释为提交数', () => {
    const run = (detail_json: string) => ({ detail_json, file_count: 0 }) as ScanRun
    expect(scanProgress()).toBeNull()
    expect(scanProgress(run('{broken'))).toBeNull()
    expect(scanProgress(run('{"errors":[]}'))).toBeNull()
    expect(scanProgress(run('{"progress":{"stage":"future","processed_files":12,"total_files":20}}')))
      .toEqual({ stage: 'future', branch: '', processed_files: 12, total_files: 20 })
    expect(scanStageLabel('future')).toBe('future')
    expect(scanStageLabel('history')).toBe('回填历史')
  })
})
