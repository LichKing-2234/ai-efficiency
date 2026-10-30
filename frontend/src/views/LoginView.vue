<script setup lang="ts">
import { onMounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { getAuthOptions } from '@/api/auth'
import AuthShell from '@/components/AuthShell.vue'
import { useI18n } from '@/i18n'
import { resolveSafeRedirect } from '@/router/authGuard'
import { Lock, User } from '@element-plus/icons-vue'

const auth = useAuthStore()
const router = useRouter()
const { t } = useI18n()

const username = ref('')
const password = ref('')
const source = ref('SSO')
const error = ref('')
const loading = ref(false)
const optionsLoaded = ref(false)
const optionsError = ref(false)
const authOptions = ref({
  ldap_enabled: false,
  dev_login_enabled: false,
  oidc_enabled: false,
})

const oidcSignedOut = ref(localStorage.getItem('oidc_logout_requested') === '1')

onMounted(async () => {
  try {
    const res = await getAuthOptions()
    authOptions.value = {
      ldap_enabled: Boolean(res.data.data?.ldap_enabled),
      dev_login_enabled: Boolean(res.data.data?.dev_login_enabled),
      oidc_enabled: Boolean(res.data.data?.oidc_enabled),
    }
    source.value = authOptions.value.ldap_enabled ? 'LDAP' : 'SSO'
    if (authOptions.value.oidc_enabled) {
      if (router.currentRoute.value.query.oidc === 'complete') {
        loading.value = true
        const user = await auth.completeOIDC()
        if (user) {
          localStorage.removeItem('oidc_logout_requested')
          await router.replace(resolveSafeRedirect(router.currentRoute.value.query.redirect))
        }
      } else if (!oidcSignedOut.value) {
        startOIDC()
      }
    }
  } catch {
    authOptions.value = {
      ldap_enabled: false,
      dev_login_enabled: false,
      oidc_enabled: false,
    }
    source.value = 'SSO'
    optionsError.value = true
  } finally {
    optionsLoaded.value = true
    loading.value = false
  }
})

function startOIDC() {
  localStorage.removeItem('oidc_logout_requested')
  oidcSignedOut.value = false
  loading.value = true
  const redirect = resolveSafeRedirect(router.currentRoute.value.query.redirect)
  window.location.assign(`/auth/oidc/start?redirect=${encodeURIComponent(redirect)}`)
}

function logoutOIDC() {
  localStorage.setItem('oidc_logout_requested', '1')
  oidcSignedOut.value = true
}

function reloadLogin() {
  window.location.reload()
}

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
      <el-alert
        v-if="router.currentRoute.value.query.redirect"
        :closable="false"
        :title="t('auth.redirectHelp')"
        show-icon
        type="info"
      />

      <div v-if="optionsError" data-testid="auth-options-error" class="space-y-4">
        <el-alert :closable="false" :title="t('auth.loginFailed')" type="error" />
        <el-button @click="reloadLogin">{{ t('auth.signIn') }}</el-button>
      </div>
      <div v-else-if="authOptions.oidc_enabled" data-testid="oidc-login" class="space-y-4">
        <p v-if="loading">{{ t('auth.oidcConnecting') }}</p>
        <p v-if="oidcSignedOut && !loading">{{ t('auth.oidcSignedOut') }}</p>
        <el-button v-if="oidcSignedOut || !loading" :loading="loading" type="primary" @click="startOIDC">
          {{ oidcSignedOut ? t('auth.oidcSignIn') : t('auth.signIn') }}
        </el-button>
        <el-button v-if="!loading && !oidcSignedOut" plain @click="logoutOIDC">{{ t('auth.signOut') }}</el-button>
      </div>
      <el-form v-else-if="optionsLoaded" class="space-y-4" label-position="top" @submit.prevent="handleLogin">
        <el-form-item :label="t('auth.email')" label-for="username">
          <el-input
            id="username"
            v-model="username"
            autocomplete="username"
            data-testid="username-field"
            placeholder="you@example.com"
            required
            :prefix-icon="User"
          />
        </el-form-item>

        <el-form-item :label="t('auth.password')" label-for="password">
          <el-input
            id="password"
            v-model="password"
            autocomplete="current-password"
            data-testid="password-field"
            required
            type="password"
            :prefix-icon="Lock"
          />
        </el-form-item>

        <el-form-item :label="t('auth.source')" label-for="source">
          <el-select
            id="source"
            v-model="source"
            data-testid="auth-source"
            class="w-full"
          >
            <el-option v-if="authOptions.ldap_enabled" label="LDAP" value="LDAP" />
            <el-option label="SSO" value="SSO" />
          </el-select>
        </el-form-item>

        <el-alert v-if="error" :closable="false" :title="error" show-icon type="error" />

        <el-button
          class="w-full"
          :loading="loading"
          native-type="submit"
          type="primary"
        >
          {{ loading ? t('auth.signingIn') : t('auth.signIn') }}
        </el-button>
      </el-form>

      <template v-if="authOptions.dev_login_enabled">
        <el-divider>{{ t('auth.devMode') }}</el-divider>

        <el-button
          class="w-full"
          :loading="loading"
          plain
          @click="handleDevLogin"
        >
          {{ t('auth.devLogin') }}
        </el-button>
      </template>
    </div>
  </AuthShell>
</template>
