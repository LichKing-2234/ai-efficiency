<script setup lang="ts">
import { computed, onMounted, reactive, ref, watch } from 'vue'
import { ElMessage } from 'element-plus'
import AppLayout from '@/components/AppLayout.vue'
import { useMediaQuery } from '@/composables/useMediaQuery'
import TeamAccountDetailDialog from '@/components/team-accounts/TeamAccountDetailDialog.vue'
import TeamAccountUserPicker from '@/components/team-accounts/TeamAccountUserPicker.vue'
import TeamAccountDepartmentPicker from '@/components/team-accounts/TeamAccountDepartmentPicker.vue'
import TeamAccountGroupShuttle from '@/components/team-accounts/TeamAccountGroupShuttle.vue'
import TeamAccountTransferDialog from '@/components/team-accounts/TeamAccountTransferDialog.vue'
import { useTeamAccountLogin } from '@/composables/useTeamAccountLogin'
import { listAdminUserSubscriptionOptions } from '@/api/adminUsers'
import {
  createAdminTeamAccount,
  getAdminTeamAccountGroupOptions,
  listAdminTeamAccounts,
  setAdminTeamAccountStatus,
} from '@/api/teamAccounts'
import { useI18n } from '@/i18n'
import { teamAccountsMessages } from '@/locales/teamAccounts'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { FULL_PAGE_SIZES } from '@/utils/pagination'
import {
  formatTeamAccountTimestamp,
  teamAccountErrorMessage,
  teamAccountStatusLabelKey,
  teamAccountStatusTagType,
} from '@/utils/teamAccounts'
import type { AdminUser, TeamAccountStatus, TeamAccountSummary } from '@/types'

interface RelayGroupOption {
  providerId: number
  providerLabel: string
  groupId: number
  name: string
  platform: string
}

type StatusFilter = 'all' | TeamAccountStatus

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'teamAccounts.', teamAccountsMessages)
const desktopPagination = useMediaQuery('(min-width: 768px)', true)
const accounts = ref<TeamAccountSummary[]>([])
const loading = ref(false)
const error = ref('')
const q = ref('')
const statusFilter = ref<StatusFilter>('all')
const page = ref(1)
const pageSize = ref(FULL_PAGE_SIZES[0])
const createOpen = ref(false)
const createForm = reactive<{ name: string; departmentExternalId: string; ownerUserID: number | null }>({ name: '', departmentExternalId: '', ownerUserID: null })
const creating = ref(false)
const createError = ref('')
const detailOpen = ref(false)
const selectedAccountID = ref<number | null>(null)
const selectedAccount = ref<TeamAccountSummary | null>(null)
const transferOpen = ref(false)
const transferAccount = ref<TeamAccountSummary | null>(null)
const statusUpdatingID = ref<number | null>(null)
const statusDialogOpen = ref(false)
const statusTarget = ref<TeamAccountSummary | null>(null)
const statusNext = ref<TeamAccountStatus>('suspended')
const statusConfirmText = ref('')
const { loginAs, pendingAccountID } = useTeamAccountLogin()
const statusConfirmed = computed(() => {
  const target = statusTarget.value
  if (!target) return false
  return statusConfirmText.value.trim().toLowerCase() === target.name.trim().toLowerCase()
})

function openStatusDialog(account: TeamAccountSummary) {
  if (statusUpdatingID.value !== null) return
  statusTarget.value = account
  statusNext.value = account.status === 'active' ? 'suspended' : 'active'
  statusConfirmText.value = ''
  statusDialogOpen.value = true
}

function closeStatusDialog() {
  if (statusUpdatingID.value !== null) return
  statusDialogOpen.value = false
}

const groupOptions = ref<RelayGroupOption[]>([])
const groupsLoading = ref(false)
const createAdditionalGroupIDs = ref<number[]>([])
const createGroupOptions = ref<{ id: number; name: string; platform: string }[]>([])
const createDefaultGroupIDs = ref<number[]>([])
const createGrantableGroupIDs = ref<number[]>([])
const createOwnerGroupIDs = ref<number[]>([])

const statusOptions = computed<TeamAccountStatus[]>(() => ['active', 'suspended', 'orphaned'])

const filteredAccounts = computed(() => {
  const query = q.value.trim().toLowerCase()
  return accounts.value.filter((account) => {
    if (statusFilter.value !== 'all' && account.status !== statusFilter.value) return false
    if (!query) return true
    return [
      account.name,
      account.department_display_path,
      account.department_external_id,
      account.relay_username,
    ].some((value) => (value ?? '').toLowerCase().includes(query))
  })
})

