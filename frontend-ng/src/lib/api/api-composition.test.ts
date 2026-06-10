import { readFileSync } from 'node:fs'
import { join } from 'node:path'
import { describe, expect, test } from 'vitest'

const ROOT = new URL('./', import.meta.url).pathname

describe('api composition', () => {
  test('reads backend health-ready as raw deployment health payload', () => {
    const client = readFileSync(join(ROOT, 'client.ts'), 'utf8')
    const api = readFileSync(join(ROOT, 'index.ts'), 'utf8')
    const types = readFileSync(join(ROOT, 'types.ts'), 'utf8')

    expect(client).toContain('export async function apiRawFetch<T>')
    expect(api).toContain('apiRawFetch')
    expect(api).toContain("ready: () => apiRawFetch<DeploymentReadyReport>('/health/ready')")
    expect(types).toContain('export interface DeploymentReadyReport')
    expect(types).toContain('checks: DeploymentHealthCheck[]')
  })
})
