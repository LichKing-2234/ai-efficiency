<script setup lang="ts">
import type { TeamUsageAuditRecord } from '@/types'

const props = defineProps<{
  items: TeamUsageAuditRecord[]
}>()

function formatMultiplier(value: number | null | undefined) {
  if (value == null) return '-'
  return `${value}x`
}

function formatChanged(changed: boolean) {
  return changed ? 'changed' : 'unchanged'
}
</script>

<template>
  <section v-if="props.items.length > 0" class="rounded-lg border border-slate-200 bg-white shadow-sm">
    <div class="border-b border-slate-200 px-4 py-3">
      <h2 class="text-base font-semibold text-slate-950">Audit</h2>
    </div>
    <div class="divide-y divide-slate-100">
      <article v-for="item in props.items" :key="item.id" class="grid gap-2 px-4 py-3 text-sm md:grid-cols-[1.1fr_1fr_1fr_1.2fr]">
        <div>
          <div class="font-medium text-slate-950">{{ item.group_name }}</div>
          <div class="text-xs text-slate-500">{{ item.created_at }}</div>
        </div>
        <div class="text-slate-700">
          <div>{{ item.action }}</div>
          <div class="text-xs text-slate-500">{{ item.status }} · {{ formatChanged(item.changed) }}</div>
        </div>
        <div class="text-slate-700">
          {{ formatMultiplier(item.old_multiplier) }} → {{ formatMultiplier(item.new_multiplier) }}
        </div>
        <div class="text-slate-700">
          <div v-if="item.rejection_reason">{{ item.rejection_reason }}</div>
          <div v-if="item.reason" class="text-slate-500">{{ item.reason }}</div>
        </div>
      </article>
    </div>
  </section>
</template>
