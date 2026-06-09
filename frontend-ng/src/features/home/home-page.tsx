import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowRightIcon, FolderGit2Icon, GitPullRequestIcon, PlugZapIcon, WorkflowIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { MetricCard } from '@/components/primitives/metric-card'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { UserUsagePanel } from '@/features/user-usage/user-usage-panel'
import { api } from '@/lib/api'
import { number, tokenTotal, dateTime } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'

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

  return (
    <Page>
      <Card className='grid-paper overflow-hidden border-[var(--ai-line)] bg-[linear-gradient(150deg,var(--ai-soft),transparent_60%),var(--surface)]'>
        <CardContent className='flex flex-col gap-5 p-6 lg:flex-row lg:items-center lg:justify-between'>
          <div className='max-w-2xl'>
            <Badge variant='ai'>{t('home.heroBadge')}</Badge>
            <h1 className='mt-4 font-semibold text-2xl tracking-tight md:text-3xl'>{t('home.heroTitle')}</h1>
            <p className='mt-2 text-muted-foreground text-sm'>
              {t('home.roleLine', {
                identity: me.data?.email || me.data?.username || t('auth.guest'),
                role: me.data?.role || 'user',
                source: me.data?.auth_source || t('common.unknown')
              })}
            </p>
          </div>
          <Button asChild>
            <Link to='/user'>{t('home.openSetup')}<ArrowRightIcon data-icon='inline-end' /></Link>
          </Button>
        </CardContent>
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
          <CardHeader>
            <CardTitle>{t('home.setupStatus')}</CardTitle>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            <StatusLine label={t('home.statusAccount')} value={t('home.statusReady')} ok />
            <StatusLine label={t('home.statusAiAccess')} value={connectedTools.size ? t('home.statusAiAccessReady') : t('home.statusAiAccessMissing')} ok={connectedTools.size > 0} to='/user' />
            <StatusLine label={t('home.statusRepositoryReporting')} value={(dashboard.data?.total_repos ?? 0) > 0 ? t('home.statusConfigured') : t('home.statusNoRepo')} ok={(dashboard.data?.total_repos ?? 0) > 0} to='/repos' />
            <StatusLine label={t('home.statusRecentUsage')} value={recentEvents.length ? t('home.statusEvents') : t('home.statusWaitingEvents')} ok={recentEvents.length > 0} to='/events' />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>{t('home.recentUsage')}</CardTitle>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            {recentEvents.length ? recentEvents.map((event) => (
              <div key={event.id} className='flex items-center gap-3 rounded-md bg-muted px-3 py-2'>
                <Badge variant={event.binding_status === 'bound' ? 'success' : 'warning'}>{event.tool}</Badge>
                <div className='min-w-0 flex-1'>
                  <div className='truncate font-medium text-sm'>{event.repo_name || event.source_basename || event.tool_session_id}</div>
                  <div className='text-muted-foreground text-xs'>{dateTime(event.observed_end_at, locale)} · {number(tokenTotal(event), locale)} tokens</div>
                </div>
              </div>
            )) : <div className='text-muted-foreground text-sm'>{t('common.empty')}</div>}
          </CardContent>
        </Card>
      </div>
    </Page>
  )
}

function StatusLine({ label, value, ok, to }: { label: string; value: string; ok: boolean; to?: '/user' | '/repos' | '/events' }) {
  const { t } = useI18n()
  return (
    <div className='flex items-center justify-between gap-3 rounded-md bg-muted px-3 py-2 text-sm'>
      <span className='text-muted-foreground'>{label}</span>
      <div className='flex items-center gap-2'>
        <Badge variant={ok ? 'success' : 'warning'}>{value}</Badge>
        {!ok && to ? <Button asChild variant='link' size='sm'><Link to={to}>{t('home.statusFix')}</Link></Button> : null}
      </div>
    </div>
  )
}
