import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Clipboard, KeyRound, RefreshCw, Terminal, Zap } from 'lucide-react'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { AppAlert } from '@/components/primitives/app-alert'
import { ConfirmAction } from '@/components/primitives/confirm-action'
import { PageEmpty } from '@/components/primitives/page-empty'
import { Page, PageHeader } from '@/components/primitives/page'
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
  modelLabel,
  secretStateKey,
  visibleCredentialSecret
} from './user-setup-state'
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
      <PageHeader title={t('userSetup.title')} description={t('userSetup.description')} />
      <div className='split-rail'>
        <div className='flex flex-col gap-4'>
          <Card>
            <CardHeader className='flex-row items-center justify-between gap-3'>
              <div className='min-w-0'>
                <CardTitle>{t('userSetup.accountAccess')}</CardTitle>
                <CardDescription>{t('userSetup.descriptionShort')}</CardDescription>
              </div>
              <Badge variant='ai' className='shrink-0 tnum'>{t('userSetup.groupsReadyShort', { ready: readyGroups, total: totalGroups })}</Badge>
            </CardHeader>
            <CardContent className='flex flex-col gap-2'>
              {providers.data?.message ? <div className='rounded-md bg-muted p-3 text-muted-foreground text-sm'>{providers.data.message}</div> : null}
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
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <div className='flex items-center gap-2'>
                <Terminal className='text-[var(--ai)]' />
                <CardTitle>{t('userSetup.cliWorkflow')}</CardTitle>
              </div>
              <CardDescription>{t('userSetup.cliDescription')}</CardDescription>
            </CardHeader>
            <CardContent className='flex flex-col gap-2'>
              <CommandBlock step={1} command={installCommand} copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              <CommandBlock step={2} command='ae-cli login' copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              <CommandBlock step={3} command={discoverCommand} disabled={!selectedProvider} copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              <CommandBlock step={4} command='ae-cli hooks enable --global' copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              <CommandBlock step={5} command='ae-cli init' copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              <CommandBlock step={6} command='ae-cli doctor' copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} />
              <Accordion type='single' collapsible className='mt-2 rounded-md border border-border px-3'>
                <AccordionItem value='windows'>
                  <AccordionTrigger>{t('userSetup.windowsInstaller')}</AccordionTrigger>
                  <AccordionContent><CommandBlock command={windowsInstallCommand} copyLabel={t('userSetup.copy')} copiedMessage={t('userSetup.commandCopied')} /></AccordionContent>
                </AccordionItem>
              </Accordion>
            </CardContent>
          </Card>
        </div>
        <div className='flex flex-col gap-4'>
          <Card>
            <CardHeader className='gap-4 lg:flex-row lg:items-start lg:justify-between'>
              <div className='min-w-0'>
                <CardTitle className='text-base'>{selectedProvider ? selectedProvider.display_name || selectedProvider.name : t('userSetup.aiAccess')}</CardTitle>
                <CardDescription className='mono mt-1 break-all'>{selectedProvider?.base_url || t('userSetup.noProvider')}</CardDescription>
              </div>
              <div className='flex flex-wrap gap-2'>
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
              </div>
            </CardHeader>
            <CardContent className='flex flex-col gap-4'>
              {selectedGroup ? (
                <>
                  <div className='grid gap-3 md:grid-cols-3'>
                    <InfoTile label={t('userSetup.group')} value={selectedGroup.group_name} />
                    <InfoTile label={t('userSetup.platform')} value={selectedGroup.platform} />
                    <InfoTile
                      label={t('userSetup.credential')}
                      value={selectedGroup.credential.state === 'existing_hidden' ? t('userSetup.ready') : t('userSetup.needsSetup')}
                      accent={selectedGroup.credential.state === 'existing_hidden'}
                    />
                  </div>
                  <div className='rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-4'>
                    <div className='mb-2 font-semibold text-muted-foreground text-xs uppercase'>{t('userSetup.apiKey')}</div>
                    <div className='flex min-w-0 items-center gap-3 rounded-[var(--r-md)] border border-border bg-card px-3 py-3'>
                      <KeyRound className={secret ? 'text-[var(--ai)]' : 'text-muted-foreground'} />
                      <span className='mono min-w-0 flex-1 truncate text-[var(--ai-deep)] text-sm'>
                        {displayedSecret || t('userSetup.noKeyProvisioned')}
                      </span>
                      {secret ? (
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
                    </div>
                    <div className='mt-3 flex flex-wrap gap-2'>
                      {selectedGroup.credential.state === 'missing' ? (
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
                    </div>
                    {createCredential.error ? <AppAlert tone='error' title={createCredential.error.message} /> : null}
                    {regenerateCredential.error ? <AppAlert tone='error' title={regenerateCredential.error.message} /> : null}
                  </div>
                </>
              ) : (
                <PageEmpty title={t('userSetup.noAccessGroup')} />
              )}
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>{t('userSetup.providerTest')}</CardTitle>
              <CardDescription>{t('userSetup.providerTestDescription')}</CardDescription>
            </CardHeader>
            <CardContent>
              <FieldGroup>
                <div className='grid gap-3 md:grid-cols-2'>
                  <Field>
                    <FieldLabel>{t('userSetup.model')}</FieldLabel>
                    {modelQuery.data?.models?.length ? (
                      <Select value={model} onValueChange={setModel}>
                        <SelectTrigger className='w-full'><SelectValue /></SelectTrigger>
                        <SelectContent>
                          {modelQuery.data.models.map((item) => <SelectItem key={item.id} value={item.id}>{modelLabel(item)}</SelectItem>)}
                        </SelectContent>
                      </Select>
                    ) : (
                      <Input value={model} placeholder={modelQuery.isFetching ? t('userSetup.loadingModels') : selectedProvider?.default_model || 'gpt-5.4'} onChange={(event) => setModel(event.target.value)} />
                    )}
                  </Field>
                  <Field>
                    <FieldLabel>{t('userSetup.platform')}</FieldLabel>
                    <Input value={selectedGroup?.platform || ''} disabled />
                  </Field>
                </div>
                {modelQuery.data?.message ? <div className='text-muted-foreground text-sm'>{modelQuery.data.message}</div> : null}
                {modelQuery.error ? <AppAlert tone='error' title={modelQuery.error.message} /> : null}
                <Field>
                  <FieldLabel>{t('userSetup.prompt')}</FieldLabel>
                  <Textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} />
                </Field>
                <div className='flex flex-wrap items-center gap-3'>
                  <Button disabled={!selectedGroup || !secret || !model.trim() || testProvider.isPending} onClick={() => testProvider.mutate()}>
                    <Zap data-icon='inline-start' />
                    {testProvider.isPending ? t('userSetup.testing') : t('userSetup.runTest')}
                  </Button>
                  {!secret ? <span className='text-muted-foreground text-sm'>{t('userSetup.createKeyBeforeTesting')}</span> : null}
                  {testResult ? <Badge variant={testResult.success ? 'success' : 'warning'}>{testResult.message}</Badge> : null}
                </div>
                {testResult?.response ? <div className='rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-4 text-sm leading-7'>{testResult.response}</div> : null}
              </FieldGroup>
            </CardContent>
          </Card>
        </div>
      </div>
    </Page>
  )
}

function InfoTile({ label, value, accent = false }: { label: string; value: string; accent?: boolean }) {
  return (
    <div className='rounded-[var(--r-md)] border border-border bg-[var(--surface-inset)] p-3'>
      <div className='font-semibold text-muted-foreground text-xs uppercase'>{label}</div>
      <div className={accent ? 'mt-1 break-all font-semibold text-[var(--pos)] text-sm' : 'mt-1 break-all font-semibold text-sm'}>{value}</div>
    </div>
  )
}

function ProviderButton({
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
    <button
      className='rounded-[var(--r-md)] border border-border bg-card p-3 text-left transition hover:border-[var(--line-strong)] hover:bg-[var(--surface-2)] data-[active=true]:border-[var(--ai-line)] data-[active=true]:bg-[var(--ai-softer)]'
      data-active={active}
      onClick={onClick}
    >
      <div className='flex items-center justify-between gap-2'>
        <div className='min-w-0 truncate font-semibold text-sm'>{name}</div>
        {primary ? <Badge variant='ai'>{labels.primary}</Badge> : null}
      </div>
      <div className='mono mt-1 truncate text-muted-foreground text-[11px]'>{baseUrl}</div>
      <div className={ready === total ? 'mt-2 font-medium text-[var(--pos)] text-xs' : 'mt-2 font-medium text-[var(--warn)] text-xs'}>{labels.groupsReady}</div>
    </button>
  )
}

function CommandBlock({ command, disabled, copyLabel, copiedMessage, step }: { command: string; disabled?: boolean; copyLabel: string; copiedMessage: string; step?: number }) {
  return (
    <div className='flex items-center gap-3'>
      {step ? <div className='grid size-5 shrink-0 place-items-center rounded-full bg-[var(--ai-soft)] font-bold text-[11px] text-[var(--ai-deep)] tnum'>{step}</div> : null}
      <button
        className='flex min-w-0 flex-1 items-center justify-between gap-3 rounded-[var(--r-sm)] border border-border bg-[var(--surface-inset)] px-3 py-2 text-left text-xs disabled:cursor-not-allowed disabled:opacity-60'
        disabled={disabled}
        onClick={() => {
          void navigator.clipboard?.writeText(command)
          toast.success(copiedMessage)
        }}
      >
        <span className='mono min-w-0 truncate text-[var(--ai-deep)]'>$ {command}</span>
        <span className='inline-flex shrink-0 items-center gap-1 text-muted-foreground'>
          <Clipboard className='size-3.5' />
          {copyLabel}
        </span>
      </button>
    </div>
  )
}
