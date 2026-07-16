import { ref } from 'vue'
import { defineStore } from 'pinia'
import { listCredentials } from '@/api/credential'
import { listDirectorySources } from '@/api/directory'
import type { Credential, DirectorySource } from '@/types'

const settingsResourceFreshnessMs = 5 * 60_000

function errorMessage(error: unknown) {
  if (error instanceof Error && error.message) return error.message
  return 'failed'
}

export const useSettingsResourcesStore = defineStore('settingsResources', () => {
  const credentials = ref<Credential[]>([])
  const credentialsLoading = ref(false)
  const credentialsLoaded = ref(false)
  const credentialsError = ref('')
  const directorySources = ref<DirectorySource[]>([])
  const directorySourcesLoading = ref(false)
  const directorySourcesLoaded = ref(false)
  const directorySourcesError = ref('')

  let credentialsFreshUntil = 0
  let directorySourcesFreshUntil = 0
  let credentialsRequest: Promise<void> | null = null
  let directorySourcesRequest: Promise<void> | null = null
  let credentialsQueuedForce: Promise<void> | null = null
  let directorySourcesQueuedForce: Promise<void> | null = null
  let generation = 0

  function loadCredentials(options: { force?: boolean } = {}): Promise<void> {
    if (credentialsRequest) {
      if (!options.force) return credentialsRequest
      if (credentialsQueuedForce) return credentialsQueuedForce
      const requestGeneration = generation
      let queued!: Promise<void>
      queued = credentialsRequest
        .then(() => {
          if (requestGeneration === generation) return loadCredentials({ force: true })
        })
        .finally(() => {
          if (credentialsQueuedForce === queued) credentialsQueuedForce = null
        })
      credentialsQueuedForce = queued
      return queued
    }
    if (!options.force && credentialsLoaded.value && Date.now() < credentialsFreshUntil) {
      return Promise.resolve()
    }

    const requestGeneration = generation
    credentialsLoading.value = true
    credentialsError.value = ''
    let request!: Promise<void>
    request = listCredentials()
      .then((response) => {
        if (requestGeneration !== generation) return
        const data = response.data.data
        credentials.value = Array.isArray(data) ? [...data] : []
        credentialsLoaded.value = true
        credentialsFreshUntil = Date.now() + settingsResourceFreshnessMs
      })
      .catch((error: unknown) => {
        if (requestGeneration !== generation) return
        credentialsError.value = errorMessage(error)
        credentialsFreshUntil = 0
      })
      .finally(() => {
        if (credentialsRequest !== request) return
        credentialsRequest = null
        credentialsLoading.value = false
      })
    credentialsRequest = request
    return request
  }

  function loadDirectorySources(options: { force?: boolean } = {}): Promise<void> {
    if (directorySourcesRequest) {
      if (!options.force) return directorySourcesRequest
      if (directorySourcesQueuedForce) return directorySourcesQueuedForce
      const requestGeneration = generation
      let queued!: Promise<void>
      queued = directorySourcesRequest
        .then(() => {
          if (requestGeneration === generation) return loadDirectorySources({ force: true })
        })
        .finally(() => {
          if (directorySourcesQueuedForce === queued) directorySourcesQueuedForce = null
        })
      directorySourcesQueuedForce = queued
      return queued
    }
    if (!options.force && directorySourcesLoaded.value && Date.now() < directorySourcesFreshUntil) {
      return Promise.resolve()
    }

    const requestGeneration = generation
    directorySourcesLoading.value = true
    directorySourcesError.value = ''
    let request!: Promise<void>
    request = listDirectorySources()
      .then((response) => {
        if (requestGeneration !== generation) return
        directorySources.value = [...(response.data.data?.items ?? [])]
        directorySourcesLoaded.value = true
        directorySourcesFreshUntil = Date.now() + settingsResourceFreshnessMs
      })
      .catch((error: unknown) => {
        if (requestGeneration !== generation) return
        directorySourcesError.value = errorMessage(error)
        directorySourcesFreshUntil = 0
      })
      .finally(() => {
        if (directorySourcesRequest !== request) return
        directorySourcesRequest = null
        directorySourcesLoading.value = false
      })
    directorySourcesRequest = request
    return request
  }

  function replaceDirectorySources(sources: DirectorySource[]) {
    directorySources.value = [...sources]
    directorySourcesLoaded.value = true
    directorySourcesError.value = ''
    directorySourcesFreshUntil = Date.now() + settingsResourceFreshnessMs
  }

  function invalidateCredentials() {
    credentialsFreshUntil = 0
  }

  function invalidateDirectorySources() {
    directorySourcesFreshUntil = 0
  }

  function resetSettingsResources() {
    generation += 1
    credentialsRequest = null
    directorySourcesRequest = null
    credentialsQueuedForce = null
    directorySourcesQueuedForce = null
    credentialsFreshUntil = 0
    directorySourcesFreshUntil = 0
    credentials.value = []
    credentialsLoading.value = false
    credentialsLoaded.value = false
    credentialsError.value = ''
    directorySources.value = []
    directorySourcesLoading.value = false
    directorySourcesLoaded.value = false
    directorySourcesError.value = ''
  }

  return {
    credentials,
    credentialsLoading,
    credentialsLoaded,
    credentialsError,
    directorySources,
    directorySourcesLoading,
    directorySourcesLoaded,
    directorySourcesError,
    loadCredentials,
    loadDirectorySources,
    replaceDirectorySources,
    invalidateCredentials,
    invalidateDirectorySources,
    resetSettingsResources,
  }
})
