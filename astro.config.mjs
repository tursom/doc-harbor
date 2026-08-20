import vue from '@astrojs/vue'
import { defineConfig } from 'astro/config'

export default defineConfig({
  output: 'static',
  trailingSlash: 'always',
  integrations: [vue()],
  vite: {
    server: {
      proxy: {
        '/api': 'http://127.0.0.1:8080'
      }
    }
  }
})
