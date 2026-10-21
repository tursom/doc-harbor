import { blobURL } from '../api'
import type { FileContent } from '../types'

// 将原组件隐式读取的当前文件改成显式参数，其余解析规则保持不变。
export function resolveMarkdownResourceURL(href: string, current: FileContent | null = null) {
  if (!current || isExternalMarkdownURL(href)) return href
  const resolved = resolveRepoRelativePath(current.file_path, href)
  return blobURL(current.repo_id, current.source_commit_sha, resolved, true)
}

export function resolveMarkdownLinkURL(href: string, current: FileContent | null = null) {
  if (isExternalMarkdownURL(href)) return href
  if (!current) return href
  const [pathPart, suffix] = splitMarkdownURLSuffix(href)
  if (!pathPart) return href
  const resolved = resolveRepoRelativePath(current.file_path, pathPart)
  return blobURL(current.repo_id, current.source_commit_sha, resolved, false) + suffix
}

export function isExternalMarkdownURL(href = '') {
  return /^(?:[a-z][a-z0-9+.-]*:|\/\/|#)/i.test(href)
}

export function splitMarkdownURLSuffix(href: string) {
  const hashIndex = href.indexOf('#')
  const queryIndex = href.indexOf('?')
  const indexes = [hashIndex, queryIndex].filter((index) => index >= 0)
  if (!indexes.length) return [href, ''] as const
  const splitAt = Math.min(...indexes)
  return [href.slice(0, splitAt), href.slice(splitAt)] as const
}

export function resolveRepoRelativePath(currentFilePath: string, href: string) {
  const [pathPart] = splitMarkdownURLSuffix(href)
  const baseDir = currentFilePath.split('/').slice(0, -1)
  const parts = pathPart.startsWith('/') ? [] : [...baseDir]
  for (const part of pathPart.split('/')) {
    if (!part || part === '.') continue
    if (part === '..') {
      parts.pop()
      continue
    }
    parts.push(part)
  }
  return parts.join('/')
}

export function isMarkdownContent(content: FileContent) {
  return content.extension === '.md' || content.extension === '.markdown'
}

export function escapeAttr(value: string) {
  return value.replace(/&/g, '&amp;').replace(/"/g, '&quot;').replace(/</g, '&lt;').replace(/>/g, '&gt;')
}
