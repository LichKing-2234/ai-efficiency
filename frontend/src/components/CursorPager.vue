<script setup lang="ts">
defineProps<{
  hasPrevious: boolean
  hasNext: boolean
  loading?: boolean
  loadingLabel: string
  previousLabel: string
  nextLabel: string
  rangeLabel?: string
  testIDPrefix: string
}>()

const emit = defineEmits<{ previous: []; next: [] }>()
</script>

<template>
  <div
    v-if="hasPrevious || hasNext"
    class="flex justify-between gap-3 border-t border-slate-200 px-5 py-3"
    :aria-busy="loading || undefined"
  >
    <ElButton
      :data-testid="`${testIDPrefix}-previous`"
      class="min-h-10 !ml-0 px-4"
      :disabled="loading || !hasPrevious"
      @click="emit('previous')"
    >
      {{ previousLabel }}
    </ElButton>
    <span
      v-if="rangeLabel || loading"
      :data-testid="`${testIDPrefix}-range`"
      class="self-center text-center text-sm text-slate-500"
      :role="loading ? 'status' : undefined"
    >
      {{ loading ? loadingLabel : rangeLabel }}
    </span>
    <ElButton
      :data-testid="`${testIDPrefix}-next`"
      class="min-h-10 !ml-0 px-4"
      :disabled="loading || !hasNext"
      @click="emit('next')"
    >
      {{ nextLabel }}
    </ElButton>
  </div>
</template>
