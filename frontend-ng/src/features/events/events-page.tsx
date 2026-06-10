import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { ActivityIcon, CoinsIcon, GitPullRequestIcon, LayersIcon } from 'lucide-react'
import { useState } from 'react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { AdvancedDataPanel } from '@/components/primitives/advanced-data-panel'
import { CardFilterBar } from '@/components/primitives/card-filter-bar'
import { CardPagerFooter } from '@/components/primitives/card-pager-footer'
import { DataGrid, DataGridHeader, DataGridRow } from '@/components/primitives/data-grid'
import { FieldItem, FieldList } from '@/components/primitives/field-list'
import { FilterRow } from '@/components/primitives/filter-row'
import { InfoTile, InfoTileGrid } from '@/components/primitives/info-tile'
import { LabeledSegmentedControl } from '@/components/primitives/labeled-segmented-control'
import { LinkedRecordItem, LinkedRecordList } from '@/components/primitives/linked-record-list'
import { MetricCard } from '@/components/primitives/metric-card'
import { OptionList } from '@/components/primitives/option-list'
import { Page } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { SearchField } from '@/components/primitives/search-field'
import { SectionEyebrow } from '@/components/primitives/section-eyebrow'
import { SlideOver } from '@/components/primitives/slide-over'
import { SlideOverStack } from '@/components/primitives/slide-over-stack'
import { TokenMeter } from '@/components/primitives/token-meter'
import { TokenBreakdown } from '@/components/primitives/token-breakdown'
import { TextField } from '@/components/primitives/text-field'
import { ToolGlyph } from '@/components/primitives/tool-glyph'
import { ToolbarSelect } from '@/components/primitives/toolbar-select'
import { api } from '@/lib/api'
import { compact, dateTime, number, tokenTotal } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import type { ToolUsageEventDetail, ToolUsageEventRow, ToolUsageEventUserOption } from '@/lib/api/types'
import { buildEventQuery, buildEventSearch, defaultEventFilters, eventDetailPrLabel, eventFiltersForRole, filterToSegment, getEventPagination, segmentToFilter, type EventFilterState } from './event-filters'

