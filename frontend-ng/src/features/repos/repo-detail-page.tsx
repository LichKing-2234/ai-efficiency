import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useParams } from '@tanstack/react-router'
import { ExternalLink, GitPullRequest, RefreshCw, Save, Waypoints } from 'lucide-react'
import { Fragment, useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Card, CardContent } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { ActionGroup } from '@/components/primitives/action-group'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { CardPagerFooter } from '@/components/primitives/card-pager-footer'
import { CheckboxField } from '@/components/primitives/checkbox-field'
import { ControlGrid } from '@/components/primitives/control-grid'
import { DataGrid, DataGridCell, DataGridHeader, DataGridHeaderCell, DataGridRow, DataGridStatusRow } from '@/components/primitives/data-grid'
import { EntityCardHeader } from '@/components/primitives/entity-card-header'
import { FilterRow } from '@/components/primitives/filter-row'
import { FilterRowTitle } from '@/components/primitives/filter-row-title'
import { InfoTile, InfoTileGrid } from '@/components/primitives/info-tile'
import { InsetPanel } from '@/components/primitives/inset-panel'
import { LinkedRecordItem } from '@/components/primitives/linked-record-list'
import { KpiCard } from '@/components/primitives/metric-card'
import { Page, PageToolbar } from '@/components/primitives/page'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { LoadingState } from '@/components/primitives/data-state'
import { Stack } from '@/components/primitives/stack'
import { StatusBadge } from '@/components/primitives/status-badge'
import { StatusWithReason } from '@/components/primitives/status-with-reason'
import { ToolbarSelect } from '@/components/primitives/toolbar-select'
import { UsageSummaryPanel } from '@/components/primitives/usage-summary-panel'
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
  const prColumns = 'minmax(260px,2fr)_0.7fr_1fr_0.7fr_0.6fr_1fr_minmax(210px,1.1fr)'
  const snapshotColumns = 'minmax(160px,1.4fr)_150px_90px_90px_90px_90px_90px_90px_minmax(180px,1fr)'
  const showWebhookRepair = canShowWebhookRepair({
    role: me.data?.role,
    bindingState: repo.data?.binding_state,
    status: repo.data?.status,
    webhookId: repo.data?.webhook_id
  })

  return (
    <Page className='stagger'>
      <PageToolbar>
        <Button onClick={() => sync.mutate()} disabled={!canSync}><RefreshCw data-icon='inline-start' />{activeJobRunning ? t('repoDetail.syncingPrs') : t('repoDetail.syncPrs')}</Button>
      </PageToolbar>
      {syncDisabledReason ? <InsetPanel compact muted>{syncDisabledReason}</InsetPanel> : null}
      {showWebhookRepair ? (
        <Alert>
          <AlertTitle>{t('repoDetail.repairWebhook')}</AlertTitle>
          <AlertDescription>
            <Stack gap='compact'>
              <span>{t('repoDetail.webhookRepairNeeded')}</span>
              {repo.data?.webhook_id ? (
                <CheckboxField
                  checked={webhookRepairForce}
                  id='repo-detail-force-replace-webhook'
                  label={t('repoDetail.forceReplaceWebhook')}
                  onCheckedChange={setWebhookRepairForce}
                />
              ) : null}
              <ActionGroup align='start'>
                <Button disabled={repairWebhook.isPending} onClick={() => repairWebhook.mutate()}>
                  {repairWebhook.isPending ? t('repoDetail.webhookRepairing') : t('repoDetail.repairWebhook')}
                </Button>
              </ActionGroup>
            </Stack>
          </AlertDescription>
        </Alert>
      ) : null}
      {webhookRepairNotice ? (
        <Alert variant={webhookRepairNotice.kind === 'error' ? 'destructive' : 'default'}>
          <AlertTitle>{webhookRepairNotice.message}</AlertTitle>
        </Alert>
      ) : null}
      <div className='kpi-grid'>
        <KpiCard label={t('repoDetail.prs')} value={number(totalPRs)} />
        <KpiCard label={t('repoDetail.withUsage')} value={number(summary?.with_usage)} accent />
        <KpiCard label={t('repoDetail.pendingUpload')} value={number(summary?.pending_upload)} />
        <KpiCard label={t('repoDetail.refreshFailed')} value={number(summary?.refresh_failed)} />
      </div>
      {currentJob ? (
        <Card>
          <SectionCardHeader title={t('repoDetail.latestSyncJob')} />
          <CardContentStack>
            <InfoTileGrid columns={4}>
              <InfoTile label={t('common.status')} value={<StatusBadge value={currentJob.status} />} />
              <InfoTile label={t('repoDetail.phaseLabel')} value={currentJob.phase || '-'} />
              <InfoTile label={t('repoDetail.fetchedLabel')} value={number(jobProgress?.fetched)} mono />
              <InfoTile label={t('repoDetail.processedLabel')} value={`${number(jobProgress?.processed)}/${number(currentJob.total_prs || currentJob.fetched_prs)}`} mono />
            </InfoTileGrid>
            <InsetPanel muted>
              {t('repoDetail.usage', { done: number(jobProgress?.usageRefreshed), total: number(jobProgress?.usageTotal) })} · {syncMessage || prSyncJobMessage(currentJob)}
            </InsetPanel>
          </CardContentStack>
        </Card>
      ) : null}
      <Card>
        <SectionCardHeader title={t('repoDetail.scmBinding')} leading={Waypoints} />
        <CardContent>
          <ControlGrid variant='inline-actions'>
            <ToolbarSelect
              ariaLabel={t('repoDetail.scmBinding')}
              disabled={scm.isLoading}
              options={[
                { value: 'none', label: t('repoDetail.noProviderBinding') },
                ...(scm.data?.items ?? []).map((provider) => ({ value: String(provider.id), label: provider.name }))
              ]}
              value={selectedProviderId || 'none'}
              width='full'
              onValueChange={(value) => setSelectedProviderId(value === 'none' ? '' : value)}
            />
            <Button variant='outline' onClick={() => saveBinding.mutate(selectedProviderId)} disabled={saveBinding.isPending}><Save data-icon='inline-start' />{t('repoDetail.saveBinding')}</Button>
            <Button variant='ghost' onClick={() => {
              setSelectedProviderId('')
              saveBinding.mutate('')
            }} disabled={saveBinding.isPending}>{t('repoDetail.clearBinding')}</Button>
          </ControlGrid>
        </CardContent>
      </Card>
      <Card className='overflow-hidden'>
        <EntityCardHeader
          title={t('repoDetail.pullRequests')}
          actions={(
            <FilterRow tone='label'>
              <FilterRowTitle title={t('repoDetail.mergedIn')} variant='label' />
              <ToolbarSelect
                ariaLabel={t('repoDetail.mergedIn')}
                options={[
                  { value: '1', label: '1' },
                  { value: '3', label: '3' },
                  { value: '6', label: '6' },
                  { value: '12', label: '12' },
                  { value: '0', label: t('common.allTime') }
                ]}
                value={String(prsMonths)}
                onValueChange={(value) => {
                  setPRsMonths(Number(value))
                  setPRsPage(0)
                }}
              />
              <ToolbarSelect
                ariaLabel={t('common.pageSizeControl')}
                options={[
                  { value: '10', label: t('common.pageSize', { size: 10 }) },
                  { value: '25', label: t('common.pageSize', { size: 25 }) },
                  { value: '50', label: t('common.pageSize', { size: 50 }) }
                ]}
                value={String(prsPageSize)}
                onValueChange={(value) => {
                  setPRsPageSize(Number(value))
                  setPRsPage(0)
                }}
              />
            </FilterRow>
          )}
        />
        <DataGrid minWidth={1180}>
          <DataGridHeader columns={prColumns}>
            <span>{t('repoDetail.prColumn')}</span>
            <span>{t('repoDetail.ai')}</span>
            <span>{t('repoDetail.usageHeader')}</span>
            <span>{t('events.tokens')}</span>
            <span>{t('repoDetail.cycle')}</span>
            <span>{t('repoDetail.merged')}</span>
            <span />
          </DataGridHeader>
              {rows.map((pr) => {
                const expanded = expandedPRId === pr.id
                const detail = expanded && prDetail.data?.id === pr.id ? prDetail.data : pr
                const snapshots = commitSnapshots(detail)
                const tokenUsage = (detail.usage_input_tokens ?? 0) + (detail.usage_output_tokens ?? 0) + (detail.usage_cached_input_tokens ?? 0) + (detail.usage_reasoning_tokens ?? 0)
                return (
                  <Fragment key={pr.id}>
                    <DataGridRow columns={prColumns}>
                      <LinkedRecordItem
                        description={`#${pr.scm_pr_id} · ${pr.author}`}
                        href={pr.scm_pr_url}
                        icon={<GitPullRequest />}
                        label={pr.title}
                        trailing={<ExternalLink />}
                        variant='plain'
                      />
                      <span><Badge variant='ai'>{pr.ai_label} · {percent(pr.ai_ratio)}</Badge></span>
                      <span>
                        <StatusWithReason reason={pr.usage_status_reason} reasonClassName='max-w-48' value={pr.usage_status || pr.attribution_status} />
                      </span>
                      <DataGridCell numeric>{compact((pr.usage_input_tokens ?? 0) + (pr.usage_output_tokens ?? 0) + (pr.usage_cached_input_tokens ?? 0) + (pr.usage_reasoning_tokens ?? 0))}</DataGridCell>
                      <DataGridCell numeric>{number(pr.cycle_time_hours)}h</DataGridCell>
                      <DataGridCell numeric tone='metadata'>{dateTime(pr.merged_at)}</DataGridCell>
                      <ActionGroup>
                        <Button variant='ghost' size='sm' onClick={() => setExpandedPRId(expanded ? null : pr.id)} disabled={prDetail.isFetching && expanded}>
                          {expanded ? t('common.hide') : t('common.details')}
                        </Button>
                        <Button variant='outline' size='sm' onClick={() => refreshUsage.mutate(pr.id)} disabled={refreshUsage.isPending}>{t('repoDetail.refreshUsage')}</Button>
                      </ActionGroup>
                    </DataGridRow>
                    {expanded ? (
                      <InsetPanel flush>
                          {prDetail.isLoading ? (
                            <DataGridStatusRow columns='1fr' tone='loading'>{t('repoDetail.loadingDetails')}</DataGridStatusRow>
                          ) : (
                            <Stack>
                              <UsageSummaryPanel
                                actions={
                                  <>
                                    <Button variant='outline' size='sm' onClick={() => refreshUsage.mutate(detail.id)} disabled={refreshUsage.isPending}>{t('repoDetail.refreshUsage')}</Button>
                                    <Button variant='outline' size='sm' onClick={() => settlePR.mutate(detail.id)} disabled={settlePR.isPending || detail.attribution_status === 'clear'}>
                                      {t('repoDetail.resolveAttribution')}
                                    </Button>
                                  </>
                                }
                                metrics={[
                                  { label: t('repoDetail.input'), value: compact(detail.usage_input_tokens), numeric: true },
                                  { label: t('repoDetail.output'), value: compact(detail.usage_output_tokens), numeric: true },
                                  { label: t('repoDetail.cache'), value: compact(detail.usage_cached_input_tokens), numeric: true },
                                  { label: t('repoDetail.reasoning'), value: compact(detail.usage_reasoning_tokens), numeric: true },
                                  { label: t('repoDetail.requests'), value: number(detail.usage_request_count), numeric: true },
                                  { label: t('repoDetail.credits'), value: number(detail.usage_credit_usage), accent: 'ai', numeric: true }
                                ]}
                                status={<StatusBadge value={detail.usage_status || detail.attribution_status} />}
                                summary={t('repoDetail.totalTokensRefreshed', { tokens: compact(tokenUsage), time: dateTime(detail.usage_refreshed_at) })}
                              />
                              <DataGrid minWidth={980}>
                                <DataGridHeader columns={snapshotColumns}>
                                  <span>{t('repoDetail.commit')}</span>
                                  <span>{t('repoDetail.captured')}</span>
                                  <DataGridHeaderCell align='right'>{t('repoDetail.input')}</DataGridHeaderCell>
                                  <DataGridHeaderCell align='right'>{t('repoDetail.output')}</DataGridHeaderCell>
                                  <DataGridHeaderCell align='right'>{t('repoDetail.cache')}</DataGridHeaderCell>
                                  <DataGridHeaderCell align='right'>{t('repoDetail.reasoning')}</DataGridHeaderCell>
                                  <DataGridHeaderCell align='right'>{t('repoDetail.credits')}</DataGridHeaderCell>
                                  <DataGridHeaderCell align='right'>{t('repoDetail.requests')}</DataGridHeaderCell>
                                  <span>{t('repoDetail.freshness')}</span>
                                </DataGridHeader>
                                {snapshots.length > 0 ? snapshots.map((snapshot) => {
                                  const freshness = commitFreshnessFor(detail, snapshot.commit_sha)
                                  return (
                                    <DataGridRow columns={snapshotColumns} key={snapshot.commit_sha}>
                                      <DataGridCell mono truncate tone='subtle'>{snapshot.commit_sha}</DataGridCell>
                                      <DataGridCell numeric tone='metadata'>{dateTime(snapshot.captured_at)}</DataGridCell>
                                      <DataGridCell align='right' numeric>{compact(snapshot.input_tokens)}</DataGridCell>
                                      <DataGridCell align='right' numeric>{compact(snapshot.output_tokens)}</DataGridCell>
                                      <DataGridCell align='right' numeric>{compact(snapshot.cached_input_tokens)}</DataGridCell>
                                      <DataGridCell align='right' numeric>{compact(snapshot.reasoning_tokens)}</DataGridCell>
                                      <DataGridCell align='right' numeric>{number(snapshot.credit_usage)}</DataGridCell>
                                      <DataGridCell align='right' numeric>{number(snapshot.request_count)}</DataGridCell>
                                      <StatusWithReason reason={freshness?.usage_status_reason} reasonClassName='max-w-64' value={freshness?.usage_status} />
                                    </DataGridRow>
                                  )
                                }) : (
                                  <DataGridStatusRow columns={snapshotColumns}>{t('repoDetail.noCommitSnapshots')}</DataGridStatusRow>
                                )}
                              </DataGrid>
                            </Stack>
                          )}
                      </InsetPanel>
                    ) : null}
                  </Fragment>
                )
              })}
        </DataGrid>
        <CardPagerFooter
          summary={t('repoDetail.pagePrs', { page: number(prsPage + 1), total: number(totalPRs) })}
          previous={<Button variant='outline' size='sm' onClick={() => setPRsPage((value) => Math.max(0, value - 1))} disabled={!hasPreviousPage || prs.isFetching}>{t('common.previous')}</Button>}
          next={<Button variant='outline' size='sm' onClick={() => setPRsPage((value) => value + 1)} disabled={!hasNextPage || prs.isFetching}>{t('common.next')}</Button>}
        />
      </Card>
    </Page>
  )
}
