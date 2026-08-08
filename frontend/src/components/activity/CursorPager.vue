<script setup lang="ts">
defineProps<{
  hasPrevious: boolean
  hasNext: boolean
  loading?: boolean
  previousLabel: string
  nextLabel: string
  testIDPrefix: string
}>()

const emit = defineEmits<{ previous: []; next: [] }>()
</script>

<template>
  <div v-if="hasPrevious || hasNext" class="flex justify-between gap-3 border-t border-slate-200 px-5 py-3">
    <button
      v-if="hasPrevious"
      type="button"
      :data-testid="`${testIDPrefix}-previous`"
      class="min-h-10 border border-slate-300 bg-white px-4 text-sm font-medium text-slate-700 hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-600 focus-visible:ring-offset-2 disabled:opacity-50"
      :disabled="loading"
      @click="emit('previous')"
    >
      {{ previousLabel }}
    </button>
    <span v-else />
    <button
      v-if="hasNext"
      type="button"
      :data-testid="`${testIDPrefix}-next`"
      class="min-h-10 border border-slate-300 bg-white px-4 text-sm font-medium text-slate-700 hover:bg-slate-50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-600 focus-visible:ring-offset-2 disabled:opacity-50"
      :disabled="loading"
      @click="emit('next')"
    >
      {{ nextLabel }}
    </button>
  </div>
</template>
