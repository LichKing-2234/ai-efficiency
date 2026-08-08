<script setup lang="ts">
import { ref, computed, onMounted } from 'vue'
import { useRoute } from 'vue-router'
import { useAuthStore } from '@/stores/auth'
import AuthShell from '@/components/AuthShell.vue'
import { approveAuthorization } from '@/api/oauth'
import { useI18n } from '@/i18n'
import { CircleCheck } from '@element-plus/icons-vue'

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
      <el-alert
        :closable="false"
        :description="t('auth.signInBeforeAuthorize')"
        show-icon
        :title="t('auth.signInToContinue')"
        type="warning"
      />
      <el-button tag="a" :href="loginUrl" class="w-full" type="primary">
        {{ t('auth.goToSignIn') }}
      </el-button>
    </div>

    <div v-else class="space-y-5">
      <el-alert :closable="false" show-icon type="info">
        <template #title>
          <span class="font-semibold">{{ clientId }}</span> {{ t('auth.requestsAccount') }}
        </template>
      </el-alert>

      <div class="rounded-md border border-slate-200 p-4">
        <h3 class="text-sm font-semibold text-slate-900">{{ t('auth.signedInAccount') }}</h3>
        <p class="mt-1 text-sm text-slate-600">{{ authStore.user?.email || authStore.user?.username }}</p>
      </div>

      <div>
        <h3 class="text-sm font-semibold text-slate-900">{{ t('auth.requestedAccess') }}</h3>
        <ul class="mt-2 space-y-2 text-sm text-slate-600">
          <li class="flex items-center gap-2">
            <el-icon class="text-emerald-600"><CircleCheck /></el-icon>
            {{ t('auth.readProfile') }}
          </li>
          <li class="flex items-center gap-2">
            <el-icon class="text-emerald-600"><CircleCheck /></el-icon>
            {{ t('auth.manageSessions') }}
          </li>
        </ul>
      </div>

      <el-alert v-if="error" :closable="false" :title="error" show-icon type="error" />

      <div class="flex gap-3">
        <el-button
          class="flex-1"
          data-action="deny"
          :disabled="loading"
          plain
          @click="approve(false)"
        >
          {{ t('auth.deny') }}
        </el-button>
        <el-button
          class="flex-1"
          data-action="approve"
          :loading="loading"
          type="primary"
          @click="approve(true)"
        >
          {{ loading ? t('auth.processing') : t('auth.authorize') }}
        </el-button>
      </div>
    </div>
  </AuthShell>
</template>
