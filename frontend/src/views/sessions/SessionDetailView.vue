<script setup lang="ts">
import { ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { getSession } from '@/api/session'
import type { AgentMetadataEvent, Session, SessionUsageEvent } from '@/types'

type RawPanel = {
  title: string
  content: string
}

const route = useRoute()
const router = useRouter()
const loading = ref(true)
const session = ref<Session | null>(null)
const rawPanel = ref<RawPanel | null>(null)
let currentLoadToken = 0

function formatDate(value?: string | null) {
  if (!value) return '—'
  return new Date(value).toLocaleString()
}

function formatNumber(value?: number | null) {
  if (value == null) return '—'
  return String(value)
}

function formatPercent(value?: number | null) {
  if (value == null) return '—'
  return `${value}%`
}

function formatRawJSON(value?: unknown, emptyLabel = 'No raw data.') {
  if (value == null) return emptyLabel
  return JSON.stringify(value, null, 2)
}

function openUsageRaw(usage: SessionUsageEvent) {
  rawPanel.value = {
    title: 'Raw Response',
    content: formatRawJSON(usage.raw_response, 'No raw response.'),
  }
}

function openAgentRaw(event: AgentMetadataEvent) {
  rawPanel.value = {
    title: 'Raw Event',
    content: formatRawJSON(event.raw_payload, 'No raw event.'),
  }
}

function closeRawPanel() {
  rawPanel.value = null
}

function agentRawKey(event: AgentMetadataEvent) {
  return `${event.source}-${event.source_session_id || event.observed_at}`
}

function agentTokenTotal(event: AgentMetadataEvent) {
  if (event.usage_unit !== 'token') return '—'
  return String(
    (event.input_tokens ?? 0) +
    (event.cached_input_tokens ?? 0) +
    (event.output_tokens ?? 0) +
    (event.reasoning_tokens ?? 0),
  )
}

async function loadSession(sessionId: string) {
  const loadToken = ++currentLoadToken
  loading.value = true
  rawPanel.value = null
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
              <td class="py-1 pr-4 text-gray-400">Owner</td>
              <td class="py-1 text-gray-900">{{ session.edges?.user?.username || '—' }}</td>
            </tr>
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

      <div class="rounded-lg bg-white p-5 shadow">
        <h2 class="text-sm font-semibold text-gray-900 uppercase tracking-wide">Session Usage</h2>
        <div v-if="session.edges?.session_usage_events?.length" class="mt-3 overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead>
              <tr class="text-xs uppercase text-gray-400">
                <th class="px-3 py-2 text-left font-medium">Model</th>
                <th class="px-3 py-2 text-left font-medium">Provider</th>
                <th class="px-3 py-2 text-left font-medium">Input</th>
                <th class="px-3 py-2 text-left font-medium">Cached Input</th>
                <th class="px-3 py-2 text-left font-medium">Output</th>
                <th class="px-3 py-2 text-left font-medium">Reasoning</th>
                <th class="px-3 py-2 text-left font-medium">Total</th>
                <th class="px-3 py-2 text-left font-medium">Status</th>
                <th class="px-3 py-2 text-left font-medium">Started</th>
                <th class="px-3 py-2 text-left font-medium">Raw</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <template v-for="usage in session.edges.session_usage_events" :key="usage.event_id">
                <tr>
                  <td class="px-3 py-2 font-mono text-xs text-gray-700">{{ usage.model }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ usage.provider_name }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ usage.input_tokens ?? '—' }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatNumber(usage.raw_metadata?.cached_input_tokens) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ usage.output_tokens ?? '—' }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatNumber(usage.raw_metadata?.reasoning_output_tokens) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ usage.total_tokens ?? '—' }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ usage.status }}</td>
                  <td class="px-3 py-2 text-xs text-gray-500">{{ formatDate(usage.started_at) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">
                    <button class="text-xs text-indigo-600 hover:text-indigo-800" @click="openUsageRaw(usage)">
                      Raw Response
                    </button>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
        <p v-else class="mt-3 text-sm text-gray-400">No usage records.</p>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <h2 class="text-sm font-semibold text-gray-900 uppercase tracking-wide">Agent Usage Snapshots</h2>
        <div v-if="session.edges?.agent_metadata_events?.length" class="mt-3 overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead>
              <tr class="text-xs uppercase text-gray-400">
                <th class="px-3 py-2 text-left font-medium">Source</th>
                <th class="px-3 py-2 text-left font-medium">Session</th>
                <th class="px-3 py-2 text-left font-medium">Unit</th>
                <th class="px-3 py-2 text-left font-medium">Input</th>
                <th class="px-3 py-2 text-left font-medium">Cached Input</th>
                <th class="px-3 py-2 text-left font-medium">Output</th>
                <th class="px-3 py-2 text-left font-medium">Reasoning</th>
                <th class="px-3 py-2 text-left font-medium">Total</th>
                <th class="px-3 py-2 text-left font-medium">Credit</th>
                <th class="px-3 py-2 text-left font-medium">Context %</th>
                <th class="px-3 py-2 text-left font-medium">Observed</th>
                <th class="px-3 py-2 text-left font-medium">Raw</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <template v-for="event in session.edges.agent_metadata_events" :key="agentRawKey(event)">
                <tr>
                  <td class="px-3 py-2 font-mono text-xs text-gray-700">{{ event.source }}</td>
                  <td class="px-3 py-2 font-mono text-xs text-gray-600">{{ event.source_session_id || '—' }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ event.usage_unit }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatNumber(event.input_tokens) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatNumber(event.cached_input_tokens) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatNumber(event.output_tokens) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatNumber(event.reasoning_tokens) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ agentTokenTotal(event) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatNumber(event.credit_usage) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">{{ formatPercent(event.context_usage_pct) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-500">{{ formatDate(event.observed_at) }}</td>
                  <td class="px-3 py-2 text-xs text-gray-600">
                    <button class="text-xs text-indigo-600 hover:text-indigo-800" @click="openAgentRaw(event)">
                      Raw Event
                    </button>
                  </td>
                </tr>
              </template>
            </tbody>
          </table>
        </div>
        <p v-else class="mt-3 text-sm text-gray-400">No agent usage snapshots.</p>
      </div>

      <div class="rounded-lg bg-white p-5 shadow">
        <h2 class="text-sm font-semibold text-gray-900 uppercase tracking-wide">Session Events</h2>
        <div v-if="session.edges?.session_events?.length" class="mt-3 overflow-x-auto">
          <table class="min-w-full divide-y divide-gray-100 text-sm">
            <thead>
              <tr class="text-xs uppercase text-gray-400">
                <th class="px-3 py-2 text-left font-medium">Type</th>
                <th class="px-3 py-2 text-left font-medium">Source</th>
                <th class="px-3 py-2 text-left font-medium">Captured</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-50">
              <tr v-for="event in session.edges.session_events" :key="event.event_id">
                <td class="px-3 py-2 font-mono text-xs text-gray-700">{{ event.event_type }}</td>
                <td class="px-3 py-2 text-xs text-gray-600">{{ event.source }}</td>
                <td class="px-3 py-2 text-xs text-gray-500">{{ formatDate(event.captured_at) }}</td>
              </tr>
            </tbody>
          </table>
        </div>
        <p v-else class="mt-3 text-sm text-gray-400">No session events.</p>
      </div>
    </div>

    <div v-if="rawPanel" class="fixed inset-0 z-40">
      <button
        aria-label="Close raw panel backdrop"
        class="absolute inset-0 bg-gray-900/30"
        @click="closeRawPanel"
      />
      <div class="absolute inset-y-0 right-0 flex w-full max-w-3xl">
        <div
          data-testid="raw-panel"
          class="ml-auto flex h-full w-full flex-col bg-white shadow-2xl ring-1 ring-gray-200"
        >
          <div class="flex items-start justify-between border-b border-gray-200 px-4 py-3 sm:px-6">
            <h2 data-testid="raw-panel-title" class="text-sm font-semibold uppercase tracking-wide text-gray-900">
              {{ rawPanel.title }}
            </h2>
            <button
              class="rounded px-2 py-1 text-sm text-gray-500 hover:bg-gray-100 hover:text-gray-700"
              @click="closeRawPanel"
            >
              Close
            </button>
          </div>
          <div class="min-h-0 flex-1 p-4 sm:p-6">
            <pre
              data-testid="raw-panel-content"
              class="h-full overflow-auto rounded bg-gray-900 p-4 text-xs text-gray-100"
            >{{ rawPanel.content }}</pre>
          </div>
        </div>
      </div>
    </div>

    <div v-else class="text-center text-gray-500 py-12">Session not found.</div>
  </AppLayout>
</template>
