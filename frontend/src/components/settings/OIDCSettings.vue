<script setup lang="ts">
import { computed, onMounted, ref } from 'vue'
import {
  getOIDCSettings,
  resetOIDCSettings,
  testOIDCSettings,
  updateOIDCSettings,
  type OIDCSettings as OIDCSettingsPayload,
  type OIDCSettingsUpdate,
} from '@/api/settings'
import { useI18n } from '@/i18n'

const { t } = useI18n()

const loading = ref(false)
const saving = ref(false)
const testing = ref(false)
const resetting = ref(false)
const error = ref('')
const success = ref('')
const source = ref<'environment' | 'database'>('environment')
const callbackPath = ref('/auth/oidc/callback')
const hasStoredSecret = ref(false)
const form = ref({
  enabled: false,
  label: '',
  issuer: '',
  client_id: '',
  client_secret: '',
  redirect_urls: '',
  allow_email_link: false,
  scopes: 'openid profile email',
})

const sourceLabel = computed(() => (source.value === 'database' ? t('settings.oidcSourceDatabase') : t('settings.oidcSourceEnvironment')))
const secretPlaceholder = computed(() => (hasStoredSecret.value ? t('settings.keepCurrentPlaceholder') : ''))

function defaultCallbackURL() {
  return `${window.location.origin}/auth/oidc/callback`
}

function applySettings(settings: OIDCSettingsPayload) {
  source.value = settings.source === 'database' ? 'database' : 'environment'
  callbackPath.value = settings.callback_path || '/auth/oidc/callback'
  hasStoredSecret.value = settings.client_secret === '***'
  const redirectURLs = settings.redirect_urls && settings.redirect_urls.length ? settings.redirect_urls : [defaultCallbackURL()]
  const scopes = settings.scopes && settings.scopes.length ? settings.scopes : ['openid', 'profile', 'email']
  form.value = {
    enabled: Boolean(settings.enabled),
    label: settings.label || '',
    issuer: settings.issuer || '',
    client_id: settings.client_id || '',
    client_secret: '',
    redirect_urls: redirectURLs.join('\n'),
    allow_email_link: Boolean(settings.allow_email_link),
    scopes: scopes.join(' '),
  }
}

function buildPayload(): OIDCSettingsUpdate {
  return {
    enabled: form.value.enabled,
    label: form.value.label.trim(),
    issuer: form.value.issuer.trim(),
    client_id: form.value.client_id.trim(),
    client_secret: form.value.client_secret.trim(),
    redirect_urls: form.value.redirect_urls.split(/\r?\n/).map((value) => value.trim()).filter(Boolean),
    allow_email_link: form.value.allow_email_link,
    scopes: form.value.scopes.split(/[\s,]+/).map((value) => value.trim()).filter(Boolean),
  }
}

function errorMessage(err: unknown, fallback: string) {
  const response = (err as { response?: { data?: { message?: string } } }).response
  return response?.data?.message || fallback
}

async function fetchSettings() {
  loading.value = true
  error.value = ''
  try {
    const { data } = await getOIDCSettings()
    if (data.data) applySettings(data.data)
  } catch (err) {
    error.value = errorMessage(err, t('settings.oidcLoadFailed'))
  } finally {
    loading.value = false
  }
}

async function handleSave() {
  saving.value = true
  error.value = ''
  success.value = ''
  try {
    const { data } = await updateOIDCSettings(buildPayload())
    if (data.data) applySettings(data.data)
    success.value = t('settings.oidcSaved')
    setTimeout(() => { success.value = '' }, 3000)
  } catch (err) {
    error.value = errorMessage(err, t('settings.oidcSaveFailed'))
  } finally {
    saving.value = false
  }
}

async function handleTest() {
  testing.value = true
  error.value = ''
  success.value = ''
  try {
    const { data } = await testOIDCSettings(buildPayload())
    const endpoint = data.data?.authorization_endpoint || data.data?.issuer || ''
    success.value = t('settings.oidcTestSuccess', { endpoint })
    setTimeout(() => { success.value = '' }, 5000)
  } catch (err) {
    error.value = errorMessage(err, t('settings.oidcSaveFailed'))
  } finally {
    testing.value = false
  }
}

async function handleReset() {
  resetting.value = true
  error.value = ''
  success.value = ''
  try {
    const { data } = await resetOIDCSettings()
    if (data.data) applySettings(data.data)
    success.value = t('settings.oidcResetDone')
    setTimeout(() => { success.value = '' }, 3000)
  } catch (err) {
    error.value = errorMessage(err, t('settings.oidcSaveFailed'))
  } finally {
    resetting.value = false
  }
}

onMounted(fetchSettings)
</script>

<template>
  <ElCard shadow="never" class="mt-4">
    <div class="space-y-4">
      <div class="flex items-start justify-between">
        <div>
          <h3 class="text-base font-semibold text-gray-900">{{ t('settings.oidcTitle') }}</h3>
          <p class="text-sm text-gray-500">{{ t('settings.oidcSubtitle') }}</p>
        </div>
        <ElTag :type="source === 'database' ? 'success' : 'info'" size="small">{{ sourceLabel }}</ElTag>
      </div>

      <div class="flex items-center">
        <ElSwitch v-model="form.enabled" id="oidc-enabled" />
        <label for="oidc-enabled" class="ml-2 text-sm text-gray-700">{{ t('settings.oidcEnabled') }}</label>
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700">{{ t('settings.oidcLabel') }}</label>
        <ElInput v-model="form.label" :placeholder="t('settings.oidcLabelPlaceholder')" class="mt-1" />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700">{{ t('settings.oidcIssuer') }}</label>
        <ElInput v-model="form.issuer" placeholder="https://authentik.example.com/application/o/app/" class="mt-1" />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700">{{ t('settings.oidcClientId') }}</label>
        <ElInput v-model="form.client_id" class="mt-1" />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700">{{ t('settings.oidcClientSecret') }}</label>
        <ElInput v-model="form.client_secret" type="password" :placeholder="secretPlaceholder" class="mt-1" />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700">{{ t('settings.oidcRedirectUrls') }}</label>
        <ElInput v-model="form.redirect_urls" type="textarea" :rows="2" class="mt-1" />
        <p class="text-xs text-gray-500">{{ t('settings.oidcRedirectUrlsHint') }}</p>
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700">{{ t('settings.oidcCallbackPath') }}</label>
        <ElInput :model-value="callbackPath" readonly class="mt-1" />
      </div>

      <div>
        <label class="block text-sm font-medium text-gray-700">{{ t('settings.oidcScopes') }}</label>
        <ElInput v-model="form.scopes" placeholder="openid profile email" class="mt-1" />
      </div>

      <div class="flex items-center">
        <ElSwitch v-model="form.allow_email_link" id="oidc-allow-email-link" />
        <label for="oidc-allow-email-link" class="ml-2 text-sm text-gray-700">{{ t('settings.oidcAllowEmailLink') }}</label>
      </div>

      <ElAlert v-if="error" type="error" :title="error" :closable="false" />
      <ElAlert v-if="success" type="success" :title="success" :closable="false" />

      <div class="flex justify-end space-x-3">
        <ElButton :loading="resetting" @click="handleReset">{{ t('settings.oidcReset') }}</ElButton>
        <ElButton :loading="testing" @click="handleTest">{{ t('settings.oidcTest') }}</ElButton>
        <ElButton type="primary" :loading="saving" @click="handleSave">{{ t('settings.save') }}</ElButton>
      </div>
    </div>
  </ElCard>
</template>
