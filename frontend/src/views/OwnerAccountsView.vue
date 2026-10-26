<template>
  <section class="space-y-4" data-testid="owner-accounts-view">
    <div v-if="initialLoading" data-testid="owner-accounts-loading" class="rounded-lg border border-slate-200 bg-white p-6 text-sm text-slate-500">
      {{ t('ownerAccounts.loading') }}
    </div>

    <ElAlert
      v-else-if="loadError"
      data-testid="owner-accounts-load-error"
      type="error"
      :closable="false"
      show-icon
      :title="t('ownerAccounts.loadFailed')"
    >
      <ElButton class="mt-3" size="small" @click="loadPage">{{ t('ownerAccounts.retry') }}</ElButton>
    </ElAlert>

    <div
      v-else-if="accounts.length === 0"
      data-testid="owner-accounts-empty"
      class="rounded-lg border border-slate-200 bg-white p-8 text-center"
    >
      <p class="text-sm font-medium text-slate-900">{{ t('ownerAccounts.empty') }}</p>
      <p class="mt-1 text-sm text-slate-500">{{ t('ownerAccounts.emptyHint') }}</p>
    </div>

    <template v-else>
      <ElAlert
        v-if="!mutationsEnabled"
        data-testid="owner-accounts-mutations-disabled"
        type="info"
        :closable="false"
        show-icon
        :title="t('ownerAccounts.mutationsDisabled')"
      />

      <div v-if="loading" data-testid="owner-accounts-progress" class="text-sm text-slate-500">
        {{ t('ownerAccounts.loading') }}
      </div>

      <div v-if="!wide" data-testid="owner-accounts-cards" class="grid gap-4">
        <article
          v-for="account in accounts"
          :key="account.id"
          :data-testid="`owner-account-${account.id}`"
          class="space-y-4 rounded-lg border border-slate-200 bg-white p-4"
        >
          <header>
            <div class="flex flex-wrap items-start justify-between gap-2">
              <h2 class="text-base font-semibold text-slate-900">{{ account.name }}</h2>
              <span class="text-xs text-slate-500">{{ account.platform }} · {{ account.type }} · {{ account.status }}</span>
            </div>
            <p class="mt-1 break-words text-sm text-slate-500">{{ account.email }}</p>
            <p v-if="account.group_names.length" class="mt-1 text-xs text-slate-500">
              {{ t('ownerAccounts.groups') }}: {{ account.group_names.join(', ') }}
            </p>
          </header>

          <OwnerAccountUsageSection
            :account="account"
            :usage="usageFor(account.id)"
            :loading="usageLoading || sectionLoading[account.id]?.usage === true"
            :error="usageErrors[account.id] ?? ''"
            :cooldown-until="cooldownActive(account.id) ? (cooldowns[account.id] ?? '') : ''"
            :now="now"
          />

          <section :data-testid="`owner-account-quota-${account.id}`" class="space-y-2 border-t border-slate-100 pt-3">
            <h3 class="text-sm font-semibold text-slate-900">{{ t('ownerAccounts.quota') }}</h3>
            <p
              v-if="sectionLoading[account.id]?.usage"
              :data-testid="`owner-account-quota-loading-${account.id}`"
              class="text-sm text-slate-500"
            >
              {{ baseT('usageDashboard.loading') }}
            </p>
            <ElAlert
              v-if="quotaErrors[account.id]"
              :data-testid="`owner-account-quota-error-${account.id}`"
              type="warning"
              :closable="false"
              show-icon
              :title="t('ownerAccounts.sectionUnavailable')"
            />
            <template v-if="quotaFor(account)">
              <dl class="grid gap-x-4 gap-y-1 text-sm sm:grid-cols-2">
                <div class="flex justify-between gap-2">
                  <dt class="text-slate-500">{{ t('ownerAccounts.resetCredits') }}</dt>
                  <dd class="font-medium text-slate-900">
                    {{ t('ownerAccounts.resetCreditsRemaining', { count: quotaFor(account)!.reset_credits_remaining }) }}
                  </dd>
                </div>
                <div class="flex justify-between gap-2">
                  <dt class="text-slate-500">{{ t('ownerAccounts.codexCredits') }}</dt>
                  <dd class="font-medium text-slate-900">{{ codexCreditsLabel(quotaFor(account)!) }}</dd>
                </div>
                <div class="flex justify-between gap-2 sm:col-span-2">
                  <dt class="text-slate-500">{{ t('ownerAccounts.autoResetOn') }}</dt>
                  <dd class="font-medium text-slate-900">
                    {{ quotaFor(account)!.auto_reset_credit ? t('ownerAccounts.autoResetOn') : t('ownerAccounts.autoResetOff') }}
                  </dd>
                </div>
              </dl>
              <div class="text-xs text-slate-500">
                <p v-for="(expiry, index) in quotaFor(account)!.reset_credit_expiries" :key="`${expiry}-${index}`">
                  {{ t('ownerAccounts.creditExpiry', { time: ownerAccountDateTime(expiry, locale) }) }}
                </p>
                <p v-if="quotaFor(account)!.reset_credit_expiries.length === 0">
                  {{ t('ownerAccounts.creditNoExpiry') }}
                </p>
              </div>
              <p class="text-xs text-slate-500">
                {{ quotaFor(account)!.observed_at
                  ? t('ownerAccounts.observedAt', { time: ownerAccountDateTime(quotaFor(account)!.observed_at, locale) })
                  : t('ownerAccounts.observedUnknown') }}
              </p>
            </template>
          </section>

          <section :data-testid="`owner-account-referral-${account.id}`" class="space-y-2 border-t border-slate-100 pt-3">
            <h3 class="text-sm font-semibold text-slate-900">{{ t('ownerAccounts.referral') }}</h3>
            <p
              v-if="sectionLoading[account.id]?.usage"
              :data-testid="`owner-account-referral-loading-${account.id}`"
              class="text-sm text-slate-500"
            >
              {{ baseT('usageDashboard.loading') }}
            </p>
            <ElAlert
              v-if="referralErrors[account.id]"
              :data-testid="`owner-account-referral-error-${account.id}`"
              type="warning"
              :closable="false"
              show-icon
              :title="t('ownerAccounts.sectionUnavailable')"
            />
            <template v-if="referralFor(account)">
              <p class="text-sm font-medium text-slate-900">
                {{ referralFor(account)!.eligible ? t('ownerAccounts.referralEligible') : t('ownerAccounts.referralIneligible') }}
              </p>
              <p v-if="referralFor(account)!.program_name" class="text-sm text-slate-600">
                {{ t('ownerAccounts.referralProgram', { program: referralFor(account)!.program_name! }) }}
              </p>
              <p class="text-sm text-slate-600">
                {{ t('ownerAccounts.invitesRemaining', { count: referralFor(account)!.remaining_invites }) }}
              </p>
              <p class="text-xs text-slate-500">
                {{ referralFor(account)!.observed_at
                  ? t('ownerAccounts.observedAt', { time: ownerAccountDateTime(referralFor(account)!.observed_at, locale) })
                  : t('ownerAccounts.observedUnknown') }}
              </p>
            </template>
          </section>

          <section class="space-y-2 border-t border-slate-100 pt-3">
            <ElButton
              :data-testid="`owner-account-health-toggle-${account.id}`"
              size="small"
              text
              @click="toggleHealth(account.id)"
            >
              {{ healthOpen[account.id] ? t('ownerAccounts.hideHealth') : t('ownerAccounts.showHealth') }}
            </ElButton>
            <OwnerAccountHealthFacts v-if="healthOpen[account.id]" :account="account" />
          </section>

          <OwnerAccountActions
            :account="account"
            :quota="quotaFor(account)"
            :referral="referralFor(account)"
            :mutations-enabled="mutationsEnabled"
            :querying="sectionLoading[account.id]?.query === true"
            :cooldown-active="cooldownActive(account.id)"
            :unknown-reset="unknownFor(account, 'reset_credit')"
            :unknown-invite="unknownFor(account, 'send_invitation')"
            @query="queryAccount(account)"
            @reset="openResetDialog(account)"
            @invite="openInviteDialog(account)"
            @history="openHistory(account)"
          />

          <OwnerAccountOperationStatus
            :account-id="account.id"
            :operation="operations[account.id] ?? null"
            :interrupted="!!interruptedOperations[account.id]"
            :unknown-reset="unknownFor(account, 'reset_credit')"
            :unknown-invite="unknownFor(account, 'send_invitation')"
            @retry-poll="retryOperationPolling(account.id)"
            @history="historyForAccount(account.id)"
          />
        </article>
      </div>

      <ElTable
        v-else
        :data="accounts"
        row-key="id"
        data-testid="owner-accounts-table"
        class="w-full"
      >
        <ElTableColumn type="expand">
          <template #default="{ row }">
            <div class="p-2">
              <OwnerAccountHealthFacts :account="accountRow(row)" />
            </div>
          </template>
        </ElTableColumn>
        <ElTableColumn :label="t('ownerAccounts.account')" min-width="180">
          <template #default="{ row }">
            <div class="min-w-0">
              <p class="font-medium text-slate-900">{{ accountRow(row).name }}</p>
              <p class="break-words text-xs text-slate-500">{{ accountRow(row).email }}</p>
              <p v-if="accountRow(row).group_names.length" class="text-xs text-slate-500">{{ accountRow(row).group_names.join(', ') }}</p>
            </div>
          </template>
        </ElTableColumn>
        <ElTableColumn :label="t('ownerAccounts.window5h')" min-width="96">
          <template #default="{ row }">
            <div class="text-sm">
              <p class="font-medium text-slate-900">{{ usageWindowLabel(accountRow(row).id, 'five_hour') }}</p>
              <p class="text-xs text-slate-500">{{ usageWindowCountdown(accountRow(row).id, 'five_hour') }}</p>
            </div>
          </template>
        </ElTableColumn>
        <ElTableColumn :label="t('ownerAccounts.window7d')" min-width="96">
          <template #default="{ row }">
            <div class="text-sm">
              <p class="font-medium text-slate-900">{{ usageWindowLabel(accountRow(row).id, 'seven_day') }}</p>
              <p class="text-xs text-slate-500">{{ usageWindowCountdown(accountRow(row).id, 'seven_day') }}</p>
            </div>
          </template>
        </ElTableColumn>
        <ElTableColumn :label="t('ownerAccounts.resetCredits')" min-width="104">
          <template #default="{ row }">
            <div class="text-sm">
              <p class="font-medium text-slate-900">{{ quotaFor(accountRow(row))?.reset_credits_remaining ?? '-' }}</p>
              <p class="text-xs text-slate-500">{{ t('ownerAccounts.codexCredits') }}: {{ quotaFor(accountRow(row)) ? codexCreditsLabel(quotaFor(accountRow(row))!) : '-' }}</p>
              <p
                v-for="(expiry, index) in quotaFor(accountRow(row))?.reset_credit_expiries ?? []"
                :key="`${expiry}-${index}`"
                class="text-xs text-slate-500"
              >
                {{ t('ownerAccounts.creditExpiry', { time: ownerAccountDateTime(expiry, locale) }) }}
              </p>
            </div>
          </template>
        </ElTableColumn>
        <ElTableColumn :label="t('ownerAccounts.referral')" min-width="112">
          <template #default="{ row }">
            <div class="text-sm">
              <p class="font-medium text-slate-900">{{ referralFor(accountRow(row))?.eligible ? t('ownerAccounts.referralEligible') : t('ownerAccounts.referralIneligible') }}</p>
              <p class="text-xs text-slate-500">{{ t('ownerAccounts.invitesRemaining', { count: referralFor(accountRow(row))?.remaining_invites ?? 0 }) }}</p>
            </div>
          </template>
        </ElTableColumn>
        <ElTableColumn :label="t('ownerAccounts.actions')" min-width="232">
          <template #default="{ row }">
            <OwnerAccountActions
              :account="accountRow(row)"
              :quota="quotaFor(accountRow(row))"
              :referral="referralFor(accountRow(row))"
              :mutations-enabled="mutationsEnabled"
              :querying="sectionLoading[accountRow(row).id]?.query === true"
              :cooldown-active="cooldownActive(accountRow(row).id)"
              :unknown-reset="unknownFor(accountRow(row), 'reset_credit')"
              :unknown-invite="unknownFor(accountRow(row), 'send_invitation')"
              @query="queryAccount(accountRow(row))"
              @reset="openResetDialog(accountRow(row))"
              @invite="openInviteDialog(accountRow(row))"
              @history="openHistory(accountRow(row))"
            />
            <OwnerAccountOperationStatus
              class="mt-2"
              :account-id="accountRow(row).id"
              :operation="operations[accountRow(row).id] ?? null"
              :interrupted="!!interruptedOperations[accountRow(row).id]"
              :unknown-reset="unknownFor(accountRow(row), 'reset_credit')"
              :unknown-invite="unknownFor(accountRow(row), 'send_invitation')"
              @retry-poll="retryOperationPolling(accountRow(row).id)"
              @history="historyForAccount(accountRow(row).id)"
            />
          </template>
        </ElTableColumn>
      </ElTable>

      <footer class="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <p class="text-sm text-slate-500" data-testid="owner-accounts-range">
          {{ t('ownerAccounts.pageRange', { start: rangeStart, end: rangeEnd, total }) }}
        </p>
        <ElPagination
          v-if="showPagination"
          data-testid="owner-accounts-pagination"
          :current-page="page"
          :page-size="pageSize"
          :page-sizes="FULL_PAGE_SIZES"
          :total="total"
          :disabled="loading"
          background
          layout="sizes, prev, pager, next"
          @current-change="changePage"
          @size-change="changePageSize"
        />
      </footer>
    </template>

    <OwnerAccountHistoryDrawer
      v-if="historyTarget"
      :open="historyOpen"
      :account="historyTarget"
      :operations="historyOperations"
      :total="historyTotal"
      :page="historyPage"
      :loading="historyLoading"
      :error="historyError ? 'history-unavailable' : ''"
      @close="closeHistory"
      @page-change="loadHistoryPage"
    />

    <OwnerAccountInviteDialog
      v-if="inviteTarget"
      :open="inviteDialogOpen"
      :account="inviteTarget"
      :referral="referralFor(inviteTarget)"
      :submitting="inviteSubmitting"
      :error="inviteDialogError"
      @close="closeInviteDialog"
      @confirm="submitInvite"
    />

    <OwnerAccountResetDialog
      v-if="resetTarget"
      :open="resetDialogOpen"
      :account="resetTarget"
      :usage="usageFor(resetTarget.id)"
      :quota="quotaFor(resetTarget)"
      :submitting="resetSubmitting"
      :error="resetDialogError"
      @close="closeResetDialog"
      @confirm="submitReset"
    />
  </section>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { createFeatureTranslator } from '@/utils/featureI18n'
