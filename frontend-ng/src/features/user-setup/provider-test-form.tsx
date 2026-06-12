import { Zap } from 'lucide-react'
import { FieldDescription, FieldGroup } from '@/components/ui/field'
import { AppAlert } from '@/components/primitives/app-alert'
import { formInsetControlClassName, formInsetTextareaClassName } from '@/components/primitives/auth-field'
import { ButtonWithIcon } from '@/components/primitives/button-with-icon'
import { ControlGrid } from '@/components/primitives/control-grid'
import { InsetPanel } from '@/components/primitives/inset-panel'
import { SelectField } from '@/components/primitives/select-field'
import { StartActions } from '@/components/primitives/start-actions'
import { StatusBadge } from '@/components/primitives/status-badge'
import { TextField } from '@/components/primitives/text-field'
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
      <ControlGrid variant='two-column'>
        {modelOptions.length ? (
          <SelectField
            id='provider-test-model'
            label={labels.model}
            options={modelOptions.map((item) => ({ label: modelLabel(item), value: item.id }))}
            triggerClassName={`${formInsetControlClassName} w-full`}
            value={model}
            onValueChange={onModelChange}
          />
        ) : (
          <TextField
            id='provider-test-model'
            label={labels.model}
            controlClassName={formInsetControlClassName}
            placeholder={loadingModels ? labels.loadingModels : modelFallbackPlaceholder}
            value={model}
            onChange={onModelChange}
          />
        )}
        <TextField id='provider-test-platform' label={labels.platform} controlClassName={formInsetControlClassName} value={platform} disabled />
      </ControlGrid>
      {message ? <FieldDescription>{message}</FieldDescription> : null}
      {error ? <AppAlert tone='error' title={error} /> : null}
      <TextField
        id='provider-test-prompt'
        label={labels.prompt}
        controlClassName={formInsetTextareaClassName}
        multiline
        value={prompt}
        onChange={onPromptChange}
      />
      <StartActions>
        <ButtonWithIcon size='sm' icon={Zap} disabled={!canRun || running} onClick={onRun}>
          {running ? labels.testing : labels.runTest}
        </ButtonWithIcon>
        {secretMissing ? <FieldDescription>{labels.createKeyBeforeTesting}</FieldDescription> : null}
        {result ? <StatusBadge value={result.success ? 'success' : 'error'} label={result.message} /> : null}
      </StartActions>
      {result?.response ? <InsetPanel comfortable>{result.response}</InsetPanel> : null}
    </FieldGroup>
  )
}
