import { describe, expect, it } from 'vitest'
import { escapeAttr, resolveMarkdownLinkURL, resolveMarkdownResourceURL, resolveRepoRelativePath, splitMarkdownURLSuffix } from './markdown-links'
import type { FileContent } from '../types'

const content = { file_path: 'docs/guide/index.md', repo_id: 7, source_commit_sha: 'abc' } as FileContent
describe('Markdown 链接保留原解析规则', () => {
  it('处理相对路径、上级与仓库根路径', () => {
    expect(resolveRepoRelativePath(content.file_path, './images/a.png')).toBe('docs/guide/images/a.png')
    expect(resolveRepoRelativePath(content.file_path, '../readme.md')).toBe('docs/readme.md')
    expect(resolveRepoRelativePath(content.file_path, '../../../root.md')).toBe('root.md')
    expect(resolveRepoRelativePath(content.file_path, '/assets/a.svg?x=1#y')).toBe('assets/a.svg')
  })
  it.each(['https://example.test/x', '//example.test/x', 'mailto:a@example.test', '#heading', 'data:image/png;base64,a'])(
    '外链与锚点 %s 不改写', (url) => {
      expect(resolveMarkdownLinkURL(url, content)).toBe(url)
      expect(resolveMarkdownResourceURL(url, content)).toBe(url)
    }
  )
  it('图片使用 inline blob，普通链接保留后缀', () => {
    expect(resolveMarkdownResourceURL('../a.png?raw=1', content)).toBe('/api/repos/7/blob/download?commit_sha=abc&path=docs%2Fa.png&inline=1')
    expect(resolveMarkdownLinkURL('../a.md#标题', content)).toBe('/api/repos/7/blob/download?commit_sha=abc&path=docs%2Fa.md&inline=0#标题')
    expect(splitMarkdownURLSuffix('a.md?q=1#x')).toEqual(['a.md', '?q=1#x'])
    expect(resolveMarkdownLinkURL('?q=1', content)).toBe('?q=1')
    expect(resolveMarkdownLinkURL('a.md')).toBe('a.md')
  })
  it('属性文本转义', () => { expect(escapeAttr('"<&>')).toBe('&quot;&lt;&amp;&gt;') })
})
