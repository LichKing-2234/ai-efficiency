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
    <ElCard shadow="never">
      <div class="space-y-4">
        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.ldapUrl') }}</label>
          <ElInput v-model="ldapForm.url" placeholder="ldap://ldap.example.com:389" class="mt-1" />
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.baseDn') }}</label>
          <ElInput v-model="ldapForm.base_dn" placeholder="dc=example,dc=com" class="mt-1" />
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.bindDn') }}</label>
          <ElInput v-model="ldapForm.bind_dn" placeholder="cn=admin,dc=example,dc=com" class="mt-1" />
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.bindPassword') }}</label>
          <ElInput v-model="ldapForm.bind_password" type="password" :placeholder="t('settings.keepCurrentPlaceholder')" class="mt-1" />
        </div>

        <div>
          <label class="block text-sm font-medium text-gray-700">{{ t('settings.userFilter') }}</label>
          <ElInput v-model="ldapForm.user_filter" placeholder="(uid=%s)" class="mt-1" />
        </div>

        <div class="flex items-center">
          <ElSwitch v-model="ldapForm.tls" id="ldap-tls" />
          <label for="ldap-tls" class="ml-2 text-sm text-gray-700">{{ t('settings.enableTls') }}</label>
        </div>

        <ElAlert v-if="ldapError" type="error" :title="ldapError" :closable="false" />
        <ElAlert v-if="ldapSuccess" type="success" :title="ldapSuccess" :closable="false" />

        <div class="flex justify-end space-x-3">
          <ElButton :loading="ldapTesting" @click="handleTestLDAP">{{ t('settings.testConnection') }}</ElButton>
          <ElButton type="primary" :loading="ldapSaving" @click="handleSaveLDAP">{{ t('settings.save') }}</ElButton>
        </div>
      </div>
    </ElCard>
    <QuotaResetApprovalSettings :credentials="credentials" />
    <DirectorySyncSettings />
  </div>
</template>
