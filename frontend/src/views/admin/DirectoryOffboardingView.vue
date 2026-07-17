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
const message = ref('')
const error = ref('')

const hasCandidates = computed(() => candidates.value.length > 0)
const totalPages = computed(() => Math.max(1, Math.ceil(total.value / pageSize)))
const canGoPrevious = computed(() => page.value > 1)
const canGoNext = computed(() => page.value < totalPages.value)

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

async function disableCandidate(candidate: DirectoryOffboardingCandidate) {
  if (!confirmed(candidate)) return
  message.value = ''
  error.value = ''
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
  } catch (e: any) {
    error.value = e?.response?.data?.message || e?.message || t('directoryOffboarding.disableFailed')
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
          <input v-model="q" type="search" class="w-56 rounded-md border border-gray-300 px-3 py-2 text-sm" :placeholder="t('directoryOffboarding.searchPlaceholder')" />
          <button data-testid="offboarding-search" type="button" class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50" @click="searchCandidates">{{ t('adminUsers.search') }}</button>
        </div>
      </div>

      <div class="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">
        {{ t('directoryOffboarding.warning') }}
      </div>
      <div v-if="message" class="rounded-md bg-green-50 p-3 text-sm text-green-700">{{ message }}</div>
      <div v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</div>

      <div class="overflow-hidden rounded-lg border border-gray-200 bg-white">
        <table class="min-w-full divide-y divide-gray-200 text-sm">
          <thead class="bg-gray-50 text-left text-xs font-medium uppercase tracking-wide text-gray-500">
            <tr>
              <th class="px-4 py-3">{{ t('adminUsers.user') }}</th>
              <th class="px-4 py-3">{{ t('directoryOffboarding.relay') }}</th>
              <th class="px-4 py-3">{{ t('directoryOffboarding.reason') }}</th>
              <th class="px-4 py-3">{{ t('directoryOffboarding.confirmation') }}</th>
              <th class="px-4 py-3 text-right">{{ t('settings.actions') }}</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            <tr v-if="loading">
              <td colspan="5" class="px-4 py-6 text-center text-gray-500">{{ t('settings.loading') }}</td>
            </tr>
            <tr v-else-if="!hasCandidates">
              <td colspan="5" class="px-4 py-6 text-center text-gray-500">{{ t('directoryOffboarding.empty') }}</td>
            </tr>
            <tr v-for="candidate in candidates" v-else :key="candidate.user_id">
              <td class="px-4 py-3">
                <div class="font-medium text-gray-900">{{ candidate.username }}</div>
                <div class="text-gray-500">{{ candidate.email }}</div>
                <div class="text-xs text-gray-400">{{ candidate.auth_source }}</div>
              </td>
              <td class="px-4 py-3 text-gray-700">{{ candidate.relay_user_id }}</td>
              <td class="px-4 py-3 text-gray-700">{{ candidate.reason }}</td>
              <td class="px-4 py-3">
                <input
                  :data-testid="`confirm-email-${candidate.user_id}`"
                  v-model="confirmations[candidate.user_id]"
                  type="text"
                  class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                  :placeholder="candidate.email"
                />
              </td>
              <td class="px-4 py-3 text-right">
                <button
                  :data-testid="`disable-relay-user-${candidate.user_id}`"
                  type="button"
                  class="rounded-md bg-red-600 px-3 py-2 text-sm font-medium text-white disabled:opacity-40"
                  :disabled="!confirmed(candidate)"
                  @click="disableCandidate(candidate)"
                >
                  {{ t('directoryOffboarding.disableRelayUser') }}
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
      <div class="flex flex-wrap items-center justify-end gap-2 text-xs text-gray-500">
        <span>{{ total }} {{ t('adminUsers.totalSuffix') }}</span>
        <button
          data-testid="offboarding-prev-page"
          type="button"
          class="rounded border border-gray-200 px-2 py-1 disabled:opacity-40"
          :disabled="!canGoPrevious || loading"
          @click="previousPage"
        >
          {{ t('adminUsers.prev') }}
        </button>
        <span data-testid="offboarding-page-status">{{ t('adminUsers.page') }} {{ page }} / {{ totalPages }}</span>
        <button
          data-testid="offboarding-next-page"
          type="button"
          class="rounded border border-gray-200 px-2 py-1 disabled:opacity-40"
          :disabled="!canGoNext || loading"
          @click="nextPage"
        >
          {{ t('adminUsers.next') }}
        </button>
      </div>
    </div>
  </AppLayout>
</template>
