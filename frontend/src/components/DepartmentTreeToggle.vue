<script setup lang="ts">
defineProps<{
  expanded: boolean
  expandedLabel: string
  collapsedLabel: string
}>()

const emit = defineEmits<{
  toggle: []
}>()
</script>

<template>
  <button
    type="button"
    class="inline-flex h-7 w-7 shrink-0 items-center justify-center rounded-md border border-gray-300 bg-white text-base font-semibold leading-none text-gray-700 shadow-sm transition-colors hover:border-gray-400 hover:bg-gray-50 focus-visible:outline focus-visible:outline-2 focus-visible:outline-offset-2 focus-visible:outline-gray-900"
    :aria-label="expanded ? expandedLabel : collapsedLabel"
    @click.stop="emit('toggle')"
    @keydown.enter.prevent.stop="emit('toggle')"
    @keydown.space.prevent.stop="emit('toggle')"
  >
    {{ expanded ? '-' : '+' }}
  </button>
</template>
