import { createFileRoute, redirect } from '@tanstack/react-router'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Navigate, useNavigate, useSearch } from '@tanstack/react-router'
import { Database, KeyRound, Layers, LockKeyhole, RefreshCw, Settings as SettingsIcon, Shield, Trash2, Waypoints } from 'lucide-react'
import { useEffect, useState } from 'react'
import { toast } from 'sonner'
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AppAlert } from '@/components/primitives/app-alert'
import { ButtonWithIcon } from '@/components/primitives/button-with-icon'
import { CategoryBadge } from '@/components/primitives/category-badge'
import { ConfirmActionButton } from '@/components/primitives/confirm-action-button'
import { DataGrid, DataGridCell, DataGridHeader, DataGridRow } from '@/components/primitives/data-grid'
import { Page } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { HealthFieldItem, HealthFieldList, type HealthStatus } from '@/components/primitives/health-field-list'
import { InfoTile, InfoTileGrid } from '@/components/primitives/info-tile'
import { PageEmpty } from '@/components/primitives/page-empty'
import { RowIconActions } from '@/components/primitives/row-icon-actions'
import { SectionCard } from '@/components/primitives/section-card'
import { SectionTableCard } from '@/components/primitives/section-table-card'
import { SectionNav, SectionNavFrame, type SectionNavItem } from '@/components/primitives/section-nav'
import { StartActions } from '@/components/primitives/start-actions'
import { Stack } from '@/components/primitives/stack'
import { SurfaceSplit } from '@/components/primitives/surface-split'
import { StatusBadge } from '@/components/primitives/status-badge'
import { api } from '@/lib/api'
import { dateTime, number } from '@/lib/format'
import { useI18n } from '@/lib/i18n/i18n'
import { CredentialForm } from './credential-form'
import { LdapSettingsForm } from './ldap-settings-form'
import { RelayProviderForm } from './relay-provider-form'
import { ScmProviderForm } from './scm-provider-form'
import type { Credential, DeploymentHealthCheck, RelayProvider, SCMProvider } from '@/lib/api/types'
import {
  buildCredentialPayload,
  buildSettingsSectionSearch,
  buildLDAPForm,
  buildLDAPPayload,
  buildRelayPayload,
  buildScmProviderPayload,
  settingsCredentialKindLabel,
  type CredentialFormState,
  type LDAPFormState,
  type RelayFormState,
  settingsScmProviderTypeLabel,
  settingsSectionMeta,
  type SettingsSection,
  settingsSectionFromSearch,
  settingsSections,
  type ScmFormState
} from './settings-payloads'

const emptyRelayForm: RelayFormState = { name: '', display_name: '', base_url: '', admin_api_key: '', is_primary: false, enabled: true }
const emptyScmForm: ScmFormState = { name: '', type: 'github', base_url: '', api_credential_id: '', clone_protocol: 'https', clone_credential_id: '', ssh_host: '' }
const emptyCredentialForm: CredentialFormState = { name: '', description: '', kind: 'secret_text', text: '', username: '', password: '', private_key: '', passphrase: '' }
const emptyLDAPForm: LDAPFormState = { url: '', base_dn: '', bind_dn: '', bind_password: '', user_filter: '(uid=%s)', tls: false }
const relayColumns = '1.2fr_1.8fr_0.7fr_0.8fr_86px'
const scmColumns = '1.4fr_0.8fr_1.8fr_0.8fr_86px'
const credentialColumns = '1.4fr_1fr_1fr_0.9fr_86px'
const settingsSectionIcons = {
  database: Database,
  layers: Layers,
  lock: LockKeyhole,
  shield: Shield,
  waypoints: Waypoints
} as const

