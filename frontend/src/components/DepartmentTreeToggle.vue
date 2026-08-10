<script setup lang="ts">
import { Minus, Plus } from '@element-plus/icons-vue'

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
  <ElButton
    circle
    class="!ml-0 h-7 w-7 shrink-0"
    :icon="expanded ? Minus : Plus"
    :aria-label="expanded ? expandedLabel : collapsedLabel"
    @click.stop="emit('toggle')"
    @keydown.enter.prevent.stop="emit('toggle')"
    @keydown.space.prevent.stop="emit('toggle')"
  />
</template>
