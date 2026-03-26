<script setup lang="ts">
import { onMounted, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import { useAuthStore } from '@/stores/auth'
import { getDashboard } from '@/api/efficiency'
import type { DashboardData } from '@/types'

const auth = useAuthStore()
const dashboard = ref<DashboardData | null>(null)
const loading = ref(true)

onMounted(async () => {
  try {
    const res = await getDashboard()
    dashboard.value = res.data.data ?? null
  } catch {
    // fallback to empty
  } finally {
    loading.value = false
  }
})
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <h1 class="text-2xl font-bold text-gray-900">
        Welcome back{{ auth.user?.username ? `, ${auth.user.username}` : '' }}
      </h1>

      <div class="grid grid-cols-1 gap-6 sm:grid-cols-2 lg:grid-cols-4">
        <div class="rounded-lg bg-white p-6 shadow">
          <p class="text-sm font-medium text-gray-500">Total Repos</p>
          <p class="mt-2 text-3xl font-semibold text-gray-900">{{ dashboard?.total_repos ?? '--' }}</p>
        </div>
        <div class="rounded-lg bg-white p-6 shadow">
          <p class="text-sm font-medium text-gray-500">Active Sessions</p>
          <p class="mt-2 text-3xl font-semibold text-gray-900">{{ dashboard?.active_sessions ?? '--' }}</p>
        </div>
        <div class="rounded-lg bg-white p-6 shadow">
          <p class="text-sm font-medium text-gray-500">Avg AI Score</p>
          <p class="mt-2 text-3xl font-semibold text-indigo-600">{{ dashboard?.avg_ai_score ?? '--' }}</p>
        </div>
        <div class="rounded-lg bg-white p-6 shadow">
          <p class="text-sm font-medium text-gray-500">AI PRs</p>
          <p class="mt-2 text-3xl font-semibold text-gray-900">{{ dashboard?.total_ai_prs ?? '--' }}</p>
        </div>
      </div>

      <div class="rounded-lg bg-white p-6 shadow">
        <h2 class="text-lg font-semibold text-gray-900">Recent Activity</h2>
        <p v-if="loading" class="mt-4 text-sm text-gray-500">Loading...</p>
        <p v-else class="mt-4 text-sm text-gray-500">Dashboard data loaded from API. Navigate to Repos for detailed analysis.</p>
      </div>
    </div>
  </AppLayout>
</template>
