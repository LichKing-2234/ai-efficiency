<template>
  <div :class="props.embedded ? 'space-y-6' : 'mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8'">
    <div v-if="props.memberRoute" class="mb-4">
      <RouterLink
        to="/usage/team"
        data-testid="member-usage-back"
        class="inline-flex items-center rounded-md border border-slate-300 bg-white px-3 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
      >
        <span aria-hidden="true" class="mr-1">←</span>
        {{ t('usageDashboard.backToTeamOverview') }}
      </RouterLink>
    </div>

    <div class="mb-6 flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
      <div v-if="!props.embedded || props.memberRoute" class="min-w-0">
        <h1 class="text-2xl font-semibold text-gray-900">
          {{ dashboardTitle }}
        </h1>
        <p class="mt-1 break-words text-sm text-gray-500">{{ dashboardSubtitle }}</p>
      </div>
      <div class="flex max-w-full shrink-0 flex-wrap items-center gap-2 pb-1 sm:flex-nowrap sm:overflow-x-auto">
        <ElSegmented
          v-if="props.unitSwitch"
          v-model="usageUnit"
          data-testid="usage-unit-switch"
          :options="unitOptions"
          class="shrink-0"
        />
        <ElRadioGroup
          :model-value="selectedRange"
          class="max-w-full shrink-0 flex-wrap sm:flex-nowrap sm:overflow-x-auto"
        >
          <ElRadioButton data-test="range-today" value="today" @click="selectRange('today')">
            {{ t('usageDashboard.today') }}
          </ElRadioButton>
          <ElRadioButton data-test="range-7d" value="7d" @click="selectRange('7d')">
            {{ t('usageDashboard.sevenDays') }}
          </ElRadioButton>
          <ElRadioButton data-test="range-30d" value="30d" @click="selectRange('30d')">
            {{ t('usageDashboard.thirtyDays') }}
          </ElRadioButton>
        </ElRadioGroup>
        <ElButton
          :disabled="activeLoading"
          :loading="activeLoading"
          @click="loadDashboard"
        >
          {{ t('usageDashboard.refresh') }}
        </ElButton>
      </div>
    </div>

    <template v-if="usageUnit === 'token'">
    <div v-if="usageLoading && !currentSnapshot" class="flex min-h-80 items-center justify-center text-sm text-gray-500">
      {{ t('usageDashboard.loading') }}
    </div>

    <ElAlert v-else-if="setupRequired" type="warning" :closable="false" show-icon>
      <template #title>{{ t('usageDashboard.setupTitle') }}</template>
      <p class="text-sm">{{ t('usageDashboard.setupHelp') }}</p>
      <router-link to="/user" class="mt-4 inline-flex rounded-md bg-amber-600 px-4 py-2 text-sm font-medium text-white hover:bg-amber-700">
        {{ t('usageDashboard.openSetup') }}
      </router-link>
    </ElAlert>

    <ElAlert v-else-if="usageErrorKind" type="error" :closable="false" show-icon>
      <template #title>{{ usageErrorTitle }}</template>
      <p class="text-sm">{{ usageErrorHelp }}</p>
      <router-link v-if="usageErrorKind === 'credential' && !props.homeMode" to="/user" class="mt-4 inline-flex rounded-md bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700">
        {{ t('usageDashboard.openSetup') }}
      </router-link>
      <ElButton
        v-if="showUsageErrorRetry"
        data-testid="usage-error-retry"
        type="primary"
        class="mt-4"
        @click="loadDashboard"
      >
        {{ t('usageDashboard.errorRetry') }}
      </ElButton>
    </ElAlert>

    <div v-else class="space-y-6">
      <ElAlert
        v-if="usageIsStale"
        data-testid="usage-stale-marker"
        type="warning"
        :closable="false"
        show-icon
        :title="t('usageDashboard.staleSnapshot')"
      />
      <Suspense v-if="selectedMemberSubject && selectedSubjectSubscriptions.length > 0">
        <SelectedSubjectSubscriptionRows
          :subject-user-id="selectedMemberSubject.user_id"
          :rows="selectedSubjectSubscriptions"
          :update-multiplier="handleMultiplierConfirm"
        />
        <template #fallback>
          <div class="rounded-lg border border-slate-200 bg-white p-4 text-sm text-slate-500">
            {{ t('usageDashboard.loading') }}
          </div>
        </template>
      </Suspense>
      <Suspense>
        <UsageGroupQuotaSection
          :quotas="currentGroupQuotas"
          :pool-usage="props.memberRoute ? null : personalPoolUsage"
          :loading="quotaLoading && !currentGroupQuotas"
          :range-label="selectedRangeLabel"
          :show-reset-request="canRequestQuotaReset"
          :reset-request-loading="quotaResetOptionsLoading"
          @request-reset="openQuotaResetModal"
        />
        <template #fallback>
          <div class="rounded-lg border border-gray-200 bg-white p-4 text-sm text-gray-500" data-testid="usage-group-quotas-loading">
            {{ t('usageDashboard.loading') }}
          </div>
        </template>
      </Suspense>
      <UsageStatsCards
        :stats="currentSnapshot?.stats ?? null"
        :trend="currentSnapshot?.trend ?? []"
        :range-label="snapshotRangeLabel"
        :hide-cost="props.homeMode"
        :loading="usageLoading && !!currentSnapshot"
        :credits="creditsForCard"
      />
      <div v-if="hasUsableUsage" class="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
        <UsageTrendChart :data="currentSnapshot?.trend ?? []" :loading="usageLoading" />
        <UsageModelChart :data="currentSnapshot?.models ?? []" :loading="usageLoading" :hide-cost="props.homeMode" />
      </div>
    </div>
    </template>

    <template v-else>
      <KiroCreditsDashboard
        :snapshot="kiroUsage"
        :loading="kiroLoading"
        :range-label="snapshotRangeLabel"
      />
    </template>

    <QuotaResetRequestModal
      v-if="quotaResetModalOpen"
      :open="quotaResetModalOpen"
      :groups="quotaResetGroups"
      :submitting="quotaResetSubmitting || quotaResetOptionsLoading"
      @close="quotaResetModalOpen = false"
      @submit="submitQuotaResetRequest"
    />
  </div>
