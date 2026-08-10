<script setup lang="ts">
import { ref } from 'vue'
import { getActivityBucket } from '@/api/activity'
import CursorPager from '@/components/activity/CursorPager.vue'
import { useI18n } from '@/i18n'
import type { ActivityBucketDetail, ActivityMemberResponse } from '@/types/activity'

const props = defineProps<{ activity: ActivityMemberResponse; prLoading?: boolean; hasPreviousPrPage?: boolean }>()
const emit = defineEmits<{ nextPr: []; previousPr: [] }>()
const { t } = useI18n()
const expandedPRs = ref(new Set<number>())
const bucketDetails = ref<Record<string, ActivityBucketDetail>>({})
const bucketErrors = ref<Record<string, boolean>>({})
const bucketLoading = ref<Record<string, boolean>>({})

function togglePR(id: number) {
  const next = new Set(expandedPRs.value)
  next.has(id) ? next.delete(id) : next.add(id)
  expandedPRs.value = next
}

async function openBucket(id: string) {
  if (bucketDetails.value[id] || bucketLoading.value[id]) return
  bucketLoading.value = { ...bucketLoading.value, [id]: true }
  bucketErrors.value = { ...bucketErrors.value, [id]: false }
  try {
    const response = await getActivityBucket(id)
    if (!response.data.data) throw new Error('Bucket detail is empty')
    bucketDetails.value = { ...bucketDetails.value, [id]: response.data.data }
  } catch {
    bucketErrors.value = { ...bucketErrors.value, [id]: true }
  } finally {
    bucketLoading.value = { ...bucketLoading.value, [id]: false }
  }
}

function shortSHA(value: string) { return value.slice(0, 10) }
function count(value: number) { return new Intl.NumberFormat().format(value) }
function allocationStatus(value: string) {
  const key = value === 'bound_auto'
    ? 'activity.allocationStatusAuto'
    : value === 'bound_manual'
      ? 'activity.allocationStatusManual'
      : value === 'shared'
        ? 'activity.allocationStatusShared'
        : 'activity.allocationStatusUnbound'
  return t(key)
}
function requestIDState(value: ActivityBucketDetail['request_ids']['state']) {
  return t(`activity.requestIDState.${value}`)
}
</script>

