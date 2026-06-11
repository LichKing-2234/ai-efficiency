import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'login-page.tsx'), 'utf8')
const routeSource = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '../../routes/login.tsx'), 'utf8')

describe('Login page composition', () => {
  test('uses the shared auth surface instead of page-local shell markup', () => {
    expect(source).toContain("from '@/components/primitives/auth-surface'")
    expect(source).toContain('<AuthSurface')
    expect(source).not.toContain("<main className='grid min-h-screen place-items-center bg-background p-4'>")
    expect(source).not.toContain("<Card className='w-full max-w-md'>")
  })

  test('delegates dev login button layout to the shared auth surface', () => {
    expect(source).toContain('actions={')
    expect(source).not.toContain("className='mt-3 w-full'")
  })

  test('exposes the localhost handoff entry instead of hiding the online-to-local auth bridge', () => {
    expect(source).toContain('localHandoffHref?: string | null')
    expect(source).toContain('<a href={localHandoffHref}>')
    expect(routeSource).toContain('process.env.AE_FRONTEND_BACKEND_URL')
    expect(routeSource).toContain("new URL('/oauth2/local', backendOrigin)")
    expect(routeSource).toContain('const currentUrl = new URL(location.href)')
    expect(routeSource).toContain("handoffUrl.searchParams.set('target', currentUrl.origin)")
    expect(source).toContain("t('auth.localHandoff')")
    expect(source).toContain("t('auth.localHandoffDescription')")
    expect(source).not.toContain('http://127.0.0.1:4317')
  })
})
