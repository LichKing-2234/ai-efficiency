<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import { ElMessage } from 'element-plus'
import TeamAccountDetailDialog from './TeamAccountDetailDialog.vue'
import TeamAccountTransferDialog from './TeamAccountTransferDialog.vue'
import { createTeamAccount, getTeamAccount, listTeamAccounts } from '@/api/teamAccounts'
import { getTeamUsageScope } from '@/api/teamUsage'
import { useTeamAccountLogin } from '@/composables/useTeamAccountLogin'
import { useI18n } from '@/i18n'
import { teamAccountsMessages } from '@/locales/teamAccounts'
import { createFeatureTranslator } from '@/utils/featureI18n'
import {
  formatTeamAccountTimestamp,
  teamAccountErrorMessage,
  teamAccountStatusLabelKey,
  teamAccountStatusTagType,
} from '@/utils/teamAccounts'
import type { TeamAccountDetail, TeamAccountSummary } from '@/types'

const { t: baseT, locale } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'teamAccounts.', teamAccountsMessages)
const accounts = ref<TeamAccountSummary[]>([])
const loading = ref(false)
const error = ref('')
const createOpen = ref(false)
const createForm = reactive({ name: '', departmentExternalId: '' })
const creating = ref(false)
const createError = ref('')
const detailOpen = ref(false)
const selectedAccountID = ref<number | null>(null)
const selectedAccount = ref<TeamAccountSummary | null>(null)
const transferOpen = ref(false)
const transferAccount = ref<TeamAccountSummary | null>(null)
const keyCounts = ref<Record<number, number>>({})
const { loginAs, pendingAccountID } = useTeamAccountLogin()
const isRepresentative = ref(false)
const representativeDepartments = ref<{ external_id: string; name: string; display_path: string }[]>([])
const scopeLoaded = ref(false)


onMounted(() => {
  void loadAccounts()
  void loadScope()
})

async function loadScope() {
  try {
    const response = await getTeamUsageScope()
    const scope = response.data.data
    isRepresentative.value = scope?.is_representative ?? false
    representativeDepartments.value = (scope?.departments ?? []).map((department) => ({
      external_id: department.external_id,
      name: department.name,
      display_path: department.display_path,
    }))
  } catch {
    isRepresentative.value = false
    representativeDepartments.value = []
  } finally {
    scopeLoaded.value = true
  }
}

const canCreate = computed(() => isRepresentative.value)

const singleRepresentedDepartment = computed(() => (
  representativeDepartments.value.length === 1 ? representativeDepartments.value[0] : null
))

async function loadAccounts() {
  loading.value = true
  error.value = ''
  try {
    const response = await listTeamAccounts()
    accounts.value = response.data.data?.items ?? []
    void hydrateKeyCounts(accounts.value)
  } catch (e: unknown) {
    accounts.value = []
    error.value = teamAccountErrorMessage(e, t('teamAccounts.loadFailed'))
  } finally {
    loading.value = false
  }
}

async function hydrateKeyCounts(items: TeamAccountSummary[]) {
  // The list contract has no key count, so fill it from each detail without
  // blocking the card grid. Failures leave the count unknown until the user
  // opens the account.
  await Promise.allSettled(items.map(async (account) => {
    try {
      const response = await getTeamAccount(account.id)
      const data = response.data.data
      if (!data) return
      keyCounts.value = { ...keyCounts.value, [account.id]: data.keys?.length ?? 0 }
    } catch {
      // The detail drawer reports access or availability errors when opened.
    }
  }))
}

function openCreate() {
  createForm.name = ''
  createForm.departmentExternalId = singleRepresentedDepartment.value?.external_id ?? ''
  createError.value = ''
  createOpen.value = true
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
  creating.value = true
  createError.value = ''
  try {
    const response = await createTeamAccount({
      name: createForm.name.trim(),
      department_external_id: createForm.departmentExternalId.trim() || undefined,
    })
    const created = response.data.data
    ElMessage.success(t('teamAccounts.createSuccess'))
    createOpen.value = false
    if (created) {
      accounts.value = [...accounts.value, created]
      openDetail(created)
    } else {
      await loadAccounts()
    }
  } catch (e: unknown) {
    createError.value = teamAccountErrorMessage(e, t('teamAccounts.createFailed'))
  } finally {
    creating.value = false
  }
}

function openDetail(account: TeamAccountSummary) {
  selectedAccount.value = account
  selectedAccountID.value = account.id
  detailOpen.value = true
}

function handleDetailUpdated(detail: TeamAccountDetail) {
  keyCounts.value = { ...keyCounts.value, [detail.id]: detail.keys.length }
  const index = accounts.value.findIndex((account) => account.id === detail.id)
  if (index >= 0) {
    const next = [...accounts.value]
    next[index] = { ...next[index], ...detail }
    accounts.value = next
    if (selectedAccount.value?.id === detail.id) selectedAccount.value = next[index]
  }
}

