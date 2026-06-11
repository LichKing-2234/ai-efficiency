import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { BarsH } from './charts'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'charts.tsx'), 'utf8')

describe('BarsH', () => {
  test('renders compact top-model rows with label, total, and share', () => {
    const html = renderToStaticMarkup(
      <BarsH
        rows={[
          { label: 'claude-sonnet', value: 4200, share: 0.42, color: 'var(--ai)' },
          { label: 'gpt-4.1', value: 1800, share: 0.18, color: 'var(--viz-output)' }
        ]}
      />
    )

    expect(html).toContain('data-slot="bars-h"')
    expect(html).toContain('data-slot="bars-h-row"')
    expect(html).toContain('claude-sonnet')
    expect(html).toContain('42%')
  })

  test('keeps the horizontal model-bar density inside the shared chart primitive', () => {
    expect(source).toContain("className={className} dataSlot='bars-h' gap='normal'")
    expect(source).toContain("className='mb-1 gap-3'")
    expect(source).toContain("className='min-w-0 font-medium text-[12px]'")
    expect(source).toContain("className='h-[9px] overflow-hidden rounded-[var(--r-full)] bg-[var(--surface-inset)]'")
  })
})
