import { Link, useNavigate } from '@tanstack/react-router'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckIcon, ChevronRightIcon, CircleDotIcon, ExternalLinkIcon, FolderGit2Icon, GitPullRequestIcon, PlusIcon, RefreshCwIcon, WrenchIcon } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { CardPagerFooter } from '@/components/primitives/card-pager-footer'
import { ButtonWithIcon } from '@/components/primitives/button-with-icon'
import { CategoryBadge } from '@/components/primitives/category-badge'
import { CountBadge } from '@/components/primitives/count-badge'
import { KpiCard } from '@/components/primitives/metric-card'
import { AppAlert } from '@/components/primitives/app-alert'
import { Page } from '@/components/primitives/page'
import { EmptyState, LoadingState } from '@/components/primitives/data-state'
import { DataGrid, DataGridHeader, DataGridHeaderCell, DataGridPrimaryLink, DataGridRecordCell, DataGridRow, DataGridRowAffordance } from '@/components/primitives/data-grid'
import { DetailFieldSection } from '@/components/primitives/detail-field-section'
import { DetailSummaryStack } from '@/components/primitives/detail-summary-stack'
import { EntityGlyph } from '@/components/primitives/entity-glyph'
import { EndActions } from '@/components/primitives/end-actions'
import { FieldItem } from '@/components/primitives/field-list'
import { FormDialog } from '@/components/primitives/form-dialog'
import { InfoTile, InfoTileGrid } from '@/components/primitives/info-tile'
import { InlineDestructiveActions } from '@/components/primitives/inline-destructive-actions'
import { KpiGrid } from '@/components/primitives/kpi-grid'
import { PagerNavButton } from '@/components/primitives/pager-nav-button'
import { QuietActionButton } from '@/components/primitives/quiet-action-button'
import { RatioMeter } from '@/components/primitives/ratio-meter'
import { RepositoriesWorkbenchShell } from '@/components/primitives/repositories-workbench-shell'
import { SegmentedControl } from '@/components/primitives/segmented-control'
import { SectionNav, type SectionNavItem } from '@/components/primitives/section-nav'
import { SlideOver } from '@/components/primitives/slide-over'
import { StatusBadge } from '@/components/primitives/status-badge'
import { SplitActions } from '@/components/primitives/split-actions'
import { api } from '@/lib/api'
import type { RepoConfig } from '@/lib/api/types'
import { dateTime, number, percent } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import { RepoCreateForm, type RepoCreateFormLabels } from './repo-create-form'
import {
  buildRepoCloneUrl,
  buildRepoCreatePayload,
  buildRepoListParams,
  buildRepoSearch,
  buildScopeNavItems,
  compareInventoryProviders,
  firstScope,
  parseRepoSearch,
  parseRepoUrl,
  selectProviderForRepoOrigin,
  webhookRepairBatchMessage,
  type ParsedRepoUrl,
  type RepoBindingFilter,
  type RepoCloneProtocol,
  type RepoWorkbenchSearch
} from './repos-state'

function readInitialSearch(): RepoWorkbenchSearch {
  if (typeof window === 'undefined') return { binding: 'all', provider: '', scope: '', page: 1, pageSize: 20 }
  return parseRepoSearch(Object.fromEntries(new URL(window.location.href).searchParams.entries()))
}

