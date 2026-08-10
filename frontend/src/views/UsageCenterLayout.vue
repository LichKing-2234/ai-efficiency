<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import UsageCenterTabs from '@/components/user/usage/UsageCenterTabs.vue'
import { getTeamUsageScope } from '@/api/teamUsage'
import { useI18n } from '@/i18n'

const route = useRoute()
const { t } = useI18n()
const hasTeamUsageScope = ref(false)
const activeTab = computed(() => route.name === 'UsageTeam' ? 'team' : route.name === 'UsageQuotaReset' ? 'quota-reset' : 'personal')

onMounted(() => {
  void getTeamUsageScope()
    .then((response) => { hasTeamUsageScope.value = response.data.data?.is_representative === true })
    .catch(() => { hasTeamUsageScope.value = false })
})
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <header data-testid="usage-center-banner">
        <h1 class="text-2xl font-semibold text-slate-950">{{ t('usageDashboard.title') }}</h1>
        <p class="mt-1 max-w-3xl text-sm text-slate-600">{{ t('usageDashboard.subtitle') }}</p>
      </header>
      <UsageCenterTabs :active="activeTab" :show-team="hasTeamUsageScope" show-quota-reset />
      <RouterView />
    </div>
  </AppLayout>
</template>
