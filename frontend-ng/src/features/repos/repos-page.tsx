import { Link, useNavigate } from '@tanstack/react-router'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { CheckIcon, CircleDotIcon, FolderGit2Icon, GitPullRequestIcon, PlusIcon, RefreshCwIcon, WrenchIcon } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { ActionGroup } from '@/components/primitives/action-group'
import { CardFilterBar } from '@/components/primitives/card-filter-bar'
import { CardPagerFooter } from '@/components/primitives/card-pager-footer'
import { MetricCard } from '@/components/primitives/metric-card'
import { AppAlert } from '@/components/primitives/app-alert'
import { Page } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { DataGrid, DataGridCell, DataGridHeader, DataGridRow } from '@/components/primitives/data-grid'
import { RecordMeta } from '@/components/primitives/record-meta'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { SectionNav, type SectionNavItem } from '@/components/primitives/section-nav'
import { StatusBadge } from '@/components/primitives/status-badge'
import { ToolbarSelect } from '@/components/primitives/toolbar-select'
import { WorkbenchRail } from '@/components/primitives/workbench-rail'
import { api } from '@/lib/api'
import { number } from '@/lib/format'
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
      <ActionGroup wrap className='ml-auto'>
        {me.data?.role === 'admin' ? (
          <>
            <Button variant='outline' onClick={() => autoBind.mutate()} disabled={autoBind.isPending}>
              <GitPullRequestIcon data-icon='inline-start' />
              {autoBind.isPending ? t('repos.autoBindComplete') : t('repos.autoBind')}
            </Button>
            <Button variant='outline' onClick={() => webhookRepair.mutate({ force: false })} disabled={webhookRepair.isPending}>
              <WrenchIcon data-icon='inline-start' />
              {webhookRepair.isPending ? t('repoDetail.webhookRepairing') : t('repos.repairWebhooks')}
            </Button>
          </>
        ) : null}
        <Button onClick={openAddDialog}>
          <PlusIcon data-icon='inline-start' />
          {t('repos.addRepo')}
        </Button>
      </ActionGroup>
      <Card>
        <CardFilterBar>
          <ToolbarSelect
            ariaLabel={t('repos.bindingFilter')}
            options={[
              { value: 'all', label: t('repos.allBindings') },
              { value: 'bound', label: t('repos.bound') },
              { value: 'unbound', label: t('repos.unbound') }
            ]}
            value={search.binding}
            onValueChange={(value) => replaceSearch({ ...search, binding: value as RepoBindingFilter, page: 1 })}
          />
          <ToolbarSelect
            ariaLabel={t('common.pageSizeControl')}
            options={[
              { value: '20', label: t('common.pageSize', { size: 20 }) },
              { value: '50', label: t('common.pageSize', { size: 50 }) },
              { value: '100', label: t('common.pageSize', { size: 100 }) }
            ]}
            value={String(search.pageSize)}
            onValueChange={(value) => replaceSearch({ ...search, pageSize: Number(value), page: 1 })}
          />
          <Button variant='ghost' onClick={() => replaceSearch({ ...search, binding: 'unbound', provider: 'unbound', page: 1 })}>{t('repos.reviewNeedsBinding')}</Button>
        </CardFilterBar>
      </Card>
      <div className='kpi-grid'>
        <MetricCard label={t('repos.totalRepositories')} value={number(health.total, locale)} icon={FolderGit2Icon} sparkline={reposForProviders.map((provider) => provider.total_repos)} />
        <MetricCard label={t('repos.boundRepositories')} value={number(health.bound, locale)} accent icon={CheckIcon} sparkline={reposForProviders.map((provider) => provider.bound_repos)} />
        <MetricCard label={t('repos.unbound')} value={number(health.unbound, locale)} icon={CircleDotIcon} sparkline={reposForProviders.map((provider) => provider.unbound_repos)} sparklineColor='var(--viz-cache)' />
        <MetricCard label={t('repos.activeConfigs')} value={number(health.active, locale)} icon={RefreshCwIcon} sparkline={reposForProviders.map((provider) => provider.active_repos)} sparklineColor='var(--viz-output)' />
      </div>
      <Alerts
        autoBindMessage={autoBindMessage}
        autoBindError={autoBindError}
        webhookRepairMessage={webhookRepairMessage}
        webhookRepairError={webhookRepairError}
      />
      {reposForProviders.length === 0 ? (
        <Card>
          <CardContent className='p-8'>
            <Empty>
              <EmptyHeader>
                <EmptyTitle>{t('common.empty')}</EmptyTitle>
                <EmptyDescription>{t('repos.healthHelp')}</EmptyDescription>
              </EmptyHeader>
            </Empty>
          </CardContent>
        </Card>
      ) : (
        <Card className='overflow-hidden'>
          <CardFilterBar>
            <Tabs value={selectedProvider?.provider_key ?? ''} onValueChange={(value) => replaceSearch({ ...search, provider: value, scope: '', page: 1 })}>
              <TabsList className='h-auto flex-wrap justify-start'>
                {reposForProviders.map((provider) => (
                  <TabsTrigger key={provider.provider_key} value={provider.provider_key} className='h-9'>
                    {provider.name}
                    <Badge variant='secondary'>{number(provider.total_repos, locale)}</Badge>
                  </TabsTrigger>
                ))}
              </TabsList>
            </Tabs>
          </CardFilterBar>
          <div className='repo-workbench'>
            <WorkbenchRail
              title={t('repos.scopeSearch')}
              actions={<Badge variant='secondary'>{number(selectedProvider?.scopes.length ?? 0, locale)}</Badge>}
            >
              <SectionNav
                ariaLabel={t('repos.scopeSearch')}
                className='max-h-[430px] overflow-y-auto'
                items={scopeItems}
                onChange={(scope) => replaceSearch({ ...search, scope, page: 1 })}
                value={selectedScope}
              />
            </WorkbenchRail>
            <section className='min-w-0'>
              <SectionCardHeader
                className='border-b border-border px-5 py-4'
                title={selectedScope || t('repos.selectedScope')}
                description={selectedProvider?.name ?? t('common.empty')}
                actions={<span className='text-muted-foreground text-sm'>{number(total, locale)} {t('repos.totalRepositories')}</span>}
              />
              {repos.isLoading ? (
                <LoadingState />
              ) : rows.length === 0 ? (
                <CardContent className='p-8'>
                  <Empty><EmptyHeader><EmptyTitle>{t('common.empty')}</EmptyTitle><EmptyDescription>{t('repos.healthHelp')}</EmptyDescription></EmptyHeader></Empty>
                </CardContent>
              ) : (
                <RepoTable
                  rows={rows}
                  deleteConfirmId={deleteConfirmId}
                  setDeleteConfirmId={setDeleteConfirmId}
                  deleteRepo={(id) => deleteRepo.mutate(id)}
                  deletePending={deleteRepo.isPending}
                />
              )}
            </section>
          </div>
        </Card>
      )}
      <Card>
        <CardPagerFooter
          summary={`${t('common.pageCount', { current: search.page, total: Math.max(1, Math.ceil(total / search.pageSize)) })} · ${number(total, locale)} ${t('repos.totalRepositories')}`}
          previous={<Button variant='outline' size='sm' onClick={() => replaceSearch({ ...search, page: Math.max(1, search.page - 1) })} disabled={!canPreviousPage || repos.isFetching}>{t('common.previous')}</Button>}
          next={<Button variant='outline' size='sm' onClick={() => replaceSearch({ ...search, page: search.page + 1 })} disabled={!canNextPage || repos.isFetching}>{t('common.next')}</Button>}
        />
      </Card>
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
    </Page>
  )
}

