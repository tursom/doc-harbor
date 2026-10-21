import { expect, test as base } from '@playwright/test'
import type { Page } from '@playwright/test'
import { documentContent, fixtureSecret, fixtureSHA, fixtureToken, mockRedesign, repository } from './fixtures/redesign'

// 项目没有安装 @types/node；这里只声明截图开关，避免为验收脚本新增依赖。
declare const process: { env: Record<string, string | undefined> }

// 每个用例都安装完整拦截；未知 API 是契约失败，不能悄悄访问本地后端。
const test = base.extend<{ mock: Awaited<ReturnType<typeof mockRedesign>> }>({
  mock: [async ({ page }, use) => {
    const mock = await mockRedesign(page)
    await use(mock)
    expect(mock.unexpected, '页面调用了 fixture 未定义的 API').toEqual([])
  }, { auto: true }]
})

const pages = [
  { key: 'docs', name: '文档', path: '/?repo=7&dir=docs&version=42', title: 'DocHarbor' },
  { key: 'history', name: '提交历史', path: `/history/?repo=7&commit=${fixtureSHA}`, title: '提交历史 · DocHarbor' },
  { key: 'scans', name: '扫描记录', path: '/scans/?repo=7', title: '扫描记录 · DocHarbor' },
  { key: 'repo-settings', name: '仓库设置', path: '/repo-settings/?repo=7', title: '仓库设置 · DocHarbor' },
  { key: 'ai', name: 'AI 问答', path: '/ai/?repo=7&session=31', title: 'AI 问答 · DocHarbor' },
  { key: 'ai-providers', name: 'AI 供应商', path: '/ai/config/', title: 'AI 供应商 · DocHarbor' },
  { key: 'ai-diagnostics', name: 'AI 诊断', path: '/ai/diagnostics/', title: 'AI 诊断 · DocHarbor' },
  { key: 'settings', name: '系统设置', path: '/settings/', title: '系统设置 · DocHarbor' }
] as const

