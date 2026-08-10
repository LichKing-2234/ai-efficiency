<script setup lang="ts">
defineProps<{
  eyebrow?: string
  title: string
  description?: string
}>()
</script>

<template>
  <header class="flex min-w-0 flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
    <div class="min-w-0">
      <slot name="before" />
      <p v-if="eyebrow" class="text-xs font-semibold uppercase tracking-wide text-teal-700">
        {{ eyebrow }}
      </p>
      <h1 class="mt-1 break-words text-2xl font-bold leading-tight text-slate-950" :title="title">
        {{ title }}
      </h1>
      <p v-if="description" class="mt-1 break-words text-sm leading-5 text-slate-600">
        {{ description }}
      </p>
      <slot name="after" />
    </div>
    <div v-if="$slots.actions" class="w-full shrink-0 sm:w-auto">
      <slot name="actions" />
    </div>
  </header>
</template>
