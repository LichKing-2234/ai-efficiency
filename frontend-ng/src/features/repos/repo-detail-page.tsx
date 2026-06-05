import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { MetricCard } from '@/components/primitives/metric-card'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { compact, dateTime, number, percent } from '@/lib/format'
import { buildRepoBindingPayload } from './repo-binding'
import {
  buildPRListParams,
  canGoNextPRPage,
  canGoPreviousPRPage,
  isActivePRSyncJob,
  isTerminalPRSyncJob,
  prSyncJobMessage,
  prSyncJobProgress,
  prUsageSummary
} from './repo-detail-state'

export function RepoDetailPage() {
  const { id } = useParams({ from: '/repos/$id' })
  const repoId = Number(id)
  const qc = useQueryClient()
  const [selectedProviderId, setSelectedProviderId] = useState('')
  const [prsPage, setPRsPage] = useState(0)
  const [prsPageSize, setPRsPageSize] = useState(10)
  const [prsMonths, setPRsMonths] = useState(3)
  const [activeJobId, setActiveJobId] = useState<number | null>(null)
  const [syncMessage, setSyncMessage] = useState('')
  const repo = useQuery({ queryKey: ['repo', repoId], queryFn: () => api.repos.get(repoId) })
  const scm = useQuery({ queryKey: ['settings', 'scm'], queryFn: () => api.settings.scmProviders(1, 100) })
  const prListParams = buildPRListParams({ page: prsPage, pageSize: prsPageSize, months: prsMonths })
  const prs = useQuery({
    queryKey: ['repo', repoId, 'prs', prListParams],
    queryFn: () => api.repos.prs(repoId, prListParams),
    placeholderData: keepPreviousData
  })
  const latestJob = useQuery({ queryKey: ['repo', repoId, 'latest-job'], queryFn: () => api.repos.latestPRSyncJob(repoId) })
  const activeJob = useQuery({
    queryKey: ['pr-sync-job', activeJobId],
    queryFn: () => api.prs.job(activeJobId ?? 0),
    enabled: activeJobId !== null,
    refetchInterval: (query) => isTerminalPRSyncJob(query.state.data) ? false : 1500
  })
  const sync = useMutation({
    mutationFn: async () => {
      const result = await api.repos.syncPRs(repoId)
      if (!result.job_id) throw new Error('PR sync job was not created')
      return result
    },
    onSuccess: (result) => {
      setActiveJobId(result.job_id)
      setSyncMessage(result.reused ? 'Existing PR sync job recovered.' : 'PR sync job started.')
      void qc.invalidateQueries({ queryKey: ['repo', repoId, 'latest-job'] })
    },
    onError: (error) => {
      setSyncMessage(error instanceof Error ? error.message : 'Failed to start PR sync.')
    }
  })
  const refreshUsage = useMutation({
    mutationFn: api.prs.refreshUsage,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['repo', repoId, 'prs'] })
  })
  const saveBinding = useMutation({
    mutationFn: (providerId: string) => api.repos.update(repoId, buildRepoBindingPayload(providerId)),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['repo', repoId] })
      void qc.invalidateQueries({ queryKey: ['repos'] })
      toast.success('Repository binding saved')
    }
  })

  useEffect(() => {
    if (repo.data) setSelectedProviderId(repo.data.scm_provider_id ? String(repo.data.scm_provider_id) : '')
  }, [repo.data])

  useEffect(() => {
    const job = latestJob.data
    if (activeJobId === null && job && isActivePRSyncJob(job)) {
      setActiveJobId(job.id)
      setSyncMessage(prSyncJobMessage(job))
    }
  }, [activeJobId, latestJob.data])

  useEffect(() => {
    const job = activeJob.data
    if (!job || activeJobId !== job.id || !isTerminalPRSyncJob(job)) return
    setSyncMessage(prSyncJobMessage(job))
    void qc.invalidateQueries({ queryKey: ['repo', repoId, 'latest-job'] })
    if (job.status === 'completed') {
      setPRsPage(0)
      void qc.invalidateQueries({ queryKey: ['repo', repoId, 'prs'] })
    }
  }, [activeJob.data, activeJobId, qc, repoId])

  if (repo.isLoading || prs.isLoading) return <LoadingState />
  const rows = prs.data?.items ?? []
  const summary = prUsageSummary(prs.data?.summary, rows)
  const currentJob = activeJob.data ?? latestJob.data
  const jobProgress = currentJob ? prSyncJobProgress(currentJob) : null
  const activeJobRunning = isActivePRSyncJob(currentJob) || sync.isPending
  const syncDisabledReason = repo.data?.binding_state === 'unbound' ? 'Bind an SCM provider before syncing PRs.' : ''
  const canSync = !activeJobRunning && !syncDisabledReason
  const totalPRs = prs.data?.total ?? summary.total
  const hasPreviousPage = canGoPreviousPRPage(prsPage)
  const hasNextPage = canGoNextPRPage(prsPage, totalPRs, prsPageSize)

  return (
    <Page>
      <PageHeader
        title={repo.data?.full_name || repo.data?.name || `Repo #${repoId}`}
        description={repo.data?.clone_url}
        actions={<Button onClick={() => sync.mutate()} disabled={!canSync}>{activeJobRunning ? 'Syncing PRs' : 'Sync PRs'}</Button>}
      />
      {syncDisabledReason ? <div className='rounded-md border border-border bg-muted/50 px-3 py-2 text-muted-foreground text-sm'>{syncDisabledReason}</div> : null}
      <div className='grid gap-4 sm:grid-cols-4'>
        <MetricCard label='PRs' value={number(totalPRs)} />
        <MetricCard label='With usage' value={number(summary?.with_usage)} accent />
        <MetricCard label='Pending upload' value={number(summary?.pending_upload)} />
        <MetricCard label='Refresh failed' value={number(summary?.refresh_failed)} />
      </div>
      {currentJob ? (
        <Card>
          <CardHeader><CardTitle>Latest PR sync job</CardTitle></CardHeader>
          <CardContent className='flex flex-wrap gap-3 text-sm'>
            <StatusBadge value={currentJob.status} />
            <span>phase: {currentJob.phase}</span>
            <span>fetched: {number(jobProgress?.fetched)}</span>
            <span>processed: {number(jobProgress?.processed)}/{number(currentJob.total_prs || currentJob.fetched_prs)}</span>
            <span>usage: {number(jobProgress?.usageRefreshed)}/{number(jobProgress?.usageTotal)}</span>
            <span className={currentJob.status === 'failed' ? 'text-[var(--ae-warn)]' : 'text-muted-foreground'}>{syncMessage || prSyncJobMessage(currentJob)}</span>
          </CardContent>
        </Card>
      ) : null}
      <Card>
        <CardHeader><CardTitle>SCM binding</CardTitle></CardHeader>
        <CardContent className='flex flex-wrap items-center gap-2'>
          <select
            className='h-8 rounded-md border border-input bg-card px-3 text-sm'
            value={selectedProviderId}
            onChange={(event) => setSelectedProviderId(event.target.value)}
          >
            <option value=''>No provider binding</option>
            {(scm.data?.items ?? []).map((provider) => (
              <option key={provider.id} value={provider.id}>{provider.name}</option>
            ))}
          </select>
          <Button variant='outline' onClick={() => saveBinding.mutate(selectedProviderId)} disabled={saveBinding.isPending}>Save binding</Button>
          <Button variant='ghost' onClick={() => {
            setSelectedProviderId('')
            saveBinding.mutate('')
          }} disabled={saveBinding.isPending}>Clear</Button>
        </CardContent>
      </Card>
      <Card className='overflow-hidden'>
        <CardHeader className='flex-row flex-wrap items-center justify-between gap-3'>
          <CardTitle>Pull requests</CardTitle>
          <div className='flex flex-wrap items-center gap-2 text-sm'>
            <span className='text-muted-foreground'>Merged in</span>
            <select
              className='h-8 rounded-md border border-input bg-card px-3 text-sm'
              value={prsMonths}
              onChange={(event) => {
                setPRsMonths(Number(event.target.value))
                setPRsPage(0)
              }}
            >
              <option value={1}>Last month</option>
              <option value={3}>Last 3 months</option>
              <option value={6}>Last 6 months</option>
              <option value={12}>Last 12 months</option>
              <option value={0}>All time</option>
            </select>
            <select
              className='h-8 rounded-md border border-input bg-card px-3 text-sm'
              value={prsPageSize}
              onChange={(event) => {
                setPRsPageSize(Number(event.target.value))
                setPRsPage(0)
              }}
            >
              <option value={10}>10 / page</option>
              <option value={25}>25 / page</option>
              <option value={50}>50 / page</option>
            </select>
          </div>
        </CardHeader>
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
        <CardFooter className='flex-wrap justify-between gap-3 text-sm'>
          <span className='text-muted-foreground'>Page {number(prsPage + 1)} · {number(totalPRs)} PRs</span>
          <div className='flex items-center gap-2'>
            <Button variant='outline' size='sm' onClick={() => setPRsPage((value) => Math.max(0, value - 1))} disabled={!hasPreviousPage || prs.isFetching}>Previous</Button>
            <Button variant='outline' size='sm' onClick={() => setPRsPage((value) => value + 1)} disabled={!hasNextPage || prs.isFetching}>Next</Button>
          </div>
        </CardFooter>
      </Card>
    </Page>
  )
}