/** 截图前先验证有效数据已呈现，防止错误页或空白页也生成「验收图」。 */
async function openPopulatedPage(page: Page, item: typeof pages[number]) {
  await page.goto(item.path)
  await expect(page).toHaveTitle(item.title)
  switch (item.key) {
    case 'docs':
      await expect(page.getByRole('heading', { name: documentContent.title, exact: true }).first()).toBeVisible()
      await expect(page.getByRole('heading', { name: '发布前检查', exact: true })).toBeVisible()
      await expect(page.locator('.mermaid-render svg')).toBeVisible()
      break
    case 'history':
      await expect(page.getByRole('heading', { name: '完善灯塔发布检查', exact: true })).toBeVisible()
      await expect(page.getByText('docs/health.md', { exact: true })).toBeVisible()
      break
    case 'scans':
      await page.getByRole('button', { name: /展开扫描 #81 错误详情$/ }).click()
      await expect(page.getByText('演示扫描错误：docs/archive/large.md 超过索引限制', { exact: true })).toBeVisible()
      break
    case 'repo-settings':
      await expect(page.getByLabel('名称', { exact: true })).toHaveValue(repository.name)
      break
    case 'ai':
      await expect(page.getByText('灯塔服务发布前要检查什么？', { exact: true })).toBeVisible()
      await expect(page.getByText(/发布前需要确认健康检查通过/)).toBeVisible()
      break
    case 'ai-providers':
      await expect(page.getByText('https://api.example.test/v1', { exact: true })).toBeVisible()
      break
    case 'ai-diagnostics':
      await page.getByLabel('诊断令牌', { exact: true }).fill(fixtureToken)
      await page.getByRole('button', { name: '读取', exact: true }).click()
      await expect(page.getByRole('heading', { name: 'Run #51', exact: true })).toBeVisible()
      await expect(page.getByRole('heading', { name: '引用核实 · 2', exact: true })).toBeVisible()
      break
    case 'settings':
      await expect(page.getByRole('button', { name: '轮换 Secret', exact: true })).toBeVisible()
      await page.getByRole('button', { name: '签发 Token', exact: true }).click()
      await expect(page.getByText(fixtureToken, { exact: true })).toBeVisible()
      await expect(page.getByRole('button', { name: '轮换 Secret', exact: true })).toBeVisible()
      break
  }
}

async function expectNoPageOverflow(page: Page) {
  // 分别测量 body 和根元素；只检查 body 可能漏掉被根元素撑宽的页面。
  const size = await page.evaluate(() => ({
    viewport: window.innerWidth, root: document.documentElement.scrollWidth, body: document.body.scrollWidth
  }))
  expect(size.root, JSON.stringify(size)).toBeLessThanOrEqual(size.viewport)
  expect(size.body, JSON.stringify(size)).toBeLessThanOrEqual(size.viewport)
}

test('repo settings saves dirty form with PATCH and supports discard', async ({ page, mock }) => {
  await page.goto('/repo-settings/?repo=7')
  const name = page.getByLabel('名称', { exact: true })
  await expect(name).toHaveValue(repository.name)
  await expect(page.getByText('有未保存的修改', { exact: true })).toHaveCount(0)
  // Payload URL 可以展示，Secret 只能在系统设置管理。
  const payload = page.getByLabel('Payload URL', { exact: true })
  await expect(payload).toHaveValue(/\/api\/webhooks\/github\/7$/)
  await expect(page.getByText(fixtureSecret, { exact: true })).toHaveCount(0)
  await expect(page.getByRole('button', { name: /显示 Secret|轮换 Secret/ })).toHaveCount(0)

  await name.fill('暂存修改')
  await expect(page.getByText('有未保存的修改', { exact: true })).toBeVisible()
  expect(await page.evaluate(() => !window.dispatchEvent(new Event('beforeunload', { cancelable: true })))).toBe(true)
  await page.getByRole('button', { name: '放弃', exact: true }).click()
  await expect(name).toHaveValue(repository.name)
  expect(mock.patches).toHaveLength(0)
  await expect(page.getByText('有未保存的修改', { exact: true })).toHaveCount(0)
  expect(await page.evaluate(() => !window.dispatchEvent(new Event('beforeunload', { cancelable: true })))).toBe(false)

  const readsBeforeSave = mock.repoReads
  await name.fill('灯塔发布文档（已更新）')
  await page.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => mock.patches.length).toBe(1)
  expect(mock.patches[0]).toMatchObject({ name: '灯塔发布文档（已更新）', repo_url: repository.repo_url })
  await expect(page.getByText('有未保存的修改', { exact: true })).toHaveCount(0)
  await expect.poll(() => mock.repoReads).toBeGreaterThan(readsBeforeSave)
  await page.reload()
  await expect(name).toHaveValue('灯塔发布文档（已更新）')
})

test('new repository modal closes with Escape and returns focus to its trigger', async ({ page }) => {
  await page.goto('/history/?repo=7')
  await expect(page.getByRole('heading', { name: repository.name, exact: true })).toBeVisible()
  const trigger = page.getByRole('button', { name: '新增仓库', exact: true })
  await trigger.click()
  const dialog = page.getByRole('dialog', { name: '新增仓库', exact: true })
  await expect(dialog).toBeVisible()
  await expect(dialog.locator('input').first()).toBeFocused()
  await expect(dialog.getByLabel('默认分支', { exact: true })).toHaveValue('main')
  await expect(dialog.getByRole('button', { name: '添加并开始扫描', exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await expect(dialog).toHaveCount(0)
  await expect(trigger).toBeFocused()
})

test('new repository creates once, scans it and navigates to its documents', async ({ page, mock }) => {
  await page.goto('/settings/')
  await page.getByRole('button', { name: '新增仓库', exact: true }).click()
  const dialog = page.getByRole('dialog', { name: '新增仓库', exact: true })
  await dialog.getByLabel('Git URL', { exact: true }).fill('https://example.test/new-docs.git')
  await dialog.getByLabel('名称', { exact: true }).fill('新建示例文档')
  await dialog.getByRole('button', { name: '添加并开始扫描', exact: true }).click()
  await expect(page).toHaveURL(/\/\?repo=9$/)
  await expect(page.getByRole('heading', { name: '新建示例文档', exact: true })).toBeVisible()
  expect(mock.createdRepos).toHaveLength(1)
  expect(mock.scannedRepos).toEqual([9])
  expect(mock.createdRepos[0]?.scan_paths.map((item) => item.path)).toEqual(['doc', 'docs'])
})

test('settings masks Secret and requires inline confirmation before rotation', async ({ page, mock }) => {
  const nativeDialogs: string[] = []
  page.on('dialog', async (dialog) => { nativeDialogs.push(dialog.type()); await dialog.dismiss() })
  await page.goto('/settings/')
  const maskedSecret = page.getByLabel('Webhook Secret', { exact: true })
  await expect(maskedSecret).toBeVisible()
  await expect(maskedSecret).toHaveAttribute('type', 'password')
  await expect(page.getByText(fixtureSecret, { exact: true })).toHaveCount(0)
  await page.getByRole('button', { name: /^(显示|显示 Secret)$/ }).click()
  await expect(maskedSecret).toHaveAttribute('type', 'text')
  await expect(maskedSecret).toHaveValue(fixtureSecret)
  await page.getByRole('button', { name: /^(隐藏|隐藏 Secret)$/ }).click()
  await expect(maskedSecret).toHaveValue(fixtureSecret)
  await expect(maskedSecret).toHaveAttribute('type', 'password')

  await page.getByRole('button', { name: '轮换 Secret', exact: true }).click()
  expect(mock.secretRotations).toBe(0)
  await page.getByRole('button', { name: '取消', exact: true }).click()
  expect(mock.secretRotations).toBe(0)
  await page.getByRole('button', { name: '轮换 Secret', exact: true }).click()
  await page.getByRole('group', { name: '确认轮换', exact: true }).getByRole('button', { name: '轮换 Secret', exact: true }).click()
  await expect.poll(() => mock.secretRotations).toBe(1)
  expect(nativeDialogs).toEqual([])
})

test('AI composer sends Enter but preserves Shift+Enter and IME composition', async ({ page, mock }) => {
  await page.goto('/ai/?repo=7&session=31')
  await expect(page.getByText('灯塔服务发布前要检查什么？', { exact: true })).toBeVisible()
  const composer = page.locator('textarea')
  await composer.fill('第一行')
  await composer.press('Shift+Enter')
  await expect(composer).toHaveValue('第一行\n')
  expect(mock.questions).toHaveLength(0)
  await composer.fill('输入法候选文字')
  // 分别覆盖标准 isComposing 和兼容旧输入法的 keyCode=229，不能仅测试一个条件。
  for (const init of [{ key: 'Enter', code: 'Enter', isComposing: true }, { key: 'Enter', code: 'Enter', keyCode: 229 }]) {
    await composer.dispatchEvent('keydown', init)
    await expect(composer).toHaveValue('输入法候选文字')
    expect(mock.questions).toHaveLength(0)
  }
  await composer.fill('请说明灯塔发布检查步骤')
  await composer.press('Enter')
  await expect.poll(() => mock.questions.length).toBe(1)
  expect(mock.questions[0]?.question).toBe('请说明灯塔发布检查步骤')
  await expect(page.getByText('模拟流式回答完成。', { exact: true })).toBeVisible()
  await expect(composer).toHaveValue('')
})

test('system dark scheme uses the dark body background token', async ({ page }) => {
  await page.emulateMedia({ colorScheme: 'dark' })
  await page.goto('/?repo=7')
  await expect(page.locator('.file-row-title').filter({ hasText: documentContent.title })).toBeVisible()
  const colors = await page.evaluate(() => ({
    body: getComputedStyle(document.body).backgroundColor,
    token: getComputedStyle(document.documentElement).getPropertyValue('--bg').trim()
  }))
  expect(colors.body).toBe('rgb(15, 18, 17)')
  expect(colors.token.toLowerCase()).toBe('#0f1211')
})

test('theme persists and rerenders Mermaid while document contents remain navigable', async ({ page }) => {
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.emulateMedia({ colorScheme: 'light' })
  await page.goto('/?repo=7&version=42')
  const diagram = page.locator('.mermaid-render svg')
  await expect(diagram).toBeVisible()
  const originalID = await diagram.getAttribute('id')
  const toc = page.getByRole('navigation', { name: '本页目录', exact: true })
  await expect(toc.getByRole('link')).toHaveCount(4)
  await toc.getByRole('link', { name: '执行发布', exact: true }).click()
  await expect(page.getByRole('heading', { name: '执行发布', exact: true })).toBeInViewport()
  await page.getByRole('button', { name: '深色', exact: true }).click()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(diagram).toBeVisible()
  await expect(diagram).not.toHaveAttribute('id', originalID!)
  expect(await page.evaluate(() => localStorage.getItem('docharbor-theme'))).toBe('dark')
  await page.reload()
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'dark')
  await expect(diagram).toBeVisible()
  await page.goto('/html-preview/')
  await expect(page.locator('html')).toHaveAttribute('data-theme', 'light')
})

test('document and AI session deep links survive reload', async ({ page, mock }) => {
  await page.goto('/?repo=7&view=branch&branch=feature%2Frelease-guide&dir=docs&version=42')
  await expect(page.getByRole('heading', { name: '发布前检查', exact: true })).toBeVisible()
  await expect(page).toHaveURL(/version=42/)
  await page.reload()
  await expect(page.getByRole('heading', { name: '发布前检查', exact: true })).toBeVisible()
  const params = new URL(page.url()).searchParams
  expect(Object.fromEntries(params)).toMatchObject({ repo: '7', view: 'branch', branch: 'feature/release-guide', dir: 'docs', version: '42' })
  await page.goto('/ai/?repo=7&session=31')
  await expect(page.getByText('灯塔服务发布前要检查什么？', { exact: true })).toBeVisible()
  expect(mock.messageReads.at(-1)).toBe(31)
  await page.reload()
  await expect(page.getByText(/发布前需要确认健康检查通过/)).toBeVisible()
  await expect(page).toHaveURL(/session=31/)
  expect(mock.messageReads.at(-1)).toBe(31)
})

test('legacy repo settings URL redirects and keeps repository context', async ({ page }) => {
  await page.goto('/?tab=repo-settings&repo=7&version=42')
  await expect(page).toHaveURL(/\/repo-settings\/\?repo=7$/)
  await expect(page.getByLabel('名称', { exact: true })).toHaveValue(repository.name)
})

for (const item of pages) {
  test(`${item.name} has no horizontal page overflow at 390px with populated data`, async ({ page }) => {
    await page.setViewportSize({ width: 390, height: 844 })
    await openPopulatedPage(page, item)
    await expectNoPageOverflow(page)
    const menu = page.getByRole('button', { name: '菜单', exact: true })
    await menu.click()
    await expect(page.getByRole('navigation', { name: '全局功能' })).toBeVisible()
    await expectNoPageOverflow(page)
    await page.keyboard.press('Escape')
    await expect(page.getByRole('navigation', { name: '全局功能' })).not.toBeVisible()
    await expect(menu).toBeFocused()
  })
}

// 手动视觉验收矩阵：默认跳过，避免日常回归重复渲染 32 页；生成的图片只写入已忽略的 test-results。
test.describe('redesign screenshots', () => {
  test.skip(process.env.DOCHARBOR_SCREENSHOTS !== '1', '设置 DOCHARBOR_SCREENSHOTS=1 生成八页亮暗截图')
  for (const width of [1440, 390]) {
    for (const theme of ['light', 'dark'] as const) {
      for (const item of pages) {
        test(`${item.key}-${width}-${theme}`, async ({ page }, testInfo) => {
          await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 })
          await page.emulateMedia({ colorScheme: theme })
          await openPopulatedPage(page, item)
          await expectNoPageOverflow(page)
          await page.evaluate(() => document.fonts.ready)
          await page.screenshot({ path: testInfo.outputPath(`${item.key}-${width}-${theme}.png`), fullPage: true, animations: 'disabled' })
        })
      }
    }
  }
})
