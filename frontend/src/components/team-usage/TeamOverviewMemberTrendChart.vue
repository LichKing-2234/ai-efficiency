<script setup lang="ts">
import { useI18n } from '@/i18n'
import type { TeamMemberTrendState } from '@/types'

const props = defineProps<{
  state: TeamMemberTrendState
}>()

const { t } = useI18n()

function formatCost(value: number) {
  return value.toFixed(2)
}

function formatTokens(value: number | null | undefined) {
  if (value == null) return '-'
  return value.toLocaleString()
}

function reasonLabel(reason: string | null | undefined) {
  if (reason === 'scope_too_large') return t('teamUsage.scopeTooLarge')
  return t('teamUsage.unavailable')
}
</script>

<template>
  <section class="rounded-lg border border-slate-200 bg-white shadow-sm">
    <div class="flex items-center justify-between border-b border-slate-200 px-4 py-3">
      <h2 class="text-base font-semibold text-slate-950">{{ t('teamUsage.topMembers') }}</h2>
      <span class="text-xs font-medium text-slate-500">{{ props.state.unit_label }}</span>
    </div>

    <div v-if="props.state.unavailable" class="px-4 py-4 text-sm text-slate-500">
      {{ reasonLabel(props.state.unavailable_reason) }}
    </div>

    <div v-else-if="props.state.series.length === 0" class="px-4 py-4 text-sm text-slate-500">
      -
    </div>

    <div v-else class="overflow-x-auto">
      <table class="min-w-full divide-y divide-slate-100 text-sm">
        <thead class="bg-slate-50 text-xs font-semibold uppercase text-slate-500">
          <tr>
            <th class="whitespace-nowrap px-4 py-2 text-left">Member</th>
            <th class="whitespace-nowrap px-4 py-2 text-left">Date</th>
            <th class="whitespace-nowrap px-4 py-2 text-right">Actual cost</th>
            <th class="whitespace-nowrap px-4 py-2 text-right">Tokens</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <template v-for="series in props.state.series" :key="series.user_id">
            <tr v-if="series.unavailable" :key="`${series.user_id}-unavailable`" class="bg-slate-50">
              <td class="whitespace-nowrap px-4 py-2 font-medium text-slate-900">
                #{{ series.rank }} {{ series.display_name }}
              </td>
              <td colspan="3" class="px-4 py-2 text-slate-500">
                {{ reasonLabel(series.unavailable_reason) }}
              </td>
            </tr>
            <tr
              v-else
              v-for="point in series.points"
              :key="`${series.user_id}-${point.date}`"
              class="hover:bg-slate-50"
            >
              <td class="whitespace-nowrap px-4 py-2 font-medium text-slate-900">
                #{{ series.rank }} {{ series.display_name }}
              </td>
              <td class="whitespace-nowrap px-4 py-2 text-slate-600">{{ point.date }}</td>
              <td class="whitespace-nowrap px-4 py-2 text-right tabular-nums text-slate-900">
                {{ formatCost(point.actual_cost) }}
              </td>
              <td class="whitespace-nowrap px-4 py-2 text-right tabular-nums text-slate-600">
                {{ formatTokens(point.total_tokens) }}
              </td>
            </tr>
          </template>
        </tbody>
      </table>
    </div>
  </section>
</template>
