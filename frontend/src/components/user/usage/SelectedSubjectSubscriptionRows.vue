<script setup lang="ts">
import { computed, ref } from 'vue'
import type { SubjectSubscriptionGroup, UpdateTeamUsageRateMultiplierRequest } from '@/types'
import TeamRateMultiplierModal from '@/components/user/usage/TeamRateMultiplierModal.vue'

const props = defineProps<{
  subjectUserId: number
  rows: SubjectSubscriptionGroup[]
  updateMultiplier?: (event: { subjectUserId: number; groupID: string; payload: UpdateTeamUsageRateMultiplierRequest }) => Promise<void>
}>()

const emit = defineEmits<{
  confirm: [event: { subjectUserId: number; groupID: string; payload: UpdateTeamUsageRateMultiplierRequest }]
}>()

const activeRow = ref<SubjectSubscriptionGroup | null>(null)
const draftMultiplier = ref<number | undefined>()
const submitting = ref(false)
const errorMessage = ref('')

const sortedRows = computed(() => [...props.rows].sort((a, b) => a.group_name.localeCompare(b.group_name)))

function formatCurrency(amount: number | null | undefined, unlimited = false) {
  if (unlimited) return '∞'
  if (amount == null) return '-'
  return `$${amount.toFixed(2)}`
}

function displayUsed(row: SubjectSubscriptionGroup, draft?: number) {
  if (draft == null || row.usage_value_basis === 'normalized_display_cost') {
    if (row.usage_value_basis !== 'normalized_display_cost' && row.effective_multiplier === 0) return 0
    return row.monthly_display_used_usd
  }
  if (draft === 0) return 0
  return row.monthly_usage_usd / draft
}

function displayQuota(row: SubjectSubscriptionGroup, draft?: number) {
  if (draft == null || row.usage_value_basis === 'normalized_display_cost') return row.monthly_effective_allowance_usd
  if (draft === 0) return null
  if (row.monthly_effective_allowance_usd == null) return null
  return row.monthly_effective_allowance_usd * row.effective_multiplier / draft
}

function quotaIsUnlimited(row: SubjectSubscriptionGroup, draft?: number) {
  if (row.usage_value_basis === 'normalized_display_cost') return !!row.monthly_effective_allowance_unlimited
  if (draft == null) return !!row.monthly_effective_allowance_unlimited
  return draft === 0
}

function rowDraft(row: SubjectSubscriptionGroup) {
  if (activeRow.value?.group_id !== row.group_id) return undefined
  return draftMultiplier.value
}

function openModal(row: SubjectSubscriptionGroup) {
  activeRow.value = row
  draftMultiplier.value = undefined
  errorMessage.value = ''
}

function closeModal() {
  if (submitting.value) return
  activeRow.value = null
  draftMultiplier.value = undefined
  errorMessage.value = ''
}

async function confirm(payload: UpdateTeamUsageRateMultiplierRequest) {
  if (!activeRow.value) return
  const event = { subjectUserId: props.subjectUserId, groupID: activeRow.value.group_id, payload }
  submitting.value = true
  errorMessage.value = ''
  try {
    if (props.updateMultiplier) {
      await props.updateMultiplier(event)
    } else {
      emit('confirm', event)
    }
    activeRow.value = null
    draftMultiplier.value = undefined
  } catch {
    errorMessage.value = 'Unable to update rate multiplier'
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <section v-if="props.rows.length > 0" class="rounded-lg border border-slate-200 bg-white shadow-sm">
    <div class="border-b border-slate-200 px-4 py-3">
      <h2 class="text-base font-semibold text-slate-950">Subscription groups</h2>
    </div>
    <div class="overflow-x-auto">
      <table class="min-w-full divide-y divide-slate-200 text-sm">
        <thead class="bg-slate-50 text-left text-xs font-medium uppercase text-slate-500">
          <tr>
            <th class="px-4 py-3">Group</th>
            <th class="px-4 py-3">Status</th>
            <th class="px-4 py-3">Multiplier</th>
            <th class="px-4 py-3">Used / Quota</th>
            <th class="px-4 py-3 text-right">Action</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-for="row in sortedRows" :key="row.group_id">
            <td class="px-4 py-3">
              <div class="font-medium text-slate-950">{{ row.group_name }}</div>
              <div class="text-xs text-slate-500">{{ row.platform }}</div>
            </td>
            <td class="px-4 py-3 text-slate-700">{{ row.subscription_status }}</td>
            <td class="px-4 py-3 text-slate-700">{{ row.effective_multiplier }}x</td>
            <td class="px-4 py-3 font-medium text-slate-950">
              {{ formatCurrency(displayUsed(row, rowDraft(row))) }} /
              {{ formatCurrency(displayQuota(row, rowDraft(row)), quotaIsUnlimited(row, rowDraft(row))) }}
            </td>
            <td class="px-4 py-3 text-right">
              <button
                type="button"
                class="rounded-md border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50"
                :data-testid="`edit-multiplier-${row.group_id}`"
                :disabled="!row.editable"
                @click="openModal(row)"
              >
                Edit
              </button>
            </td>
          </tr>
        </tbody>
      </table>
    </div>

    <TeamRateMultiplierModal
      :row="activeRow"
      :open="!!activeRow"
      :submitting="submitting"
      :error-message="errorMessage"
      @close="closeModal"
      @confirm="confirm"
      @draft-change="draftMultiplier = $event"
    />
  </section>
</template>
