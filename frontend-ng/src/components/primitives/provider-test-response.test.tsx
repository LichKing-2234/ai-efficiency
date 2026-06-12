import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { ProviderTestResponse } from './provider-test-response'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'provider-test-response.tsx'), 'utf8')

describe('ProviderTestResponse', () => {
  test('renders a comfortable inset response preview surface', () => {
    const html = renderToStaticMarkup(<ProviderTestResponse>pong</ProviderTestResponse>)

    expect(html).toContain('data-slot="provider-test-response"')
    expect(html).toContain('pong')
    expect(html).toContain('leading-7')
  })

  test('sources provider test response previews from the shared inset panel primitive', () => {
    expect(source).toContain("from '@/components/primitives/inset-panel'")
    expect(source).toContain("<InsetPanel comfortable dataSlot='provider-test-response'>")
  })
})
