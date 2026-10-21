// 保留原页面的时间、SHA 与大小格式，供各独立页面共用。
export function formatTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString()
}

export function formatHistoryTime(value?: string) {
  if (!value) return '-'
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return value
  return date.toLocaleString('en-GB', {
    day: '2-digit', month: 'short', year: 'numeric', hour: '2-digit', minute: '2-digit', hour12: false
  }).replace(',', '')
}

export function shortSha(value?: string) { return value ? value.slice(0, 12) : '-' }
export function shortCommit(value?: string) { return value ? value.slice(0, 8) : '-' }
export function formatBytes(value?: number) {
  if (!value) return '0 B'
  if (value < 1024) return `${value} B`
  if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`
  return `${(value / 1024 / 1024).toFixed(1)} MB`
}

/** 扫描未结束时保留进行中状态，错误时间不伪造耗时。 */
export function formatDuration(startedAt?: string, finishedAt?: string) {
  if (!finishedAt) return '进行中'
  if (!startedAt) return '-'
  const milliseconds = Date.parse(finishedAt) - Date.parse(startedAt)
  if (!Number.isFinite(milliseconds) || milliseconds < 0) return '-'
  return `${Number((milliseconds / 1000).toFixed(1))} 秒`
}

export function formatSyncInterval(seconds: number) {
  if (seconds === -1) return '已关闭'
  if (!Number.isFinite(seconds)) return '-'
  return seconds < 60 ? `每 ${seconds} 秒` : `每 ${Number((seconds / 60).toFixed(1))} 分钟`
}

/** detail_json 可能是旧扫描保存的普通文本，解析失败时仍显示原文。 */
export function formatDetailJSON(value: string) {
  if (!value) return ''
  try { return JSON.stringify(JSON.parse(value), null, 2) } catch { return value }
}
