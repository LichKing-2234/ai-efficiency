<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { ArrowLeft } from '@element-plus/icons-vue'
import AppLayout from '@/components/AppLayout.vue'
import AppPageHeader from '@/components/AppPageHeader.vue'
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
</script>

<template>
  <AppLayout>
    <ElSkeleton v-if="loading" :rows="8" animated />
    <div v-else-if="repo" class="space-y-5">
      <AppPageHeader :eyebrow="t('nav.adminSection')" :title="repo.name" :description="t('repoDetail.operationsDescription')">
        <template #before><ElButton class="mb-2 !ml-0 !p-0" type="primary" link :icon="ArrowLeft" @click="router.push('/repos')">{{ t('repoDetail.backToRepos') }}</ElButton></template>
        <template v-if="repo.clone_url" #after><p class="mt-0.5 break-all font-mono text-xs leading-5 text-slate-500">{{ repo.clone_url }}</p></template>
      </AppPageHeader>
      <RepositoryOperationsSection v-if="repoId" :repo-id="repoId" :repo="repo" @repo-updated="repo = $event" />
    </div>
  </AppLayout>
</template>
