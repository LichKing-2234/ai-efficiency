<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { createRelayProvider, deleteRelayProvider, listRelayProviders, updateRelayProvider } from '@/api/relayProvider'
import { useMediaQuery } from '@/composables/useMediaQuery'
import { useI18n } from '@/i18n'
import type { RelayProvider } from '@/types'

const { t } = useI18n()
const isDesktop = useMediaQuery('(min-width: 1280px)')
const relayLoading = ref(true)
const relayProviders = ref<RelayProvider[]>([])
const confirmingRelayId = ref<number | null>(null)
const deletingRelayId = ref<number | null>(null)
const showRelayDialog = ref(false)
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

async function confirmDeleteRelay(id: number, event: MouseEvent, close: (event: MouseEvent) => void) {
  if (deletingRelayId.value !== null) return

  deletingRelayId.value = id
  try {
    await deleteRelayProvider(id)
    confirmingRelayId.value = null
    close(event)
    await fetchRelayProviders()
  } catch {
    // Keep the row available for another attempt.
  } finally {
    deletingRelayId.value = null
  }
}

function setRelayDeleteVisibility(id: number, visible: boolean) {
  if (visible) {
    confirmingRelayId.value = id
  } else if (confirmingRelayId.value === id && deletingRelayId.value === null) {
    confirmingRelayId.value = null
  }
}