function Alerts(props: { autoBindMessage: string; autoBindError: string; webhookRepairMessage: string; webhookRepairError: string }) {
  const { t } = useI18n()
  return (
    <>
      {props.autoBindMessage ? <Alert><AlertTitle>{t('repos.autoBindComplete')}</AlertTitle><AlertDescription>{props.autoBindMessage}</AlertDescription></Alert> : null}
      {props.autoBindError ? <Alert variant='destructive'><AlertTitle>{t('repos.autoBindFailed')}</AlertTitle><AlertDescription>{props.autoBindError}</AlertDescription></Alert> : null}
      {props.webhookRepairMessage ? <Alert><AlertTitle>{t('repos.webhookRepairComplete')}</AlertTitle><AlertDescription>{props.webhookRepairMessage}</AlertDescription></Alert> : null}
      {props.webhookRepairError ? <Alert variant='destructive'><AlertTitle>{t('repos.webhookRepairFailed')}</AlertTitle><AlertDescription>{props.webhookRepairError}</AlertDescription></Alert> : null}
    </>
  )
}

function RepoTable({
  rows,
  deleteConfirmId,
  setDeleteConfirmId,
  deleteRepo,
  deletePending
}: {
  rows: Awaited<ReturnType<typeof api.repos.list>>['items']
  deleteConfirmId: number | null
  setDeleteConfirmId: (id: number | null) => void
  deleteRepo: (id: number) => void
  deletePending: boolean
}) {
  const { t } = useI18n()
  const columns = '1.8fr_0.8fr_1fr_0.8fr_0.8fr_1fr'
  return (
    <DataGrid minWidth={820}>
      <DataGridHeader columns={columns}>
        <span>{t('repos.repository')}</span>
        <span>{t('events.binding')}</span>
        <span>{t('repos.scmProvider')}</span>
        <span>{t('repos.defaultBranch')}</span>
        <span>{t('common.status')}</span>
        <span className='text-right' />
      </DataGridHeader>
      {rows.map((repo) => (
        <DataGridRow columns={columns} key={repo.id}>
          <span className='min-w-0'>
            <Link className='block truncate font-semibold text-foreground text-sm hover:text-[var(--ai-deep)]' to='/repos/$id' params={{ id: String(repo.id) }}>
              {repo.full_name || repo.name}
            </Link>
            <RecordMeta>{repo.clone_url}</RecordMeta>
          </span>
          <span><Badge variant={repo.binding_state === 'bound' ? 'pos' : 'warn'}>{repo.binding_state}</Badge></span>
          <DataGridCell truncate tone='muted'>{repo.edges?.scm_provider?.name || repo.scm_provider_id || '-'}</DataGridCell>
          <DataGridCell mono truncate tone='subtle'>{repo.default_branch}</DataGridCell>
          <span><StatusBadge value={repo.status} /></span>
          <ActionGroup>
            {deleteConfirmId === repo.id ? (
              <>
                <Button variant='destructive' size='sm' onClick={() => deleteRepo(repo.id)} disabled={deletePending}>{t('common.confirm')}</Button>
                <Button variant='ghost' size='sm' onClick={() => setDeleteConfirmId(null)}>{t('common.cancel')}</Button>
              </>
            ) : (
              <Button variant='ghost' size='sm' onClick={() => setDeleteConfirmId(repo.id)}>{t('common.delete')}</Button>
            )}
          </ActionGroup>
        </DataGridRow>
      ))}
    </DataGrid>
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
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('repos.addRepo')}</DialogTitle>
          <DialogDescription>{t('repos.pasteRepoUrl')}</DialogDescription>
        </DialogHeader>
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
      </DialogContent>
    </Dialog>
  )
}

async function invalidateRepos(qc: ReturnType<typeof useQueryClient>) {
  await Promise.all([
    qc.invalidateQueries({ queryKey: ['repos'] }),
    qc.invalidateQueries({ queryKey: ['repos', 'inventory'] })
  ])
}
