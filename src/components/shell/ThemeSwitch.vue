<script setup lang="ts">
import { Monitor, Moon, Sun } from 'lucide-vue-next'
import { useTheme, type Theme } from '../../composables/useTheme'

const { theme, setTheme } = useTheme()
const options = [
  { value: 'system', label: '跟随系统', icon: Monitor },
  { value: 'light', label: '浅色', icon: Sun },
  { value: 'dark', label: '深色', icon: Moon }
] satisfies { value: Theme; label: string; icon: typeof Monitor }[]
</script>

<template>
  <div class="theme-switch" role="group" aria-label="主题">
    <button v-for="option in options" :key="option.value" type="button" :aria-pressed="theme === option.value" :title="option.label" @click="setTheme(option.value)">
      <component :is="option.icon" aria-hidden="true" />{{ option.label }}
    </button>
  </div>
</template>