const total = computed(() => filteredAccounts.value.length)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
const pageStart = computed(() => (total.value === 0 ? 0 : (page.value - 1) * pageSize.value + 1))
const pageEnd = computed(() => Math.min(total.value, page.value * pageSize.value))
const pageAccounts = computed(() => {
  const start = (page.value - 1) * pageSize.value
  return filteredAccounts.value.slice(start, start + pageSize.value)
})
const showPagination = computed(() => total.value > pageSize.value)

watch([q, statusFilter], () => {
  page.value = 1
})

watch(totalPages, (pages) => {
  if (page.value > pages) page.value = pages
})

onMounted(() => {
  void loadAccounts()
})

async function loadAccounts() {
  loading.value = true
  error.value = ''
  try {
    const response = await listAdminTeamAccounts()
    accounts.value = response.data.data?.items ?? []
  } catch (e: unknown) {
    accounts.value = []
    error.value = teamAccountErrorMessage(e, t('teamAccounts.admin.loadFailed'))
  } finally {
    loading.value = false
  }
}

function onCreateDepartmentChange() {
  // Picking a user fills the department programmatically and does not emit this
  // event, so any change here comes from the operator: drop the user and their
  // group prefill.
  createForm.ownerUserID = null
  createAdditionalGroupIDs.value = [...createDefaultGroupIDs.value]
}

async function onOwnerSelected(user: AdminUser | null) {
  if (!user) return
  const departmentId = user.department?.external_id
  if (departmentId && createForm.departmentExternalId !== departmentId) {
    createForm.departmentExternalId = departmentId
  }
  // The account starts with the owner's own access groups plus the defaults.
  await loadCreateGroupOptions(user.id)
  createAdditionalGroupIDs.value = Array.from(new Set([
    ...createDefaultGroupIDs.value,
    ...createOwnerGroupIDs.value,
  ]))
}

async function loadCreateGroupOptions(ownerUserID?: number) {
  try {
    const response = await getAdminTeamAccountGroupOptions(ownerUserID)
    const options = response.data.data
    createGroupOptions.value = options?.groups ?? []
    createDefaultGroupIDs.value = options?.default_group_ids ?? []
    createGrantableGroupIDs.value = options?.grantable_group_ids ?? []
    createOwnerGroupIDs.value = options?.owner_group_ids ?? []
  } catch {
    createGroupOptions.value = []
    createDefaultGroupIDs.value = []
    createGrantableGroupIDs.value = []
    createOwnerGroupIDs.value = []
  }
}

async function openCreate() {
  void ensureGroupOptions()
  createAdditionalGroupIDs.value = []
  createForm.name = ''
  createForm.departmentExternalId = ''
  createForm.ownerUserID = null
  createError.value = ''
  createOpen.value = true
  await loadCreateGroupOptions()
  // Administrators start from the account defaults; extra groups are granted
  // by hand through the shuttle.
  createAdditionalGroupIDs.value = [...createDefaultGroupIDs.value]
}

function closeCreate() {
  if (creating.value) return
  createOpen.value = false
}

async function submitCreate() {
  if (creating.value) return
  if (!createForm.name.trim()) {
    createError.value = t('teamAccounts.nameRequired')
    return
  }
  const ownerUserID = createForm.ownerUserID
  if (ownerUserID === null || ownerUserID <= 0) {
    createError.value = t('teamAccounts.admin.ownerRequired')
    return
  }
  creating.value = true
  createError.value = ''
  try {
    await createAdminTeamAccount({
      name: createForm.name.trim(),
      department_external_id: createForm.departmentExternalId.trim() || undefined,
      owner_user_id: ownerUserID,
      additional_group_ids: createAdditionalGroupIDs.value.filter((id) => !createDefaultGroupIDs.value.includes(id)),
    })
    ElMessage.success(t('teamAccounts.admin.createSuccess'))
    createOpen.value = false
    await loadAccounts()
  } catch (e: unknown) {
    const message = teamAccountErrorMessage(e, t('teamAccounts.admin.createFailed'))
    if (message.includes('already has a team account')) {
      createError.value = t('teamAccounts.departmentTaken')
    } else if (message.includes('username already exists') || message.includes('email already exists') || message.includes('already exists in relay') || message.includes('reserved by another team account')) {
      createError.value = t('teamAccounts.usernameTaken')
    } else {
      createError.value = message
    }
  } finally {
    creating.value = false
  }
}

