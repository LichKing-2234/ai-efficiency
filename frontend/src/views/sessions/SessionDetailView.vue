<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { getSession } from '@/api/session'
import type { Session } from '@/types'

const route = useRoute()
const router = useRouter()
const loading = ref(true)
const session = ref<Session | null>(null)
let currentLoadToken = 0

function formatDate(value?: string | null) {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

async function loadSession(sessionId: string) {
  const loadToken = ++currentLoadToken
  loading.value = true
  try {
    const res = await getSession(sessionId)
    if (loadToken != currentLoadToken) return
    session.value = res.data.data ?? null
  } catch {
    if (loadToken != currentLoadToken) return
    session.value = null
    router.replace('/sessions')
  } finally {
    if (loadToken != currentLoadToken) return
    loading.value = false
  }
}

watch(
  () => String(route.params.id || ''),
  (sessionId) => {
    void loadSession(sessionId)
  },
  { immediate: true },
)
</script>

<template>
  <AppLayout>
    <div v-if="loading" class="text-center text-gray-500 py-12">Loading...</div>

    <div v-else-if="session" class="mx-auto max-w-7xl space-y-5 px-4 py-6 sm:px-6 lg:px-8">
      <div>
        <button class="text-sm text-indigo-600 hover:text-indigo-800" @click="router.push('/sessions')">
          &larr; Back to Sessions
        </button>
        <div class="mt-2">
          <h1 class="text-2xl font-bold text-gray-900">Session {{ session.id }}</h1>
          <p class="text-sm text-gray-500">{{ session.status }} · {{ session.branch }}</p>
        </div>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <h2 class="text-sm font-semibold text-gray-900 uppercase tracking-wide">Session Audit</h2>
        <table class="mt-3 w-full text-sm">
          <tbody>
            <tr>
              <td class="py-1 pr-4 text-gray-400">Provider</td>
              <td class="py-1 text-gray-900">{{ session.provider_name || '—' }}</td>
            </tr>
            <tr>
              <td class="py-1 pr-4 text-gray-400">Relay Key</td>
              <td class="py-1 text-gray-900">{{ session.relay_api_key_id ?? '—' }}</td>
            </tr>
            <tr>
              <td class="py-1 pr-4 text-gray-400">Runtime Ref</td>
              <td class="py-1 text-gray-900 font-mono">{{ session.runtime_ref || '—' }}</td>
            </tr>
            <tr>
              <td class="py-1 pr-4 text-gray-400">Workspace Root</td>
              <td class="py-1 text-gray-900 font-mono">{{ session.initial_workspace_root || '—' }}</td>
            </tr>
            <tr>
              <td class="py-1 pr-4 text-gray-400">Last Seen</td>
              <td class="py-1 text-gray-900">{{ formatDate(session.last_seen_at) }}</td>
            </tr>
          </tbody>
        </table>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <h2 class="text-sm font-semibold text-gray-900 uppercase tracking-wide">Session Workspaces</h2>
        <div v-if="session.edges?.session_workspaces?.length" class="mt-3 overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead>
              <tr class="text-xs uppercase text-gray-400">
                <th class="px-3 py-2 text-left font-medium">Workspace ID</th>
                <th class="px-3 py-2 text-left font-medium">Workspace Root</th>
                <th class="px-3 py-2 text-left font-medium">Binding</th>
                <th class="px-3 py-2 text-left font-medium">Last Seen</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <tr v-for="workspace in session.edges.session_workspaces" :key="workspace.workspace_id">
                <td class="px-3 py-2 font-mono text-xs text-gray-600">{{ workspace.workspace_id }}</td>
                <td class="px-3 py-2 font-mono text-xs text-gray-700">{{ workspace.workspace_root }}</td>
                <td class="px-3 py-2 text-xs text-gray-600">{{ workspace.binding_source }}</td>
                <td class="px-3 py-2 text-xs text-gray-500">{{ formatDate(workspace.last_seen_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="mt-3 text-sm text-gray-400">No workspace records.</p>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <h2 class="text-sm font-semibold text-gray-900 uppercase tracking-wide">Commit Checkpoints</h2>
        <div v-if="session.edges?.commit_checkpoints?.length" class="mt-3 overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead>
              <tr class="text-xs uppercase text-gray-400">
                <th class="px-3 py-2 text-left font-medium">Commit SHA</th>
                <th class="px-3 py-2 text-left font-medium">Binding Source</th>
                <th class="px-3 py-2 text-left font-medium">Captured At</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <tr v-for="checkpoint in session.edges.commit_checkpoints" :key="checkpoint.event_id || checkpoint.commit_sha">
                <td class="px-3 py-2 font-mono text-xs text-gray-700">{{ checkpoint.commit_sha }}</td>
                <td class="px-3 py-2 text-xs text-gray-600">{{ checkpoint.binding_source }}</td>
                <td class="px-3 py-2 text-xs text-gray-500">{{ formatDate(checkpoint.captured_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="mt-3 text-sm text-gray-400">No checkpoints captured.</p>
      </div>
    </div>

    <div v-else class="text-center text-gray-500 py-12">Session not found.</div>
  </AppLayout>
</template>
