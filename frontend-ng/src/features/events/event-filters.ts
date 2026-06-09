export interface EventFilterState {
  from: string
  to: string
  tool: string
  bindingStatus: string
  q: string
  limit: number
  offset: number
  userId?: number | null
}

export interface EventPaginationInput {
  total: number
  limit: number
  offset: number
}

export interface EventDetailMatchedPR {
  pr_record_id: number
  scm_pr_id: number
  title: string
  status: string
  scm_pr_url: string
}

export function defaultEventFilters(search: Record<string, unknown> = {}): EventFilterState {
  return {
    from: stringSearch(search.from) || toDateTimeLocal(new Date(Date.now() - 7 * 24 * 60 * 60 * 1000)),
    to: stringSearch(search.to) || toDateTimeLocal(new Date()),
    tool: stringSearch(search.tool),
    bindingStatus: stringSearch(search.binding_status),
    q: stringSearch(search.q),
    limit: positiveNumberSearch(search.limit, 20),
    offset: nonNegativeNumberSearch(search.offset, 0),
    userId: nonNegativeNumberSearch(search.user_id, 0) || null
  }
}

export function buildEventQuery(filters: EventFilterState, options: { includePagination?: boolean } = {}) {
  const includePagination = options.includePagination ?? true
  const params: Record<string, string | number> = {}
  const from = fromDateTimeLocal(filters.from)
  const to = fromDateTimeLocal(filters.to)
  if (from) params.from = from
  if (to) params.to = to
  if (filters.tool) params.tool = filters.tool
  if (filters.bindingStatus) params.binding_status = filters.bindingStatus
  if (filters.q.trim()) params.q = filters.q.trim()
  if (filters.userId) params.user_id = filters.userId
  if (includePagination) {
    params.limit = filters.limit
    params.offset = filters.offset
  }
  return params
}

export function eventFiltersForRole(filters: EventFilterState, role?: string) {
  return role === 'admin' ? filters : { ...filters, userId: null }
}

export function buildEventSearch(filters: EventFilterState) {
  const search: Record<string, string | number> = {}
  if (filters.from) search.from = filters.from
  if (filters.to) search.to = filters.to
  if (filters.tool) search.tool = filters.tool
  if (filters.bindingStatus) search.binding_status = filters.bindingStatus
  if (filters.q.trim()) search.q = filters.q.trim()
  if (filters.limit !== 20) search.limit = filters.limit
  if (filters.offset > 0) search.offset = filters.offset
  if (filters.userId) search.user_id = filters.userId
  return search
}

export function getEventPagination({ total, limit, offset }: EventPaginationInput) {
  const safeLimit = Math.max(1, limit)
  return {
    currentPage: Math.floor(offset / safeLimit) + 1,
    totalPages: Math.max(1, Math.ceil(total / safeLimit)),
    canGoPrev: offset > 0,
    canGoNext: offset + safeLimit < total
  }
}

export function eventDetailPrLabel(pr: EventDetailMatchedPR) {
  return `#${pr.scm_pr_id} ${pr.title} · ${pr.status}`
}

export function filterToSegment(value: string) {
  return value || 'all'
}

export function segmentToFilter(value: string) {
  return value === 'all' ? '' : value
}

export function toDateTimeLocal(date: Date) {
  const pad = (value: number) => String(value).padStart(2, '0')
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`
}

function fromDateTimeLocal(value: string) {
  if (!value) return ''
  const date = new Date(value)
  return Number.isNaN(date.getTime()) ? '' : date.toISOString()
}

function stringSearch(value: unknown) {
  return typeof value === 'string' ? value : ''
}

function positiveNumberSearch(value: unknown, fallback: number) {
  const next = Number(value)
  return Number.isFinite(next) && next > 0 ? next : fallback
}

function nonNegativeNumberSearch(value: unknown, fallback: number) {
  const next = Number(value)
  return Number.isFinite(next) && next >= 0 ? next : fallback
}
