<script setup lang="ts">
import { ref, useId } from 'vue'
import { X } from 'lucide-vue-next'
import { useOverlayFocus } from './useOverlayFocus'

// class/style 等属性落在面板上，移动侧栏可用 class="left" 或自己的 class 改位置。
defineOptions({ inheritAttrs: false })
const props = defineProps<{ open: boolean; title: string }>()
const emit = defineEmits<{ close: [] }>()
const panel = ref<HTMLElement | null>(null)
const titleId = useId()
const { isTop } = useOverlayFocus(() => props.open, panel, () => emit('close'))
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="overlay-backdrop drawer-backdrop" @click.self="isTop() && emit('close')">
      <section ref="panel" v-bind="$attrs" class="drawer-panel drawer" role="dialog" aria-modal="true" :aria-labelledby="titleId" tabindex="-1">
        <header class="overlay-head drawer-head">
          <h3 :id="titleId">{{ title }}</h3>
          <button type="button" class="btn ghost icon" aria-label="关闭" @click="emit('close')"><X aria-hidden="true" /></button>
        </header>
        <div class="overlay-body drawer-body"><slot /></div>
        <footer v-if="$slots.footer" class="overlay-foot drawer-foot"><slot name="footer" /></footer>
      </section>
    </div>
  </Teleport>
</template>
