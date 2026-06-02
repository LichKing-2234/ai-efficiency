<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import { useAuthStore } from '@/stores/auth'
import { getDashboard } from '@/api/efficiency'
import { getUserProviders } from '@/api/user'
import { listEvents } from '@/api/events'
import { useI18n } from '@/i18n'
import type { DashboardData, ToolUsageEventRow, UserProviderSummary } from '@/types'

const auth = useAuthStore()
const { t } = useI18n()
const dashboard = ref<DashboardData | null>(null)
const userProviders = ref<UserProviderSummary[]>([])
const recentEvents = ref<ToolUsageEventRow[]>([])
const loading = ref(true)
const loadFailed = ref(false)
const providersLoadFailed = ref(false)
const eventsLoadFailed = ref(false)

onMounted(async () => {
  const [dashboardResult, providersResult, eventsResult] = await Promise.allSettled([
    getDashboard(),
    getUserProviders(),
    listEvents({ limit: 3, offset: 0 }),
  ])

  if (dashboardResult.status === 'fulfilled') {
    dashboard.value = dashboardResult.value.data.data ?? null
  } else {
    loadFailed.value = true
    dashboard.value = null
  }

  if (providersResult.status === 'fulfilled') {
    userProviders.value = providersResult.value.data.data?.providers ?? []
  } else {
    providersLoadFailed.value = true
    userProviders.value = []
  }

  if (eventsResult.status === 'fulfilled') {
    recentEvents.value = eventsResult.value.data.data?.items ?? []
  } else {
    eventsLoadFailed.value = true
    recentEvents.value = []
  }

  loading.value = false
})

const displayName = computed(() => auth.user?.username || auth.user?.email || 'User')
const hasDashboardData = computed(() => !!dashboard.value && !loadFailed.value)
const connectedToolCount = computed(() => {
  if (providersLoadFailed.value) return undefined
  const platforms = new Set<string>()
  for (const provider of userProviders.value) {
    for (const group of provider.groups) {
      if (group.credential.state === 'existing_hidden') {
        platforms.add(group.platform)
      }
    }
  }
  return platforms.size
})

const connectedToolHelp = computed(() => {
  if (providersLoadFailed.value) return t('home.metricToolsHelpUnavailable')
  return connectedToolCount.value ? t('home.metricToolsHelp') : t('home.metricToolsHelpNone')
})
const hasRecentUsage = computed(() => !eventsLoadFailed.value && recentEvents.value.length > 0)
const codeReportingActive = computed(() => (dashboard.value?.total_repos ?? 0) > 0 || (dashboard.value?.tracked_workflows ?? 0) > 0)
const setupStatuses = computed(() => [
  {
    label: t('home.statusAccount'),
    value: t('home.statusAccountReady'),
    tone: 'ready',
    action: '',
    to: '',
  },
  {
    label: t('home.statusAiAccess'),
    value: providersLoadFailed.value
      ? t('home.statusUnknown')
      : connectedToolCount.value
        ? t('home.statusAiAccessReady')
        : t('home.statusAiAccessMissing'),
    tone: providersLoadFailed.value ? 'warn' : connectedToolCount.value ? 'ready' : 'warn',
    action: connectedToolCount.value ? '' : t('home.openSetup'),
    to: '/user',
  },
  {
    label: t('home.statusReporting'),
    value: codeReportingActive.value ? t('home.statusReportingActive') : t('home.statusReportingWaiting'),
    tone: codeReportingActive.value ? 'ready' : 'warn',
    action: codeReportingActive.value ? '' : t('home.openSetup'),
    to: '/user',
  },
  {
    label: t('home.statusRecentUsage'),
    value: eventsLoadFailed.value ? t('home.statusUnknown') : hasRecentUsage.value ? t('home.statusRecentUsageSeen') : t('home.statusRecentUsageMissing'),
    tone: eventsLoadFailed.value || !hasRecentUsage.value ? 'warn' : 'ready',
    action: hasRecentUsage.value ? t('home.viewRecords') : t('home.openSetup'),
    to: hasRecentUsage.value ? '/events' : '/user',
  },
])

