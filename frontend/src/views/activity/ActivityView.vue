<script setup lang="ts">
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import ActivityDateRange from '@/components/activity/ActivityDateRange.vue'
import ActivityHero from '@/components/activity/ActivityHero.vue'
import ActivityDetails from '@/components/activity/ActivityDetails.vue'
import ReportingReadinessGuide from '@/components/activity/ReportingReadinessGuide.vue'
import { useMemberActivity } from '@/composables/useMemberActivity'
import { useI18n } from '@/i18n'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const {
  data, loading, prLoading, error, range, prPageIndex,
  loadActivity, loadNextPRPage, loadPreviousPRPage, selectRange,
} = useMemberActivity(route, router, t)
</script>

<template>
  <AppLayout>
    <div class="min-w-0 space-y-6">
      <header class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div class="min-w-0">
          <p class="text-xs font-semibold uppercase tracking-[0.18em] text-cyan-700">{{ t('activity.eyebrow') }}</p>
          <h1 class="mt-1 text-2xl font-bold text-slate-950">{{ data?.member.display_name || t('activity.title') }}</h1>
          <p class="mt-1 max-w-3xl text-sm text-slate-600">{{ t('activity.subtitle') }}</p>
        </div>
        <ActivityDateRange :from="range.from" :to="range.to" :loading="loading" @change="selectRange" @refresh="loadActivity" />
      </header>

      <ReportingReadinessGuide v-if="!route.params.user_id" />

      <ElAlert v-if="error" type="error" :closable="false" show-icon :title="error" />
      <div v-if="loading && !data" aria-live="polite" class="rounded-xl border border-slate-200 bg-white px-5 py-12 text-center text-sm text-slate-500">
        {{ t('activity.loading') }}
      </div>

      <template v-if="data">
        <ActivityHero :metrics="data.metrics" />
        <ElAlert
          v-if="!data.sync_coverage.complete"
          type="warning"
          :closable="false"
          show-icon
          :title="t('activity.syncNeeded', { count: data.sync_coverage.affected_repositories })"
        />
        <ActivityDetails
          :activity="data"
          :pr-loading="loading || prLoading"
          :has-previous-pr-page="prPageIndex > 0"
          @next-pr="loadNextPRPage"
          @previous-pr="loadPreviousPRPage"
        />
      </template>
    </div>
  </AppLayout>
</template>