function keyCount(account: TeamAccountSummary) {
  const cached = keyCounts.value[account.id]
  return cached == null ? '—' : String(cached)
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
</script>

<template>
  <section
    data-testid="team-account-section"
    class="space-y-3 rounded-xl border border-slate-200 bg-white p-4 shadow-sm sm:p-5"
  >
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div class="min-w-0">
        <h2 class="text-lg font-semibold text-slate-950">{{ t('teamAccounts.title') }}</h2>
        <p class="mt-1 text-sm text-slate-500">{{ t('teamAccounts.subtitle') }}</p>
      </div>
      <div class="flex shrink-0 gap-2">
        <ElButton
          data-testid="team-account-reload"
          :loading="loading"
          @click="loadAccounts"
        >
          {{ t('teamAccounts.refresh') }}
        </ElButton>
        <ElButton v-if="canCreate" data-testid="team-account-create-open" type="primary" @click="openCreate">
          {{ t('teamAccounts.create') }}
        </ElButton>
      </div>
    </div>

    <ElAlert
      v-if="error"
      data-testid="team-account-load-error"
      type="error"
      :closable="false"
      show-icon
      :title="error"
    />

    <ElSkeleton v-if="loading && accounts.length === 0" :rows="3" animated />

    <ElEmpty v-else-if="accounts.length === 0 && !error" :description="t('teamAccounts.empty')">
      <ElButton v-if="canCreate" data-testid="team-account-empty-create" type="primary" @click="openCreate">
        {{ t('teamAccounts.create') }}
      </ElButton>
    </ElEmpty>

    <div v-else-if="accounts.length > 0" data-testid="team-account-grid" class="grid gap-3 md:grid-cols-2">
      <ElCard
        v-for="account in accounts"
        :key="account.id"
        :data-testid="`team-account-card-${account.id}`"
        shadow="hover"
        class="min-w-0 cursor-pointer transition hover:border-blue-300 focus:outline-none focus-visible:ring-2 focus-visible:ring-blue-500"
        tabindex="0"
        role="button"
        @click="openDetail(account)"
        @keydown.enter.prevent="openDetail(account)"
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
            <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('teamAccounts.keyCount') }}</dt>
            <dd class="mt-1 tabular-nums text-gray-700">{{ keyCount(account) }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('teamAccounts.owner') }}</dt>
            <dd class="mt-1 break-all text-gray-700">{{ account.owner_username || `#${account.owner_user_id}` }}</dd>
          </div>
          <div class="min-w-0">
            <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('teamAccounts.relayUsername') }}</dt>
            <dd class="mt-1 break-all text-gray-700">{{ account.relay_username || '—' }}</dd>
          </div>
          <div>
            <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('teamAccounts.createdAt') }}</dt>
            <dd class="mt-1 text-gray-700">{{ formatTeamAccountTimestamp(account.created_at, locale) }}</dd>
          </div>
        </dl>

        <div class="mt-4 flex flex-wrap justify-end gap-2">
          <ElButton size="small" @click.stop="openDetail(account)">
            {{ t('teamAccounts.viewDetail') }}
          </ElButton>
          <ElButton
            :data-testid="`team-account-login-as-${account.id}`"
            size="small"
            type="primary"
            plain
            :loading="pendingAccountID === account.id"
            @click.stop="loginAs(account)"
          >
            {{ t('teamAccounts.loginAs') }}
          </ElButton>
        </div>
      </ElCard>
    </div>

    <ElDialog
      :model-value="createOpen"
      data-testid="team-account-create-dialog"
      append-to-body
      align-center
      :show-close="false"
      width="min(100%, 32rem)"
      :close-on-click-modal="!creating"
      :close-on-press-escape="!creating"
      @update:model-value="(value) => { if (!value) closeCreate() }"
    >
      <template #header>
        <div class="flex items-start justify-between gap-4">
          <div class="min-w-0">
            <h2 class="text-base font-semibold text-gray-900">{{ t('teamAccounts.createTitle') }}</h2>
            <p class="mt-1 text-sm text-gray-500">{{ t('teamAccounts.createHelp') }}</p>
          </div>
          <ElButton :disabled="creating" @click="closeCreate">{{ t('teamAccounts.close') }}</ElButton>
        </div>
      </template>

      <div class="space-y-4">
        <label class="block text-sm">
          <span class="font-medium text-gray-700">{{ t('teamAccounts.name') }}</span>
          <ElInput
            v-model="createForm.name"
            data-testid="team-account-create-name"
            class="mt-1"
            :placeholder="t('teamAccounts.namePlaceholder')"
            :disabled="creating"
          />
        </label>
        <label v-if="!singleRepresentedDepartment" class="block text-sm">
          <span class="font-medium text-gray-700">{{ t('teamAccounts.admin.departmentSelect') }}</span>
          <ElSelect
            v-model="createForm.departmentExternalId"
            data-testid="team-account-create-department"
            class="mt-1 w-full"
            :placeholder="t('teamAccounts.admin.departmentPlaceholder')"
            :disabled="creating"
          >
            <ElOption
              v-for="department in representativeDepartments"
              :key="department.external_id"
              :label="department.display_path || department.name"
              :value="department.external_id"
            />
          </ElSelect>
        </label>
        <ElAlert
          v-if="createError"
          data-testid="team-account-create-error"
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
            data-testid="team-account-create-submit"
            type="primary"
            :loading="creating"
            :disabled="creating"
            @click="submitCreate"
          >
            {{ creating ? t('teamAccounts.creating') : t('teamAccounts.create') }}
          </ElButton>
        </div>
      </template>
    </ElDialog>

    <TeamAccountDetailDialog
      v-model="detailOpen"
      :account-id="selectedAccountID"
      :account="selectedAccount"
      @updated="handleDetailUpdated"
      @transfer-requested="openTransfer"
    />

    <TeamAccountTransferDialog
      v-model="transferOpen"
      :account="transferAccount"
      @transferred="handleTransferred"
    />
  </section>
</template>
