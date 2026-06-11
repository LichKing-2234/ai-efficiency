import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowRightIcon, CoinsIcon, DownloadIcon, FolderGit2Icon, GitPullRequestIcon, PlugZapIcon, WorkflowIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { ActionGroup } from '@/components/primitives/action-group'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { BarsH, Ring, StackedAreaChart, type StackedAreaKey } from '@/components/primitives/charts'
import { ChecklistRow } from '@/components/primitives/checklist-row'
import { CompareBar } from '@/components/primitives/compare-bar'
import { EntityCardHeader } from '@/components/primitives/entity-card-header'
import { HeroContent } from '@/components/primitives/hero-content'
import { KpiGrid } from '@/components/primitives/kpi-grid'
import { KpiCard } from '@/components/primitives/metric-card'
import { Page } from '@/components/primitives/page'
import { PageEmpty } from '@/components/primitives/page-empty'
import { ProgressFraction } from '@/components/primitives/progress-fraction'
import { PulseStat } from '@/components/primitives/pulse-stat'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { LoadingState } from '@/components/primitives/data-state'
import { UsageActivityRow } from '@/components/primitives/usage-activity-row'
import { api } from '@/lib/api'
import type { UserUsageTrendPoint } from '@/lib/api/types'
import { compact, currency, dateTime, number, percent } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import { buildHomeActivitySummary, homeSetupProgress } from './home-state'

