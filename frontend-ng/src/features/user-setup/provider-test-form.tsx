import { Zap } from 'lucide-react'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Field, FieldGroup, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { ActionGroup } from '@/components/primitives/action-group'
import { AppAlert } from '@/components/primitives/app-alert'
import { InsetPanel } from '@/components/primitives/inset-panel'
import { SelectField } from '@/components/primitives/select-field'
import { modelLabel } from './user-setup-state'
import type { UserProviderModel, UserProviderTestResult } from '@/lib/api/types'

export interface ProviderTestFormLabels {
  createKeyBeforeTesting: string
  loadingModels: string
  model: string
  platform: string
  prompt: string
  runTest: string
  testing: string
}

export function ProviderTestForm({
  canRun,
  error,
  labels,
  loadingModels,
  message,
  model,
  modelFallbackPlaceholder,
  modelOptions = [],
  onModelChange,
  onPromptChange,
  onRun,
  platform,
  prompt,
  result,
  running,
  secretMissing
}: {
  canRun: boolean
  error?: string
  labels: ProviderTestFormLabels
  loadingModels?: boolean
  message?: string
  model: string
  modelFallbackPlaceholder?: string
  modelOptions?: UserProviderModel[]
  onModelChange: (model: string) => void
  onPromptChange: (prompt: string) => void
  onRun: () => void
  platform: string
  prompt: string
  result?: UserProviderTestResult | null
  running?: boolean
  secretMissing?: boolean
}) {
  return (
    <FieldGroup>
      <div className='grid gap-3 md:grid-cols-2'>
        {modelOptions.length ? (
          <SelectField
            id='provider-test-model'
            label={labels.model}
            options={modelOptions.map((item) => ({ label: modelLabel(item), value: item.id }))}
            triggerClassName='w-full'
            value={model}
            onValueChange={onModelChange}
          />
        ) : (
          <Field>
            <FieldLabel htmlFor='provider-test-model'>{labels.model}</FieldLabel>
            <Input
              id='provider-test-model'
              value={model}
              placeholder={loadingModels ? labels.loadingModels : modelFallbackPlaceholder}
              onChange={(event) => onModelChange(event.target.value)}
            />
          </Field>
        )}
        <Field>
          <FieldLabel htmlFor='provider-test-platform'>{labels.platform}</FieldLabel>
          <Input id='provider-test-platform' value={platform} disabled />
        </Field>
      </div>
      {message ? <div className='text-muted-foreground text-sm'>{message}</div> : null}
      {error ? <AppAlert tone='error' title={error} /> : null}
      <Field>
        <FieldLabel htmlFor='provider-test-prompt'>{labels.prompt}</FieldLabel>
        <Textarea id='provider-test-prompt' value={prompt} onChange={(event) => onPromptChange(event.target.value)} />
      </Field>
      <ActionGroup wrap className='justify-start'>
        <Button disabled={!canRun || running} onClick={onRun}>
          <Zap data-icon='inline-start' />
          {running ? labels.testing : labels.runTest}
        </Button>
        {secretMissing ? <span className='text-muted-foreground text-sm'>{labels.createKeyBeforeTesting}</span> : null}
        {result ? <Badge variant={result.success ? 'success' : 'warning'}>{result.message}</Badge> : null}
      </ActionGroup>
      {result?.response ? <InsetPanel comfortable>{result.response}</InsetPanel> : null}
    </FieldGroup>
  )
}
