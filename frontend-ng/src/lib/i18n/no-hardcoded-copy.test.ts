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

const visibleCopyRoots = ['features/', 'components/layout/']
const allowedVisibleLiterals = new Set([
  'ABCD-EFGH',
  'AE',
  'AI',
  'Claude',
  'Codex',
  'Bitbucket',
  'GitHub',
  'HTTP',
  'Kiro',
  'LDAP',
  'PR',
  'SSH'
])

const visibleCopyPatterns = [
  />\s*([A-Z][A-Za-z0-9 #./:+?%-]*(?:\s+[A-Za-z0-9 #./:+?%-]+)*)\s*</g,
  /\b(?:placeholder|title|description|confirmLabel|cancelLabel)=['"]([A-Z][^'"]*[A-Za-z][^'"]*)['"]/g,
  /\btoast\.(?:success|error|warning|info)\(['"]([A-Z][^'"]*[A-Za-z][^'"]*)['"]\)/g,
  /\bnew Error\(['"]([A-Z][^'"]*[A-Za-z][^'"]*)['"]\)/g
]

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
    expect(formatMessage('en-US', 'userSetup.groupsReadyShort', { ready: 3, total: 5 })).toBe('3/5 ready')
  })

  test('keeps Chinese copy only in the zh-CN resource table', () => {
    const offenders = walk(ROOT)
      .map((file) => relative(ROOT, file))
      .filter((file) => !allowedLiteralFiles.has(file))
      .filter((file) => /[\u3400-\u9fff]/.test(readFileSync(join(ROOT, file), 'utf8')))
    expect(offenders).toEqual([])
  })

  test('keeps page-level visible English copy in message resources', () => {
    const offenders: string[] = []
    for (const file of walk(ROOT)) {
      const relativeFile = relative(ROOT, file)
      if (!visibleCopyRoots.some((root) => relativeFile.startsWith(root))) continue
      const source = readFileSync(file, 'utf8')
      for (const pattern of visibleCopyPatterns) {
        for (const match of source.matchAll(pattern)) {
          const literal = match[1].trim()
          if (!literal || allowedVisibleLiterals.has(literal)) continue
          if (/^\$|^https?:|^\/|^\d+$/.test(literal)) continue
          offenders.push(`${relativeFile}: ${literal}`)
        }
      }
    }
    expect(offenders).toEqual([])
  })
})
