<script setup lang="ts">
import { onMounted, ref } from 'vue'
import ActivityDateRange from '@/components/activity/ActivityDateRange.vue'
import CursorPager from '@/components/activity/CursorPager.vue'
import { useRepositoryActivity } from '@/composables/useRepositoryActivity'
import { useI18n } from '@/i18n'
import type { ActivityCountMetric } from '@/types/activity'

const props = defineProps<{ repoId: number }>()
const { t } = useI18n()
const {
  activity,
  range,
  loading,
  prLoading,
  error,
  prPageIndex,
  load,
  loadNextPRPage,
  loadPreviousPRPage,
  selectRange,
} = useRepositoryActivity(props.repoId)
const expandedPRs = ref(new Set<number>())

function metric(value: ActivityCountMetric) {
  return `${value.lower_bound ? '≥' : ''}${value.value}`
}

function shortSHA(value: string) {
  return value.slice(0, 10)
}

function date(value?: string) {
  return value ? new Intl.DateTimeFormat(undefined, { dateStyle: 'medium', timeStyle: 'short' }).format(new Date(value)) : '—'
}

function togglePR(prRecordID: number) {
  const next = new Set(expandedPRs.value)
  next.has(prRecordID) ? next.delete(prRecordID) : next.add(prRecordID)
  expandedPRs.value = next
}

onMounted(() => void load())
</script>

<template>
  <section data-testid="repo-activity" class="min-w-0 space-y-5">
    <div class="flex flex-col gap-3 lg:flex-row lg:items-end lg:justify-between">
      <div>
        <h2 class="text-lg font-semibold text-slate-950">{{ t('activity.repositoryActivity') }}</h2>
        <p class="mt-1 text-sm text-slate-600">{{ t('activity.repositoryActivitySubtitle') }}</p>
      </div>
      <ActivityDateRange :from="range.from" :to="range.to" :loading="loading" @change="selectRange" @refresh="load" />
    </div>

    <ElSkeleton v-if="loading && !activity" :rows="5" animated />
    <ElAlert
      v-if="error"
      type="error"
      :closable="false"
    >
      <template #title>
        <span>{{ t('activity.loadFailed') }}</span>
        <ElButton class="!ml-2" type="danger" link @click="load">{{ t('activity.retry') }}</ElButton>
      </template>
    </ElAlert>

    <template v-if="activity">
      <div class="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
        <div class="border border-slate-200 bg-white p-4"><p class="text-xs font-semibold uppercase text-slate-500">{{ t('activity.participatingMembers') }}</p><p class="mt-2 text-2xl font-semibold text-slate-950">{{ activity.participating_members }}</p></div>
        <div class="border border-slate-200 bg-white p-4"><p class="text-xs font-semibold uppercase text-slate-500">{{ t('activity.participatingPRs') }}</p><p class="mt-2 text-2xl font-semibold text-slate-950">{{ metric(activity.metrics.participating_prs) }}</p></div>
        <div class="border border-slate-200 bg-white p-4"><p class="text-xs font-semibold uppercase text-slate-500">{{ t('activity.mergedPRs') }}</p><p class="mt-2 text-2xl font-semibold text-emerald-700">{{ metric(activity.metrics.merged_prs) }}</p></div>
        <div class="border border-slate-200 bg-white p-4"><p class="text-xs font-semibold uppercase text-slate-500">{{ t('activity.commits') }}</p><p class="mt-2 text-2xl font-semibold text-slate-950">{{ activity.metrics.commit_count }}</p></div>
        <div class="border border-slate-200 bg-white p-4"><p class="text-xs font-semibold uppercase text-slate-500">{{ t('activity.latestActivity') }}</p><p data-testid="repo-activity-latest" class="mt-2 text-sm font-semibold text-slate-950">{{ date(activity.metrics.latest_activity) }}</p></div>
      </div>

      <ElAlert
        v-if="!activity.sync_coverage.complete"
        type="warning"
        :title="t('activity.syncNeeded', { count: activity.sync_coverage.affected_repositories })"
        :closable="false"
      />

      <section data-testid="repo-activity-prs" class="border-y border-slate-200 bg-white">
        <h3 class="border-b border-slate-200 px-5 py-4 font-semibold text-slate-950">{{ t('activity.pullRequests') }}</h3>
        <ElEmpty v-if="activity.prs.items.length === 0" :description="t('activity.noPullRequests')" :image-size="64" />
        <article v-for="pr in activity.prs.items" :key="`${pr.repo_config_id}:${pr.pr_record_id}`" class="border-b border-slate-100 px-5 py-4 last:border-0">
          <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
            <div class="min-w-0">
              <p class="text-xs text-slate-500">PR #{{ pr.scm_pr_id }}</p>
              <ElLink :href="pr.url" target="_blank" rel="noopener noreferrer" type="primary" class="mt-1 max-w-full font-medium">
                {{ pr.title }}
              </ElLink>
            </div>
            <ElButton
              :data-testid="`repo-activity-pr-toggle-${pr.pr_record_id}`"
              class="min-h-10 shrink-0 !ml-0"
              :aria-expanded="expandedPRs.has(pr.pr_record_id)"
              @click="togglePR(pr.pr_record_id)"
            >
              {{ t('activity.commits') }} · {{ pr.commits.length }}
            </ElButton>
          </div>
          <div v-if="expandedPRs.has(pr.pr_record_id)" :data-testid="`repo-activity-pr-commits-${pr.pr_record_id}`" class="mt-3 flex flex-wrap gap-2 bg-slate-50 p-3">
            <span v-for="commit in pr.commits" :key="`${commit.repo_config_id}:${commit.commit_sha}`" class="font-mono text-xs text-slate-500">{{ shortSHA(commit.commit_sha) }}</span>
          </div>
        </article>
        <CursorPager
          :has-previous="prPageIndex > 0"
          :has-next="Boolean(activity.prs.next_cursor)"
          :loading="loading || prLoading"
          :previous-label="t('activity.previousPage')"
          :next-label="t('activity.nextPage')"
          test-i-d-prefix="repo-activity-prs"
          @previous="loadPreviousPRPage"
          @next="loadNextPRPage"
        />
      </section>

      <section data-testid="repo-activity-commits" class="min-w-0 border-y border-slate-200 bg-white">
        <h3 class="border-b border-slate-200 px-5 py-4 font-semibold text-slate-950">{{ t('activity.commits') }}</h3>
        <ElEmpty v-if="activity.commits.items.length === 0" :description="t('activity.noCommits')" :image-size="64" />
        <div
          v-for="commit in activity.commits.items"
          :key="`${commit.repo_config_id}:${commit.commit_sha}`"
          :data-testid="`repo-activity-commit-${commit.repo_config_id}-${commit.commit_sha}`"
          class="grid gap-2 border-b border-slate-100 px-5 py-4 text-sm last:border-0 sm:grid-cols-[1fr_minmax(12rem,auto)] sm:gap-4"
        >
          <span class="break-all font-mono text-xs text-slate-700">{{ shortSHA(commit.commit_sha) }}</span>
          <span class="text-slate-600 sm:text-right">{{ commit.prs.map((pr) => `PR #${pr.scm_pr_id}`).join(' · ') }}</span>
        </div>
      </section>
    </template>
  </section>
</template>
