<script setup lang="ts">
import { useI18n } from '@/i18n'
import type { SCMProvider } from '@/types'

const { locale, t } = useI18n()

defineProps<{
  providers: SCMProvider[]
  showDeleteConfirm: number | null
}>()

defineEmits<{
  (e: 'add'): void
  (e: 'edit', provider: SCMProvider): void
  (e: 'request-delete', id: number): void
  (e: 'confirm-delete', id: number): void
  (e: 'cancel-delete'): void
}>()

function formatDate(date: string) {
  return new Date(date).toLocaleDateString(locale.value)
}
</script>

<template>
  <div class="space-y-4">
    <div class="flex flex-col gap-3 sm:flex-row sm:items-start sm:justify-between">
      <div>
        <h2 class="text-xl font-bold text-gray-900">{{ t('settings.codePlatforms') }}</h2>
        <p class="mt-1 text-sm text-gray-500">{{ t('settings.codePlatformsHelp') }}</p>
      </div>
      <button
        class="rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700"
        @click="$emit('add')"
      >
        {{ t('settings.addProvider') }}
      </button>
    </div>

    <div class="rounded-lg bg-white shadow">
      <div v-if="providers.length > 0" class="space-y-3 p-4 md:hidden">
        <article v-for="p in providers" :key="p.id" class="rounded-lg border border-gray-100 p-4">
          <div class="flex items-start justify-between gap-3">
            <div class="min-w-0">
              <div class="truncate text-sm font-medium text-gray-900">{{ p.name }}</div>
              <div class="mt-1 break-all font-mono text-xs text-gray-500">{{ p.base_url }}</div>
              <div v-if="p.ssh_host" class="mt-1 break-all font-mono text-xs text-gray-500">{{ p.ssh_host }}</div>
            </div>
            <span
              class="shrink-0 rounded-full px-2 py-0.5 text-xs font-semibold"
              :class="p.type === 'github' ? 'bg-gray-100 text-gray-800' : 'bg-blue-100 text-blue-800'"
            >
              {{ p.type }}
            </span>
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
            <button :data-testid="`provider-edit-${p.id}`" class="font-medium text-indigo-600 hover:text-indigo-800" @click="$emit('edit', p)">{{ t('settings.edit') }}</button>
            <button
              v-if="showDeleteConfirm !== p.id"
              :data-testid="`provider-delete-${p.id}`"
              class="text-red-600 hover:text-red-800"
              @click="$emit('request-delete', p.id)"
            >{{ t('settings.delete') }}</button>
            <template v-else>
              <button :data-testid="`provider-confirm-delete-${p.id}`" class="font-medium text-red-700" @click="$emit('confirm-delete', p.id)">{{ t('settings.confirm') }}</button>
              <button :data-testid="`provider-cancel-delete-${p.id}`" class="text-gray-500" @click="$emit('cancel-delete')">{{ t('settings.cancel') }}</button>
            </template>
          </div>
        </article>
      </div>
      <div v-else class="px-6 py-12 text-center text-sm text-gray-500 md:hidden">
        {{ t('settings.noScmProviders') }}
      </div>

      <table class="hidden min-w-full divide-y divide-gray-200 md:table">
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
              <span
                class="inline-flex rounded-full px-2 text-xs font-semibold leading-5"
                :class="p.type === 'github' ? 'bg-gray-100 text-gray-800' : 'bg-blue-100 text-blue-800'"
              >
                {{ p.type }}
              </span>
            </td>
            <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500 font-mono text-xs">{{ p.base_url }}</td>
            <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500 font-mono text-xs">{{ p.ssh_host || '—' }}</td>
            <td class="whitespace-nowrap px-6 py-4">
              <span class="inline-flex rounded-full px-2 text-xs font-semibold leading-5 bg-green-100 text-green-800">
                {{ p.status }}
              </span>
            </td>
            <td class="whitespace-nowrap px-6 py-4 text-sm text-gray-500">{{ formatDate(p.created_at) }}</td>
            <td class="whitespace-nowrap px-6 py-4 text-right text-sm space-x-3">
              <button :data-testid="`provider-edit-${p.id}`" class="text-indigo-600 hover:text-indigo-800" @click="$emit('edit', p)">{{ t('settings.edit') }}</button>
              <button
                v-if="showDeleteConfirm !== p.id"
                :data-testid="`provider-delete-${p.id}`"
                class="text-red-600 hover:text-red-800"
                @click="$emit('request-delete', p.id)"
              >{{ t('settings.delete') }}</button>
              <span v-else class="space-x-2">
                <button :data-testid="`provider-confirm-delete-${p.id}`" class="text-red-700 font-medium" @click="$emit('confirm-delete', p.id)">{{ t('settings.confirm') }}</button>
                <button :data-testid="`provider-cancel-delete-${p.id}`" class="text-gray-500" @click="$emit('cancel-delete')">{{ t('settings.cancel') }}</button>
              </span>
            </td>
          </tr>
          <tr v-if="providers.length === 0">
            <td colspan="7" class="px-6 py-12 text-center text-sm text-gray-500">
              {{ t('settings.noScmProviders') }}
            </td>
          </tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
