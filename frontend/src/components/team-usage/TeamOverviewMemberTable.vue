<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useI18n } from '@/i18n'
import TeamOverviewDepartmentNode from '@/components/team-usage/TeamOverviewDepartmentNode.vue'
import { formatTokenCount } from '@/utils/formatters'
import type { TeamOverviewMember, TeamUsageOrganizationDepartment } from '@/types'
import type { TeamUsageOrganizationBranchState } from '@/composables/useTeamUsageOrganization'

const props = withDefaults(defineProps<{
  members: TeamOverviewMember[]
  organizationRoot?: TeamUsageOrganizationBranchState
  organizationBranches?: Record<string, TeamUsageOrganizationBranchState>
  organizationInvalidatedDepartmentIds?: string[]
  organizationResetVersion?: number
  organizationBranchFor: (departmentID: string) => TeamUsageOrganizationBranchState | undefined
  memberLoading?: boolean
  memberError?: boolean
  memberTotalCount?: number
  hasPreviousPage?: boolean
  hasNextPage?: boolean
}>(), {
  organizationBranches: () => ({}),
  organizationInvalidatedDepartmentIds: () => [],
  organizationResetVersion: 0,
  memberLoading: false,
  memberError: false,
  memberTotalCount: 0,
  hasPreviousPage: false,
  hasNextPage: false,
})

const emit = defineEmits<{
  'open-member': [userID: number]
  'previous-page': []
  'next-page': []
  'expand-department': [departmentID: string]
  'load-more-departments': [departmentID: string | null]
  'load-more-members': [departmentID: string]
}>()

const { t } = useI18n()
type DetailView = 'ranking' | 'organization'
const detailView = ref<DetailView>('ranking')

function formatCost(value: number) {
  return `${value.toFixed(2)} USD`
}

function memberKey(member: TeamOverviewMember) {
  return member.user_id > 0 ? `user:${member.user_id}` : `directory:${member.directory_member_external_id || member.email}`
}

function memberTestID(member: TeamOverviewMember) {
  return `team-overview-member-${member.user_id > 0 ? `user-${member.user_id}` : `directory-${member.directory_member_external_id || member.email}`}`
}

function canOpen(member: TeamOverviewMember) {
  return member.selectable && member.user_id > 0
}

function isConnected(member: TeamOverviewMember) {
  return member.relay_user_id != null
}

function openMember(member: TeamOverviewMember) {
  if (!canOpen(member)) return
  emit('open-member', member.user_id)
}

function formatDepartmentTokenSummary(node: TeamUsageOrganizationDepartment) {
  if (node.range_total_tokens == null) return '-'
  return `${formatTokenCount(node.range_total_tokens)} ${t('teamUsage.tokens')}`
}

function departmentMemberCountLabel(count: number) {
  return t(count === 1 ? 'teamUsage.memberCountSingular' : 'teamUsage.memberCountPlural', { count })
}

function connectedMemberCountLabel(count: number) {
  return t(count === 1 ? 'teamUsage.connectedCountSingular' : 'teamUsage.connectedCountPlural', { count })
}

function departmentDepth(node: TeamUsageOrganizationDepartment) {
  const depth = Number(node.depth ?? 0)
  return Number.isFinite(depth) && depth > 0 ? Math.min(depth, 8) : 0
}

function departmentIndentStyle(node: TeamUsageOrganizationDepartment) {
  const depth = departmentDepth(node)
  return { paddingLeft: depth === 0 ? '1rem' : `${depth * 1.25}rem` }
}

function memberIndentStyle(node: TeamUsageOrganizationDepartment) {
  return { paddingLeft: `${(departmentDepth(node) + 1) * 1.25}rem` }
}

function departmentAriaLevel(node: TeamUsageOrganizationDepartment) {
  return String(departmentDepth(node) + 1)
}

function memberAriaLevel(node: TeamUsageOrganizationDepartment) {
  return String(departmentDepth(node) + 2)
}

const hasOrganization = computed(() => props.organizationRoot != null)
const memberPageStart = computed(() => props.members[0]?.rank ?? 0)
const memberPageEnd = computed(() => props.members[props.members.length - 1]?.rank ?? 0)
const showMemberPagination = computed(() => props.members.length > 0 && (
  props.hasPreviousPage || props.hasNextPage || props.memberTotalCount > props.members.length
))
const expandedDepartmentIds = ref<Set<string>>(new Set())

