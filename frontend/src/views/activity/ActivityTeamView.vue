<script setup lang="ts">
import { RouterLink } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import ActivityDateRange from '@/components/activity/ActivityDateRange.vue'
import CursorPager from '@/components/activity/CursorPager.vue'
import { useActivityTeam } from '@/composables/useActivityTeam'
import { useI18n } from '@/i18n'
import type { ActivityCountMetric } from '@/types/activity'

const { t } = useI18n()
const {
  team,
  loading,
  memberLoading,
  error,
  range,
  memberPageIndex,
  load,
  selectRange,
  loadNextMembers,
  loadPreviousMembers,
} = useActivityTeam()

function metric(value: ActivityCountMetric) {
  return `${value.lower_bound ? '≥' : ''}${value.value}`
}

function memberKey(userID: number, directoryID?: string) {
  return userID > 0 ? String(userID) : directoryID || 'unmatched'
}
</script>

<template>
  <AppLayout>
    <div class="min-w-0 space-y-6">
      <header class="flex flex-col gap-4 lg:flex-row lg:items-end lg:justify-between">
        <div class="min-w-0">
          <p class="text-xs font-semibold uppercase tracking-[0.18em] text-cyan-700">{{ t('activity.eyebrow') }}</p>
          <h1 class="mt-1 text-2xl font-bold text-slate-950">{{ team?.team.name || t('activity.teamTitle') }}</h1>
          <p v-if="team" class="mt-1 truncate text-sm text-slate-600">{{ team.team.display_path }}</p>
        </div>
        <ActivityDateRange :from="range.from" :to="range.to" :loading="loading" @change="selectRange" @refresh="load" />
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
        <section data-testid="activity-team-summary" class="grid gap-3 sm:grid-cols-2 xl:grid-cols-4">
          <article class="border border-slate-200 bg-white p-5 shadow-sm"><p class="text-xs font-semibold uppercase text-slate-500">{{ t('activity.activeMembers') }}</p><p class="mt-2 text-3xl font-semibold text-slate-950">{{ team.active_members }}</p></article>
          <article class="border border-slate-200 bg-white p-5 shadow-sm"><p class="text-xs font-semibold uppercase text-slate-500">{{ t('activity.participatingPRs') }}</p><p class="mt-2 text-3xl font-semibold text-slate-950">{{ metric(team.metrics.participating_prs) }}</p></article>
          <article class="border border-slate-200 bg-white p-5 shadow-sm"><p class="text-xs font-semibold uppercase text-slate-500">{{ t('activity.mergedPRs') }}</p><p class="mt-2 text-3xl font-semibold text-emerald-700">{{ metric(team.metrics.merged_prs) }}</p></article>
          <article class="border border-slate-200 bg-white p-5 shadow-sm"><p class="text-xs font-semibold uppercase text-slate-500">{{ t('activity.activeRepositories') }}</p><p class="mt-2 text-3xl font-semibold text-slate-950">{{ team.metrics.active_repositories }}</p></article>
        </section>

        <ElAlert
          v-if="!team.sync_coverage.complete"
          type="warning"
          :title="t('activity.syncNeeded', { count: team.sync_coverage.affected_repositories })"
          :closable="false"
        />

        <section class="min-w-0 border-y border-slate-200 bg-white" :aria-label="t('activity.members')">
          <div class="border-b border-slate-200 px-5 py-4"><h2 class="font-semibold text-slate-950">{{ t('activity.members') }}</h2></div>
          <div v-if="team.members.items.length === 0" class="px-5 py-10 text-sm text-slate-500">{{ t('activity.noMembers') }}</div>
          <div v-else class="divide-y divide-slate-100">
            <component
              :is="row.member.user_id > 0 ? RouterLink : 'div'"
              v-for="row in team.members.items"
              :key="memberKey(row.member.user_id, row.member.directory_member_external_id)"
              :data-testid="`activity-member-${memberKey(row.member.user_id, row.member.directory_member_external_id)}`"
              :to="row.member.user_id > 0 ? `/activity/members/${row.member.user_id}` : undefined"
              class="grid min-w-0 gap-3 px-5 py-4 sm:grid-cols-[minmax(10rem,1fr)_7rem_7rem_7rem] sm:items-center"
            >
              <div class="min-w-0"><p class="truncate font-medium text-slate-950">{{ row.member.display_name }}</p><p class="truncate text-sm text-slate-500">{{ row.member.email }}</p><p v-if="!row.available" class="mt-1 text-xs font-medium text-amber-700">{{ t('activity.noActivityData') }}</p></div>
              <div class="text-sm text-slate-600"><span class="sm:hidden">{{ t('activity.participatingPRs') }}: </span>{{ metric(row.metrics.participating_prs) }}</div>
              <div class="text-sm text-slate-600"><span class="sm:hidden">{{ t('activity.mergedPRs') }}: </span>{{ metric(row.metrics.merged_prs) }}</div>
              <div class="text-sm text-slate-600"><span class="sm:hidden">{{ t('activity.activeRepositories') }}: </span>{{ row.metrics.active_repositories }}</div>
            </component>
          </div>
          <CursorPager
            :has-previous="memberPageIndex > 0"
            :has-next="Boolean(team.members.next_cursor)"
            :loading="loading || memberLoading"
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
