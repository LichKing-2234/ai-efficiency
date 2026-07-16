<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { createRelayProvider, deleteRelayProvider, listRelayProviders, updateRelayProvider } from '@/api/relayProvider'
import { useModalFocus } from '@/composables/useModalFocus'
import { useI18n } from '@/i18n'
import type { RelayProvider } from '@/types'

const { t } = useI18n()
const relayLoading = ref(true)
const relayProviders = ref<RelayProvider[]>([])
const showDeleteConfirm = ref<number | null>(null)
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

onMounted(fetchRelayProviders)

const { handleKeydown: handleRelayDialogKeydown } = useModalFocus(showRelayDialog, relayDialog, {
  initialFocus: relayNameInput,
  onClose: closeRelayDialog,
})

async function fetchRelayProviders() {
  relayLoading.value = true
  try {
    const response = await listRelayProviders()
    const data = response.data.data
    relayProviders.value = Array.isArray(data) ? data : []
  } catch {
    relayProviders.value = []
  } finally {
    relayLoading.value = false
  }
}

function closeRelayDialog() {
  showRelayDialog.value = false
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
  } catch (error: any) {
    relayFormError.value = error.response?.data?.message || t('settings.operationFailed')
  } finally {
    relayFormLoading.value = false
  }
}

async function confirmDeleteRelay(id: number) {
  try {
    await deleteRelayProvider(id)
    showDeleteConfirm.value = null
    await fetchRelayProviders()
  } catch {
    // Keep the row available for another attempt.
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h2 class="text-xl font-bold text-gray-900">{{ t('settings.aiServices') }}</h2>
        <h3 class="mt-3 text-base font-semibold text-gray-900">{{ t('settings.relayProviders') }}</h3>
        <p class="mt-1 text-sm text-gray-500">{{ t('settings.relayProvidersHelp') }}</p>
      </div>
      <button
        class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700"
        @click="openAddRelayDialog"
      >
        {{ t('settings.addRelayProvider') }}
      </button>
    </div>

    <div v-if="relayLoading" class="text-center text-gray-500 py-12">{{ t('settings.loadingRelayProviders') }}</div>

    <div v-else class="rounded-lg bg-white shadow">
      <div v-if="relayProviders.length > 0" class="space-y-3 p-4 md:hidden">
        <article v-for="provider in relayProviders" :key="provider.id" class="rounded-lg border border-gray-100 p-4">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="truncate text-sm font-medium text-gray-900">{{ provider.display_name }}</div>
              <div class="mt-1 break-all font-mono text-xs text-gray-500">{{ provider.name }}</div>
            </div>
            <span
              class="shrink-0 rounded-full px-2 py-0.5 text-xs font-semibold"
              :class="provider.enabled ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-500'"
            >
              {{ provider.enabled ? t('settings.enabled') : t('settings.disabled') }}
            </span>
          </div>
          <dl class="mt-3 space-y-2 text-xs">
            <div>
              <dt class="text-gray-400">{{ t('settings.primary') }}</dt>
              <dd class="mt-1 text-gray-800">{{ provider.is_primary ? t('settings.primary') : t('settings.secondary') }}</dd>
            </div>
            <div>
              <dt class="text-gray-400">{{ t('settings.baseUrl') }}</dt>
              <dd class="mt-1 break-all font-mono text-gray-800">{{ provider.base_url }}</dd>
            </div>
          </dl>
          <div class="mt-3 flex flex-wrap gap-3 text-sm">
            <button :data-testid="`relay-provider-edit-${provider.id}`" class="font-medium text-indigo-600 hover:text-indigo-800" @click="openEditRelayDialog(provider)">{{ t('settings.edit') }}</button>
            <button
              v-if="showDeleteConfirm !== provider.id"
              :data-testid="`relay-provider-delete-${provider.id}`"
              class="text-red-600 hover:text-red-800"
              @click="showDeleteConfirm = provider.id"
            >{{ t('settings.delete') }}</button>
            <template v-else>
              <button :data-testid="`relay-provider-confirm-delete-${provider.id}`" class="font-medium text-red-700" @click="confirmDeleteRelay(provider.id)">{{ t('settings.confirm') }}</button>
              <button :data-testid="`relay-provider-cancel-delete-${provider.id}`" class="text-gray-500" @click="showDeleteConfirm = null">{{ t('settings.cancel') }}</button>
            </template>
          </div>
        </article>
      </div>
      <div v-else class="px-6 py-12 text-center text-sm text-gray-500 md:hidden">
        {{ t('settings.noRelayProviders') }}
      </div>

      <table class="hidden min-w-full divide-y divide-gray-200 md:table">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.name') }}</th>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.primary') }}</th>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.baseUrl') }}</th>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.state') }}</th>
            <th class="px-6 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.actions') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200">
          <tr v-for="provider in relayProviders" :key="provider.id">
            <td class="px-6 py-4">
              <div class="text-sm font-medium text-gray-900">{{ provider.display_name }}</div>
              <div class="mt-1 font-mono text-xs text-gray-500">{{ provider.name }}</div>
            </td>
            <td class="px-6 py-4">
              <div v-if="provider.is_primary" class="inline-flex rounded-full bg-emerald-100 px-2 py-1 text-xs font-semibold text-emerald-700">{{ t('settings.primary') }}</div>
              <div v-else class="inline-flex rounded-full bg-gray-100 px-2 py-1 text-xs font-semibold text-gray-500">{{ t('settings.secondary') }}</div>
            </td>
            <td class="px-6 py-4 font-mono text-xs text-gray-500">
              <div>{{ provider.base_url }}</div>
            </td>
            <td class="px-6 py-4">
              <span
                class="inline-flex rounded-full px-2 py-1 text-xs font-semibold"
                :class="provider.enabled ? 'bg-green-100 text-green-800' : 'bg-gray-100 text-gray-500'"
              >{{ provider.enabled ? t('settings.enabled') : t('settings.disabled') }}</span>
            </td>
            <td class="whitespace-nowrap px-6 py-4 text-right text-sm space-x-3">
              <button :data-testid="`relay-provider-edit-${provider.id}`" class="text-indigo-600 hover:text-indigo-800" @click="openEditRelayDialog(provider)">{{ t('settings.edit') }}</button>
              <button
                v-if="showDeleteConfirm !== provider.id"
                :data-testid="`relay-provider-delete-${provider.id}`"
                class="text-red-600 hover:text-red-800"
                @click="showDeleteConfirm = provider.id"
              >{{ t('settings.delete') }}</button>
              <span v-else class="space-x-2">
                <button :data-testid="`relay-provider-confirm-delete-${provider.id}`" class="text-red-700 font-medium" @click="confirmDeleteRelay(provider.id)">{{ t('settings.confirm') }}</button>
                <button :data-testid="`relay-provider-cancel-delete-${provider.id}`" class="text-gray-500" @click="showDeleteConfirm = null">{{ t('settings.cancel') }}</button>
              </span>
            </td>
          </tr>
          <tr v-if="relayProviders.length === 0">
            <td colspan="5" class="px-6 py-12 text-center text-sm text-gray-500">
              {{ t('settings.noRelayProviders') }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

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
</template>
