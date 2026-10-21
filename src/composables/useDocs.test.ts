// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { api } from '../api'
import { useDocs } from './useDocs'
import type { FileContent, RepoRef, Repository } from '../types'

const apps: App[] = []
const repo = (id = 7) => ({ id, default_branch: 'main', scan_paths: [{ path: 'docs', enabled: true }] }) as Repository
const content = (version_id: number) => ({ version_id, document_id: version_id, repo_id: 7, file_path: `docs/${version_id}.md` }) as FileContent
const flush = async () => { for (let index = 0; index < 12; index++) await nextTick() }
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>((done) => { resolve = done }); return { promise, resolve } }
function mount() {
  const selectedRepo = ref<Repository | null>(repo())
  const branches = ref([{ ref_name: 'main' }, { ref_name: 'feature/docs' }] as RepoRef[])
  let docs!: ReturnType<typeof useDocs>
  const host = document.createElement('div')
  document.body.append(host)
  const app = createApp(defineComponent({ setup() { docs = useDocs({ selectedRepo, branches }); return () => h('div') } }))
  app.mount(host)
  apps.push(app)
  return { docs, selectedRepo }
}
beforeEach(() => {
  window.history.replaceState(null, '', '/?repo=7')
  vi.spyOn(api, 'files').mockResolvedValue({ items: [] })
  vi.spyOn(api, 'content').mockImplementation(async (_, id) => content(id))
  vi.spyOn(api, 'docHistory').mockResolvedValue({ document: {}, versions: [], events: [] })
})
afterEach(() => { apps.splice(0).forEach((app) => app.unmount()); document.body.replaceChildren(); vi.restoreAllMocks() })

describe('文档页面状态', () => {
  it('恢复分支和版本深链接，自动加载路径事件，popstate 不新增 URL', async () => {
    window.history.replaceState(null, '', '/?repo=7&view=branch&branch=feature%2Fdocs&dir=docs&version=42')
    const push = vi.spyOn(window.history, 'pushState')
    const { docs } = mount()
    await flush()
    expect(api.files).toHaveBeenCalledWith(7, { view: 'branch', branch: 'feature/docs', dir: 'docs' })
    expect(docs.fileContent.value?.version_id).toBe(42)
    expect(api.docHistory).toHaveBeenCalledWith(7, 42)
    expect(docs.reading.value).toBe(true)
    window.history.replaceState(null, '', '/?repo=7&view=latest&dir=other')
    window.dispatchEvent(new PopStateEvent('popstate'))
    await flush()
    expect(docs.currentDir.value).toBe('other')
    expect(docs.fileContent.value).toBeNull()
    expect(push).not.toHaveBeenCalled()
  })
  it('切换仓库会忽略旧列表响应', async () => {
    const old = deferred<{ items: [] }>()
    vi.mocked(api.files).mockReturnValueOnce(old.promise).mockResolvedValue({ items: [{ kind: 'dir', name: 'new', path: 'new' }] })
    const { docs, selectedRepo } = mount()
    window.history.replaceState(null, '', '/?repo=8')
    selectedRepo.value = repo(8)
    await flush()
    old.resolve({ items: [] })
    await flush()
    expect(docs.files.value[0]?.path).toBe('new')
  })
  it('快速切换版本不能被迟到的正文覆盖，移动返回会清除 version', async () => {
    const { docs } = mount()
    await flush()
    const old = deferred<FileContent>()
    vi.mocked(api.content).mockReturnValueOnce(old.promise)
    const first = docs.openVersion(41)
    await docs.openVersion(42)
    old.resolve(content(41))
    await first
    expect(docs.fileContent.value?.version_id).toBe(42)
    expect(window.location.search).toContain('version=42')
    docs.backToFiles()
    expect(docs.reading.value).toBe(false)
    expect(window.location.search).not.toContain('version=')
  })
  it('路径事件加载失败仍保留正文与深链接', async () => {
    const { docs } = mount()
    await flush()
    vi.mocked(api.docHistory).mockRejectedValueOnce(new Error('events unavailable'))
    await docs.openVersion(42)
    expect(docs.fileContent.value?.version_id).toBe(42)
    expect(docs.eventsError.value).toBe('events unavailable')
    expect(docs.error.value).toBe('')
    expect(window.location.search).toContain('version=42')
  })
})
