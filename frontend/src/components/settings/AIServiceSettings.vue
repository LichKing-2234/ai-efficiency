<script setup lang="ts">
import { useI18n } from '@/i18n'
import type { RelayProvider } from '@/types'

const { t } = useI18n()

defineProps<{
  relayLoading: boolean
  relayProviders: RelayProvider[]
  showDeleteConfirm: number | null
}>()

defineEmits<{
  (e: 'add'): void
  (e: 'edit', provider: RelayProvider): void
  (e: 'request-delete', id: number): void
  (e: 'confirm-delete', id: number): void
  (e: 'cancel-delete'): void
}>()
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
        @click="$emit('add')"
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
            <button :data-testid="`relay-provider-edit-${provider.id}`" class="font-medium text-indigo-600 hover:text-indigo-800" @click="$emit('edit', provider)">{{ t('settings.edit') }}</button>
            <button
              v-if="showDeleteConfirm !== provider.id"
              :data-testid="`relay-provider-delete-${provider.id}`"
              class="text-red-600 hover:text-red-800"
              @click="$emit('request-delete', provider.id)"
            >{{ t('settings.delete') }}</button>
            <template v-else>
              <button :data-testid="`relay-provider-confirm-delete-${provider.id}`" class="font-medium text-red-700" @click="$emit('confirm-delete', provider.id)">{{ t('settings.confirm') }}</button>
              <button :data-testid="`relay-provider-cancel-delete-${provider.id}`" class="text-gray-500" @click="$emit('cancel-delete')">{{ t('settings.cancel') }}</button>
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
              <button :data-testid="`relay-provider-edit-${provider.id}`" class="text-indigo-600 hover:text-indigo-800" @click="$emit('edit', provider)">{{ t('settings.edit') }}</button>
              <button
                v-if="showDeleteConfirm !== provider.id"
                :data-testid="`relay-provider-delete-${provider.id}`"
                class="text-red-600 hover:text-red-800"
                @click="$emit('request-delete', provider.id)"
              >{{ t('settings.delete') }}</button>
              <span v-else class="space-x-2">
                <button :data-testid="`relay-provider-confirm-delete-${provider.id}`" class="text-red-700 font-medium" @click="$emit('confirm-delete', provider.id)">{{ t('settings.confirm') }}</button>
                <button :data-testid="`relay-provider-cancel-delete-${provider.id}`" class="text-gray-500" @click="$emit('cancel-delete')">{{ t('settings.cancel') }}</button>
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
</template>
