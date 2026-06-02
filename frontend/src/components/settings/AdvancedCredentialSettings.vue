<script setup lang="ts">
import { useI18n } from '@/i18n'
import type { Credential } from '@/types'

const { t } = useI18n()

defineProps<{
  credentials: Credential[]
  showDeleteConfirm: number | null
}>()

defineEmits<{
  (e: 'add'): void
  (e: 'edit', credential: Credential): void
  (e: 'request-delete', id: number): void
  (e: 'confirm-delete', id: number): void
  (e: 'cancel-delete'): void
}>()
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
        @click="$emit('add')"
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
            <button class="font-medium text-indigo-600 hover:text-indigo-800" @click="$emit('edit', cred)">{{ t('settings.edit') }}</button>
            <button
              v-if="showDeleteConfirm !== cred.id"
              class="text-red-600 hover:text-red-800"
              @click="$emit('request-delete', cred.id)"
            >{{ t('settings.delete') }}</button>
            <template v-else>
              <button class="font-medium text-red-700" @click="$emit('confirm-delete', cred.id)">{{ t('settings.confirm') }}</button>
              <button class="text-gray-500" @click="$emit('cancel-delete')">{{ t('settings.cancel') }}</button>
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
              <button class="text-indigo-600 hover:text-indigo-800" @click="$emit('edit', cred)">{{ t('settings.edit') }}</button>
              <button
                v-if="showDeleteConfirm !== cred.id"
                class="text-red-600 hover:text-red-800"
                @click="$emit('request-delete', cred.id)"
              >{{ t('settings.delete') }}</button>
              <span v-else class="space-x-2">
                <button class="text-red-700 font-medium" @click="$emit('confirm-delete', cred.id)">{{ t('settings.confirm') }}</button>
                <button class="text-gray-500" @click="$emit('cancel-delete')">{{ t('settings.cancel') }}</button>
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
</template>
