import { createFileRoute, redirect } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { ensureAuthenticatedUser } from '@/lib/auth/session'
import { dateTime, number } from '@/lib/format'

export function SettingsPage() {
  const qc = useQueryClient()
  const relay = useQuery({ queryKey: ['settings', 'relay'], queryFn: api.settings.relayProviders })
  const scm = useQuery({ queryKey: ['settings', 'scm'], queryFn: () => api.settings.scmProviders(1, 100) })
  const credentials = useQuery({ queryKey: ['settings', 'credentials'], queryFn: api.settings.credentials })
  const deployment = useQuery({ queryKey: ['settings', 'deployment'], queryFn: api.settings.deployment })
  const checkUpdate = useMutation({
    mutationFn: api.settings.checkUpdate,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings', 'deployment'] })
  })
  const restart = useMutation({
    mutationFn: api.settings.restart,
    onSuccess: () => toast.success('Restart requested')
  })

  if (relay.isLoading || scm.isLoading || deployment.isLoading) return <LoadingState />

  return (
    <Page>
      <PageHeader title='Admin Console' description='Task-zone settings backed by current Go APIs. Mutating deployment actions require explicit confirmation.' />
      <div className='grid gap-4 lg:grid-cols-2'>
        <Card>
          <CardHeader>
            <CardTitle>AI Services</CardTitle>
            <CardDescription>Relay providers configured in backend.</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            {(relay.data ?? []).map((provider) => (
              <div key={provider.id} className='flex items-center justify-between gap-3 rounded-md bg-muted p-3'>
                <div>
                  <div className='font-medium'>{provider.display_name || provider.name}</div>
                  <div className='text-muted-foreground text-xs'>{provider.base_url}</div>
                </div>
                <div className='flex items-center gap-2'>
                  {provider.is_primary ? <Badge variant='ai'>primary</Badge> : null}
                  <StatusBadge value={provider.enabled ? 'active' : 'disabled'} />
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Code Platforms</CardTitle>
            <CardDescription>SCM providers and clone bindings.</CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(scm.data?.items ?? []).map((provider) => (
                  <TableRow key={provider.id}>
                    <TableCell>{provider.name}</TableCell>
                    <TableCell>{provider.type}</TableCell>
                    <TableCell><StatusBadge value={provider.status} /></TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Advanced Credentials</CardTitle>
            <CardDescription>Reusable secrets referenced by providers.</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-2'>
            {(credentials.data ?? []).map((credential) => (
              <div key={credential.id} className='flex items-center justify-between rounded-md bg-muted p-3'>
                <div>
                  <div className='font-medium'>{credential.name}</div>
                  <div className='text-muted-foreground text-xs'>{credential.kind} · used {number(credential.usage_count)} times</div>
                </div>
                <span className='text-muted-foreground text-xs'>{dateTime(credential.updated_at)}</span>
              </div>
            ))}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Deployment & Runtime</CardTitle>
            <CardDescription>Current backend deployment status.</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            <div className='rounded-md bg-muted p-3'>
              <div className='font-medium'>v{deployment.data?.version.version || '-'}</div>
              <div className='text-muted-foreground text-xs'>{deployment.data?.mode || 'unknown'} · {deployment.data?.version.commit || '-'}</div>
            </div>
            {deployment.data?.update_available ? <Badge variant='ai'>Update available: v{deployment.data.latest_release?.version}</Badge> : <Badge variant='success'>Up to date</Badge>}
            <div className='flex gap-2'>
              <Button variant='outline' onClick={() => checkUpdate.mutate()} disabled={checkUpdate.isPending}>Check update</Button>
              <Button
                variant='outline'
                onClick={() => {
                  if (window.confirm('Request backend restart?')) restart.mutate()
                }}
                disabled={restart.isPending}
              >
                Restart
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
    </Page>
  )
}

export const settingsRouteGuard = async () => {
  const user = await import('@/query-client').then(({ queryClient }) =>
    queryClient.fetchQuery({ queryKey: ['auth', 'me'], queryFn: ensureAuthenticatedUser })
  )
  if (user.role !== 'admin') throw redirect({ to: '/' })
}
