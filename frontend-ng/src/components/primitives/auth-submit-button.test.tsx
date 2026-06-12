import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { AuthSubmitButton } from './auth-submit-button'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'auth-submit-button.tsx'), 'utf8')

describe('AuthSubmitButton', () => {
  test('renders the shared full-width primary auth action shell', () => {
    const html = renderToStaticMarkup(
      <AuthSubmitButton disabled>
        Sign in
      </AuthSubmitButton>
    )

    expect(html).toContain('data-slot="button"')
    expect(html).toContain('Sign in')
    expect(html).toContain('w-full')
  })

  test('sources the auth action from the shared button primitive', () => {
    expect(source).toContain("from '@/components/ui/button'")
    expect(source).toContain("return <Button className='w-full' {...props} />")
  })
})
