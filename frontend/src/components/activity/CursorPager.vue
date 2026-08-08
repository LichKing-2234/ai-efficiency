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
    <ElButton
      v-if="hasPrevious"
      :data-testid="`${testIDPrefix}-previous`"
      class="min-h-10 !ml-0 px-4"
      :disabled="loading"
      @click="emit('previous')"
    >
      {{ previousLabel }}
    </ElButton>
    <span v-else />
    <ElButton
      v-if="hasNext"
      :data-testid="`${testIDPrefix}-next`"
      class="min-h-10 !ml-0 px-4"
      :disabled="loading"
      @click="emit('next')"
    >
      {{ nextLabel }}
    </ElButton>
  </div>
</template>
