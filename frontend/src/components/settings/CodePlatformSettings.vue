<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { createProvider, deleteProvider, listProviders, updateProvider } from '@/api/scmProvider'
import { useWideContentLayout } from '@/composables/useMediaQuery'
import { useI18n } from '@/i18n'
import { useSettingsResourcesStore } from '@/stores/settingsResources'
import type { SCMProvider } from '@/types'

const { locale, t } = useI18n()
const isDesktop = useWideContentLayout()
const settingsResources = useSettingsResourcesStore()
const { credentials } = storeToRefs(settingsResources)
const providers = ref<SCMProvider[]>([])
const loading = ref(true)
const confirmingProviderId = ref<number | null>(null)
const deletingProviderId = ref<number | null>(null)
const showDialog = ref(false)
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

async function confirmDelete(id: number, event: MouseEvent, close: (event: MouseEvent) => void) {
  if (deletingProviderId.value !== null) return

  deletingProviderId.value = id
  try {
    await deleteProvider(id)
    confirmingProviderId.value = null
    close(event)
    await fetchProviders()
  } catch {
    // Keep the row available for another attempt.
  } finally {
    deletingProviderId.value = null
  }
}

function setProviderDeleteVisibility(id: number, visible: boolean) {
  if (visible) {
    confirmingProviderId.value = id
  } else if (confirmingProviderId.value === id && deletingProviderId.value === null) {
    confirmingProviderId.value = null
  }
}