export function HomePage() {
  const { locale, t } = useI18n()
  const dashboard = useQuery({ queryKey: ['dashboard'], queryFn: api.dashboard })
  const providers = useQuery({ queryKey: ['user-providers'], queryFn: api.userProviders })
  const events = useQuery({ queryKey: ['events', 'recent'], queryFn: () => api.events.list({ limit: 3, offset: 0 }) })
  const usage = useQuery({ queryKey: ['user-usage-dashboard', 'home-30d'], queryFn: () => api.userUsage.dashboard({ granularity: 'day' }) })
  const me = useQuery({ queryKey: ['auth', 'me'], queryFn: api.auth.me })

  if (dashboard.isLoading || providers.isLoading || events.isLoading || usage.isLoading) {
    return <LoadingState />
  }

  const connectedTools = new Set<string>()
  for (const provider of providers.data?.providers ?? []) {
    for (const group of provider.groups) {
      if (group.credential.state === 'existing_hidden') connectedTools.add(group.platform)
    }
  }
  const recentEvents = events.data?.items ?? []
  const usageStats = usage.data?.stats
  const usageTrend = usage.data?.trend ?? []
  const usageModels = usage.data?.models ?? []
  const setupProgress = homeSetupProgress({
    connectedTools: connectedTools.size,
    totalRepos: dashboard.data?.total_repos,
    recentEvents: recentEvents.length
  })
  const totalAiPrs = dashboard.data?.total_ai_prs ?? 0
  const totalRepos = dashboard.data?.total_repos ?? 0
  const trackedWorkflows = dashboard.data?.tracked_workflows ?? 0
  const totalRequests = usageStats?.total_requests ?? 0
  const todayRequests = usageStats?.today_requests ?? 0
  const todayActualCost = usageStats?.today_actual_cost ?? 0
  const totalActualCost = usageStats?.total_actual_cost ?? 0
  const totalStandardCost = usageStats?.total_cost ?? 0
  const totalTokens = usageStats?.total_tokens ?? 0
  const aiPrShare = totalRepos > 0 ? Math.min(1, totalAiPrs / Math.max(totalRepos, totalAiPrs)) : 0
  const savingsRatio = totalStandardCost > 0 ? Math.max(0, 1 - (totalActualCost / totalStandardCost)) : 0
  const pulseSpend = usageTrend.slice(-24).map((point) => Math.max(0, point.actual_cost))
  const pulseRequests = usageTrend.slice(-24).map((point) => Math.max(0, point.requests))
  const pulseTokens = usageTrend.slice(-24).map((point) => Math.max(0, point.total_tokens))
  const topModels = usageModels.slice(0, 5)
  const trendMini = usageTrend.slice(-14)
  const tokenKeys: Array<StackedAreaKey<UserUsageTrendPoint>> = [
    { key: 'cache_creation_tokens', label: 'Cache', color: 'var(--viz-cache)' },
    { key: 'input_tokens', label: 'Input', color: 'var(--viz-input)' },
    { key: 'output_tokens', label: 'Output', color: 'var(--viz-output)' }
  ]

  function exportOverviewReport() {
    if (typeof window === 'undefined') return
    const rows = [
      ['metric', 'value'],
      ['total_ai_prs', String(totalAiPrs)],
      ['ai_pr_share', String(aiPrShare)],
      ['tracked_repos', String(totalRepos)],
      ['tracked_workflows', String(trackedWorkflows)],
      ['requests', String(totalRequests)],
      ['actual_cost', String(totalActualCost)],
      ['standard_cost', String(totalStandardCost)],
      ['tokens', String(totalTokens)]
    ]
    const csv = rows.map((columns) => columns.map((value) => `"${String(value).replaceAll('"', '""')}"`).join(',')).join('\n')
    const blob = new Blob([csv], { type: 'text/csv;charset=utf-8' })
    const objectUrl = window.URL.createObjectURL(blob)
    const link = document.createElement('a')
    link.href = objectUrl
    link.download = 'overview-report.csv'
    link.click()
    window.URL.revokeObjectURL(objectUrl)
  }

  return (
    <Page>
      <Card variant='accent'>
        <HeroContent
          action={(
            <ActionGroup wrap>
              <Button asChild>
                <Link to='/user'>{t('home.openSetup')}<ArrowRightIcon data-icon='inline-end' /></Link>
              </Button>
              <Button variant='outline' onClick={exportOverviewReport}>
                <DownloadIcon data-icon='inline-start' />
                {t('command.exportUsageReport')}
              </Button>
            </ActionGroup>
          )}
          badge={<Badge variant='ai'>{t('home.heroBadge')}</Badge>}
          description={t('home.roleLine', {
            identity: me.data?.email || me.data?.username || t('auth.guest'),
            role: me.data?.role || 'user',
            source: me.data?.auth_source || t('common.unknown')
          })}
          title={(
            <>
              {t('home.heroHeadline', {
                count: number(totalAiPrs, locale)
              }).split(number(totalAiPrs, locale)).map((part, index, array) => (
                <span key={`${part}-${index}`}>
                  {part}
                  {index < array.length - 1 ? <span className='text-[var(--ai-deep)] tnum'>{number(totalAiPrs, locale)}</span> : null}
                </span>
              ))}
            </>
          )}
        />
        <CardContentStack className='border-border border-t px-[18px] py-4'>
          <div className='grid gap-0 overflow-hidden rounded-[var(--r-md)] border border-border bg-[var(--surface)] md:grid-cols-3'>
            <PulseStat color='var(--ai)' label={t('home.pulseSpendToday')} value={currency(todayActualCost, locale)} values={pulseSpend} />
            <PulseStat color='var(--viz-output)' divider label={t('home.pulseRequests')} value={number(todayRequests, locale)} values={pulseRequests} />
            <PulseStat color='var(--viz-reason)' divider label={t('home.pulseActiveDevs')} value={number(connectedTools.size, locale)} values={pulseTokens} />
          </div>
        </CardContentStack>
      </Card>

      <KpiGrid>
        <KpiCard
          label={t('home.aiPrShare')}
          value={percent(aiPrShare, locale)}
          helper={`${number(totalAiPrs, locale)} AI PRs`}
          accent
          icon={GitPullRequestIcon}
          sparkline={usageTrend.map((point) => point.requests)}
          sparklineColor='var(--viz-input)'
        />
        <KpiCard
          label={t('home.repos')}
          value={number(totalRepos, locale)}
          helper={t('home.reposHelp')}
          icon={FolderGit2Icon}
          sparkline={[2, 4, 5, 7, 8, totalRepos]}
          sparklineColor='var(--viz-output)'
        />
        <KpiCard
          label={t('home.tokensPerMonth')}
          value={compact(totalTokens, locale)}
          helper={t('home.trackedWorkflowsHelp')}
          icon={WorkflowIcon}
          sparkline={usageTrend.map((point) => point.total_tokens)}
          sparklineColor='var(--viz-reason)'
        />
        <KpiCard
          label={t('home.avgResponse')}
          value={number(connectedTools.size, locale)}
          helper={connectedTools.size ? [...connectedTools].join(', ') : t('home.statusAiAccessMissing')}
          icon={PlugZapIcon}
          sparkline={[0, 0, 1, 1, 2, connectedTools.size]}
          sparklineColor='var(--viz-cache)'
        />
      </KpiGrid>

      <div className='split-2'>
        <Card>
          <SectionCardHeader
            title={t('home.costEfficiency')}
            description={t('home.costEfficiencyDescription')}
            actions={<Badge variant='success'>{t('home.savedLabel', { value: percent(savingsRatio, locale) })}</Badge>}
          />
          <CardContentStack>
            <div className='flex items-baseline gap-3'>
              <div className='tnum text-3xl font-semibold leading-none'>{currency(totalActualCost, locale)}</div>
              <div className='text-[12px] text-[var(--ink-3)] line-through'>{currency(totalStandardCost, locale)}</div>
            </div>
            <CompareBar color='var(--ai)' label={t('home.actualSpend')} max={Math.max(totalStandardCost, totalActualCost, 1)} value={totalActualCost} valueLabel={currency(totalActualCost, locale)} />
            <CompareBar color='var(--surface-3)' label={t('home.standardPricing')} max={Math.max(totalStandardCost, totalActualCost, 1)} value={totalStandardCost} valueLabel={currency(totalStandardCost, locale)} />
            {trendMini.length ? (
              <CardContentStack className='border-[var(--line-faint)] border-t px-0 pt-3'>
                <div className='text-[11.5px] font-medium text-[var(--ink-3)]'>{t('home.tokenConsumption14d')}</div>
                <StackedAreaChart
                  height={120}
                  keys={tokenKeys}
                  series={trendMini}
                  valueFormatter={(value) => compact(value, locale)}
                />
              </CardContentStack>
            ) : null}
          </CardContentStack>
        </Card>
        <Card>
          <EntityCardHeader
            leading={<Ring color='var(--ai)' size={66} stroke={7} value={setupProgress.ratio}>
              <div className='text-center'>
                <ProgressFraction ready={setupProgress.ready} total={setupProgress.total} />
              </div>
            </Ring>}
            title={t('home.setupStatus')}
          />
          <CardContentStack>
            <StatusLine label={t('home.statusAccount')} value={t('home.statusReady')} ok />
            <StatusLine label={t('home.statusAiAccess')} value={connectedTools.size ? t('home.statusAiAccessReady') : t('home.statusAiAccessMissing')} ok={connectedTools.size > 0} to='/user' />
            <StatusLine label={t('home.statusRepositoryReporting')} value={(dashboard.data?.total_repos ?? 0) > 0 ? t('home.statusConfigured') : t('home.statusNoRepo')} ok={(dashboard.data?.total_repos ?? 0) > 0} to='/repos' />
            <StatusLine label={t('home.statusRecentUsage')} value={recentEvents.length ? t('home.statusEvents') : t('home.statusWaitingEvents')} ok={recentEvents.length > 0} to='/events' />
          </CardContentStack>
        </Card>
      </div>

      <div className='split-2'>
        <Card>
          <SectionCardHeader
            title={t('home.liveActivity')}
            live
            actions={(
              <Button asChild variant='link' size='sm'>
                <Link to='/events'>{t('home.viewAllRecords')}<ArrowRightIcon data-icon='inline-end' /></Link>
              </Button>
            )}
          />
          <CardContentStack gap='none'>
            {recentEvents.length ? recentEvents.map((event, index) => (
              <HomeActivityRow key={event.id} event={buildHomeActivitySummary(event)} first={index === 0} locale={locale} />
            )) : (
              <PageEmpty title={t('common.empty')} />
            )}
          </CardContentStack>
        </Card>
        <Card>
          <SectionCardHeader title={t('home.topModels')} description={t('home.topModelsDescription')} />
          <CardContentStack>
            {topModels.length ? (
              <BarsH
                rows={topModels.map((model, index) => ({
                  label: model.model,
                  value: model.total_tokens,
                  share: totalTokens > 0 ? model.total_tokens / totalTokens : 0,
                  color: ['var(--viz-input)', 'var(--viz-output)', 'var(--viz-cache)', 'var(--viz-reason)', 'var(--ai-bright)'][index % 5]
                }))}
                valueFormatter={(value) => compact(value, locale)}
              />
            ) : (
              <PageEmpty title={t('common.empty')} />
            )}
          </CardContentStack>
        </Card>
      </div>
    </Page>
  )
}

function HomeActivityRow({
  event,
  first,
  locale
}: {
  event: ReturnType<typeof buildHomeActivitySummary>
  first: boolean
  locale: ReturnType<typeof useI18n>['locale']
}) {
  const { t } = useI18n()
  return (
    <UsageActivityRow
      bound={event.bound}
      credit={number(event.credit, locale)}
      endedAt={dateTime(event.endedAt, locale)}
      first={first}
      requests={`${number(event.requests, locale)} ${t('home.requestsShort')}`}
      statusLabel={event.bound ? t('events.bound') : t('events.unbound')}
      title={event.title}
      tokens={`${compact(event.tokens, locale)} ${t('home.tokensShort')}`}
      tool={event.tool}
    />
  )
}

function StatusLine({ label, value, ok, to }: { label: string; value: string; ok: boolean; to?: '/user' | '/repos' | '/events' }) {
  const { t } = useI18n()
  return (
    <ChecklistRow
      action={!ok && to ? <Button asChild variant='link' size='sm'><Link to={to}>{t('home.statusFix')}</Link></Button> : null}
      label={label}
      ok={ok}
      value={value}
    />
  )
}
