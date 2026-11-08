import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'
import { mockRedesign, files } from './fixtures/redesign'
import type { ScanRun } from '../src/types'

/** 在既有虚构仓库上模拟 202、阶段推进和取消，不访问真实 Git/数据库。 */
async function mockScanning(page: Page) {
  const state = await mockRedesign(page)
  state.repo.latest_scan = state.scanRuns[1]
  const submissions: { force: boolean }[] = []
  let cancellations = 0
  let fileReads = 0
  let branchReads = 0
  let generation = 0
  function start() {
    const run: ScanRun = { ...state.scanRuns[1]!, id: 91 + generation++, status: 'running', file_count: 0,
      error_count: 0, finished_at: '', error_message: '',
      detail_json: JSON.stringify({ progress: { stage: 'metadata', branch: 'main', processed_files: 12, total_files: 20 } }) }
    state.repo.latest_scan = run
    state.scanRuns.unshift(run)
    return run
  }
  function finish(status = 'success', message = '') {
    Object.assign(state.repo.latest_scan!, { status, file_count: status === 'success' ? 20 : 0, error_message: message,
      finished_at: '2026-11-08T08:00:12Z', detail_json: JSON.stringify({ progress: { stage: 'finished', branch: 'main', processed_files: 20, total_files: 20 } }) })
  }
  await page.route('**/api/repos/7/scan', async route => {
    submissions.push(route.request().postDataJSON())
    return route.fulfill({ status: 202, json: start() })
  })
  await page.route('**/api/repos/7/scan/cancel', async route => {
    cancellations++
    return route.fulfill({ json: state.repo.latest_scan })
  })
  await page.route('**/api/repos/7/files?*', async route => {
    fileReads++
    return route.fulfill({ json: { items: files } })
  })
  await page.route('**/api/repos/7/branches', async route => {
    branchReads++
    return route.fulfill({ json: { items: ['main', 'feature/release-guide', ...(state.repo.latest_scan?.status === 'success' && generation ? ['release/new'] : [])]
      .map((ref_name, id) => ({ id, ref_name })) } })
  })
  return { state, submissions, start, finish, get cancellations() { return cancellations }, get fileReads() { return fileReads }, get branchReads() { return branchReads } }
}

test('增量后台扫描、进度轮询和完成刷新均保留当前阅读', async ({ page }, info) => {
  const mock = await mockScanning(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/?repo=7&view=branch&branch=main&dir=docs&version=42')
  await expect(page.getByRole('heading', { name: '发布前检查', exact: true })).toBeVisible()
  const initialFiles = mock.fileReads
  const initialBranches = mock.branchReads
  await page.getByRole('button', { name: '立即扫描', exact: true }).click()
  await expect(page.getByRole('button', { name: '取消扫描', exact: true })).toBeVisible()
  expect(mock.submissions).toEqual([{ force: false }])
  await expect(page.locator('.repo-header')).toContainText('准备文件 12 / 20')
  await expect(page.locator('.repo-header')).toContainText('已提交 0 个文件')
  const reads = mock.state.repoReads
  await expect.poll(() => mock.state.repoReads).toBeGreaterThan(reads)
  expect(mock.fileReads).toBe(initialFiles)
  await expect(page.getByRole('heading', { name: '发布前检查', exact: true })).toBeVisible()
  await page.screenshot({ path: info.outputPath('scan-running-reading-desktop.png'), fullPage: true })
  mock.finish()
  await expect(page.getByRole('button', { name: '立即扫描', exact: true })).toBeVisible()
  await expect.poll(() => mock.fileReads).toBeGreaterThan(initialFiles)
  expect(mock.branchReads).toBeGreaterThan(initialBranches)
  await expect(page.getByLabel('分支', { exact: true }).locator('option')).toHaveText(['main', 'feature/release-guide', 'release/new'])
  await expect(page.getByLabel('分支', { exact: true })).toHaveValue('main')
  await expect(page.getByRole('heading', { name: '发布前检查', exact: true })).toBeVisible()
  await expect(page).toHaveURL(/version=42/)
  expect(mock.state.unexpected).toEqual([])
})

test('完整重建必须内联确认；取消等待终态并可再次扫描', async ({ page }, info) => {
  const mock = await mockScanning(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/scans/?repo=7')
  await page.getByRole('button', { name: '完整重建', exact: true }).click()
  expect(mock.submissions).toEqual([])
  const confirmation = page.getByRole('group', { name: '按当前文档范围完整重建索引' })
  await expect(confirmation).toBeVisible()
  await confirmation.getByRole('button', { name: '完整重建', exact: true }).click()
  await expect(page.getByRole('button', { name: '取消扫描', exact: true })).toBeVisible()
  expect(mock.submissions).toEqual([{ force: true }])
  await expect(page.locator('.scan-table tbody > tr').first()).toContainText('准备文件 12 / 20')
  await page.getByRole('button', { name: '取消扫描', exact: true }).click()
  await expect(page.getByRole('button', { name: '正在取消…', exact: true })).toBeDisabled()
  expect(mock.cancellations).toBe(1)
  await expect(page.locator('.repo-title')).toContainText('扫描中')
  await expect(page.getByRole('button', { name: '立即扫描', exact: true })).toHaveCount(0)
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(true)
  await page.screenshot({ path: info.outputPath('scan-cancelling-mobile.png'), fullPage: true })
  mock.finish('failed', '扫描已取消（虚构 fixture）')
  await expect(page.getByRole('button', { name: '立即扫描', exact: true })).toBeVisible()
  await expect(page.locator('.scan-error')).toHaveText('扫描已取消（虚构 fixture）')
  await page.reload()
  await expect(page.locator('.scan-error')).toHaveText('扫描已取消（虚构 fixture）')
  await page.getByRole('button', { name: '立即扫描', exact: true }).click()
  await expect(page.getByRole('button', { name: '取消扫描', exact: true })).toBeVisible()
  expect(mock.submissions).toEqual([{ force: true }, { force: false }])
  expect(mock.state.unexpected).toEqual([])
})

test('页面重新打开恢复 running，重启中断恢复 failed，兼容 cancelled', async ({ page }, info) => {
  const mock = await mockScanning(page)
  mock.start()
  await page.goto('/scans/?repo=7')
  await expect(page.getByRole('button', { name: '取消扫描', exact: true })).toBeVisible()
  await page.reload()
  await expect(page.locator('.repo-header')).toContainText('准备文件 12 / 20')
  mock.finish('failed', '服务重启，扫描中断（虚构 fixture）')
  await page.reload()
  await expect(page.locator('.scan-error')).toContainText('服务重启，扫描中断')
  await expect(page.getByRole('button', { name: '立即扫描', exact: true })).toBeVisible()
  mock.finish('cancelled', '扫描已取消')
  await page.reload()
  await expect(page.locator('.repo-title')).toContainText('已取消')
  await page.screenshot({ path: info.outputPath('scan-restored-terminal.png'), fullPage: true })
  expect(mock.state.unexpected).toEqual([])
})