function openDetail(account: TeamAccountSummary) {
  selectedAccount.value = account
  selectedAccountID.value = account.id
  detailOpen.value = true
}

function openTransfer(account: TeamAccountSummary) {
  transferAccount.value = account
  transferOpen.value = true
}

function handleTransferred() {
  detailOpen.value = false
  transferOpen.value = false
  void loadAccounts()
}

async function submitStatusChange() {
  const target = statusTarget.value
  if (!target || !statusConfirmed.value || statusUpdatingID.value !== null) return
  const nextStatus = statusNext.value
  statusUpdatingID.value = target.id
  try {
    await setAdminTeamAccountStatus(target.id, nextStatus)
    ElMessage.success(t('teamAccounts.admin.statusUpdated'))
    statusDialogOpen.value = false
    await loadAccounts()
  } catch (e: unknown) {
    ElMessage.error(teamAccountErrorMessage(e, t('teamAccounts.admin.statusUpdateFailed')))
  } finally {
    statusUpdatingID.value = null
  }
}

async function ensureGroupOptions() {
  if (groupOptions.value.length > 0 || groupsLoading.value) return
  groupsLoading.value = true
  try {
    const response = await listAdminUserSubscriptionOptions()
    const providers = response.data.data?.providers ?? []
    groupOptions.value = providers.flatMap((provider) => provider.groups.map((group) => ({
      providerId: provider.id,
      providerLabel: provider.display_name || provider.name,
      groupId: Number(group.group_id),
      name: group.group_name,
      platform: group.platform,
    })))
  } catch {
    groupOptions.value = []
  } finally {
    groupsLoading.value = false
  }
}

function changePage(nextPage: number) {
  if (nextPage < 1 || nextPage > totalPages.value) return
  page.value = nextPage
}

function changePageSize(nextPageSize: number) {
  pageSize.value = nextPageSize
  page.value = 1
}

function statusActionLabel(account: TeamAccountSummary) {
  return account.status === 'active' ? t('teamAccounts.admin.suspend') : t('teamAccounts.admin.restore')
}

function statusActionType(account: TeamAccountSummary): 'warning' | 'success' {
  return account.status === 'active' ? 'warning' : 'success'
}
</script>

