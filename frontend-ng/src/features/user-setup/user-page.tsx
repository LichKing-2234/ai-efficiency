import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Clipboard, KeyRound, RefreshCw, Terminal } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'
import { ActionGroup } from '@/components/primitives/action-group'
import { AppAlert } from '@/components/primitives/app-alert'
import { CardContentStack } from '@/components/primitives/card-content-stack'
import { CommandAccordion } from '@/components/primitives/command-accordion'
import { CommandStep } from '@/components/primitives/command-step'
import { ConfirmAction } from '@/components/primitives/confirm-action'
import { CredentialKeyPanel } from '@/components/primitives/credential-key-panel'
import { EntityCardHeader } from '@/components/primitives/entity-card-header'
import { PageEmpty } from '@/components/primitives/page-empty'
import { Page } from '@/components/primitives/page'
import { InfoTile, InfoTileGrid } from '@/components/primitives/info-tile'
import { InsetPanel } from '@/components/primitives/inset-panel'
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
              description={t('userSetup.descriptionShort')}
              actions={<Badge variant='ai' className='shrink-0 tnum'>{t('userSetup.groupsReadyShort', { ready: readyGroups, total: totalGroups })}</Badge>}
            />
            <CardContentStack gap='compact'>
              {providers.data?.message ? <InsetPanel muted>{providers.data.message}</InsetPanel> : null}
              {rows.map((provider) => (
                <ProviderButton
                  key={provider.id}
                  active={provider.id === selectedProviderId}
                  name={provider.display_name || provider.name}
                  baseUrl={provider.base_url}
                  primary={provider.is_primary}
                  ready={provider.groups.filter((group) => group.credential.state === 'existing_hidden').length}
                  total={provider.groups.length}
                  labels={{
                    primary: t('userSetup.primary'),
                    groupsReady: t('userSetup.groupsReadyShort', {
                      ready: provider.groups.filter((group) => group.credential.state === 'existing_hidden').length,
                      total: provider.groups.length
                    })
                  }}
                  onClick={() => {
                    const selection = chooseDefaultSelection(rows, { providerId: provider.id, groupId: null })
                    setSelectedProviderId(selection.providerId)
                    setSelectedGroupId(selection.groupId)
                    setTestResult(null)
                  }}
                />
              ))}
            </CardContentStack>
          </Card>
          <Card>
            <SectionCardHeader
              title={<span className='flex items-center gap-2'><Terminal className='text-[var(--ai)]' />{t('userSetup.cliWorkflow')}</span>}
              description={t('userSetup.cliDescription')}
            />
            <CardContentStack gap='compact'>
              <CommandStep step={1} command={installCommand} copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              <CommandStep step={2} command='ae-cli login' copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              <CommandStep step={3} command={discoverCommand} disabled={!selectedProvider} copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              <CommandStep step={4} command='ae-cli hooks enable --global' copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              <CommandStep step={5} command='ae-cli init' copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              <CommandStep step={6} command='ae-cli doctor' copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              <CommandAccordion title={t('userSetup.windowsInstaller')}>
                <CommandStep command={windowsInstallCommand} copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              </CommandAccordion>
            </CardContentStack>
          </Card>
        </Stack>
        <Stack>
          <Card>
            <EntityCardHeader
              title={selectedProvider ? selectedProvider.display_name || selectedProvider.name : t('userSetup.aiAccess')}
              description={<span className='mono break-all'>{selectedProvider?.base_url || t('userSetup.noProvider')}</span>}
              actions={(
                <ActionGroup wrap className='justify-start sm:justify-end'>
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
                    <InfoTile label={t('userSetup.group')} value={selectedGroup.group_name} />
                    <InfoTile label={t('userSetup.platform')} value={selectedGroup.platform} />
                    <InfoTile
                      label={t('userSetup.credential')}
                      value={selectedGroup.credential.state === 'existing_hidden' ? t('userSetup.ready') : t('userSetup.needsSetup')}
                      accent={selectedGroup.credential.state === 'existing_hidden'}
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
            <CardContent>
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
            </CardContent>
          </Card>
        </Stack>
      </div>
    </Page>
  )
}

export function ProviderButton({
  active,
  name,
  baseUrl,
  primary,
  ready,
  total,
  labels,
  onClick
}: {
  active: boolean
  name: string
  baseUrl: string
  primary: boolean
  ready: number
  total: number
  labels: { primary: string; groupsReady: string }
  onClick: () => void
}) {
  return (
    <SelectableCard
      active={active}
      onClick={onClick}
    >
      <SelectableCardHeader>
        <SelectableCardTitle>{name}</SelectableCardTitle>
        {primary ? <Badge variant='ai'>{labels.primary}</Badge> : null}
      </SelectableCardHeader>
      <SelectableCardMeta>{baseUrl}</SelectableCardMeta>
      <SelectableCardStatus tone={ready === total ? 'success' : 'warning'}>{labels.groupsReady}</SelectableCardStatus>
    </SelectableCard>
  )
}
