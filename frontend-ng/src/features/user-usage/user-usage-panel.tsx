import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useState } from 'react'
import { Area, AreaChart, Bar, BarChart, CartesianGrid, XAxis, YAxis } from 'recharts'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { ChartContainer, ChartTooltip, ChartTooltipContent } from '@/components/ui/chart'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { ToggleGroup, ToggleGroupItem } from '@/components/ui/toggle-group'
import { MetricCard } from '@/components/primitives/metric-card'
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

  return (
    <Card>
      <CardHeader className='flex flex-col gap-3 lg:flex-row lg:items-center lg:justify-between'>
        <div>
          <CardTitle>{embedded ? t('usageDashboard.embeddedTitle') : t('usageDashboard.title')}</CardTitle>
          <CardDescription>{t('usageDashboard.subtitle')}</CardDescription>
        </div>
        <div className='flex flex-wrap items-center gap-2'>
          <ToggleGroup type='single' value={range} onValueChange={(value) => value && setRange(value as UsageRangeOption)}>
            <ToggleGroupItem value='today'>{t('usageDashboard.today')}</ToggleGroupItem>
            <ToggleGroupItem value='7d'>{t('usageDashboard.sevenDays')}</ToggleGroupItem>
            <ToggleGroupItem value='30d'>{t('usageDashboard.thirtyDays')}</ToggleGroupItem>
          </ToggleGroup>
          <Button variant='outline' disabled={query.isFetching} onClick={() => void query.refetch()}>
            {t('common.refresh')}
          </Button>
        </div>
      </CardHeader>
      <CardContent className='flex flex-col gap-4'>
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
            <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-4'>
              <MetricCard
                label={t('usageDashboard.rangeCost', { range: rangeLabel })}
                value={currency(totals.actualCost || stats?.total_actual_cost || 0, locale)}
                helper={`${t('usageDashboard.standard')}: ${currency(totals.standardCost || stats?.total_cost || 0, locale)}`}
                accent
              />
              <MetricCard
                label={t('usageDashboard.rangeRequests', { range: rangeLabel })}
                value={number(totals.requests || stats?.total_requests || 0, locale)}
                helper={t('usageDashboard.selectedRange')}
              />
              <MetricCard
                label={t('usageDashboard.rangeTokens', { range: rangeLabel })}
                value={compact(totals.tokens || stats?.total_tokens || 0, locale)}
                helper={`${t('usageDashboard.input')}: ${compact(stats?.total_input_tokens ?? 0, locale)} · ${t('usageDashboard.output')}: ${compact(stats?.total_output_tokens ?? 0, locale)}`}
              />
              <MetricCard
                label={t('usageDashboard.avgResponse')}
                value={durationMs(stats?.average_duration_ms ?? 0, locale)}
                helper={`RPM ${compact(stats?.rpm ?? 0, locale)} · TPM ${compact(stats?.tpm ?? 0, locale)}`}
              />
            </div>
            <div className='grid gap-4 xl:grid-cols-[minmax(0,1.35fr)_minmax(0,1fr)]'>
              <Card>
                <CardHeader><CardTitle>{t('usageDashboard.tokenTrend')}</CardTitle></CardHeader>
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
              <Card>
                <CardHeader><CardTitle>{t('usageDashboard.modelDistribution')}</CardTitle></CardHeader>
                <CardContent>
                  {snapshot.models.length ? (
                    <ChartContainer config={{ tokens: { label: t('usageDashboard.rangeTokens', { range: '' }) } }} className='h-64'>
                      <BarChart data={snapshot.models}>
                        <CartesianGrid vertical={false} />
                        <XAxis dataKey='model' tickLine={false} axisLine={false} />
                        <YAxis tickLine={false} axisLine={false} />
                        <ChartTooltip content={<ChartTooltipContent />} />
                        <Bar dataKey='total_tokens' fill='var(--chart-3)' radius={4} />
                      </BarChart>
                    </ChartContainer>
                  ) : (
                    <Empty><EmptyHeader><EmptyTitle>{t('usageDashboard.noModelData')}</EmptyTitle></EmptyHeader></Empty>
                  )}
                </CardContent>
              </Card>
            </div>
          </>
        ) : null}
      </CardContent>
    </Card>
  )
}
