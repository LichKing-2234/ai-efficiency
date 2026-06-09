import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { MetricCard } from '@/components/primitives/metric-card'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { compact, dateTime, number, tokenTotal } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import type { ToolUsageEventDetail, ToolUsageEventUserOption } from '@/lib/api/types'
import { buildEventQuery, buildEventSearch, defaultEventFilters, eventDetailPrLabel, eventFiltersForRole, getEventPagination, type EventFilterState } from './event-filters'

export function EventsPage() {
  const { t } = useI18n()
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as Record<string, unknown>
  const [filters, setFilters] = useState<EventFilterState>(() => defaultEventFilters(search))
  const [appliedFilters, setAppliedFilters] = useState<EventFilterState>(() => defaultEventFilters(search))
  const [userSearch, setUserSearch] = useState('')
  const [userOptions, setUserOptions] = useState<ToolUsageEventUserOption[]>([])
  const [selected, setSelected] = useState<ToolUsageEventDetail | null>(null)
  const me = useQuery({ queryKey: ['auth', 'me'], queryFn: api.auth.me })
  const isAdmin = me.data?.role === 'admin'
  const effectiveFilters = eventFiltersForRole(appliedFilters, me.data?.role)
  const listParams = buildEventQuery(effectiveFilters)
  const summaryParams = buildEventQuery(effectiveFilters, { includePagination: false })
  const summary = useQuery({ queryKey: ['events', 'summary', summaryParams], queryFn: () => api.events.summary(summaryParams) })
  const list = useQuery({ queryKey: ['events', 'list', listParams], queryFn: () => api.events.list(listParams) })
  const users = useQuery({
    queryKey: ['events', 'users', userSearch],
    queryFn: () => api.events.users({ q: userSearch, limit: 20 }),
    enabled: false
  })
  const detail = useMutation({ mutationFn: api.events.detail, onSuccess: setSelected })
  const total = list.data?.total ?? 0
  const pagination = getEventPagination({ total, limit: appliedFilters.limit, offset: appliedFilters.offset })

  function replaceSearch(nextFilters: EventFilterState) {
    void navigate({ to: '/events', search: buildEventSearch(nextFilters) })
  }

  function applyFilters(nextFilters: EventFilterState) {
    setFilters(nextFilters)
    setAppliedFilters(nextFilters)
    replaceSearch(nextFilters)
  }

  function applyCurrentFilters() {
    applyFilters({ ...filters, offset: 0 })
  }

  function clearTimeRange() {
    applyFilters({ ...filters, from: '', to: '', offset: 0 })
  }

  function previousPage() {
    if (!pagination.canGoPrev) return
    applyFilters({ ...appliedFilters, offset: Math.max(0, appliedFilters.offset - appliedFilters.limit) })
  }

  function nextPage() {
    if (!pagination.canGoNext) return
    applyFilters({ ...appliedFilters, offset: appliedFilters.offset + appliedFilters.limit })
  }

  function changePageSize(limit: number) {
    applyFilters({ ...appliedFilters, limit, offset: 0 })
  }

  async function searchUsers() {
    if (!isAdmin) return
    const result = await users.refetch()
    setUserOptions(result.data ?? [])
  }

  function selectUser(user: ToolUsageEventUserOption) {
    const nextFilters = { ...filters, userId: user.id, offset: 0 }
    setUserOptions([])
    setUserSearch('')
    applyFilters(nextFilters)
  }

  function clearSelectedUser() {
    const nextFilters = { ...filters, userId: null, offset: 0 }
    setUserOptions([])
    setUserSearch('')
    applyFilters(nextFilters)
  }

  if (list.isLoading || summary.isLoading || me.isLoading) return <LoadingState />

  return (
    <Page>
      <PageHeader title={t('events.title')} description={t('events.description')} />
      <div className='grid gap-4 sm:grid-cols-4'>
        <MetricCard label={t('events.totalEvents')} value={number(summary.data?.total_events)} />
        <MetricCard label={t('events.boundEvents')} value={number(summary.data?.bound_events)} accent />
        <MetricCard label={t('events.unboundEvents')} value={number(summary.data?.unbound_events)} />
        <MetricCard label={t('events.tool')} value={number(summary.data?.tool_counts?.length ?? 0)} />
      </div>
      <Card>
        <CardHeader>
          <CardTitle>{t('events.filters')}</CardTitle>
        </CardHeader>
        <CardContent className='flex flex-col gap-3'>
          <div className='grid gap-2 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_140px_150px_minmax(0,1.5fr)]'>
            <Input type='datetime-local' value={filters.from} onChange={(event) => setFilters((value) => ({ ...value, from: event.target.value }))} />
            <Input type='datetime-local' value={filters.to} onChange={(event) => setFilters((value) => ({ ...value, to: event.target.value }))} />
            <Select value={filters.tool || 'all'} onValueChange={(value) => setFilters((current) => ({ ...current, tool: value === 'all' ? '' : value }))}>
              <SelectTrigger className='w-full'><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='all'>{t('events.allTools')}</SelectItem>
                <SelectItem value='claude'>Claude</SelectItem>
                <SelectItem value='codex'>Codex</SelectItem>
                <SelectItem value='kiro'>Kiro</SelectItem>
              </SelectContent>
            </Select>
            <Select value={filters.bindingStatus || 'all'} onValueChange={(value) => setFilters((current) => ({ ...current, bindingStatus: value === 'all' ? '' : value }))}>
              <SelectTrigger className='w-full'><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='all'>{t('events.allCodeLinks')}</SelectItem>
                <SelectItem value='bound'>{t('repos.bound')}</SelectItem>
                <SelectItem value='unbound'>{t('repos.unbound')}</SelectItem>
              </SelectContent>
            </Select>
            <Input placeholder={t('events.searchRepoSessionSource')} value={filters.q} onChange={(event) => setFilters((value) => ({ ...value, q: event.target.value }))} />
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            {isAdmin ? (
              <>
                <Input className='w-72' placeholder={t('events.searchUsersByNameOrEmail')} value={userSearch} onChange={(event) => setUserSearch(event.target.value)} />
                <Button variant='outline' onClick={searchUsers} disabled={users.isFetching}>{t('adminUsers.searchUsers')}</Button>
              </>
            ) : null}
            {isAdmin && appliedFilters.userId ? <Button variant='ghost' onClick={clearSelectedUser}>{t('adminUsers.clearUser', { id: appliedFilters.userId })}</Button> : null}
            <div className='ml-auto flex gap-2'>
              <Button variant='outline' onClick={clearTimeRange}>{t('events.clearTime')}</Button>
              <Button onClick={applyCurrentFilters}>{t('common.applyFilters')}</Button>
            </div>
          </div>
          {userOptions.length > 0 ? (
            <div className='flex flex-col gap-1 rounded-md border border-border bg-card p-2'>
              {userOptions.map((user) => (
                <button key={user.id} className='rounded-sm px-2 py-1 text-left text-sm hover:bg-muted' type='button' onClick={() => selectUser(user)}>
                  <span className='font-medium'>{user.email || user.username}</span>
                  <span className='ml-2 text-muted-foreground text-xs'>{user.role} · {number(user.event_count)}</span>
                </button>
              ))}
            </div>
          ) : null}
        </CardContent>
      </Card>
      <Card className='overflow-hidden'>
        <CardHeader>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <CardTitle>{t('events.recentUsage')}</CardTitle>
            <div className='flex items-center gap-2 text-muted-foreground text-xs'>
              <span>{t('events.total', { total: number(total) })}</span>
              <Select value={String(appliedFilters.limit)} onValueChange={(value) => changePageSize(Number(value))}>
                <SelectTrigger size='sm'><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value='20'>20</SelectItem>
                  <SelectItem value='50'>50</SelectItem>
                  <SelectItem value='100'>100</SelectItem>
                </SelectContent>
              </Select>
              <Button size='sm' variant='outline' onClick={previousPage} disabled={!pagination.canGoPrev}>{t('common.previous')}</Button>
              <span>{t('common.pageCount', { current: pagination.currentPage, total: pagination.totalPages })}</span>
              <Button size='sm' variant='outline' onClick={nextPage} disabled={!pagination.canGoNext}>{t('common.next')}</Button>
            </div>
          </div>
        </CardHeader>
        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('events.tool')}</TableHead>
                <TableHead>{t('events.repository')}</TableHead>
                <TableHead>{t('events.requests')}</TableHead>
                <TableHead>{t('events.tokens')}</TableHead>
                <TableHead>{t('events.credit')}</TableHead>
                <TableHead>{t('events.binding')}</TableHead>
                <TableHead className='text-right'>{t('events.ended')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(list.data?.items ?? []).map((row) => (
                <TableRow key={row.id} className='cursor-pointer' onClick={() => detail.mutate(row.id)}>
                  <TableCell><Badge variant='ai'>{row.tool}</Badge></TableCell>
                  <TableCell>
                    <div className='font-medium text-foreground'>{row.repo_name || t('events.unlinked')}</div>
                    <div className='text-muted-foreground text-xs'>{row.source_basename}</div>
                  </TableCell>
                  <TableCell className='tnum'>{number(row.request_count)}</TableCell>
                  <TableCell className='tnum'>{compact(tokenTotal(row))}</TableCell>
                  <TableCell className='tnum'>{number(row.credit_usage)}</TableCell>
                  <TableCell><Badge variant={row.binding_status === 'bound' ? 'success' : 'warning'}>{row.binding_status}</Badge></TableCell>
                  <TableCell className='text-right'>{dateTime(row.observed_end_at)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Card>
      <Dialog open={!!selected} onOpenChange={(open) => !open && setSelected(null)}>
        <DialogContent className='max-w-2xl'>
          <DialogHeader>
            <DialogTitle>{t('events.detailTitle')}</DialogTitle>
            <DialogDescription>{selected?.tool_session_id || t('events.noToolSessionId')}</DialogDescription>
          </DialogHeader>
          {selected ? (
            <div className='max-h-[70vh] overflow-y-auto pr-1'>
              <div className='grid gap-3 sm:grid-cols-2'>
                <DetailItem label={t('events.tool')} value={selected.tool} />
                <DetailItem label={t('events.repository')} value={selected.repo_name || '-'} />
                <DetailItem label={t('events.observedStart')} value={dateTime(selected.observed_start_at)} />
                <DetailItem label={t('events.observedEnd')} value={dateTime(selected.observed_end_at)} />
                {isAdmin ? <DetailItem label={t('events.user')} value={selected.username || `#${selected.user_id}`} /> : null}
                <DetailItem label={t('events.context')} value={`${number(selected.context_usage_pct)}%`} />
              </div>
              <div className='mt-4 grid gap-3 sm:grid-cols-3'>
                <MetricCard label={t('usageDashboard.input')} value={compact(selected.input_tokens)} />
                <MetricCard label={t('usageDashboard.output')} value={compact(selected.output_tokens)} />
                <MetricCard label={t('events.cache')} value={compact(selected.cached_input_tokens)} />
                <MetricCard label={t('events.reasoning')} value={compact(selected.reasoning_tokens)} />
                <MetricCard label={t('events.credits')} value={number(selected.credit_usage)} accent />
                <MetricCard label={t('events.requests')} value={number(selected.request_count)} />
              </div>
              <div className='mt-4 rounded-md border border-border p-3'>
                <div className='flex flex-wrap items-center gap-2'>
                  <StatusBadge value={selected.binding_status} />
                  <span className='text-muted-foreground text-sm'>{t('events.checkpoint', { time: dateTime(selected.checkpoint_captured_at) })}</span>
                </div>
                <div className='mt-2 break-all font-mono text-xs'>{selected.commit_sha || '-'}</div>
              </div>
              <div className='mt-4'>
                <div className='font-medium text-sm'>PR</div>
                {selected.matched_prs.length > 0 ? (
                  <div className='mt-2 flex flex-col gap-2'>
                    {selected.matched_prs.map((pr) => (
                      <a key={pr.pr_record_id} className='rounded-md border border-border p-3 text-sm hover:bg-muted' href={pr.scm_pr_url} target='_blank' rel='noreferrer'>
                        {eventDetailPrLabel(pr)}
                      </a>
                    ))}
                  </div>
                ) : (
                  <div className='mt-2 text-muted-foreground text-sm'>{t('events.noMatchedPrs')}</div>
                )}
              </div>
              <Accordion type='single' collapsible className='mt-4 rounded-md border border-border px-3'>
                <AccordionItem value='advanced'>
                  <AccordionTrigger>{t('events.advancedData')}</AccordionTrigger>
                  <AccordionContent>
                    <div className='grid gap-2 text-sm'>
                      <DetailItem label={t('events.workspace')} value={selected.workspace_id} mono />
                      <DetailItem label={t('events.toolEvent')} value={selected.tool_event_id || '-'} mono />
                      {isAdmin ? <DetailItem label={t('events.dedupeKey')} value={selected.dedupe_key} mono /> : null}
                      {isAdmin ? <DetailItem label={t('events.source')} value={selected.source_basename} /> : null}
                      {isAdmin ? <DetailItem label={t('events.rawPath')} value={selected.raw_source_path || '-'} mono /> : null}
                      {isAdmin ? <DetailItem label={t('events.rawLocator')} value={selected.raw_source_locator || '-'} mono /> : null}
                    </div>
                    {isAdmin && selected.raw_payload ? (
                      <pre className='mt-3 max-h-56 overflow-auto rounded-md bg-muted p-3 text-xs'>{JSON.stringify(selected.raw_payload, null, 2)}</pre>
                    ) : null}
                  </AccordionContent>
                </AccordionItem>
              </Accordion>
            </div>
          ) : null}
        </DialogContent>
      </Dialog>
    </Page>
  )
}

function DetailItem({ label, value, mono = false }: { label: string; value: React.ReactNode; mono?: boolean }) {
  return (
    <div className='min-w-0 rounded-md border border-border p-3'>
      <div className='text-muted-foreground text-xs'>{label}</div>
      <div className={mono ? 'mt-1 break-all font-mono text-xs' : 'mt-1 truncate font-medium text-sm'}>{value}</div>
    </div>
  )
}
