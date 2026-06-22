<script setup lang="ts">
import { useI18n } from '@/i18n'
import DirectorySyncSettings from '@/components/settings/DirectorySyncSettings.vue'
import type { Credential } from '@/types'

const { t } = useI18n()

defineProps<{
  ldapForm: {
    url: string
    base_dn: string
    bind_dn: string
    bind_password: string
    user_filter: string
    tls: boolean
  }
  ldapSaving: boolean
  ldapTesting: boolean
  ldapError: string
  ldapSuccess: string
  credentials: Credential[]
}>()

defineEmits<{
  (e: 'test'): void
  (e: 'save'): void
}>()
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
          <button @click="$emit('test')" :disabled="ldapTesting" class="rounded-md border border-gray-300 px-4 py-2 text-sm text-gray-700 hover:bg-gray-50 disabled:opacity-50">
            {{ ldapTesting ? t('settings.testing') : t('settings.testConnection') }}
          </button>
          <button @click="$emit('save')" :disabled="ldapSaving" class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 disabled:opacity-50">
            {{ ldapSaving ? t('settings.saving') : t('settings.save') }}
          </button>
        </div>
      </div>
    </div>
    <DirectorySyncSettings :credentials="credentials" />
  </div>
</template>
