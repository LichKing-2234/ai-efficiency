<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import UserUsageDashboard from '@/components/user/usage/UserUsageDashboard.vue'

const route = useRoute()
const isMemberUsageRoute = computed(() => route.name === 'UsageMember')
</script>

<template>
  <AppLayout v-if="isMemberUsageRoute">
    <UserUsageDashboard embedded member-route />
  </AppLayout>
  <UserUsageDashboard v-else embedded home-mode />
</template>
