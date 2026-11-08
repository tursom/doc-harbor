// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, type App } from 'vue'
import { api } from '../api'
import { provideRepos } from './useRepos'
import { useDocs } from './useDocs'
import type { Repository, ScanRun } from '../types'

const run = (status = 'running', id = 91) => ({ id, repo_id: 7, status, file_count: 0, detail_json: '{}' }) as ScanRun
const repo = (id = 7, latest_scan = run()) => ({ id, default_branch: 'main', latest_scan,
  scan_paths: [{ path: 'docs', enabled: true }], document_count: 31 }) as Repository
const apps: App[] = []
const flush = async () => { for (let index = 0; index < 20; index++) await nextTick() }
function deferred<T>() { let resolve!: (value: T) => void; const promise = new Promise<T>((done) => { resolve = done }); return { promise, resolve } }
function mount() {
  let repos!: ReturnType<typeof provideRepos>
  let docs!: ReturnType<typeof useDocs>
  const app = createApp(defineComponent({ setup() { repos = provideRepos('docs'); docs = useDocs(repos); return () => h('div') } }))
  app.mount(document.createElement('div'))
  apps.push(app)
  return { get repos() { return repos }, get docs() { return docs } }
}
beforeEach(() => {
  vi.useFakeTimers()
  window.history.replaceState(null, '', '/?repo=7&dir=docs&version=42')
  vi.spyOn(api, 'repos').mockImplementation(async () => ({ items: [repo()] }))
  vi.spyOn(api, 'branches').mockResolvedValue({ items: [] })
  vi.spyOn(api, 'files').mockResolvedValue({ items: [] })
  vi.spyOn(api, 'content').mockResolvedValue({ version_id: 42, document_id: 11, repo_id: 7, file_path: 'docs/a.md' } as never)
  vi.spyOn(api, 'docHistory').mockResolvedValue({ document: {}, versions: [], events: [] })
  vi.spyOn(api, 'scanRepo').mockResolvedValue(run())
  vi.spyOn(api, 'cancelScan').mockResolvedValue(run())
})
afterEach(() => { apps.splice(0).forEach(app => app.unmount()); vi.restoreAllMocks(); vi.useRealTimers() })

describe('后台扫描状态', () => {
  it('进度轮询保留仓库和阅读对象，终态只刷新分支和当前文件列表', async () => {
    const { repos, docs } = mount()
    await flush()
    const selected = repos.selectedRepo.value
    const content = docs.fileContent.value
    vi.mocked(api.repos).mockResolvedValue({ items: [repo(7, { ...run(), detail_json: '{"progress":{"stage":"content"}}' })] })
    await vi.advanceTimersByTimeAsync(1500)
    expect(repos.selectedRepo.value).toBe(selected)
    expect(docs.fileContent.value).toBe(content)
    expect(api.files).toHaveBeenCalledTimes(1)
    vi.mocked(api.repos).mockResolvedValue({ items: [repo(7, run('success'))] })
    await vi.advanceTimersByTimeAsync(1500)
    expect(api.branches).toHaveBeenCalledTimes(2)
    expect(api.files).toHaveBeenCalledTimes(2)
    expect(api.files).toHaveBeenLastCalledWith(7, { view: 'latest', dir: 'docs' })
    expect(docs.fileContent.value).toBe(content)
    expect(api.content).toHaveBeenCalledTimes(1)
    expect(window.location.search).toContain('version=42')
  })
  it('202 提交后释放提交状态，普通增量与强制重建分别传 false / true', async () => {
    vi.mocked(api.repos).mockResolvedValue({ items: [repo(7, run('success', 90))] })
    const { repos } = mount()
    await flush()
    await repos.scanSelected()
    expect(api.scanRepo).toHaveBeenLastCalledWith(7, false)
    expect(repos.submitting.value).toBe(false)
    expect(repos.scanRunning.value).toBe(true)
    await repos.scanSelected(true)
    expect(api.scanRepo).toHaveBeenCalledTimes(1)
    vi.mocked(api.repos).mockResolvedValue({ items: [repo(7, run('success'))] })
    await vi.advanceTimersByTimeAsync(1500)
    await repos.scanSelected(true)
    expect(api.scanRepo).toHaveBeenLastCalledWith(7, true)
  })
  it('取消返回 running 时继续等待，服务端 failed 才显示终态和原因', async () => {
    const { repos } = mount()
    await flush()
    await repos.cancelScan()
    expect(repos.cancelRequested.value).toBe(true)
    expect(repos.scanRunning.value).toBe(true)
    await repos.cancelScan()
    expect(api.cancelScan).toHaveBeenCalledTimes(1)
    vi.mocked(api.repos).mockResolvedValue({ items: [repo(7, { ...run('failed'), error_message: '扫描已取消' })] })
    await vi.advanceTimersByTimeAsync(1500)
    expect(repos.scanRunning.value).toBe(false)
    expect(repos.cancelRequested.value).toBe(false)
    expect(repos.selectedRepo.value?.latest_scan?.error_message).toBe('扫描已取消')
  })
  it('忽略切仓库前的轮询与提交响应，卸载清理定时器', async () => {
    const { repos } = mount()
    await flush()
    const old = deferred<{ items: Repository[] }>()
    vi.mocked(api.repos).mockReturnValueOnce(old.promise)
    await vi.advanceTimersByTimeAsync(1500)
    await repos.selectRepo(repo(8, run('success', 80)))
    old.resolve({ items: [repo(7, run('failed'))] })
    await flush()
    expect(repos.selectedRepo.value?.id).toBe(8)
    expect(repos.selectedRepo.value?.latest_scan?.id).toBe(80)
    const submission = deferred<ScanRun>()
    vi.mocked(api.scanRepo).mockReturnValueOnce(submission.promise)
    const pending = repos.scanSelected()
    await repos.selectRepo(repo(7, run('success', 90)))
    submission.resolve(run())
    await pending
    expect(repos.selectedRepo.value?.latest_scan?.id).toBe(90)
    apps.splice(0).forEach(app => app.unmount())
    const reads = vi.mocked(api.repos).mock.calls.length
    await vi.advanceTimersByTimeAsync(6000)
    expect(api.repos).toHaveBeenCalledTimes(reads)
  })
})
