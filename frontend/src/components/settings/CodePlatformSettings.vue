<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { createProvider, deleteProvider, listProviders, updateProvider } from '@/api/scmProvider'
import { useModalFocus } from '@/composables/useModalFocus'
import { useI18n } from '@/i18n'
import { useSettingsResourcesStore } from '@/stores/settingsResources'
import type { SCMProvider } from '@/types'

const { locale, t } = useI18n()
const settingsResources = useSettingsResourcesStore()
const { credentials } = storeToRefs(settingsResources)
const providers = ref<SCMProvider[]>([])
const loading = ref(true)
const showDeleteConfirm = ref<number | null>(null)
const showDialog = ref(false)
const providerDialog = ref<HTMLElement | null>(null)
const providerNameInput = ref<HTMLInputElement | null>(null)
const editingId = ref<number | null>(null)
const githubDefaultSSHHost = 'github.com'
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

onMounted(fetchProviders)

const { handleKeydown: handleProviderDialogKeydown } = useModalFocus(showDialog, providerDialog, {
  initialFocus: providerNameInput,
  onClose: closeProviderDialog,
})

function formatDate(date: string) {
  return new Date(date).toLocaleDateString(locale.value)
}

async function fetchProviders() {
  loading.value = true
  try {
    const response = await listProviders()
    const data = response.data.data
    providers.value = Array.isArray(data) ? data : (data as any)?.items ?? []
  } catch {
    providers.value = []
  } finally {
    loading.value = false
  }
}

function closeProviderDialog() {
  showDialog.value = false
}

