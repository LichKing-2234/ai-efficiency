<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import { listProviders, createProvider, updateProvider, deleteProvider } from '@/api/scmProvider'
import { listRelayProviders, createRelayProvider, updateRelayProvider, deleteRelayProvider, testRelayProvider } from '@/api/relayProvider'
import { listCredentials, createCredential, updateCredential, deleteCredential } from '@/api/credential'
import { getUserProviders } from '@/api/user'
import { getSystemVersion, checkSystemUpdate } from '@/api/system'
import client from '@/api/client'
import type { Credential, RelayProvider, SCMProvider, SystemVersionStatus, UserProviderSummary } from '@/types'

const providers = ref<SCMProvider[]>([])
const relayProviders = ref<RelayProvider[]>([])
const credentials = ref<Credential[]>([])
const loading = ref(true)

// Add/Edit dialog
const showDialog = ref(false)
const editingId = ref<number | null>(null)
const form = ref({
  name: '',
  type: 'github',
  base_url: 'https://api.github.com',
  api_credential_id: 0,
  clone_protocol: 'https' as 'https' | 'ssh',
  clone_credential_id: null as number | null,
})
const formError = ref('')
const formLoading = ref(false)

// Delete confirm
const showDeleteConfirm = ref<number | null>(null)

// Relay provider dialog
const relayLoading = ref(true)
const showRelayDialog = ref(false)
const editingRelayId = ref<number | null>(null)
const relayForm = ref({
  name: '',
  display_name: '',
  base_url: '',
  admin_url: '',
  admin_api_key: '',
  is_primary: false,
  enabled: true,
})
const relayFormError = ref('')
const relayFormLoading = ref(false)
const showRelayDeleteConfirm = ref<number | null>(null)
const relayTesting = ref(false)
const relayTestProviderId = ref<number | null>(null)
const relayTestPromptDraft = ref('Hi')
const relayTestPlatform = ref('')
const relayTestModel = ref('')
const showRelayTestDialog = ref(false)
const relayTestResult = ref<{ providerId: number; success: boolean; message: string; response?: string } | null>(null)
const userProviderSummaries = ref<UserProviderSummary[]>([])

// Credential dialog
const showCredentialDialog = ref(false)
const editingCredentialId = ref<number | null>(null)
const credentialForm = ref({
  name: '',
  description: '',
  kind: 'secret_text',
  text: '',
  username: '',
  password: '',
  private_key: '',
  passphrase: '',
})
const credentialFormError = ref('')
const credentialFormLoading = ref(false)
const showCredentialDeleteConfirm = ref<number | null>(null)

// LDAP config
const ldapForm = ref({ url: '', base_dn: '', bind_dn: '', bind_password: '', user_filter: '', tls: false })
const ldapSaving = ref(false)
const ldapTesting = ref(false)
const ldapError = ref('')
const ldapSuccess = ref('')

// System version
const systemVersion = ref<SystemVersionStatus | null>(null)
const systemVersionLoading = ref(true)
const systemVersionChecking = ref(false)
const systemVersionMessage = ref('')
const systemVersionMessageKind = ref<'success' | 'error' | ''>('')
const systemVersionCheckDisabled = computed(() => (
  systemVersionChecking.value ||
  systemVersionLoading.value ||
  !systemVersion.value ||
  systemVersion.value.check_enabled === false
))

