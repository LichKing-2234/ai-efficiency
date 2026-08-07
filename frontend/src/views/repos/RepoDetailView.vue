<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import AppLayout from '@/components/AppLayout.vue'
import RepositoryActivitySection from '@/components/activity/RepositoryActivitySection.vue'
import RepositoryOperationsSection from '@/components/repos/RepositoryOperationsSection.vue'
import { getRepo } from '@/api/repo'
import { useI18n } from '@/i18n'
import type { RepoConfig } from '@/types'

const route = useRoute()
const router = useRouter()
const { t } = useI18n()
const repo = ref<RepoConfig | null>(null)
const loading = ref(true)
const repoId = computed(() => {
  const value = Number(route.params.id)
  return Number.isInteger(value) && value > 0 ? value : null
})
const activeSection = computed<'activity' | 'operations'>(() => route.query.tab === 'operations' ? 'operations' : 'activity')
let requestSequence = 0

async function loadRepository() {
  const sequence = ++requestSequence
  const id = repoId.value
  repo.value = null
  loading.value = true
  if (id == null) {
    loading.value = false
    void router.push('/repos')
    return
  }
  try {
    const response = await getRepo(id)
    if (sequence !== requestSequence) return
    repo.value = response.data.data ?? null
    if (!repo.value) throw new Error('Repository response is empty')
  } catch {
    if (sequence !== requestSequence) return
    void router.push('/repos')
  } finally {
    if (sequence === requestSequence) loading.value = false
  }
}

watch(repoId, () => void loadRepository(), { immediate: true })

function selectSection(section: 'activity' | 'operations') {
  const query = { ...route.query }
  if (section === 'operations') query.tab = 'operations'
  else delete query.tab
  void router.replace({ query })
}

function updateRepo(refreshed: RepoConfig) {
  repo.value = refreshed
}
</script>

<template>
  <AppLayout>
    <div v-if="loading" class="py-12 text-center text-gray-500">{{ t('repoDetail.loading') }}</div>

    <div v-else-if="repo" class="space-y-5">
      <header>
        <button class="text-sm text-indigo-600 hover:text-indigo-800" @click="router.push('/repos')">
          &larr; {{ t('repoDetail.backToRepos') }}
        </button>
        <div class="mt-2">
          <p class="text-xs font-semibold uppercase tracking-wide text-blue-700">{{ t('nav.codeSection') }}</p>
          <h1 class="text-2xl font-bold text-gray-900">{{ repo.name }}</h1>
          <p class="text-sm text-gray-500">{{ repo.full_name }}</p>
          <p v-if="repo.clone_url" class="mt-0.5 break-all font-mono text-xs text-gray-400">{{ repo.clone_url }}</p>
        </div>
      </header>

      <div class="flex border-b border-slate-200" role="tablist" :aria-label="t('activity.repositorySections')">
        <button type="button" role="tab" data-testid="repo-tab-activity" :aria-selected="activeSection === 'activity'" :class="['border-b-2 px-4 py-2 text-sm font-medium', activeSection === 'activity' ? 'border-cyan-700 text-cyan-800' : 'border-transparent text-slate-600']" @click="selectSection('activity')">{{ t('activity.activityTab') }}</button>
        <button type="button" role="tab" data-testid="repo-tab-operations" :aria-selected="activeSection === 'operations'" :class="['border-b-2 px-4 py-2 text-sm font-medium', activeSection === 'operations' ? 'border-cyan-700 text-cyan-800' : 'border-transparent text-slate-600']" @click="selectSection('operations')">{{ t('activity.operationsTab') }}</button>
      </div>

      <RepositoryActivitySection v-if="activeSection === 'activity' && repoId" :key="`activity:${repoId}`" :repo-id="repoId" />
      <RepositoryOperationsSection v-else-if="repoId" :key="`operations:${repoId}`" :repo-id="repoId" :repo="repo" @repo-updated="updateRepo" />
    </div>
  </AppLayout>
</template>
