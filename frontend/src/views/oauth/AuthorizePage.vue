<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AuthShell from '@/components/AuthShell.vue'
import { approveAuthorization } from '@/api/oauth'
import { useI18n } from '@/i18n'

const route = useRoute()
const authStore = useAuthStore()
const { t } = useI18n()

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
    const data = await approveAuthorization({
      client_id: clientId.value,
      redirect_uri: redirectUri.value,
      code_challenge: codeChallenge.value,
      code_challenge_method: codeChallengeMethod.value,
      state: state.value,
      approved,
    })
    if (data.redirect_uri) {
      window.location.href = data.redirect_uri
    } else {
      error.value = t('auth.authorizationUnexpectedResponse')
    }
  } catch (e: any) {
    error.value = e.message || t('auth.authorizationFailed')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AuthShell title-key="auth.authorizeTitle" subtitle-key="auth.authorizeSubtitle" eyebrow-key="auth.authorizeEyebrow">
    <div v-if="!authStore.isAuthenticated" class="space-y-4">
      <div class="rounded-md border border-amber-200 bg-amber-50 p-4">
        <h2 class="text-sm font-semibold text-amber-950">{{ t('auth.signInToContinue') }}</h2>
        <p class="mt-1 text-sm text-amber-800">{{ t('auth.signInBeforeAuthorize') }}</p>
      </div>
      <a :href="loginUrl" class="inline-flex w-full justify-center rounded-md bg-blue-700 px-4 py-2 text-sm font-medium text-white hover:bg-blue-800">
        {{ t('auth.goToSignIn') }}
      </a>
    </div>

    <div v-else class="space-y-5">
      <div class="rounded-lg border border-blue-200 bg-blue-50 p-4">
        <p class="text-sm text-blue-950">
          <span class="font-semibold">{{ clientId }}</span> {{ t('auth.requestsAccount') }}
        </p>
      </div>

      <div class="rounded-md border border-slate-200 p-4">
        <h3 class="text-sm font-semibold text-slate-900">{{ t('auth.signedInAccount') }}</h3>
        <p class="mt-1 text-sm text-slate-600">{{ authStore.user?.email || authStore.user?.username }}</p>
      </div>

      <div>
        <h3 class="text-sm font-semibold text-slate-900">{{ t('auth.requestedAccess') }}</h3>
        <ul class="mt-2 space-y-2 text-sm text-slate-600">
          <li class="flex items-center gap-2">
            <span class="h-2 w-2 rounded-full bg-emerald-500"></span>
            {{ t('auth.readProfile') }}
          </li>
          <li class="flex items-center gap-2">
            <span class="h-2 w-2 rounded-full bg-emerald-500"></span>
            {{ t('auth.manageSessions') }}
          </li>
        </ul>
      </div>

      <p v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>

      <div class="flex space-x-3">
        <button
          class="flex-1 rounded-md border border-gray-300 px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 disabled:opacity-50"
          :disabled="loading"
          @click="approve(false)"
        >
          {{ t('auth.deny') }}
        </button>
        <button
          class="flex-1 rounded-md bg-blue-700 px-4 py-2 text-sm font-medium text-white hover:bg-blue-800 disabled:opacity-50"
          :disabled="loading"
          @click="approve(true)"
        >
          {{ loading ? t('auth.processing') : t('auth.authorize') }}
        </button>
      </div>
    </div>
  </AuthShell>
</template>
