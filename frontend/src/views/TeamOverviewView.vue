<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import TeamOverviewMemberTrendChart from '@/components/team-usage/TeamOverviewMemberTrendChart.vue'
import TeamOverviewMemberTable from '@/components/team-usage/TeamOverviewMemberTable.vue'
import { getTeamUsageOverview } from '@/api/teamUsage'
import { useI18n } from '@/i18n'
import type { TeamOverviewResponse, TeamUsageOverviewParams } from '@/types'

const { t } = useI18n()
const router = useRouter()
const overview = ref<TeamOverviewResponse | null>(null)
const loading = ref(false)
const loadError = ref<'no_scope' | 'unavailable' | null>(null)

const scopeTooLarge = computed(() => {
  return overview.value?.summary.unavailable_reason === 'scope_too_large'
    || overview.value?.top_member_trend.unavailable_reason === 'scope_too_large'
})

async function loadOverview() {
  loading.value = true
  loadError.value = null
  try {
    overview.value = (await getTeamUsageOverview(buildDefaultOverviewParams())).data.data ?? null
  } catch (error) {
    overview.value = null
    loadError.value = isForbidden(error) ? 'no_scope' : 'unavailable'
  } finally {
    loading.value = false
  }
}

function isForbidden(error: unknown) {
  if (typeof error !== 'object' || error == null) return false
  const response = (error as { response?: { status?: number } }).response
  return response?.status === 403
}

function formatDate(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function buildDefaultOverviewParams(): TeamUsageOverviewParams {
  const end = new Date()
  const start = new Date(end)
  start.setDate(end.getDate() - 29)
  return {
    start_date: formatDate(start),
    end_date: formatDate(end),
    granularity: 'day',
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
  }
}

function formatSummaryCost(value: number | null | undefined, unitLabel: string) {
  if (value == null) return '-'
  return `${value.toFixed(2)} ${unitLabel}`
}

function openMember(userID: number) {
  void router.push({ path: '/', query: { subject_user_id: String(userID) } })
}

onMounted(loadOverview)
</script>

<template>
  <AppLayout>
    <div class="space-y-4">
      <header class="flex items-center justify-between">
        <h1 class="text-xl font-semibold text-slate-950">{{ t('teamUsage.title') }}</h1>
      </header>

      <section v-if="loading && !overview" class="rounded-lg border border-slate-200 bg-white p-4 text-sm text-slate-500 shadow-sm">
        {{ t('settings.loading') }}
      </section>

      <section
        v-else-if="loadError === 'no_scope' || (overview && !overview.is_representative)"
        class="rounded-lg border border-slate-200 bg-white p-4 text-sm text-slate-600 shadow-sm"
      >
        {{ t('teamUsage.noScope') }}
      </section>

      <section
        v-else-if="loadError === 'unavailable'"
        class="rounded-lg border border-slate-200 bg-white p-4 text-sm text-slate-600 shadow-sm"
      >
        {{ t('teamUsage.unavailable') }}
      </section>

      <template v-else-if="overview">
        <section class="grid gap-3 md:grid-cols-4">
          <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
            <div class="text-xs font-medium text-slate-500">{{ t('teamUsage.scopedMembers') }}</div>
            <div class="mt-1 text-xl font-semibold tabular-nums text-slate-950">
              {{ overview.summary.member_count.toLocaleString() }}
            </div>
          </div>
          <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
            <div class="text-xs font-medium text-slate-500">{{ t('teamUsage.relayMembers') }}</div>
            <div class="mt-1 text-xl font-semibold tabular-nums text-slate-950">
              {{ overview.summary.relay_member_count.toLocaleString() }}
            </div>
          </div>
          <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
            <div class="text-xs font-medium text-slate-500">{{ t('teamUsage.todayActualCost') }}</div>
            <div class="mt-1 text-xl font-semibold tabular-nums text-slate-950">
              {{ formatSummaryCost(overview.summary.today_actual_cost, overview.summary.unit_label) }}
            </div>
          </div>
          <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
            <div class="text-xs font-medium text-slate-500">{{ t('teamUsage.totalActualCost') }}</div>
            <div class="mt-1 text-xl font-semibold tabular-nums text-slate-950">
              {{ formatSummaryCost(overview.summary.total_actual_cost, overview.summary.unit_label) }}
            </div>
          </div>
        </section>

        <section
          v-if="scopeTooLarge"
          class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800"
        >
          {{ t('teamUsage.scopeTooLarge') }}
        </section>

        <TeamOverviewMemberTrendChart :state="overview.top_member_trend" />
        <TeamOverviewMemberTable :members="overview.members" @open-member="openMember" />
      </template>
    </div>
  </AppLayout>
</template>
