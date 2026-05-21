<script setup lang="ts">
import { computed, onMounted, reactive, ref } from 'vue'
import AppLayout from '@/components/AppLayout.vue'
import { createManagedKey, getUserProviders, regenerateManagedKey } from '@/api/user'
import { useAuthStore } from '@/stores/auth'
import type {
  UserProviderSummary,
  VerifyReviewSummary,
} from '@/types'
import {
  buildDeviceLoginCommand,
  buildDiscoverCommand,
  buildInstallCommand,
  buildLoginCommand,
  reviewVerifyOutput,
} from '@/utils/userSetupReview'

const auth = useAuthStore()

const loading = ref(true)
const error = ref('')
const providers = ref<UserProviderSummary[]>([])
const selectedProviderId = ref<number | null>(null)
const selectedMessage = ref('')
const sessionSecrets = reactive<Record<number, string>>({})
const revealedProviderIds = reactive<Record<number, boolean>>({})
const verifyDrafts = reactive<Record<number, { version: string; discover: string; doctor: string }>>({})
const reviewResults = reactive<Record<number, VerifyReviewSummary | null>>({})

const currentOrigin = computed(() => window.location.origin)
const selectedProvider = computed(() => providers.value.find((provider) => provider.id === selectedProviderId.value) ?? null)
const selectedSecret = computed(() => (selectedProvider.value ? sessionSecrets[selectedProvider.value.id] ?? '' : ''))
const canReveal = computed(() => !!selectedProvider.value && !!sessionSecrets[selectedProvider.value.id])
const isSecretRevealed = computed(() => !!selectedProvider.value && !!revealedProviderIds[selectedProvider.value.id])
const displayedSecret = computed(() => (isSecretRevealed.value ? selectedSecret.value : ''))
const currentReview = computed(() => (selectedProvider.value ? reviewResults[selectedProvider.value.id] ?? null : null))
const installCommand = computed(() => buildInstallCommand(currentOrigin.value))
const loginCommand = computed(() => buildLoginCommand(currentOrigin.value))
const deviceLoginCommand = computed(() => buildDeviceLoginCommand(currentOrigin.value))
const discoverCommand = computed(() => selectedProvider.value ? buildDiscoverCommand(currentOrigin.value, selectedProvider.value.name) : '')

function ensureVerifyDraft(providerId: number) {
  if (!verifyDrafts[providerId]) {
    verifyDrafts[providerId] = { version: '', discover: '', doctor: '' }
  }
  return verifyDrafts[providerId]
}

function selectDefaultProvider(rows: UserProviderSummary[]) {
  const primary = rows.find((provider) => provider.is_primary)
  selectedProviderId.value = primary?.id ?? rows[0]?.id ?? null
  if (selectedProviderId.value != null) {
    ensureVerifyDraft(selectedProviderId.value)
  }
}

function selectProvider(providerId: number) {
  selectedProviderId.value = providerId
  ensureVerifyDraft(providerId)
}

async function loadProviders() {
  loading.value = true
  error.value = ''
  try {
    const res = await getUserProviders()
    const data = res.data.data
    providers.value = data?.providers ?? []
    selectedMessage.value = data?.message ?? ''
    selectDefaultProvider(providers.value)
  } catch (err: any) {
    error.value = err.response?.data?.message || 'Failed to load user setup data.'
    providers.value = []
    selectedProviderId.value = null
  } finally {
    loading.value = false
  }
}

async function handleCreateKey() {
  if (!selectedProvider.value) return
  const providerId = selectedProvider.value.id
  const res = await createManagedKey(providerId)
  const data = res.data.data
  if (!data) return
  sessionSecrets[providerId] = data.secret
  revealedProviderIds[providerId] = false
  providers.value = providers.value.map((provider) =>
    provider.id === providerId
      ? {
          ...provider,
          managed_key: {
            state: 'existing_hidden',
            api_key_id: data.api_key_id,
            name: data.name,
            status: data.status,
          },
        }
      : provider
  )
}

async function handleRegenerateKey() {
  if (!selectedProvider.value) return
  const confirmed = window.confirm('Regenerate this managed key? The old key will be revoked and you will need to rerun discover on machines still using it.')
  if (!confirmed) return
  const providerId = selectedProvider.value.id
  const res = await regenerateManagedKey(providerId)
  const data = res.data.data
  if (!data) return
  sessionSecrets[providerId] = data.secret
  revealedProviderIds[providerId] = true
  providers.value = providers.value.map((provider) =>
    provider.id === providerId
      ? {
          ...provider,
          managed_key: {
            state: 'existing_hidden',
            api_key_id: data.api_key_id,
            name: data.name,
            status: data.status,
          },
        }
      : provider
  )
}

function handleRevealKey() {
  if (!selectedProvider.value) return
  revealedProviderIds[selectedProvider.value.id] = true
}

