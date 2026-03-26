<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'

const route = useRoute()
const authStore = useAuthStore()

const clientId = ref('')
const redirectUri = ref('')
const codeChallenge = ref('')
const codeChallengeMethod = ref('')
const state = ref('')
const loading = ref(false)
const error = ref('')

const loginUrl = computed(() => {
  const currentPath = route.fullPath
  return `/login?redirect=${encodeURIComponent(currentPath)}`
})

onMounted(() => {
  clientId.value = (route.query.client_id as string) || ''
  redirectUri.value = (route.query.redirect_uri as string) || ''
  codeChallenge.value = (route.query.code_challenge as string) || ''
  codeChallengeMethod.value = (route.query.code_challenge_method as string) || ''
  state.value = (route.query.state as string) || ''
})

async function approve(approved: boolean) {
  loading.value = true
  error.value = ''
  try {
    const token = localStorage.getItem('token')
    const resp = await fetch('/oauth/authorize/approve', {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'Authorization': `Bearer ${token}`,
      },
      body: JSON.stringify({
        client_id: clientId.value,
        redirect_uri: redirectUri.value,
        code_challenge: codeChallenge.value,
        code_challenge_method: codeChallengeMethod.value,
        state: state.value,
        approved,
      }),
    })
    const data = await resp.json()
    if (data.data?.redirect_uri) {
      window.location.href = data.data.redirect_uri
    } else if (data.redirect_uri) {
      window.location.href = data.redirect_uri
    } else {
      error.value = 'Unexpected response from server'
    }
  } catch (e: any) {
    error.value = e.message || 'Authorization failed'
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <div class="min-h-screen flex items-center justify-center bg-gray-50">
    <div class="max-w-md w-full bg-white rounded-lg shadow-md p-8">
      <h1 class="text-2xl font-bold text-center mb-6">授权请求</h1>

      <div v-if="!authStore.isAuthenticated" class="text-center">
        <p class="text-gray-600 mb-4">请先登录后再授权</p>
        <a :href="loginUrl" class="text-blue-600 hover:underline">前往登录</a>
      </div>

      <div v-else>
        <div class="bg-blue-50 border border-blue-200 rounded-lg p-4 mb-6">
          <p class="text-sm text-gray-700">
            <span class="font-semibold">{{ clientId }}</span> 请求访问你的账号
          </p>
        </div>

        <div class="mb-6">
          <h3 class="text-sm font-medium text-gray-700 mb-2">将授予以下权限：</h3>
          <ul class="text-sm text-gray-600 space-y-1">
            <li class="flex items-center">
              <span class="w-2 h-2 bg-green-400 rounded-full mr-2"></span>
              读取你的用户信息
            </li>
            <li class="flex items-center">
              <span class="w-2 h-2 bg-green-400 rounded-full mr-2"></span>
              管理 AI 工具会话
            </li>
          </ul>
        </div>

        <p v-if="error" class="text-red-500 text-sm mb-4">{{ error }}</p>

        <div class="flex space-x-4">
          <button
            @click="approve(false)"
            :disabled="loading"
            class="flex-1 px-4 py-2 border border-gray-300 rounded-md text-gray-700 hover:bg-gray-50 disabled:opacity-50"
          >
            拒绝
          </button>
          <button
            @click="approve(true)"
            :disabled="loading"
            class="flex-1 px-4 py-2 bg-blue-600 text-white rounded-md hover:bg-blue-700 disabled:opacity-50"
          >
            {{ loading ? '处理中...' : '授权' }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