<template>
  <div data-testid="activity-wide-details" class="min-w-0 space-y-4">
    <div data-testid="activity-primary-details" class="grid min-w-0 gap-4 xl:grid-cols-2">
    <section data-testid="activity-prs" class="min-w-0 rounded-lg border border-slate-200 bg-white shadow-sm">
      <div class="border-b border-slate-200 px-5 py-4"><h2 class="font-semibold text-slate-950">{{ t('activity.pullRequests') }}</h2></div>
      <div v-if="activity.prs.items.length === 0" class="px-5 py-10 text-center text-sm text-slate-500">{{ t('activity.noPullRequests') }}</div>
      <article v-for="pr in activity.prs.items" :key="`${pr.repo_config_id}:${pr.pr_record_id}`" class="border-b border-slate-100 px-5 py-4 last:border-0">
        <div class="flex flex-col gap-2 sm:flex-row sm:items-start sm:justify-between">
          <div class="min-w-0">
            <p class="text-xs text-slate-500">{{ pr.repo_name }} · PR #{{ pr.scm_pr_id }}</p>
            <ElLink :href="pr.url" target="_blank" rel="noopener noreferrer" type="primary" class="mt-1 max-w-full font-medium">
              {{ pr.title }}
            </ElLink>
          </div>
          <ElButton class="min-h-10 shrink-0 !ml-0" :aria-expanded="expandedPRs.has(pr.pr_record_id)" @click="togglePR(pr.pr_record_id)">
            {{ t('activity.commits') }} · {{ pr.commits.length }}
          </ElButton>
        </div>
        <div v-if="expandedPRs.has(pr.pr_record_id)" class="mt-3 space-y-2 rounded-lg bg-slate-50 p-3">
          <div v-for="commit in pr.commits" :key="`${commit.repo_config_id}:${commit.commit_sha}`" class="break-all font-mono text-xs text-slate-700">{{ shortSHA(commit.commit_sha) }}</div>
        </div>
      </article>
      <CursorPager
        :has-previous="Boolean(hasPreviousPrPage)"
        :has-next="Boolean(activity.prs.next_cursor)"
        :loading="prLoading"
        :previous-label="t('activity.previousPage')"
        :next-label="t('activity.nextPage')"
        test-i-d-prefix="activity-prs"
        @previous="emit('previousPr')"
        @next="emit('nextPr')"
      />
    </section>

    <section data-testid="activity-commits" class="min-w-0 rounded-lg border border-slate-200 bg-white shadow-sm">
      <div class="border-b border-slate-200 px-5 py-4"><h2 class="font-semibold text-slate-950">{{ t('activity.commits') }}</h2></div>
      <div
        v-if="activity.commits.items.length > 0"
        data-testid="activity-commit-column-labels"
        class="hidden grid-cols-[minmax(10rem,1fr)_9rem_8rem] gap-4 border-b border-slate-100 bg-slate-50 px-5 py-2 text-xs font-medium text-slate-500 sm:grid"
      >
        <span>{{ t('activity.commits') }}</span>
        <span>{{ t('activity.pullRequests') }}</span>
        <span class="text-right">{{ t('activity.processedTokens') }}</span>
      </div>
      <div class="divide-y divide-slate-100">
        <div
          v-for="commit in activity.commits.items"
          :key="`${commit.repo_config_id}:${commit.commit_sha}`"
          :data-testid="`activity-commit-${commit.repo_config_id}-${commit.commit_sha}`"
          class="grid gap-3 px-5 py-4 text-sm sm:grid-cols-[minmax(10rem,1fr)_9rem_8rem] sm:gap-4"
        >
          <div class="min-w-0"><p class="truncate font-medium text-slate-900">{{ commit.repo_name }}</p><p class="mt-1 break-all font-mono text-xs text-slate-500">{{ shortSHA(commit.commit_sha) }}</p></div>
          <div class="text-slate-600">{{ commit.prs.length }} PR</div>
          <div class="text-slate-600 sm:text-right" :title="t('activity.tokenDetail')"><span class="sm:hidden">{{ t('activity.processedTokens') }}: </span>{{ count(commit.processed_tokens) }}</div>
        </div>
      </div>
    </section>
    </div>

    <div
      data-testid="activity-diagnostics"
      class="grid min-w-0 gap-4"
      :class="activity.bucket_access ? 'xl:grid-cols-[minmax(18rem,0.7fr)_minmax(0,1.3fr)]' : ''"
    >
    <section data-testid="activity-data-quality" class="min-w-0 rounded-lg border border-slate-200 bg-white p-4 shadow-sm">
      <h2 class="font-semibold text-slate-950">{{ t('activity.dataQuality') }}</h2>
      <dl class="mt-4 grid gap-3 text-sm sm:grid-cols-3">
        <div><dt class="text-slate-500">{{ t('activity.unbound') }}</dt><dd class="mt-1 font-semibold text-slate-900">{{ activity.quality.unbound_buckets }}</dd></div>
        <div><dt class="text-slate-500">{{ t('activity.shared') }}</dt><dd class="mt-1 font-semibold text-slate-900">{{ activity.quality.multi_repo_shared_buckets }}</dd></div>
        <div><dt class="text-slate-500">{{ t('activity.invalidFacts') }}</dt><dd class="mt-1 font-semibold text-slate-900">{{ activity.quality.invalid_token_facts + activity.quality.coverage_gap_count }}</dd></div>
      </dl>
    </section>

    <section v-if="activity.bucket_access" data-testid="activity-buckets" class="min-w-0 rounded-lg border border-slate-200 bg-white shadow-sm">
      <div class="border-b border-slate-200 px-5 py-4"><h2 class="font-semibold text-slate-950">{{ t('activity.bucketDetails') }}</h2></div>
      <article v-for="bucket in activity.buckets.items" :key="bucket.bucket_id" class="border-b border-slate-100 px-5 py-4 last:border-0">
        <ElButton
          :data-testid="`activity-bucket-${bucket.bucket_id}`"
          class="!ml-0 h-auto max-w-full break-all p-0 text-left font-mono text-xs"
          type="primary"
          link
          :aria-expanded="Boolean(bucketDetails[bucket.bucket_id])"
          :disabled="bucketLoading[bucket.bucket_id]"
          @click="openBucket(bucket.bucket_id)"
        >
          {{ bucket.bucket_id }}
        </ElButton>
        <p v-if="bucketLoading[bucket.bucket_id]" class="mt-2 text-xs text-slate-500" role="status">{{ t('activity.loadingBucket') }}</p>
        <div
          v-if="bucketDetails[bucket.bucket_id]"
          :data-testid="`activity-bucket-detail-${bucket.bucket_id}`"
          class="mt-3 space-y-4 rounded-lg bg-slate-50 p-4 text-xs text-slate-700"
        >
          <dl class="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <div><dt class="text-slate-500">{{ t('activity.freshInput') }}</dt><dd class="mt-1 font-semibold text-slate-900">{{ count(bucketDetails[bucket.bucket_id].tokens.fresh_input_tokens) }}</dd></div>
            <div><dt class="text-slate-500">{{ t('activity.cacheRead') }}</dt><dd class="mt-1 font-semibold text-slate-900">{{ count(bucketDetails[bucket.bucket_id].tokens.cache_read_tokens) }}</dd></div>
            <div><dt class="text-slate-500">{{ t('activity.cacheWrite') }}</dt><dd class="mt-1 font-semibold text-slate-900">{{ count(bucketDetails[bucket.bucket_id].tokens.cache_write_tokens) }}</dd></div>
            <div><dt class="text-slate-500">{{ t('activity.outputTokens') }}</dt><dd class="mt-1 font-semibold text-slate-900">{{ count(bucketDetails[bucket.bucket_id].tokens.output_tokens) }}</dd></div>
            <div><dt class="text-slate-500">{{ t('activity.reasoningTokens') }}</dt><dd class="mt-1 font-semibold text-slate-900">{{ count(bucketDetails[bucket.bucket_id].tokens.reasoning_tokens) }}</dd></div>
            <div><dt class="text-slate-500">{{ t('activity.processedTokens') }}</dt><dd class="mt-1 font-semibold text-slate-900">{{ count(bucketDetails[bucket.bucket_id].tokens.processed_total_tokens) }}</dd></div>
            <div><dt class="text-slate-500">{{ t('activity.allocationStatus') }}</dt><dd class="mt-1 font-medium text-slate-900">{{ allocationStatus(bucket.allocation_status) }}</dd></div>
            <div><dt class="text-slate-500">{{ t('activity.revisionReason') }}</dt><dd class="mt-1 font-medium text-slate-900">{{ bucketDetails[bucket.bucket_id].revision.reason }}</dd></div>
            <div><dt class="text-slate-500">{{ t('activity.extractor') }}</dt><dd class="mt-1 font-medium text-slate-900">{{ bucketDetails[bucket.bucket_id].extractor_version }}</dd></div>
            <div><dt class="text-slate-500">{{ t('activity.normalizationVersion') }}</dt><dd class="mt-1 font-medium text-slate-900">{{ bucketDetails[bucket.bucket_id].normalization_version }}</dd></div>
            <div><dt class="text-slate-500">{{ t('activity.correlationQuality') }}</dt><dd class="mt-1 font-medium text-slate-900">{{ bucketDetails[bucket.bucket_id].correlation_quality }}</dd></div>
            <div><dt class="text-slate-500">{{ t('activity.requestIDState') }}</dt><dd class="mt-1 font-medium text-slate-900">{{ requestIDState(bucketDetails[bucket.bucket_id].request_ids.state) }}</dd></div>
          </dl>
          <div v-if="bucketDetails[bucket.bucket_id].request_ids.evidence.length" class="border-t border-slate-200 pt-3">
            <p class="font-medium text-slate-900">{{ t('activity.requestIDEvidence') }}</p>
            <ul class="mt-2 space-y-1">
              <li v-for="evidence in bucketDetails[bucket.bucket_id].request_ids.evidence" :key="`${evidence.request_id}:${evidence.observed_at}`" class="break-all font-mono">{{ evidence.request_id }}</li>
            </ul>
          </div>
        </div>
        <ElAlert
          v-if="bucketErrors[bucket.bucket_id]"
          class="mt-2"
          type="error"
          :title="t('activity.bucketLoadFailed')"
          :closable="false"
        />
      </article>
    </section>
    </div>
  </div>
</template>