function cancelProviderDelete(event: MouseEvent, close: (event: MouseEvent) => void) {
  if (deletingProviderId.value !== null) return
  confirmingProviderId.value = null
  close(event)
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h2 class="text-xl font-bold text-gray-900">{{ t('settings.codePlatforms') }}</h2>
        <p class="mt-1 text-sm text-gray-500">{{ t('settings.codePlatformsHelp') }}</p>
      </div>
      <ElButton v-if="providers.length > 0" type="primary" @click="openAddDialog">
        {{ t('settings.addProvider') }}
      </ElButton>
    </div>

    <div v-if="loading" class="py-12 text-center text-gray-500">{{ t('settings.loading') }}</div>

    <div v-else class="rounded-lg bg-white shadow">
      <ElEmpty
        v-if="providers.length === 0"
        data-testid="settings-empty-code-platforms"
        :description="t('settings.noScmProviders')"
        :image-size="64"
      >
        <ElButton data-testid="settings-empty-add-platform" type="primary" @click="openAddDialog">
          {{ t('settings.addProvider') }}
        </ElButton>
      </ElEmpty>
      <div v-else-if="!isDesktop" class="space-y-3 p-4">
        <article v-for="p in providers" :key="p.id" class="rounded-lg border border-gray-100 p-4">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="truncate text-sm font-medium text-gray-900">{{ p.name }}</div>
              <div class="mt-1 break-all font-mono text-xs text-gray-500">{{ p.base_url }}</div>
              <div v-if="p.ssh_host" class="mt-1 break-all font-mono text-xs text-gray-500">{{ p.ssh_host }}</div>
            </div>
            <ElTag :type="p.type === 'github' ? 'info' : 'primary'">
              {{ p.type }}
            </ElTag>
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
            <ElButton :data-testid="`provider-edit-${p.id}`" link type="primary" @click="openEditDialog(p)">{{ t('settings.edit') }}</ElButton>
            <ElPopconfirm
              :title="`${t('settings.confirm')} ${t('settings.delete')}?`"
              :teleported="false"
              :visible="confirmingProviderId === p.id"
              @update:visible="setProviderDeleteVisibility(p.id, $event)"
            >
              <template #reference>
                <ElButton
                  :data-testid="`provider-delete-${p.id}`"
                  :disabled="deletingProviderId !== null"
                  link
                  type="danger"
                  @click="confirmingProviderId = p.id"
                >{{ t('settings.delete') }}</ElButton>
              </template>
              <template #actions="{ confirm, cancel }">
                <ElButton
                  :data-testid="`provider-confirm-delete-${p.id}`"
                  :loading="deletingProviderId === p.id"
                  :disabled="deletingProviderId !== null"
                  link
                  type="danger"
                  @click="confirmDelete(p.id, $event, confirm)"
                >{{ t('settings.confirm') }}</ElButton>
                <ElButton
                  :data-testid="`provider-cancel-delete-${p.id}`"
                  :disabled="deletingProviderId !== null"
                  link
                  @click="cancelProviderDelete($event, cancel)"
                >{{ t('settings.cancel') }}</ElButton>
              </template>
            </ElPopconfirm>
          </div>
        </article>
      </div>
      <table v-if="isDesktop && providers.length > 0" class="min-w-full divide-y divide-gray-200">
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
              <ElTag :type="p.type === 'github' ? 'info' : 'primary'">
                {{ p.type }}
              </ElTag>
            </td>
            <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500 font-mono text-xs">{{ p.base_url }}</td>
            <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500 font-mono text-xs">{{ p.ssh_host || '—' }}</td>
            <td class="whitespace-nowrap px-6 py-4">
              <ElTag type="success">{{ p.status }}</ElTag>
            </td>
            <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500">{{ formatDate(p.created_at) }}</td>
            <td class="whitespace-nowrap px-6 py-4 text-right text-sm space-x-3">
              <ElButton :data-testid="`provider-edit-${p.id}`" link type="primary" @click="openEditDialog(p)">{{ t('settings.edit') }}</ElButton>
              <ElPopconfirm
                :title="`${t('settings.confirm')} ${t('settings.delete')}?`"
                :teleported="false"
                :visible="confirmingProviderId === p.id"
                @update:visible="setProviderDeleteVisibility(p.id, $event)"
              >
                <template #reference>
                  <ElButton
                    :data-testid="`provider-delete-${p.id}`"
                    :disabled="deletingProviderId !== null"
                    link
                    type="danger"
                    @click="confirmingProviderId = p.id"
                  >{{ t('settings.delete') }}</ElButton>
                </template>
                <template #actions="{ confirm, cancel }">
                  <ElButton
                    :data-testid="`provider-confirm-delete-${p.id}`"
                    :loading="deletingProviderId === p.id"
                    :disabled="deletingProviderId !== null"
                    link
                    type="danger"
                    @click="confirmDelete(p.id, $event, confirm)"
                  >{{ t('settings.confirm') }}</ElButton>
                  <ElButton
                    :data-testid="`provider-cancel-delete-${p.id}`"
                    :disabled="deletingProviderId !== null"
                    link
                    @click="cancelProviderDelete($event, cancel)"
                  >{{ t('settings.cancel') }}</ElButton>
                </template>
              </ElPopconfirm>
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

  <ElDialog
    v-model="showDialog"
    data-testid="code-platform-dialog"
    :title="editingId ? t('settings.editProvider') : t('settings.addScmProvider')"
    width="min(90vw, 32rem)"
    :teleported="false"
    destroy-on-close
  >
      <div class="space-y-3">
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.name') }}</label>
          <ElInput name="provider-name" v-model="form.name" placeholder="e.g. GitHub" class="mt-1" autofocus />
        </div>
        <div v-if="!editingId">
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.type') }}</label>
          <ElSelect v-model="form.type" class="mt-1 w-full" :teleported="false" @change="onTypeChange">
            <ElOption value="github" label="GitHub" />
            <ElOption value="bitbucket_server" label="Bitbucket Server" />
          </ElSelect>
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.baseUrl') }}</label>
          <ElInput v-model="form.base_url" placeholder="https://api.github.com" class="mt-1" />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.sshHost') }}</label>
          <ElInput name="provider-ssh-host" v-model="form.ssh_host" placeholder="git.example.com" class="mt-1" />
          <p class="mt-1 text-xs text-gray-400">{{ t('settings.sshHostHelp') }}</p>
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.apiCredential') }}</label>
          <ElSelect v-model="form.api_credential_id" data-testid="provider-api-credential" class="mt-1 w-full" :teleported="false">
            <ElOption :value="0" :label="t('settings.selectApiCredential')" disabled />
            <ElOption v-for="credential in credentials.filter(item => item.kind !== 'ssh_username_with_private_key')" :key="credential.id" :value="credential.id" :label="`${credential.name} (${credential.kind})`" />
          </ElSelect>
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.cloneProtocol') }}</label>
          <ElSelect v-model="form.clone_protocol" data-testid="provider-clone-protocol" class="mt-1 w-full" :teleported="false">
            <ElOption value="https" label="https" />
            <ElOption value="ssh" label="ssh" />
          </ElSelect>
        </div>
        <div v-if="form.clone_protocol === 'ssh'">
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.cloneCredential') }}</label>
          <ElSelect v-model="form.clone_credential_id" data-testid="provider-clone-credential" class="mt-1 w-full" :placeholder="t('settings.selectSshCredential')" :teleported="false" clearable>
            <ElOption v-for="credential in credentials.filter(item => item.kind === 'ssh_username_with_private_key')" :key="credential.id" :value="credential.id" :label="credential.name" />
          </ElSelect>
          <p class="mt-1 text-xs text-gray-400">{{ t('settings.sshCloneHelp') }}</p>
        </div>
        <ElAlert v-if="formError" type="error" :title="formError" :closable="false" />
      </div>

      <template #footer>
      <div class="mt-5 flex justify-end space-x-3">
        <ElButton @click="closeProviderDialog">{{ t('settings.cancel') }}</ElButton>
        <ElButton type="primary" :loading="formLoading" @click="handleSubmit">
          {{ editingId ? t('settings.update') : t('settings.create') }}
        </ElButton>
      </div>
      </template>
  </ElDialog>
</template>
