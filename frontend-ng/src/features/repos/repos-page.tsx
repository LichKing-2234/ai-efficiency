import { Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { MetricCard } from '@/components/primitives/metric-card'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { number } from '@/lib/format'

export function ReposPage() {
  const qc = useQueryClient()
  const repos = useQuery({ queryKey: ['repos'], queryFn: () => api.repos.list(1, 100) })
  const autoBind = useMutation({
    mutationFn: api.repos.autoBindUnbound,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['repos'] })
  })

  if (repos.isLoading) return <LoadingState />
  const items = repos.data?.items ?? []
  const unbound = items.filter((repo) => repo.binding_state === 'unbound').length

  return (
    <Page>
      <PageHeader
        title='Repositories'
        description='Repository health, SCM binding state, and PR usage freshness come from the existing Go backend APIs.'
        actions={<Button variant='outline' onClick={() => autoBind.mutate()} disabled={autoBind.isPending}>Auto-bind unbound</Button>}
      />
      <div className='grid gap-4 sm:grid-cols-3'>
        <MetricCard label='Tracked repos' value={number(items.length)} />
        <MetricCard label='Bound repos' value={number(items.length - unbound)} accent />
        <MetricCard label='Unbound repos' value={number(unbound)} />
      </div>
      {autoBind.data ? (
        <Card>
          <CardContent className='p-4 text-sm'>
            Auto-bind scanned {autoBind.data.summary.scanned}, bound {autoBind.data.summary.bound}, ambiguous {autoBind.data.summary.skipped_ambiguous}.
          </CardContent>
        </Card>
      ) : null}
      <Card className='overflow-hidden'>
        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Repository</TableHead>
                <TableHead>Binding</TableHead>
                <TableHead>SCM Provider</TableHead>
                <TableHead>Default Branch</TableHead>
                <TableHead>Status</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((repo) => (
                <TableRow key={repo.id}>
                  <TableCell>
                    <Link className='font-medium text-foreground hover:underline' to='/repos/$id' params={{ id: String(repo.id) }}>
                      {repo.full_name || repo.name}
                    </Link>
                    <div className='text-muted-foreground text-xs'>{repo.clone_url}</div>
                  </TableCell>
                  <TableCell><Badge variant={repo.binding_state === 'bound' ? 'success' : 'warning'}>{repo.binding_state}</Badge></TableCell>
                  <TableCell>{repo.edges?.scm_provider?.name || repo.scm_provider_id || '-'}</TableCell>
                  <TableCell>{repo.default_branch}</TableCell>
                  <TableCell><StatusBadge value={repo.status} /></TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Card>
    </Page>
  )
}
