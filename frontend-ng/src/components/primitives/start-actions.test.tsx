import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { StartActions } from './start-actions'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'start-actions.tsx'), 'utf8')

describe('StartActions', () => {
  test('renders a shared start-aligned wrapped action row', () => {
    const html = renderToStaticMarkup(
      <StartActions>
        <button type='button'>Test</button>
        <button type='button'>Save</button>
      </StartActions>
    )

    expect(html).toContain('data-slot="start-actions"')
    expect(html).toContain('justify-start')
    expect(html).toContain('flex-wrap')
    expect(html).toContain('Test')
    expect(html).toContain('Save')
  })

  test('sources layout from the shared action-group primitive', () => {
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("<ActionGroup align='start' dataSlot='start-actions' wrap>")
  })
})
