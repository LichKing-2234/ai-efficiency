import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { MetricCard } from '@/components/primitives/metric-card'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { compact, dateTime, number, tokenTotal } from '@/lib/format'
import type { ToolUsageEventDetail, ToolUsageEventUserOption } from '@/lib/api/types'
import { buildEventQuery, buildEventSearch, defaultEventFilters, eventDetailPrLabel, eventFiltersForRole, getEventPagination, type EventFilterState } from './event-filters'

export function EventsPage() {
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
      <PageHeader title='Usage Records' description='Tool usage events from backend attribution. User names are shown only when backend returns them for the current role.' />
      <div className='grid gap-4 sm:grid-cols-4'>
        <MetricCard label='Total events' value={number(summary.data?.total_events)} />
        <MetricCard label='Bound events' value={number(summary.data?.bound_events)} accent />
        <MetricCard label='Unbound events' value={number(summary.data?.unbound_events)} />
        <MetricCard label='Tools' value={number(summary.data?.tool_counts?.length ?? 0)} />
      </div>
      <Card>
        <CardHeader>
          <CardTitle>Filters</CardTitle>
        </CardHeader>
        <CardContent className='flex flex-col gap-3'>
          <div className='grid gap-2 lg:grid-cols-[minmax(0,1fr)_minmax(0,1fr)_140px_150px_minmax(0,1.5fr)]'>
            <Input type='datetime-local' value={filters.from} onChange={(event) => setFilters((value) => ({ ...value, from: event.target.value }))} />
            <Input type='datetime-local' value={filters.to} onChange={(event) => setFilters((value) => ({ ...value, to: event.target.value }))} />
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={filters.tool} onChange={(event) => setFilters((value) => ({ ...value, tool: event.target.value }))}>
              <option value=''>All tools</option>
              <option value='claude'>Claude</option>
              <option value='codex'>Codex</option>
              <option value='kiro'>Kiro</option>
            </select>
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={filters.bindingStatus} onChange={(event) => setFilters((value) => ({ ...value, bindingStatus: event.target.value }))}>
              <option value=''>All code links</option>
              <option value='bound'>Bound</option>
              <option value='unbound'>Unbound</option>
            </select>
            <Input placeholder='Search repo, session, source' value={filters.q} onChange={(event) => setFilters((value) => ({ ...value, q: event.target.value }))} />
          </div>
          <div className='flex flex-wrap items-center gap-2'>
            {isAdmin ? (
              <>
                <Input className='w-72' placeholder='Search users by name or email' value={userSearch} onChange={(event) => setUserSearch(event.target.value)} />
                <Button variant='outline' onClick={searchUsers} disabled={users.isFetching}>Search users</Button>
              </>
            ) : null}
            {isAdmin && appliedFilters.userId ? <Button variant='ghost' onClick={clearSelectedUser}>Clear user #{appliedFilters.userId}</Button> : null}
            <div className='ml-auto flex gap-2'>
              <Button variant='outline' onClick={clearTimeRange}>Clear time</Button>
              <Button onClick={applyCurrentFilters}>Apply filters</Button>
            </div>
          </div>
          {userOptions.length > 0 ? (
            <div className='flex flex-col gap-1 rounded-md border border-border bg-card p-2'>
              {userOptions.map((user) => (
                <button key={user.id} className='rounded-sm px-2 py-1 text-left text-sm hover:bg-muted' type='button' onClick={() => selectUser(user)}>
                  <span className='font-medium'>{user.email || user.username}</span>
                  <span className='ml-2 text-muted-foreground text-xs'>{user.role} · {number(user.event_count)} events</span>
                </button>
              ))}
            </div>
          ) : null}
        </CardContent>
      </Card>
      <Card className='overflow-hidden'>
        <CardHeader>
          <div className='flex flex-wrap items-center justify-between gap-2'>
            <CardTitle>Recent usage</CardTitle>
            <div className='flex items-center gap-2 text-muted-foreground text-xs'>
              <span>{number(total)} total</span>
              <select className='h-7 rounded-md border border-input bg-card px-2' value={appliedFilters.limit} onChange={(event) => changePageSize(Number(event.target.value))}>
                <option value={20}>20</option>
                <option value={50}>50</option>
                <option value={100}>100</option>
              </select>
              <Button size='sm' variant='outline' onClick={previousPage} disabled={!pagination.canGoPrev}>Prev</Button>
              <span>Page {pagination.currentPage} / {pagination.totalPages}</span>
              <Button size='sm' variant='outline' onClick={nextPage} disabled={!pagination.canGoNext}>Next</Button>
            </div>
          </div>
        </CardHeader>
        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Tool</TableHead>
                <TableHead>Repository</TableHead>
                <TableHead>Requests</TableHead>
                <TableHead>Tokens</TableHead>
                <TableHead>Credit</TableHead>
                <TableHead>Binding</TableHead>
                <TableHead className='text-right'>Ended</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {(list.data?.items ?? []).map((row) => (
                <TableRow key={row.id} className='cursor-pointer' onClick={() => detail.mutate(row.id)}>
                  <TableCell><Badge variant='ai'>{row.tool}</Badge></TableCell>
                  <TableCell>
                    <div className='font-medium text-foreground'>{row.repo_name || 'Unlinked'}</div>
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
            <DialogTitle>Usage event detail</DialogTitle>
            <DialogDescription>{selected?.tool_session_id || 'No tool session id'}</DialogDescription>
          </DialogHeader>
          {selected ? (
            <div className='max-h-[70vh] overflow-y-auto pr-1'>
              <div className='grid gap-3 sm:grid-cols-2'>
                <DetailItem label='Tool' value={selected.tool} />
                <DetailItem label='Repository' value={selected.repo_name || '-'} />
                <DetailItem label='Observed start' value={dateTime(selected.observed_start_at)} />
                <DetailItem label='Observed end' value={dateTime(selected.observed_end_at)} />
                {isAdmin ? <DetailItem label='User' value={selected.username || `User #${selected.user_id}`} /> : null}
                <DetailItem label='Context' value={`${number(selected.context_usage_pct)}%`} />
              </div>
              <div className='mt-4 grid gap-3 sm:grid-cols-3'>
                <MetricCard label='Input' value={compact(selected.input_tokens)} />
                <MetricCard label='Output' value={compact(selected.output_tokens)} />
                <MetricCard label='Cache' value={compact(selected.cached_input_tokens)} />
                <MetricCard label='Reasoning' value={compact(selected.reasoning_tokens)} />
                <MetricCard label='Credits' value={number(selected.credit_usage)} accent />
                <MetricCard label='Requests' value={number(selected.request_count)} />
              </div>
              <div className='mt-4 rounded-md border border-border p-3'>
                <div className='flex flex-wrap items-center gap-2'>
                  <StatusBadge value={selected.binding_status} />
                  <span className='text-muted-foreground text-sm'>checkpoint {dateTime(selected.checkpoint_captured_at)}</span>
                </div>
                <div className='mt-2 break-all font-mono text-xs'>{selected.commit_sha || '-'}</div>
              </div>
              <div className='mt-4'>
                <div className='font-medium text-sm'>Matched PRs</div>
                {selected.matched_prs.length > 0 ? (
                  <div className='mt-2 flex flex-col gap-2'>
                    {selected.matched_prs.map((pr) => (
                      <a key={pr.pr_record_id} className='rounded-md border border-border p-3 text-sm hover:bg-muted' href={pr.scm_pr_url} target='_blank' rel='noreferrer'>
                        {eventDetailPrLabel(pr)}
                      </a>
                    ))}
                  </div>
                ) : (
                  <div className='mt-2 text-muted-foreground text-sm'>No matched PRs.</div>
                )}
              </div>
              <details className='mt-4 rounded-md border border-border p-3'>
                <summary className='cursor-pointer font-medium text-sm'>Advanced data</summary>
                <div className='mt-3 grid gap-2 text-sm'>
                  <DetailItem label='Workspace' value={selected.workspace_id} mono />
                  <DetailItem label='Tool event' value={selected.tool_event_id || '-'} mono />
                  {isAdmin ? <DetailItem label='Dedupe key' value={selected.dedupe_key} mono /> : null}
                  {isAdmin ? <DetailItem label='Source' value={selected.source_basename} /> : null}
                  {isAdmin ? <DetailItem label='Raw path' value={selected.raw_source_path || '-'} mono /> : null}
                  {isAdmin ? <DetailItem label='Raw locator' value={selected.raw_source_locator || '-'} mono /> : null}
                </div>
                {isAdmin && selected.raw_payload ? (
                  <pre className='mt-3 max-h-56 overflow-auto rounded-md bg-muted p-3 text-xs'>{JSON.stringify(selected.raw_payload, null, 2)}</pre>
                ) : null}
              </details>
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
