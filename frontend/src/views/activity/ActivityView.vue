<script setup lang="ts">
import { computed } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import ActivityAnalytics from '@/components/activity/ActivityAnalytics.vue'
import ReportingReadinessGuide from '@/components/activity/ReportingReadinessGuide.vue'
import { useI18n } from '@/i18n'
import { activityV2Text } from '@/components/activity/activityV2Text'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const auth = useAuthStore()
const { locale, t } = useI18n()
const subjectUserId = computed(() => {
  const value = Number(route.params.user_id)
  return Number.isInteger(value) && value > 0 ? value : undefined
})
const showPersonalReadiness = computed(() => !subjectUserId.value && auth.user?.reporting_capabilities?.readiness_available === true)
</script>

<template>
  <AppLayout>
    <div class="min-w-0 space-y-5">
      <header>
        <p class="text-xs font-semibold uppercase tracking-[0.18em] text-cyan-700">{{ t('activity.eyebrow') }}</p>
        <h1 class="mt-1 text-2xl font-bold text-slate-950">{{ subjectUserId ? activityV2Text(locale, 'memberTitle') : t('activity.title') }}</h1>
        <p class="mt-1 max-w-3xl text-sm text-slate-600">{{ activityV2Text(locale, 'v2Subtitle') }}</p>
      </header>
      <ReportingReadinessGuide v-if="showPersonalReadiness" />
      <ActivityAnalytics :scope="subjectUserId ? 'member' : 'personal'" :subject-user-id="subjectUserId" />
    </div>
  </AppLayout>
</template>
