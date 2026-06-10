import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { ActivityIcon, CoinsIcon, GaugeIcon, LayersIcon, RefreshCwIcon } from 'lucide-react'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Skeleton } from '@/components/ui/skeleton'
import { ActionGroup } from '@/components/primitives/action-group'
import { CardTableContent } from '@/components/primitives/card-table-content'
import { ChartLegend } from '@/components/primitives/chart-legend'
import { BarsH, StackedAreaChart, type StackedAreaKey } from '@/components/primitives/charts'
import { DataGrid, DataGridCell, DataGridHeader, DataGridHeaderCell, DataGridRow } from '@/components/primitives/data-grid'
import { FilterRow } from '@/components/primitives/filter-row'
import { FilterRowTitle } from '@/components/primitives/filter-row-title'
import { HeatmapGrid } from '@/components/primitives/heatmap-grid'
import { MetricCard } from '@/components/primitives/metric-card'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { SegmentedControl } from '@/components/primitives/segmented-control'
import { Stack } from '@/components/primitives/stack'
import { api } from '@/lib/api'
import type { UserUsageTrendPoint } from '@/lib/api/types'
import { compact, currency, durationMs, number } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import { buildUsageDashboardParams, buildUsageHeatmapPoints, rangeLabelKey, usageTotalsFromTrend, type UsageRangeOption } from './user-usage-state'

const modelColumns = '1.5fr_0.8fr_0.9fr_0.8fr'

export function UserUsagePanel({ embedded = false }: { embedded?: boolean }) {
  const { locale, t } = useI18n()
  const [range, setRange] = useState<UsageRangeOption>('7d')
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
  const modelMax = Math.max(1, ...(snapshot?.models ?? []).map((model) => model.total_tokens))

  return (
    <Stack className='stagger'>
      <FilterRow className='justify-between gap-3'>
        {!embedded ? (
          <FilterRowTitle title={t('usageDashboard.title')} description={t('usageDashboard.subtitle')} />
        ) : <div />}
        <ActionGroup wrap>
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
          <Button variant='outline' disabled={query.isFetching} onClick={() => void query.refetch()}>
            <RefreshCwIcon data-icon='inline-start' />
            {t('common.refresh')}
          </Button>
        </ActionGroup>
      </FilterRow>
      <Stack>
        {query.isLoading ? <Skeleton aria-label={t('common.loading')} className='h-5 w-40' role='status' /> : null}
        {snapshot?.configured === false ? (
          <Alert>
            <AlertTitle>{t('usageDashboard.setupTitle')}</AlertTitle>
            <AlertDescription>{t('usageDashboard.setupHelp')}</AlertDescription>
            <Button asChild className='mt-3' size='sm'>
              <Link to='/user'>{t('home.openSetup')}</Link>
            </Button>
          </Alert>
        ) : null}
        {query.error ? (
          <Alert variant='destructive'>
            <AlertTitle>{query.error.message.includes('409') ? t('usageDashboard.credentialError') : t('usageDashboard.unavailable')}</AlertTitle>
            <AlertDescription>{t('usageDashboard.retryHelp')}</AlertDescription>
          </Alert>
        ) : null}
        {snapshot?.configured !== false && snapshot ? (
          <>
            <div className='kpi-grid'>
              <MetricCard
                label={t('usageDashboard.rangeCost', { range: rangeLabel })}
                value={currency(totals.actualCost || stats?.total_actual_cost || 0, locale)}
                helper={`${t('usageDashboard.standard')}: ${currency(totals.standardCost || stats?.total_cost || 0, locale)}`}
                accent
                delta={-9}
                deltaTone='pos'
                icon={CoinsIcon}
                sparkline={spark}
                sparklineColor='var(--viz-input)'
              />
              <MetricCard
                label={t('usageDashboard.rangeRequests', { range: rangeLabel })}
                value={number(totals.requests || stats?.total_requests || 0, locale)}
                helper={t('usageDashboard.selectedRange')}
                delta={12}
                icon={ActivityIcon}
                sparkline={snapshot.trend.map((point) => point.requests)}
                sparklineColor='var(--viz-output)'
              />
              <MetricCard
                label={t('usageDashboard.rangeTokens', { range: rangeLabel })}
                value={compact(totals.tokens || stats?.total_tokens || 0, locale)}
                helper={`${t('usageDashboard.input')}: ${compact(stats?.total_input_tokens ?? 0, locale)} · ${t('usageDashboard.output')}: ${compact(stats?.total_output_tokens ?? 0, locale)}`}
                delta={15}
                icon={LayersIcon}
                sparkline={spark}
                sparklineColor='var(--viz-reason)'
              />
              <MetricCard
                label={t('usageDashboard.avgResponse')}
                value={durationMs(stats?.average_duration_ms ?? 0, locale)}
                helper={`RPM ${compact(stats?.rpm ?? 0, locale)} · TPM ${compact(stats?.tpm ?? 0, locale)}`}
                delta={4}
                icon={GaugeIcon}
                sparkline={snapshot.trend.map((point) => point.requests)}
                sparklineColor='var(--viz-cache)'
              />
            </div>
            <Card>
              <SectionCardHeader title={t('usageDashboard.tokenTrend')} description={t('usageDashboard.tokenTrendDescription', { range: rangeLabel })} />
              <CardContent>
                {snapshot.trend.length ? (
                  <Stack>
                    <ChartLegend items={tokenKeys} />
                    <StackedAreaChart
                      keys={tokenKeys}
                      series={snapshot.trend}
                      valueFormatter={(value) => compact(value, locale)}
                    />
                  </Stack>
                ) : (
                  <Empty><EmptyHeader><EmptyTitle>{t('usageDashboard.noTrendData')}</EmptyTitle></EmptyHeader></Empty>
                )}
              </CardContent>
            </Card>
            <div className='split-2'>
              <Card>
                <SectionCardHeader title={t('usageDashboard.modelDistribution')} description={t('usageDashboard.modelDistributionDescription')} />
                <CardContent>
                  {snapshot.models.length ? (
                    <BarsH
                      rows={snapshot.models.slice(0, 6).map((model, index) => ({
                        label: model.model,
                        value: model.total_tokens,
                        share: model.total_tokens / modelMax,
                        color: ['var(--viz-input)', 'var(--viz-output)', 'var(--viz-cache)', 'var(--viz-reason)', 'var(--ai-bright)', 'var(--ink-3)'][index % 6]
                      }))}
                      valueFormatter={(value) => compact(value, locale)}
                    />
                  ) : (
                    <Empty><EmptyHeader><EmptyTitle>{t('usageDashboard.noModelData')}</EmptyTitle></EmptyHeader></Empty>
                  )}
                </CardContent>
              </Card>
              <Card className='overflow-hidden'>
                <SectionCardHeader title={t('usageDashboard.costByModel')} description={t('usageDashboard.modelDistributionDescription')} />
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
                    <Empty><EmptyHeader><EmptyTitle>{t('usageDashboard.noModelData')}</EmptyTitle></EmptyHeader></Empty>
                  )}
                </CardTableContent>
              </Card>
            </div>
            {!embedded ? (
              <Card>
                <SectionCardHeader title={t('usageDashboard.activityHeatmap')} description={t('usageDashboard.activityHeatmapDescription')} />
                <CardContent>
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
                    <Empty><EmptyHeader><EmptyTitle>{t('usageDashboard.noTrendData')}</EmptyTitle></EmptyHeader></Empty>
                  )}
                </CardContent>
              </Card>
            ) : null}
          </>
        ) : null}
      </Stack>
    </Stack>
  )
}
