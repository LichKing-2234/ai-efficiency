import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { ActivityIcon, CoinsIcon, GaugeIcon, LayersIcon, RefreshCwIcon } from 'lucide-react'
import { Area, AreaChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { MetricCard } from '@/components/primitives/metric-card'
import { SegmentedControl } from '@/components/primitives/segmented-control'
import { api } from '@/lib/api'
import { compact, currency, durationMs, number } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import { buildUsageDashboardParams, rangeLabelKey, usageTotalsFromTrend, type UsageRangeOption } from './user-usage-state'

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

  const spark = snapshot?.trend.map((point) => point.total_tokens).filter(Boolean) ?? []

  return (
    <div className='stagger flex flex-col gap-4'>
      <div className='flex flex-wrap items-center justify-between gap-3'>
        {!embedded ? (
          <div className='min-w-0'>
            <div className='font-semibold text-sm'>{t('usageDashboard.title')}</div>
            <div className='mt-0.5 text-muted-foreground text-xs'>{t('usageDashboard.subtitle')}</div>
          </div>
        ) : <div />}
        <div className='flex flex-wrap items-center gap-2'>
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
        </div>
      </div>
      <div className='flex flex-col gap-4'>
        {query.isLoading ? <div className='text-muted-foreground text-sm'>{t('common.loading')}</div> : null}
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
            <div className='split-2'>
              <Card>
                <CardHeader>
                  <CardTitle>{t('usageDashboard.tokenTrend')}</CardTitle>
                  <CardDescription>{t('usageDashboard.tokenTrendDescription', { range: rangeLabel })}</CardDescription>
                </CardHeader>
                <CardContent>
                  {snapshot.trend.length ? (
                    <ChartContainer config={{ input: { label: t('usageDashboard.input') }, output: { label: t('usageDashboard.output') } }} className='h-64'>
                      <AreaChart data={snapshot.trend}>
                        <CartesianGrid vertical={false} />
                        <XAxis dataKey='date' tickLine={false} axisLine={false} />
                        <YAxis tickLine={false} axisLine={false} />
                        <ChartTooltip content={<ChartTooltipContent />} />
                        <Area type='monotone' dataKey='input_tokens' stackId='tokens' fill='var(--chart-1)' stroke='var(--chart-1)' />
                        <Area type='monotone' dataKey='output_tokens' stackId='tokens' fill='var(--chart-2)' stroke='var(--chart-2)' />
                        <Area type='monotone' dataKey='cache_creation_tokens' stackId='tokens' fill='var(--chart-3)' stroke='var(--chart-3)' />
                        <Area type='monotone' dataKey='cache_read_tokens' stackId='tokens' fill='var(--chart-4)' stroke='var(--chart-4)' />
                      </AreaChart>
                    </ChartContainer>
                  ) : (
                    <Empty><EmptyHeader><EmptyTitle>{t('usageDashboard.noTrendData')}</EmptyTitle></EmptyHeader></Empty>
                  )}
                </CardContent>
              </Card>
              <Card className='overflow-hidden'>
                <CardHeader>
                  <CardTitle>{t('usageDashboard.modelDistribution')}</CardTitle>
                  <CardDescription>{t('usageDashboard.modelDistributionDescription')}</CardDescription>
                </CardHeader>
                <CardContent className='px-0 pb-0'>
                  {snapshot.models.length ? (
                    <div className='ae-table'>
                      <div className='ae-thead grid-cols-[1.5fr_0.8fr_0.9fr_0.8fr]'>
                        <span>{t('usageDashboard.model')}</span>
                        <span className='text-right'>{t('events.requests')}</span>
                        <span className='text-right'>{t('events.tokens')}</span>
                        <span className='text-right'>{t('events.credit')}</span>
                      </div>
                      {snapshot.models.map((model) => (
                        <div className='ae-trow grid-cols-[1.5fr_0.8fr_0.9fr_0.8fr]' key={model.model}>
                          <span className='mono min-w-0 truncate text-foreground text-xs'>{model.model}</span>
                          <span className='tnum text-right'>{number(model.requests, locale)}</span>
                          <span className='tnum text-right'>{compact(model.total_tokens, locale)}</span>
                          <span className='tnum text-right font-semibold text-foreground'>{currency(model.actual_cost || model.cost, locale)}</span>
                        </div>
                      ))}
                    </div>
                  ) : (
                    <div className='px-[18px] pb-[18px]'>
                      <Empty><EmptyHeader><EmptyTitle>{t('usageDashboard.noModelData')}</EmptyTitle></EmptyHeader></Empty>
                    </div>
                  )}
                </CardContent>
              </Card>
            </div>
          </>
        ) : null}
      </div>
    </div>
  )
}