async function handleCopyKey() {
  if (!selectedSecret.value) return
  await navigator.clipboard.writeText(selectedSecret.value)
}

function handleReviewVerify() {
  if (!selectedProvider.value) return
  const draft = ensureVerifyDraft(selectedProvider.value.id)
  reviewResults[selectedProvider.value.id] = reviewVerifyOutput({
    selectedProviderName: selectedProvider.value.name,
    versionOutput: draft.version,
    discoverOutput: draft.discover,
    doctorOutput: draft.doctor,
  })
}

function reviewClass(status: string) {
  switch (status) {
    case 'looks_good':
      return 'bg-green-50 text-green-700'
    case 'needs_attention':
      return 'bg-amber-50 text-amber-800'
    default:
      return 'bg-gray-100 text-gray-700'
  }
}

onMounted(loadProviders)
</script>

<template>
  <AppLayout>
    <div class="space-y-6">
      <div class="flex items-start justify-between">
        <div>
          <h1 class="text-2xl font-bold text-gray-900">User</h1>
          <p class="mt-1 text-sm text-gray-500">Profile, CLI setup, and managed API key self-serve for regular developers.</p>
        </div>
        <button
          class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
          @click="loadProviders"
        >
          Refresh
        </button>
      </div>

      <div class="grid gap-6 lg:grid-cols-[320px_minmax(0,1fr)]">
        <div class="space-y-6">
          <section class="rounded-lg bg-white p-5 shadow">
            <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">Profile Summary</h2>
            <dl class="mt-4 space-y-3 text-sm">
              <div class="flex justify-between gap-4"><dt class="text-gray-500">Username</dt><dd class="font-medium text-gray-900">{{ auth.user?.username ?? '—' }}</dd></div>
              <div class="flex justify-between gap-4"><dt class="text-gray-500">Email</dt><dd class="font-medium text-gray-900">{{ auth.user?.email ?? '—' }}</dd></div>
              <div class="flex justify-between gap-4"><dt class="text-gray-500">Role</dt><dd class="font-medium text-gray-900">{{ auth.user?.role ?? '—' }}</dd></div>
              <div class="flex justify-between gap-4"><dt class="text-gray-500">Auth Source</dt><dd class="font-medium text-gray-900">{{ auth.user?.auth_source ?? '—' }}</dd></div>
            </dl>
          </section>

          <section class="rounded-lg bg-white p-5 shadow">
            <div class="flex items-center justify-between">
              <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">Providers</h2>
              <span v-if="loading" class="text-xs text-gray-400">Loading...</span>
            </div>
            <p v-if="selectedMessage" class="mt-3 rounded-md bg-gray-50 p-3 text-sm text-gray-600">{{ selectedMessage }}</p>
            <p v-if="error" class="mt-3 rounded-md bg-red-50 p-3 text-sm text-red-700">{{ error }}</p>
            <div class="mt-4 space-y-3">
              <button
                v-for="provider in providers"
                :key="provider.id"
                :data-testid="`provider-${provider.id}`"
                class="w-full rounded-lg border px-4 py-3 text-left transition"
                :class="provider.id === selectedProviderId ? 'border-gray-900 bg-gray-900 text-white' : 'border-gray-200 bg-white text-gray-900 hover:border-gray-400'"
                @click="selectProvider(provider.id)"
              >
                <div class="flex items-center justify-between gap-3">
                  <div>
                    <div class="font-medium">{{ provider.display_name }}</div>
                    <div class="text-xs" :class="provider.id === selectedProviderId ? 'text-gray-300' : 'text-gray-500'">{{ provider.name }}</div>
                  </div>
                  <span
                    v-if="provider.is_primary"
                    class="rounded-full px-2 py-1 text-[11px] font-semibold"
                    :class="provider.id === selectedProviderId ? 'bg-white/15 text-white' : 'bg-gray-100 text-gray-700'"
                  >
                    Primary
                  </span>
                </div>
              </button>
            </div>
          </section>
        </div>

        <div class="space-y-6">
          <section class="rounded-lg bg-white p-5 shadow">
            <div class="flex items-start justify-between gap-4">
              <div>
                <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">Provider &amp; Managed Key</h2>
                <p v-if="selectedProvider" class="mt-1 text-sm text-gray-500">{{ selectedProvider.base_url }}</p>
              </div>
              <div v-if="selectedProvider" class="text-right text-xs text-gray-500">
                <div>Model</div>
                <div class="mt-1 font-medium text-gray-900">{{ selectedProvider.default_model }}</div>
              </div>
            </div>

            <div v-if="selectedProvider" class="mt-4 space-y-4">
              <div class="rounded-md bg-gray-50 p-4 text-sm text-gray-700">
                <div class="font-medium text-gray-900">Managed key state: {{ selectedProvider.managed_key.state }}</div>
                <div v-if="selectedProvider.managed_key.state === 'existing_hidden'" class="mt-2">
                  This provider already has a managed key, but the old secret cannot be read back. Use Regenerate if you need a new revealable secret.
                </div>
                <div v-else class="mt-2">
                  No managed key exists for this provider yet.
                </div>
              </div>

              <div class="rounded-md border border-dashed border-gray-300 p-4">
                <div class="text-xs uppercase tracking-wide text-gray-400">Current Secret</div>
                <div class="mt-2 break-all rounded-md bg-gray-950 px-3 py-2 font-mono text-sm text-green-300">
                  {{ displayedSecret || '••••••••••••••••' }}
                </div>
                <div class="mt-3 flex flex-wrap gap-2">
                  <button
                    v-if="selectedProvider.managed_key.state === 'missing'"
                    data-testid="create-key"
                    class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-black"
                    @click="handleCreateKey"
                  >
                    Create Key
                  </button>
                  <button
                    v-if="selectedProvider.managed_key.state === 'existing_hidden'"
                    data-testid="regenerate-key"
                    class="rounded-md border border-amber-300 bg-amber-50 px-3 py-2 text-sm font-medium text-amber-900 hover:bg-amber-100"
                    @click="handleRegenerateKey"
                  >
                    Regenerate
                  </button>
                  <button
                    v-if="canReveal"
                    data-testid="reveal-key"
                    class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
                    @click="handleRevealKey"
                  >
                    Reveal
                  </button>
                  <button
                    v-if="canReveal"
                    data-testid="copy-key"
                    class="rounded-md border border-gray-300 px-3 py-2 text-sm font-medium text-gray-700 hover:bg-gray-50"
                    @click="handleCopyKey"
                  >
                    Copy
                  </button>
                </div>
              </div>
            </div>
          </section>

          <section class="rounded-lg bg-white p-5 shadow">
            <h2 class="text-sm font-semibold uppercase tracking-wide text-gray-900">CLI Setup Checklist</h2>

            <div class="mt-4 space-y-4 text-sm">
              <div class="rounded-md border border-gray-200 p-4">
                <div class="font-medium text-gray-900">1. Install</div>
                <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ installCommand }}</pre>
              </div>

              <div class="rounded-md border border-gray-200 p-4">
                <div class="font-medium text-gray-900">2. Login</div>
                <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ loginCommand }}</pre>
                <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ deviceLoginCommand }}</pre>
              </div>

              <div class="rounded-md border border-gray-200 p-4">
                <div class="font-medium text-gray-900">3. Discover</div>
                <pre class="mt-2 overflow-x-auto rounded-md bg-gray-950 px-3 py-2 text-xs text-green-300">{{ discoverCommand || 'Select a provider to build the discover command.' }}</pre>
              </div>

              <div v-if="selectedProvider" class="rounded-md border border-gray-200 p-4">
                <div class="font-medium text-gray-900">4. Verify</div>
                <div class="mt-3 space-y-3">
                  <textarea
                    v-model="ensureVerifyDraft(selectedProvider.id).version"
                    rows="3"
                    class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                    placeholder="Paste ae-cli version output"
                  />
                  <textarea
                    v-model="ensureVerifyDraft(selectedProvider.id).discover"
                    rows="4"
                    class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                    placeholder="Paste ae-cli discover --dry-run output"
                  />
                  <textarea
                    v-model="ensureVerifyDraft(selectedProvider.id).doctor"
                    rows="4"
                    class="w-full rounded-md border border-gray-300 px-3 py-2 text-sm"
                    placeholder="Paste ae-cli doctor output"
                  />
                  <button
                    class="rounded-md bg-gray-900 px-3 py-2 text-sm font-medium text-white hover:bg-black"
                    @click="handleReviewVerify"
                  >
                    Review
                  </button>
                </div>

                <div v-if="currentReview" class="mt-4 grid gap-3 md:grid-cols-3">
                  <div :class="['rounded-md p-3 text-sm', reviewClass(currentReview.version.status)]">
                    <div class="font-medium">Version</div>
                    <div class="mt-1">{{ currentReview.version.message }}</div>
                  </div>
                  <div :class="['rounded-md p-3 text-sm', reviewClass(currentReview.discover.status)]">
                    <div class="font-medium">Discover</div>
                    <div class="mt-1">{{ currentReview.discover.message }}</div>
                  </div>
                  <div :class="['rounded-md p-3 text-sm', reviewClass(currentReview.doctor.status)]">
                    <div class="font-medium">Doctor</div>
                    <div class="mt-1">{{ currentReview.doctor.message }}</div>
                  </div>
                </div>
              </div>
            </div>
          </section>
        </div>
      </div>
    </div>
  </AppLayout>
</template>
