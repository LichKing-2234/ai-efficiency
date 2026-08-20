<script setup lang="ts">
import { computed, ref } from 'vue'
import { useRoute } from 'vue-router'
import DepartmentTreeToggle from '@/components/DepartmentTreeToggle.vue'
import type { TeamUsageOrganizationBranchState } from '@/composables/useTeamUsageOrganization'
import { useI18n } from '@/i18n'
import type { TeamUsageOrganizationDepartment } from '@/types'

const props = defineProps<{
  team: TeamUsageOrganizationDepartment
  branchFor: (parentID: string) => TeamUsageOrganizationBranchState | undefined
  ensureBranch: (parentID: string) => void
  loadMoreDepartments: (parentID: string | null) => void
}>()

const { t } = useI18n()
const route = useRoute()
const expanded = ref(false)
const branch = computed(() => props.branchFor(props.team.department_external_id))

function toggle() {
  if (!expanded.value) props.ensureBranch(props.team.department_external_id)
  expanded.value = !expanded.value
}
</script>

<template>
  <li class="min-w-0">
    <div class="flex min-w-0 items-center gap-2 border-b border-slate-100 px-4 py-3 last:border-0">
      <DepartmentTreeToggle
        v-if="team.has_children"
        :data-testid="`activity-team-toggle-${team.department_external_id}`"
        :expanded="expanded"
        :expanded-label="t('activity.collapseTeam', { name: team.name })"
        :collapsed-label="t('activity.expandTeam', { name: team.name })"
        @toggle="toggle"
      />
      <span v-else class="h-7 w-7 shrink-0" aria-hidden="true" />
      <RouterLink
        :data-testid="`activity-team-${team.department_external_id}`"
        :to="{ path: `/activity/teams/${encodeURIComponent(team.department_external_id)}`, query: route.query }"
        class="group flex min-w-0 flex-1 items-center justify-between gap-4 py-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-600 focus-visible:ring-offset-2"
      >
        <span class="min-w-0">
          <span class="block truncate font-medium text-slate-950 group-hover:text-cyan-800">{{ team.name }}</span>
          <span class="mt-0.5 block truncate text-sm text-slate-500">{{ team.display_path }}</span>
        </span>
        <span class="shrink-0 text-sm font-medium text-slate-600">{{ t('activity.memberCount', { count: team.aggregate_member_count }) }}</span>
      </RouterLink>
    </div>
    <div v-if="expanded && branch?.loading" role="status" class="ml-12 px-4 py-3 text-sm text-slate-500">{{ t('activity.loadingTeams') }}</div>
    <div v-else-if="expanded && branch?.error && !branch.loaded" class="ml-12 px-4 py-3 text-sm text-slate-600">
      {{ t('activity.teamsLoadFailed') }}
      <ElButton type="primary" link @click="ensureBranch(team.department_external_id)">{{ t('activity.retry') }}</ElButton>
    </div>
    <ul v-else-if="expanded && branch?.departments.length" class="ml-5 border-l border-slate-200 pl-3">
      <ActivityTeamTreeNode
        v-for="child in branch.departments"
        :key="child.department_external_id"
        :team="child"
        :branch-for="branchFor"
        :ensure-branch="ensureBranch"
        :load-more-departments="loadMoreDepartments"
      />
    </ul>
    <ElButton
      v-if="expanded && branch?.nextDepartmentCursor"
      class="ml-12 my-2"
      :disabled="branch.departmentLoading"
      @click="loadMoreDepartments(team.department_external_id)"
    >
      {{ t('teamUsage.loadMoreDepartments') }}
    </ElButton>
  </li>
</template>
