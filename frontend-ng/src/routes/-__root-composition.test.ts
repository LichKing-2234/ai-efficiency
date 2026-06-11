import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), '__root.tsx'), 'utf8')

describe('Root auth frame composition', () => {
  test('fails fast on unavailable backend instead of leaving the shell in loading state', () => {
    expect(source).toContain("retry: false")
    expect(source).toContain("<ErrorState")
    expect(source).toContain("message={error.message}")
    expect(source).toContain("retryLabel={t('common.retry')}")
    expect(source).not.toContain("if (isLoading && !user) return <AppShell user={null}><LoadingState label={t('auth.loadingAccount')} /></AppShell>\n  return <AppShell user={user ?? null}>{children}</AppShell>")
  })

  test('redirects to login only for auth failures, not generic backend proxy outages', () => {
    expect(source).toContain("if (!isPublic && error instanceof ApiError && (error.status === 401 || error.status === 403))")
    expect(source).not.toContain('if (!isPublic && error)')
  })
})
