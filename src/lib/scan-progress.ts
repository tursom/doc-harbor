import type { ScanRun } from '../types'

export interface ScanProgress {
  stage: string
  branch: string
  processed_files: number
  total_files: number
}

/** 历史记录可能没有进度，失败记录也可能只有错误详情；解析失败不能影响扫描页。 */
export function scanProgress(run?: ScanRun): ScanProgress | null {
  if (!run?.detail_json) return null
  try {
    const progress = JSON.parse(run.detail_json)?.progress
    if (!progress || typeof progress.stage !== 'string') return null
    const count = (value: unknown) => typeof value === 'number' && Number.isFinite(value) ? Math.max(0, value) : 0
    return { stage: progress.stage, branch: typeof progress.branch === 'string' ? progress.branch : '',
      processed_files: count(progress.processed_files), total_files: count(progress.total_files) }
  } catch { return null }
}

export function scanStageLabel(stage: string) {
  const stages: Record<string, string> = {
    fetching: '同步仓库', enumerating: '枚举文件', metadata: '准备元数据', content: '读取内容',
    history: '回填历史', writing: '写入索引', latest: '更新智能最新', finished: '已结束'
  }
  return stages[stage] || stage
}
