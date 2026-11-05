<script setup lang="ts">
import ActivityAnalytics from '@/components/activity/ActivityAnalytics.vue'
import { dataset } from './useActivityAnalytics'
</script>

<template>
  <div class="min-h-screen bg-slate-50">
    <div class="border-b border-amber-300 bg-amber-50 px-6 py-2 text-sm text-amber-900">
      <strong>预览</strong> · 界面为真实组件，数字为虚构示例，不连后端。
      <label class="ml-4"><input v-model="dataset" type="radio" value="codexOnly" class="mr-1">仅 Codex（无 credit）</label>
      <label class="ml-4"><input v-model="dataset" type="radio" value="mixed" class="mr-1">混合（有 Kiro credit）</label>
    </div>
    <div class="mx-auto max-w-[1400px] px-6 py-6">
      <p class="text-xs font-semibold uppercase tracking-wider text-blue-600">AI CODING ACTIVITY</p>
      <h1 class="mt-1 text-3xl font-semibold tracking-tight text-slate-950">My coding activity</h1>
      <p class="mt-1 text-slate-600">See AI Token become committed code across repositories and PRs.</p>
      <div class="mt-6"><ActivityAnalytics scope="personal" /></div>
    </div>
  </div>
</template>
