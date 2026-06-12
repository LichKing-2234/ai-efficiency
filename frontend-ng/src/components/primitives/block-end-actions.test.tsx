import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { BlockEndActions } from './block-end-actions'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'block-end-actions.tsx'), 'utf8')

describe('BlockEndActions', () => {
  test('renders a shared block-end aligned action shell', () => {
    const html = renderToStaticMarkup(
      <BlockEndActions>
        <button type='button'>Start</button>
      </BlockEndActions>
    )

    expect(html).toContain('data-slot="block-end-actions"')
    expect(html).toContain('items-end')
    expect(html).toContain('justify-end')
    expect(html).toContain('Start')
  })

  test('sources layout from the shared action-group block-end mode', () => {
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).toContain("<ActionGroup align='block-end' dataSlot='block-end-actions'>{children}</ActionGroup>")
  })
})