export function SettingsPage() {
  const { t } = useI18n()
  const qc = useQueryClient()
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as Record<string, unknown>
  const activeSection = settingsSectionFromSearch(search)
  const sectionItems = settingsSections.map((section) => ({
    value: section,
    label: t(settingsSectionMeta[section].labelKey as never),
    icon: settingsSectionIcons[settingsSectionMeta[section].iconName]
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
  const deploymentHealth = useQuery({ queryKey: ['health', 'ready'], queryFn: api.health.ready })
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
      <SurfaceSplit variant='settings'>
        <SectionNavFrame>
          <SectionNav ariaLabel={t('settings.sections')} items={sectionItems} onChange={selectSection} value={activeSection} />
        </SectionNavFrame>
        <Stack constrain='content'>
        {activeSection === 'ai-services' ? <SectionTableCard
          actions={<ButtonWithIcon size='sm' icon={Layers} onClick={openAddRelayDialog}>{t('settings.addRelayProvider')}</ButtonWithIcon>}
          description={t(settingsSectionMeta['ai-services'].descriptionKey as never)}
          leading={Layers}
          title={t(settingsSectionMeta['ai-services'].labelKey as never)}
        >
            {(relay.data ?? []).length > 0 ? (
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
                    {provider.is_primary ? <span><CategoryBadge variant='ai'>{t('common.primary')}</CategoryBadge></span> : <DataGridCell tone='metadata'>-</DataGridCell>}
                    <span><StatusBadge value={provider.enabled ? 'active' : 'disabled'} /></span>
                    <RowIconActions
                      editLabel={t('common.update')}
                      deleteLabel={t('common.delete')}
                      cancelLabel={t('common.cancel')}
                      deleteTitle={t('settings.deleteRelayProvider')}
                      deleteDescription={t('settings.deleteRelayProviderDescription', { name: provider.display_name || provider.name })}
                      deleteDisabled={deleteRelay.isPending}
                      editIcon={SettingsIcon}
                      deleteIcon={Trash2}
                      onEdit={() => openEditRelayDialog(provider)}
                      onDelete={() => deleteRelay.mutate(provider.id)}
                    />
                  </DataGridRow>
                ))}
              </DataGrid>
            ) : (
              <PageEmpty
                icon={Layers}
                title={t(settingsSectionMeta['ai-services'].labelKey as never)}
                description={t(settingsSectionMeta['ai-services'].descriptionKey as never)}
                action={<ButtonWithIcon size='sm' icon={Layers} onClick={openAddRelayDialog}>{t('settings.addRelayProvider')}</ButtonWithIcon>}
              />
            )}
        </SectionTableCard> : null}
        {activeSection === 'code-platforms' ? <SectionTableCard
          actions={<ButtonWithIcon size='sm' icon={Waypoints} onClick={openAddScmDialog}>{t('settings.addScmProvider')}</ButtonWithIcon>}
          description={t(settingsSectionMeta['code-platforms'].descriptionKey as never)}
          leading={Waypoints}
          title={t(settingsSectionMeta['code-platforms'].labelKey as never)}
        >
            {(scm.data?.items ?? []).length > 0 ? (
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
                    <span><CategoryBadge>{t(settingsScmProviderTypeLabel(provider.type) as never)}</CategoryBadge></span>
                    <DataGridCell mono truncate tone='metadata'>{provider.base_url}</DataGridCell>
                    <span><StatusBadge value={provider.status} /></span>
                    <RowIconActions
                      editLabel={t('common.update')}
                      deleteLabel={t('common.delete')}
                      cancelLabel={t('common.cancel')}
                      deleteTitle={t('settings.deleteScmProvider')}
                      deleteDescription={t('settings.deleteScmProviderDescription', { name: provider.name })}
                      deleteDisabled={deleteScm.isPending}
                      editIcon={SettingsIcon}
                      deleteIcon={Trash2}
                      onEdit={() => openEditScmDialog(provider)}
                      onDelete={() => deleteScm.mutate(provider.id)}
                    />
                  </DataGridRow>
                ))}
              </DataGrid>
            ) : (
              <PageEmpty
                icon={Waypoints}
                title={t(settingsSectionMeta['code-platforms'].labelKey as never)}
                description={t(settingsSectionMeta['code-platforms'].descriptionKey as never)}
                action={<ButtonWithIcon size='sm' icon={Waypoints} onClick={openAddScmDialog}>{t('settings.addScmProvider')}</ButtonWithIcon>}
              />
            )}
        </SectionTableCard> : null}
        {activeSection === 'advanced-credentials' ? <SectionTableCard
          actions={<ButtonWithIcon size='sm' icon={KeyRound} onClick={openAddCredentialDialog}>{t('settings.addCredential')}</ButtonWithIcon>}
          description={t(settingsSectionMeta['advanced-credentials'].descriptionKey as never)}
          leading={LockKeyhole}
          title={t(settingsSectionMeta['advanced-credentials'].labelKey as never)}
        >
            {(credentials.data ?? []).length > 0 ? (
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
                    <span><CategoryBadge>{t(settingsCredentialKindLabel(credential.kind) as never)}</CategoryBadge></span>
                    <DataGridCell numeric tone='metadata'>{number(credential.usage_count)}</DataGridCell>
                    <DataGridCell numeric tone='metadata'>{dateTime(credential.updated_at)}</DataGridCell>
                    <RowIconActions
                      editLabel={t('common.update')}
                      deleteLabel={t('common.delete')}
                      cancelLabel={t('common.cancel')}
                      deleteTitle={t('settings.deleteCredential')}
                      deleteDescription={t('settings.deleteCredentialDescription', { name: credential.name })}
                      deleteDisabled={deleteCredential.isPending}
                      editIcon={SettingsIcon}
                      deleteIcon={Trash2}
                      onEdit={() => openEditCredentialDialog(credential)}
                      onDelete={() => deleteCredential.mutate(credential.id)}
                    />
                  </DataGridRow>
                ))}
              </DataGrid>
            ) : (
              <PageEmpty
                icon={LockKeyhole}
                title={t(settingsSectionMeta['advanced-credentials'].labelKey as never)}
                description={t(settingsSectionMeta['advanced-credentials'].descriptionKey as never)}
                action={<ButtonWithIcon size='sm' icon={KeyRound} onClick={openAddCredentialDialog}>{t('settings.addCredential')}</ButtonWithIcon>}
              />
            )}
        </SectionTableCard> : null}
        {activeSection === 'organization-login' ? <SectionCard
          description={t(settingsSectionMeta['organization-login'].descriptionKey as never)}
          leading={Shield}
          title={t(settingsSectionMeta['organization-login'].labelKey as never)}
        >
            <LdapSettingsForm
              form={ldapForm}
              message={ldapMessage}
              onChange={setLDAPForm}
              onSave={() => saveLDAP.mutate()}
              onTest={() => testLDAP.mutate()}
              savePending={saveLDAP.isPending}
              testPending={testLDAP.isPending}
            />
        </SectionCard> : null}
        {activeSection === 'deployment-runtime' ? <>
          <SectionCard
            actions={
              deployment.data?.update_available
                ? <CategoryBadge variant='ai'>{t('settings.updateAvailable', { version: deployment.data.latest_release?.version || '-' })}</CategoryBadge>
                : <StatusBadge value='success' label={t('settings.upToDate')} />
            }
            description={t(settingsSectionMeta['deployment-runtime'].descriptionKey as never)}
            gap='compact'
            leading={Database}
            title={t(settingsSectionMeta['deployment-runtime'].labelKey as never)}
          >
              <InfoTileGrid columns={3} className='split-equal min-[920px]:grid-cols-3'>
                <InfoTile label={t('settings.current')} value={`v${deployment.data?.version.version || '-'}`} mono />
                <InfoTile label={t('settings.latest')} value={`v${deployment.data?.latest_release?.version || deployment.data?.version.version || '-'}`} mono />
                <InfoTile label={t('settings.mode')} value={deployment.data?.mode || t('common.unknown')} mono accent='ai' />
              </InfoTileGrid>
              <StartActions>
                <ButtonWithIcon size='sm' variant='ghost' icon={RefreshCw} onClick={() => checkUpdate.mutate()} disabled={checkUpdate.isPending}>{t('settings.checkUpdate')}</ButtonWithIcon>
                <ConfirmActionButton
                  cancelLabel={t('common.cancel')}
                  confirmLabel={t('common.apply')}
                  description={t('settings.stageUpdateDescription', { version: deployment.data?.latest_release?.version || '' })}
                  onConfirm={() => applyUpdate.mutate()}
                  disabled={!deployment.data?.latest_release?.version || applyUpdate.isPending}
                  label={t('common.apply')}
                  title={t('settings.stageUpdate')}
                />
                <ConfirmActionButton
                  cancelLabel={t('common.cancel')}
                  confirmLabel={t('settings.rollback')}
                  description={t('settings.rollbackDescription')}
                  onConfirm={() => rollback.mutate()}
                  disabled={rollback.isPending}
                  label={t('settings.rollback')}
                  title={t('settings.rollback')}
                />
                <ConfirmActionButton
                  cancelLabel={t('common.cancel')}
                  confirmLabel={t('settings.restart')}
                  description={t('settings.requestRestartDescription')}
                  onConfirm={() => restart.mutate()}
                  disabled={restart.isPending}
                  label={t('settings.restart')}
                  title={t('settings.requestRestart')}
                />
              </StartActions>
          </SectionCard>
          <SectionCard
            description={t('settings.serviceHealthDescription')}
            gap='normal'
            leading={Database}
            title={t('settings.serviceHealth')}
          >
              <HealthFieldList className='bg-[var(--surface-inset)]'>
                {deploymentHealthRows(deploymentHealth.data?.checks ?? []).map((check) => (
                  <HealthFieldItem
                    key={check.name}
                    label={deploymentHealthCheckLabel(check.name, t)}
                    status={deploymentHealthCheckStatus(check)}
                    value={deploymentHealthCheckValue(check, t)}
                    mono
                    truncate
                  />
                ))}
              </HealthFieldList>
          </SectionCard>
        </> : null}
        </Stack>
      </SurfaceSplit>
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

function deploymentHealthRows(checks: DeploymentHealthCheck[]) {
  return checks.length ? checks : [{ name: 'runtime', status: 'unknown', message: '' }]
}

function deploymentHealthCheckLabel(name: string, t: ReturnType<typeof useI18n>['t']) {
  switch (name) {
    case 'database':
      return t('settings.healthDatabase')
    case 'redis':
      return t('settings.healthRedis')
    case 'relay':
      return t('settings.healthRelay')
    case 'runtime':
      return t('settings.healthRuntime')
    default:
      return name.replaceAll('_', ' ')
  }
}

function deploymentHealthCheckStatus(check: DeploymentHealthCheck): HealthStatus {
  switch (check.status) {
    case 'up':
    case 'ready':
      return 'healthy'
    case 'down':
      return 'danger'
    case 'degraded':
    case 'not_configured':
      return 'warning'
    default:
      return 'unknown'
  }
}

function deploymentHealthCheckValue(check: DeploymentHealthCheck, t: ReturnType<typeof useI18n>['t']) {
  return check.message || check.status.replaceAll('_', ' ') || t('common.unknown')
}
