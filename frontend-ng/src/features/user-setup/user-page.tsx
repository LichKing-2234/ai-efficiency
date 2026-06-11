import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Clipboard, KeyRound, RefreshCw, Terminal } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card } from '@/components/ui/card'
import { ActionGroup } from '@/components/primitives/action-group'
import { AppAlert } from '@/components/primitives/app-alert'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { CommandAccordion } from '@/components/primitives/command-accordion'
import { CommandStep } from '@/components/primitives/command-step'
import { ConfirmAction } from '@/components/primitives/confirm-action'
import { CountBadge } from '@/components/primitives/count-badge'
import { CredentialKeyPanel } from '@/components/primitives/credential-key-panel'
import { EntityCardHeader } from '@/components/primitives/entity-card-header'
import { PageEmpty } from '@/components/primitives/page-empty'
import { Page } from '@/components/primitives/page'
import { InfoTile, InfoTileGrid } from '@/components/primitives/info-tile'
import { InsetPanel } from '@/components/primitives/inset-panel'
import { RecordMeta } from '@/components/primitives/record-meta'
import { SectionCardHeader } from '@/components/primitives/section-card-header'
import { SelectableCard, SelectableCardHeader, SelectableCardMeta, SelectableCardStatus, SelectableCardTitle } from '@/components/primitives/selectable-card'
import { Stack } from '@/components/primitives/stack'
import { LoadingState } from '@/components/primitives/data-state'
import { api } from '@/lib/api'
import { useI18n } from '@/lib/i18n/i18n'
import {
  buildDiscoverCommand,
  buildInstallCommand,
  buildProviderTestRequest,
  buildWindowsInstallCommand,
  chooseDefaultSelection,
  maskApiKey,
  secretStateKey,
  visibleCredentialSecret
} from './user-setup-state'
import { ProviderTestForm, type ProviderTestFormLabels } from './provider-test-form'
import type { GroupCredentialMutationResult, UserProviderTestResult } from '@/lib/api/types'

