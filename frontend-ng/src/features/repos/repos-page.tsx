import { Link } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
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

export function ReposPage() {
  const qc = useQueryClient()
  const [showAdd, setShowAdd] = useState(false)
  const [repoForm, setRepoForm] = useState({ scm_provider_id: '', name: '', full_name: '', clone_url: '', default_branch: 'main' })
  const repos = useQuery({ queryKey: ['repos'], queryFn: () => api.repos.list(1, 100) })
  const scm = useQuery({ queryKey: ['settings', 'scm'], queryFn: () => api.settings.scmProviders(1, 100) })
  const autoBind = useMutation({
    mutationFn: api.repos.autoBindUnbound,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['repos'] })
  })
  const createRepo = useMutation({
    mutationFn: () => api.repos.createDirect({
      scm_provider_id: Number(repoForm.scm_provider_id),
      name: repoForm.name.trim(),
      full_name: repoForm.full_name.trim(),
      clone_url: repoForm.clone_url.trim(),
      default_branch: repoForm.default_branch.trim() || 'main'
    }),
    onSuccess: () => {
      setShowAdd(false)
      setRepoForm({ scm_provider_id: '', name: '', full_name: '', clone_url: '', default_branch: 'main' })
      void qc.invalidateQueries({ queryKey: ['repos'] })
      toast.success('Repository added')
    }
  })
  const deleteRepo = useMutation({
    mutationFn: api.repos.delete,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['repos'] })
      toast.success('Repository deleted')
    }
  })

  if (repos.isLoading) return <LoadingState />
  const items = repos.data?.items ?? []
  const unbound = items.filter((repo) => repo.binding_state === 'unbound').length

  return (
    <Page>
      <PageHeader
        title='Repositories'
        description='Repository health, SCM binding state, and PR usage freshness come from the existing Go backend APIs.'
        actions={
          <div className='flex gap-2'>
            <Button variant='outline' onClick={() => autoBind.mutate()} disabled={autoBind.isPending}>Auto-bind unbound</Button>
            <Button onClick={() => {
              const firstProvider = scm.data?.items?.[0]
              setRepoForm((value) => ({ ...value, scm_provider_id: firstProvider ? String(firstProvider.id) : value.scm_provider_id }))
              setShowAdd(true)
            }}>Add repo</Button>
          </div>
        }
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
                <TableCell>
                  <div className='flex items-center gap-2'>
                    <StatusBadge value={repo.status} />
                    <Button
                      variant='ghost'
                      size='sm'
                      onClick={() => {
                        if (window.confirm(`Delete ${repo.full_name || repo.name}?`)) deleteRepo.mutate(repo.id)
                      }}
                      disabled={deleteRepo.isPending}
                    >
                      Delete
                    </Button>
                  </div>
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      </div>
      </Card>
      <Dialog open={showAdd} onOpenChange={setShowAdd}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add repository</DialogTitle>
            <DialogDescription>Creates a repository config through the existing direct repo API.</DialogDescription>
          </DialogHeader>
          <div className='flex flex-col gap-3'>
            <select
              className='h-8 rounded-md border border-input bg-card px-3 text-sm'
              value={repoForm.scm_provider_id}
              onChange={(event) => setRepoForm((value) => ({ ...value, scm_provider_id: event.target.value }))}
            >
              <option value=''>Select SCM provider</option>
              {(scm.data?.items ?? []).map((provider) => (
                <option key={provider.id} value={provider.id}>{provider.name}</option>
              ))}
            </select>
            <Input placeholder='Full name, for example org/repo' value={repoForm.full_name} onChange={(event) => {
              const fullName = event.target.value
              setRepoForm((value) => ({ ...value, full_name: fullName, name: value.name || fullName.split('/').pop() || '' }))
            }} />
            <Input placeholder='Name' value={repoForm.name} onChange={(event) => setRepoForm((value) => ({ ...value, name: event.target.value }))} />
            <Input placeholder='Clone URL' value={repoForm.clone_url} onChange={(event) => setRepoForm((value) => ({ ...value, clone_url: event.target.value }))} />
            <Input placeholder='Default branch' value={repoForm.default_branch} onChange={(event) => setRepoForm((value) => ({ ...value, default_branch: event.target.value }))} />
            {createRepo.error ? <div className='text-[var(--ae-warn)] text-sm'>{createRepo.error.message}</div> : null}
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={() => setShowAdd(false)}>Cancel</Button>
              <Button
                disabled={!repoForm.scm_provider_id || !repoForm.name.trim() || !repoForm.full_name.trim() || !repoForm.clone_url.trim() || createRepo.isPending}
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
