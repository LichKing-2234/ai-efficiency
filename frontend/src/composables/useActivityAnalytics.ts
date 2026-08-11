import { computed, ref, watch } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import {
  getActivityV2Overview,
  listActivityV2PullRequests,
  listActivityV2Repositories,
} from '@/api/activity'
import type {
  ActivityV2Overview,
  ActivityV2Page,
  ActivityV2PullRequestRow,
  ActivityV2Query,
  ActivityV2RepositoryRow,
  ActivityV2Scope,
  ActivityWindowParams,
} from '@/types/activity'

type Lane = 'ratio' | 'trend' | 'repoTop' | 'prTop' | 'repositories' | 'pullRequests'
type ActivityAnalyticsContext = Readonly<{
  scope: ActivityV2Scope
  subjectUserId?: number
  teamId?: string
}>

function queryString(value: unknown) {
  if (Array.isArray(value)) return typeof value[0] === 'string' ? value[0] : ''
  return typeof value === 'string' ? value : ''
}

function positiveQueryID(value: unknown) {
  const parsed = Number.parseInt(queryString(value), 10)
  return Number.isInteger(parsed) && parsed > 0 ? parsed : undefined
}

function localDate(value = new Date()) {
  const year = value.getFullYear()
  const month = String(value.getMonth() + 1).padStart(2, '0')
  const day = String(value.getDate()).padStart(2, '0')
  return `${year}-${month}-${day}`
}

