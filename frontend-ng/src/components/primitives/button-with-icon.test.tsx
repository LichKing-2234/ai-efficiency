import { RefreshCw } from 'lucide-react'
import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { ButtonWithIcon } from './button-with-icon'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'button-with-icon.tsx'), 'utf8')

describe('ButtonWithIcon', () => {
  test('renders a shared cta button with leading icon metadata', () => {
    const html = renderToStaticMarkup(
      <ButtonWithIcon icon={RefreshCw} variant='ghost'>
        Check update
      </ButtonWithIcon>
    )

    expect(html).toContain('data-slot="button"')
    expect(html).toContain("data-icon='inline-start'".replace(/'/g, '"'))
    expect(html).toContain('Check update')
  })

  test('supports shared asChild link composition while keeping the leading icon contract', () => {
    const html = renderToStaticMarkup(
      <ButtonWithIcon asChild icon={RefreshCw} variant='outline'>
        <a href='/target'>Open</a>
      </ButtonWithIcon>
    )

    expect(html).toContain('href="/target"')
    expect(html).toContain("data-icon='inline-start'".replace(/'/g, '"'))
    expect(html).toContain('Open')
  })

  test('supports trailing icon composition for reference-style forward actions', () => {
    const html = renderToStaticMarkup(
      <ButtonWithIcon icon={RefreshCw} iconPosition='end'>
        Continue
      </ButtonWithIcon>
    )

    expect(html).toContain('Continue')
    expect(html).toContain("data-icon='inline-end'".replace(/'/g, '"'))
  })

  test('sources both leading and trailing icon paths from one shared primitive', () => {
    expect(source).toContain("iconPosition = 'start'")
    expect(source).toContain("const iconNode = <Icon data-icon={iconPosition === 'end' ? 'inline-end' : 'inline-start'} />")
    expect(source).toContain('{iconPosition === ')
  })
})
