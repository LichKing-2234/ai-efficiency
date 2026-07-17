<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import UserUsageDashboard from '@/components/user/usage/UserUsageDashboard.vue'
import UsageCenterTabs from '@/components/user/usage/UsageCenterTabs.vue'
import { getTeamUsageScope } from '@/api/teamUsage'

const hasTeamUsageScope = ref(false)
const route = useRoute()
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
        <UsageCenterTabs active="personal" :show-team="hasTeamUsageScope" show-quota-reset />
        <UserUsageDashboard embedded home-mode />
      </template>
    </div>
  </AppLayout>
</template>
