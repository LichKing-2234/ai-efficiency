<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { ownerAccountsMessages } from '@/locales/ownerAccounts'
import { ownerAccountDateTime } from '@/utils/ownerAccounts'
import type { OwnerAccount, OwnerAccountOperation } from '@/types'

const props = defineProps<{
  open: boolean
  account: OwnerAccount
  operations: OwnerAccountOperation[]
  total: number
  page: number
  loading: boolean
  error: string
}>()

const emit = defineEmits<{
  close: []
  'page-change': [page: number]
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'ownerAccounts', ownerAccountsMessages)
const pageSize = 20
const showPagination = computed(() => props.total > pageSize)

function actionLabel(action: OwnerAccountOperation['action']) {
  switch (action) {
    case 'refresh_quota':
      return t('ownerAccounts.actionRefreshQuota')
    case 'refresh_referral':
      return t('ownerAccounts.actionRefreshReferral')
    case 'send_invitation':
      return t('ownerAccounts.actionSendInvitation')
    default:
      return t('ownerAccounts.actionResetCredit')
  }
}

function statusLabel(status: OwnerAccountOperation['status']) {
  switch (status) {
    case 'running':
      return t('ownerAccounts.statusRunning')
    case 'succeeded':
      return t('ownerAccounts.statusSucceeded')
    case 'failed':
      return t('ownerAccounts.statusFailed')
    default:
      return t('ownerAccounts.statusUnknown')
  }
}

function operationTime(operation: OwnerAccountOperation) {
  return ownerAccountDateTime(operation.created_at ?? operation.started_at, locale.value)
}
</script>

<template>
  <ElDrawer
    :model-value="open"
    append-to-body
    size="min(42rem, 100vw)"
    :title="t('ownerAccounts.historyTitle')"
    @close="emit('close')"
  >
    <div data-testid="owner-operation-history-drawer" class="space-y-3">
      <div v-if="loading" data-testid="owner-operation-history-loading" class="text-sm text-slate-500">
        {{ baseT('usageDashboard.loading') }}
      </div>

      <ElAlert
        v-else-if="error"
        data-testid="owner-operation-history-error"
        type="error"
        :closable="false"
        show-icon
        :title="t('ownerAccounts.historyLoadFailed')"
      />

      <p
        v-else-if="operations.length === 0"
        data-testid="owner-operation-history-empty"
        class="rounded-md border border-slate-200 bg-slate-50 p-4 text-sm text-slate-500"
      >
        {{ t('ownerAccounts.historyEmpty') }}
      </p>

      <ol v-else class="divide-y divide-slate-200 rounded-md border border-slate-200">
        <li
          v-for="operation in operations"
          :key="operation.id"
          :data-testid="`owner-operation-row-${operation.id}`"
          class="space-y-1 p-3 text-sm"
        >
          <div class="flex flex-wrap items-center justify-between gap-2">
            <span class="font-medium text-slate-900">{{ actionLabel(operation.action) }}</span>
            <span class="text-xs font-medium uppercase tracking-wide text-slate-500">{{ statusLabel(operation.status) }}</span>
          </div>
          <p class="text-xs text-slate-500">{{ operationTime(operation) }}</p>
          <p v-if="operation.result?.windows_reset != null" class="text-slate-700">
            {{ t('ownerAccounts.windowsReset', { count: operation.result.windows_reset }) }}
          </p>
          <p v-if="operation.invite_email" class="text-slate-700">
            {{ t('ownerAccounts.recipient', { email: operation.invite_email }) }}
          </p>
          <p v-if="operation.warning_code" class="text-amber-700">
            {{ t('ownerAccounts.warningCode', { code: operation.warning_code }) }}
          </p>
          <p v-if="operation.error_code" class="text-red-700">{{ operation.error_code }}</p>
        </li>
      </ol>

      <div v-if="showPagination && !loading && !error" class="flex justify-center">
        <ElPagination
          data-testid="owner-operation-history-pagination"
          :current-page="page"
          :page-size="pageSize"
          :total="total"
          small
          background
          layout="prev, pager, next"
          @current-change="emit('page-change', $event)"
        />
      </div>

      <div class="flex justify-end border-t border-slate-200 pt-3">
        <ElButton data-testid="owner-operation-history-close" size="small" @click="emit('close')">
          {{ t('ownerAccounts.historyClose') }}
        </ElButton>
      </div>
    </div>
  </ElDrawer>
</template>
