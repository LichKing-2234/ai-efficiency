<script setup lang="ts">
import { onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import { getAuthOptions } from '@/api/auth'
import AuthShell from '@/components/AuthShell.vue'
import { useI18n } from '@/i18n'
import { resolveSafeRedirect } from '@/router/authGuard'
import { hciLoginSuppressed } from '@/auth/browserSession'
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
const signedOut = ref(hciLoginSuppressed())
let active = true
onUnmounted(() => { active = false })
const authOptions = ref({
  hci_enabled: false,
  ldap_enabled: false,
  dev_login_enabled: false,
})

async function loadAuthOptions() {
  optionsLoaded.value = false
  optionsError.value = false
  error.value = ''
  try {
    const res = await getAuthOptions()
    authOptions.value = {
      hci_enabled: Boolean(res.data.data?.hci_enabled),
      ldap_enabled: Boolean(res.data.data?.ldap_enabled),
      dev_login_enabled: Boolean(res.data.data?.dev_login_enabled),
    }
    source.value = authOptions.value.ldap_enabled ? 'LDAP' : 'SSO'
    if (authOptions.value.hci_enabled) {
      if (hciLoginSuppressed()) {
        signedOut.value = true
      } else {
        await handleHCI()
      }
    }
  } catch {
    optionsError.value = true
    error.value = t('auth.hciFailed')
  } finally {
    optionsLoaded.value = true
  }
}

onMounted(() => { void loadAuthOptions() })

async function handleHCI() {
  if (loading.value) return
  loading.value = true
  error.value = ''
  const redirect = resolveSafeRedirect(router.currentRoute.value.query.redirect)
  try {
    const user = await auth.hciBootstrap()
    if (active && user) await router.replace(redirect)
    else if (active) error.value = t('auth.hciFailed')
  } catch {
    if (active) error.value = t('auth.hciFailed')
  } finally {
    if (active) loading.value = false
  }
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
        <el-alert :closable="false" :title="error" type="error" />
        <el-button :loading="loading" @click="loadAuthOptions">
          {{ t('auth.hciRetry') }}
        </el-button>
      </div>
      <div v-else-if="authOptions.hci_enabled" data-testid="hci-login" class="space-y-4">
        <p v-if="loading">{{ t('auth.hciConnecting') }}</p>
        <p v-if="signedOut && !loading">{{ t('auth.hciSignedOut') }}</p>
        <el-alert v-if="error" :closable="false" :title="error" type="error" />
        <el-button v-if="signedOut || error" :loading="loading" @click="handleHCI">
          {{ t(signedOut ? 'auth.hciSignIn' : 'auth.hciRetry') }}
        </el-button>
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

      <template v-if="!authOptions.hci_enabled && authOptions.dev_login_enabled">
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
