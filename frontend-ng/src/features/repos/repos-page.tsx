import { Link, useNavigate } from '@tanstack/react-router'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Empty, EmptyDescription, EmptyHeader, EmptyTitle } from '@/components/ui/empty'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { MetricCard } from '@/components/primitives/metric-card'
import { AppAlert } from '@/components/primitives/app-alert'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { number } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import {
  buildRepoCloneUrl,
  buildRepoCreatePayload,
  buildRepoListParams,
  buildRepoSearch,
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
    <Page>
      <PageHeader
        title={t('repos.title')}
        description={t('repos.subtitle')}
        actions={
          <div className='flex flex-wrap gap-2'>
            {me.data?.role === 'admin' ? (
              <>
                <Button variant='outline' onClick={() => autoBind.mutate()} disabled={autoBind.isPending}>{autoBind.isPending ? t('repos.autoBindComplete') : t('repos.autoBind')}</Button>
                <Button variant='outline' onClick={() => webhookRepair.mutate({ force: false })} disabled={webhookRepair.isPending}>{webhookRepair.isPending ? t('repoDetail.webhookRepairing') : t('repos.repairWebhooks')}</Button>
              </>
            ) : null}
            <Button onClick={openAddDialog}>{t('repos.addRepo')}</Button>
          </div>
        }
      />
      <div className='flex flex-wrap items-center gap-2'>
        <Select value={search.binding} onValueChange={(value) => replaceSearch({ ...search, binding: value as RepoBindingFilter, page: 1 })}>
          <SelectTrigger><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value='all'>{t('repos.allBindings')}</SelectItem>
            <SelectItem value='bound'>{t('repos.bound')}</SelectItem>
            <SelectItem value='unbound'>{t('repos.unbound')}</SelectItem>
          </SelectContent>
        </Select>
        <Select value={String(search.pageSize)} onValueChange={(value) => replaceSearch({ ...search, pageSize: Number(value), page: 1 })}>
          <SelectTrigger><SelectValue /></SelectTrigger>
          <SelectContent>
            <SelectItem value='20'>20 / page</SelectItem>
            <SelectItem value='50'>50 / page</SelectItem>
            <SelectItem value='100'>100 / page</SelectItem>
          </SelectContent>
        </Select>
        <Button variant='ghost' onClick={() => replaceSearch({ ...search, binding: 'unbound', provider: 'unbound', page: 1 })}>{t('repos.reviewNeedsBinding')}</Button>
      </div>
      <div className='grid gap-4 sm:grid-cols-4'>
        <MetricCard label={t('repos.totalRepositories')} value={number(health.total, locale)} />
        <MetricCard label={t('repos.boundRepositories')} value={number(health.bound, locale)} accent />
        <MetricCard label={t('repos.unbound')} value={number(health.unbound, locale)} />
        <MetricCard label={t('repos.activeConfigs')} value={number(health.active, locale)} />
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
          <CardHeader className='gap-4'>
            <div>
              <CardTitle>{t('repos.selectedScope')}</CardTitle>
              <p className='mt-1 text-muted-foreground text-sm'>{t('repos.healthHelp')}</p>
            </div>
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
          </CardHeader>
          <div className='grid min-h-[520px] lg:grid-cols-[280px_minmax(0,1fr)]'>
            <aside className='border-t border-border bg-muted/35 p-4 lg:border-r'>
              <div className='mb-3 flex items-center justify-between gap-2'>
                <div className='font-semibold text-sm'>{t('repos.scopeSearch')}</div>
                <Badge variant='secondary'>{number(selectedProvider?.scopes.length ?? 0, locale)}</Badge>
              </div>
              <div className='flex max-h-[430px] flex-col gap-1 overflow-y-auto'>
                {(selectedProvider?.scopes ?? []).map((scope) => (
                  <Button
                    key={scope.scope}
                    variant={scope.scope === selectedScope ? 'default' : 'ghost'}
                    className='h-auto justify-between gap-3 px-3 py-2 text-left'
                    onClick={() => replaceSearch({ ...search, scope: scope.scope, page: 1 })}
                  >
                    <span className='min-w-0 truncate'>{scope.scope}</span>
                    <span className='text-xs opacity-80'>{number(scope.total_repos, locale)}</span>
                  </Button>
                ))}
              </div>
            </aside>
            <section className='min-w-0 border-t border-border'>
              <div className='flex flex-col gap-2 border-b border-border px-5 py-4 md:flex-row md:items-center md:justify-between'>
                <div className='min-w-0'>
                  <div className='text-muted-foreground text-xs'>{selectedProvider?.name ?? t('common.empty')}</div>
                  <h2 className='truncate font-semibold text-lg'>{selectedScope || t('repos.selectedScope')}</h2>
                </div>
                <div className='text-muted-foreground text-sm'>{number(total, locale)} {t('repos.selectedScope')}</div>
              </div>
              {repos.isLoading ? (
                <LoadingState />
              ) : rows.length === 0 ? (
                <div className='p-8'>
                  <Empty><EmptyHeader><EmptyTitle>{t('common.empty')}</EmptyTitle><EmptyDescription>{t('repos.healthHelp')}</EmptyDescription></EmptyHeader></Empty>
                </div>
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
        <CardFooter className='flex-wrap justify-between gap-3 text-sm'>
          <span className='text-muted-foreground'>{t('common.pageCount', { current: search.page, total: Math.max(1, Math.ceil(total / search.pageSize)) })} · {number(total, locale)} {t('repos.totalRepositories')}</span>
          <div className='flex items-center gap-2'>
            <Button variant='outline' size='sm' onClick={() => replaceSearch({ ...search, page: Math.max(1, search.page - 1) })} disabled={!canPreviousPage || repos.isFetching}>{t('common.previous')}</Button>
            <Button variant='outline' size='sm' onClick={() => replaceSearch({ ...search, page: search.page + 1 })} disabled={!canNextPage || repos.isFetching}>{t('common.next')}</Button>
          </div>
        </CardFooter>
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
  return (
    <div className='overflow-x-auto'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('repos.repository')}</TableHead>
            <TableHead>{t('events.binding')}</TableHead>
            <TableHead>{t('repos.scmProvider')}</TableHead>
            <TableHead>{t('repos.defaultBranch')}</TableHead>
            <TableHead>{t('common.status')}</TableHead>
            <TableHead />
          </TableRow>
        </TableHeader>
        <TableBody>
          {rows.map((repo) => (
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
              <TableCell className='text-right'>
                {deleteConfirmId === repo.id ? (
                  <div className='flex justify-end gap-2'>
                    <Button variant='destructive' size='sm' onClick={() => deleteRepo(repo.id)} disabled={deletePending}>{t('common.confirm')}</Button>
                    <Button variant='ghost' size='sm' onClick={() => setDeleteConfirmId(null)}>{t('common.cancel')}</Button>
                  </div>
                ) : (
                  <Button variant='ghost' size='sm' onClick={() => setDeleteConfirmId(repo.id)}>{t('common.delete')}</Button>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
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
  return (
    <Dialog open={open} onOpenChange={setOpen}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('repos.addRepo')}</DialogTitle>
          <DialogDescription>{t('repos.pasteRepoUrl')}</DialogDescription>
        </DialogHeader>
        <div className='flex flex-col gap-3'>
          <Select value={selectedProviderId} onValueChange={setSelectedProviderId}>
            <SelectTrigger className='w-full'><SelectValue placeholder={t('repos.selectScmProvider')} /></SelectTrigger>
            <SelectContent>
              {providers.map((provider) => (
                <SelectItem key={provider.id} value={String(provider.id)}>{provider.name}</SelectItem>
              ))}
            </SelectContent>
          </Select>
          <Input
            placeholder='https://github.com/org/repo or https://bitbucket.example.com/projects/PROJ/repos/name/browse'
            value={repoUrl}
            onChange={(event) => {
              setRepoUrl(event.target.value)
              setAddError('')
              setCloneProtocol('http')
            }}
          />
          {parsedRepo ? (
            <div className='rounded-md border border-border bg-muted/40 p-3 text-sm'>
              <div className='flex justify-between gap-3'><span className='text-muted-foreground'>{t('repos.fullName')}</span><span className='font-medium'>{parsedRepo.project}/{parsedRepo.repo}</span></div>
              <div className='mt-1 flex justify-between gap-3'><span className='text-muted-foreground'>{t('repos.provider')}</span><span>{selectedProvider?.name || t('repos.noMatchingProvider')}</span></div>
              <div className='mt-3 flex flex-wrap items-center gap-2'>
                <span className='text-muted-foreground'>{t('repos.clone')}</span>
                <Button variant={cloneProtocol === 'http' ? 'default' : 'outline'} size='sm' onClick={() => setCloneProtocol('http')}>HTTP</Button>
                <Button variant={cloneProtocol === 'ssh' ? 'default' : 'outline'} size='sm' onClick={() => setCloneProtocol('ssh')}>SSH</Button>
              </div>
              {cloneProtocol === 'ssh' && parsedRepo.type === 'bitbucket' ? (
                <Input className='mt-2' placeholder={t('settings.sshHostExample')} value={sshHost} onChange={(event) => setSshHost(event.target.value)} />
              ) : null}
              <Input className='mt-2 font-mono text-xs' value={previewCloneUrl} readOnly />
            </div>
          ) : repoUrl ? (
            <AppAlert tone='warning' title={t('repos.enterRepoUrl')} />
          ) : null}
          <Input placeholder={t('repos.defaultBranch')} value={defaultBranch} onChange={(event) => setDefaultBranch(event.target.value)} />
          {addError ? <AppAlert tone='error' title={addError} /> : null}
          <div className='flex justify-end gap-2'>
            <Button variant='outline' onClick={() => setOpen(false)}>{t('common.cancel')}</Button>
            <Button disabled={!selectedProviderId || !parsedRepo || createPending} onClick={createRepo}>{t('common.create')}</Button>
          </div>
        </div>
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
