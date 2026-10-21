<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, useId, watch } from 'vue'
import { MoreHorizontal } from 'lucide-vue-next'
import { focusableElements, useOverlayFocus } from './useOverlayFocus'

withDefaults(defineProps<{ label?: string }>(), { label: '更多操作' })
const open = ref(false)
const root = ref<HTMLElement | null>(null)
const panel = ref<HTMLElement | null>(null)
const position = ref({ left: '0px', top: '0px' })
let resizeObserver: ResizeObserver | undefined
const menuId = useId()
function close() { open.value = false }
const { isTop } = useOverlayFocus(() => open.value, panel, close, false)

function outside(event: PointerEvent) {
  const target = event.target as Node
  if (open.value && isTop() && !root.value?.contains(target) && !panel.value?.contains(target)) close()
}

function reposition() {
  if (!open.value || !root.value || !panel.value) return
  // flex 容器可能拉伸外层 wrapper，菜单必须对齐真实触发按钮。
  const anchor = (root.value.querySelector('.dropdown-trigger') ?? root.value).getBoundingClientRect()
  const menu = panel.value.getBoundingClientRect()
  const viewport = document.documentElement
  const left = Math.max(8, Math.min(anchor.right - menu.width, viewport.clientWidth - menu.width - 8))
  const below = anchor.bottom + 4
  const preferred = below + menu.height > viewport.clientHeight - 8 && anchor.top > menu.height + 4
    ? anchor.top - menu.height - 4 : below
  const top = Math.max(8, Math.min(preferred, viewport.clientHeight - menu.height - 8))
  position.value = { left: `${left}px`, top: `${top}px` }
}

watch(open, async value => {
  resizeObserver?.disconnect()
  if (!value) return
  await nextTick()
  if (!open.value || !panel.value) return
  reposition()
  // InlineConfirm 展开附加选择时菜单高度会变化，仍需保持在可视区域内。
  if (typeof ResizeObserver !== 'undefined') {
    resizeObserver = new ResizeObserver(reposition)
    resizeObserver.observe(panel.value)
  }
})

function select(event: MouseEvent) {
  const target = event.target as HTMLElement
  // 二次确认和表单需要在菜单中继续交互；普通动作点击后即可关闭。
  // 子组件可能在事件冒泡前替换按钮，使用原始事件路径判断，避免丢失已移除节点的父级。
  if (event.composedPath().some(node => node instanceof HTMLElement &&
    node.matches('.inline-confirm, [data-keep-open], input, select, textarea, label'))) return
  if (target.closest('button:not(:disabled), a[href]')) close()
}

function navigate(event: KeyboardEvent) {
  if (!panel.value || !['ArrowDown', 'ArrowUp', 'Home', 'End'].includes(event.key)) return
  if ((event.target as HTMLElement).matches('input, select, textarea')) return
  const items = focusableElements(panel.value)
  if (!items.length) return
  event.preventDefault()
  const index = items.indexOf(document.activeElement as HTMLElement)
  const next = event.key === 'Home' ? 0 : event.key === 'End' ? items.length - 1 :
    (index + (event.key === 'ArrowDown' ? 1 : -1) + items.length) % items.length
  items[next]?.focus()
}

onMounted(() => {
  document.addEventListener('pointerdown', outside, true)
  window.addEventListener('resize', reposition)
  window.addEventListener('scroll', reposition, true)
})
onBeforeUnmount(() => {
  resizeObserver?.disconnect()
  document.removeEventListener('pointerdown', outside, true)
  window.removeEventListener('resize', reposition)
  window.removeEventListener('scroll', reposition, true)
})
</script>

<template>
  <div ref="root" class="dropdown">
    <button type="button" class="btn ghost icon dropdown-trigger" :aria-label="label" aria-haspopup="true" :aria-expanded="open" :aria-controls="open ? menuId : undefined" @click="open = !open" @keydown.down.prevent="open = true">
      <slot name="trigger"><MoreHorizontal aria-hidden="true" /></slot>
    </button>
    <!-- 菜单移到 body，避免抽屉、表格等滚动容器裁切；定位限制在视口内。 -->
    <Teleport to="body">
      <div v-if="open" :id="menuId" ref="panel" class="dropdown-menu menu" :style="position" role="group" :aria-label="label" tabindex="-1" @click="select" @keydown="navigate">
        <slot :close="close" />
      </div>
    </Teleport>
  </div>
</template>
