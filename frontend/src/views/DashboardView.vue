<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import UserUsageDashboard from '@/components/user/usage/UserUsageDashboard.vue'
import UsageCenterTabs from '@/components/user/usage/UsageCenterTabs.vue'
import { getTeamUsageScope } from '@/api/teamUsage'
import { useI18n } from '@/i18n'

const hasTeamUsageScope = ref(false)
const route = useRoute()
const { t } = useI18n()
const isMemberUsageRoute = computed(() => route.name === 'UsageMember')

onMounted(() => {
  if (isMemberUsageRoute.value) return
  void getTeamUsageScope()
    .then((response) => {
      hasTeamUsageScope.value = response.data.data?.is_representative === true
    })
    .catch(() => {
      hasTeamUsageScope.value = false
    })
})
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <UserUsageDashboard v-if="isMemberUsageRoute" embedded member-route />

      <template v-else>
        <div>
          <h1 class="text-2xl font-semibold text-slate-950">{{ t('usageDashboard.title') }}</h1>
          <p class="mt-1 max-w-3xl text-sm text-slate-600">{{ t('usageDashboard.subtitle') }}</p>
        </div>
        <UsageCenterTabs active="personal" :show-team="hasTeamUsageScope" show-quota-reset />
        <UserUsageDashboard embedded home-mode />
      </template>
    </div>
  </AppLayout>
</template>
