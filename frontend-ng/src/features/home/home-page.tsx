import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowRightIcon, CoinsIcon, DownloadIcon, FolderGit2Icon, GaugeIcon, GitPullRequestIcon, WorkflowIcon } from 'lucide-react'
import { ButtonWithIcon } from '@/components/primitives/button-with-icon'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { BarsH, Ring, StackedAreaChart, type StackedAreaKey } from '@/components/primitives/charts'
import { CategoryBadge } from '@/components/primitives/category-badge'
import { ChecklistRow } from '@/components/primitives/checklist-row'
import { CompareBar } from '@/components/primitives/compare-bar'
import { EntityCardHeader } from '@/components/primitives/entity-card-header'
import { HeroContent } from '@/components/primitives/hero-content'
import { KpiGrid } from '@/components/primitives/kpi-grid'
import { LinkAction } from '@/components/primitives/link-action'
import { KpiCard } from '@/components/primitives/metric-card'
import { Page } from '@/components/primitives/page'
import { PageEmpty } from '@/components/primitives/page-empty'
import { ProgressFraction } from '@/components/primitives/progress-fraction'
import { PulseStat } from '@/components/primitives/pulse-stat'
import { PulseStatGrid } from '@/components/primitives/pulse-stat-grid'
import { SectionCard } from '@/components/primitives/section-card'
import { LoadingState } from '@/components/primitives/data-state'
import { StartActions } from '@/components/primitives/start-actions'
import { UsageActivityRow } from '@/components/primitives/usage-activity-row'
import { ValueComparison } from '@/components/primitives/value-comparison'
import { StatusBadge } from '@/components/primitives/status-badge'
import { Card } from '@/components/ui/card'
import { api } from '@/lib/api'
import type { UserUsageTrendPoint } from '@/lib/api/types'
import { compact, currency, dateTime, durationMs, number, percent } from '@/lib/format'
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
  const setupDescription = setupProgress.ready === setupProgress.total
    ? t('home.statusReady')
    : t('home.setupStatusDescription')
  const pulseSpend = usageTrend.slice(-24).map((point) => Math.max(0, point.actual_cost))
  const pulseRequests = usageTrend.slice(-24).map((point) => Math.max(0, point.requests))
  const topModels = usageModels.slice(0, 5)
  const trendMini = usageTrend.slice(-14)
  const tokenKeys: Array<StackedAreaKey<UserUsageTrendPoint>> = [
    { key: 'cache_creation_tokens', label: t('home.tokenTypeCache'), color: 'var(--viz-cache)' },
    { key: 'input_tokens', label: t('home.tokenTypeInput'), color: 'var(--viz-input)' },
    { key: 'output_tokens', label: t('home.tokenTypeOutput'), color: 'var(--viz-output)' }
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
            <StartActions>
              <ButtonWithIcon asChild icon={ArrowRightIcon} iconPosition='end'>
                <Link to='/user'>{t('home.openSetup')}</Link>
              </ButtonWithIcon>
              <ButtonWithIcon size='sm' variant='outline' icon={DownloadIcon} onClick={exportOverviewReport}>
                {t('command.exportUsageReport')}
              </ButtonWithIcon>
            </StartActions>
          )}
          badge={<CategoryBadge variant='ai'>{t('home.heroBadge')}</CategoryBadge>}
          description={t('home.heroDescription', {
            share: percent(aiPrShare, locale),
            saving: percent(savingsRatio, locale)
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
          <PulseStatGrid>
            <PulseStat color='var(--ai)' label={t('home.pulseSpendToday')} value={currency(todayActualCost, locale)} values={pulseSpend} />
            <PulseStat color='var(--viz-output)' divider label={t('home.pulseRequests')} value={number(todayRequests, locale)} values={pulseRequests} />
            <PulseStat color='var(--viz-reason)' divider label={t('home.pulseActiveDevs')} value={number(connectedTools.size, locale)} />
          </PulseStatGrid>
        </CardContentStack>
      </Card>

      <KpiGrid>
        <KpiCard
          label={t('home.aiPrShare')}
          value={percent(aiPrShare, locale)}
          helper={t('home.aiPrCountHelper', { count: number(totalAiPrs, locale) })}
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
          value={durationMs(usageStats?.average_duration_ms ?? 0, locale)}
          helper={t('usageDashboard.avgResponseHelper', {
            rpm: compact(usageStats?.rpm ?? 0, locale),
            tpm: compact(usageStats?.tpm ?? 0, locale)
          })}
          icon={GaugeIcon}
          sparkline={usageTrend.map((point) => point.requests)}
          sparklineColor='var(--viz-cache)'
        />
      </KpiGrid>

      <div className='split-2'>
        <SectionCard
          actions={<StatusBadge value='success' label={t('home.savedLabel', { value: percent(savingsRatio, locale) })} />}
          description={t('home.costEfficiencyDescription')}
          title={t('home.costEfficiency')}
        >
            <ValueComparison current={currency(totalActualCost, locale)} previous={currency(totalStandardCost, locale)} />
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
        </SectionCard>
        <Card>
          <EntityCardHeader
            description={setupDescription}
            leading={<Ring color='var(--ai)' size={66} stroke={7} value={setupProgress.ratio}>
              <div className='text-center'>
                <ProgressFraction ready={setupProgress.ready} total={setupProgress.total} />
              </div>
            </Ring>}
            title={t('home.setupStatus')}
          />
          <CardContentStack>
            <ChecklistRow label={t('home.statusAccount')} ok value={t('home.statusReady')} />
            <ChecklistRow
              action={connectedTools.size > 0 ? null : <LinkAction asChild><Link to='/user'>{t('home.statusFix')}</Link></LinkAction>}
              label={t('home.statusAiAccess')}
              ok={connectedTools.size > 0}
              value={connectedTools.size ? t('home.statusAiAccessCount', { count: number(connectedTools.size, locale) }) : t('home.statusAiAccessMissing')}
            />
            <ChecklistRow
              action={(dashboard.data?.total_repos ?? 0) > 0 ? null : <LinkAction asChild><Link to='/repos'>{t('home.statusFix')}</Link></LinkAction>}
              label={t('home.statusRepositoryReporting')}
              ok={(dashboard.data?.total_repos ?? 0) > 0}
              value={(dashboard.data?.total_repos ?? 0) > 0 ? t('home.statusRepoCount', { count: number(dashboard.data?.total_repos ?? 0, locale) }) : t('home.statusNoRepo')}
            />
            <ChecklistRow
              action={recentEvents.length > 0 ? null : <LinkAction asChild><Link to='/events'>{t('home.statusFix')}</Link></LinkAction>}
              label={t('home.statusRecentUsage')}
              ok={recentEvents.length > 0}
              value={recentEvents.length ? t('home.statusEventCount', { count: number(recentEvents.length, locale) }) : t('home.statusWaitingEvents')}
            />
          </CardContentStack>
        </Card>
      </div>

      <div className='split-2'>
        <SectionCard
          actions={(
            <LinkAction asChild iconEnd={ArrowRightIcon}>
              <Link to='/events'>{t('home.viewAllRecords')}</Link>
            </LinkAction>
          )}
          gap='none'
          live
          title={t('home.liveActivity')}
        >
            {recentEvents.length ? recentEvents.map((event, index) => (
              <HomeActivityRow key={event.id} event={buildHomeActivitySummary(event)} first={index === 0} locale={locale} />
            )) : (
              <PageEmpty title={t('common.empty')} />
            )}
        </SectionCard>
        <SectionCard description={t('home.topModelsDescription')} title={t('home.topModels')}>
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
        </SectionCard>
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
