<script setup lang="ts">
import AppSidebar from './AppSidebar.vue'
</script>

<template>
  <div class="flex h-screen">
    <AppSidebar />
    <main class="flex-1 overflow-auto p-6">
      <slot />
    </main>
  </div>
</template>