function cancelRelayDelete(event: MouseEvent, close: (event: MouseEvent) => void) {
  if (deletingRelayId.value !== null) return
  confirmingRelayId.value = null
  close(event)
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
      <ElButton type="primary" @click="openAddRelayDialog">
        {{ t('settings.addRelayProvider') }}
      </ElButton>
    </div>

    <div v-if="relayLoading" class="text-center text-gray-500 py-12">{{ t('settings.loadingRelayProviders') }}</div>

    <div v-else class="rounded-lg bg-white shadow">
      <div v-if="!isDesktop && relayProviders.length > 0" class="space-y-3 p-4">
        <article v-for="provider in relayProviders" :key="provider.id" class="rounded-lg border border-gray-100 p-4">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="truncate text-sm font-medium text-gray-900">{{ provider.display_name }}</div>
              <div class="mt-1 break-all font-mono text-xs text-gray-500">{{ provider.name }}</div>
            </div>
            <ElTag :type="provider.enabled ? 'success' : 'info'">
              {{ provider.enabled ? t('settings.enabled') : t('settings.disabled') }}
            </ElTag>
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
            <ElButton :data-testid="`relay-provider-edit-${provider.id}`" link type="primary" @click="openEditRelayDialog(provider)">{{ t('settings.edit') }}</ElButton>
            <ElPopconfirm
              :title="`${t('settings.confirm')} ${t('settings.delete')}?`"
              :teleported="false"
              :visible="confirmingRelayId === provider.id"
              @update:visible="setRelayDeleteVisibility(provider.id, $event)"
            >
              <template #reference>
                <ElButton
                  :data-testid="`relay-provider-delete-${provider.id}`"
                  :disabled="deletingRelayId !== null"
                  link
                  type="danger"
                  @click="confirmingRelayId = provider.id"
                >{{ t('settings.delete') }}</ElButton>
              </template>
              <template #actions="{ confirm, cancel }">
                <ElButton
                  :data-testid="`relay-provider-confirm-delete-${provider.id}`"
                  :loading="deletingRelayId === provider.id"
                  :disabled="deletingRelayId !== null"
                  link
                  type="danger"
                  @click="confirmDeleteRelay(provider.id, $event, confirm)"
                >{{ t('settings.confirm') }}</ElButton>
                <ElButton
                  :data-testid="`relay-provider-cancel-delete-${provider.id}`"
                  :disabled="deletingRelayId !== null"
                  link
                  @click="cancelRelayDelete($event, cancel)"
                >{{ t('settings.cancel') }}</ElButton>
              </template>
            </ElPopconfirm>
          </div>
        </article>
      </div>
      <div v-else-if="!isDesktop" class="px-6 py-12 text-center text-sm text-gray-500">
        {{ t('settings.noRelayProviders') }}
      </div>

      <table v-if="isDesktop" class="min-w-full divide-y divide-gray-200">
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
              <ElTag v-if="provider.is_primary" type="success">{{ t('settings.primary') }}</ElTag>
              <ElTag v-else type="info">{{ t('settings.secondary') }}</ElTag>
            </td>
            <td class="px-6 py-4 font-mono text-xs text-gray-500">
              <div>{{ provider.base_url }}</div>
            </td>
            <td class="px-6 py-4">
              <ElTag :type="provider.enabled ? 'success' : 'info'">{{ provider.enabled ? t('settings.enabled') : t('settings.disabled') }}</ElTag>
            </td>
            <td class="whitespace-nowrap px-6 py-4 text-right text-sm space-x-3">
              <ElButton :data-testid="`relay-provider-edit-${provider.id}`" link type="primary" @click="openEditRelayDialog(provider)">{{ t('settings.edit') }}</ElButton>
              <ElPopconfirm
                :title="`${t('settings.confirm')} ${t('settings.delete')}?`"
                :teleported="false"
                :visible="confirmingRelayId === provider.id"
                @update:visible="setRelayDeleteVisibility(provider.id, $event)"
              >
                <template #reference>
                  <ElButton
                    :data-testid="`relay-provider-delete-${provider.id}`"
                    :disabled="deletingRelayId !== null"
                    link
                    type="danger"
                    @click="confirmingRelayId = provider.id"
                  >{{ t('settings.delete') }}</ElButton>
                </template>
                <template #actions="{ confirm, cancel }">
                  <ElButton
                    :data-testid="`relay-provider-confirm-delete-${provider.id}`"
                    :loading="deletingRelayId === provider.id"
                    :disabled="deletingRelayId !== null"
                    link
                    type="danger"
                    @click="confirmDeleteRelay(provider.id, $event, confirm)"
                  >{{ t('settings.confirm') }}</ElButton>
                  <ElButton
                    :data-testid="`relay-provider-cancel-delete-${provider.id}`"
                    :disabled="deletingRelayId !== null"
                    link
                    @click="cancelRelayDelete($event, cancel)"
                  >{{ t('settings.cancel') }}</ElButton>
                </template>
              </ElPopconfirm>
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

  <ElDialog
    v-model="showRelayDialog"
    data-testid="relay-provider-dialog"
    :title="editingRelayId ? t('settings.editRelayProvider') : t('settings.addRelayProvider')"
    width="min(90vw, 42rem)"
    :teleported="false"
    destroy-on-close
  >
      <div class="grid gap-4 md:grid-cols-2">
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.name') }}</label>
          <ElInput name="relay-provider-name" v-model="relayForm.name" :disabled="!!editingRelayId" placeholder="relay-main" class="mt-1" autofocus />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.displayName') }}</label>
          <ElInput name="relay-provider-display-name" v-model="relayForm.display_name" placeholder="Relay Main" class="mt-1" />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.baseUrl') }}</label>
          <ElInput name="relay-provider-base-url" v-model="relayForm.base_url" placeholder="https://relay.example.com" class="mt-1" />
        </div>
        <div class="md:col-span-2">
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.adminApiKey') }}</label>
          <ElInput name="relay-provider-admin-api-key" v-model="relayForm.admin_api_key" type="password" :placeholder="editingRelayId ? t('settings.keepCurrentPlaceholder') : 'admin-...'" class="mt-1" />
          <p class="mt-1 text-xs text-gray-400">{{ t('settings.relayKeyHelp') }}</p>
        </div>
        <div class="flex items-center">
          <ElSwitch id="relay-primary" v-model="relayForm.is_primary" />
          <label for="relay-primary" class="ml-2 text-sm text-gray-700">{{ t('settings.primaryProvider') }}</label>
        </div>
        <div class="flex items-center">
          <ElSwitch id="relay-enabled" v-model="relayForm.enabled" />
          <label for="relay-enabled" class="ml-2 text-sm text-gray-700">{{ t('settings.enabled') }}</label>
        </div>
      </div>

      <ElAlert v-if="relayFormError" class="mt-4" type="error" :title="relayFormError" :closable="false" />
      <template #footer>
      <div class="mt-5 flex justify-end space-x-3">
        <ElButton @click="closeRelayDialog">{{ t('settings.cancel') }}</ElButton>
        <ElButton type="primary" :loading="relayFormLoading" @click="handleRelaySubmit">
          {{ editingRelayId ? t('settings.updateRelayProvider') : t('settings.createRelayProvider') }}
        </ElButton>
      </div>
      </template>
  </ElDialog>
</template>
