// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createApp, defineComponent, h, nextTick, ref, type App } from 'vue'
import { api } from '../api'
import { useHistory } from './useHistory'
import type { CommitDetail, RepoRef, Repository } from '../types'

const apps: App[] = []
const commit = (sha: string): CommitDetail => ({ sha, message: sha, parents: [], decorations: '', author: '', author_email: '', commit_time: '', files: [] })
const flush = async () => { for (let index = 0; index < 12; index++) await nextTick() }
function mount() {
  const selectedRepo = ref<Repository | null>({ id: 7 } as Repository)
  const branches = ref([{ ref_name: 'main' }] as RepoRef[])
  let state!: ReturnType<typeof useHistory>
  const host = document.createElement('div')
  document.body.append(host)
  const app = createApp(defineComponent({ setup() { state = useHistory({ selectedRepo, branches }); return () => h('div') } }))
  app.mount(host)
  apps.push(app)
  return state
}
beforeEach(() => {
  window.history.replaceState(null, '', '/history/?repo=7')
  vi.spyOn(api, 'history').mockResolvedValue({ items: [commit('newest')] })
  vi.spyOn(api, 'commit').mockImplementation(async (_, sha) => commit(sha))
})
afterEach(() => { apps.splice(0).forEach((app) => app.unmount()); document.body.replaceChildren(); vi.restoreAllMocks() })

describe('提交历史独立状态', () => {
  it('恢复不在最近列表中的提交，前进后退不新增历史', async () => {
    window.history.replaceState(null, '', '/history/?repo=7&branch=main&commit=older')
    const push = vi.spyOn(window.history, 'pushState')
    const state = mount()
    await flush()
    expect(api.history).toHaveBeenCalledWith(7, 'main', 120)
    expect(state.selectedCommit.value?.sha).toBe('older')
    window.history.replaceState(null, '', '/history/?repo=7&commit=another')
    window.dispatchEvent(new PopStateEvent('popstate'))
    await flush()
    expect(state.selectedCommit.value?.sha).toBe('another')
    expect(push).not.toHaveBeenCalled()
  })
  it('快速点击提交时忽略迟到的详情，并同步新 SHA', async () => {
    const state = mount()
    await flush()
    let resolve!: (value: CommitDetail) => void
    vi.mocked(api.commit).mockReturnValueOnce(new Promise((done) => { resolve = done }))
    const old = state.openCommit('old')
    await state.openCommit('new')
    resolve(commit('old'))
    await old
    expect(state.selectedCommit.value?.sha).toBe('new')
    expect(window.location.search).toContain('commit=new')
  })
})
