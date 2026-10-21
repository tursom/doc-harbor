import { expect, test as base } from '@playwright/test'
import { aiSettings, fixtureToken, mockRedesign, piDiagnostics, piRun } from './fixtures/redesign'

const test = base.extend<{ mock: Awaited<ReturnType<typeof mockRedesign>> }>({
  mock: [async ({ page }, use) => { const mock = await mockRedesign(page); await use(mock); expect(mock.unexpected).toEqual([]) }, { auto: true }]
})

test('MCP settings issues a docs-only 90 day token and preserves an invalid mixed duration', async ({ page, mock }) => {
  await page.goto('/settings/#access-tokens')
  await expect(page.getByLabel('MCP 地址', { exact: true })).toHaveValue(/\/api\/mcp$/)
  await page.getByRole('checkbox', { name: /AI 历史读取/ }).uncheck()
  await page.getByRole('checkbox', { name: /文档与代码只读访问/ }).check()
  await page.getByLabel('有效期', { exact: true }).selectOption('7776000')
  await page.getByRole('button', { name: '签发 Token', exact: true }).click()
  await expect(page.getByLabel('已签发 Token', { exact: true })).toHaveValue(fixtureToken)
  expect(mock.tokenRequests).toEqual([{ ttl_seconds: 7776000, capabilities: ['docs.read'], scope: {} }])
  await page.getByRole('checkbox', { name: /AI 问答排查/ }).check()
  await expect(page.getByText(/包含 AI 历史或诊断能力时，有效期最长 24 小时/)).toBeVisible()
  await expect(page.getByLabel('有效期', { exact: true })).toHaveValue('7776000')
  await expect(page.getByRole('button', { name: '签发 Token', exact: true })).toBeDisabled()
  await page.getByLabel('有效期', { exact: true }).selectOption('86400')
  await page.getByRole('button', { name: '签发 Token', exact: true }).click()
  await expect.poll(() => mock.tokenRequests.length).toBe(2)
  expect(mock.tokenRequests[1]).toMatchObject({ ttl_seconds: 86400, capabilities: ['docs.read', 'ai.diagnostics.read'] })
  await expect(page.getByRole('link', { name: '前往 AI 诊断', exact: true })).toBeVisible()
  await expect(page.getByLabel('pi 命令', { exact: true })).toHaveValue(/--exposure direct --bearer-token-env-var DOCHARBOR_TOKEN/)
  await page.getByText('Codex · ~/.codex/config.toml', { exact: true }).click()
  await expect(page.getByLabel('Codex 配置', { exact: true })).toHaveValue(/bearer_token_env_var = "DOCHARBOR_TOKEN"/)
  await page.getByText('Claude Code', { exact: true }).click()
  await expect(page.getByLabel('Claude Code 命令', { exact: true })).toHaveValue(/\$\{DOCHARBOR_TOKEN\}/)
  expect(await page.evaluate(() => Object.values(localStorage).some(value => value.includes('fictional-access-token')))).toBe(false)
})

test('provider UI shows the current default and process pool while preserving hidden legacy fields', async ({ page }) => {
  let patch: Record<string, unknown> | undefined
  const current = structuredClone(aiSettings)
  current.engine = 'pi'
  await page.route('**/api/ai/settings', route => route.fulfill({ json: current }))
  await page.route('**/api/ai/providers/demo-primary', route => {
    patch = route.request().postDataJSON()
    return route.fulfill({ json: { settings: current, provider: current.providers[0], message: '配置已保存' } })
  })
  await page.goto('/ai/config/')
  await expect(page.getByText('引擎：pi', { exact: true })).toBeVisible()
  await expect(page.getByText(/进程池 2 个 · 忙碌 1 · 排队 0/)).toBeVisible()
  await expect(page.getByText(/路由顺序|待应用路由|前一个失败时/)).toHaveCount(0)
  await page.getByRole('button', { name: '编辑', exact: true }).first().click()
  const drawer = page.getByRole('dialog', { name: '编辑供应商', exact: true })
  await expect(drawer).toBeVisible()
  await expect(drawer.getByLabel('RPM 上限', { exact: true })).toHaveCount(0)
  await expect(drawer.getByLabel('成本等级', { exact: true })).toHaveCount(0)
  await expect(drawer.getByLabel('路由优先级', { exact: true })).toHaveCount(0)
  await drawer.getByLabel('超时（秒）', { exact: false }).fill('75')
  await drawer.getByRole('button', { name: '保存', exact: true }).click()
  await expect.poll(() => patch?.timeout_seconds).toBe(75)
  for (const key of ['provider_key', 'priority', 'max_rpm', 'cost_tier']) expect(patch).not.toHaveProperty(key)
})

test('diagnostics follows run links, filters negative feedback, and renders full results with stderr', async ({ page }) => {
  const queries: string[] = []
  const fullResult = '读取的原文内容\n'.repeat(400) + '完整结果结束标记'
  const detail = structuredClone(piDiagnostics)
  detail.transcript!.steps[1]!.preview = fullResult
  await page.route('**/api/access/ai/runs*', route => {
    queries.push(route.request().url())
    return route.fulfill({ json: { items: [piRun] } })
  })
  await page.route('**/api/access/ai/runs/51', route => route.fulfill({ json: detail }))
  await page.goto('/ai/diagnostics/?run=51')
  await expect(page.getByText('已保留目标 Run #51。', { exact: false })).toBeVisible()
  await page.getByLabel('诊断令牌', { exact: true }).fill(fixtureToken)
  await page.getByRole('checkbox', { name: '只看 👎', exact: true }).check()
  await page.getByRole('button', { name: '读取', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Run #51', exact: true })).toBeVisible()
  expect(queries.some(value => new URL(value).searchParams.get('rating') === '-1')).toBe(true)
  await page.getByTitle('mcp__docharbor__read', { exact: true }).click()
  await expect(page.locator('.tool-result').filter({ hasText: '完整结果结束标记' })).toBeVisible()
  await expect(page.getByText('示例运行没有异常。', { exact: true })).toBeVisible()
  await expect(page.getByText('希望补充回滚条件。', { exact: true })).toBeVisible()
  await expect(page.getByRole('heading', { name: /任务框架|证据契约|检索轮次|契约覆盖/ })).toHaveCount(0)
  expect(await page.evaluate(() => Object.values(localStorage).some(value => value.includes('fictional-access-token')))).toBe(false)
})
