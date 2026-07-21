import { inlineBlobURL } from './api'
import type { FileContent } from './types'

export interface HTMLPreviewResource {
  dataURL: string
  mimeType: string
  text: string
}

export type HTMLPreviewResourceLoader = (
  repoID: number,
  commit: string,
  filePath: string
) => Promise<HTMLPreviewResource>

const maxNestedHTMLDepth = 8
const embeddedFrameAttribute = 'data-doc-harbor-embedded-frame'

export async function buildHTMLPreviewSrcdoc(
  content: FileContent,
  loadResource: HTMLPreviewResourceLoader
) {
  if (!content.content || !isHTMLContent(content)) return ''

  const resourceCache = new Map<string, Promise<HTMLPreviewResource>>()
  const documentCache = new Map<string, Promise<string>>()
  const loadCached = (filePath: string) => {
    let pending = resourceCache.get(filePath)
    if (!pending) {
      pending = loadResource(content.repo_id, content.source_commit_sha, filePath)
      resourceCache.set(filePath, pending)
    }
    return pending
  }

  const renderDocument = async (
    filePath: string,
    html: string,
    ancestors: ReadonlySet<string>,
    depth: number
  ): Promise<string> => {
    const parser = new DOMParser()
    const doc = parser.parseFromString(html, 'text/html')
    const embeddedDocuments = new Map<string, { id: number; html: Promise<string> }>()

    await Promise.all([
      ...[...doc.querySelectorAll<HTMLIFrameElement>('iframe[src]')].map(async (frame) => {
        const source = frame.getAttribute('src') || ''
        const resolved = resolveLocalResourcePath(filePath, source)
        if (!resolved) return
        if (depth >= maxNestedHTMLDepth) {
          throw new Error(`HTML preview nesting exceeds ${maxNestedHTMLDepth} levels at ${resolved.filePath}`)
        }
        if (ancestors.has(resolved.filePath)) {
          throw new Error(`HTML preview contains a circular iframe reference: ${resolved.filePath}`)
        }

        const resource = await loadCached(resolved.filePath)
        if (!isHTMLResource(resolved.filePath, resource.mimeType)) {
          frame.src = resource.dataURL
          return
        }

        let embedded = embeddedDocuments.get(resolved.filePath)
        if (!embedded) {
          const nextAncestors = new Set(ancestors)
          nextAncestors.add(resolved.filePath)
          let nested = documentCache.get(resolved.filePath)
          if (!nested) {
            nested = renderDocument(resolved.filePath, resource.text, nextAncestors, depth + 1)
            documentCache.set(resolved.filePath, nested)
          }
          embedded = { id: embeddedDocuments.size, html: nested }
          embeddedDocuments.set(resolved.filePath, embedded)
        }
        frame.setAttribute(embeddedFrameAttribute, String(embedded.id))
        frame.removeAttribute('src')
        frame.removeAttribute('srcdoc')
      }),
      ...[...doc.querySelectorAll<HTMLElement>('[src]:not(iframe)')].map((element) =>
        inlineResourceAttribute(element, 'src', filePath, loadCached)
      ),
      ...[...doc.querySelectorAll<HTMLElement>('[poster]')].map((element) =>
        inlineResourceAttribute(element, 'poster', filePath, loadCached)
      ),
      ...[...doc.querySelectorAll<HTMLElement>('object[data]')].map((element) =>
        inlineResourceAttribute(element, 'data', filePath, loadCached)
      ),
      ...[...doc.querySelectorAll<HTMLElement>('use[href]')].map((element) =>
        inlineResourceAttribute(element, 'href', filePath, loadCached)
      ),
      ...[...doc.querySelectorAll<HTMLElement>('use[xlink\\:href]')].map((element) =>
        inlineResourceAttribute(element, 'xlink:href', filePath, loadCached)
      ),
      ...[...doc.querySelectorAll<HTMLElement>('[srcset]')].map(async (element) => {
        const value = element.getAttribute('srcset')
        if (!value) return
        element.setAttribute('srcset', await inlineSrcset(value, filePath, loadCached))
      }),
      ...[...doc.querySelectorAll<HTMLElement>('[style]')].map(async (element) => {
        const value = element.getAttribute('style')
        if (!value) return
        element.setAttribute('style', await inlineCSSURLs(value, filePath, loadCached))
      }),
      ...[...doc.querySelectorAll<HTMLStyleElement>('style')].map(async (element) => {
        element.textContent = await inlineCSSURLs(element.textContent || '', filePath, loadCached)
      }),
      ...[...doc.querySelectorAll<HTMLScriptElement>('script:not([src])')].map(async (element) => {
        element.textContent = await inlineScriptResourceStrings(element.textContent || '', filePath, loadCached)
      }),
      ...[...doc.querySelectorAll<HTMLLinkElement>('link[href]')].map(async (link) => {
        const source = link.getAttribute('href') || ''
        const resolved = resolveLocalResourcePath(filePath, source)
        if (!resolved) return
        const resource = await loadCached(resolved.filePath)
        if (link.relList.contains('stylesheet')) {
          const style = doc.createElement('style')
          style.textContent = await inlineCSSURLs(resource.text, resolved.filePath, loadCached)
          link.replaceWith(style)
          return
        }
        link.href = resource.dataURL + dataURLSuffix(resolved.suffix)
      })
    ])

    for (const anchor of doc.querySelectorAll<HTMLAnchorElement>('a[href]')) {
      const source = anchor.getAttribute('href') || ''
      const resolved = resolveLocalResourcePath(filePath, source)
      if (!resolved) continue
      anchor.href =
        inlineBlobURL(content.repo_id, content.source_commit_sha, resolved.filePath) + resolved.suffix
    }

    await appendEmbeddedFrameBootstrap(doc, embeddedDocuments)

    return '<!doctype html>\n' + doc.documentElement.outerHTML
  }

  return renderDocument(content.file_path, content.content, new Set([content.file_path]), 0)
}

