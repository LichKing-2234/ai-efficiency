import { renderToStaticMarkup } from 'react-dom/server'
import { readFileSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import { describe, expect, test } from 'vitest'
import { EmptyState, ErrorState, LoadingState } from './data-state'

const source = readFileSync(resolve(dirname(fileURLToPath(import.meta.url)), 'data-state.tsx'), 'utf8')

describe('DataState', () => {
  test('renders loading state rhythm through stable slots', () => {
    const html = renderToStaticMarkup(<LoadingState label='Fetching repositories' />)

    expect(html).toContain('data-slot="data-state-loading"')
    expect(html).toContain('data-slot="data-state-loading-content"')
    expect(html).toContain('data-slot="data-state-loading-copy"')
    expect(html).toContain('Fetching repositories')
  })

  test('renders empty state title and description through stable slots', () => {
    const html = renderToStaticMarkup(<EmptyState title='No records' description='Try another filter.' action={<button type='button'>Reset</button>} />)

    expect(html).toContain('data-slot="data-state-empty"')
    expect(html).toContain('data-slot="empty-title"')
    expect(html).toContain('data-slot="empty-description"')
    expect(html).toContain('data-slot="empty-content"')
    expect(html).toContain('No records')
    expect(html).toContain('Try another filter.')
    expect(html).toContain('Reset')
  })

  test('renders error state with caller-provided retry copy', () => {
    const html = renderToStaticMarkup(<ErrorState message='Request failed' retryLabel='Try again' onRetry={() => {}} />)

    expect(html).toContain('data-slot="data-state-error"')
    expect(html).toContain('data-slot="data-state-error-content"')
    expect(html).toContain('data-slot="data-state-error-message"')
    expect(html).toContain('Request failed')
    expect(html).toContain('Try again')
    expect(html).not.toContain('Retry')
  })

  test('uses shared card content stacks for standardized data state card bodies', () => {
    expect(source).toContain("from '@/components/primitives/card-content-stack'")
    expect(source).not.toContain("import { Card, CardContent } from '@/components/ui/card'")
    expect(source).not.toContain('<CardContent ')
  })

  test('uses shared stack and row primitives for empty and error state content layout', () => {
    expect(source).toContain("from '@/components/ui/empty'")
    expect(source).toContain("from '@/components/primitives/stack'")
    expect(source).toContain("from '@/components/primitives/action-group'")
    expect(source).not.toContain("className='items-center p-8 text-center'")
    expect(source).not.toContain("className='items-center justify-between p-5 text-sm md:flex-row'")
  })

  test('uses shared centered loading copy and richer error copy structure', () => {
    expect(source).toContain("dataSlot='data-state-loading-copy'")
    expect(source).toContain("data-slot='data-state-error-title'")
    expect(source).toContain("dataSlot='data-state-error-copy'")
    expect(source).toContain("<CardContentStack className='p-[18px]' dataSlot='data-state-loading-content'>")
    expect(source).toContain("<Stack className='items-center text-center text-[12px] text-[var(--ink-3)]' dataSlot='data-state-loading-copy' gap='compact'>")
    expect(source).toContain("<Empty>")
    expect(source).toContain('<EmptyHeader>')
    expect(source).toContain('<EmptyTitle>{title}</EmptyTitle>')
    expect(source).toContain('{description ? <EmptyDescription>{description}</EmptyDescription> : null}')
    expect(source).toContain('{action ? <EmptyContent>{action}</EmptyContent> : null}')
    expect(source).toContain("<CardContentStack className='p-[14px] text-[12px]' dataSlot='data-state-error-content'>")
    expect(source).not.toContain("<CardContentStack className='p-6 text-muted-foreground text-sm' dataSlot='data-state-loading-content'>{label}</CardContentStack>")
    expect(source).not.toContain("<span className='text-[var(--ae-warn)]' data-slot='data-state-error-message'>{message}</span>")
  })
})
