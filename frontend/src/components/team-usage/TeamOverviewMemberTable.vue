<script setup lang="ts">
import { useI18n } from '@/i18n'
import type { TeamOverviewMember } from '@/types'

const props = defineProps<{
  members: TeamOverviewMember[]
}>()

const emit = defineEmits<{
  'open-member': [userID: number]
}>()

const { t } = useI18n()

function formatCost(value: number) {
  return `${value.toFixed(2)} USD`
}

function memberKey(member: TeamOverviewMember) {
  return member.user_id > 0 ? `user:${member.user_id}` : `directory:${member.directory_member_external_id || member.email}`
}

function canOpen(member: TeamOverviewMember) {
  return member.selectable && member.user_id > 0
}

function openMember(member: TeamOverviewMember) {
  if (!canOpen(member)) return
  emit('open-member', member.user_id)
}
</script>

<template>
  <section class="rounded-lg border border-slate-200 bg-white shadow-sm">
    <div class="border-b border-slate-200 px-4 py-3">
      <h2 class="text-base font-semibold text-slate-950">{{ t('teamUsage.memberTable') }}</h2>
    </div>

    <div v-if="props.members.length === 0" class="px-4 py-4 text-sm text-slate-500">
      -
    </div>

    <div v-else class="overflow-x-auto">
      <table class="min-w-full divide-y divide-slate-100 text-sm">
        <thead class="bg-slate-50 text-xs font-semibold uppercase text-slate-500">
          <tr>
            <th class="whitespace-nowrap px-4 py-2 text-left">{{ t('teamUsage.memberName') }}</th>
            <th class="whitespace-nowrap px-4 py-2 text-left">{{ t('teamUsage.memberEmail') }}</th>
            <th class="whitespace-nowrap px-4 py-2 text-left">{{ t('teamUsage.memberDepartment') }}</th>
            <th class="whitespace-nowrap px-4 py-2 text-right">{{ t('teamUsage.rangeActualCost') }}</th>
            <th class="whitespace-nowrap px-4 py-2 text-right">{{ t('teamUsage.memberAction') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr
            v-for="member in props.members"
            :key="memberKey(member)"
            class="hover:bg-slate-50"
          >
            <td class="whitespace-nowrap px-4 py-2 font-medium text-slate-900">{{ member.display_name }}</td>
            <td class="whitespace-nowrap px-4 py-2 text-slate-600">{{ member.email }}</td>
            <td class="min-w-56 px-4 py-2 text-slate-600">{{ member.department_display_path || '-' }}</td>
            <td class="whitespace-nowrap px-4 py-2 text-right tabular-nums text-slate-900">
              {{ formatCost(member.range_actual_cost) }}
            </td>
            <td class="whitespace-nowrap px-4 py-2 text-right">
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
  </section>
</template>
