import { blobURL } from '../api'
import type { HTMLPreviewResource } from '../html-preview'

export async function loadHTMLPreviewResource(
  repoID: number,
  commit: string,
  filePath: string
): Promise<HTMLPreviewResource> {
  const response = await fetch(blobURL(repoID, commit, filePath, true), { credentials: 'same-origin' })
  if (!response.ok) {
    throw new Error(`加载 HTML 预览资源失败：${filePath}（HTTP ${response.status}）`)
  }
  if (response.redirected && new URL(response.url, window.location.href).origin !== window.location.origin) {
    throw new Error(`HTML 预览资源鉴权失效：${filePath}`)
  }

  const blob = await response.blob()
  const mimeType = blob.type || response.headers.get('Content-Type') || 'application/octet-stream'
  const [dataURL, text] = await Promise.all([
    blobToDataURL(blob),
    isTextPreviewResource(filePath, mimeType) ? blob.text() : Promise.resolve('')
  ])
  return { dataURL, mimeType, text }
}

function blobToDataURL(blob: Blob) {
  return new Promise<string>((resolve, reject) => {
    const reader = new FileReader()
    reader.addEventListener('load', () => resolve(String(reader.result || '')))
    reader.addEventListener('error', () => reject(reader.error || new Error('读取 HTML 预览资源失败')))
    reader.readAsDataURL(blob)
  })
}

function isTextPreviewResource(filePath: string, mimeType: string) {
  return (
    /^text\//i.test(mimeType) ||
    /(?:javascript|json|xml|svg)/i.test(mimeType) ||
    /\.(?:css|html?|js|mjs|cjs|json|svg|txt|xml)$/i.test(filePath)
  )
}
