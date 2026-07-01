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
      <div>
        <component
          :is="props.embedded ? 'h2' : 'h1'"
          :class="props.embedded ? 'text-base font-semibold text-slate-950' : 'text-2xl font-semibold text-gray-900'"
        >
          {{ dashboardTitle }}
        </component>
        <p class="mt-1 text-sm text-gray-500">{{ dashboardSubtitle }}</p>
      </div>
      <div class="flex flex-wrap items-center gap-2">
        <button data-test="range-today" type="button" :class="rangeButtonClass(selectedRange === 'today')" @click="selectRange('today')">
          {{ t('usageDashboard.today') }}
        </button>
        <button data-test="range-7d" type="button" :class="rangeButtonClass(selectedRange === '7d')" @click="selectRange('7d')">
          {{ t('usageDashboard.sevenDays') }}
        </button>
        <button data-test="range-30d" type="button" :class="rangeButtonClass(selectedRange === '30d')" @click="selectRange('30d')">
          {{ t('usageDashboard.thirtyDays') }}
        </button>
        <button
          type="button"
          class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-60"
          :disabled="loading"
          @click="loadDashboard"
        >
          {{ t('usageDashboard.refresh') }}
        </button>
      </div>
    </div>

    <div v-if="loading && !currentSnapshot" class="flex min-h-80 items-center justify-center text-sm text-gray-500">
      {{ t('usageDashboard.loading') }}
    </div>

    <div v-else-if="setupRequired" class="rounded-lg border border-amber-200 bg-amber-50 p-6">
      <h2 class="text-base font-semibold text-amber-900">{{ t('usageDashboard.setupTitle') }}</h2>
      <p class="mt-2 text-sm text-amber-800">{{ t('usageDashboard.setupHelp') }}</p>
      <router-link to="/user" class="mt-4 inline-flex rounded-md bg-amber-600 px-4 py-2 text-sm font-medium text-white hover:bg-amber-700">
        {{ t('usageDashboard.openSetup') }}
      </router-link>
    </div>

    <div v-else-if="errorMessage" class="rounded-lg border border-red-200 bg-red-50 p-6">
      <h2 class="text-base font-semibold text-red-900">{{ errorMessage }}</h2>
      <p class="mt-2 text-sm text-red-800">{{ t('usageDashboard.retryHelp') }}</p>
      <router-link v-if="credentialError" to="/user" class="mt-4 inline-flex rounded-md bg-red-600 px-4 py-2 text-sm font-medium text-white hover:bg-red-700">
        {{ t('usageDashboard.openSetup') }}
      </router-link>
    </div>

    <div v-else class="space-y-6">
      <div v-if="!props.memberRoute && hasMemberSubjects" class="flex justify-end">
        <UserUsageSubjectSelector
          v-model="selectedSubjectValue"
          :subjects="subjects"
          @select="selectSubject"
        />
      </div>
      <SelectedSubjectSubscriptionRows
        v-if="selectedMemberSubject && selectedSubjectSubscriptions.length > 0"
        :subject-user-id="selectedMemberSubject.user_id"
        :rows="selectedSubjectSubscriptions"
        :update-multiplier="handleMultiplierConfirm"
      />
      <UsageGroupQuotaSection
        :quotas="currentSnapshot?.group_quotas ?? null"
        :loading="loading && !!currentSnapshot"
        :range-label="selectedRangeLabel"
      />
      <UsageStatsCards
        :stats="currentSnapshot?.stats ?? null"
        :trend="currentSnapshot?.trend ?? []"
        :range-label="snapshotRangeLabel"
        :hide-cost="props.homeMode"
        :loading="loading && !!currentSnapshot"
      />
      <div class="grid min-w-0 grid-cols-1 gap-6 xl:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]">
        <UsageTrendChart :data="currentSnapshot?.trend ?? []" :loading="loading" />
        <UsageModelChart :data="currentSnapshot?.models ?? []" :loading="loading" :hide-cost="props.homeMode" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import { getUserUsageDashboard } from '@/api/userUsage'
import {
  getTeamUsageSubjectDashboard,
  listTeamUsageSubjects,
  updateTeamUsageRateMultiplier,
} from '@/api/teamUsage'
import { useI18n } from '@/i18n'
import type {
  SubjectSubscriptionGroup,
  TeamUsageSubject,
  UpdateTeamUsageRateMultiplierRequest,
  UserUsageDashboardParams,
  UserUsageDashboardSnapshot,
} from '@/types'
import { useAuthStore } from '@/stores/auth'
import UsageStatsCards from '@/components/user/usage/UsageStatsCards.vue'
import UsageTrendChart from '@/components/user/usage/UsageTrendChart.vue'
import UsageModelChart from '@/components/user/usage/UsageModelChart.vue'
import UsageGroupQuotaSection from '@/components/user/usage/UsageGroupQuotaSection.vue'
import UserUsageSubjectSelector from '@/components/user/usage/UserUsageSubjectSelector.vue'
import SelectedSubjectSubscriptionRows from '@/components/user/usage/SelectedSubjectSubscriptionRows.vue'

