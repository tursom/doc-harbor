import type { FileContent } from '../types'

type ContentType = Pick<FileContent, 'extension' | 'mime_type'>

export function isHTMLContent(content: ContentType) {
  return content.extension === '.html' || content.extension === '.htm' || /^text\/html\b/i.test(content.mime_type || '')
}
