import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'repo-detail-page.tsx'), 'utf8')

describe('Repo detail page composition', () => {
  test('uses shared linked record items for pull request links', () => {
    expect(source).toContain("from '@/components/primitives/linked-record-list'")
    expect(source).toContain('<LinkedRecordItem')
    expect(source).toContain("variant='plain'")
    expect(source).not.toContain("<a className='flex min-w-0 items-center gap-2 font-semibold")
    expect(source).not.toContain("className='border-0 bg-transparent p-0 hover:bg-transparent'")
    expect(source).not.toContain("<span className='min-w-0'>")
  })

  test('uses shared category badges for AI PR ratio pills', () => {
    expect(source).toContain("from '@/components/primitives/category-badge'")
    expect(source).toContain("<CategoryBadge variant='ai'>{pr.ai_label} · {percent(pr.ai_ratio)}</CategoryBadge>")
    expect(source).not.toContain("<Badge variant='ai'>{pr.ai_label} · {percent(pr.ai_ratio)}</Badge>")
  })

  test('uses the shared KPI grid utility for repository detail metrics', () => {
    expect(source).toContain("from '@/components/primitives/kpi-grid'")
    expect(source).toContain('<KpiGrid>')
    expect(source).not.toContain("<div className='kpi-grid'>")
    expect(source).not.toContain("<div className='grid gap-4 sm:grid-cols-4'>")
  })

  test('uses shared filter rows for pull request range controls', () => {
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain("from '@/components/primitives/filter-row-title'")
    expect(source).toContain("<FilterRow tone='label'>")
    expect(source).toContain("<FilterRowTitle title={t('repoDetail.mergedIn')} variant='label' />")
    expect(source).not.toContain("<FilterRow className='text-sm'>")
    expect(source).not.toContain("<div className='flex flex-wrap items-center gap-2 text-sm'>")
    expect(source).not.toContain("<span className='text-muted-foreground'>{t('repoDetail.mergedIn')}</span>")
  })

  test('uses semantic select sizing for SCM binding controls', () => {
    expect(source).toContain("from '@/components/primitives/quiet-action-button'")
    expect(source).toContain("width='full'")
    expect(source).toContain("<QuietActionButton onClick={() => {")
    expect(source).not.toContain("className='w-full'")
    expect(source).not.toContain("<Button variant='ghost' onClick={() => {")
  })

  test('uses the shared page-size select for pull request pager sizing', () => {
    expect(source).toContain("from '@/components/primitives/page-size-select'")
    expect(source).toContain("<PageSizeSelect")
    expect(source).toContain("ariaLabel={t('common.pageSizeControl')}")
    expect(source).toContain('sizes={[10, 25, 50]}')
    expect(source).toContain("tPageSize={(size) => t('common.pageSize', { size })}")
    expect(source).not.toContain("<ToolbarSelect\n                ariaLabel={t('common.pageSizeControl')}")
  })

  test('uses shared leading-icon CTA buttons for sync and binding actions', () => {
    expect(source).toContain("from '@/components/primitives/button-with-icon'")
    expect(source).toContain('<ButtonWithIcon')
    expect(source).toContain("icon={RefreshCw}")
    expect(source).toContain("icon={Save}")
    expect(source).not.toContain("<Button onClick={() => sync.mutate()} disabled={!canSync}><RefreshCw data-icon='inline-start' />")
    expect(source).not.toContain("<Button variant='outline' onClick={() => saveBinding.mutate(selectedProviderId)} disabled={saveBinding.isPending}><Save data-icon='inline-start' />")
  })

  test('uses shared card content stacks for scm binding card bodies', () => {
    expect(source).toContain("from '@/components/primitives/section-card'")
    expect(source).toContain('<SectionCard')
    expect(source).toContain("from '@/components/primitives/framed-table-card'")
    expect(source).toContain('<FramedTableCard')
    expect(source).not.toContain("from '@/components/primitives/section-card-header'")
    expect(source).not.toContain('<FramedCard>')
    expect(source).not.toContain('<CardContent>')
  })

  test('uses a shared latest-sync summary shell instead of page-local metric grid composition', () => {
    expect(source).toContain("from '@/components/primitives/summary-metrics-panel'")
    expect(source).toContain('<SummaryMetricsPanel')
    expect(source).not.toContain("<SectionCard title={t('repoDetail.latestSyncJob')}>\n            <InfoTileGrid columns={4}>")
  })

  test('uses shared stacks for repair and expanded detail vertical rhythm', () => {
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).not.toContain("<div className='flex flex-col gap-3'>")
    expect(source).not.toContain("<div className='flex flex-col gap-4'>")
  })

  test('uses shared form action rows for webhook repair and PR actions', () => {
    expect(source).toContain("from '@/components/primitives/form-actions'")
    expect(source).toContain("from '@/components/primitives/primary-action-button'")
    expect(source).toContain("from '@/components/primitives/repo-pr-actions'")
    expect(source).toContain("<FormActions align='start'>")
    expect(source).toContain('<PrimaryActionButton')
    expect(source).toContain('<RepoPrActions')
    expect(source).not.toContain("<ActionGroup align='start'>")
    expect(source).not.toContain('<ActionGroup>')
    expect(source).not.toContain("className='w-fit'")
    expect(source).not.toContain('<Button disabled={repairWebhook.isPending}')
    expect(source).not.toContain("<Button variant='ghost' size='sm' onClick={() => setExpandedPRId(expanded ? null : pr.id)}")
    expect(source).not.toContain("<Button variant='outline' size='sm' onClick={() => refreshUsage.mutate(pr.id)}")
  })

  test('uses shared app alerts for webhook repair guidance and notices', () => {
    expect(source).toContain("from '@/components/primitives/app-alert'")
    expect(source).toContain('<AppAlert')
    expect(source).toContain("tone='warning'")
    expect(source).not.toContain("from '@/components/ui/alert'")
    expect(source).not.toContain('<Alert>')
    expect(source).not.toContain('<AlertTitle>')
    expect(source).not.toContain('<AlertDescription>')
  })

  test('uses the shared inset panel flush variant for expanded pull request details', () => {
    expect(source).toContain('<InsetPanel flush>')
    expect(source).not.toContain("className='rounded-none border-x-0 border-t-0 p-4'")
  })

  test('uses the shared inset panel compact variant for sync status notes', () => {
    expect(source).toContain("from '@/components/primitives/muted-inset-note'")
    expect(source).toContain("<MutedInsetNote compact>")
    expect(source).not.toContain("className='px-3 py-2'")
  })

  test('uses tightened shared usage summary treatment for expanded pull request metrics', () => {
    expect(source).toContain('<UsageSummaryPanel')
    expect(source).toContain("summary={t('repoDetail.totalTokensRefreshed'")
    expect(source).not.toContain("className='mt-4 flex flex-wrap items-center justify-between gap-3 text-sm'")
  })

  test('uses shared status-with-reason rows for PR and snapshot usage states', () => {
    expect(source).toContain("from '@/components/primitives/status-with-reason'")
    expect(source.match(/<StatusWithReason/g)?.length).toBe(2)
    expect(source).toContain("label={usageStatusLabel(pr.usage_status || pr.attribution_status)}")
    expect(source).toContain("label={usageStatusLabel(freshness?.usage_status)}")
    expect(source).toContain("reason={usageStatusReason(pr.usage_status || pr.attribution_status, pr.usage_status_reason)}")
    expect(source).toContain("reason={usageStatusReason(freshness?.usage_status, freshness?.usage_status_reason)}")
    expect(source).not.toContain("<div className='flex flex-col gap-1'>")
    expect(source).not.toContain("<span className='flex min-w-0 flex-col gap-1'>")
  })

  test('uses shared data grid cells for PR and snapshot numeric metadata', () => {
    expect(source).toContain('DataGridCell')
    expect(source).not.toContain("className='tnum'>{compact((pr.usage_input_tokens")
    expect(source).not.toContain("className='tnum'>{number(pr.cycle_time_hours)}h")
    expect(source).not.toContain("className='tnum text-muted-foreground text-xs'")
    expect(source).not.toContain("className='mono min-w-0 truncate text-xs'")
    expect(source).not.toContain("className='tnum text-right'")
  })

  test('uses shared data grid header cells for snapshot numeric headers', () => {
    expect(source).toContain('DataGridHeaderCell')
    expect(source).not.toContain("<span className='text-right'>{t('repoDetail.input')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('repoDetail.output')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('repoDetail.cache')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('repoDetail.reasoning')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('repoDetail.credits')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('repoDetail.requests')}</span>")
  })

  test('uses shared data grid status rows for expanded PR loading and empty snapshot states', () => {
    expect(source).toContain('DataGridStatusRow')
    expect(source).not.toContain("className='py-4 text-center text-muted-foreground text-sm'")
    expect(source).not.toContain("className='justify-center py-6 text-center text-muted-foreground text-sm'")
  })

  test('uses the shared pager navigation button for pull request pagination', () => {
    expect(source).toContain("from '@/components/primitives/pager-nav-button'")
    expect(source).toContain("<PagerNavButton direction='previous' onClick={() => setPRsPage((value) => Math.max(0, value - 1))} disabled={!hasPreviousPage || prs.isFetching}>")
    expect(source).toContain("<PagerNavButton direction='next' onClick={() => setPRsPage((value) => value + 1)} disabled={!hasNextPage || prs.isFetching}>")
    expect(source).not.toContain("<Button variant='outline' size='sm' onClick={() => setPRsPage((value) => Math.max(0, value - 1))} disabled={!hasPreviousPage || prs.isFetching}>")
    expect(source).not.toContain("<Button variant='outline' size='sm' onClick={() => setPRsPage((value) => value + 1)} disabled={!hasNextPage || prs.isFetching}>")
  })
})
