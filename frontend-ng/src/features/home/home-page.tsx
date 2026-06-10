import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowRightIcon, FolderGit2Icon, GitPullRequestIcon, PlugZapIcon, WorkflowIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Empty, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { Ring } from '@/components/primitives/charts'
import { ChecklistRow } from '@/components/primitives/checklist-row'
import { EntityCardHeader } from '@/components/primitives/entity-card-header'
import { HeroContent } from '@/components/primitives/hero-content'
import { MetricCard } from '@/components/primitives/metric-card'
import { Page } from '@/components/primitives/page'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { LoadingState } from '@/components/primitives/data-state'
import { UsageActivityRow } from '@/components/primitives/usage-activity-row'
import { UserUsagePanel } from '@/features/user-usage/user-usage-panel'
import { api } from '@/lib/api'
import { compact, dateTime, number } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import { buildHomeActivitySummary, homeSetupProgress } from './home-state'

export function HomePage() {
  const { locale, t } = useI18n()
  const dashboard = useQuery({ queryKey: ['dashboard'], queryFn: api.dashboard })
  const providers = useQuery({ queryKey: ['user-providers'], queryFn: api.userProviders })
  const events = useQuery({ queryKey: ['events', 'recent'], queryFn: () => api.events.list({ limit: 3, offset: 0 }) })
  const me = useQuery({ queryKey: ['auth', 'me'], queryFn: api.auth.me })

  if (dashboard.isLoading || providers.isLoading || events.isLoading) {
    return <LoadingState />
  }

  const connectedTools = new Set<string>()
  for (const provider of providers.data?.providers ?? []) {
    for (const group of provider.groups) {
      if (group.credential.state === 'existing_hidden') connectedTools.add(group.platform)
    }
  }
  const recentEvents = events.data?.items ?? []
  const setupProgress = homeSetupProgress({
    connectedTools: connectedTools.size,
    totalRepos: dashboard.data?.total_repos,
    recentEvents: recentEvents.length
  })

  return (
    <Page>
      <Card variant='accent'>
        <HeroContent
          action={(
            <Button asChild>
              <Link to='/user'>{t('home.openSetup')}<ArrowRightIcon data-icon='inline-end' /></Link>
            </Button>
          )}
          badge={<Badge variant='ai'>{t('home.heroBadge')}</Badge>}
          description={t('home.roleLine', {
            identity: me.data?.email || me.data?.username || t('auth.guest'),
            role: me.data?.role || 'user',
            source: me.data?.auth_source || t('common.unknown')
          })}
          title={t('home.heroTitle')}
        />
      </Card>

      <UserUsagePanel embedded />

      <div className='kpi-grid'>
        <MetricCard
          label={t('home.repos')}
          value={number(dashboard.data?.total_repos, locale)}
          helper={t('home.reposHelp')}
          icon={FolderGit2Icon}
          sparkline={[2, 4, 5, 7, 8, dashboard.data?.total_repos ?? 0]}
        />
        <MetricCard
          label={t('home.trackedWorkflows')}
          value={number(dashboard.data?.tracked_workflows, locale)}
          helper={t('home.trackedWorkflowsHelp')}
          icon={WorkflowIcon}
          sparkline={[1, 3, 3, 5, 7, dashboard.data?.tracked_workflows ?? 0]}
          sparklineColor='var(--viz-output)'
        />
        <MetricCard
          label={t('home.totalAiPrs')}
          value={number(dashboard.data?.total_ai_prs, locale)}
          helper={t('home.totalAiPrsHelp')}
          accent
          icon={GitPullRequestIcon}
          sparkline={[3, 5, 8, 13, 21, dashboard.data?.total_ai_prs ?? 0]}
          sparklineColor='var(--viz-input)'
        />
        <MetricCard
          label={t('home.connectedTools')}
          value={number(connectedTools.size, locale)}
          helper={connectedTools.size ? [...connectedTools].join(', ') : t('home.statusAiAccessMissing')}
          icon={PlugZapIcon}
          sparkline={[0, 0, 1, 1, 2, connectedTools.size]}
          sparklineColor='var(--viz-reason)'
        />
      </div>

      <div className='split-2'>
        <Card>
          <EntityCardHeader
            leading={<Ring color='var(--ai)' size={66} stroke={7} value={setupProgress.ratio}>
              <div className='text-center'>
                <div className='font-bold text-base leading-none tnum'>
                  {setupProgress.ready}
                  <span className='text-[11px] text-[var(--ink-3)]'>/{setupProgress.total}</span>
                </div>
              </div>
            </Ring>}
            title={t('home.setupStatus')}
            description={setupProgress.ready === setupProgress.total ? t('home.statusReady') : t('home.statusWaitingEvents')}
          />
          <CardContentStack>
            <StatusLine label={t('home.statusAccount')} value={t('home.statusReady')} ok />
            <StatusLine label={t('home.statusAiAccess')} value={connectedTools.size ? t('home.statusAiAccessReady') : t('home.statusAiAccessMissing')} ok={connectedTools.size > 0} to='/user' />
            <StatusLine label={t('home.statusRepositoryReporting')} value={(dashboard.data?.total_repos ?? 0) > 0 ? t('home.statusConfigured') : t('home.statusNoRepo')} ok={(dashboard.data?.total_repos ?? 0) > 0} to='/repos' />
            <StatusLine label={t('home.statusRecentUsage')} value={recentEvents.length ? t('home.statusEvents') : t('home.statusWaitingEvents')} ok={recentEvents.length > 0} to='/events' />
          </CardContentStack>
        </Card>
        <Card>
          <SectionCardHeader
            title={t('home.recentUsage')}
            live
            actions={(
              <Button asChild variant='link' size='sm'>
                <Link to='/events'>{t('home.viewAllRecords')}<ArrowRightIcon data-icon='inline-end' /></Link>
              </Button>
            )}
          />
          <CardContent className='flex flex-col'>
            {recentEvents.length ? recentEvents.map((event, index) => (
              <HomeActivityRow key={event.id} event={buildHomeActivitySummary(event)} first={index === 0} locale={locale} />
            )) : (
              <Empty>
                <EmptyHeader>
                  <EmptyTitle>{t('common.empty')}</EmptyTitle>
                </EmptyHeader>
              </Empty>
            )}
          </CardContent>
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
