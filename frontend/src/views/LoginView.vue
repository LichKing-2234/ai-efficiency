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
      <el-alert
        v-if="router.currentRoute.value.query.redirect"
        :closable="false"
        :title="t('auth.redirectHelp')"
        show-icon
        type="info"
      />

      <el-form class="space-y-4" label-position="top" @submit.prevent="handleLogin">
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
            show-password
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
