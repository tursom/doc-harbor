import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vitest/config'

// 组件交互测试使用与 Astro 相同的 Vue 编译插件，不用字符串快照代替真实输入事件。
export default defineConfig({ plugins: [vue()], test: { include: ['src/**/*.test.ts'] } })
