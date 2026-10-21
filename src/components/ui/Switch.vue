<script setup lang="ts">
defineProps<{ modelValue: boolean }>()
const emit = defineEmits<{ 'update:modelValue': [value: boolean] }>()
</script>

<template>
  <button type="button" class="switch" role="switch" :aria-checked="modelValue" @click="emit('update:modelValue', !modelValue)">
    <span v-if="$slots.default" class="sr-only"><slot /></span>
  </button>
</template>
