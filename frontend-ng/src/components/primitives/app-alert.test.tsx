import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { AppAlert } from './app-alert'

describe('AppAlert', () => {
  test('renders title and optional description through shadcn alert slots', () => {
    const html = renderToStaticMarkup(
      <AppAlert title='Heads up' description='Connect your account.' />
    )

    expect(html).toContain('data-slot="alert"')
    expect(html).toContain('data-slot="alert-title"')
    expect(html).toContain('data-slot="alert-description"')
    expect(html).toContain('Heads up')
    expect(html).toContain('Connect your account.')
  })

  test('renders alert actions in a standardized content slot', () => {
    const html = renderToStaticMarkup(
      <AppAlert
        title='Setup required'
        actions={<a href='/user'>Open setup</a>}
      />
    )

    expect(html).toContain('data-slot="app-alert-actions"')
    expect(html).toContain('mt-3')
    expect(html).toContain('Open setup')
  })

  test('keeps action spacing inside the primitive action slot', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('./app-alert.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).not.toContain("data-slot='app-alert-actions' className='mt-3'")
    expect(source).not.toContain("const appAlertActionsClass = 'mt-3'")
  })

  test('inherits the denser shared alert shell instead of generic card/muted typography', async () => {
    const source = await import('node:fs/promises').then((fs) =>
      fs.readFile(new URL('../ui/alert.tsx', import.meta.url), 'utf8')
    )

    expect(source).toContain('rounded-[var(--r-md)]')
    expect(source).toContain('bg-[var(--surface-inset)]')
    expect(source).toContain('text-[12.5px]')
    expect(source).toContain('text-[12px]')
    expect(source).toContain('bg-[var(--neg-soft)]')
    expect(source).not.toContain('rounded-lg')
    expect(source).not.toContain('bg-card')
    expect(source).not.toContain('text-sm text-muted-foreground')
  })
})