</template>

<script setup lang="ts">
import { computed, defineAsyncComponent, onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { useRoute } from 'vue-router'
import { createQuotaResetRequest, getQuotaResetOptions } from '@/api/quotaReset'
import { getUserKiroUsage, getUserUsageDashboard, getUserUsageGroupPoolUsage, getUserUsageGroupQuotas } from '@/api/userUsage'
import { getTeamUsageSubjectDashboard, getTeamUsageSubjectCredits, updateTeamUsageRateMultiplier } from '@/api/teamUsage'
import { useI18n } from '@/i18n'
import { readUsageWindowPreference, writeUsageWindowPreference } from '@/utils/usageWindowPreference'
import type { UsageWindow } from '@/utils/usageWindowPreference'
import type {
  QuotaResetOptionGroup,
  SubjectSubscriptionGroup,
  TeamUsageSubject,
  UpdateTeamUsageRateMultiplierRequest,
  UserUsageDashboardParams,
  UserUsageDashboardSnapshot,
  UserUsageGroupPoolUsageState,
  UserUsageGroupQuotaState,
  UserUsageKiroSnapshot,
} from '@/types'

const UsageStatsCards = defineAsyncComponent(() => import('@/components/user/usage/UsageStatsCards.vue'))
const UsageTrendChart = defineAsyncComponent(() => import('@/components/user/usage/UsageTrendChart.vue'))
const UsageModelChart = defineAsyncComponent(() => import('@/components/user/usage/UsageModelChart.vue'))
const UsageGroupQuotaSection = defineAsyncComponent(() => import('@/components/user/usage/UsageGroupQuotaSection.vue'))
const SelectedSubjectSubscriptionRows = defineAsyncComponent(() => import('@/components/user/usage/SelectedSubjectSubscriptionRows.vue'))
const KiroCreditsDashboard = defineAsyncComponent(() => import('@/components/user/usage/KiroCreditsDashboard.vue'))
const loadQuotaResetRequestModal = () => import('@/components/quota-reset/QuotaResetRequestModal.vue')
const QuotaResetRequestModal = defineAsyncComponent(loadQuotaResetRequestModal)

const props = withDefaults(defineProps<{
  embedded?: boolean
  homeMode?: boolean
  memberRoute?: boolean
  unitSwitch?: boolean
  initialSnapshot?: UserUsageDashboardSnapshot | null
}>(), {
  embedded: false,
  homeMode: false,
  memberRoute: false,
  unitSwitch: false,
  initialSnapshot: null,
})

const initialRange = readUsageWindowPreference()
const selectedRange = ref<UsageWindow>(initialRange)
const snapshotRange = ref<UsageWindow>(initialRange)
const snapshot = ref<UserUsageDashboardSnapshot | null>(props.initialSnapshot)
const personalQuotas = ref<UserUsageGroupQuotaState | null>(props.initialSnapshot?.group_quotas ?? null)
const personalPoolUsage = ref<UserUsageGroupPoolUsageState | null>(null)
const kiroUsage = ref<UserUsageKiroSnapshot | null>(null)
const kiroLoading = ref(false)
const memberCredits = ref<number | null>(null)
const memberRouteSubject = ref<TeamUsageSubject | null>(null)
const selectedSubjectSubscriptions = ref<SubjectSubscriptionGroup[]>([])
const usageLoading = ref(!props.initialSnapshot)
type UsageUnit = 'token' | 'credits'
const USAGE_UNIT_KEY = 'ae.usage.unit'
function readUsageUnit(): UsageUnit {
  try {
    return localStorage.getItem(USAGE_UNIT_KEY) === 'credits' ? 'credits' : 'token'
  } catch {
    return 'token'
  }
}
const usageUnit = ref<UsageUnit>(props.unitSwitch ? readUsageUnit() : 'token')
const unitOptions = computed(() => [
  { label: t('usageDashboard.unitToken'), value: 'token' },
  { label: t('usageDashboard.unitCredits'), value: 'credits' },
])
const activeLoading = computed(() => usageUnit.value === 'credits' ? kiroLoading.value : usageLoading.value)
const creditsForCard = computed(() => {
  if (props.memberRoute) return memberCredits.value
  return kiroUsage.value?.available === true ? kiroUsage.value.credits : null
})
watch(usageUnit, (value) => {
  try {
    localStorage.setItem(USAGE_UNIT_KEY, value)
  } catch {
    // Preference persistence is best effort.
  }
})
const quotaLoading = ref(false)
type UsageErrorKind = '' | 'credential' | 'transient' | 'unexpected'
const usageErrorKind = ref<UsageErrorKind>('')
const quotaResetModalOpen = ref(false)
const quotaResetOptionsLoading = ref(false)
const quotaResetSubmitting = ref(false)
const quotaResetGroups = ref<QuotaResetOptionGroup[]>([])
const { t } = useI18n()
const route = useRoute()
let requestGeneration = 0
let usageController: AbortController | null = null
let quotaController: AbortController | null = null
let poolController: AbortController | null = null
let kiroController: AbortController | null = null

const currentSnapshot = computed(() => snapshot.value)
const currentGroupQuotas = computed(() => props.memberRoute ? currentSnapshot.value?.group_quotas ?? null : personalQuotas.value)
const setupRequired = computed(() => !props.homeMode && currentSnapshot.value?.configured === false)
const usageIsStale = computed(() => currentSnapshot.value?.usage_freshness?.cache_status === 'stale')
const hasUsableUsage = computed(() => currentSnapshot.value?.configured === true && currentSnapshot.value?.stats != null)
const selectedRangeLabel = computed(() => rangeLabel(selectedRange.value))
const snapshotRangeLabel = computed(() => rangeLabel(snapshotRange.value))
const dashboardTitle = computed(() => {
  if (props.memberRoute) return t('usageDashboard.memberTitle')
  return props.embedded ? t('usageDashboard.embeddedTitle') : t('usageDashboard.title')
})
const dashboardSubtitle = computed(() => {
  if (!props.memberRoute) return t('usageDashboard.subtitle')
  const subject = memberRouteSubject.value
  if (!subject) return t('usageDashboard.memberSubtitle')
  return [subject.email, subject.department_display_path].filter(Boolean).join(' · ') || t('usageDashboard.memberSubtitle')
})
const selectedMemberSubject = computed(() => props.memberRoute ? memberRouteSubject.value : null)
const canRequestQuotaReset = computed(() => !props.memberRoute && (currentGroupQuotas.value?.groups?.length ?? 0) > 0)
const usageErrorTitle = computed(() => {
  if (usageErrorKind.value === 'credential') return t('usageDashboard.credentialError')
  if (usageErrorKind.value === 'transient') return t('usageDashboard.transientErrorTitle')
  if (usageErrorKind.value === 'unexpected') return t('usageDashboard.unavailable')
  return ''
})
const usageErrorHelp = computed(() => {
  if (usageErrorKind.value === 'credential') return t('usageDashboard.retryHelp')
  if (usageErrorKind.value === 'transient') return t('usageDashboard.transientErrorHelp')
  if (usageErrorKind.value === 'unexpected') return t('usageDashboard.unexpectedErrorHelp')
  return ''
})
const showUsageErrorRetry = computed(() => usageErrorKind.value === 'transient' || usageErrorKind.value === 'unexpected')

function classifyUsageError(error: any): UsageErrorKind {
  const status = error?.response?.status
  if (status === 409) return 'credential'
  if (typeof status === 'number' && status >= 500) return 'transient'
  return 'unexpected'
}

function rangeLabel(range: UsageWindow) {
  if (range === 'today') return t('usageDashboard.today')
  if (range === '7d') return t('usageDashboard.sevenDays')
  return t('usageDashboard.thirtyDays')
}

function formatDate(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function buildParams(range: UsageWindow): UserUsageDashboardParams {
  const end = new Date()
  const start = new Date(end)
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
  if (range === 'today') {
    return { start_date: formatDate(start), end_date: formatDate(end), granularity: 'hour', timezone }
  }
  start.setDate(end.getDate() - (range === '7d' ? 6 : 29))
  return { start_date: formatDate(start), end_date: formatDate(end), granularity: 'day', timezone }
}

function routeSubjectUserID() {
  const raw = Array.isArray(route.params.user_id) ? route.params.user_id[0] : route.params.user_id
  const subjectUserID = Number(raw)
  return Number.isInteger(subjectUserID) && subjectUserID > 0 ? subjectUserID : null
}

function abortPersonalRequests() {
  usageController?.abort()
  quotaController?.abort()
  poolController?.abort()
  kiroController?.abort()
  usageController = null
  quotaController = null
  poolController = null
  kiroController = null
}

function isCanceled(error: any, signal: AbortSignal) {
  return signal.aborted || error?.name === 'AbortError' || error?.name === 'CanceledError' || error?.code === 'ERR_CANCELED'
}

function loadDashboard(): Promise<void> {
  const requestedRange = selectedRange.value
  const generation = ++requestGeneration
  abortPersonalRequests()
  return props.memberRoute
    ? loadMemberDashboard(generation, requestedRange)
    : loadPersonalDashboard(generation, requestedRange)
}

async function loadMemberDashboard(generation: number, requestedRange: UsageWindow) {
  usageLoading.value = true
  quotaLoading.value = false
  usageErrorKind.value = ''
  selectedSubjectSubscriptions.value = []
  kiroUsage.value = null
  kiroLoading.value = false
  memberCredits.value = null
  try {
    const routeUserID = routeSubjectUserID()
    if (routeUserID == null) throw new Error('invalid member route')
    const response = await getTeamUsageSubjectDashboard(routeUserID, buildParams(requestedRange))
    if (generation !== requestGeneration) return
    snapshot.value = response.data.data ?? null
    memberRouteSubject.value = (response.data.data as any)?.subject ?? null
    selectedSubjectSubscriptions.value = (response.data.data as any)?.subject_subscription_groups ?? []
    snapshotRange.value = requestedRange
    const creditsResponse = await getTeamUsageSubjectCredits(routeUserID, buildParams(requestedRange)).catch(() => null)
    if (generation !== requestGeneration) return
    const creditsUser = creditsResponse?.data?.data?.users?.[0]
    memberCredits.value = creditsUser?.key_found ? creditsUser.credits : null
  } catch (error: any) {
    if (generation !== requestGeneration) return
    memberRouteSubject.value = null
    selectedSubjectSubscriptions.value = []
    usageErrorKind.value = classifyUsageError(error)
  } finally {
    if (generation === requestGeneration) usageLoading.value = false
  }
}

async function loadPersonalDashboard(generation: number, requestedRange: UsageWindow) {
  const params = buildParams(requestedRange)
  const nextUsageController = new AbortController()
  const nextQuotaController = new AbortController()
  const nextPoolController = new AbortController()
  const nextKiroController = new AbortController()
  usageController = nextUsageController
  quotaController = nextQuotaController
  poolController = nextPoolController
  kiroController = nextKiroController
  usageLoading.value = true
  quotaLoading.value = true
  personalQuotas.value = null
  personalPoolUsage.value = null
  kiroUsage.value = null
  kiroLoading.value = true
  usageErrorKind.value = ''
  selectedSubjectSubscriptions.value = []

  const usageTask = getUserUsageDashboard(params, nextUsageController.signal)
    .then((response) => {
      if (generation !== requestGeneration || nextUsageController.signal.aborted) return
      const nextSnapshot = response.data.data ?? null
      snapshot.value = nextSnapshot
      if (nextSnapshot?.group_quotas) personalQuotas.value = nextSnapshot.group_quotas
      snapshotRange.value = requestedRange
    })
    .catch((error: any) => {
      if (generation !== requestGeneration || isCanceled(error, nextUsageController.signal)) return
      usageErrorKind.value = classifyUsageError(error)
    })
    .finally(() => {
      if (generation === requestGeneration) usageLoading.value = false
    })

  const quotaTask = getUserUsageGroupQuotas(params, nextQuotaController.signal)
    .then((response) => {
      if (generation !== requestGeneration || nextQuotaController.signal.aborted) return
      personalQuotas.value = response.data.data?.group_quotas ?? null
    })
    .catch((error: any) => {
      if (generation !== requestGeneration || isCanceled(error, nextQuotaController.signal)) return
      personalQuotas.value = {
        status: 'unavailable',
        message: t('usageDashboard.groupQuotasUnavailable'),
        groups: [],
      }
    })
    .finally(() => {
      if (generation === requestGeneration) quotaLoading.value = false
    })

  const poolTask = getUserUsageGroupPoolUsage(params, nextPoolController.signal)
    .then((response) => {
      if (generation !== requestGeneration || nextPoolController.signal.aborted) return
      const pool = response.data.data?.group_pool_usage
      personalPoolUsage.value = pool?.status === 'ok' && (pool.groups?.length ?? 0) > 0 ? pool : null
    })
    .catch((error: any) => {
      if (generation !== requestGeneration || isCanceled(error, nextPoolController.signal)) return
      personalPoolUsage.value = null
    })

  const kiroTask = getUserKiroUsage(params, nextKiroController.signal)
    .then((response) => {
      if (generation !== requestGeneration || nextKiroController.signal.aborted) return
      kiroUsage.value = response.data.data ?? null
    })
    .catch((error: any) => {
      if (generation !== requestGeneration || isCanceled(error, nextKiroController.signal)) return
      kiroUsage.value = null
    })
    .finally(() => {
      if (generation === requestGeneration) kiroLoading.value = false
    })

  await Promise.allSettled([usageTask, quotaTask, poolTask, kiroTask])
}

function selectRange(range: UsageWindow) {
  selectedRange.value = range
  writeUsageWindowPreference(range)
  void loadDashboard()
}

async function handleMultiplierConfirm(event: { subjectUserId: number; groupID: string; payload: UpdateTeamUsageRateMultiplierRequest }) {
  await updateTeamUsageRateMultiplier(event.subjectUserId, event.groupID, event.payload)
  await loadDashboard()
}

async function openQuotaResetModal() {
  if (quotaResetOptionsLoading.value) return
  quotaResetOptionsLoading.value = true
  try {
    const [response] = await Promise.all([getQuotaResetOptions(), loadQuotaResetRequestModal()])
    quotaResetGroups.value = response.data.data?.groups ?? []
    quotaResetModalOpen.value = true
  } catch {
    ElMessage.error(t('quotaReset.optionsLoadFailed'))
  } finally {
    quotaResetOptionsLoading.value = false
  }
}

async function submitQuotaResetRequest(payload: { group_id: string; reason: string }) {
  quotaResetSubmitting.value = true
  try {
    await createQuotaResetRequest(payload)
    quotaResetModalOpen.value = false
    ElMessage.success(t('quotaReset.requestSubmitted'))
  } catch (error: any) {
    if (error?.response?.data?.message === 'subscription_limit_required') {
      ElMessage.error(t('quotaReset.subscriptionLimitRequired'))
    } else {
      ElMessage.error(t('quotaReset.requestSubmitFailed'))
    }
  } finally {
    quotaResetSubmitting.value = false
  }
}

onMounted(() => {
  if (props.initialSnapshot) {
    snapshotRange.value = selectedRange.value
    return
  }
  void loadDashboard()
})

onBeforeUnmount(() => {
  requestGeneration++
  abortPersonalRequests()
})
</script>
