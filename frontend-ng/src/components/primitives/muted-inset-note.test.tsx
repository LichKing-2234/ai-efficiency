import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { MutedInsetNote } from './muted-inset-note'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'muted-inset-note.tsx'), 'utf8')

describe('MutedInsetNote', () => {
  test('renders a shared muted inset note surface', () => {
    const html = renderToStaticMarkup(<MutedInsetNote>Background sync pending</MutedInsetNote>)

    expect(html).toContain('data-slot="muted-inset-note"')
    expect(html).toContain('Background sync pending')
    expect(html).toContain('text-[var(--ink-3)]')
  })

  test('supports the compact inset note variant', () => {
    const html = renderToStaticMarkup(<MutedInsetNote compact>Bind this repository before syncing.</MutedInsetNote>)

    expect(html).toContain('data-slot="muted-inset-note"')
    expect(html).toContain('px-[11px] py-[9px]')
  })

  test('sources note styling from the shared inset panel primitive', () => {
    expect(source).toContain("from '@/components/primitives/inset-panel'")
    expect(source).toContain("<InsetPanel compact={compact} dataSlot='muted-inset-note' muted>")
  })
})
