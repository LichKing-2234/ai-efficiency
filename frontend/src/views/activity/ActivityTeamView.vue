<script setup lang="ts">
import { RouterLink, useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import ActivityAnalytics from '@/components/activity/ActivityAnalytics.vue'
import CursorPager from '@/components/CursorPager.vue'
import { useActivityTeam } from '@/composables/useActivityTeam'
import { useI18n } from '@/i18n'
import { activityV2Text } from '@/components/activity/activityV2Text'

const { locale, t } = useI18n()
const route = useRoute()
const {
  team,
  loading,
  memberLoading,
  error,
  memberPageIndex,
  load,
  loadNextMembers,
  loadPreviousMembers,
} = useActivityTeam()

function memberKey(userID: number, directoryID?: string) {
  return userID > 0 ? String(userID) : directoryID || 'unmatched'
}
</script>

<template>
  <AppLayout>
    <div class="min-w-0 space-y-6">
      <header>
        <div class="min-w-0">
          <p class="text-xs font-semibold uppercase tracking-[0.18em] text-cyan-700">{{ t('activity.eyebrow') }}</p>
          <h1 class="mt-1 text-2xl font-bold text-slate-950">{{ team?.team.name || t('activity.teamTitle') }}</h1>
          <p v-if="team" class="mt-1 truncate text-sm text-slate-600">{{ team.team.display_path }}</p>
        </div>
      </header>

      <div v-if="loading && !team" role="status" class="border-y border-slate-200 bg-white px-5 py-12 text-center text-sm text-slate-500">
        {{ t('activity.loadingTeam') }}
      </div>
      <ElAlert v-else-if="error" type="error" :closable="false">
        <template #title>
          <span>{{ t('activity.teamLoadFailed') }}</span>
          <ElButton class="!ml-2" type="primary" link @click="load">{{ t('activity.retry') }}</ElButton>
        </template>
      </ElAlert>

      <template v-if="team">
        <ActivityAnalytics scope="team" :team-id="team.team.external_id" />

        <section class="min-w-0 border-y border-slate-200 bg-white" :aria-label="t('activity.members')">
          <div class="border-b border-slate-200 px-5 py-4"><h2 class="font-semibold text-slate-950">{{ t('activity.members') }}</h2></div>
          <div v-if="team.members.items.length === 0" class="px-5 py-10 text-sm text-slate-500">{{ t('activity.noMembers') }}</div>
          <div v-else class="divide-y divide-slate-100">
            <div
              data-testid="activity-team-member-column-labels"
              class="hidden grid-cols-[minmax(10rem,1fr)_10rem_8rem] gap-3 bg-slate-50 px-5 py-2 text-xs font-medium text-slate-500 lg:grid"
            >
              <span>{{ t('activity.members') }}</span>
              <span>{{ activityV2Text(locale, 'department') }}</span>
              <span>{{ activityV2Text(locale, 'availability') }}</span>
            </div>
            <component
              :is="row.member.user_id > 0 ? RouterLink : 'div'"
              v-for="row in team.members.items"
              :key="memberKey(row.member.user_id, row.member.directory_member_external_id)"
              :data-testid="`activity-member-${memberKey(row.member.user_id, row.member.directory_member_external_id)}`"
              :to="row.member.user_id > 0 ? { path: `/activity/members/${row.member.user_id}`, query: route.query } : undefined"
              class="grid min-w-0 gap-3 px-5 py-4 lg:grid-cols-[minmax(10rem,1fr)_10rem_8rem] lg:items-center"
            >
              <div class="min-w-0"><p class="truncate font-medium text-slate-950">{{ row.member.display_name }}</p><p class="truncate text-sm text-slate-500">{{ row.member.email }}</p></div>
              <div class="text-sm text-slate-600"><span class="lg:hidden">{{ activityV2Text(locale, 'department') }}: </span>{{ row.member.department_external_ids.join(', ') || '—' }}</div>
              <div class="text-sm font-medium" :class="row.available ? 'text-emerald-700' : 'text-amber-700'"><span class="lg:hidden">{{ activityV2Text(locale, 'availability') }}: </span>{{ row.available ? activityV2Text(locale, 'activityAvailable') : t('activity.noActivityData') }}</div>
            </component>
          </div>
          <CursorPager
            :has-previous="memberPageIndex > 0"
            :has-next="Boolean(team.members.next_cursor)"
            :loading="loading || memberLoading"
            :loading-label="t('settings.loading')"
            :previous-label="t('activity.previousPage')"
            :next-label="t('activity.nextPage')"
            test-i-d-prefix="activity-team-members"
            @previous="loadPreviousMembers"
            @next="loadNextMembers"
          />
        </section>
      </template>
    </div>
  </AppLayout>
</template>
