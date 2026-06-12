import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { ActivityIcon, CoinsIcon, DownloadIcon, GaugeIcon, LayersIcon, RefreshCwIcon } from 'lucide-react'
import { Card } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { AppAlert } from '@/components/primitives/app-alert'
import { ButtonWithIcon } from '@/components/primitives/button-with-icon'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { CardTableContent } from '@/components/primitives/card-table-content'
import { ChartLegend } from '@/components/primitives/chart-legend'
import { BarsH, StackedAreaChart, type StackedAreaKey } from '@/components/primitives/charts'
import { DataGrid, DataGridCell, DataGridHeader, DataGridHeaderCell, DataGridRow } from '@/components/primitives/data-grid'
import { FilterRow } from '@/components/primitives/filter-row'
import { HeatmapGrid } from '@/components/primitives/heatmap-grid'
import { KpiGrid } from '@/components/primitives/kpi-grid'
import { LinkAction } from '@/components/primitives/link-action'
import { KpiCard } from '@/components/primitives/metric-card'
import { PageEmpty } from '@/components/primitives/page-empty'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { SegmentedControl } from '@/components/primitives/segmented-control'
import { Stack } from '@/components/primitives/stack'
import { ToolbarActions } from '@/components/primitives/toolbar-actions'
import { api } from '@/lib/api'
import type { UserUsageTrendPoint } from '@/lib/api/types'
import { compact, currency, durationMs, number } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import { buildUsageDashboardParams, buildUsageHeatmapPoints, rangeLabelKey, usageTotalsFromTrend, type UsageRangeOption } from './user-usage-state'

const modelColumns = '1.6fr_1fr_1fr_0.9fr'

