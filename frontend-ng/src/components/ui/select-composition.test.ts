import { describe, expect, test } from 'vitest'
import { readFileSync, readdirSync, statSync } from 'node:fs'
import { join, relative } from 'node:path'

const ROOT = new URL('../../', import.meta.url).pathname
const SOURCE_ROOTS = ['features', 'components']

function walk(dir: string): string[] {
  return readdirSync(dir).flatMap((name) => {
    const full = join(dir, name)
    if (statSync(full).isDirectory()) return walk(full)
    return /\.tsx$/.test(name) ? [full] : []
  })
}

describe('Select composition', () => {
  test('groups select items inside SelectGroup', () => {
    const offenders: string[] = []

    for (const root of SOURCE_ROOTS) {
      for (const file of walk(join(ROOT, root))) {
        const relativeFile = relative(ROOT, file)
        const source = readFileSync(file, 'utf8')
        const blocks = source.matchAll(/<SelectContent[\s\S]*?<\/SelectContent>/g)

        for (const block of blocks) {
          if (block[0].includes('<SelectItem') && !block[0].includes('<SelectGroup')) {
            offenders.push(relativeFile)
          }
        }
      }
    }

    expect(offenders).toEqual([])
  })
})
