import { Link } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { ArrowRightIcon } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { MetricCard } from '@/components/primitives/metric-card'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { api } from '@/lib/api'
import { number, tokenTotal, dateTime } from '@/lib/format'

export function HomePage() {
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
      <Card className='grid-bg overflow-hidden border-[var(--ae-ai-line)]'>
        <CardContent className='flex flex-col gap-5 p-6 lg:flex-row lg:items-center lg:justify-between'>
          <div className='max-w-2xl'>
            <Badge variant='ai'>Personal AI work center</Badge>
            <h1 className='mt-4 font-semibold text-2xl tracking-tight md:text-3xl'>Your AI efficiency signals are collected here.</h1>
            <p className='mt-2 text-muted-foreground text-sm'>
              Signed in as {me.data?.email || me.data?.username || 'current user'} · role from backend: {me.data?.role || 'user'} · source: {me.data?.auth_source || 'unknown'}
            </p>
          </div>
          <Button asChild>
            <Link to='/user'>Open setup<ArrowRightIcon data-icon='inline-end' /></Link>
          </Button>
        </CardContent>
      </Card>

      <div className='grid gap-4 sm:grid-cols-2 xl:grid-cols-4'>
        <MetricCard label='Repositories' value={number(dashboard.data?.total_repos)} helper='Tracked repositories in backend' />
        <MetricCard label='Tracked workflows' value={number(dashboard.data?.tracked_workflows)} helper='Code reporting paths currently known' />
        <MetricCard label='AI PRs' value={number(dashboard.data?.total_ai_prs)} helper='PRs labeled as AI-assisted' accent />
        <MetricCard label='Connected tools' value={number(connectedTools.size)} helper={connectedTools.size ? [...connectedTools].join(', ') : 'No group credential yet'} />
      </div>

      <div className='grid gap-4 lg:grid-cols-[0.9fr_1.1fr]'>
        <Card>
          <CardHeader>
            <CardTitle>Setup status</CardTitle>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            <StatusLine label='Account' value='Ready' ok />
            <StatusLine label='AI access' value={connectedTools.size ? 'Credential available' : 'Needs setup'} ok={connectedTools.size > 0} to='/user' />
            <StatusLine label='Repository reporting' value={(dashboard.data?.total_repos ?? 0) > 0 ? 'Configured' : 'No repo yet'} ok={(dashboard.data?.total_repos ?? 0) > 0} to='/repos' />
            <StatusLine label='Recent usage' value={recentEvents.length ? 'Events received' : 'Waiting for events'} ok={recentEvents.length > 0} to='/events' />
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Recent usage records</CardTitle>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            {recentEvents.length ? recentEvents.map((event) => (
              <div key={event.id} className='flex items-center gap-3 rounded-md bg-muted px-3 py-2'>
                <Badge variant={event.binding_status === 'bound' ? 'success' : 'warning'}>{event.tool}</Badge>
                <div className='min-w-0 flex-1'>
                  <div className='truncate font-medium text-sm'>{event.repo_name || event.source_basename || event.tool_session_id}</div>
                  <div className='text-muted-foreground text-xs'>{dateTime(event.observed_end_at)} · {number(tokenTotal(event))} tokens</div>
                </div>
              </div>
            )) : <div className='text-muted-foreground text-sm'>No usage records yet.</div>}
          </CardContent>
        </Card>
      </div>
    </Page>
  )
}

function StatusLine({ label, value, ok, to }: { label: string; value: string; ok: boolean; to?: '/user' | '/repos' | '/events' }) {
  return (
    <div className='flex items-center justify-between gap-3 rounded-md bg-muted px-3 py-2 text-sm'>
      <span className='text-muted-foreground'>{label}</span>
      <div className='flex items-center gap-2'>
        <Badge variant={ok ? 'success' : 'warning'}>{value}</Badge>
        {!ok && to ? <Button asChild variant='link' size='sm'><Link to={to}>Fix</Link></Button> : null}
      </div>
    </div>
  )
}