const TOOL_OPTIONS = ['claude', 'codex', 'kiro'] as const
const eventColumns = '26px_1.7fr_1.3fr_0.8fr_0.9fr_0.7fr_0.9fr'

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
  const rows = list.data?.items ?? []
  const totalTokens = rows.reduce((sum, row) => sum + tokenTotal(row), 0)
  const totalCredit = rows.reduce((sum, row) => sum + row.credit_usage, 0)
  const maxTokens = Math.max(...rows.map((row) => tokenTotal(row)), 1)
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
    <Page className='stagger'>
      <div className='kpi-grid'>
        <MetricCard label={t('events.totalEvents')} value={number(summary.data?.total_events)} icon={ActivityIcon} sparkline={rows.map((row) => row.request_count)} />
        <MetricCard label={t('events.boundEvents')} value={number(summary.data?.bound_events)} icon={GitPullRequestIcon} accent sparkline={rows.map((row) => row.binding_status === 'bound' ? 1 : 0)} />
        <MetricCard label={t('events.tokens')} value={compact(totalTokens)} icon={LayersIcon} sparkline={rows.map((row) => tokenTotal(row))} sparklineColor='var(--viz-reason)' />
        <MetricCard label={t('events.credits')} value={number(totalCredit)} icon={CoinsIcon} sparkline={rows.map((row) => row.credit_usage)} sparklineColor='var(--viz-cache)' />
      </div>

      <Card>
        <CardFilterBar stacked>
          <FilterRow>
            <SearchField
              ariaLabel={t('events.searchRepoSessionSource')}
              className='min-w-[260px] flex-1'
              clearLabel={t('common.clear')}
              onChange={(q) => setFilters((value) => ({ ...value, q }))}
              onClear={() => setFilters((value) => ({ ...value, q: '' }))}
              placeholder={t('events.searchRepoSessionSource')}
              value={filters.q}
            />
            <LabeledSegmentedControl
              ariaLabel={t('events.tool')}
              label={t('events.tool')}
              onChange={(tool) => setFilters((current) => ({ ...current, tool: segmentToFilter(tool) }))}
              options={[{ value: 'all', label: t('events.allTools') }, ...TOOL_OPTIONS.map((tool) => ({ value: tool, label: tool }))]}
              value={filterToSegment(filters.tool)}
            />
            <LabeledSegmentedControl
              ariaLabel={t('events.binding')}
              label={t('events.binding')}
              onChange={(bindingStatus) => setFilters((current) => ({ ...current, bindingStatus: segmentToFilter(bindingStatus) }))}
              options={[
                { value: 'all', label: t('events.allCodeLinks') },
                { value: 'bound', label: t('repos.bound') },
                { value: 'unbound', label: t('repos.unbound') }
              ]}
              value={filterToSegment(filters.bindingStatus)}
            />
            <Button onClick={applyCurrentFilters}>{t('common.applyFilters')}</Button>
          </FilterRow>
          <FilterRow>
            <TextField
              className='w-[220px]'
              id='events-filter-from'
              label={t('events.fromTime')}
              type='datetime-local'
              value={filters.from}
              onChange={(from) => setFilters((value) => ({ ...value, from }))}
            />
            <TextField
              className='w-[220px]'
              id='events-filter-to'
              label={t('events.toTime')}
              type='datetime-local'
              value={filters.to}
              onChange={(to) => setFilters((value) => ({ ...value, to }))}
            />
            <Button variant='outline' onClick={clearTimeRange}>{t('events.clearTime')}</Button>
            {isAdmin ? (
              <>
                <TextField
                  className='w-72'
                  id='events-user-search'
                  label={t('events.searchUsersByNameOrEmail')}
                  placeholder={t('events.searchUsersByNameOrEmail')}
                  value={userSearch}
                  onChange={setUserSearch}
                />
                <Button variant='outline' onClick={searchUsers} disabled={users.isFetching}>{t('adminUsers.searchUsers')}</Button>
              </>
            ) : null}
            {isAdmin && appliedFilters.userId ? <Button variant='ghost' onClick={clearSelectedUser}>{t('adminUsers.clearUser', { id: appliedFilters.userId })}</Button> : null}
          </FilterRow>
          {userOptions.length > 0 ? (
            <OptionList
              ariaLabel={t('events.searchUsersByNameOrEmail')}
              items={userOptions.map((user) => ({
                id: user.id,
                label: user.email || user.username,
                description: `${user.role} · ${number(user.event_count)}`
              }))}
              onSelect={(item) => {
                const user = userOptions.find((option) => option.id === item.id)
                if (user) selectUser(user)
              }}
            />
          ) : null}
        </CardFilterBar>
      </Card>

      <Card className='overflow-hidden'>
        <DataGrid minWidth={860} scrollClassName='min-w-0'>
          <DataGridHeader columns={eventColumns}>
            <span />
            <span>{t('events.repository')}</span>
            <span>{t('events.tokens')}</span>
            <span className='text-right'>{t('events.requests')}</span>
            <span className='text-right'>{t('events.credit')}</span>
            <span>{t('events.binding')}</span>
            <span className='text-right'>{t('events.ended')}</span>
          </DataGridHeader>
          {rows.map((row) => (
            <EventRow
              key={row.id}
              maxTokens={maxTokens}
              onSelect={() => detail.mutate(row.id)}
              row={row}
            />
          ))}
          {rows.length === 0 ? <div className='px-6 py-10 text-center text-muted-foreground text-sm'>{t('common.empty')}</div> : null}
        </DataGrid>
        <CardPagerFooter
          className='border-border border-t p-3'
          summary={t('events.total', { total: number(total) })}
          previous={(
            <>
              <ToolbarSelect
                ariaLabel={t('common.pageSizeControl')}
                size='sm'
                options={[
                  { value: '20', label: '20' },
                  { value: '50', label: '50' },
                  { value: '100', label: '100' }
                ]}
                value={String(appliedFilters.limit)}
                onValueChange={(value) => changePageSize(Number(value))}
              />
              <Button size='sm' variant='outline' onClick={previousPage} disabled={!pagination.canGoPrev}>{t('common.previous')}</Button>
            </>
          )}
          next={(
            <>
              <span className='text-muted-foreground text-xs'>{t('common.pageCount', { current: pagination.currentPage, total: pagination.totalPages })}</span>
              <Button size='sm' variant='outline' onClick={nextPage} disabled={!pagination.canGoNext}>{t('common.next')}</Button>
            </>
          )}
        />
      </Card>

      <EventDetail event={selected} isAdmin={isAdmin} onClose={() => setSelected(null)} />
    </Page>
  )
}

