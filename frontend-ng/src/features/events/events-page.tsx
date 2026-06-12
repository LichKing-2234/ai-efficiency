import { useMutation, useQuery } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { ActivityIcon, CoinsIcon, DownloadIcon, ExternalLinkIcon, GitPullRequestIcon, LayersIcon } from 'lucide-react'
import { useState } from 'react'
import { AdvancedDataPanel } from '@/components/primitives/advanced-data-panel'
import { ButtonWithIcon } from '@/components/primitives/button-with-icon'
import { CardFilterBar } from '@/components/primitives/card-filter-bar'
import { CardPagerFooter } from '@/components/primitives/card-pager-footer'
import { CategoryBadge } from '@/components/primitives/category-badge'
import { DataGrid, DataGridCell, DataGridHeader, DataGridHeaderCell, DataGridRow } from '@/components/primitives/data-grid'
import { DetailFieldSection } from '@/components/primitives/detail-field-section'
import { DetailRecordLinksSection } from '@/components/primitives/detail-record-links-section'
import { DetailSummaryStack } from '@/components/primitives/detail-summary-stack'
import { DetailSection } from '@/components/primitives/detail-section'
import { FieldItem } from '@/components/primitives/field-list'
import { FilterRow } from '@/components/primitives/filter-row'
import { FilterSegmentedControl } from '@/components/primitives/filter-segmented-control'
import { FramedTableCard } from '@/components/primitives/framed-table-card'
import { GlyphLabelCell } from '@/components/primitives/glyph-label-cell'
import { InfoTile, InfoTileGrid } from '@/components/primitives/info-tile'
import { KpiGrid } from '@/components/primitives/kpi-grid'
import { LinkedRecordItem, LinkedRecordList } from '@/components/primitives/linked-record-list'
import { KpiCard } from '@/components/primitives/metric-card'
import { OptionList } from '@/components/primitives/option-list'
import { Page } from '@/components/primitives/page'
import { PageEmpty } from '@/components/primitives/page-empty'
import { LoadingState } from '@/components/primitives/data-state'
import { PagerNavButton } from '@/components/primitives/pager-nav-button'
import { PageSizeSelect } from '@/components/primitives/page-size-select'
import { PrimaryActionButton } from '@/components/primitives/primary-action-button'
import { QuietActionButton } from '@/components/primitives/quiet-action-button'
import { SearchField } from '@/components/primitives/search-field'
import { SearchTableWorkbench } from '@/components/primitives/search-table-workbench'
import { SecondaryActionButton } from '@/components/primitives/secondary-action-button'
import { SlideOver } from '@/components/primitives/slide-over'
import { StatusBadge } from '@/components/primitives/status-badge'
import { TokenMeter } from '@/components/primitives/token-meter'
import { TokenBreakdown } from '@/components/primitives/token-breakdown'
import { TextField } from '@/components/primitives/text-field'
import { ToolGlyph } from '@/components/primitives/tool-glyph'
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

  function exportRows() {
    if (typeof window === 'undefined' || rows.length === 0) return
    const header = ['tool', 'repository', 'source', 'session', 'requests', 'credit', 'tokens', 'binding', 'ended']
    const data = rows.map((row) => [
      row.tool,
      row.repo_name || '',
      row.source_basename || '',
      row.tool_session_id || '',
      String(row.request_count),
      String(row.credit_usage),
      String(tokenTotal(row)),
      row.binding_status,
      row.observed_end_at
    ])
    const csv = [header, ...data]
      .map((columns) => columns.map((value) => `"${String(value).replaceAll('"', '""')}"`).join(','))
      .join('\n')
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
    const objectUrl = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = objectUrl
    link.download = 'usage-records.csv'
    link.click()
    window.URL.revokeObjectURL(objectUrl)
  }

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
      <KpiGrid>
        <KpiCard label={t('events.totalEvents')} value={number(summary.data?.total_events)} icon={ActivityIcon} sparkline={rows.map((row) => row.request_count)} />
        <KpiCard label={t('events.boundEvents')} value={number(summary.data?.bound_events)} icon={GitPullRequestIcon} accent sparkline={rows.map((row) => row.binding_status === 'bound' ? 1 : 0)} />
        <KpiCard label={t('events.tokens')} value={compact(totalTokens)} icon={LayersIcon} sparkline={rows.map((row) => tokenTotal(row))} sparklineColor='var(--viz-reason)' />
        <KpiCard label={t('events.credits')} value={number(totalCredit)} icon={CoinsIcon} sparkline={rows.map((row) => row.credit_usage)} sparklineColor='var(--viz-cache)' />
      </KpiGrid>

      <SearchTableWorkbench
        search={(
          <FilterRow className='min-w-0 flex-1'>
            <SearchField
              ariaLabel={t('events.searchRepoSessionSource')}
              clearLabel={t('common.clear')}
              onChange={(q) => setFilters((value) => ({ ...value, q }))}
              onClear={() => setFilters((value) => ({ ...value, q: '' }))}
              placeholder={t('events.searchRepoSessionSource')}
              value={filters.q}
              width='toolbar'
            />
            <FilterSegmentedControl
              ariaLabel={t('events.tool')}
              label={t('events.tool')}
              onChange={(tool) => setFilters((current) => ({ ...current, tool: segmentToFilter(tool) }))}
              options={[{ value: 'all', label: t('events.allTools') }, ...TOOL_OPTIONS.map((tool) => ({ value: tool, label: tool }))]}
              value={filterToSegment(filters.tool)}
            />
            <FilterSegmentedControl
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
            <PrimaryActionButton onClick={applyCurrentFilters}>{t('common.applyFilters')}</PrimaryActionButton>
          </FilterRow>
        )}
        actions={(
          <ButtonWithIcon size='sm' disabled={rows.length === 0} variant='outline' icon={DownloadIcon} onClick={exportRows}>
            {t('command.exportUsageReport')}
          </ButtonWithIcon>
        )}
        searchChildren={(
          <>
            <FilterRow align='start'>
              <TextField
                id='events-filter-from'
                label={t('events.fromTime')}
                type='datetime-local'
                value={filters.from}
                width='datetime'
                onChange={(from) => setFilters((value) => ({ ...value, from }))}
              />
              <TextField
                id='events-filter-to'
                label={t('events.toTime')}
                type='datetime-local'
                value={filters.to}
                width='datetime'
                onChange={(to) => setFilters((value) => ({ ...value, to }))}
              />
              <SecondaryActionButton onClick={clearTimeRange}>{t('events.clearTime')}</SecondaryActionButton>
              {isAdmin ? (
                <>
                  <TextField
                    id='events-user-search'
                    label={t('events.searchUsersByNameOrEmail')}
                    placeholder={t('events.searchUsersByNameOrEmail')}
                    value={userSearch}
                    width='wide'
                    onChange={setUserSearch}
                  />
                  <SecondaryActionButton onClick={searchUsers} disabled={users.isFetching}>{t('adminUsers.searchUsers')}</SecondaryActionButton>
                </>
              ) : null}
              {isAdmin && appliedFilters.userId ? <QuietActionButton onClick={clearSelectedUser}>{t('adminUsers.clearUser', { id: appliedFilters.userId })}</QuietActionButton> : null}
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
          </>
        )}
        footer={(
          <CardPagerFooter
            meta={t('common.pageCount', { current: pagination.currentPage, total: pagination.totalPages })}
            summary={t('events.total', { total: number(total) })}
            previous={(
              <>
                <PageSizeSelect
                  ariaLabel={t('common.pageSizeControl')}
                  labelMode='plain'
                  size='sm'
                  value={appliedFilters.limit}
                  onValueChange={changePageSize}
                />
                <PagerNavButton direction='previous' onClick={previousPage} disabled={!pagination.canGoPrev}>{t('common.previous')}</PagerNavButton>
              </>
            )}
            next={(
              <PagerNavButton direction='next' onClick={nextPage} disabled={!pagination.canGoNext}>{t('common.next')}</PagerNavButton>
            )}
          />
        )}
      >
        <DataGrid minWidth={860} scrollClassName='min-w-0'>
          <DataGridHeader columns={eventColumns}>
            <span />
            <span>{t('events.repository')}</span>
            <span>{t('events.tokens')}</span>
            <DataGridHeaderCell align='right'>{t('events.requests')}</DataGridHeaderCell>
            <DataGridHeaderCell align='right'>{t('events.credit')}</DataGridHeaderCell>
            <span>{t('events.binding')}</span>
            <DataGridHeaderCell align='right'>{t('events.ended')}</DataGridHeaderCell>
          </DataGridHeader>
          {rows.map((row) => (
            <EventRow
              key={row.id}
              maxTokens={maxTokens}
              onSelect={() => detail.mutate(row.id)}
              row={row}
            />
          ))}
          {rows.length === 0 ? <PageEmpty title={t('events.noFilteredEvents')} /> : null}
        </DataGrid>
      </SearchTableWorkbench>

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
      <GlyphLabelCell description={row.source_basename || row.tool_session_id} glyphTool={row.tool} truncate>
        {row.repo_name || t('events.unlinked')}
      </GlyphLabelCell>
      <TokenMeter label={compact(tokens)} max={maxTokens} value={tokens} />
      <DataGridCell align='right' numeric tone='muted'>{number(row.request_count)}</DataGridCell>
      <DataGridCell align='right' emphasis numeric>{number(row.credit_usage)}</DataGridCell>
      <span><StatusBadge value={row.binding_status} /></span>
      <DataGridCell align='right' tone='subtle'>{dateTime(row.observed_end_at)}</DataGridCell>
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
        <DetailSummaryStack
          statuses={(
            <>
              <CategoryBadge variant='ai'>{event.tool}</CategoryBadge>
              <StatusBadge value={event.binding_status} />
              <CategoryBadge>{number(event.context_usage_pct)}% {t('events.context')}</CategoryBadge>
            </>
          )}
          metrics={(
            <>
              <InfoTile label={t('events.tokens')} value={compact(tokens)} compact numeric />
              <InfoTile label={t('events.requests')} value={number(event.request_count)} compact numeric />
              <InfoTile label={t('events.credit')} value={number(event.credit_usage)} accent='ai' compact numeric />
            </>
          )}
        >
          <DetailSection title={t('events.tokenBreakdown')}>
            <TokenBreakdown items={tokenBreakdown} valueFormatter={number} />
          </DetailSection>

          <DetailFieldSection title={t('events.session')}>
            <FieldItem label={t('events.observedEnd')} value={dateTime(event.observed_end_at)} />
            <FieldItem label={t('repoDetail.commit')} value={event.commit_sha || '-'} mono />
            <FieldItem label={t('events.source')} value={event.source_basename} mono />
            <FieldItem label={t('events.session')} value={event.tool_session_id || t('events.noToolSessionId')} mono />
            <FieldItem label={t('events.workspace')} value={event.workspace_id} mono />
            <FieldItem label={t('events.observedStart')} value={dateTime(event.observed_start_at)} />
          </DetailFieldSection>

          <DetailRecordLinksSection emptyTitle={t('events.noMatchedPrs')} title={t('events.matchedPrs')}>
            {event.matched_prs.map((pr) => (
              <LinkedRecordItem
                description={pr.status}
                href={pr.scm_pr_url}
                icon={<GitPullRequestIcon />}
                key={pr.pr_record_id}
                label={`#${pr.scm_pr_id} ${pr.title}`}
                trailing={<ExternalLinkIcon className='size-3.5' />}
                variant='plain'
              />
            ))}
          </DetailRecordLinksSection>

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
        </DetailSummaryStack>
      ) : null}
    </SlideOver>
  )
}
