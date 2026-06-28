<template>
  <div :class="props.embedded ? 'space-y-6' : 'mx-auto max-w-7xl px-4 py-6 sm:px-6 lg:px-8'">
    <div class="mb-6 flex flex-col gap-4 md:flex-row md:items-center md:justify-between">
      <div>
        <component
          :is="props.embedded ? 'h2' : 'h1'"
          :class="props.embedded ? 'text-base font-semibold text-slate-950' : 'text-2xl font-semibold text-gray-900'"
        >
          {{ props.embedded ? t('usageDashboard.embeddedTitle') : t('usageDashboard.title') }}
        </component>
        <p class="mt-1 text-sm text-gray-500">{{ t('usageDashboard.subtitle') }}</p>
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
      <div v-if="hasMemberSubjects" class="flex justify-end">
        <UserUsageSubjectSelector
          v-model="selectedSubjectValue"
          :subjects="subjects"
          @select="selectSubject"
        />
      </div>
      <UsageGroupQuotaSection
        :quotas="currentSnapshot?.group_quotas ?? null"
        :loading="loading && !!currentSnapshot"
        :range-label="selectedRangeLabel"
      />
      <SelectedSubjectSubscriptionRows
        v-if="selectedMemberSubject && selectedSubjectSubscriptions.length > 0"
        :subject-user-id="selectedMemberSubject.user_id"
        :rows="selectedSubjectSubscriptions"
        :update-multiplier="handleMultiplierConfirm"
      />
      <TeamUsageAuditList
        v-if="selectedMemberSubject && auditItems.length > 0"
        :items="auditItems"
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
  getTeamUsageAudit,
  getTeamUsageSubjectDashboard,
  listTeamUsageSubjects,
  updateTeamUsageRateMultiplier,
} from '@/api/teamUsage'
import { useI18n } from '@/i18n'
import type {
  SubjectSubscriptionGroup,
  TeamUsageAuditRecord,
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
import TeamUsageAuditList from '@/components/user/usage/TeamUsageAuditList.vue'

type RangeOption = 'today' | '7d' | '30d'

const props = withDefaults(defineProps<{
  embedded?: boolean
  homeMode?: boolean
  initialSnapshot?: UserUsageDashboardSnapshot | null
}>(), {
  embedded: false,
  homeMode: false,
  initialSnapshot: null,
})

const selectedRange = ref<RangeOption>('30d')
const snapshotRange = ref<RangeOption>('30d')
const snapshot = ref<UserUsageDashboardSnapshot | null>(null)
const memberSubjects = ref<TeamUsageSubject[]>([])
const auth = useAuthStore()
const selectedSubjectValue = ref(subjectValue(makeSelfSubject()))
const selectedSubjectSubscriptions = ref<SubjectSubscriptionGroup[]>([])
const auditItems = ref<TeamUsageAuditRecord[]>([])
const loading = ref(false)
const errorMessage = ref('')
const credentialError = ref(false)
const { t } = useI18n()
const route = useRoute()
let dashboardRequestSeq = 0

const currentSnapshot = computed(() => snapshot.value ?? props.initialSnapshot)
const setupRequired = computed(() => currentSnapshot.value?.configured === false)
const selectedRangeLabel = computed(() => rangeLabel(selectedRange.value))
const snapshotRangeLabel = computed(() => rangeLabel(snapshotRange.value))
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
  return `${subject.subject_type}:${subject.user_id}`
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

function subjectUserIDQuery() {
  const raw = Array.isArray(route.query.subject_user_id) ? route.query.subject_user_id[0] : route.query.subject_user_id
  const subjectUserID = Number(raw)
  if (!Number.isInteger(subjectUserID)) return null
  return subjectUserID
}

async function loadSubjects(options?: { expandForSubjectQuery?: boolean }) {
  try {
    const params = options?.expandForSubjectQuery ? { page_size: 500 } : undefined
    const res = await listTeamUsageSubjects(params)
    memberSubjects.value = (res.data.data?.subjects ?? []).filter((subject) => subject.subject_type === 'member')
    if (!subjects.value.some((subject) => subjectValue(subject) === selectedSubjectValue.value)) {
      selectedSubjectValue.value = subjectValue(makeSelfSubject())
    }
  } catch {
    memberSubjects.value = []
    selectedSubjectValue.value = subjectValue(makeSelfSubject())
  }
}

function applySubjectQuerySelection() {
  const subjectUserID = subjectUserIDQuery()
  if (subjectUserID == null) return false
  const subject = subjects.value.find((item) => item.subject_type === 'member' && item.user_id === subjectUserID && item.selectable)
  if (!subject) return false
  const next = subjectValue(subject)
  if (selectedSubjectValue.value === next) return false
  selectedSubjectValue.value = next
  return true
}

async function loadAuditForSubject(targetUserID: number, requestSeq: number) {
  try {
    const res = await getTeamUsageAudit({ target_user_id: targetUserID, page_size: 20 })
    if (requestSeq !== dashboardRequestSeq || selectedMemberSubject.value?.user_id !== targetUserID) return
    auditItems.value = res.data.data?.items ?? []
  } catch {
    if (requestSeq !== dashboardRequestSeq || selectedMemberSubject.value?.user_id !== targetUserID) return
    auditItems.value = []
  }
}

async function loadDashboard() {
  const requestedRange = selectedRange.value
  const requestSeq = ++dashboardRequestSeq
  loading.value = true
  errorMessage.value = ''
  credentialError.value = false
  selectedSubjectSubscriptions.value = []
  auditItems.value = []
  try {
    const subject = selectedSubject.value
    const params = buildParams(requestedRange)
    const res = subject?.subject_type === 'member'
      ? await getTeamUsageSubjectDashboard(subject.user_id, params)
      : await getUserUsageDashboard(params)
    if (requestSeq !== dashboardRequestSeq) return
    snapshot.value = res.data.data ?? null
    selectedSubjectSubscriptions.value = subject?.subject_type === 'member'
      ? (res.data.data as any)?.subject_subscription_groups ?? []
      : []
    snapshotRange.value = requestedRange
    if (subject?.subject_type === 'member') {
      auditItems.value = []
      await loadAuditForSubject(subject.user_id, requestSeq)
    } else {
      auditItems.value = []
    }
  } catch (err: any) {
    if (requestSeq !== dashboardRequestSeq) return
    snapshot.value = null
    selectedSubjectSubscriptions.value = []
    auditItems.value = []
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
  await loadSubjects({ expandForSubjectQuery: subjectUserIDQuery() != null })
  const consumedSubjectQuery = applySubjectQuerySelection()
  if (props.initialSnapshot && !consumedSubjectQuery) {
    snapshotRange.value = selectedRange.value
    return
  }
  loadDashboard()
})
</script>
