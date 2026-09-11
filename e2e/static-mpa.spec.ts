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

// 使用远超侧栏宽度的真实分支形态，并检查几何尺寸，防止仅 DOM 可见但标题已被挤掉的假通过。
for (const width of [1440, 390]) {
  test(`document titles survive long backup branches at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await mockHistory(page)
    const branch = `backup/main-before-doc-index-migration-${'20260820-abcdef123456-'.repeat(8)}`
    const title = '部署操作手册'
    await page.route('**/api/repos/7/files?**', (route) =>
      route.fulfill({
        status: 200,
        contentType: 'application/json',
        body: JSON.stringify({
          items: [
            { kind: 'dir', name: '运维文档', path: 'docs' },
            { kind: 'file', name: 'deploy.md', path: 'deploy.md', title, version_id: 42, source_branch: branch },
            { kind: 'file', name: 'README.md', path: 'README.md', version_id: 43, source_branch: 'main' }
          ]
        })
      })
    )
    await page.goto('/?repo=7')
    const row = page.locator('.file-row').filter({ hasText: title })
    const heading = row.locator('.file-row-title')
    const source = row.locator('.file-row-branch')
    await expect(heading).toBeVisible()
    await expect(heading).toHaveText(title)
    await expect(source).toHaveText(branch)
    await expect(source).toHaveAttribute('title', branch)
    const sizes = await row.evaluate((element) => {
      const heading = element.querySelector<HTMLElement>('.file-row-title')!
      const source = element.querySelector<HTMLElement>('.file-row-branch')!
      const titleBox = heading.getBoundingClientRect()
      const branchBox = source.getBoundingClientRect()
      const rowBox = element.getBoundingClientRect()
      return {
        titleWidth: heading.clientWidth,
        titleScrollWidth: heading.scrollWidth,
        titleBottom: titleBox.bottom,
        branchTop: branchBox.top,
        branchWidth: source.clientWidth,
        branchScrollWidth: source.scrollWidth,
        branchOverflow: getComputedStyle(source).textOverflow,
        contained: titleBox.left >= rowBox.left && titleBox.right <= rowBox.right &&
          branchBox.left >= rowBox.left && branchBox.right <= rowBox.right,
        rowOverflow: element.scrollWidth > element.clientWidth
      }
    })
    // 短标题完整显示，长分支在次行省略，两者都必须位于按钮内部。
    expect(sizes.titleWidth).toBeGreaterThan(100)
    expect(sizes.titleScrollWidth).toBe(sizes.titleWidth)
    expect(sizes.branchTop).toBeGreaterThanOrEqual(sizes.titleBottom)
    expect(sizes.branchWidth).toBeGreaterThan(0)
    expect(sizes.branchScrollWidth).toBeGreaterThan(sizes.branchWidth)
    expect(sizes.branchOverflow).toBe('ellipsis')
    expect(sizes.contained).toBe(true)
    expect(sizes.rowOverflow).toBe(false)
    expect(await page.locator('.file-list').evaluate((list) => list.scrollWidth === list.clientWidth)).toBe(true)
    expect(await page.locator('body').evaluate((body) => body.scrollWidth === body.clientWidth)).toBe(true)

    // 无标题的文件仍显示文件名；目录不预留分支行，并且仍可正常进入。
    await expect(page.locator('.file-row-title').filter({ hasText: 'README.md' })).toBeVisible()
    const directory = page.getByRole('button', { name: '运维文档', exact: true })
    await expect(directory.locator('.file-row-branch')).toHaveCount(0)
    const directoryBox = await directory.boundingBox()
    const fileBox = await row.boundingBox()
    expect(directoryBox!.height).toBeLessThan(fileBox!.height)
    await directory.click()
    await expect(page.getByRole('navigation', { name: '目录路径' }).getByRole('button', { name: 'docs', exact: true })).toBeVisible()
    await expect(page.getByRole('button', { name: '上一级', exact: true })).toBeEnabled()
  })
}

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
