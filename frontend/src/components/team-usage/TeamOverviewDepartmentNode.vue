<script setup lang="ts">
import { useI18n } from '@/i18n'
import type { TeamOverviewMember, TeamOverviewMemberNode } from '@/types'

const props = defineProps<{
  node: TeamOverviewMemberNode
  expanded: (node: TeamOverviewMemberNode) => boolean
  toggle: (node: TeamOverviewMemberNode) => void
  openMember: (member: TeamOverviewMember) => void
  canOpen: (member: TeamOverviewMember) => boolean
  isConnected: (member: TeamOverviewMember) => boolean
  memberKey: (member: TeamOverviewMember) => string
  memberTestId: (member: TeamOverviewMember) => string
  formatCost: (value: number) => string
  formatTokens: (value: number | null | undefined) => string
  formatDepartmentTokenSummary: (node: TeamOverviewMemberNode) => string
  departmentMemberCountLabel: (count: number) => string
  connectedMemberCountLabel: (count: number) => string
  departmentIndentStyle: (node: TeamOverviewMemberNode) => Record<string, string>
  departmentAriaLevel: (node: TeamOverviewMemberNode) => string
}>()

const { t } = useI18n()
</script>

<template>
  <div>
    <div
      :data-testid="`team-overview-department-${props.node.department_external_id}`"
      role="treeitem"
      :aria-level="props.departmentAriaLevel(props.node)"
      :aria-expanded="(props.node.children?.length ?? 0) > 0 ? props.expanded(props.node) : undefined"
      :style="props.departmentIndentStyle(props.node)"
      class="flex flex-col gap-2 border-b border-slate-100 bg-slate-50 py-3 pr-4 text-left sm:flex-row sm:items-center sm:justify-between"
    >
      <div class="min-w-0">
        <div class="flex items-center gap-2">
          <button
            v-if="(props.node.children?.length ?? 0) > 0"
            :data-testid="`team-overview-department-toggle-${props.node.department_external_id}`"
            type="button"
            class="inline-flex h-5 w-5 items-center justify-center rounded text-xs text-slate-500 hover:bg-slate-100"
            :aria-label="props.expanded(props.node) ? t('teamUsage.collapseDepartment') : t('teamUsage.expandDepartment')"
            @click.stop="props.toggle(props.node)"
          >
            {{ props.expanded(props.node) ? '-' : '+' }}
          </button>
          <span v-else class="inline-flex h-5 w-5" aria-hidden="true"></span>
          <span class="truncate font-medium text-slate-950">{{ props.node.name }}</span>
        </div>
        <div class="mt-1 truncate text-xs text-slate-500">{{ props.node.display_path }}</div>
      </div>
      <div class="flex shrink-0 flex-wrap gap-2 text-xs">
        <span class="rounded-full bg-white px-2 py-0.5 text-slate-700">{{ props.departmentMemberCountLabel(props.node.member_count) }}</span>
        <span class="rounded-full bg-emerald-50 px-2 py-0.5 text-emerald-700">{{ props.connectedMemberCountLabel(props.node.connected_member_count) }}</span>
        <span class="rounded-full bg-blue-50 px-2 py-0.5 text-blue-700">{{ props.formatCost(props.node.range_actual_cost) }}</span>
        <span class="rounded-full bg-violet-50 px-2 py-0.5 text-violet-700">{{ props.formatDepartmentTokenSummary(props.node) }}</span>
      </div>
    </div>

    <template v-if="props.expanded(props.node) || (props.node.children?.length ?? 0) === 0">
      <div
        v-for="member in props.node.members"
        :key="props.memberKey(member)"
        :data-testid="props.memberTestId(member)"
        :class="[
          'grid gap-2 border-b border-slate-100 px-4 py-3 text-sm sm:grid-cols-[minmax(9rem,1.1fr)_minmax(12rem,1.4fr)_8rem_9rem_9rem]',
          props.isConnected(member) ? 'bg-white hover:bg-slate-50' : 'bg-red-50 text-red-950 hover:bg-red-50',
        ]"
      >
        <div class="min-w-0">
          <div class="truncate font-medium text-slate-950">{{ member.display_name }}</div>
          <div v-if="!props.isConnected(member)" class="mt-1 inline-flex rounded-full bg-red-100 px-2 py-0.5 text-xs font-medium text-red-700">
            {{ t('teamUsage.notConnected') }}
          </div>
        </div>
        <div class="min-w-0 truncate text-slate-600">{{ member.email }}</div>
        <div class="text-right tabular-nums text-slate-900 sm:text-right">{{ props.formatCost(member.range_actual_cost) }}</div>
        <div class="text-right tabular-nums text-slate-900 sm:text-right">{{ props.formatTokens(member.total_tokens) }}</div>
        <div class="text-right">
          <button
            type="button"
            class="rounded-md border border-slate-300 bg-white px-2.5 py-1 text-xs font-medium text-slate-700 hover:bg-slate-50 disabled:cursor-not-allowed disabled:opacity-50"
            :disabled="!props.canOpen(member)"
            @click="props.openMember(member)"
          >
            {{ t('teamUsage.openMember') }}
          </button>
        </div>
      </div>
      <TeamOverviewDepartmentNode
        v-for="child in props.node.children"
        :key="child.department_external_id"
        :node="child"
        :expanded="props.expanded"
        :toggle="props.toggle"
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
        :department-aria-level="props.departmentAriaLevel"
      />
    </template>
  </div>
</template>