type RangeOption = 'today' | '7d' | '30d'

const props = withDefaults(defineProps<{
  embedded?: boolean
  homeMode?: boolean
  memberRoute?: boolean
  initialSnapshot?: UserUsageDashboardSnapshot | null
}>(), {
  embedded: false,
  homeMode: false,
  memberRoute: false,
  initialSnapshot: null,
})

const selectedRange = ref<RangeOption>('30d')
const snapshotRange = ref<RangeOption>('30d')
const snapshot = ref<UserUsageDashboardSnapshot | null>(null)
const memberSubjects = ref<TeamUsageSubject[]>([])
const memberRouteSubject = ref<TeamUsageSubject | null>(null)
const auth = useAuthStore()
const selectedSubjectValue = ref(subjectValue(makeSelfSubject()))
const selectedSubjectSubscriptions = ref<SubjectSubscriptionGroup[]>([])
const loading = ref(false)
const errorMessage = ref('')
const credentialError = ref(false)
const { t } = useI18n()
const route = useRoute()
let dashboardRequestSeq = 0
const deepLinkSubjectPageSize = 200

const currentSnapshot = computed(() => snapshot.value ?? props.initialSnapshot)
const setupRequired = computed(() => currentSnapshot.value?.configured === false)
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
const subjects = computed<TeamUsageSubject[]>(() => {
  const self = makeSelfSubject()
  return [
    self,
    ...memberSubjects.value.filter((subject) =>
      subject.subject_type === 'member' && subjectValue(subject) !== subjectValue(self),
    ),
  ]
})
const hasMemberSubjects = computed(() => subjects.value.some((subject) => subject.subject_type === 'member'))
const selectedSubject = computed(() => {
  return subjects.value.find((subject) => subjectValue(subject) === selectedSubjectValue.value)
})
const selectedMemberSubject = computed(() => {
  if (props.memberRoute) return memberRouteSubject.value
  const subject = selectedSubject.value
  if (!subject || subject.subject_type !== 'member') return null
  return subject
})

function rangeLabel(range: RangeOption) {
  if (range === 'today') return t('usageDashboard.today')
  if (range === '7d') return t('usageDashboard.sevenDays')
  return t('usageDashboard.thirtyDays')
}

function rangeButtonClass(active: boolean) {
  return [
    'rounded-md px-3 py-2 text-sm font-medium transition-colors',
    active
      ? 'bg-blue-600 text-white'
      : 'border border-gray-300 text-gray-700 hover:bg-gray-50',
  ]
}

