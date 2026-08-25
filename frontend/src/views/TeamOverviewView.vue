<script setup lang="ts">
import { computed, defineAsyncComponent, onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import TeamOverviewMemberTable from '@/components/team-usage/TeamOverviewMemberTable.vue'
import { getTeamUsageMembers, getTeamUsageSummary, getTeamUsageTrend } from '@/api/teamUsage'
import { useTeamUsageOrganization } from '@/composables/useTeamUsageOrganization'
import { useI18n } from '@/i18n'
import { formatTokenCount } from '@/utils/formatters'
import { readUsageWindowPreference, writeUsageWindowPreference } from '@/utils/usageWindowPreference'
import type { UsageWindow } from '@/utils/usageWindowPreference'
import type { TeamUsageMembersResponse, TeamUsageOverviewParams, TeamUsageSummaryResponse, TeamUsageTrendResponse } from '@/types'

const TeamOverviewMemberTrendChart = defineAsyncComponent(
  () => import('@/components/team-usage/TeamOverviewMemberTrendChart.vue'),
)

const { t } = useI18n()
const router = useRouter()
const summary = ref<TeamUsageSummaryResponse | null>(null)
const trend = ref<TeamUsageTrendResponse | null>(null)
const membersPage = ref<TeamUsageMembersResponse | null>(null)
const summaryLoading = ref(false)
const trendLoading = ref(false)
const membersLoading = ref(false)
const summaryError = ref<'no_scope' | 'unavailable' | null>(null)
const trendError = ref<'no_scope' | 'unavailable' | null>(null)
const membersError = ref<'no_scope' | 'unavailable' | null>(null)
const selectedRange = ref<UsageWindow>(readUsageWindowPreference())
let summaryRequestSeq = 0
let trendRequestSeq = 0
let membersRequestSeq = 0
const memberPageCursors = ref<Array<string | null>>([null])
const memberPageIndex = ref(0)
let memberPageParams: TeamUsageOverviewParams | null = null
const {
  branches: organizationBranches,
  rootBranch: organizationRoot,
  invalidatedDepartmentIds: organizationInvalidatedDepartmentIds,
  resetVersion: organizationResetVersion,
  branchFor: organizationBranchFor,
  reset: resetOrganization,
  ensureBranch: ensureOrganizationBranch,
  loadMoreDepartments: loadMoreOrganizationDepartments,
  loadMoreMembers: loadMoreOrganizationMembers,
  retryBranch: retryOrganizationBranch,
} = useTeamUsageOrganization()

const loading = computed(() => summaryLoading.value || membersLoading.value)

const scopeTooLarge = computed(() => {
  return summary.value?.summary.unavailable_reason === 'scope_too_large'
    || trend.value?.top_member_trend.unavailable_reason === 'scope_too_large'
    || trend.value?.department_trend.unavailable_reason === 'scope_too_large'
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

async function loadTrend(params: TeamUsageOverviewParams) {
  const requestSeq = ++trendRequestSeq
  trendLoading.value = true
  trendError.value = null
  try {
    const response = await getTeamUsageTrend(params)
    if (requestSeq !== trendRequestSeq) return
    trend.value = response.data.data ?? null
  } catch (error) {
    if (requestSeq !== trendRequestSeq) return
    trend.value = null
    trendError.value = isForbidden(error) ? 'no_scope' : 'unavailable'
  } finally {
    if (requestSeq === trendRequestSeq) {
      trendLoading.value = false
    }
  }
}

async function loadMembers(
  params: TeamUsageOverviewParams,
  cursor: string | null,
  targetPageIndex: number,
  recoverSnapshot = true,
): Promise<void> {
  const requestSeq = ++membersRequestSeq
  membersLoading.value = true
  membersError.value = null
  try {
    const response = await getTeamUsageMembers({
      ...params,
      cursor: cursor ?? undefined,
      limit: 50,
    })
    if (requestSeq !== membersRequestSeq) return
    membersPage.value = response.data.data ?? null
    memberPageIndex.value = targetPageIndex
    const cursors = memberPageCursors.value.slice(0, targetPageIndex + 1)
    cursors[targetPageIndex] = cursor
    memberPageCursors.value = cursors
  } catch (error) {
    if (requestSeq !== membersRequestSeq) return
    if (recoverSnapshot && cursor != null && isSnapshotExpired(error)) {
      resetMemberPagination()
      return loadMembers(params, null, 0, false)
    }
    if (cursor == null) {
      membersPage.value = null
    }
    membersError.value = isForbidden(error) ? 'no_scope' : 'unavailable'
  } finally {
    if (requestSeq === membersRequestSeq) {
      membersLoading.value = false
    }
  }
}

function loadOverview() {
  const params = buildOverviewParams(selectedRange.value)
  resetMemberPagination()
  memberPageParams = { ...params }
  void loadSummary(params)
  void loadTrend(params)
  void loadMembers(params, null, 0)
  resetOrganization(params)
}

function resetMemberPagination() {
  memberPageCursors.value = [null]
  memberPageIndex.value = 0
}

function loadNextMemberPage() {
  const cursor = membersPage.value?.next_cursor
  if (!cursor || membersLoading.value || memberPageParams == null) return
  void loadMembers(memberPageParams, cursor, memberPageIndex.value + 1)
}

function loadPreviousMemberPage() {
  if (memberPageIndex.value <= 0 || membersLoading.value || memberPageParams == null) return
  const targetPageIndex = memberPageIndex.value - 1
  const cursor = memberPageCursors.value[targetPageIndex] ?? null
  void loadMembers(memberPageParams, cursor, targetPageIndex)
}

function isForbidden(error: unknown) {
  if (typeof error !== 'object' || error == null) return false
  const response = (error as { response?: { status?: number } }).response
  return response?.status === 403
}

function isSnapshotExpired(error: unknown) {
  if (typeof error !== 'object' || error == null) return false
  const response = (error as { response?: { status?: number; data?: { message?: string } } }).response
  return response?.status === 409 && response.data?.message === 'snapshot_expired'
}

function formatDate(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function buildOverviewParams(range: UsageWindow): TeamUsageOverviewParams {
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

function selectRange(range: UsageWindow) {
  selectedRange.value = range
  writeUsageWindowPreference(range)
  void loadOverview()
}

function handleRangeChange(range: string | number | boolean | undefined) {
  if (range === 'today' || range === '7d' || range === '30d') selectRange(range)
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
  <div class="space-y-4">
      <div class="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
        <ElRadioGroup
          :model-value="selectedRange"
          :disabled="loading"
          @change="handleRangeChange"
        >
          <ElRadioButton data-test="range-today" value="today">
            {{ t('usageDashboard.today') }}
          </ElRadioButton>
          <ElRadioButton data-test="range-7d" value="7d">
            {{ t('usageDashboard.sevenDays') }}
          </ElRadioButton>
          <ElRadioButton data-test="range-30d" value="30d">
            {{ t('usageDashboard.thirtyDays') }}
          </ElRadioButton>
        </ElRadioGroup>
      </div>

      <ElAlert
        v-if="summaryError === 'no_scope'"
        type="info"
        :closable="false"
        show-icon
        :title="t('teamUsage.noScope')"
      />

      <div
        v-else
        data-testid="team-overview-content"
        :aria-busy="loading ? 'true' : 'false'"
        :class="['space-y-4 transition-opacity', loading ? 'opacity-60' : 'opacity-100']"
      >
        <ElAlert
          v-if="loading && (summary || trend || membersPage)"
          data-testid="team-overview-refreshing"
          type="info"
          :closable="false"
          show-icon
          :title="t('teamUsage.updating')"
        />

        <ElAlert
          v-if="summaryLoading && !summary"
          data-testid="team-overview-summary-loading"
          type="info"
          :closable="false"
          :title="t('settings.loading')"
        />
        <ElAlert
          v-else-if="summaryError === 'unavailable' && !summary"
          data-testid="team-overview-summary-error"
          type="warning"
          :closable="false"
          show-icon
          :title="t('teamUsage.unavailable')"
        />
        <div v-else-if="summary" data-testid="team-overview-summary" class="space-y-3">
          <ElAlert
            v-if="summary.cache_status === 'stale'"
            data-testid="team-summary-stale-marker"
            type="warning"
            :closable="false"
            show-icon
            :title="t('usageDashboard.staleSnapshot')"
          />

          <section data-testid="team-overview-metrics" class="grid grid-cols-2 gap-3 md:grid-cols-4">
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

          <ElAlert
            v-if="scopeTooLarge"
            type="warning"
            :closable="false"
            show-icon
            :title="t('teamUsage.scopeTooLarge')"
          />
          <ElAlert
            v-else-if="summaryPartiallyUnavailable"
            type="warning"
            :closable="false"
            show-icon
            :title="t('teamUsage.summaryUnavailable')"
          />
        </div>

        <ElAlert
          v-if="trendLoading && !trend"
          data-testid="team-overview-trend-loading"
          type="info"
          :closable="false"
          :title="t('settings.loading')"
        />
        <ElAlert
          v-else-if="trendError && !trend"
          data-testid="team-overview-trend-error"
          type="warning"
          :closable="false"
          show-icon
          :title="trendError === 'no_scope' ? t('teamUsage.noScope') : t('teamUsage.unavailable')"
        />
        <div v-else-if="trend" data-testid="team-overview-trend" class="space-y-3">
          <ElAlert
            v-if="trend.cache_status === 'stale'"
            data-testid="team-trend-stale-marker"
            type="warning"
            :closable="false"
            show-icon
            :title="t('usageDashboard.staleSnapshot')"
          />
          <TeamOverviewMemberTrendChart
            :state="trend.top_member_trend"
            :department-trend="trend.department_trend"
            :window="trend.window"
          />
        </div>

        <ElAlert
          v-if="membersPage?.cache_status === 'stale'"
          data-testid="team-members-stale-marker"
          type="warning"
          :closable="false"
          show-icon
          :title="t('usageDashboard.staleSnapshot')"
        />
        <TeamOverviewMemberTable
          v-if="membersLoading || membersPage || membersError || organizationRoot"
          :members="membersPage?.items ?? []"
          :organization-root="organizationRoot"
          :organization-branches="organizationBranches"
          :organization-invalidated-department-ids="organizationInvalidatedDepartmentIds"
          :organization-reset-version="organizationResetVersion"
          :organization-branch-for="organizationBranchFor"
          :member-loading="membersLoading"
          :member-error="membersError != null"
          :member-total-count="membersPage?.total_count ?? 0"
          :has-previous-page="memberPageIndex > 0"
          :has-next-page="Boolean(membersPage?.next_cursor)"
          @open-member="openMember"
          @previous-page="loadPreviousMemberPage"
          @next-page="loadNextMemberPage"
          @expand-department="ensureOrganizationBranch"
          @load-more-departments="loadMoreOrganizationDepartments"
          @load-more-members="loadMoreOrganizationMembers"
          @retry-organization-branch="retryOrganizationBranch"
        />
      </div>
  </div>
</template>
