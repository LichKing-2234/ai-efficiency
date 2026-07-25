<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { createCredential, deleteCredential, updateCredential } from '@/api/credential'
import { useModalFocus } from '@/composables/useModalFocus'
import { useI18n } from '@/i18n'
import { useSettingsResourcesStore } from '@/stores/settingsResources'
import type { Credential } from '@/types'

const { t } = useI18n()
const settingsResources = useSettingsResourcesStore()
const { credentials } = storeToRefs(settingsResources)
const showDeleteConfirm = ref<number | null>(null)
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

onMounted(() => {
  void settingsResources.loadCredentials()
})

const { handleKeydown: handleCredentialDialogKeydown } = useModalFocus(showCredentialDialog, credentialDialog, {
  initialFocus: credentialNameInput,
  onClose: closeCredentialDialog,
})

function closeCredentialDialog() {
  showCredentialDialog.value = false
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
    await settingsResources.loadCredentials({ force: true })
  } catch (error: any) {
    credentialFormError.value = error.response?.data?.message || t('settings.operationFailed')
  } finally {
    credentialFormLoading.value = false
  }
}

async function confirmDeleteCredential(id: number) {
  try {
    await deleteCredential(id)
    showDeleteConfirm.value = null
    await settingsResources.loadCredentials({ force: true })
  } catch {
    // Keep the row available for another attempt.
  }
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h2 class="text-xl font-bold text-gray-900">{{ t('settings.credentialStore') }}</h2>
        <p class="mt-1 text-sm text-gray-500">{{ t('settings.credentialStoreHelp') }}</p>
      </div>
      <button
        class="rounded-md bg-gray-900 px-4 py-2 text-sm font-medium text-white hover:bg-black"
        @click="openAddCredentialDialog"
      >
        {{ t('settings.addCredential') }}
      </button>
    </div>

    <div class="rounded-lg bg-white shadow">
      <div v-if="credentials.length > 0" class="space-y-3 p-4 md:hidden">
        <article v-for="cred in credentials" :key="cred.id" class="rounded-lg border border-gray-100 p-4">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="truncate text-sm font-medium text-gray-900">{{ cred.name }}</div>
              <div class="mt-1 truncate text-xs text-gray-500">{{ cred.kind }}</div>
            </div>
            <span class="shrink-0 rounded-full bg-slate-100 px-2 py-0.5 text-xs font-semibold text-slate-700">
              {{ cred.usage_count }}
            </span>
          </div>
          <div class="mt-3 break-all rounded bg-gray-50 p-2 font-mono text-xs text-gray-500">
            {{ JSON.stringify(cred.summary || {}) }}
          </div>
          <div class="mt-3 flex flex-wrap gap-3 text-sm">
            <button class="font-medium text-indigo-600 hover:text-indigo-800" @click="openEditCredentialDialog(cred)">{{ t('settings.edit') }}</button>
            <button
              v-if="showDeleteConfirm !== cred.id"
              class="text-red-600 hover:text-red-800"
              @click="showDeleteConfirm = cred.id"
            >{{ t('settings.delete') }}</button>
            <template v-else>
              <button class="font-medium text-red-700" @click="confirmDeleteCredential(cred.id)">{{ t('settings.confirm') }}</button>
              <button class="text-gray-500" @click="showDeleteConfirm = null">{{ t('settings.cancel') }}</button>
            </template>
          </div>
        </article>
      </div>
      <div v-else class="px-6 py-12 text-center text-sm text-gray-500 md:hidden">
        {{ t('settings.noCredentials') }}
      </div>

      <table class="hidden min-w-full divide-y divide-gray-200 md:table">
        <thead class="bg-gray-50">
          <tr>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.name') }}</th>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.kind') }}</th>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.usage') }}</th>
            <th class="px-6 py-3 text-left text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.summary') }}</th>
            <th class="px-6 py-3 text-right text-xs font-medium uppercase tracking-wider text-gray-500">{{ t('settings.actions') }}</th>
          </tr>
        </thead>
        <tbody class="divide-y divide-gray-200">
          <tr v-for="cred in credentials" :key="cred.id">
            <td class="whitespace-nowrap px-6 py-4 text-sm font-medium text-gray-900">{{ cred.name }}</td>
            <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-600">{{ cred.kind }}</td>
            <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-600">{{ cred.usage_count }}</td>
            <td class="break-all px-6 py-4 font-mono text-xs text-gray-500">{{ JSON.stringify(cred.summary || {}) }}</td>
            <td class="whitespace-nowrap px-6 py-4 text-right text-sm space-x-3">
              <button class="text-indigo-600 hover:text-indigo-800" @click="openEditCredentialDialog(cred)">{{ t('settings.edit') }}</button>
              <button
                v-if="showDeleteConfirm !== cred.id"
                class="text-red-600 hover:text-red-800"
                @click="showDeleteConfirm = cred.id"
              >{{ t('settings.delete') }}</button>
              <span v-else class="space-x-2">
                <button class="text-red-700 font-medium" @click="confirmDeleteCredential(cred.id)">{{ t('settings.confirm') }}</button>
                <button class="text-gray-500" @click="showDeleteConfirm = null">{{ t('settings.cancel') }}</button>
              </span>
            </td>
          </tr>
          <tr v-if="credentials.length === 0">
            <td colspan="5" class="px-6 py-12 text-center text-sm text-gray-500">
              {{ t('settings.noCredentials') }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>

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
</template>
