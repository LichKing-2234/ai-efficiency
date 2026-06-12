import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { SecondaryActionButton } from './secondary-action-button'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'secondary-action-button.tsx'), 'utf8')

describe('SecondaryActionButton', () => {
  test('renders the shared outline action shell', () => {
    const html = renderToStaticMarkup(<SecondaryActionButton>Clear</SecondaryActionButton>)

    expect(html).toContain('data-slot="button"')
    expect(html).toContain('Clear')
  })

  test('sources the control from the shared button primitive', () => {
    expect(source).toContain("from '@/components/ui/button'")
    expect(source).toContain("return <Button variant='outline' {...props} />")
  })
})
