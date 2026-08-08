<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { disableDirectoryRelayUser, listDirectoryOffboardingCandidates } from '@/api/directory'
import { useI18n } from '@/i18n'
import { useWorkItemsStore } from '@/stores/workItems'
import type { DirectoryOffboardingCandidate } from '@/types'

const route = useRoute()
const { t } = useI18n()
const workItems = useWorkItemsStore()
const candidates = ref<DirectoryOffboardingCandidate[]>([])
const q = ref(typeof route.query.q === 'string' ? route.query.q : '')
const page = ref(1)
const pageSize = 20
const total = ref(0)
const confirmations = ref<Record<number, string>>({})
const loading = ref(false)
const disableDialogOpen = ref(false)
const selectedCandidate = ref<DirectoryOffboardingCandidate | null>(null)
const disablingUserID = ref<number | null>(null)
const disableError = ref('')
const message = ref('')
const error = ref('')

const hasCandidates = computed(() => candidates.value.length > 0)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const canGoPrevious = computed(() => page.value > 1)
const canGoNext = computed(() => page.value < totalPages.value)
const disableConfirmationMatches = computed(() => (
  selectedCandidate.value ? confirmed(selectedCandidate.value) : false
))

onMounted(loadCandidates)

async function loadCandidates(allowPageClamp = true) {
  loading.value = true
  error.value = ''
  try {
    const params = { q: q.value.trim(), page: page.value, page_size: pageSize }
    const res = await listDirectoryOffboardingCandidates(params)
    const data = res.data.data
    candidates.value = data?.items ?? []
    total.value = data?.total ?? candidates.value.length
    page.value = data?.page ?? page.value
    const lastPage = Math.max(1, Math.ceil(total.value / pageSize))
    if (allowPageClamp && page.value > lastPage) {
      page.value = lastPage
      await loadCandidates(false)
    }
  } catch (e: any) {
    error.value = e?.response?.data?.message || e?.message || t('directoryOffboarding.loadFailed')
  } finally {
    loading.value = false
  }
}

async function searchCandidates() {
  page.value = 1
  await loadCandidates()
}

async function previousPage() {
  if (!canGoPrevious.value || loading.value) return
  page.value -= 1
  await loadCandidates()
}

async function nextPage() {
  if (!canGoNext.value || loading.value) return
  page.value += 1
  await loadCandidates()
}

function confirmed(candidate: DirectoryOffboardingCandidate) {
  return (confirmations.value[candidate.user_id] || '').trim().toLowerCase() === candidate.email.trim().toLowerCase()
}

function openDisableDialog(candidate: DirectoryOffboardingCandidate) {
  if (disablingUserID.value !== null) return
  message.value = ''
  error.value = ''
  disableError.value = ''
  selectedCandidate.value = candidate
  disableDialogOpen.value = true
}

function closeDisableDialog() {
  if (disablingUserID.value !== null) return
  disableDialogOpen.value = false
  selectedCandidate.value = null
}

async function disableCandidate(candidate: DirectoryOffboardingCandidate) {
  if (!confirmed(candidate) || disablingUserID.value !== null) return
  disablingUserID.value = candidate.user_id
  message.value = ''
  error.value = ''
  disableError.value = ''
  try {
    await disableDirectoryRelayUser(candidate.user_id, {
      confirm_email: confirmations.value[candidate.user_id].trim(),
      reason: 'missing_from_latest_full_company_directory',
    })
    message.value = t('directoryOffboarding.disabled', { email: candidate.email })
    const remainingTotal = Math.max(0, total.value - 1)
    page.value = Math.min(page.value, Math.max(1, Math.ceil(remainingTotal / pageSize)))
    workItems.invalidateCounts()
    await Promise.all([
      loadCandidates(),
      workItems.loadCounts({ force: true }),
    ])
    disableDialogOpen.value = false
    selectedCandidate.value = null
  } catch (e: any) {
    disableError.value = e?.response?.data?.message || e?.message || t('directoryOffboarding.disableFailed')
  } finally {
    disablingUserID.value = null
  }
}
</script>

