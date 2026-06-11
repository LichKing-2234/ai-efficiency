import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { FieldDescription, FieldError, FieldGroup, FieldLabel, FieldLegend, FieldSeparator, FieldTitle } from './field'

describe('FieldGroup', () => {
  test('supports compact form rhythm without page-local gap classes', () => {
    const html = renderToStaticMarkup(
      <FieldGroup gap='compact'>
        <div>Repository URL</div>
      </FieldGroup>
    )

    expect(html).toContain('data-slot="field-group"')
    expect(html).toContain('gap-3')
    expect(html).not.toContain('gap-5')
  })

  test('keeps field labels in the compact reference typography instead of oversized default label chrome', () => {
    const html = renderToStaticMarkup(<FieldLabel htmlFor='sample'>Sample</FieldLabel>)

    expect(html).toContain('data-slot="field-label"')
    expect(html).toContain('text-[11px]')
    expect(html).toContain('uppercase')
    expect(html).toContain('tracking-[0.04em]')
    expect(html).toContain('font-medium')
    expect(html).toContain('text-[var(--ink-3)]')
  })

  test('keeps field helper copy and errors on the denser tokenized hierarchy', () => {
    const title = renderToStaticMarkup(<FieldTitle>Provider</FieldTitle>)
    const description = renderToStaticMarkup(<FieldDescription>Connect your account first.</FieldDescription>)
    const legend = renderToStaticMarkup(<FieldLegend>Runtime</FieldLegend>)
    const separator = renderToStaticMarkup(<FieldSeparator>Or</FieldSeparator>)
    const error = renderToStaticMarkup(<FieldError>Missing token</FieldError>)

    expect(title).toContain('text-[12.5px]')
    expect(title).toContain('text-[var(--ink-2)]')
    expect(description).toContain('text-[12px]')
    expect(description).toContain('text-[var(--ink-3)]')
    expect(legend).toContain('text-[14px]')
    expect(legend).toContain('text-[var(--ink)]')
    expect(separator).toContain('text-[11px]')
    expect(separator).toContain('uppercase')
    expect(error).toContain('text-[12px]')
    expect(error).toContain('text-destructive')
  })
})
