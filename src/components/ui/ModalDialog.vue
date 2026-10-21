<script setup lang="ts">
import { ref, useId } from 'vue'
import { X } from 'lucide-vue-next'
import { useOverlayFocus } from './useOverlayFocus'

defineOptions({ inheritAttrs: false })
const props = defineProps<{ open: boolean; title: string }>()
const emit = defineEmits<{ close: [] }>()
const panel = ref<HTMLElement | null>(null)
const titleId = useId()
const { isTop } = useOverlayFocus(() => props.open, panel, () => emit('close'))
</script>

<template>
  <Teleport to="body">
    <div v-if="open" class="overlay-backdrop modal-backdrop" @click.self="isTop() && emit('close')">
      <section ref="panel" v-bind="$attrs" class="modal-dialog modal" role="dialog" aria-modal="true" :aria-labelledby="titleId" tabindex="-1">
        <header class="overlay-head">
          <h3 :id="titleId">{{ title }}</h3>
          <button type="button" class="btn ghost icon" aria-label="关闭" @click="emit('close')"><X aria-hidden="true" /></button>
        </header>
        <div class="overlay-body"><slot /></div>
        <footer v-if="$slots.footer" class="overlay-foot"><slot name="footer" /></footer>
      </section>
    </div>
  </Teleport>
</template>