import {
  getOwnerAccountUsage,
  listOwnerAccounts,
  createOwnerAccountInvitation,
  createOwnerAccountReset,
  getOwnerAccountOperation,
  listOwnerAccountOperations,
  refreshOwnerAccountQuota,
  refreshOwnerAccountReferral,
  refreshOwnerAccountUsage,
} from '@/api/ownerAccounts'
import { useI18n } from '@/i18n'
import { ownerAccountsMessages } from '@/locales/ownerAccounts'
import { useWideContentLayout } from '@/composables/useMediaQuery'
import { FULL_PAGE_SIZES, fullPageSize, positivePage } from '@/utils/pagination'
import {
  ownerAccountCountdown,
  ownerAccountDateTime,
  ownerAccountUtilization,
} from '@/utils/ownerAccounts'
import OwnerAccountUsageSection from '@/components/owner-accounts/OwnerAccountUsageSection.vue'
import OwnerAccountHealthFacts from '@/components/owner-accounts/OwnerAccountHealthFacts.vue'
import OwnerAccountActions from '@/components/owner-accounts/OwnerAccountActions.vue'
import OwnerAccountOperationStatus from '@/components/owner-accounts/OwnerAccountOperationStatus.vue'
import OwnerAccountResetDialog from '@/components/owner-accounts/OwnerAccountResetDialog.vue'
import OwnerAccountInviteDialog from '@/components/owner-accounts/OwnerAccountInviteDialog.vue'
import OwnerAccountHistoryDrawer from '@/components/owner-accounts/OwnerAccountHistoryDrawer.vue'
import type {
  OwnerAccount,
  OwnerAccountOperation,
  OwnerAccountQuota,
  OwnerAccountReferral,
  OwnerAccountUsage,
} from '@/types'

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'ownerAccounts', ownerAccountsMessages)
const route = useRoute()
const router = useRouter()
const wide = useWideContentLayout()

