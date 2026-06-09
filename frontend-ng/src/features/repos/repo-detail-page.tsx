import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { Fragment, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Field, FieldLabel } from '@/components/ui/field'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { MetricCard } from '@/components/primitives/metric-card'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { compact, dateTime, number, percent } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import { buildRepoBindingPayload } from './repo-binding'
import { canShowWebhookRepair, repoWebhookRepairNotice } from './repo-webhook-state'
import {
  buildPRListParams,
  canGoNextPRPage,
  canGoPreviousPRPage,
  commitFreshnessFor,
  commitSnapshots,
  isActivePRSyncJob,
  isTerminalPRSyncJob,
  prSyncJobMessage,
  prSyncJobProgress,
  prUsageSummary,
  usageSummaryNeedsRefresh
} from './repo-detail-state'

export function RepoDetailPage() {
  const { id } = useParams({ from: '/repos/$id' })
  const repoId = Number(id)
  const qc = useQueryClient()
  const { t } = useI18n()
  const [selectedProviderId, setSelectedProviderId] = useState('')
  const [prsPage, setPRsPage] = useState(0)
  const [prsPageSize, setPRsPageSize] = useState(10)
  const [prsMonths, setPRsMonths] = useState(3)
  const [activeJobId, setActiveJobId] = useState<number | null>(null)
  const [syncMessage, setSyncMessage] = useState('')
  const [expandedPRId, setExpandedPRId] = useState<number | null>(null)
  const [autoRefreshedPRIds, setAutoRefreshedPRIds] = useState<Set<number>>(() => new Set())
  const [webhookRepairForce, setWebhookRepairForce] = useState(false)
  const [webhookRepairNotice, setWebhookRepairNotice] = useState<{ kind: 'success' | 'error'; message: string } | null>(null)
  const repo = useQuery({ queryKey: ['repo', repoId], queryFn: () => api.repos.get(repoId) })
  const me = useQuery({ queryKey: ['auth', 'me'], queryFn: api.auth.me })
  const scm = useQuery({ queryKey: ['settings', 'scm'], queryFn: () => api.settings.scmProviders(1, 100) })
  const prListParams = buildPRListParams({ page: prsPage, pageSize: prsPageSize, months: prsMonths })
  const prs = useQuery({
    queryKey: ['repo', repoId, 'prs', prListParams],
    queryFn: () => api.repos.prs(repoId, prListParams),
    placeholderData: keepPreviousData
  })
  const latestJob = useQuery({ queryKey: ['repo', repoId, 'latest-job'], queryFn: () => api.repos.latestPRSyncJob(repoId) })
  const prDetail = useQuery({
    queryKey: ['pr', expandedPRId],
    queryFn: () => api.prs.get(expandedPRId ?? 0),
    enabled: expandedPRId !== null
  })
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
    onSuccess: (updated) => {
      qc.setQueryData(['pr', updated.id], updated)
      void qc.invalidateQueries({ queryKey: ['repo', repoId, 'prs'] })
      void qc.invalidateQueries({ queryKey: ['pr', updated.id] })
    }
  })
  const settlePR = useMutation({
    mutationFn: api.prs.settle,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['repo', repoId, 'prs'] })
      if (expandedPRId !== null) void qc.invalidateQueries({ queryKey: ['pr', expandedPRId] })
      toast.success('PR attribution settled')
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : 'Failed to settle PR attribution')
    }
  })
  const saveBinding = useMutation({
    mutationFn: (providerId: string) => api.repos.update(repoId, buildRepoBindingPayload(providerId)),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['repo', repoId] })
      void qc.invalidateQueries({ queryKey: ['repos'] })
      toast.success('Repository binding saved')
    }
  })
  const repairWebhook = useMutation({
    mutationFn: () => api.repos.repairWebhook(repoId, { force: webhookRepairForce }),
    onSuccess: (item) => {
      const notice = repoWebhookRepairNotice(item)
      setWebhookRepairNotice({
        kind: notice.kind,
        message: notice.detail ? `${t(notice.key)}: ${notice.detail}` : t(notice.key)
      })
      void qc.invalidateQueries({ queryKey: ['repo', repoId] })
      void qc.invalidateQueries({ queryKey: ['repos'] })
    },
    onError: (error) => {
      setWebhookRepairNotice({ kind: 'error', message: error instanceof Error ? error.message : t('repoDetail.webhookRepairFailed') })
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

  useEffect(() => {
    const detail = prDetail.data
    if (!detail || expandedPRId !== detail.id || refreshUsage.isPending || autoRefreshedPRIds.has(detail.id)) return
    if (!usageSummaryNeedsRefresh(detail)) return
    setAutoRefreshedPRIds((ids) => new Set(ids).add(detail.id))
    refreshUsage.mutate(detail.id)
  }, [autoRefreshedPRIds, expandedPRId, prDetail.data, refreshUsage])

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
  const showWebhookRepair = canShowWebhookRepair({
    role: me.data?.role,
    bindingState: repo.data?.binding_state,
    status: repo.data?.status,
    webhookId: repo.data?.webhook_id
  })

  return (
    <Page>
      <PageHeader
        title={repo.data?.full_name || repo.data?.name || `Repo #${repoId}`}
        description={repo.data?.clone_url}
        actions={<Button onClick={() => sync.mutate()} disabled={!canSync}>{activeJobRunning ? 'Syncing PRs' : 'Sync PRs'}</Button>}
      />
      {syncDisabledReason ? <div className='rounded-md border border-border bg-muted/50 px-3 py-2 text-muted-foreground text-sm'>{syncDisabledReason}</div> : null}
      {showWebhookRepair ? (
        <Alert>
          <AlertTitle>{t('repoDetail.repairWebhook')}</AlertTitle>
          <AlertDescription>
            <div className='flex flex-col gap-3'>
              <span>{t('repoDetail.webhookRepairNeeded')}</span>
              {repo.data?.webhook_id ? (
                <Field orientation='horizontal'>
                  <Checkbox checked={webhookRepairForce} onCheckedChange={(value) => setWebhookRepairForce(value === true)} />
                  <FieldLabel>{t('repoDetail.forceReplaceWebhook')}</FieldLabel>
                </Field>
              ) : null}
              <Button className='w-fit' disabled={repairWebhook.isPending} onClick={() => repairWebhook.mutate()}>
                {repairWebhook.isPending ? t('repoDetail.webhookRepairing') : t('repoDetail.repairWebhook')}
              </Button>
            </div>
          </AlertDescription>
        </Alert>
      ) : null}
      {webhookRepairNotice ? (
        <Alert variant={webhookRepairNotice.kind === 'error' ? 'destructive' : 'default'}>
          <AlertTitle>{webhookRepairNotice.message}</AlertTitle>
        </Alert>
      ) : null}
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
            <span className='text-muted-foreground'>{syncMessage || prSyncJobMessage(currentJob)}</span>
          </CardContent>
        </Card>
      ) : null}
      <Card>
        <CardHeader><CardTitle>SCM binding</CardTitle></CardHeader>
        <CardContent className='flex flex-wrap items-center gap-2'>
          <Select value={selectedProviderId || 'none'} onValueChange={(value) => setSelectedProviderId(value === 'none' ? '' : value)}>
            <SelectTrigger><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value='none'>No provider binding</SelectItem>
              {(scm.data?.items ?? []).map((provider) => (
                <SelectItem key={provider.id} value={String(provider.id)}>{provider.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
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
            <Select value={String(prsMonths)} onValueChange={(value) => {
              setPRsMonths(Number(value))
              setPRsPage(0)
            }}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='1'>Last month</SelectItem>
                <SelectItem value='3'>Last 3 months</SelectItem>
                <SelectItem value='6'>Last 6 months</SelectItem>
                <SelectItem value='12'>Last 12 months</SelectItem>
                <SelectItem value='0'>All time</SelectItem>
              </SelectContent>
            </Select>
            <Select value={String(prsPageSize)} onValueChange={(value) => {
              setPRsPageSize(Number(value))
              setPRsPage(0)
            }}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='10'>10 / page</SelectItem>
                <SelectItem value='25'>25 / page</SelectItem>
                <SelectItem value='50'>50 / page</SelectItem>
              </SelectContent>
            </Select>
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
              {rows.map((pr) => {
                const expanded = expandedPRId === pr.id
                const detail = expanded && prDetail.data?.id === pr.id ? prDetail.data : pr
                const snapshots = commitSnapshots(detail)
                const tokenUsage = (detail.usage_input_tokens ?? 0) + (detail.usage_output_tokens ?? 0) + (detail.usage_cached_input_tokens ?? 0) + (detail.usage_reasoning_tokens ?? 0)
                return (
                  <Fragment key={pr.id}>
                    <TableRow>
                      <TableCell>
                        <a className='font-medium text-foreground hover:underline' href={pr.scm_pr_url} target='_blank' rel='noreferrer'>{pr.title}</a>
                        <div className='text-muted-foreground text-xs'>#{pr.scm_pr_id} · {pr.author}</div>
                      </TableCell>
                      <TableCell><Badge variant='ai'>{pr.ai_label} · {percent(pr.ai_ratio)}</Badge></TableCell>
                      <TableCell>
                        <div className='flex flex-col gap-1'>
                          <StatusBadge value={pr.usage_status || pr.attribution_status} />
                          {pr.usage_status_reason ? <span className='max-w-48 truncate text-muted-foreground text-xs'>{pr.usage_status_reason}</span> : null}
                        </div>
                      </TableCell>
                      <TableCell className='tnum'>{compact((pr.usage_input_tokens ?? 0) + (pr.usage_output_tokens ?? 0) + (pr.usage_cached_input_tokens ?? 0) + (pr.usage_reasoning_tokens ?? 0))}</TableCell>
                      <TableCell>{number(pr.cycle_time_hours)}h</TableCell>
                      <TableCell>{dateTime(pr.merged_at)}</TableCell>
                      <TableCell>
                        <div className='flex justify-end gap-2'>
                          <Button variant='ghost' size='sm' onClick={() => setExpandedPRId(expanded ? null : pr.id)} disabled={prDetail.isFetching && expanded}>
                            {expanded ? 'Hide' : 'Details'}
                          </Button>
                          <Button variant='outline' size='sm' onClick={() => refreshUsage.mutate(pr.id)} disabled={refreshUsage.isPending}>Refresh usage</Button>
                        </div>
                      </TableCell>
                    </TableRow>
                    {expanded ? (
                      <TableRow>
                        <TableCell colSpan={7} className='bg-muted/30 p-4'>
                          {prDetail.isLoading ? (
                            <div className='py-4 text-center text-muted-foreground text-sm'>Loading PR details...</div>
                          ) : (
                            <div className='flex flex-col gap-4'>
                              <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-6'>
                                <div>
                                  <div className='text-muted-foreground text-xs'>Input</div>
                                  <div className='tnum font-medium'>{compact(detail.usage_input_tokens)}</div>
                                </div>
                                <div>
                                  <div className='text-muted-foreground text-xs'>Output</div>
                                  <div className='tnum font-medium'>{compact(detail.usage_output_tokens)}</div>
                                </div>
                                <div>
                                  <div className='text-muted-foreground text-xs'>Cache</div>
                                  <div className='tnum font-medium'>{compact(detail.usage_cached_input_tokens)}</div>
                                </div>
                                <div>
                                  <div className='text-muted-foreground text-xs'>Reasoning</div>
                                  <div className='tnum font-medium'>{compact(detail.usage_reasoning_tokens)}</div>
                                </div>
                                <div>
                                  <div className='text-muted-foreground text-xs'>Requests</div>
                                  <div className='tnum font-medium'>{number(detail.usage_request_count)}</div>
                                </div>
                                <div>
                                  <div className='text-muted-foreground text-xs'>Credits</div>
                                  <div className='tnum font-medium'>{number(detail.usage_credit_usage)}</div>
                                </div>
                              </div>
                              <div className='flex flex-wrap items-center justify-between gap-3 text-sm'>
                                <div className='flex flex-wrap items-center gap-2'>
                                  <StatusBadge value={detail.usage_status || detail.attribution_status} />
                                  <span className='text-muted-foreground'>total {compact(tokenUsage)} tokens · refreshed {dateTime(detail.usage_refreshed_at)}</span>
                                </div>
                                <div className='flex gap-2'>
                                  <Button variant='outline' size='sm' onClick={() => refreshUsage.mutate(detail.id)} disabled={refreshUsage.isPending}>Refresh usage</Button>
                                  <Button variant='outline' size='sm' onClick={() => settlePR.mutate(detail.id)} disabled={settlePR.isPending || detail.attribution_status === 'clear'}>
                                    Resolve attribution
                                  </Button>
                                </div>
                              </div>
                              <div className='overflow-x-auto rounded-md border border-border bg-card'>
                                <Table>
                                  <TableHeader>
                                    <TableRow>
                                      <TableHead>Commit</TableHead>
                                      <TableHead>Captured</TableHead>
                                      <TableHead>Input</TableHead>
                                      <TableHead>Output</TableHead>
                                      <TableHead>Cache</TableHead>
                                      <TableHead>Reasoning</TableHead>
                                      <TableHead>Credits</TableHead>
                                      <TableHead>Requests</TableHead>
                                      <TableHead>Freshness</TableHead>
                                    </TableRow>
                                  </TableHeader>
                                  <TableBody>
                                    {snapshots.length > 0 ? snapshots.map((snapshot) => {
                                      const freshness = commitFreshnessFor(detail, snapshot.commit_sha)
                                      return (
                                        <TableRow key={snapshot.commit_sha}>
                                          <TableCell className='max-w-56 truncate font-mono text-xs'>{snapshot.commit_sha}</TableCell>
                                          <TableCell>{dateTime(snapshot.captured_at)}</TableCell>
                                          <TableCell className='tnum'>{compact(snapshot.input_tokens)}</TableCell>
                                          <TableCell className='tnum'>{compact(snapshot.output_tokens)}</TableCell>
                                          <TableCell className='tnum'>{compact(snapshot.cached_input_tokens)}</TableCell>
                                          <TableCell className='tnum'>{compact(snapshot.reasoning_tokens)}</TableCell>
                                          <TableCell className='tnum'>{number(snapshot.credit_usage)}</TableCell>
                                          <TableCell className='tnum'>{number(snapshot.request_count)}</TableCell>
                                          <TableCell>
                                            <div className='flex flex-col gap-1'>
                                              <StatusBadge value={freshness?.usage_status} />
                                              {freshness?.usage_status_reason ? <span className='max-w-64 truncate text-muted-foreground text-xs'>{freshness.usage_status_reason}</span> : null}
                                            </div>
                                          </TableCell>
                                        </TableRow>
                                      )
                                    }) : (
                                      <TableRow>
                                        <TableCell colSpan={9} className='py-6 text-center text-muted-foreground text-sm'>No commit usage snapshots.</TableCell>
                                      </TableRow>
                                    )}
                                  </TableBody>
                                </Table>
                              </div>
                            </div>
                          )}
                        </TableCell>
                      </TableRow>
                    ) : null}
                  </Fragment>
                )
              })}
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