function EventRow({ row, maxTokens, onSelect }: { row: ToolUsageEventRow; maxTokens: number; onSelect: () => void }) {
  const { t } = useI18n()
  const tokens = tokenTotal(row)
  return (
    <DataGridRow
      as='button'
      columns={eventColumns}
      onClick={onSelect}
    >
      <ToolGlyph tool={row.tool} />
      <span className='min-w-0'>
        <span className='block truncate font-medium text-foreground text-sm'>{row.repo_name || t('events.unlinked')}</span>
        <span className='mono block truncate text-[11px] text-[var(--ink-4)]'>{row.source_basename || row.tool_session_id}</span>
      </span>
      <TokenMeter label={compact(tokens)} max={maxTokens} value={tokens} />
      <span className='tnum text-right text-[var(--ink-2)]'>{number(row.request_count)}</span>
      <span className='tnum text-right font-semibold text-foreground'>{number(row.credit_usage)}</span>
      <span><Badge variant={row.binding_status === 'bound' ? 'pos' : 'warn'}>{row.binding_status}</Badge></span>
      <span className='text-right text-[var(--ink-3)] text-xs'>{dateTime(row.observed_end_at)}</span>
    </DataGridRow>
  )
}

function EventDetail({ event, isAdmin, onClose }: { event: ToolUsageEventDetail | null; isAdmin: boolean; onClose: () => void }) {
  const { t } = useI18n()
  const tokens = event ? tokenTotal(event) : 0
  const tokenBreakdown = event ? [
    { label: t('usageDashboard.input'), value: event.input_tokens, color: 'var(--viz-input)' },
    { label: t('usageDashboard.output'), value: event.output_tokens, color: 'var(--viz-output)' },
    { label: t('events.cache'), value: event.cached_input_tokens, color: 'var(--viz-cache)' },
    { label: t('events.reasoning'), value: event.reasoning_tokens, color: 'var(--viz-reason)' }
  ] : []

  return (
    <SlideOver
      leading={event ? <ToolGlyph tool={event.tool} size={34} /> : null}
      onClose={onClose}
      open={!!event}
      subtitle={event?.tool_session_id || t('events.noToolSessionId')}
      title={event?.repo_name || t('events.unlinked')}
    >
      {event ? (
        <SlideOverStack>
          <FilterRow align='start'>
            <Badge variant='ai'>{event.tool}</Badge>
            <Badge variant={event.binding_status === 'bound' ? 'pos' : 'warn'}>{event.binding_status}</Badge>
            <Badge variant='neutral'>{number(event.context_usage_pct)}% {t('events.context')}</Badge>
          </FilterRow>

          <InfoTileGrid columns={3}>
            <InfoTile label={t('events.tokens')} value={compact(tokens)} compact numeric />
            <InfoTile label={t('events.requests')} value={number(event.request_count)} compact numeric />
            <InfoTile label={t('events.credit')} value={number(event.credit_usage)} accent='ai' compact numeric />
          </InfoTileGrid>

          <section>
            <SectionEyebrow>{t('events.tokenBreakdown')}</SectionEyebrow>
            <TokenBreakdown items={tokenBreakdown} valueFormatter={number} />
          </section>

          <section>
            <SectionEyebrow>{t('events.session')}</SectionEyebrow>
            <FieldList>
              <FieldItem label={t('events.observedStart')} value={dateTime(event.observed_start_at)} />
              <FieldItem label={t('events.observedEnd')} value={dateTime(event.observed_end_at)} />
              <FieldItem label={t('repoDetail.commit')} value={event.commit_sha || '-'} mono />
              <FieldItem label={t('events.source')} value={event.source_basename} mono />
              <FieldItem label={t('events.workspace')} value={event.workspace_id} mono />
            </FieldList>
          </section>

          <section>
            <SectionEyebrow>{t('events.matchedPrs')}</SectionEyebrow>
            {event.matched_prs.length > 0 ? (
              <LinkedRecordList>
                {event.matched_prs.map((pr) => (
                  <LinkedRecordItem href={pr.scm_pr_url} icon={<GitPullRequestIcon />} key={pr.pr_record_id} label={eventDetailPrLabel(pr)} />
                ))}
              </LinkedRecordList>
            ) : (
              <div className='text-muted-foreground text-sm'>{t('events.noMatchedPrs')}</div>
            )}
          </section>

          <AdvancedDataPanel
            code={isAdmin && event.raw_payload ? JSON.stringify(event.raw_payload, null, 2) : null}
            codeAriaLabel={t('events.rawPayload')}
            fields={[
              { label: t('events.toolEvent'), value: event.tool_event_id || '-', mono: true },
              ...(isAdmin ? [
                { label: t('events.user'), value: event.username || `#${event.user_id}` },
                { label: t('events.dedupeKey'), value: event.dedupe_key, mono: true },
                { label: t('events.rawPath'), value: event.raw_source_path || '-', mono: true },
                { label: t('events.rawLocator'), value: event.raw_source_locator || '-', mono: true }
              ] : [])
            ]}
            title={t('events.advancedData')}
          />
        </SlideOverStack>
      ) : null}
    </SlideOver>
  )
}