const accounts = ref<OwnerAccount[]>([])
const total = ref(0)
const mutationsEnabled = ref(false)
const loading = ref(false)
const loadError = ref(false)
const usageLoading = ref(false)
const usageByAccount = ref<Record<number, OwnerAccountUsage>>({})
const usageErrors = reactive<Record<number, string>>({})
const quotaErrors = reactive<Record<number, string>>({})
const referralErrors = reactive<Record<number, string>>({})
const localQuota = reactive<Record<number, OwnerAccountQuota>>({})
const localReferral = reactive<Record<number, OwnerAccountReferral>>({})
const sectionLoading = reactive<Record<number, { usage: boolean; query: boolean }>>({})
const cooldowns = reactive<Record<number, string>>({})
const healthOpen = reactive<Record<number, boolean>>({})
type OperationView = {
  id: number
  action: 'refresh_quota' | 'refresh_referral' | 'reset_credit' | 'send_invitation'
  status: 'running' | 'succeeded' | 'failed' | 'unknown'
  windowsReset?: number
  sent?: boolean
  errorCode?: string
}
const operations = reactive<Record<number, OperationView>>({})
const interruptedOperations = reactive<Record<number, { operationID: number; action: 'reset_credit' | 'send_invitation' }>>({})
const unknownBlocks = reactive<Record<number, { reset?: boolean; invite?: boolean }>>({})
const resetTarget = ref<OwnerAccount | null>(null)
const resetDialogOpen = ref(false)
const resetSubmitting = ref(false)
const resetDialogError = ref('')
const inviteTarget = ref<OwnerAccount | null>(null)
const inviteDialogOpen = ref(false)
const inviteSubmitting = ref(false)
const inviteDialogError = ref('')
const historyTarget = ref<OwnerAccount | null>(null)
const historyOpen = ref(false)
const historyOperations = ref<OwnerAccountOperation[]>([])
const historyTotal = ref(0)
const historyPage = ref(1)
const historyLoading = ref(false)
const historyError = ref(false)
let historyGeneration = 0
const pollTokens: Record<number, number> = {}
const pollTimers: Record<number, ReturnType<typeof setTimeout>> = {}
const now = ref(Date.now())
// Element Plus types table slot rows as `Record<PropertyKey, any>`, not the data
// item type. Narrow once at that single boundary instead of scattering casts
// through every cell.
function accountRow(row: Record<PropertyKey, any>): OwnerAccount {
  return row as OwnerAccount
}

