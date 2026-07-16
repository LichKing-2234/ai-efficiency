<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { storeToRefs } from 'pinia'
import client from '@/api/client'
import { useI18n } from '@/i18n'
import DirectorySyncSettings from '@/components/settings/DirectorySyncSettings.vue'
import QuotaResetApprovalSettings from '@/components/settings/QuotaResetApprovalSettings.vue'
import { useSettingsResourcesStore } from '@/stores/settingsResources'

const { t } = useI18n()
const settingsResources = useSettingsResourcesStore()
const { credentials } = storeToRefs(settingsResources)
const ldapForm = ref({ url: '', base_dn: '', bind_dn: '', bind_password: '', user_filter: '', tls: false })
const ldapSaving = ref(false)
const ldapTesting = ref(false)
const ldapError = ref('')
const ldapSuccess = ref('')

onMounted(() => {
  void settingsResources.loadCredentials()
  void fetchLDAPConfig()
})

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
    // An empty form represents an unconfigured directory.
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
  } catch (error: any) {
    ldapError.value = error.response?.data?.message || t('settings.ldapSaveFailed')
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
  } catch (error: any) {
    ldapError.value = error.response?.data?.message || t('settings.ldapTestFailed')
  } finally {
    ldapTesting.value = false
  }
}
</script>

<template>
  <div class="space-y-4">
    <h2 class="text-xl font-bold text-gray-900">{{ t('settings.organizationLogin') }}</h2>
    <p class="text-sm text-gray-500">{{ t('settings.organizationLoginSubtitle') }}</p>
    <div class="overflow-hidden rounded-lg bg-white shadow p-6">
      <div class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.ldapUrl') }}</label>
          <input v-model="ldapForm.url" type="text" placeholder="ldap://ldap.example.com:389" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.baseDn') }}</label>
          <input v-model="ldapForm.base_dn" type="text" placeholder="dc=example,dc=com" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.bindDn') }}</label>
          <input v-model="ldapForm.bind_dn" type="text" placeholder="cn=admin,dc=example,dc=com" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.bindPassword') }}</label>
          <input v-model="ldapForm.bind_password" type="password" :placeholder="t('settings.keepCurrentPlaceholder')" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.userFilter') }}</label>
          <input v-model="ldapForm.user_filter" type="text" placeholder="(uid=%s)" class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm" />
        </div>

        <div class="flex items-center">
          <input v-model="ldapForm.tls" type="checkbox" id="ldap-tls" class="h-4 w-4 rounded border-gray-300 text-indigo-600 focus:ring-indigo-500" />
          <label for="ldap-tls" class="ml-2 text-sm text-gray-700">{{ t('settings.enableTls') }}</label>
        </div>

        <div v-if="ldapError" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ ldapError }}</div>
        <div v-if="ldapSuccess" class="rounded-md bg-green-50 p-3 text-sm text-green-700">{{ ldapSuccess }}</div>

        <div class="flex justify-end space-x-3">
          <button @click="handleTestLDAP" :disabled="ldapTesting" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50">
            {{ ldapTesting ? t('settings.testing') : t('settings.testConnection') }}
          </button>
          <button @click="handleSaveLDAP" :disabled="ldapSaving" class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
            {{ ldapSaving ? t('settings.saving') : t('settings.save') }}
          </button>
        </div>
      </div>
    </div>
    <QuotaResetApprovalSettings :credentials="credentials" />
    <DirectorySyncSettings />
  </div>
</template>