watch(() => props.organizationResetVersion, () => {
  expandedDepartmentIds.value = new Set()
})

watch(() => props.organizationInvalidatedDepartmentIds, (invalidatedDepartmentIds) => {
  const next = new Set(expandedDepartmentIds.value)
  let changed = false
  for (const departmentID of invalidatedDepartmentIds) {
    changed = next.delete(departmentID) || changed
  }
  if (changed) expandedDepartmentIds.value = next
})

function departmentExpanded(departmentID: string) {
  return expandedDepartmentIds.value.has(departmentID)
}

function toggleDepartment(node: TeamUsageOrganizationDepartment) {
  if (!node.has_children && node.direct_member_count <= 0) return
  const next = new Set(expandedDepartmentIds.value)
  if (next.has(node.department_external_id)) {
    next.delete(node.department_external_id)
  } else {
    next.add(node.department_external_id)
    emit('expand-department', node.department_external_id)
  }
  expandedDepartmentIds.value = next
}

function viewButtonClass(view: DetailView) {
  return [
    'rounded-md px-3 py-1.5 text-sm font-medium transition-colors',
    detailView.value === view
      ? 'bg-gray-900 text-white'
      : 'text-gray-600 hover:bg-gray-50',
  ]
}
</script>

<template>
  <section class="rounded-lg border border-slate-200 bg-white shadow-sm">
    <div class="flex flex-wrap items-center justify-between gap-3 border-b border-slate-200 px-4 py-3">
      <h2 class="text-base font-semibold text-slate-950">{{ t('teamUsage.memberTable') }}</h2>
      <div v-if="hasOrganization" class="inline-flex rounded-lg border border-gray-200 bg-white p-1" aria-label="Member detail view">
        <button
          data-testid="team-overview-ranking-view"
          type="button"
          :class="viewButtonClass('ranking')"
          @click="detailView = 'ranking'"
        >
          {{ t('teamUsage.rankingView') }}
        </button>
        <button
          data-testid="team-overview-organization-view"
          type="button"
          :class="viewButtonClass('organization')"
          @click="detailView = 'organization'"
        >
          {{ t('teamUsage.organizationView') }}
        </button>
      </div>
    </div>

    <div v-if="detailView === 'organization' && props.organizationRoot" class="p-4" data-testid="team-overview-organization-tree">
      <div
        v-if="props.organizationRoot.loading && !props.organizationRoot.loaded"
        data-testid="team-overview-organization-loading-root"
        class="py-3 text-sm text-slate-500"
      >
        {{ t('settings.loading') }}
      </div>
      <div
        v-else-if="props.organizationRoot.error && !props.organizationRoot.loaded"
        data-testid="team-overview-organization-error-root"
        class="py-3 text-sm text-slate-600"
      >
        {{ t('teamUsage.unavailable') }}
      </div>
      <div v-else-if="props.organizationRoot.departments.length === 0" class="py-3 text-sm text-slate-500">-</div>
      <template v-else>
        <div class="overflow-hidden rounded-md border border-gray-200" role="tree">
          <TeamOverviewDepartmentNode
            v-for="department in props.organizationRoot.departments"
            :key="department.department_external_id"
            :node="department"
            :expanded="departmentExpanded"
            :toggle="toggleDepartment"
            :branch-for="props.organizationBranchFor"
            :load-more-departments="(departmentID) => emit('load-more-departments', departmentID)"
            :load-more-members="(departmentID) => emit('load-more-members', departmentID)"
            :open-member="openMember"
            :can-open="canOpen"
            :is-connected="isConnected"
            :member-key="memberKey"
            :member-test-id="memberTestID"
            :format-cost="formatCost"
            :format-tokens="formatTokenCount"
            :format-department-token-summary="formatDepartmentTokenSummary"
            :department-member-count-label="departmentMemberCountLabel"
            :connected-member-count-label="connectedMemberCountLabel"
            :department-indent-style="departmentIndentStyle"
            :member-indent-style="memberIndentStyle"
            :department-aria-level="departmentAriaLevel"
            :member-aria-level="memberAriaLevel"
          />
        </div>
        <button
          v-if="props.organizationRoot.nextDepartmentCursor"
          data-testid="team-overview-departments-more-root"
          type="button"
          class="mt-3 rounded-md border border-slate-300 px-3 py-1.5 text-sm text-slate-700 disabled:opacity-50"
          :disabled="props.organizationRoot.departmentLoading"
          @click="emit('load-more-departments', null)"
        >
          {{ t('teamUsage.loadMoreDepartments') }}
        </button>
      </template>
    </div>

    <div
      v-else-if="props.memberLoading && props.members.length === 0"
      data-testid="team-overview-members-loading"
      class="px-4 py-4 text-sm text-slate-500"
    >
      {{ t('settings.loading') }}
    </div>

    <div
      v-else-if="props.memberError && props.members.length === 0"
      data-testid="team-overview-members-error"
      class="px-4 py-4 text-sm text-slate-600"
    >
      {{ t('teamUsage.unavailable') }}
    </div>

    <div v-else-if="props.members.length === 0" class="px-4 py-4 text-sm text-slate-500">-</div>

    <div v-else class="overflow-x-auto" data-testid="team-overview-ranking-table">
      <table class="min-w-full divide-y divide-slate-100 text-sm">
        <thead class="bg-slate-50 text-xs font-semibold uppercase text-slate-500">
          <tr>
            <th class="whitespace-nowrap px-4 py-2 text-left">{{ t('teamUsage.memberName') }}</th>
            <th class="whitespace-nowrap px-4 py-2 text-left">{{ t('teamUsage.memberEmail') }}</th>
            <th class="whitespace-nowrap px-4 py-2 text-left">{{ t('teamUsage.memberDepartment') }}</th>
            <th class="whitespace-nowrap px-4 py-2 text-right">{{ t('teamUsage.rangeTotalTokens') }}</th>
            <th class="whitespace-nowrap px-4 py-2 text-right">{{ t('teamUsage.rangeActualCost') }}</th>
            <th class="whitespace-nowrap px-4 py-2 text-right">{{ t('teamUsage.memberAction') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr
            v-for="member in props.members"
            :key="memberKey(member)"
            :data-testid="memberTestID(member)"
            :class="[isConnected(member) ? 'hover:bg-slate-50' : 'bg-red-50 text-red-950 hover:bg-red-50']"
          >
            <td class="whitespace-nowrap px-4 py-2 font-medium text-slate-900">{{ member.display_name }}</td>
            <td class="whitespace-nowrap px-4 py-2 text-slate-600">{{ member.email }}</td>
            <td class="min-w-56 px-4 py-2 text-slate-600">{{ member.department_display_path || '-' }}</td>
            <td class="whitespace-nowrap px-4 py-2 text-right tabular-nums text-slate-900">{{ formatTokenCount(member.total_tokens) }}</td>
            <td class="whitespace-nowrap px-4 py-2 text-right tabular-nums text-slate-900">{{ formatCost(member.range_actual_cost) }}</td>
            <td class="whitespace-nowrap px-4 py-2 text-right">
              <span v-if="!isConnected(member)" class="mr-2 rounded-full bg-red-100 px-2 py-0.5 text-xs font-medium text-red-700">{{ t('teamUsage.notConnected') }}</span>
              <button
                type="button"
                class="rounded-md border border-slate-300 px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
                :disabled="!canOpen(member)"
                @click="openMember(member)"
              >
                {{ t('teamUsage.openMember') }}
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <div
      v-if="detailView === 'ranking' && props.memberError && props.members.length > 0"
      data-testid="team-overview-members-error"
      class="border-t border-slate-200 px-4 py-2 text-sm text-slate-600"
    >
      {{ t('teamUsage.unavailable') }}
    </div>

    <div
      v-if="detailView === 'ranking' && showMemberPagination"
      data-testid="team-overview-member-pagination"
      class="flex flex-wrap items-center justify-between gap-3 border-t border-slate-200 px-4 py-3"
    >
      <span class="text-sm text-slate-500">{{ t('teamUsage.memberPageRange', { start: memberPageStart, end: memberPageEnd, total: props.memberTotalCount }) }}</span>
      <div class="flex items-center gap-2">
        <button
          data-testid="team-overview-members-previous"
          type="button"
          class="rounded-md border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
          :disabled="!props.hasPreviousPage || props.memberLoading"
          @click="emit('previous-page')"
        >
          {{ t('teamUsage.memberPagePrevious') }}
        </button>
        <button
          data-testid="team-overview-members-next"
          type="button"
          class="rounded-md border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
          :disabled="!props.hasNextPage || props.memberLoading"
          @click="emit('next-page')"
        >
          {{ t('teamUsage.memberPageNext') }}
        </button>
      </div>
    </div>
  </section>
</template>
