<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import { listProviders, createProvider, updateProvider, deleteProvider } from '@/api/scmProvider'
import { listRelayProviders, createRelayProvider, updateRelayProvider, deleteRelayProvider } from '@/api/relayProvider'
import { listCredentials, createCredential, updateCredential, deleteCredential } from '@/api/credential'
import { getSystemVersion, checkSystemUpdate } from '@/api/system'
import client from '@/api/client'
import { useI18n } from '@/i18n'
import { useModalFocus } from '@/composables/useModalFocus'
import AIServiceSettings from '@/components/settings/AIServiceSettings.vue'
import CodePlatformSettings from '@/components/settings/CodePlatformSettings.vue'
import AdvancedCredentialSettings from '@/components/settings/AdvancedCredentialSettings.vue'
import DeploymentRuntimeSettings from '@/components/settings/DeploymentRuntimeSettings.vue'
import OrganizationLoginSettings from '@/components/settings/OrganizationLoginSettings.vue'
import type { Credential, RelayProvider, SCMProvider, SystemVersionStatus } from '@/types'

const { t } = useI18n()
const route = useRoute()
const router = useRouter()
type SettingsSection = 'ai-services' | 'code-platforms' | 'organization-login' | 'deployment-runtime' | 'advanced-credentials'

const activeSection = ref<SettingsSection>(initialSettingsSection())
const settingsSections = computed<Array<{ id: SettingsSection; label: string; description: string }>>(() => [
  { id: 'ai-services', label: t('settings.aiServices'), description: t('settings.aiServicesHelp') },
  { id: 'code-platforms', label: t('settings.codePlatforms'), description: t('settings.codePlatformsHelp') },
  { id: 'organization-login', label: t('settings.organizationLogin'), description: t('settings.organizationLoginHelp') },
  { id: 'deployment-runtime', label: t('settings.deploymentRuntime'), description: t('settings.deploymentRuntimeHelp') },
  { id: 'advanced-credentials', label: t('settings.advancedCredentials'), description: t('settings.advancedCredentialsHelp') },
])
const providers = ref<SCMProvider[]>([])
const relayProviders = ref<RelayProvider[]>([])
const credentials = ref<Credential[]>([])
const loading = ref(true)
const githubDefaultSSHHost = 'github.com'

