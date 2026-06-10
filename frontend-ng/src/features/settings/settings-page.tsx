import { createFileRoute, redirect } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate, useNavigate, useSearch } from '@tanstack/react-router'
import { Database, KeyRound, Layers, LockKeyhole, RefreshCw, Shield, Waypoints } from 'lucide-react'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Badge } from '@/components/ui/badge'
import { ActionGroup } from '@/components/primitives/action-group'
import { AppAlert } from '@/components/primitives/app-alert'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { CardTableContent } from '@/components/primitives/card-table-content'
import { ConfirmAction } from '@/components/primitives/confirm-action'
import { DataGrid, DataGridCell, DataGridHeader, DataGridRow } from '@/components/primitives/data-grid'
import { Page } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { FieldItem, FieldList } from '@/components/primitives/field-list'
import { InfoTile, InfoTileGrid } from '@/components/primitives/info-tile'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { SectionNav, type SectionNavItem } from '@/components/primitives/section-nav'
import { Stack } from '@/components/primitives/stack'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { dateTime, number } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import { CredentialForm } from './credential-form'
import { LdapSettingsForm } from './ldap-settings-form'
import { RelayProviderForm } from './relay-provider-form'
import { ScmProviderForm } from './scm-provider-form'
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
const relayColumns = '1.2fr_1.8fr_0.7fr_0.8fr_190px'
const scmColumns = '1.4fr_0.8fr_1.8fr_0.8fr_180px'
const credentialColumns = '1.4fr_1fr_1fr_0.9fr_180px'