export function UserUsagePanel({ embedded = false }: { embedded?: boolean }) {
  const { locale, t } = useI18n()
  const [range, setRange] = useState<UsageRangeOption>('30d')
  const query = useQuery({
    queryKey: ['user-usage-dashboard', range],
    queryFn: () => api.userUsage.dashboard(buildUsageDashboardParams(range))
  })
  const snapshot = query.data
  const rangeLabel = t(rangeLabelKey(range))
  const totals = usageTotalsFromTrend(snapshot?.trend ?? [])
  const stats = snapshot?.stats
  const heatmapPoints = buildUsageHeatmapPoints(snapshot?.trend ?? [], snapshot?.range.granularity)

  const spark = snapshot?.trend.map((point) => point.total_tokens).filter(Boolean) ?? []
  const tokenKeys: Array<StackedAreaKey<UserUsageTrendPoint>> = [
    { key: 'cache_creation_tokens', label: t('usageDashboard.cacheCreation'), color: 'var(--viz-cache)' },
    { key: 'cache_read_tokens', label: t('usageDashboard.cacheRead'), color: 'var(--viz-reason)' },
    { key: 'input_tokens', label: t('usageDashboard.input'), color: 'var(--viz-input)' },
    { key: 'output_tokens', label: t('usageDashboard.output'), color: 'var(--viz-output)' }
  ]

  return (
    <Stack className='stagger'>
      <FilterRow justify='between' gap='lg'>
        <div />
        <ToolbarActions>
          <SegmentedControl
            ariaLabel={t('usageDashboard.selectedRange')}
            onChange={setRange}
            options={[
              { value: 'today', label: t('usageDashboard.today') },
              { value: '7d', label: t('usageDashboard.sevenDays') },
              { value: '30d', label: t('usageDashboard.thirtyDays') }
            ]}
            value={range}
          />
          <ButtonWithIcon size='sm' variant='outline' icon={RefreshCwIcon} disabled={query.isFetching} onClick={() => void query.refetch()}>
            {t('common.refresh')}
          </ButtonWithIcon>
          {!embedded ? (
            <ButtonWithIcon size='sm' variant='outline' icon={DownloadIcon}>
              {t('command.exportUsageReport')}
            </ButtonWithIcon>
          ) : null}
        </ToolbarActions>
      </FilterRow>
      <Stack>
        {query.isLoading ? <Skeleton aria-label={t('common.loading')} className='h-5 w-40' role='status' /> : null}
        {snapshot?.configured === false ? (
          <AppAlert
            title={t('usageDashboard.setupTitle')}
            description={t('usageDashboard.setupHelp')}
            actions={(
              <LinkAction asChild>
                <Link to='/user'>{t('home.openSetup')}</Link>
              </LinkAction>
            )}
          />
        ) : null}
        {query.error ? (
          <AppAlert
            tone='error'
            title={query.error.message.includes('409') ? t('usageDashboard.credentialError') : t('usageDashboard.unavailable')}
            description={t('usageDashboard.retryHelp')}
          />
        ) : null}
        {snapshot?.configured !== false && snapshot ? (
          <>
            <KpiGrid>
              <KpiCard
                label={t('usageDashboard.rangeCost', { range: rangeLabel })}
                value={currency(totals.actualCost, locale)}
                helper={t('usageDashboard.rangeCostHelper', { cost: currency(totals.standardCost, locale) })}
                accent
                icon={CoinsIcon}
                sparkline={spark}
                sparklineColor='var(--viz-input)'
              />
              <KpiCard
                label={t('usageDashboard.rangeRequests', { range: rangeLabel })}
                value={number(totals.requests, locale)}
                helper={t('usageDashboard.rangeRequestsHelper')}
                icon={ActivityIcon}
                sparkline={snapshot.trend.map((point) => point.requests)}
                sparklineColor='var(--viz-output)'
              />
              <KpiCard
                label={t('usageDashboard.rangeTokens', { range: rangeLabel })}
                value={compact(totals.tokens, locale)}
                helper={t('usageDashboard.rangeTokensHelper', {
                  input: compact(totals.inputTokens, locale),
                  output: compact(totals.outputTokens, locale),
                  cache: compact(totals.cacheCreationTokens + totals.cacheReadTokens, locale)
                })}
                icon={LayersIcon}
                sparkline={spark}
                sparklineColor='var(--viz-reason)'
              />
              <KpiCard
                label={t('usageDashboard.throughput')}
                value={`${compact(stats?.tpm ?? 0, locale)}/m`}
                helper={t('usageDashboard.throughputHelper', {
                  rpm: compact(stats?.rpm ?? 0, locale),
                  avg: durationMs(stats?.average_duration_ms ?? 0, locale)
                })}
                icon={GaugeIcon}
                sparkline={snapshot.trend.map((point) => point.requests)}
                sparklineColor='var(--viz-cache)'
              />
            </KpiGrid>
            <Card>
              <SectionCardHeader
                title={t('usageDashboard.tokenTrend')}
                description={t('usageDashboard.tokenTrendDescription', { range: rangeLabel })}
                actions={<ChartLegend className='justify-end' compact items={tokenKeys} />}
              />
              <CardContentStack className='pt-[14px]'>
                {snapshot.trend.length ? (
                  <StackedAreaChart
                    keys={tokenKeys}
                    series={snapshot.trend}
                    valueFormatter={(value) => compact(value, locale)}
                  />
                ) : (
                  <PageEmpty title={t('usageDashboard.noTrendData')} />
                )}
              </CardContentStack>
            </Card>
            <div className='split-equal'>
              <Card>
                <SectionCardHeader title={t('usageDashboard.modelDistribution')} description={t('usageDashboard.modelDistributionDescription')} />
                <CardContentStack className='pt-[16px]'>
                  {snapshot.models.length ? (
                  <BarsH
                    rows={snapshot.models.slice(0, 6).map((model, index) => ({
                      label: model.model,
                      value: model.total_tokens,
                      share: totals.tokens > 0 ? model.total_tokens / totals.tokens : 0,
                        color: ['var(--viz-input)', 'var(--viz-output)', 'var(--viz-cache)', 'var(--viz-reason)', 'var(--ai-bright)', 'var(--ink-3)'][index % 6]
                      }))}
                    valueFormatter={(value) => compact(value, locale)}
                  />
                ) : (
                  <PageEmpty title={t('usageDashboard.noModelData')} />
                )}
              </CardContentStack>
            </Card>
              <Card className='overflow-hidden'>
                <SectionCardHeader title={t('usageDashboard.costByModel')} description={t('usageDashboard.costByModelDescription')} />
                <CardTableContent>
                  {snapshot.models.length ? (
                    <DataGrid minWidth={560}>
                      <DataGridHeader columns={modelColumns}>
                        <span>{t('usageDashboard.model')}</span>
                        <DataGridHeaderCell align='right'>{t('events.requests')}</DataGridHeaderCell>
                        <DataGridHeaderCell align='right'>{t('events.tokens')}</DataGridHeaderCell>
                        <DataGridHeaderCell align='right'>{t('events.credit')}</DataGridHeaderCell>
                      </DataGridHeader>
                      {snapshot.models.map((model) => (
                        <DataGridRow columns={modelColumns} key={model.model}>
                          <DataGridCell mono truncate>{model.model}</DataGridCell>
                          <DataGridCell align='right' numeric>{number(model.requests, locale)}</DataGridCell>
                          <DataGridCell align='right' numeric>{compact(model.total_tokens, locale)}</DataGridCell>
                          <DataGridCell align='right' emphasis numeric>{currency(model.actual_cost || model.cost, locale)}</DataGridCell>
                        </DataGridRow>
                      ))}
                    </DataGrid>
                  ) : (
                    <PageEmpty title={t('usageDashboard.noModelData')} />
                  )}
                </CardTableContent>
              </Card>
            </div>
            {!embedded ? (
              <Card>
                <SectionCardHeader title={t('usageDashboard.activityHeatmap')} description={t('usageDashboard.activityHeatmapDescription')} />
                <CardContentStack className='pt-[16px]'>
                  {snapshot.trend.length ? (
                    <HeatmapGrid
                      dayLabels={[
                        t('usageDashboard.dayMon'),
                        t('usageDashboard.dayTue'),
                        t('usageDashboard.dayWed'),
                        t('usageDashboard.dayThu'),
                        t('usageDashboard.dayFri'),
                        t('usageDashboard.daySat'),
                        t('usageDashboard.daySun')
                      ]}
                      lessLabel={t('usageDashboard.less')}
                      moreLabel={t('usageDashboard.more')}
                      points={heatmapPoints}
                      valueFormatter={(value) => t('usageDashboard.heatmapRequests', { count: number(value, locale) })}
                    />
                  ) : (
                    <PageEmpty title={t('usageDashboard.noTrendData')} />
                  )}
                </CardContentStack>
              </Card>
            ) : null}
          </>
        ) : null}
      </Stack>
    </Stack>
  )
}
