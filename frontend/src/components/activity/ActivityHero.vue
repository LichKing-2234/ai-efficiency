<script setup lang="ts">
import { useI18n } from '@/i18n'
import type { ActivityMetrics } from '@/types/activity'
defineProps<{ metrics: ActivityMetrics }>()
const { t } = useI18n()
function count(metric: { value: number; lower_bound: boolean }) { return `${metric.lower_bound ? '≥' : ''}${metric.value}` }
function date(value?: string) { return value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '—' }
</script>

<template>
  <section data-testid="activity-hero" class="grid grid-cols-2 gap-3 xl:grid-cols-4">
    <article class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm"><p class="text-xs font-semibold uppercase tracking-wide text-slate-500">{{ t('activity.participatingPRs') }}</p><p class="mt-2 text-3xl font-semibold text-slate-950">{{ count(metrics.participating_prs) }}</p></article>
    <article class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm"><p class="text-xs font-semibold uppercase tracking-wide text-slate-500">{{ t('activity.mergedPRs') }}</p><p class="mt-2 text-3xl font-semibold text-emerald-700">{{ count(metrics.merged_prs) }}</p></article>
    <article class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm"><p class="text-xs font-semibold uppercase tracking-wide text-slate-500">{{ t('activity.activeRepositories') }}</p><p class="mt-2 text-3xl font-semibold text-slate-950">{{ metrics.active_repositories }}</p></article>
    <article class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm"><p class="text-xs font-semibold uppercase tracking-wide text-slate-500">{{ t('activity.latestActivity') }}</p><p class="mt-2 text-base font-semibold text-slate-950">{{ date(metrics.latest_activity) }}</p></article>
  </section>
</template>
