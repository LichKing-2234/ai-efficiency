<script setup lang="ts">
import { onMounted, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import { listProviders, createProvider, updateProvider, deleteProvider } from '@/api/scmProvider'
import { getLLMConfig, updateLLMConfig, testLLMConnection } from '@/api/settings'
import { getDeploymentStatus, checkForUpdate, applyUpdate, rollbackUpdate, restartDeployment } from '@/api/deployment'
import { waitForServiceRecovery } from '@/utils/deploymentRecovery'
import client from '@/api/client'
import type { DeploymentStatus, SCMProvider, UpdateStatus } from '@/types'

const providers = ref<SCMProvider[]>([])
const loading = ref(true)

// Add/Edit dialog
const showDialog = ref(false)
const editingId = ref<number | null>(null)
const form = ref({ name: '', type: 'github', base_url: 'https://api.github.com', token: '' })
const formError = ref('')
const formLoading = ref(false)

// Delete confirm
const showDeleteConfirm = ref<number | null>(null)

// LLM config
const llmForm = ref({ model: 'gpt-4', max_tokens_per_scan: 100000, system_prompt: '', user_prompt_template: '' })
const llmRelayURL = ref('')
const llmRelayAPIKey = ref('')
const llmRelayAdminAPIKey = ref('')
const llmEnabled = ref(false)
const llmSaving = ref(false)
const llmError = ref('')
const llmSuccess = ref('')
const llmTesting = ref(false)
const llmTestResult = ref<{ success: boolean; message: string; response?: string } | null>(null)
const showLLMTestDialog = ref(false)
const llmTestPromptDraft = ref('Hi')

// Deployment status
const deployment = ref<DeploymentStatus | null>(null)
const deploymentLoading = ref(false)
const deploymentActionLoading = ref(false)
const deploymentMessage = ref('')
const deploymentMessageKind = ref<'success' | 'error' | ''>('')

// LDAP config
const ldapForm = ref({ url: '', base_dn: '', bind_dn: '', bind_password: '', user_filter: '', tls: false })
const ldapSaving = ref(false)
const ldapTesting = ref(false)
const ldapError = ref('')
const ldapSuccess = ref('')

onMounted(async () => {
  await Promise.all([fetchProviders(), fetchLLMConfig(), fetchDeploymentStatus(), fetchLDAPConfig()])
})

async function fetchProviders() {
  loading.value = true
  try {
    const res = await listProviders()
    const data = res.data.data
    providers.value = Array.isArray(data) ? data : (data as any)?.items ?? []
  } catch {
    providers.value = []
  } finally {
    loading.value = false
  }
}

function openAddDialog() {
  editingId.value = null
  form.value = { name: '', type: 'github', base_url: 'https://api.github.com', token: '' }
  formError.value = ''
  showDialog.value = true
}

function openEditDialog(p: SCMProvider) {
  editingId.value = p.id
  form.value = { name: p.name, type: p.type, base_url: p.base_url, token: '' }
  formError.value = ''
  showDialog.value = true
}

function onTypeChange() {
  if (form.value.type === 'github') {
    form.value.base_url = 'https://api.github.com'
  } else {
    form.value.base_url = ''
  }
}

async function handleSubmit() {
  formError.value = ''
  if (!form.value.name) { formError.value = 'Name is required'; return }
  if (!form.value.base_url) { formError.value = 'Base URL is required'; return }

  formLoading.value = true
  try {
    if (editingId.value) {
      const data: any = { name: form.value.name, base_url: form.value.base_url }
      if (form.value.token) {
        data.credentials = { token: form.value.token }
      }
      await updateProvider(editingId.value, data)
    } else {
      await createProvider({
        name: form.value.name,
        type: form.value.type,
        base_url: form.value.base_url,
        credentials: { token: form.value.token },
      } as any)
    }
    showDialog.value = false
    await fetchProviders()
  } catch (e: any) {
    formError.value = e.response?.data?.message || 'Operation failed'
  } finally {
    formLoading.value = false
  }
}

async function confirmDelete(id: number) {
  try {
    await deleteProvider(id)
    showDeleteConfirm.value = null
    await fetchProviders()
  } catch {
    // delete failed
  }
}

function formatDate(date: string) {
  return new Date(date).toLocaleDateString()
}

// LLM config functions
function applyLLMConfig(data: any) {
  if (!data) return
  llmRelayURL.value = data.sub2api_url || data.relay_url || ''
  llmRelayAPIKey.value = data.sub2api_api_key || data.relay_api_key || ''
  llmRelayAdminAPIKey.value = data.relay_admin_api_key || ''
  llmForm.value = {
    model: data.model || 'gpt-4',
    max_tokens_per_scan: data.max_tokens_per_scan || 100000,
    system_prompt: data.system_prompt || '',
    user_prompt_template: data.user_prompt_template || '',
  }
  llmEnabled.value = !!data.enabled
}

async function fetchLLMConfig() {
  try {
    const res = await getLLMConfig()
    applyLLMConfig(res.data.data)
  } catch {
    // not configured yet
  }
}

async function handleSaveLLM() {
  llmError.value = ''
  llmSuccess.value = ''
  llmSaving.value = true
  try {
    const res = await updateLLMConfig({
      ...llmForm.value,
      relay_admin_api_key: llmRelayAdminAPIKey.value,
    })
    applyLLMConfig(res.data.data)
    llmSuccess.value = 'LLM configuration saved'
    setTimeout(() => { llmSuccess.value = '' }, 3000)
  } catch (e: any) {
    llmError.value = e.response?.data?.message || e.message || 'Failed to save'
  } finally {
    llmSaving.value = false
  }
}

function openLLMTestDialog() {
  llmTestPromptDraft.value = 'Hi'
  showLLMTestDialog.value = true
}

function closeLLMTestDialog() {
  showLLMTestDialog.value = false
}

async function handleTestLLM(prompt: string) {
  llmTestResult.value = null
  llmTesting.value = true
  try {
    const res = await testLLMConnection({ prompt })
    llmTestResult.value = res.data.data ?? null
  } catch (e: any) {
    llmTestResult.value = { success: false, message: e.response?.data?.message || e.message || 'Request failed' }
  } finally {
    llmTesting.value = false
  }
}

async function confirmTestLLM() {
  const prompt = llmTestPromptDraft.value
  closeLLMTestDialog()
  await handleTestLLM(prompt)
}

async function fetchDeploymentStatus() {
  deploymentLoading.value = true
  try {
    const res = await getDeploymentStatus()
    deployment.value = res.data.data ?? null
  } catch {
    deployment.value = null
  } finally {
    deploymentLoading.value = false
  }
}

function setDeploymentMessage(kind: 'success' | 'error', message: string) {
  deploymentMessageKind.value = kind
  deploymentMessage.value = message
}

function applyDeploymentUpdateStatus(status: UpdateStatus) {
  if (!deployment.value) return
  deployment.value = {
    ...deployment.value,
    update_status: status,
  }
}

function shouldWaitForRecovery(action: 'apply' | 'rollback' | 'restart') {
  return action === 'restart'
}

async function handleCheckUpdates() {
  deploymentActionLoading.value = true
  deploymentMessage.value = ''
  deploymentMessageKind.value = ''
  try {
    const res = await checkForUpdate()
    deployment.value = res.data.data ?? null
    setDeploymentMessage('success', 'Update check completed')
  } catch (e: any) {
    setDeploymentMessage('error', e.response?.data?.message || 'Failed to check updates')
  } finally {
    deploymentActionLoading.value = false
  }
}

async function handleApplyUpdate() {
  const targetVersion = deployment.value?.latest_release?.version?.trim()
  if (!targetVersion) {
    setDeploymentMessage('error', 'No target version available')
    return
  }

  deploymentActionLoading.value = true
  deploymentMessage.value = ''
  deploymentMessageKind.value = ''
  try {
    const res = await applyUpdate({ target_version: targetVersion })
    applyDeploymentUpdateStatus(res.data.data ?? { phase: 'unknown' })
    setDeploymentMessage('success', 'Update staged. Restart the service to run the new binary.')
  } catch (e: any) {
    setDeploymentMessage('error', e.response?.data?.message || 'Failed to apply update')
  } finally {
    deploymentActionLoading.value = false
  }
}

async function handleRollbackUpdate() {
  deploymentActionLoading.value = true
  deploymentMessage.value = ''
  deploymentMessageKind.value = ''
  try {
    const res = await rollbackUpdate()
    applyDeploymentUpdateStatus(res.data.data ?? { phase: 'unknown' })
    setDeploymentMessage('success', 'Rollback staged. Restart the service to run the restored binary.')
  } catch (e: any) {
    setDeploymentMessage('error', e.response?.data?.message || 'Failed to rollback update')
  } finally {
    deploymentActionLoading.value = false
  }
}

async function handleRestartDeployment() {
  deploymentActionLoading.value = true
  deploymentMessage.value = ''
  deploymentMessageKind.value = ''
  try {
    const res = await restartDeployment()
    applyDeploymentUpdateStatus(res.data.data ?? { phase: 'restart_requested' })
    setDeploymentMessage('success', 'Restart request submitted')
    if (shouldWaitForRecovery('restart')) {
      setDeploymentMessage('success', 'Restart requested. Waiting for service recovery...')
      await waitForServiceRecovery()
    }
  } catch (e: any) {
    setDeploymentMessage('error', e.response?.data?.message || 'Failed to restart service')
  } finally {
    deploymentActionLoading.value = false
  }
}

// LDAP config functions
async function fetchLDAPConfig() {
  try {
    const { data } = await client.get('/admin/settings/ldap')
    const settings = data.data
    ldapForm.value = {
      url: settings.url || '',
      base_dn: settings.base_dn || '',
      bind_dn: settings.bind_dn || '',
      bind_password: '',
      user_filter: settings.user_filter || '',
      tls: settings.tls || false,
    }
  } catch {
    // not configured yet
  }
}

async function handleSaveLDAP() {
  ldapError.value = ''
  ldapSuccess.value = ''
  if (!ldapForm.value.url) {
    ldapError.value = 'LDAP URL is required'
    return
  }
  ldapSaving.value = true
  try {
    await client.put('/admin/settings/ldap', ldapForm.value)
    ldapSuccess.value = 'LDAP configuration saved'
    setTimeout(() => { ldapSuccess.value = '' }, 3000)
  } catch (e: any) {
    ldapError.value = e.response?.data?.message || 'Failed to save'
  } finally {
    ldapSaving.value = false
  }
}

async function handleTestLDAP() {
  ldapError.value = ''
  ldapSuccess.value = ''
  ldapTesting.value = true
  try {
    await client.post('/admin/settings/ldap/test', ldapForm.value)
    ldapSuccess.value = 'LDAP connection successful'
    setTimeout(() => { ldapSuccess.value = '' }, 3000)
  } catch (e: any) {
    ldapError.value = e.response?.data?.message || 'Connection test failed'
  } finally {
    ldapTesting.value = false
  }
}
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex items-center justify-between">
        <h1 class="text-2xl font-bold text-gray-900">SCM Providers</h1>
        <button
          class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          @click="openAddDialog"
        >
          Add Provider
        </button>
      </div>

      <div v-if="loading" class="text-center text-gray-500 py-12">Loading...</div>

      <div v-else class="overflow-hidden rounded-lg bg-white shadow">
        <table class="min-w-full divide-y divide-gray-200">
          <thead class="bg-gray-50">
            <tr>
              <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Name</th>
              <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Type</th>
              <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Base URL</th>
              <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Status</th>
              <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Created</th>
              <th class="px-6 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-500">Actions</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200">
            <tr v-for="p in providers" :key="p.id">
              <td class="whitespace-nowrap px-6 py-4 text-sm font-medium text-gray-900">{{ p.name }}</td>
              <td class="whitespace-nowrap px-6 py-4">
                <span class="inline-flex rounded-full px-2 text-xs font-semibold leading-5"
                  :class="p.type === 'github' ? 'bg-gray-100 text-gray-800' : 'bg-blue-100 text-blue-800'">
                  {{ p.type }}
                </span>
              </td>
              <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500 font-mono text-xs">{{ p.base_url }}</td>
              <td class="whitespace-nowrap px-6 py-4">
                <span class="inline-flex rounded-full px-2 text-xs font-semibold leading-5 bg-green-100 text-green-800">
                  {{ p.status }}
                </span>
              </td>
              <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500">{{ formatDate(p.created_at) }}</td>
              <td class="whitespace-nowrap px-6 py-4 text-right text-sm space-x-3">
                <button class="text-indigo-600 hover:text-indigo-800" @click="openEditDialog(p)">Edit</button>
                <button
                  v-if="showDeleteConfirm !== p.id"
                  class="text-red-600 hover:text-red-800"
                  @click="showDeleteConfirm = p.id"
                >Delete</button>
                <span v-else class="space-x-2">
                  <button class="text-red-700 font-medium" @click="confirmDelete(p.id)">Confirm</button>
                  <button class="text-gray-500" @click="showDeleteConfirm = null">Cancel</button>
                </span>
              </td>
            </tr>
            <tr v-if="providers.length === 0">
              <td colspan="6" class="px-6 py-12 text-center text-sm text-gray-500">
                No SCM providers configured. Click "Add Provider" to connect GitHub or Bitbucket.
              </td>
            </tr>
          </tbody>
        </table>
      </div>
    </div>

    <!-- LLM Configuration -->
    <div class="mt-8 space-y-4">
      <h2 class="text-xl font-bold text-gray-900">LLM Configuration</h2>
      <div class="overflow-hidden rounded-lg bg-white shadow p-6">
        <div class="space-y-4">
          <div class="flex items-center justify-between">
            <span class="text-sm text-gray-500">Status</span>
            <span
              class="inline-flex rounded-full px-2 text-xs font-semibold leading-5"
              :class="llmEnabled ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-500'"
            >{{ llmEnabled ? 'Enabled' : 'Not configured' }}</span>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Relay URL</label>
            <input :value="llmRelayURL" type="text" disabled class="mt-1 block w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-500" />
            <p class="mt-1 text-xs text-gray-400">Configured via relay section in config.yaml</p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Relay API Key</label>
            <input :value="llmRelayAPIKey" type="text" disabled class="mt-1 block w-full rounded-md border border-gray-200 bg-gray-50 px-3 py-2 text-sm text-gray-500" />
            <p class="mt-1 text-xs text-gray-400">Used for relay LLM requests.</p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Relay Admin API Key</label>
            <input v-model="llmRelayAdminAPIKey" type="password" placeholder="admin-..." class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            <p class="mt-1 text-xs text-gray-400">Used as <code class="bg-gray-100 px-1 rounded">X-API-Key</code> for relay admin APIs.</p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Model</label>
            <input v-model="llmForm.model" type="text" placeholder="gpt-4" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">System Prompt</label>
            <textarea v-model="llmForm.system_prompt" rows="3" placeholder="Leave empty to use default" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm font-mono" />
            <p class="mt-1 text-xs text-gray-400">Override the system prompt used during LLM scan analysis. Leave empty for default.</p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">User Prompt Template</label>
            <textarea v-model="llmForm.user_prompt_template" rows="4" placeholder="Leave empty to use default. Use {repo_context} as placeholder." class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm font-mono" />
            <p class="mt-1 text-xs text-gray-400">Override the user prompt template. Use <code class="bg-gray-100 px-1 rounded">{repo_context}</code> placeholder for repo content.</p>
          </div>

          <div v-if="llmError" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ llmError }}</div>
          <div v-if="llmSuccess" class="rounded-md bg-green-50 p-3 text-sm text-green-700">{{ llmSuccess }}</div>

          <div v-if="llmTestResult" class="rounded-md p-3 text-sm" :class="llmTestResult.success ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'">
            <div>{{ llmTestResult.message }}</div>
            <pre v-if="llmTestResult.response" class="mt-2 whitespace-pre-wrap rounded-md bg-white/70 px-3 py-2 font-mono text-xs text-gray-700">{{ llmTestResult.response }}</pre>
          </div>

          <div class="flex justify-end space-x-3">
            <button @click="openLLMTestDialog" :disabled="llmTesting" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50">
              {{ llmTesting ? 'Testing...' : 'Test Connection' }}
            </button>
            <button @click="handleSaveLLM" :disabled="llmSaving" class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
              {{ llmSaving ? 'Saving...' : 'Save' }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Deployment -->
    <div class="mt-8 space-y-4">
      <h2 class="text-xl font-bold text-gray-900">Deployment</h2>
      <div class="overflow-hidden rounded-lg bg-white shadow p-6">
        <div v-if="deploymentLoading" class="text-sm text-gray-500">Loading deployment status...</div>

        <div v-else class="space-y-4">
          <div class="flex items-center justify-between">
            <div>
              <div class="text-sm text-gray-500">Current version</div>
              <div class="text-lg font-semibold text-gray-900">{{ deployment?.version.version || 'unknown' }}</div>
            </div>
            <span class="inline-flex rounded-full bg-gray-100 px-2 py-1 text-xs font-semibold uppercase tracking-wide text-gray-700">
              {{ deployment?.mode || 'unknown' }}
            </span>
          </div>

          <div class="grid gap-3 md:grid-cols-2">
            <div class="rounded-md bg-gray-50 p-3">
              <div class="text-xs uppercase tracking-wide text-gray-500">Commit</div>
              <div class="mt-1 font-mono text-sm text-gray-700">{{ deployment?.version.commit || 'unknown' }}</div>
            </div>
            <div class="rounded-md bg-gray-50 p-3">
              <div class="text-xs uppercase tracking-wide text-gray-500">Update phase</div>
              <div class="mt-1 text-sm text-gray-700">{{ deployment?.update_status.phase || 'unknown' }}</div>
            </div>
          </div>

          <div v-if="deployment?.latest_release" class="rounded-md bg-blue-50 p-3 text-sm text-blue-800">
            Latest release: {{ deployment.latest_release.version }}
          </div>

          <div v-if="deploymentMessage" class="rounded-md p-3 text-sm" :class="deploymentMessageKind === 'error' ? 'bg-red-50 text-red-700' : 'bg-green-50 text-green-700'">
            {{ deploymentMessage }}
          </div>

          <div class="flex flex-wrap justify-end gap-3">
            <button @click="handleCheckUpdates" :disabled="deploymentActionLoading" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50">
              {{ deploymentActionLoading ? 'Working...' : 'Check Updates' }}
            </button>
            <button @click="handleApplyUpdate" :disabled="deploymentActionLoading" class="rounded-md bg-emerald-600 px-4 py-2 text-sm font-medium text-white hover:bg-emerald-700 disabled:opacity-50">
              Apply Update
            </button>
            <button @click="handleRollbackUpdate" :disabled="deploymentActionLoading" class="rounded-md bg-amber-600 px-4 py-2 text-sm font-medium text-white hover:bg-amber-700 disabled:opacity-50">
              Rollback
            </button>
            <button @click="handleRestartDeployment" :disabled="deploymentActionLoading" class="rounded-md bg-slate-700 px-4 py-2 text-sm font-medium text-white hover:bg-slate-800 disabled:opacity-50">
              Restart Service
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- LDAP Configuration -->
    <div class="mt-8 space-y-4">
      <h2 class="text-xl font-bold text-gray-900">LDAP Configuration</h2>
      <div class="overflow-hidden rounded-lg bg-white shadow p-6">
        <div class="space-y-4">
          <div>
            <label class="block text-sm font-medium text-gray-700">LDAP URL</label>
            <input v-model="ldapForm.url" type="text" placeholder="ldap://ldap.example.com:389" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Base DN</label>
            <input v-model="ldapForm.base_dn" type="text" placeholder="dc=example,dc=com" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Bind DN</label>
            <input v-model="ldapForm.bind_dn" type="text" placeholder="cn=admin,dc=example,dc=com" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Bind Password</label>
            <input v-model="ldapForm.bind_password" type="password" placeholder="Leave empty to keep current" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">User Filter</label>
            <input v-model="ldapForm.user_filter" type="text" placeholder="(uid=%s)" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div class="flex items-center">
            <input v-model="ldapForm.tls" type="checkbox" id="ldap-tls" class="h-4 w-4 rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
            <label for="ldap-tls" class="ml-2 text-sm text-gray-700">Enable TLS</label>
          </div>

          <div v-if="ldapError" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ ldapError }}</div>
          <div v-if="ldapSuccess" class="rounded-md bg-green-50 p-3 text-sm text-green-700">{{ ldapSuccess }}</div>

          <div class="flex justify-end space-x-3">
            <button @click="handleTestLDAP" :disabled="ldapTesting" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50">
              {{ ldapTesting ? 'Testing...' : 'Test Connection' }}
            </button>
            <button @click="handleSaveLDAP" :disabled="ldapSaving" class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
              {{ ldapSaving ? 'Saving...' : 'Save' }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Add/Edit Dialog -->
    <div v-if="showLLMTestDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div class="w-full max-w-md rounded-lg bg-white p-6 shadow-xl">
        <h2 class="mb-4 text-lg font-semibold text-gray-900">Test Prompt</h2>

        <div class="space-y-3">
          <div>
            <label class="block text-sm font-medium text-gray-700">Test Prompt</label>
            <input v-model="llmTestPromptDraft" type="text" placeholder="Hi" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            <p class="mt-1 text-xs text-gray-400">Used only for this Test Connection request. It is not saved.</p>
          </div>
        </div>

        <div class="mt-5 flex justify-end space-x-3">
          <button @click="closeLLMTestDialog" :disabled="llmTesting" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50">Cancel</button>
          <button @click="confirmTestLLM" :disabled="llmTesting" class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
            {{ llmTesting ? 'Testing...' : 'Run Test' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Add/Edit Dialog -->
    <div v-if="showDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div class="w-full max-w-md rounded-lg bg-white p-6 shadow-xl">
        <h2 class="text-lg font-semibold text-gray-900 mb-4">
          {{ editingId ? 'Edit Provider' : 'Add SCM Provider' }}
        </h2>

        <div class="space-y-3">
          <div>
            <label class="block text-sm font-medium text-gray-700">Name</label>
            <input v-model="form.name" type="text" placeholder="e.g. GitHub" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div v-if="!editingId">
            <label class="block text-sm font-medium text-gray-700">Type</label>
            <select v-model="form.type" @change="onTypeChange" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option value="github">GitHub</option>
              <option value="bitbucket_server">Bitbucket Server</option>
            </select>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Base URL</label>
            <input v-model="form.base_url" type="text" placeholder="https://api.github.com" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">
              Access Token
              <span v-if="editingId" class="text-gray-400 font-normal">(leave empty to keep current)</span>
            </label>
            <input v-model="form.token" type="password" placeholder="ghp_xxxx or personal access token" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            <p class="mt-1 text-xs text-gray-400">For public repos, leave empty. For private repos, provide a token with repo scope.</p>
          </div>

          <div v-if="formError" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ formError }}</div>
        </div>

        <div class="mt-5 flex justify-end space-x-3">
          <button @click="showDialog = false" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50">Cancel</button>
          <button @click="handleSubmit" :disabled="formLoading" class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
            {{ formLoading ? 'Saving...' : editingId ? 'Update' : 'Create' }}
          </button>
        </div>
      </div>
    </div>
  </AppLayout>
</template>