onMounted(async () => {
  await Promise.all([fetchSystemVersion(), fetchProviders(), fetchRelayProviders(), fetchUserProviders(), fetchCredentials(), fetchLDAPConfig()])
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

async function fetchRelayProviders() {
  relayLoading.value = true
  try {
    const res = await listRelayProviders()
    const data = res.data.data
    relayProviders.value = Array.isArray(data) ? data : []
  } catch {
    relayProviders.value = []
  } finally {
    relayLoading.value = false
  }
}

async function fetchUserProviders() {
  try {
    const res = await getUserProviders()
    userProviderSummaries.value = res.data.data?.providers ?? []
  } catch {
    userProviderSummaries.value = []
  }
}

async function fetchCredentials() {
  try {
    const res = await listCredentials()
    const data = res.data.data
    credentials.value = Array.isArray(data) ? data : []
  } catch {
    credentials.value = []
  }
}

function openAddDialog() {
  editingId.value = null
  const defaultAPICredential = credentials.value.find((c) => c.kind !== 'ssh_username_with_private_key')?.id ?? 0
  form.value = {
    name: '',
    type: 'github',
    base_url: 'https://api.github.com',
    api_credential_id: defaultAPICredential,
    clone_protocol: 'https',
    clone_credential_id: null,
  }
  formError.value = ''
  showDialog.value = true
}

function openEditDialog(p: SCMProvider) {
  editingId.value = p.id
  const defaultAPICredential = credentials.value.find((c) => c.kind !== 'ssh_username_with_private_key')?.id ?? 0
  form.value = {
    name: p.name,
    type: p.type,
    base_url: p.base_url,
    api_credential_id: p.api_credential_id || defaultAPICredential,
    clone_protocol: p.clone_protocol || 'https',
    clone_credential_id: p.clone_credential_id ?? null,
  }
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
  if (!form.value.api_credential_id) { formError.value = 'API credential is required'; return }
  if (form.value.clone_protocol === 'ssh' && !form.value.clone_credential_id) { formError.value = 'SSH clone credential is required'; return }

  formLoading.value = true
  try {
    if (editingId.value) {
      const data: any = {
        name: form.value.name,
        base_url: form.value.base_url,
        api_credential_id: form.value.api_credential_id,
        clone_protocol: form.value.clone_protocol,
        clone_credential_id: form.value.clone_protocol === 'ssh' ? form.value.clone_credential_id : null,
      }
      await updateProvider(editingId.value, data)
    } else {
      await createProvider({
        name: form.value.name,
        type: form.value.type,
        base_url: form.value.base_url,
        api_credential_id: form.value.api_credential_id,
        clone_protocol: form.value.clone_protocol,
        clone_credential_id: form.value.clone_protocol === 'ssh' ? form.value.clone_credential_id : null,
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

function openAddRelayDialog() {
  editingRelayId.value = null
  relayForm.value = {
    name: '',
    display_name: '',
    base_url: '',
    admin_url: '',
    admin_api_key: '',
    is_primary: relayProviders.value.length === 0,
    enabled: true,
  }
  relayFormError.value = ''
  showRelayDialog.value = true
}

function openEditRelayDialog(provider: RelayProvider) {
  editingRelayId.value = provider.id
  relayForm.value = {
    name: provider.name,
    display_name: provider.display_name,
    base_url: provider.base_url,
    admin_url: provider.admin_url,
    admin_api_key: '',
    is_primary: provider.is_primary,
    enabled: provider.enabled,
  }
  relayFormError.value = ''
  showRelayDialog.value = true
}

async function handleRelaySubmit() {
  relayFormError.value = ''
  if (!relayForm.value.name.trim()) { relayFormError.value = 'Name is required'; return }
  if (!relayForm.value.display_name.trim()) { relayFormError.value = 'Display name is required'; return }
  if (!relayForm.value.base_url.trim()) { relayFormError.value = 'Base URL is required'; return }
  if (!relayForm.value.admin_url.trim()) { relayFormError.value = 'Admin URL is required'; return }
  if (!editingRelayId.value && !relayForm.value.admin_api_key.trim()) { relayFormError.value = 'Admin API key is required'; return }

  relayFormLoading.value = true
  try {
    if (editingRelayId.value) {
      await updateRelayProvider(editingRelayId.value, {
        display_name: relayForm.value.display_name,
        base_url: relayForm.value.base_url,
        admin_url: relayForm.value.admin_url,
        admin_api_key: relayForm.value.admin_api_key.trim() || undefined,
        is_primary: relayForm.value.is_primary,
        enabled: relayForm.value.enabled,
      })
    } else {
      await createRelayProvider({
        name: relayForm.value.name,
        display_name: relayForm.value.display_name,
        base_url: relayForm.value.base_url,
        admin_url: relayForm.value.admin_url,
        admin_api_key: relayForm.value.admin_api_key,
        is_primary: relayForm.value.is_primary,
        enabled: relayForm.value.enabled,
      })
    }
    showRelayDialog.value = false
    await fetchRelayProviders()
  } catch (e: any) {
    relayFormError.value = e.response?.data?.message || 'Operation failed'
  } finally {
    relayFormLoading.value = false
  }
}

async function confirmDeleteRelay(id: number) {
  try {
    await deleteRelayProvider(id)
    showRelayDeleteConfirm.value = null
    await fetchRelayProviders()
  } catch {
    // delete failed
  }
}

function openRelayTestDialog(provider: RelayProvider) {
  relayTestProviderId.value = provider.id
  relayTestPromptDraft.value = 'Hi'
  relayTestPlatform.value = relayTestPlatforms(provider.id)[0] ?? ''
  relayTestModel.value = ''
  showRelayTestDialog.value = true
}

function closeRelayTestDialog() {
  showRelayTestDialog.value = false
}

async function confirmTestRelayProvider() {
  if (!relayTestProviderId.value) return
  const providerId = relayTestProviderId.value
  const prompt = relayTestPromptDraft.value
  const platform = relayTestPlatform.value
  const model = relayTestModel.value

  closeRelayTestDialog()
  relayTesting.value = true
  try {
    const res = await testRelayProvider(providerId, { platform, model, prompt })
    relayTestResult.value = {
      providerId,
      ...(res.data.data ?? { success: false, message: 'Request failed' }),
    }
  } catch (e: any) {
    relayTestResult.value = {
      providerId,
      success: false,
      message: e.response?.data?.message || e.message || 'Request failed',
    }
  } finally {
    relayTesting.value = false
  }
}

function relayTestPlatforms(providerId: number) {
  const provider = userProviderSummaries.value.find((item) => item.id === providerId)
  if (!provider) return []
  return Array.from(new Set(provider.groups.map((group) => group.platform).filter(Boolean))).sort()
}

function openAddCredentialDialog() {
  editingCredentialId.value = null
  credentialForm.value = {
    name: '',
    description: '',
    kind: 'secret_text',
    text: '',
    username: '',
    password: '',
    private_key: '',
    passphrase: '',
  }
  credentialFormError.value = ''
  showCredentialDialog.value = true
}

function openEditCredentialDialog(credential: Credential) {
  editingCredentialId.value = credential.id
  credentialForm.value = {
    name: credential.name,
    description: credential.description || '',
    kind: credential.kind,
    text: '',
    username: String(credential.summary?.username || ''),
    password: '',
    private_key: '',
    passphrase: '',
  }
  credentialFormError.value = ''
  showCredentialDialog.value = true
}

function buildCredentialPayload() {
  switch (credentialForm.value.kind) {
    case 'secret_text':
      return { text: credentialForm.value.text }
    case 'username_password':
      return { username: credentialForm.value.username, password: credentialForm.value.password }
    default:
      return {
        username: credentialForm.value.username,
        private_key: credentialForm.value.private_key,
        passphrase: credentialForm.value.passphrase,
      }
  }
}

async function handleCredentialSubmit() {
  credentialFormError.value = ''
  if (!credentialForm.value.name) {
    credentialFormError.value = 'Name is required'
    return
  }

  credentialFormLoading.value = true
  try {
    const payload = {
      name: credentialForm.value.name,
      description: credentialForm.value.description,
      kind: credentialForm.value.kind,
      payload: buildCredentialPayload(),
    }
    if (editingCredentialId.value) {
      await updateCredential(editingCredentialId.value, payload)
    } else {
      await createCredential(payload)
    }
    showCredentialDialog.value = false
    await fetchCredentials()
  } catch (e: any) {
    credentialFormError.value = e.response?.data?.message || 'Operation failed'
  } finally {
    credentialFormLoading.value = false
  }
}

async function confirmDeleteCredential(id: number) {
  try {
    await deleteCredential(id)
    showCredentialDeleteConfirm.value = null
    await fetchCredentials()
  } catch {
    // delete failed
  }
}

function formatDate(date: string) {
  return new Date(date).toLocaleDateString()
}

function formatBuildTime(date?: string) {
  if (!date) return 'Unknown'
  const parsed = new Date(date)
  if (Number.isNaN(parsed.getTime())) return date
  return parsed.toLocaleString()
}

async function fetchSystemVersion() {
  systemVersionLoading.value = true
  try {
    const res = await getSystemVersion()
    systemVersion.value = res.data.data ?? null
  } catch {
    systemVersion.value = null
  } finally {
    systemVersionLoading.value = false
  }
}

async function handleCheckSystemUpdate() {
  if (systemVersion.value?.check_enabled === false) {
    systemVersionMessageKind.value = 'error'
    systemVersionMessage.value = 'Version check unavailable'
    return
  }

  systemVersionChecking.value = true
  systemVersionMessage.value = ''
  systemVersionMessageKind.value = ''
  try {
    const res = await checkSystemUpdate()
    systemVersion.value = res.data.data ?? null
    systemVersionMessageKind.value = 'success'
    if (systemVersion.value?.update_available) {
      systemVersionMessage.value = 'Update available'
    } else if (systemVersion.value?.check_error) {
      systemVersionMessageKind.value = 'error'
      systemVersionMessage.value = systemVersion.value.check_error
    } else if (systemVersion.value?.checked) {
      systemVersionMessage.value = 'Already current'
    } else if (systemVersion.value?.check_enabled === false) {
      systemVersionMessageKind.value = 'error'
      systemVersionMessage.value = 'Version check unavailable'
    } else {
      systemVersionMessage.value = ''
    }
  } catch (e: any) {
    systemVersionMessageKind.value = 'error'
    systemVersionMessage.value = e.response?.data?.message || 'Failed to check updates'
  } finally {
    systemVersionChecking.value = false
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

      <div class="overflow-hidden rounded-lg bg-white shadow">
        <div class="flex flex-col gap-4 px-6 py-5 md:flex-row md:items-center md:justify-between">
          <div>
            <h2 class="text-lg font-semibold text-gray-900">System Version</h2>
            <div v-if="systemVersionLoading" class="mt-2 text-sm text-gray-500">Loading version...</div>
            <div v-else-if="systemVersion" class="mt-3 grid gap-3 text-sm text-gray-600 sm:grid-cols-3">
              <div>
                <div class="text-xs font-medium uppercase tracking-wider text-gray-400">Current</div>
                <div class="mt-1 font-mono text-gray-900">{{ systemVersion.version.version }}</div>
              </div>
              <div>
                <div class="text-xs font-medium uppercase tracking-wider text-gray-400">Commit</div>
                <div class="mt-1 font-mono text-gray-900">{{ systemVersion.version.commit }}</div>
              </div>
              <div>
                <div class="text-xs font-medium uppercase tracking-wider text-gray-400">Built</div>
                <div class="mt-1 text-gray-900">{{ formatBuildTime(systemVersion.version.build_time) }}</div>
              </div>
            </div>
            <div v-else class="mt-2 text-sm text-gray-500">Version information unavailable.</div>

            <div v-if="systemVersion?.latest_release" class="mt-4 text-sm text-gray-600">
              Latest:
              <a
                :href="systemVersion.latest_release.url"
                target="_blank"
                rel="noreferrer"
                class="font-mono text-indigo-600 hover:text-indigo-800"
              >{{ systemVersion.latest_release.version }}</a>
            </div>
            <div v-if="systemVersion?.check_enabled === false" class="mt-4 rounded-md bg-yellow-50 px-3 py-2 text-sm text-yellow-700">
              Version check unavailable
            </div>
            <div
              v-if="systemVersionMessage"
              class="mt-4 rounded-md px-3 py-2 text-sm"
              :class="systemVersionMessageKind === 'error' ? 'bg-red-50 text-red-700' : 'bg-green-50 text-green-700'"
            >
              {{ systemVersionMessage }}
            </div>
          </div>
          <button
            class="self-start rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50 md:self-center"
            :disabled="systemVersionCheckDisabled"
            @click="handleCheckSystemUpdate"
          >
            {{ systemVersionChecking ? 'Checking...' : 'Check Updates' }}
          </button>
        </div>
      </div>

      <div v-if="loading" class="text-center text-gray-500 py-12">Loading...</div>

      <div class="space-y-4">
        <div class="flex items-center justify-between">
          <h2 class="text-xl font-bold text-gray-900">Credentials</h2>
          <button
            class="rounded-md bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-black"
            @click="openAddCredentialDialog"
          >
            Add Credential
          </button>
        </div>

        <div class="overflow-hidden rounded-lg bg-white shadow">
          <table class="min-w-full divide-y divide-gray-200">
            <thead class="bg-gray-50">
              <tr>
                <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Name</th>
                <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Kind</th>
                <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Usage</th>
                <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Summary</th>
                <th class="px-6 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-500">Actions</th>
              </tr>
            </thead>
            <tbody class="divide-y divide-gray-200">
              <tr v-for="cred in credentials" :key="cred.id">
                <td class="whitespace-nowrap px-6 py-4 text-sm font-medium text-gray-900">{{ cred.name }}</td>
                <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-600">{{ cred.kind }}</td>
                <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-600">{{ cred.usage_count }}</td>
                <td class="px-6 py-4 text-xs font-mono text-gray-500">{{ JSON.stringify(cred.summary || {}) }}</td>
                <td class="whitespace-nowrap px-6 py-4 text-right text-sm space-x-3">
                  <button class="text-indigo-600 hover:text-indigo-800" @click="openEditCredentialDialog(cred)">Edit</button>
                  <button
                    v-if="showCredentialDeleteConfirm !== cred.id"
                    class="text-red-600 hover:text-red-800"
                    @click="showCredentialDeleteConfirm = cred.id"
                  >Delete</button>
                  <span v-else class="space-x-2">
                    <button class="text-red-700 font-medium" @click="confirmDeleteCredential(cred.id)">Confirm</button>
                    <button class="text-gray-500" @click="showCredentialDeleteConfirm = null">Cancel</button>
                  </span>
                </td>
              </tr>
              <tr v-if="credentials.length === 0">
                <td colspan="5" class="px-6 py-12 text-center text-sm text-gray-500">
                  No credentials configured. Click "Add Credential" to create one.
                </td>
              </tr>
            </tbody>
          </table>
        </div>
      </div>

      <div v-if="!loading" class="overflow-hidden rounded-lg bg-white shadow">
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
                <button :data-testid="`provider-edit-${p.id}`" class="text-indigo-600 hover:text-indigo-800" @click="openEditDialog(p)">Edit</button>
                <button
                  v-if="showDeleteConfirm !== p.id"
                  :data-testid="`provider-delete-${p.id}`"
                  class="text-red-600 hover:text-red-800"
                  @click="showDeleteConfirm = p.id"
                >Delete</button>
                <span v-else class="space-x-2">
                  <button :data-testid="`provider-confirm-delete-${p.id}`" class="text-red-700 font-medium" @click="confirmDelete(p.id)">Confirm</button>
                  <button :data-testid="`provider-cancel-delete-${p.id}`" class="text-gray-500" @click="showDeleteConfirm = null">Cancel</button>
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

    <!-- Relay Providers -->
    <div class="mt-8 space-y-4">
      <div class="flex items-center justify-between">
        <div>
          <h2 class="text-xl font-bold text-gray-900">Relay Providers</h2>
          <p class="mt-1 text-sm text-gray-500">Manage DB-backed relay endpoints used for SSO, API key delivery, and CLI tool configuration.</p>
        </div>
        <button
          class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700"
          @click="openAddRelayDialog"
        >
          Add Relay Provider
        </button>
      </div>

      <div v-if="relayLoading" class="text-center text-gray-500 py-12">Loading relay providers...</div>

      <div v-else class="overflow-hidden rounded-lg bg-white shadow">
        <table class="min-w-full divide-y divide-gray-200">
          <thead class="bg-gray-50">
            <tr>
              <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Name</th>
              <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Primary</th>
              <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">Base URL</th>
              <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">State</th>
              <th class="px-6 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-500">Actions</th>
            </tr>
          </thead>
          <tbody class="divide-y divide-gray-200">
            <tr v-for="provider in relayProviders" :key="provider.id">
              <td class="px-6 py-4">
                <div class="text-sm font-medium text-gray-900">{{ provider.display_name }}</div>
                <div class="mt-1 font-mono text-xs text-gray-500">{{ provider.name }}</div>
                <div
                  v-if="relayTestResult?.providerId === provider.id"
                  class="mt-3 rounded-md p-3 text-sm"
                  :class="relayTestResult.success ? 'bg-green-50 text-green-700' : 'bg-red-50 text-red-700'"
                >
                  <div>{{ relayTestResult.message }}</div>
                  <pre v-if="relayTestResult.response" class="mt-2 whitespace-pre-wrap rounded-md bg-white/70 px-3 py-2 font-mono text-xs text-gray-700">{{ relayTestResult.response }}</pre>
                </div>
              </td>
              <td class="px-6 py-4">
                <div v-if="provider.is_primary" class="inline-flex rounded-full bg-emerald-100 px-2 py-1 text-xs font-semibold text-emerald-700">Primary</div>
                <div v-else class="inline-flex rounded-full bg-gray-100 px-2 py-1 text-xs font-semibold text-gray-500">Secondary</div>
              </td>
              <td class="px-6 py-4 font-mono text-xs text-gray-500">
                <div>{{ provider.base_url }}</div>
                <div class="mt-1">{{ provider.admin_url }}</div>
              </td>
              <td class="px-6 py-4">
                <span
                  class="inline-flex rounded-full px-2 py-1 text-xs font-semibold"
                  :class="provider.enabled ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-500'"
                >{{ provider.enabled ? 'Enabled' : 'Disabled' }}</span>
              </td>
              <td class="whitespace-nowrap px-6 py-4 text-right text-sm space-x-3">
                <button
                  :data-testid="`relay-provider-test-${provider.id}`"
                  class="text-slate-600 hover:text-slate-800"
                  :disabled="relayTesting"
                  @click="openRelayTestDialog(provider)"
                >{{ relayTesting && relayTestProviderId === provider.id ? 'Testing...' : 'Test' }}</button>
                <button :data-testid="`relay-provider-edit-${provider.id}`" class="text-indigo-600 hover:text-indigo-800" @click="openEditRelayDialog(provider)">Edit</button>
                <button
                  v-if="showRelayDeleteConfirm !== provider.id"
                  :data-testid="`relay-provider-delete-${provider.id}`"
                  class="text-red-600 hover:text-red-800"
                  @click="showRelayDeleteConfirm = provider.id"
                >Delete</button>
                <span v-else class="space-x-2">
                  <button :data-testid="`relay-provider-confirm-delete-${provider.id}`" class="text-red-700 font-medium" @click="confirmDeleteRelay(provider.id)">Confirm</button>
                  <button :data-testid="`relay-provider-cancel-delete-${provider.id}`" class="text-gray-500" @click="showRelayDeleteConfirm = null">Cancel</button>
                </span>
              </td>
            </tr>
            <tr v-if="relayProviders.length === 0">
              <td colspan="5" class="px-6 py-12 text-center text-sm text-gray-500">
                No relay providers configured. Add at least one primary provider so SSO login and CLI delivery have a DB-backed source of truth.
              </td>
            </tr>
          </tbody>
        </table>
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

    <!-- Add/Edit Credential Dialog -->
    <div v-if="showCredentialDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div class="w-full max-w-2xl rounded-lg bg-white p-6 shadow-xl">
        <h2 class="mb-4 text-lg font-semibold text-gray-900">
          {{ editingCredentialId ? 'Edit Credential' : 'Add Credential' }}
        </h2>

        <div class="space-y-3">
          <div>
            <label class="block text-sm font-medium text-gray-700">Name</label>
            <input name="credential-name" v-model="credentialForm.name" type="text" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Description</label>
            <input v-model="credentialForm.description" type="text" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Kind</label>
            <select name="credential-kind" v-model="credentialForm.kind" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option value="secret_text">Secret text</option>
              <option value="username_password">Username with password</option>
              <option value="ssh_username_with_private_key">SSH Username with private key</option>
            </select>
          </div>

          <div v-if="credentialForm.kind === 'secret_text'">
            <label class="block text-sm font-medium text-gray-700">Secret Text</label>
            <textarea name="credential-secret-text" v-model="credentialForm.text" rows="4" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm font-mono" />
          </div>

          <template v-else-if="credentialForm.kind === 'username_password'">
            <div>
              <label class="block text-sm font-medium text-gray-700">Username</label>
              <input v-model="credentialForm.username" type="text" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            </div>
            <div>
              <label class="block text-sm font-medium text-gray-700">Password</label>
              <input v-model="credentialForm.password" type="password" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </template>

          <template v-else>
            <div>
              <label class="block text-sm font-medium text-gray-700">SSH Username</label>
              <input v-model="credentialForm.username" type="text" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            </div>
            <div>
              <label class="block text-sm font-medium text-gray-700">Private Key</label>
              <textarea v-model="credentialForm.private_key" rows="6" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm font-mono" />
            </div>
            <div>
              <label class="block text-sm font-medium text-gray-700">Passphrase</label>
              <input v-model="credentialForm.passphrase" type="password" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </template>

          <div v-if="credentialFormError" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ credentialFormError }}</div>

          <div class="flex justify-end space-x-3">
            <button @click="showCredentialDialog = false" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50">Cancel</button>
            <button @click="handleCredentialSubmit" :disabled="credentialFormLoading" class="rounded-md bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-black disabled:opacity-50">
              {{ credentialFormLoading ? 'Saving...' : 'Save Credential' }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Add/Edit Relay Provider Dialog -->
    <div v-if="showRelayDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div class="w-full max-w-2xl rounded-lg bg-white p-6 shadow-xl">
        <h2 class="mb-4 text-lg font-semibold text-gray-900">
          {{ editingRelayId ? 'Edit Relay Provider' : 'Add Relay Provider' }}
        </h2>

        <div class="grid gap-4 md:grid-cols-2">
          <div>
            <label class="block text-sm font-medium text-gray-700">Name</label>
            <input name="relay-provider-name" v-model="relayForm.name" :disabled="!!editingRelayId" type="text" placeholder="sub2api-main" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm disabled:bg-gray-50 disabled:text-gray-500" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Display Name</label>
            <input name="relay-provider-display-name" v-model="relayForm.display_name" type="text" placeholder="Sub2API Main" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Base URL</label>
            <input name="relay-provider-base-url" v-model="relayForm.base_url" type="text" placeholder="https://sub2api.agoraio.cn" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Admin URL</label>
            <input name="relay-provider-admin-url" v-model="relayForm.admin_url" type="text" placeholder="https://sub2api.agoraio.cn" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div class="md:col-span-2">
            <label class="block text-sm font-medium text-gray-700">Admin API Key</label>
            <input name="relay-provider-admin-api-key" v-model="relayForm.admin_api_key" type="password" :placeholder="editingRelayId ? 'Leave empty to keep current key' : 'admin-...'" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            <p class="mt-1 text-xs text-gray-400">Stored encrypted in the database. Leave blank during edit to keep the current key.</p>
          </div>

          <div class="flex items-center">
            <input id="relay-primary" v-model="relayForm.is_primary" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
            <label for="relay-primary" class="ml-2 text-sm text-gray-700">Primary provider</label>
          </div>

          <div class="flex items-center">
            <input id="relay-enabled" v-model="relayForm.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
            <label for="relay-enabled" class="ml-2 text-sm text-gray-700">Enabled</label>
          </div>
        </div>

        <div v-if="relayFormError" class="mt-4 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ relayFormError }}</div>

        <div class="mt-5 flex justify-end space-x-3">
          <button @click="showRelayDialog = false" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50">Cancel</button>
          <button @click="handleRelaySubmit" :disabled="relayFormLoading" class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
            {{ relayFormLoading ? 'Saving...' : editingRelayId ? 'Update Relay Provider' : 'Create Relay Provider' }}
          </button>
        </div>
      </div>
    </div>

    <!-- Relay Provider Test Dialog -->
    <div v-if="showRelayTestDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40">
      <div class="w-full max-w-md rounded-lg bg-white p-6 shadow-xl">
        <h2 class="mb-4 text-lg font-semibold text-gray-900">Test Relay Provider</h2>

        <div class="space-y-3">
          <div>
            <label class="block text-sm font-medium text-gray-700">Platform</label>
            <select v-model="relayTestPlatform" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option v-for="platform in relayTestProviderId ? relayTestPlatforms(relayTestProviderId) : []" :key="platform" :value="platform">
                {{ platform }}
              </option>
            </select>
            <p class="mt-1 text-xs text-gray-400">Uses the current admin user's active API key for the selected provider and platform.</p>
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700">Model</label>
            <input v-model="relayTestModel" type="text" placeholder="gpt-5.4" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            <p class="mt-1 text-xs text-gray-400">Pick the concrete model to test for the selected platform.</p>
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700">Test Prompt</label>
            <input v-model="relayTestPromptDraft" type="text" placeholder="Hi" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            <p class="mt-1 text-xs text-gray-400">This sends a real chat completion through the selected relay provider and is not persisted.</p>
          </div>
          <div v-if="relayTestProviderId && relayTestPlatforms(relayTestProviderId).length === 0" class="rounded-md bg-amber-50 p-3 text-sm text-amber-700">
            Current admin user has no visible platforms under this provider.
          </div>
        </div>

        <div class="mt-5 flex justify-end space-x-3">
          <button @click="closeRelayTestDialog" :disabled="relayTesting" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50">Cancel</button>
          <button @click="confirmTestRelayProvider" :disabled="relayTesting || !relayTestPlatform || !relayTestModel" class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
            {{ relayTesting ? 'Testing...' : 'Run Test' }}
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
            <input name="provider-name" v-model="form.name" type="text" placeholder="e.g. GitHub" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
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
            <label class="block text-sm font-medium text-gray-700">API Credential</label>
            <select name="provider-api-credential" v-model.number="form.api_credential_id" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option :value="0" disabled>Select API credential</option>
              <option v-for="cred in credentials.filter(c => c.kind !== 'ssh_username_with_private_key')" :key="cred.id" :value="cred.id">
                {{ cred.name }} ({{ cred.kind }})
              </option>
            </select>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">Clone Protocol</label>
            <select name="provider-clone-protocol" v-model="form.clone_protocol" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option value="https">https</option>
              <option value="ssh">ssh</option>
            </select>
          </div>

          <div v-if="form.clone_protocol === 'ssh'">
            <label class="block text-sm font-medium text-gray-700">Clone Credential</label>
            <select name="provider-clone-credential" v-model.number="form.clone_credential_id" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option :value="null">Select SSH credential</option>
              <option v-for="cred in credentials.filter(c => c.kind === 'ssh_username_with_private_key')" :key="cred.id" :value="cred.id">
                {{ cred.name }}
              </option>
            </select>
            <p class="mt-1 text-xs text-gray-400">SSH clone still requires an API credential for SCM platform APIs.</p>
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
