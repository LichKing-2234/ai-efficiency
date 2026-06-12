import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'user-usage-panel.tsx'), 'utf8')

describe('User usage panel composition', () => {
  test('uses shared row primitives for usage range and refresh controls', () => {
    expect(source).toContain("from '@/components/primitives/toolbar-actions'")
    expect(source).toContain("from '@/components/primitives/button-with-icon'")
    expect(source).toContain("from '@/components/primitives/filter-row'")
    expect(source).toContain('<FilterRow')
    expect(source).toContain("<FilterRow justify='between' gap='lg'>")
    expect(source).toContain('<ToolbarActions>')
    expect(source).toContain('<div />')
    expect(source).toContain("t('command.exportUsageReport')")
    expect(source).not.toContain("<FilterRow className='justify-between gap-3'>")
    expect(source).not.toContain("<div className='flex flex-wrap items-center justify-between gap-3'>")
    expect(source).not.toContain("<div className='flex flex-wrap items-center gap-2'>")
    expect(source).not.toContain("<div className='font-semibold text-sm'>{t('usageDashboard.title')}</div>")
    expect(source).not.toContain("<div className='mt-0.5 text-muted-foreground text-xs'>{t('usageDashboard.subtitle')}</div>")
    expect(source).not.toContain('FilterRowTitle')
  })

  test('uses shared leading-icon CTA buttons for refresh and export controls', () => {
    expect(source).toContain("<ButtonWithIcon size='sm' variant='outline' icon={RefreshCwIcon} disabled={query.isFetching} onClick={() => void query.refetch()}>")
    expect(source).toContain("<ButtonWithIcon size='sm' variant='outline' icon={DownloadIcon}>")
    expect(source).not.toContain("<Button variant='outline' disabled={query.isFetching} onClick={() => void query.refetch()}>")
    expect(source).not.toContain("<Button variant='outline'>")
  })

  test('matches the reference usage analytics card structure', () => {
    const tokenTrendIndex = source.indexOf("title={t('usageDashboard.tokenTrend')}")
    const modelDistributionIndex = source.indexOf("title={t('usageDashboard.modelDistribution')}")
    const costByModelIndex = source.indexOf("title={t('usageDashboard.costByModel')}")
    const firstSplitAfterTrend = source.indexOf("<div className='split-equal'>", tokenTrendIndex)

    expect(tokenTrendIndex).toBeGreaterThan(0)
    expect(modelDistributionIndex).toBeGreaterThan(tokenTrendIndex)
    expect(costByModelIndex).toBeGreaterThan(modelDistributionIndex)
    expect(firstSplitAfterTrend).toBeGreaterThan(tokenTrendIndex)
    expect(modelDistributionIndex).toBeGreaterThan(firstSplitAfterTrend)
    expect(costByModelIndex).toBeGreaterThan(firstSplitAfterTrend)
    expect(source).not.toContain("<div className='px-[18px] pb-4'>")
    expect(source).not.toContain("<div className='px-[18px] pb-[18px]'>")
  })

  test('uses the section header action slot for the token trend legend like the reference', () => {
    expect(source).toContain("actions={<ChartLegend className='justify-end' compact items={tokenKeys} />}")
    expect(source).not.toContain('<ChartLegend items={tokenKeys} />')
  })

  test('uses shared table card content for edge-to-edge model cost table', () => {
    expect(source).toContain("from '@/components/primitives/card-table-content'")
    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).toContain('<CardTableContent>')
    expect(source).not.toContain("<CardContent className='px-0 pb-0'>")
    expect(source).not.toContain('<CardContent>')
  })

  test('uses dedicated cost-by-model copy instead of reusing the model distribution description', () => {
    expect(source).toContain("description={t('usageDashboard.costByModelDescription')}")
    expect(source).not.toContain("title={t('usageDashboard.costByModel')} description={t('usageDashboard.modelDistributionDescription')}")
  })

  test('uses shared data grid cells for model cost numeric columns', () => {
    expect(source).toContain('DataGridCell')
    expect(source).not.toContain("className='tnum text-right'")
    expect(source).not.toContain("className='tnum text-right font-semibold text-foreground'")
  })

  test('uses shared data grid header cells for model cost numeric headers', () => {
    expect(source).toContain('DataGridHeaderCell')
    expect(source).not.toContain("<span className='text-right'>{t('events.requests')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('events.tokens')}</span>")
    expect(source).not.toContain("<span className='text-right'>{t('events.credit')}</span>")
  })

  test('uses shadcn skeleton for the loading placeholder', () => {
    expect(source).toContain("from '@/components/ui/skeleton'")
    expect(source).toContain('<Skeleton')
    expect(source).not.toContain("<div className='text-muted-foreground text-sm'>{t('common.loading')}</div>")
  })

  test('uses shared alert actions for setup CTA spacing', () => {
    expect(source).toContain("from '@/components/primitives/app-alert'")
    expect(source).toContain("from '@/components/primitives/link-action'")
    expect(source).toContain('actions={')
    expect(source).toContain("<LinkAction asChild>")
    expect(source).not.toContain("className='mt-3'")
    expect(source).not.toContain("<Button asChild size='sm'>")
  })

  test('uses shared app alerts for usage dashboard error states', () => {
    expect(source).toContain('<AppAlert')
    expect(source).toContain("tone='error'")
    expect(source).toContain("description={t('usageDashboard.retryHelp')}")
    expect(source).not.toContain("<Alert variant='destructive'>")
    expect(source).not.toContain('<AlertTitle>')
    expect(source).not.toContain('<AlertDescription>')
  })

  test('uses shared page empty states instead of raw empty shells for no-data cards', () => {
    expect(source).toContain("from '@/components/primitives/page-empty'")
    expect(source).toContain('<PageEmpty title={t(\'usageDashboard.noTrendData\')} />')
    expect(source).toContain('<PageEmpty title={t(\'usageDashboard.noModelData\')} />')
    expect(source).not.toContain("from '@/components/ui/empty'")
    expect(source).not.toContain('<Empty><EmptyHeader><EmptyTitle>')
  })

  test('uses the shared KPI grid primitive for usage analytics metrics', () => {
    expect(source).toContain("from '@/components/primitives/kpi-grid'")
    expect(source).toContain('<KpiGrid>')
    expect(source).not.toContain("<div className='kpi-grid'>")
  })

  test('derives KPI helper breakdowns from the selected range totals instead of aggregate stats', () => {
    expect(source).toContain("helper={`${t('usageDashboard.input')}: ${compact(totals.inputTokens, locale)} · ${t('usageDashboard.output')}: ${compact(totals.outputTokens, locale)} · ${t('usageDashboard.cache')}: ${compact(totals.cacheCreationTokens + totals.cacheReadTokens, locale)}`}")
    expect(source).not.toContain("helper={`${t('usageDashboard.input')}: ${compact(stats?.total_input_tokens ?? 0, locale)} · ${t('usageDashboard.output')}: ${compact(stats?.total_output_tokens ?? 0, locale)}`}")
  })
})