const page = computed(() => positivePage(route.query.page))
const pageSize = computed(() => fullPageSize(route.query.page_size))
const initialLoading = computed(() => loading.value && accounts.value.length === 0)
const showPagination = computed(() => total.value > pageSize.value)
const rangeStart = computed(() => (accounts.value.length === 0 ? 0 : (page.value - 1) * pageSize.value + 1))
const rangeEnd = computed(() => (accounts.value.length === 0 ? 0 : rangeStart.value + accounts.value.length - 1))
let requestGeneration = 0
let clock: ReturnType<typeof setInterval> | undefined


function usageFor(accountID: number) {
  return usageByAccount.value[accountID] ?? null
}

function quotaFor(account: OwnerAccount) {
  return localQuota[account.id] ?? account.quota
}

function referralFor(account: OwnerAccount) {
  return localReferral[account.id] ?? account.referral
}

function serverUnknownFor(account: OwnerAccount, action: 'reset_credit' | 'send_invitation') {
  return (account.blocked_actions ?? []).some((item) => item.action === action && item.reason === 'unknown')
}

function unknownFor(account: OwnerAccount, action: 'reset_credit' | 'send_invitation') {
  const local = action === 'reset_credit' ? unknownBlocks[account.id]?.reset : unknownBlocks[account.id]?.invite
  return !!local || serverUnknownFor(account, action)
}

