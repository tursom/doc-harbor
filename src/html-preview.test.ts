// @vitest-environment jsdom

import { describe, expect, it, vi } from 'vitest'
import { buildHTMLPreviewSrcdoc } from './html-preview'
import type { FileContent } from './types'

function fileContent(filePath: string, content: string): FileContent {
  return {
    repo_id: 5,
    source_commit_sha: 'c4c5649dd2f404631696272a538c5ea0064fab78',
    file_path: filePath,
    extension: '.html',
    mime_type: 'text/html; charset=utf-8',
    content
  } as FileContent
}

describe('buildHTMLPreviewSrcdoc', () => {
  it('starts resources from markup and inline scripts concurrently', async () => {
    let releaseResources = () => {}
    const resourceGate = new Promise<void>((resolve) => {
      releaseResources = resolve
    })
    const loadResource = vi.fn(async (_repoID: number, _commit: string, filePath: string) => {
      await resourceGate
      return {
        dataURL: `data:image/png;base64,${btoa(filePath)}`,
        mimeType: 'image/png',
        text: ''
      }
    })

    const rendering = buildHTMLPreviewSrcdoc(
      fileContent(
        'doc/page.html',
        '<!doctype html><html><body><img src="assets/hero.png"><script>const first="assets/first.png"; const second="assets/second.png"</script></body></html>'
      ),
      loadResource
    )

    await Promise.resolve()
    await Promise.resolve()
    const startedPaths = loadResource.mock.calls.map((call) => call[2]).sort()
    releaseResources()
    await rendering

    expect(startedPaths).toEqual(['doc/assets/first.png', 'doc/assets/hero.png', 'doc/assets/second.png'])
  })

  it('inlines local nested HTML and its local assets without network iframe requests', async () => {
    const loadResource = vi.fn(async (_repoID: number, _commit: string, filePath: string) => {
      if (filePath === 'doc/prototype.html') {
        return {
          dataURL: 'data:text/html;base64,PGgxPlByb3RvdHlwZTwvaDE+',
          mimeType: 'text/html; charset=utf-8',
          text:
            '<!doctype html><html><head><style>.art{background:url(\'data:image/svg+xml,<svg><path fill="url(%23g)"/></svg>\')}</style></head><body><img src="assets/item.png"><script>const itemImage="assets/item.png"; const downloadName="report.png"</script></body></html>'
        }
      }
      if (filePath === 'doc/assets/item.png') {
        return {
          dataURL: 'data:image/png;base64,aW1hZ2U=',
          mimeType: 'image/png',
          text: ''
        }
      }
      throw new Error(`unexpected resource: ${filePath}`)
    })

    const result = await buildHTMLPreviewSrcdoc(
      fileContent(
        'doc/wrapper.html',
        '<!doctype html><html><body><iframe src="prototype.html"></iframe><iframe src="prototype.html"></iframe><iframe src="prototype.html"></iframe></body></html>'
      ),
      loadResource
    )
    const doc = new DOMParser().parseFromString(result, 'text/html')
    const frames = [...doc.querySelectorAll('iframe')]
    const bootstrap = doc.querySelector<HTMLScriptElement>('script[data-doc-harbor-preview-bootstrap]')

    expect(frames).toHaveLength(3)
    expect(frames.every((frame) => !frame.hasAttribute('src'))).toBe(true)
    expect(frames.every((frame) => !frame.hasAttribute('srcdoc'))).toBe(true)
    expect(bootstrap).not.toBeNull()
    new Function('document', bootstrap?.textContent || '')(doc)
    expect(frames.every((frame) => frame.getAttribute('srcdoc')?.includes('data:image/png;base64,aW1hZ2U='))).toBe(true)
    expect(frames.every((frame) => !frame.getAttribute('srcdoc')?.includes('assets/item.png'))).toBe(true)
    expect(frames.every((frame) => frame.getAttribute('srcdoc')?.includes('report.png'))).toBe(true)
    expect(loadResource).toHaveBeenCalledTimes(2)
  })
})
