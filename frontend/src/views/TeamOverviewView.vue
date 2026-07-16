<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import TeamOverviewMemberTrendChart from '@/components/team-usage/TeamOverviewMemberTrendChart.vue'
import TeamOverviewMemberTable from '@/components/team-usage/TeamOverviewMemberTable.vue'
import UsageCenterTabs from '@/components/user/usage/UsageCenterTabs.vue'
import { getTeamUsageOverview, getTeamUsageSummary } from '@/api/teamUsage'
import { useI18n } from '@/i18n'
import { formatTokenCount } from '@/utils/formatters'
import type { TeamOverviewResponse, TeamUsageOverviewParams, TeamUsageSummaryResponse } from '@/types'

const { t } = useI18n()
const router = useRouter()
const summary = ref<TeamUsageSummaryResponse | null>(null)
const overview = ref<TeamOverviewResponse | null>(null)
const summaryLoading = ref(false)
const sectionsLoading = ref(false)
const summaryError = ref<'no_scope' | 'unavailable' | null>(null)
const sectionsError = ref<'no_scope' | 'unavailable' | null>(null)
type RangeOption = 'today' | '7d' | '30d'
const selectedRange = ref<RangeOption>('30d')
let summaryRequestSeq = 0
let overviewRequestSeq = 0

const loading = computed(() => summaryLoading.value || sectionsLoading.value)

const scopeTooLarge = computed(() => {
  return summary.value?.summary.unavailable_reason === 'scope_too_large'
    || overview.value?.top_member_trend.unavailable_reason === 'scope_too_large'
})

const summaryPartiallyUnavailable = computed(() => {
  return summary.value?.summary.unavailable === true
    && summary.value.summary.unavailable_reason !== 'scope_too_large'
})

async function loadSummary(params: TeamUsageOverviewParams) {
  const requestSeq = ++summaryRequestSeq
  summaryLoading.value = true
  summaryError.value = null
  try {
    const response = await getTeamUsageSummary(params)
    if (requestSeq !== summaryRequestSeq) return
    summary.value = response.data.data ?? null
  } catch (error) {
    if (requestSeq !== summaryRequestSeq) return
    summary.value = null
    summaryError.value = isForbidden(error) ? 'no_scope' : 'unavailable'
  } finally {
    if (requestSeq === summaryRequestSeq) {
      summaryLoading.value = false
    }
  }
}

async function loadSections(params: TeamUsageOverviewParams) {
  const requestSeq = ++overviewRequestSeq
  sectionsLoading.value = true
  sectionsError.value = null
  try {
    const response = await getTeamUsageOverview(params)
    if (requestSeq !== overviewRequestSeq) return
    overview.value = response.data.data ?? null
  } catch (error) {
    if (requestSeq !== overviewRequestSeq) return
    overview.value = null
    sectionsError.value = isForbidden(error) ? 'no_scope' : 'unavailable'
  } finally {
    if (requestSeq === overviewRequestSeq) {
      sectionsLoading.value = false
    }
  }
}