async function appendEmbeddedFrameBootstrap(
  doc: Document,
  embeddedDocuments: Map<string, { id: number; html: Promise<string> }>
) {
  if (!embeddedDocuments.size) return
  const ordered = [...embeddedDocuments.values()].sort((left, right) => left.id - right.id)
  const documents = await Promise.all(ordered.map((item) => item.html))
  const serialized = JSON.stringify(documents)
    .replace(/</g, '\\u003c')
    .replace(/\u2028/g, '\\u2028')
    .replace(/\u2029/g, '\\u2029')
  const script = doc.createElement('script')
  script.setAttribute('data-doc-harbor-preview-bootstrap', '')
  script.textContent = `(() => { const documents = ${serialized}; for (const frame of document.querySelectorAll('iframe[${embeddedFrameAttribute}]')) { const index = Number(frame.getAttribute('${embeddedFrameAttribute}')); frame.srcdoc = documents[index] || ''; frame.removeAttribute('${embeddedFrameAttribute}'); } })();`
  doc.body.append(script)
}

async function inlineResourceAttribute(
  element: HTMLElement,
  attribute: string,
  currentFilePath: string,
  loadResource: (filePath: string) => Promise<HTMLPreviewResource>
) {
  const source = element.getAttribute(attribute) || ''
  const resolved = resolveLocalResourcePath(currentFilePath, source)
  if (!resolved) return
  const resource = await loadResource(resolved.filePath)
  element.setAttribute(attribute, resource.dataURL + dataURLSuffix(resolved.suffix))
}

async function inlineSrcset(
  value: string,
  currentFilePath: string,
  loadResource: (filePath: string) => Promise<HTMLPreviewResource>
) {
  return (
    await Promise.all(
      value.split(',').map(async (candidate) => {
        const trimmed = candidate.trim()
        if (!trimmed) return trimmed
        const parts = trimmed.split(/\s+/)
        const source = parts.shift() || ''
        const resolved = resolveLocalResourcePath(currentFilePath, source)
        if (!resolved) return trimmed
        const resource = await loadResource(resolved.filePath)
        return [resource.dataURL + dataURLSuffix(resolved.suffix), ...parts].join(' ')
      })
    )
  ).join(', ')
}

