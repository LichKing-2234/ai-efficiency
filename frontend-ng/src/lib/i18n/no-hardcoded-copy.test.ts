import { describe, expect, test } from 'vitest'
import { readdirSync, readFileSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'
import { formatMessage, messages, supportedLocales } from './messages'

const ROOT = new URL('../../', import.meta.url).pathname
const allowedLiteralFiles = new Set([
  'lib/i18n/messages.ts',
  'lib/i18n/no-hardcoded-copy.test.ts',
  'lib/api/server.ts',
  'lib/auth/gateway.ts',
  'lib/auth/cookies.ts',
  'routeTree.gen.ts'
])

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const full = join(dir, name)
    if (name === 'node_modules' || name === '.output') return []
    if (statSync(full).isDirectory()) return walk(full)
    return /\.(tsx?|css)$/.test(name) ? [full] : []
  })
}

describe('frontend-ng i18n resources', () => {
  test('defines the same message keys for every locale', () => {
    const [first, ...rest] = supportedLocales
    const expected = Object.keys(messages[first]).sort()
    for (const locale of rest) {
      expect(Object.keys(messages[locale]).sort()).toEqual(expected)
    }
  })

  test('formats simple interpolation without leaking braces', () => {
    expect(formatMessage('en-US', 'common.pageCount', { current: 2, total: 5 })).toBe('Page 2 / 5')
    expect(formatMessage('zh-CN', 'common.pageCount', { current: 2, total: 5 })).toBe('第 2 / 5 页')
  })

  test('keeps Chinese copy only in the zh-CN resource table', () => {
    const offenders = walk(ROOT)
      .map((file) => relative(ROOT, file))
      .filter((file) => !allowedLiteralFiles.has(file))
      .filter((file) => /[\u3400-\u9fff]/.test(readFileSync(join(ROOT, file), 'utf8')))
    expect(offenders).toEqual([])
  })
})
