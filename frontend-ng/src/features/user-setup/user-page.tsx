import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useEffect, useMemo, useState } from 'react'
import { toast } from 'sonner'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Accordion, AccordionContent, AccordionItem, AccordionTrigger } from '@/components/ui/accordion'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { AppAlert } from '@/components/primitives/app-alert'
import { ConfirmAction } from '@/components/primitives/confirm-action'
import { PageEmpty } from '@/components/primitives/page-empty'
import { Page, PageHeader } from '@/components/primitives/page'
import { LoadingState } from '@/components/primitives/data-state'
import { api } from '@/lib/api'
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
  const discoverCommand = selectedProvider ? buildDiscoverCommand(selectedProvider.name) : 'Select provider first'
  const modelQuery = useQuery({
    queryKey: ['user-provider-models', selectedProvider?.id, selectedGroup?.group_id, selectedGroup?.platform, !!secret],
    queryFn: () => {
      if (!selectedProvider || !selectedGroup || !secret) throw new Error('Create a key before loading models')
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
      if (!selectedProvider || !selectedGroup) throw new Error('Select an access group first')
      return api.createGroupCredential(selectedProvider.id, selectedGroup.group_id)
    },
    onSuccess: (result) => {
      rememberSecret(result)
      void qc.invalidateQueries({ queryKey: ['user-providers'] })
      void navigator.clipboard?.writeText(result.secret)
      toast.success('Credential created and copied')
    }
  })
  const regenerateCredential = useMutation({
    mutationFn: () => {
      if (!selectedProvider || !selectedGroup) throw new Error('Select an access group first')
      return api.regenerateGroupCredential(selectedProvider.id, selectedGroup.group_id)
    },
    onSuccess: (result) => {
      rememberSecret(result)
      void qc.invalidateQueries({ queryKey: ['user-providers'] })
      void navigator.clipboard?.writeText(result.secret)
      toast.success('Credential regenerated and copied')
    }
  })
  const testProvider = useMutation({
    mutationFn: () => {
      if (!selectedProvider || !selectedGroup) throw new Error('Select an access group first')
      if (!secret) throw new Error('Create a key before testing')
      return api.testUserProvider(selectedProvider.id, buildProviderTestRequest(selectedGroup, model, prompt))
    },
    onSuccess: setTestResult,
    onError: (error) => setTestResult({ success: false, message: error.message })
  })

  if (providers.isLoading) return <LoadingState />

  return (
    <Page>
      <PageHeader title='My Setup' description='CLI setup, AI access keys, model discovery, and provider testing through current Go APIs.' />
      <div className='grid gap-4 lg:grid-cols-[340px_minmax(0,1fr)]'>
        <div className='flex flex-col gap-4'>
          <Card>
            <CardHeader>
              <CardTitle>Account access</CardTitle>
              <CardDescription>{readyGroups}/{totalGroups} groups ready to use.</CardDescription>
            </CardHeader>
            <CardContent className='flex flex-col gap-2'>
              {providers.data?.message ? <div className='rounded-md bg-muted p-3 text-muted-foreground text-sm'>{providers.data.message}</div> : null}
              {rows.map((provider) => (
                <button
                  key={provider.id}
                  className='rounded-md border border-border bg-card p-3 text-left transition hover:border-foreground data-[active=true]:border-foreground data-[active=true]:bg-muted'
                  data-active={provider.id === selectedProviderId}
                  onClick={() => {
                    const selection = chooseDefaultSelection(rows, { providerId: provider.id, groupId: null })
                    setSelectedProviderId(selection.providerId)
                    setSelectedGroupId(selection.groupId)
                    setTestResult(null)
                  }}
                >
                  <div className='flex items-center justify-between gap-2'>
                    <div className='font-medium'>{provider.display_name || provider.name}</div>
                    {provider.is_primary ? <Badge variant='ai'>primary</Badge> : null}
                  </div>
                  <div className='mt-1 break-all text-muted-foreground text-xs'>{provider.base_url}</div>
                </button>
              ))}
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>CLI workflow</CardTitle>
              <CardDescription>Commands use this frontend origin and the selected provider.</CardDescription>
            </CardHeader>
            <CardContent className='flex flex-col gap-3'>
              <CommandBlock command={installCommand} />
              <CommandBlock command='ae-cli login' />
              <CommandBlock command={discoverCommand} disabled={!selectedProvider} />
              <CommandBlock command='ae-cli hooks enable --global' />
              <CommandBlock command='ae-cli init' />
              <CommandBlock command='ae-cli doctor' />
              <Accordion type='single' collapsible className='rounded-md border border-border px-3'>
                <AccordionItem value='windows'>
                  <AccordionTrigger>Windows installer</AccordionTrigger>
                  <AccordionContent><CommandBlock command={windowsInstallCommand} /></AccordionContent>
                </AccordionItem>
              </Accordion>
            </CardContent>
          </Card>
        </div>
        <div className='flex flex-col gap-4'>
          <Card>
            <CardHeader>
              <CardTitle>{selectedProvider ? selectedProvider.display_name || selectedProvider.name : 'AI access'}</CardTitle>
              <CardDescription>{selectedProvider?.base_url || 'No provider available'}</CardDescription>
            </CardHeader>
            <CardContent className='flex flex-col gap-4'>
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
              {selectedGroup ? (
                <>
                  <div className='grid gap-3 md:grid-cols-3'>
                    <InfoTile label='Group' value={selectedGroup.group_name} />
                    <InfoTile label='Platform' value={selectedGroup.platform} />
                    <InfoTile label='Credential' value={selectedGroup.credential.state === 'existing_hidden' ? 'Ready' : 'Needs setup'} />
                  </div>
                  <div className='rounded-md border border-border p-4'>
                    <div className='text-muted-foreground text-xs uppercase'>API key</div>
                    <div className='mt-2 break-all rounded-md bg-background px-3 py-2 font-mono text-[var(--ae-ai-2)] text-sm'>
                      {displayedSecret || '••••••••••••••••'}
                    </div>
                    <div className='mt-3 flex flex-wrap gap-2'>
                      {selectedGroup.credential.state === 'missing' ? (
                        <Button size='sm' disabled={createCredential.isPending} onClick={() => createCredential.mutate()}>
                          Create key
                        </Button>
                      ) : (
                        <ConfirmAction
                          trigger={<Button size='sm' variant='outline' disabled={regenerateCredential.isPending}>Regenerate</Button>}
                          title='Regenerate credential'
                          description='Existing local tool configs may need updating.'
                          confirmLabel='Regenerate'
                          cancelLabel='Cancel'
                          onConfirm={() => regenerateCredential.mutate()}
                          disabled={regenerateCredential.isPending}
                        />
                      )}
                      {secret ? (
                        <>
                          <Button size='sm' variant='outline' onClick={() => selectedSecretKey && setRevealed((value) => ({ ...value, [selectedSecretKey]: !secretIsRevealed }))}>
                            {secretIsRevealed ? 'Hide' : 'Reveal'}
                          </Button>
                          <Button size='sm' variant='ghost' onClick={() => {
                            void navigator.clipboard?.writeText(secret)
                            toast.success('Credential copied')
                          }}>
                            Copy
                          </Button>
                        </>
                      ) : null}
                    </div>
                    {createCredential.error ? <AppAlert tone='error' title={createCredential.error.message} /> : null}
                    {regenerateCredential.error ? <AppAlert tone='error' title={regenerateCredential.error.message} /> : null}
                  </div>
                </>
              ) : (
                <PageEmpty title='No access group is available for this account.' />
              )}
            </CardContent>
          </Card>
          <Card>
            <CardHeader>
              <CardTitle>Provider test</CardTitle>
              <CardDescription>Loads backend model choices for the selected group and runs the existing user provider test endpoint.</CardDescription>
            </CardHeader>
            <CardContent className='flex flex-col gap-3'>
              <div className='grid gap-3 md:grid-cols-2'>
                <label className='flex flex-col gap-1 text-sm'>
                  <span className='text-muted-foreground text-xs uppercase'>Model</span>
                  {modelQuery.data?.models?.length ? (
                    <Select value={model} onValueChange={setModel}>
                      <SelectTrigger className='w-full'><SelectValue /></SelectTrigger>
                      <SelectContent>
                        {modelQuery.data.models.map((item) => <SelectItem key={item.id} value={item.id}>{modelLabel(item)}</SelectItem>)}
                      </SelectContent>
                    </Select>
                  ) : (
                    <Input value={model} placeholder={modelQuery.isFetching ? 'Loading models' : selectedProvider?.default_model || 'gpt-5.4'} onChange={(event) => setModel(event.target.value)} />
                  )}
                </label>
                <label className='flex flex-col gap-1 text-sm'>
                  <span className='text-muted-foreground text-xs uppercase'>Platform</span>
                  <Input value={selectedGroup?.platform || ''} disabled />
                </label>
              </div>
              {modelQuery.data?.message ? <div className='text-muted-foreground text-sm'>{modelQuery.data.message}</div> : null}
              {modelQuery.error ? <AppAlert tone='error' title={modelQuery.error.message} /> : null}
              <label className='flex flex-col gap-1 text-sm'>
                <span className='text-muted-foreground text-xs uppercase'>Prompt</span>
                <Textarea value={prompt} onChange={(event) => setPrompt(event.target.value)} />
              </label>
              <div className='flex flex-wrap items-center gap-3'>
                <Button disabled={!selectedGroup || !secret || !model.trim() || testProvider.isPending} onClick={() => testProvider.mutate()}>
                  {testProvider.isPending ? 'Testing' : 'Run test'}
                </Button>
                {!secret ? <span className='text-muted-foreground text-sm'>Create a key before testing.</span> : null}
                {testResult ? <Badge variant={testResult.success ? 'success' : 'warning'}>{testResult.message}</Badge> : null}
              </div>
              {testResult?.response ? <div className='rounded-md bg-muted p-3 text-sm'>{testResult.response}</div> : null}
            </CardContent>
          </Card>
        </div>
      </div>
    </Page>
  )
}

function InfoTile({ label, value }: { label: string; value: string }) {
  return (
    <div className='rounded-md bg-muted p-3'>
      <div className='text-muted-foreground text-xs uppercase'>{label}</div>
      <div className='mt-1 break-all font-medium text-sm'>{value}</div>
    </div>
  )
}

function CommandBlock({ command, disabled }: { command: string; disabled?: boolean }) {
  return (
    <button
      className='flex items-center justify-between gap-3 rounded-md border border-border bg-background px-3 py-2 text-left font-mono text-[var(--ae-ai-2)] text-xs disabled:cursor-not-allowed disabled:opacity-60'
      disabled={disabled}
      onClick={() => {
        void navigator.clipboard?.writeText(command)
        toast.success('Command copied')
      }}
    >
      <span className='min-w-0 break-all'>$ {command}</span>
      <span className='shrink-0 text-muted-foreground'>copy</span>
    </button>
  )
}
