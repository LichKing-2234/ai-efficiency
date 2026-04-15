<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { verifyDeviceAuthorization } from '@/api/oauth'

const authStore = useAuthStore()
const route = useRoute()
const router = useRouter()

const userCode = ref('')
const loading = ref(false)
const error = ref('')
const result = ref('')

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
      ? 'Approved. You can return to the terminal.'
      : 'Access denied.'
  } catch (e: any) {
    error.value = e?.response?.data?.message || 'Code invalid or expired'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-50">
    <div class="w-full max-w-md rounded-lg bg-white p-8 shadow">
      <h1 class="mb-4 text-2xl font-bold text-gray-900">Device Login</h1>
      <p class="mb-4 text-sm text-gray-600">Enter the code shown by <code>ae-cli login --device</code>.</p>

      <label for="user-code" class="mb-2 block text-sm font-medium text-gray-700">
        User code
      </label>
      <input
        id="user-code"
        v-model="userCode"
        type="text"
        class="mb-4 w-full rounded border border-gray-300 px-3 py-2"
        placeholder="ABCD-EFGH"
      />

      <p v-if="error" class="mb-4 text-sm text-red-600">{{ error }}</p>
      <p v-if="result" class="mb-4 text-sm text-green-700">{{ result }}</p>

      <div class="flex gap-3">
        <button
          data-action="deny"
          class="flex-1 rounded border border-gray-300 px-4 py-2"
          :disabled="loading"
          @click="submit(false)"
        >
          {{ loading ? 'Working...' : 'Deny' }}
        </button>
        <button
          data-action="approve"
          class="flex-1 rounded bg-blue-600 px-4 py-2 text-white"
          :disabled="loading"
          @click="submit(true)"
        >
          {{ loading ? 'Working...' : 'Authorize' }}
        </button>
      </div>
    </div>
  </div>
</template>
