import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { StartActionsFeedback } from './start-actions-feedback'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'start-actions-feedback.tsx'), 'utf8')

describe('StartActionsFeedback', () => {
  test('renders shared description, feedback content, and start-aligned action row', () => {
    const html = renderToStaticMarkup(
      <StartActionsFeedback
        description='Ready to test the provider.'
        hint='Create a key before testing.'
        status={<span data-slot='status-pill'>Healthy</span>}
        actions={<button type='button'>Run test</button>}
      >
        <div data-slot='inline-alert'>Saved</div>
      </StartActionsFeedback>
    )

    expect(html).toContain('Ready to test the provider.')
    expect(html).toContain('data-slot="inline-alert"')
    expect(html).toContain('Saved')
    expect(html).toContain('data-slot="start-actions"')
    expect(html).toContain('Run test')
    expect(html).toContain('Create a key before testing.')
    expect(html).toContain('data-slot="status-pill"')
    expect(html).toContain('Healthy')
  })

  test('sources layout from shared field description and start actions primitives', () => {
    expect(source).toContain("from '@/components/ui/field'")
    expect(source).toContain("from '@/components/primitives/start-actions'")
    expect(source).toContain('{description ? <FieldDescription>{description}</FieldDescription> : null}')
    expect(source).toContain('<StartActions>')
    expect(source).toContain('{hint ? <FieldDescription>{hint}</FieldDescription> : null}')
  })
})
