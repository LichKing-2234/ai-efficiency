import { Link } from '@tanstack/react-router'
import { keepPreviousData, useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { MetricCard } from '@/components/primitives/metric-card'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { number } from '@/lib/format'
import {
  applyBindingFilter,
  buildRepoCloneUrl,
  buildRepoCreatePayload,
  groupRepos,
  healthSummary,
  parseRepoUrl,
  selectProviderForRepoOrigin,
  type ParsedRepoUrl,
  type RepoBindingFilter,
  type RepoCloneProtocol
} from './repos-state'

function initialBindingFilter(): RepoBindingFilter {
  if (typeof window === 'undefined') return 'all'
  const value = new URL(window.location.href).searchParams.get('binding')
  return value === 'bound' || value === 'unbound' ? value : 'all'
}

export function ReposPage() {
  const qc = useQueryClient()
  const [showAdd, setShowAdd] = useState(false)
  const [bindingFilter, setBindingFilter] = useState<RepoBindingFilter>(initialBindingFilter)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(20)
  const [collapsedGroups, setCollapsedGroups] = useState<string[]>([])
  const [deleteConfirmId, setDeleteConfirmId] = useState<number | null>(null)
  const [repoUrl, setRepoUrl] = useState('')
  const [selectedProviderId, setSelectedProviderId] = useState('')
  const [cloneProtocol, setCloneProtocol] = useState<RepoCloneProtocol>('http')
  const [sshHost, setSshHost] = useState('')
  const [defaultBranch, setDefaultBranch] = useState('main')
  const [addError, setAddError] = useState('')
  const [autoBindMessage, setAutoBindMessage] = useState('')
  const parsedRepo = useMemo(() => parseRepoUrl(repoUrl), [repoUrl])
  const repos = useQuery({
    queryKey: ['repos', page, pageSize],
    queryFn: () => api.repos.list(page, pageSize),
    placeholderData: keepPreviousData
  })
  const scm = useQuery({ queryKey: ['settings', 'scm'], queryFn: () => api.settings.scmProviders(1, 100) })
  const autoBind = useMutation({
    mutationFn: api.repos.autoBindUnbound,
    onSuccess: (result) => {
      const summary = result.summary
      setAutoBindMessage(`Auto-bind complete: ${summary.bound} bound, ${summary.skipped_no_match} no match, ${summary.skipped_ambiguous} ambiguous, ${summary.webhook_failed} webhook failed, ${summary.errors} errors.`)
      void qc.invalidateQueries({ queryKey: ['repos'] })
    },
    onError: (error) => {
      setAutoBindMessage(error instanceof Error ? error.message : 'Auto-bind failed.')
    }
  })
  const createRepo = useMutation({
    mutationFn: () => {
      if (!parsedRepo) throw new Error('Enter a GitHub or Bitbucket repository URL.')
      if (!selectedProviderId) throw new Error('Select an SCM provider.')
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
      void qc.invalidateQueries({ queryKey: ['repos'] })
      toast.success('Repository added')
    },
    onError: (error) => {
      setAddError(error instanceof Error ? error.message : 'Repository add failed.')
    }
  })
  const deleteRepo = useMutation({
    mutationFn: api.repos.delete,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['repos'] })
      toast.success('Repository deleted')
    }
  })

  const providers = scm.data?.items ?? []
  const selectedProvider = providers.find((provider) => String(provider.id) === selectedProviderId)
  const previewCloneUrl = parsedRepo ? buildRepoCloneUrl(parsedRepo, cloneProtocol, sshHost) : ''

  useEffect(() => {
    if (typeof window === 'undefined') return
    const url = new URL(window.location.href)
    if (bindingFilter === 'all') {
      url.searchParams.delete('binding')
    } else {
      url.searchParams.set('binding', bindingFilter)
    }
    window.history.replaceState(null, '', `${url.pathname}${url.search}${url.hash}`)
  }, [bindingFilter])

  useEffect(() => {
    if (!parsedRepo) return
    const providerId = selectProviderForRepoOrigin(providers, parsedRepo.origin)
    if (providerId) setSelectedProviderId(String(providerId))
  }, [parsedRepo, providers])

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

  function toggleGroup(key: string) {
    setCollapsedGroups((value) => value.includes(key) ? value.filter((item) => item !== key) : [...value, key])
  }

  if (repos.isLoading) return <LoadingState />
  const rows = repos.data?.items ?? []
  const summary = healthSummary(rows)
  const filteredRows = applyBindingFilter(rows, bindingFilter)
  const groups = groupRepos(filteredRows)
  const total = repos.data?.total ?? rows.length
  const canPreviousPage = page > 1
  const canNextPage = page * pageSize < total

  return (
    <Page>
      <PageHeader
        title='Repositories'
        description='Repository health, SCM binding state, and PR usage freshness come from the existing Go backend APIs.'
        actions={
          <div className='flex gap-2'>
            <Button variant='outline' onClick={() => autoBind.mutate()} disabled={autoBind.isPending}>Auto-bind unbound</Button>
            <Button onClick={openAddDialog}>Add repo</Button>
          </div>
        }
      />
      <div className='flex flex-wrap items-center gap-2'>
        <select
          className='h-8 rounded-md border border-input bg-card px-3 text-sm'
          value={bindingFilter}
          onChange={(event) => {
            setBindingFilter(event.target.value as RepoBindingFilter)
            setPage(1)
          }}
        >
          <option value='all'>All bindings</option>
          <option value='bound'>Bound</option>
          <option value='unbound'>Needs binding</option>
        </select>
        <select
          className='h-8 rounded-md border border-input bg-card px-3 text-sm'
          value={pageSize}
          onChange={(event) => {
            setPageSize(Number(event.target.value))
            setPage(1)
          }}
        >
          <option value={20}>20 / page</option>
          <option value={50}>50 / page</option>
          <option value={100}>100 / page</option>
        </select>
        <Button variant='ghost' onClick={() => setBindingFilter('unbound')}>Review needs binding</Button>
      </div>
      <div className='grid gap-4 sm:grid-cols-4'>
        <MetricCard label='Tracked repos' value={number(summary.total)} />
        <MetricCard label='Bound repos' value={number(summary.bound)} accent />
        <MetricCard label='Unbound repos' value={number(summary.unbound)} />
        <MetricCard label='Active configs' value={number(summary.active)} />
      </div>
      {autoBindMessage ? (
        <Card>
          <CardContent className='p-4 text-sm'>
            {autoBindMessage}
          </CardContent>
        </Card>
      ) : null}
      {groups.length === 0 ? (
        <Card><CardContent className='p-8 text-center text-muted-foreground text-sm'>No repositories match this filter.</CardContent></Card>
      ) : groups.map((group) => (
        <Card key={group.key} className='overflow-hidden'>
          <CardHeader className='flex-row items-center justify-between gap-3 bg-muted/40'>
            <button className='flex min-w-0 items-center gap-2 text-left' type='button' onClick={() => toggleGroup(group.key)} aria-expanded={!collapsedGroups.includes(group.key)}>
              <Badge variant='secondary'>{group.scmType || 'unbound'}</Badge>
              <CardTitle className='truncate'>{group.org}</CardTitle>
              <span className='text-muted-foreground text-sm'>{group.scmName} · {group.repos.length}</span>
            </button>
          </CardHeader>
          {!collapsedGroups.includes(group.key) ? (
            <div className='overflow-x-auto'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Repository</TableHead>
                    <TableHead>Binding</TableHead>
                    <TableHead>SCM Provider</TableHead>
                    <TableHead>Default Branch</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead />
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {group.repos.map((repo) => (
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
                            <Button variant='destructive' size='sm' onClick={() => deleteRepo.mutate(repo.id)} disabled={deleteRepo.isPending}>Confirm</Button>
                            <Button variant='ghost' size='sm' onClick={() => setDeleteConfirmId(null)}>Cancel</Button>
                          </div>
                        ) : (
                          <Button variant='ghost' size='sm' onClick={() => setDeleteConfirmId(repo.id)}>Delete</Button>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : null}
        </Card>
      ))}
      <Card>
        <CardFooter className='flex-wrap justify-between gap-3 text-sm'>
          <span className='text-muted-foreground'>Page {number(page)} · {number(total)} repositories</span>
          <div className='flex items-center gap-2'>
            <Button variant='outline' size='sm' onClick={() => setPage((value) => Math.max(1, value - 1))} disabled={!canPreviousPage || repos.isFetching}>Previous</Button>
            <Button variant='outline' size='sm' onClick={() => setPage((value) => value + 1)} disabled={!canNextPage || repos.isFetching}>Next</Button>
          </div>
        </CardFooter>
      </Card>
      <Dialog open={showAdd} onOpenChange={setShowAdd}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add repository</DialogTitle>
            <DialogDescription>Paste a GitHub or Bitbucket repository URL. The direct repo payload is generated from the parsed repository.</DialogDescription>
          </DialogHeader>
          <div className='flex flex-col gap-3'>
            <select
              className='h-8 rounded-md border border-input bg-card px-3 text-sm'
              value={selectedProviderId}
              onChange={(event) => setSelectedProviderId(event.target.value)}
            >
              <option value=''>Select SCM provider</option>
              {providers.map((provider) => (
                <option key={provider.id} value={provider.id}>{provider.name}</option>
              ))}
            </select>
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
                <div className='flex justify-between gap-3'><span className='text-muted-foreground'>Full name</span><span className='font-medium'>{parsedRepo.project}/{parsedRepo.repo}</span></div>
                <div className='mt-1 flex justify-between gap-3'><span className='text-muted-foreground'>Provider</span><span>{selectedProvider?.name || 'No matching provider selected'}</span></div>
                <div className='mt-3 flex flex-wrap items-center gap-2'>
                  <span className='text-muted-foreground'>Clone</span>
                  <Button variant={cloneProtocol === 'http' ? 'default' : 'outline'} size='sm' onClick={() => setCloneProtocol('http')}>HTTP</Button>
                  <Button variant={cloneProtocol === 'ssh' ? 'default' : 'outline'} size='sm' onClick={() => setCloneProtocol('ssh')}>SSH</Button>
                </div>
                {cloneProtocol === 'ssh' && parsedRepo.type === 'bitbucket' ? (
                  <Input className='mt-2' placeholder='SSH host, for example git.example.com' value={sshHost} onChange={(event) => setSshHost(event.target.value)} />
                ) : null}
                <Input className='mt-2 font-mono text-xs' value={previewCloneUrl} readOnly />
              </div>
            ) : repoUrl ? (
              <div className='text-[var(--ae-warn)] text-sm'>Enter a GitHub repo URL or Bitbucket Server browse URL.</div>
            ) : null}
            <Input placeholder='Default branch' value={defaultBranch} onChange={(event) => setDefaultBranch(event.target.value)} />
            {addError ? <div className='text-[var(--ae-warn)] text-sm'>{addError}</div> : null}
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={() => setShowAdd(false)}>Cancel</Button>
              <Button
                disabled={!selectedProviderId || !parsedRepo || createRepo.isPending}
                onClick={() => createRepo.mutate()}
              >
                Create
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </Page>
  )
}