<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900">{{ t('directoryOffboarding.title') }}</h1>
          <p class="text-sm text-gray-500">{{ t('directoryOffboarding.subtitle') }}</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <ElInput
            v-model="q"
            type="search"
            class="w-56"
            :placeholder="t('directoryOffboarding.searchPlaceholder')"
          />
          <ElButton data-testid="offboarding-search" @click="searchCandidates">
            {{ t('adminUsers.search') }}
          </ElButton>
        </div>
      </div>

      <ElAlert
        data-testid="offboarding-warning"
        :title="t('directoryOffboarding.warning')"
        type="warning"
        :closable="false"
        show-icon
      />
      <ElAlert
        v-if="message"
        data-testid="offboarding-success"
        :title="message"
        type="success"
        :closable="false"
        show-icon
      />
      <ElAlert
        v-if="error"
        data-testid="offboarding-error"
        :title="error"
        type="error"
        :closable="false"
        show-icon
      />

      <div>
        <ElSkeleton v-if="loading" :rows="3" animated class="p-4" />
        <ElEmpty v-else-if="!error && !hasCandidates" :description="t('directoryOffboarding.empty')" />
        <div v-else class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
          <ElCard
            v-for="candidate in candidates"
            :key="candidate.user_id"
            :data-testid="`offboarding-candidate-${candidate.user_id}`"
            shadow="never"
            class="min-w-0"
          >
            <template #header>
              <div class="min-w-0">
                <div class="truncate font-medium text-gray-900">{{ candidate.username }}</div>
                <div class="break-all text-sm text-gray-500">{{ candidate.email }}</div>
                <ElTag class="mt-2" effect="plain">{{ candidate.auth_source }}</ElTag>
              </div>
            </template>

            <dl class="space-y-3 text-sm">
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('directoryOffboarding.relay') }}</dt>
                <dd class="mt-1 break-all text-gray-700">{{ candidate.relay_user_id }}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('directoryOffboarding.reason') }}</dt>
                <dd class="mt-1 break-words text-gray-700">{{ candidate.reason }}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('directoryOffboarding.confirmation') }}</dt>
                <dd class="mt-1 break-all text-gray-500">
                  {{ t('adminUsers.disableUserConfirmHint', { email: candidate.email }) }}
                </dd>
              </div>
            </dl>

            <div class="mt-4 flex justify-end">
              <ElButton
                :data-testid="`disable-relay-user-${candidate.user_id}`"
                type="danger"
                class="w-full sm:w-auto"
                :disabled="disablingUserID !== null"
                @click="openDisableDialog(candidate)"
              >
                {{ t('directoryOffboarding.disableRelayUser') }}
              </ElButton>
            </div>
          </ElCard>
        </div>
      </div>
      <div class="flex flex-wrap items-center justify-end gap-2 text-xs text-gray-500">
        <span>{{ total }} {{ t('adminUsers.totalSuffix') }}</span>
        <ElButton
          data-testid="offboarding-prev-page"
          :disabled="!canGoPrevious || loading"
          @click="previousPage"
        >
          {{ t('adminUsers.prev') }}
        </ElButton>
        <span data-testid="offboarding-page-status">{{ t('adminUsers.page') }} {{ page }} / {{ totalPages }}</span>
        <ElButton
          data-testid="offboarding-next-page"
          :disabled="!canGoNext || loading"
          @click="nextPage"
        >
          {{ t('adminUsers.next') }}
        </ElButton>
      </div>

      <ElDialog
        v-if="selectedCandidate"
        :model-value="disableDialogOpen"
        :teleported="false"
        :show-close="false"
        align-center
        width="min(100%, 32rem)"
        :close-on-click-modal="disablingUserID === null"
        :close-on-press-escape="disablingUserID === null"
        @update:model-value="(value) => { if (!value) closeDisableDialog() }"
      >
        <template #header>
          <div data-testid="offboarding-disable-dialog" class="flex items-start justify-between gap-4">
            <div class="min-w-0">
              <h2 class="text-base font-semibold text-gray-900">{{ t('adminUsers.disableUserConfirmTitle') }}</h2>
              <p class="mt-1 truncate text-sm text-gray-500">{{ selectedCandidate.email }}</p>
            </div>
            <ElButton :disabled="disablingUserID !== null" @click="closeDisableDialog">
              {{ t('adminUsers.closeDialog') }}
            </ElButton>
          </div>
        </template>

        <ElAlert
          type="error"
          :closable="false"
          show-icon
          :title="t('directoryOffboarding.effectNotice')"
        />
        <label class="mt-4 block text-xs font-medium uppercase tracking-wide text-gray-500">
          {{ t('adminUsers.disableUserConfirmHint', { email: selectedCandidate.email }) }}
          <ElInput
            v-model="confirmations[selectedCandidate.user_id]"
            :data-testid="`confirm-email-${selectedCandidate.user_id}`"
            class="mt-1 block w-full"
            autofocus
            :placeholder="selectedCandidate.email"
            :disabled="disablingUserID !== null"
          />
        </label>
        <ElAlert
          v-if="disableError"
          data-testid="offboarding-disable-error"
          class="mt-3"
          type="error"
          :closable="false"
          show-icon
          :title="disableError"
        />

        <template #footer>
          <div class="flex justify-end gap-2">
            <ElButton :disabled="disablingUserID !== null" @click="closeDisableDialog">
              {{ t('adminUsers.cancelDisableUser') }}
            </ElButton>
            <ElButton
              :data-testid="`confirm-disable-relay-user-${selectedCandidate.user_id}`"
              type="danger"
              :loading="disablingUserID === selectedCandidate.user_id"
              :disabled="!disableConfirmationMatches || disablingUserID !== null"
              @click="disableCandidate(selectedCandidate)"
            >
              {{ disablingUserID === selectedCandidate.user_id ? t('adminUsers.working') : t('adminUsers.confirmDisableUser') }}
            </ElButton>
          </div>
        </template>
      </ElDialog>
    </div>
  </AppLayout>
</template>
