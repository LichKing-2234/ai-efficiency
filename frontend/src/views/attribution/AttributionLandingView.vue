<script setup lang="ts">
import AppLayout from '@/components/AppLayout.vue'
</script>

<template>
  <AppLayout>
    <div class="mx-auto max-w-5xl space-y-6 px-4 py-6 sm:px-6 lg:px-8">
      <div>
        <h1 class="text-2xl font-bold text-gray-900">Attribution</h1>
        <p class="mt-2 max-w-3xl text-sm text-gray-600">
          Attribution is the primary entrypoint for the sessionless workflow. Use repo and PR views as the formal path for usage,
          checkpoint, and settlement data.
        </p>
      </div>

      <div class="grid gap-4 md:grid-cols-1">
        <router-link
          to="/repos"
          class="rounded-lg border border-gray-200 bg-white p-5 shadow-sm transition hover:border-indigo-300 hover:shadow"
        >
          <h2 class="text-base font-semibold text-gray-900">Repo / PR Attribution</h2>
          <p class="mt-2 text-sm text-gray-600">
            Inspect repo state, PR settlement, and attribution summaries from the current formal workflow.
          </p>
        </router-link>

      </div>
    </div>
  </AppLayout>
</template>