function codexCreditsLabel(quota: OwnerAccountQuota) {
  if (quota.codex_credits_unlimited) return t('ownerAccounts.codexUnlimited')
  if (quota.codex_credits_balance == null) return t('ownerAccounts.codexNoCredits')
  if (quota.codex_credits_balance <= 0) return t('ownerAccounts.codexNoCredits')
  return String(quota.codex_credits_balance)
}

function cooldownActive(accountID: number) {
  const value = cooldowns[accountID]
  if (!value) return false
  const time = new Date(value).getTime()
  return !Number.isNaN(time) && time > now.value
}

function usageWindowLabel(accountID: number, window: 'five_hour' | 'seven_day') {
  const usage = usageByAccount.value[accountID]
  return usage ? ownerAccountUtilization(usage[window].utilization_percent) : usageLoading.value ? '…' : '-'
}

function usageWindowCountdown(accountID: number, window: 'five_hour' | 'seven_day') {
  const usage = usageByAccount.value[accountID]
  const resetsAt = usage?.[window].resets_at
  if (!resetsAt) return ''
  return ownerAccountCountdown(resetsAt, now.value, t)
}

function toggleHealth(accountID: number) {
  healthOpen[accountID] = !healthOpen[accountID]
}

function clearAccountState() {
  usageByAccount.value = {}
  for (const key of Object.keys(usageErrors)) delete usageErrors[Number(key)]
  for (const key of Object.keys(quotaErrors)) delete quotaErrors[Number(key)]
  for (const key of Object.keys(referralErrors)) delete referralErrors[Number(key)]
  for (const key of Object.keys(localQuota)) delete localQuota[Number(key)]
  for (const key of Object.keys(localReferral)) delete localReferral[Number(key)]
}

