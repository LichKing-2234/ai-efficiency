<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import UserUsageDashboard from '@/components/user/usage/UserUsageDashboard.vue'
import UsageCenterTabs from '@/components/user/usage/UsageCenterTabs.vue'
import { getUserUsageDashboard } from '@/api/userUsage'
import { getTeamUsageScope } from '@/api/teamUsage'
import { useI18n } from '@/i18n'
import type { UserUsageDashboardSnapshot } from '@/types'

const { t } = useI18n()
const usageSnapshot = ref<UserUsageDashboardSnapshot | null>(null)
const loading = ref(true)
const hasTeamUsageScope = ref(false)
const route = useRoute()
const isMemberUsageRoute = computed(() => route.name === 'UsageMember')

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

  const [usageResult, scopeResult] = await Promise.allSettled([
    getUserUsageDashboard({
      start_date: formatDate(start),
      end_date: formatDate(end),
      granularity: 'day',
      timezone,
    }),
    getTeamUsageScope(),
  ])

  if (usageResult.status === 'fulfilled') {
    usageSnapshot.value = usageResult.value.data.data ?? null
  } else {
    usageSnapshot.value = null
  }

  if (scopeResult.status === 'fulfilled') {
    hasTeamUsageScope.value = scopeResult.value.data.data?.is_representative === true
  } else {
    hasTeamUsageScope.value = false
  }

  loading.value = false
})

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
          <UserUsageDashboard embedded home-mode :initial-snapshot="usageSnapshot" />
        </template>
      </template>
    </div>
  </AppLayout>
</template>
