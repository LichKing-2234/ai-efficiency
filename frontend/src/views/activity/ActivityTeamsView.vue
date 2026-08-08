<script setup lang="ts">
import AppLayout from '@/components/AppLayout.vue'
import ActivityTeamTreeNode from '@/components/activity/ActivityTeamTreeNode.vue'
import { useActivityTeams } from '@/composables/useActivityTeams'
import { useI18n } from '@/i18n'

const { t } = useI18n()
const { scope, loading, error, load, rootBranch, branchFor, ensureBranch, hasChildren } = useActivityTeams()
</script>

<template>
  <AppLayout>
    <div class="min-w-0 space-y-6">
      <header>
        <p class="text-xs font-semibold uppercase tracking-[0.18em] text-cyan-700">{{ t('activity.eyebrow') }}</p>
        <h1 class="mt-1 text-2xl font-bold text-slate-950">{{ t('activity.teamsTitle') }}</h1>
        <p class="mt-1 max-w-3xl text-sm text-slate-600">{{ t('activity.teamsSubtitle') }}</p>
      </header>

      <div v-if="loading && !scope" role="status" class="border-y border-slate-200 bg-white px-5 py-12 text-center text-sm text-slate-500">
        {{ t('activity.loadingTeams') }}
      </div>
      <ElAlert v-else-if="error" type="error" :closable="false">
        <template #title>
          <span>{{ t('activity.teamsLoadFailed') }}</span>
          <ElButton class="!ml-2" type="primary" link @click="load">{{ t('activity.retry') }}</ElButton>
        </template>
      </ElAlert>
      <div v-else-if="scope && !scope.can_view_teams" data-testid="activity-teams-forbidden" class="border-y border-slate-200 bg-white px-5 py-10 text-sm text-slate-600">
        {{ t('activity.teamsForbidden') }}
      </div>
      <div v-else-if="scope && scope.teams.length === 0" class="border-y border-slate-200 bg-white px-5 py-10 text-sm text-slate-600">
        {{ t('activity.noTeams') }}
      </div>
      <section v-else-if="scope" class="min-w-0 border-y border-slate-200 bg-white" :aria-label="t('activity.teamsTitle')">
        <ul>
          <ActivityTeamTreeNode
            v-for="team in rootBranch?.departments ?? []"
            :key="team.external_id"
            :team="team"
            :branch-for="branchFor"
            :ensure-branch="ensureBranch"
            :has-children="hasChildren"
          />
        </ul>
      </section>
    </div>
  </AppLayout>
</template>
