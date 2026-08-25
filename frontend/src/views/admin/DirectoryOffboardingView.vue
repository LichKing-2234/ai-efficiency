<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { useMediaQuery } from '@/composables/useMediaQuery'
import { disableDirectoryRelayUser, listDirectoryOffboardingCandidates } from '@/api/directory'
import { useI18n } from '@/i18n'
import { directoryOffboardingMessages } from '@/locales/directoryOffboarding'
import { useWorkItemsStore } from '@/stores/workItems'
import type { DirectoryOffboardingCandidate } from '@/types'
import { authSourceLabel, offboardingReasonLabel, offboardingStatusLabel } from '@/utils/displayLabels'
import { createFeatureTranslator } from '@/utils/featureI18n'
import { FULL_PAGE_SIZES, fullPageSize, positivePage } from '@/utils/pagination'

const route = useRoute()
const router = useRouter()
const { locale, t: baseT } = useI18n()
const t = createFeatureTranslator(locale, baseT, 'directoryOffboarding.', directoryOffboardingMessages)
const workItems = useWorkItemsStore()
const candidates = ref<DirectoryOffboardingCandidate[]>([])
const q = ref(typeof route.query.q === 'string' ? route.query.q : '')
const page = ref(positivePage(route.query.page))
const pageSize = ref(fullPageSize(route.query.page_size))
const desktopPagination = useMediaQuery('(min-width: 768px)', true)
const total = ref(0)
const confirmations = ref<Record<number, string>>({})
const loading = ref(false)
const disableDialogOpen = ref(false)
const selectedCandidate = ref<DirectoryOffboardingCandidate | null>(null)
const disablingUserID = ref<number | null>(null)
const disableError = ref('')
const message = ref('')
const error = ref('')
let requestGeneration = 0

const hasCandidates = computed(() => candidates.value.length > 0)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)))
const pageStart = computed(() => total.value === 0 ? 0 : ((page.value - 1) * pageSize.value) + 1)
const pageEnd = computed(() => Math.min(total.value, page.value * pageSize.value))
const showPagination = computed(() => hasCandidates.value && total.value > pageSize.value)
const disableConfirmationMatches = computed(() => (
  selectedCandidate.value ? confirmed(selectedCandidate.value) : false
))

onMounted(loadCandidates)

async function loadCandidates(allowPageClamp = true, pushHistory = false) {
  const generation = ++requestGeneration
  loading.value = true
  error.value = ''
  try {
    const params = { q: q.value.trim(), page: page.value, page_size: pageSize.value }
    const res = await listDirectoryOffboardingCandidates(params)
    if (generation !== requestGeneration) return undefined
    const data = res.data.data
    candidates.value = data?.items ?? []
    total.value = data?.total ?? candidates.value.length
    page.value = data?.page ?? page.value
    pageSize.value = data?.page_size ?? pageSize.value
    const lastPage = Math.max(1, Math.ceil(total.value / pageSize.value))
    if (allowPageClamp && page.value > lastPage) {
      page.value = lastPage
      return loadCandidates(false, pushHistory)
    }
    syncQuery(pushHistory)
    return true
  } catch (e: any) {
    if (generation !== requestGeneration) return undefined
    error.value = e?.response?.data?.message || e?.message || t('directoryOffboarding.loadFailed')
    return false
  } finally {
    if (generation === requestGeneration) loading.value = false
  }
}

function syncQuery(pushHistory = false) {
  const query: Record<string, string> = {}
  if (q.value.trim()) query.q = q.value.trim()
  if (page.value > 1) query.page = String(page.value)
  if (pageSize.value !== 20) query.page_size = String(pageSize.value)
  if (pushHistory) void router.push({ query })
  else void router.replace({ query })
}

async function searchCandidates() {
  page.value = 1
  await loadCandidates(true, true)
}

async function changePage(nextPage: number) {
  if (nextPage < 1 || nextPage > totalPages.value || nextPage === page.value || loading.value) return
  const previousPage = page.value
  page.value = nextPage
  if (await loadCandidates(true, true) === false) page.value = previousPage
}

async function changePageSize(nextPageSize: number) {
  const previousPage = page.value
  const previousPageSize = pageSize.value
  page.value = 1
  pageSize.value = nextPageSize
  if (await loadCandidates(true, true) === false) {
    page.value = previousPage
    pageSize.value = previousPageSize
  }
}

watch(
  () => route.fullPath,
  () => {
    if (route.path !== '/admin/directory/offboarding') return
    const nextQuery = typeof route.query.q === 'string' ? route.query.q : ''
    const nextPage = positivePage(route.query.page)
    const nextPageSize = fullPageSize(route.query.page_size)
    if (q.value === nextQuery && page.value === nextPage && pageSize.value === nextPageSize) return
    q.value = nextQuery
    page.value = nextPage
    pageSize.value = nextPageSize
    void loadCandidates()
  },
)

