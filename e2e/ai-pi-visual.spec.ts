import { expect, test } from '@playwright/test'
import { aiSettings, fixtureToken, mockRedesign, piTurn, sessions } from './fixtures/redesign'

declare const process: { env: Record<string, string | undefined> }
const pages = [
  { key: 'pi-chat', path: '/ai/?repo=7&session=71' },
  { key: 'pi-providers', path: '/ai/config/' },
  { key: 'pi-diagnostics', path: '/ai/diagnostics/?run=51' },
  { key: 'mcp-settings', path: '/settings/#mcp' }
]

test.describe('pi frontend screenshots', () => {
  test.skip(process.env.DOCHARBOR_SCREENSHOTS !== '1', '设置 DOCHARBOR_SCREENSHOTS=1 渲染 pi 前端验收图')
  for (const width of [1440, 390]) for (const theme of ['light', 'dark'] as const) for (const item of pages) {
    test(`${item.key}-${width}-${theme}`, async ({ page }) => {
      const mock = await mockRedesign(page)
      await page.route('**/api/ai/settings', route => route.fulfill({ json: { ...aiSettings, engine: 'pi' } }))
      await page.route(/\/api\/ai\/sessions(?:\?.*)?$/, route => route.fulfill({ json: { items: [{ ...sessions[1], id: 71, engine: 'pi' }, { ...sessions[0], engine: 'legacy' }] } }))
      await page.route('**/api/ai/sessions/71', route => route.fulfill({ json: { ...sessions[1], id: 71, engine: 'pi' } }))
      await page.route('**/api/ai/sessions/71/transcript', route => route.fulfill({ json: { turns: [piTurn] } }))
      await page.setViewportSize({ width, height: width === 390 ? 844 : 1000 })
      await page.emulateMedia({ colorScheme: theme })
      await page.goto(item.path)
      if (item.key === 'pi-chat') {
        await expect(page.locator('.pi-answer .pi-citation.warn')).toBeVisible()
        await page.locator('.pi-process > summary').click()
        await expect(page.getByText('读取第 5-18 行，共 30 行', { exact: true })).toBeVisible()
      } else if (item.key === 'pi-providers') {
        await expect(page.getByText('引擎：pi', { exact: true })).toBeVisible()
      } else if (item.key === 'pi-diagnostics') {
        await page.getByLabel('诊断令牌', { exact: true }).fill(fixtureToken)
        await page.getByRole('button', { name: '读取', exact: true }).click()
        await expect(page.getByRole('heading', { name: 'Run #51', exact: true })).toBeVisible()
        await page.getByTitle('mcp__docharbor__read', { exact: true }).click()
      } else {
        await expect(page.getByLabel('MCP 地址', { exact: true })).toHaveValue(/\/api\/mcp$/)
        await page.getByText('Codex · ~/.codex/config.toml', { exact: true }).click()
      }
      await page.evaluate(() => document.fonts.ready)
      const size = await page.evaluate(() => ({ root: document.documentElement.scrollWidth, body: document.body.scrollWidth, viewport: window.innerWidth }))
      expect(size.root, JSON.stringify(size)).toBeLessThanOrEqual(size.viewport)
      expect(size.body, JSON.stringify(size)).toBeLessThanOrEqual(size.viewport)
      await page.screenshot({ path: `out/ai-ui/${item.key}-${width}-${theme}.png`, fullPage: true, animations: 'disabled' })
      expect(mock.unexpected).toEqual([])
    })
  }
})
