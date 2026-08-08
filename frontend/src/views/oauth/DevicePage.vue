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

      <el-form label-position="top" @submit.prevent="submit(true)">
        <el-form-item :label="t('auth.userCode')" label-for="user-code">
          <el-input
            id="user-code"
            v-model="userCode"
            data-testid="device-code"
            :placeholder="t('auth.devicePlaceholder')"
          />
        </el-form-item>
      </el-form>

      <el-alert v-if="error" :closable="false" :title="error" show-icon type="error" />
      <el-alert v-if="result" :closable="false" :title="result" show-icon type="success" />

      <div class="flex gap-3">
        <el-button
          data-action="deny"
          class="flex-1"
          :disabled="loading"
          plain
          @click="submit(false)"
        >
          {{ loading ? t('auth.processing') : t('auth.deny') }}
        </el-button>
        <el-button
          data-action="approve"
          class="flex-1"
          :loading="loading"
          type="primary"
          @click="submit(true)"
        >
          {{ loading ? t('auth.processing') : t('auth.authorize') }}
        </el-button>
      </div>
    </div>
  </AuthShell>
</template>
