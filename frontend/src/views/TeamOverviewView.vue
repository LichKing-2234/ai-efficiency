<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import TeamOverviewMemberTrendChart from '@/components/team-usage/TeamOverviewMemberTrendChart.vue'
import TeamOverviewMemberTable from '@/components/team-usage/TeamOverviewMemberTable.vue'
import UsageCenterTabs from '@/components/user/usage/UsageCenterTabs.vue'
import { getTeamUsageOverview } from '@/api/teamUsage'
import { useI18n } from '@/i18n'
import { formatTokenCount } from '@/utils/formatters'
import type { TeamOverviewResponse, TeamUsageOverviewParams } from '@/types'

const { t } = useI18n()
const router = useRouter()
const overview = ref<TeamOverviewResponse | null>(null)
const loading = ref(false)
const loadError = ref<'no_scope' | 'unavailable' | null>(null)
type RangeOption = 'today' | '7d' | '30d'
const selectedRange = ref<RangeOption>('30d')
let overviewRequestSeq = 0

const scopeTooLarge = computed(() => {
  return overview.value?.summary.unavailable_reason === 'scope_too_large'
    || overview.value?.top_member_trend.unavailable_reason === 'scope_too_large'
})

const summaryPartiallyUnavailable = computed(() => {
  return overview.value?.summary.unavailable === true
    && overview.value.summary.unavailable_reason !== 'scope_too_large'
})

async function loadOverview() {
  const requestSeq = ++overviewRequestSeq
  loading.value = true
  loadError.value = null
  try {
    const range = selectedRange.value
    const response = await getTeamUsageOverview(buildOverviewParams(range))
    if (requestSeq !== overviewRequestSeq) return
    overview.value = response.data.data ?? null
  } catch (error) {
    if (requestSeq !== overviewRequestSeq) return
    overview.value = null
    loadError.value = isForbidden(error) ? 'no_scope' : 'unavailable'
  } finally {
    if (requestSeq === overviewRequestSeq) {
      loading.value = false
    }
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
        <div
          data-testid="team-overview-content"
          :aria-busy="loading ? 'true' : 'false'"
          :class="['space-y-4 transition-opacity', loading ? 'opacity-60' : 'opacity-100']"
        >
          <div
            v-if="loading"
            data-testid="team-overview-refreshing"
            class="rounded-lg border border-blue-100 bg-blue-50 px-4 py-2 text-sm font-medium text-blue-700"
          >
            {{ t('teamUsage.updating') }}
          </div>

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
              <div class="text-xs font-medium text-slate-500">{{ t('teamUsage.rangeActualCost') }}</div>
              <div class="mt-1 text-xl font-semibold tabular-nums text-slate-950">
                {{ formatSummaryCost(overview.summary.range_actual_cost, overview.summary.unit_label) }}
              </div>
            </div>
            <div class="rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
              <div class="text-xs font-medium text-slate-500">{{ t('teamUsage.rangeTotalTokens') }}</div>
              <div class="mt-1 text-xl font-semibold tabular-nums text-slate-950">
                {{ formatTokenCount(overview.summary.range_total_tokens) }}
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

          <TeamOverviewMemberTrendChart :state="overview.top_member_trend" :window="overview.window" />
          <TeamOverviewMemberTable :members="overview.members" :member-tree="overview.member_tree" @open-member="openMember" />
        </div>
      </template>
    </div>
  </AppLayout>
</template>