export function ReposPage() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const { locale, t } = useI18n()
  const [search, setSearch] = useState<RepoWorkbenchSearch>(readInitialSearch)
  const [showAdd, setShowAdd] = useState(false)
  const [selectedRepo, setSelectedRepo] = useState<RepoConfig | null>(null)
  const [deleteConfirmId, setDeleteConfirmId] = useState<number | null>(null)
  const [repoUrl, setRepoUrl] = useState('')
  const [selectedProviderId, setSelectedProviderId] = useState('')
  const [cloneProtocol, setCloneProtocol] = useState<RepoCloneProtocol>('http')
  const [sshHost, setSshHost] = useState('')
  const [defaultBranch, setDefaultBranch] = useState('main')
  const [addError, setAddError] = useState('')
  const [autoBindMessage, setAutoBindMessage] = useState('')
  const [autoBindError, setAutoBindError] = useState('')
  const [webhookRepairMessage, setWebhookRepairMessage] = useState('')
  const [webhookRepairError, setWebhookRepairError] = useState('')
  const parsedRepo = useMemo(() => parseRepoUrl(repoUrl), [repoUrl])
  const inventory = useQuery({ queryKey: ['repos', 'inventory'], queryFn: api.repos.inventory })
  const reposForProviders = useMemo(() => [...(inventory.data ?? [])].sort(compareInventoryProviders), [inventory.data])
  const selectedProvider = reposForProviders.find((item) => item.provider_key === search.provider)
    ?? reposForProviders.find((item) => item.provider_key !== 'unbound')
    ?? reposForProviders[0]
    ?? null
  const selectedScope = selectedProvider?.scopes.some((scope) => scope.scope === search.scope) ? search.scope : firstScope(selectedProvider)
  const scopeItems = buildScopeNavItems(selectedProvider, (value) => number(value, locale)).map((scope) => ({
    ...scope,
    icon: FolderGit2Icon
  })) satisfies Array<SectionNavItem<string>>
  const repos = useQuery({
    queryKey: ['repos', 'workbench', selectedProvider?.provider_key, selectedScope, search.binding, search.page, search.pageSize],
    queryFn: () => api.repos.list(buildRepoListParams({
      provider: selectedProvider,
      scope: selectedScope,
      binding: search.binding,
      page: search.page,
      pageSize: search.pageSize
    })),
    enabled: !!selectedProvider && !!selectedScope,
    placeholderData: keepPreviousData
  })
  const scm = useQuery({ queryKey: ['settings', 'scm'], queryFn: () => api.settings.scmProviders(1, 100) })
  const me = useQuery({ queryKey: ['auth', 'me'], queryFn: api.auth.me })
  const autoBind = useMutation({
    mutationFn: api.repos.autoBindUnbound,
    onSuccess: (result) => {
      setAutoBindError('')
      setAutoBindMessage(t('repos.autoBindSummary', {
        bound: result.summary.bound,
        noMatch: result.summary.skipped_no_match,
        ambiguous: result.summary.skipped_ambiguous,
        webhookFailed: result.summary.webhook_failed,
        errors: result.summary.errors
      }))
      void invalidateRepos(qc)
    },
    onError: (error) => {
      setAutoBindMessage('')
      setAutoBindError(error instanceof Error ? error.message : t('repos.autoBindFailed'))
    }
  })
  const webhookRepair = useMutation({
    mutationFn: api.repos.repairFailedWebhooks,
    onSuccess: (result) => {
      const summary = webhookRepairBatchMessage(result)
      setWebhookRepairError('')
      setWebhookRepairMessage(t('repos.webhookRepairSummary', summary))
      void invalidateRepos(qc)
    },
    onError: (error) => {
      setWebhookRepairMessage('')
      setWebhookRepairError(error instanceof Error ? error.message : t('repos.webhookRepairFailed'))
    }
  })
  const createRepo = useMutation({
    mutationFn: () => {
      if (!parsedRepo) throw new Error(t('repos.enterRepoUrl'))
      if (!selectedProviderId) throw new Error(t('repos.selectScmProvider'))
      return api.repos.createDirect(buildRepoCreatePayload({
        providerId: Number(selectedProviderId),
        parsed: parsedRepo,
        cloneProtocol,
        sshHost,
        defaultBranch
      }))
    },
    onSuccess: () => {
      setShowAdd(false)
      resetAddForm()
      void invalidateRepos(qc)
      toast.success(t('repos.repoAdded'))
    },
    onError: (error) => {
      setAddError(error instanceof Error ? error.message : t('repos.addFailed'))
    }
  })
  const deleteRepo = useMutation({
    mutationFn: api.repos.delete,
    onSuccess: () => {
      setDeleteConfirmId(null)
      void invalidateRepos(qc)
      toast.success(t('repos.repoDeleted'))
    }
  })
  const syncRepo = useMutation({
    mutationFn: api.repos.syncPRs,
    onSuccess: (_result, repoId) => {
      toast.success(t('repoDetail.syncStarted'))
      void invalidateRepos(qc)
      void qc.invalidateQueries({ queryKey: ['repo', repoId] })
      void qc.invalidateQueries({ queryKey: ['repo', repoId, 'latest-job'] })
    },
    onError: (error) => {
      toast.error(error instanceof Error ? error.message : t('repoDetail.failedStartSync'))
    }
  })

  const providers = scm.data?.items ?? []
  const selectedScmProvider = providers.find((provider) => String(provider.id) === selectedProviderId)
  const previewCloneUrl = parsedRepo ? buildRepoCloneUrl(parsedRepo, cloneProtocol, sshHost) : ''
  const rows = repos.data?.items ?? []
  const total = repos.data?.total ?? rows.length
  const health = reposForProviders.reduce(
    (next, provider) => ({
      total: next.total + provider.total_repos,
      bound: next.bound + provider.bound_repos,
      unbound: next.unbound + provider.unbound_repos,
      active: next.active + provider.active_repos
    }),
    { total: 0, bound: 0, unbound: 0, active: 0 }
  )
  const canPreviousPage = search.page > 1
  const canNextPage = search.page * search.pageSize < total

  useEffect(() => {
    const providerKey = selectedProvider?.provider_key ?? ''
    if (providerKey !== search.provider || selectedScope !== search.scope) {
      replaceSearch({ ...search, provider: providerKey, scope: selectedScope, page: 1 })
    }
  }, [search, selectedProvider?.provider_key, selectedScope])

  useEffect(() => {
    if (!parsedRepo) return
    const providerId = selectProviderForRepoOrigin(providers, parsedRepo.origin)
    if (providerId) setSelectedProviderId(String(providerId))
  }, [parsedRepo, providers])

  function replaceSearch(next: RepoWorkbenchSearch) {
    setSearch(next)
    void navigate({ to: '/repos', search: buildRepoSearch(next) as never, replace: true })
  }

  function resetAddForm() {
    setRepoUrl('')
    setSelectedProviderId('')
    setCloneProtocol('http')
    setSshHost('')
    setDefaultBranch('main')
    setAddError('')
  }

  function openAddDialog() {
    resetAddForm()
    const firstProvider = providers[0]
    if (firstProvider) setSelectedProviderId(String(firstProvider.id))
    setShowAdd(true)
  }

  if (inventory.isLoading) return <LoadingState />

  return (
    <Page className='stagger'>
      <EndActions>
        {me.data?.role === 'admin' ? (
          <>
            <ButtonWithIcon size='sm' variant='outline' icon={GitPullRequestIcon} onClick={() => autoBind.mutate()} disabled={autoBind.isPending}>
              {autoBind.isPending ? t('repos.autoBindComplete') : t('repos.autoBind')}
            </ButtonWithIcon>
            <ButtonWithIcon size='sm' variant='outline' icon={WrenchIcon} onClick={() => webhookRepair.mutate({ force: false })} disabled={webhookRepair.isPending}>
              {webhookRepair.isPending ? t('repoDetail.webhookRepairing') : t('repos.repairWebhooks')}
            </ButtonWithIcon>
          </>
        ) : null}
        <ButtonWithIcon size='sm' icon={PlusIcon} onClick={openAddDialog}>
          {t('repos.addRepo')}
        </ButtonWithIcon>
      </EndActions>
      <KpiGrid>
        <KpiCard label={t('repos.totalRepositories')} value={number(health.total, locale)} icon={FolderGit2Icon} sparkline={reposForProviders.map((provider) => provider.total_repos)} />
        <KpiCard label={t('repos.boundRepositories')} value={number(health.bound, locale)} accent icon={CheckIcon} sparkline={reposForProviders.map((provider) => provider.bound_repos)} />
        <KpiCard label={t('repos.unbound')} value={number(health.unbound, locale)} icon={CircleDotIcon} sparkline={reposForProviders.map((provider) => provider.unbound_repos)} sparklineColor='var(--viz-cache)' />
        <KpiCard label={t('repos.activeConfigs')} value={number(health.active, locale)} icon={RefreshCwIcon} sparkline={reposForProviders.map((provider) => provider.active_repos)} sparklineColor='var(--viz-output)' />
      </KpiGrid>
      {health.unbound > 0 ? (
        <AppAlert
          tone='warning'
          title={t('repos.unboundWarningTitle', { count: number(health.unbound, locale) })}
          description={t('repos.unboundWarningDescription')}
          actions={(
            <QuietActionButton onClick={() => replaceSearch({ ...search, binding: 'unbound', provider: 'unbound', page: 1 })}>
              {t('repos.reviewNeedsBinding')}
              <ChevronRightIcon data-icon='inline-end' />
            </QuietActionButton>
          )}
        />
      ) : null}
      <Alerts
        autoBindMessage={autoBindMessage}
        autoBindError={autoBindError}
        webhookRepairMessage={webhookRepairMessage}
        webhookRepairError={webhookRepairError}
      />
      {reposForProviders.length === 0 ? (
        <EmptyState title={t('common.empty')} description={t('repos.healthHelp')} />
      ) : (
        <RepositoriesWorkbenchShell
          footer={(
            <CardPagerFooter
              summary={`${t('common.pageCount', { current: search.page, total: Math.max(1, Math.ceil(total / search.pageSize)) })} · ${number(total, locale)} ${t('repos.totalRepositories')}`}
              previous={<PagerNavButton direction='previous' onClick={() => replaceSearch({ ...search, page: Math.max(1, search.page - 1) })} disabled={!canPreviousPage || repos.isFetching}>{t('common.previous')}</PagerNavButton>}
              next={<PagerNavButton direction='next' onClick={() => replaceSearch({ ...search, page: search.page + 1 })} disabled={!canNextPage || repos.isFetching}>{t('common.next')}</PagerNavButton>}
            />
          )}
          header={(
            <SegmentedControl
              ariaLabel={t('repos.bindingFilter')}
              onChange={(value) => replaceSearch({ ...search, binding: value as RepoBindingFilter, page: 1 })}
              options={[
                { value: 'all', label: t('repos.allBindings') },
                { value: 'bound', label: t('repos.bound') },
                { value: 'unbound', label: t('repos.unbound') }
              ]}
              size='sm'
              value={search.binding}
            />
          )}
          meta={`${number(total, locale)} ${t('repos.totalRepositories')}`}
          providerTabs={(
            <Tabs value={selectedProvider?.provider_key ?? ''} onValueChange={(value) => replaceSearch({ ...search, provider: value, scope: '', page: 1 })}>
              <TabsList variant='line' wrap>
                {reposForProviders.map((provider) => (
                  <TabsTrigger key={provider.provider_key} value={provider.provider_key} className='h-8 gap-2 px-3'>
                    {provider.name}
                    <CountBadge variant='secondary'>{number(provider.total_repos, locale)}</CountBadge>
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
          )}
          rail={(
            <SectionNav
              ariaLabel={t('repos.scopeSearch')}
              items={scopeItems}
              onChange={(scope) => replaceSearch({ ...search, scope, page: 1 })}
              scroll='workbench'
              value={selectedScope}
            />
          )}
          railActions={<CountBadge variant='secondary'>{number(selectedProvider?.scopes.length ?? 0, locale)}</CountBadge>}
          railDescription={selectedProvider?.name ?? t('common.empty')}
          railTitle={t('repos.scopeSearch')}
          title={selectedScope || t('repos.selectedScope')}
        >
          {repos.isLoading ? (
            <LoadingState />
          ) : rows.length === 0 ? (
            <EmptyState title={t('common.empty')} description={t('repos.healthHelp')} />
          ) : (
            <RepoTable
              rows={rows}
              onSelectRepo={(repo) => setSelectedRepo(repo)}
            />
          )}
        </RepositoriesWorkbenchShell>
      )}
      <AddRepoDialog
        open={showAdd}
        setOpen={setShowAdd}
        providers={providers}
        selectedProviderId={selectedProviderId}
        setSelectedProviderId={setSelectedProviderId}
        repoUrl={repoUrl}
        setRepoUrl={setRepoUrl}
        parsedRepo={parsedRepo}
        selectedProvider={selectedScmProvider}
        cloneProtocol={cloneProtocol}
        setCloneProtocol={setCloneProtocol}
        sshHost={sshHost}
        setSshHost={setSshHost}
        previewCloneUrl={previewCloneUrl}
        defaultBranch={defaultBranch}
        setDefaultBranch={setDefaultBranch}
        addError={addError}
        setAddError={setAddError}
        createPending={createRepo.isPending}
        createRepo={() => createRepo.mutate()}
      />
      <RepoInspectSlideOver
        deleteConfirmId={deleteConfirmId}
        deletePending={deleteRepo.isPending}
        deleteRepo={(id) => deleteRepo.mutate(id)}
        repo={selectedRepo}
        setDeleteConfirmId={setDeleteConfirmId}
        syncPending={syncRepo.isPending}
        syncRepo={(id) => syncRepo.mutate(id)}
        onClose={() => setSelectedRepo(null)}
      />
    </Page>
  )
}

function Alerts(props: { autoBindMessage: string; autoBindError: string; webhookRepairMessage: string; webhookRepairError: string }) {
  const { t } = useI18n()
  return (
    <>
      {props.autoBindMessage ? <AppAlert tone='success' title={t('repos.autoBindComplete')} description={props.autoBindMessage} /> : null}
      {props.autoBindError ? <AppAlert tone='error' title={t('repos.autoBindFailed')} description={props.autoBindError} /> : null}
      {props.webhookRepairMessage ? <AppAlert tone='success' title={t('repos.webhookRepairComplete')} description={props.webhookRepairMessage} /> : null}
      {props.webhookRepairError ? <AppAlert tone='error' title={t('repos.webhookRepairFailed')} description={props.webhookRepairError} /> : null}
    </>
  )
}

function RepoTable({
  rows,
  onSelectRepo
}: {
  rows: Awaited<ReturnType<typeof api.repos.list>>['items']
  onSelectRepo: (repo: RepoConfig) => void
}) {
  const { t } = useI18n()
  const columns = '1.8fr_0.8fr_1fr_0.8fr_1fr'
  return (
    <DataGrid minWidth={820}>
      <DataGridHeader columns={columns}>
        <span>{t('repos.repository')}</span>
        <span>{t('events.binding')}</span>
        <span>{t('repos.aiPrs')}</span>
        <span>{t('common.status')}</span>
        <DataGridHeaderCell align='right' />
      </DataGridHeader>
      {rows.map((repo) => (
        <DataGridRow as='button' columns={columns} key={repo.id} onClick={() => onSelectRepo(repo)}>
          <DataGridRecordCell description={repo.clone_url}>
            <DataGridPrimaryLink asChild>
              <span>{repo.full_name || repo.name}</span>
            </DataGridPrimaryLink>
          </DataGridRecordCell>
          <span><StatusBadge value={repo.binding_state} /></span>
          <RatioMeter part={repo.pr_summary?.ai_prs ?? 0} total={repo.pr_summary?.total_prs ?? 0} />
          <span><StatusBadge value={repo.status} /></span>
          <DataGridRowAffordance>
            <ChevronRightIcon />
          </DataGridRowAffordance>
        </DataGridRow>
      ))}
    </DataGrid>
  )
}

function RepoInspectSlideOver({
  deleteConfirmId,
  deletePending,
  deleteRepo,
  repo,
  setDeleteConfirmId,
  syncPending,
  syncRepo,
  onClose
}: {
  deleteConfirmId: number | null
  deletePending: boolean
  deleteRepo: (id: number) => void
  repo: RepoConfig | null
  setDeleteConfirmId: (id: number | null) => void
  syncPending: boolean
  syncRepo: (id: number) => void
  onClose: () => void
}) {
  const { locale, t } = useI18n()
  return (
    <SlideOver
      leading={<EntityGlyph icon={FolderGit2Icon} label={t('repos.repository')} />}
      open={!!repo}
      subtitle={repo?.clone_url}
      title={repo?.full_name || repo?.name || t('repos.repository')}
      onClose={onClose}
    >
      {repo ? (
        <DetailSummaryStack
          statuses={(
            <>
              <StatusBadge value={repo.binding_state} />
              <StatusBadge value={repo.status} />
              <CategoryBadge>{repo.edges?.scm_provider?.name || t('repos.provider')}</CategoryBadge>
            </>
          )}
          metrics={(
            <>
              <InfoTile label={t('repos.totalPrs')} value={number(repo.pr_summary?.total_prs, locale)} />
              <InfoTile label={t('repos.aiPrs')} value={number(repo.pr_summary?.ai_prs, locale)} accent='ai' />
              <InfoTile label={t('repos.aiPrShare')} value={percent(repo.pr_summary?.ai_share, locale)} />
            </>
          )}
        >
          <DetailFieldSection title={t('repos.configuration')}>
            <FieldItem label={t('repos.clone')} value={repo.clone_url || '-'} mono />
            <FieldItem label={t('repos.defaultBranch')} value={repo.default_branch || '-'} mono />
            <FieldItem label={t('repos.provider')} value={repo.edges?.scm_provider?.base_url || repo.edges?.scm_provider?.name || '-'} truncate />
            <FieldItem label={t('adminUsers.updated')} value={dateTime(repo.created_at, locale)} />
          </DetailFieldSection>
          {repo.binding_state === 'unbound' ? (
            <AppAlert
              tone='info'
              title={t('repos.bindToPrSource')}
              description={t('repos.bindToPrSourceDescription')}
              actions={(
                <ButtonWithIcon asChild icon={GitPullRequestIcon}>
                  <Link to='/repos/$id' params={{ id: String(repo.id) }}>
                    {t('repos.bindRepository')}
                  </Link>
                </ButtonWithIcon>
              )}
            />
          ) : null}
          <SplitActions>
            <ButtonWithIcon variant='outline' icon={RefreshCwIcon} onClick={() => syncRepo(repo.id)} disabled={repo.binding_state === 'unbound' || syncPending}>
              {syncPending ? t('repoDetail.syncingPrs') : t('repoDetail.syncPrs')}
            </ButtonWithIcon>
            <ButtonWithIcon asChild variant='outline' icon={ExternalLinkIcon}>
              <Link to='/repos/$id' params={{ id: String(repo.id) }}>
                {t('repos.openDetails')}
              </Link>
            </ButtonWithIcon>
          </SplitActions>
          <InlineDestructiveActions
            armed={deleteConfirmId === repo.id}
            cancelLabel={t('common.cancel')}
            confirmLabel={t('common.confirm')}
            confirmPending={deletePending}
            triggerLabel={t('common.delete')}
            onArm={() => setDeleteConfirmId(repo.id)}
            onCancel={() => setDeleteConfirmId(null)}
            onConfirm={() => deleteRepo(repo.id)}
          />
        </DetailSummaryStack>
      ) : null}
    </SlideOver>
  )
}

function AddRepoDialog({
  open,
  setOpen,
  providers,
  selectedProviderId,
  setSelectedProviderId,
  repoUrl,
  setRepoUrl,
  parsedRepo,
  selectedProvider,
  cloneProtocol,
  setCloneProtocol,
  sshHost,
  setSshHost,
  previewCloneUrl,
  defaultBranch,
  setDefaultBranch,
  addError,
  setAddError,
  createPending,
  createRepo
}: {
  open: boolean
  setOpen: (open: boolean) => void
  providers: Awaited<ReturnType<typeof api.settings.scmProviders>>['items']
  selectedProviderId: string
  setSelectedProviderId: (id: string) => void
  repoUrl: string
  setRepoUrl: (url: string) => void
  parsedRepo: ParsedRepoUrl | null
  selectedProvider: Awaited<ReturnType<typeof api.settings.scmProviders>>['items'][number] | undefined
  cloneProtocol: RepoCloneProtocol
  setCloneProtocol: (protocol: RepoCloneProtocol) => void
  sshHost: string
  setSshHost: (host: string) => void
  previewCloneUrl: string
  defaultBranch: string
  setDefaultBranch: (branch: string) => void
  addError: string
  setAddError: (error: string) => void
  createPending: boolean
  createRepo: () => void
}) {
  const { t } = useI18n()
  const labels: RepoCreateFormLabels = {
    cancel: t('common.cancel'),
    clone: t('repos.clone'),
    create: t('common.create'),
    defaultBranch: t('repos.defaultBranch'),
    enterRepoUrl: t('repos.enterRepoUrl'),
    fullName: t('repos.fullName'),
    noMatchingProvider: t('repos.noMatchingProvider'),
    previewCloneUrl: t('repos.previewCloneUrl'),
    provider: t('repos.provider'),
    repoUrl: t('repos.repoUrl'),
    repoUrlPlaceholder: t('repos.repoUrlPlaceholder'),
    selectScmProvider: t('repos.selectScmProvider'),
    sshHostExample: t('settings.sshHostExample')
  }
  return (
    <FormDialog
      description={t('repos.pasteRepoUrl')}
      open={open}
      title={t('repos.addRepo')}
      onOpenChange={setOpen}
    >
        <RepoCreateForm
          addError={addError}
          cloneProtocol={cloneProtocol}
          createPending={createPending}
          defaultBranch={defaultBranch}
          labels={labels}
          parsedRepo={parsedRepo}
          previewCloneUrl={previewCloneUrl}
          providers={providers}
          repoUrl={repoUrl}
          selectedProvider={selectedProvider}
          selectedProviderId={selectedProviderId}
          sshHost={sshHost}
          onCancel={() => setOpen(false)}
          onCloneProtocolChange={setCloneProtocol}
          onCreate={createRepo}
          onDefaultBranchChange={setDefaultBranch}
          onRepoUrlChange={(value) => {
            setRepoUrl(value)
            setAddError('')
            setCloneProtocol('http')
          }}
          onSelectedProviderIdChange={setSelectedProviderId}
          onSshHostChange={setSshHost}
        />
    </FormDialog>
  )
}

async function invalidateRepos(qc: ReturnType<typeof useQueryClient>) {
  await Promise.all([
    qc.invalidateQueries({ queryKey: ['repos'] }),
    qc.invalidateQueries({ queryKey: ['repos', 'inventory'] })
  ])
}