async function loadUsage(accountIDs: number[], generation: number) {
  if (accountIDs.length === 0) return
  usageLoading.value = true
  try {
    const response = await getOwnerAccountUsage({ account_ids: accountIDs })
    if (generation !== requestGeneration) return
    const data = response.data.data
    for (const item of data?.items ?? []) usageByAccount.value[item.account_id] = item.usage
    for (const item of data?.errors ?? []) usageErrors[item.account_id] = item.code
  } catch {
    if (generation !== requestGeneration) return
    for (const accountID of accountIDs) usageErrors[accountID] = 'usage-unavailable'
  } finally {
    if (generation === requestGeneration) usageLoading.value = false
  }
}

async function loadPage() {
  const generation = ++requestGeneration
  loading.value = true
  loadError.value = false
  try {
    const response = await listOwnerAccounts({ page: page.value, page_size: pageSize.value })
    if (generation !== requestGeneration) return
    const data = response.data.data
    clearAccountState()
    accounts.value = data?.items ?? []
    total.value = data?.total ?? 0
    mutationsEnabled.value = data?.mutations_enabled === true
    for (const account of accounts.value) {
      const serverCooldown = account.refresh_cooldown_until ?? ''
      if (!cooldownActive(account.id)) cooldowns[account.id] = serverCooldown
      if (!serverUnknownFor(account, 'reset_credit')) unknownBlocks[account.id] = { ...unknownBlocks[account.id], reset: false }
      if (!serverUnknownFor(account, 'send_invitation')) unknownBlocks[account.id] = { ...unknownBlocks[account.id], invite: false }
    }
    await loadUsage(accounts.value.map((account) => account.id), generation)
  } catch {
    if (generation !== requestGeneration) return
    accounts.value = []
    total.value = 0
    loadError.value = true
  } finally {
    if (generation === requestGeneration) loading.value = false
  }
}

function errorCode(error: unknown): string {
  const response = (error as {
    response?: {
      data?: {
        details?: { error_code?: string }
        data?: { error_code?: string }
        message?: string
      }
    }
  })?.response
  return response?.data?.details?.error_code
    ?? response?.data?.data?.error_code
    ?? response?.data?.message
    ?? ''
}

function cooldownFromError(error: unknown): string {
  const response = (error as {
    response?: {
      data?: {
        details?: { retry_after_seconds?: number; cooldown_until?: string }
        data?: { cooldown_until?: string }
      }
    }
  })?.response
  const explicit = response?.data?.data?.cooldown_until ?? response?.data?.details?.cooldown_until
  if (explicit) return explicit
  const retryAfter = response?.data?.details?.retry_after_seconds
  const seconds = typeof retryAfter === 'number' && retryAfter > 0 ? retryAfter : 30
  return new Date(Date.now() + seconds * 1000).toISOString()
}

async function queryAccount(account: OwnerAccount) {
  if (sectionLoading[account.id]?.query || cooldownActive(account.id)) return
  sectionLoading[account.id] = { usage: true, query: true }
  delete usageErrors[account.id]
  delete quotaErrors[account.id]
  delete referralErrors[account.id]
  const [usageResult, quotaResult, referralResult] = await Promise.allSettled([
    refreshOwnerAccountUsage(account.id),
    refreshOwnerAccountQuota(account.id),
    refreshOwnerAccountReferral(account.id),
  ])

  let refreshed = false
  if (usageResult.status === 'fulfilled') {
    usageByAccount.value[account.id] = usageResult.value.data.data!.usage
    refreshed = true
  } else if (errorCode(usageResult.reason) === 'refresh-cooldown') {
    cooldowns[account.id] = cooldownFromError(usageResult.reason)
  } else {
    usageErrors[account.id] = errorCode(usageResult.reason) || 'usage-unavailable'
  }

  if (quotaResult.status === 'fulfilled') {
    localQuota[account.id] = quotaResult.value.data.data!.quota
    refreshed = true
  } else if (errorCode(quotaResult.reason) === 'refresh-cooldown') {
    cooldowns[account.id] = cooldownFromError(quotaResult.reason)
  } else {
    quotaErrors[account.id] = errorCode(quotaResult.reason) || 'section-unavailable'
  }

  if (referralResult.status === 'fulfilled') {
    localReferral[account.id] = referralResult.value.data.data!.referral
    refreshed = true
  } else if (errorCode(referralResult.reason) === 'refresh-cooldown') {
    cooldowns[account.id] = cooldownFromError(referralResult.reason)
  } else {
    referralErrors[account.id] = errorCode(referralResult.reason) || 'section-unavailable'
  }

  sectionLoading[account.id] = { usage: false, query: false }
  if (refreshed) await loadPage()
}