function loadOverview() {
  const params = buildOverviewParams(selectedRange.value)
  void loadSummary(params)
  void loadSections(params)
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

function buildOverviewParams(range: RangeOption): TeamUsageOverviewParams {
  const end = new Date()
  const start = new Date(end)
  if (range === 'today') {
    return {
      start_date: formatDate(start),
      end_date: formatDate(end),
      granularity: 'hour',
      timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
    }
  }
  start.setDate(end.getDate() - (range === '7d' ? 6 : 29))
  return {
    start_date: formatDate(start),
    end_date: formatDate(end),
    granularity: 'day',
    timezone: Intl.DateTimeFormat().resolvedOptions().timeZone,
  }
}

function rangeButtonClass(active: boolean) {
  return [
    'rounded-md px-3 py-2 text-sm font-medium transition-colors disabled:cursor-not-allowed disabled:opacity-60',
    active
      ? 'bg-blue-600 text-white'
      : 'border border-gray-300 bg-white text-gray-700 hover:bg-gray-50',
  ]
}

function selectRange(range: RangeOption) {
  selectedRange.value = range
  void loadOverview()
}

function formatSummaryCost(value: number | null | undefined, unitLabel: string) {
  if (value == null) return '-'
  return `${value.toFixed(2)} ${unitLabel}`
}

function openMember(userID: number) {
  void router.push({ name: 'UsageMember', params: { user_id: String(userID) } })
}

onMounted(loadOverview)
</script>

<template>
  <AppLayout>
    <div class="space-y-4">
      <div class="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <UsageCenterTabs active="team" />
        <div class="flex flex-wrap items-center gap-2">
          <button data-test="range-today" type="button" :class="rangeButtonClass(selectedRange === 'today')" :disabled="loading" @click="selectRange('today')">
            {{ t('usageDashboard.today') }}
          </button>
          <button data-test="range-7d" type="button" :class="rangeButtonClass(selectedRange === '7d')" :disabled="loading" @click="selectRange('7d')">
            {{ t('usageDashboard.sevenDays') }}
          </button>
          <button data-test="range-30d" type="button" :class="rangeButtonClass(selectedRange === '30d')" :disabled="loading" @click="selectRange('30d')">
            {{ t('usageDashboard.thirtyDays') }}
          </button>
        </div>
      </div>

      <section
        v-if="summaryError === 'no_scope'"
        class="rounded-lg border border-slate-200 bg-white p-4 text-sm text-slate-600 shadow-sm"
      >
        {{ t('teamUsage.noScope') }}
      </section>

      <div
        v-else
        data-testid="team-overview-content"
        :aria-busy="loading ? 'true' : 'false'"
        :class="['space-y-4 transition-opacity', loading ? 'opacity-60' : 'opacity-100']"
      >
        <div
          v-if="loading && (summary || overview)"
          data-testid="team-overview-refreshing"
          class="rounded-lg border border-blue-100 bg-blue-50 px-4 py-2 text-sm font-medium text-blue-700"
        >
          {{ t('teamUsage.updating') }}
        </div>

        <section
          v-if="summaryLoading && !summary"
          data-testid="team-overview-summary-loading"
          class="rounded-lg border border-slate-200 bg-white p-4 text-sm text-slate-500 shadow-sm"
        >
          {{ t('settings.loading') }}
        </section>
        <section
          v-else-if="summaryError === 'unavailable' && !summary"
          data-testid="team-overview-summary-error"
          class="rounded-lg border border-slate-200 bg-white p-4 text-sm text-slate-600 shadow-sm"
        >
          {{ t('teamUsage.unavailable') }}
        </section>
        <div v-else-if="summary" data-testid="team-overview-summary" class="space-y-3">
          <div
            v-if="summary.cache_status === 'stale'"
            data-testid="team-summary-stale-marker"
            class="rounded-lg border border-amber-200 bg-amber-50 px-4 py-2 text-sm text-amber-800"
          >
            {{ t('usageDashboard.staleSnapshot') }}
          </div>

          <section class="grid gap-3 md:grid-cols-4">
            <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
              <div class="text-xs font-medium text-slate-500">{{ t('teamUsage.scopedMembers') }}</div>
              <div class="mt-1 text-xl font-semibold tabular-nums text-slate-950">
                {{ summary.summary.member_count.toLocaleString() }}
              </div>
            </div>
            <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
              <div class="text-xs font-medium text-slate-500">{{ t('teamUsage.relayMembers') }}</div>
              <div class="mt-1 text-xl font-semibold tabular-nums text-slate-950">
                {{ summary.summary.relay_member_count.toLocaleString() }}
              </div>
            </div>
            <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
              <div class="text-xs font-medium text-slate-500">{{ t('teamUsage.rangeActualCost') }}</div>
              <div class="mt-1 text-xl font-semibold tabular-nums text-slate-950">
                {{ formatSummaryCost(summary.summary.range_actual_cost, summary.summary.unit_label) }}
              </div>
            </div>
            <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
              <div class="text-xs font-medium text-slate-500">{{ t('teamUsage.rangeTotalTokens') }}</div>
              <div class="mt-1 text-xl font-semibold tabular-nums text-slate-950">
                {{ formatTokenCount(summary.summary.range_total_tokens) }}
              </div>
            </div>
          </section>

          <section
            v-if="scopeTooLarge"
            class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800"
          >
            {{ t('teamUsage.scopeTooLarge') }}
          </section>
          <section
            v-else-if="summaryPartiallyUnavailable"
            class="rounded-lg border border-amber-200 bg-amber-50 p-4 text-sm text-amber-800"
          >
            {{ t('teamUsage.summaryUnavailable') }}
          </section>
        </div>

        <section
          v-if="sectionsLoading && !overview"
          data-testid="team-overview-sections-loading"
          class="rounded-lg border border-slate-200 bg-white p-4 text-sm text-slate-500 shadow-sm"
        >
          {{ t('settings.loading') }}
        </section>
        <section
          v-else-if="sectionsError && !overview"
          data-testid="team-overview-sections-error"
          class="rounded-lg border border-slate-200 bg-white p-4 text-sm text-slate-600 shadow-sm"
        >
          {{ t('teamUsage.unavailable') }}
        </section>
        <template v-else-if="overview">
          <TeamOverviewMemberTrendChart
            :state="overview.top_member_trend"
            :department-trend="overview.department_trend"
            :window="overview.window"
          />
          <TeamOverviewMemberTable :members="overview.members" :member-tree="overview.member_tree" @open-member="openMember" />
        </template>
      </div>
    </div>
  </AppLayout>
</template>
