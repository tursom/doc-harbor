import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e',
  timeout: 20_000,
  use: {
    baseURL: 'http://127.0.0.1:14321',
    trace: 'retain-on-failure'
  },
  webServer: {
    command:
      'DATA_DIR=/tmp/doc-harbor-playwright-data HTTP_ADDR=127.0.0.1:14321 WEB_DIR=./dist go run ./cmd/doc-harbor',
    url: 'http://127.0.0.1:14321/',
    reuseExistingServer: false,
    timeout: 30_000
  }
})
