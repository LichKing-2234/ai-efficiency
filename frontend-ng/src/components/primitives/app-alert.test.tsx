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

    expect(source).not.toContain("data-slot='app-alert-actions' className='mt-3'")
  })
})