function updateQuery(next: { page?: number; page_size?: number }) {
  const query: Record<string, string> = {}
  const nextPage = next.page ?? page.value
  const nextPageSize = next.page_size ?? pageSize.value
  if (nextPage > 1) query.page = String(nextPage)
  if (nextPageSize !== 20) query.page_size = String(nextPageSize)
  void router.replace({ query })
}

function changePage(nextPage: number) {
  if (nextPage === page.value) return
  updateQuery({ page: nextPage })
}

function changePageSize(nextPageSize: number) {
  if (nextPageSize === pageSize.value) return
  updateQuery({ page: 1, page_size: nextPageSize })
}

function openResetDialog(account: OwnerAccount) {
  resetTarget.value = account
  resetDialogError.value = ''
  resetDialogOpen.value = true
}

function closeResetDialog() {
  resetDialogOpen.value = false
  resetSubmitting.value = false
}

function openInviteDialog(account: OwnerAccount) {
  inviteTarget.value = account
  inviteDialogError.value = ''
  inviteDialogOpen.value = true
}

function closeInviteDialog() {
  inviteDialogOpen.value = false
  inviteSubmitting.value = false
}

async function submitInvite(payload: { email: string; program_id: string | null; consent: boolean }) {
  const target = inviteTarget.value
  if (!target || inviteSubmitting.value) return
  inviteSubmitting.value = true
  inviteDialogError.value = ''
  try {
    const response = await createOwnerAccountInvitation(target.id, payload)
    const operationID = response.data.data?.operation_id
    if (!operationID) throw new Error('missing operation id')
    inviteDialogOpen.value = false
    inviteTarget.value = null
    operations[target.id] = { id: operationID, action: 'send_invitation', status: 'running' }
    startOperationPolling(target.id, operationID, 'send_invitation')
  } catch (error) {
    inviteDialogError.value = operationErrorMessage(error)
  } finally {
    inviteSubmitting.value = false
  }
}

function openHistory(account: OwnerAccount) {
  historyTarget.value = account
  historyOpen.value = true
  historyOperations.value = []
  historyTotal.value = 0
  historyError.value = false
  void loadHistoryPage(1)
}

function closeHistory() {
  historyOpen.value = false
  historyTarget.value = null
  historyGeneration++
}

async function loadHistoryPage(nextPage: number) {
  const target = historyTarget.value
  if (!target) return
  const generation = ++historyGeneration
  historyPage.value = nextPage
  historyLoading.value = true
  historyError.value = false
  try {
    const response = await listOwnerAccountOperations(target.id, { page: nextPage, page_size: 20 })
    if (generation !== historyGeneration) return
    const data = response.data.data
    historyOperations.value = data?.items ?? []
    historyTotal.value = data?.total ?? 0
  } catch {
    if (generation !== historyGeneration) return
    historyOperations.value = []
    historyTotal.value = 0
    historyError.value = true
  } finally {
    if (generation === historyGeneration) historyLoading.value = false
  }
}

function markUnknown(accountID: number, action: 'reset_credit' | 'send_invitation', operationID: number) {
  operations[accountID] = { id: operationID, action, status: 'unknown' }
  unknownBlocks[accountID] = {
    ...unknownBlocks[accountID],
    ...(action === 'reset_credit' ? { reset: true } : { invite: true }),
  }
}

function applyOperationResult(
  accountID: number,
  action: 'reset_credit' | 'send_invitation',
  operation: {
    id: number
    status: 'running' | 'succeeded' | 'failed' | 'unknown'
    result?: { windows_reset?: number; sent?: boolean } | null
    error_code?: string | null
  },
) {
  delete interruptedOperations[accountID]
  if (operation.status === 'unknown') {
    markUnknown(accountID, action, operation.id)
    return
  }
  operations[accountID] = {
    id: operation.id,
    action,
    status: operation.status,
    windowsReset: operation.result?.windows_reset,
    sent: operation.result?.sent,
    errorCode: operation.error_code ?? undefined,
  }
  if (operation.status === 'succeeded' || operation.status === 'failed') {
    void loadPage()
  }
}

