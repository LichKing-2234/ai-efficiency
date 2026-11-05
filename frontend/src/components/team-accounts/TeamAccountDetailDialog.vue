<script setup lang="ts">
import { computed, reactive, ref, watch } from 'vue'
import { ElMessage, ElMessageBox } from 'element-plus'
import {
  addAdminTeamAccountManager,
  addTeamAccountManager,
  createAdminTeamAccountKey,
  createTeamAccountKey,
  getAdminTeamAccount,
  getAdminTeamAccountUsage,
  getTeamAccount,
  getTeamAccountUsage,
  listAdminTeamAccountEvents,
  listTeamAccountEvents,
  removeAdminTeamAccountManager,
  removeTeamAccountManager,
  revealAdminTeamAccountCredentials,
  revealTeamAccountCredentials,
  revokeAdminTeamAccountKey,
  revokeTeamAccountKey,
} from '@/api/teamAccounts'
import TeamAccountUserPicker from './TeamAccountUserPicker.vue'
import { useI18n } from '@/i18n'
import { teamAccountsMessages } from '@/locales/teamAccounts'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { formatTokenCount } from '@/utils/formatters'
import {
  formatTeamAccountCost,
  formatTeamAccountNumber,
  formatTeamAccountTimestamp,
  teamAccountErrorMessage,
  teamAccountEventLabelKey,
  teamAccountKeyStatusLabelKey,
  teamAccountKeyStatusTagType,
  teamAccountRoleLabelKey,
  teamAccountStatusLabelKey,
  teamAccountStatusTagType,
} from '@/utils/teamAccounts'
import type {
  TeamAccountCredentials,
  TeamAccountDetail,
  TeamAccountEvent,
  TeamAccountSummary,
  TeamAccountUsage,
} from '@/types'

type UsageRange = 'today' | '7d' | '30d'

const props = withDefaults(defineProps<{
  modelValue: boolean
  accountId: number | null
  account?: TeamAccountSummary | null
  mode?: 'user' | 'admin'
}>(), {
  account: null,
  mode: 'user',
})

const emit = defineEmits<{
  'update:modelValue': [value: boolean]
  updated: [detail: TeamAccountDetail]
  'transfer-requested': [account: TeamAccountSummary]
  'status-requested': [account: TeamAccountSummary]
}>()

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'teamAccounts.', teamAccountsMessages)
const detail = ref<TeamAccountDetail | null>(null)
const detailLoading = ref(false)
const detailError = ref('')
const credentials = ref<TeamAccountCredentials | null>(null)
const credentialsLoading = ref(false)
const credentialsError = ref('')
const keyForm = reactive({ groupId: '', name: '' })
const keyCreating = ref(false)
const keyError = ref('')
const createdKey = ref<{ name: string; secret: string } | null>(null)
const managerUserID = ref<number | null>(null)
const managerAdding = ref(false)
const managerError = ref('')
const usageRange = ref<UsageRange>('7d')
const usage = ref<TeamAccountUsage | null>(null)
const usageLoading = ref(false)
const usageError = ref('')
const events = ref<TeamAccountEvent[]>([])
const eventsLoading = ref(false)
const eventsError = ref('')
let detailRequestSeq = 0
let usageRequestSeq = 0
let eventsRequestSeq = 0

const api = computed(() => props.mode === 'admin'
  ? {
      get: getAdminTeamAccount,
      credentials: revealAdminTeamAccountCredentials,
      createKey: createAdminTeamAccountKey,
      revokeKey: revokeAdminTeamAccountKey,
      addManager: addAdminTeamAccountManager,
      removeManager: removeAdminTeamAccountManager,
      usage: getAdminTeamAccountUsage,
      events: listAdminTeamAccountEvents,
    }
  : {
      get: getTeamAccount,
      credentials: revealTeamAccountCredentials,
      createKey: createTeamAccountKey,
      revokeKey: revokeTeamAccountKey,
      addManager: addTeamAccountManager,
      removeManager: removeTeamAccountManager,
      usage: getTeamAccountUsage,
      events: listTeamAccountEvents,
    })