function confirmed(candidate: DirectoryOffboardingCandidate) {
  return (confirmations.value[candidate.user_id] || '').trim().toLowerCase() === candidate.email.trim().toLowerCase()
}

function formatTimestamp(value: string | null | undefined) {
  if (!value) return ''
  const date = new Date(value)
  if (Number.isNaN(date.getTime())) return ''
  return new Intl.DateTimeFormat(locale.value, {
    dateStyle: 'medium',
    timeStyle: 'short',
  }).format(date)
}

function offboardingStatusType(status: string | null | undefined) {
  if (status === 'succeeded') return 'success'
  if (status === 'running') return 'warning'
  if (status === 'failed' || status === 'partial_failed') return 'danger'
  return 'info'
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
    page.value = Math.min(page.value, Math.max(1, Math.ceil(remainingTotal / pageSize.value)))
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
      <div>
        <h1 class="text-2xl font-bold text-gray-900">{{ t('directoryOffboarding.title') }}</h1>
        <p class="text-sm text-gray-500">{{ t('directoryOffboarding.subtitle') }}</p>
      </div>

      <div class="max-w-5xl">
        <ElAlert
          data-testid="offboarding-warning"
          :title="t('directoryOffboarding.warning')"
          type="warning"
          :closable="false"
          show-icon
        />
      </div>
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

      <section data-testid="offboarding-work-surface" class="max-w-5xl overflow-hidden rounded-lg border border-slate-200 bg-white shadow-sm">
        <div class="flex flex-col gap-2 border-b border-slate-200 p-4 sm:flex-row sm:items-center sm:justify-between">
          <div class="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row">
            <ElInput
              v-model="q"
              type="search"
              class="w-full sm:max-w-64"
              :placeholder="t('directoryOffboarding.searchPlaceholder')"
            />
            <ElButton data-testid="offboarding-search" class="shrink-0" @click="searchCandidates">
              {{ t('adminUsers.search') }}
            </ElButton>
          </div>
        </div>

        <div class="p-4">
          <ElSkeleton v-if="loading && !hasCandidates" :rows="3" animated />
          <ElEmpty v-else-if="!error && !hasCandidates" :description="t('directoryOffboarding.empty')" />
          <div
            v-else
            data-testid="offboarding-candidate-grid"
            class="grid gap-4"
            :class="candidates.length === 1 ? 'grid-cols-1' : 'lg:grid-cols-2'"
          >
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
                <ElTag class="mt-2" effect="plain">{{ authSourceLabel(candidate.auth_source, t) }}</ElTag>
              </div>
            </template>

            <dl class="grid gap-3 text-sm sm:grid-cols-2 xl:grid-cols-3">
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('directoryOffboarding.relay') }}</dt>
                <dd class="mt-1 break-all text-gray-700">{{ candidate.relay_user_id }}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('directoryOffboarding.reason') }}</dt>
                <dd class="mt-1 break-words text-gray-700">{{ offboardingReasonLabel(candidate.reason, t) }}</dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('directoryOffboarding.latestRun') }}</dt>
                <dd class="mt-1 text-gray-700">
                  {{ t('directoryOffboarding.runSummary', { id: candidate.directory_run_id }) }}
                  <span v-if="formatTimestamp(candidate.directory_run_at)" class="text-gray-500"> · {{ formatTimestamp(candidate.directory_run_at) }}</span>
                </dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('directoryOffboarding.actionStatus') }}</dt>
                <dd class="mt-1">
                  <ElTag :type="offboardingStatusType(candidate.offboarding_status)" size="small">
                    {{ offboardingStatusLabel(candidate.offboarding_status, t) }}
                  </ElTag>
                </dd>
              </div>
              <div>
                <dt class="text-xs font-medium uppercase tracking-wide text-gray-400">{{ t('directoryOffboarding.tokenAccess') }}</dt>
                <dd class="mt-1 text-gray-700">
                  {{ candidate.token_valid_after
                    ? t('directoryOffboarding.tokenRevoked', { time: formatTimestamp(candidate.token_valid_after) })
                    : t('directoryOffboarding.tokenNotRevoked') }}
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
        <div
          v-if="showPagination"
          class="flex flex-wrap items-center justify-between gap-3 border-t border-slate-200 px-4 py-3"
        >
          <span data-testid="offboarding-page-range" class="hidden text-sm text-slate-500 md:block">
            {{ baseT('pagination.range', { start: pageStart, end: pageEnd, total }) }}
          </span>
          <ElPagination
            data-testid="offboarding-pagination"
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
              {{ baseT('pagination.pageOf', { page, pages: totalPages }) }}
            </span>
          </ElPagination>
        </div>
      </section>

      <ElDialog
        v-if="selectedCandidate"
        :model-value="disableDialogOpen"
        append-to-body
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
