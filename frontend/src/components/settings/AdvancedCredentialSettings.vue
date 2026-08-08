<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import { createCredential, deleteCredential, updateCredential } from '@/api/credential'
import { useMediaQuery } from '@/composables/useMediaQuery'
import { useI18n } from '@/i18n'
import { useSettingsResourcesStore } from '@/stores/settingsResources'
import type { Credential } from '@/types'

const { t } = useI18n()
const isDesktop = useMediaQuery('(min-width: 1280px)')
const settingsResources = useSettingsResourcesStore()
const { credentials } = storeToRefs(settingsResources)
const confirmingCredentialId = ref<number | null>(null)
const deletingCredentialId = ref<number | null>(null)
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

onMounted(() => {
  void settingsResources.loadCredentials()
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

async function confirmDeleteCredential(id: number, event: MouseEvent, close: (event: MouseEvent) => void) {
  if (deletingCredentialId.value !== null) return

  deletingCredentialId.value = id
  try {
    await deleteCredential(id)
    confirmingCredentialId.value = null
    close(event)
    await settingsResources.loadCredentials({ force: true })
  } catch {
    // Keep the row available for another attempt.
  } finally {
    deletingCredentialId.value = null
  }
}

function setCredentialDeleteVisibility(id: number, visible: boolean) {
  if (visible) {
    confirmingCredentialId.value = id
  } else if (confirmingCredentialId.value === id && deletingCredentialId.value === null) {
    confirmingCredentialId.value = null
  }
}

function cancelCredentialDelete(event: MouseEvent, close: (event: MouseEvent) => void) {
  if (deletingCredentialId.value !== null) return
  confirmingCredentialId.value = null
  close(event)
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h2 class="text-xl font-bold text-gray-900">{{ t('settings.credentialStore') }}</h2>
        <p class="mt-1 text-sm text-gray-500">{{ t('settings.credentialStoreHelp') }}</p>
      </div>
      <ElButton type="primary" @click="openAddCredentialDialog">
        {{ t('settings.addCredential') }}
      </ElButton>
    </div>

    <div class="rounded-lg bg-white shadow">
      <div v-if="!isDesktop && credentials.length > 0" class="space-y-3 p-4">
        <article v-for="cred in credentials" :key="cred.id" class="rounded-lg border border-gray-100 p-4">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="truncate text-sm font-medium text-gray-900">{{ cred.name }}</div>
              <div class="mt-1 truncate text-xs text-gray-500">{{ cred.kind }}</div>
            </div>
            <ElTag type="info">{{ cred.usage_count }}</ElTag>
          </div>
          <div class="mt-3 break-all rounded bg-gray-50 p-2 font-mono text-xs text-gray-500">
            {{ JSON.stringify(cred.summary || {}) }}
          </div>
          <div class="mt-3 flex flex-wrap gap-3 text-sm">
            <ElButton :data-testid="`credential-edit-${cred.id}`" link type="primary" @click="openEditCredentialDialog(cred)">{{ t('settings.edit') }}</ElButton>
            <ElPopconfirm
              :title="`${t('settings.confirm')} ${t('settings.delete')}?`"
              :teleported="false"
              :visible="confirmingCredentialId === cred.id"
              @update:visible="setCredentialDeleteVisibility(cred.id, $event)"
            >
              <template #reference>
                <ElButton
                  :data-testid="`credential-delete-${cred.id}`"
                  :disabled="deletingCredentialId !== null"
                  link
                  type="danger"
                  @click="confirmingCredentialId = cred.id"
                >{{ t('settings.delete') }}</ElButton>
              </template>
              <template #actions="{ confirm, cancel }">
                <ElButton
                  :data-testid="`credential-confirm-delete-${cred.id}`"
                  :loading="deletingCredentialId === cred.id"
                  :disabled="deletingCredentialId !== null"
                  link
                  type="danger"
                  @click="confirmDeleteCredential(cred.id, $event, confirm)"
                >{{ t('settings.confirm') }}</ElButton>
                <ElButton
                  :data-testid="`credential-cancel-delete-${cred.id}`"
                  :disabled="deletingCredentialId !== null"
                  link
                  @click="cancelCredentialDelete($event, cancel)"
                >{{ t('settings.cancel') }}</ElButton>
              </template>
            </ElPopconfirm>
          </div>
        </article>
      </div>
      <div v-else-if="!isDesktop" class="px-6 py-12 text-center text-sm text-gray-500">
        {{ t('settings.noCredentials') }}
      </div>

      <table v-if="isDesktop" class="min-w-full divide-y divide-gray-200">
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
              <ElButton :data-testid="`credential-edit-${cred.id}`" link type="primary" @click="openEditCredentialDialog(cred)">{{ t('settings.edit') }}</ElButton>
              <ElPopconfirm
                :title="`${t('settings.confirm')} ${t('settings.delete')}?`"
                :teleported="false"
                :visible="confirmingCredentialId === cred.id"
                @update:visible="setCredentialDeleteVisibility(cred.id, $event)"
              >
                <template #reference>
                  <ElButton
                    :data-testid="`credential-delete-${cred.id}`"
                    :disabled="deletingCredentialId !== null"
                    link
                    type="danger"
                    @click="confirmingCredentialId = cred.id"
                  >{{ t('settings.delete') }}</ElButton>
                </template>
                <template #actions="{ confirm, cancel }">
                  <ElButton
                    :data-testid="`credential-confirm-delete-${cred.id}`"
                    :loading="deletingCredentialId === cred.id"
                    :disabled="deletingCredentialId !== null"
                    link
                    type="danger"
                    @click="confirmDeleteCredential(cred.id, $event, confirm)"
                  >{{ t('settings.confirm') }}</ElButton>
                  <ElButton
                    :data-testid="`credential-cancel-delete-${cred.id}`"
                    :disabled="deletingCredentialId !== null"
                    link
                    @click="cancelCredentialDelete($event, cancel)"
                  >{{ t('settings.cancel') }}</ElButton>
                </template>
              </ElPopconfirm>
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

  <ElDialog
    v-model="showCredentialDialog"
    data-testid="credential-dialog"
    :title="editingCredentialId ? t('settings.editCredential') : t('settings.addCredential')"
    width="min(90vw, 42rem)"
    :teleported="false"
    destroy-on-close
  >
      <div class="space-y-3">
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.name') }}</label>
          <ElInput name="credential-name" v-model="credentialForm.name" class="mt-1" autofocus />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.description') }}</label>
          <ElInput v-model="credentialForm.description" class="mt-1" />
        </div>
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.kind') }}</label>
          <ElSelect v-model="credentialForm.kind" data-testid="credential-kind" class="mt-1 w-full" :teleported="false">
            <ElOption value="secret_text" :label="t('settings.secretTextKind')" />
            <ElOption value="username_password" :label="t('settings.usernamePasswordKind')" />
            <ElOption value="ssh_username_with_private_key" :label="t('settings.sshPrivateKeyKind')" />
          </ElSelect>
        </div>
        <div v-if="credentialForm.kind === 'secret_text'">
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.secretText') }}</label>
          <ElInput name="credential-secret-text" v-model="credentialForm.text" type="textarea" :rows="4" class="mt-1 font-mono" />
        </div>
        <template v-else-if="credentialForm.kind === 'username_password'">
          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.username') }}</label>
            <ElInput v-model="credentialForm.username" class="mt-1" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.password') }}</label>
            <ElInput v-model="credentialForm.password" type="password" class="mt-1" />
          </div>
        </template>
        <template v-else>
          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.sshUsername') }}</label>
            <ElInput v-model="credentialForm.username" class="mt-1" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.privateKey') }}</label>
            <ElInput v-model="credentialForm.private_key" type="textarea" :rows="6" class="mt-1 font-mono" />
          </div>
          <div>
            <label class="block text-sm font-medium text-gray-700">{{ t('settings.passphrase') }}</label>
            <ElInput v-model="credentialForm.passphrase" type="password" class="mt-1" />
          </div>
        </template>
        <ElAlert v-if="credentialFormError" type="error" :title="credentialFormError" :closable="false" />
      </div>
      <template #footer>
        <ElButton @click="closeCredentialDialog">{{ t('settings.cancel') }}</ElButton>
        <ElButton type="primary" :loading="credentialFormLoading" @click="handleCredentialSubmit">
          {{ t('settings.saveCredential') }}
        </ElButton>
      </template>
  </ElDialog>
</template>