const summary = computed<TeamAccountSummary | null>(() => detail.value ?? props.account)
const keys = computed(() => detail.value?.keys ?? [])
const managers = computed(() => detail.value?.managers ?? [])
const canCreateKey = computed(() => (
  Number.isInteger(Number(keyForm.groupId))
  && Number(keyForm.groupId) > 0
  && keyForm.name.trim().length > 0
  && !keyCreating.value
))
const canAddManager = computed(() => (
  managerUserID.value !== null
  && managerUserID.value > 0
  && !managerAdding.value
))

watch(
  () => [props.modelValue, props.accountId] as const,
  ([open, accountID]) => {
    if (!open) {
      detailRequestSeq += 1
      usageRequestSeq += 1
      eventsRequestSeq += 1
      detail.value = null
      usage.value = null
      events.value = []
      return
    }
    if (accountID == null) return
    if (detail.value && detail.value.id !== accountID) {
      detail.value = null
      usage.value = null
      events.value = []
    }
    void loadAll(accountID)
  },
  { immediate: true },
)

function resetTransientState() {
  credentials.value = null
  credentialsError.value = ''
  keyForm.groupId = ''
  keyForm.name = ''
  keyError.value = ''
  createdKey.value = null
  managerUserID.value = null
  managerError.value = ''
  usageRange.value = '7d'
  usageError.value = ''
  eventsError.value = ''
}

async function loadAll(accountID: number) {
  resetTransientState()
  await Promise.all([
    loadDetail(accountID),
    loadUsage(accountID),
    loadEvents(accountID),
  ])
}

async function loadDetail(accountID: number) {
  const requestSeq = ++detailRequestSeq
  detailLoading.value = true
  detailError.value = ''
  try {
    const response = await api.value.get(accountID)
    if (requestSeq !== detailRequestSeq) return
    const data = response.data.data
    if (!data) {
      detail.value = null
      return
    }
    // The backend omits empty key/manager collections.
    detail.value = {
      ...data,
      keys: data.keys ?? [],
      managers: data.managers ?? [],
    }
    emit('updated', detail.value)
  } catch (error: unknown) {
    if (requestSeq !== detailRequestSeq) return
    detail.value = null
    detailError.value = teamAccountErrorMessage(error, t('teamAccounts.detailLoadFailed'))
  } finally {
    if (requestSeq === detailRequestSeq) {
      detailLoading.value = false
    }
  }
}

function usageParams(range: UsageRange) {
  const to = new Date()
  const from = new Date(to)
  if (range === '7d') from.setDate(to.getDate() - 6)
  if (range === '30d') from.setDate(to.getDate() - 29)
  return { from: formatDate(from), to: formatDate(to) }
}

