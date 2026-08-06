<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { getAuthOptions } from '@/api/auth'
import AuthShell from '@/components/AuthShell.vue'
import { useI18n } from '@/i18n'
import { resolveSafeRedirect } from '@/router/authGuard'

const auth = useAuthStore()
const router = useRouter()
const { t } = useI18n()

const username = ref('')
const password = ref('')
const source = ref('SSO')
const error = ref('')
const loading = ref(false)
const authOptions = ref({
  ldap_enabled: false,
  dev_login_enabled: false,
})

onMounted(async () => {
  try {
    const res = await getAuthOptions()
    authOptions.value = {
      ldap_enabled: Boolean(res.data.data?.ldap_enabled),
      dev_login_enabled: Boolean(res.data.data?.dev_login_enabled),
    }
    source.value = authOptions.value.ldap_enabled ? 'LDAP' : 'SSO'
  } catch {
    authOptions.value = {
      ldap_enabled: false,
      dev_login_enabled: false,
    }
    source.value = 'SSO'
  }
})

async function handleLogin() {
  error.value = ''
  loading.value = true
  try {
    await auth.login({ username: username.value, password: password.value, source: source.value })
    router.push(resolveSafeRedirect(router.currentRoute.value.query.redirect))
  } catch (e: any) {
    error.value = e.response?.data?.message || t('auth.loginFailed')
  } finally {
    loading.value = false
  }
}

async function handleDevLogin() {
  error.value = ''
  loading.value = true
  try {
    const user = await auth.devLogin()
    if (user) {
      router.push(resolveSafeRedirect(router.currentRoute.value.query.redirect))
    }
  } catch (e: any) {
    error.value = e.response?.data?.message || t('auth.devLoginFailed')
  } finally {
    loading.value = false
  }
}
</script>

<template>
  <AuthShell title-key="app.fullTitle" subtitle-key="auth.signInSubtitle">
    <div class="space-y-6">
      <p v-if="router.currentRoute.value.query.redirect" class="rounded-md bg-blue-50 p-3 text-sm text-blue-800">
        {{ t('auth.redirectHelp') }}
      </p>

      <form class="space-y-4" @submit.prevent="handleLogin">
        <div>
          <label for="username" class="block text-sm font-medium text-gray-700">{{ t('auth.email') }}</label>
          <input
            id="username"
            v-model="username"
            type="text"
            required
            placeholder="you@example.com"
            autocomplete="username"
            class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          />
        </div>

        <div>
          <label for="password" class="block text-sm font-medium text-gray-700">{{ t('auth.password') }}</label>
          <input
            id="password"
            v-model="password"
            type="password"
            required
            autocomplete="current-password"
            class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          />
        </div>

        <div>
          <label for="source" class="block text-sm font-medium text-gray-700">{{ t('auth.source') }}</label>
          <select
            id="source"
            v-model="source"
            class="mt-1 block w-full rounded-md border border-gray-300 px-3 py-2 text-sm shadow-sm focus:border-indigo-500 focus:outline-none focus:ring-1 focus:ring-indigo-500"
          >
            <option v-if="authOptions.ldap_enabled" value="LDAP">LDAP</option>
            <option value="SSO">SSO</option>
          </select>
        </div>

        <div v-if="error" class="rounded-md bg-red-50 p-3 text-sm text-red-700">
          {{ error }}
        </div>

        <button
          type="submit"
          :disabled="loading"
          class="w-full rounded-md bg-indigo-600 px-4 py-2 text-sm font-medium text-white hover:bg-indigo-700 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 disabled:opacity-50"
        >
          {{ loading ? t('auth.signingIn') : t('auth.signIn') }}
        </button>
      </form>

      <template v-if="authOptions.dev_login_enabled">
        <div class="relative">
          <div class="absolute inset-0 flex items-center">
            <div class="w-full border-t border-gray-200"></div>
          </div>
          <div class="relative flex justify-center text-xs">
            <span class="bg-white px-2 text-gray-400">{{ t('auth.devMode') }}</span>
          </div>
        </div>

        <button
          :disabled="loading"
          class="w-full rounded-md border border-gray-300 bg-white px-4 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50 focus:outline-none focus:ring-2 focus:ring-indigo-500 focus:ring-offset-2 disabled:opacity-50"
          @click="handleDevLogin"
        >
          {{ t('auth.devLogin') }}
        </button>
      </template>
    </div>
  </AuthShell>
</template>
