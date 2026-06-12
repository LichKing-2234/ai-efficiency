import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, test } from 'vitest'
import { SegmentedField } from './segmented-field'

describe('SegmentedField', () => {
  test('renders a labeled segmented control inside the shared field shell', () => {
    const html = renderToStaticMarkup(
      <SegmentedField
        ariaLabel='Credential kind'
        id='credential-kind'
        label='Credential kind'
        options={[
          { value: 'secret_text', label: 'Secret text' },
          { value: 'username_password', label: 'Username + password' }
        ]}
        value='secret_text'
        onChange={() => undefined}
      />
    )

    expect(html).toContain('data-slot="field"')
    expect(html).toContain('for="credential-kind"')
    expect(html).toContain('data-slot="labeled-segmented-control"')
    expect(html).toContain('Credential kind')
  })

  test('marks the field disabled when the segmented choice should not be editable', () => {
    const html = renderToStaticMarkup(
      <SegmentedField
        ariaLabel='Code platforms'
        disabled
        id='scm-platform'
        label='Code platforms'
        options={[
          { value: 'github', label: 'GitHub' },
          { value: 'bitbucket', label: 'Bitbucket' }
        ]}
        value='github'
        onChange={() => undefined}
      />
    )

    expect(html).toContain('data-disabled="true"')
  })
})
