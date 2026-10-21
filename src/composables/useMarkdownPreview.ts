import { nextTick, onBeforeUnmount, onMounted, ref, watch, type Ref } from 'vue'
import type { FileContent } from '../types'
import { isHTMLContent } from '../lib/content-type'
import { escapeAttr, isMarkdownContent, resolveMarkdownLinkURL, resolveMarkdownResourceURL } from '../lib/markdown-links'

export interface DocHeading { id: string; text: string; level: number }

/** 渲染依赖只在打开对应格式的文档后动态加载，根节点限定到当前阅读器。 */
export function useMarkdownPreview(content: Ref<FileContent | null>, root: Ref<HTMLElement | null>, isDark: Ref<boolean>) {
  const renderedHtml = ref('')
  const markdownLoading = ref(false)
  const markdownError = ref('')
  const htmlPreviewSrcdoc = ref('')
  const htmlPreviewLoading = ref(false)
  const htmlPreviewError = ref('')
  const headings = ref<DocHeading[]>([])
  const activeHeading = ref('')
  let markdownRequest = 0
  let htmlRequest = 0
  let observer: IntersectionObserver | null = null
  const retryTimers = new Set<ReturnType<typeof setTimeout>>()
  const imageCleanups: Array<() => void> = []

  function clearMarkdownEffects() {
    observer?.disconnect()
    observer = null
    retryTimers.forEach(clearTimeout)
    retryTimers.clear()
    imageCleanups.splice(0).forEach((cleanup) => cleanup())
    headings.value = []
    activeHeading.value = ''
  }

  async function rebuildMarkdownPreview() {
    const request = ++markdownRequest
    const current = content.value
    clearMarkdownEffects()
    // 主题变化也先卸下旧 DOM，确保已替换的 Mermaid SVG 从原始代码重新构建。
    renderedHtml.value = ''
    markdownError.value = ''
    markdownLoading.value = false
    if (!current?.previewable || !current.content || !isMarkdownContent(current)) return
    markdownLoading.value = true
    try {
      await nextTick()
      const [{ marked }, { default: DOMPurify }] = await Promise.all([import('marked'), import('dompurify')])
      if (request !== markdownRequest) return
      const renderer = new marked.Renderer()
      renderer.image = ({ href, title, text }) => {
        const src = resolveMarkdownResourceURL(href, current)
        const titleAttr = title ? ` title="${escapeAttr(title)}"` : ''
        return `<img data-doc-harbor-src="${escapeAttr(src)}" alt="${escapeAttr(text || '')}"${titleAttr}>`
      }
      renderer.link = ({ href, title, text }) => {
        const target = resolveMarkdownLinkURL(href, current)
        const titleAttr = title ? ` title="${escapeAttr(title)}"` : ''
        return `<a href="${escapeAttr(target)}"${titleAttr}>${text}</a>`
      }
      const html = marked.parse(current.content, { async: false, renderer }) as string
      renderedHtml.value = DOMPurify.sanitize(html, {
        ADD_TAGS: ['svg', 'path', 'g', 'marker', 'defs', 'linearGradient', 'stop', 'rect', 'circle', 'line', 'polyline', 'polygon', 'text', 'tspan'],
        ADD_ATTR: ['data-doc-harbor-src', 'viewBox', 'd', 'x', 'y', 'x1', 'x2', 'y1', 'y2', 'points', 'marker-end', 'stroke', 'fill', 'transform', 'class', 'style', 'target', 'rel']
      })
      await nextTick()
      if (request !== markdownRequest || !root.value) return
      armMarkdownImageRetries()
      // 表格单独滚动，长列不能撑开整个页面。
      for (const table of root.value.querySelectorAll('table')) {
        const wrapper = document.createElement('div')
        wrapper.className = 'markdown-table-scroll'
        table.before(wrapper)
        wrapper.append(table)
      }
      collectHeadings()
      await renderMermaid(request)
    } catch (err) {
      if (request === markdownRequest) markdownError.value = err instanceof Error ? err.message : String(err)
    } finally {
      if (request === markdownRequest) markdownLoading.value = false
    }
  }

  async function rebuildHTMLPreview(current: FileContent | null) {
    const request = ++htmlRequest
    htmlPreviewSrcdoc.value = ''
    htmlPreviewError.value = ''
    htmlPreviewLoading.value = Boolean(current?.previewable && current.content && isHTMLContent(current))
    if (!current?.previewable || !current.content || !isHTMLContent(current)) return
    try {
      const [{ buildHTMLPreviewSrcdoc }, { loadHTMLPreviewResource }] = await Promise.all([
        import('../html-preview'), import('../lib/html-preview-resource')
      ])
      const srcdoc = await buildHTMLPreviewSrcdoc(current, loadHTMLPreviewResource)
      if (request === htmlRequest) htmlPreviewSrcdoc.value = srcdoc
    } catch (err) {
      if (request === htmlRequest) htmlPreviewError.value = err instanceof Error ? err.message : String(err)
    } finally {
      if (request === htmlRequest) htmlPreviewLoading.value = false
    }
  }

  function armMarkdownImageRetries() {
    for (const image of root.value?.querySelectorAll<HTMLImageElement>('.markdown-body img') || []) {
      const source = image.dataset.docHarborSrc || image.getAttribute('src') || ''
      if (!source) continue
      image.dataset.retryCount = '0'
      const retry = () => {
        const retryCount = Number(image.dataset.retryCount || '0')
        if (retryCount >= 1) return
        image.dataset.retryCount = String(retryCount + 1)
        const retryURL = new URL(source, window.location.href)
        retryURL.searchParams.set('_retry', String(Date.now()))
        const timer = setTimeout(() => {
          retryTimers.delete(timer)
          if (image.isConnected) image.src = retryURL.toString()
        }, 120)
        retryTimers.add(timer)
      }
      image.addEventListener('error', retry, { once: true })
      imageCleanups.push(() => image.removeEventListener('error', retry))
      if (image.getAttribute('src') !== source) image.src = source
    }
  }

  function collectHeadings() {
    const elements = [...(root.value?.querySelectorAll<HTMLElement>('.markdown-body h2, .markdown-body h3') || [])]
    elements.forEach((heading, index) => { if (!heading.id) heading.id = `doc-heading-${index + 1}` })
    headings.value = elements.map((heading) => ({ id: heading.id, text: heading.textContent || '', level: Number(heading.tagName.slice(1)) }))
    activeHeading.value = headings.value[0]?.id || ''
    observeHeadings()
  }

  function observeHeadings() {
    observer?.disconnect()
    if (!root.value || headings.value.length < 2 || typeof IntersectionObserver === 'undefined') return
    const elements = [...root.value.querySelectorAll<HTMLElement>('.markdown-body h2, .markdown-body h3')]
    const reader = root.value.closest<HTMLElement>('.reading-area')
    // Shell 未限制高度或正文较短时实际由页面滚动，不能把不滚动的容器当作观察根。
    const scrollRoot = !window.matchMedia('(max-width: 860px)').matches && reader && reader.scrollHeight > reader.clientHeight ? reader : null
    // 同时跟踪标题跨过顶部的时刻；大段正文没有可见标题时，继续高亮其前一个标题。
    observer = new IntersectionObserver(() => {
      const top = (scrollRoot?.getBoundingClientRect().top || 0) + 48
      let current = elements[0]
      for (const heading of elements) {
        if (heading.getBoundingClientRect().top <= top) current = heading
        else break
      }
      activeHeading.value = current?.id || ''
    }, { root: scrollRoot, rootMargin: '-24px 0px -65% 0px', threshold: [0, 1] })
    elements.forEach((heading) => observer?.observe(heading))
  }

  async function renderMermaid(request: number) {
    const blocks = root.value?.querySelectorAll<HTMLElement>('.markdown-body code.language-mermaid')
    if (!blocks?.length) return
    const [{ default: mermaid }, { default: DOMPurify }] = await Promise.all([import('mermaid'), import('dompurify')])
    if (request !== markdownRequest) return
    const tokens = getComputedStyle(document.documentElement)
    const color = (name: string) => tokens.getPropertyValue(name).trim()
    mermaid.initialize({
      startOnLoad: false, securityLevel: 'strict', theme: isDark.value ? 'dark' : 'default', htmlLabels: false,
      themeVariables: {
        actorBkg: color('--surface-2'), actorBorder: color('--info'), actorLineColor: color('--info'), actorTextColor: color('--fg'),
        activationBkgColor: color('--info-bg'), activationBorderColor: color('--info'), labelBoxBkgColor: color('--surface'),
        labelBoxBorderColor: color('--border-strong'), labelTextColor: color('--fg'), loopTextColor: color('--fg'),
        noteBkgColor: color('--warn-bg'), noteBorderColor: color('--warn'), noteTextColor: color('--fg'),
        signalColor: color('--fg-muted'), signalTextColor: color('--fg'), sequenceNumberColor: color('--surface')
      }
    })
    let index = 0
    for (const block of blocks) {
      if (request !== markdownRequest) return
      const source = block.textContent || ''
      const pre = block.closest('pre')
      if (!pre) continue
      try {
        const result = await mermaid.render(`doc-harbor-mermaid-${Date.now()}-${request}-${index++}`, source)
        if (request !== markdownRequest || !pre.isConnected) return
        const wrapper = document.createElement('div')
        wrapper.className = 'mermaid-render'
        wrapper.innerHTML = DOMPurify.sanitize(result.svg, {
          USE_PROFILES: { svg: true, svgFilters: true }, ADD_TAGS: ['marker'],
          ADD_ATTR: ['alignment-baseline', 'dominant-baseline', 'marker-end', 'marker-start', 'text-anchor', 'viewBox', 'xlink:href']
        })
        pre.replaceWith(wrapper)
      } catch (err) {
        if (request !== markdownRequest) return
        pre.classList.add('mermaid-error')
        const note = document.createElement('div')
        note.className = 'mermaid-error-text'
        note.textContent = err instanceof Error ? err.message : 'Mermaid 渲染失败'
        pre.appendChild(note)
      }
    }
  }

  watch([content, isDark, root], () => { void rebuildMarkdownPreview() }, { immediate: true })
  watch(content, (value) => { void rebuildHTMLPreview(value) }, { immediate: true })
  onMounted(() => window.addEventListener('resize', observeHeadings))
  onBeforeUnmount(() => {
    markdownRequest++; htmlRequest++
    clearMarkdownEffects()
    window.removeEventListener('resize', observeHeadings)
  })
  return { renderedHtml, markdownLoading, markdownError, htmlPreviewSrcdoc, htmlPreviewLoading, htmlPreviewError, headings, activeHeading }
}
