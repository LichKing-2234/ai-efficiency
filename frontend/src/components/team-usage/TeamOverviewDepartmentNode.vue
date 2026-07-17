<script setup lang="ts">
import { computed } from 'vue'
import DepartmentTreeToggle from '@/components/DepartmentTreeToggle.vue'
import { useI18n } from '@/i18n'
import type { TeamOverviewMember, TeamUsageOrganizationDepartment } from '@/types'
import type { TeamUsageOrganizationBranchState } from '@/composables/useTeamUsageOrganization'

const props = defineProps<{
  node: TeamUsageOrganizationDepartment
  expanded: (departmentID: string) => boolean
  toggle: (node: TeamUsageOrganizationDepartment) => void
  branchFor: (departmentID: string) => TeamUsageOrganizationBranchState | undefined
  loadMoreDepartments: (departmentID: string) => void
  loadMoreMembers: (departmentID: string) => void
  openMember: (member: TeamOverviewMember) => void
  canOpen: (member: TeamOverviewMember) => boolean
  isConnected: (member: TeamOverviewMember) => boolean
  memberKey: (member: TeamOverviewMember) => string
  memberTestId: (member: TeamOverviewMember) => string
  formatCost: (value: number) => string
  formatTokens: (value: number | null | undefined) => string
  formatDepartmentTokenSummary: (node: TeamUsageOrganizationDepartment) => string
  departmentMemberCountLabel: (count: number) => string
  connectedMemberCountLabel: (count: number) => string
  departmentIndentStyle: (node: TeamUsageOrganizationDepartment) => Record<string, string>
  memberIndentStyle: (node: TeamUsageOrganizationDepartment) => Record<string, string>
  departmentAriaLevel: (node: TeamUsageOrganizationDepartment) => string
  memberAriaLevel: (node: TeamUsageOrganizationDepartment) => string
}>()

const { t } = useI18n()
const branch = computed(() => props.branchFor(props.node.department_external_id))

function hasNestedRows() {
  return props.node.has_children || props.node.direct_member_count > 0
}

function toggleNode() {
  if (!hasNestedRows()) return
  props.toggle(props.node)
}
</script>