export function SettingsPage() {
  const { t } = useI18n()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as Record<string, unknown>
  const activeSection = settingsSectionFromSearch(search)
  const sectionItems = settingsSections.map((section) => ({
    value: section,
    label: settingsSectionLabel(section, t),
    icon: settingsSectionIcon(section)
  })) satisfies Array<SectionNavItem<SettingsSection>>
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
  const me = useQuery({ queryKey: ['auth', 'me'], queryFn: api.auth.me })
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

  if (me.data && me.data.role !== 'admin') return <Navigate to='/' />
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
    <Page className='stagger'>
      <div className='split-settings'>
        <Card className='p-2'>
          <SectionNav ariaLabel={t('settings.sections')} items={sectionItems} onChange={selectSection} value={activeSection} />
        </Card>
        <Stack className='min-w-0'>
        {activeSection === 'ai-services' ? <Card>
          <SectionCardHeader
            title={t('settings.aiServices')}
            description={t('settings.relayProvidersDescription')}
            actions={<Button size='sm' onClick={openAddRelayDialog}><Layers data-icon='inline-start' />{t('common.add')}</Button>}
          />
          <CardTableContent variant='flush'>
            <DataGrid minWidth={860}>
              <DataGridHeader columns={relayColumns}>
                <span>{t('settings.name')}</span>
                <span>{t('settings.baseUrl')}</span>
                <span>{t('settings.primary')}</span>
                <span>{t('common.status')}</span>
                <span />
              </DataGridHeader>
            {(relay.data ?? []).map((provider) => (
              <DataGridRow key={provider.id} columns={relayColumns}>
                <DataGridCell description={provider.name} truncate>{provider.display_name || provider.name}</DataGridCell>
                <DataGridCell mono truncate tone='metadata'>{provider.base_url}</DataGridCell>
                <span>{provider.is_primary ? <Badge variant='ai'>{t('common.primary')}</Badge> : <span className='text-muted-foreground'>-</span>}</span>
                <span><StatusBadge value={provider.enabled ? 'active' : 'disabled'} /></span>
                <ActionGroup>
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
                </ActionGroup>
              </DataGridRow>
            ))}
            </DataGrid>
          </CardTableContent>
        </Card> : null}
        {activeSection === 'code-platforms' ? <Card>
          <SectionCardHeader
            title={t('settings.codePlatforms')}
            description={t('settings.scmProvidersDescription')}
            actions={<Button size='sm' onClick={openAddScmDialog}><Waypoints data-icon='inline-start' />{t('common.add')}</Button>}
          />
          <CardTableContent variant='flush'>
            <DataGrid minWidth={840}>
              <DataGridHeader columns={scmColumns}>
                <span>{t('settings.name')}</span>
                <span>{t('common.type')}</span>
                <span>{t('settings.baseUrl')}</span>
                <span>{t('common.status')}</span>
                <span />
              </DataGridHeader>
            {(scm.data?.items ?? []).map((provider) => (
              <DataGridRow key={provider.id} columns={scmColumns}>
                <DataGridCell truncate>{provider.name}</DataGridCell>
                <span><Badge variant='secondary'>{provider.type}</Badge></span>
                <DataGridCell mono truncate tone='metadata'>{provider.base_url}</DataGridCell>
                <span><StatusBadge value={provider.status} /></span>
                <ActionGroup>
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
                </ActionGroup>
              </DataGridRow>
            ))}
            </DataGrid>
          </CardTableContent>
        </Card> : null}
        {activeSection === 'advanced-credentials' ? <Card>
          <SectionCardHeader
            title={t('settings.advancedCredentials')}
            description={t('settings.advancedCredentialsDescription')}
            actions={<Button size='sm' onClick={openAddCredentialDialog}><KeyRound data-icon='inline-start' />{t('common.add')}</Button>}
          />
          <CardTableContent variant='flush'>
            <DataGrid minWidth={760}>
              <DataGridHeader columns={credentialColumns}>
                <span>{t('settings.name')}</span>
                <span>{t('common.type')}</span>
                <span>{t('settings.usedBy')}</span>
                <span>{t('adminUsers.updated')}</span>
                <span />
              </DataGridHeader>
            {(credentials.data ?? []).map((credential) => (
              <DataGridRow key={credential.id} columns={credentialColumns}>
                <DataGridCell description={credential.description} truncate>{credential.name}</DataGridCell>
                <span><Badge variant='secondary'>{credential.kind}</Badge></span>
                <DataGridCell numeric tone='metadata'>{number(credential.usage_count)}</DataGridCell>
                <DataGridCell numeric tone='metadata'>{dateTime(credential.updated_at)}</DataGridCell>
                <ActionGroup>
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
                </ActionGroup>
              </DataGridRow>
            ))}
            </DataGrid>
          </CardTableContent>
        </Card> : null}
        {activeSection === 'organization-login' ? <Card>
          <SectionCardHeader title={t('settings.organizationLogin')} description={t('settings.ldapLoginBehavior')} />
          <CardContent>
            <LdapSettingsForm
              form={ldapForm}
              message={ldapMessage}
              onChange={setLDAPForm}
              onSave={() => saveLDAP.mutate()}
              onTest={() => testLDAP.mutate()}
              savePending={saveLDAP.isPending}
              testPending={testLDAP.isPending}
            />
          </CardContent>
        </Card> : null}
        {activeSection === 'deployment-runtime' ? <Card>
          <SectionCardHeader title={t('settings.deploymentRuntime')} description={t('settings.currentBackendDeployment')} />
          <CardContentStack>
            <InfoTileGrid columns={3}>
              <InfoTile label={t('settings.current')} value={`v${deployment.data?.version.version || '-'}`} mono />
              <InfoTile label={t('settings.mode')} value={deployment.data?.mode || t('common.unknown')} mono />
              <InfoTile label={t('settings.commit')} value={deployment.data?.version.commit || '-'} mono />
            </InfoTileGrid>
            <FieldList>
              <FieldItem label={t('settings.current')} value={`v${deployment.data?.version.version || '-'}`} mono />
              <FieldItem label={t('settings.mode')} value={deployment.data?.mode || t('common.unknown')} mono />
              <FieldItem label={t('settings.commit')} value={deployment.data?.version.commit || '-'} mono />
            </FieldList>
            {deployment.data?.update_available ? <Badge variant='ai'>{t('settings.updateAvailable', { version: deployment.data.latest_release?.version || '-' })}</Badge> : <Badge variant='success'>{t('settings.upToDate')}</Badge>}
            <ActionGroup wrap className='justify-start'>
              <Button variant='outline' onClick={() => checkUpdate.mutate()} disabled={checkUpdate.isPending}><RefreshCw data-icon='inline-start' />{t('settings.checkUpdate')}</Button>
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
            </ActionGroup>
          </CardContentStack>
        </Card> : null}
        </Stack>
      </div>
      <Dialog open={relayDialog} onOpenChange={(open) => open ? setRelayDialog(true) : closeRelayDialog()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingRelayId ? t('settings.editRelayProvider') : t('settings.addRelayProvider')}</DialogTitle>
            <DialogDescription>{editingRelayId ? t('settings.editRelayProviderDescription') : t('settings.adminApiKeyDescription')}</DialogDescription>
          </DialogHeader>
          <RelayProviderForm
            createPending={createRelay.isPending}
            editMode={!!editingRelayId}
            errors={[createRelay.error?.message, updateRelay.error?.message]}
            form={relayForm}
            onCancel={closeRelayDialog}
            onChange={setRelayForm}
            onSubmit={() => editingRelayId ? updateRelay.mutate() : createRelay.mutate()}
            updatePending={updateRelay.isPending}
          />
        </DialogContent>
      </Dialog>
      <Dialog open={scmDialog} onOpenChange={(open) => open ? setScmDialog(true) : closeScmDialog()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingScmId ? t('settings.editScmProvider') : t('settings.addScmProvider')}</DialogTitle>
            <DialogDescription>{t('settings.scmProvidersDescription')}</DialogDescription>
          </DialogHeader>
          <ScmProviderForm
            createPending={createScm.isPending}
            credentials={credentials.data ?? []}
            editMode={!!editingScmId}
            errors={[createScm.error?.message, updateScm.error?.message]}
            form={scmForm}
            onCancel={closeScmDialog}
            onChange={setScmForm}
            onSubmit={() => editingScmId ? updateScm.mutate() : createScm.mutate()}
            updatePending={updateScm.isPending}
          />
        </DialogContent>
      </Dialog>
      <Dialog open={credentialDialog} onOpenChange={(open) => open ? setCredentialDialog(true) : closeCredentialDialog()}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{editingCredentialId ? t('settings.editCredential') : t('settings.addCredential')}</DialogTitle>
            <DialogDescription>{editingCredentialId ? t('settings.editCredentialDescription') : t('settings.createCredentialDescription')}</DialogDescription>
          </DialogHeader>
          <CredentialForm
            createPending={createCredential.isPending}
            editMode={!!editingCredentialId}
            errors={[createCredential.error?.message, updateCredential.error?.message]}
            form={credentialForm}
            onCancel={closeCredentialDialog}
            onChange={setCredentialForm}
            onSubmit={() => editingCredentialId ? updateCredential.mutate() : createCredential.mutate()}
            updatePending={updateCredential.isPending}
          />
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

function settingsSectionIcon(section: SettingsSection) {
  switch (section) {
    case 'ai-services':
      return Layers
    case 'code-platforms':
      return Waypoints
    case 'organization-login':
      return Shield
    case 'deployment-runtime':
      return Database
    case 'advanced-credentials':
      return LockKeyhole
  }
}