export function UserPage() {
  const { t } = useI18n()
  const qc = useQueryClient()
  const providers = useQuery({ queryKey: ['user-providers'], queryFn: api.userProviders })
  const [selectedProviderId, setSelectedProviderId] = useState<number | null>(null)
  const [selectedGroupId, setSelectedGroupId] = useState<string | null>(null)
  const [sessionSecrets, setSessionSecrets] = useState<Record<string, string>>({})
  const [revealed, setRevealed] = useState<Record<string, boolean>>({})
  const [model, setModel] = useState('')
  const [prompt, setPrompt] = useState('Hi')
  const [testResult, setTestResult] = useState<UserProviderTestResult | null>(null)
  const rows = providers.data?.providers ?? []
  const selectedProvider = rows.find((provider) => provider.id === selectedProviderId) ?? null
  const selectedGroup = selectedProvider?.groups.find((group) => group.group_id === selectedGroupId) ?? null
  const selectedSecretKey = selectedProvider && selectedGroup ? secretStateKey(selectedProvider.id, selectedGroup.group_id) : ''
  const secret = visibleCredentialSecret(selectedProvider?.id, selectedGroup, sessionSecrets)
  const secretIsRevealed = selectedSecretKey ? !!revealed[selectedSecretKey] : false
  const displayedSecret = secret ? (secretIsRevealed ? secret : maskApiKey(secret)) : ''
  const readyGroups = useMemo(() => rows.reduce((sum, provider) => sum + provider.groups.filter((group) => group.credential.state === 'existing_hidden').length, 0), [rows])
  const totalGroups = useMemo(() => rows.reduce((sum, provider) => sum + provider.groups.length, 0), [rows])
  const origin = typeof window === 'undefined' ? '' : window.location.origin
  const installCommand = buildInstallCommand(origin)
  const windowsInstallCommand = buildWindowsInstallCommand(origin)
  const discoverCommand = selectedProvider ? buildDiscoverCommand(selectedProvider.name) : t('userSetup.selectProviderFirst')
  const providerTestLabels: ProviderTestFormLabels = {
    createKeyBeforeTesting: t('userSetup.createKeyBeforeTesting'),
    loadingModels: t('userSetup.loadingModels'),
    model: t('userSetup.model'),
    platform: t('userSetup.platform'),
    prompt: t('userSetup.prompt'),
    runTest: t('userSetup.runTest'),
    testing: t('userSetup.testing')
  }
  const modelQuery = useQuery({
    queryKey: ['user-provider-models', selectedProvider?.id, selectedGroup?.group_id, selectedGroup?.platform, !!secret],
    queryFn: () => {
      if (!selectedProvider || !selectedGroup || !secret) throw new Error(t('userSetup.createKeyBeforeModels'))
      return api.userProviderModels(selectedProvider.id, selectedGroup.group_id, selectedGroup.platform)
    },
    enabled: !!selectedProvider && !!selectedGroup && !!secret
  })

  useEffect(() => {
    const selection = chooseDefaultSelection(rows, { providerId: selectedProviderId, groupId: selectedGroupId })
    setSelectedProviderId(selection.providerId)
    setSelectedGroupId(selection.groupId)
  }, [rows, selectedProviderId, selectedGroupId])

  useEffect(() => {
    const models = modelQuery.data?.models ?? []
    if (models.length > 0 && !models.some((item) => item.id === model)) {
      setModel(models[0].id)
    }
  }, [model, modelQuery.data?.models])

  function rememberSecret(result: GroupCredentialMutationResult) {
    if (!selectedProvider || !selectedGroup) return
    const key = secretStateKey(selectedProvider.id, selectedGroup.group_id)
    setSessionSecrets((value) => ({ ...value, [key]: result.secret }))
    setRevealed((value) => ({ ...value, [key]: true }))
  }

  const createCredential = useMutation({
    mutationFn: () => {
      if (!selectedProvider || !selectedGroup) throw new Error(t('userSetup.selectAccessGroup'))
      return api.createGroupCredential(selectedProvider.id, selectedGroup.group_id)
    },
    onSuccess: (result) => {
      rememberSecret(result)
      void qc.invalidateQueries({ queryKey: ['user-providers'] })
      void navigator.clipboard?.writeText(result.secret)
      toast.success(t('userSetup.credentialCreatedCopied'))
    }
  })
  const regenerateCredential = useMutation({
    mutationFn: () => {
      if (!selectedProvider || !selectedGroup) throw new Error(t('userSetup.selectAccessGroup'))
      return api.regenerateGroupCredential(selectedProvider.id, selectedGroup.group_id)
    },
    onSuccess: (result) => {
      rememberSecret(result)
      void qc.invalidateQueries({ queryKey: ['user-providers'] })
      void navigator.clipboard?.writeText(result.secret)
      toast.success(t('userSetup.credentialRegeneratedCopied'))
    }
  })
  const testProvider = useMutation({
    mutationFn: () => {
      if (!selectedProvider || !selectedGroup) throw new Error(t('userSetup.selectAccessGroup'))
      if (!secret) throw new Error(t('userSetup.createKeyBeforeTesting'))
      return api.testUserProvider(selectedProvider.id, buildProviderTestRequest(selectedGroup, model, prompt))
    },
    onSuccess: setTestResult,
    onError: (error) => setTestResult({ success: false, message: error.message })
  })

  if (providers.isLoading) return <LoadingState />

  return (
    <Page className='stagger'>
      <div className='split-rail'>
        <Stack>
          <Card>
            <SectionCardHeader
              title={t('userSetup.accountAccess')}
              actions={<CountBadge variant='ai'>{t('userSetup.groupsReadyShort', { ready: readyGroups, total: totalGroups })}</CountBadge>}
            />
            <CardContentStack gap='compact'>
              {providers.data?.message ? <InsetPanel muted>{providers.data.message}</InsetPanel> : null}
              {rows.map((provider) => {
                const ready = provider.groups.filter((group) => group.credential.state === 'existing_hidden').length
                const total = provider.groups.length
                return (
                  <SelectableCard
                    key={provider.id}
                    active={provider.id === selectedProviderId}
                    onClick={() => {
                      const selection = chooseDefaultSelection(rows, { providerId: provider.id, groupId: null })
                      setSelectedProviderId(selection.providerId)
                      setSelectedGroupId(selection.groupId)
                      setTestResult(null)
                    }}
                  >
                    <SelectableCardHeader>
                      <SelectableCardTitle>{provider.display_name || provider.name}</SelectableCardTitle>
                      {provider.is_primary ? <Badge variant='ai'>{t('userSetup.primary')}</Badge> : null}
                    </SelectableCardHeader>
                    <SelectableCardMeta>{provider.base_url}</SelectableCardMeta>
                    <SelectableCardStatus tone={ready === total ? 'success' : 'warning'}>
                      {t('userSetup.groupsReadyShort', { ready, total })}
                    </SelectableCardStatus>
                  </SelectableCard>
                )
              })}
            </CardContentStack>
          </Card>
          <Card>
            <SectionCardHeader
              title={t('userSetup.cliWorkflow')}
              leading={Terminal}
              description={t('userSetup.cliDescription')}
            />
            <CardContentStack gap='compact'>
              <CommandStep step={1} label={t('userSetup.installCli')} command={installCommand} copiedMessage={t('userSetup.commandCopied')} />
              <CommandStep step={2} label={t('userSetup.authenticate')} command='ae-cli login' copiedMessage={t('userSetup.commandCopied')} />
              <CommandStep step={3} label={t('userSetup.discoverProvider')} command={discoverCommand} disabled={!selectedProvider} copiedMessage={t('userSetup.commandCopied')} />
              <CommandStep step={4} label={t('userSetup.enableHooks')} command='ae-cli hooks enable --global' copiedMessage={t('userSetup.commandCopied')} />
              <CommandStep step={5} label={t('userSetup.verifySetup')} command='ae-cli doctor' copiedMessage={t('userSetup.commandCopied')} />
              <CommandAccordion title={t('userSetup.windowsInstaller')}>
                <CommandStep command={windowsInstallCommand} copiedMessage={t('userSetup.commandCopied')} />
              </CommandAccordion>
            </CardContentStack>
          </Card>
        </Stack>
        <Stack>
          <Card>
            <EntityCardHeader
              title={selectedProvider ? selectedProvider.display_name || selectedProvider.name : t('userSetup.aiAccess')}
              description={<RecordMeta wrap>{selectedProvider?.base_url || t('userSetup.noProvider')}</RecordMeta>}
              actions={(
                <ActionGroup align='responsive-end' wrap>
                  {(selectedProvider?.groups ?? []).map((group) => (
                  <Button
                    key={group.group_id}
                    size='sm'
                    variant={group.group_id === selectedGroupId ? 'default' : 'outline'}
                    onClick={() => {
                      setSelectedGroupId(group.group_id)
                      setTestResult(null)
                    }}
                  >
                    {group.group_name}
                  </Button>
                ))}
                </ActionGroup>
              )}
            />
            <CardContentStack gap='normal'>
              {selectedGroup ? (
                <>
                  <InfoTileGrid columns={3}>
                    <InfoTile compact label={t('userSetup.group')} value={selectedGroup.group_name} />
                    <InfoTile compact label={t('userSetup.platform')} value={selectedGroup.platform} />
                    <InfoTile
                      compact
                      label={t('userSetup.credential')}
                      value={selectedGroup.credential.state === 'existing_hidden' ? t('userSetup.ready') : t('userSetup.needsSetup')}
                      accent={selectedGroup.credential.state === 'existing_hidden' ? 'ai' : false}
                    />
                  </InfoTileGrid>
                  <CredentialKeyPanel
                    label={t('userSetup.apiKey')}
                    value={displayedSecret || t('userSetup.noKeyProvisioned')}
                    ready={!!secret}
                    icon={KeyRound}
                    actions={secret ? (
                      <>
                        <Button size='sm' variant='ghost' onClick={() => selectedSecretKey && setRevealed((value) => ({ ...value, [selectedSecretKey]: !secretIsRevealed }))}>
                          {secretIsRevealed ? t('common.hide') : t('userSetup.reveal')}
                        </Button>
                        <Button size='icon-sm' variant='ghost' aria-label={t('userSetup.copy')} onClick={() => {
                          void navigator.clipboard?.writeText(secret)
                          toast.success(t('userSetup.credentialCopied'))
                        }}>
                          <Clipboard />
                        </Button>
                      </>
                    ) : null}
                    footer={selectedGroup.credential.state === 'missing' ? (
                      <Button size='sm' disabled={createCredential.isPending} onClick={() => createCredential.mutate()}>
                        <KeyRound data-icon='inline-start' />
                        {t('userSetup.createKey')}
                      </Button>
                    ) : (
                      <ConfirmAction
                        trigger={<Button size='sm' variant='outline' disabled={regenerateCredential.isPending}><RefreshCw data-icon='inline-start' />{t('userSetup.regenerate')}</Button>}
                        title={t('userSetup.regenerateTitle')}
                        description={t('userSetup.regenerateDescription')}
                        confirmLabel={t('userSetup.regenerate')}
                        cancelLabel={t('common.cancel')}
                        onConfirm={() => regenerateCredential.mutate()}
                        disabled={regenerateCredential.isPending}
                      />
                    )}
                  />
                  {createCredential.error ? <AppAlert tone='error' title={createCredential.error.message} /> : null}
                  {regenerateCredential.error ? <AppAlert tone='error' title={regenerateCredential.error.message} /> : null}
                </>
              ) : (
                <PageEmpty title={t('userSetup.noAccessGroup')} />
              )}
            </CardContentStack>
          </Card>
          <Card>
            <SectionCardHeader title={t('userSetup.providerTest')} description={t('userSetup.providerTestDescription')} />
            <CardContentStack gap='compact'>
              {selectedGroup ? (
                <InsetPanel muted>
                  {t('userSetup.group')}: <span className='mono'>{selectedGroup.group_name}</span> · {t('userSetup.platform')}: <span className='mono'>{selectedGroup.platform}</span>
                </InsetPanel>
              ) : null}
              <ProviderTestForm
                canRun={!!selectedGroup && !!secret && !!model.trim()}
                error={modelQuery.error?.message}
                labels={providerTestLabels}
                loadingModels={modelQuery.isFetching}
                message={modelQuery.data?.message}
                model={model}
                modelFallbackPlaceholder={selectedProvider?.default_model || 'gpt-5.4'}
                modelOptions={modelQuery.data?.models ?? []}
                onModelChange={setModel}
                onPromptChange={setPrompt}
                onRun={() => testProvider.mutate()}
                platform={selectedGroup?.platform || ''}
                prompt={prompt}
                result={testResult}
                running={testProvider.isPending}
                secretMissing={!secret}
              />
            </CardContentStack>
          </Card>
        </Stack>
      </div>
    </Page>
  )
}
