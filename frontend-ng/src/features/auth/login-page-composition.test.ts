import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'login-page.tsx'), 'utf8')

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
})