async function openAddDialog() {
  await settingsResources.loadCredentials()
  editingId.value = null
  const defaultAPICredential = credentials.value.find((credential) => credential.kind !== 'ssh_username_with_private_key')?.id ?? 0
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

async function openEditDialog(provider: SCMProvider) {
  await settingsResources.loadCredentials()
  editingId.value = provider.id
  const defaultAPICredential = credentials.value.find((credential) => credential.kind !== 'ssh_username_with_private_key')?.id ?? 0
  form.value = {
    name: provider.name,
    type: provider.type,
    base_url: provider.base_url,
    ssh_host: provider.ssh_host || '',
    api_credential_id: provider.api_credential_id || defaultAPICredential,
    clone_protocol: provider.clone_protocol || 'https',
    clone_credential_id: provider.clone_credential_id ?? null,
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
      await updateProvider(editingId.value, {
        name: form.value.name,
        base_url: form.value.base_url,
        ssh_host: form.value.ssh_host.trim(),
        api_credential_id: form.value.api_credential_id,
        clone_protocol: form.value.clone_protocol,
        clone_credential_id: form.value.clone_protocol === 'ssh' ? form.value.clone_credential_id : null,
      })
    } else {
      const data: Record<string, unknown> = {
        name: form.value.name,
        type: form.value.type,
        base_url: form.value.base_url,
        api_credential_id: form.value.api_credential_id,
        clone_protocol: form.value.clone_protocol,
        clone_credential_id: form.value.clone_protocol === 'ssh' ? form.value.clone_credential_id : null,
      }
      if (form.value.ssh_host.trim()) data.ssh_host = form.value.ssh_host.trim()
      await createProvider(data)
    }
    showDialog.value = false
    await fetchProviders()
  } catch (error: any) {
    formError.value = error.response?.data?.message || t('settings.operationFailed')
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
    // Keep the row available for another attempt.
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h2 class="text-xl font-bold text-gray-900">{{ t('settings.codePlatforms') }}</h2>
        <p class="mt-1 text-sm text-gray-500">{{ t('settings.codePlatformsHelp') }}</p>
      </div>
      <button
        class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700"
        @click="openAddDialog"
      >
        {{ t('settings.addProvider') }}
      </button>
    </div>

    <div v-if="loading" class="py-12 text-center text-gray-500">{{ t('settings.loading') }}</div>

    <div v-else class="rounded-lg bg-white shadow">
      <div v-if="providers.length > 0" class="space-y-3 p-4 md:hidden">
        <article v-for="p in providers" :key="p.id" class="rounded-lg border border-gray-100 p-4">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="truncate text-sm font-medium text-gray-900">{{ p.name }}</div>
              <div class="mt-1 break-all font-mono text-xs text-gray-500">{{ p.base_url }}</div>
              <div v-if="p.ssh_host" class="mt-1 break-all font-mono text-xs text-gray-500">{{ p.ssh_host }}</div>
            </div>
            <span
              class="shrink-0 rounded-full px-2 py-0.5 text-xs font-semibold"
              :class="p.type === 'github' ? 'bg-gray-100 text-gray-800' : 'bg-blue-100 text-blue-800'"
            >
              {{ p.type }}
            </span>
          </div>
          <dl class="mt-3 grid grid-cols-2 gap-3 text-xs">
            <div>
              <dt class="text-gray-400">{{ t('settings.status') }}</dt>
              <dd class="mt-1 text-gray-800">{{ p.status }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">{{ t('settings.created') }}</dt>
              <dd class="mt-1 text-gray-800">{{ formatDate(p.created_at) }}</dd>
            </div>
          </dl>
          <div class="mt-3 flex flex-wrap gap-3 text-sm">
            <button :data-testid="`provider-edit-${p.id}`" class="font-medium text-indigo-600 hover:text-indigo-800" @click="openEditDialog(p)">{{ t('settings.edit') }}</button>
            <button
              v-if="showDeleteConfirm !== p.id"
              :data-testid="`provider-delete-${p.id}`"
              class="text-red-600 hover:text-red-800"
              @click="showDeleteConfirm = p.id"
            >{{ t('settings.delete') }}</button>
            <template v-else>
              <button :data-testid="`provider-confirm-delete-${p.id}`" class="font-medium text-red-700" @click="confirmDelete(p.id)">{{ t('settings.confirm') }}</button>
              <button :data-testid="`provider-cancel-delete-${p.id}`" class="text-gray-500" @click="showDeleteConfirm = null">{{ t('settings.cancel') }}</button>
            </template>
          </div>
        </article>
      </div>
      <div v-else class="px-6 py-12 text-center text-sm text-gray-500 md:hidden">
        {{ t('settings.noScmProviders') }}
      </div>

      <table class="hidden min-w-full divide-y divide-gray-200 md:table">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.name') }}</th>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.type') }}</th>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.baseUrl') }}</th>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.sshHost') }}</th>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.status') }}</th>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.created') }}</th>
            <th class="px-6 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.actions') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200">
          <tr v-for="p in providers" :key="p.id">
            <td class="whitespace-nowrap px-6 py-4 text-sm font-medium text-gray-900">{{ p.name }}</td>
            <td class="whitespace-nowrap px-6 py-4">
              <span
                class="inline-flex rounded-full px-2 text-xs font-semibold leading-5"
                :class="p.type === 'github' ? 'bg-gray-100 text-gray-800' : 'bg-blue-100 text-blue-800'"
              >
                {{ p.type }}
              </span>
            </td>
            <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500 font-mono text-xs">{{ p.base_url }}</td>
            <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500 font-mono text-xs">{{ p.ssh_host || '—' }}</td>
            <td class="whitespace-nowrap px-6 py-4">
              <span class="inline-flex rounded-full px-2 text-xs font-semibold leading-5 bg-green-100 text-green-800">
                {{ p.status }}
              </span>
            </td>
            <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500">{{ formatDate(p.created_at) }}</td>
            <td class="whitespace-nowrap px-6 py-4 text-right text-sm space-x-3">
              <button :data-testid="`provider-edit-${p.id}`" class="text-indigo-600 hover:text-indigo-800" @click="openEditDialog(p)">{{ t('settings.edit') }}</button>
              <button
                v-if="showDeleteConfirm !== p.id"
                :data-testid="`provider-delete-${p.id}`"
                class="text-red-600 hover:text-red-800"
                @click="showDeleteConfirm = p.id"
              >{{ t('settings.delete') }}</button>
              <span v-else class="space-x-2">
                <button :data-testid="`provider-confirm-delete-${p.id}`" class="text-red-700 font-medium" @click="confirmDelete(p.id)">{{ t('settings.confirm') }}</button>
                <button :data-testid="`provider-cancel-delete-${p.id}`" class="text-gray-500" @click="showDeleteConfirm = null">{{ t('settings.cancel') }}</button>
              </span>
            </td>
          </tr>
          <tr v-if="providers.length === 0">
            <td colspan="7" class="px-6 py-12 text-center text-sm text-gray-500">
              {{ t('settings.noScmProviders') }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

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
            <option v-for="credential in credentials.filter(item => item.kind !== 'ssh_username_with_private_key')" :key="credential.id" :value="credential.id">
              {{ credential.name }} ({{ credential.kind }})
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
            <option v-for="credential in credentials.filter(item => item.kind === 'ssh_username_with_private_key')" :key="credential.id" :value="credential.id">
              {{ credential.name }}
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
</template>
