import { afterEach, describe, expect, it, vi } from 'vitest'
import { APIRequestError, api } from '../api'
import type { Repository } from '../types'
import { useRepoForm } from './useRepoForm'

const repository = {
  id: 7, name: 'Example docs', slug: 'example', repo_url: 'https://example.test/docs.git',
  default_branch: 'main', tracked_branches: ['*'], latest_include_branches: ['*'], latest_exclude_branches: [],
  branch_priority: ['main'], sync_interval_seconds: 3600, max_file_size_bytes: 2097152, enabled: true,
  scan_paths: [{ path: 'docs', include_globs: ['*.md'], exclude_globs: ['private/*'], enabled: true }]
} as Repository

afterEach(() => vi.restoreAllMocks())
describe('仓库设置保存', () => {
  it('保留已有目录的 glob，仅将新目录按逗号或换行转换，并在回读成功后清除未保存状态', async () => {
    const update = vi.spyOn(api, 'updateRepo').mockImplementation(async (_id, payload) => ({ ...repository, ...payload }))
    const editor = useRepoForm()
    editor.reset(repository)
    editor.form.value.name = 'Updated docs'
    editor.scanPathsText.value = 'docs, handbook\n notes '
    editor.autoSync.value = false
    expect(editor.dirty.value).toBe(true)
    await editor.save()
    expect(update).toHaveBeenCalledWith(7, expect.objectContaining({
      name: 'Updated docs', sync_interval_seconds: -1,
      scan_paths: [repository.scan_paths[0],
        { path: 'handbook', include_globs: [], exclude_globs: [], enabled: true },
        { path: 'notes', include_globs: [], exclude_globs: [], enabled: true }]
    }))
    expect(editor.dirty.value).toBe(false)
  })

  it('接口校验失败保留草稿和修改状态，字段错误能就地展示', async () => {
    vi.spyOn(api, 'updateRepo').mockRejectedValue(new APIRequestError(422, '仓库地址不可用', { fields: { repo_url: '请输入可访问的仓库地址' } }))
    const editor = useRepoForm()
    editor.reset(repository)
    editor.form.value.repo_url = 'ssh://example.test/missing.git'
    expect(await editor.save()).toBeNull()
    expect(editor.form.value.repo_url).toBe('ssh://example.test/missing.git')
    expect(editor.dirty.value).toBe(true)
    expect(editor.fieldErrors.value.repo_url).toBe('请输入可访问的仓库地址')
    expect(editor.error.value).toBe('仓库地址不可用')
    expect(editor.saving.value).toBe(false)
  })

  it('关闭自动拉取不校验间隔，重新打开恢复3600秒；无效间隔不提交', async () => {
    const update = vi.spyOn(api, 'updateRepo')
    const editor = useRepoForm()
    editor.reset(repository)
    editor.autoSync.value = false
    editor.autoSync.value = true
    expect(editor.form.value.sync_interval_seconds).toBe(3600)
    editor.form.value.sync_interval_seconds = 30
    expect(await editor.save()).toBeNull()
    expect(update).not.toHaveBeenCalled()
    expect(editor.fieldErrors.value.sync_interval_seconds).toContain('60')
  })
})
