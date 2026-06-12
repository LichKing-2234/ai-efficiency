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
    expect(source).toContain("from '@/components/primitives/start-actions'")
    expect(source).toContain("from '@/components/primitives/quiet-action-button'")
    expect(source).toContain("from '@/components/primitives/link-action'")
    expect(source).toContain('<StartActions>')
    expect(source).toContain('<QuietActionButton')
    expect(source).toContain('<LinkAction asChild')
    expect(source).not.toContain("className='flex flex-col gap-2 sm:flex-row'")
    expect(source).not.toContain("className='mt-3 w-full'")
  })

  test('uses the shared auth info panel for local handoff guidance copy', () => {
    expect(source).toContain("from '@/components/primitives/auth-info-panel'")
    expect(source).toContain('<AuthInfoPanel>')
    expect(source).not.toContain("className='text-[12px] text-[var(--ink-3)]'")
  })

  test('exposes the localhost handoff entry instead of hiding the online-to-local auth bridge', () => {
    expect(source).toContain('initialOptions?: AuthOptions | null')
    expect(source).toContain("useState(selectInitialLoginSource(initialOptions))")
    expect(source).toContain("initialData: initialOptions ?? undefined")
    expect(source).toContain('localHandoffHref?: string | null')
    expect(source).toContain('resolvedLocalHandoffHref')
    expect(source).toContain('<a href={resolvedLocalHandoffHref}>')
    expect(source).toContain("href.searchParams.set('target', window.location.origin)")
    expect(routeSource).toContain('createServerFn')
    expect(routeSource).toContain('getAuthOptionsTarget')
    expect(routeSource).toContain('authOptions')
    expect(routeSource).toContain('process.env.AE_FRONTEND_BACKEND_URL')
    expect(routeSource).toContain("new URL('/oauth2/local', backendOrigin)")
    expect(routeSource).toContain('loader: () => getLoginBootstrap()')
    expect(source).toContain("t('auth.localHandoff')")
    expect(source).toContain("t('auth.localHandoffDescription')")
    expect(source).not.toContain('http://127.0.0.1:4317')
  })
})
