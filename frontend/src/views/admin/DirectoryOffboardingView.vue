<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { disableDirectoryRelayUser, listDirectoryOffboardingCandidates } from '@/api/directory'
import type { DirectoryOffboardingCandidate } from '@/types'

const route = useRoute()
const router = useRouter()
const candidates = ref<DirectoryOffboardingCandidate[]>([])
const sourceId = ref<number>(Number(route.query.source_id || 1))
const q = ref(typeof route.query.q === 'string' ? route.query.q : '')
const confirmations = ref<Record<number, string>>({})
const loading = ref(false)
const message = ref('')
const error = ref('')

const hasCandidates = computed(() => candidates.value.length > 0)

onMounted(loadCandidates)

watch(sourceId, () => {
  void router.replace({ query: { ...route.query, source_id: String(sourceId.value || 1) } })
})

async function loadCandidates() {
  loading.value = true
  error.value = ''
  try {
    const params = { source_id: sourceId.value || 1, q: q.value.trim() }
    const res = await listDirectoryOffboardingCandidates(params)
    candidates.value = res.data.data?.items ?? []
  } catch (e: any) {
    error.value = e?.response?.data?.message || e?.message || 'Failed to load offboarding candidates'
  } finally {
    loading.value = false
  }
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
      source_id: sourceId.value || 1,
      confirm_email: confirmations.value[candidate.user_id].trim(),
      reason: 'missing_from_latest_full_company_directory',
    })
    message.value = `${candidate.email} disabled`
    await loadCandidates()
  } catch (e: any) {
    error.value = e?.response?.data?.message || e?.message || 'Failed to disable relay user'
  }
}
</script>

<template>
  <AppLayout>
    <div class="space-y-5">
      <div class="flex flex-col gap-3 sm:flex-row sm:items-end sm:justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900">Directory Offboarding</h1>
          <p class="text-sm text-gray-500">Review users missing from the latest full-company directory snapshot.</p>
        </div>
        <div class="flex flex-wrap gap-2">
          <input v-model.number="sourceId" type="number" min="1" class="w-28 rounded-md border border-gray-300 px-3 py-2 text-sm" aria-label="Source ID" />
          <input v-model="q" type="search" class="w-56 rounded-md border border-gray-300 px-3 py-2 text-sm" placeholder="Search users" />
          <button type="button" class="rounded-md border border-gray-300 px-3 py-2 text-sm text-gray-700 hover:bg-gray-50" @click="loadCandidates">Search</button>
        </div>
      </div>

      <div class="rounded-md border border-amber-200 bg-amber-50 p-3 text-sm text-amber-800">
        Disabling a candidate disables upstream AI access and revokes local AI Efficiency tokens. Subscriptions are not removed automatically.
      </div>
      <div v-if="message" class="rounded-md bg-green-50 p-3 text-sm text-green-700">{{ message }}</div>
      <div v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</div>

      <div class="overflow-hidden rounded-lg border border-gray-200 bg-white">
        <table class="min-w-full divide-y divide-gray-200 text-sm">
          <thead class="bg-gray-50 text-left text-xs font-medium uppercase tracking-wide text-gray-500">
            <tr>
              <th class="px-4 py-3">User</th>
              <th class="px-4 py-3">Relay</th>
              <th class="px-4 py-3">Reason</th>
              <th class="px-4 py-3">Confirmation</th>
              <th class="px-4 py-3 text-right">Action</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-100">
            <tr v-if="loading">
              <td colspan="5" class="px-4 py-6 text-center text-gray-500">Loading</td>
            </tr>
            <tr v-else-if="!hasCandidates">
              <td colspan="5" class="px-4 py-6 text-center text-gray-500">No offboarding candidates</td>
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
                  Disable relay user
                </button>
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>
  </AppLayout>
</template>
