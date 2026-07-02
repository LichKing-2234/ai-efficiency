<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from '@/i18n'
import { useToast } from '@/composables/useToast'

const { t } = useI18n()
const { toast, dismissToast } = useToast()

const toastClass = computed(() => {
  if (toast.tone === 'error') return 'border-red-200 bg-red-50 text-red-700'
  if (toast.tone === 'success') return 'border-emerald-200 bg-emerald-50 text-emerald-700'
  return 'border-blue-200 bg-blue-50 text-blue-800'
})
</script>

<template>
  <div
    v-if="toast.visible"
    data-testid="app-toast"
    class="fixed left-1/2 top-4 z-40 w-[min(calc(100vw-2rem),24rem)] -translate-x-1/2 rounded-lg border px-4 py-3 text-sm shadow-lg"
    :class="toastClass"
    role="status"
    aria-live="polite"
  >
    <div class="flex items-start justify-between gap-3">
      <span>{{ toast.message }}</span>
      <button
        type="button"
        data-testid="app-toast-close"
        class="shrink-0 font-medium opacity-70 hover:opacity-100"
        :aria-label="t('app.close')"
        @click="dismissToast"
      >
        {{ t('app.close') }}
      </button>
    </div>
  </div>
</template>