// A poll that times out or fails is not a server-unknown outcome: the operation
// may still be running, so the console shows a non-blocking interrupted state and
// lets the owner retry polling or inspect the history instead of blocking a spend.
function markPollInterrupted(
  accountID: number,
  operationID: number,
  action: 'reset_credit' | 'send_invitation',
) {
  operations[accountID] = { id: operationID, action, status: 'running' }
  interruptedOperations[accountID] = { operationID, action }
}

function startOperationPolling(
  accountID: number,
  operationID: number,
  action: 'reset_credit' | 'send_invitation',
) {
  const token = (pollTokens[accountID] ?? 0) + 1
  pollTokens[accountID] = token
  delete interruptedOperations[accountID]
  const startedAt = Date.now()
  const tick = async () => {
    if (pollTokens[accountID] !== token) return
    if (Date.now() - startedAt >= 90000) {
      markPollInterrupted(accountID, operationID, action)
      return
    }
    try {
      const response = await getOwnerAccountOperation(operationID)
      if (pollTokens[accountID] !== token) return
      const operation = response.data.data
      if (!operation) {
        markPollInterrupted(accountID, operationID, action)
        return
      }
      if (operation.status === 'running') {
        pollTimers[accountID] = setTimeout(tick, 2000)
        return
      }
      applyOperationResult(accountID, action, operation)
    } catch {
      if (pollTokens[accountID] !== token) return
      markPollInterrupted(accountID, operationID, action)
    }
  }
  pollTimers[accountID] = setTimeout(tick, 2000)
}

function retryOperationPolling(accountID: number) {
  const interrupted = interruptedOperations[accountID]
  const operation = operations[accountID]
  const operationID = interrupted?.operationID ?? operation?.id
  const action = interrupted?.action ?? operation?.action
  if (!operationID || !action) return
  startOperationPolling(accountID, operationID, action)
}

function historyForAccount(accountID: number) {
  const account = accounts.value.find((item) => item.id === accountID)
  if (account) openHistory(account)
}

async function submitReset() {
  const target = resetTarget.value
  if (!target || resetSubmitting.value) return
  resetSubmitting.value = true
  resetDialogError.value = ''
  try {
    const response = await createOwnerAccountReset(target.id, { confirm: true })
    const operationID = response.data.data?.operation_id
    if (!operationID) throw new Error('missing operation id')
    resetDialogOpen.value = false
    resetTarget.value = null
    operations[target.id] = { id: operationID, action: 'reset_credit', status: 'running' }
    startOperationPolling(target.id, operationID, 'reset_credit')
  } catch (error) {
    resetDialogError.value = operationErrorMessage(error)
  } finally {
    resetSubmitting.value = false
  }
}

function operationErrorMessage(error: unknown) {
  const code = errorCode(error)
  const mapping: Record<string, Parameters<typeof t>[0]> = {
    'refresh-cooldown': 'ownerAccounts.errorRefreshCooldown',
    'reset-freshness-required': 'ownerAccounts.errorFreshness',
    'invite-freshness-required': 'ownerAccounts.errorInviteFreshness',
    'compliance-ack-required': 'ownerAccounts.errorComplianceAck',
    'credits-expired': 'ownerAccounts.errorCreditsExpired',
    'actions-disabled': 'ownerAccounts.errorActionsDisabled',
    'consent-required': 'ownerAccounts.errorConsentRequired',
    'invalid-email': 'ownerAccounts.errorInvalidEmail',
    'program-changed': 'ownerAccounts.errorProgramChanged',
    'referral-unavailable': 'ownerAccounts.errorReferralUnavailable',
    'mutation-in-flight': 'ownerAccounts.errorMutationInFlight',
    busy: 'ownerAccounts.errorBusy',
    'mutation-rate-limited': 'ownerAccounts.errorRateLimited',
    'upstream-unavailable': 'ownerAccounts.errorUpstream',
    'capability-unsupported': 'ownerAccounts.errorCapability',
  }
  return mapping[code] ? t(mapping[code]) : t('ownerAccounts.errorGeneric')
}

watch(
  () => [route.query.page, route.query.page_size],
  () => { void loadPage() },
)

onMounted(() => {
  clock = setInterval(() => { now.value = Date.now() }, 1000)
  void loadPage()
})

onBeforeUnmount(() => {
  if (clock) clearInterval(clock)
  for (const timer of Object.values(pollTimers)) clearTimeout(timer)
  requestGeneration++
})
</script>