<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex flex-col gap-3 lg:flex-row lg:items-start lg:justify-between">
        <div class="min-w-0">
          <h1 class="text-2xl font-bold text-gray-900">{{ t('teamAccounts.admin.title') }}</h1>
          <p class="mt-1 text-sm text-gray-500">{{ t('teamAccounts.admin.subtitle') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <ElButton data-testid="admin-team-account-create-open" type="primary" @click="openCreate">
            {{ t('teamAccounts.admin.create') }}
          </ElButton>
        </div>
      </div>

      <ElAlert
        v-if="error"
        data-testid="admin-team-account-error"
        type="error"
        :closable="false"
        show-icon
        :title="error"
      />

      <section
        data-testid="admin-team-account-surface"
        class="max-w-6xl overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm"
      >
        <div class="flex flex-col gap-2 border-b border-slate-200 p-4 lg:flex-row lg:items-center">
          <ElInput
            v-model="q"
            data-testid="admin-team-account-search"
            type="search"
            clearable
            class="w-full lg:max-w-80"
            :placeholder="t('teamAccounts.admin.searchPlaceholder')"
          />
          <ElSelect
            v-model="statusFilter"
            data-testid="admin-team-account-status-filter"
            class="w-full lg:w-44"
            :placeholder="t('teamAccounts.admin.statusFilter')"
          >
            <ElOption :label="t('teamAccounts.admin.allStatuses')" value="all" />
            <ElOption
              v-for="status in statusOptions"
              :key="status"
              :label="t(teamAccountStatusLabelKey(status))"
              :value="status"
            />
          </ElSelect>
          <span class="text-sm text-gray-500 lg:ml-auto">{{ t('teamAccounts.admin.totalCount', { count: total }) }}</span>
        </div>

        <div class="p-4">
          <ElSkeleton v-if="loading && accounts.length === 0" :rows="3" animated />
          <ElEmpty v-else-if="pageAccounts.length === 0" :description="t('teamAccounts.admin.empty')" />
          <div v-else data-testid="admin-team-account-grid" class="grid gap-4 lg:grid-cols-2">
            <ElCard
              v-for="account in pageAccounts"
              :key="account.id"
              :data-testid="`admin-team-account-card-${account.id}`"
              shadow="never"
              class="min-w-0"
            >
              <template #header>
                <div class="flex items-start justify-between gap-3">
                  <div class="min-w-0">
                    <div class="truncate font-medium text-gray-900">{{ account.name }}</div>
                    <div class="mt-1 break-words text-sm text-gray-500">
                      {{ account.department_display_path || account.department_external_id || '—' }}
                    </div>
                  </div>
                  <ElTag :type="teamAccountStatusTagType(account.status)" size="small" effect="plain">
                    {{ t(teamAccountStatusLabelKey(account.status)) }}
                  </ElTag>
                </div>
              </template>

              <dl class="grid gap-3 text-sm sm:grid-cols-2">
                <div>
                  <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('teamAccounts.owner') }}</dt>
                  <dd class="mt-1 break-all text-gray-700">{{ account.owner_username || '—' }}</dd>
                </div>
                <div>
                  <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('teamAccounts.createdAt') }}</dt>
                  <dd class="mt-1 text-gray-700">{{ formatTeamAccountTimestamp(account.created_at, locale) }}</dd>
                </div>
                <div class="min-w-0 sm:col-span-2">
                  <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('teamAccounts.relayUsername') }}</dt>
                  <dd class="mt-1 break-all text-gray-700">{{ account.relay_username || '—' }}</dd>
                </div>
              </dl>

              <div class="mt-4 flex flex-wrap justify-end gap-2">
                <ElButton
                  :data-testid="`admin-team-account-view-${account.id}`"
                  @click="openDetail(account)"
                >
                  {{ t('teamAccounts.viewDetail') }}
                </ElButton>
                <ElButton
                  v-if="account.status === 'active'"
                  :data-testid="`admin-team-account-login-as-${account.id}`"
                  type="primary"
                  plain
                  :loading="pendingAccountID === account.id"
                  @click="loginAs(account, 'admin')"
                >
                  {{ t('teamAccounts.loginAs') }}
                </ElButton>
              </div>
            </ElCard>
          </div>
        </div>

        <div
          v-if="showPagination"
          class="flex flex-wrap items-center justify-between gap-3 border-t border-slate-200 px-4 py-3"
        >
          <span class="hidden text-sm text-slate-500 md:block">
            {{ t('pagination.range', { start: pageStart, end: pageEnd, total }) }}
          </span>
          <ElPagination
            data-testid="admin-team-account-pagination"
            class="ml-auto"
            background
            :current-page="page"
            :page-size="pageSize"
            :page-sizes="FULL_PAGE_SIZES"
            :total="total"
            :pager-count="5"
            :disabled="loading"
            :layout="desktopPagination ? 'sizes, prev, pager, next' : 'prev, slot, next'"
            @current-change="changePage"
            @size-change="changePageSize"
          >
            <span v-if="!desktopPagination" class="px-2 text-sm text-slate-600">
              {{ t('pagination.pageOf', { page, pages: totalPages }) }}
            </span>
          </ElPagination>
        </div>
      </section>

      <ElDialog
        :model-value="createOpen"
        data-testid="admin-team-account-create-dialog"
        append-to-body
        align-center
        :show-close="false"
        width="min(100%, 48rem)"
        :close-on-click-modal="!creating"
        :close-on-press-escape="!creating"
        @update:model-value="(value) => { if (!value) closeCreate() }"
      >
        <template #header>
          <div class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <h2 class="text-base font-semibold text-gray-900">{{ t('teamAccounts.admin.createTitle') }}</h2>
              <p class="mt-1 text-sm text-gray-500">{{ t('teamAccounts.admin.createHelp') }}</p>
            </div>
            <ElButton :disabled="creating" @click="closeCreate">{{ t('teamAccounts.close') }}</ElButton>
          </div>
        </template>

        <div class="space-y-4">
          <label class="block text-sm">
            <span class="font-medium text-gray-700">{{ t('teamAccounts.name') }}</span>
            <ElInput
              v-model="createForm.name"
              data-testid="admin-team-account-create-name"
              class="mt-1"
              :placeholder="t('teamAccounts.namePlaceholder')"
              :disabled="creating"
            />
          </label>
          <label class="block text-sm">
            <span class="font-medium text-gray-700">{{ t('teamAccounts.admin.departmentSelect') }}</span>
            <TeamAccountDepartmentPicker
              v-model="createForm.departmentExternalId"
              data-testid="admin-team-account-create-department"
              :disabled="creating"
              @change="onCreateDepartmentChange"
            />
          </label>
          <label class="block text-sm">
            <span class="font-medium text-gray-700">{{ t('teamAccounts.ownerUserId') }}</span>
            <TeamAccountUserPicker
              v-model="createForm.ownerUserID"
              data-testid="admin-team-account-create-owner"
              class="mt-1"
              :disabled="creating"
              :department-id="createForm.departmentExternalId"
              @select="onOwnerSelected"
            />
          </label>
          <div class="block text-sm">
            <span class="font-medium text-gray-700">{{ t('teamAccounts.additionalGroups') }}</span>
            <p class="mb-1 text-xs text-slate-500">{{ t('teamAccounts.additionalGroupsHint') }}</p>
            <TeamAccountGroupShuttle
              v-model="createAdditionalGroupIDs"
              data-testid="admin-team-account-create-groups"
              :groups="createGroupOptions"
              :default-group-ids="createDefaultGroupIDs"
              :grantable-group-ids="createGrantableGroupIDs"
              :disabled="creating"
            />
          </div>
          <ElAlert
            v-if="createError"
            data-testid="admin-team-account-create-error"
            type="error"
            :closable="false"
            show-icon
            :title="createError"
          />
        </div>

        <template #footer>
          <div class="flex justify-end gap-2">
            <ElButton :disabled="creating" @click="closeCreate">{{ t('teamAccounts.cancel') }}</ElButton>
            <ElButton
              data-testid="admin-team-account-create-submit"
              type="primary"
              :loading="creating"
              :disabled="creating"
              @click="submitCreate"
            >
              {{ creating ? t('teamAccounts.creating') : t('teamAccounts.admin.create') }}
            </ElButton>
          </div>
        </template>
      </ElDialog>


      <TeamAccountDetailDialog
        v-model="detailOpen"
        :account-id="selectedAccountID"
        :account="selectedAccount"
        mode="admin"
        @transfer-requested="openTransfer"
      @status-requested="openStatusDialog"
      />

      <ElDialog
      :model-value="statusDialogOpen"
      data-testid="admin-team-account-status-dialog"
      append-to-body
      align-center
      :show-close="false"
      width="min(100%, 30rem)"
      :close-on-click-modal="statusUpdatingID === null"
      :close-on-press-escape="statusUpdatingID === null"
      @update:model-value="(value) => { if (!value) closeStatusDialog() }"
    >
      <template #header>
        <div class="flex items-start justify-between gap-4">
          <div class="min-w-0">
            <h2 class="text-base font-semibold text-gray-900">{{ t('teamAccounts.admin.statusConfirmTitle') }}</h2>
            <p class="mt-1 break-words text-sm text-gray-500">{{ statusTarget?.name }}</p>
          </div>
          <ElButton :disabled="statusUpdatingID !== null" @click="closeStatusDialog">{{ t('teamAccounts.close') }}</ElButton>
        </div>
      </template>

      <div class="space-y-4">
        <ElAlert
          :type="statusNext === 'suspended' ? 'warning' : 'success'"
          :closable="false"
          show-icon
          :title="statusNext === 'suspended'
            ? t('teamAccounts.admin.suspendConfirm', { name: statusTarget?.name ?? '' })
            : t('teamAccounts.admin.restoreConfirm', { name: statusTarget?.name ?? '' })"
        />
        <label class="block text-sm">
          <span class="font-medium text-gray-700">{{ t('teamAccounts.admin.statusConfirmHint') }}</span>
          <ElInput
            v-model="statusConfirmText"
            data-testid="admin-team-account-status-confirm"
            class="mt-1"
            :disabled="statusUpdatingID !== null"
            :placeholder="statusTarget?.name ?? ''"
          />
        </label>
      </div>

      <template #footer>
        <div class="flex justify-end gap-2">
          <ElButton :disabled="statusUpdatingID !== null" @click="closeStatusDialog">{{ t('teamAccounts.cancel') }}</ElButton>
          <ElButton
            data-testid="admin-team-account-status-submit"
            :type="statusNext === 'suspended' ? 'warning' : 'success'"
            :loading="statusUpdatingID !== null"
            :disabled="!statusConfirmed || statusUpdatingID !== null"
            @click="submitStatusChange"
          >
            {{ t('teamAccounts.confirm') }}
          </ElButton>
        </div>
      </template>
    </ElDialog>

    <TeamAccountTransferDialog
        v-model="transferOpen"
        :account="transferAccount"
        mode="admin"
        @transferred="handleTransferred"
      />
    </div>
  </AppLayout>
</template>