// Add/Edit dialog
const showDialog = ref(false)
const providerDialog = ref<HTMLElement | null>(null)
const providerNameInput = ref<HTMLInputElement | null>(null)
const editingId = ref<number | null>(null)
const form = ref({
  name: '',
  type: 'github',
  base_url: 'https://api.github.com',
  ssh_host: githubDefaultSSHHost,
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
const relayDialog = ref<HTMLElement | null>(null)
const relayNameInput = ref<HTMLInputElement | null>(null)
const editingRelayId = ref<number | null>(null)
const relayForm = ref({
  name: '',
  display_name: '',
  base_url: '',
  admin_api_key: '',
  is_primary: false,
  enabled: true,
})
const relayFormError = ref('')
const relayFormLoading = ref(false)
const showRelayDeleteConfirm = ref<number | null>(null)

// Credential dialog
const showCredentialDialog = ref(false)
const credentialDialog = ref<HTMLElement | null>(null)
const credentialNameInput = ref<HTMLInputElement | null>(null)
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

// System version
const systemVersion = ref<SystemVersionStatus | null>(null)
const systemVersionLoading = ref(false)
const systemVersionChecking = ref(false)
const systemVersionMessage = ref('')
const systemVersionMessageKind = ref<'success' | 'error' | ''>('')

// LDAP config
const ldapForm = ref({ url: '', base_dn: '', bind_dn: '', bind_password: '', user_filter: '', tls: false })
const ldapSaving = ref(false)
const ldapTesting = ref(false)
const ldapError = ref('')
const ldapSuccess = ref('')

onMounted(async () => {
  await Promise.all([fetchProviders(), fetchRelayProviders(), fetchCredentials(), fetchSystemVersion(), fetchLDAPConfig()])
})

const { handleKeydown: handleProviderDialogKeydown } = useModalFocus(showDialog, providerDialog, {
  initialFocus: providerNameInput,
  onClose: closeProviderDialog,
})
const { handleKeydown: handleRelayDialogKeydown } = useModalFocus(showRelayDialog, relayDialog, {
  initialFocus: relayNameInput,
  onClose: closeRelayDialog,
})
const { handleKeydown: handleCredentialDialogKeydown } = useModalFocus(showCredentialDialog, credentialDialog, {
  initialFocus: credentialNameInput,
  onClose: closeCredentialDialog,
})

watch(activeSection, replaceSettingsQuery)

function initialSettingsSection(): SettingsSection {
  const section = route.query.section
  if (
    section === 'ai-services' ||
    section === 'code-platforms' ||
    section === 'organization-login' ||
    section === 'deployment-runtime' ||
    section === 'advanced-credentials'
  ) {
    return section
  }
  return 'ai-services'
}

function replaceSettingsQuery() {
  const query = activeSection.value === 'ai-services' ? {} : { section: activeSection.value }
  void router.replace({ query })
}

function selectSection(section: SettingsSection) {
  activeSection.value = section
}

function onSettingsTabKeydown(event: KeyboardEvent, index: number) {
  const keys = ['ArrowRight', 'ArrowDown', 'ArrowLeft', 'ArrowUp', 'Home', 'End']
  if (!keys.includes(event.key)) return
  event.preventDefault()
  const sections = settingsSections.value
  let nextIndex = index
  if (event.key === 'ArrowRight' || event.key === 'ArrowDown') nextIndex = (index + 1) % sections.length
  if (event.key === 'ArrowLeft' || event.key === 'ArrowUp') nextIndex = (index - 1 + sections.length) % sections.length
  if (event.key === 'Home') nextIndex = 0
  if (event.key === 'End') nextIndex = sections.length - 1
  activeSection.value = sections[nextIndex].id
  document.getElementById(`settings-tab-${sections[nextIndex].id}`)?.focus()
}

function closeProviderDialog() {
  showDialog.value = false
}

function closeRelayDialog() {
  showRelayDialog.value = false
}

function closeCredentialDialog() {
  showCredentialDialog.value = false
}

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
    ssh_host: githubDefaultSSHHost,
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
    ssh_host: p.ssh_host || '',
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
    form.value.ssh_host = githubDefaultSSHHost
  } else {
    form.value.base_url = ''
    form.value.ssh_host = ''
  }
}

async function handleSubmit() {
  formError.value = ''
  if (!form.value.name) { formError.value = t('settings.nameRequired'); return }
  if (!form.value.base_url) { formError.value = t('settings.baseUrlRequired'); return }
  if (!form.value.api_credential_id) { formError.value = t('settings.apiCredentialRequired'); return }
  if (form.value.clone_protocol === 'ssh' && !form.value.clone_credential_id) { formError.value = t('settings.sshCloneCredentialRequired'); return }

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
      data.ssh_host = form.value.ssh_host.trim()
      await updateProvider(editingId.value, data)
    } else {
      const data: any = {
        name: form.value.name,
        type: form.value.type,
        base_url: form.value.base_url,
        api_credential_id: form.value.api_credential_id,
        clone_protocol: form.value.clone_protocol,
        clone_credential_id: form.value.clone_protocol === 'ssh' ? form.value.clone_credential_id : null,
      }
      if (form.value.ssh_host.trim()) {
        data.ssh_host = form.value.ssh_host.trim()
      }
      await createProvider(data)
    }
    showDialog.value = false
    await fetchProviders()
  } catch (e: any) {
    formError.value = e.response?.data?.message || t('settings.operationFailed')
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
    admin_api_key: '',
    is_primary: provider.is_primary,
    enabled: provider.enabled,
  }
  relayFormError.value = ''
  showRelayDialog.value = true
}