const metricCards = computed(() => [
  {
    label: t('home.metricRepos'),
    value: dashboard.value?.total_repos,
    helper: t('home.metricReposHelp'),
  },
  {
    label: t('home.metricWorkflows'),
    value: dashboard.value?.tracked_workflows,
    helper: t('home.metricWorkflowsHelp'),
  },
  {
    label: t('home.metricAiPrs'),
    value: dashboard.value?.total_ai_prs,
    helper: t('home.metricAiPrsHelp'),
  },
  {
    label: t('home.metricTools'),
    value: connectedToolCount.value,
    helper: connectedToolHelp.value,
  },
])

function formatMetric(value?: number | null) {
  if (value == null || Number.isNaN(value)) return '—'
  return value.toLocaleString()
}

function formatDate(value?: string | null) {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

function formatTokens(row: ToolUsageEventRow) {
  const total = (row.input_tokens ?? 0) + (row.output_tokens ?? 0) + (row.cached_input_tokens ?? 0)
  return total > 0 ? formatMetric(total) : '—'
}
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <section class="rounded-lg border border-cyan-100 bg-white p-5 shadow-sm sm:p-6">
        <div class="flex flex-col gap-5 lg:flex-row lg:items-start lg:justify-between">
          <div>
            <p class="text-sm font-semibold text-cyan-700">{{ t('home.personalStatus') }}</p>
            <h1 class="mt-2 text-2xl font-bold tracking-normal text-slate-950 sm:text-3xl">{{ t('home.title') }}</h1>
            <p class="mt-2 max-w-2xl text-sm text-slate-600">{{ t('home.subtitle') }}</p>
          </div>
          <div class="rounded-lg border border-slate-200 bg-slate-50 px-4 py-3 text-sm text-slate-700">
            <div class="text-xs font-medium uppercase tracking-wide text-slate-500">{{ t('home.statusAccount') }}</div>
            <div class="mt-1 font-semibold text-slate-950">{{ displayName }}</div>
            <div class="mt-1 text-xs text-slate-500">{{ auth.user?.role ?? 'user' }} · {{ auth.user?.auth_source ?? 'unknown' }}</div>
          </div>
        </div>
      </section>

      <div v-if="loading" class="rounded-lg border border-slate-200 bg-white p-6 text-sm text-slate-500 shadow-sm">
        {{ t('home.loading') }}
      </div>

      <template v-else>
        <section class="space-y-4">
          <div class="flex items-center justify-between">
            <h2 class="text-base font-semibold text-slate-950">{{ t('home.thisWeek') }}</h2>
            <span class="text-xs text-slate-500">{{ t('home.signalsScope') }}</span>
          </div>
          <div class="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
            <div
              v-for="card in metricCards"
              :key="card.label"
              class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm"
            >
              <p class="text-sm font-medium text-slate-500">{{ card.label }}</p>
              <p class="mt-3 text-3xl font-semibold text-slate-950">{{ formatMetric(card.value) }}</p>
              <p class="mt-2 text-xs text-slate-500">{{ card.helper }}</p>
            </div>
          </div>
        </section>

        <section class="grid gap-4 lg:grid-cols-[minmax(0,0.85fr)_minmax(0,1.15fr)]">
          <div class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
            <h2 class="text-base font-semibold text-slate-950">{{ t('home.setupStatus') }}</h2>
            <div class="mt-4 space-y-3 text-sm">
              <div
                v-for="item in setupStatuses"
                :key="item.label"
                class="flex flex-col gap-2 rounded-md bg-slate-50 px-3 py-3 sm:flex-row sm:items-center sm:justify-between"
              >
                <span class="text-slate-600">{{ item.label }}</span>
                <div class="flex items-center gap-3">
                  <span class="font-medium" :class="item.tone === 'ready' ? 'text-emerald-700' : 'text-amber-700'">
                    {{ item.value }}
                  </span>
                  <RouterLink v-if="item.action" :to="item.to" class="font-medium text-cyan-700 hover:text-cyan-900">
                    {{ item.action }}
                  </RouterLink>
                </div>
              </div>
            </div>
          </div>

          <div class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
            <h2 class="text-base font-semibold text-slate-950">{{ t('home.nextSteps') }}</h2>
            <div class="mt-4 grid gap-3 sm:grid-cols-3">
              <RouterLink to="/user" class="rounded-lg border border-cyan-200 bg-cyan-50 p-4 text-sm hover:border-cyan-400">
                <div class="font-semibold text-cyan-950">{{ t('home.nextSetupTitle') }}</div>
                <p class="mt-2 text-xs text-cyan-800">{{ t('home.nextSetupText') }}</p>
              </RouterLink>
              <RouterLink to="/repos" class="rounded-lg border border-slate-200 p-4 text-sm hover:border-slate-400">
                <div class="font-semibold text-slate-950">{{ t('home.nextRepoTitle') }}</div>
                <p class="mt-2 text-xs text-slate-600">{{ t('home.nextRepoText') }}</p>
              </RouterLink>
              <RouterLink to="/events" class="rounded-lg border border-slate-200 p-4 text-sm hover:border-slate-400">
                <div class="font-semibold text-slate-950">{{ t('home.nextRecordsTitle') }}</div>
                <p class="mt-2 text-xs text-slate-600">{{ t('home.nextRecordsText') }}</p>
              </RouterLink>
            </div>
            <div v-if="!hasDashboardData" class="mt-4 rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-900">
              <div class="font-medium">{{ t('home.noData') }}</div>
              <p class="mt-1 text-xs">{{ t('home.noDataHelp') }}</p>
              <RouterLink to="/user" class="mt-3 inline-flex rounded-md bg-amber-900 px-3 py-2 text-xs font-medium text-white hover:bg-amber-950">
                {{ t('home.openSetup') }}
              </RouterLink>
            </div>
          </div>
        </section>

        <section class="rounded-lg border border-slate-200 bg-white p-5 shadow-sm">
          <div class="flex items-center justify-between gap-4">
            <h2 class="text-base font-semibold text-slate-950">{{ t('home.recentActivity') }}</h2>
            <RouterLink to="/events" class="text-sm font-medium text-cyan-700 hover:text-cyan-900">{{ t('home.viewRecords') }}</RouterLink>
          </div>
          <p class="mt-4 text-sm text-slate-500">
            {{ hasRecentUsage ? t('home.recentLoaded') : t('home.noDataHelp') }}
          </p>
          <div v-if="hasRecentUsage" class="mt-4 space-y-3">
            <RouterLink
              v-for="event in recentEvents"
              :key="event.id"
              to="/events"
              class="block rounded-lg border border-slate-200 p-4 text-sm hover:border-cyan-300"
            >
              <div class="flex items-start justify-between gap-3">
                <div class="min-w-0">
                  <div class="truncate font-semibold text-slate-950">{{ event.tool }}</div>
                  <div class="mt-1 truncate text-xs text-slate-500">{{ event.repo_name || t('home.unknownRepository') }}</div>
                </div>
                <span class="shrink-0 text-xs text-slate-500">{{ formatDate(event.observed_end_at) }}</span>
              </div>
              <div class="mt-3 grid grid-cols-2 gap-3 text-xs text-slate-600">
                <div>
                  <div class="text-slate-400">{{ t('home.eventTokens') }}</div>
                  <div class="mt-1 font-medium text-slate-900">{{ formatTokens(event) }}</div>
                </div>
                <div>
                  <div class="text-slate-400">{{ t('home.eventRequests') }}</div>
                  <div class="mt-1 font-medium text-slate-900">{{ formatMetric(event.request_count) }}</div>
                </div>
              </div>
            </RouterLink>
          </div>
        </section>
      </template>
    </div>
  </AppLayout>
</template>