export function useActivityAnalytics(context: ActivityAnalyticsContext, errorMessage: () => string) {
  const route = useRoute()
  const router = useRouter()
  const browserTimezone = Intl.DateTimeFormat().resolvedOptions().timeZone || 'UTC'

  function initialRange(): Required<ActivityWindowParams> {
    const fromQuery = queryString(route.query.from)
    const toQuery = queryString(route.query.to)
    if (/^\d{4}-\d{2}-\d{2}$/.test(fromQuery) && /^\d{4}-\d{2}-\d{2}$/.test(toQuery)) return { from: fromQuery, to: toQuery }
    const to = new Date()
    const from = new Date(to.getFullYear(), to.getMonth(), to.getDate())
    from.setDate(from.getDate() - 29)
    return { from: localDate(from), to: localDate(to) }
  }

  const range = ref(initialRange())
  const ratioOverview = ref<ActivityV2Overview | null>(null)
  const trendOverview = ref<ActivityV2Overview | null>(null)
  const repoTop = ref<ActivityV2RepositoryRow[]>([])
  const prTop = ref<ActivityV2PullRequestRow[]>([])
  const repositories = ref<ActivityV2Page<ActivityV2RepositoryRow> | null>(null)
  const pullRequests = ref<ActivityV2Page<ActivityV2PullRequestRow> | null>(null)
  const loading = ref<Record<Lane, boolean>>({ ratio: false, trend: false, repoTop: false, prTop: false, repositories: false, pullRequests: false })
  const errors = ref<Record<Lane, string>>({ ratio: '', trend: '', repoTop: '', prTop: '', repositories: '', pullRequests: '' })
  const activeList = ref<'repositories' | 'pullRequests'>('repositories')
  const search = ref('')
  const sort = ref<'tokens' | 'name'>('tokens')
  const repoCursors = ref<Array<string | undefined>>([undefined])
  const prCursors = ref<Array<string | undefined>>([undefined])
  const repoPage = ref(0)
  const prPage = ref(0)
  const laneGeneration: Record<Lane, number> = { ratio: 0, trend: 0, repoTop: 0, prTop: 0, repositories: 0, pullRequests: 0 }

  const selectedRepoID = computed(() => positiveQueryID(route.query.repo_id))
  const selectedPRID = computed(() => positiveQueryID(route.query.pr_record_id))
  const expandedPR = computed(() => selectedPRID.value ?? null)
  const overallQuery = computed<ActivityV2Query>(() => ({
    scope: context.scope,
    ...(context.subjectUserId ? { subject_user_id: context.subjectUserId } : {}),
    ...(context.teamId ? { team_id: context.teamId } : {}),
    from: range.value.from,
    to: range.value.to,
    timezone: queryString(route.query.timezone) || browserTimezone,
  }))
  const filteredQuery = computed<ActivityV2Query>(() => ({
    ...overallQuery.value,
    ...(selectedRepoID.value ? { repo_id: selectedRepoID.value } : {}),
    ...(selectedPRID.value ? { pr_record_id: selectedPRID.value } : {}),
  }))
  const refreshing = computed(() => Object.values(loading.value).some(Boolean))
  const selectedPRRow = computed(() => pullRequests.value?.items.find((row) => row.pr_record_id === selectedPRID.value) ?? null)

  async function runLane<T>(lane: Lane, clear: boolean, clearData: () => void, request: () => Promise<T>, apply: (value: T) => void) {
    const generation = ++laneGeneration[lane]
    if (clear) clearData()
    loading.value[lane] = true
    errors.value[lane] = ''
    try {
      const value = await request()
      if (generation === laneGeneration[lane]) apply(value)
    } catch (error) {
      if (generation !== laneGeneration[lane]) return
      console.error(error)
      errors.value[lane] = errorMessage()
    } finally {
      if (generation === laneGeneration[lane]) loading.value[lane] = false
    }
  }

  function loadRatio(clear = false) {
    return runLane('ratio', clear, () => { ratioOverview.value = null }, async () => {
      const response = await getActivityV2Overview(overallQuery.value)
      if (!response.data.data) throw new Error('empty Activity overview')
      return response.data.data
    }, (value) => { ratioOverview.value = value })
  }

  function loadTrend(clear = false) {
    return runLane('trend', clear, () => { trendOverview.value = null }, async () => {
      const response = await getActivityV2Overview(filteredQuery.value)
      if (!response.data.data) throw new Error('empty Activity overview')
      return response.data.data
    }, (value) => { trendOverview.value = value })
  }

  function loadRepoTop(clear = false) {
    return runLane('repoTop', clear, () => { repoTop.value = [] }, async () => {
      const response = await listActivityV2Repositories({ ...overallQuery.value, sort: 'tokens' })
      return response.data.data?.items ?? []
    }, (value) => { repoTop.value = value })
  }

  function loadPRTop(clear = false) {
    return runLane('prTop', clear, () => { prTop.value = [] }, async () => {
      const response = await listActivityV2PullRequests({ ...overallQuery.value, sort: 'tokens' })
      return response.data.data?.items ?? []
    }, (value) => { prTop.value = value })
  }

  function loadRepositories(clear = false, cursor?: string) {
    return runLane('repositories', clear, () => { repositories.value = null }, async () => {
      const response = await listActivityV2Repositories({ ...overallQuery.value, search: search.value.trim() || undefined, sort: sort.value, cursor })
      return response.data.data ?? { items: [] }
    }, (value) => { repositories.value = value })
  }

  function loadPullRequests(clear = false, cursor?: string) {
    return runLane('pullRequests', clear, () => { pullRequests.value = null }, async () => {
      const response = await listActivityV2PullRequests({ ...filteredQuery.value, search: search.value.trim() || undefined, sort: sort.value, cursor })
      return response.data.data ?? { items: [] }
    }, (value) => { pullRequests.value = value })
  }

  function resetPages() {
    repoCursors.value = [undefined]
    prCursors.value = [undefined]
    repoPage.value = 0
    prPage.value = 0
  }

  function resetPullRequestPage() {
    prCursors.value = [undefined]
    prPage.value = 0
  }

  function loadContext(clear: boolean) {
    resetPages()
    void loadRatio(clear)
    void loadTrend(clear)
    void loadRepoTop(clear)
    void loadPRTop(clear)
    void loadRepositories(clear)
    void loadPullRequests(clear)
  }

  function refresh() { loadContext(false) }
  function selectRange(next: ActivityWindowParams) {
    if (!next.from || !next.to) return
    range.value = { from: next.from, to: next.to }
    void router.push({ query: { ...route.query, from: next.from, to: next.to, timezone: overallQuery.value.timezone } })
  }
  function selectRepository(row: ActivityV2RepositoryRow) {
    void router.push({ query: { ...route.query, repo_id: String(row.repo_config_id), pr_record_id: undefined } })
  }
  function selectPullRequest(row: ActivityV2PullRequestRow) {
    void router.push({ query: { ...route.query, repo_id: String(row.repo_config_id), pr_record_id: String(row.pr_record_id) } })
  }
  function clearFilter() {
    void router.push({ query: { ...route.query, repo_id: undefined, pr_record_id: undefined } })
  }
  function applyListControls() {
    resetPages()
    if (activeList.value === 'repositories') void loadRepositories(true)
    else void loadPullRequests(true)
  }
  function nextPage(kind: 'repositories' | 'pullRequests') {
    if (kind === 'repositories') {
      const cursor = repositories.value?.next_cursor
      if (!cursor) return
      repoCursors.value[repoPage.value + 1] = cursor
      repoPage.value += 1
      void loadRepositories(true, cursor)
    } else {
      const cursor = pullRequests.value?.next_cursor
      if (!cursor) return
      prCursors.value[prPage.value + 1] = cursor
      prPage.value += 1
      void loadPullRequests(true, cursor)
    }
  }
  function previousPage(kind: 'repositories' | 'pullRequests') {
    if (kind === 'repositories' && repoPage.value > 0) {
      repoPage.value -= 1
      void loadRepositories(true, repoCursors.value[repoPage.value])
    } else if (kind === 'pullRequests' && prPage.value > 0) {
      prPage.value -= 1
      void loadPullRequests(true, prCursors.value[prPage.value])
    }
  }

  watch(
    [
      () => context.scope,
      () => context.subjectUserId,
      () => context.teamId,
      () => route.query.from,
      () => route.query.to,
      () => route.query.timezone,
    ],
    () => {
      range.value = initialRange()
      if (!queryString(route.query.from) || !queryString(route.query.to) || !queryString(route.query.timezone)) {
        void router.replace({ query: { ...route.query, from: range.value.from, to: range.value.to, timezone: overallQuery.value.timezone } })
      }
      loadContext(true)
    },
    { immediate: true },
  )
  watch(
    [() => route.query.repo_id, () => route.query.pr_record_id],
    () => {
      resetPullRequestPage()
      void loadTrend(true)
      void loadPullRequests(true)
    },
  )
  watch(activeList, () => {
    search.value = ''
    sort.value = 'tokens'
    resetPages()
    if (activeList.value === 'repositories') void loadRepositories(true)
    else void loadPullRequests(true)
  })

  return {
    range, ratioOverview, trendOverview, repoTop, prTop, repositories, pullRequests,
    loading, errors, activeList, search, sort, repoPage, prPage, expandedPR,
    selectedRepoID, selectedPRID, overallQuery, refreshing, selectedPRRow,
    loadRatio, loadTrend, loadRepoTop, loadPRTop, loadRepositories, loadPullRequests,
    refresh, selectRange, selectRepository, selectPullRequest, clearFilter,
    applyListControls, nextPage, previousPage,
  }
}
