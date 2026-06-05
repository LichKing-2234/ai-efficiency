import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { MetricCard } from '@/components/primitives/metric-card'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { compact, dateTime, number, percent } from '@/lib/format'

export function RepoDetailPage() {
  const { id } = useParams({ from: '/repos/$id' })
  const repoId = Number(id)
  const qc = useQueryClient()
  const repo = useQuery({ queryKey: ['repo', repoId], queryFn: () => api.repos.get(repoId) })
  const prs = useQuery({ queryKey: ['repo', repoId, 'prs'], queryFn: () => api.repos.prs(repoId, { limit: 50, offset: 0 }) })
  const latestJob = useQuery({ queryKey: ['repo', repoId, 'latest-job'], queryFn: () => api.repos.latestPRSyncJob(repoId) })
  const sync = useMutation({
    mutationFn: () => api.repos.syncPRs(repoId),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['repo', repoId, 'latest-job'] })
      void qc.invalidateQueries({ queryKey: ['repo', repoId, 'prs'] })
    }
  })
  const refreshUsage = useMutation({
    mutationFn: api.prs.refreshUsage,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['repo', repoId, 'prs'] })
  })

  if (repo.isLoading || prs.isLoading) return <LoadingState />
  const rows = prs.data?.items ?? []
  const summary = prs.data?.summary

  return (
    <Page>
      <PageHeader
        title={repo.data?.full_name || repo.data?.name || `Repo #${repoId}`}
        description={repo.data?.clone_url}
        actions={<Button onClick={() => sync.mutate()} disabled={sync.isPending}>Sync PRs</Button>}
      />
      <div className='grid gap-4 sm:grid-cols-4'>
        <MetricCard label='PRs' value={number(prs.data?.total)} />
        <MetricCard label='With usage' value={number(summary?.with_usage)} accent />
        <MetricCard label='Pending upload' value={number(summary?.pending_upload)} />
        <MetricCard label='Refresh failed' value={number(summary?.refresh_failed)} />
      </div>
      {latestJob.data ? (
        <Card>
          <CardHeader><CardTitle>Latest PR sync job</CardTitle></CardHeader>
          <CardContent className='flex flex-wrap gap-3 text-sm'>
            <StatusBadge value={latestJob.data.status} />
            <span>phase: {latestJob.data.phase}</span>
            <span>processed: {latestJob.data.processed_prs}/{latestJob.data.total_prs}</span>
            {latestJob.data.last_error ? <span className='text-[var(--ae-warn)]'>{latestJob.data.last_error}</span> : null}
          </CardContent>
        </Card>
      ) : null}
      <Card className='overflow-hidden'>
        <div className='overflow-x-auto'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>PR</TableHead>
                <TableHead>AI</TableHead>
                <TableHead>Usage</TableHead>
                <TableHead>Tokens</TableHead>
                <TableHead>Cycle</TableHead>
                <TableHead>Merged</TableHead>
                <TableHead />
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((pr) => (
                <TableRow key={pr.id}>
                  <TableCell>
                    <a className='font-medium text-foreground hover:underline' href={pr.scm_pr_url} target='_blank' rel='noreferrer'>{pr.title}</a>
                    <div className='text-muted-foreground text-xs'>#{pr.scm_pr_id} · {pr.author}</div>
                  </TableCell>
                  <TableCell><Badge variant='ai'>{pr.ai_label} · {percent(pr.ai_ratio)}</Badge></TableCell>
                  <TableCell><StatusBadge value={pr.usage_status || pr.attribution_status} /></TableCell>
                  <TableCell className='tnum'>{compact((pr.usage_input_tokens ?? 0) + (pr.usage_output_tokens ?? 0) + (pr.usage_cached_input_tokens ?? 0))}</TableCell>
                  <TableCell>{number(pr.cycle_time_hours)}h</TableCell>
                  <TableCell>{dateTime(pr.merged_at)}</TableCell>
                  <TableCell className='text-right'>
                    <Button variant='outline' size='sm' onClick={() => refreshUsage.mutate(pr.id)} disabled={refreshUsage.isPending}>Refresh usage</Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </div>
      </Card>
    </Page>
  )
}
