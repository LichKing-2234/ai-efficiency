<script setup lang="ts">
import { computed, ref } from 'vue'
import DepartmentTreeToggle from '@/components/DepartmentTreeToggle.vue'
import { useI18n } from '@/i18n'
import { useRoute } from 'vue-router'
import type { ActivityTeamIdentity } from '@/types/activity'

const props = defineProps<{
  team: ActivityTeamIdentity
  branchFor: (parentID: string) => { departments: ActivityTeamIdentity[] } | undefined
  ensureBranch: (parentID: string) => void
  hasChildren: (teamID: string) => boolean
}>()

const { t } = useI18n()
const route = useRoute()
const expanded = ref(false)
const children = computed(() => props.branchFor(props.team.external_id)?.departments ?? [])

function toggle() {
  if (!expanded.value) props.ensureBranch(props.team.external_id)
  expanded.value = !expanded.value
}
</script>

<template>
  <li class="min-w-0">
    <div class="flex min-w-0 items-center gap-2 border-b border-slate-100 px-4 py-3 last:border-0">
      <DepartmentTreeToggle
        v-if="hasChildren(team.external_id)"
        :data-testid="`activity-team-toggle-${team.external_id}`"
        :expanded="expanded"
        :expanded-label="t('activity.collapseTeam', { name: team.name })"
        :collapsed-label="t('activity.expandTeam', { name: team.name })"
        @toggle="toggle"
      />
      <span v-else class="h-7 w-7 shrink-0" aria-hidden="true" />
      <RouterLink
        :data-testid="`activity-team-${team.external_id}`"
        :to="{ path: `/activity/teams/${encodeURIComponent(team.external_id)}`, query: route.query }"
        class="group flex min-w-0 flex-1 items-center justify-between gap-4 py-1 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-cyan-600 focus-visible:ring-offset-2"
      >
        <span class="min-w-0">
          <span class="block truncate font-medium text-slate-950 group-hover:text-cyan-800">{{ team.name }}</span>
          <span class="mt-0.5 block truncate text-sm text-slate-500">{{ team.display_path }}</span>
        </span>
        <span class="shrink-0 text-sm font-medium text-slate-600">{{ t('activity.memberCount', { count: team.member_count }) }}</span>
      </RouterLink>
    </div>
    <ul v-if="expanded && children.length" class="ml-5 border-l border-slate-200 pl-3">
      <ActivityTeamTreeNode
        v-for="child in children"
        :key="child.external_id"
        :team="child"
        :branch-for="branchFor"
        :ensure-branch="ensureBranch"
        :has-children="hasChildren"
      />
    </ul>
  </li>
</template>
