import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { ExternalLink, GitPullRequest, RefreshCw, Save, Waypoints } from 'lucide-react'
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
import { InfoTile } from '@/components/primitives/info-tile'
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
      if (!result.job_id) throw new Error(t('repoDetail.prCreatedMissing'))
      return result
    },
    onSuccess: (result) => {
      setActiveJobId(result.job_id)
      setSyncMessage(result.reused ? t('repoDetail.existingSyncRecovered') : t('repoDetail.syncStarted'))
      void qc.invalidateQueries({ queryKey: ['repo', repoId, 'latest-job'] })
    },
    onError: (error) => {
      setSyncMessage(error instanceof Error ? error.message : t('repoDetail.failedStartSync'))
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
      toast.success(t('repoDetail.attributionSettled'))
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t('repoDetail.failedSettle'))
    }
  })
  const saveBinding = useMutation({
    mutationFn: (providerId: string) => api.repos.update(repoId, buildRepoBindingPayload(providerId)),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['repo', repoId] })
      void qc.invalidateQueries({ queryKey: ['repos'] })
      toast.success(t('repoDetail.bindingSaved'))
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
  const syncDisabledReason = repo.data?.binding_state === 'unbound' ? t('repoDetail.bindBeforeSync') : ''
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
    <Page className='stagger'>
      <PageHeader
        title={repo.data?.full_name || repo.data?.name || `#${repoId}`}
        description={repo.data?.clone_url}
        actions={<Button onClick={() => sync.mutate()} disabled={!canSync}><RefreshCw data-icon='inline-start' />{activeJobRunning ? t('repoDetail.syncingPrs') : t('repoDetail.syncPrs')}</Button>}
        variant='toolbar'
      />
      {syncDisabledReason ? <div className='rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] px-3 py-2 text-muted-foreground text-sm'>{syncDisabledReason}</div> : null}
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
        <MetricCard label={t('repoDetail.prs')} value={number(totalPRs)} />
        <MetricCard label={t('repoDetail.withUsage')} value={number(summary?.with_usage)} accent />
        <MetricCard label={t('repoDetail.pendingUpload')} value={number(summary?.pending_upload)} />
        <MetricCard label={t('repoDetail.refreshFailed')} value={number(summary?.refresh_failed)} />
      </div>
      {currentJob ? (
        <Card>
          <CardHeader><CardTitle>{t('repoDetail.latestSyncJob')}</CardTitle></CardHeader>
          <CardContent className='flex flex-col gap-3'>
            <div className='grid gap-3 md:grid-cols-4'>
              <InfoTile label={t('common.status')} value={<StatusBadge value={currentJob.status} />} />
              <InfoTile label={t('repoDetail.phaseLabel')} value={currentJob.phase || '-'} />
              <InfoTile label={t('repoDetail.fetchedLabel')} value={number(jobProgress?.fetched)} mono />
              <InfoTile label={t('repoDetail.processedLabel')} value={`${number(jobProgress?.processed)}/${number(currentJob.total_prs || currentJob.fetched_prs)}`} mono />
            </div>
            <div className='rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-3 text-muted-foreground text-sm'>
              {t('repoDetail.usage', { done: number(jobProgress?.usageRefreshed), total: number(jobProgress?.usageTotal) })} · {syncMessage || prSyncJobMessage(currentJob)}
            </div>
          </CardContent>
        </Card>
      ) : null}
      <Card>
        <CardHeader>
          <div className='flex items-center gap-2'>
            <Waypoints className='text-[var(--ai)]' />
            <CardTitle>{t('repoDetail.scmBinding')}</CardTitle>
          </div>
        </CardHeader>
        <CardContent className='grid gap-3 lg:grid-cols-[minmax(0,1fr)_auto_auto]'>
          <Select value={selectedProviderId || 'none'} onValueChange={(value) => setSelectedProviderId(value === 'none' ? '' : value)}>
            <SelectTrigger className='w-full'><SelectValue /></SelectTrigger>
            <SelectContent>
              <SelectItem value='none'>{t('repoDetail.noProviderBinding')}</SelectItem>
              {(scm.data?.items ?? []).map((provider) => (
                <SelectItem key={provider.id} value={String(provider.id)}>{provider.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Button variant='outline' onClick={() => saveBinding.mutate(selectedProviderId)} disabled={saveBinding.isPending}><Save data-icon='inline-start' />{t('repoDetail.saveBinding')}</Button>
          <Button variant='ghost' onClick={() => {
            setSelectedProviderId('')
            saveBinding.mutate('')
          }} disabled={saveBinding.isPending}>{t('repoDetail.clearBinding')}</Button>
        </CardContent>
      </Card>
      <Card className='overflow-hidden'>
        <CardHeader className='flex-row flex-wrap items-center justify-between gap-3'>
          <CardTitle>{t('repoDetail.pullRequests')}</CardTitle>
          <div className='flex flex-wrap items-center gap-2 text-sm'>
            <span className='text-muted-foreground'>{t('repoDetail.mergedIn')}</span>
            <Select value={String(prsMonths)} onValueChange={(value) => {
              setPRsMonths(Number(value))
              setPRsPage(0)
            }}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='1'>1</SelectItem>
                <SelectItem value='3'>3</SelectItem>
                <SelectItem value='6'>6</SelectItem>
                <SelectItem value='12'>12</SelectItem>
                <SelectItem value='0'>{t('common.allTime')}</SelectItem>
              </SelectContent>
            </Select>
            <Select value={String(prsPageSize)} onValueChange={(value) => {
              setPRsPageSize(Number(value))
              setPRsPage(0)
            }}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='10'>{t('common.pageSize', { size: 10 })}</SelectItem>
                <SelectItem value='25'>{t('common.pageSize', { size: 25 })}</SelectItem>
                <SelectItem value='50'>{t('common.pageSize', { size: 50 })}</SelectItem>
              </SelectContent>
            </Select>
          </div>
        </CardHeader>
        <div className='ae-table'>
          <div className='ae-thead grid-cols-[minmax(260px,2fr)_0.7fr_1fr_0.7fr_0.6fr_1fr_minmax(210px,1.1fr)]'>
            <span>{t('repoDetail.prColumn')}</span>
            <span>{t('repoDetail.ai')}</span>
            <span>{t('repoDetail.usageHeader')}</span>
            <span>{t('events.tokens')}</span>
            <span>{t('repoDetail.cycle')}</span>
            <span>{t('repoDetail.merged')}</span>
            <span />
          </div>
              {rows.map((pr) => {
                const expanded = expandedPRId === pr.id
                const detail = expanded && prDetail.data?.id === pr.id ? prDetail.data : pr
                const snapshots = commitSnapshots(detail)
                const tokenUsage = (detail.usage_input_tokens ?? 0) + (detail.usage_output_tokens ?? 0) + (detail.usage_cached_input_tokens ?? 0) + (detail.usage_reasoning_tokens ?? 0)
                return (
                  <Fragment key={pr.id}>
                    <div className='ae-trow grid-cols-[minmax(260px,2fr)_0.7fr_1fr_0.7fr_0.6fr_1fr_minmax(210px,1.1fr)]'>
                      <span className='min-w-0'>
                        <a className='flex min-w-0 items-center gap-2 font-semibold text-foreground text-sm hover:underline' href={pr.scm_pr_url} target='_blank' rel='noreferrer'>
                          <GitPullRequest className='shrink-0 text-[var(--ai)]' />
                          <span className='truncate'>{pr.title}</span>
                          <ExternalLink className='shrink-0 text-muted-foreground' />
                        </a>
                        <span className='mt-1 block truncate text-muted-foreground text-xs'>#{pr.scm_pr_id} · {pr.author}</span>
                      </span>
                      <span><Badge variant='ai'>{pr.ai_label} · {percent(pr.ai_ratio)}</Badge></span>
                      <span>
                        <div className='flex flex-col gap-1'>
                          <StatusBadge value={pr.usage_status || pr.attribution_status} />
                          {pr.usage_status_reason ? <span className='max-w-48 truncate text-muted-foreground text-xs'>{pr.usage_status_reason}</span> : null}
                        </div>
                      </span>
                      <span className='tnum'>{compact((pr.usage_input_tokens ?? 0) + (pr.usage_output_tokens ?? 0) + (pr.usage_cached_input_tokens ?? 0) + (pr.usage_reasoning_tokens ?? 0))}</span>
                      <span className='tnum'>{number(pr.cycle_time_hours)}h</span>
                      <span className='tnum text-muted-foreground text-xs'>{dateTime(pr.merged_at)}</span>
                      <span>
                        <div className='flex justify-end gap-2'>
                          <Button variant='ghost' size='sm' onClick={() => setExpandedPRId(expanded ? null : pr.id)} disabled={prDetail.isFetching && expanded}>
                            {expanded ? t('common.hide') : t('common.details')}
                          </Button>
                          <Button variant='outline' size='sm' onClick={() => refreshUsage.mutate(pr.id)} disabled={refreshUsage.isPending}>{t('repoDetail.refreshUsage')}</Button>
                        </div>
                      </span>
                    </div>
                    {expanded ? (
                      <div className='border-border border-b bg-[var(--surface-inset)] p-4'>
                          {prDetail.isLoading ? (
                            <div className='py-4 text-center text-muted-foreground text-sm'>{t('repoDetail.loadingDetails')}</div>
                          ) : (
                            <div className='flex flex-col gap-4'>
                              <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-6'>
                                <div>
                                  <div className='text-muted-foreground text-xs'>{t('repoDetail.input')}</div>
                                  <div className='tnum font-medium'>{compact(detail.usage_input_tokens)}</div>
                                </div>
                                <div>
                                  <div className='text-muted-foreground text-xs'>{t('repoDetail.output')}</div>
                                  <div className='tnum font-medium'>{compact(detail.usage_output_tokens)}</div>
                                </div>
                                <div>
                                  <div className='text-muted-foreground text-xs'>{t('repoDetail.cache')}</div>
                                  <div className='tnum font-medium'>{compact(detail.usage_cached_input_tokens)}</div>
                                </div>
                                <div>
                                  <div className='text-muted-foreground text-xs'>{t('repoDetail.reasoning')}</div>
                                  <div className='tnum font-medium'>{compact(detail.usage_reasoning_tokens)}</div>
                                </div>
                                <div>
                                  <div className='text-muted-foreground text-xs'>{t('repoDetail.requests')}</div>
                                  <div className='tnum font-medium'>{number(detail.usage_request_count)}</div>
                                </div>
                                <div>
                                  <div className='text-muted-foreground text-xs'>{t('repoDetail.credits')}</div>
                                  <div className='tnum font-medium'>{number(detail.usage_credit_usage)}</div>
                                </div>
                              </div>
                              <div className='flex flex-wrap items-center justify-between gap-3 text-sm'>
                                <div className='flex flex-wrap items-center gap-2'>
                                  <StatusBadge value={detail.usage_status || detail.attribution_status} />
                                  <span className='text-muted-foreground'>{t('repoDetail.totalTokensRefreshed', { tokens: compact(tokenUsage), time: dateTime(detail.usage_refreshed_at) })}</span>
                                </div>
                                <div className='flex gap-2'>
                                  <Button variant='outline' size='sm' onClick={() => refreshUsage.mutate(detail.id)} disabled={refreshUsage.isPending}>{t('repoDetail.refreshUsage')}</Button>
                                  <Button variant='outline' size='sm' onClick={() => settlePR.mutate(detail.id)} disabled={settlePR.isPending || detail.attribution_status === 'clear'}>
                                    {t('repoDetail.resolveAttribution')}
                                  </Button>
                                </div>
                              </div>
                              <div className='overflow-x-auto rounded-md border border-border bg-card'>
                                <Table>
                                  <TableHeader>
                                    <TableRow>
                                      <TableHead>{t('repoDetail.commit')}</TableHead>
                                      <TableHead>{t('repoDetail.captured')}</TableHead>
                                      <TableHead>{t('repoDetail.input')}</TableHead>
                                      <TableHead>{t('repoDetail.output')}</TableHead>
                                      <TableHead>{t('repoDetail.cache')}</TableHead>
                                      <TableHead>{t('repoDetail.reasoning')}</TableHead>
                                      <TableHead>{t('repoDetail.credits')}</TableHead>
                                      <TableHead>{t('repoDetail.requests')}</TableHead>
                                      <TableHead>{t('repoDetail.freshness')}</TableHead>
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
                                        <TableCell colSpan={9} className='py-6 text-center text-muted-foreground text-sm'>{t('repoDetail.noCommitSnapshots')}</TableCell>
                                      </TableRow>
                                    )}
                                  </TableBody>
                                </Table>
                              </div>
                            </div>
                          )}
                      </div>
                    ) : null}
                  </Fragment>
                )
              })}
        </div>
        <CardFooter className='flex-wrap justify-between gap-3 text-sm'>
          <span className='text-muted-foreground'>{t('repoDetail.pagePrs', { page: number(prsPage + 1), total: number(totalPRs) })}</span>
          <div className='flex items-center gap-2'>
            <Button variant='outline' size='sm' onClick={() => setPRsPage((value) => Math.max(0, value - 1))} disabled={!hasPreviousPage || prs.isFetching}>{t('common.previous')}</Button>
            <Button variant='outline' size='sm' onClick={() => setPRsPage((value) => value + 1)} disabled={!hasNextPage || prs.isFetching}>{t('common.next')}</Button>
          </div>
        </CardFooter>
      </Card>
    </Page>
  )
}
