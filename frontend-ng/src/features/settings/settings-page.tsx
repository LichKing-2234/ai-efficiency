import { createFileRoute, redirect } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { ensureAuthenticatedUser } from '@/lib/auth/session'
import { dateTime, number } from '@/lib/format'
import type { CredentialPayload } from '@/lib/api/types'

export function SettingsPage() {
  const qc = useQueryClient()
  const [relayDialog, setRelayDialog] = useState(false)
  const [relayForm, setRelayForm] = useState({ name: '', display_name: '', base_url: '', admin_api_key: '', is_primary: false, enabled: true })
  const [scmDialog, setScmDialog] = useState(false)
  const [scmForm, setScmForm] = useState({ name: '', type: 'github', base_url: '', api_credential_id: '', clone_protocol: 'https' as 'https' | 'ssh', clone_credential_id: '', ssh_host: '' })
  const [credentialDialog, setCredentialDialog] = useState(false)
  const [credentialForm, setCredentialForm] = useState<CredentialPayload>({ name: '', description: '', kind: 'secret_text', text: '', username: '', password: '', private_key: '' })
  const relay = useQuery({ queryKey: ['settings', 'relay'], queryFn: api.settings.relayProviders })
  const scm = useQuery({ queryKey: ['settings', 'scm'], queryFn: () => api.settings.scmProviders(1, 100) })
  const credentials = useQuery({ queryKey: ['settings', 'credentials'], queryFn: api.settings.credentials })
  const deployment = useQuery({ queryKey: ['settings', 'deployment'], queryFn: api.settings.deployment })
  const checkUpdate = useMutation({
    mutationFn: api.settings.checkUpdate,
    onSuccess: () => qc.invalidateQueries({ queryKey: ['settings', 'deployment'] })
  })
  const restart = useMutation({
    mutationFn: api.settings.restart,
    onSuccess: () => toast.success('Restart requested')
  })
  const applyUpdate = useMutation({
    mutationFn: () => api.settings.applyUpdate({ target_version: deployment.data?.latest_release?.version || '' }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['settings', 'deployment'] })
      toast.success('Update staged')
    }
  })
  const rollback = useMutation({
    mutationFn: api.settings.rollback,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['settings', 'deployment'] })
      toast.success('Rollback staged')
    }
  })
  const createRelay = useMutation({
    mutationFn: () => api.settings.createRelayProvider(relayForm),
    onSuccess: () => {
      setRelayDialog(false)
      setRelayForm({ name: '', display_name: '', base_url: '', admin_api_key: '', is_primary: false, enabled: true })
      void qc.invalidateQueries({ queryKey: ['settings', 'relay'] })
      toast.success('Relay provider created')
    }
  })
  const deleteRelay = useMutation({
    mutationFn: api.settings.deleteRelayProvider,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['settings', 'relay'] })
      toast.success('Relay provider deleted')
    }
  })
  const createScm = useMutation({
    mutationFn: () => api.settings.createSCMProvider({
      name: scmForm.name.trim(),
      type: scmForm.type,
      base_url: scmForm.base_url.trim(),
      api_credential_id: Number(scmForm.api_credential_id),
      clone_protocol: scmForm.clone_protocol,
      clone_credential_id: scmForm.clone_protocol === 'ssh' && scmForm.clone_credential_id ? Number(scmForm.clone_credential_id) : null,
      ssh_host: scmForm.ssh_host.trim() || null
    }),
    onSuccess: () => {
      setScmDialog(false)
      setScmForm({ name: '', type: 'github', base_url: '', api_credential_id: '', clone_protocol: 'https', clone_credential_id: '', ssh_host: '' })
      void qc.invalidateQueries({ queryKey: ['settings', 'scm'] })
      toast.success('SCM provider created')
    }
  })
  const deleteScm = useMutation({
    mutationFn: api.settings.deleteSCMProvider,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['settings', 'scm'] })
      toast.success('SCM provider deleted')
    }
  })
  const createCredential = useMutation({
    mutationFn: () => api.settings.createCredential(credentialForm),
    onSuccess: () => {
      setCredentialDialog(false)
      setCredentialForm({ name: '', description: '', kind: 'secret_text', text: '', username: '', password: '', private_key: '' })
      void qc.invalidateQueries({ queryKey: ['settings', 'credentials'] })
      toast.success('Credential created')
    }
  })
  const deleteCredential = useMutation({
    mutationFn: api.settings.deleteCredential,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['settings', 'credentials'] })
      toast.success('Credential deleted')
    }
  })

  if (relay.isLoading || scm.isLoading || deployment.isLoading) return <LoadingState />

  return (
    <Page>
      <PageHeader title='Admin Console' description='Task-zone settings backed by current Go APIs. Mutating deployment actions require explicit confirmation.' />
      <div className='grid gap-4 lg:grid-cols-2'>
        <Card>
          <CardHeader>
            <div className='flex items-center justify-between gap-2'>
              <CardTitle>AI Services</CardTitle>
              <Button size='sm' onClick={() => {
                setRelayForm((value) => ({ ...value, is_primary: (relay.data ?? []).length === 0 }))
                setRelayDialog(true)
              }}>Add</Button>
            </div>
            <CardDescription>Relay providers configured in backend.</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            {(relay.data ?? []).map((provider) => (
              <div key={provider.id} className='flex items-center justify-between gap-3 rounded-md bg-muted p-3'>
                <div>
                  <div className='font-medium'>{provider.display_name || provider.name}</div>
                  <div className='text-muted-foreground text-xs'>{provider.base_url}</div>
                </div>
                <div className='flex items-center gap-2'>
                  {provider.is_primary ? <Badge variant='ai'>primary</Badge> : null}
                  <StatusBadge value={provider.enabled ? 'active' : 'disabled'} />
                  <Button
                    size='sm'
                    variant='ghost'
                    onClick={() => {
                      if (window.confirm(`Delete relay provider ${provider.display_name || provider.name}?`)) deleteRelay.mutate(provider.id)
                    }}
                    disabled={deleteRelay.isPending}
                  >
                    Delete
                  </Button>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <div className='flex items-center justify-between gap-2'>
              <CardTitle>Code Platforms</CardTitle>
              <Button size='sm' onClick={() => setScmDialog(true)}>Add</Button>
            </div>
            <CardDescription>SCM providers and clone bindings.</CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Name</TableHead>
                  <TableHead>Type</TableHead>
                  <TableHead>Status</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(scm.data?.items ?? []).map((provider) => (
                  <TableRow key={provider.id}>
                    <TableCell>{provider.name}</TableCell>
                    <TableCell>{provider.type}</TableCell>
                    <TableCell>
                      <div className='flex items-center gap-2'>
                        <StatusBadge value={provider.status} />
                        <Button
                          size='sm'
                          variant='ghost'
                          onClick={() => {
                            if (window.confirm(`Delete SCM provider ${provider.name}?`)) deleteScm.mutate(provider.id)
                          }}
                          disabled={deleteScm.isPending}
                        >
                          Delete
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <div className='flex items-center justify-between gap-2'>
              <CardTitle>Advanced Credentials</CardTitle>
              <Button size='sm' onClick={() => setCredentialDialog(true)}>Add</Button>
            </div>
            <CardDescription>Reusable secrets referenced by providers.</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-2'>
            {(credentials.data ?? []).map((credential) => (
              <div key={credential.id} className='flex items-center justify-between rounded-md bg-muted p-3'>
                <div>
                  <div className='font-medium'>{credential.name}</div>
                  <div className='text-muted-foreground text-xs'>{credential.kind} · used {number(credential.usage_count)} times</div>
                </div>
                <div className='flex items-center gap-2'>
                  <span className='text-muted-foreground text-xs'>{dateTime(credential.updated_at)}</span>
                  <Button
                    size='sm'
                    variant='ghost'
                    onClick={() => {
                      if (window.confirm(`Delete credential ${credential.name}?`)) deleteCredential.mutate(credential.id)
                    }}
                    disabled={deleteCredential.isPending}
                  >
                    Delete
                  </Button>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
        <Card>
          <CardHeader>
            <CardTitle>Deployment & Runtime</CardTitle>
            <CardDescription>Current backend deployment status.</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            <div className='rounded-md bg-muted p-3'>
              <div className='font-medium'>v{deployment.data?.version.version || '-'}</div>
              <div className='text-muted-foreground text-xs'>{deployment.data?.mode || 'unknown'} · {deployment.data?.version.commit || '-'}</div>
            </div>
            {deployment.data?.update_available ? <Badge variant='ai'>Update available: v{deployment.data.latest_release?.version}</Badge> : <Badge variant='success'>Up to date</Badge>}
            <div className='flex gap-2'>
              <Button variant='outline' onClick={() => checkUpdate.mutate()} disabled={checkUpdate.isPending}>Check update</Button>
              <Button
                variant='outline'
                onClick={() => {
                  if (window.confirm(`Stage update ${deployment.data?.latest_release?.version || ''}?`)) applyUpdate.mutate()
                }}
                disabled={!deployment.data?.latest_release?.version || applyUpdate.isPending}
              >
                Apply update
              </Button>
              <Button
                variant='outline'
                onClick={() => {
                  if (window.confirm('Rollback staged update?')) rollback.mutate()
                }}
                disabled={rollback.isPending}
              >
                Rollback
              </Button>
              <Button
                variant='outline'
                onClick={() => {
                  if (window.confirm('Request backend restart?')) restart.mutate()
                }}
                disabled={restart.isPending}
              >
                Restart
              </Button>
            </div>
          </CardContent>
        </Card>
      </div>
      <Dialog open={relayDialog} onOpenChange={setRelayDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add relay provider</DialogTitle>
            <DialogDescription>Creates a backend relay provider; admin API key is sent only to the Go API.</DialogDescription>
          </DialogHeader>
          <div className='flex flex-col gap-3'>
            <Input placeholder='Name' value={relayForm.name} onChange={(event) => setRelayForm((value) => ({ ...value, name: event.target.value }))} />
            <Input placeholder='Display name' value={relayForm.display_name} onChange={(event) => setRelayForm((value) => ({ ...value, display_name: event.target.value }))} />
            <Input placeholder='Base URL' value={relayForm.base_url} onChange={(event) => setRelayForm((value) => ({ ...value, base_url: event.target.value }))} />
            <Input type='password' placeholder='Admin API key' value={relayForm.admin_api_key} onChange={(event) => setRelayForm((value) => ({ ...value, admin_api_key: event.target.value }))} />
            <label className='flex items-center gap-2 text-sm'><input type='checkbox' checked={relayForm.is_primary} onChange={(event) => setRelayForm((value) => ({ ...value, is_primary: event.target.checked }))} /> Primary</label>
            <label className='flex items-center gap-2 text-sm'><input type='checkbox' checked={relayForm.enabled} onChange={(event) => setRelayForm((value) => ({ ...value, enabled: event.target.checked }))} /> Enabled</label>
            {createRelay.error ? <div className='text-[var(--ae-warn)] text-sm'>{createRelay.error.message}</div> : null}
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={() => setRelayDialog(false)}>Cancel</Button>
              <Button disabled={!relayForm.name || !relayForm.display_name || !relayForm.base_url || !relayForm.admin_api_key || createRelay.isPending} onClick={() => createRelay.mutate()}>Create</Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
      <Dialog open={scmDialog} onOpenChange={setScmDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add SCM provider</DialogTitle>
            <DialogDescription>Creates a code platform provider using existing admin credentials.</DialogDescription>
          </DialogHeader>
          <div className='flex flex-col gap-3'>
            <Input placeholder='Name' value={scmForm.name} onChange={(event) => setScmForm((value) => ({ ...value, name: event.target.value }))} />
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={scmForm.type} onChange={(event) => setScmForm((value) => ({ ...value, type: event.target.value }))}>
              <option value='github'>GitHub</option>
              <option value='bitbucket'>Bitbucket</option>
            </select>
            <Input placeholder='Base URL' value={scmForm.base_url} onChange={(event) => setScmForm((value) => ({ ...value, base_url: event.target.value }))} />
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={scmForm.api_credential_id} onChange={(event) => setScmForm((value) => ({ ...value, api_credential_id: event.target.value }))}>
              <option value=''>API credential</option>
              {(credentials.data ?? []).map((credential) => <option key={credential.id} value={credential.id}>{credential.name}</option>)}
            </select>
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={scmForm.clone_protocol} onChange={(event) => setScmForm((value) => ({ ...value, clone_protocol: event.target.value as 'https' | 'ssh' }))}>
              <option value='https'>HTTPS clone</option>
              <option value='ssh'>SSH clone</option>
            </select>
            {scmForm.clone_protocol === 'ssh' ? (
              <>
                <Input placeholder='SSH host' value={scmForm.ssh_host} onChange={(event) => setScmForm((value) => ({ ...value, ssh_host: event.target.value }))} />
                <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={scmForm.clone_credential_id} onChange={(event) => setScmForm((value) => ({ ...value, clone_credential_id: event.target.value }))}>
                  <option value=''>Clone credential</option>
                  {(credentials.data ?? []).map((credential) => <option key={credential.id} value={credential.id}>{credential.name}</option>)}
                </select>
              </>
            ) : null}
            {createScm.error ? <div className='text-[var(--ae-warn)] text-sm'>{createScm.error.message}</div> : null}
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={() => setScmDialog(false)}>Cancel</Button>
              <Button disabled={!scmForm.name || !scmForm.base_url || !scmForm.api_credential_id || createScm.isPending} onClick={() => createScm.mutate()}>Create</Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
      <Dialog open={credentialDialog} onOpenChange={setCredentialDialog}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Add credential</DialogTitle>
            <DialogDescription>Creates a reusable admin credential for relay or SCM configuration.</DialogDescription>
          </DialogHeader>
          <div className='flex flex-col gap-3'>
            <Input placeholder='Name' value={credentialForm.name} onChange={(event) => setCredentialForm((value) => ({ ...value, name: event.target.value }))} />
            <Input placeholder='Description' value={credentialForm.description} onChange={(event) => setCredentialForm((value) => ({ ...value, description: event.target.value }))} />
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={credentialForm.kind} onChange={(event) => setCredentialForm((value) => ({ ...value, kind: event.target.value as typeof credentialForm.kind }))}>
              <option value='secret_text'>Secret text</option>
              <option value='username_password'>Username/password</option>
              <option value='ssh_username_with_private_key'>SSH private key</option>
            </select>
            {credentialForm.kind === 'secret_text' ? <Textarea placeholder='Secret text' value={credentialForm.text} onChange={(event) => setCredentialForm((value) => ({ ...value, text: event.target.value }))} /> : null}
            {credentialForm.kind !== 'secret_text' ? <Input placeholder='Username' value={credentialForm.username} onChange={(event) => setCredentialForm((value) => ({ ...value, username: event.target.value }))} /> : null}
            {credentialForm.kind === 'username_password' ? <Input type='password' placeholder='Password' value={credentialForm.password} onChange={(event) => setCredentialForm((value) => ({ ...value, password: event.target.value }))} /> : null}
            {credentialForm.kind === 'ssh_username_with_private_key' ? <Textarea placeholder='Private key' value={credentialForm.private_key} onChange={(event) => setCredentialForm((value) => ({ ...value, private_key: event.target.value }))} /> : null}
            {createCredential.error ? <div className='text-[var(--ae-warn)] text-sm'>{createCredential.error.message}</div> : null}
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={() => setCredentialDialog(false)}>Cancel</Button>
              <Button disabled={!credentialForm.name || createCredential.isPending} onClick={() => createCredential.mutate()}>Create</Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </Page>
  )
}

export const settingsRouteGuard = async () => {
  const user = await import('@/query-client').then(({ queryClient }) =>
    queryClient.fetchQuery({ queryKey: ['auth', 'me'], queryFn: ensureAuthenticatedUser })
  )
  if (user.role !== 'admin') throw redirect({ to: '/' })
}
