import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { ProviderTestForm } from './provider-test-form'
import type { UserProviderModel, UserProviderTestResult } from '@/lib/api/types'

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
    expect(html).toContain('for="provider-test-model"')
    expect(html).toContain('for="provider-test-platform"')
    expect(html).toContain('for="provider-test-prompt"')
    expect(html).toContain('data-slot="action-group"')
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
