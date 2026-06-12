import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ProviderTestForm } from './provider-test-form'
import type { UserProviderModel, UserProviderTestResult } from '@/lib/api/types'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'provider-test-form.tsx'), 'utf8')

const models: UserProviderModel[] = [
  { id: 'claude-sonnet', display_name: 'Claude Sonnet' },
  { id: 'gpt-5.4', display_name: 'gpt-5.4' }
]

describe('ProviderTestForm', () => {
  test('renders model, platform, prompt, and actions through shadcn field primitives', () => {
    const html = renderToStaticMarkup(
      <ProviderTestForm
        canRun
        labels={labels()}
        model='claude-sonnet'
        modelOptions={models}
        platform='anthropic'
        prompt='Hi'
        onModelChange={() => undefined}
        onPromptChange={() => undefined}
        onRun={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field-group"')
    expect(html).toContain('data-slot="control-grid"')
    expect(html).toContain('for="provider-test-model"')
    expect(html).toContain('for="provider-test-platform"')
    expect(html).toContain('for="provider-test-prompt"')
    expect(html).toContain('data-slot="start-actions"')
    expect(html).toContain('Claude Sonnet (claude-sonnet)')
  })

  test('renders fallback model input and result response states', () => {
    const result: UserProviderTestResult = {
      success: true,
      message: 'OK',
      response: 'pong'
    }
    const html = renderToStaticMarkup(
      <ProviderTestForm
        canRun={false}
        labels={labels()}
        model='custom-model'
        modelFallbackPlaceholder='gpt-5.4'
        platform='openai'
        prompt='Hello'
        result={result}
        secretMissing
        onModelChange={() => undefined}
        onPromptChange={() => undefined}
        onRun={() => undefined}
      />
    )

    expect(html).toContain('value="custom-model"')
    expect(html).toContain('Create a key before testing.')
    expect(html).toContain('OK')
    expect(html).toContain('pong')
  })

  test('uses shared control grid for paired model and platform fields', () => {
    expect(source).toContain("from '@/components/primitives/form-field-group'")
    expect(source).toContain('<FormFieldGroup>')
    expect(source).not.toContain("from '@/components/ui/field'")
    expect(source).toContain("from '@/components/primitives/control-grid'")
    expect(source).toContain("<ControlGrid variant='two-column'>")
    expect(source).not.toContain("<div className='grid gap-3 md:grid-cols-2'>")
  })

  test('uses shadcn field descriptions for form feedback copy', () => {
    expect(source).toContain("from '@/components/primitives/start-actions-feedback'")
    expect(source).toContain('description={message}')
    expect(source).toContain("hint={secretMissing ? labels.createKeyBeforeTesting : undefined}")
    expect(source).not.toContain("<div className='text-muted-foreground text-sm'>{message}</div>")
    expect(source).not.toContain("<span className='text-muted-foreground text-sm'>{labels.createKeyBeforeTesting}</span>")
  })

  test('uses shared start-aligned action groups for provider test actions', () => {
    expect(source).toContain("from '@/components/primitives/start-actions-feedback'")
    expect(source).toContain('<StartActionsFeedback')
    expect(source).toContain("from '@/components/primitives/button-with-icon'")
    expect(source).toContain("<ButtonWithIcon size='sm' icon={Zap} disabled={!canRun || running} onClick={onRun}>")
    expect(source).not.toContain("from '@/components/primitives/start-actions'")
    expect(source).not.toContain("<Button disabled={!canRun || running} onClick={onRun}>")
    expect(source).not.toContain("<ActionGroup wrap align='start'>")
    expect(source).not.toContain("<ActionGroup wrap className='justify-start'>")
  })

  test('uses shared inset field chrome for the reference provider test form controls', () => {
    expect(source).toContain("from '@/components/primitives/auth-field'")
    expect(source).toContain('formInsetControlClassName')
    expect(source).toContain('formInsetTextareaClassName')
    expect(source).toContain("triggerClassName={`${formInsetControlClassName} w-full`}")
    expect(source).toContain("controlClassName={formInsetControlClassName}")
    expect(source).toContain("controlClassName={formInsetTextareaClassName}")
    expect(source).not.toContain("const fieldControlClassName = 'h-10 rounded-[var(--r-md)] bg-[var(--surface-inset)] shadow-none'")
    expect(source).not.toContain("const promptControlClassName = 'min-h-24 rounded-[var(--r-md)] bg-[var(--surface-inset)] shadow-none'")
  })

  test('uses a shared provider test response preview surface instead of a raw inset panel', () => {
    expect(source).toContain("from '@/components/primitives/provider-test-response'")
    expect(source).toContain('<ProviderTestResponse')
    expect(source).not.toContain("from '@/components/primitives/inset-panel'")
    expect(source).not.toContain('{result?.response ? <InsetPanel comfortable>{result.response}</InsetPanel> : null}')
  })
})

function labels() {
  return {
    createKeyBeforeTesting: 'Create a key before testing.',
    loadingModels: 'Loading models',
    model: 'Model',
    platform: 'Platform',
    prompt: 'Prompt',
    runTest: 'Run test',
    testing: 'Testing'
  }
}
