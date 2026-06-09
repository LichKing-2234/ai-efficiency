import { createFileRoute, redirect } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useNavigate, useSearch } from '@tanstack/react-router'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Checkbox } from '@/components/ui/checkbox'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { AppAlert } from '@/components/primitives/app-alert'
import { ConfirmAction } from '@/components/primitives/confirm-action'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { ensureAuthenticatedUser } from '@/lib/auth/session'
import { dateTime, number } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
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
  const { t } = useI18n()
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
    onSuccess: () => toast.success(t('settings.restartRequested'))
  })
  const applyUpdate = useMutation({
    mutationFn: () => api.settings.applyUpdate({ target_version: deployment.data?.latest_release?.version || '' }),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['settings', 'deployment'] })
      toast.success(t('settings.updateStaged'))
    }
  })
  const rollback = useMutation({
    mutationFn: api.settings.rollback,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['settings', 'deployment'] })
      toast.success(t('settings.rollbackStaged'))
    }
  })
  const createRelay = useMutation({
    mutationFn: () => api.settings.createRelayProvider(buildRelayPayload(relayForm, 'create')),
    onSuccess: () => {
      closeRelayDialog()
      void qc.invalidateQueries({ queryKey: ['settings', 'relay'] })
      toast.success(t('settings.relayProviderCreated'))
    }
  })
  const updateRelay = useMutation({
    mutationFn: () => api.settings.updateRelayProvider(editingRelayId ?? 0, buildRelayPayload(relayForm, 'edit')),
    onSuccess: () => {
      closeRelayDialog()
      void qc.invalidateQueries({ queryKey: ['settings', 'relay'] })
      toast.success(t('settings.relayProviderUpdated'))
    }
  })
  const deleteRelay = useMutation({
    mutationFn: api.settings.deleteRelayProvider,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['settings', 'relay'] })
      toast.success(t('settings.relayProviderDeleted'))
    }
  })
  const createScm = useMutation({
    mutationFn: () => api.settings.createSCMProvider(buildScmProviderPayload(scmForm, 'create')),
    onSuccess: () => {
      closeScmDialog()
      void qc.invalidateQueries({ queryKey: ['settings', 'scm'] })
      toast.success(t('settings.scmProviderCreated'))
    }
  })
  const updateScm = useMutation({
    mutationFn: () => api.settings.updateSCMProvider(editingScmId ?? 0, buildScmProviderPayload(scmForm, 'edit')),
    onSuccess: () => {
      closeScmDialog()
      void qc.invalidateQueries({ queryKey: ['settings', 'scm'] })
      toast.success(t('settings.scmProviderUpdated'))
    }
  })
  const deleteScm = useMutation({
    mutationFn: api.settings.deleteSCMProvider,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['settings', 'scm'] })
      toast.success(t('settings.scmProviderDeleted'))
    }
  })
  const createCredential = useMutation({
    mutationFn: () => api.settings.createCredential(buildCredentialPayload(credentialForm, 'create')),
    onSuccess: () => {
      closeCredentialDialog()
      void qc.invalidateQueries({ queryKey: ['settings', 'credentials'] })
      toast.success(t('settings.credentialCreated'))
    }
  })
  const updateCredential = useMutation({
    mutationFn: () => api.settings.updateCredential(editingCredentialId ?? 0, buildCredentialPayload(credentialForm, 'edit')),
    onSuccess: () => {
      closeCredentialDialog()
      void qc.invalidateQueries({ queryKey: ['settings', 'credentials'] })
      toast.success(t('settings.credentialUpdated'))
    }
  })
  const deleteCredential = useMutation({
    mutationFn: api.settings.deleteCredential,
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['settings', 'credentials'] })
      toast.success(t('settings.credentialDeleted'))
    }
  })
  const saveLDAP = useMutation({
    mutationFn: () => api.settings.updateLDAP(buildLDAPPayload(ldapForm)),
    onSuccess: () => {
      setLDAPMessage(t('settings.ldapConfigurationSaved'))
      void qc.invalidateQueries({ queryKey: ['settings', 'ldap'] })
      toast.success(t('settings.ldapConfigurationSaved'))
    },
    onError: (error) => {
      setLDAPMessage(error instanceof Error ? error.message : t('settings.ldapSaveFailed'))
    }
  })
  const testLDAP = useMutation({
    mutationFn: () => api.settings.testLDAP(buildLDAPPayload(ldapForm)),
    onSuccess: () => {
      setLDAPMessage(t('settings.ldapConnectionSuccessful'))
      toast.success(t('settings.ldapConnectionSuccessful'))
    },
    onError: (error) => {
      setLDAPMessage(error instanceof Error ? error.message : t('settings.ldapConnectionTestFailed'))
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
      <PageHeader title={t('settings.title')} description={t('settings.description')} />
      <div className='flex flex-wrap gap-2'>
        {settingsSections.map((section) => (
          <Button
            key={section}
            variant={activeSection === section ? 'default' : 'outline'}
            size='sm'
            onClick={() => selectSection(section)}
          >
            {settingsSectionLabel(section, t)}
          </Button>
        ))}
      </div>
      <div className='grid gap-4 lg:grid-cols-2'>
        {activeSection === 'ai-services' ? <Card>
          <CardHeader>
            <div className='flex items-center justify-between gap-2'>
              <CardTitle>{t('settings.aiServices')}</CardTitle>
              <Button size='sm' onClick={openAddRelayDialog}>{t('common.add')}</Button>
            </div>
            <CardDescription>{t('settings.relayProvidersDescription')}</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            {(relay.data ?? []).map((provider) => (
              <div key={provider.id} className='flex items-center justify-between gap-3 rounded-md bg-muted p-3'>
                <div>
                  <div className='font-medium'>{provider.display_name || provider.name}</div>
                  <div className='text-muted-foreground text-xs'>{provider.base_url}</div>
                </div>
                <div className='flex items-center gap-2'>
                  {provider.is_primary ? <Badge variant='ai'>{t('common.primary')}</Badge> : null}
                  <StatusBadge value={provider.enabled ? 'active' : 'disabled'} />
                  <Button size='sm' variant='outline' onClick={() => openEditRelayDialog(provider)}>{t('common.update')}</Button>
                  <ConfirmAction
                    trigger={<Button size='sm' variant='ghost' disabled={deleteRelay.isPending}>{t('common.delete')}</Button>}
                    title={t('settings.deleteRelayProvider')}
                    description={t('settings.deleteRelayProviderDescription', { name: provider.display_name || provider.name })}
                    confirmLabel={t('common.delete')}
                    cancelLabel={t('common.cancel')}
                    onConfirm={() => deleteRelay.mutate(provider.id)}
                    disabled={deleteRelay.isPending}
                  />
                </div>
              </div>
            ))}
          </CardContent>
        </Card> : null}
        {activeSection === 'code-platforms' ? <Card>
          <CardHeader>
            <div className='flex items-center justify-between gap-2'>
              <CardTitle>{t('settings.codePlatforms')}</CardTitle>
              <Button size='sm' onClick={openAddScmDialog}>{t('common.add')}</Button>
            </div>
            <CardDescription>{t('settings.scmProvidersDescription')}</CardDescription>
          </CardHeader>
          <CardContent>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('settings.name')}</TableHead>
                  <TableHead>{t('common.type')}</TableHead>
                  <TableHead>{t('common.status')}</TableHead>
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
                        <Button size='sm' variant='outline' onClick={() => openEditScmDialog(provider)}>{t('common.update')}</Button>
                        <ConfirmAction
                          trigger={<Button size='sm' variant='ghost' disabled={deleteScm.isPending}>{t('common.delete')}</Button>}
                          title={t('settings.deleteScmProvider')}
                          description={t('settings.deleteScmProviderDescription', { name: provider.name })}
                          confirmLabel={t('common.delete')}
                          cancelLabel={t('common.cancel')}
                          onConfirm={() => deleteScm.mutate(provider.id)}
                          disabled={deleteScm.isPending}
                        />
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
              <CardTitle>{t('settings.advancedCredentials')}</CardTitle>
              <Button size='sm' onClick={openAddCredentialDialog}>{t('common.add')}</Button>
            </div>
            <CardDescription>{t('settings.advancedCredentialsDescription')}</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-2'>
            {(credentials.data ?? []).map((credential) => (
              <div key={credential.id} className='flex items-center justify-between rounded-md bg-muted p-3'>
                <div>
                  <div className='font-medium'>{credential.name}</div>
                  <div className='text-muted-foreground text-xs'>{t('settings.usedTimes', { kind: credential.kind, count: number(credential.usage_count) })}</div>
                </div>
                <div className='flex items-center gap-2'>
                  <span className='text-muted-foreground text-xs'>{dateTime(credential.updated_at)}</span>
                  <Button size='sm' variant='outline' onClick={() => openEditCredentialDialog(credential)}>{t('common.update')}</Button>
                  <ConfirmAction
                    trigger={<Button size='sm' variant='ghost' disabled={deleteCredential.isPending}>{t('common.delete')}</Button>}
                    title={t('settings.deleteCredential')}
                    description={t('settings.deleteCredentialDescription', { name: credential.name })}
                    confirmLabel={t('common.delete')}
                    cancelLabel={t('common.cancel')}
                    onConfirm={() => deleteCredential.mutate(credential.id)}
                    disabled={deleteCredential.isPending}
                  />
                </div>
              </div>
            ))}
          </CardContent>
        </Card> : null}
        {activeSection === 'organization-login' ? <Card>
          <CardHeader>
            <CardTitle>{t('settings.organizationLogin')}</CardTitle>
            <CardDescription>{t('settings.ldapLoginBehavior')}</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            <Input placeholder={t('settings.ldapUrl')} value={ldapForm.url} onChange={(event) => setLDAPForm((value) => ({ ...value, url: event.target.value }))} />
            <Input placeholder={t('settings.baseDn')} value={ldapForm.base_dn} onChange={(event) => setLDAPForm((value) => ({ ...value, base_dn: event.target.value }))} />
            <Input placeholder={t('settings.bindDn')} value={ldapForm.bind_dn} onChange={(event) => setLDAPForm((value) => ({ ...value, bind_dn: event.target.value }))} />
            <Input type='password' placeholder={t('settings.bindPassword')} value={ldapForm.bind_password} onChange={(event) => setLDAPForm((value) => ({ ...value, bind_password: event.target.value }))} />
            <Input placeholder={t('settings.userFilter')} value={ldapForm.user_filter} onChange={(event) => setLDAPForm((value) => ({ ...value, user_filter: event.target.value }))} />
            <Field orientation='horizontal'>
              <Checkbox id='ldap-starttls' checked={ldapForm.tls} onCheckedChange={(checked) => setLDAPForm((value) => ({ ...value, tls: checked === true }))} />
              <FieldLabel htmlFor='ldap-starttls'>{t('settings.useStartTls')}</FieldLabel>
            </Field>
            {ldapMessage ? (
              <AppAlert
                tone={ldapMessage.toLowerCase().includes('failed') || ldapMessage.toLowerCase().includes('required') ? 'error' : 'success'}
                title={ldapMessage}
              />
            ) : null}
            <div className='flex flex-wrap gap-2'>
              <Button
                variant='outline'
                onClick={() => testLDAP.mutate()}
                disabled={!ldapForm.url || !ldapForm.base_dn || !ldapForm.bind_dn || !ldapForm.user_filter || testLDAP.isPending}
              >
                {t('settings.testLdap')}
              </Button>
              <Button
                onClick={() => saveLDAP.mutate()}
                disabled={!ldapForm.url || !ldapForm.base_dn || !ldapForm.bind_dn || !ldapForm.user_filter || saveLDAP.isPending}
              >
                {t('settings.saveLdap')}
              </Button>
            </div>
          </CardContent>
        </Card> : null}
        {activeSection === 'deployment-runtime' ? <Card>
          <CardHeader>
            <CardTitle>{t('settings.deploymentRuntime')}</CardTitle>
            <CardDescription>{t('settings.currentBackendDeployment')}</CardDescription>
          </CardHeader>
          <CardContent className='flex flex-col gap-3'>
            <div className='rounded-md bg-muted p-3'>
              <div className='font-medium'>v{deployment.data?.version.version || '-'}</div>
              <div className='text-muted-foreground text-xs'>{deployment.data?.mode || t('common.unknown')} · {deployment.data?.version.commit || '-'}</div>
            </div>
            {deployment.data?.update_available ? <Badge variant='ai'>{t('settings.updateAvailable', { version: deployment.data.latest_release?.version || '-' })}</Badge> : <Badge variant='success'>{t('settings.upToDate')}</Badge>}
            <div className='flex gap-2'>
              <Button variant='outline' onClick={() => checkUpdate.mutate()} disabled={checkUpdate.isPending}>{t('settings.checkUpdate')}</Button>
              <ConfirmAction
                trigger={<Button variant='outline' disabled={!deployment.data?.latest_release?.version || applyUpdate.isPending}>{t('common.apply')}</Button>}
                title={t('settings.stageUpdate')}
                description={t('settings.stageUpdateDescription', { version: deployment.data?.latest_release?.version || '' })}
                confirmLabel={t('common.apply')}
                cancelLabel={t('common.cancel')}
                onConfirm={() => applyUpdate.mutate()}
                disabled={!deployment.data?.latest_release?.version || applyUpdate.isPending}
              />
              <ConfirmAction
                trigger={<Button variant='outline' disabled={rollback.isPending}>{t('settings.rollback')}</Button>}
                title={t('settings.rollback')}
                description={t('settings.rollbackDescription')}
                confirmLabel={t('settings.rollback')}
                cancelLabel={t('common.cancel')}
                onConfirm={() => rollback.mutate()}
                disabled={rollback.isPending}
              />
              <ConfirmAction
                trigger={<Button variant='outline' disabled={restart.isPending}>{t('settings.restart')}</Button>}
                title={t('settings.requestRestart')}
                description={t('settings.requestRestartDescription')}
                confirmLabel={t('settings.restart')}
                cancelLabel={t('common.cancel')}
                onConfirm={() => restart.mutate()}
                disabled={restart.isPending}
              />
            </div>
          </CardContent>
        </Card> : null}
      </div>
      <Dialog open={relayDialog} onOpenChange={(open) => open ? setRelayDialog(true) : closeRelayDialog()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingRelayId ? t('settings.editRelayProvider') : t('settings.addRelayProvider')}</DialogTitle>
            <DialogDescription>{editingRelayId ? t('settings.editRelayProviderDescription') : t('settings.adminApiKeyDescription')}</DialogDescription>
          </DialogHeader>
          <div className='flex flex-col gap-3'>
            <Input placeholder={t('settings.name')} value={relayForm.name} disabled={!!editingRelayId} onChange={(event) => setRelayForm((value) => ({ ...value, name: event.target.value }))} />
            <Input placeholder={t('settings.displayName')} value={relayForm.display_name} onChange={(event) => setRelayForm((value) => ({ ...value, display_name: event.target.value }))} />
            <Input placeholder={t('settings.baseUrl')} value={relayForm.base_url} onChange={(event) => setRelayForm((value) => ({ ...value, base_url: event.target.value }))} />
            <Input type='password' placeholder={t('settings.adminApiKey')} value={relayForm.admin_api_key} onChange={(event) => setRelayForm((value) => ({ ...value, admin_api_key: event.target.value }))} />
            <Field orientation='horizontal'>
              <Checkbox id='relay-primary' checked={relayForm.is_primary} onCheckedChange={(checked) => setRelayForm((value) => ({ ...value, is_primary: checked === true }))} />
              <FieldLabel htmlFor='relay-primary'>{t('settings.primary')}</FieldLabel>
            </Field>
            <Field orientation='horizontal'>
              <Checkbox id='relay-enabled' checked={relayForm.enabled} onCheckedChange={(checked) => setRelayForm((value) => ({ ...value, enabled: checked === true }))} />
              <FieldLabel htmlFor='relay-enabled'>{t('settings.enabled')}</FieldLabel>
            </Field>
            {createRelay.error ? <AppAlert tone='error' title={createRelay.error.message} /> : null}
            {updateRelay.error ? <AppAlert tone='error' title={updateRelay.error.message} /> : null}
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={closeRelayDialog}>{t('common.cancel')}</Button>
              <Button
                disabled={!relayForm.name || !relayForm.display_name || !relayForm.base_url || (!editingRelayId && !relayForm.admin_api_key) || createRelay.isPending || updateRelay.isPending}
                onClick={() => editingRelayId ? updateRelay.mutate() : createRelay.mutate()}
              >
                {editingRelayId ? t('common.update') : t('common.create')}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
      <Dialog open={scmDialog} onOpenChange={(open) => open ? setScmDialog(true) : closeScmDialog()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingScmId ? t('settings.editScmProvider') : t('settings.addScmProvider')}</DialogTitle>
            <DialogDescription>{t('settings.scmProvidersDescription')}</DialogDescription>
          </DialogHeader>
          <div className='flex flex-col gap-3'>
            <Input placeholder={t('settings.name')} value={scmForm.name} onChange={(event) => setScmForm((value) => ({ ...value, name: event.target.value }))} />
            <Select value={scmForm.type} disabled={!!editingScmId} onValueChange={(value) => setScmForm((current) => ({ ...current, type: value }))}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='github'>GitHub</SelectItem>
                <SelectItem value='bitbucket'>Bitbucket</SelectItem>
              </SelectContent>
            </Select>
            <Input placeholder={t('settings.baseUrl')} value={scmForm.base_url} onChange={(event) => setScmForm((value) => ({ ...value, base_url: event.target.value }))} />
            <Select value={scmForm.api_credential_id || 'none'} onValueChange={(value) => setScmForm((current) => ({ ...current, api_credential_id: value === 'none' ? '' : value }))}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='none'>{t('settings.apiCredential')}</SelectItem>
                {(credentials.data ?? []).map((credential) => <SelectItem key={credential.id} value={String(credential.id)}>{credential.name}</SelectItem>)}
              </SelectContent>
            </Select>
            <Select value={scmForm.clone_protocol} onValueChange={(value) => setScmForm((current) => ({ ...current, clone_protocol: value as 'https' | 'ssh' }))}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='https'>{t('settings.cloneHttps')}</SelectItem>
                <SelectItem value='ssh'>{t('settings.cloneSsh')}</SelectItem>
              </SelectContent>
            </Select>
            {scmForm.clone_protocol === 'ssh' ? (
              <>
                <Input placeholder={t('settings.sshHost')} value={scmForm.ssh_host} onChange={(event) => setScmForm((value) => ({ ...value, ssh_host: event.target.value }))} />
                <Select value={scmForm.clone_credential_id || 'none'} onValueChange={(value) => setScmForm((current) => ({ ...current, clone_credential_id: value === 'none' ? '' : value }))}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    <SelectItem value='none'>{t('settings.cloneCredential')}</SelectItem>
                    {(credentials.data ?? []).map((credential) => <SelectItem key={credential.id} value={String(credential.id)}>{credential.name}</SelectItem>)}
                  </SelectContent>
                </Select>
              </>
            ) : null}
            {createScm.error ? <AppAlert tone='error' title={createScm.error.message} /> : null}
            {updateScm.error ? <AppAlert tone='error' title={updateScm.error.message} /> : null}
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={closeScmDialog}>{t('common.cancel')}</Button>
              <Button
                disabled={!scmForm.name || !scmForm.base_url || !scmForm.api_credential_id || createScm.isPending || updateScm.isPending}
                onClick={() => editingScmId ? updateScm.mutate() : createScm.mutate()}
              >
                {editingScmId ? t('common.update') : t('common.create')}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
      <Dialog open={credentialDialog} onOpenChange={(open) => open ? setCredentialDialog(true) : closeCredentialDialog()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingCredentialId ? t('settings.editCredential') : t('settings.addCredential')}</DialogTitle>
            <DialogDescription>{editingCredentialId ? t('settings.editCredentialDescription') : t('settings.createCredentialDescription')}</DialogDescription>
          </DialogHeader>
          <div className='flex flex-col gap-3'>
            <Input placeholder={t('settings.name')} value={credentialForm.name} onChange={(event) => setCredentialForm((value) => ({ ...value, name: event.target.value }))} />
            <Input placeholder={t('settings.credentialDescription')} value={credentialForm.description} onChange={(event) => setCredentialForm((value) => ({ ...value, description: event.target.value }))} />
            <Select value={credentialForm.kind} onValueChange={(value) => setCredentialForm((current) => ({ ...current, kind: value as typeof credentialForm.kind }))}>
              <SelectTrigger><SelectValue /></SelectTrigger>
              <SelectContent>
                <SelectItem value='secret_text'>{t('settings.secretTextKind')}</SelectItem>
                <SelectItem value='username_password'>{t('settings.usernamePasswordKind')}</SelectItem>
                <SelectItem value='ssh_username_with_private_key'>{t('settings.sshPrivateKeyKind')}</SelectItem>
              </SelectContent>
            </Select>
            {credentialForm.kind === 'secret_text' ? <Textarea placeholder={t('settings.secretText')} value={credentialForm.text} onChange={(event) => setCredentialForm((value) => ({ ...value, text: event.target.value }))} /> : null}
            {credentialForm.kind !== 'secret_text' ? <Input placeholder={t('settings.username')} value={credentialForm.username} onChange={(event) => setCredentialForm((value) => ({ ...value, username: event.target.value }))} /> : null}
            {credentialForm.kind === 'username_password' ? <Input type='password' placeholder={t('settings.password')} value={credentialForm.password} onChange={(event) => setCredentialForm((value) => ({ ...value, password: event.target.value }))} /> : null}
            {credentialForm.kind === 'ssh_username_with_private_key' ? <Textarea placeholder={t('settings.privateKey')} value={credentialForm.private_key} onChange={(event) => setCredentialForm((value) => ({ ...value, private_key: event.target.value }))} /> : null}
            {credentialForm.kind === 'ssh_username_with_private_key' ? <Input type='password' placeholder={t('settings.passphrase')} value={credentialForm.passphrase} onChange={(event) => setCredentialForm((value) => ({ ...value, passphrase: event.target.value }))} /> : null}
            {createCredential.error ? <AppAlert tone='error' title={createCredential.error.message} /> : null}
            {updateCredential.error ? <AppAlert tone='error' title={updateCredential.error.message} /> : null}
            <div className='flex justify-end gap-2'>
              <Button variant='outline' onClick={closeCredentialDialog}>{t('common.cancel')}</Button>
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
                {editingCredentialId ? t('common.update') : t('common.create')}
              </Button>
            </div>
          </div>
        </DialogContent>
      </Dialog>
    </Page>
  )
}

function settingsSectionLabel(section: SettingsSection, t: ReturnType<typeof useI18n>['t']) {
  switch (section) {
    case 'ai-services':
      return t('settings.aiServices')
    case 'code-platforms':
      return t('settings.codePlatforms')
    case 'organization-login':
      return t('settings.organizationLogin')
    case 'deployment-runtime':
      return t('settings.deploymentRuntime')
    case 'advanced-credentials':
      return t('settings.advancedCredentials')
  }
}

export const settingsRouteGuard = async () => {
  const user = await import('@/query-client').then(({ queryClient }) =>
    queryClient.fetchQuery({ queryKey: ['auth', 'me'], queryFn: ensureAuthenticatedUser })
  )
  if (user.role !== 'admin') throw redirect({ to: '/' })
}
