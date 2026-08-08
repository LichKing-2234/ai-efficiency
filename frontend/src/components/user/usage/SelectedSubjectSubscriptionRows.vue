<script setup lang="ts">
import { computed, ref } from 'vue'
import type { SubjectSubscriptionGroup, UpdateTeamUsageRateMultiplierRequest } from '@/types'
import TeamRateMultiplierModal from '@/components/user/usage/TeamRateMultiplierModal.vue'
import { useMediaQuery } from '@/composables/useMediaQuery'
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
const isDesktop = useMediaQuery('(min-width: 768px)')

const sortedRows = computed(() => [...props.rows].sort((a, b) => a.group_name.localeCompare(b.group_name)))

function formatCurrency(amount: number | null | undefined, unlimited = false) {
  if (unlimited) return '∞'
  if (amount == null) return '-'
  return `$${amount.toFixed(2)}`
}

function openModal(row: unknown) {
  activeRow.value = row as SubjectSubscriptionGroup
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

function isMultiplierMetadataUnavailable(row: unknown) {
  return (row as SubjectSubscriptionGroup).multiplier_metadata_status === 'unavailable'
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
    <div v-if="isDesktop" data-subscription-list="desktop">
      <ElTable :data="sortedRows" row-key="group_id">
        <ElTableColumn :label="t('teamUsage.subscriptionGroup')" min-width="180">
          <template #default="{ row }">
              <div data-subscription-row class="font-medium text-slate-950">{{ row.group_name }}</div>
              <div class="text-xs text-slate-500">{{ row.platform }}</div>
          </template>
        </ElTableColumn>
        <ElTableColumn :label="t('teamUsage.subscriptionStatus')" min-width="110">
          <template #default="{ row }">{{ subscriptionStatusLabel(row.subscription_status) }}</template>
        </ElTableColumn>
        <ElTableColumn :label="t('teamUsage.multiplier')" min-width="120">
          <template #default="{ row }">
              <span
                v-if="isMultiplierMetadataUnavailable(row)"
                role="status"
                class="text-xs font-medium text-amber-700"
                :data-testid="`multiplier-metadata-warning-${row.group_id}`"
              >
                {{ t('teamUsage.multiplierUnavailable') }}
              </span>
              <template v-else>{{ row.effective_multiplier }}x</template>
          </template>
        </ElTableColumn>
        <ElTableColumn :label="t('teamUsage.usedOverQuota')" min-width="160">
          <template #default="{ row }">
              {{ formatCurrency(row.monthly_display_used_usd) }} /
              {{ formatCurrency(row.monthly_effective_allowance_usd, row.monthly_effective_allowance_unlimited) }}
          </template>
        </ElTableColumn>
        <ElTableColumn :label="t('teamUsage.memberAction')" min-width="110" align="right">
          <template #default="{ row }">
              <ElButton
                :data-testid="`edit-multiplier-${row.group_id}`"
                :disabled="!row.editable || isMultiplierMetadataUnavailable(row)"
                @click="openModal(row)"
              >
                {{ t('teamUsage.editMultiplier') }}
              </ElButton>
          </template>
        </ElTableColumn>
      </ElTable>
    </div>
    <div v-else data-subscription-list="mobile" class="space-y-3 p-3">
      <ElCard
        v-for="row in sortedRows"
        :key="row.group_id"
        data-subscription-row
        shadow="never"
      >
        <div class="flex items-start justify-between gap-3">
          <div class="min-w-0">
            <div class="break-words font-medium text-slate-950">{{ row.group_name }}</div>
            <div class="mt-1 text-xs text-slate-500">{{ row.platform }}</div>
          </div>
          <span class="shrink-0 text-xs text-slate-600">{{ subscriptionStatusLabel(row.subscription_status) }}</span>
        </div>
        <dl class="mt-4 grid grid-cols-2 gap-3 text-sm">
          <div>
            <dt class="text-xs font-medium uppercase text-slate-500">{{ t('teamUsage.multiplier') }}</dt>
            <dd class="mt-1 text-slate-900">
              <span
                v-if="isMultiplierMetadataUnavailable(row)"
                role="status"
                class="text-xs font-medium text-amber-700"
                :data-testid="`multiplier-metadata-warning-${row.group_id}`"
              >
                {{ t('teamUsage.multiplierUnavailable') }}
              </span>
              <template v-else>{{ row.effective_multiplier }}x</template>
            </dd>
          </div>
          <div>
            <dt class="text-xs font-medium uppercase text-slate-500">{{ t('teamUsage.usedOverQuota') }}</dt>
            <dd class="mt-1 text-slate-900">
              {{ formatCurrency(row.monthly_display_used_usd) }} /
              {{ formatCurrency(row.monthly_effective_allowance_usd, row.monthly_effective_allowance_unlimited) }}
            </dd>
          </div>
        </dl>
        <div class="mt-4 flex justify-end border-t border-slate-100 pt-3">
          <ElButton
            :data-testid="`edit-multiplier-${row.group_id}`"
            :disabled="!row.editable || isMultiplierMetadataUnavailable(row)"
            @click="openModal(row)"
          >
            {{ t('teamUsage.editMultiplier') }}
          </ElButton>
        </div>
      </ElCard>
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
