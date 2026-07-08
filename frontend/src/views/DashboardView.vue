<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import UserUsageDashboard from '@/components/user/usage/UserUsageDashboard.vue'
import UsageCenterTabs from '@/components/user/usage/UsageCenterTabs.vue'
import { getUserProviders } from '@/api/user'
import { getUserUsageDashboard } from '@/api/userUsage'
import { getTeamUsageScope } from '@/api/teamUsage'
import { useI18n } from '@/i18n'
import type { UserProviderSummary, UserUsageDashboardSnapshot } from '@/types'

const { t } = useI18n()
const userProviders = ref<UserProviderSummary[]>([])
const usageSnapshot = ref<UserUsageDashboardSnapshot | null>(null)
const loading = ref(true)
const providersLoadFailed = ref(false)
const usageLoadFailed = ref(false)
const hasTeamUsageScope = ref(false)
const route = useRoute()
const isMemberUsageRoute = computed(() => route.name === 'UsageMember')

type HomeLifecycleState =
  | 'needs_setup'
  | 'setup_ready_waiting_for_first_usage'
  | 'established_user'
  | 'degraded_error'

onMounted(async () => {
  if (isMemberUsageRoute.value) {
    loading.value = false
    return
  }

  const timezone = Intl.DateTimeFormat().resolvedOptions().timeZone
  const end = new Date()
  const start = new Date(end)
  start.setDate(end.getDate() - 29)
  const formatDate = (date: Date) => date.toISOString().slice(0, 10)

  const [providersResult, usageResult, scopeResult] = await Promise.allSettled([
    getUserProviders(),
    getUserUsageDashboard({
      start_date: formatDate(start),
      end_date: formatDate(end),
      granularity: 'day',
      timezone,
    }),
    getTeamUsageScope(),
  ])

  if (providersResult.status === 'fulfilled') {
    userProviders.value = providersResult.value.data.data?.providers ?? []
  } else {
    providersLoadFailed.value = true
    userProviders.value = []
  }

  if (usageResult.status === 'fulfilled') {
    usageSnapshot.value = usageResult.value.data.data ?? null
  } else {
    usageLoadFailed.value = true
    usageSnapshot.value = null
  }

  if (scopeResult.status === 'fulfilled') {
    hasTeamUsageScope.value = scopeResult.value.data.data?.is_representative === true
  } else {
    hasTeamUsageScope.value = false
  }

  loading.value = false
})

const aiAccessReady = computed(() => {
  if (providersLoadFailed.value) return false
  return userProviders.value.some((provider) =>
    provider.groups.some((group) => group.credential.state === 'existing_hidden'),
  )
})
const usageDataReady = computed(() => {
  const snapshot = usageSnapshot.value
  if (!snapshot || snapshot.configured !== true) return false
  if ((snapshot.stats?.total_requests ?? 0) > 0) return true
  if ((snapshot.stats?.total_tokens ?? 0) > 0) return true
  if (snapshot.trend.some((point) => point.requests > 0 || point.total_tokens > 0)) return true
  return snapshot.models.some((model) => model.requests > 0 || model.total_tokens > 0)
})
const homeLifecycleState = computed<HomeLifecycleState>(() => {
  if (providersLoadFailed.value || usageLoadFailed.value) return 'degraded_error'
  if (!aiAccessReady.value) return 'needs_setup'
  if (!usageDataReady.value) return 'setup_ready_waiting_for_first_usage'
  return 'established_user'
})
const guideExpanded = ref(false)
const defaultGuideExpanded = computed(() => homeLifecycleState.value !== 'established_user')
const guideTitle = computed(() => {
  if (homeLifecycleState.value === 'needs_setup') return t('home.guideNeedsSetupTitle')
  if (homeLifecycleState.value === 'setup_ready_waiting_for_first_usage') return t('home.guideWaitingUsageTitle')
  if (homeLifecycleState.value === 'degraded_error') return t('home.guideErrorTitle')
  return t('home.guideReadyTitle')
})
const guideHelp = computed(() => {
  if (homeLifecycleState.value === 'needs_setup') return t('home.guideNeedsSetupHelp')
  if (homeLifecycleState.value === 'setup_ready_waiting_for_first_usage') return t('home.guideWaitingUsageHelp')
  if (homeLifecycleState.value === 'degraded_error') return t('home.guideErrorHelp')
  return t('home.guideReadyHelp')
})
const guidePrimaryAction = computed(() => {
  if (homeLifecycleState.value === 'needs_setup') return t('home.goSetup')
  if (homeLifecycleState.value === 'setup_ready_waiting_for_first_usage') return t('home.goSetup')
  if (homeLifecycleState.value === 'degraded_error') return t('home.goSetup')
  return t('home.viewSetupGuidance')
})
const shouldShowGuideSignals = computed(() => defaultGuideExpanded.value || guideExpanded.value)

function toggleGuideExpanded() {
  if (homeLifecycleState.value !== 'established_user') return
  guideExpanded.value = !guideExpanded.value
}

</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <UserUsageDashboard v-if="isMemberUsageRoute" embedded member-route />

      <template v-else>
        <UsageCenterTabs active="personal" :show-team="hasTeamUsageScope" show-quota-reset />

        <div v-if="loading" class="rounded-lg border border-slate-200 bg-white p-6 text-sm text-slate-500 shadow-sm">
          {{ t('home.loading') }}
        </div>

        <template v-else>
          <section class="space-y-4">
            <div
              class="rounded-xl border border-slate-200 bg-white p-5 shadow-sm"
              :data-testid="`home-guide-${homeLifecycleState}`"
            >
              <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
                <div>
                  <p class="text-sm font-semibold text-cyan-700">{{ t('home.title') }}</p>
                  <h1 class="mt-2 text-2xl font-semibold text-slate-950">{{ guideTitle }}</h1>
                  <p class="mt-2 max-w-3xl text-sm text-slate-600">{{ guideHelp }}</p>
                </div>
                <RouterLink
                  v-if="homeLifecycleState !== 'established_user'"
                  to="/user"
                  class="inline-flex rounded-md bg-cyan-700 px-4 py-2 text-sm font-medium text-white hover:bg-cyan-800"
                >
                  {{ guidePrimaryAction }}
                </RouterLink>
                <button
                  v-else
                  type="button"
                  class="inline-flex rounded-md border border-slate-300 px-4 py-2 text-sm font-medium text-slate-700 hover:bg-slate-50"
                  @click="toggleGuideExpanded"
                >
                  {{ guidePrimaryAction }}
                </button>
              </div>

              <div v-if="shouldShowGuideSignals" class="mt-4 grid gap-3 sm:grid-cols-2">
                <div class="rounded-lg bg-slate-50 p-4 text-sm">
                  <div class="font-medium text-slate-950">{{ t('home.guideSignalAiAccess') }}</div>
                  <div class="mt-2 text-slate-600">{{ aiAccessReady ? t('home.guideDone') : t('home.guideTodo') }}</div>
                </div>
                <div class="rounded-lg bg-slate-50 p-4 text-sm">
                  <div class="font-medium text-slate-950">{{ t('home.guideSignalUsage') }}</div>
                  <div class="mt-2 text-slate-600">
                    {{ usageLoadFailed ? t('home.statusUnknown') : usageDataReady ? t('home.guideDone') : t('home.guideWaiting') }}
                  </div>
                </div>
              </div>
            </div>
          </section>

          <UserUsageDashboard embedded home-mode :initial-snapshot="usageSnapshot" />
        </template>
      </template>
    </div>
  </AppLayout>
</template>
