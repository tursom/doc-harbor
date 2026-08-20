import { expect, test } from '@playwright/test'
import type { Page } from '@playwright/test'

async function mockEmptyRepositories(page: Page) {
  await page.route('**/api/repos', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [] }) })
  )
}

async function mockHistory(page: Page) {
  const commits = [
    {
      sha: 'newer-sha',
      message: 'Newer commit',
      author: 'Alice',
      author_email: 'alice@example.test',
      commit_time: '2026-08-20T03:00:00Z',
      parents: ['target-sha'],
      decorations: 'HEAD -> main'
    },
    {
      sha: 'target-sha',
      message: 'Deep-linked commit',
      author: 'Bob',
      author_email: 'bob@example.test',
      commit_time: '2026-08-19T03:00:00Z',
      parents: [],
      decorations: ''
    }
  ]
  await page.route('**/api/repos', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      body: JSON.stringify({
        items: [
          {
            id: 7,
            name: 'Docs',
            repo_url: 'https://example.test/docs.git',
            default_branch: 'main',
            tracked_branches: ['*'],
            latest_include_branches: ['*'],
            latest_exclude_branches: [],
            branch_priority: ['main'],
            scan_paths: []
          }
        ]
      })
    })
  )
  await page.route('**/api/repos/7/branches', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [{ ref_name: 'main' }] }) })
  )
  await page.route('**/api/repos/7/files?**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: [] }) })
  )
  await page.route('**/api/repos/7/history?**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ items: commits }) })
  )
  await page.route('**/api/repos/7/commits/*', (route) => {
    const sha = route.request().url().split('/').pop() || ''
    const commit = commits.find((item) => item.sha === sha)
    return route.fulfill({
      status: commit ? 200 : 404,
      contentType: 'application/json',
      body: JSON.stringify(commit ? { ...commit, files: [] } : { error: 'not found' })
    })
  })
}

test('static history page remains meaningful without JavaScript', async ({ browser }) => {
  const context = await browser.newContext({ javaScriptEnabled: false })
  const page = await context.newPage()
  await page.goto('/history/')
  await expect(page).toHaveTitle('Git 历史 · DocHarbor')
  await expect(page.getByRole('heading', { name: 'DocHarbor' })).toBeVisible()
  await expect(page.getByRole('heading', { name: 'Git 历史' })).toBeVisible()
  await expect(page.getByRole('link', { name: 'AI 问答' })).toHaveAttribute('href', '/ai/')
  await context.close()
})

test('global navigation performs a real page navigation', async ({ page }) => {
  await mockEmptyRepositories(page)
  await page.goto('/history/')
  await page.getByRole('link', { name: 'AI 问答' }).click()
  await expect(page).toHaveURL(/\/ai\/$/)
  await expect(page).toHaveTitle('AI 问答 · DocHarbor')
  await expect(page.getByRole('heading', { name: 'AI 问答' })).toBeVisible()
})

test('legacy tab URL redirects to its static page and preserves context', async ({ page }) => {
  await mockEmptyRepositories(page)
  await page.goto('/?tab=history&repo=7&branch=release%2F1')
  await expect(page).toHaveURL(/\/history\/\?repo=7&branch=release%2F1$/)
})

test('history first load does not request document rendering dependencies', async ({ page }) => {
  await mockEmptyRepositories(page)
  const scripts: string[] = []
  page.on('request', (request) => {
    if (request.resourceType() === 'script') scripts.push(request.url())
  })
  await page.goto('/history/')
  await expect(page.getByRole('heading', { name: 'Git 历史' })).toBeVisible()
  expect(scripts.join('\n')).not.toMatch(/mermaid|marked|purify|html-preview/i)
})

test('history page restores a commit deep link and updates it when a row is selected', async ({ page }) => {
  await mockHistory(page)
  await page.goto('/history/?repo=7&commit=target-sha')
  await expect(page.getByRole('heading', { name: 'Deep-linked commit' })).toBeVisible()
  await expect(page).toHaveURL(/commit=target-sha/)

  await page.getByRole('button', { name: /Newer commit/ }).click()
  await expect(page.getByRole('heading', { name: 'Newer commit' })).toBeVisible()
  await expect(page).toHaveURL(/commit=newer-sha/)
})

test('mobile AI navigation exposes every global page without horizontal overflow', async ({ page }) => {
  await page.setViewportSize({ width: 390, height: 844 })
  await mockEmptyRepositories(page)
  await page.goto('/ai/')
  const navigation = page.getByRole('navigation', { name: '全局功能' })
  for (const name of ['AI 问答', 'AI 配置', 'AI 诊断', '系统设置']) {
    await expect(navigation.getByRole('link', { name })).toBeVisible()
  }
  expect(await page.locator('body').evaluate((body) => body.scrollWidth === body.clientWidth)).toBe(true)
})

test('standalone HTML preview reports missing parameters without loading the main app', async ({ page }) => {
  const scripts: string[] = []
  page.on('request', (request) => {
    if (request.resourceType() === 'script') scripts.push(request.url())
  })
  await page.goto('/html-preview/')
  await expect(page.getByRole('heading', { name: 'HTML 预览不可用' })).toBeVisible()
  await expect(page.getByText('缺少 repo 或 version 参数')).toBeVisible()
  expect(scripts.join('\n')).not.toMatch(/\/_astro\/App\./)
})