async function handleRelaySubmit() {
  relayFormError.value = ''
  if (!relayForm.value.name.trim()) { relayFormError.value = t('settings.nameRequired'); return }
  if (!relayForm.value.display_name.trim()) { relayFormError.value = t('settings.displayNameRequired'); return }
  if (!relayForm.value.base_url.trim()) { relayFormError.value = t('settings.baseUrlRequired'); return }
  if (!editingRelayId.value && !relayForm.value.admin_api_key.trim()) { relayFormError.value = t('settings.adminApiKeyRequired'); return }

  relayFormLoading.value = true
  try {
    if (editingRelayId.value) {
      await updateRelayProvider(editingRelayId.value, {
        display_name: relayForm.value.display_name,
        base_url: relayForm.value.base_url,
        admin_api_key: relayForm.value.admin_api_key.trim() || undefined,
        is_primary: relayForm.value.is_primary,
        enabled: relayForm.value.enabled,
      })
    } else {
      await createRelayProvider({
        name: relayForm.value.name,
        display_name: relayForm.value.display_name,
        base_url: relayForm.value.base_url,
        admin_api_key: relayForm.value.admin_api_key,
        is_primary: relayForm.value.is_primary,
        enabled: relayForm.value.enabled,
      })
    }
    showRelayDialog.value = false
    await fetchRelayProviders()
  } catch (e: any) {
    relayFormError.value = e.response?.data?.message || t('settings.operationFailed')
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
    credentialFormError.value = t('settings.nameRequired')
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
    credentialFormError.value = e.response?.data?.message || t('settings.operationFailed')
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

function setSystemVersionMessage(kind: 'success' | 'error', message: string) {
  systemVersionMessageKind.value = kind
  systemVersionMessage.value = message
}

async function handleCheckUpdates() {
  if (systemVersion.value?.check_enabled === false) {
    setSystemVersionMessage('error', t('settings.versionCheckUnavailable'))
    return
  }

  systemVersionChecking.value = true
  systemVersionMessage.value = ''
  systemVersionMessageKind.value = ''
  try {
    const res = await checkSystemUpdate()
    systemVersion.value = res.data.data ?? null
    if (systemVersion.value?.update_available) {
      setSystemVersionMessage('success', t('settings.updateAvailable'))
    } else if (systemVersion.value?.check_error) {
      setSystemVersionMessage('error', systemVersion.value.check_error)
    } else if (systemVersion.value?.checked) {
      setSystemVersionMessage('success', t('settings.alreadyCurrent'))
    } else if (systemVersion.value?.check_enabled === false) {
      setSystemVersionMessage('error', t('settings.versionCheckUnavailable'))
    }
  } catch (e: any) {
    setSystemVersionMessage('error', e.response?.data?.message || t('settings.checkUpdatesFailed'))
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
    ldapError.value = t('settings.ldapUrlRequired')
    return
  }
  ldapSaving.value = true
  try {
    await client.put('/admin/settings/ldap', ldapForm.value)
    ldapSuccess.value = t('settings.ldapSaved')
    setTimeout(() => { ldapSuccess.value = '' }, 3000)
  } catch (e: any) {
    ldapError.value = e.response?.data?.message || t('settings.ldapSaveFailed')
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
    ldapSuccess.value = t('settings.ldapTestSuccessful')
    setTimeout(() => { ldapSuccess.value = '' }, 3000)
  } catch (e: any) {
    ldapError.value = e.response?.data?.message || t('settings.ldapTestFailed')
  } finally {
    ldapTesting.value = false
  }
}
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <div>
        <div>
          <h1 class="text-2xl font-bold text-gray-900">{{ t('nav.adminConsole') }}</h1>
          <p class="mt-1 text-sm text-gray-500">{{ t('settings.subtitle') }}</p>
        </div>
      </div>

      <div class="grid gap-2 lg:grid-cols-5" role="tablist" aria-label="Admin console sections">
        <button
          v-for="(section, index) in settingsSections"
          :key="section.id"
          :id="`settings-tab-${section.id}`"
          :data-testid="`settings-tab-${section.id}`"
          class="min-h-20 rounded-lg border px-3 py-3 text-left transition-colors"
          :class="activeSection === section.id ? 'border-blue-300 bg-blue-50 text-blue-950' : 'border-slate-200 bg-white text-slate-700 hover:bg-slate-50'"
          type="button"
          role="tab"
          :aria-selected="activeSection === section.id"
          :aria-controls="`settings-panel-${section.id}`"
          :tabindex="activeSection === section.id ? 0 : -1"
          @click="selectSection(section.id)"
          @keydown="onSettingsTabKeydown($event, index)"
        >
          <span class="block text-sm font-semibold">{{ section.label }}</span>
          <span class="mt-1 block text-xs text-slate-500">{{ section.description }}</span>
        </button>
      </div>

      <div v-if="loading" class="text-center text-gray-500 py-12">{{ t('settings.loading') }}</div>

      <section
        v-if="activeSection === 'advanced-credentials'"
        id="settings-panel-advanced-credentials"
        role="tabpanel"
        aria-labelledby="settings-tab-advanced-credentials"
      >
        <AdvancedCredentialSettings
          :credentials="credentials"
          :show-delete-confirm="showCredentialDeleteConfirm"
          @add="openAddCredentialDialog"
          @edit="openEditCredentialDialog"
          @request-delete="showCredentialDeleteConfirm = $event"
          @confirm-delete="confirmDeleteCredential"
          @cancel-delete="showCredentialDeleteConfirm = null"
        />
      </section>

      <section
        v-if="activeSection === 'code-platforms' && !loading"
        id="settings-panel-code-platforms"
        role="tabpanel"
        aria-labelledby="settings-tab-code-platforms"
      >
        <CodePlatformSettings
          :providers="providers"
          :show-delete-confirm="showDeleteConfirm"
          @add="openAddDialog"
          @edit="openEditDialog"
          @request-delete="showDeleteConfirm = $event"
          @confirm-delete="confirmDelete"
          @cancel-delete="showDeleteConfirm = null"
        />
      </section>

      <section
        v-if="activeSection === 'ai-services'"
        id="settings-panel-ai-services"
        role="tabpanel"
        aria-labelledby="settings-tab-ai-services"
      >
        <AIServiceSettings
          :relay-loading="relayLoading"
          :relay-providers="relayProviders"
          :show-delete-confirm="showRelayDeleteConfirm"
          @add="openAddRelayDialog"
          @edit="openEditRelayDialog"
          @request-delete="showRelayDeleteConfirm = $event"
          @confirm-delete="confirmDeleteRelay"
          @cancel-delete="showRelayDeleteConfirm = null"
        />
      </section>

      <section
        v-if="activeSection === 'deployment-runtime'"
        id="settings-panel-deployment-runtime"
        role="tabpanel"
        aria-labelledby="settings-tab-deployment-runtime"
      >
        <DeploymentRuntimeSettings
          :system-version="systemVersion"
          :system-version-loading="systemVersionLoading"
          :system-version-checking="systemVersionChecking"
          :system-version-message="systemVersionMessage"
          :system-version-message-kind="systemVersionMessageKind"
          @check-updates="handleCheckUpdates"
        />
      </section>

      <section
        v-if="activeSection === 'organization-login'"
        id="settings-panel-organization-login"
        role="tabpanel"
        aria-labelledby="settings-tab-organization-login"
      >
        <OrganizationLoginSettings
          :ldap-form="ldapForm"
          :ldap-saving="ldapSaving"
          :ldap-testing="ldapTesting"
          :ldap-error="ldapError"
          :ldap-success="ldapSuccess"
          :credentials="credentials"
          @test="handleTestLDAP"
          @save="handleSaveLDAP"
        />
      </section>
    </div>

    <!-- Add/Edit Credential Dialog -->
    <div v-if="showCredentialDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <button class="absolute inset-0" type="button" :aria-label="t('settings.cancel')" @click="closeCredentialDialog" />
      <div
        ref="credentialDialog"
        class="relative max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-lg bg-white p-6 shadow-xl"
        role="dialog"
        aria-modal="true"
        aria-labelledby="credential-dialog-title"
        tabindex="-1"
        @keydown="handleCredentialDialogKeydown"
      >
        <h2 id="credential-dialog-title" class="mb-4 text-lg font-semibold text-gray-900">
          {{ editingCredentialId ? t('settings.editCredential') : t('settings.addCredential') }}
        </h2>

        <div class="space-y-3">
          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.name') }}</label>
            <input ref="credentialNameInput" name="credential-name" v-model="credentialForm.name" type="text" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.description') }}</label>
            <input v-model="credentialForm.description" type="text" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.kind') }}</label>
            <select name="credential-kind" v-model="credentialForm.kind" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option value="secret_text">{{ t('settings.secretTextKind') }}</option>
              <option value="username_password">{{ t('settings.usernamePasswordKind') }}</option>
              <option value="ssh_username_with_private_key">{{ t('settings.sshPrivateKeyKind') }}</option>
            </select>
          </div>

          <div v-if="credentialForm.kind === 'secret_text'">
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.secretText') }}</label>
            <textarea name="credential-secret-text" v-model="credentialForm.text" rows="4" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm font-mono" />
          </div>

          <template v-else-if="credentialForm.kind === 'username_password'">
            <div>
              <label class="block text-sm font-medium text-gray-700">{{ t('settings.username') }}</label>
              <input v-model="credentialForm.username" type="text" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            </div>
            <div>
              <label class="block text-sm font-medium text-gray-700">{{ t('settings.password') }}</label>
              <input v-model="credentialForm.password" type="password" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </template>

          <template v-else>
            <div>
              <label class="block text-sm font-medium text-gray-700">{{ t('settings.sshUsername') }}</label>
              <input v-model="credentialForm.username" type="text" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            </div>
            <div>
              <label class="block text-sm font-medium text-gray-700">{{ t('settings.privateKey') }}</label>
              <textarea v-model="credentialForm.private_key" rows="6" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm font-mono" />
            </div>
            <div>
              <label class="block text-sm font-medium text-gray-700">{{ t('settings.passphrase') }}</label>
              <input v-model="credentialForm.passphrase" type="password" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            </div>
          </template>

          <div v-if="credentialFormError" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ credentialFormError }}</div>

          <div class="flex justify-end space-x-3">
            <button @click="closeCredentialDialog" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50">{{ t('settings.cancel') }}</button>
            <button @click="handleCredentialSubmit" :disabled="credentialFormLoading" class="rounded-md bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-black disabled:opacity-50">
              {{ credentialFormLoading ? t('settings.saving') : t('settings.saveCredential') }}
            </button>
          </div>
        </div>
      </div>
    </div>

    <!-- Add/Edit Relay Provider Dialog -->
    <div v-if="showRelayDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <button class="absolute inset-0" type="button" :aria-label="t('settings.cancel')" @click="closeRelayDialog" />
      <div
        ref="relayDialog"
        class="relative max-h-[90vh] w-full max-w-2xl overflow-y-auto rounded-lg bg-white p-6 shadow-xl"
        role="dialog"
        aria-modal="true"
        aria-labelledby="relay-dialog-title"
        tabindex="-1"
        @keydown="handleRelayDialogKeydown"
      >
        <h2 id="relay-dialog-title" class="mb-4 text-lg font-semibold text-gray-900">
          {{ editingRelayId ? t('settings.editRelayProvider') : t('settings.addRelayProvider') }}
        </h2>

        <div class="grid gap-4 md:grid-cols-2">
          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.name') }}</label>
            <input ref="relayNameInput" name="relay-provider-name" v-model="relayForm.name" :disabled="!!editingRelayId" type="text" placeholder="relay-main" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm disabled:bg-gray-50 disabled:text-gray-500" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.displayName') }}</label>
            <input name="relay-provider-display-name" v-model="relayForm.display_name" type="text" placeholder="Relay Main" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.baseUrl') }}</label>
            <input name="relay-provider-base-url" v-model="relayForm.base_url" type="text" placeholder="https://relay.example.com" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div class="md:col-span-2">
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.adminApiKey') }}</label>
            <input name="relay-provider-admin-api-key" v-model="relayForm.admin_api_key" type="password" :placeholder="editingRelayId ? t('settings.keepCurrentPlaceholder') : 'admin-...'" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            <p class="mt-1 text-xs text-gray-400">{{ t('settings.relayKeyHelp') }}</p>
          </div>

          <div class="flex items-center">
            <input id="relay-primary" v-model="relayForm.is_primary" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
            <label for="relay-primary" class="ml-2 text-sm text-gray-700">{{ t('settings.primaryProvider') }}</label>
          </div>

          <div class="flex items-center">
            <input id="relay-enabled" v-model="relayForm.enabled" type="checkbox" class="h-4 w-4 rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
            <label for="relay-enabled" class="ml-2 text-sm text-gray-700">{{ t('settings.enabled') }}</label>
          </div>
        </div>

        <div v-if="relayFormError" class="mt-4 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ relayFormError }}</div>

        <div class="mt-5 flex justify-end space-x-3">
          <button @click="closeRelayDialog" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50">{{ t('settings.cancel') }}</button>
          <button @click="handleRelaySubmit" :disabled="relayFormLoading" class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
            {{ relayFormLoading ? t('settings.saving') : editingRelayId ? t('settings.updateRelayProvider') : t('settings.createRelayProvider') }}
          </button>
        </div>
      </div>
    </div>

    <!-- Add/Edit Dialog -->
    <div v-if="showDialog" class="fixed inset-0 z-50 flex items-center justify-center bg-black/40 p-4">
      <button class="absolute inset-0" type="button" :aria-label="t('settings.cancel')" @click="closeProviderDialog" />
      <div
        ref="providerDialog"
        class="relative max-h-[90vh] w-full max-w-md overflow-y-auto rounded-lg bg-white p-6 shadow-xl"
        role="dialog"
        aria-modal="true"
        aria-labelledby="provider-dialog-title"
        tabindex="-1"
        @keydown="handleProviderDialogKeydown"
      >
        <h2 id="provider-dialog-title" class="mb-4 text-lg font-semibold text-gray-900">
          {{ editingId ? t('settings.editProvider') : t('settings.addScmProvider') }}
        </h2>

        <div class="space-y-3">
          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.name') }}</label>
            <input ref="providerNameInput" name="provider-name" v-model="form.name" type="text" placeholder="e.g. GitHub" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div v-if="!editingId">
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.type') }}</label>
            <select v-model="form.type" @change="onTypeChange" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option value="github">GitHub</option>
              <option value="bitbucket_server">Bitbucket Server</option>
            </select>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.baseUrl') }}</label>
            <input v-model="form.base_url" type="text" placeholder="https://api.github.com" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.sshHost') }}</label>
            <input name="provider-ssh-host" v-model="form.ssh_host" type="text" placeholder="git.example.com" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
            <p class="mt-1 text-xs text-gray-400">{{ t('settings.sshHostHelp') }}</p>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.apiCredential') }}</label>
            <select name="provider-api-credential" v-model.number="form.api_credential_id" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option :value="0" disabled>{{ t('settings.selectApiCredential') }}</option>
              <option v-for="cred in credentials.filter(c => c.kind !== 'ssh_username_with_private_key')" :key="cred.id" :value="cred.id">
                {{ cred.name }} ({{ cred.kind }})
              </option>
            </select>
          </div>

          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.cloneProtocol') }}</label>
            <select name="provider-clone-protocol" v-model="form.clone_protocol" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option value="https">https</option>
              <option value="ssh">ssh</option>
            </select>
          </div>

          <div v-if="form.clone_protocol === 'ssh'">
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.cloneCredential') }}</label>
            <select name="provider-clone-credential" v-model.number="form.clone_credential_id" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm">
              <option :value="null">{{ t('settings.selectSshCredential') }}</option>
              <option v-for="cred in credentials.filter(c => c.kind === 'ssh_username_with_private_key')" :key="cred.id" :value="cred.id">
                {{ cred.name }}
              </option>
            </select>
            <p class="mt-1 text-xs text-gray-400">{{ t('settings.sshCloneHelp') }}</p>
          </div>

          <div v-if="formError" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ formError }}</div>
        </div>

        <div class="mt-5 flex justify-end space-x-3">
          <button @click="closeProviderDialog" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50">{{ t('settings.cancel') }}</button>
          <button @click="handleSubmit" :disabled="formLoading" class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
            {{ formLoading ? t('settings.saving') : editingId ? t('settings.update') : t('settings.create') }}
          </button>
        </div>
      </div>
    </div>
  </AppLayout>
</template>