function formatDate(date: Date): string {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

function buildParams(range: RangeOption): UserUsageDashboardParams {
  const end = new Date()
  const start = new Date(end)
  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone

  if (range === 'today') {
    return { start_date: formatDate(start), end_date: formatDate(end), granularity: 'hour', timezone }
  }
  if (range === '7d') {
    start.setDate(end.getDate() - 6)
  } else {
    start.setDate(end.getDate() - 29)
  }
  return { start_date: formatDate(start), end_date: formatDate(end), granularity: 'day', timezone }
}

function subjectValue(subject: TeamUsageSubject) {
  if (subject.subject_type === 'self') return `self:${subject.user_id}`
  const id = subject.user_id > 0 ? String(subject.user_id) : `directory:${subject.directory_member_external_id || subject.email}`
  return `${subject.subject_type}:${id}`
}

function makeSelfSubject(): TeamUsageSubject {
  return {
    subject_type: 'self',
    user_id: auth.user?.id ?? 0,
    display_name: auth.user?.username || auth.user?.email || 'Me',
    email: auth.user?.email || '',
    selectable: true,
  }
}

function routeSubjectUserID() {
  const raw = Array.isArray(route.params.user_id) ? route.params.user_id[0] : route.params.user_id
  const subjectUserID = Number(raw)
  if (!Number.isInteger(subjectUserID) || subjectUserID <= 0) return null
  return subjectUserID
}

async function loadSubjects(options?: { expandForRouteSubject?: boolean }) {
  try {
    memberSubjects.value = options?.expandForRouteSubject
      ? await loadSubjectsForRouteTarget(routeSubjectUserID())
      : await loadDefaultSubjects()
    if (!subjects.value.some((subject) => subjectValue(subject) === selectedSubjectValue.value)) {
      selectedSubjectValue.value = subjectValue(makeSelfSubject())
    }
  } catch {
    memberSubjects.value = []
    selectedSubjectValue.value = subjectValue(makeSelfSubject())
  }
}

async function loadDefaultSubjects() {
  const res = await listTeamUsageSubjects()
  return (res.data.data?.subjects ?? []).filter((subject) => subject.subject_type === 'member')
}

async function loadSubjectsForRouteTarget(targetUserID: number | null) {
  if (targetUserID == null) {
    return loadDefaultSubjects()
  }
  const loaded: TeamUsageSubject[] = []
  let page = 1
  while (true) {
    const res = await listTeamUsageSubjects({ page, page_size: deepLinkSubjectPageSize })
    const data = res.data.data
    const pageSubjects = (data?.subjects ?? []).filter((subject) => subject.subject_type === 'member')
    loaded.push(...pageSubjects)
    if (pageSubjects.some((subject) => subject.user_id === targetUserID)) break
    const pageSize = data?.page_size || deepLinkSubjectPageSize
    if (!data || page * pageSize >= data.total || pageSubjects.length === 0) break
    page += 1
  }
  return loaded
}

function applyRouteSubjectSelection() {
  const subjectUserID = routeSubjectUserID()
  if (subjectUserID == null) return false
  const subject = subjects.value.find((item) => item.subject_type === 'member' && item.user_id === subjectUserID && item.selectable)
  if (!subject) return false
  const next = subjectValue(subject)
  if (selectedSubjectValue.value === next) return false
  selectedSubjectValue.value = next
  return true
}

async function loadDashboard() {
  const requestedRange = selectedRange.value
  const requestSeq = ++dashboardRequestSeq
  loading.value = true
  errorMessage.value = ''
  credentialError.value = false
  selectedSubjectSubscriptions.value = []
  try {
    const params = buildParams(requestedRange)
    const routeUserID = props.memberRoute ? routeSubjectUserID() : null
    if (props.memberRoute && routeUserID == null) {
      throw new Error('invalid member route')
    }
    const subject = selectedSubject.value
    const res = props.memberRoute && routeUserID != null
      ? await getTeamUsageSubjectDashboard(routeUserID, params)
      : subject?.subject_type === 'member'
        ? await getTeamUsageSubjectDashboard(subject.user_id, params)
        : await getUserUsageDashboard(params)
    if (requestSeq !== dashboardRequestSeq) return
    snapshot.value = res.data.data ?? null
    if (props.memberRoute) {
      memberRouteSubject.value = (res.data.data as any)?.subject ?? null
      selectedSubjectSubscriptions.value = (res.data.data as any)?.subject_subscription_groups ?? []
    } else {
      selectedSubjectSubscriptions.value = subject?.subject_type === 'member'
        ? (res.data.data as any)?.subject_subscription_groups ?? []
        : []
    }
    snapshotRange.value = requestedRange
  } catch (err: any) {
    if (requestSeq !== dashboardRequestSeq) return
    snapshot.value = null
    if (props.memberRoute) {
      memberRouteSubject.value = null
    }
    selectedSubjectSubscriptions.value = []
    credentialError.value = err?.response?.status === 409
    errorMessage.value = credentialError.value ? t('usageDashboard.credentialError') : t('usageDashboard.unavailable')
  } finally {
    if (requestSeq === dashboardRequestSeq) {
      loading.value = false
    }
  }
}

function selectRange(range: RangeOption) {
  selectedRange.value = range
  loadDashboard()
}

function selectSubject(subject: TeamUsageSubject) {
  if (!subject.selectable) return
  selectedSubjectValue.value = subjectValue(subject)
  loadDashboard()
}

async function handleMultiplierConfirm(event: { subjectUserId: number; groupID: string; payload: UpdateTeamUsageRateMultiplierRequest }) {
  await updateTeamUsageRateMultiplier(event.subjectUserId, event.groupID, event.payload)
  await loadDashboard()
}

onMounted(async () => {
  if (props.memberRoute) {
    loadDashboard()
    return
  }
  await loadSubjects({ expandForRouteSubject: routeSubjectUserID() != null })
  const consumedRouteSubject = applyRouteSubjectSelection()
  if (props.initialSnapshot && !consumedRouteSubject) {
    snapshotRange.value = selectedRange.value
    return
  }
  loadDashboard()
})
</script>
