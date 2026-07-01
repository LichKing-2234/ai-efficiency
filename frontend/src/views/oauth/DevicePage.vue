<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { verifyDeviceAuthorization } from '@/api/oauth'
import AuthShell from '@/components/AuthShell.vue'
import { useI18n } from '@/i18n'

const authStore = useAuthStore()
const route = useRoute()
const router = useRouter()

const userCode = ref('')
const loading = ref(false)
const error = ref('')
const result = ref('')
const { t } = useI18n()
const signedInAccount = computed(() => authStore.user?.email || authStore.user?.username || '')

onMounted(async () => {
  if (!authStore.isAuthenticated) {
    await router.replace({ path: '/login', query: { redirect: route.fullPath } })
  }
})

async function submit(approved: boolean) {
  loading.value = true
  error.value = ''
  result.value = ''
  try {
    const resp = await verifyDeviceAuthorization({
      user_code: userCode.value,
      approved,
    })
    result.value = resp.status === 'approved'
      ? t('auth.deviceApproved')
      : t('auth.deviceDenied')
  } catch (e: any) {
    error.value = e?.response?.data?.message || t('auth.deviceInvalid')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AuthShell title-key="auth.deviceTitle" subtitle-key="auth.deviceSubtitle" eyebrow-key="auth.deviceEyebrow">
    <div class="space-y-4">
      <div v-if="signedInAccount" class="rounded-md border border-blue-100 bg-blue-50 p-3">
        <div class="text-xs font-semibold uppercase tracking-wide text-blue-700">{{ t('auth.signedInAccount') }}</div>
        <div class="mt-1 break-all text-sm font-medium text-blue-950">{{ signedInAccount }}</div>
      </div>

      <label for="user-code" class="block text-sm font-medium text-gray-700">
        {{ t('auth.userCode') }}
      </label>
      <input
        id="user-code"
        v-model="userCode"
        type="text"
        class="w-full rounded border border-gray-300 px-3 py-2"
        :placeholder="t('auth.devicePlaceholder')"
      />

      <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
      <p v-if="result" class="rounded-md bg-emerald-50 p-3 text-sm text-emerald-700">{{ result }}</p>

      <div class="flex gap-3">
        <button
          data-action="deny"
          class="flex-1 rounded border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
          :disabled="loading"
          @click="submit(false)"
        >
          {{ loading ? t('auth.processing') : t('auth.deny') }}
        </button>
        <button
          data-action="approve"
          class="flex-1 rounded bg-blue-700 px-4 py-2 text-sm font-medium text-white hover:bg-blue-800"
          :disabled="loading"
          @click="submit(true)"
        >
          {{ loading ? t('auth.processing') : t('auth.authorize') }}
        </button>
      </div>
    </div>
  </AuthShell>
</template>
