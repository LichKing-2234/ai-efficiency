import { createFileRoute, redirect } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
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
import type { Credential, RelayProvider, SCMProvider } from '@/lib/api/types'
import {
  buildCredentialPayload,
  buildSettingsSectionSearch,
  buildLDAPForm,
  buildLDAPPayload,
  buildRelayPayload,
  buildScmProviderPayload,
  type CredentialFormState,
  type LDAPFormState,
  type RelayFormState,
  type SettingsSection,
  settingsSectionFromSearch,
  settingsSections,
  type ScmFormState
} from './settings-payloads'

const emptyRelayForm: RelayFormState = { name: '', display_name: '', base_url: '', admin_api_key: '', is_primary: false, enabled: true }
const emptyScmForm: ScmFormState = { name: '', type: 'github', base_url: '', api_credential_id: '', clone_protocol: 'https', clone_credential_id: '', ssh_host: '' }
const emptyCredentialForm: CredentialFormState = { name: '', description: '', kind: 'secret_text', text: '', username: '', password: '', private_key: '', passphrase: '' }
const emptyLDAPForm: LDAPFormState = { url: '', base_dn: '', bind_dn: '', bind_password: '', user_filter: '(uid=%s)', tls: false }

export function SettingsPage() {
  const qc = useQueryClient()
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as Record<string, unknown>
  const activeSection = settingsSectionFromSearch(search)
  const [relayDialog, setRelayDialog] = useState(false)
  const [editingRelayId, setEditingRelayId] = useState<number | null>(null)
  const [relayForm, setRelayForm] = useState<RelayFormState>(emptyRelayForm)
  const [scmDialog, setScmDialog] = useState(false)
  const [editingScmId, setEditingScmId] = useState<number | null>(null)
  const [scmForm, setScmForm] = useState<ScmFormState>(emptyScmForm)
  const [credentialDialog, setCredentialDialog] = useState(false)
  const [editingCredentialId, setEditingCredentialId] = useState<number | null>(null)
  const [credentialForm, setCredentialForm] = useState<CredentialFormState>(emptyCredentialForm)
  const [ldapForm, setLDAPForm] = useState<LDAPFormState>(emptyLDAPForm)
  const [ldapMessage, setLDAPMessage] = useState('')
  const relay = useQuery({ queryKey: ['settings', 'relay'], queryFn: api.settings.relayProviders })
  const scm = useQuery({ queryKey: ['settings', 'scm'], queryFn: () => api.settings.scmProviders(1, 100) })
  const credentials = useQuery({ queryKey: ['settings', 'credentials'], queryFn: api.settings.credentials })
  const deployment = useQuery({ queryKey: ['settings', 'deployment'], queryFn: api.settings.deployment })
  const ldap = useQuery({ queryKey: ['settings', 'ldap'], queryFn: api.settings.ldap })
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
    mutationFn: () => api.settings.createRelayProvider(buildRelayPayload(relayForm, 'create')),
    onSuccess: () => {
      closeRelayDialog()
      void qc.invalidateQueries({ queryKey: ['settings', 'relay'] })
      toast.success('Relay provider created')
    }
  })
  const updateRelay = useMutation({
    mutationFn: () => api.settings.updateRelayProvider(editingRelayId ?? 0, buildRelayPayload(relayForm, 'edit')),
    onSuccess: () => {
      closeRelayDialog()
      void qc.invalidateQueries({ queryKey: ['settings', 'relay'] })
      toast.success('Relay provider updated')
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
    mutationFn: () => api.settings.createSCMProvider(buildScmProviderPayload(scmForm, 'create')),
    onSuccess: () => {
      closeScmDialog()
      void qc.invalidateQueries({ queryKey: ['settings', 'scm'] })
      toast.success('SCM provider created')
    }
  })
  const updateScm = useMutation({
    mutationFn: () => api.settings.updateSCMProvider(editingScmId ?? 0, buildScmProviderPayload(scmForm, 'edit')),
    onSuccess: () => {
      closeScmDialog()
      void qc.invalidateQueries({ queryKey: ['settings', 'scm'] })
      toast.success('SCM provider updated')
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
    mutationFn: () => api.settings.createCredential(buildCredentialPayload(credentialForm, 'create')),
    onSuccess: () => {
      closeCredentialDialog()
      void qc.invalidateQueries({ queryKey: ['settings', 'credentials'] })
      toast.success('Credential created')
    }
  })
  const updateCredential = useMutation({
    mutationFn: () => api.settings.updateCredential(editingCredentialId ?? 0, buildCredentialPayload(credentialForm, 'edit')),
    onSuccess: () => {
      closeCredentialDialog()
      void qc.invalidateQueries({ queryKey: ['settings', 'credentials'] })
      toast.success('Credential updated')
    }
  })
  const deleteCredential = useMutation({
    mutationFn: api.settings.deleteCredential,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['settings', 'credentials'] })
      toast.success('Credential deleted')
    }
  })
  const saveLDAP = useMutation({
    mutationFn: () => api.settings.updateLDAP(buildLDAPPayload(ldapForm)),
    onSuccess: () => {
      setLDAPMessage('LDAP configuration saved')
      void qc.invalidateQueries({ queryKey: ['settings', 'ldap'] })
      toast.success('LDAP configuration saved')
    },
    onError: (error) => {
      setLDAPMessage(error instanceof Error ? error.message : 'Failed to save LDAP configuration')
    }
  })
  const testLDAP = useMutation({
    mutationFn: () => api.settings.testLDAP(buildLDAPPayload(ldapForm)),
    onSuccess: () => {
      setLDAPMessage('LDAP connection successful')
      toast.success('LDAP connection successful')
    },
    onError: (error) => {
      setLDAPMessage(error instanceof Error ? error.message : 'LDAP connection test failed')
    }
  })

  useEffect(() => {
    if (ldap.data) setLDAPForm(buildLDAPForm(ldap.data))
  }, [ldap.data])

  if (relay.isLoading || scm.isLoading || deployment.isLoading) return <LoadingState />

  function selectSection(section: SettingsSection) {
    void navigate({ to: '/settings', search: buildSettingsSectionSearch(section) })
  }

  function openAddRelayDialog() {
    setEditingRelayId(null)
    setRelayForm({ ...emptyRelayForm, is_primary: (relay.data ?? []).length === 0 })
    setRelayDialog(true)
  }

  function openEditRelayDialog(provider: RelayProvider) {
    setEditingRelayId(provider.id)
    setRelayForm({
      name: provider.name,
      display_name: provider.display_name,
      base_url: provider.base_url,
      admin_api_key: '',
      is_primary: provider.is_primary,
      enabled: provider.enabled
    })
    setRelayDialog(true)
  }

  function closeRelayDialog() {
    setRelayDialog(false)
    setEditingRelayId(null)
    setRelayForm(emptyRelayForm)
  }

  function openAddScmDialog() {
    const defaultCredential = (credentials.data ?? []).find((credential) => credential.kind !== 'ssh_username_with_private_key')
    setEditingScmId(null)
    setScmForm({ ...emptyScmForm, api_credential_id: defaultCredential ? String(defaultCredential.id) : '' })
    setScmDialog(true)
  }

  function openEditScmDialog(provider: SCMProvider) {
    const defaultCredential = (credentials.data ?? []).find((credential) => credential.kind !== 'ssh_username_with_private_key')
    setEditingScmId(provider.id)
    setScmForm({
      name: provider.name,
      type: provider.type,
      base_url: provider.base_url,
      api_credential_id: provider.api_credential_id ? String(provider.api_credential_id) : defaultCredential ? String(defaultCredential.id) : '',
      clone_protocol: provider.clone_protocol ?? 'https',
      clone_credential_id: provider.clone_credential_id ? String(provider.clone_credential_id) : '',
      ssh_host: provider.ssh_host ?? ''
    })
    setScmDialog(true)
  }

  function closeScmDialog() {
    setScmDialog(false)
    setEditingScmId(null)
    setScmForm(emptyScmForm)
  }

  function openAddCredentialDialog() {
    setEditingCredentialId(null)
    setCredentialForm(emptyCredentialForm)
    setCredentialDialog(true)
  }

  function openEditCredentialDialog(credential: Credential) {
    setEditingCredentialId(credential.id)
    setCredentialForm({
      ...emptyCredentialForm,
      name: credential.name,
      description: credential.description,
      kind: credential.kind,
      username: typeof credential.summary?.username === 'string' ? credential.summary.username : ''
    })
    setCredentialDialog(true)
  }

  function closeCredentialDialog() {
    setCredentialDialog(false)
    setEditingCredentialId(null)
    setCredentialForm(emptyCredentialForm)
  }

  return (
    <Page>
      <PageHeader title='Admin Console' description='Task-zone settings backed by current Go APIs. Mutating deployment actions require explicit confirmation.' />
      <div className='flex flex-wrap gap-2'>
        {settingsSections.map((section) => (
          <Button
            key={section}
            variant={activeSection === section ? 'default' : 'outline'}
            size='sm'
            onClick={() => selectSection(section)}
          >
            {settingsSectionLabel(section)}
          </Button>
        ))}
      </div>
      <div className='grid gap-4 lg:grid-cols-2'>
        {activeSection === 'ai-services' ? <Card>
          <CardHeader>
            <div className='flex items-center justify-between gap-2'>
              <CardTitle>AI Services</CardTitle>
              <Button size='sm' onClick={openAddRelayDialog}>Add</Button>
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
                  <Button size='sm' variant='outline' onClick={() => openEditRelayDialog(provider)}>Edit</Button>
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
        </Card> : null}
        {activeSection === 'code-platforms' ? <Card>
          <CardHeader>
            <div className='flex items-center justify-between gap-2'>
              <CardTitle>Code Platforms</CardTitle>
              <Button size='sm' onClick={openAddScmDialog}>Add</Button>
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
                        <Button size='sm' variant='outline' onClick={() => openEditScmDialog(provider)}>Edit</Button>
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
        </Card> : null}
        {activeSection === 'advanced-credentials' ? <Card>
          <CardHeader>
            <div className='flex items-center justify-between gap-2'>
              <CardTitle>Advanced Credentials</CardTitle>
              <Button size='sm' onClick={openAddCredentialDialog}>Add</Button>
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
                  <Button size='sm' variant='outline' onClick={() => openEditCredentialDialog(credential)}>Edit</Button>
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
        </Card> : null}
        {activeSection === 'organization-login' ? <Card>
          <CardHeader>
            <CardTitle>Organization Login</CardTitle>
            <CardDescription>LDAP configuration and login source behavior.</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            <Input placeholder='LDAP URL' value={ldapForm.url} onChange={(event) => setLDAPForm((value) => ({ ...value, url: event.target.value }))} />
            <Input placeholder='Base DN' value={ldapForm.base_dn} onChange={(event) => setLDAPForm((value) => ({ ...value, base_dn: event.target.value }))} />
            <Input placeholder='Bind DN' value={ldapForm.bind_dn} onChange={(event) => setLDAPForm((value) => ({ ...value, bind_dn: event.target.value }))} />
            <Input type='password' placeholder='Bind password (leave blank to keep current)' value={ldapForm.bind_password} onChange={(event) => setLDAPForm((value) => ({ ...value, bind_password: event.target.value }))} />
            <Input placeholder='User filter, for example (uid=%s)' value={ldapForm.user_filter} onChange={(event) => setLDAPForm((value) => ({ ...value, user_filter: event.target.value }))} />
            <label className='flex items-center gap-2 text-sm'><input type='checkbox' checked={ldapForm.tls} onChange={(event) => setLDAPForm((value) => ({ ...value, tls: event.target.checked }))} /> Use StartTLS</label>
            {ldapMessage ? <div className={ldapMessage.toLowerCase().includes('failed') || ldapMessage.toLowerCase().includes('required') ? 'text-[var(--ae-warn)] text-sm' : 'text-[var(--ae-pos)] text-sm'}>{ldapMessage}</div> : null}
            <div className='flex flex-wrap gap-2'>
              <Button
                variant='outline'
                onClick={() => testLDAP.mutate()}
                disabled={!ldapForm.url || !ldapForm.base_dn || !ldapForm.bind_dn || !ldapForm.user_filter || testLDAP.isPending}
              >
                Test LDAP
              </Button>
              <Button
                onClick={() => saveLDAP.mutate()}
                disabled={!ldapForm.url || !ldapForm.base_dn || !ldapForm.bind_dn || !ldapForm.user_filter || saveLDAP.isPending}
              >
                Save LDAP
              </Button>
            </div>
          </CardContent>
        </Card> : null}
        {activeSection === 'deployment-runtime' ? <Card>
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
        </Card> : null}
      </div>
      <Dialog open={relayDialog} onOpenChange={(open) => open ? setRelayDialog(true) : closeRelayDialog()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingRelayId ? 'Edit relay provider' : 'Add relay provider'}</DialogTitle>
            <DialogDescription>{editingRelayId ? 'Leave admin API key empty to keep the current backend secret.' : 'Creates a backend relay provider; admin API key is sent only to the Go API.'}</DialogDescription>
          </DialogHeader>
          <div className='flex flex-col gap-3'>
            <Input placeholder='Name' value={relayForm.name} disabled={!!editingRelayId} onChange={(event) => setRelayForm((value) => ({ ...value, name: event.target.value }))} />
            <Input placeholder='Display name' value={relayForm.display_name} onChange={(event) => setRelayForm((value) => ({ ...value, display_name: event.target.value }))} />
            <Input placeholder='Base URL' value={relayForm.base_url} onChange={(event) => setRelayForm((value) => ({ ...value, base_url: event.target.value }))} />
            <Input type='password' placeholder='Admin API key' value={relayForm.admin_api_key} onChange={(event) => setRelayForm((value) => ({ ...value, admin_api_key: event.target.value }))} />
            <label className='flex items-center gap-2 text-sm'><input type='checkbox' checked={relayForm.is_primary} onChange={(event) => setRelayForm((value) => ({ ...value, is_primary: event.target.checked }))} /> Primary</label>
            <label className='flex items-center gap-2 text-sm'><input type='checkbox' checked={relayForm.enabled} onChange={(event) => setRelayForm((value) => ({ ...value, enabled: event.target.checked }))} /> Enabled</label>
            {createRelay.error ? <div className='text-[var(--ae-warn)] text-sm'>{createRelay.error.message}</div> : null}
            {updateRelay.error ? <div className='text-[var(--ae-warn)] text-sm'>{updateRelay.error.message}</div> : null}
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={closeRelayDialog}>Cancel</Button>
              <Button
                disabled={!relayForm.name || !relayForm.display_name || !relayForm.base_url || (!editingRelayId && !relayForm.admin_api_key) || createRelay.isPending || updateRelay.isPending}
                onClick={() => editingRelayId ? updateRelay.mutate() : createRelay.mutate()}
              >
                {editingRelayId ? 'Update' : 'Create'}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
      <Dialog open={scmDialog} onOpenChange={(open) => open ? setScmDialog(true) : closeScmDialog()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingScmId ? 'Edit SCM provider' : 'Add SCM provider'}</DialogTitle>
            <DialogDescription>Configures a code platform provider using existing admin credentials.</DialogDescription>
          </DialogHeader>
          <div className='flex flex-col gap-3'>
            <Input placeholder='Name' value={scmForm.name} onChange={(event) => setScmForm((value) => ({ ...value, name: event.target.value }))} />
            <select className='h-8 rounded-md border border-input bg-card px-3 text-sm' value={scmForm.type} disabled={!!editingScmId} onChange={(event) => setScmForm((value) => ({ ...value, type: event.target.value }))}>
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
            {updateScm.error ? <div className='text-[var(--ae-warn)] text-sm'>{updateScm.error.message}</div> : null}
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={closeScmDialog}>Cancel</Button>
              <Button
                disabled={!scmForm.name || !scmForm.base_url || !scmForm.api_credential_id || createScm.isPending || updateScm.isPending}
                onClick={() => editingScmId ? updateScm.mutate() : createScm.mutate()}
              >
                {editingScmId ? 'Update' : 'Create'}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
      <Dialog open={credentialDialog} onOpenChange={(open) => open ? setCredentialDialog(true) : closeCredentialDialog()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingCredentialId ? 'Edit credential' : 'Add credential'}</DialogTitle>
            <DialogDescription>{editingCredentialId ? 'Leave secret fields empty to keep existing secret values.' : 'Creates a reusable admin credential for relay or SCM configuration.'}</DialogDescription>
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
            {credentialForm.kind === 'ssh_username_with_private_key' ? <Input type='password' placeholder='Passphrase' value={credentialForm.passphrase} onChange={(event) => setCredentialForm((value) => ({ ...value, passphrase: event.target.value }))} /> : null}
            {createCredential.error ? <div className='text-[var(--ae-warn)] text-sm'>{createCredential.error.message}</div> : null}
            {updateCredential.error ? <div className='text-[var(--ae-warn)] text-sm'>{updateCredential.error.message}</div> : null}
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={closeCredentialDialog}>Cancel</Button>
              <Button
                disabled={
                  !credentialForm.name ||
                  (!editingCredentialId && credentialForm.kind === 'secret_text' && !credentialForm.text) ||
                  (!editingCredentialId && credentialForm.kind === 'username_password' && (!credentialForm.username || !credentialForm.password)) ||
                  (!editingCredentialId && credentialForm.kind === 'ssh_username_with_private_key' && (!credentialForm.username || !credentialForm.private_key)) ||
                  createCredential.isPending ||
                  updateCredential.isPending
                }
                onClick={() => editingCredentialId ? updateCredential.mutate() : createCredential.mutate()}
              >
                {editingCredentialId ? 'Update' : 'Create'}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </Page>
  )
}

function settingsSectionLabel(section: SettingsSection) {
  switch (section) {
    case 'ai-services':
      return 'AI Services'
    case 'code-platforms':
      return 'Code Platforms'
    case 'organization-login':
      return 'Organization Login'
    case 'deployment-runtime':
      return 'Deployment & Runtime'
    case 'advanced-credentials':
      return 'Advanced Credentials'
  }
}

export const settingsRouteGuard = async () => {
  const user = await import('@/query-client').then(({ queryClient }) =>
    queryClient.fetchQuery({ queryKey: ['auth', 'me'], queryFn: ensureAuthenticatedUser })
  )
  if (user.role !== 'admin') throw redirect({ to: '/' })
}
