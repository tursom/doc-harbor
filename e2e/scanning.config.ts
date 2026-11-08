import { defineConfig } from '@playwright/test'

/** 独立前端 fixture 验收不启动 Go，也不读取运行凭据；所有业务 API 均由测试拦截。 */
export default defineConfig({
  testDir: '.',
  testMatch: ['scanning.spec.ts', 'redesign.spec.ts'],
  timeout: 25_000,
  workers: 2,
  use: { baseURL: 'http://127.0.0.1:14326', trace: 'retain-on-failure' },
  webServer: {
    cwd: new URL('..', import.meta.url).pathname,
    command: 'npx astro dev --host 127.0.0.1 --port 14326',
    url: 'http://127.0.0.1:14326/',
    reuseExistingServer: false
  }
})