async function inlineCSSURLs(
  value: string,
  currentFilePath: string,
  loadResource: (filePath: string) => Promise<HTMLPreviewResource>
) {
  const pattern = /url\(\s*(['"]?)([^)'"\s]+)\1\s*\)/gi
  return replaceMatches(value, pattern, async (match) => {
    const source = match[2]
    const resolved = resolveLocalResourcePath(currentFilePath, source)
    if (!resolved) {
      return match[0]
    }
    const resource = await loadResource(resolved.filePath)
    return `url(${match[1]}${resource.dataURL + dataURLSuffix(resolved.suffix)}${match[1]})`
  })
}

async function inlineScriptResourceStrings(
  value: string,
  currentFilePath: string,
  loadResource: (filePath: string) => Promise<HTMLPreviewResource>
) {
  const pattern = /(['"`])((?!\/|[a-z][a-z0-9+.-]*:)(?:[^'"`\s]*\/)+[^'"`\s]+\.(?:avif|gif|ico|jpe?g|png|svg|webp|woff2?|ttf|otf)(?:[?#][^'"`\s]*)?)\1/gi
  return replaceMatches(value, pattern, async (match) => {
    const resolved = resolveLocalResourcePath(currentFilePath, match[2])
    if (!resolved) {
      return match[0]
    }
    const resource = await loadResource(resolved.filePath)
    return `${match[1]}${resource.dataURL + dataURLSuffix(resolved.suffix)}${match[1]}`
  })
}

async function replaceMatches(
  value: string,
  pattern: RegExp,
  replacer: (match: RegExpMatchArray) => Promise<string>
) {
  const matches = [...value.matchAll(pattern)]
  const replacements = await Promise.all(matches.map(replacer))
  let rewritten = ''
  let offset = 0
  for (const [index, match] of matches.entries()) {
    const matchIndex = match.index ?? 0
    rewritten += value.slice(offset, matchIndex) + replacements[index]
    offset = matchIndex + match[0].length
  }
  return rewritten + value.slice(offset)
}

function resolveLocalResourcePath(currentFilePath: string, source: string) {
  const trimmed = source.trim()
  if (!trimmed || isExternalURL(trimmed)) return null
  const [pathPart, suffix] = splitURLSuffix(trimmed)
  if (!pathPart) return null
  return {
    filePath: resolveRepoRelativePath(currentFilePath, pathPart),
    suffix
  }
}

function dataURLSuffix(suffix: string) {
  return suffix.startsWith('#') ? suffix : ''
}

function isHTMLContent(content: FileContent) {
  return isHTMLResource(content.file_path, content.mime_type)
}

function isHTMLResource(filePath: string, mimeType: string) {
  return /\.html?$/i.test(filePath) || /^text\/html\b/i.test(mimeType)
}

function isExternalURL(href = '') {
  return /^(?:[a-z][a-z0-9+.-]*:|\/\/|#|%23)/i.test(href)
}

function splitURLSuffix(href: string) {
  const hashIndex = href.indexOf('#')
  const queryIndex = href.indexOf('?')
  const indexes = [hashIndex, queryIndex].filter((index) => index >= 0)
  if (!indexes.length) return [href, ''] as const
  const splitAt = Math.min(...indexes)
  return [href.slice(0, splitAt), href.slice(splitAt)] as const
}

function resolveRepoRelativePath(currentFilePath: string, href: string) {
  const baseDir = currentFilePath.split('/').slice(0, -1)
  const parts = href.startsWith('/') ? [] : [...baseDir]
  for (const part of href.split('/')) {
    if (!part || part === '.') continue
    if (part === '..') {
      parts.pop()
      continue
    }
    parts.push(part)
  }
  return parts.join('/')
}