<template>
  <div
    :data-testid="`team-overview-department-${props.node.department_external_id}`"
    role="treeitem"
    :aria-level="props.departmentAriaLevel(props.node)"
    :aria-expanded="hasNestedRows() ? props.expanded(props.node.department_external_id) : undefined"
    :style="props.departmentIndentStyle(props.node)"
    class="flex w-full cursor-pointer flex-col gap-2 border-b border-gray-100 bg-white py-3 pr-4 text-left last:border-b-0 hover:bg-gray-50 sm:flex-row sm:items-center sm:justify-between"
    tabindex="0"
    @click="toggleNode"
    @keydown.enter.prevent="toggleNode"
    @keydown.space.prevent="toggleNode"
  >
    <div class="min-w-0">
      <div class="flex items-center gap-2">
        <DepartmentTreeToggle
          v-if="hasNestedRows()"
          :data-testid="`team-overview-department-toggle-${props.node.department_external_id}`"
          :expanded="props.expanded(props.node.department_external_id)"
          :expanded-label="t('teamUsage.collapseDepartment')"
          :collapsed-label="t('teamUsage.expandDepartment')"
          @toggle="toggleNode"
        />
        <span v-else class="inline-flex h-7 w-7" aria-hidden="true"></span>
        <span class="truncate font-medium text-gray-900">{{ props.node.name }}</span>
      </div>
      <div class="mt-1 truncate text-xs text-gray-500">{{ props.node.display_path }}</div>
    </div>
    <div class="flex shrink-0 flex-wrap gap-2 text-xs text-gray-600">
      <span class="rounded-full bg-gray-100 px-2 py-0.5">{{ props.departmentMemberCountLabel(props.node.aggregate_member_count) }}</span>
      <span class="rounded-full bg-emerald-50 px-2 py-0.5 text-emerald-700">{{ props.connectedMemberCountLabel(props.node.connected_member_count) }}</span>
      <span class="rounded-full bg-blue-50 px-2 py-0.5 text-blue-700">{{ props.formatCost(props.node.range_actual_cost) }}</span>
      <span class="rounded-full bg-violet-50 px-2 py-0.5 text-violet-700">{{ props.formatDepartmentTokenSummary(props.node) }}</span>
    </div>
  </div>

  <template v-if="props.expanded(props.node.department_external_id)">
    <div
      v-if="branch?.loading && !branch.loaded"
      :data-testid="`team-overview-organization-loading-${props.node.department_external_id}`"
      class="border-b border-gray-100 px-4 py-3 text-sm text-slate-500"
      :style="props.memberIndentStyle(props.node)"
    >
      {{ t('settings.loading') }}
    </div>
    <div
      v-else-if="branch?.error && !branch.loaded"
      :data-testid="`team-overview-organization-error-${props.node.department_external_id}`"
      class="border-b border-gray-100 px-4 py-3 text-sm text-slate-600"
      :style="props.memberIndentStyle(props.node)"
    >
      {{ t('teamUsage.unavailable') }}
    </div>
    <template v-else-if="branch?.loaded">
      <div
        v-for="member in branch.members"
        :key="props.memberKey(member)"
        :data-testid="props.memberTestId(member)"
        role="treeitem"
        :aria-level="props.memberAriaLevel(props.node)"
        :style="props.memberIndentStyle(props.node)"
        :class="[
          'flex w-full flex-col gap-2 border-b border-gray-100 py-3 pr-4 text-sm last:border-b-0 sm:flex-row sm:items-center sm:justify-between',
          props.isConnected(member) ? 'bg-white hover:bg-gray-50' : 'bg-red-50 text-red-950',
        ]"
      >
        <div class="min-w-0">
          <div class="flex items-center gap-2">
            <span class="inline-flex h-5 w-5" aria-hidden="true"></span>
            <span class="truncate font-medium text-gray-900">{{ member.display_name }}</span>
            <span v-if="!props.isConnected(member)" class="rounded-full bg-red-100 px-2 py-0.5 text-xs font-medium text-red-700">
              {{ t('teamUsage.notConnected') }}
            </span>
          </div>
          <div class="mt-1 truncate text-xs text-gray-500">{{ member.email }}</div>
        </div>
        <div class="flex shrink-0 flex-wrap items-center gap-2 text-xs text-gray-600">
          <span class="tabular-nums text-gray-900">{{ props.formatCost(member.range_actual_cost) }}</span>
          <span class="tabular-nums">{{ props.formatTokens(member.total_tokens) }} {{ t('teamUsage.tokens') }}</span>
          <button
            type="button"
            class="rounded-md border border-gray-300 bg-white px-2.5 py-1 text-xs font-medium text-gray-700 hover:bg-gray-50 disabled:cursor-not-allowed disabled:opacity-50"
            :disabled="!props.canOpen(member)"
            @click.stop="props.openMember(member)"
          >
            {{ t('teamUsage.openMember') }}
          </button>
        </div>
      </div>

      <TeamOverviewDepartmentNode
        v-for="child in branch.departments"
        :key="child.department_external_id"
        :node="child"
        :expanded="props.expanded"
        :toggle="props.toggle"
        :branch-for="props.branchFor"
        :load-more-departments="props.loadMoreDepartments"
        :load-more-members="props.loadMoreMembers"
        :open-member="props.openMember"
        :can-open="props.canOpen"
        :is-connected="props.isConnected"
        :member-key="props.memberKey"
        :member-test-id="props.memberTestId"
        :format-cost="props.formatCost"
        :format-tokens="props.formatTokens"
        :format-department-token-summary="props.formatDepartmentTokenSummary"
        :department-member-count-label="props.departmentMemberCountLabel"
        :connected-member-count-label="props.connectedMemberCountLabel"
        :department-indent-style="props.departmentIndentStyle"
        :member-indent-style="props.memberIndentStyle"
        :department-aria-level="props.departmentAriaLevel"
        :member-aria-level="props.memberAriaLevel"
      />

      <div
        v-if="branch.nextDepartmentCursor || branch.nextMemberCursor || branch.error"
        class="flex flex-wrap gap-2 border-b border-gray-100 px-4 py-3"
        :style="props.memberIndentStyle(props.node)"
      >
        <button
          v-if="branch.nextDepartmentCursor"
          :data-testid="`team-overview-departments-more-${props.node.department_external_id}`"
          type="button"
          class="rounded-md border border-slate-300 px-3 py-1.5 text-sm text-slate-700 disabled:opacity-50"
          :disabled="branch.departmentLoading || branch.memberLoading"
          @click.stop="props.loadMoreDepartments(props.node.department_external_id)"
        >
          {{ t('teamUsage.loadMoreDepartments') }}
        </button>
        <button
          v-if="branch.nextMemberCursor"
          :data-testid="`team-overview-members-more-${props.node.department_external_id}`"
          type="button"
          class="rounded-md border border-slate-300 px-3 py-1.5 text-sm text-slate-700 disabled:opacity-50"
          :disabled="branch.departmentLoading || branch.memberLoading"
          @click.stop="props.loadMoreMembers(props.node.department_external_id)"
        >
          {{ t('teamUsage.loadMoreMembers') }}
        </button>
        <span v-if="branch.error" :data-testid="`team-overview-organization-error-${props.node.department_external_id}`" class="text-sm text-slate-600">
          {{ t('teamUsage.unavailable') }}
        </span>
      </div>
    </template>
  </template>
</template>
