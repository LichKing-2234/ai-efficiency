<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import UserUsageDashboard from '@/components/user/usage/UserUsageDashboard.vue'
import UsageCenterTabs from '@/components/user/usage/UsageCenterTabs.vue'
import { getTeamUsageScope } from '@/api/teamUsage'
import { getOwnerAccountScope } from '@/api/ownerAccounts'
import { useI18n } from '@/i18n'

const route = useRoute()
const { t } = useI18n()
const hasTeamUsageScope = ref(false)
const hasOwnerAccountsScope = ref(false)
const isMemberUsageRoute = computed(() => route.name === 'UsageMember')
const isUsageCenterRoute = computed(() => route.matched.some((record) => record.name === 'Usage'))
const activeTab = computed(() => {
  if (route.name === 'UsageTeam') return 'team'
  if (route.name === 'UsageQuotaReset') return 'quota-reset'
  if (route.name === 'usage-accounts') return 'accounts'
  return 'personal'
})

onMounted(() => {
  if (!isUsageCenterRoute.value) return
  void getTeamUsageScope()
    .then((response) => { hasTeamUsageScope.value = response.data.data?.is_representative === true })
    .catch(() => { hasTeamUsageScope.value = false })
  void getOwnerAccountScope()
    .then((response) => { hasOwnerAccountsScope.value = (response.data.data?.account_count ?? 0) > 0 })
    .catch(() => { hasOwnerAccountsScope.value = false })
})
</script>

<template>
  <AppLayout v-if="isMemberUsageRoute">
    <UserUsageDashboard embedded member-route />
  </AppLayout>
  <AppLayout v-else-if="isUsageCenterRoute">
    <div class="space-y-6">
      <header data-testid="usage-center-banner">
        <h1 class="text-2xl font-semibold text-slate-950">{{ t('usageDashboard.title') }}</h1>
        <p class="mt-1 max-w-3xl text-sm text-slate-600">{{ t('usageDashboard.subtitle') }}</p>
      </header>
      <UsageCenterTabs :active="activeTab" :show-team="hasTeamUsageScope" :show-accounts="hasOwnerAccountsScope" show-quota-reset />
      <UserUsageDashboard v-if="route.name === 'Usage'" embedded home-mode />
      <RouterView v-else />
    </div>
  </AppLayout>
  <UserUsageDashboard v-else embedded home-mode />
</template>
