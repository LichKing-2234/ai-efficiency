import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { PrimaryActionButton } from './primary-action-button'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'primary-action-button.tsx'), 'utf8')

describe('PrimaryActionButton', () => {
  test('renders the shared primary action shell', () => {
    const html = renderToStaticMarkup(<PrimaryActionButton>Apply</PrimaryActionButton>)

    expect(html).toContain('data-slot="button"')
    expect(html).toContain('Apply')
  })

  test('sources the control from the shared button primitive', () => {
    expect(source).toContain("from '@/components/ui/button'")
    expect(source).toContain('return <Button {...props} />')
  })
})
