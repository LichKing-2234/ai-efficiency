<script setup lang="ts">
import { computed, ref } from 'vue'
import type { SubjectSubscriptionGroup, UpdateTeamUsageRateMultiplierRequest } from '@/types'
import TeamRateMultiplierModal from '@/components/user/usage/TeamRateMultiplierModal.vue'
import { useI18n } from '@/i18n'

const props = defineProps<{
  subjectUserId: number
  rows: SubjectSubscriptionGroup[]
  updateMultiplier?: (event: { subjectUserId: number; groupID: string; payload: UpdateTeamUsageRateMultiplierRequest }) => Promise<void>
}>()

const emit = defineEmits<{
  confirm: [event: { subjectUserId: number; groupID: string; payload: UpdateTeamUsageRateMultiplierRequest }]
}>()

const activeRow = ref<SubjectSubscriptionGroup | null>(null)
const submitting = ref(false)
const errorMessage = ref('')
const { t } = useI18n()

const sortedRows = computed(() => [...props.rows].sort((a, b) => a.group_name.localeCompare(b.group_name)))

function formatCurrency(amount: number | null | undefined, unlimited = false) {
  if (unlimited) return '∞'
  if (amount == null) return '-'
  return `$${amount.toFixed(2)}`
}

function openModal(row: SubjectSubscriptionGroup) {
  activeRow.value = row
  errorMessage.value = ''
}

function closeModal() {
  if (submitting.value) return
  activeRow.value = null
  errorMessage.value = ''
}

function subscriptionStatusLabel(status: string) {
  switch (status) {
    case 'active':
      return t('teamUsage.subscriptionStatusActive')
    case 'inactive':
      return t('teamUsage.subscriptionStatusInactive')
    case 'expired':
      return t('teamUsage.subscriptionStatusExpired')
    default:
      return status
  }
}

function isMultiplierMetadataUnavailable(row: SubjectSubscriptionGroup) {
  return row.multiplier_metadata_status === 'unavailable'
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
  } catch {
    errorMessage.value = t('teamUsage.updateMultiplierFailed')
  } finally {
    submitting.value = false
  }
}
</script>

<template>
  <section v-if="props.rows.length > 0" class="rounded-lg border border-slate-200 bg-white shadow-sm">
    <div class="border-b border-slate-200 px-4 py-3">
      <h2 class="text-base font-semibold text-slate-950">{{ t('teamUsage.subscriptionGroups') }}</h2>
    </div>
    <div class="overflow-x-auto">
      <table class="min-w-full divide-y divide-slate-200 text-sm">
        <thead class="bg-slate-50 text-left text-xs font-medium uppercase text-slate-500">
          <tr>
            <th class="px-4 py-3">{{ t('teamUsage.subscriptionGroup') }}</th>
            <th class="px-4 py-3">{{ t('teamUsage.subscriptionStatus') }}</th>
            <th class="px-4 py-3">{{ t('teamUsage.multiplier') }}</th>
            <th class="px-4 py-3">{{ t('teamUsage.usedOverQuota') }}</th>
            <th class="px-4 py-3 text-right">{{ t('teamUsage.memberAction') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-slate-100">
          <tr v-for="row in sortedRows" :key="row.group_id">
            <td class="px-4 py-3">
              <div class="font-medium text-slate-950">{{ row.group_name }}</div>
              <div class="text-xs text-slate-500">{{ row.platform }}</div>
            </td>
            <td class="px-4 py-3 text-slate-700">{{ subscriptionStatusLabel(row.subscription_status) }}</td>
            <td class="px-4 py-3 text-slate-700">
              <span
                v-if="isMultiplierMetadataUnavailable(row)"
                role="status"
                class="text-xs font-medium text-amber-700"
                :data-testid="`multiplier-metadata-warning-${row.group_id}`"
              >
                {{ t('teamUsage.multiplierUnavailable') }}
              </span>
              <template v-else>{{ row.effective_multiplier }}x</template>
            </td>
            <td class="px-4 py-3 font-medium text-slate-950">
              {{ formatCurrency(row.monthly_display_used_usd) }} /
              {{ formatCurrency(row.monthly_effective_allowance_usd, row.monthly_effective_allowance_unlimited) }}
            </td>
            <td class="px-4 py-3 text-right">
              <button
                type="button"
                class="rounded-md border border-slate-300 px-3 py-1.5 text-sm font-medium text-slate-700 hover:bg-slate-50 disabled:opacity-50"
                :data-testid="`edit-multiplier-${row.group_id}`"
                :disabled="!row.editable || isMultiplierMetadataUnavailable(row)"
                @click="openModal(row)"
              >
                {{ t('teamUsage.editMultiplier') }}
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
    />
  </section>
</template>
