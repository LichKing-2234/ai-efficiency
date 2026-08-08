<script setup lang="ts">
import { computed, onMounted } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import { formatWorkItemCount, useWorkItemsStore } from '@/stores/workItems'
import { useAuthStore } from '@/stores/auth'
import { useI18n } from '@/i18n'

const { t } = useI18n()
const auth = useAuthStore()
const workItems = useWorkItemsStore()

onMounted(() => {
  void workItems.loadCounts()
})

const quotaResetCount = computed(() => (
  auth.isAdmin
    ? workItems.counts.quota_reset_admin_count
    : workItems.counts.quota_reset_approval_count
))
const aiAccessSetupCount = computed(() => workItems.counts.ai_access_setup_count)
const offboardingCount = computed(() => auth.isAdmin ? workItems.counts.offboarding_count : 0)
const hasVisibleWork = computed(() => aiAccessSetupCount.value > 0 || quotaResetCount.value > 0 || offboardingCount.value > 0)
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <div>
        <h1 class="text-2xl font-semibold text-slate-950">{{ t('workItems.title') }}</h1>
        <p class="mt-1 text-sm text-slate-600">{{ t('workItems.subtitle') }}</p>
      </div>

      <el-alert
        v-if="workItems.error"
        :closable="false"
        :title="t('workItems.loadFailed')"
        show-icon
        type="warning"
      />

      <div class="grid gap-3 lg:grid-cols-2">
        <RouterLink
          v-if="aiAccessSetupCount > 0"
          to="/user"
          class="flex items-center justify-between gap-4 rounded-lg border border-slate-200 bg-white p-4 shadow-sm hover:border-cyan-300 hover:bg-cyan-50/30"
        >
          <div class="min-w-0">
            <h2 class="text-sm font-semibold text-slate-950">{{ t('workItems.aiAccessSetup') }}</h2>
            <p class="mt-1 text-sm text-slate-500">{{ t('workItems.aiAccessSetupHelp') }}</p>
          </div>
          <el-tag class="shrink-0" effect="dark" round>
            {{ formatWorkItemCount(aiAccessSetupCount) }}
          </el-tag>
        </RouterLink>

        <RouterLink
          v-if="quotaResetCount > 0"
          to="/usage/quota-reset"
          class="flex items-center justify-between gap-4 rounded-lg border border-slate-200 bg-white p-4 shadow-sm hover:border-cyan-300 hover:bg-cyan-50/30"
        >
          <div class="min-w-0">
            <h2 class="text-sm font-semibold text-slate-950">{{ t('workItems.quotaResetApprovals') }}</h2>
            <p class="mt-1 text-sm text-slate-500">{{ t('workItems.quotaResetHelp') }}</p>
          </div>
          <el-tag class="shrink-0" effect="dark" round>
            {{ formatWorkItemCount(quotaResetCount) }}
          </el-tag>
        </RouterLink>

        <RouterLink
          v-if="offboardingCount > 0"
          to="/admin/directory/offboarding"
          class="flex items-center justify-between gap-4 rounded-lg border border-slate-200 bg-white p-4 shadow-sm hover:border-cyan-300 hover:bg-cyan-50/30"
        >
          <div class="min-w-0">
            <h2 class="text-sm font-semibold text-slate-950">{{ t('workItems.offboardingReview') }}</h2>
            <p class="mt-1 text-sm text-slate-500">{{ t('workItems.offboardingHelp') }}</p>
          </div>
          <el-tag class="shrink-0" effect="dark" round>
            {{ formatWorkItemCount(offboardingCount) }}
          </el-tag>
        </RouterLink>
      </div>

      <el-empty v-if="!workItems.loading && !hasVisibleWork" :description="t('workItems.empty')" />
    </div>
  </AppLayout>
</template>