function formatDate(date: Date) {
  const year = date.getFullYear()
  const month = String(date.getMonth() + 1).padStart(2, '0')
  const day = String(date.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

async function loadUsage(accountID: number | null = props.accountId) {
  if (accountID == null) return
  const requestSeq = ++usageRequestSeq
  usageLoading.value = true
  usageError.value = ''
  try {
    const response = await api.value.usage(accountID, usageParams(usageRange.value))
    if (requestSeq !== usageRequestSeq) return
    usage.value = response.data.data ?? null
  } catch (error: unknown) {
    if (requestSeq !== usageRequestSeq) return
    usage.value = null
    usageError.value = teamAccountErrorMessage(error, t('teamAccounts.usage.loadFailed'))
  } finally {
    if (requestSeq === usageRequestSeq) {
      usageLoading.value = false
    }
  }
}

async function loadEvents(accountID: number) {
  const requestSeq = ++eventsRequestSeq
  eventsLoading.value = true
  eventsError.value = ''
  try {
    const response = await api.value.events(accountID, 100)
    if (requestSeq !== eventsRequestSeq) return
    events.value = response.data.data?.items ?? []
  } catch (error: unknown) {
    if (requestSeq !== eventsRequestSeq) return
    events.value = []
    eventsError.value = teamAccountErrorMessage(error, t('teamAccounts.events.loadFailed'))
  } finally {
    if (requestSeq === eventsRequestSeq) {
      eventsLoading.value = false
    }
  }
}

function handleUsageRangeChange(value: string | number | boolean | undefined) {
  if (value !== 'today' && value !== '7d' && value !== '30d') return
  usageRange.value = value
  void loadUsage()
}

function close() {
  emit('update:modelValue', false)
}

async function copyValue(value: string) {
  try {
    if (!navigator.clipboard?.writeText) throw new Error('clipboard unavailable')
    await navigator.clipboard.writeText(value)
    ElMessage.success(t('teamAccounts.copied'))
  } catch {
    ElMessage.error(t('teamAccounts.copyFailed'))
  }
}

async function revealCredentials() {
  if (props.accountId == null) return
  if (credentials.value) {
    credentials.value = null
    return
  }
  credentialsLoading.value = true
  credentialsError.value = ''
  try {
    const response = await api.value.credentials(props.accountId)
    credentials.value = response.data.data ?? null
  } catch (error: unknown) {
    credentialsError.value = teamAccountErrorMessage(error, t('teamAccounts.credentials.loadFailed'))
  } finally {
    credentialsLoading.value = false
  }
}

async function createKey() {
  if (props.accountId == null) return
  if (!canCreateKey.value) {
    keyError.value = t('teamAccounts.keys.fieldsRequired')
    return
  }
  keyCreating.value = true
  keyError.value = ''
  try {
    const response = await api.value.createKey(props.accountId, {
      relay_group_id: Number(keyForm.groupId),
      name: keyForm.name.trim(),
    })
    const data = response.data.data
    if (data) {
      createdKey.value = { name: data.key.name, secret: data.secret }
    }
    keyForm.groupId = ''
    keyForm.name = ''
    ElMessage.success(t('teamAccounts.keys.created'))
    await loadDetail(props.accountId)
    await loadEvents(props.accountId)
  } catch (error: unknown) {
    keyError.value = teamAccountErrorMessage(error, t('teamAccounts.keys.createFailed'))
  } finally {
    keyCreating.value = false
  }
}

async function revokeKey(keyID: number, name: string) {
  if (props.accountId == null) return
  try {
    await ElMessageBox.confirm(
      t('teamAccounts.keys.revokeConfirm', { name }),
      t('teamAccounts.keys.revokeTitle'),
      {
        type: 'warning',
        confirmButtonText: t('teamAccounts.confirm'),
        cancelButtonText: t('teamAccounts.cancel'),
      },
    )
  } catch {
    return
  }
  try {
    await api.value.revokeKey(props.accountId, keyID)
    ElMessage.success(t('teamAccounts.keys.revoked'))
    await loadDetail(props.accountId)
    await loadEvents(props.accountId)
  } catch (error: unknown) {
    ElMessage.error(teamAccountErrorMessage(error, t('teamAccounts.keys.revokeFailed')))
  }
}

async function addManager() {
  if (props.accountId == null) return
  if (!canAddManager.value) {
    managerError.value = t('teamAccounts.managers.userIdRequired')
    return
  }
  managerAdding.value = true
  managerError.value = ''
  try {
    await api.value.addManager(props.accountId, managerUserID.value as number)
    managerUserID.value = null
    ElMessage.success(t('teamAccounts.managers.added'))
    await loadDetail(props.accountId)
    await loadEvents(props.accountId)
  } catch (error: unknown) {
    managerError.value = teamAccountErrorMessage(error, t('teamAccounts.managers.addFailed'))
  } finally {
    managerAdding.value = false
  }
}

async function removeManager(userID: number, username: string) {
  if (props.accountId == null) return
  try {
    await ElMessageBox.confirm(
      t('teamAccounts.managers.removeConfirm', { username }),
      t('teamAccounts.managers.removeTitle'),
      {
        type: 'warning',
        confirmButtonText: t('teamAccounts.confirm'),
        cancelButtonText: t('teamAccounts.cancel'),
      },
    )
  } catch {
    return
  }
  try {
    await api.value.removeManager(props.accountId, userID)
    ElMessage.success(t('teamAccounts.managers.removed'))
    await loadDetail(props.accountId)
    await loadEvents(props.accountId)
  } catch (error: unknown) {
    ElMessage.error(teamAccountErrorMessage(error, t('teamAccounts.managers.removeFailed')))
  }
}

function requestTransfer() {
  const current = summary.value
  if (!current) return
  emit('transfer-requested', current)
}

function requestStatusChange() {
  const current = summary.value
  if (!current) return
  emit('status-requested', current)
}
</script>

<template>
  <ElDialog
    :model-value="props.modelValue"
    data-testid="team-account-detail-dialog"
    append-to-body
    align-center
    :show-close="false"
    width="min(100%, 60rem)"
    @update:model-value="(value) => { if (!value) close() }"
  >
    <template #header>
      <div class="flex items-start justify-between gap-4">
        <div class="min-w-0">
          <div class="flex flex-wrap items-center gap-2">
            <h2 class="break-words text-base font-semibold text-gray-900">
              {{ summary?.name || t('teamAccounts.loading') }}
            </h2>
            <ElTag v-if="summary" :type="teamAccountStatusTagType(summary.status)" size="small" effect="plain">
              {{ t(teamAccountStatusLabelKey(summary.status)) }}
            </ElTag>
          </div>
          <p v-if="summary?.department_display_path" class="mt-1 break-words text-sm text-gray-500">
            {{ summary.department_display_path }}
          </p>
        </div>
        <div class="flex shrink-0 items-center gap-2">
          <ElButton
            data-testid="team-account-detail-refresh"
            :loading="detailLoading"
            @click="props.accountId != null && loadAll(props.accountId)"
          >
            {{ t('teamAccounts.refresh') }}
          </ElButton>
          <ElButton @click="close">{{ t('teamAccounts.close') }}</ElButton>
        </div>
      </div>
    </template>

    <div class="max-h-[72vh] space-y-5 overflow-y-auto pr-1">
      <ElSkeleton v-if="detailLoading && !detail" :rows="4" animated />

      <ElAlert
        v-else-if="detailError && !detail"
        data-testid="team-account-detail-error"
        type="error"
        :closable="false"
        show-icon
        :title="detailError"
      />

      <template v-else-if="detail">
        <ElAlert
          v-if="detailError"
          data-testid="team-account-detail-refresh-error"
          type="warning"
          :closable="false"
          show-icon
          :title="detailError"
        />
        <section data-testid="team-account-detail-summary" class="rounded-lg border border-slate-200 bg-slate-50 p-4">
          <dl class="grid gap-3 text-sm sm:grid-cols-2 lg:grid-cols-4">
            <div>
              <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('teamAccounts.owner') }}</dt>
              <dd class="mt-1 text-gray-700">{{ t('teamAccounts.ownerUserId') }} #{{ detail.owner_user_id }}</dd>
            </div>
            <div>
              <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('teamAccounts.createdAt') }}</dt>
              <dd class="mt-1 text-gray-700">{{ formatTeamAccountTimestamp(detail.created_at, locale) }}</dd>
            </div>
            <div class="sm:col-span-2">
              <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('teamAccounts.relayUsername') }}</dt>
              <dd class="mt-1 flex flex-wrap items-center gap-2">
                <code class="break-all rounded bg-white px-2 py-1 text-xs text-gray-800 ring-1 ring-slate-200">
                  {{ detail.relay_username }}
                </code>
                <ElButton
                  data-testid="team-account-copy-username"
                  link
                  type="primary"
                  @click="copyValue(detail.relay_username)"
                >
                  {{ t('teamAccounts.copy') }}
                </ElButton>
              </dd>
            </div>
          </dl>
          <div class="mt-4 flex flex-wrap gap-2">
            <ElButton
              data-testid="team-account-reveal-credentials"
              type="primary"
              plain
              :loading="credentialsLoading"
              @click="revealCredentials"
            >
              {{ credentials ? t('teamAccounts.credentials.hide') : t('teamAccounts.credentials.reveal') }}
            </ElButton>
            <ElButton data-testid="team-account-transfer" @click="requestTransfer">
              {{ t('teamAccounts.transfer.button') }}
            </ElButton>
            <ElButton
              v-if="mode === 'admin' && summary"
              data-testid="team-account-status-action"
              :type="summary.status === 'active' ? 'warning' : 'success'"
              @click="requestStatusChange"
            >
              {{ summary.status === 'active' ? t('teamAccounts.admin.suspend') : t('teamAccounts.admin.restore') }}
            </ElButton>
          </div>

          <ElAlert
            v-if="credentialsError"
            data-testid="team-account-credentials-error"
            class="mt-3"
            type="error"
            :closable="false"
            show-icon
            :title="credentialsError"
          />
          <div
            v-if="credentials"
            data-testid="team-account-credentials"
            class="mt-3 rounded-lg border border-amber-200 bg-amber-50 p-3"
          >
            <p class="text-xs font-medium text-amber-800">{{ t('teamAccounts.credentials.warning') }}</p>
            <dl class="mt-2 grid gap-2 text-sm sm:grid-cols-2">
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-amber-700">{{ t('teamAccounts.credentials.username') }}</dt>
                <dd class="mt-1 flex flex-wrap items-center gap-2">
                  <code data-testid="team-account-username" class="break-all">{{ credentials.username }}</code>
                  <ElButton data-testid="team-account-copy-relay-username" link type="primary" @click="copyValue(credentials.username)">
                    {{ t('teamAccounts.copy') }}
                  </ElButton>
                </dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-amber-700">{{ t('teamAccounts.credentials.password') }}</dt>
                <dd class="mt-1 flex flex-wrap items-center gap-2">
                  <code data-testid="team-account-password" class="break-all">{{ credentials.password }}</code>
                  <ElButton data-testid="team-account-copy-password" link type="primary" @click="copyValue(credentials.password)">
                    {{ t('teamAccounts.copy') }}
                  </ElButton>
                </dd>
              </div>
            </dl>
          </div>
        </section>

        <section data-testid="team-account-keys" class="space-y-3">
          <div>
            <h3 class="text-sm font-semibold text-gray-900">{{ t('teamAccounts.keys.title') }}</h3>
            <p class="text-xs text-gray-500">{{ t('teamAccounts.keys.help') }}</p>
          </div>

          <div class="flex flex-col gap-2 sm:flex-row sm:items-end">
            <label class="block text-sm sm:w-40">
              <span class="font-medium text-gray-700">{{ t('teamAccounts.keys.groupId') }}</span>
              <ElInput
                v-model="keyForm.groupId"
                data-testid="team-account-key-group"
                class="mt-1"
                type="number"
                min="1"
                :disabled="keyCreating"
              />
            </label>
            <label class="block min-w-0 flex-1 text-sm">
              <span class="font-medium text-gray-700">{{ t('teamAccounts.keys.name') }}</span>
              <ElInput
                v-model="keyForm.name"
                data-testid="team-account-key-name"
                class="mt-1"
                :placeholder="t('teamAccounts.keys.namePlaceholder')"
                :disabled="keyCreating"
              />
            </label>
            <ElButton
              data-testid="team-account-create-key"
              type="primary"
              :loading="keyCreating"
              :disabled="keyCreating"
              @click="createKey"
            >
              {{ keyCreating ? t('teamAccounts.keys.creating') : t('teamAccounts.keys.create') }}
            </ElButton>
          </div>

          <ElAlert
            v-if="keyError"
            data-testid="team-account-key-error"
            type="error"
            :closable="false"
            show-icon
            :title="keyError"
          />
          <ElAlert
            v-if="createdKey"
            data-testid="team-account-key-secret"
            type="success"
            :closable="false"
            show-icon
            :title="t('teamAccounts.keys.secretOnce')"
          >
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-sm font-medium">{{ createdKey.name }}:</span>
              <code data-testid="team-account-key-secret-value" class="break-all rounded bg-white px-2 py-1 text-xs ring-1 ring-emerald-200">
                {{ createdKey.secret }}
              </code>
              <ElButton link type="primary" @click="copyValue(createdKey.secret)">
                {{ t('teamAccounts.copy') }}
              </ElButton>
            </div>
          </ElAlert>

          <ElTable v-if="keys.length > 0" :data="keys" size="small" class="w-full">
            <ElTableColumn prop="name" :label="t('teamAccounts.keys.name')" min-width="120" />
            <ElTableColumn prop="relay_group_id" :label="t('teamAccounts.keys.groupId')" width="130" />
            <ElTableColumn prop="secret_preview" :label="t('teamAccounts.keys.preview')" min-width="140" />
            <ElTableColumn :label="t('teamAccounts.status.label')" width="100">
              <template #default="{ row }">
                <ElTag :type="teamAccountKeyStatusTagType(row.status)" size="small">
                  {{ t(teamAccountKeyStatusLabelKey(row.status)) }}
                </ElTag>
              </template>
            </ElTableColumn>
            <ElTableColumn :label="t('teamAccounts.createdAt')" min-width="160">
              <template #default="{ row }">
                {{ formatTeamAccountTimestamp(row.created_at, locale) }}
              </template>
            </ElTableColumn>
            <ElTableColumn :label="t('teamAccounts.actions')" width="100" fixed="right">
              <template #default="{ row }">
                <ElButton
                  :data-testid="`team-account-revoke-key-${row.id}`"
                  link
                  type="danger"
                  :disabled="row.status !== 'active'"
                  @click="revokeKey(row.id, row.name)"
                >
                  {{ t('teamAccounts.keys.revoke') }}
                </ElButton>
              </template>
            </ElTableColumn>
          </ElTable>
          <ElEmpty v-else :description="t('teamAccounts.keys.empty')" :image-size="48" />
        </section>

        <section data-testid="team-account-managers" class="space-y-3">
          <div>
            <h3 class="text-sm font-semibold text-gray-900">{{ t('teamAccounts.managers.title') }}</h3>
            <p class="text-xs text-gray-500">{{ t('teamAccounts.managers.help') }}</p>
          </div>

          <div class="flex flex-col gap-2 sm:flex-row sm:items-end">
            <label class="block text-sm sm:w-64">
              <span class="font-medium text-gray-700">{{ t('teamAccounts.managers.userId') }}</span>
              <TeamAccountUserPicker
                v-model="managerUserID"
                data-testid="team-account-manager-user-id"
                class="mt-1"
                :disabled="managerAdding"
              />
            </label>
            <ElButton
              data-testid="team-account-add-manager"
              type="primary"
              :loading="managerAdding"
              :disabled="managerAdding"
              @click="addManager"
            >
              {{ managerAdding ? t('teamAccounts.managers.adding') : t('teamAccounts.managers.add') }}
            </ElButton>
          </div>

          <ElAlert
            v-if="managerError"
            data-testid="team-account-manager-error"
            type="error"
            :closable="false"
            show-icon
            :title="managerError"
          />

          <ElTable v-if="managers.length > 0" :data="managers" size="small" class="w-full">
            <ElTableColumn prop="username" :label="t('teamAccounts.managers.username')" min-width="120" />
            <ElTableColumn prop="email" :label="t('teamAccounts.managers.email')" min-width="180" />
            <ElTableColumn :label="t('teamAccounts.managers.role')" width="110">
              <template #default="{ row }">
                <ElTag size="small" effect="plain">{{ t(teamAccountRoleLabelKey(row.role)) }}</ElTag>
              </template>
            </ElTableColumn>
            <ElTableColumn :label="t('teamAccounts.actions')" width="100" fixed="right">
              <template #default="{ row }">
                <ElButton
                  :data-testid="`team-account-remove-manager-${row.user_id}`"
                  link
                  type="danger"
                  :disabled="row.role === 'owner'"
                  @click="removeManager(row.user_id, row.username)"
                >
                  {{ t('teamAccounts.managers.remove') }}
                </ElButton>
              </template>
            </ElTableColumn>
          </ElTable>
          <ElEmpty v-else :description="t('teamAccounts.managers.empty')" :image-size="48" />
        </section>

        <section data-testid="team-account-usage" class="space-y-3">
          <div class="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
            <h3 class="text-sm font-semibold text-gray-900">{{ t('teamAccounts.usage.title') }}</h3>
            <ElRadioGroup
              :model-value="usageRange"
              :disabled="usageLoading"
              @change="handleUsageRangeChange"
            >
              <ElRadioButton data-testid="team-account-usage-today" value="today">
                {{ t('teamAccounts.usage.today') }}
              </ElRadioButton>
              <ElRadioButton data-testid="team-account-usage-7d" value="7d">
                {{ t('teamAccounts.usage.sevenDays') }}
              </ElRadioButton>
              <ElRadioButton data-testid="team-account-usage-30d" value="30d">
                {{ t('teamAccounts.usage.thirtyDays') }}
              </ElRadioButton>
            </ElRadioGroup>
          </div>

          <ElAlert
            v-if="usageError"
            data-testid="team-account-usage-error"
            type="error"
            :closable="false"
            show-icon
            :title="usageError"
          />
          <ElSkeleton v-else-if="usageLoading && !usage" :rows="2" animated />
          <template v-else-if="usage">
            <ElAlert
              v-if="usageLoading"
              data-testid="team-account-usage-loading"
              type="info"
              :closable="false"
              :show-icon="true"
              :title="t('teamAccounts.loading')"
            />
            <div class="grid grid-cols-2 gap-3">
              <div class="rounded-lg border border-slate-200 bg-white p-3">
                <div class="text-xs font-medium text-slate-500">{{ t('teamAccounts.usage.totalTokens') }}</div>
                <div class="mt-1 text-lg font-semibold tabular-nums text-slate-950">
                  {{ formatTokenCount(usage.total_tokens) }}
                </div>
              </div>
              <div class="rounded-lg border border-slate-200 bg-white p-3">
                <div class="text-xs font-medium text-slate-500">{{ t('teamAccounts.usage.totalCost') }}</div>
                <div class="mt-1 text-lg font-semibold tabular-nums text-slate-950">
                  {{ formatTeamAccountCost(usage.total_cost) }}
                </div>
              </div>
            </div>

            <div v-if="usage.keys.length > 0">
              <h4 class="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-500">
                {{ t('teamAccounts.usage.byKey') }}
              </h4>
              <ElTable :data="usage.keys" size="small" class="w-full">
                <ElTableColumn prop="name" :label="t('teamAccounts.keys.name')" min-width="120" />
                <ElTableColumn prop="relay_group_id" :label="t('teamAccounts.keys.groupId')" width="130" />
                <ElTableColumn :label="t('teamAccounts.usage.tokens')" width="120">
                  <template #default="{ row }">{{ formatTeamAccountNumber(row.tokens) }}</template>
                </ElTableColumn>
                <ElTableColumn :label="t('teamAccounts.usage.cost')" width="120">
                  <template #default="{ row }">{{ formatTeamAccountCost(row.cost) }}</template>
                </ElTableColumn>
              </ElTable>
            </div>

            <div v-if="usage.days.length > 0">
              <h4 class="mb-2 text-xs font-semibold uppercase tracking-wide text-gray-500">
                {{ t('teamAccounts.usage.daily') }}
              </h4>
              <ElTable :data="usage.days" size="small" class="w-full">
                <ElTableColumn prop="date" :label="t('teamAccounts.usage.date')" min-width="120" />
                <ElTableColumn :label="t('teamAccounts.usage.tokens')" width="140">
                  <template #default="{ row }">{{ formatTeamAccountNumber(row.tokens) }}</template>
                </ElTableColumn>
                <ElTableColumn :label="t('teamAccounts.usage.cost')" width="120">
                  <template #default="{ row }">{{ formatTeamAccountCost(row.cost) }}</template>
                </ElTableColumn>
              </ElTable>
            </div>

            <ElEmpty
              v-if="usage.keys.length === 0 && usage.days.length === 0"
              :description="t('teamAccounts.usage.empty')"
              :image-size="48"
            />
          </template>
        </section>

        <section data-testid="team-account-events" class="space-y-3">
          <h3 class="text-sm font-semibold text-gray-900">{{ t('teamAccounts.events.title') }}</h3>

          <ElAlert
            v-if="eventsError"
            data-testid="team-account-events-error"
            type="error"
            :closable="false"
            show-icon
            :title="eventsError"
          />
          <ElSkeleton v-else-if="eventsLoading && events.length === 0" :rows="2" animated />
          <ElEmpty v-else-if="events.length === 0" :description="t('teamAccounts.events.empty')" :image-size="48" />
          <ul v-else class="divide-y divide-slate-100 rounded-lg border border-slate-200">
            <li
              v-for="event in events"
              :key="event.id"
              class="flex flex-col gap-1 p-3 text-sm sm:flex-row sm:items-center sm:justify-between"
            >
              <span class="font-medium text-gray-800">{{ t(teamAccountEventLabelKey(event.action)) }}</span>
              <span class="flex flex-wrap gap-x-3 gap-y-1 text-xs text-gray-500">
                <span>{{ t('teamAccounts.events.actor') }}:
                  {{ event.actor_user_id ? `#${event.actor_user_id}` : t('teamAccounts.events.system') }}
                </span>
                <span>{{ formatTeamAccountTimestamp(event.created_at, locale) }}</span>
              </span>
            </li>
          </ul>
        </section>
      </template>
    </div>
  </ElDialog>
</template>
