import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'login-form.tsx'), 'utf8')

describe('Login form composition', () => {
  test('uses the shared auth field control token instead of route-local control class strings', () => {
    expect(source).toContain("from '@/components/primitives/auth-field'")
    expect(source).toContain('authFieldControlClassName')
    expect(source).toContain("controlClassName={authFieldControlClassName}")
    expect(source).toContain("triggerClassName={`${authFieldControlClassName} w-full`}")
    expect(source).not.toContain("const fieldControlClassName = 'h-10 rounded-[var(--r-md)] bg-[var(--surface-inset)] px-3.5 text-[13px] shadow-none'")
  })
})
